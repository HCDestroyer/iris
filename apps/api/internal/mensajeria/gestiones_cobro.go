package mensajeria

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Gestiones de cobro (contrato Iris ↔ ArgosISP 0.13.0 §3.12, pedido B31). Un agente (o el asistente de
// IA) registra lo que pasó al cobrar desde la conversación: una llamada, un mensaje, una visita o una
// promesa, con un resultado de lista cerrada. Iris la guarda y la manda a Argos por la cola de
// pendientes (POST /clientes/:id/gestiones-cobro, Idempotency-Key iris:gestion:<gestion_id>), con
// X-Iris-Actor y, si habló un contacto del cliente, X-Iris-Contacto. Es solo informativa: nunca cambia
// estados, cortes ni promesas en Argos, y nunca lleva texto libre, montos ni teléfonos.

// ContratoGestionesCobro: la versión del contrato desde la que Argos recibe gestiones de cobro.
const ContratoGestionesCobro = "0.13.0"

// OpGestionCobro es la operación del pendiente hacia Argos.
const OpGestionCobro = "registrar_gestion_cobro"

// Ventana de ocurrido_en (§3.12): entre ahora − 7 días y ahora + 5 min.
const (
	GestionAtrasMaximo    = 7 * 24 * time.Hour
	GestionAdelanteMaximo = 5 * time.Minute
)

// Listas cerradas de §3.12.
var (
	TiposGestion      = []string{"llamada", "mensaje", "visita", "promesa"}
	ResultadosGestion = []string{"contesto", "no_contesto", "numero_equivocado", "promete_pagar", "rechaza", "pago_informado", "visita_sin_contacto"}
)

// resultadosConRespaldo: con el asistente de IA solo valen si hay una promesa o un comprobante real en
// la conversación (§3.12; lo valida Iris antes de mandarla).
var resultadosConRespaldo = map[string]bool{"promete_pagar": true, "pago_informado": true}

// EntradaGestionCobro es POST /conversaciones/{id}/gestiones-cobro.
type EntradaGestionCobro struct {
	ID         int64     `json:"-"`
	Tipo       string    `json:"tipo" enum:"llamada,mensaje,visita,promesa"`
	Resultado  string    `json:"resultado" enum:"contesto,no_contesto,numero_equivocado,promete_pagar,rechaza,pago_informado,visita_sin_contacto"`
	OcurridoEn time.Time `json:"ocurridoEn" doc:"Cuándo ocurrió (entre ahora − 7 días y ahora + 5 min)"`
	ServicioID *int64    `json:"servicioId,omitempty" minimum:"1" doc:"Servicio del cliente (opcional)"`
	Clave      string    `json:"-"`
}

// GestionCobro es una gestión registrada y cómo va su envío a Argos.
type GestionCobro struct {
	ID             uuid.UUID `json:"id" doc:"gestion_id de Iris (UUID v7); la clave hacia Argos es iris:gestion:<id>"`
	ConversacionID int64     `json:"conversacionId"`
	ClienteID      int64     `json:"clienteId"`
	ServicioID     *int64    `json:"servicioId" nullable:"true"`
	ContactoID     *string   `json:"contactoId" nullable:"true" doc:"El contacto del cliente en Argos que habla en la conversación (salió como X-Iris-Contacto); null = el titular"`
	Tipo           string    `json:"tipo" enum:"llamada,mensaje,visita,promesa"`
	Resultado      string    `json:"resultado" enum:"contesto,no_contesto,numero_equivocado,promete_pagar,rechaza,pago_informado,visita_sin_contacto"`
	OcurridoEn     time.Time `json:"ocurridoEn"`
	UsuarioID      *int64    `json:"usuarioId" nullable:"true" doc:"Quién la registró (null = el asistente de IA)"`
	Envio          string    `json:"envio" enum:"pendiente,enviada,fallida,resuelta_manual" doc:"pendiente: en la cola hacia Argos; enviada: Argos la registró; fallida: Argos la rechazó o se agotaron los reintentos; resuelta_manual: una persona cerró el pendiente"`
	CodigoError    *string   `json:"codigoError" nullable:"true" doc:"El tipo de error de Argos cuando falló"`
	CreadaEn       time.Time `json:"creadaEn"`
	Repetida       bool      `json:"repetida,omitempty" doc:"La Idempotency-Key ya se había usado con esta misma petición"`
}

// ListaGestionesCobro es { data } (las 100 más recientes).
type ListaGestionesCobro struct {
	Data []GestionCobro `json:"data"`
}

type entradaGestionInterna struct {
	EntradaGestionCobro
	hash []byte
}

// MarshalJSON: lo auditado (sin nada que no esté en el cuerpo).
func (e entradaGestionInterna) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"conversacion_id": e.ID, "tipo": e.Tipo, "resultado": e.Resultado, "ocurrido_en": e.OcurridoEn,
		"servicio_id": e.ServicioID})
}

