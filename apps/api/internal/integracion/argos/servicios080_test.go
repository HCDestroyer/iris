package argos_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato argosisp.md 0.8.0 §2.4 (bloque C, B6): lista blanca de la instantánea, precio y
// permisos servicios.ver_red / servicios.ver_ubicacion filtrados en el servidor.

// sembrar080 siembra un cliente con dos servicios con la forma de Argos 1.18.0, más los campos
// que Argos manda y que Iris NO debe guardar ni exponer (lista blanca).
func (e *entorno) sembrar080(cliente int64) (conPrecio, sinPrecio int64) {
	conPrecio, sinPrecio = cliente*10, cliente*10+1
	e.argos.con(func() {
		e.argos.clientes[cliente] = map[string]any{"id": cliente, "codigo": "C-080", "nombre": "María Pérez", "direccion": "Zona 1", "zona_id": 3}
		e.argos.servicios[cliente] = []map[string]any{
			{
				"id": conPrecio, "cliente_id": cliente, "empresa_id": int64(1), "plan_id": int64(12), "plan_nombre": "Chitá Fibra 50 Mbps",
				"zona_nombre": "Chitá", "router_id": int64(5), "router_nombre": "Nodo Centro", "estado": "activo",
				"tipo_conexion": "pppoe", "user_ppp": "cli1201-4410", "ipv4": "100.64.12.34",
				"prefijo_enlace_v6": "2001:db8:10::/64", "prefijo_delegado_v6": nil,
				"direccion": "3a calle 4-56 zona 1", "referencia": "Portón negro", "coordenadas": "14.634900,-90.506900",
				"precio_mensual": "250.00", "moneda": "GTQ",
				"planta": map[string]any{"caja": "C-014", "puerto": "3", "olt": nil, "slot": nil, "pon": nil, "potencia_dbm": "-19.40"},
				// Fuera de la lista blanca: nunca se guardan ni salen.
				"mac": "AA:BB:CC:DD:EE:FF", "costo_override": "199.99", "ipv6_duid": "DUID-SECRETO", "ipv6_modo": "slaac",
				"local_address": "10.255.0.1", "servidor_pppoe_id": 77, "excepciones": []any{"EXCEPCION-SECRETA"},
				"suspension_manual_motivo": "MOTIVO-SECRETO", "externo": map[string]any{"x": "EXTERNO-SECRETO"},
			},
			{
				// Como un Argos anterior a 1.18.0: sin precio_mensual ni moneda.
				"id": sinPrecio, "cliente_id": cliente, "empresa_id": int64(1), "plan_id": int64(13), "plan_nombre": "Chitá Fibra 100 Mbps",
				"zona_nombre": "Chitá", "router_id": int64(5), "router_nombre": "Nodo Centro", "estado": "suspendido",
				"tipo_conexion": "ip_estatica", "ipv4": "100.64.12.90", "direccion": "Lote 12, colonia Las Flores",
			},
		}
	})
	return conPrecio, sinPrecio
}

// usuarioConID es usuarioEn (empresa 1) devolviendo también el id del usuario.
func (e *entorno) usuarioConID(roles ...string) (*clienteHTTP, int64) {
	e.t.Helper()
	correo := pruebas.Correo("u080")
	asign := []identidad.AsignacionRol{}
	for _, r := range roles {
		asign = append(asign, identidad.AsignacionRol{Rol: r})
	}
	ctx := pruebas.ContextoSistema(e.pool)
	u, err := e.ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Usuario " + correo, Correo: correo, Contrasena: contrasena, Roles: asign})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.ident.AsignarEmpresaUsuario.Ejecutar(ctx, identidad.EntradaEmpresaUsuario{UsuarioID: u.ID, EmpresaID: 1}); err != nil {
		e.t.Fatal(err)
	}
	c := e.anonimo()
	r := c.pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correo, "contrasena": contrasena})
	if r.status != 200 {
		e.t.Fatalf("login: %d %s", r.status, r.crudo)
	}
	c.csrf, _ = r.cuerpo["csrfToken"].(string)
	return c, u.ID
}

func serviciosDeFicha(t *testing.T, r respuesta) map[float64]map[string]any {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("ficha: %d %s", r.status, r.crudo)
	}
	salida := map[float64]map[string]any{}
	for _, s := range r.cuerpo["servicios"].([]any) {
		m := mapa(t, s)
		salida[m["id"].(float64)] = m
	}
	return salida
}

