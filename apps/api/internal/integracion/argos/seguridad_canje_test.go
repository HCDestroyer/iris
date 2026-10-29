package argos_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/identidad"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/pruebas"
)

// Revisión de seguridad de la PR #50: el iris_pat_ del canje va al origen del enlace. Solo
// orígenes conocidos, vida corta hasta activar, revocado al reemplazar o desactivar el conector,
// emitido por quien opera en todas las empresas y sin ampliar las de un usuario existente.

const correoIntegracion = "integracion.argosisp@iris.local"

func (e *entorno) codigoNuevo() {
	e.argos.con(func() { e.argos.codigo = "codigo-" + pruebas.Unico() + "-0123456789" })
}

// tokensIntegracion cuenta los iris_pat_ del usuario de integración de Argos.
func (e *entorno) tokensIntegracion() int {
	return e.contar(`SELECT count(*) FROM nucleo.tokens_api t JOIN nucleo.usuarios u ON u.id = t.usuario_id WHERE u.correo = $1`, correoIntegracion)
}

func (e *entorno) canjes() int {
	n := 0
	e.argos.con(func() { n = len(e.argos.canjes) })
	return n
}

// tokenDelUltimoCanje es el iris_pat_ que Iris mandó en el último canje.
func (e *entorno) tokenDelUltimoCanje() string {
	var v string
	e.argos.con(func() { v, _ = e.argos.canjes[len(e.argos.canjes)-1]["token"].(string) })
	return v
}

type estadoToken struct {
	expira   *time.Time
	revocado bool
}

func (e *entorno) estadoToken(valor string) estadoToken {
	e.t.Helper()
	var s estadoToken
	if err := e.pool.QueryRow(context.Background(), `SELECT expira_en, revocado_en IS NOT NULL FROM nucleo.tokens_api WHERE hash = $1`,
		identidad.Hash256(valor)).Scan(&s.expira, &s.revocado); err != nil {
		e.t.Fatalf("token del canje: %v", err)
	}
	return s
}

func (e *entorno) conectarComoAdmin(admin *clienteHTTP) int64 {
	e.t.Helper()
	r := admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()})
	if r.status != 201 {
		e.t.Fatalf("conectar: %d %s", r.status, r.crudo)
	}
	return int64(r.cuerpo["id"].(float64))
}

func TestCanjeSoloAOrigenesPermitidos(t *testing.T) {
	e := nuevoEntorno(t)
	ctx := pruebas.ContextoSistema(e.pool)
	antes, canjes := e.tokensIntegracion(), e.canjes()

	// Con lista: un origen que no está → 422, sin token emitido ni llamada a Argos.
	conLista := e.cfg
	conLista.Origenes = []string{"https://argos.ejemplo.gt"}
	m := argos.Nuevo(conLista)
	m.Nucleo().FijarTokens(e.ident)
	if _, err := m.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); !esProblema(err, "origen_no_permitido") {
		t.Fatalf("origen fuera de la lista: %v", err)
	}
	p, err := m.Previsualizar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()})
	if err != nil || p.Permitido || p.Motivo == nil || *p.Motivo != "origen_no_permitido" || p.Origen != e.argos.srv.URL || p.ConectorExistente {
		t.Fatalf("previsualizar: %+v %v", p, err)
	}

	// Sin lista y fuera de desarrollo (https, sin PermitirHTTP) → 422 origenes_no_configurados.
	produccion := e.cfg
	produccion.PermitirHTTP = false
	mp := argos.Nuevo(produccion)
	mp.Nucleo().FijarTokens(e.ident)
	if _, err := mp.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: "https://argos-falso.example/conectar#codigo-de-argos-0123456789"}); !esProblema(err, "origenes_no_configurados") {
		t.Fatalf("sin IRIS_ARGOS_ORIGENES: %v", err)
	}
	if e.tokensIntegracion() != antes || e.canjes() != canjes {
		t.Fatalf("un origen no permitido no emite token ni llama afuera: tokens %d→%d, canjes %d→%d", antes, e.tokensIntegracion(), canjes, e.canjes())
	}

	// En la lista: permitido (la previsualización lo dice sin llamar afuera).
	conLista.Origenes = append(conLista.Origenes, e.argos.srv.URL)
	m2 := argos.Nuevo(conLista)
	m2.Nucleo().FijarTokens(e.ident)
	if p, err := m2.Previsualizar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); err != nil || !p.Permitido || p.Motivo != nil {
		t.Fatalf("previsualizar permitido: %+v %v", p, err)
	}
	if e.canjes() != canjes {
		t.Fatal("previsualizar no llama a Argos")
	}
	// Con un conector de ese origen, el mismo origen sirve aunque no esté en la lista.
	if _, err := m2.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); err != nil {
		t.Fatalf("conectar desde la lista: %v", err)
	}
	e.codigoNuevo()
	if _, err := m.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); err != nil {
		t.Fatalf("mismo origen que un conector existente: %v", err)
	}
}

