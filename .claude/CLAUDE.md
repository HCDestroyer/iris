# CLAUDE.md — Iris

Este archivo lo leen todos los agentes y es la ley del repositorio. Si una tarea te obliga a romper una regla, detente y pregunta al líder.

## Qué es Iris

Iris es el sistema de operación de campo y atención de un ISP. Forma parte de la familia Horus, junto con ArgosISP (gestión, facturación y RADIUS) y Horus Flow (tráfico y seguridad). Cubre:

- **Tickets y SLA:** soporte con prioridad, categoría, SLA, escalamiento e historial por cliente.
- **Órdenes de trabajo:** instalación, visita técnica, retiro y cambio de equipo, con checklist, fotos, firma, GPS y materiales.
- **Agenda y rutas:** calendario por técnico y zona, ruta del día y ubicación en vivo.
- **Seguimiento comercial:** prospecto → factibilidad → cotización → contrato → alta en Argos.

Iris tiene **usuarios propios** (decisión del PM): desde Argos y Horus se entra con autologin por ticket y vinculación de usuarios, nunca con cuentas compartidas.

Iris **no** es dueño de clientes, servicios, facturación ni inventario. Esos datos viven en ArgosISP y se usan por contrato (`.claude/integration/contratos/argosisp.md`). Las alertas de red llegan de Horus (`contratos/horus.md`). Iris funciona completo sin los otros dos: si un conector está apagado, se trabaja con datos locales.

El plan está en `.claude/PLAN.md`, el modelo de datos en `.claude/modelo-datos.md` y cada frente en `.claude/agents/<agente>.md`. Lee los cuatro antes de escribir código.

## Stack (fijo)

| Capa | Tecnología |
| --- | --- |
| API | Go 1.24, `chi` (rutas), `pgx/v5` + `sqlc` (SQL tipado), `goose` (migraciones), `River` (colas sobre Postgres) |
| Base de datos | PostgreSQL 16, un schema por módulo: `nucleo`, `soporte`, `campo`, `agenda`, `comercial`, `integracion`, `auditoria` |
| Caché | Redis 7, solo como caché, con invalidación por evento |
| Panel web | Next.js 15 (App Router) con React 19, TypeScript estricto, Tailwind 4, shadcn/ui e identidad Horus |
| App de campo | PWA en Next.js (`/campo`) con IndexedDB y service worker, que funciona sin señal. Fase 2: Expo (React Native) que reutiliza `packages/` |
| Contrato | OpenAPI 3.1 generado desde la API (`make openapi`) → `packages/contratos` (tipos y cliente con `openapi-typescript` y `openapi-fetch`) |
| Mapas | MapLibre GL con teselas OSM; rutas con OSRM (servicio propio en Docker) |
| Infra | Docker Compose (dev y lab), GitHub Actions; Node 22 y pnpm 10 para el frontend |

## Estructura del monorepo

```
apps/api            Go: cmd/iris (servidor), cmd/worker (River), internal/<modulo>/
apps/web            Next.js: panel (/), app de campo (/campo), portal de tickets (/soporte)
apps/movil          Expo (fase 2)
packages/contratos  OpenAPI generado y cliente TS
packages/ui         componentes compartidos e identidad Horus (tokens, logo)
packages/sync       motor de sincronización sin conexión (cola local, conflictos), compartido con la app nativa
infra/              docker-compose, OSRM, seeds, CI
.claude/            reglas, plan, modelo de datos, contratos y agentes
docs/               runbooks, marca (logo y tokens)
```

Cada agente es dueño de las carpetas que indica su brief y no toca las de otro. Lo que necesite de otro frente lo pide en su archivo `.claude/integration/<agente>.md`.

## Reglas de arquitectura

### 1. La lógica vive en casos de uso

- `apps/api/internal/<modulo>/<verbo_sustantivo>.go`: un caso de uso = un tipo con `Ejecutar(ctx, entrada) (salida, error)`.
- Los handlers HTTP, workers, webhooks y crons solo validan entrada y llaman casos de uso. Cero lógica de negocio fuera de ellos.
- `ctx` lleva `Actor` (usuario, token, integración, sistema), `Canal` y `RequestID` (paquete `internal/nucleo/contexto`).
- Todo caso de uso corre en una transacción (`nucleo.EnTx`) y escribe su fila de auditoría y sus eventos en la misma transacción.

