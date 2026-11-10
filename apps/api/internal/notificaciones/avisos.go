package notificaciones

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Avisos del panel (la campana de la barra superior) y correos del sistema. El primer uso es el
// tope de uso del asistente de IA (contratos/conversaciones.md §16.1 y §16.9): al 80 % y al 100 %
// se avisa a quienes tienen mensajeria.asistente_configurar en esa empresa, por la campana y por
// correo. Uno por usuario y por clave (empresa, mes y umbral): un evento repetido no avisa dos
// veces.

// Tipos de aviso.
const TipoAvisoAsistenteTope = "asistente_tope"

// EnlaceAsistente es la pantalla de los ajustes y el uso del asistente en el panel.
const EnlaceAsistente = "/administracion/asistente"

// RetencionAvisos: cuánto se guardan los avisos y los correos del sistema (180 días).
const RetencionAvisos = 180 * 24 * time.Hour

// Motivos de un correo omitido o fallido.
const (
	CorreoSinSistema = "sin_correo_sistema" // no hay IRIS_SMTP_HOST / IRIS_SMTP_REMITENTE
	CorreoSinCorreo  = "sin_correo"         // el usuario no tiene correo
)

// AvisoPanel es un aviso de la campana.
type AvisoPanel struct {
	ID        int64          `json:"id"`
	Tipo      string         `json:"tipo" enum:"asistente_tope" doc:"asistente_tope: el asistente de IA de la empresa llegó al 80 % o al 100 % de su tope del mes"`
	Nivel     string         `json:"nivel" enum:"info,alerta,peligro" doc:"alerta al 80 %, peligro al 100 % (el asistente se pausó)"`
	Titulo    string         `json:"titulo" doc:"Texto en español, listo para mostrar"`
	Enlace    *string        `json:"enlace" nullable:"true" doc:"Ruta del panel a la que lleva (p. ej. /administracion/asistente)"`
	Datos     map[string]any `json:"datos" doc:"Referencias del aviso; asistente_tope: porcentaje (80, 100), mes (YYYY-MM) y tope (tokens del mes cuando se avisó)"`
	Leido     bool           `json:"leido"`
	LeidoEn   *time.Time     `json:"leidoEn" nullable:"true"`
	CreadoEn  time.Time      `json:"creadoEn"`
	EmpresaID *int64         `json:"empresaId" nullable:"true"`
}

func aAviso(f notificacionesbd.NotificacionesAviso) AvisoPanel {
	datos := map[string]any{}
	_ = json.Unmarshal(f.Datos, &datos)
	return AvisoPanel{ID: f.ID, Tipo: f.Tipo, Nivel: f.Nivel, Titulo: f.Titulo, Enlace: f.Enlace, Datos: datos, Leido: f.LeidoEn != nil,
		LeidoEn: f.LeidoEn, CreadoEn: f.CreadoEn, EmpresaID: f.EmpresaID}
}

// EntradaListarAvisos es GET /yo/avisos.
type EntradaListarAvisos struct {
	nucleo.ParamsPagina
	SoloNoLeidos bool `query:"soloNoLeidos" doc:"Solo los que no se han leído"`
}

// ResumenAvisos es GET /yo/avisos/resumen (el punto de la campana).
type ResumenAvisos struct {
	NoLeidos int32 `json:"noLeidos"`
}

// AvisosLeidos es POST /yo/avisos/leer-todos.
type AvisosLeidos struct {
	Leidos int64 `json:"leidos"`
}

// EntradaLeerAviso es POST /yo/avisos/{id}/leer.
type EntradaLeerAviso struct {
	ID int64 `json:"id"`
}

func (m *Modulo) nuevoListarAvisos() *auditoria.Caso[EntradaListarAvisos, nucleo.Pagina[AvisoPanel]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarAvisos, nucleo.Pagina[AvisoPanel]]{
		Accion: "avisos.listar", Acceso: auditoria.Autenticado,
		AuditarExito: func(nucleo.Pagina[AvisoPanel]) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarAvisos) (nucleo.Pagina[AvisoPanel], error) {
			a, err := persona(ctx)
			if err != nil {
				return nucleo.Pagina[AvisoPanel]{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return nucleo.Pagina[AvisoPanel]{}, err
			}
			var antes int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &antes); err != nil {
				return nucleo.Pagina[AvisoPanel]{}, err
			}
			limite := in.Limite()
			fs, err := notificacionesbd.New(t).ListarAvisos(ctx, notificacionesbd.ListarAvisosParams{UsuarioID: a.ID, Empresa: empresa,
				SoloNoLeidos: in.SoloNoLeidos, AntesDe: antes, Limite: int32(limite + 1)}) //nolint:gosec // acotado a 200
			if err != nil {
				return nucleo.Pagina[AvisoPanel]{}, err
			}
			return nucleo.Paginar(fs, limite, aAviso, func(f notificacionesbd.NotificacionesAviso) []any { return []any{f.ID} }), nil
		},
	})
}

