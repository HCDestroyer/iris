package campo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Bienvenida en la orden (contrato argosisp.md 0.9.0 §12.5, pedido B16). Argos decide cuándo sale y
// por qué canales; la orden la muestra (webhook servicio.bienvenida_cambiada), ofrece «Reenviar»
// (POST /servicios/:id/bienvenida/reenviar por la cola, iris:bienvenida:<orden_id>:<n>) y, con el
// ajuste cierre_exige_bienvenida y el modo «exigir lista», el cierre la exige (activacion.go). La
// bandeja «Bienvenidas sin enviar» lista las instalaciones activas sin bienvenida.

// Eventos de la bienvenida (catálogo de CLAUDE.md; nunca el texto ni las cuentas).
const (
	EventoBienvenidaCambiada  = "orden.bienvenida_cambiada"
	EventoBienvenidaReenviada = "orden.bienvenida_reenviada"
)

// CanalBienvenida es el estado de un canal.
type CanalBienvenida struct {
	// Relacionada: llegó sin origen y se relacionó por servicio (0.14.3): no cuenta para el cierre.
	Relacionada bool    `json:"relacionadaPorServicio,omitempty"`
	Canal       string  `json:"canal" enum:"whatsapp,correo,telegram"`
	Estado      string  `json:"estado" enum:"pendiente,enviada,fallida,omitida"`
	Motivo      *string `json:"motivo" nullable:"true" doc:"Por qué falló u omitió (sin_permiso, baja, sin_contacto…)"`
	AvisoID     *string `json:"avisoId,omitempty" doc:"WhatsApp: el aviso de Iris"`
}

// Bienvenida es el bloque «Bienvenida» de la orden.
type Bienvenida struct {
	OrdenID       int64             `json:"ordenId"`
	Canales       []CanalBienvenida `json:"canales" doc:"Lo último que informó Argos por canal (vacío: aún no hay noticias)"`
	Cumplida      bool              `json:"cumplida" doc:"Salió por algún canal, o no salió porque el cliente no acepta avisos"`
	ActualizadaEn *time.Time        `json:"actualizadaEn" nullable:"true"`
	Reenvios      int32             `json:"reenvios"`
	// Reenvio: el último reenvío pedido desde Iris y cómo va en la cola hacia Argos.
	Reenvio *ReenvioBienvenida `json:"reenvio,omitempty"`
}

// ReenvioBienvenida: el último reenvío pedido.
type ReenvioBienvenida struct {
	Estado string  `json:"estado" doc:"El del pendiente hacia Argos: pendiente, en_curso, hecho, fallido_negocio…"`
	Codigo *string `json:"codigo" nullable:"true" doc:"Con fallido_negocio: servicio_no_activo, bienvenida_reciente, demasiados_reenvios…"`
}

// EntradaReenviarBienvenida es la entrada de ordenes.reenviar_bienvenida.
type EntradaReenviarBienvenida struct {
	ID      int64    `json:"-"`
	Canales []string `json:"canales,omitempty" maxItems:"3" uniqueItems:"true" enum:"whatsapp,correo,telegram" doc:"Sin ellos, todos los que Argos tenga configurados"`
}

// soloRelacionada: la orden tiene canales de bienvenida y todos llegaron sin origen (relacionados
// por servicio).
func soloRelacionada(canales []CanalBienvenida) bool {
	if len(canales) == 0 {
		return false
	}
	for _, c := range canales {
		if !c.Relacionada {
			return false
		}
	}
	return true
}

func leerCanales(crudo []byte) []CanalBienvenida {
	c := []CanalBienvenida{}
	if len(crudo) > 0 {
		_ = json.Unmarshal(crudo, &c)
	}
	return c
}

func (m *Modulo) bienvenidaDe(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, orden int64) (Bienvenida, error) {
	b := Bienvenida{OrdenID: orden, Canales: []CanalBienvenida{}}
	oa, err := q.OrdenActivacion(ctx, orden)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, nil
	}
	if err != nil {
		return b, err
	}
	b.Canales, b.Cumplida, b.ActualizadaEn, b.Reenvios = leerCanales(oa.BienvenidaCanales), bienvenidaCumplida(oa.BienvenidaCanales), oa.BienvenidaEn, oa.BienvenidaReenvios
	if oa.BienvenidaReenvios > 0 {
		reg, err := pendientes.PorClave(ctx, t, pendientes.ClaveBienvenida(orden, oa.BienvenidaReenvios))
		if err == nil {
			b.Reenvio = &ReenvioBienvenida{Estado: reg.Estado, Codigo: reg.CodigoError}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return b, err
		}
	}
	return b, nil
}

