package soporte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Proceso de un correo guardado (job soporte.procesar_correos, único por buzón, en orden de
// llegada; cada correo en su transacción y una sola vez: estado pendiente → procesado | ignorado
// | error):
//
//   - Autorespuestas y rebotes (Auto-Submitted, Precedence bulk/junk/list/auto_reply,
//     X-Autoreply, MAILER-DAEMON, multipart/report, Return-Path <>) y correos del propio buzón:
//     ignorado.
//   - Respuesta a un ticket existente si In-Reply-To/References apuntan a un correo de ese hilo
//     (una respuesta que enviamos o un correo del cliente ya ligado) o si el asunto trae el número
//     ([TK-000123]) y el remitente es el contacto del ticket. El ticket debe ser de la misma
//     empresa del buzón (si no, se trata como correo nuevo). Se agrega como mensaje público del
//     cliente y: en_espera → abierto (reanuda), resuelto → abierto (reabre). Si el ticket se
//     fusionó, va al destino; si está cerrado, se abre uno nuevo que lo menciona.
//   - Si no: ticket nuevo con origen correo, empresa, categoría y prioridad del buzón, y el
//     remitente como contacto (sin conector con lectura por correo en Argos no se identifica al
//     cliente: queda el contacto a mano).
//   - Una regla de negocio que impide crear (p. ej. el buzón quedó sin empresa al registrar
//     empresas) deja el correo en error con el motivo; un error de base se reintenta.

var numeroEnAsunto = regexp.MustCompile(`\[(TK-\d{6,})\]`)

// EntradaProcesarCorreo es la entrada de correos.procesar (acción interna del worker).
type EntradaProcesarCorreo struct {
	ID int64 `json:"correoId"`
}

// ResultadoProcesarCorreo dice qué se hizo con el correo.
type ResultadoProcesarCorreo struct {
	Estado    string  `json:"estado"`
	Resultado *string `json:"resultado"`
	TicketID  *int64  `json:"ticketId"`
	MensajeID *int64  `json:"mensajeId"`
	Motivo    *string `json:"motivo"`
}

func aResultadoCorreo(e soportebd.SoporteCorreosEntrante) ResultadoProcesarCorreo {
	return ResultadoProcesarCorreo{Estado: e.Estado, Resultado: e.Resultado, TicketID: e.TicketID, MensajeID: e.MensajeID, Motivo: e.Motivo}
}

func (m *Modulo) nuevoProcesarCorreo() *auditoria.Caso[EntradaProcesarCorreo, ResultadoProcesarCorreo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaProcesarCorreo, ResultadoProcesarCorreo]{
		Accion: "correos.procesar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaProcesarCorreo) (ResultadoProcesarCorreo, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoProcesarCorreo{}, err
			}
			q := soportebd.New(t)
			t.FijarEntidad("correo_entrante", in.ID)
			e, err := q.CorreoEntranteParaProcesar(ctx, in.ID)
			if err != nil {
				return ResultadoProcesarCorreo{}, err
			}
			t.FijarEmpresa(deref(e.EmpresaID))
			if e.Estado != "pendiente" {
				t.Anotar(map[string]any{"repetida": true})
				return aResultadoCorreo(e), nil
			}
			b, err := q.BuzonPorID(ctx, e.BuzonID)
			if err != nil {
				return ResultadoProcesarCorreo{}, err
			}
			r, err := m.procesarCorreo(ctx, t, q, b, e)
			var p *nucleo.Problema
			if errors.As(err, &p) && p.Status < 500 {
				// Regla de negocio: no se arregla reintentando. Queda en error con su motivo.
				r = ResultadoProcesarCorreo{Estado: "error", Motivo: ptr(correo.Recortar(p.Codigo()+": "+p.Detail, 500))}
			} else if err != nil {
				return ResultadoProcesarCorreo{}, err
			}
			if err := q.CerrarCorreoEntrante(ctx, soportebd.CerrarCorreoEntranteParams{
				ID: e.ID, Estado: r.Estado, Resultado: r.Resultado, Motivo: r.Motivo, TicketID: r.TicketID,
				MensajeID: r.MensajeID, Ahora: ptr(m.ahora()),
			}); err != nil {
				return ResultadoProcesarCorreo{}, err
			}
			t.Anotar(map[string]any{"estado": r.Estado, "resultado": r.Resultado, "ticket_id": r.TicketID, "motivo": r.Motivo})
			return r, t.Emitir("correo.recibido", map[string]any{
				"correo_id": e.ID, "buzon_id": e.BuzonID, "estado": r.Estado, "resultado": r.Resultado,
				"ticket_id": r.TicketID, "mensaje_id": r.MensajeID, "motivo": r.Motivo,
			})
		},
	})
}

