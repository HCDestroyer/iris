// Package sippuente es el puente SIP del teléfono de Iris (contratos/voz.md §3.12, E4c): una conexión
// WebSocket del navegador (o de la app) con una conexión de Iris al WSS de la Issabel, 1:1. Iris no es
// una centralita ni un B2BUA: reenvía mensajes SIP con reglas escritas en el servidor.
//
//   - La contraseña SIP solo vive aquí: Iris responde los desafíos Digest (401/407) y ninguno llega al
//     navegador. Para que el navegador no note el reintento, el puente lleva por Call-ID el desfase de
//     CSeq y por transacción la branch original.
//   - El navegador solo marca los destinos que le da la política (los que resolvió el servidor), solo
//     transfiere a la lista de su empresa, su From es fijo y los métodos y tamaños están acotados.
//   - Al cortar (sin acceso, reemplazada) manda BYE a cada diálogo establecido.
//
// Lee y escribe los mensajes con github.com/emiago/sipgo/sip (solo el analizador; no usa sus
// transportes ni transacciones).
package sippuente

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
)

// Lado es un extremo del puente: un WebSocket con subprotocolo sip (un mensaje SIP por mensaje).
type Lado interface {
	Leer(ctx context.Context) ([]byte, error)
	Escribir(ctx context.Context, b []byte) error
	// Cerrar con un código de cierre de WebSocket y su motivo.
	Cerrar(codigo int, motivo string)
}

// Credencial SIP de la extensión. El secreto nunca sale de este paquete.
type Credencial struct {
	Usuario string
	Secreto string
	Dominio string
}

// Destino de un INVITE nuevo o de un REFER, resuelto por la política.
type Destino struct {
	// Usuario: la parte usuario de la URI hacia la Issabel (dígitos).
	Usuario string
	// LlamadaID: la llamada de Iris (saliente externa); nil en una interna.
	LlamadaID *uuid.UUID
}

// Hechos de una saliente del puente.
const (
	HechoContestada = "contestada"
	HechoFallo      = "fallo"
	HechoColgada    = "colgada"
	// HechoTransferida: la Issabel aceptó (2xx) el REFER del navegador.
	HechoTransferida = "transferida"
)

// HechoLlamada es un cambio de una saliente que marcó el puente.
type HechoLlamada struct {
	LlamadaID uuid.UUID
	Tipo      string
	// Codigo SIP final (fallo).
	Codigo int
	// Cancelada: el navegador mandó CANCEL antes de que contestaran.
	Cancelada bool
	// Destino: el número al que se transfirió (transferida).
	Destino string
}

// Politica decide en el servidor lo que el navegador puede hacer.
type Politica interface {
	// Invitar: el destino de un INVITE nuevo cuya Request-URI trae usuario (error = 403 local).
	Invitar(ctx context.Context, usuario string) (Destino, error)
	// Transferir: el número al que se transfiere (Refer-To con usuario; error = 403 local).
	Transferir(ctx context.Context, usuario string, llamada *uuid.UUID) (string, error)
	// Entrante: la llamada de Iris de un INVITE que llega desde la Issabel (o nil).
	Entrante(ctx context.Context, numero string) *uuid.UUID
	// Hecho: un cambio de una saliente (contestada, fallo, colgada, transferida). En orden, fuera del bucle.
	Hecho(ctx context.Context, h HechoLlamada)
	// Credenciales: la Issabel rechazó la credencial guardada (el puente corta después).
	Credenciales(ctx context.Context)
}

// Opciones del puente.
type Opciones struct {
	Credencial Credencial
	// Registrar: puede mandar REGISTER y recibir llamadas (panel). La app, primera etapa, solo marca.
	Registrar bool
	Politica  Politica
	// MaxPorMinuto: mensajes del navegador por minuto (200); más, cierre 4008.
	MaxPorMinuto int
	// MaxDialogos: llamadas a la vez por conexión (3); el siguiente INVITE, 486 local.
	MaxDialogos int
	// Relevo: la IP del TURN de Iris hacia la Issabel (IRIS_TURN_RELAY_IP). Con ella, el SDP del
	// navegador solo puede anunciar esa IP; sin ella, solo candidatos relay.
	Relevo string
	Log    *slog.Logger
}

// Códigos de cierre del WebSocket del navegador (contratos/voz.md §3.12, E4c).
const (
	CierreNormal        = 1000
	CierreApagado       = 1001
	CierreReemplazada   = 4001
	CierreSinAcceso     = 4003
	CierreLimite        = 4008
	CierreNoResponde    = 4502
	maxExpiraciones     = 600
	vidaTransaccion     = 64 * time.Second
	maxDialogosGuardado = 64
	// maxOtrosGuardados: REGISTER y OPTIONS fuera de un diálogo (mapa aparte: nunca quitan lugar a una llamada).
	maxOtrosGuardados = 16
	// maxDeCentralita: transacciones de la Issabel que esperan respuesta del navegador.
	maxDeCentralita = 256
	vidaOtro        = 15 * time.Minute
)

// ErrCortado: el puente terminó por Cortar.
var ErrCortado = errors.New("sippuente: cortado")

var metodosNavegador = map[sip.RequestMethod]bool{
	sip.REGISTER: true, sip.INVITE: true, sip.ACK: true, sip.BYE: true, sip.CANCEL: true, sip.OPTIONS: true,
	sip.INFO: true, sip.UPDATE: true, sip.PRACK: true, sip.REFER: true,
}

type claveTx struct {
	rama   string
	metodo sip.RequestMethod
}

type transaccion struct {
	ramaOriginal string
	ramaEnviada  string
	metodo       sip.RequestMethod
	delta        uint32
	seqOriginal  uint32
	texto        string
	intentos     int
	callID       string
	creada       time.Time
	acusada      bool // Iris ya mandó el ACK de su final (401/407 convertido)
	final        bool
	// destino: el número del REFER (para el hecho «transferida» con el 2xx).
	destino string
	// otro: REGISTER u OPTIONS fuera de un diálogo (su estado vive en Puente.otros).
	otro bool
}

