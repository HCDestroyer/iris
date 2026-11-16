package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Aprobar y rechazar boletas desde la conversación (contrato con Argos 0.7.0 §7.7, pedido B3;
// contratos/conversaciones.md §6.4.1). Una persona de Iris transmite la decisión y Argos decide con
// el usuario de Argos vinculado a esa persona (X-Iris-Actor: usuario:<id>). El asistente de IA, los
// tokens y las integraciones nunca pueden.
//
// La llamada a Argos es sincrónica y va fuera de la transacción (como las promesas): quien decide
// necesita la respuesta al momento, así que no pasa por integracion.pendientes. Con el 200 de Argos
// la boleta local se resuelve en una transacción (auditoría, mensaje de sistema, aviso al cliente y
// evento).
//
// Una boleta se resuelve UNA sola vez, por el camino que llegue primero, y todos pasan por
// aplicarResolucion (la fila bloqueada, solo desde `registrada`):
//
//   - la respuesta 200 de Argos a quien decidió en Iris;
//   - el webhook comprobante.aprobado / comprobante.rechazado (decidió caja en Argos, o llegó antes
//     que la respuesta): el worker lo deja en boletas.resolucion y el despacho de este proceso lo
//     aplica pasada la gracia;
//   - releer el comprobante (GET /comprobantes/:id) cuando Argos dice que ya estaba revisado.
//
// Por eso el aviso al cliente (boleta_confirmada, boleta_rechazada o boleta_falta_dato, §6.3 del
// contrato con Argos) sale una sola vez.

// Estados de una boleta.
const (
	BoletaRegistrada = "registrada"
	BoletaAprobada   = "aprobada"
	BoletaRechazada  = "rechazada"
)

// Aviso al cliente de una boleta resuelta.
const (
	AvisoBoletaEnviado = "enviado"
	AvisoBoletaSinAvis = "sin_aviso"
)

// GraciaResolucion: cuánto espera el despacho antes de aplicar la resolución que trajo un webhook.
// Le da tiempo a la respuesta de Argos de quien decidió en Iris (así la boleta queda a su nombre);
// si esa respuesta se perdió, el webhook la resuelve igual.
const GraciaResolucion = 20 * time.Second

// MonedaBoletas: los comprobantes de Argos son en quetzales (§7.2).
const MonedaBoletas = "GTQ"

// EntradaAprobarBoleta es POST /boletas/{id}/aprobar.
type EntradaAprobarBoleta struct {
	ID       uuid.UUID `json:"-"`
	Nota     *string   `json:"nota,omitempty" maxLength:"400" doc:"Nota para caja (queda en Argos)"`
	Facturas []int64   `json:"facturas,omitempty" maxItems:"50" doc:"Ids de Argos de las facturas a las que se aplica el pago; sin ellas, Argos lo aplica en su orden"`
}

// MarshalJSON: lo auditado (sin el texto de la nota).
func (e EntradaAprobarBoleta) MarshalJSON() ([]byte, error) {
	largo := 0
	if e.Nota != nil {
		largo = len([]rune(*e.Nota))
	}
	return json.Marshal(map[string]any{"boleta_id": e.ID, "largo_nota": largo, "facturas": e.Facturas})
}

// EntradaRechazarBoleta es POST /boletas/{id}/rechazar.
type EntradaRechazarBoleta struct {
	ID     uuid.UUID `json:"-"`
	Motivo string    `json:"motivo" minLength:"3" maxLength:"500" doc:"Por qué se rechaza: se le dice al cliente"`
	Causa  string    `json:"causa,omitempty" enum:"no_aparece,monto_distinto,ilegible,falta_dato,duplicado,otro" doc:"Sin causa, Argos usa otro. Con falta_dato el cliente recibe «le falta un dato» y puede mandarla de nuevo"`
}

// MarshalJSON: lo auditado.
func (e EntradaRechazarBoleta) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"boleta_id": e.ID, "motivo": e.Motivo, "causa": e.Causa})
}

// ListaBoletas son las boletas de una conversación.
type ListaBoletas struct {
	Data []Boleta `json:"data"`
}

// EntradaBoletaID identifica una boleta.
type EntradaBoletaID struct {
	ID uuid.UUID `json:"boleta_id"`
}

// Errores ---------------------------------------------------------------------------------------------

func boletaNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La boleta no existe o no la puedes ver")
}

func soloPersonasBoleta() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusForbidden, "solo_personas", "Solo personas",
		"Aprobar o rechazar una boleta solo lo puede hacer una persona con su sesión del panel: ni un token, ni una integración, ni el asistente de IA")
}

// argosNoDisponibleBoleta: 502 (no el 503 de las lecturas): nada quedó a medias y se puede repetir.
func argosNoDisponibleBoleta() *nucleo.Problema { return argos.NoDisponible() }

