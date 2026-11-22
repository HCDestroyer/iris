package pendientes_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Cobro de la instalación, contrato 0.14.3 §3 («0.9.0:»): el cargo del cierre con lineas[], la línea
// de instalación (codigo_sistema), el 422 capacidad_no_disponible terminal con su cobro de reemplazo
// y el «cobro por confirmar».

// contrato0143: Argos 0.14.3 con capacidades.equipos para la empresa 1.
func (e *entorno) contrato0143(cobroEquipo bool) {
	e.t.Helper()
	caps := `[{"empresa_id":1,"estado_equipo":true,"wifi":true,"bienvenida":true,"cobro_equipo":` + strconv.FormatBool(cobroEquipo) + `}]`
	if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.conectores SET contrato_remoto = '0.14.3', capacidades_equipos = $2::jsonb
		WHERE id = $1`, e.conID, caps); err != nil {
		e.t.Fatal(err)
	}
}

// cierreConLineas: instalación exitosa con un cargo de dos líneas: la de instalación (concepto 5,
// codigo_sistema instalacion en el Argos falso) y una visita (concepto 9).
func cierreConLineas(orden string, conceptos ...int) map[string]any {
	lineas := []any{}
	for _, c := range conceptos {
		precio := "100.00"
		if c == 5 {
			precio = "350"
		}
		lineas = append(lineas, map[string]any{"concepto_id": c, "precio_unitario": precio, "descripcion": "Línea " + strconv.Itoa(c)})
	}
	return map[string]any{"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"cargo": map[string]any{"descripcion": "Cobro", "lineas": lineas}}
}

// cargosDe: los pendientes de cargo de la orden, por clave.
func (e *entorno) cargosDe(orden string) map[string]pendientes.Pendiente {
	e.t.Helper()
	ps, err := pendientes.DeEntidad(context.Background(), e.pool, "orden", orden)
	if err != nil {
		e.t.Fatal(err)
	}
	salida := map[string]pendientes.Pendiente{}
	for _, p := range ps {
		if p.Operacion == pendientes.OpCargo {
			salida[p.IdempotencyKey] = p
		}
	}
	return salida
}

func conceptosDe(t *testing.T, cuerpo map[string]any) []string {
	t.Helper()
	lineas, _ := cuerpo["lineas"].([]any)
	var r []string
	for _, l := range lineas {
		m := l.(map[string]any)
		r = append(r, strconv.FormatFloat(m["concepto_id"].(float64), 'f', -1, 64)+"@"+m["precio_unitario"].(string))
	}
	return r
}

func (e *entorno) supervisorCon(permisos ...nucleo.Permiso) context.Context {
	e.t.Helper()
	u, err := identidad.Nuevo(identidad.Config{}).CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Supervisor", Correo: pruebas.Correo("sup"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		e.t.Fatal(err)
	}
	return pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 1, permisos...)
}

func codigoDe(t *testing.T, err error, status int, codigo string) {
	t.Helper()
	p, _ := nucleo.AProblema(err)
	if p == nil || p.Status != status || p.Codigo() != codigo {
		t.Fatalf("esperaba %d %s: %v", status, codigo, err)
	}
}

// Con cobro_equipo en false, el cierre manda el cargo sin la línea de instalación; si no le queda
// ninguna, no sale (pendiente_omitido cargo_sin_lineas). El cuerpo es el real de Argos (lineas[],
// precio_unitario como texto con dos decimales).
func TestCargoDelCierreConCobroEquipoApagado(t *testing.T) {
	e := nuevoEntorno(t)
	e.contrato0143(false)
	orden := nuevaOrden()
	e.cerrar(cierreConLineas(orden, 5, 9))
	cs := e.cargosDe(orden)
	if len(cs) != 1 {
		t.Fatalf("un cargo: %+v", cs)
	}
	e.procesar()
	l := e.llamadaA("iris:cargo:" + orden)
	if got := conceptosDe(t, l.Cuerpo); len(got) != 1 || got[0] != "9@100.00" || l.Cuerpo["monto"] != nil || l.Cuerpo["producto_id"] != nil {
		t.Fatalf("sin la línea de instalación: %s", l.Crudo)
	}
	solo := nuevaOrden()
	e.cerrar(cierreConLineas(solo, 5))
	if cs := e.cargosDe(solo); len(cs) != 0 {
		t.Fatalf("sin líneas no sale: %+v", cs)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_omitido' AND payload->>'orden_id' = $1
		AND payload->>'motivo' = 'cargo_sin_lineas'`, solo); n != 1 {
		t.Fatalf("aviso de cargo sin líneas: %d", n)
	}
	// Con la marca encendida, la línea de instalación sale.
	e.contrato0143(true)
	con := nuevaOrden()
	e.cerrar(cierreConLineas(con, 5, 9))
	e.procesar()
	if got := conceptosDe(t, e.llamadaA("iris:cargo:"+con).Cuerpo); len(got) != 2 || got[0] != "5@350.00" {
		t.Fatalf("con la marca: %v", got)
	}
}

