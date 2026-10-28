package soporte_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Empresa activa (multiempresa de I1): modo sin conector, empresa del ticket al crear,
// asignación dentro de la empresa, aislamiento por HTTP con el middleware real y adopción.

// integracionEnEmpresa: la integración operando en la empresa dada (la de su usuario dueño).
func integracionEnEmpresa(t *testing.T, empresa int64) context.Context {
	t.Helper()
	ctx := integracion(t)
	return contexto.ConEmpresas(ctx, contexto.Empresas{
		Estado: contexto.EmpresaActiva, Activa: empresa, Disponibles: []int64{empresa},
	})
}

func idsDe(p nucleo.Pagina[soporte.Ticket]) []int64 {
	ids := make([]int64, 0, len(p.Data))
	for _, x := range p.Data {
		ids = append(ids, x.ID)
	}
	return ids
}

func TestEmpresaDelTicketSinConector(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)

	// Persona: el ticket se guarda con empresa null; no puede fijar una.
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Sin conector"})
	if tk.EmpresaID != nil {
		t.Fatalf("persona sin conector: %v", *tk.EmpresaID)
	}
	_, err := m.CrearTicket.Ejecutar(como(t, yo), soporte.EntradaCrearTicket{Asunto: "Elige", EmpresaID: ptr(int64(5))})
	problema(t, err, 422, "empresa_no_coincide")

	// Integración y sistema: tampoco eligen una empresa arbitraria.
	tk = crear(t, m, integracion(t), soporte.EntradaCrearTicket{Asunto: "Caída de nodo", Origen: "horus"})
	if tk.EmpresaID != nil || tk.Origen != "horus" {
		t.Fatalf("integración sin conector: %+v %s", tk.EmpresaID, tk.Origen)
	}
	_, err = m.CrearTicket.Ejecutar(integracion(t), soporte.EntradaCrearTicket{Asunto: "Otra", Origen: "horus", EmpresaID: ptr(int64(12))})
	problema(t, err, 422, "empresa_no_coincide")
	sistema := pruebas.ContextoSistema(pruebas.Pool(t))
	tk = crear(t, m, sistema, soporte.EntradaCrearTicket{Asunto: "Del sistema", Origen: "correo"})
	if tk.EmpresaID != nil {
		t.Fatalf("sistema sin conector: %v", *tk.EmpresaID)
	}
	_, err = m.CrearTicket.Ejecutar(sistema, soporte.EntradaCrearTicket{Asunto: "Elige", Origen: "correo", EmpresaID: ptr(int64(5))})
	problema(t, err, 422, "empresa_no_coincide")
}

func TestEmpresaDelTicketConEmpresas(t *testing.T) {
	registrarEmpresas(t, 9, 10)
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)

	// Persona con empresa activa: esa; el cuerpo solo puede repetirla.
	tk := crear(t, m, comoEnEmpresa(t, yo, 9), soporte.EntradaCrearTicket{EmpresaID: ptr(int64(9))})
	if tk.EmpresaID == nil || *tk.EmpresaID != 9 {
		t.Fatalf("empresa activa: %+v", tk.EmpresaID)
	}
	if es := eventosDe(t, tk.ID); len(es) != 1 || es[0].Empresa == nil || *es[0].Empresa != 9 {
		t.Fatalf("el evento lleva la empresa: %+v", es)
	}
	_, err := m.CrearTicket.Ejecutar(comoEnEmpresa(t, yo, 9), soporte.EntradaCrearTicket{Asunto: "Otra", EmpresaID: ptr(int64(10))})
	problema(t, err, 422, "empresa_no_coincide")

	// Integración: la empresa activa (una de las de su usuario dueño), no otra.
	tk = crear(t, m, integracionEnEmpresa(t, 10), soporte.EntradaCrearTicket{Asunto: "Caída", Origen: "horus"})
	if tk.EmpresaID == nil || *tk.EmpresaID != 10 {
		t.Fatalf("integración: %+v", tk.EmpresaID)
	}
	_, err = m.CrearTicket.Ejecutar(integracionEnEmpresa(t, 10), soporte.EntradaCrearTicket{Asunto: "Ajena", Origen: "horus", EmpresaID: ptr(int64(9))})
	problema(t, err, 422, "empresa_no_coincide")

	// Sistema: con empresas registradas la indica, y debe estar registrada.
	sistema := pruebas.ContextoSistema(pruebas.Pool(t))
	_, err = m.CrearTicket.Ejecutar(sistema, soporte.EntradaCrearTicket{Asunto: "Sin empresa", Origen: "correo"})
	problema(t, err, 422, "empresa_requerida")
	_, err = m.CrearTicket.Ejecutar(sistema, soporte.EntradaCrearTicket{Asunto: "Inventada", Origen: "correo", EmpresaID: ptr(int64(99))})
	problema(t, err, 422, "empresa_no_registrada")
	tk = crear(t, m, sistema, soporte.EntradaCrearTicket{Asunto: "De la 10", Origen: "correo", EmpresaID: ptr(int64(10))})
	if tk.EmpresaID == nil || *tk.EmpresaID != 10 {
		t.Fatalf("sistema: %+v", tk.EmpresaID)
	}

	// Por elegir o sin empresa asignada: auditoria.Caso corta antes de la lógica.
	porElegir := contexto.ConEmpresas(como(t, yo), contexto.Empresas{Estado: contexto.EmpresaPorElegir, Disponibles: []int64{9, 10}})
	_, err = m.CrearTicket.Ejecutar(porElegir, soporte.EntradaCrearTicket{Asunto: "¿Cuál?"})
	problema(t, err, 422, "empresa_requerida")
	_, err = m.ListarTickets.Ejecutar(porElegir, soporte.EntradaListarTickets{})
	problema(t, err, 422, "empresa_requerida")
}

