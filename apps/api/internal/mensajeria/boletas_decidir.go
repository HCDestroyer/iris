package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

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
// necesita la respuesta al momento, así que no pasa por integracion.pendientes. El intento se audita
// antes de llamar; con el 200 de Argos la boleta local se resuelve en otra transacción.
//
// El estado de una boleta solo cambia con lo que dice Argos de su comprobante, y todos los caminos
// pasan por aplicarResolucion (la fila bloqueada):
//
//   - la respuesta 200 de Argos a quien decidió en Iris;
//   - el webhook comprobante.aprobado / comprobante.rechazado: es solo un aviso. El worker lo deja en
//     boletas.resolucion y el despacho de este proceso relee GET /comprobantes/:id pasada la gracia y
//     aplica lo que diga Argos, nunca el payload;
//   - releer el comprobante cuando Argos responde que ya estaba revisado.
//
// La boleta se resuelve UNA vez (de registrada a aprobada o rechazada) y ahí sale el aviso al
// cliente. Si después Argos dice otra cosa, gana Argos: se corrige con boleta.corregida y sin otro
// aviso.

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

// Confirmación con Argos de una resolución que avisó su webhook.
const (
	ConfirmacionPendiente = "pendiente"
	ConfirmacionAgotada   = "agotada"
)

// GraciaResolucion: cuánto espera el despacho antes de confirmar la resolución que avisó un webhook.
// Le da tiempo a la respuesta de Argos de quien decidió en Iris (así la boleta queda a su nombre).
const GraciaResolucion = 20 * time.Second

// EsperasConfirmacion son las esperas entre intentos de confirmar con Argos; después, agotada.
var EsperasConfirmacion = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// MonedaBoletas: los comprobantes de Argos son en quetzales (§7.2).
const MonedaBoletas = "GTQ"

// EntradaAprobarBoleta es POST /boletas/{id}/aprobar.
type EntradaAprobarBoleta struct {
	ID       uuid.UUID `json:"-"`
	Nota     *string   `json:"nota,omitempty" maxLength:"400" doc:"Nota para caja (queda en Argos)"`
	Facturas []int64   `json:"facturas,omitempty" maxItems:"50" doc:"Ids de Argos de las facturas a las que se aplica el pago; sin ellas, Argos lo aplica en su orden"`
	// MontoEsperado: el monto que la persona vio y confirmó en el diálogo.
	MontoEsperado *string `json:"montoEsperado,omitempty" pattern:"^[0-9]{1,12}\\.[0-9]{2}$" doc:"El monto que la persona vio y confirmó (decimal con dos decimales). Si viene, Iris lo compara con el de la boleta y con el del comprobante en Argos (lo relee) ANTES de aprobar; si alguno no coincide, 409 monto_cambio con montoActual y no se aprueba nada"`
}

// MarshalJSON: lo auditado (sin el texto de la nota).
func (e EntradaAprobarBoleta) MarshalJSON() ([]byte, error) {
	largo := 0
	if e.Nota != nil {
		largo = len([]rune(*e.Nota))
	}
	return json.Marshal(map[string]any{"boleta_id": e.ID, "largo_nota": largo, "facturas": e.Facturas, "monto_esperado": e.MontoEsperado})
}

// montoCambio: lo que la persona confirmó ya no es lo que se aprobaría.
func montoCambio(actual string) *nucleo.Problema {
	p := nucleo.Conflicto("monto_cambio", "El monto de esta boleta ya no es el que confirmaste: ahora es "+MonedaBoletas+" "+actual+". Revísalo y vuelve a confirmar")
	p.MontoActual = &actual
	return p
}

