package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
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

// Reconexión temporal sin personal de caja (contrato con Argos 0.8.0 §7.3, bloque B4; §10.2 puntos
// 1, 2 y 12). El motivo lo decide SIEMPRE el servidor, con el reloj y la disponibilidad: nunca lo
// que diga el cliente ni el modelo (la herramienta pedir_reconexion no lleva argumentos).
//
//   - Fuera del horario hábil: fuera_de_horario, como en 0.7.1.
//   - En horario hábil: sin_personal solo si (1) la empresa lo encendió (apagado de fábrica, su
//     propio interruptor, lo enciende un admin), (2) el Argos conectado informa contrato ≥ 0.8.0,
//     (3) la empresa tiene al menos una persona con el rol caja y (4) ninguna está disponible en la
//     bandeja en este momento (sin espera de minutos). Si algo falta: 422 reconexion_no_corresponde
//     sin llamar a Argos, y la boleta la revisa una persona.
//
// La verificación fuerte y el comprobante registrado se exigen igual con los dos motivos. El cupo es
// uno solo para los dos (lo controla Argos: su 422 reconexion_no_permitida/cupo_agotado).

// ReconexionSinPersonal: motivo de Argos 0.8.0 (§7.3).
const ReconexionSinPersonal = argos.MotivoReconexionSinPersonal

// ArgosSinPersonal dice si el Argos conectado acepta motivo sin_personal (contrato ≥ 0.8.0). Es
// opcional: un cliente de Argos que no lo implementa se trata como uno viejo (nunca se pide).
type ArgosSinPersonal interface {
	ReconexionSinPersonal(ctx context.Context, q conector.DBTX) (bool, error)
}

// argosSinPersonal: falla cerrado (false) sin Argos o si no se puede saber.
func (m *Modulo) argosSinPersonal(ctx context.Context, q conector.DBTX) (bool, error) {
	a, ok := m.cfg.Argos.(ArgosSinPersonal)
	if !ok || m.cfg.Argos == nil {
		return false, nil
	}
	return a.ReconexionSinPersonal(ctx, q)
}

// criterioReconexion es lo que el servidor miró para decidir el motivo (va a la auditoría del
// intento: cuántas personas de caja, nunca quiénes).
type criterioReconexion struct {
	FueraDeHorario bool   `json:"fuera_de_horario"`
	Encendido      bool   `json:"sin_personal_encendido"`
	ArgosLoPermite bool   `json:"argos_080"`
	CajaTotal      int32  `json:"caja_total"`
	CajaDisp       int32  `json:"caja_disponibles"`
	Motivo         string `json:"motivo,omitempty"`
}

// motivoReconexion decide con qué motivo se pide la reconexión, o por qué no corresponde. El
// criterio queda anotado en la auditoría del intento (también si se rechaza).
func (m *Modulo) motivoReconexion(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion) (criterioReconexion, error) {
	var cr criterioReconexion
	fuera, err := m.fueraDeHorarioConv(ctx, q, c)
	if err != nil {
		return cr, err
	}
	cr.FueraDeHorario = fuera
	if fuera {
		cr.Motivo = ReconexionFueraDeHorario
		t.Anotar(map[string]any{"criterio": cr})
		return cr, nil
	}
	empresa := empresaDeConv(c)
	if aj, err := q.AjustesReconexion(ctx, empresa); err == nil {
		cr.Encendido = aj.SinPersonal
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return cr, err
	}
	if cr.ArgosLoPermite, err = m.argosSinPersonal(ctx, t); err != nil {
		return cr, err
	}
	caja, err := q.CajaDeEmpresa(ctx, mensajeriabd.CajaDeEmpresaParams{Ahora: m.ahora(), Empresa: empresa})
	if err != nil {
		return cr, err
	}
	cr.CajaTotal, cr.CajaDisp = caja.Total, caja.Disponibles
	if cr.Encendido && cr.ArgosLoPermite && cr.CajaTotal > 0 && cr.CajaDisp == 0 {
		cr.Motivo = ReconexionSinPersonal
	}
	t.Anotar(map[string]any{"criterio": cr})
	if cr.Motivo == "" {
		return cr, reglaConv("reconexion_no_corresponde", "En horario hábil la boleta la revisa una persona de caja: la reconexión temporal no corresponde ahora")
	}
	return cr, nil
}

