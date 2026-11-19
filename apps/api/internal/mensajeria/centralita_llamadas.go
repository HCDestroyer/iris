package mensajeria

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Llamadas de la centralita (E4a): lo que pasa con cada evento del AMI y lo que la persona hace
// después (nota, cliente, ticket). Van fuera de la bandeja: en la ficha del cliente y del ticket.

// Plazos para cerrar llamadas que el AMI no cerró (conexión perdida, reinicio de Iris).
const (
	CentralitaSonandoMax = 15 * time.Minute
	CentralitaEnCursoMax = 4 * time.Hour
)

// PersonaCentralita es alguien a quien le sonó o que contestó. Solo el nombre de pila.
type PersonaCentralita struct {
	UsuarioID int64  `json:"usuarioId"`
	Nombre    string `json:"nombre" doc:"Nombre de pila"`
	Extension string `json:"extension"`
}

// CandidatoCentralita es un cliente posible cuando el número es de varios (solo con clientes.ver).
type CandidatoCentralita struct {
	ClienteID int64  `json:"clienteId"`
	Nombre    string `json:"nombre"`
	Codigo    string `json:"codigo"`
}

func nombreDePila(n string) string {
	n = strings.TrimSpace(n)
	if i := strings.IndexAny(n, " \t"); i > 0 {
		return n[:i]
	}
	return n
}

// vistaCentralita: quién mira y qué puede ver o hacer.
type vistaCentralita struct {
	usuario, empresa                                      int64
	verClientes, verTickets, anota, supervisa, candidatos bool
}

func vistaDe(ctx context.Context, candidatos bool) vistaCentralita {
	p := contexto.PermisosDe(ctx)
	return vistaCentralita{usuario: contexto.ActorDe(ctx).ID, empresa: empresaDeCtx(ctx), verClientes: p.Tiene(string(nucleo.PermClientesVer)),
		verTickets: p.Tiene(string(nucleo.PermTicketsVer)), anota: p.Tiene(string(nucleo.PermLlamadasAnotar)),
		supervisa: p.Tiene(string(nucleo.PermCentralitaAsignarExtension)), candidatos: candidatos}
}

// llamadasCentralitaDelFeed arma las llamadas de los eventos dirigidos (sin conversación) del feed.
func (m *Modulo) llamadasCentralitaDelFeed(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, filas []mensajeriabd.MensajeriaConvEvento) (map[uuid.UUID]Llamada, error) {
	var ids []uuid.UUID
	for _, f := range filas {
		if id := uuidDe(f.LlamadaID); id != nil && f.ConversacionID == nil {
			ids = append(ids, *id)
		}
	}
	out := map[uuid.UUID]Llamada{}
	if len(ids) == 0 {
		return out, nil
	}
	fs, err := q.LlamadasCentralitaPorIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	ls := make([]mensajeriabd.MensajeriaLlamada, 0, len(fs))
	for _, f := range fs {
		if derefEmpresa(f.EmpresaID) == a.empresa {
			ls = append(ls, f)
		}
	}
	p := contexto.PermisosDe(ctx)
	v, err := m.vistaLlamadas(ctx, q, ls, vistaCentralita{usuario: a.usuario, empresa: a.empresa, verClientes: a.verClientes,
		verTickets: p.Tiene(string(nucleo.PermTicketsVer)), anota: a.anotaLlamadas, supervisa: a.supervisaExtensiones, candidatos: true})
	if err != nil {
		return nil, err
	}
	for _, l := range v {
		out[l.ID] = l
	}
	return out, nil
}