type dialogo struct {
	offset     uint32
	saliente   bool
	confirmado bool
	terminado  bool
	cancelado  bool
	avisado    map[string]bool
	llamada    *uuid.UUID
	// Saliente: el último INVITE enviado a la Issabel y su 2xx (para el BYE al cortar).
	invite, resp2xx string
	// Entrante: el INVITE de la Issabel y la 2xx del navegador.
	inviteEntrante, resp2xxNav string
	ultimaCSeq                 uint32
	actividad                  time.Time
	esLlamada                  bool
	// Etiquetas del diálogo: la del navegador y la de la Issabel (la segunda, cuando se conoce).
	tagLocal, tagRemoto string
	// Último DTMF por INFO (para cortar ## y *2).
	ultimoDTMF   byte
	ultimoDTMFEn time.Time
}

// Puente une el navegador con la Issabel.
type Puente struct {
	o        Opciones
	nav, cen Lado

	mu       sync.Mutex
	dialogos map[string]*dialogo
	// otros: REGISTER y OPTIONS fuera de un diálogo, con su propio tope (no cuentan en dialogos).
	otros map[string]*dialogo
	// pendientesCen: branch y método de cada petición de la Issabel que se pasó al navegador; solo
	// sus respuestas vuelven a la Issabel.
	pendientesCen map[claveTx]time.Time
	txs           map[claveTx]*transaccion
	invites       map[string]*transaccion // branch original del INVITE → último intento
	locales       map[string]bool         // branch de INVITE rechazado aquí (su ACK no sale)
	minuto        time.Time
	cuenta        int
	cortado       bool

	hechos chan HechoLlamada
	fin    chan struct{}
	finUna sync.Once
	motivo struct {
		codigo int
		texto  string
	}
}

// Nuevo arma el puente (no arranca nada hasta Correr).
func Nuevo(o Opciones, navegador, centralita Lado) *Puente {
	if o.MaxPorMinuto <= 0 {
		o.MaxPorMinuto = 200
	}
	if o.MaxDialogos <= 0 {
		o.MaxDialogos = 3
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Puente{o: o, nav: navegador, cen: centralita, dialogos: map[string]*dialogo{}, otros: map[string]*dialogo{},
		pendientesCen: map[claveTx]time.Time{}, txs: map[claveTx]*transaccion{},
		invites: map[string]*transaccion{}, locales: map[string]bool{}, hechos: make(chan HechoLlamada, 64), fin: make(chan struct{})}
}

// Correr reenvía hasta que un lado se cierra, el contexto termina o alguien llama a Cortar.
func (p *Puente) Correr(ctx context.Context) error {
	ctx, cancelar := context.WithCancel(ctx)
	defer cancelar()
	hechosHechos := make(chan struct{})
	go func() {
		defer close(hechosHechos)
		for h := range p.hechos {
			p.unHecho(ctx, h)
		}
	}()
	errs := make(chan error, 2)
	go func() { errs <- p.bucle(ctx, p.nav, p.deNavegador) }()
	go func() { errs <- p.bucle(ctx, p.cen, p.deCentralita) }()
	var err error
	pendientes := 2
	select {
	case err = <-errs:
		pendientes--
	case <-p.fin:
		err = ErrCortado
	case <-ctx.Done():
		err = ctx.Err()
	}
	// Lo que quedó abierto se cuelga en la Issabel (no queda una llamada sin control).
	p.colgarTodo(context.WithoutCancel(ctx))
	codigo, texto := CierreNormal, ""
	p.mu.Lock()
	if p.motivo.codigo != 0 {
		codigo, texto = p.motivo.codigo, p.motivo.texto
	}
	p.mu.Unlock()
	if errors.Is(err, errCentralita) && codigo == CierreNormal {
		codigo, texto = CierreNoResponde, "centralita_no_responde"
	}
	p.nav.Cerrar(codigo, texto)
	p.cen.Cerrar(CierreNormal, "")
	cancelar()
	for ; pendientes > 0; pendientes-- {
		<-errs
	}
	close(p.hechos)
	<-hechosHechos
	return err
}

// Cortar termina el puente con un código para el navegador (4001, 4003…): manda BYE a lo establecido.
func (p *Puente) Cortar(codigo int, motivo string) {
	p.mu.Lock()
	if p.motivo.codigo == 0 {
		p.motivo.codigo, p.motivo.texto = codigo, motivo
	}
	p.mu.Unlock()
	p.finUna.Do(func() { close(p.fin) })
}

var errCentralita = errors.New("sippuente: la centralita cerró")

func (p *Puente) unHecho(ctx context.Context, h HechoLlamada) {
	defer func() {
		if r := recover(); r != nil {
			p.o.Log.Error("telefono: pánico al guardar un hecho", "panico", r)
		}
	}()
	hctx, c := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer c()
	p.o.Politica.Hecho(hctx, h)
}

func (p *Puente) bucle(ctx context.Context, l Lado, manejar func(context.Context, []byte) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			p.o.Log.Error("telefono: pánico en el puente", "panico", r)
			err = errors.New("sippuente: pánico")
		}
	}()
	for {
		b, err := l.Leer(ctx)
		if err != nil {
			if l == p.cen {
				return errCentralita
			}
			return err
		}
		if err := manejar(ctx, b); err != nil {
			return err
		}
	}
}

// Del navegador ---------------------------------------------------------------------------------------

func (p *Puente) deNavegador(ctx context.Context, b []byte) error {
	p.mu.Lock()
	ahora := time.Now()
	if ahora.Sub(p.minuto) >= time.Minute {
		p.minuto, p.cuenta = ahora, 0
	}
	p.cuenta++
	if p.cuenta > p.o.MaxPorMinuto {
		if p.motivo.codigo == 0 {
			p.motivo.codigo, p.motivo.texto = CierreLimite, "limite"
		}
		p.mu.Unlock()
		p.finUna.Do(func() { close(p.fin) })
		return nil
	}
	p.mu.Unlock()
	if esLatido(b) {
		return nil // CRLF de keep-alive
	}
	if validarCrudo(b) != nil {
		p.o.Log.DebugContext(ctx, "telefono: mensaje del navegador mal formado")
		return nil
	}
	msg, err := sip.ParseMessage(b)
	if err != nil || !completo(msg) {
		p.o.Log.DebugContext(ctx, "telefono: mensaje del navegador no válido")
		return nil
	}
	switch m := msg.(type) {
	case *sip.Request:
		return p.peticionNavegador(ctx, m)
	case *sip.Response:
		return p.respuestaNavegador(ctx, m)
	}
	return nil
}

