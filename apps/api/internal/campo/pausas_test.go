package campo_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Pausar y reanudar desde el panel: estados, motivos, tiempo pausado, efectos en el cierre, la
// agenda y la ruta.
func TestPausarYReanudar(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	cs := supervisor(t)

	o := programada(t, e, cd, "instalacion", tec)
	_, err := e.m.PausarOrden.Ejecutar(ct, campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "clima"})
	problema(t, err, 422, "transicion_invalida") // desde programada no
	// Ruta publicada del día (sin OSRM: degradada).
	ag := e.m.Agenda()
	r := deber(ag.Proponer.Ejecutar(cs, agenda.EntradaProponerRuta{TecnicoID: tec, Fecha: "2026-10-28"}))(t)
	r = deber(ag.Publicar.Ejecutar(cs, agenda.EntradaPublicarRuta{ID: r.ID, Version: r.Version}))(t)

	o = deber(e.m.IniciarCamino.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)

	// Motivo «otro» sin nota: 422; lectura no pausa.
	_, err = e.m.PausarOrden.Ejecutar(ct, campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "otro"})
	if c := problema(t, err, 422, "validacion"); c[0].Field != "nota" {
		t.Fatalf("nota: %+v", c)
	}
	_, err = e.m.PausarOrden.Ejecutar(como(t, usuarioCon(t, rolDe("lectura")), "lectura"), campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "clima"})
	problema(t, err, 403, "prohibido")
	// Otro técnico no la ve.
	_, err = e.m.PausarOrden.Ejecutar(como(t, tecnico(t), "tecnico"), campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "clima"})
	problema(t, err, 404, "no_encontrado")

	o = deber(e.m.PausarOrden.Ejecutar(ct, campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "esperando_material", Nota: "Falta conector SC/APC"}))(t)
	if o.Estado != "pausada" || o.PausadaDesde == nil || *o.PausadaDesde != "en_sitio" || o.PausadaEn == nil || !o.PausadaEn.Equal(inicio) {
		t.Fatalf("pausada: %+v", o)
	}
	_, err = e.m.PausarOrden.Ejecutar(ct, campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "clima"})
	problema(t, err, 409, "orden_ya_pausada")
	ev := ultimo(t, eventosDe(t, o.ID), "orden.estado_cambiada")
	if ev["de"] != "en_sitio" || ev["a"] != "pausada" || ev["motivo"] != "esperando_material: Falta conector SC/APC" {
		t.Fatalf("orden.estado_cambiada: %v", ev)
	}
	if p := ultimo(t, eventosDe(t, o.ID), "orden.pausada"); p["motivo"] != "esperando_material" || p["desde"] != "en_sitio" {
		t.Fatalf("orden.pausada: %v", p)
	}

	// Pausada: no se cierra, falla, llega ni reasigna sin reanudar; sí admite evidencia.
	for _, err := range []error{
		func() error {
			_, err := e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
			return err
		}(),
		func() error {
			_, err := e.m.FallarOrden.Ejecutar(ct, campo.EntradaFallarOrden{ID: o.ID, Version: o.Version, MotivoFalla: "clima"})
			return err
		}(),
		func() error {
			_, err := e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version})
			return err
		}(),
		func() error {
			_, err := e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version})
			return err
		}(),
	} {
		problema(t, err, 422, "orden_pausada")
	}
	o = deber(e.m.AgregarNota.Ejecutar(ct, campo.EntradaAgregarNota{ID: o.ID, Texto: "Espero al proveedor"}))(t)
	dt := detalle(t, e, o.ID)
	it := dt.Checklist[0]
	o = deber(e.m.MarcarChecklist.Ejecutar(ct, campo.EntradaMarcarChecklist{ID: o.ID, ItemID: &it.ItemID, Valor: true}))(t)

	// Sigue abierta: en el paquete, en el calendario (pausada) y en la ruta, que no se marca
	// desactualizada (la cita no se movió).
	paq := deber(e.m.Paquete(ct, campo.EntradaPaquete{Fecha: "2026-10-28"}))(t)
	if k := slices.IndexFunc(paq.Ordenes, func(x campo.OrdenCampo) bool { return x.ID == o.ID }); k < 0 || paq.Ordenes[k].Cita == nil ||
		paq.Ruta == nil || !slices.Contains(paq.Ruta.OrdenIDs, o.ID) || paq.Ruta.Desactualizada {
		t.Fatalf("paquete con la pausada: %+v", paq.Ruta)
	}
	cal := deber(ag.Calendario.Ejecutar(cd, agenda.EntradaCalendario{Desde: "2026-10-28T00:00:00Z", Hasta: "2026-10-29T00:00:00Z"}))(t)
	if k := slices.IndexFunc(cal.Citas, func(c agenda.CitaAgenda) bool { return c.OrdenID == o.ID }); k < 0 || cal.Citas[k].Estado != "pausada" {
		t.Fatalf("calendario: %+v", cal.Citas)
	}
	if v := deber(ag.VerRuta.Ejecutar(cs, agenda.EntradaVerRuta{ID: r.ID}))(t); v.Desactualizada {
		t.Fatal("pausar no desactualiza la ruta")
	}

	// Reanudar diez minutos después: vuelve a en_sitio con el tiempo pausado.
	e.reloj.avanzar(10 * time.Minute)
	_, err = e.m.ReanudarOrden.Ejecutar(como(t, usuarioCon(t, rolDe("lectura")), "lectura"), campo.EntradaReanudar{ID: o.ID, Version: o.Version})
	problema(t, err, 403, "prohibido")
	o = deber(e.m.ReanudarOrden.Ejecutar(ct, campo.EntradaReanudar{ID: o.ID, Version: o.Version}))(t)
	if o.Estado != "en_sitio" || o.TiempoPausadoSeg != 600 || o.PausadaEn != nil || o.PausadaDesde != nil {
		t.Fatalf("reanudada: %+v", o)
	}
	_, err = e.m.ReanudarOrden.Ejecutar(ct, campo.EntradaReanudar{ID: o.ID, Version: o.Version})
	problema(t, err, 409, "orden_no_pausada")
	if p := ultimo(t, eventosDe(t, o.ID), "orden.reanudada"); p["a"] != "en_sitio" || p["duracion_seg"] != float64(600) || p["tiempo_pausado_seg"] != float64(600) {
		t.Fatalf("orden.reanudada: %v", p)
	}

	// Despacho la pausa otra vez y la cancela: la pausa se cierra y su tiempo se suma.
	o = deber(e.m.PausarOrden.Ejecutar(cd, campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "cliente_ausente"}))(t)
	e.reloj.avanzar(5 * time.Minute)
	o = deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "El cliente no volvió"}))(t)
	dt = detalle(t, e, o.ID)
	if o.Estado != "cancelada" || o.TiempoPausadoSeg != 900 || len(dt.Pausas) != 2 || *dt.Pausas[0].Cierre != "reanudada" ||
		*dt.Pausas[0].DuracionSeg != 600 || *dt.Pausas[1].Cierre != "cancelada" || dt.Pausas[1].CerradaPor == nil || *dt.Pausas[1].CerradaPor != d {
		t.Fatalf("pausas: %+v / %+v", o, dt.Pausas)
	}

	// En camino → pausada → reprogramar: programada, con la pausa cerrada.
	o2 := programada(t, e, cd, "visita", tec)
	o2 = deber(e.m.IniciarCamino.Ejecutar(ct, campo.EntradaPosicion{ID: o2.ID, Version: o2.Version}))(t)
	o2 = deber(e.m.PausarOrden.Ejecutar(ct, campo.EntradaPausar{ID: o2.ID, Version: o2.Version, Motivo: "acceso"}))(t)
	if *o2.PausadaDesde != "en_camino" {
		t.Fatalf("desde en camino: %+v", o2)
	}
	o2 = deber(e.m.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o2.ID, Version: o2.Version,
		Inicio: inicio.Add(26 * time.Hour), Motivo: ptr("Sin acceso hoy")}))(t)
	if dt := detalle(t, e, o2.ID); o2.Estado != "programada" || len(dt.Pausas) != 1 || *dt.Pausas[0].Cierre != "reprogramada" || o2.PausadaEn != nil {
		t.Fatalf("reprogramada: %+v %+v", o2, dt.Pausas)
	}
}

