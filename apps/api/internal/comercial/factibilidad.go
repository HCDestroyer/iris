package comercial

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Resultados de la factibilidad.
const (
	FactibleViable        = "viable"
	FactibleNoViable      = "no_viable"
	FactibleRequiereObra  = "requiere_obra"
	FactibleIndeterminado = "indeterminado"
)

// Motivos de una factibilidad indeterminada (no se inventa un resultado).
const (
	MotivoSinConector        = "sin_conector"
	MotivoConectorApagado    = "conector_apagado"
	MotivoSinCoordenadas     = "sin_coordenadas"
	MotivoSinEmpresa         = "sin_empresa"
	MotivoSinInstantanea     = "sin_instantanea"
	MotivoSinRoutersConGeo   = "sin_routers_con_coordenadas"
	MotivoPlantaNoDisponible = "planta_no_disponible"
)

// Evaluacion es lo que responde la planta externa.
type Evaluacion struct {
	Resultado    string
	Motor        string
	Motivo       string // solo si Resultado = indeterminado
	RouterID     *int64
	RouterNombre string
	Caja         string
	DistanciaM   *float64
	ViableM      *int32
	ObraM        *int32
}

// PlantaExterna evalúa si se puede dar servicio en una coordenada. La fase 6 la reemplaza por
// el adaptador de OZmap (viabilidad por coordenada, caja con puertos libres). Se llama FUERA de
// toda transacción (puede hablar con otro sistema); ctx trae la base (nucleo.BDDe), el actor y
// la empresa activa. Un error es «no se pudo evaluar»: el caso de uso lo guarda como
// indeterminado (planta_no_disponible), nunca como un resultado inventado.
type PlantaExterna interface {
	Evaluar(ctx context.Context, empresa *int64, lat, lon float64) (Evaluacion, error)
}

// PlantaPorDistancia: distancia en línea recta (haversine) al router más cercano de la
// instantánea de Argos de la empresa (integracion.argos_routers), con los umbrales de la
// empresa (comercial.umbrales_factibilidad): hasta viable_m, viable; hasta obra_m,
// requiere_obra; más lejos, no_viable. Sin conector, con el conector apagado, sin empresa o sin
// routers con coordenadas: indeterminado con el motivo.
type PlantaPorDistancia struct{}

// Evaluar implementa PlantaExterna.
func (PlantaPorDistancia) Evaluar(ctx context.Context, empresa *int64, lat, lon float64) (Evaluacion, error) {
	ev := Evaluacion{Motor: "distancia", Resultado: FactibleIndeterminado}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ev, nucleo.ErrSinBD
	}
	q := comercialbd.New(bd)
	estado, err := q.EstadoConectorArgos(ctx)
	if err != nil {
		return ev, err
	}
	switch {
	case estado == "":
		ev.Motivo = MotivoSinConector
		return ev, nil
	case estado != "activo":
		ev.Motivo = MotivoConectorApagado
		return ev, nil
	case empresa == nil:
		ev.Motivo = MotivoSinEmpresa
		return ev, nil
	}
	total, err := q.ContarRoutersDeEmpresa(ctx, *empresa)
	if err != nil {
		return ev, err
	}
	if total == 0 {
		ev.Motivo = MotivoSinInstantanea
		return ev, nil
	}
	routers, err := q.RoutersConCoordenadas(ctx, *empresa)
	if err != nil {
		return ev, err
	}
	if len(routers) == 0 {
		ev.Motivo = MotivoSinRoutersConGeo
		return ev, nil
	}
	u, err := q.UmbralesDeEmpresa(ctx, empresa)
	if err != nil {
		return ev, err
	}
	mejor := -1.0
	for _, r := range routers {
		d := Haversine(lat, lon, r.Lat, r.Lon)
		if mejor < 0 || d < mejor {
			mejor = d
			ev.RouterID, ev.RouterNombre = ptr(r.ID), r.Nombre
		}
	}
	mejor = math.Round(mejor*10) / 10
	ev.DistanciaM, ev.ViableM, ev.ObraM = &mejor, &u.ViableM, &u.ObraM
	switch {
	case mejor <= float64(u.ViableM):
		ev.Resultado = FactibleViable
	case mejor <= float64(u.ObraM):
		ev.Resultado = FactibleRequiereObra
	default:
		ev.Resultado = FactibleNoViable
	}
	ev.Motivo = ""
	return ev, nil
}