func esLatido(b []byte) bool {
	return len(strings.TrimSpace(string(b))) == 0
}

// completo: las cabeceras sin las que no se puede seguir un mensaje.
func completo(m sip.Message) bool {
	return m.CallID() != nil && m.CSeq() != nil && m.From() != nil && m.To() != nil && m.Via() != nil && *m.CallID() != ""
}

type conCabeceras interface {
	Headers() []sip.Header
	RemoveHeader(name string) bool
}

// quitar borra todas las cabeceras con esos nombres sin importar mayúsculas (RemoveHeader de sipgo
// compara el nombre exacto: «authorization:» se le escaparía).
func quitar(m conCabeceras, nombres ...string) {
	for {
		borro := false
		for _, h := range m.Headers() {
			for _, n := range nombres {
				if strings.EqualFold(h.Name(), n) {
					m.RemoveHeader(h.Name())
					borro = true
				}
			}
			if borro {
				break
			}
		}
		if !borro {
			return
		}
	}
}

var cabecerasAutenticacion = []string{"Authorization", "Proxy-Authorization", "WWW-Authenticate", "Proxy-Authenticate", "Authentication-Info"}

func (p *Puente) responderNavegador(ctx context.Context, req *sip.Request, codigo int, razon string) error {
	if req.IsAck() {
		return nil
	}
	r := sip.NewResponseFromRequest(req, codigo, razon, nil)
	if req.IsInvite() {
		p.mu.Lock()
		p.locales[req.Via().Params.GetOr("branch", "")] = true
		p.mu.Unlock()
	}
	return p.nav.Escribir(ctx, []byte(r.String()))
}

func (p *Puente) fromPropio(m sip.Message) bool {
	f := m.From()
	return f != nil && f.Address.User == p.o.Credencial.Usuario && strings.EqualFold(f.Address.Host, p.o.Credencial.Dominio)
}

