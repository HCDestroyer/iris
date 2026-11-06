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
- **Diseño «combinada» (nov 2026), igual que la PWA:** encabezado dentro del desplazamiento
  (fecha, punto, «Hola, <nombre>», buscar, cuenta y chips Hoy · Mañana · Semana) que vuelve como
  barra compacta al subir; «Hoy» con la siguiente orden en grande (anillo «2/4», «Continuar
  trabajo», «Cómo llegar», «Llamar»), mosaicos con datos reales y «Después»; **isla de menú**
  Hoy · Por recibir · «+» · Agenda · Yo (54 px, 44 encogida al bajar) con la **píldora de
  estado** encima (lleva a «Por enviar», que también está en «Yo»); hoja del «+» («Agregar
  formulario» con «Deshacer», solo con `ordenes.registrar`; «Agendar tarea propia»
  «Próximamente»); agenda «Mañana» y «Semana» con la tira de 7 días (`src/campo/agenda.tsx`, lee
  `GET /campo/paquete?fecha=<hoy+6>` con señal); pasos con chips, tarjetas de 28 px, − / + en
  materiales y avisos que entran desde arriba con su acción. La lógica es la de `@iris/sync/campo`
  (`inicio.ts`); los colores, `campo` de `useTema()` (mezclas de los `--hx-*`, `coloresCampo`).
  Las vistas son las de la PWA (`vista.ts`) con una pila propia (`src/campo/navegacion.ts`): un
  botón de la isla o un chip empieza desde él con «Hoy» debajo, lo demás se apila, y el «Atrás»
  de Android vuelve a la vista anterior (en «Hoy» sale de la app).
- **Hoy (mis órdenes):** orden de ruta publicada o por hora de cita, llegada estimada, ventana
  acordada, «Tu ruta cambió», «Reintentar» si no se pudo bajar el paquete (nunca totales inventados).
- **Entrada del técnico** (`src/pantallas/Entrar.tsx`, diseño «Login C», igual que la PWA): «Hola,
  técnico», usuario recordado (solo nombre y correo) con «¿No eres tú?», el ojo de la contraseña,
  errores en línea (intentos solo si la API los informa), «¿Olvidaste tu contraseña?», el código de 6
  casillas (`InputOTP` de HeroUI Native) solo con `mfaRequerido`, y el aviso «Sin conexión». **Huella**
  (`expo-local-authentication`), **opcional y apagada por defecto**: sin encenderla, la app abre
  como siempre con la sesión guardada, sin pedir nada. Se enciende en «Yo» («Desbloquear con
  huella») o aceptando el aviso que sale una sola vez tras entrar con la contraseña («¿Usar tu huella
  la próxima vez?»; si no la acepta, queda en «no» y no vuelve a salir). Encendida, con biometría y
  la sesión del usuario recordado guardada (token en el llavero), la app pide al abrir «Usar huella»
  o la contraseña (no depende del usuario recordado); la huella solo desbloquea esa sesión y luego
  se verifica como siempre (`GET /yo` o, sin señal, `usableSinSenal`; regla en `abrirApp`). Nunca
  guarda la contraseña. «¿No eres tú?» con una sesión guardada la cierra entera (`noSoyYo`:
  `DELETE /sesion` o cierre pendiente sin señal, CSRF del llavero, sesión guardada, huella apagada).
  Vuelve a pedirla al regresar al frente tras más de 5 min fuera (`src/campo/bloqueo.ts`).
