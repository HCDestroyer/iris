package mensajeria_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Bandeja de conversaciones con clientes (contratos/conversaciones.md): pruebas contra Postgres real
// y por HTTP, con un número de WhatsApp Cloud (Meta falso) por prueba.

const (
	rutaMsj   = "/api/v1/mensajeria"
	secretoWA = "app-secret-0123456789"
)

// argosFalso es lo que la bandeja usa de Argos.
type argosFalso struct {
	mu          sync.Mutex
	telefono    []argos.ClienteTelefono
	caido       bool
	comprobante int64
	errComprob  error
	promesa     argos.PromesaArgos
	// alCrearPromesa corre mientras «Argos» atiende la promesa (fuera de la transacción de Iris).
	alCrearPromesa func()
	claves         []string
	actores        []argos.AtencionIris
	modelos        []string
	// cuenta: el estado de cuenta que responde (nil = Argos no responde); cuentas: cuántas lecturas.
	cuenta  *argos.EstadoCuenta
	cuentas int
}

func (f *argosFalso) BuscarPorTelefono(_ context.Context, a argos.AtencionIris, _ string) ([]argos.ClienteTelefono, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actores = append(f.actores, a)
	if f.caido {
		return nil, &conector.ErrorRemoto{Status: 0}
	}
	return f.telefono, nil
}

func (f *argosFalso) AsegurarInstantanea(context.Context, int64, *int64) error { return nil }

// EstadoCuentaCliente: sin cuenta configurada, «Argos» no responde (la sugerencia sale sin saldo).
func (f *argosFalso) EstadoCuentaCliente(_ context.Context, a argos.AtencionIris, _ int64) (argos.EstadoCuenta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actores = append(f.actores, a)
	f.cuentas++
	if f.cuenta == nil || f.caido {
		return argos.EstadoCuenta{}, &conector.ErrorRemoto{Status: 0}
	}
	return *f.cuenta, nil
}

func (f *argosFalso) EquiposEnComodato(context.Context, int64, int64) ([]argos.EquipoComodatoArgos, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []argos.EquipoComodatoArgos{}
	for _, m := range f.modelos {
		out = append(out, argos.EquipoComodatoArgos{Modelo: m})
	}
	return out, nil
}

func (f *argosFalso) RegistrarComprobante(_ context.Context, a argos.AtencionIris, in argos.EntradaComprobante) (argos.ComprobanteArgos, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claves = append(f.claves, in.Clave)
	f.actores = append(f.actores, a)
	if f.errComprob != nil {
		return argos.ComprobanteArgos{}, f.errComprob
	}
	f.comprobante++
	return argos.ComprobanteArgos{ID: 7000 + f.comprobante, Estado: "pendiente"}, nil
}

func (f *argosFalso) CuentasBancarias(context.Context, argos.AtencionIris) ([]argos.CuentaBancariaArgos, error) {
	return []argos.CuentaBancariaArgos{{ID: 3, Nombre: "Monetaria", Banco: json.RawMessage(`{"nombre":"Banco Industrial"}`), Numero: "123", Moneda: "GTQ"},
		{ID: 4, Nombre: "Dólares", Moneda: "USD"}}, nil
}

func (f *argosFalso) Bancos(context.Context, argos.AtencionIris) ([]argos.BancoArgos, error) {
	return []argos.BancoArgos{{ID: 1, Nombre: "Banrural"}}, nil
}

func (f *argosFalso) SimularPromesa(context.Context, argos.AtencionIris, int64, *time.Time) (argos.SimulacionPromesa, error) {
	return argos.SimulacionPromesa{Resultado: "aprobada", Monto: json.RawMessage(`"250.00"`)}, nil
}

func (f *argosFalso) CrearPromesa(_ context.Context, _ argos.AtencionIris, _ int64, _ time.Time, clave string) (argos.PromesaArgos, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claves = append(f.claves, clave)
	if f.alCrearPromesa != nil {
		f.alCrearPromesa()
	}
	return f.promesa, nil
}

// bandeja es una prueba con su número de WhatsApp, su cola y su gente.
type bandeja struct {
	*entorno
	argos      *argosFalso
	canal      mensajeria.Canal
	pnid       string
	cola       int64
	supervisor persona
	agente     persona // miembro de la cola
	otro       persona // soporte, no es de la cola
	lectura    persona
	tecnico    persona // sin permisos de conversaciones
}

func nuevaBandeja(t *testing.T, ajustar func(*mensajeria.Config)) *bandeja {
	t.Helper()
	f := &argosFalso{promesa: argos.PromesaArgos{ID: 501, Estado: "activa", Monto: json.RawMessage(`"250.00"`)}}
	dir := t.TempDir()
	almacen, err := adjuntos.NuevoDisco(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := nuevoEntorno(t, func(c *mensajeria.Config) {
		c.Argos = f
		c.Almacen = almacen
		c.Soporte = soporte.Nuevo(soporte.Config{})
		if ajustar != nil {
			ajustar(c)
		}
	})
	mensajeria.RegistrarAdjuntos()
	b := &bandeja{entorno: e, argos: f}
	b.supervisor = b.persona([]int64{1}, rolBase("supervisor"))
	b.agente = b.persona([]int64{1}, rolBase("soporte"))
	b.otro = b.persona([]int64{1}, rolBase("soporte"))
	b.lectura = b.persona([]int64{1}, rolBase("lectura"))
	b.tecnico = b.persona([]int64{1}, rolBase("tecnico"))
	r := b.api(b.supervisor, http.MethodPost, "/colas", map[string]any{"nombre": "Soporte " + pruebas.Unico(), "slaRespuestaMin": 60})
	e.exigir(r, http.StatusCreated)
	b.cola = int64(r.cuerpo["id"].(float64))
	e.exigir(b.api(b.supervisor, http.MethodPost, fmt.Sprintf("/colas/%d/miembros", b.cola),
		map[string]any{"version": r.cuerpo["version"], "usuarioIds": []int64{b.agente.id}}), http.StatusOK)
	b.pnid = fmt.Sprint(time.Now().UnixNano() % 1e12)
	empresa := int64(1)
	b.canal, err = e.m.CrearCanal.Ejecutar(e.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &empresa, Nombre: "Soporte", Numero: "+50222000010",
		PhoneNumberID: b.pnid, Token: "token-de-meta-0123456789abcdef", AppSecret: secretoWA, VerifyToken: "verify-token-0123456789",
		Usos: []string{"soporte"}, ColaID: &b.cola,
		Plantillas: map[string]mensajeria.PlantillaMeta{"bienvenida": {Nombre: "bienvenida_v1", Idioma: "es"}}})
	if err != nil {
		t.Fatal(err)
	}
	if b.canal.AvisosEmpresa {
		t.Fatal("un número sin el uso avisos no es el de avisos")
	}
	return b
}

