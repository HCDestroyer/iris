package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// maxPorCorrida acota una corrida (lo que quede, lo toma la siguiente).
const maxPorCorrida = 500

// Procesar ejecuta todo lo vencido hasta ahora (CLAUDE.md §6: «todo lo pendiente hasta
// ahora»), uno por uno: toma el pendiente en una transacción corta (en_curso), llama a Argos
// fuera de transacción y guarda el resultado en un caso de uso (auditado, con su evento).
// Devuelve cuántos procesó. ctx debe traer el pool (nucleo.ConBD) y el actor sistema.
func (m *Modulo) Procesar(ctx context.Context) (int, error) {
	ahora := m.ahora()
	// Red de seguridad: dependientes con marcadores que quedaron sin resolver (no debería haber).
	if _, err := m.resolverListos.Ejecutar(ctx, ahora); err != nil {
		return 0, fmt.Errorf("pendientes: resolver dependientes listos: %w", err)
	}
	n := 0
	for n < maxPorCorrida {
		f, ok, err := m.tomar(ctx, ahora)
		if err != nil || !ok {
			return n, err
		}
		n++
		res := m.intentar(ctx, f)
		res.ahora = ahora
		if _, err := m.resolver.Ejecutar(ctx, res); err != nil {
			return n, fmt.Errorf("pendientes: guardar el resultado de %d: %w", f.ID, err)
		}
	}
	return n, nil
}

func (m *Modulo) tomar(ctx context.Context, ahora time.Time) (pendientesbd.IntegracionPendiente, bool, error) {
	var f pendientesbd.IntegracionPendiente
	hay := false
	err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := pendientesbd.New(tx)
		var err error
		f, err = q.TomarVencido(ctx, pendientesbd.TomarVencidoParams{Ahora: ahora, ReclamarAntes: ahora.Add(-m.cfg.Reclamo)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		hay = true
		return q.MarcarEnCurso(ctx, pendientesbd.MarcarEnCursoParams{Ahora: ahora, ID: f.ID})
	})
	return f, hay, err
}

// resultadoIntento es lo que pasó al llamar a Argos.
type resultadoIntento struct {
	ID int64 `json:"pendiente_id"`
	// Exito, o error de negocio (Negocio = true) o temporal.
	Exito   bool   `json:"exito"`
	Negocio bool   `json:"negocio"`
	Status  *int32 `json:"status"`
	Error   string `json:"error,omitempty"`
	// Codigo: type del problem+json de Argos (tecnico_no_encontrado, existencia_insuficiente…).
	Codigo string `json:"codigo,omitempty"`
	// Conciliado: un 409 que significa «ya está hecho en Argos» (cargo_duplicado, traslado_ya_recibido).
	Conciliado string `json:"conciliado,omitempty"`

	// respuesta del 2xx, ya limpia (no va a la auditoría: queda en el pendiente).
	respuesta []byte
	ahora     time.Time
	// sinRespuesta: la llamada salió y Argos no respondió (timeout, red o 5xx): pudo hacerlo.
	sinRespuesta bool
	// conceptoInstalacion: con un cargo rechazado por 422 capacidad_no_disponible, el concepto de
	// instalación de la empresa (para el cobro de reemplazo); 0 si no tiene.
	conceptoInstalacion int64
}