func boletaYaDecidida(f mensajeriabd.MensajeriaBoleta) *nucleo.Problema {
	return nucleo.Conflicto("boleta_ya_decidida", "Esta boleta ya fue "+descripcionDecision(f))
}

func descripcionDecision(f mensajeriabd.MensajeriaBoleta) string {
	d := f.Estado
	if f.VerificadoPorNombre != nil && *f.VerificadoPorNombre != "" {
		d += " por " + *f.VerificadoPorNombre
	}
	return d
}

// Vista -----------------------------------------------------------------------------------------------

// apruebaComprobantes: el conector Argos activo informa contrato ≥ 0.7.0 (false sin Argos o apagado).
func (m *Modulo) apruebaComprobantes(ctx context.Context, q conector.DBTX) bool {
	if m.cfg.Argos == nil {
		return false
	}
	ok, err := m.cfg.Argos.ApruebaComprobantes(ctx, q)
	return err == nil && ok
}

// boletaVista completa lo que el panel necesita: si se puede decidir y quién decidió.
func (m *Modulo) boletaVista(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaBoleta, aprueba bool) Boleta {
	b := aBoleta(f)
	b.PuedeDecidir = aprueba && f.Estado == BoletaRegistrada && f.ComprobanteID != nil
	if f.DecididaPor != nil {
		ref := UsuarioRef{ID: *f.DecididaPor}
		if n := actorNombre(ctx, q, *f.DecididaPor); n != nil {
			ref.Nombre = *n
		}
		b.DecididaPor = &ref
	}
	return b
}

// boletaVisible lee la boleta si el actor ve su conversación (404 si no).
func boletaVisible(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, id uuid.UUID) (mensajeriabd.MensajeriaBoleta, mensajeriabd.MensajeriaConversacion, error) {
	f, err := q.BoletaPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, mensajeriabd.MensajeriaConversacion{}, boletaNoEncontrada()
	}
	if err != nil {
		return f, mensajeriabd.MensajeriaConversacion{}, err
	}
	c, err := convVisible(ctx, q, a, f.ConversacionID, false)
	if err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) && p.Status == http.StatusNotFound {
			err = boletaNoEncontrada()
		}
		return f, c, err
	}
	return f, c, nil
}

func nuevoVerBoleta(m *Modulo) *auditoria.Caso[EntradaBoletaID, Boleta] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBoletaID, Boleta]{
		Accion: "boletas.ver", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[Boleta],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBoletaID) (Boleta, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return Boleta{}, boletaNoEncontrada()
			}
			q := mensajeriabd.New(t)
			f, _, err := boletaVisible(ctx, q, a, in.ID)
			if err != nil {
				return Boleta{}, err
			}
			return m.boletaVista(ctx, q, f, m.apruebaComprobantes(ctx, t)), nil
		},
	})
}

func nuevoListarBoletas(m *Modulo) *auditoria.Caso[EntradaConversacion, ListaBoletas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, ListaBoletas]{
		Accion: "boletas.listar", Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaBoletas],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (ListaBoletas, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaBoletas{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return ListaBoletas{}, err
			}
			filas, err := q.BoletasDeConversacion(ctx, c.ID)
			if err != nil {
				return ListaBoletas{}, err
			}
			aprueba := len(filas) > 0 && m.apruebaComprobantes(ctx, t)
			l := ListaBoletas{Data: make([]Boleta, 0, len(filas))}
			for _, f := range filas {
				l.Data = append(l.Data, m.boletaVista(ctx, q, f, aprueba))
			}
			return l, nil
		},
	})
}

// Resolución (el único camino que cambia el estado) -----------------------------------------------

// resolucionBoleta es lo que decidió Argos sobre el comprobante de una boleta.
type resolucionBoleta struct {
	// Estado: aprobada o rechazada.
	Estado              string  `json:"estado"`
	PagoID              *int64  `json:"pago_id"`
	Motivo              *string `json:"motivo"`
	Causa               *string `json:"causa"`
	VerificadoPorNombre *string `json:"verificado_por_nombre,omitempty"`
	// actor: el usuario de Iris que decidió desde la conversación (nil = caja en Argos).
	actor *int64
}

// resolucionDeComprobante traduce el comprobante de Argos; ok = false si sigue pendiente.
func resolucionDeComprobante(c argos.Comprobante) (resolucionBoleta, bool) {
	r := resolucionBoleta{PagoID: c.PagoID, VerificadoPorNombre: c.VerificadoPorNombre}
	switch c.Estado {
	case argos.ComprobanteAprobado:
		r.Estado = BoletaAprobada
	case argos.ComprobanteRechazado:
		r.Estado, r.PagoID, r.Motivo, r.Causa = BoletaRechazada, nil, c.MotivoRechazo, c.CausaRechazo
	default:
		return r, false
	}
	return r, true
}

