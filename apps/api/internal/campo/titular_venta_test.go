package campo_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Venta a instalación, lado campo (contrato argosisp.md 0.10.0 §3.4): modo empresarial (NIT y
// representante, §3.4.2), el paso desde el panel (§3.4, «Quién»), los planes permitidos en el
// paquete del día (B21) y las órdenes que pasan al titular nuevo (B17).

// conModo marca el prospecto de la orden como venta empresarial.
func conModo(t *testing.T, o campo.Orden, modo string) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.prospectos SET modo = $1 WHERE id = $2", modo, *o.ProspectoID); err != nil {
		t.Fatal(err)
	}
}

func TestTitularEmpresarial(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	conModo(t, o, "empresarial")
	orden := strconv.FormatInt(o.ID, 10)

	// En empresarial, la identidad (A41) es la del representante, con DPI; el titular (A39), la
	// empresa con su NIT y el representante.
	ident := datosIdentidad()
	ident["documentoTipo"], ident["documentoNumero"] = "pasaporte", "AB12345"
	dpiSolo := datosCambioTitular()
	sinRepresentante := datosCambioTitular()
	sinRepresentante["titular"].(map[string]any)["documentoTipo"] = "nit"
	sinRepresentante["titular"].(map[string]any)["documentoNumero"] = nitPrueba
	for i, datos := range []struct {
		tipo  string
		d     map[string]any
		regla string
	}{{campo.OpRegistrarIdentidad, ident, "empresarial"}, {campo.OpCambiarTitular, dpiSolo, "empresarial"}, {campo.OpCambiarTitular, sinRepresentante, "required"}} {
		r := e.lote(ct, op(datos.tipo, o.ID, datos.d))
		esperar(t, r, "rechazada:validacion")
		if r.Resultados[0].Error == nil || len(r.Resultados[0].Error.Errors) == 0 || r.Resultados[0].Error.Errors[0].Rule != datos.regla {
			t.Fatalf("caso %d: %+v, esperaba la regla %s", i, r.Resultados[0].Error, datos.regla)
		}
	}
	empresa := datosCambioTitular()
	tit := empresa["titular"].(map[string]any)
	tit["nombre"], tit["documentoTipo"], tit["documentoNumero"] = "Distribuidora La Ceiba, S. A.", "nit", nitPrueba
	tit["representante"] = map[string]any{"nombre": "Ana María Pérez", "documentoNumero": dpiPrueba}
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, empresa)), "aplicada")
	e.procesar()
	l := e.falso.porClave("iris:titular:" + orden + ":1")
	if l == nil {
		t.Fatal("A39 no salió")
	}
	cli, _ := l.JSON["cliente"].(map[string]any)
	rep, _ := l.JSON["representante"].(map[string]any)
	if cli["documento_tipo"] != "nit" || cli["documento_numero"] != nitPrueba || rep["documento_tipo"] != "dpi" ||
		rep["documento_numero"] != dpiPrueba || rep["nombre"] != "Ana María Pérez" {
		t.Fatalf("A39 empresarial: %+v", l.JSON)
	}
	est := deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if est.Modo != "empresarial" || est.DocumentoTipo == nil || *est.DocumentoTipo != "nit" {
		t.Fatalf("estado: %+v", est)
	}
	sinSensibles(t)

	// Residencial: el NIT como documento no vale.
	o2 := e.instalacion(tec, ct, true)
	r := e.lote(ct, op(campo.OpCambiarTitular, o2.ID, empresa))
	esperar(t, r, "rechazada:validacion")
	if r.Resultados[0].Error.Errors[0].Rule != "residencial" {
		t.Fatalf("residencial: %+v", r.Resultados[0].Error)
	}
}

func TestTitularDesdeElPanel(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	o = deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t).Orden

	// Sin versión: 422; con una vieja: 409.
	_, err := e.m.Titular.CambiarPlan.Ejecutar(e.cs, campo.EntradaCambiarPlan{ID: o.ID, PlanID: 300, Motivo: "mas_velocidad"})
	if !esProblema(err, "validacion") {
		t.Fatalf("sin versión: %v", err)
	}
	vieja := o.Version - 1
	if _, err := e.m.Titular.CambiarPlan.Ejecutar(e.cs, campo.EntradaCambiarPlan{ID: o.ID, PlanID: 300, Motivo: "mas_velocidad", Version: &vieja}); err == nil {
		t.Fatal("versión vieja aceptada")
	}

	// El supervisor completa datos y cambia el plan desde el panel (después de la identidad tomada
	// en sitio): mismas colas que el lote.
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "aplicada")
	o = deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	o2 := deber(e.m.Titular.CompletarDatos.Ejecutar(e.cs, campo.EntradaCompletarDatos{ID: o.ID, Version: &o.Version,
		Servicio: &campo.LugarServicio{Referencia: ptr("Portón verde")}}))(t)
	deber(e.m.Titular.CambiarPlan.Ejecutar(e.cs, campo.EntradaCambiarPlan{ID: o.ID, PlanID: 300, Motivo: "mas_velocidad", Version: &o2.Version}))(t)
	pd, pp := e.pendiente("iris:datos:"+orden+":1"), e.pendiente("iris:plan:"+orden+":1")
	if len(pp.DependeDe) != 1 || pp.DependeDe[0] != pd.ID || pp.Actor == nil || *pp.Actor != "usuario:"+strconv.FormatInt(e.sup, 10) {
		t.Fatalf("pendientes desde el panel: %+v %+v", pd, pp)
	}

	// Un técnico que no es el de la orden no lo hace (ni desde la API).
	_, otro := e.tecnicoEnEmpresa()
	o3 := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	if _, err := e.m.Titular.CambiarPlan.Ejecutar(otro, campo.EntradaCambiarPlan{ID: o.ID, PlanID: 301, Motivo: "mas_velocidad", Version: &o3.Version}); err == nil {
		t.Fatal("otro técnico cambió el plan")
	}
	// Una orden cancelada no acepta el paso desde el panel.
	oc := deber(e.m.CancelarOrden.Ejecutar(e.cs, campo.EntradaMotivo{ID: o.ID, Version: o3.Version, Motivo: "El cliente desistió de la instalación"}))(t)
	if _, err := e.m.Titular.CambiarPlan.Ejecutar(e.cs, campo.EntradaCambiarPlan{ID: o.ID, PlanID: 301, Motivo: "mas_velocidad", Version: &oc.Version}); !esProblema(err, "transicion_invalida") {
		t.Fatalf("cancelada: %v", err)
	}
}