func envioDe(estado, codigo *string) (string, *string) {
	switch {
	case estado == nil:
		return "pendiente", nil
	case *estado == pendientes.EstadoHecho:
		return "enviada", nil
	case *estado == pendientes.EstadoFallidoNegocio || *estado == pendientes.EstadoAgotado:
		return "fallida", codigo
	case *estado == pendientes.EstadoResueltoManual:
		return "resuelta_manual", nil
	}
	return "pendiente", nil
}

func aGestion(f mensajeriabd.MensajeriaGestionesCobro, estado, codigo *string) GestionCobro {
	g := GestionCobro{ID: f.ID, ConversacionID: f.ConversacionID, ClienteID: f.ClienteID, ServicioID: f.ServicioID, Tipo: f.Tipo,
		Resultado: f.Resultado, OcurridoEn: f.OcurridoEn, UsuarioID: f.UsuarioID, CreadaEn: f.CreatedAt}
	if f.ContactoID != nil {
		s := strconv.FormatInt(*f.ContactoID, 10)
		g.ContactoID = &s
	}
	g.Envio, g.CodigoError = envioDe(estado, codigo)
	return g
}

// validarGestion: las listas cerradas y la ventana de ocurrido_en (§3.12), antes de abrir la transacción.
func validarGestion(in EntradaGestionCobro, ahora time.Time) error {
	var errs []nucleo.ErrorCampo
	if !slices.Contains(TiposGestion, in.Tipo) {
		errs = append(errs, nucleo.ErrorCampo{Field: "tipo", Message: "Debe ser llamada, mensaje, visita o promesa", Rule: "enum"})
	}
	if !slices.Contains(ResultadosGestion, in.Resultado) {
		errs = append(errs, nucleo.ErrorCampo{Field: "resultado", Message: "Resultado fuera de la lista", Rule: "enum"})
	}
	if in.OcurridoEn.IsZero() || in.OcurridoEn.Before(ahora.Add(-GestionAtrasMaximo)) || in.OcurridoEn.After(ahora.Add(GestionAdelanteMaximo)) {
		errs = append(errs, nucleo.ErrorCampo{Field: "ocurridoEn", Message: "Debe estar entre hace 7 días y dentro de 5 minutos", Rule: "ventana"})
	}
	if len(errs) > 0 {
		return nucleo.Validacion(errs...)
	}
	return nil
}

