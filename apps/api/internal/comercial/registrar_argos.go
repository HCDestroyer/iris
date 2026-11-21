package comercial

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Prospectos registrados desde Argos (contrato argosisp.md 0.10.0 §3.6, A44; pedido B23). Una
// persona de Argos (oficina, caja o soporte) registra en Iris a alguien interesado; ventas lo
// atiende como cualquier prospecto. El token de Argos solo registra: no lee, lista ni edita.
//
// Lo que habla con Argos (planes del modo, el cliente elegido) y la planta externa corre fuera de
// la transacción, como EvaluarFactibilidad; el caso de uso guarda todo en una.

// OrigenArgosPanel: lo registró una persona en el panel de Argos.
const OrigenArgosPanel = "argos_panel"

// Límites de §3.6 (cuentan números, no peticiones).
const (
	LimiteNumerosActor   = 30
	LimiteNumerosEmpresa = 300
	ventanaLimite        = time.Minute
)

// VigenciaClaveArgos: Iris guarda la Idempotency-Key de §3.6 al menos 30 días.
const VigenciaClaveArgos = 30 * 24 * time.Hour

// RutaRegistrarArgos es la ruta de §3.6.
const RutaRegistrarArgos = "/api/v1/integraciones/argosisp/prospectos"

// ReglaIdempotenciaArgos: la regla del núcleo para la ruta de §3.6 (clave de 30 días, huella con
// X-Iris-Empresa y X-Iris-Actor y el código del contrato). nil en cualquier otra ruta.
func ReglaIdempotenciaArgos(r *http.Request) *nucleo.ReglaIdempotencia {
	if r.Method != http.MethodPost || r.URL.Path != RutaRegistrarArgos {
		return nil
	}
	return &nucleo.ReglaIdempotencia{Vigencia: VigenciaClaveArgos, Cabeceras: []string{identidad.CabeceraEmpresa, CabeceraActorIris},
		CodigoReutilizada: "idempotency_key_reutilizada",
		// El tope por minuto y el conector apagado no son el resultado del registro: Argos reintenta con
		// la misma clave y los mismos bytes, y entonces se ejecuta.
		Transitorio: func(status int, codigo string) bool {
			return status == http.StatusTooManyRequests || (status == http.StatusConflict && codigo == "argos_apagado")
		}}
}

// CabeceraActorIris: quién lo registró en Argos (usuario:<id de Argos>).
const CabeceraActorIris = "X-Iris-Actor"

// TelefonoArgosEntrada es un número del prospecto (§3.6).
type TelefonoArgosEntrada struct {
	Numero    string  `json:"numero" maxLength:"32" doc:"E.164 (+50255123344)"`
	Whatsapp  bool    `json:"whatsapp"`
	Principal bool    `json:"principal" doc:"Exactamente uno"`
	Nombre    *string `json:"nombre,omitempty" nullable:"true" maxLength:"200" doc:"De quién es el número si no es del prospecto (al ganar, contacto del cliente)"`
	Relacion  *string `json:"relacion,omitempty" nullable:"true" maxLength:"20" doc:"titular (solo el principal), familiar u otro"`
}

// EntradaRegistrarArgos es el cuerpo de POST /integraciones/argosisp/prospectos (snake_case, como
// el contrato).
type EntradaRegistrarArgos struct {
	Origen        string                 `json:"origen" enum:"oficina,llamada,whatsapp,referido,otro"`
	Nombre        string                 `json:"nombre" minLength:"2" maxLength:"200"`
	Telefonos     []TelefonoArgosEntrada `json:"telefonos" minItems:"1" maxItems:"5"`
	Modo          string                 `json:"modo" enum:"residencial,empresarial"`
	Direccion     string                 `json:"direccion" minLength:"1" maxLength:"300"`
	ZonaID        *int64                 `json:"zona_id,omitempty" nullable:"true" minimum:"1"`
	Coordenadas   *string                `json:"coordenadas,omitempty" nullable:"true" maxLength:"60" doc:"\"lat,lng\""`
	PlanInteresID *int64                 `json:"plan_interes_id,omitempty" nullable:"true" minimum:"1" doc:"Plan de Argos aplicable al modo"`
	Comentario    *string                `json:"comentario,omitempty" nullable:"true" maxLength:"500"`
	ClienteID     *int64                 `json:"cliente_id,omitempty" nullable:"true" minimum:"1" doc:"Cliente existente que eligió quien lo registró (de la empresa)"`

	// Lo pone el handler.
	Actor             string `json:"-"`
	ClaveIdempotencia string `json:"-"`
}