// vistaLlamadas arma las llamadas de la centralita como el mismo Llamada de WhatsApp (los mismos
// componentes del panel). Nombres de clientes y servicio solo con clientes.ver; ticket abierto con
// tickets.ver.
func (m *Modulo) vistaLlamadas(ctx context.Context, q *mensajeriabd.Queries, filas []mensajeriabd.MensajeriaLlamada, vv vistaCentralita) ([]Llamada, error) {
	if len(filas) == 0 {
		return []Llamada{}, nil
	}
	ids := make([]uuid.UUID, 0, len(filas))
	var usuarios []int64
	for _, f := range filas {
		ids = append(ids, f.ID)
		if f.UsuarioID != nil {
			usuarios = append(usuarios, *f.UsuarioID)
		}
	}
	rs, err := q.ReceptoresDeLlamadas(ctx, ids)
	if err != nil {
		return nil, err
	}
	receptores := map[uuid.UUID][]PersonaCentralita{}
	for _, r := range rs {
		receptores[r.LlamadaID] = append(receptores[r.LlamadaID], PersonaCentralita{UsuarioID: r.UsuarioID, Nombre: nombreDePila(r.Nombre), Extension: r.Extension})
	}
	nombres := map[int64]string{}
	if len(usuarios) > 0 {
		ns, err := q.NombresUsuarios(ctx, usuarios)
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			nombres[n.ID] = n.Nombre
		}
	}
	out := make([]Llamada, 0, len(filas))
	for _, f := range filas {
		if f.Origen != "centralita" || f.CentralitaID == nil {
			continue
		}
		c := &CentralitaDeLlamada{ID: *f.CentralitaID, Extension: f.Extension, Receptores: receptores[f.ID], Coincidencias: f.Coincidencias,
			Candidatos: []CandidatoCentralita{}, Asociacion: f.Asociacion}
		if c.Receptores == nil {
			c.Receptores = []PersonaCentralita{}
		}
		l := Llamada{ID: f.ID, EmpresaID: f.EmpresaID, Origen: f.Origen, Direccion: f.Direccion, Numero: f.Numero, Titulo: f.Numero,
			Contacto: ContactoDeLlamada{Tipo: "desconocido"}, Estado: f.Estado, AtendidaPor: f.AtendidaPor, Tramos: []TramoLlamada{}, SonoEn: f.SonoEn,
			ContestadaEn: f.ContestadaEn, TerminadaEn: f.TerminadaEn, DuracionSeg: f.DuracionSeg, MotivoFin: f.MotivoFin, Nota: f.Nota,
			Resultado: f.Resultado, TicketID: f.TicketID, Centralita: c, Version: f.Version}
		if f.Numero == "" {
			l.Titulo = "Número oculto"
		}
		if f.UsuarioID != nil {
			// Solo el nombre de pila de quien contestó (o marcó).
			l.Usuario = &UsuarioRef{ID: *f.UsuarioID, Nombre: nombreDePila(nombres[*f.UsuarioID])}
		}
		mia := f.UsuarioID != nil && *f.UsuarioID == vv.usuario
		for _, r := range c.Receptores {
			if r.UsuarioID == vv.usuario {
				ext := r.Extension
				mia, c.TuExtension = true, &ext
			}
		}
		if f.Direccion == "saliente" && mia {
			c.TuExtension = f.Extension
		}
		puedeActuar := vv.anota && (mia || vv.supervisa)
		l.Puede = PuedeLlamada{Anotar: puedeActuar}
		c.Asociar = puedeActuar && vv.verClientes
		if f.ClienteID != nil {
			l.Contacto = ContactoDeLlamada{Tipo: "cliente", ClienteID: f.ClienteID, ServicioID: f.ServicioID}
			if vv.verClientes {
				cl, err := q.ClienteInstantanea(ctx, *f.ClienteID)
				if err == nil {
					l.Titulo = cl.Nombre
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return nil, err
				}
			}
			if !llamadaFinal(f.Estado) {
				s, err := senalesDe(ctx, q, derefEmpresa(f.EmpresaID), *f.ClienteID, f.ServicioID, vv.verClientes, vv.verTickets)
				if err != nil {
					return nil, err
				}
				l.Senales = s
			}
		} else if vv.verClientes && vv.candidatos && f.Coincidencias > 1 && f.Numero != "" {
			cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(f.Numero), Empresa: derefEmpresa(f.EmpresaID)})
			if err != nil {
				return nil, err
			}
			for _, x := range cs {
				c.Candidatos = append(c.Candidatos, CandidatoCentralita{ClienteID: x.ID, Nombre: x.Nombre, Codigo: x.Codigo})
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// Procesar los hechos del AMI (actor sistema) -------------------------------------------------------

type entradaHechoAMI struct {
	CentralitaID int64    `json:"centralita_id"`
	Hecho        hechoAMI `json:"-"`
	Tipo         string   `json:"tipo"`
	Extension    string   `json:"extension,omitempty"`
}

func nuevoProcesarHechoAMI(m *Modulo) *auditoria.Caso[entradaHechoAMI, bool] {
	return auditoria.Nuevo(auditoria.Caso[entradaHechoAMI, bool]{
		Accion: "centralita.evento", Acceso: auditoria.Autenticado,
		// Solo deja fila cuando cambió una llamada (los eventos de la centralita son muchos).
		AuditarExito: func(cambio bool) bool { return cambio },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaHechoAMI) (bool, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return false, centralitaNoEncontrada()
			}
			q := mensajeriabd.New(t)
			c, err := q.CentralitaDelMotor(ctx, in.CentralitaID)
			if err != nil {
				return false, err
			}
			if !c.Activa {
				return false, nil
			}
			t.FijarEmpresa(derefEmpresa(c.EmpresaID))
			h := in.Hecho
			if err := q.CandadoChat(ctx, "centralita:"+strconv.FormatInt(c.ID, 10)+":"+h.linkedid); err != nil {
				return false, err
			}
			ahora := m.ahora()
			switch h.tipo {
			case hechoSuena:
				if h.interno {
					return false, nil
				}
				return m.centralitaSuena(ctx, t, q, c, h, ahora)
			case hechoMarca:
				return m.centralitaMarca(ctx, t, q, c, h, ahora)
			}
			l, err := q.LlamadaCentralitaPorExterno(ctx, mensajeriabd.LlamadaCentralitaPorExternoParams{CentralitaID: c.ID, IDExterno: h.linkedid})
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			t.FijarEntidad("centralita_llamada", l.ID)
			switch h.tipo {
			case hechoContesta:
				if l.Estado != LlamadaSonando && l.Estado != LlamadaMarcando {
					return false, nil
				}
				x, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: c.ID, Extension: h.extension})
				if errors.Is(err, pgx.ErrNoRows) {
					return false, nil
				}
				if err != nil {
					return false, err
				}
				if l.Direccion == "entrante" {
					l.UsuarioID, l.Extension = &x.UsuarioID, &x.Extension
				}
				l.Estado, l.ContestadaEn, l.AtendidaPor = LlamadaEnCurso, &ahora, texto("persona")
				if l, err = guardarLlamadaCentralita(ctx, q, l); err != nil {
					return false, err
				}
				espera := int(ahora.Sub(l.SonoEn).Seconds())
				if err := t.Emitir("llamada.contestada", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "origen": "centralita",
					"centralita_id": c.ID, "atendida_por": "persona", "usuario_id": l.UsuarioID, "espera_seg": max(espera, 0)}); err != nil {
					return false, err
				}
				return true, m.avisarReceptores(ctx, q, l)
			case hechoCuelga:
				return m.cerrarLlamadaCentralita(ctx, t, q, l, ahora, nil)
			case hechoRegistro:
				if l.Estado == LlamadaSonando || l.Estado == LlamadaMarcando || l.Estado == LlamadaEnCurso {
					if h.contestada && l.Estado != LlamadaEnCurso {
						// El CDR dice que se contestó y no vimos el DialEnd: queda contestada sin persona.
						l.Estado, l.ContestadaEn, l.AtendidaPor = LlamadaEnCurso, &ahora, texto("persona")
					}
					seg := int32(h.segundos) //nolint:gosec // acotado a 24 h
					return m.cerrarLlamadaCentralita(ctx, t, q, l, ahora, &seg)
				}
				if l.Estado == LlamadaTerminada && h.segundos > 0 {
					seg := int32(h.segundos) //nolint:gosec // acotado a 24 h
					if l.DuracionSeg == nil || *l.DuracionSeg != seg {
						l.DuracionSeg = &seg
						_, err := guardarLlamadaCentralita(ctx, q, l)
						return false, err
					}
				}
			}
			return false, nil
		},
	})
}

