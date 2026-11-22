package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// conectorAlMenos: el contrato informado es ≥ minima.
func conectorAlMenos(contrato *string, minima string) bool {
	return conector.VersionAlMenos(contrato, minima)
}

// Contactos del cliente en la atención (contrato argosisp.md 0.10.0 §3.5, A42/B22; 0.10.1 §3.8,
// A46/B25). Argos es el dueño de los contactos; Iris lee su caché (integracion.argos_contactos, que
// llena integracion/argos) dentro de la transacción de la conversación:
//
//   - Al llegar un mensaje, el número se busca en la instantánea (teléfono y móvil del cliente) y en
//     la caché de contactos: con una sola persona, la conversación se asocia con el rol del contacto.
//     Un contacto con persona_cliente_id (José, a cargo del servicio de su mamá) cuenta como su
//     persona: la conversación queda con José y el servicio de la mamá aparece «a su cargo».
//   - Con varias personas no se asocia y nunca se muestran sus datos: se pide una referencia (el
//     código del servicio) y se compara solo contra los clientes que comparten el número (elegir_cliente).
//   - El rol decide qué se puede hacer: un contacto solo usa sus permisos (consultar, reportar,
//     pagar) y sobre sus servicios; las acciones van a Argos con X-Iris-Contacto (§7.1).
//   - Lo reservado al titular (cambiar titular, plan, Wi-Fi, traspaso y baja) nunca lo abre un
//     contacto: sigue con la identidad del titular (§7.5, A41).

// Permisos de un contacto (§3.5).
const (
	PermisoContactoConsultar = "consultar"
	PermisoContactoReportar  = "reportar"
	PermisoContactoPagar     = "pagar"
)

// rolEnCliente es quién habla en una conversación respecto de un cliente de Argos.
type rolEnCliente struct {
	ClienteID int64
	// ContactoID: el contacto de Argos; nil = el titular (o nadie, con SinRol).
	ContactoID *int64
	Nombre     string
	Relacion   string
	Permisos   argos.PermisosContacto
	// Servicios: nil = todos los del cliente.
	Servicios []int64
	// SinRol: no se sabe quién habla (el número no es del titular ni de un contacto en la caché y nadie
	// lo verificó): ningún permiso, falla cerrado.
	SinRol bool
}

func (r rolEnCliente) esTitular() bool { return r.ContactoID == nil && !r.SinRol }

// cubre: el servicio está entre los del contacto (el titular los tiene todos; sin rol, ninguno).
func (r rolEnCliente) cubre(servicio int64) bool {
	if r.SinRol {
		return false
	}
	return r.esTitular() || r.Servicios == nil || slices.Contains(r.Servicios, servicio)
}

// permite: el contacto tiene ese permiso (el titular, todos; sin rol, ninguno).
func (r rolEnCliente) permite(permiso string) bool {
	if r.SinRol {
		return false
	}
	if r.esTitular() {
		return true
	}
	switch permiso {
	case PermisoContactoConsultar:
		return r.Permisos.Consultar
	case PermisoContactoReportar:
		return r.Permisos.Reportar
	case PermisoContactoPagar:
		return r.Permisos.Pagar
	}
	return false
}

// cabecera: el valor de X-Iris-Contacto (§7.1).
func (r rolEnCliente) cabecera() string { return argos.ValorContacto(r.ContactoID) }

// rolConversacion: el rol en el cliente asociado y los servicios de otros clientes a su cargo (B25).
type rolConversacion struct {
	Propio *rolEnCliente
	ACargo []rolEnCliente
}

func rolDeFila(x mensajeriabd.ContactosArgosDelNumeroRow) rolEnCliente {
	r := rolEnCliente{ClienteID: x.ClienteID, Nombre: x.Nombre, Relacion: x.Relacion, Servicios: x.Servicios,
		Permisos: argos.PermisosContacto{Consultar: x.Consultar, Reportar: x.Reportar, Pagar: x.Pagar, Avisos: x.Avisos}}
	if !x.EsTitular {
		id := x.ContactoID
		r.ContactoID = &id
	}
	return r
}

// titularesDelNumero: los clientes de la empresa cuyo teléfono o móvil (instantánea) es ese número.
func titularesDelNumero(ctx context.Context, q *mensajeriabd.Queries, empresa int64, numero string) ([]int64, error) {
	cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(numero), Empresa: empresa})
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, x := range cs {
		if (x.PorTelefono || x.PorMovil) && !slices.Contains(out, x.ID) {
			out = append(out, x.ID)
		}
	}
	return out, nil
}

// personaDe: de quién es la persona de esa fila. La persona a cargo (A46, José) solo si el número es
// el teléfono o el móvil de José en la instantánea (titulares); si no, el cliente del contacto (la
// mamá), con el rol del contacto: un número del contacto que no es de José nunca abre la cuenta de José.
func personaDe(x mensajeriabd.ContactosArgosDelNumeroRow, titulares []int64) int64 {
	if x.PersonaClienteID != nil && slices.Contains(titulares, *x.PersonaClienteID) {
		return *x.PersonaClienteID
	}
	return x.ClienteID
}