// causaValida deja solo las causas de §7.2 (lo demás es «otro»).
func causaValida(c *string) *string {
	if c == nil || *c == "" {
		return nil
	}
	switch *c {
	case "no_aparece", "monto_distinto", "ilegible", "falta_dato", "duplicado", "otro":
		return c
	}
	return nombrePtr("otro")
}

func textoLimpio(s *string, max int) *string {
	if s == nil {
		return nil
	}
	v := strings.Join(strings.Fields(*s), " ")
	if v == "" {
		return nil
	}
	return nombrePtr(recortar(v, max))
}

// aplicarResolucion resuelve la boleta (leída con FOR UPDATE) si sigue registrada: mensaje de
// sistema, aviso al cliente, auditoría y evento, una sola vez. Si ya estaba resuelta solo completa
// quién decidió. cambio = true si esta llamada la resolvió.
func (m *Modulo) aplicarResolucion(ctx context.Context, t *auditoria.Tx, f mensajeriabd.MensajeriaBoleta, r resolucionBoleta) (mensajeriabd.MensajeriaBoleta, bool, error) {
	q := mensajeriabd.New(t)
	r.Motivo, r.Causa, r.VerificadoPorNombre = textoLimpio(r.Motivo, 500), causaValida(r.Causa), textoLimpio(r.VerificadoPorNombre, 200)
	switch f.Estado {
	case BoletaAprobada, BoletaRechazada:
		// Ya resuelta (llegó antes el webhook o la releyó otro): se completa quién decidió, sin otro
		// aviso ni otro evento. Si Argos dice otra cosa, manda lo que ya se le dijo al cliente.
		p := mensajeriabd.CompletarDecisionBoletaParams{ID: f.ID}
		if f.Estado == r.Estado {
			p.DecididaPor, p.VerificadoPorNombre, p.PagoID = r.actor, r.VerificadoPorNombre, r.PagoID
		}
		g, err := q.CompletarDecisionBoleta(ctx, p)
		return g, false, err
	case BoletaRegistrada:
	default:
		// por_enviar o fallida: no hay comprobante que resolver.
		if len(f.Resolucion) > 0 {
			return f, false, q.DescartarResolucionBoleta(ctx, f.ID)
		}
		return f, false, nil
	}
	if r.Estado != BoletaAprobada && r.Estado != BoletaRechazada {
		return f, false, errors.New("mensajeria: resolución de boleta sin estado")
	}
	if r.Estado == BoletaAprobada {
		r.Motivo, r.Causa = nil, nil
	} else {
		r.PagoID = nil
		if r.Causa == nil {
			r.Causa = nombrePtr("otro")
		}
	}
	c, err := q.ConversacionClienteParaActualizar(ctx, f.ConversacionID)
	if err != nil {
		return f, false, err
	}
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	t.FijarEntidad("boleta", f.ID)
	nombre := r.VerificadoPorNombre
	if nombre == nil && r.actor != nil {
		nombre = actorNombre(ctx, q, *r.actor)
	}
	ev := EventoSistema{Tipo: "boleta_" + r.Estado, ActorNombre: nombre, Referencia: nombrePtr("Boleta " + f.Referencia),
		EntidadID: nombrePtr(f.ID.String()), Detalle: r.Motivo}
	if err := m.sistema(ctx, t, &c, ev); err != nil {
		return f, false, err
	}
	aviso, mensaje, err := m.avisarBoleta(ctx, t, q, &c, f, r)
	if err != nil {
		return f, false, err
	}
	ahora := m.ahora()
	g, err := q.ResolverBoleta(ctx, mensajeriabd.ResolverBoletaParams{ID: f.ID, Estado: r.Estado, DecididaEn: &ahora, DecididaPor: r.actor, PagoID: r.PagoID,
		MotivoRechazo: r.Motivo, CausaRechazo: r.Causa, VerificadoPorNombre: r.VerificadoPorNombre, AvisoCliente: &aviso, AvisoMensajeID: pgUUID(mensaje)})
	if err != nil {
		return f, false, err
	}
	if _, err := guardarConv(ctx, q, c, false); err != nil {
		return f, false, err
	}
	t.RegistrarCambios("boleta", g.ID, map[string]any{"estado": f.Estado}, map[string]any{"estado": g.Estado, "pago_id": g.PagoID,
		"causa_rechazo": g.CausaRechazo, "decidida_por": g.DecididaPor, "aviso_cliente": g.AvisoCliente})
	origen := "argos"
	if r.actor != nil {
		origen = "iris"
	}
	payload := map[string]any{"boleta_id": g.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "comprobante_id": g.ComprobanteID,
		"pago_id": g.PagoID, "actor_id": g.DecididaPor, "origen": origen, "aviso_cliente": aviso}
	if r.Estado == BoletaRechazada {
		payload["causa"] = g.CausaRechazo
		return g, true, t.Emitir("boleta.rechazada", payload)
	}
	return g, true, t.Emitir("boleta.aprobada", payload)
}

