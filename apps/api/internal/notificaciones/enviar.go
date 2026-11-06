package notificaciones

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	notificacionesbd "github.com/HCDestroyer/iris/apps/api/internal/notificaciones/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// Estados de un envío.
const (
	EnvioPendiente = "pendiente"
	EnvioEnviado   = "enviado"
	EnvioOmitido   = "omitido"
	EnvioFallido   = "fallido"
)

// Motivos de un envío omitido (no salió: no es un error) o fallido.
const (
	OmitidoDispositivo = "dispositivo_quitado"
	OmitidoPreferencia = "preferencia"
	OmitidoSilencio    = "no_molestar"
	OmitidoSinPermiso  = "sin_permiso"
	OmitidoNoAplica    = "ya_no_aplica"
	FallidoAgotado     = "agotado"
	FallidoRechazado   = "rechazado"
)

func esSinFilas(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// EnvioListo es el mensaje armado que el job manda a FCM (fuera de la transacción).
type EnvioListo struct {
	EnvioID       int64
	DispositivoID int64
	Mensaje       MensajeFCM `json:"-"`
}

// destinatario es el usuario del dispositivo con sus permisos de ahora.
type destinatario struct {
	ctx  context.Context
	disp notificacionesbd.NotificacionesDispositivo
	pref notificacionesbd.NotificacionesPreferencia
	zona *time.Location
}

// nuevoPrepararEnvio vuelve a comprobar todo (credencial, empresa, preferencias, permisos y No
// molestar) y arma el mensaje. Si no debe salir, cierra el envío como omitido. Corre como sistema.
func (m *Modulo) nuevoPrepararEnvio() *auditoria.Caso[int64, *EnvioListo] {
	return auditoria.Nuevo(auditoria.Caso[int64, *EnvioListo]{
		Accion:       "notificaciones.preparar_envio",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(*EnvioListo) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, id int64) (*EnvioListo, error) {
			q := notificacionesbd.New(t)
			env, err := q.EnvioParaEnviar(ctx, id)
			if esSinFilas(err) {
				return nil, nil
			}
			if err != nil || env.Estado != EnvioPendiente {
				return nil, err
			}
			omitir := func(motivo string) (*EnvioListo, error) {
				return nil, q.CerrarEnvio(ctx, notificacionesbd.CerrarEnvioParams{ID: env.ID, Estado: EnvioOmitido, Motivo: &motivo})
			}
			disp, err := q.DispositivoPorID(ctx, env.DispositivoID)
			if esSinFilas(err) {
				return omitir(OmitidoDispositivo)
			}
			if err != nil {
				return nil, err
			}
			t.FijarEmpresa(valor(disp.EmpresaID))
			d, quitarPor, err := m.destinatario(ctx, t, q, disp)
			if err != nil {
				return nil, err
			}
			if quitarPor != "" {
				t.FijarEntidad("dispositivo", disp.ID)
				if err := quitar(ctx, t, q, disp, quitarPor); err != nil {
					return nil, err
				}
				return omitir(OmitidoDispositivo)
			}
			if !mismoID(env.EmpresaID, disp.EmpresaID) {
				return omitir(OmitidoSinPermiso)
			}
			if !tipoEncendido(d.pref, env.Tipo) {
				return omitir(OmitidoPreferencia)
			}
			var datos map[string]any
			if err := json.Unmarshal(env.Datos, &datos); err != nil {
				return nil, err
			}
			ahora := m.ahora()
			c, motivo, err := m.armar(ctx, t, d, env, datos, ahora)
			if err != nil {
				return nil, err
			}
			if motivo != "" {
				return omitir(motivo)
			}
			if enSilencio(d.pref, ahora) && (!c.urgente || !d.pref.UrgenteIgual) {
				// No se guarda para después: la app lo muestra al abrirse.
				return omitir(OmitidoSilencio)
			}
			return &EnvioListo{EnvioID: env.ID, DispositivoID: disp.ID, Mensaje: c.mensaje(env, disp)}, nil
		},
	})
}

// destinatario arma el contexto del usuario del dispositivo con sus permisos de ahora. Si su
// credencial ya no vale o salió de la empresa, devuelve el motivo para quitar el dispositivo.
func (m *Modulo) destinatario(ctx context.Context, t *auditoria.Tx, q *notificacionesbd.Queries, disp notificacionesbd.NotificacionesDispositivo) (destinatario, string, error) {
	empresas := contexto.Empresas{Estado: contexto.EmpresaSinConector, Disponibles: []int64{}}
	if disp.EmpresaID != nil {
		en, err := identidad.UsuarioEnEmpresa(ctx, t, disp.UsuarioID, *disp.EmpresaID)
		if err != nil || !en {
			return destinatario{}, QuitadoSinAcceso, err
		}
		empresas = contexto.Empresas{Estado: contexto.EmpresaActiva, Activa: *disp.EmpresaID, Disponibles: []int64{*disp.EmpresaID}}
	} else {
		// Sin conector: si ya hay empresas registradas, un dispositivo sin empresa no es de nadie.
		hay, err := q.HayEmpresas(ctx)
		if err != nil || hay {
			return destinatario{}, QuitadoSinAcceso, err
		}
	}
	actor := contexto.Actor{Tipo: contexto.ActorUsuario, ID: disp.UsuarioID}
	uctx := context.WithoutCancel(ctx)
	if disp.TokenApiID != nil {
		actor.Tipo, actor.TokenID = contexto.ActorToken, *disp.TokenApiID
		uctx = contexto.ConSesion(uctx, contexto.Sesion{})
	} else if disp.SesionID != nil {
		uctx = contexto.ConSesion(uctx, contexto.Sesion{ID: *disp.SesionID, UsuarioID: disp.UsuarioID})
	}
	uctx = contexto.ConEmpresas(contexto.ConActor(uctx, actor, contexto.Permisos{}), empresas)
	uctx, err := identidad.Revalidar(uctx, t)
	if err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) {
			return destinatario{}, QuitadoSesionCerrada, nil
		}
		return destinatario{}, "", err
	}
	pref, err := leerPreferencias(ctx, q, disp.UsuarioID)
	if err != nil {
		return destinatario{}, "", err
	}
	return destinatario{ctx: uctx, disp: disp, pref: pref, zona: zonaDe(pref)}, "", nil
}

