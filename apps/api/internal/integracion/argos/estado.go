package argos

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Pantalla de estado de la conexión con Argos (contrato 0.8.0 §2.5 d) y acción de reanudar o volver
// a lanzar la primera sincronización. Cada lado muestra lo que él sabe: aquí, nunca lo que Argos
// tiene guardado del lado suyo. Ningún token ni secreto (S5).

// Reanudar o volver a lanzar ----------------------------------------------------------------------

// EsperaRelanzarSincronizacion: tras una completa, cuánto hay que esperar para lanzar otra (409
// sincronizacion_reciente; revisión de seguridad de B7).
const EsperaRelanzarSincronizacion = 10 * time.Minute

// EntradaSincronizar es la entrada de integraciones.sincronizar.
type EntradaSincronizar struct {
	ID int64 `json:"id"`
}

// nuevoSincronizar: con una en curso, la reanuda (vuelve a encolar el job, que sigue desde el último
// cursor); con una con_errores y el mismo contrato de Argos, la reabre con su mismo id; si no (completa,
// ninguna o Argos cambió de contrato), crea otra (UUID v7 más nuevo).
func nuevoSincronizar(m *Modulo) *auditoria.Caso[EntradaSincronizar, Sincronizacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSincronizar, Sincronizacion]{
		Accion: "integraciones.sincronizar", Permiso: nucleo.PermIntegracionesSincronizar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSincronizar) (Sincronizacion, error) {
			t.FijarEntidad("conector", in.ID)
			t.FijarEmpresa(0)
			f, err := conectorbd.New(t).ConectorPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Sincronizacion{}, nucleo.NoEncontrado("No existe el conector")
			}
			if err != nil {
				return Sincronizacion{}, err
			}
			if f.Sistema != Sistema {
				return Sincronizacion{}, nucleo.ReglaNegocio("sistema_sin_sincronizacion", "Solo los conectores con ArgosISP tienen primera sincronización")
			}
			if f.Estado != "activo" {
				return Sincronizacion{}, nucleo.ReglaNegocio("conector_apagado", "Activa el conector para sincronizar con Argos")
			}
			if m.cfg.Encolar == nil {
				return Sincronizacion{}, nucleo.NuevoProblema(http.StatusServiceUnavailable, "cola_no_disponible", "Cola no disponible",
					"No se pudo encolar la sincronización; vuelve a intentar en unos minutos")
			}
			q := argosbd.New(t)
			sinc, err := q.SincronizacionEnCurso(ctx, f.ID)
			modo := "reanudar"
			switch {
			case err == nil:
			case !errors.Is(err, pgx.ErrNoRows):
				return Sincronizacion{}, err
			default:
				ultima, errU := q.UltimaSincronizacion(ctx, f.ID)
				if errU == nil && ultima.Estado == SincConErrores && SoportaPrimeraSincronizacion(f.ContratoRemoto) == ultima.Informar {
					ahora, err := q.RelojBD(ctx)
					if err != nil {
						return Sincronizacion{}, err
					}
					var por *int64
					if a := t.Actor(); a.EsUsuario() {
						por = &a.ID
					}
					if err := q.ReabrirEtapas(ctx, ultima.ID); err != nil {
						return Sincronizacion{}, err
					}
					if sinc, err = q.ReabrirSincronizacion(ctx, argosbd.ReabrirSincronizacionParams{ID: ultima.ID, Ahora: ahora, IniciadaPor: por}); err != nil {
						return Sincronizacion{}, err
					}
					modo = "reabrir"
					if err := m.encolarInforme(ctx, t.Tx, sinc); err != nil {
						return Sincronizacion{}, err
					}
					if err := t.Emitir(EventoSincronizacionIniciada, map[string]any{"sincronizacion_id": sinc.ID, "conector_id": f.ID, "origen": "manual",
						"excluir_retirados": sinc.ExcluirRetirados, "contrato_remoto": sinc.ContratoRemoto}); err != nil {
						return Sincronizacion{}, err
					}
				} else if errU != nil && !errors.Is(errU, pgx.ErrNoRows) {
					return Sincronizacion{}, errU
				} else {
					// Volver a lanzar una que acaba de terminar completa no aporta nada y carga a Argos.
					if errU == nil && ultima.Estado == SincCompleta && ultima.TerminadaEn != nil && mismoTexto(ultima.ContratoRemoto, f.ContratoRemoto) {
						ahora, err := q.RelojBD(ctx)
						if err != nil {
							return Sincronizacion{}, err
						}
						if ahora.Sub(*ultima.TerminadaEn) < EsperaRelanzarSincronizacion {
							return Sincronizacion{}, nucleo.Conflicto("sincronizacion_reciente",
								"La última sincronización terminó completa hace menos de 10 minutos. Espera un poco para volver a lanzarla.")
						}
					}
					r, err := crearSincronizacion(ctx, t, m, entradaCrearSincronizacion{ConectorID: f.ID, Contrato: f.ContratoRemoto, Origen: "manual"})
					if err != nil {
						return Sincronizacion{}, err
					}
					sinc, modo = r.fila, "nueva"
				}
			}
			t.Anotar(map[string]any{"modo": modo, "sincronizacion_id": sinc.ID})
			if err := m.cfg.Encolar(ctx, t.Tx, ArgsPrimeraSincronizacion{ConectorID: f.ID}); err != nil {
				return Sincronizacion{}, err
			}
			etapas, err := q.EtapasDeSincronizacion(ctx, sinc.ID)
			if err != nil {
				return Sincronizacion{}, err
			}
			return aSincronizacion(sinc, etapas), nil
		},
	})
}

