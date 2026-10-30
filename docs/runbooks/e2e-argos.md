# Prueba de punta a punta Iris ↔ ArgosISP

Prueba el contrato `.claude/integration/contratos/argosisp.md` (§1.3, §2 y §3) contra una versión
publicada de Argos, sin falsos. **No corre en el CI de las PR**: se lanza a mano antes de cambiar el
conector, al salir una versión de Argos o al cambiar el contrato.

## Qué necesita

- Postgres 16 con un usuario que pueda crear bases (`E2E_PG`, por defecto
  `postgres://iris:iris@localhost:55432`). Se crean y se **borran** `iris_e2e` y `argos_e2e`.
- `redis-server`, `node` 22 y `pnpm` 10, `go`, `psql`, `python3`, `git`, `curl`, `openssl`.
- Red hacia GitHub (la release y el código de Argos) y hacia el registro de npm.
- Puertos libres: Argos 3520, Iris 3521, SMTP 3528, Redis 3529 (`E2E_PUERTO_*`).

No hace falta Docker. La release de Argos (`argosisp-<v>.tar.gz`) es un despliegue con imágenes de
GHCR. El script descarga la release y verifica su `sha256` y su `VERSION`, y después corre el
**código de la misma etiqueta** con `node` (API, `eventos:relay`, `iris:worker` y `notify:worker`)
sobre su propia base y su propio Redis. El clon de Argos es solo de lectura: el script se niega a
correr si tiene cambios.

## Uso

```sh
infra/e2e/argos.sh                  # todo: prepara, corre los pasos y apaga
E2E_DEJAR=1 infra/e2e/argos.sh      # deja Argos e Iris arriba para mirar (luego: argos.sh parar)
infra/e2e/argos.sh limpiar          # apaga, borra las bases y infra/e2e/.trabajo (clon de Argos incluido)
```

Variables útiles:

| Variable | Para qué |
| --- | --- |
| `ARGOS_VERSION` | Versión de Argos (`1.4.0`, la que cumple el contrato 0.3.1). |
| `E2E_ARGOS_DIR` | Un clon de Argos que ya tengas en la etiqueta (evita clonar). |
| `E2E_IRIS_API_DIR` | Otro árbol de Iris para compilar, p. ej. una rama sin mergear. |
| `E2E_TRABAJO` | Carpeta de trabajo (logs, binarios, adjuntos, correos); por defecto `infra/e2e/.trabajo`. |
| `E2E_PASOS` | Solo algunos pasos (`datos,conexion,lecturas,...`). Los pasos dependen de los anteriores. |

El reporte queda en `<trabajo>/reporte.txt` y los logs de cada proceso en `<trabajo>/logs/`. El
script sale distinto de 0 si algún paso falla.

## Pasos

| Paso | Qué prueba |
| --- | --- |
| datos | Catálogo mínimo en Argos por su API: zona, router con red IPv4, plan solo v4, bodega con dos ONU con serie y un cliente. |
| 1 conexión | Enlace generado en Argos, canje desde Iris (§1.3 → Horus §1.4), conector activo en los dos lados. |
| 2 lecturas | Planes y catálogos a través de Iris; búsqueda y ficha del cliente. |
| 3 webhooks | `servicio.creado`, `cliente.actualizado` con `empresas[]`, firma `v1=` y deduplicación por `id` (repite un envío con el secreto del receptor), marca fuera de ventana, retiro (`argos.servicio_retirado`) y retiro con equipo en comodato (orden de retiro). |
| 4 alta | Prospecto → factibilidad → cotización → contrato firmado → ganar: `POST /clientes` y `POST /servicios` (`codigo_externo=iris:prospecto:<id>`, §3.1–§3.2). La orden de instalación se cierra con checklist, foto (tus), ONU y firma: `activar-instalacion` y comodato en Argos, y su bienvenida (sumidero SMTP). |
| 5 caída | Argos se apaga antes de ganar: el pendiente falla y se reintenta; al volver, el alta se completa sin duplicar en Argos. |
| 6 SSO | «Ver en Iris» desde Argos. |
| 7 eliminación | `DELETE /servicios/:id` en Argos (token admin temporal) → `servicio.eliminado` → lápida y `serviciosEliminados` en Iris. |

## Resultado con Argos 1.4.0 (30 oct)

Con Iris de `main` (contrato 0.3.1, #60): **17 de 17 pasos OK**.

- Conexión: el canje informa `empresas[]` y `alcance_global`.
- Planes y catálogos; búsqueda y ficha del cliente.
- Webhooks: firma, deduplicación, ventana, `cliente.actualizado` con `campos[]` al cambiar el teléfono, y retiro con comodato.
- Alta desde el prospecto e instalación cerrada: `activar-instalacion` sin `version`, comodato y bienvenida.
- Activación repetida: 409 `servicio_no_pendiente`.
- Argos caído y de vuelta, sin duplicar.
- «Ver en Iris» (SSO).
- `servicio.eliminado`: lápida y `serviciosEliminados`.

Los routers de la siembra no tienen coordenadas, así que la factibilidad sigue `indeterminado` (esperado).

## Resultado con Argos 1.3.0 (29 oct)

Con Iris de `agente/i1-argos-correcciones` (que arregla el canje, las coordenadas, la empresa de
routers y zonas y la carga de catálogos al activar):

- **OK:** conexión, planes y catálogos, webhooks (firma, deduplicación, ventana, retiro y orden de
  retiro con comodato), alta desde el prospecto, comodato al cerrar la instalación, bienvenida de
  Argos (activando a mano) y Argos caído y de vuelta sin duplicar.
- **Falla, Iris:** búsqueda y ficha del cliente. En Argos el cliente no tiene empresa (la tienen sus
  servicios) e Iris filtra por `cliente.empresa_id`. Tampoco hay SSO (`/integraciones/argosisp/sso/tickets`).
- **Falla, Argos o contrato:** `activar-instalacion` exige `version` (422) y el contrato no la
  menciona. Además choca con §3.1 (los reintentos mandan los mismos bytes). Cambiar el teléfono o la
  dirección no emite `cliente.actualizado`. El canje no informa empresas. Los routers no tienen
  coordenadas, así que la factibilidad por distancia queda `indeterminado`.

El detalle, con evidencia, está en `.claude/integration/i1.md`.
