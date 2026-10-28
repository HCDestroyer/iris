package campo_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

func punto(cuando time.Time, lat float64) campo.PuntoPosicion {
	return campo.PuntoPosicion{Lat: lat, Lon: -90.5, Precision: ptr(8.0), RegistradaEn: cuando}
}

func enCampo(ctx context.Context) context.Context { return contexto.ConCanal(ctx, contexto.CanalCampo) }

// Posiciones en vivo: lote con validación de rango y hora, deduplicado, límite de lotes por
// minuto, sin auditoría por fila ni eventos, última por técnico con alcance y empresa.
func TestPosicionesEnVivo(t *testing.T) {
	e := nuevoEntorno()
	tec := tecnico(t)
	ct := enCampo(como(t, tec, "tecnico"))
	ahora := e.reloj.ahora()

	r := deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{DispositivoID: "tel-1", Puntos: []campo.PuntoPosicion{
		punto(ahora.Add(-10*time.Minute), 14.60), punto(ahora.Add(-9*time.Minute), 14.61),
		punto(ahora.Add(-9*time.Minute+2*time.Second), 14.62), // más seguido que el intervalo mínimo
		punto(ahora.Add(-25*time.Hour), 14.63),                // vieja
		punto(ahora.Add(10*time.Minute), 14.64),               // adelantada
	}}))(t)
	if r.Aceptadas != 2 || r.Duplicadas != 0 || r.Descartadas != 3 || r.Motivos["frecuencia"] != 1 || r.Motivos["vieja"] != 1 || r.Motivos["futura"] != 1 {
		t.Fatalf("lote: %+v", r)
	}
	// Reenviar el mismo lote no duplica.
	r = deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{
		punto(ahora.Add(-10*time.Minute), 14.60), punto(ahora.Add(-9*time.Minute), 14.61)}}))(t)
	if r.Aceptadas != 0 || r.Duplicadas != 2 {
		t.Fatalf("reenvío: %+v", r)
	}
	// El reloj del teléfono atrasado 1 h se corrige con enviadoEn.
	r = deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{EnviadoEn: ptr(ahora.Add(-time.Hour)),
		Puntos: []campo.PuntoPosicion{punto(ahora.Add(-time.Hour-time.Minute), 14.65)}}))(t)
	if r.Aceptadas != 1 || r.DesfaseMs != time.Hour.Milliseconds() {
		t.Fatalf("desfase: %+v", r)
	}
	if n := contar(t, "SELECT count(*) FROM campo.posiciones WHERE tecnico_id = $1", tec); n != 3 {
		t.Fatalf("posiciones guardadas: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM campo.posiciones WHERE tecnico_id = $1 AND registrada_en = $2", tec, ahora.Add(-time.Minute)); n != 1 {
		t.Fatal("la hora corregida")
	}

	// Solo el técnico con sesión de la app de campo; rango validado.
	_, err := e.m.RegistrarPosiciones.Ejecutar(como(t, tec, "tecnico"), campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(ahora, 14.6)}})
	problema(t, err, 403, "requiere_sesion_campo")
	token := contexto.ConActor(ct, contexto.Actor{Tipo: contexto.ActorToken, ID: tec, TokenID: 9}, contexto.PermisosDe(ct))
	_, err = e.m.RegistrarPosiciones.Ejecutar(token, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(ahora, 14.6)}})
	problema(t, err, 403, "requiere_sesion_campo")
	_, err = e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(ahora, 95)}})
	if p := problema(t, err, 422, "validacion"); p[0].Field != "puntos[0].lat" {
		t.Fatalf("rango: %+v", p)
	}
	desp := despacho(t)
	_, err = e.m.RegistrarPosiciones.Ejecutar(enCampo(como(t, desp, "despacho")), campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(ahora, 14.6)}})
	problema(t, err, 403, "prohibido")

	// Límite de lotes por minuto (6): ya van 4 (los 3 aceptados y el rechazado por rango, que
	// también cuenta); el 7.º recibe 429 con Retry-After.
	for i := range 2 {
		deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(ahora.Add(time.Duration(-i)*time.Minute-30*time.Second), 14.6)}}))(t)
	}
	_, err = e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(ahora, 14.7)}})
	var p *nucleo.Problema
	if !errorsAs(err, &p) || p.Status != 429 || p.Codigo() != "demasiadas_posiciones" || p.GetHeaders().Get("Retry-After") == "" {
		t.Fatalf("límite: %v", err)
	}
	e.reloj.avanzar(61 * time.Second)
	deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(e.reloj.ahora(), 14.7)}}))(t)

	// Sin auditoría por fila: ni los lotes aceptados ni el 429; tampoco eventos.
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'campo.registrar_posiciones' AND resultado = 'ok'"); n != 0 {
		t.Fatalf("auditoría de lotes aceptados: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'campo.registrar_posiciones' AND error->>'status' = '429'"); n != 0 {
		t.Fatalf("auditoría del 429: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento LIKE 'posicion%' OR evento LIKE 'campo.posicion%'"); n != 0 {
		t.Fatalf("eventos: %d", n)
	}

	// Última posición: despacho global la ve; con alcance de otra cuadrilla, no; con la del técnico, sí.
	ult := deber(e.m.UltimasPosiciones.Ejecutar(como(t, desp, "despacho"), campo.EntradaUltimasPosiciones{TecnicoID: tec}))(t)
	if len(ult.Data) != 1 || ult.Data[0].Lat != 14.7 || ult.Data[0].AntiguedadSeg != 0 || ult.Data[0].FichaID == nil {
		t.Fatalf("última: %+v", ult)
	}
	ag := e.m.Agenda()
	sup := usuarioCon(t, rolDe("supervisor"))
	cs := como(t, sup, "supervisor")
	c := deber(ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla posiciones " + pruebas.Unico()}))(t)
	otra := deber(ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Otra cuadrilla " + pruebas.Unico()}))(t)
	ficha := deber(ag.ListarTecnicos.Ejecutar(cs, agenda.EntradaListarTecnicos{UsuarioID: tec}))(t).Data[0]
	deber(ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: ficha.ID, Version: ficha.Version, CuadrillaID: &c.ID}))(t)
	conCuadrilla := func(id int64) context.Context {
		return conAlcance(t, desp, map[string][]contexto.Alcance{"campo.ver_posiciones": {{CuadrillaID: id}}})
	}
	if u := deber(e.m.UltimasPosiciones.Ejecutar(conCuadrilla(otra.ID), campo.EntradaUltimasPosiciones{}))(t); len(u.Data) != 0 {
		t.Fatalf("otra cuadrilla: %+v", u.Data)
	}
	if u := deber(e.m.UltimasPosiciones.Ejecutar(conCuadrilla(c.ID), campo.EntradaUltimasPosiciones{}))(t); len(u.Data) != 1 || *u.Data[0].CuadrillaID != c.ID {
		t.Fatalf("su cuadrilla: %+v", u.Data)
	}
	// Otra empresa activa: no la ve (la posición se guardó sin empresa).
	enEmpresa := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), desp, 5, permisosDe("despacho")...)
	if u := deber(e.m.UltimasPosiciones.Ejecutar(enEmpresa, campo.EntradaUltimasPosiciones{}))(t); len(u.Data) != 0 {
		t.Fatalf("otra empresa: %+v", u.Data)
	}
	// Muy vieja para el filtro.
	e.reloj.avanzar(2 * time.Hour)
	if u := deber(e.m.UltimasPosiciones.Ejecutar(como(t, desp, "despacho"), campo.EntradaUltimasPosiciones{TecnicoID: tec, MaxAntiguedad: 60}))(t); len(u.Data) != 0 {
		t.Fatalf("antigüedad: %+v", u.Data)
	}
}

