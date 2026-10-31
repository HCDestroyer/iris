package servidor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de la empresa activa (administración): «nadie da más de lo que tiene» también en
// empresas, empresas.todas solo sin límite de alcance, listas acotadas a las empresas del actor
// y la adopción de los datos sin empresa.

// empresasAB registra las empresas 101 (A) y 102 (B) y devuelve un ayudante que crea usuarios
// con un rol y sus empresas (como el sistema).
func empresasAB(t *testing.T, e *entorno) func(rol string, empresas ...int64) (int64, string) {
	t.Helper()
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	for id, nombre := range map[int64]string{101: "A", 102: "B"} {
		if _, err := e.mod.CrearEmpresa.Ejecutar(ctx, identidad.EntradaCrearEmpresa{ID: id, Nombre: nombre}); err != nil {
			t.Fatal(err)
		}
	}
	return func(rol string, empresas ...int64) (int64, string) {
		t.Helper()
		id, correo := e.usuario(rol)
		for _, emp := range empresas {
			if _, err := e.mod.AsignarEmpresaUsuario.Ejecutar(ctx, identidad.EntradaEmpresaUsuario{UsuarioID: id, EmpresaID: emp}); err != nil {
				t.Fatal(err)
			}
		}
		return id, correo
	}
}

// tokenDe crea, con la sesión dada, un token propio limitado a los permisos dados.
func tokenDe(t *testing.T, e *entorno, c *cliente, permisos ...string) *cliente {
	t.Helper()
	r := c.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "limitado " + pruebas.Unico(), "permisos": permisos})
	if r.status != 201 {
		t.Fatalf("token: %d %s", r.status, r.crudo)
	}
	bot := e.cliente()
	bot.bearer = r.cuerpo["valor"].(string)
	return bot
}

func idsDe(t *testing.T, r respuesta) []int64 {
	t.Helper()
	if r.status != 200 {
		t.Fatalf("lista: %d %s", r.status, r.crudo)
	}
	ids := []int64{}
	for _, d := range r.cuerpo["data"].([]any) {
		ids = append(ids, int64(d.(map[string]any)["id"].(float64)))
	}
	return ids
}

// Punto 1 (ALTO), escenario del revisor: un token limitado sin empresas.todas ni membresía no
// cambia la contraseña de un usuario de la empresa 102 (y no entra como él).
func TestTokenLimitadoNoTomaUsuarioDeOtraEmpresa(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	_, correoAdmin := nuevo("admin")
	idB, correoB := nuevo("lectura", 102)
	admin := e.cliente()
	admin.entrar(correoAdmin)
	bot := tokenDe(t, e, admin, "usuarios.editar", "usuarios.ver", "roles.ver", "permisos.ver", "clientes.ver")

	nueva := "otra-contrasena-larga-999"
	bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idB), map[string]any{"version": 1, "contrasena": nueva}).problema(t, 404, "no_encontrado") // fuera de su alcance: como inexistente
	if r := e.cliente().pedir(http.MethodPost, "/api/v1/sesion", map[string]any{"correo": correoB, "contrasena": nueva}); r.status == 200 {
		t.Fatal("entró con la contraseña que puso el token")
	}
	e.cliente().entrar(correoB) // la de siempre sigue valiendo
}

