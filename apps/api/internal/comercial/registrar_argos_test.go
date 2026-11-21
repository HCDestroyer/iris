package comercial_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// Prospectos registrados desde Argos (contrato 0.10.0 §3.6, A44 / B23), por HTTP: el token de la
// integración, la idempotencia de 30 días con X-Iris-Empresa y X-Iris-Actor en la huella, los
// números, los duplicados, los límites y la factibilidad.

type registroArgos struct {
	t     *testing.T
	e     *entorno
	base  string
	token string
}

func nuevoRegistroArgos(t *testing.T) *registroArgos {
	t.Helper()
	limpiarConector(t)
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM comercial.registros_argos", "DELETE FROM integracion.argos_clientes WHERE id IN (8801, 8804)"} {
			if _, err := pruebas.Pool(t).Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	e := nuevoEntorno(t, time.Now)
	e.conectar()
	almacen, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	com := comercial.Nuevo(comercial.Config{Planes: comercial.PlanesDeArgos(e.am), Altas: comercial.AltasDeArgos(e.am), Campo: e.cmp,
		Documentos: comercial.DocumentosDeAdjuntos(adjuntos.Nuevo(adjuntos.Config{Almacen: almacen})),
		Clientes:   e.am, URLBase: "https://iris.prueba/"})
	e.com = com
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pruebas.Pool(t), Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Idempotencia: nucleo.OpcionesIdempotencia{Regla: comercial.ReglaIdempotenciaArgos},
		Modulos:      []func(huma.API){com.Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	u, err := ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Argos", Correo: pruebas.Correo("argos"), Tipo: "integracion",
		Roles: []identidad.AsignacionRol{{Rol: "integracion_argos"}}, Empresas: []int64{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := ident.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{UsuarioID: u.ID, Nombre: "panel de Argos"})
	if err != nil {
		t.Fatal(err)
	}
	return &registroArgos{t: t, e: e, base: srv.URL, token: tk.Valor}
}

func (r *registroArgos) pedir(cuerpo any, cabeceras ...string) respuesta {
	r.t.Helper()
	c := &clienteHTTP{t: r.t, base: r.base, http: http.DefaultClient}
	base := []string{"Authorization", "Bearer " + r.token, "X-Iris-Empresa", "1", "X-Iris-Actor", "usuario:12",
		"Idempotency-Key", "argos:prospecto:" + pruebas.Unico()}
	return c.pedir(http.MethodPost, comercial.RutaRegistrarArgos, cuerpo, append(base, cabeceras...)...)
}

// numero: un número de Guatemala válido y distinto en cada llamada.
func numero() string {
	return fmt.Sprintf("+5025%07d", time.Now().UnixNano()%10_000_000)
}

func cuerpoArgos(numeros ...string) map[string]any {
	tels := []map[string]any{}
	for i, n := range numeros {
		t := map[string]any{"numero": n, "whatsapp": true, "principal": i == 0}
		if i == 1 {
			t["nombre"], t["relacion"] = "Jorge López", "familiar"
		}
		tels = append(tels, t)
	}
	return map[string]any{"origen": "oficina", "nombre": "Marta López", "telefonos": tels, "modo": "residencial",
		"direccion": "3a calle 4-50, zona 2, Mixco", "zona_id": 7001, "coordenadas": "14.6001,-90.5001", "plan_interes_id": 300,
		"comentario": "Pregunta por 100 Mbps", "cliente_id": nil}
}

func TestRegistrarProspectoDesdeArgos(t *testing.T) {
	r := nuevoRegistroArgos(t)
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	zona := int64(7001)
	vendedor, err := ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Vendedora Centro", Correo: pruebas.Correo("ventas"),
		Contrasena: contrasena, Roles: []identidad.AsignacionRol{{Rol: "ventas", ZonaID: &zona}}, Empresas: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}

	principal, familiar, suelto := numero(), numero(), numero()
	cuerpo := cuerpoArgos(principal, familiar, suelto)
	clave := "argos:prospecto:" + pruebas.Unico()
	res := r.pedir(cuerpo, "Idempotency-Key", clave)
	p := res.ok(t, http.StatusCreated)
	id := int64(p["prospecto_id"].(float64))
	if p["asignado"] != true || p["estado"] != "factibilidad" || p["url_iris"] != fmt.Sprintf("https://iris.prueba/comercial/%d", id) ||
		!strings.HasPrefix(p["numero"].(string), "PR-") {
		t.Fatalf("respuesta: %v", p)
	}
	var origen, telefono, modo, actor string
	var vend *int64
	var validado *bool
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT origen, telefono, modo, registrado_por_argos, vendedor_id, cliente_validado
		FROM comercial.prospectos WHERE id = $1`, id).Scan(&origen, &telefono, &modo, &actor, &vend, &validado); err != nil {
		t.Fatal(err)
	}
	if origen != "argos_panel" || modo != "residencial" || actor != "usuario:12" || vend == nil || *vend != vendedor.ID || validado != nil ||
		!strings.HasPrefix(telefono, "+502 5") {
		t.Fatalf("prospecto: %s %s %s %s %v %v", origen, telefono, modo, actor, vend, validado)
	}
	var tels, principales, conNombre int
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT count(*), count(*) FILTER (WHERE principal), count(nombre)
		FROM comercial.prospecto_telefonos WHERE prospecto_id = $1 AND empresa_id = 1`, id).Scan(&tels, &principales, &conNombre); err != nil {
		t.Fatal(err)
	}
	if tels != 3 || principales != 1 || conNombre != 1 {
		t.Fatalf("teléfonos: %d %d %d", tels, principales, conNombre)
	}
	// La factibilidad (router 51 a unos metros) y el evento de estado con el origen para Argos.
	var resultado string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT resultado FROM comercial.factibilidades WHERE prospecto_id = $1`, id).Scan(&resultado); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT payload FROM integracion.outbox WHERE evento = 'prospecto.estado_cambiado'
		AND (payload->>'prospecto_id')::bigint = $1`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	_ = json.Unmarshal(payload, &ev)
	if resultado != "viable" || ev["origen"] != "argos_panel" || ev["empresa_id"] != float64(1) || ev["a"] != "factibilidad" {
		t.Fatalf("factibilidad %s, evento %v", resultado, ev)
	}
	// La auditoría no guarda los números completos.
	var entrada string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT entrada::text FROM auditoria.acciones WHERE accion = 'prospectos.registrar_integracion'
		AND entidad_id = $1`, fmt.Sprint(id)).Scan(&entrada); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entrada, principal) || strings.Contains(entrada, familiar[1:]) {
		t.Fatalf("la auditoría guarda los números: %s", entrada)
	}

	// Misma clave y misma huella: la respuesta original. Otra huella (otro actor): 422.
	otra := r.pedir(cuerpo, "Idempotency-Key", clave)
	if otra.ok(t, http.StatusCreated)["prospecto_id"] != p["prospecto_id"] || otra.h.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("repetición: %v %v", otra.cuerpo, otra.h)
	}
	r.pedir(cuerpo, "Idempotency-Key", clave, "X-Iris-Actor", "usuario:13").problema(t, http.StatusUnprocessableEntity, "idempotency_key_reutilizada")
	if n := contar(t, `SELECT count(*) FROM nucleo.idempotencia WHERE clave = $1 AND expira_en > now() + interval '29 days'`, clave); n != 1 {
		t.Fatal("la clave de §3.6 se guarda 30 días")
	}

	// Cualquiera de los números en un prospecto abierto: 409 con url_iris, sin decir cuál ni el id;
	// el pedido queda como actividad del existente.
	dup := r.pedir(cuerpoArgos(numero(), suelto))
	dup.problema(t, http.StatusConflict, "prospecto_duplicado")
	if dup.cuerpo["url_iris"] != p["url_iris"] || strings.Contains(string(dup.crudo), suelto) || dup.cuerpo["prospecto_id"] != nil {
		t.Fatalf("duplicado: %s", dup.crudo)
	}
	var actividades int
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT count(*) FROM comercial.actividades WHERE prospecto_id = $1
		AND descripcion LIKE 'Nuevo pedido registrado desde Argos (oficina)%'`, id).Scan(&actividades); err != nil {
		t.Fatal(err)
	}
	if actividades != 1 {
		t.Fatalf("actividades: %d", actividades)
	}
}

