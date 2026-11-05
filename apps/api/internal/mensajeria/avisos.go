package mensajeria

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	identidadbd "github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad/bd"
)

// Estados y motivos de un aviso (§6.2 y §6.6; GET /avisos/:id, 0.6.1).
const (
	EstadoEnCola    = "en_cola"
	EstadoBloqueado = "bloqueado"
	EstadoEntregado = "entregado"
	EstadoFallido   = "fallido"

	// bloqueado (no se reintenta)
	MotivoSinPermiso        = "sin_permiso"
	MotivoBaja              = "baja"
	MotivoLimiteDiario      = "limite_diario"
	MotivoSinContacto       = "sin_contacto"
	MotivoCanalNoDisponible = "canal_no_disponible"
	// fallido (aviso.fallido)
	MotivoVencido        = "vencido"
	MotivoNumeroInvalido = "numero_invalido"
	MotivoRechazado      = "rechazado_por_canal"
	MotivoAgotado        = "agotado"
)

// EntradaAviso es el cuerpo de POST /mensajeria/avisos (§6.2). No acepta número de teléfono ni
// ningún otro campo: Iris toma el contacto de su instantánea de Argos.
type EntradaAviso struct {
	EmpresaID      int64             `json:"empresa_id" minimum:"1" doc:"Empresa en Argos"`
	ClienteID      int64             `json:"cliente_id" minimum:"1" doc:"Cliente en Argos"`
	ServicioID     *int64            `json:"servicio_id,omitempty" minimum:"1" nullable:"true" doc:"Servicio en Argos (opcional; debe ser del cliente y de la empresa)"`
	Plantilla      string            `json:"plantilla" minLength:"1" maxLength:"64" doc:"Código de la plantilla (§6.3; GET /mensajeria/plantillas)"`
	Variables      map[string]string `json:"variables,omitempty" maxProperties:"20" doc:"Solo las variables declaradas por la plantilla (otras: 422 variable_desconocida). Montos con dos decimales, fechas YYYY-MM-DD en hora de Guatemala"`
	CanalPreferido string            `json:"canal_preferido,omitempty" enum:"whatsapp,telegram,correo" default:"whatsapp" doc:"Fase 1: solo whatsapp; otro medio queda bloqueado con canal_no_disponible"`
	Prioridad      string            `json:"prioridad,omitempty" enum:"normal,urgente" default:"normal" doc:"urgente (solo corte_programado) salta el límite diario, nunca el horario de silencio ni la falta de permiso"`
	IdempotencyKey string            `json:"-"`
}

// AvisoCreado es la respuesta 202 (§6.2).
type AvisoCreado struct {
	AvisoID string  `json:"aviso_id" doc:"Id del aviso (uuid)"`
	Estado  string  `json:"estado" enum:"en_cola,bloqueado,entregado,fallido" doc:"Al crearlo: en_cola o bloqueado. Una repetición con la misma Idempotency-Key devuelve el estado actual"`
	Motivo  *string `json:"motivo,omitempty" doc:"Con bloqueado: sin_permiso, baja, limite_diario, sin_contacto o canal_no_disponible; con fallido, el de aviso.fallido"`
	// Repetido: la clave ya se había usado (cabecera Idempotent-Replayed).
	Repetido bool `json:"-"`
}

