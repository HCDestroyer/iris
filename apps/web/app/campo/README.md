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
- **Navegación:** isla de menú Hoy · Por recibir · «+» · Agenda · Yo; cada vista y cada paso con
  su URL (`@iris/sync/campo` `vista.ts`). El estado es un punto con todo enviado y crece como
  píldora encima de la isla solo cuando hace falta (por enviar, rechazos, sesión vencida, empresa
  no válida).
- **Diseño «combinada» (nov 2026, `campo.css` y `@iris/sync/campo` `inicio.ts`):**
    - **Encabezado que no queda fijo:** fecha con el punto de conexión, «Hola, <nombre>», quién
      está (y la empresa), buscar (entre las órdenes del teléfono, sin señal) y la cuenta; en Hoy,
      Mañana y Semana los chips con su contador (solo con datos leídos). Al bajar se va con el
      contenido; al subir vuelve como barra compacta (`desplazar`).
    - **Hoy:** la siguiente orden en grande (la primera en curso o, si no hay, la primera por
      hacer) con estado, código y hora, cliente, tipo, dirección, anillo de avance «2/4» y
      «Continuar trabajo» / «Cómo llegar» / «Llamar»; mosaicos «Ruta de hoy» (órdenes que quedan:
      la ruta del teléfono no trae km) y «Por recibir» (traslados, en tono de aviso; «—» si no se
      pudo leer); lista «Después» con la hora en un bloque, cliente, tipo e insignia.
    - **Isla de menú** (reemplaza la barra inferior): Hoy · Por recibir (contador) · «+» · Agenda
      · Yo, zonas de 54 px (44 encogida al bajar). «Por enviar» ya no es pestaña: se abre desde la
      **píldora de estado** encima de la isla (`avisoIsla`, la barra discreta de la fase 4: sin
      señal con algo por enviar, rechazos, sesión vencida, empresa no válida) y desde «Yo». En los
      pasos de la orden no hay isla: va la barra «Anterior» / «Siguiente».
    - **«+»:** hoja de acciones rápidas. «Agregar formulario» a una orden con el trabajo empezado
      (`orden.agregar_formulario`, sin señal también; solo con `ordenes.registrar` en `GET /yo`,
      si no, no aparece), con «Deshacer» (`orden.quitar_formulario` del mismo id). «Agendar tarea
      propia» va deshabilitada con «Próximamente»: no tiene API.
    - **Agenda «Mañana» y «Semana»** (`?vista=agenda[&dia=]`, `?vista=semana`): tira de 7 días con
      el día elegido en teal y las citas en bloques (en curso en tinta, sin formulario en aviso).
      El técnico no tiene `agenda.ver`: se lee `GET /campo/paquete?fecha=<hoy+6>` con señal y queda
      en memoria; hoy usa las órdenes del teléfono. Sin leer no hay contadores; sin señal lo dice
      con «Reintentar» y se relee sola al volver la señal.
    - **Pasos:** chips de pasos (listo en marca suave, el actual en tinta), barra por pasos,
      tarjetas de 28 px de radio, − / + junto a «Cantidad» en materiales (`pasoCantidad`) y los
      avisos que entran desde arriba con su acción («Deshacer»; un rechazo nuevo del servidor,
      con «Revisar»).
    - **Tema:** los dos de C-Hoy llevados a los tokens (`color-mix` de `--hx-*`), un solo teal
      (claro `#0f766e`, oscuro `#2dd4bf`), claro y oscuro según el teléfono.
- **La orden paso a paso:** resumen → «Empezar trabajo» → llegada · checklist · materiales y
  equipos · fotos → «Firma del cliente y cierre» (paso final y único; solo con lo obligatorio listo).
- **Por recibir y Mi bodega:** traslados de Argos (`GET /campo/inventario/traslados?estado=enviado`)
  con recepción firmada por el técnico (`traslado.recibir` en la cola) y la bodega del técnico
  (`GET /campo/inventario/mi-bodega`), solo con señal y en memoria mientras la app está abierta.