func (m *Modulo) centralitaSuena(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCentralita, h hechoAMI, ahora time.Time) (bool, error) {
	x, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: c.ID, Extension: h.extension})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // extensión no registrada: no se guarda nada
	}
	if err != nil {
		return false, err
	}
	l, err := q.LlamadaCentralitaPorExterno(ctx, mensajeriabd.LlamadaCentralitaPorExternoParams{CentralitaID: c.ID, IDExterno: h.linkedid})
	nueva := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !nueva {
		return false, err
	}
	if nueva {
		p, err := m.paramsLlamadaCentralita(ctx, q, c, h, "entrante", ahora)
		if err != nil {
			return false, err
		}
		l, err = q.InsertarLlamadaCentralita(ctx, p)
		if err != nil {
			return false, err
		}
		t.FijarEntidad("centralita_llamada", l.ID)
		if err := t.Emitir("llamada.entrante", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "canal_id": nil,
			"centralita_id": c.ID, "cola_id": nil, "cliente_id": l.ClienteID, "origen": "centralita"}); err != nil {
			return false, err
		}
	} else {
		t.FijarEntidad("centralita_llamada", l.ID)
		if l.Direccion != "entrante" || l.Estado != LlamadaSonando {
			return false, nil
		}
	}
	r, err := q.InsertarReceptorCentralita(ctx, mensajeriabd.InsertarReceptorCentralitaParams{LlamadaID: l.ID, UsuarioID: x.UsuarioID, EmpresaID: l.EmpresaID,
		Extension: x.Extension, SonoEn: ahora})
	if errors.Is(err, pgx.ErrNoRows) {
		return nueva, nil // ya le sonaba (otro DialBegin de la misma llamada)
	}
	if err != nil {
		return false, err
	}
	return true, m.eventoCentralita(ctx, q, l, []int64{r}, EventoLlamadaEntrante)
}

func (m *Modulo) centralitaMarca(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCentralita, h hechoAMI, ahora time.Time) (bool, error) {
	x, err := q.ExtensionActivaPorNumero(ctx, mensajeriabd.ExtensionActivaPorNumeroParams{CentralitaID: c.ID, Extension: h.extension})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	p, err := m.paramsLlamadaCentralita(ctx, q, c, h, "saliente", ahora)
	if err != nil {
		return false, err
	}
	// Solo se guardan las salientes a un cliente (las personales no quedan en Iris).
	if p.Coincidencias == 0 {
		return false, nil
	}
	p.UsuarioID, p.Extension, p.Estado = &x.UsuarioID, &x.Extension, LlamadaMarcando
	l, err := q.InsertarLlamadaCentralita(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // ya registrada
	}
	if err != nil {
		return false, err
	}
	t.FijarEntidad("centralita_llamada", l.ID)
	return true, t.Emitir("llamada.saliente_iniciada", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "canal_id": nil,
		"centralita_id": c.ID, "usuario_id": x.UsuarioID, "devolucion_id": nil, "origen": "centralita"})
}