// Pausar y reanudar sin señal (orden.pausar y orden.reanudar) con sus conflictos.
func TestPausaPorSync(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")

	// Pausa a las 15:05 y reanuda a las 15:25 (hora del teléfono): 20 minutos.
	e.reloj.avanzar(30 * time.Minute)
	pausar := op(campo.OpPausar, o.ID, map[string]any{"motivo": "clima", "nota": "Tormenta"})
	pausar.OcurridoEn = ptr(inicio.Add(5 * time.Minute))
	nota := op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "Bajo techo"})
	reanudar := op(campo.OpReanudar, o.ID, nil)
	reanudar.OcurridoEn = ptr(inicio.Add(25 * time.Minute))
	r := lote(t, e, ct, pausar, nota, reanudar)
	esperar(t, r, "aplicada", "aplicada", "aplicada")
	if r.Resultados[0].Orden.Estado != "pausada" || r.Resultados[2].Orden.Estado != "en_sitio" {
		t.Fatalf("estados: %+v", r.Resultados)
	}
	dt := detalle(t, e, o.ID)
	if len(dt.Pausas) != 1 || !dt.Pausas[0].Inicio.Equal(inicio.Add(5*time.Minute)) || *dt.Pausas[0].DuracionSeg != 1200 || dt.TiempoPausadoSeg != 1200 {
		t.Fatalf("pausa con la hora del teléfono: %+v", dt.Pausas)
	}
	esperar(t, lote(t, e, ct, pausar), "duplicada")

	// Reanudar una que no está pausada (despacho ya la reanudó): rechazada y bloquea las
	// siguientes de la orden.
	esperar(t, lote(t, e, ct, op(campo.OpReanudar, o.ID, nil), op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "x"})),
		"rechazada:orden_no_pausada", "rechazada:bloqueada_por_rechazo")
	// Pausar dos veces (otro teléfono ya la pausó): 409.
	o = detalle(t, e, o.ID).Orden
	deber(e.m.PausarOrden.Ejecutar(cd, campo.EntradaPausar{ID: o.ID, Version: o.Version, Motivo: "acceso"}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpPausar, o.ID, map[string]any{"motivo": "clima"})), "rechazada:orden_ya_pausada")
	// Con la orden pausada, el cierre por lote: 422 orden_pausada.
	esperar(t, lote(t, e, ct, op(campo.OpCerrar, o.ID, nil)), "rechazada:orden_pausada")
	// Motivo fuera del catálogo: esquema.
	esperar(t, lote(t, e, ct, op(campo.OpPausar, o.ID, map[string]any{"motivo": "almuerzo"})), "rechazada:validacion")

	// Cancelada mientras el técnico estaba sin señal: orden_cancelada (no bloquea).
	o = detalle(t, e, o.ID).Orden
	deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "Se canceló"}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpReanudar, o.ID, nil), op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "evidencia"})),
		"rechazada:orden_cancelada", "aplicada")

	// Reasignada: orden_reasignada.
	o2 := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpIniciarCamino, o2.ID, nil)), "aplicada")
	o2 = detalle(t, e, o2.ID).Orden
	otro := tecnico(t)
	deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o2.ID, Version: o2.Version, TecnicoID: &otro}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpPausar, o2.ID, map[string]any{"motivo": "clima"})), "rechazada:orden_reasignada")
}