// personaNumero: una persona con ese número y, si llegó por un contacto (no por ser titular), ese contacto.
type personaNumero struct {
	cliente  int64
	contacto *int64
}

// personasDelNumero: los clientes (personas) de la empresa con ese número, en orden: los titulares
// por la instantánea (teléfono o móvil) y los de la caché de contactos. Un contacto a cargo de otro
// cliente cuenta como esa persona solo si el número es el de esa persona (personaDe).
func personasDelNumero(ctx context.Context, q *mensajeriabd.Queries, empresa int64, numero string) ([]personaNumero, error) {
	titulares, err := titularesDelNumero(ctx, q, empresa, numero)
	if err != nil {
		return nil, err
	}
	personas := []personaNumero{}
	esta := func(id int64) bool {
		return slices.ContainsFunc(personas, func(p personaNumero) bool { return p.cliente == id })
	}
	for _, id := range titulares {
		personas = append(personas, personaNumero{cliente: id})
	}
	ks, err := q.ContactosArgosDelNumero(ctx, mensajeriabd.ContactosArgosDelNumeroParams{Digitos: soloDigitos(numero), Empresa: empresa})
	if err != nil {
		return nil, err
	}
	for _, x := range ks {
		if !x.ClienteEnEmpresa {
			continue
		}
		if p := personaDe(x, titulares); !esta(p) {
			id := x.ContactoID
			personas = append(personas, personaNumero{cliente: p, contacto: &id})
		}
	}
	return personas, nil
}

// clienteUnicoDelNumero: el cliente a asociar solo (automática) si el número es de una sola persona
// de la empresa y está en la instantánea; si no, ninguno. contacto: el contacto de Argos por el que se
// asocia (nil si es el titular), que se guarda en la conversación.
func clienteUnicoDelNumero(ctx context.Context, q *mensajeriabd.Queries, empresa int64, numero string) (cliente *int64, asociacion *string, contacto *int64, err error) {
	ps, err := personasDelNumero(ctx, q, empresa, numero)
	if err != nil || len(ps) != 1 {
		return nil, nil, nil, err
	}
	if _, err := q.ClienteInstantanea(ctx, ps[0].cliente); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, nil
	} else if err != nil {
		return nil, nil, nil, err
	}
	return &ps[0].cliente, nombrePtr("automatica"), ps[0].contacto, nil
}

// fijarContactoConv guarda en la conversación el contacto con que se asoció (B22).
func fijarContactoConv(ctx context.Context, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion, contacto *int64) error {
	if err := q.FijarContactoArgosConv(ctx, mensajeriabd.FijarContactoArgosConvParams{ID: c.ID, ContactoArgosID: contacto}); err != nil {
		return err
	}
	c.ContactoArgosID = contacto
	return nil
}

// rolDeConversacion: el rol del número de la conversación en su cliente y lo que tiene a su cargo.
// Falla cerrado: titular solo si el número es el teléfono o el móvil del cliente, o si un supervisor
// confirmó el cliente (verificado_por) o pasó la verificación fuerte; un contacto, solo si está en la
// caché (y, si la conversación guardó con cuál se asoció, ese mismo); si no, sin rol (sin permisos).
// Sin cliente asociado, vacío.
func (m *Modulo) rolDeConversacion(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (rolConversacion, error) {
	var r rolConversacion
	if c.ClienteID == nil {
		return r, nil
	}
	cliente := *c.ClienteID
	empresa := empresaDeConv(c)
	titulares, err := titularesDelNumero(ctx, q, empresa, c.Numero)
	if err != nil {
		return r, err
	}
	titular := slices.Contains(titulares, cliente)
	ks, err := q.ContactosArgosDelNumero(ctx, mensajeriabd.ContactosArgosDelNumeroParams{Digitos: soloDigitos(c.Numero), Empresa: empresa})
	if err != nil {
		return r, err
	}
	for _, x := range ks {
		switch {
		case x.ClienteID == cliente && r.Propio == nil && !titular && (c.ContactoArgosID == nil || *c.ContactoArgosID == x.ContactoID):
			p := rolDeFila(x)
			r.Propio = &p
		case titular && x.ClienteID != cliente && x.ClienteEnEmpresa && x.PersonaClienteID != nil && *x.PersonaClienteID == cliente:
			// A su cargo (A46): solo si el número es el de esta persona (es su titular).
			r.ACargo = append(r.ACargo, rolDeFila(x))
		}
	}
	if r.Propio != nil {
		return r, nil
	}
	confiable := titular || (c.ClienteVerificado && c.VerificadoPor != nil)
	if !confiable && c.ClienteVerificado {
		vig, err := vigenciaFuerte(ctx, q, empresa)
		if err != nil {
			return r, err
		}
		confiable = verificacionDe(c, m.ahora(), vig) == VerificacionFuerte
	}
	if confiable {
		r.Propio = &rolEnCliente{ClienteID: cliente, Permisos: argos.PermisosContacto{Consultar: true, Reportar: true, Pagar: true, Avisos: true}}
	} else {
		r.Propio = &rolEnCliente{ClienteID: cliente, SinRol: true}
	}
	return r, nil
}

// Errores de contacto (mismos códigos que Argos, §7.1).
func servicioFueraDelContacto() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "servicio_fuera_del_contacto", "Servicio fuera del contacto",
		"Quien escribe es un contacto del cliente y ese servicio no está entre los suyos")
}

