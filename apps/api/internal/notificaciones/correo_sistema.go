package notificaciones

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Correo del sistema configurable desde el panel (contratos/correo-sistema.md). Un ajuste global
// (empresa null) y, opcional, uno por empresa. Precedencia en cada envío: el de la empresa → el
// global → IRIS_SMTP_* del entorno → ninguno. Un ajuste apagado se salta.

// Ámbito global en la ruta.
const AmbitoGlobal = "global"

// Origen del correo que sale hoy.
const (
	OrigenEmpresa = "empresa"
	OrigenGlobal  = "global"
	OrigenEntorno = "entorno"
	OrigenNinguno = "ninguno"
)

// Causas de una prueba fallida (lista cerrada: nunca la respuesta cruda del servidor).
const (
	CausaNoConecta             = "no_conecta"
	CausaDestinoNoPermitido    = "destino_no_permitido"
	CausaCertificado           = "certificado"
	CausaSeguridad             = "seguridad"
	CausaCredenciales          = "credenciales"
	CausaRemitenteRechazado    = "remitente_rechazado"
	CausaDestinatarioRechazado = "destinatario_rechazado"
	CausaTiempoAgotado         = "tiempo_agotado"
	CausaOtro                  = "otro"
)

// Topes de «probar» (por hora): la prueba no es un escáner ni un relay.
const (
	TopePruebasUsuario     = 5
	TopePruebasInstalacion = 20
	ventanaPruebas         = time.Hour
	// plazoPrueba: tiempo total de una prueba (conexión, TLS, AUTH y envío).
	plazoPrueba = 20 * time.Second
)

// PuertosCorreo son los puertos SMTP permitidos.
var PuertosCorreo = []int{25, 465, 587, 2525}

var hostDNS = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

// DatosAsociadosCorreo ata la contraseña cifrada a su fila (rotación de IRIS_CLAVE_CIFRADO).
func DatosAsociadosCorreo(id int64) []byte {
	return fmt.Appendf(nil, "notificaciones.correo_ajustes:%d:contrasena", id)
}

// EntornoCorreo: lo que se sabe de IRIS_SMTP_* (nunca el usuario ni la contraseña).
type EntornoCorreo struct {
	Configurado     bool   `json:"configurado"`
	Host            string `json:"host"`
	RemitenteCorreo string `json:"remitenteCorreo"`
}

// PruebaCorreo es la última prueba de un ámbito.
type PruebaCorreo struct {
	Estado string    `json:"estado" enum:"ok,error"`
	En     time.Time `json:"en"`
	Causa  *string   `json:"causa" nullable:"true" enum:"no_conecta,destino_no_permitido,certificado,seguridad,credenciales,remitente_rechazado,destinatario_rechazado,tiempo_agotado,otro"`
}

// CorreoSistema es el ajuste de un ámbito y por dónde sale el correo hoy.
type CorreoSistema struct {
	Ambito          string        `json:"ambito" doc:"global o el id de la empresa"`
	EmpresaID       *int64        `json:"empresaId" nullable:"true"`
	Configurado     bool          `json:"configurado" doc:"Hay un ajuste guardado en este ámbito"`
	Host            string        `json:"host"`
	Puerto          int32         `json:"puerto"`
	Seguridad       string        `json:"seguridad" enum:"starttls,tls,ninguna"`
	Usuario         string        `json:"usuario"`
	TieneContrasena bool          `json:"tieneContrasena" doc:"Lo único que se sabe de la contraseña: nunca se devuelve"`
	RemitenteNombre string        `json:"remitenteNombre"`
	RemitenteCorreo string        `json:"remitenteCorreo"`
	Activo          bool          `json:"activo"`
	Version         int32         `json:"version" doc:"0 si no hay ajuste guardado"`
	ActualizadoEn   *time.Time    `json:"actualizadoEn" nullable:"true"`
	Origen          string        `json:"origen" enum:"empresa,global,entorno,ninguno" doc:"Por dónde saldría hoy un correo de este ámbito"`
	OrigenDetalle   string        `json:"origenDetalle" doc:"El origen en una frase, lista para mostrar"`
	EnUso           bool          `json:"enUso" doc:"El ajuste guardado de este ámbito es el que se usa hoy"`
	Entorno         EntornoCorreo `json:"entorno" doc:"IRIS_SMTP_* del servidor (respaldo)"`
	UltimaPrueba    *PruebaCorreo `json:"ultimaPrueba,omitempty" doc:"La última prueba de este ámbito; ausente si nunca se probó"`
}

// CamposCorreo son los campos del formulario (guardar y probar).
type CamposCorreo struct {
	Host            string  `json:"host" minLength:"1" maxLength:"253" doc:"Nombre DNS o IP, sin esquema ni puerto"`
	Puerto          int32   `json:"puerto" doc:"25, 465, 587 o 2525"`
	Seguridad       string  `json:"seguridad" enum:"starttls,tls,ninguna" doc:"ninguna: solo un relay de la red interna permitida y sin usuario ni contraseña"`
	Usuario         string  `json:"usuario,omitempty" maxLength:"254" doc:"Vacío = sin autenticación"`
	Contrasena      *string `json:"contrasena,omitempty" maxLength:"512" auditoria:"sensible" doc:"Solo escritura. Ausente = se conserva la guardada; \"\" = se borra"`
	RemitenteNombre string  `json:"remitenteNombre,omitempty" maxLength:"80"`
	RemitenteCorreo string  `json:"remitenteCorreo" minLength:"3" maxLength:"254"`
}