// intentar hace la llamada a Argos y clasifica el resultado.
func (m *Modulo) intentar(ctx context.Context, f pendientesbd.IntegracionPendiente) resultadoIntento {
	r := resultadoIntento{ID: f.ID}
	// Un paso del titular de una orden ya cancelada o fallida no sale (lo detiene el resolver).
	if como, err := ordenTerminada(ctx, pendientesbd.New(nucleo.BDDe(ctx)), f); err != nil {
		r.Error = "no se pudo leer la orden: " + err.Error()
		return r
	} else if como != "" {
		r.Error = "la orden se canceló o falló: el paso del titular no sale"
		return r
	}
	if m.cfg.Argos == nil {
		r.Error = "conector Argos no configurado en el worker"
		return r
	}
	cliente, err := m.cfg.Argos.ClienteActivo(ctx, nucleo.BDDe(ctx))
	if err != nil {
		// Sin conector activo (o sin poder descifrar su token): cuenta como intento fallido y
		// se reintenta con la misma espera; al agotarse, alerta.
		r.Error = "sin conector Argos activo: " + resumenProblema(err)
		return r
	}
	var cuerpo any = json.RawMessage(f.Payload)
	var crudo conector.Crudo
	cli := cliente.Cliente
	if f.Operacion == OpRecepcion && f.EmpresaID != nil {
		// Sin servicio, Argos toma la bodega del técnico en la empresa de X-Empresa-Id (0.5.0 §3.3).
		cli = cli.ConCabecera(argos.CabeceraEmpresaArgos, strconv.FormatInt(*f.EmpresaID, 10))
	}
	if f.Actor != nil {
		// La persona que actúa (contrato §3.4, §3.5 y §7.1): Argos audita con ella.
		cli = cli.ConCabecera(CabeceraActorIris, *f.Actor)
	}
	if f.Operacion == OpCambiarWifi && f.Entidad == EntidadTransitoConversacion {
		// El cambio de Wi-Fi desde una conversación lleva su id (§7.1 y §12.4).
		cli = cli.ConCabecera(CabeceraConversacionIris, f.EntidadID)
	}
	var partes []conector.Parte
	if f.TransitoID.Valid {
		d, err := m.leerTransito(ctx, pendientesbd.New(nucleo.BDDe(ctx)), f)
		if errors.Is(err, errVencido) && conClaveWifi(f.Operacion) {
			// La clave Wi-Fi ya se descartó (rechazo o 7 días): se pide otra vez (otro intento).
			r.Negocio, r.Codigo, r.Error = true, CodigoClaveDescartada, "la clave Wi-Fi ya se descartó: pide el cambio otra vez"
			return r
		}
		if errors.Is(err, errVencido) {
			// Se borró a los 7 días (o al resolverse): no se manda nada; hay que tomarlo otra vez.
			r.Negocio, r.Codigo, r.Error = true, CodigoFotoVencida, "los datos del titular se borraron (7 días): hay que tomarlos otra vez"
			return r
		}
		if err != nil {
			r.Error = err.Error()
			return r
		}
		if esMultipart(f.Operacion) {
			if partes, err = partesMultipart(f.Operacion, f.Payload, d); err != nil {
				r.Error = "no se pudo armar el multipart: " + err.Error()
				return r
			}
		} else {
			mezcla, err := cuerpoConTransito(f.Payload, d)
			if err != nil {
				r.Error = "no se pudo armar el cuerpo: " + err.Error()
				return r
			}
			cuerpo = mezcla
		}
	}
	if partes != nil {
		err = cli.EscribirPartes(ctx, f.Ruta, partes, f.IdempotencyKey, &crudo)
	} else {
		err = cli.Escribir(ctx, f.Metodo, f.Ruta, cuerpo, f.IdempotencyKey, &crudo)
	}
	if err == nil {
		r.Exito, r.respuesta = true, limpiarRespuesta(crudo)
		if len(crudo) > 0 && r.respuesta == nil {
			slog.WarnContext(ctx, "pendientes: respuesta de Argos sin guardar (no es JSON, pasa de 64 KiB o no trae campos de la lista blanca)", "pendiente", f.ID, "bytes", len(crudo))
		}
		if motivo := inconsistencia(f.Verificar, f.Payload, r.respuesta); motivo != "" {
			// Argos hizo algo, pero no lo que se le pidió: no es «hecho» (los dependientes no se
			// resuelven con esta respuesta) y reintentar no lo arregla.
			r.Exito, r.Negocio, r.Error = false, true, motivo
		}
		return r
	}
	var er *conector.ErrorRemoto
	if !errors.As(err, &er) {
		r.Error = "error al llamar a Argos: " + sinCredenciales(err.Error())
		r.sinRespuesta = true
		return r
	}
	r.sinRespuesta = !er.Bloqueado() && (er.Status == 0 || er.Status >= 500)
	if er.Status != 0 {
		s := int32(er.Status) //nolint:gosec // código HTTP
		r.Status = &s
	}
	r.Codigo = TipoDe(er)
	if bodegaRequerida(er, f.Payload) {
		// Argos 1.5.x no entiende tecnico (contrato 0.5.0 §3.3): «bodega_id required».
		r.Codigo = CodigoBodegaRequerida
	}
	if er.Status == http.StatusConflict && f.Operacion == OpActivarInstalacion && r.Codigo == CodigoServicioNoPendiente &&
		activacionLista(ctx, nucleo.BDDe(ctx), f) {
		// La orden tiene una activación lista (§12.2): Argos ya activó el servicio; no es error (§3).
		r.Exito, r.Conciliado = true, r.Codigo
		return r
	}
	if er.Status == http.StatusConflict && conciliable(f.Operacion, r.Codigo) {
		// Argos ya lo tiene hecho (contrato 0.5.0 §3.3): cuenta como hecho, con la nota.
		r.Exito, r.Conciliado = true, r.Codigo
		return r
	}
	if f.Operacion == OpRecepcion && r.Codigo == CodigoIdempotenciaReutilizada && m.trasladoYaRecibido(ctx, f) {
		// La clave ya viajó con otro cuerpo (una recepción rehecha o tomada): si Argos muestra el
		// traslado recibido, queda hecho, conciliado como traslado_ya_recibido.
		r.Exito, r.Conciliado = true, CodigoTrasladoYaRecibido
		return r
	}
	r.Negocio = EsNegocio(er)
	r.Error = describir(er)
	if r.Negocio && esCargoSinCapacidad(f.Operacion, er.Status, r.Codigo) && f.ReemplazoDe == nil && !f.EnvioSinRespuesta {
		// El cargo queda terminal y sus demás líneas salen como un cobro de reemplazo (0.14.3 §3): hace
		// falta el concepto de instalación de la empresa. Si no se puede leer ahora, el intento cuenta
		// como temporal (Argos no guarda la clave en un 4xx: repetirlo es seguro) y se reintenta.
		id, err := m.cfg.Argos.ConceptoInstalacion(ctx, deref64(f.EmpresaID))
		switch {
		case err == nil:
			r.conceptoInstalacion = id
		case errors.Is(err, argos.ErrSinConceptoInstalacion):
			// Sin concepto de instalación no se sabe qué línea quitar: no hay reemplazo (se avisa).
		default:
			r.Negocio = false
			r.Error += " (no se pudo leer el concepto de instalación para el cobro de reemplazo; se reintenta)"
		}
	}
	return r
}