func permisoDeContacto(permiso string) *nucleo.Problema {
	que := map[string]string{PermisoContactoConsultar: "consultar el saldo y el estado", PermisoContactoReportar: "reportar fallas o abrir tickets",
		PermisoContactoPagar: "pagar (boletas y promesas)"}[permiso]
	return nucleo.NuevoProblema(http.StatusForbidden, "permiso_de_contacto", "Sin permiso del titular",
		"Quien escribe es un contacto del cliente y el titular no le dio permiso para "+que)
}

func servicioRequeridoContacto() *nucleo.Problema {
	return campoConv("servicioId", "Quien escribe es un contacto con varios servicios: elige de cuál se trata", "required")
}

// accesoServicio es a nombre de quién va una acción sobre un servicio: el cliente dueño del servicio
// y el valor de X-Iris-Contacto.
type accesoServicio struct {
	ClienteID  int64
	ServicioID *int64
	// Contacto: X-Iris-Contacto («titular» o el contacto_id de Argos).
	Contacto string
	// ACargo: el servicio es de otro cliente a cargo de quien habla (B25).
	ACargo bool
}

// accesoAServicio decide si quien habla en la conversación puede hacer una acción (permiso del
// contacto) sobre el servicio (nil = el de la conversación o, para un contacto, el único suyo).
// Para el titular no cambia nada: el cliente de la conversación y «titular».
func (m *Modulo) accesoAServicio(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, servicio *int64, permiso string) (accesoServicio, error) {
	if c.ClienteID == nil {
		return accesoServicio{}, reglaConv("sin_cliente", "Asocia la conversación a un cliente de Argos")
	}
	rol, err := m.rolDeConversacion(ctx, q, c)
	if err != nil {
		return accesoServicio{}, err
	}
	propio := *rol.Propio
	if propio.SinRol {
		// No se sabe quién habla: un supervisor confirma el cliente (o el titular se verifica).
		return accesoServicio{}, clienteSinConfirmar()
	}
	if servicio == nil {
		servicio = c.ServicioID
	}
	if servicio == nil {
		if propio.esTitular() {
			return accesoServicio{ClienteID: *c.ClienteID, Contacto: argos.ContactoTitular}, nil
		}
		if !propio.permite(permiso) {
			return accesoServicio{}, permisoDeContacto(permiso)
		}
		// Un contacto siempre actúa sobre un servicio suyo (§3.5): si tiene uno solo, ese.
		unico, err := servicioUnicoDe(ctx, q, empresaDeConv(c), propio)
		if err != nil {
			return accesoServicio{}, err
		}
		if unico == nil {
			return accesoServicio{}, servicioRequeridoContacto()
		}
		return accesoServicio{ClienteID: *c.ClienteID, ServicioID: unico, Contacto: propio.cabecera()}, nil
	}
	s, err := q.ServicioInstantaneaPorID(ctx, *servicio)
	if errors.Is(err, pgx.ErrNoRows) {
		// No está en la instantánea: la validación de siempre (servicio del cliente en la empresa) decide.
		if !propio.esTitular() {
			return accesoServicio{}, servicioFueraDelContacto()
		}
		return accesoServicio{ClienteID: *c.ClienteID, ServicioID: servicio, Contacto: argos.ContactoTitular}, nil
	}
	if err != nil {
		return accesoServicio{}, err
	}
	if s.ClienteID == *c.ClienteID {
		if !propio.cubre(s.ID) {
			return accesoServicio{}, servicioFueraDelContacto()
		}
		if !propio.permite(permiso) {
			return accesoServicio{}, permisoDeContacto(permiso)
		}
		return accesoServicio{ClienteID: s.ClienteID, ServicioID: &s.ID, Contacto: propio.cabecera()}, nil
	}
	for _, k := range rol.ACargo {
		if k.ClienteID != s.ClienteID || !k.cubre(s.ID) {
			continue
		}
		// Verificación heredada (§3.8): vale solo con los permisos de ese contacto.
		if !k.permite(permiso) {
			return accesoServicio{}, permisoDeContacto(permiso)
		}
		return accesoServicio{ClienteID: s.ClienteID, ServicioID: &s.ID, Contacto: k.cabecera(), ACargo: true}, nil
	}
	if !propio.esTitular() || len(rol.ACargo) > 0 {
		return accesoServicio{}, servicioFueraDelContacto()
	}
	// Titular con un servicio de otro cliente: la validación de siempre lo rechaza.
	return accesoServicio{ClienteID: *c.ClienteID, ServicioID: servicio, Contacto: argos.ContactoTitular}, nil
}

