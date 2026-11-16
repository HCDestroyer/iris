package mensajeria_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de seguridad de la #159 (fase 3a): un caso por hallazgo.

// ajustesDe guarda unos ajustes apagados para la empresa de p (así se siembran sus categorías).
func (x *bandejaIA) ajustesDe(p persona) {
	x.t.Helper()
	r := x.api(p, http.MethodGet, "/asistente/ajustes", nil)
	x.exigir(r, http.StatusOK)
	x.exigir(x.api(p, http.MethodPost, "/asistente/ajustes/actualizar", map[string]any{"version": r.cuerpo["version"], "activo": false,
		"proveedor": "ollama", "url": "", "modelo": "", "modo": "boton", "fuentes": map[string]any{"manuales": true, "argos": true, "horus": true, "historial": true},
		"topeTokensMes": 2000000, "precioEntradaMillon": nil, "precioSalidaMillon": nil}), http.StatusOK)
}

// clasificar manda un mensaje del cliente y corre el job con lo que «dice» el modelo.
func (x *bandejaIA) clasificar(num, wamid, texto, categoria string, confianza float64) map[string]any {
	x.t.Helper()
	x.ia.decir(fmt.Sprintf(`{"categoria": %q, "confianza": %v}`, categoria, confianza))
	x.texto(num, wamid, texto)
	*x.reloj = x.reloj.Add(21 * time.Second)
	c := x.conv(num)
	if _, err := x.m.SugerirAutomatico(x.ctx, idDe(c)); err != nil {
		x.t.Fatal(err)
	}
	return x.conv(num)
}

func (x *bandejaIA) categoriaConCola(nombre string, cola int64, sla any) map[string]any {
	x.t.Helper()
	k := x.categorias(x.admin)[nombre]
	r := x.api(x.admin, http.MethodPost, fmt.Sprintf("/asistente/categorias/%d/actualizar", idDe(k)),
		camposCategoria(k, map[string]any{"colaId": cola, "slaPrimeraRespuestaMin": sla}))
	x.exigir(r, http.StatusOK)
	return r.cuerpo
}

func colaDe(c map[string]any) int64 {
	if c["cola"] == nil {
		return 0
	}
	return idDe(c["cola"].(map[string]any))
}

// 1 y 10: la categoría no cruza de empresa (préstamo de números) y la base lo impide.
func TestAsistente3CategoriaNoCruzaDeEmpresa(t *testing.T) {
	x := nuevaBandejaIA(t)
	sup2 := x.persona([]int64{2}, rolBase("supervisor"))
	admin2 := x.persona([]int64{2}, rolBase("admin"))
	admin2.empresa = 2
	x.ajustesDe(admin2)
	r := x.api(sup2, http.MethodPost, "/colas", map[string]any{"nombre": "Norte " + pruebas.Unico()})
	x.exigir(r, http.StatusCreated)
	x.exigir(x.api(x.supervisor, http.MethodPost, fmt.Sprintf("/canales/%d/prestamos", x.canal.ID), map[string]any{"empresaId": 2,
		"usos": []string{"soporte"}, "colaId": idDe(r.cuerpo)}), http.StatusOK)
	x.activar(map[string]any{"modo": "boton"})
	num := numeroCliente()
	c := x.clasificar(num, "wamid.s1", "No tengo internet", "Sin internet", 0.9)
	if c["categoria"] == nil || c["actividadAsistente"] == nil {
		t.Fatalf("sin clasificar: %v", c)
	}
	conv := cid(c)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+conv+"/pasar-empresa", map[string]any{"version": ver(c), "empresaId": 2}), http.StatusOK)
	d := x.api(sup2, http.MethodGet, "/conversaciones/"+conv, nil)
	x.exigir(d, http.StatusOK, "categoriaOrigen", "<nil>", "categoriaConfianza", "<nil>")
	if d.cuerpo["categoria"] != nil || d.cuerpo["actividadAsistente"] != nil {
		t.Fatalf("la categoría de la otra empresa sobrevivió: %v", d.cuerpo)
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND categoria_id IS NULL AND categoria_en IS NULL
		AND clasificado_mensaje_id IS NULL AND asistente_actividad_tipo IS NULL`, conv); n != 1 {
		t.Fatal("quedaron datos de la categoría anterior")
	}
	if k := x.api(sup2, http.MethodGet, "/conversaciones/contadores", nil); len(k.cuerpo["porCategoria"].([]any)) != 0 {
		t.Fatalf("contadores de la otra empresa: %v", k.cuerpo["porCategoria"])
	}
	// La base no acepta una categoría de otra empresa.
	otra := idDe(x.categorias(x.admin)["Otro"])
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET categoria_id = $1 WHERE id = $2`, otra, conv); err == nil ||
		!strings.Contains(err.Error(), "no es de la empresa") {
		t.Fatalf("la base aceptó una categoría de otra empresa: %v", err)
	}
}

