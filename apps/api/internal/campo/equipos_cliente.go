package campo

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Equipos del cliente (contrato con ArgosISP 0.12.0 §3.10 y §10.7, pedido B27). Argos es el dueño de
// las unidades y de su estado; Iris registra lo que hizo el técnico y lo pide por la cola de
// pendientes al cerrar la orden:
//
//   - equipo.cambiar (orden cambio_equipo): sale una unidad del comodato del servicio y entra otra, en
//     una sola operación; motivo, destino de la que sale (por defecto «a revisión») y un cobro propuesto
//     sin monto (lo decide una persona en Argos).
//   - equipo.no_recuperado (orden retiro): un intento de recuperación, con motivo, foto (obligatoria si
//     se negó o se perdió), la ubicación tomada en sitio y como máximo uno por unidad al día. Declarar
//     la unidad no recuperada o cobrarla lo decide una persona en Argos, nunca el técnico.
//
// La fecha es la del teléfono (nunca anterior a la asignación de la orden) y recibido_en la del
// servidor al aceptar el lote. Las notas (hasta 500 caracteres) no van en eventos ni en logs.

// Acciones de equipo nuevas (orden_equipos.accion).
const (
	AccionCambio       = "cambio"
	AccionNoRecuperado = "no_recuperado"
)

// Operaciones del lote (contratos/sync.md).
const (
	OpEquipoCambiar      = "equipo.cambiar"
	OpEquipoNoRecuperado = "equipo.no_recuperado"
)

// CodigoDemasiadosIntentos: más de un intento de recuperación de la unidad en el día.
const CodigoDemasiadosIntentos = "demasiados_intentos"

// NotaMaxima es el largo máximo de nota y cobroPropuesto.nota.
const NotaMaxima = 500

// Ubicación del intento en sitio (M-271-4; contratos/sync.md).
const (
	// PrecisionMaximaM: la precisión del GPS debe ser de 200 m o mejor.
	PrecisionMaximaM = 200
	// RadioSitioM: la ubicación debe estar a 500 m o menos de las coordenadas de la orden (el
	// servicio) y de la última posición reciente del técnico.
	RadioSitioM = 500
	// PosicionReciente: la posición del técnico de referencia es la última de los 30 minutos
	// anteriores a la fecha del intento (recibida antes del lote).
	PosicionReciente = 30 * time.Minute
	// CodigoUbicacionFueraDeSitio: la ubicación del intento no está en el sitio.
	CodigoUbicacionFueraDeSitio = "ubicacion_fuera_de_sitio"
	// CodigoUbicacionSinReferencia: sin coordenadas del servicio o sin una posición del técnico en
	// los 30 min anteriores, no se puede verificar: no se acepta en silencio.
	CodigoUbicacionSinReferencia = "ubicacion_sin_referencia"
	// CodigoSinLlegada: el cambio o el intento necesitan una llegada (orden.llegar) del mismo actor,
	// el mismo día y antes de la fecha.
	CodigoSinLlegada = "sin_llegada"
)

// soloElTecnico: el cambio y el intento los registra el técnico de la orden (A-271-1): nadie lo
// hace a su nombre (ni despacho ni supervisión con ordenes.registrar).
func soloElTecnico(ctx context.Context, f campobd.CampoOrden, s *OpSync) error {
	if !propia(ctx, f) && (s == nil || !s.gracia) {
		return nucleo.Prohibido("Solo el técnico de la orden registra cambios de equipo e intentos de recuperación")
	}
	return nil
}

// EquipoQueEntra identifica la unidad que entra (serie, MAC o unidad de Argos).
type EquipoQueEntra struct {
	UnidadID *int64  `json:"unidadId,omitempty" minimum:"1"`
	Serie    *string `json:"serie,omitempty" maxLength:"100"`
	MAC      *string `json:"mac,omitempty" maxLength:"30" doc:"Se normaliza a AA:BB:CC:DD:EE:FF"`
}

// CobroPropuesto es una propuesta de cobro al cliente, sin monto.
type CobroPropuesto struct {
	Motivo string `json:"motivo" enum:"golpe,humedad,manipulado,otro"`
	Nota   string `json:"nota,omitempty" maxLength:"500" auditoria:"sensible" doc:"Se muestra escapada; nunca va en eventos ni en logs"`
}