// montoNormal deja un monto decimal con dos decimales y sin ceros a la izquierda ("" si no lo es).
func montoNormal(s string) string {
	s = strings.TrimSpace(s)
	if !regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,2})?$`).MatchString(s) {
		return ""
	}
	entero, decimales, _ := strings.Cut(s, ".")
	entero = strings.TrimLeft(entero, "0")
	if entero == "" {
		entero = "0"
	}
	return entero + "." + (decimales + "00")[:2]
}

// EntradaRechazarBoleta es POST /boletas/{id}/rechazar.
type EntradaRechazarBoleta struct {
	ID     uuid.UUID `json:"-"`
	Motivo string    `json:"motivo" minLength:"3" maxLength:"500" doc:"Por qué se rechaza: queda en Argos y en la boleta. Al cliente se le dice un texto fijo según la causa; este motivo se agrega solo si no trae enlaces, dominios ni números largos"`
	Causa  string    `json:"causa,omitempty" enum:"no_aparece,monto_distinto,ilegible,falta_dato,duplicado,otro" doc:"Sin causa, Argos usa otro. Decide el texto que recibe el cliente"`
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

// argosNoDisponibleBoleta: 502 (no el 503 de las lecturas). Un timeout no dice si Argos llegó a
// aplicar la decisión: repetir es seguro (la clave sale del comprobante) y confirma cómo quedó.
func argosNoDisponibleBoleta() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadGateway, "argos_no_disponible", "Argos no respondió",
		"Puede haberse aplicado en Argos: vuelve a intentar para confirmarlo")
}

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

// Lo que se le dice al cliente ------------------------------------------------------------------------

// textosCausa es lo que el cliente lee según la causa del rechazo (§6.4.1): un texto fijo que dice
// qué pasó y qué hacer. Va en la variable `motivo` de boleta_rechazada («No pudimos confirmar tu
// boleta de … : <texto>») o, con falta_dato, de boleta_falta_dato («… le falta un dato: <texto>.
// Envíala de nuevo, por favor.»).
var textosCausa = map[string]string{
	"no_aparece":     "no encontramos el depósito. Revisa la referencia y la fecha, y envíala de nuevo",
	"monto_distinto": "el monto no coincide con el depósito. Envíanos la boleta con el monto correcto",
	"ilegible":       "la foto no se lee bien. Envíanos una foto más clara",
	"falta_dato":     "revisa que se vean la referencia, el monto y la fecha",
	"duplicado":      "esa boleta ya se había usado. Si hiciste otro pago, envíanos su boleta",
	"otro":           "no pudimos confirmarla con el banco. Escríbenos por aquí para revisarla",
}

// motivoLimpio normaliza el motivo libre antes de filtrarlo: NFKC (los dígitos y letras de ancho
// completo, los superíndices y las ligaduras pasan a su forma común), sin caracteres de formato
// (categoría Cf: espacios de ancho cero, marcas de dirección…) y con los espacios colapsados.
func motivoLimpio(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, norm.NFKC.String(s))
	return strings.Join(strings.Fields(s), " ")
}

// letrasMotivo: lo único que puede llevar un motivo libre que se le manda al cliente.
const letrasMotivo = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZáéíóúüñÁÉÍÓÚÜÑ ,;:¿?¡!()"

// motivoSeguro: el motivo libre (lo escribe quien rechaza, en Iris o en la caja de Argos) solo se le
// manda al cliente si no puede llevarlo a otro lado. Es una lista blanca sobre el texto ya
// normalizado (motivoLimpio): letras del español, espacios y `,;:¿?¡!()`. Cualquier otra cosa —un
// dígito (en cualquier escritura), un punto, una barra, un guion, una arroba, una letra de otro
// alfabeto— lo deja fuera: sin puntos ni dígitos no hay enlaces, dominios, teléfonos ni cuentas. Sale
// por el número oficial de la empresa.
func motivoSeguro(limpio string) bool {
	if limpio == "" {
		return false
	}
	for _, r := range limpio {
		if !strings.ContainsRune(letrasMotivo, r) {
			return false
		}
	}
	return true
}

// motivoParaCliente arma la variable `motivo` del aviso: el texto fijo de la causa y, si pasa el
// filtro, el motivo libre al final. Hasta 200 caracteres (formato texto de las plantillas).
func motivoParaCliente(causa *string, libre *string) string {
	texto, ok := "", false
	if causa != nil {
		texto, ok = textosCausa[*causa]
	}
	if !ok {
		texto = textosCausa["otro"]
	}
	if libre == nil {
		return texto
	}
	// El punto final de una frase se quita (es lo único que se le perdona); cualquier otro punto la
	// deja fuera.
	l := strings.TrimRight(motivoLimpio(*libre), ". ")
	if !motivoSeguro(l) {
		return texto
	}
	if resto := 200 - len([]rune(texto)) - len([]rune(". Detalle: ")); resto >= 10 {
		return texto + ". Detalle: " + recortar(l, resto)
	}
	return texto
}

// Resolución (el único camino que cambia el estado) -----------------------------------------------

// resolucionBoleta es lo que decidió Argos sobre el comprobante de una boleta.
type resolucionBoleta struct {
	// Estado: aprobada o rechazada.
	Estado              string
	PagoID              *int64
	Motivo              *string
	Causa               *string
	VerificadoPorNombre *string
	// monto: el del comprobante en Argos (decimal con dos decimales), para el aviso; vacío = el local.
	monto string
	// actor: el usuario de Iris que decidió desde la conversación (nil = caja en Argos, o Argos no lo dice).
	actor *int64
}

var reMontoArgos = regexp.MustCompile(`^[0-9]{1,12}\.[0-9]{2}$`)

// resolucionDeComprobante traduce el comprobante de Argos; ok = false si sigue pendiente.
func resolucionDeComprobante(c argos.Comprobante) (resolucionBoleta, bool) {
	r := resolucionBoleta{PagoID: c.PagoID, VerificadoPorNombre: c.VerificadoPorNombre}
	if m := string(c.Monto); reMontoArgos.MatchString(m) {
		r.monto = m
	}
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

// comprobanteAjeno dice por qué el comprobante que devolvió Argos no es el de la boleta ("" = lo es).
// El token de Iris lee cualquier comprobante de sus empresas, no solo los de canal iris: se valida aquí.
func comprobanteAjeno(c argos.Comprobante, f mensajeriabd.MensajeriaBoleta, exigirEmpresa bool) string {
	switch {
	case f.ComprobanteID == nil || c.ID != *f.ComprobanteID:
		return "Argos devolvió otro comprobante"
	case !strings.EqualFold(strings.TrimSpace(c.Canal), argos.CanalComprobanteIris):
		return "el comprobante no es de canal iris"
	case c.EmpresaID != nil && f.EmpresaID != nil && *c.EmpresaID != *f.EmpresaID:
		return "el comprobante es de otra empresa"
	case exigirEmpresa && (c.EmpresaID == nil || f.EmpresaID == nil):
		return "falta la empresa del comprobante"
	}
	return ""
}

// actorVerificado: el usuario de Iris que decidió según Argos (verificado_iris_actor, contrato 0.7.1):
// solo «usuario:<id>» de una persona de esta instalación. Con Argos 1.15.0 no viene: nil.
func actorVerificado(ctx context.Context, q *mensajeriabd.Queries, actor *string) *int64 {
	if actor == nil {
		return nil
	}
	crudo, ok := strings.CutPrefix(strings.TrimSpace(*actor), "usuario:")
	if !ok {
		return nil
	}
	id, err := strconv.ParseInt(crudo, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != crudo {
		return nil
	}
	if _, err := q.UsuarioPersona(ctx, id); err != nil {
		return nil
	}
	return &id
}

// causaValida deja solo las causas de §7.2 (lo demás es «otro»).
func causaValida(c *string) *string {
	if c == nil || *c == "" {
		return nil
	}
	if _, ok := textosCausa[*c]; ok {
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

// Resultados de aplicarResolucion.
const (
	resolucionNada       = ""
	resolucionResuelta   = "resuelta"
	resolucionCompletada = "completada"
	resolucionCorregida  = "corregida"
)

// aplicarResolucion deja la boleta (leída con FOR UPDATE) como dice Argos:
//
//   - registrada → resuelta: mensaje de sistema, aviso al cliente, auditoría y evento, una sola vez;
//   - ya resuelta con ese estado: solo completa quién decidió;
//   - ya resuelta con el otro estado: gana Argos. Se corrige con boleta.corregida, sin otro aviso.
func (m *Modulo) aplicarResolucion(ctx context.Context, t *auditoria.Tx, f mensajeriabd.MensajeriaBoleta, r resolucionBoleta) (mensajeriabd.MensajeriaBoleta, string, error) {
	q := mensajeriabd.New(t)
	r.Motivo, r.Causa, r.VerificadoPorNombre = textoLimpio(r.Motivo, 500), causaValida(r.Causa), textoLimpio(r.VerificadoPorNombre, 200)
	if r.Estado != BoletaAprobada && r.Estado != BoletaRechazada {
		return f, resolucionNada, errors.New("mensajeria: resolución de boleta sin estado")
	}
	if r.Estado == BoletaAprobada {
		r.Motivo, r.Causa = nil, nil
	} else {
		r.PagoID = nil
		if r.Causa == nil {
			r.Causa = nombrePtr("otro")
		}
	}
	switch f.Estado {
	case r.Estado:
		g, err := q.CompletarDecisionBoleta(ctx, mensajeriabd.CompletarDecisionBoletaParams{ID: f.ID, DecididaPor: r.actor,
			VerificadoPorNombre: r.VerificadoPorNombre, PagoID: r.PagoID})
		return g, resolucionCompletada, err
	case BoletaAprobada, BoletaRechazada, BoletaRegistrada:
	default:
		// por_enviar o fallida: no hay comprobante que resolver.
		if len(f.Resolucion) > 0 {
			return f, resolucionNada, q.DescartarResolucionBoleta(ctx, f.ID)
		}
		return f, resolucionNada, nil
	}
	c, err := q.ConversacionClienteParaActualizar(ctx, f.ConversacionID)
	if err != nil {
		return f, resolucionNada, err
	}
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	t.FijarEntidad("boleta", f.ID)
	// En el hilo se ve quién fue: la persona de Iris si decidió desde la conversación; si no, el
	// usuario de Argos. Al cliente nunca se le dice.
	nombre := r.VerificadoPorNombre
	if r.actor != nil {
		if n := actorNombre(ctx, q, *r.actor); n != nil {
			nombre = n
		}
	}
	corrige := f.Estado != BoletaRegistrada
	ev := EventoSistema{Tipo: "boleta_" + r.Estado, ActorNombre: nombre, Referencia: nombrePtr("Boleta " + f.Referencia),
		EntidadID: nombrePtr(f.ID.String()), Detalle: r.Motivo}
	if corrige {
		ev.Tipo, ev.Detalle = "boleta_corregida", nombrePtr(f.Estado+" → "+r.Estado)
	}
	if err := m.sistema(ctx, t, &c, ev); err != nil {
		return f, resolucionNada, err
	}
	// El cliente recibe un solo aviso: el de la primera resolución. Una corrección la ve el equipo.
	aviso, mensaje := f.AvisoCliente, uuidDe(f.AvisoMensajeID)
	if !corrige {
		a, msg, err := m.avisarBoleta(ctx, t, q, &c, f, r)
		if err != nil {
			return f, resolucionNada, err
		}
		aviso, mensaje = &a, msg
	}
	ahora := m.ahora()
	g, err := q.ResolverBoleta(ctx, mensajeriabd.ResolverBoletaParams{ID: f.ID, Estado: r.Estado, DecididaEn: &ahora, DecididaPor: r.actor, PagoID: r.PagoID,
		MotivoRechazo: r.Motivo, CausaRechazo: r.Causa, VerificadoPorNombre: r.VerificadoPorNombre, AvisoCliente: aviso, AvisoMensajeID: pgUUID(mensaje)})
	if err != nil {
		return f, resolucionNada, err
	}
	if _, err := guardarConv(ctx, q, c, false); err != nil {
		return f, resolucionNada, err
	}
	t.RegistrarCambios("boleta", g.ID, map[string]any{"estado": f.Estado, "decidida_por": f.DecididaPor}, map[string]any{"estado": g.Estado, "pago_id": g.PagoID,
		"causa_rechazo": g.CausaRechazo, "decidida_por": g.DecididaPor, "aviso_cliente": g.AvisoCliente})
	origen := "argos"
	if r.actor != nil {
		origen = "iris"
	}
	payload := map[string]any{"boleta_id": g.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "comprobante_id": g.ComprobanteID,
		"pago_id": g.PagoID, "actor_id": g.DecididaPor, "origen": origen}
	if corrige {
		payload["de"], payload["a"], payload["causa"] = f.Estado, g.Estado, g.CausaRechazo
		return g, resolucionCorregida, t.Emitir("boleta.corregida", payload)
	}
	payload["aviso_cliente"] = g.AvisoCliente
	if r.Estado == BoletaRechazada {
		payload["causa"] = g.CausaRechazo
		return g, resolucionResuelta, t.Emitir("boleta.rechazada", payload)
	}
	return g, resolucionResuelta, t.Emitir("boleta.aprobada", payload)
}

// avisarBoleta le dice al cliente, en la conversación, cómo quedó su boleta (§6.3 del contrato con
// Argos): con la ventana de 24 h abierta, como texto; cerrada, solo si la plantilla está aprobada en
// el número y el cliente tiene permiso de contacto. Nunca a una baja. Devuelve enviado (con el
// mensaje en cola) o sin_aviso.
func (m *Modulo) avisarBoleta(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion,
	f mensajeriabd.MensajeriaBoleta, r resolucionBoleta) (string, *uuid.UUID, error) {
	codigo, motivo := "boleta_confirmada", "¡Gracias!"
	if r.Estado == BoletaRechazada {
		codigo = "boleta_rechazada"
		if r.Causa != nil && *r.Causa == "falta_dato" {
			codigo = "boleta_falta_dato"
		}
		motivo = motivoParaCliente(r.Causa, r.Motivo)
	}
	p, ok := PlantillaPorCodigo(codigo)
	if !ok {
		return AvisoBoletaSinAvis, nil, nil
	}
	monto := r.monto
	if monto == "" {
		monto = aBoleta(f).Monto
	}
	vars := map[string]string{"monto": monto, "moneda": MonedaBoletas, "motivo": motivo}
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
		usables, err := m.plantillasDeCanal(ctx, q, canal, "")
		if err != nil {
			return "", nil, err
		}
		pm, aprobada := usables[p.Codigo]
		if !aprobada || (!uso.propio && !tieneUsoConversacion(uso.usos)) {
			return AvisoBoletaSinAvis, nil, nil
		}
		// Una plantilla con datos del cliente de Argos solo sale con el cliente confirmado.
		if necesitaDatosCliente(pm) && !c.ClienteVerificado {
			return AvisoBoletaSinAvis, nil, nil
		}
		if p.validarVariables(vars, m.ahora()) != nil {
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
	// MontoEsperado: lo que la persona confirmó en el diálogo ("" = no lo mandó).
	MontoEsperado string `json:"monto_esperado,omitempty"`
	Motivo        string `json:"motivo,omitempty"`
	Causa         string `json:"causa,omitempty"`
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

// entradaAplicar es lo que Argos dijo del comprobante de una boleta, listo para aplicar.
type entradaAplicar struct {
	Boleta        uuid.UUID `json:"boleta_id"`
	Accion        string    `json:"accion,omitempty"`
	ComprobanteID int64     `json:"comprobante_id"`
	EstadoArgos   string    `json:"estado_argos"`
	// Origen: respuesta (el 200 de Argos a esta persona), relectura (GET /comprobantes/:id tras un
	// 409), webhook (GET tras el aviso de Argos) o respuesta_sin_sesion (el 200 llegó, pero la persona
	// ya no podía guardar: lo aplica el sistema).
	Origen string `json:"origen"`
	// DecididaPor: la persona de la sesión cuando la decisión sale de la respuesta a su propia llamada.
	DecididaPor *int64 `json:"decidida_por,omitempty"`
	// actorArgos: verificado_iris_actor del comprobante (webhook y relectura).
	actorArgos *string
	r          resolucionBoleta
}

type boletaAplicada struct {
	boleta    Boleta
	fila      mensajeriabd.MensajeriaBoleta
	resultado string
}

// DecidirBoleta aprueba o rechaza una boleta en Argos y deja el resultado en la conversación.
type DecidirBoleta struct {
	m      *Modulo
	previo map[string]*auditoria.Caso[decision, previaDecision]
	final  map[string]*auditoria.Caso[entradaAplicar, boletaAplicada]
	// releida: quien provocó la relectura sincroniza lo que ya decidió otro (con conversaciones.ver).
	releida *auditoria.Caso[entradaAplicar, boletaAplicada]
	// sistema: aplica lo que dijo Argos sin sesión (webhook confirmado, o el 200 cuya persona ya no
	// podía guardar).
	sistema   *auditoria.Caso[entradaAplicar, boletaAplicada]
	recibir   *auditoria.Caso[entradaResolucionArgos, int64]
	reintento *auditoria.Caso[entradaReintento, bool]
}

func permisoDecision(accion string) nucleo.Permiso {
	if accion == accionAprobar {
		return nucleo.PermBoletasAprobar
	}
	return nucleo.PermBoletasRechazar
}

func nuevoDecidirBoleta(m *Modulo) *DecidirBoleta {
	k := &DecidirBoleta{m: m, previo: map[string]*auditoria.Caso[decision, previaDecision]{}, final: map[string]*auditoria.Caso[entradaAplicar, boletaAplicada]{}}
	for _, accion := range []string{accionAprobar, accionRechazar} {
		// El intento queda auditado ANTES de llamar a Argos (acción, boleta, comprobante y actor): si
		// después falla el guardado local, igual consta quién lo pidió.
		k.previo[accion] = auditoria.Nuevo(auditoria.Caso[decision, previaDecision]{
			Accion: "boletas." + accion, Permiso: permisoDecision(accion), Logica: k.revisar,
			AuditarExito: func(p previaDecision) bool { return p.repetida == nil },
		})
		k.final[accion] = auditoria.Nuevo(auditoria.Caso[entradaAplicar, boletaAplicada]{
			Accion: "boletas." + accion, Permiso: permisoDecision(accion), Logica: k.guardar,
		})
	}
	k.releida = auditoria.Nuevo(auditoria.Caso[entradaAplicar, boletaAplicada]{
		Accion: "boletas.sincronizar", Permiso: nucleo.PermConversacionesVer, Logica: k.guardar,
		AuditarExito: func(boletaAplicada) bool { return false },
	})
	k.sistema = auditoria.Nuevo(auditoria.Caso[entradaAplicar, boletaAplicada]{
		Accion: "mensajeria.resolver_boleta", Acceso: auditoria.Autenticado, Logica: k.aplicar,
		AuditarExito: func(boletaAplicada) bool { return false },
	})
	k.recibir = auditoria.Nuevo(auditoria.Caso[entradaResolucionArgos, int64]{
		Accion: "mensajeria.recibir_resolucion_boleta", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[int64],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaResolucionArgos) (int64, error) {
			// Sin empresa no se sabe de quién es el comprobante: no se toca ninguna boleta.
			if in.ComprobanteID <= 0 || in.EmpresaID == nil || *in.EmpresaID <= 0 ||
				(in.Estado != argos.ComprobanteAprobado && in.Estado != argos.ComprobanteRechazado) {
				return 0, nil
			}
			// Solo queda dicho que Argos avisó (y qué): lo que se aplica sale de releer el comprobante.
			crudo, err := json.Marshal(map[string]any{"estado": in.Estado})
			if err != nil {
				return 0, err
			}
			ahora := m.ahora()
			proximo := ahora.Add(GraciaResolucion)
			return mensajeriabd.New(t).GuardarResolucionBoleta(ctx, mensajeriabd.GuardarResolucionBoletaParams{Resolucion: crudo, Ahora: &ahora,
				Proximo: &proximo, ComprobanteID: in.ComprobanteID, EmpresaID: *in.EmpresaID})
		},
	})
	k.reintento = auditoria.Nuevo(auditoria.Caso[entradaReintento, bool]{
		Accion: "mensajeria.confirmar_boleta", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[bool], Logica: k.reintentar,
	})
	return k
}

// revisar: quién puede y sobre qué boleta (antes de llamar a Argos). Su éxito es la auditoría del intento.
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
	t.FijarEmpresa(empresaDeConv(c))
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
	t.Anotar(map[string]any{"fase": "intento", "comprobante_id": *f.ComprobanteID, "conversacion_id": f.ConversacionID})
	return previaDecision{a: a, boleta: f, comprobante: *f.ComprobanteID}, nil
}

// guardar aplica lo que dijo Argos con la sesión de la persona (debe seguir viendo la boleta).
func (k *DecidirBoleta) guardar(ctx context.Context, t *auditoria.Tx, in entradaAplicar) (boletaAplicada, error) {
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return boletaAplicada{}, boletaNoEncontrada()
	}
	q := mensajeriabd.New(t)
	if _, _, err := boletaVisible(ctx, q, a, in.Boleta); err != nil {
		return boletaAplicada{}, err
	}
	r, err := k.aplicar(ctx, t, in)
	if err != nil {
		return r, err
	}
	return r, k.m.tocarActividad(ctx, q, a)
}

// aplicar bloquea la boleta y la deja como dice Argos.
func (k *DecidirBoleta) aplicar(ctx context.Context, t *auditoria.Tx, in entradaAplicar) (boletaAplicada, error) {
	q := mensajeriabd.New(t)
	f, err := q.BoletaParaEnviar(ctx, in.Boleta)
	if err != nil {
		return boletaAplicada{}, err
	}
	t.FijarEntidad("boleta", f.ID)
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	if f.ComprobanteID == nil || *f.ComprobanteID != in.ComprobanteID {
		return boletaAplicada{}, nucleo.Conflicto("boleta_cambiada", "La boleta cambió mientras Argos respondía; vuelve a abrirla")
	}
	r := in.r
	r.actor = in.DecididaPor
	if r.actor == nil {
		r.actor = actorVerificado(ctx, q, in.actorArgos)
	}
	t.Anotar(map[string]any{"fase": "resultado"})
	g, resultado, err := k.m.aplicarResolucion(ctx, t, f, r)
	if err != nil {
		return boletaAplicada{}, err
	}
	return boletaAplicada{boleta: k.m.boletaVista(ctx, q, g, false), fila: g, resultado: resultado}, nil
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
	if in.MontoEsperado != nil {
		if d.MontoEsperado = montoNormal(*in.MontoEsperado); d.MontoEsperado == "" {
			return Boleta{}, campoConv("montoEsperado", "Monto inválido (decimal con dos decimales)", "pattern")
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
	if err := k.exigirMonto(ctx, d, pre, at); err != nil {
		return Boleta{}, err
	}
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
	if r.Estado == BoletaRechazada && r.Motivo == nil {
		r.Motivo, r.Causa = &d.Motivo, causaValida(&d.Causa)
	}
	// La respuesta 200 es a la llamada de esta persona: la decisión es suya.
	in := entradaAplicar{Boleta: d.Boleta, Accion: d.Accion, ComprobanteID: pre.comprobante, EstadoArgos: c.Estado, Origen: "respuesta",
		DecididaPor: &pre.a.usuario, r: r}
	sinCancelar := context.WithoutCancel(ctx)
	res, err := k.final[d.Accion].Ejecutar(sinCancelar, in)
	if err != nil {
		// Argos ya decidió. Si la persona ya no puede guardar (permiso retirado, re-autenticación
		// vencida, conversación que dejó de ver), la boleta igual queda como dice Argos; el error se
		// le responde tal cual.
		in.Origen = "respuesta_sin_sesion"
		if bd := nucleo.BDDe(ctx); bd != nil {
			if _, errSis := k.sistema.Ejecutar(ctxSistema(sinCancelar, bd), in); errSis != nil {
				slog.ErrorContext(ctx, "mensajeria: Argos decidió una boleta y no se pudo guardar; la sincroniza el webhook", "boleta", d.Boleta, "err", errSis)
			}
		}
		return Boleta{}, err
	}
	// Argos resolvió otra cosa que la pedida (no debería): se dice.
	if res.fila.Estado != d.estado() {
		return Boleta{}, boletaYaDecidida(res.fila)
	}
	return res.boleta, nil
}

// exigirMonto: si la persona mandó el monto que confirmó, debe ser el de la boleta y el del comprobante
// en Argos ahora mismo (se relee antes de aprobar: aprobar no lleva monto). Si no, 409 monto_cambio y
// no se llama a aprobar.
func (k *DecidirBoleta) exigirMonto(ctx context.Context, d decision, pre previaDecision, at argos.AtencionIris) error {
	if d.Accion != accionAprobar || d.MontoEsperado == "" {
		return nil
	}
	if local := montoNormal(aBoleta(pre.boleta).Monto); local != d.MontoEsperado {
		return montoCambio(local)
	}
	c, err := k.m.cfg.Argos.ObtenerComprobante(ctx, at, pre.comprobante)
	if err != nil {
		var er *conector.ErrorRemoto
		switch {
		case !errors.As(err, &er):
			return err
		case er.Caido():
			// Solo fue la lectura previa: nada se aplicó.
			return argos.NoDisponible()
		case er.Status == http.StatusNotFound:
			return nucleo.Conflicto("comprobante_no_existe", "Argos ya no tiene el comprobante de esta boleta; revísalo en Argos")
		}
		return argos.Problema(err)
	}
	if c.ID != pre.comprobante {
		return conector.Rechazo(argos.Remoto, "Argos respondió un comprobante que Iris no reconoce; revisa la boleta en Argos")
	}
	if enArgos := montoNormal(string(c.Monto)); enArgos != d.MontoEsperado {
		return montoCambio(enArgos)
	}
	return nil
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
			return Boleta{}, comprobanteNoEsDeIris()
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

func comprobanteNoEsDeIris() *nucleo.Problema {
	return nucleo.Conflicto("comprobante_no_es_de_iris", "Este comprobante no nació en Iris: se resuelve en la caja de Argos")
}

// releer lee el comprobante de Argos y sincroniza la boleta local. Si Argos dice que lo decidió esta
// misma persona con el mismo resultado (se perdió la respuesta), responde la boleta; si no, 409.
func (k *DecidirBoleta) releer(ctx context.Context, d decision, pre previaDecision, at argos.AtencionIris, codigo string) (Boleta, error) {
	c, err := k.m.cfg.Argos.ObtenerComprobante(ctx, at, pre.comprobante)
	if err != nil {
		if conector.EsCaido(err) {
			return Boleta{}, argosNoDisponibleBoleta()
		}
		return Boleta{}, nucleo.Conflicto(codigo, "Argos ya tiene una decisión sobre este comprobante, pero no se pudo releer; vuelve a abrir la boleta")
	}
	// El token de Iris lee cualquier comprobante de sus empresas: se valida que sea el de la boleta.
	if c.ID != pre.comprobante {
		return Boleta{}, conector.Rechazo(argos.Remoto, "Argos respondió un comprobante que Iris no reconoce; revisa la boleta en Argos")
	}
	if comprobanteAjeno(c, pre.boleta, false) != "" {
		return Boleta{}, comprobanteNoEsDeIris()
	}
	r, ok := resolucionDeComprobante(c)
	if !ok {
		// Sigue pendiente: otra persona tiene la decisión a medias (o la clave quedó de otra petición).
		return Boleta{}, nucleo.Conflicto(codigo, "Otra persona está decidiendo esta boleta; vuelve a abrirla en unos segundos")
	}
	// Quién decidió lo dice verificado_iris_actor (contrato 0.7.1); iris_actor es quien registró la boleta.
	res, err := k.releida.Ejecutar(context.WithoutCancel(ctx), entradaAplicar{Boleta: d.Boleta, Accion: d.Accion, ComprobanteID: pre.comprobante,
		EstadoArgos: c.Estado, Origen: "relectura", actorArgos: c.VerificadoIrisActor, r: r})
	if err != nil {
		return Boleta{}, err
	}
	if c.VerificadoIrisActor != nil && *c.VerificadoIrisActor == at.Actor && res.fila.Estado == d.estado() {
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
	ComprobanteID int64  `json:"comprobante_id"`
	Estado        string `json:"estado"`
	EmpresaID     *int64 `json:"empresa_id"`
}

// SuscribirComprobantes registra en el bus del relay (cmd/worker) el suscriptor de
// argos.comprobante_resuelto (webhooks comprobante.aprobado y comprobante.rechazado de Argos): marca
// la boleta de ese comprobante y esa empresa como «por confirmar». El despacho del proceso de
// mensajería relee el comprobante en Argos y aplica lo que diga. Idempotente.
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

type entradaReintento struct {
	ID    uuid.UUID `json:"boleta_id"`
	Error string    `json:"error"`
}

// reintentar anota que Argos no confirmó: otro intento con más espera o, agotados, la boleta queda
// marcada (mensaje de sistema y boleta.confirmacion_agotada). true si se agotó.
func (k *DecidirBoleta) reintentar(ctx context.Context, t *auditoria.Tx, in entradaReintento) (bool, error) {
	q := mensajeriabd.New(t)
	f, err := q.BoletaParaEnviar(ctx, in.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(f.Resolucion) == 0 || f.ResolucionAgotadaEn != nil {
		return false, nil
	}
	ahora := k.m.ahora()
	p := mensajeriabd.ReintentarResolucionBoletaParams{ID: f.ID, Error: nombrePtr(recortar(in.Error, 300))}
	if i := int(f.ResolucionIntentos); i < len(EsperasConfirmacion) {
		proximo := ahora.Add(EsperasConfirmacion[i])
		p.Proximo = &proximo
		_, err := q.ReintentarResolucionBoleta(ctx, p)
		return false, err
	}
	p.Proximo, p.AgotadaEn = f.ResolucionProximo, &ahora
	g, err := q.ReintentarResolucionBoleta(ctx, p)
	if err != nil {
		return false, err
	}
	t.FijarEntidad("boleta", g.ID)
	if g.EmpresaID != nil {
		t.FijarEmpresa(*g.EmpresaID)
	}
	t.RegistrarCambios("boleta", g.ID, map[string]any{"confirmacion": ConfirmacionPendiente}, map[string]any{"confirmacion": ConfirmacionAgotada})
	c, err := q.ConversacionClienteParaActualizar(ctx, g.ConversacionID)
	if err != nil {
		return false, err
	}
	if err := k.m.sistema(ctx, t, &c, EventoSistema{Tipo: "boleta_sin_confirmar", Referencia: nombrePtr("Boleta " + g.Referencia),
		EntidadID: nombrePtr(g.ID.String()), Detalle: g.ResolucionError}); err != nil {
		return false, err
	}
	if _, err := guardarConv(ctx, q, c, false); err != nil {
		return false, err
	}
	return true, t.Emitir("boleta.confirmacion_agotada", map[string]any{"boleta_id": g.ID, "conversacion_id": g.ConversacionID, "empresa_id": g.EmpresaID,
		"comprobante_id": g.ComprobanteID, "intentos": g.ResolucionIntentos, "detalle": g.ResolucionError})
}

// confirmarResolucion relee en Argos el comprobante de una boleta «por confirmar» y aplica lo que
// diga Argos (estado, monto, empresa y quién decidió), nunca el payload del webhook. Si Argos no lo
// confirma, queda para otro intento. El error que devuelve es solo de la base.
func (m *Modulo) confirmarResolucion(ctx context.Context, bd nucleo.BD, id uuid.UUID) error {
	f, err := mensajeriabd.New(bd).BoletaPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(f.Resolucion) == 0 || f.ResolucionAgotadaEn != nil {
		return nil
	}
	reintentar := func(motivo string) error {
		_, err := m.boletas.reintento.Ejecutar(ctx, entradaReintento{ID: id, Error: motivo})
		return err
	}
	if m.cfg.Argos == nil || f.ComprobanteID == nil {
		return reintentar("sin conector con Argos en este proceso")
	}
	// La lectura va a nombre de quien registró la boleta (como su envío): no hay persona en el despacho.
	a := alcanceConv{}
	if f.RegistradaPor != nil {
		a.usuario = *f.RegistradaPor
	}
	if f.EmpresaID != nil {
		a.empresa = *f.EmpresaID
	}
	c, err := m.cfg.Argos.ObtenerComprobante(ctx, atencionDe(a, f.ConversacionID), *f.ComprobanteID)
	if err != nil {
		motivo, _ := resumenErrorArgos(err)
		return reintentar(motivo)
	}
	if ajeno := comprobanteAjeno(c, f, true); ajeno != "" {
		return reintentar(ajeno)
	}
	r, ok := resolucionDeComprobante(c)
	if !ok {
		return reintentar("Argos todavía lo tiene pendiente")
	}
	_, err = m.boletas.sistema.Ejecutar(ctx, entradaAplicar{Boleta: id, ComprobanteID: *f.ComprobanteID, EstadoArgos: c.Estado, Origen: "webhook",
		actorArgos: c.VerificadoIrisActor, r: r})
	if err != nil {
		return reintentar("no se pudo guardar la resolución")
	}
	return nil
}

// despacharResoluciones confirma con Argos las resoluciones que avisó su webhook (hasta 50). Un error
// en una boleta no corta la corrida: cada una lleva su cuenta de intentos.
func (m *Modulo) despacharResoluciones(ctx context.Context, bd nucleo.BD) error {
	ahora := m.ahora()
	ids, err := mensajeriabd.New(bd).BoletasPorResolver(ctx, &ahora)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := m.confirmarResolucion(ctx, bd, id); err != nil {
			errs = append(errs, errors.New("confirmar la boleta "+id.String()+": "+err.Error()))
		}
	}
	return errors.Join(errs...)
}
