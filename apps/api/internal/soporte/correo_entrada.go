package soporte

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	soportebd "github.com/HCDestroyer/iris/apps/api/internal/soporte/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte/correo"
)

// Lectura de un buzón (job soporte.leer_buzon, único por buzón):
//
//  1. Conecta por IMAP (TLS obligatorio) y selecciona la carpeta.
//  2. Pide los UID nuevos: los mayores que ultimo_uid con la misma UIDVALIDITY («todo lo
//     pendiente hasta ahora»); la primera vez (o si cambió UIDVALIDITY), solo los no leídos, para
//     no importar el historial del buzón.
//  3. Cada correo se guarda en soporte.correos_entrantes (deduplicado por Message-ID y buzón) y
//     en la misma transacción avanza ultimo_uid: tras una caída se reanuda desde ahí.
//  4. Solo después de guardarlo se marca leído (o se mueve) en el servidor.
//  5. Al final encola soporte.procesar_correos del buzón, que los convierte en tickets.

// Límites de lo que se guarda de un correo.
const (
	maxAsuntoCorreo = 500
	maxCuerpoCorreo = 20000
	loteLectura     = 200
)

// EntradaRegistrarCorreo guarda un correo leído (acción interna del worker).
type EntradaRegistrarCorreo struct {
	BuzonID     int64  `json:"buzonId"`
	UIDValidity uint32 `json:"uidValidity"`
	UID         uint32 `json:"uid"`
	MessageID   string `json:"messageId"`
	Remitente   string `json:"remitente"`
	Truncado    bool   `json:"truncado"`
	// AvanzarCursor: en una lectura normal el UID avanza con el guardado. En la primera (solo
	// los no leídos) el cursor se fija al terminar.
	AvanzarCursor bool `json:"avanzarCursor"`

	Correo correo.Correo `json:"-"`
}

// ResultadoRegistrarCorreo: id del correo guardado (0 si ya estaba).
type ResultadoRegistrarCorreo struct {
	ID        int64 `json:"id"`
	Duplicado bool  `json:"duplicado"`
}

// EntradaCerrarLectura cierra una lectura correcta (o anota su error).
type EntradaCerrarLectura struct {
	BuzonID     int64  `json:"buzonId"`
	UIDValidity uint32 `json:"uidValidity"`
	UltimoUID   uint32 `json:"ultimoUid"`
	Error       string `json:"error,omitempty"`
}

// ResultadoLectura de un buzón.
type ResultadoLectura struct {
	Nuevos     int `json:"nuevos"`
	Duplicados int `json:"duplicados"`
}

