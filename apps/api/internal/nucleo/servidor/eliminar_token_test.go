package servidor_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// tokenCon crea con la sesión c un token propio y devuelve su id y su valor.
func tokenCon(t *testing.T, c *cliente, cuerpo map[string]any) (int64, string) {
	t.Helper()
	if cuerpo == nil {
		cuerpo = map[string]any{}
	}
	cuerpo["nombre"] = "token " + pruebas.Unico()
	r := c.pedir(http.MethodPost, "/api/v1/tokens", cuerpo)
	if r.status != 201 {
		t.Fatalf("crear token: %d %s", r.status, r.crudo)
	}
	return int64(r.cuerpo["token"].(map[string]any)["id"].(float64)), r.cuerpo["valor"].(string)
}

// vencer deja el token vencido (como si hubiera pasado su expiraEn).
func vencer(t *testing.T, id int64) {
	t.Helper()
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE nucleo.tokens_api SET expira_en = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
}

func idsTokens(t *testing.T, c *cliente, consulta string) []int64 {
	t.Helper()
	return idsDe(t, c.pedir(http.MethodGet, "/api/v1/tokens?limit=200"+consulta, nil))
}

func TestEliminarToken(t *testing.T) {
	e := nuevoEntorno(t)
	_, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	ruta := func(id int64) string { return fmt.Sprintf("/api/v1/tokens/%d/eliminar", id) }

	activo, valorActivo := tokenCon(t, admin, nil)
	revocado, valorRevocado := tokenCon(t, admin, nil)
	vencido, _ := tokenCon(t, admin, nil)
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", revocado), map[string]any{}); r.status != 200 {
		t.Fatalf("revocar: %d %s", r.status, r.crudo)
	}
	vencer(t, vencido)

	// Filtro vigentes: solo el activo.
	if ids := idsTokens(t, admin, "&vigentes=true"); !slices.Contains(ids, activo) || slices.Contains(ids, revocado) || slices.Contains(ids, vencido) {
		t.Fatalf("vigentes: %v", ids)
	}

	// Activo: 409 «revócalo primero», sin cambios.
	r := admin.pedir(http.MethodPost, ruta(activo), map[string]any{})
	r.problema(t, 409, "token_vigente")
	if !strings.Contains(r.cuerpo["detail"].(string), "revócalo primero") {
		t.Fatalf("detalle: %s", r.crudo)
	}
	api := e.cliente()
	api.bearer = valorActivo
	if r := api.pedir(http.MethodGet, "/api/v1/yo", nil); r.status != 200 {
		t.Fatalf("el 409 no toca el token: %d", r.status)
	}

	// Sin permiso: 403 (supervisor ve tokens, no los elimina).
	_, correoSup := e.usuario("supervisor")
	sup := e.cliente()
	sup.entrar(correoSup)
	sup.pedir(http.MethodPost, ruta(revocado), map[string]any{}).problema(t, 403, "prohibido")
	// Inexistente: 404.
	admin.pedir(http.MethodPost, ruta(1<<40), map[string]any{}).problema(t, 404, "no_encontrado")

	// Revocado y vencido: 200, borrado lógico sin hash, fuera de la lista.
	for _, id := range []int64{revocado, vencido} {
		r := admin.pedir(http.MethodPost, ruta(id), map[string]any{"motivo": "limpieza"})
		if r.status != 200 || r.cuerpo["eliminadoEn"] == nil {
			t.Fatalf("eliminar %d: %d %s", id, r.status, r.crudo)
		}
	}
	if ids := idsTokens(t, admin, ""); slices.Contains(ids, revocado) || slices.Contains(ids, vencido) || !slices.Contains(ids, activo) {
		t.Fatalf("la lista no trae eliminados: %v", ids)
	}
	if n := contarSQL(t, `SELECT count(*) FROM nucleo.tokens_api WHERE id = ANY($1) AND eliminado_en IS NOT NULL AND eliminado_por IS NOT NULL AND hash IS NULL`, []int64{revocado, vencido}); n != 2 {
		t.Fatalf("borrado lógico sin hash: %d", n)
	}
	// Un token eliminado no autentica (ni por su valor ni revocándolo de nuevo).
	viejo := e.cliente()
	viejo.bearer = valorRevocado
	viejo.pedir(http.MethodGet, "/api/v1/yo", nil).problema(t, 401, "no_autenticado")
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", revocado), map[string]any{}).problema(t, 404, "no_encontrado")

	// Dos veces: 200 sin evento nuevo.
	if r := admin.pedir(http.MethodPost, ruta(revocado), map[string]any{}); r.status != 200 {
		t.Fatalf("eliminar otra vez: %d %s", r.status, r.crudo)
	}
	if n := eventos(t, "token.eliminado", "token_id", revocado); n != 1 {
		t.Fatalf("token.eliminado: %d", n)
	}
	if n := contarSQL(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'token.eliminado' AND (payload->>'token_id')::bigint = $1
		AND payload->>'motivo' = 'manual' AND payload ? 'usuario_id'`, vencido); n != 1 {
		t.Fatalf("payload de token.eliminado: %d", n)
	}
	if n := auditadas(t, "tokens.eliminar", fmt.Sprint(revocado)); n != 2 {
		t.Fatalf("auditoría (eliminar y repetir): %d", n)
	}
	if n := contarSQL(t, `SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'tokens.eliminar' AND c.entidad = 'token' AND c.entidad_id = $1 AND c.campo = 'eliminado_en'`, fmt.Sprint(vencido)); n != 1 {
		t.Fatalf("auditoria.cambios con eliminado_en: %d", n)
	}
}

// De otro usuario fuera de tu alcance de empresas: 404, como uno inexistente; el lote tampoco lo
// toca. Quien elimina: un token limitado (sin empresas.todas) de un admin de la empresa 101.
func TestEliminarTokenFueraDeAlcance(t *testing.T) {
	e := entornoEmpresas(t)
	nuevo := empresasAB(t, e)
	_, correoAdmin := nuevo("admin", 101)
	admin := e.cliente()
	admin.entrar(correoAdmin)
	bot := tokenDe(t, e, admin, "tokens.ver", "tokens.revocar", "tokens.eliminar", "usuarios.ver")
	// Integración de la empresa 102, con un token revocado (fixture como la consola).
	integB := e.integracion("Integ B", "lectura")
	ctx := pruebas.ContextoSistema(pruebas.Pool(t))
	if _, err := e.mod.AsignarEmpresaUsuario.Ejecutar(ctx, identidad.EntradaEmpresaUsuario{UsuarioID: integB, EmpresaID: 102}); err != nil {
		t.Fatal(err)
	}
	tk := admin.pedir(http.MethodPost, "/api/v1/tokens", map[string]any{"nombre": "b", "usuarioId": integB})
	if tk.status != 201 {
		t.Fatalf("token de B: %d %s", tk.status, tk.crudo)
	}
	ajeno := int64(tk.cuerpo["token"].(map[string]any)["id"].(float64))
	if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", ajeno), map[string]any{}); r.status != 200 {
		t.Fatalf("revocar: %d %s", r.status, r.crudo)
	}
	r := bot.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/eliminar", ajeno), map[string]any{})
	r.problema(t, 404, "no_encontrado")
	if strings.Contains(string(r.crudo), "102") {
		t.Fatalf("el 404 nombra la empresa del dueño: %s", r.crudo)
	}
	if r := bot.pedir(http.MethodPost, "/api/v1/tokens/eliminar-inactivos", map[string]any{}); r.status != 200 || slices.Contains(numeros(r.cuerpo["ids"]), ajeno) {
		t.Fatalf("lote del bot: %d %s", r.status, r.crudo)
	}
	if n := contarSQL(t, `SELECT count(*) FROM nucleo.tokens_api WHERE id = $1 AND eliminado_en IS NULL`, ajeno); n != 1 {
		t.Fatal("el token de B sigue sin eliminar")
	}
}

func TestEliminarTokensInactivosEnLote(t *testing.T) {
	e := nuevoEntorno(t)
	idAdmin, correoAdmin := e.usuario("admin")
	admin := e.cliente()
	admin.entrar(correoAdmin)
	activo, _ := tokenCon(t, admin, nil)
	var inactivos []int64
	for i := range 3 {
		id, _ := tokenCon(t, admin, nil)
		if i == 0 {
			vencer(t, id)
		} else if r := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/tokens/%d/revocar", id), map[string]any{}); r.status != 200 {
			t.Fatalf("revocar: %d %s", r.status, r.crudo)
		}
		inactivos = append(inactivos, id)
	}
	filtro := fmt.Sprintf("?usuario_id=%d", idAdmin)
	if r := admin.pedir(http.MethodGet, "/api/v1/tokens/eliminables"+filtro, nil); r.status != 200 || r.cuerpo["cantidad"] != float64(3) || r.cuerpo["enUso"] != float64(0) {
		t.Fatalf("eliminables: %d %s", r.status, r.crudo)
	}
	r := admin.pedir(http.MethodPost, "/api/v1/tokens/eliminar-inactivos", map[string]any{"usuarioId": idAdmin})
	if r.status != 200 || r.cuerpo["eliminados"] != float64(3) || !slices.Equal(numeros(r.cuerpo["ids"]), inactivos) {
		t.Fatalf("lote: %d %s", r.status, r.crudo)
	}
	for _, id := range inactivos {
		if n := contarSQL(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'token.eliminado' AND (payload->>'token_id')::bigint = $1 AND payload->>'motivo' = 'lote'`, id); n != 1 {
			t.Fatalf("token.eliminado (lote) de %d: %d", id, n)
		}
		if n := auditadas(t, "tokens.eliminar", fmt.Sprint(id)); n != 1 {
			t.Fatalf("auditoría por token de %d: %d", id, n)
		}
	}
	if ids := idsTokens(t, admin, ""); !slices.Contains(ids, activo) || slices.ContainsFunc(ids, func(id int64) bool { return slices.Contains(inactivos, id) }) {
		t.Fatalf("lista tras el lote: %v", ids)
	}
	if n := contarSQL(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'tokens.eliminar_inactivos' AND resultado = 'ok' AND actor_id = $1`, idAdmin); n != 1 {
		t.Fatalf("auditoría del lote: %d", n)
	}
	// Otra vez: nada que eliminar.
	if r := admin.pedir(http.MethodPost, "/api/v1/tokens/eliminar-inactivos", map[string]any{"usuarioId": idAdmin}); r.status != 200 || r.cuerpo["eliminados"] != float64(0) {
		t.Fatalf("lote vacío: %d %s", r.status, r.crudo)
	}
	// Sin permiso: 403.
	_, correoSup := e.usuario("supervisor")
	sup := e.cliente()
	sup.entrar(correoSup)
	sup.pedir(http.MethodPost, "/api/v1/tokens/eliminar-inactivos", map[string]any{}).problema(t, 403, "prohibido")
	sup.pedir(http.MethodGet, "/api/v1/tokens/eliminables", nil).problema(t, 403, "prohibido")
}

func contarSQL(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pruebas.Pool(t).QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func numeros(v any) []int64 {
	salida := []int64{}
	l, _ := v.([]any)
	for _, x := range l {
		salida = append(salida, int64(x.(float64)))
	}
	return salida
}
