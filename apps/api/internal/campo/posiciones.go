package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Ubicación en vivo de los técnicos (campo.posiciones, particionada por mes): el teléfono manda
// lotes de puntos; despacho ve la última de cada técnico. Sin auditoría por fila (CLAUDE.md §9):
// un lote aceptado no deja fila en auditoria.acciones ni evento en el outbox.

// LimitesPosiciones de la ingesta (variables IRIS_POSICIONES_*).
type LimitesPosiciones struct {
	// MaxPorLote: puntos por petición (500 como mucho).
	MaxPorLote int
	// LotesPorMinuto por técnico; el siguiente recibe 429 demasiadas_posiciones.
	LotesPorMinuto int
	// IntervaloMin entre dos puntos guardados de un lote (los más seguidos se descartan).
	IntervaloMin time.Duration
	// Antiguedad máxima de un punto (24 h).
	Antiguedad time.Duration
	// ToleranciaFutura: un punto más adelantado que esto respecto del servidor se descarta.
	ToleranciaFutura time.Duration
	// Retencion: las particiones (meses) que terminaron antes de ahora − Retencion se borran.
	Retencion time.Duration
}

// maxPuntosLote es el tope del esquema (maxItems).
const maxPuntosLote = 500

func (l LimitesPosiciones) conDefectos() LimitesPosiciones {
	if l.MaxPorLote <= 0 || l.MaxPorLote > maxPuntosLote {
		l.MaxPorLote = maxPuntosLote
	}
	if l.LotesPorMinuto <= 0 {
		l.LotesPorMinuto = 6
	}
	if l.IntervaloMin <= 0 {
		l.IntervaloMin = 5 * time.Second
	}
	if l.Antiguedad <= 0 {
		l.Antiguedad = 24 * time.Hour
	}
	if l.ToleranciaFutura <= 0 {
		l.ToleranciaFutura = 2 * time.Minute
	}
	if l.Retencion <= 0 {
		l.Retencion = 90 * 24 * time.Hour
	}
	return l
}

// LimitesPosicionesDesdeEntorno lee IRIS_POSICIONES_MAX_POR_LOTE (500),
// IRIS_POSICIONES_LOTES_POR_MINUTO (6), IRIS_POSICIONES_INTERVALO_MIN (5s) e
// IRIS_POSICIONES_RETENCION_DIAS (90).
func LimitesPosicionesDesdeEntorno() (LimitesPosiciones, error) {
	var l LimitesPosiciones
	entero := func(nombre string, destino *int) error {
		if v := os.Getenv(nombre); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("%s: entero positivo", nombre)
			}
			*destino = n
		}
		return nil
	}
	if err := entero("IRIS_POSICIONES_MAX_POR_LOTE", &l.MaxPorLote); err != nil {
		return l, err
	}
	if err := entero("IRIS_POSICIONES_LOTES_POR_MINUTO", &l.LotesPorMinuto); err != nil {
		return l, err
	}
	var dias int
	if err := entero("IRIS_POSICIONES_RETENCION_DIAS", &dias); err != nil {
		return l, err
	}
	l.Retencion = time.Duration(dias) * 24 * time.Hour
	if v := os.Getenv("IRIS_POSICIONES_INTERVALO_MIN"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return l, errors.New("IRIS_POSICIONES_INTERVALO_MIN: duración positiva (p. ej. 5s)")
		}
		l.IntervaloMin = d
	}
	return l.conDefectos(), nil
}

// PuntoPosicion es un punto GPS del teléfono.
type PuntoPosicion struct {
	Lat          float64   `json:"lat" minimum:"-90" maximum:"90"`
	Lon          float64   `json:"lon" minimum:"-180" maximum:"180"`
	Precision    *float64  `json:"precision,omitempty" minimum:"0" maximum:"100000" doc:"Metros"`
	Velocidad    *float64  `json:"velocidad,omitempty" minimum:"0" maximum:"150" doc:"m/s"`
	Rumbo        *float64  `json:"rumbo,omitempty" minimum:"0" maximum:"360" doc:"Grados"`
	RegistradaEn time.Time `json:"registradaEn" doc:"Hora del punto (reloj del teléfono; se corrige con enviadoEn)"`
}

