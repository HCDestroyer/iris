package soporte

import (
	"context"
	"errors"

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
	ContactoNombre   *string
	ContactoTelefono *string
	Asunto           string
}

// TicketParaOrden lee el ticket (FOR SHARE: no se cierra ni se fusiona hasta el COMMIT) dentro
// de la transacción del caso de uso de campo y exige que el actor lo vea (tickets.ver con su
// alcance y la empresa activa; si no, el mismo 404 que soporte). Un ticket cerrado da 422
// ticket_cerrado.
func TicketParaOrden(ctx context.Context, t *auditoria.Tx, id int64) (TicketDeOrden, error) {
	q := soportebd.New(t)
	f, err := q.TicketPorIDCompartido(ctx, id)
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
		ServicioID: f.ServicioID, ZonaID: f.ZonaID, CuadrillaID: f.CuadrillaID,
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