// Estado de la conexión -------------------------------------------------------------------------

// ActivacionConector: cuándo y quién activó el conector (de la auditoría).
type ActivacionConector struct {
	En          time.Time `json:"en"`
	UsuarioID   *int64    `json:"usuarioId" nullable:"true"`
	ActorNombre string    `json:"actorNombre" doc:"Nombre de quien lo activó tal como quedó en la auditoría"`
}

// FuncionApagada es una función de Iris que no se usa por la versión de Argos (§4.1).
type FuncionApagada struct {
	Codigo string `json:"codigo" enum:"primera_sincronizacion_completa,informe_sincronizacion,reconexion_sin_personal,precio_servicio,cuentas_bancarias_webhook,aprobar_comprobantes,inventario_por_tecnico,buscar_sin_red"`
	Texto  string `json:"texto" doc:"Qué no hace Iris con este Argos, en español llano"`
	Desde  string `json:"desde" doc:"Contrato o versión de Argos que la trae (\"contrato 0.8.0\", \"Argos 1.21.0\")"`
}

// InstantaneaArgos: cuántos hay en Iris y cuándo se leyó lo último.
type InstantaneaArgos struct {
	Recurso              string     `json:"recurso" enum:"clientes,servicios,zonas,routers"`
	Cuantos              int64      `json:"cuantos"`
	UltimoSincronizadoEn *time.Time `json:"ultimoSincronizadoEn" nullable:"true"`
}

// FallosArgos: lo que falló hacia o desde Argos.
type FallosArgos struct {
	WebhooksConError     int64                 `json:"webhooksConError" doc:"Webhooks de Argos guardados que no se pudieron aplicar (se reintentan)"`
	UltimoWebhookErrorEn *time.Time            `json:"ultimoWebhookErrorEn" nullable:"true"`
	WebhooksRechazados   int64                 `json:"webhooksRechazados" doc:"Webhooks con firma inválida rechazados desde que arrancó el servidor (de cualquier conector)"`
	Pendientes           []PendientesPorEstado `json:"pendientes" doc:"Acciones hacia Argos que no están hechas, por estado"`
}

// PendientesPorEstado cuenta las acciones hacia Argos de integracion.pendientes.
type PendientesPorEstado struct {
	Estado  string `json:"estado" enum:"pendiente,en_curso,fallido_negocio,agotado"`
	Cuantos int64  `json:"cuantos"`
}

// EmpresaAlcance es una empresa de Argos que cubre el token.
type EmpresaAlcance struct {
	ID     int64  `json:"id"`
	Nombre string `json:"nombre"`
	Nit    string `json:"nit"`
}

