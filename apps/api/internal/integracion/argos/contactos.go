package argos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Contactos del cliente (contrato 0.10.0 §3.5, A42; 0.10.1 §3.8, A46; pedidos B22 y B25). Argos es
// el dueño: Iris guarda una caché de lectura (integracion.argos_contactos) que llena con
// GET /clientes/:id/contactos y refresca con el webhook cliente.contactos_cambiados. La mensajería
// la lee en sus transacciones para asociar una conversación al cliente con el rol del contacto.

// ContratoContactosCliente es el primer contrato con contactos (A42): buscar, leer y crear.
const ContratoContactosCliente = "0.10.0"

// ContratoContactoACargo es el primer contrato con X-Iris-Contacto (§7.1) y persona_cliente_id (A46).
const ContratoContactoACargo = "0.10.1"

// CabeceraContacto: quién habla en la conversación (§7.1, 0.10.1): <contacto_id> o «titular».
const CabeceraContacto = "X-Iris-Contacto"

// ContactoTitular es el valor literal de X-Iris-Contacto cuando habla el titular.
const ContactoTitular = "titular"

// EventoContactosCambiados: webhook de Argos (§2.3) cuando cambia un contacto (sin números).
const EventoContactosCambiados = "cliente.contactos_cambiados"

// EventoContactosArgosCambiados es el evento interno (outbox) que sale al aplicar el webhook.
const EventoContactosArgosCambiados = "argos.contactos_cambiados"

// ActorSistema: X-Iris-Actor del refresco que hace Iris sola (contrato 0.14.2): vale solo en
// GET /clientes/:id/contactos, GET /cuentas-bancarias y GET /bancos, y nunca va con
// X-Iris-Conversacion ni X-Iris-Contacto.
const ActorSistema = "sistema"

// VigenciaContactos: con más de esto, una persona que necesita los contactos los vuelve a leer.
const VigenciaContactos = 24 * time.Hour

var reCabeceraContacto = regexp.MustCompile(`^([1-9][0-9]{0,17}|titular)$`)

// ValorContacto devuelve el valor de X-Iris-Contacto para un contacto (nil = titular).
func ValorContacto(contactoID *int64) string {
	if contactoID == nil {
		return ContactoTitular
	}
	return strconv.FormatInt(*contactoID, 10)
}

// PermisosContacto son los permisos que el titular le da a un contacto (§3.5).
type PermisosContacto struct {
	Consultar bool `json:"consultar"`
	Reportar  bool `json:"reportar"`
	Pagar     bool `json:"pagar"`
	Avisos    bool `json:"avisos"`
}

// TelefonoContacto es un número de un contacto (E.164).
type TelefonoContacto struct {
	Numero    string `json:"numero"`
	Whatsapp  bool   `json:"whatsapp"`
	Principal bool   `json:"principal"`
}

// ContactoClienteArgos es un contacto de GET /clientes/:id/contactos (§3.5).
type ContactoClienteArgos struct {
	ContactoID int64  `json:"contacto_id"`
	ID         int64  `json:"id"`
	ClienteID  int64  `json:"cliente_id"`
	EmpresaID  *int64 `json:"empresa_id"`
	Nombre     string `json:"nombre"`
	Relacion   string `json:"relacion"`
	EsTitular  bool   `json:"es_titular"`
	// Servicios: nil = todos los servicios del cliente.
	Servicios []int64            `json:"servicios"`
	Permisos  *PermisosContacto  `json:"permisos"`
	Telefonos []TelefonoContacto `json:"telefonos"`
	// PersonaClienteID: otro cliente de la misma empresa a cargo de este servicio (0.10.1, A46).
	PersonaClienteID *int64 `json:"persona_cliente_id"`
	Version          int32  `json:"version"`
}

// id: Argos manda contacto_id (forma de §3.5); se acepta también id.
func (c ContactoClienteArgos) id() int64 {
	if c.ContactoID > 0 {
		return c.ContactoID
	}
	return c.ID
}