// 422 capacidad_no_disponible: el cargo queda terminal (nunca se reintenta con su clave, ni solo ni a
// mano) y sus demás líneas salen como un cobro nuevo con otro cargo_id, sin la de instalación.
func TestCargo422TerminalConReemplazo(t *testing.T) {
	e := nuevoEntorno(t)
	e.contrato0143(true)
	orden := nuevaOrden()
	e.cerrar(cierreConLineas(orden, 5, 9))
	e.argos.mu.Lock()
	e.argos.tipos["/facturas/cargo-unico"] = "urn:argosisp:problema:capacidad_no_disponible"
	e.argos.mu.Unlock()
	e.argos.responder("/facturas/cargo-unico", 422, 201)
	e.procesar()
	cs := e.cargosDe(orden)
	original := cs["iris:cargo:"+orden]
	if original.Estado != pendientes.EstadoFallidoNegocio || !original.CargoTerminal || original.PorConfirmar || original.ReemplazadoPor == nil {
		t.Fatalf("original terminal y reemplazado: %+v", original)
	}
	var reemplazo pendientes.Pendiente
	for clave, p := range cs {
		if strings.HasPrefix(clave, "iris:cargo:"+orden+":") {
			reemplazo = p
		}
	}
	if reemplazo.ID != *original.ReemplazadoPor || reemplazo.ReemplazoDe == nil || *reemplazo.ReemplazoDe != original.ID || len(cs) != 2 {
		t.Fatalf("reemplazo: %+v", cs)
	}
	e.procesar()
	l := e.llamadaA(reemplazo.IdempotencyKey)
	if got := conceptosDe(t, l.Cuerpo); len(got) != 1 || got[0] != "9@100.00" || l.Cuerpo["origen_id"] != orden {
		t.Fatalf("reemplazo sin la línea de instalación: %s", l.Crudo)
	}
	if p := e.cargosDe(orden)[reemplazo.IdempotencyKey]; p.Estado != pendientes.EstadoHecho {
		t.Fatalf("reemplazo hecho: %+v", p)
	}
	// Nunca se reintenta a mano con su clave.
	sup := e.supervisorCon(nucleo.PermIntegracionesReintentar)
	original = e.cargosDe(orden)["iris:cargo:"+orden]
	_, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: original.ID, Version: original.Version})
	codigoDe(t, err, 409, "cargo_terminal")
	llamadas := 0
	for _, l := range e.argos.copia() {
		if l.Clave == "iris:cargo:"+orden {
			llamadas++
		}
	}
	if llamadas != 1 {
		t.Fatalf("la clave del cargo terminal salió %d veces", llamadas)
	}
}