// CertificadoArgos es el certificado TLS de la API de Argos que vio Iris (§1.5, solo informa).
type CertificadoArgos struct {
	HuellaSHA256  string               `json:"huellaSha256"`
	Emisor        string               `json:"emisor"`
	Sujeto        string               `json:"sujeto"`
	Host          string               `json:"host"`
	VenceEn       *time.Time           `json:"venceEn" nullable:"true"`
	VistoEn       time.Time            `json:"vistoEn" doc:"La primera vez que Iris lo vio"`
	UltimoVistoEn time.Time            `json:"ultimoVistoEn"`
	Cambiado      bool                 `json:"cambiado" doc:"true: reemplazó a otro en este conector sin reconectar (Iris avisó al admin y no detuvo nada)"`
	Anterior      *CertificadoAnterior `json:"anterior,omitempty" required:"false"`
}

// CertificadoAnterior es el que se veía antes del cambio.
type CertificadoAnterior struct {
	HuellaSHA256  string    `json:"huellaSha256"`
	Emisor        string    `json:"emisor"`
	Sujeto        string    `json:"sujeto"`
	UltimoVistoEn time.Time `json:"ultimoVistoEn"`
}

// EstadoConexionArgos es lo que muestra Administración → Integraciones → Argos.
type EstadoConexionArgos struct {
	Conector   *Conector           `json:"conector,omitempty" required:"false" doc:"El conector de Argos activo o, si no hay, el más reciente. ausente = Iris nunca se conectó"`
	Activacion *ActivacionConector `json:"activacion,omitempty" required:"false" doc:"Última activación (de la auditoría); ausente si nunca se activó"`
	// Versiones de cada lado.
	ContratoIris      string             `json:"contratoIris"`
	ContratoArgos     *string            `json:"contratoArgos" nullable:"true"`
	VersionArgos      *string            `json:"versionArgos" nullable:"true"`
	FuncionesApagadas []FuncionApagada   `json:"funcionesApagadas" doc:"Lo que Iris no hace por la versión de Argos (§4.1); vacío si Argos tiene todo"`
	Sincronizacion    *Sincronizacion    `json:"sincronizacion,omitempty" required:"false" doc:"La última primera sincronización del conector; ausente = no ha empezado"`
	Instantaneas      []InstantaneaArgos `json:"instantaneas"`
	Fallos            FallosArgos        `json:"fallos"`
	Empresas          []EmpresaAlcance   `json:"empresas" doc:"Empresas del alcance del token (lo que informó el canje)"`
	AlcanceGlobal     *bool              `json:"alcanceGlobal" nullable:"true" doc:"true: también las empresas que se creen después en Argos; null = Argos no lo informó"`
	Certificado       *CertificadoArgos  `json:"certificado,omitempty" required:"false" doc:"Certificado TLS de la API de Argos; ausente = no se ha visto (sin llamadas por HTTPS todavía)"`
	// Capacidades (contrato 0.9.0 §4.3): lo que Iris ofrece al portal de Argos.
	Capacidades CapacidadesIris `json:"capacidades" doc:"Contrato 0.9.0 §4.3: las secciones del portal de Argos que Iris atiende (Argos muestra cada una solo con su marca en true)"`
}

// CapacidadesIris son las marcas de §4.3 (nombres del contrato, en snake_case).
type CapacidadesIris struct {
	PortalArgos         bool `json:"portal_argos" doc:"Tickets del cliente en el portal de Argos (§11.3–§11.6)"`
	SolicitudesServicio bool `json:"solicitudes_servicio" doc:"Solicitudes de servicio (§11.7)"`
	AdjuntosTicket      bool `json:"adjuntos_ticket" doc:"Adjuntos en los tickets del portal (§11.6)"`
	// 0.15.0 §4.6 (G1): las marcas nuevas van en este mismo objeto.
	TipoServicio bool `json:"tipo_servicio" doc:"Bloque A de la 0.15.0 (§13.1): Iris guarda y usa tipo_servicio y componentes[]"`
	// Mensajeria (0.15.0 §4.6 y §13.3, bloque C): solo en GET …/capacidades y solo a un conector que
	// informó contrato ≥ 0.15.0; ausente = entrega_auto false para todas las empresas.
	Mensajeria []CapacidadMensajeria `json:"mensajeria,omitempty" required:"false" doc:"Contrato 0.15.0 §13.3 (solo en GET …/capacidades, a un Argos ≥ 0.15.0): por empresa del alcance del conector, los canales por los que Iris entrega recordatorios y los códigos de §6.3 aprobados en cada uno. Ausente = entrega_auto false para todas"`
}