// ProspectoRegistrado es la respuesta 201 de §3.6.
type ProspectoRegistrado struct {
	ProspectoID int64  `json:"prospecto_id"`
	Numero      string `json:"numero"`
	Estado      string `json:"estado"`
	Asignado    bool   `json:"asignado" doc:"Ya tiene vendedor"`
	URLIris     string `json:"url_iris" doc:"Enlace plano al prospecto en Iris (el autologin se pide al hacer clic, §1.3)"`
}

// resultadoRegistroArgos: lo que devuelve el caso de uso (Duplicado = 409 prospecto_duplicado,
// con el pedido ya sumado como actividad del existente).
type resultadoRegistroArgos struct {
	ProspectoRegistrado
	Duplicado bool `json:"duplicado"`
}

// entradaRegistroArgos: la entrada validada, con lo que se leyó fuera de la transacción.
type entradaRegistroArgos struct {
	EntradaRegistrarArgos
	telefonos       []telefonoValidado
	lat, lon        *float64
	clienteValidado *bool
	evaluacion      *Evaluacion
}

// MarshalJSON de la auditoría: los números quedan con los 4 últimos dígitos; sin nombres,
// dirección ni comentario.
func (e entradaRegistroArgos) MarshalJSON() ([]byte, error) {
	return jsonDe(e.auditable(e.clienteValidado, e.lat != nil))
}

// MarshalJSON de la auditoría de la entrada sin validar (los rechazos también se auditan): igual
// de enmascarada.
func (e EntradaRegistrarArgos) MarshalJSON() ([]byte, error) {
	return jsonDe(e.auditable(nil, e.Coordenadas != nil && *e.Coordenadas != ""))
}

func (e EntradaRegistrarArgos) auditable(validado *bool, conCoordenadas bool) map[string]any {
	tels := make([]map[string]any, 0, len(e.Telefonos))
	for _, t := range e.Telefonos {
		tels = append(tels, map[string]any{"numero": enmascarar(strings.TrimSpace(t.Numero)), "whatsapp": t.Whatsapp, "principal": t.Principal,
			"con_nombre": t.Nombre != nil, "relacion": t.Relacion})
	}
	return map[string]any{"origen": e.Origen, "modo": e.Modo, "telefonos": tels, "zona_id": e.ZonaID,
		"plan_interes_id": e.PlanInteresID, "cliente_id": e.ClienteID, "cliente_validado": validado,
		"con_coordenadas": conCoordenadas, "actor": e.Actor}
}

type telefonoValidado struct {
	numero    string
	whatsapp  bool
	principal bool
	nombre    *string
	relacion  *string
}

var (
	patronE164  = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	patronActor = regexp.MustCompile(`^usuario:[A-Za-z0-9_-]{1,64}$`)
)

// enmascarar deja ver solo los 4 últimos dígitos (+502 ****-3344).
func enmascarar(numero string) string {
	if len(numero) <= 4 {
		return "****"
	}
	return "****" + numero[len(numero)-4:]
}