// servicioUnicoDe: el único servicio del contacto en la empresa (nil si tiene varios o ninguno).
func servicioUnicoDe(ctx context.Context, q *mensajeriabd.Queries, empresa int64, r rolEnCliente) (*int64, error) {
	ss, err := q.ServiciosDeClientesInstantanea(ctx, mensajeriabd.ServiciosDeClientesInstantaneaParams{Clientes: []int64{r.ClienteID}, Empresa: empresa})
	if err != nil {
		return nil, err
	}
	var unico *int64
	for _, s := range ss {
		if !r.cubre(s.ID) {
			continue
		}
		if unico != nil {
			return nil, nil
		}
		id := s.ID
		unico = &id
	}
	return unico, nil
}

// Lo que ve el panel ----------------------------------------------------------------------------------

// ServicioContacto es un servicio que el panel ofrece en la ficha: solo el código y el nombre (A45).
type ServicioContacto struct {
	ID     int64  `json:"id"`
	Codigo string `json:"codigo" doc:"Código del servicio en Argos (S-000412)"`
	Nombre string `json:"nombre" doc:"Nombre que eligió el cliente («Casa»); texto del cliente: mostrarlo escapado"`
}

// PermisosContactoConv son los permisos del contacto que habla (§3.5).
type PermisosContactoConv struct {
	Consultar bool `json:"consultar" doc:"Saldo, estado del servicio y facturas"`
	Reportar  bool `json:"reportar" doc:"Tickets y fallas"`
	Pagar     bool `json:"pagar" doc:"Boletas y promesas"`
	Avisos    bool `json:"avisos" doc:"Recibe avisos de cobro"`
}

// ACargoConv son los servicios de otro cliente a cargo de quien habla (§3.8, B25).
type ACargoConv struct {
	ContactoID int64                `json:"contactoId" doc:"El contacto de Argos de quien habla en ese cliente (X-Iris-Contacto)"`
	Permisos   PermisosContactoConv `json:"permisos"`
	Servicios  []ServicioContacto   `json:"servicios" doc:"Solo código y nombre: nunca el nombre del cliente ni otros datos"`
}

// RolContactoConv es quién habla en la conversación respecto del cliente asociado (B22, B25).
type RolContactoConv struct {
	Rol        string                `json:"rol" enum:"titular,contacto,sin_confirmar" doc:"titular: el número es el teléfono o el móvil del titular, o un supervisor lo confirmó o pasó la verificación fuerte; contacto: un contacto del cliente en Argos; sin_confirmar: no se sabe quién habla (sin permisos hasta que un supervisor confirme)"`
	ContactoID *int64                `json:"contactoId" nullable:"true"`
	Nombre     *string               `json:"nombre" nullable:"true" doc:"Nombre del contacto en Argos"`
	Relacion   *string               `json:"relacion" nullable:"true" enum:"titular,familiar,inquilino,empleado,otro"`
	Permisos   PermisosContactoConv  `json:"permisos" doc:"Con rol titular, todos (siguen valiendo las reglas de verificación)"`
	Servicios  []ServicioContacto    `json:"servicios" doc:"«Sus servicios»: los del cliente a los que aplica el rol"`
	ACargo     []ACargoConv          `json:"aCargo" doc:"«Los que tiene a cargo»: servicios de otros clientes (A46)"`
	Reservado  string                `json:"reservado" doc:"Lo que solo pide el titular, con su identidad (A41): cambiar titular, plan, Wi-Fi, traspaso y baja"`
	Varios     bool                  `json:"varios" doc:"Sin cliente asociado: el número está en varios clientes. Se pide el número de servicio (elegir-cliente), sin mostrar datos de ninguno"`
	Elegir     *IntentosEleccionConv `json:"elegir,omitempty" doc:"Con varios: los intentos que quedan"`
}

// IntentosEleccionConv son los intentos que quedan para elegir el cliente.
type IntentosEleccionConv struct {
	Conversacion int `json:"conversacion" doc:"De 3 por conversación"`
	Numero       int `json:"numero" doc:"De 5 por número al día"`
}

// TextoReservadoTitular: lo que ningún permiso de contacto abre (§3.5).
const TextoReservadoTitular = "Cambiar titular, cambiar plan, cambiar el Wi-Fi, traspaso y baja: solo el titular, con su identidad"

func permisosConv(p argos.PermisosContacto) PermisosContactoConv {
	return PermisosContactoConv{Consultar: p.Consultar, Reportar: p.Reportar, Pagar: p.Pagar, Avisos: p.Avisos}
}

