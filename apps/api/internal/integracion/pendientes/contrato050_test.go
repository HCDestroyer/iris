package pendientes_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato Iris ↔ ArgosISP 0.5.0 §3.3: `tecnico` en consumos, comodatos y devoluciones.

func (e *entorno) argos16() {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE integracion.conectores SET version_remota = '1.6.0' WHERE id = $1`, e.conID); err != nil {
		e.t.Fatal(err)
	}
}

func (e *entorno) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatal(err)
	}
}

// tecnico crea un usuario técnico con ficha de agenda vinculada (idExterno vacío = sin vincular).
func (e *entorno) tecnico(idExterno string) int64 {
	e.t.Helper()
	u, err := identidad.Nuevo(identidad.Config{}).CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Técnico", Correo: pruebas.Correo("tec"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		e.t.Fatal(err)
	}
	if idExterno == "" {
		e.exec(`INSERT INTO agenda.tecnicos (usuario_id) VALUES ($1)`, u.ID)
	} else {
		e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = $1`, idExterno)
		e.exec(`INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_vinculado_en) VALUES ($1, $2, now())`, u.ID, idExterno)
	}
	return u.ID
}

func (e *entorno) llamadaA(clave string) llamada {
	e.t.Helper()
	ls := e.argos.copia()
	for i := len(ls) - 1; i >= 0; i-- {
		if ls[i].Clave == clave {
			return ls[i] // la última
		}
	}
	e.t.Fatalf("Argos no recibió %s", clave)
	return llamada{}
}

// Con Argos ≥ 1.6.0 y el técnico vinculado: tecnico en vez de bodega_id; el consumo lleva solo
// consumibles (un comodato se omite con aviso) y el cargo de un producto que Argos ya cobra por
// modalidad no se manda (409 cargo_duplicado).
func TestCierreConTecnicoArgos16(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	tec := e.tecnico("u-16")
	orden := nuevaOrden()
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "instalacion", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20,
		"tecnico_id": tec, "bodega_id": 3, "tecnico": map[string]any{"id_externo": "u-16"},
		"equipos": []any{map[string]any{"accion": "instalar", "serie": "ONT-1", "bodega_id": 3}},
		"materiales": []any{
			map[string]any{"producto_id": 5, "cantidad": 2, "bodega_id": 3, "modalidad": "consumible_cobro"},
			map[string]any{"producto_id": 6, "cantidad": 1, "bodega_id": 3, "modalidad": "comodato"},
			map[string]any{"producto_id": 7, "cantidad": 3, "bodega_id": 3, "modalidad": nil},
		},
		"cargo": map[string]any{"producto_id": 5, "monto": "50.00", "descripcion": "Cable"},
	})
	ps := e.pendientes(orden)
	cargo, ok := ps["iris:cargo:"+orden]
	if !ok || len(ps) != 4 {
		t.Fatalf("el cargo se encola siempre (nunca se descarta): %+v", ps)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND depende_de = ARRAY[$2::bigint]`, cargo.ID, ps["iris:consumo:"+orden].ID); n != 1 {
		t.Fatal("el cargo del mismo producto espera al consumo")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_omitido' AND payload->>'orden_id' = $1
		AND payload->>'motivo' = 'material_comodato'`, orden); n != 1 {
		t.Fatalf("aviso del comodato: %d", n)
	}
	// Argos ya lo cobró por modalidad en el consumo: 409 cargo_duplicado = hecho.
	e.argos.mu.Lock()
	e.argos.tipos["/facturas/cargo-unico"] = "urn:argosisp:problema:cargo_duplicado"
	e.argos.mu.Unlock()
	e.argos.responder("/facturas/cargo-unico", 409)
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND tecnico_usuario_id = $2`, orden, tec); n != 2 {
		t.Fatalf("tecnico_usuario_id (comodato y consumo): %d", n)
	}
	e.argos.mu.Lock()
	e.argos.cuerpos["/inventario/consumos"] = `{"id": 900, "movimientos": [{"id": 900}], "cargos": [{"id": 31, "producto_id": 5, "monto": "50.00", "nota": "x"}], "factura_id": 77}`
	e.argos.mu.Unlock()
	e.procesar()
	c := e.llamadaA("iris:consumo:" + orden)
	lineas, _ := c.Cuerpo["lineas"].([]any)
	if _, conBodega := c.Cuerpo["bodega_id"]; conBodega || c.Cuerpo["tecnico"].(map[string]any)["id_externo"] != "u-16" || len(lineas) != 2 {
		t.Fatalf("consumo con tecnico y solo consumibles: %s", c.Crudo)
	}
	if com := e.llamadaA("iris:comodato:" + orden + ":ONT-1"); com.Cuerpo["tecnico"] == nil || com.Cuerpo["serie"] != "ONT-1" {
		t.Fatalf("comodato con tecnico: %s", com.Crudo)
	}
	// cargos[] y factura_id del consumo quedan en el pendiente (para la orden).
	if f := e.pendientes(orden)["iris:cargo:"+orden]; f.Estado != "hecho" {
		t.Fatalf("cargo conciliado: %+v", f)
	}
	ps2, err := pendientes.DeEntidad(context.Background(), e.pool, "orden", orden)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps2 {
		if p.Operacion == pendientes.OpConsumo && (len(p.Cargos) != 1 || p.Cargos[0].Monto != "50.00" || p.FacturaID == nil || *p.FacturaID != 77) {
			t.Fatalf("cargos del consumo: %+v", p)
		}
	}
}

// Argos < 1.6.0 (sin versión): todo sigue con bodega_id aunque el técnico esté vinculado; si luego
// Argos pasa a 1.6.0 y la orden se cierra de nuevo, lo ya encolado no cambia ni se marca conflicto.
func TestCierreConArgosAnteriorUsaBodega(t *testing.T) {
	e := nuevoEntorno(t)
	tec := e.tecnico("u-15")
	orden := nuevaOrden()
	payload := map[string]any{
		"orden_id": orden, "tipo": "retiro", "resultado": "exitosa", "servicio_id": 20, "tecnico_id": tec, "bodega_id": 3,
		"tecnico":    map[string]any{"id_externo": "u-15"},
		"equipos":    []any{map[string]any{"accion": "retirar", "unidad_id": 44, "condicion": "buena", "bodega_id": 3}},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "bodega_id": 3}},
	}
	e.cerrar(payload)
	e.argos16()
	e.cerrar(payload)
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND conflicto IS NOT NULL`, orden); n != 0 {
		t.Fatal("cambiar bodega_id por tecnico no es un conflicto")
	}
	e.procesar()
	if d := e.llamadaA("iris:devolucion:" + orden + ":44"); d.Cuerpo["bodega_id"] != float64(3) || d.Cuerpo["tecnico"] != nil {
		t.Fatalf("devolución con bodega_id: %s", d.Crudo)
	}
	if c := e.llamadaA("iris:consumo:" + orden); c.Cuerpo["bodega_id"] != float64(3) || c.Cuerpo["tecnico"] != nil {
		t.Fatalf("consumo con bodega_id: %s", c.Crudo)
	}
}