// validarTelefonos aplica las reglas de §3.6: de 1 a 5, E.164, exactamente un principal, relacion
// titular solo en el principal, sin repetir. Los errores son por posición y nunca repiten el número.
func validarTelefonos(entrada []TelefonoArgosEntrada) ([]telefonoValidado, error) {
	var errs []nucleo.ErrorCampo
	mal := func(i int, campo, mensaje, regla string) {
		errs = append(errs, nucleo.ErrorCampo{Field: fmt.Sprintf("telefonos[%d].%s", i, campo), Message: mensaje, Rule: regla})
	}
	if len(entrada) < 1 || len(entrada) > 5 {
		errs = append(errs, nucleo.ErrorCampo{Field: "telefonos", Message: "De 1 a 5 números", Rule: "cantidad"})
	}
	salida := make([]telefonoValidado, 0, len(entrada))
	vistos := map[string]bool{}
	principales := 0
	for i, t := range entrada {
		v := telefonoValidado{whatsapp: t.Whatsapp, principal: t.Principal}
		numero := strings.TrimSpace(t.Numero)
		e164, _, ok := formato.NormalizarTelefono(numero)
		switch {
		case !patronE164.MatchString(numero) || !ok || e164 != numero:
			mal(i, "numero", "Número no válido: E.164 (+ y de 8 a 15 dígitos; Guatemala +502 y 8 dígitos que empiezan en 2–7)", "e164")
		case vistos[numero]:
			mal(i, "numero", "Número repetido", "unico")
		}
		vistos[numero] = true
		v.numero = numero
		if t.Principal {
			principales++
		}
		if t.Nombre != nil {
			n := strings.TrimSpace(*t.Nombre)
			if n == "" || len([]rune(n)) > 200 {
				mal(i, "nombre", "De 1 a 200 caracteres", "longitud")
			} else {
				v.nombre = &n
			}
		}
		if t.Relacion != nil {
			r := *t.Relacion
			switch {
			case !slices.Contains([]string{"titular", "familiar", "otro"}, r):
				mal(i, "relacion", "titular, familiar u otro", "enum")
			case t.Principal && r != "titular":
				mal(i, "relacion", "El principal no lleva relación o lleva titular", "principal")
			case !t.Principal && r == "titular":
				mal(i, "relacion", "titular solo vale en el principal", "titular")
			default:
				v.relacion = &r
			}
		}
		salida = append(salida, v)
	}
	if len(entrada) > 0 && principales != 1 {
		errs = append(errs, nucleo.ErrorCampo{Field: "telefonos", Message: "Exactamente un número principal", Rule: "principal"})
	}
	if len(errs) > 0 {
		return nil, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "telefonos_invalidos", "Teléfonos inválidos",
			"Revisa los números del prospecto", errs...)
	}
	return salida, nil
}

// coordenadasArgos: "lat,lng" de Argos.
func coordenadasArgos(v *string) (lat, lon *float64, err error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil, nil
	}
	partes := strings.Split(*v, ",")
	invalida := campoInvalido("coordenadas", "Coordenadas \"lat,lng\" válidas", "coordenadas")
	if len(partes) != 2 {
		return nil, nil, invalida
	}
	la, err1 := strconv.ParseFloat(strings.TrimSpace(partes[0]), 64)
	lo, err2 := strconv.ParseFloat(strings.TrimSpace(partes[1]), 64)
	if err1 != nil || err2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
		return nil, nil, invalida
	}
	return &la, &lo, nil
}

// ValidadorClientes pregunta a Argos por un cliente (argos.Modulo.ValidarCliente).
type ValidadorClientes interface {
	ValidarCliente(ctx context.Context, clienteID int64) (argos.ClienteValidado, error)
}

// LectorPlanesModo lee los planes aplicables a un modo (GET /planes?activo=true&modo=, A40). Un
// Argos anterior a 0.10.0 ignora modo y devuelve todos.
type LectorPlanesModo interface {
	PlanesDeModo(ctx context.Context, modo string) ([]PlanComercial, error)
}