// EntradaCambiarEquipo es la entrada de ordenes.cambiar_equipo (operación equipo.cambiar).
type EntradaCambiarEquipo struct {
	ID             int64           `json:"-"`
	Version        *int32          `json:"version,omitempty" minimum:"1"`
	SaleUnidadID   int64           `json:"saleUnidadId" minimum:"1" doc:"La unidad en comodato del servicio que sale"`
	Entra          EquipoQueEntra  `json:"entra" doc:"La que entra: de la bodega del técnico o de su cuadrilla, disponible (lo comprueba Argos)"`
	Motivo         string          `json:"motivo" enum:"danado,actualizacion,garantia,otro"`
	DestinoSale    string          `json:"destinoSale,omitempty" enum:"funciona,revision,garantia,danado," doc:"Por defecto revision; funciona no vale con danado o garantia"`
	FotoAdjuntoID  *uuid.UUID      `json:"fotoAdjuntoId,omitempty" doc:"Foto opcional (adjunto de la orden)"`
	CobroPropuesto *CobroPropuesto `json:"cobroPropuesto,omitempty"`
	Nota           string          `json:"nota,omitempty" maxLength:"500" auditoria:"sensible"`
	Sync           *OpSync         `json:"-"`

	pre *prelectura
}

// EntradaNoRecuperado es la entrada de ordenes.no_recuperado (operación equipo.no_recuperado).
type EntradaNoRecuperado struct {
	ID            int64      `json:"-"`
	Version       *int32     `json:"version,omitempty" minimum:"1"`
	UnidadID      int64      `json:"unidadId" minimum:"1" doc:"La unidad en comodato del servicio que no se pudo recuperar"`
	Motivo        string     `json:"motivo" enum:"cliente_ausente,se_nego,equipo_perdido,otro"`
	FotoAdjuntoID *uuid.UUID `json:"fotoAdjuntoId" doc:"Obligatoria con cualquier motivo (0.13.1): adjunto foto de la orden, tuyo, tomado después de tu llegada, no rechazado ni usado en otro cambio o intento"`
	Lat           *float64   `json:"lat" minimum:"-90" maximum:"90" doc:"Ubicación del intento, tomada en sitio: a 500 m o menos del servicio y de tu posición de los 30 min anteriores (422 ubicacion_fuera_de_sitio; sin coordenadas del servicio o sin esa posición, 422 ubicacion_sin_referencia)"`
	Lon           *float64   `json:"lon" minimum:"-180" maximum:"180"`
	PrecisionM    *float64   `json:"precisionM" minimum:"0" maximum:"200" doc:"Precisión del GPS, obligatoria: 200 m o mejor"`
	Nota          string     `json:"nota,omitempty" maxLength:"500" auditoria:"sensible"`
	Sync          *OpSync    `json:"-"`

	pre *prelectura
}

func (e EntradaCambiarEquipo) ordenID() int64 { return e.ID }
func (e EntradaNoRecuperado) ordenID() int64  { return e.ID }

// CambiarEquipoPanel: ordenes.cambiar_equipo fuera del lote (lee el comodato de Argos antes).
func (m *Modulo) CambiarEquipoPanel(ctx context.Context, in EntradaCambiarEquipo) (Orden, error) {
	if f, ok := leerOrden(ctx, in.ID); ok {
		in.pre = m.comodatoDe(ctx, deref(f.EmpresaID), f.ServicioID)
	}
	return m.CambiarEquipo.Ejecutar(ctx, in)
}

// NoRecuperadoPanel: ordenes.no_recuperado fuera del lote (lee el comodato de Argos antes).
func (m *Modulo) NoRecuperadoPanel(ctx context.Context, in EntradaNoRecuperado) (Orden, error) {
	if f, ok := leerOrden(ctx, in.ID); ok {
		in.pre = m.comodatoDe(ctx, deref(f.EmpresaID), f.ServicioID)
	}
	return m.NoRecuperado.Ejecutar(ctx, in)
}

func notaOpcional(campo, nota string) (*string, error) {
	n := strings.TrimSpace(nota)
	if n == "" {
		return nil, nil
	}
	if strings.ContainsRune(n, 0) || len([]rune(n)) > NotaMaxima {
		return nil, campoInvalido(campo, "Hasta 500 caracteres", "maxLength")
	}
	return &n, nil
}