func (m *Modulo) nuevoRegistrarGestionCobro() *auditoria.Caso[entradaGestionInterna, GestionCobro] {
	const caso = "conversaciones.registrar_gestion_cobro"
	return auditoria.Nuevo(auditoria.Caso[entradaGestionInterna, GestionCobro]{
		Accion: caso, Permiso: nucleo.PermConversacionesRegistrarGestionCobro,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGestionInterna) (GestionCobro, error) {
			a, err := alcanceAccionDe(ctx, caso)
			if err != nil {
				return GestionCobro{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return GestionCobro{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("conversacion", c.ID)
			if in.Clave != "" {
				f, err := q.GestionCobroPorClave(ctx, mensajeriabd.GestionCobroPorClaveParams{ConversacionID: c.ID, Usuario: a.usuario, IdempotencyKey: &in.Clave})
				switch {
				case err == nil:
					if string(f.HashPeticion) != string(in.hash) {
						return GestionCobro{}, idempotenciaReutilizada()
					}
					g := aGestion(f, nil, nil)
					if f.PendienteID != nil {
						if p, err := pendientes.PorID(ctx, t, *f.PendienteID); err == nil {
							g.Envio, g.CodigoError = envioDe(&p.Estado, p.CodigoError)
						}
					}
					g.Repetida = true
					return g, nil
				case !errors.Is(err, pgx.ErrNoRows):
					return GestionCobro{}, err
				}
			}
			// Solo con el cliente confirmado: nunca para un número sin cliente (§3.12).
			if c.ClienteID == nil {
				return GestionCobro{}, reglaConv("sin_cliente", "Asocia la conversación a un cliente de Argos antes de registrar una gestión de cobro")
			}
			if !c.ClienteVerificado {
				return GestionCobro{}, clienteSinConfirmar()
			}
			if c.EmpresaID == nil {
				return GestionCobro{}, reglaConv("sin_empresa", "La conversación no es de una empresa de Argos")
			}
			contrato, err := q.ContratoArgosActivo(ctx)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return GestionCobro{}, nucleo.Conflicto("argos_apagado", "El conector con Argos está apagado en Iris")
			case err != nil:
				return GestionCobro{}, err
			case !conector.VersionAlMenos(conector.ContratoAcordado(contrato.ContratoRemoto, contrato.ContratoInformado), ContratoGestionesCobro):
				return GestionCobro{}, nucleo.Conflicto("argos_sin_gestiones",
					"El Argos conectado todavía no recibe gestiones de cobro (necesita el contrato "+ContratoGestionesCobro+" o posterior)")
			}
			// Quién habla sale de la conversación (su número), nunca del cuerpo (§3.12, §7.1).
			quien, err := m.contactoDeGestion(ctx, q, c, in.ServicioID)
			if err != nil {
				return GestionCobro{}, err
			}
			in.ServicioID = quien.ServicioID
			contacto := quien.ContactoID
			if in.ServicioID != nil {
				ok, err := q.ServicioDelClienteInstantanea(ctx, mensajeriabd.ServicioDelClienteInstantaneaParams{ServicioID: *in.ServicioID, ClienteID: quien.ClienteID,
					Empresa: *c.EmpresaID})
				if err != nil {
					return GestionCobro{}, err
				}
				if !ok {
					return GestionCobro{}, servicioNoDelCliente()
				}
			}
			// El asistente solo informa un pago o una promesa si hay una promesa o un comprobante real en
			// la conversación (§3.12).
			if a.asistente && resultadosConRespaldo[in.Resultado] {
				ok, err := q.RespaldoPagoEnConversacion(ctx, mensajeriabd.RespaldoPagoEnConversacionParams{ConversacionID: c.ID, ClienteID: quien.ClienteID})
				if err != nil {
					return GestionCobro{}, err
				}
				if !ok {
					return GestionCobro{}, reglaConv("sin_respaldo", "El asistente solo registra un pago o una promesa si hay una promesa o un comprobante en la conversación")
				}
			}
			id, err := uuid.NewV7()
			if err != nil {
				return GestionCobro{}, err
			}
			params := mensajeriabd.InsertarGestionCobroParams{ID: id, EmpresaID: *c.EmpresaID, ConversacionID: c.ID, ClienteID: quien.ClienteID,
				ServicioID: in.ServicioID, ContactoID: contacto, Tipo: in.Tipo, Resultado: in.Resultado, OcurridoEn: in.OcurridoEn.UTC(),
				OrigenTipo: "conversacion", OrigenID: origenConversacion(c.ID), UsuarioID: a.usuarioPtr(), Asistente: a.asistente, HashPeticion: in.hash}
			if in.Clave != "" {
				params.IdempotencyKey = &in.Clave
			}
			f, err := q.InsertarGestionCobro(ctx, params)
			if errors.Is(err, pgx.ErrNoRows) {
				return GestionCobro{}, nucleo.Conflicto("idempotencia_en_curso", "Una petición con esta clave sigue en curso; vuelve a intentar")
			}
			if err != nil {
				return GestionCobro{}, err
			}
			cuerpo := map[string]any{"tipo": f.Tipo, "resultado": f.Resultado, "ocurrido_en": f.OcurridoEn.UTC().Format(time.RFC3339),
				"origen_tipo": f.OrigenTipo, "origen_id": f.OrigenID}
			if f.ServicioID != nil {
				cuerpo["servicio_id"] = *f.ServicioID
			}
			crudo, err := json.Marshal(cuerpo)
			if err != nil {
				return GestionCobro{}, err
			}
			accion := pendientes.Accion{EmpresaID: c.EmpresaID, Operacion: OpGestionCobro, IdempotencyKey: "iris:gestion:" + f.ID.String(),
				Entidad: "gestion_cobro", EntidadID: f.ID.String(), Ruta: "/clientes/" + strconv.FormatInt(f.ClienteID, 10) + "/gestiones-cobro",
				Cuerpo: crudo, Actor: actorGestion(a)}
			// Solo si quien habla es un contacto (también a cargo); el titular va sin la cabecera.
			accion.Contacto = quien.Contacto
			var aviso pendientes.Encolador
			if m.cfg.Encolar != nil {
				aviso = pendientes.Encolador(m.cfg.Encolar)
			}
			p, _, err := pendientes.Encolar(ctx, t, accion, aviso)
			if err != nil {
				return GestionCobro{}, err
			}
			if err := q.FijarPendienteGestionCobro(ctx, mensajeriabd.FijarPendienteGestionCobroParams{PendienteID: &p.ID, ID: f.ID}); err != nil {
				return GestionCobro{}, err
			}
			t.RegistrarCambios("gestion_cobro", 0, nil, map[string]any{"gestion_id": f.ID, "tipo": f.Tipo, "resultado": f.Resultado,
				"servicio_id": f.ServicioID, "contacto_id": f.ContactoID, "pendiente_id": p.ID})
			if err := t.Emitir("gestion_cobro.registrada", map[string]any{"gestion_id": f.ID, "conversacion_id": c.ID, "empresa_id": f.EmpresaID,
				"cliente_id": f.ClienteID, "servicio_id": f.ServicioID, "tipo": f.Tipo, "resultado": f.Resultado,
				"origen": origenActorGestion(a), "pendiente_id": p.ID}); err != nil {
				return GestionCobro{}, err
			}
			return aGestion(f, &p.Estado, p.CodigoError), nil
		},
	})
}

// origenConversacion: el origen_id que ve Argos (el id de la conversación en Iris, como §3.12: «c-8812»).
func origenConversacion(id int64) string { return "c-" + strconv.FormatInt(id, 10) }

// actorGestion: X-Iris-Actor (§7.1).
func actorGestion(a alcanceConv) string {
	if a.asistente {
		return ActorAsistenteIA
	}
	return pendientes.ActorUsuario(a.usuario)
}

func origenActorGestion(a alcanceConv) string {
	if a.asistente {
		return "asistente"
	}
	return "persona"
}

func (m *Modulo) nuevoListarGestionesCobro() *auditoria.Caso[EntradaConversacion, ListaGestionesCobro] {
	const caso = "conversaciones.ver_gestiones_cobro"
	return auditoria.Nuevo(auditoria.Caso[EntradaConversacion, ListaGestionesCobro]{
		Accion: caso, Permiso: nucleo.PermConversacionesVer, AuditarExito: sinAuditar[ListaGestionesCobro],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConversacion) (ListaGestionesCobro, error) {
			a, err := alcanceConvDe(ctx)
			if err != nil {
				return ListaGestionesCobro{}, err
			}
			q := mensajeriabd.New(t)
			c, err := convVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return ListaGestionesCobro{}, err
			}
			fs, err := q.GestionesCobroDeConversacion(ctx, c.ID)
			if err != nil {
				return ListaGestionesCobro{}, err
			}
			out := ListaGestionesCobro{Data: make([]GestionCobro, 0, len(fs))}
			for _, f := range fs {
				out.Data = append(out.Data, aGestion(mensajeriabd.MensajeriaGestionesCobro{ID: f.ID, EmpresaID: f.EmpresaID,
					ConversacionID: f.ConversacionID, ClienteID: f.ClienteID, ServicioID: f.ServicioID, ContactoID: f.ContactoID, Tipo: f.Tipo,
					Resultado: f.Resultado, OcurridoEn: f.OcurridoEn, UsuarioID: f.UsuarioID, CreatedAt: f.CreatedAt}, f.PendienteEstado, f.PendienteCodigo))
			}
			return out, nil
		},
	})
}

