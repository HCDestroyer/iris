package argos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Portal del cliente de Argos (contrato 0.9.0 §11, bloque P; §11.10 de 0.12.1). Argos llama a Iris
// servidor a servidor con el token_portal (§1.2), en nombre de un cliente que su portal ya
// identificó. Aquí se resuelve la sesión de cada petición, antes del caso de uso que la atiende
// (soporte para los tickets, comercial para las solicitudes):
//
//  1. el token es el token_portal de un conector Argos (403 solo_integracion_argos) y ese conector
//     está activo (409 argos_apagado);
//  2. X-Iris-Empresa y X-Argos-Portal-Sesion (400 cabecera_requerida) y, con contrato ≥ 0.12.1,
//     X-Iris-Contacto (400 cabecera_iris_requerida);
//  3. el tope del conector (600 por minuto, 429 demasiadas_peticiones);
//  4. el cliente es de la empresa: por la instantánea o, si Iris no lo conoce, leyéndolo en Argos
//     (422 cliente_no_existe o empresa_no_coincide). Si Argos no responde, la sesión queda con
//     ClienteValidado = false: solo POST …/tickets la acepta (las demás rutas, 503);
//  5. con un contacto, su caché (§11.10): si falta o su versión es menor que
//     X-Iris-Contacto-Version, se relee en Argos antes de responder (503 si no responde).
//
// Lo que habla con Argos corre fuera de la transacción; las dos partes con la base son casos de
// uso que auditan los rechazos (el éxito lo audita la acción que sigue, con en_nombre_de).

// ContratoPortal es el primer contrato con el portal del cliente de Argos (§4.3).
const ContratoPortal = "0.9.0"

// ContratoContactoPortal es el primer contrato con X-Iris-Contacto en §11 (0.12.1, §11.10).
const ContratoContactoPortal = "0.12.1"

// SoportaPortal indica si un contrato incluye el portal del cliente de Argos (§11).
func SoportaPortal(contrato *string) bool { return conector.VersionAlMenos(contrato, ContratoPortal) }

// TopePeticionesPortal es el tope del conector (§11.2): peticiones por minuto.
const TopePeticionesPortal = 600

// Cabeceras de §11.
const (
	CabeceraPortalSesion    = "X-Argos-Portal-Sesion"
	CabeceraContacto        = "X-Iris-Contacto"
	CabeceraContactoVersion = "X-Iris-Contacto-Version"
)

// ContactoTitular es el valor literal de X-Iris-Contacto cuando la sesión es del titular.
const ContactoTitular = "titular"

// ActorLecturaContactos es el X-Iris-Actor con que Iris lee GET /clientes/:id/contactos para su
// caché (0.14.2 §7.1: «sistema», solo en esa lectura, en /cuentas-bancarias y en /bancos, y nunca con
// X-Iris-Conversacion ni X-Iris-Contacto).
const ActorLecturaContactos = "sistema"

var (
	patronPortalSesion = regexp.MustCompile(`^(documento|ip):([A-Za-z0-9_-]{8,64})$`)
	patronIDArgos      = regexp.MustCompile(`^[1-9]\d{0,17}$`)
	patronVersion      = regexp.MustCompile(`^[1-9]\d{0,9}$`)
)

// PrefijoRutasPortal es el prefijo de las rutas de §11.3.
const PrefijoRutasPortal = "/api/v1/integraciones/argosisp/portal/clientes/"

// MaxCuerpoPortal: el cuerpo de un POST de §11 con 3 adjuntos de 10 MB (§11.6), más el JSON.
const MaxCuerpoPortal = 3*(10<<20) + 1<<20

// ReglaIdempotenciaPortal: la regla del núcleo para los POST de §11 (nil en cualquier otra ruta). La
// clave es por credencial y la ruta lleva el cliente (§11.2); la huella incluye X-Iris-Empresa y
// X-Iris-Contacto (otra empresa u otro contacto con la misma clave: 422 idempotencia_reutilizada). Un
// tope (429) o el conector apagado no son el resultado: la misma petición se ejecuta al repetirla.
func ReglaIdempotenciaPortal(r *http.Request) *nucleo.ReglaIdempotencia {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, PrefijoRutasPortal) {
		return nil
	}
	return &nucleo.ReglaIdempotencia{Cabeceras: []string{"X-Iris-Empresa", CabeceraContacto}, MaxCuerpo: MaxCuerpoPortal,
		Transitorio: func(status int, codigo string) bool {
			return status == http.StatusTooManyRequests || (status == http.StatusConflict && codigo == "argos_apagado")
		}}
}

