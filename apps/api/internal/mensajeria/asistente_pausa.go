package mensajeria

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// El asistente de IA por conversación (contratos/conversaciones.md §17.11): si la atiende solo ahora
// y por qué no, y las acciones «pausar» y «reanudar».
//
// La pausa es de la conversación y del periodo: mientras está, el asistente no le contesta solo al
// cliente (las sugerencias al agente siguen: no llegan al cliente). Se quita al reanudar o cuando la
// conversación se resuelve (el trigger conv_asistente_estado). «Reanudar» también le devuelve la
// conversación al asistente cuando quedó detenido porque una persona la tomó o escribió, o cuando él
// mismo la pasó a una persona: es la decisión explícita de alguien, nunca algo que ocurra solo.

// Por qué el asistente no atiende una conversación (lista cerrada, en este orden).
const (
	NoAtiendePausado          = "pausado"
	NoAtiendePersonaAsignada  = "persona_asignada"
	NoAtiendePersonaPresente  = "persona_presente"
	NoAtiendeEsperaPersona    = "espera_persona"
	NoAtiendeTope             = "tope"
	NoAtiendeBot              = "bot"
	NoAtiendeCategoriaSugiere = "categoria_sugiere"
	NoAtiendeHorarioSugiere   = "horario_solo_sugiere"
)

// Motivos de una pausa.
const (
	PausaManual              = "manual"
	PausaCasoDelicado        = "caso_delicado"
	PausaRespuestaIncorrecta = "respuesta_incorrecta"
	PausaClienteLoPidio      = "cliente_lo_pidio"
)

// PausaAsistente es la pausa vigente del asistente en la conversación.
type PausaAsistente struct {
	Por    *UsuarioRef `json:"por,omitempty" doc:"Quién lo pausó"`
	En     time.Time   `json:"en"`
	Motivo string      `json:"motivo" enum:"manual,caso_delicado,respuesta_incorrecta,cliente_lo_pidio"`
}

// AtencionAsistente dice si el asistente atiende solo esta conversación ahora y, si no, por qué.
type AtencionAsistente struct {
	Atiende    bool            `json:"atiende" doc:"true: nada en la conversación le impide contestar solo el próximo mensaje del cliente (cada mensaje pasa todavía por las reglas del turno: categoría, condición, límites). false: no va a contestar; motivo dice por qué"`
	Motivo     *string         `json:"motivo" nullable:"true" enum:"pausado,persona_asignada,persona_presente,espera_persona,tope,bot,categoria_sugiere,horario_solo_sugiere" doc:"Con atiende = false, el primero que aplica: pausado (alguien lo pausó aquí), persona_asignada, persona_presente (una persona la tomó o le escribió al cliente en este periodo), espera_persona (el asistente la pasó a una persona: asistente.motivoPaso), tope (tope de tokens del mes), bot (la tomó el bot de menú), categoria_sugiere (su categoría no está en «responde solo»), horario_solo_sugiere (la franja del horario dice «solo sugerir»). null con atiende = true"`
	Reanudable bool            `json:"reanudable" doc:"«Reanudar» cambia algo: está pausado, detenido por una persona o esperando a una persona"`
	Pausa      *PausaAsistente `json:"pausa,omitempty" doc:"Ausente: nadie lo pausó en esta conversación"`
}

// autonomoEmpresa es lo que la empresa tiene configurado (caché por empresa al armar una lista).
type autonomoEmpresa struct {
	aj        *mensajeriabd.MensajeriaAsistenteAjuste
	encendido bool // activo y con un modo autónomo
	tope      bool // en pausa por el tope del mes
}

func (m *Modulo) autonomoDeEmpresa(ctx context.Context, q *mensajeriabd.Queries, empresa int64) (autonomoEmpresa, error) {
	e := autonomoEmpresa{}
	if !AutonomoDisponible {
		return e, nil
	}
	aj, err := leerAjustesAsistente(ctx, q, empresa)
	if err != nil || aj == nil || !aj.Activo || aj.ModoAutonomo == AutonomoNunca {
		return e, err
	}
	e.aj, e.encendido = aj, true
	e.tope, err = m.pausadoPorTope(ctx, q, empresa, aj.TopeTokensMes)
	return e, err
}