// 2 y 3: sin ping-pong de colas, sin pisar a una persona y sin meterse con el bot.
func TestAsistente3TransfiereConCuidado(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(map[string]any{"modo": "boton"})
	ventas, cobros := x.otraCola(), x.otraCola()
	x.categoriaConCola("Planes y precios", ventas, nil)
	x.categoriaConCola("Pagos y saldo", cobros, nil)
	// Confianza media: clasifica, pero no mueve la cola.
	num := numeroCliente()
	c := x.clasificar(num, "wamid.t1", "¿Qué planes hay?", "Planes y precios", 0.6)
	if c["categoria"].(map[string]any)["nombre"] != "Planes y precios" || colaDe(c) != x.cola {
		t.Fatalf("con confianza media no se transfiere: %v", c["cola"])
	}
	// Confianza alta en otra categoría: una transferencia.
	if c = x.clasificar(num, "wamid.t2", "¿Cuánto debo?", "Pagos y saldo", 0.95); colaDe(c) != cobros {
		t.Fatalf("no transfirió: %v", c["cola"])
	}
	// El cliente alterna de tema: la categoría cambia, la cola ya no (una vez por periodo abierto).
	if c = x.clasificar(num, "wamid.t3", "¿Y los planes?", "Planes y precios", 0.95); colaDe(c) != cobros ||
		c["categoria"].(map[string]any)["nombre"] != "Planes y precios" {
		t.Fatalf("ping-pong de colas: %v %v", c["cola"], c["categoria"])
	}
	if tr := x.outbox("conversacion.transferida", cid(c)); len(tr) != 1 {
		t.Fatalf("transferencias: %d", len(tr))
	}
	// Una persona la transfirió: el asistente no la mueve.
	otro := numeroCliente()
	x.texto(otro, "wamid.t4", "Hola")
	c = x.conv(otro)
	x.exigir(x.api(x.supervisor, http.MethodPost, "/conversaciones/"+cid(c)+"/transferir", map[string]any{"version": ver(c), "colaId": ventas,
		"resumen": "Es de ventas"}), http.StatusOK)
	if c = x.clasificar(otro, "wamid.t5", "¿Cuánto debo?", "Pagos y saldo", 0.99); colaDe(c) != ventas {
		t.Fatalf("pisó la transferencia de una persona: %v", c["cola"])
	}
	// El bot atiende (menú pendiente): clasifica, no transfiere y no toca el menú ni los temporizadores.
	tercero := numeroCliente()
	x.texto(tercero, "wamid.t6", "Hola")
	c = x.conv(tercero)
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.conversaciones SET bot_menu = 'esperando', bot_atendio_en = now(), bot_espera_en = now() WHERE id = $1`, idDe(c)); err != nil {
		t.Fatal(err)
	}
	x.ia.decir(`{"categoria": "Pagos y saldo", "confianza": 0.99}`)
	*x.reloj = x.reloj.Add(21 * time.Second)
	if _, err := x.m.SugerirAutomatico(x.ctx, idDe(c)); err != nil {
		t.Fatal(err)
	}
	if c = x.conv(tercero); c["categoria"].(map[string]any)["nombre"] != "Pagos y saldo" || colaDe(c) != x.cola {
		t.Fatalf("con el bot atendiendo: %v %v", c["categoria"], c["cola"])
	}
	if n := x.contar(`SELECT count(*) FROM mensajeria.conversaciones WHERE id = $1 AND bot_menu = 'esperando' AND bot_atendio_en IS NOT NULL AND bot_espera_en IS NOT NULL`, idDe(c)); n != 1 {
		t.Fatal("el asistente tocó el estado del bot")
	}
}

// 4: «por vencer» con una categoría sin SLA usa el margen de la cola; lista y contadores coinciden.
func TestAsistente3PorVencerSinSLADeCategoria(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(map[string]any{"modo": "boton"})
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.colas SET sla_respuesta_min = 60, sla_por_vencer_min = NULL WHERE id = $1`, x.cola); err != nil {
		t.Fatal(err)
	}
	num := numeroCliente()
	x.clasificar(num, "wamid.v1", "Me mudo", "Mudanza o traslado", 0.9)
	// A los 50 min faltan 10 para vencer: dentro del margen de la cola (15 min), no del de 1 min.
	*x.reloj = x.reloj.Add(50*time.Minute - 21*time.Second)
	c := x.conv(num)
	if c["sla"].(map[string]any)["estado"] != "por_vencer" {
		t.Fatalf("detalle: %v", c["sla"])
	}
	l := x.api(x.supervisor, http.MethodGet, "/conversaciones?sla=por_vencer", nil)
	x.exigir(l, http.StatusOK)
	k := x.api(x.supervisor, http.MethodGet, "/conversaciones/contadores", nil)
	x.exigir(k, http.StatusOK, "porVencer", fmt.Sprint(len(l.cuerpo["data"].([]any))))
	esta := false
	for _, d := range l.cuerpo["data"].([]any) {
		esta = esta || idDe(d.(map[string]any)) == idDe(c)
	}
	if !esta {
		t.Fatalf("la lista no la muestra por vencer: %v", l.cuerpo)
	}
}