// PlanesDeModo implementa LectorPlanesModo con el conector activo.
func (p planesArgos) PlanesDeModo(ctx context.Context, modo string) ([]PlanComercial, error) {
	cli, err := p.am.ClienteActivo(ctx, nucleo.BDDe(ctx))
	if err != nil {
		return nil, err
	}
	ps, err := conector.Todas[planArgos](ctx, cli.Cliente, "/planes", url.Values{"activo": {"true"}, "modo": {modo}})
	if err != nil {
		return nil, argos.Problema(err)
	}
	salida := make([]PlanComercial, 0, len(ps))
	for _, pl := range ps {
		if pl.Activo != nil && !*pl.Activo {
			continue
		}
		salida = append(salida, PlanComercial{ID: pl.ID, Nombre: pl.Nombre, Precio: pl.Precio.Texto(), Activo: true})
	}
	return salida, nil
}

// RegistrarProspectoArgos: POST /integraciones/argosisp/prospectos. Valida los números, el plan
// (con Argos) y el cliente elegido (instantánea o Argos), evalúa la factibilidad si trae
// coordenadas y guarda en una transacción. Duplicado: 409 prospecto_duplicado { url_iris } con el
// pedido sumado como actividad del prospecto existente.
func (m *Modulo) RegistrarProspectoArgos(ctx context.Context, in EntradaRegistrarArgos) (ProspectoRegistrado, error) {
	if !patronActor.MatchString(in.Actor) {
		return ProspectoRegistrado{}, nucleo.NuevoProblema(http.StatusBadRequest, "cabecera_iris_requerida", "Cabecera de Iris requerida",
			"X-Iris-Actor debe ser usuario:<id de Argos>")
	}
	// Todo el cuerpo se valida antes de abrir una transacción: ningún rechazo de forma llega
	// después de contar los números ni de buscar duplicados.
	e := entradaRegistroArgos{EntradaRegistrarArgos: in}
	var err error
	if err = validarCuerpoArgos(in); err != nil {
		return ProspectoRegistrado{}, err
	}
	if e.telefonos, err = validarTelefonos(in.Telefonos); err != nil {
		return ProspectoRegistrado{}, err
	}
	if e.lat, e.lon, err = coordenadasArgos(in.Coordenadas); err != nil {
		return ProspectoRegistrado{}, err
	}
	// Permiso, empresa y conector antes de hablar con Argos.
	previo, err := m.prepararRegistroArgos.Ejecutar(ctx, in)
	if err != nil {
		return ProspectoRegistrado{}, err
	}
	// Los números cuentan para el tope en su propia transacción, ya confirmada, antes de buscar
	// duplicados: cualquier resultado (201, 409, 422 o 500) los gasta. Así nadie sondea qué números
	// están en un prospecto sin que cuente.
	if _, err := m.contarRegistroArgos.Ejecutar(ctx, e); err != nil {
		return ProspectoRegistrado{}, err
	}
	if in.PlanInteresID != nil {
		if err := m.validarPlanModo(ctx, *in.PlanInteresID, in.Modo); err != nil {
			return ProspectoRegistrado{}, err
		}
	}
	if in.ClienteID != nil {
		switch previo.Cliente {
		case clienteDeLaEmpresa:
			e.clienteValidado = ptr(true)
		case clienteDeOtraEmpresa:
			return ProspectoRegistrado{}, clienteNoValido()
		default:
			v, err := m.validarClienteArgos(ctx, *in.ClienteID, previo.EmpresaID)
			if err != nil {
				return ProspectoRegistrado{}, err
			}
			e.clienteValidado = &v
		}
	}
	if e.lat != nil {
		ev, err := m.cfg.Planta.Evaluar(ctx, &previo.EmpresaID, *e.lat, *e.lon)
		if err != nil {
			motor := ev.Motor
			if motor == "" {
				motor = "planta"
			}
			ev = Evaluacion{Motor: motor, Resultado: FactibleIndeterminado, Motivo: MotivoPlantaNoDisponible}
		}
		e.evaluacion = &ev
	}
	r, err := m.registrarArgos.Ejecutar(ctx, e)
	if err != nil {
		return ProspectoRegistrado{}, err
	}
	if r.Duplicado {
		p := nucleo.Conflicto("prospecto_duplicado", "Uno de los números ya está en un prospecto abierto de la empresa; el pedido se sumó como actividad")
		p.URLIris = &r.URLIris
		return ProspectoRegistrado{}, p
	}
	return r.ProspectoRegistrado, nil
}

