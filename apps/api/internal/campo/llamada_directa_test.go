package campo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// llamada.directa del lote (E4c fase 3, contratos/sync.md §2): «Llamar desde mi celular» sin señal se
// anota en la orden al volver la señal, con la hora real, el número que pone el servidor y el mismo
// llamada.directa_registrada que POST /telefono/llamada-directa.

func conDirectas(c *campo.Config) { c.LlamadasDirectas = mensajeria.LlamadasDirectas{} }

// ordenConContacto: una visita programada para el técnico, con contacto en sitio, asignada una hora
// antes de inicio (el reloj queda en inicio).
func ordenConContacto(t *testing.T, e entorno, cd context.Context, tec int64) campo.Orden {
	t.Helper()
	e.reloj.fijar(inicio.Add(-time.Hour))
	asegurarZona(t, 3)
	o := deber(e.m.CrearOrden.Ejecutar(cd, campo.EntradaCrearOrden{
		Tipo: "visita", Direccion: ptr("Zona 10, 5a avenida 12-34"), Lat: ptr(14.6), Lon: ptr(-90.5), ZonaID: ptr(int64(3)),
		Descripcion: "Sin internet", Contacto: &campo.EntradaContactoOrden{Nombre: ptr("María"), Telefono: ptr("5512 3480")},
	}))(t)
	// El siguiente hueco libre del técnico (como `programada`): la agenda no deja que se crucen.
	for {
		huecos.Lock()
		n := huecos.siguiente[tec]
		huecos.siguiente[tec] = n + 1
		huecos.Unlock()
		p, err := e.m.ProgramarOrden.Ejecutar(cd, campo.EntradaProgramarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tec,
			Inicio: inicio.Add(24*time.Hour + time.Duration(n)*2*time.Hour)})
		var pr *nucleo.Problema
		if n < 12 && errors.As(err, &pr) && pr.Codigo() == "solape" {
			continue
		}
		o = deber(p, err)(t)
		break
	}
	e.reloj.fijar(inicio)
	return o
}

func directa(orden int64, en *time.Time) campo.OperacionLote {
	o := op(campo.OpLlamadaDirecta, orden, nil)
	o.OcurridoEn = en
	return o
}

func TestLoteLlamadaDirecta(t *testing.T) {
	e := nuevoEntorno(conDirectas)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := ordenConContacto(t, e, cd, tec)

	en := inicio.Add(-10 * time.Minute)
	llamada := directa(o.ID, &en)
	r := lote(t, e, ct, llamada)
	esperar(t, r, "aplicada")
	if r.Resultados[0].LlamadaID == nil || r.Resultados[0].LlamadaID.String() != llamada.ID {
		t.Fatalf("la llamada lleva el id de la operación: %+v", r.Resultados[0])
	}
	// La orden no cambia (ni su versión).
	if r.Resultados[0].Orden == nil || r.Resultados[0].Orden.Version != o.Version {
		t.Fatalf("orden: %+v (versión %d)", r.Resultados[0].Orden, o.Version)
	}
	var origen, numero, motivo string
	var sono time.Time
	var usuario, orden int64
	if err := pruebas.Pool(t).QueryRow(context.Background(),
		"SELECT origen, numero, motivo_fin, sono_en, usuario_id, orden_id FROM mensajeria.llamadas WHERE id = $1",
		uuid.MustParse(llamada.ID)).Scan(&origen, &numero, &motivo, &sono, &usuario, &orden); err != nil {
		t.Fatal(err)
	}
	if origen != "celular" || numero != "+50255123480" || motivo != "directa" || !sono.Equal(en) || usuario != tec || orden != o.ID {
		t.Fatalf("fila: %s %s %s %s %d %d", origen, numero, motivo, sono, usuario, orden)
	}
	ev := ultimo(t, eventosDe(t, o.ID), "llamada.directa_registrada")
	if ev == nil || ev["llamada_id"] != llamada.ID || ev["origen"] != "celular" || ev["usuario_id"] != float64(tec) {
		t.Fatalf("evento: %v", ev)
	}
	for _, v := range ev {
		if s, ok := v.(string); ok && s == "+50255123480" {
			t.Fatal("el evento nunca lleva el número")
		}
	}

	// La misma operación otra vez (el teléfono no vio la respuesta): duplicada, una sola fila.
	esperar(t, lote(t, e, ct, llamada), "duplicada")
	if n := contar(t, "SELECT count(*) FROM mensajeria.llamadas WHERE orden_id = $1", o.ID); n != 1 {
		t.Fatalf("filas: %d", n)
	}
}

func TestLoteLlamadaDirectaReglas(t *testing.T) {
	e := nuevoEntorno(conDirectas)
	d, tec, otro, tercero := despacho(t), tecnico(t), tecnico(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := ordenConContacto(t, e, cd, tec)

	antes := inicio.Add(-2 * time.Hour) // antes de la asignación (una hora antes de inicio)
	futuro := inicio.Add(10 * time.Minute)
	tolerada := inicio.Add(-time.Hour - 4*time.Minute)
	// Sin hora, futura o anterior a la asignación: rechazadas. Con la tolerancia de 5 min: aplicada.
	// Una llamada rechazada no bloquea la orden: la nota que sigue se aplica.
	r := lote(t, e, ct, directa(o.ID, nil), directa(o.ID, &futuro), directa(o.ID, &antes), directa(o.ID, &tolerada),
		op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "Llamé y no contestó"}))
	esperar(t, r, "rechazada:validacion", "rechazada:hora_invalida", "rechazada:hora_invalida", "aplicada", "aplicada")

	// Una orden que nunca fue suya: 404; reasignada: orden_reasignada.
	ajena := ordenConContacto(t, e, cd, otro)
	en := inicio.Add(-time.Minute)
	esperar(t, lote(t, e, ct, directa(ajena.ID, &en)), "rechazada:no_encontrado")
	o = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t).Orden
	deber(e.m.AsignarOrden.Ejecutar(cd, campo.EntradaAsignarOrden{ID: o.ID, Version: o.Version, TecnicoID: &tercero}))(t)
	esperar(t, lote(t, e, ct, directa(o.ID, &en)), "rechazada:orden_reasignada")
}

func TestLoteLlamadaDirectaSinMensajeria(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := ordenConContacto(t, e, cd, tec)
	en := inicio.Add(-time.Minute)
	esperar(t, lote(t, e, ct, directa(o.ID, &en)), "rechazada:no_disponible")
}

// Revisión de #258, B1: a lo más 20 llamadas directas por orden, técnico y día; la 21 se rechaza con
// limite_llamadas (y no deja fila).
func TestLoteLlamadaDirectaTope(t *testing.T) {
	e := nuevoEntorno(conDirectas)
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := ordenConContacto(t, e, cd, tec)
	ops := make([]campo.OperacionLote, 0, 21)
	quiero := make([]string, 0, 21)
	for i := range 21 {
		en := inicio.Add(-time.Duration(i+1) * time.Minute)
		ops = append(ops, directa(o.ID, &en))
		quiero = append(quiero, "aplicada")
	}
	quiero[20] = "rechazada:limite_llamadas"
	esperar(t, lote(t, e, ct, ops...), quiero...)
	if n := contar(t, "SELECT count(*) FROM mensajeria.llamadas WHERE orden_id = $1", o.ID); n != 20 {
		t.Fatalf("filas: %d", n)
	}
}
