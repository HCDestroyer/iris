package mensajeria

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/sippuente"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// El teléfono de Iris, etapa E4c (contratos/voz.md §3.12, «E4c: el teléfono dentro de Iris»): la
// administración (WSS de la Issabel, límites, grupos, credencial SIP de cada extensión y las pruebas).
// El puente está en telefono_puente.go y las llamadas en telefono_llamar.go.

// PuertoWSS es el WSS de Asterisk (http.conf). Otro solo si la instalación lo permite.
const PuertoWSS = 8089

// TelefonoCentralita es el teléfono de Iris de la empresa (dentro de Centralita). Nunca trae secretos.
type TelefonoCentralita struct {
	Activo                bool       `json:"activo"`
	WssPuerto             int32      `json:"wssPuerto"`
	WssRuta               string     `json:"wssRuta"`
	HuellaWss             *string    `json:"huellaWss" nullable:"true" doc:"SHA-256 del certificado del WSS que confirmó un administrador"`
	SipDominio            *string    `json:"sipDominio" nullable:"true" doc:"Dominio SIP de From/To (null = la dirección de la centralita)"`
	PrefijoSalida         string     `json:"prefijoSalida" doc:"Lo que se antepone a los 8 dígitos para salir por la troncal"`
	LlamadasPorUsuarioDia int32      `json:"llamadasPorUsuarioDia"`
	LlamadasSimultaneas   int32      `json:"llamadasSimultaneas"`
	ProbadoEn             *time.Time `json:"probadoEn" nullable:"true" doc:"La última prueba del WSS que pasó con los datos actuales (encender la exige)"`
	// RestriccionesConfirmadasEn: el administrador confirmó la configuración de la Issabel del runbook §9.3
	// (contexto que solo sale a Guatemala, sin transferencias por DTMF ni desvíos). Encender la exige.
	RestriccionesConfirmadasEn *time.Time `json:"restriccionesConfirmadasEn" nullable:"true" doc:"Cuándo el administrador confirmó las restricciones de la Issabel (runbook §9.3); encender la exige"`
}

func telefonoCentralitaDe(f mensajeriabd.MensajeriaCentralita) TelefonoCentralita {
	return TelefonoCentralita{Activo: f.TelefonoActivo, WssPuerto: f.WssPuerto, WssRuta: f.WssRuta, HuellaWss: f.WssHuella, SipDominio: f.SipDominio,
		PrefijoSalida: f.PrefijoSalida, LlamadasPorUsuarioDia: f.LlamadasPorUsuarioDia, LlamadasSimultaneas: f.LlamadasSimultaneas, ProbadoEn: f.TelefonoProbadoEn,
		RestriccionesConfirmadasEn: f.RestriccionesConfirmadasEn}
}

func telefonoAuditable(f mensajeriabd.MensajeriaCentralita) map[string]any {
	return map[string]any{"telefono_activo": f.TelefonoActivo, "wss_puerto": f.WssPuerto, "wss_ruta": f.WssRuta, "wss_huella": f.WssHuella,
		"sip_dominio": f.SipDominio, "prefijo_salida": f.PrefijoSalida, "llamadas_por_usuario_dia": f.LlamadasPorUsuarioDia,
		"llamadas_simultaneas": f.LlamadasSimultaneas, "restricciones_confirmadas": f.RestriccionesConfirmadasEn != nil}
}

var (
	rutaWSS       = regexp.MustCompile(`^/[A-Za-z0-9/_.-]{0,63}$`)
	prefijoSalida = regexp.MustCompile(`^[0-9]{0,6}$`)
	sipUsuario    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	// numeroInternoValido: lo que el teléfono marca o transfiere dentro de la Issabel (extensiones y
	// grupos). De 2 a 6 cifras: con 8 o más, una ruta de salida podría mandarlo a la calle.
	numeroInternoValido = regexp.MustCompile(`^[0-9]{2,6}$`)
)

func (m *Modulo) validarPuertoWSS(p int32) error {
	if p == PuertoWSS || slices.Contains(m.cfg.CentralitaPuertos, int(p)) {
		return nil
	}
	return nucleo.ReglaNegocio("puerto_no_permitido", "Solo el WSS de Asterisk en el puerto 8089")
}

// dominioSIP: el dominio de From/To (el de la centralita si no hay otro).
func dominioSIP(f mensajeriabd.MensajeriaCentralita) string {
	if f.SipDominio != nil && *f.SipDominio != "" {
		return *f.SipDominio
	}
	return f.Host
}

func (m *Modulo) opcionesWSS(host string, puerto int32, ruta, huella string) sippuente.OpcionesWSS {
	return sippuente.OpcionesWSS{Host: host, Puerto: int(puerto), Ruta: ruta, Huella: huella, Plazo: m.cfg.CentralitaPlazo, Control: controlIA(m.cfg.CentralitaLoopback)}
}

// Configurar --------------------------------------------------------------------------------------------

// EntradaTelefonoCentralita es POST /centralitas/{id}/telefono.
type EntradaTelefonoCentralita struct {
	ID                    int64   `json:"-"`
	Version               int32   `json:"version" minimum:"1"`
	WssPuerto             int32   `json:"wssPuerto,omitempty" minimum:"1" maximum:"65535" doc:"8089 de fábrica"`
	WssRuta               string  `json:"wssRuta,omitempty" maxLength:"64" doc:"/ws de fábrica"`
	HuellaWss             string  `json:"huellaWss" minLength:"64" maxLength:"95" doc:"La huella que mostró «Probar» y que el administrador comparó con la de Issabel"`
	SipDominio            *string `json:"sipDominio,omitempty" maxLength:"253" doc:"Vacío = la dirección de la centralita"`
	PrefijoSalida         string  `json:"prefijoSalida" maxLength:"6" pattern:"^[0-9]{0,6}$"`
	LlamadasPorUsuarioDia int32   `json:"llamadasPorUsuarioDia" minimum:"1" maximum:"300"`
	LlamadasSimultaneas   int32   `json:"llamadasSimultaneas" minimum:"1" maximum:"100"`
	// ConfirmoRestricciones: «Confirmo que la Issabel está configurada como dice la guía (§9.3)».
	ConfirmoRestricciones bool `json:"confirmoRestricciones,omitempty" doc:"El administrador confirma la configuración de la Issabel del runbook §9.3: las extensiones del teléfono de Iris en un contexto que solo sale a Guatemala (prefijo + 8 dígitos) y a extensiones y grupos, TRANSFER_CONTEXT a ese contexto, sin las opciones T/t de Dial, desvíos ignorados y la ruta de salida sin internacionales. Sin esto no se puede encender"`
}