func TestRegistrarProspectoArgosValidaciones(t *testing.T) {
	r := nuevoRegistroArgos(t)
	n := numero()

	// Números: dos principales, titular fuera del principal, número inválido; sin repetir el número.
	mal := cuerpoArgos(n, numero(), "5512")
	tels := mal["telefonos"].([]map[string]any)
	tels[1]["relacion"] = "titular"
	tels[2]["principal"] = true
	res := r.pedir(mal)
	res.problema(t, http.StatusUnprocessableEntity, "telefonos_invalidos")
	if strings.Contains(string(res.crudo), n) || !strings.Contains(string(res.crudo), "telefonos[2].numero") ||
		!strings.Contains(string(res.crudo), "telefonos[1].relacion") || !strings.Contains(string(res.crudo), "Exactamente un número principal") {
		t.Fatalf("errores: %s", res.crudo)
	}
	// Plan que no es de los activos del modo.
	plan := cuerpoArgos(numero())
	plan["plan_interes_id"] = 999
	r.pedir(plan).problema(t, http.StatusUnprocessableEntity, "plan_no_permitido")
	// Cliente que Argos no tiene: 422; uno de la instantánea de la empresa: validado.
	cli := cuerpoArgos(numero())
	cli["cliente_id"] = 8801
	r.pedir(cli).problema(t, http.StatusUnprocessableEntity, "cliente_no_valido")
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en)
		VALUES (8801, 1, 'Cliente de Argos', now())`); err != nil {
		t.Fatal(err)
	}
	ok := r.pedir(cli).ok(t, http.StatusCreated)
	var validado bool
	var existente int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT cliente_validado, cliente_existente_id FROM comercial.prospectos WHERE id = $1`,
		int64(ok["prospecto_id"].(float64))).Scan(&validado, &existente); err != nil {
		t.Fatal(err)
	}
	if !validado || existente != 8801 {
		t.Fatalf("cliente: %v %d", validado, existente)
	}
	// La instantánea lo tiene en otra empresa: 422 (no se pregunta a Argos).
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en)
		VALUES (8804, 2, 'Cliente de otra empresa', now())`); err != nil {
		t.Fatal(err)
	}
	otra := cuerpoArgos(numero())
	otra["cliente_id"] = 8804
	r.pedir(otra).problema(t, http.StatusUnprocessableEntity, "cliente_no_valido")
	// Argos rechaza la lectura (403): 422, nunca «sin validar».
	rechazo := cuerpoArgos(numero())
	rechazo["cliente_id"] = 8803
	r.pedir(rechazo).problema(t, http.StatusUnprocessableEntity, "cliente_no_valido")
	// Argos no disponible: se crea con cliente_validado false.
	r.e.falso.fijar(func(a *argosFalso) { a.lecturaCaida = true })
	caido := cuerpoArgos(numero())
	caido["cliente_id"] = 8802
	sinValidar := int64(r.pedir(caido).ok(t, http.StatusCreated)["prospecto_id"].(float64))
	r.e.falso.fijar(func(a *argosFalso) { a.lecturaCaida = false })
	if n := contar(t, `SELECT count(*) FROM comercial.prospectos WHERE id = $1 AND cliente_validado = false`, sinValidar); n != 1 {
		t.Fatal("con Argos caído el cliente queda sin validar")
	}
	// Cuerpo inválido (NUL): 422 antes de abrir una transacción.
	nul := cuerpoArgos(numero())
	nul["nombre"] = "Marta\u0000López"
	r.pedir(nul).problema(t, http.StatusUnprocessableEntity, "validacion")
	// Cabeceras: actor mal formado, empresa fuera del alcance del token, sin empresa.
	r.pedir(cuerpoArgos(numero()), "X-Iris-Actor", "asistente_ia").problema(t, http.StatusBadRequest, "cabecera_iris_requerida")
	r.pedir(cuerpoArgos(numero()), "X-Iris-Empresa", "2").problema(t, http.StatusForbidden, "empresa_no_permitida")
	r.pedir(cuerpoArgos(numero()), "X-Iris-Empresa", "").problema(t, http.StatusUnprocessableEntity, "validacion")

	// Una persona (aunque tenga el permiso) no usa la ruta.
	admin := entrar(t, r.base, "admin")
	admin.pedir(http.MethodPost, comercial.RutaRegistrarArgos, cuerpoArgos(numero()), "X-Iris-Empresa", "1", "X-Iris-Actor", "usuario:1",
		"Idempotency-Key", "argos:prospecto:"+pruebas.Unico()).problema(t, http.StatusForbidden, "solo_integracion_argos")

	// Conector apagado: 409 argos_apagado, que no es el resultado del registro: libera la clave y la
	// misma petición, con el conector encendido, se ejecuta. La auditoría del rechazo, enmascarada.
	r.e.apagar()
	n = numero()
	clave := "argos:prospecto:" + pruebas.Unico()
	r.pedir(cuerpoArgos(n), "Idempotency-Key", clave).problema(t, http.StatusConflict, "argos_apagado")
	var entrada string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT entrada::text FROM auditoria.acciones
		WHERE accion = 'prospectos.registrar_integracion' AND error::text LIKE '%argos_apagado%' ORDER BY id DESC LIMIT 1`).Scan(&entrada); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entrada, n) || strings.Contains(entrada, "Marta") || strings.Contains(entrada, "3a calle") {
		t.Fatalf("la auditoría del rechazo guarda datos personales: %s", entrada)
	}
	r.e.encender()
	if res := r.pedir(cuerpoArgos(n), "Idempotency-Key", clave); res.status != http.StatusCreated || res.h.Get("Idempotent-Replayed") != "" {
		t.Fatalf("tras argos_apagado la misma clave se ejecuta: %d %s", res.status, res.crudo)
	}
}

