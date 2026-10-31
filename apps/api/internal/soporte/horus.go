package soporte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
)

// Tickets desde Horus (contrato horus.md §2). El conector (I1) publica los webhooks como eventos
// internos horus.*; soporte se suscribe (cmd/worker) y decide con estos casos de uso (actor
// sistema):
//
//   - horus.router_estado_cambiado / horus.servicio_infra_cambiado con «caido»: abre un ticket
//     maestro incidente_red (prioridad alta, origen horus) por router (o servicio de
//     infraestructura) y empresa; si ya hay uno no cerrado, le agrega una nota (y descarta una
//     propuesta de resolver pendiente); si estaba resuelto hace menos de VentanaReapertura, lo
//     reabre; si se resolvió antes, lo cierra y abre uno nuevo que lo menciona. Un cerrado no
//     se reabre (cerrado es final): se abre uno nuevo.
//   - Con «arriba»: nota y propuesta de resolver en el maestro abierto (ticket.resolucion_propuesta).
//     No se resuelve solo: un agente confirma con tickets.resolver, que avisa a los hijos.
//   - horus.hallazgo_abierto: solo si la categoría tiene una regla activa (soporte.reglas_hallazgos):
//     ticket del cliente resuelto por Argos (o sin cliente, con el motivo), uno por hallazgo.
//   - horus.hallazgo_resuelto: nota en el ticket del hallazgo y propuesta de resolver (abierto)
//     o cerrar (resuelto).
//
// Idempotencia: soporte.horus_eventos, una fila por entrante_id reclamada en la misma
// transacción (un evento repetido, o el mismo en dos workers a la vez, no hace nada dos veces).
// Dos eventos distintos de un mismo router se serializan con un advisory lock por router y
// empresa, y el índice único parcial de maestros no cerrados lo garantiza en la base.
//
// Empresa: la del evento. Sin empresas registradas (sin conector) los tickets van con empresa
// null. Con empresas registradas, un evento sin empresa (o con una que Iris no tiene) no crea
// nada: queda en horus_eventos y se emite soporte.horus_descartado.

// VentanaReapertura: un maestro resuelto hace menos que esto se reabre si el router vuelve a
// caer; si es más viejo, se cierra y se abre uno nuevo.
const VentanaReapertura = 24 * time.Hour

// Resultados de un evento de Horus (soporte.horus_eventos.resultado).
const (
	HorusRepetido         = "repetido"
	HorusObsoleto         = "obsoleto"
	HorusMaestroCreado    = "maestro_creado"
	HorusMaestroReabierto = "maestro_reabierto"
	HorusMaestroExistente = "maestro_existente"
	HorusPropuesta        = "propuesta"
	HorusSinMaestro       = "sin_maestro_abierto"
	HorusTicketCreado     = "ticket_creado"
	HorusTicketExistente  = "ticket_existente"
	HorusSinRegla         = "sin_regla"
	HorusSinTicket        = "sin_ticket"
	HorusNota             = "nota"
	HorusSinEmpresa       = "sin_empresa"
	HorusEmpresaNoReg     = "empresa_no_registrada"
	HorusError            = "error"
)

// ResultadoHorus dice qué hizo soporte con un evento de Horus.
type ResultadoHorus struct {
	Resultado string `json:"resultado"`
	TicketID  *int64 `json:"ticketId"`
}

// EntradaIncidenteHorus es un cambio de estado de un router (tipo router) o de un servicio de
// infraestructura (tipo infra).
type EntradaIncidenteHorus struct {
	EntranteID int64     `json:"entranteId"`
	Evento     string    `json:"evento"`
	Tipo       string    `json:"tipo"` // router | infra
	RouterID   *int64    `json:"routerId"`
	InfraID    *string   `json:"infraId"`
	InfraTipo  string    `json:"infraTipo"`
	Nombre     string    `json:"nombre"`
	Estado     string    `json:"estado"` // caido | arriba
	OcurridoEn time.Time `json:"ocurridoEn"`
	EmpresaID  *int64    `json:"empresaId"`
}

// EntradaDescartarHorus registra un evento que no se pudo aplicar por una regla de negocio.
type EntradaDescartarHorus struct {
	EntranteID int64  `json:"entranteId"`
	Evento     string `json:"evento"`
	EmpresaID  *int64 `json:"empresaId"`
	Motivo     string `json:"motivo"`
}

