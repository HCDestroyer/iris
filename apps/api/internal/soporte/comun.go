package soporte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Piezas comunes de las acciones sobre un ticket: tomarlo con candado verificando empresa,
// alcance, versión y estado; guardarlo con el SLA recalculado y auditado; historial y eventos.

// conIdempotencia identifica la operación por la credencial y su Idempotency-Key (las claves
// son por credencial, como en nucleo.MiddlewareIdempotencia). nil sin clave.
func conIdempotencia(ctx context.Context, clave string) *string {
	if clave == "" {
		return nil
	}
	a := contexto.ActorDe(ctx)
	credencial := fmt.Sprintf("%s:%d", a.Tipo, a.ID)
	if a.Tipo == contexto.ActorCliente {
		// El actor cliente no es un usuario (ID 0): la clave es por cliente de Argos.
		credencial = fmt.Sprintf("cliente:%d:%d", a.EmpresaID, a.ClienteID)
	}
	if a.TokenID != 0 {
		credencial = fmt.Sprintf("token:%d", a.TokenID)
	}
	v := credencial + "|" + clave
	return &v
}

// esSistemaOIntegracion: actores que no son una persona (cron, workers, Argos, Horus).
func esSistemaOIntegracion(ctx context.Context) bool {
	switch contexto.ActorDe(ctx).Tipo {
	case contexto.ActorSistema, contexto.ActorIntegracion:
		return true
	}
	return false
}

// noEncontrado oculta también los tickets de otra empresa o fuera del alcance de lectura.
func noEncontrado() *nucleo.Problema { return nucleo.NoEncontrado("El ticket no existe") }

func fueraDeAlcance(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "fuera_de_alcance", "Fuera de tu alcance", detalle)
}

// transicionInvalida: la acción no aplica en el estado actual (422).
func transicionInvalida(accion, estado string) *nucleo.Problema {
	return nucleo.ReglaNegocio("transicion_invalida",
		fmt.Sprintf("No se puede %s un ticket en estado %s", accion, estado))
}

func campoObligatorio(campo string) *nucleo.Problema {
	return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "Es obligatorio", Rule: "required"})
}

// texto recorta espacios y exige un largo entre min y max (en caracteres).
func texto(campo, valor string, minimo, maximo int) (string, error) {
	v := strings.TrimSpace(valor)
	n := len([]rune(v))
	if n == 0 && minimo > 0 {
		return "", campoObligatorio(campo)
	}
	if n < minimo || n > maximo {
		return "", nucleo.Validacion(nucleo.ErrorCampo{Field: campo,
			Message: fmt.Sprintf("Debe tener entre %d y %d caracteres", minimo, maximo), Rule: "length"})
	}
	return v, nil
}

