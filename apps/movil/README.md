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
- **Mis órdenes:** orden de ruta publicada o por hora de cita, llegada estimada, ventana acordada,
  «Tu ruta cambió», «Reintentar» si no se pudo bajar el paquete (nunca totales inventados).
- **Detalle de la orden:** En camino y Llegué (con GPS si hay permiso), pausar con motivo / reanudar,
  formularios y checklist (sí/no, selección, texto, número, medición, foto), agregar y quitar
  formularios, materiales de la bodega, equipos (serie/MAC escaneadas), firma, fotos, notas, cerrar
  o «No se pudo» con motivo. Las reglas (`accionesDe`, `faltantesCierre`), los esquemas zod y los
  textos vienen de `@iris/sync/campo`, los mismos de la PWA.
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
  `hasta` = medianoche de Guatemala o +12 h (lo primero), que solo se renueva cuando la app confirma
  con el servidor; la tarea descarta y apaga si pasó `hasta`, si la sesión guardada ya no vale sin
  señal o es de otro usuario. Toda salida (cerrar sesión, sesión vencida o ajena, cambiar de empresa)
  apaga el GPS y borra la marca. «Dejar de compartir ubicación» lo apaga hasta que cambie de estado
  alguna orden.
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
sin señal y enviarla al volver (motor real sobre memoria, API simulada); cola con rechazo
(Rehacer, Descartar todo de la orden); pausa con motivo y reanudación; regla del GPS (en curso,
pausada, explicación, permisos, topes, «Dejar de compartir», tarea en segundo plano con marca
vencida, sesión no vigente o de otro usuario); salidas de la sesión (vencida, ajena, cambiar de
empresa, orden cancelada al sincronizar) apagan el GPS; cierre de sesión pendiente; EXIF de Android e
iOS; compresión y temporales de la cámara; HTTPS obligatorio; base64.
El adaptador SQLite se prueba en `packages/sync` con `node:sqlite`.

## Lo que necesita un dispositivo

La lógica está probada; lo nativo (permisos reales, la tarea de GPS con la app en segundo plano o
cerrada, la cámara, el lector de códigos y el PNG de la firma con `toDataURL`) se valida en un
teléfono con un dev build (`npx expo run:android`), que no se hace en CI.
