package servidor_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
)

// sonda es un caso de uso operativo (como los de I2: tickets, órdenes) que devuelve la empresa
// que le llegó en el contexto.
var sonda = auditoria.Nuevo(auditoria.Caso[struct{}, int64]{
	Accion: "sonda.empresa", Permiso: nucleo.PermClientesVer,
	AuditarExito: func(int64) bool { return false },
	Logica: func(ctx context.Context, _ *auditoria.Tx, _ struct{}) (int64, error) {
		return contexto.EmpresaDe(ctx), nil
	},
})

type salidaSonda struct {
	Body struct {
		Empresa int64 `json:"empresa"`
	}
}

// entornoEmpresas es el servidor con la sonda montada en GET /api/v1/prueba/empresa. Empieza
// sin empresas registradas (modo sin conector).
func entornoEmpresas(t *testing.T) *entorno {
	t.Helper()
	e := nuevoEntorno(t)
	pool := pruebas.Pool(t)
	for _, sql := range []string{"DELETE FROM nucleo.usuarios_empresas", "DELETE FROM nucleo.empresas"} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: pool, Identidad: e.mod, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modulos: []func(huma.API){func(api huma.API) {
			huma.Register(api, huma.Operation{OperationID: "sonda", Method: http.MethodGet, Path: "/api/v1/prueba/empresa"},
				func(ctx context.Context, _ *struct{}) (*salidaSonda, error) {
					id, err := sonda.Ejecutar(ctx, struct{}{})
					if err != nil {
						return nil, err
					}
					s := &salidaSonda{}
					s.Body.Empresa = id
					return s, nil
				})
		}},
	})
	e.srv = httptest.NewServer(h)
	t.Cleanup(e.srv.Close)
	return e
}

func (c *cliente) empresaDeSonda(cabeceras ...string) int64 {
	c.t.Helper()
	r := c.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, cabeceras...)
	if r.status != 200 {
		c.t.Fatalf("sonda: %d %s", r.status, r.crudo)
	}
	return int64(r.cuerpo["empresa"].(float64))
}

