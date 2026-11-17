package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Tablero, umbrales y modelos del servidor de IA (contratos/voz.md §4.12). Nunca lleva texto de
// prompts ni de respuestas: solo cuentas, tiempos y lecturas del servidor ya validadas.

// EntradaModelosMotor es POST /voz/motores/{id}/modelos/actualizar.
type EntradaModelosMotor struct {
	ID      int64    `json:"-"`
	Version int32    `json:"version" minimum:"1"`
	Modelos []string `json:"modelos" nullable:"true" maxItems:"256" doc:"De catalogo.llm, los que las empresas pueden elegir; null = todos"`
}

// UmbralesIA: cuándo avisa el servidor de IA y cuánto se guardan sus series.
type UmbralesIA struct {
	Version              int32 `json:"version" doc:"0 si todavía no hay servidor"`
	VramPct              int32 `json:"vramPct" doc:"Memoria de video por encima de este porcentaje (50 a 100; 95)"`
	VramMinutos          int32 `json:"vramMinutos" doc:"…durante estos minutos (1 a 60; 5)"`
	DiscoPct             int32 `json:"discoPct" doc:"Disco de los modelos por encima de este porcentaje (50 a 99; 90)"`
	TemperaturaC         int32 `json:"temperaturaC" doc:"Temperatura de la GPU (60 a 100; 85)"`
	TemperaturaMinutos   int32 `json:"temperaturaMinutos" doc:"…durante estos minutos (1 a 60; 5)"`
	SaturacionRechazos   int32 `json:"saturacionRechazos" doc:"Peticiones rechazadas por falta de plaza (1 a 1000; 10)"`
	SaturacionMinutos    int32 `json:"saturacionMinutos" doc:"…en estos minutos (1 a 60; 5)"`
	RetencionDetalleDias int32 `json:"retencionDetalleDias" doc:"Días que se guardan las muestras en detalle (1 a 30; 7)"`
	RetencionHoraDias    int32 `json:"retencionHoraDias" doc:"Días que se guardan los agregados por hora (7 a 365; 90)"`
}

// EntradaUmbralesIA es POST /servidor-ia/umbrales/actualizar.
type EntradaUmbralesIA struct {
	Version              int32  `json:"version" minimum:"1"`
	VramPct              int32  `json:"vramPct" minimum:"50" maximum:"100"`
	VramMinutos          int32  `json:"vramMinutos" minimum:"1" maximum:"60"`
	DiscoPct             int32  `json:"discoPct" minimum:"50" maximum:"99"`
	TemperaturaC         int32  `json:"temperaturaC" minimum:"60" maximum:"100"`
	TemperaturaMinutos   int32  `json:"temperaturaMinutos" minimum:"1" maximum:"60"`
	SaturacionRechazos   int32  `json:"saturacionRechazos" minimum:"1" maximum:"1000"`
	SaturacionMinutos    int32  `json:"saturacionMinutos" minimum:"1" maximum:"60"`
	RetencionDetalleDias *int32 `json:"retencionDetalleDias,omitempty" minimum:"1" maximum:"30"`
	RetencionHoraDias    *int32 `json:"retencionHoraDias,omitempty" minimum:"7" maximum:"365"`
}

func aUmbrales(f mensajeriabd.MensajeriaVozMotor) UmbralesIA {
	return UmbralesIA{Version: f.UmbralesVersion, VramPct: f.VramPct, VramMinutos: f.VramMinutos, DiscoPct: f.DiscoPct,
		TemperaturaC: f.TemperaturaC, TemperaturaMinutos: f.TemperaturaMinutos, SaturacionRechazos: f.SaturacionRechazos,
		SaturacionMinutos: f.SaturacionMinutos, RetencionDetalleDias: f.RetencionDetalleDias, RetencionHoraDias: f.RetencionHoraDias}
}

// EntradaTableroIA es GET /servidor-ia/tablero.
type EntradaTableroIA struct {
	Rango string `query:"rango" enum:"ahora,1h,24h,7d" default:"1h"`
}

