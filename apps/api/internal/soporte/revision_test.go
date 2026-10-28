package soporte_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Pruebas de regresión de la revisión de la PR #5.

// integracion devuelve el contexto de un usuario de tipo integración (Horus, Argos) con todos
// los permisos de tickets, en modo sin conector.
func integracion(t *testing.T) context.Context {
	t.Helper()
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Horus", Correo: pruebas.Correo("horus"), Tipo: "integracion",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := pruebas.ContextoUsuario(pruebas.Pool(t), u.ID, permisosTickets...)
	ctx = contexto.ConActor(ctx, contexto.Actor{Tipo: contexto.ActorIntegracion, ID: u.ID, Nombre: "Horus"}, contexto.PermisosDe(ctx))
	return contexto.ConCanal(ctx, contexto.CanalIntegracion)
}

func TestRevisionEscalarNoIncumpleLoCumplido(t *testing.T) {
	// Lunes 2 nov 2026 09:00 en Guatemala; lentitud media: respuesta 4 h laborales.
	m, r := nuevoModulo(time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC))
	yo := agente(t)
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Lento", CategoriaID: categoria(t, "lentitud")})
	r.avanzar(2 * time.Hour)
	res, err := m.ResponderTicket.Ejecutar(como(t, yo), soporte.EntradaResponderTicket{ID: tk.ID, Cuerpo: "Revisando"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ticket.SLARespuesta.Estado != "cumplido" {
		t.Fatalf("respondió a tiempo: %s", res.Ticket.SLARespuesta.Estado)
	}
	r.avanzar(time.Minute)
	esc, err := m.EscalarTicket.Ejecutar(como(t, yo), soporte.EntradaEscalarTicket{ID: tk.ID, Version: res.Ticket.Version, Prioridad: ptr("critica"), Motivo: "Cliente empresarial"})
	if err != nil {
		t.Fatal(err)
	}
	if esc.SLARespuesta.Estado != "cumplido" || *esc.SLARespuesta.ObjetivoMin != *res.Ticket.SLARespuesta.ObjetivoMin {
		t.Fatalf("la respuesta cumplida no cambia al escalar: %+v", esc.SLARespuesta)
	}
	iguales(t, "vencimiento de respuesta", esc.SLARespuesta.VenceEn, *res.Ticket.SLARespuesta.VenceEn)
	// La resolución, que sigue corriendo, sí toma el objetivo de la nueva prioridad.
	if *esc.SLAResolucion.ObjetivoMin >= *res.Ticket.SLAResolucion.ObjetivoMin || !esc.SLAResolucion.VenceEn.Before(*res.Ticket.SLAResolucion.VenceEn) {
		t.Fatalf("la resolución se recalcula: %+v → %+v", res.Ticket.SLAResolucion, esc.SLAResolucion)
	}
}

func TestRevisionNotasInternasSoloConPermiso(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Descripcion: "No tengo internet"})
	if _, err := m.AgregarNotaInterna.Ejecutar(como(t, yo), soporte.EntradaNotaInterna{ID: tk.ID, Cuerpo: "Cliente moroso, no prometer visita"}); err != nil {
		t.Fatal(err)
	}
	dup := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{})
	if _, err := m.FusionarTickets.Ejecutar(como(t, yo), soporte.EntradaFusionarTickets{ID: dup.ID, Version: dup.Version, DestinoID: tk.ID, Motivo: ptr("Mismo cliente moroso")}); err != nil {
		t.Fatal(err)
	}
	mensajes := func(ctx context.Context) []soporte.Mensaje {
		t.Helper()
		p, err := m.ListarMensajes.Ejecutar(ctx, soporte.EntradaListarMensajes{ID: tk.ID})
		if err != nil {
			t.Fatal(err)
		}
		return p.Data
	}
	if n := len(mensajes(como(t, yo))); n != 3 {
		t.Fatalf("con tickets.ver_notas se ven las notas: %d", n)
	}
	lectura := como(t, yo, nucleo.PermTicketsVer)
	for nombre, ctx := range map[string]context.Context{
		"sin tickets.ver_notas": lectura,
		"por el portal":         contexto.ConCanal(como(t, yo), contexto.CanalPortal),
	} {
		ms := mensajes(ctx)
		if len(ms) != 1 || !ms[0].Publico {
			t.Fatalf("%s solo ve lo público: %+v", nombre, ms)
		}
	}
	// El motivo de la fusión es el texto de la nota: tampoco sale en el historial.
	motivoFusion := func(ctx context.Context) *string {
		t.Helper()
		p, err := m.ListarEventos.Ejecutar(ctx, soporte.EntradaListarEventos{ID: dup.ID})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range p.Data {
			if e.Tipo == "fusionado" {
				return e.Motivo
			}
		}
		t.Fatal("sin evento fusionado")
		return nil
	}
	if motivoFusion(lectura) != nil || motivoFusion(como(t, yo)) == nil {
		t.Fatal("el motivo de la fusión solo con tickets.ver_notas")
	}

	// Roles base: lectura no ve notas; soporte, supervisor, despacho y admin sí.
	for _, r := range nucleo.RolesBase {
		tiene := false
		for _, p := range r.PermisosDeRol() {
			tiene = tiene || p == nucleo.PermTicketsVerNotas
		}
		quiere := map[string]bool{"admin": true, "supervisor": true, "soporte": true, "despacho": true}[r.Codigo]
		if tiene != quiere {
			t.Fatalf("rol %s: tickets.ver_notas = %v", r.Codigo, tiene)
		}
	}
}

