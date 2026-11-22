package campo_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE integracion.conectores SET contrato_remoto = '0.14.3',
		capacidades_equipos = '[{"empresa_id":1,"estado_equipo":true,"wifi":true,"bienvenida":true,"cobro_equipo":false}]' WHERE sistema = 'argosisp'`); err != nil {
		t.Fatal(err)
	}
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
