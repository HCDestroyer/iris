package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Pendiente es una acción hacia Argos tal como la ve el panel.
type Pendiente struct {
	ID               int64          `json:"id"`
	Sistema          string         `json:"sistema" enum:"argosisp,horus"`
	Operacion        string         `json:"operacion" doc:"activar_instalacion, asignar_comodato, consumo, devolucion, perdida, retiro, cargo"`
	IdempotencyKey   string         `json:"idempotencyKey" doc:"Idempotency-Key del contrato (iris:<operacion>:<id>)"`
	Entidad          string         `json:"entidad" doc:"orden (más adelante prospecto)"`
	EntidadID        string         `json:"entidadId"`
	Metodo           string         `json:"metodo"`
	Ruta             string         `json:"ruta" doc:"Ruta en la API de Argos (bajo /api/v1)"`
	Payload          map[string]any `json:"payload" doc:"Cuerpo de la llamada (la imagen de la firma, png_base64, como ***)"`
	DependeDe        []int64        `json:"dependeDe" doc:"Pendientes que deben estar hechos antes"`
	DatosIncompletos bool           `json:"datosIncompletos" doc:"La orden no traía un dato obligatorio: no se puede reintentar"`
	Estado           string         `json:"estado" enum:"pendiente,en_curso,hecho,fallido_negocio,agotado,bloqueado,resuelto_manual" doc:"bloqueado: una dependencia quedó fallido_negocio o agotado (ultimoError = depende_de_<id>_fallido); vuelve a pendiente cuando la dependencia termina hecha o resuelto_manual. resuelto_manual: final, un supervisor lo corrigió en Argos (resueltoMotivo); no vuelve a salir"`
	Intentos         int32          `json:"intentos"`
	ProximoIntento   *time.Time     `json:"proximoIntento" nullable:"true" doc:"Solo en pendiente"`
	UltimoError      *string        `json:"ultimoError" nullable:"true" doc:"Resumen del último error (con el detail de Argos en un 4xx de negocio)"`
	UltimoStatus     *int32         `json:"ultimoStatus" nullable:"true" doc:"HTTP de Argos del último intento (null si no respondió)"`
	HechoEn          *time.Time     `json:"hechoEn" nullable:"true"`
	Conflicto        *string        `json:"conflicto" nullable:"true" doc:"cierre_repetido_con_otros_datos: La orden se volvió a cerrar con otros datos para esta acción y no se pisó: corrígelo a mano en Argos (payloadConflicto = datos nuevos)"`
	PayloadConflicto map[string]any `json:"payloadConflicto" nullable:"true"`
	ConflictoEn      *time.Time     `json:"conflictoEn" nullable:"true"`
	Respuesta        any            `json:"respuesta,omitempty" doc:"Cuerpo del primer 2xx de Argos (lo sensible como ***; ausente si no se guardó)"`
	PorResolver      bool           `json:"porResolver,omitempty" doc:"El cuerpo espera la respuesta de una dependencia (marcadores sin resolver)"`
	// Contrato 0.5.0 §3.3.
	CodigoError    *string      `json:"codigoError" nullable:"true" required:"false" doc:"type del último error de Argos (tecnico_no_encontrado, existencia_insuficiente, producto_es_comodato…); en un hecho conciliado, cargo_duplicado o traslado_ya_recibido"`
	Corregible     bool         `json:"corregible" required:"false" doc:"El rechazo es por el técnico (tecnico_no_encontrado, tecnico_sin_bodega, tecnico_ambiguo, empresa_requerida) o Argos volvió a 1.5.x (bodega_id_requerida): vincula bien al técnico en Agenda (o dale bodega en Argos) y reintenta; el reintento manda el vínculo actual o, sin la capacidad inventario_por_tecnico, la bodega de la orden"`
	Cargos         []CargoArgos `json:"cargos,omitempty" doc:"Consumo hecho: cargos únicos que Argos emitió por productos consumible_cobro"`
	FacturaID      *int64       `json:"facturaId,omitempty" doc:"Consumo hecho: factura de Argos con esos cargos (si la hubo)"`
	ResueltoMotivo *string      `json:"resueltoMotivo,omitempty" doc:"resuelto_manual: qué se hizo en Argos"`
	ResueltoPor    *int64       `json:"resueltoPor,omitempty" doc:"resuelto_manual: usuario que lo resolvió"`
	ResueltoEn     *time.Time   `json:"resueltoEn,omitempty"`
	CreadoEn       time.Time    `json:"creadoEn"`
	Version        int32        `json:"version"`

	empresaID *int64
}