func (m *Modulo) nuevoResumenAvisos() *auditoria.Caso[struct{}, ResumenAvisos] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ResumenAvisos]{
		Accion: "avisos.resumen", Acceso: auditoria.Autenticado,
		AuditarExito: func(ResumenAvisos) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ResumenAvisos, error) {
			a, err := persona(ctx)
			if err != nil {
				return ResumenAvisos{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return ResumenAvisos{}, err
			}
			n, err := notificacionesbd.New(t).ContarAvisosNoLeidos(ctx, notificacionesbd.ContarAvisosNoLeidosParams{UsuarioID: a.ID, Empresa: empresa})
			return ResumenAvisos{NoLeidos: n}, err
		},
	})
}

// Marcar leído es un estado personal de la campana (como abrir una notificación): no es un hecho de
// negocio, no emite evento ni deja fila de auditoría.
func (m *Modulo) nuevoLeerAviso() *auditoria.Caso[EntradaLeerAviso, AvisoPanel] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLeerAviso, AvisoPanel]{
		Accion: "avisos.leer", Acceso: auditoria.Autenticado,
		AuditarExito: func(AvisoPanel) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLeerAviso) (AvisoPanel, error) {
			a, err := persona(ctx)
			if err != nil {
				return AvisoPanel{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return AvisoPanel{}, err
			}
			f, err := notificacionesbd.New(t).LeerAviso(ctx, notificacionesbd.LeerAvisoParams{ID: in.ID, UsuarioID: a.ID, Empresa: empresa, Ahora: ptr(m.ahora())})
			if esSinFilas(err) {
				return AvisoPanel{}, nucleo.NoEncontrado("No existe el aviso")
			}
			if err != nil {
				return AvisoPanel{}, err
			}
			return aAviso(f), nil
		},
	})
}

func (m *Modulo) nuevoLeerTodosLosAvisos() *auditoria.Caso[struct{}, AvisosLeidos] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, AvisosLeidos]{
		Accion: "avisos.leer_todos", Acceso: auditoria.Autenticado,
		AuditarExito: func(AvisosLeidos) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (AvisosLeidos, error) {
			a, err := persona(ctx)
			if err != nil {
				return AvisosLeidos{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return AvisosLeidos{}, err
			}
			n, err := notificacionesbd.New(t).LeerTodosLosAvisos(ctx, notificacionesbd.LeerTodosLosAvisosParams{UsuarioID: a.ID, Empresa: empresa, Ahora: ptr(m.ahora())})
			return AvisosLeidos{Leidos: n}, err
		},
	})
}

func ptr[T any](v T) *T { return &v }

// reMes: el mes del evento (YYYY-MM); va en la clave y en el texto del correo.
var reMes = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// Tope del asistente ------------------------------------------------------------------------------

// EntradaAvisarTope es el evento asistente.tope_alcanzado (lo llama el suscriptor del worker).
type EntradaAvisarTope struct {
	EventoID   uuid.UUID `json:"-"`
	EmpresaID  *int64    `json:"empresa_id"`
	Porcentaje int       `json:"porcentaje"`
	Mes        string    `json:"mes"`
	Tokens     int64     `json:"tokens"`
	Tope       int64     `json:"tope"`
}

// ResultadoAvisarTope: cuántos avisos nuevos y cuántos correos quedaron por enviar u omitidos.
type ResultadoAvisarTope struct {
	Avisos            int `json:"avisos"`
	CorreosPendientes int `json:"correos_pendientes"`
	CorreosOmitidos   int `json:"correos_omitidos"`
}

// TituloTope es el texto del aviso y el asunto del correo (decisión del PM).
func TituloTope(empresa string, porcentaje int) string {
	if porcentaje >= 100 {
		return fmt.Sprintf("El asistente de IA de %s se pausó: llegó a su tope del mes", empresa)
	}
	return fmt.Sprintf("El asistente de IA de %s usó el 80 %% de su tope del mes", empresa)
}

// ClaveTope une los avisos de una empresa, un mes, un umbral y un valor del tope: una sola vez por
// umbral y por mes con ese tope. Si suben el tope dentro del mes y lo vuelven a cruzar, la clave es
// otra y se avisa de nuevo.
func ClaveTope(empresa *int64, mes string, porcentaje int, tope int64) string {
	var id int64
	if empresa != nil {
		id = *empresa
	}
	return fmt.Sprintf("asistente_tope:%d:%s:%d:%d", id, mes, porcentaje, tope)
}