// ContactoBuscado es una fila de GET /contactos/buscar?telefono= (§3.5): nunca trae otros números.
type ContactoBuscado struct {
	ClienteID  int64             `json:"cliente_id"`
	EmpresaID  *int64            `json:"empresa_id"`
	ContactoID *int64            `json:"contacto_id"`
	Nombre     string            `json:"nombre"`
	Relacion   string            `json:"relacion"`
	EsTitular  bool              `json:"es_titular"`
	Adicional  bool              `json:"adicional"`
	Servicios  []int64           `json:"servicios"`
	Permisos   *PermisosContacto `json:"permisos"`
	// ACargo: 0.10.1, la fila es de un contacto que no es el titular (§3.8).
	ACargo bool `json:"a_cargo"`
}

// relaciones válidas en la caché (§3.5).
var relacionesContacto = map[string]bool{"titular": true, "familiar": true, "inquilino": true, "empleado": true, "otro": true}

// reE164 es la forma de un número de contacto.
var reE164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// DigitosTelefono deja solo los dígitos, con 502 delante de un número de 8 dígitos (la misma regla
// que la búsqueda en la instantánea de clientes).
func DigitosTelefono(numero string) string {
	var b strings.Builder
	for _, r := range numero {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) == 8 {
		return "502" + d
	}
	return d
}

// SoportaContactos indica si el conector Argos activo informa contrato ≥ 0.10.0 (contactos) y
// ≥ 0.10.1 (X-Iris-Contacto y A46). false sin conector activo: falla cerrado.
func (m *Modulo) SoportaContactos(ctx context.Context, q conector.DBTX) (contactos, aCargo bool, err error) {
	var contrato *string
	err = q.QueryRow(ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE sistema = $1 AND estado = 'activo'
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&contrato)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	return conector.VersionAlMenos(contrato, ContratoContactosCliente), conector.VersionAlMenos(contrato, ContratoContactoACargo), err
}