// CapacidadesDeIris: con un Iris que habla el contrato 0.9.0, las tres del portal; con la 0.15.0,
// también tipo_servicio (§4.6: Argos las usa solo si Iris informa ≥ 0.15.0).
func CapacidadesDeIris() CapacidadesIris {
	si := SoportaPortal(&contratoIris)
	return CapacidadesIris{PortalArgos: si, SolicitudesServicio: si, AdjuntosTicket: si, TipoServicio: SoportaTipoServicio(&contratoIris)}
}

// ConContratoIris cambia la versión del contrato que habla este Iris (las marcas de capacidades y las
// funciones que dependen de ella) y devuelve cómo restaurarla. SOLO para pruebas: en producción la
// versión es ContratoIris.
func ConContratoIris(v string) (restaurar func()) {
	antes := contratoIris
	contratoIris = v
	return func() { contratoIris = antes }
}

// FuncionesApagadas: lo que Iris deja de hacer con este Argos (§4.1, §3.3, §4.2).
func FuncionesApagadas(version, contrato *string) []FuncionApagada {
	f := []FuncionApagada{}
	if !SoportaPrimeraSincronizacion(contrato) {
		f = append(f,
			FuncionApagada{Codigo: "primera_sincronizacion_completa", Desde: "contrato 0.8.0",
				Texto: "La primera sincronización no trae cuentas ni bancos y recorre todos los clientes, también los que solo tienen servicios retirados"},
			FuncionApagada{Codigo: "informe_sincronizacion", Desde: "contrato 0.8.0", Texto: "Argos no se entera del avance de la sincronización (su pantalla dice «no informado»)"},
			FuncionApagada{Codigo: "reconexion_sin_personal", Desde: "contrato 0.8.0", Texto: "No se pide la reconexión temporal cuando no hay nadie de caja en horario hábil"},
			FuncionApagada{Codigo: "precio_servicio", Desde: "contrato 0.8.0", Texto: "No se muestra el precio mensual del servicio"},
			FuncionApagada{Codigo: "cuentas_bancarias_webhook", Desde: "contrato 0.8.0", Texto: "Argos no avisa cuando cambia una cuenta bancaria: Iris la relee al usarla"},
		)
	}
	if !SoportaAprobarComprobantes(contrato) {
		f = append(f, FuncionApagada{Codigo: "aprobar_comprobantes", Desde: "contrato 0.7.0", Texto: "No se aprueban ni rechazan boletas desde la conversación"})
	}
	if !SoportaTitularEmpresa(contrato) {
		f = append(f, FuncionApagada{Codigo: "titular_empresa", Desde: "contrato " + ContratoTitularEmpresa,
			Texto: "En la instalación no se cambia el titular a una empresa (con su NIT y representante)"})
	}
	if !SoportaInventarioTecnico(version) {
		f = append(f, FuncionApagada{Codigo: "inventario_por_tecnico", Desde: "Argos " + VersionInventarioTecnico, Texto: "Sin inventario por técnico: Iris usa el número de bodega"})
	}
	if !SoportaBuscarSinRed(version) {
		f = append(f, FuncionApagada{Codigo: "buscar_sin_red", Desde: "Argos " + VersionBuscarSinRed,
			Texto: "La búsqueda de clientes todavía encuentra por usuario PPPoE a quien no puede ver los datos de red"})
	}
	return f
}

func tiempoDe(v any) *time.Time {
	if t, ok := v.(time.Time); ok {
		return &t
	}
	return nil
}