// CabecerasPortal es lo que llega en cada petición de §11 (el cliente va en la ruta).
type CabecerasPortal struct {
	ClienteID       int64  `json:"cliente_id"`
	Empresa         string `json:"empresa"`
	Sesion          string `json:"sesion"`
	Contacto        string `json:"contacto,omitempty"`
	ContactoVersion string `json:"contacto_version,omitempty"`
}

// ContactoPortal es el contacto de la sesión (§11.10), de la caché.
type ContactoPortal struct {
	ID     int64  `json:"contacto_id"`
	Nombre string `json:"-"`
	// Servicios: los del contacto; nil = todos los del cliente.
	Servicios []int64 `json:"servicios"`
	Reportar  bool    `json:"reportar"`
	Consultar bool    `json:"consultar"`
}

// CubreServicio indica si el servicio está entre los del contacto (nil = todos).
func (c ContactoPortal) CubreServicio(servicio int64) bool {
	return c.Servicios == nil || slices.Contains(c.Servicios, servicio)
}

// SesionPortal es la sesión resuelta de una petición de §11.
type SesionPortal struct {
	ConectorID int64 `json:"conector_id"`
	ClienteID  int64 `json:"cliente_id"`
	EmpresaID  int64 `json:"empresa_id"`
	// Metodo: documento o ip (cómo identificó Argos al cliente); Sesion: el id opaco de la sesión.
	Metodo string `json:"metodo"`
	Sesion string `json:"sesion"`
	// Contacto: la sesión es de un contacto de otro cliente (§11.10); nil = del titular.
	Contacto *ContactoPortal `json:"contacto,omitempty"`
	// ClienteValidado: false si Iris no conocía al cliente y Argos no respondió (solo POST …/tickets).
	ClienteValidado bool `json:"cliente_validado"`
}

// EsIP indica una sesión abierta por la IP del cliente (§11.2: solo la lista, categorías y abrir
// tickets sin adjuntos).
func (s SesionPortal) EsIP() bool { return s.Metodo == "ip" }

// EnNombreDe es lo que va en la auditoría de cada acción (§11.1).
func (s SesionPortal) EnNombreDe() map[string]any {
	e := map[string]any{"cliente_id": s.ClienteID, "empresa_id": s.EmpresaID, "metodo": s.Metodo, "sesion": s.Sesion}
	if s.Contacto != nil {
		e["contacto_id"] = s.Contacto.ID
	}
	if !s.ClienteValidado {
		e["cliente_validado"] = false
	}
	return e
}

// NombreActor es el nombre con que la acción queda en la auditoría y el historial (§11.1).
func (s SesionPortal) NombreActor() string {
	return fmt.Sprintf("Argos (portal) · cliente %d", s.ClienteID)
}

// ExigirClienteValidado: las rutas que no son POST …/tickets necesitan el cliente confirmado.
func (s SesionPortal) ExigirClienteValidado() error {
	if s.ClienteValidado {
		return nil
	}
	return ArgosNoDisponiblePortal()
}

// ExigirDocumento: la acción exige una sesión con documento (§11.2).
func (s SesionPortal) ExigirDocumento() error {
	if s.EsIP() {
		return nucleo.NuevoProblema(http.StatusForbidden, "sesion_insuficiente", "Sesión insuficiente",
			"Con una sesión abierta por la IP del cliente solo se abren tickets y se ve la lista")
	}
	return nil
}

// ArgosNoDisponiblePortal: 503 argos_no_disponible (§11.2).
func ArgosNoDisponiblePortal() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "argos_no_disponible", "Argos no disponible",
		"Iris no puede confirmar el cliente con Argos en este momento; intenta más tarde").ConCabecera("Retry-After", "30")
}

func cabeceraRequerida(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadRequest, "cabecera_requerida", "Cabecera requerida", detalle)
}

