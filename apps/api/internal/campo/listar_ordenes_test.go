package campo_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Filtro de cliente del tablero de órdenes (contrato con Argos, A7): Argos lee las órdenes del
// cliente para su ficha con cliente_id; con el nombre equivocado Iris ignoraba el filtro y
// devolvía las órdenes de todos los clientes de la empresa.

type tokenHTTP struct {
	c     *clienteHTTP
	valor string
}

func conToken(t *testing.T, base, valor string) *tokenHTTP {
	return &tokenHTTP{c: &clienteHTTP{t: t, base: base, http: &http.Client{}}, valor: valor}
}

func (k *tokenHTTP) get(ruta string) respuesta {
	return k.c.pedir(http.MethodGet, ruta, nil, "Authorization", "Bearer "+k.valor)
}

// tokenIntegracionArgos crea el usuario de integración con el rol integracion_argos (§1.2) y su
// token.
func tokenIntegracionArgos(t *testing.T) string {
	t.Helper()
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	u, err := ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{
		Nombre: "Argos", Correo: pruebas.Correo("argos"), Tipo: "integracion",
		Roles: []identidad.AsignacionRol{{Rol: "integracion_argos"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := ident.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{UsuarioID: u.ID, Nombre: "argos"})
	if err != nil {
		t.Fatal(err)
	}
	return tk.Valor
}

// tokenPersona crea un token de API del usuario (con los roles dados), acotado a los permisos.
func tokenPersona(t *testing.T, rol string, permisos ...nucleo.Permiso) string {
	t.Helper()
	id := usuarioCon(t, identidad.AsignacionRol{Rol: rol})
	if rol == "tecnico" {
		conAgenda(t, id)
	}
	tk, err := ident.CrearToken.Ejecutar(pruebas.ContextoUsuario(pruebas.Pool(t), id, nucleo.PermTokensCrear),
		identidad.EntradaCrearToken{Nombre: "script", Permisos: permisos})
	if err != nil {
		t.Fatal(err)
	}
	return tk.Valor
}

func idsDeLista(t *testing.T, r respuesta) []int64 {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("lista: %d %s", r.status, r.crudo)
	}
	var ids []int64
	for _, x := range r.cuerpo["data"].([]any) {
		ids = append(ids, int64(x.(map[string]any)["id"].(float64)))
	}
	slices.Sort(ids)
	return ids
}

func TestHTTPListarOrdenesFiltroCliente(t *testing.T) {
	e := nuevoEntorno()
	asegurarZona(t, 3)
	ctx := como(t, despacho(t), "despacho")
	clienteA := 660000 + time.Now().UnixNano()%100000
	clienteB := clienteA + 1
	nueva := func(cliente int64) int64 {
		return deber(e.m.CrearOrden.Ejecutar(ctx, campo.EntradaCrearOrden{
			Tipo: "visita", Direccion: ptr("Zona 10"), ZonaID: ptr(int64(3)), ClienteID: &cliente, Descripcion: "Prueba A7",
		}))(t).ID
	}
	deA := []int64{nueva(clienteA), nueva(clienteA)}
	slices.Sort(deA)
	deB := nueva(clienteB)

	base := nuevoServidor(t)
	panel := entrar(t, base, "despacho")
	argos := conToken(t, base, tokenIntegracionArgos(t))
	todo := conToken(t, base, tokenPersona(t, "admin", nucleo.PermOrdenesVer, nucleo.PermOrdenesListarTodos))
	script := conToken(t, base, tokenPersona(t, "admin", nucleo.PermOrdenesVer))
	tec := conToken(t, base, tokenPersona(t, "tecnico"))
	lista := "/api/v1/ordenes?limit=200"
	conA := fmt.Sprintf("%s&cliente_id=%d", lista, clienteA)

	// cliente_id filtra igual que clienteId, en el panel y por token.
	if ids := idsDeLista(t, panel.pedir(http.MethodGet, conA, nil)); !slices.Equal(ids, deA) {
		t.Fatalf("panel con cliente_id: %v, esperaba %v", ids, deA)
	}
	if ids := idsDeLista(t, argos.get(conA)); !slices.Equal(ids, deA) {
		t.Fatalf("Argos con cliente_id: %v, esperaba %v", ids, deA)
	}
	if ids := idsDeLista(t, argos.get(fmt.Sprintf("%s&clienteId=%d", conA, clienteA))); !slices.Equal(ids, deA) {
		t.Fatalf("los dos iguales: %v", ids)
	}
	r := argos.get(fmt.Sprintf("%s&clienteId=%d", conA, clienteB))
	r.problema(t, http.StatusUnprocessableEntity, "validacion")
	if errs, _ := r.cuerpo["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "cliente_id" {
		t.Fatalf("el error nombra cliente_id: %s", r.crudo)
	}
	argos.get(conA+"&servicio_id=5&servicioId=6").problema(t, http.StatusUnprocessableEntity, "validacion")
	if ids := idsDeLista(t, argos.get(conA+"&servicio_id=5&servicioId=5")); len(ids) != 0 {
		t.Fatalf("servicio_id filtra (ninguna con ese servicio): %v", ids)
	}

	// Un token con ordenes.ver sin filtro de cliente: 422, nunca la empresa entera.
	argos.get(lista).problema(t, http.StatusUnprocessableEntity, "filtro_cliente_requerido")
	argos.get(lista+"&servicio_id=5").problema(t, http.StatusUnprocessableEntity, "filtro_cliente_requerido")
	script.get(lista).problema(t, http.StatusUnprocessableEntity, "filtro_cliente_requerido")
	if ids := idsDeLista(t, todo.get(lista)); !slices.Contains(ids, deB) || !slices.Contains(ids, deA[0]) {
		t.Fatalf("token con listar_todos: %v", ids)
	}
	// El técnico (ordenes.ver_propias) ya ve solo lo suyo: su token no necesita el filtro.
	if r := tec.get(lista); r.status != http.StatusOK {
		t.Fatalf("técnico por token: %d %s", r.status, r.crudo)
	}

	// El panel no cambia.
	if ids := idsDeLista(t, panel.pedir(http.MethodGet, lista, nil)); !slices.Contains(ids, deB) || !slices.Contains(ids, deA[0]) {
		t.Fatalf("panel sin filtro: %v", ids)
	}

	// Parámetro desconocido: 422 en el panel y por token.
	for _, c := range []func(string) respuesta{
		argos.get,
		func(ruta string) respuesta { return panel.pedir(http.MethodGet, ruta, nil) },
		tec.get,
	} {
		r := c(fmt.Sprintf("%s&clienteID=%d", lista, clienteA))
		r.problema(t, http.StatusUnprocessableEntity, "validacion")
		if errs, _ := r.cuerpo["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "clienteID" ||
			errs[0].(map[string]any)["rule"] != "desconocido" {
			t.Fatalf("el error nombra el parámetro: %s", r.crudo)
		}
	}
}
