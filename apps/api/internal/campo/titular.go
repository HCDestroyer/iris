package campo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/formato"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Paso «Confirmar titular y plan» de la orden de instalación (contrato argosisp.md 0.10.0 §3.4 y
// §3.4.8, pedidos B18 y B19; reglas del lote en contratos/sync.md §2 y §3). Antes de activar, el
// técnico (en sitio, también sin señal) confirma quién firma y su identidad, completa lo que falte
// del titular y del lugar y, si hace falta, cambia el plan. Cada acción encola su llamada a Argos
// en la cola de pendientes, en orden y con dependencias:
//
//	identidad (A41) o cambio de titular (A39) → completar datos (A43) → cambiar plan (A40) → activar
//
// Lo sensible (número de documento, teléfono, correo, NIT/CUI y la foto del documento) nunca se
// guarda en la orden, en la auditoría (***), en los eventos, en los logs ni en el payload de los
// pendientes: va cifrado al depósito de tránsito y se borra al 2xx de Argos o a los 7 días.

// Operaciones del lote del paso (contratos/sync.md §2).
const (
	OpRegistrarIdentidad = "orden.registrar_identidad"
	OpCambiarTitular     = "orden.cambiar_titular"
	OpCompletarDatos     = "orden.completar_datos"
	OpCambiarPlan        = "orden.cambiar_plan"
)

// tipoInstalacion: el código del tipo de orden de instalación.
const tipoInstalacion = "instalacion"

// esOpTitular: las operaciones del paso (bloque propio del despachador del lote).
func esOpTitular(tipo string) bool {
	switch tipo {
	case OpRegistrarIdentidad, OpCambiarTitular, OpCompletarDatos, OpCambiarPlan:
		return true
	}
	return false
}

// Límites del paso.
const (
	// MaxFotoDocumento: bytes de cada foto del documento en el lote (el teléfono la comprime; el
	// lote completo es de hasta 4 MiB).
	MaxFotoDocumento = 1 << 20
	// maxBase64Foto: largo en base64 de MaxFotoDocumento.
	maxBase64Foto = (MaxFotoDocumento + 2) / 3 * 4
	// maxTomasIdentidad: tomas de la identidad por orden (A41 y A39 juntas, también las rechazadas).
	maxTomasIdentidad = 6
	// maxCambiosTitular: cambios de titular vivos por orden (Argos acepta 2 por servicio, §3.4.4).
	maxCambiosTitular = 2
	// maxPasosDatos: completar datos y cambiar plan, cada uno, por orden.
	maxPasosDatos = 10
)

// Eventos del paso (catálogo de CLAUDE.md; nunca el número de documento ni datos de contacto).
const (
	EventoIdentidadRegistrada    = "orden.identidad_registrada"
	EventoCambioTitular          = "orden.cambio_titular_registrado"
	EventoDatosCompletados       = "orden.datos_completados"
	EventoCambioPlan             = "orden.cambio_plan_registrado"
	EventoIdentidadValidada      = "orden.identidad_validada"
	metodoFotoDocumento          = "foto_documento"
	metodoRevisionPanel          = "revision_panel"
	metodoSinIdentidad           = "sin_identidad"
	codigoTitularNoCoincide      = "titular_no_coincide"
	origenTipoOrden              = pendientes.OrigenTipo
	motivoReemplazoToma          = "reemplazado por otra toma de la identidad"
	motivoReemplazoRevisionPanel = "reemplazado por la revisión desde el panel"
)

// ConfigTitular conecta el paso con la cola de pendientes (I1).
type ConfigTitular struct {
	// Cifrador del depósito de tránsito (IRIS_CLAVE_CIFRADO). nil = lo sensible no se guarda (503).
	Cifrador *nucleo.Cifrador
	// Conector: hay un conector de Argos registrado (argos.Modulo.HayConectorRegistrado). nil = modo
	// sin conector: el paso no aplica (no hay servicio pendiente en Argos) y el cierre no exige la
	// identidad.
	Conector func(ctx context.Context, t *auditoria.Tx) (bool, error)
	// Encolar avisa a la cola de pendientes al encolar (nil = la toma el periódico).
	Encolar pendientes.Encolador
}

// ConectorDeArgos adapta argos.Modulo.HayConectorRegistrado para ConfigTitular.Conector.
func ConectorDeArgos(a *argos.Modulo) func(ctx context.Context, t *auditoria.Tx) (bool, error) {
	if a == nil {
		return nil
	}
	return func(ctx context.Context, t *auditoria.Tx) (bool, error) { return a.HayConectorRegistrado(ctx, t) }
}

// --- Entradas ----------------------------------------------------------------------------------

// FotoDocumento es una foto del documento de identidad. Vive solo en la cola del teléfono hasta
// que el servidor la confirma; en Iris, solo en el depósito de tránsito cifrado.
type FotoDocumento struct {
	Mime   string `json:"mime" enum:"image/jpeg,image/png"`
	Base64 string `json:"base64" minLength:"8" maxLength:"1398104" auditoria:"sensible" doc:"La imagen en base64 (hasta 1 MiB: el teléfono la comprime). Nunca se devuelve ni queda en la auditoría"`
}

// EntradaRegistrarIdentidad: «La misma persona de la venta» (A41, §3.4.3). Datos de
// orden.registrar_identidad.
type EntradaRegistrarIdentidad struct {
	ID              int64          `json:"-"`
	DocumentoTipo   string         `json:"documentoTipo" enum:"dpi,pasaporte"`
	DocumentoNumero string         `json:"documentoNumero" minLength:"5" maxLength:"20" auditoria:"sensible" doc:"DPI: 13 dígitos con verificador, departamento y municipio. Pasaporte: 5 a 20 letras o dígitos. Nunca se devuelve ni va en eventos, logs ni auditoría"`
	Nombre          string         `json:"nombre" minLength:"2" maxLength:"200" doc:"Como aparece en el documento"`
	MayorDeEdad     bool           `json:"mayorDeEdad" doc:"«El titular es mayor de edad»: debe ser true"`
	Coincide        bool           `json:"coincide" doc:"«Coincide con el titular»: el técnico comprobó que la foto es de quien está presente; debe ser true"`
	Frente          FotoDocumento  `json:"frente"`
	Reverso         *FotoDocumento `json:"reverso,omitempty"`
	Sync            *OpSync        `json:"-"`
}

// TitularNuevo son los datos de quien queda como titular (A39, §3.4.2).
type TitularNuevo struct {
	Nombre              string  `json:"nombre" minLength:"2" maxLength:"200" doc:"Como aparece en el documento"`
	DocumentoTipo       string  `json:"documentoTipo" enum:"dpi,pasaporte"`
	DocumentoNumero     string  `json:"documentoNumero" minLength:"5" maxLength:"20" auditoria:"sensible"`
	MayorDeEdad         bool    `json:"mayorDeEdad" doc:"Debe ser true"`
	Telefono            string  `json:"telefono" minLength:"8" maxLength:"25" auditoria:"sensible" doc:"8 dígitos de Guatemala (+502 opcional)"`
	Whatsapp            bool    `json:"whatsapp"`
	Email               *string `json:"email,omitempty" maxLength:"254" auditoria:"sensible"`
	TipoDocumentoFiscal string  `json:"tipoDocumentoFiscal" enum:"cf,nit,cui" doc:"«Facturar a» (FEL)"`
	NitCui              *string `json:"nitCui,omitempty" maxLength:"20" auditoria:"sensible" doc:"NIT (con verificador, K permitido) o CUI; vacío con cf"`
	Relacion            *string `json:"relacion,omitempty" enum:"titular,familiar,otro" doc:"Relación con quien pidió el servicio"`
}

