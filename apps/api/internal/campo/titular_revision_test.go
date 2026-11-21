package campo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"strconv"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Hallazgos de la revisión de seguridad de #273 sobre el paso «Confirmar titular y plan».

func (e *entornoTitular) versionPendiente(clave string) int32 {
	e.t.Helper()
	var v int32
	if err := pruebas.Pool(e.t).QueryRow(context.Background(), "SELECT version FROM integracion.pendientes WHERE idempotency_key = $1", clave).Scan(&v); err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *entornoTitular) versionOrden(id int64) int32 {
	e.t.Helper()
	return deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: id}))(e.t).Version
}

func (e *entornoTitular) cerradaConCargo(orden string) {
	e.t.Helper()
	if _, err := pruebas.Pool(e.t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), 'orden.cerrada', 1, $1)",
		`{"orden_id":"`+orden+`","tipo":"instalacion","resultado":"exitosa","cliente_id":990,"servicio_id":9901,"bodega_id":9,`+
			`"materiales":[{"producto_id":77,"cantidad":"2","bodega_id":9}],"cargo":{"monto":"150.00","descripcion":"Instalación"}}`); err != nil {
		e.t.Fatal(err)
	}
	e.relay()
}

// ALTA 1: el consumo (que puede emitir cargos) espera a los pasos del titular, como el cobro.
func TestTitularConsumoEsperaAlTitular(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()

	// Argos no responde al cambio de titular (503): el consumo espera, no sale.
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/cambiar-titular", "503")
	ops := append([]campo.OperacionLote{op(campo.OpCambiarTitular, o.ID, datosCambioTitular())}, trabajoDeInstalacion(o.ID)[2:]...)
	esperar(t, e.lote(ct, ops...), repetir(len(ops), "aplicada")...)
	e.relay()
	e.procesar()
	pt, consumo := e.pendiente("iris:titular:"+orden+":1"), e.pendiente("iris:consumo:"+orden)
	if pt.Estado != pendientes.EstadoPendiente || len(consumo.DependeDe) != 1 || consumo.DependeDe[0] != pt.ID || consumo.Estado != pendientes.EstadoPendiente {
		t.Fatalf("el consumo espera al titular: %+v %+v", pt, consumo)
	}
	if l := e.falso.porClave("iris:consumo:" + orden); l != nil {
		t.Fatalf("el consumo salió antes del titular: %+v", l)
	}

	// El cambio de titular ya rechazado al cerrar: el consumo nace bloqueado.
	o2 := e.instalacion(tec, ct, true)
	orden2 := strconv.FormatInt(o2.ID, 10)
	e.falso.rechazar("/cambiar-titular", "validacion")
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o2.ID, datosCambioTitular())), "aplicada")
	e.procesar()
	if p := e.pendiente("iris:titular:" + orden2 + ":1"); p.Estado != pendientes.EstadoFallidoNegocio {
		t.Fatalf("A39 rechazado: %+v", p)
	}
	cierre := trabajoDeInstalacion(o2.ID)[2:]
	esperar(t, e.lote(ct, cierre...), repetir(len(cierre), "aplicada")...)
	e.relay()
	if c := e.pendiente("iris:consumo:" + orden2); c.Estado != pendientes.EstadoBloqueado {
		t.Fatalf("consumo bloqueado al nacer: %+v", c)
	}
	e.procesar()
	if l := e.falso.porClave("iris:consumo:" + orden2); l != nil {
		t.Fatalf("el consumo salió con el titular rechazado: %+v", l)
	}
}

