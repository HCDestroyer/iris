# apps/api · API de Iris (Go)

API REST bajo `/api/v1` con OpenAPI 3.1 generado del código (huma v2 sobre chi), Postgres con
`pgx` + `sqlc`, migraciones `goose` y colas River.

```
cmd/iris       servidor HTTP · `iris migrar` · `iris admin crear --correo --nombre`
cmd/worker     workers de River (relay del outbox)
cmd/openapi    escribe ../../packages/contratos/openapi.json
internal/nucleo         errores RFC 9457, EnTx, paginación por cursor, version → 409,
                        Idempotency-Key, cifrado, catálogo de permisos (permisos.go)
internal/nucleo/contexto   Actor, Canal, RequestID, permisos efectivos con alcance
internal/nucleo/identidad  usuarios, sesiones + CSRF, TOTP, roles, tokens iris_pat_
internal/nucleo/servidor   chi + huma, middlewares, /salud y /listo
internal/nucleo/pruebas    Postgres real para pruebas de integración
internal/auditoria      base de casos de uso (Caso[In, Out]): permiso, auditoría y outbox
internal/integracion    outbox, relay (River, único) y suscriptores internos
migraciones/            goose (SQL embebido) + River en el schema `river`
```

## Uso

```sh
cp .env.example .env            # y ajusta IRIS_DB_URL / IRIS_CLAVE_CIFRADO
go run ./cmd/iris migrar        # goose + River + permisos y roles base
go run ./cmd/iris admin crear --correo admin@isp.gt --nombre "Admin"   # imprime la contraseña
go run ./cmd/iris               # :3500, docs en /api/v1/docs
go run ./cmd/worker
go run ./cmd/iris salud          # GET a /api/v1/salud local; sale 0 o 1 (healthcheck de la imagen)
go run ./cmd/iris clave rotar --simular   # rotación de IRIS_CLAVE_CIFRADO (docs/runbooks/rotar-clave-cifrado.md)
```

`make test | lint | sqlc | sqlc-verificar | openapi | openapi-verificar | migrar` (Makefile local).
Herramientas: `sqlc` 1.30, `goose` v3.26 (`go install github.com/pressly/goose/v3/cmd/goose@v3.26.0`),
`golangci-lint` v2.

Pruebas: `go test ./...` contra Postgres real. Cada paquete borra, recrea y migra
`iris_test_i1` (o `IRIS_DB` / `IRIS_DB_URL`) bajo un advisory lock, así los paquetes no chocan.

## Casos de uso

```go
var Escalar = auditoria.Nuevo(auditoria.Caso[EntradaEscalar, Ticket]{
    Accion:  "tickets.escalar",            // = permiso (nucleo/permisos.go)
    Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEscalar) (Ticket, error) {
        q := soportebd.New(t)                       // t es la transacción
        antes, err := q.TicketParaActualizar(ctx, in.ID)
        …
        t.RegistrarCambios("ticket", in.ID, antes, despues) // diff; lo sensible → "***"
        return aTicket(despues), t.Emitir("ticket.estado_cambiado", payload)
    },
})
```

`Ejecutar(ctx, in)` abre la transacción (savepoint si ya hay una), exige el permiso (401/403),
corre la lógica, escribe `auditoria.acciones` + `auditoria.cambios` y los eventos en
`integracion.outbox` en el mismo COMMIT. Si falla, revierte y audita el error aparte.
`AuditarExito` evita la fila en lecturas; `Acceso: auditoria.Publico | Autenticado` para casos
sin permiso. Detalle para otros frentes: `.claude/integration/i1.md`.

## Eventos

Emite (outbox, `empresa_id` null: la identidad es global):

| Evento | Payload |
| --- | --- |
| `usuario.creado` | usuario_id, tipo |
| `usuario.actualizado` | usuario_id, campos[] |
| `usuario.desactivado` | usuario_id, motivo |
| `token.creado` | token_id, usuario_id |
| `token.revocado` | token_id, usuario_id, motivo |

Consume: ninguno todavía. El worker registra un suscriptor `registro` (log en debug).