func TestRegistrarProspectoArgosLimites(t *testing.T) {
	r := nuevoRegistroArgos(t)
	cinco := func() map[string]any {
		c := cuerpoArgos(numero(), numero(), numero(), numero(), numero())
		delete(c, "zona_id")
		delete(c, "coordenadas")
		return c
	}
	// 30 números por minuto por actor: 6 registros de 5 pasan; el 7.º, 429 con Retry-After.
	for i := range 6 {
		if res := r.pedir(cinco()); res.status != http.StatusCreated {
			t.Fatalf("registro %d: %d %s", i, res.status, res.crudo)
		}
	}
	res := r.pedir(cinco())
	res.problema(t, http.StatusTooManyRequests, "demasiados_registros")
	if res.h.Get("Retry-After") == "" {
		t.Fatal("sin Retry-After")
	}
	// Otro actor de la misma empresa todavía puede.
	r.pedir(cinco(), "X-Iris-Actor", "usuario:99").ok(t, http.StatusCreated)

	// Los números cuentan aunque el registro falle después de contarlos (aquí, una zona que no
	// existe: 422 dentro de la transacción del registro).
	malo := func() map[string]any {
		c := cinco()
		c["zona_id"] = 999999
		return c
	}
	for i := range 6 {
		if res := r.pedir(malo(), "X-Iris-Actor", "usuario:77"); res.status != http.StatusUnprocessableEntity {
			t.Fatalf("registro fallido %d: %d %s", i, res.status, res.crudo)
		}
	}
	r.pedir(cinco(), "X-Iris-Actor", "usuario:77").problema(t, http.StatusTooManyRequests, "demasiados_registros")
}

