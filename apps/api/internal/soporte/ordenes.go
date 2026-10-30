package soporte

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Enlace con las órdenes de trabajo (internal/campo): una orden creada desde un ticket lee el
// ticket con las reglas de acceso de soporte y deja notas internas en él. campo importa soporte
// (nunca al revés).

// TicketDeOrden son los datos del ticket que hereda una orden creada desde él.
type TicketDeOrden struct {
	ID               int64
	Numero           string
	Estado           string
	EmpresaID        *int64
	ClienteID        *int64
	ServicioID       *int64
	ZonaID           *int64
	CuadrillaID      *int64
	AsignadoA        *int64
	Version          int32
	ContactoNombre   *string
	ContactoTelefono *string
	Asunto           string
}

// TicketParaOrden lee el ticket con FOR UPDATE dentro de la transacción del caso de uso de campo
// (no se cierra ni se fusiona hasta el COMMIT, y las acciones que después lo cambian —asignarlo,
// descartar o dejar la propuesta— no tienen que subir un FOR SHARE, que con dos transacciones a
// la vez acaba en deadlock) y exige que el actor lo vea (tickets.ver con su alcance y la empresa
// activa; si no, el mismo 404 que soporte). Un ticket cerrado da 422 ticket_cerrado. Orden de
// bloqueo de campo: primero la orden, después el ticket.
func TicketParaOrden(ctx context.Context, t *auditoria.Tx, id int64) (TicketDeOrden, error) {
	q := soportebd.New(t)
	f, err := q.TicketPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return TicketDeOrden{}, noEncontrado()
	}
	if err != nil {
		return TicketDeOrden{}, err
	}
	if err := verificarAcceso(ctx, nucleo.PermTicketsVer, f); err != nil {
		return TicketDeOrden{}, err
	}
	if f.Estado == EstadoCerrado {
		return TicketDeOrden{}, nucleo.ReglaNegocio("ticket_cerrado", "El ticket "+f.Numero+" está cerrado")
	}
	return TicketDeOrden{
		ID: f.ID, Numero: f.Numero, Estado: f.Estado, EmpresaID: f.EmpresaID, ClienteID: f.ClienteID,
		ServicioID: f.ServicioID, ZonaID: f.ZonaID, CuadrillaID: f.CuadrillaID, AsignadoA: f.AsignadoA, Version: f.Version,
		ContactoNombre: f.ContactoNombre, ContactoTelefono: f.ContactoTelefono, Asunto: f.Asunto,
	}, nil
}

// NotaDeOrden agrega una nota interna al ticket con el actor de la acción como autor (o Iris si
// es el sistema). No cambia el estado ni la versión del ticket, como tickets.nota_interna. El
// acceso ya lo verificó la acción de la orden.
func NotaDeOrden(ctx context.Context, t *auditoria.Tx, ticketID int64, cuerpo string) error {
	q := soportebd.New(t)
	f, err := q.TicketPorIDCompartido(ctx, ticketID)
	if err != nil {
		return err
	}
	_, err = mensaje(ctx, t, q, f, false, cuerpo)
	return err
}

// CierreDeOrden describe el cierre de una orden ligada a un ticket.
type CierreDeOrden struct {
	TicketID  int64
	OrdenID   int64
	Numero    string
	Resultado string // exitosa | parcial
	// OtrasAbiertas devuelve los números de las otras órdenes del ticket que siguen abiertas (ni
	// cerradas ni canceladas). Se llama con el ticket ya bloqueado (FOR UPDATE): dos cierres a la
	// vez de órdenes del mismo ticket se serializan ahí, y el segundo ve el COMMIT del primero.
	OtrasAbiertas func(ctx context.Context) ([]string, error)
	Ahora         time.Time
}

