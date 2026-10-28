package soporte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Salida mínima por correo: cuando un agente responde (ticket.respondido) en un ticket de origen
// correo, se encola el envío por el SMTP del buzón del que llegó el último correo del cliente,
// con el número en el asunto y In-Reply-To/References al correo del cliente.
//
//   - El consumidor es idempotente: una fila por mensaje (correos_salientes.mensaje_id único);
//     un evento repetido no encola otro envío.
//   - El Message-ID se fija al encolar: si el proceso cae entre el envío y la marca, el
//     reintento sale con el mismo Message-ID (los clientes de correo lo deduplican).
//   - Sin SMTP en el buzón (o sin correo del cliente ligado), no se envía nada.

// EntradaEncolarRespuesta es la entrada de correos.encolar_respuesta (consumidor del evento).
type EntradaEncolarRespuesta struct {
	TicketID  int64 `json:"ticketId"`
	MensajeID int64 `json:"mensajeId"`
}

// ResultadoEncolarRespuesta: id del correo saliente encolado (0 si no aplica o ya estaba).
type ResultadoEncolarRespuesta struct {
	SalienteID int64  `json:"salienteId"`
	Motivo     string `json:"motivo,omitempty"`
}

func (m *Modulo) nuevoEncolarRespuesta() *auditoria.Caso[EntradaEncolarRespuesta, ResultadoEncolarRespuesta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEncolarRespuesta, ResultadoEncolarRespuesta]{
		Accion: "correos.encolar_respuesta",
		Acceso: auditoria.Autenticado,
		// Solo deja fila cuando encola (la mayoría de las respuestas no son de tickets de correo).
		AuditarExito: func(r ResultadoEncolarRespuesta) bool { return r.SalienteID != 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEncolarRespuesta) (ResultadoEncolarRespuesta, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoEncolarRespuesta{}, err
			}
			q := soportebd.New(t)
			nada := func(motivo string) (ResultadoEncolarRespuesta, error) {
				return ResultadoEncolarRespuesta{Motivo: motivo}, nil
			}
			tk, err := q.TicketPorID(ctx, in.TicketID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nada("ticket_inexistente")
			}
			if err != nil {
				return ResultadoEncolarRespuesta{}, err
			}
			t.FijarEntidad("ticket", tk.ID)
			t.FijarEmpresa(deref(tk.EmpresaID))
			if tk.Origen != "correo" {
				return nada("origen_no_correo")
			}
			msj, err := q.MensajePorID(ctx, in.MensajeID)
			if err != nil {
				return ResultadoEncolarRespuesta{}, err
			}
			if msj.TicketID != tk.ID || !msj.Publico || msj.AutorTipo != "usuario" {
				return nada("no_es_respuesta_de_agente")
			}
			ult, err := q.UltimoCorreoDeTicket(ctx, &tk.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nada("sin_correo_del_cliente")
			}
			if err != nil {
				return ResultadoEncolarRespuesta{}, err
			}
			b, err := q.BuzonPorID(ctx, ult.BuzonID)
			if err != nil {
				return ResultadoEncolarRespuesta{}, err
			}
			if b.SmtpHost == nil || !mismoID(b.EmpresaID, tk.EmpresaID) {
				return nada("buzon_sin_smtp")
			}
			asunto := tk.Asunto
			if !strings.Contains(asunto, "["+tk.Numero+"]") {
				asunto = "[" + tk.Numero + "] " + asunto
			}
			if !strings.HasPrefix(strings.ToLower(asunto), "re:") {
				asunto = "Re: " + asunto
			}
			_, dominio, _ := strings.Cut(b.Correo, "@")
			s, err := q.CrearCorreoSaliente(ctx, soportebd.CrearCorreoSalienteParams{
				BuzonID: b.ID, EmpresaID: tk.EmpresaID, TicketID: tk.ID, MensajeID: msj.ID,
				MessageID:    fmt.Sprintf("iris.%s.%s@%s", strings.ToLower(tk.Numero), uuid.NewString(), dominio),
				InReplyTo:    &ult.MessageID,
				Destinatario: ult.Remitente, Asunto: correo.Recortar(asunto, 250),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return nada("ya_encolado") // el evento llegó dos veces
			}
			if err != nil {
				return ResultadoEncolarRespuesta{}, err
			}
			t.FijarEntidad("correo_saliente", s.ID)
			if m.cfg.Encolar != nil {
				if err := m.cfg.Encolar(ctx, t.Tx, ArgsEnviarCorreo{SalienteID: s.ID}); err != nil {
					return ResultadoEncolarRespuesta{}, err
				}
			}
			return ResultadoEncolarRespuesta{SalienteID: s.ID}, nil
		},
	})
}

