package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Verificación de un cambio o un intento de recuperación, de servidor a servidor (contrato con
// ArgosISP 0.14.0 §3.10): Argos consulta con la Idempotency-Key con la que lo recibió si Iris se
// lo mandó. Nunca devuelve la imagen, el autor ni las horas.

// Motivos de una verificación no exitosa (lista cerrada del contrato).
const (
	MotivoEnvioEnCurso      = "envio_en_curso"
	MotivoNoEnviadoPorIris  = "no_enviado_por_iris"
	MotivoNoCoincide        = "no_coincide"
	MotivoFotoPendiente     = "foto_pendiente"
	MotivoFotoInvalida      = "foto_invalida"
	TopeVerificacionMinuto  = 60
	TopeVerificacionPorDia  = 20
	diasVerificacionGuardas = 2
)

// EntradaVerificarIntento: GET /integraciones/argosisp/intentos-recuperacion/verificacion.
type EntradaVerificarIntento struct {
	EmpresaID      int64  `query:"empresa_id" required:"true" minimum:"1" doc:"Empresa del servicio"`
	IdempotencyKey string `query:"idempotency_key" required:"true" minLength:"1" maxLength:"300" doc:"La Idempotency-Key con la que Argos recibió el cambio o el intento, tal cual"`
}

// VerificacionIntento: el resultado (motivo null si verificado).
type VerificacionIntento struct {
	Verificado bool    `json:"verificado"`
	Motivo     *string `json:"motivo" nullable:"true" enum:"envio_en_curso,no_enviado_por_iris,no_coincide,foto_pendiente,foto_invalida" doc:"Por qué no está verificado (null si lo está)"`
}

func verificacionNoEncontrada() *nucleo.Problema {
	return nucleo.NoEncontrado("La operación no existe")
}

// VerificarIntento aplica el tope (60 por minuto por conector y 20 por clave al día, fuera de la
// transacción del caso de uso: cuenta también lo que termina en error) y verifica.
func (m *Modulo) VerificarIntento(ctx context.Context, in EntradaVerificarIntento) (VerificacionIntento, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return VerificacionIntento{}, nucleo.ErrSinBD
	}
	actor := contexto.ActorDe(ctx)
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermAdjuntosVerificarIntegracion)) {
		// Sin el permiso ni siquiera se cuenta: lo rechaza el caso de uso (403, auditado).
		return m.verificarIntento.Ejecutar(ctx, in)
	}
	q := pendientesbd.New(bd)
	conectorID, err := q.ConectorDeToken(ctx, &actor.TokenID)
	if errors.Is(err, pgx.ErrNoRows) {
		return m.verificarIntento.Ejecutar(ctx, in) // sin conector: 404 (en el caso de uso)
	}
	if err != nil {
		return VerificacionIntento{}, err
	}
	ahora := m.ahora()
	l := ahora.In(zonaGuatemala)
	dia := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zonaGuatemala)
	// M-1: contar e insertar en una transacción, con el conector y la clave bloqueados (siempre en
	// ese orden): dos consultas a la vez no pasan el tope. Se confirma aunque la consulta termine en
	// error (cuenta igual).
	var tope error
	err = nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		qt := pendientesbd.New(tx)
		if err := qt.BloquearVerificacion(ctx, "verificacion:conector:"+strconv.FormatInt(conectorID, 10)); err != nil {
			return err
		}
		if err := qt.BloquearVerificacion(ctx, "verificacion:clave:"+in.IdempotencyKey); err != nil {
			return err
		}
		n, err := qt.ContarVerificaciones(ctx, pendientesbd.ContarVerificacionesParams{ConectorID: conectorID, Minuto: ahora.Add(-time.Minute),
			Clave: in.IdempotencyKey, Dia: dia})
		if err != nil {
			return err
		}
		switch {
		case n.PorMinuto >= TopeVerificacionMinuto:
			tope = nucleo.DemasiadosIntentos("demasiadas_peticiones", "Más de 60 verificaciones por minuto de este conector", time.Minute)
			return nil
		case n.PorClave >= TopeVerificacionPorDia:
			tope = nucleo.DemasiadosIntentos("demasiadas_peticiones", "Más de 20 verificaciones de esta clave hoy", dia.AddDate(0, 0, 1).Sub(ahora))
			return nil
		}
		return qt.RegistrarVerificacion(ctx, pendientesbd.RegistrarVerificacionParams{ConectorID: conectorID, Clave: in.IdempotencyKey, En: ahora})
	})
	if err != nil {
		return VerificacionIntento{}, err
	}
	if tope != nil {
		return VerificacionIntento{}, tope
	}
	_ = q.PurgarVerificaciones(ctx, ahora.AddDate(0, 0, -diasVerificacionGuardas))
	return m.verificarIntento.Ejecutar(ctx, in)
}

var zonaGuatemala = func() *time.Location {
	z, err := time.LoadLocation("America/Guatemala")
	if err != nil {
		return time.FixedZone("America/Guatemala", -6*3600)
	}
	return z
}()

