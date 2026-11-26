# CLAUDE.md — Iris

Este archivo lo leen todos los agentes y es la ley del repositorio. Si una tarea te obliga a romper una regla, detente y pregunta al líder.

## Regla del PM: siempre en español

**Al PM se le responde siempre en español, sin excepción**, también después de que se comprima el contexto y aunque las herramientas, los agentes o los resúmenes vengan en inglés. Es la única regla que el PM pidió recordar siempre. Los mensajes al PM van breves y con el resultado primero: el PM no es técnico y lee en el teléfono.

## Memoria del líder

El líder lee `.claude/lider.md` al empezar cada sesión y después de cada compresión del contexto: tiene las reglas del PM, la forma de trabajar y el estado actual. Lo actualiza cuando algo de eso cambia. **Regla de trabajo del PM:** si ya dijo cómo lo quiere, se avanza y se le muestra el resultado; no se espera un segundo visto bueno. Esto vale para decisiones de producto y de diseño: no sustituye el CI en verde, la revisión de seguridad de cada PR ni la confirmación de acciones irreversibles o que afecten a otros sistemas.

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
| Panel web | Next.js 15 (App Router) con React 19, TypeScript estricto, Tailwind 4 y HeroUI v3 (React Aria) envuelto en `packages/ui` con los tokens de Iris (`--iris-*`: índigo y cian, logo en degradado) |
| App de campo | PWA en Next.js (`/campo`) con IndexedDB y service worker, que funciona sin señal. Fase 2: Expo (React Native) que reutiliza `packages/`. App nativa: HeroUI Native (Uniwind) en la fase 5 |
| Contrato | OpenAPI 3.1 generado desde la API (`make openapi`) → `packages/contratos` (tipos y cliente con `openapi-typescript` y `openapi-fetch`) |
| Mapas | MapLibre GL con teselas OSM; rutas con OSRM (servicio propio en Docker) |
| Infra | Docker Compose (dev y lab), GitHub Actions; Node 22 y pnpm 10 para el frontend |

## Estructura del monorepo

```
apps/api            Go: cmd/iris (servidor), cmd/worker (River), internal/<modulo>/
apps/web            Next.js: panel (/), app de campo (/campo), portal de tickets (/soporte)
apps/movil          Expo (fase 2)
packages/contratos  OpenAPI generado y cliente TS
packages/ui         componentes compartidos e identidad de Iris (tokens `--iris-*`, logo en degradado)
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
- Roles base: `admin`, `supervisor`, `despacho`, `tecnico`, `soporte`, `ventas`, `lectura`, `caja`, y para los usuarios de tipo integración `integracion_argos` e `integracion_horus` (permisos del §1.2 de cada contrato).
- Un token de API o de integración es un usuario con alcances acotados y pasa por el mismo ACL.

### 9. Auditoría

- `auditoria.acciones`: actor, canal, acción, entidad, entrada, resultado y `request_id`, en la misma transacción. Nunca `UPDATE` ni `DELETE` sobre `auditoria.*`.
- `auditoria.cambios`: diff por campo. Lo sensible (contraseñas, tokens, firmas) queda como `***`.
- Las posiciones GPS de alta frecuencia no son acciones: van a `campo.posiciones`, sin auditoría por fila.

### 10. Frontend

**Sistema visual:** HeroUI v3 (React Aria) envuelto en `packages/ui` (`@iris/ui`), con los tokens `--iris-*` de `packages/ui/src/estilos/iris-tokens.css` (copia de `docs/marca/iris-tokens.css`): índigo `#4F46E5` como color principal (`#818CF8` en oscuro, con texto `#0B0F1A` encima), cian `#06B6D4` como secundario y el logo en degradado índigo → cian (`#4F46E5 → #4338CA → #0E7490` cuando lleva texto blanco encima). Ningún teal. Guía, reglas, contraste y mapa de componentes: `docs/ui/guia.md`.

