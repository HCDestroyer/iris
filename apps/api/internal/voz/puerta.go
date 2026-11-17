package voz

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Rutas de la puerta (literales exactos: sin query, sin «%», sin normalizar).
const (
	RutaEnlazar    = Base + "/enlazar"
	RutaVersion    = Base + "/version"
	RutaSalud      = Base + "/salud"
	RutaMetricas   = Base + "/metricas"
	RutaCredencial = Base + "/credencial"
	RutaChat       = Base + "/llm/v1/chat/completions"
	RutaModelos    = Base + "/llm/v1/models"
)

// Topes de fábrica del proxy /llm/ (voz.md §4.4).
const (
	TopePeticionLLM  = 1 << 20
	TopeRespuestaLLM = 4 << 20
	PlazoLLM         = 120 * time.Second
	topeCuerpoCorto  = 16 << 10
)

// Config de la puerta.
type Config struct {
	Almacen    *Almacen
	Manifiesto Manifiesto
	// OllamaURL: base del contenedor llm en la red interna (http://llm:11434).
	OllamaURL string
	// HTTPOllama: cliente hacia Ollama (pruebas). Por defecto, uno sin conexiones reutilizadas.
	HTTPOllama *http.Client
	// MedidorRuta: archivo que deja el contenedor medidor con la lectura de la GPU ("" = sin GPU).
	MedidorRuta string
	// DiscoRuta: directorio del volumen de modelos (para el uso de disco).
	DiscoRuta string
	// MaxSimultaneas: peticiones al modelo a la vez (0 = calcular con la VRAM y los modelos permitidos).
	MaxSimultaneas int
	// MaxTokens: tope de max_tokens por petición (0 = 4096).
	MaxTokens int
	// PlazoLLM: duración máxima de una petición al modelo (0 = PlazoLLM).
	PlazoLLM time.Duration
	Ahora    func() time.Time
	Log      *slog.Logger
}

// Puerta termina TLS y verifica mTLS, firma y nonce de toda ruta (voz.md §4.1).
type Puerta struct {
	cfg      Config
	nonces   *nonces
	inicio   time.Time
	ollama   *http.Client
	mu       sync.Mutex
	plazas   int // ocupadas (bajo mu)
	limMetr  *cubo
	limEnl   *cubo
	intentos map[string]int // intentos de enlace por IP en la ventana actual (bajo mu)
	ventanaC string         // código de la ventana a la que pertenecen los intentos (bajo mu)
	cache    cacheOllama
	cpu      medidorCPU
}

// Nueva arma la puerta.
func Nueva(cfg Config) (*Puerta, error) {
	if cfg.Almacen == nil {
		return nil, errors.New("falta el almacén de estado")
	}
	if err := cfg.Manifiesto.Validar(); err != nil {
		return nil, err
	}
	if cfg.Ahora == nil {
		cfg.Ahora = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 4096
	}
	if cfg.PlazoLLM <= 0 {
		cfg.PlazoLLM = PlazoLLM
	}
	p := &Puerta{cfg: cfg, nonces: nuevosNonces(), inicio: cfg.Ahora(), ollama: cfg.HTTPOllama,
		limMetr: nuevoCubo(1, 5), limEnl: nuevoCubo(0.5, 1), intentos: map[string]int{}}
	if p.ollama == nil {
		// Una conexión nueva por petición: tras un corte nunca se reutiliza una conexión a medias.
		// El plazo de cada petición lo pone su contexto.
		p.ollama = &http.Client{Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil,
			DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}}
	}
	return p, nil
}

func (p *Puerta) fijada(e Estado, huella string, ahora time.Time) bool {
	if !e.Enlazado() || huella == "" {
		return false
	}
	if Iguales(huella, e.HuellaCliente) {
		return true
	}
	return e.HuellaClienteAnterior != "" && e.anteriorVigente(ahora) && Iguales(huella, e.HuellaClienteAnterior)
}

// TLS es la configuración del puerto de control: TLS 1.3, certificado de cliente obligatorio y
// fijado por huella en el propio saludo. Sin certificado fijado solo se pasa del saludo mientras la
// ventana de enlace está abierta, y entonces solo existe /enlazar.
func (p *Puerta) TLS() *tls.Config {
	return &tls.Config{
		MinVersion:             tls.VersionTLS13,
		ClientAuth:             tls.RequireAnyClientCert,
		SessionTicketsDisabled: true,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return p.cfg.Almacen.Certificado()
		},
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("sin certificado de cliente")
			}
			ahora := p.cfg.Ahora()
			e, err := p.cfg.Almacen.Leer()
			if err != nil {
				return err
			}
			if p.fijada(e, Huella(cs.PeerCertificates[0].Raw), ahora) {
				return nil
			}
			if _, ok := p.cfg.Almacen.Ventana(ahora); ok {
				return nil
			}
			return errors.New("certificado de cliente no fijado")
		},
	}
}

