package campo_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Cobro de la instalación, contrato 0.14.3 §3: el cargo del cierre y el cobro de A36 nunca quedan
// vigentes a la vez con la línea de instalación (concepto 5, codigo_sistema instalacion en el Argos
// falso), con cobro_equipo en false nadie la manda, y el «cobro por confirmar» se ve en la orden.

// plantillaConCobro: la plantilla de la orden trae el concepto de instalación y su precio (A36). Al
// terminar vuelve a como estaba (la comparten las demás pruebas del paquete).
func plantillaConCobro(t *testing.T, orden int64) {
	t.Helper()
	pool := pruebas.Pool(t)
	var concepto *int64
	var precio *float64
	if err := pool.QueryRow(context.Background(), `SELECT cobro_concepto_id, cobro_precio::float8 FROM campo.plantillas_checklist
		WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, orden).Scan(&concepto, &precio); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE campo.plantillas_checklist SET cobro_concepto_id = $2, cobro_precio = $3
			WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, orden, concepto, precio)
	})
	if _, err := pool.Exec(context.Background(), `UPDATE campo.plantillas_checklist SET cobro_concepto_id = 5, cobro_precio = 350
		WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, orden); err != nil {
		t.Fatal(err)
	}
}

// argos0143: el conector informa la 0.14.3 (codigo_sistema en GET /conceptos-cargo y la lectura de
// facturas de origen Iris), con todas las marcas de capacidades.equipos en true salvo las indicadas.
func argos0143(t *testing.T, cobroEquipo bool) {
	t.Helper()
	caps := `[{"empresa_id":1,"estado_equipo":true,"wifi":true,"bienvenida":true,"cobro_equipo":` + strconv.FormatBool(cobroEquipo) + `}]`
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE integracion.conectores SET contrato_remoto = '0.14.3',
		capacidades_equipos = $1::jsonb WHERE sistema = 'argosisp'`, caps); err != nil {
		t.Fatal(err)
	}
}

// cierreConCargo deja en el outbox el orden.cerrada de la orden con un cargo de lineas[] (la forma del
// contrato de eventos), sin publicarlo.
func cierreConCargo(t *testing.T, o campo.Orden, conceptos ...int) {
	t.Helper()
	lineas := []any{}
	for _, c := range conceptos {
		precio := "100.00"
		if c == 5 {
			precio = "350.00"
		}
		lineas = append(lineas, map[string]any{"concepto_id": c, "precio_unitario": precio})
	}
	crudo, _ := json.Marshal(map[string]any{"orden_id": o.ID, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 990,
		"servicio_id": *o.ServicioID, "empresa_id": 1, "activacion": map[string]any{"modo": "al_cerrar", "estado": "ninguna"},
		"cargo": map[string]any{"descripcion": "Cobro de la orden", "lineas": lineas}})
	if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
		crudo); err != nil {
		t.Fatal(err)
	}
}

// lineasDelCierre: los conceptos del cargo del cierre encolado (nil si no hay).
func lineasDelCierre(t *testing.T, orden int64) []float64 {
	t.Helper()
	var crudo []byte
	err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT payload FROM integracion.pendientes WHERE idempotency_key = $1",
		"iris:cargo:"+strconv.FormatInt(orden, 10)).Scan(&crudo)
	if err != nil {
		return nil
	}
	var c struct {
		Lineas []struct {
			ConceptoID float64 `json:"concepto_id"`
		} `json:"lineas"`
	}
	_ = json.Unmarshal(crudo, &c)
	r := []float64{}
	for _, l := range c.Lineas {
		r = append(r, l.ConceptoID)
	}
	return r
}

func TestCierreConCobroA36Vigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	tec, ct := e.tecnicoEnEmpresa()

	// A36 vigente (ya facturado) con la línea de instalación: el cierre sale solo con la visita.
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	c := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	e.procesar()
	e.relay()
	cierreConCargo(t, o, 5, 9)
	e.relay()
	if got := lineasDelCierre(t, o.ID); len(got) != 1 || got[0] != 9 {
		t.Fatalf("el cierre sin la línea de instalación: %v", got)
	}
	e.procesar()
	l := e.falso.porClave("iris:cargo:" + strconv.FormatInt(o.ID, 10))
	if l == nil {
		t.Fatal("no salió el cargo del cierre")
	}
	if lineas, _ := l.JSON["lineas"].([]any); len(lineas) != 1 || lineas[0].(map[string]any)["precio_unitario"] != "100.00" {
		t.Fatalf("cargo del cierre: %+v", l)
	}
	if cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); len(cs) != 1 || cs[0].ID != c.ID {
		t.Fatalf("un solo cobro de instalación: %+v", cs)
	}

	// A36 aún encolado y un cierre solo con la instalación: el cargo del cierre no sale.
	o2 := e.instalacionPropia(tec, ct)
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o2.ID}))(t)
	cierreConCargo(t, o2, 5)
	e.relay()
	if got := lineasDelCierre(t, o2.ID); got != nil {
		t.Fatalf("sin líneas no sale: %v", got)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_omitido' AND payload->>'orden_id' = $1
		AND payload->>'motivo' = 'cargo_sin_lineas'`, strconv.FormatInt(o2.ID, 10)); n != 1 {
		t.Fatalf("aviso: %d", n)
	}

	// El cierre ya pidió la instalación: A36 responde 409 cobro_al_cerrar (A1). Sin la línea de
	// instalación en el cierre, A36 sí se registra.
	o3 := e.instalacionPropia(tec, ct)
	cierreConCargo(t, o3, 5, 9)
	e.relay()
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o3.ID})
	problema(t, err, 409, "cobro_al_cerrar")
	o4 := e.instalacionPropia(tec, ct)
	cierreConCargo(t, o4, 9)
	e.relay()
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o4.ID}))(t)
}

