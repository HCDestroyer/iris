package adjuntos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	adjuntosbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos/bd"
)

// Alta -----------------------------------------------------------------------------------

// EntradaRegistrar es el alta de un adjunto (POST /api/v1/adjuntos).
type EntradaRegistrar struct {
	ID             uuid.UUID  `json:"id" doc:"UUID (v7) generado en el cliente; el alta es idempotente por él"`
	Entidad        string     `json:"entidad" pattern:"^[a-z][a-z_]{0,39}$" doc:"Entidad dueña registrada por su módulo: orden, ticket…"`
	EntidadID      string     `json:"entidadId" minLength:"1" maxLength:"64"`
	Tipo           string     `json:"tipo" enum:"foto,firma,documento"`
	ClaveChecklist *string    `json:"claveChecklist,omitempty" minLength:"1" maxLength:"80" doc:"Ítem del checklist que respalda"`
	Mime           string     `json:"mime" minLength:"3" maxLength:"100" doc:"Tipo MIME; debe estar en IRIS_ADJUNTOS_MIME"`
	Tamano         int64      `json:"tamano" minimum:"1" doc:"Bytes exactos del archivo (máximo IRIS_ADJUNTOS_TAMANO_MAX)"`
	SHA256         string     `json:"sha256" pattern:"^[0-9a-fA-F]{64}$" doc:"SHA-256 del archivo en hex"`
	Lat            *float64   `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon            *float64   `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	TomadoEn       *time.Time `json:"tomadoEn,omitempty" doc:"Hora de captura (EXIF o del teléfono con su desfase)"`
}

// Registrado es el resultado del alta: Nuevo = false si el id ya existía con los mismos datos.
type Registrado struct {
	Adjunto Adjunto
	Nuevo   bool
}

type eventoAdjunto struct {
	AdjuntoID      uuid.UUID `json:"adjunto_id"`
	Entidad        string    `json:"entidad"`
	EntidadID      string    `json:"entidad_id"`
	Tipo           string    `json:"tipo"`
	ClaveChecklist *string   `json:"clave_checklist"`
}

type eventoCompletado struct {
	eventoAdjunto
	Mime   string `json:"mime"`
	Tamano int64  `json:"tamano"`
	SHA256 string `json:"sha256"`
}

type eventoRechazado struct {
	eventoAdjunto
	Motivo string `json:"motivo"`
}

func eventoDe(a Adjunto) eventoAdjunto {
	return eventoAdjunto{AdjuntoID: a.ID, Entidad: a.Entidad, EntidadID: a.EntidadID, Tipo: a.Tipo, ClaveChecklist: a.ClaveChecklist}
}

// TamanoExcedido es el 413 del alta y de la subida.
func TamanoExcedido(detalle string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusRequestEntityTooLarge, MotivoTamanoExcedido, "Adjunto demasiado grande", detalle)
}

