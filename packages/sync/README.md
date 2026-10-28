# @iris/sync

Motor de sincronización sin conexión de la app de campo (`contratos/sync.md`). Lo usan la PWA
`/campo` y, en la fase 2, la app de Expo. El núcleo no toca el DOM: persistencia, red y reloj
se inyectan.

## Qué hace

- **Cola local** de las 11 operaciones del §2 con id UUID v7 del teléfono (`uuid7.ts`, sin
  dependencia), `versionBase` (última versión del servidor que veía) y `ocurridoEn` (hora del
  teléfono **sin corregir**: el lote lleva `enviadoEn` con el mismo reloj y la API corrige cada
  hora con `servidorEn − enviadoEn`, I2). `datos` = cuerpo camelCase de la acción del panel sin
  `version` (`DatosPorTipo`, derivado del OpenAPI). Cada acción se guarda en el almacén **antes**
  de aplicarse o enviarse; si la escritura falla, `registrar` lanza y no queda nada.
- **Vista optimista:** la orden cambia al instante (base del servidor + cola aplicada con
  `aplicarOperacion`) y se reconcilia con el `orden { version, estado }` de cada resultado.
- **Lotes** a `POST /sync/lote` en el orden de la cola (100 operaciones y 3,5 MiB por lote; la
  API acepta 200 y 4 MiB), con `dispositivoId`, `plataforma`, `enviadoEn` y el `cursor` propio
  (el de la respuesta se ignora). Un 409 `dispositivo_ajeno` genera otro dispositivo y repite:
  - `aplicada` / `duplicada` → sale de la cola y avanza la base;
  - `rechazada` → queda visible (`estado.rechazos`) hasta que el técnico la **descarte** o la
    **rehaga** (`rehacer`: id nuevo en el mismo lugar de la cola, datos corregidos opcionales);
    retiene solo las siguientes de **su** orden. Lo rechazado después del primer rechazo de la
    misma orden en un lote (`bloqueada_por_rechazo`) vuelve a la cola, detrás, con id nuevo;
  - sin respuesta → sigue pendiente y no se repite en la misma pasada.
- **Conflictos del §3:**
  - `orden_reasignada` / `orden_cancelada` no retienen la cola: la API juzga cada operación (lo
    ya aplicado se conserva; `nota.agregar` y `adjunto.registrar` pasan como evidencia). La
    orden queda marcada (`reasignada`, `cancelada`) y el teléfono ya no acepta otras acciones;
    `descartarOrden` limpia lo que queda.
  - Checklist: gana la marca con `ocurridoEn` más reciente y el historial guarda ambas
    (`fusionarMarca`).
  - `faltan_adjuntos` (`errors[].field` = `adjuntos.firma`, `adjuntos.foto:<código del ítem>`):
    si lo que falta se está subiendo desde el teléfono, el cierre pasa a
    `esperando_adjuntos` y vuelve solo a la cola (id nuevo) cuando termina la subida; si no,
    queda como rechazo. Además `orden.cerrar` no sale mientras haya adjuntos `obligatorio` sin
    subir, y lo que cita un adjunto (`adjunto.registrar`, la marca de un ítem foto, la firma) no
    sale hasta que `POST /adjuntos` creó el id.
  - Material mayor que la existencia: `registrar` devuelve el aviso
    `material_excede_existencia`, la existencia local queda en negativo y la operación se envía.
- **Paquete del día** (`cargarPaquete(fecha)`) y **cambios por cursor** (en cada
  `sincronizar`, página por página). Las órdenes salen en el orden de la ruta publicada
  (`ruta.ordenIds`) o, sin ruta, por cita (`programadaInicio`) y número. Existencias por bodega:
  `material.usar` descuenta de la bodega de su orden (o la única del técnico). El desfase se mide
  con `servidorEn` (paquete y lote) o la cabecera `Date` (cambios).