### 2. Acciones explícitas, no PATCH de estados

`POST /tickets/:id/escalar`, `POST /ordenes/:id/cerrar`, `POST /prospectos/:id/ganar`. Nunca `PATCH { estado }`. Cada acción tiene su permiso con el mismo nombre (`ordenes.cerrar`).

### 3. Eventos con outbox

- Los hechos de negocio se escriben en `integracion.outbox` en la misma transacción. Un relay los publica a los consumidores internos y a los webhooks de Argos y Horus.
- Los consumidores son idempotentes: un evento puede llegar dos veces.
- Los webhooks entrantes se guardan primero en `integracion.entrantes` (deduplicados por `X-Webhook-Id`) y se procesan en un job.

### 4. Iris no duplica datos de Argos

- De Argos se guarda solo una **instantánea de lectura** (nombre, dirección, coordenadas, plan, estado) con `sincronizado_en`, que se refresca por evento.
- Lo que cambia datos de Argos (activar servicio, consumir material, crear cliente) se hace llamando a su API, nunca escribiendo en una copia. Si Argos falla, la acción queda en `integracion.pendientes` y se reintenta con espera exponencial. La orden no se pierde y se muestra el error.

### 5. Sin conexión primero (app de campo)

- Todo lo que hace el técnico se guarda primero en el teléfono, en una cola local (`packages/sync`) con id generado en el cliente (UUID v7), y luego se envía a `POST /sync/lote`.
- El servidor es idempotente por id de operación, y los conflictos se resuelven por regla escrita en `contratos/sync.md`, nunca en silencio.
- Las fotos se suben en partes reanudables y la orden no se cierra en el servidor hasta tener sus adjuntos obligatorios.

### 6. Idempotencia y concurrencia

- Toda operación que cambia estado acepta `Idempotency-Key` y la guarda con su respuesta.
- Las ediciones llevan `version`; si hay conflicto, responde 409.
- Los jobs repetibles de River son únicos por clave (`unique`); los crons procesan «todo lo pendiente hasta ahora».

### 7. API

- REST bajo `/api/v1`, recursos en español y en plural: `/tickets`, `/ordenes`, `/visitas`, `/rutas`, `/prospectos`.
- Errores RFC 9457 (`application/problem+json`) con `type`, `title`, `status`, `detail` y `errors[]`.
- Listas con paginación por cursor (`?cursor=&limit=`); la respuesta es `{ data, nextCursor }`.
- El token va en `Authorization: Bearer`. La sesión del panel va en una cookie `HttpOnly`.
- Todo endpoint valida su entrada, y el OpenAPI se genera del código (`make openapi`). Si cambias un endpoint, regenera y commitea `packages/contratos`.

### 8. Permisos (ACL)

- Permiso = nombre de la acción (`tickets.crear`). Los roles agrupan permisos, con alcance opcional por zona o cuadrilla.
- Roles base: `admin`, `supervisor`, `despacho`, `tecnico`, `soporte`, `ventas`, `lectura`.
- Un token de API o de integración es un usuario con alcances acotados y pasa por el mismo ACL.

### 9. Auditoría

- `auditoria.acciones`: actor, canal, acción, entidad, entrada, resultado y `request_id`, en la misma transacción. Nunca `UPDATE` ni `DELETE` sobre `auditoria.*`.
- `auditoria.cambios`: diff por campo. Lo sensible (contraseñas, tokens, firmas) queda como `***`.
- Las posiciones GPS de alta frecuencia no son acciones: van a `campo.posiciones`, sin auditoría por fila.

### 10. Frontend

- Componentes de shadcn/ui en `packages/ui` con los tokens de la identidad Horus (`docs/marca/horus-tokens.css`): un solo color de marca (teal) y estados con icono + texto + color. Íconos solo de Lucide.
- Nada de `<button>` o `<input>` crudos fuera de `packages/ui`.
- Indentación de 4 espacios. TypeScript estricto, sin `any`.
- Tipos y cliente HTTP desde `packages/contratos`; nada de interfaces duplicadas a mano.
- Formularios con `react-hook-form` + `zod`, espejo del validador del backend.
- Modo claro y oscuro funcionando; revisar en 390 px y 1280 px.
- Errores visibles con «Reintentar». Nunca mostrar totales ni estados calculados sobre datos que no se pudieron leer.

### 11. Pruebas y Definition of Done

Una tarea está terminada solo si:

- [ ] El caso de uso tiene pruebas de integración en Go contra Postgres real (testcontainers o el Postgres de CI) y pasan en CI
- [ ] El endpoint está en el OpenAPI y `packages/contratos` se regeneró
- [ ] El caso de uso audita y, si cambia estado, emite su evento por outbox
- [ ] `make lint` (golangci-lint + eslint) y `make typecheck` pasan
- [ ] Las pantallas tienen prueba (Vitest + Playwright) en escritorio y teléfono
- [ ] Hay un `README.md` corto en el módulo: qué hace, qué eventos emite y consume
- [ ] Actualizaste `.claude/integration/<tu-agente>.md` si dependes de otro frente

### 12. Trabajo en paralelo

- **Rama correcta:** empieza siempre con `git fetch origin && git checkout -B agente/<agente> origin/main` o desde la rama que te indique el líder.
- **Base y puerto propios:** pruebas con `IRIS_DB=iris_test_<agente>` y `PORT=35<n>`. Nunca uses la base de otro.
- **Archivos generados nunca a mano:** `packages/contratos/*`, `pnpm-lock.yaml`, `go.sum` y el código de `sqlc` se regeneran.
- **Migraciones con `goose create`:** el nombre lleva la marca de tiempo real.
- **Una PR se mergea solo con CI en verde contra el `main` actual,** de una en una.
- **Fixtures fieles a la API:** tipados con los tipos de `packages/contratos`.
- **Contratos con otros sistemas:** `contratos/argosisp.md` y `contratos/horus.md` son espejo del repositorio del otro lado. Se cambian con PRs enlazadas en los dos repos.

## Catálogo de eventos (nombre, payload mínimo)

| Evento | Emite | Payload |
| --- | --- | --- |
| `ticket.creado` | soporte | ticket_id, empresa_id, cliente_id (o null), servicio_id (o null), categoria, prioridad, origen |
| `ticket.estado_cambiado` | soporte | ticket_id, de, a, motivo |
| `ticket.asignado` | soporte | ticket_id, usuario_id (o null), cuadrilla_id (o null) |
| `ticket.respondido` | soporte | ticket_id, mensaje_id |
| `ticket.escalado` | soporte | ticket_id, de, a, nivel, motivo |
| `ticket.fusionado` | soporte | ticket_id, destino_id, motivo (o null) |
| `ticket.sla_en_riesgo` / `ticket.sla_vencido` | soporte (cron) | ticket_id, sla, vence_en |
| `orden.creada` | campo | orden_id, tipo, ticket_id (o null), cliente_id, servicio_id, zona_id |
| `orden.programada` | agenda | orden_id, tecnico_id, cuadrilla_id, inicio, fin |
| `orden.estado_cambiada` | campo | orden_id, de, a, motivo |
| `orden.cerrada` | campo | orden_id, tipo, resultado, materiales[], equipos[] (serie, mac, accion) |
| `ruta.publicada` | agenda | ruta_id, tecnico_id, fecha, ordenes[] |
| `prospecto.creado` / `prospecto.ganado` / `prospecto.perdido` | comercial | prospecto_id, empresa_id, motivo (perdido) |
| `factibilidad.evaluada` | comercial | prospecto_id, resultado, nodo/router sugerido |
| `usuario.creado` / `usuario.actualizado` / `usuario.desactivado` | identidad (núcleo) | usuario_id; tipo (creado), campos[] (actualizado), motivo (desactivado) |
| `token.creado` / `token.revocado` | identidad (núcleo) | token_id, usuario_id, motivo (revocado) |

El detalle de cada payload vive en `.claude/integration/contratos/eventos.md`.

## Convenciones

- Código, tablas, rutas y mensajes en español. Go: nombres exportados en `PascalCase` y paquetes en minúscula; TS: `camelCase`/`PascalCase`; tablas y columnas en `snake_case`.
- Commits Conventional Commits con alcance = módulo: `feat(soporte): escalar ticket por SLA`.
- Una PR por tarea del brief, con su número en el título.
- Variables de entorno documentadas en `.env.example`; nunca secretos en el repo.
- Fechas en UTC en la base; zona `America/Guatemala` solo al mostrar.
- Multiempresa igual que Argos: todo lo operativo lleva `empresa_id` (la de Argos) y se filtra por la empresa activa.
- No agregues dependencias nuevas sin anotar el motivo en la PR.