// paramsLlamadaCentralita asocia el número a un cliente con la regla de §3.4 del chat: una sola
// coincidencia en la instantánea de la empresa (y su servicio si tiene uno solo); si no, sin cliente.
func (m *Modulo) paramsLlamadaCentralita(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaCentralita, h hechoAMI, direccion string,
	ahora time.Time) (mensajeriabd.InsertarLlamadaCentralitaParams, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return mensajeriabd.InsertarLlamadaCentralitaParams{}, err
	}
	p := mensajeriabd.InsertarLlamadaCentralitaParams{ID: id, EmpresaID: c.EmpresaID, CentralitaID: c.ID, IDExterno: h.linkedid, Direccion: direccion,
		Numero: h.numero, Estado: LlamadaSonando, SonoEn: ahora, SuenaHasta: ahora.Add(CentralitaSonandoMax)}
	if h.numero == "" {
		return p, nil
	}
	cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: soloDigitos(h.numero), Empresa: derefEmpresa(c.EmpresaID)})
	if err != nil {
		return p, err
	}
	p.Coincidencias = int32(len(cs)) //nolint:gosec // como mucho 10
	if len(cs) == 1 {
		p.ClienteID, p.Asociacion = &cs[0].ID, texto("automatica")
		ss, err := q.ServiciosInstantanea(ctx, cs[0].ID)
		if err != nil {
			return p, err
		}
		if len(ss) == 1 {
			p.ServicioID = &ss[0].ID
		}
	}
	return p, nil
}

func guardarLlamadaCentralita(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada) (mensajeriabd.MensajeriaLlamada, error) {
	return q.GuardarLlamadaCentralita(ctx, mensajeriabd.GuardarLlamadaCentralitaParams{ID: l.ID, Estado: l.Estado, UsuarioID: l.UsuarioID, Extension: l.Extension,
		ContestadaEn: l.ContestadaEn, TerminadaEn: l.TerminadaEn, DuracionSeg: l.DuracionSeg, ClienteID: l.ClienteID, ServicioID: l.ServicioID,
		AtendidaPor: l.AtendidaPor, MotivoFin: l.MotivoFin, Asociacion: l.Asociacion, Nota: l.Nota, Resultado: l.Resultado, AnotadaPor: l.AnotadaPor, TicketID: l.TicketID})
}

// cerrarLlamadaCentralita: terminada si se contestó, perdida si no. seg: los del CDR si llegaron.
func (m *Modulo) cerrarLlamadaCentralita(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada,
	ahora time.Time, seg *int32) (bool, error) {
	if l.Estado != LlamadaSonando && l.Estado != LlamadaMarcando && l.Estado != LlamadaEnCurso {
		return false, nil
	}
	l.TerminadaEn = &ahora
	evento := "llamada.perdida"
	if l.Estado == LlamadaEnCurso {
		l.Estado, evento = LlamadaTerminada, "llamada.terminada"
		d := seg
		if d == nil && l.ContestadaEn != nil {
			x := int32(max(ahora.Sub(*l.ContestadaEn).Seconds(), 0))
			d = &x
		}
		l.DuracionSeg = d
	} else {
		l.Estado, l.MotivoFin = LlamadaPerdida, texto("nadie_contesto")
	}
	l, err := guardarLlamadaCentralita(ctx, q, l)
	if err != nil {
		return false, err
	}
	payload := map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "origen": "centralita", "centralita_id": l.CentralitaID,
		"direccion": l.Direccion}
	if evento == "llamada.terminada" {
		payload["atendida_por"], payload["usuario_id"], payload["duracion_seg"], payload["motivo_fin"], payload["tramos"] = "persona", l.UsuarioID, l.DuracionSeg, nil, 1
	} else {
		payload["cola_id"], payload["sono_seg"], payload["devolucion_id"] = nil, int(max(ahora.Sub(l.SonoEn).Seconds(), 0)), nil
	}
	if err := t.Emitir(evento, payload); err != nil {
		return false, err
	}
	return true, m.avisarReceptores(ctx, q, l)
}

// avisarReceptores pone la llamada en el feed de cada persona a la que le sonó.
func (m *Modulo) avisarReceptores(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada) error {
	if l.Direccion != "entrante" {
		return nil
	}
	rs, err := q.ReceptoresCentralita(ctx, l.ID)
	if err != nil {
		return err
	}
	us := make([]int64, 0, len(rs))
	for _, r := range rs {
		us = append(us, r.UsuarioID)
	}
	tipo := EventoLlamadaActualizada
	if llamadaFinal(l.Estado) {
		tipo = EventoLlamadaTerminada
	}
	return m.eventoCentralita(ctx, q, l, us, tipo)
}

