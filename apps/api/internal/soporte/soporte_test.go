package soporte_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"

	auditoriabd "github.com/HCDestroyer/iris/apps/api/internal/auditoria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

func TestMain(m *testing.M) { os.Exit(pruebas.Correr(m)) }

// reloj controlable para el módulo.
type reloj struct {
	mu sync.Mutex
	t  time.Time
}

func (r *reloj) ahora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *reloj) avanzar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

func nuevoModulo(inicio time.Time) (*soporte.Modulo, *reloj) {
	r := &reloj{t: inicio}
	return soporte.Nuevo(soporte.Config{Reloj: r.ahora}), r
}

var permisosTickets = []nucleo.Permiso{
	nucleo.PermTicketsVer, nucleo.PermTicketsCrear, nucleo.PermTicketsAsignar, nucleo.PermTicketsResponder,
	nucleo.PermTicketsNotaInterna, nucleo.PermTicketsEscalar, nucleo.PermTicketsPonerEnEspera,
	nucleo.PermTicketsReanudar, nucleo.PermTicketsResolver, nucleo.PermTicketsReabrir,
	nucleo.PermTicketsCerrar, nucleo.PermTicketsFusionar, nucleo.PermTicketsVerNotas,
}

// empresaPrueba es la empresa activa de los contextos de prueba (la fija el middleware de
// empresa activa de I1 en producción).
const empresaPrueba int64 = 1

var ident = identidad.Nuevo(identidad.Config{})

// agente crea un usuario real con el rol soporte (los mensajes y la asignación lo referencian).
func agente(t *testing.T) int64 {
	t.Helper()
	return usuarioCon(t, identidad.AsignacionRol{Rol: "soporte"})
}

