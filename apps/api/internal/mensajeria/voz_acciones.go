package mensajeria

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Contestar una llamada entrante (§3.3): reclamar (atómico) → contestar (pre_accept) →
// confirmar-medio (accept) → colgar, con soltar y rechazar. Las llamadas a Meta van en dos fases:
// la intención se guarda en una transacción, Meta se llama sin transacción abierta y el resultado
// se guarda en otra.

// casosVoz reúne los casos de uso de la voz.
type casosVoz struct {
	Reclamar          *auditoria.Caso[EntradaLlamada, LlamadaReclamada]
	contestar         *auditoria.Caso[EntradaContestar, Llamada]
	confirmarMedio    *auditoria.Caso[EntradaLlamada, Llamada]
	Soltar            *auditoria.Caso[EntradaSoltar, Llamada]
	colgar            *auditoria.Caso[EntradaLlamada, Llamada]
	rechazar          *auditoria.Caso[EntradaRechazar, Llamada]
	resultadoMeta     *auditoria.Caso[resultadoMeta, bool]
	Anotar            *auditoria.Caso[EntradaAnotarLlamada, Llamada]
	Ver               *auditoria.Caso[EntradaLlamada, Llamada]
	Activa            *auditoria.Caso[EntradaActiva, *Llamada]
	Listar            *auditoria.Caso[EntradaListarLlamadas, nucleo.Pagina[Llamada]]
	Recibir           *auditoria.Caso[EntradaRecibir, RecibirLlamadas]
	VerRecibir        *auditoria.Caso[EntradaVerRecibir, RecibirLlamadas]
	Devoluciones      *auditoria.Caso[EntradaListarDevoluciones, nucleo.Pagina[DevolucionLlamada]]
	Descartar         *auditoria.Caso[EntradaDescartarDevolucion, DevolucionLlamada]
	VerCanal          *auditoria.Caso[EntradaIDConv, LlamadasCanal]
	ConfigCanal       *auditoria.Caso[EntradaLlamadasCanal, LlamadasCanal]
	comprobar         *auditoria.Caso[entradaComprobado, LlamadasCanal]
	prepararCanal     *auditoria.Caso[EntradaIDConv, canalPorComprobar]
	VerAjustes        *auditoria.Caso[struct{}, AjustesVoz]
	FijarAjustes      *auditoria.Caso[EntradaAjustesVoz, AjustesVoz]
	resultadoAjustes  *auditoria.Caso[resultadoAjustesMeta, bool]
	reintentarAjustes *auditoria.Caso[EntradaIDConv, LlamadasCanal]
	VerPermiso        *auditoria.Caso[EntradaConvVoz, PermisoLlamada]
	SolicitarPermiso  *auditoria.Caso[EntradaConvVoz, PermisoLlamada]
	previoLlamar      *auditoria.Caso[EntradaLlamar, previoLlamar]
	llamar            *auditoria.Caso[EntradaLlamar, Llamada]
	resultadoLlamar   *auditoria.Caso[resultadoLlamar, bool]
	RespuestaSDP      *auditoria.Caso[EntradaLlamada, RespuestaSDP]
	resolverSolicitud *auditoria.Caso[solicitudResuelta, bool]
	mantener          *auditoria.Caso[uuid.UUID, mantenimientoLlamada]
	vencer            *auditoria.Caso[struct{}, int]
}

