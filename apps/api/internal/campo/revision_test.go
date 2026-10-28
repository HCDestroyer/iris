package campo_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Regresiones de la revisión de la PR de campo (una por punto).

// 1. El técnico usa siempre la bodega de la orden; otra solo la indica despacho y debe existir
// en la empresa según Argos.
func TestRevisionBodegaDeLaOrden(t *testing.T) {
	inv := &inventarioFalso{existencias: map[int64][]campo.Existencia{9: {{ProductoID: 77, Cantidad: 50}}, 12: {}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := crearOrden(t, e, cd, "instalacion")
	o = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec,
		BodegaID: ptr(int64(9)), Inicio: inicio}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil),
		op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 2, "bodegaId": 4242}),
	), "aplicada", "rechazada:validacion")
	_, err := e.m.UsarMaterialPanel(ct, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 77, Cantidad: 1, BodegaID: ptr(int64(12))})
	problema(t, err, 403, "prohibido")
	_, err = e.m.UsarMaterialPanel(cd, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 77, Cantidad: 1, BodegaID: ptr(int64(4242))})
	problema(t, err, 422, "bodega_invalida")
	sin := nuevoEntorno()
	_, err = sin.m.UsarMaterialPanel(cd, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 77, Cantidad: 1, BodegaID: ptr(int64(12))})
	problema(t, err, 422, "bodega_invalida")
	deber(e.m.UsarMaterialPanel(cd, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 77, Cantidad: 1, BodegaID: ptr(int64(12))}))(t)
	deber(e.m.UsarMaterialPanel(ct, campo.EntradaUsarMaterial{ID: o.ID, ProductoID: 77, Cantidad: 1}))(t)
	if n := contar(t, "SELECT count(*) FROM campo.orden_materiales WHERE orden_id = $1 AND bodega_id = 12", o.ID); n != 1 {
		t.Fatalf("material de despacho en la bodega 12: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_materiales WHERE orden_id = $1 AND bodega_id = 9", o.ID); n != 1 {
		t.Fatalf("material del técnico en la bodega de la orden: %d", n)
	}
}

// 2. Los datos del lote pasan por el esquema de la entrada de la acción.
func TestRevisionDatosDelLoteConEsquema(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "retiro", tec)
	largo := strings.Repeat("a", 9000)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, map[string]any{"lat": 14.6, "lon": -90.5, "precision": -1})), "rechazada:validacion")
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")
	for _, mala := range []campo.OperacionLote{
		op(campo.OpMaterialUsar, o.ID, map[string]any{"productoId": 77, "cantidad": 1, "descripcion": largo}),
		op(campo.OpEquipoRetirar, o.ID, map[string]any{"unidadId": -5, "condicion": "buena"}),
		op(campo.OpEquipoRetirar, o.ID, map[string]any{"serie": "X1", "condicion": "robado"}),
		op(campo.OpFirmaCapturar, o.ID, map[string]any{"nombre": "Ana", "documento": largo, "svg": "<svg/>xxxxxxx"}),
		op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "ok", "desconocido": 1}),
		op(campo.OpChecklistMarcar, o.ID, map[string]any{"codigo": strings.Repeat("c", 100), "valor": true}),
	} {
		r := lote(t, e, ct, mala)
		esperar(t, r, "rechazada:validacion")
		if f := r.Resultados[0].Error.Errors; len(f) == 0 || !strings.HasPrefix(f[0].Field, "datos.") {
			t.Fatalf("errores por campo de datos (%s): %+v", mala.Tipo, f)
		}
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_equipos WHERE orden_id = $1", o.ID); n != 0 {
		t.Fatalf("nada entró: %d", n)
	}
}

