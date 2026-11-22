package pendientes_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// con014: la cola con un conector que informa contrato 0.14.0 (Config.ContratoIris).
func (e *entorno) con014() *pendientes.Modulo {
	e.t.Helper()
	cfg := e.acfg
	cfg.ContratoIris = "0.14.0"
	am := argos.Nuevo(cfg)
	am.Nucleo().FijarTokens(pruebas.TokensFalsos{})
	// La carga inicial de vínculos del conector, ya cerrada (si no, depende_de espera).
	e.exec(`UPDATE integracion.conectores SET vinculos_carga_id = $2, vinculos_carga_n = 0, vinculos_cargados_en = now() WHERE id = $1`,
		e.conID, uuid.Must(uuid.NewV7()))
	return pendientes.Nuevo(pendientes.Config{Argos: am, Reloj: func() time.Time { return e.reloj }})
}

// vinculado: un tecnico.argos_* del técnico en la empresa 1 (en el outbox, ya publicado) y su
// webhook hacia el conector con ese estado ("" = aún sin programar).
func (e *entorno) vinculado(evento string, usuario int64, idExterno string, version int, estado string) uuid.UUID {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7())
	var ficha int64
	if err := e.pool.QueryRow(context.Background(), `SELECT id FROM agenda.tecnicos WHERE usuario_id = $1`, usuario).Scan(&ficha); err != nil {
		e.t.Fatal(err)
	}
	crudo, _ := json.Marshal(map[string]any{"tecnico_id": ficha, "usuario_id": usuario, "empresa_id": 1, "version": version, "id_externo": idExterno})
	e.exec(`INSERT INTO integracion.outbox (id, evento, empresa_id, payload, publicado_en) VALUES ($1, $2, 1, $3, now())`, id, evento, crudo)
	if estado != "" {
		e.exec(`INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, conector_origen)
			VALUES ($1, $2, 1, $3, now(), $4, $5)`, id, evento, crudo, estado, e.conID)
	}
	return id
}

func (e *entorno) estadoPendiente(id int64) (estado, codigo string, intentos int) {
	e.t.Helper()
	if err := e.pool.QueryRow(context.Background(), `SELECT estado, coalesce(codigo_error, ''), intentos FROM integracion.pendientes WHERE id = $1`,
		id).Scan(&estado, &codigo, &intentos); err != nil {
		e.t.Fatal(err)
	}
	return estado, codigo, intentos
}

func (e *entorno) opConTecnico(tec int64, n string) pendientes.Registro {
	e.t.Helper()
	return e.encolar(pendientes.Accion{Operacion: "consumir_material", IdempotencyKey: "iris:prueba:vinculo:" + n + ":" + uuid.NewString(),
		Ruta: "/servicios/59/consumos", TecnicoUsuarioID: &tec,
		Cuerpo: cuerpo(e.t, map[string]any{"lineas": []map[string]any{{"producto_id": 1, "cantidad": 1, "tecnico": "x"}}})})
}

func procesarCon(t *testing.T, e *entorno, m *pendientes.Modulo) {
	t.Helper()
	if _, err := m.Procesar(e.ctx()); err != nil {
		t.Fatal(err)
	}
}

