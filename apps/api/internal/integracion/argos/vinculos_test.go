package argos_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Contrato 0.14.0 §4.5 y §6.6: al informar 0.14.0 a un Argos que lo habla, una sola vez por
// conector, el iris_pat_ vigente suma adjuntos.verificar_integracion y agenda emite la carga inicial
// de vínculos; la carga se cierra con PUT …/contrato { vinculos_cargados } solo con el 2xx de todos
// sus vinculados (los omitidos no cuentan). Mientras Iris informe una versión anterior, nada de esto
// corre aunque Argos hable 0.14.0.
func TestCargaDeVinculosAlInformar014(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version, e.argos.contratoMax = "1.30.0", "0.14.0" })
	_, id, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	permiso := string(nucleo.PermAdjuntosVerificarIntegracion)
	// Un token con lista de permisos anterior a la 0.14.0: sin el de la verificación (uno sin lista ya
	// tiene los del rol, y la ruta solo responde con 0.14.0 informado).
	if _, err := e.pool.Exec(ctx, `UPDATE nucleo.tokens_api SET permisos = ARRAY[$2]
		WHERE id = (SELECT token_entregado_id FROM integracion.conectores WHERE id = $1)`, id, string(nucleo.PermCuadrillasLeerIntegracion)); err != nil {
		t.Fatal(err)
	}
	tienePermiso := func() bool {
		return e.contar(`SELECT count(*) FROM nucleo.tokens_api t JOIN integracion.conectores c ON c.token_entregado_id = t.id
			WHERE c.id = $1 AND $2 = ANY(t.permisos)`, id, permiso) == 1
	}

	// Iris informa 0.8.1 (el de esta versión): sin carga ni permiso, aunque Argos hable 0.14.0.
	if _, err := e.mod.InformarContrato.Ejecutar(ctx, id); err != nil {
		t.Fatal(err)
	}
	if tienePermiso() || e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_carga_id IS NULL`, id) != 1 {
		t.Fatal("con 0.8.1 informado no hay carga ni permiso nuevo")
	}

	// Iris configurada para informar 0.14.0: la carga emite 2 vinculados (agenda, aquí falsa).
	cargas := []uuid.UUID{}
	cfg := e.cfg
	cfg.ContratoIris = "0.14.0"
	cfg.Vinculos = func(_ context.Context, conectorID int64, carga uuid.UUID) (int, error) {
		if conectorID != id {
			t.Errorf("conector de la carga: %d", conectorID)
		}
		cargas = append(cargas, carga)
		return 2, nil
	}
	mod := argos.Nuevo(cfg)
	mod.Nucleo().FijarTokens(e.ident)
	if !mod.InformaVinculos() || e.mod.InformaVinculos() {
		t.Fatal("InformaVinculos sigue a la versión informada")
	}
	if _, err := mod.InformarContrato.Ejecutar(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(cargas) != 1 || !tienePermiso() {
		t.Fatalf("carga y permiso: %v %v", cargas, tienePermiso())
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_carga_id = $2 AND vinculos_carga_n = 2
		AND vinculos_cargados_en IS NULL`, id, cargas[0]) != 1 {
		t.Fatal("carga guardada y abierta")
	}
	// Informar otra vez no empieza otra carga ni repite el permiso.
	if _, err := mod.InformarContrato.Ejecutar(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(cargas) != 1 || e.contar(`SELECT cardinality(array_positions(t.permisos, $2)) FROM nucleo.tokens_api t
		JOIN integracion.conectores c ON c.token_entregado_id = t.id WHERE c.id = $1`, id, permiso) != 1 {
		t.Fatal("una sola carga y el permiso una vez")
	}

	webhook := func(estado string) uuid.UUID {
		w := uuid.Must(uuid.NewV7())
		crudo, _ := json.Marshal(map[string]any{"tecnico_id": 1})
		if _, err := e.pool.Exec(ctx, `INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, conector_origen, carga_id)
			VALUES ($1, 'tecnico.argos_vinculado', 1, $2, now(), $3, $4, $5)`, w, crudo, estado, id, cargas[0]); err != nil {
			t.Fatal(err)
		}
		return w
	}
	ultimoCargado := func() any {
		var v any
		e.argos.con(func() { v = e.argos.cargados[len(e.argos.cargados)-1] })
		return v
	}
	cerrar := func() bool {
		t.Helper()
		ok, err := mod.CerrarCargaVinculos(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	// Falta uno de los dos en la cola: abierta.
	webhook("entregado")
	if cerrar() || ultimoCargado() != nil {
		t.Fatal("con un vinculado aún sin programar no se cierra")
	}
	// Los dos, pero uno sin el 2xx: abierta. Agotado tampoco cierra.
	otro := webhook("pendiente")
	if cerrar() {
		t.Fatal("con un vinculado pendiente no se cierra")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE mensajeria.webhooks_salida SET estado = 'agotado' WHERE id = $1`, otro); err != nil {
		t.Fatal(err)
	}
	if cerrar() {
		t.Fatal("con un vinculado agotado no se cierra")
	}
	// Omitido (fuera del alcance): no salió, no cuenta. Se cierra con vinculos_cargados = 1.
	if _, err := e.pool.Exec(ctx, `UPDATE mensajeria.webhooks_salida SET estado = 'omitido' WHERE id = $1`, otro); err != nil {
		t.Fatal(err)
	}
	if !cerrar() {
		t.Fatal("con el 2xx de todos se cierra")
	}
	var informado any
	e.argos.con(func() { informado = e.argos.informados[len(e.argos.informados)-1] })
	if v, ok := ultimoCargado().(float64); !ok || v != 1 || informado != "0.14.0" {
		t.Fatalf("PUT de cierre: vinculos_cargados=%v contrato=%v", ultimoCargado(), informado)
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_cargados_en IS NOT NULL`, id) != 1 ||
		e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.cerrar_carga_vinculos_argos' AND entidad_id = $1`,
			strconv.FormatInt(id, 10)) != 1 {
		t.Fatal("carga cerrada y auditada")
	}
	if cerrar() {
		t.Fatal("una carga cerrada no se vuelve a cerrar")
	}
}

// A-1: reconectar la misma instancia de Argos (base_url normalizada y, si los dos lo traen, remoto_id)
// pasa al conector nuevo los vínculos de los técnicos y los webhooks tecnico.argos_* sin entregar. Lo
// entregado y la carga cerrada NO pasan: el conector nuevo corre su propia carga al informar 0.14.0
// (un Argos reinstalado en la misma URL no hereda «entregado»). Otra instancia no se lleva nada.
func TestReconexionConservaLosVinculos(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version, e.argos.contratoMax = "1.30.0", "0.14.0" })
	admin, viejo, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	u, err := e.ident.CrearUsuario.Ejecutar(ctx, identidad.EntradaCrearUsuario{Nombre: "Técnico", Correo: pruebas.Correo("vinc"), Contrasena: contrasena})
	if err != nil {
		t.Fatal(err)
	}
	var ficha int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO agenda.tecnicos (usuario_id, argos_id_externo, argos_vinculado_en, argos_conector_id)
		VALUES ($1, $2, now(), $3) RETURNING id`, u.ID, "u-recon-"+pruebas.Unico(), viejo).Scan(&ficha); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = e.pool.Exec(context.Background(), `DELETE FROM agenda.tecnicos WHERE id = $1`, ficha) })
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET vinculos_carga_id = $2, vinculos_carga_n = 2, vinculos_cargados_en = now()
		WHERE id = $1`, viejo, uuid.Must(uuid.NewV7())); err != nil {
		t.Fatal(err)
	}
	webhook := func(evento, estado string) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		if _, err := e.pool.Exec(ctx, `INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, conector_origen)
			VALUES ($1, $2, 1, '{}', now(), $3, $4)`, id, evento, estado, viejo); err != nil {
			t.Fatal(err)
		}
		return id
	}
	entregado, pendiente := webhook("tecnico.argos_vinculado", "entregado"), webhook("tecnico.argos_vinculado", "pendiente")

	// La misma URL escrita distinto (mayúsculas, puerto por defecto) es la misma instancia.
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET base_url = upper(base_url) WHERE id = $1`, viejo); err != nil {
		t.Fatal(err)
	}
	nuevo := e.reconectar(admin, false)
	if nuevo == viejo {
		t.Fatal("la reconexión crea otro conector")
	}
	if e.contar(`SELECT count(*) FROM agenda.tecnicos WHERE id = $1 AND argos_conector_id = $2`, ficha, nuevo) != 1 {
		t.Fatal("el vínculo pasa al conector nuevo")
	}
	if e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND conector_origen = $2`, pendiente, nuevo) != 1 ||
		e.contar(`SELECT count(*) FROM mensajeria.webhooks_salida WHERE id = $1 AND conector_origen = $2`, entregado, viejo) != 1 {
		t.Fatal("solo el webhook sin entregar pasa al conector nuevo")
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_carga_id IS NULL AND vinculos_cargados_en IS NULL`, nuevo) != 1 {
		t.Fatal("la carga cerrada no se hereda")
	}
	// Al informar 0.14.0, el conector nuevo corre su carga (con los vínculos que ahora son suyos).
	cargas := []int64{}
	cfg := e.cfg
	cfg.ContratoIris = "0.14.0"
	cfg.Vinculos = func(_ context.Context, conectorID int64, _ uuid.UUID) (int, error) {
		cargas = append(cargas, conectorID)
		return 1, nil
	}
	mod := argos.Nuevo(cfg)
	mod.Nucleo().FijarTokens(e.ident)
	if _, err := mod.InformarContrato.Ejecutar(ctx, nuevo); err != nil {
		t.Fatal(err)
	}
	if len(cargas) != 1 || cargas[0] != nuevo || e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_carga_id IS NOT NULL
		AND vinculos_cargados_en IS NULL`, nuevo) != 1 {
		t.Fatalf("la carga se vuelve a correr en el conector nuevo: %v", cargas)
	}

	// Otra instancia: otra base_url, o la misma con otro remoto_id, no se lleva nada.
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET base_url = 'https://otro-argos.prueba' WHERE id = $1`, nuevo); err != nil {
		t.Fatal(err)
	}
	otro := e.reconectar(admin, false)
	if e.contar(`SELECT count(*) FROM agenda.tecnicos WHERE id = $1 AND argos_conector_id = $2`, ficha, nuevo) != 1 || otro == nuevo {
		t.Fatal("otra URL no se lleva el vínculo")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE agenda.tecnicos SET argos_conector_id = $2 WHERE id = $1`, ficha, otro); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET remoto_id = 'argos-a' WHERE id = $1`, otro); err != nil {
		t.Fatal(err)
	}
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 {
		t.Fatalf("conectar: %d %s", r.status, r.crudo)
	}
	ultimo := int64(r.cuerpo["id"].(float64))
	if _, err := e.pool.Exec(ctx, `UPDATE integracion.conectores SET remoto_id = 'argos-b' WHERE id = $1`, ultimo); err != nil {
		t.Fatal(err)
	}
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", ultimo), map[string]any{"version": 1}); a.status != 200 {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	if e.contar(`SELECT count(*) FROM agenda.tecnicos WHERE id = $1 AND argos_conector_id = $2`, ficha, otro) != 1 {
		t.Fatal("otro remoto_id no se lleva el vínculo")
	}
}

// M-2: con un vinculado de la carga agotado, la carga no se cierra y el admin recibe un aviso (un
// evento argos.carga_vinculos_detenida por episodio); reenviado y entregado, se cierra.
func TestCargaDetenidaAvisa(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version, e.argos.contratoMax = "1.30.0", "0.14.0" })
	_, id, _ := e.conectado()
	ctx := pruebas.ContextoSistema(e.pool)
	cfg := e.cfg
	cfg.ContratoIris = "0.14.0"
	cfg.Vinculos = func(context.Context, int64, uuid.UUID) (int, error) { return 1, nil }
	mod := argos.Nuevo(cfg)
	mod.Nucleo().FijarTokens(e.ident)
	if _, err := mod.InformarContrato.Ejecutar(ctx, id); err != nil {
		t.Fatal(err)
	}
	var carga uuid.UUID
	if err := e.pool.QueryRow(ctx, `SELECT vinculos_carga_id FROM integracion.conectores WHERE id = $1`, id).Scan(&carga); err != nil {
		t.Fatal(err)
	}
	w := uuid.Must(uuid.NewV7())
	if _, err := e.pool.Exec(ctx, `INSERT INTO mensajeria.webhooks_salida (id, evento, empresa_id, payload, ocurrido_en, estado, conector_origen, carga_id)
		VALUES ($1, 'tecnico.argos_vinculado', 1, '{}', now(), 'agotado', $2, $3)`, w, id, carga); err != nil {
		t.Fatal(err)
	}
	avisos := func() int {
		return e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = $1 AND payload->>'carga_id' = $2`, argos.EventoCargaDetenida, carga.String())
	}
	for range 2 {
		if ok, err := mod.CerrarCargaVinculos(ctx); err != nil || ok {
			t.Fatalf("con un agotado no se cierra: %v %v", ok, err)
		}
	}
	if avisos() != 1 {
		t.Fatalf("un aviso por episodio: %d", avisos())
	}
	// Reenviado (vuelve a pendiente): el episodio termina; entregado, se cierra.
	if _, err := e.pool.Exec(ctx, `UPDATE mensajeria.webhooks_salida SET estado = 'pendiente' WHERE id = $1`, w); err != nil {
		t.Fatal(err)
	}
	if ok, err := mod.CerrarCargaVinculos(ctx); err != nil || ok {
		t.Fatalf("pendiente: %v %v", ok, err)
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND vinculos_carga_avisada_en IS NULL`, id) != 1 {
		t.Fatal("el episodio terminó")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE mensajeria.webhooks_salida SET estado = 'entregado' WHERE id = $1`, w); err != nil {
		t.Fatal(err)
	}
	if ok, err := mod.CerrarCargaVinculos(ctx); err != nil || !ok {
		t.Fatalf("entregado: %v %v", ok, err)
	}
}