// EntradaPosiciones es el lote de puntos.
type EntradaPosiciones struct {
	DispositivoID string          `json:"dispositivoId,omitempty" maxLength:"100"`
	EnviadoEn     *time.Time      `json:"enviadoEn,omitempty" doc:"Hora del teléfono al enviar (mismo reloj que registradaEn): corrige el desfase"`
	Puntos        []PuntoPosicion `json:"puntos" minItems:"1" maxItems:"500"`
}

// MarshalJSON resume el lote para la auditoría de los rechazos (CLAUDE.md §9: las posiciones no
// se auditan por fila): nunca las coordenadas, solo cuántos puntos traía. Solo afecta la
// serialización (la API lee el cuerpo con el esquema y json.Unmarshal).
func (e EntradaPosiciones) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"dispositivoId": e.DispositivoID, "enviadoEn": e.EnviadoEn, "puntos": len(e.Puntos)})
}

// RespuestaPosiciones dice qué se guardó.
type RespuestaPosiciones struct {
	Aceptadas   int            `json:"aceptadas" doc:"Puntos nuevos guardados"`
	Duplicadas  int            `json:"duplicadas" doc:"Ya estaban (mismo técnico y hora): reenviar un lote es seguro"`
	Descartadas int            `json:"descartadas"`
	Motivos     map[string]int `json:"motivos" doc:"Descartados por motivo: vieja (más de 24 h), futura (adelantada) o frecuencia (más seguidos que el intervalo mínimo)"`
	ServidorEn  time.Time      `json:"servidorEn"`
	DesfaseMs   int64          `json:"desfaseMs" doc:"servidorEn − enviadoEn (0 sin enviadoEn)"`
}

func requiereSesionCampo() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "requiere_sesion_campo", "Solo desde la app de campo",
		"La ubicación la envía el técnico con su sesión de la app de campo (X-Iris-Canal: campo)")
}