// Punto 1: con empresas.todas solo global, quien administra usuarios solo toca a los que operan
// únicamente en sus empresas: editar (nombre, contraseña, roles), desactivar, tokens para otro,
// revocar, asignar y quitar empresa, y crear usuarios (con empresa propia).
func TestAdministrarSoloUsuariosDeEmpresasPropias(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	idAdmin, correoAdmin := nuevo("admin", 101) // su token limitado opera solo en 101
	idA, _ := nuevo("lectura", 101)
	idB, _ := nuevo("lectura", 102)
	idAB, _ := nuevo("lectura", 101, 102)
	idNadie, _ := nuevo("lectura")
	idGlobal, _ := nuevo("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	bot := tokenDe(t, e, admin, "usuarios.ver", "usuarios.crear", "usuarios.editar", "usuarios.desactivar",
		"usuarios.asignar_empresa", "usuarios.quitar_empresa", "tokens.crear", "tokens.revocar", "tokens.ver",
		"roles.ver", "permisos.ver", "clientes.ver", "empresas.ver", "tickets.ver", "ordenes.ver", "agenda.ver", "rutas.ver", "prospectos.ver", "reportes.ver") // los del rol lectura

	editar := func(id int64, cuerpo map[string]any) respuesta {
		cuerpo["version"] = 1
		return bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", id), cuerpo)
	}
	// A (solo 101): sí.
	if r := editar(idA, map[string]any{"nombre": "A editado", "contrasena": "contrasena-nueva-de-A-1"}); r.status != 200 {
		t.Fatalf("editar a A: %d %s", r.status, r.crudo)
	}
	// B (102), AB (101 y 102), sin empresas y admin global: no, ni siquiera el nombre o roles.
	// Están fuera de su alcance: 404, como si no existieran (no se revela ni su versión).
	for _, id := range []int64{idB, idAB, idNadie, idGlobal} {
		editar(id, map[string]any{"nombre": "x"}).problema(t, 404, "no_encontrado")
		editar(id, map[string]any{"contrasena": "contrasena-nueva-123"}).problema(t, 404, "no_encontrado")
		editar(id, map[string]any{"roles": []map[string]any{{"rol": "lectura"}, {"rol": "tecnico"}}}).problema(t, 404, "no_encontrado")
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/desactivar", id), map[string]any{"version": 1, "motivo": "x"}).problema(t, 404, "no_encontrado")
	}
	// Empresas: a B no se le asigna 101 (opera en 102); a quien no tiene empresas tampoco; a AB
	// no se le quita 101. Están fuera de su alcance: 404, sin nombrar sus empresas.
	for _, r := range []respuesta{
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", idB), map[string]any{"empresaId": 101}),
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", idNadie), map[string]any{"empresaId": 101}),
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/quitar-empresa", idAB), map[string]any{"empresaId": 101}),
		bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", 999999999), map[string]any{"empresaId": 101}),
	} {
		r.problema(t, 404, "no_encontrado")
		if strings.Contains(string(r.crudo), "102") {
			t.Fatalf("el 404 nombra la empresa del usuario: %s", r.crudo)
		}
	}

	// Tokens para integraciones: solo la de 101.
	integ := func(empresa int64) int64 {
		// Fixture (como la consola): por HTTP una integración solo lleva su rol de contrato.
		id := e.integracion("Integ", "lectura")
		if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", id), map[string]any{"empresaId": empresa}); r.status != 200 {
			t.Fatalf("asignar: %d %s", r.status, r.crudo)
		}
		return id
	}
	integA, integB := integ(101), integ(102)
	if r := bot.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "a", "usuarioId": integA}); r.status != 201 {
		t.Fatalf("token para la integración de 101: %d %s", r.status, r.crudo)
	}
	// Fuera de su alcance: 404 (no se revela si es integración ni sus empresas).
	bot.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "b", "usuarioId": integB}).problema(t, 404, "no_encontrado")
	bot.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "b", "usuarioId": idB}).problema(t, 404, "no_encontrado")
	tkB := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "b", "usuarioId": integB})
	if tkB.status != 201 {
		t.Fatalf("token de B: %d %s", tkB.status, tkB.crudo)
	}
	idTkB := int64(tkB.cuerpo["token"].(map[string]any)["id"].(float64))
	// Revocar un token de un usuario fuera de su alcance: 404, igual que un token inexistente.
	for _, id := range []int64{idTkB, 999999999} {
		r := bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", id), map[string]any{})
		r.problema(t, 404, "no_encontrado")
		if strings.Contains(string(r.crudo), "102") {
			t.Fatalf("el 404 nombra la empresa del dueño: %s", r.crudo)
		}
	}

	// Crear usuarios: con empresa propia; sin empresa (quedaría huérfano) 422; ajena 403.
	crear := func(empresas []int64) respuesta {
		return bot.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Nuevo", "correo": pruebas.Correo("nuevo"),
			"contrasena": contrasena, "roles": []map[string]any{{"rol": "lectura"}}, "empresas": empresas})
	}
	crear(nil).problema(t, 422, "validacion")
	crear([]int64{102}).problema(t, 403, "prohibido")
	r := crear([]int64{101})
	if r.status != 201 || len(r.cuerpo["empresas"].([]any)) != 1 {
		t.Fatalf("crear con empresa propia: %d %s", r.status, r.crudo)
	}
	idNuevo := int64(r.cuerpo["id"].(float64))
	if r := editar(idNuevo, map[string]any{"nombre": "Nuevo editado"}); r.status != 200 {
		t.Fatalf("editar al usuario creado: %d %s", r.status, r.crudo)
	}

	// La sesión del admin global (empresas.todas) sí toca a todos.
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/editar", idB), map[string]any{"version": 1, "nombre": "B por admin"}); r.status != 200 {
		t.Fatalf("admin global: %d %s", r.status, r.crudo)
	}
	_ = idAdmin
}

