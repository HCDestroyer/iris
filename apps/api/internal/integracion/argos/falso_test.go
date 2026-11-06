package argos_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// argosFalso es un servidor de Argos que cumple el contrato (§1.3 y §2.1) para las pruebas.
// Responde a propósito con estilos distintos (camelCase, { data }, arreglos planos, números
// como texto) para cubrir la lectura tolerante del cliente.
type argosFalso struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	token      string
	codigo     string
	canjes     []map[string]any
	clientes   map[int64]map[string]any
	servicios  map[int64][]map[string]any
	estados    map[int64]map[string]any
	ips        map[string]map[string]any
	zonas      []map[string]any
	routers    []map[string]any
	caido      bool
	fallarUna  int // responde 503 a las próximas n peticiones
	sinA8      bool
	llamadas   map[string]int
	sinPermiso bool
	// empresaPedida: X-Empresa-Id de la última búsqueda.
	empresaPedida string
	// alcance: empresas del token de Iris en Argos (vacío = global). Sin global, ?empresas=todas
	// responde 403 y /servicios solo devuelve las de su alcance.
	alcance map[int64]bool
	// estadoPedido: ?empresa_id= del último estado de cuenta.
	estadoPedido string
	conectorID   any // lo que responde el canje en conector_id (nil = "77")
	// empresasCanje y alcanceGlobal: lo que responde el canje (0.3.1). nil = como Argos 1.3.0
	// (empresas [{ id: 1 }] y sin alcance_global).
	empresasCanje []any
	alcanceGlobal *bool
	// proxy404: rutas a las que un proxy responde 404 en HTML (no es «no encontrado» de Argos).
	proxy404 map[string]bool
	// fueraDeAlcance: rutas por id a las que Argos responde 403 empresa_no_permitida (el recurso
	// existe fuera del alcance del token; nunca 404).
	fueraDeAlcance map[string]bool
	// sinGet: servicios que la lista todavía trae (réplica atrasada) pero el GET por id ya no.
	sinGet map[int64]bool
	// desafio: code_challenge al que quedó ligado el código vigente (§1.4); vacío = código de §1.3.
	desafio string
	// panelURL: lo que responde el canje en panel_url (0.4.0); vacío = no lo manda.
	panelURL string
	// Contrato 0.5.0 (Argos ≥ 1.6.0): version vacía = Argos 1.5.x (no conoce las rutas nuevas ni
	// informa versión). contrato: el que registró para Iris (canje o PUT …/contrato).
	version  string
	contrato string
	tecnicos []map[string]any
	// retrasoTecnicos: cuánto tarda cada página de GET /inventario/tecnicos (Argos lento).
	retrasoTecnicos time.Duration
	planes          []map[string]any
	contratos       int // PUT /integraciones/iris/contrato recibidos
	// webhookURL: el receptor que responde el canje (vacío = el de su propio host).
	webhookURL string
	// sinWebhook: el canje no devuelve el bloque webhook (Argos fuera de contrato, 0.6.2).
	sinWebhook bool
	// receptores: receptor_argos de cada PUT …/contrato (0.6.2; nil = no vino).
	receptores []any
	// Contrato 0.6.3 §1.3. tokenCanje: el atk_ que entrega el próximo canje (vacío = token). Con un
	// token en uso, el canje nuevo queda pendiente hasta la primera llamada autenticada con su atk_:
	// entonces reemplaza a token (y el anterior deja de autenticar).
	tokenCanje string
	pendiente  string
	// usos: cada llamada autenticada, en orden («METODO /ruta» y el token con que llegó).
	usos []usoToken
	// revocaciones: cada POST …/conexion/revocar recibido. respuestasRevocar: status que responde,
	// por token y en orden (204, 401, 403, 503…); sin respuesta configurada, como Argos: 204 si el
	// token autentica (y lo revoca) y 401 si no.
	revocaciones      []revocacion
	respuestasRevocar map[string][]int
	// retryAfter: Retry-After que acompaña a un 429 de revocar, por token (vacío = sin cabecera).
	retryAfter map[string]string
}

type usoToken struct {
	ruta, token string
}

type revocacion struct {
	token  string
	cuerpo int
	status int
}

const tokenArgos = "atk_prueba_0123456789abcdef"

