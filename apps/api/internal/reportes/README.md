# reportes

Reportes de solo lectura sobre soporte y campo (Fase 5). Cada reporte es un caso de uso
(`auditoria.Caso`) con el permiso `reportes.ver` (roles `admin`, `supervisor` y `lectura`):
transacción de lectura, empresa activa (`X-Iris-Empresa`; sin conector, solo lo guardado con
`empresa_id` null; el actor sistema ve todas) y alcance por zona o cuadrilla del rol (una fila sin
zona ni cuadrilla no la ve un alcance limitado). No deja fila de auditoría al terminar bien (sí al
fallar), como las demás lecturas. **No emite ni consume eventos.**

Nunca devuelve totales calculados sobre datos que no se pudieron leer: si una consulta falla, el
reporte entero falla. Un porcentaje sin base es `null` (nunca 0 % ni 100 % inventados).

## Endpoints (`GET /api/v1/reportes/...`, tag `reportes`)

Filtros comunes: `desde` y `hasta` (obligatorios, `YYYY-MM-DD`; días de **America/Guatemala**: el
período va de `desde` 00:00 al fin de `hasta`, y se compara en UTC), `zonaId`, `cuadrillaId`,
`tecnicoId` (usuario: técnico de la orden o responsable asignado del ticket). Rango máximo
366 días (422 `rango_excedido`); `hasta` antes de `desde` o una fecha inválida, 422 `validacion`.

| Ruta | Base | Extra |
| --- | --- | --- |
| `sla` | Tickets resueltos en el período (`resuelto_en`), sin fusionados | `categoriaId`, `prioridad` (varias). Total, `porPrioridad` y `porCategoria`: `% primera respuesta ≤ vence_respuesta` y `% resuelto ≤ vence_resolucion` (los relojes sin SLA no cuentan). |
| `resolucion` | Igual | Promedio y mediana en minutos **de reloj** (creación → resolución). |
| `tecnicos` | Órdenes `cerrada` o `fallida` con `finalizada_en` en el período | Por técnico: exitosas, parciales, fallidas, % éxito, duración media en sitio (llegada → cierre, menos las pausas tomadas en sitio), tiempo pausado (total y medio). |
| `primera-visita` | Instalaciones y visitas cerradas `exitosa` | `dias` (7–90, 30). `resuelta` si el mismo servicio no tuvo otra orden (no cancelada) ni ticket (no fusionado) en los `dias` siguientes; `enObservacion` si la ventana aún no termina y `sinServicio` si no hay servicio: ninguna de las dos cuenta en el %. Total, por tipo y por técnico. |
| `satisfaccion` | Encuestas creadas en el período (`soporte.encuestas`) | `categoriaId`, `entidad`. Enviadas, respondidas, pendientes, vencidas, % respondidas, promedio, alertas (1 o 2) y distribución 1–5; total y por entidad. Zona, cuadrilla y técnico son los copiados al crear la encuesta. |
| `reincidencias` | Tickets resueltos en el período | `dias` (1–90, 30), `categoriaId`, `prioridad`, `cursor`/`limit`. Reincide si el mismo servicio (o, sin servicio, el mismo cliente) abre otro ticket de la misma empresa en los `dias` siguientes. Totales (`resueltos`, `evaluables`, `reincidentes`, `porcentaje`) y la lista paginada (`data`, `nextCursor`; más recientes primero). |

**CSV:** `?formato=csv` o `Accept: text/csv` → `text/csv; charset=utf-8`, UTF-8 con BOM, encabezados
en español, fechas en hora de Guatemala, `attachment; filename="reporte-<ruta>-<desde>_<hasta>.csv"`.
Los textos que empiezan con `= + - @` llevan un apóstrofo (inyección de fórmulas). El CSV de
reincidencias trae todas las filas (sin cursor) hasta 10 000; con más, 422 `exportacion_excedida`.
Los errores siempre son `application/problem+json`.

## Rendimiento

Migración `soporte_reportes_indices`: `soporte.tickets (resuelto_en)` parcial, `(servicio_id,
created_at)`, `(cliente_id, created_at)`; `campo.ordenes (finalizada_en)` y `(servicio_id,
created_at)`. `carga_test.go` siembra 10 000 tickets abiertos, 60 000 resueltos y 20 000 órdenes en
un año, mide cada reporte del año (límite 3 s; localmente decenas de ms) y verifica con `EXPLAIN`
sobre la consulta real de sqlc que las de un mes usan esos índices.
