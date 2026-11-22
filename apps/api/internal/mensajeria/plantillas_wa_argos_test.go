package mensajeria_test

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// B32 (contrato Iris ↔ ArgosISP 0.13.0 §6.7): Argos lee las plantillas de WhatsApp que puede usar una
// empresa de su alcance, sin el id de la cuenta, el id de Meta ni el motivo de un rechazo.
func TestPlantillasWhatsAppParaArgos(t *testing.T) {
	e := nuevoEntorno(t, nil)
	base := time.Now().UnixNano() % 1_000_000_000
	empA, empB, empC := 5_000_000_000+base, 6_000_000_000+base, 7_000_000_000+base
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.empresas (id, nombre, origen) VALUES ($1, 'A', 'argos'), ($2, 'B', 'argos'), ($3, 'C', 'argos')`,
		empA, empB, empC); err != nil {
		t.Fatal(err)
	}
	id := identidad.Nuevo(identidad.Config{})
	r, err := id.PrepararIntegracion(e.ctx, identidad.EntradaPrepararIntegracion{Sistema: "argosisp", Empresas: []int64{empA}, Correo: pruebas.Correo("argos-pwa")})
	if err != nil {
		t.Fatal(err)
	}
	tokenA := r.Token.Valor
	wabaA, wabaB, wabaC := nuevaWaba(), nuevaWaba(), nuevaWaba()
	sufijo := fmt.Sprint(base)
	numeros := int64(0)
	canal := func(empresa int64, waba string) int64 {
		t.Helper()
		numeros++
		var id int64
		if err := e.bd.QueryRow(e.ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, waba_id, credenciales_cifradas, plantillas, usos, activo)
			VALUES ($1, 'whatsapp_cloud', 'whatsapp', $2, $3, $4, $5, '\x00', '{}', '{avisos}', true) RETURNING id`,
			empresa, "Número "+pruebas.Unico(), fmt.Sprintf("+5023%07d", (base+numeros)%10_000_000), nuevaWaba(), waba).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	plantilla := func(empresa int64, waba, nombre, categoria, estado string, codigos, usos []string) {
		t.Helper()
		var borrada *time.Time
		if estado == "DELETED" {
			ahora := time.Now()
			borrada = &ahora
		}
		if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.plantillas_whatsapp (empresa_id, waba_id, meta_id, nombre, idioma, etiqueta, categoria, estado, motivo,
				componentes, codigos, usos, origen, borrada_en)
			VALUES ($1, $2, $3, $4, 'es', 'Etiqueta', $5, $6, $7, $8::jsonb, $9, $10, 'iris', $11)`,
			empresa, waba, nuevaWaba(), nombre+"_"+sufijo, categoria, estado, "motivo_secreto",
			`{"cuerpo":"Hola {{cliente_nombre}}, tu factura vence el {{fecha_corte}}.","botones":[],"variables":[{"nombre":"cliente_nombre","ejemplo":"Ana"}]}`,
			codigos, usos, borrada); err != nil {
			t.Fatal(err)
		}
	}
	canal(empA, wabaA)
	// Una cuenta propia sin número activo, y otra con su número desconectado: no se ven.
	wabaSin, wabaDesc := nuevaWaba(), nuevaWaba()
	plantilla(empA, wabaSin, "a_cuenta_sin_numero", "UTILITY", "APPROVED", []string{"pago_recibido"}, []string{"avisos"})
	desc := canal(empA, wabaDesc)
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.canales SET desconectado_en = now() WHERE id = $1`, desc); err != nil {
		t.Fatal(err)
	}
	plantilla(empA, wabaDesc, "a_cuenta_desconectada", "UTILITY", "APPROVED", []string{"pago_recibido"}, []string{"avisos"})
	plantilla(empA, wabaA, "a_aprobada", "UTILITY", "APPROVED", []string{"factura_por_vencer", "codigo_que_no_existe"}, []string{"avisos", "conversaciones"})
	plantilla(empA, wabaA, "a_rechazada", "UTILITY", "REJECTED", []string{"pago_recibido"}, []string{"avisos"})
	plantilla(empA, wabaA, "a_migrada", "UTILITY", "APPROVED", []string{"bienvenida"}, []string{})
	plantilla(empA, wabaA, "a_solo_conversaciones", "UTILITY", "APPROVED", []string{"factura_vencida"}, []string{"conversaciones"})
	plantilla(empA, wabaA, "a_borrador", "UTILITY", "BORRADOR", []string{}, []string{"avisos"})
	plantilla(empA, wabaA, "a_borrada", "UTILITY", "DELETED", []string{}, []string{"avisos"})
	plantilla(empA, wabaA, "a_marketing", "MARKETING", "APPROVED", []string{"servicio_reactivado"}, []string{"avisos"})
	// B le presta a A un número con el uso avisos: A ve las de esa cuenta (con avisos).
	canalB := canal(empB, wabaB)
	plantilla(empB, wabaB, "b_prestada", "UTILITY", "APPROVED", []string{"corte_por_falta_de_pago"}, []string{"avisos"})
	plantilla(empB, wabaB, "b_solo_conversaciones", "UTILITY", "APPROVED", []string{}, []string{"conversaciones"})
	// Con el uso avisos pero sin marcarlo como su número de avisos: todavía no las ve.
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.canal_prestamos (canal_id, empresa_id, usos, activo, prestado_en) VALUES ($1, $2, '{avisos,soporte}', true, now())`,
		canalB, empA); err != nil {
		t.Fatal(err)
	}
	ruta0 := fmt.Sprintf("/api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=%d", empA)
	if r := e.pedir(http.MethodGet, ruta0, tokenA, "", nil); r.status != http.StatusOK || fmt.Sprint(r.cuerpo["data"]) == "" ||
		contieneNombre(r.cuerpo, "b_prestada_"+sufijo) {
		t.Fatalf("préstamo sin avisos_empresa: %v", r.cuerpo)
	}
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.canal_prestamos SET avisos_empresa = true WHERE canal_id = $1 AND empresa_id = $2`, canalB, empA); err != nil {
		t.Fatal(err)
	}
	// C le presta a A un número solo para soporte: sus plantillas no se ven.
	canalC := canal(empC, wabaC)
	plantilla(empC, wabaC, "c_sin_avisos", "UTILITY", "APPROVED", []string{"pago_recibido"}, []string{"avisos"})
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.canal_prestamos (canal_id, empresa_id, usos, activo, prestado_en) VALUES ($1, $2, '{soporte}', true, now())`,
		canalC, empA); err != nil {
		t.Fatal(err)
	}

	ruta := fmt.Sprintf("/api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=%d", empA)
	nombres := func(r respuesta) map[string]map[string]any {
		t.Helper()
		e.exigir(r, http.StatusOK)
		out := map[string]map[string]any{}
		for _, d := range r.cuerpo["data"].([]any) {
			p := d.(map[string]any)
			claves := make([]string, 0, len(p))
			for k := range p {
				claves = append(claves, k)
			}
			sort.Strings(claves)
			// Nunca la cuenta, el id de Meta ni el motivo del rechazo.
			if !slices.Equal(claves, []string{"categoria", "codigos_aviso", "estado", "idioma", "nombre", "vista_previa"}) {
				t.Fatalf("campos de más o de menos: %v", claves)
			}
			out[fmt.Sprint(p["nombre"])] = p
		}
		return out
	}
	l := nombres(e.pedir(http.MethodGet, ruta, tokenA, "", nil))
	quiero := []string{"a_aprobada", "a_rechazada", "a_migrada", "b_prestada"}
	for _, n := range quiero {
		if l[n+"_"+sufijo] == nil {
			t.Fatalf("falta %s: %v", n, l)
		}
	}
	for _, n := range []string{"a_cuenta_sin_numero", "a_cuenta_desconectada", "a_solo_conversaciones", "a_borrador", "a_borrada", "a_marketing", "b_solo_conversaciones", "c_sin_avisos"} {
		if l[n+"_"+sufijo] != nil {
			t.Fatalf("no debía verse %s", n)
		}
	}
	ap := l["a_aprobada_"+sufijo]
	if fmt.Sprint(ap["codigos_aviso"]) != "[factura_por_vencer]" || ap["estado"] != "APPROVED" || ap["idioma"] != "es" ||
		ap["vista_previa"] != "Hola {{cliente_nombre}}, tu factura vence el {{fecha_corte}}." {
		t.Fatalf("plantilla: %v", ap)
	}
	if l["a_rechazada_"+sufijo]["estado"] != "REJECTED" {
		t.Fatalf("rechazada: %v", l["a_rechazada_"+sufijo])
	}

	// Sin caché y por páginas: recorrer de a 2 da lo mismo que una página grande.
	if r := e.pedir(http.MethodGet, ruta, tokenA, "", nil); r.h.Get("Cache-Control") != "no-store" || r.cuerpo["nextCursor"] != nil {
		t.Fatalf("Cache-Control y última página: %v %v", r.h.Get("Cache-Control"), r.cuerpo["nextCursor"])
	}
	porPaginas := map[string]bool{}
	cursor, paginas := "", 0
	for {
		u := ruta + "&limit=2"
		if cursor != "" {
			u += "&cursor=" + cursor
		}
		r := e.pedir(http.MethodGet, u, tokenA, "", nil)
		e.exigir(r, http.StatusOK)
		paginas++
		for _, d := range r.cuerpo["data"].([]any) {
			porPaginas[fmt.Sprint(d.(map[string]any)["nombre"])] = true
		}
		sig, _ := r.cuerpo["nextCursor"].(string)
		if sig == "" || paginas > 20 {
			break
		}
		cursor = sig
	}
	if len(porPaginas) != len(l) || paginas < 2 {
		t.Fatalf("paginación: %d de %d en %d páginas", len(porPaginas), len(l), paginas)
	}
	e.exigir(e.pedir(http.MethodGet, ruta+"&cursor=basura", tokenA, "", nil), http.StatusUnprocessableEntity)

	// Un token de Horus con el permiso (por un rol a medida): 403, solo la integración de Argos.
	rh, err := id.PrepararIntegracion(e.ctx, identidad.EntradaPrepararIntegracion{Sistema: "horus", Empresas: []int64{empA}, Correo: pruebas.Correo("horus-pwa")})
	if err != nil {
		t.Fatal(err)
	}
	var rol int64
	if err := e.bd.QueryRow(e.ctx, `INSERT INTO nucleo.roles (codigo, nombre) VALUES ($1, 'Lee plantillas') RETURNING id`, "pwa_"+sufijo).Scan(&rol); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.roles_permisos (rol_id, permiso) VALUES ($1, 'plantillas_whatsapp.leer_integracion')`, rol); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO nucleo.usuarios_roles (usuario_id, rol_id) VALUES ($1, $2)`, rh.Usuario.ID, rol); err != nil {
		t.Fatal(err)
	}
	e.exigir(e.pedir(http.MethodGet, ruta, rh.Token.Valor, "", nil), http.StatusForbidden, "type", "urn:iris:problema:solo_integracion_argos")

	// Marketing activado por la dueña: se ve.
	if _, err := e.bd.Exec(e.ctx, `INSERT INTO mensajeria.plantillas_whatsapp_ajustes (empresa_id, marketing_permitido) VALUES ($1, true)`, empA); err != nil {
		t.Fatal(err)
	}
	if l := nombres(e.pedir(http.MethodGet, ruta, tokenA, "", nil)); l["a_marketing_"+sufijo] == nil {
		t.Fatal("con marketing activado, la de marketing se ve")
	}
	// Préstamo revocado: ya no se ven las de B.
	if _, err := e.bd.Exec(e.ctx, `UPDATE mensajeria.canal_prestamos SET activo = false, avisos_empresa = false, revocado_en = now() WHERE canal_id = $1`, canalB); err != nil {
		t.Fatal(err)
	}
	if l := nombres(e.pedir(http.MethodGet, ruta, tokenA, "", nil)); l["b_prestada_"+sufijo] != nil {
		t.Fatal("con el préstamo revocado no se ven")
	}

	// Empresa fuera del alcance del conector: 403 empresa_no_permitida (también la dueña del préstamo).
	for _, emp := range []int64{empB, 1} {
		e.exigir(e.pedir(http.MethodGet, fmt.Sprintf("/api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=%d", emp), tokenA, "", nil),
			http.StatusForbidden, "type", "urn:iris:problema:empresa_no_permitida")
	}
	// Sin empresa: 422; sin token: 401.
	e.exigir(e.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/plantillas-whatsapp", tokenA, "", nil), http.StatusUnprocessableEntity)
	e.exigir(e.pedir(http.MethodGet, ruta, "", "", nil), http.StatusUnauthorized)
	// Las plantillas de B no las ve otra empresa del token de 1 y 2 sin préstamo.
	l2 := nombres(e.pedir(http.MethodGet, "/api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=2", e.otro, "", nil))
	for n := range l2 {
		if len(n) > len(sufijo) && n[len(n)-len(sufijo):] == sufijo {
			t.Fatalf("la empresa 2 ve %s", n)
		}
	}
}

func contieneNombre(cuerpo map[string]any, nombre string) bool {
	for _, d := range cuerpo["data"].([]any) {
		if d.(map[string]any)["nombre"] == nombre {
			return true
		}
	}
	return false
}
