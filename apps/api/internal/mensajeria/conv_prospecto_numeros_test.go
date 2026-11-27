package mensajeria_test

import (
	"net/http"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
)

// B23 (contrato con Argos 0.10.0 §3.6, P2): el chat ubica al prospecto por cualquiera de sus
// números, no solo por el principal, y solo en la empresa de la conversación. Un número que no es
// el principal queda marcado (prospectoRestringido): no se le comparte la cotización, la dirección
// ni el plan. Las coincidencias muestran los prospectos solo a quien los ve (prospectos.ver con su
// alcance) y nunca la dirección, el plan ni la cotización.
func TestConvProspectoPorCualquierNumero(t *testing.T) {
	b := nuevaBandeja(t, nil)
	zona := siguienteCliente.Add(1)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO agenda.zonas (id, empresa_id, origen, nombre) VALUES ($1, 1, 'local', 'Zona prospectos')`, zona); err != nil {
		t.Fatal(err)
	}
	// prospecto crea un prospecto abierto con sus números (el primero es el principal); sin números,
	// uno de Iris con solo prospectos.telefono.
	prospecto := func(empresa int64, zona *int64, estado string, telefono string, numeros ...string) int64 {
		t.Helper()
		var id int64
		motivo := any(nil)
		if estado == "perdido" {
			motivo = "precio"
		}
		if err := b.bd.QueryRow(b.ctx, `INSERT INTO comercial.prospectos (empresa_id, origen, nombre, direccion, telefono, zona_id, estado, motivo_perdida)
			VALUES ($1, 'argos_panel', 'Marta López', '3a calle 4-50', $2, $3, $4, $5) RETURNING id`, empresa, telefono, zona, estado, motivo).Scan(&id); err != nil {
			t.Fatal(err)
		}
		for i, n := range numeros {
			if _, err := b.bd.Exec(b.ctx, `INSERT INTO comercial.prospecto_telefonos (prospecto_id, empresa_id, posicion, numero, whatsapp, principal)
				VALUES ($1, $2, $3, $4, true, $5)`, id, empresa, i, "+"+n, i == 0); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	contacto := func(num string) map[string]any {
		t.Helper()
		return b.conv(num)["contacto"].(map[string]any)
	}
	principal, familiar, compartido, ajeno, cerrado := numeroCliente(), numeroCliente(), numeroCliente(), numeroCliente(), numeroCliente()
	marta := prospecto(1, &zona, "nuevo", "+502 "+principal[3:7]+"-"+principal[7:], principal, familiar, compartido)
	// Otro prospecto abierto de la empresa con el número compartido, sin zona ni vendedor.
	otro := prospecto(1, nil, "contactado", "", numeroCliente(), compartido)
	// De otra empresa: nunca se ve desde la empresa 1.
	prospecto(2, nil, "nuevo", "", ajeno)
	// Cerrado: no se ubica.
	prospecto(1, nil, "perdido", "", cerrado)

	// Un número que no es el principal lo ubica, marcado: sin cotización, dirección ni plan.
	b.texto(familiar, "wamid.pn1", "Hola, soy Jorge")
	if k := contacto(familiar); k["tipo"] != "prospecto" || k["prospectoId"].(float64) != float64(marta) || k["prospectoRestringido"] != true {
		t.Fatalf("número adicional: %v", k)
	}
	// El principal lo ubica sin restricción.
	b.texto(principal, "wamid.pn2", "Hola")
	if k := contacto(principal); k["tipo"] != "prospecto" || k["prospectoId"].(float64) != float64(marta) || k["prospectoRestringido"] != false {
		t.Fatalf("número principal: %v", k)
	}
	// Dos prospectos con el número (en ninguno es el principal): ninguno se asocia solo; las
	// coincidencias muestran los dos, sin ser el principal.
	b.texto(compartido, "wamid.pn3", "Buenas")
	c := b.conv(compartido)
	if k := c["contacto"].(map[string]any); k["tipo"] != "desconocido" || k["prospectoId"] != nil || k["prospectoRestringido"] != false {
		t.Fatalf("número de dos prospectos: %v", k)
	}
	ruta := "/conversaciones/" + cid(c) + "/coincidencias"
	r := b.api(b.supervisor, http.MethodGet, ruta, nil)
	b.exigir(r, http.StatusOK)
	ps := r.cuerpo["prospectos"].([]any)
	if len(ps) != 2 {
		t.Fatalf("coincidencias del supervisor: %v", r.cuerpo["prospectos"])
	}
	for _, x := range ps {
		p := x.(map[string]any)
		id := int64(p["prospectoId"].(float64))
		if (id != marta && id != otro) || p["principal"] != false || p["nombre"] != "Marta López" || p["numero"] == "" {
			t.Fatalf("coincidencia: %v", p)
		}
		for _, campo := range []string{"direccion", "planInteresId", "cotizacion", "telefono"} {
			if _, hay := p[campo]; hay {
				t.Fatalf("la coincidencia no lleva %s: %v", campo, p)
			}
		}
	}
	// Soporte no tiene prospectos.ver: no los ve.
	r = b.api(b.agente, http.MethodGet, ruta, nil)
	b.exigir(r, http.StatusOK)
	if ps := r.cuerpo["prospectos"].([]any); len(ps) != 0 {
		t.Fatalf("soporte sin prospectos.ver: %v", ps)
	}
	// Ventas con alcance por zona: solo el de su zona.
	ventas := b.persona([]int64{1}, identidad.AsignacionRol{Rol: "ventas", ZonaID: &zona})
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO mensajeria.cola_miembros (cola_id, usuario_id) VALUES ($1, $2)`, b.cola, ventas.id); err != nil {
		t.Fatal(err)
	}
	r = b.api(ventas, http.MethodGet, ruta, nil)
	b.exigir(r, http.StatusOK)
	if ps := r.cuerpo["prospectos"].([]any); len(ps) != 1 || ps[0].(map[string]any)["prospectoId"].(float64) != float64(marta) {
		t.Fatalf("ventas por zona: %v", ps)
	}

	// De otra empresa o cerrado: no se ubica ni aparece.
	for _, n := range []string{ajeno, cerrado} {
		b.texto(n, "wamid.pn-"+n, "Hola")
		c := b.conv(n)
		if k := c["contacto"].(map[string]any); k["tipo"] != "desconocido" {
			t.Fatalf("%s: %v", n, k)
		}
		r := b.api(b.supervisor, http.MethodGet, "/conversaciones/"+cid(c)+"/coincidencias", nil)
		b.exigir(r, http.StatusOK)
		if ps := r.cuerpo["prospectos"].([]any); len(ps) != 0 {
			t.Fatalf("%s: %v", n, ps)
		}
	}

	// Un prospecto de Iris (un solo número en prospectos.telefono, 8 dígitos): se ubica sin restricción.
	iris := numeroCliente()
	deIris := prospecto(1, nil, "nuevo", iris[3:7]+"-"+iris[7:])
	b.texto(iris, "wamid.pn4", "Hola")
	if k := contacto(iris); k["tipo"] != "prospecto" || k["prospectoId"].(float64) != float64(deIris) || k["prospectoRestringido"] != false {
		t.Fatalf("prospecto de Iris: %v", k)
	}

	// Con cliente en la instantánea, gana el cliente.
	conCliente := numeroCliente()
	prospecto(1, nil, "nuevo", "", numeroCliente(), conCliente)
	if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, sincronizado_en, empresas)
		VALUES ($1, 1, 'Cliente', $2, now(), '{1}')`, siguienteCliente.Add(1), "+"+conCliente); err != nil {
		t.Fatal(err)
	}
	b.texto(conCliente, "wamid.pn5", "Hola")
	if k := contacto(conCliente); k["tipo"] != "cliente" || k["prospectoId"] != nil {
		t.Fatalf("con cliente: %v", k)
	}

	// El número es de dos clientes (no se asocia solo: se pide el número de servicio) y además es un
	// número adicional de un prospecto: nunca se presenta como prospecto.
	deDos := numeroCliente()
	prospecto(1, nil, "nuevo", "", numeroCliente(), deDos)
	for _, nombre := range []string{"Cliente A", "Cliente B"} {
		if _, err := b.bd.Exec(b.ctx, `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, movil, sincronizado_en, empresas)
			VALUES ($1, 1, $2, $3, now(), '{1}')`, siguienteCliente.Add(1), nombre, "+"+deDos); err != nil {
			t.Fatal(err)
		}
	}
	b.texto(deDos, "wamid.pn6", "Hola")
	if k := contacto(deDos); k["tipo"] != "desconocido" || k["prospectoId"] != nil || k["clienteId"] != nil {
		t.Fatalf("número de dos clientes y de un prospecto: %v", k)
	}
}