// persona: la bandeja es de personas con sesión del panel (cookie + CSRF), no de tokens.
func (b *bandeja) persona(empresas []int64, roles ...identidad.AsignacionRol) persona {
	b.t.Helper()
	p := b.entorno.persona(empresas, roles...)
	return b.sesion(p)
}

func (b *bandeja) sesion(p persona) persona {
	b.t.Helper()
	s, err := identChat.IniciarSesion.Ejecutar(nucleo.ConBD(context.Background(), b.bd), identidad.EntradaIniciarSesion{Correo: p.correo, Contrasena: "contrasena-larga-1"})
	if err != nil {
		b.t.Fatal(err)
	}
	p.cookie, p.csrf = s.Token, s.CSRFToken
	return p
}

func (b *bandeja) api(p persona, metodo, ruta string, cuerpo any) respuesta {
	b.t.Helper()
	return b.apiClave(p, metodo, ruta, "", cuerpo)
}

func (b *bandeja) apiClave(p persona, metodo, ruta, clave string, cuerpo any) respuesta {
	b.t.Helper()
	var rd io.Reader
	if cuerpo != nil {
		crudo, _ := json.Marshal(cuerpo)
		rd = bytes.NewReader(crudo)
	}
	req := httptest.NewRequest(metodo, rutaMsj+ruta, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.cookie != "" {
		req.AddCookie(&http.Cookie{Name: identidad.CookieSesion, Value: p.cookie})
		req.AddCookie(&http.Cookie{Name: identidad.CookieCSRF, Value: p.csrf})
		req.Header.Set(identidad.CabeceraCSRF, p.csrf)
	} else {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	if p.empresa != 0 {
		req.Header.Set(identidad.CabeceraEmpresa, fmt.Sprint(p.empresa))
	}
	if clave != "" {
		req.Header.Set("Idempotency-Key", clave)
	}
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, req)
	r := respuesta{status: w.Code, h: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &r.cuerpo)
	return r
}

// webhook manda un cuerpo de Meta firmado y procesa los entrantes nuevos.
func (b *bandeja) webhook(cuerpo string) {
	b.t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("%s/canales/%d/webhook", rutaMsj, b.canal.ID), strings.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", mensajeria.FirmarMeta(secretoWA, []byte(cuerpo)))
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		b.t.Fatalf("webhook: %d %s", w.Code, w.Body.String())
	}
	filas, err := b.bd.Query(b.ctx, `SELECT id FROM mensajeria.entrantes WHERE canal_id = $1 AND procesado_en IS NULL ORDER BY id`, b.canal.ID)
	if err != nil {
		b.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(filas, pgx.RowTo[int64])
	if err != nil {
		b.t.Fatal(err)
	}
	for _, id := range ids {
		if err := b.m.ProcesarEntrante(b.ctx, id); err != nil {
			b.t.Fatal(err)
		}
	}
}

func (b *bandeja) mensajeCliente(de, wamid, mensaje string) string {
	return fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":%q},`+
		`"contacts":[{"wa_id":%q,"profile":{"name":"Carlos R."}}],"messages":[{"from":%q,"id":%q,"timestamp":"%d",%s}]}}]}]}`,
		b.pnid, de, de, wamid, b.reloj.Unix(), mensaje)
}

func (b *bandeja) texto(de, wamid, texto string) {
	b.t.Helper()
	b.webhook(b.mensajeCliente(de, wamid, fmt.Sprintf(`"type":"text","text":{"body":%q}`, texto)))
}

func (b *bandeja) estadoWA(wamid, estado string) {
	b.t.Helper()
	b.webhook(fmt.Sprintf(`{"object":"whatsapp_business_account","entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":%q},`+
		`"statuses":[{"id":%q,"status":%q,"timestamp":"%d"}]}}]}]}`, b.pnid, wamid, estado, b.reloj.Unix()))
}

func numeroCliente() string { return fmt.Sprintf("5025%07d", siguienteCliente.Add(1)%10_000_000) }

// conv lee la conversación del número (como supervisor).
func (b *bandeja) conv(numero string) map[string]any {
	b.t.Helper()
	var id int64
	if err := b.bd.QueryRow(b.ctx, `SELECT id FROM mensajeria.conversaciones WHERE canal_id = $1 AND numero = $2`, b.canal.ID, "+"+numero).Scan(&id); err != nil {
		b.t.Fatalf("conversación de %s: %v", numero, err)
	}
	r := b.api(b.supervisor, http.MethodGet, fmt.Sprintf("/conversaciones/%d", id), nil)
	b.exigir(r, http.StatusOK)
	return r.cuerpo
}

func cid(c map[string]any) string { return fmt.Sprint(int64(c["id"].(float64))) }

func ver(c map[string]any) float64 { return c["version"].(float64) }

func (b *bandeja) mensajes(p persona, conv string) []map[string]any {
	b.t.Helper()
	r := b.api(p, http.MethodGet, "/conversaciones/"+conv+"/mensajes?limit=200", nil)
	b.exigir(r, http.StatusOK)
	out := []map[string]any{}
	for _, x := range r.cuerpo["data"].([]any) {
		out = append(out, x.(map[string]any))
	}
	return out
}

func (b *bandeja) jobsDe(kind string) []any {
	var out []any
	for _, j := range *b.jobs {
		if j.Kind() == kind {
			out = append(out, j)
		}
	}
	return out
}

// Pruebas -----------------------------------------------------------------------------------------