// EntradaCambiarTitular: «Otra persona» (A39, §3.4.4). Datos de orden.cambiar_titular.
type EntradaCambiarTitular struct {
	ID           int64          `json:"-"`
	Titular      TitularNuevo   `json:"titular"`
	Coincide     bool           `json:"coincide" doc:"«Coincide con el titular»: debe ser true"`
	Frente       FotoDocumento  `json:"frente"`
	Reverso      *FotoDocumento `json:"reverso,omitempty"`
	MotivoCodigo string         `json:"motivoCodigo" enum:"menor_de_edad,otro"`
	Motivo       string         `json:"motivo" minLength:"10" maxLength:"500"`
	Sync         *OpSync        `json:"-"`
}

// ClienteCompletar: lo que falte del titular (A43 solo llena campos vacíos en Argos).
type ClienteCompletar struct {
	Nombre              *string `json:"nombre,omitempty" minLength:"2" maxLength:"200" doc:"Solo para corregirlo como en el documento (con la identidad registrada en esta orden)"`
	Telefono            *string `json:"telefono,omitempty" minLength:"8" maxLength:"25" auditoria:"sensible"`
	Whatsapp            *bool   `json:"whatsapp,omitempty"`
	Email               *string `json:"email,omitempty" maxLength:"254" auditoria:"sensible"`
	TipoDocumentoFiscal *string `json:"tipoDocumentoFiscal,omitempty" enum:"cf,nit,cui"`
	NitCui              *string `json:"nitCui,omitempty" maxLength:"20" auditoria:"sensible"`
	Relacion            *string `json:"relacion,omitempty" enum:"titular,familiar,otro"`
}

// LugarServicio: el lugar de la instalación (se corrige aunque ya tenga datos).
type LugarServicio struct {
	Direccion  *string  `json:"direccion,omitempty" minLength:"3" maxLength:"300"`
	Referencia *string  `json:"referencia,omitempty" maxLength:"300"`
	Lat        *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon        *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	PrecisionM *float64 `json:"precisionM,omitempty" minimum:"0" maximum:"10000" doc:"Precisión del GPS en metros"`
}

// EntradaCompletarDatos: A43 (§3.4.6). Datos de orden.completar_datos.
type EntradaCompletarDatos struct {
	ID       int64             `json:"-"`
	Cliente  *ClienteCompletar `json:"cliente,omitempty"`
	Servicio *LugarServicio    `json:"servicio,omitempty"`
	Sync     *OpSync           `json:"-"`
}

// EntradaCambiarPlan: A40 (§3.4.5). Datos de orden.cambiar_plan.
type EntradaCambiarPlan struct {
	ID          int64   `json:"-"`
	PlanID      int64   `json:"planId" minimum:"1" doc:"Uno de los planes permitidos (mismo modo y router); Argos lo vuelve a comprobar (plan_no_permitido)"`
	Motivo      string  `json:"motivo" enum:"mas_velocidad,menos_velocidad,otro"`
	MotivoTexto *string `json:"motivoTexto,omitempty" minLength:"10" maxLength:"500" doc:"Obligatorio con otro"`
	Sync        *OpSync `json:"-"`
}

// EntradaValidarIdentidad: POST /ordenes/{id}/validar-identidad (supervisor, desde el panel).
type EntradaValidarIdentidad struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string `json:"motivo" minLength:"10" maxLength:"500" doc:"Por qué se valida (queda en la auditoría)"`
}

func (e EntradaRegistrarIdentidad) ordenID() int64 { return e.ID }
func (e EntradaCambiarTitular) ordenID() int64     { return e.ID }
func (e EntradaCompletarDatos) ordenID() int64     { return e.ID }
func (e EntradaCambiarPlan) ordenID() int64        { return e.ID }

// --- Estado del paso ---------------------------------------------------------------------------

// PasoTitularVista es un paso como lo ven el panel y el teléfono: sin cuerpo, respuesta ni detalle
// de Argos (el técnico no ve posible_duplicado ni datos de otro cliente).
type PasoTitularVista struct {
	PendienteID   int64     `json:"pendienteId"`
	Operacion     string    `json:"operacion" enum:"registrar_identidad,cambiar_titular,completar_datos,cambiar_plan"`
	Estado        string    `json:"estado" enum:"enviando,hecho,rechazado,agotado,bloqueado,resuelto" doc:"enviando: en la cola; rechazado: Argos lo rechazó (codigo); agotado: Argos no respondió tras los reintentos; bloqueado: espera un paso anterior que falló; resuelto: reemplazado por otra toma o resuelto a mano"`
	Codigo        *string   `json:"codigo,omitempty" doc:"Código del rechazo (titular_no_coincide, foto_vencida, validacion…)"`
	ActualizadoEn time.Time `json:"actualizadoEn"`
}

// EstadoTitular es el paso «Confirmar titular y plan» de una orden.
type EstadoTitular struct {
	OrdenID       int64   `json:"ordenId"`
	QuienFirma    *string `json:"quienFirma" nullable:"true" enum:"misma,otra" doc:"misma: la persona de la venta (A41); otra: cambio de titular (A39)"`
	DocumentoTipo *string `json:"documentoTipo" nullable:"true" enum:"dpi,pasaporte"`
	Nombre        *string `json:"nombre" nullable:"true" doc:"Del titular, como en el documento (nunca el número)"`
	Identidad     string  `json:"identidad" enum:"falta,registrada,validada" doc:"registrada: tomada en sitio y en la cola hacia Argos; validada: un supervisor la validó desde el panel (solo para activar)"`
	EnRevision    bool    `json:"enRevision" doc:"Argos respondió titular_no_coincide: la activación no sale hasta que un supervisor lo resuelva"`
	Pendiente     bool    `json:"pendienteEnArgos" doc:"Algún paso no llegó a Argos y no saldrá solo: la activación queda bloqueada"`
	PlanID        *int64  `json:"planId" nullable:"true" doc:"El último plan pedido"`
	// Pasos, en el orden en que se encolaron.
	Pasos   []PasoTitularVista `json:"pasos"`
	Version int32              `json:"version" doc:"0 si la orden aún no tiene el paso"`
}

// CasosTitular son los casos de uso del paso.
type CasosTitular struct {
	RegistrarIdentidad *auditoria.Caso[EntradaRegistrarIdentidad, Orden]
	CambiarTitular     *auditoria.Caso[EntradaCambiarTitular, Orden]
	CompletarDatos     *auditoria.Caso[EntradaCompletarDatos, Orden]
	CambiarPlan        *auditoria.Caso[EntradaCambiarPlan, Orden]
	ValidarIdentidad   *auditoria.Caso[EntradaValidarIdentidad, Orden]
	Ver                *auditoria.Caso[EntradaVerOrden, EstadoTitular]
}

func (m *Modulo) nuevosCasosTitular() CasosTitular {
	return CasosTitular{
		RegistrarIdentidad: m.nuevoRegistrarIdentidad(),
		CambiarTitular:     m.nuevoCambiarTitular(),
		CompletarDatos:     m.nuevoCompletarDatos(),
		CambiarPlan:        m.nuevoCambiarPlan(),
		ValidarIdentidad:   m.nuevoValidarIdentidad(),
		Ver:                m.nuevoVerTitular(),
	}
}

// --- Piezas comunes ----------------------------------------------------------------------------