// ProponerPorOrden: la orden ligada al ticket se cerró. Deja una nota interna (autor: quien
// cerró) y, si el ticket sigue abierto (nuevo, abierto o en espera), no le quedan otras órdenes
// abiertas y no tiene ya una propuesta de Horus (que manda: no se pisa), la propuesta de
// resolverlo (origen orden) con ticket.resolucion_propuesta. Nunca cambia el estado del ticket:
// un agente confirma con tickets.resolver. Devuelve si propuso.
func ProponerPorOrden(ctx context.Context, t *auditoria.Tx, c CierreDeOrden) (bool, error) {
	q := soportebd.New(t)
	antes, err := q.TicketPorIDParaActualizar(ctx, c.TicketID)
	if err != nil {
		return false, err
	}
	abiertas, err := c.OtrasAbiertas(ctx)
	if err != nil {
		return false, err
	}
	deHorus := antes.Propuesta != nil && origenPropuesta(antes) == PropuestaDeHorus
	nota := fmt.Sprintf("La orden %s se cerró (%s).", c.Numero, c.Resultado)
	proponer := abierto(antes.Estado) && len(abiertas) == 0 && !deHorus
	switch {
	case !abierto(antes.Estado):
	case len(abiertas) > 0:
		nota += " Siguen abiertas: " + strings.Join(abiertas, ", ") + "; cuando terminen se propondrá resolver el ticket."
	case deHorus:
		nota += " Ya hay una propuesta de Horus pendiente."
	default:
		nota += " Propuesta: resolver el ticket (un agente lo confirma)."
	}
	if _, err := mensaje(ctx, t, q, antes, false, nota); err != nil {
		return false, err
	}
	if !proponer {
		return false, nil
	}
	cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
	if err != nil {
		return false, err
	}
	accion, motivo := "resolver", fmt.Sprintf("La orden %s se cerró (%s).", c.Numero, c.Resultado)
	despues := antes
	despues.Propuesta, despues.PropuestaEn, despues.PropuestaMotivo = &accion, &c.Ahora, &motivo
	despues.PropuestaOrigen, despues.PropuestaOrdenID = ptr(PropuestaDeOrden), &c.OrdenID
	f, err := guardar(ctx, t, q, antes, despues, cal)
	if err != nil {
		return false, err
	}
	return true, t.EmitirEnEmpresa("ticket.resolucion_propuesta", f.EmpresaID, map[string]any{
		"ticket_id": f.ID, "accion": accion, "motivo": motivo, "origen": PropuestaDeOrden,
		"orden_id": c.OrdenID, "entrante_id": nil,
	})
}

// DescartarPropuestaDeOrden: el ticket vuelve a tener una orden abierta (nueva o reabierta), así
// que la propuesta de resolver que dejó el cierre de otra orden ya no vale. Solo descarta las de
// origen orden (las de Horus siguen) y deja una nota interna. Sin propuesta de orden, nada.
func DescartarPropuestaDeOrden(ctx context.Context, t *auditoria.Tx, ticketID int64, numero string) error {
	q := soportebd.New(t)
	antes, err := q.TicketPorIDParaActualizar(ctx, ticketID)
	if err != nil {
		return err
	}
	if antes.Propuesta == nil || origenPropuesta(antes) != PropuestaDeOrden {
		return nil
	}
	cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
	if err != nil {
		return err
	}
	despues := antes
	despues.Propuesta, despues.PropuestaEn, despues.PropuestaMotivo = nil, nil, nil
	despues.PropuestaOrigen, despues.PropuestaOrdenID = nil, nil
	if _, err := guardar(ctx, t, q, antes, despues, cal); err != nil {
		return err
	}
	_, err = mensaje(ctx, t, q, antes, false, fmt.Sprintf("Se descarta la propuesta de resolver: la orden %s está abierta.", numero))
	return err
}

// origenPropuesta: horus u orden (las propuestas sin origen son de Horus: las escribió una
// versión anterior).
func origenPropuesta(f soportebd.SoporteTicket) string {
	if f.PropuestaOrigen == nil {
		return PropuestaDeHorus
	}
	return *f.PropuestaOrigen
}
