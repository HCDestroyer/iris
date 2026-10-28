// Package nucleo reúne las piezas comunes de la API: errores RFC 9457, transacciones (EnTx),
// paginación por cursor, control de versión, Idempotency-Key y el catálogo de permisos.
package nucleo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TipoContenidoProblema es el media type de RFC 9457.
const TipoContenidoProblema = "application/problem+json"

// ErrorCampo describe un error de un campo de la entrada.
type ErrorCampo struct {
	Field   string `json:"field" doc:"Campo con el error (p. ej. correo, roles[0].rol)"`
	Message string `json:"message" doc:"Mensaje legible"`
	Rule    string `json:"rule,omitempty" doc:"Regla que falló"`
}

// Problema es la respuesta de error de toda la API (RFC 9457) y a la vez un error de Go que
// los casos de uso devuelven. Implementa huma.StatusError.
type Problema struct {
	Type   string       `json:"type" doc:"URI estable del tipo de problema (urn:iris:problema:<codigo>)"`
	Title  string       `json:"title" doc:"Resumen legible del tipo de problema"`
	Status int          `json:"status" doc:"Código HTTP"`
	Detail string       `json:"detail,omitempty" doc:"Explicación de este caso"`
	Errors []ErrorCampo `json:"errors,omitempty" doc:"Errores por campo"`

	cabeceras http.Header
}

// ConCabecera agrega una cabecera HTTP a la respuesta del problema (p. ej. Retry-After).
func (p *Problema) ConCabecera(clave, valor string) *Problema {
	if p.cabeceras == nil {
		p.cabeceras = http.Header{}
	}
	p.cabeceras.Set(clave, valor)
	return p
}

// GetHeaders implementa huma.HeadersError.
func (p *Problema) GetHeaders() http.Header { return p.cabeceras }

// Error implementa error.
func (p *Problema) Error() string {
	if p.Detail != "" {
		return p.Title + ": " + p.Detail
	}
	return p.Title
}

// GetStatus implementa huma.StatusError.
func (p *Problema) GetStatus() int { return p.Status }

// ContentType implementa huma.ContentTypeFilter: los errores salen como problem+json.
func (p *Problema) ContentType(string) string { return TipoContenidoProblema }

// Codigo devuelve el código corto del tipo (lo que sigue a urn:iris:problema:).
func (p *Problema) Codigo() string {
	const prefijo = "urn:iris:problema:"
	if len(p.Type) > len(prefijo) {
		return p.Type[len(prefijo):]
	}
	return p.Type
}

// NuevoProblema arma un problema con el código corto del tipo.
func NuevoProblema(status int, codigo, titulo, detalle string, errores ...ErrorCampo) *Problema {
	return &Problema{
		Type:   "urn:iris:problema:" + codigo,
		Title:  titulo,
		Status: status,
		Detail: detalle,
		Errors: errores,
	}
}

// Constructores de los errores de dominio más comunes.

func SolicitudInvalida(detalle string, errores ...ErrorCampo) *Problema {
	return NuevoProblema(http.StatusBadRequest, "solicitud_invalida", "Solicitud inválida", detalle, errores...)
}

func NoAutenticado(detalle string) *Problema {
	if detalle == "" {
		detalle = "Inicia sesión o envía un token Bearer válido"
	}
	return NuevoProblema(http.StatusUnauthorized, "no_autenticado", "No autenticado", detalle)
}

func CredencialesInvalidas() *Problema {
	return NuevoProblema(http.StatusUnauthorized, "credenciales_invalidas", "Credenciales inválidas",
		"Correo o contraseña incorrectos")
}

func Prohibido(detalle string) *Problema {
	if detalle == "" {
		detalle = "No tienes permiso para esta acción"
	}
	return NuevoProblema(http.StatusForbidden, "prohibido", "Permiso insuficiente", detalle)
}

func NoEncontrado(detalle string) *Problema {
	if detalle == "" {
		detalle = "El recurso no existe"
	}
	return NuevoProblema(http.StatusNotFound, "no_encontrado", "No encontrado", detalle)
}

func Conflicto(codigo, detalle string) *Problema {
	if codigo == "" {
		codigo = "conflicto"
	}
	return NuevoProblema(http.StatusConflict, codigo, "Conflicto", detalle)
}

// ConflictoVersion: la entidad cambió desde que el cliente la leyó (control optimista).
func ConflictoVersion(entidad string, id any) *Problema {
	return NuevoProblema(http.StatusConflict, "conflicto_version", "Conflicto de versión",
		fmt.Sprintf("%s %v fue modificado por otra sesión; recarga y vuelve a intentar", entidad, id))
}

func Validacion(errores ...ErrorCampo) *Problema {
	return NuevoProblema(http.StatusUnprocessableEntity, "validacion", "Datos inválidos",
		"Los datos enviados no son válidos", errores...)
}

// ReglaNegocio: una regla impide la acción (no es un error de formato).
func ReglaNegocio(codigo, detalle string) *Problema {
	if codigo == "" {
		codigo = "regla_negocio"
	}
	return NuevoProblema(http.StatusUnprocessableEntity, codigo, "Regla de negocio", detalle)
}

func ErrorInterno() *Problema {
	return NuevoProblema(http.StatusInternalServerError, "interno", "Error interno",
		"Ocurrió un error inesperado; usa el request id para rastrearlo")
}

// DemasiadosIntentos: 429 con Retry-After (segundos hasta poder reintentar).
func DemasiadosIntentos(codigo, detalle string, reintentar time.Duration) *Problema {
	segundos := int(reintentar.Round(time.Second) / time.Second)
	if segundos < 1 {
		segundos = 1
	}
	return NuevoProblema(http.StatusTooManyRequests, codigo, "Demasiados intentos", detalle).
		ConCabecera("Retry-After", strconv.Itoa(segundos))
}

func NoDisponible(detalle string) *Problema {
	return NuevoProblema(http.StatusServiceUnavailable, "no_disponible", "Servicio no disponible", detalle)
}

// AProblema traduce cualquier error al problema que ve el cliente:
//   - *Problema tal cual;
//   - pgx.ErrNoRows → 404;
//   - Postgres: unique → 409, foreign key / check / not null → 422, serialización → 409;
//   - context.Canceled / DeadlineExceeded → 503;
//   - el resto → 500 genérico (sin filtrar el mensaje interno).
//
// interno indica que el error no era de dominio y conviene registrarlo en el log.
func AProblema(err error) (p *Problema, interno bool) {
	if err == nil {
		return nil, false
	}
	if errors.As(err, &p) {
		return p, false
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return NoEncontrado(""), false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return Conflicto("duplicado", "Ya existe un registro con esos datos ("+pgErr.ConstraintName+")"), false
		case "23503":
			return ReglaNegocio("referencia_invalida", "El registro referido no existe o está en uso ("+pgErr.ConstraintName+")"), false
		case "23514", "23502":
			return ReglaNegocio("restriccion", "Los datos no cumplen una restricción ("+pgErr.ConstraintName+")"), false
		case "40001", "40P01":
			return Conflicto("concurrencia", "Otra operación modificó los mismos datos; vuelve a intentar"), false
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return NoDisponible("La operación se canceló o excedió el tiempo"), true
	}
	return ErrorInterno(), true
}

// EscribirProblema responde un problema RFC 9457 desde un middleware o handler net/http.
func EscribirProblema(w http.ResponseWriter, p *Problema) {
	for clave, valores := range p.cabeceras {
		for _, v := range valores {
			w.Header().Add(clave, v)
		}
	}
	w.Header().Set("Content-Type", TipoContenidoProblema)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