// RegistrarGestionCobro valida, arma la huella y corre el caso.
func (m *Modulo) RegistrarGestionCobro(ctx context.Context, in EntradaGestionCobro) (GestionCobro, error) {
	if err := validarGestion(in, m.ahora()); err != nil {
		return GestionCobro{}, err
	}
	in.OcurridoEn = in.OcurridoEn.UTC()
	b, _ := json.Marshal(in)
	h := sha256.Sum256(append(b, []byte(strconv.FormatInt(in.ID, 10))...))
	return m.GestionesCobro.registrar.Ejecutar(ctx, entradaGestionInterna{EntradaGestionCobro: in, hash: h[:]})
}

// GestionesCobro agrupa los casos.
type GestionesCobro struct {
	registrar *auditoria.Caso[entradaGestionInterna, GestionCobro]
	Listar    *auditoria.Caso[EntradaConversacion, ListaGestionesCobro]
}

func (m *Modulo) registrarRutasGestionesCobro(api huma.API, base string) {
	reg := op("registrarGestionCobroConversacion", http.MethodPost, base+"/{id}/gestiones-cobro",
		"Registrar en Argos una gestión de cobro (contrato con Argos 0.13.0 §3.12)", nucleo.PermConversacionesRegistrarGestionCobro,
		http.StatusCreated, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict)
	reg.Description += " Solo informativa: nunca cambia estados, cortes ni promesas en Argos. Sin texto libre, montos ni teléfonos. " +
		"Solo con el cliente confirmado (422 `sin_cliente`, `cliente_sin_confirmar`). `ocurridoEn` entre ahora − 7 días y ahora + 5 min " +
		"(422 `validacion`, regla `ventana`). Sale a Argos por la cola de pendientes (`iris:gestion:<id>`, X-Iris-Actor y, si quien habla es un contacto del cliente, " +
		"X-Iris-Contacto). 409 `argos_apagado` y `argos_sin_gestiones` (Argos con contrato anterior a 0.13.0). Con el asistente de IA, " +
		"`promete_pagar` y `pago_informado` exigen una promesa o un comprobante en la conversación (422 `sin_respaldo`). Quien habla sale del rol de la conversación: " +
		"sin rol, 422 `cliente_sin_confirmar`; un contacto necesita el permiso «pagar» (403 `permiso_de_contacto`), solo sus servicios (403 `servicio_fuera_del_contacto`) " +
		"y, si sus servicios están acotados, el servicio (el único suyo o 422 en `servicioId`). Un servicio de otro cliente a cargo de quien habla (§3.8, B25) " +
		"va a nombre de ese cliente, con X-Iris-Contacto = su contacto en él y el permiso «pagar» de ese vínculo. Idempotency-Key obligatoria (400 `idempotency_key_requerida`)."
	huma.Register(api, reg, func(ctx context.Context, in *idCuerpoHTTP[EntradaGestionCobro]) (*cuerpo[GestionCobro], error) {
		if in.IdempotencyKey == "" {
			return nil, gestionSinClave()
		}
		in.Body.ID, in.Body.Clave = in.ID, in.IdempotencyKey
		return responder(m.RegistrarGestionCobro(ctx, in.Body))
	})
	ver := op("listarGestionesCobroConversacion", http.MethodGet, base+"/{id}/gestiones-cobro", "Gestiones de cobro de la conversación",
		nucleo.PermConversacionesVer, http.StatusOK, http.StatusNotFound)
	ver.Description += " Las 100 más recientes, con cómo va su envío a Argos."
	huma.Register(api, ver, func(ctx context.Context, in *idHTTP) (*cuerpo[ListaGestionesCobro], error) {
		return responder(m.GestionesCobro.Listar.Ejecutar(ctx, EntradaConversacion{ID: in.ID}))
	})
}

