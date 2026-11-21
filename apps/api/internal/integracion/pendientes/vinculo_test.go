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
	return pendientes.Nuevo(pendientes.Config{Argos: am, Reloj: func() time.Time { return e.reloj }})
}

// vinculado: un tecnico.argos_vinculado del técnico hacia el conector, con ese estado.
func (e *entorno) vinculado(usuario int64, version int, estado string) uuid.UUID {
	e.t.Helper()
	id := uuid.Must(uuid.NewV7())
	crudo, _ := json.Marshal(map[string]any{"usuario_id": usuario, "version": version})
	e.exec(`INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, conector_origen)
		VALUES ($1, 'tecnico.argos_vinculado', 1, $2, now(), $3, $4)`, id, crudo, estado, e.conID)
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

// Contrato 0.14.0 §6.6: con Iris y Argos en 0.14.0, una operación que lleva `tecnico` no sale antes
// del 2xx del último tecnico.argos_vinculado de su técnico hacia ese conector (depende_de): espera
// sin gastar intentos. Una sin `tecnico` sale; con Iris informando una versión anterior, tampoco espera.
func TestOperacionConTecnicoEsperaSuVinculado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	tec := e.tecnico("u-81")
	conTecnico := func(n string) pendientes.Registro {
		return e.encolar(pendientes.Accion{Operacion: "consumir_material", IdempotencyKey: "iris:prueba:vinculo:" + n + ":" + uuid.NewString(),
			Ruta: "/servicios/59/consumos", TecnicoUsuarioID: &tec,
			Cuerpo: cuerpo(t, map[string]any{"lineas": []map[string]any{{"producto_id": 1, "cantidad": 1, "tecnico": "u-81"}}})})
	}

	// Iris informa 0.8.1 (la de esta versión): sale sin mirar el vínculo.
	antes := conTecnico("antes")
	e.procesar()
	if estado, _, _ := e.estadoPendiente(antes.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("sin 0.14.0 informado no espera: %s", estado)
	}

	m := e.con014()
	procesar := func() {
		t.Helper()
		if _, err := m.Procesar(e.ctx()); err != nil {
			t.Fatal(err)
		}
	}
	op := conTecnico("espera")
	sinTecnico := e.encolar(pendientes.Accion{Operacion: "devolver_equipo", IdempotencyKey: "iris:prueba:vinculo:sin:" + uuid.NewString(),
		Ruta: "/servicios/59/equipos/1/devolver", TecnicoUsuarioID: &tec, Cuerpo: cuerpo(t, map[string]any{"motivo": "retiro"})})
	procesar()
	if estado, codigo, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado || intentos != 0 {
		t.Fatalf("sin vinculado espera: %s %s %d", estado, codigo, intentos)
	}
	if estado, _, _ := e.estadoPendiente(sinTecnico.ID); estado != pendientes.EstadoHecho {
		t.Fatalf("sin tecnico sale: %s", estado)
	}

	// Vinculado aún pendiente: sigue esperando, también al vencer la espera.
	w := e.vinculado(tec, 1, "pendiente")
	e.reloj = e.reloj.Add(2 * pendientes.EsperaVinculo)
	procesar()
	if estado, codigo, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado || intentos != 0 {
		t.Fatalf("vinculado sin 2xx: %s %s %d", estado, codigo, intentos)
	}
	// Con el 2xx, sale.
	e.exec(`UPDATE mensajeria.webhooks_salida SET estado = 'entregado' WHERE id = $1`, w)
	e.reloj = e.reloj.Add(2 * pendientes.EsperaVinculo)
	procesar()
	if estado, _, intentos := e.estadoPendiente(op.ID); estado != pendientes.EstadoHecho || intentos != 1 {
		t.Fatalf("con el 2xx del vinculado: %s %d", estado, intentos)
	}

	// Un vinculado más nuevo (otro id_externo) sin 2xx: la siguiente operación vuelve a esperar.
	e.vinculado(tec, 2, "pendiente")
	otra := conTecnico("nuevo")
	procesar()
	if estado, codigo, _ := e.estadoPendiente(otra.ID); estado != pendientes.EstadoPendiente || codigo != pendientes.CodigoDependeDeVinculado {
		t.Fatalf("el último vinculado manda: %s %s", estado, codigo)
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
