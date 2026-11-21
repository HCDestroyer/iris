package campo_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// fotoDe registra una foto de la orden (como POST /adjuntos; puede no haber terminado de subir).
func fotoDe(t *testing.T, orden, autor int64) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO nucleo.adjuntos (id, entidad, entidad_id, tipo, mime, tamano, sha256, subido_por, almacenamiento, clave)
		VALUES ($1, 'orden', $2, 'foto', 'image/jpeg', 10, repeat('a', 64), $3, 'local', $4)`, id, strconv.FormatInt(orden, 10), autor, "orden/"+id.String()); err != nil {
		t.Fatal(err)
	}
	return id
}

func conServicio(t *testing.T, orden, servicio int64) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE campo.ordenes SET servicio_id = $2 WHERE id = $1`, orden, servicio); err != nil {
		t.Fatal(err)
	}
}

// Contrato 0.12.0 §3.10 y §10.7 (B27): cambio de equipo desde la app, con sus reglas.
func TestLoteCambioDeEquipo(t *testing.T) {
	servicio := int64(940000) + time.Now().UnixNano()%10000
	inv := &inventarioFalso{comodato: map[int64][]campo.EquipoComodato{servicio: {{UnidadID: 812, Serie: "ZTEGC1234567"}}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "cambio_equipo", tec)
	conServicio(t, o.ID, servicio)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")

	cambio := func(datos map[string]any) campo.OperacionLote { return op(campo.OpEquipoCambiar, o.ID, datos) }
	// «Funciona» no vale con dañado; la que sale debe estar en comodato del servicio.
	esperar(t, lote(t, e, ct, cambio(map[string]any{"saleUnidadId": 812, "entra": map[string]any{"serie": "ZTEGC8A1B2C3"},
		"motivo": "danado", "destinoSale": "funciona"})), "rechazada:validacion")
	esperar(t, lote(t, e, ct, cambio(map[string]any{"saleUnidadId": 999, "entra": map[string]any{"serie": "ZTEGC8A1B2C3"},
		"motivo": "actualizacion"})), "rechazada:equipo_no_en_comodato")
	// Una fecha anterior a la asignación de la orden: 422.
	vieja := cambio(map[string]any{"saleUnidadId": 812, "entra": map[string]any{"serie": "ZTEGC8A1B2C3"}, "motivo": "danado"})
	antes := time.Now().Add(-30 * 24 * time.Hour)
	vieja.OcurridoEn = &antes
	esperar(t, lote(t, e, ct, vieja), "rechazada:validacion")

	foto := fotoDe(t, o.ID, tec)
	ok := cambio(map[string]any{"saleUnidadId": 812, "entra": map[string]any{"serie": "ztegc8a1b2c3", "mac": "a4-5e-60-11-22-33"},
		"motivo": "danado", "fotoAdjuntoId": foto.String(), "cobroPropuesto": map[string]any{"motivo": "golpe", "nota": "Carcasa <b>rota</b>"},
		"nota": "Cliente avisó del golpe"})
	esperar(t, lote(t, e, ct, ok), "aplicada")
	det := deber(e.m.VerOrdenPropia.Ejecutar(ct, campo.EntradaVerOrden{ID: o.ID}))(t)
	var eq *campo.EquipoOrden
	for i := range det.Equipos {
		if det.Equipos[i].Accion == campo.AccionCambio {
			eq = &det.Equipos[i]
		}
	}
	if eq == nil || eq.SaleUnidadID == nil || *eq.SaleUnidadID != 812 || eq.Serie == nil || *eq.Serie != "ZTEGC8A1B2C3" ||
		eq.MAC == nil || *eq.MAC != "A4:5E:60:11:22:33" || eq.DestinoSale == nil || *eq.DestinoSale != "revision" ||
		eq.CobroPropuesto == nil || *eq.CobroPropuesto != "golpe" || eq.RecibidoEn == nil || !eq.Validado ||
		eq.Nota == nil || *eq.Nota != "Cliente avisó del golpe" {
		t.Fatalf("cambio guardado (por defecto a revisión, verificado contra el comodato): %+v", eq)
	}
	// La nota no queda en la auditoría.
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios WHERE entidad = 'orden_equipo' AND entidad_id = $1
		AND (despues::text LIKE '%golpe%avis%' OR despues::text LIKE '%Carcasa%' OR despues::text LIKE '%Cliente avisó%')`, ok.ID); n != 0 {
		t.Fatalf("la nota no se audita: %d", n)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.cambiar_equipo' AND entrada::text LIKE '%Cliente avisó%'`); n != 0 {
		t.Fatalf("la nota no va en la entrada auditada: %d", n)
	}
	// En una orden de otro tipo, no.
	otra := programada(t, e, cd, "visita", tec)
	conServicio(t, otra.ID, servicio)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, otra.ID, nil),
		op(campo.OpEquipoCambiar, otra.ID, map[string]any{"saleUnidadId": 812, "entra": map[string]any{"serie": "X1"}, "motivo": "otro"})),
		"aplicada", "rechazada:equipo_no_admitido")
}