func nuevoArgosFalso(t *testing.T) *argosFalso {
	a := &argosFalso{
		t: t, token: tokenArgos, codigo: "codigo-de-argos-0123456789",
		clientes: map[int64]map[string]any{}, servicios: map[int64][]map[string]any{},
		estados: map[int64]map[string]any{}, ips: map[string]map[string]any{}, llamadas: map[string]int{},
		proxy404: map[string]bool{}, fueraDeAlcance: map[string]bool{}, sinGet: map[int64]bool{},
		respuestasRevocar: map[string][]int{}, retryAfter: map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/canjear", a.canjear)
	mux.HandleFunc("POST /api/v1/integraciones/iris/conexion/revocar", a.revocar)
	mux.HandleFunc("GET /api/v1/clientes", a.conToken(a.buscar))
	mux.HandleFunc("GET /api/v1/clientes/{id}", a.conToken(a.cliente))
	mux.HandleFunc("GET /api/v1/clientes/{id}/estado-cuenta", a.conToken(a.estadoCuenta))
	mux.HandleFunc("GET /api/v1/servicios", a.conToken(a.serviciosDe))
	mux.HandleFunc("GET /api/v1/servicios/{id}", a.conToken(a.servicio))
	mux.HandleFunc("POST /api/v1/integraciones/iris/resolver", a.conToken(a.resolverA8))
	mux.HandleFunc("GET /api/v1/ip/{ip}", a.conToken(a.resolverIP))
	mux.HandleFunc("GET /api/v1/zonas", a.conToken(func(w http.ResponseWriter, r *http.Request) { a.paginar(w, r, a.zonas) }))
	mux.HandleFunc("GET /api/v1/routers", a.conToken(func(w http.ResponseWriter, r *http.Request) { a.paginar(w, r, a.routers) }))
	mux.HandleFunc("GET /api/v1/routers/{id}", a.conToken(a.router))
	a.rutas050(mux)
	a.srv = httptest.NewServer(mux)
	t.Cleanup(a.srv.Close)
	return a
}

// rutas050: lo nuevo de Argos 1.6.0 (contrato 0.5.0). Con version vacía responde 404 (Argos 1.5.x).
func (a *argosFalso) rutas050(mux *http.ServeMux) {
	solo16 := func(fn http.HandlerFunc) http.HandlerFunc {
		return a.conToken(func(w http.ResponseWriter, r *http.Request) {
			if a.version == "" {
				problema(w, 404, "urn:argosisp:problema:no_encontrado", "Ruta no encontrada")
				return
			}
			fn(w, r)
		})
	}
	mux.HandleFunc("PUT /api/v1/integraciones/iris/contrato", solo16(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		// 0.6.2: receptor_argos?: boolean; otro tipo es 400 como en Argos.
		if v, ok := p["receptor_argos"]; ok {
			if _, esBool := v.(bool); !esBool {
				problema(w, 400, "urn:argosisp:problema:validacion", "receptor_argos debe ser boolean")
				return
			}
		}
		a.contrato, _ = p["contrato"].(string)
		a.contratos++
		a.receptores = append(a.receptores, p["receptor_argos"])
		escribir(w, map[string]any{"contrato": a.contrato, "version_argos": a.version})
	}))
	mux.HandleFunc("GET /api/v1/integraciones/iris/estado", solo16(func(w http.ResponseWriter, _ *http.Request) {
		escribir(w, map[string]any{"version_argos": a.version, "contrato": a.contrato, "activo": true})
	}))
	mux.HandleFunc("GET /api/v1/inventario/tecnicos", solo16(func(w http.ResponseWriter, r *http.Request) {
		if a.retrasoTecnicos > 0 {
			select {
			case <-time.After(a.retrasoTecnicos):
			case <-r.Context().Done():
				return
			}
		}
		a.paginar(w, r, a.tecnicos)
	}))
	mux.HandleFunc("GET /api/v1/planes", a.conToken(func(w http.ResponseWriter, r *http.Request) { a.paginar(w, r, a.planes) }))
}

func (a *argosFalso) enlace() string { return a.srv.URL + "/conectar#" + a.codigo }

