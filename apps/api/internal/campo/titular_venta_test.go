package campo_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
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

// contratoArgos fija el contrato que informa el conector de Argos (0.14.1: titular empresa).
func contratoArgos(t *testing.T, version string) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.conectores SET contrato_remoto = $1 WHERE sistema = 'argosisp'", version); err != nil {
		t.Fatal(err)
	}
}

// datosTitularEmpresa: cambio de titular a una empresa (contrato 0.14.1 §3.4.4).
func datosTitularEmpresa() map[string]any {
	d := datosCambioTitular()
	tit := d["titular"].(map[string]any)
	delete(tit, "documentoTipo")
	delete(tit, "documentoNumero")
	delete(tit, "mayorDeEdad")
	tit["tipo"], tit["nombre"], tit["tipoDocumentoFiscal"], tit["nitCui"] = "empresa", "Distribuidora La Ceiba, S. A.", "nit", nitPrueba
	tit["representante"] = map[string]any{"nombre": "Rosa Elvira Quiñónez", "documentoTipo": "pasaporte", "documentoNumero": "RQ998877", "mayorDeEdad": true}
	d["motivoCodigo"], d["motivo"] = "otro", "La venta se hizo a nombre de la empresa del cliente"
	return d
}

// Titular empresa (contrato 0.14.1, decisión del PM): la empresa va con su razón social y el NIT en
// los campos fiscales, sin documento de identidad, y la identidad es la de su representante, con DPI
// o pasaporte. El tipo de titular no depende del modo de la venta (el plan va aparte, ajuste f de
// §10.11). Con un Argos < 0.14.1 no se ofrece.
func TestTitularEmpresarial(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	conModo(t, o, "empresarial")
	orden := strconv.FormatInt(o.ID, 10)

	// Argos sin 0.14.1: el cambio a una empresa no se ofrece (422 con motivo claro).
	est := deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if est.TitularEmpresaDisponible {
		t.Fatalf("titular empresa con Argos < 0.14.1: %+v", est)
	}
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosTitularEmpresa())), "rechazada:titular_empresa_no_disponible")
	contratoArgos(t, "0.14.1")

	// Una empresa necesita representante y NIT y no lleva documento propio; una persona no lleva
	// representante.
	persona := datosCambioTitular()
	persona["titular"].(map[string]any)["representante"] = map[string]any{"nombre": "Rosa Elvira Quiñónez", "documentoTipo": "dpi",
		"documentoNumero": dpiPrueba, "mayorDeEdad": true}
	sinRepresentante := datosTitularEmpresa()
	delete(sinRepresentante["titular"].(map[string]any), "representante")
	conDocumento := datosTitularEmpresa()
	conDocumento["titular"].(map[string]any)["documentoTipo"] = "dpi"
	conDocumento["titular"].(map[string]any)["documentoNumero"] = dpiPrueba
	sinNIT := datosTitularEmpresa()
	sinNIT["titular"].(map[string]any)["tipoDocumentoFiscal"], sinNIT["titular"].(map[string]any)["nitCui"] = "cf", nil
	for i, datos := range []struct {
		d     map[string]any
		regla string
	}{{persona, "persona"}, {sinRepresentante, "required"}, {conDocumento, "empresa"}, {sinNIT, "empresa"}} {
		r := e.lote(ct, op(campo.OpCambiarTitular, o.ID, datos.d))
		esperar(t, r, "rechazada:validacion")
		if r.Resultados[0].Error == nil || len(r.Resultados[0].Error.Errors) == 0 || r.Resultados[0].Error.Errors[0].Rule != datos.regla {
			t.Fatalf("caso %d: %+v, esperaba la regla %s", i, r.Resultados[0].Error, datos.regla)
		}
	}
	// El representante con pasaporte (decisión del PM).
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosTitularEmpresa())), "aplicada")
	e.procesar()
	l := e.falso.porClave("iris:titular:" + orden + ":1")
	if l == nil {
		t.Fatal("A39 no salió")
	}
	cli, _ := l.JSON["cliente"].(map[string]any)
	rep, _ := l.JSON["representante"].(map[string]any)
	for _, k := range []string{"documento_tipo", "documento_numero", "mayor_de_edad"} {
		if _, ok := cli[k]; ok {
			t.Fatalf("A39 empresa con %s en cliente: %+v", k, cli)
		}
	}
	if cli["tipo"] != "empresa" || cli["nombre"] != "Distribuidora La Ceiba, S. A." || cli["tipo_documento_fiscal"] != "nit" || cli["nit_cui"] != nitPrueba ||
		rep["documento_tipo"] != "pasaporte" || rep["documento_numero"] != "RQ998877" || rep["nombre"] != "Rosa Elvira Quiñónez" || rep["mayor_de_edad"] != true ||
		l.Archivos["frente"] != tipoYTamano(frenteJPEG) {
		t.Fatalf("A39 empresarial: %+v %+v", l.JSON, l.Archivos)
	}
	est = deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if est.Modo != "empresarial" || !est.TitularEmpresaDisponible || est.TipoTitular == nil || *est.TipoTitular != "empresa" ||
		est.DocumentoTipo == nil || *est.DocumentoTipo != "pasaporte" {
		t.Fatalf("estado: %+v", est)
	}
	// Pendientes, auditoría y eventos: todo el representante sale como *** salvo documento_tipo.
	pag := deber(e.pend.Listar.Ejecutar(e.cs, pendientes.EntradaListar{Entidad: "orden", EntidadID: orden}))(t)
	vistos := 0
	for _, p := range pag.Data {
		if r, ok := p.Payload["representante"].(map[string]any); ok {
			vistos++
			if r["documento_tipo"] != "pasaporte" || r["nombre"] != "***" || r["mayor_de_edad"] != "***" {
				t.Fatalf("representante en el panel de pendientes: %+v", r)
			}
		}
	}
	if vistos != 1 {
		t.Fatalf("pendientes con representante: %d", vistos)
	}
	for _, consulta := range []string{
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE $1",
		"SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') LIKE $1 OR coalesce(despues::text, '') LIKE $1",
		"SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE $1",
	} {
		for _, v := range []string{"%Rosa Elvira%", "%RQ998877%"} {
			if n := contar(t, consulta, v); n != 0 {
				t.Fatalf("representante fuera del depósito (%s): %q", v, consulta)
			}
		}
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.cambiar_titular' AND entidad_id = $1
		AND entrada->'titular'->'representante'->>'documentoTipo' = 'pasaporte' AND entrada->'titular'->'representante'->>'nombre' = '***'`, orden); n == 0 {
		t.Fatal("la auditoría no muestra el representante enmascarado")
	}
	sinSensibles(t)

	// En una venta residencial, la empresa también vale: el plan va aparte (A40).
	o2 := e.instalacion(tec, ct, true)
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o2.ID, datosTitularEmpresa())), "aplicada")
}

// 0.14.1: titular_empresa_en_revision se trata igual que titular_no_coincide (rechazada, orden en
// revisión de un supervisor, la marca no se levanta sola y el técnico no ve el código); los
// errores representante_requerido, nit_requerido y representante_no_permitido dejan el paso
// rechazado con su código para rehacerlo.
func TestTitularEmpresaEnRevision(t *testing.T) {
	e := nuevoEntornoTitular(t)
	contratoArgos(t, "0.14.1")
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	conModo(t, o, "empresarial")
	orden := strconv.FormatInt(o.ID, 10)
	e.falso.rechazar("/cambiar-titular", "representante_requerido", "titular_empresa_en_revision")

	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosTitularEmpresa())), "aplicada")
	e.procesar()
	if p := e.pendiente("iris:titular:" + orden + ":1"); p.Estado != pendientes.EstadoFallidoNegocio || p.Codigo == nil || *p.Codigo != "representante_requerido" {
		t.Fatalf("representante_requerido: %+v", p)
	}
	est := deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if est.EnRevision || len(est.Pasos) != 1 || est.Pasos[0].Estado != "rechazado" || est.Pasos[0].Codigo == nil || *est.Pasos[0].Codigo != "representante_requerido" {
		t.Fatalf("rechazo para rehacer: %+v", est)
	}
	// Se rehace (reemplaza al rechazado) y Argos la deja en revisión.
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosTitularEmpresa())), "aplicada")
	e.procesar()
	if p := e.pendiente("iris:titular:" + orden + ":2"); p.Estado != pendientes.EstadoFallidoNegocio || p.Codigo == nil || *p.Codigo != "titular_empresa_en_revision" {
		t.Fatalf("titular_empresa_en_revision: %+v", p)
	}
	est = deber(e.m.Titular.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if !est.EnRevision {
		t.Fatalf("no quedó en revisión: %+v", est)
	}
	for _, p := range est.Pasos {
		if p.Codigo != nil && *p.Codigo == "titular_empresa_en_revision" {
			t.Fatalf("el técnico ve el código: %+v", p)
		}
	}
	// No se levanta sola: otra toma se rechaza hasta que un supervisor la levante.
	esperar(t, e.lote(ct, op(campo.OpCambiarTitular, o.ID, datosTitularEmpresa())), "rechazada:titular_en_revision")
}

// MEDIA 3 de #280: desde el panel, completar-datos no cambia el nombre ligado a la identidad.
func TestCompletarDatosPanelSinNombre(t *testing.T) {
	e := nuevoEntornoTitular(t)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacion(tec, ct, true)
	esperar(t, e.lote(ct, op(campo.OpRegistrarIdentidad, o.ID, datosIdentidad())), "aplicada")
	f := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	_, err := e.m.Titular.CompletarDatos.Ejecutar(e.cs, campo.EntradaCompletarDatos{ID: o.ID, Version: &f.Version,
		Cliente: &campo.ClienteCompletar{Nombre: ptr("Otra Persona Distinta")}})
	if !esProblema(err, "nombre_solo_en_sitio") {
		t.Fatalf("nombre desde el panel: %v", err)
	}
	// En sitio (lote), sí.
	esperar(t, e.lote(ct, op(campo.OpCompletarDatos, o.ID, map[string]any{"cliente": map[string]any{"nombre": "Ana María Pérez López"}})), "aplicada")
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
	pool := pruebas.Pool(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []int64{990, 777} {
		exec(`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
			VALUES ($1, 1, '{1}', 'Titular', $2, '5512 3344', 'Zona 10', now()) ON CONFLICT (id) DO NOTHING`, c, "C-"+strconv.FormatInt(c, 10))
	}
	exec(`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, sincronizado_en)
		VALUES (9901, 1, 990, 'Fibra 50', 'pendiente_instalacion', now()) ON CONFLICT (id) DO UPDATE SET cliente_id = 990`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = 9901")
		_, _ = pool.Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id IN (990, 777)")
	})
	o := e.instalacion(tec, ct, false)
	antes := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	in := campo.EntradaTitularArgos{ServicioID: 9901, DeClienteID: ptr(int64(990)), AClienteID: 777, EntranteID: 1, TipoTitular: ptr("empresa")}
	if _, err := e.m.MoverAlTitular.Ejecutar(e.cs, in); err == nil {
		t.Fatal("un usuario movió la orden")
	}
	// MEDIA 2 de #280: la instantánea todavía dice 990 (el aviso no está confirmado): nada se mueve.
	if r := deber(e.m.MoverAlTitular.Ejecutar(sistema(t), in))(t); len(r.Ordenes) != 0 {
		t.Fatalf("movida sin la instantánea: %+v", r)
	}
	exec("UPDATE integracion.argos_servicios SET cliente_id = 777 WHERE id = 9901")
	r := deber(e.m.MoverAlTitular.Ejecutar(sistema(t), in))(t)
	if !slices.Contains(r.Ordenes, o.ID) {
		t.Fatalf("movidas: %+v", r)
	}
	v := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if v.ClienteID == nil || *v.ClienteID != 777 || v.Version != antes.Version+1 {
		t.Fatalf("cliente %v, versión %d (antes %d)", v.ClienteID, v.Version, antes.Version)
	}
	// Con guardar: el feed del técnico y el diff en la auditoría.
	if n := contar(t, "SELECT count(*) FROM campo.orden_cambios WHERE orden_id = $1 AND tecnico_id = $2 AND tipo = 'actualizada'", o.ID, tec); n == 0 {
		t.Fatal("sin cambio en el feed del técnico")
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.cambios WHERE entidad = 'orden' AND entidad_id = $1 AND campo = 'cliente_id'", strconv.FormatInt(o.ID, 10)); n == 0 {
		t.Fatal("sin diff del cliente en la auditoría")
	}
	// Repetido: nada. Un aviso atrasado (de vuelta a 990): tampoco.
	if r := deber(e.m.MoverAlTitular.Ejecutar(sistema(t), in))(t); slices.Contains(r.Ordenes, o.ID) {
		t.Fatalf("repetido: %+v", r)
	}
	atrasado := campo.EntradaTitularArgos{ServicioID: 9901, DeClienteID: ptr(int64(1)), AClienteID: 990, EntranteID: 2}
	if r := deber(e.m.MoverAlTitular.Ejecutar(sistema(t), atrasado))(t); len(r.Ordenes) != 0 {
		t.Fatalf("el aviso atrasado devolvió la orden: %+v", r)
	}
}

// esProblema: el error es un problema con ese código.
func esProblema(err error, codigo string) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Codigo() == codigo
}