func nuevosCasosVoz(m *Modulo) casosVoz {
	return casosVoz{Reclamar: nuevoReclamarLlamada(m), contestar: nuevoContestarLlamada(m), confirmarMedio: nuevoConfirmarMedio(m),
		Soltar: nuevoSoltarLlamada(m), colgar: nuevoColgarLlamada(m), rechazar: nuevoRechazarLlamada(m), resultadoMeta: nuevoResultadoMeta(m),
		Anotar: nuevoAnotarLlamada(m), Ver: nuevoVerLlamada(m), Activa: nuevoLlamadaActiva(m), Listar: nuevoListarLlamadas(m),
		Recibir: nuevoRecibirLlamadas(m), VerRecibir: nuevoVerRecibir(m), Devoluciones: nuevoListarDevoluciones(m), Descartar: nuevoDescartarDevolucion(m),
		VerCanal: nuevoVerLlamadasCanal(m), ConfigCanal: nuevoConfigurarLlamadasCanal(m), comprobar: nuevoGuardarComprobacion(m),
		prepararCanal: nuevoPrepararComprobacion(m), VerAjustes: nuevoVerAjustesVoz(m), FijarAjustes: nuevoFijarAjustesVoz(m),
		VerPermiso: nuevoVerPermisoLlamada(m), SolicitarPermiso: nuevoSolicitarPermisoLlamada(m), previoLlamar: nuevoPrevioLlamar(m),
		llamar: nuevoLlamar(m), resultadoLlamar: nuevoResultadoLlamar(m), RespuestaSDP: nuevoRespuestaSDP(m), resolverSolicitud: nuevoResolverSolicitud(m),
		mantener: nuevoMantenerLlamada(m), vencer: nuevoVencerDevoluciones(m), resultadoAjustes: nuevoResultadoAjustesMeta(m),
		reintentarAjustes: nuevoReintentarAjustesMeta(m)}
}

// EntradaLlamada identifica una llamada.
type EntradaLlamada struct {
	ID uuid.UUID `json:"id"`
	// Navegador: el id de «Recibir llamadas» de este navegador (cabecera X-Iris-Navegador). La reserva
	// de reclamar queda atada a él.
	Navegador string `json:"navegador,omitempty"`
	// Pestana: el id de la pestaña (X-Iris-Pestana), en respuesta-sdp.
	Pestana string `json:"pestana,omitempty"`
	// SoloPropia (colgar): solo si la llamada es de quien lo pide; no vale supervisar. Es lo que manda
	// el panel al cerrarse la pestaña, para no colgar la llamada de un compañero.
	SoloPropia bool `json:"soloPropia,omitempty"`
}

// LlamadaReclamada es la respuesta de reclamar: la oferta SDP solo viaja aquí y solo a quien la ganó.
type LlamadaReclamada struct {
	Llamada   Llamada    `json:"llamada"`
	SDPOferta string     `json:"sdpOferta" doc:"La oferta SDP de Meta para el RTCPeerConnection del navegador"`
	ICE       ICELlamada `json:"ice"`
	VenceEn   time.Time  `json:"venceEn" doc:"Hasta cuándo vale la reserva: hay que mandar contestar antes"`
}

// ICELlamada: servidores ICE para el navegador (vacío: Meta es ICE-lite con IP pública).
type ICELlamada struct {
	Servidores []string `json:"servidores"`
}

// EntradaContestar es POST /llamadas/{id}/contestar.
type EntradaContestar struct {
	ID           uuid.UUID `json:"id"`
	Navegador    string    `json:"navegador,omitempty"`
	SDPRespuesta string    `json:"-"`
	// Huella: lo único del SDP que queda en la auditoría (la del SDP saneado, el que va a Meta).
	Huella string `json:"sdp_sha256"`
}

// EntradaSoltar es POST /llamadas/{id}/soltar.
type EntradaSoltar struct {
	ID        uuid.UUID `json:"id"`
	Navegador string    `json:"navegador,omitempty"`
	Motivo    string    `json:"motivo,omitempty"`
}

// EntradaRechazar es POST /llamadas/{id}/rechazar.
type EntradaRechazar struct {
	ID        uuid.UUID `json:"id"`
	Motivo    string    `json:"motivo,omitempty"`
	ParaTodos bool      `json:"paraTodos"`
}

// resultadoMeta es la tercera fase de una acción con Meta.
type resultadoMeta struct {
	ID     uuid.UUID `json:"id"`
	Accion string    `json:"accion"`
	Fallo  bool      `json:"fallo"`
}