// Regresión (revisión de seguridad, hallazgo 1): la empresa activa existe y llega poblada a los
// casos de uso; un usuario solo opera en sus empresas; sin elegir, lo operativo no corre «sin
// filtro».
func TestEmpresaActiva(t *testing.T) {
	e := entornoEmpresas(t)
	_, correoAdmin := e.usuario("admin")
	idLectura, correoLectura := e.usuario("lectura")
	admin, lectura := e.cliente(), e.cliente()
	admin.entrar(correoAdmin)
	lectura.entrar(correoLectura)

	// Sin empresas registradas: modo sin conector (empresa_id null), y la cabecera no vale.
	if id := lectura.empresaDeSonda(); id != 0 {
		t.Fatalf("sin conector, la empresa debía ser 0: %d", id)
	}
	if yo := lectura.pedir(http.MethodGet, "/api/v1/yo", nil); yo.cuerpo["modoEmpresa"] != "sin_conector" || yo.cuerpo["empresaActiva"] != nil {
		t.Fatalf("yo sin conector: %s", yo.crudo)
	}
	lectura.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, identidad.CabeceraEmpresa, "1").problema(t, 403, "empresa_no_permitida")

	// Se registran dos empresas: quien no tiene ninguna no ve datos operativos.
	lectura.pedir(http.MethodPost, "/api/v1/empresas", map[string]any{"id": 1, "nombre": "ISP Norte"}).problema(t, 403, "prohibido")
	for id, nombre := range map[int]string{1: "ISP Norte", 2: "ISP Sur"} {
		if r := admin.pedir(http.MethodPost, "/api/v1/empresas", map[string]any{"id": id, "nombre": nombre, "nit": "123-K"}); r.status != 201 {
			t.Fatalf("crear empresa: %d %s", r.status, r.crudo)
		}
	}
	admin.pedir(http.MethodPost, "/api/v1/empresas", map[string]any{"id": 1, "nombre": "Repetida"}).problema(t, 409, "duplicado")
	lectura.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil).problema(t, 403, "sin_empresa")
	if yo := lectura.pedir(http.MethodGet, "/api/v1/yo", nil); yo.status != 200 || yo.cuerpo["modoEmpresa"] != "sin_empresa" || len(yo.cuerpo["empresas"].([]any)) != 0 {
		t.Fatalf("yo sin empresa: %d %s", yo.status, yo.crudo)
	}

	// Con una empresa asignada, esa es la activa por defecto; otra por cabecera, 403.
	asignar := fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", idLectura)
	if r := admin.pedir(http.MethodPost, asignar, map[string]any{"empresaId": 1}); r.status != 200 || len(r.cuerpo["empresas"].([]any)) != 1 {
		t.Fatalf("asignar: %d %s", r.status, r.crudo)
	}
	admin.pedir(http.MethodPost, asignar, map[string]any{"empresaId": 1}).problema(t, 422, "empresa_ya_asignada")
	admin.pedir(http.MethodPost, asignar, map[string]any{"empresaId": 99}).problema(t, 404, "no_encontrado")
	if id := lectura.empresaDeSonda(); id != 1 {
		t.Fatalf("empresa por defecto: %d", id)
	}
	lectura.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, identidad.CabeceraEmpresa, "2").problema(t, 403, "empresa_no_permitida")
	lectura.pedir(http.MethodGet, "/api/v1/yo", nil, identidad.CabeceraEmpresa, "abc").problema(t, 403, "empresa_no_permitida")
	yo := lectura.pedir(http.MethodGet, "/api/v1/yo", nil)
	if activa, _ := yo.cuerpo["empresaActiva"].(map[string]any); yo.cuerpo["modoEmpresa"] != "activa" || activa["id"] != float64(1) || activa["nombre"] != "ISP Norte" {
		t.Fatalf("yo con una empresa: %s", yo.crudo)
	}

	// Con dos, hay que elegir: sin cabecera lo operativo responde 422; lo administrativo y /yo no.
	if r := admin.pedir(http.MethodPost, asignar, map[string]any{"empresaId": 2}); r.status != 200 {
		t.Fatalf("asignar 2: %d %s", r.status, r.crudo)
	}
	lectura.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil).problema(t, 422, "empresa_requerida")
	if id := lectura.empresaDeSonda(identidad.CabeceraEmpresa, "2"); id != 2 {
		t.Fatalf("empresa por cabecera: %d", id)
	}
	yo = lectura.pedir(http.MethodGet, "/api/v1/yo", nil)
	if yo.status != 200 || yo.cuerpo["modoEmpresa"] != "por_elegir" || len(yo.cuerpo["empresas"].([]any)) != 2 {
		t.Fatalf("yo por elegir: %s", yo.crudo)
	}
	if r := lectura.pedir(http.MethodGet, "/api/v1/usuarios", nil); r.status != 200 {
		t.Fatalf("lo administrativo no exige empresa: %d %s", r.status, r.crudo)
	}

	// Admin (empresas.todas): opera en cualquiera sin asignación.
	yo = admin.pedir(http.MethodGet, "/api/v1/yo", nil)
	if yo.cuerpo["todasLasEmpresas"] != true || len(yo.cuerpo["empresas"].([]any)) != 2 {
		t.Fatalf("yo admin: %s", yo.crudo)
	}
	if id := admin.empresaDeSonda(identidad.CabeceraEmpresa, "1"); id != 1 {
		t.Fatalf("admin con cabecera: %d", id)
	}

	// Quitar.
	quitar := fmt.Sprintf("/api/v1/usuarios/%d/quitar-empresa", idLectura)
	if r := admin.pedir(http.MethodPost, quitar, map[string]any{"empresaId": 2}); r.status != 200 || len(r.cuerpo["empresas"].([]any)) != 1 {
		t.Fatalf("quitar: %d %s", r.status, r.crudo)
	}
	admin.pedir(http.MethodPost, quitar, map[string]any{"empresaId": 2}).problema(t, 422, "empresa_no_asignada")
	if id := lectura.empresaDeSonda(); id != 1 {
		t.Fatalf("tras quitar la 2: %d", id)
	}
	lectura.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, identidad.CabeceraEmpresa, "2").problema(t, 403, "empresa_no_permitida")

	// Auditado y con evento.
	pool := pruebas.Pool(t)
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria.acciones WHERE accion IN ('usuarios.asignar_empresa', 'usuarios.quitar_empresa') AND resultado = 'ok' AND entidad_id = $1`, fmt.Sprint(idLectura)).Scan(&n); err != nil || n != 3 {
		t.Fatalf("auditoría de membresía: %d %v", n, err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM integracion.outbox WHERE evento IN ('usuario.empresa_asignada', 'usuario.empresa_quitada') AND payload->>'usuario_id' = $1`, fmt.Sprint(idLectura)).Scan(&n); err != nil || n != 3 {
		t.Fatalf("eventos de membresía: %d %v", n, err)
	}
}

