package voz_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/voz"
	"github.com/HCDestroyer/iris/apps/api/internal/voz/vozprueba"
)

const versionIris = "0.28.0"

func codigoDe(err error) string {
	var e *voz.ErrorPuerta
	if errors.As(err, &e) {
		return strconv.Itoa(e.Estado) + " " + e.Codigo
	}
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
}

func esperar(t *testing.T, quiero string, err error) {
	t.Helper()
	if got := codigoDe(err); got != quiero {
		t.Fatalf("quería %q, salió %q", quiero, got)
	}
}

// chatAdaptador es la forma exacta de lo que manda el adaptador compatible con OpenAI de Iris.
func chatAdaptador(modelo string, conHerramientas bool) []byte {
	cuerpo := map[string]any{
		"model": modelo, "max_tokens": 600, "stream": false,
		"messages": []map[string]any{
			{"role": "system", "content": "Eres el asistente."},
			{"role": "user", "content": "Hola"},
		},
	}
	if conHerramientas {
		cuerpo["messages"] = append(cuerpo["messages"].([]map[string]any),
			map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{
				{"id": "l1", "type": "function", "function": map[string]any{"name": "crear_ticket", "arguments": `{"asunto":"x"}`}}}},
			map[string]any{"role": "tool", "tool_call_id": "l1", "content": `{"ok":true}`})
		cuerpo["tools"] = []map[string]any{{"type": "function", "function": map[string]any{
			"name": "crear_ticket", "description": "Crea un ticket", "strict": true,
			"parameters": map[string]any{"type": "object", "properties": map[string]any{"asunto": map[string]any{"type": "string"}},
				"required": []string{"asunto"}, "additionalProperties": false}}}}
		cuerpo["tool_choice"] = "auto"
	}
	b, _ := json.Marshal(cuerpo)
	return b
}

func TestEmparejamiento(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	ctx := t.Context()

	// Sin ventana abierta y sin certificado fijado, la conexión se corta en el saludo TLS.
	codigo, _ := voz.NuevoCodigo()
	if _, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: b.URL, Codigo: codigo, Cert: cert}); err == nil || errors.Is(err, voz.ErrEnlace) {
		t.Fatalf("sin ventana debía cortarse en el saludo: %v", err)
	}

	// Con la ventana abierta, un código equivocado no enlaza y NO cierra la ventana.
	bueno := b.AbrirVentana()
	if _, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: b.URL, Codigo: codigo, Cert: cert}); !errors.Is(err, voz.ErrEnlace) {
		t.Fatalf("código equivocado: %v", err)
	}
	if _, abierta := b.Almacen.VentanaAbierta(b.Ahora()); !abierta {
		t.Fatal("un intento fallido cerró la ventana")
	}
	// Con la ventana abierta solo existe /enlazar: lo demás sigue pidiendo el certificado fijado.
	intruso := b.Cliente(cert, make([]byte, voz.LargoToken), versionIris, b.Huella)
	_, err := intruso.Salud(ctx)
	esperar(t, "401 no_autorizado", err)

	b.Adelantar(3 * time.Second)
	r, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: b.URL, Codigo: bueno, Cert: cert})
	if err != nil {
		t.Fatalf("enlazar: %v", err)
	}
	if r.HuellaServidor != b.Huella {
		t.Fatalf("huella vista %s, la del instalador %s", r.HuellaServidor, b.Huella)
	}
	e, _ := b.Almacen.Leer()
	if !bytes.Equal(e.Token, r.Token) || len(r.Token) != voz.LargoToken {
		t.Fatal("el token derivado no coincide en los dos lados")
	}
	if e.IPObservada != "127.0.0.1" || !e.Enlazado() {
		t.Fatalf("estado tras enlazar: %+v", e.IPObservada)
	}
	if _, abierta := b.Almacen.VentanaAbierta(b.Ahora()); abierta {
		t.Fatal("la ventana debía cerrarse con el primer enlace correcto")
	}
	if r.Manifiesto.Version != "0.1.0" {
		t.Fatalf("manifiesto: %+v", r.Manifiesto)
	}

	// El código es de un solo uso: repetirlo ya no pasa del saludo con otro certificado.
	otro := vozprueba.Certificado(t)
	if _, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: b.URL, Codigo: bueno, Cert: otro}); err == nil || errors.Is(err, voz.ErrEnlace) {
		t.Fatalf("código reutilizado con otro certificado: %v", err)
	}
	// Y con el certificado fijado, /enlazar ya no existe.
	b.Adelantar(3 * time.Second)
	if _, err := voz.Enlazar(ctx, voz.OpcionesEnlace{URL: b.URL, Codigo: bueno, Cert: cert}); !errors.Is(err, voz.ErrEnlace) {
		t.Fatalf("código reutilizado: %v", err)
	}

	c := b.Cliente(cert, r.Token, versionIris, r.HuellaServidor)
	s, err := c.Salud(ctx)
	if err != nil {
		t.Fatalf("salud: %v", err)
	}
	if s.Estado != voz.EstadoListo || s.STT.Estado != voz.EstadoOmitido || s.TTS.Estado != voz.EstadoOmitido ||
		s.UDP.Estado != voz.EstadoOmitido || s.LLM.Estado != voz.EstadoListo || len(s.LLM.Modelos) != 1 || s.IPObservada != "127.0.0.1" {
		t.Fatalf("salud: %+v", s)
	}
}