func (m *Modulo) nuevoVerBienvenida() *auditoria.Caso[EntradaVerOrden, Bienvenida] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerOrden, Bienvenida]{
		Accion: "ordenes.ver_bienvenida", Acceso: auditoria.Autenticado, AuditarExito: soloLecturas[Bienvenida],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerOrden) (Bienvenida, error) {
			q := campobd.New(t)
			f, err := q.OrdenPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return Bienvenida{}, noEncontrada()
			}
			if err != nil {
				return Bienvenida{}, err
			}
			if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, f); err != nil {
				return Bienvenida{}, err
			}
			return m.bienvenidaDe(ctx, t, q, f.ID)
		},
	})
}

func (m *Modulo) nuevoReenviarBienvenida() *auditoria.Caso[EntradaReenviarBienvenida, Bienvenida] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReenviarBienvenida, Bienvenida]{
		Accion: "ordenes.reenviar_bienvenida", Permiso: nucleo.PermOrdenesReenviarBienvenida,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReenviarBienvenida) (Bienvenida, error) {
			if err := sinNulos(in); err != nil {
				return Bienvenida{}, err
			}
			q := campobd.New(t)
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesReenviarBienvenida, nil, nil)
			if err != nil {
				return Bienvenida{}, err
			}
			a := contexto.ActorDe(ctx)
			if !a.EsUsuario() {
				return Bienvenida{}, nucleo.Prohibido("El reenvío lo pide una persona (X-Iris-Actor)")
			}
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return Bienvenida{}, err
			}
			if tipo.Codigo != tipoInstalacion || f.ServicioID == nil {
				return Bienvenida{}, nucleo.ReglaNegocio("orden_no_es_instalacion", "La bienvenida se reenvía desde una instalación con su servicio de Argos")
			}
			if m.cfg.Equipos.Capacidad == nil {
				return Bienvenida{}, nucleo.ReglaNegocio("bienvenida_no_disponible", "Iris no tiene conexión con Argos")
			}
			if ok, err := m.cfg.Equipos.Capacidad(ctx, t, f.EmpresaID, argos.Funcion(argos.FuncionBienvenida)); err != nil {
				return Bienvenida{}, err
			} else if !ok {
				return Bienvenida{}, nucleo.ReglaNegocio(argos.CodigoCapacidadNoDisponible, "La bienvenida no está disponible para esta empresa")
			}
			activa := f.Estado == EstadoCerrada && f.Resultado != nil && *f.Resultado == "exitosa"
			if viva, err := q.ActivacionVivaDeOrden(ctx, f.ID); err == nil && viva.Estado == ActivacionLista {
				activa = true
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Bienvenida{}, err
			}
			if !activa {
				return Bienvenida{}, nucleo.Conflicto("servicio_no_activo", "El servicio aún no está activo: la bienvenida sale al activarlo")
			}
			oa, err := q.AsegurarOrdenActivacion(ctx, campobd.AsegurarOrdenActivacionParams{OrdenID: f.ID, EmpresaID: f.EmpresaID})
			if err != nil {
				return Bienvenida{}, err
			}
			n, err := q.SumarReenvioBienvenida(ctx, f.ID)
			if err != nil {
				return Bienvenida{}, err
			}
			cuerpo := map[string]any{}
			if !soloRelacionada(leerCanales(oa.BienvenidaCanales)) {
				cuerpo["origen_tipo"], cuerpo["origen_id"] = pendientes.OrigenTipo, strconv.FormatInt(f.ID, 10)
			} else {
				// La bienvenida llegó sin origen y solo se relacionó por servicio (0.14.3): esta orden no es su
				// origen real; el reenvío no lo afirma.
				t.Anotar(map[string]any{"sin_origen": true})
			}
			if len(in.Canales) > 0 {
				cuerpo["canales"] = in.Canales
			}
			crudo, err := json.Marshal(cuerpo)
			if err != nil {
				return Bienvenida{}, err
			}
			reg, _, err := pendientes.Encolar(ctx, t, pendientes.Accion{
				EmpresaID: f.EmpresaID, Operacion: pendientes.OpReenviarBienvenida, IdempotencyKey: pendientes.ClaveBienvenida(f.ID, n),
				Entidad: "orden", EntidadID: strconv.FormatInt(f.ID, 10), Ruta: "/servicios/" + strconv.FormatInt(*f.ServicioID, 10) + "/bienvenida/reenviar",
				Cuerpo: crudo, Desde: m.ahora(), Actor: pendientes.ActorUsuario(a.ID),
			}, m.cfg.Titular.Encolar)
			if err != nil {
				return Bienvenida{}, err
			}
			t.RegistrarCambios("orden_activacion", f.ID, nil, map[string]any{"bienvenida_reenvios": n, "pendiente_id": reg.ID, "canales": in.Canales})
			if err := t.Emitir(EventoBienvenidaReenviada, map[string]any{"orden_id": f.ID, "servicio_id": *f.ServicioID, "n": n,
				"canales": in.Canales, "pendiente_id": reg.ID}); err != nil {
				return Bienvenida{}, err
			}
			return m.bienvenidaDe(ctx, t, q, f.ID)
		},
	})
}