// EstadoTableroIA: el estado general (lo ve también el admin de empresa).
type EstadoTableroIA struct {
	MotorID            *int64     `json:"motorId" nullable:"true" doc:"null: no hay ningún servidor enlazado"`
	Estado             *string    `json:"estado" nullable:"true" enum:"sin_enlazar,listo,degradado,no_disponible,incompatible,desactivado"`
	Detalle            *string    `json:"detalle" nullable:"true"`
	Version            *string    `json:"version" nullable:"true"`
	Compatible         *bool      `json:"compatible" nullable:"true"`
	MotivoIncompatible *string    `json:"motivoIncompatible" nullable:"true" enum:"protocolo,iris_antiguo,iris_nuevo,paquete_antiguo"`
	EncendidoSeg       *int64     `json:"encendidoSeg" nullable:"true"`
	TomadoEn           *time.Time `json:"tomadoEn" nullable:"true" doc:"Cuándo se tomó la última muestra que sí se pudo leer"`
	LecturaOk          bool       `json:"lecturaOk" doc:"false: la última lectura falló; lo de servidor no es de ahora y no se muestra como actual"`
	ModelosDisponibles []string   `json:"modelosDisponibles"`
	// Solo para quien ve todo.
	EmpresasQueLoUsan   []string `json:"empresasQueLoUsan,omitempty" doc:"Solo con servidor_ia.ver"`
	EmpresasActivas     *int     `json:"empresasActivas,omitempty" doc:"Solo con servidor_ia.ver"`
	PlazasInsuficientes *bool    `json:"plazasInsuficientes,omitempty" doc:"Solo con servidor_ia.ver: hay más empresas activas que plazas"`
}

// GPUTablero es una tarjeta gráfica (un dato que la tarjeta no da es null).
type GPUTablero struct {
	Indice       int      `json:"indice"`
	Nombre       string   `json:"nombre"`
	UsoPct       *float64 `json:"usoPct" nullable:"true"`
	VRAMUsadaMb  *float64 `json:"vramUsadaMb" nullable:"true"`
	VRAMTotalMb  *float64 `json:"vramTotalMb" nullable:"true"`
	TemperaturaC *float64 `json:"temperaturaC" nullable:"true"`
	PotenciaW    *float64 `json:"potenciaW" nullable:"true"`
}

// GPUsTablero: siempre un objeto.
type GPUsTablero struct {
	Estado string       `json:"estado" enum:"ok,sin_gpu,omitido" doc:"ok: hay tarjetas en lista. sin_gpu: el servidor respondió y no tiene tarjeta. omitido: no hubo lectura de la GPU (no se puede afirmar nada)"`
	Lista  []GPUTablero `json:"lista"`
}

// UsoRecurso en megas.
type UsoRecurso struct {
	UsadoMb int64 `json:"usadoMb"`
	TotalMb int64 `json:"totalMb"`
}

// DiscoTablero: el volumen de modelos (null si no hay lectura).
type DiscoTablero struct {
	_       struct{} `nullable:"true"`
	UsadoMb int64    `json:"usadoMb"`
	TotalMb int64    `json:"totalMb"`
}

// CPUTablero del servidor.
type CPUTablero struct {
	Nucleos int      `json:"nucleos"`
	UsoPct  *float64 `json:"usoPct" nullable:"true"`
}

// ModeloTablero es un modelo instalado en el servidor.
type ModeloTablero struct {
	ID           string     `json:"id"`
	TamanoMb     int64      `json:"tamanoMb"`
	Permitido    bool       `json:"permitido"`
	Cargado      bool       `json:"cargado"`
	VRAMMb       int64      `json:"vramMb"`
	CargadoHasta *time.Time `json:"cargadoHasta" nullable:"true"`
}

// OcupacionIA: peticiones al modelo (no hay cola).
type OcupacionIA struct {
	EnCurso        int64 `json:"enCurso"`
	MaxSimultaneas int32 `json:"maxSimultaneas"`
}

// ServidorTablero: la última muestra buena del servidor (solo con servidor_ia.ver).
type ServidorTablero struct {
	_          struct{}        `nullable:"true"`
	GPUs       GPUsTablero     `json:"gpus"`
	CPU        CPUTablero      `json:"cpu"`
	Memoria    UsoRecurso      `json:"memoria"`
	Disco      *DiscoTablero   `json:"disco"`
	Modelos    []ModeloTablero `json:"modelos"`
	LLM        OcupacionIA     `json:"llm"`
	Rechazadas int64           `json:"rechazadas" doc:"Rechazadas por saturación (servidor_ia_ocupado) en el rango"`
}