// auditarHorus: se auditan los eventos que hicieron algo (los repetidos y los que no aplican,
// no: pueden ser muchos).
func auditarHorus(r ResultadoHorus) bool {
	switch r.Resultado {
	case HorusRepetido, HorusObsoleto, HorusSinRegla, HorusSinTicket, HorusSinMaestro:
		return false
	}
	return true
}

// reclamarHorus registra el evento; false = ya se procesó (se devuelve lo que se hizo).
func reclamarHorus(ctx context.Context, q *soportebd.Queries, entrante int64, evento string, empresa *int64) (bool, ResultadoHorus, error) {
	n, err := q.ReclamarEventoHorus(ctx, soportebd.ReclamarEventoHorusParams{EntranteID: entrante, Evento: evento, EmpresaID: empresa})
	if err != nil || n == 1 {
		return n == 1, ResultadoHorus{}, err
	}
	previo, err := q.EventoHorus(ctx, entrante)
	if err != nil {
		return false, ResultadoHorus{}, err
	}
	return false, ResultadoHorus{Resultado: HorusRepetido, TicketID: previo.TicketID}, nil
}

func cerrarHorus(ctx context.Context, q *soportebd.Queries, entrante int64, r ResultadoHorus, detalle *string) (ResultadoHorus, error) {
	return r, q.CerrarEventoHorus(ctx, soportebd.CerrarEventoHorusParams{
		EntranteID: entrante, Resultado: r.Resultado, TicketID: r.TicketID, Detalle: detalle,
	})
}

// empresaHorus decide la empresa del ticket: sin empresas registradas, null (sin conector);
// con empresas, la del evento, que debe estar registrada. motivo ≠ "" = no se crea nada.
func empresaHorus(ctx context.Context, q *soportebd.Queries, pedida *int64) (empresa *int64, motivo string, err error) {
	hay, err := q.HayEmpresas(ctx)
	if err != nil || !hay {
		return nil, "", err
	}
	if pedida == nil {
		return nil, HorusSinEmpresa, nil
	}
	existe, err := q.EmpresaRegistrada(ctx, *pedida)
	if err != nil {
		return nil, "", err
	}
	if !existe {
		return nil, HorusEmpresaNoReg, nil
	}
	return pedida, "", nil
}

// descartar cierra el evento sin ticket y emite la alerta interna soporte.horus_descartado.
func descartar(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, entrante int64, evento string, empresa *int64, motivo, detalle string) (ResultadoHorus, error) {
	t.FijarEmpresa(deref(empresa))
	t.Anotar(map[string]any{"descartado": motivo})
	r, err := cerrarHorus(ctx, q, entrante, ResultadoHorus{Resultado: motivo}, &detalle)
	if err != nil {
		return r, err
	}
	return r, t.Emitir("soporte.horus_descartado", map[string]any{
		"evento": evento, "entrante_id": entrante, "empresa_id": empresa, "motivo": motivo, "detalle": detalle,
	})
}

func detalleSinEmpresa(motivo string, empresa *int64) string {
	if motivo == HorusEmpresaNoReg && empresa != nil {
		return fmt.Sprintf("La empresa %d no está registrada en Iris", *empresa)
	}
	return "El evento no trae empresa y hay empresas registradas: no se sabe de quién es"
}