// eventoCentralita escribe un evento dirigido por usuario (sin conversación) y despierta los flujos.
func (m *Modulo) eventoCentralita(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada, usuarios []int64, tipo string) error {
	if len(usuarios) == 0 {
		return nil
	}
	clave := strconv.FormatInt(derefEmpresa(l.EmpresaID), 10)
	if err := q.CandadoChat(ctx, "conv:eventos:"+clave); err != nil {
		return err
	}
	for _, u := range usuarios {
		if _, err := q.InsertarEventoCentralita(ctx, mensajeriabd.InsertarEventoCentralitaParams{EmpresaID: l.EmpresaID, Tipo: tipo, UsuarioID: u, OcurridoEn: m.ahora(),
			LlamadaID: l.ID}); err != nil {
			return err
		}
	}
	return q.NotificarConv(ctx, clave)
}

func nuevoCerrarViejasCentralita(m *Modulo) *auditoria.Caso[int64, int] {
	return auditoria.Nuevo(auditoria.Caso[int64, int]{
		Accion: "centralita.cerrar_viejas", Acceso: auditoria.Autenticado,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (int, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return 0, centralitaNoEncontrada()
			}
			q := mensajeriabd.New(t)
			ahora := m.ahora()
			fs, err := q.LlamadasCentralitaVivas(ctx, mensajeriabd.LlamadasCentralitaVivasParams{CentralitaID: id, Antes: ahora.Add(-CentralitaSonandoMax)})
			if err != nil {
				return 0, err
			}
			n := 0
			for _, l := range fs {
				if l.Estado == LlamadaEnCurso && l.ContestadaEn != nil && ahora.Sub(*l.ContestadaEn) < CentralitaEnCursoMax {
					continue
				}
				if l.Estado == LlamadaEnCurso {
					// No se sabe cuánto duró: queda sin duración.
					l.ContestadaEn = nil
				}
				if ok, err := m.cerrarLlamadaCentralita(ctx, t, q, l, ahora, nil); err != nil {
					return n, err
				} else if ok {
					n++
				}
			}
			if n > 0 {
				t.Anotar(map[string]any{"centralita_id": id, "cerradas": n})
			}
			return n, nil
		},
	})
}

// Casos de la persona --------------------------------------------------------------------------------

// EntradaListarLlamadasCentralita es GET /centralitas/llamadas.
type EntradaListarLlamadasCentralita struct {
	ClienteID int64  `query:"clienteId" minimum:"0" doc:"Las del cliente (ficha del cliente)"`
	TicketID  int64  `query:"ticketId" minimum:"0" doc:"Las vinculadas al ticket"`
	Mias      bool   `query:"mias" doc:"Las que te sonaron o marcaste"`
	Hoy       bool   `query:"hoy" doc:"Solo las de hoy (hora de Guatemala)"`
	Cursor    string `query:"cursor" maxLength:"80"`
	Limit     int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

// ListaLlamadasCentralita es la respuesta paginada.
type ListaLlamadasCentralita struct {
	Data       []Llamada `json:"data"`
	NextCursor *string   `json:"nextCursor" nullable:"true"`
}

// EntradaLlamadaCentralita es GET /centralitas/llamadas/{id}.
type EntradaLlamadaCentralita struct {
	ID uuid.UUID `json:"-"`
}

// EntradaAnotarCentralita es POST /centralitas/llamadas/{id}/anotar.
type EntradaAnotarCentralita struct {
	ID        uuid.UUID `json:"-"`
	Version   int32     `json:"version" minimum:"1"`
	Nota      *string   `json:"nota,omitempty" maxLength:"2000"`
	Resultado *string   `json:"resultado,omitempty" enum:"resuelto,devolver,equivocado,otro"`
}

// MarshalJSON: a la auditoría va si hay nota y su largo, nunca el texto (puede tener datos del cliente).
func (e EntradaAnotarCentralita) MarshalJSON() ([]byte, error) {
	largo := 0
	if e.Nota != nil {
		largo = len([]rune(*e.Nota))
	}
	return json.Marshal(map[string]any{"id": e.ID, "version": e.Version, "resultado": e.Resultado, "largo_nota": largo})
}

// EntradaAsociarCentralita es POST /centralitas/llamadas/{id}/asociar-cliente.
type EntradaAsociarCentralita struct {
	ID         uuid.UUID `json:"-"`
	ClienteID  int64     `json:"clienteId" minimum:"1"`
	ServicioID *int64    `json:"servicioId,omitempty" minimum:"1"`
}

// EntradaVincularTicket es POST /centralitas/llamadas/{id}/vincular-ticket.
type EntradaVincularTicket struct {
	ID       uuid.UUID `json:"-"`
	TicketID int64     `json:"ticketId" minimum:"1"`
}

func llamadaCentralitaNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La llamada no existe")
}