func (m *Modulo) procesarCorreo(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, b soportebd.SoporteBuzon, e soportebd.SoporteCorreosEntrante) (ResultadoProcesarCorreo, error) {
	ignorado := func(motivo string) (ResultadoProcesarCorreo, error) {
		return ResultadoProcesarCorreo{Estado: "ignorado", Motivo: &motivo}, nil
	}
	switch {
	case e.Automatico != nil:
		return ignorado("automatico: " + *e.Automatico)
	case strings.EqualFold(e.Remitente, b.Correo):
		return ignorado("remitente_propio")
	}
	ticket, cerrado, err := m.ticketDelCorreo(ctx, q, b, e)
	if err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	if ticket != nil {
		return m.respuestaDelCliente(ctx, t, q, *ticket, e)
	}
	return m.ticketDesdeCorreo(ctx, t, q, b, e, cerrado)
}

// ticketDelCorreo busca el ticket al que responde el correo (FOR UPDATE). cerrado es el número
// de un ticket cerrado al que respondía (para mencionarlo en el nuevo).
func (m *Modulo) ticketDelCorreo(ctx context.Context, q *soportebd.Queries, b soportebd.SoporteBuzon, e soportebd.SoporteCorreosEntrante) (*soportebd.SoporteTicket, string, error) {
	var id int64
	ids := append([]string{}, e.Referencias...)
	if e.InReplyTo != nil {
		ids = append(ids, *e.InReplyTo)
	}
	if len(ids) > 0 {
		v, err := q.TicketDeHilo(ctx, soportebd.TicketDeHiloParams{BuzonID: b.ID, Ids: ids})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, "", err
		}
		id = v
	}
	if id == 0 {
		// Por número en el asunto, solo si lo manda el contacto del ticket (cualquiera puede
		// escribir [TK-…] en un asunto).
		if n := numeroEnAsunto.FindStringSubmatch(e.Asunto); n != nil {
			f, err := q.TicketPorNumero(ctx, n[1])
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, "", err
			}
			if err == nil && f.ContactoCorreo != nil && strings.EqualFold(*f.ContactoCorreo, e.Remitente) {
				id = f.ID
			}
		}
	}
	cerrado := ""
	for saltos := 0; id != 0 && saltos < 5; saltos++ {
		f, err := q.TicketPorIDParaActualizar(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, cerrado, nil
		}
		if err != nil {
			return nil, "", err
		}
		// Un buzón nunca toca tickets de otra empresa.
		if !mismoID(f.EmpresaID, b.EmpresaID) {
			return nil, cerrado, nil
		}
		if f.Estado != EstadoCerrado {
			return &f, cerrado, nil
		}
		cerrado = f.Numero
		if f.FusionadoEnID == nil {
			return nil, cerrado, nil
		}
		id = *f.FusionadoEnID
	}
	return nil, cerrado, nil
}

// textoDelCorreo arma el cuerpo del mensaje del cliente.
func textoDelCorreo(e soportebd.SoporteCorreosEntrante, sinCita bool) string {
	cuerpo := e.Cuerpo
	if sinCita {
		cuerpo = correo.QuitarCita(cuerpo)
	}
	if e.Truncado {
		cuerpo = "(El correo superó el tamaño máximo y no se importó su contenido; revísalo en el buzón.)"
	}
	var adjuntos []correo.Adjunto
	_ = json.Unmarshal(e.Adjuntos, &adjuntos)
	if len(adjuntos) > 0 {
		lineas := []string{"", "Adjuntos del correo (no importados):"}
		for _, a := range adjuntos {
			nombre := a.Nombre
			if nombre == "" {
				nombre = "(sin nombre)"
			}
			lineas = append(lineas, fmt.Sprintf("- %s (%s, %d bytes)", nombre, a.Tipo, a.Tamano))
		}
		cuerpo += strings.Join(lineas, "\n")
	}
	if strings.TrimSpace(cuerpo) == "" {
		cuerpo = "(Correo sin texto)"
	}
	return correo.Recortar(strings.TrimSpace(cuerpo), maxCuerpoCorreo)
}

func autorDelCorreo(e soportebd.SoporteCorreosEntrante) string {
	if e.RemitenteNombre != "" {
		return correo.Recortar(e.RemitenteNombre+" <"+e.Remitente+">", 300)
	}
	return e.Remitente
}