// Haversine: distancia en metros entre dos coordenadas.
func Haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const radio = 6_371_000.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * radio * math.Asin(math.Min(1, math.Sqrt(a)))
}

// EntradaManual: la factibilidad que registra un técnico tras una visita (o sin conector).
type EntradaManual struct {
	Resultado string  `json:"resultado" enum:"viable,no_viable,requiere_obra"`
	Caja      *string `json:"caja,omitempty" maxLength:"120"`
	RouterID  *int64  `json:"routerId,omitempty" minimum:"1"`
}

// EntradaFactibilidad evalúa la factibilidad del prospecto en sus coordenadas actuales.
type EntradaFactibilidad struct {
	ProspectoID int64          `json:"-"`
	Version     int32          `json:"version" minimum:"1" doc:"Versión del prospecto"`
	Manual      *EntradaManual `json:"manual,omitempty" doc:"Resultado de una visita técnica (motor manual; exige notas). Sin él, evalúa la planta externa"`
	Notas       string         `json:"notas,omitempty" maxLength:"2000"`
}

// previoFactibilidad: lo leído antes de llamar a la planta.
type previoFactibilidad struct {
	EmpresaID *int64   `json:"empresa_id"`
	Lat       *float64 `json:"lat"`
	Lon       *float64 `json:"lon"`
}

// entradaGuardarFactibilidad: la evaluación ya hecha.
type entradaGuardarFactibilidad struct {
	EntradaFactibilidad
	previo previoFactibilidad
	ev     Evaluacion
}

// MarshalJSON de la auditoría: la entrada y el resultado de la planta.
func (e entradaGuardarFactibilidad) MarshalJSON() ([]byte, error) {
	return jsonDe(map[string]any{"prospecto_id": e.ProspectoID, "version": e.Version, "manual": e.Manual, "notas": e.Notas,
		"resultado": e.ev.Resultado, "motor": e.ev.Motor, "motivo": e.ev.Motivo, "distancia_m": e.ev.DistanciaM, "router_id": e.ev.RouterID})
}

var estadosFactibilidad = []string{EstadoNuevo, EstadoContactado, EstadoFactibilidad, EstadoCotizado}

func (m *Modulo) nuevoPrepararFactibilidad() *auditoria.Caso[EntradaFactibilidad, previoFactibilidad] {
	return auditoria.Nuevo(auditoria.Caso[EntradaFactibilidad, previoFactibilidad]{
		Accion: "factibilidad.evaluar", Permiso: nucleo.PermFactibilidadEvaluar, AuditarExito: soloLecturas[previoFactibilidad],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaFactibilidad) (previoFactibilidad, error) {
			q := comercialbd.New(t)
			f, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermFactibilidadEvaluar, &in.Version)
			if err != nil {
				return previoFactibilidad{}, err
			}
			if err := exigirEstado(f, "evaluar la factibilidad de", estadosFactibilidad...); err != nil {
				return previoFactibilidad{}, err
			}
			return previoFactibilidad{EmpresaID: f.EmpresaID, Lat: f.Lat, Lon: f.Lon}, nil
		},
	})
}

