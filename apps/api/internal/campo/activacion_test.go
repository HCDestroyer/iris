package campo_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/equipos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Activación desde la orden (contrato argosisp.md 0.9.0 §12.2, B12) de punta a punta: lote del
// teléfono o panel → intento → cola de pendientes → Argos falso → resultado (cola, webhook o
// consulta) → cierre sin repetir la activación ni el comodato.

const claveWifiPrueba = "Clave-Secreta-Wifi-2026"

var servicioActivacion atomic.Int64

func init() { servicioActivacion.Store(45000) }

// entornoActivacion: el del titular, con Argos 0.9.0 que puede aprovisionar FTTH y wireless en la
// empresa 1, y la activación suscrita a la cola.
type entornoActivacion struct {
	*entornoTitular
}

func nuevoEntornoActivacion(t *testing.T, modo string) *entornoActivacion {
	t.Helper()
	e := &entornoActivacion{nuevoEntornoTitular(t)}
	pool := pruebas.Pool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE integracion.conectores SET contrato_remoto = '0.9.0',
		capacidades_aprovisionamiento = '[{"empresa_id":1,"ftth":true,"wireless":true,"tr069":false}]' WHERE sistema = 'argosisp'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campo.ajustes (empresa_id, activacion_modo) VALUES (1, $1)
		ON CONFLICT (coalesce(empresa_id, 0)) DO UPDATE SET activacion_modo = EXCLUDED.activacion_modo`, modo); err != nil {
		t.Fatal(err)
	}
	// La activación usa el mismo Argos que la cola.
	e.m = campo.Nuevo(campo.Config{Reloj: e.reloj.ahora, Adjuntos: campo.SinAdjuntosObligatorios{},
		Titular: campo.ConfigTitular{Cifrador: cifradorDe(t, e.entornoTitular), Conector: campo.ConectorDeArgos(e.am)}, Equipos: campo.EquiposDeArgos(e.am)})
	e.m.SuscribirActivaciones(e.bus, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	equipos.Nuevo().Suscribir(e.bus, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM campo.activaciones", "DELETE FROM campo.orden_activacion", "DELETE FROM integracion.cambios_wifi",
			"DELETE FROM campo.ajustes", "DELETE FROM campo.cobros", "DELETE FROM campo.orden_eventos WHERE tipo = 'activacion'"} {
			if _, err := pool.Exec(context.Background(), sql); err != nil {
				t.Error(err)
			}
		}
	})
	return e
}

// cifradorDe: el cifrador de la cola (el mismo que abre el depósito).
func cifradorDe(t *testing.T, e *entornoTitular) *nucleo.Cifrador {
	t.Helper()
	return e.am.Config().Cifrador
}

// instalacionPropia: orden de instalación con su propio servicio (los intentos vivos son únicos por
// servicio), del técnico y en sitio.
func (e *entornoActivacion) instalacionPropia(tec int64, ct context.Context) campo.Orden {
	e.t.Helper()
	servicio := servicioActivacion.Add(1)
	o := deber(e.m.CrearOrden.Ejecutar(e.cs, campo.EntradaCrearOrden{Tipo: "instalacion", Direccion: ptr("Zona 10"),
		ClienteID: ptr(int64(990)), ServicioID: &servicio}))(e.t)
	o = deber(e.m.ProgramarOrden.Ejecutar(e.cs, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec,
		BodegaID: ptr(int64(9)), Inicio: hoyEnGuatemala(1 + 2*(e.citas%11))}))(e.t)
	e.citas++
	e.lote(ct, op(campo.OpLlegar, o.ID, nil))
	// Sin identidad tomada, un supervisor la valida (solo sirve para activar, §3.4): el cierre no la exige.
	f := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(e.t)
	deber(e.m.Titular.ValidarIdentidad.Ejecutar(e.cs, campo.EntradaValidarIdentidad{ID: o.ID, Version: f.Version, Motivo: "Validada para la prueba de activación"}))(e.t)
	return o
}

func datosActivacion(conClave bool) map[string]any {
	wifi := map[string]any{"ssid": "Familia Pérez"}
	if conClave {
		wifi["clave"] = claveWifiPrueba
	}
	return map[string]any{"equipo": map[string]any{"tipo": "ont", "serie": "HWTC1234", "modelo": "F670L"},
		"planta": map[string]any{"olt": "OLT-Centro", "slot": 1, "pon": 7, "caja": "CJ-118", "puerto": 4}, "wifi": wifi}
}

// sinClaveWifi: la clave no está en ninguna tabla de Iris fuera del depósito cifrado.
func sinClaveWifi(t *testing.T) {
	t.Helper()
	for _, consulta := range []string{
		"SELECT count(*) FROM auditoria.acciones WHERE entrada::text LIKE $1 OR coalesce(resultado::text, '') LIKE $1 OR coalesce(error::text, '') LIKE $1",
		"SELECT count(*) FROM auditoria.cambios WHERE coalesce(antes::text, '') LIKE $1 OR coalesce(despues::text, '') LIKE $1",
		"SELECT count(*) FROM integracion.outbox WHERE payload::text LIKE $1",
		"SELECT count(*) FROM integracion.pendientes WHERE payload::text LIKE $1 OR coalesce(respuesta::text, '') LIKE $1 OR coalesce(ultimo_error, '') LIKE $1",
		"SELECT count(*) FROM campo.operaciones_sync WHERE coalesce(resultado::text, '') LIKE $1",
		"SELECT count(*) FROM campo.activaciones a WHERE row_to_json(a)::text LIKE $1",
		"SELECT count(*) FROM integracion.cambios_wifi c WHERE row_to_json(c)::text LIKE $1",
		"SELECT count(*) FROM integracion.datos_transito WHERE position(convert_to('" + claveWifiPrueba + "', 'UTF8') in coalesce(cifrado, ''::bytea)) > 0 AND $1 <> ''",
	} {
		if n := contar(t, consulta, "%"+claveWifiPrueba+"%"); n != 0 {
			t.Fatalf("la clave Wi-Fi quedó fuera del depósito: %d filas en %q", n, consulta[:60])
		}
	}
}

func (e *entornoActivacion) activacion(id int64) campo.EstadoActivacion {
	e.t.Helper()
	return deber(e.m.Activacion.Ver.Ejecutar(e.cs, campo.EntradaVerOrden{ID: id}))(e.t)
}

func TestActivacionSinSenalHastaListaYCierreSinRepetir(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)

	// El teléfono manda la activación sin señal; el reintento de la misma operación es «duplicada».
	act := op(campo.OpActivarServicio, o.ID, datosActivacion(true))
	esperar(t, e.lote(ct, act, act), "aplicada", "duplicada")
	esperar(t, e.lote(ct, act), "duplicada")
	clave := pendientes.ClaveAprovisionar(o.ID, 1)
	p := e.pendiente(clave)
	if p.Actor == nil || *p.Actor != "usuario:"+strconv.FormatInt(tec, 10) {
		t.Fatalf("pendiente de aprovisionar: %+v", p)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 1 {
		t.Fatalf("la clave va al depósito cifrado: %d", n)
	}
	if st := e.activacion(o.ID); st.Estado != campo.ActivacionPendiente || st.Actual == nil || !st.Actual.ConWifi || st.Actual.ID.String() != act.ID {
		t.Fatalf("intento pendiente con el id de la operación: %+v", st)
	}
	// Otro pedido mientras tanto (el panel): 409.
	_, err := e.m.Activacion.Activar.Ejecutar(e.cs, campo.EntradaActivarServicio{ID: o.ID, Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}})
	problema(t, err, 409, "activacion_en_curso")
	// Cerrar con «exigir lista» sin la activación lista: 422.
	cierre := trabajoDeInstalacion(o.ID)[2:]
	r := e.lote(ct, cierre...)
	if fin := r.Resultados[len(r.Resultados)-1]; fin.Error == nil || fin.Error.Codigo() != "activacion_requerida" {
		t.Fatalf("cierre sin la activación lista: %+v", fin)
	}

	// Sale a Argos con la clave (solo en el cuerpo) y el actor; el depósito se borra al 2xx.
	e.procesar()
	l := e.falso.porClave(clave)
	if l == nil || l.Ruta != "/servicios/"+strconv.FormatInt(*o.ServicioID, 10)+"/aprovisionar" || l.Actor != "usuario:"+strconv.FormatInt(tec, 10) {
		t.Fatalf("llamada a aprovisionar: %+v", l)
	}
	wifi, _ := l.JSON["wifi"].(map[string]any)
	if wifi["clave"] != claveWifiPrueba || wifi["ssid"] != "Familia Pérez" || l.JSON["origen_id"] != orden || l.JSON["origen_tipo"] != "iris_orden" {
		t.Fatalf("cuerpo de aprovisionar: %+v", l.JSON)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 0 {
		t.Fatalf("la clave se descarta al 2xx: %d", n)
	}
	e.relay()
	st := e.activacion(o.ID)
	if st.Estado != campo.ActivacionEnProceso || st.Actual.AprovisionamientoID == nil {
		t.Fatalf("en proceso con el aprovisionamiento: %+v", st.Actual)
	}
	aprov := *st.Actual.AprovisionamientoID

	// El webhook llega dos veces (y uno tardío de fallo): queda listo una sola vez.
	ok := campo.EntradaResolverActivacion{Fuente: campo.FuenteWebhook, IdempotencyKey: &clave, AprovisionamientoID: &aprov, Estado: campo.ActivacionLista}
	r1 := deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), ok))(t)
	r2 := deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), ok))(t)
	tarde := ok
	tarde.Estado, tarde.Causa = campo.ActivacionFallida, ptr("otro")
	r3 := deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), tarde))(t)
	if r1.Resultado != "aplicado" || r2.Resultado != "sin_cambios" || r3.Resultado != "sin_cambios" {
		t.Fatalf("webhooks repetidos: %+v %+v %+v", r1, r2, r3)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.activacion_cambiada' AND payload->>'orden_id' = $1 AND payload->>'a' = 'listo'", orden); n != 1 {
		t.Fatalf("orden.activacion_cambiada a listo: %d", n)
	}

	// Cierre con la activación lista: no manda activar-instalacion ni el comodato de la ONT.
	esperar(t, e.lote(ct, op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa"})), "aplicada")
	var payload map[string]any
	var crudo []byte
	if err := pruebas.Pool(t).QueryRow(context.Background(), "SELECT payload FROM integracion.outbox WHERE evento = 'orden.cerrada' AND payload->>'orden_id' = $1", orden).Scan(&crudo); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(crudo, &payload)
	a, _ := payload["activacion"].(map[string]any)
	if a["modo"] != campo.ModoExigirLista || a["estado"] != campo.ActivacionLista || a["aprovisionamiento_id"] != float64(aprov) {
		t.Fatalf("orden.cerrada.activacion: %+v", a)
	}
	eqs, _ := payload["equipos"].([]any)
	if len(eqs) != 1 || eqs[0].(map[string]any)["aprovisionado"] != true {
		t.Fatalf("equipos[].aprovisionado: %+v", eqs)
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion IN ('activar_instalacion', 'asignar_comodato')", orden); n != 0 {
		t.Fatalf("el cierre repitió la activación o el comodato: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'consumo'", orden); n != 1 {
		t.Fatalf("el consumo sigue saliendo: %d", n)
	}
	// Con la orden cerrada y la activación lista, nadie activa otra vez.
	_, err = e.m.Activacion.Activar.Ejecutar(e.cs, campo.EntradaActivarServicio{ID: o.ID, Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}})
	problema(t, err, 409, "activacion_lista")
	sinClaveWifi(t)
}

func TestActivacionConcurrenteTelefonoYPanel(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	for ronda := range 3 {
		o := e.instalacionPropia(tec, ct)
		var wg sync.WaitGroup
		var aplicadas, rechazos atomic.Int32
		for i := range 8 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if i%2 == 0 {
					r, err := e.m.Lote(ct, campo.EntradaLote{DispositivoID: "tel-act-" + pruebas.Unico(), Plataforma: "pwa",
						Operaciones: []campo.OperacionLote{op(campo.OpActivarServicio, o.ID, datosActivacion(false))}})
					if err != nil {
						t.Error(err)
						return
					}
					switch res := r.Resultados[0]; {
					case res.Estado == "aplicada":
						aplicadas.Add(1)
					case res.Error != nil && (res.Error.Codigo() == "activacion_en_curso" || res.Error.Codigo() == "concurrencia"):
						rechazos.Add(1)
					default:
						t.Errorf("resultado inesperado: %+v", res)
					}
					return
				}
				_, err := e.m.Activacion.Activar.Ejecutar(e.cs, campo.EntradaActivarServicio{ID: o.ID,
					Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}})
				var p *nucleo.Problema
				switch {
				case err == nil:
					aplicadas.Add(1)
				case errors.As(err, &p) && (p.Codigo() == "activacion_en_curso" || p.Codigo() == "concurrencia"):
					rechazos.Add(1)
				default:
					t.Errorf("panel: %v", err)
				}
			}(i)
		}
		wg.Wait()
		if aplicadas.Load() != 1 || rechazos.Load() != 7 {
			t.Fatalf("ronda %d: %d aplicadas y %d rechazadas (una sola activación)", ronda, aplicadas.Load(), rechazos.Load())
		}
		if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE operacion = 'aprovisionar' AND entidad_id = $1", strconv.FormatInt(o.ID, 10)); n != 1 {
			t.Fatalf("ronda %d: %d pendientes de aprovisionar", ronda, n)
		}
		if n := contar(t, "SELECT count(*) FROM campo.activaciones WHERE orden_id = $1", o.ID); n != 1 {
			t.Fatalf("ronda %d: %d intentos", ronda, n)
		}
	}
}

func TestActivacionFallidaReintentoYPendientePorAgente(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)

	// Argos rechaza el primer intento (la ONT no está en la bodega del técnico): fallido, con la causa,
	// y la clave se descarta ya.
	e.falso.rechazar("/aprovisionar", "409:unidad_en_otra_bodega")
	esperar(t, e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(true))), "aplicada")
	e.procesar()
	e.relay()
	st := e.activacion(o.ID)
	if st.Estado != campo.ActivacionFallida || st.Actual.Causa == nil || *st.Actual.Causa != "unidad_en_otra_bodega" || !st.Disponible {
		t.Fatalf("intento fallido con causa y «Reintentar»: %+v %+v", st, st.Actual)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 0 {
		t.Fatalf("la clave rechazada se descarta: %d", n)
	}

	// «Pendiente por agente»: el técnico cierra sin la activación; el cierre no manda activar-instalacion
	// (sí el comodato: no hay un intento vivo con ese equipo).
	esperar(t, e.lote(ct, trabajoDeInstalacion(o.ID)[2:]...), repetir(len(trabajoDeInstalacion(o.ID))-2, "aplicada")...)
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'activar_instalacion'", orden); n != 0 {
		t.Fatalf("el cierre mandó activar-instalacion: %d", n)
	}
	// El técnico ya no activa con la orden cerrada; el agente sí (otro intento, otra clave) y manda al
	// técnico de la orden.
	_, err := e.m.Activacion.Activar.Ejecutar(ct, campo.EntradaActivarServicio{ID: o.ID, Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}})
	problema(t, err, 403, "orden_no_en_curso")
	deber(e.m.Activacion.Activar.Ejecutar(e.cs, campo.EntradaActivarServicio{ID: o.ID, Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}}))(t)
	e.procesar()
	l := e.falso.porClave(pendientes.ClaveAprovisionar(o.ID, 2))
	if l == nil || l.Actor != "usuario:"+strconv.FormatInt(e.sup, 10) || l.JSON["wifi"] != nil {
		t.Fatalf("segundo intento desde el panel: %+v", l)
	}
	e.relay()
	// El webhook no llegó: a los 10 min se consulta Argos.
	st = e.activacion(o.ID)
	aprov := *st.Actual.AprovisionamientoID
	e.falso.mu.Lock()
	e.falso.aprovs[strconv.FormatInt(aprov, 10)] = `{"aprovisionamiento_id":` + strconv.FormatInt(aprov, 10) +
		`,"servicio_id":1,"estado":"listo","pasos":[{"paso":"ip","estado":"hecho"},{"paso":"activacion","estado":"hecho"},{"paso":"raro","estado":"x"}]}`
	e.falso.mu.Unlock()
	if n := deber(e.m.Activacion.Consultar(sistema(t)))(t); n != 0 {
		t.Fatalf("antes de 10 min no se consulta: %d", n)
	}
	e.reloj.avanzar(11 * time.Minute)
	if n := deber(e.m.Activacion.Consultar(sistema(t)))(t); n != 1 {
		t.Fatalf("consulta a los 10 min: %d", n)
	}
	st = e.activacion(o.ID)
	if st.Estado != campo.ActivacionLista || len(st.Actual.Pasos) != 2 || len(st.Intentos) != 2 {
		t.Fatalf("lista por la consulta (pasos válidos): %+v", st.Actual)
	}
	sinClaveWifi(t)
}

func TestActivacionNoRepiteLaDelCierre(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	// Modo «al cerrar»: no se ofrece activar.
	r := e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(false)))
	if r.Resultados[0].Error == nil || r.Resultados[0].Error.Codigo() != "activacion_no_disponible" {
		t.Fatalf("modo al cerrar: %+v", r.Resultados[0])
	}
	esperar(t, e.lote(ct, trabajoDeInstalacion(o.ID)[2:]...), repetir(len(trabajoDeInstalacion(o.ID))-2, "aplicada")...)
	e.relay()
	e.pendiente("iris:instalacion:" + strconv.FormatInt(o.ID, 10))
	// La empresa cambia a «pendiente por agente» después: el cierre ya mandó activar-instalacion.
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ajustes SET activacion_modo = 'pendiente_agente' WHERE empresa_id = 1"); err != nil {
		t.Fatal(err)
	}
	_, err := e.m.Activacion.Activar.Ejecutar(e.cs, campo.EntradaActivarServicio{ID: o.ID, Equipo: campo.EquipoActivacion{Tipo: "ont", Serie: ptr("HWTC1234")}})
	problema(t, err, 409, "activacion_al_cerrar")
	if st := e.activacion(o.ID); st.Disponible || st.NoDisponible == nil || *st.NoDisponible != "activacion_al_cerrar" {
		t.Fatalf("estado: %+v", st)
	}
}

func TestActivacionExigeListaSalvoSupervisor(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	trabajo := trabajoDeInstalacion(o.ID)[2:]
	esperar(t, e.lote(ct, trabajo[:len(trabajo)-1]...), repetir(len(trabajo)-1, "aplicada")...)
	// El técnico con motivo: no es supervisor.
	r := e.lote(ct, op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa", "sinActivacionMotivo": "Argos no responde desde la mañana"}))
	if r.Resultados[0].Error == nil || r.Resultados[0].Error.Status != 403 {
		t.Fatalf("técnico sin ordenes.cerrar_sin_activacion: %+v", r.Resultados[0])
	}
	// Un cierre parcial no exige la activación.
	f := deber(e.m.VerOrden.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	c := deber(e.m.CerrarOrden.Ejecutar(e.cs, campo.EntradaCerrarOrden{ID: o.ID, Version: f.Version, Resultado: "exitosa",
		SinActivacionMotivo: ptr("Argos no responde desde la mañana; la termina un agente")}))(t)
	if c.Estado != campo.EstadoCerrada {
		t.Fatalf("cerrada por el supervisor: %+v", c)
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'activar_instalacion'", strconv.FormatInt(o.ID, 10)); n != 0 {
		t.Fatalf("sin activar-instalacion: %d", n)
	}
	if st := e.activacion(o.ID); st.SinActivacionMotivo == nil || !st.Disponible {
		t.Fatalf("cerrada sin activación, un agente la termina: %+v", st)
	}
}

func TestEquipoYWifiDesdeLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	servicio := strconv.FormatInt(*o.ServicioID, 10)
	// Sin gestión: 409; Argos no responde: 503 «Reintentar».
	_, err := e.m.VerEquipo(ct, campo.EntradaEquipoOrden{ID: o.ID})
	problema(t, err, 409, "equipo_sin_gestion")
	e.falso.mu.Lock()
	e.falso.equipos[servicio] = "503"
	e.falso.mu.Unlock()
	_, err = e.m.VerEquipo(ct, campo.EntradaEquipoOrden{ID: o.ID})
	problema(t, err, 503, "equipo_sin_respuesta")
	e.falso.mu.Lock()
	e.falso.equipos[servicio] = `{"servicio_id":` + servicio + `,"tecnologia":"ftth","tipo":"ont","serie":"HWTC1234","en_linea":true,"senal":{"rx_dbm":-19.8,"tx_dbm":2.1,"clave":1},"fuente":"smartolt"}`
	e.falso.mu.Unlock()
	eq := deber(e.m.VerEquipo(ct, campo.EntradaEquipoOrden{ID: o.ID}))(t)
	if !eq.EnLinea || eq.Serie == nil || len(eq.Senal) != 2 {
		t.Fatalf("equipo: %+v", eq)
	}

	// Cambio de Wi-Fi del técnico en sitio: presencial, la clave solo viaja a Argos.
	cw := deber(e.m.CambiarWifi.Ejecutar(ct, campo.EntradaWifiOrden{ID: o.ID, Clave: ptr(claveWifiPrueba)}))(t)
	if cw.Metodo != equipos.MetodoPresencial || !cw.ConClave || cw.ConSSID {
		t.Fatalf("cambio: %+v", cw)
	}
	_, err = e.m.CambiarWifi.Ejecutar(ct, campo.EntradaWifiOrden{ID: o.ID, Clave: ptr("corta")})
	problema(t, err, 422, "validacion")
	e.procesar()
	e.relay()
	var l *llamadaArgos
	for _, x := range e.falso.recibidas() {
		if x.Ruta == "/servicios/"+servicio+"/wifi" {
			l = &x
		}
	}
	if l == nil {
		t.Fatal("no llegó el POST /wifi")
	}
	ver, _ := l.JSON["verificacion"].(map[string]any)
	if l.JSON["clave"] != claveWifiPrueba || ver["metodo"] != "presencial" || ver["cliente_id"] != 990.0 {
		t.Fatalf("POST /wifi: %+v", l)
	}
	lista := deber(e.m.CambiosWifi.Ejecutar(e.cs, campo.EntradaEquipoOrden{ID: o.ID}))(t)
	if len(lista) != 1 || lista[0].Estado != equipos.EstadoEnProceso {
		t.Fatalf("cambios: %+v", lista)
	}
	// M4: desde el panel, sin una verificación §7.5 vigente y sin usar, 422; con ella, un solo cambio.
	_, err = e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel"), Metodo: "llamada_verificada"})
	problema(t, err, 422, "verificacion_requerida")
	verif := verificacionDePrueba(t, 990)
	deber(e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel"), Metodo: "datos_titular", VerificacionID: &verif}))(t)
	// Métodos del PM: el código al teléfono o al correo aún no; presencial solo el técnico.
	_, err = e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel"), Metodo: "codigo_telefono"})
	problema(t, err, 422, "metodo_no_disponible")
	_, err = e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel"), Metodo: "codigo_correo"})
	problema(t, err, 422, "metodo_no_disponible")
	_, err = e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel"), Metodo: "presencial"})
	problema(t, err, 422, "validacion")
	_, err = e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel 2"), Metodo: "llamada_verificada", VerificacionID: &verif})
	problema(t, err, 422, "verificacion_requerida")
	otra := verificacionDePrueba(t, 991) // de otro cliente
	_, err = e.m.CambiarWifi.Ejecutar(e.cs, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Panel 3"), Metodo: "llamada_verificada", VerificacionID: &otra})
	problema(t, err, 422, "verificacion_requerida")
	// Tres por día: el cuarto no sale.
	deber(e.m.CambiarWifi.Ejecutar(ct, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Casa")}))(t)
	_, err = e.m.CambiarWifi.Ejecutar(ct, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Casa 3")})
	problema(t, err, 429, "demasiados_cambios_wifi")
	sinClaveWifi(t)
}

// eventoArgos deja un evento interno de Argos en el outbox (como lo deja el procesador del webhook).
func (e *entornoActivacion) eventoArgos(evento string, payload map[string]any) {
	e.t.Helper()
	crudo, _ := json.Marshal(payload)
	if _, err := pruebas.Pool(e.t).Exec(context.Background(), "INSERT INTO integracion.outbox (id, evento, empresa_id, payload) VALUES (gen_random_uuid(), $1, 1, $2)",
		evento, crudo); err != nil {
		e.t.Fatal(err)
	}
	e.relay()
}

func TestCobroDeLaInstalacion(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	e.m.SuscribirCobros(e.bus, pruebas.Pool(t))
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	// La plantilla trae el concepto y el precio del cobro, y además cobra al cerrar (el de antes). Al
	// terminar vuelve a como estaba (la comparten las demás pruebas del paquete).
	var cobrar *bool
	var monto *float64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT cobrar, cargo_monto::float8 FROM campo.plantillas_checklist
		WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, o.ID).Scan(&cobrar, &monto); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE campo.plantillas_checklist SET cobro_concepto_id = NULL, cobro_precio = NULL,
			cobrar = $2, cargo_monto = $3 WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, o.ID, cobrar, monto); err != nil {
			t.Error(err)
		}
	})
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE campo.plantillas_checklist SET cobro_concepto_id = 5, cobro_precio = 350,
		cobrar = true, cargo_monto = 100 WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, o.ID); err != nil {
		t.Fatal(err)
	}
	// El técnico no registra cobros; despacho no cambia el precio sin el permiso.
	_, err := e.m.RegistrarCobro.Ejecutar(ct, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 403, "prohibido")
	desp := usuarioCon(t, identidad.AsignacionRol{Rol: "despacho"})
	asignarEmpresa(t, desp, 1)
	cd := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), desp, 1, permisosDe("despacho")...)
	_, err = e.m.RegistrarCobro.Ejecutar(cd, campo.EntradaRegistrarCobro{ID: o.ID, PrecioUnitario: ptr("300.00"), MotivoPrecio: ptr("Descuento por promoción del mes")})
	problema(t, err, 403, "fuera_de_alcance")
	// Nunca Q0.
	_, err = e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, PrecioUnitario: ptr("0"), MotivoPrecio: ptr("Instalación de cortesía")})
	problema(t, err, 422, "validacion")
	c := deber(e.m.RegistrarCobro.Ejecutar(cd, campo.EntradaRegistrarCobro{ID: o.ID}))(t)
	if c.PrecioUnitario != "350.00" || c.ConceptoID != 5 || c.PrecioEditado || c.Estado != campo.CobroPendiente {
		t.Fatalf("cobro con el precio de la plantilla: %+v", c)
	}
	_, err = e.m.RegistrarCobro.Ejecutar(cd, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 409, "cobro_registrado")
	// Sale en el momento, con el cuerpo real de Argos y el actor.
	e.procesar()
	clave := "iris:cargo:" + orden + ":" + c.ID.String()
	l := e.falso.porClave(clave)
	if l == nil {
		t.Fatal("no llegó el cargo-unico")
	}
	lineas, _ := l.JSON["lineas"].([]any)
	if l.Actor != "usuario:"+strconv.FormatInt(desp, 10) || len(lineas) != 1 || l.JSON["monto"] != nil {
		t.Fatalf("cargo-unico: %+v", l)
	}
	if ln := lineas[0].(map[string]any); ln["concepto_id"] != 5.0 || ln["precio_unitario"] != "350.00" {
		t.Fatalf("línea: %+v", ln)
	}
	e.relay()
	cs := deber(e.m.VerCobros.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if cs[0].Estado != campo.CobroFacturado || cs[0].FacturaID == nil || *cs[0].FacturaID != 555 || cs[0].Numero == nil {
		t.Fatalf("facturado: %+v", cs[0])
	}
	// La orden sigue la factura (pago.registrado, dos veces: una sola transición).
	pago := map[string]any{"webhook": "pago.registrado", "factura_id": 555, "origen_id": orden, "empresa_id": 1}
	e.eventoArgos("argos.factura_cambiada", pago)
	e.eventoArgos("argos.factura_cambiada", pago)
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.cobro_cambiado' AND payload->>'orden_id' = $1 AND payload->>'a' = 'pagado'", orden); n != 1 {
		t.Fatalf("orden.cobro_cambiado a pagado: %d", n)
	}
	// El cierre no manda el cargo de instalación de la plantilla.
	esperar(t, e.lote(ct, trabajoDeInstalacion(o.ID)[2:]...), repetir(len(trabajoDeInstalacion(o.ID))-2, "aplicada")...)
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.cerrada' AND payload->>'orden_id' = $1 AND payload->'cargo' = 'null'::jsonb", orden); n != 1 {
		t.Fatalf("orden.cerrada sin cargo: %d", n)
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE idempotency_key = $1", "iris:cargo:"+orden); n != 0 {
		t.Fatalf("cargo de la plantilla al cerrar: %d", n)
	}
	// Anulada en Argos: se puede cobrar otra vez, con otro cargo_id y el precio cambiado (supervisor).
	e.eventoArgos("argos.factura_cambiada", map[string]any{"webhook": "factura.anulada", "factura_id": 555, "origen_id": orden, "empresa_id": 1})
	c2 := deber(e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, PrecioUnitario: ptr("300"), MotivoPrecio: ptr("Descuento autorizado por gerencia")}))(t)
	if c2.ID == c.ID || c2.PrecioUnitario != "300.00" || !c2.PrecioEditado {
		t.Fatalf("segundo cobro: %+v", c2)
	}
}

func TestBienvenidaEnLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	e.m.SuscribirBienvenidas(e.bus, pruebas.Pool(t))
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE campo.ajustes SET cierre_exige_bienvenida = true WHERE empresa_id = 1"); err != nil {
		t.Fatal(err)
	}
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	// Sin servicio activo, no se reenvía.
	_, err := e.m.ReenviarBienvenida.Ejecutar(e.cs, campo.EntradaReenviarBienvenida{ID: o.ID})
	problema(t, err, 409, "servicio_no_activo")
	esperar(t, e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(false))), "aplicada")
	e.procesar()
	e.relay()
	clave := pendientes.ClaveAprovisionar(o.ID, 1)
	deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), campo.EntradaResolverActivacion{Fuente: campo.FuenteWebhook, IdempotencyKey: &clave,
		Estado: campo.ActivacionLista}))(t)
	// Bandeja: activa y sin bienvenida.
	enBandeja := func() bool {
		pag := deber(e.m.BienvenidasSinEnviar.Ejecutar(e.cs, campo.EntradaBienvenidasSinEnviar{Limit: 100}))(t)
		for _, f := range pag.Data {
			if f.OrdenID == o.ID {
				return true
			}
		}
		return false
	}
	if !enBandeja() {
		t.Fatal("la instalación activa sin bienvenida no está en la bandeja")
	}
	// Cierre con «exigir lista» y la bienvenida exigida: aún no salió.
	trabajo := trabajoDeInstalacion(o.ID)[2:]
	r := e.lote(ct, trabajo...)
	if fin := r.Resultados[len(r.Resultados)-1]; fin.Error == nil || fin.Error.Codigo() != "bienvenida_requerida" {
		t.Fatalf("cierre sin bienvenida: %+v", fin)
	}
	// Reenviar: sale por la cola con la clave del contrato y el origen.
	b := deber(e.m.ReenviarBienvenida.Ejecutar(e.cs, campo.EntradaReenviarBienvenida{ID: o.ID, Canales: []string{"whatsapp"}}))(t)
	if b.Reenvios != 1 || b.Reenvio == nil {
		t.Fatalf("reenvío: %+v", b)
	}
	e.procesar()
	l := e.falso.porClave(pendientes.ClaveBienvenida(o.ID, 1))
	if l == nil || l.Ruta != "/servicios/"+strconv.FormatInt(*o.ServicioID, 10)+"/bienvenida/reenviar" || l.JSON["origen_id"] != orden {
		t.Fatalf("reenviar: %+v", l)
	}
	// Argos avisa por canal: correo fallido, WhatsApp enviada (dos webhooks, mezclados por canal).
	e.eventoArgos("argos.bienvenida_cambiada", map[string]any{"servicio_id": *o.ServicioID, "empresa_id": 1, "origen_tipo": "iris_orden",
		"origen_id": orden, "canales": []any{map[string]any{"canal": "correo", "estado": "fallida", "motivo": "sin_contacto"}}})
	// Sin origen: se relaciona por servicio y titular (una sola instalación en 7 días), pero no cuenta.
	e.eventoArgos("argos.bienvenida_cambiada", map[string]any{"servicio_id": *o.ServicioID, "empresa_id": 1, "cliente_id": 990, "origen_tipo": nil,
		"origen_id": nil, "canales": []any{map[string]any{"canal": "telegram", "estado": "enviada"}}})
	if b := deber(e.m.VerBienvenida.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t); b.Cumplida || len(b.Canales) != 2 || !b.Canales[1].Relacionada {
		t.Fatalf("relacionada por servicio: %+v", b)
	}
	e.eventoArgos("argos.bienvenida_cambiada", map[string]any{"servicio_id": *o.ServicioID, "empresa_id": 1, "origen_tipo": "iris_orden",
		"origen_id": orden, "canales": []any{map[string]any{"canal": "whatsapp", "estado": "enviada"}}})
	b = deber(e.m.VerBienvenida.Ejecutar(e.cs, campo.EntradaVerOrden{ID: o.ID}))(t)
	if !b.Cumplida || len(b.Canales) != 3 {
		t.Fatalf("bienvenida: %+v", b)
	}
	if enBandeja() {
		t.Fatal("la bienvenida enviada sigue en la bandeja")
	}
	esperar(t, e.lote(ct, op(campo.OpCerrar, o.ID, map[string]any{"resultado": "exitosa"})), "aplicada")
}

// A1: si el cierre ya pidió el cargo de la plantilla, registrar el cobro daría una segunda factura.
func TestCobroDespuesDelCierreConCargo(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoAlCerrar)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	var cobrar *bool
	var monto *float64
	if err := pruebas.Pool(t).QueryRow(context.Background(), `SELECT cobrar, cargo_monto::float8 FROM campo.plantillas_checklist
		WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, o.ID).Scan(&cobrar, &monto); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pruebas.Pool(t).Exec(context.Background(), `UPDATE campo.plantillas_checklist SET cobro_concepto_id = NULL, cobro_precio = NULL,
			cobrar = $2, cargo_monto = $3 WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, o.ID, cobrar, monto)
	})
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE campo.plantillas_checklist SET cobro_concepto_id = 5, cobro_precio = 350,
		cobrar = true, cargo_monto = 100 WHERE id = (SELECT plantilla_id FROM campo.ordenes WHERE id = $1)`, o.ID); err != nil {
		t.Fatal(err)
	}
	esperar(t, e.lote(ct, trabajoDeInstalacion(o.ID)[2:]...), repetir(len(trabajoDeInstalacion(o.ID))-2, "aplicada")...)
	e.relay()
	_, err := e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID})
	problema(t, err, 409, "cobro_al_cerrar")
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'cargo'", orden); n != 1 {
		t.Fatalf("pendientes de cargo: %d", n)
	}
	// Otro concepto que el de la plantilla exige el permiso y el motivo (despacho no lo tiene).
	o2 := e.instalacionPropia(tec, ct)
	desp := usuarioCon(t, identidad.AsignacionRol{Rol: "despacho"})
	asignarEmpresa(t, desp, 1)
	cd := pruebas.ContextoUsuarioEnEmpresa(pruebas.Pool(t), desp, 1, permisosDe("despacho")...)
	_, err = e.m.RegistrarCobro.Ejecutar(cd, campo.EntradaRegistrarCobro{ID: o2.ID, ConceptoID: ptr(int64(9))})
	problema(t, err, 403, "fuera_de_alcance")
}