// contactoDeConv arma la ficha del contacto para el panel. nil: sin cliente y número de una sola
// persona (o de nadie).
func (m *Modulo) rolContactoDeConv(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (*RolContactoConv, error) {
	empresa := empresaDeConv(c)
	if c.ClienteID == nil {
		ps, err := personasDelNumero(ctx, q, empresa, c.Numero)
		if err != nil || len(ps) < 2 {
			return nil, err
		}
		quedan, err := m.intentosEleccion(ctx, q, c)
		if err != nil {
			return nil, err
		}
		return &RolContactoConv{Rol: "titular", Servicios: []ServicioContacto{}, ACargo: []ACargoConv{}, Reservado: TextoReservadoTitular, Varios: true, Elegir: &quedan}, nil
	}
	rol, err := m.rolDeConversacion(ctx, q, c)
	if err != nil {
		return nil, err
	}
	clientes := []int64{*c.ClienteID}
	for _, k := range rol.ACargo {
		clientes = append(clientes, k.ClienteID)
	}
	ss, err := q.ServiciosDeClientesInstantanea(ctx, mensajeriabd.ServiciosDeClientesInstantaneaParams{Clientes: clientes, Empresa: empresa})
	if err != nil {
		return nil, err
	}
	de := func(r rolEnCliente) []ServicioContacto {
		out := []ServicioContacto{}
		for _, s := range ss {
			if s.ClienteID == r.ClienteID && r.cubre(s.ID) {
				out = append(out, ServicioContacto{ID: s.ID, Codigo: s.Codigo, Nombre: s.Nombre})
			}
		}
		return out
	}
	p := *rol.Propio
	out := &RolContactoConv{Rol: "titular", Permisos: permisosConv(p.Permisos), Servicios: de(p), ACargo: []ACargoConv{}, Reservado: TextoReservadoTitular}
	switch {
	case p.SinRol:
		out.Rol = "sin_confirmar"
	case !p.esTitular():
		out.Rol, out.ContactoID, out.Nombre, out.Relacion = "contacto", p.ContactoID, &p.Nombre, &p.Relacion
	}
	for _, k := range rol.ACargo {
		out.ACargo = append(out.ACargo, ACargoConv{ContactoID: *k.ContactoID, Permisos: permisosConv(k.Permisos), Servicios: de(k)})
	}
	return out, nil
}

// datoContactoIA es lo que ve el modelo sobre quién escribe (nunca el nombre del servicio: es texto
// del cliente y no va al proveedor de IA, §3.7; el código sí).
func (m *Modulo) datoContactoIA(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (string, error) {
	f, err := m.rolContactoDeConv(ctx, q, c)
	if err != nil || f == nil {
		return "", err
	}
	if f.Varios {
		return "El número del cliente está registrado en varios clientes: no sabes cuál es. Pídele su número de servicio (como S-000412) " +
			"y usa elegir_cliente; nunca menciones nombres, direcciones ni otros datos de ningún cliente.", nil
	}
	var b strings.Builder
	codigos := func(ss []ServicioContacto) string {
		cs := make([]string, 0, len(ss))
		for _, s := range ss {
			if s.Codigo != "" {
				cs = append(cs, limpiarDato(s.Codigo, 20))
			}
		}
		if len(cs) == 0 {
			return "ninguno identificado"
		}
		return strings.Join(cs, ", ")
	}
	permite := func(p PermisosContactoConv) string {
		var si, no []string
		for _, x := range []struct {
			ok bool
			n  string
		}{{p.Consultar, "consultar saldo y estado"}, {p.Reportar, "reportar fallas"}, {p.Pagar, "pagar (boletas y promesas)"}} {
			if x.ok {
				si = append(si, x.n)
			} else {
				no = append(no, x.n)
			}
		}
		s := "Puede: " + strings.Join(si, ", ") + "."
		if len(si) == 0 {
			s = "No tiene permisos del titular."
		}
		if len(no) > 0 {
			s += " No puede: " + strings.Join(no, ", ") + "."
		}
		return s
	}
	if f.Rol == "sin_confirmar" {
		b.WriteString("No se sabe si quien escribe es el titular o un contacto: no des saldos, estados ni datos del cliente y no registres pagos; si lo pide, pasa a una persona. ")
	}
	if f.Rol == "contacto" {
		b.WriteString("Quien escribe NO es el titular: es un contacto del cliente (" + limpiarDato(derefTexto(f.Relacion), 20) + "). ")
		b.WriteString(permite(f.Permisos) + " Sus servicios: " + codigos(f.Servicios) + ". ")
	}
	for _, k := range f.ACargo {
		b.WriteString("Además tiene a su cargo servicios de otro cliente: " + codigos(k.Servicios) + ". Sobre ellos: " + permite(k.Permisos) + " ")
	}
	if b.Len() == 0 {
		return "", nil
	}
	b.WriteString("Cambiar titular, plan, Wi-Fi, traspaso o baja: solo el titular con su identidad; no lo ofrezcas.")
	return b.String(), nil
}

// Elegir entre varios clientes (§3.5, ajustes g, ac y ah) ----------------------------------------------

// Límites de elegir cliente: 3 intentos fallidos por conversación y 5 por número al día.
const (
	EleccionesPorConversacion = 3
	EleccionesPorNumeroDia    = 5
)

func (m *Modulo) intentosEleccion(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (IntentosEleccionConv, error) {
	nc, err := q.EleccionesClienteConv(ctx, c.ID)
	if err != nil {
		return IntentosEleccionConv{}, err
	}
	nn, err := q.EleccionesClienteNumero(ctx, mensajeriabd.EleccionesClienteNumeroParams{Empresa: empresaDeConv(c), Numero: c.Numero,
		Desde: m.ahora().Add(-24 * time.Hour)})
	if err != nil {
		return IntentosEleccionConv{}, err
	}
	return IntentosEleccionConv{Conversacion: max(0, EleccionesPorConversacion-int(nc)), Numero: max(0, EleccionesPorNumeroDia-int(nn))}, nil
}

// normalizarCodigo: «s 000412», «S-000412» y «000412» se comparan igual (solo letras y dígitos, en
// mayúsculas, sin ceros a la izquierda de la parte numérica).
func normalizarCodigo(s string) string {
	var letras, digitos strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		switch {
		case r >= '0' && r <= '9':
			digitos.WriteRune(r)
		case r >= 'A' && r <= 'Z' && digitos.Len() == 0:
			letras.WriteRune(r)
		}
	}
	d := strings.TrimLeft(digitos.String(), "0")
	if d == "" {
		return ""
	}
	l := letras.String()
	if l == "S" {
		l = ""
	}
	return l + d
}

// EntradaElegirCliente es POST /conversaciones/{id}/elegir-cliente.
type EntradaElegirCliente struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version,omitempty" minimum:"0" doc:"0 = sin control de versión (el asistente)"`
	// Referencia: el número de servicio que da el cliente (S-000412). Nunca se guarda ni se audita.
	Referencia string `json:"referencia" minLength:"1" maxLength:"40" doc:"El número de servicio que da el cliente (S-000412)"`
}

// MarshalJSON: sin la referencia.
func (e EntradaElegirCliente) MarshalJSON() ([]byte, error) {
	return []byte(`{"id":` + strconv.FormatInt(e.ID, 10) + `}`), nil
}

// EleccionCliente es el resultado: si coincidió (y la conversación quedó asociada) y los intentos que quedan.
type EleccionCliente struct {
	Coincide     bool                       `json:"coincide"`
	Intentos     IntentosEleccionConv       `json:"intentos"`
	Conversacion ConversacionClienteDetalle `json:"conversacion"`
}

// errEleccion: los intentos se agotaron.
func eleccionAgotada() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusTooManyRequests, "demasiados_intentos", "Demasiados intentos",
		"Se agotaron los intentos para identificar al cliente por su número de servicio: asócialo a mano después de confirmar con él")
}