func (p *Puente) peticionNavegador(ctx context.Context, req *sip.Request) error {
	if !metodosNavegador[req.Method] {
		return p.responderNavegador(ctx, req, sip.StatusMethodNotAllowed, "Method Not Allowed")
	}
	if len(req.GetHeaders("Via")) != 1 {
		return p.responderNavegador(ctx, req, sip.StatusBadRequest, "Bad Request")
	}
	soloPermitidas(req, cabecerasPeticion)
	if req.Method != sip.REFER {
		quitar(req, "Refer-To")
	}
	rama := req.Via().Params.GetOr("branch", "")
	if !strings.HasPrefix(rama, sip.RFC3261BranchMagicCookie) || len(rama) > 128 {
		return p.responderNavegador(ctx, req, sip.StatusBadRequest, "Bad Request")
	}
	// From siempre el de su extensión (también dentro de un diálogo, en ACK y en CANCEL).
	if !p.fromPropio(req) {
		return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
	}
	if !p.contactosValidos(req, req.Method == sip.REGISTER) {
		return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
	}
	if err := filtrarSDP(req, p.o.Relevo); err != nil {
		return p.responderNavegador(ctx, req, sip.StatusNotAcceptableHere, "Not Acceptable Here")
	}
	callID := string(*req.CallID())
	etiquetaTo, _ := req.To().Params.Get("tag")

	p.mu.Lock()
	p.limpiar()
	d := p.dialogos[callID]
	p.mu.Unlock()

	switch req.Method {
	case sip.ACK:
		return p.ackNavegador(ctx, req, d)
	case sip.CANCEL:
		return p.cancelNavegador(ctx, req, d)
	}

	if etiquetaTo == "" { // fuera de un diálogo
		quitar(req, "Route")
		switch req.Method {
		case sip.REGISTER:
			if !p.o.Registrar {
				return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
			}
			to := req.To()
			if to.Address.User != p.o.Credencial.Usuario || !strings.EqualFold(to.Address.Host, p.o.Credencial.Dominio) {
				return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
			}
			acotarExpira(req)
		case sip.INVITE:
			p.mu.Lock()
			previo := p.invites[rama]
			p.mu.Unlock()
			if previo != nil { // retransmisión del mismo INVITE: ya salió
				return nil
			}
			if p.dialogosVivos() >= p.o.MaxDialogos {
				return p.responderNavegador(ctx, req, sip.StatusBusyHere, "Busy Here")
			}
			dest, err := p.o.Politica.Invitar(ctx, req.Recipient.User)
			if err != nil || !soloDigitos(dest.Usuario) {
				return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
			}
			req.Recipient = sip.Uri{Scheme: "sip", User: dest.Usuario, Host: p.o.Credencial.Dominio}
			// To es la misma URI que se marca y From va sin nombre visible: el navegador no elige
			// qué ve la Issabel (ni lo que mostraría o guardaría en su CDR).
			req.To().DisplayName, req.To().Address = "", sip.Uri{Scheme: "sip", User: dest.Usuario, Host: p.o.Credencial.Dominio}
			req.From().DisplayName = ""
			p.mu.Lock()
			if d == nil {
				if d = p.nuevoDialogo(callID); d == nil {
					p.mu.Unlock()
					return p.responderNavegador(ctx, req, sip.StatusServiceUnavailable, "Service Unavailable")
				}
			}
			d.saliente, d.llamada, d.esLlamada = true, dest.LlamadaID, true
			d.tagLocal, _ = req.From().Params.Get("tag")
			p.mu.Unlock()
		case sip.OPTIONS:
		default:
			// BYE, INFO, UPDATE, PRACK y REFER fuera de un diálogo (sin etiqueta de To): no.
			return p.responderNavegador(ctx, req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist")
		}
	} else {
		etiquetaFrom, _ := req.From().Params.Get("tag")
		p.mu.Lock()
		// Solo una llamada que esta conexión conoce, con sus etiquetas (no se cuela otro diálogo).
		valido := d != nil && !d.terminado && d.esLlamada && d.tagLocal != "" && etiquetaFrom == d.tagLocal &&
			(d.tagRemoto == "" || etiquetaTo == d.tagRemoto)
		confirmado := valido && d.confirmado
		p.mu.Unlock()
		if !valido {
			return p.responderNavegador(ctx, req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist")
		}
		switch req.Method {
		case sip.INVITE, sip.REFER:
			if !confirmado {
				return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
			}
		case sip.REGISTER, sip.OPTIONS:
			return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
		case sip.INFO:
			c, err := digitoINFO(req)
			if err != nil {
				return p.responderNavegador(ctx, req, sip.StatusUnsupportedMediaType, "Unsupported Media Type")
			}
			if c != 0 {
				p.mu.Lock()
				ok := d.dtmfPermitido(c, time.Now())
				p.mu.Unlock()
				if !ok {
					return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
				}
			}
		}
		if req.Method == sip.REFER {
			if !p.o.Registrar {
				return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
			}
			rt := req.ReferTo()
			if rt == nil || len(req.GetHeaders("Refer-To"))+len(req.GetHeaders("r")) != 1 {
				return p.responderNavegador(ctx, req, sip.StatusBadRequest, "Bad Request")
			}
			p.mu.Lock()
			llamada := d.llamada
			p.mu.Unlock()
			destino, err := p.o.Politica.Transferir(ctx, rt.Address.User, llamada)
			if err != nil || !soloDigitos(destino) {
				return p.responderNavegador(ctx, req, sip.StatusForbidden, "Forbidden")
			}
			// Solo transferencia ciega a la URI que dio el servidor: sin parámetros ni Replaces.
			quitar(req, "Refer-To", "r", "Referred-By", "b")
			req.AppendHeader(sip.NewHeader("Refer-To", "<sip:"+destino+"@"+p.o.Credencial.Dominio+">"))
			return p.enviarPeticion(ctx, req, callID, rama, destino)
		}
	}
	return p.enviarPeticion(ctx, req, callID, rama, "")
}

// nuevoDialogo (con p.mu tomado): nil si ya hay demasiados guardados.
func (p *Puente) nuevoDialogo(callID string) *dialogo {
	if len(p.dialogos) >= maxDialogosGuardado {
		p.limpiar()
		if len(p.dialogos) >= maxDialogosGuardado {
			return nil
		}
	}
	d := &dialogo{avisado: map[string]bool{}, actividad: time.Now()}
	p.dialogos[callID] = d
	return d
}

// otroDialogo (con p.mu tomado): el estado de un REGISTER u OPTIONS fuera de un diálogo. Si el mapa
// está lleno se olvida el más viejo (solo pierde su desfase de CSeq; nunca bloquea una llamada).
func (p *Puente) otroDialogo(callID string) *dialogo {
	if d := p.otros[callID]; d != nil {
		return d
	}
	if len(p.otros) >= maxOtrosGuardados {
		viejo, cuando := "", time.Time{}
		for k, d := range p.otros {
			if viejo == "" || d.actividad.Before(cuando) {
				viejo, cuando = k, d.actividad
			}
		}
		delete(p.otros, viejo)
	}
	d := &dialogo{avisado: map[string]bool{}, actividad: time.Now()}
	p.otros[callID] = d
	return d
}

// dialogoDe (con p.mu tomado): el estado al que pertenece la transacción.
func (p *Puente) dialogoDe(t *transaccion) *dialogo {
	if t.otro {
		return p.otros[t.callID]
	}
	return p.dialogos[t.callID]
}

// contactosValidos: todos los Contact son del navegador y con su usuario SIP ("*" solo en un REGISTER).
func (p *Puente) contactosValidos(m sip.Message, registro bool) bool {
	for _, h := range m.GetHeaders("Contact") {
		if c, ok := h.(*sip.ContactHeader); ok {
			if !contactoPropio(c) || c.Address.User != p.o.Credencial.Usuario {
				return false
			}
			continue
		}
		if registro && strings.TrimSpace(h.Value()) == "*" {
			continue
		}
		return false
	}
	return true
}

func (p *Puente) dialogosVivos() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, d := range p.dialogos {
		if d.esLlamada && !d.terminado {
			n++
		}
	}
	return n
}

// enviarPeticion aplica el desfase de CSeq del diálogo y anota la transacción.
func (p *Puente) enviarPeticion(ctx context.Context, req *sip.Request, callID, rama, destino string) error {
	p.mu.Lock()
	d := p.dialogos[callID]
	otro := false
	if d == nil {
		// Solo REGISTER y OPTIONS llegan aquí sin diálogo (el INVITE lo creó antes): van al mapa
		// aparte, que nunca deja sin lugar a una llamada.
		if req.Method != sip.REGISTER && req.Method != sip.OPTIONS {
			p.mu.Unlock()
			return p.responderNavegador(ctx, req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist")
		}
		d, otro = p.otroDialogo(callID), true
	}
	cs := req.CSeq()
	orig := cs.SeqNo
	cs.SeqNo = orig + d.offset
	if cs.SeqNo > d.ultimaCSeq {
		d.ultimaCSeq = cs.SeqNo
	}
	d.actividad = time.Now()
	texto := req.String()
	t := &transaccion{ramaOriginal: rama, ramaEnviada: rama, metodo: req.Method, delta: d.offset, seqOriginal: orig, texto: texto, callID: callID, creada: time.Now(),
		destino: destino, otro: otro}
	p.txs[claveTx{rama, req.Method}] = t
	if req.IsInvite() {
		p.invites[rama] = t
		d.invite = texto
	}
	p.mu.Unlock()
	return p.cen.Escribir(ctx, []byte(texto))
}

func (p *Puente) ackNavegador(ctx context.Context, req *sip.Request, d *dialogo) error {
	rama := req.Via().Params.GetOr("branch", "")
	p.mu.Lock()
	if p.locales[rama] {
		p.mu.Unlock()
		return nil
	}
	if t := p.invites[rama]; t != nil { // ACK de una final no 2xx: misma transacción que el INVITE
		if t.acusada {
			p.mu.Unlock()
			return nil
		}
		req.Via().Params.Add("branch", t.ramaEnviada)
		req.CSeq().SeqNo = t.seqOriginal + t.delta
		req.Recipient = invitacionURI(t.texto, req.Recipient)
		p.mu.Unlock()
		return p.cen.Escribir(ctx, []byte(req.String()))
	}
	if etiqueta, _ := req.From().Params.Get("tag"); d == nil || !d.confirmado || etiqueta != d.tagLocal {
		p.mu.Unlock()
		return nil
	}
	req.CSeq().SeqNo += d.offset
	p.mu.Unlock()
	return p.cen.Escribir(ctx, []byte(req.String()))
}

// invitacionURI: la Request-URI con la que salió el INVITE (el ACK de una final no 2xx la repite).
func invitacionURI(texto string, otra sip.Uri) sip.Uri {
	m, err := sip.ParseMessage([]byte(texto))
	if r, ok := m.(*sip.Request); err == nil && ok {
		return r.Recipient
	}
	return otra
}

func (p *Puente) cancelNavegador(ctx context.Context, req *sip.Request, d *dialogo) error {
	rama := req.Via().Params.GetOr("branch", "")
	p.mu.Lock()
	t := p.invites[rama]
	if t == nil || t.final || d == nil {
		local := p.locales[rama]
		p.mu.Unlock()
		if local {
			return nil
		}
		return p.responderNavegador(ctx, req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist")
	}
	d.cancelado = true
	req.Via().Params.Add("branch", t.ramaEnviada)
	req.CSeq().SeqNo = t.seqOriginal + t.delta
	req.Recipient = invitacionURI(t.texto, req.Recipient)
	p.txs[claveTx{t.ramaEnviada, sip.CANCEL}] = &transaccion{ramaOriginal: rama, ramaEnviada: t.ramaEnviada, metodo: sip.CANCEL, delta: t.delta,
		seqOriginal: t.seqOriginal, callID: t.callID, creada: time.Now(), intentos: 9}
	p.mu.Unlock()
	return p.cen.Escribir(ctx, []byte(req.String()))
}

func (p *Puente) respuestaNavegador(ctx context.Context, res *sip.Response) error {
	soloPermitidas(res, cabecerasRespuesta)
	if res.StatusCode < 100 || res.StatusCode > 699 {
		return nil
	}
	// Solo la respuesta a una petición que la Issabel mandó por esta conexión (misma branch y método).
	p.mu.Lock()
	_, conocida := p.pendientesCen[claveTx{res.Via().Params.GetOr("branch", ""), res.CSeq().MethodName}]
	if conocida && res.StatusCode >= 200 && res.CSeq().MethodName != sip.INVITE {
		delete(p.pendientesCen, claveTx{res.Via().Params.GetOr("branch", ""), res.CSeq().MethodName})
	}
	p.mu.Unlock()
	if !conocida {
		return nil
	}
	if !requireValido(res) {
		quitar(res, "Require")
	}
	if esDesvio(res.StatusCode) {
		// Un desvío haría que la Issabel llame a otro número (a la troncal): se cambia por 480.
		res.StatusCode, res.Reason = sip.StatusTemporarilyUnavailable, "Temporarily Unavailable"
		quitar(res, "Contact")
	}
	if !p.contactosValidos(res, false) || filtrarSDP(res, p.o.Relevo) != nil {
		// Un Contact ajeno o audio fuera del relevo: la Issabel recibe 488 en su lugar.
		res.StatusCode, res.Reason = sip.StatusNotAcceptableHere, "Not Acceptable Here"
		quitar(res, "Contact", "m", "Content-Type", "c")
		res.SetBody(nil)
	}
	callID := string(*res.CallID())
	p.mu.Lock()
	d := p.dialogos[callID]
	if d != nil && !d.saliente && res.CSeq().MethodName == sip.INVITE {
		switch {
		case res.IsSuccess():
			d.confirmado, d.resp2xxNav = true, res.String()
			d.tagLocal, _ = res.To().Params.Get("tag")
		case res.StatusCode >= 300:
			d.terminado = true
		case res.StatusCode > 100 && d.tagLocal == "":
			// La etiqueta del navegador sale en su primera provisional con To (o en la 2xx).
			d.tagLocal, _ = res.To().Params.Get("tag")
		}
	}
	if d != nil && res.CSeq().MethodName == sip.BYE && res.IsSuccess() {
		d.terminado = true
	}
	p.mu.Unlock()
	return p.cen.Escribir(ctx, []byte(res.String()))
}

// De la Issabel -----------------------------------------------------------------------------------------

func (p *Puente) deCentralita(ctx context.Context, b []byte) error {
	if esLatido(b) {
		return nil
	}
	msg, err := sip.ParseMessage(b)
	if err != nil || !completo(msg) {
		p.o.Log.DebugContext(ctx, "telefono: mensaje de la centralita no válido")
		return nil
	}
	switch m := msg.(type) {
	case *sip.Response:
		return p.respuestaCentralita(ctx, m)
	case *sip.Request:
		return p.peticionCentralita(ctx, m)
	}
	return nil
}

func (p *Puente) respuestaCentralita(ctx context.Context, res *sip.Response) error {
	rama := res.Via().Params.GetOr("branch", "")
	clave := claveTx{rama, res.CSeq().MethodName}
	p.mu.Lock()
	t := p.txs[clave]
	if t == nil {
		p.mu.Unlock()
		return nil
	}
	desafio := res.StatusCode == sip.StatusUnauthorized || res.StatusCode == sip.StatusProxyAuthRequired
	sinDesafioUtil := false
	if desafio {
		dz, ok := elegirDesafio(res)
		switch {
		case !ok:
			// Ningún desafío que Iris sepa contestar (p. ej. solo SHA-512 o auth-int): no es la
			// credencial de la extensión, es la configuración de la Issabel.
			sinDesafioUtil = true
		case t.intentos < 2 && (t.intentos == 0 || dz.Stale):
			p.mu.Unlock()
			return p.reintentar(ctx, t, res, dz, res.StatusCode == sip.StatusProxyAuthRequired)
		}
	}
	credenciales := false
	if desafio {
		// Credenciales rechazadas: el navegador recibe 403, nunca el desafío, y el puente corta (si no,
		// el navegador reintentaría y el fail2ban de la Issabel bloquearía la IP de Iris para todos).
		credenciales = !sinDesafioUtil
		res.StatusCode, res.Reason = sip.StatusForbidden, "Forbidden"
		if t.metodo == sip.INVITE {
			if ack := ackNo2xx(t, res); ack != "" {
				t.acusada = true
				p.mu.Unlock()
				if err := p.cen.Escribir(ctx, []byte(ack)); err != nil {
					return err
				}
				p.mu.Lock()
			}
		}
	}
	quitar(res, cabecerasAutenticacion...)
	soloPermitidas(res, cabecerasHaciaNavegador)
	res.CSeq().SeqNo -= t.delta
	res.Via().Params.Add("branch", t.ramaOriginal)
	final := res.StatusCode >= 200
	if final {
		t.final = true
	}
	if t.otro && final && t.metodo == sip.OPTIONS {
		delete(p.otros, t.callID) // un OPTIONS no deja estado
	}
	d := p.dialogoDe(t)
	if t.otro {
		if d != nil {
			d.actividad = time.Now()
		}
		d = nil // un REGISTER u OPTIONS no es una llamada
	}
	var hecho *HechoLlamada
	if d != nil {
		d.actividad = time.Now()
		switch {
		case t.metodo == sip.INVITE && res.IsSuccess() && d.saliente:
			d.confirmado = true
			d.resp2xx = res.String()
			d.tagRemoto, _ = res.To().Params.Get("tag")
			d.tagLocal, _ = res.From().Params.Get("tag")
			if d.llamada != nil && !d.avisado[HechoContestada] {
				d.avisado[HechoContestada] = true
				hecho = &HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoContestada}
			}
		case t.metodo == sip.INVITE && res.StatusCode >= 300 && d.saliente && !d.confirmado:
			d.terminado = true
			if d.llamada != nil && !d.avisado[HechoFallo] {
				d.avisado[HechoFallo] = true
				hecho = &HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoFallo, Codigo: res.StatusCode, Cancelada: d.cancelado}
			}
		case t.metodo == sip.BYE && final:
			if d.llamada != nil && d.confirmado && !d.avisado[HechoColgada] {
				d.avisado[HechoColgada] = true
				hecho = &HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoColgada}
			}
			d.terminado = true
		case t.metodo == sip.REFER && res.IsSuccess() && d.llamada != nil && t.destino != "":
			hecho = &HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoTransferida, Destino: t.destino}
			t.destino = "" // una vez por REFER
		}
	}
	p.mu.Unlock()
	if hecho != nil {
		p.avisar(*hecho)
	}
	err := p.nav.Escribir(ctx, []byte(res.String()))
	if credenciales {
		go func() {
			defer func() { _ = recover() }()
			hctx, c := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer c()
			p.o.Politica.Credenciales(hctx)
		}()
		p.Cortar(CierreSinAcceso, "credenciales")
	}
	if desafio && sinDesafioUtil {
		p.o.Log.WarnContext(ctx, "telefono: la centralita pidió una autenticación que Iris no sabe contestar")
		p.Cortar(CierreSinAcceso, "desafio_no_soportado")
	}
	return err
}