// paso es lo que cada acción lee de la orden con candado.
type paso struct {
	f      campobd.CampoOrden
	tit    campobd.CampoOrdenTitular
	pasos  []pendientes.PasoDeTitular
	cuando time.Time
	actor  int64
}

func (p paso) buscar(id *int64) *pendientes.PasoDeTitular {
	if id == nil {
		return nil
	}
	for i := range p.pasos {
		if p.pasos[i].ID == *id {
			return &p.pasos[i]
		}
	}
	return nil
}

func (m *Modulo) conectorRegistrado(ctx context.Context, t *auditoria.Tx) (bool, error) {
	if m.cfg.Titular.Conector == nil {
		return false, nil
	}
	return m.cfg.Titular.Conector(ctx, t)
}

// leerPaso lee lo registrado del paso y sus pendientes (la orden ya está tomada).
func (m *Modulo) leerPaso(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden) (campobd.CampoOrdenTitular, []pendientes.PasoDeTitular, error) {
	tit, err := q.TitularDeOrden(ctx, f.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		tit, err = campobd.CampoOrdenTitular{OrdenID: f.ID, EmpresaID: f.EmpresaID}, nil
	}
	if err != nil {
		return tit, nil, err
	}
	pasos, err := pendientes.PasosDeOrden(ctx, t, f.ID)
	return tit, pasos, err
}

// abrirPaso toma la orden (sus reglas del lote, empresa, alcance) y exige lo del paso: el
// técnico, en sitio, en una instalación con su servicio pendiente de Argos (§3.4, «Quién»).
func (m *Modulo) abrirPaso(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, id int64, permiso nucleo.Permiso, s *OpSync, accion string) (paso, error) {
	f, err := tomar(ctx, t, q, id, permiso, nil, s)
	if err != nil {
		return paso{}, err
	}
	if err := noPausada(f, accion); err != nil {
		return paso{}, err
	}
	if err := exigirEstado(f, accion, EstadoEnSitio); err != nil {
		return paso{}, err
	}
	if err := exigirInstalacion(ctx, q, f); err != nil {
		return paso{}, err
	}
	registrado, err := m.conectorRegistrado(ctx, t)
	if err != nil {
		return paso{}, err
	}
	if !registrado {
		return paso{}, nucleo.ReglaNegocio("argos_sin_conector", "Sin conexión con Argos no hay un servicio pendiente que confirmar: el titular y el plan quedan los de la venta")
	}
	a := contexto.ActorDe(ctx)
	if !a.EsUsuario() {
		return paso{}, nucleo.Prohibido("El paso del titular lo hace una persona (X-Iris-Actor)")
	}
	tit, pasos, err := m.leerPaso(ctx, t, q, f)
	if err != nil {
		return paso{}, err
	}
	return paso{f: f, tit: tit, pasos: pasos, cuando: m.cuandoEn(s, f), actor: a.ID}, nil
}

func exigirInstalacion(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden) error {
	tipo, err := tipoDe(ctx, q, f)
	if err != nil {
		return err
	}
	if tipo.Codigo != tipoInstalacion {
		return nucleo.ReglaNegocio("orden_no_es_instalacion", "El titular y el plan se confirman solo en una orden de instalación")
	}
	if f.ServicioID == nil || f.ClienteID == nil {
		return nucleo.ReglaNegocio("orden_sin_servicio", "La orden no tiene el cliente y el servicio de Argos")
	}
	return nil
}

// encolar manda el paso a la cola con el actor de la persona.
func (m *Modulo) encolar(ctx context.Context, t *auditoria.Tx, p paso, operacion, clave, ruta string, cuerpo map[string]any,
	transito *pendientes.Transito, deps []int64) (pendientes.Registro, error) {
	return pendientes.EncolarPaso(ctx, t, m.cfg.Titular.Cifrador, pendientes.PasoTitular{
		EmpresaID: p.f.EmpresaID, OrdenID: p.f.ID, Operacion: operacion, Clave: clave, Ruta: ruta, Cuerpo: cuerpo,
		Transito: transito, TomadoEn: p.cuando, DependeDe: deps, Actor: pendientes.ActorUsuario(p.actor),
	}, m.cfg.Titular.Encolar)
}

// guardarTitular escribe lo registrado del paso, auditado (sin nada sensible: la tabla no lo tiene).
func guardarTitular(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, antes, despues campobd.CampoOrdenTitular) error {
	g, err := q.GuardarTitular(ctx, campobd.GuardarTitularParams{
		OrdenID: despues.OrdenID, EmpresaID: despues.EmpresaID, QuienFirma: despues.QuienFirma, DocumentoTipo: despues.DocumentoTipo,
		Nombre: despues.Nombre, IdentidadPendienteID: despues.IdentidadPendienteID, UltimoPendienteID: despues.UltimoPendienteID,
		Tomas: despues.Tomas, CambiosTitular: despues.CambiosTitular, DatosN: despues.DatosN, CambiosPlan: despues.CambiosPlan,
		PlanID: despues.PlanID, ValidadaPor: despues.ValidadaPor, ValidadaMotivo: despues.ValidadaMotivo, ValidadaEn: despues.ValidadaEn,
	})
	if err != nil {
		return err
	}
	var previo any
	if antes.Version > 0 {
		previo = antes
	}
	t.RegistrarCambios("orden_titular", g.OrdenID, previo, g)
	return nil
}

// identidadNueva decide de qué depende una toma nueva de la identidad (A41 o A39) y si reemplaza a
// la anterior: si la anterior quedó rechazada o agotada, la nueva ocupa su lugar en la cadena (lo
// que la esperaba pasa a esperar a la nueva). Si no, va después del último paso, siempre antes de
// completar datos o cambiar el plan (§3.4, «Orden»).
func identidadNueva(p paso) (deps []int64, reemplaza *pendientes.PasoDeTitular, err error) {
	if p.tit.Tomas >= maxTomasIdentidad {
		return nil, nil, nucleo.ReglaNegocio("demasiadas_tomas", "Ya se tomó la identidad demasiadas veces en esta orden; pide a un supervisor que la revise")
	}
	if previo := p.buscar(p.tit.IdentidadPendienteID); previo != nil && previo.Fallido() {
		return previo.DependeDe, previo, nil
	}
	if p.tit.DatosN > 0 || p.tit.CambiosPlan > 0 {
		return nil, nil, nucleo.ReglaNegocio("titular_despues_de_datos",
			"Ya se completaron los datos o se cambió el plan con este titular; para cambiarlo, pide a un supervisor que lo corrija")
	}
	if p.tit.UltimoPendienteID != nil {
		deps = []int64{*p.tit.UltimoPendienteID}
	}
	return deps, nil, nil
}

// reemplazar deja al paso anterior como reemplazado (si lo hay) y actualiza la cadena.
func reemplazar(ctx context.Context, t *auditoria.Tx, tit *campobd.CampoOrdenTitular, previo *pendientes.PasoDeTitular, nuevo int64,
	motivo string, usuario *int64, ahora time.Time) error {
	if previo != nil {
		if err := pendientes.ReemplazarPaso(ctx, t, previo.ID, nuevo, fmt.Sprintf("%s (pendiente %d)", motivo, nuevo), usuario, ahora); err != nil {
			return err
		}
	}
	if previo == nil || tit.UltimoPendienteID == nil || *tit.UltimoPendienteID == previo.ID {
		tit.UltimoPendienteID = &nuevo
	}
	tit.IdentidadPendienteID = &nuevo
	return nil
}

func reemplazaID(p *pendientes.PasoDeTitular) *int64 {
	if p == nil {
		return nil
	}
	return &p.ID
}

