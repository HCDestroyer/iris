// Package auditoria es la base de los casos de uso (CLAUDE.md §1, §3 y §9): cada ejecución
// corre en una transacción, verifica el permiso del actor, audita en auditoria.acciones y
// auditoria.cambios y escribe sus eventos en integracion.outbox, todo en la misma transacción.
//
//	var CrearTicket = auditoria.Nuevo(auditoria.Caso[EntradaTicket, Ticket]{
//	    Accion:  "tickets.crear",
//	    Permiso: nucleo.PermTicketsCrear,
//	    Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTicket) (Ticket, error) {
//	        q := soportebd.New(t)
//	        fila, err := q.CrearTicket(ctx, …)
//	        if err != nil { return Ticket{}, err }
//	        t.RegistrarCambios("ticket", fila.ID, nil, fila)
//	        return aTicket(fila), t.Emitir("ticket.creado", map[string]any{"ticket_id": fila.ID})
//	    },
//	})
//	ticket, err := CrearTicket.Ejecutar(ctx, entrada)
package auditoria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	auditoriabd "github.com/HCDestroyer/iris/apps/api/internal/auditoria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Acceso dice qué exige un caso de uso antes de correr.
type Acceso int

const (
	// ConPermiso (por defecto): el actor debe tener Permiso. Sin credenciales, 401; sin el
	// permiso, 403.
	ConPermiso Acceso = iota
	// Autenticado: basta un actor identificado (p. ej. GET /yo, cerrar sesión).
	Autenticado
	// Publico: sin credenciales (inicio de sesión). Se audita igual.
	Publico
)

// Caso es un caso de uso. Se arma una vez (con Nuevo, normalmente en una variable del
// paquete) y se ejecuta con Ejecutar.
type Caso[In, Out any] struct {
	// Accion es el nombre de la acción: va en auditoria.acciones.accion y, con Acceso
	// ConPermiso, es también el permiso si Permiso queda vacío.
	Accion string
	// Permiso exigido (ConPermiso). Debe estar en el catálogo (nucleo/permisos.go).
	Permiso nucleo.Permiso
	Acceso  Acceso
	// Logica es el cuerpo del caso de uso. Corre dentro de la transacción; ctx trae la
	// transacción (un caso anidado abre un savepoint) y t los ayudantes.
	Logica func(ctx context.Context, t *Tx, in In) (Out, error)
	// AuditarExito decide si un resultado exitoso deja fila en auditoria.acciones (nil =
	// siempre). Para lecturas sin datos sensibles o telemetría. Si la ejecución registró
	// cambios, la fila se escribe igual. Los errores se auditan siempre.
	AuditarExito func(Out) bool
}

// Nuevo valida la definición y devuelve el caso listo. Entra en pánico si está mal armado (se
// descubre al arrancar, no en producción): sin acción, sin lógica o con un permiso que no está
// en el catálogo.
func Nuevo[In, Out any](c Caso[In, Out]) *Caso[In, Out] {
	if c.Accion == "" || c.Logica == nil {
		panic("auditoria.Nuevo: el caso necesita Accion y Logica")
	}
	if c.Acceso == ConPermiso {
		if c.Permiso == "" {
			c.Permiso = nucleo.Permiso(c.Accion)
		}
		if !nucleo.EsPermiso(string(c.Permiso)) {
			panic(fmt.Sprintf("auditoria.Nuevo: %s exige el permiso %q, que no está en nucleo.Catalogo", c.Accion, c.Permiso))
		}
	}
	return &c
}

type claveCaso int

const claveTxCaso claveCaso = 0

// Ejecutar corre el caso de uso:
//
//  1. abre la transacción (o un savepoint si ya hay una en ctx);
//  2. verifica el acceso del actor;
//  3. corre la lógica;
//  4. escribe auditoria.acciones (y los cambios registrados) en la misma transacción;
//  5. confirma; los eventos emitidos salen en el mismo COMMIT;
//  6. corre los ganchos AlConfirmar (solo el caso más externo).
//
// Si algo falla, la transacción se revierte y el error se audita en otra transacción; el error
// se devuelve tal cual (los handlers lo traducen con nucleo.AProblema).
func (c *Caso[In, Out]) Ejecutar(ctx context.Context, in In) (Out, error) {
	var salida Out
	entrada := Enmascarar(in)
	externa, _ := ctx.Value(claveTxCaso).(*Tx)
	var t *Tx

	err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		t = &Tx{Tx: tx}
		ctx = context.WithValue(ctx, claveTxCaso, t)
		t.ctx = ctx
		if err := c.autorizar(ctx); err != nil {
			return err
		}
		var err error
		salida, err = c.Logica(ctx, t, in)
		if err != nil {
			return err
		}
		if c.AuditarExito != nil && !c.AuditarExito(salida) && len(t.cambios) == 0 {
			return nil
		}
		return registrar(ctx, t, tx, c.Accion, t.entradaAuditada(entrada), nil)
	})
	if err != nil {
		c.auditarError(ctx, t, entrada, err)
		var cero Out
		return cero, err
	}

	if externa != nil {
		externa.ganchos = append(externa.ganchos, t.ganchos...)
		return salida, nil
	}
	for _, g := range t.ganchos {
		correrGancho(ctx, g)
	}
	return salida, nil
}

