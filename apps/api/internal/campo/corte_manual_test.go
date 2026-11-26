package campo_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.15.0 §13.2 (B34): corte manual de respaldo del cable.

type entornoCorte struct {
	*entornoTitular
	conector int64
	tec      int64
	ct       context.Context
}

func nuevoEntornoCorte(t *testing.T) *entornoCorte {
	t.Helper()
	e := &entornoCorte{entornoTitular: nuevoEntornoTitular(t)}
	pool := pruebas.Pool(t)
	ctx := context.Background()
	if err := pool.QueryRow(ctx, "SELECT id FROM integracion.conectores WHERE sistema = 'argosisp' ORDER BY id DESC LIMIT 1").Scan(&e.conector); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
			VALUES (880, 1, '{1}', 'Cliente cable', 'C-880', '5512 3344', 'Zona 5', now()) ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, tipo_servicio, componentes, sincronizado_en)
			VALUES (8801, 1, 880, 'TV Básica', 'activo', 'cable', '{cable}', now()) ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, router_id, estado, tipo_servicio, componentes, sincronizado_en)
			VALUES (8802, 1, 880, 'Combo', 5, 'activo', 'mixto', '{internet,cable}', now()) ON CONFLICT (id) DO NOTHING`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM campo.cortes_manuales", "DELETE FROM integracion.argos_servicios WHERE cliente_id = 880",
			"DELETE FROM integracion.argos_clientes WHERE id = 880"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	e.tec, e.ct = e.tecnicoEnEmpresa()
	return e
}

var solicitudCorte = time.Now().UnixMilli() % 1_000_000_000

// aviso aplica argos.corte_manual como lo haría el relay.
func (e *entornoCorte) aviso(evento string, solicitud int64, version int32, accion, servicio string, descarte *string) campo.ResultadoCorte {
	e.t.Helper()
	s, _ := strconv.ParseInt(servicio, 10, 64)
	r, err := e.m.RegistrarCorte.Ejecutar(sistema(e.t), campo.EntradaCorteArgos{Evento: evento, SolicitudID: solicitud, Version: version,
		ServicioID: s, EmpresaID: 1, ClienteID: ptr(int64(880)), Componente: "cable", Accion: accion, Causa: "sin_onu",
		ConectorID: e.conector, Descarte: descarte})
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// estados: lo que Iris mandó a Argos de una solicitud (corte_manual.estado_cambiado), en orden.
func (e *entornoCorte) estados(solicitud int64) []map[string]any {
	e.t.Helper()
	filas, err := pruebas.Pool(e.t).Query(context.Background(), `SELECT payload FROM integracion.outbox
		WHERE evento = 'corte_manual.estado_cambiado' AND payload->>'solicitud_id' = $1 ORDER BY created_at, id`, strconv.FormatInt(solicitud, 10))
	if err != nil {
		e.t.Fatal(err)
	}
	defer filas.Close()
	out := []map[string]any{}
	for filas.Next() {
		var b []byte
		if err := filas.Scan(&b); err != nil {
			e.t.Fatal(err)
		}
		var p map[string]any
		_ = json.Unmarshal(b, &p)
		out = append(out, p)
	}
	return out
}

func (e *entornoCorte) ultimo(solicitud int64) map[string]any {
	e.t.Helper()
	s := e.estados(solicitud)
	if len(s) == 0 {
		e.t.Fatalf("la solicitud %d no informó nada a Argos", solicitud)
	}
	return s[len(s)-1]
}

// asignar programa la orden al técnico.
func (e *entornoCorte) asignar(id int64) campo.Orden {
	e.t.Helper()
	o := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: id}))(e.t)
	return deber(e.m.ProgramarOrden.Ejecutar(e.cs, campo.EntradaProgramarOrden{ID: id, Version: o.Version, TecnicoID: &e.tec,
		Inicio: hoyEnGuatemala(9)}))(e.t)
}

func TestCorteManualSolicitadoAsignadoYResuelto(t *testing.T) {
	e := nuevoEntornoCorte(t)
	sol := solicitudCorte + 1
	r := e.aviso("solicitado", sol, 1, "corte", "8802", nil)
	if r.Estado != campo.CorteAbierta || r.OrdenID == nil {
		t.Fatalf("solicitado: %+v", r)
	}
	// Idempotente por solicitud_id y version (B3): el mismo aviso no abre otra orden.
	if r2 := e.aviso("solicitado", sol, 1, "corte", "8802", nil); !r2.Repetido || *r2.OrdenID != *r.OrdenID {
		t.Fatalf("repetido: %+v", r2)
	}
	d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: *r.OrdenID}))(t)
	if d.Tipo != campo.TipoCorteCATV || d.Prioridad != campo.PrioridadNormal || d.Origen != "argos" || d.CorteManual == nil ||
		d.CorteManual.Causa != "sin_onu" || d.CorteManual.CausaTexto == "" || d.TicketID != nil {
		t.Fatalf("orden de corte: %+v %+v", d.Orden, d.CorteManual)
	}
	// B8: en un servicio mixto solo se toca el cable.
	if !strings.Contains(d.Descripcion, "solo se toca el cable") || !strings.Contains(d.Descripcion, "no tiene ONU registrada") {
		t.Fatalf("descripción: %q", d.Descripcion)
	}
	if len(e.estados(sol)) != 0 {
		t.Fatal("al abrir la orden todavía no se informa nada")
	}

	// Asignar → en_curso (version 1 de Iris, con la referencia a la orden).
	o := e.asignar(*r.OrdenID)
	if u := e.ultimo(sol); u["estado"] != "en_curso" || u["version"] != float64(1) || u["referencia"] != o.Numero || u["referencia_tipo"] != "orden" {
		t.Fatalf("en_curso: %v", u)
	}
	// Reprogramar no vuelve a avisar en_curso.
	o = deber(e.m.ReprogramarOrden.Ejecutar(e.cs, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &e.tec,
		Inicio: hoyEnGuatemala(10), Motivo: ptr("El cliente pidió otra hora")}))(t)
	if n := len(e.estados(sol)); n != 1 {
		t.Fatalf("en_curso se informa una vez: %d", n)
	}

	// La app de campo, sin señal: llegar, checklist de cierre «Hecho» y cerrar (POST /sync/lote).
	esperar(t, e.lote(e.ct,
		op(campo.OpLlegar, o.ID, nil),
		op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "resultado", "valor": "Hecho"}),
		op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "donde", "valor": "Puerto CATV de la ONU"}),
		op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa"}),
	), "aplicada", "aplicada", "aplicada", "aplicada")
	if u := e.ultimo(sol); u["estado"] != "resuelta" || u["version"] != float64(2) || u["motivo_codigo"] != nil {
		t.Fatalf("resuelta: %v", u)
	}
	// B7: cerrar no activa ni retira en Argos; B6: sin ticket ni encuesta.
	e.relay()
	if n := contar(t, `SELECT count(*) FROM integracion.pendientes WHERE idempotency_key LIKE '%:'||$1 OR idempotency_key LIKE '%:'||$1||':%'`,
		strconv.FormatInt(o.ID, 10)); n != 0 {
		t.Fatalf("el cierre de un corte no llama a Argos: %d pendientes", n)
	}
	if n := contar(t, `SELECT count(*) FROM soporte.encuestas WHERE entidad = 'orden' AND orden_id = $1`, o.ID); n != 0 {
		t.Fatal("sin encuesta al cliente")
	}

	// Un cancelado que llega tarde (el corte ya se hizo): se informa resuelta otra vez (B4).
	e.aviso("cancelado", sol, 2, "corte", "8802", nil)
	if u := e.ultimo(sol); u["estado"] != "resuelta" || u["version"] != float64(3) {
		t.Fatalf("cancelado tarde: %v", u)
	}
	// Un aviso atrasado (versión menor) no cambia nada.
	if r := e.aviso("cancelado", sol, 1, "corte", "8802", nil); !r.Repetido || len(e.estados(sol)) != 3 {
		t.Fatalf("atrasado: %+v", r)
	}
}

func TestCorteManualReconexionYNoRealizada(t *testing.T) {
	e := nuevoEntornoCorte(t)
	sol := solicitudCorte + 2
	r := e.aviso("solicitado", sol, 1, "reconexion", "8801", nil)
	d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: *r.OrdenID}))(t)
	if d.Tipo != campo.TipoReconexionCATV || d.Prioridad != campo.PrioridadAlta {
		t.Fatalf("la reconexión va con prioridad alta (B5): %+v", d.Orden)
	}
	o := e.asignar(d.ID)
	// Checklist «Equipo no encontrado»: no_realizada con su motivo_codigo.
	esperar(t, e.lote(e.ct,
		op(campo.OpLlegar, o.ID, nil),
		op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "resultado", "valor": "Equipo no encontrado"}),
		op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa"}),
	), "aplicada", "aplicada", "aplicada")
	if u := e.ultimo(sol); u["estado"] != "no_realizada" || u["motivo_codigo"] != "equipo_no_encontrado" {
		t.Fatalf("no_realizada: %v", u)
	}
	// no_realizada es terminal: un cancelado después no cambia nada.
	n := len(e.estados(sol))
	e.aviso("cancelado", sol, 2, "reconexion", "8801", nil)
	if len(e.estados(sol)) != n || e.ultimo(sol)["estado"] != "no_realizada" {
		t.Fatal("los terminales no retroceden")
	}

	// Falla en sitio sin acceso: no_realizada sin_acceso.
	sol2 := solicitudCorte + 3
	r2 := e.aviso("solicitado", sol2, 1, "corte", "8801", nil)
	o2 := e.asignar(*r2.OrdenID)
	esperar(t, e.lote(e.ct, op(campo.OpLlegar, o2.ID, nil), op(campo.OpFallar, o2.ID, map[string]any{"motivoFalla": "sin_acceso"})), "aplicada", "aplicada")
	if u := e.ultimo(sol2); u["estado"] != "no_realizada" || u["motivo_codigo"] != "sin_acceso" {
		t.Fatalf("fallada sin acceso: %v", u)
	}
}

func TestCorteManualCancelaciones(t *testing.T) {
	e := nuevoEntornoCorte(t)
	// Cancelado con la orden sin empezar: se cancela y se informa cancelada.
	sol := solicitudCorte + 4
	r := e.aviso("solicitado", sol, 1, "corte", "8801", nil)
	o := e.asignar(*r.OrdenID)
	e.aviso("cancelado", sol, 2, "corte", "8801", nil)
	if u := e.ultimo(sol); u["estado"] != "cancelada" || u["version"] != float64(2) {
		t.Fatalf("cancelada: %v", u)
	}
	if d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); d.Estado != campo.EstadoCancelada {
		t.Fatalf("la orden se cancela: %s", d.Estado)
	}

	// Cancelado antes que el solicitado: se guarda y no abre nada; el solicitado de una versión menor o
	// igual tampoco.
	sol2 := solicitudCorte + 5
	if r := e.aviso("cancelado", sol2, 2, "corte", "8801", nil); r.Estado != campo.CorteCanceladaAntes || r.OrdenID != nil {
		t.Fatalf("cancelado antes: %+v", r)
	}
	if r := e.aviso("solicitado", sol2, 1, "corte", "8801", nil); r.OrdenID != nil {
		t.Fatalf("el solicitado atrasado no abre la orden: %+v", r)
	}
	if len(e.estados(sol2)) != 0 {
		t.Fatal("nada que informar")
	}

	// Cancelado con el técnico en sitio: decide el cierre. Sin hacerlo, cancelada (no no_realizada).
	sol3 := solicitudCorte + 6
	r3 := e.aviso("solicitado", sol3, 1, "corte", "8801", nil)
	o3 := e.asignar(*r3.OrdenID)
	esperar(t, e.lote(e.ct, op(campo.OpLlegar, o3.ID, nil)), "aplicada")
	e.aviso("cancelado", sol3, 2, "corte", "8801", nil)
	if d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o3.ID}))(t); d.Estado != campo.EstadoEnSitio || d.CorteManual == nil || !d.CorteManual.CancelacionPedida {
		t.Fatalf("en sitio: %s %+v", d.Estado, d.CorteManual)
	}
	esperar(t, e.lote(e.ct,
		op(campo.OpChecklistMarcar, o3.ID, map[string]any{"codigo": "resultado", "valor": "Sin acceso"}),
		op(campo.OpCerrar, o3.ID, map[string]any{"resultado": "exitosa"}),
	), "aplicada", "aplicada")
	if u := e.ultimo(sol3); u["estado"] != "cancelada" {
		t.Fatalf("cancelación pedida y sin hacer: %v", u)
	}

	// B9: empresa fuera del alcance o servicio desconocido: se guarda sin abrir nada.
	sol4 := solicitudCorte + 7
	if r := e.aviso("solicitado", sol4, 1, "corte", "8801", ptr("fuera_de_alcance")); r.Estado != campo.CorteDescartada || r.OrdenID != nil {
		t.Fatalf("descartada: %+v", r)
	}
	if n := contar(t, `SELECT count(*) FROM campo.cortes_manuales WHERE solicitud_id = $1 AND descarte = 'fuera_de_alcance'`, sol4); n != 1 {
		t.Fatal("la solicitud descartada queda guardada")
	}
}

// avisoCon: argos.corte_manual con la entrada completa (empresa, servicio, conector) y sin t.Fatal.
func (e *entornoCorte) avisoCon(in campo.EntradaCorteArgos) (campo.ResultadoCorte, error) {
	if in.ConectorID == 0 {
		in.ConectorID = e.conector
	}
	if in.EmpresaID == 0 {
		in.EmpresaID = 1
	}
	if in.Accion == "" {
		in.Accion = "corte"
	}
	in.Componente, in.Causa = "cable", "sin_onu"
	return e.m.RegistrarCorte.Ejecutar(sistema(e.t), in)
}

// Revisión de seguridad de #297 (B9): un cancelado con descarte, o con otra empresa, servicio o acción
// que la solicitud guardada, no cancela su orden: solo se anota. La empresa de la auditoría es la de la
// solicitud, nunca la del aviso.
func TestCorteManualCanceladoAjenoNoTocaNada(t *testing.T) {
	e := nuevoEntornoCorte(t)
	sol := solicitudCorte + 11
	r := e.aviso("solicitado", sol, 1, "corte", "8801", nil)
	o := e.asignar(*r.OrdenID)
	for i, in := range []campo.EntradaCorteArgos{
		{Evento: "cancelado", SolicitudID: sol, Version: 2, ServicioID: 8801, Descarte: ptr("fuera_de_alcance")},
		{Evento: "cancelado", SolicitudID: sol, Version: 3, ServicioID: 8801, EmpresaID: 2},
		{Evento: "cancelado", SolicitudID: sol, Version: 4, ServicioID: 8802},
		{Evento: "cancelado", SolicitudID: sol, Version: 5, ServicioID: 8801, Accion: "reconexion"},
	} {
		res, err := e.avisoCon(in)
		if err != nil || res.Ignorado == "" || res.Estado != campo.CorteEnCurso {
			t.Fatalf("aviso %d: %+v %v", i, res, err)
		}
	}
	if d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); d.Estado != campo.EstadoProgramada {
		t.Fatalf("la orden no se toca: %s", d.Estado)
	}
	if u := e.ultimo(sol); u["estado"] != "en_curso" {
		t.Fatalf("a Argos no se informa nada nuevo: %v", u)
	}
	// Queda anotado, con la empresa de la solicitud (1), nunca la del aviso (2).
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.corte_manual' AND entidad_id = $1
		AND entrada->'auditoria'->>'ignorado' = 'empresa_no_coincide' AND empresa_id = 1`, strconv.FormatInt(sol, 10)); n != 1 {
		t.Fatalf("anotado con la empresa de la solicitud: %d", n)
	}
	// Un cancelado con descarte de una solicitud que Iris no conoce tampoco guarda nada.
	if res, err := e.avisoCon(campo.EntradaCorteArgos{Evento: "cancelado", SolicitudID: sol + 1, Version: 1, ServicioID: 8801, EmpresaID: 99,
		Descarte: ptr("fuera_de_alcance")}); err != nil || res.Ignorado == "" {
		t.Fatalf("cancelado ajeno y nuevo: %+v %v", res, err)
	}
	if n := contar(t, `SELECT count(*) FROM campo.cortes_manuales WHERE solicitud_id = $1`, sol+1); n != 0 {
		t.Fatal("no se guarda nada de otra empresa")
	}
	// Un aviso válido después sí se aplica (la versión ignorada no cuenta).
	e.aviso("cancelado", sol, 6, "corte", "8801", nil)
	if u := e.ultimo(sol); u["estado"] != "cancelada" {
		t.Fatalf("cancelado válido: %v", u)
	}
}