func aPendiente(f pendientesbd.IntegracionPendiente) Pendiente {
	p := Pendiente{
		ID: f.ID, Sistema: f.Sistema, Operacion: f.Operacion, IdempotencyKey: f.IdempotencyKey, Entidad: f.Entidad,
		EntidadID: f.EntidadID, Metodo: f.Metodo, Ruta: f.Ruta, DependeDe: f.DependeDe, DatosIncompletos: f.DatosIncompletos,
		Estado: f.Estado, Intentos: f.Intentos, UltimoError: f.UltimoError, UltimoStatus: f.UltimoStatus,
		HechoEn: f.HechoEn, CreadoEn: f.CreatedAt, Version: f.Version, empresaID: f.EmpresaID,
	}
	if p.DependeDe == nil {
		p.DependeDe = []int64{}
	}
	if f.Estado == EstadoPendiente && !f.PorResolver {
		t := f.ProximoIntento
		p.ProximoIntento = &t
	}
	_ = json.Unmarshal(f.Payload, &p.Payload)
	p.Conflicto, p.ConflictoEn = f.Conflicto, f.ConflictoEn
	if len(f.ConflictoPayload) > 0 {
		_ = json.Unmarshal(f.ConflictoPayload, &p.PayloadConflicto)
	}
	if len(f.Respuesta) > 0 {
		_ = json.Unmarshal(f.Respuesta, &p.Respuesta)
	}
	p.PorResolver = f.PorResolver
	p.ResueltoMotivo, p.ResueltoPor, p.ResueltoEn = f.ResueltoMotivo, f.ResueltoPor, f.ResueltoEn
	p.CodigoError, p.Corregible = f.CodigoError, f.Estado == EstadoFallidoNegocio && EsCorregible(f.CodigoError)
	if f.Operacion == OpConsumo {
		p.Cargos, p.FacturaID = cargosDeRespuesta(f.Respuesta)
	}
	if p.Payload == nil {
		p.Payload = map[string]any{}
	}
	ocultarSensible(p.Payload)
	ocultarSensible(p.PayloadConflicto)
	return p
}

// camposOcultos: lo que la API de pendientes nunca muestra (la imagen de la firma de una
// recepción; su sha256 sí, para cotejarla). Se manda a Argos tal cual: solo se oculta al leer.
var camposOcultos = map[string]bool{"png_base64": true}

// ocultarSensible reemplaza por *** los camposOcultos en cualquier nivel del cuerpo.
func ocultarSensible(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, hijo := range x {
			if camposOcultos[k] {
				x[k] = "***"
				continue
			}
			ocultarSensible(hijo)
		}
	case []any:
		for _, hijo := range x {
			ocultarSensible(hijo)
		}
	}
}

// filtroEmpresa: sin conector → empresa_id null; con empresa activa → esa; actor sistema → todas.
func filtroEmpresa(ctx context.Context) (todas bool, empresa *int64) {
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		e := contexto.EmpresaDe(ctx)
		return false, &e
	case contexto.EmpresaNoResuelta:
		return true, nil
	}
	return false, nil
}

func visible(ctx context.Context, empresa *int64) bool {
	todas, activa := filtroEmpresa(ctx)
	if todas {
		return true
	}
	if activa == nil || empresa == nil {
		return activa == nil && empresa == nil
	}
	return *activa == *empresa
}

