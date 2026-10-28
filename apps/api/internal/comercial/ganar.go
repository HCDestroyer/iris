package comercial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	comercialbd "github.com/HCDestroyer/iris/apps/api/internal/comercial/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Ganar un prospecto (contratos/argosisp.md §3, «Prospecto ganado»):
//
//   - con conector (activo o apagado, como la cola de I1): se encola POST /clientes
//     (iris:cliente:<prospecto_id>). Cuando queda hecho, comercial recupera el id del cliente y
//     encola POST /servicios en pendiente_instalacion (iris:servicio:<prospecto_id>), que depende
//     del primero. Cuando ese queda hecho, crea la orden de instalación con el cliente y el
//     servicio de Argos (origen sistema), una sola vez.
//   - sin conector (modo local): crea la orden con los datos capturados y el prospecto queda
//     ganado sin ids de Argos.
//
// La cola de I1 descarta el cuerpo de la respuesta de Argos: para saber el id que Argos dio al
// cliente (y al servicio), comercial repite la llamada con la MISMA Idempotency-Key, que Argos
// responde con el resultado guardado sin crear otro (RecuperadorAltas). Pedido a I1 (i2.md):
// guardar la respuesta del 2xx en el pendiente y armar el cuerpo de un dependiente con datos de
// su dependencia; con eso los dos pendientes se encolan juntos al ganar y la repetición sobra.

// Operaciones de la cola (integracion.pendientes.operacion).
const (
	OpCrearCliente  = "crear_cliente"
	OpCrearServicio = "crear_servicio"
	OrigenTipo      = "iris_prospecto"
)

// Estados del alta.
const (
	AltaLocal             = "local"
	AltaArgos             = "argos"
	AltaEsperandoCliente  = "esperando_cliente"
	AltaEsperandoServicio = "esperando_servicio"
	AltaCompleta          = "completa"
)

// ClaveCliente es la Idempotency-Key del alta del cliente.
func ClaveCliente(prospecto int64) string { return "iris:cliente:" + idTexto(prospecto) }

// ClaveServicio es la Idempotency-Key del alta del servicio.
func ClaveServicio(prospecto int64) string { return "iris:servicio:" + idTexto(prospecto) }

// RecuperadorAltas devuelve el id que Argos dio a un alta ya hecha por la cola (POST /clientes,
// POST /servicios), repitiendo la llamada con la misma Idempotency-Key y el mismo cuerpo. Fuera
// de toda transacción.
type RecuperadorAltas interface {
	Recuperar(ctx context.Context, p pendientesbd.IntegracionPendiente) (int64, error)
}

// AltasDeArgos repite la llamada por el cliente del conector Argos activo.
func AltasDeArgos(am *argos.Modulo) RecuperadorAltas { return altasArgos{am: am} }

type altasArgos struct{ am *argos.Modulo }

// ErrAltaInconsistente: el id que devolvió Argos no es del alta del prospecto (codigo_externo o
// cliente distintos). No se reintenta: el alta queda en error para revisión manual.
var ErrAltaInconsistente = errors.New("comercial: el recurso de Argos no corresponde al alta del prospecto")

