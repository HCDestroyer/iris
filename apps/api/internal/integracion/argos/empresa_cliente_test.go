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

// Revisión de seguridad de la PR #52.

// Una lista de servicios vacía por el alcance del token (sin global en Argos) no es «sin
// servicios»: ese cliente queda fuera de la búsqueda, la ficha y el portal.
func TestListaVaciaPorAlcanceQuedaFuera(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado()
	e.exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2')`)
	e.sembrarComoArgos13(91, "Gaby Ajena", 2)
	e.argos.con(func() { e.argos.alcance = map[int64]bool{1: true} })
	uno := e.usuarioEn([]int64{1}, "soporte")
	if b := uno.pedir(http.MethodGet, "/api/v1/clientes?q=gaby", nil); b.status != 200 || len(b.cuerpo["data"].([]any)) != 0 {
		t.Fatalf("búsqueda: %d %s", b.status, b.crudo)
	}
	uno.pedir(http.MethodGet, "/api/v1/clientes/91", nil).problema(t, 404, "no_encontrado")
	if n := e.contar(`SELECT count(*) FROM integracion.argos_clientes WHERE id = 91 AND sin_servicios`); n != 0 {
		t.Fatal("una lista parcial vacía no marca sin_servicios")
	}
	if v, err := e.mod.ValidarCliente(pruebas.ContextoSistema(e.pool), 91); err != nil || !v.Validado || v.SinServicios || len(v.Empresas) != 0 {
		t.Fatalf("portal: %+v %v", v, err)
	}
}

// La ficha solo muestra los servicios de la empresa activa (con empresas.todas, todos) y pide el
// estado de cuenta de esa empresa. Sin servicios: la ficha se ve, el estado de cuenta solo con
// empresas.todas. Un servicio sin empresa no se atribuye a nadie.
func TestFichaPorEmpresaYSinServicios(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado()
	e.exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2')`)
	e.sembrarComoArgos13(93, "Hugo Mixto", 2, 1)
	e.sembrarComoArgos13(94, "Hugo Nuevo")
	e.sembrarComoArgos13(95, "Hugo Sin Empresa", 0)
	uno := e.usuarioEn([]int64{1}, "soporte")

	f := uno.pedir(http.MethodGet, "/api/v1/clientes/93", nil)
	servicios, _ := f.cuerpo["servicios"].([]any)
	if f.status != 200 || len(servicios) != 1 || servicios[0].(map[string]any)["empresaId"] != float64(1) {
		t.Fatalf("solo los servicios de la empresa activa: %d %s", f.status, f.crudo)
	}
	var pedido string
	e.argos.con(func() { pedido = e.argos.estadoPedido })
	if f.cuerpo["estadoCuenta"] == nil || pedido != "1" {
		t.Fatalf("estado de cuenta de la empresa activa: %q %s", pedido, f.crudo)
	}
	admin := e.usuario("admin")
	if f := admin.pedir(http.MethodGet, "/api/v1/clientes/93", nil, "X-Iris-Empresa", "1"); f.status != 200 || len(f.cuerpo["servicios"].([]any)) != 2 {
		t.Fatalf("con empresas.todas, todos los servicios: %d %s", f.status, f.crudo)
	}

	f = uno.pedir(http.MethodGet, "/api/v1/clientes/94", nil)
	if f.status != 200 || f.cuerpo["estadoCuenta"] != nil || f.cuerpo["estadoCuentaError"] != "sin_permiso" {
		t.Fatalf("sin servicios: ficha sí, estado de cuenta no: %d %s", f.status, f.crudo)
	}
	if f := admin.pedir(http.MethodGet, "/api/v1/clientes/94", nil, "X-Iris-Empresa", "1"); f.status != 200 || f.cuerpo["estadoCuenta"] == nil {
		t.Fatalf("sin servicios, con empresas.todas: %d %s", f.status, f.crudo)
	}

	uno.pedir(http.MethodGet, "/api/v1/clientes/95", nil).problema(t, 404, "no_encontrado")
	admin.pedir(http.MethodGet, "/api/v1/clientes/95", nil, "X-Iris-Empresa", "1").problema(t, 404, "no_encontrado")
	// Con un servicio en la empresa 1 y otro sin empresa: se ve, y el segundo queda en 0.
	e.sembrarComoArgos13(97, "Hugo Medio", 1, 0)
	if f := uno.pedir(http.MethodGet, "/api/v1/clientes/97", nil); f.status != 200 || len(f.cuerpo["servicios"].([]any)) != 1 {
		t.Fatalf("cliente con un servicio sin empresa: %d %s", f.status, f.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_servicios WHERE cliente_id = 97 AND empresa_id = 0`); n != 1 {
		t.Fatal("el servicio sin empresa no hereda la del cliente")
	}
	e.argos.con(func() { e.argos.ips["10.0.0.95"] = map[string]any{"cliente_id": 95, "servicio_id": 950, "empresa_id": 0} })
	uno.pedir(http.MethodGet, "/api/v1/clientes/resolver-ip?ip=10.0.0.95", nil).problema(t, 404, "no_encontrado")
}

// Un cliente recién creado por un alta desde Iris, aún sin servicio, solo se ve en la empresa
// del prospecto (pista local).
func TestPistaDeAltaSinServicio(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado()
	e.exec(`INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2')`)
	e.sembrarComoArgos13(96, "Iván Recién Ganado")
	e.exec(`INSERT INTO integracion.pendientes (operacion, idempotency_key, entidad, entidad_id, metodo, ruta, empresa_id, estado, respuesta)
		VALUES ('crear_cliente', 'iris:cliente:e2e-' || $1, 'prospecto', $1, 'POST', '/clientes', 2, 'hecho', '{"id": 96}')`, pruebas.Unico())
	e.usuarioEn([]int64{1}, "soporte").pedir(http.MethodGet, "/api/v1/clientes/96", nil).problema(t, 404, "no_encontrado")
	if f := e.usuarioEn([]int64{2}, "soporte").pedir(http.MethodGet, "/api/v1/clientes/96", nil); f.status != 200 {
		t.Fatalf("en la empresa del prospecto: %d %s", f.status, f.crudo)
	}
}

// La búsqueda verifica a lo sumo TopeVerificaciones clientes desconocidos; los demás quedan fuera.
func TestTopeDeVerificacionesPorBusqueda(t *testing.T) {
	e := nuevoEntorno(t)
	e.conectado()
	for i := int64(0); i < 25; i++ {
		e.sembrarComoArgos13(700+i, fmt.Sprintf("Zeta %02d", i), 1)
	}
	uno := e.usuarioEn([]int64{1}, "soporte")
	antes := e.argos.llamadasA("/api/v1/servicios")
	b := uno.pedir(http.MethodGet, "/api/v1/clientes?q=zeta&limit=50", nil)
	llamadas := e.argos.llamadasA("/api/v1/servicios") - antes
	if b.status != 200 || len(b.cuerpo["data"].([]any)) != 20 || llamadas != 20 {
		t.Fatalf("tope de 20: %d resultados, %d llamadas: %s", len(b.cuerpo["data"].([]any)), llamadas, b.crudo)
	}
	// Caché: la misma búsqueda no vuelve a verificar a los ya verificados.
	antes = e.argos.llamadasA("/api/v1/servicios")
	uno.pedir(http.MethodGet, "/api/v1/clientes?q=zeta&limit=50", nil)
	if n := e.argos.llamadasA("/api/v1/servicios") - antes; n != 5 {
		t.Fatalf("con caché solo se verifican los 5 restantes: %d", n)
	}
}