// elegirDesafio revisa todos los desafíos de la respuesta (puede haber uno por algoritmo) y se queda
// con MD5 si está; si no, con el primero que Iris sepa contestar.
func elegirDesafio(res *sip.Response) (Desafio, bool) {
	cab := "www-authenticate"
	if res.StatusCode == sip.StatusProxyAuthRequired {
		cab = "proxy-authenticate"
	}
	var elegido Desafio
	hay := false
	for _, h := range res.Headers() {
		if strings.ToLower(h.Name()) != cab {
			continue
		}
		dz, err := LeerDesafio(h.Value())
		if err != nil {
			continue
		}
		if dz.Algoritmo == "MD5" {
			return dz, true
		}
		if !hay {
			elegido, hay = dz, true
		}
	}
	return elegido, hay
}

// reintentar manda otra vez la petición con la credencial (CSeq + 1, otra branch). Con un INVITE,
// Iris acusa el 401 con su ACK (el navegador nunca lo vio).
func (p *Puente) reintentar(ctx context.Context, t *transaccion, res *sip.Response, dz Desafio, proxy bool) error {
	m, err := sip.ParseMessage([]byte(t.texto))
	req, ok := m.(*sip.Request)
	if err != nil || !ok {
		return nil
	}
	p.mu.Lock()
	d := p.dialogoDe(t)
	if d == nil {
		p.mu.Unlock()
		return nil
	}
	ack := ""
	if t.metodo == sip.INVITE {
		ack = ackNo2xx(t, res)
		t.acusada = true
	}
	d.offset++
	nueva := t.ramaOriginal + "." + strconv.Itoa(t.intentos+1) + sip.GenerateTagN(6)
	req.Via().Params.Add("branch", nueva)
	req.CSeq().SeqNo = t.seqOriginal + d.offset
	if req.CSeq().SeqNo > d.ultimaCSeq {
		d.ultimaCSeq = req.CSeq().SeqNo
	}
	quitar(req, cabecerasAutenticacion...)
	nombre := "Authorization"
	if proxy {
		nombre = "Proxy-Authorization"
	}
	valor, err := Responder(dz, p.o.Credencial.Usuario, p.o.Credencial.Secreto, string(req.Method), req.Recipient.String(), 1)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	req.AppendHeader(sip.NewHeader(nombre, valor))
	texto := req.String()
	t2 := &transaccion{ramaOriginal: t.ramaOriginal, ramaEnviada: nueva, metodo: t.metodo, delta: d.offset, seqOriginal: t.seqOriginal, texto: texto,
		intentos: t.intentos + 1, callID: t.callID, creada: time.Now(), otro: t.otro}
	p.txs[claveTx{nueva, t.metodo}] = t2
	if t.metodo == sip.INVITE {
		p.invites[t.ramaOriginal] = t2
		d.invite = texto
	}
	p.mu.Unlock()
	if ack != "" {
		if err := p.cen.Escribir(ctx, []byte(ack)); err != nil {
			return err
		}
	}
	return p.cen.Escribir(ctx, []byte(texto))
}