// Contrato 0.12.0 §3.10 y §10.7 a (B27): «No se pudo recuperar» en un retiro: motivo, foto
// obligatoria si se negó o se perdió, ubicación en sitio y un intento por unidad al día.
func TestLoteNoRecuperado(t *testing.T) {
	servicio := int64(950000) + time.Now().UnixNano()%10000
	unidad := int64(7_000_000) + time.Now().UnixNano()%100000
	inv := &inventarioFalso{comodato: map[int64][]campo.EquipoComodato{servicio: {{UnidadID: unidad, Serie: "TPL-91AA"}}}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "retiro", tec)
	conServicio(t, o.ID, servicio)
	intento := func(orden int64, datos map[string]any) campo.OperacionLote {
		return op(campo.OpEquipoNoRecuperado, orden, datos)
	}
	base := func(motivo string) map[string]any {
		return map[string]any{"unidadId": unidad, "motivo": motivo, "lat": 14.6, "lon": -90.5, "precisionM": 12}
	}
	// Antes de llegar (en camino), no: el intento se registra en sitio.
	esperar(t, lote(t, e, ct, op(campo.OpIniciarCamino, o.ID, map[string]any{"lat": 14.6, "lon": -90.5}), intento(o.ID, base("cliente_ausente"))),
		"aplicada", "rechazada:transicion_invalida")
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")
	// Se negó sin foto, o sin ubicación: 422.
	esperar(t, lote(t, e, ct, intento(o.ID, base("se_nego"))), "rechazada:validacion")
	sinUbicacion := base("cliente_ausente")
	delete(sinUbicacion, "lat")
	esperar(t, lote(t, e, ct, intento(o.ID, sinUbicacion)), "rechazada:validacion")
	// Con foto de esta orden: aplicada.
	conFoto := base("se_nego")
	conFoto["fotoAdjuntoId"] = fotoDe(t, o.ID, tec).String()
	esperar(t, lote(t, e, ct, intento(o.ID, conFoto)), "aplicada")
	// Una foto de otra orden no vale.
	otra := programada(t, e, cd, "retiro", tec)
	conServicio(t, otra.ID, servicio)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, otra.ID, nil)), "aplicada")
	ajena := base("equipo_perdido")
	ajena["fotoAdjuntoId"] = fotoDe(t, o.ID, tec).String()
	esperar(t, lote(t, e, ct, intento(otra.ID, ajena)), "rechazada:validacion")
	// Otro intento de la misma unidad el mismo día (en otra orden): 429 demasiados_intentos.
	r := lote(t, e, ct, intento(otra.ID, base("cliente_ausente")))
	esperar(t, r, "rechazada:demasiados_intentos")
	if r.Resultados[0].Error.Status != 429 {
		t.Fatalf("429: %+v", r.Resultados[0].Error)
	}
	// Una unidad que no está en comodato del servicio: no.
	ajenaUnidad := base("cliente_ausente")
	ajenaUnidad["unidadId"] = unidad + 1
	esperar(t, lote(t, e, ct, intento(otra.ID, ajenaUnidad)), "rechazada:equipo_no_en_comodato")
	// En una instalación, no.
	inst := programada(t, e, cd, "instalacion", tec)
	conServicio(t, inst.ID, servicio)
	esperar(t, lote(t, e, ct, op(campo.OpLlegar, inst.ID, nil), intento(inst.ID, base("otro"))), "aplicada", "rechazada:equipo_no_admitido")
}
