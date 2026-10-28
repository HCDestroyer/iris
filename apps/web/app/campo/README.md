# /campo · app de campo (PWA)

App del técnico, pensada para el teléfono (390 px, una mano, claro y oscuro). Funciona sin señal:
todo lo que hace se guarda primero en el teléfono con `@iris/sync` (IndexedDB) y se envía solo.

## Qué hace

- **Sesión:** la del panel (`GET /yo`; sin señal, la última guardada). Un 401 lleva a
  `/entrar?siguiente=/campo` (que carga /campo completa para que el service worker la controle).
  Empresa activa: si hay varias, se elige; hay un motor e IndexedDB por usuario y empresa
  (`iris-campo-<usuario>-<empresa>`), así la cola de una empresa nunca sale con la cabecera de
  otra. Al abrir, al cambiar de empresa o de día y al volver la señal se baja el paquete.
- **Un teléfono, varios técnicos** (reglas de seguridad):
  - Antes de cada envío o bajada (lote, alta y tus de adjuntos, paquete, cambios) el motor
    comprueba con `GET /yo` que la sesión del navegador es del dueño de la cola (y de su empresa).
    Si entró otra persona, el motor se detiene y se desmonta sin enviar nada («Entró otra persona
    en este navegador»); su cola queda intacta en su base y sale cuando vuelve a entrar. Un 409
    `dispositivo_ajeno` se trata igual (sesión), nunca se regenera el dispositivo.
  - **Abrir sin señal:** solo con la sesión guardada de un `GET /yo` correcto de ese usuario de
    hace menos de 12 h (`NEXT_PUBLIC_IRIS_CAMPO_HORAS_SIN_SENAL`) y sin cierre de sesión; si no,
    «Conéctate para entrar». A los 7 días la sesión guardada se borra.
  - **Cerrar sesión:** sin pendientes borra la base del teléfono (órdenes, clientes, fotos y
    firmas), la sesión guardada y la empresa elegida, y cierra la sesión en la API
    (`X-Iris-Canal: campo`). Con pendientes avisa («Tienes N cambios sin enviar») y deja elegir
    «Enviar y salir» (con señal) o «Salir y conservarlos»: quedan solo la cola y los archivos
    pendientes, sin datos del servidor, y la app no los muestra sin entrar. Sin señal no navega a
    /entrar (no está en caché): muestra «Cerraste sesión en este teléfono» y cierra la sesión en la
    API al volver la señal.
- **Mis órdenes del día** en orden de ruta publicada (`ruta.ordenIds` del paquete, cuando I2 la
  publique), con la «Llegada estimada» de cada parada (y si queda fuera de la ventana) y el aviso
  «Tu ruta cambió» si está `desactualizada`; sin ruta, por la hora de la cita de la agenda
  (`cita.inicio`) con la ventana de llegada acordada («Llegada acordada 07:30–09:00»); estado con icono + texto + color,
  y la barra de conexión con «N pendientes por enviar» → vista *Por enviar* (cola, archivos,
  rechazos con «Descartar» / «Rehacer», cierres esperando archivos por `faltan_adjuntos`).
- **Detalle:** cliente, llamar, «Cómo llegar» (app de mapas con las coordenadas), servicio,
  comodato, notas; «En camino» / «Llegué» con GPS si se permite; en sitio: checklist (sí/no,
  texto, número, medición, foto), fotos (≤ 1600 px, ~300 KB, hora y GPS del EXIF), materiales
  de la bodega de la orden, equipos (instalar/retirar según el tipo, escáner o a mano), firma
  (SVG a la API y PNG como adjunto), cerrar o «No se pudo» con motivo. Lo que no vale para el
  estado o el tipo no se muestra (`lib/campo/reglas.ts`).
