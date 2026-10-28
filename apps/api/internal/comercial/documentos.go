package comercial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Entidades adjuntables de comercial (los PDF de la cotización y del contrato).
const (
	EntidadCotizacion = "cotizacion"
	EntidadContrato   = "contrato"
	// EntidadPendientes: entidad de los pendientes del alta en integracion.pendientes.
	EntidadPendientes = "prospecto"
)

// PDF es un documento generado en el servidor para guardar como adjunto.
type PDF struct {
	ID        uuid.UUID // id del adjunto (fijado antes, al crear la cotización o el contrato)
	Entidad   string    // cotizacion | contrato
	EntidadID string
	Contenido []byte
}

// SHA256 del contenido en hex.
func (p PDF) SHA256() string {
	s := sha256.Sum256(p.Contenido)
	return hex.EncodeToString(s[:])
}

// Documentos guarda un PDF generado en el servidor como adjunto del núcleo (tipo documento,
// application/pdf). Idempotente por el id: si ya está completo con el mismo contenido, no hace
// nada. Se llama FUERA de toda transacción.
type Documentos interface {
	Guardar(ctx context.Context, pdf PDF) error
}

// DocumentosDeAdjuntos guarda los PDF con el módulo de adjuntos del núcleo, por su API pública:
// alta (adjuntos.Registrar) y subida tus en el mismo proceso (adjuntos.Subir), como actor
// sistema. Así el PDF queda verificado (tamaño, SHA-256 y firma %PDF), en el almacenamiento y
// auditado (adjunto.registrado, adjunto.completado). Pedido a I1 (i2.md): una función
// adjuntos.GuardarGenerado que lo haga sin pasar por tus.
func DocumentosDeAdjuntos(adj *adjuntos.Modulo) Documentos { return &docsAdjuntos{adj: adj} }

type docsAdjuntos struct{ adj *adjuntos.Modulo }

// respuesta mínima para el handler de subida en proceso.
type respuesta struct {
	cabeceras http.Header
	status    int
	cuerpo    bytes.Buffer
}

func (r *respuesta) Header() http.Header         { return r.cabeceras }
func (r *respuesta) Write(b []byte) (int, error) { return r.cuerpo.Write(b) }
func (r *respuesta) WriteHeader(s int)           { r.status = s }

func (d *docsAdjuntos) Guardar(ctx context.Context, pdf PDF) error {
	if nucleo.TxDe(ctx) != nil {
		return errors.New("comercial: Documentos.Guardar se llama fuera de la transacción")
	}
	sctx := contexto.Sistema(ctx, contexto.CanalDe(ctx))
	reg, err := d.adj.Registrar.Ejecutar(sctx, adjuntos.EntradaRegistrar{
		ID: pdf.ID, Entidad: pdf.Entidad, EntidadID: pdf.EntidadID, Tipo: "documento", Mime: "application/pdf",
		Tamano: int64(len(pdf.Contenido)), SHA256: pdf.SHA256(),
	})
	if err != nil {
		return err
	}
	if reg.Adjunto.Estado == adjuntos.EstadoCompleto {
		return nil
	}
	desde := reg.Adjunto.Recibido
	req, err := http.NewRequestWithContext(sctx, http.MethodPatch, adjuntos.RutaSubida(pdf.ID), bytes.NewReader(pdf.Contenido[desde:]))
	if err != nil {
		return err
	}
	req.Header.Set("Tus-Resumable", adjuntos.VersionTus)
	req.Header.Set("Content-Type", adjuntos.TipoParte)
	req.Header.Set("Upload-Offset", strconv.FormatInt(desde, 10))
	req.ContentLength = int64(len(pdf.Contenido)) - desde
	w := &respuesta{cabeceras: http.Header{}, status: http.StatusOK}
	d.adj.Subir(sctx, w, req, pdf.ID.String())
	if w.status != http.StatusNoContent {
		return fmt.Errorf("comercial: guardar el PDF %s: HTTP %d %s", pdf.ID, w.status, strings.TrimSpace(w.cuerpo.String()))
	}
	return nil
}

var registroAdjuntos sync.Once

// RegistrarAdjuntos registra la cotización y el contrato como entidades adjuntables (cmd/iris y
// cmd/worker; idempotente). Ver = el prospecto es visible (prospectos.ver con alcance); subir =
// solo el sistema (los PDF los genera el servidor; nadie adjunta otro documento).
func RegistrarAdjuntos() {
	registroAdjuntos.Do(func() {
		adjuntos.RegistrarEntidadAdjuntable(EntidadCotizacion, func(ctx context.Context, q adjuntos.DBTX, id string, uso adjuntos.Uso) (int64, error) {
			return accesoDocumento(ctx, q, EntidadCotizacion, id, uso)
		})
		adjuntos.RegistrarEntidadAdjuntable(EntidadContrato, func(ctx context.Context, q adjuntos.DBTX, id string, uso adjuntos.Uso) (int64, error) {
			return accesoDocumento(ctx, q, EntidadContrato, id, uso)
		})
	})
}

func accesoDocumento(ctx context.Context, db adjuntos.DBTX, entidad, idTexto string, uso adjuntos.Uso) (int64, error) {
	id, err := strconv.ParseInt(idTexto, 10, 64)
	if err != nil || id <= 0 {
		return 0, nucleo.NoEncontrado("")
	}
	q := comercialbd.New(db)
	var prospecto int64
	switch entidad {
	case EntidadCotizacion:
		c, err := q.CotizacionPorID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nucleo.NoEncontrado("")
		}
		if err != nil {
			return 0, err
		}
		prospecto = c.ProspectoID
	default:
		c, err := q.ContratoPorID(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nucleo.NoEncontrado("")
		}
		if err != nil {
			return 0, err
		}
		prospecto = c.ProspectoID
	}
	p, err := q.ProspectoPorID(ctx, prospecto)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nucleo.NoEncontrado("")
	}
	if err != nil {
		return 0, err
	}
	if uso == adjuntos.UsoSubir {
		if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
			return 0, nucleo.NoEncontrado("")
		}
		return deref(p.EmpresaID), nil
	}
	if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
		if err := verificarAcceso(ctx, "prospectos.ver", p); err != nil {
			return 0, nucleo.NoEncontrado("")
		}
	}
	return deref(p.EmpresaID), nil
}

func idTexto(id int64) string { return strconv.FormatInt(id, 10) }

func jsonDe(v any) ([]byte, error) { return json.Marshal(v) }

// guardarPDF guarda el documento si hay Documentos; un error queda en el log y el documento
// sale «no_generado» (se vuelve a pedir con POST …/pdf).
func (m *Modulo) guardarPDF(ctx context.Context, pdf PDF) error {
	if m.cfg.Documentos == nil {
		return nil
	}
	return m.cfg.Documentos.Guardar(ctx, pdf)
}