// CodigoCapacidadNoDisponible: Argos ≥ 0.14.3 rechaza una llamada sin su capacidad (§4.3).
const CodigoCapacidadNoDisponible = "capacidad_no_disponible"

// esCargoSinCapacidad: un cargo rechazado con 422 capacidad_no_disponible (cobro_equipo en false).
func esCargoSinCapacidad(operacion string, status int, codigo string) bool {
	return operacion == OpCargo && status == http.StatusUnprocessableEntity && codigo == CodigoCapacidadNoDisponible
}

func deref64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// Códigos de Argos 0.5.0 (§3.3) que Iris trata aparte.
const (
	// CodigoCargoDuplicado: el cargo único de la orden es de un producto que Argos ya cobró por
	// modalidad (consumible_cobro) en el consumo de esa orden: ya está cobrado.
	CodigoCargoDuplicado = "cargo_duplicado"
	// CodigoTrasladoYaRecibido: otra recepción de la misma orden de traslado ya quedó (2xx definitivo).
	CodigoTrasladoYaRecibido = "traslado_ya_recibido"
)

// CodigoIdempotenciaReutilizada: misma Idempotency-Key con otro cuerpo (422, contrato §3).
const CodigoIdempotenciaReutilizada = "idempotency_key_reutilizada"

// trasladoYaRecibido confirma con Argos (GET /inventario/traslados/:id, con la protección de
// lecturas del conector) si el traslado de una recepción ya está recibido.
func (m *Modulo) trasladoYaRecibido(ctx context.Context, f pendientesbd.IntegracionPendiente) bool {
	id, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if err != nil || id <= 0 {
		return false
	}
	var empresa int64
	if f.EmpresaID != nil {
		empresa = *f.EmpresaID
	}
	if err := m.cfg.Argos.ConfirmarTraslado(ctx, id, empresa); err != nil {
		slog.WarnContext(ctx, "pendientes: no se pudo confirmar el traslado tras idempotency_key_reutilizada", "pendiente", f.ID, "err", err)
		return false
	}
	t, err := argosbd.New(nucleo.BDDe(ctx)).TrasladoPorID(ctx, id)
	return err == nil && t.Estado == "recibido"
}

