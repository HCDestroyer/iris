package comercial_test

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/comercial"
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