// PuntoSerieIA es un punto de las series; un valor null = sin lectura en ese instante.
type PuntoSerieIA struct {
	T               time.Time `json:"t"`
	UsoPct          *float64  `json:"usoPct" nullable:"true"`
	VRAMPct         *float64  `json:"vramPct" nullable:"true"`
	PeticionesMin   *float64  `json:"peticionesMin" nullable:"true"`
	LatenciaMediaMs *float64  `json:"latenciaMediaMs" nullable:"true"`
	LatenciaP95Ms   *float64  `json:"latenciaP95Ms" nullable:"true"`
}

// UsoEmpresaIA: el uso de una empresa en el rango.
type UsoEmpresaIA struct {
	EmpresaID       int64    `json:"empresaId"`
	Nombre          string   `json:"nombre"`
	Peticiones      int64    `json:"peticiones"`
	TokensEntrada   int64    `json:"tokensEntrada"`
	TokensSalida    int64    `json:"tokensSalida"`
	LatenciaMediaMs *float64 `json:"latenciaMediaMs" nullable:"true"`
	Errores         int64    `json:"errores"`
	Rechazadas      int64    `json:"rechazadas"`
}

// ErrorRecienteIA: errores del rango por motivo.
type ErrorRecienteIA struct {
	Motivo   string    `json:"motivo" enum:"tiempo_agotado,no_disponible,ocupado,respuesta_invalida,modelo_no_instalado,otro"`
	Cuenta   int64     `json:"cuenta"`
	UltimoEn time.Time `json:"ultimoEn"`
}

// UsoTableroIA: uso del rango.
type UsoTableroIA struct {
	_        struct{}          `nullable:"true"`
	Empresas []UsoEmpresaIA    `json:"empresas"`
	Errores  []ErrorRecienteIA `json:"errores"`
}

// AvisoServidorIA: un episodio de aviso del servidor.
type AvisoServidorIA struct {
	Tipo   string     `json:"tipo" enum:"caido,incompatible,vram,disco,temperatura,saturacion"`
	Activo bool       `json:"activo"`
	Desde  time.Time  `json:"desde"`
	Hasta  *time.Time `json:"hasta" nullable:"true"`
	Valor  *float64   `json:"valor" nullable:"true"`
	Umbral *float64   `json:"umbral" nullable:"true"`
}

// TableroIA es GET /servidor-ia/tablero.
type TableroIA struct {
	Estado   EstadoTableroIA   `json:"estado"`
	Servidor *ServidorTablero  `json:"servidor" doc:"Solo con servidor_ia.ver; null para el admin de empresa o si nunca hubo lectura"`
	Series   []PuntoSerieIA    `json:"series" nullable:"true"`
	Uso      *UsoTableroIA     `json:"uso"`
	Avisos   []AvisoServidorIA `json:"avisos"`
}

var motivosErrorIA = []string{"tiempo_agotado", "no_disponible", "ocupado", "respuesta_invalida", "modelo_no_instalado", "otro"}

