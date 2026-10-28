package adjuntos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	adjuntosbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos/bd"
)

// Estados de un adjunto.
const (
	EstadoPendiente = "pendiente"
	EstadoSubiendo  = "subiendo"
	EstadoCompleto  = "completo"
	EstadoRechazado = "rechazado"
)

// Eventos que emite el módulo (contratos/eventos.md, «Adjuntos»).
const (
	EventoRegistrado = "adjunto.registrado"
	EventoCompletado = "adjunto.completado"
	EventoRechazado  = "adjunto.rechazado"
)

// Motivos de rechazo.
const (
	MotivoSHA256         = "sha256_distinto"
	MotivoTamanoExcedido = "tamano_excedido"
	MotivoMIME           = "mime_distinto"
)

// Adjunto es la vista pública de un adjunto (sin la clave del almacenamiento).
type Adjunto struct {
	ID             uuid.UUID  `json:"id"`
	Entidad        string     `json:"entidad" doc:"Entidad dueña: orden, ticket…"`
	EntidadID      string     `json:"entidadId"`
	Tipo           string     `json:"tipo" enum:"foto,firma,documento"`
	ClaveChecklist *string    `json:"claveChecklist" nullable:"true" doc:"Ítem del checklist que respalda (o null)"`
	Mime           string     `json:"mime"`
	Tamano         int64      `json:"tamano" doc:"Bytes declarados en el alta"`
	SHA256         string     `json:"sha256" doc:"SHA-256 en hex (minúsculas) declarado en el alta"`
	Recibido       int64      `json:"recibido" doc:"Bytes recibidos (Upload-Offset de tus)"`
	Estado         string     `json:"estado" enum:"pendiente,subiendo,completo,rechazado"`
	MotivoRechazo  *string    `json:"motivoRechazo" nullable:"true" doc:"sha256_distinto, tamano_excedido o mime_distinto"`
	Lat            *float64   `json:"lat" nullable:"true"`
	Lon            *float64   `json:"lon" nullable:"true"`
	TomadoEn       *time.Time `json:"tomadoEn" nullable:"true" doc:"Hora de captura en el teléfono (UTC)"`
	SubidoPor      *int64     `json:"subidoPor" nullable:"true"`
	CreadoEn       time.Time  `json:"creadoEn"`
	CompletadoEn   *time.Time `json:"completadoEn" nullable:"true"`
	URLSubida      string     `json:"urlSubida" doc:"URL de subida tus (HEAD/PATCH), relativa al origen de la API"`
	URLDescarga    string     `json:"urlDescarga" doc:"URL de descarga (solo sirve con estado completo)"`
	Version        int32      `json:"version"`

	empresaID *int64
	clave     string
}

// RutaSubida es la URL tus de un adjunto.
func RutaSubida(id uuid.UUID) string { return "/api/v1/adjuntos/" + id.String() + "/subida" }

// RutaDescarga es la URL de descarga de un adjunto.
func RutaDescarga(id uuid.UUID) string { return "/api/v1/adjuntos/" + id.String() }

func aAdjunto(f adjuntosbd.NucleoAdjunto) Adjunto {
	return Adjunto{
		ID: f.ID, Entidad: f.Entidad, EntidadID: f.EntidadID, Tipo: f.Tipo, ClaveChecklist: f.ClaveChecklist,
		Mime: f.Mime, Tamano: f.Tamano, SHA256: f.Sha256, Recibido: f.Recibido, Estado: f.Estado,
		MotivoRechazo: f.MotivoRechazo, Lat: f.Lat, Lon: f.Lon, TomadoEn: f.TomadoEn, SubidoPor: f.SubidoPor,
		CreadoEn: f.CreatedAt, CompletadoEn: f.CompletadoEn, URLSubida: RutaSubida(f.ID), URLDescarga: RutaDescarga(f.ID),
		Version: f.Version, empresaID: f.EmpresaID, clave: f.Clave,
	}
}

func empresaDe(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// Funciones para otros módulos (I2: campo) ---------------------------------------------------

// Requisito es un adjunto obligatorio de una entidad (p. ej. de la plantilla de checklist de la
// orden): Tipo (foto, firma, documento), ClaveChecklist opcional (el ítem que respalda; vacío =
// cualquiera de ese tipo) y Minimo (0 = 1).
type Requisito struct {
	Tipo           string
	ClaveChecklist string
	Minimo         int
}

// Clave identifica el requisito en la lista de faltantes: "firma", "foto:antena".
func (r Requisito) Clave() string {
	if r.ClaveChecklist == "" {
		return r.Tipo
	}
	return r.Tipo + ":" + r.ClaveChecklist
}

// Faltantes devuelve los requisitos sin suficientes adjuntos completos (subidos y verificados)
// de la entidad, en el orden dado, como Requisito.Clave() ("firma", "foto:antena"). Vacío =
// están todos. Un requisito sin ClaveChecklist cuenta los adjuntos de ese tipo con o sin clave.
// q es la transacción del caso de uso que cierra (no verifica permisos: lo hace quien llama).
func Faltantes(ctx context.Context, q DBTX, entidad, entidadID string, requeridos []Requisito) ([]string, error) {
	if len(requeridos) == 0 {
		return nil, nil
	}
	filas, err := adjuntosbd.New(q).ContarCompletos(ctx, adjuntosbd.ContarCompletosParams{Entidad: entidad, EntidadID: entidadID})
	if err != nil {
		return nil, err
	}
	porTipo := map[string]int64{}
	porClave := map[string]int64{}
	for _, f := range filas {
		porTipo[f.Tipo] += f.Total
		if f.ClaveChecklist != "" {
			porClave[f.Tipo+":"+f.ClaveChecklist] += f.Total
		}
	}
	faltan := []string{}
	for _, r := range requeridos {
		minimo := int64(max(r.Minimo, 1))
		hay := porTipo[r.Tipo]
		if r.ClaveChecklist != "" {
			hay = porClave[r.Clave()]
		}
		if hay < minimo {
			faltan = append(faltan, r.Clave())
		}
	}
	return faltan, nil
}

// DeEntidad devuelve el adjunto id si pertenece a esa entidad (para validar la operación
// adjunto.registrar del lote de sync). nucleo.NoEncontrado si no existe o es de otra entidad.
func DeEntidad(ctx context.Context, q DBTX, id uuid.UUID, entidad, entidadID string) (Adjunto, error) {
	f, err := adjuntosbd.New(q).AdjuntoPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (f.Entidad != entidad || f.EntidadID != entidadID)) {
		return Adjunto{}, nucleo.NoEncontrado(fmt.Sprintf("El adjunto %s no es de %s %s", id, entidad, entidadID))
	}
	if err != nil {
		return Adjunto{}, err
	}
	return aAdjunto(f), nil
}
