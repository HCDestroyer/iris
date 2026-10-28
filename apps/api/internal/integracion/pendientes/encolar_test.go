package pendientes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
)

// encolar corre pendientes.Encolar en un caso de uso de prueba (actor sistema), como lo hace
// comercial o campo en su transacción.
var casoEncolar = auditoria.Nuevo(auditoria.Caso[pendientes.Accion, pendientes.Registro]{
	Accion: "prueba.encolar", Acceso: auditoria.Autenticado,
	Logica: func(ctx context.Context, t *auditoria.Tx, a pendientes.Accion) (pendientes.Registro, error) {
		r, _, err := pendientes.Encolar(ctx, t, a, nil)
		return r, err
	},
})

func (e *entorno) encolar(a pendientes.Accion) pendientes.Registro {
	e.t.Helper()
	if a.Entidad == "" {
		a.Entidad, a.EntidadID = "prueba", "1"
	}
	empresa := int64(1)
	a.EmpresaID, a.Desde = &empresa, e.reloj
	r, err := casoEncolar.Ejecutar(e.ctx(), a)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *entorno) registro(clave string) pendientes.Registro {
	e.t.Helper()
	r, err := pendientes.PorClave(context.Background(), e.pool, clave)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func cuerpo(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// La respuesta del primer 2xx queda guardada (sin lo sensible) y el dependiente arma su cuerpo
// con ella una sola vez: los reintentos mandan los mismos bytes.
func TestRespuestaGuardadaYDependienteResuelto(t *testing.T) {
	e := nuevoEntorno(t)
	sufijo := strings.ReplaceAll(nuevaOrden(), "-", "")
	claveCli, claveSrv := "iris:cliente:"+sufijo, "iris:servicio:"+sufijo
	e.argos.cuerpos["/clientes"] = `{"data":{"id":9001,"nombre":"Ana","clave_ppp":"definida","token":"atk_secreto_que_no_se_guarda","password_ppp":"x"}}`
	cli := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: claveCli, Ruta: "/clientes", Cuerpo: []byte(`{"nombre":"Ana"}`)})
	srv := e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: claveSrv, Ruta: "/servicios",
		Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(0, "id"), "plan_id": 300}), DependeDe: []int64{cli.ID}})
	if !srv.PorResolver || !bytes.Contains(srv.Payload, []byte("$iris_ref")) {
		t.Fatalf("el dependiente nace por resolver: %+v %s", srv, srv.Payload)
	}

	// Primera corrida: el cliente sale; el servicio se resuelve y su primer intento falla (503).
	e.argos.responder("/servicios", 503, 201)
	if n := e.procesar(); n != 2 {
		t.Fatalf("procesó %d", n)
	}
	c := e.registro(claveCli)
	var resp map[string]any
	_ = json.Unmarshal(c.Respuesta, &resp)
	data, _ := resp["data"].(map[string]any)
	if c.Estado != "hecho" || data["id"] != float64(9001) || data["clave_ppp"] != "***" || data["token"] != "***" ||
		data["password_ppp"] != "***" || data["nombre"] != "Ana" || bytes.Contains(c.Respuesta, []byte("atk_secreto")) {
		t.Fatalf("respuesta guardada: %s %s", c.Estado, c.Respuesta)
	}
	if id, ok := pendientes.IDDeRespuesta(c.Respuesta); !ok || id != 9001 {
		t.Fatalf("id de la respuesta: %d %v", id, ok)
	}
	s := e.registro(claveSrv)
	if s.PorResolver || bytes.Contains(s.Payload, []byte("$iris_ref")) || s.Estado != "pendiente" {
		t.Fatalf("servicio resuelto y para reintentar: %+v %s", s, s.Payload)
	}
	var plantilla []byte
	_ = e.pool.QueryRow(context.Background(), "SELECT plantilla::text FROM integracion.pendientes WHERE id = $1", s.ID).Scan(&plantilla)
	if !bytes.Contains(plantilla, []byte("$iris_ref")) {
		t.Fatalf("la plantilla queda guardada: %s", plantilla)
	}

	// Reintento: los mismos bytes que el primer envío, con el cliente_id de la respuesta.
	e.reloj = e.reloj.Add(time.Minute)
	if n := e.procesar(); n != 1 {
		t.Fatalf("reintento: %d", n)
	}
	var envios [][]byte
	for _, l := range e.argos.copia() {
		if l.Clave == claveSrv {
			envios = append(envios, l.Crudo)
			if l.Cuerpo["cliente_id"] != float64(9001) || l.Cuerpo["plan_id"] != float64(300) {
				t.Fatalf("cuerpo del servicio: %v", l.Cuerpo)
			}
		}
	}
	if len(envios) != 2 || !bytes.Equal(envios[0], envios[1]) {
		t.Fatalf("los reintentos mandan los mismos bytes: %q", envios)
	}
	if s := e.registro(claveSrv); s.Estado != "hecho" || !bytes.Equal(s.Payload, e.registro(claveSrv).Payload) {
		t.Fatalf("servicio hecho: %+v", s)
	}

	// Encolar un dependiente cuando la dependencia ya está hecha lo resuelve en el acto.
	tarde := e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: claveSrv + ":tarde", Ruta: "/servicios",
		Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(0, "id")}), DependeDe: []int64{cli.ID}})
	if tarde.PorResolver || !bytes.Contains(tarde.Payload, []byte("9001")) {
		t.Fatalf("resuelto al encolar: %+v %s", tarde, tarde.Payload)
	}
	// Encolar dos veces la misma clave devuelve la existente (no cambia su cuerpo).
	otra := e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: claveSrv, Ruta: "/servicios", Cuerpo: []byte(`{"x":1}`)})
	if otra.ID != srv.ID || bytes.Contains(otra.Payload, []byte(`"x"`)) {
		t.Fatalf("misma clave: %+v", otra)
	}
}