// ackNo2xx arma el ACK de una final no 2xx a un INVITE que salió de Iris (RFC 3261 §17.1.1.3).
func ackNo2xx(t *transaccion, res *sip.Response) string {
	m, err := sip.ParseMessage([]byte(t.texto))
	inv, ok := m.(*sip.Request)
	if err != nil || !ok {
		return ""
	}
	ack := sip.NewRequest(sip.ACK, inv.Recipient)
	ack.AppendHeader(inv.Via().Clone())
	for _, r := range inv.GetHeaders("Route") {
		ack.AppendHeader(sip.NewHeader("Route", r.Value()))
	}
	mf := sip.MaxForwardsHeader(70)
	ack.AppendHeader(&mf)
	ack.AppendHeader(sip.NewHeader("From", inv.From().Value()))
	ack.AppendHeader(sip.NewHeader("To", res.To().Value()))
	cid := *inv.CallID()
	ack.AppendHeader(&cid)
	ack.AppendHeader(&sip.CSeqHeader{SeqNo: inv.CSeq().SeqNo, MethodName: sip.ACK})
	cl := sip.ContentLengthHeader(0)
	ack.AppendHeader(&cl)
	return ack.String()
}

func (p *Puente) peticionCentralita(ctx context.Context, req *sip.Request) error {
	soloPermitidas(req, cabecerasHaciaNavegador)

	callID := string(*req.CallID())
	etiquetaTo, _ := req.To().Params.Get("tag")
	p.mu.Lock()
	d := p.dialogos[callID]
	p.mu.Unlock()
	if req.IsAck() && d == nil {
		return nil // el ACK de una final que dio el puente (480, 486)
	}
	if req.IsInvite() && etiquetaTo == "" {
		if !p.o.Registrar {
			return p.responderCentralita(ctx, req, sip.StatusTemporarilyUnavailable, "Temporarily Unavailable")
		}
		if d == nil && p.dialogosVivos() >= p.o.MaxDialogos {
			return p.responderCentralita(ctx, req, sip.StatusBusyHere, "Busy Here")
		}
		p.mu.Lock()
		if d == nil {
			if d = p.nuevoDialogo(callID); d == nil {
				p.mu.Unlock()
				return p.responderCentralita(ctx, req, sip.StatusBusyHere, "Busy Here")
			}
			d.esLlamada = true
		}
		d.inviteEntrante, d.actividad = req.String(), time.Now()
		d.tagRemoto, _ = req.From().Params.Get("tag")
		// La etiqueta del navegador la fija su primera respuesta con To (provisional o 2xx): hasta
		// entonces no vale ninguna petición suya dentro de este diálogo.
		d.tagLocal = ""
		p.mu.Unlock()
		if !p.anotarDeCentralita(req) {
			return p.responderCentralita(ctx, req, sip.StatusServiceUnavailable, "Service Unavailable")
		}
		if id := p.o.Politica.Entrante(ctx, req.From().Address.User); id != nil {
			req.AppendHeader(sip.NewHeader("X-Iris-Llamada", id.String()))
		}
		return p.nav.Escribir(ctx, []byte(req.String()))
	}
	var hecho *HechoLlamada
	p.mu.Lock()
	if d != nil {
		d.actividad = time.Now()
		switch req.Method {
		case sip.BYE:
			if d.saliente && d.llamada != nil && d.confirmado && !d.avisado[HechoColgada] {
				d.avisado[HechoColgada] = true
				hecho = &HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoColgada}
			}
			d.terminado = true
		case sip.CANCEL:
			d.terminado = true
		}
	}
	p.mu.Unlock()
	if hecho != nil {
		p.avisar(*hecho)
	}
	if !req.IsAck() && !p.anotarDeCentralita(req) {
		return p.responderCentralita(ctx, req, sip.StatusServiceUnavailable, "Service Unavailable")
	}
	return p.nav.Escribir(ctx, []byte(req.String()))
}

