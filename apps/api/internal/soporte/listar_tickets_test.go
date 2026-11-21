package soporte_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Filtro de cliente de la bandeja (contrato con Argos, A7): Argos pide
// GET /tickets?cliente_id=; con el nombre equivocado Iris ignoraba el filtro y devolvía los
// tickets de todos los clientes de la empresa.

// conToken es un cliente HTTP que entra con Authorization: Bearer (sin cookie ni CSRF).
func conToken(t *testing.T, base, valor string) *tokenHTTP {
	return &tokenHTTP{c: &clienteHTTP{t: t, base: base, http: &http.Client{}}, valor: valor}
}

type tokenHTTP struct {
	c     *clienteHTTP
	valor string
}

func (k *tokenHTTP) get(ruta string) respuesta {
	return k.c.pedir(http.MethodGet, ruta, nil, "Authorization", "Bearer "+k.valor)
}

// tokenIntegracionArgos crea el usuario de integración con el rol integracion_argos (§1.2) y
// su token.
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

// tokenPersona crea un token de API de un admin, acotado a los permisos dados.
func tokenPersona(t *testing.T, permisos ...nucleo.Permiso) string {
	t.Helper()
	id := usuarioCon(t, identidad.AsignacionRol{Rol: "admin"})
	tk, err := ident.CrearToken.Ejecutar(pruebas.ContextoUsuario(pruebas.Pool(t), id, nucleo.PermTokensCrear),
		identidad.EntradaCrearToken{Nombre: "script", Permisos: permisos})
	if err != nil {
		t.Fatal(err)
	}
	return tk.Valor
}

func idsDeRespuesta(t *testing.T, r respuesta) []int64 {
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

func TestHTTPListarTicketsFiltroCliente(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	clienteA := 770000 + time.Now().UnixNano()%100000
	clienteB := clienteA + 1
	servicioA := clienteA * 10
	var deA []int64
	for range 2 {
		deA = append(deA, crear(t, m, como(t, yo), soporte.EntradaCrearTicket{ClienteID: &clienteA, ServicioID: &servicioA}).ID)
	}
	slices.Sort(deA)
	deB := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{ClienteID: &clienteB}).ID

	base := nuevoServidor(t)
	panel := entrar(t, base, nil, "soporte")
	argos := conToken(t, base, tokenIntegracionArgos(t))
	todo := conToken(t, base, tokenPersona(t, nucleo.PermTicketsVer, nucleo.PermTicketsListarTodos))
	script := conToken(t, base, tokenPersona(t, nucleo.PermTicketsVer))
	lista := "/api/v1/tickets?limit=200"
	conA := fmt.Sprintf("%s&cliente_id=%d", lista, clienteA)

	// cliente_id (el nombre de Argos) filtra igual que clienteId, en el panel y por token.
	if ids := idsDeRespuesta(t, panel.pedir(http.MethodGet, conA, nil)); !slices.Equal(ids, deA) {
		t.Fatalf("panel con cliente_id: %v, esperaba %v", ids, deA)
	}
	if ids := idsDeRespuesta(t, argos.get(conA)); !slices.Equal(ids, deA) {
		t.Fatalf("Argos con cliente_id: %v, esperaba %v", ids, deA)
	}
	if ids := idsDeRespuesta(t, argos.get(fmt.Sprintf("%s&clienteId=%d", lista, clienteA))); !slices.Equal(ids, deA) {
		t.Fatalf("Argos con clienteId: %v", ids)
	}
	// Los dos con el mismo valor valen; con valores distintos, 422.
	if ids := idsDeRespuesta(t, argos.get(fmt.Sprintf("%s&clienteId=%d", conA, clienteA))); !slices.Equal(ids, deA) {
		t.Fatalf("los dos iguales: %v", ids)
	}
	r := argos.get(fmt.Sprintf("%s&clienteId=%d", conA, clienteB))
	r.problema(t, http.StatusUnprocessableEntity, "validacion")
	if errs, _ := r.cuerpo["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "cliente_id" {
		t.Fatalf("el error nombra cliente_id: %s", r.crudo)
	}
	panel.pedir(http.MethodGet, fmt.Sprintf("%s&clienteId=%d", conA, clienteB), nil).problema(t, http.StatusUnprocessableEntity, "validacion")
	// servicio_id es alias de servicioId (con el cliente: el token lo exige).
	if ids := idsDeRespuesta(t, argos.get(fmt.Sprintf("%s&servicio_id=%d", conA, servicioA))); !slices.Equal(ids, deA) {
		t.Fatalf("servicio_id: %v", ids)
	}
	argos.get(fmt.Sprintf("%s&servicio_id=%d&servicioId=%d", conA, servicioA, servicioA+1)).
		problema(t, http.StatusUnprocessableEntity, "validacion")

	// Un token sin filtro de cliente: 422, nunca la empresa entera (integración y API de persona).
	argos.get(lista).problema(t, http.StatusUnprocessableEntity, "filtro_cliente_requerido")
	argos.get(fmt.Sprintf("%s&servicio_id=%d", lista, servicioA)).problema(t, http.StatusUnprocessableEntity, "filtro_cliente_requerido")
	script.get(lista).problema(t, http.StatusUnprocessableEntity, "filtro_cliente_requerido")
	// Con tickets.listar_todos, el token lista todo.
	if ids := idsDeRespuesta(t, todo.get(lista)); !slices.Contains(ids, deB) || !slices.Contains(ids, deA[0]) {
		t.Fatalf("token con listar_todos: %v", ids)
	}

	// El panel no cambia: sin filtro ve a los dos clientes.
	if ids := idsDeRespuesta(t, panel.pedir(http.MethodGet, lista, nil)); !slices.Contains(ids, deB) || !slices.Contains(ids, deA[0]) {
		t.Fatalf("panel sin filtro: %v", ids)
	}

	// Un parámetro desconocido (un filtro mal escrito) responde 422 y no amplía el resultado.
	for _, c := range []func(string) respuesta{
		argos.get,
		func(ruta string) respuesta { return panel.pedir(http.MethodGet, ruta, nil) },
	} {
		r := c(fmt.Sprintf("%s&clienteID=%d", lista, clienteA))
		r.problema(t, http.StatusUnprocessableEntity, "validacion")
		if errs, _ := r.cuerpo["errors"].([]any); len(errs) != 1 || errs[0].(map[string]any)["field"] != "clienteID" ||
			errs[0].(map[string]any)["rule"] != "desconocido" {
			t.Fatalf("el error nombra el parámetro: %s", r.crudo)
		}
	}
	argos.get(conA+"&cuadrilla=3").problema(t, http.StatusUnprocessableEntity, "validacion")
}