// EntradaCambioTelefono: activar y desactivar el teléfono.
type EntradaCambioTelefono struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

func (m *Modulo) guardarTelefono(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaCentralita) (mensajeriabd.MensajeriaCentralita, error) {
	return q.GuardarTelefonoCentralita(ctx, mensajeriabd.GuardarTelefonoCentralitaParams{ID: f.ID, TelefonoActivo: f.TelefonoActivo, WssPuerto: f.WssPuerto,
		WssRuta: f.WssRuta, WssHuella: f.WssHuella, SipDominio: f.SipDominio, PrefijoSalida: f.PrefijoSalida, LlamadasPorUsuarioDia: f.LlamadasPorUsuarioDia,
		LlamadasSimultaneas: f.LlamadasSimultaneas, TelefonoProbadoEn: f.TelefonoProbadoEn, RestriccionesConfirmadasEn: f.RestriccionesConfirmadasEn})
}

type casosTelefono struct {
	Configurar    *auditoria.Caso[EntradaTelefonoCentralita, Centralita]
	Activar       *auditoria.Caso[EntradaCambioTelefono, Centralita]
	Desactivar    *auditoria.Caso[EntradaCambioTelefono, Centralita]
	Probar        *ProbarTelefono
	Grupos        *auditoria.Caso[EntradaVerCentralita, ListaGrupos]
	CrearGrupo    *auditoria.Caso[EntradaCrearGrupo, GrupoCentralita]
	EditarGrupo   *auditoria.Caso[EntradaEditarGrupo, GrupoCentralita]
	Extension     *auditoria.Caso[EntradaTelefonoExtension, Extension]
	ProbarReg     *ProbarRegistro
	Mio           *auditoria.Caso[struct{}, TelefonoMio]
	Abrir         *auditoria.Caso[EntradaAbrirTelefono, SesionTelefono]
	Llamar        *auditoria.Caso[EntradaLlamarTelefono, MarcacionTelefono]
	Directa       *auditoria.Caso[EntradaLlamadaDirecta, LlamadaDirecta]
	Destinos      *auditoria.Caso[struct{}, DestinosTransferencia]
	conectar      *auditoria.Caso[entradaConectar, conexionTelefono]
	invitar       *auditoria.Caso[entradaInvitar, sippuente.Destino]
	transferir    *auditoria.Caso[entradaTransferir, string]
	hecho         *auditoria.Caso[entradaHechoPuente, bool]
	revalidar     *auditoria.Caso[entradaRevalidar, struct{}]
	cerrarConexio *auditoria.Caso[entradaCerrarConexion, struct{}]
	credenciales  *auditoria.Caso[entradaCredenciales, struct{}]
	// rechazoPuente: suma el rechazo a la centralita como sistema (no depende de la sesión ni de la
	// extensión de la persona).
	rechazoPuente *auditoria.Caso[entradaRechazoPuente, struct{}]
}

func nuevosCasosTelefono(m *Modulo) casosTelefono {
	k := casosTelefono{
		Configurar: auditoria.Nuevo(auditoria.Caso[EntradaTelefonoCentralita, Centralita]{
			Accion: "centralita.telefono", Permiso: nucleo.PermCentralitaConfigurar,
			Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTelefonoCentralita) (Centralita, error) {
				q := mensajeriabd.New(t)
				f, err := m.centralitaParaCambiar(ctx, q, in.ID, in.Version)
				if err != nil {
					return Centralita{}, err
				}
				if in.WssPuerto == 0 {
					in.WssPuerto = PuertoWSS
				}
				if in.WssRuta == "" {
					in.WssRuta = "/ws"
				}
				if err := m.validarPuertoWSS(in.WssPuerto); err != nil {
					return Centralita{}, err
				}
				if !rutaWSS.MatchString(in.WssRuta) {
					return Centralita{}, campoCentralita("wssRuta", "Empieza con / y solo lleva letras, números, /, _, . y -")
				}
				huella := normalizarHuella(in.HuellaWss)
				if !huellaCentralita.MatchString(huella) {
					return Centralita{}, campoCentralita("huellaWss", "La huella debe ser el SHA-256 del certificado (64 caracteres hexadecimales)")
				}
				var dominio *string
				if in.SipDominio != nil && strings.TrimSpace(*in.SipDominio) != "" {
					d := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(*in.SipDominio), "."))
					if !hostCentralita.MatchString(d) || len(d) > 253 {
						return Centralita{}, campoCentralita("sipDominio", "Escribe un nombre de dominio o una IP, sin sip: ni puerto")
					}
					dominio = &d
				}
				if !prefijoSalida.MatchString(in.PrefijoSalida) {
					return Centralita{}, campoCentralita("prefijoSalida", "Hasta 6 dígitos")
				}
				if in.LlamadasPorUsuarioDia < 1 || in.LlamadasPorUsuarioDia > 300 || in.LlamadasSimultaneas < 1 || in.LlamadasSimultaneas > 100 {
					return Centralita{}, campoCentralita("llamadasPorUsuarioDia", "Fuera de rango")
				}
				antes := f
				cambiaConexion := in.WssPuerto != f.WssPuerto || in.WssRuta != f.WssRuta || f.WssHuella == nil || *f.WssHuella != huella ||
					derefTexto(dominio) != derefTexto(f.SipDominio)
				f.WssPuerto, f.WssRuta, f.WssHuella, f.SipDominio, f.PrefijoSalida = in.WssPuerto, in.WssRuta, &huella, dominio, in.PrefijoSalida
				f.LlamadasPorUsuarioDia, f.LlamadasSimultaneas = in.LlamadasPorUsuarioDia, in.LlamadasSimultaneas
				if cambiaConexion {
					// Otros datos del WSS: hay que volver a probar y encender (las conexiones se cortan).
					f.TelefonoActivo, f.TelefonoProbadoEn = false, nil
				}
				switch {
				case !in.ConfirmoRestricciones:
					f.RestriccionesConfirmadasEn, f.TelefonoActivo = nil, false
				case f.RestriccionesConfirmadasEn == nil:
					ahora := m.ahora()
					f.RestriccionesConfirmadasEn = &ahora
				}
				if f, err = m.guardarTelefono(ctx, q, f); err != nil {
					return Centralita{}, err
				}
				t.FijarEntidad("centralita", f.ID)
				campos := t.RegistrarCambios("centralita", f.ID, telefonoAuditable(antes), telefonoAuditable(f))
				return aCentralita(f), t.Emitir("centralita.telefono_cambiado", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos,
					"activo": f.TelefonoActivo})
			},
		}),
		Activar:    nuevoCambioTelefono(m, "centralita.activar_telefono", true),
		Desactivar: nuevoCambioTelefono(m, "centralita.desactivar_telefono", false),
		Probar:     nuevoProbarTelefono(m),
		ProbarReg:  nuevoProbarRegistro(m),
	}
	k.Grupos, k.CrearGrupo, k.EditarGrupo = nuevosCasosGrupos(m)
	k.Extension = nuevoTelefonoExtension(m)
	m.nuevosCasosTelefonoPersona(&k)
	m.nuevosCasosPuente(&k)
	return k
}

