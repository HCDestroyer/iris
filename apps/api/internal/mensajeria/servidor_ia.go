package mensajeria

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/voz"
)

// Servidor de IA («Iris Voz», contratos/voz.md §4): un servidor dedicado con GPU, de la instalación,
// que Iris usa como proveedor del asistente de chat por un canal autenticado. Solo Iris abre
// conexiones hacia él (paquete internal/voz).

// ProveedorServidorIA: el asistente de la empresa usa el servidor de IA de la instalación.
const ProveedorServidorIA = "servidor_ia"

// VozMin: la versión mínima del paquete «Iris Voz» que esta versión de Iris sabe usar.
const VozMin = "0.1.0"

// Estados del servidor de IA.
const (
	MotorSinEnlazar   = "sin_enlazar"
	MotorListo        = "listo"
	MotorDegradado    = "degradado"
	MotorNoDisponible = "no_disponible"
	MotorIncompatible = "incompatible"
	MotorDesactivado  = "desactivado"
)

const (
	// plazoLatidoIA: lo que se espera /salud.
	plazoLatidoIA = 2 * time.Second
	// fallosParaCaido: latidos fallidos seguidos para darlo por caído (30 s).
	fallosParaCaido = 3
	// vidaEnlaceSinActivar: un enlace que nadie activó caduca.
	vidaEnlaceSinActivar = 24 * time.Hour
	// convivenciaRotacion: cuánto valen las credenciales anteriores en una rotación normal.
	convivenciaRotacion = 24 * time.Hour
	// vidaToken: rotación automática.
	vidaToken = 30 * 24 * time.Hour
	// vidaCertCliente y margenCertCliente: el certificado de cliente de Iris.
	vidaCertCliente   = 365 * 24 * time.Hour
	margenCertCliente = 30 * 24 * time.Hour
)

// DatosAsociadosMotor ata cada secreto cifrado a su fila y a su campo (rotación de IRIS_CLAVE_CIFRADO).
func DatosAsociadosMotor(id int64, campo string) []byte {
	return fmt.Appendf(nil, "mensajeria.voz_motor:%d:%s", id, campo)
}

// PaqueteMotor: la versión del paquete instalado y si Iris puede usarlo.
type PaqueteMotor struct {
	Version            *string `json:"version" nullable:"true"`
	Protocolo          *string `json:"protocolo" nullable:"true"`
	IrisMin            *string `json:"irisMin" nullable:"true"`
	IrisMax            *string `json:"irisMax" nullable:"true"`
	Compatible         bool    `json:"compatible"`
	MotivoIncompatible *string `json:"motivoIncompatible" nullable:"true" enum:"protocolo,iris_antiguo,iris_nuevo,paquete_antiguo" doc:"iris_antiguo: hay que actualizar Iris; cualquier otro: hay que actualizar el servidor de IA"`
	VersionDisponible  *string `json:"versionDisponible" nullable:"true" doc:"Siempre null: Iris no consulta versiones nuevas del paquete"`
}

// CapacidadMotor: peticiones al modelo a la vez.
type CapacidadMotor struct {
	MaxSimultaneas      int32 `json:"maxSimultaneas"`
	EnCurso             int64 `json:"enCurso"`
	EmpresasActivas     int   `json:"empresasActivas" doc:"Empresas con el asistente encendido sobre este servidor"`
	PlazasInsuficientes bool  `json:"plazasInsuficientes" doc:"Hay más empresas activas que plazas: la plaza garantizada por empresa no alcanza para todas"`
}

// GPUMotor: la GPU que declara el servidor (null si no la informa).
type GPUMotor struct {
	_      struct{} `nullable:"true"`
	Modelo string   `json:"modelo"`
	VramMb int64    `json:"vramMb"`
}

// ModeloMotor es un modelo que el servidor deja usar.
type ModeloMotor struct {
	ID string `json:"id"`
}

// CatalogoMotor: lo que el servidor declara.
type CatalogoMotor struct {
	LLM []ModeloMotor `json:"llm"`
}

type catalogoGuardado struct {
	LLM []string  `json:"llm"`
	GPU *GPUMotor `json:"gpu,omitempty"`
}