// usuarioCon crea un usuario persona con los roles dados.
func usuarioCon(t *testing.T, roles ...identidad.AsignacionRol) int64 {
	t.Helper()
	u, err := ident.CrearUsuario.Ejecutar(pruebas.ContextoSistema(pruebas.Pool(t)), identidad.EntradaCrearUsuario{
		Nombre: "Agente", Correo: pruebas.Correo("agente"), Contrasena: "contrasena-larga-1", Roles: roles,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// como devuelve un contexto nuevo (request id propio) del usuario con todos los permisos de
// tickets, o con los dados, en la empresa de prueba.
func como(t *testing.T, id int64, permisos ...nucleo.Permiso) context.Context {
	if len(permisos) == 0 {
		permisos = permisosTickets
	}
	return contexto.ConEmpresa(pruebas.ContextoUsuario(pruebas.Pool(t), id, permisos...), empresaPrueba)
}

// conAlcance: el usuario con permisos limitados (permiso → alcances).
func conAlcance(t *testing.T, id int64, mapa map[string][]contexto.Alcance) context.Context {
	ctx := como(t, id)
	return contexto.ConActor(ctx, contexto.ActorDe(ctx), contexto.NuevosPermisos(mapa))
}

func categoria(t *testing.T, codigo string) *int64 {
	t.Helper()
	var id int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT id FROM soporte.categorias WHERE codigo = $1 AND empresa_id IS NULL", codigo).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return &id
}

func problema(t *testing.T, err error, status int, codigo string) {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) || p.Status != status || p.Codigo() != codigo {
		t.Fatalf("esperaba %d %s, llegó %v", status, codigo, err)
	}
}

type evento struct {
	Evento  string
	Empresa *int64
	Payload map[string]any
}

// eventosDe devuelve los eventos del outbox de un ticket, en orden.
func eventosDe(t *testing.T, ticketID int64) []evento {
	t.Helper()
	filas, err := pruebas.Pool(t).Query(context.Background(),
		"SELECT evento, empresa_id, payload FROM integracion.outbox WHERE (payload->>'ticket_id')::bigint = $1 ORDER BY id", ticketID)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var salida []evento
	for filas.Next() {
		var e evento
		var crudo []byte
		if err := filas.Scan(&e.Evento, &e.Empresa, &crudo); err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(crudo, &e.Payload)
		salida = append(salida, e)
	}
	return salida
}

func nombres(es []evento) []string {
	salida := make([]string, 0, len(es))
	for _, e := range es {
		salida = append(salida, e.Evento)
	}
	return salida
}

func contar(es []evento, nombre string) int {
	n := 0
	for _, e := range es {
		if e.Evento == nombre {
			n++
		}
	}
	return n
}

func iguales(t *testing.T, que string, got *time.Time, espera time.Time) {
	t.Helper()
	if got == nil || !got.Equal(espera) {
		t.Fatalf("%s: %v, esperaba %s", que, got, espera.UTC())
	}
}

// crear abre un ticket como el usuario dado.
func crear(t *testing.T, m *soporte.Modulo, ctx context.Context, e soporte.EntradaCrearTicket) soporte.Ticket {
	t.Helper()
	if e.Asunto == "" {
		e.Asunto = "Sin internet en casa"
	}
	tk, err := m.CrearTicket.Ejecutar(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func TestCrearAuditaYEmite(t *testing.T) {
	m, _ := nuevoModulo(hora(2026, 10, 30, 15, 0))
	yo := agente(t)
	ctx := como(t, yo)
	nombre, correo := "Ana López", "ANA@correo.gt"
	tk := crear(t, m, ctx, soporte.EntradaCrearTicket{
		ClienteID: ptr(int64(501)), ServicioID: ptr(int64(9001)), ZonaID: ptr(int64(3)), CategoriaID: categoria(t, "lentitud"),
		Contacto: &soporte.EntradaContacto{Nombre: &nombre, Correo: &correo}, Descripcion: "Va lento desde ayer",
	})
	if tk.Estado != "nuevo" || tk.Prioridad != "media" || tk.Origen != "panel" || tk.Version != 1 || len(tk.Numero) < 9 || tk.Numero[:3] != "TK-" {
		t.Fatalf("ticket: %+v", tk)
	}
	if tk.Contacto.Correo == nil || *tk.Contacto.Correo != "ana@correo.gt" {
		t.Fatalf("correo normalizado: %v", tk.Contacto.Correo)
	}

	// Auditoría en la misma transacción, con los cambios de creación.
	acciones, _ := auditoriabd.New(pruebas.Pool(t)).AccionesPorRequest(context.Background(), contexto.RequestIDDe(ctx))
	if len(acciones) != 1 || acciones[0].Accion != "tickets.crear" || acciones[0].Resultado != "ok" ||
		*acciones[0].Entidad != "ticket" || acciones[0].ActorID == nil || *acciones[0].ActorID != yo {
		t.Fatalf("auditoría: %+v", acciones)
	}
	cambios, _ := auditoriabd.New(pruebas.Pool(t)).CambiosDeAccion(context.Background(), acciones[0].ID)
	if len(cambios) == 0 {
		t.Fatal("la creación registra sus campos en auditoria.cambios")
	}

	es := eventosDe(t, tk.ID)
	if len(es) != 1 || es[0].Evento != "ticket.creado" {
		t.Fatalf("eventos: %v", nombres(es))
	}
	p := es[0].Payload
	if p["cliente_id"] != float64(501) || p["servicio_id"] != float64(9001) || p["categoria"] != "lentitud" ||
		p["prioridad"] != "media" || p["origen"] != "panel" || p["empresa_id"] != float64(empresaPrueba) {
		t.Fatalf("payload de ticket.creado: %v", p)
	}

	// La descripción es el primer mensaje público; el historial registra la creación.
	msjs, err := m.ListarMensajes.Ejecutar(ctx, soporte.EntradaListarMensajes{ID: tk.ID})
	if err != nil || len(msjs.Data) != 1 || !msjs.Data[0].Publico || msjs.Data[0].Cuerpo != "Va lento desde ayer" {
		t.Fatalf("mensajes: %+v %v", msjs, err)
	}
	hist, err := m.ListarEventos.Ejecutar(ctx, soporte.EntradaListarEventos{ID: tk.ID})
	if err != nil || len(hist.Data) != 1 || hist.Data[0].Tipo != "creado" {
		t.Fatalf("historial: %+v %v", hist, err)
	}
}

func TestCrearValida(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	ctx := como(t, agente(t))
	_, err := m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "  "})
	problema(t, err, 422, "validacion")
	_, err = m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Servicio sin cliente", ServicioID: ptr(int64(1))})
	problema(t, err, 422, "validacion")
	_, err = m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Categoría rara", CategoriaID: ptr(int64(999999))})
	problema(t, err, 422, "validacion")
	_, err = m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Padre inexistente", TicketPadreID: ptr(int64(999999))})
	problema(t, err, 422, "ticket_padre_invalido")
	_, err = m.CrearTicket.Ejecutar(ctx, soporte.EntradaCrearTicket{Asunto: "Prioridad", Prioridad: "urgente"})
	problema(t, err, 422, "validacion")

	// El error también queda auditado.
	acciones, _ := auditoriabd.New(pruebas.Pool(t)).AccionesPorRequest(context.Background(), contexto.RequestIDDe(ctx))
	if len(acciones) == 0 || acciones[len(acciones)-1].Resultado != "error" {
		t.Fatalf("el error se audita: %+v", acciones)
	}
}

func TestSLAEnHorarioLaboral(t *testing.T) {
	// Viernes 15:00 en Guatemala, SLA estándar (laboral), prioridad media: 240 / 1440 min.
	m, r := nuevoModulo(hora(2026, 10, 30, 15, 0))
	yo := agente(t)
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{CategoriaID: categoria(t, "lentitud")})
	if tk.SLARespuesta.ObjetivoMin == nil || *tk.SLARespuesta.ObjetivoMin != 240 || *tk.SLAResolucion.ObjetivoMin != 1440 {
		t.Fatalf("objetivos: %+v %+v", tk.SLARespuesta, tk.SLAResolucion)
	}
	// Respuesta: viernes 2 h + sábado 2 h → sábado 10:00. Riesgo (192 min) → sábado 09:12.
	iguales(t, "vence respuesta", tk.SLARespuesta.VenceEn, hora(2026, 10, 31, 10, 0))
	iguales(t, "riesgo respuesta", tk.SLARespuesta.RiesgoEn, hora(2026, 10, 31, 9, 12))
	// Resolución: 24 h laborales → viernes 2 + sábado 4 + lunes 9 + martes 9 → martes 17:00.
	iguales(t, "vence resolución", tk.SLAResolucion.VenceEn, hora(2026, 11, 3, 17, 0))
	if tk.SLARespuesta.Estado != "en_tiempo" {
		t.Fatalf("estado del reloj: %s", tk.SLARespuesta.Estado)
	}

	// Responde a los 30 min (cumple) y pasa a abierto.
	r.avanzar(30 * time.Minute)
	res, err := m.ResponderTicket.Ejecutar(como(t, yo), soporte.EntradaResponderTicket{ID: tk.ID, Cuerpo: "Lo revisamos"})
	if err != nil || res.Ticket.Estado != "abierto" || res.Ticket.SLARespuesta.Estado != "cumplido" || !res.Mensaje.Publico {
		t.Fatalf("responder: %+v %v", res, err)
	}

	// En espera a las 16:00: la resolución se pausa (sin vencimiento).
	r.avanzar(30 * time.Minute)
	tk, err = m.PonerTicketEnEspera.Ejecutar(como(t, yo), soporte.EntradaPonerEnEspera{ID: tk.ID, Version: res.Ticket.Version, Motivo: "Esperamos al cliente"})
	if err != nil || tk.Estado != "en_espera" || tk.SLAResolucion.Estado != "pausado" || tk.SLAResolucion.VenceEn != nil {
		t.Fatalf("en espera: %+v %v", tk, err)
	}

	// Reanuda el lunes a las 10:00: 7 h laborales en pausa (viernes 1 + sábado 4 + lunes 2).
	r.t = hora(2026, 11, 2, 10, 0)
	tk, err = m.ReanudarTicket.Ejecutar(como(t, yo), soporte.EntradaReanudarTicket{ID: tk.ID, Version: tk.Version})
	if err != nil || tk.Estado != "abierto" {
		t.Fatalf("reanudar: %+v %v", tk, err)
	}
	// 24 h + 7 h laborales desde el viernes 15:00 → miércoles 15:00.
	iguales(t, "vence resolución tras la pausa", tk.SLAResolucion.VenceEn, hora(2026, 11, 4, 15, 0))
	var pausa int64
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT pausa_seg FROM soporte.tickets WHERE id = $1", tk.ID).Scan(&pausa); err != nil || pausa != 7*3600 {
		t.Fatalf("pausa_seg: %d %v", pausa, err)
	}
	es := nombres(eventosDe(t, tk.ID))
	if contar(eventosDe(t, tk.ID), "ticket.estado_cambiado") != 3 {
		t.Fatalf("eventos: %v", es)
	}
}