func TestConvEntranteCreaYDeduplica(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.a1", "Buenos días, no tengo internet")
	c := b.conv(num)
	if c["estado"] != "abierta" || c["perfilNombre"] != "Carlos R." || c["cola"].(map[string]any)["id"].(float64) != float64(b.cola) {
		t.Fatalf("conversación nueva: %v", c)
	}
	if c["sla"].(map[string]any)["estado"] != "en_tiempo" || c["ventana"].(map[string]any)["abierta"] != true {
		t.Fatalf("SLA y ventana: %v %v", c["sla"], c["ventana"])
	}
	if c["contacto"].(map[string]any)["tipo"] != "desconocido" {
		t.Fatalf("contacto: %v", c["contacto"])
	}
	// El mismo mensaje otra vez (otro cuerpo, mismo wamid) y el mismo cuerpo: nada nuevo.
	b.texto(num, "wamid.a1", "Buenos días, no tengo internet")
	b.webhook(b.mensajeCliente(num, "wamid.a1", `"type":"text","text":{"body":"x"}`))
	ms := b.mensajes(b.supervisor, cid(c))
	entrantes := 0
	for _, m := range ms {
		if m["direccion"] == "entrante" {
			entrantes++
		}
	}
	if entrantes != 1 || ms[0]["direccion"] != "sistema" || ms[0]["evento"].(map[string]any)["tipo"] != "conversacion_creada" {
		t.Fatalf("mensajes: %v", ms)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.creada' AND payload->>'conversacion_id' = $1`, cid(c)); n != 1 {
		t.Fatalf("conversacion.creada: %d", n)
	}
	// El texto del cliente no va a la auditoría.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%no tengo internet%'`); n != 0 {
		t.Fatal("el texto del cliente quedó en la auditoría")
	}
	// Una conversación por (empresa, número, contacto): el segundo mensaje entra a la misma.
	b.texto(num, "wamid.a2", "¿Me ayudan?")
	if n := b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE numero = $1`, "+"+num); n != 1 {
		t.Fatalf("conversaciones del número: %d", n)
	}
}

func TestConvAsociacionAutomaticaYCoincidencias(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	cliente := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, codigo, sincronizado_en, empresas)
		VALUES ($1, 1, 'Carlos Ruiz', $2, 'C-1', now(), '{1}')`, cliente, "+502 "+num[3:7]+"-"+num[7:]); err != nil {
		t.Fatal(err)
	}
	b.texto(num, "wamid.b1", "Hola")
	c := b.conv(num)
	k := c["contacto"].(map[string]any)
	if k["tipo"] != "cliente" || k["clienteId"].(float64) != float64(cliente) || k["asociacion"] != "automatica" || k["verificado"] != true || c["titulo"] != "Carlos Ruiz" {
		t.Fatalf("asociación automática: %v %v", k, c["titulo"])
	}
	if c["clienteResumen"].(map[string]any)["nombre"] != "Carlos Ruiz" {
		t.Fatalf("resumen de la instantánea: %v", c["clienteResumen"])
	}
	// Coincidencias: Argos responde; sin Argos, la instantánea.
	b.argos.telefono = []argos.ClienteTelefono{{ClienteArgos: argos.ClienteArgos{ID: 99, Nombre: "Luis Herrera"}, CoincideEn: []string{"contacto_servicio"}}}
	r := b.api(b.agente, http.MethodGet, "/conversaciones/"+cid(c)+"/coincidencias", nil)
	b.exigir(r, http.StatusOK, "argosConsultado", "true")
	if cl := r.cuerpo["clientes"].([]any); len(cl) != 1 || cl[0].(map[string]any)["origen"] != "argos" {
		t.Fatalf("coincidencias de Argos: %v", r.cuerpo)
	}
	if a := b.argos.actores[len(b.argos.actores)-1]; a.Actor != fmt.Sprintf("usuario:%d", b.agente.id) || a.Conversacion != "iris:conversacion:"+cid(c) {
		t.Fatalf("cabeceras §7.1: %+v", a)
	}
	b.argos.caido = true
	r = b.api(b.agente, http.MethodGet, "/conversaciones/"+cid(c)+"/coincidencias", nil)
	b.exigir(r, http.StatusOK, "argosConsultado", "false")
	cl := r.cuerpo["clientes"].([]any)
	if len(cl) != 1 || cl[0].(map[string]any)["origen"] != "instantanea" || fmt.Sprint(cl[0].(map[string]any)["coincideEn"]) != "[movil]" {
		t.Fatalf("coincidencias de la instantánea: %v", r.cuerpo)
	}
	// Asociar a mano otro cliente: debe ser de la empresa.
	otra := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 2, 'De otra', now(), '{2}')`, otra); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/asociar-cliente", map[string]any{"version": ver(c), "clienteId": otra}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_fuera_de_empresa")
	// Contacto de Iris y luego unirlo a la ficha de Argos.
	r = b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/contacto-iris", map[string]any{"version": ver(c), "nombre": "Proveedor X", "tipo": "proveedor"})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["contacto"].(map[string]any)["tipo"] != "contacto_iris" || r.cuerpo["titulo"] != "Proveedor X" {
		t.Fatalf("contacto de Iris: %v", r.cuerpo)
	}
	contacto := int64(r.cuerpo["contacto"].(map[string]any)["contactoIrisId"].(float64))
	u := b.api(b.agente, http.MethodPost, fmt.Sprintf("/contactos-iris/%d/unir-cliente", contacto), map[string]any{"version": 1, "clienteId": cliente})
	b.exigir(u, http.StatusOK)
	c = b.conv(num)
	if c["contacto"].(map[string]any)["tipo"] != "cliente" {
		t.Fatalf("al unir, la conversación pasa al cliente: %v", c["contacto"])
	}
}

func TestConvVisibilidadYPermisos(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.c1", "Hola")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)
	b.exigir(b.api(b.agente, http.MethodGet, ruta, nil), http.StatusOK)
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusNotFound)
	b.exigir(b.api(b.tecnico, http.MethodGet, ruta, nil), http.StatusForbidden)
	// lectura ve (sin cola: no) y no responde.
	b.exigir(b.api(b.lectura, http.MethodPost, ruta+"/mensajes", map[string]any{"id": nuevoID(), "texto": "x"}), http.StatusForbidden)
	// De otra empresa: 404.
	ajeno := b.persona([]int64{2}, rolBase("supervisor"))
	b.exigir(b.api(ajeno, http.MethodGet, ruta, nil), http.StatusNotFound)
	// Las listas solo traen lo que se ve.
	lista := func(p persona, q string) []any {
		r := b.api(p, http.MethodGet, "/conversaciones?limit=200"+q, nil)
		b.exigir(r, http.StatusOK)
		return r.cuerpo["data"].([]any)
	}
	tiene := func(xs []any) bool {
		for _, x := range xs {
			if cid(x.(map[string]any)) == cid(c) {
				return true
			}
		}
		return false
	}
	if !tiene(lista(b.agente, "&vista=sin_asignar")) || tiene(lista(b.otro, "")) || !tiene(lista(b.supervisor, fmt.Sprintf("&colaId=%d", b.cola))) {
		t.Fatal("lista por alcance de cola")
	}
	if !tiene(lista(b.supervisor, "&q="+num[len(num)-6:])) || !tiene(lista(b.supervisor, "&q=Carlos")) {
		t.Fatal("búsqueda por número y nombre")
	}
	// Mencionar a «otro» le da visibilidad y le avisa.
	nota := b.api(b.agente, http.MethodPost, ruta+"/notas", map[string]any{"id": nuevoID(), "texto": "Ayúdame con esta", "menciones": []int64{b.otro.id}})
	b.exigir(nota, http.StatusCreated, "direccion", "nota")
	b.exigir(b.api(b.otro, http.MethodGet, ruta, nil), http.StatusOK)
	r := b.api(b.otro, http.MethodGet, "/conversaciones/cambios?cursor=0&limit=200", nil)
	b.exigir(r, http.StatusOK)
	mencion := false
	for _, x := range r.cuerpo["data"].([]any) {
		ev := x.(map[string]any)
		mencion = mencion || (ev["tipo"] == "mencion" && fmt.Sprint(int64(ev["conversacionId"].(float64))) == cid(c))
	}
	if !mencion {
		t.Fatalf("evento mencion: %v", r.cuerpo)
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.mencion' AND payload->>'usuario_id' = $1`, fmt.Sprint(b.otro.id)) != 1 {
		t.Fatal("conversacion.mencion")
	}
	// Mencionar a quien no atiende conversaciones: 422.
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/notas", map[string]any{"id": nuevoID(), "texto": "x", "menciones": []int64{b.tecnico.id}}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:usuario_no_disponible")
	// La nota no sale a WhatsApp.
	if len(b.jobsDe("mensajeria.enviar_mensaje")) != 0 {
		t.Fatal("una nota no se envía")
	}
}