// codigosPorPersona: para cada código (normalizado), las personas con ese número que pueden
// nombrarlo: un titular (su teléfono o móvil), sus servicios; un contacto, solo los suyos, como esa
// persona (personaDe: la persona a cargo solo si el número es el de ella). contacto: el contacto por el
// que se asociaría (nil si la persona es titular de ese número).
func codigosPorPersona(ctx context.Context, q *mensajeriabd.Queries, empresa int64, numero string) (map[string][]personaNumero, error) {
	titulares, err := titularesDelNumero(ctx, q, empresa, numero)
	if err != nil {
		return nil, err
	}
	ks, err := q.ContactosArgosDelNumero(ctx, mensajeriabd.ContactosArgosDelNumeroParams{Digitos: soloDigitos(numero), Empresa: empresa})
	if err != nil {
		return nil, err
	}
	// cliente dueño del servicio → (persona, rol en ese cliente)
	type via struct {
		persona personaNumero
		rol     rolEnCliente
	}
	vias := map[int64][]via{}
	clientes := []int64{}
	agregar := func(cliente int64, v via) {
		if !slices.Contains(clientes, cliente) {
			clientes = append(clientes, cliente)
		}
		vias[cliente] = append(vias[cliente], v)
	}
	for _, p := range titulares {
		agregar(p, via{persona: personaNumero{cliente: p}, rol: rolEnCliente{ClienteID: p}})
	}
	for _, x := range ks {
		if !x.ClienteEnEmpresa {
			continue
		}
		p := personaNumero{cliente: personaDe(x, titulares)}
		if p.cliente == x.ClienteID {
			id := x.ContactoID
			p.contacto = &id
		}
		agregar(x.ClienteID, via{persona: p, rol: rolDeFila(x)})
	}
	ss, err := q.ServiciosDeClientesInstantanea(ctx, mensajeriabd.ServiciosDeClientesInstantaneaParams{Clientes: clientes, Empresa: empresa})
	if err != nil {
		return nil, err
	}
	out := map[string][]personaNumero{}
	for _, s := range ss {
		cod := normalizarCodigo(s.Codigo)
		if cod == "" {
			continue
		}
		for _, v := range vias[s.ClienteID] {
			if !v.rol.cubre(s.ID) {
				continue
			}
			if !slices.ContainsFunc(out[cod], func(o personaNumero) bool { return o.cliente == v.persona.cliente }) {
				out[cod] = append(out[cod], v.persona)
			}
		}
	}
	return out, nil
}