type problema struct {
	Codigo       string `json:"codigo"`
	Detalle      string `json:"detalle,omitempty"`
	HoraServidor int64  `json:"horaServidor,omitempty"`
}

func ipDe(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// respuesta lleva lo necesario para firmar lo que se contesta.
type respuesta struct {
	w     http.ResponseWriter
	token []byte
	nonce string
}

func (o respuesta) json(estado int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		estado, b = http.StatusInternalServerError, []byte(`{"codigo":"error"}`)
	}
	o.crudo(estado, b)
}

func (o respuesta) crudo(estado int, b []byte) {
	h := o.w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	if o.token != nil && o.nonce != "" {
		h.Set(CabFirmaRespuesta, FirmaRespuesta(o.token, o.nonce, estado, b))
	}
	o.w.WriteHeader(estado)
	_, _ = o.w.Write(b)
}

func (o respuesta) error(estado int, codigo, detalle string) {
	o.json(estado, problema{Codigo: codigo, Detalle: detalle})
}

func leerCuerpo(r *http.Request, tope int64) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r.Body, tope+1))
	if err != nil || int64(len(b)) > tope {
		return nil, false
	}
	return b, true
}

// ServeHTTP atiende el puerto de control.
func (p *Puerta) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ahora := p.cfg.Ahora()
	sinFirma := respuesta{w: w}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		sinFirma.error(http.StatusUnauthorized, "no_autorizado", "")
		return
	}
	huella := Huella(r.TLS.PeerCertificates[0].Raw)
	uri := r.RequestURI
	if uri == RutaEnlazar {
		p.enlazar(sinFirma, r, huella, ahora)
		return
	}
	e, err := p.cfg.Almacen.Leer()
	if err != nil {
		p.cfg.Log.Error("estado ilegible", "error", err)
		sinFirma.error(http.StatusInternalServerError, "error", "")
		return
	}
	// El saludo ya fijó la huella; se repite por petición porque una conexión abierta sobrevive a
	// una revocación y porque con la ventana de enlace abierta el saludo admite a cualquiera.
	if !p.fijada(e, huella, ahora) {
		sinFirma.error(http.StatusUnauthorized, "no_autorizado", "")
		return
	}
	if e.RestringirIP && ipDe(r) != e.IPObservada {
		sinFirma.error(http.StatusForbidden, "ip_no_permitida", "")
		return
	}
	marca, errM := strconv.ParseInt(r.Header.Get(CabMarca), 10, 64)
	nonce, version, firma := r.Header.Get(CabNonce), r.Header.Get(CabVersion), r.Header.Get(CabFirma)
	if errM != nil || !nonceValido(nonce) || !VersionValida(version) || len(firma) != 64 {
		sinFirma.error(http.StatusUnauthorized, "no_autorizado", "")
		return
	}
	if d := ahora.Sub(time.Unix(marca, 0)); d > VentanaMarca || d < -VentanaMarca {
		sinFirma.json(http.StatusUnauthorized, problema{Codigo: "reloj_desfasado", HoraServidor: ahora.Unix()})
		return
	}
	tope := int64(topeCuerpoCorto)
	if uri == RutaChat {
		tope = TopePeticionLLM
	}
	cuerpo, ok := leerCuerpo(r, tope)
	if !ok {
		sinFirma.error(http.StatusRequestEntityTooLarge, "cuerpo_demasiado_grande", "")
		return
	}
	token := e.Token
	if !Iguales(firma, FirmaPeticion(token, r.Method, uri, r.Header.Get(CabMarca), nonce, version, cuerpo)) {
		token = nil
		if len(e.TokenAnterior) == LargoToken && e.anteriorVigente(ahora) &&
			Iguales(firma, FirmaPeticion(e.TokenAnterior, r.Method, uri, r.Header.Get(CabMarca), nonce, version, cuerpo)) {
			token = e.TokenAnterior
		}
	}
	if token == nil {
		sinFirma.error(http.StatusUnauthorized, "no_autorizado", "")
		return
	}
	if !p.nonces.usar(nonce, ahora) {
		sinFirma.error(http.StatusUnauthorized, "no_autorizado", "")
		return
	}
	o := respuesta{w: w, token: token, nonce: nonce}
	inicio := time.Now()
	estado := p.despachar(o, r, uri, cuerpo, e, version, ahora)
	// Nunca el cuerpo: solo ruta, tamaño, duración y código.
	p.cfg.Log.Info("peticion", "ruta", uri, "metodo", r.Method, "estado", estado, "bytes", len(cuerpo), "ms", time.Since(inicio).Milliseconds())
}