// CuerpoGuardarCorreo es POST /correo-sistema/{ambito}/guardar.
type CuerpoGuardarCorreo struct {
	Version int32 `json:"version" minimum:"0" doc:"La que leíste; 0 al crear"`
	CamposCorreo
	Activo bool `json:"activo"`
}

// EntradaGuardarCorreo: el cuerpo más el ámbito de la ruta.
type EntradaGuardarCorreo struct {
	Ambito string `json:"ambito"`
	CuerpoGuardarCorreo
}

// CuerpoProbarCorreo es POST /correo-sistema/{ambito}/probar.
type CuerpoProbarCorreo struct {
	CamposCorreo
	Para string `json:"para" minLength:"3" maxLength:"254" doc:"Una sola dirección: a quién se manda el correo de prueba"`
}

// EntradaProbarCorreo: el cuerpo más el ámbito de la ruta.
type EntradaProbarCorreo struct {
	Ambito string `json:"ambito"`
	CuerpoProbarCorreo
}

// ResultadoPruebaCorreo es la respuesta de «probar» (siempre 200).
type ResultadoPruebaCorreo struct {
	Estado  string  `json:"estado" enum:"ok,error"`
	Causa   *string `json:"causa" nullable:"true" enum:"no_conecta,destino_no_permitido,certificado,seguridad,credenciales,remitente_rechazado,destinatario_rechazado,tiempo_agotado,otro"`
	Detalle string  `json:"detalle" doc:"Texto llano en español, listo para mostrar"`
}

// EntradaActivarCorreo es POST /correo-sistema/{ambito}/activar y /desactivar.
type EntradaActivarCorreo struct {
	Ambito  string `json:"ambito"`
	Version int32  `json:"version"`
}

// EntradaVerCorreo es GET /correo-sistema/{ambito}.
type EntradaVerCorreo struct {
	Ambito string `json:"ambito"`
}

// descriptorCorreo lo implementa el emisor del entorno para decir su host y remitente.
type descriptorCorreo interface {
	Describir() (host, remitente string)
}

// Describir: el host y el remitente (para «hoy se usa el del servidor»).
func (s *SMTPSistema) Describir() (string, string) { return s.Servidor.Host, s.Remitente.Address }

func (m *Modulo) puertosCorreo() []int {
	if len(m.cfg.PuertosCorreo) > 0 {
		return m.cfg.PuertosCorreo
	}
	return PuertosCorreo
}

func campoCorreo(campo, mensaje, regla string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: mensaje, Rule: regla})
}

func reglaCorreo(codigo, detalle string, errores ...nucleo.ErrorCampo) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, codigo, "No se puede", detalle, errores...)
}

// conControl: CR, LF u otro carácter de control (o separadores de línea Unicode): con ellos se
// podría inyectar una cabecera o un comando SMTP.
func conControl(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool {
		return unicode.IsControl(r) || r == ' ' || r == ' ' || r == '\u0085'
	})
}