- **La orden paso a paso:** resumen (cliente, avisos, pausa con motivo / reanudar, avance, notas,
  «No se pudo») → «Empezar trabajo» → **Llegada** (En camino y Llegué, con GPS si hay permiso) ·
  **Checklist** (formularios de todos los tipos; agregar y quitar) · **Materiales y equipos**
  (búsqueda por nombre o código en la bodega del paquete con lo que queda y la modalidad, el
  comodato a «Equipos», cantidad con − / + de 48 px y atajos; serie/MAC escaneadas, comodato) · **Fotos**, cada
  paso en su vista, guardado en el teléfono al tocar (texto y números, al salir del campo), con
  «N de 4 pasos listos» y la **isla de pasos** (`IslaPasos`, la misma píldora que la de menú):
  «Anterior» · un primario que avanza (`estadoIslaPasos` de `@iris/sync/campo`). **Sin cambios no
  hay «Guardar»**: el primario es «Siguiente» («Ir a firma» antes de la firma). Con algo escrito sin
  guardar es **«Guardar y seguir» con el contador**: guarda todo el paso y avanza; si algo no valida,
  se queda y lo dice arriba. Al guardar, «Guardado» con un check en la isla por 1,5 s
  (`MS_GUARDADO`; `RegistroBorradores` y `avisoGuardado`, `src/campo/borradores.tsx`). Sin
  «Guardar» aparte (a 360 px no cabe sin apretar los botones) ni por campo. Con la orden **en pausa** los pasos son de solo lectura, con «Orden en pausa» y
  «Reanudar» arriba (con `ordenes.reanudar`): ni el motor ni la API registran trabajo en pausa.
  Reglas de `@iris/sync/campo` (`estadoPasos`, `accionesDe`); los textos del avance en
  `src/campo/pasos.ts`.
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
- **«Yo»** (diseño aprobado por el PM, nov 2026; `src/pantallas/Yo.tsx` y `Cuenta.tsx`): el
  nombre y «Rol · Empresa» de `GET /yo` (la cuadrilla no tiene nombre en `/yo`: no se muestra).
  **Apariencia** Teléfono · Claro · Oscuro (`Segmentado`), guardada en el almacén de claves del
  teléfono (`iris-campo-apariencia`): `ProveedorTema` elige `crearTema(oscuro)` y
  `Uniwind.setTheme("light" | "dark" | "system")` al instante; «Teléfono» sigue al sistema.
  **Seguridad:** «Cambiar contraseña» y «Verificación en dos pasos» en pantallas propias de la pila
  (`{ tipo: "yo", seccion }`, «Atrás» vuelve y salir con algo escrito pregunta, también con el
  «Atrás» de Android) y «Desbloquear con huella». Contraseña: validación espejo de la API
  (`esquemaContrasenaPropia` de `@iris/sync/campo`) con las ayudas en verde, `POST
  /yo/contrasena` con `Idempotency-Key`, 422 por campo, 429 `demasiados_intentos` (la API cerró
  la sesión: «Ir a iniciar sesión» conserva lo pendiente) y sin señal no se intenta (nada de esto
  va a la cola). Dos pasos apagada: `POST /yo/mfa` una vez por intento con una clave nueva (no es
  repetible), «Abrir app» (`otpauth://` de la API), «Copiar clave» (la hoja del sistema: la app no
  trae módulo de portapapeles; la clave también se puede seleccionar) y el código de 6 dígitos →
  `POST /yo/mfa/activar`; activa, solo informa (desactivar no existe: la reinicia un admin).
  **Este teléfono:** «Mi bodega» (existencias, equipos con serie y movimientos, solo lectura, con
  «Reintentar»), «Por enviar», actualizar el día, cambiar de empresa y «Cerrar sesión» de contorno
  rojo (enviar y salir, o salir conservando lo pendiente). Las lecturas de inventario (`src/campo/inventario.tsx`) se hacen con
  señal y quedan en memoria mientras la sesión está abierta.
- **Por enviar:** la cola, lo que espera archivos, archivos por subir y los rechazos con su motivo
  (Descartar / Rehacer / Descartar todo de esta orden), con «Reintentar».
- **Cámara:** `expo-image-picker` (cámara del sistema, con EXIF) → `expo-image-manipulator` a lado
  ≤ 1600 px y ~300 KB en JPEG; la hora y el GPS del EXIF (o la posición actual) van en el alta.
