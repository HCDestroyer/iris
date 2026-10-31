# apps/movil · Iris Campo (Expo)

App nativa del técnico (Android e iOS) con Expo SDK 58. Hace lo mismo que la PWA `/campo` y con el
mismo motor sin conexión (`@iris/sync`): las acciones se guardan primero en el teléfono y salen en
orden a `POST /sync/lote`; las fotos y la firma suben por partes (tus 1.0) y el cierre espera a sus
adjuntos. Lo que agrega lo nativo: cámara del sistema, escáner de códigos y GPS en segundo plano.

## Qué hace

- **Sesión:** la misma del panel (`POST /sesion` → `POST /sesion/mfa` con el código TOTP → `GET /yo`).
  La cookie la guarda el sistema de red del teléfono; el token CSRF sale del cuerpo de la respuesta y
  vive en el llavero (`expo-secure-store`). Sin señal abre solo con una verificación de < 12 h
  (`EXPO_PUBLIC_IRIS_CAMPO_HORAS_SIN_SENAL`) y a los 7 días olvida la sesión guardada, como la PWA.
  Antes de cada envío el motor comprueba con `GET /yo` que la sesión sigue siendo del dueño de la cola.
  Cerrar sesión sin señal deja un «cierre pendiente» (con el CSRF de esa sesión en el llavero): la app
  no entra sola y reintenta `DELETE /sesion` antes de cualquier `GET /yo` o inicio de sesión. En
  producción la URL de la API tiene que ser HTTPS (falla al iniciar si no). `allowBackup` apagado en
  Android.
- **Empresa:** con varias, se elige; la cola de cada usuario y empresa va en su propio archivo SQLite
  (`iris-campo-<usuario>-<empresa>.db`, `AlmacenSqlite` de `@iris/sync/sqlite`).
- **Barra inferior** Hoy · Por recibir · Pendientes · Yo (zonas de 56 px) con el contador de lo que
  falta por enviar y por recibir (`contadoresPestanas` de `@iris/sync/campo`). Las vistas son las
  de la PWA (`@iris/sync/campo` `vista.ts`) con una pila propia (`src/campo/navegacion.ts`): una
  pestaña empieza desde ella con «Hoy» debajo, lo demás se apila, y el «Atrás» de Android vuelve a
  la vista anterior (en «Hoy» sale de la app).
- **Hoy (mis órdenes):** orden de ruta publicada o por hora de cita, llegada estimada, ventana
  acordada, «Tu ruta cambió», «Reintentar» si no se pudo bajar el paquete (nunca totales inventados).
- **La orden paso a paso:** resumen (cliente, avisos, pausa con motivo / reanudar, avance, notas,
  «No se pudo») → «Empezar trabajo» → **Llegada** (En camino y Llegué, con GPS si hay permiso) ·
  **Checklist** (formularios de todos los tipos; agregar y quitar) · **Materiales y equipos**
  (bodega, serie/MAC escaneadas, comodato) · **Fotos**, cada
  paso en su vista, guardado en el teléfono al tocar, con «N de 4 pasos listos», «Anterior» /
  «Siguiente» y «Atrás». Reglas de `@iris/sync/campo` (`estadoPasos`, `accionesDe`); los textos del
  avance en `src/campo/pasos.ts`.
- **«Firma del cliente y cierre», paso final y único** (`src/campo/cierre.ts`): solo se abre con lo
  obligatorio listo (si no, dice qué falta y lleva ahí), muestra el resumen del trabajo, el
  cliente firma y «Firmar y cerrar orden» encola la firma y el cierre juntos
  (`encolarFirmaYCierre`), con guarda contra el doble toque (`useAccionUnica`). La firma no
  aparece en otro lugar.
- **Barra de estado discreta:** con señal y todo enviado es un punto junto a «Iris Campo» («En
  línea, todo enviado»); crece con texto y acción solo si hay algo por enviar, un rechazo, la
  sesión venció o la empresa ya no vale (`resumenEnvio`, igual que la PWA).
- **«Por recibir»:** traslados de Argos hacia la bodega del técnico; al abrir uno marca lo que
  faltó (cantidad o series) y firma; la recepción va a la cola con `motor.recibirTraslado`
  (`src/campo/recepcion.ts`, con la misma guarda). Si la firma da 404 (el traslado aún no llega a
  Iris) se reintenta sola; `traslado_no_encontrado` solo se descarta.
- **«Yo»:** «Mi bodega» (existencias, equipos con serie y movimientos, solo lectura, con
  «Reintentar»), actualizar el día, cambiar de empresa y cerrar sesión (enviar y salir, o salir
  conservando lo pendiente). Las lecturas de inventario (`src/campo/inventario.tsx`) se hacen con
  señal y quedan en memoria mientras la sesión está abierta.
- **Por enviar:** la cola, lo que espera archivos, archivos por subir y los rechazos con su motivo
  (Descartar / Rehacer / Descartar todo de esta orden), con «Reintentar».
- **Cámara:** `expo-image-picker` (cámara del sistema, con EXIF) → `expo-image-manipulator` a lado
  ≤ 1600 px y ~300 KB en JPEG; la hora y el GPS del EXIF (o la posición actual) van en el alta.
