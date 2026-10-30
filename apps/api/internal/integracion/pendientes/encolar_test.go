package pendientes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	e.argos.cuerpos["/clientes"] = `{"meta":{"id":1},"estado":"creado","psk":"wifi-secreto","data":{"id":9001,"nombre":"Ana","clave_ppp":"definida",` +
		`"token":"atk_secreto_que_no_se_guarda","contraseña":"x","wpa_key":"y","x-api-key":"z","pin":"1234","llave":"w","private_key":"k",` +
		`"community":"public","codigoExterno":"iris:prospecto:1","servicio":{"id":5},"equipos":[{"id":6}]}}`
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
	// Solo la lista blanca (en la raíz y en data); credenciales, datos personales, objetos y
	// listas no se guardan.
	c := e.registro(claveCli)
	var resp, esperada any
	_ = json.Unmarshal(c.Respuesta, &resp)
	_ = json.Unmarshal([]byte(`{"estado":"creado","data":{"id":9001,"codigoExterno":"iris:prospecto:1"}}`), &esperada)
	if c.Estado != "hecho" || fmt.Sprint(resp) != fmt.Sprint(esperada) {
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

// Los 409 se distinguen por type: solo idempotencia_en_curso se reintenta (contrato 0.3.1 §3.1);
// user_ppp_duplicado, servicio_no_pendiente, cualquier otro 409 (también sin type o con
// about:blank) y el 422 idempotency_key_reutilizada fallan sin reintento.
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
		{"/servicios/no-pendiente", "urn:argosisp:problema:servicio_no_pendiente", 409, "fallido_negocio"},
		{"/servicios/sin-tipo", "", 409, "fallido_negocio"},
		{"/servicios/about-blank", "about:blank", 409, "fallido_negocio"},
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

// Carrera: Encolar el dependiente mientras su dependencia queda hecha en otra transacción. Con el
// candado compartido, Encolar espera a que esa confirme y lo resuelve; sin él quedaría por
// resolver para siempre (la otra no ve al dependiente nuevo).
func TestEncolarConDependenciaTerminandoEnOtraTransaccion(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()
	sufijo := strings.ReplaceAll(nuevaOrden(), "-", "")
	cli := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:" + sufijo, Ruta: "/clientes", Cuerpo: []byte(`{}`)})

	// Otra transacción (como el resultado del worker) deja la dependencia hecha sin confirmar.
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM integracion.pendientes WHERE id = $1 FOR UPDATE`, cli.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE integracion.pendientes SET estado = 'hecho', hecho_en = now(), respuesta = '{"data":{"id":777}}' WHERE id = $1`, cli.ID); err != nil {
		t.Fatal(err)
	}

	hecho := make(chan pendientes.Registro, 1)
	go func() {
		empresa := int64(1)
		r, err := casoEncolar.Ejecutar(e.ctx(), pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: "iris:servicio:" + sufijo,
			Entidad: "prueba", EntidadID: "1", Ruta: "/servicios", EmpresaID: &empresa, Desde: e.reloj,
			Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(0, "id")}), DependeDe: []int64{cli.ID}})
		if err != nil {
			t.Error(err)
		}
		hecho <- r
	}()
	// Encolar queda esperando el candado de la dependencia.
	esperando := false
	for range 100 {
		var n int
		_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n)
		if n > 0 {
			esperando = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !esperando {
		t.Fatal("Encolar no esperó el candado de la dependencia")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-hecho:
		if r.PorResolver || !bytes.Contains(r.Payload, []byte("777")) {
			t.Fatalf("el dependiente quedó por resolver: %+v %s", r, r.Payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Encolar no terminó")
	}
}

// Red de seguridad: un dependiente atascado por resolver con todas sus dependencias hechas lo
// resuelve el job; y mientras está por resolver nunca vence, así que ni un worker de antes de
// los marcadores (su TomarVencido no mira por_resolver) lo manda a Argos.
func TestPorResolverAtascadoYCandadoParaWorkerViejo(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := context.Background()
	sufijo := strings.ReplaceAll(nuevaOrden(), "-", "")
	cli := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:" + sufijo, Ruta: "/clientes", Cuerpo: []byte(`{}`)})
	srv := e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: "iris:servicio:" + sufijo, Ruta: "/servicios",
		Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(0, "id")}), DependeDe: []int64{cli.ID}})
	// La dependencia queda hecha por fuera (sin resolver al dependiente).
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.pendientes SET estado = 'hecho', hecho_en = now(), respuesta = '{"id":4242}' WHERE id = $1`, cli.ID); err != nil {
		t.Fatal(err)
	}
	var proximo time.Time
	_ = e.pool.QueryRow(ctx, "SELECT proximo_intento FROM integracion.pendientes WHERE id = $1", srv.ID).Scan(&proximo)
	if !proximo.Equal(pendientes.NuncaVence) {
		t.Fatalf("por resolver no vence: %s", proximo)
	}
	// TomarVencido de antes de los marcadores (sin «NOT p.por_resolver»): no lo toma.
	var viejo int
	_ = e.pool.QueryRow(ctx, `SELECT count(*) FROM integracion.pendientes p
		WHERE ((p.estado = 'pendiente' AND p.proximo_intento <= $1) OR (p.estado = 'en_curso' AND p.tomado_en <= $1))
		  AND NOT EXISTS (SELECT 1 FROM integracion.pendientes d WHERE d.id = ANY (p.depende_de) AND d.estado <> 'hecho')
		  AND p.id = $2`, e.reloj.Add(1000*time.Hour), srv.ID).Scan(&viejo)
	if viejo != 0 {
		t.Fatal("un worker viejo tomaría el cuerpo con el marcador crudo")
	}
	// El job lo resuelve (red de seguridad) y lo manda en la misma corrida.
	if n := e.procesar(); n != 1 {
		t.Fatalf("procesó %d", n)
	}
	s := e.registro(srv.IdempotencyKey)
	if s.Estado != "hecho" || s.PorResolver || !bytes.Contains(s.Payload, []byte("4242")) {
		t.Fatalf("atascado resuelto y enviado: %+v %s", s, s.Payload)
	}
	for _, l := range e.argos.copia() {
		if bytes.Contains(l.Crudo, []byte("$iris_ref")) {
			t.Fatalf("a Argos llegó un marcador: %s", l.Crudo)
		}
	}
}

// DependeDe: cada dependencia debe existir y ser de la misma empresa (error de programación).
func TestEncolarValidaDependencias(t *testing.T) {
	e := nuevoEntorno(t)
	sufijo := strings.ReplaceAll(nuevaOrden(), "-", "")
	cli := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:" + sufijo, Ruta: "/clientes", Cuerpo: []byte(`{}`)})
	otra := int64(2)
	for nombre, a := range map[string]pendientes.Accion{
		"inexistente":  {DependeDe: []int64{999999999}},
		"repetida":     {DependeDe: []int64{cli.ID, cli.ID}},
		"otra empresa": {DependeDe: []int64{cli.ID}, EmpresaID: &otra},
		"sin empresa":  {DependeDe: []int64{cli.ID}, EmpresaID: nil},
	} {
		a.Operacion, a.IdempotencyKey, a.Entidad, a.EntidadID, a.Ruta, a.Desde = "crear_servicio", "iris:dep:"+nombre+sufijo, "prueba", "1", "/servicios", e.reloj
		if nombre != "otra empresa" && nombre != "sin empresa" {
			uno := int64(1)
			a.EmpresaID = &uno
		}
		if _, err := casoEncolar.Ejecutar(e.ctx(), a); err == nil {
			t.Fatalf("%s: debía fallar", nombre)
		}
	}
	uno := int64(1)
	if _, err := casoEncolar.Ejecutar(e.ctx(), pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: "iris:dep:ok" + sufijo,
		Entidad: "prueba", EntidadID: "1", Ruta: "/servicios", EmpresaID: &uno, Desde: e.reloj, DependeDe: []int64{cli.ID}}); err != nil {
		t.Fatalf("misma empresa: %v", err)
	}
}

// Verificar: si la respuesta del 2xx trae otro codigo_externo que el enviado, el pendiente queda
// fallido_negocio (respuesta_inconsistente), no resuelve a su dependiente (queda bloqueado) y el
// dependiente nunca sale hacia Argos.
func TestRespuestaInconsistenteNoResuelveDependientes(t *testing.T) {
	e := nuevoEntorno(t)
	sufijo := strings.ReplaceAll(nuevaOrden(), "-", "")
	e.argos.cuerpos["/clientes"] = `{"data":{"id":555,"codigo_externo":"iris:prospecto:otro"}}`
	cli := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:" + sufijo, Ruta: "/clientes",
		Cuerpo: []byte(`{"nombre":"Ana","codigo_externo":"iris:prospecto:1"}`), Verificar: []string{"codigo_externo"}})
	srv := e.encolar(pendientes.Accion{Operacion: "crear_servicio", IdempotencyKey: "iris:servicio:" + sufijo, Ruta: "/servicios",
		Cuerpo: cuerpo(t, map[string]any{"cliente_id": pendientes.Ref(0, "id")}), DependeDe: []int64{cli.ID}})
	if n := e.procesar(); n != 1 {
		t.Fatalf("procesó %d", n)
	}
	c := e.registro(cli.IdempotencyKey)
	if c.Estado != "fallido_negocio" || c.UltimoError == nil || !strings.HasPrefix(*c.UltimoError, pendientes.MotivoInconsistente) {
		t.Fatalf("cliente inconsistente: %+v", c)
	}
	s := e.registro(srv.IdempotencyKey)
	if s.Estado != "bloqueado" || !s.PorResolver {
		t.Fatalf("servicio bloqueado sin resolver: %+v", s)
	}
	e.reloj = e.reloj.Add(24 * time.Hour)
	e.procesar()
	for _, l := range e.argos.copia() {
		if l.Clave == srv.IdempotencyKey {
			t.Fatal("el servicio salió hacia Argos")
		}
	}
	// Con el mismo codigo_externo (o sin repetirlo) queda hecho; verificar exige el campo en el
	// cuerpo y en la lista blanca.
	e.argos.cuerpos["/clientes"] = `{"data":{"id":556,"codigo_externo":"iris:prospecto:2"}}`
	ok := e.encolar(pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:ok:" + sufijo, Ruta: "/clientes",
		Cuerpo: []byte(`{"codigo_externo":"iris:prospecto:2"}`), Verificar: []string{"codigo_externo"}})
	e.procesar()
	if r := e.registro(ok.IdempotencyKey); r.Estado != "hecho" {
		t.Fatalf("respuesta consistente: %+v", r)
	}
	for _, v := range [][]string{{"nombre"}, {"codigo_externo"}} {
		_, err := casoEncolar.Ejecutar(e.ctx(), pendientes.Accion{Operacion: "crear_cliente", IdempotencyKey: "iris:cliente:mal:" + v[0] + sufijo,
			Entidad: "prueba", EntidadID: "1", Ruta: "/clientes", Desde: e.reloj, Cuerpo: []byte(`{"nombre":"x"}`), Verificar: v})
		if err == nil {
			t.Fatalf("verificar %v debía fallar al encolar", v)
		}
	}
}