// unaDireccion valida una sola dirección de correo «pelada» (sin nombre, sin lista).
func unaDireccion(s string) bool {
	if conControl(s) || strings.ContainsAny(s, " <>,;\"()") || len(s) > 254 {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}

// ambitoCorreo resuelve el ámbito de la ruta y revisa el alcance: el global exige operar sin
// límite de empresa (empresas.todas, o una instalación sin empresas); el de una empresa exige
// estar operando en ella. Devuelve 0 para el global.
func ambitoCorreo(ctx context.Context, ambito string) (int64, error) {
	if a := contexto.ActorDe(ctx); a.Tipo != contexto.ActorUsuario || a.ID == 0 {
		return 0, nucleo.NoEncontrado("El correo del sistema no existe")
	}
	e := contexto.EmpresasDe(ctx)
	if ambito == AmbitoGlobal {
		if e.Todas || e.Estado == contexto.EmpresaSinConector {
			return 0, nil
		}
		return 0, nucleo.Prohibido("El correo global de la instalación lo configura quien opera en todas las empresas")
	}
	id, err := strconv.ParseInt(ambito, 10, 64)
	if err != nil || id <= 0 {
		return 0, nucleo.NoEncontrado("El correo del sistema no existe")
	}
	if e.Estado != contexto.EmpresaActiva || e.Activa != id {
		return 0, nucleo.NoEncontrado("El correo del sistema de esa empresa no existe o no estás operando en ella")
	}
	return id, nil
}

func empresaPtr(e int64) *int64 {
	if e == 0 {
		return nil
	}
	return &e
}

// validarCampos normaliza y valida el formulario. conContrasena: habrá contraseña (la nueva o la
// guardada) al usar estos valores.
func (m *Modulo) validarCampos(ctx context.Context, c *CamposCorreo, conContrasena bool) error {
	c.Host = strings.ToLower(strings.TrimSpace(c.Host))
	c.Usuario = strings.TrimSpace(c.Usuario)
	c.RemitenteNombre = strings.TrimSpace(c.RemitenteNombre)
	c.RemitenteCorreo = strings.TrimSpace(c.RemitenteCorreo)
	if !slices.Contains(m.puertosCorreo(), int(c.Puerto)) {
		return campoCorreo("puerto", "Solo 25, 465, 587 o 2525", "enum")
	}
	switch c.Seguridad {
	case correo.TLSStartTLS, correo.TLSImplicito, correo.TLSNinguna:
	default:
		return campoCorreo("seguridad", "starttls, tls o ninguna", "enum")
	}
	if conControl(c.Usuario) || utf8.RuneCountInString(c.Usuario) > 254 {
		return campoCorreo("usuario", "Hasta 254 caracteres, sin saltos de línea", "formato")
	}
	if conControl(c.RemitenteNombre) || utf8.RuneCountInString(c.RemitenteNombre) > 80 {
		return campoCorreo("remitenteNombre", "Hasta 80 caracteres, sin saltos de línea", "formato")
	}
	if !unaDireccion(c.RemitenteCorreo) {
		return campoCorreo("remitenteCorreo", "Escribe una sola dirección de correo, p. ej. avisos@tuempresa.com", "correo")
	}
	if c.Contrasena != nil && utf8.RuneCountInString(*c.Contrasena) > 512 {
		return campoCorreo("contrasena", "Hasta 512 caracteres", "longitud")
	}
	if c.Seguridad == correo.TLSNinguna {
		if c.Usuario != "" {
			return reglaCorreo("sin_cifrado_sin_credenciales", "Sin cifrado no se mandan usuario ni contraseña: elige STARTTLS o TLS, o deja el usuario vacío",
				nucleo.ErrorCampo{Field: "usuario", Message: "Sin cifrado, el usuario va vacío", Rule: "sin_cifrado_sin_credenciales"})
		}
		if conContrasena {
			return reglaCorreo("sin_cifrado_sin_credenciales", "Sin cifrado no se mandan usuario ni contraseña: elige STARTTLS o TLS, o quita la contraseña",
				nucleo.ErrorCampo{Field: "contrasena", Message: "Sin cifrado no se guarda contraseña", Rule: "sin_cifrado_sin_credenciales"})
		}
	} else {
		if conContrasena && c.Usuario == "" {
			return campoCorreo("usuario", "Con contraseña hace falta el usuario", "requerido")
		}
		if !conContrasena && c.Usuario != "" {
			return reglaCorreo("contrasena_requerida", "Con usuario hace falta la contraseña",
				nucleo.ErrorCampo{Field: "contrasena", Message: "Escribe la contraseña", Rule: "contrasena_requerida"})
		}
	}
	return m.validarHost(ctx, c.Host, c.Seguridad)
}

// validarHost: un nombre DNS o una IP, y la guarda de red. Una IP literal se revisa aquí; un nombre
// se resuelve (plazo corto): si alguna de sus IPs no está permitida, 422. La guarda definitiva es la
// del conector (mira la IP a la que de verdad se conecta, en la prueba y en cada envío).
func (m *Modulo) validarHost(ctx context.Context, host, seguridad string) error {
	noPermitido := func() error {
		return reglaCorreo(CausaDestinoNoPermitido, detalleCausa[CausaDestinoNoPermitido],
			nucleo.ErrorCampo{Field: "host", Message: "Ese servidor no se puede usar desde Iris", Rule: CausaDestinoNoPermitido})
	}
	soloInterno := func() error {
		return reglaCorreo("sin_cifrado_solo_red_interna", "Sin cifrado solo se puede usar un servidor de la red interna permitida (IRIS_CORREO_REDES_PERMITIDAS)",
			nucleo.ErrorCampo{Field: "seguridad", Message: "Elige STARTTLS o TLS para este servidor", Rule: "sin_cifrado_solo_red_interna"})
	}
	redes := m.cfg.RedCorreo.RedesPermitidas
	dentro := func(ip netip.Addr) bool {
		ip = ip.Unmap()
		return slices.ContainsFunc(redes, func(p netip.Prefix) bool { return p.Contains(ip) })
	}
	if host == "" || len(host) > 253 || conControl(host) {
		return campoCorreo("host", "Escribe el servidor, p. ej. smtp.tuproveedor.com", "formato")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.Zone() != "" || !argos.DestinoPermitido(ip, redes) {
			return noPermitido()
		}
		if seguridad == correo.TLSNinguna && !dentro(ip) {
			return soloInterno()
		}
		return nil
	}
	if !hostDNS.MatchString(host) {
		return campoCorreo("host", "Solo el nombre del servidor o su IP, sin http://, ruta ni puerto", "formato")
	}
	ctxDNS, cancelar := context.WithTimeout(ctx, 3*time.Second)
	defer cancelar()
	ips, err := net.DefaultResolver.LookupNetIP(ctxDNS, "ip", host)
	if err != nil || len(ips) == 0 {
		// Sin DNS ahora mismo: se deja guardar («probar» dirá que no conecta), salvo sin cifrado, que
		// exige saber que el servidor está en la red interna.
		if seguridad == correo.TLSNinguna {
			return soloInterno()
		}
		return nil
	}
	for _, ip := range ips {
		if !argos.DestinoPermitido(ip, redes) {
			return noPermitido()
		}
		if seguridad == correo.TLSNinguna && !dentro(ip) {
			return soloInterno()
		}
	}
	return nil
}

func contrasenaRequerida() *nucleo.Problema {
	return reglaCorreo("contrasena_requerida", "Cambiaste el servidor, el puerto o el usuario: la contraseña guardada no se manda a otro servidor. Vuelve a escribirla",
		nucleo.ErrorCampo{Field: "contrasena", Message: "Vuelve a escribir la contraseña", Rule: "contrasena_requerida"})
}

// mismoDestino: la contraseña guardada solo vale para el mismo servidor, puerto y usuario.
func mismoDestino(f notificacionesbd.NotificacionesCorreoAjuste, c CamposCorreo) bool {
	return f.Host == c.Host && f.Puerto == c.Puerto && f.Usuario == c.Usuario
}

func (m *Modulo) entornoCorreo() EntornoCorreo {
	if m.cfg.Correo == nil {
		return EntornoCorreo{}
	}
	e := EntornoCorreo{Configurado: true}
	if d, ok := m.cfg.Correo.(descriptorCorreo); ok {
		e.Host, e.RemitenteCorreo = d.Describir()
	}
	return e
}

// origenCorreo: por dónde saldría hoy un correo de esa empresa (0 = sin empresa).
func (m *Modulo) origenCorreo(ctx context.Context, q *notificacionesbd.Queries, empresa int64) (string, *notificacionesbd.NotificacionesCorreoAjuste, error) {
	vigentes, err := q.CorreoAjustesVigentes(ctx, empresa)
	if err != nil {
		return "", nil, err
	}
	if len(vigentes) > 0 {
		if vigentes[0].EmpresaID != nil {
			return OrigenEmpresa, &vigentes[0], nil
		}
		return OrigenGlobal, &vigentes[0], nil
	}
	if m.cfg.Correo != nil {
		return OrigenEntorno, nil, nil
	}
	return OrigenNinguno, nil, nil
}

// emisorVigente arma el emisor del correo que aplica a esa empresa (nil = no hay correo del
// sistema). Se llama en cada envío: un cambio en el panel vale para el correo siguiente.
func (m *Modulo) emisorVigente(ctx context.Context, q *notificacionesbd.Queries, empresa *int64) (EmisorCorreo, error) {
	var e int64
	if empresa != nil {
		e = *empresa
	}
	origen, f, err := m.origenCorreo(ctx, q, e)
	if err != nil {
		return nil, err
	}
	switch origen {
	case OrigenNinguno:
		return nil, nil
	case OrigenEntorno:
		return m.cfg.Correo, nil
	}
	clave, err := m.contrasenaGuardada(*f)
	if err != nil {
		return emisorRoto{dominio: dominioDe(f.RemitenteCorreo), err: err}, nil
	}
	return m.emisorDe(aCampos(*f), clave), nil
}

// emisorRoto: hay un ajuste vigente pero no se puede usar (la contraseña no se pudo leer).
type emisorRoto struct {
	dominio string
	err     error
}

func (e emisorRoto) Enviar(context.Context, mail.Address, string, string, string) error { return e.err }
func (e emisorRoto) Dominio() string                                                    { return e.dominio }

func dominioDe(direccion string) string {
	if _, d, ok := strings.Cut(direccion, "@"); ok && d != "" {
		return d
	}
	return "iris"
}

func (m *Modulo) contrasenaGuardada(f notificacionesbd.NotificacionesCorreoAjuste) (string, error) {
	if f.ContrasenaCifrada == nil {
		return "", nil
	}
	if m.cfg.Cifrador == nil {
		return "", errors.New("falta IRIS_CLAVE_CIFRADO para leer la contraseña del correo del sistema")
	}
	plano, err := m.cfg.Cifrador.Descifrar(f.ContrasenaCifrada, DatosAsociadosCorreo(f.ID))
	if err != nil {
		return "", errors.New("no se pudo leer la contraseña guardada del correo del sistema; vuelve a escribirla")
	}
	return string(plano), nil
}

func aCampos(f notificacionesbd.NotificacionesCorreoAjuste) CamposCorreo {
	return CamposCorreo{Host: f.Host, Puerto: f.Puerto, Seguridad: f.Seguridad, Usuario: f.Usuario,
		RemitenteNombre: f.RemitenteNombre, RemitenteCorreo: f.RemitenteCorreo}
}

func (m *Modulo) emisorDe(c CamposCorreo, contrasena string) *SMTPSistema {
	return &SMTPSistema{Red: m.cfg.RedCorreo, Remitente: mail.Address{Name: c.RemitenteNombre, Address: c.RemitenteCorreo},
		Servidor: correo.Servidor{Host: c.Host, Puerto: int(c.Puerto), TLS: c.Seguridad, Usuario: c.Usuario, Contrasena: contrasena}}
}

var detalleOrigen = map[string]string{
	OrigenEmpresa: "Los correos de esta empresa salen por su propio ajuste.",
	OrigenGlobal:  "Los correos salen por el ajuste global de la instalación.",
	OrigenEntorno: "Los correos salen por la configuración del servidor (IRIS_SMTP_* del .env).",
	OrigenNinguno: "No hay correo configurado: los avisos solo salen en la campana del panel.",
}

func (m *Modulo) vistaCorreo(ctx context.Context, q *notificacionesbd.Queries, ambito string, empresa int64, f *notificacionesbd.NotificacionesCorreoAjuste) (CorreoSistema, error) {
	r := CorreoSistema{Ambito: ambito, EmpresaID: empresaPtr(empresa), Puerto: 587, Seguridad: correo.TLSStartTLS, Activo: true, Entorno: m.entornoCorreo()}
	if f != nil {
		r.Configurado, r.Host, r.Puerto, r.Seguridad, r.Usuario = true, f.Host, f.Puerto, f.Seguridad, f.Usuario
		r.TieneContrasena, r.RemitenteNombre, r.RemitenteCorreo = f.ContrasenaCifrada != nil, f.RemitenteNombre, f.RemitenteCorreo
		r.Activo, r.Version, r.ActualizadoEn = f.Activo, f.Version, &f.UpdatedAt
	}
	origen, vigente, err := m.origenCorreo(ctx, q, empresa)
	if err != nil {
		return r, err
	}
	r.Origen, r.OrigenDetalle = origen, detalleOrigen[origen]
	r.EnUso = f != nil && vigente != nil && vigente.ID == f.ID
	u, err := q.UltimaPruebaCorreo(ctx, empresa)
	if err == nil {
		r.UltimaPrueba = &PruebaCorreo{Estado: u.Estado, En: u.CreadoEn, Causa: u.Causa}
	} else if !esSinFilas(err) {
		return r, err
	}
	return r, nil
}

func (m *Modulo) nuevoVerCorreoSistema() *auditoria.Caso[EntradaVerCorreo, CorreoSistema] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerCorreo, CorreoSistema]{
		Accion: "correo_sistema.ver", Permiso: nucleo.PermCorreoSistemaVer, SinEmpresa: true,
		AuditarExito: func(CorreoSistema) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCorreo) (CorreoSistema, error) {
			empresa, err := ambitoCorreo(ctx, in.Ambito)
			if err != nil {
				return CorreoSistema{}, err
			}
			q := notificacionesbd.New(t)
			f, err := q.CorreoAjustesDeAmbito(ctx, empresa)
			if esSinFilas(err) {
				return m.vistaCorreo(ctx, q, in.Ambito, empresa, nil)
			}
			if err != nil {
				return CorreoSistema{}, err
			}
			return m.vistaCorreo(ctx, q, in.Ambito, empresa, &f)
		},
	})
}

