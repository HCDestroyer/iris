package argos_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato 0.12.0 §2.3 (B27): inventario.unidad_estado_cambiado e inventario.equipo_cambiado llenan el
// estado y el historial de cada unidad (sin costos); un webhook más viejo no pisa el estado; uno
// repetido no duplica; fuera del alcance, con payload inválido o con un conector anterior a la
// 0.12.0, se ignoran.
func TestWebhooksEquiposDelCliente(t *testing.T) {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.argos_unidades_historial", "DELETE FROM integracion.argos_unidades"} {
			if _, err := e.pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.12.0', contrato_informado = '0.12.0', alcance_global = false,
		empresas = ARRAY[1]::bigint[] WHERE id = $1`, id)
	// enviarEn: el webhook con su ocurrido_en, procesado.
	enviarEn := func(evento string, payload map[string]any, en time.Time) argos.ResultadoEntrante {
		t.Helper()
		ent := e.entranteDe(e.enviar(id, secreto, "wh-eq-"+pruebas.Unico(), evento, payload, time.Now()))
		e.exec(`UPDATE integracion.entrantes SET ocurrido_en = $2 WHERE id = $1`, ent, en)
		r, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent)
		if err != nil {
			t.Fatalf("%s: %v", evento, err)
		}
		return r
	}
	ahora := time.Now().UTC().Truncate(time.Second)
	estado := func(unidad int64) (string, *int64) {
		t.Helper()
		var est *string
		var servicio *int64
		if err := e.pool.QueryRow(ctx, `SELECT estado, servicio_id FROM integracion.argos_unidades WHERE unidad_id = $1`, unidad).Scan(&est, &servicio); err != nil {
			t.Fatal(err)
		}
		if est == nil {
			return "", servicio
		}
		return *est, servicio
	}

	// Cambio de equipo desde una orden de Iris: la que entra queda en el servicio, la que sale sin él;
	// historial de las dos, con la orden; el evento interno sin costos.
	cambio := map[string]any{"servicio_id": 4410, "cliente_id": 990, "empresa_id": 1, "sale_unidad_id": 7001, "entra_unidad_id": 7002,
		"motivo": "danado", "destino_sale": "revision", "origen": "iris_orden", "origen_id": "555", "costo": "350.00", "proveedor": "X"}
	if r := enviarEn(argos.EventoEquipoCambiado, cambio, ahora); r.Resultado != "aplicado" {
		t.Fatalf("equipo_cambiado: %+v", r)
	}
	if _, s := estado(7002); s == nil || *s != 4410 {
		t.Fatalf("la que entra queda en el servicio: %v", s)
	}
	if _, s := estado(7001); s != nil {
		t.Fatalf("la que sale queda sin servicio: %v", *s)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'entra_unidad_id')::bigint = 7002
		AND payload->'origen_id' = 'null'::jsonb AND NOT payload ? 'costo' AND NOT payload ? 'proveedor'`, argos.EventoEquipoCambiadoInterno); n != 1 {
		t.Fatalf("argos.equipo_cambiado: %d", n)
	}

	// Estados: la que sale pasa a revisión; uno más viejo llega después y no la pisa (sí queda en el
	// historial); el mismo webhook repetido no duplica.
	revision := map[string]any{"unidad_id": 7001, "empresa_id": 1, "servicio_id": nil, "de": "en_comodato", "a": "revision", "motivo": "cambio_equipo"}
	if r := enviarEn(argos.EventoUnidadEstadoCambiado, revision, ahora.Add(time.Minute)); r.Resultado != "aplicado" {
		t.Fatalf("unidad_estado_cambiado: %+v", r)
	}
	viejo := map[string]any{"unidad_id": 7001, "empresa_id": 1, "servicio_id": 4410, "de": "disponible", "a": "en_comodato", "motivo": "comodato"}
	enviarEn(argos.EventoUnidadEstadoCambiado, viejo, ahora.Add(-time.Hour))
	if est, s := estado(7001); est != "revision" || s != nil {
		t.Fatalf("un aviso más viejo no pisa el estado ni el servicio: %s %v", est, s)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades_historial WHERE unidad_id = 7001`); n != 3 {
		t.Fatalf("historial de la que sale (sale, revisión y el viejo): %d", n)
	}
	// El mismo entrante procesado otra vez no cambia nada.
	ent := e.entranteDe(e.enviar(id, secreto, "wh-eq-rep-"+pruebas.Unico(), argos.EventoUnidadEstadoCambiado,
		map[string]any{"unidad_id": 7002, "empresa_id": 1, "servicio_id": 4410, "de": "disponible", "a": "en_comodato"}, time.Now()))
	for range 2 {
		if _, err := e.mod.ProcesarEntrante.Ejecutar(ctx, ent); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades_historial WHERE unidad_id = 7002 AND tipo = 'estado'`); n != 1 {
		t.Fatalf("webhook repetido: %d", n)
	}

	// Lectura para la orden: las del servicio y las de la orden, del más nuevo al más viejo por llegada a
	// Iris, solo los renglones del servicio de la orden (el estado «revision» llegó sin servicio).
	us, err := argos.EstadosDeUnidades(ctx, e.pool, []int64{1}, 4410, 0, []int64{7001})
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 2 || us[0].UnidadID != 7001 || *us[0].Estado != "revision" || us[0].ServicioID != nil || len(us[0].Historial) != 2 ||
		us[0].Historial[0].Tipo != "estado" || *us[0].Historial[0].A != "en_comodato" || us[0].Historial[1].Tipo != "sale" ||
		us[0].Historial[1].OrdenID != nil || *us[0].Historial[1].DestinoSale != "revision" ||
		us[1].UnidadID != 7002 || *us[1].Estado != "en_comodato" || us[1].ServicioID == nil || *us[1].ServicioID != 4410 {
		t.Fatalf("estados de la orden: %+v", us)
	}
	if otra, _ := argos.EstadosDeUnidades(ctx, e.pool, []int64{2}, 4410, 0, []int64{7001}); len(otra) != 0 {
		t.Fatalf("otra empresa no ve las unidades: %+v", otra)
	}

	// Ignorados: empresa fuera del alcance, estado desconocido, cambio sin la que entra y un conector
	// anterior a la 0.12.0.
	for _, c := range []struct {
		evento, omitido string
		payload         map[string]any
	}{
		{argos.EventoUnidadEstadoCambiado, "empresa_fuera_de_alcance", map[string]any{"unidad_id": 7003, "empresa_id": 2, "a": "revision"}},
		{argos.EventoUnidadEstadoCambiado, "payload_invalido", map[string]any{"unidad_id": 7003, "empresa_id": 1, "a": "vendida"}},
		{argos.EventoEquipoCambiado, "payload_invalido", map[string]any{"servicio_id": 4410, "empresa_id": 1, "sale_unidad_id": 7001}},
	} {
		if r := enviarEn(c.evento, c.payload, ahora); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != c.omitido {
			t.Fatalf("%s %v: %+v %s", c.evento, c.payload, r, e.omitidoDe(r.EntranteID))
		}
	}
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.11.0' WHERE id = $1`, id)
	if r := enviarEn(argos.EventoUnidadEstadoCambiado, revision, ahora.Add(2*time.Minute)); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "contrato_anterior" {
		t.Fatalf("contrato anterior: %+v", r)
	}
	// Vale el contrato acordado (#299): Argos dice 0.14.3, pero lo que Argos aceptó de Iris es 0.11.0.
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.14.3', contrato_informado = '0.11.0' WHERE id = $1`, id)
	if r := enviarEn(argos.EventoUnidadEstadoCambiado, revision, ahora.Add(3*time.Minute)); r.Resultado != "ignorado" || e.omitidoDe(r.EntranteID) != "contrato_anterior" {
		t.Fatalf("contrato acordado anterior a la 0.12.0: %+v", r)
	}
	e.exec(`UPDATE integracion.conectores SET contrato_informado = '0.12.0' WHERE id = $1`, id)
	if r := enviarEn(argos.EventoUnidadEstadoCambiado, revision, ahora.Add(4*time.Minute)); r.Resultado != "aplicado" {
		t.Fatalf("contrato acordado 0.12.0: %+v", r)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades WHERE unidad_id = 7003`); n != 0 {
		t.Fatalf("lo ignorado no se guarda: %d", n)
	}
}

// entornoEquipos: conector con contrato 0.12.0 en la empresa 1 y un envío de webhooks de equipos con
// o sin ocurrido_en (cadena RFC 3339 tal cual).
type entornoEquipos struct {
	*entorno
	id      int64
	secreto string
}

func nuevoEntornoEquipos(t *testing.T) *entornoEquipos {
	e := nuevoEntorno(t)
	_, id, secreto := e.conectado()
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM integracion.argos_unidades_historial", "DELETE FROM integracion.argos_unidades"} {
			if _, err := e.pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.12.0', contrato_informado = '0.12.0', alcance_global = false,
		empresas = ARRAY[1, 2]::bigint[] WHERE id = $1`, id)
	return &entornoEquipos{entorno: e, id: id, secreto: secreto}
}