// conciliable: un 409 que significa que la acción ya está hecha en Argos.
func conciliable(operacion, codigo string) bool {
	return (operacion == OpCargo && codigo == CodigoCargoDuplicado) || (operacion == OpRecepcion && codigo == CodigoTrasladoYaRecibido)
}

// CodigosCorregibles son los rechazos de Argos por el técnico (contrato 0.5.0 §3.3) que se arreglan
// vinculando bien al técnico (o dándole bodega en Argos) y reintentando a mano: el reintento manda el
// vínculo actual de su ficha.
var CodigosCorregibles = map[string]bool{
	"tecnico_no_encontrado": true, "tecnico_sin_bodega": true, "tecnico_ambiguo": true, "empresa_requerida": true,
	CodigoBodegaRequerida: true,
}

// CodigoBodegaRequerida: Argos (1.5.x) rechazó un cuerpo con tecnico y sin bodega_id con un 422 de
// validación sobre bodega_id. Corregible: al reintentar se replanifica con bodega_id si Argos ya no
// tiene la capacidad inventario_por_tecnico.
const CodigoBodegaRequerida = "bodega_id_requerida"

// bodegaRequerida: 422 de validación que nombra bodega_id («bodega_id required»).
func bodegaRequerida(er *conector.ErrorRemoto, payload []byte) bool {
	if er.Status != http.StatusUnprocessableEntity {
		return false
	}
	var cuerpo map[string]json.RawMessage
	if json.Unmarshal(payload, &cuerpo) != nil {
		return false
	}
	if _, conTecnico := cuerpo["tecnico"]; !conTecnico {
		return false // sin tecnico, un error en bodega_id es de la bodega enviada, no de la versión
	}
	if slices.Contains(er.Campos, "bodega_id") {
		return true // errors[].field == "bodega_id"
	}
	texto := strings.ToLower(er.Detalle + " " + er.Titulo)
	return strings.Contains(texto, "bodega_id") && (strings.Contains(texto, "required") || strings.Contains(texto, "requerid") || strings.Contains(texto, "obligatori"))
}

// EsCorregible indica si el último error de Argos se corrige con el vínculo del técnico.
func EsCorregible(codigo *string) bool { return codigo != nil && CodigosCorregibles[*codigo] }

// MotivoInconsistente: la respuesta del 2xx trae un campo de identidad distinto del enviado.
const MotivoInconsistente = "respuesta_inconsistente"

// inconsistencia compara los campos de verificar entre el cuerpo enviado y la respuesta
// guardada. Un campo que la respuesta no trae no se puede comparar (no es inconsistencia).
func inconsistencia(verificar []string, enviado, respuesta []byte) string {
	for _, c := range verificar {
		recibido, ok := CampoDeRespuesta(respuesta, c)
		if !ok {
			continue
		}
		mandado, _ := CampoDeRespuesta(enviado, c)
		if escalar(recibido) != escalar(mandado) {
			return fmt.Sprintf("%s: Argos respondió %s %q y se mandó %q", MotivoInconsistente, c, escalar(recibido), escalar(mandado))
		}
	}
	return ""
}

// escalar: un valor JSON escalar como texto ("9001" y 9001 son iguales).
func escalar(v []byte) string {
	t := string(v)
	if u, err := strconv.Unquote(t); err == nil {
		return u
	}
	return t
}

// TipoEnCurso: 409 de Argos mientras procesa la misma Idempotency-Key (se reintenta).
const TipoEnCurso = "idempotencia_en_curso"

// TipoServicioNoPendiente: 409 de activar-instalacion sobre un servicio que ya no está
// pendiente_instalacion (contrato 0.3.1 §3.1; antes 422). De negocio: la orden queda «cerrada con
// pendiente en Argos» y se avisa al supervisor (integracion.pendiente_fallido).
const TipoServicioNoPendiente = "servicio_no_pendiente"