func nuevoCambioTelefono(m *Modulo, accion string, activar bool) *auditoria.Caso[EntradaCambioTelefono, Centralita] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCambioTelefono, Centralita]{
		Accion: accion, Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCambioTelefono) (Centralita, error) {
			q := mensajeriabd.New(t)
			f, err := m.centralitaParaCambiar(ctx, q, in.ID, in.Version)
			if err != nil {
				return Centralita{}, err
			}
			t.FijarEntidad("centralita", f.ID)
			if f.TelefonoActivo == activar {
				return aCentralita(f), nil
			}
			if activar {
				if f.TelefonoProbadoEn == nil || f.WssHuella == nil {
					return Centralita{}, nucleo.ReglaNegocio("sin_probar", "Prueba el WSS de la centralita antes de encender el teléfono")
				}
				if f.RestriccionesConfirmadasEn == nil {
					return Centralita{}, nucleo.ReglaNegocio("restricciones_sin_confirmar",
						"Confirma que la Issabel está configurada como dice la guía (contexto que solo sale a Guatemala y sin transferencias por teclado)")
				}
				if !m.cfg.TURN.Configurado() {
					return Centralita{}, nucleo.ReglaNegocio("turn_no_configurado", "La instalación no tiene el relevo de audio (TURN): pide a quien administra el servidor que lo configure")
				}
			}
			antes := f
			f.TelefonoActivo = activar
			if f, err = m.guardarTelefono(ctx, q, f); err != nil {
				return Centralita{}, err
			}
			campos := t.RegistrarCambios("centralita", f.ID, telefonoAuditable(antes), telefonoAuditable(f))
			return aCentralita(f), t.Emitir("centralita.telefono_cambiado", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "campos": campos,
				"activo": f.TelefonoActivo})
		},
	})
}

// Probar el WSS --------------------------------------------------------------------------------------

// EntradaProbarTelefono es POST /centralitas/{id}/telefono/probar. Sin datos, prueba los guardados (con
// la huella fijada) y, si pasa, deja probadoEn; con datos distintos, solo informa (no guarda).
type EntradaProbarTelefono struct {
	ID        int64  `json:"-"`
	WssPuerto int32  `json:"wssPuerto,omitempty" minimum:"1" maximum:"65535"`
	WssRuta   string `json:"wssRuta,omitempty" maxLength:"64"`
}

// PruebaTelefono es el resultado: un solo «no_conecta» para todo lo que no sea huella o SIP.
type PruebaTelefono struct {
	Estado     string      `json:"estado" enum:"ok,huella_distinta,no_es_sip,no_conecta"`
	Huella     *string     `json:"huella" nullable:"true" doc:"La huella del certificado que presentó el WSS (para compararla con la de Issabel)"`
	Centralita *Centralita `json:"centralita,omitempty" doc:"Al día, si se probaron los datos guardados"`
}

// ProbarTelefono conecta fuera de la transacción y deja el resultado auditado.
type ProbarTelefono struct {
	m       *Modulo
	previo  *auditoria.Caso[EntradaProbarTelefono, destinoTelefono]
	guardar *auditoria.Caso[entradaTelefonoProbado, Centralita]
}

type destinoTelefono struct {
	opciones  sippuente.OpcionesWSS
	dominio   string
	guardados bool
	id        int64
	version   int32
}

type entradaTelefonoProbado struct {
	ID        int64  `json:"id"`
	Version   int32  `json:"version"`
	Estado    string `json:"estado"`
	Guardados bool   `json:"guardados"`
}

