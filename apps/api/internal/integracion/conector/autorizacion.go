package conector

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// «Conectar con un botón» (contrato Iris ↔ ArgosISP 0.4.0 §1.4): alternativa al enlace de §1.3.
//
//  1. Iniciar (POST /integraciones/<sistema>/autorizaciones): el administrador escribe el panel del
//     otro sistema; Iris genera un state (32 bytes, un uso, 10 min, ligado a la sesión del panel y
//     al usuario) y un code_verifier (32 bytes, cifrado y atado a la fila, nunca sale del
//     servidor), y devuelve la URL de autorizar del panel con el code_challenge S256.
//  2. El panel del otro sistema aprueba o rechaza y vuelve a
//     <iris>/administracion/integraciones/argos/vuelta#codigo=…&api=…&state=… (o #error=…&state=…).
//  3. Completar (POST /integraciones/<sistema>/autorizaciones/completar): Iris verifica el state y
//     lo gasta, exige que api esté en Sistema.Origenes y canjea servidor a servidor con el
//     code_verifier, con las mismas defensas que el enlace (canjearYGuardar). El conector queda
//     apagado hasta «Activar».

// VigenciaAutorizacion del state y el code_verifier.
const VigenciaAutorizacion = 10 * time.Minute

// stateValido: 32 bytes en base64url sin relleno.
var stateValido = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// errorRemotoValido: el código de error de la vuelta (rechazado, …), sin texto libre.
var errorRemotoValido = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// DatosAsociadosAutorizacion ata el code_verifier cifrado a su fila (también lo usa la rotación de
// la clave, internal/nucleo/clave).
func DatosAsociadosAutorizacion(id int64) []byte {
	return fmt.Appendf(nil, "autorizacion:%d:code_verifier", id)
}

// DesafioS256 es el code_challenge de un code_verifier: base64url(SHA-256(verifier)), sin relleno.
func DesafioS256(verificador string) string {
	h := sha256.Sum256([]byte(verificador))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func problemaAutorizacion(codigo, titulo, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, codigo, titulo, detalle)
}

func stateInvalido(detalle string) *nucleo.Problema {
	return problemaAutorizacion("state_invalido", "Autorización inválida", detalle+". Vuelve a pulsar «Conectar con Argos».")
}

// Tipos de la API --------------------------------------------------------------------------

// EntradaAutorizar es la entrada de integraciones.autorizar.
type EntradaAutorizar struct {
	PanelURL string `json:"panelUrl" minLength:"8" maxLength:"500" doc:"Dirección del panel del otro sistema (https://host[:puerto]); se normaliza al origen"`
}

// AutorizacionIniciada es la URL de autorizar del panel del otro sistema.
type AutorizacionIniciada struct {
	URL      string    `json:"url" doc:"https://<panel>/integraciones/iris/autorizar?iris=…&state=…&code_challenge=…&code_challenge_method=S256: el panel de Iris la abre en la misma pestaña"`
	PanelURL string    `json:"panelUrl" doc:"Origen normalizado del panel"`
	ExpiraEn time.Time `json:"expiraEn" doc:"Vence el state (10 min)"`
}

// EntradaCompletarAutorizacion es lo que el panel de Iris leyó del fragmento de la vuelta.
type EntradaCompletarAutorizacion struct {
	State  string `json:"state" minLength:"1" maxLength:"200" auditoria:"sensible" doc:"state de la vuelta"`
	Codigo string `json:"codigo,omitempty" maxLength:"200" auditoria:"sensible" doc:"Código de conexión (si el administrador aprobó)"`
	API    string `json:"api,omitempty" maxLength:"500" doc:"Origen de la API del otro sistema (si aprobó); debe estar en la lista de orígenes"`
	Error  string `json:"error,omitempty" maxLength:"100" doc:"Error de la vuelta (rechazado) en vez de codigo y api"`
}

// EmpresaAutorizada es una empresa que verá Iris, tal como la informó el canje.
type EmpresaAutorizada struct {
	ID     int64   `json:"id"`
	Nombre *string `json:"nombre" nullable:"true"`
	NIT    *string `json:"nit" nullable:"true"`
}

// AutorizacionCompletada es el resumen que se muestra antes de «Activar».
type AutorizacionCompletada struct {
	Conector Conector            `json:"conector" doc:"El conector nuevo, apagado"`
	Empresas []EmpresaAutorizada `json:"empresas" doc:"Empresas del alcance del usuario técnico, según el canje"`
}

// Iniciar --------------------------------------------------------------------------------

