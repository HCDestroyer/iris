package campo_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Tomar una orden libre: apagado por defecto (403), el administrador lo enciende, el técnico ve las
// de su cuadrilla y sus zonas por distancia, y de dos que la toman a la vez uno gana y el otro
// recibe 409 con quién.
func TestTomarOrdenLibre(t *testing.T) {
	const empresa = 68
	registrarEmpresas(t, empresa)
	e := nuevoEntorno()
	pool := pruebas.Pool(t)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	zonaDe(t, 681, empresa)
	zonaDe(t, 682, empresa)
	var cuadrilla int64
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO agenda.cuadrillas (empresa_id, nombre) VALUES ($1, 'Norte ' || $2) RETURNING id", empresa, pruebas.Unico()).Scan(&cuadrilla); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO agenda.cuadrilla_zonas (cuadrilla_id, zona_id) VALUES ($1, 681)", cuadrilla)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM campo.ajustes WHERE empresa_id = $1", empresa)
		_, _ = pool.Exec(context.Background(), "DELETE FROM campo.posiciones_ultimas WHERE empresa_id = $1", empresa)
	})

	d := despacho(t)
	asignarEmpresa(t, d, empresa)
	cd := pruebas.ContextoUsuarioEnEmpresa(pool, d, empresa, permisosDe("despacho")...)
	tecnicoDe := func() (int64, context.Context) {
		id := usuarioCon(t, identidad.AsignacionRol{Rol: "tecnico"})
		asignarEmpresa(t, id, empresa)
		exec("INSERT INTO agenda.tecnicos (usuario_id, cuadrilla_id, bodega_id) VALUES ($1, $2, 900)", id, cuadrilla)
		return id, contexto.ConCanal(pruebas.ContextoUsuarioEnEmpresa(pool, id, empresa, permisosDe("tecnico")...), contexto.CanalCampo)
	}
	a, ca := tecnicoDe()
	b, cb := tecnicoDe()

	nueva := func(zona int64, lat, lon *float64) campo.Orden {
		t.Helper()
		return deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{Tipo: "visita", Direccion: ptr("Zona norte"), ZonaID: &zona,
			Lat: lat, Lon: lon, Descripcion: "Libre"}))(t)
	}
	cerca := nueva(681, ptr(14.601), ptr(-90.501))
	lejos := nueva(681, ptr(14.650), ptr(-90.550))
	sinCoordenadas := nueva(681, nil, nil)
	otraZona := nueva(682, ptr(14.600), ptr(-90.500))

	// Apagado por defecto: 403 y el paquete no ofrece la opción.
	_, err := e.m.OrdenesLibres.Ejecutar(ca, campo.EntradaOrdenesLibres{})
	problema(t, err, 403, "libres_apagado")
	_, err = e.m.TomarOrden.Ejecutar(ca, campo.EntradaTomarOrden{ID: cerca.ID})
	problema(t, err, 403, "libres_apagado")
	if p := deber(e.m.Paquete(ca, campo.EntradaPaquete{}))(t); p.TomarLibres {
		t.Fatal("el paquete ofrece tomar libres con el ajuste apagado")
	}

	// Solo el administrador (campo.configurar) lo enciende; emite campo.ajustes_cambiados.
	_, err = e.m.FijarAjustes.Ejecutar(cd, campo.EntradaFijarAjustes{TecnicosTomanLibres: true})
	problema(t, err, 403, "prohibido")
	admin := usuarioCon(t, identidad.AsignacionRol{Rol: "admin"})
	cadm := pruebas.ContextoUsuarioEnEmpresa(pool, admin, empresa, permisosDe("admin")...)
	if v := deber(e.m.VerAjustes.Ejecutar(cadm, struct{}{}))(t); !v.PorDefecto || v.TecnicosTomanLibres {
		t.Fatalf("por defecto: %+v", v)
	}
	aj := deber(e.m.FijarAjustes.Ejecutar(cadm, campo.EntradaFijarAjustes{TecnicosTomanLibres: true}))(t)
	if !aj.TecnicosTomanLibres || aj.Version != 1 || aj.PorDefecto {
		t.Fatalf("ajustes: %+v", aj)
	}
	_, err = e.m.FijarAjustes.Ejecutar(cadm, campo.EntradaFijarAjustes{TecnicosTomanLibres: true})
	problema(t, err, 409, "conflicto_version")
	if n := contar(t, "SELECT count(*) FROM integracion.outbox WHERE evento = 'campo.ajustes_cambiados' AND (payload->>'empresa_id')::bigint = $1", empresa); n != 1 {
		t.Fatalf("campo.ajustes_cambiados: %d", n)
	}
	if p := deber(e.m.Paquete(ca, campo.EntradaPaquete{}))(t); !p.TomarLibres {
		t.Fatal("el paquete no ofrece tomar libres")
	}

	// Sin posición: por fecha (las de su cuadrilla y zonas; la otra zona no sale).
	l := deber(e.m.OrdenesLibres.Ejecutar(ca, campo.EntradaOrdenesLibres{}))(t)
	if l.Orden != "fecha" || l.Total != 3 || len(l.Data) != 3 || len(l.Zonas) != 1 || l.Zonas[0].ID != 681 {
		t.Fatalf("libres por fecha: %+v", l)
	}
	// Con posición: la más cercana primero; las sin coordenadas al final. Paginado por cursor.
	exec(`INSERT INTO campo.posiciones_ultimas (tecnico_id, empresa_id, lat, lon, registrada_en, recibida_en)
		VALUES ($1, $2, 14.600, -90.500, now(), now())`, a, empresa)
	l = deber(e.m.OrdenesLibres.Ejecutar(ca, campo.EntradaOrdenesLibres{ParamsPagina: nucleo.ParamsPagina{Limit: 2}}))(t)
	if l.Orden != "distancia" || len(l.Data) != 2 || l.Data[0].ID != cerca.ID || l.Data[1].ID != lejos.ID || l.NextCursor == nil ||
		l.Data[0].DistanciaM == nil || *l.Data[0].DistanciaM > 200 {
		t.Fatalf("libres por distancia: %+v", l)
	}
	resto := deber(e.m.OrdenesLibres.Ejecutar(ca, campo.EntradaOrdenesLibres{ParamsPagina: nucleo.ParamsPagina{Limit: 2, Cursor: *l.NextCursor}}))(t)
	if len(resto.Data) != 1 || resto.Data[0].ID != sinCoordenadas.ID || resto.NextCursor != nil || resto.Data[0].DistanciaM != nil {
		t.Fatalf("segunda página: %+v", resto)
	}

	// Fuera de sus zonas: 404 (no se revela).
	_, err = e.m.TomarOrden.Ejecutar(ca, campo.EntradaTomarOrden{ID: otraZona.ID})
	problema(t, err, 404, "no_encontrado")

	// Carrera: los dos la toman a la vez; uno gana y el otro recibe 409 con quién.
	var wg sync.WaitGroup
	resultados := make([]error, 2)
	for i, ctx := range []context.Context{ca, cb} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, resultados[i] = e.m.TomarOrden.Ejecutar(ctx, campo.EntradaTomarOrden{ID: cerca.ID})
		}()
	}
	wg.Wait()
	ganador, perdedor := a, resultados[1]
	if resultados[0] != nil {
		ganador, perdedor = b, resultados[0]
		if resultados[1] != nil {
			t.Fatalf("ninguno la tomó: %v / %v", resultados[0], resultados[1])
		}
	}
	var p *nucleo.Problema
	if !errors.As(perdedor, &p) || p.Status != 409 || p.Codigo() != "orden_ya_tomada" || p.TomadaPor == nil ||
		p.TomadaPor.ID != ganador || p.TomadaPor.Nombre == "" {
		t.Fatalf("el segundo: %v (%+v)", perdedor, p)
	}
	o := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: cerca.ID}))(t)
	if o.TecnicoID == nil || *o.TecnicoID != ganador || o.BodegaID == nil || *o.BodegaID != 900 || o.Estado != campo.EstadoPendiente {
		t.Fatalf("tomada: %+v", o.Orden)
	}
	ev := ultimo(t, eventosDe(t, cerca.ID), "orden.asignada")
	if ev["origen"] != campo.OrigenTomar || ev["motivo"] != "tomada" || int64(ev["tecnico_id"].(float64)) != ganador {
		t.Fatalf("orden.asignada: %+v", ev)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.tomar' AND entidad_id = $1::bigint::text", cerca.ID); n != 3 {
		t.Fatalf("auditoría de tomar (el 403 con el ajuste apagado, el que ganó y el 409): %d", n)
	}
	// Reintento del que ganó: 200 sin cambios. Ya no sale en la lista de nadie.
	ctxGanador := ca
	if ganador == b {
		ctxGanador = cb
	}
	if o2 := deber(e.m.TomarOrden.Ejecutar(ctxGanador, campo.EntradaTomarOrden{ID: cerca.ID}))(t); o2.Version != o.Version {
		t.Fatalf("reintento: %+v", o2)
	}
	if l := deber(e.m.OrdenesLibres.Ejecutar(ca, campo.EntradaOrdenesLibres{}))(t); l.Total != 2 {
		t.Fatalf("quedan: %+v", l)
	}
	// Despacho (sin ordenes.tomar) no usa esta lectura.
	_, err = e.m.OrdenesLibres.Ejecutar(cd, campo.EntradaOrdenesLibres{})
	problema(t, err, 403, "prohibido")
}