func nuevoProbarTelefono(m *Modulo) *ProbarTelefono {
	k := &ProbarTelefono{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaProbarTelefono, destinoTelefono]{
		Accion: "centralita.probar_telefono", Permiso: nucleo.PermCentralitaConfigurar, AuditarExito: sinAuditar[destinoTelefono],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarTelefono) (destinoTelefono, error) {
			f, err := m.centralitaVisible(ctx, mensajeriabd.New(t), in.ID)
			if err != nil {
				return destinoTelefono{}, err
			}
			puerto, ruta := f.WssPuerto, f.WssRuta
			if in.WssPuerto != 0 {
				puerto = in.WssPuerto
			}
			if in.WssRuta != "" {
				ruta = in.WssRuta
			}
			if err := m.validarPuertoWSS(puerto); err != nil {
				return destinoTelefono{}, err
			}
			if !rutaWSS.MatchString(ruta) {
				return destinoTelefono{}, campoCentralita("wssRuta", "Empieza con / y solo lleva letras, números, /, _, . y -")
			}
			if err := m.validarHostCentralita(f.Host); err != nil {
				return destinoTelefono{}, err
			}
			guardados := puerto == f.WssPuerto && ruta == f.WssRuta && f.WssHuella != nil
			huella := ""
			if guardados {
				huella = *f.WssHuella
			}
			return destinoTelefono{opciones: m.opcionesWSS(f.Host, puerto, ruta, huella), dominio: dominioSIP(f), guardados: guardados, id: f.ID, version: f.Version}, nil
		},
	})
	k.guardar = auditoria.Nuevo(auditoria.Caso[entradaTelefonoProbado, Centralita]{
		Accion: "centralita.probar_telefono", Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaTelefonoProbado) (Centralita, error) {
			q := mensajeriabd.New(t)
			f, err := m.centralitaVisible(ctx, q, in.ID)
			if err != nil {
				return Centralita{}, err
			}
			t.FijarEntidad("centralita", f.ID)
			t.Anotar(map[string]any{"estado": in.Estado})
			if in.Guardados && f.Version == in.Version {
				if f, err = m.centralitaParaCambiar(ctx, q, in.ID, in.Version); err != nil {
					return Centralita{}, err
				}
				ahora := m.ahora()
				antes := f.TelefonoProbadoEn
				if in.Estado == sippuente.PruebaOK {
					f.TelefonoProbadoEn = &ahora
				} else {
					f.TelefonoProbadoEn = nil
				}
				if (antes == nil) != (f.TelefonoProbadoEn == nil) || in.Estado == sippuente.PruebaOK {
					if f, err = m.guardarTelefono(ctx, q, f); err != nil {
						return Centralita{}, err
					}
				}
			}
			return aCentralita(f), t.Emitir("centralita.telefono_probado", map[string]any{"centralita_id": f.ID, "empresa_id": f.EmpresaID, "estado": in.Estado})
		},
	})
	return k
}

// Ejecutar prueba el WSS (fuera de la transacción) y deja el resultado.
func (k *ProbarTelefono) Ejecutar(ctx context.Context, in EntradaProbarTelefono) (PruebaTelefono, error) {
	d, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return PruebaTelefono{}, err
	}
	r := sippuente.Probar(ctx, d.opciones, d.dominio)
	c, err := k.guardar.Ejecutar(ctx, entradaTelefonoProbado{ID: d.id, Version: d.version, Estado: r.Estado, Guardados: d.guardados})
	if err != nil {
		return PruebaTelefono{}, err
	}
	out := PruebaTelefono{Estado: r.Estado}
	if r.Huella != "" {
		h := r.Huella
		out.Huella = &h
	}
	if d.guardados {
		out.Centralita = &c
	}
	return out, nil
}

// Grupos -----------------------------------------------------------------------------------------------

// GrupoCentralita es un grupo de timbrado o una cola de Issabel (destino de «Transferir»).
type GrupoCentralita struct {
	ID      int64  `json:"id"`
	Nombre  string `json:"nombre"`
	Numero  string `json:"numero"`
	Activo  bool   `json:"activo"`
	Version int32  `json:"version"`
}

// ListaGrupos es GET /centralitas/{id}/grupos.
type ListaGrupos struct {
	Data []GrupoCentralita `json:"data"`
}

// EntradaCrearGrupo es POST /centralitas/{id}/grupos.
type EntradaCrearGrupo struct {
	CentralitaID int64  `json:"-"`
	Nombre       string `json:"nombre" minLength:"1" maxLength:"60"`
	Numero       string `json:"numero" pattern:"^[0-9]{2,6}$" doc:"El número del grupo o la cola en Issabel (2 a 6 dígitos)"`
}

// EntradaEditarGrupo es POST /centralitas/{id}/grupos/{grupoId}/actualizar.
type EntradaEditarGrupo struct {
	CentralitaID int64  `json:"-"`
	GrupoID      int64  `json:"-"`
	Version      int32  `json:"version" minimum:"1"`
	Nombre       string `json:"nombre" minLength:"1" maxLength:"60"`
	Numero       string `json:"numero" pattern:"^[0-9]{2,6}$"`
	Activo       bool   `json:"activo"`
}

func aGrupo(g mensajeriabd.MensajeriaCentralitaGrupo) GrupoCentralita {
	return GrupoCentralita{ID: g.ID, Nombre: g.Nombre, Numero: g.Numero, Activo: g.Activo, Version: g.Version}
}