// aprobar hace lo que el panel de Argos al pulsar «Aprobar» (§1.4 pasos 3 y 4) con la URL de
// autorizar que armó Iris: valida la consulta, crea un código ligado al code_challenge y devuelve
// el fragmento de la vuelta. desafioAlterado liga el código a otro challenge (un code_verifier
// que no corresponde).
func (a *argosFalso) aprobar(autorizar string, desafioAlterado bool) (codigo, api, state string) {
	a.t.Helper()
	u, err := url.Parse(autorizar)
	if err != nil {
		a.t.Fatalf("URL de autorizar: %v", err)
	}
	q := u.Query()
	if u.Path != "/integraciones/iris/autorizar" || q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 ||
		q.Get("iris") == "" || len(q.Get("state")) != 43 || u.Fragment != "" {
		a.t.Fatalf("URL de autorizar fuera de contrato: %s", autorizar)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.codigo = "codigo-boton-" + q.Get("state")[:12] + "-0123456789"
	a.desafio = q.Get("code_challenge")
	if desafioAlterado {
		a.desafio = strings.Repeat("A", 43)
	}
	return a.codigo, a.srv.URL, q.Get("state")
}

func (a *argosFalso) llamadasA(ruta string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.llamadas[ruta]
}

func (a *argosFalso) con(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn()
}

func problema(w http.ResponseWriter, status int, tipo, detalle string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": tipo, "title": http.StatusText(status), "status": status, "detail": detalle})
}

func escribir(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *argosFalso) canjear(w http.ResponseWriter, r *http.Request) {
	var p map[string]any
	_ = json.NewDecoder(r.Body).Decode(&p)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.canjes = append(a.canjes, p)
	if r.Header.Get("Authorization") != "" {
		problema(w, 400, "urn:argos:token_no_esperado", "El canje no lleva token")
		return
	}
	if p["codigo"] != a.codigo || !canjeCanonico(p) {
		problema(w, 422, "urn:argos:codigo_invalido", "El código venció o ya se usó")
		return
	}
	// §1.4: un código nacido con code_challenge exige code_verifier S256; si no coincide, 401
	// codigo_invalido y el código queda gastado. Uno de §1.3 no lo pide.
	if a.desafio != "" {
		verificador, _ := p["code_verifier"].(string)
		suma := sha256.Sum256([]byte(verificador))
		if base64.RawURLEncoding.EncodeToString(suma[:]) != a.desafio {
			a.codigo = "usado-" + a.codigo
			problema(w, 401, "urn:argos:codigo_invalido", "El code_verifier no corresponde")
			return
		}
	}
	a.codigo = "usado-" + a.codigo
	token := a.token
	if a.tokenCanje != "" {
		// 0.6.3: sobre un conector en uso, el canje queda pendiente; si no, lo reemplaza al momento.
		token, a.tokenCanje = a.tokenCanje, ""
		if a.token != "" {
			a.pendiente = token
		} else {
			a.token = token
		}
	}
	// Como Argos 1.3.0: su token, su receptor (reservado) y el id del conector allá. Las empresas no
	// las manda Argos; el falso las agrega para registrar la empresa 1 (campo opcional).
	// El receptor en el mismo host que la API de Argos (contrato 0.6.0 §6.6); webhookURL lo cambia.
	receptor := a.srv.URL + "/api/v1/integraciones/iris/webhook"
	if a.webhookURL != "" {
		receptor = a.webhookURL
	}
	resp := map[string]any{"token": token, "webhook": map[string]any{"url": receptor, "secreto": "secreto-de-argos-para-iris"},
		"conector_id": a.idDelCanje(), "empresas": []any{map[string]any{"id": 1}}}
	if a.sinWebhook {
		delete(resp, "webhook")
	}
	if a.empresasCanje != nil {
		resp["empresas"] = a.empresasCanje
	}
	if a.alcanceGlobal != nil {
		resp["alcance_global"] = *a.alcanceGlobal
	}
	if a.panelURL != "" {
		resp["panel_url"] = a.panelURL
	}
	if a.version != "" {
		// Argos 1.6.0: informa su versión y registra el contrato que mandó Iris.
		a.contrato, _ = p["contrato"].(string)
		resp["version_argos"], resp["contrato"] = a.version, a.contrato
	}
	escribir(w, resp)
}

func (a *argosFalso) idDelCanje() any {
	if a.conectorID != nil {
		return a.conectorID
	}
	return "77"
}

func (a *argosFalso) conToken(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.llamadas[r.URL.Path]++
		caido := a.caido || a.fallarUna > 0
		if a.fallarUna > 0 {
			a.fallarUna--
		}
		sinPermiso := a.sinPermiso
		proxy := a.proxy404[r.URL.Path]
		fuera := a.fueraDeAlcance[r.URL.Path]
		a.mu.Unlock()
		if fuera {
			problema(w, 403, "urn:argosisp:problema:empresa_no_permitida", "Fuera del alcance del token")
			return
		}
		if proxy {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<html><body>404 Not Found</body></html>"))
			return
		}
		if !a.autenticar(r) {
			problema(w, 401, "urn:argos:no_autenticado", "Token inválido")
			return
		}
		if caido {
			problema(w, 503, "urn:argos:no_disponible", "Mantenimiento")
			return
		}
		if sinPermiso {
			problema(w, 403, "urn:argos:prohibido", "Falta el permiso")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		fn(w, r)
	}
}

// autenticar acepta el token en uso o el pendiente de una reconexión (0.6.3): la primera llamada
// con el pendiente lo aplica y el anterior deja de autenticar. Registra el uso.
func (a *argosFalso) autenticar(r *http.Request) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case token != "" && token == a.token:
	case token != "" && token == a.pendiente:
		a.token, a.pendiente = a.pendiente, ""
	default:
		return false
	}
	a.usos = append(a.usos, usoToken{ruta: r.Method + " " + r.URL.Path, token: token})
	return true
}