// llamadaDeQuienLaReclamo: después de reclamar la llamada es de quien la reclamó (M1), en el
// navegador donde la reclamó. Cualquier otro recibe 404, también un supervisor; la misma persona en
// otro navegador, 409 con la regla otro_navegador.
func llamadaDeQuienLaReclamo(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id uuid.UUID, navegador string) (mensajeriabd.MensajeriaLlamada, error) {
	l, err := q.LlamadaParaActualizar(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, llamadaNoEncontrada()
	}
	if err != nil {
		return l, err
	}
	if derefEmpresa(l.EmpresaID) != a.empresa {
		return l, llamadaNoEncontrada()
	}
	if llamadaFinal(l.Estado) && l.UsuarioID != nil && *l.UsuarioID == a.usuario {
		return l, llamadaTerminada()
	}
	if l.ReclamadaPor == nil || *l.ReclamadaPor != a.usuario {
		return l, llamadaNoEncontrada()
	}
	if l.ReclamadaNavegador == nil || *l.ReclamadaNavegador != navegador {
		return l, otroNavegador()
	}
	return l, nil
}

func nuevoReclamarLlamada(m *Modulo) *auditoria.Caso[EntradaLlamada, LlamadaReclamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamada, LlamadaReclamada]{
		Accion: "llamadas.reclamar", Permiso: nucleo.PermLlamadasContestar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamada) (LlamadaReclamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasContestar)
			if err != nil {
				return LlamadaReclamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			ahora := m.ahora()
			// Repetir (mismo navegador, misma Idempotency-Key): quien ya la tiene recibe otra vez la
			// oferta, releída de la llamada. Desde otro navegador de la misma persona, no: 409 misma_persona.
			if l, err := q.LlamadaPorID(ctx, in.ID); err == nil && derefEmpresa(l.EmpresaID) == a.empresa && l.Estado == LlamadaConectando &&
				l.ReclamadaPor != nil && *l.ReclamadaPor == a.usuario && l.ReclamadaNavegador != nil && *l.ReclamadaNavegador == in.Navegador && l.SdpOferta != nil && l.PreAceptadaEn == nil && l.ReclamadaHasta != nil && ahora.Before(*l.ReclamadaHasta) {
				return m.reclamada(ctx, q, a, l)
			}
			// Sin «Recibir llamadas» encendido y un flujo vivo no se puede contestar a tiempo.
			vivo, err := q.ReceptorVivo(ctx, mensajeriabd.ReceptorVivoParams{Empresa: a.empresa, UsuarioID: a.usuario, VivoDesde: ahora.Add(-FlujoVivo)})
			if err != nil {
				return LlamadaReclamada{}, err
			}
			// Sin el id del navegador no hay reserva (a quien no es candidato le toca su 404 de siempre).
			if in.Navegador == "" {
				if candidato, err := q.EsCandidato(ctx, mensajeriabd.EsCandidatoParams{ID: in.ID, UsuarioID: a.usuario, Empresa: a.empresa}); err != nil {
					return LlamadaReclamada{}, err
				} else if candidato {
					return LlamadaReclamada{}, reglaVoz("navegador_requerido", "Falta la cabecera X-Iris-Navegador con el id de este navegador")
				}
			}
			l, err := q.ReclamarLlamada(ctx, mensajeriabd.ReclamarLlamadaParams{ID: in.ID, Empresa: a.empresa, UsuarioID: &a.usuario, Hasta: ptrT(ahora.Add(ReservaLlamada)),
				Ahora: ahora, Navegador: in.Navegador})
			if errors.Is(err, pgx.ErrNoRows) {
				// ¿No es candidato (404), o alguien se adelantó o ya no suena (409)?
				candidato, err := q.EsCandidato(ctx, mensajeriabd.EsCandidatoParams{ID: in.ID, UsuarioID: a.usuario, Empresa: a.empresa})
				if err != nil {
					return LlamadaReclamada{}, err
				}
				actual, err := q.LlamadaPorID(ctx, in.ID)
				if err != nil || derefEmpresa(actual.EmpresaID) != a.empresa {
					return LlamadaReclamada{}, llamadaNoEncontrada()
				}
				misma := actual.ReclamadaPor != nil && *actual.ReclamadaPor == a.usuario || actual.UsuarioID != nil && *actual.UsuarioID == a.usuario
				fue, err := q.FueCandidato(ctx, mensajeriabd.FueCandidatoParams{ID: in.ID, UsuarioID: a.usuario, Empresa: a.empresa})
				if err != nil {
					return LlamadaReclamada{}, err
				}
				if !fue && !misma {
					return LlamadaReclamada{}, llamadaNoEncontrada()
				}
				// Le sonó y ya terminó (perdida, rechazada, contestada por otro y colgada): ya no suena.
				if llamadaFinal(actual.Estado) {
					return LlamadaReclamada{}, llamadaYaContestada("", false)
				}
				// Sigue viva pero a esta persona ya no le suena (la rechazó o soltó dos veces).
				if !candidato && !misma {
					return LlamadaReclamada{}, llamadaNoEncontrada()
				}
				// Suena y es candidato, pero no la pudo reclamar: tiene otra llamada viva, o hay una acción
				// con Meta a medias de la reserva anterior, o ya se acabó el tiempo del timbre.
				if actual.Estado == LlamadaSonando {
					if otra, err := q.LlamadaActivaDeUsuario(ctx, mensajeriabd.LlamadaActivaDeUsuarioParams{Empresa: a.empresa, UsuarioID: &a.usuario}); err == nil && otra.ID != actual.ID {
						return LlamadaReclamada{}, nucleo.Conflicto("llamada_en_curso", "Ya tienes una llamada: termínala antes de contestar otra")
					} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
						return LlamadaReclamada{}, err
					}
					if accionPendiente(actual) {
						return LlamadaReclamada{}, accionEnCurso()
					}
					return LlamadaReclamada{}, llamadaYaContestada("", false)
				}
				quien := ""
				if id := actual.ReclamadaPor; id != nil && !misma {
					quien = nombreUsuario(ctx, q, *id)
				}
				return LlamadaReclamada{}, llamadaYaContestada(quien, misma)
			}
			if err != nil {
				return LlamadaReclamada{}, err
			}
			if !vivo || l.SdpOferta == nil {
				return LlamadaReclamada{}, reglaVoz("no_recibe_llamadas", "Enciende «Recibir llamadas» y deja el panel abierto para contestar")
			}
			// A los demás deja de sonarles.
			if err := m.emitirEventoLlamada(ctx, t, l, EventoLlamadaActualizada, nil); err != nil {
				return LlamadaReclamada{}, err
			}
			return m.reclamada(ctx, q, a, l)
		},
	})
}

