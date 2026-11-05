package campo_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/campo"
)

func TestNotasEditarYEliminar(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)
	o = deber(e.m.AgregarNota.Ejecutar(ct, campo.EntradaAgregarNota{ID: o.ID, Texto: "Portón negro"}))(t)
	o = deber(e.m.AgregarNota.Ejecutar(cd, campo.EntradaAgregarNota{ID: o.ID, Texto: "Perro en el patio"}))(t)
	det := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if len(det.Notas) != 2 || det.Notas[0].Version != 1 || det.Notas[0].EditadaEn != nil {
		t.Fatalf("notas: %+v", det.Notas)
	}
	propia, deDespacho := det.Notas[0], det.Notas[1]

	// El autor la edita: sube la versión de la nota y de la orden, y audita el diff del texto.
	o2 := deber(e.m.EditarNota.Ejecutar(ct, campo.EntradaEditarNota{ID: o.ID, NotaID: propia.ID, Version: 1, Texto: "Portón negro, timbre izquierdo"}))(t)
	if o2.Version <= o.Version {
		t.Fatalf("la orden sube de versión: %d → %d", o.Version, o2.Version)
	}
	det = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if det.Notas[0].Texto != "Portón negro, timbre izquierdo" || det.Notas[0].Version != 2 || det.Notas[0].EditadaEn == nil {
		t.Fatalf("editada: %+v", det.Notas[0])
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios WHERE entidad = 'orden_nota' AND entidad_id = $1 AND campo ILIKE 'texto'
		AND antes::text LIKE '%Portón negro%' AND despues::text LIKE '%timbre izquierdo%'`, propia.ID.String()); n != 1 {
		t.Fatalf("diff del texto en la auditoría: %d", n)
	}
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.editar_nota' AND resultado = 'ok'"); n < 1 {
		t.Fatal("sin auditoría de editar_nota")
	}

	// Con la versión vieja: 409. El mismo texto no cambia nada.
	_, err := e.m.EditarNota.Ejecutar(ct, campo.EntradaEditarNota{ID: o.ID, NotaID: propia.ID, Version: 1, Texto: "Otra cosa"})
	problema(t, err, 409, "conflicto_version")
	o3 := deber(e.m.EditarNota.Ejecutar(ct, campo.EntradaEditarNota{ID: o.ID, NotaID: propia.ID, Version: 2, Texto: "Portón negro, timbre izquierdo"}))(t)
	if o3.Version != o2.Version {
		t.Fatalf("sin cambios no sube la versión: %d → %d", o2.Version, o3.Version)
	}

	// El técnico no edita ni elimina la de despacho (403); despacho sí la del técnico (ordenes.asignar).
	_, err = e.m.EditarNota.Ejecutar(ct, campo.EntradaEditarNota{ID: o.ID, NotaID: deDespacho.ID, Version: 1, Texto: "No es mía"})
	problema(t, err, 403, "nota_ajena")
	_, err = e.m.EliminarNota.Ejecutar(ct, campo.EntradaEliminarNota{ID: o.ID, NotaID: deDespacho.ID, Version: 1})
	problema(t, err, 403, "nota_ajena")
	deber(e.m.EditarNota.Ejecutar(cd, campo.EntradaEditarNota{ID: o.ID, NotaID: propia.ID, Version: 2, Texto: "Portón negro (corregido por despacho)"}))(t)

	// Un técnico que no ve la orden: 404 (no revela la nota). Una nota que no es de la orden: 404.
	otro := como(t, tecnico(t), "tecnico")
	_, err = e.m.EditarNota.Ejecutar(otro, campo.EntradaEditarNota{ID: o.ID, NotaID: propia.ID, Version: 3, Texto: "x"})
	problema(t, err, 404, "no_encontrado")
	_, err = e.m.EliminarNota.Ejecutar(ct, campo.EntradaEliminarNota{ID: o.ID, NotaID: uuid.Must(uuid.NewV7()), Version: 1})
	problema(t, err, 404, "nota_no_encontrada")

	// Eliminar: sale de la orden, queda la fila con quién y cuándo; repetir no cambia nada; editarla, 409.
	o4 := deber(e.m.EliminarNota.Ejecutar(ct, campo.EntradaEliminarNota{ID: o.ID, NotaID: propia.ID, Version: 3}))(t)
	det = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if len(det.Notas) != 1 || det.Notas[0].ID != deDespacho.ID {
		t.Fatalf("la eliminada sale de la orden: %+v", det.Notas)
	}
	if n := contar(t, "SELECT count(*) FROM campo.orden_notas WHERE id = $1 AND eliminada_en IS NOT NULL AND eliminada_por = $2", propia.ID, tec); n != 1 {
		t.Fatal("eliminar es lógico: queda la fila con quién la eliminó")
	}
	o5 := deber(e.m.EliminarNota.Ejecutar(ct, campo.EntradaEliminarNota{ID: o.ID, NotaID: propia.ID, Version: 4}))(t)
	if o5.Version != o4.Version {
		t.Fatalf("eliminar de nuevo no cambia nada: %d → %d", o4.Version, o5.Version)
	}
	_, err = e.m.EditarNota.Ejecutar(ct, campo.EntradaEditarNota{ID: o.ID, NotaID: propia.ID, Version: 4, Texto: "x"})
	problema(t, err, 409, "nota_eliminada")

	// En cualquier estado: también cancelada (evidencia).
	deber(e.m.CancelarOrden.Ejecutar(cd, campo.EntradaMotivo{ID: o.ID, Version: o5.Version, Motivo: "Cliente desistió"}))(t)
	deber(e.m.EditarNota.Ejecutar(cd, campo.EntradaEditarNota{ID: o.ID, NotaID: deDespacho.ID, Version: 1, Texto: "Perro bravo en el patio"}))(t)
}

func TestLoteNotasIdempotenteYConflictos(t *testing.T) {
	e := nuevoEntorno()
	d, tec := despacho(t), tecnico(t)
	cd, ct := como(t, d, "despacho"), como(t, tec, "tecnico")
	o := programada(t, e, cd, "visita", tec)

	// Sin señal: agregar, editar y eliminar en el mismo lote (la nota es el id de su operación).
	agregar := op(campo.OpNotaAgregar, o.ID, map[string]any{"texto": "Llegué, nadie abre"})
	editar := op(campo.OpNotaEditar, o.ID, map[string]any{"notaId": agregar.ID, "versionNota": 1, "texto": "Llegué, nadie abre; dejé aviso"})
	esperar(t, lote(t, e, ct, agregar, editar), "aplicada", "aplicada")
	det := deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if len(det.Notas) != 1 || det.Notas[0].Texto != "Llegué, nadie abre; dejé aviso" || det.Notas[0].Version != 2 {
		t.Fatalf("nota tras el lote: %+v", det.Notas)
	}

	// El mismo lote otra vez (se perdió la respuesta): duplicadas, sin volver a aplicar.
	esperar(t, lote(t, e, ct, agregar, editar), "duplicada", "duplicada")
	if n := contar(t, "SELECT count(*) FROM auditoria.acciones WHERE accion = 'ordenes.editar_nota' AND entidad_id = $1::bigint::text", o.ID); n != 1 {
		t.Fatalf("editar aplicada una vez: %d", n)
	}

	// Despacho la corrigió mientras el teléfono no tenía señal: la edición vieja se rechaza
	// (conflicto_version, nunca en silencio) y lo siguiente de la orden queda bloqueado.
	deber(e.m.EditarNota.Ejecutar(cd, campo.EntradaEditarNota{ID: o.ID, NotaID: det.Notas[0].ID, Version: 2, Texto: "Corregida por despacho"}))(t)
	r := lote(t, e, ct,
		op(campo.OpNotaEditar, o.ID, map[string]any{"notaId": agregar.ID, "versionNota": 2, "texto": "Mi versión"}),
		op(campo.OpLlegar, o.ID, nil),
	)
	esperar(t, r, "rechazada:conflicto_version", "rechazada:bloqueada_por_rechazo")
	det = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if det.Notas[0].Texto != "Corregida por despacho" {
		t.Fatalf("gana lo del servidor hasta que el técnico decida: %+v", det.Notas[0])
	}

	// Eliminar con la versión actual; repetido (otra operación), aplicada sin cambios.
	esperar(t, lote(t, e, ct, op(campo.OpNotaEliminar, o.ID, map[string]any{"notaId": agregar.ID, "versionNota": 3})), "aplicada")
	esperar(t, lote(t, e, ct, op(campo.OpNotaEliminar, o.ID, map[string]any{"notaId": agregar.ID, "versionNota": 4})), "aplicada")
	// Editar una eliminada: rechazada con nota_eliminada. Sin versionNota: validación.
	esperar(t, lote(t, e, ct, op(campo.OpNotaEditar, o.ID, map[string]any{"notaId": agregar.ID, "versionNota": 4, "texto": "x"})), "rechazada:nota_eliminada")
	esperar(t, lote(t, e, ct, op(campo.OpNotaEditar, o.ID, map[string]any{"notaId": agregar.ID, "texto": "x"})), "rechazada:validacion")
	det = deber(e.m.VerOrden.Ejecutar(cd, campo.EntradaVerOrden{ID: o.ID}))(t)
	if len(det.Notas) != 0 {
		t.Fatalf("eliminada: %+v", det.Notas)
	}
}
