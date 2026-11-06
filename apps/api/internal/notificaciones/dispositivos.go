package notificaciones

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Motivos de dispositivo.quitado.
const (
	QuitadoUsuario       = "usuario"
	QuitadoSesionCerrada = "sesion_cerrada"
	QuitadoTokenInvalido = "token_invalido"
	QuitadoOtroUsuario   = "otro_usuario"
	QuitadoSinAcceso     = "sin_acceso"
)

// PlataformaAndroid es la única plataforma de esta fase (iOS: más adelante).
const PlataformaAndroid = "android"

// Dispositivo es un teléfono registrado. Nunca lleva el token FCM.
type Dispositivo struct {
	ID            int64      `json:"id"`
	Plataforma    string     `json:"plataforma" enum:"android"`
	AppVersion    string     `json:"appVersion"`
	TokenFin      string     `json:"tokenFin" doc:"Últimos 6 caracteres del token FCM (nunca el token completo)"`
	EmpresaID     *int64     `json:"empresaId" nullable:"true" doc:"Empresa en que recibe push; null sin conector"`
	RegistradoEn  time.Time  `json:"registradoEn"`
	ActualizadoEn time.Time  `json:"actualizadoEn"`
	UltimoEnvioEn *time.Time `json:"ultimoEnvioEn" nullable:"true" doc:"Último push enviado a este teléfono"`
}

func aDispositivo(d notificacionesbd.NotificacionesDispositivo) Dispositivo {
	return Dispositivo{ID: d.ID, Plataforma: d.Plataforma, AppVersion: d.AppVersion, TokenFin: d.TokenFin, EmpresaID: d.EmpresaID,
		RegistradoEn: d.CreatedAt, ActualizadoEn: d.UpdatedAt, UltimoEnvioEn: d.UltimoEnvioEn}
}

// EntradaRegistrarDispositivo es POST /dispositivos.
type EntradaRegistrarDispositivo struct {
	Token      string `json:"token" minLength:"1" maxLength:"4096" doc:"Token nativo de FCM (getDevicePushTokenAsync)" auditoria:"sensible"`
	Plataforma string `json:"plataforma" enum:"android"`
	AppVersion string `json:"appVersion" minLength:"1" maxLength:"40" doc:"Versión de la app (1.4.0)"`
}

// DispositivoRegistrado es la respuesta (201 nuevo, 200 ya estaba).
type DispositivoRegistrado struct {
	Dispositivo
	Nuevo bool `json:"-"`
}

// empresaDispositivo: la empresa activa de la petición (nil = sin conector).
func empresaDispositivo(ctx context.Context) (*int64, error) {
	switch e := contexto.EmpresasDe(ctx); e.Estado {
	case contexto.EmpresaActiva:
		id := e.Activa
		return &id, nil
	case contexto.EmpresaSinConector:
		return nil, nil
	case contexto.EmpresaPorElegir:
		return nil, nucleo.EmpresaRequerida()
	case contexto.EmpresaSinAsignar:
		return nil, nucleo.SinEmpresa()
	case contexto.EmpresaNoResuelta:
	}
	return nil, auditoria.ErrEmpresaNoResuelta
}

// persona: los dispositivos son de personas (sesión de la app o token iris_pat_ propio), nunca de
// un usuario de integración.
func persona(ctx context.Context) (contexto.Actor, error) {
	a := contexto.ActorDe(ctx)
	if a.ID == 0 || (a.Tipo != contexto.ActorUsuario && a.Tipo != contexto.ActorToken) {
		return a, nucleo.Prohibido("Las notificaciones push son de las personas que usan la app")
	}
	return a, nil
}

func validarToken(token string) error {
	if token == "" || utf8.RuneCountInString(token) > 4096 || strings.IndexFunc(token, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }) >= 0 {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "token", Message: "El token de FCM no es válido", Rule: "token"})
	}
	return nil
}

// finDe son los últimos 6 caracteres del token (lo único que va a la auditoría y a los eventos).
func finDe(token string) string {
	if len(token) <= 6 {
		return token
	}
	return token[len(token)-6:]
}