func (m *Modulo) reclamada(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, l mensajeriabd.MensajeriaLlamada) (LlamadaReclamada, error) {
	f, err := m.aLlamada(ctx, q, a, l)
	if err != nil {
		return LlamadaReclamada{}, err
	}
	return LlamadaReclamada{Llamada: f, SDPOferta: *l.SdpOferta, ICE: ICELlamada{Servidores: []string{}}, VenceEn: *l.ReclamadaHasta}, nil
}

func ptrT(t time.Time) *time.Time { return &t }

// accionEnCurso: hay una acción con WhatsApp a medias sobre la llamada (un pre_accept o un accept que
// todavía no respondió): mientras tanto no se puede soltar ni reclamar.
func accionEnCurso() *nucleo.Problema {
	return nucleo.Conflicto("accion_en_curso", "WhatsApp todavía no respondió a la acción anterior sobre esta llamada. Vuelve a intentar en unos segundos")
}

func nombreUsuario(ctx context.Context, q *mensajeriabd.Queries, id int64) string {
	ns, err := q.NombresUsuarios(ctx, []int64{id})
	if err != nil || len(ns) == 0 {
		return ""
	}
	return ns[0].Nombre
}

func nuevoContestarLlamada(m *Modulo) *auditoria.Caso[EntradaContestar, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaContestar, Llamada]{
		Accion: "llamadas.contestar", Permiso: nucleo.PermLlamadasContestar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaContestar) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasContestar)
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			l, err := llamadaDeQuienLaReclamo(ctx, q, a, in.ID, in.Navegador)
			if err != nil {
				return Llamada{}, err
			}
			ahora := m.ahora()
			if l.Estado != LlamadaConectando || l.ReclamadaHasta == nil || !ahora.Before(*l.ReclamadaHasta) || l.PreAceptadaEn != nil ||
				(l.AccionMetaEstado != nil && *l.AccionMetaEstado == "pendiente") {
				return Llamada{}, reservaLlamadaVencida()
			}
			// Solo se rechaza lo inválido: las direcciones locales del navegador se quitan (SanearSDP).
			if _, err := SanearSDP(in.SDPRespuesta); err != nil {
				return Llamada{}, sdpInvalido("sdpRespuesta", err.Error())
			}
			intencionMeta(&l, MetaPreAceptar, ahora)
			// La reserva cubre la llamada a Meta y la espera del medio.
			l.ReclamadaHasta = ptrT(ahora.Add(PlazoMeta + EsperaMedio + 2*time.Second))
			if l, err = guardarLlamada(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// Contestar valida y guarda la intención, manda pre_accept a Meta y devuelve la llamada. Si Meta la
// rechaza, la llamada vuelve a sonar y responde 502 meta_rechazo.
func (m *Modulo) Contestar(ctx context.Context, in EntradaContestar) (Llamada, error) {
	// A Meta va el SDP saneado, y su huella es la que queda en la auditoría. Si no es válido, lo dice
	// el caso de uso (después de comprobar el permiso y de quién es la llamada).
	saneado, errSDP := SanearSDP(in.SDPRespuesta)
	if errSDP == nil {
		in.Huella = huellaSDP(saneado)
	}
	if _, err := m.Voz.contestar.Ejecutar(ctx, in); err != nil {
		return Llamada{}, err
	}
	if errSDP != nil {
		return Llamada{}, sdpInvalido("sdpRespuesta", errSDP.Error())
	}
	if err := m.cumplirMeta(ctx, nucleo.BDDe(ctx), in.ID, saneado); err != nil {
		var em *ErrMetaLlamada
		if errors.As(err, &em) {
			return Llamada{}, metaRechazo()
		}
		return Llamada{}, err
	}
	return m.Voz.Ver.Ejecutar(ctx, EntradaLlamada{ID: in.ID})
}

func nuevoConfirmarMedio(m *Modulo) *auditoria.Caso[EntradaLlamada, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamada, Llamada]{
		Accion: "llamadas.confirmar_medio", Permiso: nucleo.PermLlamadasContestar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamada) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasContestar)
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			l, err := llamadaDeQuienLaReclamo(ctx, q, a, in.ID, in.Navegador)
			if err != nil {
				return Llamada{}, err
			}
			ahora := m.ahora()
			if l.Estado != LlamadaConectando || l.PreAceptadaEn == nil || ahora.After(l.PreAceptadaEn.Add(EsperaMedio)) ||
				(l.AccionMetaEstado != nil && *l.AccionMetaEstado == "pendiente") {
				return Llamada{}, reservaLlamadaVencida()
			}
			intencionMeta(&l, MetaAceptar, ahora)
			l.ReclamadaHasta = ptrT(ahora.Add(PlazoMeta + 2*time.Second))
			if l, err = guardarLlamada(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// ConfirmarMedio: el navegador ya tiene el audio conectado; se manda accept y la llamada queda en curso.
func (m *Modulo) ConfirmarMedio(ctx context.Context, in EntradaLlamada) (Llamada, error) {
	if _, err := m.Voz.confirmarMedio.Ejecutar(ctx, in); err != nil {
		return Llamada{}, err
	}
	if err := m.cumplirMeta(ctx, nucleo.BDDe(ctx), in.ID, ""); err != nil {
		var em *ErrMetaLlamada
		if errors.As(err, &em) {
			return Llamada{}, metaRechazo()
		}
		return Llamada{}, err
	}
	return m.Voz.Ver.Ejecutar(ctx, in)
}

// nuevoResultadoMeta guarda lo que respondió Meta (tercera fase). Corre como sistema.
func nuevoResultadoMeta(m *Modulo) *auditoria.Caso[resultadoMeta, bool] {
	return auditoria.Nuevo(auditoria.Caso[resultadoMeta, bool]{
		Accion: "llamadas.resultado_meta", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(cambio bool) bool { return cambio },
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoMeta) (bool, error) {
			if err := soloSistemaVoz(ctx); err != nil {
				return false, err
			}
			q := mensajeriabd.New(t)
			l, err := q.LlamadaParaActualizar(ctx, in.ID)
			if err != nil {
				return false, err
			}
			if l.AccionMeta == nil || *l.AccionMeta != in.Accion || l.AccionMetaEstado == nil || *l.AccionMetaEstado != "pendiente" {
				return false, nil
			}
			t.FijarEmpresa(derefEmpresa(l.EmpresaID))
			t.FijarEntidad("llamada", l.ID)
			ahora := m.ahora()
			estado := "hecha"
			if in.Fallo {
				estado = "fallida"
			}
			l.AccionMetaEstado = &estado
			var c *mensajeriabd.MensajeriaConversacion
			if l.ConversacionID != nil {
				f, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
				if err != nil {
					return false, err
				}
				c = &f
			}
			// El resultado de un pre_accept o un accept solo vale si la llamada sigue conectando en manos de
			// quien tenía la reserva al pedirlo. Si no (se soltó, venció, la tiene otro), no se aplica: la
			// llamada se termina en Meta y queda fallida, sin dejarla a nombre de quien no tiene el audio.
			esReserva := in.Accion == MetaPreAceptar || in.Accion == MetaAceptar
			vigente := l.Estado == LlamadaConectando && l.ReclamadaPor != nil && l.AccionMetaPor != nil && *l.ReclamadaPor == *l.AccionMetaPor
			if esReserva && !vigente && !llamadaFinal(l.Estado) {
				t.Anotar(map[string]any{"descartado": in.Accion})
				return true, m.cerrarLlamada(ctx, t, &l, c, LlamadaFallida, "error_meta", ahora, MetaTerminar)
			}
			switch {
			case llamadaFinal(l.Estado):
				// reject o terminate de una llamada ya cerrada: solo queda anotado. Si Meta no
				// respondió, la intención sigue pendiente y el job de escrituras a Meta la repite (son idempotentes).
				if in.Fallo {
					return false, nil
				}
				_, err = guardarLlamada(ctx, q, l)
				return true, err
			case esReserva && l.ReclamadaPor == nil:
				return false, nil // inalcanzable (vigente lo exige); sin desreferenciar nil
			case in.Accion == MetaPreAceptar && !in.Fallo:
				l.PreAceptadaEn = &ahora
				l.ReclamadaHasta = ptrT(ahora.Add(EsperaMedio))
			case in.Accion == MetaPreAceptar:
				// Meta rechazó el pre_accept: la llamada vuelve a sonar a los demás.
				if err := m.soltar(ctx, t, &l, ahora); err != nil {
					return false, err
				}
				return true, nil
			case in.Accion == MetaAceptar && !in.Fallo:
				persona := "persona"
				l.Estado, l.AtendidaPor, l.UsuarioID, l.ContestadaEn = LlamadaEnCurso, &persona, l.ReclamadaPor, &ahora
				l.ReclamadaPor, l.ReclamadaHasta, l.SdpOferta, l.PreAceptadaEn = nil, nil, nil, nil
				if err := q.RetirarCandidatos(ctx, mensajeriabd.RetirarCandidatosParams{LlamadaID: l.ID, Ahora: &ahora}); err != nil {
					return false, err
				}
				if err := q.InsertarTramo(ctx, mensajeriabd.InsertarTramoParams{LlamadaID: l.ID, UsuarioID: *l.UsuarioID, Comienzo: ahora}); err != nil {
					return false, err
				}
				// Contestada por una persona: cuenta como primera respuesta de la conversación (SLA).
				if c != nil && c.EsperaDesde != nil {
					c.EsperaDesde, c.SlaVenceEn, c.SlaAvisado = nil, nil, false
					if _, err := guardarConv(ctx, q, *c, false); err != nil {
						return false, err
					}
				}
				if err := t.Emitir("llamada.contestada", map[string]any{"llamada_id": l.ID, "conversacion_id": l.ConversacionID, "empresa_id": l.EmpresaID,
					"atendida_por": "persona", "usuario_id": l.UsuarioID, "espera_seg": int(max(0, ahora.Sub(l.SonoEn)/time.Second))}); err != nil {
					return false, err
				}
			case in.Accion == MetaAceptar:
				// No se sabe si Meta la conectó: no se repite a ciegas. Queda fallida y, si sí la
				// había conectado, el terminate siguiente la cierra.
				if err := m.cerrarLlamada(ctx, t, &l, c, LlamadaFallida, "error_meta", ahora, MetaTerminar); err != nil {
					return false, err
				}
				return true, nil
			}
			if l, err = guardarLlamada(ctx, q, l); err != nil {
				return false, err
			}
			return true, m.emitirEventoLlamada(ctx, t, l, EventoLlamadaActualizada, nil)
		},
	})
}