// PasoPrueba es un paso de «Probar».
type PasoPrueba struct {
	Codigo  string `json:"codigo" enum:"conexion,compatibilidad,gpu,stt,tts,llm,medios,capacidad"`
	Estado  string `json:"estado" enum:"ok,error,omitido"`
	Ms      int64  `json:"ms"`
	Detalle string `json:"detalle"`
}

// PruebaMotor es el resultado de «Probar».
type PruebaMotor struct {
	_         struct{}     `nullable:"true"`
	Estado    string       `json:"estado" enum:"ok,error"`
	ProbadoEn time.Time    `json:"probadoEn"`
	Pasos     []PasoPrueba `json:"pasos"`
}

// MotorIA es la tarjeta del servidor de IA. Nunca trae el token, el código ni claves.
type MotorIA struct {
	ID                    int64          `json:"id"`
	Nombre                string         `json:"nombre"`
	URL                   string         `json:"url"`
	Version               int32          `json:"version"`
	Estado                string         `json:"estado" enum:"sin_enlazar,listo,degradado,no_disponible,incompatible,desactivado"`
	Detalle               *string        `json:"detalle" nullable:"true"`
	Enlazado              bool           `json:"enlazado" doc:"El intercambio con código se completó"`
	Activado              bool           `json:"activado" doc:"El administrador comparó la huella y lo activó"`
	EnlaceVenceEn         *time.Time     `json:"enlaceVenceEn" nullable:"true" doc:"Hasta cuándo vale el paso pendiente del enlace: los 15 min del código antes de «Conectar» o las 24 h para «Activar» después; null si no hay nada pendiente"`
	EnlazadoEn            *time.Time     `json:"enlazadoEn" nullable:"true"`
	UltimoLatidoEn        *time.Time     `json:"ultimoLatidoEn" nullable:"true"`
	Paquete               PaqueteMotor   `json:"paquete"`
	Capacidad             CapacidadMotor `json:"capacidad"`
	GPU                   *GPUMotor      `json:"gpu"`
	Catalogo              CatalogoMotor  `json:"catalogo"`
	HuellaTLS             *string        `json:"huellaTls" nullable:"true" doc:"La huella del certificado del servidor que vio Iris: compárala con la que imprimió el instalador antes de activar"`
	TokenRotadoEn         *time.Time     `json:"tokenRotadoEn" nullable:"true"`
	CertClienteVenceEn    *time.Time     `json:"certClienteVenceEn" nullable:"true"`
	IPObservada           *string        `json:"ipObservada" nullable:"true" doc:"La IP de Iris que vio el servidor al enlazar"`
	RestringeIP           bool           `json:"restringeIp" doc:"El servidor solo acepta esa IP (se enciende en el servidor)"`
	EmpresasPermitidas    []int64        `json:"empresasPermitidas" nullable:"true" doc:"null = todas"`
	ModelosPermitidos     []string       `json:"modelosPermitidos" nullable:"true" doc:"De catalogo.llm, los que las empresas pueden elegir; null = todos"`
	SimultaneasPorEmpresa int32          `json:"simultaneasPorEmpresa"`
	UltimaPrueba          *PruebaMotor   `json:"ultimaPrueba"`
}

func catalogoDe(f mensajeriabd.MensajeriaVozMotor) catalogoGuardado {
	var c catalogoGuardado
	_ = json.Unmarshal(f.Catalogo, &c)
	return c
}

