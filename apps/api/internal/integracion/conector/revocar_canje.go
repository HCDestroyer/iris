package conector

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Revocación del token del otro sistema cuando Iris no completa un canje que el otro respondió con
// 200 (contrato Iris ↔ ArgosISP 0.6.3 §1.3, «Canje rechazado o incompleto»): Iris recibió el atk_
// pero rechazó el canje (422 webhook_invalido, conector_id inválido, error al guardar, pánico).
// Iris llama a POST <base>/api/v1<Sistema.RutaRevocarCanje> con ese token como Bearer y sin cuerpo,
// en segundo plano:
//
//   - el job se encola fuera de la transacción del canje (que se deshizo), en una propia, desde el
//     mismo defer que revoca el iris_pat_ entregado;
//   - el token va cifrado en los argumentos del job con la clave de los conectores, atado al id de
//     conector reservado para ese canje y a la base a la que se manda (DatosTokenCanje): nunca en
//     claro en river.river_job ni en el log, y si alguien cambia la base en el job no descifra (el
//     job se cancela);
//   - único por sistema y conector (cada canje reserva su id);
//   - 204 o 401 (el token ya no autentica) terminan; 403 u otro 4xx quedan cancelados en el log,
//     sin reintento; 5xx, 429 y timeouts (sin respuesta) se reintentan, con un máximo de
//     IntentosRevocarCanje: el 429 respeta Retry-After si viene (hasta MaxRetryAfter) y si no, como
//     los demás, la espera de River.
//
// Iris no repite el canje para recuperar un token perdido en un timeout: el atk_ de un canje que
// nadie usa vence solo a las 24 h (0.6.3, «Canje sin uso»).

// DatosTokenCanje son los datos asociados del token del job: el conector y la base del canje.
func DatosTokenCanje(id int64, base string) []byte {
	return append(DatosAsociados(id, CampoTokenCanje+":"), base...)
}

// CampoTokenCanje es el dato asociado del token de un canje no completado (job de revocación).
const CampoTokenCanje = "token_canje_revocar" //nolint:gosec // nombre del dato asociado, no una credencial

// IntentosRevocarCanje: intentos del job (la espera de River crece como intento⁴ segundos: ~4 h en
// total, dentro de las 24 h en que el atk_ vence solo).
const IntentosRevocarCanje = 10

// MaxRetryAfter: el Retry-After de un 429 más largo se recorta a esto.
const MaxRetryAfter = 6 * time.Hour

// ArgsRevocarCanje revoca en el otro sistema el token de un canje que Iris no completó.
type ArgsRevocarCanje struct {
	Sistema    string `json:"sistema" river:"unique"`
	ConectorID int64  `json:"conector_id" river:"unique"`
	// BaseURL: la base del canje (el <base> del enlace o api= del botón), ya validada.
	BaseURL string `json:"base_url"`
	// TokenCifrado: el token del canje cifrado, atado a ConectorID y BaseURL (DatosTokenCanje).
	TokenCifrado []byte `json:"token_cifrado"`
}

// Kind implementa river.JobArgs.
func (ArgsRevocarCanje) Kind() string { return "integracion.revocar_canje_remoto" }

// InsertOpts: único por sistema y conector; reintentos acotados.
func (ArgsRevocarCanje) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: integracion.ColaIntegracion, MaxAttempts: IntentosRevocarCanje, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// encolarRevocarCanje encola la revocación del token de un canje que respondió 200 y no se completó.
// No hace nada si el sistema no tiene RutaRevocarCanje (Horus), si el canje no respondió 200 o si no
// trajo un token del sistema. Nunca devuelve error: lo peor queda en el log (el token vence solo).
func (c *Conexion) encolarRevocarCanje(ctx context.Context, g entradaGuardarConector) {
	if c.s.RutaRevocarCanje == "" || g.errRemoto != nil {
		return
	}
	token := strings.TrimSpace(g.respuesta.Token)
	if !strings.HasPrefix(token, c.s.PrefijoToken) || len(token) <= len(c.s.PrefijoToken) {
		return
	}
	attrs := []any{"sistema", c.s.Nombre, "conector_id", g.ConectorID}
	if c.n.cfg.Cifrador == nil {
		slog.ErrorContext(ctx, "conector: sin IRIS_CLAVE_CIFRADO para revocar el token del canje no completado; vencerá solo", attrs...)
		return
	}
	cifrado, err := c.n.cfg.Cifrador.Cifrar([]byte(token), DatosTokenCanje(g.ConectorID, g.Base))
	if err != nil {
		slog.ErrorContext(ctx, "conector: no se pudo cifrar el token del canje no completado; vencerá solo", append(attrs, "err", err)...)
		return
	}
	if c.n.cfg.Encolar == nil {
		slog.ErrorContext(ctx, "conector: sin cola para revocar el token del canje no completado; vencerá solo", attrs...)
		return
	}
	args := ArgsRevocarCanje{Sistema: c.s.Nombre, ConectorID: g.ConectorID, BaseURL: g.Base, TokenCifrado: cifrado}
	if err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return c.n.cfg.Encolar(ctx, tx, args) }); err != nil {
		slog.ErrorContext(ctx, "conector: no se pudo encolar la revocación del token del canje; vencerá solo", append(attrs, "err", err)...)
		return
	}
	slog.WarnContext(ctx, "conector: canje no completado; se revoca el token recibido", attrs...)
}

