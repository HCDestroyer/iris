package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
)

// Webhooks de coexistencia (contratos/conversaciones.md §15.2): lo que se manda desde la app
// WhatsApp Business del teléfono (smb_message_echoes), el historial que el dueño comparte (history),
// los contactos del teléfono (smb_app_state_sync) y la desconexión del número (account_update).
// Nada de esto asigna, dispara el bot, manda avisos o encuestas, ni abre una espera.

// Orígenes de un mensaje que no salió de Iris.
const (
	OrigenTelefono  = "telefono"
	OrigenHistorial = "historial"
)

// codigoHistorialRechazado: el dueño no aceptó compartir el historial.
const codigoHistorialRechazado = 2593109

func (m *Modulo) coexistencia(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, _ string, ecos []metaMensaje,
	historial []metaHistorial, contactos []metaEstadoContact, perfiles map[string]string, ahora time.Time) error {
	for _, e := range ecos {
		if err := m.ecoTelefono(ctx, t, canal, e, perfiles[e.To], ahora); err != nil {
			return err
		}
	}
	if len(historial) > 0 {
		if err := m.importarHistorial(ctx, t, canal, historial, ahora); err != nil {
			return err
		}
	}
	if len(contactos) > 0 {
		return m.contactosTelefono(ctx, t, canal, contactos)
	}
	return nil
}

// ecoTelefono guarda un mensaje mandado desde el teléfono en la conversación del cliente.
func (m *Modulo) ecoTelefono(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, msg metaMensaje, perfil string, ahora time.Time) error {
	numero := "+" + strings.TrimPrefix(msg.To, "+")
	if msg.ID == "" || !numeroWA.MatchString(numero) {
		return nil
	}
	q := mensajeriabd.New(t)
	// La misma regla que un mensaje que llega: una conversación abierta en otra empresa del número
	// recibe el eco (el cliente ve una sola conversación).
	d, err := m.rutearEntrante(ctx, q, canal, numero)
	if err != nil {
		return err
	}
	t.FijarEmpresa(d.empresa)
	c, nueva, err := m.conversacionEco(ctx, t, canal, d, numero, perfil, ahora)
	if err != nil {
		return err
	}
	wamid := msg.ID
	if _, err := q.MensajeEntrantePorProveedor(ctx, mensajeriabd.MensajeEntrantePorProveedorParams{ConversacionID: c.ID, ProveedorID: &wamid}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	n := contenidoEntrante(msg)
	cuando := instanteMeta(msg.Timestamp, ahora)
	n.direccion, n.proveedor, n.creadoEn, n.origen = DirSaliente, &wamid, cuando, texto(OrigenTelefono)
	n.estadoEntrega = texto(EntregaEnviado)
	antes := c
	// Como una respuesta: se detiene el SLA y espera al cliente (sin asignar a nadie).
	if c.Estado == EstadoConvAbierta {
		c.Estado = EstadoConvEsperando
	}
	if c.Estado != EstadoConvResuelta {
		c.EsperaDesde, c.SlaVenceEn, c.SlaAvisado = nil, nil, false
	}
	f, err := m.agregarMensaje(ctx, t, &c, n)
	if err != nil {
		return err
	}
	if c, err = guardarConv(ctx, q, c, nueva || antes.Estado != c.Estado); err != nil {
		return err
	}
	if antes.Estado != c.Estado {
		if err := limpiarEspera(ctx, q, &c); err != nil {
			return err
		}
	}
	if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
		return err
	}
	if n.medio != nil {
		if err := m.encolar(ctx, t.Tx, ArgsDescargarMedio{MensajeID: f.ID}); err != nil {
			return err
		}
	}
	if err := q.MarcarEcoTelefono(ctx, mensajeriabd.MarcarEcoTelefonoParams{ID: canal.ID, EcoEn: ahora}); err != nil {
		return err
	}
	return t.Emitir("conversacion.mensaje_enviado", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
		"autor_id": nil, "tipo": f.Tipo, "plantilla": nil, "respuesta_rapida_id": nil, "origen": OrigenTelefono})
}