// auditableCorreo: lo que queda en auditoria.cambios (la contraseña como ***).
func auditableCorreo(f notificacionesbd.NotificacionesCorreoAjuste, contrasena any) map[string]any {
	return map[string]any{"host": f.Host, "puerto": f.Puerto, "seguridad": f.Seguridad, "usuario": f.Usuario, "contrasena": contrasena,
		"remitente_nombre": f.RemitenteNombre, "remitente_correo": f.RemitenteCorreo, "activo": f.Activo}
}

func marcaContrasena(f notificacionesbd.NotificacionesCorreoAjuste) any {
	if f.ContrasenaCifrada == nil {
		return nil
	}
	return "guardada"
}

func (m *Modulo) nuevoGuardarCorreoSistema() *auditoria.Caso[EntradaGuardarCorreo, CorreoSistema] {
	return auditoria.Nuevo(auditoria.Caso[EntradaGuardarCorreo, CorreoSistema]{
		Accion: "correo_sistema.guardar", Permiso: nucleo.PermCorreoSistemaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaGuardarCorreo) (CorreoSistema, error) {
			empresa, err := ambitoCorreo(ctx, in.Ambito)
			if err != nil {
				return CorreoSistema{}, err
			}
			t.FijarEmpresa(empresa)
			q := notificacionesbd.New(t)
			antes, err := q.CorreoAjustesParaActualizar(ctx, empresa)
			nuevo := esSinFilas(err)
			if err != nil && !nuevo {
				return CorreoSistema{}, err
			}
			if (nuevo && in.Version != 0) || (!nuevo && antes.Version != in.Version) {
				return CorreoSistema{}, nucleo.ConflictoVersion("correo_sistema", in.Ambito)
			}
			c := in.CamposCorreo
			// Sin usuario no hay autenticación: la contraseña guardada se borra sola.
			if c.Contrasena == nil && strings.TrimSpace(c.Usuario) == "" {
				c.Contrasena = ptr("")
			}
			conContrasena := !nuevo && antes.ContrasenaCifrada != nil
			if c.Contrasena != nil {
				conContrasena = *c.Contrasena != ""
			}
			if err := m.validarCampos(ctx, &c, conContrasena); err != nil {
				return CorreoSistema{}, err
			}
			// La contraseña guardada nunca viaja a otro servidor ni con otro usuario.
			if !nuevo && antes.ContrasenaCifrada != nil && c.Contrasena == nil && !mismoDestino(antes, c) {
				return CorreoSistema{}, contrasenaRequerida()
			}
			if c.Contrasena != nil && *c.Contrasena != "" && m.cfg.Cifrador == nil {
				return CorreoSistema{}, nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO: no se puede guardar la contraseña")
			}
			var f notificacionesbd.NotificacionesCorreoAjuste
			if nuevo {
				f, err = q.InsertarCorreoAjustes(ctx, notificacionesbd.InsertarCorreoAjustesParams{EmpresaID: empresaPtr(empresa), Host: c.Host, Puerto: c.Puerto,
					Seguridad: c.Seguridad, Usuario: c.Usuario, RemitenteNombre: c.RemitenteNombre, RemitenteCorreo: c.RemitenteCorreo, Activo: in.Activo})
				if esSinFilas(err) {
					return CorreoSistema{}, nucleo.ConflictoVersion("correo_sistema", in.Ambito)
				}
			} else {
				// Primero se quita la contraseña si el modo nuevo no la admite (restricción de la tabla).
				if c.Contrasena != nil && *c.Contrasena == "" && antes.ContrasenaCifrada != nil {
					if _, err = q.FijarContrasenaCorreoAjustes(ctx, notificacionesbd.FijarContrasenaCorreoAjustesParams{ID: antes.ID}); err != nil {
						return CorreoSistema{}, err
					}
				}
				f, err = q.ActualizarCorreoAjustes(ctx, notificacionesbd.ActualizarCorreoAjustesParams{ID: antes.ID, Host: c.Host, Puerto: c.Puerto,
					Seguridad: c.Seguridad, Usuario: c.Usuario, RemitenteNombre: c.RemitenteNombre, RemitenteCorreo: c.RemitenteCorreo, Activo: in.Activo})
			}
			if err != nil {
				return CorreoSistema{}, err
			}
			marca := marcaContrasena(f)
			if c.Contrasena != nil && *c.Contrasena != "" {
				cifrada, err := m.cfg.Cifrador.Cifrar([]byte(*c.Contrasena), DatosAsociadosCorreo(f.ID))
				if err != nil {
					return CorreoSistema{}, err
				}
				if f, err = q.FijarContrasenaCorreoAjustes(ctx, notificacionesbd.FijarContrasenaCorreoAjustesParams{ID: f.ID, ContrasenaCifrada: cifrada}); err != nil {
					return CorreoSistema{}, err
				}
				marca = "nueva"
			}
			t.FijarEntidad("correo_sistema", f.ID)
			var previo any
			if !nuevo {
				previo = auditableCorreo(antes, marcaContrasena(antes))
			}
			campos := t.RegistrarCambios("correo_sistema", f.ID, previo, auditableCorreo(f, marca))
			if campos == nil {
				campos = []string{}
			}
			if err := t.Emitir("correo_sistema.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos, "activo": f.Activo}); err != nil {
				return CorreoSistema{}, err
			}
			return m.vistaCorreo(ctx, q, in.Ambito, empresa, &f)
		},
	})
}