// EntradaListar filtra la cola.
type EntradaListar struct {
	nucleo.ParamsPagina
	Entidad   string `query:"entidad" pattern:"^[a-z][a-z_]{0,39}$" doc:"orden…"`
	EntidadID string `query:"entidadId" maxLength:"64"`
	Estado    string `query:"estado" enum:"pendiente,en_curso,hecho,fallido_negocio,agotado,bloqueado,resuelto_manual,"`
	// EstadoDistintoDe: estadoDistintoDe=hecho trae todo lo que aún no llegó a Argos en una sola
	// lectura (el tablero de despacho hacía una por estado).
	EstadoDistintoDe string `query:"estadoDistintoDe" enum:"pendiente,en_curso,hecho,fallido_negocio,agotado,bloqueado,resuelto_manual," doc:"Excluye ese estado (estadoDistintoDe=hecho: todo lo que aún no llegó a Argos). Se combina con estado y conflicto"`
	Conflicto        bool   `query:"conflicto" doc:"Solo los que tienen un conflicto por cierre repetido"`
}

func texto(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nuevoListar(*Modulo) *auditoria.Caso[EntradaListar, nucleo.Pagina[Pendiente]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListar, nucleo.Pagina[Pendiente]]{
		Accion: "integraciones.ver_pendientes", AuditarExito: func(nucleo.Pagina[Pendiente]) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListar) (nucleo.Pagina[Pendiente], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Pendiente]{}, err
			}
			todas, empresa := filtroEmpresa(ctx)
			limite := in.Limite()
			filas, err := pendientesbd.New(t).ListarPendientes(ctx, pendientesbd.ListarPendientesParams{
				DespuesDe: despues, Todas: todas, EmpresaID: empresa, Entidad: texto(in.Entidad),
				EntidadID: texto(in.EntidadID), Estado: texto(in.Estado), EstadoDistintoDe: texto(in.EstadoDistintoDe), SoloConflicto: in.Conflicto, Limite: int32(limite + 1), //nolint:gosec // acotado a 201
			})
			if err != nil {
				return nucleo.Pagina[Pendiente]{}, err
			}
			return nucleo.Paginar(filas, limite, aPendiente, func(f pendientesbd.IntegracionPendiente) []any { return []any{f.ID} }), nil
		},
	})
}

// EntradaReintentar es el reintento manual de un pendiente.
type EntradaReintentar struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

type eventoReintentado struct {
	PendienteID int64  `json:"pendiente_id"`
	Operacion   string `json:"operacion"`
	Entidad     string `json:"entidad"`
	EntidadID   string `json:"entidad_id"`
	De          string `json:"de"`
}

func nuevoReintentar(m *Modulo) *auditoria.Caso[EntradaReintentar, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReintentar, Pendiente]{
		Accion: "integraciones.reintentar",
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReintentar) (Pendiente, error) {
			q := pendientesbd.New(t)
			f, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			if !visible(ctx, f.EmpresaID) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			t.FijarEntidad("pendiente", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			if err := nucleo.ExigirVersion(f.Version, in.Version, "pendiente", f.ID); err != nil {
				return Pendiente{}, err
			}
			switch {
			case f.DatosIncompletos:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_incompleto",
					"La orden no traía los datos para esta acción ("+deref(f.UltimoError)+"); vuelve a cerrar la orden con los datos "+
						"(el pendiente se corrige y vuelve a la cola) o hazlo a mano en Argos")
			case f.Estado == EstadoBloqueado && len(f.DependeDe) == 0:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_esperando",
					"Espera algo de Iris ("+deref(f.UltimoError)+", p. ej. que termine de subir la firma); sale solo hacia Argos cuando llegue")
			case f.Estado == EstadoBloqueado:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_no_reintentable",
					"Está bloqueado ("+deref(f.UltimoError)+"): reintenta su dependencia; al quedar hecha, este vuelve a la cola")
			case f.Estado == EstadoHecho || f.Estado == EstadoEnCurso || f.Estado == EstadoResueltoManual:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_no_reintentable", "El pendiente está "+f.Estado)
			}
			if corregido, ok, err := m.corregirTecnico(ctx, t, f, in.Version); err != nil || ok {
				return corregido, err
			}
			nueva, err := q.ReintentarPendiente(ctx, pendientesbd.ReintentarPendienteParams{Ahora: m.ahora(), ID: f.ID, Version: in.Version})
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": f.Estado, "intentos": f.Intentos},
				map[string]any{"estado": nueva.Estado, "intentos": nueva.Intentos})
			if err := t.Emitir(EventoReintentado, eventoReintentado{
				PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID, De: f.Estado,
			}); err != nil {
				return Pendiente{}, err
			}
			return aPendiente(nueva), m.avisarCola(ctx, t.Tx)
		},
	})
}

