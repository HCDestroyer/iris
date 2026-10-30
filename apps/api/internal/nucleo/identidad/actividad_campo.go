package identidad

import (
	"context"
	"log/slog"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Actividad de la app de campo por técnico: última conexión (X-Iris-App, último lote o última
// posición), último POST /sync/lote, operaciones rechazadas que siguen así, pendientes que informa
// la app (X-Iris-Pendientes), versión y plataforma, y última posición GPS. Es solo lectura: junta
// lo que ya guardan campo.dispositivos, campo.operaciones_sync, campo.posiciones_ultimas y
// nucleo.conexiones_app.

// Umbrales del indicador (los publica la respuesta, para que el panel los muestre).
const (
	// UmbralSinConexion: sin ninguna conexión en este tiempo, el técnico está «sin conexión».
	UmbralSinConexion = 30 * time.Minute
	// UmbralAtraso: con pendientes en la app y sin un lote en este tiempo, está «atrasado».
	UmbralAtraso = 15 * time.Minute
	// VentanaRechazos: se cuentan las operaciones rechazadas de este tiempo.
	VentanaRechazos = 7 * 24 * time.Hour
	// limiteTecnicos: tope de la lista (una empresa no tiene más técnicos activos que esto).
	limiteTecnicos = 1000
)

// Estados del indicador.
const (
	EstadoAlDia       = "al_dia"
	EstadoAtrasado    = "atrasado"
	EstadoSinConexion = "sin_conexion"
)

// registrarConexionApp guarda la última conexión de la app (X-Iris-App) del usuario. Escribe
// como mucho cada 30 s por usuario y plataforma; nunca falla la petición.
func registrarConexionApp(ctx context.Context, q *identidadbd.Queries, usuarioID int64) {
	c := ClienteHTTPDe(ctx)
	if c.App == "" {
		return
	}
	if err := q.RegistrarConexionApp(ctx, identidadbd.RegistrarConexionAppParams{
		UsuarioID: usuarioID, App: c.AppNombre, Plataforma: c.Plataforma, Version: c.Version, Pendientes: c.Pendientes,
		Ip: recortar(c.IP, 64), UserAgent: c.UserAgent,
	}); err != nil {
		slog.WarnContext(ctx, "identidad: registrar conexión de la app", "err", err)
	}
}

// EntradaActividadCampo filtra la lista por estado.
type EntradaActividadCampo struct {
	Estado string `query:"estado" enum:"al_dia,atrasado,sin_conexion" doc:"Solo los técnicos en este estado"`
}

// AppCampo es la app que usó el técnico en su última conexión.
type AppCampo struct {
	Version    string `json:"version"`
	Plataforma string `json:"plataforma" doc:"pwa, android, ios…"`
}

// PosicionCampo es la última ubicación que mandó el técnico.
type PosicionCampo struct {
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	PrecisionM   *float64  `json:"precisionM" nullable:"true"`
	RegistradaEn time.Time `json:"registradaEn" doc:"Hora del punto (corregida con el desfase del teléfono)"`
}

// ActividadTecnico es la fila de un técnico.
type ActividadTecnico struct {
	UsuarioID            int64          `json:"usuarioId"`
	FichaID              int64          `json:"fichaId" doc:"Id de la ficha de agenda (agenda.tecnicos)"`
	Nombre               string         `json:"nombre"`
	Correo               string         `json:"correo"`
	CuadrillaID          *int64         `json:"cuadrillaId" nullable:"true"`
	Estado               string         `json:"estado" enum:"al_dia,atrasado,sin_conexion"`
	UltimaConexion       *time.Time     `json:"ultimaConexion" nullable:"true" doc:"La más reciente entre la app, el último lote y la última posición; null si nunca se conectó"`
	UltimaSincronizacion *time.Time     `json:"ultimaSincronizacion" nullable:"true" doc:"Último POST /sync/lote"`
	Pendientes           *int32         `json:"pendientes" nullable:"true" doc:"Operaciones sin enviar que informó la app (X-Iris-Pendientes); null si la app no lo informa"`
	PendientesEn         *time.Time     `json:"pendientesEn" nullable:"true"`
	Rechazadas           int32          `json:"rechazadas" doc:"Operaciones del lote rechazadas en los últimos 7 días que siguen así"`
	UltimoRechazo        *time.Time     `json:"ultimoRechazo" nullable:"true"`
	App                  *AppCampo      `json:"app,omitempty" doc:"Ausente si la app no manda X-Iris-App (versión anterior)"`
	Plataforma           *string        `json:"plataforma" nullable:"true" doc:"La de X-Iris-App o, si no, la del último lote"`
	UltimaPosicion       *PosicionCampo `json:"ultimaPosicion,omitempty" doc:"Ausente si nunca mandó su ubicación"`
}

// ActividadCampo es la respuesta: la lista y los umbrales con que se calculó el estado.
type ActividadCampo struct {
	Data             []ActividadTecnico `json:"data"`
	NextCursor       *string            `json:"nextCursor" nullable:"true" doc:"Siempre null: la lista viene completa"`
	GeneradoEn       time.Time          `json:"generadoEn"`
	SinConexionMin   int                `json:"sinConexionMin" doc:"Minutos sin conexión para «sin conexión»"`
	AtrasoMin        int                `json:"atrasoMin" doc:"Minutos sin sincronizar con pendientes para «atrasado»"`
	VentanaRechazosD int                `json:"ventanaRechazosDias"`
}

// EstadoActividad calcula el indicador de un técnico.
func EstadoActividad(ahora time.Time, ultimaConexion, ultimaSync *time.Time, pendientes *int32) string {
	if ultimaConexion == nil || ahora.Sub(*ultimaConexion) > UmbralSinConexion {
		return EstadoSinConexion
	}
	if pendientes != nil && *pendientes > 0 && (ultimaSync == nil || ahora.Sub(*ultimaSync) > UmbralAtraso) {
		return EstadoAtrasado
	}
	return EstadoAlDia
}

// filtroEmpresaActividad: la empresa activa (todas = sin filtro; nil = modo sin conector).
func filtroEmpresaActividad(ctx context.Context) (todas bool, id *int64) {
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
	case contexto.EmpresaSinConector:
		return false, nil
	case contexto.EmpresaNoResuelta:
		if !contexto.ActorDe(ctx).EsUsuario() {
			return true, nil
		}
	case contexto.EmpresaPorElegir, contexto.EmpresaSinAsignar:
	}
	cero := int64(0)
	return false, &cero
}

func mayor(ts ...*time.Time) *time.Time {
	var m *time.Time
	for _, t := range ts {
		if t != nil && (m == nil || t.After(*m)) {
			m = t
		}
	}
	return m
}

func nuevoActividadCampo() *auditoria.Caso[EntradaActividadCampo, ActividadCampo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaActividadCampo, ActividadCampo]{
		Accion:       "campo.ver_actividad",
		Permiso:      nucleo.PermCampoVerActividad,
		AuditarExito: soloLecturas[ActividadCampo],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActividadCampo) (ActividadCampo, error) {
			q := identidadbd.New(t)
			todas, empresa := filtroEmpresaActividad(ctx)
			global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermCampoVerActividad))
			tecnicos, err := q.TecnicosVisibles(ctx, identidadbd.TecnicosVisiblesParams{
				Todas: todas, EmpresaID: empresa, AlcanceGlobal: global, Cuadrillas: lista(cuadrillas), Zonas: lista(zonas), Limite: limiteTecnicos,
			})
			if err != nil {
				return ActividadCampo{}, err
			}
			ids := make([]int64, 0, len(tecnicos))
			for _, f := range tecnicos {
				ids = append(ids, f.UsuarioID)
			}
			conexiones, err := q.ConexionesAppDe(ctx, ids)
			if err != nil {
				return ActividadCampo{}, err
			}
			lotes, err := q.UltimosLotesDe(ctx, ids)
			if err != nil {
				return ActividadCampo{}, err
			}
			rechazos, err := q.RechazosSyncDe(ctx, identidadbd.RechazosSyncDeParams{Usuarios: ids, VentanaSeg: int64(VentanaRechazos / time.Second)})
			if err != nil {
				return ActividadCampo{}, err
			}
			posiciones, err := q.UltimasPosicionesDe(ctx, identidadbd.UltimasPosicionesDeParams{Usuarios: ids, Todas: todas, EmpresaID: empresa})
			if err != nil {
				return ActividadCampo{}, err
			}
			porConexion := map[int64]identidadbd.NucleoConexionesApp{}
			for _, c := range conexiones {
				porConexion[c.UsuarioID] = c
			}
			porLote := map[int64]identidadbd.UltimosLotesDeRow{}
			for _, l := range lotes {
				porLote[l.UsuarioID] = l
			}
			porRechazo := map[int64]identidadbd.RechazosSyncDeRow{}
			for _, r := range rechazos {
				porRechazo[r.UsuarioID] = r
			}
			porPosicion := map[int64]identidadbd.UltimasPosicionesDeRow{}
			for _, p := range posiciones {
				porPosicion[p.TecnicoID] = p
			}

			ahora := time.Now()
			salida := ActividadCampo{Data: []ActividadTecnico{}, GeneradoEn: ahora.UTC(),
				SinConexionMin: int(UmbralSinConexion / time.Minute), AtrasoMin: int(UmbralAtraso / time.Minute),
				VentanaRechazosD: int(VentanaRechazos / (24 * time.Hour))}
			for _, f := range tecnicos {
				a := ActividadTecnico{UsuarioID: f.UsuarioID, FichaID: f.FichaID, Nombre: f.Nombre, Correo: f.Correo, CuadrillaID: f.CuadrillaID}
				var conexionApp, recibida *time.Time
				if c, ok := porConexion[f.UsuarioID]; ok {
					conexionApp = &c.UltimaConexion
					a.App = &AppCampo{Version: c.Version, Plataforma: c.Plataforma}
					a.Plataforma = &c.Plataforma
					a.Pendientes, a.PendientesEn = c.Pendientes, c.PendientesEn
				}
				if l, ok := porLote[f.UsuarioID]; ok {
					a.UltimaSincronizacion = &l.UltimoContacto
					if a.Plataforma == nil && l.Plataforma != "" {
						a.Plataforma = &l.Plataforma
					}
				}
				if r, ok := porRechazo[f.UsuarioID]; ok {
					a.Rechazadas, a.UltimoRechazo = r.Rechazadas, &r.Ultimo
				}
				if p, ok := porPosicion[f.UsuarioID]; ok {
					a.UltimaPosicion = &PosicionCampo{Lat: p.Lat, Lon: p.Lon, PrecisionM: p.PrecisionM, RegistradaEn: p.RegistradaEn}
					recibida = &p.RecibidaEn
				}
				a.UltimaConexion = mayor(conexionApp, a.UltimaSincronizacion, recibida)
				a.Estado = EstadoActividad(ahora, a.UltimaConexion, a.UltimaSincronizacion, a.Pendientes)
				if in.Estado == "" || in.Estado == a.Estado {
					salida.Data = append(salida.Data, a)
				}
			}
			return salida, nil
		},
	})
}
