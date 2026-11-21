package campo_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// retiroEnSitio: una orden de retiro del técnico, en sitio, con servicio y unidades en comodato.
type retiroEnSitio struct {
	e        entorno
	cd, ct   context.Context
	tec      int64
	servicio int64
	unidades []int64
	orden    campo.Orden
}

func nuevoRetiroEnSitio(t *testing.T, unidades int, ajustes ...func(*campo.Config)) *retiroEnSitio {
	t.Helper()
	r := &retiroEnSitio{servicio: int64(960000) + time.Now().UnixNano()%10000}
	base := int64(8_000_000) + time.Now().UnixNano()%100000*10
	comodato := []campo.EquipoComodato{}
	for i := range unidades {
		u := base + int64(i)
		r.unidades = append(r.unidades, u)
		comodato = append(comodato, campo.EquipoComodato{UnidadID: u, Serie: "TPL-" + pruebas.Unico()})
	}
	inv := &inventarioFalso{comodato: map[int64][]campo.EquipoComodato{r.servicio: comodato}}
	r.e = nuevoEntorno(append([]func(*campo.Config){func(c *campo.Config) { c.Inventario = inv }}, ajustes...)...)
	d := despacho(t)
	r.tec = tecnico(t)
	r.cd, r.ct = como(t, d, "despacho"), como(t, r.tec, "tecnico")
	// Su posición de hace 5 minutos, en el sitio (la referencia de la ubicación del intento).
	posicion(t, r.tec, 14.6, -90.5, r.e.reloj.ahora().Add(-5*time.Minute), r.e.reloj.ahora().Add(-4*time.Minute))
	r.orden = r.otraOrden(t)
	return r
}

// intento: los datos de un intento en el sitio, con una foto nueva del técnico de esa orden.
func (r *retiroEnSitio) intento(t *testing.T, orden, unidad int64, motivo string) map[string]any {
	t.Helper()
	d := intentoEn(unidad, motivo)
	d["fotoAdjuntoId"] = fotoDe(t, orden, r.tec).String()
	return d
}

// otraOrden: otra orden de retiro del mismo técnico y servicio, en sitio.
func (r *retiroEnSitio) otraOrden(t *testing.T) campo.Orden {
	t.Helper()
	o := programada(t, r.e, r.cd, "retiro", r.tec)
	conServicio(t, o.ID, r.servicio)
	esperar(t, lote(t, r.e, r.ct, op(campo.OpLlegar, o.ID, nil)), "aplicada")
	return o
}

func intentoEn(unidad int64, motivo string) map[string]any {
	return map[string]any{"unidadId": unidad, "motivo": motivo, "lat": 14.6, "lon": -90.5, "precisionM": 12}
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// mensajeDe: el mensaje del primer error de campo del resultado.
func mensajeDe(r campo.RespuestaLote) string {
	if e := r.Resultados[0].Error; e != nil && len(e.Errors) > 0 {
		return e.Errors[0].Message
	}
	return ""
}

// A-271-1: el cambio y el intento los registra solo el técnico de la orden, ni en el lote ni por
// REST (un supervisor con ordenes.registrar no lo hace a su nombre).
func TestEquiposSoloElTecnicoDeLaOrden(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 1)
	sup := supervisor(t)
	res := lote(t, r.e, sup, op(campo.OpEquipoNoRecuperado, r.orden.ID, r.intento(t, r.orden.ID, r.unidades[0], "cliente_ausente")))
	if got := estados(res)[0]; got != "rechazada:prohibido" && got != "rechazada:no_encontrado" {
		t.Fatalf("lote de un supervisor: %s", got)
	}
	lat, lon, prec := 14.6, -90.5, 12.0
	_, err := r.e.m.NoRecuperadoPanel(sup, campo.EntradaNoRecuperado{ID: r.orden.ID, UnidadID: r.unidades[0], Motivo: "cliente_ausente",
		Lat: &lat, Lon: &lon, PrecisionM: &prec})
	prohibidoONoEncontrado(t, err)

	cambio := programada(t, r.e, r.cd, "cambio_equipo", r.tec)
	conServicio(t, cambio.ID, r.servicio)
	esperar(t, lote(t, r.e, r.ct, op(campo.OpLlegar, cambio.ID, nil)), "aplicada")
	datos := map[string]any{"saleUnidadId": r.unidades[0], "entra": map[string]any{"serie": "ZTEGC8A1B2C3"}, "motivo": "danado"}
	res = lote(t, r.e, sup, op(campo.OpEquipoCambiar, cambio.ID, datos))
	if got := estados(res)[0]; got != "rechazada:prohibido" && got != "rechazada:no_encontrado" {
		t.Fatalf("cambio en el lote de un supervisor: %s", got)
	}
	serie := "ZTEGC8A1B2C3"
	_, err = r.e.m.CambiarEquipoPanel(sup, campo.EntradaCambiarEquipo{ID: cambio.ID, SaleUnidadID: r.unidades[0],
		Entra: campo.EquipoQueEntra{Serie: &serie}, Motivo: "danado"})
	prohibidoONoEncontrado(t, err)
	if n := contar(t, `SELECT count(*) FROM campo.orden_equipos WHERE orden_id = ANY($1)`, []int64{r.orden.ID, cambio.ID}); n != 0 {
		t.Fatalf("nada registrado: %d", n)
	}
}