// validarCuerpoArgos: los textos del cuerpo (longitudes y sin NUL) antes de cualquier transacción.
func validarCuerpoArgos(in EntradaRegistrarArgos) error {
	if _, err := texto("nombre", in.Nombre, 2, 200); err != nil {
		return err
	}
	if _, err := texto("direccion", in.Direccion, 1, 300); err != nil {
		return err
	}
	if _, err := texto("comentario", deref(in.Comentario), 0, 500); err != nil {
		return err
	}
	textos := []string{in.Origen, in.Nombre, in.Modo, in.Direccion, deref(in.Coordenadas), deref(in.Comentario)}
	for _, t := range in.Telefonos {
		textos = append(textos, t.Numero, deref(t.Nombre), deref(t.Relacion))
	}
	for _, t := range textos {
		if strings.ContainsRune(t, 0) {
			return campoInvalido("cuerpo", "El texto no puede llevar el carácter NUL", "texto")
		}
	}
	return nil
}

func clienteNoValido() error {
	return nucleo.ReglaNegocio("cliente_no_valido", "El cliente no es de la empresa")
}

// Lo que dice la instantánea del cliente elegido.
const (
	clienteDesconocido   = ""
	clienteDeLaEmpresa   = "de_la_empresa"
	clienteDeOtraEmpresa = "de_otra_empresa"
)

// previoRegistroArgos: lo leído antes de hablar con Argos.
type previoRegistroArgos struct {
	EmpresaID int64  `json:"empresa_id"`
	Cliente   string `json:"cliente,omitempty" doc:"de_la_empresa, de_otra_empresa o vacío (no está en la instantánea)"`
}

// validarPlanModo: el plan es de los aplicables al modo (422 plan_no_permitido). Si Argos no
// responde, el plan queda como vino (Argos lo vuelve a validar al cotizar y al activar).
func (m *Modulo) validarPlanModo(ctx context.Context, plan int64, modo string) error {
	lector, ok := m.cfg.Planes.(LectorPlanesModo)
	if !ok {
		return nil
	}
	ps, err := lector.PlanesDeModo(ctx, modo)
	var pr *nucleo.Problema
	if errors.As(err, &pr) && pr.Status >= 500 {
		return nil
	}
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(ps, func(p PlanComercial) bool { return p.ID == plan }) {
		return nucleo.ReglaNegocio("plan_no_permitido", "El plan no está activo o no es aplicable al modo")
	}
	return nil
}

// validarClienteArgos: el cliente elegido es de la empresa según Argos. true = validado; false =
// Argos no está disponible (se confirma al ganar, §3.6 «cliente_validado: false»). Argos que
// rechaza la lectura, no lo tiene o lo tiene en otra empresa: 422 cliente_no_valido.
func (m *Modulo) validarClienteArgos(ctx context.Context, cliente, empresa int64) (bool, error) {
	if m.cfg.Clientes == nil {
		return false, nil
	}
	v, err := m.cfg.Clientes.ValidarCliente(ctx, cliente)
	if err != nil {
		return false, err
	}
	if !v.Validado {
		if v.Motivo == argos.MotivoArgosNoDisponible {
			return false, nil
		}
		return false, clienteNoValido()
	}
	// Un cliente sin servicios no es de ninguna empresa todavía: lo acepta cualquiera del alcance.
	if !v.Existe || (!v.SinServicios && !slices.Contains(v.Empresas, empresa)) {
		return false, clienteNoValido()
	}
	return true, nil
}