func nuevoElegirCliente(m *Modulo) *auditoria.Caso[EntradaElegirCliente, EleccionCliente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaElegirCliente, EleccionCliente]{
		Accion: "conversaciones.elegir_cliente", Permiso: nucleo.PermConversacionesElegirCliente,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaElegirCliente) (EleccionCliente, error) {
			var out EleccionCliente
			d, err := m.accionConvDe(ctx, t, "conversaciones.elegir_cliente", in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.ClienteID != nil {
					return reglaConv("ya_asociada", "La conversación ya tiene un cliente asociado")
				}
				empresa := empresaDeConv(*c)
				personas, err := personasDelNumero(ctx, q, empresa, c.Numero)
				if err != nil {
					return err
				}
				if len(personas) < 2 {
					return reglaConv("sin_varios_clientes", "El número no está en varios clientes: asócialo como siempre")
				}
				quedan, err := m.intentosEleccion(ctx, q, *c)
				if err != nil {
					return err
				}
				if quedan.Conversacion == 0 || quedan.Numero == 0 {
					return eleccionAgotada()
				}
				cods, err := codigosPorPersona(ctx, q, empresa, c.Numero)
				if err != nil {
					return err
				}
				var elegido, contacto *int64
				if ps := cods[normalizarCodigo(in.Referencia)]; len(ps) == 1 {
					elegido, contacto = &ps[0].cliente, ps[0].contacto
				}
				// Cada intento queda anotado (sin la referencia) antes de decidir: un fallo cuenta.
				if err := q.InsertarEleccionCliente(ctx, mensajeriabd.InsertarEleccionClienteParams{EmpresaID: c.EmpresaID, ConversacionID: c.ID,
					Numero: c.Numero, Acierto: elegido != nil, ClienteID: elegido, UsuarioID: a.usuarioPtr(), CreadoEn: m.ahora()}); err != nil {
					return err
				}
				t.Anotar(map[string]any{"coincide": elegido != nil})
				if elegido == nil {
					quedan.Conversacion--
					quedan.Numero--
					out.Intentos = quedan
					return nil
				}
				out.Coincide, out.Intentos = true, quedan
				// Acertar asocia la conversación, pero no la verifica (§7.5): el número sí es de esa
				// persona (titular o contacto), como en la asociación automática.
				c.ClienteID, c.ServicioID, c.Asociacion = elegido, nil, nombrePtr("automatica")
				c.ClienteVerificado, c.VerificadoPor = true, nil
				if err := fijarContactoConv(ctx, q, c, contacto); err != nil {
					return err
				}
				nombre := NombreAsistenteIA
				if !a.asistente {
					nombre = derefTexto(actorNombre(ctx, q, a.usuario))
				}
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "cliente_asociado", ActorNombre: &nombre,
					EntidadID: nombrePtr(strconv.FormatInt(*elegido, 10))}); err != nil {
					return err
				}
				return t.Emitir("conversacion.cliente_asociado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "cliente_id": *elegido,
					"servicio_id": nil, "asociacion": "automatica", "origen": "referencia", "actor_id": a.usuarioPtr()})
			})
			out.Conversacion = d
			return out, err
		},
	})
}

// ArgosCuentaServicio es el estado de cuenta de un solo servicio (§2.1, ?servicio_id=, 0.10.0). Es
// opcional: sin él, un contacto con servicios acotados no ve el saldo (solo el estado del servicio).
type ArgosCuentaServicio interface {
	EstadoCuentaServicio(ctx context.Context, a argos.AtencionIris, clienteID int64, servicioID *int64) (argos.EstadoCuenta, error)
}

// errSinConsultar: quien habla es un contacto sin «consultar» (o sin un servicio que se pueda leer).
var errSinConsultar = errors.New("mensajeria: el contacto no puede consultar el estado de cuenta")