// Lo que se le dice al cliente (§7.3 «Qué le dice el asistente al cliente», §10.2 punto 12) --------

var (
	diasSemana = [...]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	mesesAnio  = [...]string{"", "enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
)

// HastaReconexionTexto pasa el `hasta` de Argos a hora de Guatemala, en llano. Con una hora
// cualquiera: «las 10:00 de la mañana del jueves 19 de noviembre». A las 00:00 nombra los dos días:
// «la medianoche del miércoles 18 (00:00 del jueves 19)». Nunca «las 12:00 de la noche» (ambiguo) ni
// una hora de corte: `hasta` es el fin de la promesa, no el corte.
func HastaReconexionTexto(hasta time.Time, zona *time.Location) string {
	l := hasta.In(zona)
	if l.Hour() == 0 && l.Minute() == 0 {
		a := l.AddDate(0, 0, -1)
		return fmt.Sprintf("la medianoche del %s %d (00:00 del %s %d)", diasSemana[a.Weekday()], a.Day(), diasSemana[l.Weekday()], l.Day())
	}
	h := l.Hour() % 12
	if h == 0 {
		h = 12
	}
	periodo := "de la noche"
	switch hh := l.Hour(); {
	case hh < 6:
		periodo = "de la madrugada"
	case hh < 12:
		periodo = "de la mañana"
	case hh == 12:
		periodo = "del mediodía"
	case hh < 19:
		periodo = "de la tarde"
	}
	articulo := "las"
	if h == 1 {
		articulo = "la"
	}
	return fmt.Sprintf("%s %d:%02d %s del %s %d de %s", articulo, h, l.Minute(), periodo, diasSemana[l.Weekday()], l.Day(), mesesAnio[l.Month()])
}

// FraseReconexion es la frase que el servidor pone en el mensaje del asistente (la marca
// {{reconexion}}): hasta cuándo vale y que el comprobante queda pendiente de revisión.
func FraseReconexion(hasta time.Time, zona *time.Location) string {
	return "La reconexión temporal vale hasta " + HastaReconexionTexto(hasta, zona) + "; su comprobante queda pendiente de revisión."
}

var (
	// reMarcaReconexion: la marca que el servidor reemplaza por FraseReconexion.
	reMarcaReconexion = regexp.MustCompile(`\{\{\s*reconexion\s*\}\}\.?`)
	// reCorte: una hora de corte o «medianoche» escrita por el modelo. Nunca pasa: la única hora que
	// el cliente lee sobre la reconexión es la de FraseReconexion (§7.3).
	reCorte = regexp.MustCompile(`(?i)medianoche|12(?::00)?\s+de\s+la\s+noche|` +
		`(?:^|[^\p{L}])(?:se\s+(?:le\s+)?(?:corta|cortar[aá]|suspende|suspender[aá]|desconecta|desconectar[aá])|(?:te|le)\s+(?:cortamos|cortaremos|suspendemos|suspenderemos))(?:[^\p{L}]|$)|` +
		`(?:^|[^\p{L}])(?:hora|momento)\s+(?:del?\s+)?corte(?:[^\p{L}]|$)`)
	// reReconexionConCuando: el modelo habla de la reconexión y le pone él una fecha u hora.
	reReconexionMencion = regexp.MustCompile(`(?i)reconex|reconect`)
	reCuando            = regexp.MustCompile(`(?i)\b\d{1,2}:\d{2}\b|\b\d{1,2}\s*(?:a\.?\s?m\.?|p\.?\s?m\.?|hrs?\b|horas\b)|(?:^|[^\p{L}])(?:lunes|martes|mi[eé]rcoles|jueves|viernes|s[aá]bado|domingo|hoy|ma[ñn]ana|esta\s+noche|de\s+la\s+(?:tarde|noche|madrugada))(?:[^\p{L}]|$)`)
)

// cuandoInventado: el texto (antes de reemplazar las marcas) da una hora de corte, o pone por su
// cuenta cuándo vence la reconexión.
func cuandoInventado(texto string) bool {
	if reCorte.MatchString(texto) {
		return true
	}
	sinMarca := reMarcaReconexion.ReplaceAllString(texto, " ")
	return reReconexionMencion.MatchString(sinMarca) && reCuando.MatchString(sinMarca)
}

// conFraseReconexion pone la frase del servidor donde el modelo dejó la marca; si la reconexión se
// aceptó en este turno y el modelo no la dejó, la agrega al final (el cliente siempre sabe hasta
// cuándo vale y que su comprobante sigue pendiente).
func conFraseReconexion(t *turnoIA, texto string) (string, bool) {
	if reMarcaReconexion.MatchString(texto) {
		if t.fraseReconexion == "" {
			return "", false
		}
		return strings.TrimSpace(reMarcaReconexion.ReplaceAllString(texto, t.fraseReconexion)), true
	}
	if t.fraseReconexion != "" {
		return strings.TrimSpace(texto + " " + t.fraseReconexion), true
	}
	return texto, true
}

// Ajustes de la reconexión (el interruptor) --------------------------------------------------------

// CajaReconexion: cuántas personas con el rol caja operan en la empresa y cuántas están disponibles
// ahora en la bandeja.
type CajaReconexion struct {
	Total       int32 `json:"total"`
	Disponibles int32 `json:"disponibles"`
}

// AjustesReconexion son los ajustes de la reconexión temporal de la empresa.
type AjustesReconexion struct {
	SinPersonal    bool           `json:"sinPersonal" doc:"También en horario hábil cuando nadie de caja está disponible (apagado de fábrica)"`
	Version        int32          `json:"version" doc:"0 si nunca se cambiaron"`
	ArgosLoPermite bool           `json:"argosLoPermite" doc:"El Argos conectado informa contrato ≥ 0.8.0; sin eso nunca se pide sin_personal"`
	ContratoMinimo string         `json:"contratoMinimo" doc:"El contrato con Argos que hace falta (0.8.0)"`
	Caja           CajaReconexion `json:"caja"`
	ActualizadoEn  *time.Time     `json:"actualizadoEn" nullable:"true"`
}

// EntradaAjustesReconexion es POST /asistente/reconexion/actualizar.
type EntradaAjustesReconexion struct {
	Version     int32 `json:"version" minimum:"0"`
	SinPersonal bool  `json:"sinPersonal"`
}

func (m *Modulo) ajustesReconexion(ctx context.Context, q *mensajeriabd.Queries, t conector.DBTX, empresa int64, f *mensajeriabd.MensajeriaAjustesReconexion) (AjustesReconexion, error) {
	r := AjustesReconexion{ContratoMinimo: argos.ContratoSinPersonal}
	if f != nil {
		r.SinPersonal, r.Version, r.ActualizadoEn = f.SinPersonal, f.Version, &f.UpdatedAt
	}
	var err error
	if r.ArgosLoPermite, err = m.argosSinPersonal(ctx, t); err != nil {
		return r, err
	}
	caja, err := q.CajaDeEmpresa(ctx, mensajeriabd.CajaDeEmpresaParams{Ahora: m.ahora(), Empresa: empresa})
	if err != nil {
		return r, err
	}
	r.Caja = CajaReconexion{Total: caja.Total, Disponibles: caja.Disponibles}
	return r, nil
}

func nuevoVerAjustesReconexion(m *Modulo) *auditoria.Caso[struct{}, AjustesReconexion] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AjustesReconexion]{
		Accion: "mensajeria.ver_ajustes_reconexion", Permiso: nucleo.PermMensajeriaAsistenteConfigurar, AuditarExito: sinAuditar[AjustesReconexion],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AjustesReconexion, error) {
			empresa, err := empresaPersona(ctx)
			if err != nil {
				return AjustesReconexion{}, err
			}
			q := mensajeriabd.New(t)
			f, err := q.AjustesReconexion(ctx, empresa)
			if errors.Is(err, pgx.ErrNoRows) {
				return m.ajustesReconexion(ctx, q, t, empresa, nil)
			}
			if err != nil {
				return AjustesReconexion{}, err
			}
			return m.ajustesReconexion(ctx, q, t, empresa, &f)
		},
	})
}