func (m *Modulo) nuevoRegistrarDispositivo() *auditoria.Caso[EntradaRegistrarDispositivo, DispositivoRegistrado] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrarDispositivo, DispositivoRegistrado]{
		Accion: "dispositivos.registrar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarDispositivo) (DispositivoRegistrado, error) {
			a, err := persona(ctx)
			if err != nil {
				return DispositivoRegistrado{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return DispositivoRegistrado{}, err
			}
			in.Token = strings.TrimSpace(in.Token)
			if err := validarToken(in.Token); err != nil {
				return DispositivoRegistrado{}, err
			}
			if in.Plataforma != PlataformaAndroid {
				return DispositivoRegistrado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "plataforma", Message: "Por ahora solo android", Rule: "enum"})
			}
			in.AppVersion = strings.TrimSpace(in.AppVersion)
			if in.AppVersion == "" || utf8.RuneCountInString(in.AppVersion) > 40 {
				return DispositivoRegistrado{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "appVersion", Message: "Versión inválida", Rule: "length"})
			}
			p := notificacionesbd.GuardarDispositivoParams{UsuarioID: a.ID, EmpresaID: empresa, Plataforma: in.Plataforma,
				Token: in.Token, TokenFin: finDe(in.Token), AppVersion: in.AppVersion}
			h := sha256.Sum256([]byte(in.Token))
			p.TokenHash = h[:]
			switch s := contexto.SesionDe(ctx); {
			case a.TokenID != 0:
				p.TokenApiID = &a.TokenID
			case s.ID != 0:
				p.SesionID = &s.ID
			default:
				return DispositivoRegistrado{}, nucleo.NoAutenticado("")
			}
			q := notificacionesbd.New(t)
			// El teléfono cambió de manos: el token deja de ser del usuario anterior.
			otros, err := q.QuitarTokenDeOtros(ctx, notificacionesbd.QuitarTokenDeOtrosParams{TokenHash: p.TokenHash, UsuarioID: a.ID})
			if err != nil {
				return DispositivoRegistrado{}, err
			}
			for _, d := range otros {
				if err := emitirQuitado(t, d, QuitadoOtroUsuario); err != nil {
					return DispositivoRegistrado{}, err
				}
			}
			f, err := q.GuardarDispositivo(ctx, p)
			if err != nil {
				return DispositivoRegistrado{}, err
			}
			d := notificacionesbd.NotificacionesDispositivo{ID: f.ID, UsuarioID: f.UsuarioID, EmpresaID: f.EmpresaID, SesionID: f.SesionID,
				TokenApiID: f.TokenApiID, Plataforma: f.Plataforma, TokenFin: f.TokenFin, AppVersion: f.AppVersion,
				UltimoEnvioEn: f.UltimoEnvioEn, CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt}
			vista := aDispositivo(d)
			t.FijarEntidad("dispositivo", f.ID)
			t.Anotar(map[string]any{"fin_fcm": f.TokenFin, "nuevo": f.Nuevo})
			if f.Nuevo {
				t.RegistrarCambios("dispositivo", f.ID, nil, map[string]any{"plataforma": f.Plataforma, "app_version": f.AppVersion, "empresa_id": f.EmpresaID})
			}
			if err := t.EmitirEnEmpresa("dispositivo.registrado", f.EmpresaID, map[string]any{"dispositivo_id": f.ID, "usuario_id": f.UsuarioID,
				"empresa_id": f.EmpresaID, "plataforma": f.Plataforma, "app_version": f.AppVersion, "token_fin": f.TokenFin, "nuevo": f.Nuevo}); err != nil {
				return DispositivoRegistrado{}, err
			}
			return DispositivoRegistrado{Dispositivo: vista, Nuevo: f.Nuevo}, nil
		},
	})
}

// EntradaListarDispositivos es GET /dispositivos.
type EntradaListarDispositivos struct {
	nucleo.ParamsPagina
}

