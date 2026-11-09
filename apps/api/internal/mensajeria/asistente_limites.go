package mensajeria

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Límites del asistente (contratos/conversaciones.md §16.2): costo, frecuencia y generaciones
// atascadas.

const (
	// PedidosAsistentePorMinuto: sugerencias (botón, «Otra») y resúmenes por persona y minuto.
	PedidosAsistentePorMinuto = 10
	// SugerenciasPorHoraConv: sugerencias (de cualquier origen) por conversación y hora.
	SugerenciasPorHoraConv = 20
	// AutomaticasPorHoraConv: sugerencias automáticas por conversación y hora.
	AutomaticasPorHoraConv = 6
	// SalidaEstimadaIA: tokens de salida que se reservan por llamada (se ajusta al terminar).
	SalidaEstimadaIA = 1024
	// MargenAtascadaIA: una sugerencia en generando más que PlazoIA + esto pasa a error.
	MargenAtascadaIA = 2 * time.Minute
)

// estimarTokens: entrada aproximada (3 caracteres por token) más la salida estimada.
func estimarTokens(p PedidoIA) int64 {
	n := int64(len([]rune(p.Instrucciones))+len([]rune(p.Contenido)))/3 + 1
	return n + min(p.MaxTokens, SalidaEstimadaIA)
}

// reservarTokens reserva de forma atómica los tokens estimados de una llamada: solo si lo usado más
// lo reservado más esta reserva cabe en el tope del mes. Si no, ErrIATope.
func (m *Modulo) reservarTokens(ctx context.Context, q *mensajeriabd.Queries, id uuid.UUID, empresa, tope, tokens int64) error {
	// Una reserva anterior con el mismo id (un reintento del job) se libera primero.
	if err := m.liberarReserva(ctx, q, id); err != nil {
		return err
	}
	mes, _, _ := m.mesDe(m.ahora())
	if err := q.AsegurarUsoAsistente(ctx, mensajeriabd.AsegurarUsoAsistenteParams{EmpresaID: empresaPtrDe(empresa), Mes: mes}); err != nil {
		return err
	}
	if _, err := q.ReservarTokens(ctx, mensajeriabd.ReservarTokensParams{Tokens: tokens, Empresa: empresa, Mes: mes, Tope: tope}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &ErrorIA{Codigo: ErrIATope, Detalle: "No queda tope de tokens este mes para otra llamada"}
		}
		return err
	}
	return q.InsertarReserva(ctx, mensajeriabd.InsertarReservaParams{ID: id, EmpresaID: empresaPtrDe(empresa), Mes: mes, Tokens: tokens, CreadaEn: m.ahora()})
}

// liberarReserva devuelve los tokens reservados (no hace nada si no hay reserva).
func (m *Modulo) liberarReserva(ctx context.Context, q *mensajeriabd.Queries, id uuid.UUID) error {
	r, err := q.LiberarReserva(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return q.DescontarReservados(ctx, mensajeriabd.DescontarReservadosParams{Tokens: r.Tokens, Empresa: derefEmpresa(r.EmpresaID), Mes: r.Mes})
}

// limitePedidos: 10 por minuto por persona (429 con Retry-After). Toma el candado de la persona
// hasta el COMMIT y anota el pedido.
func (m *Modulo) limitePedidos(ctx context.Context, q *mensajeriabd.Queries, usuario int64, c mensajeriabd.MensajeriaConversacion, tipo string) error {
	if err := q.CandadoChat(ctx, fmt.Sprintf("asistente:usuario:%d", usuario)); err != nil {
		return err
	}
	ahora := m.ahora()
	r, err := q.PedidosRecientesUsuario(ctx, mensajeriabd.PedidosRecientesUsuarioParams{UsuarioID: usuario, Desde: ahora.Add(-time.Minute)})
	if err != nil {
		return err
	}
	if r.Total >= PedidosAsistentePorMinuto {
		return nucleo.DemasiadosIntentos("demasiados_pedidos",
			fmt.Sprintf("Máximo %d pedidos al asistente por minuto: espera un momento", PedidosAsistentePorMinuto), r.Primero.Add(time.Minute).Sub(ahora))
	}
	return q.InsertarPedidoAsistente(ctx, mensajeriabd.InsertarPedidoAsistenteParams{UsuarioID: usuario, EmpresaID: c.EmpresaID,
		ConversacionID: c.ID, Tipo: tipo, CreadoEn: ahora})
}

// limiteConversacion: 20 sugerencias por hora por conversación (429 con Retry-After).
func (m *Modulo) limiteConversacion(ctx context.Context, q *mensajeriabd.Queries, conv int64) error {
	ahora := m.ahora()
	r, err := q.SugerenciasRecientesConv(ctx, mensajeriabd.SugerenciasRecientesConvParams{ConversacionID: conv, Desde: ahora.Add(-time.Hour)})
	if err != nil {
		return err
	}
	if r.Total >= SugerenciasPorHoraConv {
		return nucleo.DemasiadosIntentos("demasiadas_sugerencias",
			fmt.Sprintf("Máximo %d sugerencias por hora en una conversación", SugerenciasPorHoraConv), r.Primera.Add(time.Hour).Sub(ahora))
	}
	return nil
}

// vigilarCancelacion cancela ctx si la sugerencia deja de estar en generando («Otra», descartar,
// vencida): la llamada al proveedor se corta. Devuelve el contexto y cómo pararlo.
func (m *Modulo) vigilarCancelacion(ctx context.Context, id uuid.UUID) (context.Context, func()) {
	cctx, cancelar := context.WithCancel(ctx)
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return cctx, cancelar
	}
	listo := make(chan struct{})
	go func() {
		t := time.NewTicker(m.cfg.SondeoCancelacionIA)
		defer t.Stop()
		for {
			select {
			case <-listo:
				return
			case <-cctx.Done():
				return
			case <-t.C:
				s, err := mensajeriabd.New(bd).SugerenciaPorID(cctx, id)
				if err == nil && s.Estado != SugGenerando {
					cancelar()
					return
				}
			}
		}
	}()
	return cctx, func() { close(listo); cancelar() }
}