// Al ganar (§3.6): con un cliente nuevo, el principal es su teléfono, los números sin nombre van en
// telefonos_adicionales del mismo alta y los que tienen nombre, como contactos (A42); con el
// cliente que eligió Argos, Iris no lo crea: el servicio sale con su id. Solo con Argos ≥ 0.10.0.
func TestGanarProspectoDesdeArgos(t *testing.T) {
	r := nuevoRegistroArgos(t)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE integracion.conectores SET contrato_remoto = '0.10.0' WHERE sistema = 'argosisp'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en)
		VALUES (8801, 1, 'Cliente de Argos', now())`); err != nil {
		t.Fatal(err)
	}
	v := usuarioCon(t, rol("ventas"))
	asignarEmpresa(t, v, 1)
	ctx := enEmpresa(t, v, 1, "ventas")
	lineas := []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(300))}}

	principal, familiar, suelto := numero(), numero(), numero()
	id := int64(r.pedir(cuerpoArgos(principal, familiar, suelto)).ok(t, http.StatusCreated)["prospecto_id"].(float64))
	d := hastaContrato(t, r.e.com, ctx, id, lineas, "")
	g := deber(r.e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: id, Version: d.Version}))(t)
	if g.Alta == nil || g.Alta.Estado != comercial.AltaEsperandoCliente {
		t.Fatalf("alta: %+v", g.Alta)
	}
	r.e.procesar()
	r.e.procesar()
	cli := r.e.falso.cuerpo(comercial.ClaveCliente(id))
	adic, _ := cli["telefonos_adicionales"].([]any)
	if cli == nil || len(adic) != 1 || adic[0].(map[string]any)["numero"] != suelto || !strings.HasPrefix(fmt.Sprint(cli["telefono"]), "+502 5") {
		t.Fatalf("POST /clientes: %v", cli)
	}
	var clienteID, telID int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT p.cliente_argos_id, t.id FROM comercial.prospectos p
		JOIN comercial.prospecto_telefonos t ON t.prospecto_id = p.id AND t.nombre IS NOT NULL WHERE p.id = $1`, id).Scan(&clienteID, &telID); err != nil {
		t.Fatal(err)
	}
	r.e.procesar()
	ct := r.e.falso.cuerpo(comercial.ClaveContacto(clienteID, telID))
	tels, _ := ct["telefonos"].([]any)
	if ct == nil || ct["nombre"] != "Jorge López" || ct["relacion"] != "familiar" || len(tels) != 1 || tels[0].(map[string]any)["numero"] != familiar {
		t.Fatalf("contacto: %v", ct)
	}
	if principal == "" {
		t.Fatal("principal")
	}

	// Con el cliente que eligió Argos: sin POST /clientes; el servicio con su id.
	c := cuerpoArgos(numero(), numero())
	c["cliente_id"] = 8801
	id2 := int64(r.pedir(c).ok(t, http.StatusCreated)["prospecto_id"].(float64))
	d = hastaContrato(t, r.e.com, ctx, id2, lineas, "")
	g = deber(r.e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: id2, Version: d.Version}))(t)
	if g.Alta == nil || g.Alta.Estado != comercial.AltaEsperandoServicio || g.Alta.ClienteArgosID == nil || *g.Alta.ClienteArgosID != 8801 {
		t.Fatalf("alta con cliente existente: %+v", g.Alta)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1`, comercial.ClaveCliente(id2)); n != 0 {
		t.Fatal("no se crea el cliente que eligió Argos")
	}
	if n := contar(t, `SELECT count(*) FROM integracion.pendientes WHERE idempotency_key LIKE 'iris:contacto:8801:pt%'`); n != 1 {
		t.Fatalf("contactos del cliente existente: %d", n)
	}
	r.e.procesar()
	r.e.procesar()
	srv := r.e.falso.cuerpo(comercial.ClaveServicio(id2))
	if srv == nil || srv["cliente_id"] != float64(8801) {
		t.Fatalf("POST /servicios: %v", srv)
	}
	// El contacto, solo para el servicio nuevo, con X-Iris-Actor de quien ganó y sin permisos por
	// encima de los suyos (ventas: estado de cuenta sí; tickets y boletas no).
	var clave2 string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT idempotency_key FROM integracion.pendientes
		WHERE idempotency_key LIKE 'iris:contacto:8801:pt%'`).Scan(&clave2); err != nil {
		t.Fatal(err)
	}
	ct2 := r.e.falso.cuerpo(clave2)
	servs, _ := ct2["servicios"].([]any)
	perm, _ := ct2["permisos"].(map[string]any)
	idSrv := r.e.falso.altas[comercial.ClaveServicio(id2)]
	if len(servs) != 1 || servs[0] != float64(idSrv) || perm["consultar"] != true || perm["reportar"] != false || perm["pagar"] != false {
		t.Fatalf("contacto del cliente existente: %v", ct2)
	}
	r.e.falso.fijar(func(a *argosFalso) {
		if a.actores[clave2] != fmt.Sprintf("usuario:%d", v) {
			t.Errorf("X-Iris-Actor del contacto: %q", a.actores[clave2])
		}
	})
	if o := contar(t, `SELECT count(*) FROM comercial.prospectos WHERE id = $1 AND alta_estado = 'completa' AND orden_id IS NOT NULL`, id2); o != 1 {
		t.Fatal("el alta con el cliente existente crea la orden")
	}
}