// exigirEmpresaYConector: la empresa de X-Iris-Empresa y el conector Argos activo (409 argos_apagado).
// Solo la integración de Argos (un token de usuario de tipo integración): 403 solo_integracion_argos.
func exigirEmpresaYConector(ctx context.Context, q *comercialbd.Queries) (int64, error) {
	if contexto.ActorDe(ctx).Tipo != contexto.ActorIntegracion {
		return 0, nucleo.NuevoProblema(http.StatusForbidden, "solo_integracion_argos", "Solo la integración de Argos",
			"Esta ruta la usa solo el token de integración de Argos")
	}
	_, empresa := filtroEmpresa(ctx)
	if empresa == nil || *empresa == 0 {
		return 0, nucleo.EmpresaRequerida()
	}
	estado, err := q.EstadoConectorArgos(ctx)
	if err != nil {
		return 0, err
	}
	if estado != "activo" {
		return 0, nucleo.Conflicto("argos_apagado", "El conector con Argos está apagado en Iris")
	}
	return *empresa, nil
}

func (m *Modulo) nuevoPrepararRegistroArgos() *auditoria.Caso[EntradaRegistrarArgos, previoRegistroArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrarArgos, previoRegistroArgos]{
		Accion: "prospectos.registrar_integracion", Permiso: nucleo.PermProspectosRegistrarIntegracion,
		AuditarExito: soloLecturas[previoRegistroArgos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarArgos) (previoRegistroArgos, error) {
			q := comercialbd.New(t)
			empresa, err := exigirEmpresaYConector(ctx, q)
			if err != nil {
				return previoRegistroArgos{}, err
			}
			p := previoRegistroArgos{EmpresaID: empresa}
			if in.ClienteID != nil {
				c, err := q.ClienteEnEmpresaInstantanea(ctx, comercialbd.ClienteEnEmpresaInstantaneaParams{ClienteID: *in.ClienteID, EmpresaID: empresa})
				if err != nil {
					return p, err
				}
				switch {
				case c.DeLaEmpresa != nil && *c.DeLaEmpresa:
					p.Cliente = clienteDeLaEmpresa
				case c.Existe:
					// La instantánea lo tiene, y no en esta empresa.
					p.Cliente = clienteDeOtraEmpresa
				}
			}
			return p, nil
		},
	})
}

func (m *Modulo) nuevoRegistrarArgos() *auditoria.Caso[entradaRegistroArgos, resultadoRegistroArgos] {
	return auditoria.Nuevo(auditoria.Caso[entradaRegistroArgos, resultadoRegistroArgos]{
		Accion: "prospectos.registrar_integracion", Permiso: nucleo.PermProspectosRegistrarIntegracion,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRegistroArgos) (resultadoRegistroArgos, error) {
			q := comercialbd.New(t)
			empresa, err := exigirEmpresaYConector(ctx, q)
			if err != nil {
				return resultadoRegistroArgos{}, err
			}
			t.FijarEmpresa(empresa)
			if err := q.BloquearRegistrosArgos(ctx, empresa); err != nil {
				return resultadoRegistroArgos{}, err
			}
			ahora := m.ahora()
			if err := validarZona(ctx, t, in.ZonaID, &empresa); err != nil {
				return resultadoRegistroArgos{}, err
			}
			numeros := make([]string, 0, len(in.telefonos))
			formatoArgos := make([]string, 0, len(in.telefonos))
			for _, tel := range in.telefonos {
				numeros = append(numeros, tel.numero)
				if f, ok := formato.TelefonoArgos(tel.numero); ok {
					formatoArgos = append(formatoArgos, f)
				}
			}
			existente, err := q.ProspectoAbiertoConNumeros(ctx, comercialbd.ProspectoAbiertoConNumerosParams{
				EmpresaID: &empresa, Numeros: numeros, Telefonos: formatoArgos})
			switch {
			case err == nil:
				return m.sumarPedidoArgos(ctx, t, q, existente, in, ahora)
			case !errors.Is(err, pgx.ErrNoRows):
				return resultadoRegistroArgos{}, err
			}
			return m.crearProspectoArgos(ctx, t, q, empresa, in, ahora)
		},
	})
}