// EntradaResolverManual: dar por resuelto a mano un pendiente que Argos rechazó por negocio o que
// nació incompleto, después de corregirlo directo en Argos.
type EntradaResolverManual struct {
	ID      int64  `json:"-"`
	Version int32  `json:"version" minimum:"1"`
	Motivo  string `json:"motivo" minLength:"5" maxLength:"1000" doc:"Qué se hizo en Argos (queda en la auditoría y en el pendiente)"`
	// Cargo: obligatorio si un cargo único esperaba a este consumo por un producto del consumo.
	Cargo string `json:"cargo,omitempty" required:"false" enum:"liberar,resolver_tambien" doc:"Solo al resolver un consumo con un cargo de un producto del consumo esperándolo (si no, 422 decision_cargo_requerida). liberar: el cargo sale hacia Argos (el consumo hecho en Argos no lo cobró). resolver_tambien: el cargo también queda resuelto_manual con el mismo motivo y no sale (Argos ya lo cobró al consumir un consumible_cobro)"`
}

// Decisiones sobre el cargo que esperaba al consumo resuelto a mano.
const (
	CargoLiberar         = "liberar"
	CargoResolverTambien = "resolver_tambien"
)

// MotivoDependenciaResueltaAMano: el dependiente esperaba la respuesta (marcadores) de un pendiente
// que se resolvió a mano; queda incompleto.
const MotivoDependenciaResueltaAMano = "dependencia_resuelta_a_mano"

type eventoResueltoManual struct {
	PendienteID    int64  `json:"pendiente_id"`
	Operacion      string `json:"operacion"`
	Entidad        string `json:"entidad"`
	EntidadID      string `json:"entidad_id"`
	IdempotencyKey string `json:"idempotency_key"`
	De             string `json:"de"`
	Motivo         string `json:"motivo"`
	UsuarioID      *int64 `json:"usuario_id"`
}