// EvaluarFactibilidad: lee el prospecto, evalúa la planta fuera de la transacción (o toma el
// resultado manual) y guarda la evaluación (si el prospecto cambió entretanto, 409).
func (m *Modulo) EvaluarFactibilidad(ctx context.Context, in EntradaFactibilidad) (Factibilidad, error) {
	previo, err := m.prepararFactibilidad.Ejecutar(ctx, in)
	if err != nil {
		return Factibilidad{}, err
	}
	e := entradaGuardarFactibilidad{EntradaFactibilidad: in, previo: previo}
	switch {
	case in.Manual != nil:
		e.ev = Evaluacion{Motor: "manual", Resultado: in.Manual.Resultado, RouterID: in.Manual.RouterID, Caja: deref(in.Manual.Caja)}
	case previo.Lat == nil || previo.Lon == nil:
		e.ev = Evaluacion{Motor: "distancia", Resultado: FactibleIndeterminado, Motivo: MotivoSinCoordenadas}
	default:
		e.ev, err = m.cfg.Planta.Evaluar(ctx, previo.EmpresaID, *previo.Lat, *previo.Lon)
		if err != nil {
			e.ev = Evaluacion{Motor: e.ev.Motor, Resultado: FactibleIndeterminado, Motivo: MotivoPlantaNoDisponible}
			if e.ev.Motor == "" {
				e.ev.Motor = "planta"
			}
		}
	}
	return m.guardarFactibilidad.Ejecutar(ctx, e)
}

func (m *Modulo) nuevoGuardarFactibilidad() *auditoria.Caso[entradaGuardarFactibilidad, Factibilidad] {
	return auditoria.Nuevo(auditoria.Caso[entradaGuardarFactibilidad, Factibilidad]{
		Accion: "factibilidad.evaluar", Permiso: nucleo.PermFactibilidadEvaluar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarFactibilidad) (Factibilidad, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermFactibilidadEvaluar, &in.Version)
			if err != nil {
				return Factibilidad{}, err
			}
			if err := exigirEstado(antes, "evaluar la factibilidad de", estadosFactibilidad...); err != nil {
				return Factibilidad{}, err
			}
			notas, err := texto("notas", in.Notas, 0, 2000)
			if err != nil {
				return Factibilidad{}, err
			}
			ev := in.ev
			if in.Manual != nil {
				if !slices.Contains([]string{FactibleViable, FactibleNoViable, FactibleRequiereObra}, ev.Resultado) {
					return Factibilidad{}, campoInvalido("manual.resultado", "viable, no_viable o requiere_obra", "enum")
				}
				if len([]rune(notas)) < 5 {
					return Factibilidad{}, campoInvalido("notas", "Describe la visita o el criterio (al menos 5 caracteres)", "required")
				}
				if ev.Caja != "" {
					if ev.Caja, err = texto("manual.caja", ev.Caja, 1, 120); err != nil {
						return Factibilidad{}, err
					}
				}
			}
			d := comercialbd.CrearFactibilidadParams{
				EmpresaID: antes.EmpresaID, ProspectoID: antes.ID, Resultado: ev.Resultado, Motor: ev.Motor,
				Lat: antes.Lat, Lon: antes.Lon, RouterID: ev.RouterID, DistanciaM: ev.DistanciaM, ViableM: ev.ViableM,
				ObraM: ev.ObraM, Notas: notas, EvaluadaPor: actorUsuario(t),
			}
			if ev.Resultado == FactibleIndeterminado {
				motivo := ev.Motivo
				if motivo == "" {
					motivo = MotivoPlantaNoDisponible
				}
				d.Motivo = &motivo
			}
			if ev.RouterNombre != "" {
				d.RouterNombre = &ev.RouterNombre
			}
			if ev.Caja != "" {
				d.Caja = &ev.Caja
			}
			fa, err := q.CrearFactibilidad(ctx, d)
			if err != nil {
				return Factibilidad{}, err
			}
			despues := antes
			if antes.Estado == EstadoNuevo || antes.Estado == EstadoContactado {
				despues.Estado = EstadoFactibilidad
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return Factibilidad{}, err
			}
			t.RegistrarCambios("factibilidad", fa.ID, nil, fa)
			if err := historial(ctx, t, q, f.ID, "factibilidad", nil, &fa.Resultado, fa.Motivo, map[string]any{
				"factibilidad_id": fa.ID, "motor": fa.Motor, "distancia_m": fa.DistanciaM, "router_id": fa.RouterID,
			}); err != nil {
				return Factibilidad{}, err
			}
			if err := t.Emitir("factibilidad.evaluada", map[string]any{
				"prospecto_id": f.ID, "factibilidad_id": fa.ID, "resultado": fa.Resultado, "motor": fa.Motor,
				"motivo": fa.Motivo, "router_id": fa.RouterID, "caja": fa.Caja, "distancia_m": fa.DistanciaM,
			}); err != nil {
				return Factibilidad{}, err
			}
			return aFactibilidad(fa), cambioDeEstado(ctx, t, q, antes, f, nil)
		},
	})
}