func soloIntegracionArgos() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "solo_integracion_argos", "Solo el portal de Argos",
		"Estas rutas las usa solo el token_portal de un conector Argos")
}

// previoPortal: lo resuelto con la base antes de hablar con Argos.
type previoPortal struct {
	SesionPortal
	conClienteConocido bool
	contactoID         *int64
	contactoVersion    int32
	contacto           *argosbd.IntegracionArgosContacto
	contratoRemoto     *string
}

// entradaCerrarPortal: lo leído de Argos (fuera de la transacción).
type entradaCerrarPortal struct {
	CabecerasPortal
	previo     previoPortal
	validacion *ClienteValidado
	contactos  []ContactoClienteArgos
	// contactosInicio: hora de la base en que empezó la lectura de contactos en Argos.
	contactosInicio time.Time
	errArgos        error
}

// SesionPortal resuelve la sesión de una petición de §11 (ver el comentario del archivo).
func (m *Modulo) SesionPortal(ctx context.Context, cab CabecerasPortal) (SesionPortal, error) {
	previo, err := m.prepararPortal.Ejecutar(ctx, cab)
	if err != nil {
		return SesionPortal{}, err
	}
	in := entradaCerrarPortal{CabecerasPortal: cab, previo: previo}
	if !previo.conClienteConocido {
		v, err := m.ValidarCliente(ctx, cab.ClienteID)
		if err != nil {
			return SesionPortal{}, err
		}
		in.validacion = &v
	}
	if previo.contactoID != nil && (previo.contacto == nil || previo.contacto.Version < previo.contactoVersion) {
		in.contactosInicio, in.contactos, in.errArgos = m.leerContactosArgos(ctx, cab.ClienteID)
		if in.errArgos == nil && in.contactos == nil {
			in.contactos = []ContactoClienteArgos{} // leída y vacía: el contacto ya no está
		}
	}
	return m.cerrarPortal.Ejecutar(ctx, in)
}

// leerContactosArgos: GET /clientes/:id/contactos con el conector activo.
// Devuelve también la hora de la base en que empezó la lectura (para guardarContactos).
func (m *Modulo) leerContactosArgos(ctx context.Context, cliente int64) (time.Time, []ContactoClienteArgos, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return time.Time{}, nil, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return time.Time{}, nil, err
	}
	inicio, err := inicioLectura(ctx, bd)
	if err != nil {
		return time.Time{}, nil, err
	}
	cli := m.clienteDe(c)
	l, err := leerContactosDe(ctx, &Cliente{Cliente: cli.ConCabecera(CabeceraActor, ActorLecturaContactos), contrato: cli.contrato}, cliente)
	return inicio, l, err
}