func aMotorIA(f mensajeriabd.MensajeriaVozMotor, enCurso int64) MotorIA {
	c := catalogoDe(f)
	m := MotorIA{ID: f.ID, Nombre: f.Nombre, URL: f.Url, Version: f.Version, Estado: f.Estado, Detalle: f.Detalle,
		Enlazado: f.EnlazadoEn != nil, Activado: f.ActivadoEn != nil, EnlaceVenceEn: f.EnlaceVenceEn, EnlazadoEn: f.EnlazadoEn,
		UltimoLatidoEn: f.UltimoLatidoEn,
		Paquete: PaqueteMotor{Version: f.VersionMotor, Protocolo: f.VersionProtocolo, IrisMin: f.IrisMin, IrisMax: f.IrisMax,
			Compatible: f.Compatible, MotivoIncompatible: f.MotivoIncompatible},
		Capacidad: CapacidadMotor{MaxSimultaneas: f.MaxSimultaneas, EnCurso: enCurso}, GPU: c.GPU, Catalogo: CatalogoMotor{LLM: []ModeloMotor{}},
		HuellaTLS: f.HuellaTls, TokenRotadoEn: f.TokenRotadoEn, CertClienteVenceEn: f.CertClienteVenceEn, IPObservada: f.IpObservada,
		RestringeIP: f.RestringeIp, EmpresasPermitidas: f.EmpresasPermitidas, SimultaneasPorEmpresa: f.SimultaneasPorEmpresa,
		ModelosPermitidos: f.ModelosPermitidos}
	if f.ActivadoEn == nil && f.EnlazadoEn != nil {
		v := f.EnlazadoEn.Add(vidaEnlaceSinActivar)
		m.EnlaceVenceEn = &v
	}
	for _, id := range c.LLM {
		m.Catalogo.LLM = append(m.Catalogo.LLM, ModeloMotor{ID: id})
	}
	if f.UltimaPrueba != nil {
		var p PruebaMotor
		if json.Unmarshal(f.UltimaPrueba, &p) == nil {
			m.UltimaPrueba = &p
		}
	}
	return m
}

// soloInstalacion: el servidor de IA es de la instalación. Solo una persona con sesión y
// empresas.todas sin límite de alcance; cualquier otro recibe 404.
func soloInstalacion(ctx context.Context) error {
	actor := contexto.ActorDe(ctx)
	if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 || !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermEmpresasTodas)) {
		return nucleo.NoEncontrado("No existe")
	}
	return nil
}

func reglaMotor(status int, codigo, detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(status, codigo, "Servidor de IA", detalle)
}

// motorNoResponde: una sola respuesta para cualquier falla al hablar con el servidor (no sirve
// para escanear la red ni para distinguir un código equivocado de un puerto cerrado).
func motorNoResponde() *nucleo.Problema {
	return reglaMotor(http.StatusBadGateway, "motor_no_responde",
		"No se pudo hablar con el servidor de IA: revisa la dirección, que el puerto esté abierto para Iris y, al enlazar, que el código esté escrito en el servidor")
}

func (m *Modulo) versionIris() string {
	if v := strings.TrimPrefix(strings.TrimSpace(m.cfg.Version), "v"); voz.VersionValida(v) {
		return v
	}
	return "0.0.0"
}

// validarURLMotor: https://servidor:puerto, con las reglas de URL del asistente (IP privada sí;
// loopback, link-local y metadata no).
func (m *Modulo) validarURLMotor(crudo string) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(crudo), "/")
	mal := func(d string) error {
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "url_no_permitida", "URL no permitida", d,
			nucleo.ErrorCampo{Field: "url", Message: d, Rule: "url_no_permitida"})
	}
	if !strings.HasPrefix(u, "https://") || len(u) > 300 {
		return "", mal("La dirección debe ser https://servidor:puerto")
	}
	if err := validarURLIA(u, m.cfg.PermitirLoopbackIA, true); err != nil {
		return "", mal(detalleIA(err))
	}
	if strings.ContainsAny(strings.TrimPrefix(u, "https://"), "/?#") {
		return "", mal("La dirección no lleva ruta: https://servidor:puerto")
	}
	return u, nil
}

// conexionMotor: las credenciales del canal ya descifradas (solo viven en memoria).
type conexionMotor struct {
	fila           mensajeriabd.MensajeriaVozMotor
	cert           tls.Certificate
	token          []byte
	tokenSiguiente []byte
	// certSiguiente: el certificado de cliente que se está entregando en una rotación (nil = el mismo).
	certSiguiente *tls.Certificate
}

func (m *Modulo) descifrarMotor(id int64, campo string, cifrado []byte) ([]byte, error) {
	if cifrado == nil {
		return nil, nil
	}
	if m.cfg.Cifrador == nil {
		return nil, errors.New("falta IRIS_CLAVE_CIFRADO")
	}
	return m.cfg.Cifrador.Descifrar(cifrado, DatosAsociadosMotor(id, campo))
}

func (m *Modulo) cifrarMotor(id int64, campo string, plano []byte) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO: no se pueden guardar las credenciales del servidor de IA")
	}
	return m.cfg.Cifrador.Cifrar(plano, DatosAsociadosMotor(id, campo))
}

