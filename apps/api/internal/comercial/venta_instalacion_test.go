package comercial_test

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
	"github.com/HCDestroyer/iris/apps/api/internal/notificaciones"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// ventaEnLaInstalacion: de la venta a la instalación (contrato argosisp.md 0.10.0 §3.4.8). Recibe
// un prospecto ganado con el alta en Argos esperando la activación de su orden.
//
//   - B20 (ALTA 1 de #280): el cambio de plan y de titular hechos en Argos crean una revisión nueva
//     de la cotización, **vigente** y con vigencia nueva, y un contrato regenerado en borrador a
//     nombre del titular, ligado a la orden. El contrato firmado sigue vivo hasta que el titular
//     firma el regenerado en la orden (orden.firmar_contrato): ahí el anterior se anula y la
//     revisión queda aceptada. Avisan al vendedor (prospecto.venta_revisada). Repetir no crea otra.
//   - B17: servicio.titular_cambiado pasa el prospecto al cliente nuevo solo si la instantánea dice
//     que el servicio es suyo; la activación hecha lo completa.
func ventaEnLaInstalacion(t *testing.T, e *entorno, ctx context.Context, prospecto int64, d comercial.ProspectoDetalle) {
	t.Helper()
	orden := *d.Alta.OrdenID
	antes := d.Cotizacion
	contratoAntes := d.Contrato
	if antes == nil || contratoAntes == nil || contratoAntes.Estado != "firmado" {
		t.Fatalf("venta sin cotización o contrato firmado: %+v", d)
	}
	clavePlan := "iris:plan:" + strconv.FormatInt(orden, 10) + ":1"
	// La vigencia de la venta ya venció: la revisión trae una nueva.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET vigente_hasta = now() - interval '1 day' WHERE id = $1", antes.ID); err != nil {
		t.Fatal(err)
	}

	// Un usuario no revisa la venta (lo hace el sistema al quedar hecho el pendiente).
	if _, err := e.com.RevisarVenta(ctx, comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan, PlanID: ptr(int64(301)), Clave: clavePlan}); err == nil {
		t.Fatal("un usuario revisó la venta")
	}
	r := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: clavePlan}))(t)
	if r.CotizacionID == nil || r.ContratoID == nil {
		t.Fatalf("revisión: %+v", r)
	}
	v := deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	// Lo vivo no cambia hasta la firma: la cotización aceptada y el contrato firmado.
	if v.Cotizacion == nil || v.Cotizacion.ID != antes.ID || v.Cotizacion.Estado != "aceptada" || v.Contrato == nil ||
		v.Contrato.ID != contratoAntes.ID || v.Contrato.Estado != "firmado" {
		t.Fatalf("lo vivo cambió sin la firma: %+v %+v", v.Cotizacion, v.Contrato)
	}
	pf := v.PorFirmarEnOrden
	if pf == nil || pf.OrdenID != orden || pf.Cotizacion.ID != *r.CotizacionID || pf.Cotizacion.Estado != "vigente" || pf.Cotizacion.Vencida ||
		pf.Cotizacion.Revision != antes.Revision+1 || pf.Cotizacion.PlanID == nil || *pf.Cotizacion.PlanID != 301 ||
		pf.Cotizacion.Lineas[0].Descripcion != "Fibra 200 (sin precio)" || pf.Cotizacion.TotalMensual != antes.TotalMensual ||
		pf.Contrato.ID != *r.ContratoID || pf.Contrato.Estado != "borrador" || pf.Contrato.CotizacionID != pf.Cotizacion.ID {
		t.Fatalf("revisión por firmar: %+v", pf)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'firmado'", contratoAntes.ID); n != 1 {
		t.Fatal("el contrato firmado se anuló antes de firmar el regenerado")
	}
	// El vendedor no lo firma desde el panel: el regenerado solo se firma en sitio (409).
	_, err := e.com.FirmarContrato(ctx, comercial.EntradaFirmar{ContratoID: pf.Contrato.ID, Version: pf.Contrato.Version,
		FirmanteNombre: "Ana López", SVG: firmaSVG})
	problema(t, err, 409, "se_firma_en_la_orden")
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND (contenido->>'ordenId')::bigint = $2", *r.ContratoID, orden); n != 1 {
		t.Fatal("el contrato regenerado no apunta a la orden donde se firma")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'prospecto.venta_revisada' AND (payload->>'prospecto_id')::bigint = $1 AND payload->>'motivo' = 'plan'", prospecto); n != 1 {
		t.Fatal("sin aviso al vendedor")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'cotizacion.aceptada' AND (payload->>'cotizacion_id')::bigint = $1", *r.CotizacionID); n != 0 {
		t.Fatal("la revisión se dio por aceptada sin la firma")
	}
	// Repetido (el evento llegó dos veces): nada nuevo.
	if r2 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: clavePlan}))(t); r2.CotizacionID != nil {
		t.Fatalf("revisión repetida: %+v", r2)
	}

	// Cambio de titular (aún sin firmar el de plan): la revisión anterior queda reemplazada y su
	// contrato anulado; el nuevo va a nombre del titular confirmado en la orden.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"INSERT INTO campo.orden_titular (orden_id, empresa_id, quien_firma, documento_tipo, nombre) VALUES ($1, 1, 'otra', 'dpi', 'Ana Pérez')", orden); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM campo.orden_titular WHERE orden_id = $1", orden)
	})
	rt := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorTitular,
		Clave: "iris:titular:" + strconv.FormatInt(orden, 10) + ":1"}))(t)
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND contenido->'cliente'->>'nombre' = 'Ana Pérez' AND contenido->'cliente'->>'documento' = ''", *rt.ContratoID); n != 1 {
		t.Fatal("el contrato no va a nombre del titular nuevo (sin su documento)")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'reemplazada' AND reemplazada_por = $2", *r.CotizacionID, *rt.CotizacionID); n != 1 {
		t.Fatal("la revisión sin firmar no quedó reemplazada")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'anulado'", *r.ContratoID); n != 1 {
		t.Fatal("el regenerado sin firmar no quedó anulado")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND plan_id = 301", *rt.CotizacionID); n != 1 {
		t.Fatal("la revisión del titular perdió el plan nuevo")
	}

	// La firma en la orden: el técnico asignado, en sitio, con el lote (también sin señal).
	tec := usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, tec, 1)
	ct := enEmpresa(t, tec, 1, "tecnico")
	firmar := func(c context.Context, contrato int64) campo.ResultadoOperacion {
		t.Helper()
		id, _ := uuid.NewV7()
		res, err := e.cmp.Lote(c, campo.EntradaLote{DispositivoID: "tel-venta-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{{
			ID: id.String(), Tipo: campo.OpFirmarContrato, OrdenID: orden,
			Datos: map[string]any{"contratoId": contrato, "firmanteNombre": "Ana Pérez", "firmanteDocumento": "1234567890101", "svg": firmaSVG}}}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Resultados[0]
	}
	// Sin comercial conectado: 503; de otro técnico o fuera de sitio: no se firma.
	if res := firmar(ct, *rt.ContratoID); res.Estado != "rechazada" || res.Error == nil || res.Error.Codigo() != "no_disponible" {
		t.Fatalf("sin comercial: %+v", res)
	}
	e.cmp.ConectarContratos(e.com)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET tecnico_id = $2, estado = 'en_camino' WHERE id = $1", orden, tec); err != nil {
		t.Fatal(err)
	}
	if res := firmar(ct, *rt.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("fuera de sitio: %+v", res)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', en_sitio_en = now() WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}
	otro := usuarioCon(t, rol("tecnico"))
	asignarEmpresa(t, otro, 1)
	if res := firmar(enEmpresa(t, otro, 1, "tecnico"), *rt.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("otro técnico firmó: %+v", res)
	}
	// El contrato anulado (de la revisión reemplazada) no se firma.
	if res := firmar(ct, *r.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("contrato anulado firmado: %+v", res)
	}
	if res := firmar(ct, *rt.ContratoID); res.Estado != "aplicada" {
		t.Fatalf("firma en la orden: %+v", res)
	}
	v = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	if v.PorFirmarEnOrden != nil || v.Contrato == nil || v.Contrato.ID != *rt.ContratoID || v.Contrato.Estado != "firmado" ||
		v.Cotizacion == nil || v.Cotizacion.ID != *rt.CotizacionID || v.Cotizacion.Estado != "aceptada" {
		t.Fatalf("tras la firma: %+v %+v %+v", v.PorFirmarEnOrden, v.Contrato, v.Cotizacion)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'anulado'", contratoAntes.ID); n != 1 {
		t.Fatal("el contrato anterior no se anuló al firmar el regenerado")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'reemplazada' AND reemplazada_por = $2", antes.ID, *rt.CotizacionID); n != 1 {
		t.Fatal("la cotización aceptada anterior no quedó reemplazada por la revisión")
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'contrato.firmado' AND (payload->>'contrato_id')::bigint = $1 AND (payload->>'orden_id')::bigint = $2", *rt.ContratoID, orden); n != 1 {
		t.Fatal("sin contrato.firmado con la orden")
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.firmar_contrato' AND entrada::text LIKE '%M 10 40%'"); n != 0 {
		t.Fatal("el trazo de la firma quedó en la auditoría")
	}
	// Ya firmado: otra firma no.
	if res := firmar(ct, *rt.ContratoID); res.Estado != "rechazada" {
		t.Fatalf("firmado dos veces: %+v", res)
	}

	// Ciclo de la orden con las acciones reales de campo y los eventos por el bus (como en
	// cmd/worker): la anulación y la regeneración del contrato por firmar.
	e.com.SuscribirRevisionVenta(e.bus, pruebas.Pool(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	sup := usuarioCon(t, rol("supervisor"))
	asignarEmpresa(t, sup, 1)
	cs := enEmpresa(t, sup, 1, "supervisor")
	version := func() int32 {
		t.Helper()
		return deber(e.cmp.VerOrden.Ejecutar(cs, campo.EntradaVerOrden{ID: orden}))(t).Version
	}
	auditada := func(accion string, contrato int64) {
		t.Helper()
		if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = $1 AND entidad = 'contrato' AND entidad_id = $2 AND resultado = 'ok'",
			accion, strconv.FormatInt(contrato, 10)); n != 1 {
			t.Fatalf("sin auditoría de %s del contrato %d", accion, contrato)
		}
	}
	creado := func(contrato int64) {
		t.Helper()
		if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'contrato.creado' AND (payload->>'contrato_id')::bigint = $1", contrato); n != 1 {
			t.Fatalf("sin contrato.creado del %d", contrato)
		}
	}
	porFirmar := func() *comercial.RevisionEnOrden {
		t.Helper()
		return deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t).PorFirmarEnOrden
	}

	// Cancelar la orden (ordenes.cancelar) con una revisión por firmar anula el regenerado; la
	// revisión queda sin aceptar y el firmado, vivo.
	r3 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(300)), Clave: "iris:plan:" + strconv.FormatInt(orden, 10) + ":2"}))(t)
	if r3.ContratoID == nil {
		t.Fatalf("tercera revisión: %+v", r3)
	}
	deber(e.cmp.CancelarOrden.Ejecutar(cs, campo.EntradaMotivo{ID: orden, Version: version(), Motivo: "El cliente pidió posponer"}))(t)
	e.relay()
	auditada("prospectos.revision_sin_firma", *r3.ContratoID)
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE (id = $1 AND estado = 'anulado') OR (id = $2 AND estado = 'firmado')", *r3.ContratoID, *rt.ContratoID); n != 2 {
		t.Fatal("cancelar no anuló el regenerado o tocó el firmado")
	}
	if pf := porFirmar(); pf != nil {
		t.Fatalf("por firmar con la orden cancelada: %+v", pf)
	}
	if _, err := e.com.OrdenTerminada(ctx, comercial.EntradaOrdenTerminada{OrdenID: orden, Estado: "cancelada"}); err == nil {
		t.Fatal("un usuario anuló el regenerado")
	}

	// Argos acepta otro cambio de plan con la orden ya cancelada: la revisión se crea igual
	// (reemplaza a la anterior), sin contrato.
	r4 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: "iris:plan:" + strconv.FormatInt(orden, 10) + ":3"}))(t)
	if r4.CotizacionID == nil || r4.ContratoID != nil {
		t.Fatalf("revisión con la orden cancelada: %+v", r4)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'reemplazada' AND reemplazada_por = $2", *r3.CotizacionID, *r4.CotizacionID); n != 1 {
		t.Fatal("la revisión anterior no quedó reemplazada")
	}
	// Vence mientras la orden está cancelada: al reabrir recibe vigencia nueva.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET vigente_hasta = now() - interval '1 day' WHERE id = $1", *r4.CotizacionID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.com.OrdenReabierta(ctx, comercial.EntradaOrdenTerminada{OrdenID: orden, Estado: "pendiente", De: "cancelada"}); err == nil {
		t.Fatal("un usuario regeneró el contrato")
	}

	// Reabrir (ordenes.reabrir): el contrato se regenera con la última revisión (plan 301).
	deber(e.cmp.ReabrirOrden.Ejecutar(cs, campo.EntradaMotivo{ID: orden, Version: version(), Motivo: "El cliente ya puede recibirnos"}))(t)
	e.relay()
	pf = porFirmar()
	if pf == nil || pf.Cotizacion.ID != *r4.CotizacionID || pf.Cotizacion.PlanID == nil || *pf.Cotizacion.PlanID != 301 || pf.Cotizacion.Vencida ||
		pf.Contrato.Estado != "borrador" || pf.Contrato.CotizacionID != *r4.CotizacionID {
		t.Fatalf("por firmar tras reabrir: %+v", pf)
	}
	reabierto := pf.Contrato.ID
	auditada("prospectos.revision_por_firmar", reabierto)
	creado(reabierto)
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND pdf_adjunto_id IS NOT NULL AND vigente_hasta > now()", *r4.CotizacionID); n != 1 {
		t.Fatal("la revisión renovada sin vigencia o sin PDF")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'firmado'", *rt.ContratoID); n != 1 {
		t.Fatal("el firmado no sigue vivo mientras se espera la firma")
	}
	// El evento repetido no crea otro.
	if r2 := deber(e.com.OrdenReabierta(sistema(t), comercial.EntradaOrdenTerminada{OrdenID: orden, Estado: "pendiente", De: "cancelada"}))(t); r2.ContratoID != nil {
		t.Fatalf("reabierta repetida: %+v", r2)
	}

	// Fallida → reprogramada (ordenes.fallar y ordenes.reprogramar): se anula y se regenera igual.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', en_sitio_en = now(), programada_inicio = now() WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}
	deber(e.cmp.FallarOrden.Ejecutar(ct, campo.EntradaFallarOrden{ID: orden, Version: version(), MotivoFalla: "cliente_ausente"}))(t)
	e.relay()
	auditada("prospectos.revision_sin_firma", reabierto)
	// Reprogramar pide la ficha del técnico en la agenda, con turno.
	if _, err := pruebas.Pool(t).Exec(context.Background(), `
		WITH f AS (INSERT INTO agenda.tecnicos (usuario_id) VALUES ($1) RETURNING id)
		INSERT INTO agenda.turnos (tecnico_id, dia, inicio, fin) SELECT f.id, d, '00:00', '24:00' FROM f, generate_series(1, 7) d`, tec); err != nil {
		t.Fatal(err)
	}
	gt := time.FixedZone("America/Guatemala", -6*3600)
	pasado := time.Now().In(gt).AddDate(0, 0, 2)
	inicio := time.Date(pasado.Year(), pasado.Month(), pasado.Day(), 10, 0, 0, 0, gt).UTC()
	deber(e.cmp.ReprogramarOrden.Ejecutar(cs, campo.EntradaProgramarOrden{ID: orden, Version: version(), Inicio: inicio, Motivo: ptr("Vuelve el cliente el jueves")}))(t)
	e.relay()
	pf = porFirmar()
	if pf == nil || pf.Contrato.ID == reabierto || pf.Cotizacion.ID != *r4.CotizacionID {
		t.Fatalf("por firmar tras reprogramar: %+v", pf)
	}
	reprogramado := pf.Contrato.ID
	auditada("prospectos.revision_por_firmar", reprogramado)
	creado(reprogramado)

	// Se firma en sitio: el cambio de contrato y la revisión aceptada.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', en_sitio_en = now() WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}
	if res := firmar(ct, reprogramado); res.Estado != "aplicada" {
		t.Fatalf("firma tras reprogramar: %+v", res)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE (id = $1 AND estado = 'firmado') OR (id = $2 AND estado = 'anulado')", reprogramado, *rt.ContratoID); n != 2 {
		t.Fatal("la firma tras reprogramar no hizo el cambio de contrato")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'aceptada'", *r4.CotizacionID); n != 1 {
		t.Fatal("la revisión no quedó aceptada con la firma")
	}

	// Las notas de la revisión salen en el PDF del cliente: sin avisos internos (orden cancelada,
	// regenerar al reabrir…), también después de renovar la vigencia.
	if txt := textoPDF(t, deber(e.com.PDFCotizacionDePrueba(ctx, *r4.CotizacionID))(t)); !strings.Contains(txt, "Notas") ||
		strings.Contains(txt, "cancelada") || strings.Contains(txt, "reabrir") || strings.Contains(txt, "se acepta al firmar") {
		t.Fatalf("aviso interno en el PDF de la revisión: %q", txt)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios WHERE entidad = 'cotizacion' AND entidad_id = $1 AND campo = 'pdf_adjunto_id'`,
		strconv.FormatInt(*r4.CotizacionID, 10)); n == 0 {
		t.Fatal("renovar la vigencia sin el PDF nuevo en el diff")
	}

	// Orden cerrada (verificación de #280). Con un regenerado sin firmar, el cierre se rechaza con
	// 422 contrato_por_firmar (en el lote y en el panel); un supervisor cierra igual desde el panel
	// con motivo: el regenerado se anula (solo si sigue en borrador), sale orden.contrato_sin_firma
	// y llega a la campana de los supervisores de la orden. La revisión aceptada y el firmado siguen.
	notif := notificaciones.Nuevo(notificaciones.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	notif.SuscribirAvisos(e.bus, pruebas.Pool(t))
	// Lo demás del cierre (checklist y equipo) no es lo que se prueba aquí.
	for _, sql := range []string{"DELETE FROM campo.orden_formularios WHERE orden_id = $1", "UPDATE campo.tipos_orden SET exige_instalar = false WHERE codigo = 'instalacion' AND $1 > 0"} {
		if _, err := pruebas.Pool(t).Exec(context.Background(), sql, orden); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.tipos_orden SET exige_instalar = true WHERE codigo = 'instalacion'")
	})
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.orden_titular SET validada_en = now(), validada_por = $2, validada_motivo = 'Validada para la prueba' WHERE orden_id = $1", orden, sup); err != nil {
		t.Fatal(err)
	}
	r5 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(300)), Clave: "iris:plan:" + strconv.FormatInt(orden, 10) + ":4"}))(t)
	if r5.ContratoID == nil {
		t.Fatalf("revisión en sitio: %+v", r5)
	}
	idCierre, _ := uuid.NewV7()
	lc, err := e.cmp.Lote(ct, campo.EntradaLote{DispositivoID: "tel-cierre-" + pruebas.Unico(), Operaciones: []campo.OperacionLote{
		{ID: idCierre.String(), Tipo: campo.OpCerrar, OrdenID: orden}}})
	if err != nil {
		t.Fatal(err)
	}
	if res := lc.Resultados[0]; res.Estado != "rechazada" || res.Error == nil || res.Error.Codigo() != "contrato_por_firmar" {
		t.Fatalf("cierre sin firma en el lote: %+v", res)
	}
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	// El técnico no cierra sin la firma ni con motivo.
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: ptr("El cliente no quiso firmar hoy")})
	problema(t, err, 403, "prohibido")
	deber(e.cmp.CerrarOrden.Ejecutar(cs, campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: ptr("El titular no estaba; firma en la oficina")}))(t)
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.cerrar' AND entidad_id = $1 AND resultado = 'ok'
		AND entrada->>'motivoSinContratoFirmado' = 'El titular no estaba; firma en la oficina'`, strconv.FormatInt(orden, 10)); n != 1 {
		t.Fatal("el cierre sin firma sin el motivo en la auditoría")
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'anulado'", *r5.ContratoID); n != 1 {
		t.Fatal("el regenerado no se anuló al cerrar")
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND (payload->>'orden_id')::bigint = $1
		AND (payload->>'contrato_id')::bigint = $2 AND payload->>'operacion' = 'cambiar_plan' AND payload->>'motivo' = 'cierre_sin_firma'`, orden, *r5.ContratoID); n != 1 {
		t.Fatal("sin orden.contrato_sin_firma")
	}
	clave := "contrato_sin_firma:" + strconv.FormatInt(*r5.ContratoID, 10)
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND tipo = 'contrato_sin_firma' AND clave = $2", sup, clave); n != 1 {
		t.Fatal("la campana del supervisor no recibió el cierre sin firma")
	}
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND clave = $2", tec, clave); n != 0 {
		t.Fatal("el aviso de supervisión le llegó al técnico")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND estado = 'firmado'", reprogramado); n != 1 {
		t.Fatal("el contrato firmado no sigue vivo al cerrar")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE id = $1 AND estado = 'aceptada'", *r4.CotizacionID); n != 1 {
		t.Fatal("la revisión aceptada no sigue viva al cerrar")
	}
	// Un segundo OrdenTerminada con la orden cerrada no hace nada; tampoco anula un firmado.
	if rc := deber(e.com.OrdenTerminada(sistema(t), comercial.EntradaOrdenTerminada{OrdenID: orden, Estado: "cerrada", De: "en_sitio"}))(t); rc.ContratoID != nil {
		t.Fatalf("segundo OrdenTerminada: %+v", rc)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND (payload->>'orden_id')::bigint = $1", orden); n != 1 {
		t.Fatal("orden.contrato_sin_firma repetido")
	}
	// Argos acepta tarde otro cambio de plan con la orden cerrada: revisión sin contrato, nada por firmar.
	r6 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(301)), Clave: "iris:plan:" + strconv.FormatInt(orden, 10) + ":5"}))(t)
	if r6.CotizacionID == nil || r6.ContratoID != nil {
		t.Fatalf("revisión con la orden cerrada: %+v", r6)
	}
	if pf := porFirmar(); pf != nil {
		t.Fatalf("por firmar con la orden cerrada: %+v", pf)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'prospecto.venta_revisada' AND (payload->>'cotizacion_id')::bigint = $1 AND payload->'contrato_id' = 'null'::jsonb", *r6.CotizacionID); n != 1 {
		t.Fatal("venta_revisada sin contrato con la orden cerrada")
	}
	// La revisión de Argos tardía de r6 avisa a supervisión aunque no haya contrato que anular.
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND (payload->>'cotizacion_id')::bigint = $1
		AND payload->'contrato_id' = 'null'::jsonb AND payload->>'motivo' = 'cambio_tras_cierre'`, *r6.CotizacionID); n != 1 {
		t.Fatal("sin orden.contrato_sin_firma de la revisión tardía")
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND clave = $2", sup,
		"contrato_sin_firma:cotizacion:"+strconv.FormatInt(*r6.CotizacionID, 10)); n != 1 {
		t.Fatal("la campana no recibió la revisión tardía")
	}

	// Carrera (verificación de #280): el cambio de plan sigue en la cola de Argos, o ya está hecho
	// pero el relay todavía no creó la revisión: tampoco cierra sin la firma.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', finalizada_en = NULL, resultado = NULL WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}
	clave7 := "iris:plan:" + strconv.FormatInt(orden, 10) + ":7"
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.pendientes (operacion, idempotency_key, entidad, entidad_id, metodo, ruta, estado)
		VALUES ('cambiar_plan', $1, 'orden', $2, 'POST', '/servicios/1/cambiar-plan', 'pendiente')`, clave7, strconv.FormatInt(orden, 10)); err != nil {
		t.Fatal(err)
	}
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.pendientes SET estado = 'hecho' WHERE idempotency_key = $1", clave7); err != nil {
		t.Fatal(err)
	}
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	// El técnico asignado no se autoriza a sí mismo, aunque tenga el permiso; un token de API tampoco.
	motivo7 := "Argos ya tiene el plan; el titular firma en la oficina"
	tecSup := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), tec, 1, append(permisosDe("tecnico"), nucleo.PermOrdenesValidarIdentidad)...)
	_, err = e.cmp.CerrarOrden.Ejecutar(tecSup, campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: &motivo7})
	problema(t, err, 403, "prohibido")
	_, err = e.cmp.CerrarOrden.Ejecutar(contexto.ConCanal(cs, contexto.CanalAPI), campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: &motivo7})
	problema(t, err, 403, "prohibido")
	deber(e.cmp.CerrarOrden.Ejecutar(cs, campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: &motivo7}))(t)
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.cerrar' AND resultado = 'ok' AND entrada::text LIKE '%' || $1 || '%'", motivo7); n != 1 {
		t.Fatal("el motivo del supervisor no está en la entrada auditada")
	}
	e.relay()
	// Llega la revisión (relay del pendiente hecho): sin contrato, y la campana de supervisión.
	r7 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan,
		PlanID: ptr(int64(300)), Clave: clave7}))(t)
	if r7.CotizacionID == nil || r7.ContratoID != nil {
		t.Fatalf("revisión tras el cierre: %+v", r7)
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND tipo = 'contrato_sin_firma' AND clave = $2", sup,
		"contrato_sin_firma:cotizacion:"+strconv.FormatInt(*r7.CotizacionID, 10)); n != 1 {
		t.Fatal("la campana no recibió el cambio aplicado tras el cierre")
	}
	if pf := porFirmar(); pf != nil {
		t.Fatalf("por firmar tras el cierre: %+v", pf)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', finalizada_en = NULL, resultado = NULL WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}

	// Las claves de esta parte: un pendiente por cambio (como los que deja la cola de Argos).
	ordenTxt := strconv.FormatInt(orden, 10)
	pendienteDe := func(clave, estado, payload string) {
		t.Helper()
		if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.pendientes (operacion, idempotency_key, entidad, entidad_id, metodo, ruta, estado, payload)
			VALUES ('cambiar_plan', $1, 'orden', $2, 'POST', '/servicios/1/cambiar-plan', $3, $4::jsonb)`, clave, ordenTxt, estado, payload); err != nil {
			t.Fatal(err)
		}
	}
	quitarPendiente := func(clave string) {
		t.Helper()
		if _, err := pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.pendientes WHERE idempotency_key = $1", clave); err != nil {
			t.Fatal(err)
		}
	}
	quitarPendiente(clave7)

	// M1: cancelada, Argos acepta el cambio (revisión sin contrato), se reabre y se cierra antes de
	// que OrdenReabierta regenere: 422 (la revisión no está firmada). Si un supervisor la cierra igual,
	// OrdenReabierta encuentra la orden cerrada y avisa a supervisión.
	clave8 := "iris:plan:" + ordenTxt + ":8"
	deber(e.cmp.CancelarOrden.Ejecutar(cs, campo.EntradaMotivo{ID: orden, Version: version(), Motivo: "El cliente pidió otra fecha"}))(t)
	pendienteDe(clave8, "hecho", `{"plan_id": 301}`)
	r8 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan, PlanID: ptr(int64(301)), Clave: clave8}))(t)
	if r8.CotizacionID == nil || r8.ContratoID != nil {
		t.Fatalf("revisión con la orden cancelada: %+v", r8)
	}
	deber(e.cmp.ReabrirOrden.Ejecutar(cs, campo.EntradaMotivo{ID: orden, Version: version(), Motivo: "El cliente ya puede recibirnos"}))(t)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', en_sitio_en = now() WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	deber(e.cmp.CerrarOrden.Ejecutar(cs, campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: ptr("El titular firma la próxima visita")}))(t)
	e.relay()
	e.relay()
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND (payload->>'cotizacion_id')::bigint = $1
		AND payload->>'motivo' = 'sin_regenerar'`, *r8.CotizacionID); n != 1 {
		t.Fatal("OrdenReabierta con la orden ya cerrada no avisó")
	}
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND clave = $2", sup,
		"contrato_sin_firma:cotizacion:"+strconv.FormatInt(*r8.CotizacionID, 10)); n != 1 {
		t.Fatal("la campana no recibió la orden cerrada antes de regenerar")
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'prospectos.revision_por_firmar' AND resultado = 'ok' AND entrada->'auditoria'->>'aviso' = 'sin_regenerar'"); n != 1 {
		t.Fatalf("auditoría de sin_regenerar: %d", n)
	}
	quitarPendiente(clave8)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET estado = 'reemplazada' WHERE id = $1", *r8.CotizacionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', finalizada_en = NULL, resultado = NULL WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}

	// M2: un fallido_negocio que un supervisor resolvió a mano (lo corrigió en Argos) bloquea el
	// cierre hasta firmar, y su evento revisa la venta; resuelto por reemplazo, no.
	resueltoAMano := func(clave, como string) {
		t.Helper()
		pendienteDe(clave, "fallido_negocio", `{"plan_id": 300}`)
		if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE integracion.pendientes SET estado = 'resuelto_manual', resuelto_como = $2,
			resuelto_motivo = 'Corregido en Argos', resuelto_en = now() WHERE idempotency_key = $1`, clave, como); err != nil {
			t.Fatal(err)
		}
		if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.outbox (id, evento, empresa_id, payload)
			SELECT gen_random_uuid(), 'integracion.pendiente_resuelto_manual', 1, jsonb_build_object('pendiente_id', id, 'operacion', operacion,
				'entidad', 'orden', 'entidad_id', entidad_id, 'idempotency_key', idempotency_key, 'de', 'fallido_negocio', 'motivo', 'Corregido en Argos')
			FROM integracion.pendientes WHERE idempotency_key = $1`, clave); err != nil {
			t.Fatal(err)
		}
	}
	clave9, clave10 := "iris:plan:"+ordenTxt+":9", "iris:plan:"+ordenTxt+":10"
	resueltoAMano(clave9, "manual")
	resueltoAMano(clave10, "reemplazado")
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	e.relay()
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE idempotencia = $1 AND estado = 'vigente'", "iris:venta:"+clave9); n != 1 {
		t.Fatal("el resuelto a mano no revisó la venta")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE idempotencia = $1", "iris:venta:"+clave10); n != 0 {
		t.Fatal("un resuelto por reemplazo revisó la venta")
	}
	pf = porFirmar()
	if pf == nil {
		t.Fatal("sin contrato por firmar tras el resuelto a mano")
	}
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	if res := firmar(ct, pf.Contrato.ID); res.Estado != "aplicada" {
		t.Fatalf("firma del resuelto a mano: %+v", res)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.cotizaciones WHERE idempotencia = $1 AND estado = 'aceptada'", "iris:venta:"+clave9); n != 1 {
		t.Fatal("la revisión del resuelto a mano no quedó aceptada al firmar")
	}
	quitarPendiente(clave9)
	quitarPendiente(clave10)

	// Bajo: un cambio de plan hecho sin plan_id no tiene revisión posible: la campana lo explica.
	clave11 := "iris:plan:" + ordenTxt + ":11"
	pendienteDe(clave11, "hecho", `{}`)
	// El evento llega dos veces (reintento del relay): un solo aviso y una sola acción auditada.
	for range 2 {
		if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.outbox (id, evento, empresa_id, payload)
			VALUES (gen_random_uuid(), 'integracion.pendiente_hecho', 1, jsonb_build_object('pendiente_id', 1, 'operacion', 'cambiar_plan',
				'entidad', 'orden', 'entidad_id', $1::text, 'idempotency_key', $2::text))`, ordenTxt, clave11); err != nil {
			t.Fatal(err)
		}
		e.relay()
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND payload->>'clave' = $1 AND payload->>'motivo' = 'sin_plan'", clave11); n != 1 {
		t.Fatalf("sin_plan repetido: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'prospectos.revision_imposible' AND resultado = 'ok' AND entrada->>'clave' = $1", clave11); n != 1 {
		t.Fatalf("auditoría de sin_plan: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND tipo = 'contrato_sin_firma' AND clave = $2 AND datos->>'motivo' = 'sin_plan'", sup,
		"contrato_sin_firma:pendiente:"+clave11); n != 1 {
		t.Fatal("la campana no explica el cambio sin revisión")
	}
	_, err = e.cmp.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: orden, Version: version()})
	problema(t, err, 422, "contrato_por_firmar")
	quitarPendiente(clave11)

	// Sin cotización de la venta (aquí, la aceptada pasa por un momento a reemplazada): avisa con
	// sin_cotizacion_viva, auditado, una sola vez.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET estado = 'reemplazada', aceptada_en = NULL WHERE idempotencia = $1", "iris:venta:"+clave9); err != nil {
		t.Fatal(err)
	}
	clave13 := "iris:plan:" + ordenTxt + ":13"
	for i := range 2 {
		r13 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorPlan, PlanID: ptr(int64(301)), Clave: clave13}))(t)
		if r13.CotizacionID != nil || (i == 0) != (r13.Aviso != nil) {
			t.Fatalf("sin cotización viva (%d): %+v", i, r13)
		}
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND payload->>'clave' = $1 AND payload->>'motivo' = 'sin_cotizacion_viva'", clave13); n != 1 {
		t.Fatalf("sin_cotizacion_viva: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'prospectos.venta_revisada' AND resultado = 'ok' AND entrada->'auditoria'->>'aviso' = 'sin_cotizacion_viva'"); n != 1 {
		t.Fatalf("auditoría de sin_cotizacion_viva: %d", n)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET estado = 'aceptada', aceptada_en = now() WHERE idempotencia = $1", "iris:venta:"+clave9); err != nil {
		t.Fatal(err)
	}

	// El supervisor cierra con el cambio todavía en la cola de Argos: el aviso sale al cerrar, con
	// la clave del pendiente.
	clave12 := "iris:plan:" + ordenTxt + ":12"
	pendienteDe(clave12, "pendiente", `{"plan_id": 301}`)
	deber(e.cmp.CerrarOrden.Ejecutar(cs, campo.EntradaCerrarOrden{ID: orden, Version: version(), MotivoSinContratoFirmado: ptr("Argos sigue sin responder; firma luego")}))(t)
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND payload->>'clave' = $1
		AND payload->>'motivo' = 'cierre_sin_firma' AND payload->'contrato_id' = 'null'::jsonb AND payload->>'operacion' = 'cambiar_plan'`, clave12); n != 1 {
		t.Fatal("el cierre con el cambio en la cola no avisó")
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE usuario_id = $1 AND clave = $2", sup, "contrato_sin_firma:pendiente:"+clave12); n != 1 {
		t.Fatal("la campana no recibió el cierre con el cambio en la cola")
	}
	quitarPendiente(clave12)
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', finalizada_en = NULL, resultado = NULL WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}

	// Cambio de titular resuelto a mano con el cliente final indicado: el contrato va a su nombre
	// (el de la instantánea de Argos).
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
		VALUES (888, 1, '{1}', 'Titular Resuelto, S. A.', 'C-888', '5555-0000', 'Zona 1', now()) ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id = 888")
	})
	clave14 := "iris:titular:" + ordenTxt + ":14"
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.pendientes (operacion, idempotency_key, entidad, entidad_id, metodo, ruta,
		estado, resuelto_como, resuelto_cliente_id, resuelto_motivo, resuelto_en)
		VALUES ('cambiar_titular', $1, 'orden', $2, 'POST', '/servicios/1/cambiar-titular', 'resuelto_manual', 'manual', 888, 'Corregido en Argos', now())`, clave14, ordenTxt); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.outbox (id, evento, empresa_id, payload)
		VALUES (gen_random_uuid(), 'integracion.pendiente_resuelto_manual', 1, jsonb_build_object('pendiente_id', 1, 'operacion', 'cambiar_titular',
			'entidad', 'orden', 'entidad_id', $1::text, 'idempotency_key', $2::text, 'de', 'fallido_negocio', 'motivo', 'Corregido en Argos'))`, ordenTxt, clave14); err != nil {
		t.Fatal(err)
	}
	e.relay()
	pf = porFirmar()
	if pf == nil {
		t.Fatal("sin contrato por firmar del titular resuelto a mano")
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND contenido->'cliente'->>'nombre' = 'Titular Resuelto, S. A.'", pf.Contrato.ID); n != 1 {
		t.Fatal("el contrato no va a nombre del cliente que indicó el supervisor")
	}
	if res := firmar(ct, pf.Contrato.ID); res.Estado != "aplicada" {
		t.Fatalf("firma del titular resuelto a mano: %+v", res)
	}
	quitarPendiente(clave14)

	// B1: el cliente indicado no está en la instantánea (o es de otra empresa): el nombre es el del
	// titular tomado en la orden, nunca con el documento anterior.
	nombreOrden := "Marta Lucía Ortiz"
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.orden_titular SET nombre = $2 WHERE orden_id = $1", orden, nombreOrden); err != nil {
		t.Fatal(err)
	}
	clave15 := "iris:titular:" + ordenTxt + ":15"
	r15 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorTitular, Clave: clave15,
		ClienteArgosID: ptr(int64(999999))}))(t)
	if r15.ContratoID == nil {
		t.Fatalf("titular sin cliente en la instantánea: %+v", r15)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.contratos WHERE id = $1 AND contenido->'cliente'->>'nombre' = $2 AND contenido->'cliente'->>'documento' = ''", *r15.ContratoID, nombreOrden); n != 1 {
		t.Fatal("el contrato no va al titular de la orden, sin documento")
	}
	if res := firmar(ct, *r15.ContratoID); res.Estado != "aplicada" {
		t.Fatalf("firma del titular de la orden: %+v", res)
	}
	// Sin ningún nombre: la revisión sin contrato y aviso sin_titular.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.orden_titular SET nombre = NULL WHERE orden_id = $1", orden); err != nil {
		t.Fatal(err)
	}
	clave16 := "iris:titular:" + ordenTxt + ":16"
	r16 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorTitular, Clave: clave16}))(t)
	if r16.CotizacionID == nil || r16.ContratoID != nil {
		t.Fatalf("titular sin nombre: %+v", r16)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND payload->>'motivo' = 'sin_titular' AND (payload->>'cotizacion_id')::bigint = $1", *r16.CotizacionID); n != 1 {
		t.Fatal("sin aviso sin_titular")
	}
	// Seguimiento de #280: el aviso queda en el resultado y la auditoría, y el historial dice que
	// la revisión quedó sin contrato.
	if r16.Aviso == nil || *r16.Aviso != "sin_titular" {
		t.Fatalf("sin_titular sin aviso en el resultado: %+v", r16)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'prospectos.venta_revisada' AND resultado = 'ok' AND entrada->'auditoria'->>'aviso' = 'sin_titular'"); n != 1 {
		t.Fatalf("auditoría de sin_titular: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM comercial.prospecto_eventos WHERE prospecto_id = $1 AND tipo = 'venta_revisada' AND (datos->>'cotizacion_id')::bigint = $2 AND (datos->>'sin_contrato')::boolean",
		prospecto, *r16.CotizacionID); n != 1 {
		t.Fatal("el historial no dice que la revisión quedó sin contrato")
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET estado = 'reemplazada' WHERE id = $1", *r16.CotizacionID); err != nil {
		t.Fatal(err)
	}
	// Seguimiento de #280: con la orden cancelada, la revisión de un cambio de titular sin nombre se
	// crea sin contrato; al reabrir, OrdenReabierta no regenera un contrato sin titular: avisa
	// sin_titular, una sola vez aunque el evento llegue dos veces.
	deber(e.cmp.CancelarOrden.Ejecutar(cs, campo.EntradaMotivo{ID: orden, Version: version(), Motivo: "El cliente pidió otra fecha"}))(t)
	clave17 := "iris:titular:" + ordenTxt + ":17"
	r17 := deber(e.com.RevisarVenta(sistema(t), comercial.EntradaRevisarVenta{OrdenID: orden, Motivo: comercial.RevisionPorTitular, Clave: clave17}))(t)
	if r17.CotizacionID == nil || r17.ContratoID != nil {
		t.Fatalf("titular sin nombre con la orden cancelada: %+v", r17)
	}
	deber(e.cmp.ReabrirOrden.Ejecutar(cs, campo.EntradaMotivo{ID: orden, Version: version(), Motivo: "El cliente ya puede recibirnos"}))(t)
	e.relay()
	for range 2 {
		if rr := deber(e.com.OrdenReabierta(sistema(t), comercial.EntradaOrdenTerminada{OrdenID: orden, Estado: "pendiente", De: "cancelada"}))(t); rr.ContratoID != nil {
			t.Fatalf("contrato regenerado sin titular: %+v", rr)
		}
	}
	if pf := porFirmar(); pf != nil {
		t.Fatalf("por firmar sin titular: %+v", pf)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.contrato_sin_firma' AND payload->>'motivo' = 'sin_titular' AND (payload->>'cotizacion_id')::bigint = $1", *r17.CotizacionID); n != 1 {
		t.Fatalf("sin_titular al reabrir: %d (uno)", n)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.orden_titular SET nombre = $2 WHERE orden_id = $1", orden, nombreOrden); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE comercial.cotizaciones SET estado = 'reemplazada' WHERE id = $1", *r17.CotizacionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', en_sitio_en = now() WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}

	// orden.paso_titular_tras_cancelar llega a la campana de supervisión una sola vez (el relay
	// entrega el evento dos veces).
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.outbox (id, evento, empresa_id, payload)
		VALUES (gen_random_uuid(), 'orden.paso_titular_tras_cancelar', 1, $1)`,
		`{"orden_id":`+strconv.FormatInt(orden, 10)+`,"pendiente_id":987654,"operacion":"cambiar_plan","estado_orden":"cerrada"}`); err != nil {
		t.Fatal(err)
	}
	e.relay()
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.outbox SET publicado_en = NULL WHERE evento = 'orden.paso_titular_tras_cancelar' AND payload->>'pendiente_id' = '987654'"); err != nil {
		t.Fatal(err)
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM notificaciones.avisos WHERE tipo = 'paso_titular' AND clave = 'paso_titular:987654'"); n != 1 {
		t.Fatalf("avisos de paso_titular: %d (uno por supervisor, una vez)", n)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ordenes SET estado = 'en_sitio', finalizada_en = NULL, resultado = NULL WHERE id = $1", orden); err != nil {
		t.Fatal(err)
	}

	// MEDIA de la verificación de #280: el DPI de quien firma (/firmar y la firma en la orden) nunca
	// queda en claro en la auditoría.
	for _, consulta := range []string{
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE '%1234567890101%' OR coalesce(resultado::text, '') LIKE '%1234567890101%'",
		"SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') LIKE '%1234567890101%' OR coalesce(despues::text, '') LIKE '%1234567890101%'",
	} {
		if n := contar(t, consulta); n != 0 {
			t.Fatalf("DPI del firmante en la auditoría: %s", consulta)
		}
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.cambios WHERE entidad = 'contrato' AND campo = 'firmante_documento' AND despues = '\"***\"'::jsonb"); n == 0 {
		t.Fatal("el documento del firmante no quedó como *** en el diff")
	}

	// B17: el titular cambió en Argos → el prospecto pasa al cliente nuevo (idempotente).
	// MEDIA 2 de #280: solo si la instantánea dice que el servicio ya es del cliente nuevo.
	in := comercial.EntradaTitularCambiado{ServicioID: *d.Alta.ServicioArgosID, DeClienteID: d.Alta.ClienteArgosID, AClienteID: 777, EntranteID: 1}
	for _, c := range []int64{*d.Alta.ClienteArgosID, 777} {
		if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, empresas, nombre, codigo, telefono, direccion, sincronizado_en)
			VALUES ($1, 1, '{1}', 'Cliente', $2, '5555-0000', 'Zona 1', now()) ON CONFLICT (id) DO NOTHING`, c, "C-"+strconv.FormatInt(c, 10)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, plan_nombre, estado, sincronizado_en)
		VALUES ($1, 1, $2, 'Fibra 100', 'pendiente_instalacion', now()) ON CONFLICT (id) DO UPDATE SET cliente_id = $2`, *d.Alta.ServicioArgosID, *d.Alta.ClienteArgosID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.argos_servicios WHERE id = $1", *d.Alta.ServicioArgosID)
		_, _ = pruebas.Pool(t).Exec(context.Background(), "DELETE FROM integracion.argos_clientes WHERE id IN ($1, 777)", *d.Alta.ClienteArgosID)
	})
	if rm := deber(e.com.MoverTitular(sistema(t), in))(t); len(rm.Prospectos) != 0 {
		t.Fatalf("movido con la instantánea en otro titular: %+v", rm)
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.argos_servicios SET cliente_id = 777 WHERE id = $1", *d.Alta.ServicioArgosID); err != nil {
		t.Fatal(err)
	}
	if rm := deber(e.com.MoverTitular(sistema(t), in))(t); len(rm.Prospectos) != 1 {
		t.Fatalf("titular nuevo: %+v", rm)
	}
	if rm := deber(e.com.MoverTitular(sistema(t), in))(t); len(rm.Prospectos) != 0 {
		t.Fatalf("titular repetido: %+v", rm)
	}
	// Un aviso atrasado (de vuelta al anterior) no lo devuelve: la instantánea dice 777.
	atrasado := comercial.EntradaTitularCambiado{ServicioID: *d.Alta.ServicioArgosID, DeClienteID: ptr(int64(1)), AClienteID: *d.Alta.ClienteArgosID, EntranteID: 2}
	if rm := deber(e.com.MoverTitular(sistema(t), atrasado))(t); len(rm.Prospectos) != 0 {
		t.Fatalf("el aviso atrasado devolvió el prospecto: %+v", rm)
	}
	v = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	if v.Alta.ClienteArgosID == nil || *v.Alta.ClienteArgosID != 777 || v.Alta.Estado != comercial.AltaEsperandoActivacion {
		t.Fatalf("prospecto con el titular nuevo: %+v", v.Alta)
	}

	// B17: la activación hecha en Argos completa el alta (una vez).
	if ra := deber(e.com.ActivarAlta(sistema(t), orden))(t); len(ra.Prospectos) != 1 {
		t.Fatalf("activación: %+v", ra)
	}
	if ra := deber(e.com.ActivarAlta(sistema(t), orden))(t); len(ra.Prospectos) != 0 {
		t.Fatalf("activación repetida: %+v", ra)
	}
	v = deber(e.com.VerProspecto.Ejecutar(ctx, comercial.EntradaVerProspecto{ID: prospecto}))(t)
	if v.Alta.Estado != comercial.AltaCompleta {
		t.Fatalf("alta tras activar: %+v", v.Alta)
	}
	// Vuelve a esperar la activación, para el resto de la prueba.
	if _, err := pruebas.Pool(t).Exec(context.Background(),
		"UPDATE comercial.prospectos SET alta_estado = 'esperando_activacion', cliente_argos_id = $2 WHERE id = $1", prospecto, *d.Alta.ClienteArgosID); err != nil {
		t.Fatal(err)
	}
}

var flujoPDF = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)

// textoPDF: el contenido de los flujos del PDF (descomprimidos), para buscar texto.
func textoPDF(t *testing.T, pdf []byte) string {
	t.Helper()
	var b strings.Builder
	for _, m := range flujoPDF.FindAllSubmatch(pdf, -1) {
		r, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue
		}
		crudo, _ := io.ReadAll(r)
		b.Write(crudo)
	}
	return b.String()
}