// revocar: POST /integraciones/iris/conexion/revocar (0.6.3 §1.3), con el atk_ como Bearer y sin
// cuerpo.
func (a *argosFalso) revocar(w http.ResponseWriter, r *http.Request) {
	cuerpo, _ := io.ReadAll(r.Body)
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	a.mu.Lock()
	defer a.mu.Unlock()
	status := http.StatusUnauthorized
	if cola := a.respuestasRevocar[token]; len(cola) > 0 {
		status, a.respuestasRevocar[token] = cola[0], cola[1:]
	} else if token != "" && (token == a.token || token == a.pendiente) {
		status = http.StatusNoContent
	}
	a.revocaciones = append(a.revocaciones, revocacion{token: token, cuerpo: len(cuerpo), status: status})
	switch status {
	case http.StatusNoContent:
		// Pendiente: se descarta y el conector sigue igual. En uso: queda desconectado.
		switch token {
		case a.pendiente:
			a.pendiente = ""
		case a.token:
			a.token = ""
		}
		w.WriteHeader(http.StatusNoContent)
	case http.StatusUnauthorized:
		problema(w, status, "urn:argosisp:problema:no_autenticado", "Token inválido")
	case http.StatusForbidden:
		problema(w, status, "urn:argosisp:problema:prohibido", "Otra credencial")
	case http.StatusTooManyRequests:
		if v := a.retryAfter[token]; v != "" {
			w.Header().Set("Retry-After", v)
		}
		problema(w, status, "urn:argosisp:problema:demasiadas_peticiones", "Demasiadas peticiones")
	default:
		problema(w, status, "urn:argosisp:problema:no_disponible", "Mantenimiento")
	}
}

// revocacionesDe devuelve las revocaciones recibidas con ese token.
func (a *argosFalso) revocacionesDe(token string) []revocacion {
	a.mu.Lock()
	defer a.mu.Unlock()
	var r []revocacion
	for _, x := range a.revocaciones {
		if x.token == token {
			r = append(r, x)
		}
	}
	return r
}

func (a *argosFalso) buscar(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	if v := r.URL.Query().Get("empresas"); v != "" && v != "todas" {
		// Como Argos 1.3.0: ?empresas= solo acepta «todas»; la empresa va en X-Empresa-Id.
		problema(w, 422, "urn:argos:validacion", "Los datos enviados no son válidos")
		return
	}
	a.empresaPedida = r.Header.Get("X-Empresa-Id") // conToken ya tiene el candado
	data := []any{}
	for _, c := range a.clientes {
		if strings.Contains(strings.ToLower(c["nombre"].(string)), q) {
			data = append(data, c)
		}
	}
	escribir(w, map[string]any{"data": data, "nextCursor": nil})
}

func idDe(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (a *argosFalso) cliente(w http.ResponseWriter, r *http.Request) {
	c, ok := a.clientes[idDe(r)]
	if !ok {
		problema(w, 404, "urn:argosisp:problema:no_encontrado", "No existe")
		return
	}
	escribir(w, map[string]any{"data": c})
}

func (a *argosFalso) estadoCuenta(w http.ResponseWriter, r *http.Request) {
	a.estadoPedido = r.URL.Query().Get("empresa_id")
	e, ok := a.estados[idDe(r)]
	if !ok {
		problema(w, 404, "urn:argosisp:problema:no_encontrado", "No existe")
		return
	}
	escribir(w, e)
}

func (a *argosFalso) serviciosDe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.URL.Query().Get("cliente_id"), 10, 64)
	todas := r.URL.Query().Get("empresas") == "todas"
	if todas && len(a.alcance) > 0 {
		// Como Argos 1.3.0: el consolidado exige alcance global.
		problema(w, 403, "urn:argos:empresa_no_permitida", "El consolidado de todas las empresas requiere alcance global")
		return
	}
	lista := []map[string]any{}
	for _, s := range a.servicios[id] {
		// Sin «todas», solo lo del alcance del token (si el falso tiene uno).
		if !todas && len(a.alcance) > 0 && !a.alcance[enteroDe(s["empresa_id"])] {
			continue
		}
		lista = append(lista, s)
	}
	escribir(w, lista) // arreglo plano, sin { data }
}