func (m *Modulo) nuevoRegistrarCorreo() *auditoria.Caso[EntradaRegistrarCorreo, ResultadoRegistrarCorreo] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRegistrarCorreo, ResultadoRegistrarCorreo]{
		Accion: "correos.registrar",
		Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRegistrarCorreo) (ResultadoRegistrarCorreo, error) {
			if err := esSistema(ctx); err != nil {
				return ResultadoRegistrarCorreo{}, err
			}
			q := soportebd.New(t)
			b, err := q.BuzonPorID(ctx, in.BuzonID)
			if err != nil {
				return ResultadoRegistrarCorreo{}, err
			}
			t.FijarEmpresa(deref(b.EmpresaID))
			c := in.Correo
			messageID := correo.Recortar(strings.TrimSpace(c.MessageID), 900)
			if messageID == "" {
				messageID = fmt.Sprintf("sin-message-id.%d.%d@buzon-%d.iris", in.UIDValidity, in.UID, b.ID)
			}
			adjuntos, err := json.Marshal(noNulo(c.Adjuntos))
			if err != nil {
				return ResultadoRegistrarCorreo{}, err
			}
			var inReplyTo *string
			if c.InReplyTo != "" {
				inReplyTo = ptr(correo.Recortar(c.InReplyTo, 900))
			}
			refs := noNulo(c.Referencias)
			if len(refs) > 50 {
				refs = refs[len(refs)-50:]
			}
			id, err := q.RegistrarCorreoEntrante(ctx, soportebd.RegistrarCorreoEntranteParams{
				BuzonID: b.ID, EmpresaID: b.EmpresaID, MessageID: messageID, UidValidity: int64(in.UIDValidity),
				Uid: int64(in.UID), Remitente: correo.Recortar(c.Remitente, 254), RemitenteNombre: correo.Recortar(c.RemitenteNombre, 200),
				Asunto: correo.Recortar(c.Asunto, maxAsuntoCorreo), InReplyTo: inReplyTo, Referencias: refs,
				Cuerpo: correo.Recortar(c.Texto, maxCuerpoCorreo), Adjuntos: adjuntos, Automatico: strPtr(c.Automatico),
				Truncado: in.Truncado, Fecha: c.Fecha,
			})
			duplicado := errors.Is(err, pgx.ErrNoRows)
			if err != nil && !duplicado {
				return ResultadoRegistrarCorreo{}, err
			}
			if in.AvanzarCursor {
				if err := q.AvanzarCursorBuzon(ctx, soportebd.AvanzarCursorBuzonParams{
					ID: b.ID, Uid: int64(in.UID), UidValidity: ptr(int64(in.UIDValidity)),
				}); err != nil {
					return ResultadoRegistrarCorreo{}, err
				}
			}
			if duplicado {
				t.FijarEntidad("buzon", b.ID)
				t.Anotar(map[string]any{"duplicado": true})
				return ResultadoRegistrarCorreo{Duplicado: true}, nil
			}
			t.FijarEntidad("correo_entrante", id)
			return ResultadoRegistrarCorreo{ID: id}, nil
		},
	})
}

func (m *Modulo) nuevoCerrarLectura() *auditoria.Caso[EntradaCerrarLectura, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCerrarLectura, struct{}]{
		Accion: "correos.leer",
		Acceso: auditoria.Autenticado,
		// Una lectura sin novedad no deja fila (corre cada minuto); los errores sí.
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCerrarLectura) (struct{}, error) {
			if err := esSistema(ctx); err != nil {
				return struct{}{}, err
			}
			q := soportebd.New(t)
			t.FijarEntidad("buzon", in.BuzonID)
			b, err := q.BuzonPorID(ctx, in.BuzonID)
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(deref(b.EmpresaID))
			if in.Error != "" {
				t.Anotar(map[string]any{"error": in.Error})
				return struct{}{}, q.ErrorLecturaBuzon(ctx, soportebd.ErrorLecturaBuzonParams{ID: b.ID, UltimoError: &in.Error, Ahora: ptr(m.ahora())})
			}
			if err := q.CerrarLecturaBuzon(ctx, soportebd.CerrarLecturaBuzonParams{
				ID: b.ID, UidValidity: ptr(int64(in.UIDValidity)), UltimoUid: int64(in.UltimoUID), Ahora: ptr(m.ahora()),
			}); err != nil {
				return struct{}{}, err
			}
			hay, err := q.HayCorreosPendientes(ctx, b.ID)
			if err != nil {
				return struct{}{}, err
			}
			if hay && m.cfg.Encolar != nil {
				return struct{}{}, m.cfg.Encolar(ctx, t.Tx, ArgsProcesarCorreos{BuzonID: b.ID})
			}
			return struct{}{}, nil
		},
	})
}