// clienteConActor: el cliente del conector activo con X-Iris-Actor (buscar y leer contactos lo
// exigen, §3.5). actor vacío: «sistema» (0.14.2), que Argos solo acepta en
// GET /clientes/:id/contactos: con buscar, una persona o el asistente siempre.
func (m *Modulo) clienteConActor(ctx context.Context, actor string) (*Cliente, conector.Leido, error) {
	if actor != "" && !reCabeceraActor.MatchString(actor) {
		return nil, conector.Leido{}, ErrCabecerasIris
	}
	if actor == "" {
		actor = ActorSistema
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nil, conector.Leido{}, nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	if err != nil {
		return nil, c, err
	}
	cli := m.clienteDe(c)
	return &Cliente{Cliente: cli.ConCabecera(CabeceraActor, actor), contrato: cli.contrato}, c, nil
}

// BuscarContactos: GET /contactos/buscar?telefono=<E.164> (§3.5), con X-Iris-Actor de quien busca.
// Con Argos < 0.10.0 devuelve (nil, false, nil): la ruta no existe y se busca como antes (§7.6).
func (m *Modulo) BuscarContactos(ctx context.Context, actor, telefono string) ([]ContactoBuscado, bool, error) {
	if actor == "" {
		// «sistema» no vale en buscar (0.14.2): siempre una persona o el asistente.
		return nil, false, ErrCabecerasIris
	}
	cli, c, err := m.clienteConActor(ctx, actor)
	if err != nil {
		return nil, false, err
	}
	if !conector.VersionAlMenos(c.ContratoRemoto, ContratoContactosCliente) {
		return nil, false, nil
	}
	p, err := conector.ListaDe[ContactoBuscado](ctx, cli.Cliente, "/contactos/buscar", url.Values{"telefono": {telefono}})
	if err != nil {
		return nil, true, err
	}
	return p.Data, true, nil
}

// LeerContactosCliente: GET /clientes/:id/contactos (§3.5). actor vacío = «sistema» (0.14.2).
func (m *Modulo) LeerContactosCliente(ctx context.Context, actor string, clienteID int64) ([]ContactoClienteArgos, error) {
	cli, c, err := m.clienteConActor(ctx, actor)
	if err != nil {
		return nil, err
	}
	if !conector.VersionAlMenos(c.ContratoRemoto, ContratoContactosCliente) {
		return nil, ErrSinContactos
	}
	return leerContactosDe(ctx, cli, clienteID)
}

// ErrSinContactos: el conector Argos activo informa un contrato anterior a 0.10.0.
var ErrSinContactos = errors.New("argos: el contrato de Argos no tiene contactos (A42, 0.10.0)")

func leerContactosDe(ctx context.Context, cli *Cliente, clienteID int64) ([]ContactoClienteArgos, error) {
	var l []ContactoClienteArgos
	var cuerpo json.RawMessage
	if err := cli.Pedir(ctx, http.MethodGet, "/clientes/"+strconv.FormatInt(clienteID, 10)+"/contactos", nil, nil, true,
		&conector.Recurso{Destino: &cuerpo}); err != nil {
		return nil, err
	}
	// Lista plana o { data: [...] } (como las demás listas de Argos).
	t := strings.TrimSpace(string(cuerpo))
	if strings.HasPrefix(t, "[") {
		if err := json.Unmarshal(cuerpo, &l); err != nil {
			return nil, err
		}
	} else {
		var o struct {
			Data []ContactoClienteArgos `json:"data"`
		}
		if err := json.Unmarshal(cuerpo, &o); err != nil {
			return nil, err
		}
		l = o.Data
	}
	salida := make([]ContactoClienteArgos, 0, len(l))
	for _, x := range l {
		// Solo los contactos de ese cliente, con id y relación conocidos: lo demás no se guarda.
		if x.id() <= 0 || !relacionesContacto[x.Relacion] || (x.ClienteID != 0 && x.ClienteID != clienteID) {
			continue
		}
		x.ContactoID, x.ClienteID = x.id(), clienteID
		salida = append(salida, x)
	}
	return salida, nil
}

// guardarContactos reemplaza la caché de contactos del cliente por lo leído de Argos.
// Devuelve cuántos contactos que Iris tenía ya no devolvió Argos.
func guardarContactos(ctx context.Context, q *argosbd.Queries, clienteID int64, l []ContactoClienteArgos, ahora time.Time) (int64, error) {
	vigentes := make([]int64, 0, len(l))
	for _, x := range l {
		p := x.Permisos
		if p == nil {
			// Todos encendidos por defecto (§3.5).
			p = &PermisosContacto{Consultar: true, Reportar: true, Pagar: true, Avisos: true}
		}
		version := x.Version
		if version <= 0 {
			version = 1
		}
		nombre := x.Nombre
		if r := []rune(nombre); len(r) > 200 {
			nombre = string(r[:200])
		}
		if err := q.GuardarContactoArgos(ctx, argosbd.GuardarContactoArgosParams{ContactoID: x.ContactoID, ClienteID: clienteID, EmpresaID: x.EmpresaID,
			Nombre: nombre, Relacion: x.Relacion, EsTitular: x.EsTitular || x.Relacion == "titular", Servicios: x.Servicios,
			Consultar: p.Consultar, Reportar: p.Reportar, Pagar: p.Pagar, Avisos: p.Avisos, PersonaClienteID: x.PersonaClienteID,
			Version: version, SincronizadoEn: ahora}); err != nil {
			return 0, err
		}
		if err := q.BorrarTelefonosContactoArgos(ctx, x.ContactoID); err != nil {
			return 0, err
		}
		for _, t := range x.Telefonos {
			if !reE164.MatchString(t.Numero) {
				continue
			}
			if err := q.GuardarTelefonoContactoArgos(ctx, argosbd.GuardarTelefonoContactoArgosParams{ContactoID: x.ContactoID, Numero: t.Numero,
				Digitos: DigitosTelefono(t.Numero), Whatsapp: t.Whatsapp, Principal: t.Principal}); err != nil {
				return 0, err
			}
		}
		vigentes = append(vigentes, x.ContactoID)
	}
	quitados, err := q.BorrarContactosAusentes(ctx, argosbd.BorrarContactosAusentesParams{ClienteID: clienteID, Vigentes: vigentes})
	if err != nil {
		return 0, err
	}
	return quitados, q.MarcarContactosLeidos(ctx, argosbd.MarcarContactosLeidosParams{ClienteID: clienteID, SincronizadoEn: ahora})
}

// EntradaRefrescarContactos: el cliente y quién lo pide (X-Iris-Actor de §7.1).
type EntradaRefrescarContactos struct {
	ClienteID int64 `json:"cliente_id"`
	// Actor: «usuario:<id>» (una persona) o «asistente_ia».
	Actor string `json:"actor"`
	// Forzar: lee aunque la caché sea reciente.
	Forzar bool `json:"forzar"`
}

type entradaGuardarContactos struct {
	ClienteID int64  `json:"cliente_id"`
	Actor     string `json:"actor"`
	lista     []ContactoClienteArgos
	borrar    bool
}

func nuevoGuardarContactos() *auditoria.Caso[entradaGuardarContactos, int] {
	return auditoria.Nuevo(auditoria.Caso[entradaGuardarContactos, int]{
		// Lectura de datos personales (nombres y números de contactos): auditada.
		Accion: "clientes.sincronizar_contactos", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarContactos) (int, error) {
			t.FijarEntidad("cliente_argos", in.ClienteID)
			q := argosbd.New(t)
			antes, err := q.HuellaContactosCliente(ctx, in.ClienteID)
			if err != nil {
				return 0, err
			}
			var quitados int64
			if in.borrar {
				t.Anotar(map[string]any{"resultado_argos": "no_existe"})
				n, err := q.BorrarContactosDeCliente(ctx, in.ClienteID)
				if err != nil {
					return 0, err
				}
				if err := q.OlvidarContactosLeidos(ctx, in.ClienteID); err != nil {
					return 0, err
				}
				quitados = n
			} else {
				t.Anotar(map[string]any{"contactos": len(in.lista)})
				n, err := guardarContactos(ctx, q, in.ClienteID, in.lista, time.Now())
				if err != nil {
					return 0, err
				}
				quitados = n
			}
			despues, err := q.HuellaContactosCliente(ctx, in.ClienteID)
			if err != nil {
				return 0, err
			}
			if quitados > 0 || (antes != "" && antes != despues) {
				// Contactos que Argos ya no devuelve o cuyos números cambiaron: la mensajería quita la
				// verificación a las conversaciones de esos contactos (sin números ni nombres).
				accion := "editado"
				if quitados > 0 {
					accion = "quitado"
				}
				if err := t.Emitir(EventoContactosArgosCambiados, map[string]any{"cliente_id": in.ClienteID, "empresa_id": nil,
					"contacto_id": nil, "accion": accion, "persona_cliente_id": nil, "entrante_id": nil}); err != nil {
					return 0, err
				}
			}
			return len(in.lista), nil
		},
	})
}