// entradaBienvenidaArgos: servicio.bienvenida_cambiada (argos.bienvenida_cambiada).
type entradaBienvenidaArgos struct {
	ClienteID  *int64            `json:"cliente_id"`
	ServicioID int64             `json:"servicio_id"`
	EmpresaID  *int64            `json:"empresa_id"`
	OrigenTipo *string           `json:"origen_tipo"`
	OrigenID   *string           `json:"origen_id"`
	Canales    []CanalBienvenida `json:"canales"`
	EntranteID *int64            `json:"entrante_id"`
}

func (m *Modulo) nuevoActualizarBienvenida() *auditoria.Caso[entradaBienvenidaArgos, *Bienvenida] {
	return auditoria.Nuevo(auditoria.Caso[entradaBienvenidaArgos, *Bienvenida]{
		Accion: "ordenes.actualizar_bienvenida", Acceso: auditoria.Autenticado,
		AuditarExito: func(b *Bienvenida) bool { return b != nil },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaBienvenidaArgos) (*Bienvenida, error) {
			q := campobd.New(t)
			// La orden del origen (de ese servicio) o, si la activación fue desde Argos, la última
			// instalación del servicio.
			var orden int64
			if in.OrigenTipo != nil && *in.OrigenTipo == pendientes.OrigenTipo && in.OrigenID != nil {
				if n, err := strconv.ParseInt(*in.OrigenID, 10, 64); err == nil {
					orden = n
				}
			}
			relacionada := false
			if orden == 0 {
				// Sin origen (0.14.3): una sola instalación del servicio y del titular actual en los 7 días
				// anteriores; se muestra «relacionada por servicio» y no cuenta para el cierre.
				if in.ClienteID == nil {
					return nil, nil
				}
				// La hora en que llegó el aviso (el webhook en integracion.entrantes), no la de ahora.
				// Sin el entrante (un evento interno sin él), no se filtra por la creación de la orden.
				recibido, creadaAntes := m.ahora(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
				if in.EntranteID != nil {
					if r, err := q.RecibidoDeEntrante(ctx, *in.EntranteID); err == nil {
						recibido, creadaAntes = r, r
					} else if !errors.Is(err, pgx.ErrNoRows) {
						return nil, err
					}
				}
				ids, err := q.OrdenesParaBienvenidaSinOrigen(ctx, campobd.OrdenesParaBienvenidaSinOrigenParams{ServicioID: &in.ServicioID,
					ClienteID: in.ClienteID, Recibido: recibido, Desde: recibido.Add(-7 * 24 * time.Hour), CreadaAntes: creadaAntes})
				if err != nil {
					return nil, err
				}
				if len(ids) != 1 {
					t.Anotar(map[string]any{"bienvenida_sin_relacionar": len(ids)})
					return nil, nil
				}
				orden, relacionada = ids[0], true
			}
			f, err := q.OrdenPorIDParaActualizar(ctx, orden)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if f.ServicioID == nil || *f.ServicioID != in.ServicioID || (in.EmpresaID != nil && !mismoID(in.EmpresaID, f.EmpresaID)) {
				t.Anotar(map[string]any{"orden_de_otro_servicio": orden})
				return nil, nil
			}
			t.FijarEntidad("orden", f.ID)
			t.FijarEmpresa(deref(f.EmpresaID))
			antes, err := q.AsegurarOrdenActivacion(ctx, campobd.AsegurarOrdenActivacionParams{OrdenID: f.ID, EmpresaID: f.EmpresaID})
			if err != nil {
				return nil, err
			}
			// Se mezcla por canal: lo que no vino queda como estaba.
			canales := leerCanales(antes.BienvenidaCanales)
			for _, c := range in.Canales {
				c.Relacionada = relacionada
				hecho := false
				for i := range canales {
					if canales[i].Canal == c.Canal {
						canales[i], hecho = c, true
					}
				}
				if !hecho {
					canales = append(canales, c)
				}
			}
			crudo, err := json.Marshal(canales)
			if err != nil {
				return nil, err
			}
			if mismoJSON(crudo, antes.BienvenidaCanales) {
				return nil, nil
			}
			ahora := m.ahora()
			d, err := q.FijarBienvenidaOrden(ctx, campobd.FijarBienvenidaOrdenParams{OrdenID: f.ID, Canales: crudo, En: &ahora})
			if err != nil {
				return nil, err
			}
			t.RegistrarCambios("orden_activacion", f.ID, map[string]any{"bienvenida_canales": json.RawMessage(antes.BienvenidaCanales)},
				map[string]any{"bienvenida_canales": json.RawMessage(d.BienvenidaCanales)})
			if _, err := guardar(ctx, t, q, f, f); err != nil {
				return nil, err
			}
			resumen := make([]map[string]any, 0, len(canales))
			for _, c := range canales {
				resumen = append(resumen, map[string]any{"canal": c.Canal, "estado": c.Estado, "motivo": c.Motivo})
			}
			if err := t.Emitir(EventoBienvenidaCambiada, map[string]any{"orden_id": f.ID, "servicio_id": in.ServicioID,
				"canales": resumen, "cumplida": bienvenidaCumplida(d.BienvenidaCanales), "entrante_id": in.EntranteID}); err != nil {
				return nil, err
			}
			b, err := m.bienvenidaDe(ctx, t, q, f.ID)
			return &b, err
		},
	})
}

func mismoJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return len(a) == 0 && len(b) == 0
	}
	ja, _ := json.Marshal(x)
	jb, _ := json.Marshal(y)
	return string(ja) == string(jb)
}

// SuscribirBienvenidas: servicio.bienvenida_cambiada de Argos actualiza el bloque de la orden.
func (m *Modulo) SuscribirBienvenidas(bus *integracion.Bus, bd nucleo.BD) {
	bus.Suscribir("campo.bienvenida", argos.EventoBienvenidaCambiadaInterno, func(ctx context.Context, e integracion.Evento) error {
		var in entradaBienvenidaArgos
		if json.Unmarshal(e.Payload, &in) != nil || in.ServicioID <= 0 {
			return nil
		}
		_, err := m.ActualizarBienvenida.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}

// --- Bandeja «Bienvenidas sin enviar» ----------------------------------------------------------

// EntradaBienvenidasSinEnviar: la página de la bandeja.
type EntradaBienvenidasSinEnviar struct {
	Cursor string `query:"cursor" doc:"nextCursor de la página anterior"`
	Limit  int32  `query:"limit" minimum:"1" maximum:"100" default:"50"`
}

// BienvenidaSinEnviar es una fila de la bandeja.
type BienvenidaSinEnviar struct {
	OrdenID    int64             `json:"ordenId"`
	Numero     string            `json:"numero"`
	ClienteID  *int64            `json:"clienteId" nullable:"true"`
	ServicioID *int64            `json:"servicioId" nullable:"true"`
	Estado     string            `json:"estado"`
	Canales    []CanalBienvenida `json:"canales"`
}

func (m *Modulo) nuevoBienvenidasSinEnviar() *auditoria.Caso[EntradaBienvenidasSinEnviar, nucleo.Pagina[BienvenidaSinEnviar]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaBienvenidasSinEnviar, nucleo.Pagina[BienvenidaSinEnviar]]{
		Accion: "ordenes.bienvenidas_sin_enviar", Permiso: nucleo.PermOrdenesReenviarBienvenida, AuditarExito: soloLecturas[nucleo.Pagina[BienvenidaSinEnviar]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaBienvenidasSinEnviar) (nucleo.Pagina[BienvenidaSinEnviar], error) {
			r := nucleo.Pagina[BienvenidaSinEnviar]{Data: []BienvenidaSinEnviar{}}
			limite := in.Limit
			if limite <= 0 || limite > 100 {
				limite = 50
			}
			var antes *int64
			if in.Cursor != "" {
				n, err := strconv.ParseInt(in.Cursor, 10, 64)
				if err != nil || n <= 0 {
					return r, campoInvalido("cursor", "Cursor inválido", "cursor")
				}
				antes = &n
			}
			todas, empresa := filtroEmpresa(ctx)
			desde := m.ahora().Add(-30 * 24 * time.Hour)
			filas, err := campobd.New(t).BienvenidasSinEnviar(ctx, campobd.BienvenidasSinEnviarParams{Todas: todas, EmpresaID: empresa,
				Desde: &desde, Antes: antes, Limite: limite + 1})
			if err != nil {
				return r, err
			}
			for i, f := range filas {
				if i == int(limite) {
					c := strconv.FormatInt(filas[i-1].ID, 10)
					r.NextCursor = &c
					break
				}
				// Solo las de la zona o cuadrilla del actor (alcance de ordenes.reenviar_bienvenida).
				if !cubre(ctx, nucleo.PermOrdenesReenviarBienvenida, campobd.CampoOrden{ID: f.ID, EmpresaID: f.EmpresaID, ZonaID: f.ZonaID,
					CuadrillaID: f.CuadrillaID, TecnicoID: f.TecnicoID}) {
					continue
				}
				r.Data = append(r.Data, BienvenidaSinEnviar{OrdenID: f.ID, Numero: f.Numero, ClienteID: f.ClienteID, ServicioID: f.ServicioID,
					Estado: f.Estado, Canales: leerCanales(f.BienvenidaCanales)})
			}
			return r, nil
		},
	})
}

