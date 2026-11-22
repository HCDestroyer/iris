package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaConsentimiento es POST /mensajeria/consentimientos (§6.4, 0.6.1), con
// Idempotency-Key = argos:consentimiento:<empresa_id>:<cliente_id>:<canal>:<ocurrido_en>: el mismo
// cliente en dos empresas son dos permisos distintos.
type EntradaConsentimiento struct {
	EmpresaID  int64     `json:"empresa_id" minimum:"1" doc:"Empresa en Argos (el permiso es por empresa)"`
	ClienteID  int64     `json:"cliente_id" minimum:"1" doc:"Cliente en Argos"`
	Canal      string    `json:"canal" enum:"whatsapp" doc:"Fase 1: whatsapp"`
	Estado     string    `json:"estado" enum:"acepta,baja"`
	Origen     string    `json:"origen" enum:"argos_panel,argos_portal" doc:"argos_panel: un operador lo marcó; argos_portal: el cliente en su portal"`
	OcurridoEn time.Time `json:"ocurrido_en" doc:"Cuándo lo dio el cliente (RFC 3339). Manda el registro más reciente: un acepta más viejo se guarda, pero no gana a una baja más nueva"`
	// Telefono: 0.10.0 (A42): el número de un contacto del cliente; el permiso queda por número. Con
	// él, la clave lleva el HMAC del número (0.12.1), nunca el número en claro.
	Telefono       *string `json:"telefono,omitempty" pattern:"^\\+[1-9][0-9]{7,14}$" doc:"Opcional (contrato 0.10.0): el número E.164 de un contacto del cliente; el permiso es de ese número. Con él, la Idempotency-Key es argos:consentimiento:<empresa_id>:<cliente_id>:whatsapp:<h>:<ocurrido_en>, con h = 16 hex del HMAC-SHA256 del número (0.12.1)"`
	IdempotencyKey string  `json:"-"`
}

// reClaveConsentimientoNumero: la forma de la clave con número (0.12.1): el número nunca va en ella.
var reClaveConsentimientoNumero = regexp.MustCompile(`^argos:consentimiento:[0-9]+:[0-9]+:whatsapp:[0-9a-f]{16}:.+$`)

// MarshalJSON: en la auditoría, el número enmascarado (+502 ****-3344, §3.5).
func (e EntradaConsentimiento) MarshalJSON() ([]byte, error) {
	type sinMetodos EntradaConsentimiento
	x := sinMetodos(e)
	if x.Telefono != nil {
		v := enmascararNumero(*x.Telefono)
		x.Telefono = &v
	}
	return json.Marshal(x)
}

