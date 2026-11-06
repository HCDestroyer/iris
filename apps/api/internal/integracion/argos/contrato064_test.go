package argos_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
)

// Contrato Iris ↔ ArgosISP 0.6.4 (A22): `nombre` en cada elemento de `bodegas[]` de
// GET /inventario/tecnicos, para que el panel elija la bodega del técnico por nombre.

// bodegaArgos064 y tecnicoArgos064 son la forma de GET /inventario/tecnicos en el contrato 0.6.4
// §3.3: { id_externo, correo, nombre, bodegas: [{ id, empresa_id, activa, nombre }] }.
type bodegaArgos064 struct {
	ID        int64  `json:"id"`
	EmpresaID int64  `json:"empresa_id"`
	Activa    bool   `json:"activa"`
	Nombre    string `json:"nombre"`
}

type tecnicoArgos064 struct {
	IDExterno string           `json:"id_externo"`
	Correo    string           `json:"correo"`
	Nombre    string           `json:"nombre"`
	Bodegas   []bodegaArgos064 `json:"bodegas"`
}

// comoMapas convierte la lista tipada a lo que sirve el Argos simulado.
func comoMapas(t *testing.T, xs []tecnicoArgos064) []map[string]any {
	t.Helper()
	b, err := json.Marshal(xs)
	if err != nil {
		t.Fatal(err)
	}
	var r []map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestContratoIris064(t *testing.T) {
	if argos.ContratoIris != "0.6.4" {
		t.Fatalf("ContratoIris: %s", argos.ContratoIris)
	}
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.9.0" })
	_, _, _ = e.conectado()
	var canje map[string]any
	e.argos.con(func() { canje = e.argos.canjes[len(e.argos.canjes)-1] })
	if canje["contrato"] != "0.6.4" {
		t.Fatalf("el canje debía mandar contrato 0.6.4: %v", canje["contrato"])
	}
}

// El selector devuelve el nombre de cada bodega y, con sinVincular, salta los ya vinculados a una
// ficha (de cualquier empresa) leyendo de Argos las páginas que hagan falta para llenar el límite.
func TestSelectorTecnicosArgosNombreYSinVincular(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() {
		e.argos.version = "1.9.0"
		e.argos.tecnicos = comoMapas(t, []tecnicoArgos064{
			{IDExterno: "u-21", Correo: "ana@argos.gt", Nombre: "Ana Pérez", Bodegas: []bodegaArgos064{{ID: 210, EmpresaID: 1, Activa: true, Nombre: "Bodega Norte"}}},
			{IDExterno: "u-22", Correo: "beto@argos.gt", Nombre: "Beto Gómez", Bodegas: []bodegaArgos064{{ID: 220, EmpresaID: 1, Activa: true, Nombre: "Bodega Sur"}}},
			{IDExterno: "u-23", Correo: "caro@argos.gt", Nombre: "Caro Díaz", Bodegas: []bodegaArgos064{{ID: 230, EmpresaID: 1, Activa: true, Nombre: "  Bodega Centro "}}},
			{IDExterno: "u-24", Correo: "dani@argos.gt", Nombre: "Dani Ruiz", Bodegas: []bodegaArgos064{{ID: 240, EmpresaID: 1, Activa: true, Nombre: "Bodega Este"}}},
			{IDExterno: "u-25", Correo: "eva@argos.gt", Nombre: "Eva Luna", Bodegas: []bodegaArgos064{{ID: 250, EmpresaID: 1, Activa: true}}},
		})
	})
	admin, _, _ := e.conectado()
	// Dos fichas ya vinculadas: u-21 y u-22 (la primera página entera del simulado, de 2 en 2).
	e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo IN ('u-21', 'u-22')`)
	for _, id := range []string{"u-21", "u-22"} {
		e.usuario("tecnico")
		usuario := e.contar(`SELECT id FROM nucleo.usuarios ORDER BY id DESC LIMIT 1`)
		e.exec(`INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_vinculado_en) VALUES ($1, $2, now())`, usuario, id)
	}
	defer e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo IN ('u-21', 'u-22')`)

	auditadas := func() int {
		return e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'agenda.ver_tecnicos_argos' AND resultado = 'ok'`)
	}
	antes := auditadas()
	// Sin filtro: la página de Argos tal cual, con el nombre de la bodega.
	todo := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?limit=2", nil, "X-Iris-Empresa", "1")
	if todo.status != 200 {
		t.Fatalf("selector: %d %s", todo.status, todo.crudo)
	}
	data := todo.cuerpo["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("página: %s", todo.crudo)
	}
	ana := data[0].(map[string]any)
	bodega := ana["bodegas"].([]any)[0].(map[string]any)
	if bodega["nombre"] != "Bodega Norte" || bodega["id"] != float64(210) || ana["vinculadoA"] == nil {
		t.Fatalf("ana: %v", ana)
	}

	// sinVincular: salta la primera página (los dos vinculados) y llena el límite con la siguiente.
	libres := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?limit=2&sinVincular=true", nil, "X-Iris-Empresa", "1")
	if libres.status != 200 {
		t.Fatalf("sin vincular: %d %s", libres.status, libres.crudo)
	}
	data = libres.cuerpo["data"].([]any)
	if len(data) != 2 || data[0].(map[string]any)["idExterno"] != "u-23" || data[1].(map[string]any)["idExterno"] != "u-24" {
		t.Fatalf("sin vincular: %s", libres.crudo)
	}
	caro := data[0].(map[string]any)
	if caro["vinculadoA"] != nil || caro["enEmpresa"] != true || caro["bodegas"].([]any)[0].(map[string]any)["nombre"] != "Bodega Centro" {
		t.Fatalf("caro: %v", caro)
	}
	if libres.cuerpo["nextCursor"] != "4" {
		t.Fatalf("cursor: %v", libres.cuerpo["nextCursor"])
	}
	// Última página: Eva, con un Argos que no manda el nombre (anterior a 0.6.4): queda vacío.
	fin := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?limit=2&sinVincular=true&cursor=4", nil, "X-Iris-Empresa", "1")
	data = fin.cuerpo["data"].([]any)
	if fin.status != 200 || len(data) != 1 || fin.cuerpo["nextCursor"] != nil {
		t.Fatalf("fin: %s", fin.crudo)
	}
	if b := data[0].(map[string]any)["bodegas"].([]any)[0].(map[string]any); b["nombre"] != "" {
		t.Fatalf("sin nombre: %v", b)
	}

	// Una sola lectura auditada por llamada, aunque lea varias páginas de Argos.
	if n := auditadas() - antes; n != 3 {
		t.Fatalf("auditoría: %d", n)
	}
	// Sin permiso de vincular: 403.
	e.usuario("supervisor").pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?sinVincular=true", nil).problema(t, 403, "prohibido")
}

// Argos lento: la lectura con sinVincular tiene un tope global; al vencer entrega lo leído con su
// nextCursor (la página que no alcanzó queda como siguiente).
func TestSelectorTecnicosArgosLentoEntregaLoLeido(t *testing.T) {
	e := nuevoEntorno(t)
	xs := []tecnicoArgos064{}
	for i := range 12 {
		id := fmt.Sprintf("u-l%d", i)
		xs = append(xs, tecnicoArgos064{IDExterno: id, Correo: id + "@argos.gt", Nombre: "Técnico " + id, Bodegas: []bodegaArgos064{{ID: int64(700 + i), EmpresaID: 1, Activa: true, Nombre: "Bodega " + id}}})
	}
	e.argos.con(func() {
		e.argos.version = "1.9.0"
		e.argos.tecnicos = comoMapas(t, xs)
	})
	admin, _, _ := e.conectado()
	// 2 por página (simulado) y limit 10: haría falta leer 5 páginas; cada una tarda 300 ms y el tope
	// es de 700 ms, así que alcanzan 2 (4 técnicos).
	e.argos.con(func() { e.argos.retrasoTecnicos = 300 * time.Millisecond })
	e.mod.FijarPlazoListaTecnicos(700 * time.Millisecond)
	defer e.mod.FijarPlazoListaTecnicos(0)
	inicio := time.Now()
	r := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?limit=10&sinVincular=true", nil, "X-Iris-Empresa", "1")
	if r.status != 200 {
		t.Fatalf("lento: %d %s", r.status, r.crudo)
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Fatalf("debía cortar al vencer el tope: %s", d)
	}
	data := r.cuerpo["data"].([]any)
	if len(data) == 0 || len(data) >= 10 || len(data)%2 != 0 {
		t.Fatalf("debía entregar las páginas leídas: %d %s", len(data), r.crudo)
	}
	if r.cuerpo["nextCursor"] != fmt.Sprint(len(data)) {
		t.Fatalf("el cursor debía apuntar a la página que no alcanzó: %v (%d leídos)", r.cuerpo["nextCursor"], len(data))
	}
	// Desde ese cursor, sin retraso, sigue donde quedó.
	e.argos.con(func() { e.argos.retrasoTecnicos = 0 })
	sig := admin.pedir(http.MethodGet, fmt.Sprintf("/api/v1/integraciones/argosisp/tecnicos?limit=2&sinVincular=true&cursor=%v", r.cuerpo["nextCursor"]), nil, "X-Iris-Empresa", "1")
	if sig.status != 200 || sig.cuerpo["data"].([]any)[0].(map[string]any)["idExterno"] != fmt.Sprintf("u-l%d", len(data)) {
		t.Fatalf("siguiente: %s", sig.crudo)
	}
}

// Argos caído o conector apagado: problema claro (el panel muestra «Reintentar» y el campo a mano).
func TestSelectorTecnicosArgosSinArgos(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?sinVincular=true", nil).problema(t, 503, "conector_apagado")

	e.argos.con(func() {
		e.argos.version = "1.9.0"
		e.argos.tecnicos = comoMapas(t, []tecnicoArgos064{
			{IDExterno: "u-31", Correo: "f@argos.gt", Nombre: "F", Bodegas: []bodegaArgos064{{ID: 310, EmpresaID: 1, Activa: true, Nombre: "Bodega Oeste"}}},
		})
	})
	admin, _, _ = e.conectado()
	e.argos.con(func() { e.argos.caido = true })
	r := admin.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/tecnicos?sinVincular=true", nil, "X-Iris-Empresa", "1")
	r.problema(t, 503, "argos_no_disponible")
	if r.cuerpo["title"] == nil || r.cuerpo["title"] == "" {
		t.Fatalf("el problema debía traer título: %s", r.crudo)
	}
}