// --- A41: registrar la identidad del mismo titular ---------------------------------------------

func (m *Modulo) nuevoRegistrarIdentidad() *auditoria.Caso[EntradaRegistrarIdentidad, Orden] {
	return accionOrden("ordenes.registrar_identidad", nucleo.PermOrdenesCambiarTitular, func(e EntradaRegistrarIdentidad) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaRegistrarIdentidad) (campobd.CampoOrden, string, error) {
			p, err := m.abrirPaso(ctx, t, q, in.ID, nucleo.PermOrdenesCambiarTitular, in.Sync, "registrar la identidad del titular de")
			if err != nil {
				return p.f, "", err
			}
			numero, err := documentoValido("documentoNumero", in.DocumentoTipo, in.DocumentoNumero)
			if err != nil {
				return p.f, "", err
			}
			nombre, err := nombreValido("nombre", in.Nombre)
			if err != nil {
				return p.f, "", err
			}
			if err := marcasDelTecnico(in.MayorDeEdad, in.Coincide); err != nil {
				return p.f, "", err
			}
			fotos, err := fotosDocumento(in.Frente, in.Reverso)
			if err != nil {
				return p.f, "", err
			}
			deps, previo, err := identidadNueva(p)
			if err != nil {
				return p.f, "", err
			}
			// «La misma persona de la venta» no aplica si el servicio ya tiene otro titular en camino.
			for _, x := range p.pasos {
				if x.Operacion == pendientes.OpCambiarTitular && x.Estado != pendientes.EstadoResueltoManual && !x.Fallido() &&
					(previo == nil || x.ID != previo.ID) {
					return p.f, "", nucleo.ReglaNegocio("titular_ya_cambiado", "Ya se pidió el cambio de titular de esta orden: la identidad va con ese cambio")
				}
			}
			tit := p.tit
			tit.Tomas++
			cuerpo := map[string]any{
				"documento_tipo": in.DocumentoTipo, "nombre": nombre, "mayor_de_edad": true, "coincide": true,
				"verificado_en": p.cuando.Format(time.RFC3339), "metodo": metodoFotoDocumento,
				"origen_tipo": origenTipoOrden, "origen_id": strconv.FormatInt(p.f.ID, 10),
			}
			r, err := m.encolar(ctx, t, p, pendientes.OpRegistrarIdentidad, pendientes.ClaveIdentidad(p.f.ID, tit.Tomas),
				"/clientes/"+strconv.FormatInt(*p.f.ClienteID, 10)+"/identidad", cuerpo,
				&pendientes.Transito{Campos: map[string]any{"documento_numero": numero}, Fotos: fotos}, deps)
			if err != nil {
				return p.f, "", err
			}
			if err := reemplazar(ctx, t, &tit, previo, r.ID, motivoReemplazoToma, &p.actor, m.ahora()); err != nil {
				return p.f, "", err
			}
			tit.QuienFirma, tit.DocumentoTipo, tit.Nombre = ptr("misma"), &in.DocumentoTipo, &nombre
			if err := guardarTitular(ctx, t, q, p.tit, tit); err != nil {
				return p.f, "", err
			}
			if err := t.Emitir(EventoIdentidadRegistrada, map[string]any{
				"orden_id": p.f.ID, "pendiente_id": r.ID, "toma": tit.Tomas, "documento_tipo": in.DocumentoTipo,
				"metodo": metodoFotoDocumento, "reemplaza_id": reemplazaID(previo),
			}); err != nil {
				return p.f, "", err
			}
			f, err := tocar(ctx, t, q, p.f)
			return f, "", err
		})
}

// --- A39: cambiar el titular ------------------------------------------------------------------

func (m *Modulo) nuevoCambiarTitular() *auditoria.Caso[EntradaCambiarTitular, Orden] {
	return accionOrden("ordenes.cambiar_titular", nucleo.PermOrdenesCambiarTitular, func(e EntradaCambiarTitular) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCambiarTitular) (campobd.CampoOrden, string, error) {
			p, err := m.abrirPaso(ctx, t, q, in.ID, nucleo.PermOrdenesCambiarTitular, in.Sync, "cambiar el titular de")
			if err != nil {
				return p.f, "", err
			}
			n := in.Titular
			numero, err := documentoValido("titular.documentoNumero", n.DocumentoTipo, n.DocumentoNumero)
			if err != nil {
				return p.f, "", err
			}
			nombre, err := nombreValido("titular.nombre", n.Nombre)
			if err != nil {
				return p.f, "", err
			}
			if err := marcasDelTecnico(n.MayorDeEdad, in.Coincide); err != nil {
				return p.f, "", err
			}
			telefono, err := telefonoValido("titular.telefono", n.Telefono)
			if err != nil {
				return p.f, "", err
			}
			sensible := map[string]any{"cliente.documento_numero": numero, "cliente.telefono": telefono, "cliente.email": nil, "cliente.nit_cui": nil}
			if n.Email != nil && strings.TrimSpace(*n.Email) != "" {
				correo, err := correoValido("titular.email", *n.Email)
				if err != nil {
					return p.f, "", err
				}
				sensible["cliente.email"] = correo
			}
			fiscal, err := fiscalValido("titular", n.TipoDocumentoFiscal, n.NitCui)
			if err != nil {
				return p.f, "", err
			}
			if fiscal != "" {
				sensible["cliente.nit_cui"] = fiscal
			}
			motivo, err := texto("motivo", in.Motivo, 10, 500)
			if err != nil {
				return p.f, "", err
			}
			fotos, err := fotosDocumento(in.Frente, in.Reverso)
			if err != nil {
				return p.f, "", err
			}
			deps, previo, err := identidadNueva(p)
			if err != nil {
				return p.f, "", err
			}
			vivos := 0
			for _, x := range p.pasos {
				if x.Operacion == pendientes.OpCambiarTitular && x.Estado != pendientes.EstadoResueltoManual && !x.Fallido() {
					vivos++
				}
			}
			if vivos >= maxCambiosTitular {
				return p.f, "", nucleo.ReglaNegocio("demasiados_cambios_titular", "El servicio ya tiene 2 cambios de titular (el límite de Argos)")
			}
			tit := p.tit
			tit.Tomas++
			tit.CambiosTitular++
			cliente := map[string]any{"nombre": nombre, "documento_tipo": n.DocumentoTipo, "mayor_de_edad": true,
				"whatsapp": n.Whatsapp, "tipo_documento_fiscal": n.TipoDocumentoFiscal}
			if n.Relacion != nil {
				cliente["relacion"] = *n.Relacion
			}
			cuerpo := map[string]any{
				"cliente":       cliente,
				"identidad":     map[string]any{"coincide": true, "verificado_en": p.cuando.Format(time.RFC3339), "metodo": metodoFotoDocumento},
				"motivo_codigo": in.MotivoCodigo, "motivo": motivo,
				"origen_tipo": origenTipoOrden, "origen_id": strconv.FormatInt(p.f.ID, 10),
			}
			r, err := m.encolar(ctx, t, p, pendientes.OpCambiarTitular, pendientes.ClaveTitular(p.f.ID, tit.CambiosTitular),
				"/servicios/"+strconv.FormatInt(*p.f.ServicioID, 10)+"/cambiar-titular", cuerpo,
				&pendientes.Transito{Campos: sensible, Fotos: fotos}, deps)
			if err != nil {
				return p.f, "", err
			}
			if err := reemplazar(ctx, t, &tit, previo, r.ID, motivoReemplazoToma, &p.actor, m.ahora()); err != nil {
				return p.f, "", err
			}
			tit.QuienFirma, tit.DocumentoTipo, tit.Nombre = ptr("otra"), &n.DocumentoTipo, &nombre
			if err := guardarTitular(ctx, t, q, p.tit, tit); err != nil {
				return p.f, "", err
			}
			if err := t.Emitir(EventoCambioTitular, map[string]any{
				"orden_id": p.f.ID, "pendiente_id": r.ID, "cambio": tit.CambiosTitular, "documento_tipo": n.DocumentoTipo,
				"motivo_codigo": in.MotivoCodigo, "reemplaza_id": reemplazaID(previo),
			}); err != nil {
				return p.f, "", err
			}
			f, err := tocar(ctx, t, q, p.f)
			return f, "", err
		})
}