// La clave es (conector_id, solicitud_id): con un conector nuevo, la misma solicitud_id es otra solicitud y
// no toca la del conector anterior.
func TestCorteManualClavePorConector(t *testing.T) {
	e := nuevoEntornoCorte(t)
	var otro int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `INSERT INTO integracion.conectores (sistema, base_url, origen, token_cifrado, secreto_webhook_cifrado)
		SELECT sistema, base_url || '/otro', origen, token_cifrado, secreto_webhook_cifrado FROM integracion.conectores WHERE id = $1 RETURNING id`, e.conector).Scan(&otro); err != nil {
		t.Fatal(err)
	}
	sol := solicitudCorte + 21
	r1 := e.aviso("solicitado", sol, 1, "corte", "8801", nil)
	r2, err := e.avisoCon(campo.EntradaCorteArgos{Evento: "solicitado", SolicitudID: sol, Version: 1, ServicioID: 8801, ConectorID: otro})
	if err != nil || r2.Repetido || r2.OrdenID == nil || *r2.OrdenID == *r1.OrdenID {
		t.Fatalf("la del conector nuevo es otra solicitud: %+v %v (anterior %+v)", r2, err, r1)
	}
	if _, err := e.avisoCon(campo.EntradaCorteArgos{Evento: "cancelado", SolicitudID: sol, Version: 2, ServicioID: 8801, ConectorID: otro}); err != nil {
		t.Fatal(err)
	}
	if d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: *r1.OrdenID}))(t); d.Estado == campo.EstadoCancelada {
		t.Fatal("el cancelado del conector nuevo no cancela la orden del anterior")
	}
	if d := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: *r2.OrdenID}))(t); d.Estado != campo.EstadoCancelada {
		t.Fatalf("la del conector nuevo se cancela: %s", d.Estado)
	}
	// El estado de cada una sale con su conector de origen (mensajería solo lo manda a ese conector).
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'corte_manual.estado_cambiado'
		AND payload->>'solicitud_id' = $1 AND payload->>'conector_id' = $2 AND payload->>'estado' = 'cancelada'`,
		strconv.FormatInt(sol, 10), strconv.FormatInt(otro, 10)); n != 1 {
		t.Fatalf("estado con su conector: %d", n)
	}
}

// Candados orden → solicitud (revisión de #297): el cancelado de Argos mientras el lote cierra la orden no
// se interbloquea (40P01). Gana uno: cancelada, o resuelta (y el cancelado tardío la vuelve a informar).
func TestCorteManualCanceladoMientrasCierra(t *testing.T) {
	e := nuevoEntornoCorte(t)
	for i := range 25 {
		sol := solicitudCorte + 100 + int64(i)
		r := e.aviso("solicitado", sol, 1, "corte", "8801", nil)
		o := e.asignar(*r.OrdenID)
		// En sitio y con el checklist marcado: el lote que compite solo cierra (la transacción que toma la
		// orden y al final la solicitud).
		esperar(t, e.lote(e.ct, op(campo.OpLlegar, o.ID, nil),
			op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "resultado", "valor": "Hecho"})), "aplicada", "aplicada")
		var errLote, errAviso error
		var lote campo.RespuestaLote
		salida := make(chan struct{})
		listo := make(chan struct{})
		go func() {
			defer close(listo)
			<-salida
			lote, errLote = e.m.Lote(e.ct, campo.EntradaLote{DispositivoID: "tel-" + pruebas.Unico(), Plataforma: "pwa",
				Operaciones: []campo.OperacionLote{op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa"})}})
		}()
		close(salida)
		_, errAviso = e.avisoCon(campo.EntradaCorteArgos{Evento: "cancelado", SolicitudID: sol, Version: 2, ServicioID: 8801})
		<-listo
		for _, err := range []error{errLote, errAviso} {
			if err != nil {
				t.Fatalf("vuelta %d (¿interbloqueo?): %v", i, err)
			}
		}
		for _, res := range lote.Resultados {
			if res.Error != nil && res.Error.Status >= 500 {
				t.Fatalf("vuelta %d, el lote falló (¿interbloqueo?): %+v", i, res.Error)
			}
		}
		if u := e.ultimo(sol); u["estado"] != "cancelada" && u["estado"] != "resuelta" {
			t.Fatalf("vuelta %d: %v", i, u)
		}
	}
}

// Orden de los candados, sin depender del azar: mientras otra transacción tiene la orden (como el lote al
// cerrar), el cancelado espera por la orden sin haber tomado la solicitud; así el que cierra puede tomar la
// solicitud después (orden → solicitud) y nadie se interbloquea.
func TestCorteManualCanceladoEsperaLaOrdenAntesQueLaSolicitud(t *testing.T) {
	e := nuevoEntornoCorte(t)
	ctx := context.Background()
	sol := solicitudCorte + 200
	r := e.aviso("solicitado", sol, 1, "corte", "8801", nil)
	o := e.asignar(*r.OrdenID)
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT 1 FROM campo.ordenes WHERE id = $1 FOR UPDATE", o.ID); err != nil {
		t.Fatal(err)
	}
	listo := make(chan error, 1)
	go func() {
		_, err := e.avisoCon(campo.EntradaCorteArgos{Evento: "cancelado", SolicitudID: sol, Version: 2, ServicioID: 8801})
		listo <- err
	}()
	time.Sleep(300 * time.Millisecond) // el cancelado ya está esperando
	if _, err := tx.Exec(ctx, "SELECT 1 FROM campo.cortes_manuales WHERE conector_id = $1 AND solicitud_id = $2 FOR UPDATE NOWAIT", e.conector, sol); err != nil {
		t.Fatalf("el cancelado tomó la solicitud antes que la orden (interbloqueo con el cierre): %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-listo; err != nil {
		t.Fatal(err)
	}
	if u := e.ultimo(sol); u["estado"] != "cancelada" {
		t.Fatalf("cancelada: %v", u)
	}
}

// La plantilla del sistema del corte no pierde el ítem «resultado» ni cambia de tipo.
func TestCorteManualPlantillaProtegida(t *testing.T) {
	e := nuevoEntornoCorte(t)
	var id int64
	var version int32
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT id, version FROM campo.plantillas_checklist WHERE empresa_id IS NULL AND codigo = 'corte_catv'`).Scan(&id, &version); err != nil {
		t.Fatal(err)
	}
	admin := pruebas.ContextoSistema(pruebas.Pool(t))
	sinResultado := []campo.EntradaItemPlantilla{{Codigo: "observaciones", Etiqueta: "Observaciones", Tipo: "texto"}}
	_, err := e.m.EditarPlantilla.Ejecutar(admin, campo.EntradaEditarPlantilla{ID: id, Version: version, Nombre: "Corte", Tipo: ptr("corte_catv"), Items: sinResultado})
	problema(t, err, 422, "plantilla_corte_protegida")
	resultado := campo.EntradaItemPlantilla{Codigo: "resultado", Etiqueta: "¿Se cortó?", Tipo: "seleccion", Obligatorio: true, Opciones: campo.OpcionesResultadoCorte}
	_, err = e.m.EditarPlantilla.Ejecutar(admin, campo.EntradaEditarPlantilla{ID: id, Version: version, Nombre: "Corte", Tipo: ptr("visita"), Items: []campo.EntradaItemPlantilla{resultado}})
	problema(t, err, 422, "plantilla_corte_protegida")
	opcional := resultado
	opcional.Obligatorio = false
	_, err = e.m.EditarPlantilla.Ejecutar(admin, campo.EntradaEditarPlantilla{ID: id, Version: version, Nombre: "Corte", Tipo: ptr("corte_catv"), Items: []campo.EntradaItemPlantilla{opcional}})
	problema(t, err, 422, "plantilla_corte_protegida")
	// Con el ítem tal cual se puede editar lo demás (etiqueta, otros ítems).
	if _, err := e.m.EditarPlantilla.Ejecutar(admin, campo.EntradaEditarPlantilla{ID: id, Version: version, Nombre: "Corte de cable", Tipo: ptr("corte_catv"),
		Items: append([]campo.EntradaItemPlantilla{resultado}, sinResultado...)}); err != nil {
		t.Fatal(err)
	}
}