func (m *Modulo) conexionDeMotor(f mensajeriabd.MensajeriaVozMotor) (conexionMotor, error) {
	c := conexionMotor{fila: f}
	if f.CertCliente == nil || f.CertClienteClave == nil {
		return c, errors.New("servidor de IA sin certificado de cliente")
	}
	clave, err := m.descifrarMotor(f.ID, "cert_cliente_clave", f.CertClienteClave)
	if err != nil {
		return c, err
	}
	if c.cert, err = tls.X509KeyPair([]byte(*f.CertCliente), clave); err != nil {
		return c, err
	}
	if c.token, err = m.descifrarMotor(f.ID, "token", f.Token); err != nil {
		return c, err
	}
	if c.tokenSiguiente, err = m.descifrarMotor(f.ID, "token", f.TokenSiguiente); err != nil {
		return c, err
	}
	if f.CertClienteSiguiente != nil && f.CertClienteClaveSiguiente != nil {
		clave, err := m.descifrarMotor(f.ID, "cert_cliente_clave", f.CertClienteClaveSiguiente)
		if err != nil {
			return c, err
		}
		par, err := tls.X509KeyPair([]byte(*f.CertClienteSiguiente), clave)
		if err != nil {
			return c, err
		}
		c.certSiguiente = &par
	}
	return c, nil
}

func (c conexionMotor) huellas() []string {
	var h []string
	if c.fila.HuellaTls != nil {
		h = append(h, *c.fila.HuellaTls)
	}
	if c.fila.HuellaTlsSiguiente != nil {
		h = append(h, *c.fila.HuellaTlsSiguiente)
	}
	return h
}

// clienteMotor arma el cliente firmado con ese token. Un cliente por uso: sin conexiones ni
// credenciales que queden en memoria.
func (m *Modulo) clienteMotor(c conexionMotor, token []byte) (*voz.Cliente, error) {
	return m.clienteCon(c, c.cert, token)
}

func (m *Modulo) clienteCon(c conexionMotor, cert tls.Certificate, token []byte) (*voz.Cliente, error) {
	return voz.NuevoCliente(voz.OpcionesCliente{URL: c.fila.Url, Huellas: c.huellas(), Cert: cert, Token: token,
		VersionIris: m.versionIris(), Control: controlIA(m.cfg.PermitirLoopbackIA), Ahora: m.cfg.Ahora})
}

// motorUtilizable: activado, compatible y contestando.
func motorUtilizable(f mensajeriabd.MensajeriaVozMotor) bool {
	return f.ActivadoEn != nil && f.Compatible && f.Estado == MotorListo
}

func empresaPermitida(f mensajeriabd.MensajeriaVozMotor, empresa int64) bool {
	return f.EmpresasPermitidas == nil || slices.Contains(f.EmpresasPermitidas, empresa)
}

// modelosUtilizables: los que declara el servidor (ya validados) y que el admin de la instalación
// deja elegir a las empresas.
func modelosUtilizables(f mensajeriabd.MensajeriaVozMotor) []string {
	out := []string{}
	for _, id := range catalogoDe(f).LLM {
		if f.ModelosPermitidos == nil || slices.Contains(f.ModelosPermitidos, id) {
			out = append(out, id)
		}
	}
	return out
}

// ServidorIAAsistente: lo que ve una empresa en los ajustes de su asistente (sin URL ni huella).
type ServidorIAAsistente struct {
	Disponible bool     `json:"disponible" doc:"Hay un servidor de IA activo, compatible, contestando y que esta empresa puede usar"`
	Modelos    []string `json:"modelos" doc:"Los modelos que el servidor deja usar"`
}