func soloSistemaVoz(ctx context.Context) error {
	if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
		return llamadaNoEncontrada()
	}
	return nil
}

// trasCommitMeta ejecuta, después del COMMIT, la acción con Meta que quedó anotada como intención.
func (m *Modulo) trasCommitMeta(ctx context.Context, t *auditoria.Tx, id uuid.UUID) {
	if ctx.Value(claveSinMeta{}) != nil {
		return
	}
	bd := nucleo.BDDe(ctx)
	t.AlConfirmar(func(ctx context.Context) { _ = m.cumplirMeta(ctx, bd, id, "") })
}

// soltar devuelve la llamada a «sonando» (la reserva se soltó, venció o Meta rechazó el pre_accept)
// y suena otra vez a los candidatos que quedan, mientras haya tiempo. Quien suelta dos veces deja de
// recibir esta llamada.
func (m *Modulo) soltar(ctx context.Context, t *auditoria.Tx, l *mensajeriabd.MensajeriaLlamada, ahora time.Time) error {
	q := mensajeriabd.New(t)
	if l.ReclamadaPor != nil {
		if _, err := q.ContarSuelta(ctx, mensajeriabd.ContarSueltaParams{LlamadaID: l.ID, UsuarioID: *l.ReclamadaPor, Ahora: ahora}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	l.Estado, l.ReclamadaPor, l.ReclamadaHasta, l.PreAceptadaEn = LlamadaSonando, nil, nil, nil
	var err error
	if *l, err = guardarLlamada(ctx, q, *l); err != nil {
		return err
	}
	return m.volverASonar(ctx, t, *l)
}

func nuevoSoltarLlamada(m *Modulo) *auditoria.Caso[EntradaSoltar, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaSoltar, Llamada]{
		Accion: "llamadas.soltar", Permiso: nucleo.PermLlamadasContestar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaSoltar) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasContestar)
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			l, err := llamadaDeQuienLaReclamo(ctx, q, a, in.ID, in.Navegador)
			if err != nil {
				return Llamada{}, err
			}
			if l.Estado != LlamadaConectando {
				return Llamada{}, reservaLlamadaVencida()
			}
			// Con un pre_accept o un accept en vuelo no se suelta: su resultado decide (M1).
			if accionPendiente(l) {
				return Llamada{}, accionEnCurso()
			}
			if err := m.soltar(ctx, t, &l, m.ahora()); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

func nuevoColgarLlamada(m *Modulo) *auditoria.Caso[EntradaLlamada, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamada, Llamada]{
		Accion: "llamadas.colgar", Permiso: nucleo.PermLlamadasColgar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamada) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasColgar)
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			l, err := llamadaVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return Llamada{}, err
			}
			// Quien la tiene (en curso, o reservada a su nombre mientras conecta), o un supervisor. Con
			// soloPropia (cierre de pestaña) supervisar no cuenta: si la reserva propia venció y la
			// llamada ya es de otra persona, no se le cuelga.
			reservada := l.ReclamadaPor != nil && *l.ReclamadaPor == a.usuario
			duena := (l.UsuarioID != nil && *l.UsuarioID == a.usuario) || reservada
			if !duena && (!a.supervisa || in.SoloPropia) {
				return Llamada{}, llamadaNoEncontrada()
			}
			if llamadaFinal(l.Estado) {
				return Llamada{}, llamadaTerminada()
			}
			// Una saliente se puede colgar mientras marca (se cancela: no cuenta como sin contestar).
			marcando := l.Direccion == "saliente" && l.Estado == LlamadaMarcando
			// La pestaña que la estaba contestando se cierra (propia=true) con la llamada conectando a su
			// nombre, quizá con el pre_accept o el accept en vuelo: se termina. cerrarLlamada cambia la
			// intención con Meta a terminate, así que el resultado del accept que llegue después ya no se
			// aplica: no queda «en curso» sin audio ni en un estado intermedio.
			conectandoMia := in.SoloPropia && l.Estado == LlamadaConectando && reservada
			if l.Estado != LlamadaEnCurso && !marcando && !conectandoMia {
				return Llamada{}, nucleo.Conflicto("llamada_sin_contestar", "La llamada todavía no está en curso: usa rechazar o soltar")
			}
			c, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
			if err != nil {
				return Llamada{}, err
			}
			if err := m.cerrarLlamada(ctx, t, &l, &c, LlamadaTerminada, "agente_colgo", m.ahora(), MetaTerminar); err != nil {
				return Llamada{}, err
			}
			if _, err := guardarConv(ctx, q, c, false); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// Colgar termina la llamada en Iris y se lo pide a Meta (terminate es idempotente: si Meta no