// gestionSinClave: la ruta exige Idempotency-Key (cualquier valor de hasta 200 caracteres: la usa
// para no registrar dos veces la misma gestión).
func gestionSinClave() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadRequest, "idempotency_key_requerida", "Falta Idempotency-Key",
		"Registrar una gestión de cobro exige la cabecera Idempotency-Key: repetirla devuelve la misma gestión",
		nucleo.ErrorCampo{Field: nucleo.CabeceraIdempotencia, Message: "Obligatoria", Rule: "required"})
}

// servicioNoDelCliente: el mismo 422 para un servicio que no es del cliente ni está a cargo de quien
// habla, exista o no.
func servicioNoDelCliente() *nucleo.Problema {
	return campoConv("servicioId", "El servicio no es de este cliente", "servicio_del_cliente")
}

// gestionDe es a nombre de quién va una gestión de cobro en Argos.
type gestionDe struct {
	// ClienteID: el cliente de la ruta en Argos (el de la conversación o, a cargo, el dueño del servicio).
	ClienteID int64
	// ContactoID: el contacto de Argos que habló (nil = el titular).
	ContactoID *int64
	// Contacto: el valor de X-Iris-Contacto ("" = el titular, sin la cabecera).
	Contacto   string
	ServicioID *int64
	ACargo     bool
}