// El reemplazo también recibe 422: queda fallido (terminal), sin otro reenvío; se avisa.
func TestReemplazoCon422NoGeneraOtro(t *testing.T) {
	e := nuevoEntorno(t)
	e.contrato0143(true)
	orden := nuevaOrden()
	e.cerrar(cierreConLineas(orden, 5, 9))
	e.argos.mu.Lock()
	e.argos.tipos["/facturas/cargo-unico"] = "urn:argosisp:problema:capacidad_no_disponible"
	e.argos.mu.Unlock()
	e.argos.responder("/facturas/cargo-unico", 422)
	e.procesar()
	e.procesar()
	e.procesar()
	cs := e.cargosDe(orden)
	if len(cs) != 2 {
		t.Fatalf("solo el original y un reemplazo: %+v", cs)
	}
	for _, p := range cs {
		if p.Estado != pendientes.EstadoFallidoNegocio || !p.CargoTerminal || p.PorConfirmar {
			t.Fatalf("los dos fallidos y terminales: %+v", p)
		}
		if p.ReemplazoDe != nil && p.ReemplazadoPor != nil {
			t.Fatalf("el reemplazo no tiene reemplazo: %+v", p)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_fallido' AND payload->>'entidad_id' = $1`, orden); n != 2 {
		t.Fatalf("aviso de los dos rechazos: %d", n)
	}
}

// facturaArgos: el cuerpo de GET /facturas/origen-iris para la orden (servicio 20, líneas 5 y 9).
func facturaArgos(id int, orden, estado, emitida string, lineas string) string {
	return `{"factura_id":` + strconv.Itoa(id) + `,"numero":null,"empresa_id":1,"servicio_id":20,"cliente_id":10,"origen_tipo":"iris_orden",` +
		`"origen_id":"` + orden + `","estado":"` + estado + `","total":"450.00","saldo":"450.00","moneda":"GTQ","emitida_en":"` + emitida + `",` +
		`"dte":null,"lineas":` + lineas + `}`
}

const lineasArgos = `[{"concepto_id":5,"precio_unitario":"350.00","cantidad":1},{"concepto_id":9,"precio_unitario":"100","cantidad":1}]`

// porConfirmar: un cargo con las líneas 5 y 9 que recibe 503 y luego 422 capacidad_no_disponible.
func (e *entorno) porConfirmar() (string, pendientes.Pendiente) {
	e.t.Helper()
	e.argos.mu.Lock()
	e.argos.tipos["/facturas/cargo-unico"] = "urn:argosisp:problema:capacidad_no_disponible"
	e.argos.mu.Unlock()
	orden := nuevaOrden()
	e.cerrar(cierreConLineas(orden, 5, 9))
	e.argos.responder("/facturas/cargo-unico", 503, 422)
	e.procesar()
	e.reloj = e.reloj.Add(pendientes.Esperas[0])
	e.procesar()
	cs := e.cargosDe(orden)
	p := cs["iris:cargo:"+orden]
	if len(cs) != 1 || p.Estado != pendientes.EstadoFallidoNegocio || !p.PorConfirmar || !p.CargoTerminal || p.ReemplazadoPor != nil {
		e.t.Fatalf("por confirmar, sin reemplazo: %+v", cs)
	}
	return orden, p
}

func (e *entorno) fijarFactura(cuerpo string) {
	e.argos.mu.Lock()
	e.argos.facturas = cuerpo
	e.argos.mu.Unlock()
}

func ptrEntero(n int64) *int64 { return &n }

// Un 422 tras un envío sin respuesta (5xx o timeout): «cobro por confirmar», sin reemplazo ni
// reintento, hasta que un supervisor concilie con GET /facturas/origen-iris (0.14.3 final). Un 404 o un
// desajuste solo rechazan; un error de lectura es «reintentar»; si coincide, el cargo queda hecho con
// ese factura_id (único en Iris); «no existe» es una acción aparte y saca el reemplazo.
func TestCobroPorConfirmar(t *testing.T) {
	e := nuevoEntorno(t)
	e.contrato0143(true)
	orden, p := e.porConfirmar()
	emitida := e.reloj.Format(time.RFC3339)
	sup := e.supervisorCon(nucleo.PermIntegracionesReintentar, nucleo.PermIntegracionesResolverPendiente, nucleo.PermIntegracionesConfirmarCobro)
	_, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: p.ID, Version: p.Version})
	codigoDe(t, err, 409, "cargo_terminal")
	_, err = e.m.ResolverManual.Ejecutar(sup, pendientes.EntradaResolverManual{ID: p.ID, Version: p.Version, Motivo: "Revisado en Argos"})
	codigoDe(t, err, 409, "cobro_por_confirmar")
	existe := func(id int64) error {
		_, err := e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: p.Version, Existe: true, FacturaID: ptrEntero(id), Motivo: "Revisado en Argos"})
		return err
	}
	// Sin el permiso, ni siquiera se lee Argos.
	sinPermiso := e.supervisorCon(nucleo.PermIntegracionesReintentar)
	_, err = e.m.ConfirmarCobro(sinPermiso, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: p.Version, Existe: true, FacturaID: ptrEntero(901), Motivo: "Revisado"})
	codigoDe(t, err, 403, "prohibido")
	e.argos.mu.Lock()
	for _, l := range e.argos.lecturas {
		if strings.Contains(l, "origen-iris") {
			t.Fatal("sin permiso no se lee Argos")
		}
	}
	e.argos.mu.Unlock()
	// 404: solo rechaza. 503: error de lectura. Ninguno cambia el cargo.
	codigoDe(t, existe(901), 422, "factura_no_encontrada")
	e.fijarFactura("503")
	codigoDe(t, existe(901), 503, "no_disponible")
	// Otro servicio, otras líneas, anulada o anterior al primer envío: rechaza.
	e.fijarFactura(strings.Replace(facturaArgos(901, orden, "emitida", emitida, lineasArgos), `"servicio_id":20`, `"servicio_id":21`, 1))
	codigoDe(t, existe(901), 422, "factura_no_coincide")
	e.fijarFactura(facturaArgos(901, orden, "emitida", emitida, `[{"concepto_id":5,"precio_unitario":"350.00","cantidad":1}]`))
	codigoDe(t, existe(901), 422, "factura_no_coincide")
	e.fijarFactura(facturaArgos(901, orden, "anulada", emitida, lineasArgos))
	codigoDe(t, existe(901), 422, "factura_no_coincide")
	e.fijarFactura(facturaArgos(901, orden, "emitida", e.reloj.Add(-2*time.Hour).Format(time.RFC3339), lineasArgos))
	codigoDe(t, existe(901), 422, "factura_no_coincide")
	if c := e.cargosDe(orden)["iris:cargo:"+orden]; !c.PorConfirmar || c.Version != p.Version {
		t.Fatalf("los rechazos no cambian el cargo: %+v", c)
	}
	// La que coincide: hecho con ese factura_id; la lectura lleva el actor y la «Factura #».
	e.fijarFactura(facturaArgos(901, orden, "emitida", emitida, lineasArgos))
	r, err := e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: p.Version, Existe: true, FacturaID: ptrEntero(901), Motivo: "Revisado en Argos"})
	if err != nil || r.Estado != pendientes.EstadoHecho || r.PorConfirmar {
		t.Fatalf("confirmado: %+v %v", r, err)
	}
	e.argos.mu.Lock()
	ultima := e.argos.lecturas[len(e.argos.lecturas)-1]
	e.argos.mu.Unlock()
	if !strings.Contains(ultima, "numero=901") || !strings.Contains(ultima, "origen_id="+orden) || !strings.Contains(ultima, "empresa_id=1") ||
		!strings.HasSuffix(ultima, "|usuario:") && !strings.Contains(ultima, "|usuario:") {
		t.Fatalf("lectura: %s", ultima)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND factura_id = 901`, p.ID); n != 1 {
		t.Fatal("el cargo guarda el factura_id")
	}
	if cs := e.cargosDe(orden); len(cs) != 1 {
		t.Fatalf("sin reemplazo: %+v", cs)
	}
	_, err = e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: r.Version, Existe: false, Motivo: "Otra vez"})
	codigoDe(t, err, 409, "cobro_no_por_confirmar")

	// Otra orden: la misma factura ya está ligada (única en Iris); «no existe» saca el reemplazo.
	orden2, p2 := e.porConfirmar()
	e.fijarFactura(strings.Replace(facturaArgos(901, orden2, "emitida", emitida, lineasArgos), `"factura_id":901`, `"factura_id":901`, 1))
	_, err = e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p2.ID, Version: p2.Version, Existe: true, FacturaID: ptrEntero(901), Motivo: "Revisado en Argos"})
	codigoDe(t, err, 422, "factura_no_coincide")
	r, err = e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p2.ID, Version: p2.Version, Existe: false, Motivo: "No hay factura en Argos"})
	if err != nil || r.Estado != pendientes.EstadoFallidoNegocio || r.PorConfirmar || !r.CargoTerminal || r.ReemplazadoPor == nil {
		t.Fatalf("confirmado sin factura: %+v %v", r, err)
	}
	e.argos.responder("/facturas/cargo-unico", 201)
	e.procesar()
	var reemplazo pendientes.Pendiente
	for clave, c := range e.cargosDe(orden2) {
		if clave != "iris:cargo:"+orden2 {
			reemplazo = c
		}
	}
	if reemplazo.Estado != pendientes.EstadoHecho {
		t.Fatalf("reemplazo hecho: %+v", reemplazo)
	}
	if got := conceptosDe(t, e.llamadaA(reemplazo.IdempotencyKey).Cuerpo); len(got) != 1 || got[0] != "9@100.00" {
		t.Fatalf("reemplazo: %v", got)
	}
}

// Factura anterior a la 0.14.3 (líneas sin concepto_id): queda como factura por anular (no liga el
// cargo); «no existe» solo cuando una lectura de la misma acción la ve anulada y, con pagos, con la
// confirmación del supervisor; quitarla solo si no está anulada.
func TestFacturaPorAnular(t *testing.T) {
	e := nuevoEntorno(t)
	e.contrato0143(true)
	orden, p := e.porConfirmar()
	emitida := e.reloj.Format(time.RFC3339)
	sup := e.supervisorCon(nucleo.PermIntegracionesConfirmarCobro)
	sinConcepto := `[{"concepto_id":null,"precio_unitario":"350.00","cantidad":1},{"concepto_id":null,"precio_unitario":"100.00","cantidad":1}]`
	e.fijarFactura(facturaArgos(902, orden, "emitida", emitida, sinConcepto))
	r, err := e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: p.Version, Existe: true, FacturaID: ptrEntero(902), Motivo: "La factura vieja"})
	if err != nil || r.FacturaPorAnular == nil || *r.FacturaPorAnular != 902 || !r.PorConfirmar || r.Estado != pendientes.EstadoFallidoNegocio {
		t.Fatalf("factura por anular: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND factura_id IS NULL`, p.ID); n != 1 {
		t.Fatal("la factura por anular no liga el cargo")
	}
	noExiste := func(v int32, pagos bool) (pendientes.Pendiente, error) {
		return e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: v, Existe: false, PagosAnulados: pagos, Motivo: "Anulada en Argos"})
	}
	_, err = noExiste(r.Version, false)
	codigoDe(t, err, 422, "factura_por_anular_vigente")
	// Quitarla (no está anulada) y volver a indicarla.
	r, err = e.m.QuitarFacturaPorAnular(sup, pendientes.EntradaQuitarPorAnular{ID: p.ID, Version: r.Version, Motivo: "Era otra factura"})
	if err != nil || r.FacturaPorAnular != nil || !r.PorConfirmar {
		t.Fatalf("quitada: %+v %v", r, err)
	}
	r, err = e.m.ConfirmarCobro(sup, pendientes.EntradaConfirmarCobro{ID: p.ID, Version: r.Version, Existe: true, FacturaID: ptrEntero(902), Motivo: "La factura vieja"})
	if err != nil || r.FacturaPorAnular == nil {
		t.Fatalf("otra vez por anular: %+v %v", r, err)
	}
	// Anulada en Argos, con pagos: no se quita; «no existe» exige confirmar los pagos.
	anulada := strings.Replace(facturaArgos(902, orden, "anulada", emitida, sinConcepto), `"saldo":"450.00"`, `"saldo":"100.00"`, 1)
	e.fijarFactura(anulada)
	_, err = e.m.QuitarFacturaPorAnular(sup, pendientes.EntradaQuitarPorAnular{ID: p.ID, Version: r.Version, Motivo: "Era otra factura"})
	codigoDe(t, err, 409, "factura_anulada")
	_, err = noExiste(r.Version, false)
	codigoDe(t, err, 422, "pagos_por_confirmar")
	r, err = noExiste(r.Version, true)
	if err != nil || r.PorConfirmar || r.ReemplazadoPor == nil {
		t.Fatalf("no existe: %+v %v", r, err)
	}
}