// Contrato 0.14.0 §6.6: con Iris y Argos en 0.14.0, una operación que lleva `tecnico` no sale antes
// del 2xx del tecnico.argos_vinculado VIGENTE de su técnico (el último emitido para esa empresa, con
// el id_externo de la ficha; M-3) hacia ese conector: espera sin gastar intentos. Una sin `tecnico`
// sale; con Iris informando una versión anterior, tampoco espera.
func TestOperacionConTecnicoEsperaSuVinculado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	ext := "u-81-" + uuid.NewString()[:8]
	tec := e.tecnico(ext)

	// Iris informa 0.8.1 (la de esta versión): sale sin mirar el vínculo.
	antes := e.opConTecnico(tec, "antes")
	e.procesar()
	if estado, _, _ := e.estadoPendiente(antes.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("sin 0.14.0 informado no espera: %s", estado)
	}

	m := e.con014()
	op := e.opConTecnico(tec, "espera")
	sinTecnico := e.encolar(pendientes.Accion{Operacion: "devolver_equipo", IdempotencyKey: "iris:prueba:vinculo:sin:" + uuid.NewString(),
		Ruta: "/servicios/59/equipos/1/devolver", TecnicoUsuarioID: &tec, Cuerpo: cuerpo(t, map[string]any{"motivo": "retiro"})})
	procesarCon(t, e, m)
	if estado, codigo, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado || intentos != 0 {
		t.Fatalf("sin vinculado espera: %s %s %d", estado, codigo, intentos)
	}
	if estado, _, _ := e.estadoPendiente(sinTecnico.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("sin tecnico sale: %s", estado)
	}

	// Vinculado en el outbox (sin programar) y luego pendiente: sigue esperando.
	w := e.vinculado("tecnico.argos_vinculado", tec, ext, 1, "pendiente")
	e.reloj = e.reloj.Add(2 * pendientes.EsperaVinculo)
	procesarCon(t, e, m)
	if estado, codigo, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado || intentos != 0 {
		t.Fatalf("vinculado sin 2xx: %s %s %d", estado, codigo, intentos)
	}
	// Con el 2xx, sale.
	e.exec(`UPDATE mensajeria.webhooks_salida SET estado = 'entregado' WHERE id = $1`, w)
	e.reloj = e.reloj.Add(2 * pendientes.EsperaVinculo)
	procesarCon(t, e, m)
	if estado, _, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoHecho || intentos != 1 {
		t.Fatalf("con el 2xx del vinculado: %s %d", estado, intentos)
	}

	// M-3: la ficha cambió de id_externo y su evento aún no se emitió: la fila entregada (del id
	// viejo) no cuenta; espera el vinculado del vigente.
	nuevo := ext + "-b"
	e.exec(`UPDATE agenda.tecnicos SET argos_id_externo = $2 WHERE usuario_id = $1`, tec, nuevo)
	otra := e.opConTecnico(tec, "vigente")
	procesarCon(t, e, m)
	if estado, codigo, _ := e.estadoPendiente(otra.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado {
		t.Fatalf("la versión vigente manda: %s %s", estado, codigo)
	}
	e.vinculado("tecnico.argos_desvinculado", tec, ext, 2, "entregado")
	w2 := e.vinculado("tecnico.argos_vinculado", tec, nuevo, 3, "entregado")
	e.reloj = e.reloj.Add(2 * pendientes.EsperaVinculo)
	procesarCon(t, e, m)
	if estado, _, _ := e.estadoPendiente(otra.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("con el vinculado vigente entregado: %s", estado)
	}

	// A-1: reconectar la misma instancia de Argos: el vínculo pasa al conector nuevo, pero lo
	// entregado no se hereda: hasta cerrar la carga nueva (que reemite el vinculado con versión mayor),
	// la operación espera; después sale.
	pendienteViejo := e.vinculado("tecnico.argos_vinculado", tec, nuevo, 4, "pendiente")
	c, err := e.am.Conectar.Ejecutar(e.ctx(), argos.EntradaConectar{Enlace: e.argos.srv.URL + "/conectar#codigo-de-reconexion-456"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.am.ActivarConector.Ejecutar(e.ctx(), argos.EntradaActivarConector{ID: c.ID, Version: c.Version}); err != nil {
		t.Fatal(err)
	}
	e.exec(`UPDATE integracion.conectores SET contrato_remoto = '0.14.0', version_remota = (SELECT version_remota FROM integracion.conectores WHERE id = $2) WHERE id = $1`, c.ID, e.conID)
	if e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND conector_origen = $2`, w2, e.conID) != 1 ||
		e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND conector_origen = $2`, pendienteViejo, c.ID) != 1 {
		t.Fatal("solo el webhook sin entregar pasa al conector nuevo")
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_carga_id IS NULL AND vinculos_cargados_en IS NULL`, c.ID) != 1 {
		t.Fatal("la carga cerrada no se hereda")
	}
	tras := e.opConTecnico(tec, "reconexion")
	procesarCon(t, e, m)
	if estado, codigo, _ := e.estadoPendiente(tras.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado {
		t.Fatalf("con la carga nueva abierta espera: %s %s", estado, codigo)
	}
	// La carga nueva reemite el vinculado (versión mayor) hacia el conector nuevo y se cierra.
	e.exec(`UPDATE mensajeria.webhooks_salida SET estado = 'entregado' WHERE id = $1`, pendienteViejo)
	e.exec(`UPDATE integracion.conectores SET vinculos_carga_id = $2, vinculos_carga_n = 1, vinculos_cargados_en = now() WHERE id = $1`, c.ID, uuid.Must(uuid.NewV7()))
	e.reloj = e.reloj.Add(2 * pendientes.EsperaVinculo)
	procesarCon(t, e, m)
	if estado, _, _ := e.estadoPendiente(tras.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("con la carga cerrada y el vinculado entregado sale: %s", estado)
	}
}

// depende_de con salida: agotado → bloqueado a la vista (integracion.pendiente_bloqueado) hasta el
// 2xx; 24 h esperando → bloqueado; omitido (empresa fuera del alcance) → fallido_negocio con
// empresa_no_permitida.
func TestVinculoAgotadoTopeYOmitido(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	m := e.con014()
	bloqueados := func(id int64) int {
		return e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'pendiente_id')::bigint = $2`, pendientes.EventoBloqueado, id)
	}

	// Agotado: bloqueado ya; al reenviarse y entregarse, vuelve a la cola y sale.
	ext := "u-82-" + uuid.NewString()[:8]
	tec := e.tecnico(ext)
	w := e.vinculado("tecnico.argos_vinculado", tec, ext, 1, "agotado")
	op := e.opConTecnico(tec, "agotado")
	procesarCon(t, e, m)
	if estado, codigo, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoBloqueado || codigo != pendientes.CodigoDependeDeVinculado || intentos != 0 || bloqueados(op.ID) != 1 {
		t.Fatalf("agotado: %s %s %d %d", estado, codigo, intentos, bloqueados(op.ID))
	}
	e.exec(`UPDATE mensajeria.webhooks_salida SET estado = 'entregado' WHERE id = $1`, w)
	// Con el conector apagado el estado del vínculo no se puede saber: sigue bloqueado.
	e.exec(`UPDATE integracion.conectores SET estado = 'apagado' WHERE id = $1`, e.conID)
	procesarCon(t, e, m)
	e.exec(`UPDATE integracion.conectores SET estado = 'activo' WHERE id = $1`, e.conID)
	if estado, _, _ := e.estadoPendiente(op.ID); estado != pendientes.EstadoBloqueado {
		t.Fatalf("sin conector no se desbloquea: %s", estado)
	}
	procesarCon(t, e, m)
	if estado, _, _ := e.estadoPendiente(op.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("con el 2xx se desbloquea y sale: %s", estado)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND (payload->>'pendiente_id')::bigint = $2`, pendientes.EventoDesbloqueado, op.ID); n != 1 {
		t.Fatalf("desbloqueado: %d", n)
	}

	// Tope: 24 h esperando un vinculado sin 2xx → bloqueado.
	ext2 := "u-83-" + uuid.NewString()[:8]
	tec2 := e.tecnico(ext2)
	e.vinculado("tecnico.argos_vinculado", tec2, ext2, 1, "pendiente")
	larga := e.opConTecnico(tec2, "tope")
	procesarCon(t, e, m)
	if estado, _, _ := e.estadoPendiente(larga.ID); estado != pendientes.EstadoPendiente {
		t.Fatalf("antes del tope espera: %s", estado)
	}
	e.reloj = e.reloj.Add(pendientes.TopeEsperaVinculo + time.Minute)
	procesarCon(t, e, m)
	if estado, codigo, _ := e.estadoPendiente(larga.ID); estado != pendientes.EstadoBloqueado || codigo != pendientes.CodigoDependeDeVinculado || bloqueados(larga.ID) != 1 {
		t.Fatalf("al tope, bloqueado: %s %s", estado, codigo)
	}

	// Omitido: fallido de negocio con empresa_no_permitida (para una persona).
	ext3 := "u-84-" + uuid.NewString()[:8]
	tec3 := e.tecnico(ext3)
	wo := e.vinculado("tecnico.argos_vinculado", tec3, ext3, 1, "omitido")
	e.exec(`UPDATE mensajeria.webhooks_salida SET ultimo_error = 'empresa_no_permitida: la empresa no está en el alcance del conector Argos' WHERE id = $1`, wo)
	om := e.opConTecnico(tec3, "omitido")
	procesarCon(t, e, m)
	if estado, codigo, _ := e.estadoPendiente(om.ID); estado != pendientes.EstadoFallidoNegocio || codigo != pendientes.CodigoEmpresaNoPermitida {
		t.Fatalf("omitido: %s %s", estado, codigo)
	}

	// Omitido porque el conector no informó su receptor: sin_receptor (no empresa_no_permitida).
	ext4 := "u-85-" + uuid.NewString()[:8]
	tec4 := e.tecnico(ext4)
	wr := e.vinculado("tecnico.argos_vinculado", tec4, ext4, 1, "omitido")
	e.exec(`UPDATE mensajeria.webhooks_salida SET ultimo_error = 'sin_receptor: el conector Argos no habla el contrato 0.14.0 o no informó su receptor de webhooks' WHERE id = $1`, wr)
	sr := e.opConTecnico(tec4, "sin_receptor")
	procesarCon(t, e, m)
	if estado, codigo, _ := e.estadoPendiente(sr.ID); estado != pendientes.EstadoFallidoNegocio || codigo != pendientes.CodigoSinReceptor {
		t.Fatalf("sin receptor: %s %s", estado, codigo)
	}
}

// Argos responde 422 idempotency_key_reutilizada: el pendiente queda para una persona y emite
// integracion.pendiente_clave_reutilizada (seguridad en un cambio o un intento), sin la clave.
func TestClaveReutilizadaAvisa(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	ruta := "/servicios/59/consumos/reutilizada"
	e.argos.responder(ruta, 422)
	e.argos.tipos[ruta] = "urn:argosisp:problema:idempotency_key_reutilizada"
	clave := "iris:prueba:reutilizada:" + uuid.NewString()
	p := e.encolar(pendientes.Accion{Operacion: "consumir_material", IdempotencyKey: clave, Ruta: ruta, Cuerpo: []byte(`{"x":1}`)})
	e.procesar()
	if estado, codigo, _ := e.estadoPendiente(p.ID); estado != pendientes.EstadoFallidoNegocio || codigo != pendientes.CodigoIdempotenciaReutilizada {
		t.Fatalf("fallido de negocio: %s %s", estado, codigo)
	}
	var crudo []byte
	if err := e.pool.QueryRow(context.Background(), `SELECT payload FROM integracion.outbox WHERE evento = $1
		AND (payload->>'pendiente_id')::bigint = $2`, pendientes.EventoClaveReutilizada, p.ID).Scan(&crudo); err != nil {
		t.Fatalf("evento: %v", err)
	}
	var ev map[string]any
	_ = json.Unmarshal(crudo, &ev)
	if ev["operacion"] != "consumir_material" || ev["seguridad"] != false || ev["entidad"] != "prueba" {
		t.Fatalf("payload: %v", ev)
	}
	for k, v := range ev {
		if s, ok := v.(string); ok && s == clave {
			t.Fatalf("la clave no viaja en el evento (%s)", k)
		}
	}
}
