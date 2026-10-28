package campo_test

import (
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de seguridad (PR #20), punto 1: un lote rechazado también gasta cupo (el contador no
// se revierte con la acción) y su auditoría no lleva coordenadas, solo cuántos puntos traía.
func TestRevisionLotesRechazadosCuentanYNoAuditanCoordenadas(t *testing.T) {
	e := nuevoEntorno()
	tec := tecnico(t)
	ct := enCampo(como(t, tec, "tecnico"))
	ahora := e.reloj.ahora()
	malos := make([]campo.PuntoPosicion, 500)
	for i := range malos {
		malos[i] = punto(ahora.Add(-time.Duration(i)*time.Second), 95) // latitud fuera de rango
	}
	rechazos := map[int]int{}
	for range 20 {
		_, err := e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{DispositivoID: "tel-malo", Puntos: malos})
		var p interface{ GetStatus() int }
		if errorsAsStatus(err, &p) {
			rechazos[p.GetStatus()]++
		}
	}
	if rechazos[422] != 6 || rechazos[429] != 14 {
		t.Fatalf("los lotes inválidos cuentan para el límite: %v", rechazos)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'campo.registrar_posiciones' AND actor_id = $1", tec); n != 6 {
		t.Fatalf("filas de auditoría: %d (solo los 422; el 429 no se audita)", n)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'campo.registrar_posiciones' AND actor_id = $1
		AND entrada->>'puntos' = '500' AND entrada::text NOT LIKE '%lat%' AND length(entrada::text) < 200`, tec); n != 6 {
		t.Fatalf("la auditoría resume el lote ({puntos: 500}, sin coordenadas): %d", n)
	}
	// Pasado el minuto vuelve a entrar.
	e.reloj.avanzar(61 * time.Second)
	deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{punto(e.reloj.ahora(), 14.6)}}))(t)
}

// Punto 2: el intervalo mínimo se mide contra lo ya guardado, no solo dentro del lote: seis
// lotes de 500 puntos corridos 1 µs no meten 3000 puntos.
func TestRevisionIntervaloMinimoEntreLotes(t *testing.T) {
	e := nuevoEntorno()
	tec := tecnico(t)
	ct := enCampo(como(t, tec, "tecnico"))
	base := e.reloj.ahora().Add(-time.Minute)
	aceptadas, frecuencia := 0, 0
	for lote := range 6 {
		puntos := make([]campo.PuntoPosicion, 500)
		for i := range puntos {
			puntos[i] = punto(base.Add(time.Duration(lote*500+i)*time.Microsecond), 14.6)
		}
		r := deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: puntos}))(t)
		aceptadas += r.Aceptadas
		frecuencia += r.Motivos["frecuencia"]
	}
	if aceptadas != 1 || frecuencia != 2999 || contar(t, "SELECT count(*) FROM campo.posiciones WHERE tecnico_id = $1", tec) != 1 {
		t.Fatalf("aceptadas %d, frecuencia %d", aceptadas, frecuencia)
	}
	// Un punto anterior pero a menos del intervalo de uno guardado también es «frecuencia»; uno
	// lejos entra; repetir la hora exacta es «duplicada».
	e.reloj.avanzar(61 * time.Second)
	r := deber(e.m.RegistrarPosiciones.Ejecutar(ct, campo.EntradaPosiciones{Puntos: []campo.PuntoPosicion{
		punto(base.Add(-2*time.Second), 14.6), punto(base.Add(-30*time.Second), 14.6), punto(base, 14.6)}}))(t)
	if r.Aceptadas != 1 || r.Motivos["frecuencia"] != 1 || r.Duplicadas != 1 {
		t.Fatalf("contra lo guardado: %+v", r)
	}
}

// Punto 6: con alcance por zona no se ven las posiciones de técnicos de una cuadrilla sin zonas.
func TestRevisionPosicionesCuadrillaSinZonas(t *testing.T) {
	e := nuevoEntorno()
	ag := e.m.Agenda()
	cs := como(t, usuarioCon(t, rolDe("supervisor")), "supervisor")
	z := deber(ag.CrearZona.Ejecutar(cs, agenda.EntradaCrearZona{Nombre: "Zona sin cuadrilla " + pruebas.Unico()}))(t)
	c := deber(ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Sin zonas " + pruebas.Unico()}))(t)
	tec := tecnico(t)
	f := deber(ag.ListarTecnicos.Ejecutar(cs, agenda.EntradaListarTecnicos{UsuarioID: tec}))(t).Data[0]
	deber(ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, CuadrillaID: &c.ID}))(t)
	deber(e.m.RegistrarPosiciones.Ejecutar(enCampo(como(t, tec, "tecnico")), campo.EntradaPosiciones{
		Puntos: []campo.PuntoPosicion{punto(e.reloj.ahora(), 14.6)}}))(t)
	desp := despacho(t)
	porZona := conAlcance(t, desp, map[string][]contexto.Alcance{"campo.ver_posiciones": {{ZonaID: z.ID}}})
	if u := deber(e.m.UltimasPosiciones.Ejecutar(porZona, campo.EntradaUltimasPosiciones{TecnicoID: tec}))(t); len(u.Data) != 0 {
		t.Fatalf("una cuadrilla sin zonas no la ve el alcance por zona: %+v", u.Data)
	}
	if u := deber(e.m.UltimasPosiciones.Ejecutar(como(t, desp, "despacho"), campo.EntradaUltimasPosiciones{TecnicoID: tec}))(t); len(u.Data) != 1 {
		t.Fatalf("el alcance global sí: %+v", u.Data)
	}
}

func errorsAsStatus(err error, p *interface{ GetStatus() int }) bool {
	if err == nil {
		return false
	}
	s, ok := err.(interface{ GetStatus() int })
	*p = s
	return ok
}