func TestSLA24x7EscalarYReabrir(t *testing.T) {
	t0 := time.Date(2026, 12, 25, 3, 0, 0, 0, time.UTC) // feriado y de madrugada: 24/7 no lo mira
	m, r := nuevoModulo(t0)
	yo := agente(t)
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{CategoriaID: categoria(t, "sin_servicio")})
	iguales(t, "respuesta 24/7 (media 60 min)", tk.SLARespuesta.VenceEn, t0.Add(60*time.Minute))
	iguales(t, "resolución 24/7 (media 1440 min)", tk.SLAResolucion.VenceEn, t0.Add(1440*time.Minute))

	// Escalar a crítica recalcula desde el inicio: 15 / 240 min.
	r.avanzar(5 * time.Minute)
	esc, err := m.EscalarTicket.Ejecutar(como(t, yo), soporte.EntradaEscalarTicket{ID: tk.ID, Version: tk.Version, Prioridad: ptr("critica"), Motivo: "Cliente corporativo"})
	if err != nil || esc.Prioridad != "critica" || esc.NivelEscalamiento != 1 || esc.Estado != "nuevo" {
		t.Fatalf("escalar: %+v %v", esc, err)
	}
	iguales(t, "respuesta crítica", esc.SLARespuesta.VenceEn, t0.Add(15*time.Minute))
	iguales(t, "resolución crítica", esc.SLAResolucion.VenceEn, t0.Add(240*time.Minute))
	_, err = m.EscalarTicket.Ejecutar(como(t, yo), soporte.EntradaEscalarTicket{ID: tk.ID, Version: esc.Version, Prioridad: ptr("alta"), Motivo: "Bajar"})
	problema(t, err, 422, "prioridad_no_mayor")
	es := eventosDe(t, tk.ID)
	if es[len(es)-1].Evento != "ticket.escalado" || es[len(es)-1].Payload["a"] != "critica" || es[len(es)-1].Payload["nivel"] != float64(1) {
		t.Fatalf("ticket.escalado: %+v", es[len(es)-1])
	}

	// Resolver a la hora y reabrir dos horas después: esas dos horas son pausa.
	r.t = t0.Add(time.Hour)
	res, err := m.ResolverTicket.Ejecutar(como(t, yo), soporte.EntradaResolverTicket{ID: tk.ID, Version: esc.Version, Solucion: "Se reinició la ONU"})
	if err != nil || res.Estado != "resuelto" || res.SLAResolucion.Estado != "cumplido" || res.SLARespuesta.Estado != "incumplido" {
		t.Fatalf("resolver: %+v %v", res, err)
	}
	r.t = t0.Add(3 * time.Hour)
	ab, err := m.ReabrirTicket.Ejecutar(como(t, yo), soporte.EntradaReabrirTicket{ID: tk.ID, Version: res.Version, Motivo: "Volvió a fallar"})
	if err != nil || ab.Estado != "abierto" || ab.SLAResolucion.CumplidoEn != nil {
		t.Fatalf("reabrir: %+v %v", ab, err)
	}
	iguales(t, "resolución tras reabrir (+2 h)", ab.SLAResolucion.VenceEn, t0.Add(240*time.Minute+2*time.Hour))
}

