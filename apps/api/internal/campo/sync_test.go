package campo_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func opID() string { return uuid.Must(uuid.NewV7()).String() }

func op(tipo string, orden int64, datos map[string]any) campo.OperacionLote {
	return campo.OperacionLote{ID: opID(), Tipo: tipo, OrdenID: orden, Datos: datos}
}

func lote(t *testing.T, e entorno, ctx context.Context, ops ...campo.OperacionLote) campo.RespuestaLote {
	t.Helper()
	r, err := e.m.Lote(ctx, campo.EntradaLote{DispositivoID: "tel-" + pruebas.Unico(), Plataforma: "pwa", Operaciones: ops})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Resultados) != len(ops) {
		t.Fatalf("un resultado por operación: %d de %d", len(r.Resultados), len(ops))
	}
	for i, res := range r.Resultados {
		if res.ID != ops[i].ID {
			t.Fatalf("resultado %d fuera de orden: %s ≠ %s", i, res.ID, ops[i].ID)
		}
	}
	return r
}

func estados(r campo.RespuestaLote) []string {
	s := []string{}
	for _, x := range r.Resultados {
		e := x.Estado
		if x.Error != nil {
			e += ":" + x.Error.Codigo()
		}
		s = append(s, e)
	}
	return s
}

func esperar(t *testing.T, r campo.RespuestaLote, quiero ...string) {
	t.Helper()
	if got := estados(r); !slices.Equal(got, quiero) {
		t.Fatalf("resultados %v, esperaba %v", got, quiero)
	}
}

// trabajoDeInstalacion son las operaciones para cerrar una instalación de fibra.
func trabajoDeInstalacion(orden int64) []campo.OperacionLote {
	foto := uuid.Must(uuid.NewV7()).String()
	return []campo.OperacionLote{
		op(campo.OpIniciarCamino, orden, map[string]any{"lat": 14.6, "lon": -90.5}),
		op(campo.OpLlegar, orden, nil),
		op(campo.OpChecklistMarcar, orden, map[string]any{"codigo": "acometida_tendida", "valor": true}),
		op(campo.OpChecklistMarcar, orden, map[string]any{"codigo": "potencia_rx", "valor": -19.8}),
		op(campo.OpChecklistMarcar, orden, map[string]any{"codigo": "onu_registrada", "valor": true}),
		op(campo.OpChecklistMarcar, orden, map[string]any{"codigo": "velocidad_bajada", "valor": 98.5}),
		op(campo.OpChecklistMarcar, orden, map[string]any{"codigo": "wifi_configurado", "valor": true}),
		op(campo.OpAdjuntoRegistrar, orden, map[string]any{"adjuntoId": foto, "tipo": "foto"}),
		op(campo.OpChecklistMarcar, orden, map[string]any{"codigo": "foto_onu", "valor": foto}),
		op(campo.OpEquipoInstalar, orden, map[string]any{"serie": "HWTC1234", "mac": "a4-5e-60-11-22-33"}),
		op(campo.OpMaterialUsar, orden, map[string]any{"productoId": 77, "cantidad": 7}),
		op(campo.OpNotaAgregar, orden, map[string]any{"texto": "Cliente conforme"}),
		op(campo.OpFirmaCapturar, orden, map[string]any{"nombre": "Ana López", "svg": "<svg><path d='M0 0'/></svg>"}),
		op(campo.OpCerrar, orden, map[string]any{"resultado": "exitosa"}),
	}
}