// Con cobro_equipo en false nadie manda la línea de instalación: A36 la rechaza (otro concepto sí) y el
// cierre sale sin ella.
func TestCobroConCobroEquipoApagado(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, false)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 422, "capacidad_no_disponible")
	c := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(9)), PrecioUnitario: ptr("100"),
		MotivoPrecio: ptr("Visita técnica adicional cobrada")}))(t)
	if c.ConceptoID != 9 {
		t.Fatalf("otro concepto: %+v", c)
	}
	o2 := e.instalacionPropia(tec, ct)
	cierreConCargo(t, o2, 5, 9)
	e.relay()
	if got := lineasDelCierre(t, o2.ID); len(got) != 1 || got[0] != 9 {
		t.Fatalf("cierre sin la línea de instalación: %v", got)
	}
}

// El cierre y el registro de A36 a la vez: con la orden bloqueada, una sola línea de instalación
// queda vigente.
func TestCierreYCobroA36ALaVez(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	pool := pruebas.Pool(t)
	for i := range 6 {
		o := e.instalacionPropia(tec, ct)
		plantillaConCobro(t, o.ID)
		cierreConCargo(t, o, 5)
		var wg sync.WaitGroup
		var errRelay, errCobro error
		wg.Add(2)
		go func() {
			defer wg.Done()
			r := &integracion.Relay{BD: pool, Bus: e.bus, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			_, errRelay = r.Ejecutar(context.Background())
		}()
		go func() {
			defer wg.Done()
			_, errCobro = e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
		}()
		wg.Wait()
		if errRelay != nil {
			t.Fatal(errRelay)
		}
		if p, _ := nucleo.AProblema(errCobro); errCobro != nil && (p == nil || p.Codigo() != "cobro_al_cerrar") {
			t.Fatalf("vuelta %d: %v", i, errCobro)
		}
		a36 := contar(t, "SELECT count(*) FROM campo.cobros WHERE orden_id = $1 AND es_instalacion AND estado = 'pendiente'", o.ID)
		cierre := 0
		for _, c := range lineasDelCierre(t, o.ID) {
			if c == 5 {
				cierre++
			}
		}
		if a36+cierre != 1 {
			t.Fatalf("vuelta %d: %d líneas de instalación vigentes (A36 %d, cierre %d)", i, a36+cierre, a36, cierre)
		}
	}
}

// 422 capacidad_no_disponible tras un 503: el cobro de A36 queda «por confirmar» (vigente) y la orden
// lo muestra; el supervisor confirma que no hay factura: fallido, y se puede cobrar otra vez.
func TestCobroA36PorConfirmarEnLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_eventos WHERE tipo = 'cobro'")
	})
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	e.falso.rechazar("/facturas/cargo-unico", "503", "capacidad_no_disponible")
	c := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	e.procesar()
	e.reloj.avanzar(pendientes.Esperas[0])
	e.procesar()
	e.relay()
	cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if cs[0].Estado != campo.CobroPorConfirmar {
		t.Fatalf("por confirmar: %+v", cs[0])
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro' AND a = 'por_confirmar'", o.ID); n != 1 {
		t.Fatalf("aviso en la orden: %d", n)
	}
	// Vigente: no se registra otro.
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 409, "cobro_registrado")
	p, err := pendientes.PorClave(context.Background(), pruebas.Pool(t), "iris:cargo:"+strconv.FormatInt(o.ID, 10)+":"+c.ID.String())
	if err != nil {
		t.Fatal(err)
	}
	deber(e.pend.ConfirmarCobro(e.cs, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: p.Version, Motivo: "No hay factura de la orden en Argos"}))(t)
	e.relay()
	cs = deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if cs[0].Estado != campo.CobroFallido {
		t.Fatalf("fallido tras confirmar: %+v", cs[0])
	}
	// Solo tenía la línea de instalación: sin reemplazo.
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'cargo'", strconv.FormatInt(o.ID, 10)); n != 1 {
		t.Fatalf("sin reemplazo: %d", n)
	}
	c2 := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	if c2.ID == c.ID {
		t.Fatal("otro cargo_id")
	}
}

// Una bienvenida que llegó sin origen y solo se relacionó por servicio: el reenvío no manda esta orden
// como origen.
func TestReenviarBienvenidaSinOrigenReal(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	e.m.SuscribirBienvenidas(e.bus, pruebas.Pool(t))
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	esperar(t, e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(false))), "aplicada")
	e.procesar()
	e.relay()
	clave := pendientes.ClaveAprovisionar(o.ID, 1)
	deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), campo.EntradaResolverActivacion{Fuente: campo.FuenteWebhook, IdempotencyKey: &clave,
		Estado: campo.ActivacionLista}))(t)
	e.eventoArgos("argos.bienvenida_cambiada", map[string]any{"servicio_id": *o.ServicioID, "empresa_id": 1, "cliente_id": 990, "origen_tipo": nil,
		"origen_id": nil, "canales": []any{map[string]any{"canal": "whatsapp", "estado": "fallida", "motivo": "sin_contacto"}}})
	deber(e.m.ReenviarBienvenida.Ejecutar(e.cs, campo.EntradaReenviarBienvenida{ID: o.ID}))(t)
	e.procesar()
	l := e.falso.porClave(pendientes.ClaveBienvenida(o.ID, 1))
	if l == nil {
		t.Fatal("no salió el reenvío")
	}
	if _, con := l.JSON["origen_id"]; con || l.JSON["origen_tipo"] != nil {
		t.Fatalf("sin origen real no se manda la orden como origen: %+v", l.JSON)
	}
}