func (m *Modulo) nuevoVerificarIntento() *auditoria.Caso[EntradaVerificarIntento, VerificacionIntento] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerificarIntento, VerificacionIntento]{
		// Auditada con actor token (cada consulta, también la verificada).
		Accion: "adjuntos.verificar_integracion", Permiso: nucleo.PermAdjuntosVerificarIntegracion, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerificarIntento) (VerificacionIntento, error) {
			actor := contexto.ActorDe(ctx)
			q := pendientesbd.New(t)
			conectorID, err := q.ConectorDeToken(ctx, &actor.TokenID)
			if errors.Is(err, pgx.ErrNoRows) {
				return VerificacionIntento{}, verificacionNoEncontrada()
			}
			if err != nil {
				return VerificacionIntento{}, err
			}
			// Fuera del alcance del conector, lo mismo que una clave que no existe.
			if !empresaEnAlcance(ctx, in.EmpresaID) {
				return VerificacionIntento{}, verificacionNoEncontrada()
			}
			f, err := q.PendientePorClave(ctx, in.IdempotencyKey)
			if errors.Is(err, pgx.ErrNoRows) {
				return VerificacionIntento{}, verificacionNoEncontrada()
			}
			if err != nil {
				return VerificacionIntento{}, err
			}
			if (f.Operacion != OpCambio && f.Operacion != OpNoRecuperado) || f.EmpresaID == nil || *f.EmpresaID != in.EmpresaID {
				return VerificacionIntento{}, verificacionNoEncontrada()
			}
			t.FijarEntidad("pendiente", f.ID)
			t.FijarEmpresa(in.EmpresaID)
			motivo, err := motivoVerificacion(ctx, t, q, f, conectorID)
			if err != nil {
				return VerificacionIntento{}, err
			}
			if motivo == "" {
				return VerificacionIntento{Verificado: true}, nil
			}
			return VerificacionIntento{Motivo: &motivo}, nil
		},
	})
}

// motivoVerificacion: vacío si está verificado.
func motivoVerificacion(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente, conectorID int64) (string, error) {
	switch {
	case f.CodigoError != nil && *f.CodigoError == CodigoIdempotenciaReutilizada:
		return MotivoNoCoincide, nil
	case f.Estado == EstadoBloqueado && f.UltimoError != nil && *f.UltimoError == EsperandoFoto:
		return MotivoFotoPendiente, nil
	case f.Estado == EstadoHecho:
		if f.EnviadoAConector == nil || *f.EnviadoAConector != conectorID {
			return MotivoNoEnviadoPorIris, nil
		}
	case f.Estado == EstadoPendiente || f.Estado == EstadoEnCurso || f.Estado == EstadoAgotado || f.Estado == EstadoBloqueado:
		return MotivoEnvioEnCurso, nil
	default:
		// fallido_negocio o resuelto a mano: Argos nunca respondió 2xx a esta clave desde Iris.
		return MotivoNoEnviadoPorIris, nil
	}
	var cuerpo struct {
		FotoRef string     `json:"foto_ref"`
		Fecha   *time.Time `json:"fecha"`
	}
	_ = json.Unmarshal(f.Payload, &cuerpo)
	if cuerpo.FotoRef == "" {
		if f.Operacion == OpNoRecuperado {
			return MotivoFotoInvalida, nil // un intento siempre lleva foto
		}
		return "", nil
	}
	return fotoVerificable(ctx, t, q, f, cuerpo.FotoRef, cuerpo.Fecha)
}

// fotoVerificable: la foto está completo, es de esa orden y del autor (X-Iris-Actor), se tomó
// después de su llegada y no la usa otro cambio o intento.
func fotoVerificable(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente, foto string, fecha *time.Time) (string, error) {
	id, err := uuid.Parse(foto)
	if err != nil || f.TecnicoUsuarioID == nil || fecha == nil {
		return MotivoFotoInvalida, nil
	}
	a, err := adjuntos.DeEntidad(ctx, t, id, entidadOrdenAdjunto, f.EntidadID)
	if err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) || errors.Is(err, pgx.ErrNoRows) {
			return MotivoFotoInvalida, nil
		}
		return "", err
	}
	switch {
	case a.Estado == adjuntos.EstadoRechazado:
		return MotivoFotoInvalida, nil
	case a.Estado != adjuntos.EstadoCompleto:
		return MotivoFotoPendiente, nil
	case a.SubidoPor == nil || *a.SubidoPor != *f.TecnicoUsuarioID || a.TomadoEn == nil:
		return MotivoFotoInvalida, nil
	}
	l := fecha.In(zonaGuatemala)
	dia := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zonaGuatemala)
	llegada, err := q.LlegadaParaVerificar(ctx, pendientesbd.LlegadaParaVerificarParams{OrdenID: f.EntidadID, ActorID: f.TecnicoUsuarioID,
		Desde: dia, Fecha: *fecha})
	if err != nil {
		return "", err
	}
	// M-2: sin llegada del autor no hay foto posterior a ella. M-3: la foto debió quedar completo
	// dentro de las 72 h desde la fecha.
	if llegada.Year() <= 1 || a.TomadoEn.Before(llegada) {
		return MotivoFotoInvalida, nil
	}
	if a.CompletadoEn == nil || a.CompletadoEn.After(fecha.Add(VentanaFoto)) {
		return MotivoFotoInvalida, nil
	}
	usos, err := q.UsosDeFoto(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return "", err
	}
	if usos > 1 {
		return MotivoFotoInvalida, nil
	}
	return "", nil
}

// empresaEnAlcance: el actor (el token de Argos) opera en esa empresa.
func empresaEnAlcance(ctx context.Context, empresa int64) bool {
	e := contexto.EmpresasDe(ctx)
	if e.Estado == contexto.EmpresaNoResuelta && !contexto.ActorDe(ctx).EsUsuario() {
		return true
	}
	return e.Todas || slices.Contains(e.Disponibles, empresa)
}