// Un token o una integración opera en las empresas de su usuario dueño, nada más.
func TestEmpresaDeTokens(t *testing.T) {
	e := entornoEmpresas(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	for id := 1; id <= 2; id++ {
		if r := admin.pedir(http.MethodPost, "/api/v1/empresas", map[string]any{"id": id, "nombre": fmt.Sprint("Empresa ", id)}); r.status != 201 {
			t.Fatalf("crear empresa: %d %s", r.status, r.crudo)
		}
	}
	r := admin.pedir(http.MethodPost, "/api/v1/usuarios", map[string]any{"nombre": "Argos", "correo": pruebas.Correo("argos"), "tipo": "integracion", "roles": []map[string]any{{"rol": "lectura"}}})
	if r.status != 201 {
		t.Fatalf("crear integración: %d %s", r.status, r.crudo)
	}
	integ := int64(r.cuerpo["id"].(float64))
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/usuarios/%d/asignar-empresa", integ), map[string]any{"empresaId": 2}); r.status != 200 {
		t.Fatalf("asignar: %d %s", r.status, r.crudo)
	}
	tk := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "argos", "usuarioId": integ})
	if tk.status != 201 {
		t.Fatalf("token: %d %s", tk.status, tk.crudo)
	}
	bot := e.cliente()
	bot.bearer = tk.cuerpo["valor"].(string)
	if id := bot.empresaDeSonda(); id != 2 {
		t.Fatalf("token: empresa %d", id)
	}
	bot.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, identidad.CabeceraEmpresa, "1").problema(t, 403, "empresa_no_permitida")

	// Un token del admin limitado a clientes.ver pierde empresas.todas: sin asignaciones, nada.
	tk = admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "limitado", "permisos": []string{"clientes.ver"}})
	if tk.status != 201 {
		t.Fatalf("token limitado: %d %s", tk.status, tk.crudo)
	}
	lim := e.cliente()
	lim.bearer = tk.cuerpo["valor"].(string)
	lim.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil).problema(t, 403, "sin_empresa")
	lim.pedir(http.MethodGet, "/api/v1/prueba/empresa", nil, identidad.CabeceraEmpresa, "1").problema(t, 403, "empresa_no_permitida")
}

// Nadie da ni quita acceso a una empresa en que no opera.
func TestAsignarEmpresaSoloPropia(t *testing.T) {
	e := entornoEmpresas(t)
	pool := pruebas.Pool(t)
	ctx := pruebas.ContextoSistema(pool)
	for id, nombre := range map[int64]string{1: "Uno", 2: "Dos"} {
		if _, err := e.mod.CrearEmpresa.Ejecutar(ctx, identidad.EntradaCrearEmpresa{ID: id, Nombre: nombre}); err != nil {
			t.Fatal(err)
		}
	}
	idJefe, _ := e.usuario("lectura")
	idOtro, _ := e.usuario("lectura")
	if _, err := e.mod.AsignarEmpresaUsuario.Ejecutar(ctx, identidad.EntradaEmpresaUsuario{UsuarioID: idJefe, EmpresaID: 1}); err != nil {
		t.Fatal(err)
	}
	jefe := pruebas.ContextoUsuario(pool, idJefe, nucleo.PermUsuariosAsignarEmpresa)
	if _, err := e.mod.AsignarEmpresaUsuario.Ejecutar(jefe, identidad.EntradaEmpresaUsuario{UsuarioID: idOtro, EmpresaID: 2}); !esProblema(err, "prohibido") {
		t.Fatalf("asignar una empresa ajena debía ser 403: %v", err)
	}
	if _, err := e.mod.AsignarEmpresaUsuario.Ejecutar(jefe, identidad.EntradaEmpresaUsuario{UsuarioID: idOtro, EmpresaID: 1}); err != nil {
		t.Fatalf("asignar la propia: %v", err)
	}
}

func esProblema(err error, codigo string) bool {
	p, _ := nucleo.AProblema(err)
	return err != nil && p.Codigo() == codigo
}
