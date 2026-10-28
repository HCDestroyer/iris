package adjuntos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	adjuntosbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Núcleo del protocolo tus 1.0.0 (https://tus.io/protocols/resumable-upload): el recurso de
// subida ya existe (lo crea el alta, POST /api/v1/adjuntos), así que no se implementa la
// extensión creation. HEAD devuelve Upload-Offset y Upload-Length; PATCH agrega bytes desde
// Upload-Offset con Content-Type application/offset+octet-stream. Lo que llega se guarda de a
// segmentos: si la conexión se corta, lo ya guardado cuenta y el cliente retoma con HEAD.

const (
	// VersionTus es la única versión soportada.
	VersionTus = "1.0.0"
	// TipoParte es el Content-Type obligatorio del PATCH.
	TipoParte = "application/offset+octet-stream"
)

// cabecerasTus van en toda respuesta de la subida.
func cabecerasTus(w http.ResponseWriter) {
	w.Header().Set("Tus-Resumable", VersionTus)
	w.Header().Set("Cache-Control", "no-store")
}

func exigirVersion(r *http.Request) *nucleo.Problema {
	if v := r.Header.Get("Tus-Resumable"); v != VersionTus {
		return nucleo.NuevoProblema(http.StatusPreconditionFailed, "tus_version", "Versión de tus no soportada",
			"Envía Tus-Resumable: "+VersionTus).ConCabecera("Tus-Version", VersionTus)
	}
	return nil
}

func idDeRuta(s string) (uuid.UUID, *nucleo.Problema) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, nucleo.NoEncontrado("")
	}
	return id, nil
}

// responderError escribe el problema (los errores internos van al log y salen como 500).
func responderError(ctx context.Context, w http.ResponseWriter, err error) {
	p, interno := nucleo.AProblema(err)
	if interno {
		slog.ErrorContext(ctx, "adjuntos: error en la subida", "err", err, "request_id", contexto.RequestIDDe(ctx))
	}
	nucleo.EscribirProblema(w, p)
}

// Consultar (HEAD): offset y largo de la subida.
func (m *Modulo) Consultar(ctx context.Context, w http.ResponseWriter, r *http.Request, idTexto string) {
	cabecerasTus(w)
	if p := exigirVersion(r); p != nil {
		nucleo.EscribirProblema(w, p)
		return
	}
	id, p := idDeRuta(idTexto)
	if p != nil {
		nucleo.EscribirProblema(w, p)
		return
	}
	a, err := m.consultar.Ejecutar(ctx, entradaConsultar{ID: id})
	if err != nil {
		responderError(ctx, w, err)
		return
	}
	if a.Estado == EstadoRechazado {
		nucleo.EscribirProblema(w, AdjuntoRechazado(a.MotivoRechazo))
		return
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(a.Recibido, 10))
	w.Header().Set("Upload-Length", strconv.FormatInt(a.Tamano, 10))
	w.WriteHeader(http.StatusOK)
}