// textoOpcional: nil o vacío = nil.
func textoOpcional(campo string, valor *string, maximo int) (*string, error) {
	if valor == nil || strings.TrimSpace(*valor) == "" {
		return nil, nil
	}
	v, err := texto(campo, *valor, 1, maximo)
	return &v, err
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptr[T any](v T) *T { return &v }

// filtroEmpresa dice qué tickets ve el contexto según cómo se resolvió la empresa activa
// (middleware de I1):
//   - EmpresaActiva: solo los de esa empresa (id).
//   - EmpresaSinConector: no hay empresas registradas; solo los guardados con empresa_id null
//     (id nil, todas false). Nunca «sin filtro».
//   - EmpresaNoResuelta: solo llega el actor sistema (auditoria.Caso corta a usuarios, tokens e
//     integraciones); con EmpresaDe = 0 ve todas.
//   - Cualquier otro estado (por elegir, sin empresa) no llega a la lógica; por si acaso, nada.
func filtroEmpresa(ctx context.Context) (todas bool, id *int64) {
	e := contexto.EmpresasDe(ctx)
	switch e.Estado {
	case contexto.EmpresaActiva:
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
	case contexto.EmpresaSinConector:
		return false, nil
	case contexto.EmpresaNoResuelta:
		if contexto.ActorDe(ctx).EsUsuario() {
			break
		}
		if activa := contexto.EmpresaDe(ctx); activa != 0 {
			return false, &activa
		}
		return true, nil
	case contexto.EmpresaPorElegir, contexto.EmpresaSinAsignar:
	}
	return false, ptr(int64(0)) // ninguna empresa tiene id 0: no ve nada
}

// enEmpresa: el ticket es de la empresa que ve el contexto (ver filtroEmpresa).
func enEmpresa(ctx context.Context, f soportebd.SoporteTicket) bool {
	todas, id := filtroEmpresa(ctx)
	return todas || mismoID(f.EmpresaID, id)
}

// empresaActiva devuelve la empresa activa para la configuración (categorías y SLA): las
// globales (empresa_id null) más las de esa empresa. nil = solo las globales (sin conector,
// sistema).
func empresaActiva(ctx context.Context) *int64 {
	if todas, id := filtroEmpresa(ctx); !todas && id != nil && *id != 0 {
		return id
	}
	return nil
}

// dentroDeAlcance: el permiso cubre el ticket si es global, si el ticket está en una de sus
// zonas o cuadrillas, o si está asignado al actor.
func dentroDeAlcance(ctx context.Context, permiso nucleo.Permiso, f soportebd.SoporteTicket) bool {
	global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(permiso))
	if global {
		return true
	}
	if f.ZonaID != nil && slices.Contains(zonas, *f.ZonaID) {
		return true
	}
	if f.CuadrillaID != nil && slices.Contains(cuadrillas, *f.CuadrillaID) {
		return true
	}
	actor := contexto.ActorDe(ctx)
	return actor.EsUsuario() && f.AsignadoA != nil && *f.AsignadoA == actor.ID
}

// tomar lee el ticket con FOR UPDATE y verifica empresa, alcance del permiso y versión (si se
// envía). Fija la entidad y la empresa de la auditoría y de los eventos.
func tomar(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, id int64, permiso nucleo.Permiso, version *int32) (soportebd.SoporteTicket, error) {
	t.FijarEntidad("ticket", id)
	f, err := q.TicketPorIDParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrado()
	}
	if err != nil {
		return f, err
	}
	if err := verificarAcceso(ctx, permiso, f); err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if version != nil {
		if err := nucleo.ExigirVersion(*version, f.Version, "ticket", id); err != nil {
			return f, err
		}
	}
	return f, nil
}

// verificarAcceso: fuera de la empresa activa o del alcance de lectura, 404 (no revela que
// existe); visible pero fuera del alcance del permiso de la acción, 403.
//
// El actor cliente del portal solo alcanza los tickets de su cliente en su empresa (lo demás,
// 404), sin importar el permiso.
func verificarAcceso(ctx context.Context, permiso nucleo.Permiso, f soportebd.SoporteTicket) error {
	if a := contexto.ActorDe(ctx); a.Tipo == contexto.ActorCliente {
		if esDelCliente(a, f) {
			return nil
		}
		return noEncontrado()
	}
	if !enEmpresa(ctx, f) {
		return noEncontrado()
	}
	if dentroDeAlcance(ctx, permiso, f) {
		return nil
	}
	if permiso != nucleo.PermTicketsVer && dentroDeAlcance(ctx, nucleo.PermTicketsVer, f) {
		return fueraDeAlcance(fmt.Sprintf("Tu permiso %s no cubre la zona o cuadrilla de este ticket", permiso))
	}
	return noEncontrado()
}

// esDelCliente: el ticket es del cliente de la sesión del portal y de su empresa.
func esDelCliente(a contexto.Actor, f soportebd.SoporteTicket) bool {
	return a.EsCliente() && f.ClienteID != nil && *f.ClienteID == a.ClienteID &&
		f.EmpresaID != nil && *f.EmpresaID == a.EmpresaID
}

// exigirEstado: la acción solo aplica desde los estados dados.
func exigirEstado(f soportebd.SoporteTicket, accion string, permitidos ...string) error {
	if !slices.Contains(permitidos, f.Estado) {
		return transicionInvalida(accion, f.Estado)
	}
	return nil
}

