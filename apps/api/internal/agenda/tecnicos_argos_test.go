package agenda_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// argosTecnicosFalso: GET /inventario/tecnicos de Argos ≥ 1.6.0 (contrato 0.5.0 §3.3).
type argosTecnicosFalso struct {
	tecnicos map[string]agenda.TecnicoArgosVerificado
	llamadas int
}

func (a *argosTecnicosFalso) BuscarTecnico(_ context.Context, id string) (agenda.TecnicoArgosVerificado, error) {
	a.llamadas++
	if t, ok := a.tecnicos[id]; ok {
		return t, nil
	}
	return agenda.TecnicoArgosVerificado{}, nucleo.ReglaNegocio("tecnico_no_encontrado", "no está")
}

// El administrador vincula la ficha con el usuario de Argos verificado (con bodega técnico activa
// en la empresa), auditado y con su evento; un usuario de Argos es de una sola ficha; desvincular
// lo quita. Sin el permiso (supervisor), 403 sin llamar a Argos.
func TestVincularTecnicoConArgos(t *testing.T) {
	sufijo := pruebas.Unico()
	falso := &argosTecnicosFalso{tecnicos: map[string]agenda.TecnicoArgosVerificado{
		"u-" + sufijo:         {IDExterno: "u-" + sufijo, Correo: "tec@argos.gt", Nombre: "Técnico Argos", Bodegas: []agenda.BodegaArgosVerificada{{ID: 31, EmpresaID: 0, Activa: true}}},
		"u-inactiva" + sufijo: {IDExterno: "u-inactiva" + sufijo, Bodegas: []agenda.BodegaArgosVerificada{{ID: 32, Activa: false}}},
	}}
	ag := agenda.Nuevo(agenda.Config{Reloj: func() time.Time { return gt(2, 9, 0) }, Argos: falso})
	admin := usuarioCon(t, rol("admin"))
	ca := como(t, admin, "admin")
	tec := usuarioCon(t, rol("tecnico"))
	f := deber(ag.CrearTecnico.Ejecutar(ca, agenda.EntradaCrearTecnico{UsuarioID: tec}))(t)
	if f.Argos != nil {
		t.Fatal("una ficha nueva no está vinculada")
	}

	// Supervisor: sin agenda.vincular_argos (es de administración).
	sup := usuarioCon(t, rol("supervisor"))
	_, err := ag.VincularArgos.Ejecutar(como(t, sup, "supervisor"), agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: "u-" + sufijo})
	problema(t, err, http.StatusForbidden, "prohibido")
	if falso.llamadas != 0 {
		t.Fatal("sin permiso no se llama a Argos")
	}
	// No existe en Argos / sin bodega activa.
	_, err = ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: "u-nadie"})
	problema(t, err, http.StatusUnprocessableEntity, "tecnico_no_encontrado")
	_, err = ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: "u-inactiva" + sufijo})
	problema(t, err, http.StatusUnprocessableEntity, "tecnico_sin_bodega")

	v := deber(ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: " u-" + sufijo + " "}))(t)
	if v.Argos == nil || v.Argos.IDExterno != "u-"+sufijo || v.Argos.Correo != "tec@argos.gt" || v.Version != f.Version+1 {
		t.Fatalf("vínculo: %+v", v.Argos)
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'tecnico.argos_vinculado'
		AND (payload->>'tecnico_id')::bigint = $1 AND payload->>'id_externo' = $2 AND payload->'anterior_id_externo' = 'null'`, f.ID, "u-"+sufijo); n != 1 {
		t.Fatalf("tecnico.argos_vinculado: %d", n)
	}
	if n := contar(t, `SELECT count(*) FROM auditoria.acciones WHERE accion = 'agenda.vincular_argos' AND resultado = 'ok' AND entidad_id = $1`, itoa(f.ID)); n != 1 {
		t.Fatalf("auditoría: %d", n)
	}
	// Versión vieja: 409. Mismo vínculo: sin_cambios.
	_, err = ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: f.Version, IDExterno: "u-" + sufijo})
	problema(t, err, http.StatusConflict, "conflicto_version")
	_, err = ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: v.Version, IDExterno: "u-" + sufijo})
	problema(t, err, http.StatusUnprocessableEntity, "sin_cambios")

	// Otro técnico con el mismo usuario de Argos: 409.
	otro := deber(ag.CrearTecnico.Ejecutar(ca, agenda.EntradaCrearTecnico{UsuarioID: usuarioCon(t, rol("tecnico"))}))(t)
	_, err = ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: otro.ID, Version: otro.Version, IDExterno: "u-" + sufijo})
	problema(t, err, http.StatusConflict, "argos_vinculado_a_otro")

	// Desvincular.
	d := deber(ag.DesvincularArgos.Ejecutar(ca, agenda.EntradaDesvincularArgos{ID: f.ID, Version: v.Version, Motivo: "cambió de usuario"}))(t)
	if d.Argos != nil {
		t.Fatal("debía quedar sin vínculo")
	}
	if n := contar(t, `SELECT count(*) FROM integracion.outbox WHERE evento = 'tecnico.argos_desvinculado'
		AND (payload->>'tecnico_id')::bigint = $1 AND payload->>'motivo' = 'cambió de usuario'`, f.ID); n != 1 {
		t.Fatalf("tecnico.argos_desvinculado: %d", n)
	}
	_, err = ag.DesvincularArgos.Ejecutar(ca, agenda.EntradaDesvincularArgos{ID: f.ID, Version: d.Version})
	problema(t, err, http.StatusUnprocessableEntity, "sin_cambios")
	// Ya libre: el otro lo puede tomar.
	deber(ag.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: otro.ID, Version: otro.Version, IDExterno: "u-" + sufijo}))(t)

	// Sin conector: 503 conector_apagado.
	sinArgos := agenda.Nuevo(agenda.Config{})
	_, err = sinArgos.VincularArgos.Ejecutar(ca, agenda.EntradaVincularArgos{ID: f.ID, Version: d.Version, IDExterno: "u-x"})
	problema(t, err, http.StatusServiceUnavailable, "conector_apagado")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