// ALTA 2: titular_no_coincide deja la orden en revisión (marca persistente): ni A39 ni A41 se toman
// otra vez hasta que un supervisor la levanta, A41 nunca reemplaza a un A39 rechazado y GET /titular
// no dice el código de Argos.
func TestTitularNoCoincideNoSeReemplazaSinSupervisor(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/cambiar-titular", "titular_no_coincide")
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	e.procesar()
	if n := contar(t, "SELECT count(*) FROM campo.orden_titular WHERE orden_id = $1 AND en_revision", o.ID); n != 1 {
		t.Fatalf("marca en_revision: %d", n)
	}
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "rechazada:titular_en_revision")
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "rechazada:titular_en_revision")

	// Sin oráculo: solo «en revisión», sin el código de Argos (ni para el técnico ni para el panel).
	for _, ctx := range []context.Context{ct, e.cs} {
		est := deber(e.m.Titular.Ver.Ejecutar(ctx, campo.EntradaVerOrden{ID: o.ID}))(t)
		crudo, _ := json.Marshal(est)
		if !est.EnRevision || est.Pasos[0].Estado != "en_revision" || est.Pasos[0].Codigo != nil || strings.Contains(string(crudo), "titular_no_coincide") {
			t.Fatalf("estado en revisión: %s", crudo)
		}
	}

	// Solo un supervisor con ordenes.validar_identidad la levanta, con motivo y auditado.
	_, err := e.m.Titular.LevantarRevision.Ejecutar(ct, campo.EntradaLevantarRevision{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "Ya lo revisé con el cliente"})
	problema(t, err, 403, "prohibido")
	_, err = e.m.Titular.LevantarRevision.Ejecutar(e.cs, campo.EntradaLevantarRevision{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "corto"})
	problema(t, err, 422, "validacion")
	deber(e.m.Titular.LevantarRevision.Ejecutar(e.cs, campo.EntradaLevantarRevision{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "El nombre estaba mal escrito; revisado con el DPI físico"}))(t)
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.levantar_revision_titular' AND entidad_id = $1 AND error IS NULL", orden); n != 1 {
		t.Fatalf("auditoría de levantar la revisión: %d", n)
	}
	_, err = e.m.Titular.LevantarRevision.Ejecutar(e.cs, campo.EntradaLevantarRevision{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "Otra vez la misma revisión"})
	problema(t, err, 409, "sin_revision")

	// «La misma persona» nunca reemplaza a un cambio de titular rechazado; otro A39 sí.
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "rechazada:titular_ya_cambiado")
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
	if p := e.pendiente("iris:titular:" + orden + ":1"); p.Estado != pendientes.EstadoResueltoManual {
		t.Fatalf("el A39 rechazado queda reemplazado: %+v", p)
	}
	sinSensibles(t)
}

// MEDIA 4: cancelar o fallar la orden detiene los pasos que no salieron; lo en curso termina y avisa;
// lo hecho no se deshace (cambiadoEnArgos); al reabrir, el paso se toma de nuevo.
func TestTitularCancelarDetieneLosPasos(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()

	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad()),
		op(campo.OpCompletarDatos, o.ID, map[string]any{"cliente": map[string]any{"telefono": telefonoPrueba}})), "aplicada", "aplicada")
	o2 := deber(e.m.CancelarOrden.Ejecutar(e.cs, campo.EntradaMotivo{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "El cliente desistió"}))(t)
	for _, k := range []string{"iris:identidad:" + orden + ":1", "iris:datos:" + orden + ":1"} {
		var estado, como string
		if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT estado, resuelto_como FROM integracion.pendientes WHERE idempotency_key = $1", k).Scan(&estado, &como); err != nil {
			t.Fatal(err)
		}
		if estado != pendientes.EstadoResueltoManual || como != pendientes.ComoCancelada {
			t.Fatalf("%s tras cancelar: %s %s", k, estado, como)
		}
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NULL AND llave_cifrada IS NULL AND motivo_borrado = 'cancelado'", o.ID); n != 2 {
		t.Fatalf("depósitos borrados al cancelar: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_resuelto_manual' AND payload->>'entidad_id' = $1 AND payload->>'motivo' = 'orden_cancelada'", orden); n != 2 {
		t.Fatalf("eventos de resuelto a mano: %d", n)
	}
	e.procesar()
	if l := e.falso.porClave("iris:identidad:" + orden + ":1"); l != nil {
		t.Fatalf("salió un paso de una orden cancelada: %+v", l)
	}
	// Al reabrir, todo se toma de nuevo.
	deber(e.m.ReabrirOrden.Ejecutar(e.cs, campo.EntradaMotivo{ID: o.ID, Version: o2.Version, Motivo: "El cliente volvió"}))(t)
	if est := deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); est.Identidad != "falta" || est.QuienFirma != nil {
		t.Fatalf("tras reabrir: %+v", est)
	}

	// Un paso en curso al fallar la orden termina; si queda hecho, avisa a supervisión.
	p := e.instalacion(tec, ct, true)
	ordenP := strconv.FormatInt(p.ID, 10)
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, p.ID, datosCambioTitular())), "aplicada")
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET estado = 'en_curso', tomado_en = now() - interval '1 hour' WHERE idempotency_key = $1",
		"iris:titular:"+ordenP+":1"); err != nil {
		t.Fatal(err)
	}
	esperar(t, e.lote(ct, op(campo.OpFallar, p.ID, map[string]any{"motivoFalla": "cliente_rechaza"})), "aplicada")
	if pt := e.pendiente("iris:titular:" + ordenP + ":1"); pt.Estado != pendientes.EstadoEnCurso {
		t.Fatalf("lo en curso no se toca: %+v", pt)
	}
	e.procesar()
	if pt := e.pendiente("iris:titular:" + ordenP + ":1"); pt.Estado != pendientes.EstadoHecho {
		t.Fatalf("el en curso termina: %+v", pt)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.paso_titular_tras_cancelar' AND payload->>'orden_id' = $1", ordenP); n != 1 {
		t.Fatalf("aviso a supervisión: %d", n)
	}
	if est := deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: p.ID}))(t); !est.CambiadoEnArgos {
		t.Fatalf("titular ya cambiado en Argos: %+v", est)
	}
}