func nuevoAutorizar(c *Conexion) *auditoria.Caso[EntradaAutorizar, AutorizacionIniciada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAutorizar, AutorizacionIniciada]{
		Accion: "integraciones.autorizar", Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAutorizar) (AutorizacionIniciada, error) {
			t.FijarEmpresa(0)
			if err := c.exigirConfiguracion(); err != nil {
				return AutorizacionIniciada{}, err
			}
			sesion := contexto.SesionDe(ctx)
			actor := t.Actor()
			if sesion.ID == 0 || !actor.EsUsuario() || sesion.UsuarioID != actor.ID {
				return AutorizacionIniciada{}, nucleo.ReglaNegocio("sesion_requerida",
					"«Conectar con "+c.s.Remoto.Nombre+"» se usa desde el panel de Iris con tu sesión (no con un token de API)")
			}
			panel, err := c.panelAutorizar(in.PanelURL)
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			// Lo que exigirá completar, comprobado antes de mandar al administrador al otro sistema.
			if c.n.tokensFijados() == nil {
				return AutorizacionIniciada{}, errSinTokens
			}
			if !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas)) {
				return AutorizacionIniciada{}, nucleo.Prohibido("Conectar con " + c.s.Remoto.Nombre + " exige operar en todas las empresas (empresas.todas sin límite de alcance)")
			}
			if len(c.s.Origenes) == 0 {
				return AutorizacionIniciada{}, c.sinOrigenes()
			}
			state, err := NuevoSecreto()
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			verificador, err := NuevoSecreto()
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			q := conectorbd.New(t)
			// Solo vale la última de la sesión: las anteriores sin usar quedan gastadas y sin verifier.
			anuladas, err := q.AnularAutorizacionesDeSesion(ctx, conectorbd.AnularAutorizacionesDeSesionParams{SesionID: sesion.ID, Sistema: c.s.Nombre})
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			if len(anuladas) > 0 {
				t.Anotar(map[string]any{"autorizaciones_anuladas": anuladas})
			}
			id, err := q.SiguienteIDAutorizacion(ctx)
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			if c.n.cfg.Cifrador == nil {
				return AutorizacionIniciada{}, SinCifrado()
			}
			cifrado, err := c.n.cfg.Cifrador.Cifrar([]byte(verificador), DatosAsociadosAutorizacion(id))
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			f, err := q.InsertarAutorizacion(ctx, conectorbd.InsertarAutorizacionParams{
				ID: id, Sistema: c.s.Nombre, StateHash: Hash256(state), VerificadorCifrado: cifrado, PanelUrl: panel,
				UsuarioID: actor.ID, SesionID: sesion.ID, ExpiraEn: time.Now().Add(VigenciaAutorizacion),
			})
			if err != nil {
				return AutorizacionIniciada{}, err
			}
			t.FijarEntidad("autorizacion", f.ID)
			t.Anotar(map[string]any{"sistema": c.s.Nombre, "panel_url": panel})
			v := url.Values{}
			v.Set("iris", c.n.cfg.BaseURL)
			v.Set("state", state)
			v.Set("code_challenge", DesafioS256(verificador))
			v.Set("code_challenge_method", "S256")
			return AutorizacionIniciada{URL: panel + c.s.RutaAutorizar + "?" + v.Encode(), PanelURL: panel, ExpiraEn: f.ExpiraEn}, nil
		},
	})
}

// panelAutorizar valida y normaliza la dirección del panel a esquema://host[:puerto] (https, o
// http con PermitirHTTP). Iris no llama al panel (solo redirige el navegador), así que no hay
// riesgo de SSRF; sí se rechazan credenciales, consulta y fragmento.
func (c *Conexion) panelAutorizar(crudo string) (string, error) {
	u, err := c.n.validarOrigen(crudo, "panelUrl")
	if err != nil {
		return "", err
	}
	origen, err := nucleo.NormalizarOrigen(u.String())
	if err != nil {
		return "", nucleo.Validacion(nucleo.ErrorCampo{Field: "panelUrl", Message: "Debe ser https://host[:puerto]", Rule: "url"})
	}
	return origen, nil
}

func (c *Conexion) sinOrigenes() *nucleo.Problema {
	return nucleo.ReglaNegocio("origenes_no_configurados", "Falta "+c.s.Remoto.VariableOrigenes+": configura el origen de la API de "+
		c.s.Remoto.Nombre+" (https://host[:puerto]) antes de conectar; Iris solo entrega su token a orígenes conocidos")
}

// Completar ------------------------------------------------------------------------------

// autorizacionTomada: lo que se lee al gastar el state (solo dentro del proceso).
type autorizacionTomada struct {
	ID          int64
	PanelURL    string
	verificador string
}

// entradaReservarAutorizacion: la vuelta ya con el state gastado.
type entradaReservarAutorizacion struct {
	AutorizacionID int64  `json:"autorizacion_id"`
	API            string `json:"api,omitempty"`
	Error          string `json:"error,omitempty"`
	codigo         string
}

// reservaAutorizacion: el id del conector y el origen de la API validado.
type reservaAutorizacion struct {
	ConectorID int64
	Base       string
}