// Sin el concepto de instalación (Argos no lo informa), el reemplazo nace por resolver con todas las
// líneas: visible, sin salir solo y sin perderlas.
func TestReemplazoSinConceptoQuedaPorResolver(t *testing.T) {
	e := nuevoEntorno(t)
	e.contrato0143(true)
	e.argos.mu.Lock()
	e.argos.conceptos = `[]`
	e.argos.tipos["/facturas/cargo-unico"] = "urn:argosisp:problema:capacidad_no_disponible"
	e.argos.mu.Unlock()
	orden := nuevaOrden()
	e.cerrar(cierreConLineas(orden, 5, 9))
	e.argos.responder("/facturas/cargo-unico", 422)
	e.procesar()
	cs := e.cargosDe(orden)
	if len(cs) != 2 {
		t.Fatalf("original y reemplazo: %+v", cs)
	}
	for clave, c := range cs {
		if clave == "iris:cargo:"+orden {
			continue
		}
		if c.Estado != pendientes.EstadoFallidoNegocio || !c.DatosIncompletos || len(c.Payload["lineas"].([]any)) != 2 {
			t.Fatalf("reemplazo por resolver: %+v", c)
		}
	}
	if n := len(e.argos.copia()); n != 1 {
		t.Fatalf("solo salió el original: %d", n)
	}
}