func nuevosCasosGrupos(m *Modulo) (*auditoria.Caso[EntradaVerCentralita, ListaGrupos], *auditoria.Caso[EntradaCrearGrupo, GrupoCentralita],
	*auditoria.Caso[EntradaEditarGrupo, GrupoCentralita]) {
	listar := auditoria.Nuevo(auditoria.Caso[EntradaVerCentralita, ListaGrupos]{
		Accion: "centralita.grupos", Permiso: nucleo.PermCentralitaConfigurar, AuditarExito: sinAuditar[ListaGrupos],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerCentralita) (ListaGrupos, error) {
			q := mensajeriabd.New(t)
			if _, err := m.centralitaVisible(ctx, q, in.ID); err != nil {
				return ListaGrupos{}, err
			}
			gs, err := q.GruposDeCentralita(ctx, in.ID)
			if err != nil {
				return ListaGrupos{}, err
			}
			out := ListaGrupos{Data: make([]GrupoCentralita, 0, len(gs))}
			for _, g := range gs {
				out.Data = append(out.Data, aGrupo(g))
			}
			return out, nil
		},
	})
	validar := func(nombre, numero string) (string, error) {
		n := strings.TrimSpace(nombre)
		if n == "" || len([]rune(n)) > 60 {
			return "", campoCentralita("nombre", "De 1 a 60 caracteres")
		}
		if !numeroInternoValido.MatchString(numero) {
			return "", campoCentralita("numero", "De 2 a 6 dígitos (con 8 o más saldría a la calle)")
		}
		return n, nil
	}
	crear := auditoria.Nuevo(auditoria.Caso[EntradaCrearGrupo, GrupoCentralita]{
		Accion: "centralita.crear_grupo", Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCrearGrupo) (GrupoCentralita, error) {
			nombre, err := validar(in.Nombre, in.Numero)
			if err != nil {
				return GrupoCentralita{}, err
			}
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return GrupoCentralita{}, err
			}
			g, err := q.InsertarGrupoCentralita(ctx, mensajeriabd.InsertarGrupoCentralitaParams{EmpresaID: c.EmpresaID, CentralitaID: c.ID, Nombre: nombre, Numero: in.Numero})
			if esUnica(err) {
				return GrupoCentralita{}, nucleo.Conflicto("grupo_existente", "Ya hay un grupo activo con ese número")
			}
			if err != nil {
				return GrupoCentralita{}, err
			}
			t.FijarEntidad("centralita_grupo", g.ID)
			t.RegistrarCambios("centralita_grupo", g.ID, nil, map[string]any{"nombre": g.Nombre, "numero": g.Numero})
			return aGrupo(g), t.Emitir("grupo_centralita.creado", map[string]any{"grupo_id": g.ID, "centralita_id": c.ID, "empresa_id": c.EmpresaID,
				"nombre": g.Nombre, "numero": g.Numero})
		},
	})
	editar := auditoria.Nuevo(auditoria.Caso[EntradaEditarGrupo, GrupoCentralita]{
		Accion: "centralita.editar_grupo", Permiso: nucleo.PermCentralitaConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEditarGrupo) (GrupoCentralita, error) {
			nombre, err := validar(in.Nombre, in.Numero)
			if err != nil {
				return GrupoCentralita{}, err
			}
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return GrupoCentralita{}, err
			}
			g, err := q.GrupoCentralitaPorID(ctx, mensajeriabd.GrupoCentralitaPorIDParams{ID: in.GrupoID, CentralitaID: c.ID})
			if errors.Is(err, pgx.ErrNoRows) {
				return GrupoCentralita{}, nucleo.NoEncontrado("El grupo no existe")
			}
			if err != nil {
				return GrupoCentralita{}, err
			}
			if g.Version != in.Version {
				return GrupoCentralita{}, nucleo.ConflictoVersion("centralita_grupo", g.ID)
			}
			antes := map[string]any{"nombre": g.Nombre, "numero": g.Numero, "activo": g.Activo}
			n, err := q.GuardarGrupoCentralita(ctx, mensajeriabd.GuardarGrupoCentralitaParams{ID: g.ID, Nombre: nombre, Numero: in.Numero, Activo: in.Activo})
			if esUnica(err) {
				return GrupoCentralita{}, nucleo.Conflicto("grupo_existente", "Ya hay un grupo activo con ese número")
			}
			if err != nil {
				return GrupoCentralita{}, err
			}
			t.FijarEntidad("centralita_grupo", n.ID)
			campos := t.RegistrarCambios("centralita_grupo", n.ID, antes, map[string]any{"nombre": n.Nombre, "numero": n.Numero, "activo": n.Activo})
			return aGrupo(n), t.Emitir("grupo_centralita.actualizado", map[string]any{"grupo_id": n.ID, "centralita_id": c.ID, "empresa_id": c.EmpresaID,
				"campos": campos, "activo": n.Activo})
		},
	})
	return listar, crear, editar
}

// Credencial SIP de la extensión ----------------------------------------------------------------------

// EntradaTelefonoExtension es POST /centralitas/{id}/extensiones/{extensionId}/telefono.
type EntradaTelefonoExtension struct {
	CentralitaID int64   `json:"-"`
	ExtensionID  int64   `json:"-"`
	Tipo         string  `json:"tipo" enum:"escritorio,navegador" doc:"navegador: contesta y llama en el teléfono de Iris"`
	SipUsuario   *string `json:"sipUsuario,omitempty" maxLength:"64" doc:"El usuario SIP del endpoint en Issabel (de fábrica, la extensión)"`
	SipSecreto   *string `json:"sipSecreto,omitempty" minLength:"8" maxLength:"128" doc:"Contraseña SIP del endpoint (solo escritura: se guarda cifrada y nunca se devuelve)"`
}

func validarSecretoSIP(s string) error {
	if len(s) < 8 || len(s) > 128 || !ami.ValorSeguro(s) || strings.ContainsAny(s, "\"\\") {
		return campoCentralita("sipSecreto", "La contraseña SIP debe tener entre 8 y 128 caracteres, sin saltos de línea, comillas ni \\")
	}
	return nil
}

func (m *Modulo) cifrarSIP(id int64, plano string) ([]byte, error) {
	if m.cfg.Cifrador == nil {
		return nil, nucleo.ReglaNegocio("cifrado_no_configurado", "Falta IRIS_CLAVE_CIFRADO: no se puede guardar la contraseña SIP")
	}
	return m.cfg.Cifrador.Cifrar([]byte(plano), DatosAsociadosExtensionSIP(id))
}

func (m *Modulo) descifrarSIP(id int64, cif []byte) (string, error) {
	if cif == nil || m.cfg.Cifrador == nil {
		return "", errors.New("extensión sin credencial o sin cifrador")
	}
	b, err := m.cfg.Cifrador.Descifrar(cif, DatosAsociadosExtensionSIP(id))
	return string(b), err
}

func aExtension(x mensajeriabd.MensajeriaCentralitaExtensione, nombre string) Extension {
	return Extension{ID: x.ID, Extension: x.Extension, UsuarioID: x.UsuarioID, UsuarioNombre: nombre, Tipo: x.Tipo, SipUsuario: x.SipUsuario,
		SipGuardadoEn: x.SipGuardadoEn, SipProbadoEn: x.SipProbadoEn, SipRechazadoEn: x.SipRechazadoEn}
}

