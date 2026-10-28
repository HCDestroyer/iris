package auditoria

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Tx es la unidad de trabajo que recibe la lógica de un caso de uso: la transacción (embebida,
// así sirve directo a sqlc: `identidadbd.New(t)`) más los ayudantes de auditoría y outbox.
type Tx struct {
	pgx.Tx

	ctx        context.Context
	actor      *contexto.Actor
	entidad    string
	entidadID  string
	empresaID  *int64
	empresaFij bool
	cambios    []cambio
	anotacion  map[string]any
	ganchos    []func(context.Context)
	alFallar   []func(context.Context, pgx.Tx) error
	eventos    int
}

type cambio struct {
	entidad   string
	entidadID string
	campo     string
	antes     []byte
	despues   []byte
}

// Campos que no se registran como cambio.
var camposIgnorados = []string{"created_at", "updated_at", "version"}

// Actor devuelve el actor de la ejecución (el del contexto o el fijado con IdentificarActor).
func (t *Tx) Actor() contexto.Actor {
	if t.actor != nil {
		return *t.actor
	}
	return contexto.ActorDe(t.ctx)
}

// IdentificarActor fija el actor cuando se conoce dentro de la acción: el inicio de sesión
// llega anónimo y queda auditado con el usuario que entró. Solo para casos que autentican.
func (t *Tx) IdentificarActor(a contexto.Actor) { t.actor = &a }

// FijarEntidad fija la entidad principal (auditoria.acciones.entidad / entidad_id). tipo en
// singular y snake_case: "usuario", "token", "ticket".
func (t *Tx) FijarEntidad(tipo string, id any) {
	t.entidad = tipo
	t.entidadID = fmt.Sprint(id)
}

// FijarEmpresa fija la empresa de lo que hizo la acción (id de Argos; 0 = ninguna). Sin
// llamarla se usa la empresa activa del contexto. También va en los eventos que emita después.
func (t *Tx) FijarEmpresa(empresaID int64) {
	t.empresaFij = true
	if empresaID == 0 {
		t.empresaID = nil
		return
	}
	t.empresaID = &empresaID
}

func (t *Tx) empresa() *int64 {
	if t.empresaFij {
		return t.empresaID
	}
	if id := contexto.EmpresaDe(t.ctx); id != 0 {
		return &id
	}
	return nil
}

// Anotar agrega datos a auditoria.acciones.entrada bajo la clave "auditoria" (p. ej. qué
// clientes leyó una integración). Se enmascaran igual que la entrada.
func (t *Tx) Anotar(datos map[string]any) {
	if t.anotacion == nil {
		t.anotacion = map[string]any{}
	}
	for k, v := range datos {
		t.anotacion[k] = v
	}
}

// Emitir escribe el evento en integracion.outbox dentro de la misma transacción: si la acción
// se revierte, el evento no existe.
func (t *Tx) Emitir(evento string, payload any) error {
	_, err := integracion.Insertar(t.ctx, t.Tx, evento, t.empresa(), contexto.RequestIDDe(t.ctx), payload)
	if err == nil {
		t.eventos++
	}
	return err
}

// AlConfirmar registra trabajo que solo debe correr si el COMMIT más externo se confirmó
// (invalidar una caché, avisar a un proceso). Si algo se revierte, se descarta. Sus errores no
// afectan la respuesta.
func (t *Tx) AlConfirmar(fn func(ctx context.Context)) { t.ganchos = append(t.ganchos, fn) }

// AlFallar registra trabajo que debe quedar escrito aunque la acción falle (p. ej. contar un
// intento de inicio de sesión fallido): fn corre, si el caso devuelve error, en la misma
// transacción aparte que escribe la fila de error de auditoría, antes de escribirla (puede
// llamar a Anotar). Cada fn va en un savepoint: si falla, se registra en el log y la auditoría
// sigue. Si el caso termina bien, se descarta.
func (t *Tx) AlFallar(fn func(ctx context.Context, tx pgx.Tx) error) {
	t.alFallar = append(t.alFallar, fn)
}

// RegistrarCambios compara antes y despues (structs o mapas; se usan sus nombres JSON) y
// guarda un cambio por campo distinto en auditoria.cambios. antes nil = creación. Los campos
// sensibles (por nombre o con `auditoria:"sensible"`) quedan como "***". Ignora created_at,
// updated_at y version. Devuelve los campos que cambiaron (snake_case, ordenados).
func (t *Tx) RegistrarCambios(entidad string, id any, antes, despues any) []string {
	if t.entidad == "" {
		t.FijarEntidad(entidad, id)
	}
	a, _ := aJSON(antes).(map[string]any)
	d, _ := aJSON(despues).(map[string]any)
	marcados := etiquetados(tipoDe(antes, despues))

	claves := map[string]string{} // snake → original
	for k := range a {
		claves[SnakeCase(k)] = k
	}
	for k := range d {
		claves[SnakeCase(k)] = k
	}
	campos := make([]string, 0, len(claves))
	for snake := range claves {
		if !slices.Contains(camposIgnorados, snake) {
			campos = append(campos, snake)
		}
	}
	slices.Sort(campos)

	idTexto := fmt.Sprint(id)
	var cambiaron []string
	for _, campo := range campos {
		original := claves[campo]
		va, vd := a[original], d[original]
		ja, _ := json.Marshal(va)
		jd, _ := json.Marshal(vd)
		if string(ja) == string(jd) {
			continue
		}
		if marcados[original] || EsCampoSensible(campo) {
			if va != nil {
				ja, _ = json.Marshal(Mascara)
			}
			if vd != nil {
				jd, _ = json.Marshal(Mascara)
			}
		}
		t.cambios = append(t.cambios, cambio{entidad: entidad, entidadID: idTexto, campo: campo, antes: ja, despues: jd})
		cambiaron = append(cambiaron, campo)
	}
	return cambiaron
}

func tipoDe(vs ...any) reflect.Type {
	for _, v := range vs {
		if v != nil {
			return reflect.TypeOf(v)
		}
	}
	return nil
}
