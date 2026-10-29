package argos_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Respuestas reales de Argos 1.3.0 (prueba e2e, infra/e2e/argos.sh), recortadas: las coordenadas
// vienen como texto "lat,lng" (contrato §3.2). Antes Iris solo aceptaba un objeto y toda ficha,
// búsqueda o lista de servicios con coordenadas fallaba con 502 «Respuesta ilegible».
func TestCoordenadasDeArgos13(t *testing.T) {
	var c argos.ClienteArgos
	if err := conector.Decodificar([]byte(`{"id":1,"codigo":"C-000001","codigo_externo":null,"nombre":"Ana Pérez E2E",
		"telefono":"55551234","direccion":"Zona 1, Guatemala","coordenadas":"14.6349,-90.5069","zona_id":1,
		"campos_personalizados":{},"codigos_referido":[],"externo":null,"version":1}`), &c); err != nil {
		t.Fatalf("cliente: %v", err)
	}
	if lat, lon := c.LatLon(); lat == nil || lon == nil || *lat != 14.6349 || *lon != -90.5069 || c.ID != 1 || c.Nombre != "Ana Pérez E2E" {
		t.Fatalf("cliente: %+v %v %v", c, lat, lon)
	}
	var s argos.ServicioArgos
	if err := conector.Decodificar([]byte(`{"id":3,"cliente_id":1,"empresa_id":1,"router_id":1,"plan_id":2,"estado":"activo",
		"direccion":"Zona 1","coordenadas":" 14.6349 , -90.5069 ","planta":null,"excepciones":{},"plan_nombre":"Plan 30 v4 E2E"}`), &s); err != nil {
		t.Fatalf("servicio: %v", err)
	}
	if lat, lon := s.LatLon(); lat == nil || *lat != 14.6349 || *lon != -90.5069 || s.EmpresaID != 1 {
		t.Fatalf("servicio: %+v %v %v", s, lat, lon)
	}
	// Siguen valiendo el objeto, null, "" y un texto que no son coordenadas (sin mapa, sin error).
	for cuerpo, conCoord := range map[string]bool{
		`{"id":1,"coordenadas":{"lat":"14.5","lon":-90.1}}`:        true,
		`{"id":1,"coordenadas":{"latitud":14.5,"longitud":-90.1}}`: true,
		`{"id":1,"coordenadas":null}`:                              false,
		`{"id":1,"coordenadas":""}`:                                false,
		`{"id":1,"coordenadas":"sin dato"}`:                        false,
		`{"id":1,"coordenadas":"95.0,10.0"}`:                       false,
		`{"id":1,"lat":14.5,"lon":-90.1}`:                          true,
	} {
		var c argos.ClienteArgos
		if err := conector.Decodificar([]byte(cuerpo), &c); err != nil {
			t.Fatalf("%s: %v", cuerpo, err)
		}
		if lat, lon := c.LatLon(); (lat != nil && lon != nil) != conCoord {
			t.Errorf("%s: coordenadas %v %v", cuerpo, lat, lon)
		}
	}
}

// Argos 1.3.0 no tiene routers ni zonas «de una empresa»: devuelve empresa_sugerida_id (o null =
// la usan todas). La instantánea guarda esa empresa, o NULL.
func TestEmpresaSugeridaDeArgos13(t *testing.T) {
	var r argos.RouterArgos
	if err := conector.Decodificar([]byte(`{"id":1,"nombre":"RB-E2E","host":"192.0.2.10","empresa_sugerida_id":1,"zona_id":1,"activo":true}`), &r); err != nil {
		t.Fatal(err)
	}
	var sin argos.RouterArgos
	if err := conector.Decodificar([]byte(`{"id":2,"nombre":"RB-2","empresa_sugerida_id":null}`), &sin); err != nil {
		t.Fatal(err)
	}
	var z argos.ZonaArgos
	if err := conector.Decodificar([]byte(`{"id":1,"nombre":"Zona Centro","empresa_sugerida_id":3,"version":1}`), &z); err != nil {
		t.Fatal(err)
	}
	var vieja argos.ZonaArgos
	if err := conector.Decodificar([]byte(`{"id":2,"nombre":"Zona","empresa_id":4,"empresa_sugerida_id":5}`), &vieja); err != nil {
		t.Fatal(err)
	}
	if r.Empresa() == nil || *r.Empresa() != 1 || sin.Empresa() != nil || z.Empresa() == nil || *z.Empresa() != 3 || *vieja.Empresa() != 4 {
		t.Fatalf("empresas: router %v, sin %v, zona %v, con empresa_id %v", r.Empresa(), sin.Empresa(), z.Empresa(), vieja.Empresa())
	}
}