func (m *Modulo) nuevoRegistrarPosiciones() *auditoria.Caso[EntradaPosiciones, RespuestaPosiciones] {
	lim := func() LimitesPosiciones { return m.cfg.Posiciones }
	return auditoria.Nuevo(auditoria.Caso[EntradaPosiciones, RespuestaPosiciones]{
		Accion: "campo.registrar_posiciones", Permiso: nucleo.PermCampoRegistrarPosiciones,
		// Sin auditoría por fila: ni el lote aceptado ni el rechazo por frecuencia (inundable). Los
		// demás rechazos se auditan con la entrada resumida ({dispositivoId, enviadoEn, puntos: n}).
		AuditarExito: soloLecturas[RespuestaPosiciones],
		AuditarError: func(err error) bool {
			var p *nucleo.Problema
			return !errors.As(err, &p) || p.Status != http.StatusTooManyRequests
		},
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPosiciones) (RespuestaPosiciones, error) {
			l := lim()
			actor := contexto.ActorDe(ctx)
			if actor.Tipo != contexto.ActorUsuario || actor.ID == 0 || contexto.CanalDe(ctx) != contexto.CanalCampo {
				return RespuestaPosiciones{}, requiereSesionCampo()
			}
			ahora := m.ahora()
			salida := RespuestaPosiciones{Motivos: map[string]int{}, ServidorEn: ahora}
			// El lote cuenta para el límite antes de validarlo y fuera de la transacción de la
			// acción (con el pool): un lote rechazado también gasta su cupo.
			bd := nucleo.BDDe(ctx)
			if bd == nil {
				return salida, nucleo.ErrSinBD
			}
			lote, err := campobd.New(bd).ContarLotePosiciones(ctx, campobd.ContarLotePosicionesParams{TecnicoID: actor.ID, Ahora: ahora})
			if err != nil {
				return salida, err
			}
			if int(lote.Lotes) > l.LotesPorMinuto {
				espera := lote.VentanaInicio.Add(time.Minute).Sub(ahora)
				return salida, nucleo.DemasiadosIntentos("demasiadas_posiciones",
					fmt.Sprintf("Hasta %d lotes de posiciones por minuto; junta los puntos en menos envíos", l.LotesPorMinuto), max(espera, time.Second))
			}
			if len(in.Puntos) > l.MaxPorLote {
				return salida, campoInvalido("puntos", fmt.Sprintf("Hasta %d puntos por lote", l.MaxPorLote), "maxItems")
			}
			if err := sinNulos(in); err != nil {
				return salida, err
			}
			var desfase time.Duration
			if in.EnviadoEn != nil {
				desfase = ahora.Sub(*in.EnviadoEn)
				salida.DesfaseMs = desfase.Milliseconds()
			}
			q := campobd.New(t)
			type punto struct {
				Lat          float64   `json:"lat"`
				Lon          float64   `json:"lon"`
				PrecisionM   *float64  `json:"precision_m"`
				Velocidad    *float64  `json:"velocidad"`
				Rumbo        *float64  `json:"rumbo"`
				RegistradaEn time.Time `json:"registrada_en"`
			}
			// Rango (también lo valida el esquema en HTTP; aquí, para cualquier llamador).
			for i, p := range in.Puntos {
				campo := fmt.Sprintf("puntos[%d]", i)
				switch {
				case p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180:
					return salida, campoInvalido(campo+".lat", "Coordenadas fuera de rango", "coordenadas")
				case p.Precision != nil && (*p.Precision < 0 || *p.Precision > 100000):
					return salida, campoInvalido(campo+".precision", "Precisión en metros, de 0 a 100000", "rango")
				case p.Velocidad != nil && (*p.Velocidad < 0 || *p.Velocidad > 150):
					return salida, campoInvalido(campo+".velocidad", "Velocidad en m/s, de 0 a 150", "rango")
				case p.Rumbo != nil && (*p.Rumbo < 0 || *p.Rumbo > 360):
					return salida, campoInvalido(campo+".rumbo", "Rumbo en grados, de 0 a 360", "rango")
				case p.RegistradaEn.IsZero():
					return salida, campoObligatorio(campo + ".registradaEn")
				}
			}
			puntos := make([]punto, 0, len(in.Puntos))
			for _, p := range in.Puntos {
				cuando := p.RegistradaEn.Add(desfase).UTC().Truncate(time.Microsecond)
				switch {
				case cuando.After(ahora.Add(l.ToleranciaFutura)):
					salida.Motivos["futura"]++
					continue
				case cuando.Before(ahora.Add(-l.Antiguedad)):
					salida.Motivos["vieja"]++
					continue
				}
				puntos = append(puntos, punto{Lat: p.Lat, Lon: p.Lon, PrecisionM: p.Precision, Velocidad: p.Velocidad,
					Rumbo: p.Rumbo, RegistradaEn: minT(cuando, ahora)})
			}
			slices.SortStableFunc(puntos, func(a, b punto) int { return a.RegistradaEn.Compare(b.RegistradaEn) })
			// Intervalo mínimo contra lo ya guardado del técnico (no solo dentro del lote): con su
			// candado, se leen sus posiciones cercanas al rango del lote. Misma hora = duplicada;
			// a menos del intervalo de una guardada o aceptada = frecuencia.
			guardar := make([]punto, 0, len(puntos))
			duplicadas := 0
			if len(puntos) > 0 {
				if err := q.CandadoPosicionesTecnico(ctx, actor.ID); err != nil {
					return salida, err
				}
				ocupadas, err := q.PosicionesCercanas(ctx, campobd.PosicionesCercanasParams{TecnicoID: actor.ID,
					Desde: puntos[0].RegistradaEn.Add(-l.IntervaloMin), Hasta: puntos[len(puntos)-1].RegistradaEn.Add(l.IntervaloMin)})
				if err != nil {
					return salida, err
				}
				enBD := slices.Clone(ocupadas)
				cmp := func(a, b time.Time) int { return a.Compare(b) }
				for _, p := range puntos {
					if _, ya := slices.BinarySearchFunc(enBD, p.RegistradaEn, cmp); ya {
						duplicadas++
						continue
					}
					i, igual := slices.BinarySearchFunc(ocupadas, p.RegistradaEn, cmp)
					if igual || (i > 0 && p.RegistradaEn.Sub(ocupadas[i-1]) < l.IntervaloMin) ||
						(i < len(ocupadas) && ocupadas[i].Sub(p.RegistradaEn) < l.IntervaloMin) {
						salida.Motivos["frecuencia"]++
						continue
					}
					guardar = append(guardar, p)
					ocupadas = slices.Insert(ocupadas, i, p.RegistradaEn)
				}
			}
			for _, n := range salida.Motivos {
				salida.Descartadas += n
			}
			salida.Duplicadas = duplicadas
			if len(guardar) == 0 {
				return salida, nil
			}
			_, empresa := filtroEmpresa(ctx)
			if empresa != nil && *empresa == 0 {
				return salida, nucleo.EmpresaRequerida()
			}
			for _, mes := range []time.Time{guardar[0].RegistradaEn, guardar[len(guardar)-1].RegistradaEn} {
				if err := q.AsegurarParticionPosiciones(ctx, mes); err != nil {
					return salida, err
				}
			}
			crudo, err := json.Marshal(guardar)
			if err != nil {
				return salida, err
			}
			var dispositivo *string
			if in.DispositivoID != "" {
				dispositivo = &in.DispositivoID
			}
			n, err := q.InsertarPosiciones(ctx, campobd.InsertarPosicionesParams{TecnicoID: actor.ID, EmpresaID: empresa,
				DispositivoID: dispositivo, Puntos: crudo})
			if err != nil {
				return salida, err
			}
			salida.Aceptadas, salida.Duplicadas = int(n), duplicadas+len(guardar)-int(n)
			u := guardar[len(guardar)-1]
			return salida, q.GuardarUltimaPosicion(ctx, campobd.GuardarUltimaPosicionParams{TecnicoID: actor.ID, EmpresaID: empresa,
				Lat: u.Lat, Lon: u.Lon, PrecisionM: u.PrecisionM, Velocidad: u.Velocidad, Rumbo: u.Rumbo, DispositivoID: dispositivo,
				RegistradaEn: u.RegistradaEn, RecibidaEn: ahora})
		},
	})
}

