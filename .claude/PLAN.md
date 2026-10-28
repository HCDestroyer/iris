# Plan de Iris

Iris es el sistema de operación de campo y atención del ISP. Lo que no es de Iris (clientes, servicios, facturación e inventario) se usa desde ArgosISP por contrato.

## Frentes (agentes)

| Agente | Dueño de | Brief |
| --- | --- | --- |
| I1 · núcleo | `apps/api/internal/{nucleo,auditoria,integracion}`, `cmd/`, migraciones base, OpenAPI | `agents/i1-nucleo.md` |
| I2 · operaciones | `apps/api/internal/{soporte,campo,agenda,comercial}` | `agents/i2-operaciones.md` |
| I3 · panel | `apps/web` (panel y portal de tickets), `packages/ui` | `agents/i3-panel.md` |
| I4 · campo | `apps/web/app/campo`, `packages/sync`, `apps/movil` (fase 2) | `agents/i4-campo.md` |
| I5 · infra | `infra/`, `.github/`, `Makefile`, OSRM, backups, observabilidad | `agents/i5-infra.md` |

Los cambios en ArgosISP y Horus Flow **no** los hace ningún agente de Iris. Se piden en §5 del contrato correspondiente, y los implementa el equipo de ese repositorio.

## Fase 0 · Cimientos (semana 1)

1. **I5:** monorepo (pnpm + Go workspace), `docker-compose` con Postgres 16, Redis 7 y OSRM (Guatemala), `Makefile` (`dev`, `test`, `lint`, `typecheck`, `openapi`, `sqlc`, `migrar`) y CI con Postgres real. Release-please.
2. **I1:** esqueleto de la API:
    - `chi`, configuración, logging estructurado, `/salud` y errores RFC 9457;
    - contexto (`Actor`, `Canal`, `RequestID`) y `EnTx`;
    - auditoría (`acciones`, `cambios`), outbox y relay, idempotencia y paginación por cursor;
    - generación de OpenAPI con `huma` sobre `chi`.
3. **I1:** usuarios, sesiones (cookie `HttpOnly` y MFA TOTP opcional), roles, permisos con alcance y tokens de API. Seed de roles base.
4. **I3:** esqueleto de Next.js con la identidad Horus (tokens, logo Iris, aura, vidrio), inicio de sesión, layout con menú por permisos, modo claro y oscuro, y cliente de `packages/contratos`.

## Fase 1 · Tickets y SLA (semana 2)

1. **I2:** tickets.
    - Modelo, categorías y prioridades.
    - SLA por categoría y prioridad, con horario laboral y feriados de Guatemala.
    - Acciones: `crear`, `asignar`, `responder`, `escalar`, `poner_en_espera`, `resolver`, `reabrir` y `cerrar`.
    - Notas internas y públicas, con adjuntos.
    - Cron de SLA, que emite `sla_en_riesgo` y `sla_vencido`.
    - Fusión de tickets duplicados.
2. **I1:** conector Argos (lectura), según `contratos/argosisp.md` §2.
    - Enlace de conexión.
    - Búsqueda de cliente y servicio.
    - Instantánea de cliente y servicio, refrescada por webhook.
    - Resolución por IP.
3. **I3:** bandeja de tickets (filtros, vistas guardadas, SLA visible), detalle con historial del cliente (desde Argos) y alta rápida.
4. **I2 + I1:** entrada de tickets.
    - Portal de soporte (`/soporte`, con el cliente identificado por Argos).
    - Correo entrante (IMAP).
    - Webhook de Horus: un hallazgo o la caída de un router abre o agrupa tickets.

## Fase 2 · Órdenes de trabajo y app de campo (semanas 3 y 4)

1. **I2:** órdenes de trabajo.
    - Tipos: instalación, visita, retiro, cambio de equipo y mantenimiento.
    - Plantillas de checklist por tipo.
    - Estados: `pendiente` → `programada` → `en_camino` → `en_sitio` → `cerrada` / `fallida` / `cancelada`.
    - Evidencias: fotos, firma, GPS con hora, y materiales y equipos (serie/MAC).