const (
	// pruebasRegistroExtension y pruebasRegistroCentralita: «Probar registro» por minuto. Cada prueba
	// es un REGISTER contra la Issabel: muchas seguidas con una contraseña mala las cuenta fail2ban.
	pruebasRegistroExtension  = 3
	pruebasRegistroCentralita = 10
	// esperaRechazo: con la contraseña que la Issabel ya rechazó no se vuelve a probar hasta escribir
	// otra o hasta que pase este tiempo (fail2ban suele olvidar en 10 a 30 min).
	esperaRechazo = 30 * time.Minute
	// rechazosCentralita en ventanaRechazos: contraseñas que la Issabel rechazó en toda la centralita
	// («Probar registro» y el puente). fail2ban suele bloquear con ~5 fallos en 10 min: pasado el tope
	// no se prueba ni se abre el teléfono hasta que pase la ventana (429 `demasiados_rechazos`).
	rechazosCentralita = 3
	ventanaRechazos    = 10 * time.Minute
)

// limitarRechazos: 429 `demasiados_rechazos` si la centralita ya juntó el tope de contraseñas
// rechazadas en la ventana. Vale para «Probar registro», /telefono/abrir y el puente.
func (m *Modulo) limitarRechazos(ctx context.Context, q *mensajeriabd.Queries, centralitaID int64) error {
	r, err := q.RechazosCentralita(ctx, centralitaID)
	if err != nil {
		return err
	}
	ahora := m.ahora()
	if r.RechazosDesde == nil || r.Rechazos < rechazosCentralita || !ahora.Before(r.RechazosDesde.Add(ventanaRechazos)) {
		return nil
	}
	espera := int(math.Ceil(r.RechazosDesde.Add(ventanaRechazos).Sub(ahora).Seconds()))
	return nucleo.NuevoProblema(http.StatusTooManyRequests, "demasiados_rechazos", "La centralita rechazó varias contraseñas seguidas",
		"Para que la Issabel no bloquee a Iris, el teléfono y las pruebas de registro esperan unos minutos. Revisa las contraseñas de las extensiones en Issabel.").
		ConCabecera("Retry-After", strconv.Itoa(max(espera, 1)))
}

// turnoPruebaRegistro: cuánto dura la reserva de «Probar registro» si el proceso muere antes de
// soltarla (el REGISTER tarda a lo más 2 × 10 s).
const turnoPruebaRegistro = 30 * time.Second

// reservarPruebaRegistro: un solo REGISTER de prueba a la vez por centralita. El REGISTER corre fuera
// de la transacción, así que leer el contador de rechazos no basta: varias pruebas a la vez lo verían
// igual y pasarían todas. La reserva es un UPDATE condicional (atómico); la suelta `guardar`. Si otra
// prueba la tiene: 429 con un Retry-After corto (`demasiados_rechazos` si la centralita ya tiene
// rechazos en la ventana, `prueba_en_curso` si no).
func (m *Modulo) reservarPruebaRegistro(ctx context.Context, q *mensajeriabd.Queries, centralitaID int64) error {
	ahora := m.ahora()
	n, err := q.ReservarPruebaRegistro(ctx, mensajeriabd.ReservarPruebaRegistroParams{ID: centralitaID, Hasta: ahora.Add(turnoPruebaRegistro), Ahora: ahora})
	if err != nil || n == 1 {
		return err
	}
	r, err := q.RechazosCentralita(ctx, centralitaID)
	if err != nil {
		return err
	}
	if r.RechazosDesde != nil && r.Rechazos > 0 && ahora.Before(r.RechazosDesde.Add(ventanaRechazos)) {
		return nucleo.NuevoProblema(http.StatusTooManyRequests, "demasiados_rechazos", "La centralita rechazó contraseñas hace poco",
			"Hay otra prueba de registro en curso en esta centralita: espera unos segundos.").ConCabecera("Retry-After", "10")
	}
	return nucleo.NuevoProblema(http.StatusTooManyRequests, "prueba_en_curso", "Otra prueba de registro en curso",
		"Hay otra prueba de registro en curso en esta centralita: espera unos segundos.").ConCabecera("Retry-After", "10")
}

// anotarRechazo suma un rechazo de la Issabel a la centralita (en la misma transacción que lo marca).
func (m *Modulo) anotarRechazo(ctx context.Context, q *mensajeriabd.Queries, centralitaID int64) error {
	ahora := m.ahora()
	_, err := q.ContarRechazoCentralita(ctx, mensajeriabd.ContarRechazoCentralitaParams{ID: centralitaID, Desde: ahora.Add(-ventanaRechazos), Ahora: ahora})
	return err
}

// limitarPruebaRegistro (en la transacción, con la extensión bloqueada): 429 si la contraseña es la
// que la Issabel ya rechazó o si se pasó el tope por minuto de la extensión o de la centralita.
func (m *Modulo) limitarPruebaRegistro(ctx context.Context, q *mensajeriabd.Queries, x mensajeriabd.MensajeriaCentralitaExtensione, centralitaID int64) error {
	ahora := m.ahora()
	if r := x.SipRechazadoEn; r != nil && (x.SipGuardadoEn == nil || !x.SipGuardadoEn.After(*r)) && ahora.Sub(*r) < esperaRechazo {
		espera := int(math.Ceil((esperaRechazo - ahora.Sub(*r)).Seconds()))
		return nucleo.NuevoProblema(http.StatusTooManyRequests, "credencial_rechazada", "La centralita ya rechazó esta contraseña",
			"Escribe otra vez la contraseña de la extensión con «Cambiar»: probar la misma solo haría que la Issabel bloquee a Iris.").
			ConCabecera("Retry-After", strconv.Itoa(espera))
	}
	if err := m.limitarRechazos(ctx, q, centralitaID); err != nil {
		return err
	}
	// El tope por minuto queda solo contra ráfagas (el de rechazos es el que cuida a fail2ban).
	desde := ahora.Add(-time.Minute)
	nx, err := q.ContarPruebaRegistroExtension(ctx, mensajeriabd.ContarPruebaRegistroExtensionParams{ID: x.ID, Desde: desde, Ahora: ahora})
	if err != nil {
		return err
	}
	nc, err := q.ContarPruebaRegistroCentralita(ctx, mensajeriabd.ContarPruebaRegistroCentralitaParams{ID: centralitaID, Desde: desde, Ahora: ahora})
	if err != nil {
		return err
	}
	if nx > pruebasRegistroExtension || nc > pruebasRegistroCentralita {
		return nucleo.NuevoProblema(http.StatusTooManyRequests, "demasiados_intentos", "Demasiadas pruebas seguidas",
			"Espera un minuto antes de probar otra vez.").ConCabecera("Retry-After", "60")
	}
	return nil
}