func TestMaquinaDeEstados(t *testing.T) {
	m, r := nuevoModulo(time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC))
	yo := agente(t)
	otro := agente(t)
	ctx := func() context.Context { return como(t, yo) }

	tk := crear(t, m, ctx(), soporte.EntradaCrearTicket{})
	// Desde nuevo no se pone en espera, no se reanuda ni se reabre.
	_, err := m.PonerTicketEnEspera.Ejecutar(ctx(), soporte.EntradaPonerEnEspera{ID: tk.ID, Version: tk.Version, Motivo: "Nada"})
	problema(t, err, 422, "transicion_invalida")
	_, err = m.ReanudarTicket.Ejecutar(ctx(), soporte.EntradaReanudarTicket{ID: tk.ID, Version: tk.Version})
	problema(t, err, 422, "transicion_invalida")
	_, err = m.ReabrirTicket.Ejecutar(ctx(), soporte.EntradaReabrirTicket{ID: tk.ID, Version: tk.Version, Motivo: "Nada"})
	problema(t, err, 422, "transicion_invalida")

	// Asignar: nuevo → abierto, emite ticket.asignado y estado_cambiado.
	tk, err = m.AsignarTicket.Ejecutar(ctx(), soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: &otro, CuadrillaID: ptr(int64(4))})
	if err != nil || tk.Estado != "abierto" || *tk.AsignadoA != otro || *tk.CuadrillaID != 4 {
		t.Fatalf("asignar: %+v %v", tk, err)
	}
	_, err = m.AsignarTicket.Ejecutar(ctx(), soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: &otro, CuadrillaID: ptr(int64(4))})
	problema(t, err, 422, "sin_cambios")
	_, err = m.AsignarTicket.Ejecutar(ctx(), soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: ptr(int64(99999999))})
	problema(t, err, 422, "validacion")

	// Versión vieja → 409.
	_, err = m.ResolverTicket.Ejecutar(ctx(), soporte.EntradaResolverTicket{ID: tk.ID, Version: tk.Version - 1, Solucion: "Listo ya"})
	problema(t, err, 409, "conflicto_version")

	tk, err = m.PonerTicketEnEspera.Ejecutar(ctx(), soporte.EntradaPonerEnEspera{ID: tk.ID, Version: tk.Version, Motivo: "Repuesto"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.PonerTicketEnEspera.Ejecutar(ctx(), soporte.EntradaPonerEnEspera{ID: tk.ID, Version: tk.Version, Motivo: "Otra vez"})
	problema(t, err, 422, "transicion_invalida")
	r.avanzar(time.Hour)
	tk, err = m.ResolverTicket.Ejecutar(ctx(), soporte.EntradaResolverTicket{ID: tk.ID, Version: tk.Version, Solucion: "Cambiamos el equipo"})
	if err != nil || tk.Estado != "resuelto" || tk.EnEsperaDesde != nil || tk.SLAResolucion.CumplidoEn == nil {
		t.Fatalf("resolver desde en espera: %+v %v", tk, err)
	}
	// Desde resuelto: no se asigna, escala, resuelve ni pone en espera; responder no lo reabre.
	_, err = m.AsignarTicket.Ejecutar(ctx(), soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version})
	problema(t, err, 422, "transicion_invalida")
	_, err = m.EscalarTicket.Ejecutar(ctx(), soporte.EntradaEscalarTicket{ID: tk.ID, Version: tk.Version, Motivo: "Tarde"})
	problema(t, err, 422, "transicion_invalida")
	_, err = m.ResolverTicket.Ejecutar(ctx(), soporte.EntradaResolverTicket{ID: tk.ID, Version: tk.Version, Solucion: "Otra vez"})
	problema(t, err, 422, "transicion_invalida")
	resp, err := m.ResponderTicket.Ejecutar(ctx(), soporte.EntradaResponderTicket{ID: tk.ID, Version: &tk.Version, Cuerpo: "¿Todo bien?"})
	if err != nil || resp.Ticket.Estado != "resuelto" {
		t.Fatalf("responder en resuelto: %+v %v", resp, err)
	}
	tk = resp.Ticket

	// Cerrar desde resuelto sin motivo; cerrado es final.
	tk, err = m.CerrarTicket.Ejecutar(ctx(), soporte.EntradaCerrarTicket{ID: tk.ID, Version: tk.Version})
	if err != nil || tk.Estado != "cerrado" || tk.CerradoEn == nil {
		t.Fatalf("cerrar: %+v %v", tk, err)
	}
	for nombre, intento := range map[string]func() error{
		"responder": func() error {
			_, err := m.ResponderTicket.Ejecutar(ctx(), soporte.EntradaResponderTicket{ID: tk.ID, Cuerpo: "Hola"})
			return err
		},
		"cerrar": func() error {
			_, err := m.CerrarTicket.Ejecutar(ctx(), soporte.EntradaCerrarTicket{ID: tk.ID, Version: tk.Version})
			return err
		},
		"reabrir": func() error {
			_, err := m.ReabrirTicket.Ejecutar(ctx(), soporte.EntradaReabrirTicket{ID: tk.ID, Version: tk.Version, Motivo: "Volvió"})
			return err
		},
		"asignar": func() error {
			_, err := m.AsignarTicket.Ejecutar(ctx(), soporte.EntradaAsignarTicket{ID: tk.ID, Version: tk.Version, UsuarioID: &yo})
			return err
		},
	} {
		if err := intento(); err == nil {
			t.Fatalf("%s en cerrado debería fallar", nombre)
		} else {
			problema(t, err, 422, "transicion_invalida")
		}
	}
	// La nota interna sí se permite en cerrado y no cambia la versión.
	nota, err := m.AgregarNotaInterna.Ejecutar(ctx(), soporte.EntradaNotaInterna{ID: tk.ID, Cuerpo: "Cliente satisfecho"})
	if err != nil || nota.Mensaje.Publico || nota.Ticket.Version != tk.Version {
		t.Fatalf("nota interna: %+v %v", nota, err)
	}

	// Cerrar sin resolver exige motivo.
	otroTk := crear(t, m, ctx(), soporte.EntradaCrearTicket{Asunto: "Spam"})
	_, err = m.CerrarTicket.Ejecutar(ctx(), soporte.EntradaCerrarTicket{ID: otroTk.ID, Version: otroTk.Version})
	problema(t, err, 422, "validacion")
	if _, err := m.CerrarTicket.Ejecutar(ctx(), soporte.EntradaCerrarTicket{ID: otroTk.ID, Version: otroTk.Version, Motivo: ptr("Spam")}); err != nil {
		t.Fatal(err)
	}

	// Historial y eventos del primero: creado, asignado, estados y respuestas.
	es := eventosDe(t, tk.ID)
	esperados := []string{"ticket.creado", "ticket.asignado", "ticket.estado_cambiado", "ticket.estado_cambiado",
		"ticket.estado_cambiado", "ticket.respondido", "ticket.estado_cambiado"}
	if got := nombres(es); len(got) != len(esperados) {
		t.Fatalf("eventos: %v", got)
	}
	for i, e := range esperados {
		if es[i].Evento != e {
			t.Fatalf("evento %d: %s, esperaba %s (%v)", i, es[i].Evento, e, nombres(es))
		}
	}
	if es[1].Payload["usuario_id"] != float64(otro) || es[1].Payload["cuadrilla_id"] != float64(4) {
		t.Fatalf("ticket.asignado: %v", es[1].Payload)
	}
	if es[2].Payload["de"] != "nuevo" || es[2].Payload["a"] != "abierto" {
		t.Fatalf("estado_cambiado: %v", es[2].Payload)
	}
	hist, _ := m.ListarEventos.Ejecutar(ctx(), soporte.EntradaListarEventos{ID: tk.ID})
	tipos := map[string]int{}
	for _, h := range hist.Data {
		tipos[h.Tipo]++
	}
	if tipos["creado"] != 1 || tipos["asignado"] != 1 || tipos["estado"] != 4 {
		t.Fatalf("historial: %v", tipos)
	}
}