func cursorLlamadas(en time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(en.UnixMicro(), 10) + "_" + id.String()))
}

func leerCursorLlamadas(c string) (*time.Time, *uuid.UUID, error) {
	if c == "" {
		return nil, nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return nil, nil, campoCentralita("cursor", "Cursor inválido")
	}
	a, z, ok := strings.Cut(string(b), "_")
	n, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := uuid.Parse(z)
	if !ok || err1 != nil || err2 != nil {
		return nil, nil, campoCentralita("cursor", "Cursor inválido")
	}
	t := time.UnixMicro(n).UTC()
	return &t, &id, nil
}

// leerLlamadaVisible: la llamada de la empresa activa (FOR UPDATE).
func leerLlamadaVisible(ctx context.Context, q *mensajeriabd.Queries, id uuid.UUID) (mensajeriabd.MensajeriaLlamada, error) {
	l, err := q.LlamadaCentralitaParaActualizar(ctx, mensajeriabd.LlamadaCentralitaParaActualizarParams{ID: id, Empresa: empresaDeCtx(ctx)})
	if errors.Is(err, pgx.ErrNoRows) {
		return l, llamadaCentralitaNoEncontrada()
	}
	return l, err
}

// puedeActuar: le sonó, la contestó o la marcó; o supervisa las extensiones.
func puedeActuar(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada) error {
	usuario := contexto.ActorDe(ctx).ID
	if contexto.PermisosDe(ctx).Tiene(string(nucleo.PermCentralitaAsignarExtension)) || (l.UsuarioID != nil && *l.UsuarioID == usuario) {
		return nil
	}
	es, err := q.EsReceptorCentralita(ctx, mensajeriabd.EsReceptorCentralitaParams{LlamadaID: l.ID, UsuarioID: usuario})
	if err != nil {
		return err
	}
	if !es {
		return llamadaCentralitaNoEncontrada()
	}
	return nil
}

func (m *Modulo) unaLlamada(ctx context.Context, q *mensajeriabd.Queries, l mensajeriabd.MensajeriaLlamada, candidatos bool) (Llamada, error) {
	v, err := m.vistaLlamadas(ctx, q, []mensajeriabd.MensajeriaLlamada{l}, vistaDe(ctx, candidatos))
	if err != nil {
		return Llamada{}, err
	}
	return v[0], nil
}