// FalloFCM describe por qué FCM no aceptó el mensaje (sin el token ni el contenido).
type FalloFCM struct {
	Status        int    `json:"status"`
	Codigo        string `json:"codigo"`
	Reintentable  bool   `json:"reintentable"`
	TokenInvalido bool   `json:"tokenInvalido"`
}

// ResultadoEnvio es lo que respondió FCM.
type ResultadoEnvio struct {
	EnvioID       int64     `json:"envioId"`
	DispositivoID int64     `json:"dispositivoId"`
	Nombre        string    `json:"-"`
	Fallo         *FalloFCM `json:"fallo"`
	// Ultimo: era el último intento del job.
	Ultimo bool `json:"ultimo"`
}

// ErrReintentar: FCM respondió 429/5xx o no respondió; el job se reintenta con espera.
var ErrReintentar = errors.New("notificaciones: FCM no disponible; se reintenta")

// nuevoCerrarEnvio registra la respuesta de FCM: enviado, fallido (4xx: no se reintenta; un token
// inválido quita el dispositivo) o, con 429/5xx, true = reintentar (hasta agotar los intentos).
func (m *Modulo) nuevoCerrarEnvio() *auditoria.Caso[ResultadoEnvio, bool] {
	return auditoria.Nuevo(auditoria.Caso[ResultadoEnvio, bool]{
		Accion:       "notificaciones.cerrar_envio",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(bool) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, r ResultadoEnvio) (bool, error) {
			q := notificacionesbd.New(t)
			ahora := m.ahora()
			if r.Fallo == nil {
				if err := q.MarcarUltimoEnvio(ctx, notificacionesbd.MarcarUltimoEnvioParams{ID: r.DispositivoID, Ahora: &ahora}); err != nil {
					return false, err
				}
				nombre := r.Nombre
				return false, q.CerrarEnvio(ctx, notificacionesbd.CerrarEnvioParams{ID: r.EnvioID, Estado: EnvioEnviado,
					FcmMensaje: &nombre, EnviadoEn: &ahora, Intentos: 1})
			}
			if r.Fallo.Reintentable && !r.Ultimo {
				if err := q.ContarIntento(ctx, r.EnvioID); err != nil {
					return false, err
				}
				return true, nil
			}
			motivo := FallidoRechazado
			if r.Fallo.Reintentable {
				motivo = FallidoAgotado
			}
			if r.Fallo.TokenInvalido {
				motivo = QuitadoTokenInvalido
				d, err := q.DispositivoPorID(ctx, r.DispositivoID)
				switch {
				case err == nil:
					t.FijarEmpresa(valor(d.EmpresaID))
					t.FijarEntidad("dispositivo", d.ID)
					t.Anotar(map[string]any{"fcm_status": r.Fallo.Status, "fcm_codigo": r.Fallo.Codigo})
					if err := quitar(ctx, t, q, d, QuitadoTokenInvalido); err != nil {
						return false, err
					}
				case !esSinFilas(err):
					return false, err
				}
			}
			detalle := motivo + ": " + r.Fallo.Codigo
			return false, q.CerrarEnvio(ctx, notificacionesbd.CerrarEnvioParams{ID: r.EnvioID, Estado: EnvioFallido, Motivo: &detalle, Intentos: 1})
		},
	})
}

// Enviar corre un envío completo: prepara (transacción corta), manda a FCM fuera de la transacción
// y registra el resultado. Devuelve ErrReintentar si hay que reintentar. Si el proceso cae entre la
// respuesta de FCM y el registro, el reintento puede mandarlo otra vez (raro; la app descarta el
// repetido por `id`).
func (m *Modulo) Enviar(ctx context.Context, envioID int64, ultimo bool) error {
	listo, err := m.PrepararEnvio.Ejecutar(ctx, envioID)
	if err != nil || listo == nil {
		return err
	}
	if m.cfg.FCM == nil {
		motivo := "sin_configurar"
		return nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return notificacionesbd.New(tx).CerrarEnvio(ctx, notificacionesbd.CerrarEnvioParams{ID: envioID, Estado: EnvioOmitido, Motivo: &motivo})
		})
	}
	r := ResultadoEnvio{EnvioID: listo.EnvioID, DispositivoID: listo.DispositivoID, Ultimo: ultimo}
	nombre, err := m.cfg.FCM.Enviar(ctx, listo.Mensaje)
	if err != nil {
		var e *ErrorFCM
		if !errors.As(err, &e) {
			e = &ErrorFCM{causa: err}
		}
		r.Fallo = &FalloFCM{Status: e.Status, Codigo: e.Codigo, Reintentable: e.Reintentable(), TokenInvalido: e.TokenInvalido()}
		m.cfg.Log.WarnContext(ctx, "notificaciones: FCM no aceptó el mensaje", "envio", envioID, "status", e.Status, "codigo", e.Codigo,
			"reintentable", r.Fallo.Reintentable)
	}
	r.Nombre = nombre
	reintentar, err := m.CerrarEnvio.Ejecutar(ctx, r)
	if err == nil && reintentar {
		return ErrReintentar
	}
	return err
}

func valor(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func mismoID(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