func nuevoFijarAjustesReconexion(m *Modulo) *auditoria.Caso[EntradaAjustesReconexion, AjustesReconexion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAjustesReconexion, AjustesReconexion]{
		Accion: "mensajeria.ajustes_reconexion", Permiso: nucleo.PermMensajeriaAsistenteConfigurar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAjustesReconexion) (AjustesReconexion, error) {
			empresa, err := empresaPersona(ctx)
			if err != nil {
				return AjustesReconexion{}, err
			}
			usuario := contexto.ActorDe(ctx).ID
			t.FijarEmpresa(empresa)
			q := mensajeriabd.New(t)
			f, err := q.AjustesReconexionParaActualizar(ctx, empresa)
			anterior := false
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if in.Version != 0 {
					return AjustesReconexion{}, nucleo.ConflictoVersion("ajustes_reconexion", empresa)
				}
				f, err = q.InsertarAjustesReconexion(ctx, mensajeriabd.InsertarAjustesReconexionParams{EmpresaID: empresaPtrDe(empresa),
					SinPersonal: in.SinPersonal, ActualizadoPor: &usuario})
				if errors.Is(err, pgx.ErrNoRows) {
					return AjustesReconexion{}, nucleo.ConflictoVersion("ajustes_reconexion", empresa)
				}
				if err != nil {
					return AjustesReconexion{}, err
				}
			case err != nil:
				return AjustesReconexion{}, err
			default:
				if f.Version != in.Version {
					return AjustesReconexion{}, nucleo.ConflictoVersion("ajustes_reconexion", empresa)
				}
				anterior = f.SinPersonal
				if f, err = q.ActualizarAjustesReconexion(ctx, mensajeriabd.ActualizarAjustesReconexionParams{ID: f.ID, SinPersonal: in.SinPersonal,
					ActualizadoPor: &usuario}); err != nil {
					return AjustesReconexion{}, err
				}
			}
			t.FijarEntidad("ajustes_reconexion", f.ID)
			campos := t.RegistrarCambios("ajustes_reconexion", f.ID, map[string]any{"sin_personal": anterior}, map[string]any{"sin_personal": f.SinPersonal})
			if campos == nil {
				campos = []string{}
			}
			r, err := m.ajustesReconexion(ctx, q, t, empresa, &f)
			if err != nil {
				return AjustesReconexion{}, err
			}
			return r, t.Emitir("reconexion.ajustes_cambiados", map[string]any{"empresa_id": f.EmpresaID, "campos": campos, "sin_personal": f.SinPersonal})
		},
	})
}

