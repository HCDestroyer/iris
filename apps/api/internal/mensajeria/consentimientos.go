package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaConsentimiento es POST /mensajeria/consentimientos (§6.4, 0.6.1), con
// Idempotency-Key = argos:consentimiento:<empresa_id>:<cliente_id>:<canal>:<ocurrido_en>: el mismo
// cliente en dos empresas son dos permisos distintos.
type EntradaConsentimiento struct {
	EmpresaID      int64     `json:"empresa_id" minimum:"1" doc:"Empresa en Argos (el permiso es por empresa)"`
	ClienteID      int64     `json:"cliente_id" minimum:"1" doc:"Cliente en Argos"`
	Canal          string    `json:"canal" enum:"whatsapp" doc:"Fase 1: whatsapp"`
	Estado         string    `json:"estado" enum:"acepta,baja"`
	Origen         string    `json:"origen" enum:"argos_panel,argos_portal" doc:"argos_panel: un operador lo marcó; argos_portal: el cliente en su portal"`
	OcurridoEn     time.Time `json:"ocurrido_en" doc:"Cuándo lo dio el cliente (RFC 3339). Manda el registro más reciente: un acepta más viejo se guarda, pero no gana a una baja más nueva"`
	IdempotencyKey string    `json:"-"`
}

// Consentimiento es la respuesta (201 nuevo, 200 repetición).
type Consentimiento struct {
	ConsentimientoID int64  `json:"consentimiento_id"`
	EstadoVigente    string `json:"estado_vigente" enum:"acepta,baja" doc:"El del registro más reciente (por ocurrido_en) del cliente en la empresa y el canal"`
}

type resultadoConsentimiento struct {
	Consentimiento
	Nuevo bool `json:"nuevo"`
}

// hechoConsentimiento: lo que identifica la petición (para comparar una clave repetida).
type hechoConsentimiento struct {
	EmpresaID  int64  `json:"empresa_id"`
	ClienteID  int64  `json:"cliente_id"`
	Canal      string `json:"canal"`
	Estado     string `json:"estado"`
	Origen     string `json:"origen"`
	OcurridoEn string `json:"ocurrido_en"`
}

// RegistrarConsentimiento registra un permiso de contacto en dos casos de uso, sin transacción
// abierta mientras se consulta Argos: el previo valida permiso, entrada y empresa; luego se lee el
// cliente de la instantánea (refrescada si tiene más de 24 h) y el final guarda el registro.
type RegistrarConsentimiento struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaConsentimiento, struct{}]
	final  *auditoria.Caso[entradaFinalConsentimiento, resultadoConsentimiento]
}

type entradaFinalConsentimiento struct {
	EntradaConsentimiento
	fuera bool
}

// MarshalJSON: lo auditado es la entrada.
func (e entradaFinalConsentimiento) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.EntradaConsentimiento)
}

func nuevoRegistrarConsentimiento(m *Modulo) *RegistrarConsentimiento {
	const accion = "mensajeria.registrar_consentimiento"
	k := &RegistrarConsentimiento{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaConsentimiento, struct{}]{
		Accion: accion, Permiso: nucleo.PermMensajeriaRegistrarConsentimiento, SinEmpresa: true,
		AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConsentimiento) (struct{}, error) {
			if in.IdempotencyKey == "" {
				return struct{}{}, claveRequerida()
			}
			if in.OcurridoEn.After(m.ahora().Add(5 * time.Minute)) {
				return struct{}{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "ocurrido_en", Message: "No puede ser futuro", Rule: "pasado"})
			}
			return struct{}{}, exigirEmpresa(ctx, t, in.EmpresaID)
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[entradaFinalConsentimiento, resultadoConsentimiento]{
		Accion: accion, Permiso: nucleo.PermMensajeriaRegistrarConsentimiento, SinEmpresa: true,
		// Una repetición no deja otra fila.
		AuditarExito: func(r resultadoConsentimiento) bool { return r.Nuevo },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaFinalConsentimiento) (resultadoConsentimiento, error) {
			if in.fuera {
				return resultadoConsentimiento{}, clienteFueraDeEmpresa()
			}
			t.FijarEmpresa(in.EmpresaID)
			hecho := hechoConsentimiento{EmpresaID: in.EmpresaID, ClienteID: in.ClienteID, Canal: in.Canal, Estado: in.Estado, Origen: in.Origen,
				OcurridoEn: in.OcurridoEn.UTC().Format(time.RFC3339Nano)}
			return m.guardarConsentimiento(ctx, t, hecho, &in.IdempotencyKey, nil)
		},
	})
	return k
}

// Ejecutar registra el permiso.
func (k *RegistrarConsentimiento) Ejecutar(ctx context.Context, in EntradaConsentimiento) (resultadoConsentimiento, error) {
	if _, err := k.previo.Ejecutar(ctx, in); err != nil {
		return resultadoConsentimiento{}, err
	}
	fuera, err := k.m.clienteFueraDeEmpresa(ctx, in.ClienteID, in.EmpresaID)
	if err != nil {
		return resultadoConsentimiento{}, err
	}
	return k.final.Ejecutar(ctx, entradaFinalConsentimiento{EntradaConsentimiento: in, fuera: fuera})
}