// avisarBoleta le dice al cliente, en la conversación, cómo quedó su boleta (§6.3 del contrato con
// Argos): con la ventana de 24 h abierta, como texto; cerrada, solo si la plantilla está aprobada en
// el número y el cliente tiene permiso de contacto. Nunca a una baja. Devuelve enviado (con el
// mensaje en cola) o sin_aviso.
func (m *Modulo) avisarBoleta(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion,
	f mensajeriabd.MensajeriaBoleta, r resolucionBoleta) (string, *uuid.UUID, error) {
	codigo, motivo := "boleta_confirmada", "¡Gracias!"
	if r.Estado == BoletaRechazada {
		codigo, motivo = "boleta_rechazada", "comunícate con nosotros para revisarla"
		if r.Causa != nil && *r.Causa == "falta_dato" {
			codigo, motivo = "boleta_falta_dato", "revisa los datos del depósito"
		}
		if r.Motivo != nil {
			motivo = recortar(*r.Motivo, 200)
		}
	}
	p, ok := PlantillaPorCodigo(codigo)
	if !ok {
		return AvisoBoletaSinAvis, nil, nil
	}
	vars := map[string]string{"monto": aBoleta(f).Monto, "moneda": MonedaBoletas, "motivo": motivo}
	canal, err := q.CanalPorID(ctx, c.CanalID)
	if err != nil {
		return "", nil, err
	}
	uso, err := usoDe(ctx, q, canal, empresaDeConv(*c))
	if err != nil {
		return "", nil, err
	}
	if !canal.Activo || canal.DesconectadoEn != nil || !uso.activo {
		return AvisoBoletaSinAvis, nil, nil
	}
	texto := recortar(p.Render(vars), MaxTextoWhatsApp)
	n := nuevoMensaje{direccion: DirSaliente, tipo: "texto", texto: &texto, estadoEntrega: nombrePtr(EntregaEnCola), automatico: nombrePtr("bot")}
	abierta := ventanaDe(c.UltimoEntranteEn, m.ahora()).Abierta
	var pr *nucleo.Problema
	if c.Estado == EstadoConvResuelta || !abierta {
		// Escribir en una resuelta o fuera de la ventana abre la conversación al cliente: en un número
		// compartido, no si otra empresa tiene una abierta con ese teléfono (§15.3).
		if err := otraEmpresaConversa(ctx, q, canal.ID, empresaDeConv(*c), c.Numero); errors.As(err, &pr) {
			return AvisoBoletaSinAvis, nil, nil
		} else if err != nil {
			return "", nil, err
		}
	}
	if abierta {
		puede, err := m.puedeContestarBot(ctx, q, *c)
		if err != nil {
			return "", nil, err
		}
		if !puede {
			return AvisoBoletaSinAvis, nil, nil
		}
	} else {
		// Fuera de la ventana solo sale una plantilla aprobada en el número, con permiso de contacto.
		if _, aprobada := plantillasDeCanal(canal)[p.Codigo]; !aprobada || (!uso.propio && !tieneUsoConversacion(uso.usos)) {
			return AvisoBoletaSinAvis, nil, nil
		}
		if p.validarVariables(vars) != nil {
			return AvisoBoletaSinAvis, nil, nil
		}
		if err := permisoPlantilla(ctx, q, *c, true); errors.As(err, &pr) {
			return AvisoBoletaSinAvis, nil, nil
		} else if err != nil {
			return "", nil, err
		}
		n.tipo, n.plantilla = "plantilla", &p.Codigo
		if n.variables, err = json.Marshal(vars); err != nil {
			return "", nil, err
		}
	}
	msg, err := m.agregarMensaje(ctx, t, c, n)
	if err != nil {
		return "", nil, err
	}
	if err := m.encolar(ctx, t.Tx, ArgsEnviarMensaje{MensajeID: msg.ID}); err != nil {
		return "", nil, err
	}
	return AvisoBoletaEnviado, &msg.ID, nil
}

// Decidir (aprobar y rechazar) ------------------------------------------------------------------------

const (
	accionAprobar  = "aprobar"
	accionRechazar = "rechazar"
)

// decision es lo que pide la persona.
type decision struct {
	Boleta   uuid.UUID `json:"boleta_id"`
	Accion   string    `json:"accion"`
	Nota     string    `json:"-"`
	Facturas []int64   `json:"facturas,omitempty"`
	Motivo   string    `json:"motivo,omitempty"`
	Causa    string    `json:"causa,omitempty"`
}

func (d decision) estado() string {
	if d.Accion == accionAprobar {
		return BoletaAprobada
	}
	return BoletaRechazada
}