func nuevoEstadoConexion(m *Modulo) *auditoria.Caso[struct{}, EstadoConexionArgos] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, EstadoConexionArgos]{
		Accion: "integraciones.ver_estado_argos", Permiso: nucleo.PermIntegracionesVer, SinEmpresa: true,
		AuditarExito: func(EstadoConexionArgos) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (EstadoConexionArgos, error) {
			t.FijarEmpresa(0)
			r := EstadoConexionArgos{ContratoIris: m.contratoIris(), Capacidades: CapacidadesDeIris(), FuncionesApagadas: []FuncionApagada{}, Instantaneas: []InstantaneaArgos{},
				Empresas: []EmpresaAlcance{}, Fallos: FallosArgos{Pendientes: []PendientesPorEstado{}, WebhooksRechazados: m.RechazosSinAuditar()}}
			q := argosbd.New(t)
			var f conectorbd.IntegracionConectore
			err := t.QueryRow(ctx, `SELECT id FROM integracion.conectores WHERE sistema = $1 ORDER BY (estado = 'activo') DESC, id DESC LIMIT 1`, Sistema).Scan(&f.ID)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			inst, err := q.ResumenInstantaneas(ctx)
			if err != nil {
				return r, err
			}
			for _, i := range inst {
				r.Instantaneas = append(r.Instantaneas, InstantaneaArgos{Recurso: i.Recurso, Cuantos: i.Cuantos, UltimoSincronizadoEn: tiempoDe(i.Ultimo)})
			}
			pend, err := q.ResumenPendientesArgos(ctx)
			if err != nil {
				return r, err
			}
			for _, p := range pend {
				r.Fallos.Pendientes = append(r.Fallos.Pendientes, PendientesPorEstado{Estado: p.Estado, Cuantos: p.Cuantos})
			}
			if f.ID == 0 {
				r.FuncionesApagadas = FuncionesApagadas(nil, nil)
				return r, nil
			}
			if f, err = conectorbd.New(t).ConectorPorID(ctx, f.ID); err != nil {
				return r, err
			}
			c := m.nucleo.AConector(f)
			r.Conector, r.ContratoArgos, r.VersionArgos, r.AlcanceGlobal = &c, f.ContratoRemoto, f.VersionRemota, f.AlcanceGlobal
			r.FuncionesApagadas = FuncionesApagadas(f.VersionRemota, f.ContratoRemoto)
			if a, err := q.ActivacionDeConector(ctx, f.ID); err == nil {
				r.Activacion = &ActivacionConector{En: a.CreatedAt, UsuarioID: a.ActorID, ActorNombre: a.ActorNombre}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			if len(f.Empresas) > 0 {
				emp, err := q.EmpresasPorIDs(ctx, f.Empresas)
				if err != nil {
					return r, err
				}
				nombres := map[int64]argosbd.EmpresasPorIDsRow{}
				for _, e := range emp {
					nombres[e.ID] = e
				}
				for _, id := range f.Empresas {
					e := nombres[id]
					r.Empresas = append(r.Empresas, EmpresaAlcance{ID: id, Nombre: e.Nombre, Nit: e.Nit})
				}
			}
			if s, err := q.UltimaSincronizacion(ctx, f.ID); err == nil {
				etapas, err := q.EtapasDeSincronizacion(ctx, s.ID)
				if err != nil {
					return r, err
				}
				v := aSincronizacion(s, etapas)
				r.Sincronizacion = &v
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			wh, err := q.ResumenEntrantesFallidos(ctx, f.ID)
			if err != nil {
				return r, err
			}
			r.Fallos.WebhooksConError, r.Fallos.UltimoWebhookErrorEn = wh.Cuantos, tiempoDe(wh.Ultimo)
			if cert, err := q.UltimoCertificado(ctx, f.ID); err == nil {
				v := CertificadoArgos{HuellaSHA256: cert.HuellaSha256, Emisor: cert.Emisor, Sujeto: cert.Sujeto, Host: cert.Host, VenceEn: cert.VenceEn,
					VistoEn: cert.VistoEn, UltimoVistoEn: cert.UltimoVistoEn, Cambiado: cert.Cambiado}
				if cert.AnteriorID != nil {
					if a, err := q.CertificadoPorID(ctx, *cert.AnteriorID); err == nil {
						v.Anterior = &CertificadoAnterior{HuellaSHA256: a.HuellaSha256, Emisor: a.Emisor, Sujeto: a.Sujeto, UltimoVistoEn: a.UltimoVistoEn}
					} else if !errors.Is(err, pgx.ErrNoRows) {
						return r, err
					}
				}
				r.Certificado = &v
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			return r, nil
		},
	})
}

// Certificado TLS (§1.5, «registrar y avisar») ---------------------------------------------------

// EventoCertificadoCambiado: el certificado de la API de Argos cambió sin reconexión.
const EventoCertificadoCambiado = "argos.certificado_cambiado"

// EntradaCertificado: el certificado que vio una llamada al conector.
type EntradaCertificado struct {
	ConectorID int64                     `json:"conector_id"`
	Visto      conector.CertificadoVisto `json:"visto"`
}

// ResultadoCertificado: si era nuevo y si reemplazó a otro.
type ResultadoCertificado struct {
	Nuevo    bool
	Cambiado bool
}

// nuevoRegistrarCertificado guarda el certificado si es la primera vez que se ve en el conector. Si
// reemplaza a otro (misma conexión, cadena válida: si no validara, TLS habría cortado antes), emite
// argos.certificado_cambiado para avisar al admin. Nunca bloquea nada ni marca el conector.
func nuevoRegistrarCertificado() *auditoria.Caso[EntradaCertificado, ResultadoCertificado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCertificado, ResultadoCertificado]{
		Accion: "integraciones.registrar_certificado", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoCertificado) bool { return r.Nuevo },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCertificado) (ResultadoCertificado, error) {
			t.FijarEmpresa(0)
			t.FijarEntidad("conector", in.ConectorID)
			q := argosbd.New(t)
			ahora, err := q.RelojBD(ctx)
			if err != nil {
				return ResultadoCertificado{}, err
			}
			var anterior *argosbd.IntegracionArgosCertificado
			if a, err := q.UltimoCertificado(ctx, in.ConectorID); err == nil {
				anterior = &a
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return ResultadoCertificado{}, err
			}
			visto, err := q.CertificadoPorHuella(ctx, argosbd.CertificadoPorHuellaParams{ConectorID: in.ConectorID, HuellaSha256: in.Visto.HuellaSHA256})
			switch {
			case err == nil && anterior != nil && anterior.ID == visto.ID:
				// El mismo de la última vez: solo se anota que se volvió a ver.
				return ResultadoCertificado{}, q.VolverAVerCertificado(ctx, argosbd.VolverAVerCertificadoParams{ID: visto.ID, VistoEn: ahora})
			case err == nil && anterior != nil:
				// Uno que ya se había visto vuelve después de otro (A → B → A): también es un cambio.
				f, err := q.CertificadoVuelveAVerse(ctx, argosbd.CertificadoVuelveAVerseParams{ID: visto.ID, VistoEn: ahora, AnteriorID: &anterior.ID})
				if err != nil {
					return ResultadoCertificado{}, err
				}
				return avisarCambioCertificado(t, in.ConectorID, f, *anterior)
			case err != nil && !errors.Is(err, pgx.ErrNoRows):
				return ResultadoCertificado{}, err
			}
			p := argosbd.RegistrarCertificadoParams{ConectorID: in.ConectorID, HuellaSha256: in.Visto.HuellaSHA256, Emisor: in.Visto.Emisor,
				Sujeto: in.Visto.Sujeto, Host: in.Visto.Host, VistoEn: ahora, Cambiado: anterior != nil}
			if !in.Visto.VenceEn.IsZero() {
				p.VenceEn = &in.Visto.VenceEn
			}
			if anterior != nil {
				p.AnteriorID = &anterior.ID
			}
			f, err := q.RegistrarCertificado(ctx, p)
			if err != nil {
				return ResultadoCertificado{}, err
			}
			t.Anotar(map[string]any{"huella_sha256": f.HuellaSha256, "emisor": f.Emisor, "sujeto": f.Sujeto, "host": f.Host})
			if anterior == nil {
				return ResultadoCertificado{Nuevo: true}, nil
			}
			return avisarCambioCertificado(t, in.ConectorID, f, *anterior)
		},
	})
}