// --- A43: completar los datos del titular y del lugar ------------------------------------------

func (m *Modulo) nuevoCompletarDatos() *auditoria.Caso[EntradaCompletarDatos, Orden] {
	return accionOrden("ordenes.completar_datos", nucleo.PermOrdenesCompletarDatos, func(e EntradaCompletarDatos) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCompletarDatos) (campobd.CampoOrden, string, error) {
			p, err := m.abrirPaso(ctx, t, q, in.ID, nucleo.PermOrdenesCompletarDatos, in.Sync, "completar los datos de")
			if err != nil {
				return p.f, "", err
			}
			identidad := p.buscar(p.tit.IdentidadPendienteID)
			if identidad == nil {
				return p.f, "", faltaIdentidad()
			}
			if p.tit.DatosN >= maxPasosDatos {
				return p.f, "", nucleo.ReglaNegocio("demasiados_cambios", "Ya se completaron los datos demasiadas veces en esta orden")
			}
			cliente, sensible, campos, err := clienteCompletar(in.Cliente)
			if err != nil {
				return p.f, "", err
			}
			lugar, camposLugar, err := lugarCompletar(in.Servicio)
			if err != nil {
				return p.f, "", err
			}
			campos = append(campos, camposLugar...)
			if len(campos) == 0 {
				return p.f, "", campoInvalido("cliente", "Indica al menos un dato del titular o del lugar", "requerido")
			}
			cuerpo := map[string]any{"origen_tipo": origenTipoOrden, "origen_id": strconv.FormatInt(p.f.ID, 10)}
			if len(cliente) > 0 || len(sensible) > 0 {
				cuerpo["cliente"] = cliente
			}
			if len(lugar) > 0 {
				cuerpo["servicio"] = lugar
			}
			deps := []int64{}
			if p.tit.UltimoPendienteID != nil {
				deps = append(deps, *p.tit.UltimoPendienteID)
			}
			// Con nombre, la identidad registrada en esta instalación (A41 o A39) lo deja corregir.
			if _, ok := cliente["nombre"]; ok && identidad.Estado != pendientes.EstadoResueltoManual {
				i := slices.Index(deps, identidad.ID)
				if i < 0 {
					deps = append(deps, identidad.ID)
					i = len(deps) - 1
				}
				cuerpo["identidad_id"] = pendientes.Ref(i, "identidad_id")
			}
			var transito *pendientes.Transito
			if len(sensible) > 0 {
				transito = &pendientes.Transito{Campos: sensible}
			}
			tit := p.tit
			tit.DatosN++
			r, err := m.encolar(ctx, t, p, pendientes.OpCompletarDatos, pendientes.ClaveDatos(p.f.ID, tit.DatosN),
				"/servicios/"+strconv.FormatInt(*p.f.ServicioID, 10)+"/completar-datos", cuerpo, transito, deps)
			if err != nil {
				return p.f, "", err
			}
			tit.UltimoPendienteID = &r.ID
			if err := guardarTitular(ctx, t, q, p.tit, tit); err != nil {
				return p.f, "", err
			}
			slices.Sort(campos)
			if err := t.Emitir(EventoDatosCompletados, map[string]any{
				"orden_id": p.f.ID, "pendiente_id": r.ID, "n": tit.DatosN, "campos": campos,
			}); err != nil {
				return p.f, "", err
			}
			f, err := tocar(ctx, t, q, p.f)
			return f, "", err
		})
}

// --- A40: cambiar el plan ----------------------------------------------------------------------

func (m *Modulo) nuevoCambiarPlan() *auditoria.Caso[EntradaCambiarPlan, Orden] {
	return accionOrden("ordenes.cambiar_plan", nucleo.PermOrdenesCambiarPlan, func(e EntradaCambiarPlan) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCambiarPlan) (campobd.CampoOrden, string, error) {
			p, err := m.abrirPaso(ctx, t, q, in.ID, nucleo.PermOrdenesCambiarPlan, in.Sync, "cambiar el plan de")
			if err != nil {
				return p.f, "", err
			}
			if p.tit.IdentidadPendienteID == nil {
				return p.f, "", faltaIdentidad()
			}
			if p.tit.CambiosPlan >= maxPasosDatos {
				return p.f, "", nucleo.ReglaNegocio("demasiados_cambios", "Ya se cambió el plan demasiadas veces en esta orden")
			}
			if in.PlanID <= 0 {
				return p.f, "", campoObligatorio("planId")
			}
			cuerpo := map[string]any{"plan_id": in.PlanID, "motivo": in.Motivo,
				"origen_tipo": origenTipoOrden, "origen_id": strconv.FormatInt(p.f.ID, 10)}
			switch in.Motivo {
			case "mas_velocidad", "menos_velocidad":
				if in.MotivoTexto != nil && strings.TrimSpace(*in.MotivoTexto) != "" {
					return p.f, "", campoInvalido("motivoTexto", "Solo con motivo otro", "solo_con_otro")
				}
			case "otro":
				if in.MotivoTexto == nil {
					return p.f, "", campoObligatorio("motivoTexto")
				}
				textoMotivo, err := texto("motivoTexto", *in.MotivoTexto, 10, 500)
				if err != nil {
					return p.f, "", err
				}
				cuerpo["motivo_texto"] = textoMotivo
			default:
				return p.f, "", campoInvalido("motivo", "mas_velocidad, menos_velocidad u otro", "enum")
			}
			tit := p.tit
			tit.CambiosPlan++
			var deps []int64
			if tit.UltimoPendienteID != nil {
				deps = []int64{*tit.UltimoPendienteID}
			}
			r, err := m.encolar(ctx, t, p, pendientes.OpCambiarPlan, pendientes.ClavePlan(p.f.ID, tit.CambiosPlan),
				"/servicios/"+strconv.FormatInt(*p.f.ServicioID, 10)+"/cambiar-plan", cuerpo, nil, deps)
			if err != nil {
				return p.f, "", err
			}
			anterior := tit.PlanID
			tit.UltimoPendienteID, tit.PlanID = &r.ID, &in.PlanID
			if err := guardarTitular(ctx, t, q, p.tit, tit); err != nil {
				return p.f, "", err
			}
			if err := t.Emitir(EventoCambioPlan, map[string]any{
				"orden_id": p.f.ID, "pendiente_id": r.ID, "cambio": tit.CambiosPlan, "plan_id": in.PlanID,
				"plan_anterior_id": anterior, "motivo": in.Motivo,
			}); err != nil {
				return p.f, "", err
			}
			f, err := tocar(ctx, t, q, p.f)
			return f, "", err
		})
}

func faltaIdentidad() *nucleo.Problema {
	return nucleo.ReglaNegocio("falta_identidad", "Primero confirma quién firma y su identidad (titular → datos → plan)")
}