// cierreAnterior deja en el outbox un orden.cerrada de instalación con el cargo de la forma anterior.
func cierreAnterior(t *testing.T, o campo.Orden, conCliente bool) {
	t.Helper()
	p := map[string]any{"orden_id": o.ID, "tipo": "instalacion", "resultado": "exitosa", "servicio_id": *o.ServicioID, "empresa_id": 1,
		"activacion": map[string]any{"modo": "al_cerrar", "estado": "ninguna"}, "cargo": map[string]any{"monto": "150.00", "descripcion": "Instalación"}}
	if conCliente {
		p["cliente_id"] = 990
	}
	crudo, _ := json.Marshal(p)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
		crudo); err != nil {
		t.Fatal(err)
	}
}

func (e *entornoActivacion) pendienteDe(clave string) pendientes.Pendiente {
	e.t.Helper()
	r, err := pendientes.PorClave(context.Background(), pruebas.Pool(e.t), clave)
	if err != nil {
		e.t.Fatalf("pendiente %s: %v", clave, err)
	}
	ps, err := pendientes.DeEntidad(context.Background(), pruebas.Pool(e.t), "orden", r.EntidadID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, p := range ps {
		if p.ID == r.ID {
			return p
		}
	}
	e.t.Fatalf("pendiente %s no está en la orden", clave)
	return pendientes.Pendiente{}
}

// A1-bis: reintentar a mano un cargo con la línea de instalación cuando ya hay otro vigente responde
// 409 cargo_instalacion_vigente (cargo del cierre tras un A36, y un A36 fallido tras otro A36).
func TestReintentarCargoConOtraInstalacionVigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_eventos WHERE tipo = 'cobro'")
	})
	tec, ct := e.tecnicoEnEmpresa()

	// El cargo del cierre falla con un 4xx, se registra A36 y alguien reintenta el del cierre.
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	e.falso.rechazar("/facturas/cargo-unico", "validacion")
	cierreConCargo(t, o, 5, 9)
	e.relay()
	e.procesar()
	cierre := e.pendienteDe("iris:cargo:" + strconv.FormatInt(o.ID, 10))
	if cierre.Estado != pendientes.EstadoFallidoNegocio {
		t.Fatalf("cargo del cierre fallido: %+v", cierre)
	}
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	_, err := e.pend.Reintentar.Ejecutar(e.cs, pendientes.EntradaReintentar{ID: cierre.ID, Version: cierre.Version})
	problema(t, err, 409, "cargo_instalacion_vigente")
	e.procesar()

	// A36 #1 fallido, A36 #2 registrado: reintentar el #1 es 409; resolverlo a mano no lo revive en silencio.
	o2 := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o2.ID)
	e.falso.rechazar("/facturas/cargo-unico", "validacion")
	c1 := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o2.ID}))(t)
	e.procesar()
	e.relay()
	c2 := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o2.ID}))(t)
	p1 := e.pendienteDe("iris:cargo:" + strconv.FormatInt(o2.ID, 10) + ":" + c1.ID.String())
	_, err = e.pend.Reintentar.Ejecutar(e.cs, pendientes.EntradaReintentar{ID: p1.ID, Version: p1.Version})
	problema(t, err, 409, "cargo_instalacion_vigente")
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: p1.ID, Version: p1.Version, Motivo: "Revisado en Argos"}))(t)
	e.relay()
	cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o2.ID}))(t)
	for _, c := range cs {
		if c.ID == c1.ID && c.Estado != campo.CobroFallido {
			t.Fatalf("el #1 no revive con el #2 vigente: %+v", c)
		}
		if c.ID == c2.ID && c.Estado != campo.CobroPendiente && c.Estado != campo.CobroFacturado {
			t.Fatalf("el #2 sigue vigente: %+v", c)
		}
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro' AND a = 'revivir_rechazado'", o2.ID); n != 1 {
		t.Fatalf("aviso al supervisor: %d", n)
	}
}