// La migración sobre órdenes existentes: su plantilla pasa a ser el primer formulario (origen
// tipo, versión 1) y sus marcas quedan ligadas a él. Se reconstruye el estado previo en una
// transacción (sección Down), se insertan datos como los de main, se corre la sección Up y se
// revierte todo al final.
func TestMigracionFormulariosDeOrdenesExistentes(t *testing.T) {
	crudo, err := os.ReadFile("../../migraciones/20261028215056_campo_formularios_pausas.sql")
	if err != nil {
		t.Fatal(err)
	}
	partes := strings.SplitN(string(crudo), "-- +goose Down", 2)
	up, down := strings.SplitN(partes[0], "-- +goose Up", 2)[1], partes[1]
	ctx := context.Background()
	tx, err := pruebas.Pool(t).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%.200s: %v", sql, err)
		}
	}
	uno := func(sql string, args ...any) (s string) {
		t.Helper()
		if err := tx.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
			t.Fatalf("%.200s: %v", sql, err)
		}
		return s
	}
	// Lo que dejaron otras pruebas que la sección Down no admite (mismo código en dos empresas).
	exec(`UPDATE campo.plantillas_checklist SET codigo = codigo || '_' || id WHERE empresa_id IS NOT NULL`)
	exec(down)
	// Una orden de instalación de fibra con dos marcas (y su historial) y una sin plantilla.
	orden := uno(`INSERT INTO campo.ordenes (tipo_id, plantilla_id, direccion)
		SELECT t.id, p.id, 'Migrada' FROM campo.tipos_orden t, campo.plantillas_checklist p
		WHERE t.codigo = 'instalacion' AND p.codigo = 'instalacion_fibra' RETURNING id::text`)
	sinPlantilla := uno(`INSERT INTO campo.ordenes (tipo_id, direccion) SELECT id, 'Sin plantilla' FROM campo.tipos_orden
		WHERE codigo = 'visita' RETURNING id::text`)
	exec(`INSERT INTO campo.orden_checklist (orden_id, item_id, valor, ocurrido_en)
		SELECT $1::bigint, i.id, 'true', now() FROM campo.plantilla_items i JOIN campo.plantillas_checklist p ON p.id = i.plantilla_id
		WHERE p.codigo = 'instalacion_fibra' AND i.codigo IN ('acometida_tendida', 'onu_registrada')`, orden)
	exec(`INSERT INTO campo.orden_checklist_historial (orden_id, item_id, valor, ocurrido_en, vigente)
		SELECT orden_id, item_id, valor, ocurrido_en, true FROM campo.orden_checklist WHERE orden_id = $1::bigint`, orden)

	exec(up)

	if got := uno(`SELECT f.origen || '|' || v.numero || '|' || p.codigo || '|' || (f.quitado_en IS NULL)
		FROM campo.orden_formularios f JOIN campo.plantilla_versiones v ON v.id = f.version_id
		JOIN campo.plantillas_checklist p ON p.id = f.plantilla_id WHERE f.orden_id = $1::bigint`, orden); got != "tipo|1|instalacion_fibra|true" {
		t.Fatalf("primer formulario: %s", got)
	}
	if got := uno(`SELECT count(*)::text FROM campo.orden_formularios WHERE orden_id = $1::bigint`, sinPlantilla); got != "0" {
		t.Fatalf("sin plantilla, sin formularios: %s", got)
	}
	if got := uno(`SELECT count(*)::text || '|' || count(*) FILTER (WHERE c.formulario_id = f.id)::text
		FROM campo.orden_checklist c JOIN campo.orden_formularios f ON f.orden_id = c.orden_id WHERE c.orden_id = $1::bigint`, orden); got != "2|2" {
		t.Fatalf("marcas ligadas al formulario: %s", got)
	}
	if got := uno(`SELECT count(*)::text FROM campo.orden_checklist_historial WHERE orden_id = $1::bigint AND formulario_id IS NOT NULL`, orden); got != "2" {
		t.Fatalf("historial ligado: %s", got)
	}
	// Todas las plantillas con su versión 1 y los ítems en ella.
	if got := uno(`SELECT count(*)::text FROM campo.plantillas_checklist p JOIN campo.plantilla_versiones v ON v.id = p.version_actual_id
		WHERE v.numero <> 1 OR NOT EXISTS (SELECT 1 FROM campo.plantilla_items i WHERE i.version_id = v.id)`); got != "0" {
		t.Fatalf("plantillas sin versión 1 con ítems: %s", got)
	}
}