- **Escáner:** `expo-camera` (`CameraView` con lector nativo: QR, Code 128/39/93, EAN, UPC, Data
  Matrix…); lo leído pasa por `interpretarCodigo` (serie, MAC o QR con ambos).
- **GPS en segundo plano:** `expo-location` + `expo-task-manager`, encendido **solo** mientras hay una
  orden en curso (`en_camino` o `en_sitio`; una pausada no cuenta) y con explicación antes de que el
  sistema pida el permiso. Un punto por minuto a la `ColaPosiciones` de `@iris/sync` (misma base) y de
  ahí a `POST /campo/posiciones`. Con la app cerrada, la tarea guarda los puntos en la base del usuario
  y salen al abrir. Topes: la marca de rastreo lleva `{ usuarioId, empresaId, base, hasta }` con
  `hasta` = medianoche de Guatemala o +12 h (lo primero) desde la última respuesta del servidor
  (paquete o cambios) en esta sesión de la app; nada local lo crea ni lo extiende, y al tope la marca
  queda `vencida`; la tarea descarta y apaga si pasó `hasta`, si la sesión guardada ya no vale sin
  señal o es de otro usuario. Toda salida (cerrar sesión, sesión vencida o ajena, cambiar de empresa)
  apaga el GPS y borra la marca. «Dejar de compartir ubicación» lo apaga (guardado por usuario y
  empresa) hasta el próximo «En camino», «Llegué» o «Reanudar» del técnico. Un error de pantalla
  apaga el GPS en el acto (`RecuperarError`).
- **Capas:** la lógica de las pantallas vive en `src/campo/` (hooks y módulos sin dibujo:
  navegación, pasos, cierre, recepción, inventario, guarda contra el doble toque) y en
  `@iris/sync/campo`; `src/pantallas/` solo dibuja. Así, pasar a HeroUI Native es cambiar la capa
  visual.
- **Identidad Horus:** tokens de `docs/marca/horus-tokens.css` traducidos a `src/tema/tokens.ts`
  (claro y oscuro según el teléfono), íconos de `lucide-react-native`, estados con icono + texto +
  color. Las pantallas usan `src/componentes` (no `Pressable`/`TextInput` crudos; ESLint lo exige).

## Eventos

No emite ni consume eventos del catálogo: habla con la API por HTTP (`/sesion`, `/yo`,
`/campo/paquete`, `/campo/cambios`, `/sync/lote`, `/adjuntos`, `/campo/posiciones`).

## Correr

```bash
cp ../../.env.example .env           # EXPO_PUBLIC_IRIS_API_URL apunta a la API
pnpm --filter @iris/movil start      # Expo (Expo Go no trae la tarea de GPS: usar un dev build)
pnpm --filter @iris/movil exportar   # bundle de Android en dist/ (comprobación sin dispositivo)
```

## Pruebas

`pnpm --filter @iris/movil test` (Jest + jest-expo + React Native Testing Library), sin red ni
dispositivo: inicio de sesión con MFA y CSRF, 429, sin señal; `verificarDueno`; cerrar una orden
paso a paso sin señal y enviarla al volver (motor real sobre memoria, API simulada); barra
inferior con contadores, pasos con «Anterior» / «Siguiente» y «Atrás» de Android, la firma solo
en el paso final, firma obligatoria y doble toque al cerrar, barra de estado discreta; «Por
recibir» (lo que faltó, firma del técnico, doble toque, firma en 404 que espera y se reintenta,
`traslado_no_encontrado` solo se descarta, sin señal con «Reintentar») y «Mi bodega» con error y
«Reintentar», en claro y oscuro (el lienzo de firma se reemplaza en Jest); cola con rechazo
(Rehacer, Descartar todo de la orden); pausa con motivo y reanudación; regla del GPS (en curso,
pausada, explicación, permisos, topes, «Dejar de compartir», tarea en segundo plano con marca
vencida, sesión no vigente o de otro usuario); salidas de la sesión (vencida, ajena, cambiar de
empresa, orden cancelada al sincronizar) apagan el GPS; cierre de sesión pendiente; EXIF de Android e
iOS; compresión y temporales de la cámara; HTTPS obligatorio; base64.
El adaptador SQLite se prueba en `packages/sync` con `node:sqlite`.

## APK

El APK de Android se compila en GitHub Actions (`.github/workflows/movil-apk.yml`, sin EAS):
a mano con la URL de la API o solo en cada versión. La versión y el `versionCode` los pone el CI por
`app.config.js` (`IRIS_MOVIL_VERSION`, `IRIS_MOVIL_VERSION_CODE`); `android/` se genera con
`expo prebuild` y no se commitea. Llave, instalación y cómo reconocer un APK de prueba:
`docs/runbooks/movil-apk.md`.

## Lo que necesita un dispositivo

La lógica está probada; lo nativo (permisos reales, la tarea de GPS con la app en segundo plano o
cerrada, la cámara, el lector de códigos y el PNG de la firma con `toDataURL`) se valida en un
teléfono con un dev build (`npx expo run:android`), que no se hace en CI.