func (a altasArgos) Recuperar(ctx context.Context, p pendientesbd.IntegracionPendiente) (int64, error) {
	cli, err := a.am.ClienteActivo(ctx, nucleo.BDDe(ctx))
	if err != nil {
		return 0, err
	}
	var r struct {
		ID json.Number `json:"id"`
	}
	// Mismos bytes que el primer envío: el cuerpo guardado en el pendiente, sin reserializar.
	if err := cli.Escribir(ctx, p.Metodo, p.Ruta, json.RawMessage(p.Payload), p.IdempotencyKey, &conector.Recurso{Destino: &r}); err != nil {
		return 0, err
	}
	id, err := r.ID.Int64()
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: Argos no devolvió el id en %s %s (%s)", ErrAltaInconsistente, p.Metodo, p.Ruta, p.IdempotencyKey)
	}
	// El cliente de Argos no tiene empresa: se comprueba que sea el del prospecto por su
	// codigo_externo; el servicio, por su cliente.
	var enviado struct {
		CodigoExterno string      `json:"codigo_externo"`
		ClienteID     json.Number `json:"cliente_id"`
	}
	_ = json.Unmarshal(p.Payload, &enviado)
	var leido struct {
		CodigoExterno string      `json:"codigo_externo"`
		ClienteID     json.Number `json:"cliente_id"`
	}
	if err := cli.Pedir(ctx, http.MethodGet, p.Ruta+"/"+strconv.FormatInt(id, 10), nil, nil, true, &conector.Recurso{Destino: &leido}); err != nil {
		return 0, err
	}
	switch p.Ruta {
	case "/clientes":
		if enviado.CodigoExterno == "" || leido.CodigoExterno != enviado.CodigoExterno {
			return 0, fmt.Errorf("%w: el cliente %d de Argos tiene codigo_externo %q (se esperaba %q)", ErrAltaInconsistente, id, leido.CodigoExterno, enviado.CodigoExterno)
		}
	case "/servicios":
		if leido.ClienteID.String() != enviado.ClienteID.String() {
			return 0, fmt.Errorf("%w: el servicio %d de Argos es del cliente %s (se esperaba %s)", ErrAltaInconsistente, id, leido.ClienteID, enviado.ClienteID)
		}
	}
	return id, nil
}

// EntradaGanar gana el prospecto.
type EntradaGanar struct {
	ID            int64  `json:"-"`
	Version       int32  `json:"version" minimum:"1"`
	ConfirmarObra bool   `json:"confirmarObra,omitempty" doc:"Obligatorio (true) si la factibilidad es requiere_obra"`
	RouterID      *int64 `json:"routerId,omitempty" minimum:"1" doc:"Router de Argos del servicio (de la empresa del prospecto). Por defecto, el sugerido por la factibilidad; con conector y sin router sugerido (manual sin router), obligatorio: 422 router_requerido"`
}

// requisitos de ganar: contrato firmado, su cotización aceptada y la factibilidad al día.
type requisitos struct {
	contrato     comercialbd.ComercialContrato
	cotizacion   comercialbd.ComercialCotizacion
	factibilidad comercialbd.ComercialFactibilidad
}

func exigirRequisitos(ctx context.Context, q *comercialbd.Queries, p comercialbd.ComercialProspecto, confirmarObra bool) (requisitos, error) {
	var r requisitos
	var err error
	r.contrato, err = q.ContratoVivoDeProspecto(ctx, p.ID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && r.contrato.Estado != "firmado") {
		return r, nucleo.ReglaNegocio("contrato_no_firmado", "Para ganar, el contrato debe estar firmado por el cliente")
	}
	if err != nil {
		return r, err
	}
	if r.cotizacion, err = q.CotizacionPorID(ctx, r.contrato.CotizacionID); err != nil {
		return r, err
	}
	if r.cotizacion.Estado != "aceptada" {
		return r, nucleo.ReglaNegocio("cotizacion_no_aceptada", "La cotización del contrato no está aceptada")
	}
	if r.factibilidad, err = exigirFactible(ctx, q, p.ID); err != nil {
		return r, err
	}
	fa := r.factibilidad
	if !mismaCoordenada(fa.Lat, p.Lat) || !mismaCoordenada(fa.Lon, p.Lon) {
		return r, nucleo.ReglaNegocio("factibilidad_desactualizada", "Las coordenadas del prospecto cambiaron después de la factibilidad; evalúala de nuevo")
	}
	if fa.Resultado == FactibleRequiereObra && !confirmarObra {
		return r, nucleo.Validacion(nucleo.ErrorCampo{Field: "confirmarObra", Rule: "requiere_obra",
			Message: "La factibilidad requiere obra: confirma explícitamente (confirmarObra: true) que se acordó con el cliente"})
	}
	return r, nil
}