// atencionAsistente arma el campo de lectura. Ausente (nil): la conversación está resuelta o es de
// solo lectura, o la empresa no tiene el asistente atendiendo solo y nadie lo pausó aquí.
// soloSugerir: la franja vigente del horario de la cola dice «solo sugerir» (se pregunta al final,
// solo si hace falta).
func atencionAsistente(f mensajeriabd.ResumenConversacionesClienteRow, e autonomoEmpresa, soloSugerir func() (bool, error)) (*AtencionAsistente, error) {
	if f.Estado == EstadoConvResuelta || f.SoloLectura {
		return nil, nil
	}
	var pausa *PausaAsistente
	if f.AsistentePausadoEn != nil && f.AsistentePausaMotivo != nil {
		pausa = &PausaAsistente{En: *f.AsistentePausadoEn, Motivo: *f.AsistentePausaMotivo}
		if f.AsistentePausadoPor != nil && f.PausadoPorNombre != nil {
			pausa.Por = &UsuarioRef{ID: *f.AsistentePausadoPor, Nombre: *f.PausadoPorNombre}
		}
	}
	if pausa == nil && !e.encendido {
		return nil, nil
	}
	estado := ""
	if f.AsistenteEstado != nil {
		estado = *f.AsistenteEstado
	}
	no := func(motivo string) (*AtencionAsistente, error) {
		return &AtencionAsistente{Motivo: &motivo, Pausa: pausa,
			Reanudable: pausa != nil || motivo == NoAtiendePersonaPresente || motivo == NoAtiendeEsperaPersona}, nil
	}
	switch {
	case pausa != nil:
		return no(NoAtiendePausado)
	case f.AsignadoA != nil:
		return no(NoAtiendePersonaAsignada)
	case estado == AsistenteDetenido || (estado == "" && f.PersonaEscribio):
		return no(NoAtiendePersonaPresente)
	case estado == AsistenteEsperaPersona:
		return no(NoAtiendeEsperaPersona)
	case e.tope:
		return no(NoAtiendeTope)
	case estado == AsistenteAtendiendo:
		return &AtencionAsistente{Atiende: true}, nil
	case f.BotMenu != nil || f.BotAtendioEn != nil:
		return no(NoAtiendeBot)
	case f.CategoriaID != nil && (f.CategoriaModo == nil || *f.CategoriaModo != ModoCategoriaRespondeSolo || f.CategoriaActiva == nil || !*f.CategoriaActiva):
		return no(NoAtiendeCategoriaSugiere)
	}
	solo, err := soloSugerir()
	if err != nil {
		return nil, err
	}
	if solo {
		return no(NoAtiendeHorarioSugiere)
	}
	return &AtencionAsistente{Atiende: true}, nil
}

// desdePersona: desde cuándo cuenta «una persona ya escribió» para el asistente: el periodo de la
// conversación o, si alguien le devolvió la conversación después, la reanudación.
func desdePersona(c mensajeriabd.MensajeriaConversacion) time.Time {
	desde := inicioPeriodo(c)
	if c.AsistenteReanudadoEn != nil && c.AsistenteReanudadoEn.After(desde) {
		// Estrictamente después de reanudar (lo escrito en ese mismo instante es de antes).
		return c.AsistenteReanudadoEn.Add(time.Microsecond)
	}
	return desde
}

// EntradaPausarAsistente es POST /conversaciones/{id}/asistente/pausar.
type EntradaPausarAsistente struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Motivo  string `json:"motivo,omitempty" enum:"manual,caso_delicado,respuesta_incorrecta,cliente_lo_pidio" doc:"Por qué (lista cerrada). Ausente = manual"`
}