// nuevoContarRegistroArgos: cuenta los números de la petición para el tope (su propia
// transacción, que se confirma aunque lo que sigue falle). 429 sin anotar si ya se pasó.
func (m *Modulo) nuevoContarRegistroArgos() *auditoria.Caso[entradaRegistroArgos, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[entradaRegistroArgos, struct{}]{
		Accion: "prospectos.registrar_integracion", Permiso: nucleo.PermProspectosRegistrarIntegracion,
		AuditarExito: soloLecturas[struct{}],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRegistroArgos) (struct{}, error) {
			q := comercialbd.New(t)
			empresa, err := exigirEmpresaYConector(ctx, q)
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(empresa)
			if err := q.BloquearRegistrosArgos(ctx, empresa); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, m.limiteArgos(ctx, q, in.Actor, empresa, len(in.telefonos), m.ahora())
		},
	})
}

// limiteArgos: 30 números por minuto por actor y 300 por empresa (429 con Retry-After). Anota los
// de esta petición.
func (m *Modulo) limiteArgos(ctx context.Context, q *comercialbd.Queries, actor string, empresa int64, numeros int, ahora time.Time) error {
	if err := q.PurgarRegistrosArgos(ctx, ahora.Add(-time.Hour)); err != nil {
		return err
	}
	c, err := q.NumerosRegistradosArgos(ctx, comercialbd.NumerosRegistradosArgosParams{Actor: actor, EmpresaID: empresa, Desde: ahora.Add(-ventanaLimite)})
	if err != nil {
		return err
	}
	if c.PorActor+int64(numeros) > LimiteNumerosActor || c.PorEmpresa+int64(numeros) > LimiteNumerosEmpresa {
		return nucleo.DemasiadosIntentos("demasiados_registros", "Demasiados números registrados en el último minuto; reintenta después", ventanaLimite)
	}
	return q.AnotarRegistroArgos(ctx, comercialbd.AnotarRegistroArgosParams{Actor: actor, EmpresaID: empresa, Numeros: int32(numeros), Ahora: ahora}) //nolint:gosec // de 1 a 5
}

// urlProspecto: enlace plano al prospecto en el panel de Iris.
func (m *Modulo) urlProspecto(id int64) string {
	return strings.TrimRight(m.cfg.URLBase, "/") + "/comercial/" + strconv.FormatInt(id, 10)
}

// sumarPedidoArgos: el número ya está en un prospecto abierto; el pedido se suma como actividad
// (sin decir qué número coincidió ni datos del existente).
func (m *Modulo) sumarPedidoArgos(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, p comercialbd.ComercialProspecto,
	in entradaRegistroArgos, ahora time.Time) (resultadoRegistroArgos, error) {
	desc := "Nuevo pedido registrado desde Argos (" + in.Origen + ")"
	if c := strings.TrimSpace(deref(in.Comentario)); c != "" {
		desc += ": " + c
	}
	a, err := q.CrearActividad(ctx, comercialbd.CrearActividadParams{EmpresaID: p.EmpresaID, ProspectoID: p.ID, Tipo: "nota",
		Descripcion: desc, OcurridaEn: ahora, AutorID: actorUsuario(t)})
	if err != nil {
		return resultadoRegistroArgos{}, err
	}
	t.FijarEntidad("prospecto", p.ID)
	t.RegistrarCambios("actividad", a.ID, nil, a)
	t.Anotar(map[string]any{"duplicado": true})
	if err := t.Emitir("prospecto.actividad_registrada", map[string]any{
		"prospecto_id": p.ID, "actividad_id": a.ID, "tipo": a.Tipo, "proxima_fecha": nil, "origen": OrigenArgosPanel,
	}); err != nil {
		return resultadoRegistroArgos{}, err
	}
	return resultadoRegistroArgos{ProspectoRegistrado: ProspectoRegistrado{URLIris: m.urlProspecto(p.ID)}, Duplicado: true}, nil
}