// RevocarCanje llama a la revocación del otro sistema con el token del canje. nil: revocado (204) o
// ya no autenticaba (401). river.JobCancel: rechazo definitivo (403, otro 4xx, destino bloqueado o
// token ilegible), sin reintento. Otro error: 5xx, 429 o sin respuesta, River reintenta (el 429 con su
// Retry-After, ErrorRemoto.ReintentarEn).
func (n *Nucleo) RevocarCanje(ctx context.Context, a ArgsRevocarCanje) error {
	attrs := []any{"sistema", a.Sistema, "conector_id", a.ConectorID}
	c := n.sistema(a.Sistema)
	if c == nil || c.s.RutaRevocarCanje == "" {
		slog.ErrorContext(ctx, "conector: revocar canje de un sistema sin revocación", attrs...)
		return river.JobCancel(errors.New("conector: el sistema " + a.Sistema + " no revoca canjes"))
	}
	if n.cfg.Cifrador == nil {
		slog.ErrorContext(ctx, "conector: revocar canje sin IRIS_CLAVE_CIFRADO", attrs...)
		return river.JobCancel(SinCifrado())
	}
	token, err := n.cfg.Cifrador.Descifrar(a.TokenCifrado, DatosTokenCanje(a.ConectorID, a.BaseURL))
	if err != nil {
		slog.ErrorContext(ctx, "conector: no se pudo descifrar el token del canje a revocar", append(attrs, "err", err)...)
		return river.JobCancel(err)
	}
	base, err := c.validarBase(a.BaseURL, "base_url")
	if err != nil {
		slog.ErrorContext(ctx, "conector: base del canje a revocar inválida", append(attrs, "err", err)...)
		return river.JobCancel(err)
	}
	err = c.clienteHacia(base, string(token)).Pedir(ctx, http.MethodPost, c.s.RutaRevocarCanje, nil, nil, false, nil)
	var er *ErrorRemoto
	switch {
	case err == nil:
		slog.InfoContext(ctx, "conector: token del canje no completado revocado", attrs...)
		return nil
	case !errors.As(err, &er):
		return err
	case er.Status == http.StatusUnauthorized:
		slog.InfoContext(ctx, "conector: el token del canje no completado ya no autenticaba", attrs...)
		return nil
	case er.Status >= http.StatusInternalServerError, er.Status == http.StatusTooManyRequests, er.Status == 0 && !er.Bloqueado():
		slog.WarnContext(ctx, "conector: revocar el token del canje falló; se reintenta", append(attrs, "status", er.Status)...)
		return err
	default:
		slog.ErrorContext(ctx, "conector: el otro sistema rechazó revocar el token del canje", append(attrs, "status", er.Status, "tipo", er.Tipo)...)
		return river.JobCancel(err)
	}
}

type trabajadorRevocarCanje struct {
	river.WorkerDefaults[ArgsRevocarCanje]
	n *Nucleo
	// esperas: Retry-After del 429 de este intento (id → duración). Solo se guarda si River va a
	// reintentar (entonces llama a NextRetry, que la saca); cada intento borra la del anterior.
	esperas sync.Map
}

func (w *trabajadorRevocarCanje) Work(ctx context.Context, j *river.Job[ArgsRevocarCanje]) error {
	w.esperas.Delete(j.ID)
	err := w.n.RevocarCanje(ctx, j.Args)
	if d := EsperaDe429(err); d > 0 && j.Attempt < j.MaxAttempts {
		w.esperas.Store(j.ID, d)
	}
	return err
}

// NextRetry: tras un 429 con Retry-After, ese plazo; si no, la espera de River (tiempo cero).
func (w *trabajadorRevocarCanje) NextRetry(j *river.Job[ArgsRevocarCanje]) time.Time {
	if d, ok := w.esperas.LoadAndDelete(j.ID); ok {
		return time.Now().Add(d.(time.Duration)) //nolint:forcetypeassert // solo se guardan duraciones
	}
	return time.Time{}
}

// EsperaDe429 es el Retry-After de un 429 del otro sistema (recortado a MaxRetryAfter); 0 si err no es
// un 429 o no lo trajo.
func EsperaDe429(err error) time.Duration {
	var er *ErrorRemoto
	if !errors.As(err, &er) || er.Status != http.StatusTooManyRequests {
		return 0
	}
	return min(er.ReintentarEn(), MaxRetryAfter)
}

// RegistrarRevocarCanje agrega el job de revocación de canjes al worker (lo llama el conector
// Argos, dueño del núcleo).
func (n *Nucleo) RegistrarRevocarCanje(workers *river.Workers) {
	river.AddWorker(workers, &trabajadorRevocarCanje{n: n})
}