func nuevoRegistrar(m *Modulo) *auditoria.Caso[EntradaRegistrar, Registrado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrar, Registrado]{
		Accion: "adjuntos.registrar", Permiso: nucleo.PermAdjuntosSubir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrar) (Registrado, error) {
			if in.ID == uuid.Nil {
				return Registrado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "id", Message: "UUID vacío", Rule: "uuid"})
			}
			mime := normalizarMIME(in.Mime)
			if !m.mimePermitido(mime) {
				return Registrado{}, nucleo.ReglaNegocio("mime_no_permitido",
					fmt.Sprintf("El tipo %q no está permitido (permitidos: %s)", mime, strings.Join(m.cfg.MIMEPermitidos, ", ")))
			}
			if in.Tamano > m.cfg.TamanoMax {
				return Registrado{}, TamanoExcedido(fmt.Sprintf("El máximo es %d bytes", m.cfg.TamanoMax))
			}
			if m.cfg.Almacen == nil {
				return Registrado{}, nucleo.NoDisponible("No hay almacenamiento de adjuntos configurado")
			}
			empresa, err := verificarEntidad(ctx, t, in.Entidad, in.EntidadID, UsoSubir)
			if err != nil {
				return Registrado{}, err
			}
			sha := strings.ToLower(in.SHA256)
			var empresaID *int64
			if empresa != 0 {
				empresaID = &empresa
			}
			var subidoPor *int64
			if a := t.Actor(); a.EsUsuario() {
				subidoPor = &a.ID
			}
			q := adjuntosbd.New(t)
			if _, err := q.AdjuntoPorID(ctx, in.ID); errors.Is(err, pgx.ErrNoRows) {
				// Alta nueva: cuotas de subidas sin terminar (repetir un alta no cuenta).
				if err := m.verificarCuota(ctx, t, subidoPor, in); err != nil {
					return Registrado{}, err
				}
			} else if err != nil {
				return Registrado{}, err
			}
			fila, err := q.InsertarAdjunto(ctx, adjuntosbd.InsertarAdjuntoParams{
				ID: in.ID, EmpresaID: empresaID, Entidad: in.Entidad, EntidadID: in.EntidadID, Tipo: in.Tipo,
				ClaveChecklist: in.ClaveChecklist, Mime: mime, Tamano: in.Tamano, Sha256: sha,
				Lat: in.Lat, Lon: in.Lon, TomadoEn: utc(in.TomadoEn), SubidoPor: subidoPor,
				Almacenamiento: m.cfg.Almacen.Nombre(), Clave: fmt.Sprintf("e%d/%s/%s", empresa, in.Entidad, in.ID),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				// Ya existía: idempotente si es el mismo adjunto (mismos datos, misma empresa).
				previo, err := q.AdjuntoPorID(ctx, in.ID)
				if err != nil {
					return Registrado{}, err
				}
				if previo.Entidad != in.Entidad || previo.EntidadID != in.EntidadID || previo.Tipo != in.Tipo ||
					previo.Mime != mime || previo.Tamano != in.Tamano || previo.Sha256 != sha ||
					empresaDe(previo.EmpresaID) != empresa || !mismoTexto(previo.ClaveChecklist, in.ClaveChecklist) {
					return Registrado{}, nucleo.Conflicto("adjunto_existente", "Ya existe un adjunto con ese id y otros datos; genera otro id")
				}
				t.FijarEntidad("adjunto", previo.ID)
				t.FijarEmpresa(empresa)
				return Registrado{Adjunto: aAdjunto(previo)}, nil
			}
			if err != nil {
				return Registrado{}, err
			}
			a := aAdjunto(fila)
			t.FijarEntidad("adjunto", a.ID)
			t.FijarEmpresa(empresa)
			t.RegistrarCambios("adjunto", a.ID, nil, a)
			return Registrado{Adjunto: a, Nuevo: true}, t.Emitir(EventoRegistrado, eventoDe(a))
		},
	})
}

// DemasiadasSubidas es el 429 de las cuotas del alta.
func DemasiadasSubidas(detalle string) *nucleo.Problema {
	return nucleo.DemasiadosIntentos("demasiadas_subidas", detalle+"; termina o descarta las subidas pendientes", time.Minute)
}

// verificarCuota: subidas sin terminar por usuario (cantidad y bytes) y por entidad. Un candado
// por usuario (transaccional) evita que dos altas simultáneas pasen la cuota a la vez.
func (m *Modulo) verificarCuota(ctx context.Context, t *auditoria.Tx, subidoPor *int64, in EntradaRegistrar) error {
	q := adjuntosbd.New(t)
	if subidoPor != nil {
		if _, err := t.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('adjuntos:usuario:' || $1::bigint::text, 0))", *subidoPor); err != nil {
			return err
		}
		c, err := q.EnCursoDeUsuario(ctx, subidoPor)
		if err != nil {
			return err
		}
		if c.Total >= int64(m.cfg.MaxEnCursoUsuario) {
			return DemasiadasSubidas(fmt.Sprintf("Tienes %d adjuntos sin terminar de subir (máximo %d)", c.Total, m.cfg.MaxEnCursoUsuario))
		}
		if c.Bytes+in.Tamano > m.cfg.MaxBytesUsuario {
			return DemasiadasSubidas(fmt.Sprintf("Tus subidas sin terminar suman %d bytes (máximo %d)", c.Bytes+in.Tamano, m.cfg.MaxBytesUsuario))
		}
	}
	n, err := q.EnCursoDeEntidad(ctx, adjuntosbd.EnCursoDeEntidadParams{Entidad: in.Entidad, EntidadID: in.EntidadID})
	if err != nil {
		return err
	}
	if n >= int64(m.cfg.MaxEnCursoEntidad) {
		return DemasiadasSubidas(fmt.Sprintf("La %s %s tiene %d adjuntos sin terminar (máximo %d)", in.Entidad, in.EntidadID, n, m.cfg.MaxEnCursoEntidad))
	}
	return nil
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func mismoTexto(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// cargar lee el adjunto y verifica que el actor pueda usar su entidad (404 si no).
func cargar(ctx context.Context, t *auditoria.Tx, id uuid.UUID, uso Uso) (Adjunto, error) {
	f, err := adjuntosbd.New(t).AdjuntoPorID(ctx, id)
	if err != nil {
		return Adjunto{}, err
	}
	if !empresaVisible(ctx, empresaDe(f.EmpresaID)) {
		return Adjunto{}, nucleo.NoEncontrado("")
	}
	empresa, err := verificarEntidad(ctx, t, f.Entidad, f.EntidadID, uso)
	if err != nil {
		return Adjunto{}, err
	}
	if empresa != empresaDe(f.EmpresaID) {
		// La entidad cambió de empresa después del alta: no se mezcla.
		return Adjunto{}, nucleo.NoEncontrado("")
	}
	t.FijarEntidad("adjunto", f.ID)
	t.FijarEmpresa(empresa)
	return aAdjunto(f), nil
}

func sinAuditar[Out any](Out) bool { return false }

// Subida (tus) ----------------------------------------------------------------------------

type entradaConsultar struct {
	ID uuid.UUID
}

func nuevoConsultar(*Modulo) *auditoria.Caso[entradaConsultar, Adjunto] {
	return auditoria.Nuevo(auditoria.Caso[entradaConsultar, Adjunto]{
		Accion: "adjuntos.consultar_subida", Permiso: nucleo.PermAdjuntosSubir, AuditarExito: sinAuditar[Adjunto],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaConsultar) (Adjunto, error) {
			return cargar(ctx, t, in.ID, UsoSubir)
		},
	})
}