func TestEnlaceLimiteDeFrecuencia(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	bueno := b.AbrirVentana()
	malo, _ := voz.NuevoCodigo()
	// Una ráfaga de fallos desde una IP agota su cubo: hasta el código bueno recibe 429 desde ahí.
	for range 6 {
		if _, err := voz.Enlazar(t.Context(), voz.OpcionesEnlace{URL: b.URL, Codigo: malo, Cert: cert}); !errors.Is(err, voz.ErrEnlace) {
			t.Fatalf("intento fallido: %v", err)
		}
	}
	if _, err := voz.Enlazar(t.Context(), voz.OpcionesEnlace{URL: b.URL, Codigo: bueno, Cert: cert}); !errors.Is(err, voz.ErrEnlace) {
		t.Fatalf("con el cubo de la IP agotado: %v", err)
	}
	// Los fallos no cierran la ventana ni bloquean el enlace: pasado un momento, el código bueno entra.
	if _, abierta := b.Almacen.VentanaAbierta(b.Ahora()); !abierta {
		t.Fatal("los fallos cerraron la ventana")
	}
	b.Adelantar(5 * time.Second)
	if _, err := voz.Enlazar(t.Context(), voz.OpcionesEnlace{URL: b.URL, Codigo: bueno, Cert: cert}); err != nil {
		t.Fatalf("el enlace legítimo quedó bloqueado por los fallos: %v", err)
	}
	b.AbrirVentana()
	// La ventana vence sola a los 15 minutos.
	b.Adelantar(voz.VidaCodigo)
	if _, abierta := b.Almacen.VentanaAbierta(b.Ahora()); abierta {
		t.Fatal("la ventana no venció")
	}
}

func TestMTLSFijado(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	c, r := b.Enlazar(cert, versionIris)
	if _, err := c.Salud(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Otro certificado, aun con el token correcto: se corta en el saludo.
	otro := b.Cliente(vozprueba.Certificado(t), r.Token, versionIris, r.HuellaServidor)
	if _, err := otro.Salud(t.Context()); err == nil || codigoDe(err) != "error: "+err.Error() {
		t.Fatalf("otro certificado debía cortarse en el saludo: %v", codigoDe(err))
	}
	// Sin certificado de cliente.
	sin := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if resp, err := sin.Get(b.URL + voz.RutaSalud); err == nil {
		_ = resp.Body.Close()
		t.Fatal("sin certificado debía cortarse en el saludo")
	}
	// Iris fija la huella del servidor: con otra huella no habla.
	falsa := b.Cliente(cert, r.Token, versionIris, "sha256:"+strings.Repeat("0", 64))
	if _, err := falsa.Salud(t.Context()); !errors.Is(err, voz.ErrHuella) {
		t.Fatalf("huella del servidor distinta: %v", err)
	}
}

// crudo manda una petición con las cabeceras que se le den, por una conexión con el certificado fijado.
func crudo(t *testing.T, b *vozprueba.Banco, cert tls.Certificate, metodo, ruta string, cuerpo []byte, cab map[string]string) (int, []byte, http.Header) {
	t.Helper()
	cli := &http.Client{Transport: &http.Transport{DisableKeepAlives: true,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, Certificates: []tls.Certificate{cert}}}}
	req, err := http.NewRequestWithContext(t.Context(), metodo, b.URL+ruta, bytes.NewReader(cuerpo))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range cab {
		req.Header.Set(k, v)
	}
	resp, err := cli.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, resp.Header
}

func firmadas(token []byte, metodo, ruta string, cuerpo []byte, marca time.Time, nonce string) map[string]string {
	m := strconv.FormatInt(marca.Unix(), 10)
	return map[string]string{voz.CabMarca: m, voz.CabNonce: nonce, voz.CabVersion: versionIris,
		voz.CabFirma: voz.FirmaPeticion(token, metodo, ruta, m, nonce, versionIris, cuerpo)}
}