// LeerBuzon trae los correos nuevos del buzón y los guarda (ver arriba). Lo llama el worker
// (actor sistema). Un error de conexión queda en buzones.ultimo_error (resumido) y se devuelve
// para que River reintente.
func (m *Modulo) LeerBuzon(ctx context.Context, buzonID int64) (ResultadoLectura, error) {
	var res ResultadoLectura
	b, err := soportebd.New(nucleo.BDDe(ctx)).BuzonPorID(ctx, buzonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	if !b.Activo {
		return res, nil
	}
	fallar := func(err error) (ResultadoLectura, error) {
		detalle := err.Error()
		var p *nucleo.Problema
		if errors.As(err, &p) {
			detalle = p.Detail
		} else if r := correo.Resumir(err); r != nil {
			detalle = r.Error()
		}
		if _, e := m.CerrarLectura.Ejecutar(ctx, EntradaCerrarLectura{BuzonID: b.ID, Error: correo.Recortar(detalle, 500)}); e != nil {
			return res, errors.Join(err, e)
		}
		return res, err
	}
	srv, _, err := m.servidores(b)
	if err != nil {
		return fallar(err)
	}
	ses, carpeta, err := correo.AbrirIMAP(ctx, m.cfg.Correo.Red, srv, b.ImapCarpeta)
	if err != nil {
		return fallar(err)
	}
	defer ses.Cerrar()

	primera := b.UidValidity == nil || *b.UidValidity != int64(carpeta.UIDValidity)
	desde := uint32(0)
	if !primera {
		desde = uint32(b.UltimoUid) //nolint:gosec // UID de 32 bits guardado en bigint
	}
	uids, err := ses.UIDs(desde, primera)
	if err != nil {
		return fallar(err)
	}
	ultimo := desde
	if primera && carpeta.UIDNext > 0 {
		ultimo = carpeta.UIDNext - 1
	}
	for i, uid := range uids {
		if i >= loteLectura && !primera {
			break // el resto, en la siguiente corrida (el cursor ya avanzó)
		}
		crudo, truncado, err := ses.Traer(uid, m.cfg.Correo.tamanoMax())
		if err != nil {
			return fallar(err)
		}
		if crudo == nil {
			continue
		}
		c, err := correo.Leer(crudo, truncado)
		if err != nil {
			c = correo.Correo{Asunto: "(correo ilegible)", Automatico: "ilegible"}
		}
		r, err := m.RegistrarCorreo.Ejecutar(ctx, EntradaRegistrarCorreo{
			BuzonID: b.ID, UIDValidity: carpeta.UIDValidity, UID: uid, MessageID: c.MessageID,
			Remitente: c.Remitente, Truncado: truncado, AvanzarCursor: !primera, Correo: c,
		})
		if err != nil {
			return fallar(err)
		}
		if r.Duplicado {
			res.Duplicados++
		} else {
			res.Nuevos++
		}
		// Ya está en la base: ahora sí se marca leído (o se mueve) en el servidor.
		if err := ses.Marcar(uid, derefStr(b.ImapCarpetaProcesados)); err != nil {
			return fallar(err)
		}
		ultimo = max(ultimo, uid)
	}
	if _, err := m.CerrarLectura.Ejecutar(ctx, EntradaCerrarLectura{BuzonID: b.ID, UIDValidity: carpeta.UIDValidity, UltimoUID: ultimo}); err != nil {
		return res, err
	}
	return res, nil
}

// EntradaProgramarLecturas: encola la lectura de cada buzón activo (cron).
type EntradaProgramarLecturas struct{}

func (m *Modulo) nuevoProgramarLecturas() *auditoria.Caso[EntradaProgramarLecturas, int] {
	return auditoria.Nuevo(auditoria.Caso[EntradaProgramarLecturas, int]{
		Accion:       "correos.programar",
		Acceso:       auditoria.Autenticado,
		AuditarExito: func(int) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ EntradaProgramarLecturas) (int, error) {
			if err := esSistema(ctx); err != nil {
				return 0, err
			}
			ids, err := soportebd.New(t).BuzonesActivos(ctx)
			if err != nil || m.cfg.Encolar == nil {
				return 0, err
			}
			for _, id := range ids {
				if err := m.cfg.Encolar(ctx, t.Tx, ArgsLeerBuzon{BuzonID: id}); err != nil {
					return 0, err
				}
			}
			return len(ids), nil
		},
	})
}