// M1-bis: la forma anterior en una instalación cuenta como la línea de instalación (falla cerrado): con un
// A36 vigente no sale, ni al encolar ni al corregir un incompleto. Con el concepto de instalación conocido,
// la forma anterior con solo monto se cobra con ese concepto.
func TestCargoAnteriorConCobroA36(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()

	o := e.instalacionPropia(tec, ct)
	cierreAnterior(t, o, true)
	e.relay()
	if got := lineasDelCierre(t, o.ID); len(got) != 1 || got[0] != 5 {
		t.Fatalf("forma anterior con el concepto de instalación: %v", got)
	}

	e.falso.mu.Lock()
	e.falso.conceptos = `[]`
	e.falso.mu.Unlock()
	// El relay se atrasa y A36 se registra en medio.
	o2 := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o2.ID)
	cierreAnterior(t, o2, true)
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o2.ID}))(t)
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1", "iris:cargo:"+strconv.FormatInt(o2.ID, 10)); n != 0 {
		t.Fatalf("el cargo anterior no sale con A36 vigente: %d", n)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_omitido' AND payload->>'orden_id' = $1
		AND payload->>'motivo' = 'cobro_a36_vigente'`, strconv.FormatInt(o2.ID, 10)); n != 1 {
		t.Fatalf("aviso: %d", n)
	}

	// Un incompleto que se corrige después de un A36: no revive.
	o3 := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o3.ID)
	cierreAnterior(t, o3, false)
	e.relay()
	if p := e.pendienteDe("iris:cargo:" + strconv.FormatInt(o3.ID, 10)); !p.DatosIncompletos {
		t.Fatalf("incompleto: %+v", p)
	}
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o3.ID}))(t)
	cierreAnterior(t, o3, true)
	e.relay()
	if p := e.pendienteDe("iris:cargo:" + strconv.FormatInt(o3.ID, 10)); !p.DatosIncompletos || p.Estado != pendientes.EstadoFallidoNegocio {
		t.Fatalf("sigue incompleto: %+v", p)
	}
}

// Bajo 8: un A36 vigente con otro concepto (no de instalación) quita esa misma línea del cierre.
func TestCierreNoRepiteLineaDeA36(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(9)), PrecioUnitario: ptr("100"),
		MotivoPrecio: ptr("Visita técnica adicional cobrada")}))(t)
	cierreConCargo(t, o, 5, 9)
	e.relay()
	if got := lineasDelCierre(t, o.ID); len(got) != 1 || got[0] != 5 {
		t.Fatalf("sin la línea repetida: %v", got)
	}
}

// Bajo 5: 422 capacidad_no_disponible en aprovisionar con Wi-Fi: se reenvía sin Wi-Fi con otra clave, la
// clave cifrada se descarta y el intento sigue.
func TestAprovisionarSinMarcaWifi(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	e.falso.rechazar("/aprovisionar", "capacidad_no_disponible")
	esperar(t, e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(true))), "aplicada")
	e.procesar()
	e.relay()
	clave := pendientes.ClaveAprovisionar(o.ID, 1) + ":sin-wifi"
	e.procesar()
	e.relay()
	l := e.falso.porClave(clave)
	if l == nil || l.JSON["wifi"] != nil {
		t.Fatalf("reenvío sin Wi-Fi: %+v", l)
	}
	if a := e.activacion(o.ID); a.Estado != campo.ActivacionEnProceso {
		t.Fatalf("el intento sigue: %+v", a)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 0 {
		t.Fatalf("clave descartada: %d", n)
	}
	sinClaveWifi(t)
}

// Bajo 4: la bienvenida sin origen se relaciona por la fecha de cierre de la instalación respecto de la
// llegada del aviso (7 días), no por updated_at.
func TestBienvenidaSinOrigenVentana(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	e.m.SuscribirBienvenidas(e.bus, pruebas.Pool(t))
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	cerrar := func(hace time.Duration) {
		if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'cerrada', resultado = 'exitosa', finalizada_en = $2, updated_at = now() WHERE id = $1",
			o.ID, e.reloj.ahora().Add(-hace)); err != nil {
			t.Fatal(err)
		}
	}
	aviso := func(canal string) {
		e.eventoArgos("argos.bienvenida_cambiada", map[string]any{"servicio_id": *o.ServicioID, "empresa_id": 1, "cliente_id": 990,
			"origen_tipo": nil, "origen_id": nil, "canales": []any{map[string]any{"canal": canal, "estado": "enviada"}}})
	}
	cerrar(10 * 24 * time.Hour)
	aviso("whatsapp")
	if b := deber(e.m.VerBienvenida.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); len(b.Canales) != 0 {
		t.Fatalf("cerrada hace 10 días: no se relaciona: %+v", b)
	}
	cerrar(2 * 24 * time.Hour)
	aviso("correo")
	if b := deber(e.m.VerBienvenida.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); len(b.Canales) != 1 || !b.Canales[0].Relacionada {
		t.Fatalf("cerrada hace 2 días: relacionada: %+v", b)
	}
}

// factura.* de una factura sin cobro ligado: no cambia ningún cobro; queda como aviso en la orden.
func TestFacturaSinCobroLigadoEsAviso(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_eventos WHERE tipo = 'cobro'")
	})
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	orden := strconv.FormatInt(o.ID, 10)
	ev := map[string]any{"webhook": "factura.anulada", "factura_id": 77777, "origen_id": orden, "empresa_id": 1}
	e.eventoArgos("argos.factura_cambiada", ev)
	e.eventoArgos("argos.factura_cambiada", ev)
	if cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); cs[0].Estado != campo.CobroPendiente {
		t.Fatalf("no cambia el cobro: %+v", cs[0])
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro' AND a = 'factura_sin_cobro'", o.ID); n != 1 {
		t.Fatalf("aviso en la orden (una vez): %d", n)
	}
}

// A1-ter: reintentar un A36 fallido lo vuelve vivo en la misma transacción; registrar otro da 409.
func TestReintentarA36LoVuelveVigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	e.falso.rechazar("/facturas/cargo-unico", "validacion", "503")
	c := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	e.procesar()
	e.relay()
	if cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); cs[0].Estado != campo.CobroFallido {
		t.Fatalf("fallido: %+v", cs[0])
	}
	p := e.pendienteDe("iris:cargo:" + strconv.FormatInt(o.ID, 10) + ":" + c.ID.String())
	deber(e.pend.Reintentar.Ejecutar(e.cs, pendientes.EntradaReintentar{ID: p.ID, Version: p.Version}))(t)
	if cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); cs[0].Estado != campo.CobroPendiente {
		t.Fatalf("vivo al reintentar: %+v", cs[0])
	}
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 409, "cobro_registrado")
}

// Bajo 6: un reemplazo «por resolver» resuelto a mano cuenta como vigente con la línea de instalación.
func TestReemplazoResueltoAManoCuentaComoVigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	orden := strconv.FormatInt(o.ID, 10)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key,
		entidad, entidad_id, metodo, ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, resuelto_como, resuelto_motivo, resuelto_en)
		VALUES (1, 'argosisp', 'cargo', $1, 'orden', $2, 'POST', '/facturas/cargo-unico',
		'{"lineas":[{"concepto_id":5,"precio_unitario":"350.00"},{"concepto_id":9,"precio_unitario":"100.00"}]}', '{}', true,
		'resuelto_manual', now(), 'manual', 'Cobrado a mano en Argos', now())`, "iris:cargo:"+orden+":"+pruebas.Unico(), orden); err != nil {
		t.Fatal(err)
	}
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 409, "cargo_instalacion_vigente")
}