func TestFirmaYRepeticion(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	_, r := b.Enlazar(cert, versionIris)
	ahora := time.Now()
	nonce := func() string { n, _ := voz.NuevoNonce(); return n }

	// Bien firmada pasa, y la respuesta viene firmada con el nonce de la petición.
	n1 := nonce()
	cab := firmadas(r.Token, "GET", voz.RutaSalud, nil, ahora, n1)
	est, cuerpo, h := crudo(t, b, cert, "GET", voz.RutaSalud, nil, cab)
	if est != 200 || !voz.Iguales(h.Get(voz.CabFirmaRespuesta), voz.FirmaRespuesta(r.Token, n1, 200, cuerpo)) {
		t.Fatalf("petición firmada: %d %s", est, cuerpo)
	}
	// Repetida (mismo nonce): 401.
	if est, _, _ := crudo(t, b, cert, "GET", voz.RutaSalud, nil, cab); est != 401 {
		t.Fatalf("repetida: %d", est)
	}
	casos := map[string]map[string]string{
		"sin firma":            {voz.CabMarca: strconv.FormatInt(ahora.Unix(), 10), voz.CabNonce: nonce(), voz.CabVersion: versionIris},
		"firma de otra ruta":   firmadas(r.Token, "GET", voz.RutaVersion, nil, ahora, nonce()),
		"firma de otro método": firmadas(r.Token, "POST", voz.RutaSalud, nil, ahora, nonce()),
		"otro token":           firmadas(make([]byte, voz.LargoToken), "GET", voz.RutaSalud, nil, ahora, nonce()),
		"firma de otro cuerpo": firmadas(r.Token, "GET", voz.RutaSalud, []byte("x"), ahora, nonce()),
	}
	for nombre, cab := range casos {
		if est, out, h := crudo(t, b, cert, "GET", voz.RutaSalud, nil, cab); est != 401 || h.Get(voz.CabFirmaRespuesta) != "" {
			t.Fatalf("%s: %d %s", nombre, est, out)
		}
	}
	// La versión de Iris va dentro de la cadena firmada: cambiarla invalida la firma.
	cab = firmadas(r.Token, "GET", voz.RutaSalud, nil, ahora, nonce())
	cab[voz.CabVersion] = "9.9.9"
	if est, _, _ := crudo(t, b, cert, "GET", voz.RutaSalud, nil, cab); est != 401 {
		t.Fatalf("versión cambiada: %d", est)
	}
	// Marca fuera de la ventana: reloj_desfasado con la hora del servidor.
	est, out, _ := crudo(t, b, cert, "GET", voz.RutaSalud, nil, firmadas(r.Token, "GET", voz.RutaSalud, nil, ahora.Add(-2*time.Minute), nonce()))
	var p struct {
		Codigo       string `json:"codigo"`
		HoraServidor int64  `json:"horaServidor"`
	}
	_ = json.Unmarshal(out, &p)
	if est != 401 || p.Codigo != "reloj_desfasado" || p.HoraServidor == 0 {
		t.Fatalf("reloj: %d %s", est, out)
	}
	// Una marca anterior al arranque del proceso no vale (los nonces vistos no sobreviven a un
	// reinicio): aunque esté dentro de la ventana y bien firmada.
	b2 := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	_, r2 := b2.Enlazar(cert, versionIris)
	antes := b2.Inicio().Add(-5 * time.Second)
	if est, _, _ := crudo(t, b2, cert, "GET", voz.RutaSalud, nil, firmadas(r2.Token, "GET", voz.RutaSalud, nil, antes, nonce())); est != 401 {
		t.Fatalf("marca anterior al arranque: %d", est)
	}

	// Ruta literal exacta: con query, con «%» o sin estar en la lista, 404 (aunque esté bien firmada).
	for _, ruta := range []string{voz.RutaSalud + "?a=1", "/voz/v1/%73alud", voz.Base + "/llm/api/pull", voz.Base + "/llm/api/tags",
		voz.Base + "/llm/v1/chat/completions/", voz.Base + "/llm/../salud", "/api/pull"} {
		metodo := "POST"
		if est, _, _ := crudo(t, b, cert, metodo, ruta, nil, firmadas(r.Token, metodo, ruta, nil, ahora, nonce())); est != 404 {
			t.Fatalf("%s: %d", ruta, est)
		}
	}
	for _, p := range b.Ollama.Peticiones() {
		if p.Ruta != "/api/tags" && p.Ruta != "/api/ps" {
			t.Fatalf("llegó a Ollama una ruta que no debía: %s %s", p.Metodo, p.Ruta)
		}
	}
}

