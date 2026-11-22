package comercial_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/servidor"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Portal del cliente de Argos (contrato 0.9.0 §11, B8–B11, B13; §11.10 de 0.12.1, B28), por HTTP
// contra Postgres real: el token_portal, las cabeceras, la vista reducida, la idempotencia, los
// adjuntos, la confirmación, los contactos a cargo y las solicitudes de servicio como prospectos.

const (
	clientePortal = int64(8850)
	otroCliente   = int64(8851)
)

type portalArgos struct {
	t      *testing.T
	e      *entorno
	base   string
	portal string // token_portal
	panel  string // el iris_pat_ del canje (panel de Argos)
}

func nuevoPortalArgos(t *testing.T) *portalArgos {
	t.Helper()
	limpiarConector(t)
	bd := pruebas.Pool(t)
	t.Cleanup(func() {
		for _, sql := range []string{
			"DELETE FROM integracion.argos_contactos", "DELETE FROM integracion.portal_argos_minutos",
			"DELETE FROM integracion.argos_servicios WHERE cliente_id IN (8850, 8851)",
			"DELETE FROM integracion.argos_clientes WHERE id IN (8850, 8851)",
		} {
			if _, err := bd.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	e := nuevoEntorno(t, time.Now)
	e.conectar()
	ctx := pruebas.ContextoSistema(bd)
	// El usuario de integración y su iris_pat_ (el del canje), con el token_portal hijo.
	u, err := ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Argos", Correo: pruebas.Correo("argos-portal"),
		Tipo: "integracion", Roles: []identidad.AsignacionRol{{Rol: "integracion_argos"}}, Empresas: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	panel, err := ident.CrearToken.Ejecutar(ctx, identidad.EntradaCrearToken{UsuarioID: u.ID, Nombre: "canje"})
	if err != nil {
		t.Fatal(err)
	}
	_, portal, err := ident.EmitirTokenPortal(ctx, panel.Token.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bd.Exec(context.Background(), `UPDATE integracion.conectores SET token_entregado_id = $1 WHERE id = $2`, panel.Token.ID, e.conID); err != nil {
		t.Fatal(err)
	}
	// La instantánea: el cliente con dos servicios en la empresa 1 y otro cliente.
	for _, sql := range []string{
		`INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, telefono, correo, direccion, zona_id, sincronizado_en)
		 VALUES (8850, 1, '{1}', 'María Pérez', '+502 5512-3344', 'maria@correo.test', '5a avenida 10-20', 7001, now()),
		        (8851, 1, '{1}', 'Otro Cliente', '', '', '', NULL, now())`,
		`INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, direccion, zona_id, sincronizado_en)
		 VALUES (4410, 1, 8850, 'Casa', 7001, now()), (4411, 1, 8850, 'Oficina', 7001, now()), (4412, 1, 8851, 'Otra', NULL, now())`,
	} {
		if _, err := bd.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
	almacen, err := adjuntos.NuevoDisco(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sop := soporte.Nuevo(soporte.Config{PortalArgos: e.am, Almacen: almacen})
	com := comercial.Nuevo(comercial.Config{Planes: comercial.PlanesDeArgos(e.am), Altas: comercial.AltasDeArgos(e.am), Campo: e.cmp,
		Clientes: e.am, PortalArgos: e.am})
	e.com = com
	h, _ := servidor.Nuevo(servidor.Deps{
		BD: bd, Identidad: ident, Version: "prueba", Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Idempotencia: nucleo.OpcionesIdempotencia{Regla: nucleo.PrimeraRegla(comercial.ReglaIdempotenciaArgos, argos.ReglaIdempotenciaPortal)},
		Modulos:      []func(huma.API){sop.Registrar, com.Registrar},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &portalArgos{t: t, e: e, base: srv.URL, portal: portal, panel: panel.Valor}
}

func rutaPortal(cliente int64, resto string) string {
	return "/api/v1/integraciones/argosisp/portal/clientes/" + strconv.FormatInt(cliente, 10) + resto
}

// pedir con las cabeceras de una sesión de documento del titular (se pueden pisar).
func (p *portalArgos) pedir(metodo string, cliente int64, ruta string, cuerpo any, cabeceras ...string) respuesta {
	p.t.Helper()
	c := &clienteHTTP{t: p.t, base: p.base, http: http.DefaultClient}
	base := []string{"Authorization", "Bearer " + p.portal, "X-Iris-Empresa", "1", "X-Argos-Portal-Sesion", "documento:sesion-abc-123",
		"X-Iris-Contacto", "titular"}
	if metodo == http.MethodPost {
		base = append(base, "Idempotency-Key", "argos:portal:prueba:"+pruebas.Unico())
	}
	return c.pedir(metodo, rutaPortal(cliente, ruta), cuerpo, append(base, cabeceras...)...)
}

func (p *portalArgos) multipart(cliente int64, ruta string, datos any, archivos map[string][]byte, cabeceras ...string) respuesta {
	p.t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	j, _ := json.Marshal(datos)
	_ = w.WriteField("datos", string(j))
	for nombre, contenido := range archivos {
		f, _ := w.CreateFormFile("archivos", nombre)
		_, _ = f.Write(contenido)
	}
	_ = w.Close()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.base+rutaPortal(cliente, ruta), &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	todas := append([]string{"Authorization", "Bearer " + p.portal, "X-Iris-Empresa", "1", "X-Argos-Portal-Sesion", "documento:sesion-abc-123",
		"X-Iris-Contacto", "titular", "Idempotency-Key", "argos:portal:ticket:" + pruebas.Unico()}, cabeceras...)
	for i := 0; i+1 < len(todas); i += 2 {
		req.Header.Set(todas[i], todas[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	crudo, _ := io.ReadAll(res.Body)
	r := respuesta{status: res.StatusCode, crudo: crudo, h: res.Header}
	_ = json.Unmarshal(crudo, &r.cuerpo)
	return r
}

func fotoPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	// Un bloque tEXt (con su CRC) con un dato que no debe quedar guardado (metadatos).
	crudo := b.Bytes()
	datos := []byte("tEXtGPS\x00secreto")
	bloque := binary.BigEndian.AppendUint32(nil, uint32(len(datos)-4))
	bloque = append(bloque, datos...)
	bloque = binary.BigEndian.AppendUint32(bloque, crc32.ChecksumIEEE(datos))
	return append(append(append([]byte(nil), crudo[:33]...), bloque...), crudo[33:]...)
}

func TestPortalArgosTickets(t *testing.T) {
	p := nuevoPortalArgos(t)
	bd := pruebas.Pool(t)

	// El iris_pat_ del panel de Argos no sirve para el portal (S5/§1.2).
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil, "Authorization", "Bearer "+p.panel).problema(t, http.StatusForbidden, "solo_integracion_argos")
	// Cabeceras obligatorias.
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil, "X-Argos-Portal-Sesion", "documento:corta").problema(t, http.StatusBadRequest, "cabecera_requerida")
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil, "X-Iris-Empresa", "").problema(t, http.StatusBadRequest, "cabecera_requerida")
	p.pedir(http.MethodPost, clientePortal, "/tickets", map[string]any{"asunto": "x", "descripcion": "y"}, "Idempotency-Key", "").
		problema(t, http.StatusBadRequest, "idempotency_key_requerida")
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil).ok(t, http.StatusOK)

	// Abrir un ticket (§11.4).
	clave := "argos:portal:ticket:" + pruebas.Unico()
	cuerpo := map[string]any{"asunto": "Sin internet desde la mañana", "descripcion": "La luz LOS está en rojo.", "servicio_id": 4410}
	tk := p.pedir(http.MethodPost, clientePortal, "/tickets", cuerpo, "Idempotency-Key", clave).ok(t, http.StatusCreated)
	id := int64(tk["id"].(float64))
	if tk["origen"] != "portal_argos" || tk["vista"] != "completa" || tk["estado"] != "nuevo" || tk["puede_confirmar"] != false {
		t.Fatalf("ticket: %v", tk)
	}
	// La misma clave y el mismo cuerpo: el mismo ticket; otro cuerpo: 422.
	if r := p.pedir(http.MethodPost, clientePortal, "/tickets", cuerpo, "Idempotency-Key", clave).ok(t, http.StatusCreated); r["id"] != tk["id"] {
		t.Fatalf("repetido: %v", r)
	}
	cuerpo["asunto"] = "Otro asunto"
	p.pedir(http.MethodPost, clientePortal, "/tickets", cuerpo, "Idempotency-Key", clave).problema(t, http.StatusUnprocessableEntity, "idempotencia_reutilizada")
	var autor, canal string
	if err := bd.QueryRow(context.Background(), `SELECT autor_tipo, canal FROM soporte.ticket_mensajes WHERE ticket_id = $1`, id).Scan(&autor, &canal); err != nil {
		t.Fatal(err)
	}
	if autor != "cliente" || canal != "portal_argos" {
		t.Fatalf("mensaje: %s %s", autor, canal)
	}
	var auditado int
	if err := bd.QueryRow(context.Background(), `SELECT count(*) FROM auditoria.acciones WHERE accion = 'portal_argos.crear_ticket'
		AND entrada->'auditoria'->'en_nombre_de'->>'sesion' = 'sesion-abc-123' AND entrada->'auditoria'->'en_nombre_de'->>'metodo' = 'documento'
		AND canal = 'integracion' AND actor_nombre = 'Argos (portal) · cliente 8850'`).Scan(&auditado); err != nil {
		t.Fatal(err)
	}
	if auditado == 0 {
		var crudo string
		_ = bd.QueryRow(context.Background(), `SELECT entrada::text FROM auditoria.acciones WHERE accion = 'portal_argos.crear_ticket' ORDER BY id DESC LIMIT 1`).Scan(&crudo)
		t.Fatalf("sin auditoría con en_nombre_de: %s", crudo)
	}

	// Sesión ip: lista reducida, sin asunto; el detalle y los mensajes, 403.
	ip := []string{"X-Argos-Portal-Sesion", "ip:sesion-ip-0001"}
	lista := p.pedir(http.MethodGet, clientePortal, "/tickets", nil, ip...).ok(t, http.StatusOK)
	primero := lista["data"].([]any)[0].(map[string]any)
	if primero["vista"] != "reducida" || primero["asunto"] != nil || primero["numero"] == nil {
		t.Fatalf("lista ip: %v", primero)
	}
	p.pedir(http.MethodGet, clientePortal, "/tickets/"+strconv.FormatInt(id, 10), nil, ip...).problema(t, http.StatusForbidden, "sesion_insuficiente")
	p.multipart(clientePortal, "/tickets", map[string]any{"asunto": "Con foto", "descripcion": "x"}, map[string][]byte{"f.png": fotoPNG(t)}, ip...).
		problema(t, http.StatusUnprocessableEntity, "adjuntos_no_permitidos")

	// De otro cliente: 404, como uno que no existe.
	p.pedir(http.MethodGet, otroCliente, "/tickets/"+strconv.FormatInt(id, 10), nil).problema(t, http.StatusNotFound, "no_encontrado")

	// Responder con una foto (multipart): el archivo se guarda sin metadatos y se descarga.
	r := p.multipart(clientePortal, "/tickets/"+strconv.FormatInt(id, 10)+"/responder", map[string]any{"cuerpo": "Les mando la foto"},
		map[string][]byte{"../C:\\mi \"foto\".png": fotoPNG(t)}, "Idempotency-Key", "argos:portal:respuesta:"+pruebas.Unico()).ok(t, http.StatusOK)
	msj := r["mensaje"].(map[string]any)
	adj := msj["adjuntos"].([]any)
	if msj["autor_tipo"] != "cliente" || len(adj) != 1 {
		t.Fatalf("mensaje: %v", msj)
	}
	a := adj[0].(map[string]any)
	if a["mime"] != "image/png" || strings.ContainsAny(a["nombre"].(string), "/\\\"") {
		t.Fatalf("adjunto: %v", a)
	}
	c := &clienteHTTP{t: t, base: p.base, http: http.DefaultClient}
	baja := c.pedir(http.MethodGet, rutaPortal(clientePortal, "/tickets/"+strconv.FormatInt(id, 10)+"/adjuntos/"+a["id"].(string)), nil,
		"Authorization", "Bearer "+p.portal, "X-Iris-Empresa", "1", "X-Argos-Portal-Sesion", "documento:sesion-abc-123", "X-Iris-Contacto", "titular")
	if baja.status != http.StatusOK || baja.h.Get("X-Content-Type-Options") != "nosniff" || baja.h.Get("Content-Security-Policy") != "sandbox" ||
		!strings.HasPrefix(baja.h.Get("Content-Disposition"), "attachment; filename*=UTF-8''") || bytes.Contains(baja.crudo, []byte("secreto")) {
		t.Fatalf("descarga: %d %v %q", baja.status, baja.h, baja.crudo[:min(40, len(baja.crudo))])
	}

	// Un agente lo resuelve; el cliente confirma (§11.5) y responder a un cerrado es 422.
	if _, err := bd.Exec(context.Background(), `UPDATE soporte.tickets SET estado = 'resuelto', resuelto_en = now(), version = version + 1 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	v := p.pedir(http.MethodGet, clientePortal, "/tickets/"+strconv.FormatInt(id, 10), nil).ok(t, http.StatusOK)
	if v["puede_confirmar"] != true {
		t.Fatalf("resuelto: %v", v)
	}
	p.pedir(http.MethodPost, clientePortal, "/tickets/"+strconv.FormatInt(id, 10)+"/confirmar-solucion", map[string]any{"version": 1}).
		problema(t, http.StatusConflict, "conflicto_version")
	cerrado := p.pedir(http.MethodPost, clientePortal, "/tickets/"+strconv.FormatInt(id, 10)+"/confirmar-solucion", map[string]any{"version": v["version"]}).ok(t, http.StatusOK)
	if cerrado["estado"] != "cerrado" {
		t.Fatalf("confirmar: %v", cerrado)
	}
	p.pedir(http.MethodPost, clientePortal, "/tickets/"+strconv.FormatInt(id, 10)+"/responder", map[string]any{"cuerpo": "hola"}).
		problema(t, http.StatusUnprocessableEntity, "transicion_invalida")
	var eventos int
	if err := bd.QueryRow(context.Background(), `SELECT count(*) FROM integracion.outbox WHERE evento = 'ticket.estado_cambiado'
		AND payload->>'ticket_id' = $1 AND payload->>'origen' = 'portal_argos' AND payload->>'cliente_id' = '8850'`, strconv.FormatInt(id, 10)).Scan(&eventos); err != nil {
		t.Fatal(err)
	}
	if eventos < 1 {
		t.Fatal("sin ticket.estado_cambiado con el cliente")
	}

	// Conector apagado: 409 argos_apagado.
	p.e.apagar()
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil).problema(t, http.StatusConflict, "argos_apagado")
	p.e.encender()

	// Cliente que Iris no conoce y Argos no responde: el ticket se abre (queda por conciliar) y lo demás es 503.
	p.e.falso.fijar(func(a *argosFalso) { a.lecturaCaida = true })
	nuevo := p.pedir(http.MethodPost, 8860, "/tickets", map[string]any{"asunto": "Sin señal", "descripcion": "nada"}).ok(t, http.StatusCreated)
	var revision *string
	if err := bd.QueryRow(context.Background(), `SELECT cliente_revision FROM soporte.tickets WHERE id = $1`, int64(nuevo["id"].(float64))).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision == nil || *revision != "pendiente" {
		t.Fatalf("revisión: %v", revision)
	}
	p.pedir(http.MethodGet, 8860, "/tickets", nil).problema(t, http.StatusServiceUnavailable, "argos_no_disponible")
	p.e.falso.fijar(func(a *argosFalso) { a.lecturaCaida = false })

	// El token_portal vive con el iris_pat_ del canje: revocado el padre, el portal deja de entrar.
	if _, err := bd.Exec(context.Background(), `UPDATE nucleo.tokens_api SET revocado_en = now()
		WHERE id = (SELECT token_entregado_id FROM integracion.conectores WHERE id = $1)`, p.e.conID); err != nil {
		t.Fatal(err)
	}
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil).problema(t, http.StatusUnauthorized, "no_autenticado")
}

func TestPortalArgosContactos(t *testing.T) {
	p := nuevoPortalArgos(t)
	bd := pruebas.Pool(t)
	// Argos informa 0.12.1: X-Iris-Contacto es obligatoria.
	if _, err := bd.Exec(context.Background(), `UPDATE integracion.conectores SET contrato_remoto = '0.12.1' WHERE id = $1`, p.e.conID); err != nil {
		t.Fatal(err)
	}
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil, "X-Iris-Contacto", "").problema(t, http.StatusBadRequest, "cabecera_iris_requerida")
	p.pedir(http.MethodGet, clientePortal, "/categorias", nil, "X-Iris-Contacto", "77", "X-Iris-Contacto-Version", "x").
		problema(t, http.StatusUnprocessableEntity, "validacion")

	// El titular abre uno en 4410.
	propio := p.pedir(http.MethodPost, clientePortal, "/tickets", map[string]any{"asunto": "Del titular", "descripcion": "x", "servicio_id": 4410}).ok(t, http.StatusCreated)

	p.e.falso.fijar(func(a *argosFalso) {
		a.contactosDe["8850"] = `{"contacto_id":77,"cliente_id":8850,"empresa_id":1,"nombre":"José Pérez","servicios":[4410],
			"permisos":{"consultar":true,"reportar":true,"pagar":false,"avisos":false},"version":3}`
	})
	jose := []string{"X-Iris-Contacto", "77", "X-Iris-Contacto-Version", "3"}
	// Sin servicio: 422; un servicio que no es suyo: 403.
	p.pedir(http.MethodPost, clientePortal, "/tickets", map[string]any{"asunto": "Del contacto", "descripcion": "x"}, jose...).
		problema(t, http.StatusUnprocessableEntity, "validacion")
	p.pedir(http.MethodPost, clientePortal, "/tickets", map[string]any{"asunto": "Del contacto", "descripcion": "x", "servicio_id": 4411}, jose...).
		problema(t, http.StatusForbidden, "servicio_fuera_del_contacto")
	suyo := p.pedir(http.MethodPost, clientePortal, "/tickets", map[string]any{"asunto": "Del contacto", "descripcion": "x", "servicio_id": 4410}, jose...).
		ok(t, http.StatusCreated)
	// La lista del contacto: completo el suyo, reducido el del titular.
	lista := p.pedir(http.MethodGet, clientePortal, "/tickets?servicio_id=4410", nil, jose...).ok(t, http.StatusOK)
	vistas := map[float64]string{}
	for _, x := range lista["data"].([]any) {
		m := x.(map[string]any)
		vistas[m["id"].(float64)] = m["vista"].(string)
	}
	if vistas[suyo["id"].(float64)] != "completa" || vistas[propio["id"].(float64)] != "reducida" {
		t.Fatalf("vistas del contacto: %v", vistas)
	}
	p.pedir(http.MethodGet, clientePortal, "/tickets", nil, jose...).problema(t, http.StatusUnprocessableEntity, "validacion")
	// Escribir en el del titular: 403; las solicitudes: solo el titular.
	p.pedir(http.MethodPost, clientePortal, "/tickets/"+strconv.FormatInt(int64(propio["id"].(float64)), 10)+"/responder", map[string]any{"cuerpo": "x"}, jose...).
		problema(t, http.StatusForbidden, "permiso_de_contacto")
	// Revisión M1: la foto no se decodifica antes del permiso. Este JPEG pasa el encabezado pero no se
	// puede recodificar (422 mime_no_permitido); como el contacto no puede escribir ahí, responde 403.
	jpegSinDatos := []byte{0xff, 0xd8, 0xff, 0xc2, 0, 17, 8, 0x0b, 0xb8, 0x0f, 0xa0, 3, 1, 0x22, 0, 2, 0x11, 1, 3, 0x11, 1,
		0xff, 0xda, 0, 12, 3, 1, 0, 2, 0x11, 3, 0x11, 0, 0x3f, 0}
	ruta := "/tickets/" + strconv.FormatInt(int64(propio["id"].(float64)), 10) + "/responder"
	p.multipart(clientePortal, ruta, map[string]any{"cuerpo": "x"}, map[string][]byte{"f.jpg": jpegSinDatos}, jose...).
		problema(t, http.StatusForbidden, "permiso_de_contacto")
	p.multipart(clientePortal, ruta, map[string]any{"cuerpo": "x"}, map[string][]byte{"f.jpg": jpegSinDatos}).
		problema(t, http.StatusUnprocessableEntity, "mime_no_permitido")
	p.pedir(http.MethodGet, clientePortal, "/solicitudes", nil, jose...).problema(t, http.StatusForbidden, "solo_titular")
	// El titular lo ve abierto por su contacto.
	v := p.pedir(http.MethodGet, clientePortal, "/tickets/"+strconv.FormatInt(int64(suyo["id"].(float64)), 10), nil).ok(t, http.StatusOK)
	if v["abierto_por_contacto"] != "José Pérez" {
		t.Fatalf("titular: %v", v)
	}
	// La caché sirve mientras la versión no suba; una versión mayor relee Argos.
	antes := 0
	p.e.falso.fijar(func(a *argosFalso) { antes = a.lecturasContactos })
	p.pedir(http.MethodGet, clientePortal, "/tickets?servicio_id=4410", nil, jose...).ok(t, http.StatusOK)
	p.e.falso.fijar(func(a *argosFalso) {
		if a.lecturasContactos != antes {
			t.Errorf("releyó con la misma versión")
		}
		a.contactosDe["8850"] = ""
	})
	// Versión 4 y Argos ya no lo trae (quitado): 403.
	p.pedir(http.MethodGet, clientePortal, "/tickets?servicio_id=4410", nil, "X-Iris-Contacto", "77", "X-Iris-Contacto-Version", "4").
		problema(t, http.StatusForbidden, "servicio_fuera_del_contacto")
}

func TestPortalArgosSolicitudes(t *testing.T) {
	p := nuevoPortalArgos(t)
	bd := pruebas.Pool(t)
	p.pedir(http.MethodPost, clientePortal, "/solicitudes", map[string]any{"tipo": "traslado", "direccion": "Zona 1"}).
		problema(t, http.StatusUnprocessableEntity, "servicio_requerido")
	p.pedir(http.MethodPost, clientePortal, "/solicitudes", map[string]any{"tipo": "cambio_plan", "servicio_id": 4412}).
		problema(t, http.StatusUnprocessableEntity, "validacion")
	p.pedir(http.MethodPost, clientePortal, "/solicitudes", map[string]any{"tipo": "alta", "direccion": "Zona 1", "plan_interes_id": 999}).
		problema(t, http.StatusUnprocessableEntity, "validacion")
	s := p.pedir(http.MethodPost, clientePortal, "/solicitudes", map[string]any{"tipo": "traslado", "servicio_id": 4410,
		"direccion": "5a avenida 10-20, zona 1, Mixco", "coordenadas": "14.6349,-90.5069", "plan_interes_id": 300,
		"comentario": "Me mudo el 1 de diciembre"}).ok(t, http.StatusCreated)
	id := int64(s["id"].(float64))
	if s["estado_cliente"] != "recibida" || s["tipo"] != "traslado" || !strings.HasPrefix(s["numero"].(string), "PR-") {
		t.Fatalf("solicitud: %v", s)
	}
	var origen string
	var cliente *int64
	var correo *string
	if err := bd.QueryRow(context.Background(), `SELECT origen, cliente_existente_id, correo FROM comercial.prospectos WHERE id = $1`, id).
		Scan(&origen, &cliente, &correo); err != nil {
		t.Fatal(err)
	}
	if origen != "portal_argos" || cliente == nil || *cliente != clientePortal || correo == nil || *correo != "maria@correo.test" {
		t.Fatalf("prospecto: %s %v %v", origen, cliente, correo)
	}
	// Otra del mismo tipo y servicio: 409 con su id.
	r := p.pedir(http.MethodPost, clientePortal, "/solicitudes", map[string]any{"tipo": "traslado", "servicio_id": 4410, "direccion": "Otra"})
	r.problema(t, http.StatusConflict, "solicitud_abierta")
	if r.cuerpo["solicitud_id"] != float64(id) {
		t.Fatalf("solicitud_id: %v", r.cuerpo)
	}
	// Lista y detalle; de otro cliente, 404.
	l := p.pedir(http.MethodGet, clientePortal, "/solicitudes?estado=recibida", nil).ok(t, http.StatusOK)
	if len(l["data"].([]any)) != 1 {
		t.Fatalf("lista: %v", l)
	}
	p.pedir(http.MethodGet, otroCliente, "/solicitudes/"+strconv.FormatInt(id, 10), nil).problema(t, http.StatusNotFound, "no_encontrado")
	// Un asesor la contacta: en_revision, con el evento que va a Argos (sin datos personales).
	ver, err := p.e.com.VerProspecto.Ejecutar(sistema(t), comercial.EntradaVerProspecto{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.e.com.Contactar.Ejecutar(sistema(t), comercial.EntradaTransicion{ID: id, Version: ver.Version}); err != nil {
		t.Fatal(err)
	}
	if v := p.pedir(http.MethodGet, clientePortal, "/solicitudes/"+strconv.FormatInt(id, 10), nil).ok(t, http.StatusOK); v["estado_cliente"] != "en_revision" {
		t.Fatalf("detalle: %v", v)
	}
	var payload string
	if err := bd.QueryRow(context.Background(), `SELECT payload::text FROM integracion.outbox WHERE evento = 'solicitud_servicio.estado_cambiado'
		AND payload->>'solicitud_id' = $1`, strconv.FormatInt(id, 10)).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload, `"a": "en_revision"`) || !strings.Contains(payload, `"de": "recibida"`) || strings.Contains(payload, "Mixco") {
		t.Fatalf("evento: %s", payload)
	}
	// La regla de estado_cliente.
	casos := []struct {
		estado, tipo string
		orden        *int64
		completada   bool
		quiere       string
	}{
		{"nuevo", "alta", nil, false, "recibida"}, {"factibilidad", "alta", nil, false, "en_revision"}, {"cotizado", "alta", nil, false, "cotizada"},
		{"contrato", "alta", nil, false, "en_proceso"}, {"ganado", "alta", nil, false, "en_proceso"}, {"ganado", "traslado", nil, false, "completada"},
		{"ganado", "alta", new(int64), true, "completada"}, {"perdido", "otro", nil, false, "no_procede"},
	}
	for _, c := range casos {
		if got := comercial.EstadoClienteDe(c.estado, c.tipo, c.orden, c.completada); got != c.quiere {
			t.Errorf("%s/%s: %s, quería %s", c.estado, c.tipo, got, c.quiere)
		}
	}
}
