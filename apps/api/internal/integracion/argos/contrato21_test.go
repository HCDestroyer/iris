package argos_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Diagnóstico del conector (entrega 2.1): el PM veía «Argos < 1.6.0» con un Argos 1.14. Iris guarda el
// último resultado de informar_contrato (versionEstado, versionDetalle con la URL base y el status) y
// distingue el panel de Nuxt (404 HTML), el token sin permiso en el PUT y la vuelta atrás real.

const (
	rutaContratoArgos = "/api/v1/integraciones/iris/contrato"
	rutaEstadoArgos   = "/api/v1/integraciones/iris/estado"
)

// conectado114: un conector activo con Argos 1.14.0 cuya versión ya leyó el job una vez (ok).
func conectado114(t *testing.T) (*entorno, *clienteHTTP, int64) {
	t.Helper()
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.14.0" })
	admin, id, _ := e.conectado()
	r, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id)
	if err != nil || r.Estado != argos.VersionOK || r.VersionRemota == nil || *r.VersionRemota != "1.14.0" {
		t.Fatalf("lectura inicial: %+v %v", r, err)
	}
	return e, admin, id
}

func TestVersionOKConDetalle(t *testing.T) {
	e, admin, id := conectado114(t)
	con := conectorDe(t, admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil), id)
	detalle, _ := con["versionDetalle"].(string)
	if con["versionEstado"] != "ok" || !strings.Contains(detalle, "1.14.0") || !strings.Contains(detalle, e.argos.srv.URL) ||
		!strings.Contains(detalle, "HTTP 200") || con["versionConsultadaEn"] == nil || fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico]" {
		t.Fatalf("conector: %v", con)
	}
	if strings.Contains(detalle, tokenArgos) {
		t.Fatal("el detalle no lleva el token")
	}
}

// La URL base del conector es la del panel de Argos (Nuxt): /api/v1 responde 404 en HTML. No es
// «Argos < 1.6.0»: la versión ≥ 1.6.0 guardada se conserva y el detalle explica qué revisar.
func TestVersionPanelHTMLNoBorraLaVersion(t *testing.T) {
	e, admin, id := conectado114(t)
	e.argos.con(func() {
		e.argos.proxy404[rutaContratoArgos] = true
		e.argos.proxy404[rutaEstadoArgos] = true
	})
	r, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id)
	if err != nil || r.Estado != argos.VersionRutaNoExiste || r.VersionRemota == nil || *r.VersionRemota != "1.14.0" ||
		r.ContratoInformado == nil || *r.ContratoInformado != argos.ContratoIris {
		t.Fatalf("404 HTML: %+v %v", r, err)
	}
	con := conectorDe(t, admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil), id)
	detalle, _ := con["versionDetalle"].(string)
	esperado := "La dirección " + e.argos.srv.URL + " no responde como la API de Argos: revisa que sea la de la API, no la del panel"
	if con["versionEstado"] != "ruta_no_existe" || !strings.HasPrefix(detalle, esperado) || !strings.Contains(detalle, "HTTP 404") ||
		fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico]" {
		t.Fatalf("conector: %v", con)
	}
	if ok, _ := e.mod.InventarioPorTecnico(pruebas.ContextoSistema(e.pool), e.pool); !ok {
		t.Fatal("con un 404 HTML Iris sigue con tecnico (no vuelve a bodega_id)")
	}
	if n := e.contar(`SELECT count(*) FROM integracion.outbox WHERE evento = 'conector.actualizado' AND (payload->>'conector_id')::bigint = $1
		AND payload->'campos' ? 'version_estado' AND NOT payload->'campos' ? 'version_remota'`, id); n < 1 {
		t.Fatalf("conector.actualizado por version_estado: %d", n)
	}
}

// El PUT del contrato responde 403 (al token le falta el permiso): igual se lee GET …/estado.
func TestVersionPutProhibidoLeeElEstado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.14.0" })
	admin, id, _ := e.conectado()
	// Sin versión guardada (como un conector que nunca llegó al GET por el 403).
	e.exec(`UPDATE integracion.conectores SET version_remota = NULL, contrato_remoto = NULL WHERE id = $1`, id)
	e.argos.con(func() { e.argos.rutaStatus[http.MethodPut+" "+rutaContratoArgos] = http.StatusForbidden })
	antes := 0
	e.argos.con(func() { antes = e.argos.llamadas[rutaEstadoArgos] })
	r, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id)
	if err != nil || r.Estado != argos.VersionSinPermiso || r.VersionRemota == nil || *r.VersionRemota != "1.14.0" {
		t.Fatalf("403 en el PUT: %+v %v", r, err)
	}
	e.argos.con(func() {
		if e.argos.llamadas[rutaEstadoArgos] != antes+1 {
			t.Fatalf("debía leer GET …/estado: %d → %d", antes, e.argos.llamadas[rutaEstadoArgos])
		}
	})
	con := conectorDe(t, admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil), id)
	detalle, _ := con["versionDetalle"].(string)
	if con["versionEstado"] != "sin_permiso" || !strings.Contains(detalle, "PUT /api/v1/integraciones/iris/contrato") || !strings.Contains(detalle, "HTTP 403") ||
		!strings.Contains(detalle, e.argos.srv.URL) || !strings.Contains(detalle, "1.14.0") || fmt.Sprint(con["capacidades"]) != "[inventario_por_tecnico]" {
		t.Fatalf("conector: %v", con)
	}
}