func nuevosCasosTablero(m *Modulo, k *CasosServidorIA) {
	motor := func(ctx context.Context, q *mensajeriabd.Queries) (*mensajeriabd.MensajeriaVozMotor, error) {
		f, err := q.MotorIAUnicoParaActualizar(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return &f, err
	}

	k.Modelos = auditoria.Nuevo(auditoria.Caso[EntradaModelosMotor, MotorIA]{
		Accion: "servidor_ia.modelos", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaModelosMotor) (MotorIA, error) {
			if err := soloInstalacion(ctx); err != nil {
				return MotorIA{}, err
			}
			t.FijarEmpresa(0)
			q := mensajeriabd.New(t)
			f, err := q.MotorIAParaActualizar(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return MotorIA{}, nucleo.NoEncontrado("El servidor de IA no existe")
			}
			if err != nil {
				return MotorIA{}, err
			}
			if f.Version != in.Version {
				return MotorIA{}, nucleo.ConflictoVersion("voz_motor", f.ID)
			}
			var modelos []string
			if in.Modelos != nil {
				modelos = slices.Clone(in.Modelos)
				slices.Sort(modelos)
				modelos = slices.Compact(modelos)
				if modelos == nil {
					modelos = []string{}
				}
				// Iris no puede ampliar lo que deja usar el administrador del servidor.
				cat := catalogoDe(f).LLM
				for _, id := range modelos {
					if !slices.Contains(cat, id) {
						return MotorIA{}, nucleo.NuevoProblema(422, "modelo_no_instalado", "Modelo no disponible",
							"Ese modelo no está entre los que el servidor de IA deja usar", nucleo.ErrorCampo{Field: "modelos", Message: id, Rule: "modelo_no_instalado"})
					}
				}
			}
			antes := map[string]any{"modelos": f.ModelosPermitidos}
			if f, err = q.ActualizarModelosMotorIA(ctx, mensajeriabd.ActualizarModelosMotorIAParams{ID: f.ID, ModelosPermitidos: modelos}); err != nil {
				return MotorIA{}, err
			}
			t.FijarEntidad("voz_motor", f.ID)
			t.RegistrarCambios("voz_motor", f.ID, antes, map[string]any{"modelos": f.ModelosPermitidos})
			return aMotorIA(f, 0), t.EmitirEnEmpresa("voz.motor_modelos_cambiados", nil, map[string]any{"motor_id": f.ID,
				"usuario_id": contexto.ActorDe(ctx).ID, "modelos": f.ModelosPermitidos})
		},
	})

	k.Umbrales = auditoria.Nuevo(auditoria.Caso[struct{}, UmbralesIA]{
		Accion: "servidor_ia.ver", Permiso: nucleo.PermServidorIAVer, SinEmpresa: true, AuditarExito: sinAuditar[UmbralesIA],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (UmbralesIA, error) {
			if err := soloInstalacion(ctx); err != nil {
				return UmbralesIA{}, err
			}
			f, err := motor(ctx, mensajeriabd.New(t))
			if err != nil {
				return UmbralesIA{}, err
			}
			if f == nil {
				// Los de fábrica (los mismos DEFAULT de la tabla).
				return UmbralesIA{VramPct: 95, VramMinutos: 5, DiscoPct: 90, TemperaturaC: 85, TemperaturaMinutos: 5,
					SaturacionRechazos: 10, SaturacionMinutos: 5, RetencionDetalleDias: 7, RetencionHoraDias: 90}, nil
			}
			return aUmbrales(*f), nil
		},
	})

	k.FijarUmbrales = auditoria.Nuevo(auditoria.Caso[EntradaUmbralesIA, UmbralesIA]{
		Accion: "servidor_ia.umbrales", Permiso: nucleo.PermServidorIAAdministrar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaUmbralesIA) (UmbralesIA, error) {
			if err := soloInstalacion(ctx); err != nil {
				return UmbralesIA{}, err
			}
			t.FijarEmpresa(0)
			q := mensajeriabd.New(t)
			f, err := motor(ctx, q)
			if err != nil {
				return UmbralesIA{}, err
			}
			if f == nil {
				return UmbralesIA{}, nucleo.NoEncontrado("Todavía no hay un servidor de IA")
			}
			if f.UmbralesVersion != in.Version {
				return UmbralesIA{}, nucleo.ConflictoVersion("servidor_ia_umbrales", f.ID)
			}
			en := func(v, lo, hi int32) bool { return v >= lo && v <= hi }
			detalle, hora := f.RetencionDetalleDias, f.RetencionHoraDias
			if in.RetencionDetalleDias != nil {
				detalle = *in.RetencionDetalleDias
			}
			if in.RetencionHoraDias != nil {
				hora = *in.RetencionHoraDias
			}
			switch {
			case !en(in.VramPct, 50, 100):
				return UmbralesIA{}, campoConv("vramPct", "Entre 50 y 100", "rango")
			case !en(in.VramMinutos, 1, 60):
				return UmbralesIA{}, campoConv("vramMinutos", "Entre 1 y 60", "rango")
			case !en(in.DiscoPct, 50, 99):
				return UmbralesIA{}, campoConv("discoPct", "Entre 50 y 99", "rango")
			case !en(in.TemperaturaC, 60, 100):
				return UmbralesIA{}, campoConv("temperaturaC", "Entre 60 y 100", "rango")
			case !en(in.TemperaturaMinutos, 1, 60):
				return UmbralesIA{}, campoConv("temperaturaMinutos", "Entre 1 y 60", "rango")
			case !en(in.SaturacionRechazos, 1, 1000):
				return UmbralesIA{}, campoConv("saturacionRechazos", "Entre 1 y 1000", "rango")
			case !en(in.SaturacionMinutos, 1, 60):
				return UmbralesIA{}, campoConv("saturacionMinutos", "Entre 1 y 60", "rango")
			case !en(detalle, 1, 30):
				return UmbralesIA{}, campoConv("retencionDetalleDias", "Entre 1 y 30", "rango")
			case !en(hora, 7, 365):
				return UmbralesIA{}, campoConv("retencionHoraDias", "Entre 7 y 365", "rango")
			}
			antes := aUmbrales(*f)
			f2, err := q.ActualizarUmbralesMotorIA(ctx, mensajeriabd.ActualizarUmbralesMotorIAParams{ID: f.ID, VramPct: in.VramPct, VramMinutos: in.VramMinutos,
				DiscoPct: in.DiscoPct, TemperaturaC: in.TemperaturaC, TemperaturaMinutos: in.TemperaturaMinutos, SaturacionRechazos: in.SaturacionRechazos,
				SaturacionMinutos: in.SaturacionMinutos, RetencionDetalleDias: detalle, RetencionHoraDias: hora})
			if err != nil {
				return UmbralesIA{}, err
			}
			t.FijarEntidad("voz_motor", f.ID)
			campos := t.RegistrarCambios("servidor_ia_umbrales", f.ID, antes, aUmbrales(f2))
			if campos == nil {
				campos = []string{}
			}
			return aUmbrales(f2), t.EmitirEnEmpresa("servidor_ia.ajustes_cambiados", nil, map[string]any{"motor_id": f.ID, "campos": campos})
		},
	})

	k.Tablero = auditoria.Nuevo(auditoria.Caso[EntradaTableroIA, TableroIA]{
		Accion: "servidor_ia.tablero", Acceso: auditoria.Autenticado, SinEmpresa: true, AuditarExito: sinAuditar[TableroIA],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTableroIA) (TableroIA, error) {
			actor, perms := contexto.ActorDe(ctx), contexto.PermisosDe(ctx)
			if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
				return TableroIA{}, nucleo.NoEncontrado("No existe")
			}
			todo := perms.Tiene(string(nucleo.PermServidorIAVer)) && perms.TieneGlobal(string(nucleo.PermEmpresasTodas))
			var empresa int64 = -1
			if !todo {
				// El admin de empresa: el estado general y solo lo de su empresa activa.
				if !perms.Tiene(string(nucleo.PermServidorIAVerUso)) || contexto.EmpresasDe(ctx).Estado != contexto.EmpresaActiva {
					return TableroIA{}, nucleo.NoEncontrado("No existe")
				}
				empresa = contexto.EmpresaDe(ctx)
			}
			r := TableroIA{Estado: EstadoTableroIA{ModelosDisponibles: []string{}}, Avisos: []AvisoServidorIA{}}
			q := mensajeriabd.New(t)
			f, err := q.MotorIA(ctx)
			if errors.Is(err, pgx.ErrNoRows) {
				return r, nil
			}
			if err != nil {
				return r, err
			}
			if !todo && !empresaPermitida(f, empresa) {
				return r, nil // para esa empresa es como si no hubiera servidor
			}
			r.Estado.MotorID, r.Estado.Estado, r.Estado.Version = &f.ID, &f.Estado, f.VersionMotor
			if todo {
				// El detalle puede traer el motivo libre que escribió el admin de la instalación: el
				// admin de empresa solo ve el estado (lista cerrada).
				r.Estado.Detalle = f.Detalle
			}
			r.Estado.Compatible, r.Estado.MotivoIncompatible = &f.Compatible, f.MotivoIncompatible
			r.Estado.ModelosDisponibles = modelosUtilizables(f)
			// lecturaOk: la última muestra es buena y reciente. tomadoEn: la última que sí se pudo leer.
			var buena *muestraGuardada
			if u, err := q.UltimaMuestraIA(ctx, f.ID); err == nil {
				r.Estado.LecturaOk = u.Ok && m.ahora().Sub(u.TomadaEn) <= vidaLecturaIA && f.Estado != MotorDesactivado
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			if b, err := q.UltimaMuestraBuenaIA(ctx, f.ID); err == nil {
				var g muestraGuardada
				if json.Unmarshal(b.Datos, &g) == nil {
					buena = &g
					r.Estado.TomadoEn, r.Estado.EncendidoSeg = &b.TomadaEn, &g.EncendidoSeg
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return r, err
			}
			ahora := m.ahora()
			series, desde, err := m.seriesIA(ctx, q, f.ID, in.Rango, empresa, todo)
			if err != nil {
				return r, err
			}
			desde = desde.Truncate(time.Minute)
			filas, err := q.UsoIAEnRango(ctx, mensajeriabd.UsoIAEnRangoParams{MotorID: f.ID, Desde: desde, EmpresaID: empresa})
			if err != nil {
				return r, err
			}
			uso := &UsoTableroIA{Empresas: []UsoEmpresaIA{}, Errores: []ErrorRecienteIA{}}
			var rechazadas int64
			for _, u := range filas {
				e := UsoEmpresaIA{EmpresaID: u.EmpresaID, Nombre: u.Nombre, Peticiones: u.Peticiones, TokensEntrada: u.TokensEntrada,
					TokensSalida: u.TokensSalida, Errores: u.Errores, Rechazadas: u.Rechazos}
				if u.Peticiones > 0 {
					media := float64(u.LatenciaMsSuma) / float64(u.Peticiones)
					e.LatenciaMediaMs = &media
				}
				rechazadas += u.Rechazos
				uso.Empresas = append(uso.Empresas, e)
			}
			errs, err := q.ErroresIAEnRango(ctx, mensajeriabd.ErroresIAEnRangoParams{MotorID: f.ID, Desde: desde, EmpresaID: empresa})
			if err != nil {
				return r, err
			}
			for _, x := range errs {
				motivo := x.Motivo
				if !slices.Contains(motivosErrorIA, motivo) {
					motivo = "otro"
				}
				uso.Errores = append(uso.Errores, ErrorRecienteIA{Motivo: motivo, Cuenta: x.Cuenta, UltimoEn: x.UltimoEn})
			}
			r.Uso, r.Series = uso, series
			if !todo {
				return r, nil
			}
			activas, err := q.EmpresasActivasIA(ctx)
			if err != nil {
				return r, err
			}
			n := 0
			r.Estado.EmpresasQueLoUsan = []string{}
			for _, a := range activas {
				if empresaPermitida(f, a.EmpresaID) {
					n++
					r.Estado.EmpresasQueLoUsan = append(r.Estado.EmpresasQueLoUsan, a.Nombre)
				}
			}
			insuf := f.ActivadoEn != nil && f.MaxSimultaneas > 0 && n > int(f.MaxSimultaneas)
			r.Estado.EmpresasActivas, r.Estado.PlazasInsuficientes = &n, &insuf
			enCurso, err := q.PlazasEnCursoIA(ctx, mensajeriabd.PlazasEnCursoIAParams{MotorID: f.ID, Ahora: ahora})
			if err != nil {
				return r, err
			}
			if buena != nil {
				sv := buena.Servidor
				// La ocupación y los rechazos los lleva Iris; lo demás es la última lectura buena.
				sv.LLM, sv.Rechazadas = OcupacionIA{EnCurso: enCurso, MaxSimultaneas: f.MaxSimultaneas}, rechazadas
				r.Servidor = &sv
			}
			avisos, err := q.AvisosIAEnRango(ctx, mensajeriabd.AvisosIAEnRangoParams{MotorID: f.ID, Desde: &desde})
			if err != nil {
				return r, err
			}
			for _, a := range avisos {
				av := AvisoServidorIA{Tipo: a.Tipo, Activo: a.Hasta == nil, Desde: a.Desde, Hasta: a.Hasta}
				if a.Valor != nil {
					v := float64(*a.Valor)
					av.Valor = &v
				}
				if a.Umbral != nil {
					v := float64(*a.Umbral)
					av.Umbral = &v
				}
				r.Avisos = append(r.Avisos, av)
			}
			return r, nil
		},
	})
}