// --- Umbrales y cobertura --------------------------------------------------------------------

// Umbrales de la factibilidad por distancia de la empresa activa.
type Umbrales struct {
	EmpresaID  *int64 `json:"empresaId" nullable:"true"`
	ViableM    int32  `json:"viableM" doc:"Viable hasta estos metros del router más cercano"`
	ObraM      int32  `json:"obraM" doc:"Requiere obra hasta estos metros; más lejos, no viable"`
	PorDefecto bool   `json:"porDefecto" doc:"La empresa no tiene los suyos: son los de Iris"`
	Version    int32  `json:"version" doc:"Versión de los de la empresa (0 si son los por defecto)"`
}

// EntradaUmbrales fija los umbrales de la empresa activa.
type EntradaUmbrales struct {
	Version int32 `json:"version" minimum:"0" doc:"Versión leída (0 si eran los por defecto)"`
	ViableM int32 `json:"viableM" minimum:"1" maximum:"50000"`
	ObraM   int32 `json:"obraM" minimum:"1" maximum:"50000" doc:"Mayor o igual que viableM"`
}

func aUmbrales(f comercialbd.ComercialUmbralesFactibilidad, empresa *int64) Umbrales {
	u := Umbrales{EmpresaID: empresa, ViableM: f.ViableM, ObraM: f.ObraM, PorDefecto: !mismoID(f.EmpresaID, empresa), Version: f.Version}
	if u.PorDefecto {
		u.Version = 0
	}
	return u
}

func (m *Modulo) nuevoVerUmbrales() *auditoria.Caso[struct{}, Umbrales] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, Umbrales]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[Umbrales],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (Umbrales, error) {
			todas, empresa := filtroEmpresa(ctx)
			if todas {
				empresa = nil
			}
			f, err := comercialbd.New(t).UmbralesDeEmpresa(ctx, empresa)
			if err != nil {
				return Umbrales{}, err
			}
			return aUmbrales(f, empresa), nil
		},
	})
}

func (m *Modulo) nuevoFijarUmbrales() *auditoria.Caso[EntradaUmbrales, Umbrales] {
	return auditoria.Nuevo(auditoria.Caso[EntradaUmbrales, Umbrales]{
		Accion: "factibilidad.configurar", Permiso: nucleo.PermFactibilidadConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaUmbrales) (Umbrales, error) {
			if !contexto.PermisosDe(ctx).TieneGlobal(string(nucleo.PermFactibilidadConfigurar)) {
				return Umbrales{}, nucleo.Prohibido("Los umbrales de la empresa exigen factibilidad.configurar sin límite de zona")
			}
			if in.ObraM < in.ViableM {
				return Umbrales{}, campoInvalido("obraM", "Debe ser mayor o igual que viableM", "min")
			}
			todas, empresa := filtroEmpresa(ctx)
			if todas || (empresa != nil && *empresa == 0) {
				return Umbrales{}, nucleo.EmpresaRequerida()
			}
			t.FijarEntidad("umbrales_factibilidad", deref(empresa))
			q := comercialbd.New(t)
			antes, err := q.UmbralesPropiosParaActualizar(ctx, empresa)
			var f comercialbd.ComercialUmbralesFactibilidad
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if in.Version != 0 {
					return Umbrales{}, nucleo.ConflictoVersion("umbrales_factibilidad", deref(empresa))
				}
				f, err = q.CrearUmbrales(ctx, comercialbd.CrearUmbralesParams{EmpresaID: empresa, ViableM: in.ViableM, ObraM: in.ObraM})
				if err != nil {
					return Umbrales{}, err
				}
				t.RegistrarCambios("umbrales_factibilidad", f.ID, nil, f)
			case err != nil:
				return Umbrales{}, err
			default:
				if err := nucleo.ExigirVersion(in.Version, antes.Version, "umbrales_factibilidad", antes.ID); err != nil {
					return Umbrales{}, err
				}
				f, err = q.GuardarUmbrales(ctx, comercialbd.GuardarUmbralesParams{ViableM: in.ViableM, ObraM: in.ObraM, ID: antes.ID, Version: antes.Version})
				if err != nil {
					return Umbrales{}, nucleo.VerificarVersion(err, "umbrales_factibilidad", antes.ID)
				}
				t.RegistrarCambios("umbrales_factibilidad", f.ID, antes, f)
			}
			return aUmbrales(f, empresa), t.Emitir("factibilidad.umbrales_cambiados", map[string]any{
				"empresa_id": empresa, "viable_m": f.ViableM, "obra_m": f.ObraM,
			})
		},
	})
}