// Rechazo por el técnico (422 tecnico_no_encontrado): fallido_negocio corregible; el reintento a mano
// exige el vínculo y manda el actual (integracion.pendiente_corregido). 409 existencia_insuficiente es
// de negocio y no se reintenta solo. Un cargo del mismo producto (modalidad desconocida) espera al
// consumo y un 409 cargo_duplicado cuenta como hecho.
func TestCorregibleYCargoDuplicado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	tec := e.tecnico("u-viejo")
	ident := identidad.Nuevo(identidad.Config{})
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Supervisor", Correo: pruebas.Correo("sup"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 1, nucleo.PermIntegracionesReintentar)
	orden := nuevaOrden()
	e.argos.mu.Lock()
	e.argos.tipos["/inventario/consumos"] = "urn:argosisp:problema:tecnico_no_encontrado"
	e.argos.tipos["/facturas/cargo-unico"] = "urn:argosisp:problema:cargo_duplicado"
	e.argos.mu.Unlock()
	e.argos.responder("/inventario/consumos", 422, 201)
	e.argos.responder("/facturas/cargo-unico", 409)
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20, "tecnico_id": tec,
		"tecnico":    map[string]any{"id_externo": "u-viejo"},
		"materiales": []any{map[string]any{"producto_id": 7, "cantidad": 1}},
		"cargo":      map[string]any{"producto_id": 7, "descripcion": "Visita"},
	})
	e.procesar()
	ps := e.pendientes(orden)
	consumo, cargo := ps["iris:consumo:"+orden], ps["iris:cargo:"+orden]
	if consumo.Estado != "fallido_negocio" || cargo.Estado != "bloqueado" {
		t.Fatalf("consumo rechazado y cargo esperándolo: %+v %+v", consumo, cargo)
	}
	ps2, _ := pendientes.DeEntidad(context.Background(), e.pool, "orden", orden)
	for _, p := range ps2 {
		if p.Operacion == pendientes.OpConsumo && (!p.Corregible || p.CodigoError == nil || *p.CodigoError != "tecnico_no_encontrado") {
			t.Fatalf("corregible: %+v", p)
		}
	}
	// Sin vínculo: el reintento lo pide.
	e.exec(`UPDATE agenda.tecnicos SET argos_id_externo = NULL, argos_vinculado_en = NULL WHERE usuario_id = $1`, tec)
	_, err = e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: consumo.ID, Version: consumo.Version})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "tecnico_sin_vincular" {
		t.Fatalf("sin vínculo: %v", err)
	}
	// Vinculado de nuevo (a otro usuario de Argos): el cuerpo se corrige.
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = 'u-nuevo'`)
	e.exec(`UPDATE agenda.tecnicos SET argos_id_externo = 'u-nuevo', argos_vinculado_en = now() WHERE usuario_id = $1`, tec)
	p, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: consumo.ID, Version: consumo.Version})
	if err != nil || p.Estado != "pendiente" || p.Payload["tecnico"].(map[string]any)["id_externo"] != "u-nuevo" {
		t.Fatalf("corregido: %+v %v", p, err)
	}
	if n := e.eventos("integracion.pendiente_corregido", orden); n != 1 {
		t.Fatalf("pendiente_corregido: %d", n)
	}
	e.procesar() // consumo hecho → el cargo vuelve a la cola
	e.procesar() // cargo: 409 cargo_duplicado → hecho conciliado
	ps = e.pendientes(orden)
	if ps["iris:consumo:"+orden].Estado != "hecho" || ps["iris:cargo:"+orden].Estado != "hecho" {
		t.Fatalf("tras corregir: %+v", ps)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1 AND codigo_error = 'cargo_duplicado' AND ultimo_status = 409`,
		"iris:cargo:"+orden); n != 1 {
		t.Fatal("el cargo duplicado debía quedar hecho con la nota")
	}

	// 409 existencia_insuficiente: de negocio, no corregible.
	orden2 := nuevaOrden()
	e.argos.mu.Lock()
	e.argos.tipos["/inventario/consumos"] = "urn:argosisp:problema:existencia_insuficiente"
	e.argos.mu.Unlock()
	e.argos.responder("/inventario/consumos", 409)
	e.cerrar(map[string]any{"orden_id": orden2, "tipo": "visita", "resultado": "exitosa", "servicio_id": 20, "tecnico_id": tec,
		"tecnico": map[string]any{"id_externo": "u-nuevo"}, "materiales": []any{map[string]any{"producto_id": 7, "cantidad": 1}}})
	e.procesar()
	if f := e.pendientes(orden2)["iris:consumo:"+orden2]; f.Estado != "fallido_negocio" || f.Intentos != 1 {
		t.Fatalf("existencia insuficiente: %+v", f)
	}
}