type previaDecision struct {
	repetida    *Boleta
	a           alcanceConv
	boleta      mensajeriabd.MensajeriaBoleta
	comprobante int64
}

type entradaFinalDecision struct {
	decision
	ComprobanteID int64  `json:"comprobante_id"`
	EstadoArgos   string `json:"estado_argos"`
	// Origen: respuesta (el 200 de Argos a esta persona) o relectura (GET /comprobantes/:id).
	Origen string `json:"origen"`
	r      resolucionBoleta
}

type finalDecision struct {
	boleta Boleta
	fila   mensajeriabd.MensajeriaBoleta
}

// DecidirBoleta aprueba o rechaza una boleta en Argos y deja el resultado en la conversación.
type DecidirBoleta struct {
	m        *Modulo
	previo   map[string]*auditoria.Caso[decision, previaDecision]
	final    map[string]*auditoria.Caso[entradaFinalDecision, finalDecision]
	releida  *auditoria.Caso[entradaFinalDecision, finalDecision]
	resolver *auditoria.Caso[EntradaBoletaID, bool]
	recibir  *auditoria.Caso[entradaResolucionArgos, int64]
}

func permisoDecision(accion string) nucleo.Permiso {
	if accion == accionAprobar {
		return nucleo.PermBoletasAprobar
	}
	return nucleo.PermBoletasRechazar
}

func nuevoDecidirBoleta(m *Modulo) *DecidirBoleta {
	k := &DecidirBoleta{m: m, previo: map[string]*auditoria.Caso[decision, previaDecision]{}, final: map[string]*auditoria.Caso[entradaFinalDecision, finalDecision]{}}
	for _, accion := range []string{accionAprobar, accionRechazar} {
		k.previo[accion] = auditoria.Nuevo(auditoria.Caso[decision, previaDecision]{
			Accion: "boletas." + accion, Permiso: permisoDecision(accion), AuditarExito: sinAuditar[previaDecision], Logica: k.revisar,
		})
		k.final[accion] = auditoria.Nuevo(auditoria.Caso[entradaFinalDecision, finalDecision]{
			Accion: "boletas." + accion, Permiso: permisoDecision(accion), Logica: k.guardar,
		})
	}
	// La relectura sincroniza lo que ya decidió otro: la audita quien la provocó, con conversaciones.ver.
	k.releida = auditoria.Nuevo(auditoria.Caso[entradaFinalDecision, finalDecision]{
		Accion: "boletas.sincronizar", Permiso: nucleo.PermConversacionesVer, Logica: k.guardar,
		AuditarExito: func(finalDecision) bool { return false },
	})
	k.resolver = auditoria.Nuevo(auditoria.Caso[EntradaBoletaID, bool]{
		Accion: "mensajeria.resolver_boleta", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[bool], Logica: k.resolverPendiente,
	})
	k.recibir = auditoria.Nuevo(auditoria.Caso[entradaResolucionArgos, int64]{
		Accion: "mensajeria.recibir_resolucion_boleta", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[int64],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaResolucionArgos) (int64, error) {
			if in.ComprobanteID <= 0 || (in.Estado != argos.ComprobanteAprobado && in.Estado != argos.ComprobanteRechazado) {
				return 0, nil
			}
			r := resolucionBoleta{Estado: BoletaAprobada, PagoID: in.PagoID}
			if in.Estado == argos.ComprobanteRechazado {
				r = resolucionBoleta{Estado: BoletaRechazada, Motivo: in.Motivo, Causa: in.Causa}
			}
			crudo, err := json.Marshal(r)
			if err != nil {
				return 0, err
			}
			ahora := m.ahora()
			return mensajeriabd.New(t).GuardarResolucionBoleta(ctx, mensajeriabd.GuardarResolucionBoletaParams{Resolucion: crudo, Ahora: &ahora,
				ComprobanteID: in.ComprobanteID, EmpresaID: in.EmpresaID})
		},
	})
	return k
}