func nuevoTelefonoExtension(m *Modulo) *auditoria.Caso[EntradaTelefonoExtension, Extension] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTelefonoExtension, Extension]{
		Accion: "centralita.telefono_extension", Permiso: nucleo.PermCentralitaAsignarExtension,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTelefonoExtension) (Extension, error) {
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return Extension{}, err
			}
			x, err := q.ExtensionPorID(ctx, mensajeriabd.ExtensionPorIDParams{ID: in.ExtensionID, CentralitaID: c.ID})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !x.Activa) {
				return Extension{}, nucleo.NoEncontrado("La extensión no existe")
			}
			if err != nil {
				return Extension{}, err
			}
			antes := map[string]any{"tipo": x.Tipo, "sip_usuario": x.SipUsuario, "credencial_guardada": x.SipSecretoCifrado != nil}
			estadoClave := "sin_cambio"
			p := mensajeriabd.GuardarTelefonoExtensionParams{ID: x.ID, Tipo: in.Tipo}
			switch in.Tipo {
			case "escritorio":
				if x.SipSecretoCifrado != nil {
					estadoClave = "borrada"
				}
			case "navegador":
				usuario := x.Extension
				if in.SipUsuario != nil && strings.TrimSpace(*in.SipUsuario) != "" {
					usuario = strings.TrimSpace(*in.SipUsuario)
				}
				if !sipUsuario.MatchString(usuario) {
					return Extension{}, campoCentralita("sipUsuario", "Solo letras, números, punto, guion y guion bajo")
				}
				p.SipUsuario = &usuario
				// La contraseña guardada nunca se usa con otro usuario SIP.
				cambiaUsuario := x.SipUsuario == nil || *x.SipUsuario != usuario
				switch {
				case in.SipSecreto != nil:
					if err := validarSecretoSIP(*in.SipSecreto); err != nil {
						return Extension{}, err
					}
					cif, err := m.cifrarSIP(x.ID, *in.SipSecreto)
					if err != nil {
						return Extension{}, err
					}
					ahora := m.ahora()
					p.SipSecretoCifrado, p.SipGuardadoEn, estadoClave = cif, &ahora, "guardada"
				case x.SipSecretoCifrado == nil || cambiaUsuario:
					return Extension{}, nucleo.ReglaNegocio("clave_requerida", "Escribe la contraseña SIP de la extensión (se guarda cifrada y no se vuelve a mostrar)")
				default:
					p.SipSecretoCifrado, p.SipGuardadoEn, p.SipProbadoEn = x.SipSecretoCifrado, x.SipGuardadoEn, x.SipProbadoEn
				}
			default:
				return Extension{}, campoCentralita("tipo", "escritorio o navegador")
			}
			// Cualquier cambio corta la conexión de esa persona en el siguiente latido del puente (la
			// credencial o el tipo ya no son los mismos).
			n, err := q.GuardarTelefonoExtension(ctx, p)
			if err != nil {
				return Extension{}, err
			}
			t.FijarEntidad("centralita_extension", n.ID)
			t.RegistrarCambios("centralita_extension", n.ID, antes, map[string]any{"tipo": n.Tipo, "sip_usuario": n.SipUsuario,
				"credencial_guardada": n.SipSecretoCifrado != nil})
			nombre := ""
			if u, err := q.UsuarioDeEmpresaActivo(ctx, mensajeriabd.UsuarioDeEmpresaActivoParams{Usuario: n.UsuarioID, Empresa: 0}); err == nil {
				nombre = u.Nombre
			}
			return aExtension(n, nombre), t.Emitir("extension.telefono_cambiado", map[string]any{"centralita_id": c.ID, "empresa_id": c.EmpresaID,
				"extension_id": n.ID, "usuario_id": n.UsuarioID, "tipo": n.Tipo, "credencial": estadoClave})
		},
	})
}

// Probar registro -----------------------------------------------------------------------------------

// EntradaProbarRegistro es POST /centralitas/{id}/extensiones/{extensionId}/probar-registro.
type EntradaProbarRegistro struct {
	CentralitaID int64 `json:"centralita_id"`
	ExtensionID  int64 `json:"extension_id"`
}

// PruebaRegistro es el resultado de «Probar registro».
type PruebaRegistro struct {
	Estado    string     `json:"estado" enum:"ok,credenciales,huella_distinta,no_conecta,sin_credencial"`
	Extension *Extension `json:"extension,omitempty"`
}

// ProbarRegistro manda un REGISTER de consulta con la credencial guardada (fuera de la transacción).
type ProbarRegistro struct {
	m       *Modulo
	previo  *auditoria.Caso[EntradaProbarRegistro, destinoRegistro]
	guardar *auditoria.Caso[entradaRegistroProbado, Extension]
}

type destinoRegistro struct {
	opciones sippuente.OpcionesWSS
	cred     sippuente.Credencial
	sin      bool
	// guardadoEn: qué contraseña se prueba (sip_guardado_en); el resultado solo marca esa.
	guardadoEn *time.Time
}

type entradaRegistroProbado struct {
	CentralitaID int64      `json:"centralita_id"`
	ExtensionID  int64      `json:"extension_id"`
	Estado       string     `json:"estado"`
	GuardadoEn   *time.Time `json:"guardado_en"`
	// Reservado: esta prueba tiene el turno de la centralita (salió el REGISTER) y lo suelta.
	Reservado bool `json:"reservado"`
}