// conversacionEco: la conversación del eco; si es nueva se crea sin asignar y esperando al cliente.
func (m *Modulo) conversacionEco(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, d destinoEntrante, numero, perfil string,
	ahora time.Time) (mensajeriabd.MensajeriaConversacion, bool, error) {
	c, nueva, err := m.conversacionDe(ctx, t, canal, d, numero, perfil, ahora, OrigenTelefono)
	if err != nil || !nueva {
		return c, nueva, err
	}
	c.Estado = EstadoConvEsperando
	return c, true, nil
}

// importarHistorial guarda el historial como conversaciones resueltas de la empresa dueña, sin
// eventos de conversación (ni bot, ni avisos, ni encuestas), deduplicado por id de Meta.
func (m *Modulo) importarHistorial(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, lotes []metaHistorial, ahora time.Time) error {
	q := mensajeriabd.New(t)
	f, err := q.CanalParaSincronizar(ctx, canal.ID)
	if err != nil {
		return err
	}
	sync, ok := sincronizacionDe(f)
	if !ok {
		return nil
	}
	empresa := derefEmpresa(canal.EmpresaID)
	t.FijarEmpresa(empresa)
	negocio := soloDigitos(canal.Numero)
	guardados := 0
	for _, l := range lotes {
		if tieneCodigo(l.Errors, codigoHistorialRechazado) {
			sync.Mensajes.Estado = SyncRechazado
			continue
		}
		if l.Metadata.Phase > sync.Mensajes.Fase {
			sync.Mensajes.Fase = l.Metadata.Phase
		}
		if l.Metadata.Progress > sync.Mensajes.Progreso {
			sync.Mensajes.Progreso = min(l.Metadata.Progress, 100)
		}
		for _, h := range l.Threads {
			cliente := h.Context.WaID
			if cliente == "" {
				cliente = h.ID
			}
			numero := "+" + strings.TrimPrefix(soloDigitos(cliente), "+")
			if !numeroWA.MatchString(numero) || len(h.Messages) == 0 {
				continue
			}
			n, err := m.hiloHistorial(ctx, t, canal, empresa, numero, negocio, h.Messages, ahora)
			if err != nil {
				return err
			}
			guardados += n
		}
	}
	sync.Mensajes.Mensajes += guardados
	switch {
	case sync.Mensajes.Estado == SyncRechazado:
	case sync.Mensajes.Progreso >= 100:
		sync.Mensajes.Estado = SyncListo
	default:
		sync.Mensajes.Estado = SyncRecibiendo
	}
	crudo, _ := json.Marshal(sync)
	_, err = q.GuardarSincronizacion(ctx, mensajeriabd.GuardarSincronizacionParams{ID: canal.ID, Sincronizacion: crudo})
	t.Anotar(map[string]any{"historial_mensajes": guardados, "historial_progreso": sync.Mensajes.Progreso})
	return err
}

func tieneCodigo(errs []struct {
	Code int `json:"code"`
}, codigo int) bool {
	for _, e := range errs {
		if e.Code == codigo {
			return true
		}
	}
	return false
}