func mismaCoordenada(a, b *float64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func (m *Modulo) nuevoGanar() *auditoria.Caso[EntradaGanar, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[EntradaGanar, Prospecto]{
		Accion: "prospectos.ganar", Permiso: nucleo.PermProspectosGanar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaGanar) (Prospecto, error) {
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermProspectosGanar, &in.Version)
			if err != nil {
				return Prospecto{}, err
			}
			if err := exigirEstado(antes, "ganar", EstadoContrato); err != nil {
				return Prospecto{}, err
			}
			r, err := exigirRequisitos(ctx, q, antes, in.ConfirmarObra)
			if err != nil {
				return Prospecto{}, err
			}
			conector, err := q.EstadoConectorArgos(ctx)
			if err != nil {
				return Prospecto{}, err
			}
			ahora := m.ahora()
			d := antes
			d.Estado, d.GanadoEn, d.ObraConfirmada = EstadoGanado, &ahora, r.factibilidad.Resultado == FactibleRequiereObra
			evento := map[string]any{"prospecto_id": antes.ID, "empresa_id": antes.EmpresaID, "contrato_id": r.contrato.ID,
				"cotizacion_id": r.cotizacion.ID, "plan_id": r.cotizacion.PlanID, "obra_confirmada": d.ObraConfirmada}
			if conector == "" {
				// Modo sin conector: la orden con los datos capturados, ya.
				o, err := m.crearOrdenInstalacion(ctx, antes, r, nil, nil)
				if err != nil {
					return Prospecto{}, err
				}
				d.AltaModo, d.AltaEstado, d.OrdenID = ptr(AltaLocal), ptr(AltaCompleta), &o.ID
				evento["modo"], evento["orden_id"], evento["pendientes"] = AltaLocal, o.ID, []int64{}
			} else {
				if antes.EmpresaID == nil {
					return Prospecto{}, nucleo.ReglaNegocio("empresa_requerida",
						"El prospecto no tiene empresa (se capturó sin conector): adopta los datos sin empresa antes de ganarlo")
				}
				if r.cotizacion.PlanID == nil {
					return Prospecto{}, nucleo.ReglaNegocio("plan_requerido",
						"La cotización aceptada no tiene un plan de Argos (se hizo sin conector): crea una revisión con el plan y vuelve a contratar")
				}
				router := r.factibilidad.RouterID
				if in.RouterID != nil {
					router = in.RouterID
				}
				if router == nil {
					return Prospecto{}, nucleo.ReglaNegocio("router_requerido",
						"Argos exige el router del servicio y la factibilidad no sugirió uno: indica routerId al ganar")
				}
				if _, err := routerDeEmpresa(ctx, q, *router, antes.EmpresaID); err != nil {
					return Prospecto{}, err
				}
				d.RouterID = router
				evento["router_id"] = *router
				p, err := m.encolar(ctx, t, pendientesbd.InsertarPendienteParams{
					EmpresaID: antes.EmpresaID, Operacion: OpCrearCliente, IdempotencyKey: ClaveCliente(antes.ID),
					Ruta: "/clientes", Payload: payloadCliente(antes), DependeDe: []int64{},
				}, ahora)
				if err != nil {
					return Prospecto{}, err
				}
				d.AltaModo, d.AltaEstado = ptr(AltaArgos), ptr(AltaEsperandoCliente)
				evento["modo"], evento["orden_id"], evento["pendientes"] = AltaArgos, nil, []int64{p.ID}
			}
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			if err := historial(ctx, t, q, f.ID, "ganado", nil, f.AltaModo, nil, evento); err != nil {
				return Prospecto{}, err
			}
			if err := t.Emitir("prospecto.ganado", evento); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), cambioDeEstado(ctx, t, q, antes, f, nil)
		},
	})
}

// CodigoExterno identifica al prospecto en el cliente de Argos (POST /clientes codigo_externo).
// Tras recuperar el id del cliente se comprueba con GET /clientes/{id}.
func CodigoExterno(prospecto int64) string { return "iris:prospecto:" + idTexto(prospecto) }