func nuevoProbarRegistro(m *Modulo) *ProbarRegistro {
	k := &ProbarRegistro{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaProbarRegistro, destinoRegistro]{
		Accion: "centralita.probar_registro", Permiso: nucleo.PermCentralitaAsignarExtension, AuditarExito: sinAuditar[destinoRegistro],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProbarRegistro) (destinoRegistro, error) {
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return destinoRegistro{}, err
			}
			x, err := q.ExtensionPorID(ctx, mensajeriabd.ExtensionPorIDParams{ID: in.ExtensionID, CentralitaID: c.ID})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !x.Activa) {
				return destinoRegistro{}, nucleo.NoEncontrado("La extensión no existe")
			}
			if err != nil {
				return destinoRegistro{}, err
			}
			if x.Tipo != "navegador" || x.SipSecretoCifrado == nil || x.SipUsuario == nil || c.WssHuella == nil {
				return destinoRegistro{sin: true}, nil
			}
			if err := m.limitarPruebaRegistro(ctx, q, x, c.ID); err != nil {
				return destinoRegistro{}, err
			}
			// La contraseña solo viaja al certificado fijado del WSS.
			s, err := m.descifrarSIP(x.ID, x.SipSecretoCifrado)
			if err != nil {
				return destinoRegistro{sin: true}, nil //nolint:nilerr // sin credencial legible: «sin_credencial»
			}
			if err := m.validarHostCentralita(c.Host); err != nil {
				return destinoRegistro{}, err
			}
			// Lo último: solo se reserva si va a salir el REGISTER (la suelta `guardar`).
			if err := m.reservarPruebaRegistro(ctx, q, c.ID); err != nil {
				return destinoRegistro{}, err
			}
			return destinoRegistro{opciones: m.opcionesWSS(c.Host, c.WssPuerto, c.WssRuta, *c.WssHuella),
				cred: sippuente.Credencial{Usuario: *x.SipUsuario, Secreto: s, Dominio: dominioSIP(c)}, guardadoEn: x.SipGuardadoEn}, nil
		},
	})
	k.guardar = auditoria.Nuevo(auditoria.Caso[entradaRegistroProbado, Extension]{
		Accion: "centralita.probar_registro", Permiso: nucleo.PermCentralitaAsignarExtension,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRegistroProbado) (Extension, error) {
			q := mensajeriabd.New(t)
			c, err := m.centralitaVisible(ctx, q, in.CentralitaID)
			if err != nil {
				return Extension{}, err
			}
			x, err := q.ExtensionPorID(ctx, mensajeriabd.ExtensionPorIDParams{ID: in.ExtensionID, CentralitaID: c.ID})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !x.Activa) {
				return Extension{}, nucleo.NoEncontrado("La extensión no existe")
			}
			if err != nil {
				return Extension{}, err
			}
			t.FijarEntidad("centralita_extension", x.ID)
			// El turno de la centralita queda libre para la siguiente prueba.
			if in.Reservado {
				if err := q.SoltarPruebaRegistro(ctx, c.ID); err != nil {
					return Extension{}, err
				}
			}
			// La Issabel vio el rechazo aunque la contraseña ya haya cambiado: cuenta para fail2ban.
			if in.Estado == sippuente.PruebaCredenciales {
				if err := m.anotarRechazo(ctx, q, c.ID); err != nil {
					return Extension{}, err
				}
			}
			// Solo marca si la contraseña probada sigue siendo la guardada (si no, la nueva queda como estaba).
			vigente := in.GuardadoEn != nil && x.SipGuardadoEn != nil && x.SipGuardadoEn.Equal(*in.GuardadoEn)
			t.Anotar(map[string]any{"estado": in.Estado, "vigente": vigente})
			if vigente {
				ahora := m.ahora()
				var n int64
				switch in.Estado {
				case sippuente.PruebaOK:
					n, err = q.MarcarRegistroProbado(ctx, mensajeriabd.MarcarRegistroProbadoParams{ID: x.ID, SipProbadoEn: &ahora, Guardado: *in.GuardadoEn})
					x.SipProbadoEn, x.SipRechazadoEn = &ahora, nil
				case sippuente.PruebaCredenciales:
					n, err = q.MarcarRegistroRechazado(ctx, mensajeriabd.MarcarRegistroRechazadoParams{ID: x.ID, Ahora: ahora, Guardado: *in.GuardadoEn})
					x.SipProbadoEn, x.SipRechazadoEn = nil, &ahora
				default:
					n, err = q.MarcarRegistroProbado(ctx, mensajeriabd.MarcarRegistroProbadoParams{ID: x.ID, SipProbadoEn: nil, Guardado: *in.GuardadoEn})
					x.SipProbadoEn = nil
				}
				if err != nil {
					return Extension{}, err
				}
				if n != 1 {
					return Extension{}, errors.New("la extensión cambió durante la prueba")
				}
			}
			if err := t.Emitir("extension.registro_probado", map[string]any{"centralita_id": c.ID, "empresa_id": c.EmpresaID, "extension_id": x.ID,
				"usuario_id": x.UsuarioID, "estado": in.Estado}); err != nil {
				return Extension{}, err
			}
			return aExtension(x, ""), nil
		},
	})
	return k
}

// Ejecutar prueba el registro y deja el resultado.
func (k *ProbarRegistro) Ejecutar(ctx context.Context, in EntradaProbarRegistro) (PruebaRegistro, error) {
	d, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return PruebaRegistro{}, err
	}
	estado := "sin_credencial"
	if k.m.antesDeProbarRegistro != nil {
		k.m.antesDeProbarRegistro()
	}
	if !d.sin {
		estado = sippuente.ProbarRegistro(ctx, d.opciones, d.cred).Estado
		if estado == sippuente.PruebaNoEsSIP {
			estado = sippuente.PruebaNoConecta
		}
	}
	x, err := k.guardar.Ejecutar(ctx, entradaRegistroProbado{CentralitaID: in.CentralitaID, ExtensionID: in.ExtensionID, Estado: estado, GuardadoEn: d.guardadoEn,
		Reservado: !d.sin})
	if err != nil {
		return PruebaRegistro{}, err
	}
	return PruebaRegistro{Estado: estado, Extension: &x}, nil
}