func (e *entorno) contar(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// MEDIUM 2 de la revisión: el cargo nunca se descarta. Si el consumo falla atómicamente en Argos, el
// cargo queda esperándolo (bloqueado) y sale cuando el consumo se reintenta y queda hecho.
func TestCargoNoSeDescartaSiElConsumoFalla(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	tec := e.tecnico("u-cargo")
	orden := nuevaOrden()
	e.argos.mu.Lock()
	e.argos.tipos["/inventario/consumos"] = "urn:argosisp:problema:existencia_insuficiente"
	e.argos.mu.Unlock()
	e.argos.responder("/inventario/consumos", 409, 201)
	e.cerrar(map[string]any{
		"orden_id": orden, "tipo": "visita", "resultado": "exitosa", "cliente_id": 10, "servicio_id": 20, "tecnico_id": tec,
		"tecnico":    map[string]any{"id_externo": "u-cargo"},
		"materiales": []any{map[string]any{"producto_id": 5, "cantidad": 1, "modalidad": "consumible_cobro"}},
		"cargo":      map[string]any{"producto_id": 5, "monto": "50.00", "descripcion": "Cable"},
	})
	e.procesar()
	ps := e.pendientes(orden)
	if ps["iris:consumo:"+orden].Estado != "fallido_negocio" || ps["iris:cargo:"+orden].Estado != "bloqueado" {
		t.Fatalf("consumo fallido y cargo esperándolo: %+v", ps)
	}
	u, err := identidad.Nuevo(identidad.Config{}).CrearUsuario.Ejecutar(pruebas.ContextoSistema(e.pool), identidad.EntradaCrearUsuario{
		Nombre: "Supervisor", Correo: pruebas.Correo("sup"), Contrasena: "contrasena-de-prueba-123"})
	if err != nil {
		t.Fatal(err)
	}
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, u.ID, 1, nucleo.PermIntegracionesReintentar)
	c := ps["iris:consumo:"+orden]
	if _, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: c.ID, Version: c.Version}); err != nil {
		t.Fatal(err)
	}
	e.procesar()
	ps = e.pendientes(orden)
	if ps["iris:consumo:"+orden].Estado != "hecho" || ps["iris:cargo:"+orden].Estado != "hecho" {
		t.Fatalf("tras el reintento, el cargo sale: %+v", ps)
	}
	if l := e.llamadaA("iris:cargo:" + orden); l.Cuerpo["producto_id"] != float64(5) {
		t.Fatalf("cargo: %s", l.Crudo)
	}
}