- **Solo componentes del kit.** Las pantallas importan de `@iris/ui`, nunca de `@heroui/react`. Nada de `<button>`, `<input>`, `<select>`, `<textarea>`, `<table>` ni `<dialog>` crudos fuera de `packages/ui` (lo revisa ESLint). Si falta una pieza, se agrega a `packages/ui` con nombre en español y prueba.
- **HeroUI primero:** toda pieza de interfaz es un componente de HeroUI envuelto en `@iris/ui`; un control propio solo si HeroUI no lo trae, con su motivo escrito en `docs/ui/guia.md` (ESLint prohíbe importar `@heroui/*` fuera de `packages/ui`). El CSS propio que haga falta para lograr el diseño va en los envoltorios de `packages/ui`, no regado en cada pantalla.
- **Campos de HeroUI:** fechas y horas con `CampoFecha`, `SelectorRangoFechas`, `CampoHora`; montos con `CampoMoneda` (NumberField en GTQ, con su paso); texto con `Campo` (TextField), `CampoArea`, `CampoBusqueda`; elección con `CampoSelector`, `Autocompletar`, `GrupoOpciones`, `Casilla` e `Interruptor`; archivos con `SelectorArchivo` / `MenuArchivos`.
- **Marco del panel:** menú como isla flotante a 16 px del borde (`MenuIsla`), compacto o expandido, con el botón `‹ ›` de 28 px sobre el borde derecho a la altura del logo. Encabezado sin barra (`EncabezadoPanel`): a la izquierda lo que haces (Buscar Ctrl K, accesos rápidos y «Más»), a la derecha lo que vigilas (Argos y Horus, avisos y la cuenta, `MenuCuenta`). En el teléfono, la isla va abajo con el botón redondo de la acción principal.
- **Íconos de Lucide** en menú, botones, pestañas y filas; los botones de solo ícono llevan `aria-label` y globo. **Cada estado lleva ícono + texto + color**; el punto discreto solo crece cuando hay que actuar.
- **Qué contenedor usar:** modal para acciones cortas de 1 a 3 campos (nunca uno encima de otro); panel lateral (`PanelLateral`, con telón si es un formulario) para hacer algo sin perder la lista; vista rápida (`VistaRapida`) en las listas, sin quitarle ancho; página propia (`FormularioPagina`) para formularios de más de 3 campos o con pasos. «Atrás» vuelve a donde estabas y salir con cambios sin guardar pregunta antes.
- **Listas en lugar de cuadros** (`ListaFilas`), con buscador y filtros cuando pueden crecer, vista rápida y menú `⋯` por fila (`MenuAcciones`: frecuentes primero; peligrosas al final, separadas y en rojo; filtrado por permisos).
- **Fichas:** franja con la identidad, sus contadores, una sola acción principal y «Más» (`FranjaFicha`). Pestañas segmentadas (`PestanasSegmentadas`) dentro de la misma tarjeta que su contenido.
- **Un solo campo destacado a la vez. Un mismo dato aparece una sola vez por pantalla.**
- **El scroll va dentro del contenedor,** nunca en la página.
- **Un «?» del manual en cada pantalla,** junto al título (`BotonManual`, enlace a `/manual/<clave>`; dentro de `/campo`, a `/campo/manual/<pantalla>`). Las claves son las de `docs/manual/claves.md`.
- **Teléfono:** cada pantalla a pantalla completa, con la acción principal fija abajo (`AccionFija`).
- Modo claro y oscuro; revisar en 390 px y 1280 px, sin desbordes.
- Errores visibles con «Reintentar». Nunca mostrar totales ni estados calculados sobre datos que no se pudieron leer.
- Migas (`Migas`) en cada pantalla del panel. Aviso emergente (`avisar`) después de cada acción que cambia algo.
- Indentación de 4 espacios. TypeScript estricto, sin `any`. Tipos y cliente HTTP desde `packages/contratos`; nada de interfaces duplicadas a mano. Formularios con `react-hook-form` + `zod`, espejo del validador del backend.

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
| `ticket.estado_cambiado` | soporte | ticket_id, de, a, motivo, numero, empresa_id, cliente_id, servicio_id, origen; va a Argos (§6.6, 0.9.0) sin motivo y con puede_confirmar |
| `ticket.asignado` | soporte | ticket_id, usuario_id (o null), cuadrilla_id (o null) |
| `ticket.respondido` | soporte | ticket_id, mensaje_id, numero, empresa_id, cliente_id, autor_tipo (agente, sistema), masivo, aviso_por_correo; va a Argos (§6.6, 0.9.0) |
| `ticket.escalado` | soporte | ticket_id, de, a, nivel, motivo |
| `ticket.fusionado` | soporte | ticket_id, destino_id, motivo (o null) |
| `ticket.sla_en_riesgo` / `ticket.sla_vencido` | soporte (cron) | ticket_id, sla, vence_en |
| `ticket.resolucion_propuesta` | soporte (Horus, cierre de orden) | ticket_id, accion (resolver \| cerrar), motivo, origen (horus \| orden), entrante_id (o null), orden_id (o null) |
| `ticket.mensaje_cliente` | soporte (portal, portal de Argos) | ticket_id, mensaje_id, canal (portal, portal_argos); nunca vuelve a Argos |
| `encuesta.enviada` | soporte | encuesta_id, entidad (ticket \| orden), entidad_id, canal (correo \| enlace) |
| `encuesta.respondida` | soporte (público) | encuesta_id, entidad, entidad_id, calificacion, alerta (true si ≤ 2) |
| `encuesta.anulada` | soporte | encuesta_id, entidad (ticket), entidad_id, motivo (reabierto \| fusionado) |
| `soporte.horus_descartado` | soporte (Horus) | evento, entrante_id, empresa_id (o null), motivo, detalle |
| `soporte.posible_falla_general` | soporte (caídas de Argos) | ticket_id, empresa_id, tickets, agrupados |
| `soporte.caidas_ajustes_cambiados` | soporte | empresa_id, campos[], caidas_umbral, caidas_ventana_min, falla_umbral, verificacion_horas |
| `soporte.aviso_falla` | soporte (falla general) | incidente, empresa_id, cliente_id, tipo (apertura, cierre), ticket_id; nunca servicio ni dirección |
| `soporte.verificacion_vencida` | soporte (falla general) | ticket_id, numero, empresa_id, asignado_a (o null) |
| `correo.recibido` | soporte (worker de correo) | correo_id, buzon_id, estado, resultado (o null), ticket_id (o null), mensaje_id (o null), motivo (o null) |
| `buzon.creado` / `buzon.actualizado` / `buzon.activado` / `buzon.desactivado` | soporte | buzon_id; correo (creado) |
| `orden.creada` | campo | orden_id, tipo, ticket_id (o null), cliente_id, servicio_id, zona_id, origen (panel, api, argos, sistema, asistente) |
| `orden.asignada` | campo | orden_id, tecnico_id (o null), cuadrilla_id (o null), tecnico_anterior_id (o null), origen (asignar, tomar, programar, reprogramar, crear), motivo (o null) |
| `orden.programada` | agenda | orden_id, tecnico_id, cuadrilla_id, inicio, fin, ventana_inicio/ventana_fin (o null), reprogramada, origen (programar, reprogramar, asignar), motivo (o null) |
| `orden.estado_cambiada` | campo | orden_id, de, a, motivo |
| `orden.cerrada` | campo | orden_id, tipo, resultado, servicio_id, cliente_id, tecnico_id, bodega_id, retirar_servicio, equipos_validados, materiales[] (producto_id, cantidad, bodega_id, bodega_tipo (o null), usuario_id (o null), desde_app, fecha, recibido_en_iris), equipos[] (accion: instalar \| retirar \| cambio \| no_recuperado, unidad_id, serie, mac, condicion, bodega_id, usuario_id (o null), desde_app, enviado_al_aplicar, fecha, recibido_en_iris; cambio: sale_unidad_id, entra, motivo, destino_sale, cobro_propuesto?; no_recuperado: motivo, foto_ref (obligatoria desde 0.14.0)), cargo (o null: lineas[] (concepto_id, precio_unitario, descripcion?) y descripcion desde 0.14.3, la cola quita la línea de instalación si hay un cobro de A36 vigente; la forma anterior producto_id?, monto?, descripcion es null si la instalación ya tiene un cobro registrado desde la orden, B15), activacion (o null fuera de una instalación: modo, estado (ninguna, pendiente, en_proceso, listo, fallido, activado_fuera), aprovisionamiento_id, cerrada_sin_activacion_motivo) y equipos[].aprovisionado (0.9.0 §12.2: no se repiten activar-instalacion ni el comodato) |
| `orden.activacion_pedida` / `orden.activacion_cambiada` | campo (activación, 0.9.0 §12.2) | orden_id, activacion_id, intento, servicio_id; equipo_tipo, con_wifi, origen (campo, panel), pendiente_id (pedida); de, a, causa (o null), aprovisionamiento_id (o null), fuente (pendiente, webhook, consulta) (cambiada); nunca el SSID ni la clave |
| `orden.cobro_registrado` / `orden.cobro_cambiado` | campo (cobro de la instalación, B15) | orden_id, cargo_id; servicio_id, concepto_id, precio_unitario, precio_editado, pendiente_id (registrado); de, a (pendiente, facturado, pagado, anulado, fallido), factura_id (o null) (cambiado) |
| `orden.bienvenida_cambiada` / `orden.bienvenida_reenviada` | campo (bienvenida, B16) | orden_id, servicio_id; canales[] (canal, estado, motivo), cumplida, entrante_id (cambiada); n, canales[], pendiente_id (reenviada); nunca el texto ni las cuentas |
| `corte_manual.estado_cambiado` | campo (corte manual de cable, 0.15.0 §13.2) | solicitud_id, empresa_id, version (contador de Iris), estado (en_curso, resuelta, no_realizada, cancelada), motivo_codigo (sin_acceso, equipo_no_encontrado, otro; solo no_realizada), referencia (número de la orden o null), referencia_tipo (orden o null), ocurrido_en, orden_id, servicio_id, accion, conector_id; va a Argos ≥ 0.15.0 (§6.6) solo al conector que pidió el corte y en las empresas de su alcance, sin orden_id, servicio_id, accion ni conector_id |
| `wifi.cambio_pedido` / `wifi.cambio_resuelto` | integracion (equipos, 0.9.0 §12.4) | cambio_id, empresa_id, servicio_id, origen_tipo (iris_orden, iris_ticket, iris_conversacion), origen_id; actor, metodo, con_ssid, con_clave (pedido); de, a (en_proceso, aplicado, fallido), causa (o null) (resuelto); nunca el SSID ni la clave |
| `orden.pausada` / `orden.reanudada` | campo | orden_id, pausa_id; motivo, nota (o null), desde (pausada); a, duracion_seg, tiempo_pausado_seg (reanudada) |
| `orden.formulario_agregado` / `orden.formulario_quitado` | campo | orden_id, formulario_id, plantilla_id, origen; version_id, numero_version (agregado) |
| `campo.ajustes_cambiados` | campo | empresa_id (o null), campos[], tecnicos_toman_libres, activacion_modo (al_cerrar, exigir_lista, pendiente_agente), cierre_exige_bienvenida |
| `plantilla.creada` / `plantilla.actualizada` / `plantilla.activada` / `plantilla.desactivada` | campo | plantilla_id; codigo, empresa_id (creada); version_id, numero_version, version_nueva (actualizada) |
| `zona.creada` / `zona.actualizada` | agenda | zona_id; origen, nombre (creada); campos[], activa (actualizada) |
| `cuadrilla.creada` / `cuadrilla.actualizada` | agenda | cuadrilla_id, empresa_id, version, miembros[] (usuario_id, id_externo o null), jefe_usuario_id (o null); nombre, zonas[] (creada); campos[], activa (actualizada); van también a Argos |
| `tecnico.creado` / `tecnico.actualizado` | agenda | tecnico_id (ficha), usuario_id; cuadrilla_id (creado); campos[], activo (actualizado) |
| `tecnico.ausencia_registrada` / `tecnico.ausencia_anulada` | agenda | tecnico_id, usuario_id, ausencia_id; inicio, fin, motivo, citas_en_conflicto (registrada); motivo (anulada) |
| `tecnico.argos_vinculado` / `tecnico.argos_desvinculado` | agenda (I1) | tecnico_id, usuario_id, empresa_id, version, id_externo, conector_id (o null), carga_id (o null); anterior_id_externo (vinculado); motivo (o null; desactivado) (desvinculado); uno por empresa, todos con la misma version (un contador por técnico, uno por cambio); al cambiar el id_externo, primero el desvinculado del anterior; van a Argos ≥ 0.14.0 (§6.6), sin conector_id ni carga_id |
| `tarea.creada` / `tarea.completada` / `tarea.anulada` | agenda | tarea_id, empresa_id, tecnico_id; creada_por, inicio, fin, duracion_min, orden_id (o null), cliente_id (o null), origen, solapa_con (creada); completada_por, completada_en (completada); anulada_por, motivo (o null) (anulada) |
| `ruta.propuesta` / `ruta.publicada` | agenda | ruta_id, tecnico_id, fecha, ordenes[], motor, degradada; modo, motivo_motor (propuesta); reemplaza_id, version (publicada) |
| `ruta.desactualizada` | agenda | ruta_id, tecnico_id, fecha, estado, orden_id, motivo |
| `prospecto.creado` / `prospecto.ganado` / `prospecto.perdido` | comercial | prospecto_id, empresa_id; origen, zona_id, vendedor_id, solicitud_tipo (portal_argos) (creado); modo (local, argos, solicitud), contrato_id (o null), cotizacion_id, orden_id (o null), pendientes[], tarea_argos (solicitud) (ganado); motivo (perdido) |
| `prospecto.tarea_argos_completada` | comercial (portal de Argos) | prospecto_id, empresa_id, solicitud_tipo |
| `solicitudes.ajustes_cambiados` | comercial (portal de Argos) | empresa_id, cambio_plan_exige_contrato, version |
| `solicitud_servicio.estado_cambiado` | comercial (portal de Argos) | solicitud_id, numero, empresa_id, cliente_id, tipo, servicio_id (o null), de, a (estado_cliente); va a Argos (§6.6, 0.9.0) |
| `prospecto.actualizado` / `prospecto.asignado` / `prospecto.estado_cambiado` / `prospecto.actividad_registrada` | comercial | prospecto_id; campos[] (actualizado); vendedor_id, vendedor_anterior_id (asignado); de, a, motivo (estado); actividad_id, tipo (actividad) |
| `prospecto.alta_actualizada` | comercial (alta en Argos) | prospecto_id, estado (esperando_servicio, esperando_activacion, completa), cliente_id, servicio_id (o null), orden_id (o null); también al pasar al titular nuevo (B17) |
| `prospecto.venta_revisada` | comercial (B20, cambio de plan o titular hecho en Argos) | prospecto_id, empresa_id, vendedor_id (o null), orden_id, motivo (plan, titular), cotizacion_id (la revisión, vigente), contrato_id (regenerado en borrador, se firma en la orden; o null con la orden cancelada, fallida o cerrada) |
| `factibilidad.evaluada` | comercial | prospecto_id, factibilidad_id, resultado (viable, no_viable, requiere_obra, indeterminado), motor, motivo (o null), router_id/caja (o null), distancia_m |
| `factibilidad.umbrales_cambiados` | comercial | empresa_id, viable_m, obra_m |
| `cotizacion.creada` / `cotizacion.aceptada` | comercial | cotizacion_id, prospecto_id, numero; revision, plan_id, total_mensual, total_unico, moneda, reemplaza_id (creada) |
| `contrato.creado` / `contrato.firmado` / `contrato.anulado` | comercial | contrato_id, prospecto_id; cotizacion_id (creado); adjunto_id, sha256, orden_id (o null: regenerado firmado en la orden) (firmado); de, motivo (anulado) |
| `horus.router_estado_cambiado` / `horus.servicio_infra_cambiado` | integracion (horus) | router_id / infra_id, nombre, estado (caido, arriba), empresa_id (o null) |
| `horus.hallazgo_abierto` / `horus.hallazgo_resuelto` | integracion (horus) | hallazgo_id, categoria, severidad, ip, cliente_id/servicio_id/empresa_id (o null + motivo_sin_cliente), url_horus |
| `usuario.creado` / `usuario.actualizado` / `usuario.desactivado` | identidad (núcleo) | usuario_id; tipo (creado), campos[] (actualizado), motivo (desactivado) |
| `usuario.mfa_reiniciado` | identidad (núcleo) | usuario_id, motivo, sesiones_cerradas, tokens_revocados, tokens_conservados |
| `usuario.contrasena_restablecida` | identidad (núcleo) | usuario_id, motivo, sesiones_cerradas, tokens_revocados, tokens_conservados; nunca la contraseña |
| `usuario.sso_vinculado` / `usuario.sso_desvinculado` | identidad (núcleo, autologin) | usuario_id, vinculo_id, conector_id, usuario_externo; sistema, origen (usuario, admin) (vinculado); motivo (desvinculado) |
| `usuario.sesiones_cerradas` | identidad (núcleo) | usuario_id, sesiones[], todas, motivo (o null) |
| `usuario.argos_abierto` | integracion (núcleo, autologin Iris → Argos) | usuario_id, conector_id, con_destino; nunca la URL ni el ticket |
| `usuario.rol_asignado` / `usuario.rol_quitado` | identidad (núcleo) | usuario_id, rol, zona_id (o null), cuadrilla_id (o null) |
| `rol.creado` / `rol.actualizado` / `rol.activado` / `rol.desactivado` | identidad (núcleo) | rol_id; codigo, permisos[] (creado); campos[], permisos_agregados[], permisos_quitados[] (actualizado); codigo, usuarios, motivo (activado, desactivado) |
| `token.creado` / `token.revocado` / `token.eliminado` | identidad (núcleo) | token_id, usuario_id; motivo (revocado); motivo (manual, lote, conector) (eliminado) |
| `conector.creado` / `conector.activado` / `conector.desactivado` / `conector.eliminado` | integracion (núcleo) | conector_id, sistema; origen (creado), motivo (desactivado); motivo (manual, lote), token_id (o null) (eliminado) |
| `argos.servicio_retirado` | integracion (núcleo, webhook de Argos) | servicio_id, cliente_id (o null), de, a, entrante_id |
| `argos.datos_cambiados` | integracion (argos) | entidad (servicio \| cliente), cliente_id, servicio_id (o null), empresa_id (o null), campos[], entrante_id |
| `argos.traslado_cambiado` | integracion (argos) | traslado_id, estado (enviado, recibido, cancelado), empresa_id, usuario_id (o null), entrante_id |
| `argos.equipo_cambiado` | integracion (argos, webhook de Argos) | servicio_id, cliente_id (o null), empresa_id, sale_unidad_id, entra_unidad_id, motivo (o null), destino_sale (funciona, revision, garantia, danado o null), origen (iris_orden o null), origen_id (o null), entrante_id; nunca costos, proveedor ni RMA |
| `argos.unidad_estado_cambiado` | integracion (argos, webhook de Argos) | unidad_id, empresa_id, servicio_id (o null), de (o null), a (disponible, en_comodato, por_recuperar, revision, garantia, dada_de_baja, no_recuperada), motivo (o null), entrante_id; nunca costos, proveedor ni RMA |
| `argos.sincronizacion_iniciada` / `argos.sincronizacion_terminada` | integracion (argos, primera sincronización) | sincronizacion_id, conector_id; origen (activacion, manual), excluir_retirados, contrato_remoto (iniciada); resultado (completa, con_errores), recursos[] (recurso, estado, filas), iniciada_en, terminada_en (terminada) |
| `argos.certificado_cambiado` | integracion (argos, certificado TLS) | conector_id, certificado_id, anterior_id, host, huella_sha256, huella_anterior, emisor; nunca bloquea |
| `argos.comprobante_resuelto` | integracion (argos, webhook de Argos) | comprobante_id, estado (aprobado, rechazado), empresa_id, cliente_id, servicio_id (o null), canal, pago_id (o null), servicio_estado (o null), entrante_id; es solo un aviso (sin monto, motivo ni causa) |
| `argos.instalacion_resuelta` | integracion (argos, webhooks instalacion.aprovisionada / .fallida, 0.9.0) | aprovisionamiento_id, servicio_id, empresa_id, cliente_id (o null), origen_tipo, origen_id, idempotency_key (o null), estado (listo, fallido), entrante_id; servicio_estado, equipo (tipo, unidad_id) (listo); causa, reintentable, detalle (fallido); nunca el Wi-Fi ni credenciales |
| `argos.wifi_resuelto` | integracion (argos, webhook equipo.wifi_resuelto, 0.9.0) | solicitud_id, servicio_id, empresa_id, origen_tipo, origen_id, idempotency_key (o null), estado (aplicado, fallido), causa (o null), entrante_id; nunca el SSID ni la clave |
| `argos.bienvenida_cambiada` | integracion (argos, webhook servicio.bienvenida_cambiada, 0.9.0) | servicio_id, empresa_id, cliente_id (o null), origen_tipo (o null), origen_id (o null), reenvio, canales[] (canal, estado, motivo, aviso_id), entrante_id; nunca el texto ni las cuentas |
| `argos.factura_cambiada` | integracion (argos, webhooks factura.* y pago.* de una orden de Iris, 0.9.0) | webhook, factura_id, pago_id (o null), numero, empresa_id, origen_id, estado, total, saldo, moneda, entrante_id |
| `argos.cuenta_bancaria_confirmada` | integracion (argos, bienvenida §12.5) | cuenta_bancaria_id, empresa_id, usuario_id; nunca el número ni el titular |
| `argos.promesa_cambiada` | integracion (argos, webhooks promesa.*) | promesa_id, estado, tipo, empresa_id, cliente_id, servicio_id, comprobante_id (o null), canal, vence, motivo_reconexion (o null), motivo (cancelada), entrante_id; nunca el monto |
| `argos.contactos_cambiados` | integracion (argos, webhook `cliente.contactos_cambiados`) | cliente_id, empresa_id (o null), contacto_id (o null), accion (creado, editado, quitado), persona_cliente_id (o null), entrante_id; nunca números ni nombres |
| `argos.caidas_repetidas` | integracion (argos, webhook servicio.caidas_repetidas) | servicio_id, empresa_id, cliente_id (o null), router_id (o null), sesiones, ventana_minutos, desde, hasta, entrante_id |
| `argos.falla_general_abierta` / `argos.falla_general_cerrada` | integracion (argos, webhooks red.falla_general_*) | incidente_id, empresa_id, alcance, alcance_id, desde, hasta (o null), entrante_id; servicios[] (los de la instantánea), descartados, por_verificar, paginando (abierta); nunca nombres ni direcciones |
| `argos.falla_general_servicios` | integracion (argos, job argos.verificar_incidente) | incidente_id, empresa_id, entrante_id, servicios[] (verificados en Argos), descartados, no_verificados |
| `argos.corte_manual` | integracion (argos, webhooks servicio.corte_manual_*, 0.15.0) | evento (solicitado, cancelado), solicitud_id, version, solicitada_en, servicio_id, empresa_id, cliente_id (o null), componente, accion (corte, reconexion), causa, conector_id, entrante_id, descarte (o null: fuera_de_alcance, servicio_desconocido); nunca datos personales |
| `argos.cuenta_bancaria_cambiada` | integracion (argos, webhook de Argos y relecturas) | cuenta_bancaria_id, empresa_id, campos[], activa, creada, entrante_id (o null); nunca el número de cuenta ni el titular |
| `orden.datos_argos_cambiados` | campo (I1) | orden_id, numero, entidad, campos[], entrante_id |
| `argos.titular_cambiado` | integracion (argos, webhook servicio.titular_cambiado confirmado con Argos; B17) | servicio_id, empresa_id (o null), de_cliente_id (o null), a_cliente_id, de_cliente_eliminado, origen_tipo (o null), origen_id (o null), tipo_titular (persona, empresa), entrante_id; nunca el documento ni el motivo |
| `orden.titular_movido` | campo (B17) | orden_id, numero, servicio_id, de_cliente_id (o null), a_cliente_id, tipo_titular (o null), entrante_id |
| `orden.identidad_registrada` / `orden.cambio_titular_registrado` | campo (paso «Confirmar titular y plan», §3.4) | orden_id, pendiente_id, documento_tipo (dpi, pasaporte), reemplaza_id (o null); toma, metodo (identidad); cambio, tipo_titular (persona, empresa), motivo_codigo (cambio); nunca el número, el nombre ni la foto |
| `orden.revision_titular_levantada` | campo (panel, §3.4.8) | orden_id, usuario_id; nunca el motivo |
| `orden.contrato_sin_firma` | comercial y campo (B20: cambio de plan o titular sin la firma del contrato nuevo; campo, al cerrar el supervisor con el cambio en la cola) | orden_id, prospecto_id, empresa_id, contrato_id (o null), cotizacion_id (o null), operacion (cambiar_plan, cambiar_titular o null), motivo (cierre_sin_firma, cambio_tras_cierre, sin_regenerar; diagnósticos sin_cotizacion_viva, sin_plan, revision_rechazada, sin_titular), clave y detalle (diagnósticos) |
| `orden.paso_titular_tras_cancelar` | integracion (pendientes) | orden_id, pendiente_id, operacion, estado_orden (cancelada, fallida) |
| `argos.servicio_eliminado` / `argos.router_eliminado` | integracion (núcleo, webhook de Argos) | servicio_id, cliente_id, empresa_id, router_id (o null), entrante_id (servicio); router_id, empresa_id (o null = compartido), entrante_id (router) |
| `argos.servicio_restaurado` / `argos.router_restaurado` | integracion (núcleo, lectura de Argos) | servicio_id, cliente_id, empresa_id (o null), eliminado_entrante_id (servicio); router_id, empresa_id (o null), eliminado_entrante_id (router) |
| `empresa.creada` / `empresa.actualizada` | identidad (núcleo) | empresa_id; origen (creada), campos[] (actualizada) |
| `usuario.empresa_asignada` / `usuario.empresa_quitada` | identidad (núcleo) | usuario_id, empresa_id |
| `empresa.datos_adoptados` | identidad (núcleo) | empresa_id, filas (por tabla) |
| `adjunto.registrado` / `adjunto.completado` / `adjunto.rechazado` | adjuntos (núcleo) | adjunto_id, entidad, entidad_id, tipo, clave_checklist (o null); mime, tamano, sha256 (completado); motivo (rechazado; también `abandonado`) |
| `integracion.pendiente_hecho` / `integracion.pendiente_fallido` / `integracion.pendiente_agotado` / `integracion.pendiente_reintentado` | integracion (pendientes) | pendiente_id, operacion, entidad, entidad_id, idempotency_key; intentos, status, detalle (fallido, agotado); de (reintentado) |
| `integracion.pendiente_omitido` | integracion (pendientes) | orden_id, motivo, operaciones[] |
| `integracion.pendiente_corregido` / `integracion.pendiente_conflicto` | integracion (pendientes) | pendiente_id, operacion, entidad, entidad_id, idempotency_key; estado, motivo, payload_anterior, payload_nuevo (conflicto) |
| `integracion.pendiente_bloqueado` / `integracion.pendiente_desbloqueado` | integracion (pendientes) | pendiente_id, operacion, entidad, entidad_id, idempotency_key, depende_de; motivo (bloqueado) |
| `integracion.pendiente_resuelto_manual` | integracion (pendientes) | pendiente_id, operacion, entidad, entidad_id, idempotency_key, de, motivo, usuario_id (o null), resuelto_cliente_id (o null: cliente final de un cambio de titular) |
| `integracion.pendiente_cliente_fijado` | integracion (pendientes) | pendiente_id, operacion, entidad, entidad_id, idempotency_key, cliente_id, motivo, usuario_id (o null) |
| `integracion.webhook_reenviado` | mensajeria (webhooks a Argos) | webhook_id, evento, empresa_id (o null), intentos_anteriores |
| `argos.carga_vinculos_detenida` | integracion (argos, carga de vínculos 0.14.0) | conector_id, carga_id, agotados, episodio; uno por episodio |
| `integracion.pendiente_clave_reutilizada` | integracion (pendientes) | pendiente_id, operacion, entidad, entidad_id, empresa_id (o null), seguridad (cambio o intento); nunca la clave ni los cuerpos |
| `aviso.creado` | mensajeria | aviso_id, empresa_id, cliente_id, servicio_id (o null), plantilla, estado (en_cola \| bloqueado), motivo (o null), origen (argos \| iris), prioridad, programado_en (o null), aviso_padre_id (solo en la copia a un contacto) |
| `aviso.enviado` / `aviso.leido` | mensajeria | aviso_id, origen; canal, canal_id (o null) (enviado); leido_en (leido) |
| `aviso.entregado` / `aviso.fallido` | mensajeria | aviso_id, idempotency_key, empresa_id, cliente_id, canal (o null), origen; entregado_en, leido_en (o null) (entregado); motivo (vencido, numero_invalido, rechazado_por_canal, agotado), detalle (o null) (fallido). Uno por aviso; van a Argos (§6.6) |
| `consentimiento.registrado` | mensajeria | consentimiento_id, empresa_id, cliente_id, canal, estado (acepta \| baja), origen, ocurrido_en, estado_vigente, por_numero (permiso de un número de contacto; nunca el número) |
| `contacto.baja` | mensajeria (chat) | cliente_id, empresa_id, canal, ocurrido_en, origen (cliente \| agente), consentimiento_id; va a Argos (§6.6) |
| `chat.conversacion_creada` | mensajeria (chat) | conversacion_id, empresa_id, tipo (directa \| cuadrilla \| orden), cuadrilla_id (o null), orden_id (o null), creada_por, miembros[] |
| `chat.mensaje_enviado` | mensajeria (chat) | conversacion_id, mensaje_id, empresa_id, tipo, cuadrilla_id, orden_id, autor_id, seq, contenido (texto, foto, ubicacion), enviado_en, destinatarios[]; nunca el texto |
| `chat.mensaje_eliminado` | mensajeria (chat) | conversacion_id, mensaje_id, empresa_id, autor_id, eliminado_en |
| `chat.mencion` | mensajeria (chat) | conversacion_id, mensaje_id, empresa_id, usuario_id, autor_id; uno por mencionado, nunca al autor ni con el texto |
| `chat.miembro_agregado` | mensajeria (chat) | conversacion_id, empresa_id, orden_id, usuario_id, agregado_por |
| `canal_mensajeria.creado` / `canal_mensajeria.actualizado` / `canal_mensajeria.activado` / `canal_mensajeria.desactivado` / `canal_mensajeria.calidad` / `canal_mensajeria.probado` | mensajeria | canal_id, empresa_id; tipo, canal, tipo_conexion (api, qr) (creado); campos[] (actualizado); evento, pausado_hasta (calidad); estado, origen (envio o ausente) (probado) |
| `canal_mensajeria.desconectado` / `canal_mensajeria.reconectado` | mensajeria (QR) | canal_id, empresa_id; motivo (partner_removed, account_offboarded o la razón de Meta) (desconectado); origen (qr, meta) (reconectado) |
| `canal_mensajeria.prestado` / `canal_mensajeria.prestamo_revocado` | mensajeria (préstamo) | canal_id, empresa_id (la dueña), prestatario_id; usos[], cola_id (o null), nuevo (prestado); motivo, conversaciones_cerradas, avisos_cancelados (revocado) |
| `meta.conexion_cambiada` / `meta.conexion_probada` | mensajeria (conexión con Meta) | modo; anterior, campos[] (cambiada); estado (lista, error, sin_servicio_central) (probada); nunca secretos |
| `conversacion.creada` | mensajeria (bandeja) | conversacion_id, empresa_id (o null), canal_id, cola_id (o null), cliente_id (o null), asociacion (automatica o null), origen (cliente, agente, telefono) |
| `conversacion.empresa_asignada` | mensajeria (préstamo) | conversacion_id, empresa_id, canal_id, motivo (iniciada_por_empresa, cliente_de_empresa, sin_coincidencia, varias_coincidencias, pasada), anterior_empresa_id (o null), actor_id (pasada) |
| `plantilla_whatsapp.creada` / `plantilla_whatsapp.actualizada` / `plantilla_whatsapp.enviada` / `plantilla_whatsapp.estado_cambiado` / `plantilla_whatsapp.categoria_cambiada` / `plantilla_whatsapp.calidad_cambiada` / `plantilla_whatsapp.borrada` | mensajeria (plantillas de WhatsApp) | plantilla_id, empresa_id, waba_id; nombre, idioma, categoria, origen (iris, sugerida, meta, migrada, canal) (creada); campos[] (actualizada); meta_id, tipo (creacion, edicion), estado (enviada); de, a, motivo (o null), origen, pausada_hasta (o null) (estado); de, a, origen, pendiente, en_uso (categoría); de, a, origen (calidad); nombre, idioma, origen (iris, meta, sistema), estaba_en_uso, codigos[], nombre_libre_en (o null), motivo y usos[] solo con sistema (borrada) |
| `plantilla_whatsapp.sincronizada` / `plantillas_whatsapp.ajustes_cambiados` | mensajeria (plantillas de WhatsApp) | waba_id, empresa_id, origen (manual, nocturna, webhook), total, nuevas, cambiadas, borradas (sincronizada); empresa_id, campos[], marketing_permitido (ajustes) |
| `conversacion.mensaje_recibido` / `conversacion.mensaje_enviado` / `conversacion.mensaje_fallido` | mensajeria (bandeja) | conversacion_id, mensaje_id, empresa_id; canal_id, tipo, cliente_id, asignado_a, cola_id (recibido); autor_id (o null), tipo, plantilla, respuesta_rapida_id (o null), origen (telefono o null) (enviado); motivo (numero_invalido, rechazado_por_canal, fuera_de_ventana, agotado, token_invalido, sin_permiso_meta, cliente_sin_confirmar), autor_id (fallido); nunca el texto |
| `conversacion.nota_agregada` / `conversacion.mencion` | mensajeria (bandeja) | conversacion_id, mensaje_id, empresa_id, autor_id; menciones[] (nota); usuario_id (mención, uno por mencionado) |
| `conversacion.asignada` / `conversacion.transferida` | mensajeria (bandeja) | conversacion_id, empresa_id, actor_id; usuario_id (o null), anterior_id (o null), origen (tomar, asignar, responder, pasar, iniciar), disponible y nota_id (pasar) (asignada); de_cola_id, a_cola_id, asignado_a, origen (asistente, categoria; ausente = una persona) (transferida) |
| `conversacion.etiquetada` / `conversacion.resuelta` / `conversacion.reabierta` | mensajeria (bandeja) | conversacion_id, empresa_id, actor_id (o null); etiquetas[], anteriores[] (etiquetada); asignado_a (resuelta, reabierta); motivo (prestamo_revocado, con actor_id null) (resuelta); origen (cliente, agente) (reabierta) |
| `conversacion.cliente_asociado` / `conversacion.cliente_confirmado` | mensajeria (bandeja) | conversacion_id, empresa_id, cliente_id (o null al desasociar), actor_id; servicio_id, asociacion, origen (referencia, contacto; ausente = a mano) (asociado); contacto_argos_id (o null) (confirmado) |
| `conversacion.vinculo_creado` | mensajeria (bandeja) | conversacion_id, empresa_id, tipo (ticket, orden, prospecto, boleta, promesa), entidad_id, referencia, actor_id |
| `conversacion.sla_vencido` | mensajeria (bandeja, mantenimiento) | conversacion_id, empresa_id, cola_id, asignado_a, vence_en, espera_desde |
| `contacto_iris.creado` / `contacto_iris.unido` / `contacto_iris.consentimiento` | mensajeria (bandeja) | contacto_iris_id, empresa_id; tipo, conversacion_id (o null), origen (agente, telefono) (creado); cliente_id, conversaciones[] (unido); estado (acepta, baja), origen (consentimiento) |
| `cola.creada` / `cola.actualizada` / `etiqueta.creada` / `etiqueta.actualizada` / `respuesta_rapida.creada` / `respuesta_rapida.actualizada` | mensajeria (bandeja) | cola_id / etiqueta_id / respuesta_id, empresa_id; nombre o atajo (creada); campos[], activa (actualizada) |
| `boleta.registrada` / `boleta.registrada_en_argos` / `boleta.fallida` | mensajeria (bandeja) | boleta_id, conversacion_id, empresa_id; cliente_id, servicio_id (registrada); comprobante_id (en Argos); detalle (fallida) |
| `boleta.aprobada` / `boleta.rechazada` | mensajeria (bandeja) | boleta_id, conversacion_id, empresa_id, comprobante_id, pago_id (o null), actor_id (o null si decidió caja en Argos), origen (iris, argos), aviso_cliente (enviado, sin_aviso); causa (rechazada). Una vez por boleta |
| `boleta.corregida` / `boleta.confirmacion_agotada` | mensajeria (bandeja) | boleta_id, conversacion_id, empresa_id, comprobante_id; de, a, pago_id, causa, actor_id, origen (corregida: Argos dice otro estado, sin segundo aviso); intentos, detalle (agotada: no se pudo confirmar el webhook con Argos) |
| `promesa.solicitada` | mensajeria (bandeja) | promesa_id, conversacion_id, empresa_id, cliente_id, servicio_id, estado (activa, rechazada, pendiente_aprobacion), promesa_argos_id |
| `gestion_cobro.registrada` | mensajeria (bandeja, gestiones de cobro) | gestion_id, conversacion_id, empresa_id, cliente_id, servicio_id (o null), tipo, resultado, origen (persona, asistente), pendiente_id; nunca texto, montos ni teléfonos |
| `horario.creado` / `horario.actualizado` | mensajeria (bandeja) | horario_id, empresa_id; nombre (creado); campos[], activo (actualizado) |
| `conversacion.fuera_de_horario` / `conversacion.guardia_avisada` | mensajeria (bandeja) | conversacion_id, empresa_id; mensaje_id, hasta (fuera de horario); usuario_id, motivo (etiqueta, falla_masiva) (guardia) |
| `manual.creado` / `manual.actualizado` / `manual.enviado` / `manual.enlace_revocado` | mensajeria (bandeja) | manual_id, empresa_id; visibilidad (creado); campos[], activo (actualizado); conversacion_id, mensaje_id, modo (enlace, texto) (enviado); enlace_id, conversacion_id, actor_id (revocado) |
| `conversacion.iniciada` | mensajeria (bandeja) | conversacion_id, empresa_id, canal_id, cola_id, mensaje_id, plantilla, autor_id, nueva, cliente_id (o null), contacto_iris_id (o null) |
| `conversacion.aviso_sin_atender` | mensajeria (bandeja, mantenimiento) | conversacion_id, empresa_id, cola_id, usuarios[], bot_contesta_en (o null) |
| `conversacion.bot_atendio` / `conversacion.menu_elegido` | mensajeria (bandeja) | conversacion_id, empresa_id; cola_id, motivo (sin_confirmacion, sin_disponibles, fuera_de_horario), mensaje_id (o null), menu (bot_atendio); opcion, texto, de_cola_id, a_cola_id, etiquetas[] (menu_elegido) |
| `agente.disponibilidad_cambiada` | mensajeria (bandeja) | usuario_id, empresa_id, estado, origen (manual, cierre_sesion, inactividad, horario), anterior, desde |
| `conversaciones.ajustes_cambiados` | mensajeria (bandeja) | empresa_id, campos[], minutos_inactividad |
| `asistente.ajustes_cambiados` | mensajeria (asistente de IA) | empresa_id, campos[]; nunca la clave |
| `asistente.sugerencia_creada` | mensajeria (asistente de IA) | sugerencia_id, conversacion_id, empresa_id, origen (automatico, boton, otra), proveedor, modelo, tokens_entrada, tokens_salida, estado, error (o null); nunca el texto |
| `asistente.sugerencia_usada` / `asistente.sugerencia_calificada` | mensajeria (asistente de IA) | sugerencia_id, conversacion_id, empresa_id; editada (true, false o null) (usada); util (calificada) |
| `asistente.tope_alcanzado` | mensajeria (asistente de IA) | empresa_id, porcentaje (80, 100), mes, tokens, tope |
| `conversacion.categorizada` | mensajeria (asistente de IA, fase 3) | conversacion_id, empresa_id, categoria_id (o null), anterior_id (o null), origen (asistente, agente), confianza (o null), actor_id (o null) |
| `categoria.creada` / `categoria.actualizada` | mensajeria (asistente de IA, fase 3) | categoria_id, empresa_id; nombre (creada); campos[], activa (actualizada) |
| `asistente.mensaje_probado` | mensajeria (asistente de IA, fase 3) | empresa_id, haria (responde_solo, sugiere, pasa_a_persona), categoria_id (o null), tokens_entrada, tokens_salida; nunca el texto |
| `asistente.accion_ejecutada` | mensajeria (asistente de IA, fase 3b) | conversacion_id, empresa_id, accion, resultado (ok, rechazada, fallo), entidad (o null), entidad_id (o null), turno; nunca argumentos, texto ni montos |
| `conversacion.identidad_verificada` | mensajeria (asistente de IA, fase 3b) | conversacion_id, empresa_id, cliente_id, resultado (coincide, no_coincide, no_disponible, error), origen (asistente); nunca el dato |
| `reconexion.solicitada` | mensajeria (bandeja, fase 3b) | reconexion_id, conversacion_id, empresa_id, cliente_id, servicio_id, comprobante_id, motivo (fuera_de_horario, sin_personal), estado (aceptada, rechazada), promesa_argos_id (o null) |
| `reconexion.actualizada` | mensajeria (bandeja, webhooks promesa.*) | reconexion_id, conversacion_id, empresa_id, comprobante_id, promesa_argos_id, estado (activa, cumplida, incumplida, cancelada), motivo, fin (comprobante_rechazado, vencida_sin_revisar, otro, o null), hasta (o null), entrante_id |
| `reconexion.ajustes_cambiados` | mensajeria (bandeja) | empresa_id (o null), campos[], sin_personal |
| `reconexion.cliente_avisado` | mensajeria (bandeja) | reconexion_id, conversacion_id, empresa_id, estado (cumplida, incumplida, cancelada), fin (o null), aviso_cliente (enviado, sin_aviso), mensaje_id (o null); nunca el texto |
| `asistente.paso_a_persona` | mensajeria (asistente de IA, fase 3b) | conversacion_id, empresa_id, motivo (cliente_lo_pidio, enojo, sin_respuesta, herramienta_fallo, max_turnos, persona_presente, tope), cola_id (o null), turnos |
| `conversacion.resumen_guardado` | mensajeria (asistente de IA, §16.4) | conversacion_id, empresa_id, resumen_id, usuario_id, proveedor, modelo, hasta_mensaje_id, tokens_entrada, tokens_salida; nunca el texto |
| `conversacion.asistente_pausado` / `conversacion.asistente_reanudado` | mensajeria (asistente de IA, §17.11) | conversacion_id, empresa_id, actor_id, asignado_a (o null); motivo (manual, caso_delicado, respuesta_incorrecta, cliente_lo_pidio), atendia (pausado); de (pausado, persona_presente, espera_persona), motivo_paso (o null), pausa_motivo_anterior (o null) (reanudado) |
| `llamada.entrante` / `llamada.saliente_iniciada` | mensajeria (voz; contrato `voz.md`; E1: entrante; E1b: saliente) | llamada_id, conversacion_id, empresa_id, canal_id; cola_id (o null), cliente_id (o null), origen (whatsapp, centralita) (entrante); usuario_id, devolucion_id (o null) (saliente) |
| `llamada.contestada` / `llamada.terminada` | mensajeria (voz) | llamada_id, conversacion_id, empresa_id, atendida_por (persona, asistente), usuario_id (o null); espera_seg (contestada); direccion, duracion_seg, motivo_fin, tramos (terminada) |
| `llamada.perdida` / `llamada.rechazada` / `llamada.fallida` | mensajeria (voz) | llamada_id, conversacion_id, empresa_id; direccion, cola_id, sono_seg, devolucion_id (o null), usuarios[] (perdida); motivo, usuario_id (o null) (rechazada); motivo, etapa (fallida) |
| `llamada.anotada` | mensajeria (voz) | llamada_id, conversacion_id, empresa_id, usuario_id, resultado (o null); nunca la nota |
| `llamada.permiso_solicitado` / `llamada.permiso_cambiado` | mensajeria (voz, E1b) | conversacion_id (o null en cambiado, si llega por webhook), empresa_id, canal_id; usuario_id (o null), mensaje_id (solicitado); de, a, vence_en (cambiado); nunca el número |
| `llamada.devolucion_pedida` / `llamada.devolucion_cerrada` | mensajeria (voz) | devolucion_id, conversacion_id, empresa_id, llamada_id; pedida_por (sistema, asistente, cliente) (pedida); estado (hecha, descartada, vencida), usuario_id (o null), llamada_devuelta_id (o null) (cerrada) |
| `llamada.grabacion_disponible` / `llamada.grabacion_borrada` / `llamada.transcripcion_lista` | mensajeria (voz) | llamada_id, empresa_id; grabacion_id, origen, duracion_seg, vence_en (disponible); grabacion_id, motivo (retencion, manual), usuario_id (o null) (borrada); transcripcion_id, origen, segmentos (lista); nunca audio ni texto |
| `llamada.tomada` / `llamada.transferida` | mensajeria (voz, E3) | llamada_id, conversacion_id, empresa_id, usuario_id; de (asistente o usuario_id), a_usuario_id (o null), a_cola_id (o null) |
| `llamada.escucha_iniciada` / `llamada.escucha_terminada` | mensajeria (voz, E3) | llamada_id, conversacion_id, empresa_id, usuario_id (quien escucha); duracion_seg (terminada) |
| `canal_mensajeria.llamadas_cambiadas` | mensajeria (voz) | canal_id, empresa_id, campos[], llamadas_activas |
| `centralita.telefono_cambiado` / `centralita.telefono_probado` | mensajeria (voz, E4c) | centralita_id, empresa_id; campos[], activo (cambiado); estado (probado); nunca un secreto |
| `extension.telefono_cambiado` / `extension.registro_probado` | mensajeria (voz, E4c) | centralita_id, empresa_id, extension_id, usuario_id; tipo, credencial (guardada, borrada, sin_cambio) (cambiado); estado, origen (probado); nunca la contraseña |
| `grupo_centralita.creado` / `grupo_centralita.actualizado` | mensajeria (voz, E4c) | grupo_id, centralita_id, empresa_id; nombre, numero (creado); campos[], activo (actualizado) |
| `llamada.directa_registrada` | mensajeria (voz, E4c) | llamada_id, empresa_id, orden_id, cliente_id (o null), servicio_id (o null), usuario_id, origen (celular); nunca el número |
| `voz.ajustes_cambiados` / `voz.tope_alcanzado` | mensajeria (voz) | empresa_id; campos[] (ajustes; nunca claves); tipo (asistente, salientes), porcentaje (80, 100), mes, minutos, tope (tope) |
| `voz.mal_uso_detectado` / `voz.numero_bloqueado` / `voz.numero_desbloqueado` | mensajeria (voz) | empresa_id; llamada_id, conversacion_id, tipo, regla, accion (mal uso); bloqueo_id, hasta, motivo, usuario_id (o null) (bloqueo); nunca el texto ni el número |
| `voz.rafaga_levantada` | mensajeria (voz) | empresa_id, canal_id, conversacion_id (o null), origen (manual, llamada), usuario_id (o null), llamada_id (o null); nunca el número |
| `voz.motor_enlazado` / `voz.motor_token_rotado` / `voz.motor_estado_cambiado` | mensajeria (voz; servidor de voz, empresa_id null) | motor_id; version, protocolo, usuario_id (enlazado); origen (rotado); de, a, en_curso, version, compatible, llamadas_cortadas (estado); nunca el código ni el token |
| `voz.motor_activado` / `voz.motor_desactivado` / `voz.motor_empresas_cambiadas` / `voz.motor_modelos_cambiados` | mensajeria (Servidor de IA, empresa_id null) | motor_id, usuario_id; version (activado); motivo (desactivado); empresas (o null), simultaneas_por_empresa (empresas); modelos (o null) (modelos) |
| `servidor_ia.umbral_superado` / `servidor_ia.umbral_normalizado` / `servidor_ia.ajustes_cambiados` | mensajeria (Servidor de IA, empresa_id null) | motor_id; tipo (vram, disco, temperatura, saturacion), valor, umbral (umbral); campos[] (ajustes) |
| `centralita.creada` / `centralita.actualizada` / `centralita.estado_cambiado` / `extension.asignada` / `extension.quitada` | mensajeria (voz, E4) | centralita_id, empresa_id; tipo, modo (creada); campos[], activa (actualizada); de, a (estado); extension_id, usuario_id (extensión); nunca el secreto |
| `llamada.vinculada` | mensajeria (voz, E4a) | llamada_id, empresa_id, ticket_id, cliente_id (o null), usuario_id, origen (centralita) |
| `llamada.cliente_asociado` | mensajeria (voz, E4a) | llamada_id, empresa_id, cliente_id, servicio_id (o null), usuario_id, origen (centralita) |
| `notificaciones.avisos_creados` | notificaciones (avisos del panel) | tipo (asistente_tope, venta_revisada…), clave, empresa_id (o null), usuarios[], correo (enviar, omitido) |
| `notificaciones.correo_enviado` / `notificaciones.correo_fallido` | notificaciones (correo del sistema) | correo_id, usuario_id, tipo, clave, motivo (o null) |
| `correo_sistema.ajustes_cambiados` | notificaciones (correo del sistema) | empresa_id (o null = global), campos[], activo; nunca la contraseña |
| `correo_sistema.probado` | notificaciones (correo del sistema) | empresa_id (o null), estado (ok, error), causa (o null), usuario_id; nunca el host, el destino ni credenciales |
| `datos_legales.cambiados` | legal (núcleo) | campos[], completo, faltan[]; nunca los valores |
| `dispositivo.registrado` / `dispositivo.quitado` | notificaciones | dispositivo_id, usuario_id, empresa_id, token_fin; plataforma, app_version, nuevo (registrado); motivo (usuario, sesion_cerrada, token_invalido, otro_usuario, sin_acceso) (quitado); nunca el token |
| `notificaciones.preferencias_cambiadas` | notificaciones | usuario_id, campos[], version |

El detalle de cada payload vive en `.claude/integration/contratos/eventos.md`.

## Convenciones

- Código, tablas, rutas y mensajes en español. Go: nombres exportados en `PascalCase` y paquetes en minúscula; TS: `camelCase`/`PascalCase`; tablas y columnas en `snake_case`.
- Commits Conventional Commits con alcance = módulo: `feat(soporte): escalar ticket por SLA`.
- Una PR por tarea del brief, con su número en el título.
- Variables de entorno documentadas en `.env.example`; nunca secretos en el repo.
- Fechas en UTC en la base; zona `America/Guatemala` solo al mostrar.
- Multiempresa igual que Argos: todo lo operativo lleva `empresa_id` (la de Argos) y se filtra por la empresa activa.
- No agregues dependencias nuevas sin anotar el motivo en la PR.