// Bajo 5: factura.anulada del cargo del cierre (ligado por factura_id) lo deja de contar como vigente.
func TestFacturaAnuladaDelCierreLiberaA36(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_eventos WHERE tipo = 'cobro'")
	})
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	orden := strconv.FormatInt(o.ID, 10)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET factura_id = NULL WHERE factura_id = 555"); err != nil {
		t.Fatal(err)
	}
	cierreConCargo(t, o, 5, 9)
	e.relay()
	e.procesar()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1 AND factura_id = 555", "iris:cargo:"+orden); n != 1 {
		t.Fatalf("cargo del cierre ligado a su factura: %d", n)
	}
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 409, "cobro_al_cerrar")
	e.eventoArgos("argos.factura_cambiada", map[string]any{"webhook": "factura.anulada", "factura_id": 555, "origen_id": orden, "empresa_id": 1})
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
}

// Bajo 9: un pago.registrado que llegó como aviso antes de ligar la factura se aplica al ligarla.
func TestAvisoDeFacturaSeAplicaAlLigarla(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_eventos WHERE tipo = 'cobro'")
	})
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET factura_id = NULL WHERE factura_id = 555"); err != nil {
		t.Fatal(err)
	}
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	plantillaConCobro(t, o.ID)
	deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	e.eventoArgos("argos.factura_cambiada", map[string]any{"webhook": "pago.registrado", "factura_id": 555,
		"origen_id": strconv.FormatInt(o.ID, 10), "empresa_id": 1})
	e.procesar()
	e.relay()
	if cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); cs[0].Estado != campo.CobroPagado {
		t.Fatalf("pagado al ligar la factura: %+v", cs[0])
	}
}

// A1-quater: el cargo del cierre que espera al titular (por resolver) cuenta como vigente: A36 con la
// instalación da 409 cobro_al_cerrar. Y si un A36 de instalación nació entretanto, el cuerpo se vuelve a
// decidir al resolverlo (con la orden bloqueada): sale sin esa línea.
func TestCargoPorResolverCuentaComoVigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	cierre := func(o campo.Orden) {
		t.Helper()
		esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
		if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
			`{"orden_id":"`+strconv.FormatInt(o.ID, 10)+`","tipo":"instalacion","resultado":"exitosa","cliente_id":990,"servicio_id":9901,"empresa_id":1,`+
				`"cargo":{"descripcion":"Cobro","lineas":[{"concepto_id":5,"precio_unitario":"350.00"},{"concepto_id":9,"precio_unitario":"100.00"}]}}`); err != nil {
			t.Fatal(err)
		}
		e.relay()
	}
	o := e.instalacion(tec, ct, true)
	cierre(o)
	if c := e.pendiente("iris:cargo:" + strconv.FormatInt(o.ID, 10)); len(c.DependeDe) != 1 {
		t.Fatalf("el cobro espera al titular: %+v", c)
	}
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(5)), PrecioUnitario: ptr("350"),
		MotivoPrecio: ptr("Cobro de la instalación acordado")})
	problema(t, err, 409, "cobro_al_cerrar")

	// Un A36 de instalación vigente que nació en medio (sin pasar por el registro): al resolver el cuerpo
	// del cargo del cierre, sale sin la línea de instalación.
	o2 := e.instalacion(tec, ct, true)
	cierre(o2)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO campo.cobros (id, orden_id, empresa_id, servicio_id, cliente_id, concepto_id,
		precio_unitario, estado, idempotency_key, es_instalacion) VALUES (gen_random_uuid(), $1, 1, 9901, 990, 5, 350, 'facturado', $2, true)`,
		o2.ID, "iris:cargo:"+strconv.FormatInt(o2.ID, 10)+":"+pruebas.Unico()); err != nil {
		t.Fatal(err)
	}
	e.procesar()
	l := e.falso.porClave("iris:cargo:" + strconv.FormatInt(o2.ID, 10))
	if l == nil {
		t.Fatal("no salió el cargo del cierre")
	}
	if lineas, _ := l.JSON["lineas"].([]any); len(lineas) != 1 || lineas[0].(map[string]any)["concepto_id"] != 9.0 {
		t.Fatalf("sin la línea de instalación: %+v", l.JSON)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.cobros WHERE orden_id = $1", o2.ID)
	})
}

// M1: el cobro de A36 registrado con un cambio de titular pendiente sale después del titular, a nombre
// del titular nuevo (§3.4.8); con el cambio resuelto a mano sin el cliente final, 409.
func TestCobroA36EsperaAlTitular(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	cobro := func(o campo.Orden) (campo.Cobro, error) {
		return e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(5)), PrecioUnitario: ptr("350"),
			MotivoPrecio: ptr("Cobro de la instalación acordado")})
	}
	o := e.instalacion(tec, ct, true)
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	c := deber(cobro(o))(t)
	clave := "iris:cargo:" + strconv.FormatInt(o.ID, 10) + ":" + c.ID.String()
	if p := e.pendienteDe(clave); len(p.DependeDe) == 0 {
		t.Fatalf("el cobro espera al titular: %+v", p)
	}
	e.procesar()
	l := e.falso.porClave(clave)
	if l == nil || l.JSON["cliente_id"] != 777.0 {
		t.Fatalf("cobro al titular nuevo: %+v", l)
	}

	// Reintentar un paso del titular sin alcance sobre la orden: 403 (B4).
	o2 := e.instalacion(tec, ct, true)
	e.falso.rechazar("/cambiar-titular", "servicio_no_pendiente")
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o2.ID, datosCambioTitular())), "aplicada")
	e.procesar()
	pt := e.pendienteDe("iris:titular:" + strconv.FormatInt(o2.ID, 10) + ":1")
	sinAlcance := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), e.sup, 1, nucleo.PermIntegracionesReintentar)
	_, err := e.pend.Reintentar.Ejecutar(sinAlcance, pendientes.EntradaReintentar{ID: pt.ID, Version: pt.Version})
	problema(t, err, 403, "prohibido")
	// Resuelto a mano sin el cliente final: el cobro no puede salir.
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: pt.ID, Version: pt.Version, Motivo: "Lo corregí en Argos a mano"}))(t)
	_, err = cobro(o2)
	problema(t, err, 409, "titular_por_confirmar")
}

// a36Vigente deja un cobro de A36 de instalación vigente en la orden (como si hubiera nacido entretanto).
func a36Vigente(t *testing.T, o campo.Orden) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO campo.cobros (id, orden_id, empresa_id, servicio_id, cliente_id, concepto_id,
		precio_unitario, estado, idempotency_key, es_instalacion) VALUES (gen_random_uuid(), $1, 1, $2, 990, 5, 350, 'facturado', $3, true)`,
		o.ID, *o.ServicioID, "iris:cargo:"+strconv.FormatInt(o.ID, 10)+":"+pruebas.Unico()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.cobros WHERE orden_id = $1", o.ID)
	})
}