// Una respuesta sin el dato que pide el marcador deja al dependiente en fallido de negocio (no
// se adivina); una muy grande no se guarda; un marcador fuera de DependeDe no se encola.
func TestResolucionSinDatoYRespuestaGrande(t *testing.T) {
	e := nuevoEntorno(t)
	sufijo := strings.ReplaceAll(nuevaOrden(), "-", "")
	e.argos.cuerpos["/clientes"] = `{"data":{"nombre":"Sin id"}}`
	cli := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:" + sufijo, Ruta: "/clientes", Cuerpo: []byte(`{}`)})
	srv := e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: "iris:servicio:" + sufijo, Ruta: "/servicios",
		Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(0, "id")}), DependeDe: []int64{cli.ID}})
	if n := e.procesar(); n != 1 {
		t.Fatalf("solo sale el cliente: %d", n)
	}
	s := e.registro(srv.IdempotencyKey)
	var incompleto bool
	var motivo string
	_ = e.pool.QueryRow(context.Background(), "SELECT datos_incompletos, ultimo_error FROM integracion.pendientes WHERE id = $1", s.ID).Scan(&incompleto, &motivo)
	if s.Estado != "fallido_negocio" || !incompleto || !strings.Contains(motivo, "respuesta_sin_id") {
		t.Fatalf("sin el dato: %+v %v %q", s, incompleto, motivo)
	}
	var fallidos int
	_ = e.pool.QueryRow(context.Background(), "SELECT count(*) FROM integracion.outbox WHERE evento = 'integracion.pendiente_fallido' AND (payload->>'pendiente_id')::bigint = $1", s.ID).Scan(&fallidos)
	if fallidos != 1 {
		t.Fatalf("aviso de fallido: %d", fallidos)
	}

	grande := `{"data":{"id":1,"relleno":"` + strings.Repeat("x", pendientes.MaxRespuesta) + `"}}`
	e.argos.cuerpos["/clientes"] = grande
	g := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:grande:" + sufijo, Ruta: "/clientes", Cuerpo: []byte(`{}`)})
	e.procesar()
	if r := e.registro(g.IdempotencyKey); r.Estado != "hecho" || r.Respuesta != nil {
		t.Fatalf("respuesta de más de 64 KiB: %s %d", r.Estado, len(r.Respuesta))
	}

	_, err := casoEncolar.Ejecutar(e.ctx(), pendientes.Accion{Desde: e.reloj, Operacion: "crear_servicio", IdempotencyKey: "iris:servicio:malo:" + sufijo,
		Entidad: "prueba", EntidadID: "1", Ruta: "/servicios", Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(1, "id")}), DependeDe: []int64{cli.ID}})
	if err == nil {
		t.Fatal("un marcador fuera de DependeDe no se encola")
	}
}

// Los 409 se distinguen por type: idempotencia_en_curso se reintenta; user_ppp_duplicado (y
// los demás de negocio) y el 422 idempotency_key_reutilizada fallan sin reintento.
func TestConflictosPorTipo(t *testing.T) {
	e := nuevoEntorno(t)
	casos := []struct {
		ruta, tipo string
		status     int
		estado     string
	}{
		{"/servicios/en-curso", "urn:argos:idempotencia_en_curso", 409, "pendiente"},
		{"/servicios/ppp", "urn:argos:user_ppp_duplicado", 409, "fallido_negocio"},
		{"/servicios/otro-409", "router_sin_capacidad", 409, "fallido_negocio"},
		{"/servicios/reutilizada", "https://argos.example/problemas/idempotency_key_reutilizada", 422, "fallido_negocio"},
		{"/servicios/sin-tipo", "", 409, "pendiente"},
	}
	for i, c := range casos {
		e.argos.responder(c.ruta, c.status)
		e.argos.tipos[c.ruta] = c.tipo
		e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: "iris:conflicto:" + nuevaOrden(), Ruta: c.ruta,
			Cuerpo: []byte(`{}`), EntidadID: string(rune('a' + i)), Entidad: "conflicto"})
	}
	e.procesar()
	rows, err := e.pool.Query(context.Background(), "SELECT ruta, estado, intentos FROM integracion.pendientes WHERE entidad = 'conflicto'")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	estados := map[string]string{}
	intentosDe := map[string]int{}
	for rows.Next() {
		var ruta, estado string
		var intentos int
		if err := rows.Scan(&ruta, &estado, &intentos); err != nil {
			t.Fatal(err)
		}
		estados[ruta], intentosDe[ruta] = estado, intentos
	}
	rows.Close()
	for _, c := range casos {
		if estados[c.ruta] != c.estado || intentosDe[c.ruta] != 1 {
			t.Fatalf("%s %d %s: %s, %d intentos (esperaba %s tras 1)", c.ruta, c.status, c.tipo, estados[c.ruta], intentosDe[c.ruta], c.estado)
		}
	}
}