// coordenadas en el formato de Argos ("lat,lng"), o "" sin ellas.
func coordenadas(lat, lon *float64) string {
	if lat == nil || lon == nil {
		return ""
	}
	return strconv.FormatFloat(*lat, 'f', -1, 64) + "," + strconv.FormatFloat(*lon, 'f', -1, 64)
}

// conDatos agrega al cuerpo solo los opcionales con valor (Argos guarda la huella del cuerpo
// crudo: nada de nulls de más).
func conDatos(cuerpo map[string]any, opcionales map[string]any) map[string]any {
	for k, v := range opcionales {
		switch x := v.(type) {
		case *string:
			if x != nil && *x != "" {
				cuerpo[k] = *x
			}
		case string:
			if x != "" {
				cuerpo[k] = x
			}
		case *int64:
			if x != nil {
				cuerpo[k] = *x
			}
		}
	}
	return cuerpo
}

// payloadCliente: cuerpo de POST /clientes (Argos 1.2). Sin empresa_id (el cliente es de la
// instalación); codigo_externo = iris:prospecto:<id>. Se serializa UNA vez y se guarda en el
// pendiente: la cola y la repetición mandan esos mismos bytes (la huella de Argos es el cuerpo crudo).
func payloadCliente(p comercialbd.ComercialProspecto) []byte {
	b, _ := json.Marshal(conDatos(map[string]any{"nombre": p.Nombre, "codigo_externo": CodigoExterno(p.ID)}, map[string]any{
		"nit_cui": p.Documento, "email": p.Correo, "telefono": p.Telefono, "direccion": p.Direccion,
		"coordenadas": coordenadas(p.Lat, p.Lon), "zona_id": p.ZonaID,
	}))
	return b
}

// payloadServicio: cuerpo de POST /servicios (Argos 1.2): cliente, router y plan obligatorios,
// empresa_id siempre (la del prospecto) y estado_inicial pendiente_instalacion (A1). Sin
// origen_tipo ni origen_id (van solo en activar-instalacion).
func payloadServicio(p comercialbd.ComercialProspecto, r requisitos, cliente int64) []byte {
	b, _ := json.Marshal(conDatos(map[string]any{
		"cliente_id": cliente, "router_id": deref(p.RouterID), "plan_id": deref(r.cotizacion.PlanID),
		"empresa_id": deref(p.EmpresaID), "estado_inicial": "pendiente_instalacion",
	}, map[string]any{"direccion": p.Direccion, "coordenadas": coordenadas(p.Lat, p.Lon)}))
	return b
}

// encolar inserta un pendiente hacia Argos (idempotente por su Idempotency-Key) y avisa a la cola.
func (m *Modulo) encolar(ctx context.Context, t *auditoria.Tx, p pendientesbd.InsertarPendienteParams, ahora time.Time) (pendientesbd.IntegracionPendiente, error) {
	p.Sistema, p.Entidad, p.Metodo, p.Estado, p.ProximoIntento = argos.Sistema, EntidadPendientes, "POST", pendientes.EstadoPendiente, ahora
	if p.EntidadID == "" {
		p.EntidadID = idTexto(prospectoDeClave(p.IdempotencyKey))
	}
	pq := pendientesbd.New(t)
	f, err := pq.InsertarPendiente(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return pq.PendientePorClave(ctx, p.IdempotencyKey)
	}
	if err != nil {
		return f, err
	}
	t.RegistrarCambios("pendiente", f.ID, nil, map[string]any{"operacion": f.Operacion, "estado": f.Estado, "idempotency_key": f.IdempotencyKey})
	if m.cfg.Encolar != nil {
		if err := m.cfg.Encolar(ctx, t.Tx, pendientes.ArgsProcesar{}); err != nil {
			return f, err
		}
	}
	return f, nil
}

// prospectoDeClave: el id al final de iris:<op>:<id>.
func prospectoDeClave(clave string) int64 {
	for i := len(clave) - 1; i >= 0; i-- {
		if clave[i] == ':' {
			n, _ := strconv.ParseInt(clave[i+1:], 10, 64)
			return n
		}
	}
	return 0
}