// Retención: el mantenimiento crea las particiones próximas y borra las que terminaron antes de
// ahora − retención.
func TestPurgaDePosiciones(t *testing.T) {
	e := nuevoEntorno(func(c *campo.Config) { c.Posiciones.Retencion = 90 * 24 * time.Hour })
	tec := tecnico(t)
	pool := pruebas.Pool(t)
	ctx := context.Background()
	for _, sql := range []string{
		"SELECT campo.asegurar_particion_posiciones('2026-01-15T00:00:00Z')",
		"SELECT campo.asegurar_particion_posiciones('2026-08-15T00:00:00Z')",
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO campo.posiciones (tecnico_id, lat, lon, registrada_en) VALUES ($1, 14.6, -90.5, '2026-01-15T10:00:00Z')", tec); err != nil {
		t.Fatal(err)
	}
	r := deber(e.m.MantenerPosiciones.Ejecutar(pruebas.ContextoSistema(pool), struct{}{}))(t)
	if r.ParticionesBorradas != 1 {
		t.Fatalf("purga: %+v", r)
	}
	existe := func(nombre string) bool {
		var ok bool
		if err := pool.QueryRow(ctx, "SELECT to_regclass('campo.' || $1::text) IS NOT NULL", nombre).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if existe("posiciones_202601") || !existe("posiciones_202608") || !existe("posiciones_202610") || !existe("posiciones_202612") {
		t.Fatal("particiones tras el mantenimiento")
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'campo.mantener_posiciones' AND resultado = 'ok'"); n != 1 {
		t.Fatalf("auditoría de la purga: %d", n)
	}
	// Una persona no lo corre.
	_, err := e.m.MantenerPosiciones.Ejecutar(como(t, despacho(t), "despacho"), struct{}{})
	problema(t, err, 403, "prohibido")
}

func eventoRetiro(servicio, cliente, entrante int64) integracion.Evento {
	crudo, _ := json.Marshal(map[string]any{"servicio_id": servicio, "cliente_id": cliente, "de": "activo", "a": "retirado", "entrante_id": entrante})
	return integracion.Evento{ID: uuid.Must(uuid.NewV7()), Evento: "argos.servicio_retirado", Payload: crudo}
}

// Orden de retiro automática desde argos.servicio_retirado: solo con equipos en comodato y sin
// otra de retiro abierta; idempotente.
func TestRetiroAutomaticoDesdeArgos(t *testing.T) {
	inv := &inventarioFalso{comodato: map[int64][]campo.EquipoComodato{
		9911: {{UnidadID: 71, Serie: "ZTE9911", Modelo: "F660"}},
	}}
	e := nuevoEntorno(func(c *campo.Config) { c.Inventario = inv })
	sis := pruebas.ContextoSistema(pruebas.Pool(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	retiros := func(servicio int64) int {
		return contar(t, `SELECT count(*) FROM campo.ordenes o JOIN campo.tipos_orden tp ON tp.id = o.tipo_id
			WHERE o.servicio_id = $1 AND tp.codigo = 'retiro' AND o.origen = 'argos'`, servicio)
	}

	if err := e.m.AlServicioRetirado(sis, eventoRetiro(9911, 991, 1), log); err != nil {
		t.Fatal(err)
	}
	if retiros(9911) != 1 {
		t.Fatalf("orden de retiro: %d", retiros(9911))
	}
	var id int64
	var desc, dir string
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT id, descripcion, direccion FROM campo.ordenes WHERE servicio_id = 9911").Scan(&id, &desc, &dir); err != nil {
		t.Fatal(err)
	}
	ev := eventosDe(t, id)
	if p := ultimo(t, ev, "orden.creada"); p["origen"] != "argos" || p["tipo"] != "retiro" || p["cliente_id"] != float64(991) {
		t.Fatalf("orden.creada: %+v", p)
	}
	if dir == "" || desc == "" {
		t.Fatalf("dirección por confirmar y equipos en la descripción: %q %q", dir, desc)
	}
	// El mismo evento otra vez (el relay puede repetir) o otro aviso del mismo servicio: nada nuevo.
	for _, ev := range []integracion.Evento{eventoRetiro(9911, 991, 1), eventoRetiro(9911, 991, 2)} {
		if err := e.m.AlServicioRetirado(sis, ev, log); err != nil {
			t.Fatal(err)
		}
	}
	if retiros(9911) != 1 {
		t.Fatalf("idempotente: %d", retiros(9911))
	}
	// Cancelada la orden, un aviso nuevo crea otra.
	o := deber(e.m.VerOrden.Ejecutar(sis, campo.EntradaVerOrden{ID: id}))(t)
	deber(e.m.CancelarOrden.Ejecutar(sis, campo.EntradaMotivo{ID: id, Version: o.Version, Motivo: "Duplicada"}))(t)
	if err := e.m.AlServicioRetirado(sis, eventoRetiro(9911, 991, 3), log); err != nil {
		t.Fatal(err)
	}
	if retiros(9911) != 2 {
		t.Fatalf("tras cancelar: %d", retiros(9911))
	}
	// Sin equipos en comodato: no hay nada que recuperar.
	if err := e.m.AlServicioRetirado(sis, eventoRetiro(9912, 992, 4), log); err != nil || retiros(9912) != 0 {
		t.Fatalf("sin comodato: %v %d", err, retiros(9912))
	}
	// Argos caído: el relay reintenta (error); sin conector: no se sabe, no se crea.
	inv.caido = true
	if err := e.m.AlServicioRetirado(sis, eventoRetiro(9913, 993, 5), log); err == nil {
		t.Fatal("con Argos caído debe reintentar")
	}
	sinConector := nuevoEntorno()
	if err := sinConector.m.AlServicioRetirado(sis, eventoRetiro(9913, 993, 6), log); err != nil || retiros(9913) != 0 {
		t.Fatalf("sin conector: %v", err)
	}
	// Payload ilegible: se descarta sin reintentar.
	if err := e.m.AlServicioRetirado(sis, integracion.Evento{Evento: "argos.servicio_retirado", Payload: []byte(`{"servicio_id": "x"}`)}, log); err != nil {
		t.Fatal(err)
	}
}

// Visita cobrada: el tipo (o la plantilla) dice cobrar → orden.cerrada lleva cargo.
func TestCargoEnOrdenCerrada(t *testing.T) {
	pool := pruebas.Pool(t)
	ctx := context.Background()
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE campo.tipos_orden SET cobrar = true, cargo_monto = 150, cargo_descripcion = 'Visita de mantenimiento' WHERE codigo = 'mantenimiento'")
	t.Cleanup(func() {
		exec("UPDATE campo.tipos_orden SET cobrar = false, cargo_monto = NULL, cargo_producto_id = NULL, cargo_descripcion = '' WHERE codigo = 'mantenimiento'")
		exec("UPDATE campo.plantillas_checklist SET cobrar = NULL, cargo_producto_id = NULL, cargo_monto = NULL WHERE codigo = 'mantenimiento'")
	})
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	cerrar := func() map[string]any {
		t.Helper()
		o := programada(t, e, cd, "mantenimiento", tec)
		o = deber(e.m.Llegar.Ejecutar(ct, campo.EntradaPosicion{ID: o.ID, Version: o.Version}))(t)
		completarChecklist(t, e, ct, o.ID)
		o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
		deber(e.m.CerrarOrden.Ejecutar(ct, campo.EntradaCerrarOrden{ID: o.ID, Version: o.Version}))(t)
		return ultimo(t, eventosDe(t, o.ID), "orden.cerrada")
	}
	p := cerrar()
	cargo, ok := p["cargo"].(map[string]any)
	if !ok || cargo["monto"] != 150.0 || cargo["producto_id"] != nil || cargo["descripcion"] == "" {
		t.Fatalf("cargo del tipo: %+v", p["cargo"])
	}
	// La plantilla manda: con producto propio…
	exec("UPDATE campo.plantillas_checklist SET cobrar = true, cargo_producto_id = 77 WHERE codigo = 'mantenimiento'")
	if c, _ := cerrar()["cargo"].(map[string]any); c["producto_id"] != 77.0 || c["monto"] != nil {
		t.Fatalf("cargo de la plantilla: %+v", c)
	}
	// …o diciendo que no se cobra.
	exec("UPDATE campo.plantillas_checklist SET cobrar = false WHERE codigo = 'mantenimiento'")
	if c := cerrar()["cargo"]; c != nil {
		t.Fatalf("sin cargo: %+v", c)
	}
	// Un tipo que no cobra: cargo null (visita normal).
	exec("UPDATE campo.plantillas_checklist SET cobrar = NULL, cargo_producto_id = NULL WHERE codigo = 'mantenimiento'")
	exec("UPDATE campo.tipos_orden SET cobrar = false WHERE codigo = 'mantenimiento'")
	if c := cerrar()["cargo"]; c != nil {
		t.Fatalf("tipo sin cobro: %+v", c)
	}
}

func rolDe(r string) identidad.AsignacionRol { return identidad.AsignacionRol{Rol: r} }

func errorsAs(err error, p **nucleo.Problema) bool { return errors.As(err, p) }