// enOrden aplica el orden de los eventos de Horus (que reintenta y puede desordenar): guarda el
// último estado y ocurrido_en por clave (soporte.horus_estados) y devuelve false si el evento
// tiene un ocurrido_en igual o anterior al guardado (obsoleto: no hace nada). Quien llama tiene
// el advisory lock de la clave.
func enOrden(ctx context.Context, q *soportebd.Queries, clave string, empresa *int64, estado string, ocurrido time.Time, entrante int64) (bool, error) {
	ocurrido = ocurrido.UTC().Truncate(time.Microsecond)
	previo, err := q.EstadoHorus(ctx, clave)
	if err == nil && !ocurrido.After(previo.OcurridoEn) {
		return false, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	return true, q.GuardarEstadoHorus(ctx, soportebd.GuardarEstadoHorusParams{
		Clave: clave, EmpresaID: empresa, Estado: estado, OcurridoEn: ocurrido, EntranteID: entrante,
	})
}

func obsoleto(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, entrante int64) (ResultadoHorus, error) {
	t.Anotar(map[string]any{"entrante_id": entrante, "resultado": HorusObsoleto})
	return cerrarHorus(ctx, q, entrante, ResultadoHorus{Resultado: HorusObsoleto}, ptr("ocurrido_en igual o anterior al último evento conocido"))
}

// proponer deja la propuesta (resolver o cerrar) en el ticket con una nota interna y emite
// ticket.resolucion_propuesta. Si ya estaba la misma propuesta, solo la nota.
func (m *Modulo) proponer(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, antes soportebd.SoporteTicket, accion, motivo string, entrante int64) (soportebd.SoporteTicket, error) {
	if _, err := mensajeSistema(ctx, q, antes, false, motivo+" Propuesta: "+accion+" el ticket (un agente lo confirma)."); err != nil {
		return antes, err
	}
	// La misma propuesta de Horus ya estaba: solo la nota. Una del cierre de una orden se
	// reemplaza: Horus manda (la suya no se pierde).
	if antes.Propuesta != nil && *antes.Propuesta == accion && origenPropuesta(antes) == PropuestaDeHorus {
		return antes, nil
	}
	cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
	if err != nil {
		return antes, err
	}
	ahora := m.ahora()
	despues := antes
	despues.Propuesta, despues.PropuestaEn, despues.PropuestaMotivo = &accion, &ahora, &motivo
	despues.PropuestaOrigen, despues.PropuestaOrdenID = ptr(PropuestaDeHorus), nil
	f, err := guardar(ctx, t, q, antes, despues, cal)
	if err != nil {
		return antes, err
	}
	return f, t.Emitir("ticket.resolucion_propuesta", map[string]any{
		"ticket_id": f.ID, "accion": accion, "motivo": motivo, "origen": PropuestaDeHorus, "entrante_id": entrante, "orden_id": nil,
	})
}

func abierto(estado string) bool {
	return estado == EstadoNuevo || estado == EstadoAbierto || estado == EstadoEnEspera
}

func (in EntradaIncidenteHorus) descripcion() string {
	if in.Tipo == "router" {
		if in.Nombre != "" {
			return fmt.Sprintf("router %s (#%d)", in.Nombre, deref(in.RouterID))
		}
		return fmt.Sprintf("router #%d", deref(in.RouterID))
	}
	nombre := derefStr(in.InfraID)
	if in.Nombre != "" {
		nombre = in.Nombre
	}
	if in.InfraTipo != "" {
		return in.InfraTipo + " " + nombre
	}
	return "servicio de infraestructura " + nombre
}

func (m *Modulo) nuevoIncidenteHorus() *auditoria.Caso[EntradaIncidenteHorus, ResultadoHorus] {
	return auditoria.Nuevo(auditoria.Caso[EntradaIncidenteHorus, ResultadoHorus]{
		Accion:       "tickets.horus_incidente",
		Acceso:       auditoria.Autenticado,
		AuditarExito: auditarHorus,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaIncidenteHorus) (ResultadoHorus, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoHorus{}, err
			}
			if (in.Tipo != "router" || in.RouterID == nil) && (in.Tipo != "infra" || in.InfraID == nil) ||
				(in.Estado != horus.EstadoCaido && in.Estado != horus.EstadoArriba) {
				return ResultadoHorus{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "tipo", Message: "Incidente de Horus inválido", Rule: "horus"})
			}
			q := soportebd.New(t)
			t.FijarEntidad("horus_entrante", in.EntranteID)
			nuevo, previo, err := reclamarHorus(ctx, q, in.EntranteID, in.Evento, in.EmpresaID)
			if err != nil || !nuevo {
				return previo, err
			}
			empresa, motivo, err := empresaHorus(ctx, q, in.EmpresaID)
			if err != nil {
				return ResultadoHorus{}, err
			}
			if motivo != "" {
				return descartar(ctx, t, q, in.EntranteID, in.Evento, in.EmpresaID, motivo, detalleSinEmpresa(motivo, in.EmpresaID))
			}
			t.FijarEmpresa(deref(empresa))
			ref := strconv.FormatInt(deref(in.RouterID), 10)
			if in.Tipo == "infra" {
				ref = *in.InfraID
			}
			clave := fmt.Sprintf("%s:%d:%s", in.Tipo, deref(empresa), ref)
			if err := q.BloquearIncidente(ctx, "soporte.incidente:"+clave); err != nil {
				return ResultadoHorus{}, err
			}
			// Un caído más viejo que el último «arriba» (o un arriba más viejo que la caída que
			// abrió el maestro) es obsoleto: no abre ni propone nada.
			if ok, err := enOrden(ctx, q, clave, empresa, in.Estado, in.OcurridoEn, in.EntranteID); err != nil || !ok {
				if err != nil {
					return ResultadoHorus{}, err
				}
				return obsoleto(ctx, t, q, in.EntranteID)
			}
			ultimo, err := q.UltimoMaestro(ctx, soportebd.UltimoMaestroParams{Maestro: in.Tipo, RouterID: in.RouterID, InfraID: in.InfraID, EmpresaID: empresa})
			hay := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ResultadoHorus{}, err
			}
			cuando := in.OcurridoEn.UTC().Format(time.RFC3339)
			que := in.descripcion()
			var r ResultadoHorus
			if in.Estado == horus.EstadoArriba {
				r, err = m.incidenteArriba(ctx, t, q, in, ultimo, hay, que, cuando)
			} else {
				r, err = m.incidenteCaido(ctx, t, q, in, empresa, ultimo, hay, que, cuando)
			}
			if err != nil {
				return ResultadoHorus{}, err
			}
			if r.TicketID != nil {
				t.FijarEntidad("ticket", *r.TicketID)
			}
			t.Anotar(map[string]any{"entrante_id": in.EntranteID, "resultado": r.Resultado})
			return cerrarHorus(ctx, q, in.EntranteID, r, nil)
		},
	})
}