// B1: el titular resuelto a mano fija el cliente del cargo del cierre; con un A36 de instalación vigente,
// el cargo (solo con esa línea) queda fallido e incompleto con cobro_a36_vigente y no sale.
func TestTitularAManoConA36Vigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/cambiar-titular", "servicio_no_pendiente")
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
		`{"orden_id":"`+orden+`","tipo":"instalacion","resultado":"exitosa","cliente_id":990,"servicio_id":9901,"empresa_id":1,`+
			`"cargo":{"descripcion":"Cobro","lineas":[{"concepto_id":5,"precio_unitario":"350.00"}]}}`); err != nil {
		t.Fatal(err)
	}
	e.relay()
	e.procesar()
	a36Vigente(t, o)
	clave := "iris:titular:" + orden + ":1"
	pt := e.pendiente(clave)
	e.falso.fijarServicio("9901", `{"id":9901,"empresa_id":1,"cliente_id":888}`)
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: pt.ID, Version: e.versionPendiente(clave),
		Motivo: "Lo corregí en Argos a mano", ClienteID: ptr(int64(888))}))(t)
	var estado, codigo string
	var incompleto bool
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT estado, coalesce(codigo_error, ''), datos_incompletos FROM integracion.pendientes
		WHERE idempotency_key = $1`, "iris:cargo:"+orden).Scan(&estado, &codigo, &incompleto); err != nil {
		t.Fatal(err)
	}
	if estado != pendientes.EstadoFallidoNegocio || codigo != "cobro_a36_vigente" || !incompleto {
		t.Fatalf("cargo del cierre con A36 vigente: %s %s %v", estado, codigo, incompleto)
	}
	e.procesar()
	if l := e.falso.porClave("iris:cargo:" + orden); l != nil {
		t.Fatalf("no sale: %+v", l)
	}
}

// B2: liberar el cargo (consumible_cobro) que esperaba a un consumo resuelto a mano, con un A36 de
// instalación vigente: no sale.
func TestLiberarCargoConA36Vigente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/inventario/consumos", "existencia_insuficiente")
	crudo, _ := json.Marshal(map[string]any{"orden_id": o.ID, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 990,
		"servicio_id": *o.ServicioID, "empresa_id": 1, "bodega_id": 9, "activacion": map[string]any{"modo": "al_cerrar", "estado": "ninguna"},
		// Una línea registrada desde el panel, como la manda campo en orden.cerrada (0.14.0): quién, desde dónde,
		// el tipo de la bodega en Argos y cuándo; así el consumo sale y lo rechaza Argos (no nace incompleto).
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 9, "modalidad": "consumible_cobro",
			"usuario_id": tec, "desde_app": false, "bodega_tipo": "bodega",
			"fecha": "2026-11-21T16:00:00Z", "recibido_en_iris": "2026-11-21T16:00:05Z"}},
		"cargo":      map[string]any{"producto_id": 5, "monto": "50.00", "descripcion": "Cable"}})
	if _, err := pruebas.Pool(t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
		crudo); err != nil {
		t.Fatal(err)
	}
	e.relay()
	e.procesar()
	// Con Argos ≥ 0.14.0 hay un consumo por actor y bodega (iris:consumo:<orden>:<usuario>:<bodega>).
	var clave string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT idempotency_key FROM integracion.pendientes
		WHERE entidad = 'orden' AND entidad_id = $1 AND operacion = 'consumo'`, orden).Scan(&clave); err != nil {
		t.Fatal(err)
	}
	consumo := e.pendienteDe(clave)
	if consumo.Estado != pendientes.EstadoFallidoNegocio || consumo.DatosIncompletos || e.falso.porClave(clave) == nil {
		t.Fatalf("consumo enviado y rechazado por Argos (no incompleto): %+v", consumo)
	}
	if c := e.pendienteDe("iris:cargo:" + orden); !c.EsperaConsumo {
		t.Fatalf("el cargo espera al consumo: %+v", c)
	}
	a36Vigente(t, o)
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: consumo.ID, Version: consumo.Version,
		Motivo: "Consumo rehecho en Argos", Cargo: pendientes.CargoLiberar}))(t)
	c := e.pendienteDe("iris:cargo:" + orden)
	if c.Estado != pendientes.EstadoFallidoNegocio || c.CodigoError == nil || *c.CodigoError != "cobro_a36_vigente" || c.EsperaConsumo {
		t.Fatalf("liberado con A36 vigente: no sale: %+v", c)
	}
	e.procesar()
	if l := e.falso.porClave("iris:cargo:" + orden); l != nil {
		t.Fatalf("no sale: %+v", l)
	}
}

