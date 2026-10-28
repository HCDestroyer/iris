package campo_test

import (
	"fmt"
	"net/http"
	"testing"
)

// Por HTTP: plantillas (crear, editar, ver una versión, desactivar), formularios de la orden
// (agregar y quitar con su uuid en la ruta), pausar y reanudar, y el estado en el paquete.
func TestHTTPPlantillasFormulariosYPausa(t *testing.T) {
	base := nuevoServidor(t)
	sup := entrar(t, base, "supervisor")
	d := entrar(t, base, "despacho")
	tec := entrar(t, base, "tecnico")

	items := []map[string]any{
		{"codigo": "estado", "etiqueta": "Estado del poste", "tipo": "seleccion", "obligatorio": true, "opciones": []string{"Bueno", "Malo"}},
		{"codigo": "foto", "etiqueta": "Foto", "tipo": "foto"},
	}
	d.pedir(http.MethodPost, "/api/v1/plantillas", map[string]any{"codigo": "postes", "nombre": "Postes", "items": items}).problema(t, 403, "prohibido")
	r := sup.pedir(http.MethodPost, "/api/v1/plantillas", map[string]any{"codigo": "postes_http", "nombre": "Postes", "items": items})
	if r.status != http.StatusCreated || r.cuerpo["numeroVersion"] != float64(1) {
		t.Fatalf("crear plantilla: %d %s", r.status, r.crudo)
	}
	pid := int64(r.cuerpo["id"].(float64))
	sup.pedir(http.MethodPost, "/api/v1/plantillas", map[string]any{"codigo": "MAL CODIGO", "nombre": "X", "items": items}).problema(t, 422, "validacion")
	if r := tec.pedir(http.MethodGet, "/api/v1/plantillas", nil); r.status != 200 || len(r.cuerpo["data"].([]any)) == 0 {
		t.Fatalf("el técnico ve el catálogo: %d %s", r.status, r.crudo)
	}

	// Orden en sitio del técnico, con el formulario agregado por despacho.
	r = d.pedir(http.MethodPost, "/api/v1/ordenes", map[string]any{"tipo": "visita", "direccion": "Zona 1", "zonaId": 3})
	ruta := fmt.Sprintf("/api/v1/ordenes/%v", r.cuerpo["id"])
	r = d.pedir(http.MethodPost, ruta+"/programar", map[string]any{"version": r.cuerpo["version"], "tecnicoId": tec.id, "inicio": "2026-10-28T17:00:00Z"})
	r = d.pedir(http.MethodPost, ruta+"/formularios", map[string]any{"version": r.cuerpo["version"], "plantillaId": pid})
	if r.status != 200 {
		t.Fatalf("agregar formulario: %d %s", r.status, r.crudo)
	}
	tec.pedir(http.MethodPost, ruta+"/formularios", map[string]any{"version": r.cuerpo["version"], "plantillaId": pid}).problema(t, 403, "prohibido")
	det := d.pedir(http.MethodGet, ruta, nil)
	forms := det.cuerpo["formularios"].([]any)
	if len(forms) != 2 {
		t.Fatalf("formularios: %s", det.crudo)
	}
	fid := forms[1].(map[string]any)["id"].(string)

	// Editar con uso: versión 2; la orden conserva la 1.
	r = sup.pedir(http.MethodPost, fmt.Sprintf("/api/v1/plantillas/%d/editar", pid), map[string]any{"version": 1, "nombre": "Postes v2", "items": items[:1]})
	if r.status != 200 || r.cuerpo["numeroVersion"] != float64(2) {
		t.Fatalf("editar: %d %s", r.status, r.crudo)
	}
	if v := sup.pedir(http.MethodGet, fmt.Sprintf("/api/v1/plantillas/%d?numeroVersion=1", pid), nil); v.status != 200 || len(v.cuerpo["items"].([]any)) != 2 ||
		len(v.cuerpo["versiones"].([]any)) != 2 {
		t.Fatalf("ver versión 1: %d %s", v.status, v.crudo)
	}
	if r := sup.pedir(http.MethodPost, fmt.Sprintf("/api/v1/plantillas/%d/desactivar", pid), map[string]any{"version": 2}); r.status != 200 || r.cuerpo["activa"] != false {
		t.Fatalf("desactivar: %d %s", r.status, r.crudo)
	}

	// Quitar el formulario (uuid en la ruta).
	v := det.cuerpo["version"]
	d.pedir(http.MethodPost, ruta+"/formularios/no-es-uuid/quitar", map[string]any{"version": v}).problema(t, 422, "validacion")
	r = d.pedir(http.MethodPost, ruta+"/formularios/"+fid+"/quitar", map[string]any{"version": v})
	if r.status != 200 {
		t.Fatalf("quitar: %d %s", r.status, r.crudo)
	}

	// Pausar y reanudar.
	r = tec.pedir(http.MethodPost, ruta+"/llegar", map[string]any{"version": r.cuerpo["version"]})
	if r.status != 200 {
		t.Fatalf("llegar: %d %s", r.status, r.crudo)
	}
	tec.pedir(http.MethodPost, ruta+"/pausar", map[string]any{"version": r.cuerpo["version"], "motivo": "siesta"}).problema(t, 422, "validacion")
	r = tec.pedir(http.MethodPost, ruta+"/pausar", map[string]any{"version": r.cuerpo["version"], "motivo": "cliente_ausente"})
	if r.status != 200 || r.cuerpo["estado"] != "pausada" || r.cuerpo["pausadaDesde"] != "en_sitio" {
		t.Fatalf("pausar: %d %s", r.status, r.crudo)
	}
	tec.pedir(http.MethodPost, ruta+"/cerrar", map[string]any{"version": r.cuerpo["version"]}).problema(t, 422, "orden_pausada")
	p := tec.pedir(http.MethodGet, "/api/v1/campo/paquete?fecha=2026-10-28", nil)
	if p.status != 200 || len(p.cuerpo["plantillas"].([]any)) == 0 {
		t.Fatalf("paquete: %d %s", p.status, p.crudo)
	}
	r = tec.pedir(http.MethodPost, ruta+"/reanudar", map[string]any{"version": r.cuerpo["version"]})
	if r.status != 200 || r.cuerpo["estado"] != "en_sitio" {
		t.Fatalf("reanudar: %d %s", r.status, r.crudo)
	}
	if det := d.pedir(http.MethodGet, ruta, nil); len(det.cuerpo["pausas"].([]any)) != 1 {
		t.Fatalf("pausas en el detalle: %s", det.crudo)
	}
}