// exigirFechaDesdeAsignacion: la fecha del teléfono no puede ser anterior a la asignación de la
// orden (§10.7 c). Devuelve la fecha que se guarda (la del teléfono corregida, nunca futura).
func (m *Modulo) exigirFechaDesdeAsignacion(s *OpSync, f campobd.CampoOrden) (time.Time, error) {
	ahora := m.ahora()
	if s == nil {
		return ahora, nil
	}
	// Con gracia (orden ya reasignada), el historial ya comprobó que la tenía en esa fecha.
	if !s.gracia && f.AsignadaEn != nil && s.OcurridoEn.Before(*f.AsignadaEn) {
		return time.Time{}, campoInvalido("ocurridoEn", "Anterior a la asignación de la orden ("+f.AsignadaEn.UTC().Format(time.RFC3339)+")", "fecha")
	}
	if s.OcurridoEn.After(ahora) {
		return ahora, nil
	}
	return s.OcurridoEn, nil
}

// enComodato: con lo leído de Argos (antes de la transacción), la unidad está en comodato en el
// servicio de la orden. Sin lectura (sin conector o Argos caído) se acepta sin verificar.
func enComodato(pre *prelectura, f campobd.CampoOrden, unidad int64) (validado bool, err error) {
	if pre == nil || !pre.comodatoConocido || f.ServicioID == nil || pre.servicio != *f.ServicioID {
		return false, nil
	}
	for _, eq := range pre.comodato {
		if eq.UnidadID == unidad {
			return true, nil
		}
	}
	return false, nucleo.ReglaNegocio("equipo_no_en_comodato", "El equipo no está en comodato en el servicio de la orden según Argos")
}

// fotoDeLaOrden (M-271-3): la foto es un adjunto tipo foto de esta orden (puede no haber terminado de
// subir), del mismo técnico, registrado después de llegar al sitio, no rechazado ni abandonado y no
// usado en otro cambio o intento.
func fotoDeLaOrden(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, foto uuid.UUID, f campobd.CampoOrden, llegada time.Time) error {
	invalida := func(motivo string) error { return campoInvalido("fotoAdjuntoId", motivo, "foto") }
	a, err := adjuntos.DeEntidad(ctx, t, foto, "orden", strconv.FormatInt(f.ID, 10))
	if err != nil || a.Tipo != "foto" {
		return invalida("La foto debe ser un adjunto tipo foto de esta orden (POST /adjuntos)")
	}
	if actor := t.Actor(); !actor.EsUsuario() || a.SubidoPor == nil || *a.SubidoPor != actor.ID {
		return invalida("La foto debe ser tuya")
	}
	if a.Estado == "rechazado" {
		return invalida("La foto se rechazó al subir: tómala de nuevo")
	}
	if a.CreadoEn.Before(llegada) {
		return invalida("La foto debe tomarse en el sitio, después de llegar")
	}
	usada, err := q.FotoUsadaEnEquipo(ctx, &foto)
	if err != nil {
		return err
	}
	if usada {
		return invalida("Esa foto ya respalda otro cambio o intento")
	}
	return nil
}

// ubicacionEnSitio (M-271-4; 0.13.1 §3.10): precisión de 200 m o mejor, a 500 m o menos de las
// coordenadas del servicio y de la última posición del técnico en los 30 min anteriores a la fecha
// del intento, contando solo las que se recibieron antes de aceptar el lote. Sin coordenadas o sin
// esa posición, 422 ubicacion_sin_referencia (no se acepta sin verificar).
func ubicacionEnSitio(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden, actor int64, lat, lon float64, precision *float64,
	cuando, recibido time.Time) error {
	if precision == nil {
		return campoInvalido("precisionM", "La precisión del GPS es obligatoria", "required")
	}
	if *precision > PrecisionMaximaM {
		return campoInvalido("precisionM", "La precisión del GPS debe ser de 200 m o mejor: espera a tener mejor señal", "maximum")
	}
	problema := func(codigo, titulo, detalle string) error {
		p := nucleo.NuevoProblema(http.StatusUnprocessableEntity, codigo, titulo, detalle)
		p.Errors = []nucleo.ErrorCampo{{Field: "lat", Message: detalle, Rule: codigo}}
		return p
	}
	sinReferencia := func(detalle string) error {
		return problema(CodigoUbicacionSinReferencia, "Ubicación sin referencia", detalle)
	}
	fuera := func(detalle string) error {
		return problema(CodigoUbicacionFueraDeSitio, "Ubicación fuera del sitio", detalle)
	}
	if f.Lat == nil || f.Lon == nil {
		return sinReferencia("El servicio no tiene coordenadas: pide a despacho que las corrija antes de registrar el intento")
	}
	if metros(lat, lon, *f.Lat, *f.Lon) > RadioSitioM {
		return fuera("La ubicación del intento está a más de 500 m del servicio")
	}
	pos, err := q.PosicionDeReferencia(ctx, campobd.PosicionDeReferenciaParams{
		TecnicoID: actor, Hasta: cuando, Desde: cuando.Add(-PosicionReciente), RecibidoAntes: recibido})
	if errors.Is(err, pgx.ErrNoRows) {
		return sinReferencia("No hay una posición tuya de los 30 minutos anteriores al intento: activa la ubicación y espera a que se envíe")
	}
	if err != nil {
		return err
	}
	if metros(lat, lon, pos.Lat, pos.Lon) > RadioSitioM {
		return fuera("La ubicación del intento está a más de 500 m de tu posición de ese momento")
	}
	return nil
}