func (p *Puerta) despachar(o respuesta, r *http.Request, uri string, cuerpo []byte, e Estado, versionIris string, ahora time.Time) int {
	switch r.Method + " " + uri {
	case http.MethodGet + " " + RutaVersion:
		return p.version(o, r, e)
	case http.MethodGet + " " + RutaSalud:
		return p.salud(o, r, e)
	case http.MethodGet + " " + RutaMetricas:
		if !p.limMetr.permitir(ahora) {
			o.error(http.StatusTooManyRequests, "demasiadas_peticiones", "")
			return http.StatusTooManyRequests
		}
		return p.metricas(o, r, e)
	case http.MethodPost + " " + RutaCredencial:
		return p.credencial(o, cuerpo, ahora)
	case http.MethodPost + " " + RutaChat, http.MethodGet + " " + RutaModelos:
		if !p.cfg.Manifiesto.AdmiteIris(versionIris) {
			o.error(http.StatusUpgradeRequired, "iris_incompatible", "")
			return http.StatusUpgradeRequired
		}
		if r.Method == http.MethodGet {
			return p.modelos(o, r, e)
		}
		return p.chat(o, r, cuerpo, e)
	}
	o.error(http.StatusNotFound, "no_encontrado", "")
	return http.StatusNotFound
}

// --- Enlace --------------------------------------------------------------------------------------

type peticionEnlace struct {
	Nonce  string `json:"nonce"`
	Prueba string `json:"prueba"`
}

type respuestaEnlace struct {
	Nonce      string     `json:"nonce"`
	Prueba     string     `json:"prueba"`
	Manifiesto Manifiesto `json:"manifiesto"`
}

const intentosEnlacePorIP = 5

// enlazar es la única ruta antes del mTLS fijado y solo existe con la ventana abierta. Un código
// vencido, usado o equivocado recibe siempre la misma respuesta. Los fallos no cierran la ventana
// (cualquiera que alcance el puerto podría impedir el enlace): hay límite de frecuencia.
func (p *Puerta) enlazar(o respuesta, r *http.Request, huellaCliente string, ahora time.Time) {
	codigo, abierta := p.cfg.Almacen.Ventana(ahora)
	if !abierta || r.Method != http.MethodPost {
		o.error(http.StatusNotFound, "no_encontrado", "")
		return
	}
	ip := ipDe(r)
	p.mu.Lock()
	if p.ventanaC != codigo {
		p.ventanaC, p.intentos = codigo, map[string]int{}
	}
	p.intentos[ip]++
	pasa := p.intentos[ip] <= intentosEnlacePorIP && len(p.intentos) <= 1024
	p.mu.Unlock()
	if !pasa || !p.limEnl.permitir(ahora) {
		o.error(http.StatusTooManyRequests, "demasiadas_peticiones", "")
		return
	}
	negar := func() { o.error(http.StatusUnauthorized, "no_autorizado", "") }
	cuerpo, ok := leerCuerpo(r, 4<<10)
	if !ok {
		negar()
		return
	}
	var in peticionEnlace
	dec := json.NewDecoder(bytes.NewReader(cuerpo))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || !nonceValido(in.Nonce) || len(in.Prueba) != 64 {
		negar()
		return
	}
	cert, err := p.cfg.Almacen.Certificado()
	if err != nil {
		negar()
		return
	}
	huellaServidor := huellaDeTLS(cert)
	if !Iguales(in.Prueba, PruebaIris(codigo, in.Nonce, huellaServidor, huellaCliente)) {
		p.cfg.Log.Warn("enlace rechazado", "ip", ip)
		negar()
		return
	}
	nonceS, err := NuevoNonce()
	if err != nil {
		negar()
		return
	}
	token := TokenDeEnlace(codigo, in.Nonce, nonceS, huellaServidor, huellaCliente)
	err = p.cfg.Almacen.Cambiar(func(e *Estado) error {
		e.Token, e.TokenAnterior, e.AnteriorVence = token, nil, time.Time{}
		e.HuellaCliente, e.HuellaClienteAnterior = huellaCliente, ""
		e.IPObservada, e.EnlazadoEn = ip, ahora.UTC()
		return nil
	})
	if err == nil {
		err = p.cfg.Almacen.CerrarVentana()
	}
	if err != nil {
		p.cfg.Log.Error("no se pudo guardar el enlace", "error", err)
		o.error(http.StatusInternalServerError, "error", "")
		return
	}
	p.cfg.Log.Info("enlazado", "ip", ip)
	o.json(http.StatusOK, respuestaEnlace{Nonce: nonceS,
		Prueba: PruebaServidor(codigo, in.Nonce, nonceS, huellaServidor, huellaCliente), Manifiesto: p.cfg.Manifiesto})
}

