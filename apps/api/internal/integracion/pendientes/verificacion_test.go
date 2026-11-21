package pendientes_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

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
		e.exec(`UPDATE integracion.conectores SET token_entregado_id = $2, contrato_informado = '0.14.0' WHERE id = $1`, e.conID, token)
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
	orden := e.ordenCampo(tec, true)
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

// ordenCampo: una orden de campo real (la llegada se busca en campo.orden_eventos por su id), con la
// llegada del técnico una hora antes del reloj si llego.
func (e *entorno) ordenCampo(tec int64, llego bool) string {
	e.t.Helper()
	var orden int64
	if err := e.pool.QueryRow(context.Background(), `INSERT INTO campo.ordenes (tipo_id, direccion)
		VALUES ((SELECT id FROM campo.tipos_orden WHERE codigo = 'retiro'), 'Prueba') RETURNING id`).Scan(&orden); err != nil {
		e.t.Fatal(err)
	}
	id := strconv.FormatInt(orden, 10)
	if llego {
		e.llegada(id, tec)
	}
	return id
}

func (e *entorno) llegada(orden string, tec int64) {
	e.t.Helper()
	e.exec(`INSERT INTO campo.orden_eventos (orden_id, tipo, de, a, actor_tipo, actor_id, ocurrido_en)
		VALUES ($1::bigint, 'estado', 'en_camino', 'en_sitio', 'usuario', $2, $3)`, orden, tec, e.reloj.Add(-time.Hour))
}

// M-1: el tope por clave aguanta consultas concurrentes (cuenta e inserta con candado).
func TestVerificacionTopeConcurrente(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	argos := e.tokenArgos(true, []nucleo.Permiso{nucleo.PermAdjuntosVerificarIntegracion}, 1)
	clave := "iris:no_recuperado:" + nuevaOrden() + ":1:" + uuid.Must(uuid.NewV7()).String()
	const n = 30
	resultados := make(chan error, n)
	for range n {
		go func() {
			_, err := e.m.VerificarIntento(argos, pendientes.EntradaVerificarIntento{EmpresaID: 1, IdempotencyKey: clave})
			resultados <- err
		}()
	}
	topes := 0
	for range n {
		var p *nucleo.Problema
		if err := <-resultados; errors.As(err, &p) && p.Status == 429 {
			topes++
		}
	}
	if topes != n-pendientes.TopeVerificacionPorDia {
		t.Fatalf("pasaron %d de %d (tope %d)", n-topes, n, pendientes.TopeVerificacionPorDia)
	}
	if c := e.contar(`SELECT count(*) FROM integracion.verificaciones_intento WHERE clave = $1`, clave); c != pendientes.TopeVerificacionPorDia {
		t.Fatalf("registradas: %d", c)
	}
}

// M-2 y M-3: sin llegada del autor, o con la foto completa fuera de la ventana de 72 h, la
// verificación responde foto_invalida aunque Argos haya respondido 2xx.
func TestVerificacionSinLlegadaOFueraDeVentana(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	tec := e.tecnico("u-49")
	id := e.ordenCampo(tec, false)
	argos := e.tokenArgos(true, []nucleo.Permiso{nucleo.PermAdjuntosVerificarIntegracion}, 1)
	aplicado := func(unidad int64, completado time.Time) string {
		foto := e.foto(id, "completo")
		e.exec(`UPDATE nucleo.adjuntos SET subido_por = $2, tomado_en = $3, completado_en = $4 WHERE id = $1`, foto, tec, e.reloj, completado)
		intento := uuid.Must(uuid.NewV7()).String()
		o := intentoAplicado(id, intento, unidad, tec, foto, e.reloj)
		o["empresa_id"] = 1
		if !e.aplicar(o) {
			t.Fatal("encolado")
		}
		return "iris:no_recuperado:" + id + ":" + strconv.FormatInt(unidad, 10) + ":" + intento
	}
	sinLlegada := aplicado(120, e.reloj)
	e.procesar()
	if v, err := e.m.VerificarIntento(argos, pendientes.EntradaVerificarIntento{EmpresaID: 1, IdempotencyKey: sinLlegada}); err != nil ||
		v.Verificado || v.Motivo == nil || *v.Motivo != pendientes.MotivoFotoInvalida {
		t.Fatalf("sin llegada: %+v %v", v, err)
	}
	e.llegada(id, tec)
	if v, err := e.m.VerificarIntento(argos, pendientes.EntradaVerificarIntento{EmpresaID: 1, IdempotencyKey: sinLlegada}); err != nil || !v.Verificado {
		t.Fatalf("con llegada: %+v %v", v, err)
	}
	tarde := aplicado(121, e.reloj.Add(73*time.Hour))
	e.procesar()
	if v, err := e.m.VerificarIntento(argos, pendientes.EntradaVerificarIntento{EmpresaID: 1, IdempotencyKey: tarde}); err != nil ||
		v.Motivo == nil || *v.Motivo != pendientes.MotivoFotoInvalida {
		t.Fatalf("foto completa fuera de la ventana: %+v %v", v, err)
	}
}

// M-3: una foto que quedó completo más de 72 h después de la fecha no deja salir el intento.
func TestIntentoConFotoCompletaFueraDeVentana(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	tec := e.tecnico("u-50")
	orden := nuevaOrden()
	foto := e.foto(orden, "pendiente")
	intento := uuid.Must(uuid.NewV7()).String()
	e.aplicar(intentoAplicado(orden, intento, 122, tec, foto, e.reloj))
	e.exec(`UPDATE nucleo.adjuntos SET estado = 'completo', completado_en = $2 WHERE id = $1`, foto, e.reloj.Add(73*time.Hour))
	e.procesar()
	p := e.pendientes(orden)["iris:no_recuperado:"+orden+":122:"+intento]
	if p.Estado != "fallido_negocio" || e.contar(`SELECT count(*) FROM integracion.pendientes WHERE id = $1 AND codigo_error = $2`, p.ID, pendientes.CodigoFotoFueraDeVentana) != 1 {
		t.Fatalf("fuera de la ventana: %+v", p)
	}
}

// 0.14.0 §4.5: la verificación solo responde a un conector al que Iris ya informó 0.14.0; antes,
// el mismo 404 que una clave que no existe (el permiso del token no basta).
func TestVerificacionSoloConContrato014Informado(t *testing.T) {
	e := nuevoEntorno(t)
	e.argos16()
	e.contrato("0.14.0")
	argos := e.tokenArgos(true, []nucleo.Permiso{nucleo.PermAdjuntosVerificarIntegracion}, 1)
	clave := "iris:no_recuperado:" + nuevaOrden() + ":1:" + uuid.Must(uuid.NewV7()).String()
	e.exec(`UPDATE integracion.conectores SET contrato_informado = '0.8.1' WHERE id = $1`, e.conID)
	_, err := e.m.VerificarIntento(argos, pendientes.EntradaVerificarIntento{EmpresaID: 1, IdempotencyKey: clave})
	if s, codigo := codigoDe(t, err); s != 404 || codigo != "no_encontrado" {
		t.Fatalf("con 0.8.1 informado: %d %s", s, codigo)
	}
	if n := e.contar(`SELECT count(*) FROM integracion.verificaciones_intento WHERE clave = $1`, clave); n != 0 {
		t.Fatalf("sin conector habilitado no cuenta para el tope: %d", n)
	}
}