func (m *Modulo) incidenteArriba(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, in EntradaIncidenteHorus, ultimo soportebd.SoporteTicket, hay bool, que, cuando string) (ResultadoHorus, error) {
	if !hay || !abierto(ultimo.Estado) {
		return ResultadoHorus{Resultado: HorusSinMaestro}, nil
	}
	f, err := m.proponer(ctx, t, q, ultimo, "resolver", fmt.Sprintf("Horus: %s volvió a estar arriba (%s).", que, cuando), in.EntranteID)
	if err != nil {
		return ResultadoHorus{}, err
	}
	return ResultadoHorus{Resultado: HorusPropuesta, TicketID: &f.ID}, nil
}

func (m *Modulo) incidenteCaido(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, in EntradaIncidenteHorus, empresa *int64, ultimo soportebd.SoporteTicket, hay bool, que, cuando string) (ResultadoHorus, error) {
	ahora := m.ahora()
	nota := fmt.Sprintf("Horus: %s caído (%s).", que, cuando)
	anterior := ""
	if hay {
		switch {
		case abierto(ultimo.Estado):
			// Sigue abierto: nota y, si había propuesta de resolver, se descarta.
			if _, err := mensajeSistema(ctx, q, ultimo, false, nota); err != nil {
				return ResultadoHorus{}, err
			}
			if ultimo.Propuesta != nil {
				cal, err := cargarCalendario(ctx, q, ultimo.CalendarioID)
				if err != nil {
					return ResultadoHorus{}, err
				}
				despues := ultimo
				despues.Propuesta, despues.PropuestaEn, despues.PropuestaMotivo = nil, nil, nil
				despues.PropuestaOrigen, despues.PropuestaOrdenID = nil, nil
				if _, err := guardar(ctx, t, q, ultimo, despues, cal); err != nil {
					return ResultadoHorus{}, err
				}
			}
			return ResultadoHorus{Resultado: HorusMaestroExistente, TicketID: &ultimo.ID}, nil
		case ultimo.Estado == EstadoResuelto && ultimo.ResueltoEn != nil && in.OcurridoEn.Sub(*ultimo.ResueltoEn) <= VentanaReapertura:
			// La ventana se mide con la hora de la caída según Horus, no con la de proceso.
			cal, err := cargarCalendario(ctx, q, ultimo.CalendarioID)
			if err != nil {
				return ResultadoHorus{}, err
			}
			despues := ultimo
			acumularPausa(&despues, cal, ultimo.ResueltoEn, ahora)
			despues.Estado, despues.ResueltoEn = EstadoAbierto, nil
			f, err := guardar(ctx, t, q, ultimo, despues, cal)
			if err != nil {
				return ResultadoHorus{}, err
			}
			if _, err := mensajeSistema(ctx, q, f, false, nota+" Se reabre el incidente (resuelto hace menos de 24 h)."); err != nil {
				return ResultadoHorus{}, err
			}
			if err := cambioDeEstado(ctx, t, q, ultimo, f, ptr("Horus: volvió a caer")); err != nil {
				return ResultadoHorus{}, err
			}
			if err := m.encolarAviso(ctx, t, f, AvisoReabierto, ""); err != nil {
				return ResultadoHorus{}, err
			}
			return ResultadoHorus{Resultado: HorusMaestroReabierto, TicketID: &f.ID}, nil
		case ultimo.Estado == EstadoResuelto:
			// Resuelto hace más de la ventana: se cierra y la caída nueva es otro incidente.
			cal, err := cargarCalendario(ctx, q, ultimo.CalendarioID)
			if err != nil {
				return ResultadoHorus{}, err
			}
			f, err := guardar(ctx, t, q, ultimo, cerrar(ultimo, cal, ahora), cal)
			if err != nil {
				return ResultadoHorus{}, err
			}
			if err := cambioDeEstado(ctx, t, q, ultimo, f, ptr("Horus: nueva caída; se abre otro incidente")); err != nil {
				return ResultadoHorus{}, err
			}
		}
		anterior = ultimo.Numero
	}
	var categoria *int64
	if c, err := q.CategoriaPorCodigo(ctx, soportebd.CategoriaPorCodigoParams{Codigo: "incidente_red", EmpresaID: empresa}); err == nil {
		categoria = &c.ID
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ResultadoHorus{}, err
	}
	if anterior != "" {
		nota += " Incidente anterior: " + anterior + "."
	}
	maestro := in.Tipo
	nombre := in.Nombre
	inc := &datosIncidente{maestro: &maestro, routerID: in.RouterID, infraID: in.InfraID}
	if nombre != "" {
		inc.nombre = &nombre
	}
	asunto := "Caída de " + que
	f, err := m.crearTicket(ctx, t, EntradaCrearTicket{
		EmpresaID: empresa, CategoriaID: categoria, Prioridad: "alta", Origen: "horus", Asunto: recortarRunas(asunto, 200),
	}, opcionesCrear{incidente: inc, nota: nota})
	if err != nil {
		return ResultadoHorus{}, err
	}
	return ResultadoHorus{Resultado: HorusMaestroCreado, TicketID: &f.ID}, nil
}