func minT(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// EntradaUltimasPosiciones filtra la última posición de los técnicos.
type EntradaUltimasPosiciones struct {
	TecnicoID     int64 `query:"tecnicoId" minimum:"0" doc:"Usuario técnico"`
	CuadrillaID   int64 `query:"cuadrillaId" minimum:"0"`
	ZonaID        int64 `query:"zonaId" minimum:"0" doc:"Técnicos cuya cuadrilla atiende la zona"`
	MaxAntiguedad int32 `query:"maxAntiguedadMin" minimum:"0" maximum:"10080" doc:"Solo las posiciones de los últimos N minutos (1440 por defecto)"`
}

// PosicionTecnico es la última posición de un técnico.
type PosicionTecnico struct {
	TecnicoID     int64     `json:"tecnicoId" doc:"Usuario técnico"`
	FichaID       *int64    `json:"fichaId" nullable:"true" doc:"Ficha de agenda"`
	Nombre        string    `json:"nombre"`
	CuadrillaID   *int64    `json:"cuadrillaId" nullable:"true"`
	Lat           float64   `json:"lat"`
	Lon           float64   `json:"lon"`
	Precision     *float64  `json:"precision" nullable:"true" doc:"Metros"`
	Velocidad     *float64  `json:"velocidad" nullable:"true" doc:"m/s"`
	Rumbo         *float64  `json:"rumbo" nullable:"true"`
	RegistradaEn  time.Time `json:"registradaEn"`
	RecibidaEn    time.Time `json:"recibidaEn"`
	AntiguedadSeg int64     `json:"antiguedadSeg" doc:"Segundos desde registradaEn"`
}

// UltimasPosiciones de los técnicos visibles.
type UltimasPosiciones struct {
	ServidorEn time.Time         `json:"servidorEn"`
	Data       []PosicionTecnico `json:"data"`
}

func (m *Modulo) nuevoUltimasPosiciones() *auditoria.Caso[EntradaUltimasPosiciones, UltimasPosiciones] {
	return auditoria.Nuevo(auditoria.Caso[EntradaUltimasPosiciones, UltimasPosiciones]{
		Accion: "campo.ver_posiciones", Permiso: nucleo.PermCampoVerPosiciones, AuditarExito: soloLecturas[UltimasPosiciones],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaUltimasPosiciones) (UltimasPosiciones, error) {
			ahora := m.ahora()
			antiguedad := time.Duration(in.MaxAntiguedad) * time.Minute
			if antiguedad <= 0 {
				antiguedad = 24 * time.Hour
			}
			todas, empresa := filtroEmpresa(ctx)
			global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermCampoVerPosiciones))
			if zonas == nil {
				zonas = []int64{}
			}
			if cuadrillas == nil {
				cuadrillas = []int64{}
			}
			filas, err := campobd.New(t).UltimasPosiciones(ctx, campobd.UltimasPosicionesParams{TodasEmpresas: todas, EmpresaID: empresa,
				Desde: ahora.Add(-antiguedad), TecnicoID: filtroID(in.TecnicoID), CuadrillaID: filtroID(in.CuadrillaID),
				ZonaID: filtroID(in.ZonaID), AlcanceGlobal: global, Zonas: zonas, Cuadrillas: cuadrillas,
				CuadrillaComun: agenda.PuedeTocarLoComun(ctx)})
			if err != nil {
				return UltimasPosiciones{}, err
			}
			salida := UltimasPosiciones{ServidorEn: ahora, Data: make([]PosicionTecnico, 0, len(filas))}
			for _, f := range filas {
				salida.Data = append(salida.Data, PosicionTecnico{TecnicoID: f.TecnicoID, FichaID: f.FichaID, Nombre: f.Nombre,
					CuadrillaID: f.CuadrillaID, Lat: f.Lat, Lon: f.Lon, Precision: f.PrecisionM, Velocidad: f.Velocidad, Rumbo: f.Rumbo,
					RegistradaEn: f.RegistradaEn, RecibidaEn: f.RecibidaEn, AntiguedadSeg: int64(ahora.Sub(f.RegistradaEn) / time.Second)})
			}
			return salida, nil
		},
	})
}