// CompletarAutorizacion completa la vuelta del otro sistema: gasta el state (un caso que se
// confirma aunque lo demás falle: el state es de un solo uso), valida la vuelta y reserva el id
// del conector, canjea fuera de transacción y guarda el conector apagado.
type CompletarAutorizacion struct {
	c        *Conexion
	tomar    *auditoria.Caso[EntradaCompletarAutorizacion, autorizacionTomada]
	reservar *auditoria.Caso[entradaReservarAutorizacion, reservaAutorizacion]
	guardar  *auditoria.Caso[entradaGuardarConector, Conector]
}

const accionCompletar = "integraciones.completar_autorizacion"

func nuevoCompletarAutorizacion(c *Conexion) *CompletarAutorizacion {
	k := &CompletarAutorizacion{c: c}
	k.tomar = auditoria.Nuevo(auditoria.Caso[EntradaCompletarAutorizacion, autorizacionTomada]{
		Accion: accionCompletar, Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		// El éxito lo audita reservar (error) o guardar (conector creado); los rechazos del state sí.
		AuditarExito: soloLecturas[autorizacionTomada],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCompletarAutorizacion) (autorizacionTomada, error) {
			t.FijarEmpresa(0)
			if !stateValido.MatchString(in.State) {
				return autorizacionTomada{}, stateInvalido("El state de la vuelta no tiene el formato esperado")
			}
			q := conectorbd.New(t)
			f, err := q.AutorizacionParaUsar(ctx, conectorbd.AutorizacionParaUsarParams{StateHash: Hash256(in.State), Sistema: c.s.Nombre})
			if esSinFilas(err) {
				return autorizacionTomada{}, stateInvalido("Iris no inició esta autorización")
			}
			if err != nil {
				return autorizacionTomada{}, err
			}
			t.FijarEntidad("autorizacion", f.ID)
			sesion := contexto.SesionDe(ctx)
			switch {
			case f.UsadoEn != nil:
				return autorizacionTomada{}, stateInvalido("Esta autorización ya se usó o la reemplazó otra más nueva")
			case sesion.ID == 0 || sesion.ID != f.SesionID || t.Actor().ID != f.UsuarioID:
				// Sin gastarla: quien la inició puede terminarla desde su sesión.
				return autorizacionTomada{}, stateInvalido("La autorización se inició desde otra sesión")
			case !time.Now().Before(f.ExpiraEn):
				return autorizacionTomada{}, problemaAutorizacion("state_vencido", "Autorización vencida",
					"Pasaron más de 10 minutos desde que pulsaste «Conectar con "+c.s.Remoto.Nombre+"». Vuelve a pulsarlo.")
			}
			if f.VerificadorCifrado == nil || c.n.cfg.Cifrador == nil {
				return autorizacionTomada{}, SinCifrado()
			}
			verificador, err := c.n.cfg.Cifrador.Descifrar(f.VerificadorCifrado, DatosAsociadosAutorizacion(f.ID))
			if err != nil {
				return autorizacionTomada{}, fmt.Errorf("autorización %d: descifrar code_verifier: %w", f.ID, err)
			}
			if err := q.UsarAutorizacion(ctx, f.ID); err != nil {
				return autorizacionTomada{}, err
			}
			return autorizacionTomada{ID: f.ID, PanelURL: f.PanelUrl, verificador: string(verificador)}, nil
		},
	})
	k.reservar = auditoria.Nuevo(auditoria.Caso[entradaReservarAutorizacion, reservaAutorizacion]{
		Accion: accionCompletar, Permiso: nucleo.PermIntegracionesConectar, SinEmpresa: true,
		AuditarExito: soloLecturas[reservaAutorizacion],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaReservarAutorizacion) (reservaAutorizacion, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("autorizacion", in.AutorizacionID)
			if in.Error != "" {
				detalle := "El administrador rechazó la conexión en " + c.s.Remoto.Nombre + "."
				if in.Error != "rechazado" && errorRemotoValido.MatchString(in.Error) {
					detalle = c.s.Remoto.Nombre + " no autorizó la conexión (" + in.Error + ")."
				}
				return reservaAutorizacion{}, problemaAutorizacion("rechazado", "Conexión rechazada", detalle+" Puedes volver a intentarlo.")
			}
			if err := c.exigirConfiguracion(); err != nil {
				return reservaAutorizacion{}, err
			}
			if !codigoValido.MatchString(in.codigo) {
				return reservaAutorizacion{}, problemaAutorizacion("codigo_invalido", "Código inválido",
					"La vuelta de "+c.s.Remoto.Nombre+" no trae un código de conexión válido. Vuelve a pulsar «Conectar con "+c.s.Remoto.Nombre+"».")
			}
			// El api es un origen (https://host[:puerto]): con una ruta, el canje iría a otro lugar
			// del mismo origen que el que se comparó con la lista.
			if u, err := url.Parse(strings.TrimSpace(in.API)); err == nil && u.Path != "" && u.Path != "/" {
				return reservaAutorizacion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "api", Message: "Debe ser un origen https://host[:puerto], sin ruta", Rule: "origen"})
			}
			base, err := c.validarBase(in.API, "api")
			if err != nil {
				return reservaAutorizacion{}, err
			}
			// Solo la lista: el api lo escribe el otro sistema en la URL de vuelta, así que no vale
			// el origen de un conector existente (a diferencia del enlace de §1.3).
			origen, err := nucleo.NormalizarOrigen(base)
			if err != nil {
				return reservaAutorizacion{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "api", Message: "Origen inválido", Rule: "url"})
			}
			if len(c.s.Origenes) == 0 {
				return reservaAutorizacion{}, c.sinOrigenes()
			}
			if !slices.Contains(c.s.Origenes, origen) {
				return reservaAutorizacion{}, nucleo.ReglaNegocio("origen_no_permitido", "La API de "+c.s.Remoto.Nombre+" que trae la vuelta ("+origen+
					") no está en "+c.s.Remoto.VariableOrigenes+": si de verdad es "+c.s.Remoto.Nombre+", agrégala a la lista y vuelve a conectar")
			}
			if c.n.tokensFijados() == nil {
				return reservaAutorizacion{}, errSinTokens
			}
			if !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas)) {
				return reservaAutorizacion{}, nucleo.Prohibido("Conectar con " + c.s.Remoto.Nombre + " exige operar en todas las empresas (empresas.todas sin límite de alcance)")
			}
			id, err := conectorbd.New(t).SiguienteIDConector(ctx)
			// La base del canje es exactamente el origen que coincidió con la lista.
			return reservaAutorizacion{ConectorID: id, Base: origen}, err
		},
	})
	k.guardar = nuevoGuardarConector(c, accionCompletar)
	return k
}

