package campo

import (
	"context"

	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
)

// Lo que mensajería necesita de campo para que el asistente de IA agende una visita
// (contratos/conversaciones.md §17.10). Crear y programar van por CrearOrden y ProgramarOrden; los
// huecos, por Agenda().

// OrdenAbierta es una orden viva de un cliente (lo mínimo para el tope de visitas abiertas).
type OrdenAbierta struct {
	ID         int64
	Numero     string
	Estado     string
	Version    int32
	Origen     string
	ServicioID *int64
	ZonaID     *int64
	Tipo       string
}

// OrdenesAbiertasDeCliente lista las órdenes vivas de un cliente en la empresa, de esos tipos.
func OrdenesAbiertasDeCliente(ctx context.Context, db campobd.DBTX, empresa *int64, cliente int64, tipos []string) ([]OrdenAbierta, error) {
	fs, err := campobd.New(db).OrdenesAbiertasDeCliente(ctx, campobd.OrdenesAbiertasDeClienteParams{ClienteID: &cliente, EmpresaID: empresa, Tipos: tipos})
	if err != nil {
		return nil, err
	}
	out := make([]OrdenAbierta, 0, len(fs))
	for _, f := range fs {
		out = append(out, OrdenAbierta{ID: f.ID, Numero: f.Numero, Estado: f.Estado, Version: f.Version, Origen: f.Origen, ServicioID: f.ServicioID,
			ZonaID: f.ZonaID, Tipo: f.Tipo})
	}
	return out, nil
}