// guardar recalcula el SLA, escribe el ticket (control de versión) y registra los cambios.
// Un cambio de estado descarta la propuesta de Horus (resolver o cerrar) pendiente.
func guardar(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, antes, despues soportebd.SoporteTicket, cal *Calendario) (soportebd.SoporteTicket, error) {
	aplicarSLA(&despues, cal)
	if despues.Estado != antes.Estado {
		despues.Propuesta, despues.PropuestaEn, despues.PropuestaMotivo = nil, nil, nil
	}
	f, err := q.GuardarTicket(ctx, soportebd.GuardarTicketParams{
		Estado: despues.Estado, Prioridad: despues.Prioridad, AsignadoA: despues.AsignadoA, CuadrillaID: despues.CuadrillaID,
		NivelEscalamiento: despues.NivelEscalamiento, SlaRespuestaMin: despues.SlaRespuestaMin,
		SlaResolucionMin: despues.SlaResolucionMin, PausaSeg: despues.PausaSeg,
		RiesgoRespuesta: despues.RiesgoRespuesta, VenceRespuesta: despues.VenceRespuesta,
		RiesgoResolucion: despues.RiesgoResolucion, VenceResolucion: despues.VenceResolucion,
		PrimeraRespuestaEn: despues.PrimeraRespuestaEn, EnEsperaDesde: despues.EnEsperaDesde,
		ResueltoEn: despues.ResueltoEn, CerradoEn: despues.CerradoEn, FusionadoEnID: despues.FusionadoEnID,
		Propuesta: despues.Propuesta, PropuestaEn: despues.PropuestaEn, PropuestaMotivo: despues.PropuestaMotivo,
		ID: antes.ID, Version: antes.Version,
	})
	if err != nil {
		return f, nucleo.VerificarVersion(err, "ticket", antes.ID)
	}
	t.RegistrarCambios("ticket", f.ID, antes, f)
	return f, nil
}

// historial escribe una entrada en soporte.ticket_eventos con el actor de la ejecución.
func historial(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, f soportebd.SoporteTicket, tipo string, de, a, motivo *string, datos map[string]any) error {
	if datos == nil {
		datos = map[string]any{}
	}
	crudo, err := json.Marshal(datos)
	if err != nil {
		return err
	}
	actor := t.Actor()
	var actorID *int64
	if actor.ID != 0 {
		actorID = &actor.ID
	}
	return q.CrearEventoTicket(ctx, soportebd.CrearEventoTicketParams{
		TicketID: f.ID, EmpresaID: f.EmpresaID, Tipo: tipo, De: de, A: a, Motivo: motivo, Datos: crudo,
		ActorTipo: string(actor.Tipo), ActorID: actorID, ActorNombre: actor.Nombre, RequestID: contexto.RequestIDDe(ctx),
	})
}

// cambioDeEstado registra el historial y emite ticket.estado_cambiado si el estado cambió.
func cambioDeEstado(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, antes, despues soportebd.SoporteTicket, motivo *string) error {
	if antes.Estado == despues.Estado {
		return nil
	}
	if err := historial(ctx, t, q, despues, "estado", &antes.Estado, &despues.Estado, motivo, nil); err != nil {
		return err
	}
	return t.Emitir("ticket.estado_cambiado", map[string]any{
		"ticket_id": despues.ID, "de": antes.Estado, "a": despues.Estado, "motivo": motivo,
	})
}

// mensaje agrega un mensaje del actor (o del sistema) al ticket.
func mensaje(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, f soportebd.SoporteTicket, publico bool, cuerpo string) (soportebd.SoporteTicketMensaje, error) {
	return mensajeIdempotente(ctx, t, q, f, publico, cuerpo, nil)
}