// Subir (PATCH): agrega bytes desde Upload-Offset. Responde 204 con el nuevo Upload-Offset. Al
// llegar al tamaño declarado verifica y completa (o rechaza: 422 sha256_distinto /
// mime_distinto). Un PATCH vacío con Upload-Offset = tamaño reintenta el cierre si quedó a
// medias (p. ej. el almacenamiento no respondió).
func (m *Modulo) Subir(ctx context.Context, w http.ResponseWriter, r *http.Request, idTexto string) {
	cabecerasTus(w)
	if p := exigirVersion(r); p != nil {
		nucleo.EscribirProblema(w, p)
		return
	}
	id, p := idDeRuta(idTexto)
	if p != nil {
		nucleo.EscribirProblema(w, p)
		return
	}
	if normalizarMIME(r.Header.Get("Content-Type")) != TipoParte {
		nucleo.EscribirProblema(w, nucleo.NuevoProblema(http.StatusUnsupportedMediaType, "tipo_contenido",
			"Content-Type inválido", "El PATCH de tus lleva Content-Type: "+TipoParte))
		return
	}
	desde, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || desde < 0 {
		nucleo.EscribirProblema(w, nucleo.SolicitudInvalida("Falta Upload-Offset o no es un entero no negativo"))
		return
	}
	a, err := m.prepararSubida.Ejecutar(ctx, entradaPreparar{ID: id, Desde: desde})
	if err != nil {
		responderError(ctx, w, err)
		return
	}
	if a.Estado == EstadoCompleto {
		w.Header().Set("Upload-Offset", strconv.FormatInt(a.Tamano, 10))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	restante := a.Tamano - desde
	if r.ContentLength > restante {
		m.rechazarYResponder(ctx, w, a, MotivoTamanoExcedido,
			TamanoExcedido(fmt.Sprintf("El adjunto declara %d bytes y esta parte llega hasta %d", a.Tamano, desde+r.ContentLength)))
		return
	}
	// Una foto por una red lenta tarda más que el ReadTimeout del servidor.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(m.cfg.TiempoSubida))
	_ = rc.SetWriteDeadline(time.Now().Add(m.cfg.TiempoSubida + time.Minute))

	recibido, exceso, err := m.recibir(ctx, id, desde, restante, r.Body)
	if exceso {
		m.rechazarYResponder(ctx, w, a, MotivoTamanoExcedido,
			TamanoExcedido(fmt.Sprintf("Llegaron más de los %d bytes declarados", a.Tamano)))
		return
	}
	if err != nil {
		var pr *nucleo.Problema
		if errors.As(err, &pr) {
			nucleo.EscribirProblema(w, pr)
			return
		}
		// Corte de la conexión a mitad de la parte: lo guardado cuenta. Si el cliente sigue ahí,
		// le decimos hasta dónde llegamos.
		slog.InfoContext(ctx, "adjuntos: parte interrumpida", "adjunto", id, "recibido", recibido, "err", err)
	}
	a.Recibido = recibido
	if recibido == a.Tamano {
		if p := m.finalizar(ctx, a); p != nil {
			nucleo.EscribirProblema(w, p)
			return
		}
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(recibido, 10))
	w.WriteHeader(http.StatusNoContent)
}

func (m *Modulo) rechazarYResponder(ctx context.Context, w http.ResponseWriter, a Adjunto, motivo string, p *nucleo.Problema) {
	if _, err := m.rechazar.Ejecutar(ctx, entradaRechazar{ID: a.ID, Motivo: motivo}); err != nil {
		responderError(ctx, w, err)
		return
	}
	nucleo.EscribirProblema(w, p)
}

// recibir lee el cuerpo de a segmentos y guarda cada uno en su propia transacción corta
// (avanza recibido solo si sigue en desde: dos PATCH a la vez no se mezclan). Devuelve hasta
// dónde quedó y si llegaron bytes de más que los restante declarados.
func (m *Modulo) recibir(ctx context.Context, id uuid.UUID, desde, restante int64, cuerpo io.Reader) (int64, bool, error) {
	buf := make([]byte, m.cfg.Segmento)
	actual := desde
	limite := desde + restante
	cuerpo = io.LimitReader(cuerpo, restante+1)
	for {
		n, errLeer := io.ReadFull(cuerpo, buf)
		if n > 0 {
			if actual+int64(n) > limite {
				return actual, true, nil
			}
			if err := m.guardarSegmento(ctx, id, actual, buf[:n]); err != nil {
				return actual, false, err
			}
			actual += int64(n)
		}
		switch {
		case errLeer == nil:
			continue
		case errors.Is(errLeer, io.EOF), errors.Is(errLeer, io.ErrUnexpectedEOF):
			return actual, false, nil
		default:
			return actual, false, errLeer
		}
	}
}

func (m *Modulo) guardarSegmento(ctx context.Context, id uuid.UUID, desde int64, datos []byte) error {
	// La petición puede cortarse justo ahora: lo que llegó se guarda igual.
	ctx = context.WithoutCancel(ctx)
	return nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := adjuntosbd.New(tx)
		if _, err := q.AvanzarRecibido(ctx, adjuntosbd.AvanzarRecibidoParams{N: int64(len(datos)), ID: id, Desde: desde}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				var recibido int64
				_ = tx.QueryRow(ctx, "SELECT recibido FROM nucleo.adjuntos WHERE id = $1", id).Scan(&recibido)
				return OffsetDistinto(recibido)
			}
			return err
		}
		return q.InsertarParte(ctx, adjuntosbd.InsertarParteParams{AdjuntoID: id, Desde: desde, Datos: datos})
	})
}

// lectorPartes lee las partes guardadas en orden (una a la vez en memoria).
type lectorPartes struct {
	ctx    context.Context
	bd     nucleo.BD
	id     uuid.UUID
	desde  int64
	total  int64
	actual []byte
}

func (l *lectorPartes) Read(p []byte) (int, error) {
	for len(l.actual) == 0 {
		if l.desde >= l.total {
			return 0, io.EOF
		}
		datos, err := adjuntosbd.New(l.bd).ParteDesde(l.ctx, adjuntosbd.ParteDesdeParams{AdjuntoID: l.id, Desde: l.desde})
		if err != nil {
			return 0, fmt.Errorf("adjuntos: parte %d de %s: %w", l.desde, l.id, err)
		}
		if len(datos) == 0 {
			return 0, fmt.Errorf("adjuntos: parte vacía %d de %s", l.desde, l.id)
		}
		l.actual = datos
		l.desde += int64(len(datos))
	}
	n := copy(p, l.actual)
	l.actual = l.actual[n:]
	return n, nil
}