// planesFalsos: GET /planes de Argos.
type planesFalsos struct {
	llamadas *[]string
	fallar   bool
}

func (p planesFalsos) PlanesPermitidos(_ context.Context, router int64, modo string) ([]argos.PlanPermitido, error) {
	*p.llamadas = append(*p.llamadas, strconv.FormatInt(router, 10)+":"+modo)
	if p.fallar {
		return nil, errors.New("argos caído")
	}
	precio := argos.Decimal("350.00")
	return []argos.PlanPermitido{{ID: 300, Nombre: "Fibra 100", Precio: &precio, Modo: modo}}, nil
}

func TestPlanesPermitidosEnElPaquete(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	pool := pruebas.Pool(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
		VALUES (990, 1, '{1}', 'Prospecto titular', 'C-000990', '5512 3344', 'Zona 10', now()) ON CONFLICT (id) DO NOTHING`)
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, router_id, estado, sincronizado_en)
		VALUES (9901, 1, 990, 'Fibra 50', 55, 'pendiente_instalacion', now()) ON CONFLICT (id) DO UPDATE SET estado = 'pendiente_instalacion', router_id = 55`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = 9901")
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = 990")
	})
	o := e.instalacion(tec, ct, true)
	conModo(t, o, "empresarial")
	o2 := e.instalacion(tec, ct, false)

	llamadas := []string{}
	m := campo.Nuevo(campo.Config{Planes: planesFalsos{llamadas: &llamadas}})
	paq := deber(m.Paquete(ct, campo.EntradaPaquete{Fecha: hoyEnGuatemala(12).Format("2006-01-02")}))(t)
	vistos := 0
	for _, x := range paq.Ordenes {
		switch x.ID {
		case o.ID, o2.ID:
			vistos++
			modo := "residencial"
			if x.ID == o.ID {
				modo = "empresarial"
			}
			if x.Planes == nil || !x.Planes.Disponible || x.Planes.Modo != modo || len(x.Planes.Data) != 1 || x.Planes.Data[0].ID != 300 ||
				x.Planes.Data[0].Precio == nil || *x.Planes.Data[0].Precio != "350.00" {
				t.Fatalf("planes de %d: %+v", x.ID, x.Planes)
			}
		}
	}
	if vistos != 2 || len(llamadas) != 2 {
		t.Fatalf("órdenes %d, llamadas %v (una por router y modo)", vistos, llamadas)
	}

	// Argos caído: disponible = false con el motivo (nunca «sin planes»).
	m = campo.Nuevo(campo.Config{Planes: planesFalsos{llamadas: &llamadas, fallar: true}})
	paq = deber(m.Paquete(ct, campo.EntradaPaquete{Fecha: hoyEnGuatemala(12).Format("2006-01-02")}))(t)
	for _, x := range paq.Ordenes {
		if x.ID == o.ID && (x.Planes == nil || x.Planes.Disponible || x.Planes.Motivo == nil || *x.Planes.Motivo != campo.MotivoArgosNoDisponible) {
			t.Fatalf("Argos caído: %+v", x.Planes)
		}
	}

	// Servicio ya activo: la orden no trae planes.
	exec("UPDATE integracion.argos_servicios SET estado = 'activo' WHERE id = 9901")
	paq = deber(m.Paquete(ct, campo.EntradaPaquete{Fecha: hoyEnGuatemala(12).Format("2006-01-02")}))(t)
	for _, x := range paq.Ordenes {
		if x.ID == o.ID && x.Planes != nil {
			t.Fatalf("servicio activo con planes: %+v", x.Planes)
		}
	}
}

func TestOrdenPasaAlTitularNuevo(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, false)
	in := campo.EntradaTitularArgos{ServicioID: 9901, DeClienteID: ptr(int64(990)), AClienteID: 777, EntranteID: 1}
	if _, err := e.m.MoverAlTitular.Ejecutar(e.cs, in); err == nil {
		t.Fatal("un usuario movió la orden")
	}
	r := deber(e.m.MoverAlTitular.Ejecutar(sistema(t), in))(t)
	if !slices.Contains(r.Ordenes, o.ID) {
		t.Fatalf("movidas: %+v", r)
	}
	v := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if v.ClienteID == nil || *v.ClienteID != 777 {
		t.Fatalf("cliente: %v", v.ClienteID)
	}
	// Repetido: nada.
	if r := deber(e.m.MoverAlTitular.Ejecutar(sistema(t), in))(t); slices.Contains(r.Ordenes, o.ID) {
		t.Fatalf("repetido: %+v", r)
	}
}

// esProblema: el error es un problema con ese código.
func esProblema(err error, codigo string) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == codigo
}