func (m *Modulo) nuevoAvisarTope() *auditoria.Caso[EntradaAvisarTope, ResultadoAvisarTope] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAvisarTope, ResultadoAvisarTope]{
		Accion: "avisos.asistente_tope", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoAvisarTope) bool { return r.Avisos > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAvisarTope) (ResultadoAvisarTope, error) {
			if err := soloSistema(ctx); err != nil {
				return ResultadoAvisarTope{}, err
			}
			r := ResultadoAvisarTope{}
			if (in.Porcentaje != 80 && in.Porcentaje != 100) || in.Tope <= 0 || !reMes.MatchString(in.Mes) {
				return r, nil
			}
			if in.EmpresaID != nil {
				t.FijarEmpresa(*in.EmpresaID)
				t.FijarEntidad("empresa", *in.EmpresaID)
			} else {
				t.FijarEmpresa(0)
			}
			q := notificacionesbd.New(t)
			nombre := "tu empresa"
			if in.EmpresaID != nil {
				if n, err := q.NombreEmpresa(ctx, *in.EmpresaID); err == nil && n != "" {
					nombre = n
				} else if err != nil && !esSinFilas(err) {
					return r, err
				}
			}
			us, err := q.DestinatariosConPermiso(ctx, notificacionesbd.DestinatariosConPermisoParams{
				Permiso: string(nucleo.PermMensajeriaAsistenteConfigurar), Empresa: in.EmpresaID})
			if err != nil {
				return r, err
			}
			clave := ClaveTope(in.EmpresaID, in.Mes, in.Porcentaje, in.Tope)
			titulo := TituloTope(nombre, in.Porcentaje)
			nivel := "alerta"
			if in.Porcentaje >= 100 {
				nivel = "peligro"
			}
			datos, err := json.Marshal(map[string]any{"porcentaje": in.Porcentaje, "mes": in.Mes, "tope": in.Tope})
			if err != nil {
				return r, err
			}
			datosCorreo, err := json.Marshal(map[string]any{"porcentaje": in.Porcentaje, "mes": in.Mes, "tokens": in.Tokens, "tope": in.Tope, "empresa": nombre})
			if err != nil {
				return r, err
			}
			enlace := EnlaceAsistente
			evento := pgtype.UUID{Bytes: in.EventoID, Valid: in.EventoID != uuid.Nil}
			usuarios := []int64{}
			for _, u := range us {
				av, err := q.InsertarAviso(ctx, notificacionesbd.InsertarAvisoParams{UsuarioID: u.ID, EmpresaID: in.EmpresaID, Tipo: TipoAvisoAsistenteTope,
					Clave: clave, Nivel: nivel, Titulo: titulo, Enlace: &enlace, Datos: datos, EventoID: evento})
				if esSinFilas(err) {
					continue // ya avisado (evento repetido)
				}
				if err != nil {
					return r, err
				}
				r.Avisos++
				usuarios = append(usuarios, u.ID)
				estado, motivo := "pendiente", (*string)(nil)
				switch {
				case m.cfg.Correo == nil:
					estado, motivo = "omitido", ptr(CorreoSinSistema)
				case u.Correo == "":
					estado, motivo = "omitido", ptr(CorreoSinCorreo)
				}
				c, err := q.InsertarCorreo(ctx, notificacionesbd.InsertarCorreoParams{UsuarioID: u.ID, EmpresaID: in.EmpresaID, AvisoID: &av.ID,
					Tipo: TipoAvisoAsistenteTope, Clave: clave, Para: u.Correo, Asunto: titulo, Datos: datosCorreo, Estado: estado, Motivo: motivo})
				if esSinFilas(err) {
					continue
				}
				if err != nil {
					return r, err
				}
				if estado == "omitido" {
					r.CorreosOmitidos++
					continue
				}
				r.CorreosPendientes++
				if m.cfg.Encolar != nil {
					if err := m.cfg.Encolar(ctx, t.Tx, ArgsEnviarCorreo{CorreoID: c.ID}); err != nil {
						return r, err
					}
				}
			}
			if r.Avisos == 0 {
				return r, nil
			}
			t.Anotar(map[string]any{"clave": clave, "avisos": r.Avisos, "correos_pendientes": r.CorreosPendientes, "correos_omitidos": r.CorreosOmitidos})
			correo := "enviar"
			if m.cfg.Correo == nil {
				correo = "omitido"
			}
			return r, t.Emitir("notificaciones.avisos_creados", map[string]any{"tipo": TipoAvisoAsistenteTope, "clave": clave,
				"empresa_id": in.EmpresaID, "usuarios": usuarios, "correo": correo})
		},
	})
}

// SuscribirAvisos registra el suscriptor de los avisos del panel (cmd/worker). Corre aunque el push
// esté apagado: la campana y el correo no dependen de FCM.
func (m *Modulo) SuscribirAvisos(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("notificaciones.avisos", "asistente.tope_alcanzado", func(ctx context.Context, e integracion.Evento) error {
		var in EntradaAvisarTope
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			return fmt.Errorf("notificaciones: payload de %s: %w", e.Evento, err)
		}
		in.EventoID = e.ID
		_, err := m.AvisarTope.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