// --- Validar la identidad desde el panel -------------------------------------------------------

// nuevoValidarIdentidad: un supervisor (ordenes.validar_identidad) deja activar la instalación sin
// la identidad registrada en Argos, con motivo (§3.4.8, ajuste n de §10.5). Nunca cambia el titular
// ni registra una identidad por su cuenta:
//
//   - sin identidad tomada: la orden puede cerrarse y activarse (metodo sin_identidad);
//   - con la identidad del mismo titular (A41) rechazada o agotada y su foto aún en el depósito
//     (7 días): la manda otra vez a Argos como revisión desde el panel (metodo revision_panel, con
//     X-Iris-Actor del supervisor), sobre la foto tomada en sitio, que nadie ve en Iris;
//   - si la foto ya se borró: el paso queda resuelto y la activación sale sin la identidad;
//   - un cambio de titular (A39) no se valida: 422 titular_en_revision (lo resuelve un supervisor
//     en Argos o se toma otra vez en sitio).
func (m *Modulo) nuevoValidarIdentidad() *auditoria.Caso[EntradaValidarIdentidad, Orden] {
	return accionOrden("ordenes.validar_identidad", nucleo.PermOrdenesValidarIdentidad, func(EntradaValidarIdentidad) *OpSync { return nil },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaValidarIdentidad) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesValidarIdentidad, &in.Version, nil)
			if err != nil {
				return f, "", err
			}
			if f.Estado == EstadoCancelada {
				return f, "", transicionInvalida("validar la identidad de", f.Estado)
			}
			if err := exigirInstalacion(ctx, q, f); err != nil {
				return f, "", err
			}
			registrado, err := m.conectorRegistrado(ctx, t)
			if err != nil {
				return f, "", err
			}
			if !registrado {
				return f, "", nucleo.ReglaNegocio("argos_sin_conector", "Sin conexión con Argos no hay identidad que validar")
			}
			motivo, err := texto("motivo", in.Motivo, 10, 500)
			if err != nil {
				return f, "", err
			}
			a := contexto.ActorDe(ctx)
			if !a.EsUsuario() {
				return f, "", nucleo.Prohibido("La identidad la valida una persona")
			}
			tit, pasos, err := m.leerPaso(ctx, t, q, f)
			if err != nil {
				return f, "", err
			}
			p := paso{f: f, tit: tit, pasos: pasos, cuando: m.ahora(), actor: a.ID}
			if tit.ValidadaEn != nil {
				return f, "", nucleo.Conflicto("identidad_ya_validada", "La identidad de esta orden ya se validó desde el panel")
			}
			ahora := m.ahora()
			metodo := metodoSinIdentidad
			var nuevo, reemplazado *int64
			if previo := p.buscar(tit.IdentidadPendienteID); previo != nil {
				switch {
				case previo.Operacion == pendientes.OpCambiarTitular && previo.Estado == pendientes.EstadoHecho:
					return f, "", nucleo.Conflicto("identidad_registrada", "La identidad ya está registrada en Argos con el cambio de titular")
				case previo.Operacion == pendientes.OpCambiarTitular:
					return f, "", nucleo.ReglaNegocio("titular_en_revision",
						"El cambio de titular no se valida desde Iris: resuélvelo en Argos o que el técnico lo tome otra vez")
				case previo.Estado == pendientes.EstadoHecho:
					return f, "", nucleo.Conflicto("identidad_registrada", "La identidad ya está registrada en Argos")
				case !previo.Fallido():
					return f, "", nucleo.Conflicto("identidad_en_envio", "La identidad va en camino a Argos; espera su resultado")
				case previo.TransitoVigente && previo.TransitoID != nil:
					// Revisión desde el panel sobre la foto tomada en sitio (A41 con revision_panel).
					antes, err := pendientes.PorID(ctx, t, previo.ID)
					if err != nil {
						return f, "", err
					}
					var cuerpo map[string]any
					if err := json.Unmarshal(antes.Payload, &cuerpo); err != nil {
						return f, "", err
					}
					cuerpo["metodo"], cuerpo["coincide"], cuerpo["verificado_en"] = metodoRevisionPanel, true, ahora.Format(time.RFC3339)
					tit.Tomas++
					r, err := pendientes.EncolarPaso(ctx, t, m.cfg.Titular.Cifrador, pendientes.PasoTitular{
						EmpresaID: f.EmpresaID, OrdenID: f.ID, Operacion: pendientes.OpRegistrarIdentidad,
						Clave: pendientes.ClaveIdentidad(f.ID, tit.Tomas), Ruta: antes.Ruta, Cuerpo: cuerpo,
						TransitoID: previo.TransitoID, TomadoEn: ahora, DependeDe: previo.DependeDe, Actor: pendientes.ActorUsuario(a.ID),
					}, m.cfg.Titular.Encolar)
					if err != nil {
						return f, "", err
					}
					if err := reemplazar(ctx, t, &tit, previo, r.ID, motivoReemplazoRevisionPanel, &a.ID, ahora); err != nil {
						return f, "", err
					}
					metodo, nuevo, reemplazado = metodoRevisionPanel, &r.ID, &previo.ID
				default:
					// La foto ya no está: el paso queda resuelto y la activación sale sin la identidad.
					if err := pendientes.DarPorValidado(ctx, t, previo.ID, "identidad validada desde el panel sin la foto: "+motivo, &a.ID, ahora); err != nil {
						return f, "", err
					}
					reemplazado = &previo.ID
				}
			}
			tit.ValidadaPor, tit.ValidadaMotivo, tit.ValidadaEn = &a.ID, &motivo, &ahora
			if err := guardarTitular(ctx, t, q, p.tit, tit); err != nil {
				return f, "", err
			}
			if err := t.Emitir(EventoIdentidadValidada, map[string]any{
				"orden_id": f.ID, "usuario_id": a.ID, "metodo": metodo, "pendiente_id": nuevo, "reemplaza_id": reemplazado,
			}); err != nil {
				return f, "", err
			}
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

// --- Estado del paso ---------------------------------------------------------------------------

func (m *Modulo) nuevoVerTitular() *auditoria.Caso[EntradaVerOrden, EstadoTitular] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerOrden, EstadoTitular]{
		Accion:       "ordenes.ver_titular",
		Acceso:       auditoria.Autenticado,
		AuditarExito: soloLecturas[EstadoTitular],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerOrden) (EstadoTitular, error) {
			q := campobd.New(t)
			t.FijarEntidad("orden", in.ID)
			f, err := q.OrdenPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return EstadoTitular{}, noEncontrada()
			}
			if err != nil {
				return EstadoTitular{}, err
			}
			if !enEmpresa(ctx, f) || !visible(ctx, f) {
				return EstadoTitular{}, noEncontrada()
			}
			t.FijarEmpresa(deref(f.EmpresaID))
			tit, pasos, err := m.leerPaso(ctx, t, q, f)
			if err != nil {
				return EstadoTitular{}, err
			}
			return estadoTitular(tit, pasos), nil
		},
	})
}