func TestSinConectorSoloVeLoNull(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	nulo := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Sin empresa " + pruebas.Unico()})
	// Un ticket con empresa (quedó de antes o lo creó otra empresa) no existe sin conector.
	conEmpresa := crear(t, m, comoEnEmpresa(t, yo, 33), soporte.EntradaCrearTicket{Asunto: "De la 33"})

	sin := como(t, yo)
	if _, err := m.VerTicket.Ejecutar(sin, soporte.EntradaVerTicket{ID: nulo.ID}); err != nil {
		t.Fatal(err)
	}
	_, err := m.VerTicket.Ejecutar(sin, soporte.EntradaVerTicket{ID: conEmpresa.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = m.ResponderTicket.Ejecutar(sin, soporte.EntradaResponderTicket{ID: conEmpresa.ID, Cuerpo: "Hola"})
	problema(t, err, 404, "no_encontrado")
	_, err = m.ListarMensajes.Ejecutar(sin, soporte.EntradaListarMensajes{ID: conEmpresa.ID})
	problema(t, err, 404, "no_encontrado")

	pag, err := m.ListarTickets.Ejecutar(sin, soporte.EntradaListarTickets{ParamsPagina: nucleo.ParamsPagina{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsDe(pag); !slices.Contains(ids, nulo.ID) || slices.Contains(ids, conEmpresa.ID) {
		t.Fatalf("sin conector la lista trae solo empresa_id null: %v", ids)
	}
	for _, x := range pag.Data {
		if x.EmpresaID != nil {
			t.Fatalf("ticket con empresa en modo sin conector: %+v", x)
		}
	}

	// Y desde la empresa 33, al revés.
	pag, err = m.ListarTickets.Ejecutar(comoEnEmpresa(t, yo, 33), soporte.EntradaListarTickets{ParamsPagina: nucleo.ParamsPagina{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsDe(pag); slices.Contains(ids, nulo.ID) || !slices.Contains(ids, conEmpresa.ID) {
		t.Fatalf("la empresa 33 ve solo lo suyo: %v", ids)
	}
	_, err = m.VerTicket.Ejecutar(comoEnEmpresa(t, yo, 33), soporte.EntradaVerTicket{ID: nulo.ID})
	problema(t, err, 404, "no_encontrado")
}

func TestAsignarSoloDentroDeLaEmpresa(t *testing.T) {
	registrarEmpresas(t, 21, 22)
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	asignarEmpresa(t, yo, 21)
	tk := crear(t, m, comoEnEmpresa(t, yo, 21), soporte.EntradaCrearTicket{Asunto: "En la 21"})
	asignar := func(usuario int64) (soporte.Ticket, error) {
		actual, err := m.VerTicket.Ejecutar(comoEnEmpresa(t, yo, 21), soporte.EntradaVerTicket{ID: tk.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m.AsignarTicket.Ejecutar(comoEnEmpresa(t, yo, 21), soporte.EntradaAsignarTicket{ID: tk.ID, Version: actual.Version, UsuarioID: &usuario})
	}

	deOtra := agente(t)
	asignarEmpresa(t, deOtra, 22)
	_, err := asignar(deOtra)
	problema(t, err, 422, "asignado_fuera_de_empresa")
	_, err = asignar(agente(t)) // sin ninguna empresa asignada
	problema(t, err, 422, "asignado_fuera_de_empresa")

	deLaMisma := agente(t)
	asignarEmpresa(t, deLaMisma, 21)
	if a, err := asignar(deLaMisma); err != nil || a.AsignadoA == nil || *a.AsignadoA != deLaMisma {
		t.Fatalf("de la misma empresa: %v", err)
	}
	// empresas.todas global (admin) opera en todas.
	if _, err := asignar(usuarioCon(t, identidad.AsignacionRol{Rol: "admin"})); err != nil {
		t.Fatalf("admin global: %v", err)
	}
}

func TestHTTPAislamientoEntreEmpresas(t *testing.T) {
	registrarEmpresas(t, 41, 42)
	base := nuevoServidor(t)
	a := entrar(t, base, []int64{41}, "soporte")
	b := entrar(t, base, []int64{42}, "soporte")

	r := a.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Solo de la 41", "descripcion": "Sin señal"})
	if r.status != http.StatusCreated || r.cuerpo["empresaId"] != float64(41) {
		t.Fatalf("crear en la 41: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	version := r.cuerpo["version"]
	ruta := fmt.Sprintf("/api/v1/tickets/%d", id)

	// B no lo ve ni lo toca: el mismo 404 que si no existiera.
	for _, sub := range []string{"", "/mensajes", "/eventos"} {
		b.pedir(http.MethodGet, ruta+sub, nil).problema(t, http.StatusNotFound, "no_encontrado")
	}
	acciones := map[string]map[string]any{
		"asignar":         {"version": version, "usuarioId": b.id},
		"responder":       {"cuerpo": "Hola"},
		"nota-interna":    {"cuerpo": "Nota"},
		"escalar":         {"version": version, "prioridad": "critica", "motivo": "Urgente"},
		"poner-en-espera": {"version": version, "motivo": "Esperando"},
		"resolver":        {"version": version, "solucion": "Reinicio del equipo"},
		"cerrar":          {"version": version, "motivo": "Duplicado"},
	}
	for accion, cuerpo := range acciones {
		b.pedir(http.MethodPost, ruta+"/"+accion, cuerpo).problema(t, http.StatusNotFound, "no_encontrado")
	}
	// Fusionar un ticket de B en el de A tampoco: el destino no existe para B.
	rb := b.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "De la 42"})
	if rb.status != http.StatusCreated || rb.cuerpo["empresaId"] != float64(42) {
		t.Fatalf("crear en la 42: %d %s", rb.status, rb.crudo)
	}
	idB := int64(rb.cuerpo["id"].(float64))
	b.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tickets/%d/fusionar", idB), map[string]any{"version": rb.cuerpo["version"], "destinoId": id}).
		problema(t, http.StatusUnprocessableEntity, "fusion_invalida")

	// La lista de B no lo trae; la de A sí (y no trae el de B).
	listar := func(c *clienteHTTP) []int64 {
		r := c.pedir(http.MethodGet, "/api/v1/tickets?limit=200", nil)
		if r.status != http.StatusOK {
			t.Fatalf("lista: %d %s", r.status, r.crudo)
		}
		var ids []int64
		for _, x := range r.cuerpo["data"].([]any) {
			ids = append(ids, int64(x.(map[string]any)["id"].(float64)))
		}
		return ids
	}
	if ids := listar(b); slices.Contains(ids, id) || !slices.Contains(ids, idB) {
		t.Fatalf("lista de B: %v", ids)
	}
	if ids := listar(a); !slices.Contains(ids, id) || slices.Contains(ids, idB) {
		t.Fatalf("lista de A: %v", ids)
	}

	// B no puede pedir la empresa de A ni crear en ella.
	b.pedir(http.MethodGet, ruta, nil, "X-Iris-Empresa", "41").problema(t, http.StatusForbidden, "empresa_no_permitida")
	b.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Colado", "empresaId": 41}).
		problema(t, http.StatusUnprocessableEntity, "empresa_no_coincide")

	// Nada cambió en el ticket de A.
	if r := a.pedir(http.MethodGet, ruta, nil); r.status != http.StatusOK || r.cuerpo["version"] != version ||
		r.cuerpo["estado"] != "nuevo" {
		t.Fatalf("el ticket de A quedó intacto: %d %s", r.status, r.crudo)
	}
	if r := a.pedir(http.MethodGet, ruta+"/mensajes", nil); r.status != http.StatusOK || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("mensajes de A: %d %s", r.status, r.crudo)
	}
}

func TestHTTPSinConectorYAdopcion(t *testing.T) {
	pool := pruebas.Pool(t)
	base := nuevoServidor(t)
	s := entrar(t, base, nil, "soporte")

	// Sin empresas registradas: se crea y se lista con empresa null.
	r := s.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Antes del conector", "descripcion": "Sin señal"})
	if r.status != http.StatusCreated || r.cuerpo["empresaId"] != nil {
		t.Fatalf("crear sin conector: %d %s", r.status, r.crudo)
	}
	id := int64(r.cuerpo["id"].(float64))
	ruta := fmt.Sprintf("/api/v1/tickets/%d", id)
	if l := s.pedir(http.MethodGet, "/api/v1/tickets?limit=200", nil); l.status != http.StatusOK ||
		!slices.ContainsFunc(l.cuerpo["data"].([]any), func(x any) bool { return x.(map[string]any)["id"] == float64(id) }) {
		t.Fatalf("lista sin conector: %d %s", l.status, l.crudo)
	}
	s.pedir(http.MethodPost, "/api/v1/tickets", map[string]any{"asunto": "Con empresa", "empresaId": 51}).
		problema(t, http.StatusUnprocessableEntity, "empresa_no_coincide")

	// Se registra la empresa: lo guardado sin empresa deja de verse.
	registrarEmpresas(t, 51)
	asignarEmpresa(t, s.id, 51)
	s.pedir(http.MethodGet, ruta, nil).problema(t, http.StatusNotFound, "no_encontrado")

	// Un admin global lo adopta: el ticket, sus mensajes y su historial pasan a la empresa.
	admin := entrar(t, base, nil, "admin")
	ad := admin.pedir(http.MethodPost, "/api/v1/empresas/51/adoptar-sin-empresa", nil)
	filas, _ := ad.cuerpo["filas"].(map[string]any)
	if ad.status != http.StatusOK || filas["soporte.tickets"] == nil || filas["soporte.tickets"].(float64) < 1 ||
		filas["soporte.ticket_mensajes"].(float64) < 1 || filas["soporte.ticket_eventos"].(float64) < 1 {
		t.Fatalf("adoptar: %d %s", ad.status, ad.crudo)
	}
	if r := s.pedir(http.MethodGet, ruta, nil); r.status != http.StatusOK || r.cuerpo["empresaId"] != float64(51) {
		t.Fatalf("tras adoptar: %d %s", r.status, r.crudo)
	}
	if r := s.pedir(http.MethodGet, ruta+"/mensajes", nil); r.status != http.StatusOK || len(r.cuerpo["data"].([]any)) != 1 {
		t.Fatalf("mensajes tras adoptar: %d %s", r.status, r.crudo)
	}
	for _, tabla := range []string{"soporte.ticket_mensajes", "soporte.ticket_eventos"} {
		var nulos int
		if err := pool.QueryRow(context.Background(),
			"SELECT count(*) FROM "+tabla+" WHERE ticket_id = $1 AND empresa_id IS DISTINCT FROM 51", id).Scan(&nulos); err != nil || nulos != 0 {
			t.Fatalf("%s sin adoptar: %d %v", tabla, nulos, err)
		}
	}
	// Las categorías y los SLA globales (empresa_id null = de todas) no se adoptan.
	var globales int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM soporte.slas WHERE empresa_id IS NULL").Scan(&globales); err != nil || globales == 0 {
		t.Fatalf("SLA globales: %d %v", globales, err)
	}
}