func nuevoPausarAsistente(m *Modulo) *auditoria.Caso[EntradaPausarAsistente, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaPausarAsistente, ConversacionClienteDetalle]{
		Accion: "conversaciones.pausar_asistente", Permiso: nucleo.PermConversacionesPausarAsistente,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaPausarAsistente) (ConversacionClienteDetalle, error) {
			motivo := in.Motivo
			if motivo == "" {
				motivo = PausaManual
			}
			switch motivo {
			case PausaManual, PausaCasoDelicado, PausaRespuestaIncorrecta, PausaClienteLoPidio:
			default:
				return ConversacionClienteDetalle{}, campoConv("motivo", "manual, caso_delicado, respuesta_incorrecta o cliente_lo_pidio", "enum")
			}
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.Estado == EstadoConvResuelta {
					return reglaConv("conversacion_resuelta", "La conversación está resuelta: el asistente no la está atendiendo")
				}
				if c.AsistentePausadoEn != nil {
					return nil
				}
				atendia := c.AsistenteEstado != nil && *c.AsistenteEstado == AsistenteAtendiendo
				ahora := m.ahora()
				if err := q.PausarAsistenteConv(ctx, mensajeriabd.PausarAsistenteConvParams{ID: c.ID, Ahora: &ahora, Usuario: &a.usuario, Motivo: &motivo}); err != nil {
					return err
				}
				// El turno que estuviera corriendo ya no envía nada.
				if err := q.DescartarTurnosEnCurso(ctx, mensajeriabd.DescartarTurnosEnCursoParams{ConversacionID: c.ID, Ahora: &ahora}); err != nil {
					return err
				}
				t.RegistrarCambios("conversacion", c.ID, map[string]any{"asistente_pausado": false}, map[string]any{"asistente_pausado": true, "asistente_pausa_motivo": motivo})
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "asistente_pausado", ActorNombre: actorNombre(ctx, q, a.usuario), Detalle: &motivo}); err != nil {
					return err
				}
				return t.Emitir("conversacion.asistente_pausado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "actor_id": a.usuario,
					"motivo": motivo, "atendia": atendia, "asignado_a": c.AsignadoA})
			})
		},
	})
}

func nuevoReanudarAsistente(m *Modulo) *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]{
		Accion: "conversaciones.reanudar_asistente", Permiso: nucleo.PermConversacionesPausarAsistente,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAccionConv) (ConversacionClienteDetalle, error) {
			return m.accionConv(ctx, t, in.ID, in.Version, func(a alcanceConv, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion) error {
				if c.Estado == EstadoConvResuelta {
					return reglaConv("conversacion_resuelta", "La conversación está resuelta: al reabrirse el asistente vuelve a lo que diga la empresa")
				}
				de := ""
				switch {
				case c.AsistentePausadoEn != nil:
					de = NoAtiendePausado
				case c.AsistenteEstado != nil && *c.AsistenteEstado == AsistenteEsperaPersona:
					de = NoAtiendeEsperaPersona
				case c.AsistenteEstado != nil && *c.AsistenteEstado == AsistenteDetenido:
					de = NoAtiendePersonaPresente
				default:
					// Sin pausa y sin estado: ¿una persona le escribió al cliente en este periodo?
					escribio, err := q.PersonaEscribioDesde(ctx, mensajeriabd.PersonaEscribioDesdeParams{ConversacionID: c.ID, Desde: desdePersona(*c)})
					if err != nil {
						return err
					}
					if !escribio {
						return nil
					}
					de = NoAtiendePersonaPresente
				}
				ahora := m.ahora()
				if err := q.ReanudarAsistenteConv(ctx, mensajeriabd.ReanudarAsistenteConvParams{ID: c.ID, Ahora: &ahora}); err != nil {
					return err
				}
				// Es la decisión de una persona: el bot de menú deja la conversación.
				if err := limpiarEspera(ctx, q, c); err != nil {
					return err
				}
				// Si el cliente está esperando y nadie la tiene, el asistente contesta ese mensaje ahora (el
				// turno que se descartó por la persona vuelve a quedar pendiente).
				if c.AsignadoA == nil && c.Estado == EstadoConvAbierta {
					ultimo, err := q.UltimoEntranteConv(ctx, c.ID)
					if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						return err
					}
					if err == nil {
						if _, err := q.ReabrirTurnoDescartado(ctx, mensajeriabd.ReabrirTurnoDescartadoParams{ConversacionID: c.ID, MensajeID: ultimo.ID}); err != nil {
							return err
						}
						aj, err := leerAjustesAsistente(ctx, q, empresaDeConv(*c))
						if err != nil {
							return err
						}
						if aj != nil && aj.Activo {
							if err := m.encolar(ctx, t.Tx, ArgsAsistenteSugerir{ConversacionID: c.ID}); err != nil {
								return err
							}
						}
					}
				}
				t.RegistrarCambios("conversacion", c.ID, map[string]any{"asistente": de}, map[string]any{"asistente": "reanudado"})
				if err := m.sistema(ctx, t, c, EventoSistema{Tipo: "asistente_reanudado", ActorNombre: actorNombre(ctx, q, a.usuario), Detalle: &de}); err != nil {
					return err
				}
				return t.Emitir("conversacion.asistente_reanudado", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "actor_id": a.usuario,
					"de": de, "asignado_a": c.AsignadoA})
			})
		},
	})
}