// Vuelta atrás real a 1.5.x: 404 JSON (problem+json) o 405 en GET …/estado. La versión se borra.
func TestVersionVueltaAtrasReal(t *testing.T) {
	for _, caso := range []struct {
		nombre string
		poner  func(*argosFalso)
		status string
	}{
		{"404 JSON", func(a *argosFalso) { a.version = "" }, "HTTP 404"},
		{"405", func(a *argosFalso) {
			a.rutaStatus[http.MethodPut+" "+rutaContratoArgos] = http.StatusMethodNotAllowed
			a.rutaStatus[http.MethodGet+" "+rutaEstadoArgos] = http.StatusMethodNotAllowed
		}, "HTTP 405"},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			e, admin, id := conectado114(t)
			e.argos.con(func() { caso.poner(e.argos) })
			r, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id)
			if err != nil || r.Estado != argos.VersionRutaNoExiste || r.VersionRemota != nil || r.ContratoInformado != nil {
				t.Fatalf("vuelta atrás: %+v %v", r, err)
			}
			if ok, _ := e.mod.InventarioPorTecnico(pruebas.ContextoSistema(e.pool), e.pool); ok {
				t.Fatal("sin versión, Iris vuelve a bodega_id")
			}
			con := conectorDe(t, admin.pedir(http.MethodGet, "/api/v1/integraciones/conectores?limit=200", nil), id)
			detalle, _ := con["versionDetalle"].(string)
			if !strings.Contains(detalle, "anterior a 1.6.0") || !strings.Contains(detalle, caso.status) || fmt.Sprint(con["capacidades"]) != "[]" {
				t.Fatalf("conector: %v", con)
			}
		})
	}
}

// Argos caído: no_responde, la versión se conserva y el job devuelve error (River reintenta).
func TestVersionArgosCaido(t *testing.T) {
	e, _, id := conectado114(t)
	e.argos.con(func() { e.argos.caido = true })
	r, err := e.mod.InformarContrato.Ejecutar(pruebas.ContextoSistema(e.pool), id)
	if err == nil || r.Estado != argos.VersionNoResponde || r.VersionRemota == nil || *r.VersionRemota != "1.14.0" {
		t.Fatalf("caído: %+v %v", r, err)
	}
}

// POST /integraciones/conectores/{id}/revisar-version: solo admin, encola el job enseguida e
// idempotente con Idempotency-Key.
func TestRevisarVersionEncolaYa(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos.con(func() { e.argos.version = "1.14.0" })
	admin, id, _ := e.conectado()
	e.exec(`DELETE FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato'`)
	ruta := fmt.Sprintf("/api/v1/integraciones/conectores/%d/revisar-version", id)
	r := admin.pedir(http.MethodPost, ruta, nil, "Idempotency-Key", "revisar-"+pruebas.Unico())
	if r.status != http.StatusAccepted || int64(r.cuerpo["id"].(float64)) != id {
		t.Fatalf("revisar: %d %s", r.status, r.crudo)
	}
	if n := e.contar(`SELECT count(*) FROM river.river_job WHERE kind = 'integracion.argos_informar_contrato' AND (args->>'conector_id')::bigint = $1`, id); n != 1 {
		t.Fatalf("job encolado: %d", n)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'integraciones.revisar_version' AND resultado = 'ok' AND entidad_id = $1`, fmt.Sprint(id)); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	e.usuario("supervisor").pedir(http.MethodPost, ruta, nil).problema(t, http.StatusForbidden, "prohibido")
	admin.pedir(http.MethodPost, "/api/v1/integraciones/conectores/999999/revisar-version", nil).problema(t, http.StatusNotFound, "no_encontrado")
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", id), map[string]any{"version": e.contar(`SELECT version FROM integracion.conectores WHERE id = $1`, id), "motivo": "prueba"}); a.status != 200 {
		t.Fatalf("desactivar: %d %s", a.status, a.crudo)
	}
	admin.pedir(http.MethodPost, ruta, nil).problema(t, http.StatusUnprocessableEntity, "conector_apagado")
}