func (m *Modulo) nuevoActivarCorreoSistema(activar bool) *auditoria.Caso[EntradaActivarCorreo, CorreoSistema] {
	accion := "correo_sistema.desactivar"
	if activar {
		accion = "correo_sistema.activar"
	}
	return auditoria.Nuevo(auditoria.Caso[EntradaActivarCorreo, CorreoSistema]{
		Accion: accion, Permiso: nucleo.PermCorreoSistemaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActivarCorreo) (CorreoSistema, error) {
			empresa, err := ambitoCorreo(ctx, in.Ambito)
			if err != nil {
				return CorreoSistema{}, err
			}
			t.FijarEmpresa(empresa)
			q := notificacionesbd.New(t)
			antes, err := q.CorreoAjustesParaActualizar(ctx, empresa)
			if esSinFilas(err) {
				return CorreoSistema{}, nucleo.NoEncontrado("Todavía no hay un correo guardado en este ámbito")
			}
			if err != nil {
				return CorreoSistema{}, err
			}
			t.FijarEntidad("correo_sistema", antes.ID)
			if antes.Version != in.Version {
				return CorreoSistema{}, nucleo.ConflictoVersion("correo_sistema", in.Ambito)
			}
			if antes.Activo == activar {
				return m.vistaCorreo(ctx, q, in.Ambito, empresa, &antes)
			}
			f, err := q.ActivarCorreoAjustes(ctx, notificacionesbd.ActivarCorreoAjustesParams{ID: antes.ID, Activo: activar})
			if err != nil {
				return CorreoSistema{}, err
			}
			campos := t.RegistrarCambios("correo_sistema", f.ID, map[string]any{"activo": antes.Activo}, map[string]any{"activo": f.Activo})
			if err := t.Emitir("correo_sistema.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos, "activo": f.Activo}); err != nil {
				return CorreoSistema{}, err
			}
			return m.vistaCorreo(ctx, q, in.Ambito, empresa, &f)
		},
	})
}

