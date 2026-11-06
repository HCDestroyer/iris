package agenda_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// La bodega del técnico la decide el servidor: el vínculo con Argos la fija con la bodega activa de
// ese usuario en la empresa; a mano (respaldo) solo con agenda.vincular_argos y verificada en Argos;
// con la ficha vinculada, otra bodega es 422 bodega_por_vinculo.
func TestBodegaDelTecnicoLaDecideElServidor(t *testing.T) {
	sufijo := pruebas.Unico()
	falso := &argosTecnicosFalso{
		tecnicos: map[string]agenda.TecnicoArgosVerificado{
			"u-b" + sufijo: {IDExterno: "u-b" + sufijo, Correo: "b@argos.gt", Nombre: "Bodeguero", Bodegas: []agenda.BodegaArgosVerificada{
				{ID: 40, EmpresaID: 0, Activa: false}, {ID: 41, EmpresaID: 0, Activa: true},
			}},
		},
		bodegas: map[int64]int64{50: 1, 51: 1},
	}
	ag := agenda.Nuevo(agenda.Config{Reloj: func() time.Time { return gt(2, 9, 0) }, Argos: falso})
	ca := como(t, usuarioCon(t, rol("admin")), "admin")
	cs := como(t, usuarioCon(t, rol("supervisor")), "supervisor")

	// Supervisor (crea y edita fichas, sin agenda.vincular_argos): la bodega a mano es 403 y no se
	// pregunta a Argos.
	_, err := ag.CrearTecnicoVerificado(cs, agenda.EntradaCrearTecnico{UsuarioID: usuarioCon(t, rol("tecnico")), BodegaID: ptr(int64(50))})
	problema(t, err, http.StatusForbidden, "prohibido")
	f := deber(ag.CrearTecnicoVerificado(cs, agenda.EntradaCrearTecnico{UsuarioID: usuarioCon(t, rol("tecnico"))}))(t)
	if f.BodegaID != nil {
		t.Fatal("sin bodega a mano nace sin bodega")
	}
	_, err = ag.EditarTecnicoVerificado(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, BodegaID: ptr(int64(50)), Telefono: "5555 1234"})
	problema(t, err, http.StatusForbidden, "prohibido")
	if falso.consultasBodega != 0 {
		t.Fatalf("sin el permiso no se llama a Argos: %d", falso.consultasBodega)
	}
	// Editar otra cosa sin mandar bodega: la deja como está.
	f = deber(ag.EditarTecnicoVerificado(cs, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, Telefono: "5555 1234"}))(t)

	// Admin, a mano: una bodega que Argos no tiene en la empresa es 422; si Argos no responde, su error.
	_, err = ag.EditarTecnicoVerificado(ca, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, BodegaID: ptr(int64(99)), Telefono: "5555 1234"})
	problema(t, err, http.StatusUnprocessableEntity, "bodega_invalida")
	falso.caido = true
	_, err = ag.EditarTecnicoVerificado(ca, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, BodegaID: ptr(int64(50)), Telefono: "5555 1234"})
	problema(t, err, http.StatusServiceUnavailable, "argos_no_disponible")
	falso.caido = false
	// El caso de uso sin la verificación (otro llamador) tampoco la acepta.
	_, err = ag.EditarTecnico.Ejecutar(ca, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, BodegaID: ptr(int64(50)), Telefono: "5555 1234"})
	problema(t, err, http.StatusUnprocessableEntity, "bodega_invalida")
	f = deber(ag.EditarTecnicoVerificado(ca, agenda.EntradaEditarTecnico{ID: f.ID, Version: f.Version, BodegaID: ptr(int64(50)), Telefono: "5555 1234"}))(t)
	if f.BodegaID == nil || *f.BodegaID != 50 {
		t.Fatalf("bodega a mano: %v", f.BodegaID)
	}

	// Vincular fija la bodega activa del usuario de Argos (41, no la inactiva 40), auditada.
	v := deber(ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: "u-b" + sufijo}))(t)
	if v.BodegaID == nil || *v.BodegaID != 41 {
		t.Fatalf("el vínculo debía fijar la bodega 41: %v", v.BodegaID)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.cambios c JOIN auditoria.acciones a ON a.id = c.accion_id
		WHERE a.accion = 'agenda.vincular_argos' AND c.entidad_id = $1 AND c.campo = 'bodega_id' AND c.despues::text = '41'`, itoa(f.ID)); n != 1 {
		t.Fatalf("auditoría del cambio de bodega: %d", n)
	}
	// Vinculada: otra bodega es 422 (también para el admin); la misma, o ninguna, no cambia nada.
	_, err = ag.EditarTecnicoVerificado(ca, agenda.EntradaEditarTecnico{ID: v.ID, Version: v.Version, BodegaID: ptr(int64(51)), Telefono: "5555 9999"})
	problema(t, err, http.StatusUnprocessableEntity, "bodega_por_vinculo")
	e := deber(ag.EditarTecnicoVerificado(cs, agenda.EntradaEditarTecnico{ID: v.ID, Version: v.Version, BodegaID: ptr(int64(41)), Telefono: "5555 9999"}))(t)
	if e.BodegaID == nil || *e.BodegaID != 41 {
		t.Fatalf("la misma bodega se conserva: %v", e.BodegaID)
	}
}

// Si el vínculo falla (409 argos_vinculado_a_otro, 503 de Argos), la ficha queda sin bodega: nunca
// con la de otro técnico.
func TestVinculoFallidoDejaLaFichaSinBodega(t *testing.T) {
	sufijo := pruebas.Unico()
	falso := &argosTecnicosFalso{tecnicos: map[string]agenda.TecnicoArgosVerificado{
		"u-c" + sufijo: {IDExterno: "u-c" + sufijo, Bodegas: []agenda.BodegaArgosVerificada{{ID: 61, EmpresaID: 0, Activa: true}}},
	}}
	ag := agenda.Nuevo(agenda.Config{Reloj: func() time.Time { return gt(2, 9, 0) }, Argos: falso})
	ca := como(t, usuarioCon(t, rol("admin")), "admin")
	duena := deber(ag.CrearTecnicoVerificado(ca, agenda.EntradaCrearTecnico{UsuarioID: usuarioCon(t, rol("tecnico"))}))(t)
	deber(ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: duena.ID, Version: duena.Version, IDExterno: "u-c" + sufijo}))(t)

	nueva := deber(ag.CrearTecnicoVerificado(ca, agenda.EntradaCrearTecnico{UsuarioID: usuarioCon(t, rol("tecnico"))}))(t)
	_, err := ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: nueva.ID, Version: nueva.Version, IDExterno: "u-c" + sufijo})
	problema(t, err, http.StatusConflict, "argos_vinculado_a_otro")
	falso.caido = true
	_, err = ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: nueva.ID, Version: nueva.Version, IDExterno: "u-c" + sufijo})
	problema(t, err, http.StatusServiceUnavailable, "argos_no_disponible")
	if n := contar(t, `SELECT count(*) FROM agenda.tecnicos WHERE id = $1 AND bodega_id IS NULL AND argos_id_externo IS NULL`, nueva.ID); n != 1 {
		t.Fatal("tras un vínculo fallido la ficha queda sin bodega ni vínculo")
	}
}