- **Ubicación en vivo** (`lib/campo/ubicacion.ts`, `_componentes/ubicacion.tsx`): en turno y con
  la app abierta, un punto por minuto (`getCurrentPosition` sin caché) a la `ColaPosiciones` de
  `@iris/sync` (misma base del usuario y la empresa; sin señal se acumula y sale en lotes al
  volver, respetando 6 lotes/min y `Retry-After`).
  - **Explicación** «¿Compartir tu ubicación?» la primera vez por usuario, aunque el navegador ya
    tenga el permiso (se recuerda al aceptarla; con «Ahora no» se vuelve a mostrar en la próxima
    visita). El navegador pregunta solo tras «Permitir ubicación»; fotos y cierre nunca preguntan.
  - **Turno de la agenda** (`Paquete.turno`, guardado por el motor por usuario y empresa con el
    día del paquete, sobrevive sin señal; también el de `/campo/cambios` si algún día lo trae;
    `lib/campo/turno.ts`): al empezar un tramo se enciende solo (con la explicación aceptada, el
    permiso dado y sin «Ahora no» en la visita), con el aviso «Turno iniciado: se envía tu
    ubicación cada minuto»; al terminar el tramo se apaga solo y lo avisa. Un «Terminar turno» a
    mano dentro de un tramo se respeta hasta el tramo siguiente (también al reabrir la app). Fuera
    de tramo, «En camino» / «Llegué» lo encienden igual (hay trabajo real) y termina con la última
    orden o al final del tramo, lo que ocurra después.
  - **Sin turno en la agenda** (ese día no trabaja, sin ficha o paquete de otro día): empieza con
    «Iniciar turno» o solo con «En camino» / «Llegué», y termina cuando no quedan órdenes abiertas
    del día (cerró o falló la última, o se la cancelaron o reasignaron) o a mano.
  - Siempre: «Terminar turno» a mano y, como red de seguridad, fin a medianoche de Guatemala (con la
    app abierta se baja el paquete del día nuevo). Cerrar sesión lo termina antes de esperar a la
    red y cierra la cola (no entran más puntos).
  - **Indicador** bajo la barra de conexión (icono + texto + color): «Ubicación activa», «Sin
    permiso», «Sin señal GPS», «Buscando GPS…», «Sin GPS», «Fuera de turno» y «Pendientes de
    enviar: n»; con agenda, además «En turno hasta 17:00» (o «En turno fuera de horario») o
    «Fuera de turno · empieza 08:00». El panel muestra los tramos del día, último envío, la espera del servidor, «Reintentar» tras una falla o
    una pausa por 4xx, y «Vuelve a entrar» si la sesión terminó.
- **«En camino»:** `orden.iniciar_camino` (con GPS si hay permiso) a la cola sin conexión, e inicia
  el turno como se dice arriba. El aviso al cliente por Argos o Telegram todavía no existe en la API (pedido en
  `.claude/integration/i4.md`).
- **Service worker** (Serwist, `app/sw.ts`, alcance `/campo`): precachea el shell y los estáticos;
  nunca toca `/api`. Manifest «Iris Campo» en `/campo/manifest.webmanifest`.

Piezas: `_componentes/` (pantallas), `lib/campo/` (sesión, motor, textos, reglas, esquemas zod,
EXIF, compresión, GPS).

## Eventos

No emite ni consume eventos: habla con la API (`/campo/paquete`, `/campo/cambios`, `/sync/lote`,
`/adjuntos`, `/campo/posiciones`); los eventos los emite campo (I2) al aplicar cada operación. Las
posiciones no son acciones: no emiten eventos ni se auditan por fila.

## Pruebas

Vitest (`lib/campo/*.test.ts`, con `ubicacion.test.ts`: turno, permiso, lectura por minuto y día, y
el turno de la agenda con el reloj falso: cruces de tramo, manual dentro de un tramo, fuera de
tramo, día sin turno y medianoche de Guatemala; `turno.test.ts`) y Playwright (`e2e/campo.spec.ts`, teléfono claro y oscuro, con
`context.setOffline`; `e2e/campo-sesiones.spec.ts`: dos técnicos en el mismo navegador, cerrar
sesión con y sin pendientes, abrir sin señal; `e2e/campo-ubicacion.spec.ts`: explicación antes del
permiso, un punto por minuto con el reloj de Playwright, sin señal y de vuelta, 429 con
`Retry-After`, permiso negado y «Ahora no», orden por cita y por ruta; `e2e/campo-turno.spec.ts`:
turno de la agenda con el reloj de Playwright; `e2e/capturas-campo.spec.ts`; `e2e/campo-api-real.spec.ts` contra la API Go
con `IRIS_E2E_API_REAL=1 IRIS_E2E_TECNICO=<correo>:<contraseña>`).
