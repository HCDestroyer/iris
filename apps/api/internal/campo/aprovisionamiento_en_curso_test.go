package campo_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// 409 aprovisionamiento_en_curso (contrato 0.14.3 §12.2, §10.13 puntos 4 a 6): ajeno (Argos u otra
// orden) queda fallido sin seguirlo; antes de decidir se reintenta el intento propio sin respuesta;
// si nombra un intento de Iris de la misma orden, se sigue ese y se descarta el nuevo.
func TestAprovisionamientoEnCursoPropioOAjeno(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	activar := func() {
		t.Helper()
		deber(e.m.Activacion.Activar.Ejecutar(e.cs, campo.EntradaActivarServicio{ID: o.ID, Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}}))(t)
	}
	pendienteDe := func(intento int32) (estado string, errorTexto string) {
		t.Helper()
		var ue *string
		if err := pool.QueryRow(ctx, `SELECT estado, ultimo_error FROM integracion.pendientes WHERE idempotency_key = $1`,
			pendientes.ClaveAprovisionar(o.ID, intento)).Scan(&estado, &ue); err != nil {
			t.Fatal(err)
		}
		if ue != nil {
			errorTexto = *ue
		}
		return estado, errorTexto
	}

	// 1. Ajeno, iniciado por Argos: el intento queda fallido con la causa y no toma el
	// aprovisionamiento_id ajeno.
	e.falso.rechazar("/aprovisionar", `409:aprovisionamiento_en_curso;{"aprovisionamiento_id":9100,"iniciado_por":"argos"}`)
	activar()
	e.procesar()
	e.relay()
	st := e.activacion(o.ID)
	if st.Estado != campo.ActivacionFallida || st.Actual.Causa == nil || *st.Actual.Causa != pendientes.CodigoAprovisionamientoEnCurso ||
		st.Actual.AprovisionamientoID != nil || !st.Disponible {
		t.Fatalf("ajeno: fallido con la causa, sin seguirlo: %+v %+v", st, st.Actual)
	}
	if est, txt := pendienteDe(1); est != pendientes.EstadoFallidoNegocio || !strings.Contains(txt, "iniciado_por: argos") {
		t.Fatalf("ajeno en la cola: %s %q", est, txt)
	}

	// 2. Otro intento de Iris de la orden salió sin respuesta y sigue en la cola: el 409 espera a que
	// ese se reintente (temporal), aunque iniciado_por diga iris.
	if _, err := pool.Exec(ctx, `UPDATE integracion.pendientes SET estado = 'pendiente', envio_sin_respuesta = true,
		proximo_intento = now() + interval '1 day' WHERE idempotency_key = $1`, pendientes.ClaveAprovisionar(o.ID, 1)); err != nil {
		t.Fatal(err)
	}
	e.falso.rechazar("/aprovisionar", `409:aprovisionamiento_en_curso;{"aprovisionamiento_id":9200,"iniciado_por":"iris"}`)
	activar()
	e.procesar()
	e.relay()
	if est, txt := pendienteDe(2); est != pendientes.EstadoPendiente || !strings.Contains(txt, "sin respuesta") {
		t.Fatalf("con un intento propio sin respuesta, el 409 es temporal: %s %q", est, txt)
	}
	if st := e.activacion(o.ID); st.Estado != campo.ActivacionPendiente {
		t.Fatalf("el intento sigue pendiente: %+v", st.Actual)
	}
	// El intento anterior se resolvió: el nuevo se reintenta y Argos lo acepta.
	if _, err := pool.Exec(ctx, `UPDATE integracion.pendientes SET estado = 'fallido_negocio' WHERE idempotency_key = $1`,
		pendientes.ClaveAprovisionar(o.ID, 1)); err != nil {
		t.Fatal(err)
	}
	e.reloj.avanzar(2 * time.Minute)
	e.procesar()
	e.relay()
	st = e.activacion(o.ID)
	if st.Estado != campo.ActivacionEnProceso || st.Actual.Intento != 2 || st.Actual.AprovisionamientoID == nil {
		t.Fatalf("segundo intento en proceso: %+v", st.Actual)
	}
	aprov := *st.Actual.AprovisionamientoID

	// 3. El intento 2 quedó fallido en Iris, pero Argos sigue con su aprovisionamiento: el 409 del
	// intento 3 lo nombra con iniciado_por iris. No es ajeno: se sigue el 2 y se descarta el 3.
	causa := "otro"
	deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), campo.EntradaResolverActivacion{Fuente: campo.FuenteWebhook,
		AprovisionamientoID: &aprov, Estado: campo.ActivacionFallida, Causa: &causa}))(t)
	e.falso.rechazar("/aprovisionar", `409:aprovisionamiento_en_curso;{"aprovisionamiento_id":`+strconv.FormatInt(aprov, 10)+`,"iniciado_por":"iris"}`)
	activar()
	e.procesar()
	e.relay()
	st = e.activacion(o.ID)
	if len(st.Intentos) != 3 || st.Intentos[0].Intento != 3 || st.Intentos[0].Estado != campo.ActivacionFallida ||
		st.Intentos[0].Causa == nil || *st.Intentos[0].Causa != pendientes.CodigoAprovisionamientoPropio || st.Intentos[0].AprovisionamientoID != nil {
		t.Fatalf("el intento 3 se descarta: %+v", st.Intentos)
	}
	if st.Estado != campo.ActivacionEnProceso || st.Actual.Intento != 2 || *st.Actual.AprovisionamientoID != aprov {
		t.Fatalf("se sigue el intento 2: %+v", st.Actual)
	}

	// Nunca dos intentos vivos en la orden.
	if n := contar(t, `SELECT count(*) FROM campo.activaciones WHERE orden_id = $1 AND estado <> 'fallido'`, o.ID); n != 1 {
		t.Fatalf("un solo intento vivo: %d", n)
	}
}
