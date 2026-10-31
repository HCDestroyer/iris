package soporte_test

import (
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/horus"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Revisión de seguridad de la PR #70: un ticket no puede atarse a un cliente o servicio de otra
// empresa (la copia de Argos expondría sus datos). Con la instantánea, 422 empresa_no_coincide;
// un servicio de otro cliente, 422 de validación; Horus abre el ticket sin cliente.
func TestTicketConClienteDeOtraEmpresa(t *testing.T) {
	registrarEmpresas(t, 66, 67)
	instantanea(t, 67, 6701, 670101, 6700) // cliente y servicio de la empresa 67
	instantanea(t, 66, 6601, 660101, 6600) // y de la 66
	m, _ := nuevoModulo(time.Now())
	ag := agente(t)
	asignarEmpresa(t, ag, 66)
	c66 := comoEnEmpresa(t, ag, 66, append([]nucleo.Permiso{nucleo.PermClientesVer}, permisosTickets...)...)

	_, err := m.CrearTicket.Ejecutar(c66, soporte.EntradaCrearTicket{Asunto: "Sin señal", ClienteID: ptr(int64(6701))})
	problema(t, err, 422, "empresa_no_coincide")
	_, err = m.CrearTicket.Ejecutar(c66, soporte.EntradaCrearTicket{Asunto: "Sin señal", ClienteID: ptr(int64(6601)), ServicioID: ptr(int64(670101))})
	problema(t, err, 422, "empresa_no_coincide")
	// El suyo, sí, con su copia.
	tk := deber(m.CrearTicket.Ejecutar(c66, soporte.EntradaCrearTicket{Asunto: "Sin señal", ClienteID: ptr(int64(6601)), ServicioID: ptr(int64(660101))}))(t)
	if v := deber(m.VerTicket.Ejecutar(c66, soporte.EntradaVerTicket{ID: tk.ID}))(t); v.DatosArgos == nil || v.DatosArgos.Cliente.ID != 6601 {
		t.Fatalf("copia del ticket propio: %+v", v.DatosArgos)
	}

	// Horus manda un hallazgo de la empresa 66 con un cliente de la 67: ticket sin cliente.
	regla(t, nil, "malware_emp", "equipo", "alta", true)
	h := "h-" + pruebas.Unico()
	e, p := hallazgo(ptr(int64(66)), h, "malware_emp", ptr(int64(6701)), ptr(int64(670101)), nil)
	aplicarHorus(t, m, eventoHorus(t, horus.EventoHallazgoAbierto, p))
	r, id := resultadoHorus(t, e)
	if r != "ticket_creado" || id == nil {
		t.Fatalf("hallazgo con cliente de otra empresa: %s", r)
	}
	sis := pruebas.ContextoSistema(pruebas.Pool(t))
	v := deber(m.VerTicket.Ejecutar(sis, soporte.EntradaVerTicket{ID: *id}))(t)
	if v.ClienteID != nil || v.ServicioID != nil || v.DatosArgos != nil {
		t.Fatalf("el ticket de Horus no debía quedar con el cliente de otra empresa: %+v", v)
	}
}

func deber[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}