func TestPermisosYAlcance(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	tz5 := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Zona cinco", ZonaID: ptr(int64(5))})
	tz7 := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{Asunto: "Zona siete", ZonaID: ptr(int64(7))})

	// Sin el permiso de la acción → 403.
	soloVer := como(t, yo, nucleo.PermTicketsVer)
	_, err := m.ResolverTicket.Ejecutar(soloVer, soporte.EntradaResolverTicket{ID: tz5.ID, Version: tz5.Version, Solucion: "Hecho y listo"})
	problema(t, err, 403, "prohibido")
	_, err = m.CrearTicket.Ejecutar(soloVer, soporte.EntradaCrearTicket{Asunto: "No puedo"})
	problema(t, err, 403, "prohibido")

	// Alcance por zona: solo ve y trabaja la zona 5.
	zona5 := []contexto.Alcance{{ZonaID: 5}}
	otro := agente(t)
	limitado := func() context.Context {
		return conAlcance(t, otro, map[string][]contexto.Alcance{
			"tickets.ver": zona5, "tickets.resolver": zona5, "tickets.crear": zona5,
		})
	}
	if _, err := m.VerTicket.Ejecutar(limitado(), soporte.EntradaVerTicket{ID: tz5.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = m.VerTicket.Ejecutar(limitado(), soporte.EntradaVerTicket{ID: tz7.ID})
	problema(t, err, 404, "no_encontrado")
	_, err = m.ResolverTicket.Ejecutar(limitado(), soporte.EntradaResolverTicket{ID: tz7.ID, Version: tz7.Version, Solucion: "No es mío"})
	problema(t, err, 404, "no_encontrado")
	_, err = m.CrearTicket.Ejecutar(limitado(), soporte.EntradaCrearTicket{Asunto: "Otra zona", ZonaID: ptr(int64(7))})
	problema(t, err, 403, "fuera_de_alcance")
	if _, err := m.CrearTicket.Ejecutar(limitado(), soporte.EntradaCrearTicket{Asunto: "Mi zona", ZonaID: ptr(int64(5))}); err != nil {
		t.Fatal(err)
	}
	pag, err := m.ListarTickets.Ejecutar(limitado(), soporte.EntradaListarTickets{ParamsPagina: nucleo.ParamsPagina{Limit: 200}})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range pag.Data {
		if x.ZonaID == nil || *x.ZonaID != 5 {
			t.Fatalf("la lista filtra por alcance: %+v", x)
		}
	}

	// Ve todo pero resuelve solo la zona 5 → 403 fuera_de_alcance en la 7.
	verTodo := conAlcance(t, otro, map[string][]contexto.Alcance{"tickets.ver": {{}}, "tickets.resolver": zona5})
	_, err = m.ResolverTicket.Ejecutar(verTodo, soporte.EntradaResolverTicket{ID: tz7.ID, Version: tz7.Version, Solucion: "No me toca"})
	problema(t, err, 403, "fuera_de_alcance")

	// Lo asignado al actor se ve aunque esté fuera de su zona.
	if _, err := m.AsignarTicket.Ejecutar(como(t, yo), soporte.EntradaAsignarTicket{ID: tz7.ID, Version: tz7.Version, UsuarioID: &otro}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.VerTicket.Ejecutar(limitado(), soporte.EntradaVerTicket{ID: tz7.ID}); err != nil {
		t.Fatalf("lo asignado al actor se ve: %v", err)
	}

	// Empresa activa: los tickets de otra empresa no existen para el actor.
	deOtra := contexto.ConEmpresa(como(t, yo), 77)
	_, err = m.VerTicket.Ejecutar(deOtra, soporte.EntradaVerTicket{ID: tz5.ID})
	problema(t, err, 404, "no_encontrado")
	tkEmp := crear(t, m, deOtra, soporte.EntradaCrearTicket{Asunto: "De la 77"})
	if tkEmp.EmpresaID == nil || *tkEmp.EmpresaID != 77 {
		t.Fatalf("empresa activa: %+v", tkEmp)
	}
	if es := eventosDe(t, tkEmp.ID); len(es) != 1 || es[0].Empresa == nil || *es[0].Empresa != 77 {
		t.Fatalf("el evento lleva la empresa: %+v", es)
	}
	_, err = m.CrearTicket.Ejecutar(contexto.ConEmpresa(como(t, yo), 77), soporte.EntradaCrearTicket{Asunto: "De otra", EmpresaID: ptr(int64(78))})
	problema(t, err, 422, "empresa_no_coincide")
}

func TestListarConFiltrosYCursor(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	cliente := 880000 + time.Now().UnixNano()%1000
	var ids []int64
	for i := range 3 {
		prio := "baja"
		if i == 2 {
			prio = "alta"
		}
		ids = append(ids, crear(t, m, como(t, yo), soporte.EntradaCrearTicket{ClienteID: &cliente, Prioridad: prio}).ID)
	}
	p1, err := m.ListarTickets.Ejecutar(como(t, yo), soporte.EntradaListarTickets{ClienteID: cliente, ParamsPagina: nucleo.ParamsPagina{Limit: 2}})
	if err != nil || len(p1.Data) != 2 || p1.NextCursor == nil || p1.Data[0].ID != ids[2] {
		t.Fatalf("página 1: %+v %v", p1, err)
	}
	p2, err := m.ListarTickets.Ejecutar(como(t, yo), soporte.EntradaListarTickets{ClienteID: cliente, ParamsPagina: nucleo.ParamsPagina{Limit: 2, Cursor: *p1.NextCursor}})
	if err != nil || len(p2.Data) != 1 || p2.NextCursor != nil || p2.Data[0].ID != ids[0] {
		t.Fatalf("página 2: %+v %v", p2, err)
	}
	altas, _ := m.ListarTickets.Ejecutar(como(t, yo), soporte.EntradaListarTickets{ClienteID: cliente, Prioridad: []string{"alta", "critica"}})
	if len(altas.Data) != 1 || altas.Data[0].ID != ids[2] {
		t.Fatalf("filtro de prioridad: %+v", altas)
	}
	nuevos, _ := m.ListarTickets.Ejecutar(como(t, yo), soporte.EntradaListarTickets{ClienteID: cliente, Estado: []string{"nuevo"}, SinAsignar: true})
	if len(nuevos.Data) != 3 {
		t.Fatalf("filtro de estado: %d", len(nuevos.Data))
	}
	_, err = m.ListarTickets.Ejecutar(como(t, yo), soporte.EntradaListarTickets{ParamsPagina: nucleo.ParamsPagina{Cursor: "@@"}})
	problema(t, err, 422, "validacion")

	cats, err := m.ListarCategorias.Ejecutar(como(t, yo), soporte.EntradaListarCategorias{})
	if err != nil || len(cats.Data) < 9 {
		t.Fatalf("categorías: %+v %v", cats, err)
	}
	slas, err := m.ListarSLAs.Ejecutar(como(t, yo), struct{}{})
	if err != nil || len(slas.Data) < 2 || len(slas.Data[0].Objetivos) != 4 {
		t.Fatalf("slas: %+v %v", slas, err)
	}
}

func TestFusionar(t *testing.T) {
	m, _ := nuevoModulo(time.Now())
	yo := agente(t)
	ctx := func() context.Context { return como(t, yo) }
	a := crear(t, m, ctx(), soporte.EntradaCrearTicket{Asunto: "Duplicado"})
	b := crear(t, m, ctx(), soporte.EntradaCrearTicket{Asunto: "Original"})

	_, err := m.FusionarTickets.Ejecutar(ctx(), soporte.EntradaFusionarTickets{ID: a.ID, Version: a.Version, DestinoID: a.ID})
	problema(t, err, 422, "fusion_invalida")
	_, err = m.FusionarTickets.Ejecutar(ctx(), soporte.EntradaFusionarTickets{ID: a.ID, Version: a.Version, DestinoID: 99999999})
	problema(t, err, 422, "fusion_invalida")

	res, err := m.FusionarTickets.Ejecutar(ctx(), soporte.EntradaFusionarTickets{ID: a.ID, Version: a.Version, DestinoID: b.ID, Motivo: ptr("Mismo cliente")})
	if err != nil || res.Origen.Estado != "cerrado" || res.Origen.FusionadoEnID == nil || *res.Origen.FusionadoEnID != b.ID || res.Destino.Estado != "nuevo" {
		t.Fatalf("fusionar: %+v %v", res, err)
	}
	if got := nombres(eventosDe(t, a.ID)); len(got) != 3 || got[1] != "ticket.estado_cambiado" || got[2] != "ticket.fusionado" {
		t.Fatalf("eventos de la fusión: %v", got)
	}
	notas, _ := m.ListarMensajes.Ejecutar(ctx(), soporte.EntradaListarMensajes{ID: b.ID})
	if len(notas.Data) != 1 || notas.Data[0].Publico {
		t.Fatalf("nota en el destino: %+v", notas)
	}
	// Un cerrado no se fusiona ni recibe fusiones.
	c := crear(t, m, ctx(), soporte.EntradaCrearTicket{Asunto: "Tercero"})
	_, err = m.FusionarTickets.Ejecutar(ctx(), soporte.EntradaFusionarTickets{ID: c.ID, Version: c.Version, DestinoID: a.ID})
	problema(t, err, 422, "fusion_invalida")
	_, err = m.FusionarTickets.Ejecutar(ctx(), soporte.EntradaFusionarTickets{ID: a.ID, Version: res.Origen.Version, DestinoID: c.ID})
	problema(t, err, 422, "transicion_invalida")
}

func TestCronSLAIdempotente(t *testing.T) {
	t0 := time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)
	m, _ := nuevoModulo(t0)
	yo := agente(t)
	// 24/7 crítica: respuesta 15 min (riesgo 12), resolución 240 min (riesgo 192).
	tk := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{CategoriaID: categoria(t, "incidente_red"), Prioridad: "critica"})
	espera := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{CategoriaID: categoria(t, "incidente_red"), Prioridad: "critica"})
	// Este responde enseguida y queda en espera: no alerta de respuesta ni de resolución.
	r1, err := m.ResponderTicket.Ejecutar(como(t, yo), soporte.EntradaResponderTicket{ID: espera.ID, Cuerpo: "Recibido"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.PonerTicketEnEspera.Ejecutar(como(t, yo), soporte.EntradaPonerEnEspera{ID: espera.ID, Version: r1.Ticket.Version, Motivo: "Esperando a Horus"}); err != nil {
		t.Fatal(err)
	}

	sis := func() context.Context {
		return contexto.Sistema(pruebas.ContextoSistema(pruebas.Pool(t)), contexto.CanalCron)
	}
	correr := func(d time.Duration) soporte.ResultadoRevisarSLA {
		t.Helper()
		res, err := m.RevisarSLA.Ejecutar(sis(), soporte.EntradaRevisarSLA{Ahora: t0.Add(d), Lote: 2})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	correr(5 * time.Minute)
	if n := len(eventosDe(t, tk.ID)); n != 1 {
		t.Fatalf("antes del 80 %% no hay alertas: %d", n)
	}
	// Al 80 % de la respuesta, una sola vez aunque corra dos veces (lote 2: varias consultas).
	correr(12 * time.Minute)
	correr(13 * time.Minute)
	es := eventosDe(t, tk.ID)
	if contar(es, "ticket.sla_en_riesgo") != 1 || es[1].Payload["sla"] != "respuesta" {
		t.Fatalf("en riesgo: %v", nombres(es))
	}
	correr(16 * time.Minute)
	correr(16 * time.Minute)
	if contar(eventosDe(t, tk.ID), "ticket.sla_vencido") != 1 {
		t.Fatalf("vencido: %v", nombres(eventosDe(t, tk.ID)))
	}
	// Resolución al 80 % y vencida.
	correr(192 * time.Minute)
	res := correr(241 * time.Minute)
	correr(300 * time.Minute)
	es = eventosDe(t, tk.ID)
	if contar(es, "ticket.sla_en_riesgo") != 2 || contar(es, "ticket.sla_vencido") != 2 {
		t.Fatalf("resolución: %v", nombres(es))
	}
	if res.Vencidos < 1 {
		t.Fatalf("resultado: %+v", res)
	}
	if v, _ := time.Parse(time.RFC3339, es[len(es)-1].Payload["vence_en"].(string)); !v.Equal(t0.Add(240 * time.Minute)) {
		t.Fatalf("vence_en del payload: %v", es[len(es)-1].Payload)
	}
	// Un reloj que cruza el 80 % y el vencimiento entre dos corridas solo emite el vencido.
	salto := crear(t, m, como(t, yo), soporte.EntradaCrearTicket{CategoriaID: categoria(t, "incidente_red"), Prioridad: "critica"})
	correr(20 * time.Minute)
	if got := nombres(eventosDe(t, salto.ID)); len(got) != 2 || got[1] != "ticket.sla_vencido" {
		t.Fatalf("si ya venció, solo el vencido: %v", got)
	}
	if got := nombres(eventosDe(t, espera.ID)); contar(eventosDe(t, espera.ID), "ticket.sla_vencido")+contar(eventosDe(t, espera.ID), "ticket.sla_en_riesgo") != 0 {
		t.Fatalf("en espera y respondido no alerta: %v", got)
	}
	// Historial de SLA en el ticket y fila de auditoría del cron.
	hist, _ := m.ListarEventos.Ejecutar(como(t, yo), soporte.EntradaListarEventos{ID: tk.ID})
	n := 0
	for _, h := range hist.Data {
		if h.Tipo == "sla_en_riesgo" || h.Tipo == "sla_vencido" {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("historial de SLA: %d", n)
	}
	// El cron audita solo las corridas que emitieron algo.
	contarCorridas := func() int {
		var n int
		if err := pruebas.Pool(t).QueryRow(context.Background(),
			"SELECT count(*) FROM auditoria.acciones WHERE accion = 'soporte.revisar_sla' AND resultado = 'ok'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	antes := contarCorridas()
	if antes == 0 {
		t.Fatal("las corridas con alertas dejan fila de auditoría")
	}
	correr(20 * time.Minute)
	if contarCorridas() != antes {
		t.Fatal("una corrida sin alertas no audita")
	}
	// El trabajo de River corre el mismo caso como sistema (canal cron).
	w := &soporte.TrabajadorSLA{BD: pruebas.Pool(t), Modulo: m}
	if err := w.Work(context.Background(), &river.Job[soporte.ArgsRevisarSLA]{}); err != nil {
		t.Fatal(err)
	}
	// Solo el sistema corre el cron.
	_, err = m.RevisarSLA.Ejecutar(como(t, yo), soporte.EntradaRevisarSLA{})
	problema(t, err, 403, "prohibido")
}

func ptr[T any](v T) *T { return &v }