// revisar: quién puede y sobre qué boleta (antes de llamar a Argos).
func (k *DecidirBoleta) revisar(ctx context.Context, t *auditoria.Tx, in decision) (previaDecision, error) {
	// Solo una persona: un token de API, una integración o el sistema (el asistente de IA) nunca.
	if actor := t.Actor(); actor.Tipo != contexto.ActorUsuario || actor.ID == 0 {
		return previaDecision{}, soloPersonasBoleta()
	}
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return previaDecision{}, boletaNoEncontrada()
	}
	q := mensajeriabd.New(t)
	f, c, err := boletaVisible(ctx, q, a, in.Boleta)
	if err != nil {
		return previaDecision{}, err
	}
	t.FijarEntidad("boleta", f.ID)
	if lectura, err := soloLecturaConv(ctx, q, c); err != nil || lectura {
		if err == nil {
			err = conversacionSoloLectura()
		}
		return previaDecision{}, err
	}
	switch f.Estado {
	case BoletaAprobada, BoletaRechazada:
		// Repetir la misma decisión (la misma persona) devuelve la boleta como quedó.
		if f.Estado == in.estado() && f.DecididaPor != nil && *f.DecididaPor == a.usuario {
			b := k.m.boletaVista(ctx, q, f, false)
			return previaDecision{repetida: &b}, nil
		}
		return previaDecision{}, boletaYaDecidida(f)
	case BoletaRegistrada:
	default:
		return previaDecision{}, reglaConv("boleta_no_registrada", "La boleta todavía no está registrada en Argos (o Argos no la aceptó): no hay nada que aprobar ni rechazar")
	}
	if f.ComprobanteID == nil {
		return previaDecision{}, reglaConv("boleta_no_registrada", "La boleta no tiene comprobante en Argos")
	}
	if k.m.cfg.Argos == nil {
		return previaDecision{}, noDisponible("Aprobar y rechazar boletas en Argos")
	}
	ok, err := k.m.cfg.Argos.ApruebaComprobantes(ctx, t)
	if err != nil {
		return previaDecision{}, err
	}
	if !ok {
		return previaDecision{}, argos.SinAprobacion()
	}
	return previaDecision{a: a, boleta: f, comprobante: *f.ComprobanteID}, nil
}

// guardar aplica lo que dijo Argos (su 200 o la relectura) sobre la boleta.
func (k *DecidirBoleta) guardar(ctx context.Context, t *auditoria.Tx, in entradaFinalDecision) (finalDecision, error) {
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return finalDecision{}, boletaNoEncontrada()
	}
	q := mensajeriabd.New(t)
	if _, _, err := boletaVisible(ctx, q, a, in.Boleta); err != nil {
		return finalDecision{}, err
	}
	f, err := q.BoletaParaEnviar(ctx, in.Boleta)
	if err != nil {
		return finalDecision{}, err
	}
	t.FijarEntidad("boleta", f.ID)
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	if f.ComprobanteID == nil || *f.ComprobanteID != in.ComprobanteID {
		return finalDecision{}, nucleo.Conflicto("boleta_cambiada", "La boleta cambió mientras Argos respondía; vuelve a abrirla")
	}
	g, _, err := k.m.aplicarResolucion(ctx, t, f, in.r)
	if err != nil {
		return finalDecision{}, err
	}
	if err := k.m.tocarActividad(ctx, q, a); err != nil {
		return finalDecision{}, err
	}
	return finalDecision{boleta: k.m.boletaVista(ctx, q, g, false), fila: g}, nil
}

// Aprobar aprueba la boleta en Argos (registra el pago) y la deja aprobada en la conversación.
func (k *DecidirBoleta) Aprobar(ctx context.Context, in EntradaAprobarBoleta) (Boleta, error) {
	d := decision{Boleta: in.ID, Accion: accionAprobar, Facturas: in.Facturas}
	if in.Nota != nil {
		d.Nota = strings.TrimSpace(*in.Nota)
	}
	for _, f := range d.Facturas {
		if f <= 0 {
			return Boleta{}, campoConv("facturas", "Ids de factura inválidos", "minimum")
		}
	}
	return k.decidir(ctx, d)
}

// Rechazar rechaza la boleta en Argos, con motivo y causa.
func (k *DecidirBoleta) Rechazar(ctx context.Context, in EntradaRechazarBoleta) (Boleta, error) {
	d := decision{Boleta: in.ID, Accion: accionRechazar, Motivo: strings.Join(strings.Fields(in.Motivo), " "), Causa: in.Causa}
	if n := len([]rune(d.Motivo)); n < 3 || n > 500 {
		return Boleta{}, campoConv("motivo", "Escribe el motivo (3 a 500 caracteres)", "minLength")
	}
	return k.decidir(ctx, d)
}