// verificacionDePrueba: una verificación §7.5 que coincidió hace un momento (en una conversación).
func verificacionDePrueba(t *testing.T, cliente int64) int64 {
	t.Helper()
	pool := pruebas.Pool(t)
	ctx := context.Background()
	var canal, conv, id int64
	if err := pool.QueryRow(ctx, `INSERT INTO mensajeria.canales (empresa_id, tipo, canal, nombre, numero, phone_number_id, credenciales_cifradas)
		VALUES (1, 'whatsapp_cloud', 'whatsapp', $1, $2, $3, '\x00') RETURNING id`, "Verif "+pruebas.Unico(),
		"+50224"+pruebas.Unico()[:6], pruebas.Unico()).Scan(&canal); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO mensajeria.conversaciones (empresa_id, canal_id, numero, ultima_actividad, cliente_id)
		VALUES (1, $1, '+50255550009', now(), $2) RETURNING id`, canal, cliente).Scan(&conv); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO mensajeria.asistente_verificaciones (empresa_id, conversacion_id, cliente_id, resultado, creada_en)
		VALUES (1, $1, $2, 'coincide', now()) RETURNING id`, conv, cliente).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Bajo 6: un webhook de instalación de otro servicio no toca el intento.
func TestActivacionWebhookDeOtroServicio(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	esperar(t, e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(false))), "aplicada")
	clave := pendientes.ClaveAprovisionar(o.ID, 1)
	otro := *o.ServicioID + 1000
	r := deber(e.m.Activacion.Resolver.Ejecutar(sistema(t), campo.EntradaResolverActivacion{Fuente: campo.FuenteWebhook, IdempotencyKey: &clave,
		Estado: campo.ActivacionLista, ServicioID: &otro}))(t)
	if r.Resultado != "sin_cambios" || e.activacion(o.ID).Estado != campo.ActivacionPendiente {
		t.Fatalf("webhook de otro servicio: %+v", r)
	}
}