func nuevoPrepararPortal(m *Modulo) *auditoria.Caso[CabecerasPortal, previoPortal] {
	return auditoria.Nuevo(auditoria.Caso[CabecerasPortal, previoPortal]{
		Accion: "portal_argos.sesion", Acceso: auditoria.Autenticado,
		AuditarExito: soloLecturas[previoPortal],
		Logica: func(ctx context.Context, t *auditoria.Tx, in CabecerasPortal) (previoPortal, error) {
			var p previoPortal
			a := contexto.ActorDe(ctx)
			if a.Tipo != contexto.ActorIntegracion || a.TokenID == 0 {
				return p, soloIntegracionArgos()
			}
			q := argosbd.New(t)
			c, err := q.ConectorDeTokenPortal(ctx, a.TokenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return p, soloIntegracionArgos()
			}
			if err != nil {
				return p, err
			}
			if c.Estado != "activo" {
				return p, nucleo.Conflicto("argos_apagado", "El conector con Argos está apagado en Iris")
			}
			p.ConectorID, p.contratoRemoto = c.ID, c.ContratoRemoto
			// Cabeceras.
			if !patronIDArgos.MatchString(strings.TrimSpace(in.Empresa)) {
				return p, cabeceraRequerida("Falta X-Iris-Empresa o no es el id de una empresa de Argos")
			}
			empresa := contexto.EmpresaDe(ctx)
			if empresa == 0 || strconv.FormatInt(empresa, 10) != strings.TrimSpace(in.Empresa) {
				return p, nucleo.EmpresaNoPermitida()
			}
			ses := patronPortalSesion.FindStringSubmatch(in.Sesion)
			if ses == nil {
				return p, cabeceraRequerida("Falta " + CabeceraPortalSesion + " o no tiene la forma <documento|ip>:<id>")
			}
			p.ClienteID, p.EmpresaID, p.Metodo, p.Sesion = in.ClienteID, empresa, ses[1], ses[2]
			t.FijarEmpresa(empresa)
			t.Anotar(map[string]any{"en_nombre_de": map[string]any{"cliente_id": in.ClienteID, "empresa_id": empresa, "metodo": ses[1], "sesion": ses[2]}})
			if err := topeConector(ctx, c.ID); err != nil {
				return p, err
			}
			if err := contactoDeCabeceras(&p, in); err != nil {
				return p, err
			}
			// El cliente según la instantánea.
			cl, err := q.ClienteEnInstantanea(ctx, in.ClienteID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return p, err
			default:
				p.conClienteConocido = true
				if !clienteDeEmpresa(cl.Empresas, cl.EmpresaID, cl.SinServicios, empresa) {
					return p, empresaNoCoincide()
				}
				p.ClienteValidado = true
			}
			if p.contactoID != nil {
				ct, err := q.ContactoEnCache(ctx, *p.contactoID)
				switch {
				case errors.Is(err, pgx.ErrNoRows):
				case err != nil:
					return p, err
				default:
					p.contacto = &ct
				}
			}
			return p, nil
		},
	})
}

// contactoDeCabeceras aplica X-Iris-Contacto y X-Iris-Contacto-Version (§11.10). Con contrato ≥
// 0.12.1 la cabecera es obligatoria; con uno anterior, si no viene, la sesión es del titular.
func contactoDeCabeceras(p *previoPortal, in CabecerasPortal) error {
	v := strings.TrimSpace(in.Contacto)
	if v == "" {
		if conector.VersionAlMenos(p.contratoRemoto, ContratoContactoPortal) {
			return nucleo.NuevoProblema(http.StatusBadRequest, "cabecera_iris_requerida", "Cabecera de Iris requerida",
				"Falta "+CabeceraContacto+" (<contacto_id> o titular)")
		}
		return nil
	}
	if v == ContactoTitular {
		return nil
	}
	if !patronIDArgos.MatchString(v) {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: CabeceraContacto, Message: "<contacto_id> (entero positivo) o titular", Rule: "formato"})
	}
	id, _ := strconv.ParseInt(v, 10, 64)
	ver := strings.TrimSpace(in.ContactoVersion)
	if !patronVersion.MatchString(ver) {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: CabeceraContactoVersion, Message: "La versión del contacto, entero positivo", Rule: "formato"})
	}
	n, err := strconv.ParseInt(ver, 10, 32)
	if err != nil {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: CabeceraContactoVersion, Message: "Versión fuera de rango", Rule: "formato"})
	}
	p.contactoID, p.contactoVersion = &id, int32(n)
	return nil
}

func empresaNoCoincide() *nucleo.Problema {
	return nucleo.ReglaNegocio("empresa_no_coincide", "El cliente no es de la empresa de X-Iris-Empresa")
}

// clienteDeEmpresa: el cliente de la instantánea es de la empresa si alguno de sus servicios lo es
// (o, en filas anteriores a empresas[], su empresa_id), o si no tiene servicios.
func clienteDeEmpresa(empresas []int64, empresaFila int64, sinServicios bool, empresa int64) bool {
	return slices.Contains(empresas, empresa) || sinServicios || (len(empresas) == 0 && empresaFila == empresa)
}

// topeConector cuenta la petición en el minuto en curso (fuera de la transacción del caso: el
// conteo queda aunque la acción falle) y responde 429 demasiadas_peticiones al pasar el tope.
func topeConector(ctx context.Context, conectorID int64) error {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	n, err := q.ContarPeticionPortal(ctx, conectorID)
	if err != nil {
		return err
	}
	if n == 1 {
		// Primer pedido del minuto: se limpian los minutos viejos.
		if err := q.PurgarPeticionesPortal(ctx); err != nil {
			return err
		}
	}
	if n > TopePeticionesPortal {
		resto := time.Minute - time.Duration(time.Now().Second())*time.Second
		return nucleo.DemasiadosIntentos("demasiadas_peticiones", "El portal de Argos pasó el tope de peticiones por minuto", resto)
	}
	return nil
}