// Ejecutar gasta el state, valida la vuelta, canjea con el code_verifier y guarda el conector
// apagado. Devuelve el resumen para «Activar».
func (k *CompletarAutorizacion) Ejecutar(ctx context.Context, in EntradaCompletarAutorizacion) (AutorizacionCompletada, error) {
	in.State = strings.TrimSpace(in.State)
	tomada, err := k.tomar.Ejecutar(ctx, in)
	if err != nil {
		return AutorizacionCompletada{}, err
	}
	r, err := k.reservar.Ejecutar(ctx, entradaReservarAutorizacion{
		AutorizacionID: tomada.ID, API: strings.TrimSpace(in.API), Error: strings.TrimSpace(in.Error), codigo: strings.TrimSpace(in.Codigo),
	})
	if err != nil {
		return AutorizacionCompletada{}, err
	}
	g := entradaGuardarConector{
		Base: r.Base, ConectorID: r.ConectorID, Origen: "autorizacion", PanelSugerido: tomada.PanelURL, autorizacionID: tomada.ID,
	}
	var respuesta RespuestaCanje
	con, err := k.c.canjearYGuardar(ctx, k.guardar, g, strings.TrimSpace(in.Codigo), tomada.verificador, k.c.errorCanjeAutorizacion, &respuesta)
	if err != nil {
		return AutorizacionCompletada{}, err
	}
	empresas := []EmpresaAutorizada{}
	for _, e := range respuesta.EmpresasCanje() {
		r := EmpresaAutorizada{ID: e.ID}
		if e.Nombre != "" {
			r.Nombre = &e.Nombre
		}
		if e.NIT != "" {
			r.NIT = &e.NIT
		}
		empresas = append(empresas, r)
	}
	return AutorizacionCompletada{Conector: con, Empresas: empresas}, nil
}

// errorCanjeAutorizacion: el otro sistema rechaza el código o el code_verifier (401
// codigo_invalido, o un 4xx con ese tipo) → 422 codigo_invalido. Lo demás, como el enlace.
func (c *Conexion) errorCanjeAutorizacion(err error) error {
	var er *ErrorRemoto
	if !errors.As(err, &er) || er.Bloqueado() {
		return Problema(err)
	}
	tipo := er.Tipo
	if i := strings.LastIndexAny(tipo, ":/"); i >= 0 {
		tipo = tipo[i+1:]
	}
	if er.Status == http.StatusUnauthorized || (er.Status >= 400 && er.Status < 500 && tipo == "codigo_invalido") {
		return problemaAutorizacion("codigo_invalido", "Código inválido",
			c.s.Remoto.Nombre+" no aceptó el código de conexión (venció, ya se usó o no corresponde a esta autorización). Vuelve a pulsar «Conectar con "+c.s.Remoto.Nombre+"».")
	}
	return Problema(err)
}