2. **I4:** PWA de campo (`/campo`).
    - Mis órdenes del día y detalle.
    - Navegación al sitio.
    - Checklist, fotos (compresión y subida reanudable), firma del cliente y escaneo de serie/MAC por cámara.
    - Funciona sin conexión con IndexedDB y `POST /sync/lote` (`packages/sync`).
3. **I1:** acciones en Argos al cerrar una orden (§3 del contrato), con cola de pendientes y reintentos:
    - instalación → activar el servicio y asignar el comodato;
    - retiro → devolver el comodato y retirar el servicio;
    - materiales → movimiento de consumo desde la bodega del técnico.
4. **I3:** tablero de órdenes y detalle con evidencias, más reapertura y reprogramación.

## Fase 3 · Agenda y rutas (semana 5)

1. **I2:** agenda.
    - Técnicos, cuadrillas, zonas, turnos y ausencias.
    - Capacidad diaria por tipo de orden.
    - Ventanas de cita acordadas con el cliente.
2. **I2:** rutas.
    - Propuesta de ruta del día con OSRM: orden óptimo con ventanas y duración estimada por tipo.
    - Publicación de la ruta.
    - Reasignación en caliente.
3. **I4:** ubicación en vivo del técnico (cada 60 s con la app abierta, sin auditoría por fila) y estado «en camino», con aviso al cliente por Argos/Telegram.
4. **I3:** despacho, con calendario por técnico, mapa con órdenes y técnicos, arrastrar y soltar para asignar, y ruta propuesta frente a la real.

## Fase 4 · Seguimiento comercial (semana 6)

1. **I2:** comercial.
    - Prospectos y su origen.
    - Factibilidad: coordenadas → cobertura. Mientras no exista la fase 6, la distancia al router más cercano de la instantánea de Argos; con la fase 6, viabilidad de OZmap.
    - Cotización con planes de Argos.
    - Contrato con firma digital.
    - `ganar`: crea el cliente y el servicio en Argos y abre la orden de instalación.
2. **I3:** embudo comercial, mapa de prospectos y cobertura, y cotización en PDF.

## Fase 5 · Endurecimiento y app nativa (semanas 7 y 8)

- Reportes: SLA cumplido, tiempo medio de resolución, órdenes por técnico, primera visita resuelta y reincidencias.
- Encuesta de satisfacción al cerrar.
- Pruebas de carga: 50 técnicos sincronizando y 10 000 tickets abiertos.
- Respaldo y restauración ensayados en CI.
- Expo (`apps/movil`) con `packages/sync` y `packages/contratos`: cámara, escaneo y GPS en segundo plano.

## Fase 6 · Planta externa con OZmap (al final del plan)

Decisión del PM (28 oct): la planta externa de fibra vive en OZmap, no en Argos (reemplaza el pedido A5). Se arranca solo después de las fases anteriores.

- **Puerto «planta externa»** en `internal/integracion`, con un adaptador OZmap intercambiable (por si algún día se cambia a NetBox u otro). Sin el conector, Iris sigue funcionando con la factibilidad por distancia.
- Conector con la API oficial de OZmap y token creado en su panel, guardado cifrado como los demás conectores. El sandbox se pide a integracoes@ozmap.com.br.
- **Al vender:** viabilidad por radio o por coordenada, cajas con puertos libres, splitters de la caja y viabilidad por puerto (OLT, slot, PON).
- **Al cerrar una instalación:** registrar el cliente en su puerto con la potencia medida (por la cola de pendientes) y mandar la referencia a Argos (pedido A9).
- Contrato propio en `contratos/ozmap.md` (solo del lado de Iris: OZmap es un sistema externo).

## Pedidos a otros sistemas

Viven en §5 de cada contrato. Resumen inicial:

- **ArgosISP:**
    - conector Iris (token y webhooks, como Horus);
    - `servicios.activar` desde una orden;
    - consumo de inventario por técnico;
    - comodato;
    - alta de cliente y servicio desde un prospecto;
    - referencia de planta en el servicio (A9; la cobertura A5 pasó a OZmap);
    - autologin.
- **Horus Flow:** webhook de router caído o hallazgo de seguridad hacia Iris, y autologin.
