package mensajeria

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Conectar un número con QR (coexistencia) y traer sus contactos e historial
// (contratos/conversaciones.md §15.2). Las llamadas a Meta van fuera de las transacciones: un caso
// valida y reserva (el SHA-256 del código queda registrado: se canjea una sola vez), se habla con
// Meta y otro caso guarda el número. El código y el token nunca se auditan ni se registran.

// Tipos de conexión de un número.
const (
	ConexionAPI = "api"
	ConexionQR  = "qr"
)

// plazoSincronizacion: Meta entrega contactos e historial solo si se piden en las 24 h después de
// conectar.
const plazoSincronizacion = 24 * time.Hour

// pedidoSinRespuesta: un pedido a Meta que no terminó en este tiempo se puede repetir.
const pedidoSinRespuesta = 10 * time.Minute

// Estados de la sincronización (contactos y mensajes).
const (
	SyncSinPedir   = "sin_pedir"
	SyncPedido     = "pedido"
	SyncRecibiendo = "recibiendo"
	SyncListo      = "listo"
	SyncRechazado  = "rechazado"
	SyncFallido    = "fallido"
)

// parteSincronizacion es el estado de los contactos o de los mensajes (JSON en la base).
type parteSincronizacion struct {
	Estado    string     `json:"estado"`
	PedidoEn  *time.Time `json:"pedido_en,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	Total     int        `json:"total,omitempty"`
	Asociados int        `json:"asociados,omitempty"`
	Progreso  int        `json:"progreso,omitempty"`
	Fase      int        `json:"fase,omitempty"`
	Mensajes  int        `json:"mensajes,omitempty"`
	Detalle   *string    `json:"detalle,omitempty"`
}

type sincronizacionBD struct {
	Plazo     time.Time           `json:"plazo"`
	Contactos parteSincronizacion `json:"contactos"`
	Mensajes  parteSincronizacion `json:"mensajes"`
}

func sincronizacionDe(f mensajeriabd.MensajeriaCanal) (sincronizacionBD, bool) {
	var s sincronizacionBD
	if f.TipoConexion != ConexionQR || json.Unmarshal(f.Sincronizacion, &s) != nil || s.Plazo.IsZero() {
		return s, false
	}
	if s.Contactos.Estado == "" {
		s.Contactos.Estado = SyncSinPedir
	}
	if s.Mensajes.Estado == "" {
		s.Mensajes.Estado = SyncSinPedir
	}
	return s, true
}

func sincronizacionNueva(conectado time.Time) []byte {
	crudo, _ := json.Marshal(sincronizacionBD{Plazo: conectado.Add(plazoSincronizacion),
		Contactos: parteSincronizacion{Estado: SyncSinPedir}, Mensajes: parteSincronizacion{Estado: SyncSinPedir}})
	return crudo
}

// ParteSincronizacion es el estado de los contactos o de los mensajes para el panel.
type ParteSincronizacion struct {
	Estado    string     `json:"estado" enum:"sin_pedir,pedido,recibiendo,listo,rechazado,fallido"`
	PedidoEn  *time.Time `json:"pedidoEn" nullable:"true"`
	Total     int        `json:"total" doc:"Contactos recibidos"`
	Asociados int        `json:"asociados" doc:"Contactos que coinciden con un cliente de Argos"`
	Progreso  int        `json:"progreso" doc:"Historial: 0–100 (progress de Meta)"`
	Fase      int        `json:"fase" doc:"Historial: fase de Meta (0: día 0–1, 1: días 1–90, 2: días 90–180)"`
	Mensajes  int        `json:"mensajes" doc:"Historial: mensajes guardados"`
	Detalle   *string    `json:"detalle" nullable:"true"`
}

// SincronizacionCanal es el estado de «Traer historial» de un número QR.
type SincronizacionCanal struct {
	Plazo     time.Time           `json:"plazo" doc:"Hasta cuándo se puede pedir (24 h después de conectar)"`
	Contactos ParteSincronizacion `json:"contactos"`
	Mensajes  ParteSincronizacion `json:"mensajes"`
}

func aParte(p parteSincronizacion) ParteSincronizacion {
	return ParteSincronizacion{Estado: p.Estado, PedidoEn: p.PedidoEn, Total: p.Total, Asociados: p.Asociados, Progreso: p.Progreso,
		Fase: p.Fase, Mensajes: p.Mensajes, Detalle: p.Detalle}
}

// Conectar con QR ------------------------------------------------------------------------------------

// EntradaCanalQR es POST /canales/qr.
type EntradaCanalQR struct {
	Code          string   `json:"code" minLength:"1" maxLength:"2048" doc:"El código del callback de FB.login (vive ~30 s). Nunca se guarda"`
	WabaID        string   `json:"wabaId" pattern:"^[0-9]{1,30}$"`
	PhoneNumberID string   `json:"phoneNumberId" pattern:"^[0-9]{1,30}$"`
	Nombre        string   `json:"nombre" minLength:"1" maxLength:"120"`
	Usos          []string `json:"usos,omitempty" maxItems:"4" uniqueItems:"true" doc:"Por defecto soporte"`
	ColaID        *int64   `json:"colaId,omitempty" minimum:"1" nullable:"true"`
	EmpresaID     *int64   `json:"empresaId,omitempty" minimum:"1" nullable:"true" doc:"Por defecto la empresa activa"`
}

// MarshalJSON: lo auditado nunca lleva el código.
func (e EntradaCanalQR) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"waba_id": e.WabaID, "phone_number_id": e.PhoneNumberID, "nombre": e.Nombre, "usos": e.Usos,
		"cola_id": e.ColaID, "empresa_id": e.EmpresaID})
}

// CanalQRConectado es el número y si se creó (201) o se reconectó (200).
type CanalQRConectado struct {
	Canal  Canal
	Creado bool
}

// CrearCanalQR es el caso completo (preparar, Meta, guardar).
type CrearCanalQR struct {
	m        *Modulo
	preparar *auditoria.Caso[EntradaCanalQR, qrPreparado]
	guardar  *auditoria.Caso[qrCanjeado, CanalQRConectado]
	fallar   *auditoria.Caso[qrFallido, struct{}]
}

type qrPreparado struct {
	id          int64
	reconectar  bool
	empresa     *int64
	usos        []string
	conexion    conexionEfectiva
	verifyToken string
	alterno     bool
}

type qrCanjeado struct {
	in     EntradaCanalQR
	prep   qrPreparado
	token  string
	numero NumeroRegistrado
}

// MarshalJSON: lo auditado (sin el código ni el token).
func (e qrCanjeado) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"entrada": e.in, "canal_id": e.prep.id, "reconectar": e.prep.reconectar, "numero": e.numero.Numero,
		"webhook_alterno": e.prep.alterno})
}

type qrFallido struct {
	In     EntradaCanalQR `json:"entrada"`
	Codigo string         `json:"codigo"`
	Paso   string         `json:"paso"`
}

func problemaRegistro(e *ErrorRegistro) *nucleo.Problema {
	if e.Codigo == ErrQRErrorMeta {
		return nucleo.NuevoProblema(http.StatusBadGateway, e.Codigo, "Meta respondió con un error", e.Detalle)
	}
	return reglaConv(e.Codigo, e.Detalle)
}

func servicioCentralNoDisponible() *nucleo.Problema {
	return reglaConv(ErrQRServicioNoDisponible, detalleServicioCentral)
}

func nuevoCrearCanalQR(m *Modulo) *CrearCanalQR {
	k := &CrearCanalQR{m: m}
	k.preparar = auditoria.Nuevo(auditoria.Caso[EntradaCanalQR, qrPreparado]{
		Accion: "mensajeria.conectar_qr", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		AuditarExito: func(qrPreparado) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCanalQR) (qrPreparado, error) {
			if err := soloPersonas(ctx); err != nil {
				return qrPreparado{}, err
			}
			if !reDigitos.MatchString(in.WabaID) || !reDigitos.MatchString(in.PhoneNumberID) {
				return qrPreparado{}, campoConv("phoneNumberId", "wabaId y phoneNumberId son solo dígitos", "pattern")
			}
			if m.cfg.Cifrador == nil {
				return qrPreparado{}, errSinCifrado()
			}
			q := mensajeriabd.New(t)
			c, err := m.conexion(ctx, q)
			if err != nil {
				return qrPreparado{}, err
			}
			switch {
			case c.modo == ModoMetaCompartida:
				return qrPreparado{}, servicioCentralNoDisponible()
			case !c.completa():
				return qrPreparado{}, sinConexionMeta()
			}
			empresa := in.EmpresaID
			if empresa == nil && contexto.EmpresasDe(ctx).Estado == contexto.EmpresaActiva {
				e := contexto.EmpresaDe(ctx)
				empresa = &e
			}
			if err := exigirEmpresaCanal(ctx, t, empresa); err != nil {
				return qrPreparado{}, err
			}
			e := int64(0)
			if empresa != nil {
				e = *empresa
			}
			p := qrPreparado{empresa: empresa, conexion: c, usos: in.Usos}
			if len(p.usos) == 0 {
				p.usos = []string{"soporte"}
			}
			if err := validarUsos(p.usos, false); err != nil {
				return qrPreparado{}, err
			}
			if err := m.colaDeEmpresa(ctx, q, e, in.ColaID); err != nil {
				return qrPreparado{}, err
			}
			previo, err := q.CanalQRDeEmpresa(ctx, mensajeriabd.CanalQRDeEmpresaParams{Empresa: e, PhoneNumberID: in.PhoneNumberID})
			switch {
			case err == nil:
				p.id, p.reconectar = previo.ID, true
			case !errors.Is(err, pgx.ErrNoRows):
				return qrPreparado{}, err
			}
			if activo, err := q.CanalActivoPorPhoneNumber(ctx, in.PhoneNumberID); err == nil && activo.ID != p.id {
				return qrPreparado{}, nucleo.Conflicto("canal_existente", "Ese phone number ID ya está en otro número activo")
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return qrPreparado{}, err
			}
			if !p.reconectar {
				if p.id, err = q.SiguienteIDCanal(ctx); err != nil {
					return qrPreparado{}, err
				}
			}
			if p.verifyToken, err = generarVerifyToken(); err != nil {
				return qrPreparado{}, err
			}
			// Con varios números QR, cada uno con su URL (override_callback_uri); con uno solo
			// alcanza el webhook de la app. En saas y compartida la app no es de esta instalación:
			// siempre con la URL del número, que exige IRIS_BASE_URL absoluta.
			otro, err := q.HayOtroCanalQRActivo(ctx, p.id)
			if err != nil {
				return qrPreparado{}, err
			}
			_, completa := m.webhookURL(p.id)
			siempre := c.modo == ModoMetaSaaS || c.modo == ModoMetaCompartida
			if siempre && !completa {
				return qrPreparado{}, reglaConv("base_url_requerida",
					"Falta IRIS_BASE_URL (la URL pública de Iris, absoluta): Meta debe mandar los mensajes de este número a la URL propia del número")
			}
			p.alterno = completa && (otro || siempre)
			h := sha256.Sum256([]byte(in.Code))
			n, err := q.InsertarRegistroQR(ctx, mensajeriabd.InsertarRegistroQRParams{HashCodigo: h[:], EmpresaID: empresa, UsuarioID: usuarioDe(ctx)})
			if err != nil {
				return qrPreparado{}, err
			}
			if n == 0 {
				return qrPreparado{}, reglaConv(ErrQRCodigoVencido, "Ese código de Meta ya se usó: vuelve a abrir la ventana de Meta")
			}
			return p, nil
		},
	})
	k.guardar = auditoria.Nuevo(auditoria.Caso[qrCanjeado, CanalQRConectado]{
		Accion: "mensajeria.conectar_qr", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in qrCanjeado) (CanalQRConectado, error) {
			q := mensajeriabd.New(t)
			p := in.prep
			fijarEmpresaCanal(t, p.empresa)
			cifradas, err := m.cifrarCredenciales(p.id, credenciales{Token: in.token, VerifyToken: p.verifyToken})
			if err != nil {
				return CanalQRConectado{}, err
			}
			ahora := m.ahora()
			sync := sincronizacionNueva(ahora)
			if p.reconectar {
				antes, err := m.canalParaActualizar(ctx, t, p.id)
				if err != nil {
					return CanalQRConectado{}, err
				}
				f, err := q.ReconectarCanalQR(ctx, mensajeriabd.ReconectarCanalQRParams{ID: p.id, Numero: in.numero.Numero, WabaID: in.in.WabaID,
					CredencialesCifradas: cifradas, ConectadoEn: &ahora, Sincronizacion: sync})
				if err != nil {
					return CanalQRConectado{}, conflictoCanal(err)
				}
				c, err := m.canalVisto(ctx, q, f, p.empresa)
				if err != nil {
					return CanalQRConectado{}, err
				}
				t.RegistrarCambios("canal_mensajeria", f.ID, auditableCanal(m.aCanal(antes), "sin_cambio"), auditableCanal(c, "***"))
				return CanalQRConectado{Canal: c}, t.Emitir("canal_mensajeria.reconectado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "origen": "qr"})
			}
			e := int64(0)
			if p.empresa != nil {
				e = *p.empresa
			}
			avisos := false
			if slices.Contains(p.usos, "avisos") {
				hay, err := q.HayCanalAvisos(ctx, mensajeriabd.HayCanalAvisosParams{EmpresaID: e, Excepto: 0})
				if err != nil {
					return CanalQRConectado{}, err
				}
				avisos = !hay
			}
			f, err := q.InsertarCanalQR(ctx, mensajeriabd.InsertarCanalQRParams{ID: p.id, EmpresaID: p.empresa, Nombre: in.in.Nombre, Numero: in.numero.Numero,
				PhoneNumberID: in.in.PhoneNumberID, WabaID: in.in.WabaID, CredencialesCifradas: cifradas, CreadoPor: usuarioDe(ctx), Usos: p.usos,
				AvisosEmpresa: avisos, ColaID: in.in.ColaID, ConectadoEn: &ahora, Sincronizacion: sync})
			if err != nil {
				return CanalQRConectado{}, conflictoCanal(err)
			}
			c, err := m.canalVisto(ctx, q, f, p.empresa)
			if err != nil {
				return CanalQRConectado{}, err
			}
			t.RegistrarCambios("canal_mensajeria", f.ID, nil, auditableCanal(c, "***"))
			return CanalQRConectado{Canal: c, Creado: true}, t.Emitir("canal_mensajeria.creado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID,
				"tipo": f.Tipo, "canal": f.Canal, "tipo_conexion": ConexionQR})
		},
	})
	k.fallar = auditoria.Nuevo(auditoria.Caso[qrFallido, struct{}]{
		Accion: "mensajeria.conectar_qr_fallido", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(_ context.Context, t *auditoria.Tx, in qrFallido) (struct{}, error) {
			t.Anotar(map[string]any{"codigo": in.Codigo, "paso": in.Paso})
			return struct{}{}, nil
		},
	})
	return k
}

// Ejecutar conecta el número: reserva el código, lo canjea, lee el número, suscribe la app a la
// WABA (nunca /register) y lo guarda.
func (k *CrearCanalQR) Ejecutar(ctx context.Context, in EntradaCanalQR) (CanalQRConectado, error) {
	p, err := k.preparar.Ejecutar(ctx, in)
	if err != nil {
		return CanalQRConectado{}, err
	}
	reg := k.m.registroDe(p.conexion)
	falla := func(paso string, err error) (CanalQRConectado, error) {
		var e *ErrorRegistro
		if !errors.As(err, &e) {
			return CanalQRConectado{}, err
		}
		if _, ferr := k.fallar.Ejecutar(ctx, qrFallido{In: in, Codigo: e.Codigo, Paso: paso}); ferr != nil {
			return CanalQRConectado{}, ferr
		}
		return CanalQRConectado{}, problemaRegistro(e)
	}
	token, err := reg.CanjearCodigo(ctx, in.Code)
	if err != nil {
		return falla("canje", err)
	}
	numero, err := reg.ConsultarNumero(ctx, token, in.WabaID, in.PhoneNumberID)
	if err != nil {
		return falla("numero", err)
	}
	var alterno *WebhookAlterno
	if p.alterno {
		u, _ := k.m.webhookURL(p.id)
		alterno = &WebhookAlterno{URL: u, VerifyToken: p.verifyToken}
	}
	if err := reg.SuscribirWABA(ctx, token, in.WabaID, alterno); err != nil {
		return falla("suscripcion", err)
	}
	return k.guardar.Ejecutar(ctx, qrCanjeado{in: in, prep: p, token: token, numero: numero})
}

// Sincronizar contactos e historial ------------------------------------------------------------------

// EntradaSincronizar es POST /canales/{id}/sincronizar.
type EntradaSincronizar struct {
	ID        int64 `json:"canal_id"`
	Contactos bool  `json:"contactos,omitempty"`
	Mensajes  bool  `json:"mensajes,omitempty"`
}

// Sincronizar pide a Meta los contactos y el historial de un número QR (una vez, dentro de 24 h).
type Sincronizar struct {
	m        *Modulo
	reservar *auditoria.Caso[EntradaSincronizar, syncReservada]
	guardar  *auditoria.Caso[syncPedida, Canal]
}

type syncReservada struct {
	canal    int64
	pnid     string
	token    string
	tipos    []string
	conexion conexionEfectiva
}

type syncPedida struct {
	CanalID   int64             `json:"canal_id"`
	Resultado map[string]string `json:"resultado"`
	requestID map[string]string
	detalle   map[string]string
}

func nuevoSincronizar(m *Modulo) *Sincronizar {
	s := &Sincronizar{m: m}
	s.reservar = auditoria.Nuevo(auditoria.Caso[EntradaSincronizar, syncReservada]{
		Accion: "mensajeria.sincronizar_canal", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		AuditarExito: func(syncReservada) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSincronizar) (syncReservada, error) {
			if err := soloPersonas(ctx); err != nil {
				return syncReservada{}, err
			}
			if !in.Contactos && !in.Mensajes {
				return syncReservada{}, campoConv("contactos", "Pide contactos, mensajes o los dos", "required")
			}
			f, err := m.canalParaActualizar(ctx, t, in.ID)
			if err != nil {
				return syncReservada{}, err
			}
			sync, ok := sincronizacionDe(f)
			if !ok {
				return syncReservada{}, reglaConv("no_es_coexistencia", "Solo los números conectados con QR traen contactos e historial")
			}
			ahora := m.ahora()
			if !ahora.Before(sync.Plazo) {
				return syncReservada{}, reglaConv("sincronizacion_vencida", "Meta solo entrega contactos e historial si se piden en las 24 horas después de conectar")
			}
			out := syncReservada{canal: f.ID, pnid: f.PhoneNumberID}
			for _, x := range []struct {
				pedir bool
				tipo  string
				parte *parteSincronizacion
			}{{in.Contactos, SyncContactos, &sync.Contactos}, {in.Mensajes, SyncHistorial, &sync.Mensajes}} {
				if !x.pedir {
					continue
				}
				// Un «pedido» de más de 10 min sin respuesta de Meta (la llamada no terminó) se
				// puede pedir otra vez.
				trabado := x.parte.Estado == SyncPedido && x.parte.RequestID == "" && x.parte.PedidoEn != nil &&
					ahora.Sub(*x.parte.PedidoEn) > pedidoSinRespuesta
				if x.parte.Estado != SyncSinPedir && x.parte.Estado != SyncFallido && !trabado {
					return syncReservada{}, reglaConv("sincronizacion_ya_pedida", "Ya se pidió a Meta y solo se puede pedir una vez")
				}
				*x.parte = parteSincronizacion{Estado: SyncPedido, PedidoEn: &ahora}
				out.tipos = append(out.tipos, x.tipo)
			}
			if out.conexion, err = m.conexion(ctx, mensajeriabd.New(t)); err != nil {
				return syncReservada{}, err
			}
			if out.conexion.modo == ModoMetaCompartida {
				return syncReservada{}, servicioCentralNoDisponible()
			}
			cr, err := m.credencialesDe(f)
			if err != nil {
				return syncReservada{}, err
			}
			out.token = cr.Token
			crudo, _ := json.Marshal(sync)
			if _, err := mensajeriabd.New(t).GuardarSincronizacion(ctx, mensajeriabd.GuardarSincronizacionParams{ID: f.ID, Sincronizacion: crudo}); err != nil {
				return syncReservada{}, err
			}
			return out, nil
		},
	})
	s.guardar = auditoria.Nuevo(auditoria.Caso[syncPedida, Canal]{
		Accion: "mensajeria.sincronizar_canal", Permiso: nucleo.PermMensajeriaConfigurar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in syncPedida) (Canal, error) {
			q := mensajeriabd.New(t)
			f, err := q.CanalParaSincronizar(ctx, in.CanalID)
			if err != nil {
				return Canal{}, err
			}
			fijarEmpresaCanal(t, f.EmpresaID)
			sync, _ := sincronizacionDe(f)
			for tipo, res := range in.Resultado {
				parte := &sync.Contactos
				if tipo == SyncHistorial {
					parte = &sync.Mensajes
				}
				if res == SyncFallido {
					d := in.detalle[tipo]
					parte.Estado, parte.Detalle = SyncFallido, &d
					continue
				}
				parte.RequestID = in.requestID[tipo]
			}
			crudo, _ := json.Marshal(sync)
			if f, err = q.GuardarSincronizacion(ctx, mensajeriabd.GuardarSincronizacionParams{ID: f.ID, Sincronizacion: crudo}); err != nil {
				return Canal{}, err
			}
			t.FijarEntidad("canal_mensajeria", f.ID)
			return m.aCanal(f), t.Emitir("canal_mensajeria.actualizado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "campos": []string{"sincronizacion"}})
		},
	})
	return s
}

// Ejecutar reserva, pide a Meta cada tipo y guarda. Si todo falló, 502 error_meta (el estado
// fallido queda guardado: se puede pedir otra vez dentro del plazo).
func (s *Sincronizar) Ejecutar(ctx context.Context, in EntradaSincronizar) (Canal, error) {
	r, err := s.reservar.Ejecutar(ctx, in)
	if err != nil {
		return Canal{}, err
	}
	reg := s.m.registroDe(r.conexion)
	p := syncPedida{CanalID: r.canal, Resultado: map[string]string{}, requestID: map[string]string{}, detalle: map[string]string{}}
	var ultimo *ErrorRegistro
	for _, tipo := range r.tipos {
		id, err := reg.PedirSincronizacion(ctx, r.token, r.pnid, tipo)
		if err != nil {
			var e *ErrorRegistro
			if !errors.As(err, &e) {
				e = &ErrorRegistro{Codigo: ErrQRErrorMeta, Detalle: "No se pudo pedir a Meta"}
			}
			p.Resultado[tipo], p.detalle[tipo], ultimo = SyncFallido, e.Detalle, e
			continue
		}
		p.Resultado[tipo], p.requestID[tipo] = SyncPedido, id
	}
	c, err := s.guardar.Ejecutar(ctx, p)
	if err != nil {
		return Canal{}, err
	}
	if ultimo != nil && !slices.Contains(mapaValores(p.Resultado), SyncPedido) {
		return Canal{}, problemaRegistro(ultimo)
	}
	return c, nil
}

func mapaValores(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// purgarRegistrosQR borra las huellas de los códigos de Embedded Signup de más de 7 días (el
// código vive ~30 s; la huella solo evita canjearlo dos veces).
func (m *Modulo) purgarRegistrosQR(ctx context.Context, bd nucleo.BD) error {
	_, err := mensajeriabd.New(bd).PurgarRegistrosQR(ctx, m.ahora().Add(-7*24*time.Hour))
	return err
}