// estadoCuentaConv lee el estado de cuenta del cliente de la conversación según quién habla
// (§3.5, decisión 11 del PM): el titular, la cuenta del cliente; un contacto, solo con «consultar»,
// con X-Iris-Contacto y, si sus servicios están acotados, solo el de su servicio (?servicio_id=).
// Con un contacto que no puede, errSinConsultar (nunca se lee).
func (m *Modulo) estadoCuentaConv(ctx context.Context, a argos.AtencionIris, c mensajeriabd.MensajeriaConversacion) (argos.EstadoCuenta, error) {
	if c.ClienteID == nil || m.cfg.Argos == nil {
		return argos.EstadoCuenta{}, errSinConsultar
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return argos.EstadoCuenta{}, nucleo.ErrSinBD
	}
	q := mensajeriabd.New(bd)
	rol, err := m.rolDeConversacion(ctx, q, c)
	if err != nil {
		return argos.EstadoCuenta{}, err
	}
	p := *rol.Propio
	if p.esTitular() {
		a.Contacto = argos.ContactoTitular
		return m.cfg.Argos.EstadoCuentaCliente(ctx, a, *c.ClienteID)
	}
	if !p.permite(PermisoContactoConsultar) {
		return argos.EstadoCuenta{}, errSinConsultar
	}
	a.Contacto = p.cabecera()
	if p.Servicios == nil {
		return m.cfg.Argos.EstadoCuentaCliente(ctx, a, *c.ClienteID)
	}
	servicio := c.ServicioID
	if servicio == nil || !p.cubre(*servicio) {
		if servicio, err = servicioUnicoDe(ctx, q, empresaDeConv(c), p); err != nil {
			return argos.EstadoCuenta{}, err
		}
	}
	cs, ok := m.cfg.Argos.(ArgosCuentaServicio)
	if servicio == nil || !ok {
		return argos.EstadoCuenta{}, errSinConsultar
	}
	return cs.EstadoCuentaServicio(ctx, a, *c.ClienteID, servicio)
}

// Contactos de Iris unidos a un cliente (§3.5, B22) --------------------------------------------------

// OpCrearContactoArgos: POST /clientes/{id}/contactos (A42), la misma operación que usa comercial.
const OpCrearContactoArgos = "crear_contacto"

// ClaveContactoIris: iris:contacto:<cliente_id>:<contacto_iris_id> (§3.5).
func ClaveContactoIris(cliente, contacto int64) string {
	return "iris:contacto:" + strconv.FormatInt(cliente, 10) + ":" + strconv.FormatInt(contacto, 10)
}

// crearContactoEnArgos encola la creación en Argos del contacto de Iris que se unió al cliente, con
// la cola de pendientes (si Argos falla se reintenta; la unión en Iris no se pierde). Solo con un
// conector Argos que informe contrato ≥ 0.10.0; si no, nada (devuelve nil).
//
//   - Todos los permisos por defecto: consultar, reportar, pagar y avisos. Es la decisión 7 del PM
//     (contrato argosisp.md §10.5, «todo desde el inicio»), que deja superado el ajuste i de la misma
//     sección («nunca por encima de los permisos del agente»): los permisos no se recortan según quién
//     une el contacto. El titular recibe el aviso de Argos y puede quitarlos.
//   - Nunca titular (relación «otro»; un proveedor o un prospecto de Iris tampoco lo es).
//   - X-Iris-Actor = el agente (contactos.gestionar exige un actor; el asistente nunca une).
func (m *Modulo) crearContactoEnArgos(ctx context.Context, t *auditoria.Tx, a alcanceConv, f mensajeriabd.MensajeriaContactoIris, cliente int64) (*int64, error) {
	if a.asistente || a.usuario == 0 {
		return nil, nil
	}
	var contrato *string
	err := t.QueryRow(ctx, `SELECT contrato_remoto FROM integracion.conectores WHERE sistema = $1
		ORDER BY (estado = 'activo') DESC, id DESC LIMIT 1`, argos.Sistema).Scan(&contrato)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !conectorAlMenos(contrato, argos.ContratoContactosCliente) {
		return nil, nil
	}
	// Todos los permisos por defecto (decisión 7 del PM, §10.5): «todo desde el inicio», aunque el
	// titular no esté verificado en esta conversación; Argos avisa al titular con la opción de quitarlo.
	permisos := map[string]bool{"consultar": true, "reportar": true, "pagar": true, "avisos": true}
	cuerpo, err := json.Marshal(map[string]any{
		"nombre": f.Nombre, "relacion": "otro", "servicios": nil, "permisos": permisos,
		"telefonos": []map[string]any{{"numero": f.Telefono, "whatsapp": true, "principal": true}},
	})
	if err != nil {
		return nil, err
	}
	var aviso pendientes.Encolador
	if m.cfg.Encolar != nil {
		aviso = pendientes.Encolador(m.cfg.Encolar)
	}
	r, _, err := pendientes.Encolar(ctx, t, pendientes.Accion{EmpresaID: f.EmpresaID, Sistema: argos.Sistema, Operacion: OpCrearContactoArgos,
		IdempotencyKey: ClaveContactoIris(cliente, f.ID), Entidad: "contacto_iris", EntidadID: strconv.FormatInt(f.ID, 10), Metodo: "POST",
		Ruta: "/clientes/" + strconv.FormatInt(cliente, 10) + "/contactos", Cuerpo: cuerpo, Desde: m.ahora(),
		Actor: "usuario:" + strconv.FormatInt(a.usuario, 10)}, aviso)
	if err != nil {
		return nil, err
	}
	t.Anotar(map[string]any{"pendiente_contacto_argos": r.ID, "permisos_contacto": permisos})
	return &r.ID, nil
}