// Atascadas -----------------------------------------------------------------------------------------

func nuevoVencerAtascada(m *Modulo) *auditoria.Caso[uuidSugerencia, bool] {
	return auditoria.Nuevo(auditoria.Caso[uuidSugerencia, bool]{
		Accion: "mensajeria.asistente_atascada", Acceso: auditoria.Autenticado, AuditarExito: func(b bool) bool { return b },
		Logica: func(ctx context.Context, t *auditoria.Tx, in uuidSugerencia) (bool, error) {
			if err := soloSistema(ctx); err != nil {
				return false, err
			}
			q := mensajeriabd.New(t)
			s, err := q.SugerenciaParaActualizar(ctx, in.ID)
			if err != nil || s.Estado != SugGenerando {
				return false, err
			}
			t.FijarEmpresa(derefEmpresa(s.EmpresaID))
			t.FijarEntidad("conversacion", s.ConversacionID)
			if err := m.liberarReserva(ctx, q, s.ID); err != nil {
				return false, err
			}
			return true, m.terminarSugerencia(ctx, t, s, SugError, "", nil, nil, ErrIANoResponde,
				"El servidor de IA no respondió a tiempo", resultadoGeneracion{SugerenciaID: s.ID})
		},
	})
}

// mantenerAsistente (mantenimiento, cada minuto): sugerencias atascadas en generando → error
// proveedor_no_responde; reservas huérfanas (de un resumen o una sugerencia que no terminó) se
// liberan; los pedidos de más de un día se borran.
func (m *Modulo) mantenerAsistente(ctx context.Context, bd nucleo.BD, log *slog.Logger) error {
	q := mensajeriabd.New(bd)
	antes := m.ahora().Add(-(m.cfg.PlazoIA + MargenAtascadaIA))
	ids, err := q.SugerenciasAtascadas(ctx, antes)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := m.Asistente.vencerAtascada.Ejecutar(ctx, uuidSugerencia{ID: id}); err != nil {
			log.WarnContext(ctx, "mensajeria: sugerencia atascada", "sugerencia_id", id, "err", err)
		}
	}
	reservas, err := q.ReservasHuerfanas(ctx, antes)
	if err != nil {
		return err
	}
	for _, id := range reservas {
		if err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return m.liberarReserva(ctx, mensajeriabd.New(tx), id) }); err != nil {
			log.WarnContext(ctx, "mensajeria: liberar reserva", "reserva_id", id, "err", err)
		}
	}
	return q.PurgarPedidosAsistente(ctx, m.ahora().Add(-24*time.Hour))
}

// VencerAtascadas corre el barrido (pruebas).
func (m *Modulo) VencerAtascadas(ctx context.Context, bd nucleo.BD) error {
	return m.mantenerAsistente(ctx, bd, slog.Default())
}