func (m *Modulo) nuevoListarDispositivos() *auditoria.Caso[EntradaListarDispositivos, nucleo.Pagina[Dispositivo]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarDispositivos, nucleo.Pagina[Dispositivo]]{
		Accion:       "dispositivos.listar",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(nucleo.Pagina[Dispositivo]) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarDispositivos) (nucleo.Pagina[Dispositivo], error) {
			a, err := persona(ctx)
			if err != nil {
				return nucleo.Pagina[Dispositivo]{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return nucleo.Pagina[Dispositivo]{}, err
			}
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Dispositivo]{}, err
			}
			limite := in.Limite()
			fs, err := notificacionesbd.New(t).ListarDispositivos(ctx, notificacionesbd.ListarDispositivosParams{
				UsuarioID: a.ID, Empresa: empresa, Despues: despues, Limite: int32(limite + 1)}) //nolint:gosec // acotado a 200
			if err != nil {
				return nucleo.Pagina[Dispositivo]{}, err
			}
			return nucleo.Paginar(fs, limite, aDispositivo, func(f notificacionesbd.NotificacionesDispositivo) []any { return []any{f.ID} }), nil
		},
	})
}

// EntradaQuitarDispositivo es POST /dispositivos/{id}/quitar.
type EntradaQuitarDispositivo struct {
	ID int64 `json:"-"`
}

func (m *Modulo) nuevoQuitarDispositivo() *auditoria.Caso[EntradaQuitarDispositivo, Dispositivo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaQuitarDispositivo, Dispositivo]{
		Accion: "dispositivos.quitar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaQuitarDispositivo) (Dispositivo, error) {
			a, err := persona(ctx)
			if err != nil {
				return Dispositivo{}, err
			}
			empresa, err := empresaDispositivo(ctx)
			if err != nil {
				return Dispositivo{}, err
			}
			q := notificacionesbd.New(t)
			d, err := q.DispositivoDeUsuario(ctx, notificacionesbd.DispositivoDeUsuarioParams{ID: in.ID, UsuarioID: a.ID, Empresa: empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return Dispositivo{}, nucleo.NoEncontrado("El dispositivo no existe o no es tuyo")
			}
			if err != nil {
				return Dispositivo{}, err
			}
			if err := quitar(ctx, t, q, d, QuitadoUsuario); err != nil {
				return Dispositivo{}, err
			}
			return aDispositivo(d), nil
		},
	})
}

// quitar borra el dispositivo, deja su cambio en la auditoría de la acción y emite
// dispositivo.quitado.
func quitar(ctx context.Context, t *auditoria.Tx, q *notificacionesbd.Queries, d notificacionesbd.NotificacionesDispositivo, motivo string) error {
	if _, err := q.BorrarDispositivo(ctx, d.ID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	t.RegistrarCambios("dispositivo", d.ID, map[string]any{"plataforma": d.Plataforma, "app_version": d.AppVersion, "empresa_id": d.EmpresaID}, map[string]any{})
	t.Anotar(map[string]any{"dispositivo_id": d.ID, "fin_fcm": d.TokenFin, "motivo": motivo})
	return emitirQuitado(t, d, motivo)
}

func emitirQuitado(t *auditoria.Tx, d notificacionesbd.NotificacionesDispositivo, motivo string) error {
	return t.EmitirEnEmpresa("dispositivo.quitado", d.EmpresaID, map[string]any{"dispositivo_id": d.ID, "usuario_id": d.UsuarioID,
		"empresa_id": d.EmpresaID, "token_fin": d.TokenFin, "motivo": motivo})
}

// quitarDeSesion es el gancho de sesion.cerrar: los dispositivos de esa sesión, en su transacción.
func quitarDeSesion(ctx context.Context, t *auditoria.Tx, sesionID int64) error {
	ds, err := notificacionesbd.New(t).BorrarDispositivosDeSesion(ctx, &sesionID)
	if err != nil {
		return err
	}
	for _, d := range ds {
		t.Anotar(map[string]any{"dispositivos_quitados": len(ds)})
		if err := emitirQuitado(t, d, QuitadoSesionCerrada); err != nil {
			return err
		}
	}
	return nil
}