func recortarRunas(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}

// Hallazgos ------------------------------------------------------------------------------

// motivosSinCliente traduce motivo_sin_cliente (Horus/Argos) para la descripción del ticket.
var motivosSinCliente = map[string]string{
	"sin_ip":                 "el hallazgo no trae IP",
	"ip_invalida":            "la IP del hallazgo no es válida",
	"conector_argos_apagado": "el conector con Argos está apagado",
	"ip_sin_cliente":         "Argos no tiene un cliente con esa IP",
	"argos_no_disponible":    "Argos no respondió",
	"argos_rechazo":          "Argos rechazó la consulta",
	"otra_empresa":           "la IP es de un cliente de otra empresa",
	"empresa_desconocida":    "no se sabe de qué empresa es el hallazgo",
}

func (m *Modulo) nuevoHallazgoAbierto() *auditoria.Caso[horus.HallazgoAbierto, ResultadoHorus] {
	return auditoria.Nuevo(auditoria.Caso[horus.HallazgoAbierto, ResultadoHorus]{
		Accion:       "tickets.horus_hallazgo",
		Acceso:       auditoria.Autenticado,
		AuditarExito: auditarHorus,
		Logica: func(ctx context.Context, t *auditoria.Tx, in horus.HallazgoAbierto) (ResultadoHorus, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoHorus{}, err
			}
			if strings.TrimSpace(in.HallazgoID) == "" || strings.TrimSpace(in.Categoria) == "" {
				return ResultadoHorus{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "hallazgo_id", Message: "Hallazgo sin id o sin categoría", Rule: "horus"})
			}
			q := soportebd.New(t)
			t.FijarEntidad("horus_entrante", in.EntranteID)
			nuevo, previo, err := reclamarHorus(ctx, q, in.EntranteID, horus.EventoHallazgoAbierto, in.EmpresaID)
			if err != nil || !nuevo {
				return previo, err
			}
			categoria := strings.ToLower(strings.TrimSpace(in.Categoria))
			// La regla se busca primero: un hallazgo sin regla no abre ticket y no alerta.
			empresa, motivo, err := empresaHorus(ctx, q, in.EmpresaID)
			if err != nil {
				return ResultadoHorus{}, err
			}
			regla, err := q.ReglaDeHallazgo(ctx, soportebd.ReglaDeHallazgoParams{CategoriaHorus: categoria, EmpresaID: in.EmpresaID})
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !regla.Activa) {
				return cerrarHorus(ctx, q, in.EntranteID, ResultadoHorus{Resultado: HorusSinRegla}, &categoria)
			}
			if err != nil {
				return ResultadoHorus{}, err
			}
			if motivo != "" {
				return descartar(ctx, t, q, in.EntranteID, horus.EventoHallazgoAbierto, in.EmpresaID, motivo, detalleSinEmpresa(motivo, in.EmpresaID))
			}
			t.FijarEmpresa(deref(empresa))
			clave := fmt.Sprintf("hallazgo:%d:%s", deref(empresa), in.HallazgoID)
			if err := q.BloquearIncidente(ctx, "soporte."+clave); err != nil {
				return ResultadoHorus{}, err
			}
			// Si Horus ya avisó que el hallazgo se resolvió después de esta apertura (llegó antes
			// el resuelto), no se abre ticket: obsoleto.
			if ok, err := enOrden(ctx, q, clave, empresa, "abierto", in.OcurridoEn, in.EntranteID); err != nil || !ok {
				if err != nil {
					return ResultadoHorus{}, err
				}
				return obsoleto(ctx, t, q, in.EntranteID)
			}
			existente, err := q.TicketDeHallazgo(ctx, soportebd.TicketDeHallazgoParams{HallazgoID: &in.HallazgoID, EmpresaID: empresa})
			if err == nil && mismoID(existente.EmpresaID, empresa) {
				// Horus lo reenvió con otro webhook: un ticket por hallazgo.
				t.FijarEntidad("ticket", existente.ID)
				return cerrarHorus(ctx, q, in.EntranteID, ResultadoHorus{Resultado: HorusTicketExistente, TicketID: &existente.ID}, nil)
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return ResultadoHorus{}, err
			}

			entrada := EntradaCrearTicket{
				EmpresaID: empresa, CategoriaID: &regla.CategoriaID, Prioridad: regla.Prioridad, Origen: "horus",
				Asunto: recortarRunas("Seguridad: "+categoria, 200),
			}
			detalles := []string{fmt.Sprintf("Horus abrió el hallazgo %s (categoría %s", in.HallazgoID, categoria)}
			if in.Severidad != "" {
				detalles[0] += ", severidad " + in.Severidad
			}
			detalles[0] += ")."
			if in.IP != nil {
				detalles = append(detalles, "IP: "+*in.IP)
			}
			if in.Resumen != "" {
				detalles = append(detalles, "Resumen: "+in.Resumen)
			}
			if in.URLHorus != nil {
				detalles = append(detalles, "Ver en Horus: "+*in.URLHorus)
			}
			// El cliente que manda Horus debe ser de la empresa del hallazgo según la instantánea
			// de Argos; si no, el ticket se abre sin cliente (nunca con datos de otra empresa).
			if in.ClienteID != nil {
				if err := argos.ValidarClienteEnEmpresa(ctx, t, *in.ClienteID, in.ServicioID, empresa); err != nil {
					var p *nucleo.Problema
					if !errors.As(err, &p) || (p.Codigo() != "empresa_no_coincide" && p.Codigo() != "validacion") {
						return ResultadoHorus{}, err
					}
					t.Anotar(map[string]any{"cliente_descartado": *in.ClienteID})
					motivo := "otra_empresa"
					in.ClienteID, in.ServicioID, in.MotivoSinCliente = nil, nil, &motivo
				}
			}
			if in.ClienteID != nil {
				entrada.ClienteID = in.ClienteID
				entrada.ServicioID = in.ServicioID
				entrada.Descripcion = "Detectamos un posible problema de seguridad en tu servicio (" + categoria +
					"). Nuestro equipo de soporte lo revisará y te contactará."
			} else {
				porque := derefStr(in.MotivoSinCliente)
				if texto, ok := motivosSinCliente[porque]; ok {
					porque += ": " + texto
				}
				if porque == "" {
					porque = "sin datos"
				}
				entrada.Descripcion = "Hallazgo de seguridad sin cliente identificado (" + porque + ")."
				detalles = append(detalles, "Sin cliente: "+porque)
			}
			hallazgo := in.HallazgoID
			f, err := m.crearTicket(ctx, t, entrada, opcionesCrear{
				incidente: &datosIncidente{hallazgoID: &hallazgo, urlHorus: in.URLHorus, routerID: in.RouterID},
				nota:      strings.Join(detalles, "\n"),
			})
			if err != nil {
				return ResultadoHorus{}, err
			}
			t.Anotar(map[string]any{"entrante_id": in.EntranteID, "hallazgo_id": in.HallazgoID})
			return cerrarHorus(ctx, q, in.EntranteID, ResultadoHorus{Resultado: HorusTicketCreado, TicketID: &f.ID}, nil)
		},
	})
}