// crearOrdenInstalacion crea la orden de instalación del prospecto como actor sistema (origen
// sistema), en la transacción de la acción. Idempotente por la clave comercial.instalacion:<id>.
func (m *Modulo) crearOrdenInstalacion(ctx context.Context, p comercialbd.ComercialProspecto, r requisitos, cliente, servicio *int64) (campo.Orden, error) {
	if m.cfg.Campo == nil {
		return campo.Orden{}, nucleo.NoDisponible("No hay módulo de campo configurado para crear la orden de instalación")
	}
	plan := ""
	if ls, err := comercialbd.New(nucleo.TxDe(ctx)).LineasDeCotizacion(ctx, r.cotizacion.ID); err == nil {
		for _, l := range ls {
			if l.Tipo == "plan" {
				plan = l.Descripcion
			}
		}
	}
	descripcion := fmt.Sprintf("Instalación del prospecto %s (%s). Plan: %s. Contrato %s, cotización %s.",
		p.Numero, p.Nombre, plan, r.contrato.Numero, r.cotizacion.Numero)
	if r.factibilidad.Resultado == FactibleRequiereObra {
		descripcion += " Requiere obra (confirmada al ganar)."
	}
	if r.factibilidad.RouterNombre != nil {
		descripcion += " Router sugerido: " + *r.factibilidad.RouterNombre + "."
	}
	if r.factibilidad.Caja != nil {
		descripcion += " Caja: " + *r.factibilidad.Caja + "."
	}
	direccion := p.Direccion
	entrada := campo.EntradaCrearOrden{
		Tipo: "instalacion", EmpresaID: p.EmpresaID, ProspectoID: &p.ID, ClienteID: cliente, ServicioID: servicio,
		ZonaID: p.ZonaID, Direccion: &direccion, Lat: p.Lat, Lon: p.Lon,
		Contacto:    &campo.EntradaContactoOrden{Nombre: &p.Nombre, Telefono: p.Telefono},
		Descripcion: descripcion, ClaveIdempotencia: "comercial.instalacion:" + idTexto(p.ID), Origen: "sistema",
	}
	return m.cfg.Campo.CrearOrden.Ejecutar(contexto.Sistema(ctx, contexto.CanalDe(ctx)), entrada)
}

// --- Alta en Argos: avance por la cola -------------------------------------------------------

type entradaAltaArgos struct {
	ProspectoID int64 `json:"prospecto_id"`
	ArgosID     int64 `json:"argos_id"`
}

func exigirSistema(ctx context.Context) error {
	if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
		return nucleo.Prohibido("El alta en Argos la avanza el sistema")
	}
	return nil
}

func (m *Modulo) nuevoRegistrarCliente() *auditoria.Caso[entradaAltaArgos, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[entradaAltaArgos, Prospecto]{
		Accion: "prospectos.alta_cliente", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAltaArgos) (Prospecto, error) {
			if err := exigirSistema(ctx); err != nil {
				return Prospecto{}, err
			}
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return Prospecto{}, err
			}
			if deref(antes.AltaEstado) != AltaEsperandoCliente {
				return aProspecto(antes), nil // ya avanzó (evento repetido)
			}
			r, err := requisitosGanado(ctx, q, antes)
			if err != nil {
				return Prospecto{}, err
			}
			a, err := pendientesbd.New(t).PendientePorClave(ctx, ClaveCliente(antes.ID))
			if err != nil {
				return Prospecto{}, err
			}
			b, err := m.encolar(ctx, t, pendientesbd.InsertarPendienteParams{
				EmpresaID: antes.EmpresaID, Operacion: OpCrearServicio, IdempotencyKey: ClaveServicio(antes.ID),
				Ruta: "/servicios", Payload: payloadServicio(antes, r, in.ArgosID), DependeDe: []int64{a.ID},
			}, m.ahora())
			if err != nil {
				return Prospecto{}, err
			}
			d := antes
			d.ClienteArgosID, d.AltaEstado = &in.ArgosID, ptr(AltaEsperandoServicio)
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			if err := historial(ctx, t, q, f.ID, "alta", ptr(AltaEsperandoCliente), f.AltaEstado, nil, map[string]any{
				"cliente_id": in.ArgosID, "pendiente_servicio_id": b.ID,
			}); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.alta_actualizada", map[string]any{
				"prospecto_id": f.ID, "estado": *f.AltaEstado, "cliente_id": in.ArgosID, "servicio_id": nil, "orden_id": nil,
			})
		},
	})
}