// Revisión de la PR #29, punto 3: una pausa con la hora del teléfono no se encima a la anterior
// (se ajusta a su fin, con aviso) y el tiempo pausado nunca supera lo transcurrido desde que salió.
func TestRevisionPausaNoSeEncima(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpIniciarCamino, o.ID, nil)), "aplicada") // 15:00
	en := func(tipo string, datos map[string]any, m time.Duration) campo.OperacionLote {
		x := op(tipo, o.ID, datos)
		x.OcurridoEn = ptr(inicio.Add(m))
		return x
	}
	e.reloj.avanzar(60 * time.Minute)
	// Pausa de 15:10 a 15:40.
	esperar(t, lote(t, e, ct, en(campo.OpPausar, map[string]any{"motivo": "clima"}, 10*time.Minute),
		en(campo.OpReanudar, nil, 40*time.Minute)), "aplicada", "aplicada")
	// Otra que dice empezar a las 15:05 (antes del fin de la anterior) y termina a las 15:50.
	r := lote(t, e, ct, en(campo.OpPausar, map[string]any{"motivo": "acceso"}, 5*time.Minute), en(campo.OpReanudar, nil, 50*time.Minute))
	esperar(t, r, "aplicada", "aplicada")
	if r.Resultados[0].Aviso == nil || *r.Resultados[0].Aviso != campo.AvisoPausaAjustada {
		t.Fatalf("aviso: %+v", r.Resultados[0])
	}
	dt := detalle(t, e, o.ID)
	if !dt.Pausas[1].Inicio.Equal(inicio.Add(40*time.Minute)) || dt.TiempoPausadoSeg != 40*60 {
		t.Fatalf("ajustada al fin de la anterior (30 + 10 min): %+v, %d", dt.Pausas[1], dt.TiempoPausadoSeg)
	}
	if transcurrido := int64(e.reloj.ahora().Sub(*dt.EnCaminoEn) / time.Second); dt.TiempoPausadoSeg > transcurrido {
		t.Fatalf("pausado %d > transcurrido %d", dt.TiempoPausadoSeg, transcurrido)
	}
}