func TestConvResponderVentanaYEntrega(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.d1", "Hola")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c) + "/mensajes"
	id := nuevoID()
	r := b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": id, "texto": "  Te enviamos un técnico hoy.  "})
	b.exigir(r, http.StatusCreated, "estadoEntrega", "en_cola", "texto", "Te enviamos un técnico hoy.")
	c = b.conv(num)
	if c["estado"] != "esperando_cliente" || c["sla"].(map[string]any)["estado"] != "sin" {
		t.Fatalf("al responder: %v %v", c["estado"], c["sla"])
	}
	// Reenvío del mismo id: 200 con el mismo; otro contenido: 409.
	r2 := b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": id, "texto": "Te enviamos un técnico hoy."})
	b.exigir(r2, http.StatusOK, "id", id)
	if r2.h.Get("Idempotent-Replayed") != "true" {
		t.Fatal("reenvío sin Idempotent-Replayed")
	}
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": id, "texto": "otro"}), http.StatusConflict, "type", "urn:iris:problema:mensaje_id_reutilizado")
	// El job lo manda a Meta como texto.
	if _, err := b.m.ProcesarMensaje(b.ctx, uuid.MustParse(id), false); err != nil {
		t.Fatal(err)
	}
	env := b.meta.envios[len(b.meta.envios)-1]
	if env["type"] != "text" || env["to"] != num || env["text"].(map[string]any)["body"] != "Te enviamos un técnico hoy." {
		t.Fatalf("envío a Meta: %v", env)
	}
	var wamid string
	if err := b.bd.QueryRow(b.ctx, `SELECT proveedor_id FROM mensajeria.conversacion_mensajes WHERE id = $1`, id).Scan(&wamid); err != nil {
		t.Fatal(err)
	}
	b.estadoWA(wamid, "read")
	b.estadoWA(wamid, "delivered") // no retrocede
	var estado string
	if err := b.bd.QueryRow(b.ctx, `SELECT estado_entrega FROM mensajeria.conversacion_mensajes WHERE id = $1`, id).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado != "leido" {
		t.Fatalf("estado de entrega: %s", estado)
	}

	// Fuera de la ventana de 24 h: texto 422; plantilla no aprobada 422; aprobada sin permiso 422.
	*b.reloj = b.reloj.Add(25 * time.Hour)
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": nuevoID(), "texto": "¿Sigue?"}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:fuera_de_ventana")
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"id": nuevoID(), "plantilla": map[string]any{"codigo": "factura_por_vencer",
		"variables": map[string]string{"monto": "1.00", "moneda": "GTQ", "vence_en": "2026-11-10"}}}), http.StatusUnprocessableEntity,
		"type", "urn:iris:problema:plantilla_no_aprobada")
	p, ok := mensajeria.PlantillaPorCodigo("bienvenida")
	if !ok {
		t.Fatal("bienvenida")
	}
	vars := map[string]string{}
	for _, v := range p.Variables {
		vars[v.Nombre] = ejemploVariable(v.Formato)
	}
	pl := map[string]any{"id": nuevoID(), "plantilla": map[string]any{"codigo": "bienvenida", "variables": vars}}
	b.exigir(b.api(b.agente, http.MethodPost, ruta, pl), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_permiso")
	// Con cliente y permiso vigente, sale.
	cliente := b.cliente(1, "5512-0001")
	if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_id = $2 WHERE id = $1`, cid(c), cliente); err != nil {
		t.Fatal(err)
	}
	b.exigir(b.consentir(cliente, "acepta", b.reloj.Add(-time.Hour)), http.StatusCreated)
	// Asociado a mano y el número no es del cliente: hasta que un supervisor lo confirme, no.
	b.exigir(b.api(b.agente, http.MethodPost, ruta, pl), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	conf := "/conversaciones/" + cid(c) + "/confirmar-cliente"
	b.exigir(b.api(b.agente, http.MethodPost, conf, map[string]any{"version": ver(b.conv(num))}), http.StatusForbidden)
	cf := b.api(b.supervisor, http.MethodPost, conf, map[string]any{"version": ver(b.conv(num))})
	b.exigir(cf, http.StatusOK)
	if cf.cuerpo["contacto"].(map[string]any)["verificado"] != true {
		t.Fatalf("confirmado: %v", cf.cuerpo["contacto"])
	}
	r = b.api(b.agente, http.MethodPost, ruta, pl)
	b.exigir(r, http.StatusCreated, "tipo", "plantilla")
	if _, err := b.m.ProcesarMensaje(b.ctx, uuid.MustParse(r.cuerpo["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	if env := b.meta.envios[len(b.meta.envios)-1]; env["type"] != "template" {
		t.Fatalf("plantilla a Meta: %v", env)
	}
	// Con baja vigente, nunca sale como plantilla.
	b.exigir(b.consentir(cliente, "baja", b.reloj.Add(-time.Minute)), http.StatusCreated)
	pl["id"] = nuevoID()
	b.exigir(b.api(b.agente, http.MethodPost, ruta, pl), http.StatusUnprocessableEntity, "type", "urn:iris:problema:baja")
	// El cliente vuelve a escribir: la conversación se reabre la ventana.
	b.texto(num, "wamid.d2", "Ya llegó el técnico")
	if c = b.conv(num); c["estado"] != "abierta" || c["ventana"].(map[string]any)["abierta"] != true {
		t.Fatalf("vuelve a abrir: %v", c)
	}
	// El texto del agente no queda en la auditoría.
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.responder' AND entrada::text LIKE '%técnico hoy%'`); n != 0 {
		t.Fatal("texto en la auditoría")
	}
	if n := b.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'conversaciones.responder' AND entidad_id = $1`, cid(c)); n < 2 {
		t.Fatalf("auditoría de responder: %d", n)
	}
}

func ejemploVariable(formato string) string {
	switch formato {
	case "fecha":
		return "2026-11-10"
	case "hora":
		return "13:30"
	case "monto":
		return "10.00"
	case "moneda":
		return "GTQ"
	}
	return "Tomás"
}

func TestConvTomarAsignarTransferirResolver(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.e1", "Hola")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)
	// Quien la ve (mencionado) sin ser de la cola no la toma (igual que asignar).
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/notas", map[string]any{"id": nuevoID(), "texto": "mira", "menciones": []int64{b.otro.id}}), http.StatusCreated)
	c = b.conv(num)
	v := ver(c)
	b.exigir(b.api(b.otro, http.MethodPost, ruta+"/tomar", map[string]any{"version": v}), http.StatusUnprocessableEntity)
	// Dos agentes toman a la vez: uno gana, el otro 409 ya_asignada.
	segundo := b.persona([]int64{1}, rolBase("soporte"))
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.cola_miembros (cola_id, usuario_id) VALUES ($1, $2)`, b.cola, segundo.id); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codigos := make([]int, 2)
	for i, p := range []persona{b.agente, segundo} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codigos[i] = b.api(p, http.MethodPost, ruta+"/tomar", map[string]any{"version": v}).status
		}()
	}
	wg.Wait()
	if codigos[0]+codigos[1] != 609 || (codigos[0] != 200 && codigos[1] != 200) {
		t.Fatalf("tomar a la vez: %v", codigos)
	}
	c = b.conv(num)
	// Versión vieja: 409.
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta+"/asignar", map[string]any{"version": v, "usuarioId": b.agente.id}), http.StatusConflict,
		"type", "urn:iris:problema:conflicto_version")
	// Asignar a quien no es de la cola: 422.
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta+"/asignar", map[string]any{"version": ver(c), "usuarioId": b.otro.id}), http.StatusUnprocessableEntity)
	r := b.api(b.supervisor, http.MethodPost, ruta+"/asignar", map[string]any{"version": ver(c), "usuarioId": b.agente.id})
	b.exigir(r, http.StatusOK)
	if r.cuerpo["asignado"].(map[string]any)["id"].(float64) != float64(b.agente.id) {
		t.Fatalf("asignado: %v", r.cuerpo["asignado"])
	}
	// Soporte no asigna a otros (sin conversaciones.asignar).
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/asignar", map[string]any{"version": r.cuerpo["version"], "usuarioId": nil}), http.StatusForbidden)
	// Etiquetas.
	et := b.api(b.supervisor, http.MethodPost, "/etiquetas", map[string]any{"nombre": "Sin servicio " + pruebas.Unico(), "color": "peligro"})
	b.exigir(et, http.StatusCreated)
	r = b.api(b.agente, http.MethodPost, ruta+"/etiquetar", map[string]any{"version": r.cuerpo["version"], "etiquetaIds": []any{et.cuerpo["id"]}})
	b.exigir(r, http.StatusOK)
	if len(r.cuerpo["etiquetas"].([]any)) != 1 {
		t.Fatalf("etiquetas: %v", r.cuerpo["etiquetas"])
	}
	// Transferir a otra cola con resumen: queda sin asignar y con la nota.
	otra := b.api(b.supervisor, http.MethodPost, "/colas", map[string]any{"nombre": "Cobros " + pruebas.Unico()})
	b.exigir(otra, http.StatusCreated)
	r = b.api(b.agente, http.MethodPost, ruta+"/transferir", map[string]any{"version": r.cuerpo["version"], "colaId": otra.cuerpo["id"], "resumen": "Quiere prórroga"})
	b.exigir(r, http.StatusOK)
	if _, ok := r.cuerpo["asignado"]; ok || r.cuerpo["cola"].(map[string]any)["id"] != otra.cuerpo["id"] {
		t.Fatalf("transferida: %v", r.cuerpo)
	}
	// El agente ya no la ve (no es de la otra cola).
	b.exigir(b.api(b.agente, http.MethodGet, ruta, nil), http.StatusNotFound)
	// Resolver y reabrir al escribir el cliente.
	r = b.api(b.supervisor, http.MethodPost, ruta+"/resolver", map[string]any{"version": r.cuerpo["version"]})
	b.exigir(r, http.StatusOK, "estado", "resuelta")
	b.texto(num, "wamid.e2", "Otra vez sin internet")
	c = b.conv(num)
	if c["estado"] != "abierta" || c["reabiertaEn"] == nil {
		t.Fatalf("reabierta: %v", c)
	}
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.reabierta' AND payload->>'conversacion_id' = $1`, cid(c)) != 1 {
		t.Fatal("conversacion.reabierta")
	}
	// Contadores.
	k := b.api(b.supervisor, http.MethodGet, "/conversaciones/contadores", nil)
	b.exigir(k, http.StatusOK)
	if k.cuerpo["todas"].(float64) < 1 {
		t.Fatalf("contadores: %v", k.cuerpo)
	}
}