// respuestaDelCliente agrega el correo como mensaje público del cliente y aplica la regla de
// estado: en_espera → abierto (reanuda) y resuelto → abierto (reabre).
func (m *Modulo) respuestaDelCliente(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, antes soportebd.SoporteTicket, e soportebd.SoporteCorreosEntrante) (ResultadoProcesarCorreo, error) {
	ahora := m.ahora()
	cal, err := cargarCalendario(ctx, q, antes.CalendarioID)
	if err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	despues := antes
	var motivo *string
	switch antes.Estado {
	case EstadoEnEspera:
		salirDeEspera(&despues, cal, ahora)
		despues.Estado, motivo = EstadoAbierto, ptr("El cliente respondió por correo")
	case EstadoResuelto:
		acumularPausa(&despues, cal, antes.ResueltoEn, ahora)
		despues.Estado, despues.ResueltoEn, motivo = EstadoAbierto, nil, ptr("El cliente respondió por correo a un ticket resuelto")
	}
	f, err := guardar(ctx, t, q, antes, despues, cal)
	if err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	msj, err := q.CrearMensajeCliente(ctx, soportebd.CrearMensajeClienteParams{
		TicketID: f.ID, EmpresaID: f.EmpresaID, AutorNombre: autorDelCorreo(e), Canal: "correo", Cuerpo: textoDelCorreo(e, true),
	})
	if err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	if err := cambioDeEstado(ctx, t, q, antes, f, motivo); err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	return ResultadoProcesarCorreo{Estado: "procesado", Resultado: ptr("respuesta"), TicketID: &f.ID, MensajeID: &msj.ID}, nil
}

// asuntoDelCorreo deja un asunto válido para el ticket (3 a 200 caracteres).
func asuntoDelCorreo(e soportebd.SoporteCorreosEntrante) string {
	a := strings.TrimSpace(e.Asunto)
	if len([]rune(a)) < 3 {
		a = "Correo de " + e.Remitente
	}
	return correo.Recortar(a, 200)
}

// ticketDesdeCorreo crea el ticket (tickets.crear anidado, origen correo) y le agrega el correo
// como primer mensaje del cliente.
func (m *Modulo) ticketDesdeCorreo(ctx context.Context, t *auditoria.Tx, q *soportebd.Queries, b soportebd.SoporteBuzon, e soportebd.SoporteCorreosEntrante, cerrado string) (ResultadoProcesarCorreo, error) {
	contacto := &EntradaContacto{Correo: &e.Remitente}
	if e.RemitenteNombre != "" {
		contacto.Nombre = ptr(correo.Recortar(e.RemitenteNombre, 120))
	}
	tk, err := m.CrearTicket.Ejecutar(ctx, EntradaCrearTicket{
		EmpresaID: b.EmpresaID, CategoriaID: b.CategoriaID, Prioridad: b.Prioridad, Origen: "correo",
		Asunto: asuntoDelCorreo(e), Contacto: contacto,
	})
	if err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	cuerpo := textoDelCorreo(e, false)
	if cerrado != "" {
		cuerpo = correo.Recortar("(Seguimiento de "+cerrado+", que ya estaba cerrado.)\n\n"+cuerpo, maxCuerpoCorreo)
	}
	msj, err := q.CrearMensajeCliente(ctx, soportebd.CrearMensajeClienteParams{
		TicketID: tk.ID, EmpresaID: tk.EmpresaID, AutorNombre: autorDelCorreo(e), Canal: "correo", Cuerpo: cuerpo,
	})
	if err != nil {
		return ResultadoProcesarCorreo{}, err
	}
	return ResultadoProcesarCorreo{Estado: "procesado", Resultado: ptr("ticket_creado"), TicketID: &tk.ID, MensajeID: &msj.ID}, nil
}

// ProcesarCorreos procesa, en orden, todos los correos pendientes del buzón. Lo llama el worker
// (actor sistema). Si uno falla por un error que no es de negocio, se detiene (para no
// desordenar el hilo) y River reintenta.
func (m *Modulo) ProcesarCorreos(ctx context.Context, buzonID int64) (int, error) {
	q := soportebd.New(nucleo.BDDe(ctx))
	n := 0
	for {
		ids, err := q.CorreosPendientes(ctx, soportebd.CorreosPendientesParams{BuzonID: buzonID, Limite: 100})
		if err != nil {
			return n, err
		}
		if len(ids) == 0 {
			return n, nil
		}
		for _, id := range ids {
			if _, err := m.ProcesarCorreo.Ejecutar(ctx, EntradaProcesarCorreo{ID: id}); err != nil {
				return n, err
			}
			n++
		}
	}
}
