package mensajeria_test

import (
	"net/http"
	"testing"
)

// Bug del PM (0.31.0): asociar un cliente de Argos sin servicios daba 422 cliente_fuera_de_empresa
// aunque la ficha (GET /clientes/{id}) y AsegurarInstantanea lo aceptaban. Argos confirma que no
// tiene servicios (lista consolidada vacía): la instantánea queda con empresa_id 0, empresas vacío y
// sin_servicios, y como en Argos es de cualquier empresa. clienteEnEmpresa no miraba sin_servicios.
// Un cliente que no está en la instantánea después de pedírsela a Argos tampoco es «fuera de la
// empresa»: Argos no respondió (503 con «Reintentar»).
func TestConvAsociarClienteSinServicios(t *testing.T) {
	b := nuevaBandeja(t, nil)
	num := numeroCliente()
	b.texto(num, "wamid.as1", "Hola")
	ruta := "/conversaciones/" + cid(b.conv(num)) + "/asociar-cliente"
	sembrar := func(empresa int64, empresas string, sinServicios bool) int64 {
		t.Helper()
		id := siguienteCliente.Add(1)
		if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en, empresas, sin_servicios)
			VALUES ($1, $2, 'Cliente nuevo', now(), $3::bigint[], $4)`, id, empresa, empresas, sinServicios); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// Sin empresa y sin la confirmación de que no tiene servicios: no se sabe de qué empresa es.
	dudoso := sembrar(0, "{}", false)
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"version": ver(b.conv(num)), "clienteId": dudoso}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_fuera_de_empresa")
	// De otra empresa: sigue siendo 422.
	ajeno := sembrar(2, "{2}", false)
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"version": ver(b.conv(num)), "clienteId": ajeno}),
		http.StatusUnprocessableEntity, "type", "urn:iris:problema:cliente_fuera_de_empresa")
	// Argos no dejó la instantánea (caído o en espera): 503, no «fuera de la empresa».
	sinLeer := siguienteCliente.Add(1)
	b.exigir(b.api(b.agente, http.MethodPost, ruta, map[string]any{"version": ver(b.conv(num)), "clienteId": sinLeer}),
		http.StatusServiceUnavailable, "type", "urn:iris:problema:argos_no_disponible")
	// Sin servicios en Argos: se asocia (misma regla que la ficha y AsegurarInstantanea).
	nuevo := sembrar(0, "{}", true)
	r := b.api(b.agente, http.MethodPost, ruta, map[string]any{"version": ver(b.conv(num)), "clienteId": nuevo})
	b.exigir(r, http.StatusOK)
	if k := r.cuerpo["contacto"].(map[string]any); k["tipo"] != "cliente" || k["clienteId"].(float64) != float64(nuevo) {
		t.Fatalf("cliente sin servicios asociado: %v", k)
	}
}