func estadoTitular(tit campobd.CampoOrdenTitular, pasos []pendientes.PasoDeTitular) EstadoTitular {
	e := EstadoTitular{OrdenID: tit.OrdenID, QuienFirma: tit.QuienFirma, DocumentoTipo: tit.DocumentoTipo, Nombre: tit.Nombre,
		Identidad: "falta", PlanID: tit.PlanID, Pasos: make([]PasoTitularVista, 0, len(pasos)), Version: tit.Version}
	switch {
	case tit.ValidadaEn != nil:
		e.Identidad = "validada"
	case tit.IdentidadPendienteID != nil:
		e.Identidad = "registrada"
	}
	for _, p := range pasos {
		v := PasoTitularVista{PendienteID: p.ID, Operacion: p.Operacion, ActualizadoEn: p.ActualizadoEn}
		switch p.Estado {
		case pendientes.EstadoPendiente, pendientes.EstadoEnCurso:
			v.Estado = "enviando"
		case pendientes.EstadoHecho:
			v.Estado = "hecho"
		case pendientes.EstadoFallidoNegocio:
			v.Estado, v.Codigo, e.Pendiente = "rechazado", p.CodigoError, true
			if p.CodigoError != nil && *p.CodigoError == codigoTitularNoCoincide {
				e.EnRevision = true
			}
		case pendientes.EstadoAgotado:
			v.Estado, e.Pendiente = "agotado", true
		case pendientes.EstadoBloqueado:
			v.Estado = "bloqueado"
		default:
			v.Estado = "resuelto"
		}
		e.Pasos = append(e.Pasos, v)
	}
	return e
}

// exigirIdentidad: una instalación de una venta de Iris (prospecto) no se cierra con éxito sin la
// identidad del titular registrada (tomada en sitio) o validada desde el panel (§3.4.8). Sin
// conector (modo sin Argos) no aplica.
func (m *Modulo) exigirIdentidad(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, tipo, resultado string) error {
	if tipo != tipoInstalacion || resultado != "exitosa" || f.ProspectoID == nil {
		return nil
	}
	registrado, err := m.conectorRegistrado(ctx, t)
	if err != nil || !registrado {
		return err
	}
	tit, err := q.TitularDeOrden(ctx, f.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if tit.IdentidadPendienteID != nil || tit.ValidadaEn != nil {
		return nil
	}
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "identidad_requerida", "Falta la identidad del titular",
		"Confirma quién firma y toma la foto de su documento (o que un supervisor la valide desde el panel) antes de cerrar",
		nucleo.ErrorCampo{Field: "titular.identidad", Message: "Sin la identidad del titular la instalación no se activa", Rule: "requerido"})
}

// --- Despacho del lote -------------------------------------------------------------------------

// datosTitularLote: los esquemas de los datos de las operaciones del paso (validación del lote).
var datosTitularLote = []any{EntradaRegistrarIdentidad{}, EntradaCambiarTitular{}, EntradaCompletarDatos{}, EntradaCambiarPlan{}}

// despacharTitular traduce una operación del paso a su caso de uso.
func (m *Modulo) despacharTitular(ctx context.Context, s *OpSync, op OperacionLote) (Orden, error) {
	switch op.Tipo {
	case OpRegistrarIdentidad:
		e, err := datosDe[EntradaRegistrarIdentidad](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.Titular.RegistrarIdentidad.Ejecutar(ctx, e)
	case OpCambiarTitular:
		e, err := datosDe[EntradaCambiarTitular](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.Titular.CambiarTitular.Ejecutar(ctx, e)
	case OpCompletarDatos:
		e, err := datosDe[EntradaCompletarDatos](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.Titular.CompletarDatos.Ejecutar(ctx, e)
	case OpCambiarPlan:
		e, err := datosDe[EntradaCambiarPlan](op)
		if err != nil {
			return Orden{}, err
		}
		e.ID, e.Sync = op.OrdenID, s
		return m.Titular.CambiarPlan.Ejecutar(ctx, e)
	}
	return Orden{}, campoInvalido("tipo", "Tipo de operación desconocido", "enum")
}

// --- Validaciones (los mensajes nunca repiten el valor recibido) ----------------------------------

func marcasDelTecnico(mayor, coincide bool) error {
	var errores []nucleo.ErrorCampo
	if !mayor {
		errores = append(errores, nucleo.ErrorCampo{Field: "mayorDeEdad", Message: "El titular debe ser mayor de edad", Rule: "verdadero"})
	}
	if !coincide {
		errores = append(errores, nucleo.ErrorCampo{Field: "coincide", Message: "Comprueba que la foto del documento es de quien está presente", Rule: "verdadero"})
	}
	if len(errores) > 0 {
		return nucleo.Validacion(errores...)
	}
	return nil
}

var (
	patronPasaporte = regexp.MustCompile(`^[A-Z0-9]{5,20}$`)
	soloDigitos     = regexp.MustCompile(`^[0-9]+$`)
	espacios        = regexp.MustCompile(`\s+`)
)

// documentoValido normaliza y valida el documento de identidad (§3.4.2).
func documentoValido(campo, tipo, numero string) (string, error) {
	n := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(numero)))
	switch tipo {
	case "dpi":
		if !CUIValido(n) {
			return "", campoInvalido(campo, "DPI inválido: 13 dígitos con verificador, departamento y municipio", "dpi")
		}
	case "pasaporte":
		if !patronPasaporte.MatchString(n) {
			return "", campoInvalido(campo, "Pasaporte inválido: de 5 a 20 letras o dígitos", "pasaporte")
		}
	default:
		return "", campoInvalido(campo, "dpi o pasaporte", "enum")
	}
	return n, nil
}

// municipiosPorDepartamento de Guatemala (01 a 22), para el CUI del DPI.
var municipiosPorDepartamento = []int{17, 8, 16, 16, 13, 14, 19, 8, 24, 21, 9, 30, 32, 21, 8, 17, 14, 5, 11, 11, 7, 17}

// CUIValido: 13 dígitos (8 del correlativo, el verificador, 2 del departamento y 2 del
// municipio), con el verificador módulo 11 y un departamento y municipio que existen.
func CUIValido(n string) bool {
	if len(n) != 13 || !soloDigitos.MatchString(n) {
		return false
	}
	depto, _ := strconv.Atoi(n[9:11])
	muni, _ := strconv.Atoi(n[11:13])
	if depto < 1 || depto > len(municipiosPorDepartamento) || muni < 1 || muni > municipiosPorDepartamento[depto-1] {
		return false
	}
	total := 0
	for i := range 8 {
		total += int(n[i]-'0') * (i + 2)
	}
	return total%11 == int(n[8]-'0')
}

// NITValido: dígitos y el verificador módulo 11 (K = 10).
func NITValido(n string) bool {
	if len(n) < 2 || len(n) > 13 {
		return false
	}
	cuerpo, dv := n[:len(n)-1], n[len(n)-1]
	if !soloDigitos.MatchString(cuerpo) {
		return false
	}
	total := 0
	for i := range len(cuerpo) {
		total += int(cuerpo[i]-'0') * (len(cuerpo) + 1 - i)
	}
	esperado := (11 - total%11) % 11
	if esperado == 10 {
		return dv == 'K'
	}
	return dv == byte('0'+esperado)
}

func nombreValido(campo, valor string) (string, error) {
	n := espacios.ReplaceAllString(strings.TrimSpace(valor), " ")
	if c := utf8.RuneCountInString(n); c < 2 || c > 200 {
		return "", campoInvalido(campo, "De 2 a 200 caracteres, como en el documento", "longitud")
	}
	return n, nil
}

// telefonoValido: 8 dígitos de Guatemala (regla de §3.2), normalizado como Argos («+502 5512-3344»).
func telefonoValido(campo, valor string) (string, error) {
	n, ok := formato.TelefonoArgos(valor)
	if !ok || !strings.HasPrefix(n, "+502 ") {
		return "", campoInvalido(campo, "8 dígitos de Guatemala (+502 opcional)", "telefono")
	}
	return n, nil
}

func correoValido(campo, valor string) (string, error) {
	v := strings.TrimSpace(valor)
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || len(v) > 254 {
		return "", campoInvalido(campo, "Correo inválido", "correo")
	}
	return v, nil
}

// fiscalValido: «Facturar a» (§3.4.2): cf sin número; nit con su verificador; cui como el DPI.
func fiscalValido(prefijo, tipo string, numero *string) (string, error) {
	n := ""
	if numero != nil {
		n = strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(*numero)))
	}
	campo := prefijo + ".nitCui"
	switch tipo {
	case "cf":
		if n != "" {
			return "", campoInvalido(campo, "Con consumidor final (cf) no va número", "vacio_con_cf")
		}
	case "nit":
		if !NITValido(n) {
			return "", campoInvalido(campo, "NIT inválido (con dígito verificador; K permitido)", "nit")
		}
	case "cui":
		if !CUIValido(n) {
			return "", campoInvalido(campo, "CUI inválido: 13 dígitos con verificador, departamento y municipio", "cui")
		}
	default:
		return "", campoInvalido(prefijo+".tipoDocumentoFiscal", "cf, nit o cui", "enum")
	}
	return n, nil
}

