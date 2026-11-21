package pendientes_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// tokenArgos: el iris_pat_ entregado al conector (el que consulta la verificación), con los
// permisos dados y las empresas de su alcance.
func (e *entorno) tokenArgos(entregado bool, permisos []nucleo.Permiso, empresas ...int64) context.Context {
	e.t.Helper()
	usuario := e.tecnico("")
	var token int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO nucleo.tokens_api (usuario_id, nombre, prefijo, hash)
		VALUES ($1, 'argos', 'iris_pat_', $2) RETURNING id`, usuario, []byte(uuid.NewString())).Scan(&token); err != nil {
		e.t.Fatal(err)
	}
	if entregado {
		e.exec(`UPDATE integracion.conectores SET token_entregado_id = $2 WHERE id = $1`, e.conID, token)
	}
	mapa := map[string][]contexto.Alcance{}
	for _, p := range permisos {
		mapa[string(p)] = []contexto.Alcance{{}}
	}
	ctx := contexto.ConActor(context.Background(), contexto.Actor{Tipo: contexto.ActorToken, ID: usuario, TokenID: token, Nombre: "Argos"},
		contexto.NuevosPermisos(mapa))
	ctx = contexto.ConCanal(ctx, contexto.CanalAPI)
	ctx = contexto.ConRequestID(ctx, contexto.NuevoRequestID())
	ctx = contexto.ConEmpresas(ctx, contexto.Empresas{Estado: contexto.EmpresaPorElegir, Disponibles: empresas})
	return nucleo.ConBD(ctx, e.pool)
}

func codigoDe(t *testing.T, err error) (int, string) {
	t.Helper()
	var p *nucleo.Problema
	if !errors.As(err, &p) {
		t.Fatalf("se esperaba un problema: %v", err)
	}
	return p.Status, p.Codigo()
}

// Contrato 0.14.0 §3.10: GET …/intentos-recuperacion/verificacion. verificado solo con el 2xx de
// Argos a ese conector y la foto válida; motivos de la lista cerrada; 404 igual para una clave
// desconocida, de otra empresa o de un token que no es el del conector; 403 sin el permiso (como el
// token_portal); 422 idempotency_key_reutilizada → no_coincide; tope de 20 por clave al día.
func TestVerificacionDelIntento014(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	tec := e.tecnico("u-47")
	orden := nuevaOrden()
	foto := e.foto(orden, "completo")
	e.exec(`UPDATE nucleo.adjuntos SET subido_por = $2, tomado_en = $3 WHERE id = $1`, foto, tec, e.reloj)
	intento := uuid.Must(uuid.NewV7()).String()
	orden1 := intentoAplicado(orden, intento, 103, tec, foto, e.reloj)
	orden1["empresa_id"] = 1
	if !e.aplicar(orden1) {
		t.Fatal("encolado al aplicar")
	}
	clave := "iris:no_recuperado:" + orden + ":103:" + intento
	argos := e.tokenArgos(true, []nucleo.Permiso{nucleo.PermAdjuntosVerificarIntegracion}, 1)
	verificar := func(ctx context.Context, empresa int64, k string) (pendientes.VerificacionIntento, error) {
		return e.m.VerificarIntento(ctx, pendientes.EntradaVerificarIntento{EmpresaID: empresa, IdempotencyKey: k})
	}
	v, err := verificar(argos, 1, clave)
	if err != nil || v.Verificado || v.Motivo == nil || *v.Motivo != pendientes.MotivoEnvioEnCurso {
		t.Fatalf("antes de enviarlo: %+v %v", v, err)
	}
	e.procesar()
	if v, err := verificar(argos, 1, clave); err != nil || !v.Verificado || v.Motivo != nil {
		t.Fatalf("enviado y con 2xx: %+v %v", v, err)
	}
	// 404 igual: clave desconocida, otra empresa (fuera del alcance) o un token que no es del conector.
	for nombre, c := range map[string]func() error{
		"desconocida":  func() error { _, err := verificar(argos, 1, clave+"x"); return err },
		"otra empresa": func() error { _, err := verificar(argos, 2, clave); return err },
		"otro conector": func() error {
			_, err := verificar(e.tokenArgos(false, []nucleo.Permiso{nucleo.PermAdjuntosVerificarIntegracion}, 1), 1, clave)
			return err
		},
	} {
		if s, _ := codigoDe(t, c()); s != 404 {
			t.Fatalf("%s: %d", nombre, s)
		}
	}
	// Sin el permiso (el token_portal no lo tiene): 403.
	if s, _ := codigoDe(t, func() error {
		_, err := verificar(e.tokenArgos(true, []nucleo.Permiso{nucleo.PermTicketsVer}, 1), 1, clave)
		return err
	}()); s != 403 {
		t.Fatalf("sin permiso: %d", s)
	}
	argos = e.tokenArgos(true, []nucleo.Permiso{nucleo.PermAdjuntosVerificarIntegracion}, 1)

	// Una foto que aún no sube: foto_pendiente.
	otra := e.foto(orden, "pendiente")
	e.exec(`UPDATE nucleo.adjuntos SET subido_por = $2, tomado_en = $3 WHERE id = $1`, otra, tec, e.reloj)
	i2 := uuid.Must(uuid.NewV7()).String()
	o2 := intentoAplicado(orden, i2, 104, tec, otra, e.reloj)
	o2["empresa_id"] = 1
	e.aplicar(o2)
	if v, err := verificar(argos, 1, "iris:no_recuperado:"+orden+":104:"+i2); err != nil || v.Motivo == nil || *v.Motivo != pendientes.MotivoFotoPendiente {
		t.Fatalf("foto pendiente: %+v %v", v, err)
	}

	// Argos tiene otro cuerpo con esa clave (422 idempotency_key_reutilizada): no_coincide.
	tercera := e.foto(orden, "completo")
	e.exec(`UPDATE nucleo.adjuntos SET subido_por = $2, tomado_en = $3 WHERE id = $1`, tercera, tec, e.reloj)
	i3 := uuid.Must(uuid.NewV7()).String()
	o3 := intentoAplicado(orden, i3, 105, tec, tercera, e.reloj)
	o3["empresa_id"] = 1
	ruta := "/servicios/59/equipos/105/no-recuperado"
	e.argos.responder(ruta, 422)
	e.argos.tipos[ruta] = "urn:argosisp:problema:idempotency_key_reutilizada"
	e.aplicar(o3)
	e.procesar()
	if v, err := verificar(argos, 1, "iris:no_recuperado:"+orden+":105:"+i3); err != nil || v.Motivo == nil || *v.Motivo != pendientes.MotivoNoCoincide {
		t.Fatalf("no coincide: %+v %v", v, err)
	}

	// Tope: 20 por clave al día (cuentan también las que terminaron en 404 por otra empresa).
	hechas := e.contar(`SELECT count(*) FROM integracion.verificaciones_intento WHERE clave = $1`, clave)
	for range pendientes.TopeVerificacionPorDia - hechas {
		if _, err := verificar(argos, 1, clave); err != nil {
			t.Fatal(err)
		}
	}
	if s, c := codigoDe(t, func() error { _, err := verificar(argos, 1, clave); return err }()); s != 429 || c != "demasiadas_peticiones" {
		t.Fatalf("tope por clave: %d %s", s, c)
	}
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'adjuntos.verificar_integracion'`); n == 0 {
		t.Fatal("cada consulta se audita")
	}
}