// MEDIA 5 y B9: un cambio de titular resuelto a mano (no reemplazado por otra toma) deja el cobro y el
// consumo sin salir, salvo que quien lo resolvió indique el cliente final; resolverlo exige además
// ordenes.validar_identidad.
func TestTitularResueltoAManoConfirmaElCliente(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	for _, cliente := range []*int64{nil, ptr(int64(888))} {
		o := e.instalacion(tec, ct, true)
		orden := strconv.FormatInt(o.ID, 10)
		e.falso.rechazar("/cambiar-titular", "servicio_no_pendiente")
		esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosCambioTitular())), "aplicada")
		e.procesar()
		clave := "iris:titular:" + orden + ":1"
		pt := e.pendiente(clave)
		sinPermiso := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), e.sup, 1, nucleo.PermIntegracionesResolverPendiente, nucleo.PermIntegracionesVerPendientes)
		_, err := e.pend.ResolverManual.Ejecutar(sinPermiso, pendientes.EntradaResolverManual{ID: pt.ID, Version: e.versionPendiente(clave), Motivo: "Lo corregí en Argos"})
		problema(t, err, 403, "prohibido")
		deber(e.pend.ResolverManual.Ejecutar(e.cs, pendientes.EntradaResolverManual{ID: pt.ID, Version: e.versionPendiente(clave),
			Motivo: "Lo corregí en Argos a mano", ClienteID: cliente}))(t)
		e.cerradaConCargo(orden)
		cargo, consumo := e.pendiente("iris:cargo:"+orden), e.pendiente("iris:consumo:"+orden)
		if cliente == nil {
			if cargo.Estado != pendientes.EstadoFallidoNegocio || consumo.Estado != pendientes.EstadoFallidoNegocio {
				t.Fatalf("sin cliente confirmado no salen: %+v %+v", cargo, consumo)
			}
			var motivo string
			_ = pruebas.Pool(t).QueryRow(context.Background(), "SELECT ultimo_error FROM integracion.pendientes WHERE idempotency_key = $1", "iris:cargo:"+orden).Scan(&motivo)
			if !strings.Contains(motivo, pendientes.MotivoTitularResueltoAMano) {
				t.Fatalf("motivo: %q", motivo)
			}
			continue
		}
		e.procesar()
		if l := e.falso.porClave("iris:cargo:" + orden); l == nil || l.JSON["cliente_id"] != 888.0 {
			t.Fatalf("cobro al cliente indicado: %+v", l)
		}
		if l := e.falso.porClave("iris:consumo:" + orden); l == nil {
			t.Fatal("el consumo sale con el cliente confirmado")
		}
	}
	// Reemplazado por otra toma no es «a mano»: el cobro va al titular nuevo (prueba del cobro).
}

// MEDIA 7: toda instalación con el servicio pendiente en Argos exige la identidad (tenga prospecto o
// no); con el servicio ya activo en la instantánea, no.
func TestTitularCierreSinProspecto(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, false)
	cierre := trabajoDeInstalacion(o.ID)[2:]
	r := e.lote(ct, cierre...)
	if fin := r.Resultados[len(r.Resultados)-1]; fin.Error == nil || fin.Error.Codigo() != "identidad_requerida" {
		t.Fatalf("instalación sin prospecto y sin identidad: %+v", fin)
	}
	// Servicio ya activo según la instantánea de Argos: no hace falta.
	pool := pruebas.Pool(t)
	for _, sql := range []string{
		"INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (995, 1, 'Cliente activo', now()) ON CONFLICT (id) DO NOTHING",
		"INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, estado, sincronizado_en) VALUES (9905, 1, 995, 'activo', now()) ON CONFLICT (id) DO UPDATE SET estado = 'activo'",
	} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = 995")
	})
	a := deber(e.m.CrearOrden.Ejecutar(e.cs, campo.EntradaCrearOrden{Tipo: "instalacion", Direccion: ptr("Zona 10"), ClienteID: ptr(int64(995)), ServicioID: ptr(int64(9905))}))(t)
	a = deber(e.m.ProgramarOrden.Ejecutar(e.cs, campo.EntradaProgramarOrden{ID: a.ID, Version: a.Version, TecnicoID: &tec, BodegaID: ptr(int64(9)), Inicio: hoyEnGuatemala(21)}))(t)
	ops := trabajoDeInstalacion(a.ID)[1:]
	esperar(t, e.lote(ct, ops...), repetir(len(ops), "aplicada")...)
	e.relay() // lo encolado al cerrar queda con este conector (no con el de la prueba siguiente)
}