func TestRevisionAlertaTrasNuevoVencimiento(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 2, 0, 0, 0, time.UTC)
	m, r := nuevoModulo(t0)
	yo := agente(t)
	// 24/7 crítica: resolución 240 min (riesgo a los 192).
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{CategoriaID: categoria(t, "incidente_red"), Prioridad: "critica"})
	res, err := m.ResponderTicket.Ejecutar(como(t, yo), soporte.EntradaResponderTicket{ID: tk.ID, Cuerpo: "Recibido"})
	if err != nil {
		t.Fatal(err)
	}
	correr := func(d time.Duration) {
		t.Helper()
		if _, err := m.RevisarSLA.Ejecutar(contexto.Sistema(pruebas.ContextoSistema(pruebas.Pool(t)), contexto.CanalCron),
			soporte.EntradaRevisarSLA{Ahora: t0.Add(d)}); err != nil {
			t.Fatal(err)
		}
	}
	enRiesgo := func() int {
		n := 0
		for _, e := range eventosDe(t, tk.ID) {
			if e.Evento == "ticket.sla_en_riesgo" && e.Payload["sla"] == "resolucion" {
				n++
			}
		}
		return n
	}
	correr(200 * time.Minute)
	if enRiesgo() != 1 {
		t.Fatalf("primera alerta: %v", nombres(eventosDe(t, tk.ID)))
	}
	// En espera 100 min: el vencimiento se corre (riesgo a los 292, vence a los 340).
	r.avanzar(200 * time.Minute)
	esp, err := m.PonerTicketEnEspera.Ejecutar(como(t, yo), soporte.EntradaPonerEnEspera{ID: tk.ID, Version: res.Ticket.Version, Motivo: "Esperando repuesto"})
	if err != nil {
		t.Fatal(err)
	}
	r.avanzar(100 * time.Minute)
	if _, err := m.ReanudarTicket.Ejecutar(como(t, yo), soporte.EntradaReanudarTicket{ID: tk.ID, Version: esp.Version}); err != nil {
		t.Fatal(err)
	}
	correr(300 * time.Minute)
	correr(301 * time.Minute)
	if enRiesgo() != 2 {
		t.Fatalf("el nuevo plazo vuelve a alertar una vez: %d", enRiesgo())
	}
}