// M-a y B-e: el A36 registrado antes del cambio de titular (sin pasos) pasa a esperar el cambio que el
// técnico sincroniza después y sale al titular nuevo; el cobro guarda ese titular. Si ya había salido,
// el cambio no lo rechaza: queda un aviso en la orden (las facturas no se mueven, §3.4.4).
func TestA36AntesDelCambioDeTitular(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_eventos WHERE tipo = 'cobro'")
	})
	tec, ct := e.tecnicoEnEmpresa()
	cobro := func(o campo.Orden) campo.Cobro {
		return deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(5)), PrecioUnitario: ptr("350"),
			MotivoPrecio: ptr("Cobro de la instalación acordado")}))(t)
	}
	o := e.instalacion(tec, ct, true)
	c := cobro(o)
	clave := "iris:cargo:" + strconv.FormatInt(o.ID, 10) + ":" + c.ID.String()
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	if p := e.pendienteDe(clave); len(p.DependeDe) != 1 || !p.PorResolver {
		t.Fatalf("el A36 espera al cambio de titular: %+v", p)
	}
	e.procesar()
	if l := e.falso.porClave(clave); l == nil || l.JSON["cliente_id"] != 777.0 {
		t.Fatalf("A36 al titular nuevo: %+v", l)
	}
	if cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); len(cs) != 1 ||
		contar(t, "SELECT count(*) FROM campo.cobros WHERE id = $1 AND cliente_id = 777", cs[0].ID) != 1 {
		t.Fatalf("el cobro guarda el titular efectivo: %+v", cs)
	}

	// Ya salió a Argos: el cambio no se rechaza y queda el aviso.
	o2 := e.instalacion(tec, ct, true)
	cobro(o2)
	e.procesar()
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o2.ID, datosCambioTitular())), "aplicada")
	if n := contar(t, "SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro' AND a = 'cobro_titular_anterior'", o2.ID); n != 1 {
		t.Fatalf("aviso del cobro ya enviado: %d", n)
	}
	// Otro cambio de titular en la misma orden: el aviso de ese cobro no se repite.
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o2.ID, datosCambioTitular())), "aplicada")
	if n := contar(t, "SELECT count(*) FROM campo.orden_eventos WHERE orden_id = $1 AND tipo = 'cobro' AND a = 'cobro_titular_anterior'", o2.ID); n != 1 {
		t.Fatalf("el aviso no se repite: %d", n)
	}
}

