# Pruebas de carga

Fase 5 del plan: **50 técnicos sincronizando y 10 000 tickets abiertos**, con el cron de SLA
corriendo. Todo vive en `infra/carga/` y corre con k6 en Docker (`grafana/k6:1.3.0`) contra un
compose propio (`iris-carga`), nunca contra la base ni los puertos de dev.

## Correrla

```sh
make carga-corta    # local: 2 min, 10 técnicos, 5 usuarios del panel (~4 min en total)
make carga          # completa: 15 min, 50 técnicos, 20 usuarios del panel (~18 min)
make carga-limpiar  # baja el compose de carga y borra su volumen y infra/carga/.salida
```

Necesita Docker (con compose) y Node 22 (la siembra por la API). `make carga` hace, en orden
(`infra/carga/carga.sh`):

1. Construye `iris/api:carga` e `iris/worker:carga` desde el árbol actual (`CARGA_CONSTRUIR=0` para
   reusar las que ya hay). Detrás de un proxy que intercepta TLS: `IRIS_BUILD_CA_EXTRA=/ruta/ca.pem`
   y, si el proxy solo escucha en localhost, `CARGA_BUILD_RED=host`.
2. Levanta `postgres` (con `pg_stat_statements`), `redis`, `migrar` y `api` con
   `infra/docker-compose.yml` + `infra/carga/compose.yml`, proyecto `iris-carga` (volumen propio).
   Solo publica la API en `127.0.0.1:3505` y Postgres en `127.0.0.1:3515`
   (`IRIS_CARGA_API_PUERTO`, `IRIS_CARGA_PG_PUERTO`). Sin MinIO, Mailpit, OSRM ni web.
3. Crea un administrador nuevo (`admin-<fecha>@carga.local`) y una contraseña al azar para esta
   corrida; solo vive en memoria del script. Nada de credenciales en el repo.
4. **Siembra** (repetible: completa lo que falte, no duplica):
    - `siembra/api.mjs`, por la API como el panel: empresa 9001, 5 zonas, 10 cuadrillas, 50 técnicos
      (usuario `tecnicoNNN@carga.local`, ficha con turnos de todo el día) con **8 órdenes programadas
      hoy** cada uno (~400; solo repone las que la corrida anterior dejó fallidas), 14 usuarios de
      soporte y 6 de despacho. Vuelve a fijar la contraseña de la corrida a todos.
    - `siembra/tickets.sql`, por SQL porque necesita historia: **10 000 tickets abiertos** (nuevo,
      abierto, en espera; mezcla de prioridades, las 9 categorías y los dos SLA) y **50 000 resueltos o
      cerrados** del último año, con su evento «creado» y sus mensajes. El 85 % de los abiertos ya
      venció y está alertado; el 15 % cruza el 80 % o el vencimiento de la respuesta durante la
      prueba, para que el cron de SLA tenga trabajo cada minuto. No pasa por los casos de uso: no
      deja auditoría ni outbox de la historia (el detalle y los listados se leen igual).
5. Arranca el **worker** (relay del outbox y cron de SLA cada minuto) justo antes de k6.
6. Corre `escenarios/carga.js` con k6 (ver abajo).
7. `verificar.sql`: duplicados en la base y cómo corrió el cron de SLA. Guarda las consultas más
   caras (`pg_stat_statements`) y detiene el compose (`CARGA_MANTENER=1` lo deja arriba para medir
   a mano: `psql postgres://iris:iris@127.0.0.1:3515/iris`).

Variables: `CARGA_DURACION`, `CARGA_TECNICOS`, `CARGA_PANEL`, `CARGA_ABIERTOS`, `CARGA_RESUELTOS`,
`CARGA_ORDENES_POR_TECNICO`, `CARGA_INTERVALO_SYNC` (30 s), `CARGA_INTERVALO_PAQUETE` (300 s),
`CARGA_REENVIO` (0.1), `K6_IMAGEN`. Disco: la base de carga ocupa ~250 MB; `make carga-limpiar` la
borra (la próxima corrida la vuelve a sembrar en ~20 s).

En CI: `.github/workflows/carga.yml`, a mano (`workflow_dispatch`, con duración, técnicos y
usuarios del panel) y cada lunes 08:17 UTC. No corre en las PR. Sube `resumen.txt`,
`resumen.json`, `verificacion.txt` y `pg_top.txt` como artefacto `carga-<run>` (30 días) y pega el
resumen en la página de la corrida.

## Escenarios