// EntradaRegistrarEnvio anota el resultado de un envío (acción interna del worker).
type EntradaRegistrarEnvio struct {
	SalienteID int64  `json:"salienteId"`
	Error      string `json:"error,omitempty"`
}

func (m *Modulo) nuevoRegistrarEnvio() *auditoria.Caso[EntradaRegistrarEnvio, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrarEnvio, struct{}]{
		Accion: "correos.enviar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarEnvio) (struct{}, error) {
			if err := esSistema(ctx); err != nil {
				return struct{}{}, err
			}
			q := soportebd.New(t)
			t.FijarEntidad("correo_saliente", in.SalienteID)
			s, err := q.CorreoSalienteParaActualizar(ctx, in.SalienteID)
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(deref(s.EmpresaID))
			if in.Error != "" {
				return struct{}{}, q.FalloCorreoSaliente(ctx, soportebd.FalloCorreoSalienteParams{ID: s.ID, UltimoError: &in.Error})
			}
			if s.Estado == "enviado" {
				t.Anotar(map[string]any{"repetida": true})
				return struct{}{}, nil
			}
			return struct{}{}, q.MarcarCorreoEnviado(ctx, soportebd.MarcarCorreoEnviadoParams{ID: s.ID, Ahora: ptr(m.ahora())})
		},
	})
}

// EnviarCorreo envía un correo saliente encolado (lo llama el worker como actor sistema). Si ya
// salió, no hace nada. Un error de SMTP se anota (resumido) y se devuelve para que River
// reintente.
func (m *Modulo) EnviarCorreo(ctx context.Context, salienteID int64) error {
	q := soportebd.New(nucleo.BDDe(ctx))
	s, err := q.CorreoSalientePorID(ctx, salienteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || s.Estado == "enviado" {
		return err
	}
	fallar := func(err error) error {
		detalle := err.Error()
		var p *nucleo.Problema
		if errors.As(err, &p) {
			detalle = p.Detail
		} else if r := correo.Resumir(err); r != nil {
			detalle = r.Error()
		}
		if _, e := m.RegistrarEnvio.Ejecutar(ctx, EntradaRegistrarEnvio{SalienteID: s.ID, Error: correo.Recortar(detalle, 500)}); e != nil {
			return errors.Join(err, e)
		}
		return err
	}
	b, err := q.BuzonPorID(ctx, s.BuzonID)
	if err != nil {
		return err
	}
	_, srv, err := m.servidores(b)
	if err != nil {
		return fallar(err)
	}
	if srv == nil {
		return fallar(errors.New("el buzón ya no tiene SMTP"))
	}
	msj, err := q.MensajePorID(ctx, s.MensajeID)
	if err != nil {
		return err
	}
	tk, err := q.TicketPorID(ctx, s.TicketID)
	if err != nil {
		return err
	}
	saliente := correo.Saliente{
		De: mail.Address{Name: b.Nombre, Address: b.Correo}, Para: mail.Address{Address: s.Destinatario},
		Asunto: s.Asunto, MessageID: s.MessageID, Fecha: m.ahora(),
		Texto: msj.Cuerpo + "\n\n-- \nPara seguir con este caso responde a este correo conservando " + tk.Numero + " en el asunto.",
	}
	if s.InReplyTo != nil {
		saliente.InReplyTo, saliente.Referencias = *s.InReplyTo, []string{*s.InReplyTo}
	}
	if err := correo.Enviar(ctx, m.cfg.Correo.Red, *srv, saliente); err != nil {
		return fallar(err)
	}
	_, err = m.RegistrarEnvio.Ejecutar(ctx, EntradaRegistrarEnvio{SalienteID: s.ID})
	return err
}

// AlResponderTicket es el suscriptor de ticket.respondido (idempotente: ver arriba).
func (m *Modulo) AlResponderTicket(ctx context.Context, e integracion.Evento) error {
	var p struct {
		TicketID  int64 `json:"ticket_id"`
		MensajeID int64 `json:"mensaje_id"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.TicketID == 0 || p.MensajeID == 0 {
		return nil // payload ajeno: nada que enviar
	}
	_, err := m.EncolarRespuesta.Ejecutar(ctx, EntradaEncolarRespuesta{TicketID: p.TicketID, MensajeID: p.MensajeID})
	return err
}