// avisarCambioCertificado anota y emite argos.certificado_cambiado (solo informa; nunca bloquea).
func avisarCambioCertificado(t *auditoria.Tx, conectorID int64, f, anterior argosbd.IntegracionArgosCertificado) (ResultadoCertificado, error) {
	t.Anotar(map[string]any{"huella_sha256": f.HuellaSha256, "huella_anterior": anterior.HuellaSha256, "host": f.Host})
	return ResultadoCertificado{Nuevo: true, Cambiado: true}, t.Emitir(EventoCertificadoCambiado, map[string]any{
		"conector_id": conectorID, "certificado_id": f.ID, "anterior_id": anterior.ID, "host": f.Host,
		"huella_sha256": f.HuellaSha256, "huella_anterior": anterior.HuellaSha256, "emisor": f.Emisor,
	})
}

// registrarCertificadoVisto: lo que capturó ConCapturaTLS (si hubo HTTPS). Un error aquí no corta
// nada (es solo informativo): queda en el log.
func (m *Modulo) registrarCertificadoVisto(ctx context.Context, conectorID int64, visto func() *conector.CertificadoVisto) {
	v := visto()
	if v == nil {
		return
	}
	if _, err := m.RegistrarCertificado.Ejecutar(ctx, EntradaCertificado{ConectorID: conectorID, Visto: *v}); err != nil {
		slog.WarnContext(ctx, "argos: no se pudo registrar el certificado TLS de Argos (solo informativo)", "conector", conectorID, "error", err)
	}
}