// llegadaDelActor (0.13.1 §3.10; M-271-3): el último orden.llegar del actor en la orden, del mismo
// día (hora local) y no posterior a la fecha. Sin ella, 422 sin_llegada.
func (m *Modulo) llegadaDelActor(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden, actor int64, cuando time.Time) (time.Time, error) {
	l := cuando.In(m.cfg.ZonaHoraria)
	dia := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, m.cfg.ZonaHoraria)
	llegada, err := q.LlegadaDelActor(ctx, campobd.LlegadaDelActorParams{OrdenID: f.ID, ActorID: &actor, Desde: dia, Hasta: cuando})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nucleo.ReglaNegocio(CodigoSinLlegada,
			"Marca la llegada a la orden (en sitio) antes de registrar el cambio o el intento")
	}
	return llegada, err
}

// sinNotas: el registro auditado sin los textos libres (nunca van en logs; §10.7 i).
func sinNotas(eq campobd.CampoOrdenEquipo) campobd.CampoOrdenEquipo {
	oculto := "***"
	if eq.Nota != nil {
		eq.Nota = &oculto
	}
	if eq.CobroNota != nil {
		eq.CobroNota = &oculto
	}
	return eq
}

func (m *Modulo) nuevoCambiarEquipo() *auditoria.Caso[EntradaCambiarEquipo, Orden] {
	return accionOrden("ordenes.cambiar_equipo", nucleo.PermOrdenesRegistrar, func(e EntradaCambiarEquipo) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCambiarEquipo) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			if err := soloElTecnico(ctx, f, in.Sync); err != nil {
				return f, "", err
			}
			if err := sinTrabajoEnPausa(ctx, f, in.Sync, "cambiar equipos"); err != nil {
				return f, "", err
			}
			if in.Sync == nil || !in.Sync.gracia {
				if err := exigirEstado(f, "cambiar equipos en", estadosEnCurso...); err != nil {
					return f, "", err
				}
			}
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return f, "", err
			}
			if tipo.Codigo != "cambio_equipo" {
				return f, "", nucleo.ReglaNegocio("equipo_no_admitido", "Solo una orden de cambio de equipo cambia equipos")
			}
			if f.ServicioID == nil {
				return f, "", nucleo.ReglaNegocio("orden_sin_servicio", "La orden no tiene servicio de Argos")
			}
			cuando, err := m.exigirFechaDesdeAsignacion(in.Sync, f)
			if err != nil {
				return f, "", err
			}
			actor := deref(actorUsuario(t))
			llegada, err := m.llegadaDelActor(ctx, q, f, actor, cuando)
			if err != nil {
				return f, "", err
			}
			destino := in.DestinoSale
			if destino == "" {
				destino = "revision"
			}
			switch {
			case in.SaleUnidadID <= 0:
				return f, "", campoObligatorio("saleUnidadId")
			case in.Motivo != "danado" && in.Motivo != "actualizacion" && in.Motivo != "garantia" && in.Motivo != "otro":
				return f, "", campoInvalido("motivo", "Motivo desconocido", "enum")
			case destino != "funciona" && destino != "revision" && destino != "garantia" && destino != "danado":
				return f, "", campoInvalido("destinoSale", "Destino desconocido", "enum")
			case destino == "funciona" && (in.Motivo == "danado" || in.Motivo == "garantia"):
				return f, "", campoInvalido("destinoSale", "Un equipo dañado o en garantía no vuelve como «funciona»", "destino")
			}
			serie, err := textoOpcional("entra.serie", in.Entra.Serie, 100)
			if err != nil {
				return f, "", err
			}
			if serie != nil {
				s := strings.ToUpper(*serie)
				serie = &s
			}
			var mac *string
			if in.Entra.MAC != nil && strings.TrimSpace(*in.Entra.MAC) != "" {
				v, ok := normalizarMAC(*in.Entra.MAC)
				if !ok {
					return f, "", campoInvalido("entra.mac", "MAC inválida", "mac")
				}
				mac = &v
			}
			if serie == nil && mac == nil && in.Entra.UnidadID == nil {
				return f, "", campoInvalido("entra", "Indica la serie, la MAC o la unidad que entra", "required")
			}
			if in.Entra.UnidadID != nil && *in.Entra.UnidadID == in.SaleUnidadID {
				return f, "", campoInvalido("entra", "La que entra no puede ser la misma que sale", "distinta")
			}
			nota, err := notaOpcional("nota", in.Nota)
			if err != nil {
				return f, "", err
			}
			var cobroMotivo, cobroNota *string
			if in.CobroPropuesto != nil {
				switch in.CobroPropuesto.Motivo {
				case "golpe", "humedad", "manipulado", "otro":
				default:
					return f, "", campoInvalido("cobroPropuesto.motivo", "Motivo desconocido", "enum")
				}
				cobroMotivo = &in.CobroPropuesto.Motivo
				if cobroNota, err = notaOpcional("cobroPropuesto.nota", in.CobroPropuesto.Nota); err != nil {
					return f, "", err
				}
			}
			if in.FotoAdjuntoID != nil {
				if err := fotoDeLaOrden(ctx, t, q, *in.FotoAdjuntoID, f, llegada); err != nil {
					return f, "", err
				}
			}
			validado, err := enComodato(in.pre, f, in.SaleUnidadID)
			if err != nil {
				return f, "", err
			}
			id, err := idDeEvidencia(in.Sync)
			if err != nil {
				return f, "", err
			}
			recibido := m.ahora()
			eq, err := q.CrearEquipoCliente(ctx, campobd.CrearEquipoClienteParams{
				ID: id, OrdenID: f.ID, Accion: AccionCambio, UnidadID: in.Entra.UnidadID, Serie: serie, Mac: mac, Validado: validado,
				RegistradoPor: actorUsuario(t), Canal: string(contexto.CanalDe(ctx)), OcurridoEn: cuando, OperacionID: operacionDe(in.Sync),
				SaleUnidadID: &in.SaleUnidadID, Motivo: &in.Motivo, DestinoSale: &destino, CobroMotivo: cobroMotivo, CobroNota: cobroNota,
				Nota: nota, FotoAdjuntoID: in.FotoAdjuntoID, RecibidoEn: &recibido,
			})
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return f, "", nucleo.ReglaNegocio("equipo_duplicado", "Ese equipo ya está registrado en la orden")
			}
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_equipo", eq.ID, nil, sinNotas(eq))
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoNoRecuperado() *auditoria.Caso[EntradaNoRecuperado, Orden] {
	return accionOrden("ordenes.no_recuperado", nucleo.PermOrdenesRegistrar, func(e EntradaNoRecuperado) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaNoRecuperado) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, in.Version, in.Sync)
			if err != nil {
				return f, "", err
			}
			if err := soloElTecnico(ctx, f, in.Sync); err != nil {
				return f, "", err
			}
			if err := sinTrabajoEnPausa(ctx, f, in.Sync, "registrar un intento de recuperación"); err != nil {
				return f, "", err
			}
			// En sitio (§10.7 a): el intento y su ubicación se registran donde está el equipo. Con
			// gracia (ya reasignada) lo prueba su propia llegada.
			if in.Sync == nil || !in.Sync.gracia {
				if err := exigirEstado(f, "registrar un intento de recuperación en", "en_sitio"); err != nil {
					return f, "", err
				}
			}
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return f, "", err
			}
			if tipo.Codigo != "retiro" {
				return f, "", nucleo.ReglaNegocio("equipo_no_admitido", "Solo una orden de retiro registra equipos no recuperados")
			}
			if f.ServicioID == nil {
				return f, "", nucleo.ReglaNegocio("orden_sin_servicio", "La orden no tiene servicio de Argos")
			}
			cuando, err := m.exigirFechaDesdeAsignacion(in.Sync, f)
			if err != nil {
				return f, "", err
			}
			switch in.Motivo {
			case "cliente_ausente", "se_nego", "equipo_perdido", "otro":
			default:
				return f, "", campoInvalido("motivo", "Motivo desconocido", "enum")
			}
			if in.UnidadID <= 0 {
				return f, "", campoObligatorio("unidadId")
			}
			if in.Lat == nil || in.Lon == nil {
				return f, "", campoInvalido("lat", "La ubicación del intento es obligatoria (tomada en sitio)", "required")
			}
			// 0.13.1: la foto es obligatoria con cualquier motivo (con cliente_ausente, la fachada).
			if in.FotoAdjuntoID == nil {
				return f, "", campoInvalido("fotoAdjuntoId", "La foto es obligatoria (con «cliente ausente», la de la fachada o el acceso)", "required")
			}
			actor := deref(actorUsuario(t))
			llegada, err := m.llegadaDelActor(ctx, q, f, actor, cuando)
			if err != nil {
				return f, "", err
			}
			recibidoLote := m.ahora()
			if in.Sync != nil && !in.Sync.RecibidoEn.IsZero() {
				recibidoLote = in.Sync.RecibidoEn
			}
			if err := ubicacionEnSitio(ctx, q, f, actor, *in.Lat, *in.Lon, in.PrecisionM, cuando, recibidoLote); err != nil {
				return f, "", err
			}
			if in.FotoAdjuntoID != nil {
				if err := fotoDeLaOrden(ctx, t, q, *in.FotoAdjuntoID, f, llegada); err != nil {
					return f, "", err
				}
			}
			nota, err := notaOpcional("nota", in.Nota)
			if err != nil {
				return f, "", err
			}
			// Un intento por unidad al día (hora local), en cualquier orden (Argos responde lo mismo).
			// Por la fecha del teléfono y por la del servidor, con la unidad bloqueada (A-271-2).
			if err := q.BloquearIntentosDeUnidad(ctx, strconv.FormatInt(in.UnidadID, 10)); err != nil {
				return f, "", err
			}
			diaDe := func(x time.Time) time.Time {
				l := x.In(m.cfg.ZonaHoraria)
				return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, m.cfg.ZonaHoraria)
			}
			dia, hoy := diaDe(cuando), diaDe(m.ahora())
			n, err := q.IntentosNoRecuperado(ctx, campobd.IntentosNoRecuperadoParams{UnidadID: &in.UnidadID, Desde: dia, Hasta: dia.AddDate(0, 0, 1),
				RecibidoDesde: &hoy, RecibidoHasta: ptr(hoy.AddDate(0, 0, 1))})
			if err != nil {
				return f, "", err
			}
			if n > 0 {
				return f, "", nucleo.NuevoProblema(http.StatusTooManyRequests, CodigoDemasiadosIntentos, "Ya hubo un intento hoy",
					"Ya se registró un intento de recuperación de este equipo hoy: se puede intentar de nuevo otro día")
			}
			validado, err := enComodato(in.pre, f, in.UnidadID)
			if err != nil {
				return f, "", err
			}
			id, err := idDeEvidencia(in.Sync)
			if err != nil {
				return f, "", err
			}
			var precision *float32
			if in.PrecisionM != nil {
				p := float32(*in.PrecisionM)
				precision = &p
			}
			recibido := m.ahora()
			eq, err := q.CrearEquipoCliente(ctx, campobd.CrearEquipoClienteParams{
				ID: id, OrdenID: f.ID, Accion: AccionNoRecuperado, UnidadID: &in.UnidadID, Validado: validado,
				RegistradoPor: actorUsuario(t), Canal: string(contexto.CanalDe(ctx)), OcurridoEn: cuando, OperacionID: operacionDe(in.Sync),
				Motivo: &in.Motivo, Nota: nota, FotoAdjuntoID: in.FotoAdjuntoID, Lat: in.Lat, Lon: in.Lon, PrecisionM: precision, RecibidoEn: &recibido,
			})
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return f, "", nucleo.ReglaNegocio("equipo_duplicado", "Ese equipo ya está registrado en la orden")
			}
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_equipo", eq.ID, nil, sinNotas(eq))
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}
