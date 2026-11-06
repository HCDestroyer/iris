package mensajeria_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
)

// clavesDeLista son los campos de lista de las respuestas de la bandeja: nunca null, siempre [].
var clavesDeLista = map[string]bool{"data": true, "clientes": true, "coincideEn": true, "colas": true, "contactosIris": true,
	"etiquetas": true, "menciones": true, "miembros": true, "participantes": true, "porCanal": true, "porCola": true,
	"porEtiqueta": true, "servicios": true, "usos": true, "variables": true, "vinculos": true, "etiquetaIds": true,
	"franjas": true, "feriados": true, "dias": true, "motivos": true}

func listasNulas(ruta string, v any, malas *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, h := range x {
			if clavesDeLista[k] && h == nil {
				*malas = append(*malas, ruta+"."+k)
			}
			listasNulas(ruta+"."+k, h, malas)
		}
	case []any:
		for i, h := range x {
			listasNulas(fmt.Sprintf("%s[%d]", ruta, i), h, malas)
		}
	}
}

// Las listas de la bandeja salen [] (nunca null), también vacías.
func TestConvListasNuncaNull(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.l1", "Hola")
	c := b.conv(num)
	ruta := "/conversaciones/" + cid(c)
	// Una cola sin miembros y sin conversaciones.
	b.exigir(b.api(b.supervisor, http.MethodPost, "/colas", map[string]any{"nombre": "Vacía " + numeroCliente()}), http.StatusCreated)
	rutas := []string{
		"/conversaciones?limit=50",
		"/conversaciones?estado=resuelta&limit=50",
		"/conversaciones/contadores",
		"/conversaciones/canales",
		"/conversaciones/agentes",
		"/conversaciones/cambios?cursor=0&limit=200",
		ruta,
		ruta + "/mensajes?limit=50",
		ruta + "/plantillas",
		ruta + "/coincidencias",
		"/colas",
		"/etiquetas",
		"/respuestas-rapidas",
		"/contactos-iris?q=nadie",
	}
	for _, r := range rutas {
		res := b.api(b.supervisor, http.MethodGet, r, nil)
		b.exigir(res, http.StatusOK)
		var malas []string
		listasNulas(r, res.cuerpo, &malas)
		if len(malas) > 0 {
			t.Errorf("listas null: %v", malas)
		}
	}

	// El contrato lo dice: las listas de esas respuestas no son anulables, y el 200 de un reenvío
	// trae el mismo cuerpo que el 201.
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("openapi: %d", w.Code)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Responses   map[string]struct {
				Content map[string]any `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type any `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range mensajeria.EsquemasBandeja() {
		s, ok := doc.Components.Schemas[n]
		if !ok {
			t.Errorf("esquema %s no está en el contrato", n)
			continue
		}
		for k, p := range s.Properties {
			if fmt.Sprint(p.Type) == "[array null]" {
				t.Errorf("%s.%s es array|null", n, k)
			}
		}
	}
	reenvios := map[string]bool{}
	for _, item := range doc.Paths {
		if o, ok := item["post"]; ok && mensajeria.ConReenvio(o.OperationID) {
			reenvios[o.OperationID] = true
			if len(o.Responses["200"].Content) == 0 || fmt.Sprint(o.Responses["200"].Content) != fmt.Sprint(o.Responses["201"].Content) {
				t.Errorf("%s: el 200 no declara el cuerpo del 201", o.OperationID)
			}
		}
	}
	if len(reenvios) == 0 {
		t.Fatal("sin operaciones con reenvío")
	}
}

// PR B: horarios, manuales, sugerencias y enlaces también salen con [].
func TestHorariosYManualesListasNuncaNull(t *testing.T) {
	b := nuevaBandeja(t, nil)
	h := b.horario(todosLosDias("08:00", "17:00"), []map[string]any{{"fecha": "2026-12-25", "nombre": "Navidad", "franjas": []any{}}})
	m := b.api(b.supervisor, http.MethodPost, "/manuales", map[string]any{"titulo": "Sin etiquetas", "cuerpo": "Un artículo sin etiquetas.", "visibilidad": "cliente"})
	b.exigir(m, http.StatusCreated)
	num := numeroCliente()
	b.texto(num, "wamid.lm1", "Hola")
	c := b.conv(num)
	rutas := []string{
		"/horarios",
		fmt.Sprintf("/horarios/%d", int64(h["id"].(float64))),
		"/manuales?q=nada-que-encontrar",
		"/manuales",
		fmt.Sprintf("/manuales/%d", int64(m.cuerpo["id"].(float64))),
		"/conversaciones/" + cid(c) + "/manuales-sugeridos",
		"/conversaciones/" + cid(c) + "/manual-enlaces",
	}
	for _, r := range rutas {
		res := b.api(b.supervisor, http.MethodGet, r, nil)
		b.exigir(res, http.StatusOK)
		var malas []string
		listasNulas(r, res.cuerpo, &malas)
		if len(malas) > 0 {
			t.Errorf("listas null: %v", malas)
		}
	}
}