func TestRevisionIdempotenciaTrasReclamo(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	clave := "crear-" + pruebas.Unico()
	entrada := soporte.EntradaCrearTicket{Asunto: "Sin señal " + pruebas.Unico(), Descripcion: "Desde ayer", ClaveIdempotencia: clave}

	// La clave se reclamó (el proceso cayó tras el COMMIT) y la petición se ejecuta de nuevo.
	a := crear(t, m, como(t, yo), entrada)
	b := crear(t, m, como(t, yo), entrada)
	if a.ID != b.ID || len(eventosDe(t, a.ID)) != 1 {
		t.Fatalf("un solo ticket: %d %d %v", a.ID, b.ID, nombres(eventosDe(t, a.ID)))
	}
	// Las claves son por credencial: otro usuario con la misma clave crea el suyo.
	if c := crear(t, m, como(t, agente(t)), entrada); c.ID == a.ID {
		t.Fatal("la clave de otro usuario no devuelve este ticket")
	}

	claveResp, claveNota := "resp-"+pruebas.Unico(), "nota-"+pruebas.Unico()
	for range 2 {
		if _, err := m.ResponderTicket.Ejecutar(como(t, yo), soporte.EntradaResponderTicket{ID: a.ID, Cuerpo: "Vamos", ClaveIdempotencia: claveResp}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.AgregarNotaInterna.Ejecutar(como(t, yo), soporte.EntradaNotaInterna{ID: a.ID, Cuerpo: "Ojo", ClaveIdempotencia: claveNota}); err != nil {
			t.Fatal(err)
		}
	}
	p, err := m.ListarMensajes.Ejecutar(como(t, yo), soporte.EntradaListarMensajes{ID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Data) != 3 || contar(eventosDe(t, a.ID), "ticket.respondido") != 1 {
		t.Fatalf("descripción, respuesta y nota una vez: %d mensajes, %v", len(p.Data), nombres(eventosDe(t, a.ID)))
	}
}

func TestRevisionTicketPadre(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	padre := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Nodo caído", ZonaID: ptr(int64(7))})

	// Quien no ve el maestro recibe el mismo 422 que si no existiera.
	zona3 := map[string][]contexto.Alcance{}
	for _, p := range permisosTickets {
		zona3[string(p)] = []contexto.Alcance{{ZonaID: 3}}
	}
	_, err := m.CrearTicket.Ejecutar(conAlcance(t, yo, zona3), soporte.EntradaCrearTicket{Asunto: "Hijo", ZonaID: ptr(int64(3)), TicketPadreID: &padre.ID})
	problema(t, err, 422, "ticket_padre_invalido")

	// El maestro se lee con FOR SHARE: mientras otro lo modifica (p. ej. lo cierra: un UPDATE
	// toma FOR NO KEY UPDATE, que la FK sola no espera), crear el hijo espera.
	tx, err := pruebas.Pool(t).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(), "SELECT 1 FROM soporte.tickets WHERE id = $1 FOR NO KEY UPDATE", padre.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancelar := context.WithTimeout(como(t, yo), 500*time.Millisecond)
	defer cancelar()
	if _, err := m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Hijo", TicketPadreID: &padre.ID}); err == nil {
		t.Fatal("crear el hijo debía esperar el candado del maestro")
	}
	_ = tx.Rollback(context.Background())
	hijo := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Hijo", TicketPadreID: &padre.ID})
	if hijo.TicketPadreID == nil || *hijo.TicketPadreID != padre.ID {
		t.Fatalf("hijo: %+v", hijo.TicketPadreID)
	}
}

func TestRevisionAsignarValidaAlAsignado(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{ZonaID: ptr(int64(3))})
	asignar := func(usuario int64, cuadrilla *int64) (soporte.Ticket, error) {
		actual, err := m.VerTicket.Ejecutar(como(t, yo), soporte.EntradaVerTicket{ID: tk.ID})
		if err != nil {
			t.Fatal(err)
		}
		return m.AsignarTicket.Ejecutar(como(t, yo), soporte.EntradaAsignarTicket{ID: tk.ID, Version: actual.Version, UsuarioID: &usuario, CuadrillaID: cuadrilla})
	}
	invalido := func(usuario int64, cuadrilla *int64, que string) {
		t.Helper()
		_, err := asignar(usuario, cuadrilla)
		var p *nucleo.Problema
		if !errors.As(err, &p) || p.Status != 422 || len(p.Errors) != 1 || p.Errors[0].Field != "usuarioId" {
			t.Fatalf("%s: %v", que, err)
		}
	}
	horus, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Horus", Correo: pruebas.Correo("horus"), Tipo: "integracion", Roles: []identidad.AsignacionRol{{Rol: "soporte"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	invalido(horus.ID, nil, "una integración")
	invalido(usuarioCon(t), nil, "sin tickets.ver")
	invalido(usuarioCon(t, identidad.AsignacionRol{Rol: "lectura", ZonaID: ptr(int64(5))}), nil, "otra zona")
	invalido(usuarioCon(t, identidad.AsignacionRol{Rol: "lectura", CuadrillaID: ptr(int64(44))}), ptr(int64(45)), "otra cuadrilla")

	deZona := usuarioCon(t, identidad.AsignacionRol{Rol: "lectura", ZonaID: ptr(int64(3))})
	if a, err := asignar(deZona, nil); err != nil || *a.AsignadoA != deZona {
		t.Fatalf("de la zona del ticket: %v", err)
	}
	deCuadrilla := usuarioCon(t, identidad.AsignacionRol{Rol: "lectura", CuadrillaID: ptr(int64(44))})
	if _, err := asignar(deCuadrilla, ptr(int64(44))); err != nil {
		t.Fatalf("de la cuadrilla asignada: %v", err)
	}
}