// clienteFueraDeEmpresa: según la instantánea de Argos, el cliente no existe o es de otra empresa
// (422 cliente_fuera_de_empresa). Si Argos no responde y no hay instantánea, se acepta: el permiso
// lo manda Argos de un cliente suyo, y el cliente se verifica de nuevo al pedir cada aviso.
func (m *Modulo) clienteFueraDeEmpresa(ctx context.Context, cliente, empresa int64) (bool, error) {
	if m.cfg.Contactos == nil {
		return false, nil
	}
	c, err := m.cfg.Contactos.ContactoCliente(ctx, cliente)
	if err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) && p.Status >= 500 {
			return false, nil
		}
		return false, err
	}
	return !c.Existe || !c.EnEmpresa(empresa), nil
}

// guardarConsentimiento inserta el registro (idempotente por clave y por hecho), calcula el vigente
// y emite consentimiento.registrado; con una baja que queda vigente y vino del chat, contacto.baja.
func (m *Modulo) guardarConsentimiento(ctx context.Context, t *auditoria.Tx, h hechoConsentimiento, clave *string, entrante *int64) (resultadoConsentimiento, error) {
	q := mensajeriabd.New(t)
	if err := q.BloquearConsentimientos(ctx, mensajeriabd.BloquearConsentimientosParams{EmpresaID: h.EmpresaID, ClienteID: h.ClienteID, Canal: h.Canal}); err != nil {
		return resultadoConsentimiento{}, err
	}
	hash := hashDe(h)
	usuario := usuarioDe(ctx)
	if clave != nil && usuario != nil {
		previo, err := q.ConsentimientoPorClave(ctx, mensajeriabd.ConsentimientoPorClaveParams{RegistradoPor: *usuario, IdempotencyKey: *clave})
		switch {
		case err == nil:
			if !bytes.Equal(previo.HashPeticion, hash) {
				return resultadoConsentimiento{}, idempotenciaReutilizada()
			}
			return m.vigenteDe(ctx, q, previo, false)
		case !errors.Is(err, pgx.ErrNoRows):
			return resultadoConsentimiento{}, err
		}
	}
	ocurrido, _ := time.Parse(time.RFC3339Nano, h.OcurridoEn)
	f, err := q.InsertarConsentimiento(ctx, mensajeriabd.InsertarConsentimientoParams{EmpresaID: h.EmpresaID, ClienteID: h.ClienteID,
		Canal: h.Canal, Estado: h.Estado, Origen: h.Origen, OcurridoEn: ocurrido, RegistradoPor: usuario, IdempotencyKey: clave,
		HashPeticion: hash, EntranteID: entrante})
	if errors.Is(err, pgx.ErrNoRows) {
		// El mismo hecho ya estaba (otra clave o sin ella): es una repetición.
		previo, err := q.ConsentimientoPorHecho(ctx, mensajeriabd.ConsentimientoPorHechoParams{EmpresaID: h.EmpresaID, ClienteID: h.ClienteID,
			Canal: h.Canal, OcurridoEn: ocurrido, Estado: h.Estado, Origen: h.Origen})
		if err != nil {
			return resultadoConsentimiento{}, err
		}
		return m.vigenteDe(ctx, q, previo, false)
	}
	if err != nil {
		return resultadoConsentimiento{}, err
	}
	r, err := m.vigenteDe(ctx, q, f, true)
	if err != nil {
		return r, err
	}
	t.FijarEntidad("consentimiento", f.ID)
	t.RegistrarCambios("consentimiento", f.ID, nil, h)
	if err := t.Emitir("consentimiento.registrado", map[string]any{"consentimiento_id": f.ID, "empresa_id": f.EmpresaID, "cliente_id": f.ClienteID,
		"canal": f.Canal, "estado": f.Estado, "origen": f.Origen, "ocurrido_en": f.OcurridoEn, "estado_vigente": r.EstadoVigente}); err != nil {
		return r, err
	}
	if f.Estado == "baja" && r.EstadoVigente == "baja" && (f.Origen == "cliente" || f.Origen == "agente") {
		// Una baja que se da en Iris (chat o agente) va a Argos (§6.4 y §6.6).
		if err := t.Emitir("contacto.baja", map[string]any{"cliente_id": f.ClienteID, "empresa_id": f.EmpresaID, "canal": f.Canal,
			"ocurrido_en": f.OcurridoEn, "origen": f.Origen, "consentimiento_id": f.ID}); err != nil {
			return r, err
		}
	}
	return r, nil
}

func (m *Modulo) vigenteDe(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaConsentimiento, nuevo bool) (resultadoConsentimiento, error) {
	v, err := q.ConsentimientoVigente(ctx, mensajeriabd.ConsentimientoVigenteParams{EmpresaID: f.EmpresaID, ClienteID: f.ClienteID, Canal: f.Canal})
	if err != nil {
		return resultadoConsentimiento{}, err
	}
	return resultadoConsentimiento{Consentimiento: Consentimiento{ConsentimientoID: f.ID, EstadoVigente: v.Estado}, Nuevo: nuevo}, nil
}