func filtroID(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

// ResultadoMantenimiento de las particiones de posiciones.
type ResultadoMantenimiento struct {
	ParticionesBorradas int   `json:"particionesBorradas"`
	UltimasBorradas     int64 `json:"ultimasBorradas"`
}

// nuevoMantenerPosiciones: crea las particiones del mes actual y los dos siguientes y borra las
// que terminaron antes de ahora − retención (y las últimas posiciones así de viejas). Job del
// worker (actor sistema); audita solo si borró algo.
func (m *Modulo) nuevoMantenerPosiciones() *auditoria.Caso[struct{}, ResultadoMantenimiento] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ResultadoMantenimiento]{
		Accion: "campo.mantener_posiciones", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoMantenimiento) bool { return r.ParticionesBorradas > 0 || r.UltimasBorradas > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ResultadoMantenimiento, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return ResultadoMantenimiento{}, nucleo.Prohibido("Solo el sistema mantiene las particiones de posiciones")
			}
			q := campobd.New(t)
			ahora := m.ahora()
			mes := time.Date(ahora.Year(), ahora.Month(), 1, 12, 0, 0, 0, time.UTC)
			for i := range 3 {
				if err := q.AsegurarParticionPosiciones(ctx, mes.AddDate(0, i, 0)); err != nil {
					return ResultadoMantenimiento{}, err
				}
			}
			limite := ahora.Add(-m.cfg.Posiciones.Retencion)
			n, err := q.PurgarPosiciones(ctx, limite)
			if err != nil {
				return ResultadoMantenimiento{}, err
			}
			u, err := q.PurgarUltimasPosiciones(ctx, limite)
			if err != nil {
				return ResultadoMantenimiento{}, err
			}
			if n > 0 {
				t.Anotar(map[string]any{"limite": limite})
			}
			return ResultadoMantenimiento{ParticionesBorradas: int(n), UltimasBorradas: u}, nil
		},
	})
}