// hiloHistorial guarda los mensajes de un hilo (en orden de fecha) en la conversación del número.
func (m *Modulo) hiloHistorial(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, empresa int64, numero, negocio string,
	mensajes []metaMensaje, ahora time.Time) (int, error) {
	q := mensajeriabd.New(t)
	c, err := q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: empresa, CanalID: canal.ID, Numero: numero})
	nueva := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !nueva {
		return 0, err
	}
	if nueva {
		// Nace resuelta, sin eventos: es historia.
		var cliente *int64
		var asociacion *string
		cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(numero), Empresa: empresa})
		if err != nil {
			return 0, err
		}
		titulares := []int64{}
		for _, x := range cs {
			if x.PorTelefono || x.PorMovil {
				titulares = append(titulares, x.ID)
			}
		}
		if len(titulares) == 1 {
			cliente, asociacion = &titulares[0], texto("automatica")
		}
		c, err = q.InsertarConversacionHistorial(ctx, mensajeriabd.InsertarConversacionHistorialParams{EmpresaID: canal.EmpresaID, CanalID: canal.ID,
			Numero: numero, ColaID: canal.ColaID, ClienteID: cliente, Asociacion: asociacion, ClienteVerificado: cliente != nil,
			UltimaActividad: ahora, ResueltaEn: &ahora})
		if errors.Is(err, pgx.ErrNoRows) {
			c, err = q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: empresa, CanalID: canal.ID, Numero: numero})
			nueva = false
		}
		if err != nil {
			return 0, err
		}
	}
	orden := make([]metaMensaje, len(mensajes))
	copy(orden, mensajes)
	sort.SliceStable(orden, func(i, j int) bool {
		return instanteMeta(orden[i].Timestamp, ahora).Before(instanteMeta(orden[j].Timestamp, ahora))
	})
	guardados := 0
	var ultimo time.Time
	for _, msg := range orden {
		if msg.ID == "" {
			continue
		}
		wamid := msg.ID
		if _, err := q.MensajeEntrantePorProveedor(ctx, mensajeriabd.MensajeEntrantePorProveedorParams{ConversacionID: c.ID, ProveedorID: &wamid}); err == nil {
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		n := contenidoEntrante(msg)
		// Los medios del historial no se descargan (Meta solo da el id de los de 14 días).
		if n.medio != nil && n.texto == nil {
			n.texto = texto("[" + n.tipo + "]")
		}
		n.medio = nil
		cuando := instanteMeta(msg.Timestamp, ahora)
		n.proveedor, n.creadoEn, n.origen, n.sinFeed = &wamid, cuando, texto(OrigenHistorial), true
		if soloDigitos(msg.From) == negocio {
			n.direccion, n.estadoEntrega = DirSaliente, texto(EntregaEnviado)
		}
		if _, err := m.agregarMensaje(ctx, t, &c, n); err != nil {
			return 0, err
		}
		if cuando.After(ultimo) {
			ultimo = cuando
		}
		guardados++
	}
	if nueva && !ultimo.IsZero() {
		// Una conversación que solo trae historia queda con la fecha de su último mensaje.
		if err := q.FijarUltimaActividad(ctx, mensajeriabd.FijarUltimaActividadParams{ID: c.ID, UltimaActividad: ultimo}); err != nil {
			return 0, err
		}
	}
	return guardados, nil
}

// contactosTelefono: los contactos de la app del teléfono. Un cliente de Argos de la dueña solo se
// cuenta como asociado (nunca se toca Argos); los demás se crean o se renombran como contactos de
// Iris.
func (m *Modulo) contactosTelefono(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, contactos []metaEstadoContact) error {
	q := mensajeriabd.New(t)
	f, err := q.CanalParaSincronizar(ctx, canal.ID)
	if err != nil {
		return err
	}
	sync, ok := sincronizacionDe(f)
	if !ok {
		return nil
	}
	empresa := derefEmpresa(canal.EmpresaID)
	t.FijarEmpresa(empresa)
	creados, renombrados := 0, 0
	for _, x := range contactos {
		if x.Type != "" && x.Type != "contact" {
			continue
		}
		if x.Action != "" && x.Action != "add" {
			continue
		}
		numero := "+" + soloDigitos(x.Contact.PhoneNumber)
		nombre := strings.TrimSpace(x.Contact.FullName)
		if nombre == "" {
			nombre = strings.TrimSpace(x.Contact.FirstName)
		}
		if !numeroWA.MatchString(numero) || len(numero) < 9 {
			continue
		}
		sync.Contactos.Total++
		cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(numero), Empresa: empresa})
		if err != nil {
			return err
		}
		if len(cs) > 0 {
			sync.Contactos.Asociados++
			continue
		}
		if nombre == "" {
			nombre = numero
		}
		nombre = recortar(nombre, 120)
		existentes, err := q.ContactosIrisPorTelefono(ctx, mensajeriabd.ContactosIrisPorTelefonoParams{Empresa: empresa, Telefono: numero})
		if err != nil {
			return err
		}
		if len(existentes) > 0 {
			e := existentes[0]
			if e.Nombre != nombre {
				if _, err := q.RenombrarContactoIris(ctx, mensajeriabd.RenombrarContactoIrisParams{ID: e.ID, Nombre: nombre}); err != nil {
					return err
				}
				renombrados++
			}
			continue
		}
		ci, err := q.InsertarContactoIris(ctx, mensajeriabd.InsertarContactoIrisParams{EmpresaID: canal.EmpresaID, Nombre: nombre, Tipo: "otro",
			Telefono: numero, Nota: "Contacto del teléfono del número " + canal.Nombre})
		if err != nil {
			return err
		}
		creados++
		if err := t.Emitir("contacto_iris.creado", map[string]any{"contacto_iris_id": ci.ID, "empresa_id": ci.EmpresaID, "tipo": ci.Tipo,
			"conversacion_id": nil, "origen": OrigenTelefono}); err != nil {
			return err
		}
	}
	sync.Contactos.Estado = SyncListo
	crudo, _ := json.Marshal(sync)
	_, err = q.GuardarSincronizacion(ctx, mensajeriabd.GuardarSincronizacionParams{ID: canal.ID, Sincronizacion: crudo})
	t.Anotar(map[string]any{"contactos_creados": creados, "contactos_renombrados": renombrados})
	return err
}