- **Escáner por campo:** cada campo (serie y MAC) lleva su botón y el visor abre con su objetivo
  («Escaneando: Número de serie»). `expo-camera` (`CameraView` con lector nativo: QR, Code
  128/39/93, EAN, UPC, Data Matrix…) a pantalla completa, oscurecida salvo el recuadro con esquinas
  teal: solo cuentan los códigos cuyo `bounds`/`cornerPoints` cae dentro. Junta los distintos
  durante ~800 ms (`SesionEscaneo` de `@iris/sync/campo`, con `Vibration` de React Native al primero)
  y muestra la hoja con lo detectado (tipo, valor, «Parece serie», «Parece MAC», «QR con serie y
  MAC», «Parece código de producto (EAN/UPC)», «No es una MAC»…); un solo candidato que encaja se
  confirma. Linterna (`enableTorch`), «Seguir escaneando» y «Escribir a mano». Al elegir, aviso
  («Serie: ABC123») e insignia «Escaneado»; si ya está en otro equipo de la orden, se avisa antes.
  Lógica en `src/campo/escaner.ts` (`useEscaneo`, `codigoDeExpo`).
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
- **Identidad Horus con HeroUI Native (fase 5):** HeroUI Native 1 sobre Uniwind (Tailwind 4 para
  React Native). `src/global.css` trae el tema: cada variable de HeroUI apunta a un `--hx-*` de
  `docs/marca/horus-tokens.css` (el mismo mapeo que `packages/ui/src/estilos/tema-heroui.css` en la
  web), con un solo color de marca (teal) y claro y oscuro según el teléfono (`@variant light` /
  `@variant dark`). `src/tema/tokens.ts` lleva los mismos colores en JS para lo que no se pinta con
  clases (íconos de Lucide, lienzo de firma). `prueba/tema.test.ts` comprueba que los tres coinciden.
  `GestureHandlerRootView` va en la raíz (`App.tsx`) y `ProveedorTema` monta `HeroUINativeProvider`.
- **Componentes** (`src/componentes`, el equivalente de `@iris/ui`; las pantallas no importan de
  `heroui-native` ni usan `Pressable`/`TextInput`/`ActivityIndicator` crudos, ESLint lo exige). Los
  nombres y las props no cambiaron con HeroUI:

  | Componente | Por dentro |
  | --- | --- |
  | `Boton` | HeroUI `Button` (`primary`, `outline`, `ghost`, `danger`), esquina de 6 px, 48 px de alto, `Spinner` al cargar |
  | `Campo` | HeroUI `TextField` + `Label` + `Input` + `Description` + `FieldError` (48 px) |
  | `Tarjeta` | HeroUI `Card` (radio 28 px, borde de 1 px, panel del tema) |
  | `BotonRedondo` | HeroUI `PressableFeedback` redondo (isla, buscar, cuenta, − / +): 44 px como mínimo |
  | `Insignia` | HeroUI `Chip` suave con icono; «info» con el azul de Horus |
  | `Aviso` | HeroUI `Alert` con fondo tintado; `alert` solo en errores |
  | `Opciones` | HeroUI `RadioGroup` + `Radio` (48 px por opción) |
  | `Tocable` | HeroUI `PressableFeedback` (44 px como mínimo; 56 px en la barra inferior) |
  | `Cargando` | HeroUI `Spinner` teal con nombre accesible |
  | `Dialogo` | `Modal` de React Native con la capa flotante de Horus (ver abajo) |

  - **`Dialogo` no es el `Dialog` de HeroUI:** ese no atiende el «Atrás» de Android, que entonces
    cambiaría de vista debajo de la hoja abierta. El `Modal` lo cierra (`onRequestClose`). Toda hoja
    lleva su «X» («Cerrar»); con `cambios`, la «X» y «Atrás» preguntan «¿Descartar los cambios?».
    Con el teclado abierto la hoja sube por encima (`useAltoTeclado`: con edge-to-edge la ventana
    ya no se encoge sola) y `fijo` deja arriba lo que no se desplaza (el campo de «Buscar orden»,
    que filtra en vivo con `ESPERA_BUSQUEDA_MS`).
  - **Avisos emergentes:** el `Toast` de HeroUI Native (`AvisoEmergente`; `toast` en
    `src/tema/tema.tsx`), arriba, con su animación de entrada y salida, uno a la vez. Va debajo del
    margen seguro (`insetsAvisos`: margen + 8 px; un `insets` propio reemplaza el de HeroUI, no se
    suma), así que no queda bajo la barra de estado ni detrás de la franja sólida. En las
    pruebas, `avisoArriba()`: HeroUI dibuja además una copia invisible para medir su alto.
  - **Encabezado:** una franja sólida (el lienzo) bajo la barra de estado y la barra compacta, sólida,
    que entra y se va deslizándose (solo `translateY`, nunca la opacidad).
  - **El lienzo de firma sigue con `PanResponder`** y no va dentro de ningún detector de Gesture
    Handler 3; una vez que el dedo empieza a firmar, el desplazamiento de la vista no se lleva el
    trazo (`onPanResponderTerminationRequest: false`).
  - **Las medidas táctiles van en `style`** (y las pruebas las miden); los colores y la forma, en
    clases de Uniwind.
  - **Fondos tintados con `bg-suave-*`**, no `bg-success/10` ni `bg-*-soft`: con un color que cambia
    con el tema, Uniwind resuelve esa mezcla al compilar y la deja en `unset`.
  - **Barra de estado:** con señal y todo enviado es un punto de 8 px con su halo, sin texto (el
    estado completo va en su nombre accesible). **Barra inferior y encabezado** son vidrio
    (`bg-glass`); la pestaña actual va en teal, con raya, icono más grueso y texto en negrita.
  - **«Firma del cliente y cierre»:** dice «Todo lo obligatorio está listo», muestra el resumen,
    pide la firma y, si falta el trazo, lo dice con un aviso (icono + texto + color).