var (
	camposRed       = []string{"routerId", "routerNombre", "ip", "prefijoEnlaceV6", "prefijoDelegadoV6", "userPpp", "tipoConexion", "planta"}
	camposUbicacion = []string{"direccion", "lat", "lon", "referencia", "contactoNombre", "contactoTelefono", "contactoTelefonos"}
	fueraDeLista    = []string{"AA:BB:CC:DD:EE:FF", "199.99", "DUID-SECRETO", "10.255.0.1", "EXCEPCION-SECRETA", "MOTIVO-SECRETO", "EXTERNO-SECRETO", "costoOverride", "costo_override", "\"mac\""}
)

func sinCampos(t *testing.T, rol string, s map[string]any, campos []string) {
	t.Helper()
	for _, c := range campos {
		if _, ok := s[c]; ok {
			t.Errorf("%s: el servicio %v no debía traer %q", rol, s["id"], c)
		}
	}
}

func TestServicios080ListaBlancaPrecioYPermisos(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	con, sin := e.sembrar080(900)

	// soporte ve todo: precio, red y ubicación; numerados por id.
	soporte := e.usuario("soporte")
	r := soporte.pedir(http.MethodGet, "/api/v1/clientes/900", nil)
	ss := serviciosDeFicha(t, r)
	s1, s2 := ss[float64(con)], ss[float64(sin)]
	if s1["precioMensual"] != "250.00" || s1["moneda"] != "GTQ" || s1["ip"] != "100.64.12.34" || s1["routerNombre"] != "Nodo Centro" ||
		s1["prefijoEnlaceV6"] != "2001:db8:10::/64" || s1["userPpp"] != "cli1201-4410" || s1["direccion"] != "3a calle 4-56 zona 1" ||
		s1["referencia"] != "Portón negro" || s1["numero"] != float64(1) || s1["zonaNombre"] != "Chitá" {
		t.Fatalf("servicio con precio (soporte): %v", s1)
	}
	if p := mapa(t, s1["planta"]); p["caja"] != "C-014" || p["puerto"] != "3" || p["potenciaDbm"] != "-19.40" || p["olt"] != nil {
		t.Fatalf("planta: %v", p)
	}
	// Argos anterior a 1.18.0: sin precio no se inventa nada (ni «0»).
	if _, ok := s2["precioMensual"]; ok || s2["numero"] != float64(2) {
		t.Fatalf("servicio sin precio: %v", s2)
	}
	if _, ok := s2["moneda"]; ok {
		t.Fatalf("sin precio tampoco va la moneda: %v", s2)
	}
	// Lista blanca: nada de lo que Argos manda de más sale por la API ni queda en la instantánea.
	for _, x := range fueraDeLista {
		if strings.Contains(string(r.crudo), x) {
			t.Fatalf("la ficha trae %q, fuera de la lista blanca: %s", x, r.crudo)
		}
	}
	fila := e.consultarTexto(`SELECT row_to_json(s)::text FROM integracion.argos_servicios s WHERE id = $1`, con)
	for _, x := range fueraDeLista[:7] {
		if strings.Contains(fila, x) {
			t.Fatalf("la instantánea guardó %q: %s", x, fila)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = $1 AND precio_mensual = 250.00 AND moneda = 'GTQ'
		AND planta->>'potenciaDbm' = '-19.40'`, con); n != 1 {
		t.Fatal("la instantánea no guardó el precio ni la planta")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = $1 AND precio_mensual IS NULL AND moneda IS NULL`, sin); n != 1 {
		t.Fatal("sin precio en Argos, la instantánea debía quedar sin precio")
	}
	// Auditoría: qué servicios mostraron red y ubicación, nunca los valores.
	entrada := e.consultarTexto(`SELECT entrada::text FROM auditoria.acciones WHERE accion = 'clientes.ver' AND entidad_id = '900' ORDER BY id DESC LIMIT 1`)
	if !strings.Contains(entrada, fmt.Sprintf(`"servicios_ver_red": [%d, %d]`, con, sin)) || !strings.Contains(entrada, `"servicios_ver_ubicacion"`) {
		t.Fatalf("auditoría de la lectura: %s", entrada)
	}
	for _, x := range []string{"100.64.12.34", "3a calle", "cli1201", "14.6349"} {
		if strings.Contains(entrada, x) {
			t.Fatalf("la auditoría guardó un valor sensible %q: %s", x, entrada)
		}
	}

	// Por rol: lo que el servidor omite (§10.2 puntos 4 y 5).
	casos := []struct {
		rol            string
		red, ubicacion bool
	}{
		{"supervisor", true, true}, {"soporte", true, true}, {"despacho", false, true}, {"ventas", false, true},
		{"caja", false, false}, {"lectura", false, false},
	}
	for _, c := range casos {
		u := e.usuario(c.rol)
		for _, s := range serviciosDeFicha(t, u.pedir(http.MethodGet, "/api/v1/clientes/900", nil)) {
			if s["planNombre"] == "" || s["zonaNombre"] != "Chitá" || s["estado"] == "" {
				t.Errorf("%s: plan, zona y estado los ve cualquiera con clientes.ver: %v", c.rol, s)
			}
			if s["id"] == float64(con) && s["precioMensual"] != "250.00" {
				t.Errorf("%s: el precio lo ve cualquiera con clientes.ver: %v", c.rol, s)
			}
			if !c.red {
				sinCampos(t, c.rol, s, camposRed)
			} else if s["ip"] == nil {
				t.Errorf("%s: debía ver la IP: %v", c.rol, s)
			}
			if !c.ubicacion {
				sinCampos(t, c.rol, s, camposUbicacion)
			} else if s["direccion"] == nil {
				t.Errorf("%s: debía ver la dirección: %v", c.rol, s)
			}
		}
		// La búsqueda con servicios recorta igual.
		b := u.pedir(http.MethodGet, "/api/v1/clientes?q=mar&servicios=true", nil)
		if b.status != 200 {
			t.Fatalf("%s: buscar: %d %s", c.rol, b.status, b.crudo)
		}
		if !c.red && strings.Contains(string(b.crudo), "100.64.12") {
			t.Errorf("%s: la búsqueda trae la IP: %s", c.rol, b.crudo)
		}
		if !c.ubicacion && strings.Contains(string(b.crudo), "3a calle") {
			t.Errorf("%s: la búsqueda trae la dirección: %s", c.rol, b.crudo)
		}
	}

	// El técnico: red y ubicación solo de los servicios de sus órdenes asignadas.
	tecnico, tecnicoID := e.usuarioConID("tecnico")
	for _, s := range serviciosDeFicha(t, tecnico.pedir(http.MethodGet, "/api/v1/clientes/900", nil)) {
		sinCampos(t, "tecnico sin órdenes", s, append(append([]string{}, camposRed...), camposUbicacion...))
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, cliente_id, servicio_id, tecnico_id)
		SELECT id, 'Zona 1', 1, 900, $1, $2 FROM campo.tipos_orden WHERE codigo = 'visita'`, con, tecnicoID); err != nil {
		t.Fatal(err)
	}
	ss = serviciosDeFicha(t, tecnico.pedir(http.MethodGet, "/api/v1/clientes/900", nil))
	if ss[float64(con)]["ip"] != "100.64.12.34" || ss[float64(con)]["direccion"] != "3a calle 4-56 zona 1" {
		t.Fatalf("el técnico debía ver la red y la ubicación del servicio de su orden: %v", ss[float64(con)])
	}
	sinCampos(t, "tecnico, servicio ajeno", ss[float64(sin)], append(append([]string{}, camposRed...), camposUbicacion...))
}

// catalogo.cambiado de un plan con `precio` relee GET /servicios?plan_id= y refresca el precio;
// servicio.config_cambiada con `precio` relee la ficha.
func TestServicios080RefrescoDePrecio(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	con, sin := e.sembrar080(901)
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/901", nil); r.status != 200 {
		t.Fatalf("ficha: %d %s", r.status, r.crudo)
	}
	e.argos.con(func() {
		e.argos.planes = []map[string]any{{"id": int64(12), "nombre": "Chitá Fibra 50 Mbps", "precio": "275.00"}}
		e.argos.servicios[901][0]["precio_mensual"] = "275.00"
	})
	antes := e.argos.llamadasA("/api/v1/servicios")
	// Dos avisos seguidos del mismo plan: un solo job pendiente (único por plan); el webhook no lee la lista.
	for range 2 {
		r := e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "plan", "id": 12, "empresa_id": nil, "campos": []string{"precio"}})
		if r.Resultado != "aplicado" {
			t.Fatalf("catalogo.cambiado: %+v", r)
		}
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_precios_plan' AND (args->>'plan_id')::bigint = 12
		AND (args->>'conector_id')::bigint = $1 AND state = 'available'`, id); n != 1 {
		t.Fatalf("job de precios del plan: %d (se esperaba uno, único por plan)", n)
	}
	if e.argos.llamadasA("/api/v1/servicios") != antes {
		t.Fatal("el webhook no debía leer la lista de servicios del plan")
	}
	ctx := pruebas.ContextoSistema(e.pool)
	res, err := e.mod.RefrescarPreciosPlan.Ejecutar(ctx, argos.ArgsPreciosPlan{ConectorID: id, PlanID: 12})
	if err != nil || res.Cambiados != 1 {
		t.Fatalf("refrescar precios del plan: %+v %v", res, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = $1 AND precio_mensual = 275.00`, con); n != 1 {
		t.Fatal("no se refrescó el precio del servicio del plan")
	}
	if e.argos.llamadasA("/api/v1/servicios") == antes {
		t.Fatal("debía releer los servicios del plan en Argos")
	}
	// Un precio que no es un monto (NaN, infinito, negativo, fuera de numeric(12,2)) queda sin
	// precio y no rompe la ficha.
	for _, malo := range []string{"NaN", "Inf", "-5.00", "99999999999.00"} {
		e.argos.con(func() { e.argos.servicios[901][0]["precio_mensual"] = malo })
		res, err := e.mod.RefrescarPreciosPlan.Ejecutar(ctx, argos.ArgsPreciosPlan{ConectorID: id, PlanID: 12})
		if err != nil || res.Invalidos != 1 {
			t.Fatalf("precio %q: %+v %v", malo, res, err)
		}
		if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = $1 AND precio_mensual IS NULL AND moneda IS NULL`, con); n != 1 {
			t.Fatalf("precio %q: debía quedar sin precio", malo)
		}
		f := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/901?refrescar=true", nil)
		if f.status != 200 {
			t.Fatalf("precio %q: la ficha falló: %d %s", malo, f.status, f.crudo)
		}
		if _, ok := serviciosDeFicha(t, f)[float64(con)]["precioMensual"]; ok {
			t.Fatalf("precio %q: la ficha no debía traer precio", malo)
		}
		e.argos.con(func() { e.argos.servicios[901][0]["precio_mensual"] = "275.00" })
	}

	// Argos actualizado: el servicio sin precio ahora lo trae (servicio.config_cambiada con precio).
	e.argos.con(func() {
		e.argos.servicios[901][1]["precio_mensual"] = "350.00"
		e.argos.servicios[901][1]["moneda"] = "GTQ"
	})
	r := e.procesarWebhook(id, secreto, "servicio.config_cambiada", map[string]any{"servicio_id": sin, "empresa_id": 1, "cliente_id": 901, "campos": []string{"precio"}})
	if r.Resultado != "aplicado" {
		t.Fatalf("config_cambiada: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = $1 AND precio_mensual = 350.00 AND moneda = 'GTQ'`, sin); n != 1 {
		t.Fatal("servicio.config_cambiada con precio no refrescó el precio")
	}
}

// argos.ServiciosVisibles es lo que usan la ficha de la conversación y otros módulos: el mismo recorte
// por permisos, también para quien solo ve sus órdenes.
func TestServicios080VisiblesParaOtrosModulos(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	con, sin := e.sembrar080(902)
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/902", nil); r.status != 200 {
		t.Fatalf("ficha: %d %s", r.status, r.crudo)
	}
	leer := func(ctx context.Context) map[int64]argos.Servicio {
		t.Helper()
		ss, err := argos.ServiciosVisiblesDeCliente(ctx, e.pool, 902, 1)
		if err != nil {
			t.Fatal(err)
		}
		salida := map[int64]argos.Servicio{}
		for _, s := range ss {
			salida[s.ID] = s
		}
		return salida
	}
	todo := leer(pruebas.ContextoUsuario(e.pool, 1, nucleo.PermClientesVer, nucleo.PermServiciosVerRed, nucleo.PermServiciosVerUbicacion))
	if s := todo[con]; s.IP != "100.64.12.34" || s.Direccion == "" || s.Numero != 1 || s.PrecioMensual == nil || *s.PrecioMensual != "250.00" {
		t.Fatalf("con los dos permisos: %+v", s)
	}
	nada := leer(pruebas.ContextoUsuario(e.pool, 1, nucleo.PermClientesVer))
	for _, s := range nada {
		if s.IP != "" || s.RouterNombre != "" || s.UserPPP != "" || s.Planta != nil || s.Direccion != "" || s.Lat != nil || s.Referencia != "" {
			t.Fatalf("sin permisos: %+v", s)
		}
	}
	if s := nada[con]; s.PrecioMensual == nil || s.PlanNombre == "" || s.ZonaNombre != "Chitá" {
		t.Fatalf("plan, zona y precio los ve cualquiera: %+v", s)
	}
	// Quien solo ve sus órdenes: solo los servicios de sus órdenes asignadas.
	_, tecnico := e.usuarioConID("tecnico")
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, cliente_id, servicio_id, tecnico_id)
		SELECT id, 'Zona 1', 1, 902, $1, $2 FROM campo.tipos_orden WHERE codigo = 'visita'`, sin, tecnico); err != nil {
		t.Fatal(err)
	}
	propias := leer(pruebas.ContextoUsuario(e.pool, tecnico, nucleo.PermClientesVer, nucleo.PermOrdenesVerPropias, nucleo.PermServiciosVerRed, nucleo.PermServiciosVerUbicacion))
	if propias[sin].IP != "100.64.12.90" || propias[sin].Direccion == "" || propias[con].IP != "" || propias[con].Direccion != "" {
		t.Fatalf("técnico: %+v / %+v", propias[sin], propias[con])
	}
	// Con ordenes.ver no se limita a las suyas.
	sup := leer(pruebas.ContextoUsuario(e.pool, tecnico, nucleo.PermClientesVer, nucleo.PermOrdenesVer, nucleo.PermOrdenesVerPropias, nucleo.PermServiciosVerRed))
	if sup[con].IP == "" || sup[con].Direccion != "" {
		t.Fatalf("con ordenes.ver y solo ver_red: %+v", sup[con])
	}

	// La copia de un ticket u orden: el mismo recorte.
	vis, err := argos.LeerVisibilidad(pruebas.ContextoUsuario(e.pool, 1, nucleo.PermClientesVer), e.pool)
	if err != nil {
		t.Fatal(err)
	}
	c := &argos.CopiaServicio{ID: con, IP: "100.64.12.34", RouterNombre: "Nodo Centro", Direccion: "3a calle", PlanNombre: "Fibra"}
	vis.RecortarCopia(c, nil)
	if c.IP != "" || c.RouterNombre != "" || c.Direccion != "" || c.PlanNombre != "Fibra" {
		t.Fatalf("copia sin permisos: %+v", c)
	}
}

// conAlcances: el usuario en la empresa 1 con esos permisos y alcances (zona o cuadrilla; vacío = global).
func conAlcances(pool nucleo.BD, usuario int64, permisos map[nucleo.Permiso][]contexto.Alcance) context.Context {
	mapa := map[string][]contexto.Alcance{}
	for p, a := range permisos {
		if len(a) == 0 {
			a = []contexto.Alcance{{}}
		}
		mapa[string(p)] = a
	}
	ctx := pruebas.ContextoUsuarioEnEmpresa(pool, usuario, 1)
	return contexto.ConActor(ctx, contexto.Actor{Tipo: contexto.ActorUsuario, ID: usuario, Nombre: "prueba"}, contexto.NuevosPermisos(mapa))
}

// Revisión de seguridad: «sus órdenes» son de la empresa activa, abiertas o terminadas hace menos de
// 24 h, nunca canceladas ni reasignadas; el alcance por zona de servicios.ver_* y de ordenes.ver
// se respeta.
func TestServicios080AlcanceYOrdenes(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	con, sin := e.sembrar080(903)
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/903", nil); r.status != 200 {
		t.Fatalf("ficha: %d %s", r.status, r.crudo)
	}
	_, tecnico := e.usuarioConID("tecnico")
	_, otro := e.usuarioConID("tecnico")
	orden := func(servicio, tecnicoID, empresa int64, estado, finalizada string) {
		t.Helper()
		if _, err := e.pool.Exec(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, cliente_id, servicio_id, tecnico_id, estado, finalizada_en, resultado)
			SELECT id, 'Zona 1', $3, 903, $1, $2, $4::text,
				CASE WHEN $4::text IN ('cerrada', 'cancelada') THEN now() - coalesce(nullif($5::text, ''), '0 seconds')::interval END,
				CASE $4::text WHEN 'cerrada' THEN 'exitosa' WHEN 'cancelada' THEN 'cancelada' END
			FROM campo.tipos_orden WHERE codigo = 'visita'`,
			servicio, tecnicoID, empresa, estado, finalizada); err != nil {
			t.Fatal(err)
		}
	}
	tec := map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermOrdenesVerPropias: nil, nucleo.PermServiciosVerRed: nil, nucleo.PermServiciosVerUbicacion: nil}
	ve := func(ctx context.Context) map[int64]bool {
		t.Helper()
		ss, err := argos.ServiciosVisiblesDeCliente(ctx, e.pool, 903, 1)
		if err != nil {
			t.Fatal(err)
		}
		salida := map[int64]bool{}
		for _, s := range ss {
			if s.IP != "" {
				salida[s.ID] = true
			}
		}
		return salida
	}
	// Ninguna de estas da la red: cerrada hace 25 h, cancelada, de otro técnico (reasignada) y de otra empresa.
	orden(con, tecnico, 1, "cerrada", "25 hours")
	orden(con, tecnico, 1, "cancelada", "")
	orden(con, otro, 1, "en_camino", "")
	orden(sin, tecnico, 2, "pendiente", "")
	if v := ve(conAlcances(e.pool, tecnico, tec)); len(v) != 0 {
		t.Fatalf("técnico sin órdenes vigentes vio la red: %v", v)
	}
	// Cerrada hace 1 h: sí (dentro de las 24 h).
	orden(sin, tecnico, 1, "cerrada", "1 hour")
	if v := ve(conAlcances(e.pool, tecnico, tec)); !v[sin] || v[con] {
		t.Fatalf("técnico con una cerrada hace 1 h: %v", v)
	}
	// Sin clientes.ver no hay nada (aunque tenga servicios.ver_*).
	sinClientes := map[nucleo.Permiso][]contexto.Alcance{nucleo.PermServiciosVerRed: nil, nucleo.PermServiciosVerUbicacion: nil}
	if v := ve(conAlcances(e.pool, tecnico, sinClientes)); len(v) != 0 {
		t.Fatalf("sin clientes.ver: %v", v)
	}

	// Sin órdenes propias: solo cuenta el alcance.
	_, tercero := e.usuarioConID("supervisor")
	// Alcance por zona de servicios.ver_red (los dos servicios son de la zona 3, la del cliente).
	if v := ve(conAlcances(e.pool, tercero, map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: {{ZonaID: 3}}})); !v[con] || !v[sin] {
		t.Fatalf("ver_red de la zona 3: %v", v)
	}
	if v := ve(conAlcances(e.pool, tercero, map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: {{ZonaID: 9}}})); len(v) != 0 {
		t.Fatalf("ver_red de otra zona: %v", v)
	}
	// ordenes.ver con alcance de zona no da la vista completa: solo los servicios de órdenes de esa zona.
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES (9, 1, 'migrada', 'Zona 9') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, cliente_id, servicio_id, zona_id, estado)
		SELECT id, 'Zona 1', 1, 903, $1, 9, 'pendiente' FROM campo.tipos_orden WHERE codigo = 'visita'`, con); err != nil {
		t.Fatal(err)
	}
	supZona := map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: nil, nucleo.PermOrdenesVer: {{ZonaID: 9}}}
	if v := ve(conAlcances(e.pool, tercero, supZona)); !v[con] || v[sin] {
		t.Fatalf("supervisor de zona 9: %v", v)
	}
	// Con ordenes.ver global, todo.
	if v := ve(conAlcances(e.pool, tercero, map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: nil, nucleo.PermOrdenesVer: nil})); !v[con] || !v[sin] {
		t.Fatalf("ordenes.ver global: %v", v)
	}

	// GET /clientes/resolver-ip: exige servicios.ver_red y su alcance.
	e.argos.con(func() {
		e.argos.ips["100.64.12.34"] = map[string]any{"cliente_id": 903, "servicio_id": con, "empresa_id": 1}
	})
	e.usuario("caja").pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=100.64.12.34", nil).problema(t, 403, "prohibido")
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=100.64.12.34", nil); r.status != 200 || r.cuerpo["servicioId"] != float64(con) {
		t.Fatalf("soporte resuelve la IP: %d %s", r.status, r.crudo)
	}
	// Un técnico sin orden vigente en ese servicio: como una IP sin cliente.
	tecHTTP, _ := e.usuarioConID("tecnico")
	tecHTTP.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=100.64.12.34", nil).problema(t, 404, "no_encontrado")
}

// El job de precios del plan es único solo mientras está pendiente: un segundo cambio de precio
// después de completado encola otro; un aviso que llega mientras el job lee hace que vuelva a leer.
func TestServicios080PreciosPlanSegundoAviso(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	con, _ := e.sembrar080(904)
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/904", nil); r.status != 200 {
		t.Fatalf("ficha: %d %s", r.status, r.crudo)
	}
	aviso := func() {
		t.Helper()
		if r := e.procesarWebhook(id, secreto, "catalogo.cambiado", map[string]any{"catalogo": "plan", "id": 12, "empresa_id": nil, "campos": []string{"precio"}}); r.Resultado != "aplicado" {
			t.Fatalf("catalogo.cambiado: %+v", r)
		}
	}
	jobs := func(estado string) int {
		return e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_precios_plan' AND (args->>'plan_id')::bigint = 12
			AND (args->>'conector_id')::bigint = $1 AND state = $2`, id, estado)
	}
	aviso()
	if jobs("available") != 1 {
		t.Fatal("el primer aviso debía encolar el job")
	}
	// Termina el job: el segundo cambio de precio del día encola otro (no lo bloquea el completado).
	if _, err := e.pool.Exec(context.Background(), `UPDATE river.river_job SET state = 'completed', finalized_at = now()
		WHERE kind = 'integracion.argos_precios_plan' AND state = 'available'`); err != nil {
		t.Fatal(err)
	}
	aviso()
	if jobs("available") != 1 || jobs("completed") < 1 {
		t.Fatalf("el segundo aviso debía encolar otro job: available=%d completed=%d", jobs("available"), jobs("completed"))
	}
	// Un aviso que llega mientras el job lee la lista (su propio job se descarta por único): el job
	// vuelve a leer y se queda con el último precio.
	ctx := pruebas.ContextoSistema(e.pool)
	e.argos.con(func() {
		e.argos.servicios[904][0]["precio_mensual"] = "260.00"
		e.argos.alLeerPlan = func() {
			e.argos.servicios[904][0]["precio_mensual"] = "299.00"
			e.enviar(id, secreto, "wh-precio-"+pruebas.Unico(), "catalogo.cambiado", map[string]any{"catalogo": "plan", "id": 12, "empresa_id": nil, "campos": []string{"precio"}}, time.Now())
		}
	})
	res, err := e.mod.RefrescarPreciosPlan.Ejecutar(ctx, argos.ArgsPreciosPlan{ConectorID: id, PlanID: 12})
	if err != nil || res.Relecturas != 1 {
		t.Fatalf("debía volver a leer una vez: %+v %v", res, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE id = $1 AND precio_mensual = 299.00`, con); n != 1 {
		t.Fatal("se quedó con un precio viejo")
	}
}

// Buscar clientes por IP sin servicios.ver_red global: lista vacía y sin preguntar a Argos (oráculo).
func TestServicios080BuscarPorIPSinVerRed(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	e.sembrar080(905)
	antes := e.argos.llamadasA("/api/v1/clientes")
	for _, q := range []string{"100.64.12.34", "100.64.12", "100.64.0.0/16"} {
		r := e.usuario("caja").pedir(http.MethodGet, "/api/v1/clientes?q="+q, nil)
		if r.status != 200 || len(r.cuerpo["data"].([]any)) != 0 {
			t.Fatalf("caja busca %q: %d %s", q, r.status, r.crudo)
		}
	}
	if e.argos.llamadasA("/api/v1/clientes") != antes {
		t.Fatal("sin servicios.ver_red no se debía preguntar a Argos por una IP")
	}
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes?q=100.64.12.34", nil); r.status != 200 || e.argos.llamadasA("/api/v1/clientes") == antes {
		t.Fatalf("soporte sí busca por IP: %d %s", r.status, r.crudo)
	}
}

// Revisión final (C, D, E): resolver-ip sin servicio con órdenes acotadas es 404; la copia de un
// ticket u orden respeta la zona del servicio; la orden asignada cuenta solo con ordenes.ver_propias.
func TestServicios080AlcanceCopiaYPropias(t *testing.T) {
	e := nuevoEntorno(t)
	_, _, _ = e.conectado()
	con, _ := e.sembrar080(906)
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/906", nil); r.status != 200 {
		t.Fatalf("ficha: %d %s", r.status, r.crudo)
	}
	// C: Argos resuelve la IP a un cliente sin servicio; con órdenes acotadas (técnico), 404.
	e.argos.con(func() {
		e.argos.ips["100.64.99.1"] = map[string]any{"cliente_id": 906, "servicio_id": nil, "empresa_id": 1}
	})
	tec, _ := e.usuarioConID("tecnico")
	tec.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=100.64.99.1", nil).problema(t, 404, "no_encontrado")
	if r := e.usuario("soporte").pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=100.64.99.1", nil); r.status != 200 {
		t.Fatalf("soporte (global, sin órdenes acotadas): %d %s", r.status, r.crudo)
	}

	// D: la copia con la zona del servicio (zona 3, la del cliente en la instantánea).
	_, usuario := e.usuarioConID("lectura")
	copia := fmt.Sprintf(`{"cliente": {"id": 906, "codigo": "C-080", "nombre": "María Pérez", "direccion": "Zona 1", "lat": null, "lon": null, "zonaNombre": "Chitá", "estado": "activo"},
		"servicio": {"id": %d, "planNombre": "Fibra", "estado": "activo", "direccion": "3a calle", "ip": "100.64.12.34", "routerNombre": "Nodo Centro", "zonaNombre": "Chitá"},
		"direccion": "3a calle", "completa": true, "anonimizada": false}`, con)
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO integracion.argos_copias (entidad, entidad_id, empresa_id, cliente_id, servicio_id, datos, sincronizado_en)
		VALUES ('orden', 906906, NULL, 906, $1, $2::jsonb, now()) ON CONFLICT DO NOTHING`, con, copia); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.pool.Exec(context.Background(), `DELETE FROM integracion.argos_copias WHERE cliente_id = 906`)
	})
	ipDeCopia := func(zona int64) string {
		t.Helper()
		ctx := conAlcances(e.pool, usuario, map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: {{ZonaID: zona}}})
		c, err := argos.CopiaDe(ctx, e.pool, argos.CopiaOrden, 906906, nil)
		if err != nil || c == nil || c.Servicio == nil {
			t.Fatalf("copia: %+v %v", c, err)
		}
		return c.Servicio.IP
	}
	if ip := ipDeCopia(3); ip != "100.64.12.34" {
		t.Fatalf("ver_red de la zona del servicio debía ver la IP de la copia: %q", ip)
	}
	if ip := ipDeCopia(9); ip != "" {
		t.Fatalf("ver_red de otra zona no ve la IP de la copia: %q", ip)
	}

	// E: una orden asignada a él cuenta solo si tiene ordenes.ver_propias.
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, cliente_id, servicio_id, tecnico_id)
		SELECT id, 'Zona 1', 1, 906, $1, $2 FROM campo.tipos_orden WHERE codigo = 'visita'`, con, usuario); err != nil {
		t.Fatal(err)
	}
	ve := func(permisos map[nucleo.Permiso][]contexto.Alcance) bool {
		t.Helper()
		ss, err := argos.ServiciosVisiblesDeCliente(conAlcances(e.pool, usuario, permisos), e.pool, 906, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range ss {
			if s.ID == con {
				return s.IP != ""
			}
		}
		return false
	}
	if ve(map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: {{ZonaID: 9}}}) {
		t.Fatal("sin ordenes.ver_propias, una orden asignada no da la red")
	}
	if !ve(map[nucleo.Permiso][]contexto.Alcance{nucleo.PermClientesVer: nil, nucleo.PermServiciosVerRed: {{ZonaID: 9}}, nucleo.PermOrdenesVerPropias: nil}) {
		t.Fatal("con ordenes.ver_propias, su orden asignada da la red")
	}
}