func nuevosCasosLlamadasCentralita(m *Modulo) (*auditoria.Caso[EntradaListarLlamadasCentralita, ListaLlamadasCentralita],
	*auditoria.Caso[EntradaLlamadaCentralita, Llamada], *auditoria.Caso[EntradaAnotarCentralita, Llamada],
	*AsociarClienteCentralita, *auditoria.Caso[EntradaVincularTicket, Llamada]) {
	listar := auditoria.Nuevo(auditoria.Caso[EntradaListarLlamadasCentralita, ListaLlamadasCentralita]{
		Accion: "centralita.llamadas", Permiso: nucleo.PermLlamadasVer, AuditarExito: sinAuditar[ListaLlamadasCentralita],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarLlamadasCentralita) (ListaLlamadasCentralita, error) {
			if in.ClienteID == 0 && in.TicketID == 0 && !in.Mias {
				return ListaLlamadasCentralita{}, campoCentralita("clienteId", "Pide las de un cliente, las de un ticket o las tuyas (mias=true)")
			}
			q := mensajeriabd.New(t)
			en, id, err := leerCursorLlamadas(in.Cursor)
			if err != nil {
				return ListaLlamadasCentralita{}, err
			}
			limite := in.Limit
			if limite <= 0 || limite > 100 {
				limite = 20
			}
			usuario := contexto.ActorDe(ctx).ID
			ac, errConv := alcanceConvDe(ctx)
			p := mensajeriabd.ListarLlamadasClienteParams{Empresa: empresaDeCtx(ctx), CursorEn: en, CursorID: pgUUID(id), Limite: int32(limite + 1), //nolint:gosec // ≤ 101
				Conversaciones: errConv == nil, Usuario: usuario, Supervisa: ac.supervisa}
			if in.ClienteID > 0 {
				p.ClienteID = &in.ClienteID
			}
			if in.TicketID > 0 {
				p.TicketID = &in.TicketID
			}
			if in.Mias {
				p.Receptor = &usuario
			}
			if in.Hoy {
				d := inicioDia(m.ahora())
				p.Desde = &d
			}
			fs, err := q.ListarLlamadasCliente(ctx, p)
			if err != nil {
				return ListaLlamadasCentralita{}, err
			}
			var sig *string
			if len(fs) > limite {
				fs = fs[:limite]
				s := cursorLlamadas(fs[len(fs)-1].SonoEn, fs[len(fs)-1].ID)
				sig = &s
			}
			// Las de WhatsApp con su vista de siempre; las de la centralita con la suya; en el mismo orden.
			var wa, ce []mensajeriabd.MensajeriaLlamada
			for _, f := range fs {
				if f.Origen == "centralita" {
					ce = append(ce, f)
				} else {
					wa = append(wa, f)
				}
			}
			vistas := map[uuid.UUID]Llamada{}
			if len(wa) > 0 && errConv == nil {
				ls, err := m.aLlamadas(ctx, q, ac, wa)
				if err != nil {
					return ListaLlamadasCentralita{}, err
				}
				for _, l := range ls {
					vistas[l.ID] = l
				}
			}
			ls, err := m.vistaLlamadas(ctx, q, ce, vistaDe(ctx, false))
			if err != nil {
				return ListaLlamadasCentralita{}, err
			}
			for _, l := range ls {
				vistas[l.ID] = l
			}
			v := make([]Llamada, 0, len(fs))
			for _, f := range fs {
				if l, ok := vistas[f.ID]; ok {
					v = append(v, l)
				}
			}
			return ListaLlamadasCentralita{Data: v, NextCursor: sig}, err
		},
	})
	ver := auditoria.Nuevo(auditoria.Caso[EntradaLlamadaCentralita, Llamada]{
		Accion: "centralita.ver_llamada", Permiso: nucleo.PermLlamadasVer, AuditarExito: sinAuditar[Llamada],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamadaCentralita) (Llamada, error) {
			q := mensajeriabd.New(t)
			l, err := leerLlamadaVisible(ctx, q, in.ID)
			if err != nil {
				return Llamada{}, err
			}
			return m.unaLlamada(ctx, q, l, true)
		},
	})
	anotar := auditoria.Nuevo(auditoria.Caso[EntradaAnotarCentralita, Llamada]{
		Accion: "centralita.anotar_llamada", Permiso: nucleo.PermLlamadasAnotar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAnotarCentralita) (Llamada, error) {
			q := mensajeriabd.New(t)
			l, err := leerLlamadaVisible(ctx, q, in.ID)
			if err != nil {
				return Llamada{}, err
			}
			if err := puedeActuar(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			if l.Version != in.Version {
				return Llamada{}, nucleo.ConflictoVersion("llamada", l.ID)
			}
			t.FijarEntidad("centralita_llamada", l.ID)
			antes := map[string]any{"nota": l.Nota != nil, "resultado": l.Resultado}
			if in.Nota != nil {
				n := strings.TrimSpace(*in.Nota)
				l.Nota = nil
				if n != "" {
					l.Nota = &n
				}
			}
			if in.Resultado != nil {
				l.Resultado = in.Resultado
			}
			u := contexto.ActorDe(ctx).ID
			l.AnotadaPor = &u
			if l, err = guardarLlamadaCentralita(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			// La nota no va a la auditoría (puede tener datos del cliente): solo si hay.
			t.RegistrarCambios("centralita_llamada", l.ID, antes, map[string]any{"nota": l.Nota != nil, "resultado": l.Resultado})
			if err := t.Emitir("llamada.anotada", map[string]any{"llamada_id": l.ID, "conversacion_id": nil, "empresa_id": l.EmpresaID, "usuario_id": u,
				"resultado": l.Resultado, "origen": "centralita"}); err != nil {
				return Llamada{}, err
			}
			return m.unaLlamada(ctx, q, l, false)
		},
	})
	vincular := auditoria.Nuevo(auditoria.Caso[EntradaVincularTicket, Llamada]{
		Accion: "centralita.vincular_ticket", Permiso: nucleo.PermLlamadasAnotar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVincularTicket) (Llamada, error) {
			if m.cfg.Soporte == nil {
				return Llamada{}, noDisponible("Vincular tickets")
			}
			q := mensajeriabd.New(t)
			l, err := leerLlamadaVisible(ctx, q, in.ID)
			if err != nil {
				return Llamada{}, err
			}
			if err := puedeActuar(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			// El ticket tiene que verlo quien vincula (tickets.ver y su alcance): si no, 404.
			tk, err := m.cfg.Soporte.VerTicket.Ejecutar(ctx, soporte.EntradaVerTicket{ID: in.TicketID})
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEntidad("centralita_llamada", l.ID)
			if l.TicketID != nil && *l.TicketID == tk.ID {
				return m.unaLlamada(ctx, q, l, false)
			}
			if l.ClienteID != nil && tk.ClienteID != nil && *l.ClienteID != *tk.ClienteID {
				return Llamada{}, nucleo.ReglaNegocio("cliente_distinto", "El ticket es de otro cliente")
			}
			antes := map[string]any{"ticket_id": l.TicketID, "cliente_id": l.ClienteID}
			if l.ClienteID == nil && tk.ClienteID != nil {
				l.ClienteID, l.ServicioID, l.Asociacion = tk.ClienteID, tk.ServicioID, texto("manual")
			}
			l.TicketID = &tk.ID
			if l, err = guardarLlamadaCentralita(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			t.RegistrarCambios("centralita_llamada", l.ID, antes, map[string]any{"ticket_id": l.TicketID, "cliente_id": l.ClienteID})
			if err := t.Emitir("llamada.vinculada", map[string]any{"llamada_id": l.ID, "empresa_id": l.EmpresaID, "ticket_id": tk.ID,
				"cliente_id": l.ClienteID, "usuario_id": contexto.ActorDe(ctx).ID, "origen": "centralita"}); err != nil {
				return Llamada{}, err
			}
			return m.unaLlamada(ctx, q, l, false)
		},
	})
	return listar, ver, anotar, nuevoAsociarClienteCentralita(m), vincular
}

// AsociarClienteCentralita elige el cliente de la llamada (varios posibles o desconocido): asegura
// la instantánea fuera de la transacción y después asocia.
type AsociarClienteCentralita struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaAsociarCentralita, struct{}]
	final  *auditoria.Caso[EntradaAsociarCentralita, Llamada]
}