// Webhooks promesa.* de Argos (por el outbox) ------------------------------------------------------

// entradaPromesaArgos es el payload de argos.promesa_cambiada (sin monto).
type entradaPromesaArgos struct {
	PromesaID        int64      `json:"promesa_id"`
	Estado           string     `json:"estado"`
	Tipo             string     `json:"tipo"`
	EmpresaID        *int64     `json:"empresa_id"`
	ComprobanteID    *int64     `json:"comprobante_id"`
	Vence            *time.Time `json:"vence"`
	MotivoReconexion *string    `json:"motivo_reconexion"`
	Motivo           *string    `json:"motivo"`
	EntranteID       int64      `json:"entrante_id"`
}

// promesaAplicada: si la reconexión local cambió con el webhook.
type promesaAplicada struct {
	Aplicada bool `json:"aplicada"`
}

// nuevoReconexionArgos guarda en la reconexión local lo que Argos cuenta de su promesa (activa,
// cumplida, incumplida o cancelada, con su motivo_reconexion), deja el mensaje de sistema en la
// conversación y emite reconexion.actualizada. Idempotente: el mismo entrante no se aplica dos veces
// y un estado final no lo pisa un webhook atrasado.
func nuevoReconexionArgos(m *Modulo) *auditoria.Caso[entradaPromesaArgos, promesaAplicada] {
	return auditoria.Nuevo(auditoria.Caso[entradaPromesaArgos, promesaAplicada]{
		Accion: "conversaciones.reconexion_argos", Acceso: auditoria.Autenticado,
		AuditarExito: func(r promesaAplicada) bool { return r.Aplicada },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPromesaArgos) (promesaAplicada, error) {
			if err := soloSistema(ctx); err != nil {
				return promesaAplicada{}, err
			}
			if in.Tipo != "reconexion_temporal" || in.ComprobanteID == nil || in.PromesaID <= 0 {
				return promesaAplicada{}, nil
			}
			switch in.Estado {
			case "activa", "cumplida", "incumplida", "cancelada":
			default:
				return promesaAplicada{}, nil
			}
			q := mensajeriabd.New(t)
			f, err := q.ReconexionDePromesa(ctx, mensajeriabd.ReconexionDePromesaParams{ComprobanteID: *in.ComprobanteID, Empresa: derefEmpresa(in.EmpresaID),
				PromesaID: in.PromesaID})
			if errors.Is(err, pgx.ErrNoRows) {
				return promesaAplicada{}, nil
			}
			if err != nil {
				return promesaAplicada{}, err
			}
			motivoArgos := argos.MotivoReconexionValido(in.MotivoReconexion)
			var fin *string
			if in.Estado == "cancelada" {
				v := "otro"
				if in.Motivo != nil && (*in.Motivo == "comprobante_rechazado" || *in.Motivo == "vencida_sin_revisar") {
					v = *in.Motivo
				}
				fin = &v
			}
			g, err := q.ActualizarReconexionArgos(ctx, mensajeriabd.ActualizarReconexionArgosParams{ID: f.ID, ArgosEstado: in.Estado,
				ArgosMotivo: motivoArgos, ArgosFinMotivo: fin, EntranteID: in.EntranteID, Vence: in.Vence})
			if errors.Is(err, pgx.ErrNoRows) {
				return promesaAplicada{}, nil
			}
			if err != nil {
				return promesaAplicada{}, err
			}
			c, err := q.ConversacionClienteParaActualizar(ctx, g.ConversacionID)
			if err != nil {
				return promesaAplicada{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			motivo := g.Motivo
			if g.ArgosMotivo != nil {
				motivo = *g.ArgosMotivo
			}
			if motivoArgos != nil && *motivoArgos != g.Motivo {
				// Argos la guardó con otro motivo: se muestra el de Argos y queda anotado.
				t.Anotar(map[string]any{"motivo_distinto": map[string]string{"iris": g.Motivo, "argos": *motivoArgos}})
			}
			t.RegistrarCambios("reconexion", g.ID, map[string]any{"argos_estado": f.ArgosEstado, "argos_motivo": f.ArgosMotivo, "hasta": f.Hasta},
				map[string]any{"argos_estado": g.ArgosEstado, "argos_motivo": g.ArgosMotivo, "argos_fin_motivo": g.ArgosFinMotivo, "hasta": g.Hasta})
			ev := EventoSistema{Tipo: "reconexion_actualizada", ActorNombre: nombrePtr("Argos"), Detalle: &in.Estado, Motivo: &motivo, Fin: fin}
			if in.Estado == "activa" {
				ev.Hasta = g.Hasta
			}
			if err := m.sistema(ctx, t, &c, ev); err != nil {
				return promesaAplicada{}, err
			}
			return promesaAplicada{Aplicada: true}, t.Emitir("reconexion.actualizada", map[string]any{"reconexion_id": g.ID, "conversacion_id": c.ID,
				"empresa_id": c.EmpresaID, "comprobante_id": g.ComprobanteID, "promesa_argos_id": in.PromesaID, "estado": in.Estado, "motivo": motivo,
				"fin": fin, "hasta": g.Hasta, "entrante_id": in.EntranteID})
		},
	})
}

// SuscribirPromesas registra en el bus del relay (cmd/worker) el suscriptor de argos.promesa_cambiada
// (webhooks promesa.* de Argos): actualiza la reconexión temporal de la conversación. Idempotente.
func (m *Modulo) SuscribirPromesas(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("mensajeria.reconexiones", argos.EventoPromesaCambiada, func(ctx context.Context, e integracion.Evento) error {
		var in entradaPromesaArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return nil // un payload ilegible no se arregla reintentando
		}
		_, err := m.Asistente.reconexionArgos.Ejecutar(ctxSistema(ctx, bd), in)
		return err
	})
}