func prohibidoONoEncontrado(t *testing.T, err error) {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) || (p.Status != 403 && p.Status != 404) {
		t.Fatalf("403 o 404: %v", err)
	}
}

// A-271-2: el límite de un intento por unidad al día no se evade con la fecha del teléfono (cuenta
// también la hora en que lo recibió el servidor) ni con dos lotes a la vez.
func TestNoRecuperadoLimiteDiarioSinEvasion(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 2)
	otra := r.otraOrden(t)
	// La otra orden se asignó hace días: una fecha de ayer es válida para ella.
	exec(t, `UPDATE campo.ordenes SET asignada_en = asignada_en - interval '3 days' WHERE id = $1`, otra.ID)
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, r.orden.ID, r.intento(t, r.orden.ID, r.unidades[0], "cliente_ausente"))), "aplicada")
	// Ayer: con su llegada y su posición de ayer, para que solo cuente el límite.
	antes := r.e.reloj.ahora().Add(-24 * time.Hour) // la hora del servidor es la del reloj del módulo
	llegadaEn(t, otra.ID, r.tec, antes.Add(-10*time.Minute))
	posicion(t, r.tec, 14.6, -90.5, antes.Add(-5*time.Minute), antes.Add(-4*time.Minute))
	ayer := op(campo.OpEquipoNoRecuperado, otra.ID, r.intento(t, otra.ID, r.unidades[0], "cliente_ausente"))
	ayer.OcurridoEn = &antes
	res := lote(t, r.e, r.ct, ayer)
	esperar(t, res, "rechazada:demasiados_intentos")
	if res.Resultados[0].Error.Status != 429 {
		t.Fatalf("429: %+v", res.Resultados[0].Error)
	}

	// Dos lotes a la vez sobre la otra unidad: la unidad queda bloqueada mientras se cuenta, así que
	// solo uno se aplica. La prueba retiene el candado hasta que los dos esperan por él.
	unidad := r.unidades[1]
	tercera := r.otraOrden(t)
	pool := pruebas.Pool(t)
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(), `SELECT pg_advisory_xact_lock(hashtextextended('campo.no_recuperado:' || $1::bigint::text, 0))`, unidad); err != nil {
		t.Fatal(err)
	}
	type resultado struct {
		r   campo.RespuestaLote
		err error
	}
	listo := make(chan resultado, 2)
	for _, o := range []int64{otra.ID, tercera.ID} {
		operacion := op(campo.OpEquipoNoRecuperado, o, r.intento(t, o, unidad, "cliente_ausente"))
		go func() {
			res, err := r.e.m.Lote(r.ct, campo.EntradaLote{DispositivoID: "tel-" + pruebas.Unico(), Plataforma: "pwa",
				Operaciones: []campo.OperacionLote{operacion}})
			listo <- resultado{res, err}
		}()
	}
	limite := time.Now().Add(30 * time.Second)
	for {
		var n int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND wait_event = 'advisory' AND query LIKE '%pg_advisory_xact_lock%campo.no_recuperado%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= 2 {
			break
		}
		if time.Now().After(limite) {
			t.Fatalf("los dos lotes no llegaron al candado: %d", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	cuenta := map[string]int{}
	for range 2 {
		x := <-listo
		if x.err != nil {
			t.Fatal(x.err)
		}
		cuenta[estados(x.r)[0]]++
	}
	if cuenta["aplicada"] != 1 || cuenta["rechazada:demasiados_intentos"] != 1 {
		t.Fatalf("solo uno se aplica: %v", cuenta)
	}
}

// M-271-3: la foto del intento es del técnico, tomada en el sitio después de llegar, no rechazada y
// no usada en otro cambio o intento; al cerrar, debe estar subida (faltan_adjuntos).
func TestNoRecuperadoFotoValida(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 2)
	o := r.orden.ID
	con := func(unidad int64, foto string) campo.OperacionLote {
		d := intentoEn(unidad, "se_nego")
		d["fotoAdjuntoId"] = foto
		return op(campo.OpEquipoNoRecuperado, o, d)
	}
	rechazo := func(foto, quiero string) {
		t.Helper()
		res := lote(t, r.e, r.ct, con(r.unidades[0], foto))
		esperar(t, res, "rechazada:validacion")
		if m := mensajeDe(res); !strings.Contains(m, quiero) {
			t.Fatalf("motivo %q, esperaba %q", m, quiero)
		}
	}
	// De otro usuario (aunque sea de esta orden).
	rechazo(fotoDe(t, o, despacho(t)).String(), "tuya")
	// Tomada antes de llegar.
	vieja := fotoDe(t, o, r.tec)
	exec(t, `UPDATE nucleo.adjuntos SET tomado_en = (SELECT en_sitio_en FROM campo.ordenes WHERE id = $2) - interval '2 hours' WHERE id = $1`, vieja, o)
	rechazo(vieja.String(), "después de llegar")
	// Rechazada al subir.
	mala := fotoDe(t, o, r.tec)
	exec(t, `UPDATE nucleo.adjuntos SET estado = 'rechazado' WHERE id = $1`, mala)
	rechazo(mala.String(), "rechazó")
	// Ya usada en otro intento.
	buena := fotoDe(t, o, r.tec)
	esperar(t, lote(t, r.e, r.ct, con(r.unidades[0], buena.String())), "aplicada")
	res := lote(t, r.e, r.ct, con(r.unidades[1], buena.String()))
	esperar(t, res, "rechazada:validacion")
	if m := mensajeDe(res); !strings.Contains(m, "ya respalda") {
		t.Fatalf("foto ya usada: %q", m)
	}

	// Al cerrar, la foto del intento debe haber terminado de subir.
	var intento string
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT id::text FROM campo.orden_equipos WHERE orden_id = $1 AND foto_adjunto_id = $2`, o, buena).
		Scan(&intento); err != nil {
		t.Fatal(err)
	}
	evidencia := fotoDe(t, o, r.tec).String()
	esperar(t, lote(t, r.e, r.ct,
		op(campo.OpChecklistMarcar, o, map[string]any{"codigo": "equipos_recuperados", "valor": false}),
		op(campo.OpAdjuntoRegistrar, o, map[string]any{"adjuntoId": evidencia, "tipo": "foto"}),
		op(campo.OpChecklistMarcar, o, map[string]any{"codigo": "foto_equipos", "valor": evidencia}),
		op(campo.OpFirmaCapturar, o, map[string]any{"nombre": "Ana López", "svg": "<svg><path d='M0 0'/></svg>"}),
	), "aplicada", "aplicada", "aplicada", "aplicada")
	cerrar := op(campo.OpCerrar, o, map[string]any{"resultado": "parcial"})
	res = lote(t, r.e, r.ct, cerrar)
	if e := res.Resultados[0].Error; e != nil && e.Codigo() == "cierre_incompleto" {
		t.Fatalf("cierre incompleto: %+v", e.Errors)
	}
	esperar(t, res, "rechazada:faltan_adjuntos")
	if errs := res.Resultados[0].Error.Errors; len(errs) != 1 || errs[0].Field != "adjuntos.equipos."+intento+".foto" {
		t.Fatalf("falta la foto del intento: %+v", errs)
	}
	exec(t, `UPDATE nucleo.adjuntos SET estado = 'completo' WHERE id = $1`, buena)
	esperar(t, lote(t, r.e, r.ct, cerrar), "aplicada")
}

// M-271-4 (0.13.1): la ubicación del intento lleva precisión (200 m o mejor) y está a 500 m o menos
// del servicio y de la última posición del técnico de los 30 min anteriores a la fecha, recibida
// antes del lote. Sin coordenadas del servicio o sin esa posición: 422 ubicacion_sin_referencia.
func TestNoRecuperadoUbicacionEnSitio(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 1)
	o, u := r.orden.ID, r.unidades[0]
	ahora := r.e.reloj.ahora()
	intento := func(cambios map[string]any) campo.RespuestaLote {
		d := r.intento(t, o, u, "cliente_ausente")
		for k, v := range cambios {
			if v == nil {
				delete(d, k)
			} else {
				d[k] = v
			}
		}
		return lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, d))
	}
	esperar(t, intento(map[string]any{"precisionM": nil}), "rechazada:validacion")
	esperar(t, intento(map[string]any{"precisionM": 900}), "rechazada:validacion")
	// A unos 5 km del servicio.
	res := intento(map[string]any{"lat": 14.645})
	esperar(t, res, "rechazada:"+campo.CodigoUbicacionFueraDeSitio)
	if res.Resultados[0].Error.Status != 422 {
		t.Fatalf("422: %+v", res.Resultados[0].Error)
	}
	// En las coordenadas del servicio, pero el teléfono reportó hace 2 minutos estar a 5 km.
	posicion(t, r.tec, 14.645, -90.5, ahora.Add(-2*time.Minute), ahora.Add(-time.Minute))
	esperar(t, intento(nil), "rechazada:"+campo.CodigoUbicacionFueraDeSitio)
	exec(t, `DELETE FROM campo.posiciones WHERE tecnico_id = $1 AND lat > 14.64`, r.tec)
	// El servicio sin coordenadas: no se puede verificar.
	exec(t, `UPDATE campo.ordenes SET lat = NULL, lon = NULL WHERE id = $1`, o)
	esperar(t, intento(nil), "rechazada:"+campo.CodigoUbicacionSinReferencia)
	exec(t, `UPDATE campo.ordenes SET lat = 14.6, lon = -90.5 WHERE id = $1`, o)
	// Sin posición de los 30 min anteriores (la única es de hace 2 h).
	exec(t, `DELETE FROM campo.posiciones WHERE tecnico_id = $1`, r.tec)
	posicion(t, r.tec, 14.6, -90.5, ahora.Add(-2*time.Hour), ahora.Add(-2*time.Hour))
	esperar(t, intento(nil), "rechazada:"+campo.CodigoUbicacionSinReferencia)
	// Una posición recibida después del lote no cuenta (ni para bien ni para mal).
	posicion(t, r.tec, 14.6, -90.5, ahora.Add(-3*time.Minute), ahora.Add(time.Hour))
	esperar(t, intento(nil), "rechazada:"+campo.CodigoUbicacionSinReferencia)
	posicion(t, r.tec, 14.6, -90.5, ahora.Add(-10*time.Minute), ahora.Add(-9*time.Minute))
	posicion(t, r.tec, 14.645, -90.5, ahora.Add(-time.Minute), ahora.Add(time.Hour))
	esperar(t, intento(nil), "aplicada")
}

// M-271-3 (0.13.1): el cambio y el intento necesitan la llegada (orden.llegar) del mismo actor, el
// mismo día y antes de la fecha; si no, 422 sin_llegada.
func TestEquiposSinLlegada(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 1)
	o := r.orden.ID
	// La llegada fue ayer: no vale para hoy.
	exec(t, `UPDATE campo.orden_eventos SET ocurrido_en = ocurrido_en - interval '1 day' WHERE orden_id = $1 AND a = 'en_sitio'`, o)
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, r.intento(t, o, r.unidades[0], "cliente_ausente"))),
		"rechazada:"+campo.CodigoSinLlegada)
	// En un cambio, la llegada de otro (despacho la marcó) tampoco.
	cambio := programada(t, r.e, r.cd, "cambio_equipo", r.tec)
	conServicio(t, cambio.ID, r.servicio)
	esperar(t, lote(t, r.e, r.ct, op(campo.OpLlegar, cambio.ID, nil)), "aplicada")
	exec(t, `UPDATE campo.orden_eventos SET actor_id = NULL WHERE orden_id = $1 AND a = 'en_sitio'`, cambio.ID)
	datos := map[string]any{"saleUnidadId": r.unidades[0], "entra": map[string]any{"serie": "ZTEGC8A1B2C3"}, "motivo": "danado"}
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoCambiar, cambio.ID, datos)), "rechazada:"+campo.CodigoSinLlegada)
}

// 0.13.1 §3.10 (orden_reasignada): quien tenía la orden en la fecha declarada envía su intento
// hasta 12 h después de que se la quitaran (hora del servidor), registrado a su nombre; después, o
// con una fecha en que ya no era suya, 409 orden_reasignada.
func TestNoRecuperadoOrdenReasignada(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 3)
	o := r.orden.ID
	t0 := r.e.reloj.ahora()
	// Registró dos intentos sin señal 5 minutos después de llegar; a los 10 se la pasan a otro.
	fecha := t0.Add(5 * time.Minute)
	pendiente := func(unidad int64, cuando time.Time) campo.OperacionLote {
		x := op(campo.OpEquipoNoRecuperado, o, r.intento(t, o, unidad, "cliente_ausente"))
		x.OcurridoEn = &cuando
		return x
	}
	primero, segundo := pendiente(r.unidades[0], fecha), pendiente(r.unidades[1], fecha)
	tarde := pendiente(r.unidades[2], t0.Add(15*time.Minute)) // ya no era suya
	r.e.reloj.avanzar(10 * time.Minute)
	// Reasignada a otro técnico (una orden en sitio no se reasigna desde el panel: se simula la
	// asignación con su fila de historial, como la escribe ordenes.asignar).
	otro := tecnico(t)
	exec(t, `UPDATE campo.ordenes SET tecnico_id = $2, asignada_en = $3 WHERE id = $1`, o, otro, r.e.reloj.ahora())
	exec(t, `INSERT INTO campo.orden_eventos (orden_id, tipo, datos, actor_tipo, ocurrido_en)
		VALUES ($1, 'asignada', jsonb_build_object('tecnico_id', $2::bigint, 'tecnico_anterior', $3::bigint), 'sistema', $4)`,
		o, otro, r.tec, r.e.reloj.ahora())

	r.e.reloj.avanzar(2 * time.Hour)
	esperar(t, lote(t, r.e, r.ct, primero), "aplicada")
	if n := contar(t, `SELECT count(*) FROM campo.orden_equipos WHERE orden_id = $1 AND registrado_por = $2`, o, r.tec); n != 1 {
		t.Fatalf("registrado a nombre de quien lo hizo: %d", n)
	}
	esperar(t, lote(t, r.e, r.ct, tarde), "rechazada:orden_reasignada")
	r.e.reloj.avanzar(11 * time.Hour) // 13 h después de quitársela
	esperar(t, lote(t, r.e, r.ct, segundo), "rechazada:orden_reasignada")
}

// llegadaEn: una llegada (orden.llegar) del técnico a la orden en ese momento.
func llegadaEn(t *testing.T, orden, tec int64, cuando time.Time) {
	t.Helper()
	exec(t, `INSERT INTO campo.orden_eventos (orden_id, tipo, de, a, actor_tipo, actor_id, ocurrido_en)
		VALUES ($1, 'estado', 'en_camino', 'en_sitio', 'usuario', $2, $3)`, orden, tec, cuando)
}

// equiposFalsos hace de pendientes (EquiposArgos): dice si Argos es ≥ 0.14.0 y guarda lo encolado.
type equiposFalsos struct {
	catorce bool
	cuerpos []map[string]any
}

func (f *equiposFalsos) ClavesPorOperacion(context.Context, *auditoria.Tx) (bool, error) {
	return f.catorce, nil
}

func (f *equiposFalsos) EncolarAlAplicar(_ context.Context, _ *auditoria.Tx, orden []byte) (bool, error) {
	var m map[string]any
	if err := json.Unmarshal(orden, &m); err != nil {
		return false, err
	}
	f.cuerpos = append(f.cuerpos, m)
	return f.catorce, nil
}

// 0.14.0 §3.10: con un Argos ≥ 0.14.0 el intento se encola al aplicarse (con el id de la operación
// y quien lo registró) y queda marcado para que el cierre solo lo resuma.
func TestIntentoSeEncolaAlAplicarse014(t *testing.T) {
	f := &equiposFalsos{catorce: true}
	r := nuevoRetiroEnSitio(t, 2, func(c *campo.Config) { c.Equipos = f })
	o := r.orden.ID
	x := op(campo.OpEquipoNoRecuperado, o, r.intento(t, o, r.unidades[0], "cliente_ausente"))
	esperar(t, lote(t, r.e, r.ct, x), "aplicada")
	if len(f.cuerpos) != 1 {
		t.Fatalf("encolado al aplicar: %d", len(f.cuerpos))
	}
	eq := f.cuerpos[0]["equipos"].([]any)[0].(map[string]any)
	if eq["id"] != x.ID || eq["accion"] != "no_recuperado" || eq["usuario_id"] != float64(r.tec) || eq["foto_ref"] == nil {
		t.Fatalf("equipo encolado: %+v", eq)
	}
	if n := contar(t, `SELECT count(*) FROM campo.orden_equipos WHERE id = $1 AND enviado_al_aplicar`, x.ID); n != 1 {
		t.Fatal("marcado como enviado al aplicar")
	}
	// Con 0.14.0 una orden puede llevar otro intento de la misma unidad (otro día).
	r.e.reloj.avanzar(24 * time.Hour)
	posicion(t, r.tec, 14.6, -90.5, r.e.reloj.ahora().Add(-5*time.Minute), r.e.reloj.ahora().Add(-4*time.Minute))
	llegadaEn(t, o, r.tec, r.e.reloj.ahora().Add(-10*time.Minute))
	otro := r.intento(t, o, r.unidades[0], "cliente_ausente")
	exec(t, `UPDATE nucleo.adjuntos SET tomado_en = $2 WHERE id = $1`, otro["fotoAdjuntoId"], r.e.reloj.ahora())
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, otro)), "aplicada")
}

// 0.14.0 §4.5: con un Argos < 0.14.0, un solo intento por unidad y orden (intento_repetido).
func TestIntentoRepetidoConArgosAnterior(t *testing.T) {
	f := &equiposFalsos{}
	r := nuevoRetiroEnSitio(t, 1, func(c *campo.Config) { c.Equipos = f })
	o := r.orden.ID
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, r.intento(t, o, r.unidades[0], "cliente_ausente"))), "aplicada")
	r.e.reloj.avanzar(24 * time.Hour)
	posicion(t, r.tec, 14.6, -90.5, r.e.reloj.ahora().Add(-5*time.Minute), r.e.reloj.ahora().Add(-4*time.Minute))
	llegadaEn(t, o, r.tec, r.e.reloj.ahora().Add(-10*time.Minute))
	otro := r.intento(t, o, r.unidades[0], "cliente_ausente")
	exec(t, `UPDATE nucleo.adjuntos SET tomado_en = $2 WHERE id = $1`, otro["fotoAdjuntoId"], r.e.reloj.ahora())
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, otro)), "rechazada:"+campo.CodigoIntentoRepetido)
	if n := contar(t, `SELECT count(*) FROM campo.orden_equipos WHERE orden_id = $1 AND enviado_al_aplicar`, o); n != 0 {
		t.Fatal("con un Argos anterior nada sale al aplicarse")
	}
}

// 0.14.0 §3.10 (R-07, T-7): la foto lleva tomadoEn (no posterior al lote) y un cambio con cobro
// propuesto exige foto.
func TestFotoConTomadoEnYCobroConFoto(t *testing.T) {
	r := nuevoRetiroEnSitio(t, 1)
	o := r.orden.ID
	sinHora := r.intento(t, o, r.unidades[0], "cliente_ausente")
	exec(t, `UPDATE nucleo.adjuntos SET tomado_en = NULL WHERE id = $1`, sinHora["fotoAdjuntoId"])
	res := lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, sinHora))
	esperar(t, res, "rechazada:validacion")
	if m := mensajeDe(res); !strings.Contains(m, "tomadoEn") {
		t.Fatalf("sin hora de captura: %q", m)
	}
	futura := r.intento(t, o, r.unidades[0], "cliente_ausente")
	exec(t, `UPDATE nucleo.adjuntos SET tomado_en = $2 WHERE id = $1`, futura["fotoAdjuntoId"], r.e.reloj.ahora().Add(time.Hour))
	esperar(t, lote(t, r.e, r.ct, op(campo.OpEquipoNoRecuperado, o, futura)), "rechazada:validacion")

	cambio := programada(t, r.e, r.cd, "cambio_equipo", r.tec)
	conServicio(t, cambio.ID, r.servicio)
	esperar(t, lote(t, r.e, r.ct, op(campo.OpLlegar, cambio.ID, nil)), "aplicada")
	res = lote(t, r.e, r.ct, op(campo.OpEquipoCambiar, cambio.ID, map[string]any{"saleUnidadId": r.unidades[0],
		"entra": map[string]any{"serie": "ZTEGC8A1B2C3"}, "motivo": "danado", "cobroPropuesto": map[string]any{"motivo": "golpe"}}))
	esperar(t, res, "rechazada:validacion")
	if res.Resultados[0].Error.Errors[0].Field != "fotoAdjuntoId" {
		t.Fatalf("cobro sin foto: %+v", res.Resultados[0].Error)
	}
}