- **Caducidad:** 7 días sin contacto con la API borran lo que vino del servidor (paquete,
  existencias, ruta, cursor) y `estado.caducado` pide bajar el paquete. La cola y los adjuntos
  sin subir **nunca** se borran.
- **Adjuntos (I1):** `agregarAdjunto` guarda los bytes y encola `adjunto.registrar` en una sola
  escritura. Alta `POST /adjuntos` (`entidad: "orden"`, `claveChecklist` = código del ítem foto,
  `lat`/`lon`, `tomadoEn` corregido con el desfase; idempotente por id, retoma desde `recibido`)
  y tus 1.0 en partes (256 KiB) con `PATCH /adjuntos/{id}/subida`, guardando el offset tras cada
  parte; tras un corte o un reinicio se reanuda con `HEAD`; un 409 `offset_distinto` sigue desde
  el `Upload-Offset` del servidor; con todo enviado, un `PATCH` vacío en `tamano` confirma (y
  repite el de un 503). 429 y 5xx son temporales; los demás 4xx dejan el adjunto `rechazado` y
  visible: `descartarAdjunto` o `rehacerAdjunto` (id nuevo, mismos bytes, reescribe la cola). Al
  terminar se borran los bytes del teléfono.
- **Bucle** (`iniciar` / `detener`): sincroniza cada 60 s; tras una falla de red o 5xx espera
  `min(5 min, 2 s·2ⁿ)` con jitter (mitad fija, mitad al azar). Una acción nueva no acorta esa
  espera; `establecerConexion(true)` o `reintentarAhora()` sí. Un 401/403 deja
  `conexion: "sin_sesion"` (y un problema de empresa, `"sin_empresa"`) y no acelera.
- **Orden del día** (`compararOrdenesDelDia`): primero la ruta publicada (`ruta.ordenIds` del
  paquete, en su orden), luego la hora de la cita de la agenda (`cita.inicio`, o
  `programadaInicio` sin cita; las sin hora al final) y el número.
- **Ubicación en vivo** (`ColaPosiciones`, aparte del motor): cola de puntos GPS en la misma base
  (tabla `posiciones` de Dexie v3, clave = hora del punto en ms) y envío en lote a
  `POST /campo/posiciones` (`crearTransportePosicionesApi`, `X-Iris-Canal: campo`, mismo
  `dispositivoId` y la misma `verificarIdentidad` que el motor). `agregar` normaliza el punto
  (6 decimales; sin rumbo `NaN`, velocidad > 150 m/s ni precisión > 100 km), lo guarda primero e
  ignora una hora repetida o de más de 24 h. Tope de 1500 puntos (≈ 25 h a uno por minuto): se
  descartan los más viejos. Lotes de hasta 500, en orden, a lo sumo 6 por minuto (el límite de la
  API); un 429 se espera según `Retry-After` (segundos o fecha HTTP; 30 s si falta) sin descartar
  nada, y ni `reintentarAhora` ni un punto nuevo lo acortan. Lo que la API contesta con 200
  (aceptado, duplicado o descartado por `vieja`/`futura`/`frecuencia`) sale de la cola; un 422
  borra ese lote y lo deja visible (`ultimoError`, `descartadas.invalida`); red y 5xx esperan con
  espera exponencial; 401/403 (`requiere_sesion_campo`, sesión de otra persona) detiene el bucle
  con la cola intacta (`detenida`: hay que volver a entrar); otro 4xx que no se arregla solo (400,
  403 de empresa, 404, 413…) deja el envío en pausa (`pausada`) hasta `reintentarAhora`.
  `detener()` cierra la cola (`cerrada`): `agregar` devuelve `false` hasta `iniciar()`. Estado para React con `suscribir` / `obtenerEstado` (`EstadoPosiciones`).

## Uso (React)