// Aviso al cliente de cómo terminó (§7.3 «Cómo termina») ------------------------------------------

// TextoFinReconexion es lo que el servidor le dice al cliente cuando Argos avisa el final de su
// reconexión temporal. Nunca montos ni una hora de corte: el corte lo decide Argos con su plantilla.
// "" = no se avisa (activa: el cliente ya lo supo al pedirla).
func TextoFinReconexion(estado string, fin *string) string {
	switch estado {
	case "cumplida":
		return "Su pago quedó confirmado y la reconexión temporal ya no hace falta. ¡Gracias!"
	case "incumplida":
		return "La reconexión temporal terminó. El pago registrado no cubrió todo el saldo vencido, así que el servicio puede volver a suspenderse. " +
			"Si necesita ayuda, escríbanos y una persona le atiende."
	case "cancelada":
		switch {
		case fin != nil && *fin == "comprobante_rechazado":
			return "Caja no pudo confirmar su comprobante, así que la reconexión temporal terminó y el servicio puede volver a suspenderse. " +
				"Si cree que es un error, escríbanos y una persona lo revisa."
		case fin != nil && *fin == "vencida_sin_revisar":
			return "La reconexión temporal terminó. Su comprobante sigue pendiente de revisión en caja: si se confirma, el servicio vuelve a activarse. " +
				"Mientras tanto, el servicio puede volver a suspenderse."
		default:
			return "La reconexión temporal terminó. Si tiene dudas, escríbanos y una persona le atiende."
		}
	}
	return ""
}