// EsNegocio: un 4xx que reintentar no arregla (datos, estado del servicio, existencias,
// user_ppp_duplicado, 409 servicio_no_pendiente, 422 idempotency_key_reutilizada). No lo son
// 401/403 (credencial o permiso del token: se arregla en Argos y el reintento pasa), 408, 425,
// 429 ni el 409 idempotencia_en_curso (Argos procesando la misma Idempotency-Key). Contrato
// 0.3.1 §3.1: de los 409, solo idempotencia_en_curso se reintenta; cualquier otro (con otro
// type, sin type o con about:blank) es de negocio.
func EsNegocio(er *conector.ErrorRemoto) bool {
	if er.Bloqueado() || er.Status < 400 || er.Status >= 500 {
		return false
	}
	switch er.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout,
		http.StatusTooEarly, http.StatusTooManyRequests:
		return false
	case http.StatusConflict:
		return TipoDe(er) != TipoEnCurso
	}
	return true
}

// TipoDe es el type del problem+json de Argos sin prefijo (acepta «x», «urn:…:x» o una URL
// …/x).
func TipoDe(er *conector.ErrorRemoto) string {
	t := strings.TrimSpace(er.Tipo)
	if t == "about:blank" {
		return "" // RFC 9457: sin tipo propio
	}
	if i := strings.LastIndexAny(t, ":/"); i >= 0 {
		t = t[i+1:]
	}
	return t
}

var credencial = regexp.MustCompile(`(?i)(atk_|iris_pat_|hf_pat_|bearer\s+)[A-Za-z0-9._~+/=-]+`)