func (m *Modulo) crearProspectoArgos(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, empresa int64,
	in entradaRegistroArgos, _ time.Time) (resultadoRegistroArgos, error) {
	var principal string
	for _, tel := range in.telefonos {
		if tel.principal {
			principal, _ = formato.TelefonoArgos(tel.numero)
		}
	}
	d := comercialbd.CrearProspectoArgosParams{EmpresaID: &empresa, OrigenDetalle: &in.Origen, Telefono: &principal,
		Lat: in.lat, Lon: in.lon, ZonaID: in.ZonaID, PlanInteresID: in.PlanInteresID, Modo: &in.Modo, RegistradoPorArgos: &in.Actor,
		ClienteExistenteID: in.ClienteID, ClienteValidado: in.clienteValidado, Idempotencia: conIdempotencia(ctx, in.ClaveIdempotencia)}
	var err error
	if d.Nombre, err = texto("nombre", in.Nombre, 2, 200); err != nil {
		return resultadoRegistroArgos{}, err
	}
	if d.Direccion, err = texto("direccion", in.Direccion, 1, 300); err != nil {
		return resultadoRegistroArgos{}, err
	}
	if d.Notas, err = texto("comentario", deref(in.Comentario), 0, 500); err != nil {
		return resultadoRegistroArgos{}, err
	}
	if in.ZonaID != nil {
		if d.VendedorID, err = vendedorDeZona(ctx, t, q, *in.ZonaID, empresa); err != nil {
			return resultadoRegistroArgos{}, err
		}
	}
	f, err := q.CrearProspectoArgos(ctx, d)
	if err != nil {
		return resultadoRegistroArgos{}, err
	}
	t.FijarEntidad("prospecto", f.ID)
	t.RegistrarCambios("prospecto", f.ID, nil, f)
	for i, tel := range in.telefonos {
		if _, err := q.CrearTelefonoProspecto(ctx, comercialbd.CrearTelefonoProspectoParams{ProspectoID: f.ID, EmpresaID: &empresa,
			Posicion: int16(i), Numero: tel.numero, Whatsapp: tel.whatsapp, Principal: tel.principal, Nombre: tel.nombre, Relacion: tel.relacion}); err != nil { //nolint:gosec // de 0 a 4
			return resultadoRegistroArgos{}, err
		}
	}
	if err := historial(ctx, t, q, f.ID, "creado", nil, &f.Estado, nil, map[string]any{"origen": f.Origen, "registrado_por_argos": in.Actor,
		"cliente_validado": in.clienteValidado}); err != nil {
		return resultadoRegistroArgos{}, err
	}
	if err := t.Emitir("prospecto.creado", map[string]any{
		"prospecto_id": f.ID, "numero": f.Numero, "empresa_id": f.EmpresaID, "origen": f.Origen,
		"zona_id": f.ZonaID, "vendedor_id": f.VendedorID,
	}); err != nil {
		return resultadoRegistroArgos{}, err
	}
	if in.evaluacion != nil {
		if _, err := m.registrarEvaluacion(ctx, t, q, f, *in.evaluacion, "", nil, nil); err != nil {
			return resultadoRegistroArgos{}, err
		}
		if f, err = q.ProspectoPorID(ctx, f.ID); err != nil {
			return resultadoRegistroArgos{}, err
		}
	}
	return resultadoRegistroArgos{ProspectoRegistrado: ProspectoRegistrado{ProspectoID: f.ID, Numero: f.Numero, Estado: f.Estado,
		Asignado: f.VendedorID != nil, URLIris: m.urlProspecto(f.ID)}}, nil
}

// vendedorDeZona: el vendedor del prospecto por su zona (§3.6 «le asigna vendedor por zona»): la
// persona activa con prospectos.contactar en esa zona y en la empresa, con menos prospectos
// abiertos. nil si no hay (queda sin asignar).
func vendedorDeZona(ctx context.Context, t *auditoria.Tx, q *comercialbd.Queries, zona, empresa int64) (*int64, error) {
	ids, err := q.VendedoresDeZona(ctx, &zona)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		ok, err := identidad.UsuarioEnEmpresa(ctx, t, id, empresa)
		if err != nil {
			return nil, err
		}
		if ok {
			return &id, nil
		}
	}
	return nil, nil
}