func enteroDe(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func (a *argosFalso) servicio(w http.ResponseWriter, r *http.Request) {
	if a.sinGet[idDe(r)] {
		problema(w, 404, "urn:argosisp:problema:no_encontrado", "No existe")
		return
	}
	for _, lista := range a.servicios {
		for _, s := range lista {
			if id, _ := s["id"].(int64); id == idDe(r) {
				escribir(w, s)
				return
			}
		}
	}
	problema(w, 404, "urn:argosisp:problema:no_encontrado", "No existe")
}

func (a *argosFalso) router(w http.ResponseWriter, r *http.Request) {
	for _, rt := range a.routers {
		if enteroDe(rt["id"]) == idDe(r) {
			escribir(w, map[string]any{"data": rt})
			return
		}
	}
	problema(w, 404, "urn:argosisp:problema:no_encontrado", "No existe")
}

// quitarServicio borra un servicio como lo hace Argos al eliminarlo (0.3.1). Llamar dentro de con.
func (a *argosFalso) quitarServicio(id int64) {
	for c, lista := range a.servicios {
		for i, s := range lista {
			if enteroDe(s["id"]) == id {
				a.servicios[c] = append(lista[:i:i], lista[i+1:]...)
				return
			}
		}
	}
}

// quitarRouter borra un router (0.3.1). Llamar dentro de con.
func (a *argosFalso) quitarRouter(id int64) {
	for i, rt := range a.routers {
		if enteroDe(rt["id"]) == id {
			a.routers = append(a.routers[:i:i], a.routers[i+1:]...)
			return
		}
	}
}

func (a *argosFalso) resolverA8(w http.ResponseWriter, r *http.Request) {
	if a.sinA8 {
		problema(w, 404, "urn:argos:ruta", "Ruta inexistente")
		return
	}
	var p struct {
		IP string `json:"ip"`
	}
	_ = json.NewDecoder(r.Body).Decode(&p)
	res, ok := a.ips[p.IP]
	if !ok {
		problema(w, 404, "urn:argosisp:problema:no_encontrado", "IP sin cliente")
		return
	}
	escribir(w, res)
}

func (a *argosFalso) resolverIP(w http.ResponseWriter, r *http.Request) {
	res, ok := a.ips[r.PathValue("ip")]
	if !ok {
		problema(w, 404, "urn:argosisp:problema:no_encontrado", "IP sin cliente")
		return
	}
	// El respaldo responde anidado.
	escribir(w, map[string]any{"servicio": map[string]any{"id": res["servicio_id"], "clienteId": res["cliente_id"], "empresaId": res["empresa_id"]}})
}

// paginar devuelve de a 2 elementos con cursor.
func (a *argosFalso) paginar(w http.ResponseWriter, r *http.Request, todos []map[string]any) {
	desde, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	hasta := min(desde+2, len(todos))
	var siguiente any
	if hasta < len(todos) {
		siguiente = strconv.Itoa(hasta)
	}
	escribir(w, map[string]any{"data": todos[desde:hasta], "nextCursor": siguiente})
}

// canjeCanonico valida el cuerpo del canje como el contrato Argos ↔ Horus §1.4 (y Argos 1.3.0):
// sistema "iris", base_url, el token iris_pat_ que Iris emite para el otro sistema y su receptor de
// webhooks con el secreto.
func canjeCanonico(p map[string]any) bool {
	token, _ := p["token"].(string)
	base, _ := p["base_url"].(string)
	wh, _ := p["webhook"].(map[string]any)
	url, _ := wh["url"].(string)
	secreto, _ := wh["secreto"].(string)
	// 0.6.2: receptor_argos?: boolean (si viene, debe ser boolean).
	if v, ok := p["receptor_argos"]; ok {
		if _, esBool := v.(bool); !esBool {
			return false
		}
	}
	return p["sistema"] == "iris" && base != "" && strings.HasPrefix(token, "iris_pat_") && url != "" && len(secreto) >= 16
}