func (e *entornoEquipos) mandar(evento string, payload map[string]any, ocurrido string) argos.ResultadoEntrante {
	e.t.Helper()
	wid := "wh-eqh-" + pruebas.Unico()
	cuerpo := map[string]any{"id": wid, "evento": evento, "payload": payload}
	if ocurrido != "" {
		cuerpo["ocurrido_en"] = ocurrido
	}
	crudo, _ := json.Marshal(cuerpo)
	ent := e.entranteDe(e.firmado(e.id, e.secreto, wid, crudo, time.Now()))
	r, err := e.mod.ProcesarEntrante.Ejecutar(pruebas.ContextoSistema(e.pool), ent)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

// unidad: estado, estado_en y servicio_en de la instantánea.
func (e *entornoEquipos) unidad(id int64) (string, *time.Time, *time.Time) {
	e.t.Helper()
	var est string
	var en, sen *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT coalesce(estado, ''), estado_en, servicio_en FROM integracion.argos_unidades
		WHERE unidad_id = $1`, id).Scan(&est, &en, &sen); err != nil {
		e.t.Fatal(err)
	}
	return est, en, sen
}

// MEDIA 1 de #301: una hora futura de Argos no congela la unidad. horaDeArgos la descarta: no se guarda
// ni ordena, y el aviso siguiente con la hora real se aplica.
func TestEquiposHoraFutura(t *testing.T) {
	e := nuevoEntornoEquipos(t)
	ahora := time.Now().UTC().Truncate(time.Second)
	estado := func(a string) map[string]any {
		return map[string]any{"unidad_id": 7101, "empresa_id": 1, "servicio_id": 4501, "a": a}
	}
	if r := e.mandar(argos.EventoUnidadEstadoCambiado, estado("revision"), ahora.Add(24*time.Hour).Format(time.RFC3339)); r.Resultado != "aplicado" {
		t.Fatalf("hora futura: %+v", r)
	}
	if est, en, sen := e.unidad(7101); est != "revision" || en != nil || sen != nil {
		t.Fatalf("la hora futura no se guarda: %s %v %v", est, en, sen)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades_historial WHERE unidad_id = 7101 AND ocurrido_en IS NULL`); n != 1 {
		t.Fatalf("el historial no guarda la hora futura: %d", n)
	}
	if r := e.mandar(argos.EventoUnidadEstadoCambiado, estado("disponible"), ahora.Format(time.RFC3339)); r.Resultado != "aplicado" {
		t.Fatalf("hora real: %+v", r)
	}
	if est, en, _ := e.unidad(7101); est != "disponible" || en == nil || !en.Equal(ahora) {
		t.Fatalf("la unidad quedó congelada por la hora futura: %s %v", est, en)
	}
	// Lo mismo en un cambio de equipo: la hora futura no congela el servicio de la unidad.
	cambio := func(servicio, sale, entra int64) map[string]any {
		return map[string]any{"servicio_id": servicio, "empresa_id": 1, "sale_unidad_id": sale, "entra_unidad_id": entra}
	}
	e.mandar(argos.EventoEquipoCambiado, cambio(4501, 7102, 7103), ahora.Add(24*time.Hour).Format(time.RFC3339))
	e.mandar(argos.EventoEquipoCambiado, cambio(4502, 7103, 7104), ahora.Format(time.RFC3339))
	var servicio *int64
	if err := e.pool.QueryRow(context.Background(), `SELECT servicio_id FROM integracion.argos_unidades WHERE unidad_id = 7103`).Scan(&servicio); err != nil {
		t.Fatal(err)
	}
	if servicio != nil {
		t.Fatalf("la que sale con la hora real quedó en el servicio de la hora futura: %v", *servicio)
	}
}

