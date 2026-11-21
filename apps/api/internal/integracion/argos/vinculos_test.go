package argos_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
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