// sinCredenciales quita tokens que el otro sistema pudiera repetir en su detail.
func sinCredenciales(s string) string {
	s = credencial.ReplaceAllString(s, "${1}***")
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

// describir resume el error para ultimo_error: HTTP, type y el detail de Argos (sin
// credenciales, a lo más 500 caracteres). Es para el supervisor (contrato §3).
func describir(er *conector.ErrorRemoto) string {
	switch {
	case er.Bloqueado():
		return "destino_no_permitido: la URL de Argos resuelve a una red interna (IRIS_ARGOS_REDES_PERMITIDAS)"
	case er.Status == 0:
		return "Argos no respondió: " + sinCredenciales(er.Error())
	}
	partes := []string{fmt.Sprintf("HTTP %d", er.Status)}
	if er.Tipo != "" {
		partes = append(partes, er.Tipo)
	}
	msg := strings.Join(partes, " ")
	if er.Detalle != "" {
		msg += ": " + er.Detalle
	} else if er.Titulo != "" {
		msg += ": " + er.Titulo
	}
	return sinCredenciales(msg)
}

func resumenProblema(err error) string {
	p, interno := nucleo.AProblema(err)
	if interno {
		return "error interno"
	}
	return p.Codigo()
}

type eventoPendiente struct {
	PendienteID    int64  `json:"pendiente_id"`
	Operacion      string `json:"operacion"`
	Entidad        string `json:"entidad"`
	EntidadID      string `json:"entidad_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type eventoFallo struct {
	eventoPendiente
	Intentos int32  `json:"intentos"`
	Status   *int32 `json:"status"`
	Detalle  string `json:"detalle"`
}

func eventoDe(f pendientesbd.IntegracionPendiente) eventoPendiente {
	return eventoPendiente{PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID, IdempotencyKey: f.IdempotencyKey}
}

// nuevoResolver guarda el resultado de un intento: hecho, fallido_negocio, reintento con la
// espera del contrato o agotado. Corre como sistema (worker).
func nuevoResolver(m *Modulo) *auditoria.Caso[resultadoIntento, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[resultadoIntento, Pendiente]{
		Accion: "integraciones.procesar_pendiente", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in resultadoIntento) (Pendiente, error) {
			q := pendientesbd.New(t)
			// Paso del titular: la orden (FOR SHARE) antes que el pendiente, en el mismo orden que campo
			// (orden → pendiente): sin interbloqueo con cerrar o cancelar la orden.
			// B3: si hay un cargo en juego (el propio, o uno que depende de este y se resolverá aquí), la orden
			// FOR UPDATE antes que el pendiente (como el cierre y A36), en vez de FOR SHARE: sin subir el
			// candado en medio de la transacción.
			previo, err := q.PendientePorID(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			cargo := false
			if previo.Entidad == "orden" {
				cargo = previo.Operacion == OpCargo
				if !cargo {
					if cargo, err = q.HayCargoDependiente(ctx, previo.ID); err != nil {
						return Pendiente{}, err
					}
				}
			}
			if cargo {
				if err := bloquearOrden(ctx, q, previo); err != nil {
					return Pendiente{}, err
				}
			} else if orden, ok := ordenDePasoTitular(previo); ok {
				if _, err := q.EstadoDeOrdenCompartido(ctx, orden); err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Pendiente{}, err
				}
			}
			antes, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			t.FijarEntidad("pendiente", antes.ID)
			if antes.EmpresaID != nil {
				t.FijarEmpresa(*antes.EmpresaID)
			}
			// Paso del titular de una orden cancelada o fallida: si no salió bien, queda detenido (no
			// vuelve a la cola: un 503 lo reintentaría con la orden ya terminada).
			if !in.Exito && antes.Estado == EstadoEnCurso {
				como, err := ordenTerminada(ctx, q, antes)
				if err != nil {
					return Pendiente{}, err
				}
				if como != "" {
					return detenerEnCurso(ctx, t, antes, como, in.ahora)
				}
			}
			p := pendientesbd.ResolverPendienteParams{ID: in.ID, ProximoIntento: in.ahora, UltimoStatus: in.Status}
			evento, payload := "", any(nil)
			intentos := antes.Intentos + 1
			switch {
			case in.Exito:
				p.Estado, p.HechoEn, p.Respuesta = EstadoHecho, &in.ahora, in.respuesta
				evento, payload = EventoHecho, eventoDe(antes)
				if in.Conciliado != "" {
					// 409 que dice «ya hecho»: queda hecho y el motivo en ultimo_error, para revisarlo.
					nota := "conciliado: " + in.Conciliado + " (Argos ya lo tenía hecho)"
					p.UltimoError, p.CodigoError, p.UltimoStatus = &nota, &in.Conciliado, in.Status
					t.Anotar(map[string]any{"conciliado": in.Conciliado})
				}
			case in.Negocio:
				// Con respuesta (inconsistente) se guarda para revisarla; los dependientes no la usan.
				p.Estado, p.UltimoError, p.Respuesta = EstadoFallidoNegocio, &in.Error, in.respuesta
				p.CodigoError = texto(in.Codigo)
				evento, payload = EventoFallido, eventoFallo{eventoPendiente: eventoDe(antes), Intentos: intentos, Status: in.Status, Detalle: in.Error}
			case int(intentos) <= len(Esperas):
				p.Estado, p.UltimoError, p.CodigoError = EstadoPendiente, &in.Error, texto(in.Codigo)
				p.ProximoIntento = in.ahora.Add(Esperas[intentos-1])
			default:
				p.Estado, p.UltimoError, p.CodigoError = EstadoAgotado, &in.Error, texto(in.Codigo)
				evento, payload = EventoAgotado, eventoFallo{eventoPendiente: eventoDe(antes), Intentos: intentos, Status: in.Status, Detalle: in.Error}
			}
			f, err := q.ResolverPendiente(ctx, p)
			if errors.Is(err, pgx.ErrNoRows) {
				// Ya no estaba en curso (otro worker lo reclamó): no se pisa.
				return aPendiente(antes), nil
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": antes.Estado, "intentos": antes.Intentos},
				map[string]any{"estado": f.Estado, "intentos": f.Intentos})
			if in.sinRespuesta && !in.Exito {
				if err := q.MarcarEnvioSinRespuesta(ctx, f.ID); err != nil {
					return Pendiente{}, err
				}
			}
			var reemplazo *pendientesbd.IntegracionPendiente
			if f.Estado == EstadoFallidoNegocio && f.Operacion == OpCargo && deref(f.CodigoError) == CodigoCapacidadNoDisponible {
				if f, reemplazo, err = m.cargoSinCapacidad(ctx, t, antes, f, in.conceptoInstalacion, in.ahora); err != nil {
					return Pendiente{}, err
				}
			}
			if f.Estado == EstadoFallidoNegocio && f.Operacion == OpAprovisionar && deref(f.CodigoError) == CodigoCapacidadNoDisponible {
				// §4.3: sin la marca wifi, Argos rechaza el campo wifi; se reenvía sin él, con otra clave.
				if f, reemplazo, err = aprovisionarSinWifi(ctx, t, f, in.ahora); err != nil {
					return Pendiente{}, err
				}
			}
			if evento != "" {
				if err := t.Emitir(evento, payload); err != nil {
					return Pendiente{}, err
				}
			}
			switch f.Estado {
			case EstadoHecho:
				// Lo sensible del paso del titular se borra al recibir el 2xx (§3.4.8).
				if err := borrarTransito(ctx, t, f, "entregado", in.ahora); err != nil {
					return Pendiente{}, err
				}
				if err := despuesDeHecho(ctx, t, f, in.ahora); err != nil {
					return Pendiente{}, err
				}
				// Primero los cuerpos con marcadores (con la respuesta recién guardada), una vez.
				if err := resolverDependientes(ctx, t, f.ID, in.ahora); err != nil {
					return Pendiente{}, err
				}
				if err := m.desbloquearDependientes(ctx, t, f.ID, in.ahora); err != nil {
					return Pendiente{}, err
				}
			case EstadoFallidoNegocio, EstadoAgotado:
				if err := bloquearDependientes(ctx, t, f.ID, f.Estado); err != nil {
					return Pendiente{}, err
				}
				// La clave Wi-Fi rechazada por Argos se descarta ya (§12.1, «Secretos»).
				if err := descartarClave(ctx, t, f, in.ahora); err != nil {
					return Pendiente{}, err
				}
			}
			if err := despuesDelPasoTitular(ctx, t, f, in.ahora); err != nil {
				return Pendiente{}, err
			}
			if (deref(f.CodigoError) == CodigoBodegaRequerida || deref(f.CodigoError) == "capacidad_no_disponible") && m.cfg.Argos != nil {
				// Argos volvió a 1.5.x: se relee su versión ya (no al próximo periodo), para que lo
				// que se encole desde ahora salga con bodega_id.
				if err := m.cfg.Argos.RevisarContratoYa(ctx, t.Tx); err != nil {
					return Pendiente{}, err
				}
			}
			if f.Estado == EstadoFallidoNegocio || f.Estado == EstadoAgotado {
				slog.WarnContext(ctx, "pendientes: acción hacia Argos sin completar", "pendiente", f.ID,
					"operacion", f.Operacion, "estado", f.Estado, "error", in.Error)
			}
			if reemplazo != nil {
				if err := m.avisarCola(ctx, t.Tx); err != nil {
					return Pendiente{}, err
				}
			}
			return aPendiente(f), nil
		},
	})
}

// Jobs de River ----------------------------------------------------------------------------

// ArgsProcesar procesa todo lo vencido de la cola.
type ArgsProcesar struct{}

// Kind implementa river.JobArgs.
func (ArgsProcesar) Kind() string { return "integracion.pendientes_procesar" }

// InsertOpts: cola de integración, único por ventana de 10 s (el periódico y los avisos al
// encolar no se amontonan).
func (ArgsProcesar) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 10 * time.Second}}
}

type trabajador struct {
	river.WorkerDefaults[ArgsProcesar]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajador) Work(ctx context.Context, _ *river.Job[ArgsProcesar]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalWorker), w.bd)
	_, err := w.m.Procesar(ctx)
	return err
}

func (w *trabajador) Timeout(*river.Job[ArgsProcesar]) time.Duration { return 10 * time.Minute }

// RegistrarWorkers agrega el worker de la cola al cliente de River.
func (m *Modulo) RegistrarWorkers(workers *river.Workers, bd nucleo.BD) {
	river.AddWorker(workers, &trabajador{m: m, bd: bd})
	river.AddWorker(workers, &trabajadorPurga{m: m, bd: bd})
}

// TrabajoPeriodico procesa la cola cada intervalo (1 min por defecto: la espera más corta del
// contrato).
func TrabajoPeriodico(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsProcesar{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