func nuevoCerrarPortal(m *Modulo) *auditoria.Caso[entradaCerrarPortal, SesionPortal] {
	return auditoria.Nuevo(auditoria.Caso[entradaCerrarPortal, SesionPortal]{
		Accion: "portal_argos.sesion", Acceso: auditoria.Autenticado,
		AuditarExito: soloLecturas[SesionPortal],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCerrarPortal) (SesionPortal, error) {
			p := in.previo
			s := p.SesionPortal
			t.FijarEmpresa(s.EmpresaID)
			t.Anotar(map[string]any{"en_nombre_de": s.EnNombreDe()})
			if v := in.validacion; v != nil {
				switch {
				case v.Validado && !v.Existe:
					return s, nucleo.ReglaNegocio("cliente_no_existe", "Argos no tiene ese cliente")
				case v.Validado && !v.SinServicios && !slices.Contains(v.Empresas, s.EmpresaID):
					return s, empresaNoCoincide()
				case v.Validado:
					s.ClienteValidado = true
				case v.Motivo == MotivoConectorApagado:
					return s, nucleo.Conflicto("argos_apagado", "El conector con Argos está apagado en Iris")
				}
				// Argos no respondió (o rechazó la lectura): sin validar; solo POST …/tickets sigue.
			}
			if p.contactoID == nil {
				return s, nil
			}
			q := argosbd.New(t)
			ct := p.contacto
			if in.contactos != nil || in.errArgos != nil {
				if in.errArgos != nil {
					// Nunca se resuelve con la caché vieja (§11.10).
					t.Anotar(map[string]any{"contactos": "argos_no_respondio"})
					return s, ArgosNoDisponiblePortal()
				}
				leido, err := guardarContactosLeidos(ctx, t, q, s.ClienteID, in.contactos, in.contactosInicio, *p.contactoID)
				if err != nil {
					return s, err
				}
				ct = leido
			}
			if ct == nil || ct.Quitado || ct.ClienteID != s.ClienteID {
				return s, servicioFueraDelContacto()
			}
			if !ct.Reportar && !ct.Consultar {
				return s, permisoDeContacto("El contacto no tiene permiso de reportar ni de consultar sobre este cliente")
			}
			s.Contacto = &ContactoPortal{ID: ct.ContactoID, Nombre: ct.Nombre, Servicios: ct.Servicios, Reportar: ct.Reportar, Consultar: ct.Consultar}
			return s, nil
		},
	})
}

// guardarContactosLeidos guarda en la caché los contactos que devolvió Argos (la misma escritura que
// la atención, guardarContactos: marca quitados los que ya no vienen y nunca baja la versión) y
// devuelve el pedido (nil si no está).
func guardarContactosLeidos(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, cliente int64, contactos []ContactoClienteArgos, inicio time.Time, buscado int64) (*argosbd.IntegracionArgosContacto, error) {
	if err := guardarYAvisar(ctx, t, q, cliente, contactos, inicio); err != nil {
		return nil, err
	}
	if buscado == 0 {
		return nil, nil
	}
	ct, err := q.ContactoEnCache(ctx, buscado)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &ct, err
}

func recortarRunas(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func servicioFueraDelContacto() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "servicio_fuera_del_contacto", "Servicio fuera del contacto",
		"El contacto no es de este cliente o el servicio no está entre los suyos")
}

func permisoDeContacto(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "permiso_de_contacto", "Permiso de contacto", detalle)
}

// ServicioFueraDelContacto: 403 servicio_fuera_del_contacto (§11.10).
func ServicioFueraDelContacto() *nucleo.Problema { return servicioFueraDelContacto() }

// PermisoDeContacto: 403 permiso_de_contacto (§11.10).
func PermisoDeContacto(detalle string) *nucleo.Problema { return permisoDeContacto(detalle) }
