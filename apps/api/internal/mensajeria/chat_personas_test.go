package mensajeria_test

import (
	"net/http"
	"testing"
)

// Reporte del PM (APK 0.18.0): desde la app, un técnico no podía escribirle a soporte ni a
// despacho. La regla del servidor ya lo permite (la app usaba solo las personas que el teléfono ya
// conocía): «Nuevo mensaje» lista a toda persona activa de la empresa con chat.ver aunque no sea
// técnico ni tenga cuadrilla, y la directa se abre con ella. Esta prueba lo fija.
func TestChatTecnicoEscribeASoporteYDespacho(t *testing.T) {
	e := nuevoEntorno(t, nil)
	q := e.equipo(1)
	uno := []int64{1}
	tecnico := e.persona(uno, rolBase("tecnico"))
	e.ficha(tecnico.id, q.cuadrilla1)
	soporte := e.persona(uno, rolBase("soporte"))
	despacho := e.persona(uno, rolBase("despacho"))
	otraEmpresa := e.persona([]int64{2}, rolBase("soporte"))

	ids := func(r respuesta) map[int64]bool {
		e.exigir(r, http.StatusOK)
		vistos := map[int64]bool{}
		for _, p := range lista(r) {
			vistos[numero(p["id"])] = true
		}
		return vistos
	}
	vistos := ids(e.chat(tecnico, http.MethodGet, "/personas?limit=200", "", nil))
	if !vistos[soporte.id] || !vistos[despacho.id] {
		t.Fatalf("faltan soporte o despacho (sin cuadrilla) en personas: %v", vistos)
	}
	if vistos[otraEmpresa.id] {
		t.Fatal("personas incluye a alguien de otra empresa")
	}
	for _, p := range []persona{soporte, despacho} {
		e.exigir(e.chat(tecnico, http.MethodPost, "/directas", "", map[string]any{"usuario_id": p.id}), http.StatusCreated, "tipo", "directa")
	}
	// Y al revés: soporte también encuentra al técnico.
	if !ids(e.chat(soporte, http.MethodGet, "/personas?limit=200", "", nil))[tecnico.id] {
		t.Fatal("soporte no ve al técnico en personas")
	}
	// Alguien de otra empresa no se abre.
	e.exigir(e.chat(tecnico, http.MethodPost, "/directas", "", map[string]any{"usuario_id": otraEmpresa.id}), http.StatusUnprocessableEntity)
}