// MEDIA 1 de #301: un aviso sin ocurrido_en no mezcla la hora de Iris con la de Argos: no deja hora,
// no pisa una hora de Argos y no impide que el siguiente aviso con hora de Argos se aplique.
func TestEquiposSinOcurridoEn(t *testing.T) {
	e := nuevoEntornoEquipos(t)
	t0 := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	estado := func(unidad int64, a string) map[string]any {
		return map[string]any{"unidad_id": unidad, "empresa_id": 1, "a": a}
	}
	// Primero sin hora: se guarda el estado sin hora.
	e.mandar(argos.EventoUnidadEstadoCambiado, estado(7201, "en_comodato"), "")
	if est, en, _ := e.unidad(7201); est != "en_comodato" || en != nil {
		t.Fatalf("sin hora no deja la de Iris: %s %v", est, en)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.argos_unidades_historial WHERE unidad_id = 7201 AND ocurrido_en IS NULL
		AND recibido_en IS NOT NULL`); n != 1 {
		t.Fatalf("historial sin hora: %d", n)
	}
	// Con hora de Argos (aunque anterior a la llegada del de arriba): se aplica.
	e.mandar(argos.EventoUnidadEstadoCambiado, estado(7201, "revision"), t0.Format(time.RFC3339))
	if est, en, _ := e.unidad(7201); est != "revision" || en == nil || !en.Equal(t0) {
		t.Fatalf("con hora de Argos: %s %v", est, en)
	}
	// Otra vez sin hora: no pisa la hora (ni el estado) que vino de Argos.
	e.mandar(argos.EventoUnidadEstadoCambiado, estado(7201, "garantia"), "")
	if est, en, _ := e.unidad(7201); est != "revision" || en == nil || !en.Equal(t0) {
		t.Fatalf("sin hora pisó la de Argos: %s %v", est, en)
	}
	// Y el siguiente de Argos, diez minutos después de t0 (antes de la llegada a Iris del de arriba), se aplica.
	e.mandar(argos.EventoUnidadEstadoCambiado, estado(7201, "disponible"), t0.Add(10*time.Minute).Format(time.RFC3339))
	if est, en, _ := e.unidad(7201); est != "disponible" || en == nil || !en.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("la hora de Iris bloqueó un aviso de Argos: %s %v", est, en)
	}
	// Cambio de equipo sin hora después de uno con hora: no pisa el servicio ni la hora.
	cambio := func(servicio int64) map[string]any {
		return map[string]any{"servicio_id": servicio, "empresa_id": 1, "sale_unidad_id": 7202, "entra_unidad_id": 7203}
	}
	e.mandar(argos.EventoEquipoCambiado, cambio(4601), t0.Format(time.RFC3339))
	e.mandar(argos.EventoEquipoCambiado, cambio(4602), "")
	var servicio *int64
	var sen *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT servicio_id, servicio_en FROM integracion.argos_unidades WHERE unidad_id = 7203`).Scan(&servicio, &sen); err != nil {
		t.Fatal(err)
	}
	if servicio == nil || *servicio != 4601 || sen == nil || !sen.Equal(t0) {
		t.Fatalf("un cambio sin hora pisó el de Argos: %v %v", servicio, sen)
	}
}