// responde, el mantenimiento lo repite).
func (m *Modulo) Colgar(ctx context.Context, in EntradaLlamada) (Llamada, error) {
	l, err := m.Voz.colgar.Ejecutar(ctx, in)
	if err != nil {
		return Llamada{}, err
	}
	_ = m.cumplirMeta(ctx, nucleo.BDDe(ctx), in.ID, "")
	return l, nil
}

func nuevoRechazarLlamada(m *Modulo) *auditoria.Caso[EntradaRechazar, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRechazar, Llamada]{
		Accion: "llamadas.rechazar", Permiso: nucleo.PermLlamadasRechazar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRechazar) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasRechazar)
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			l, err := llamadaVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return Llamada{}, err
			}
			if llamadaFinal(l.Estado) {
				return Llamada{}, llamadaTerminada()
			}
			if l.Estado != LlamadaSonando {
				return Llamada{}, llamadaYaContestada("", false)
			}
			ahora := m.ahora()
			if in.ParaTodos {
				if !a.supervisa {
					return Llamada{}, nucleo.Prohibido("Rechazar la llamada para todos es de quien supervisa la cola")
				}
				c, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
				if err != nil {
					return Llamada{}, err
				}
				l.UsuarioID = &a.usuario
				if err := m.cerrarLlamada(ctx, t, &l, &c, LlamadaRechazada, "rechazada_agente", ahora, MetaRechazar); err != nil {
					return Llamada{}, err
				}
				if _, err := guardarConv(ctx, q, c, false); err != nil {
					return Llamada{}, err
				}
				return m.aLlamada(ctx, q, a, l)
			}
			// Por defecto solo deja de sonarle a esta persona.
			if _, err := q.RetirarCandidato(ctx, mensajeriabd.RetirarCandidatoParams{LlamadaID: l.ID, UsuarioID: a.usuario, Ahora: &ahora, Motivo: nombrePtr("rechazo")}); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// Rechazar: deja de sonarle a quien rechaza o, con paraTodos (supervisor), rechaza la llamada en Meta.
func (m *Modulo) Rechazar(ctx context.Context, in EntradaRechazar) (Llamada, error) {
	l, err := m.Voz.rechazar.Ejecutar(ctx, in)
	if err != nil {
		return Llamada{}, err
	}
	if in.ParaTodos {
		_ = m.cumplirMeta(ctx, nucleo.BDDe(ctx), in.ID, "")
	}
	return l, nil
}
