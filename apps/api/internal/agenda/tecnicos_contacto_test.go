package agenda_test

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Teléfono de la ficha (crear, editar, validar, auditar y emitir) y candidatos a ficha.
func TestTelefonoYCandidatosDeTecnico(t *testing.T) {
	e := nuevoEntorno()
	sup := usuarioCon(t, rol("supervisor"))
	cs := como(t, sup, "supervisor")
	c := deber(e.ag.CrearCuadrilla.Ejecutar(cs, agenda.EntradaCrearCuadrilla{Nombre: "Cuadrilla contacto " + pruebas.Unico()}))(t)

	tec := usuarioCon(t, rol("tecnico"))
	ventas := usuarioCon(t, rol("ventas"))

	// Candidatos: el técnico sin ficha sí; ventas (sin permisos de órdenes) no.
	tiene := func(p []agenda.CandidatoTecnico, id int64) bool {
		for _, x := range p {
			if x.UsuarioID == id {
				return true
			}
		}
		return false
	}
	todos := func(q string) []agenda.CandidatoTecnico {
		var salida []agenda.CandidatoTecnico
		cursor := ""
		for {
			p := deber(e.ag.CandidatosTecnico.Ejecutar(cs, agenda.EntradaCandidatosTecnico{Q: q, ParamsPagina: paginaCon(cursor)}))(t)
			salida = append(salida, p.Data...)
			if p.NextCursor == nil {
				return salida
			}
			cursor = *p.NextCursor
		}
	}
	lista := todos("")
	if !tiene(lista, tec) || tiene(lista, ventas) {
		t.Fatalf("candidatos: técnico %v, ventas %v", tiene(lista, tec), tiene(lista, ventas))
	}
	// Sin el permiso de crear fichas → 403.
	_, err := e.ag.CandidatosTecnico.Ejecutar(como(t, tec, "tecnico"), agenda.EntradaCandidatosTecnico{})
	problema(t, err, 403, "prohibido")

	// Teléfono inválido → 422 en el campo.
	_, err = e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &c.ID, Telefono: "llámame"})
	if p := problema(t, err, 422, "validacion"); p.Errors[0].Field != "telefono" {
		t.Fatalf("teléfono inválido: %+v", p.Errors)
	}
	_, err = e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &c.ID, Telefono: "123"})
	problema(t, err, 422, "validacion")

	f := deber(e.ag.CrearTecnico.Ejecutar(cs, agenda.EntradaCrearTecnico{UsuarioID: tec, CuadrillaID: &c.ID, Telefono: " +502 5555-1234 "}))(t)
	if f.Telefono != "+502 5555-1234" {
		t.Fatalf("teléfono al crear: %q", f.Telefono)
	}
	// Ya tiene ficha: deja de ser candidato.
	if tiene(todos(""), tec) {
		t.Fatal("con ficha sigue como candidato")
	}

	// Editar solo el teléfono: emite tecnico.actualizado con campos [telefono] y lo audita.
	f2 := deber(e.ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, CuadrillaID: &c.ID,
		Habilidades: f.Habilidades, Telefono: "(502) 4444 0000"}))(t)
	if f2.Telefono != "(502) 4444 0000" || f2.Version != f.Version+1 {
		t.Fatalf("teléfono editado: %+v", f2)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'tecnico.actualizado'
		AND (payload->>'tecnico_id')::bigint = $1 AND payload->'campos' = '["telefono"]'::jsonb`, f.ID); n != 1 {
		t.Fatalf("evento con campos [telefono]: %d", n)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios WHERE entidad = 'tecnico' AND entidad_id = $1::bigint::text AND campo ILIKE 'telefono'`, f.ID); n < 1 {
		t.Fatalf("auditoría del teléfono: %d", n)
	}
	// La lista también lo trae.
	l := deber(e.ag.ListarTecnicos.Ejecutar(cs, agenda.EntradaListarTecnicos{UsuarioID: tec}))(t)
	if len(l.Data) != 1 || l.Data[0].Telefono != "(502) 4444 0000" {
		t.Fatalf("lista: %+v", l.Data)
	}
	// Mandar el mismo teléfono (y lo demás igual) → sin cambios.
	_, err = e.ag.EditarTecnico.Ejecutar(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f2.Version, CuadrillaID: &c.ID,
		Habilidades: f.Habilidades, Telefono: "(502) 4444 0000"})
	problema(t, err, 422, "sin_cambios")
}

func paginaCon(cursor string) nucleo.ParamsPagina {
	return nucleo.ParamsPagina{Cursor: cursor, Limit: 200}
}