func nuevoAsociarClienteCentralita(m *Modulo) *AsociarClienteCentralita {
	k := &AsociarClienteCentralita{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaAsociarCentralita, struct{}]{
		Accion: "centralita.asociar_cliente", Permiso: nucleo.PermLlamadasAnotar, AuditarExito: sinAuditar[struct{}],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsociarCentralita) (struct{}, error) {
			if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesVer)) {
				return struct{}{}, nucleo.Prohibido("Te falta el permiso clientes.ver")
			}
			q := mensajeriabd.New(t)
			l, err := leerLlamadaVisible(ctx, q, in.ID)
			if err != nil {
				return struct{}{}, err
			}
			return struct{}{}, puedeActuar(ctx, q, l)
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[EntradaAsociarCentralita, Llamada]{
		Accion: "centralita.asociar_cliente", Permiso: nucleo.PermLlamadasAnotar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAsociarCentralita) (Llamada, error) {
			if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesVer)) {
				return Llamada{}, nucleo.Prohibido("Te falta el permiso clientes.ver")
			}
			q := mensajeriabd.New(t)
			l, err := leerLlamadaVisible(ctx, q, in.ID)
			if err != nil {
				return Llamada{}, err
			}
			if err := puedeActuar(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			t.FijarEntidad("centralita_llamada", l.ID)
			c, err := q.ClienteInstantanea(ctx, in.ClienteID)
			empresa := empresaDeCtx(ctx)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && empresa != 0 && c.EmpresaID != empresa && !slices.Contains(c.Empresas, empresa)) {
				return Llamada{}, nucleo.ReglaNegocio("cliente_no_valido", "Ese cliente no está en la empresa")
			}
			if err != nil {
				return Llamada{}, err
			}
			servicio := in.ServicioID
			ss, err := q.ServiciosInstantanea(ctx, c.ID)
			if err != nil {
				return Llamada{}, err
			}
			if servicio != nil && !slices.ContainsFunc(ss, func(s mensajeriabd.IntegracionArgosServicio) bool { return s.ID == *servicio }) {
				return Llamada{}, nucleo.ReglaNegocio("servicio_no_valido", "Ese servicio no es del cliente")
			}
			if servicio == nil && len(ss) == 1 {
				servicio = &ss[0].ID
			}
			if l.TicketID != nil && l.ClienteID != nil && *l.ClienteID != c.ID {
				return Llamada{}, nucleo.ReglaNegocio("cliente_distinto", "La llamada ya está en un ticket de otro cliente")
			}
			antes := map[string]any{"cliente_id": l.ClienteID, "servicio_id": l.ServicioID}
			l.ClienteID, l.ServicioID, l.Asociacion = &c.ID, servicio, texto("manual")
			if l, err = guardarLlamadaCentralita(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			t.RegistrarCambios("centralita_llamada", l.ID, antes, map[string]any{"cliente_id": l.ClienteID, "servicio_id": l.ServicioID})
			if err := t.Emitir("llamada.cliente_asociado", map[string]any{"llamada_id": l.ID, "empresa_id": l.EmpresaID, "cliente_id": c.ID,
				"servicio_id": servicio, "usuario_id": contexto.ActorDe(ctx).ID, "origen": "centralita"}); err != nil {
				return Llamada{}, err
			}
			if err := m.avisarReceptores(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			return m.unaLlamada(ctx, q, l, false)
		},
	})
	return k
}

// Ejecutar asocia el cliente.
func (k *AsociarClienteCentralita) Ejecutar(ctx context.Context, in EntradaAsociarCentralita) (Llamada, error) {
	if _, err := k.previo.Ejecutar(ctx, in); err != nil {
		return Llamada{}, err
	}
	if k.m.cfg.Argos != nil {
		if err := k.m.cfg.Argos.AsegurarInstantanea(ctx, in.ClienteID, in.ServicioID); err != nil {
			return Llamada{}, err
		}
	}
	return k.final.Ejecutar(ctx, in)
}
