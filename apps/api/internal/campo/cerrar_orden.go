package campo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// EntradaCerrarOrden es la entrada de ordenes.cerrar.
type EntradaCerrarOrden struct {
	ID        int64    `json:"-"`
	Version   int32    `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Resultado string   `json:"resultado,omitempty" enum:"exitosa,parcial" doc:"exitosa por defecto. Solo una instalación exitosa activa el servicio en Argos"`
	Lat       *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon       *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	// MotivoSinContratoFirmado: un supervisor cierra una instalación con el contrato regenerado sin firmar.
	MotivoSinContratoFirmado *string `json:"motivoSinContratoFirmado,omitempty" minLength:"10" maxLength:"500" doc:"Solo desde el panel y con ordenes.validar_identidad (supervisor) sobre la orden: cierra la instalación aunque el titular no haya firmado el contrato regenerado (B20). Queda en la auditoría y avisa a supervisión (orden.contrato_sin_firma)"`
	Sync                     *OpSync `json:"-"`
}

func (e EntradaCerrarOrden) ordenID() int64 { return e.ID }

// exigirContratoFirmado (B20, verificación de #280): una instalación con el contrato regenerado
// en la orden sin firmar no cierra (422 contrato_por_firmar). Tampoco mientras el cambio de plan o
// de titular que lo va a regenerar sigue sin revisión: en la cola de Argos, o hecho sin que el relay
// haya creado la revisión en comercial (CambioSinRevisionDeOrden). Un supervisor la cierra igual
// desde el panel con motivo (motivoSinContratoFirmado): ordenes.validar_identidad con alcance de su
// rol sobre la orden (global, zona o cuadrilla; nunca por ser el técnico asignado), canal panel.
// Argos ya tiene (o va a tener) el plan o el titular nuevo: sin la firma, la subida de precio no
// queda aceptada (B6).
func exigirContratoFirmado(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, tipo string, in EntradaCerrarOrden) error {
	if tipo != tipoInstalacion {
		if in.MotivoSinContratoFirmado != nil {
			return campoInvalido("motivoSinContratoFirmado", "Solo para una instalación con el contrato regenerado sin firmar", "instalacion")
		}
		return nil
	}
	detalle := ""
	var enCola *campobd.CambioSinRevisionDeOrdenRow
	if _, err := q.ContratoPorFirmarDeOrden(ctx, f.ID); err == nil {
		detalle = "El titular debe firmar en sitio el contrato regenerado (cambio de plan o de titular) antes de cerrar"
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	} else if c, err := q.CambioSinRevisionDeOrden(ctx, f.ID); err == nil {
		cambio := "de plan"
		if c.Operacion == "cambiar_titular" {
			cambio = "de titular"
		}
		detalle = "El cambio " + cambio + " todavía no tiene el contrato nuevo firmado (sigue en curso con Argos o espera el contrato); espera la firma del titular antes de cerrar"
		if slices.Contains([]string{"pendiente", "en_curso", "bloqueado", "agotado"}, c.Estado) {
			enCola = &c
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if detalle == "" {
		if in.MotivoSinContratoFirmado != nil {
			return campoInvalido("motivoSinContratoFirmado", "Esta orden no tiene un contrato por firmar", "sin_contrato")
		}
		return nil
	}
	if in.MotivoSinContratoFirmado == nil {
		return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "contrato_por_firmar", "Falta la firma del contrato nuevo",
			detalle+"; un supervisor puede cerrar sin la firma desde el panel, con motivo",
			nucleo.ErrorCampo{Field: "contrato", Message: "Falta la firma del contrato nuevo", Rule: "firma"})
	}
	if in.Sync != nil || contexto.CanalDe(ctx) != contexto.CanalPanel || !supervisaOrden(ctx, f) {
		return nucleo.Prohibido("Solo un supervisor cierra sin la firma del contrato nuevo, desde el panel")
	}
	if _, err := texto("motivoSinContratoFirmado", *in.MotivoSinContratoFirmado, 10, 500); err != nil {
		return err
	}
	// Lo único que bloqueaba era un cambio todavía en la cola de Argos: no hay contrato ni revisión
	// que anular después, así que el aviso a supervisión sale aquí, en la misma transacción.
	if enCola != nil {
		return t.Emitir(eventoContratoSinFirma, map[string]any{"orden_id": f.ID, "prospecto_id": enCola.ProspectoID, "empresa_id": f.EmpresaID,
			"contrato_id": nil, "cotizacion_id": nil, "operacion": enCola.Operacion, "motivo": "cierre_sin_firma", "clave": enCola.IdempotencyKey})
	}
	return nil
}

// eventoContratoSinFirma: el aviso a supervisión de comercial (comercial.EventoContratoSinFirma;
// campo no importa comercial). Aquí, un supervisor cerró con el cambio todavía en la cola.
const eventoContratoSinFirma = "orden.contrato_sin_firma"

// supervisaOrden: ordenes.validar_identidad con el alcance de su rol sobre la orden (global, su
// zona o su cuadrilla). Sin «propia»: el técnico asignado nunca se autoriza a sí mismo.
func supervisaOrden(ctx context.Context, f campobd.CampoOrden) bool {
	a := contexto.ActorDe(ctx)
	if !a.EsUsuario() || (f.TecnicoID != nil && *f.TecnicoID == a.ID) {
		return false
	}
	p := contexto.PermisosDe(ctx)
	if !p.Tiene(string(nucleo.PermOrdenesValidarIdentidad)) {
		return false
	}
	global, zonas, cuadrillas := p.Alcances(string(nucleo.PermOrdenesValidarIdentidad))
	return global || (f.ZonaID != nil && slices.Contains(zonas, *f.ZonaID)) || (f.CuadrillaID != nil && slices.Contains(cuadrillas, *f.CuadrillaID))
}

// cierreIncompleto: faltan requisitos de la plantilla o del tipo (422 con la lista).
func cierreIncompleto(errores []nucleo.ErrorCampo) *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "cierre_incompleto", "Cierre incompleto",
		"Faltan requisitos para cerrar la orden", errores...)
}

// faltanAdjuntos: hay adjuntos obligatorios sin subir (contratos/sync.md §3); el teléfono
// reintenta cuando terminan.
func faltanAdjuntos(faltan []string) *nucleo.Problema {
	errores := make([]nucleo.ErrorCampo, 0, len(faltan))
	for _, f := range faltan {
		errores = append(errores, nucleo.ErrorCampo{Field: "adjuntos." + f, Message: "Adjunto obligatorio sin subir", Rule: "subido"})
	}
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "faltan_adjuntos", "Faltan adjuntos",
		"Hay adjuntos obligatorios que no han terminado de subir", errores...)
}

func (m *Modulo) nuevoCerrarOrden() *auditoria.Caso[EntradaCerrarOrden, Orden] {
	return accionOrden("ordenes.cerrar", nucleo.PermOrdenesCerrar, func(e EntradaCerrarOrden) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCerrarOrden) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesCerrar, versionDe(in.Version, in.Sync), in.Sync)
			if err != nil {
				return antes, "", err
			}
			// Orden de bloqueo de campo: orden → ticket → candado del técnico (guardar).
			if err := bloquearTicket(ctx, t, antes); err != nil {
				return antes, "", err
			}
			cuando := m.cuandoEn(in.Sync, antes)
			if err := noPausada(antes, "cerrarla"); err != nil {
				return antes, "", err
			}
			if err := exigirEstado(antes, "cerrar", EstadoEnSitio); err != nil {
				return antes, "", err
			}
			resultado := in.Resultado
			if resultado == "" {
				resultado = "exitosa"
			}
			if resultado != "exitosa" && resultado != "parcial" {
				return antes, "", campoInvalido("resultado", "Resultado desconocido", "enum")
			}
			if err := validarCoordenadas(in.Lat, in.Lon); err != nil {
				return antes, "", err
			}
			tipo, err := tipoDe(ctx, q, antes)
			if err != nil {
				return antes, "", err
			}
			det, err := detalles(ctx, q, []campobd.CampoOrden{antes})
			if err != nil {
				return antes, "", err
			}
			d := det[0]
			if errores := requisitosDeCierre(d, tipo); len(errores) > 0 {
				return antes, "", cierreIncompleto(errores)
			}
			// Instalación de una venta: la identidad del titular registrada o validada (§3.4.8).
			if err := m.exigirIdentidad(ctx, t, q, antes, tipo.Codigo, resultado); err != nil {
				return antes, "", err
			}
			// Y el contrato regenerado en la orden, firmado (o un supervisor cierra sin la firma).
			if err := exigirContratoFirmado(ctx, t, q, antes, tipo.Codigo, in); err != nil {
				return antes, "", err
			}
			if in.MotivoSinContratoFirmado != nil {
				t.Anotar(map[string]any{"cierre_sin_contrato_firmado": true})
			}
			faltan, err := m.cfg.Adjuntos.Faltantes(ctx, antes.ID, requisitosAdjuntos(d))
			if err != nil {
				return antes, "", err
			}
			// La foto de cada cambio o intento también (M-271-3): Argos la abre por su id.
			for _, e := range d.Equipos {
				if (e.Accion == AccionCambio || e.Accion == AccionNoRecuperado) && e.FotoAdjuntoID != nil {
					a, err := adjuntos.DeEntidad(ctx, t, *e.FotoAdjuntoID, "orden", strconv.FormatInt(antes.ID, 10))
					if err != nil || a.Estado != "completo" {
						faltan = append(faltan, "equipos."+e.ID.String()+".foto")
					}
				}
			}
			if len(faltan) > 0 {
				return antes, "", faltanAdjuntos(faltan)
			}

			despues := antes
			despues.Estado, despues.FinalizadaEn, despues.Resultado = EstadoCerrada, &cuando, &resultado
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &resultado, gps(in.Lat, in.Lon, nil, cuando), cuando); err != nil {
				return f, "", err
			}
			payload := payloadCerrada(f, tipo.Codigo, d)
			var plantilla *campobd.CampoPlantillasChecklist
			if f.PlantillaID != nil {
				p, err := q.PlantillaPorID(ctx, *f.PlantillaID)
				if err != nil {
					return f, "", err
				}
				plantilla = &p
			}
			payload["cargo"] = cargoDe(f, tipo, plantilla)
			// El técnico en Argos (contrato 0.5.0 §3.3), congelado al cerrar: el vínculo de su ficha de
			// agenda. null = sin vincular (la cola usa bodega_id, también con Argos < 1.6.0).
			if payload["tecnico"], err = tecnicoArgos(ctx, q, f.TecnicoID); err != nil {
				return f, "", err
			}
			if err := t.Emitir("orden.cerrada", payload); err != nil {
				return f, "", err
			}
			// Encuesta de satisfacción: instalación o visita cerrada con éxito (una por orden).
			if m.cfg.Encuestas != nil && resultado == "exitosa" && (tipo.Codigo == "instalacion" || tipo.Codigo == "visita") {
				if err := m.cfg.Encuestas(ctx, t, soporte.EncuestaDeOrden{
					OrdenID: f.ID, Numero: f.Numero, EmpresaID: f.EmpresaID, TicketID: f.TicketID,
					ZonaID: f.ZonaID, CuadrillaID: f.CuadrillaID, TecnicoID: f.TecnicoID,
				}); err != nil {
					return f, "", err
				}
			}
			return f, "", proponerEnTicket(ctx, t, q, f, resultado, m.ahora())
		})
}

// proponerEnTicket: al cerrar una orden ligada a un ticket, el ticket recibe una nota interna y,
// si sigue abierto y no le quedan otras órdenes abiertas, la propuesta de resolverlo (soporte
// emite ticket.resolucion_propuesta con origen orden). El ticket nunca se resuelve solo.
func proponerEnTicket(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, resultado string, ahora time.Time) error {
	if f.TicketID == nil {
		return nil
	}
	// Las otras órdenes abiertas se cuentan con el ticket ya bloqueado (soporte lo toma con FOR
	// UPDATE antes de llamar): si dos órdenes del ticket se cierran a la vez, la segunda espera y
	// ve cerrada la primera, así que una de las dos propone.
	propuso, err := soporte.ProponerPorOrden(ctx, t, soporte.CierreDeOrden{
		TicketID: *f.TicketID, OrdenID: f.ID, Numero: f.Numero, Resultado: resultado, Ahora: ahora,
		OtrasAbiertas: func(ctx context.Context) ([]string, error) {
			return q.OrdenesAbiertasDeTicket(ctx, campobd.OrdenesAbiertasDeTicketParams{TicketID: f.TicketID, Excluir: f.ID})
		},
	})
	if err == nil {
		t.Anotar(map[string]any{"ticket_resolucion_propuesta": propuso})
	}
	return err
}

// requisitosDeCierre: los obligatorios de todos los formularios de la orden (uno por uno),
// equipos coherentes con el tipo (instalación: al menos uno instalado; retiro: al menos uno
// retirado, con su condición) y la firma si algún formulario la pide.
func requisitosDeCierre(d OrdenDetalle, tipo campobd.CampoTiposOrden) []nucleo.ErrorCampo {
	errores := requisitosFormularios(d)
	instalados, retirados := 0, 0
	for _, e := range d.Equipos {
		switch e.Accion {
		case AccionInstalar:
			instalados++
		case AccionRetirar, AccionNoRecuperado:
			retirados++
		case AccionCambio:
			instalados++
			retirados++
		}
	}
	if tipo.ExigeInstalar && instalados == 0 {
		errores = append(errores, nucleo.ErrorCampo{Field: "equipos.instalar", Message: "Registra al menos un equipo instalado (serie o unidad)", Rule: "equipo"})
	}
	if tipo.ExigeRetirar && retirados == 0 {
		errores = append(errores, nucleo.ErrorCampo{Field: "equipos.retirar", Message: "Registra al menos un equipo retirado con su condición", Rule: "equipo"})
	}
	if d.RequiereFirma && d.Firma == nil {
		errores = append(errores, nucleo.ErrorCampo{Field: "firma", Message: "Falta la firma del cliente (la pide " + formulariosConFirma(d) + ")", Rule: "firma"})
	}
	return errores
}

// payloadCerrada arma orden.cerrada: lo que el núcleo (I1) necesita para los pendientes en
// Argos (contratos/argosisp.md §3). Materiales sumados por producto, bodega y quién los registró
// (desde la app o el panel): nunca se juntan líneas de varios actores bajo uno (M-268-1).
func payloadCerrada(f campobd.CampoOrden, tipo string, d OrdenDetalle) map[string]any {
	type clave struct {
		producto int64
		bodega   int64
		desdeApp bool
		actor    int64
	}
	orden := []clave{}
	suma := map[clave]map[string]any{}
	for _, mt := range d.Materiales {
		k := clave{mt.ProductoID, deref(mt.BodegaID), mt.desdeApp, deref(mt.registradoPor)}
		if s, ok := suma[k]; ok {
			s["cantidad"] = s["cantidad"].(float64) + mt.Cantidad
			if mt.OcurridoEn.After(s["fecha"].(time.Time)) {
				s["fecha"] = mt.OcurridoEn
			}
			if r, ok := s["recibido_en_iris"].(*time.Time); mt.RecibidoEn != nil && (!ok || r == nil || mt.RecibidoEn.After(*r)) {
				s["recibido_en_iris"] = mt.RecibidoEn
			}
			s["excede_existencia"] = s["excede_existencia"].(bool) || mt.ExcedeExistencia
			if s["modalidad"] == (*string)(nil) && mt.Modalidad != nil {
				s["modalidad"] = mt.Modalidad
			}
			continue
		}
		orden = append(orden, k)
		// modalidad (Argos ≥ 1.6.0, o null): un comodato no va en el consumo (contrato 0.5.0 §3.3).
		suma[k] = map[string]any{"producto_id": mt.ProductoID, "cantidad": mt.Cantidad, "bodega_id": mt.BodegaID,
			"excede_existencia": mt.ExcedeExistencia, "modalidad": mt.Modalidad,
			// 0.12.0 §3.3: la fecha del teléfono (la última del producto) y la del servidor al aceptarlo.
			"fecha": mt.OcurridoEn, "recibido_en_iris": mt.RecibidoEn,
			// desde_app: lo registró un técnico desde el lote (con registrado_por): sale con tecnico,
			// bodega_id y X-Iris-Actor de quien lo usó (M-268-1). Si no, lo indicó despacho en el panel.
			"desde_app": mt.desdeApp, "usuario_id": mt.registradoPor,
			// bodega_tipo: el tipo de la bodega según Argos cuando despacho lo registró (M-4).
			"bodega_tipo": mt.bodegaTipo}
	}
	materiales := make([]map[string]any, 0, len(orden))
	for _, k := range orden {
		s := suma[k]
		s["cantidad"] = float64(int64(s["cantidad"].(float64)*1000+0.5)) / 1000
		materiales = append(materiales, s)
	}
	equipos := make([]map[string]any, 0, len(d.Equipos))
	validados := true
	for _, e := range d.Equipos {
		if e.Accion == AccionRetirar && !e.Validado {
			validados = false
		}
		if (e.Accion == AccionCambio || e.Accion == AccionNoRecuperado) && !e.Validado {
			validados = false
		}
		equipos = append(equipos, equipoCerrada(f, e))
	}
	return map[string]any{
		"orden_id": f.ID, "numero": f.Numero, "tipo": tipo, "resultado": f.Resultado,
		"empresa_id": f.EmpresaID, "cliente_id": f.ClienteID, "servicio_id": f.ServicioID,
		"ticket_id": f.TicketID, "prospecto_id": f.ProspectoID, "tecnico_id": f.TecnicoID, "bodega_id": f.BodegaID,
		"cerrada_en": f.FinalizadaEn, "materiales": materiales, "equipos": equipos,
		// Un retiro exitoso retira el servicio en Argos (POST /servicios/:id/retirar).
		"retirar_servicio": tipo == "retiro" && f.Resultado != nil && *f.Resultado == "exitosa",
		// false: algún equipo retirado no se pudo verificar contra el comodato (sin conector o
		// Argos caído): I1 lo trata como dato no verificado.
		"equipos_validados": validados,
	}
}

// equipoCerrada: el equipo como va en orden.cerrada (y, solo él, al encolar un cambio o un intento
// al aplicarse; 0.14.0 §3.10). Nunca la nota.
func equipoCerrada(f campobd.CampoOrden, e EquipoOrden) map[string]any {
	// condicion: buena | danada | perdido (perdido: desde 0.12.0 nunca sale solo como pérdida;
	// queda para una persona).
	eq := map[string]any{
		"id": e.ID, "accion": e.Accion, "unidad_id": e.UnidadID, "serie": e.Serie, "mac": e.MAC,
		"modelo": e.Modelo, "condicion": e.Condicion, "bodega_id": f.BodegaID, "validado": e.Validado,
		// 0.13.1: quien lo registró desde la app va en X-Iris-Actor (y su I1 como tecnico), también
		// en comodatos y devoluciones.
		"usuario_id": e.registradoPor, "desde_app": e.desdeApp, "enviado_al_aplicar": e.enviadoAlAplicar,
	}
	// 0.12.0 §3.10 (B27): fecha del teléfono y recibido_en_iris; nunca la nota.
	switch e.Accion {
	case AccionCambio, AccionNoRecuperado:
		eq["motivo"], eq["foto_ref"] = e.Motivo, e.FotoAdjuntoID
		eq["fecha"], eq["recibido_en_iris"] = e.OcurridoEn, e.RecibidoEn
	}
	switch e.Accion {
	case AccionCambio:
		var cobro any
		if e.CobroPropuesto != nil {
			cobro = map[string]any{"motivo": *e.CobroPropuesto}
		}
		eq["sale_unidad_id"], eq["destino_sale"], eq["cobro_propuesto"] = e.SaleUnidadID, e.DestinoSale, cobro
		eq["entra"] = map[string]any{"unidad_id": e.UnidadID, "serie": e.Serie, "mac": e.MAC}
	case AccionNoRecuperado:
		eq["ubicacion"] = map[string]any{"lat": e.Lat, "lng": e.Lon, "precision_m": e.PrecisionM}
	}
	return eq
}

// formulariosConFirma: los nombres de los formularios que piden la firma.
func formulariosConFirma(d OrdenDetalle) string {
	nombres := []string{}
	for _, fo := range d.Formularios {
		if fo.RequiereFirma {
			nombres = append(nombres, "«"+fo.Nombre+"»")
		}
	}
	return strings.Join(nombres, ", ")
}

// requisitosAdjuntos: una foto completa por ítem foto obligatorio de los formularios (clave =
// su código) y la firma si algún formulario la pide.
func requisitosAdjuntos(d OrdenDetalle) []Requisito {
	r := fotosObligatorias(d)
	if d.RequiereFirma {
		r = append(r, Requisito{Tipo: "firma", Minimo: 1})
	}
	return r
}

// AccesoAdjuntos es la regla de acceso de una orden para el módulo de adjuntos de I1
// (adjuntos.RegistrarEntidadAdjuntable("orden", …); el líder la conecta): ver = la orden es
// visible para el actor (ordenes.ver con alcance u ordenes.ver_propias); subir = además
// ordenes.registrar con alcance. Si no, el mismo 404 que la orden. Devuelve la empresa de la
// orden (0 = sin empresa).
func AccesoAdjuntos(ctx context.Context, db campobd.DBTX, ordenID string, subir bool) (int64, error) {
	id, err := strconv.ParseInt(ordenID, 10, 64)
	if err != nil || id <= 0 {
		return 0, noEncontrada()
	}
	f, err := campobd.New(db).OrdenPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, noEncontrada()
	}
	if err != nil {
		return 0, err
	}
	permiso := nucleo.PermOrdenesVer
	if subir {
		permiso = nucleo.PermOrdenesRegistrar
	}
	if err := verificarAcceso(ctx, permiso, f); err != nil {
		var p *nucleo.Problema
		if errors.As(err, &p) && p.Status == http.StatusForbidden {
			return 0, noEncontrada()
		}
		return 0, err
	}
	return deref(f.EmpresaID), nil
}

// cargoDe: el cargo único que el cierre pide cobrar en Argos (visita cobrada), o nil. La
// plantilla manda si define cobrar (true o false; con sus propios producto, monto o descripción
// o, si no los trae, los del tipo); si no, el tipo. Formato que espera la cola de I1
// (contratos/eventos.md): {producto_id?, monto? (> 0), descripcion?}. Un tipo o plantilla que
// dice cobrar sin producto ni monto no cobra (la base lo impide en el tipo).
func cargoDe(f campobd.CampoOrden, tipo campobd.CampoTiposOrden, plantilla *campobd.CampoPlantillasChecklist) map[string]any {
	cobrar, producto, monto, descripcion := tipo.Cobrar, tipo.CargoProductoID, tipo.CargoMonto, tipo.CargoDescripcion
	if plantilla != nil && plantilla.Cobrar != nil {
		cobrar = *plantilla.Cobrar
		if plantilla.CargoProductoID != nil || plantilla.CargoMonto.Valid {
			producto, monto = plantilla.CargoProductoID, plantilla.CargoMonto
		}
		if strings.TrimSpace(plantilla.CargoDescripcion) != "" {
			descripcion = plantilla.CargoDescripcion
		}
	}
	valor, err := monto.Float64Value()
	conMonto := err == nil && valor.Valid && valor.Float64 > 0
	if !cobrar || (producto == nil && !conMonto) {
		return nil
	}
	if strings.TrimSpace(descripcion) == "" {
		descripcion = tipo.Nombre
	}
	cargo := map[string]any{"descripcion": strings.TrimSpace(descripcion) + " (" + f.Numero + ")"}
	if producto != nil {
		cargo["producto_id"] = *producto
	}
	if conMonto {
		cargo["monto"] = json.Number(strconv.FormatFloat(valor.Float64, 'f', 2, 64))
	}
	return cargo
}

// tecnicoArgos: { id_externo } del vínculo con Argos de la ficha del técnico, o nil.
func tecnicoArgos(ctx context.Context, q *campobd.Queries, tecnico *int64) (map[string]any, error) {
	if tecnico == nil {
		return nil, nil
	}
	id, err := q.VinculoArgosDeTecnico(ctx, *tecnico)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && id == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id_externo": id}, nil
}
