# @iris/sync

Motor de sincronización sin conexión de la app de campo (`contratos/sync.md`). Lo usan la PWA
`/campo` y, en la fase 2, la app de Expo. El núcleo no toca el DOM: persistencia, red y reloj
se inyectan.

## Qué hace

- **Cola local** de las 11 operaciones del §2 con id UUID v7 del teléfono (`uuid7.ts`, sin
  dependencia), `versionBase` (última versión del servidor que veía) y `ocurridoEn` (hora del
  teléfono + desfase medido con la cabecera `Date` del último contacto). Cada acción se guarda en
  el almacén **antes** de aplicarse o enviarse; si la escritura falla, `registrar` lanza y no
  queda nada.
- **Vista optimista:** la orden cambia al instante (base del servidor + cola aplicada con
  `aplicarOperacion`) y se reconcilia con el `orden { version, estado }` de cada resultado.
- **Lotes** a `POST /sync/lote` en el orden de la cola (100 por lote):
  - `aplicada` / `duplicada` → sale de la cola y avanza la base;
  - `rechazada` → queda visible (`estado.rechazos`) hasta que el técnico la **descarte** o la
    **rehaga** (`rehacer`: id nuevo en el mismo lugar de la cola, datos corregidos opcionales);
    retiene solo las siguientes de **su** orden. Lo rechazado después del primer rechazo de la
    misma orden en un lote vuelve a la cola, detrás, con id nuevo;
  - sin respuesta → sigue pendiente y no se repite en la misma pasada.
- **Conflictos del §3:**
  - `orden_reasignada` / `orden_cancelada` no retienen la cola: la API juzga cada operación (lo
    ya aplicado se conserva; `nota.agregar` y `adjunto.registrar` pasan como evidencia). La
    orden queda marcada (`reasignada`, `cancelada`) y el teléfono ya no acepta otras acciones;
    `descartarOrden` limpia lo que queda.
  - Checklist: gana la marca con `ocurridoEn` más reciente y el historial guarda ambas
    (`fusionarMarca`).
  - `faltan_adjuntos`: si lo que falta se está subiendo desde el teléfono, el cierre pasa a
    `esperando_adjuntos` y vuelve solo a la cola (id nuevo) cuando termina la subida; si no,
    queda como rechazo. Además `orden.cerrar` no sale mientras haya adjuntos `obligatorio` sin
    subir, y `adjunto.registrar` no sale hasta que `POST /adjuntos` creó el id.
  - Material mayor que la existencia: `registrar` devuelve el aviso
    `material_excede_existencia`, la existencia local queda en negativo y la operación se envía.
- **Paquete del día** (`cargarPaquete(fecha)`) y **cambios por cursor** (en cada
  `sincronizar`, página por página). Las órdenes salen en el orden de la ruta publicada.
- **Caducidad:** 7 días sin contacto con la API borran lo que vino del servidor (paquete,
  existencias, ruta, cursor) y `estado.caducado` pide bajar el paquete. La cola y los adjuntos
  sin subir **nunca** se borran.
- **Adjuntos:** `agregarAdjunto` guarda los bytes y encola `adjunto.registrar` en una sola
  escritura. La subida es tus 1.0 en partes (256 KiB): `POST /adjuntos` → `PATCH` con
  `Upload-Offset`, guardando el offset tras cada parte; tras un corte o un reinicio se reanuda
  con `HEAD`. Al terminar se borran los bytes del teléfono.
- **Bucle** (`iniciar` / `detener`): sincroniza cada 60 s; tras una falla de red o 5xx espera
  `min(5 min, 2 s·2ⁿ)` con jitter (mitad fija, mitad al azar). Una acción nueva no acorta esa
  espera; `establecerConexion(true)` o `reintentarAhora()` sí. Un 401/403 deja
  `conexion: "sin_sesion"` y no acelera.

## Uso (React)

```ts
import { MotorSync, crearSubidorTus, crearTransporteHttp } from "@iris/sync";
import { AlmacenDexie } from "@iris/sync/dexie";

const motor = await MotorSync.abrir({
    almacen: new AlmacenDexie({ nombre: `iris-campo-${usuarioId}` }),
    transporte: crearTransporteHttp({ cabeceras: () => ({ "X-CSRF-Token": csrf() }) }),
    subidor: crearSubidorTus({ cabeceras: () => ({ "X-CSRF-Token": csrf() }) }),
});
motor.iniciar();
window.addEventListener("online", () => motor.establecerConexion(true));
window.addEventListener("offline", () => motor.establecerConexion(false));

// En un componente:
const estado = useSyncExternalStore(motor.suscribir, motor.obtenerEstado);
// estado.conexion, estado.pendientes, estado.rechazos, estado.subidas, estado.ordenes…
await motor.registrar("orden.llegar", orden.id, { posicion });
```

## Piezas

| Archivo | Qué es |
| --- | --- |
| `motor.ts` | `MotorSync`: cola, lotes, conflictos, paquete, cambios, subidas, bucle y estado |
| `aplicar.ts` | Aplicación optimista pura y regla del checklist |
| `contrato-sync.ts` | Tipos del cable, **provisionales** (ver abajo) |
| `almacen.ts`, `almacen-memoria.ts`, `almacen-dexie.ts` | Persistencia: interfaz, memoria (pruebas) e IndexedDB (`@iris/sync/dexie`) |
| `transporte.ts`, `http.ts` | Interfaces `Transporte` y `Subidor` y su implementación con `fetch` (tus 1.0) |
| `reloj.ts`, `uuid7.ts` | Reloj inyectable, espera exponencial con jitter y UUID v7 monótono |
| `pruebas/` | `@iris/sync/pruebas`: reloj manual, transporte y subidor falsos, fábricas |

`contrato-sync.ts` es la única excepción a «nada de interfaces a mano»: los endpoints de campo
y de adjuntos aún no están en `packages/contratos`. Cuando I2 e I1 los publiquen, se reemplaza
por alias a `Schema<"…">` y `crearTransporteHttp` por el cliente `openapi-fetch`.

## Eventos

No emite ni consume eventos del catálogo: habla con la API por HTTP. Hacia la interfaz expone
`suscribir(oyente)` con la instantánea `EstadoSync`.

## Dependencias

- `dexie` (solo `@iris/sync/dexie`): IndexedDB con transacciones y versiones de esquema,
  según el brief de I4.
- `fake-indexeddb` (desarrollo): IndexedDB completo en Node para probar el adaptador de Dexie.

## Pruebas

`pnpm --filter @iris/sync test` (Vitest, sin red ni tiempo real): UUID v7, persistencia antes
de enviar, lotes con duplicados y sin respuesta, rechazo que retiene solo su orden, cada regla
del §3, desfase de reloj, espera exponencial con reloj manual, paquete, cambios por cursor,
caducidad, subida con corte y reanudación, cerrar esperando adjuntos, adaptador `fetch`/tus y
Dexie sobre `fake-indexeddb`.