// fotosDocumento decodifica y valida las fotos (JPEG o PNG, hasta 1 MiB, el tipo que dicen ser).
func fotosDocumento(frente FotoDocumento, reverso *FotoDocumento) ([]pendientes.FotoTransito, error) {
	f, err := fotoDocumento("frente", frente)
	if err != nil {
		return nil, err
	}
	fotos := []pendientes.FotoTransito{f}
	if reverso != nil {
		r, err := fotoDocumento("reverso", *reverso)
		if err != nil {
			return nil, err
		}
		fotos = append(fotos, r)
	}
	return fotos, nil
}

func fotoGrande(campo string) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusRequestEntityTooLarge, "tamano_excedido", "Foto demasiado grande",
		"La foto del documento pasa de 1 MiB: comprímela", nucleo.ErrorCampo{Field: campo, Message: "Hasta 1 MiB", Rule: "tamano"})
}

func fotoDocumento(campo string, f FotoDocumento) (pendientes.FotoTransito, error) {
	crudo := strings.TrimSpace(f.Base64)
	if len(crudo) > maxBase64Foto {
		return pendientes.FotoTransito{}, fotoGrande(campo)
	}
	datos, err := base64.StdEncoding.DecodeString(crudo)
	if err != nil || len(datos) == 0 {
		return pendientes.FotoTransito{}, campoInvalido(campo+".base64", "La foto no está en base64", "base64")
	}
	if len(datos) > MaxFotoDocumento {
		return pendientes.FotoTransito{}, fotoGrande(campo)
	}
	var esperado []byte
	switch f.Mime {
	case "image/jpeg":
		esperado = []byte{0xFF, 0xD8, 0xFF}
	case "image/png":
		esperado = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
	default:
		return pendientes.FotoTransito{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "mime_no_permitido", "Tipo no permitido",
			"La foto del documento va en JPEG o PNG", nucleo.ErrorCampo{Field: campo + ".mime", Message: "image/jpeg o image/png", Rule: "enum"})
	}
	if !bytes.HasPrefix(datos, esperado) {
		return pendientes.FotoTransito{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "mime_distinto", "La foto no es del tipo indicado",
			"El contenido de la foto no coincide con su tipo", nucleo.ErrorCampo{Field: campo, Message: "No es " + f.Mime, Rule: "mime"})
	}
	return pendientes.FotoTransito{Campo: campo, Mime: f.Mime, Datos: datos}, nil
}

// clienteCompletar separa lo que va en el payload (nombre, whatsapp, tipo fiscal, relación) de lo
// sensible (teléfono, correo, NIT/CUI: al depósito). campos: los nombres que cambian (evento).
func clienteCompletar(c *ClienteCompletar) (cuerpo, sensible map[string]any, campos []string, err error) {
	cuerpo, sensible = map[string]any{}, map[string]any{}
	if c == nil {
		return cuerpo, sensible, nil, nil
	}
	if c.Nombre != nil {
		n, err := nombreValido("cliente.nombre", *c.Nombre)
		if err != nil {
			return nil, nil, nil, err
		}
		cuerpo["nombre"], campos = n, append(campos, "nombre")
	}
	if c.Telefono != nil {
		n, err := telefonoValido("cliente.telefono", *c.Telefono)
		if err != nil {
			return nil, nil, nil, err
		}
		sensible["cliente.telefono"], campos = n, append(campos, "telefono")
	}
	if c.Whatsapp != nil {
		cuerpo["whatsapp"], campos = *c.Whatsapp, append(campos, "whatsapp")
	}
	if c.Email != nil {
		n, err := correoValido("cliente.email", *c.Email)
		if err != nil {
			return nil, nil, nil, err
		}
		sensible["cliente.email"], campos = n, append(campos, "email")
	}
	switch {
	case c.TipoDocumentoFiscal != nil:
		n, err := fiscalValido("cliente", *c.TipoDocumentoFiscal, c.NitCui)
		if err != nil {
			return nil, nil, nil, err
		}
		cuerpo["tipo_documento_fiscal"], campos = *c.TipoDocumentoFiscal, append(campos, "tipo_documento_fiscal")
		if n != "" {
			sensible["cliente.nit_cui"], campos = n, append(campos, "nit_cui")
		}
	case c.NitCui != nil:
		return nil, nil, nil, campoInvalido("cliente.tipoDocumentoFiscal", "Indica cf, nit o cui con el número", "requerido")
	}
	if c.Relacion != nil {
		cuerpo["relacion"], campos = *c.Relacion, append(campos, "relacion")
	}
	return cuerpo, sensible, campos, nil
}

// lugarCompletar: dirección, referencia y coordenadas («lat,lng» como Argos) con su precisión.
func lugarCompletar(l *LugarServicio) (map[string]any, []string, error) {
	salida := map[string]any{}
	if l == nil {
		return salida, nil, nil
	}
	var campos []string
	if l.Direccion != nil {
		d, err := texto("servicio.direccion", *l.Direccion, 3, 300)
		if err != nil {
			return nil, nil, err
		}
		salida["direccion"], campos = d, append(campos, "direccion")
	}
	if l.Referencia != nil {
		r, err := texto("servicio.referencia", *l.Referencia, 1, 300)
		if err != nil {
			return nil, nil, err
		}
		salida["referencia"], campos = r, append(campos, "referencia")
	}
	if (l.Lat == nil) != (l.Lon == nil) {
		return nil, nil, campoInvalido("servicio.lat", "Latitud y longitud van juntas", "par")
	}
	if l.Lat != nil {
		if err := validarCoordenadas(l.Lat, l.Lon); err != nil {
			return nil, nil, err
		}
		lat, lon := formato.Coordenadas(l.Lat, l.Lon)
		if lat == nil {
			return nil, nil, campoInvalido("servicio.lat", "Coordenadas inválidas", "coordenadas")
		}
		salida["coordenadas"], campos = strconv.FormatFloat(*lat, 'f', 6, 64)+","+strconv.FormatFloat(*lon, 'f', 6, 64), append(campos, "coordenadas")
		if l.PrecisionM != nil {
			salida["precision_m"] = *l.PrecisionM
		}
	} else if l.PrecisionM != nil {
		return nil, nil, campoInvalido("servicio.precisionM", "La precisión va con las coordenadas", "par")
	}
	return salida, campos, nil
}