// MEDIA 3, B8, B10, B12 y B13.
func TestTitularRevisionSeguridad(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	orden := strconv.FormatInt(o.ID, 10)

	// MEDIA 3: despacho no tiene los permisos del paso; con ellos, tampoco en una orden ajena.
	d := despacho(t)
	asignarEmpresa(t, d, 1)
	cd := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), d, 1, permisosDe("despacho")...)
	esperar(t, e.lote(cd, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "rechazada:prohibido")
	cdMas := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), d, 1, append(permisosDe("despacho"), nucleo.PermOrdenesCambiarTitular)...)
	esperar(t, e.lote(cdMas, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "rechazada:fuera_de_alcance")

	// B10: solo la cabecera de la imagen, hasta 8000 × 8000.
	var grande bytes.Buffer
	if err := png.Encode(&grande, image.NewGray(image.Rect(0, 0, 9000, 1))); err != nil {
		t.Fatal(err)
	}
	enorme := datosIdentidad()
	enorme["frente"] = map[string]any{"mime": "image/png", "base64": b64(grande.Bytes())}
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, enorme)), "rechazada:dimensiones_excedidas")
	rota := datosIdentidad()
	rota["frente"] = map[string]any{"mime": "image/jpeg", "base64": b64(append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 200)...))}
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, rota)), "rechazada:imagen_invalida")

	// B8: lo agotado no se toma otra vez (se reintenta con su clave) ni se valida encima.
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "aplicada")
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET estado = 'agotado' WHERE idempotency_key = $1", "iris:identidad:"+orden+":1"); err != nil {
		t.Fatal(err)
	}
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "rechazada:paso_agotado")
	_, err := e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o.ID, Version: e.versionOrden(o.ID), Motivo: "Validada encima de un agotado"})
	problema(t, err, 409, "identidad_en_envio")

	// B11 y B12: cada depósito con su llave, atada a su orden y su empresa: cambiado de orden no abre.
	o2 := e.instalacion(tec, ct, true)
	orden2 := strconv.FormatInt(o2.ID, 10)
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o2.ID, datosIdentidad())), "aplicada")
	o3 := e.instalacion(tec, ct, true)
	orden3 := strconv.FormatInt(o3.ID, 10)
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o3.ID, datosIdentidad())), "aplicada")
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = ANY($1) AND llave_cifrada IS NOT NULL", []int64{o2.ID, o3.ID}); n != 2 {
		t.Fatalf("una llave por depósito: %d", n)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), `
		WITH a AS (SELECT transito_id FROM integracion.pendientes WHERE idempotency_key = $1),
		     b AS (SELECT transito_id FROM integracion.pendientes WHERE idempotency_key = $2)
		UPDATE integracion.pendientes p SET transito_id = CASE WHEN p.idempotency_key = $1 THEN (SELECT transito_id FROM b) ELSE (SELECT transito_id FROM a) END
		WHERE p.idempotency_key IN ($1, $2)`, "iris:identidad:"+orden2+":1", "iris:identidad:"+orden3+":1"); err != nil {
		t.Fatal(err)
	}
	e.procesar()
	for _, k := range []string{"iris:identidad:" + orden2 + ":1", "iris:identidad:" + orden3 + ":1"} {
		if l := e.falso.porClave(k); l != nil {
			t.Fatalf("un depósito de otra orden abrió: %+v", l)
		}
		if p := e.pendiente(k); p.Estado != pendientes.EstadoPendiente {
			t.Fatalf("%s sigue en la cola sin salir: %+v", k, p)
		}
	}

	// B13: la lectura del estado del paso se audita; el motivo libre de A39 y A40 no se ve en el panel.
	deber(e.m.Titular.Ver.Ejecutar(ct, campo.EntradaVerOrden{ID: o2.ID}))(t)
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.ver_titular' AND entidad_id = $1", orden2); n != 1 {
		t.Fatalf("lectura auditada: %d", n)
	}
	o4 := e.instalacion(tec, ct, true)
	orden4 := strconv.FormatInt(o4.ID, 10)
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o4.ID, datosCambioTitular()),
		op(campo.OpCambiarPlan, o4.ID, map[string]any{"planId": 301, "motivo": "otro", "motivoTexto": "Texto libre del cliente sobre su plan"})), "aplicada", "aplicada")
	lista := deber(e.pend.Listar.Ejecutar(e.cs, pendientes.EntradaListar{Entidad: "orden", EntidadID: orden4}))(t)
	crudo, _ := json.Marshal(lista)
	if strings.Contains(string(crudo), "queda de titular su madre") || strings.Contains(string(crudo), "Texto libre del cliente") {
		t.Fatalf("el panel ve el motivo libre: %s", crudo)
	}
	sinSensibles(t)
}