// Bajo 8: la clave Wi-Fi de un pendiente agotado se descarta a las 24 h (no espera 7 días).
func TestClaveWifiAgotadaSeDescarta(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	esperar(t, e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(true))), "aplicada")
	clave := pendientes.ClaveAprovisionar(o.ID, 1)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE integracion.pendientes SET estado = 'agotado', updated_at = now() - interval '25 hours'
		WHERE idempotency_key = $1`, clave); err != nil {
		t.Fatal(err)
	}
	e.purgar()
	if n := contar(t, "SELECT count(*) FROM integracion.datos_transito WHERE orden_id = $1 AND cifrado IS NOT NULL", o.ID); n != 0 {
		t.Fatalf("clave de un agotado de más de 24 h: %d", n)
	}
}

// 0.14.3: con capacidades.equipos, cada función solo con su marca (una empresa sin fila: todo false);
// sin el campo, como en la 0.14.2.
func TestCapacidadesEquipos(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoPendienteAgente)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	if _, err := pruebas.Pool(t).Exec(context.Background(), `UPDATE integracion.conectores SET capacidades_equipos =
		'[{"empresa_id":1,"estado_equipo":false,"wifi":false,"bienvenida":true,"cobro_equipo":false}]' WHERE sistema = 'argosisp'`); err != nil {
		t.Fatal(err)
	}
	_, err := e.m.VerEquipo(ct, campo.EntradaEquipoOrden{ID: o.ID})
	problema(t, err, 422, "capacidad_no_disponible")
	_, err = e.m.CambiarWifi.Ejecutar(ct, campo.EntradaWifiOrden{ID: o.ID, SSID: ptr("Casa")})
	problema(t, err, 422, "capacidad_no_disponible")
	_, err = e.m.RegistrarCobro.Ejecutar(e.cs, campo.EntradaRegistrarCobro{ID: o.ID, ConceptoID: ptr(int64(5)), PrecioUnitario: ptr("100"), MotivoPrecio: ptr("Cobro de prueba sin plantilla")})
	problema(t, err, 422, "capacidad_no_disponible")
	r := e.lote(ct, op(campo.OpActivarServicio, o.ID, datosActivacion(false)))
	if r.Resultados[0].Error == nil || r.Resultados[0].Error.Codigo() != "capacidad_no_disponible" {
		t.Fatalf("Wi-Fi inicial sin la marca: %+v", r.Resultados[0])
	}
	if _, err := pruebas.Pool(t).Exec(context.Background(), "UPDATE integracion.conectores SET capacidades_equipos = '[]' WHERE sistema = 'argosisp'"); err != nil {
		t.Fatal(err)
	}
	_, err = e.m.VerEquipo(ct, campo.EntradaEquipoOrden{ID: o.ID})
	problema(t, err, 422, "capacidad_no_disponible") // empresa sin fila
}

// 0.14.3: «activado fuera de la orden»: con exigir lista, cierra sin supervisor y sin comodato.
func TestActivadoFueraDeLaOrden(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (990, 1, 'Ana', now())
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, estado, sincronizado_en)
		VALUES ($1, 1, 990, 'activo', now()) ON CONFLICT (id) DO UPDATE SET estado = 'activo'`, *o.ServicioID); err != nil {
		t.Fatal(err)
	}
	// Bajo 3: solo la unidad que Argos tiene en el servicio (GET /servicios/:id/equipo) ya está en comodato.
	e.falso.mu.Lock()
	e.falso.equipos[strconv.FormatInt(*o.ServicioID, 10)] = `{"servicio_id":` + strconv.FormatInt(*o.ServicioID, 10) +
		`,"tecnologia":"ftth","serie":"hwtc1234","mac":"A4:5E:60:11:22:33","en_linea":true}`
	e.falso.mu.Unlock()
	esperar(t, e.lote(ct, trabajoDeInstalacion(o.ID)[2:]...), repetir(len(trabajoDeInstalacion(o.ID))-2, "aplicada")...)
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'orden.cerrada' AND payload->>'orden_id' = $1 AND payload->'activacion'->>'estado' = 'activado_fuera'", orden); n != 1 {
		t.Fatalf("activado_fuera: %d", n)
	}
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion IN ('activar_instalacion', 'asignar_comodato')", orden); n != 0 {
		t.Fatalf("activación o comodato al cerrar: %d", n)
	}
}