func (k *DecidirBoleta) decidir(ctx context.Context, d decision) (Boleta, error) {
	pre, err := k.previo[d.Accion].Ejecutar(ctx, d)
	if err != nil {
		return Boleta{}, err
	}
	if pre.repetida != nil {
		return *pre.repetida, nil
	}
	at := atencionDe(pre.a, pre.boleta.ConversacionID)
	var c argos.Comprobante
	if d.Accion == accionAprobar {
		c, err = k.m.cfg.Argos.AprobarComprobante(ctx, at, pre.comprobante, d.Facturas, d.Nota)
	} else {
		c, err = k.m.cfg.Argos.RechazarComprobante(ctx, at, pre.comprobante, d.Motivo, d.Causa)
	}
	if err != nil {
		return k.alFallar(ctx, d, pre, at, err)
	}
	r, ok := resolucionDeComprobante(c)
	if !ok || (c.ID != 0 && c.ID != pre.comprobante) {
		return Boleta{}, conector.Rechazo(argos.Remoto, "Argos respondió un comprobante que Iris no reconoce; revisa la boleta en Argos")
	}
	r.actor = &pre.a.usuario
	if r.Estado == BoletaRechazada && r.Motivo == nil {
		r.Motivo, r.Causa = &d.Motivo, causaValida(&d.Causa)
	}
	res, err := k.final[d.Accion].Ejecutar(context.WithoutCancel(ctx), entradaFinalDecision{decision: d, ComprobanteID: pre.comprobante, EstadoArgos: c.Estado, Origen: "respuesta", r: r})
	if err != nil {
		return Boleta{}, err
	}
	// Llegó antes el webhook y Argos resolvió otra cosa que la pedida (no debería): se dice.
	if res.fila.Estado != d.estado() {
		return Boleta{}, boletaYaDecidida(res.fila)
	}
	return res.boleta, nil
}

// alFallar traduce el error de Argos a lo que ve la persona (contratos/conversaciones.md §6.4.1).
func (k *DecidirBoleta) alFallar(ctx context.Context, d decision, pre previaDecision, at argos.AtencionIris, err error) (Boleta, error) {
	var er *conector.ErrorRemoto
	if !errors.As(err, &er) {
		// Sin conector, sin contrato 0.7.0 o sin base: el problema ya viene armado.
		return Boleta{}, err
	}
	if er.Caido() {
		return Boleta{}, argosNoDisponibleBoleta()
	}
	switch er.Status {
	case http.StatusForbidden:
		switch er.Codigo {
		case "actor_sin_vinculo":
			return Boleta{}, nucleo.Conflicto("argos_sin_vinculo", "Vincula tu usuario de Argos para aprobar pagos")
		case "actor_inactivo":
			return Boleta{}, nucleo.NuevoProblema(http.StatusForbidden, "argos_usuario_inactivo", "Usuario de Argos inactivo",
				"Tu usuario de Argos está inactivo: pide a un administrador de Argos que lo active")
		case "actor_sin_permiso":
			return Boleta{}, nucleo.NuevoProblema(http.StatusForbidden, "argos_sin_permiso", "Sin permiso en Argos", "Tu usuario de Argos no tiene permiso de caja")
		case "actor_sin_empresa":
			return Boleta{}, nucleo.NuevoProblema(http.StatusForbidden, "argos_sin_empresa", "Sin acceso a la empresa en Argos",
				"Tu usuario de Argos no tiene acceso a la empresa de este pago")
		case "actor_ia_no_permitido":
			return Boleta{}, soloPersonasBoleta()
		case "empresa_no_permitida":
			// El alcance del usuario técnico Iris en Argos (el token), no el de la persona.
			return Boleta{}, conector.Rechazo(argos.Remoto, "El conector de Iris no tiene acceso a la empresa de este pago en Argos: revisa en Argos el alcance del usuario técnico Iris")
		}
		return Boleta{}, conector.Rechazo(argos.Remoto, "Argos rechazó el token del conector: revisa en Argos los permisos del usuario técnico Iris (comprobantes.aprobar y comprobantes.rechazar)")
	case http.StatusUnauthorized:
		return Boleta{}, conector.Rechazo(argos.Remoto, "Argos rechazó el token del conector: vuelve a conectar Iris con Argos")
	case http.StatusNotFound:
		return Boleta{}, nucleo.Conflicto("comprobante_no_existe", "Argos ya no tiene el comprobante de esta boleta; revísalo en Argos")
	case http.StatusConflict:
		switch {
		case er.Codigo == "comprobante_revisado":
			return k.releer(ctx, d, pre, at, "comprobante_revisado")
		case er.Codigo == "comprobante_no_es_de_iris":
			return Boleta{}, nucleo.Conflicto("comprobante_no_es_de_iris", "Este comprobante no nació en Iris: se resuelve en la caja de Argos")
		case er.Codigo == "iris_apagado":
			return Boleta{}, argos.IrisApagado()
		case argos.EsEnCurso(err):
			return Boleta{}, nucleo.Conflicto("idempotencia_en_curso", "Argos todavía procesa esta decisión; vuelve a intentar en unos segundos")
		}
		return Boleta{}, nucleo.Conflicto("argos_conflicto", "Argos no pudo aplicar la decisión por el estado del comprobante; revísalo en Argos")
	case http.StatusUnprocessableEntity:
		if er.Codigo == "idempotency_key_reutilizada" {
			// La clave (una por comprobante y acción) la usó otro actor o esta persona con otro cuerpo.
			return k.releer(ctx, d, pre, at, "otra_persona_decidio")
		}
		return Boleta{}, reglaConv("decision_no_permitida", "Argos no aceptó la decisión: revisa las facturas, el motivo o la causa")
	}
	return Boleta{}, argos.Problema(err)
}