// mensajeIdempotente agrega el mensaje guardando la clave de la operación (ver conIdempotencia).
func mensajeIdempotente(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, f soportebd.SoporteTicket, publico bool, cuerpo string, idem *string) (soportebd.SoporteTicketMensaje, error) {
	actor := t.Actor()
	p := soportebd.CrearMensajeParams{
		TicketID: f.ID, EmpresaID: f.EmpresaID, Publico: publico, AutorTipo: "sistema",
		AutorNombre: actor.Nombre, Canal: string(contexto.CanalDe(ctx)), Cuerpo: cuerpo, Idempotencia: idem,
	}
	switch {
	case actor.EsUsuario():
		p.AutorTipo, p.AutorID = "usuario", &actor.ID
	case actor.Tipo == contexto.ActorCliente:
		p.AutorTipo = "cliente"
	}
	return q.CrearMensaje(ctx, p)
}

// mensajeSistema agrega un mensaje de Iris (autor sistema), aunque la acción la haga una
// persona: avisos automáticos (vinculación a un incidente, notas de Horus, aviso a los hijos).
func mensajeSistema(ctx context.Context, q *soportebd.Queries, f soportebd.SoporteTicket, publico bool, cuerpo string) (soportebd.SoporteTicketMensaje, error) {
	return q.CrearMensaje(ctx, soportebd.CrearMensajeParams{
		TicketID: f.ID, EmpresaID: f.EmpresaID, Publico: publico, AutorTipo: "sistema", AutorNombre: "Iris",
		Canal: string(contexto.CanalDe(ctx)), Cuerpo: cuerpo,
	})
}

// mensajeRepetido busca el mensaje que dejó una ejecución anterior de la misma operación (la
// clave se reclamó y la petición se repite): si existe, la acción no se vuelve a aplicar. El
// actor debe poder ver el ticket.
func mensajeRepetido(ctx context.Context, q *soportebd.Queries, id int64, idem *string, ahora time.Time) (*TicketConMensaje, error) {
	if idem == nil {
		return nil, nil
	}
	msj, err := q.MensajePorIdempotencia(ctx, soportebd.MensajePorIdempotenciaParams{TicketID: id, Idempotencia: idem})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := leer(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return &TicketConMensaje{Ticket: aTicket(f, ahora), Mensaje: aMensaje(msj)}, nil
}

// abrirSiNuevo: la primera atención (asignar o responder) pasa el ticket de nuevo a abierto.
func abrirSiNuevo(f *soportebd.SoporteTicket) {
	if f.Estado == EstadoNuevo {
		f.Estado = EstadoAbierto
	}
}

// marcarRespuesta fija la primera respuesta (cumple el SLA de respuesta).
func marcarRespuesta(f *soportebd.SoporteTicket, ahora time.Time) {
	if f.PrimeraRespuestaEn == nil {
		f.PrimeraRespuestaEn = &ahora
	}
}

// reactivarPorCliente aplica la regla de estado de un mensaje del cliente (correo o portal) y
// guarda el ticket: en_espera → abierto (reanuda; corre el SLA de resolución) y resuelto →
// abierto (reabre; el tiempo resuelto cuenta como pausa); nuevo y abierto no cambian (no se
// guarda: la versión no sube). No cuenta
// como primera respuesta. Devuelve el motivo del cambio de estado (nil si no cambió); quien
// llama agrega el mensaje y registra el cambio con cambioDeEstado.
func (m *Modulo) reactivarPorCliente(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, antes soportebd.SoporteTicket, canal string) (soportebd.SoporteTicket, *string, error) {
	ahora := m.ahora()
	cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
	if err != nil {
		return antes, nil, err
	}
	despues := antes
	var motivo *string
	switch antes.Estado {
	case EstadoEnEspera:
		salirDeEspera(&despues, cal, ahora)
		despues.Estado, motivo = EstadoAbierto, ptr("El cliente respondió por "+canal)
	case EstadoResuelto:
		acumularPausa(&despues, cal, antes.ResueltoEn, ahora)
		despues.Estado, despues.ResueltoEn, motivo = EstadoAbierto, nil, ptr("El cliente respondió por "+canal+" a un ticket resuelto")
	}
	if despues.Estado == antes.Estado {
		return antes, nil, nil // nuevo o abierto: el ticket no cambia (ni su versión)
	}
	f, err := guardar(ctx, t, q, antes, despues, cal)
	return f, motivo, err
}
