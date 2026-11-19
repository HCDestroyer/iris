package argos_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// enLista dice si el conector sale en GET /integraciones/conectores y si es eliminable.
func enLista(t *testing.T, c *clienteHTTP, id int64) (esta, eliminable bool) {
	t.Helper()
	r := c.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil)
	if r.status != 200 {
		t.Fatalf("lista: %d %s", r.status, r.crudo)
	}
	for _, d := range r.cuerpo["data"].([]any) {
		m := d.(map[string]any)
		if int64(m["id"].(float64)) == id {
			return true, m["eliminable"] == true
		}
	}
	return false, false
}

// Eliminar un conector apagado: solo apagado y sin reconexión pendiente; borrado lógico sin
// credenciales; su iris_pat_ se elimina con él; idempotente; auditado y con evento.
func TestEliminarConector(t *testing.T) {
	e := nuevoEntorno(t)
	admin, viejo, nuevo, patViejo := e.reconectado()
	var tokenViejo int64
	if err := e.pool.QueryRow(t.Context(), `SELECT token_entregado_id FROM integracion.conectores WHERE id = $1`, viejo).Scan(&tokenViejo); err != nil {
		t.Fatal(err)
	}
	secretoViejo := ""
	e.argos.con(func() { secretoViejo = secretoDelCanje(e.argos.canjes[len(e.argos.canjes)-2]) })
	ruta := func(id int64) string { return fmt.Sprintf("/api/v1/integraciones/conectores/%d/eliminar", id) }

	// Un token revocado que un conector activo usa (el del activo, aquí revocado a mano) no se elimina.
	var tokenNuevo int64
	if err := e.pool.QueryRow(t.Context(), `UPDATE nucleo.tokens_api SET revocado_en = now()
		WHERE id = (SELECT token_entregado_id FROM integracion.conectores WHERE id = $1) RETURNING id`, nuevo).Scan(&tokenNuevo); err != nil {
		t.Fatal(err)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/eliminar", tokenNuevo), map[string]any{}).problema(t, 409, "token_en_uso_por_conector")
	if r := admin.pedir(http.MethodGet, "/api/v1/tokens/eliminables", nil); r.status != 200 || r.cuerpo["enUso"].(float64) < 1 {
		t.Fatalf("eliminables cuenta el que está en uso: %d %s", r.status, r.crudo)
	}
	// Y el iris_pat_ anterior (reemplaza_token_id del activo) tampoco, aunque se revoque.
	if _, err := e.pool.Exec(t.Context(), `UPDATE nucleo.tokens_api SET revocado_en = now() WHERE id = $1`, tokenViejo); err != nil {
		t.Fatal(err)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/eliminar", tokenViejo), map[string]any{}).problema(t, 409, "token_en_uso_por_conector")
	if _, err := e.pool.Exec(t.Context(), `UPDATE nucleo.tokens_api SET revocado_en = NULL WHERE id = ANY($1)`, []int64{tokenViejo, tokenNuevo}); err != nil {
		t.Fatal(err)
	}

	// Sin permiso: 403 (supervisor ve integraciones, no las elimina).
	e.usuario("supervisor").pedir(http.MethodPost, ruta(viejo), map[string]any{}).problema(t, 403, "prohibido")
	// Activo: 409.
	admin.pedir(http.MethodPost, ruta(nuevo), map[string]any{}).problema(t, 409, "conector_activo")
	// Reemplazado por el activo con la reconexión sin confirmar: Argos aún usa su acceso.
	if _, eliminable := enLista(t, admin, viejo); eliminable {
		t.Fatal("el reemplazado sin confirmar no es eliminable")
	}
	admin.pedir(http.MethodPost, ruta(viejo), map[string]any{}).problema(t, 409, "conector_en_uso")
	if r := e.leerConPat(patViejo); r.status != 200 {
		t.Fatalf("el 409 no toca su iris_pat_: %d", r.status)
	}

	// Confirmada la reconexión (Argos usó el atk_ nuevo), el viejo queda libre.
	if r, err := e.mod.Nucleo().ConfirmarReemplazo.Ejecutar(pruebas.ContextoSistema(e.pool), nuevo); err != nil || !r.Cerrado {
		t.Fatalf("confirmar: %+v %v", r, err)
	}
	if esta, eliminable := enLista(t, admin, viejo); !esta || !eliminable {
		t.Fatal("el apagado libre sale en la lista como eliminable")
	}
	r := admin.pedir(http.MethodPost, ruta(viejo), map[string]any{"motivo": "ya no sirve"})
	if r.status != 200 || r.cuerpo["eliminadoEn"] == nil || r.cuerpo["estado"] != "apagado" {
		t.Fatalf("eliminar: %d %s", r.status, r.crudo)
	}
	if esta, _ := enLista(t, admin, viejo); esta {
		t.Fatal("un conector eliminado no sale en la lista")
	}
	if e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND eliminado_en IS NOT NULL AND eliminado_por IS NOT NULL
		AND token_cifrado IS NULL AND secreto_webhook_cifrado IS NULL AND webhook_salida_secreto_cifrado IS NULL`, viejo) != 1 {
		t.Fatal("borrado lógico sin credenciales")
	}
	if e.contar(`SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND eliminado_en IS NOT NULL AND hash IS NULL AND revocado_en IS NOT NULL`, tokenViejo) != 1 {
		t.Fatal("su iris_pat_ queda revocado y eliminado, sin hash")
	}
	if r := e.leerConPat(patViejo); r.status != 401 {
		t.Fatalf("el iris_pat_ del eliminado no autentica: %d", r.status)
	}
	// Webhooks firmados con su secreto: rechazados como una firma mala (no revela que existió).
	e.enviar(viejo, secretoViejo, "wh-elim-"+pruebas.Unico(), "cliente.actualizado", map[string]any{"empresa_id": 1, "cliente_id": 71}, time.Now()).
		problema(t, 401, "firma_invalida")
	// Activarlo: 404.
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", viejo), map[string]any{"version": 3}).problema(t, 404, "no_encontrado")

	// Idempotente: otra vez 200, sin evento nuevo.
	if r := admin.pedir(http.MethodPost, ruta(viejo), map[string]any{}); r.status != 200 {
		t.Fatalf("eliminar otra vez: %d %s", r.status, r.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.eliminado' AND (payload->>'conector_id')::bigint = $1
		AND payload->>'motivo' = 'manual' AND (payload->>'token_id')::bigint = $2`, viejo, tokenViejo); n != 1 {
		t.Fatalf("conector.eliminado: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'token.eliminado' AND (payload->>'token_id')::bigint = $1
		AND payload->>'motivo' = 'conector'`, tokenViejo); n != 1 {
		t.Fatalf("token.eliminado del iris_pat_: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.eliminar' AND entidad_id = $1 AND resultado = 'ok'`, fmt.Sprint(viejo)); n != 2 {
		t.Fatalf("auditoría de integraciones.eliminar (eliminar y repetir): %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'tokens.eliminar' AND entidad_id = $1 AND resultado = 'ok'`, fmt.Sprint(tokenViejo)); n != 1 {
		t.Fatalf("auditoría de tokens.eliminar: %d", n)
	}
	e.sinSecretos(patViejo, secretoViejo)

	// En lote: desactivado el nuevo, «Eliminar apagados» se lo lleva (con su iris_pat_).
	if d := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", nuevo), map[string]any{"version": 2}); d.status != 200 {
		t.Fatalf("desactivar: %d %s", d.status, d.crudo)
	}
	lote := admin.pedir(http.MethodPost, "/api/v1/integraciones/conectores/eliminar-apagados", nil)
	if lote.status != 200 || lote.cuerpo["eliminados"].(float64) < 1 {
		t.Fatalf("lote: %d %s", lote.status, lote.crudo)
	}
	if esta, _ := enLista(t, admin, nuevo); esta {
		t.Fatal("el lote elimina el apagado")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.eliminado' AND (payload->>'conector_id')::bigint = $1 AND payload->>'motivo' = 'lote'`, nuevo); n != 1 {
		t.Fatalf("conector.eliminado del lote: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.conectores WHERE estado = 'apagado' AND eliminado_en IS NULL`); n != 0 {
		t.Fatalf("quedan apagados sin eliminar: %d", n)
	}
	// Repetir el lote: nada que hacer.
	if r := admin.pedir(http.MethodPost, "/api/v1/integraciones/conectores/eliminar-apagados", nil); r.status != 200 || r.cuerpo["eliminados"] != float64(0) {
		t.Fatalf("lote vacío: %d %s", r.status, r.crudo)
	}
}