- **Mis órdenes del día** en orden de ruta publicada (`ruta.ordenIds` del paquete, cuando I2 la
  publique), con la «Llegada estimada» de cada parada (y si queda fuera de la ventana) y el aviso
  «Tu ruta cambió» si está `desactualizada`; sin ruta, por la hora de la cita de la agenda
  (`cita.inicio`) con la ventana de llegada acordada («Llegada acordada 07:30–09:00»); estado con icono + texto + color,
  y la barra de conexión con «N pendientes por enviar» → vista *Por enviar* (cola, archivos,
  rechazos con «Descartar» / «Rehacer», cierres esperando archivos por `faltan_adjuntos`).
- **Detalle:** cliente, llamar, «Cómo llegar» (app de mapas con las coordenadas), servicio,
  comodato, notas; «En camino» / «Llegué» con GPS si se permite; en sitio: checklist (sí/no,
  texto, número, medición, foto), fotos (≤ 1600 px, ~300 KB, hora y GPS del EXIF), materiales
  de la bodega de la orden, equipos (instalar/retirar según el tipo, escáner por campo o a mano), firma
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
    de tramo solo lo mantiene una orden **en curso** (`en_camino` o `en_sitio`; ni pendientes, ni
    programadas, ni atrasadas, ni pausadas): termina cuando no queda ninguna y, como tope, 2 h
    después del fin del tramo («Turno terminado: pasaron 2 h de tu horario…»). «En camino» /
    «Llegué» lo encienden igual fuera de tramo (antes del primer tramo se ata a él; después, tope
    de 2 h).
  - **Sin turno en la agenda** (ese día no trabaja, sin ficha o paquete de otro día): empieza con
    «Iniciar turno» o solo con «En camino» / «Llegué», y termina cuando no queda ninguna orden en
    curso (la cerró, falló, se la cancelaron o reasignaron) o a mano.
  - Siempre: «Terminar turno» a mano y, como red de seguridad, fin a medianoche de Guatemala (con la
    app abierta se baja el paquete del día nuevo una vez por día; tras un error se reintenta al
    volver la señal o la app, con espera exponencial, y nunca solo sin sesión o sin empresa). El
    turno local va por usuario y empresa (`iris-campo-turno-<usuario>-<empresa>`) y cambiar de
    empresa lo termina. Cerrar sesión lo termina antes de esperar a la
    red y cierra la cola (no entran más puntos).
  - **Indicador** bajo la barra de conexión (icono + texto + color): «Ubicación activa», «Sin
    permiso», «Sin señal GPS», «Buscando GPS…», «Sin GPS», «Fuera de turno» y «Pendientes de
    enviar: n»; con agenda, además «En turno hasta 17:00» (o «En turno fuera de horario») o
    «Fuera de turno · empieza 08:00». El panel muestra los tramos del día, último envío, la espera del servidor, «Reintentar» tras una falla o
    una pausa por 4xx, y «Vuelve a entrar» si la sesión terminó.
- **«En camino»:** `orden.iniciar_camino` (con GPS si hay permiso) a la cola sin conexión, e inicia
  el turno como se dice arriba. El aviso al cliente por Argos o Telegram todavía no existe en la API (pedido en
  `.claude/integration/i4.md`).
- **Formularios** (`_componentes/checklist.tsx`): cada formulario de la orden en su tarjeta con
  nombre, versión, origen y sus ítems (`checklist[]` plano agrupado por `formularioId`; tipo
  `seleccion` con botones o lista). «Agregar formulario» suma una plantilla del catálogo del
  teléfono (`Paquete.plantillas` / `Cambios.plantillas`) con `orden.agregar_formulario` (también sin
  señal; el id de la operación es el del formulario y viaja la versión que se vio). «Quitar» solo en
  los que agregó el técnico sin respuestas; el del tipo nunca. Se ofrece con el trabajo empezado
  (en camino o en sitio; pausada, no). Un `cierre_incompleto` marca en cada
  formulario el ítem que falta («Falta para cerrar») y «Por enviar» muestra el `message` de la API.