```ts
import { MotorSync, crearSubidorApi, crearTransporteApi } from "@iris/sync";
import { AlmacenDexie } from "@iris/sync/dexie";

// Cliente openapi-fetch de @iris/contratos con cookies + X-CSRF-Token y X-Iris-Empresa (PWA)
// o Bearer (Expo); el transporte agrega X-Iris-Canal: campo.
const cliente = crearClienteIris({ leerEmpresa: () => empresaId });
const motor = await MotorSync.abrir({
    almacen: new AlmacenDexie({ nombre: `iris-campo-${usuarioId}-${empresaId ?? 0}` }),
    transporte: crearTransporteApi(cliente),
    subidor: crearSubidorApi(cliente),
});
motor.iniciar();
window.addEventListener("online", () => motor.establecerConexion(true));
window.addEventListener("offline", () => motor.establecerConexion(false));

// En un componente:
const estado = useSyncExternalStore(motor.suscribir, motor.obtenerEstado);
// estado.conexion, estado.pendientes, estado.rechazos, estado.subidas, estado.ordenes…
await motor.registrar("orden.llegar", orden.id, { lat, lon, precision });
```

## Piezas

| Archivo | Qué es |
| --- | --- |
| `motor.ts` | `MotorSync`: cola, lotes, conflictos, paquete, cambios, subidas, bucle y estado |
| `posiciones.ts` | `ColaPosiciones`: ubicación en vivo (cola, tope, límite por minuto, `Retry-After`) |
| `aplicar.ts` | Aplicación optimista pura y regla del checklist |
| `contrato.ts` | Alias de `@iris/contratos` (`Schema<"OrdenCampo">`…) y `DatosPorTipo` derivado de las entradas de cada acción |
| `almacen.ts`, `almacen-memoria.ts`, `almacen-dexie.ts` | Persistencia: interfaz, memoria (pruebas) e IndexedDB (`@iris/sync/dexie`) |
| `transporte.ts`, `http.ts` | Interfaces `Transporte` y `Subidor` y su implementación con el cliente `openapi-fetch` (lote, paquete, cambios, alta y tus 1.0) |
| `reloj.ts`, `uuid7.ts` | Reloj inyectable, espera exponencial con jitter y UUID v7 monótono |
| `pruebas/` | `@iris/sync/pruebas`: reloj manual, transporte y subidor falsos, `TransportePosicionesFalso` (6 lotes/min, 429, duplicadas), fábricas |

Nada del cable se declara a mano: si cambia el OpenAPI, `tsc` marca lo que hay que adaptar. El
modelo local (`modelo.ts`: operación, orden, adjunto y meta guardados) sí es propio del teléfono.

## Eventos

No emite ni consume eventos del catálogo: habla con la API por HTTP. Hacia la interfaz expone
`suscribir(oyente)` con la instantánea `EstadoSync`.

## Dependencias

- `dexie` (solo `@iris/sync/dexie`): IndexedDB con transacciones y versiones de esquema,
  según el brief de I4.
- `fake-indexeddb` (desarrollo): IndexedDB completo en Node para probar el adaptador de Dexie.
- `@iris/contratos` (del monorepo): tipos y cliente del OpenAPI.

## Pruebas

`pnpm --filter @iris/sync test` (Vitest, sin red ni tiempo real): UUID v7, persistencia antes
de enviar, lotes con duplicados y sin respuesta, rechazo que retiene solo su orden, cada regla
del §3, desfase de reloj, espera exponencial con reloj manual, paquete, cambios por cursor,
caducidad, subida con corte y reanudación, cerrar esperando adjuntos, Dexie sobre
`fake-indexeddb`; `motor-contrato.test.ts` (forma de I2/I1: `bloqueada_por_rechazo`,
`dispositivo_ajeno`, tope de bytes, claves de `faltan_adjuntos`, alta con `claveChecklist`,
`offset_distinto`, adjunto rechazado), `posiciones.test.ts` (persistir antes de enviar, sin
señal y reinicio, 6 lotes por minuto sin llegar al 429, 429 con `Retry-After`, tope, 24 h, 422,
bucle con espera y sesión, aislamiento por usuario y empresa en IndexedDB y migración a v3) y
`http.test.ts` (transporte real con respuestas tipadas del OpenAPI).
