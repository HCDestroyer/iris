package argos_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// sembrarComoArgos13 siembra un cliente con la forma de Argos 1.3.0: sin empresa (la tienen sus
// servicios; multiempresa.md de Argos). empresas: la de cada servicio (vacío = sin servicios).
func (e *entorno) sembrarComoArgos13(id int64, nombre string, empresas ...int64) {
	e.sembrarCliente(id, nombre)
	e.argos.con(func() {
		delete(e.argos.clientes[id], "empresaId")
		var servicios []map[string]any
		for i, emp := range empresas {
			servicios = append(servicios, map[string]any{"id": id*10 + int64(i), "empresa_id": emp, "cliente_id": id, "estado": "activo"})
		}
		e.argos.servicios[id] = servicios
	})
}

// El cliente de Argos no tiene empresa: Iris lo muestra en las empresas de sus servicios (o en
// todas si no tiene servicios), en la búsqueda (verificando en Argos los que no conoce), la ficha
// y el refresco por webhook, que ya no deja empresa_id = 0.
func TestEmpresaDelClienteSegunSusServicios(t *testing.T) {
	e := nuevoEntorno(t)
	_, conector, secreto := e.conectado()
	e.exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2')`)
	e.sembrarComoArgos13(81, "Beto Norte", 1)
	e.sembrarComoArgos13(82, "Beto Sur", 2)
	e.sembrarComoArgos13(83, "Beto Nuevo") // sin servicios
	e.sembrarComoArgos13(84, "Beto Mixto", 2, 1)
	uno := e.usuarioEn([]int64{1}, "soporte")

	// Búsqueda: Iris no los conoce; verifica sus servicios en Argos y descarta el de la empresa 2.
	b := uno.pedir(http.MethodGet, "/api/v1/clientes?q=beto", nil)
	ids := map[float64]bool{}
	for _, c := range b.cuerpo["data"].([]any) {
		ids[c.(map[string]any)["id"].(float64)] = true
	}
	if b.status != 200 || len(ids) != 3 || !ids[81] || !ids[83] || !ids[84] || ids[82] {
		t.Fatalf("búsqueda por las empresas de los servicios: %d %s", b.status, b.crudo)
	}
	var pedida string
	e.argos.con(func() { pedida = e.argos.empresaPedida })
	if pedida != "1" {
		t.Fatalf("la empresa activa va a Argos en X-Empresa-Id: %q", pedida)
	}

	// Ficha: la de la empresa 2 es 404; la sin servicios y la mixta, 200 con sus empresas.
	f := uno.pedir(http.MethodGet, "/api/v1/clientes/81", nil)
	if f.status != 200 || fmt.Sprint(f.cuerpo["cliente"].(map[string]any)["empresas"]) != "[1]" {
		t.Fatalf("ficha de la empresa 1: %d %s", f.status, f.crudo)
	}
	uno.pedir(http.MethodGet, "/api/v1/clientes/82", nil).problema(t, 404, "no_encontrado")
	if f := uno.pedir(http.MethodGet, "/api/v1/clientes/83", nil); f.status != 200 {
		t.Fatalf("sin servicios, lo ven todas: %d %s", f.status, f.crudo)
	}
	if f := uno.pedir(http.MethodGet, "/api/v1/clientes/84", nil); f.status != 200 ||
		fmt.Sprint(f.cuerpo["cliente"].(map[string]any)["empresas"]) != "[1 2]" {
		t.Fatalf("mixto: %d %s", f.status, f.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 81 AND empresas = '{1}' AND empresa_id = 1`); n != 1 {
		t.Fatal("la instantánea guarda las empresas de los servicios")
	}

	// Ya conocido: la búsqueda usa la instantánea (sin volver a leer sus servicios).
	antes := e.argos.llamadasA("/api/v1/servicios")
	uno.pedir(http.MethodGet, "/api/v1/clientes?q=beto%20norte", nil)
	if e.argos.llamadasA("/api/v1/servicios") != antes {
		t.Fatal("un cliente en la instantánea no se vuelve a verificar en Argos")
	}

	// Refresco por webhook: cliente.actualizado (con empresas[] como manda Argos) deja las
	// empresas de sus servicios, nunca 0.
	e.argos.con(func() { e.argos.clientes[81]["nombre"] = "Beto Norte Ruiz" })
	ent := e.entranteDe(e.enviar(conector, secreto, "wh-emp-"+pruebas.Unico(), "cliente.actualizado",
		map[string]any{"cliente_id": 81, "campos": []string{"nombre"}, "empresas": []int64{1}, "empresa_id": 1}, time.Now()))
	if r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent); err != nil || r.Resultado != "aplicado" {
		t.Fatalf("refresco: %+v %v", r, err)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 81 AND empresas = '{1}' AND empresa_id = 1
		AND nombre = 'Beto Norte Ruiz'`); n != 1 {
		t.Fatal("el refresco no deja empresa_id = 0")
	}
	if f := uno.pedir(http.MethodGet, "/api/v1/clientes/81", nil); f.status != 200 {
		t.Fatalf("tras el refresco sigue visible: %d %s", f.status, f.crudo)
	}
}