// Punto 2 (MEDIO): empresas.todas con alcance de zona no da acceso a todas las empresas.
func TestEmpresasTodasConAlcanceNoEsGlobal(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	var rol int64
	if err := pool.QueryRow(ctx, `INSERT INTO nucleo.roles (codigo, nombre, descripcion) VALUES ($1, 'Zonal', '') RETURNING id`, "zonal_"+pruebas.Unico()).Scan(&rol); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // otras pruebas cuentan los roles
		for _, sql := range []string{"DELETE FROM nucleo.usuarios_roles WHERE rol_id = $1",
			"DELETE FROM nucleo.roles_permisos WHERE rol_id = $1", "DELETE FROM nucleo.roles WHERE id = $1"} {
			if _, err := pool.Exec(context.Background(), sql, rol); err != nil {
				t.Error(err)
			}
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, 'empresas.todas'), ($1, 'clientes.ver'), ($1, 'usuarios.asignar_empresa'), ($1, 'empresas.ver')`, rol); err != nil {
		t.Fatal(err)
	}
	idZonal, correo := nuevo("lectura")
	if _, err := pool.Exec(ctx, `INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id, zona_id) VALUES ($1, $2, 7)`, idZonal, rol); err != nil {
		t.Fatal(err)
	}
	idA, _ := nuevo("lectura", 101)
	zonal := e.cliente()
	zonal.entrar(correo)

	yo := zonal.pedir(http.MethodGet, "/api/v1/yo", nil)
	if yo.cuerpo["todasLasEmpresas"] != false || yo.cuerpo["modoEmpresa"] != "sin_empresa" {
		t.Fatalf("yo con empresas.todas por zona: %s", yo.crudo)
	}
	zonal.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil).problema(t, 403, "sin_empresa")
	zonal.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, identidad.CabeceraEmpresa, "101").problema(t, 403, "empresa_no_permitida")
	zonal.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", idA), map[string]any{"empresaId": 102}).problema(t, 404, "no_encontrado") // fuera de su alcance
	if ids := idsDe(t, zonal.pedir(http.MethodGet, "/api/v1/empresas", nil)); len(ids) != 0 {
		t.Fatalf("lista de empresas con empresas.todas por zona: %v", ids)
	}
}

// Punto 3 (MEDIO): usuarios, tokens y empresas se listan solo dentro de las empresas del actor
// (en la consulta, con el cursor), salvo con empresas.todas global o sin empresas registradas.
func TestListasAcotadasPorEmpresa(t *testing.T) {
	e := entornoEmpresas(t)

	// Sin empresas registradas no hay separación: lectura ve a todos.
	idSin, correoSin := e.usuario("lectura")
	idOtroSin, _ := e.usuario("lectura")
	sin := e.cliente()
	sin.entrar(correoSin)
	todos := listarTodo(t, sin, "/api/v1/usuarios")
	if !slices.Contains(todos, idSin) || !slices.Contains(todos, idOtroSin) {
		t.Fatalf("sin conector, lista completa: %v", todos)
	}

	nuevo := empresasAB(t, e)
	idA, correoA := nuevo("lectura", 101)
	idA2, _ := nuevo("lectura", 101)
	idB, _ := nuevo("lectura", 102)
	idAB, _ := nuevo("lectura", 101, 102)
	idAdmin, correoAdmin := nuevo("admin", 101)
	a := e.cliente()
	a.entrar(correoA)
	ids := listarTodo(t, a, "/api/v1/usuarios")
	slices.Sort(ids)
	esperado := []int64{idA, idA2, idAB, idAdmin}
	slices.Sort(esperado)
	if !slices.Equal(ids, esperado) {
		t.Fatalf("usuarios visibles para A: %v, esperaba %v (B=%d)", ids, esperado, idB)
	}

	admin := e.cliente()
	admin.entrar(correoAdmin)
	if ids := listarTodo(t, admin, "/api/v1/usuarios"); !slices.Contains(ids, idB) || !slices.Contains(ids, idSin) {
		t.Fatalf("admin global ve a todos: %v", ids)
	}
	if ids := idsDe(t, admin.pedir(http.MethodGet, "/api/v1/empresas", nil)); !slices.Equal(ids, []int64{101, 102}) {
		t.Fatalf("empresas del admin: %v", ids)
	}

	// Tokens: uno de A2 (101) y uno de B (102). El token limitado del admin (sin empresas.todas,
	// opera en 101) ve los de 101 y los propios, no el de B.
	tkA2 := tokenPropio(t, e, idA2)
	tkB := tokenPropio(t, e, idB)
	bot := tokenDe(t, e, admin, "tokens.ver", "usuarios.ver", "empresas.ver")
	visibles := listarTodo(t, bot, "/api/v1/tokens")
	if !slices.Contains(visibles, tkA2) || slices.Contains(visibles, tkB) {
		t.Fatalf("tokens visibles: %v (A2=%d, B=%d)", visibles, tkA2, tkB)
	}
	if ids := listarTodo(t, bot, "/api/v1/usuarios"); slices.Contains(ids, idB) || !slices.Contains(ids, idA) {
		t.Fatalf("usuarios visibles para el token limitado: %v", ids)
	}
	if ids := idsDe(t, bot.pedir(http.MethodGet, "/api/v1/empresas", nil)); !slices.Equal(ids, []int64{101}) {
		t.Fatalf("empresas del token limitado: %v", ids)
	}
}

// listarTodo recorre una lista paginada de a 1 (para ejercitar el cursor) y devuelve los ids.
func listarTodo(t *testing.T, c *cliente, ruta string) []int64 {
	t.Helper()
	ids := []int64{}
	cursor := ""
	for range 500 {
		r := c.pedir(http.MethodGet, ruta+"?limit=1&cursor="+cursor, nil)
		pagina := idsDe(t, r)
		if len(pagina) > 1 {
			t.Fatalf("limit=1 devolvió %d", len(pagina))
		}
		ids = append(ids, pagina...)
		siguiente, _ := r.cuerpo["nextCursor"].(string)
		if siguiente == "" {
			return ids
		}
		cursor = siguiente
	}
	t.Fatal("la paginación no termina")
	return nil
}

// tokenPropio crea un token del usuario dado como él mismo (sesión propia).
func tokenPropio(t *testing.T, e *entorno, usuario int64) int64 {
	t.Helper()
	ctx := pruebas.ContextoUsuario(pruebas.Pool(t), usuario, nucleo.PermTokensCrear)
	tk, err := e.mod.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{Nombre: "propio"})
	if err != nil {
		t.Fatal(err)
	}
	return tk.Token.ID
}

// Punto 3: UsuarioEnEmpresa (para I2 al asignar tickets).
func TestUsuarioEnEmpresa(t *testing.T) {
	e := entornoEmpresas(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	idSin, _ := e.usuario("lectura")
	if ok, err := identidad.UsuarioEnEmpresa(ctx, pool, idSin, 0); err != nil || !ok {
		t.Fatalf("sin conector, empresa null: %v %v", ok, err)
	}
	nuevo := empresasAB(t, e)
	idA, _ := nuevo("lectura", 101)
	idAdmin, _ := nuevo("admin")
	casos := []struct {
		usuario, empresa int64
		esperado         bool
	}{
		{idA, 101, true}, {idA, 102, false}, {idA, 0, false}, {idA, 999, false},
		{idAdmin, 101, true}, {idAdmin, 102, true}, {idAdmin, 999, false}, {idSin, 101, false},
	}
	for _, c := range casos {
		if ok, err := identidad.UsuarioEnEmpresa(ctx, pool, c.usuario, c.empresa); err != nil || ok != c.esperado {
			t.Fatalf("UsuarioEnEmpresa(%d, %d) = %v %v, esperaba %v", c.usuario, c.empresa, ok, err, c.esperado)
		}
	}
}

// Punto 4 (MEDIO): al pasar a «con conector», un admin global adopta los datos sin empresa.
func TestAdoptarDatosSinEmpresa(t *testing.T) {
	e := entornoEmpresas(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	const tabla = "public.prueba_operativa"
	for _, sql := range []string{
		"DROP TABLE IF EXISTS public.prueba_operativa",
		"CREATE TABLE public.prueba_operativa (id bigserial PRIMARY KEY, empresa_id bigint)",
		"INSERT INTO public.prueba_operativa (empresa_id) VALUES (NULL), (NULL), (NULL), (102)",
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	nucleo.RegistrarTablaConEmpresa(tabla)
	t.Cleanup(func() {
		nucleo.QuitarTablaConEmpresa(tabla)
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS public.prueba_operativa")
	})

	// Sin empresas registradas no hay aviso (los datos null son los de ese modo).
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	if yo := admin.pedir(http.MethodGet, "/api/v1/yo", nil); yo.cuerpo["hayDatosSinEmpresa"] != false {
		t.Fatalf("sin conector no hay aviso: %s", yo.crudo)
	}

	nuevo := empresasAB(t, e)
	_, correoLectura := nuevo("lectura", 101)
	lectura := e.cliente()
	lectura.entrar(correoLectura)
	if yo := admin.pedir(http.MethodGet, "/api/v1/yo", nil); yo.cuerpo["hayDatosSinEmpresa"] != true {
		t.Fatalf("admin debía ver el aviso: %s", yo.crudo)
	}
	if yo := lectura.pedir(http.MethodGet, "/api/v1/yo", nil); yo.cuerpo["hayDatosSinEmpresa"] != false {
		t.Fatalf("lectura no ve el aviso: %s", yo.crudo)
	}

	ruta := "/api/v1/empresas/101/adoptar-sin-empresa"
	lectura.pedir(http.MethodPost, ruta, nil).problema(t, 403, "prohibido")
	// Un token del admin con empresas.adoptar pero sin empresas.todas: no.
	tokenDe(t, e, admin, "empresas.adoptar").pedir(http.MethodPost, ruta, nil).problema(t, 403, "prohibido")
	admin.pedir(http.MethodPost, "/api/v1/empresas/999/adoptar-sin-empresa", nil).problema(t, 404, "no_encontrado")

	r := admin.pedir(http.MethodPost, ruta, nil)
	if r.status != 200 || r.cuerpo["total"] != float64(3) || r.cuerpo["filas"].(map[string]any)[tabla] != float64(3) {
		t.Fatalf("adoptar: %d %s", r.status, r.crudo)
	}
	var n101, nNull int
	if err := pool.QueryRow(ctx, "SELECT count(*) FILTER (WHERE empresa_id = 101), count(*) FILTER (WHERE empresa_id IS NULL) FROM public.prueba_operativa").Scan(&n101, &nNull); err != nil || n101 != 3 || nNull != 0 {
		t.Fatalf("filas: 101=%d null=%d %v", n101, nNull, err)
	}
	if yo := admin.pedir(http.MethodGet, "/api/v1/yo", nil); yo.cuerpo["hayDatosSinEmpresa"] != false {
		t.Fatalf("tras adoptar no hay aviso: %s", yo.crudo)
	}
	// Repetir (otra clave) no adopta nada.
	if r := admin.pedir(http.MethodPost, ruta, nil); r.status != 200 || r.cuerpo["total"] != float64(0) {
		t.Fatalf("repetir: %d %s", r.status, r.crudo)
	}

	// Auditado y con evento.
	var payload string
	if err := pool.QueryRow(ctx, `SELECT payload::text FROM integracion.outbox WHERE evento = 'empresa.datos_adoptados' AND empresa_id = 101 ORDER BY id LIMIT 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	// Las demás tablas registradas (las de los módulos que se enlazan en este binario de
	// pruebas, p. ej. soporte) van con 0.
	var ev struct {
		Filas     map[string]int `json:"filas"`
		EmpresaID int64          `json:"empresa_id"`
	}
	if err := json.Unmarshal([]byte(payload), &ev); err != nil || ev.EmpresaID != 101 || ev.Filas[tabla] != 3 {
		t.Fatalf("payload: %s", payload)
	}
	for k, v := range ev.Filas {
		if k != tabla && v != 0 {
			t.Fatalf("payload: %s", payload)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'empresas.adoptar' AND resultado = 'ok' AND entidad_id = '101'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("auditoría: %d %v", n, err)
	}
}