type entradaPreparar struct {
	ID    uuid.UUID
	Desde int64
}

// AdjuntoRechazado es el 410 de un adjunto rechazado (hay que registrarlo de nuevo con otro id).
func AdjuntoRechazado(motivo *string) *nucleo.Problema {
	m := ""
	if motivo != nil {
		m = *motivo
	}
	return nucleo.NuevoProblema(http.StatusGone, "adjunto_rechazado", "Adjunto rechazado",
		"El adjunto fue rechazado ("+m+"); regístralo de nuevo con otro id")
}

// OffsetDistinto es el 409 de tus cuando Upload-Offset no es lo recibido.
func OffsetDistinto(recibido int64) *nucleo.Problema {
	return nucleo.Conflicto("offset_distinto", fmt.Sprintf("El servidor tiene %d bytes; consulta con HEAD y sigue desde ahí", recibido)).
		ConCabecera("Upload-Offset", fmt.Sprint(recibido))
}

func nuevoPrepararSubida(*Modulo) *auditoria.Caso[entradaPreparar, Adjunto] {
	return auditoria.Nuevo(auditoria.Caso[entradaPreparar, Adjunto]{
		Accion: "adjuntos.subir", Permiso: nucleo.PermAdjuntosSubir, AuditarExito: sinAuditar[Adjunto],
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaPreparar) (Adjunto, error) {
			a, err := cargar(ctx, t, in.ID, UsoSubir)
			if err != nil {
				return Adjunto{}, err
			}
			switch {
			case a.Estado == EstadoRechazado:
				return Adjunto{}, AdjuntoRechazado(a.MotivoRechazo)
			case a.Estado == EstadoCompleto && in.Desde == a.Tamano:
				return a, nil
			case in.Desde != a.Recibido:
				return Adjunto{}, OffsetDistinto(a.Recibido)
			}
			return a, nil
		},
	})
}

type entradaCompletar struct {
	ID uuid.UUID
}

func nuevoCompletar(*Modulo) *auditoria.Caso[entradaCompletar, Adjunto] {
	return auditoria.Nuevo(auditoria.Caso[entradaCompletar, Adjunto]{
		Accion: "adjuntos.completar", Permiso: nucleo.PermAdjuntosSubir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaCompletar) (Adjunto, error) {
			q := adjuntosbd.New(t)
			antes, err := q.AdjuntoPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Adjunto{}, err
			}
			t.FijarEntidad("adjunto", antes.ID)
			t.FijarEmpresa(empresaDe(antes.EmpresaID))
			if antes.Estado == EstadoCompleto {
				return aAdjunto(antes), nil // otra petición lo completó
			}
			f, err := q.CompletarAdjunto(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Adjunto{}, nucleo.Conflicto("adjunto_incompleto", "El adjunto no está listo para completarse")
			}
			if err != nil {
				return Adjunto{}, err
			}
			if err := q.BorrarPartes(ctx, in.ID); err != nil {
				return Adjunto{}, err
			}
			a := aAdjunto(f)
			t.RegistrarCambios("adjunto", a.ID, map[string]any{"estado": antes.Estado}, map[string]any{"estado": a.Estado})
			return a, t.Emitir(EventoCompletado, eventoCompletado{eventoAdjunto: eventoDe(a), Mime: a.Mime, Tamano: a.Tamano, SHA256: a.SHA256})
		},
	})
}