// requisitosGanado: contrato firmado, cotización y factibilidad de un prospecto ganado.
func requisitosGanado(ctx context.Context, q *comercialbd.Queries, p comercialbd.ComercialProspecto) (requisitos, error) {
	var r requisitos
	var err error
	if r.contrato, err = q.ContratoVivoDeProspecto(ctx, p.ID); err != nil {
		return r, err
	}
	if r.cotizacion, err = q.CotizacionPorID(ctx, r.contrato.CotizacionID); err != nil {
		return r, err
	}
	r.factibilidad, err = q.UltimaFactibilidad(ctx, p.ID)
	return r, err
}

func (m *Modulo) nuevoCompletarAlta() *auditoria.Caso[entradaAltaArgos, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[entradaAltaArgos, Prospecto]{
		Accion: "prospectos.alta_servicio", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAltaArgos) (Prospecto, error) {
			if err := exigirSistema(ctx); err != nil {
				return Prospecto{}, err
			}
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return Prospecto{}, err
			}
			if deref(antes.AltaEstado) != AltaEsperandoServicio {
				return aProspecto(antes), nil
			}
			r, err := requisitosGanado(ctx, q, antes)
			if err != nil {
				return Prospecto{}, err
			}
			o, err := m.crearOrdenInstalacion(ctx, antes, r, antes.ClienteArgosID, &in.ArgosID)
			if err != nil {
				return Prospecto{}, err
			}
			d := antes
			d.ServicioArgosID, d.AltaEstado, d.OrdenID = &in.ArgosID, ptr(AltaCompleta), &o.ID
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			if err := historial(ctx, t, q, f.ID, "alta", ptr(AltaEsperandoServicio), f.AltaEstado, nil, map[string]any{
				"cliente_id": f.ClienteArgosID, "servicio_id": in.ArgosID, "orden_id": o.ID, "orden_numero": o.Numero,
			}); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.alta_actualizada", map[string]any{
				"prospecto_id": f.ID, "estado": AltaCompleta, "cliente_id": f.ClienteArgosID, "servicio_id": in.ArgosID, "orden_id": o.ID,
			})
		},
	})
}

// CompletarAlta avanza el alta en Argos de un prospecto ganado (actor sistema, fuera de
// transacción): si el pendiente del paso actual quedó hecho, recupera el id de Argos y da el
// paso siguiente. Idempotente; sin nada que hacer, no hace nada. Un error (Argos caído) se
// devuelve: lo reintenta el relay o el barrido periódico.
func (m *Modulo) CompletarAlta(ctx context.Context, prospecto int64) error {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nucleo.ErrSinBD
	}
	for range 2 {
		p, err := comercialbd.New(bd).ProspectoPorID(ctx, prospecto)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if deref(p.AltaModo) != AltaArgos || p.AltaError != nil {
			return nil
		}
		var clave string
		var paso *auditoria.Caso[entradaAltaArgos, Prospecto]
		switch deref(p.AltaEstado) {
		case AltaEsperandoCliente:
			clave, paso = ClaveCliente(p.ID), m.registrarCliente
		case AltaEsperandoServicio:
			clave, paso = ClaveServicio(p.ID), m.completarAlta
		default:
			return nil
		}
		pend, err := pendientesbd.New(bd).PendientePorClave(ctx, clave)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if pend.Estado != pendientes.EstadoHecho {
			return nil
		}
		if m.cfg.Altas == nil {
			slog.WarnContext(ctx, "comercial: sin RecuperadorAltas; el alta en Argos no avanza", "prospecto", p.ID)
			return nil
		}
		// Argos guarda la clave al menos 30 días: más viejo, no se repite (revisión manual).
		if pend.HechoEn != nil && m.ahora().Sub(*pend.HechoEn) > RetencionIdempotencia {
			return m.errorDeAlta(ctx, p.ID, fmt.Sprintf("%s quedó hecho hace más de 30 días sin completar el alta: revisar a mano en Argos", clave))
		}
		id, err := m.cfg.Altas.Recuperar(ctx, pend)
		var er *conector.ErrorRemoto
		switch {
		case errors.Is(err, ErrAltaInconsistente):
			return m.errorDeAlta(ctx, p.ID, err.Error())
		case errors.As(err, &er) && rechazoDeNegocio(er):
			return m.errorDeAlta(ctx, p.ID, fmt.Sprintf("Argos rechazó %s %s al recuperar el alta (HTTP %d %s)", pend.Metodo, pend.Ruta, er.Status, er.Tipo))
		case err != nil:
			return err
		}
		if _, err := paso.Ejecutar(ctx, entradaAltaArgos{ProspectoID: p.ID, ArgosID: id}); err != nil {
			return err
		}
	}
	return nil
}