func (m *Modulo) nuevoHallazgoResuelto() *auditoria.Caso[horus.HallazgoResuelto, ResultadoHorus] {
	return auditoria.Nuevo(auditoria.Caso[horus.HallazgoResuelto, ResultadoHorus]{
		Accion:       "tickets.horus_hallazgo_resuelto",
		Acceso:       auditoria.Autenticado,
		AuditarExito: auditarHorus,
		Logica: func(ctx context.Context, t *auditoria.Tx, in horus.HallazgoResuelto) (ResultadoHorus, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoHorus{}, err
			}
			q := soportebd.New(t)
			t.FijarEntidad("horus_entrante", in.EntranteID)
			nuevo, previo, err := reclamarHorus(ctx, q, in.EntranteID, horus.EventoHallazgoResuelto, in.EmpresaID)
			if err != nil || !nuevo {
				return previo, err
			}
			// El ticket ya existe: se busca por hallazgo (y por la empresa, si el evento la trae y
			// hay empresas registradas; sin conector los tickets no tienen empresa).
			hay, err := q.HayEmpresas(ctx)
			if err != nil {
				return ResultadoHorus{}, err
			}
			var empresa *int64
			if hay {
				empresa = in.EmpresaID
			}
			// Se guarda aunque todavía no haya ticket: si la apertura llega después (fuera de
			// orden), queda obsoleta y no abre nada.
			clave := fmt.Sprintf("hallazgo:%d:%s", deref(empresa), in.HallazgoID)
			if err := q.BloquearIncidente(ctx, "soporte."+clave); err != nil {
				return ResultadoHorus{}, err
			}
			if ok, err := enOrden(ctx, q, clave, empresa, "resuelto", in.OcurridoEn, in.EntranteID); err != nil || !ok {
				if err != nil {
					return ResultadoHorus{}, err
				}
				return obsoleto(ctx, t, q, in.EntranteID)
			}
			f, err := q.TicketDeHallazgo(ctx, soportebd.TicketDeHallazgoParams{HallazgoID: &in.HallazgoID, EmpresaID: empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return cerrarHorus(ctx, q, in.EntranteID, ResultadoHorus{Resultado: HorusSinTicket}, nil)
			}
			if err != nil {
				return ResultadoHorus{}, err
			}
			t.FijarEntidad("ticket", f.ID)
			t.FijarEmpresa(deref(f.EmpresaID))
			nota := fmt.Sprintf("Horus marcó el hallazgo %s como resuelto (%s).", in.HallazgoID, in.OcurridoEn.UTC().Format(time.RFC3339))
			if in.Resumen != "" {
				nota += " " + in.Resumen
			}
			r := ResultadoHorus{Resultado: HorusPropuesta, TicketID: &f.ID}
			switch {
			case abierto(f.Estado):
				_, err = m.proponer(ctx, t, q, f, "resolver", nota, in.EntranteID)
			case f.Estado == EstadoResuelto:
				_, err = m.proponer(ctx, t, q, f, "cerrar", nota, in.EntranteID)
			default:
				r.Resultado = HorusNota
				_, err = mensajeSistema(ctx, q, f, false, nota)
			}
			if err != nil {
				return ResultadoHorus{}, err
			}
			return cerrarHorus(ctx, q, in.EntranteID, r, nil)
		},
	})
}