// Un cliente elegido en Argos que quedó sin validar (Argos caído al registrarlo) se confirma al
// ganar: de otra empresa, 422 cliente_no_valido; de la empresa, el alta sale; sin respuesta de
// Argos, el alta no sale (422 cliente_sin_confirmar), tampoco por el caso de uso directo.
func TestGanarClienteSinValidar(t *testing.T) {
	r := nuevoRegistroArgos(t)
	v := usuarioCon(t, rol("ventas"))
	asignarEmpresa(t, v, 1)
	ctx := enEmpresa(t, v, 1, "ventas")
	lineas := []comercial.EntradaLinea{{Tipo: "plan", PlanID: ptr(int64(300))}}
	r.e.falso.fijar(func(a *argosFalso) {
		a.porID["8802"] = map[string]any{"id": 8802, "nombre": "Cliente de Argos"}
		a.serviciosDe["8802"] = `{"id":7702,"empresa_id":1,"cliente_id":8802}`
	})
	sinValidar := func() (int64, int32) {
		t.Helper()
		c := cuerpoArgos(numero())
		c["cliente_id"] = 8802
		id := int64(r.pedir(c).ok(t, http.StatusCreated)["prospecto_id"].(float64))
		// Como si Argos no hubiera respondido al registrarlo.
		if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE comercial.prospectos SET cliente_validado = false WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return id, hastaContrato(t, r.e.com, ctx, id, lineas, "").Version
	}

	id, ver := sinValidar()
	r.e.falso.fijar(func(a *argosFalso) { a.serviciosDe["8802"] = `{"id":7702,"empresa_id":2,"cliente_id":8802}` })
	_, err := r.e.com.GanarProspecto(ctx, comercial.EntradaGanar{ID: id, Version: ver})
	problema(t, err, http.StatusUnprocessableEntity, "cliente_no_valido")
	r.e.falso.fijar(func(a *argosFalso) { a.serviciosDe["8802"] = `{"id":7702,"empresa_id":1,"cliente_id":8802}` })
	g := deber(r.e.com.GanarProspecto(ctx, comercial.EntradaGanar{ID: id, Version: ver}))(t)
	if g.Alta == nil || g.Alta.ClienteArgosID == nil || *g.Alta.ClienteArgosID != 8802 || g.ClienteValidado == nil || !*g.ClienteValidado {
		t.Fatalf("alta: %+v", g)
	}

	id2, ver2 := sinValidar()
	r.e.falso.fijar(func(a *argosFalso) { a.lecturaCaida = true })
	_, err = r.e.com.GanarProspecto(ctx, comercial.EntradaGanar{ID: id2, Version: ver2})
	problema(t, err, http.StatusUnprocessableEntity, "cliente_sin_confirmar")
	_, err = r.e.com.Ganar.Ejecutar(ctx, comercial.EntradaGanar{ID: id2, Version: ver2})
	problema(t, err, http.StatusUnprocessableEntity, "cliente_sin_confirmar")
}