func TestRevisionFusionarActualizaDestino(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	destino := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Original", ZonaID: ptr(int64(3))})
	dup := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Duplicado", ZonaID: ptr(int64(3))})

	// Sin acceso a los tickets, la fusión responde sin esperar sus candados.
	tx, err := pruebas.Pool(t).Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(context.Background(), "SELECT 1 FROM soporte.tickets WHERE id = ANY($1) FOR UPDATE", []int64{destino.ID, dup.ID}); err != nil {
		t.Fatal(err)
	}
	zona9 := map[string][]contexto.Alcance{}
	for _, p := range permisosTickets {
		zona9[string(p)] = []contexto.Alcance{{ZonaID: 9}}
	}
	ctx, cancelar := context.WithTimeout(conAlcance(t, yo, zona9), 2*time.Second)
	defer cancelar()
	_, err = m.FusionarTickets.Ejecutar(ctx, soporte.EntradaFusionarTickets{ID: dup.ID, Version: dup.Version, DestinoID: destino.ID})
	problema(t, err, 404, "no_encontrado")
	_ = tx.Rollback(context.Background())

	res, err := m.FusionarTickets.Ejecutar(como(t, yo), soporte.EntradaFusionarTickets{ID: dup.ID, Version: dup.Version, DestinoID: destino.ID})
	if err != nil {
		t.Fatal(err)
	}
	visto, err := m.VerTicket.Ejecutar(como(t, yo), soporte.EntradaVerTicket{ID: destino.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Destino.Version != destino.Version+1 || visto.Version != res.Destino.Version || !res.Destino.ActualizadoEn.Equal(visto.ActualizadoEn) {
		t.Fatalf("el destino sube su versión y se devuelve actualizado: %d → %d (%d)", destino.Version, res.Destino.Version, visto.Version)
	}
}

func TestRevisionOrigenSegunActor(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	for _, origen := range []string{"horus", "argos", "correo", "telegram", "portal"} {
		_, err := m.CrearTicket.Ejecutar(como(t, yo), soporte.EntradaCrearTicket{Asunto: "Falso", Origen: origen})
		problema(t, err, 422, "origen_no_permitido")
	}
	if tk := crear(t, m, contexto.ConCanal(como(t, yo), contexto.CanalPortal), soporte.EntradaCrearTicket{}); tk.Origen != "portal" {
		t.Fatalf("por el portal: %s", tk.Origen)
	}
	if tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Origen: "panel"}); tk.Origen != "panel" {
		t.Fatalf("panel: %s", tk.Origen)
	}
	_, err := m.CrearTicket.Ejecutar(integracion(t), soporte.EntradaCrearTicket{Asunto: "Portal falso", Origen: "portal"})
	problema(t, err, 422, "origen_no_permitido")
	if tk := crear(t, m, integracion(t), soporte.EntradaCrearTicket{Origen: "telegram"}); tk.Origen != "telegram" {
		t.Fatalf("integración: %s", tk.Origen)
	}
}