func (m *Modulo) registrarBienvenida(api huma.API) {
	ver := operacion("verBienvenidaOrden", http.MethodGet, "/api/v1/ordenes/{id}/bienvenida", "Bienvenida del servicio de la orden",
		nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	ver.Description += " El estado por canal (WhatsApp, correo, Telegram) que informa Argos y el último reenvío. Nunca el texto ni las cuentas."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerOrden) (*cuerpo[Bienvenida], error) {
		return responder(m.VerBienvenida.Ejecutar(ctx, *in))
	})
	re := operacion("reenviarBienvenidaOrden", http.MethodPost, "/api/v1/ordenes/{id}/bienvenida/reenviar", "Reenviar la bienvenida",
		nucleo.PermOrdenesReenviarBienvenida, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	re.Description += " Argos la vuelve a disparar por sus canales con las mismas reglas (permisos de contacto, límites y horario de silencio). " +
		"Sale por la cola de pendientes (iris:bienvenida:<orden>:<n>); su rechazo (servicio_no_activo, bienvenida_reciente, demasiados_reenvios) " +
		"queda en `reenvio`. 409 `servicio_no_activo` si la orden aún no tiene el servicio activo. Emite `orden.bienvenida_reenviada`."
	huma.Register(api, re, func(ctx context.Context, in *conID[EntradaReenviarBienvenida]) (*cuerpo[Bienvenida], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.ReenviarBienvenida.Ejecutar(ctx, e))
	})
	bandeja := operacion("bienvenidasSinEnviar", http.MethodGet, "/api/v1/ordenes/bienvenidas-sin-enviar", "Bienvenidas sin enviar",
		nucleo.PermOrdenesReenviarBienvenida, http.StatusOK)
	bandeja.Description += " Instalaciones activas de los últimos 30 días (empresa activa) cuya bienvenida no salió por ningún canal ni quedó sin enviar " +
		"porque el cliente no acepta avisos. Paginada por cursor."
	huma.Register(api, bandeja, func(ctx context.Context, in *EntradaBienvenidasSinEnviar) (*cuerpo[nucleo.Pagina[BienvenidaSinEnviar]], error) {
		return responder(m.BienvenidasSinEnviar.Ejecutar(ctx, *in))
	})
}