// nuevoResolverManual: solo desde fallido_negocio (incluido el incompleto). Lo agotado se
// reintenta y lo bloqueado espera a su dependencia: ninguno se puede saltar. Queda
// resuelto_manual (final, nunca vuelve a salir) y sus dependientes:
//
//   - los que esperaban su respuesta (marcadores) nunca la tendrán: quedan incompletos
//     (dependencia_resuelta_a_mano), para resolverlos también a mano o corregirlos con los datos;
//   - un cargo de un producto del consumo exige la decisión explícita (cargo): liberar o
//     resolver_tambien, porque el consumo rehecho en Argos sin el origen_id de la orden pudo
//     cobrarlo ya (consumible_cobro) y Argos no respondería 409 cargo_duplicado;
//   - el resto sale como si estuviera hecho.
func nuevoResolverManual(m *Modulo) *auditoria.Caso[EntradaResolverManual, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResolverManual, Pendiente]{
		Accion: "integraciones.resolver_pendiente",
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResolverManual) (Pendiente, error) {
			q := pendientesbd.New(t)
			f, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			if !visible(ctx, f.EmpresaID) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			t.FijarEntidad("pendiente", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			if err := nucleo.ExigirVersion(f.Version, in.Version, "pendiente", f.ID); err != nil {
				return Pendiente{}, err
			}
			motivo := strings.TrimSpace(in.Motivo)
			if n := utf8.RuneCountInString(motivo); n < 5 || n > 1000 {
				return Pendiente{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "motivo", Message: "Escribe qué se hizo en Argos (5 a 1000 caracteres)", Rule: "longitud"})
			}
			if in.Cargo != "" && in.Cargo != CargoLiberar && in.Cargo != CargoResolverTambien {
				return Pendiente{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "cargo", Message: "liberar o resolver_tambien", Rule: "enum"})
			}
			if f.Estado != EstadoFallidoNegocio {
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_no_resoluble",
					"Solo se resuelve a mano lo que Argos rechazó (fallido_negocio); está "+f.Estado+
						": lo agotado se reintenta y lo bloqueado espera a su dependencia")
			}
			dependientes, err := q.DependientesAbiertos(ctx, f.ID)
			if err != nil {
				return Pendiente{}, err
			}
			cargos := cargosDelConsumo(f, dependientes)
			if len(cargos) > 0 && in.Cargo == "" {
				ids := make([]string, 0, len(cargos))
				for _, c := range cargos {
					ids = append(ids, strconv.FormatInt(c.ID, 10))
				}
				return Pendiente{}, nucleo.NuevoProblema(http.StatusUnprocessableEntity, "decision_cargo_requerida", "Decide qué pasa con el cargo",
					"El cargo único (pendiente "+strings.Join(ids, ", ")+") espera a este consumo y es de un producto del consumo: si el "+
						"consumo hecho a mano en Argos ya lo cobró (consumible_cobro), manda cargo = resolver_tambien; si no, "+
						"cargo = liberar y sale hacia Argos",
					nucleo.ErrorCampo{Field: "cargo", Message: "Obligatorio: liberar o resolver_tambien", Rule: "requerido"})
			}
			var usuario *int64
			if a := contexto.ActorDe(ctx); a.EsUsuario() {
				id := a.ID
				usuario = &id
			}
			ahora := m.ahora()
			nueva, err := q.ResolverManual(ctx, pendientesbd.ResolverManualParams{
				Motivo: motivo, ResueltoPor: usuario, Ahora: ahora, ID: f.ID, Version: in.Version,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": f.Estado},
				map[string]any{"estado": nueva.Estado, "resuelto_motivo": motivo})
			if err := t.Emitir(EventoResueltoManual, eventoResueltoManual{
				PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID,
				IdempotencyKey: f.IdempotencyKey, De: f.Estado, Motivo: motivo, UsuarioID: usuario,
			}); err != nil {
				return Pendiente{}, err
			}
			if in.Cargo == CargoResolverTambien {
				for _, c := range cargos {
					if err := m.resolverCargoTambien(ctx, t, q, c, motivo, usuario, ahora); err != nil {
						return Pendiente{}, err
					}
				}
			}
			for _, d := range dependientes {
				if d.PorResolver {
					if err := m.fallarPorResolucionManual(ctx, t, q, d, f.ID); err != nil {
						return Pendiente{}, err
					}
				}
			}
			if err := m.desbloquearDependientes(ctx, t, f.ID, ahora); err != nil {
				return Pendiente{}, err
			}
			return aPendiente(nueva), m.avisarCola(ctx, t.Tx)
		},
	})
}

// cargosDelConsumo: los cargos dependientes de un consumo cuyo producto_id está en sus lineas[].
func cargosDelConsumo(f pendientesbd.IntegracionPendiente, dependientes []pendientesbd.IntegracionPendiente) []pendientesbd.IntegracionPendiente {
	if f.Operacion != OpConsumo {
		return nil
	}
	var consumo struct {
		Lineas []struct {
			ProductoID json.Number `json:"producto_id"`
		} `json:"lineas"`
	}
	if json.Unmarshal(f.Payload, &consumo) != nil {
		return nil
	}
	productos := map[string]bool{}
	for _, l := range consumo.Lineas {
		if l.ProductoID != "" {
			productos[l.ProductoID.String()] = true
		}
	}
	var salida []pendientesbd.IntegracionPendiente
	for _, d := range dependientes {
		if d.Operacion != OpCargo {
			continue
		}
		var cargo struct {
			ProductoID json.Number `json:"producto_id"`
		}
		if json.Unmarshal(d.Payload, &cargo) == nil && cargo.ProductoID != "" && productos[cargo.ProductoID.String()] {
			salida = append(salida, d)
		}
	}
	return salida
}

