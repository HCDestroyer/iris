package campo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// EntradaCerrarOrden es la entrada de ordenes.cerrar.
type EntradaCerrarOrden struct {
	ID        int64    `json:"-"`
	Version   int32    `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Resultado string   `json:"resultado,omitempty" enum:"exitosa,parcial" doc:"exitosa por defecto. Solo una instalación exitosa activa el servicio en Argos"`
	Lat       *float64 `json:"lat,omitempty" minimum:"-90" maximum:"90"`
	Lon       *float64 `json:"lon,omitempty" minimum:"-180" maximum:"180"`
	Sync      *OpSync  `json:"-"`
}

func (e EntradaCerrarOrden) ordenID() int64 { return e.ID }

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
			cuando := m.cuandoEn(in.Sync, antes)
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
			faltan, err := m.cfg.Adjuntos.Faltantes(ctx, antes.ID, requisitosAdjuntos(d))
			if err != nil {
				return antes, "", err
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
			if err := t.Emitir("orden.cerrada", payloadCerrada(f, tipo.Codigo, d)); err != nil {
				return f, "", err
			}
			return f, "", notaEnTicket(ctx, t, f, fmt.Sprintf("La orden %s se cerró (%s).", f.Numero, resultado))
		})
}

// requisitosDeCierre: checklist obligatorio completo, equipos coherentes con el tipo
// (instalación: al menos uno instalado; retiro: al menos uno retirado, con su condición) y la
// firma si la plantilla la pide.
func requisitosDeCierre(d OrdenDetalle, tipo campobd.CampoTiposOrden) []nucleo.ErrorCampo {
	var errores []nucleo.ErrorCampo
	for _, it := range d.Checklist {
		if it.Obligatorio && it.Valor == nil {
			errores = append(errores, nucleo.ErrorCampo{Field: "checklist." + it.Codigo, Message: "Falta: " + it.Etiqueta, Rule: "obligatorio"})
		}
	}
	instalados, retirados := 0, 0
	for _, e := range d.Equipos {
		switch e.Accion {
		case AccionInstalar:
			instalados++
		case AccionRetirar:
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
		errores = append(errores, nucleo.ErrorCampo{Field: "firma", Message: "Falta la firma del cliente", Rule: "firma"})
	}
	return errores
}

// payloadCerrada arma orden.cerrada: lo que el núcleo (I1) necesita para los pendientes en
// Argos (contratos/argosisp.md §3). Materiales sumados por producto y bodega.
func payloadCerrada(f campobd.CampoOrden, tipo string, d OrdenDetalle) map[string]any {
	type clave struct {
		producto int64
		bodega   int64
	}
	orden := []clave{}
	suma := map[clave]map[string]any{}
	for _, mt := range d.Materiales {
		k := clave{mt.ProductoID, deref(mt.BodegaID)}
		if s, ok := suma[k]; ok {
			s["cantidad"] = s["cantidad"].(float64) + mt.Cantidad
			s["excede_existencia"] = s["excede_existencia"].(bool) || mt.ExcedeExistencia
			continue
		}
		orden = append(orden, k)
		suma[k] = map[string]any{"producto_id": mt.ProductoID, "cantidad": mt.Cantidad, "bodega_id": mt.BodegaID,
			"excede_existencia": mt.ExcedeExistencia}
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
		// condicion: buena | danada | perdido (perdido → pérdida del comodato en Argos).
		equipos = append(equipos, map[string]any{
			"id": e.ID, "accion": e.Accion, "unidad_id": e.UnidadID, "serie": e.Serie, "mac": e.MAC,
			"modelo": e.Modelo, "condicion": e.Condicion, "bodega_id": f.BodegaID, "validado": e.Validado,
		})
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

// requisitosAdjuntos: una foto completa por ítem foto obligatorio del checklist (clave = su
// código) y la firma si la plantilla la pide.
func requisitosAdjuntos(d OrdenDetalle) []Requisito {
	r := []Requisito{}
	for _, it := range d.Checklist {
		if it.Tipo == ItemFoto && it.Obligatorio {
			r = append(r, Requisito{Tipo: "foto", ClaveChecklist: it.Codigo, Minimo: 1})
		}
	}
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