| Escenario | Usuarios | Qué hace |
| --- | --- | --- |
| `campo` | 50 técnicos (uno por VU), sesión de la app de campo (cookie + `X-CSRF-Token` + `X-Iris-Canal: campo`) | Al empezar, `GET /campo/paquete`. Cada ~30 s: `POST /campo/posiciones` (3 puntos) y `POST /sync/lote` con el paso siguiente de su orden en curso: iniciar camino → llegar + checklist → checklist + nota → pausar (30 %) o checklist → reanudar + checklist + fallar (cliente ausente), y pasa a la siguiente orden. Cada ~5 min alterna `GET /campo/cambios?cursor=` y el paquete completo. El 10 % de los lotes se **reenvía igual** enseguida (respuesta perdida): todo debe volver `duplicada`. |
| `panel` | 14 de soporte y 6 de despacho (sesión del panel) | Soporte: `GET /tickets` de abiertos con un filtro al azar (prioridad, categoría, asignados a mí, sin asignar, búsqueda `q`), a veces 2–3 páginas con el cursor; abre uno (`/tickets/{id}`, `/mensajes`, `/eventos`) y responde el 35 % de las veces con `Idempotency-Key` (el 10 % repite la misma clave: debe devolver el mismo mensaje). Despacho: tickets sin asignar, `GET /ordenes` del día (por estado, cuadrilla o zona), `/campo/posiciones/ultimas` y técnicos. Pausas de 2–6 s entre acciones. |
| cron de SLA | worker | `soporte.revisar_sla` cada minuto sobre los 10 000 abiertos. |

## Umbrales (fallan la prueba)

| Qué | Umbral |
| --- | --- |
| `POST /sync/lote` | p95 < 500 ms y errores < 1 % |
| Listados (`GET /tickets`, `/ordenes`, `/tecnicos`, `/campo/posiciones/ultimas`) | p95 < 500 ms y errores < 1 % (en conjunto; `/tickets` y `/ordenes` además cada uno p95 < 500 ms) |
| Operaciones del lote rechazadas | < 1 % |
| Reenvíos de lote que no vuelven todos `duplicada` | 0 |
| Respuesta repetida con la misma `Idempotency-Key` y otro mensaje | 0 |
| `checks` de k6 (status esperados) | > 99 % |
| **Duplicados en la base** (`verificar.sql`) | 0: notas con el mismo texto, marcas con el mismo `operacion_id`, transiciones de orden repetidas (en `campo.orden_eventos` y en el outbox) y mensajes de ticket con la misma clave |

k6 sale con 99 si cruza un umbral; `verificar.sql` termina con error si encuentra duplicados o si
no se aplicó ninguna operación (la prueba no llegó a la base). `make carga` falla en ambos casos.

## Leer los resultados

`infra/carga/.salida/` (ignorada por git):

- **`resumen.txt`**: por endpoint, peticiones, req/s, p50, p95, p99, máximo y % de error; después
  operaciones del lote enviadas y rechazadas, reenvíos, respuestas y cada umbral (`ok` / `FALLA`).
  `resumen.json` es el resumen completo de k6.
- **`verificacion.txt`**: la tabla de duplicados (todo en 0), el volumen de la corrida (operaciones
  aplicadas y rechazadas, posiciones, respuestas, eventos del outbox y los que quedaron sin
  publicar) y el cron de SLA: corridas, duración media y máxima, errores y alertas emitidas.
- **`pg_top.txt`**: las 30 consultas con más tiempo total en la corrida (`pg_stat_statements`):
  llamadas, ms medio y máximo, filas. Postgres además registra en su log toda consulta de más de
  500 ms (`IRIS_CARGA_PG_LENTAS_MS`).

Si un umbral falla:

1. Mira en `resumen.txt` qué endpoint y si es latencia (p95) o error. Los rechazos y reenvíos
   fallidos salen en el log de k6 con el `problem+json` de la API.
2. Busca su consulta en `pg_top.txt` y repítela con `CARGA_MANTENER=1`:
   `EXPLAIN (ANALYZE, BUFFERS) …` en `psql postgres://iris:iris@127.0.0.1:3515/iris`.
3. No se arregla en `infra/`: el endpoint, la consulta y el índice o cambio propuesto van al agente
   dueño (I1 núcleo, sync y outbox; I2 soporte y campo) por `.claude/integration/i5.md`.

## Línea base

Ver `.claude/integration/i5.md` («Pruebas de carga · resultados») para la última corrida medida y
los riesgos de escala detectados.
