package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// depende_de el vinculado del técnico (contrato 0.14.0 §6.6). Con Iris y Argos en 0.14.0, una
// operación que lleva `tecnico` no sale antes del 2xx del tecnico.argos_vinculado VIGENTE de su
// técnico en esa empresa hacia el conector activo:
//
//   - entregado: sale;
//   - en camino (en el outbox, pendiente de entrega, de otro conector o sin vínculo): espera cada
//     EsperaVinculo sin gastar intentos, hasta TopeEsperaVinculo (24 h); al tope queda bloqueado;
//   - agotado: bloqueado a la vista (integracion.pendiente_bloqueado) hasta que una persona lo
//     reenvíe (POST /mensajeria/webhooks-salida/{id}/reenviar) y Argos responda 2xx;
//   - omitido: fallido_negocio para una persona, con empresa_no_permitida (fuera del alcance),
//     sin_receptor (el conector no informó su receptor) u otro_conector (otra instancia);
//   - carga_abierta: el conector aún no cerró su carga inicial de vínculos (tras reconectar no se
//     hereda nada): espera.
//
// Un bloqueado por el vínculo vuelve solo a la cola cuando su vinculado tiene el 2xx.

// CodigoDependeDeVinculado: la operación espera (o quedó bloqueada por) el vinculado de su técnico.
const CodigoDependeDeVinculado = "depende_de_vinculado"

// Por qué un vinculado se omitió (los prefijos de mensajeria): la empresa está fuera del alcance del
// conector; el conector no informó su receptor (o no habla 0.14.0); el vínculo es de otra instancia.
const (
	CodigoEmpresaNoPermitida = "empresa_no_permitida"
	CodigoSinReceptor        = "sin_receptor"
	CodigoOtroConector       = "otro_conector"
)

// EsperaVinculo: cada cuánto se vuelve a mirar el vinculado (no gasta intentos).
var EsperaVinculo = time.Minute

// TopeEsperaVinculo: tras esta espera, la operación queda bloqueada a la vista.
var TopeEsperaVinculo = 24 * time.Hour

// Estados del vínculo (EstadoVinculo).
const (
	vinculoEntregado = "entregado"
	vinculoAgotado   = "agotado"
	vinculoOmitido   = "omitido"
)

// estadoVinculo: "" = la operación no depende del vínculo (sin `tecnico`, sin 0.14.0 o sin conector).
// motivo: el de un vinculado omitido.
func (m *Modulo) estadoVinculo(ctx context.Context, f pendientesbd.IntegracionPendiente) (estado, motivo string, err error) {
	if m.cfg.Argos == nil || f.TecnicoUsuarioID == nil || f.EmpresaID == nil || !llevaTecnico(f.Payload) || !m.cfg.Argos.InformaVinculos() {
		return "", "", nil
	}
	bd := nucleo.BDDe(ctx)
	ok, err := m.cfg.Argos.ClavesPorOperacion(ctx, bd)
	if err != nil || !ok {
		return "", "", err
	}
	conectorID, err := m.cfg.Argos.ConectorActivoID(ctx, bd)
	if err != nil {
		return "", "", nil //nolint:nilerr // sin conector activo: el intento lo registra como fallo temporal
	}
	r, err := pendientesbd.New(bd).EstadoVinculo(ctx, pendientesbd.EstadoVinculoParams{
		ConectorID: conectorID, EmpresaID: *f.EmpresaID, UsuarioID: *f.TecnicoUsuarioID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "sin_vinculo", "", nil
	}
	return r.Estado, r.Motivo, err
}

// dependeDeVinculo decide antes de llamar a Argos. true = puede salir.
func (m *Modulo) dependeDeVinculo(ctx context.Context, f pendientesbd.IntegracionPendiente, ahora time.Time) (bool, error) {
	estado, motivoOmitido, err := m.estadoVinculo(ctx, f)
	if err != nil {
		return false, err
	}
	q := pendientesbd.New(nucleo.BDDe(ctx))
	switch estado {
	case "":
		return true, nil
	case vinculoEntregado:
		if f.EsperandoVinculoDesde != nil {
			if err := q.TerminarEsperaVinculo(ctx, f.ID); err != nil {
				return false, err
			}
		}
		return true, nil
	case vinculoOmitido:
		codigo, detalle := CodigoSinReceptor, "el conector Argos no informó su receptor de webhooks o no habla 0.14.0; vuelve a conectar o hazlo a mano"
		switch motivoOmitido {
		case CodigoEmpresaNoPermitida:
			codigo, detalle = CodigoEmpresaNoPermitida, fmt.Sprintf("la empresa %d está fuera del alcance del conector; corrige el alcance o hazlo a mano", *f.EmpresaID)
		case CodigoOtroConector:
			codigo, detalle = CodigoOtroConector, "el vínculo salió de otra instancia de Argos; vuelve a vincular al técnico o hazlo a mano"
		}
		_, err := m.resolver.Ejecutar(ctx, resultadoIntento{ID: f.ID, Negocio: true, Codigo: codigo, ahora: ahora,
			Error: fmt.Sprintf("el vínculo del usuario %d no se mandó a Argos: %s", *f.TecnicoUsuarioID, detalle)})
		return false, err
	case vinculoAgotado:
		return false, m.bloquearVinculo(ctx, f, fmt.Sprintf("depende_de: el tecnico.argos_vinculado del usuario %d en la empresa %d agotó sus reintentos; "+
			"reenvíalo (POST /mensajeria/webhooks-salida/{id}/reenviar) y sale solo con el 2xx", *f.TecnicoUsuarioID, *f.EmpresaID))
	}
	motivo := fmt.Sprintf("depende_de: el tecnico.argos_vinculado del usuario %d en la empresa %d todavía no tiene el 2xx de Argos (%s)",
		*f.TecnicoUsuarioID, *f.EmpresaID, estado)
	desde, err := q.PostergarPendiente(ctx, pendientesbd.PostergarPendienteParams{ID: f.ID, ProximoIntento: ahora.Add(EsperaVinculo),
		UltimoError: &motivo, CodigoError: texto(CodigoDependeDeVinculado), Ahora: &ahora})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ahora.Sub(desde) >= TopeEsperaVinculo {
		return false, m.bloquearVinculo(ctx, f, motivo+"; pasaron más de 24 h esperando")
	}
	return false, nil
}

type eventoBloqueoVinculo struct {
	eventoPendiente
	DependeDe *int64 `json:"depende_de"`
	Motivo    string `json:"motivo"`
}

func (m *Modulo) bloquearVinculo(ctx context.Context, f pendientesbd.IntegracionPendiente, motivo string) error {
	_, err := m.bloquearPorVinculo.Ejecutar(ctx, entradaBloqueoVinculo{ID: f.ID, Motivo: motivo})
	return err
}

type entradaBloqueoVinculo struct {
	ID     int64  `json:"pendiente_id"`
	Motivo string `json:"motivo"`
}

func nuevoBloquearPorVinculo() *auditoria.Caso[entradaBloqueoVinculo, bool] {
	return auditoria.Nuevo(auditoria.Caso[entradaBloqueoVinculo, bool]{
		Accion: "integraciones.bloquear_pendiente_vinculo", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaBloqueoVinculo) (bool, error) {
			t.FijarEntidad("pendiente", in.ID)
			f, err := pendientesbd.New(t).BloquearPorVinculo(ctx, pendientesbd.BloquearPorVinculoParams{ID: in.ID, UltimoError: &in.Motivo})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoEnCurso}, map[string]any{"estado": f.Estado})
			return true, t.Emitir(EventoBloqueado, eventoBloqueoVinculo{eventoPendiente: eventoDe(f), Motivo: CodigoDependeDeVinculado + ": " + in.Motivo})
		},
	})
}