func TestConvSLAVencido(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.f1", "Hola")
	*b.reloj = b.reloj.Add(50 * time.Minute)
	c := b.conv(num)
	if c["sla"].(map[string]any)["estado"] != "por_vencer" {
		t.Fatalf("por vencer: %v", c["sla"])
	}
	*b.reloj = b.reloj.Add(15 * time.Minute)
	if _, err := b.m.VencerSLA(b.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := b.m.VencerSLA(b.ctx); err != nil {
		t.Fatal(err)
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.sla_vencido' AND payload->>'conversacion_id' = $1`, cid(c)); n != 1 {
		t.Fatalf("sla_vencido una vez: %d", n)
	}
	r := b.api(b.supervisor, http.MethodGet, "/conversaciones?sla=vencido&limit=200", nil)
	b.exigir(r, http.StatusOK)
	if len(r.cuerpo["data"].([]any)) == 0 {
		t.Fatal("filtro sla=vencido")
	}
}

func TestConvMedioEntranteYAdjuntoSaliente(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.webhook(b.mensajeCliente(num, "wamid.g1", `"type":"image","image":{"id":"medio1","mime_type":"image/jpeg","caption":"Foto del módem"}`))
	c := b.conv(num)
	ms := b.mensajes(b.supervisor, cid(c))
	foto := ms[len(ms)-1]
	if foto["tipo"] != "imagen" || foto["texto"] != "Foto del módem" || foto["adjunto"].(map[string]any)["estado"] != "pendiente" {
		t.Fatalf("medio pendiente: %v", foto)
	}
	if len(b.jobsDe("mensajeria.descargar_medio")) != 1 {
		t.Fatal("no se encoló la descarga")
	}
	if err := b.m.DescargarMedio(b.ctx, uuid.MustParse(foto["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	ms = b.mensajes(b.supervisor, cid(c))
	adj := ms[len(ms)-1]["adjunto"].(map[string]any)
	if adj["estado"] != "completo" || adj["mime"] != "image/jpeg" || int(adj["tamano"].(float64)) != len(medioFalso) {
		t.Fatalf("medio descargado: %v", adj)
	}
	// Un medio que Meta ya no tiene: el mensaje queda sin archivo (sin reintentos).
	b.webhook(b.mensajeCliente(num, "wamid.g2", `"type":"document","document":{"id":"perdido","mime_type":"application/pdf","filename":"boleta.pdf"}`))
	ms = b.mensajes(b.supervisor, cid(c))
	if err := b.m.DescargarMedio(b.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	ms = b.mensajes(b.supervisor, cid(c))
	if ms[len(ms)-1]["motivoFalla"] != "medio_no_disponible" {
		t.Fatalf("medio perdido: %v", ms[len(ms)-1])
	}
	// El agente reenvía la foto como adjunto: se sube a Meta y sale como imagen.
	r := b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/mensajes", map[string]any{"id": nuevoID(), "adjuntoId": adj["id"], "texto": "¿Es esta?"})
	b.exigir(r, http.StatusCreated, "tipo", "imagen")
	if _, err := b.m.ProcesarMensaje(b.ctx, uuid.MustParse(r.cuerpo["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	env := b.meta.envios[len(b.meta.envios)-1]
	if env["type"] != "image" || b.meta.medios != 1 {
		t.Fatalf("adjunto a Meta: %v %d", env, b.meta.medios)
	}
	// Un adjunto de otra conversación: 422.
	b.exigir(b.api(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/mensajes", map[string]any{"id": nuevoID(), "adjuntoId": uuid.NewString()}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:adjunto_no_listo")
}

func TestConvCrearTicketYBuscarPorReferencia(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.h1", "Sin servicio")
	c := b.conv(num)
	r := b.apiClave(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/crear-ticket", "tk-"+cid(c), map[string]any{"asunto": "Sin servicio"})
	b.exigir(r, http.StatusCreated)
	v := r.cuerpo["vinculo"].(map[string]any)
	ref := v["referencia"].(string)
	if v["tipo"] != "ticket" || !strings.HasPrefix(ref, "TK-") {
		t.Fatalf("vínculo: %v", v)
	}
	// Repetir con la misma Idempotency-Key no crea otro.
	r2 := b.apiClave(b.agente, http.MethodPost, "/conversaciones/"+cid(c)+"/crear-ticket", "tk-"+cid(c), map[string]any{"asunto": "Sin servicio"})
	b.exigir(r2, http.StatusCreated)
	if r2.cuerpo["vinculo"].(map[string]any)["referencia"] != ref {
		t.Fatalf("repetición: %v", r2.cuerpo["vinculo"])
	}
	ms := b.mensajes(b.agente, cid(c))
	ultimo := ms[len(ms)-1]["evento"].(map[string]any)
	if ultimo["tipo"] != "ticket_creado" || ultimo["referencia"] != ref || ultimo["actorNombre"] == nil {
		t.Fatalf("mensaje de sistema: %v", ultimo)
	}
	l := b.api(b.agente, http.MethodGet, "/conversaciones?q="+ref, nil)
	b.exigir(l, http.StatusOK)
	if d := l.cuerpo["data"].([]any); len(d) != 1 || cid(d[0].(map[string]any)) != cid(c) {
		t.Fatalf("buscar por TK: %v", l.cuerpo)
	}
	// Sin tickets.crear (ventas): 403.
	ventas := b.persona([]int64{1}, rolBase("ventas"))
	b.exigir(b.api(ventas, http.MethodPost, "/conversaciones/"+cid(c)+"/crear-ticket", map[string]any{"asunto": "Otro asunto"}), http.StatusForbidden)
}

func TestConvBoletaYPromesa(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.webhook(b.mensajeCliente(num, "wamid.i1", `"type":"image","image":{"id":"boleta1","mime_type":"image/jpeg"}`))
	c := b.conv(num)
	ms := b.mensajes(b.supervisor, cid(c))
	if err := b.m.DescargarMedio(b.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	ms = b.mensajes(b.supervisor, cid(c))
	adj := ms[len(ms)-1]["adjunto"].(map[string]any)["id"]
	cu := b.api(b.agente, http.MethodGet, "/conversaciones/cuentas-bancarias", nil)
	b.exigir(cu, http.StatusOK)
	if d := cu.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["banco"] != "Banco Industrial" {
		t.Fatalf("cuentas en GTQ: %v", cu.cuerpo)
	}
	b.exigir(b.api(b.lectura, http.MethodGet, "/conversaciones/bancos", nil), http.StatusForbidden)
	boleta := map[string]any{"adjuntoId": adj, "metodo": "deposito", "cuentaBancariaId": 3, "referencia": "123456", "monto": "250.00", "fechaDeposito": "2026-11-04"}
	ruta := "/conversaciones/" + cid(c)
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta), http.StatusUnprocessableEntity, "type", "urn:iris:problema:sin_cliente")
	cliente := b.cliente(1, "5512-0002")
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'María López', now(), '{1}')
		ON CONFLICT DO NOTHING`, cliente); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, sincronizado_en) VALUES ($1, 1, $2, now())`, cliente*10, cliente); err != nil {
		t.Fatal(err)
	}
	r := b.api(b.agente, http.MethodPost, ruta+"/asociar-cliente", map[string]any{"version": ver(b.conv(num)), "clienteId": cliente, "servicioId": cliente * 10})
	b.exigir(r, http.StatusOK)
	// Asociado a mano sin que el número coincida: ni boletas ni promesas hasta que un supervisor confirme.
	if r.cuerpo["contacto"].(map[string]any)["verificado"] != false {
		t.Fatalf("asociación manual sin coincidencia: %v", r.cuerpo["contacto"])
	}
	pr := map[string]any{"servicioId": cliente * 10, "venceEl": "2026-11-20"}
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	b.exigir(b.apiClave(b.agente, http.MethodPost, ruta+"/promesas", "promesa-0", pr), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", pr), http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_sin_confirmar")
	b.exigir(b.api(b.supervisor, http.MethodPost, ruta+"/confirmar-cliente", map[string]any{"version": ver(b.conv(num))}), http.StatusOK)
	if b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conversacion.cliente_confirmado' AND payload->>'conversacion_id' = $1`, cid(c)) != 1 {
		t.Fatal("conversacion.cliente_confirmado")
	}
	// Simular con un servicio de otro cliente: no.
	ajeno := b.cliente(1, "5512-0003")
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas) VALUES ($1, 1, 'Otro', now(), '{1}')
		ON CONFLICT DO NOTHING`, ajeno); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, sincronizado_en) VALUES ($1, 1, $2, now())`, ajeno*10, ajeno); err != nil {
		t.Fatal(err)
	}
	if s := b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", map[string]any{"servicioId": ajeno * 10, "venceEl": "2026-11-20"}); s.status != http.StatusNotFound && s.status != http.StatusUnprocessableEntity {
		t.Fatalf("simular con servicio ajeno: %d %v", s.status, s.cuerpo)
	}
	// Un «JPEG» que no lo es (los bytes mandan): no es boleta.
	b.webhook(b.mensajeCliente(num, "wamid.i2", `"type":"image","image":{"id":"boletatexto","mime_type":"image/jpeg"}`))
	ms = b.mensajes(b.supervisor, cid(c))
	if err := b.m.DescargarMedio(b.ctx, uuid.MustParse(ms[len(ms)-1]["id"].(string)), false); err != nil {
		t.Fatal(err)
	}
	ms = b.mensajes(b.supervisor, cid(c))
	falso := ms[len(ms)-1]["adjunto"].(map[string]any)
	if falso["mime"] == "image/jpeg" {
		t.Fatalf("el tipo sale de los bytes: %v", falso)
	}
	otraBoleta := map[string]any{"adjuntoId": falso["id"], "metodo": "deposito", "cuentaBancariaId": 3, "referencia": "654321", "monto": "250.00", "fechaDeposito": "2026-11-04"}
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/boletas", otraBoleta), http.StatusUnprocessableEntity, "type", "urn:iris:problema:adjunto_no_listo")
	r = b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta)
	b.exigir(r, http.StatusCreated, "estado", "por_enviar")
	if err := b.m.DespacharBoletas(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	var estado string
	var comprobante *int64
	if err := b.bd.QueryRow(b.ctx, `SELECT estado, comprobante_id FROM mensajeria.boletas WHERE id = $1`, r.cuerpo["id"]).Scan(&estado, &comprobante); err != nil {
		t.Fatal(err)
	}
	if estado != "registrada" || comprobante == nil || b.argos.claves[0] != "iris:boleta:"+r.cuerpo["id"].(string) {
		t.Fatalf("boleta en Argos: %s %v %v", estado, comprobante, b.argos.claves)
	}
	// Argos la rechaza por negocio: fallida, sin reintentos.
	b.argos.errComprob = &conector.ErrorRemoto{Status: http.StatusConflict, Tipo: "urn:argos:referencia_duplicada"}
	r = b.api(b.agente, http.MethodPost, ruta+"/boletas", boleta)
	b.exigir(r, http.StatusCreated)
	if err := b.m.DespacharBoletas(b.ctx, b.bd); err != nil {
		t.Fatal(err)
	}
	if err := b.bd.QueryRow(b.ctx, `SELECT estado FROM mensajeria.boletas WHERE id = $1`, r.cuerpo["id"]).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado != "fallida" {
		t.Fatalf("boleta rechazada: %s", estado)
	}

	// Promesa: Argos decide; repetir con la misma clave no pide otra.
	r = b.apiClave(b.agente, http.MethodPost, ruta+"/promesas", "promesa-1", pr)
	b.exigir(r, http.StatusCreated, "estado", "activa")
	pedidas := len(b.argos.claves)
	r2 := b.apiClave(b.agente, http.MethodPost, ruta+"/promesas", "promesa-1", pr)
	b.exigir(r2, http.StatusCreated, "id", r.cuerpo["id"].(string))
	if len(b.argos.claves) != pedidas {
		t.Fatal("la repetición volvió a pedir a Argos")
	}
	b.exigir(b.api(b.agente, http.MethodPost, ruta+"/promesas/simular", pr), http.StatusOK, "resultado", "aprobada")
	// Si mientras Argos respondía la conversación perdió la verificación (o cambió de cliente): 409.
	b.argos.alCrearPromesa = func() {
		if _, err := b.bd.Exec(b.ctx, `UPDATE mensajeria.conversaciones SET cliente_verificado = false WHERE id = $1`, cid(c)); err != nil {
			t.Error(err)
		}
	}
	b.exigir(b.apiClave(b.agente, http.MethodPost, ruta+"/promesas", "promesa-2", map[string]any{"servicioId": cliente * 10, "venceEl": "2026-11-21"}),
		http.StatusConflict, "type", "urn:iris:problema:cliente_cambiado")
	b.argos.alCrearPromesa = nil
	// Desasociar quita también la verificación.
	d := b.api(b.supervisor, http.MethodPost, ruta+"/desasociar", map[string]any{"version": ver(b.conv(num))})
	b.exigir(d, http.StatusOK)
	if d.cuerpo["contacto"].(map[string]any)["verificado"] != false || b.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND (cliente_verificado OR verificado_por IS NOT NULL)`, cid(c)) != 0 {
		t.Fatalf("desasociar deja verificado: %v", d.cuerpo["contacto"])
	}
	if n := b.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'promesa.solicitada' AND payload->>'conversacion_id' = $1`, cid(c)); n != 1 {
		t.Fatalf("promesa.solicitada: %d", n)
	}
}

func TestConvVariosNumerosYAvisos(t *testing.T) {
	b := nuevaBandeja(t, nil)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES (3, 'Empresa 3', 'argos') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	empresa := int64(3)
	crear := func(nombre string, usos []string) mensajeria.Canal {
		c, err := b.m.CrearCanal.Ejecutar(b.ctx, mensajeria.EntradaCrearCanal{EmpresaID: &empresa, Nombre: nombre, Numero: "+50222000020",
			PhoneNumberID: fmt.Sprint(time.Now().UnixNano() % 1e12), Token: "token-de-meta-0123456789abcdef", AppSecret: secretoWA,
			VerifyToken: "verify-token-0123456789", Usos: usos})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	avisos := crear("Avisos", []string{"avisos", "soporte"})
	cobros := crear("Cobros", []string{"cobros"})
	ventas := crear("Ventas y avisos", []string{"ventas", "avisos"})
	if !avisos.AvisosEmpresa || cobros.AvisosEmpresa || ventas.AvisosEmpresa {
		t.Fatalf("solo el primero con avisos es el de avisos: %v %v %v", avisos.AvisosEmpresa, cobros.AvisosEmpresa, ventas.AvisosEmpresa)
	}
	// Marcar otro lo quita del anterior.
	admin := b.persona([]int64{3}, rolBase("supervisor"))
	r := b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/configurar", ventas.ID), map[string]any{"version": ventas.Version, "usos": []string{"ventas", "avisos"}, "avisosEmpresa": true})
	b.exigir(r, http.StatusOK, "avisosEmpresa", "true")
	if b.contar(`SELECT count(*) FROM mensajeria.canales WHERE empresa_id = 3 AND activo AND avisos_empresa`) != 1 {
		t.Fatal("un solo número de avisos por empresa")
	}
	b.exigir(b.api(admin, http.MethodPost, fmt.Sprintf("/canales/%d/configurar", cobros.ID), map[string]any{"version": cobros.Version, "usos": []string{"cobros"}, "avisosEmpresa": true}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:numero_avisos_requiere_uso")
	// Los números se listan para quien solo atiende (sin credenciales).
	agente := b.persona([]int64{3}, rolBase("soporte"))
	l := b.api(agente, http.MethodGet, "/conversaciones/canales", nil)
	b.exigir(l, http.StatusOK)
	if n := len(l.cuerpo["data"].([]any)); n != 3 {
		t.Fatalf("números: %d", n)
	}
	if strings.Contains(fmt.Sprint(l.cuerpo), "phoneNumberId") {
		t.Fatal("ids de Meta en la lista de la bandeja")
	}
	// Agentes de la empresa y plantillas del número.
	ag := b.api(b.agente, http.MethodGet, "/conversaciones/agentes", nil)
	b.exigir(ag, http.StatusOK)
	sup := false
	for _, x := range ag.cuerpo["data"].([]any) {
		a := x.(map[string]any)
		if a["id"].(float64) == float64(b.supervisor.id) {
			sup = a["supervisa"] == true
		}
	}
	if !sup {
		t.Fatal("el supervisor aparece como supervisa")
	}
	num := numeroCliente()
	b.texto(num, "wamid.j1", "Hola")
	pl := b.api(b.agente, http.MethodGet, "/conversaciones/"+cid(b.conv(num))+"/plantillas", nil)
	b.exigir(pl, http.StatusOK)
	if d := pl.cuerpo["data"].([]any); len(d) != 1 || d[0].(map[string]any)["codigo"] != "bienvenida" {
		t.Fatalf("plantillas del número: %v", pl.cuerpo)
	}
	_ = identidad.CabeceraEmpresa
}