// Un sitio que responde 200 con un atk_ cualquiera (enlace de phishing con origen permitido en
// desarrollo): el iris_pat_ vence en 15 min y, vencido, el conector no se activa.
func TestTokenDelCanjeVenceSinActivar(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	id := e.conectarComoAdmin(admin)
	tok := e.estadoToken(e.tokenDelUltimoCanje())
	if tok.revocado || tok.expira == nil || time.Until(*tok.expira) < 14*time.Minute || time.Until(*tok.expira) > 16*time.Minute {
		t.Fatalf("el token del canje nace con 15 min: %+v", tok)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE nucleo.tokens_api SET expira_en = now() - interval '1 minute' WHERE hash = $1`,
		identidad.Hash256(e.tokenDelUltimoCanje())); err != nil {
		t.Fatal(err)
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": 1}).
		problema(t, 422, "token_entregado_vencido")
	if n := e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND estado = 'apagado'`, id); n != 1 {
		t.Fatal("con el token vencido el conector sigue apagado")
	}

	// A tiempo: activar le quita el vencimiento.
	e.codigoNuevo()
	id2 := e.conectarComoAdmin(admin)
	valor := e.tokenDelUltimoCanje()
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id2), map[string]any{"version": 1}); a.status != 200 {
		t.Fatalf("activar a tiempo: %d %s", a.status, a.crudo)
	}
	if tok := e.estadoToken(valor); tok.expira != nil || tok.revocado {
		t.Fatalf("activado, el token no vence: %+v", tok)
	}
}

// Reconectar revoca el token del conector anterior; desactivar revoca el suyo (y volver a
// activarlo exige conectar de nuevo).
func TestReconectarYDesactivarRevocan(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	e.conectarComoAdmin(admin)
	primero := e.tokenDelUltimoCanje()
	e.codigoNuevo()
	id := e.conectarComoAdmin(admin)
	segundo := e.tokenDelUltimoCanje()
	if !e.estadoToken(primero).revocado || e.estadoToken(segundo).revocado {
		t.Fatalf("reconectar: el anterior se revoca (%+v) y el nuevo no (%+v)", e.estadoToken(primero), e.estadoToken(segundo))
	}
	if a := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": 1}); a.status != 200 {
		t.Fatalf("activar: %d %s", a.status, a.crudo)
	}
	if d := admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/desactivar", id), map[string]any{"version": 2, "motivo": "prueba"}); d.status != 200 {
		t.Fatalf("desactivar: %d %s", d.status, d.crudo)
	}
	if !e.estadoToken(segundo).revocado {
		t.Fatal("desactivar revoca el token entregado")
	}
	admin.pedir(http.MethodPost, fmt.Sprintf("/api/v1/integraciones/conectores/%d/activar", id), map[string]any{"version": 3}).
		problema(t, 422, "token_entregado_vencido")
}