// resolverCargoTambien: cargo = resolver_tambien. Mismo motivo, auditado y con su evento; no sale.
func (m *Modulo) resolverCargoTambien(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries,
	c pendientesbd.IntegracionPendiente, motivo string, usuario *int64, ahora time.Time) error {
	g, err := q.ResolverManualDependiente(ctx, pendientesbd.ResolverManualDependienteParams{
		Motivo: motivo, ResueltoPor: usuario, Ahora: ahora, ID: c.ID,
	})
	if err != nil {
		return err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"estado": c.Estado},
		map[string]any{"estado": g.Estado, "resuelto_motivo": motivo})
	if err := t.Emitir(EventoResueltoManual, eventoResueltoManual{
		PendienteID: g.ID, Operacion: g.Operacion, Entidad: g.Entidad, EntidadID: g.EntidadID,
		IdempotencyKey: g.IdempotencyKey, De: c.Estado, Motivo: motivo, UsuarioID: usuario,
	}); err != nil {
		return err
	}
	return m.desbloquearDependientes(ctx, t, g.ID, ahora)
}

// fallarPorResolucionManual: el dependiente esperaba la respuesta del pendiente resuelto a mano.
func (m *Modulo) fallarPorResolucionManual(ctx context.Context, t *auditoria.Tx, q *pendientesbd.Queries,
	d pendientesbd.IntegracionPendiente, padre int64) error {
	motivo := fmt.Sprintf("%s: el pendiente %d se resolvió a mano y este esperaba su respuesta; resuélvelo también a mano o corrígelo con los datos reales",
		MotivoDependenciaResueltaAMano, padre)
	g, err := q.FallarPorResolucionManual(ctx, pendientesbd.FallarPorResolucionManualParams{Motivo: motivo, ID: d.ID})
	if err != nil {
		return err
	}
	t.RegistrarCambios("pendiente", g.ID, map[string]any{"estado": d.Estado},
		map[string]any{"estado": g.Estado, "datos_incompletos": true, "codigo_error": MotivoDependenciaResueltaAMano})
	if err := t.Emitir(EventoFallido, eventoFallo{eventoPendiente: eventoDe(g), Intentos: g.Intentos, Detalle: motivo}); err != nil {
		return err
	}
	return bloquearDependientes(ctx, t, g.ID, g.Estado)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// DeEntidad devuelve los pendientes de una entidad (p. ej. para mostrar en la orden «cerrada
// con pendiente en Argos»). No filtra por empresa: quien llama ya validó la entidad.
func DeEntidad(ctx context.Context, q pendientesbd.DBTX, entidad, entidadID string) ([]Pendiente, error) {
	filas, err := pendientesbd.New(q).PendientesDeEntidad(ctx, pendientesbd.PendientesDeEntidadParams{Entidad: entidad, EntidadID: entidadID})
	if err != nil {
		return nil, err
	}
	salida := make([]Pendiente, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aPendiente(f))
	}
	return salida, nil
}

// ConPendienteEnArgos indica si alguno no está hecho ni resuelto a mano («cerrada con pendiente en Argos»).
func ConPendienteEnArgos(ps []Pendiente) bool {
	for _, p := range ps {
		if p.Estado != EstadoHecho && p.Estado != EstadoResueltoManual {
			return true
		}
	}
	return false
}

func errorf(formato string, args ...any) *string {
	s := fmt.Sprintf(formato, args...)
	return &s
}