// MEDIUM 3 de la revisión: Argos volvió a 1.5.x. El 422 «bodega_id required» de un cuerpo con
// tecnico es corregible; al reintentar sin la capacidad, se replanifica con la bodega de la orden
// (sin bodega, 422 bodega_requerida).
func TestArgosVueltoA15ReplanificaConBodega(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	tec := e.tecnico("u-15b")
	var orden, sinBodega int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, bodega_id)
		SELECT id, 'Zona 1', 33 FROM campo.tipos_orden WHERE codigo = 'visita' RETURNING id`).Scan(&orden); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion)
		SELECT id, 'Zona 1' FROM campo.tipos_orden WHERE codigo = 'visita' RETURNING id`).Scan(&sinBodega); err != nil {
		t.Fatal(err)
	}
	e.argos.mu.Lock()
	e.argos.tipos["/inventario/consumos"] = "urn:argosisp:problema:validacion"
	e.argos.detalle = "bodega_id required"
	e.argos.mu.Unlock()
	e.argos.responder("/inventario/consumos", 422, 422, 201)
	for _, id := range []int64{orden, sinBodega} {
		e.cerrar(map[string]any{"orden_id": id, "tipo": "visita", "resultado": "exitosa", "servicio_id": 20, "tecnico_id": tec,
			"tecnico": map[string]any{"id_externo": "u-15b"}, "materiales": []any{map[string]any{"producto_id": 7, "cantidad": 1}}})
	}
	e.procesar()
	sup := pruebas.ContextoUsuarioEnEmpresa(e.pool, tec, 1, nucleo.PermIntegracionesReintentar)
	clave := func(id int64) string { return "iris:consumo:" + strconv.FormatInt(id, 10) }
	ps, _ := pendientes.DeEntidad(context.Background(), e.pool, "orden", strconv.FormatInt(orden, 10))
	if len(ps) != 1 || !ps[0].Corregible || *ps[0].CodigoError != pendientes.CodigoBodegaRequerida {
		t.Fatalf("corregible: %+v", ps)
	}
	// Argos volvió a 1.5.x (la revisión borró la versión).
	e.exec(`UPDATE integracion.conectores SET version_remota = NULL WHERE id = $1`, e.conID)
	f := e.pendientes(strconv.FormatInt(sinBodega, 10))[clave(sinBodega)]
	_, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: f.ID, Version: f.Version})
	if p, _ := nucleo.AProblema(err); p == nil || p.Codigo() != "bodega_requerida" {
		t.Fatalf("sin bodega: %v", err)
	}
	p, err := e.m.Reintentar.Ejecutar(sup, pendientes.EntradaReintentar{ID: ps[0].ID, Version: ps[0].Version})
	if err != nil || p.Payload["tecnico"] != nil || p.Payload["bodega_id"] != float64(33) || p.Estado != "pendiente" {
		t.Fatalf("replanificado con bodega_id: %+v %v", p, err)
	}
	e.procesar()
	if l := e.llamadaA(clave(orden)); l.Cuerpo["bodega_id"] != float64(33) || l.Cuerpo["tecnico"] != nil {
		t.Fatalf("Argos recibe bodega_id: %s", l.Crudo)
	}
	if f := e.pendientes(strconv.FormatInt(orden, 10))[clave(orden)]; f.Estado != "hecho" {
		t.Fatalf("hecho: %+v", f)
	}
}