// --- Probar antes de guardar ---

// pruebaLista es la prueba ya validada y contada, lista para mandar fuera de la transacción.
type pruebaLista struct {
	ID      int64
	Empresa int64
	emisor  *SMTPSistema
	para    mail.Address
}

type cierrePrueba struct {
	ID      int64   `json:"id"`
	Empresa int64   `json:"-"`
	Estado  string  `json:"estado"`
	Causa   *string `json:"causa"`
}

func (m *Modulo) nuevoPrepararPruebaCorreo() *auditoria.Caso[EntradaProbarCorreo, *pruebaLista] {
	return auditoria.Nuevo(auditoria.Caso[EntradaProbarCorreo, *pruebaLista]{
		Accion: "correo_sistema.probar", Permiso: nucleo.PermCorreoSistemaConfigurar, SinEmpresa: true,
		// La fila de auditoría de una prueba la deja su cierre (con el resultado).
		AuditarExito: func(*pruebaLista) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarCorreo) (*pruebaLista, error) {
			empresa, err := ambitoCorreo(ctx, in.Ambito)
			if err != nil {
				return nil, err
			}
			t.FijarEmpresa(empresa)
			usuario := contexto.ActorDe(ctx).ID
			q := notificacionesbd.New(t)
			c := in.CamposCorreo
			in.Para = strings.TrimSpace(in.Para)
			if !unaDireccion(in.Para) {
				return nil, campoCorreo("para", "Escribe una sola dirección de correo", "correo")
			}
			// Sin contraseña en el formulario se usa la guardada de este ámbito, solo hacia el mismo
			// servidor, puerto y usuario: la prueba no sirve para mandarla a otro lado.
			contrasena := ""
			guardada, err := q.CorreoAjustesDeAmbito(ctx, empresa)
			hayGuardada := err == nil && guardada.ContrasenaCifrada != nil
			if err != nil && !esSinFilas(err) {
				return nil, err
			}
			conContrasena := c.Contrasena != nil && *c.Contrasena != ""
			usarGuardada := c.Contrasena == nil && hayGuardada && strings.TrimSpace(c.Usuario) != "" && c.Seguridad != correo.TLSNinguna
			if err := m.validarCampos(ctx, &c, conContrasena || usarGuardada); err != nil {
				return nil, err
			}
			switch {
			case conContrasena:
				contrasena = *c.Contrasena
			case usarGuardada:
				if !mismoDestino(guardada, c) {
					return nil, contrasenaRequerida()
				}
				if contrasena, err = m.contrasenaGuardada(guardada); err != nil {
					return nil, reglaCorreo("contrasena_requerida", "No se pudo leer la contraseña guardada: vuelve a escribirla",
						nucleo.ErrorCampo{Field: "contrasena", Message: "Vuelve a escribir la contraseña", Rule: "contrasena_requerida"})
				}
			}
			// Tope por usuario y por instalación en la última hora (cuenta también las que están en curso).
			if err := q.BloquearPruebasCorreo(ctx); err != nil {
				return nil, err
			}
			ahora := m.ahora()
			recientes, err := q.PruebasCorreoDesde(ctx, ahora.Add(-ventanaPruebas))
			if err != nil {
				return nil, err
			}
			var mias []time.Time
			for _, p := range recientes {
				if p.UsuarioID == usuario {
					mias = append(mias, p.CreadoEn)
				}
			}
			if len(mias) >= TopePruebasUsuario {
				return nil, nucleo.DemasiadosIntentos("demasiadas_pruebas", "Ya hiciste 5 pruebas en la última hora. Espera un poco antes de probar otra vez",
					mias[len(mias)-TopePruebasUsuario].Add(ventanaPruebas).Sub(ahora))
			}
			if len(recientes) >= TopePruebasInstalacion {
				return nil, nucleo.DemasiadosIntentos("demasiadas_pruebas", "Se hicieron muchas pruebas de correo en la última hora. Espera un poco antes de probar otra vez",
					recientes[len(recientes)-TopePruebasInstalacion].CreadoEn.Add(ventanaPruebas).Sub(ahora))
			}
			p, err := q.InsertarPruebaCorreo(ctx, notificacionesbd.InsertarPruebaCorreoParams{EmpresaID: empresaPtr(empresa), UsuarioID: usuario, Ahora: ahora})
			if err != nil {
				return nil, err
			}
			return &pruebaLista{ID: p.ID, Empresa: empresa, emisor: m.emisorDe(c, contrasena), para: mail.Address{Address: in.Para}}, nil
		},
	})
}