func (m *Modulo) nuevoDescartarHorus() *auditoria.Caso[EntradaDescartarHorus, ResultadoHorus] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDescartarHorus, ResultadoHorus]{
		Accion: "tickets.horus_descartar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDescartarHorus) (ResultadoHorus, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoHorus{}, err
			}
			q := soportebd.New(t)
			t.FijarEntidad("horus_entrante", in.EntranteID)
			nuevo, previo, err := reclamarHorus(ctx, q, in.EntranteID, in.Evento, in.EmpresaID)
			if err != nil || !nuevo {
				return previo, err
			}
			return descartar(ctx, t, q, in.EntranteID, in.Evento, in.EmpresaID, HorusError, recortarRunas(in.Motivo, 500))
		},
	})
}

// Suscriptores (cmd/worker) ------------------------------------------------------------

// SuscribirHorus registra los consumidores de los eventos internos horus.* (idempotentes por
// entrante_id).
func (m *Modulo) SuscribirHorus(bus *integracion.Bus, bd nucleo.BD) {
	for _, ev := range []string{horus.EventoRouterEstadoCambiado, horus.EventoServicioInfraCambiado, horus.EventoHallazgoAbierto, horus.EventoHallazgoResuelto} {
		bus.Suscribir("soporte.horus", ev, func(ctx context.Context, e integracion.Evento) error {
			return m.AlEventoHorus(nucleo.ConBD(ctx, bd), e)
		})
	}
}