func repetir(n int, s string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func TestLoteAplicaEnOrdenEsIdempotenteYAudita(t *testing.T) {
	inv := &inventarioFalso{existencias: map[int64][]campo.Existencia{9: {{ProductoID: 77, Cantidad: 5}}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "instalacion", tec)
	o = deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec, BodegaID: ptr(int64(9))}))(t)

	ops := trabajoDeInstalacion(o.ID)
	ops = append(ops, ops[11]) // la misma nota dos veces en el lote
	r := lote(t, e, ct, ops...)
	esperar(t, r, append(repetir(14, "aplicada"), "duplicada")...)
	final := r.Resultados[13].Orden
	if req := e.adjuntos.requisitos[o.ID]; !slices.Equal(req, []campo.Requisito{{Tipo: "foto", ClaveChecklist: "foto_onu", Minimo: 1}, {Tipo: "firma", Minimo: 1}}) {
		t.Fatalf("requisitos de adjuntos: %+v", req)
	}
	if final == nil || final.Estado != "cerrada" {
		t.Fatalf("cerrada: %+v", r.Resultados[13])
	}
	// Material sobre la existencia (5) de la bodega: aceptado con aviso.
	if a := r.Resultados[10].Aviso; a == nil || *a != campo.AvisoExistenciaInsuficiente {
		t.Fatalf("aviso de existencia: %+v", r.Resultados[10])
	}
	c := ultimo(t, eventosDe(t, o.ID), "orden.cerrada")
	if m := c["materiales"].([]any)[0].(map[string]any); m["excede_existencia"] != true || m["cantidad"] != 7.0 || m["bodega_id"] != 9.0 {
		t.Fatalf("material en orden.cerrada: %v", m)
	}
	if eq := c["equipos"].([]any)[0].(map[string]any); eq["mac"] != "A4:5E:60:11:22:33" || eq["id"] != ops[9].ID {
		t.Fatalf("el equipo usa el id de la operación y la MAC normalizada: %v", eq)
	}
	det := deber(e.m.VerOrdenPropia.Ejecutar(ct, campo.EntradaVerOrden{ID: o.ID}))(t)
	if det.Notas[0].ID.String() != ops[11].ID || det.Materiales[0].ID.String() != ops[10].ID {
		t.Fatalf("nota y material con el id de la operación: %+v %+v", det.Notas, det.Materiales)
	}

	// Reenviar todo: duplicadas, con el resultado original y sin efectos nuevos.
	r2 := lote(t, e, ct, ops...)
	esperar(t, r2, repetir(15, "duplicada")...)
	if r2.Resultados[13].Orden.Version != final.Version {
		t.Fatalf("resultado original: %+v", r2.Resultados[13])
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_notas WHERE orden_id = $1", o.ID); n != 1 {
		t.Fatalf("notas: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.cerrada' AND (payload->>'orden_id')::bigint = $1", o.ID); n != 1 {
		t.Fatalf("orden.cerrada una vez: %d", n)
	}
	// Auditoría: cada operación aplicada como su acción, canal campo, con el id de la operación.
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.llegar' AND canal = 'campo'
		AND resultado = 'ok' AND entrada->'auditoria'->>'operacion_id' = $1`, ops[1].ID); n != 1 {
		t.Fatalf("auditoría de llegar: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM campo.operaciones_sync WHERE orden_id = $1 AND estado = 'aplicada'", o.ID); n != 14 {
		t.Fatalf("operaciones guardadas: %d", n)
	}
}

func TestLoteRechazoBloqueaSoloSuOrden(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	a := programada(t, e, cd, "instalacion", tec)
	b := programada(t, e, cd, "visita", tec)

	cerrarA := op(campo.OpCerrar, a.ID, nil)
	r := lote(t, e, ct,
		op(campo.OpLlegar, a.ID, nil),
		cerrarA, // sin checklist ni equipos
		op(campo.OpNotaAgregar, a.ID, map[string]any{"texto": "después del rechazo"}),
		op(campo.OpLlegar, b.ID, nil),
		op(campo.OpNotaAgregar, b.ID, map[string]any{"texto": "otra orden"}),
	)
	esperar(t, r, "aplicada", "rechazada:cierre_incompleto", "rechazada:bloqueada_por_rechazo", "aplicada", "aplicada")
	if r.Resultados[1].Orden == nil || r.Resultados[1].Orden.Estado != "en_sitio" || len(r.Resultados[1].Error.Errors) == 0 {
		t.Fatalf("el rechazo trae el estado de la orden y la lista: %+v", r.Resultados[1])
	}
	// El rechazo se reevalúa con el mismo id cuando el técnico completa el trabajo.
	trabajo := trabajoDeInstalacion(a.ID)[2:13]
	esperar(t, lote(t, e, ct, trabajo...), repetir(11, "aplicada")...)
	esperar(t, lote(t, e, ct, cerrarA), "aplicada")
	if n := contar(t, "SELECT count(*) FROM campo.operaciones_sync WHERE id = $1 AND estado = 'aplicada'", uuid.MustParse(cerrarA.ID)); n != 1 {
		t.Fatal("la operación quedó aplicada")
	}
}

func TestLoteReasignadaYCancelada(t *testing.T) {
	e := nuevoEntorno()
	d, t1, t2 := despacho(t), tecnico(t), tecnico(t)
	cd, c1 := como(t, d, "despacho"), como(t, t1, "tecnico")
	o := programada(t, e, cd, "visita", t1)
	esperar(t, lote(t, e, c1, op(campo.OpIniciarCamino, o.ID, nil)), "aplicada")

	// Despacho la reasigna: lo ya aplicado se conserva; lo nuevo del primero se rechaza, una por una.
	o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	o = deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version, TecnicoID: &t2}))(t)
	if o.Estado != "programada" || o.EnCaminoEn != nil {
		t.Fatalf("reasignada en camino vuelve a programada: %+v", o)
	}
	r := lote(t, e, c1, op(campo.OpLlegar, o.ID, nil), op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "x"}))
	esperar(t, r, "rechazada:orden_reasignada", "rechazada:orden_reasignada")
	if r.Resultados[0].Orden == nil || r.Resultados[0].Orden.Version != o.Version {
		t.Fatalf("el rechazo trae la orden: %+v", r.Resultados[0])
	}
	// Una orden que nunca fue suya: 404 (no revela nada).
	ajena := programada(t, e, cd, "visita", t2)
	r = lote(t, e, c1, op(campo.OpLlegar, ajena.ID, nil))
	esperar(t, r, "rechazada:no_encontrado")
	if r.Resultados[0].Orden != nil {
		t.Fatal("no revela la orden ajena")
	}

	// Cancelada: se rechaza, salvo notas y adjuntos (evidencia), aun después del rechazo.
	c2 := como(t, t2, "tecnico")
	esperar(t, lote(t, e, c2, op(campo.OpLlegar, o.ID, nil)), "aplicada")
	o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "Cliente canceló"}))(t)
	r = lote(t, e, c2,
		op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "diagnostico", "valor": "Fibra rota"}),
		op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "Llegué y ya estaba cancelada"}),
		op(campo.OpAdjuntoRegistrar, o.ID, map[string]any{"adjuntoId": opID(), "tipo": "foto"}),
		op(campo.OpCerrar, o.ID, nil),
	)
	esperar(t, r, "rechazada:orden_cancelada", "aplicada", "aplicada", "rechazada:orden_cancelada")
}

func TestLoteChecklistGanaElMasReciente(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")

	// Despacho corrige desde el panel a las 15:00.
	deber(e.m.MarcarChecklist.Ejecutar(cd, campo.EntradaMarcarChecklist{ID: o.ID, Codigo: ptr("diagnostico"), Valor: "Del panel"}))(t)
	// El teléfono marcó antes (14:50) y sincroniza después: queda en el historial, no gana.
	viejo := op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "diagnostico", "valor": "Del teléfono, antes"})
	viejo.OcurridoEn = ptr(inicio.Add(-10 * time.Minute))
	e.reloj.avanzar(time.Minute)
	r := lote(t, e, ct, viejo)
	esperar(t, r, "aplicada")
	if r.Resultados[0].Aviso == nil || *r.Resultados[0].Aviso != campo.AvisoChecklistNoVigente {
		t.Fatalf("aviso: %+v", r.Resultados[0])
	}
	valor := func() any {
		dd := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
		for _, it := range dd.Checklist {
			if it.Codigo == "diagnostico" {
				return it.Valor
			}
		}
		return nil
	}
	if v := valor(); v != "Del panel" {
		t.Fatalf("gana la más reciente: %v", v)
	}
	// Una marca posterior del teléfono gana.
	nuevo := op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "diagnostico", "valor": "Del teléfono, después"})
	nuevo.OcurridoEn = ptr(inicio.Add(30 * time.Second))
	esperar(t, lote(t, e, ct, nuevo), "aplicada")
	if v := valor(); v != "Del teléfono, después" {
		t.Fatalf("gana la más reciente: %v", v)
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_checklist_historial WHERE orden_id = $1", o.ID); n != 3 {
		t.Fatalf("historial de las tres marcas: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_checklist_historial WHERE orden_id = $1 AND NOT vigente", o.ID); n != 1 {
		t.Fatalf("una no vigente: %d", n)
	}
	// Valor del tipo equivocado → validación.
	esperar(t, lote(t, e, ct, op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "problema_resuelto", "valor": "sí"})),
		"rechazada:validacion")
}

func TestLoteFaltanAdjuntos(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "mantenimiento", tec)
	foto := opID()
	esperar(t, lote(t, e, ct,
		op(campo.OpLlegar, o.ID, nil),
		op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": "trabajo_realizado", "valor": "Cambio de conector"}),
		op(campo.OpAdjuntoRegistrar, o.ID, map[string]any{"adjuntoId": foto, "tipo": "foto"}),
	), "aplicada", "aplicada", "aplicada")
	e.adjuntos.fijar(o.ID, "foto:foto_evidencia")
	cerrar := op(campo.OpCerrar, o.ID, nil)
	r := lote(t, e, ct, cerrar)
	esperar(t, r, "rechazada:faltan_adjuntos")
	if errs := r.Resultados[0].Error.Errors; len(errs) != 1 || errs[0].Field != "adjuntos.foto:foto_evidencia" {
		t.Fatalf("lista de faltantes: %+v", errs)
	}
	e.adjuntos.fijar(o.ID)
	esperar(t, lote(t, e, ct, cerrar), "aplicada")

	// Un adjunto que el módulo de adjuntos no reconoce como de la orden se rechaza.
	ajeno := uuid.Must(uuid.NewV7())
	e.adjuntos.ajenos[ajeno] = true
	esperar(t, lote(t, e, ct, op(campo.OpAdjuntoRegistrar, o.ID, map[string]any{"adjuntoId": ajeno.String(), "tipo": "foto"})),
		"rechazada:no_encontrado")
}

func TestLoteDesfaseDeReloj(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)

	// El teléfono va 2 minutos atrasado: marcó la llegada «hace 3 minutos» en su reloj.
	enviado := inicio.Add(-2 * time.Minute)
	llegar := op(campo.OpLlegar, o.ID, nil)
	llegar.OcurridoEn = ptr(enviado.Add(-3 * time.Minute))
	futuro := op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "reloj adelantado"})
	futuro.OcurridoEn = ptr(inicio.Add(time.Hour))
	r, err := e.m.Lote(ct, campo.EntradaLote{DispositivoID: "tel-desfase-" + pruebas.Unico(), Plataforma: "android",
		EnviadoEn: &enviado, Operaciones: []campo.OperacionLote{llegar, futuro}})
	if err != nil {
		t.Fatal(err)
	}
	esperar(t, r, "aplicada", "aplicada")
	if r.DesfaseMs != 120000 || !r.ServidorEn.Equal(inicio) {
		t.Fatalf("desfase: %+v", r)
	}
	var en, tel time.Time
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT en_sitio_en, (SELECT ocurrido_en_telefono FROM campo.operaciones_sync WHERE id = $2) FROM campo.ordenes WHERE id = $1",
		o.ID, uuid.MustParse(llegar.ID)).Scan(&en, &tel); err != nil {
		t.Fatal(err)
	}
	if !en.Equal(inicio.Add(-3*time.Minute)) || !tel.Equal(*llegar.OcurridoEn) {
		t.Fatalf("hora corregida %s (teléfono %s)", en, tel)
	}
	var nota time.Time
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT ocurrido_en FROM campo.orden_notas WHERE orden_id = $1", o.ID).Scan(&nota); err != nil || !nota.Equal(inicio) {
		t.Fatalf("nunca en el futuro: %s %v", nota, err)
	}
	if n := contar(t, "SELECT count(*) FROM campo.dispositivos WHERE usuario_id = $1 AND desfase_ms = 120000 AND plataforma = 'android'", tec); n != 1 {
		t.Fatalf("dispositivo: %d", n)
	}
}

func TestPaqueteYCambios(t *testing.T) {
	inv := &inventarioFalso{
		existencias: map[int64][]campo.Existencia{9: {{ProductoID: 77, Descripcion: "Cable drop", Cantidad: 120, Unidad: "m"}}},
		comodato:    map[int64][]campo.EquipoComodato{9901: {{UnidadID: 5, Serie: "ZTE1", Modelo: "F660"}}},
	}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	d, t1, t2 := despacho(t), tecnico(t), tecnico(t)
	cd, c1 := como(t, d, "despacho"), como(t, t1, "tecnico")

	hoy := programada(t, e, cd, "instalacion", t1)
	hoy = deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: hoy.ID, Version: hoy.Version, TecnicoID: &t1, BodegaID: ptr(int64(9))}))(t)
	conServicio := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "retiro", Direccion: ptr("Villa Nueva"),
		ClienteID: ptr(int64(990)), ServicioID: ptr(int64(9901))}))(t)
	conServicio = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: conServicio.ID, Version: conServicio.Version,
		TecnicoID: &t1, Inicio: inicio.Add(2 * time.Hour)}))(t)
	manana := crearOrden(t, e, cd, "visita")
	manana = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: manana.ID, Version: manana.Version,
		TecnicoID: &t1, Inicio: inicio.Add(24 * time.Hour)}))(t)
	ajena := programada(t, e, cd, "visita", t2)

	p, err := e.m.Paquete(c1, campo.EntradaPaquete{})
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, o := range p.Ordenes {
		ids = append(ids, o.ID)
	}
	if p.Fecha != "2026-10-28" || !slices.Equal(ids, []int64{hoy.ID, conServicio.ID}) || p.Cursor == "" || p.Ruta != nil {
		t.Fatalf("paquete de hoy: %s %v", p.Fecha, ids)
	}
	if len(p.Ordenes[0].Checklist) != 9 || !p.Ordenes[0].RequiereFirma {
		t.Fatalf("checklist de la plantilla: %+v", p.Ordenes[0].Checklist)
	}
	if !p.Existencias.Disponible || len(p.Existencias.Bodegas) != 1 || p.Existencias.Bodegas[0].Lineas[0].Cantidad != 120 {
		t.Fatalf("existencias: %+v", p.Existencias)
	}
	if co := p.Ordenes[1].Comodato; !co.Disponible || len(co.Equipos) != 1 || co.Equipos[0].Serie != "ZTE1" {
		t.Fatalf("comodato: %+v", co)
	}
	if co := p.Ordenes[0].Comodato; co.Disponible || co.Motivo == nil || *co.Motivo != campo.MotivoSinServicio {
		t.Fatalf("sin servicio: %+v", co)
	}
	pm := deber(e.m.Paquete(c1, campo.EntradaPaquete{Fecha: "2026-10-29"}))(t)
	if len(pm.Ordenes) != 3 {
		t.Fatalf("mañana trae también las de hoy pendientes: %d", len(pm.Ordenes))
	}

	// Sin conector: vacío y marcado.
	sin := nuevoEntorno()
	ps := deber(sin.m.Paquete(c1, campo.EntradaPaquete{}))(t)
	if ps.Existencias.Disponible || ps.Existencias.Motivo == nil || *ps.Existencias.Motivo != campo.MotivoSinConector ||
		ps.Ordenes[0].Comodato.Disponible || *ps.Ordenes[0].Comodato.Motivo != campo.MotivoSinConector {
		t.Fatalf("sin conector: %+v", ps.Existencias)
	}

	// Cambios: reprogramada, cancelada, reasignada (sin orden), nueva y la ajena nunca.
	cursor := p.Cursor
	vacio := deber(e.m.Cambios(c1, campo.EntradaCambios{Cursor: cursor}))(t)
	if len(vacio.Cambios) != 0 || vacio.Cursor != cursor {
		t.Fatalf("sin cambios: %+v", vacio)
	}
	hoy = deber(e.m.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: hoy.ID, Version: hoy.Version, Inicio: inicio.Add(3 * time.Hour), Motivo: ptr("Tráfico")}))(t)
	deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: conServicio.ID, Version: conServicio.Version, Motivo: "Desistió"}))(t)
	deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: manana.ID, Version: manana.Version, TecnicoID: &t2}))(t)
	deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: ajena.ID, Version: ajena.Version, TecnicoID: &t1}))(t)

	c := deber(e.m.Cambios(c1, campo.EntradaCambios{Cursor: cursor}))(t)
	tipos := map[int64]string{}
	for _, x := range c.Cambios {
		tipos[x.OrdenID] = x.Tipo
		if (x.Tipo == "reasignada") != (x.Orden == nil) {
			t.Fatalf("orden ausente solo si se reasignó: %+v", x)
		}
	}
	if tipos[hoy.ID] != "reprogramada" || tipos[conServicio.ID] != "cancelada" || tipos[manana.ID] != "reasignada" ||
		tipos[ajena.ID] != "nueva" || len(c.Cambios) != 4 || c.HayMas {
		t.Fatalf("cambios: %v", tipos)
	}
	// Paginado y fin.
	p1 := deber(e.m.Cambios(c1, campo.EntradaCambios{Cursor: cursor, Limit: 2}))(t)
	if !p1.HayMas || len(p1.Cambios) == 0 {
		t.Fatalf("página 1: %+v", p1)
	}
	if fin := deber(e.m.Cambios(c1, campo.EntradaCambios{Cursor: c.Cursor}))(t); len(fin.Cambios) != 0 {
		t.Fatalf("después del último cursor: %+v", fin)
	}
	// El otro técnico ve su nueva y no la que perdió... ni la de t1.
	c2 := deber(e.m.Cambios(como(t, t2, "tecnico"), campo.EntradaCambios{}))(t)
	vistos := map[int64]string{}
	for _, x := range c2.Cambios {
		vistos[x.OrdenID] = x.Tipo
	}
	if vistos[manana.ID] != "nueva" || vistos[ajena.ID] != "reasignada" || vistos[hoy.ID] != "" {
		t.Fatalf("cambios de t2: %v", vistos)
	}
}