func (m *Modulo) nuevoCerrarPruebaCorreo() *auditoria.Caso[cierrePrueba, bool] {
	return auditoria.Nuevo(auditoria.Caso[cierrePrueba, bool]{
		Accion: "correo_sistema.probar", Permiso: nucleo.PermCorreoSistemaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in cierrePrueba) (bool, error) {
			usuario := contexto.ActorDe(ctx).ID
			t.FijarEmpresa(in.Empresa)
			t.FijarEntidad("correo_prueba", in.ID)
			if _, err := notificacionesbd.New(t).CerrarPruebaCorreo(ctx, notificacionesbd.CerrarPruebaCorreoParams{ID: in.ID, UsuarioID: usuario,
				Estado: in.Estado, Causa: in.Causa}); err != nil {
				if esSinFilas(err) {
					return false, nil
				}
				return false, err
			}
			return true, t.Emitir("correo_sistema.probado", map[string]any{"empresa_id": empresaPtr(in.Empresa), "estado": in.Estado,
				"causa": in.Causa, "usuario_id": usuario})
		},
	})
}

// Asunto y cuerpo fijos del correo de prueba: quien prueba no elige el contenido.
const (
	asuntoPrueba = "Correo de prueba de Iris"
	textoPrueba  = "Este es un correo de prueba de Iris.\n\nSi lo recibes, el correo del sistema está bien configurado.\n\n" +
		"Lo pidió una persona desde Configuración › Correo del sistema. Si no lo esperabas, puedes ignorarlo.\n"
)