// RefrescarContactos deja en la caché los contactos del cliente: si ya los leyó hace menos de
// VigenciaContactos (y no se fuerza) no llama a Argos. Se llama fuera de transacción. Con Argos
// < 0.10.0, sin conector o con Argos caído, no hace nada y lo dice (false): quien llama sigue con
// lo que haya. Un 404 borra la caché del cliente.
func (m *Modulo) RefrescarContactos(ctx context.Context, in EntradaRefrescarContactos) (bool, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return false, nucleo.ErrSinBD
	}
	if !in.Forzar {
		en, err := argosbd.New(bd).ContactosLeidosEn(ctx, in.ClienteID)
		switch {
		case err == nil && time.Since(en) < VigenciaContactos:
			return true, nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return false, err
		}
	}
	l, err := m.LeerContactosCliente(ctx, in.Actor, in.ClienteID)
	var p *nucleo.Problema
	switch {
	case errors.Is(err, ErrSinContactos), errors.As(err, &p) && p.Codigo() == "conector_apagado":
		return false, nil
	case EsNoEncontrado(err):
		_, err = m.guardarContactos.Ejecutar(ctx, entradaGuardarContactos{ClienteID: in.ClienteID, Actor: in.Actor, borrar: true})
		return false, err
	case err != nil:
		return false, nil
	}
	_, err = m.guardarContactos.Ejecutar(ctx, entradaGuardarContactos{ClienteID: in.ClienteID, Actor: in.Actor, lista: l})
	return err == nil, err
}

// Webhook cliente.contactos_cambiados (§2.3) --------------------------------------------------------

// cambioContactos es lo que se aplica de un cliente.contactos_cambiados.
type cambioContactos struct {
	ClienteID  int64
	EmpresaID  *int64
	ContactoID *int64
	Accion     string
	Persona    *int64
	// lista: lo releído de Argos (nil con accion quitado o si hay que olvidar la caché).
	lista []ContactoClienteArgos
	// olvidar: Argos no dejó releer sin X-Iris-Actor; la caché del cliente se invalida y la vuelve a
	// leer una persona cuando la necesite.
	olvidar bool
	// borrar: Argos ya no tiene al cliente (404).
	borrar bool
}