// corregirTecnico: el reintento a mano de un pendiente rechazado por el técnico (CodigosCorregibles):
//
//   - si el cuerpo lleva tecnico y Argos ya no lo entiende (volvió a 1.5.x: sin la capacidad), se
//     replanifica con bodega_id (la de la orden; sin ella, 422 bodega_requerida) o, en el comodato,
//     sin tecnico;
//   - si no, manda el vínculo actual del técnico. Si cambió, el cuerpo se corrige; si es el mismo,
//     sigue el reintento normal (p. ej. le dieron bodega en Argos). Sin vínculo: 422
//     tecnico_sin_vincular.
//
// Corregido: integracion.pendiente_corregido y vuelve a la cola. ok = ya se corrigió y encoló.
func (m *Modulo) corregirTecnico(ctx context.Context, t *auditoria.Tx, f pendientesbd.IntegracionPendiente, version int32) (Pendiente, bool, error) {
	if f.Estado != EstadoFallidoNegocio || !EsCorregible(f.CodigoError) {
		return Pendiente{}, false, nil
	}
	var cuerpo map[string]any
	if json.Unmarshal(f.Payload, &cuerpo) != nil {
		return Pendiente{}, false, nil
	}
	campo := "tecnico"
	if _, ok := cuerpo[campo]; !ok {
		campo = "recibido_por" // recepción de un traslado
		if _, ok := cuerpo[campo]; !ok {
			return Pendiente{}, false, nil
		}
	}
	q := pendientesbd.New(t)
	anterior, _ := cuerpo[campo].(map[string]any)
	porTecnico := true
	switch {
	case campo == "tecnico" && deref(f.CodigoError) == CodigoBodegaRequerida:
		// Argos mismo dijo que no entiende tecnico (1.5.x): se replanifica con la bodega sin esperar
		// a que la revisión del contrato borre la versión guardada.
		porTecnico = false
	case campo == "tecnico" && m.cfg.Argos != nil:
		var err error
		if porTecnico, err = m.cfg.Argos.InventarioPorTecnico(ctx, t); err != nil {
			return Pendiente{}, false, err
		}
	}
	if !porTecnico {
		delete(cuerpo, "tecnico")
		if f.Operacion == OpConsumo || f.Operacion == OpDevolucion {
			bodega, err := m.bodegaDe(ctx, q, f)
			if err != nil {
				return Pendiente{}, false, err
			}
			cuerpo["bodega_id"] = bodega
		}
	} else {
		if f.TecnicoUsuarioID == nil {
			return Pendiente{}, false, nil
		}
		actual, err := q.VinculoArgosDeTecnico(ctx, *f.TecnicoUsuarioID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && actual == "") {
			return Pendiente{}, false, nucleo.ReglaNegocio("tecnico_sin_vincular",
				"Argos rechazó al técnico ("+deref(f.CodigoError)+") y su ficha no está vinculada a un usuario de Argos: vincúlala en Agenda y reintenta")
		}
		if err != nil {
			return Pendiente{}, false, err
		}
		if anterior != nil && anterior["id_externo"] == actual {
			return Pendiente{}, false, nil
		}
		cuerpo[campo] = map[string]any{"id_externo": actual}
	}
	nuevo, err := json.Marshal(cuerpo)
	if err != nil {
		return Pendiente{}, false, err
	}
	g, err := q.CorregirTecnico(ctx, pendientesbd.CorregirTecnicoParams{Payload: nuevo, Ahora: m.ahora(), ID: f.ID, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return Pendiente{}, false, nucleo.ConflictoVersion("pendiente", f.ID)
	}
	if err != nil {
		return Pendiente{}, false, err
	}
	// Solo lo que cambió (técnico o bodega), nunca el cuerpo entero: el de una recepción lleva la
	// firma (png_base64).
	t.RegistrarCambios("pendiente", g.ID,
		map[string]any{"estado": f.Estado, "codigo_error": f.CodigoError, campo: anterior, "bodega_id": nil},
		map[string]any{"estado": g.Estado, "codigo_error": g.CodigoError, campo: cuerpo[campo], "bodega_id": cuerpo["bodega_id"]})
	if err := t.Emitir(EventoCorregido, eventoDe(g)); err != nil {
		return Pendiente{}, false, err
	}
	return aPendiente(g), true, m.avisarCola(ctx, t.Tx)
}

// bodegaDe: la bodega de la orden del pendiente (422 bodega_requerida si no tiene).
func (m *Modulo) bodegaDe(ctx context.Context, q *pendientesbd.Queries, f pendientesbd.IntegracionPendiente) (int64, error) {
	sinBodega := nucleo.ReglaNegocio("bodega_requerida",
		"Argos ya no entiende tecnico (anterior a 1.6.0) y la orden no tiene bodega: asígnale la bodega del técnico y reintenta")
	id, err := strconv.ParseInt(f.EntidadID, 10, 64)
	if f.Entidad != "orden" || err != nil {
		return 0, sinBodega
	}
	b, err := q.BodegaDeOrden(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && b == nil) {
		return 0, sinBodega
	}
	if err != nil {
		return 0, err
	}
	return *b, nil
}
