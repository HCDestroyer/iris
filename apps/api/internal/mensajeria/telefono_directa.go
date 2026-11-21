package mensajeria

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// La llamada directa («Llamar desde mi celular», contratos/voz.md §3.12, E4c): una fila en
// mensajeria.llamadas con origen celular y el evento llamada.directa_registrada. La anotan
// POST /telefono/llamada-directa (con señal: el panel o la app piden el número y abren tel:) y la
// operación llamada.directa de POST /sync/lote (sin señal: el teléfono ya llamó con el número de la
// orden guardada y la anota al volver la señal, con la hora real).

type directa struct {
	id       uuid.UUID
	empresa  *int64
	orden    int64
	cliente  *int64
	servicio *int64
	numero   string
	usuario  int64
	en       time.Time
}

// anotarDirecta guarda la fila y emite el evento (nunca el número). Devuelve la fila y el número en E.164.
func anotarDirecta(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, d directa) (mensajeriabd.MensajeriaLlamada, string, error) {
	dig := soloDigitos(d.numero)
	if len(dig) < 8 || len(dig) > 15 {
		return mensajeriabd.MensajeriaLlamada{}, "", nucleo.ReglaNegocio("sin_contacto", "La orden no tiene un teléfono válido")
	}
	e164 := "+" + dig
	if g := nacionalGT(d.numero); g != "" {
		e164 = "+502" + g
	}
	l, err := q.InsertarLlamadaDirecta(ctx, mensajeriabd.InsertarLlamadaDirectaParams{ID: d.id, IDExterno: "celular:" + d.id.String(), EmpresaID: d.empresa,
		Numero: e164, ClienteID: d.cliente, ServicioID: d.servicio, UsuarioID: d.usuario, Ahora: d.en, OrdenID: d.orden})
	if err != nil {
		return l, "", err
	}
	return l, e164, t.Emitir("llamada.directa_registrada", map[string]any{"llamada_id": l.ID, "empresa_id": l.EmpresaID, "orden_id": d.orden,
		"cliente_id": d.cliente, "servicio_id": d.servicio, "usuario_id": d.usuario, "origen": "celular"})
}

// LlamadasDirectas anota llamada.directa del lote de campo (campo.Config.LlamadasDirectas). No
// necesita el módulo de mensajería entero: cmd/iris (que atiende /sync/lote) la usa sin él.
type LlamadasDirectas struct{}

var _ campo.LlamadasDirectas = LlamadasDirectas{}

// RegistrarDirectaEnTx anota la llamada en la transacción de la operación. El número lo pone el
// servidor: el de la orden o el del cliente en la instantánea de Argos (el que eligió el técnico,
// o el primero que exista: contacto en sitio, móvil, principal), nunca uno que mande el teléfono.
func (LlamadasDirectas) RegistrarDirectaEnTx(ctx context.Context, t *auditoria.Tx, in campo.EntradaLlamadaDirectaSync) (uuid.UUID, error) {
	q := mensajeriabd.New(t)
	o := in.Orden
	tels := map[string]string{}
	if o.ClienteID != nil && o.EmpresaID != nil {
		var err error
		if tels, err = telefonosDeCliente(ctx, q, *o.EmpresaID, *o.ClienteID, o.ServicioID); err != nil {
			if _, interno := nucleo.AProblema(err); interno {
				return uuid.Nil, err
			}
			tels = map[string]string{}
		}
	}
	if o.ContactoTelefono != nil && strings.TrimSpace(*o.ContactoTelefono) != "" {
		tels["contacto"] = *o.ContactoTelefono
	}
	num, err := elegirTelefono(in.Telefono, []string{"contacto", "movil", "principal"}, tels)
	if err != nil {
		return uuid.Nil, err
	}
	l, _, err := anotarDirecta(ctx, t, q, directa{id: in.ID, empresa: o.EmpresaID, orden: o.ID, cliente: o.ClienteID, servicio: o.ServicioID,
		numero: num, usuario: in.UsuarioID, en: in.En})
	if err != nil {
		return uuid.Nil, err
	}
	return l.ID, nil
}