## Eventos

No emite ni consume eventos del catálogo: habla con la API por HTTP (`/sesion`, `/yo`,
`/campo/paquete`, `/campo/cambios`, `/sync/lote`, `/adjuntos`, `/campo/posiciones`).

## Correr

```bash
cp ../../.env.example .env           # EXPO_PUBLIC_IRIS_API_URL apunta a la API
pnpm --filter @iris/movil start      # Expo (Expo Go no trae la tarea de GPS: usar un dev build)
pnpm --filter @iris/movil exportar   # bundle de Android en dist/ (comprobación sin dispositivo)
```

Metro compila el tema con Uniwind (`metro.config.js`) y escribe `src/uniwind-types.d.ts` (tipos de
`className`; se regenera al arrancar Metro y se commitea para el `typecheck` del CI). No hace falta
`babel.config.js`: `babel-preset-expo` agrega solo el plugin de `react-native-worklets`.

## Pruebas

`pnpm --filter @iris/movil test` (Jest + jest-expo + React Native Testing Library), sin red ni
dispositivo. Reanimated y Worklets van en su versión de JS (`prueba/resolver.js`, el resolutor de
Reanimated corregido para las rutas de pnpm) con el simulacro oficial de Worklets, y Gesture
Handler con su `jestSetup`; en Jest Uniwind no compila clases, así que las pruebas miden lo que va
en `style`. Cubre: componentes sobre HeroUI (zonas de 44 px o más, deshabilitado y ocupado, radios,
errores con texto, «Atrás» cierra la hoja, el lienzo no suelta el trazo) y el tema (cada `--hx-*`
igual a `docs/marca/horus-tokens.css`, en claro y oscuro); inicio de sesión con MFA y CSRF, 429, sin señal; `verificarDueno`; cerrar una orden
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
iOS; compresión y temporales de la cámara; HTTPS obligatorio; base64; escáner por campo
(`prueba/escaner.test.tsx`: abre con su objetivo, no acepta con un cuadro, solo cuenta lo del
recuadro, lista varios códigos y llena el campo correcto, «Seguir escaneando», QR con serie y MAC,
aviso de repetido, linterna y sin permiso, con `onBarcodeScanned` simulado); «Yo»
(`prueba/yo.test.tsx`, claro y oscuro: «Rol · Empresa», los tres modos de apariencia y la
guardada al abrir, cambiar contraseña con sus ayudas, 422, 429 y sin señal, salir con cambios con
el botón y con «Atrás» de Android, y los dos pasos completos con la API simulada).
El adaptador SQLite se prueba en `packages/sync` con `node:sqlite`.

## APK

El APK de Android se compila en GitHub Actions (`.github/workflows/movil-apk.yml`, sin EAS):
a mano con la URL de la API o solo en cada versión. La versión y el `versionCode` los pone el CI por
`app.config.js` (`IRIS_MOVIL_VERSION`, `IRIS_MOVIL_VERSION_CODE`); `android/` se genera con
`expo prebuild` y no se commitea. Llave, instalación y cómo reconocer un APK de prueba:
`docs/runbooks/movil-apk.md`.

## Lo que necesita un dispositivo

La lógica está probada; lo nativo (permisos reales, la tarea de GPS con la app en segundo plano o
cerrada, la cámara, el lector de códigos con varios códigos en la etiqueta (`bounds` reales del
recuadro, linterna, vibración), el trazo y el PNG de la firma con `toDataURL`, y cómo se
ven los componentes de HeroUI con el tema compilado por Uniwind, en claro y oscuro) se valida en un
teléfono con un dev build (`npx expo run:android`), que no se hace en CI.