func (m *Modulo) servidorIAPara(ctx context.Context, q *mensajeriabd.Queries, empresa int64) (ServidorIAAsistente, error) {
	r := ServidorIAAsistente{Modelos: []string{}}
	f, err := q.MotorIA(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if !empresaPermitida(f, empresa) || f.ActivadoEn == nil {
		return r, nil
	}
	r.Disponible = motorUtilizable(f)
	r.Modelos = modelosUtilizables(f)
	return r, nil
}

// --- Casos ---------------------------------------------------------------------------------------

// CasosServidorIA reúne los casos del servidor de IA.
type CasosServidorIA struct {
	Listar        *auditoria.Caso[struct{}, ListaMotoresIA]
	Enlazar       *auditoria.Caso[EntradaEnlazarMotor, EnlaceMotor]
	Conectar      *ConectarMotor
	Activar       *ActivarMotor
	Desactivar    *auditoria.Caso[EntradaDesactivarMotor, MotorIA]
	Probar        *ProbarMotor
	Rotar         *RotarMotor
	Empresas      *auditoria.Caso[EntradaEmpresasMotor, MotorIA]
	Modelos       *auditoria.Caso[EntradaModelosMotor, MotorIA]
	Umbrales      *auditoria.Caso[struct{}, UmbralesIA]
	FijarUmbrales *auditoria.Caso[EntradaUmbralesIA, UmbralesIA]
	Tablero       *auditoria.Caso[EntradaTableroIA, TableroIA]

	// admin: la entrada de activar, rotar y probar. Lleva el permiso sensible: así el caso exige la
	// re-autenticación de una sesión de autologin ANTES de cualquier llamada de red, y sin permiso es 403.
	admin *auditoria.Caso[int64, conexionMotor]
	// leer: lo mismo para el sistema (latido, rotación automática); nunca se llama por una persona.
	leer            *auditoria.Caso[int64, conexionMotor]
	fijarSiguiente  *auditoria.Caso[entradaFijarSiguiente, struct{}]
	conectarFallido *auditoria.Caso[int64, struct{}]
	// descartarSiguiente: el latido borra lo «siguiente» de una entrega que nunca llegó.
	descartarSiguiente *auditoria.Caso[int64, int64]
	latido             *auditoria.Caso[resultadoLatido, mensajeriabd.MensajeriaVozMotor]
	reservar           *auditoria.Caso[entradaPlaza, plazaIA]
	terminar           *auditoria.Caso[usoPeticionIA, struct{}]
	mantener           *auditoria.Caso[struct{}, struct{}]
	motorActual        *auditoria.Caso[struct{}, *mensajeriabd.MensajeriaVozMotor]
}

// ListaMotoresIA es GET /voz/motores.
type ListaMotoresIA struct {
	Data []MotorIA `json:"data"`
}

// EntradaEnlazarMotor es POST /voz/motores/enlazar.
type EntradaEnlazarMotor struct {
	Nombre string `json:"nombre" minLength:"1" maxLength:"80"`
	URL    string `json:"url" minLength:"9" maxLength:"300" doc:"https://servidor:puerto (la que mostró el instalador)"`
}

// EnlaceMotor lleva el código de un solo uso (se muestra una vez).
type EnlaceMotor struct {
	Motor   MotorIA   `json:"motor"`
	Codigo  string    `json:"codigo" doc:"Código de un solo uso (160 bits): se escribe en el servidor con «iris-voz pair». No se vuelve a mostrar"`
	VenceEn time.Time `json:"venceEn"`
}

// EntradaDesactivarMotor es POST /voz/motores/{id}/desactivar.
type EntradaDesactivarMotor struct {
	ID     int64  `json:"-"`
	Motivo string `json:"motivo,omitempty" maxLength:"300"`
}

// EntradaEmpresasMotor es POST /voz/motores/{id}/empresas/actualizar.
type EntradaEmpresasMotor struct {
	ID                    int64   `json:"-"`
	Version               int32   `json:"version" minimum:"1"`
	Empresas              []int64 `json:"empresas" nullable:"true" maxItems:"500" doc:"Las empresas que pueden usarlo; null = todas"`
	SimultaneasPorEmpresa int32   `json:"simultaneasPorEmpresa" minimum:"1" maximum:"64" doc:"Peticiones al modelo a la vez por empresa"`
}

// entradaFijarSiguiente: lo que se guarda antes de entregarlo al servidor. Nada de esto se audita.
type entradaFijarSiguiente struct {
	ID        int64      `json:"id"`
	Version   int32      `json:"version"`
	Token     []byte     `json:"-"`
	Cert      *string    `json:"-"`
	CertClave []byte     `json:"-"`
	CertVence *time.Time `json:"-"`
}

func nuevosCasosServidorIA(m *Modulo) *CasosServidorIA {
	k := &CasosServidorIA{}
	enCurso := func(ctx context.Context, q *mensajeriabd.Queries, id int64) int64 {
		n, _ := q.PlazasEnCursoIA(ctx, mensajeriabd.PlazasEnCursoIAParams{MotorID: id, Ahora: m.ahora()})
		return n
	}
	motorDe := func(ctx context.Context, q *mensajeriabd.Queries, id int64, bloquear bool) (mensajeriabd.MensajeriaVozMotor, error) {
		var f mensajeriabd.MensajeriaVozMotor
		var err error
		if bloquear {
			f, err = q.MotorIAParaActualizar(ctx, id)
		} else {
			f, err = q.MotorIAPorID(ctx, id)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return f, nucleo.NoEncontrado("El servidor de IA no existe")
		}
		return f, err
	}

	k.Listar = auditoria.Nuevo(auditoria.Caso[struct{}, ListaMotoresIA]{
		Accion: "servidor_ia.ver", Permiso: nucleo.PermServidorIAVer, SinEmpresa: true, AuditarExito: sinAuditar[ListaMotoresIA],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ListaMotoresIA, error) {
			r := ListaMotoresIA{Data: []MotorIA{}}
			if err := soloInstalacion(ctx); err != nil {
				return r, err
			}
			q := mensajeriabd.New(t)
			f, err := q.MotorIA(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			mo := aMotorIA(f, enCurso(ctx, q, f.ID))
			activas, err := q.EmpresasActivasIA(ctx)
			if err != nil {
				return r, err
			}
			for _, a := range activas {
				if empresaPermitida(f, a.EmpresaID) {
					mo.Capacidad.EmpresasActivas++
				}
			}
			mo.Capacidad.PlazasInsuficientes = f.ActivadoEn != nil && mo.Capacidad.EmpresasActivas > int(f.MaxSimultaneas) && f.MaxSimultaneas > 0
			r.Data = append(r.Data, mo)
			return r, nil
		},
	})

	k.Enlazar = auditoria.Nuevo(auditoria.Caso[EntradaEnlazarMotor, EnlaceMotor]{
		Accion: "servidor_ia.enlazar", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEnlazarMotor) (EnlaceMotor, error) {
			if err := soloInstalacion(ctx); err != nil {
				return EnlaceMotor{}, err
			}
			t.FijarEmpresa(0)
			url, err := m.validarURLMotor(in.URL)
			if err != nil {
				return EnlaceMotor{}, err
			}
			nombre := strings.TrimSpace(in.Nombre)
			if nombre == "" {
				return EnlaceMotor{}, campoConv("nombre", "Escribe un nombre", "required")
			}
			q := mensajeriabd.New(t)
			f, err := q.MotorIAUnicoParaActualizar(ctx)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if f, err = q.InsertarMotorIA(ctx, mensajeriabd.InsertarMotorIAParams{Nombre: nombre, Url: url}); err != nil {
					return EnlaceMotor{}, err
				}
			case err != nil:
				return EnlaceMotor{}, err
			case f.ActivadoEn != nil && f.Estado != MotorDesactivado:
				// Enlazar de nuevo invalida el canal: primero hay que desactivarlo a propósito.
				return EnlaceMotor{}, reglaMotor(http.StatusConflict, "motor_ya_enlazado", "Ya hay un servidor de IA activo: desactívalo antes de enlazar otra vez")
			}
			codigo, err := voz.NuevoCodigo()
			if err != nil {
				return EnlaceMotor{}, err
			}
			cert, clave, err := voz.GenerarCertificado("iris", vidaCertCliente)
			if err != nil {
				return EnlaceMotor{}, err
			}
			codigoCifrado, err := m.cifrarMotor(f.ID, "enlace_codigo", []byte(codigo))
			if err != nil {
				return EnlaceMotor{}, err
			}
			claveCifrada, err := m.cifrarMotor(f.ID, "cert_cliente_clave", clave)
			if err != nil {
				return EnlaceMotor{}, err
			}
			ahora := m.ahora()
			vence, venceCert := ahora.Add(voz.VidaCodigo), ahora.Add(vidaCertCliente)
			certPEM := string(cert)
			f, err = q.PrepararEnlaceMotorIA(ctx, mensajeriabd.PrepararEnlaceMotorIAParams{ID: f.ID, Nombre: nombre, Url: url,
				EnlaceCodigo: codigoCifrado, EnlaceVenceEn: &vence, CertCliente: &certPEM, CertClienteClave: claveCifrada, CertClienteVenceEn: &venceCert})
			if err != nil {
				return EnlaceMotor{}, err
			}
			t.FijarEntidad("voz_motor", f.ID)
			// Nunca el código: ni en la auditoría ni en ningún evento.
			t.Anotar(map[string]any{"nombre": nombre, "url": url, "vence_en": vence})
			return EnlaceMotor{Motor: aMotorIA(f, 0), Codigo: codigo, VenceEn: vence}, nil
		},
	})

	conexion := func(ctx context.Context, t *auditoria.Tx, id int64) (conexionMotor, error) {
		f, err := motorDe(ctx, mensajeriabd.New(t), id, false)
		if err != nil {
			return conexionMotor{}, err
		}
		c, err := m.conexionDeMotor(f)
		if err != nil {
			// Sin credenciales legibles no hay canal: se trata como un servidor que no responde.
			return conexionMotor{fila: f}, nil
		}
		return c, nil
	}
	k.admin = auditoria.Nuevo(auditoria.Caso[int64, conexionMotor]{
		Accion: "servidor_ia.administrar", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true, AuditarExito: sinAuditar[conexionMotor],
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (conexionMotor, error) {
			if err := soloInstalacion(ctx); err != nil {
				return conexionMotor{}, err
			}
			return conexion(ctx, t, id)
		},
	})
	k.leer = auditoria.Nuevo(auditoria.Caso[int64, conexionMotor]{
		Accion: "servidor_ia.leer", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[conexionMotor],
		Logica: conexion,
	})
	k.fijarSiguiente = auditoria.Nuevo(auditoria.Caso[entradaFijarSiguiente, struct{}]{
		Accion: "servidor_ia.rotar", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[struct{}],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFijarSiguiente) (struct{}, error) {
			cifrado, err := m.cifrarMotor(in.ID, "token", in.Token)
			if err != nil {
				return struct{}{}, err
			}
			ahora := m.ahora()
			_, err = mensajeriabd.New(t).FijarSiguienteMotorIA(ctx, mensajeriabd.FijarSiguienteMotorIAParams{ID: in.ID, Version: in.Version,
				Ahora: ahora, Vencida: ahora.Add(-plazoRotacionIA),
				TokenSiguiente: cifrado, CertClienteSiguiente: in.Cert, CertClienteClaveSiguiente: in.CertClave, CertClienteVenceSiguiente: in.CertVence})
			if errors.Is(err, pgx.ErrNoRows) {
				// Otra rotación (o un cambio de la tarjeta) llegó antes: esta no pisa nada.
				return struct{}{}, rotacionEnCurso()
			}
			return struct{}{}, err
		},
	})
	k.descartarSiguiente = auditoria.Nuevo(auditoria.Caso[int64, int64]{
		Accion: "servidor_ia.rotacion_descartada", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(n int64) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (int64, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("voz_motor", id)
			return mensajeriabd.New(t).DescartarSiguienteMotorIA(ctx, mensajeriabd.DescartarSiguienteMotorIAParams{ID: id, Vencida: m.ahora().Add(-plazoRotacionIA)})
		},
	})
	k.conectarFallido = auditoria.Nuevo(auditoria.Caso[int64, struct{}]{
		Accion: "servidor_ia.conectar_fallido", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(_ context.Context, t *auditoria.Tx, id int64) (struct{}, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("voz_motor", id)
			// Sin el código ni el motivo (no se distingue: voz.md §4.8).
			t.Anotar(map[string]any{"estado": "error"})
			return struct{}{}, nil
		},
	})

	k.Desactivar = auditoria.Nuevo(auditoria.Caso[EntradaDesactivarMotor, MotorIA]{
		Accion: "servidor_ia.desactivar", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDesactivarMotor) (MotorIA, error) {
			if err := soloInstalacion(ctx); err != nil {
				return MotorIA{}, err
			}
			t.FijarEmpresa(0)
			q := mensajeriabd.New(t)
			f, err := motorDe(ctx, q, in.ID, true)
			if err != nil {
				return MotorIA{}, err
			}
			t.FijarEntidad("voz_motor", f.ID)
			if f.Estado == MotorDesactivado {
				return aMotorIA(f, 0), nil
			}
			de := f.Estado
			var detalle *string
			if mot := strings.TrimSpace(in.Motivo); mot != "" {
				detalle = &mot
			}
			if f, err = q.DesactivarMotorIA(ctx, mensajeriabd.DesactivarMotorIAParams{ID: f.ID, Detalle: detalle}); err != nil {
				return MotorIA{}, err
			}
			if err := t.EmitirEnEmpresa("voz.motor_desactivado", nil, map[string]any{"motor_id": f.ID, "usuario_id": contexto.ActorDe(ctx).ID, "motivo": detalle}); err != nil {
				return MotorIA{}, err
			}
			return aMotorIA(f, 0), emitirEstadoMotor(t, f, de)
		},
	})

	k.Empresas = auditoria.Nuevo(auditoria.Caso[EntradaEmpresasMotor, MotorIA]{
		Accion: "servidor_ia.empresas", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEmpresasMotor) (MotorIA, error) {
			if err := soloInstalacion(ctx); err != nil {
				return MotorIA{}, err
			}
			t.FijarEmpresa(0)
			q := mensajeriabd.New(t)
			f, err := motorDe(ctx, q, in.ID, true)
			if err != nil {
				return MotorIA{}, err
			}
			if f.Version != in.Version {
				return MotorIA{}, nucleo.ConflictoVersion("voz_motor", f.ID)
			}
			if in.SimultaneasPorEmpresa < 1 || in.SimultaneasPorEmpresa > 64 {
				return MotorIA{}, campoConv("simultaneasPorEmpresa", "Entre 1 y 64", "rango")
			}
			var empresas []int64
			if in.Empresas != nil {
				empresas = slices.Clone(in.Empresas)
				slices.Sort(empresas)
				empresas = slices.Compact(empresas)
				if empresas == nil {
					empresas = []int64{}
				}
				if slices.ContainsFunc(empresas, func(e int64) bool { return e < 0 }) {
					return MotorIA{}, campoConv("empresas", "Ids de empresa", "rango")
				}
			}
			antes := map[string]any{"empresas": f.EmpresasPermitidas, "simultaneas_por_empresa": f.SimultaneasPorEmpresa}
			if f, err = q.ActualizarEmpresasMotorIA(ctx, mensajeriabd.ActualizarEmpresasMotorIAParams{ID: f.ID, EmpresasPermitidas: empresas,
				SimultaneasPorEmpresa: in.SimultaneasPorEmpresa}); err != nil {
				return MotorIA{}, err
			}
			t.FijarEntidad("voz_motor", f.ID)
			t.RegistrarCambios("voz_motor", f.ID, antes, map[string]any{"empresas": f.EmpresasPermitidas, "simultaneas_por_empresa": f.SimultaneasPorEmpresa})
			return aMotorIA(f, enCurso(ctx, q, f.ID)), t.EmitirEnEmpresa("voz.motor_empresas_cambiadas", nil, map[string]any{"motor_id": f.ID,
				"usuario_id": contexto.ActorDe(ctx).ID, "empresas": f.EmpresasPermitidas, "simultaneas_por_empresa": f.SimultaneasPorEmpresa})
		},
	})

	nuevosCasosTablero(m, k)
	k.Conectar = nuevoConectarMotor(m, k)
	k.Activar = nuevoActivarMotor(m, k)
	k.Probar = nuevoProbarMotor(m, k)
	k.Rotar = nuevoRotarMotor(m, k)
	nuevosCasosLatido(m, k)
	nuevosCasosPlazas(m, k)
	return k
}

// emitirEstadoMotor avisa de un cambio de estado (salud o compatibilidad).
func emitirEstadoMotor(t *auditoria.Tx, f mensajeriabd.MensajeriaVozMotor, de string) error {
	if de == f.Estado {
		return nil
	}
	return t.EmitirEnEmpresa("voz.motor_estado_cambiado", nil, map[string]any{"motor_id": f.ID, "de": de, "a": f.Estado, "en_curso": 0,
		"version": f.VersionMotor, "compatible": f.Compatible, "llamadas_cortadas": 0})
}