// desbloquearPorVinculo: los bloqueados por el vínculo cuyo vinculado ya tuvo el 2xx vuelven a la cola.
func (m *Modulo) desbloquearPorVinculo(ctx context.Context, ahora time.Time) error {
	fs, err := pendientesbd.New(nucleo.BDDe(ctx)).BloqueadosPorVinculo(ctx)
	if err != nil {
		return err
	}
	for _, f := range fs {
		estado, _, err := m.estadoVinculo(ctx, f)
		if err != nil {
			return err
		}
		// Solo con el 2xx: sin conector (o sin 0.14.0) el estado viene vacío y sigue bloqueado.
		if estado != vinculoEntregado {
			continue
		}
		if _, err := m.desbloquearVinculo.Ejecutar(ctx, entradaDesbloqueoVinculo{ID: f.ID, Ahora: ahora}); err != nil {
			return err
		}
	}
	return nil
}

type entradaDesbloqueoVinculo struct {
	ID    int64     `json:"pendiente_id"`
	Ahora time.Time `json:"-"`
}

func nuevoDesbloquearPorVinculo() *auditoria.Caso[entradaDesbloqueoVinculo, bool] {
	return auditoria.Nuevo(auditoria.Caso[entradaDesbloqueoVinculo, bool]{
		Accion: "integraciones.desbloquear_pendiente_vinculo", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaDesbloqueoVinculo) (bool, error) {
			t.FijarEntidad("pendiente", in.ID)
			f, err := pendientesbd.New(t).DesbloquearPorVinculo(ctx, pendientesbd.DesbloquearPorVinculoParams{ID: in.ID, Ahora: in.Ahora})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": EstadoBloqueado}, map[string]any{"estado": f.Estado})
			return true, t.Emitir(EventoDesbloqueado, eventoBloqueoVinculo{eventoPendiente: eventoDe(f), Motivo: CodigoDependeDeVinculado})
		},
	})
}

// llevaTecnico: el cuerpo trae `tecnico` (no nulo) arriba o en alguna de sus líneas.
func llevaTecnico(payload []byte) bool {
	var p struct {
		Tecnico any `json:"tecnico"`
		Lineas  []struct {
			Tecnico any `json:"tecnico"`
		} `json:"lineas"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return false
	}
	if p.Tecnico != nil {
		return true
	}
	for _, l := range p.Lineas {
		if l.Tecnico != nil {
			return true
		}
	}
	return false
}