func TestRotacion(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	viejo, r := b.Enlazar(cert, versionIris)
	ctx := t.Context()

	// Al activar: token aleatorio sin convivencia. El derivado del código deja de valer en el acto.
	t1, _ := voz.NuevoToken()
	if err := viejo.Credencial(ctx, voz.PeticionCredencial{Token: t1}); err != nil {
		t.Fatalf("rotar: %v", err)
	}
	_, err := viejo.Salud(ctx)
	esperar(t, "401 no_autorizado", err)
	c1 := b.Cliente(cert, t1, versionIris, r.HuellaServidor)
	if _, err := c1.Salud(ctx); err != nil {
		t.Fatalf("token nuevo: %v", err)
	}

	// Rotación con convivencia: valen los dos, token y certificado de cliente, hasta que vence.
	t2, _ := voz.NuevoToken()
	cert2 := vozprueba.Certificado(t)
	h2 := voz.Huella(cert2.Certificate[0])
	if err := c1.Credencial(ctx, voz.PeticionCredencial{Token: t2, HuellaCliente: h2, ConvivenciaSeg: 3600}); err != nil {
		t.Fatalf("rotar con convivencia: %v", err)
	}
	c2 := b.Cliente(cert2, t2, versionIris, r.HuellaServidor)
	for nombre, c := range map[string]*voz.Cliente{"anterior": c1, "nuevo": c2} {
		if _, err := c.Salud(ctx); err != nil {
			t.Fatalf("%s durante la convivencia: %v", nombre, err)
		}
	}
	// El certificado nuevo con el token viejo también vale mientras dura; pasada la ventana, no.
	b.Adelantar(20 * time.Second)
	c1.Cerrar()
	// (la firma tolera ±30 s; el reloj de la puerta va 20 s adelantado respecto al del cliente)
	e, _ := b.Almacen.Leer()
	if e.TokenAnteriorVence.IsZero() || e.HuellaAnteriorVence.IsZero() || e.HuellaClienteAnterior == "" || len(e.TokenAnterior) != voz.LargoToken {
		t.Fatalf("convivencia no guardada")
	}
	vencer := func() {
		t.Helper()
		if err := b.Almacen.Cambiar(func(e *voz.Estado) error {
			e.TokenAnteriorVence, e.HuellaAnteriorVence = b.Ahora().Add(-time.Second), b.Ahora().Add(-time.Second)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	vencer()
	if _, err := c1.Salud(ctx); err == nil {
		t.Fatal("el certificado anterior debía dejar de valer al vencer la convivencia")
	}
	if _, err := c2.Salud(ctx); err != nil {
		t.Fatalf("nuevo tras la convivencia: %v", err)
	}

	// Una credencial anterior ya vencida no revive al rotar la otra con convivencia: se rota solo el
	// certificado de cliente (el token anterior, t1, venció arriba y sigue en el archivo) y t1 no
	// vuelve a valer; y al revés, se rota solo el token y la huella anterior vencida tampoco revive.
	cert3 := vozprueba.Certificado(t)
	if err := c2.Credencial(ctx, voz.PeticionCredencial{HuellaCliente: voz.Huella(cert3.Certificate[0]), ConvivenciaSeg: 3600}); err != nil {
		t.Fatal(err)
	}
	if e, _ := b.Almacen.Leer(); e.TokenAnterior != nil || !e.TokenAnteriorVence.IsZero() {
		t.Fatal("el token anterior vencido debía limpiarse al rotar la huella")
	}
	_, err = b.Cliente(cert3, t1, versionIris, r.HuellaServidor).Salud(ctx)
	esperar(t, "401 no_autorizado", err)
	vencer() // vence la huella anterior (cert2)
	t2b, _ := voz.NuevoToken()
	c2n := b.Cliente(cert3, t2, versionIris, r.HuellaServidor)
	if err := c2n.Credencial(ctx, voz.PeticionCredencial{Token: t2b, ConvivenciaSeg: 3600}); err != nil {
		t.Fatal(err)
	}
	if e, _ := b.Almacen.Leer(); e.HuellaClienteAnterior != "" {
		t.Fatal("la huella anterior vencida debía limpiarse al rotar el token")
	}
	c2.Cerrar()
	if _, err := c2.Salud(ctx); err == nil {
		t.Fatal("el certificado anterior vencido revivió al rotar el token")
	}
	// Convivencia 0 sin token ni huella nuevos revoca de verdad lo anterior (aquí, t2).
	if _, err := c2n.Salud(ctx); err != nil {
		t.Fatalf("t2 debía valer durante la convivencia: %v", err)
	}
	c3n := b.Cliente(cert3, t2b, versionIris, r.HuellaServidor)
	if err := c3n.Credencial(ctx, voz.PeticionCredencial{ConvivenciaSeg: 0}); err != nil {
		t.Fatal(err)
	}
	_, err = c2n.Salud(ctx)
	esperar(t, "401 no_autorizado", err)
	c2, cert2 = c3n, cert3

	// «Revocar ahora»: convivencia 0.
	t3, _ := voz.NuevoToken()
	if err := c2.Credencial(ctx, voz.PeticionCredencial{Token: t3, ConvivenciaSeg: 0}); err != nil {
		t.Fatal(err)
	}
	_, err = c2.Salud(ctx)
	esperar(t, "401 no_autorizado", err)

	// Credencial mal formada.
	c3 := b.Cliente(cert2, t3, versionIris, r.HuellaServidor)
	esperar(t, "422 credencial_invalida", c3.Credencial(ctx, voz.PeticionCredencial{Token: []byte("corto")}))
	esperar(t, "422 credencial_invalida", c3.Credencial(ctx, voz.PeticionCredencial{Token: t3, ConvivenciaSeg: 90000}))

	// `iris-voz unpair`: nada vale hasta un enlace nuevo.
	if err := b.Almacen.Desenlazar(); err != nil {
		t.Fatal(err)
	}
	c3.Cerrar()
	if _, err := c3.Salud(ctx); err == nil {
		t.Fatal("tras desenlazar no debía aceptar nada")
	}
}

func TestRenovarCertificadoPorElCanal(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	c, r := b.Enlazar(cert, versionIris)
	ctx := t.Context()
	siguiente, err := b.Almacen.PrepararSiguiente("iris-voz-prueba")
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Salud(ctx)
	if err != nil || s.HuellaSiguiente != siguiente {
		t.Fatalf("la huella siguiente debía anunciarse por el canal vigente: %v %q", err, s.HuellaSiguiente)
	}
	// Confirmar una huella que no es la anunciada no cambia nada.
	esperar(t, "409 sin_huella_siguiente", c.Credencial(ctx, voz.PeticionCredencial{ConfirmaHuellaServidor: "sha256:" + strings.Repeat("a", 64)}))
	// Hay una rotación de token en convivencia: confirmar la huella del servidor (una petición que
	// no trae convivenciaSeg) no debe revocar el token anterior ni cortar esa convivencia.
	nuevo, _ := voz.NuevoToken()
	if err := c.Credencial(ctx, voz.PeticionCredencial{Token: nuevo, ConvivenciaSeg: 3600}); err != nil {
		t.Fatal(err)
	}
	// Iris fija las dos y confirma (firmando con el token anterior); la puerta empieza a usar la nueva.
	ambas := b.Cliente(cert, r.Token, versionIris, r.HuellaServidor, siguiente)
	if err := ambas.Credencial(ctx, voz.PeticionCredencial{ConfirmaHuellaServidor: siguiente}); err != nil {
		t.Fatal(err)
	}
	ambas.Cerrar()
	if e, _ := b.Almacen.Leer(); len(e.TokenAnterior) != voz.LargoToken || e.TokenAnteriorVence.IsZero() {
		t.Fatal("confirmar la huella del servidor cortó la convivencia del token")
	}
	if _, err := ambas.Salud(ctx); err != nil || ambas.HuellaVista() != siguiente {
		t.Fatalf("tras confirmar debía verse la huella nueva: %v %s", err, ambas.HuellaVista())
	}
	// Quien solo tiene la anterior ya no habla (y no la acepta con un clic: hay que re-enlazar).
	c.Cerrar()
	if _, err := c.Salud(ctx); !errors.Is(err, voz.ErrHuella) {
		t.Fatalf("huella anterior: %v", err)
	}
	// Certificado perdido: regenerar desenlaza.
	nueva, err := b.Almacen.RegenerarCertificado("iris-voz-prueba")
	if err != nil || nueva == siguiente {
		t.Fatal("regenerar")
	}
	if e, _ := b.Almacen.Leer(); e.Enlazado() {
		t.Fatal("regenerar el certificado debía desenlazar")
	}
}

func TestReserializacion(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	cert := vozprueba.Certificado(t)
	c, r := b.Enlazar(cert, versionIris)
	ctx := t.Context()

	// La petición real del adaptador (con herramientas y tool_choice) pasa.
	out, err := c.Hacer(ctx, "POST", voz.RutaChat, chatAdaptador("modelo-a:latest", true), voz.TopeRespuestaLLM)
	if err != nil {
		t.Fatalf("chat del adaptador: %v", err)
	}
	if !bytes.Contains(out, []byte("prompt_tokens")) {
		t.Fatalf("respuesta: %s", out)
	}
	chats := b.Ollama.Chats()
	if len(chats) != 1 {
		t.Fatalf("chats en Ollama: %d", len(chats))
	}
	var llego map[string]json.RawMessage
	if err := json.Unmarshal(chats[0].Cuerpo, &llego); err != nil {
		t.Fatal(err)
	}
	for k := range llego {
		switch k {
		case "model", "messages", "stream", "max_tokens", "tools", "tool_choice":
		default:
			t.Fatalf("a Ollama llegó un campo fuera de la lista: %s", k)
		}
	}
	// Petición nueva: ninguna cabecera del cliente llega a Ollama, ni Transfer-Encoding.
	for _, k := range []string{voz.CabFirma, voz.CabNonce, voz.CabMarca, voz.CabVersion, "Authorization", "Transfer-Encoding", "X-Colado"} {
		if chats[0].Cabeceras.Get(k) != "" {
			t.Fatalf("a Ollama llegó la cabecera %s", k)
		}
	}

	rechazos := map[string]string{
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"keep_alive":"24h"}`:                                              "422 campo_no_permitido",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"options":{"num_ctx":999999}}`:                                    "422 campo_no_permitido",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x","images":["a"]}]}`:                                                  "422 campo_no_permitido",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"a","url":"x"}}]}`: "422 campo_no_permitido",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"stream":true}`:                                                   "422 flujo_no_disponible",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"tool_choice":"required"}`:                                        "422 peticion_invalida",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"max_tokens":999999}`:                                             "422 peticion_invalida",
		`{"model":"modelo-a:latest","messages":[{"role":"raiz","content":"x"}]}`:                                                                 "422 peticion_invalida",
		`{"model":"modelo-a:latest","messages":[]}`:                                                                                              "422 peticion_invalida",
		`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}]} {"otro":1}`:                                                      "422 peticion_invalida",
		`no es json`: "422 peticion_invalida",
		// Instalado pero no permitido, y uno que no existe: nunca se dispara una descarga.
		`{"model":"modelo-b:latest","messages":[{"role":"user","content":"x"}]}`: "422 modelo_no_instalado",
		`{"model":"otro:latest","messages":[{"role":"user","content":"x"}]}`:     "422 modelo_no_instalado",
		`{"model":"../../x","messages":[{"role":"user","content":"x"}]}`:         "422 modelo_no_instalado",
	}
	for cuerpo, quiero := range rechazos {
		_, err := c.Hacer(ctx, "POST", voz.RutaChat, []byte(cuerpo), voz.TopeRespuestaLLM)
		if got := codigoDe(err); got != quiero {
			t.Fatalf("%s: quería %q, salió %q", cuerpo, quiero, got)
		}
	}
	if n := len(b.Ollama.Chats()); n != 1 {
		t.Fatalf("un rechazo llegó a Ollama (%d chats)", n)
	}

	// parameters y json_schema se recodifican: clave repetida en cualquier nivel o demasiada
	// profundidad, 422; lo que llega a Ollama es la recodificación.
	conEsquema := func(esquema string) []byte {
		return []byte(`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"a","parameters":` + esquema + `}}]}`)
	}
	hondo := strings.Repeat(`{"a":`, 40) + `1` + strings.Repeat(`}`, 40)
	for nombre, cuerpo := range map[string][]byte{
		"clave repetida en parameters":   conEsquema(`{"type":"object","properties":{"a":{"type":"string","type":"number"}}}`),
		"clave repetida arriba":          conEsquema(`{"type":"object","type":"array"}`),
		"parameters demasiado profundo":  conEsquema(hondo),
		"parameters que no es un objeto": conEsquema(`[1]`),
		"json_schema con clave repetida": []byte(`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema","json_schema":{"a":{"b":1,"b":2}}}}`),
		"json_schema demasiado profundo": []byte(`{"model":"modelo-a:latest","messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema","json_schema":` + hondo + `}}`),
	} {
		_, err := c.Hacer(ctx, "POST", voz.RutaChat, cuerpo, voz.TopeRespuestaLLM)
		if got := codigoDe(err); got != "422 peticion_invalida" {
			t.Fatalf("%s: %s", nombre, got)
		}
	}
	antesN := len(b.Ollama.Chats())
	if _, err := c.Hacer(ctx, "POST", voz.RutaChat, conEsquema("{ \"type\" : \"object\",\n \"properties\": { \"a\": {\"type\":\"string\"} } }"), voz.TopeRespuestaLLM); err != nil {
		t.Fatalf("esquema válido: %v", err)
	}
	if got := b.Ollama.Chats()[antesN].Cuerpo; !bytes.Contains(got, []byte(`"parameters":{"type":"object","properties":{"a":{"type":"string"}}}`)) {
		t.Fatalf("parameters no se recodificó: %s", got)
	}
	base := len(b.Ollama.Chats())

	// Clave repetida: a Ollama llega una sola vez (no se reenvía lo que escribió el cliente).
	dup := `{"model":"modelo-b:latest","model":"modelo-a:latest","messages":[{"role":"user","content":"x"}]}`
	if _, err := c.Hacer(ctx, "POST", voz.RutaChat, []byte(dup), voz.TopeRespuestaLLM); err != nil {
		t.Fatalf("clave repetida: %v", err)
	}
	ultimo := b.Ollama.Chats()[base].Cuerpo
	if bytes.Count(ultimo, []byte(`"model"`)) != 1 || !bytes.Contains(ultimo, []byte(`"stream":false`)) || !bytes.Contains(ultimo, []byte(`"max_tokens"`)) {
		t.Fatalf("cuerpo reserializado: %s", ultimo)
	}

	// La lista de modelos es la de los permitidos e instalados.
	out, err = c.Hacer(ctx, "GET", voz.RutaModelos, nil, 64<<10)
	if err != nil || !bytes.Contains(out, []byte("modelo-a:latest")) || bytes.Contains(out, []byte("modelo-b")) {
		t.Fatalf("modelos: %v %s", err, out)
	}
	// Cabeceras de Ollama no vuelven.
	_, _, h := crudo(t, b, cert, "POST", voz.RutaChat, chatAdaptador("modelo-a:latest", false),
		firmadas(r.Token, "POST", voz.RutaChat, chatAdaptador("modelo-a:latest", false), time.Now(), strings.Repeat("ab", 16)))
	if h.Get("X-Ollama-Interno") != "" {
		t.Fatal("una cabecera de Ollama volvió al cliente")
	}
}

func TestTopes(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{MaxSimultaneas: 1, PlazoLLM: 400 * time.Millisecond})
	cert := vozprueba.Certificado(t)
	c, _ := b.Enlazar(cert, versionIris)
	ctx := t.Context()
	chat := chatAdaptador("modelo-a:latest", false)

	// Petición de más de 1 MB.
	grande := []byte(`{"model":"modelo-a:latest","messages":[{"role":"user","content":"` + strings.Repeat("a", voz.TopePeticionLLM) + `"}]}`)
	_, err := c.Hacer(ctx, "POST", voz.RutaChat, grande, voz.TopeRespuestaLLM)
	esperar(t, "413 cuerpo_demasiado_grande", err)

	// Respuesta de más de 4 MB.
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) {
		o.Respuesta = []byte(`{"x":"` + strings.Repeat("a", voz.TopeRespuestaLLM) + `"}`)
	})
	_, err = c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM)
	esperar(t, "502 respuesta_demasiado_grande", err)

	// Respuesta que no es JSON y error de Ollama: no se reenvía el cuerpo.
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Respuesta = []byte("<html>") })
	_, err = c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM)
	esperar(t, "502 respuesta_invalida", err)
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Respuesta, o.Estado = []byte(`{"error":"secreto del prompt"}`), 500 })
	_, err = c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM)
	esperar(t, "502 llm_error", err)
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Respuesta, o.Estado = nil, 0 })

	// Ollama redirige: no se sigue (ni a otro host ni a otra ruta) y es un error.
	destino := vozprueba.Nuevo("x")
	otro := httptest.NewServer(destino)
	defer otro.Close()
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Redirigir = otro.URL + "/v1/chat/completions" })
	_, err = c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM)
	esperar(t, "502 llm_error", err)
	if n := len(destino.Peticiones()); n != 0 {
		t.Fatalf("se siguió la redirección de Ollama (%d peticiones al destino)", n)
	}
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Redirigir = "" })

	// Duración máxima por petición.
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Demora = 2 * time.Second })
	_, err = c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM)
	esperar(t, "504 tiempo_agotado", err)

	// Simultáneas: con una plaza, la segunda a la vez recibe 503 al instante (no hay cola).
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Demora = 250 * time.Millisecond })
	var wg sync.WaitGroup
	res := make([]string, 2)
	for i := range res {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM)
			res[i] = codigoDe(err)
		}()
	}
	wg.Wait()
	slices.Sort(res)
	if res[0] != "503 sin_capacidad" || res[1] != "ok" {
		t.Fatalf("simultáneas: %v", res)
	}
	// La plaza se suelta: la siguiente pasa.
	if _, err := c.Hacer(ctx, "POST", voz.RutaChat, chat, voz.TopeRespuestaLLM); err != nil {
		t.Fatalf("tras soltar la plaza: %v", err)
	}

	// Límite de frecuencia de /metricas.
	var limitada bool
	for range 12 {
		if _, err := c.Metricas(ctx); err != nil {
			esperar(t, "429 demasiadas_peticiones", err)
			limitada = true
		}
	}
	if !limitada {
		t.Fatal("/metricas sin límite de frecuencia")
	}
}