// 5: costo de clasificar: máximo por empresa y hora, y se detiene al 80 % del tope.
func TestAsistente3LimitesDeClasificar(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(map[string]any{"modo": "boton", "topeTokensMes": 1000})
	// 300 clasificaciones aparte de la empresa en la última hora: la siguiente no llama al modelo.
	num := numeroCliente()
	x.texto(num, "wamid.l1", "Hola")
	c := x.conv(num)
	if _, err := x.bd.Exec(x.ctx, `INSERT INTO mensajeria.asistente_clasificaciones (id, empresa_id, conversacion_id, creada_en)
		SELECT gen_random_uuid(), 1, $1, $2 FROM generate_series(1, 300)`, idDe(c), x.reloj.Add(-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	otroNum := numeroCliente()
	x.texto(otroNum, "wamid.l1b", "Hola")
	*x.reloj = x.reloj.Add(21 * time.Second)
	if _, err := x.m.SugerirAutomatico(x.ctx, idDe(x.conv(otroNum))); err != nil || x.ia.pedidos() != 0 {
		t.Fatalf("límite por empresa y hora: %d %v", x.ia.pedidos(), err)
	}
	// Pasada la hora, clasifica (150 tokens de 1000).
	*x.reloj = x.reloj.Add(time.Hour)
	c = x.clasificar(numeroCliente(), "wamid.l2", "¿Planes?", "Planes y precios", 0.9)
	if c["categoria"] == nil || x.ia.pedidos() != 1 {
		t.Fatalf("debía clasificar: %d", x.ia.pedidos())
	}
	// Al 80 % del tope se deja de clasificar, aunque el asistente no esté en pausa.
	if _, err := x.bd.Exec(x.ctx, `UPDATE mensajeria.asistente_uso SET tokens_entrada = 800 WHERE empresa_id = 1`); err != nil {
		t.Fatal(err)
	}
	if c = x.clasificar(numeroCliente(), "wamid.l3", "¿Planes?", "Planes y precios", 0.9); c["categoria"] != nil || x.ia.pedidos() != 1 {
		t.Fatalf("al 80 %% no se clasifica: %d %v", x.ia.pedidos(), c["categoria"])
	}
	x.exigir(x.api(x.agente, http.MethodGet, "/asistente/estado", nil), http.StatusOK, "pausado", "false")
}

// 6: corregir la categoría no transfiere si quien corrige no puede transferir.
func TestAsistente3CategorizarSinPermisoDeTransferir(t *testing.T) {
	x := nuevaBandejaIA(t)
	ventas := x.otraCola()
	k := x.categoriaConCola("Planes y precios", ventas, nil)
	despacho := x.persona([]int64{1}, rolBase("despacho"))
	x.miembro(despacho)
	num := numeroCliente()
	x.texto(num, "wamid.p1", "Hola")
	c := x.conv(num)
	r := x.api(despacho, http.MethodPost, "/conversaciones/"+cid(c)+"/categorizar", map[string]any{"categoriaId": idDe(k), "version": ver(c)})
	x.exigir(r, http.StatusOK, "categoriaOrigen", "agente")
	if colaDe(r.cuerpo) != x.cola || len(x.outbox("conversacion.transferida", cid(c))) != 0 {
		t.Fatalf("transfirió sin conversaciones.transferir: %v", r.cuerpo["cola"])
	}
	// Con el permiso sí se mueve.
	otro := numeroCliente()
	x.texto(otro, "wamid.p2", "Hola")
	c = x.conv(otro)
	r = x.api(x.supervisor, http.MethodPost, "/conversaciones/"+cid(c)+"/categorizar", map[string]any{"categoriaId": idDe(k), "version": ver(c)})
	if x.exigir(r, http.StatusOK); colaDe(r.cuerpo) != ventas {
		t.Fatalf("con permiso debía transferir: %v", r.cuerpo["cola"])
	}
}

// 7: sin confianza o fuera de rango cae en «Otro».
func TestAsistente3ConfianzaAusenteOFueraDeRango(t *testing.T) {
	x := nuevaBandejaIA(t)
	x.activar(map[string]any{"modo": "boton"})
	for i, salida := range []string{`{"categoria": "Planes y precios"}`, `{"categoria": "Planes y precios", "confianza": 7}`,
		`{"categoria": "Planes y precios", "confianza": -1}`} {
		x.ia.decir(salida)
		num := numeroCliente()
		x.texto(num, fmt.Sprintf("wamid.f%d", i), "¿Planes?")
		*x.reloj = x.reloj.Add(21 * time.Second)
		if _, err := x.m.SugerirAutomatico(x.ctx, idDe(x.conv(num))); err != nil {
			t.Fatal(err)
		}
		if c := x.conv(num); c["categoria"].(map[string]any)["nombre"] != "Otro" || c["categoriaConfianza"] != nil {
			t.Fatalf("%s debía caer en Otro: %v", salida, c["categoria"])
		}
	}
	if pedido, _, _ := x.ia.ultimo(); !strings.Contains(pedido, "lo de \\u003ccategorias\\u003e lo configuró la empresa") &&
		!strings.Contains(pedido, "lo de <categorias> lo configuró la empresa") {
		t.Fatalf("las categorías no van declaradas como datos: %s", pedido)
	}
}

// 8: probar-mensaje no devuelve ni emite un id de categoría de otra empresa.
func TestAsistente3ProbarMensajeIdDeOtraEmpresa(t *testing.T) {
	x := nuevaBandejaIA(t)
	admin2 := x.persona([]int64{2}, rolBase("admin"))
	admin2.empresa = 2
	x.ajustesDe(admin2)
	ajena := idDe(x.categorias(admin2)["Otro"])
	x.ia.decir(`{"categoria": "Planes", "confianza": 0.9, "texto": "Hola, soy el asistente virtual.", "fuentes": [], "acciones": [], "pasar": null}`)
	r := x.api(x.admin, http.MethodPost, "/asistente/probar-mensaje", map[string]any{"texto": "hola", "ajustes": map[string]any{
		"categorias": []any{map[string]any{"id": ajena, "nombre": "Planes", "modo": "sugiere"}}}})
	x.exigir(r, http.StatusOK)
	if r.cuerpo["categoria"].(map[string]any)["id"] != nil {
		t.Fatalf("devolvió un id de otra empresa: %v", r.cuerpo["categoria"])
	}
	if n := x.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'asistente.mensaje_probado' AND (payload->>'categoria_id')::bigint = $1`, ajena); n != 0 {
		t.Fatal("el evento lleva un id de otra empresa")
	}
}

// 9: GET /asistente/categorias no siembra y, sin permiso de configurar, solo da la ficha.
func TestAsistente3ListarCategoriasNoSiembraYLimita(t *testing.T) {
	x := nuevaBandejaIA(t)
	sup3 := x.persona([]int64{2}, rolBase("supervisor"))
	antes := x.contar(`SELECT count(*) FROM mensajeria.asistente_categorias`)
	r := x.api(sup3, http.MethodGet, "/asistente/categorias", nil)
	if x.exigir(r, http.StatusOK); len(r.cuerpo["data"].([]any)) != 0 || x.contar(`SELECT count(*) FROM mensajeria.asistente_categorias`) != antes {
		t.Fatalf("el GET sembró categorías: %v", r.cuerpo)
	}
	// La empresa 1 ya las tiene (se sembraron al guardar los ajustes). Un agente ve solo la ficha.
	cat := x.categoriaConCola("Sin internet", x.cola, 10)
	r = x.api(x.agente, http.MethodGet, "/asistente/categorias", nil)
	x.exigir(r, http.StatusOK)
	for _, d := range r.cuerpo["data"].([]any) {
		k := d.(map[string]any)
		if idDe(k) != idDe(cat) {
			continue
		}
		_, conModo := k["modo"]
		if k["nombre"] != "Sin internet" || k["tono"] != "error" || k["activa"] != true || conModo || k["colaId"] != nil || k["slaPrimeraRespuestaMin"] != nil ||
			k["condicion"] != nil || k["descripcion"] != "" {
			t.Fatalf("un agente ve la configuración: %v", k)
		}
		return
	}
	t.Fatal("el agente no ve la categoría")
}