// contactoDeGestion: quién habla en la conversación (B22), con el mismo rol que el resto de la atención
// (rolDeConversacion, que respeta contacto_argos_id y falla cerrado):
//   - sin rol (nadie confirmó quién habla): 422 cliente_sin_confirmar; nunca sale a nombre del titular;
//   - el titular (su número, o un cliente que confirmó un supervisor o la verificación fuerte): sin
//     X-Iris-Contacto;
//   - un contacto: necesita el permiso «pagar» (403 permiso_de_contacto) y solo registra sobre sus
//     servicios (403 servicio_fuera_del_contacto); si sus servicios están acotados, la gestión lleva
//     el servicio: el único suyo o, si tiene varios, 422 en servicioId;
//   - un servicio de otro cliente a cargo de quien habla (§3.8, B25): la gestión va al cliente dueño
//     del servicio, con X-Iris-Contacto = el contacto de quien habla en ese cliente y su permiso «pagar».
func (m *Modulo) contactoDeGestion(ctx context.Context, q *mensajeriabd.Queries, c mensajeriabd.MensajeriaConversacion, servicio *int64) (gestionDe, error) {
	rol, err := m.rolDeConversacion(ctx, q, c)
	if err != nil {
		return gestionDe{}, err
	}
	if rol.Propio == nil || rol.Propio.SinRol {
		return gestionDe{}, clienteSinConfirmar()
	}
	p := *rol.Propio
	if servicio != nil && len(rol.ACargo) > 0 {
		s, err := q.ServicioInstantaneaPorID(ctx, *servicio)
		switch {
		case err == nil && s.EmpresaID == empresaDeConv(c) && s.ClienteID != *c.ClienteID:
			// Solo servicios de la empresa de la conversación: uno ajeno (de otro cliente sin vínculo, de
			// otra empresa o inexistente) responde lo mismo, 422 en servicioId, sin decir si existe.
			for _, k := range rol.ACargo {
				if k.ClienteID != s.ClienteID || !k.cubre(s.ID) {
					continue
				}
				// Verificación heredada: solo con los permisos de ese vínculo.
				if !k.permite(PermisoContactoPagar) {
					return gestionDe{}, permisoDeContacto(PermisoContactoPagar)
				}
				return gestionDe{ClienteID: s.ClienteID, ContactoID: k.ContactoID, Contacto: k.cabecera(), ServicioID: &s.ID, ACargo: true}, nil
			}
			return gestionDe{}, servicioNoDelCliente()
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return gestionDe{}, err
		}
	}
	if p.esTitular() {
		return gestionDe{ClienteID: *c.ClienteID, ServicioID: servicio}, nil
	}
	if !p.permite(PermisoContactoPagar) {
		return gestionDe{}, permisoDeContacto(PermisoContactoPagar)
	}
	g := gestionDe{ClienteID: *c.ClienteID, ContactoID: p.ContactoID, Contacto: p.cabecera(), ServicioID: servicio}
	if servicio != nil {
		if !p.cubre(*servicio) {
			return gestionDe{}, servicioFueraDelContacto()
		}
		return g, nil
	}
	if p.Servicios == nil {
		// Contacto de todos los servicios del cliente: la gestión puede ir sin servicio.
		return g, nil
	}
	unico, err := servicioUnicoDe(ctx, q, empresaDeConv(c), p)
	if err != nil {
		return gestionDe{}, err
	}
	if unico == nil {
		return gestionDe{}, servicioRequeridoContacto()
	}
	g.ServicioID = unico
	return g, nil
}