// ProbarCorreoSistema manda el correo de prueba con los valores del formulario, sin guardar. La
// validación, el tope y el registro van en una transacción; el SMTP, fuera; el resultado, en otra.
func (m *Modulo) ProbarCorreoSistema(ctx context.Context, in EntradaProbarCorreo) (ResultadoPruebaCorreo, error) {
	p, err := m.prepararPruebaCorreo.Ejecutar(ctx, in)
	if err != nil {
		return ResultadoPruebaCorreo{}, err
	}
	ctxEnvio, cancelar := context.WithTimeout(ctx, plazoPrueba)
	errEnvio := p.emisor.Enviar(ctxEnvio, p.para, asuntoPrueba, textoPrueba,
		fmt.Sprintf("iris.prueba.%d.%s@%s", p.ID, uuid.NewString()[:8], p.emisor.Dominio()))
	cancelar()
	r := ResultadoPruebaCorreo{Estado: "ok", Detalle: "El correo de prueba salió. Revisa la bandeja de " + p.para.Address + "."}
	if errEnvio != nil {
		causa := CausaDeCorreo(errEnvio)
		r = ResultadoPruebaCorreo{Estado: "error", Causa: &causa, Detalle: detalleCausa[causa]}
	}
	if _, err := m.cerrarPruebaCorreo.Ejecutar(context.WithoutCancel(ctx), cierrePrueba{ID: p.ID, Empresa: p.Empresa, Estado: r.Estado, Causa: r.Causa}); err != nil {
		return ResultadoPruebaCorreo{}, err
	}
	return r, nil
}

var detalleCausa = map[string]string{
	CausaNoConecta:             "No se pudo conectar: revisa el servidor y el puerto.",
	CausaDestinoNoPermitido:    "Ese servidor no se puede usar desde Iris (red interna o no permitida).",
	CausaCertificado:           "El certificado del servidor no es válido para ese nombre.",
	CausaSeguridad:             "El servidor no acepta la seguridad elegida (prueba STARTTLS o TLS).",
	CausaCredenciales:          "El usuario o la contraseña no son correctos.",
	CausaRemitenteRechazado:    "El servidor no acepta ese remitente.",
	CausaDestinatarioRechazado: "El servidor no acepta esa dirección de destino.",
	CausaTiempoAgotado:         "El servidor no respondió a tiempo. Si usas el puerto 465 elige TLS; con 587 o 25, STARTTLS.",
	CausaOtro:                  "El servidor rechazó el envío.",
}

// CausaDeCorreo traduce un error de SMTP a una causa de la lista cerrada. Todo fallo al abrir la
// conexión (rechazo, filtrado o DNS) es no_conecta: la prueba no distingue un puerto cerrado de
// uno filtrado.
func CausaDeCorreo(err error) string {
	var op *net.OpError
	dial := errors.As(err, &op) && op.Op == "dial"
	e := correo.Resumir(err)
	switch e.Codigo {
	case "destino_no_permitido":
		return CausaDestinoNoPermitido
	case "dns":
		return CausaNoConecta
	case "conexion":
		if dial {
			return CausaNoConecta
		}
		return CausaTiempoAgotado
	case "tls":
		// Un servidor que no habla TLS en ese puerto no es un problema de certificado.
		var rec tls.RecordHeaderError
		if errors.As(err, &rec) {
			return CausaSeguridad
		}
		return CausaCertificado
	case "sin_tls", "claro_no_permitido":
		return CausaSeguridad
	case "autenticacion":
		return CausaCredenciales
	case "protocolo":
		switch e.Paso {
		case "remitente":
			return CausaRemitenteRechazado
		case "destinatario":
			return CausaDestinatarioRechazado
		}
	}
	if dial {
		return CausaNoConecta
	}
	return CausaOtro
}