// 3. ocurridoEn no va más atrás que el estado de la orden ni que 7 días.
func TestRevisionOcurridoEnAcotado(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	e.reloj.fijar(inicio.Add(-30 * 24 * time.Hour)) // asignada hace 30 días
	o := programada(t, e, cd, "visita", tec)
	e.reloj.fijar(inicio)
	viejo := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	nota := op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "fechada en 2001"})
	nota.OcurridoEn = &viejo
	esperar(t, lote(t, e, ct, nota), "aplicada")
	var en time.Time
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT ocurrido_en FROM campo.orden_notas WHERE orden_id = $1", o.ID).Scan(&en); err != nil ||
		!en.Equal(inicio.Add(-7*24*time.Hour)) {
		t.Fatalf("acotada a 7 días: %s %v", en, err)
	}
	e.reloj.avanzar(time.Hour)
	llegar := op(campo.OpLlegar, o.ID, nil)
	llegar.OcurridoEn = ptr(inicio.Add(10 * time.Minute))
	fallar := op(campo.OpFallar, o.ID, map[string]any{"motivoFalla": "clima"})
	fallar.OcurridoEn = &viejo
	esperar(t, lote(t, e, ct, llegar, fallar), "aplicada", "aplicada")
	var sitio, fin, tel time.Time
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT en_sitio_en, finalizada_en,
		(SELECT ocurrido_en_telefono FROM campo.operaciones_sync WHERE id = $2::uuid) FROM campo.ordenes WHERE id = $1`, o.ID, fallar.ID).
		Scan(&sitio, &fin, &tel); err != nil {
		t.Fatal(err)
	}
	if !sitio.Equal(inicio.Add(10*time.Minute)) || !fin.Equal(sitio) || !tel.Equal(viejo) {
		t.Fatalf("la falla no va antes de la llegada: sitio %s fin %s teléfono %s", sitio, fin, tel)
	}
}

// 4. Equipos coherentes con el tipo y, con conector, con el comodato del servicio.
func TestRevisionEquiposCoherentes(t *testing.T) {
	inv := &inventarioFalso{comodato: map[int64][]campo.EquipoComodato{9901: {{UnidadID: 5, Serie: "ZTE1"}}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	inst := programada(t, e, cd, "instalacion", tec)
	vis := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, inst.ID, nil), op(campo.OpLlegar, vis.ID, nil),
		op(campo.OpEquipoRetirar, inst.ID, map[string]any{"serie": "A", "condicion": "buena"}),
		op(campo.OpEquipoInstalar, vis.ID, map[string]any{"serie": "B"}),
	), "aplicada", "aplicada", "rechazada:equipo_no_admitido", "rechazada:equipo_no_admitido")

	ret := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "retiro", Direccion: ptr("x"),
		ClienteID: ptr(int64(990)), ServicioID: ptr(int64(9901))}))(t)
	ret = deber(e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: ret.ID, Version: ret.Version, TecnicoID: &tec, Inicio: inicio}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, ret.ID, nil),
		op(campo.OpEquipoRetirar, ret.ID, map[string]any{"serie": "OTRO", "condicion": "buena"}),
	), "aplicada", "rechazada:equipo_no_en_comodato")
	esperar(t, lote(t, e, ct, op(campo.OpEquipoRetirar, ret.ID, map[string]any{"serie": "zte1", "condicion": "danada"})), "aplicada")
	_, err := e.m.RetirarEquipoPanel(cd, campo.EntradaEquipo{ID: ret.ID, UnidadID: ptr(int64(77)), Condicion: ptr("perdido")})
	problema(t, err, 422, "equipo_no_en_comodato")
	completarChecklist(t, e, ct, ret.ID)
	deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: ret.ID, Nombre: "Ana", SVG: "<svg><path/></svg>"}))(t)
	esperar(t, lote(t, e, ct, op(campo.OpCerrar, ret.ID, nil)), "aplicada")
	c := ultimo(t, eventosDe(t, ret.ID), "orden.cerrada")
	if c["equipos_validados"] != true || c["equipos"].([]any)[0].(map[string]any)["validado"] != true {
		t.Fatalf("validado contra el comodato: %v", c)
	}

	// Sin conector: se acepta, pero queda como no verificado.
	sin := nuevoEntorno()
	r2 := programada(t, sin, cd, "retiro", tec)
	esperar(t, lote(t, sin, ct, op(campo.OpLlegar, r2.ID, nil),
		op(campo.OpEquipoRetirar, r2.ID, map[string]any{"serie": "CUALQUIERA", "condicion": "buena"})), "aplicada", "aplicada")
	completarChecklist(t, sin, ct, r2.ID)
	deber(sin.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: r2.ID, Nombre: "Ana", SVG: "<svg><path/></svg>"}))(t)
	esperar(t, lote(t, sin, ct, op(campo.OpCerrar, r2.ID, nil)), "aplicada")
	if c := ultimo(t, eventosDe(t, r2.ID), "orden.cerrada"); c["equipos_validados"] != false {
		t.Fatalf("sin conector, no verificado: %v", c)
	}
}

// 5. Una orden cancelada ajena responde igual que una inexistente.
func TestRevisionSinOraculoDeCanceladas(t *testing.T) {
	e := nuevoEntorno()
	d, tecA, tecB := despacho(t), tecnico(t), tecnico(t)
	cd, cb := como(t, d, "despacho"), como(t, tecB, "tecnico")
	o := programada(t, e, cd, "instalacion", tecA)
	deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o.Version, Motivo: "cliente desiste"}))(t)
	o2 := programada(t, e, cd, "instalacion", tecA)
	r := lote(t, e, cb, op(campo.OpLlegar, o.ID, nil), op(campo.OpLlegar, o2.ID, nil), op(campo.OpLlegar, 99999999, nil))
	esperar(t, r, "rechazada:no_encontrado", "rechazada:no_encontrado", "rechazada:no_encontrado")
}

// 6. Repetida en el mismo lote tras un rechazo: sigue rechazada.
func TestRevisionRepetidaRechazada(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	o := programada(t, e, como(t, d, "despacho"), "visita", tec)
	mala := op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": ""})
	esperar(t, lote(t, e, como(t, tec, "tecnico"), mala, mala), "rechazada:validacion", "rechazada:validacion")
}

// 8. Un dispositivo no cambia de usuario.
func TestRevisionDispositivoAjeno(t *testing.T) {
	e := nuevoEntorno()
	t1, t2 := tecnico(t), tecnico(t)
	disp := "tel-compartido-" + pruebas.Unico()
	if _, err := e.m.Lote(como(t, t1, "tecnico"), campo.EntradaLote{DispositivoID: disp}); err != nil {
		t.Fatal(err)
	}
	_, err := e.m.Lote(como(t, t2, "tecnico"), campo.EntradaLote{DispositivoID: disp})
	problema(t, err, 409, "dispositivo_ajeno")
	if n := contar(t, "SELECT count(*) FROM campo.dispositivos WHERE id = $1 AND usuario_id = $2", disp, t1); n != 1 {
		t.Fatal("el dispositivo sigue siendo del primero")
	}
}

// 9. NUL → 422; la firma de un intento fallido no vale para el siguiente cierre; lote de 4 MiB.
func TestRevisionPuntosSueltos(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil), op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "a\x00b"})),
		"aplicada", "rechazada:validacion")
	_, err := e.m.Lote(ct, campo.EntradaLote{DispositivoID: "x\x00y"})
	problema(t, err, 422, "validacion")

	deber(e.m.CapturarFirma.Ejecutar(ct, campo.EntradaCapturarFirma{ID: o.ID, Nombre: "Ana", SVG: "<svg><path/></svg>"}))(t)
	o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	o = deber(e.m.FallarOrden.Ejecutar(ct, campo.EntradaFallarOrden{ID: o.ID, Version: o.Version, MotivoFalla: "cliente_ausente"}))(t)
	o = deber(e.m.ReprogramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, Inicio: inicio.Add(time.Hour), Motivo: ptr("Otro día")}))(t)
	if det := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t); det.Firma != nil {
		t.Fatal("la firma del intento fallido quedó vigente")
	}
	o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
	completarChecklist(t, e, ct, o.ID)
	o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	_, err = e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version})
	if errs := problema(t, err, 422, "cierre_incompleto"); len(errs) != 1 || errs[0].Field != "firma" {
		t.Fatalf("pide otra firma: %+v", errs)
	}
	if n := contar(t, "SELECT count(*) FROM campo.firmas WHERE orden_id = $1 AND invalidada_en IS NOT NULL", o.ID); n != 1 {
		t.Fatal("la firma anterior queda como evidencia")
	}

	// Por HTTP: un lote de ~2 MiB (varias firmas) entra; uno de más de 4 MiB, 413.
	base := nuevoServidor(t)
	c := entrar(t, base, "tecnico")
	dh := entrar(t, base, "despacho")
	dh.pedir(http.MethodPost, "/api/v1/ordenes", map[string]any{"tipo": "visita", "direccion": "Zona\u00001"}).problema(t, 422, "validacion")
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.crear' AND actor_id = $1", dh.id); n != 0 {
		t.Fatalf("el NUL no llega al caso de uso: %d", n)
	}
	svg := "<svg>" + strings.Repeat("M0 0L1 1 ", 21_000) + "</svg>"
	ops := []map[string]any{}
	for range 11 {
		ops = append(ops, map[string]any{"id": opID(), "tipo": "firma.capturar", "ordenId": 99999999, "datos": map[string]any{"nombre": "Ana", "svg": svg}})
	}
	if r := c.pedir(http.MethodPost, "/api/v1/sync/lote", map[string]any{"dispositivoId": "pwa-grande-" + pruebas.Unico(), "operaciones": ops}); r.status != 200 {
		t.Fatalf("lote de ~2 MiB: %d %.300s", r.status, r.crudo)
	}
	for range 12 {
		ops = append(ops, ops[0])
	}
	if r := c.pedir(http.MethodPost, "/api/v1/sync/lote", map[string]any{"dispositivoId": "pwa-grande", "operaciones": ops}); r.status != 413 {
		t.Fatalf("lote de más de 4 MiB: %d", r.status)
	}
}