// --- Credencial ----------------------------------------------------------------------------------

// PeticionCredencial: rotación del token y del certificado de cliente, y confirmación de la huella
// siguiente del servidor (voz.md §4.2 y §4.10).
type PeticionCredencial struct {
	// Token nuevo (32 bytes). Vacío = no cambia.
	Token []byte `json:"token,omitempty"`
	// HuellaCliente: huella del certificado de cliente nuevo. Vacío = no cambia.
	HuellaCliente string `json:"huellaCliente,omitempty"`
	// ConvivenciaSeg: cuánto siguen valiendo el token y la huella anteriores (0 = revocar ahora).
	ConvivenciaSeg int `json:"convivenciaSeg"`
	// ConfirmaHuellaServidor: Iris ya fijó esta huella siguiente; la puerta puede empezar a usarla.
	ConfirmaHuellaServidor string `json:"confirmaHuellaServidor,omitempty"`
}

// ConvivenciaMaxima de una rotación.
const ConvivenciaMaxima = 24 * time.Hour

func (p *Puerta) credencial(o respuesta, cuerpo []byte, ahora time.Time) int {
	var in PeticionCredencial
	dec := json.NewDecoder(bytes.NewReader(cuerpo))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || (len(in.Token) != 0 && len(in.Token) != LargoToken) ||
		(in.HuellaCliente != "" && !HuellaValida(in.HuellaCliente)) ||
		(in.ConfirmaHuellaServidor != "" && !HuellaValida(in.ConfirmaHuellaServidor)) {
		o.error(http.StatusUnprocessableEntity, "credencial_invalida", "")
		return http.StatusUnprocessableEntity
	}
	conv := time.Duration(in.ConvivenciaSeg) * time.Second
	if conv < 0 || conv > ConvivenciaMaxima {
		o.error(http.StatusUnprocessableEntity, "credencial_invalida", "")
		return http.StatusUnprocessableEntity
	}
	if in.ConfirmaHuellaServidor != "" {
		if err := p.cfg.Almacen.PromoverSiguiente(in.ConfirmaHuellaServidor); err != nil {
			o.error(http.StatusConflict, "sin_huella_siguiente", "")
			return http.StatusConflict
		}
		p.cfg.Log.Info("certificado del servidor renovado")
	}
	if len(in.Token) != 0 || in.HuellaCliente != "" {
		err := p.cfg.Almacen.Cambiar(func(e *Estado) error {
			// Token y certificado de cliente caen juntos: la convivencia es una sola.
			if conv > 0 {
				if len(in.Token) != 0 {
					e.TokenAnterior = e.Token
				}
				if in.HuellaCliente != "" && in.HuellaCliente != e.HuellaCliente {
					e.HuellaClienteAnterior = e.HuellaCliente
				}
				e.AnteriorVence = ahora.Add(conv).UTC()
			} else {
				e.TokenAnterior, e.HuellaClienteAnterior, e.AnteriorVence = nil, "", time.Time{}
			}
			if len(in.Token) != 0 {
				e.Token = in.Token
			}
			if in.HuellaCliente != "" {
				e.HuellaCliente = in.HuellaCliente
			}
			return nil
		})
		if err != nil {
			p.cfg.Log.Error("no se pudo guardar la credencial", "error", err)
			o.error(http.StatusInternalServerError, "error", "")
			return http.StatusInternalServerError
		}
		p.cfg.Log.Info("credencial rotada", "convivencia_seg", in.ConvivenciaSeg)
	}
	o.json(http.StatusOK, struct {
		Estado string `json:"estado"`
	}{"ok"})
	return http.StatusOK
}

// reservar toma una plaza del modelo; false si no hay (no hay cola).
func (p *Puerta) reservar(max int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.plazas >= max {
		return false
	}
	p.plazas++
	return true
}

func (p *Puerta) soltar() {
	p.mu.Lock()
	p.plazas--
	p.mu.Unlock()
}

func (p *Puerta) ocupadas() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.plazas
}
