package campo

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
	FotoAdjuntoID *uuid.UUID `json:"fotoAdjuntoId,omitempty" doc:"Obligatoria con se_nego y equipo_perdido (adjunto de la orden)"`
	Lat           *float64   `json:"lat" minimum:"-90" maximum:"90" doc:"Ubicación del intento, tomada en sitio"`
	Lon           *float64   `json:"lon" minimum:"-180" maximum:"180"`
	PrecisionM    *float64   `json:"precisionM,omitempty" minimum:"0" maximum:"100000"`
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
	if f.AsignadaEn != nil && s.OcurridoEn.Before(*f.AsignadaEn) {
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

// fotoDeLaOrden: la foto es un adjunto de esta orden (puede no haber terminado de subir).
func fotoDeLaOrden(ctx context.Context, t *auditoria.Tx, foto uuid.UUID, orden int64) error {
	a, err := adjuntos.DeEntidad(ctx, t, foto, "orden", strconv.FormatInt(orden, 10))
	if err != nil || a.Tipo != "foto" {
		return campoInvalido("fotoAdjuntoId", "La foto debe ser un adjunto tipo foto de esta orden (POST /adjuntos)", "foto")
	}
	return nil
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
			if err := sinTrabajoEnPausa(ctx, f, in.Sync, "cambiar equipos"); err != nil {
				return f, "", err
			}
			if err := exigirEstado(f, "cambiar equipos en", estadosEnCurso...); err != nil {
				return f, "", err
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
				if err := fotoDeLaOrden(ctx, t, *in.FotoAdjuntoID, f.ID); err != nil {
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
			if err := sinTrabajoEnPausa(ctx, f, in.Sync, "registrar un intento de recuperación"); err != nil {
				return f, "", err
			}
			// En sitio (§10.7 a): el intento y su ubicación se registran donde está el equipo.
			if err := exigirEstado(f, "registrar un intento de recuperación en", "en_sitio"); err != nil {
				return f, "", err
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
			if (in.Motivo == "se_nego" || in.Motivo == "equipo_perdido") && in.FotoAdjuntoID == nil {
				return f, "", campoInvalido("fotoAdjuntoId", "La foto es obligatoria si el cliente se negó o el equipo se perdió", "required")
			}
			if in.FotoAdjuntoID != nil {
				if err := fotoDeLaOrden(ctx, t, *in.FotoAdjuntoID, f.ID); err != nil {
					return f, "", err
				}
			}
			nota, err := notaOpcional("nota", in.Nota)
			if err != nil {
				return f, "", err
			}
			// Un intento por unidad al día (hora local), en cualquier orden (Argos responde lo mismo).
			l := cuando.In(m.cfg.ZonaHoraria)
			dia := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, m.cfg.ZonaHoraria)
			n, err := q.IntentosNoRecuperado(ctx, campobd.IntentosNoRecuperadoParams{UnidadID: &in.UnidadID, Desde: dia, Hasta: dia.AddDate(0, 0, 1)})
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