// uuidReconexion: el id de una reconexión (entrada del aviso de su final).
type uuidReconexion = uuid.UUID

// avisoFinReconexion: lo que se decidió con el aviso de un final.
type avisoFinReconexion struct {
	ID      uuid.UUID  `json:"reconexion_id"`
	Aviso   string     `json:"aviso,omitempty"`
	Mensaje *uuid.UUID `json:"mensaje_id,omitempty"`
	hecho   bool
}

// nuevoAvisarFinReconexion manda (una sola vez) el aviso de cómo terminó la reconexión: solo como
// texto dentro de la ventana de 24 h. Fuera de ella no hay una plantilla aprobada para estos finales
// (contrato con Argos §6.3), así que no se manda nada: queda sin_aviso y el final se ve en el hilo.
// Corre en el proceso de mensajería (mantenimiento), que es el que encola los envíos.
func nuevoAvisarFinReconexion(m *Modulo) *auditoria.Caso[uuidReconexion, avisoFinReconexion] {
	return auditoria.Nuevo(auditoria.Caso[uuidReconexion, avisoFinReconexion]{
		Accion: "conversaciones.reconexion_aviso", Acceso: auditoria.Autenticado,
		AuditarExito: func(r avisoFinReconexion) bool { return r.hecho },
		Logica: func(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (avisoFinReconexion, error) {
			if err := soloSistema(ctx); err != nil {
				return avisoFinReconexion{}, err
			}
			q := mensajeriabd.New(t)
			f, err := q.ReconexionPorAvisar(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return avisoFinReconexion{ID: id}, nil
			}
			if err != nil {
				return avisoFinReconexion{}, err
			}
			c, err := q.ConversacionClienteParaActualizar(ctx, f.ConversacionID)
			if err != nil {
				return avisoFinReconexion{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			estado := ""
			if f.ArgosEstado != nil {
				estado = *f.ArgosEstado
			}
			aviso, mensaje := AvisoBoletaSinAvis, (*uuid.UUID)(nil)
			if texto := TextoFinReconexion(estado, f.ArgosFinMotivo); texto != "" {
				if aviso, mensaje, err = m.avisoDentroDeVentana(ctx, t, q, &c, texto); err != nil {
					return avisoFinReconexion{}, err
				}
			}
			g, err := q.MarcarAvisoReconexion(ctx, mensajeriabd.MarcarAvisoReconexionParams{ID: f.ID, Aviso: aviso, MensajeID: pgUUID(mensaje)})
			if err != nil {
				return avisoFinReconexion{}, err
			}
			t.RegistrarCambios("reconexion", g.ID, map[string]any{"argos_aviso": "pendiente"}, map[string]any{"argos_aviso": aviso})
			return avisoFinReconexion{ID: g.ID, Aviso: aviso, Mensaje: mensaje, hecho: true}, t.Emitir("reconexion.cliente_avisado", map[string]any{
				"reconexion_id": g.ID, "conversacion_id": c.ID, "empresa_id": c.EmpresaID, "estado": estado, "fin": g.ArgosFinMotivo,
				"aviso_cliente": aviso, "mensaje_id": mensaje})
		},
	})
}

// avisoDentroDeVentana deja un texto del servidor para el cliente si la ventana de 24 h está abierta,
// el número sigue activo para la empresa, el cliente no se dio de baja y (en una conversación resuelta
// de un número compartido) ninguna otra empresa está conversando con ese teléfono.
func (m *Modulo) avisoDentroDeVentana(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, c *mensajeriabd.MensajeriaConversacion,
	texto string) (string, *uuid.UUID, error) {
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
	puede, err := m.puedeContestarBot(ctx, q, *c)
	if err != nil || !puede {
		return AvisoBoletaSinAvis, nil, err
	}
	if c.Estado == EstadoConvResuelta {
		var pr *nucleo.Problema
		if err := otraEmpresaConversa(ctx, q, canal.ID, empresaDeConv(*c), c.Numero); errors.As(err, &pr) {
			return AvisoBoletaSinAvis, nil, nil
		} else if err != nil {
			return "", nil, err
		}
	}
	msg, err := m.mensajeAutomatico(ctx, t, c, texto, "bot")
	if err != nil {
		return "", nil, err
	}
	return AvisoBoletaEnviado, &msg.ID, nil
}

// avisarFinesReconexion manda los avisos pendientes (mantenimiento del proceso de mensajería).
func (m *Modulo) avisarFinesReconexion(ctx context.Context, bd nucleo.BD, log *slog.Logger) error {
	ids, err := mensajeriabd.New(bd).ReconexionesPorAvisar(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := m.Asistente.avisarFinReconexion.Ejecutar(ctx, id); err != nil {
			log.WarnContext(ctx, "mensajeria: aviso del final de una reconexión", "reconexion_id", id, "err", err)
		}
	}
	return nil
}