// Sin empresas.todas global no se conecta (403) ni se emite el token; un conector_id inválido
// hace fallar el canje y revoca el token.
func TestCanjeExigeTodasLasEmpresasYValidaConectorID(t *testing.T) {
	e := nuevoEntorno(t)
	antes, canjes := e.tokensIntegracion(), e.canjes()
	ctx := pruebas.ContextoUsuario(e.pool, 0, nucleo.PermIntegracionesConectar)
	if _, err := e.mod.Conectar.Ejecutar(ctx, argos.EntradaConectar{Enlace: e.argos.enlace()}); !esProblema(err, "prohibido") {
		t.Fatalf("sin empresas.todas: %v", err)
	}
	if e.tokensIntegracion() != antes || e.canjes() != canjes {
		t.Fatal("sin empresas.todas no se emite token ni se llama a Argos")
	}

	admin := e.usuario("admin")
	for _, malo := range []any{"../conector", 1.5, -3, map[string]any{"id": 1}, "x-" + string(make([]byte, 70))} {
		e.codigoNuevo()
		e.argos.con(func() { e.argos.conectorID = malo })
		admin.pedir(http.MethodPost, "/api/v1/integraciones/argosisp/conectar", map[string]any{"enlace": e.argos.enlace()}).
			problema(t, 502, "argos_rechazo")
		if !e.estadoToken(e.tokenDelUltimoCanje()).revocado {
			t.Fatalf("conector_id %v: el token del canje fallido se revoca", malo)
		}
	}
	e.codigoNuevo()
	e.argos.con(func() { e.argos.conectorID = 9001 })
	if id := e.conectarComoAdmin(admin); e.contar(`SELECT count(*) FROM integracion.conectores WHERE id = $1 AND remoto_id = '9001'`, id) != 1 {
		t.Fatal("un conector_id entero positivo vale")
	}
}

// Un usuario de integración que ya existe no gana empresas por conectar (se crea con todas solo
// la primera vez).
func TestCanjeNoAmpliaEmpresasDelUsuarioExistente(t *testing.T) {
	e := nuevoEntorno(t)
	admin := e.usuario("admin")
	e.conectarComoAdmin(admin) // lo crea (o ya existía)
	var antes []int64
	if err := e.pool.QueryRow(context.Background(), `SELECT coalesce(array_agg(ue.empresa_id ORDER BY ue.empresa_id) FILTER (WHERE ue.empresa_id IS NOT NULL), '{}') FROM nucleo.usuarios u
		LEFT JOIN nucleo.usuarios_empresas ue ON ue.usuario_id = u.id WHERE u.correo = $1`, correoIntegracion).Scan(&antes); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO nucleo.empresas (id, nombre) VALUES (2, 'Empresa 2') ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	e.codigoNuevo()
	e.conectarComoAdmin(admin)
	var despues []int64
	if err := e.pool.QueryRow(context.Background(), `SELECT coalesce(array_agg(ue.empresa_id ORDER BY ue.empresa_id) FILTER (WHERE ue.empresa_id IS NOT NULL), '{}') FROM nucleo.usuarios u
		LEFT JOIN nucleo.usuarios_empresas ue ON ue.usuario_id = u.id WHERE u.correo = $1`, correoIntegracion).Scan(&despues); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(antes) != fmt.Sprint(despues) {
		t.Fatalf("las empresas del usuario de integración cambiaron: %v → %v", antes, despues)
	}
	// La emisión queda auditada con el actor real (el admin), no con el sistema.
	if n := e.contar(`SELECT count(*) FROM auditoria.acciones WHERE accion = 'tokens.crear' AND actor_tipo = 'usuario' AND resultado = 'ok'
		AND entrada->>'usuarioId' = (SELECT id::text FROM nucleo.usuarios WHERE correo = $1)`, correoIntegracion); n < 1 {
		t.Fatal("tokens.crear del canje auditado con el usuario que conecta")
	}
}