// finalizar verifica tamaño, SHA-256 y contenido, pasa el archivo al almacenamiento y lo marca
// completo. Devuelve el problema a responder (nil = completo).
func (m *Modulo) finalizar(ctx context.Context, a Adjunto) *nucleo.Problema {
	ctx = context.WithoutCancel(ctx)
	bd := nucleo.BDDe(ctx)
	h := sha256.New()
	var cabeza bytes.Buffer
	n, err := io.Copy(io.MultiWriter(h, &primeros{b: &cabeza, max: 512}), &lectorPartes{ctx: ctx, bd: bd, id: a.ID, total: a.Tamano})
	if err != nil {
		slog.ErrorContext(ctx, "adjuntos: leer partes", "adjunto", a.ID, "err", err)
		return nucleo.ErrorInterno()
	}
	motivo := ""
	switch {
	case n != a.Tamano:
		motivo = MotivoTamanoExcedido
	case hex.EncodeToString(h.Sum(nil)) != a.SHA256:
		motivo = MotivoSHA256
	case !ContenidoEs(a.Mime, cabeza.Bytes()):
		motivo = MotivoMIME
	}
	if motivo != "" {
		if _, err := m.rechazar.Ejecutar(ctx, entradaRechazar{ID: a.ID, Motivo: motivo}); err != nil {
			p, _ := nucleo.AProblema(err)
			return p
		}
		detalle := map[string]string{
			MotivoSHA256:         "El SHA-256 del archivo recibido no coincide con el declarado",
			MotivoMIME:           "El contenido no es del tipo declarado (" + a.Mime + ")",
			MotivoTamanoExcedido: "El archivo recibido no tiene el tamaño declarado",
		}[motivo]
		return nucleo.ReglaNegocio(motivo, detalle+"; el adjunto quedó rechazado")
	}
	if m.cfg.Almacen == nil {
		return nucleo.NoDisponible("No hay almacenamiento de adjuntos configurado")
	}
	lector := &lectorPartes{ctx: ctx, bd: bd, id: a.ID, total: a.Tamano}
	if err := m.cfg.Almacen.Guardar(ctx, a.clave, lector, a.Tamano, a.Mime, a.SHA256); err != nil {
		slog.ErrorContext(ctx, "adjuntos: guardar en el almacenamiento", "adjunto", a.ID, "almacen", m.cfg.Almacen.Nombre(), "err", err)
		return nucleo.NuevoProblema(http.StatusServiceUnavailable, "almacen_no_disponible", "Almacenamiento no disponible",
			"No se pudo guardar el adjunto; repite el último PATCH (Upload-Offset = tamaño, sin cuerpo)").
			ConCabecera("Retry-After", "30")
	}
	if _, err := m.completar.Ejecutar(ctx, entradaCompletar{ID: a.ID}); err != nil {
		p, interno := nucleo.AProblema(err)
		if interno {
			slog.ErrorContext(ctx, "adjuntos: completar", "adjunto", a.ID, "err", err)
		}
		return p
	}
	return nil
}

// primeros guarda los primeros max bytes que pasan (para reconocer el tipo).
type primeros struct {
	b   *bytes.Buffer
	max int
}

func (p *primeros) Write(d []byte) (int, error) {
	if falta := p.max - p.b.Len(); falta > 0 {
		p.b.Write(d[:min(falta, len(d))])
	}
	return len(d), nil
}

// ContenidoEs dice si los primeros bytes corresponden al MIME declarado: imágenes y PDF por su
// firma (http.DetectContentType); SVG debe ser XML con <svg y nunca HTML; cualquier otro tipo
// configurado se acepta salvo que el contenido parezca HTML.
func ContenidoEs(mime string, cabeza []byte) bool {
	detectado := normalizarMIME(http.DetectContentType(cabeza))
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif", "application/pdf":
		return detectado == mime
	case "image/svg+xml":
		return (detectado == "text/xml" || detectado == "text/plain") &&
			strings.Contains(strings.ToLower(string(cabeza)), "<svg")
	}
	return detectado != "text/html"
}

// esImagenSegura: imágenes de mapa de bits que el navegador puede mostrar en línea (su firma ya
// se verificó al completar). SVG no: puede llevar scripts.
func esImagenSegura(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return true
	}
	return false
}

// extension para el nombre de la descarga.
func extension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	}
	return ".bin"
}