// AvisoDetalle es GET /mensajeria/avisos/:id (0.6.1).
type AvisoDetalle struct {
	AvisoID        string     `json:"aviso_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	EmpresaID      int64      `json:"empresa_id"`
	ClienteID      int64      `json:"cliente_id"`
	ServicioID     *int64     `json:"servicio_id" nullable:"true"`
	Plantilla      string     `json:"plantilla"`
	Estado         string     `json:"estado" enum:"en_cola,bloqueado,entregado,fallido"`
	Canal          *string    `json:"canal" nullable:"true"`
	Motivo         *string    `json:"motivo" nullable:"true"`
	Detalle        *string    `json:"detalle" nullable:"true"`
	CreadoEn       time.Time  `json:"creado_en"`
	EntregadoEn    *time.Time `json:"entregado_en" nullable:"true"`
	LeidoEn        *time.Time `json:"leido_en" nullable:"true"`
}

func aDetalle(a mensajeriabd.MensajeriaAviso) AvisoDetalle {
	clave := ""
	if a.IdempotencyKey != nil {
		clave = *a.IdempotencyKey
	}
	return AvisoDetalle{AvisoID: a.ID.String(), IdempotencyKey: clave, EmpresaID: a.EmpresaID, ClienteID: a.ClienteID, ServicioID: a.ServicioID,
		Plantilla: a.Plantilla, Estado: a.Estado, Canal: a.Canal, Motivo: a.Motivo, Detalle: a.Detalle, CreadoEn: a.CreatedAt,
		EntregadoEn: a.EntregadoEn, LeidoEn: a.LeidoEn}
}

func aCreado(a mensajeriabd.MensajeriaAviso, repetido bool) AvisoCreado {
	return AvisoCreado{AvisoID: a.ID.String(), Estado: a.Estado, Motivo: a.Motivo, Repetido: repetido}
}

// normalizar aplica los valores por defecto.
func (in *EntradaAviso) normalizar() {
	if in.CanalPreferido == "" {
		in.CanalPreferido = CanalWhatsApp
	}
	if in.Prioridad == "" {
		in.Prioridad = "normal"
	}
	if in.Variables == nil {
		in.Variables = map[string]string{}
	}
}

// hash de la petición (sin la clave): la misma clave con otro cuerpo es 422.
func hashDe(v any) []byte {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return h[:]
}

func idempotenciaReutilizada() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "idempotencia_reutilizada", "Clave de idempotencia reutilizada",
		"La Idempotency-Key ya se usó con otra petición")
}

func claveRequerida() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusBadRequest, "idempotency_key_requerida", "Falta Idempotency-Key",
		"Esta operación exige la cabecera Idempotency-Key (argos:aviso:<plantilla>:<envio_id>, argos:consentimiento:…)",
		nucleo.ErrorCampo{Field: nucleo.CabeceraIdempotencia, Message: "Obligatoria", Rule: "required"})
}

func clienteFueraDeEmpresa() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "cliente_fuera_de_empresa", "Cliente fuera de la empresa",
		"El cliente no existe en Argos o no es de una empresa de esta integración",
		nucleo.ErrorCampo{Field: "cliente_id", Message: "No es un cliente de la empresa", Rule: "empresa"})
}

func errSinCifrado() *nucleo.Problema {
	return nucleo.ReglaNegocio("cifrado_no_disponible", "La mensajería necesita IRIS_CLAVE_CIFRADO para las credenciales de los canales")
}

// exigirEmpresa: un usuario, token o integración solo opera en sus empresas (422
// cliente_fuera_de_empresa, sin revelar si la empresa existe); con X-Iris-Empresa debe ser esa (422
// empresa_no_coincide). El actor sistema no se limita.
func exigirEmpresa(ctx context.Context, q identidadbd.DBTX, empresa int64) error {
	if !contexto.ActorDe(ctx).EsUsuario() {
		return nil
	}
	e := contexto.EmpresasDe(ctx)
	if e.Estado == contexto.EmpresaNoResuelta {
		var err error
		if e, err = identidad.ResolverEmpresa(ctx, identidadbd.New(q), ""); err != nil {
			return err
		}
	}
	switch {
	case !slices.Contains(e.Disponibles, empresa):
		return clienteFueraDeEmpresa()
	case e.Estado == contexto.EmpresaActiva && e.Activa != empresa && len(e.Disponibles) > 1:
		return nucleo.ReglaNegocio("empresa_no_coincide", "empresa_id no es la empresa activa (X-Iris-Empresa)")
	}
	return nil
}

func usuarioDe(ctx context.Context) *int64 {
	if a := contexto.ActorDe(ctx); a.EsUsuario() {
		return &a.ID
	}
	return nil
}

// EnviarAviso (POST /mensajeria/avisos, mensajeria.enviar) ---------------------------------------

// EnviarAviso aplica §6.2 en dos casos de uso, sin transacción abierta mientras se consulta Argos:
//
//  1. previo: permiso, forma de la entrada (plantilla, variables, prioridad), empresa del actor y la
//     Idempotency-Key (obligatoria; una repetición devuelve el aviso con su estado actual, sin
//     consultar Argos; la misma clave con otro cuerpo, 422 idempotencia_reutilizada);
//  2. el contacto del cliente desde la instantánea de Argos (refrescada si tiene más de 24 h);
//  3. final: valida cliente y servicio en la empresa, decide en_cola o bloqueado (permiso, baja,
//     contacto, canal y plantilla aprobada, límite diario), guarda el aviso, lo audita, emite
//     aviso.creado y, en cola, encola su envío (después del horario de silencio si toca).
type EnviarAviso struct {
	m      *Modulo
	previo *auditoria.Caso[EntradaAviso, *AvisoCreado]
	final  *auditoria.Caso[entradaFinalAviso, AvisoCreado]
}

type entradaFinalAviso struct {
	EntradaAviso
	contacto argos.ContactoArgos
	hash     []byte
}

// MarshalJSON: lo que queda en auditoria.acciones.entrada (sin el contacto de Argos).
func (e entradaFinalAviso) MarshalJSON() ([]byte, error) { return json.Marshal(e.EntradaAviso) }

func nuevoEnviarAviso(m *Modulo) *EnviarAviso {
	k := &EnviarAviso{m: m}
	k.previo = auditoria.Nuevo(auditoria.Caso[EntradaAviso, *AvisoCreado]{
		Accion: "mensajeria.enviar", Permiso: nucleo.PermMensajeriaEnviar, SinEmpresa: true,
		AuditarExito: func(*AvisoCreado) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAviso) (*AvisoCreado, error) {
			if err := validarAviso(in); err != nil {
				return nil, err
			}
			if err := exigirEmpresa(ctx, t, in.EmpresaID); err != nil {
				return nil, err
			}
			return repeticion(ctx, mensajeriabd.New(t), in)
		},
	})
	k.final = auditoria.Nuevo(auditoria.Caso[entradaFinalAviso, AvisoCreado]{
		Accion: "mensajeria.enviar", Permiso: nucleo.PermMensajeriaEnviar, SinEmpresa: true,
		Logica: k.crear,
	})
	return k
}

func validarAviso(in EntradaAviso) error {
	p, ok := PlantillaPorCodigo(in.Plantilla)
	if !ok {
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "plantilla_desconocida", "Plantilla desconocida",
			"plantilla debe ser un código de GET /api/v1/mensajeria/plantillas",
			nucleo.ErrorCampo{Field: "plantilla", Message: "No es una plantilla de Iris", Rule: "plantilla"})
	}
	if err := p.validarVariables(in.Variables); err != nil {
		return err
	}
	if in.Prioridad == "urgente" && !p.Urgente {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "prioridad", Message: "urgente solo con corte_programado", Rule: "prioridad"})
	}
	if in.IdempotencyKey == "" {
		return claveRequerida()
	}
	return nil
}

// repeticion devuelve el aviso de la misma clave (con su estado actual) o nil si es nueva.
func repeticion(ctx context.Context, q *mensajeriabd.Queries, in EntradaAviso) (*AvisoCreado, error) {
	usuario := usuarioDe(ctx)
	if usuario == nil {
		return nil, nil
	}
	a, err := q.AvisoPorClave(ctx, mensajeriabd.AvisoPorClaveParams{UsuarioID: *usuario, IdempotencyKey: in.IdempotencyKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(a.HashPeticion, hashDe(in)) {
		return nil, idempotenciaReutilizada()
	}
	r := aCreado(a, true)
	return &r, nil
}

// Ejecutar crea el aviso (o devuelve el de la misma clave).
func (k *EnviarAviso) Ejecutar(ctx context.Context, in EntradaAviso) (AvisoCreado, error) {
	in.normalizar()
	previo, err := k.previo.Ejecutar(ctx, in)
	if err != nil {
		return AvisoCreado{}, err
	}
	if previo != nil {
		return *previo, nil
	}
	var contacto argos.ContactoArgos
	if k.m.cfg.Contactos != nil {
		if contacto, err = k.m.cfg.Contactos.ContactoCliente(ctx, in.ClienteID); err != nil {
			return AvisoCreado{}, err
		}
	}
	return k.final.Ejecutar(ctx, entradaFinalAviso{EntradaAviso: in, contacto: contacto, hash: hashDe(in)})
}

// avisoAuditable es lo que queda en auditoria.cambios (sin el destino).
type avisoAuditable struct {
	EmpresaID int64   `json:"empresa_id"`
	ClienteID int64   `json:"cliente_id"`
	Plantilla string  `json:"plantilla"`
	Estado    string  `json:"estado"`
	Motivo    *string `json:"motivo"`
	Canal     *string `json:"canal"`
}

func auditable(a mensajeriabd.MensajeriaAviso) avisoAuditable {
	return avisoAuditable{EmpresaID: a.EmpresaID, ClienteID: a.ClienteID, Plantilla: a.Plantilla, Estado: a.Estado, Motivo: a.Motivo, Canal: a.Canal}
}

func (k *EnviarAviso) crear(ctx context.Context, t *auditoria.Tx, in entradaFinalAviso) (AvisoCreado, error) {
	m := k.m
	q := mensajeriabd.New(t)
	t.FijarEmpresa(in.EmpresaID)
	if err := q.BloquearAvisosCliente(ctx, mensajeriabd.BloquearAvisosClienteParams{EmpresaID: in.EmpresaID, ClienteID: in.ClienteID}); err != nil {
		return AvisoCreado{}, err
	}
	// Otra petición con la misma clave pudo terminar mientras se consultaba Argos.
	if r, err := repeticion(ctx, q, in.EntradaAviso); err != nil || r != nil {
		if r != nil {
			return *r, err
		}
		return AvisoCreado{}, err
	}
	c := in.contacto
	if !c.Existe || !c.EnEmpresa(in.EmpresaID) {
		t.Anotar(map[string]any{"cliente_en_argos": c.Existe})
		return AvisoCreado{}, clienteFueraDeEmpresa()
	}
	if in.ServicioID != nil {
		if e, ok := c.Servicios[*in.ServicioID]; !ok || e != in.EmpresaID {
			return AvisoCreado{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "servicio_fuera_de_empresa", "Servicio fuera de la empresa",
				"El servicio no es del cliente o no es de la empresa", nucleo.ErrorCampo{Field: "servicio_id", Message: "No es un servicio del cliente en la empresa", Rule: "empresa"})
		}
	}
	p, _ := PlantillaPorCodigo(in.Plantilla)
	ahora := m.ahora()
	id, err := uuid.NewV7()
	if err != nil {
		return AvisoCreado{}, err
	}
	fila := mensajeriabd.InsertarAvisoParams{
		ID: id, EmpresaID: in.EmpresaID, ClienteID: in.ClienteID, ServicioID: in.ServicioID, Plantilla: in.Plantilla,
		CanalPreferido: in.CanalPreferido, Prioridad: in.Prioridad, Origen: origenDe(ctx), Estado: EstadoEnCola,
		UsuarioID: usuarioDe(ctx), IdempotencyKey: &in.IdempotencyKey, HashPeticion: in.hash, VenceEn: p.venceEn(in.Variables, m.cfg.Zona), CreadoEn: ahora,
	}
	if a := contexto.ActorDe(ctx); a.TokenID != 0 {
		fila.TokenID = &a.TokenID
	}
	if fila.Variables, err = json.Marshal(in.Variables); err != nil {
		return AvisoCreado{}, err
	}
	motivo, err := m.decidir(ctx, q, in, p, &fila, ahora)
	if err != nil {
		return AvisoCreado{}, err
	}
	if motivo != "" {
		fila.Estado, fila.Motivo, fila.FinalizadoEn = EstadoBloqueado, &motivo, &ahora
	} else {
		_, fin := m.cfg.Silencio.enSilencio(ahora, m.cfg.Zona)
		fila.ProgramadoEn = &fin
	}
	a, err := q.InsertarAviso(ctx, fila)
	if errors.Is(err, pgx.ErrNoRows) {
		// La misma clave entró a la vez (ON CONFLICT): se devuelve ese aviso.
		r, err := repeticion(ctx, q, in.EntradaAviso)
		if err == nil && r == nil {
			err = nucleo.Conflicto("idempotencia_en_curso", "Una petición con esta clave sigue en curso; vuelve a intentar")
		}
		if err != nil {
			return AvisoCreado{}, err
		}
		return *r, nil
	}
	if err != nil {
		return AvisoCreado{}, err
	}
	t.RegistrarCambios("aviso", a.ID, nil, auditable(a))
	if a.Estado == EstadoEnCola {
		if err := m.encolar(ctx, t.Tx, ArgsEnviarAviso{AvisoID: a.ID, programado: *a.ProgramadoEn}); err != nil {
			return AvisoCreado{}, err
		}
	}
	return aCreado(a, false), t.Emitir("aviso.creado", map[string]any{
		"aviso_id": a.ID, "empresa_id": a.EmpresaID, "cliente_id": a.ClienteID, "servicio_id": a.ServicioID, "plantilla": a.Plantilla,
		"estado": a.Estado, "motivo": a.Motivo, "origen": a.Origen, "prioridad": a.Prioridad, "programado_en": a.ProgramadoEn,
	})
}

// origenDe: los avisos de un usuario de integración de Argos son de Argos (sus webhooks van a
// Argos); los demás, de Iris.
func origenDe(ctx context.Context) string {
	if contexto.ActorDe(ctx).Tipo == contexto.ActorIntegracion {
		return "argos"
	}
	return "iris"
}

// decidir aplica las reglas de §6.2 y §6.5 en este orden: permiso registrado, baja, contacto, canal
// (configurado, que admite avisos y con la plantilla aprobada) y límite diario. Devuelve el motivo
// del bloqueo ("" = en cola) y completa canal, remitente y destino.
func (m *Modulo) decidir(ctx context.Context, q *mensajeriabd.Queries, in entradaFinalAviso, p Plantilla, fila *mensajeriabd.InsertarAvisoParams, ahora time.Time) (string, error) {
	medio := in.CanalPreferido
	vigente, err := q.ConsentimientoVigente(ctx, mensajeriabd.ConsentimientoVigenteParams{EmpresaID: in.EmpresaID, ClienteID: in.ClienteID, Canal: medio})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return MotivoSinPermiso, nil
	case err != nil:
		return "", err
	case vigente.Estado == "baja":
		return MotivoBaja, nil
	}
	destino := destinoDe(in.contacto, medio)
	if destino == "" {
		return MotivoSinContacto, nil
	}
	if medio != CanalWhatsApp {
		return MotivoCanalNoDisponible, nil
	}
	r, err := m.resolver(ctx, q, in.EmpresaID, medio)
	if err != nil {
		return "", err
	}
	if r == nil || !r.a.PermiteAvisos() || !r.a.Aprobada(p.Codigo) {
		return MotivoCanalNoDisponible, nil
	}
	fila.Canal, fila.CanalID, fila.Remitente, fila.Destino = &medio, r.canalID, &r.remitente, &destino
	if in.Prioridad != "urgente" {
		n, err := q.ContarAvisosDelDia(ctx, mensajeriabd.ContarAvisosDelDiaParams{EmpresaID: in.EmpresaID, ClienteID: in.ClienteID,
			Desde: inicioDelDia(ahora, m.cfg.Zona)})
		if err != nil {
			return "", err
		}
		if n >= int64(m.cfg.LimiteDiario) {
			return MotivoLimiteDiario, nil
		}
	}
	return "", nil
}

// destinoDe: el teléfono E.164 del cliente (primero el móvil) para WhatsApp; "" si no hay uno válido.
func destinoDe(c argos.ContactoArgos, medio string) string {
	if medio != CanalWhatsApp && medio != CanalTelegram {
		return ""
	}
	tels := append(formato.Telefonos("movil", c.Movil), formato.Telefonos("telefono", c.Telefono)...)
	if p := formato.Principal(tels); p != nil && p.E164 != nil {
		return *p.E164
	}
	return ""
}

// ArgsEnviarAviso es el job que manda un aviso en cola.
type ArgsEnviarAviso struct {
	AvisoID    uuid.UUID `json:"aviso_id"`
	programado time.Time
}

// Kind implementa river.JobArgs.
func (ArgsEnviarAviso) Kind() string { return "mensajeria.enviar_aviso" }

// InsertOpts: en la cola de mensajería, después del horario de silencio si toca.
func (a ArgsEnviarAviso) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 8, ScheduledAt: a.programado}
}

// VerAviso (GET /mensajeria/avisos/:id, mensajeria.enviar) ---------------------------------------

// EntradaVerAviso es el id del aviso.
type EntradaVerAviso struct {
	ID string `json:"id" path:"id" maxLength:"64" doc:"aviso_id"`
}

func nuevoVerAviso(_ *Modulo) *auditoria.Caso[EntradaVerAviso, AvisoDetalle] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerAviso, AvisoDetalle]{
		Accion: "mensajeria.ver_aviso", Permiso: nucleo.PermMensajeriaEnviar, SinEmpresa: true,
		AuditarExito: func(AvisoDetalle) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerAviso) (AvisoDetalle, error) {
			noExiste := nucleo.NoEncontrado("El aviso no existe")
			id, err := uuid.Parse(in.ID)
			if err != nil {
				return AvisoDetalle{}, noExiste
			}
			a, err := mensajeriabd.New(t).AvisoPorID(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return AvisoDetalle{}, noExiste
			}
			if err != nil {
				return AvisoDetalle{}, err
			}
			// Cada integración solo ve sus avisos: los de otra son 404, como uno inexistente.
			if u := usuarioDe(ctx); u != nil && (a.UsuarioID == nil || *a.UsuarioID != *u) {
				return AvisoDetalle{}, noExiste
			}
			return aDetalle(a), nil
		},
	})
}