type entradaRechazar struct {
	ID     uuid.UUID
	Motivo string
}

func nuevoRechazar(*Modulo) *auditoria.Caso[entradaRechazar, Adjunto] {
	return auditoria.Nuevo(auditoria.Caso[entradaRechazar, Adjunto]{
		Accion: "adjuntos.rechazar", Permiso: nucleo.PermAdjuntosSubir,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaRechazar) (Adjunto, error) {
			q := adjuntosbd.New(t)
			f, err := q.RechazarAdjunto(ctx, adjuntosbd.RechazarAdjuntoParams{ID: in.ID, Motivo: &in.Motivo})
			if errors.Is(err, pgx.ErrNoRows) {
				previo, err := q.AdjuntoPorID(ctx, in.ID)
				if err != nil {
					return Adjunto{}, err
				}
				return aAdjunto(previo), nil
			}
			if err != nil {
				return Adjunto{}, err
			}
			if err := q.BorrarPartes(ctx, in.ID); err != nil {
				return Adjunto{}, err
			}
			a := aAdjunto(f)
			t.FijarEntidad("adjunto", a.ID)
			t.FijarEmpresa(empresaDe(f.EmpresaID))
			t.RegistrarCambios("adjunto", a.ID, nil, map[string]any{"estado": a.Estado, "motivo_rechazo": in.Motivo})
			return a, t.Emitir(EventoRechazado, eventoRechazado{eventoAdjunto: eventoDe(a), Motivo: in.Motivo})
		},
	})
}

// Lectura ---------------------------------------------------------------------------------

// EntradaDescargar pide el contenido de un adjunto completo.
type EntradaDescargar struct {
	ID uuid.UUID
}

func nuevoDescargar(*Modulo) *auditoria.Caso[EntradaDescargar, Adjunto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDescargar, Adjunto]{
		// Lectura auditada: fotos de la casa del cliente, firmas y documentos son datos personales.
		Accion: "adjuntos.ver", Permiso: nucleo.PermAdjuntosVer,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDescargar) (Adjunto, error) {
			a, err := cargar(ctx, t, in.ID, UsoVer)
			if err != nil {
				return Adjunto{}, err
			}
			if a.Estado != EstadoCompleto {
				return Adjunto{}, nucleo.Conflicto("adjunto_incompleto", "El adjunto todavía no termina de subirse o fue rechazado")
			}
			return a, nil
		},
	})
}

// EntradaListar lista los adjuntos de una entidad.
type EntradaListar struct {
	nucleo.ParamsPagina
	Entidad   string `query:"entidad" required:"true" pattern:"^[a-z][a-z_]{0,39}$"`
	EntidadID string `query:"entidadId" required:"true" minLength:"1" maxLength:"64"`
}

func nuevoListar(*Modulo) *auditoria.Caso[EntradaListar, nucleo.Pagina[Adjunto]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListar, nucleo.Pagina[Adjunto]]{
		Accion: "adjuntos.listar", Permiso: nucleo.PermAdjuntosVer, AuditarExito: sinAuditar[nucleo.Pagina[Adjunto]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListar) (nucleo.Pagina[Adjunto], error) {
			if _, err := verificarEntidad(ctx, t, in.Entidad, in.EntidadID, UsoVer); err != nil {
				return nucleo.Pagina[Adjunto]{}, err
			}
			var despues uuid.UUID
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Adjunto]{}, err
			}
			limite := in.Limite()
			filas, err := adjuntosbd.New(t).AdjuntosDeEntidad(ctx, adjuntosbd.AdjuntosDeEntidadParams{
				Entidad: in.Entidad, EntidadID: in.EntidadID, DespuesDe: despues, Limite: int32(limite + 1), //nolint:gosec // acotado a 201
			})
			if err != nil {
				return nucleo.Pagina[Adjunto]{}, err
			}
			visibles := filas[:0]
			for _, f := range filas {
				if empresaVisible(ctx, empresaDe(f.EmpresaID)) {
					visibles = append(visibles, f)
				}
			}
			return nucleo.Paginar(visibles, limite, aAdjunto, func(f adjuntosbd.NucleoAdjunto) []any { return []any{f.ID} }), nil
		},
	})
}