// B-a: liberar un cargo [instalación, otra línea] que esperaba al consumo, con un A36 de instalación
// vigente: sale solo la otra línea (el cuerpo nuevo se guarda antes de liberarlo).
func TestLiberarCargoGuardaElCuerpoSinInstalacion(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	pool := pruebas.Pool(t)
	var consumo int64
	if err := pool.QueryRow(context.Background(), `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad,
		entidad_id, metodo, ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, ultimo_error)
		VALUES (1, 'argosisp', 'consumo', $1, 'orden', $2, 'POST', '/inventario/consumos', '{"lineas":[{"producto_id":5,"cantidad":1}]}', '{}',
		false, 'fallido_negocio', now(), 'HTTP 422') RETURNING id`, "iris:consumo:"+orden, orden).Scan(&consumo); err != nil {
		t.Fatal(err)
	}
	cuerpo := `{"cliente_id":990,"servicio_id":` + strconv.FormatInt(*o.ServicioID, 10) + `,"producto_id":5,"origen_tipo":"iris_orden","origen_id":"` + orden +
		`","lineas":[{"concepto_id":5,"precio_unitario":"350.00"},{"concepto_id":9,"precio_unitario":"100.00"}]}`
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.pendientes (empresa_id, sistema, operacion, idempotency_key, entidad,
		entidad_id, metodo, ruta, payload, depende_de, datos_incompletos, estado, proximo_intento, espera_consumo)
		VALUES (1, 'argosisp', 'cargo', $1, 'orden', $2, 'POST', '/facturas/cargo-unico', $3, ARRAY[$4::bigint], false, 'bloqueado', now(), true)`,
		"iris:cargo:"+orden, orden, cuerpo, consumo); err != nil {
		t.Fatal(err)
	}
	a36Vigente(t, o)
	p := e.pendienteDe("iris:consumo:" + orden)
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: p.ID, Version: p.Version,
		Motivo: "Consumo rehecho en Argos", Cargo: pendientes.CargoLiberar}))(t)
	e.procesar()
	l := e.falso.porClave("iris:cargo:" + orden)
	if l == nil {
		t.Fatal("la otra línea sale")
	}
	if lineas, _ := l.JSON["lineas"].([]any); len(lineas) != 1 || lineas[0].(map[string]any)["concepto_id"] != 9.0 {
		t.Fatalf("sin la línea de instalación: %+v", l.JSON)
	}
}

// B-d: un cambio de titular resuelto a mano sin cliente deja el A36 en 409 titular_por_confirmar hasta
// que un supervisor indica el cliente final en ese paso (comprobado en Argos); después el A36 sale a él.
// También B4: el técnico asignado y un supervisor de otra zona no reintentan el paso (403).
func TestTitularPorConfirmarTieneSalida(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/cambiar-titular", "servicio_no_pendiente")
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	e.procesar()
	clave := "iris:titular:" + orden + ":1"
	pt := e.pendienteDe(clave)
	// M1 «nace bloqueado»: el A36 registrado con el paso del titular fallido.
	c := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(5)), PrecioUnitario: ptr("350"),
		MotivoPrecio: ptr("Cobro de la instalación acordado")}))(t)
	cargo := "iris:cargo:" + orden + ":" + c.ID.String()
	if p := e.pendienteDe(cargo); p.Estado != pendientes.EstadoBloqueado {
		t.Fatalf("el A36 nace bloqueado: %+v", p)
	}
	// B4: el técnico asignado (aunque tuviera los permisos) y un supervisor de otra zona: 403.
	tecConPermisos := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), tec, 1, nucleo.PermIntegracionesReintentar, nucleo.PermOrdenesValidarIdentidad)
	_, err := e.pend.Reintentar.Ejecutar(tecConPermisos, pendientes.EntradaReintentar{ID: pt.ID, Version: pt.Version})
	problema(t, err, 403, "prohibido")
	// B-f: el 403 va antes que el 409 de versión, al reintentar y al resolver a mano.
	_, err = e.pend.Reintentar.Ejecutar(tecConPermisos, pendientes.EntradaReintentar{ID: pt.ID, Version: pt.Version + 99})
	problema(t, err, 403, "prohibido")
	_, err = e.pend.ResolverManual.Ejecutar(tecConPermisos, pendientes.EntradaResolverManual{ID: pt.ID, Version: pt.Version + 99,
		Motivo: "Lo corregí en Argos a mano"})
	problema(t, err, 403, "prohibido")
	otraZona := map[string][]contexto.Alcance{}
	for _, pm := range permisosDe("supervisor") {
		otraZona[string(pm)] = []contexto.Alcance{{ZonaID: 987654}}
	}
	deOtraZona := contexto.ConActor(e.cs, contexto.ActorDe(e.cs), contexto.NuevosPermisos(otraZona))
	_, err = e.pend.Reintentar.Ejecutar(deOtraZona, pendientes.EntradaReintentar{ID: pt.ID, Version: pt.Version})
	problema(t, err, 403, "prohibido")
	_, err = e.pend.ResolverManual.Ejecutar(deOtraZona, pendientes.EntradaResolverManual{ID: pt.ID, Version: pt.Version + 99,
		Motivo: "Lo corregí en Argos a mano"})
	problema(t, err, 403, "prohibido")
	// Resuelto a mano sin cliente: el cobro de A36 nuevo no puede salir.
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: pt.ID, Version: e.versionPendiente(clave), Motivo: "Lo corregí en Argos a mano"}))(t)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.cobros SET estado = 'anulado' WHERE id = $1", c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET factura_anulada = true WHERE idempotency_key = $1", cargo); err != nil {
		t.Fatal(err)
	}
	cobrar := func() (campo.Cobro, error) {
		return e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(5)), PrecioUnitario: ptr("350"),
			MotivoPrecio: ptr("Cobro de la instalación acordado")})
	}
	_, err = cobrar()
	problema(t, err, 409, "titular_por_confirmar")
	// La salida: el supervisor indica el cliente final en ese paso (comprobado en Argos).
	e.falso.fijarServicio("9901", `{"id":9901,"empresa_id":1,"cliente_id":888}`)
	deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: pt.ID, Version: e.versionPendiente(clave),
		Motivo: "El titular final es el cliente 888", ClienteID: ptr(int64(888))}))(t)
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_cliente_fijado'
		AND (payload->>'pendiente_id')::bigint = $1 AND payload->>'cliente_id' = '888'`, pt.ID); n != 1 {
		t.Fatalf("evento del cliente fijado: %d", n)
	}
	// Una vez: ya tiene cliente.
	_, err = e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: pt.ID, Version: e.versionPendiente(clave),
		Motivo: "El titular final es el cliente 888", ClienteID: ptr(int64(888))})
	problema(t, err, 422, "pendiente_no_resoluble")
	c2 := deber(cobrar())(t)
	e.procesar()
	if l := e.falso.porClave("iris:cargo:" + orden + ":" + c2.ID.String()); l == nil || l.JSON["cliente_id"] != 888.0 {
		t.Fatalf("A36 al cliente confirmado: %+v", l)
	}
}

// B5: un supervisor no cierra sin la activación por sync (403).
func TestSinActivacionPorSyncEsProhibido(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	trabajo := trabajoDeInstalacion(o.ID)[2:]
	esperar(t, e.lote(ct, trabajo[:len(trabajo)-1]...), repetir(len(trabajo)-1, "aplicada")...)
	r := e.lote(e.cs, op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa", "sinActivacionMotivo": "Argos no responde desde la mañana"}))
	if r.Resultados[0].Error == nil || r.Resultados[0].Error.Status != 403 {
		t.Fatalf("supervisor por sync: %+v", r.Resultados[0])
	}
	if f := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); f.Estado != campo.EstadoEnSitio {
		t.Fatalf("la orden sigue en sitio: %s", f.Estado)
	}
}

// B-d: un cambio de titular detenido al cancelar la orden (no resuelto a mano por una persona) no
// acepta clienteId (422) ni emite el evento.
func TestClienteFijadoSoloEnResueltoAMano(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	argos0143(t, true)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	clave := "iris:titular:" + strconv.FormatInt(o.ID, 10) + ":1"
	deber(e.m.CancelarOrden.Ejecutar(e.cs, campo.EntradaMotivo{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "El cliente desistió"}))(t)
	p := e.pendienteDe(clave)
	if p.Estado != pendientes.EstadoResueltoManual {
		t.Fatalf("el paso queda detenido: %+v", p)
	}
	e.falso.fijarServicio("9901", `{"id":9901,"empresa_id":1,"cliente_id":888}`)
	_, err := e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: p.ID, Version: e.versionPendiente(clave),
		Motivo: "El titular final es el cliente 888", ClienteID: ptr(int64(888))})
	problema(t, err, 422, "pendiente_no_resoluble")
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_cliente_fijado'
		AND (payload->>'pendiente_id')::bigint = $1`, p.ID); n != 0 {
		t.Fatalf("sin evento: %d", n)
	}
}