type payloadContactos struct {
	ClienteID        *int64 `json:"cliente_id"`
	EmpresaID        *int64 `json:"empresa_id"`
	ContactoID       *int64 `json:"contacto_id"`
	Accion           string `json:"accion"`
	PersonaClienteID *int64 `json:"persona_cliente_id"`
}

// leerContactosWebhook relee los contactos del cliente del webhook (fuera de la transacción). Solo
// de clientes que Iris ya conoce (instantánea o caché); el resto se ignora. Con accion quitado no se
// lee nada: el contacto sale de la caché al momento (§3.8: quitarlo rompe el vínculo al momento).
func (p *ProcesarEntrante) leerContactosWebhook(ctx context.Context, q conector.DBTX, e conector.Entrante) (*cambioContactos, string, error) {
	var d payloadContactos
	if err := conector.Decodificar(e.Payload, &d); err != nil || d.ClienteID == nil || *d.ClienteID <= 0 {
		return nil, "payload_invalido", nil
	}
	cam := &cambioContactos{ClienteID: *d.ClienteID, EmpresaID: d.EmpresaID, ContactoID: d.ContactoID, Accion: d.Accion, Persona: d.PersonaClienteID}
	var conocido bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM integracion.argos_clientes WHERE id = $1)
		OR EXISTS (SELECT 1 FROM integracion.argos_contactos WHERE cliente_id = $1)
		OR EXISTS (SELECT 1 FROM integracion.argos_contactos_leidos WHERE cliente_id = $1)`, cam.ClienteID).Scan(&conocido); err != nil {
		return nil, "", err
	}
	if !conocido {
		return nil, "cliente_no_en_instantanea", nil
	}
	if d.Accion == "quitado" && d.ContactoID != nil {
		return cam, "", nil
	}
	cli, err := p.clienteArgos(ctx, q, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	// Iris relee sola, sin una persona: X-Iris-Actor «sistema» (0.14.2), solo en esta ruta y nunca
	// con X-Iris-Conversacion ni X-Iris-Contacto (el cliente del conector no las lleva).
	cli = &Cliente{Cliente: cli.ConCabecera(CabeceraActor, ActorSistema), contrato: cli.contrato}
	l, err := leerContactosDe(ctx, cli, cam.ClienteID)
	var er *ErrorArgos
	switch {
	case err == nil:
		cam.lista = l
	case EsNoEncontrado(err):
		cam.borrar = true
	case errors.As(err, &er) && (er.Status == http.StatusBadRequest || er.Status == http.StatusForbidden):
		// Un Argos anterior a 0.14.2 no acepta «sistema» en GET /clientes/:id/contactos: se olvida la
		// caché del cliente (se relee con la próxima persona que la necesite).
		cam.olvidar = true
	default:
		return nil, "", err
	}
	return cam, "", nil
}

// aplicarContactos aplica el cambio en la transacción del webhook y emite argos.contactos_cambiados.
func aplicarContactos(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, e conector.Entrante, cam cambioContactos) error {
	switch {
	case cam.borrar:
		if _, err := q.BorrarContactosDeCliente(ctx, cam.ClienteID); err != nil {
			return err
		}
		if err := q.OlvidarContactosLeidos(ctx, cam.ClienteID); err != nil {
			return err
		}
	case cam.olvidar:
		t.Anotar(map[string]any{"contactos": "olvidados"})
		if _, err := q.BorrarContactosDeCliente(ctx, cam.ClienteID); err != nil {
			return err
		}
		if err := q.OlvidarContactosLeidos(ctx, cam.ClienteID); err != nil {
			return err
		}
	case cam.lista != nil:
		if _, err := guardarContactos(ctx, q, cam.ClienteID, cam.lista, time.Now()); err != nil {
			return err
		}
	case cam.ContactoID != nil:
		// quitado: sale de la caché al momento (sus teléfonos se van en cascada).
		if _, err := q.BorrarContactoArgos(ctx, *cam.ContactoID); err != nil {
			return err
		}
	}
	if cam.EmpresaID != nil {
		t.FijarEmpresa(*cam.EmpresaID)
	}
	// Sin números ni nombres (§2.3).
	return t.Emitir(EventoContactosArgosCambiados, map[string]any{"cliente_id": cam.ClienteID, "empresa_id": cam.EmpresaID,
		"contacto_id": cam.ContactoID, "accion": cam.Accion, "persona_cliente_id": cam.Persona, "entrante_id": e.ID})
}