// anotarDeCentralita guarda la branch de una petición de la Issabel: solo sus respuestas vuelven.
func (p *Puente) anotarDeCentralita(req *sip.Request) bool {
	rama := req.Via().Params.GetOr("branch", "")
	if rama == "" || len(rama) > 128 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pendientesCen) >= maxDeCentralita {
		p.limpiar()
		if len(p.pendientesCen) >= maxDeCentralita {
			return false
		}
	}
	p.pendientesCen[claveTx{rama, req.Method}] = time.Now()
	return true
}

func (p *Puente) responderCentralita(ctx context.Context, req *sip.Request, codigo int, razon string) error {
	return p.cen.Escribir(ctx, []byte(sip.NewResponseFromRequest(req, codigo, razon, nil).String()))
}

func (p *Puente) avisar(h HechoLlamada) {
	defer func() { _ = recover() }() // el canal ya cerrado al terminar: el hecho se pierde
	select {
	case p.hechos <- h:
	default:
		p.o.Log.Warn("telefono: cola de hechos llena", "llamada", h.LlamadaID, "tipo", h.Tipo)
	}
}

func acotarExpira(req *sip.Request) {
	if h := req.GetHeader("Expires"); h != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Value())); err != nil || n > maxExpiraciones || n < 0 {
			quitar(req, "Expires")
			req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(maxExpiraciones)))
		}
	}
	for _, c := range req.GetHeaders("Contact") {
		if ch, ok := c.(*sip.ContactHeader); ok {
			if v, ok := ch.Params.Get("expires"); ok {
				if n, err := strconv.Atoi(v); err != nil || n > maxExpiraciones || n < 0 {
					ch.Params.Add("expires", strconv.Itoa(maxExpiraciones))
				}
			}
		}
	}
}