// Rutas -----------------------------------------------------------------------------------------

func (m *Modulo) registrarEstado(api huma.API) {
	op := conector.Operacion(conector.EtiquetaIntegraciones, "estadoConexionArgos", http.MethodGet, "/api/v1/integraciones/argosisp/estado",
		"Estado de la conexión con Argos (pantalla de estado)", nucleo.PermIntegracionesVer, http.StatusOK)
	op.Description += " Contrato 0.8.0 §2.5 (d): el conector (activo o el más reciente) y quién lo activó, el contrato de cada lado con las funciones que quedan " +
		"apagadas por la versión de Argos, la primera sincronización por recurso, las instantáneas (cuántos y el último `sincronizadoEn`), " +
		"lo que falló (webhooks con error, rechazados y pendientes hacia Argos), las empresas del alcance y el certificado TLS de la API de Argos " +
		"(solo informa: si cambió, `cambiado` y `anterior`). Nunca tokens ni secretos. Solo lee la base de Iris: no llama a Argos."
	huma.Register(api, op, func(ctx context.Context, _ *struct{}) (*conector.Cuerpo[EstadoConexionArgos], error) {
		return conector.Responder(m.EstadoConexion.Ejecutar(ctx, struct{}{}))
	})

	sinc := conector.Operacion(conector.EtiquetaIntegraciones, "sincronizarConectorArgos", http.MethodPost, "/api/v1/integraciones/conectores/{id}/sincronizar",
		"Reanudar o volver a lanzar la primera sincronización con Argos", nucleo.PermIntegracionesSincronizar, http.StatusAccepted, http.StatusConflict,
		http.StatusNotFound, http.StatusServiceUnavailable)
	sinc.Description += " Contrato 0.8.0 §2.5. Con una en curso, la reanuda desde el último cursor; con una `con_errores`, la reabre (mismo id); si ya terminó " +
		"completa (o Argos cambió de contrato), lanza otra (409 `sincronizacion_reciente` si la última terminó completa hace menos de 10 minutos). Solo lee de Argos. Responde **202** con la sincronización; el avance llega en " +
		"`GET /integraciones/argosisp/estado`. Acepta `Idempotency-Key`. 422 `conector_apagado` si no está activo y `sistema_sin_sincronizacion` si no es de ArgosISP; 404 si no existe."
	huma.Register(api, sinc, func(ctx context.Context, in *struct {
		nucleo.ConIdempotencia
		ID int64 `path:"id" minimum:"1"`
	}) (*conector.Cuerpo[Sincronizacion], error) {
		return conector.Responder(m.Sincronizar.Ejecutar(ctx, EntradaSincronizar{ID: in.ID}))
	})
}