func (c *Caso[In, Out]) autorizar(ctx context.Context) error {
	if c.Acceso == Publico {
		return nil
	}
	actor := contexto.ActorDe(ctx)
	if actor.Tipo == contexto.ActorAnonimo {
		return nucleo.NoAutenticado("")
	}
	if c.Acceso == ConPermiso && !contexto.PermisosDe(ctx).Tiene(string(c.Permiso)) {
		return nucleo.Prohibido(fmt.Sprintf("Falta el permiso %s", c.Permiso))
	}
	return nil
}

func (t *Tx) entradaAuditada(entrada any) any {
	if len(t.anotacion) == 0 {
		return entrada
	}
	base, ok := entrada.(map[string]any)
	if !ok {
		base = map[string]any{"entrada": entrada}
	}
	base["auditoria"] = Enmascarar(t.anotacion)
	return base
}

// auditarError escribe la fila de error en una transacción aparte (la de la acción ya se
// revirtió). Nunca falla la petición: si no puede auditar, lo registra en el log.
func (c *Caso[In, Out]) auditarError(ctx context.Context, t *Tx, entrada any, causa error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		slog.ErrorContext(ctx, "auditoria: sin base para auditar el error", "accion", c.Accion, "err", causa)
		return
	}
	if t == nil {
		t = &Tx{}
	}
	t.ctx = ctx
	t.cambios = nil
	actx := context.WithoutCancel(ctx)
	tx, err := bd.Begin(actx)
	if err == nil {
		for _, fn := range t.alFallar {
			c.correrAlFallar(actx, tx, fn)
		}
		err = registrar(actx, t, tx, c.Accion, t.entradaAuditada(entrada), causa)
		if err == nil {
			err = tx.Commit(actx)
		} else {
			_ = tx.Rollback(actx)
		}
	}
	if err != nil {
		slog.ErrorContext(ctx, "auditoria: no se pudo auditar el error", "accion", c.Accion, "err", err, "causa", causa)
	}
}

// correrAlFallar corre un gancho AlFallar en un savepoint: si falla, solo se revierte lo suyo.
func (c *Caso[In, Out]) correrAlFallar(ctx context.Context, tx pgx.Tx, fn func(context.Context, pgx.Tx) error) {
	sp, err := tx.Begin(ctx)
	if err == nil {
		err = fn(ctx, sp)
		if err == nil {
			err = sp.Commit(ctx)
		} else {
			_ = sp.Rollback(ctx)
		}
	}
	if err != nil {
		slog.ErrorContext(ctx, "auditoria: falló un gancho AlFallar", "accion", c.Accion, "err", err)
	}
}

func registrar(ctx context.Context, t *Tx, tx pgx.Tx, accion string, entrada any, causa error) error {
	actor := t.Actor()
	entradaJSON, err := json.Marshal(entrada)
	if err != nil {
		entradaJSON = []byte("null")
	}
	resultado := "ok"
	var errorJSON []byte
	if causa != nil {
		resultado = "error"
		errorJSON = describirError(causa)
	}
	var actorID, tokenID *int64
	if actor.ID != 0 {
		actorID = &actor.ID
	}
	if actor.TokenID != 0 {
		tokenID = &actor.TokenID
	}
	var entidad, entidadID *string
	if t.entidad != "" {
		entidad, entidadID = &t.entidad, &t.entidadID
	}
	q := auditoriabd.New(tx)
	id, err := q.InsertarAccion(ctx, auditoriabd.InsertarAccionParams{
		ActorTipo: string(actor.Tipo), ActorID: actorID, ActorNombre: actor.Nombre, TokenID: tokenID,
		Canal: string(contexto.CanalDe(ctx)), Accion: accion, Entidad: entidad, EntidadID: entidadID,
		EmpresaID: t.empresa(), Entrada: entradaJSON, Resultado: resultado, Error: errorJSON,
		RequestID: contexto.RequestIDDe(ctx),
	})
	if err != nil {
		return fmt.Errorf("auditoria: insertar acción %s: %w", accion, err)
	}
	for _, c := range t.cambios {
		if err := q.InsertarCambio(ctx, auditoriabd.InsertarCambioParams{
			AccionID: id, Entidad: c.entidad, EntidadID: c.entidadID, Campo: c.campo, Antes: c.antes, Despues: c.despues,
		}); err != nil {
			return fmt.Errorf("auditoria: insertar cambio %s.%s: %w", c.entidad, c.campo, err)
		}
	}
	return nil
}

// describirError guarda el problema que vio el cliente y, si no era de dominio, el mensaje
// interno (solo para auditoría; nunca sale en la respuesta).
func describirError(causa error) []byte {
	p, interno := nucleo.AProblema(causa)
	d := map[string]any{"type": p.Type, "title": p.Title, "status": p.Status}
	if p.Detail != "" {
		d["detail"] = p.Detail
	}
	var prob *nucleo.Problema
	if interno || !errors.As(causa, &prob) {
		d["interno"] = causa.Error()
	}
	b, _ := json.Marshal(d)
	return b
}

func correrGancho(ctx context.Context, g func(context.Context)) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "auditoria: pánico en un gancho AlConfirmar", "panico", r)
		}
	}()
	g(ctx)
}