// BAJA 4 de #301: origen_id solo se guarda si es el id numérico de una orden de Iris de la misma
// empresa; si no, null (en el historial y en el evento).
func TestEquiposOrigenIDValidado(t *testing.T) {
	e := nuevoEntornoEquipos(t)
	orden := func(empresa int64) int64 {
		var id int64
		if err := e.pool.QueryRow(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion, empresa_id, cliente_id, servicio_id)
			SELECT id, 'Zona 1', $1, 990, 4701 FROM campo.tipos_orden WHERE codigo = 'visita' RETURNING id`, empresa).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { e.exec(`DELETE FROM campo.ordenes WHERE id = $1`, id) })
		return id
	}
	propia, ajena := orden(1), orden(2)
	ahora := time.Now().UTC().Truncate(time.Second)
	for i, c := range []struct {
		origenID string
		valido   bool
	}{
		{strconv.FormatInt(propia, 10), true},
		{strconv.FormatInt(ajena, 10), false},        // orden de otra empresa
		{"999999999", false},                         // no es una orden de Iris
		{"<script>alert(1)</script>", false},         // texto libre
		{" " + strconv.FormatInt(propia, 10), false}, // no es exactamente el id
		{"0" + strconv.FormatInt(propia, 10), false}, // ceros a la izquierda
		{"-" + strconv.FormatInt(propia, 10), false}, // negativo
		{"99999999999999999999999999", false},        // fuera de rango
	} {
		sale, entra := int64(7301+2*i), int64(7302+2*i)
		r := e.mandar(argos.EventoEquipoCambiado, map[string]any{"servicio_id": 4701, "empresa_id": 1, "sale_unidad_id": sale,
			"entra_unidad_id": entra, "origen": "iris_orden", "origen_id": c.origenID}, ahora.Format(time.RFC3339))
		if r.Resultado != "aplicado" {
			t.Fatalf("%q: %+v", c.origenID, r)
		}
		var guardado *int64
		if err := e.pool.QueryRow(context.Background(), `SELECT origen_id FROM integracion.argos_unidades_historial
			WHERE unidad_id = $1 AND tipo = 'sale'`, sale).Scan(&guardado); err != nil {
			t.Fatal(err)
		}
		var evento json.RawMessage
		if err := e.pool.QueryRow(context.Background(), `SELECT payload->'origen_id' FROM integracion.outbox WHERE evento = $1
			AND (payload->>'sale_unidad_id')::bigint = $2`, argos.EventoEquipoCambiadoInterno, sale).Scan(&evento); err != nil {
			t.Fatal(err)
		}
		if c.valido {
			if guardado == nil || *guardado != propia || string(evento) != strconv.FormatInt(propia, 10) {
				t.Fatalf("%q: la orden propia debía guardarse: %v %s", c.origenID, guardado, evento)
			}
			continue
		}
		if guardado != nil || string(evento) != "null" {
			t.Fatalf("%q: debía quedar null: %v %s", c.origenID, guardado, evento)
		}
	}
	// En la lectura, ordenId solo aparece para esa orden.
	us, err := argos.EstadosDeUnidades(pruebas.ContextoSistema(e.pool), e.pool, []int64{1}, 4701, propia, []int64{7301})
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 || us[0].UnidadID != 7301 || len(us[0].Historial) != 1 || us[0].Historial[0].OrdenID == nil || *us[0].Historial[0].OrdenID != propia {
		t.Fatalf("ordenId de esta orden: %+v", us)
	}
	us, _ = argos.EstadosDeUnidades(pruebas.ContextoSistema(e.pool), e.pool, []int64{1}, 4701, propia+1, []int64{7301})
	if len(us) == 0 || us[0].Historial[0].OrdenID != nil {
		t.Fatalf("ordenId de otra orden debía ir en null: %+v", us)
	}
}