// RouterCobertura es un router de la instantánea de Argos con coordenadas (mapa de cobertura).
type RouterCobertura struct {
	ID             int64     `json:"id"`
	Nombre         string    `json:"nombre"`
	ZonaID         *int64    `json:"zonaId" nullable:"true"`
	Lat            float64   `json:"lat"`
	Lon            float64   `json:"lon"`
	SincronizadoEn time.Time `json:"sincronizadoEn"`
}

// Cobertura: los routers con coordenadas y los umbrales (círculos viable y requiere obra).
type Cobertura struct {
	Disponible bool              `json:"disponible" doc:"false = no hay datos para dibujar cobertura (motivo); nunca suponer «sin cobertura»"`
	Motivo     *string           `json:"motivo" nullable:"true" doc:"sin_conector, conector_apagado, sin_empresa, sin_instantanea, sin_routers_con_coordenadas"`
	Motor      string            `json:"motor" doc:"distancia (fase 6: ozmap)"`
	Umbrales   Umbrales          `json:"umbrales"`
	Routers    []RouterCobertura `json:"routers"`
}

func (m *Modulo) nuevoCobertura() *auditoria.Caso[struct{}, Cobertura] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, Cobertura]{
		Accion: "prospectos.ver", Permiso: nucleo.PermProspectosVer, AuditarExito: soloLecturas[Cobertura],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (Cobertura, error) {
			q := comercialbd.New(t)
			todas, empresa := filtroEmpresa(ctx)
			if todas {
				empresa = nil
			}
			c := Cobertura{Motor: "distancia", Routers: []RouterCobertura{}}
			u, err := q.UmbralesDeEmpresa(ctx, empresa)
			if err != nil {
				return c, err
			}
			c.Umbrales = aUmbrales(u, empresa)
			estado, err := q.EstadoConectorArgos(ctx)
			if err != nil {
				return c, err
			}
			motivo := ""
			switch {
			case estado == "":
				motivo = MotivoSinConector
			case estado != "activo":
				motivo = MotivoConectorApagado
			case empresa == nil:
				motivo = MotivoSinEmpresa
			}
			if motivo == "" {
				rs, err := q.RoutersConCoordenadas(ctx, *empresa)
				if err != nil {
					return c, err
				}
				for _, r := range rs {
					c.Routers = append(c.Routers, RouterCobertura{ID: r.ID, Nombre: r.Nombre, ZonaID: r.ZonaID, Lat: r.Lat, Lon: r.Lon, SincronizadoEn: r.SincronizadoEn})
				}
				if len(rs) == 0 {
					motivo = MotivoSinRoutersConGeo
					if n, err := q.ContarRoutersDeEmpresa(ctx, *empresa); err != nil {
						return c, err
					} else if n == 0 {
						motivo = MotivoSinInstantanea
					}
				}
			}
			if motivo != "" {
				c.Motivo = &motivo
			}
			c.Disponible = motivo == ""
			return c, nil
		},
	})
}
