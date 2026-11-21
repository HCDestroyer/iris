package inventario_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/inventario"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// cuadrilla crea una cuadrilla de la empresa 1 con esos técnicos (usuario → id_externo) y su jefe.
func (e *entorno) cuadrilla(jefe int64, miembros map[int64]string) int64 {
	e.t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES (1, $1) RETURNING id`,
		"Cuadrilla "+pruebas.Unico()).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	for u, externo := range miembros {
		e.exec(`DELETE FROM agenda.tecnicos WHERE argos_id_externo = $1 OR usuario_id = $2`, externo, u)
		e.exec(`INSERT INTO agenda.tecnicos (usuario_id, cuadrilla_id, argos_id_externo, argos_correo, argos_vinculado_en)
			VALUES ($1, $2, $3, $3 || '@argos.gt', now())`, u, id, externo)
	}
	if jefe != 0 {
		e.exec(`UPDATE agenda.cuadrillas SET jefe_usuario_id = $2 WHERE id = $1`, id, jefe)
	}
	return id
}

func nuevoID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

func contar(t *testing.T, e *entorno, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// actuarComo hace que las ayudas del entorno (firma, recibir) actúen como ese técnico.
func (e *entorno) actuarComo(usuario int64) {
	e.tec, e.ctxTec = usuario, e.como(usuario)
}

// Contrato 0.11.0 §3.9 (B26): «Mi bodega» con la de la cuadrilla y la suya; Iris guarda qué bodega es
// de qué cuadrilla para validar el bodega_id del lote.
func TestMiBodegaConCuadrilla(t *testing.T) {
	e := nuevoEntorno(t)
	jefe, miembro := e.usuario(), e.usuario()
	c := e.cuadrilla(jefe, map[int64]string{jefe: "u-88", miembro: "u-89"})
	cs := strconv.FormatInt(c, 10)
	bodegaCuadrilla := `{"id":81,"empresa_id":1,"nombre":"Pickup P-12","cuadrilla":true,"jefe":{"id_externo":"u-88","nombre":"Carlos Pérez"},
		"bodega":{"id":81,"nombre":"Pickup P-12","empresa_id":1,"tipo":"cuadrilla","cuadrilla_id_iris":"` + cs + `"},
		"existencias":[{"producto_id":5,"nombre":"Cable drop","cantidad":"420","unidad":"m","modalidad":"consumible_gasto"}],
		"unidades":[],"traslados_pendientes":[{"id":214}],"kardex":[]}`
	personal := `{"id":71,"empresa_id":1,"nombre":"Bodega Carlos","cuadrilla":false,"jefe":null,
		"bodega":{"id":71,"nombre":"Bodega Carlos","empresa_id":1,"tipo":"tecnico","cuadrilla_id_iris":null},
		"existencias":[{"producto_id":7,"nombre":"Conector","cantidad":"6","unidad":"u","modalidad":"consumible_gasto"}],
		"unidades":[],"traslados_pendientes":[],"kardex":[]}`
	e.argos.miBodega["u-88"] = `{"id":71,"empresa_id":1,"nombre":"Bodega Carlos","cuadrilla":false,"jefe":null,
		"bodega":{"id":71,"nombre":"Bodega Carlos","empresa_id":1},
		"existencias":[{"producto_id":7,"nombre":"Conector","cantidad":"6","unidad":"u","modalidad":"consumible_gasto"}],
		"unidades":[],"traslados_pendientes":[],"kardex":[],"bodegas":[` + bodegaCuadrilla + `,` + personal + `]}`
	e.argos.miBodega["u-89"] = strings.Replace(bodegaCuadrilla, `"id":81,"empresa_id":1,"nombre":"Pickup P-12","cuadrilla":true`,
		`"bodegas":[`+bodegaCuadrilla+`],"id":81,"empresa_id":1,"nombre":"Pickup P-12","cuadrilla":true`, 1)

	b, err := e.inv.MiBodega.Ejecutar(e.como(jefe))
	if err != nil {
		t.Fatal(err)
	}
	if b.Bodega.ID != 71 || len(b.Bodegas) != 2 {
		t.Fatalf("la principal es la personal y vienen las dos: %+v", b)
	}
	bc := b.Bodegas[0]
	if !bc.Cuadrilla || bc.CuadrillaID == nil || *bc.CuadrillaID != c || bc.Jefe == nil || !bc.Jefe.SoyYo || bc.Jefe.Nombre != "Carlos Pérez" ||
		len(bc.Existencias) != 1 || bc.Existencias[0].Cantidad != 420 || len(bc.TrasladosPendientes) != 1 {
		t.Fatalf("la de la cuadrilla: %+v", bc)
	}
	if b.Bodegas[1].Cuadrilla || b.Bodegas[1].Bodega.ID != 71 || b.Bodegas[1].Jefe != nil {
		t.Fatalf("la personal: %+v", b.Bodegas[1])
	}
	var cuadrilla, empresa int64
	if err := e.pool.QueryRow(context.Background(), `SELECT cuadrilla_id, empresa_id FROM integracion.argos_bodegas_cuadrilla WHERE bodega_id = 81`).
		Scan(&cuadrilla, &empresa); err != nil || cuadrilla != c || empresa != 1 {
		t.Fatalf("bodega de cuadrilla guardada: %d %d %v", cuadrilla, empresa, err)
	}
	// La personal queda con su empresa y su dueño (M-268-1: el lote la valida en la empresa de la orden).
	var dueno int64
	if err := e.pool.QueryRow(context.Background(), `SELECT usuario_id, empresa_id FROM integracion.argos_bodegas_tecnico WHERE bodega_id = 71`).
		Scan(&dueno, &empresa); err != nil || dueno != jefe || empresa != 1 {
		t.Fatalf("bodega personal guardada: %d %d %v", dueno, empresa, err)
	}
	if n := contar(t, e, `SELECT count(*) FROM integracion.argos_bodegas_tecnico WHERE bodega_id = 81`); n != 0 {
		t.Fatal("la de cuadrilla no es personal")
	}
	// Para un miembro que no es el jefe, el jefe no es él.
	b, err = e.inv.MiBodega.Ejecutar(e.como(miembro))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Bodegas) != 1 || b.Bodegas[0].Jefe == nil || b.Bodegas[0].Jefe.SoyYo {
		t.Fatalf("miembro: %+v", b.Bodegas)
	}
	// Con un Argos anterior (sin bodegas[]), solo la suya.
	b, err = e.inv.MiBodega.Ejecutar(e.ctxTec)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Bodegas) != 1 || b.Bodegas[0].Cuadrilla || b.Bodegas[0].Bodega.ID != 70 {
		t.Fatalf("Argos anterior: %+v", b.Bodegas)
	}
}

// Contrato 0.11.0 §3.9 y §10.6 k: el traslado a la bodega de la cuadrilla lo recibe y firma solo el
// jefe; los demás miembros lo ven en camino y el servidor les responde 403 solo_jefe_recibe antes de
// encolar; a alguien de fuera, el mismo 404 de siempre. La recepción sale con X-Iris-Actor del jefe.
func TestTrasladoACuadrillaSoloElJefe(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	jefe, miembro, ajeno := e.usuario(), e.usuario(), e.usuario()
	c := e.cuadrilla(jefe, map[int64]string{jefe: "u-88", miembro: "u-89"})
	e.cuadrilla(ajeno, map[int64]string{ajeno: "u-90"})
	tr := trasladoNuevo()
	e.exec(`DELETE FROM integracion.argos_traslados WHERE id = $1`, tr)
	e.exec(`INSERT INTO integracion.argos_traslados (id, empresa_id, bodega_destino_id, cuadrilla_id, estado, lineas, enviado_en)
		VALUES ($1, 1, 81, $2, 'enviado', '[{"linea_id": 1, "producto_id": 5, "cantidad": "10", "series": []}]', '2026-10-30T15:00:00Z')`, tr, c)

	// El miembro lo ve, sin poder recibirlo.
	l, err := e.inv.Traslados.Ejecutar(e.como(miembro), inventario.EntradaTraslados{Estado: "enviado"})
	if err != nil {
		t.Fatal(err)
	}
	visto := false
	for _, x := range l.Data {
		if x.ID == tr {
			visto = true
			if x.PuedeRecibir || x.CuadrillaID == nil || *x.CuadrillaID != c {
				t.Fatalf("el miembro lo ve en camino: %+v", x)
			}
		}
	}
	if !visto {
		t.Fatal("el miembro ve el traslado de su cuadrilla")
	}
	// Ni la firma ni la recepción del miembro: 404 en la firma (solo quien recibe), 403 en el lote.
	e.actuarComo(miembro)
	contenido := pngDe(t, 300, 100)
	if _, err := e.adj.Registrar.Ejecutar(e.ctxTec, adjuntos.EntradaRegistrar{ID: nuevoID(), Entidad: "traslado", EntidadID: strconv.FormatInt(tr, 10),
		Tipo: "firma", Mime: "image/png", Tamano: 10, SHA256: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("un miembro que no es el jefe no registra la firma")
	}
	e.actuarComo(jefe)
	firma := e.firma(tr, contenido, true)
	e.actuarComo(miembro)
	r := e.recibir(tr, firma)
	if r.Estado != "rechazada" || r.Error == nil || r.Error.Status != http.StatusForbidden || r.Error.Codigo() != inventario.CodigoSoloJefeRecibe {
		t.Fatalf("miembro: %+v %+v", r, r.Error)
	}
	if n := contar(t, e, `SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1`, inventario.ClaveRecepcion(tr)); n != 0 {
		t.Fatal("no se encola la recepción de quien no es el jefe")
	}
	// De otra cuadrilla: como si no existiera.
	e.actuarComo(ajeno)
	if r := e.recibir(tr, firma); r.Estado != "rechazada" || r.Error == nil || r.Error.Codigo() != inventario.CodigoTrasladoNoEncontrado {
		t.Fatalf("ajeno: %+v %+v", r, r.Error)
	}

	// El jefe lo recibe; sale con su X-Iris-Actor y su id_externo en recibido_por.
	e.actuarComo(jefe)
	l, err = e.inv.Traslados.Ejecutar(e.ctxTec, inventario.EntradaTraslados{Estado: "enviado"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range l.Data {
		if x.ID == tr && !x.PuedeRecibir {
			t.Fatal("el jefe puede recibir")
		}
	}
	r = e.recibir(tr, firma)
	if r.Estado != "aplicada" || r.Recepcion == nil {
		t.Fatalf("jefe: %+v %+v", r, r.Error)
	}
	if res, err := e.inv.ArmarFirma(ctx, r.Recepcion.PendienteID); err != nil || res.Resultado != "lista" {
		t.Fatalf("armar: %+v %v", res, err)
	}
	if _, err := e.pend.Procesar(ctx); err != nil {
		t.Fatal(err)
	}
	e.argos.mu.Lock()
	var enviada *recibida
	for i := range e.argos.recibidas {
		if e.argos.recibidas[i].Clave == inventario.ClaveRecepcion(tr) {
			enviada = &e.argos.recibidas[i]
		}
	}
	e.argos.mu.Unlock()
	if enviada == nil || enviada.Actor != "usuario:"+strconv.FormatInt(jefe, 10) ||
		enviada.Cuerpo["recibido_por"].(map[string]any)["id_externo"] != "u-88" {
		t.Fatalf("recepción en Argos: %+v", enviada)
	}
}

// Si Iris no conoce el traslado, lo confirma con Argos: GET /inventario/traslados/:id solo trae la
// bodega (tipo cuadrilla); la cuadrilla sale de la bodega que Iris ya vio en mi-bodega.
func TestTrasladoACuadrillaConfirmadoConArgos(t *testing.T) {
	e := nuevoEntorno(t)
	jefe, miembro := e.usuario(), e.usuario()
	c := e.cuadrilla(jefe, map[int64]string{jefe: "u-88", miembro: "u-89"})
	e.exec(`INSERT INTO integracion.argos_bodegas_cuadrilla (bodega_id, empresa_id, cuadrilla_id) VALUES (82, 1, $1)
		ON CONFLICT (bodega_id) DO UPDATE SET cuadrilla_id = excluded.cuadrilla_id`, c)
	tr := trasladoNuevo()
	e.exec(`DELETE FROM integracion.argos_traslados WHERE id = $1`, tr)
	e.argos.mu.Lock()
	e.argos.traslados[strconv.FormatInt(tr, 10)] = strings.Replace(detalleArgos(tr, "cuadrilla", "null", "en_transito"),
		`"bodega_destino_id":70`, `"bodega_destino_id":82`, 1)
	e.argos.mu.Unlock()

	e.actuarComo(miembro)
	if r := e.recibir(tr, nuevoID()); r.Estado != "rechazada" || r.Error == nil || r.Error.Codigo() != inventario.CodigoSoloJefeRecibe {
		t.Fatalf("miembro: %+v %+v", r, r.Error)
	}
	var cuadrilla *int64
	if err := e.pool.QueryRow(context.Background(), `SELECT cuadrilla_id FROM integracion.argos_traslados WHERE id = $1`, tr).Scan(&cuadrilla); err != nil ||
		cuadrilla == nil || *cuadrilla != c {
		t.Fatalf("cuadrilla del traslado confirmado: %v %v", cuadrilla, err)
	}
	e.actuarComo(jefe)
	firma := e.firma(tr, pngDe(t, 300, 100), true)
	if r := e.recibir(tr, firma); r.Estado != "aplicada" {
		t.Fatalf("jefe: %+v %+v", r, r.Error)
	}
	// Sin jefe, nadie lo recibe (el traslado espera).
	tr2 := trasladoNuevo() + 1
	e.exec(`DELETE FROM integracion.argos_traslados WHERE id = $1`, tr2)
	e.exec(`INSERT INTO integracion.argos_traslados (id, empresa_id, bodega_destino_id, cuadrilla_id, estado, lineas, enviado_en)
		VALUES ($1, 1, 82, $2, 'enviado', '[]', '2026-10-30T15:00:00Z')`, tr2, c)
	e.exec(`UPDATE agenda.cuadrillas SET jefe_usuario_id = NULL WHERE id = $1`, c)
	if r := e.recibir(tr2, nuevoID()); r.Estado != "rechazada" || r.Error == nil || r.Error.Codigo() != inventario.CodigoSoloJefeRecibe {
		t.Fatalf("sin jefe: %+v %+v", r, r.Error)
	}
}