// RetencionIdempotencia: Argos guarda una Idempotency-Key al menos 30 días.
const RetencionIdempotencia = 30 * 24 * time.Hour

// rechazoDeNegocio: un 4xx que repetir no arregla (422 idempotency_key_reutilizada, 404…). No lo
// son 401/403 (credencial), 408, 409 (idempotencia_en_curso), 425 ni 429: esos se reintentan.
func rechazoDeNegocio(er *conector.ErrorRemoto) bool {
	if er.Bloqueado() || er.Status < 400 || er.Status >= 500 {
		return false
	}
	switch er.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusRequestTimeout, http.StatusConflict,
		http.StatusTooEarly, http.StatusTooManyRequests:
		return false
	}
	return true
}

type entradaErrorAlta struct {
	ProspectoID int64  `json:"prospecto_id"`
	Error       string `json:"error"`
}

// errorDeAlta deja el alta en error visible (alta.error) y lo registra; el barrido la salta.
func (m *Modulo) errorDeAlta(ctx context.Context, prospecto int64, detalle string) error {
	slog.ErrorContext(ctx, "comercial: el alta en Argos necesita revisión manual", "prospecto", prospecto, "detalle", detalle)
	_, err := m.marcarErrorAlta.Ejecutar(ctx, entradaErrorAlta{ProspectoID: prospecto, Error: detalle})
	return err
}

func (m *Modulo) nuevoMarcarErrorAlta() *auditoria.Caso[entradaErrorAlta, Prospecto] {
	return auditoria.Nuevo(auditoria.Caso[entradaErrorAlta, Prospecto]{
		Accion: "prospectos.alta_error", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaErrorAlta) (Prospecto, error) {
			if err := exigirSistema(ctx); err != nil {
				return Prospecto{}, err
			}
			q := comercialbd.New(t)
			antes, err := tomar(ctx, t, q, in.ProspectoID, nucleo.PermProspectosGanar, nil)
			if err != nil {
				return Prospecto{}, err
			}
			if antes.AltaError != nil || deref(antes.AltaEstado) == AltaCompleta {
				return aProspecto(antes), nil
			}
			detalle := in.Error
			if r := []rune(detalle); len(r) > 1000 {
				detalle = string(r[:1000])
			}
			d := antes
			d.AltaError = &detalle
			f, err := guardar(ctx, t, q, antes, d)
			if err != nil {
				return Prospecto{}, err
			}
			if err := historial(ctx, t, q, f.ID, "alta_error", f.AltaEstado, f.AltaEstado, &detalle, nil); err != nil {
				return Prospecto{}, err
			}
			return aProspecto(f), t.Emitir("prospecto.alta_actualizada", map[string]any{
				"prospecto_id": f.ID, "estado": deref(f.AltaEstado), "cliente_id": f.ClienteArgosID, "servicio_id": f.ServicioArgosID,
				"orden_id": f.OrdenID, "error": detalle,
			})
		},
	})
}