// Bajo 3: activado fuera de la orden con otra unidad en Argos: el equipo de la orden sí lleva comodato.
func TestActivadoFueraConOtraUnidad(t *testing.T) {
	e := nuevoEntornoActivacion(t, campo.ModoExigirLista)
	tec, ct := e.tecnicoEnEmpresa()
	o := e.instalacionPropia(tec, ct)
	orden := strconv.FormatInt(o.ID, 10)
	pool := pruebas.Pool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_clientes (id, empresa_id, nombre, sincronizado_en) VALUES (990, 1, 'Ana', now())
		ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO integracion.argos_servicios (id, empresa_id, cliente_id, estado, sincronizado_en)
		VALUES ($1, 1, 990, 'activo', now()) ON CONFLICT (id) DO UPDATE SET estado = 'activo'`, *o.ServicioID); err != nil {
		t.Fatal(err)
	}
	e.falso.mu.Lock()
	e.falso.equipos[strconv.FormatInt(*o.ServicioID, 10)] = `{"servicio_id":` + strconv.FormatInt(*o.ServicioID, 10) +
		`,"tecnologia":"ftth","serie":"OTRA-999","mac":"00:11:22:33:44:55","en_linea":true}`
	e.falso.mu.Unlock()
	esperar(t, e.lote(ct, trabajoDeInstalacion(o.ID)[2:]...), repetir(len(trabajoDeInstalacion(o.ID))-2, "aplicada")...)
	e.relay()
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'asignar_comodato'", orden); n != 1 {
		t.Fatalf("comodato del equipo que Argos no tiene: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM integracion.pendientes WHERE entidad_id = $1 AND operacion = 'activar_instalacion'", orden); n != 0 {
		t.Fatalf("sin activar-instalacion: %d", n)
	}
}