// AlEventoHorus aplica un evento horus.* (actor sistema del relay). Un payload ilegible no se
// reintenta; una regla de negocio que impide aplicarlo (p. ej. la categoría de la regla quedó
// inactiva) queda registrada como «error» con soporte.horus_descartado, sin reintentos; un
// error de base se reintenta (el relay).
func (m *Modulo) AlEventoHorus(ctx context.Context, e integracion.Evento) error {
	var (
		entrante int64
		empresa  *int64
		err      error
	)
	switch e.Evento {
	case horus.EventoRouterEstadoCambiado:
		var p horus.RouterEstadoCambiado
		if json.Unmarshal(e.Payload, &p) != nil || p.EntranteID == 0 || p.RouterID == 0 {
			return nil
		}
		entrante, empresa = p.EntranteID, p.EmpresaID
		_, err = m.IncidenteHorus.Ejecutar(ctx, EntradaIncidenteHorus{
			EntranteID: p.EntranteID, Evento: e.Evento, Tipo: "router", RouterID: &p.RouterID, Nombre: p.Nombre,
			Estado: p.Estado, OcurridoEn: p.OcurridoEn, EmpresaID: p.EmpresaID,
		})
	case horus.EventoServicioInfraCambiado:
		var p horus.ServicioInfraCambiado
		if json.Unmarshal(e.Payload, &p) != nil || p.EntranteID == 0 || p.InfraID == "" {
			return nil
		}
		entrante, empresa = p.EntranteID, p.EmpresaID
		_, err = m.IncidenteHorus.Ejecutar(ctx, EntradaIncidenteHorus{
			EntranteID: p.EntranteID, Evento: e.Evento, Tipo: "infra", RouterID: p.RouterID, InfraID: &p.InfraID,
			InfraTipo: p.Tipo, Nombre: p.Nombre, Estado: p.Estado, OcurridoEn: p.OcurridoEn, EmpresaID: p.EmpresaID,
		})
	case horus.EventoHallazgoAbierto:
		var p horus.HallazgoAbierto
		if json.Unmarshal(e.Payload, &p) != nil || p.EntranteID == 0 || p.HallazgoID == "" {
			return nil
		}
		entrante, empresa = p.EntranteID, p.EmpresaID
		_, err = m.HallazgoAbierto.Ejecutar(ctx, p)
	case horus.EventoHallazgoResuelto:
		var p horus.HallazgoResuelto
		if json.Unmarshal(e.Payload, &p) != nil || p.EntranteID == 0 || p.HallazgoID == "" {
			return nil
		}
		entrante, empresa = p.EntranteID, p.EmpresaID
		_, err = m.HallazgoResuelto.Ejecutar(ctx, p)
	default:
		return nil
	}
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Status < 500 {
		_, err = m.DescartarHorus.Ejecutar(ctx, EntradaDescartarHorus{
			EntranteID: entrante, Evento: e.Evento, EmpresaID: empresa, Motivo: p.Codigo() + ": " + p.Detail,
		})
	}
	return err
}