- **Pausa** (`_componentes/pausa.tsx`): «Pausar» en camino o en sitio con motivo de catálogo (nota
  obligatoria con «Otro»), aviso con motivo, nota y tiempo pausado, y «Reanudar». Pausada no ofrece
  cerrar, fallar, llegar ni salir, y en pausa no se registra trabajo: los pasos quedan de solo
  lectura con el aviso «Orden en pausa» y «Reanudar» (solo notas, como evidencia). Una
  orden pausada no cuenta como «en curso»: no mantiene encendida la ubicación (reanudar la vuelve a
  encender como «En camino»).
- **Escáner de serie y MAC** (`_componentes/escaner-equipo.tsx`, `lib/campo/escaner.ts`): un botón
  por campo («Escanear número de serie», «Escanear MAC») abre `VisorEscaner` de `@iris/ui` con su
  objetivo. Solo cuentan los códigos dentro del recuadro (`boundingBox` de `BarcodeDetector` pasado
  a la vista con `object-fit: cover`; con ZXing cuentan todos), se juntan ~800 ms (`SesionEscaneo`
  de `@iris/sync/campo`, con `navigator.vibrate` si existe) y la hoja muestra lo detectado con lo
  que parece; un solo candidato se confirma, un QR con serie y MAC llena los dos, la serie o MAC ya
  registrada en la orden se avisa antes de usarla. Linterna con `torch` si la pista lo admite.
- **Avisos del servidor** (`version_anterior`, `pausa_ajustada`) en el detalle de la orden hasta
  «Entendido»: ningún ajuste pasa en silencio.
- **Contraseña temporal:** con `usuario.cambioContrasenaRequerido` en `GET /yo` o un 403
  `cambio_contrasena_requerido` (paquete, lote, ubicaciones), va a
  `/cambiar-contrasena?siguiente=/campo` (pantalla del panel, I3, #27).
- **Service worker** (Serwist, `app/sw.ts`, alcance `/campo`): precachea el shell y los estáticos;
  nunca toca `/api`. Manifest «Iris Campo» en `/campo/manifest.webmanifest`.

Piezas: `_componentes/` (pantallas), `lib/campo/` (sesión, motor, textos, reglas, esquemas zod,
EXIF, compresión, GPS).

## Eventos

No emite ni consume eventos: habla con la API (`/campo/paquete`, `/campo/cambios`, `/sync/lote`,
`/adjuntos`, `/campo/posiciones`); los eventos los emite campo (I2) al aplicar cada operación. Las
posiciones no son acciones: no emiten eventos ni se auditan por fila.

## Pruebas

Vitest (`lib/campo/*.test.ts`; la lógica del diseño en `packages/sync/src/campo-inicio.test.ts`, con `ubicacion.test.ts`: turno, permiso, lectura por minuto y día, y
el turno de la agenda con el reloj falso: cruces de tramo, manual dentro de un tramo, fuera de
tramo, día sin turno y medianoche de Guatemala; `turno.test.ts`) y Playwright (`e2e/campo.spec.ts`, teléfono claro y oscuro, con
`context.setOffline`; `e2e/campo-sesiones.spec.ts`: dos técnicos en el mismo navegador, cerrar
sesión con y sin pendientes, abrir sin señal; `e2e/campo-ubicacion.spec.ts`: explicación antes del
permiso, un punto por minuto con el reloj de Playwright, sin señal y de vuelta, 429 con
`Retry-After`, permiso negado y «Ahora no», orden por cita y por ruta; `e2e/campo-diseno.spec.ts`: el diseño «combinada» (Hoy, encabezado y isla al desplazar, píldora,
«+» con y sin permiso, Mañana y Semana con y sin señal, pasos con chips y − / +; con
`IRIS_CAPTURAS=<carpeta>` deja las capturas); `e2e/campo-turno.spec.ts`:
turno de la agenda con el reloj de Playwright; `e2e/campo-formularios.spec.ts`: formularios y pausa
sin señal, rechazos y cierre incompleto por formulario; `e2e/campo-escaner.spec.ts`: escáner por campo
con cámara y `BarcodeDetector` simulados, en escritorio y teléfono, claro y oscuro (con
`IRIS_CAPTURAS=<carpeta>` deja capturas); `e2e/capturas-campo.spec.ts`; `e2e/campo-api-real.spec.ts` contra la API Go
con `IRIS_E2E_API_REAL=1 IRIS_E2E_TECNICO=<correo>:<contraseña>`).