// eventoPendienteHecho: lo que usa comercial de integracion.pendiente_hecho.
type eventoPendienteHecho struct {
	Entidad   string `json:"entidad"`
	EntidadID string `json:"entidad_id"`
}

// SuscribirAltas: al quedar hecho un pendiente de un prospecto, avanza su alta (cmd/worker).
// Una regla de negocio (4xx) queda en el log; un error temporal lo reintenta el relay.
func (m *Modulo) SuscribirAltas(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("comercial.alta_argos", pendientes.EventoHecho, func(ctx context.Context, e integracion.Evento) error {
		var p eventoPendienteHecho
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Entidad != EntidadPendientes {
			return nil
		}
		id, err := strconv.ParseInt(p.EntidadID, 10, 64)
		if err != nil || id <= 0 {
			return nil
		}
		err = m.CompletarAlta(nucleo.ConBD(ctx, bd), id)
		var pr *nucleo.Problema
		if errors.As(err, &pr) && pr.Status < 500 {
			log.WarnContext(ctx, "comercial: el alta del prospecto no avanzó", "prospecto", id, "codigo", pr.Codigo(), "detalle", pr.Detail)
			return nil
		}
		return err
	})
}

// ArgsCompletarAltas barre los prospectos ganados con el alta en Argos sin terminar (por si se
// perdió un evento): todo lo pendiente hasta ahora.
type ArgsCompletarAltas struct{}

// Kind implementa river.JobArgs.
func (ArgsCompletarAltas) Kind() string { return "comercial.completar_altas" }

// InsertOpts: único por periodo.
func (ArgsCompletarAltas) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: 4 * time.Minute}}
}

// TrabajadorAltas corre el barrido como actor sistema.
type TrabajadorAltas struct {
	river.WorkerDefaults[ArgsCompletarAltas]
	BD     nucleo.BD
	Modulo *Modulo
}

// Work implementa river.Worker.
func (w *TrabajadorAltas) Work(ctx context.Context, _ *river.Job[ArgsCompletarAltas]) error {
	ctx = nucleo.ConBD(contexto.Sistema(ctx, contexto.CanalCron), w.BD)
	_, err := w.Modulo.BarrerAltas(ctx)
	return err
}

// BarrerAltas intenta avanzar cada alta pendiente; los errores quedan en el log. Devuelve
// cuántas revisó.
func (m *Modulo) BarrerAltas(ctx context.Context) (int, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return 0, nucleo.ErrSinBD
	}
	var despues int64
	n := 0
	for {
		ids, err := comercialbd.New(bd).ProspectosConAltaPendiente(ctx, comercialbd.ProspectosConAltaPendienteParams{DespuesDe: despues, Limite: 200})
		if err != nil || len(ids) == 0 {
			return n, err
		}
		for _, id := range ids {
			n++
			if err := m.CompletarAlta(ctx, id); err != nil {
				slog.WarnContext(ctx, "comercial: el alta del prospecto no avanzó", "prospecto", id, "err", err)
			}
		}
		despues = ids[len(ids)-1]
	}
}

// TrabajoPeriodicoAltas agenda el barrido cada intervalo (5 min por defecto).
func TrabajoPeriodicoAltas(intervalo time.Duration) *river.PeriodicJob {
	if intervalo <= 0 {
		intervalo = 5 * time.Minute
	}
	return river.NewPeriodicJob(river.PeriodicInterval(intervalo),
		func() (river.JobArgs, *river.InsertOpts) { return ArgsCompletarAltas{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}

// RegistrarTrabajos agrega los workers de comercial (integracion.OpcionesWorker.Registrar).
func (m *Modulo) RegistrarTrabajos(bd nucleo.BD) func(*river.Workers) {
	return func(w *river.Workers) {
		river.AddWorker(w, &TrabajadorAltas{BD: bd, Modulo: m})
	}
}