func soloDigitos(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// limpiar borra transacciones viejas y diálogos terminados (con p.mu tomado).
func (p *Puente) limpiar() {
	ahora := time.Now()
	for k, t := range p.txs {
		if ahora.Sub(t.creada) > vidaTransaccion {
			delete(p.txs, k)
		}
	}
	for k, t := range p.invites {
		if ahora.Sub(t.creada) > vidaTransaccion {
			delete(p.invites, k)
			delete(p.locales, k)
		}
	}
	if len(p.locales) > 256 {
		p.locales = map[string]bool{}
	}
	for k, c := range p.pendientesCen {
		if ahora.Sub(c) > vidaTransaccion {
			delete(p.pendientesCen, k)
		}
	}
	for k, d := range p.otros {
		if ahora.Sub(d.actividad) > vidaOtro {
			delete(p.otros, k)
		}
	}
	for k, d := range p.dialogos {
		if (d.terminado || !d.confirmado) && ahora.Sub(d.actividad) > 5*time.Minute {
			delete(p.dialogos, k)
		}
	}
}

// Colgar al cortar --------------------------------------------------------------------------------------

// colgarTodo manda BYE a cada diálogo establecido y CANCEL a cada INVITE saliente sin final.
func (p *Puente) colgarTodo(ctx context.Context) {
	p.mu.Lock()
	if p.cortado {
		p.mu.Unlock()
		return
	}
	p.cortado = true
	var mensajes []string
	var hechos []HechoLlamada
	for callID, d := range p.dialogos {
		if d.terminado || !d.esLlamada {
			continue
		}
		switch {
		case d.confirmado && d.saliente:
			if bye := byeSaliente(d); bye != "" {
				mensajes = append(mensajes, bye)
			}
			if d.llamada != nil && !d.avisado[HechoColgada] {
				d.avisado[HechoColgada] = true
				hechos = append(hechos, HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoColgada})
			}
		case d.confirmado:
			if bye := byeEntrante(d); bye != "" {
				mensajes = append(mensajes, bye)
			}
		case d.saliente:
			for _, t := range p.invites {
				if t.callID == callID && !t.final {
					if c := cancelDe(t); c != "" {
						mensajes = append(mensajes, c)
					}
				}
			}
			if d.llamada != nil && !d.avisado[HechoFallo] {
				d.avisado[HechoFallo] = true
				hechos = append(hechos, HechoLlamada{LlamadaID: *d.llamada, Tipo: HechoFallo, Codigo: sip.StatusRequestTerminated, Cancelada: true})
			}
		}
		d.terminado = true
	}
	p.mu.Unlock()
	for _, h := range hechos {
		p.avisar(h)
	}
	wctx, cancelar := context.WithTimeout(ctx, 2*time.Second)
	defer cancelar()
	for _, m := range mensajes {
		if err := p.cen.Escribir(wctx, []byte(m)); err != nil {
			return
		}
	}
}

func viaNueva(de *sip.ViaHeader) *sip.ViaHeader {
	host := "iris.invalid"
	if de != nil && de.Host != "" {
		host = de.Host
	}
	v := &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "WSS", Host: host, Params: sip.HeaderParams{}}
	v.Params.Add("branch", sip.GenerateBranch())
	return v
}

func byeSaliente(d *dialogo) string {
	mi, err1 := sip.ParseMessage([]byte(d.invite))
	mr, err2 := sip.ParseMessage([]byte(d.resp2xx))
	inv, ok1 := mi.(*sip.Request)
	res, ok2 := mr.(*sip.Response)
	if err1 != nil || err2 != nil || !ok1 || !ok2 {
		return ""
	}
	destino := inv.Recipient
	if c := res.Contact(); c != nil {
		destino = c.Address
	}
	d.ultimaCSeq++
	bye := sip.NewRequest(sip.BYE, destino)
	bye.AppendHeader(viaNueva(inv.Via()))
	mf := sip.MaxForwardsHeader(70)
	bye.AppendHeader(&mf)
	rr := res.GetHeaders("Record-Route")
	for i := len(rr) - 1; i >= 0; i-- {
		bye.AppendHeader(sip.NewHeader("Route", rr[i].Value()))
	}
	bye.AppendHeader(sip.NewHeader("From", inv.From().Value()))
	bye.AppendHeader(sip.NewHeader("To", res.To().Value()))
	cid := *inv.CallID()
	bye.AppendHeader(&cid)
	bye.AppendHeader(&sip.CSeqHeader{SeqNo: d.ultimaCSeq, MethodName: sip.BYE})
	cl := sip.ContentLengthHeader(0)
	bye.AppendHeader(&cl)
	return bye.String()
}

func byeEntrante(d *dialogo) string {
	mi, err1 := sip.ParseMessage([]byte(d.inviteEntrante))
	mr, err2 := sip.ParseMessage([]byte(d.resp2xxNav))
	inv, ok1 := mi.(*sip.Request)
	res, ok2 := mr.(*sip.Response)
	if err1 != nil || err2 != nil || !ok1 || !ok2 || inv.Contact() == nil {
		return ""
	}
	if d.ultimaCSeq < 1 {
		d.ultimaCSeq = 1
	}
	d.ultimaCSeq++
	bye := sip.NewRequest(sip.BYE, inv.Contact().Address)
	bye.AppendHeader(viaNueva(nil))
	mf := sip.MaxForwardsHeader(70)
	bye.AppendHeader(&mf)
	for _, r := range inv.GetHeaders("Record-Route") {
		bye.AppendHeader(sip.NewHeader("Route", r.Value()))
	}
	bye.AppendHeader(sip.NewHeader("From", res.To().Value()))
	bye.AppendHeader(sip.NewHeader("To", inv.From().Value()))
	cid := *inv.CallID()
	bye.AppendHeader(&cid)
	bye.AppendHeader(&sip.CSeqHeader{SeqNo: d.ultimaCSeq, MethodName: sip.BYE})
	cl := sip.ContentLengthHeader(0)
	bye.AppendHeader(&cl)
	return bye.String()
}

func cancelDe(t *transaccion) string {
	m, err := sip.ParseMessage([]byte(t.texto))
	inv, ok := m.(*sip.Request)
	if err != nil || !ok {
		return ""
	}
	c := sip.NewRequest(sip.CANCEL, inv.Recipient)
	c.AppendHeader(inv.Via().Clone())
	mf := sip.MaxForwardsHeader(70)
	c.AppendHeader(&mf)
	c.AppendHeader(sip.NewHeader("From", inv.From().Value()))
	c.AppendHeader(sip.NewHeader("To", inv.To().Value()))
	cid := *inv.CallID()
	c.AppendHeader(&cid)
	c.AppendHeader(&sip.CSeqHeader{SeqNo: inv.CSeq().SeqNo, MethodName: sip.CANCEL})
	cl := sip.ContentLengthHeader(0)
	c.AppendHeader(&cl)
	return c.String()
}