// cuentaActualizada aplica account_update a los números QR de la WABA: PARTNER_REMOVED y
// ACCOUNT_OFFBOARDED los desconectan; ACCOUNT_RECONNECTED los reconecta.
func (m *Modulo) cuentaActualizada(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal mensajeriabd.MensajeriaCanal, entrada, evento string,
	waba *struct {
		WabaID string `json:"waba_id"`
	}, desconexion *struct {
		Reason      string `json:"reason"`
		InitiatedBy string `json:"initiated_by"`
	}) error {
	if canal.TipoConexion != ConexionQR {
		return nil
	}
	// El evento es de la WABA del número que recibió el webhook (si no, se ignora).
	id := entrada
	if waba != nil && waba.WabaID != "" {
		id = waba.WabaID
	}
	if id != "" && canal.WabaID != "" && id != canal.WabaID {
		t.Anotar(map[string]any{"account_update_ajeno": true})
		return nil
	}
	f, err := q.CanalParaSincronizar(ctx, canal.ID)
	if err != nil {
		return err
	}
	ahora := m.ahora()
	switch strings.ToUpper(evento) {
	case "PARTNER_REMOVED", "ACCOUNT_OFFBOARDED":
		if f.DesconectadoEn != nil {
			return nil
		}
		motivo := strings.ToLower(evento)
		if desconexion != nil && reCortoSeguro.MatchString(desconexion.Reason) {
			motivo = strings.ToLower(desconexion.Reason)
		}
		motivo = recortar(motivo, 60)
		f, err = q.DesconectarCanal(ctx, mensajeriabd.DesconectarCanalParams{ID: f.ID, DesconectadoEn: &ahora, Motivo: &motivo})
		if err != nil {
			return err
		}
		t.FijarEmpresa(derefEmpresa(f.EmpresaID))
		t.RegistrarCambios("canal_mensajeria", f.ID, map[string]any{"desconectado": false}, map[string]any{"desconectado": true, "motivo": motivo})
		return t.Emitir("canal_mensajeria.desconectado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "motivo": motivo})
	case "ACCOUNT_RECONNECTED":
		if f.DesconectadoEn == nil {
			return nil
		}
		if f, err = q.QuitarDesconexionCanal(ctx, f.ID); err != nil {
			return err
		}
		t.FijarEmpresa(derefEmpresa(f.EmpresaID))
		t.RegistrarCambios("canal_mensajeria", f.ID, map[string]any{"desconectado": true}, map[string]any{"desconectado": false})
		return t.Emitir("canal_mensajeria.reconectado", map[string]any{"canal_id": f.ID, "empresa_id": f.EmpresaID, "origen": "meta"})
	}
	return nil
}