// releer lee el comprobante de Argos y sincroniza la boleta local. Si lo decidió esta misma persona
// con el mismo resultado (se perdió la respuesta anterior), responde la boleta; si no, 409.
func (k *DecidirBoleta) releer(ctx context.Context, d decision, pre previaDecision, at argos.AtencionIris, codigo string) (Boleta, error) {
	c, err := k.m.cfg.Argos.ObtenerComprobante(ctx, at, pre.comprobante)
	if err != nil {
		if conector.EsCaido(err) {
			return Boleta{}, argosNoDisponibleBoleta()
		}
		return Boleta{}, nucleo.Conflicto(codigo, "Argos ya tiene una decisión sobre este comprobante, pero no se pudo releer; vuelve a abrir la boleta")
	}
	r, ok := resolucionDeComprobante(c)
	if !ok {
		// Sigue pendiente: otra persona tiene la decisión a medias (o la clave quedó de otra petición).
		return Boleta{}, nucleo.Conflicto(codigo, "Otra persona está decidiendo esta boleta; vuelve a abrirla en unos segundos")
	}
	mia := c.IrisActor != nil && *c.IrisActor == at.Actor
	if mia {
		r.actor = &pre.a.usuario
	}
	res, err := k.releida.Ejecutar(context.WithoutCancel(ctx), entradaFinalDecision{decision: d, ComprobanteID: pre.comprobante, EstadoArgos: c.Estado, Origen: "relectura", r: r})
	if err != nil {
		return Boleta{}, err
	}
	if mia && res.fila.Estado == d.estado() {
		return res.boleta, nil
	}
	detalle := "Esta boleta ya fue " + descripcionDecision(res.fila)
	if codigo == "otra_persona_decidio" {
		detalle = "Otra persona ya decidió esta boleta: fue " + descripcionDecision(res.fila)
	}
	return Boleta{}, nucleo.Conflicto(codigo, detalle)
}

// Webhook de Argos (por el outbox) ---------------------------------------------------------------------

// entradaResolucionArgos es el payload de argos.comprobante_resuelto.
type entradaResolucionArgos struct {
	ComprobanteID int64   `json:"comprobante_id"`
	Estado        string  `json:"estado"`
	EmpresaID     *int64  `json:"empresa_id"`
	PagoID        *int64  `json:"pago_id"`
	Motivo        *string `json:"motivo"`
	Causa         *string `json:"causa"`
}

// SuscribirComprobantes registra en el bus del relay (cmd/worker) el suscriptor de
// argos.comprobante_resuelto (webhooks comprobante.aprobado y comprobante.rechazado de Argos): deja
// la resolución en la boleta de ese comprobante, si sigue registrada. La aplica el despacho del
// proceso de mensajería (mensaje de sistema y aviso al cliente). Idempotente: una boleta ya resuelta
// o con una resolución guardada no cambia.
func (m *Modulo) SuscribirComprobantes(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("mensajeria.boletas", argos.EventoComprobanteResuelto, func(ctx context.Context, e integracion.Evento) error {
		var in entradaResolucionArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return nil // un payload ilegible no se arregla reintentando
		}
		_, err := m.boletas.recibir.Ejecutar(ctxSistema(ctx, bd), in)
		return err
	})
}

// resolverPendiente aplica la resolución que dejó el webhook (true si la boleta cambió).
func (k *DecidirBoleta) resolverPendiente(ctx context.Context, t *auditoria.Tx, in EntradaBoletaID) (bool, error) {
	q := mensajeriabd.New(t)
	f, err := q.BoletaParaEnviar(ctx, in.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(f.Resolucion) == 0 {
		return false, nil
	}
	var r resolucionBoleta
	if err := json.Unmarshal(f.Resolucion, &r); err != nil || (r.Estado != BoletaAprobada && r.Estado != BoletaRechazada) {
		return false, q.DescartarResolucionBoleta(ctx, f.ID)
	}
	_, cambio, err := k.m.aplicarResolucion(ctx, t, f, r)
	return cambio, err
}

// despacharResoluciones aplica las resoluciones de webhooks que ya pasaron la gracia (hasta 50).
func (m *Modulo) despacharResoluciones(ctx context.Context, bd nucleo.BD) error {
	hasta := m.ahora().Add(-GraciaResolucion)
	ids, err := mensajeriabd.New(bd).BoletasPorResolver(ctx, &hasta)
	if err != nil {
		return err
	}
	var primero error
	for _, id := range ids {
		if _, err := m.boletas.resolver.Ejecutar(ctx, EntradaBoletaID{ID: id}); err != nil && primero == nil {
			primero = errors.New("resolver la boleta " + id.String() + ": " + err.Error())
		}
	}
	return primero
}