func TestCaidaDeOllama(t *testing.T) {
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{})
	c, _ := b.Enlazar(vozprueba.Certificado(t), versionIris)
	ctx := t.Context()
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Caido = true })
	time.Sleep(2100 * time.Millisecond) // la lista de modelos se guarda 2 s
	s, err := c.Salud(ctx)
	if err != nil || s.Estado != voz.EstadoDegradado || s.LLM.Estado != voz.EstadoError {
		t.Fatalf("salud con Ollama caído: %v %+v", err, s)
	}
	_, err = c.Hacer(ctx, "POST", voz.RutaChat, chatAdaptador("modelo-a:latest", false), voz.TopeRespuestaLLM)
	esperar(t, "503 llm_no_disponible", err)
	// Vuelve solo.
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Caido = false })
	time.Sleep(2100 * time.Millisecond)
	if s, err := c.Salud(ctx); err != nil || s.Estado != voz.EstadoListo {
		t.Fatalf("salud al volver: %v %+v", err, s)
	}
}

func TestMetricasVersionYCompatibilidad(t *testing.T) {
	medidor := filepath.Join(t.TempDir(), "gpu.json")
	man := voz.Manifiesto{Version: "1.2.3", Protocolo: voz.Protocolo, IrisMin: "0.28.0", IrisMax: "0.99.0"}
	b := vozprueba.NuevoBanco(t, vozprueba.Opciones{Manifiesto: &man, MedidorRuta: medidor})
	cert := vozprueba.Certificado(t)
	c, r := b.Enlazar(cert, versionIris)
	ctx := t.Context()

	// Sin lectura del medidor: GPU omitida, no inventada.
	m, err := c.Metricas(ctx)
	if err != nil || m.GPUs.Estado != voz.EstadoOmitido || len(m.GPUs.Lista) != 0 {
		t.Fatalf("métricas sin GPU: %v %+v", err, m.GPUs)
	}
	if len(m.Modelos) != 2 || m.LLM.MaxSimultaneas != 4 || m.Version != "1.2.3" || m.Memoria.TotalMb == 0 || m.CPU.Nucleos == 0 || m.Disco.TotalMb == 0 {
		t.Fatalf("métricas: %+v", m)
	}
	// Lectura del medidor (la salida real de nvidia-smi, con un dato que la tarjeta no da).
	gpus := voz.InterpretarSMI([]byte("0, NVIDIA GeForce RTX 4090, 37, 8123, 24564, 61, 180.52\n1, Tesla T4, 0, 3, 15360, 40, [N/A]\nbasura\n2, <script>, 1, 1, 1, 1, 1\n"))
	if len(gpus) != 2 || gpus[1].PotenciaW != nil || *gpus[0].VRAMTotalMb != 24564 {
		t.Fatalf("nvidia-smi: %+v", gpus)
	}
	escribir := func(en time.Time) {
		j, _ := json.Marshal(voz.LecturaGPU{TomadoEn: en, GPUs: gpus})
		if err := os.WriteFile(medidor, j, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	escribir(time.Now())
	b.Ollama.Cambiar(func(o *vozprueba.Ollama) { o.Cargados["modelo-a:latest"] = 3 << 30 })
	time.Sleep(2100 * time.Millisecond)
	m, err = c.Metricas(ctx)
	if err != nil || m.GPUs.Estado != "" || len(m.GPUs.Lista) != 2 || m.GPUs.Lista[0].Nombre != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("métricas con GPU: %v %+v", err, m.GPUs)
	}
	var cargado bool
	for _, x := range m.Modelos {
		if x.ID == "modelo-a:latest" {
			cargado = x.Cargado && x.VRAMMb == 3<<10 && x.CargadoHasta != nil && x.Permitido
		}
	}
	if !cargado {
		t.Fatalf("modelo cargado: %+v", m.Modelos)
	}
	v, err := c.Version(ctx)
	if err != nil || v.Version != "1.2.3" || v.IrisMin != "0.28.0" || v.GPU.Modelo != "NVIDIA GeForce RTX 4090" || v.GPU.VRAMMb != 24564+15360 || len(v.Catalogo.LLM) != 1 {
		t.Fatalf("versión: %v %+v", err, v)
	}
	// Una lectura vieja se da por perdida.
	escribir(time.Now().Add(-time.Minute))
	if m, _ := c.Metricas(ctx); m.GPUs.Estado != voz.EstadoOmitido {
		t.Fatalf("lectura vieja: %+v", m.GPUs)
	}

	// Compatibilidad en los dos sentidos: un Iris fuera del rango no usa el modelo, pero sí lee
	// /salud y /version para decir qué actualizar.
	viejo := b.Cliente(cert, r.Token, "0.27.0", r.HuellaServidor)
	_, err = viejo.Hacer(ctx, "POST", voz.RutaChat, chatAdaptador("modelo-a:latest", false), voz.TopeRespuestaLLM)
	esperar(t, "426 iris_incompatible", err)
	s, err := viejo.Salud(ctx)
	if err != nil || s.IrisMin != "0.28.0" {
		t.Fatalf("salud de un Iris incompatible: %v", err)
	}
	if ok, motivo := voz.Compatible(v.Manifiesto, "0.27.0", "0.1.0"); ok || motivo != "iris_antiguo" {
		t.Fatalf("compatible: %v %s", ok, motivo)
	}
	if ok, motivo := voz.Compatible(v.Manifiesto, "1.0.0", "0.1.0"); ok || motivo != "iris_nuevo" {
		t.Fatalf("compatible: %v %s", ok, motivo)
	}
	if ok, motivo := voz.Compatible(v.Manifiesto, "0.30.1", "2.0.0"); ok || motivo != "paquete_antiguo" {
		t.Fatalf("compatible: %v %s", ok, motivo)
	}
	if ok, _ := voz.Compatible(v.Manifiesto, "0.30.1", "1.0.0"); !ok {
		t.Fatal("debía ser compatible")
	}

	// Restricción por IP: se enciende en el servidor; desde otra IP, 403.
	if err := b.Almacen.Cambiar(func(e *voz.Estado) error { e.RestringirIP, e.IPObservada = true, "203.0.113.9"; return nil }); err != nil {
		t.Fatal(err)
	}
	_, err = c.Salud(ctx)
	esperar(t, "403 ip_no_permitida", err)
}

func TestProxyDeDescarga(t *testing.T) {
	p := voz.ProxyDescarga{Hosts: []string{"registry.ollama.ai", "almacen.r2.cloudflarestorage.com", ".r2.cloudflarestorage.com"}}
	for host, quiero := range map[string]bool{
		"registry.ollama.ai": true, "REGISTRY.ollama.ai.": true, "almacen.r2.cloudflarestorage.com": true,
		// Solo nombres exactos: el bucket de otro en el mismo almacén no pasa, ni con un sufijo en la lista.
		"otro.r2.cloudflarestorage.com": false, "x.almacen.r2.cloudflarestorage.com": false,
		"r2.cloudflarestorage.com": false, "evil.com": false, "registry.ollama.ai.evil.com": false,
		"127.0.0.1": false, "10.0.0.1": false, "": false,
	} {
		if p.Permitido(host) != quiero {
			t.Fatalf("%q: quería %v", host, quiero)
		}
	}
}

// El paquete del servidor no puede traer nada con acceso a la base ni a mensajería (voz.md §4.7).
func TestSinDependenciasDeIris(t *testing.T) {
	ctx, cancelar := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancelar()
	out, err := exec.CommandContext(ctx, "go", "list", "-deps", ".", "../../cmd/voz-puerta").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		prohibido := strings.Contains(dep, "jackc/pgx") || strings.Contains(dep, "riverqueue") || strings.Contains(dep, "redis") ||
			(strings.HasPrefix(dep, "github.com/HCDestroyer/iris/apps/api/internal/") && !strings.HasPrefix(dep, "github.com/HCDestroyer/iris/apps/api/internal/voz"))
		if prohibido {
			t.Fatalf("el servidor de IA no puede depender de %s", dep)
		}
	}
}