// enmascararNumero deja solo los últimos 4 dígitos.
func enmascararNumero(n string) string {
	if len(n) <= 4 {
		return "****"
	}
	return "****" + n[len(n)-4:]
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
	// Telefono: solo con permiso por número (omitempty: la huella de los demás no cambia).
	Telefono string `json:"telefono,omitempty"`
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
			if in.Telefono != nil {
				if in.Canal != CanalWhatsApp {
					return struct{}{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "telefono", Message: "Solo con canal whatsapp", Rule: "canal"})
				}
				// 0.12.1: con número, la clave lleva su HMAC y nunca el número en claro.
				if !reClaveConsentimientoNumero.MatchString(in.IdempotencyKey) || strings.Contains(in.IdempotencyKey, soloDigitos(*in.Telefono)) {
					return struct{}{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "Idempotency-Key", Message: "Con telefono, la clave lleva el HMAC del número (contrato 0.12.1)", Rule: "formato"})
				}
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
			if in.Telefono != nil {
				// El número debe ser de un contacto del cliente en la caché de Argos (§3.5).
				es, err := mensajeriabd.New(t).NumeroDeContactoDelCliente(ctx, mensajeriabd.NumeroDeContactoDelClienteParams{Numero: *in.Telefono,
					ClienteID: in.ClienteID, Empresa: in.EmpresaID})
				if err != nil {
					return resultadoConsentimiento{}, err
				}
				if !es {
					return resultadoConsentimiento{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "numero_no_es_contacto", "El número no es de un contacto",
						"El número no está entre los contactos del cliente que Iris conoce de Argos", nucleo.ErrorCampo{Field: "telefono", Message: "No es de un contacto del cliente", Rule: "contacto"})
				}
			}
			t.FijarEmpresa(in.EmpresaID)
			hecho := hechoConsentimiento{EmpresaID: in.EmpresaID, ClienteID: in.ClienteID, Canal: in.Canal, Estado: in.Estado, Origen: in.Origen,
				OcurridoEn: in.OcurridoEn.UTC().Format(time.RFC3339Nano)}
			if in.Telefono != nil {
				hecho.Telefono = *in.Telefono
			}
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
	if in.Telefono != nil {
		// Antes de exigir que el número sea de un contacto, Iris relee los contactos del cliente si no
		// lo encuentra (X-Iris-Actor «sistema», 0.14.2): un contacto recién creado en Argos aún no está.
		k.m.asegurarNumeroDeContacto(ctx, in.EmpresaID, in.ClienteID, *in.Telefono)
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
	var telefono *string
	if h.Telefono != "" {
		telefono = &h.Telefono
	}
	f, err := q.InsertarConsentimiento(ctx, mensajeriabd.InsertarConsentimientoParams{EmpresaID: h.EmpresaID, ClienteID: h.ClienteID,
		Canal: h.Canal, Telefono: telefono, Estado: h.Estado, Origen: h.Origen, OcurridoEn: ocurrido, RegistradoPor: usuario, IdempotencyKey: clave,
		HashPeticion: hash, EntranteID: entrante})
	if errors.Is(err, pgx.ErrNoRows) {
		// El mismo hecho ya estaba (otra clave o sin ella): es una repetición.
		previo, err := q.ConsentimientoPorHecho(ctx, mensajeriabd.ConsentimientoPorHechoParams{EmpresaID: h.EmpresaID, ClienteID: h.ClienteID,
			Canal: h.Canal, Telefono: telefono, OcurridoEn: ocurrido, Estado: h.Estado, Origen: h.Origen})
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
	auditado := h
	if auditado.Telefono != "" {
		auditado.Telefono = enmascararNumero(auditado.Telefono)
	}
	t.RegistrarCambios("consentimiento", f.ID, nil, auditado)
	// Nunca el número (§6.4): por_numero dice si el permiso es de un número de contacto.
	if err := t.Emitir("consentimiento.registrado", map[string]any{"consentimiento_id": f.ID, "empresa_id": f.EmpresaID, "cliente_id": f.ClienteID,
		"canal": f.Canal, "estado": f.Estado, "origen": f.Origen, "ocurrido_en": f.OcurridoEn, "estado_vigente": r.EstadoVigente,
		"por_numero": f.Telefono != nil}); err != nil {
		return r, err
	}
	if f.Telefono == nil && f.Estado == "baja" && r.EstadoVigente == "baja" && (f.Origen == "cliente" || f.Origen == "agente") {
		// Una baja que se da en Iris (chat o agente) va a Argos (§6.4 y §6.6).
		if err := t.Emitir("contacto.baja", map[string]any{"cliente_id": f.ClienteID, "empresa_id": f.EmpresaID, "canal": f.Canal,
			"ocurrido_en": f.OcurridoEn, "origen": f.Origen, "consentimiento_id": f.ID}); err != nil {
			return r, err
		}
	}
	return r, nil
}

func (m *Modulo) vigenteDe(ctx context.Context, q *mensajeriabd.Queries, f mensajeriabd.MensajeriaConsentimiento, nuevo bool) (resultadoConsentimiento, error) {
	var v mensajeriabd.MensajeriaConsentimiento
	var err error
	if f.Telefono != nil {
		v, err = q.ConsentimientoVigenteNumero(ctx, mensajeriabd.ConsentimientoVigenteNumeroParams{EmpresaID: f.EmpresaID, ClienteID: f.ClienteID,
			Canal: f.Canal, Telefono: *f.Telefono})
	} else {
		v, err = q.ConsentimientoVigente(ctx, mensajeriabd.ConsentimientoVigenteParams{EmpresaID: f.EmpresaID, ClienteID: f.ClienteID, Canal: f.Canal})
	}
	if err != nil {
		return resultadoConsentimiento{}, err
	}
	return resultadoConsentimiento{Consentimiento: Consentimiento{ConsentimientoID: f.ID, EstadoVigente: v.Estado}, Nuevo: nuevo}, nil
}

// asegurarNumeroDeContacto relee los contactos del cliente si el número no está en la caché. De mejor
// esfuerzo: si Argos no responde, decide la caché.
func (m *Modulo) asegurarNumeroDeContacto(ctx context.Context, empresa, cliente int64, numero string) {
	ac, ok := m.cfg.Argos.(ArgosContactos)
	bd := nucleo.BDDe(ctx)
	if m.cfg.Argos == nil || !ok || bd == nil {
		return
	}
	es, err := mensajeriabd.New(bd).NumeroDeContactoDelCliente(ctx, mensajeriabd.NumeroDeContactoDelClienteParams{Numero: numero, ClienteID: cliente, Empresa: empresa})
	if err != nil || es {
		return
	}
	_, _ = ac.RefrescarContactos(ctx, argos.EntradaRefrescarContactos{ClienteID: cliente, Forzar: true})
}
