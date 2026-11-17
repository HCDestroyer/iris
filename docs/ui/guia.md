# Guía de interfaz de Iris

Decisiones aprobadas por el PM (propuesta «Nueva interfaz de Iris», 31 oct 2026) y cómo aplicarlas. La
siguen todos los frentes que tocan pantallas: panel (I3), app de campo (I4) y portal de soporte.

## Librería

- **Web (`apps/web`: panel, `/campo` y `/soporte`): HeroUI v3** (`@heroui/react` y `@heroui/styles`
  3.2.6), sobre React Aria, Tailwind 4 y React 19. No necesita proveedor.
- **App nativa (`apps/movil`, fase 5): HeroUI Native v1** (Expo SDK 58, Uniwind), con el mismo tema.
- HeroUI se adapta a la identidad Horus y no la reemplaza:
    - un solo color de marca (teal);
    - todo estado con icono + texto + color;
    - íconos solo de Lucide;
    - modo claro y oscuro.
- **Las pantallas importan de `@iris/ui`, nunca de `@heroui/react`.** Si falta una pieza, se agrega a
  `packages/ui` con nombre en español, su prueba y su entrada en el mapa de abajo.
- Nada de `<button>` ni `<input>` crudos fuera de `packages/ui`.

## Tema Horus

- `docs/marca/horus-tokens.css` es la única fuente de color. `packages/ui/src/estilos/horus-tokens.css`
  es copia exacta (una prueba lo verifica).
- `packages/ui/src/estilos/tema-heroui.css` apunta cada variable de HeroUI a un token `--hx-*`, sin
  colores nuevos:

| HeroUI | Horus |
| --- | --- |
| `accent`, `focus`, `link` | `--hx-primary` (teal 700 en claro, 400 en oscuro) |
| `success`, `warning`, `danger` | `--hx-success`, `--hx-warning`, `--hx-error`; texto sobre el relleno = fondo del tema |
| `background`, `foreground`, `muted` | `--hx-bg`, `--hx-text`, `--hx-text-muted` |
| `default`, `background-secondary` | `--hx-bg-elevated`, `--hx-bg-muted` |
| `surface` (tarjetas) | `--hx-card`: casi sólida, radio 14 px |
| `overlay` (todo lo flotante: modales, hojas, popovers, menús, listas, globos y avisos emergentes) | **sólido**: `--hx-card` en claro (blanco opaco) y `--hx-bg-elevated` en oscuro (opaco, más claro que el lienzo y las tarjetas), con borde de 1 px `--hx-border` y sombra; sin desenfoque |
| `backdrop` (telón de modal, hoja y confirmación) | negro al 45 % en claro y al 62 % en oscuro: lo de atrás siempre se oscurece |
| `field-*` | fondo del tema, borde de 1 px `--hx-border`, radio 6 px, foco teal |
| `radius` | `--hx-radius` (6 px): botones con esquina de Horus, no píldora |

- HeroUI no tiene «info»: el azul de Horus es la utilidad `info` de Iris (`text-info`, `bg-info/10`).
  `accent` nunca es un estado.
- El modo oscuro va por la clase `.dark` (next-themes) o por `[data-theme]`. La variante `dark:` de
  Iris reemplaza la de HeroUI, que además seguía `prefers-color-scheme` aunque se eligiera «Claro».
- `muted` en HeroUI es texto atenuado. El fondo tenue de Iris es `bg-background-secondary`, antes
  `bg-muted`.
- **Nada de vidrio en lo flotante** (pedido del PM: «alertas o sidebars sin fondo se notan como
  parte del primer plano»). El vidrio (`glass-material`) queda solo en la barra lateral y la
  superior del panel, como pide la identidad Horus. Todo panel flotante es de color sólido con
  borde y sombra, y toda capa modal lleva su telón oscurecido; lo verifica
  `e2e/capas-flotantes.spec.ts` (estilo computado del panel y del telón, en los 4 proyectos).
- Los `Aviso` de la página tienen fondo tintado **opaco** por tono: el 10 % del color del tono
  mezclado con el lienzo del tema (`color-mix(… var(--hx-bg))`), con borde al 30 %.
- Con `prefers-reduced-motion` no hay animaciones.

## Reglas de experiencia

1. **Formularios de más de 3 campos van en pantalla propia**, con su ruta (`/ordenes/nueva`,
   `/usuarios/[id]/editar`) y `FormularioPagina`:
    - secciones;
    - «Guardar» siempre visible al pie (`BarraGuardar`);
    - se puede compartir el enlace.
2. **Un formulario nunca se abre encima de otro.** Desde una ficha, «Editar» lleva a otra pantalla y
   «Atrás» vuelve a donde estabas. Si sales con cambios sin guardar, Iris pregunta antes de
   descartar, sea por un enlace, por «Cancelar», por «Atrás» o al recargar.
3. **Los diálogos son solo para confirmar y para acciones cortas (1 a 3 campos):** desactivar,
   cancelar, cambiar contraseña, reautenticar, «Mover a…», cuadrilla o zona nueva, guardar vista. Las
   confirmaciones destructivas usan `DialogoConfirmar peligro`.
4. **Migas en cada pantalla del panel** (`Migas`). Reemplazan los enlaces «← Volver».
5. **Aviso emergente después de cada acción** que cambia algo (`avisar`), con «Deshacer» cuando se
   puede. Los errores que piden actuar no van en un aviso emergente: van como `Aviso` en la pantalla,
   con «Reintentar».
6. **Indicadores de estado discretos.** Si todo está bien, es solo un punto. Crecen y explican solo
   cuando hace falta actuar: sin señal con cosas por enviar, sesión vencida o un error accionable.
7. **Fichas con pestañas** (`Pestanas`):
    - Ticket: Resumen · Conversación · Órdenes · Historial;
    - Orden: Resumen · Evidencias · Formularios · Argos;
    - Técnico: Datos · Agenda · Ausencias.

   Las acciones de la ficha van en un panel lateral.
8. **App de campo:** cada paso de trabajo es una vista independiente que se guarda sola en el
   teléfono:
    - llegada;
    - checklist;
    - materiales y equipos;
    - fotos.

   La **firma del cliente es el paso final y único, junto con el cierre**. Solo se abre con todo lo
   obligatorio completo, y la firma no aparece antes ni en otro lugar.

Además se mantienen las reglas de siempre:

- react-hook-form + zod;
- modo claro y oscuro;
- revisar en 390 px y 1280 px;
- «Reintentar» en los errores;
- nunca totales calculados sobre datos que no se pudieron leer.

## Mapa de componentes

Los nombres y las props de `@iris/ui` no cambiaron: las pantallas siguen igual y por dentro usan
**los componentes de HeroUI v3** (`@heroui/react` 3.2.6, sus piezas compuestas: `Modal.Backdrop`,
`Card.Header`…), con los tokens de Horus. React Aria da el comportamiento: teclado, foco y lectores
de pantalla. Los envoltorios que reciben un icono de Lucide como componente (`Aviso`,
`InsigniaEstado`, `EstadoVacio`) siguen sirviendo desde un componente de servidor: el icono se
dibuja afuera y la pieza de HeroUI va en un archivo `"use client"` aparte (`aviso-alerta.tsx`,
`chip-estado.tsx`, `raiz-vacio.tsx`), porque `@heroui/react` solo corre en el cliente.

| `@iris/ui` | Componente de HeroUI | Notas |
| --- | --- | --- |
| `Boton`, `BotonEnviar` | `Button` | Variantes de Iris → `primary`, `tertiary`, `outline`, `ghost`; medidas y radio de Horus. `disabled` → `isDisabled`; `onClick`, `type="submit"`, `data-*` y `aria-*` como siempre. Lo que el `Button` de React Aria filtra (`title`, `role`, `tabIndex`, `aria-invalid`, `aria-keyshortcuts`, el arrastre nativo `draggable`/`onDrag*` del calendario de despacho) va directo al `<button>` por `render`. Como todo `Button` de React Aria, el clic no sigue subiendo a los padres. `asChild` (un `<Link>` con aspecto de botón) usa `Ranura` con las mismas clases |
| `Tarjeta*` | `Card`, `Card.Header`, `Card.Title` (como `<h2>`), `Card.Description`, `Card.Content`, `Card.Footer` | Acomodo neutro: cada pantalla manda con sus clases; material `surface-card` |
| `Aviso`, `ErrorFormulario` | `Alert`, `Alert.Indicator`, `Alert.Content`, `Alert.Title` y la pieza `alert__description` | Fondo tintado opaco por tono, borde, icono de Lucide + título + texto; `role="alert"` en `error`. La descripción es un `<div>` (`Alert.Description` es un `<span>` y aquí caben párrafos y listas) |
| `Dialogo*`, `DialogoProtegido` | `Modal.Backdrop` + `Modal.Container` + `Modal.Dialog` + `Modal.Heading` | Panel sólido sobre telón; en el teléfono es una hoja inferior a lo ancho. La «X» es un `Boton` con el icono `X` de Lucide (no el `CloseButton` de HeroUI, que trae su propio icono) |
| `DialogoConfirmar` | `AlertDialog.Backdrop` + `AlertDialog.Container` + `AlertDialog.Dialog` + `AlertDialog.Heading` | `role="alertdialog"`, foco inicial en «Cancelar», no se cierra tocando fuera |
| `Hoja*`, `PanelProtegido` | `Drawer.Backdrop` + `Drawer.Content` + `Drawer.Dialog` + `Drawer.Heading` | Siempre con telón oscurecido: `fondo="transparente"` se acepta pero ya no quita el telón. Se cierra también arrastrando hacia su lado; el contenido se desplaza en vertical (`touch-action: pan-y`) |
| `PanelEmergente` | `Popover`, `Popover.Content`, `Popover.Dialog`, `Popover.Heading` | Diálogo no modal junto al disparador |
| `PanelLateral` | — (`<section role="dialog" aria-modal="false">` propio) | **Excepción aprobada por el PM a la regla de `Hoja`:** panel **flotante** sin telón, bajo la barra superior. Desde `sm`: tarjeta de 380 px a 12 px de los bordes, con borde, sombra y esquinas (`--hx-radius-card`); en el teléfono, a pantalla completa bajo la barra. **Flota sobre la pantalla: la página no se corre ni cambia de ancho al abrirlo, y los clics fuera del panel le llegan a ella** (pedido del PM, nov. 2026: la agenda quedaba apretada). Lo que queda debajo se recupera cerrándolo: la cabecera con «Cerrar» no se desplaza y siempre se ve. En escritorio (desde 1024 px) los avisos emergentes se corren a su izquierda (`globals.css`, por `[data-region="avisos-emergentes"]`: HeroUI pisa el `data-slot` de la región); debajo van en la barra superior y no se cruzan con él. No atrapa el foco: al abrir lo lleva al elemento con `data-foco-inicial` (o al panel), Escape lo cierra solo con el foco adentro (y si nada de adentro usó la tecla) y al cerrar vuelve al disparador. Sólido (`--overlay`). Hoy solo lo usa el chat del equipo |
| `ListaSugerencias`, `teclasSugerencias`, `idOpcion` | — (combobox de ARIA con foco virtual) | Sugerencias bajo un `AreaTexto` que sigue con el foco: las `@menciones` del chat. El área lleva `role="combobox"`, `aria-controls` y `aria-activedescendant`; flechas, Enter/Tab y Escape. `Autocompletar` no sirve: reemplaza todo el valor de un campo de una línea |
| `MenuDesplegable*`, `MenuEnLinea` | `Dropdown`, `Dropdown.Popover`, `Dropdown.Menu`, `Dropdown.Section`, `Dropdown.Item`, `Menu` y `Separator` | `onSelect` → `onAction`. Un `Boton` como disparador se conecta solo; otro elemento, por `Pressable` |
| `Globo` | `Tooltip`, `Tooltip.Content` | Un `Boton` se conecta solo; otro elemento, por `Focusable` |
| `GrupoSegmentado` | `ToggleButtonGroup` + `ToggleButton` (selección única) | `role="radiogroup"` y `role="radio"`; como un grupo de radios, las flechas mueven y eligen |
| `GrupoOpciones` | `RadioGroup`, `Label`, `Radio`, `Radio.Content`, `Radio.Indicator` | Radios reales; tarjetas grandes con la marca `CircleCheck` de Lucide |
| `Casilla` | `Checkbox`, `Checkbox.Content`, `Checkbox.Control`, `Checkbox.Indicator` | Sirve con `register` y controlada: el `ref` llega al `<input>` (`inputRef`), `onChange` recibe un evento con `target` = ese input, y la marca (`Check` de Lucide) se dibuja desde `:checked`, porque react-hook-form escribe `checked` directo en el input (valores iniciales, `reset`, `setValue`) |
| `Tabla*` | `Table` (variante `secondary`) + `Table.ScrollContainer`, con una `<table>` semántica y las piezas `table__row`, `table__cell`… | Ver «Lo que no se usa» (`Table.Content`) |
| `Esqueleto` | `Skeleton` (pulso) | |
| `Cargando` | `Spinner` | Es un indicador animado, no un icono; `role="status"` con su texto |
| `EstadoVacio` | `EmptyState` | |
| `InsigniaEstado` | `Chip` suave + `Chip.Label` | `chip--success`, `--warning`, `--danger`; `info` y `neutral` con el color de Iris |
| `PaletaBusqueda` | `Modal.Backdrop` + `Modal.Container` + `Modal.Dialog` y `Kbd` en la ayuda de teclas | El buscador por dentro sigue siendo el `Autocomplete` de React Aria (ver abajo) |
| `Pestanas` | `Tabs` | Fichas con pestañas; con `href`, cada pestaña tiene URL |
| `ProveedorAvisosEmergentes`, `avisar`, `useAvisoEmergente`, `cerrarAviso` | `Toast` | Panel sólido (HeroUI usa `surface`, translúcida en oscuro) |
| `Migas` | `Breadcrumbs` | |
| `Selector`, `CampoSelector` | `Select`, `Select.Trigger`, `Select.Value`, `Select.Indicator`, `Select.Popover`, `ListBox`, `ListBox.Item`, `Label` | Se escriben con `<option>` como antes. `register`, `reset` y `setValue` siguen sirviendo: el `ref` va a un input oculto con el `name` y cada escritura de `value` mueve la opción (`control-nativo.tsx`); `onChange(e)` recibe `e.target.value`. El botón lleva `data-valor`. Chevron de Lucide. En el teléfono la lista es un popover del ancho del campo que no se sale de la pantalla |
| `SelectorFecha`, `SelectorRangoFechas` | `DatePicker`, `DateRangePicker` | |
| `CampoFecha`, `CampoHora` | `DatePicker` (con `DateField` y `Calendar`), `TimeField` | Reemplazan a `<Campo type="date">` y `type="time"` con la misma forma (`register` o `value` + `onChange`). Valores `AAAA-MM-DD` y `HH:MM` (24 h): hora local de Guatemala, a UTC al enviar |
| `Autocompletar` | `ComboBox` | Con `alBuscar` busca en el servidor: `cargando`, `sinResultados`, `cabeceraLista` («Recientes») y opciones con `contenido` propio; la primera queda resaltada al llegar los resultados y Enter la elige. Enter nunca envía el formulario desde el campo. Salir del campo o Escape no borran lo escrito. `SelectorCliente` avisa con `alBuscarSinElegir` si hay una búsqueda sin elegir o «Cambiar cliente» abierto, y el formulario no se envía hasta elegir, borrar o «Dejar el cliente elegido» |
| `ListaEleccion` | `RadioGroup`, `Label`, `Radio`, `Radio.Content`, `Radio.Indicator` | Una entre varias filas con contenido propio (avatar, detalle, estado a la derecha); `deshabilitada` no se elige, `atenuada` sí; `pegadas` para una lista de búsqueda |
| `ListaOpciones` | `ListBox` (`onAction`) | Elegir una tarjeta entre pocas (servicio del cliente): flechas, Enter y clic |
| `Interruptor` | `Switch`, `Switch.Content`, `Switch.Control`, `Switch.Thumb` | Ajustes de encendido/apagado que cambian lo que sigue. Con `register` como `Casilla`. `alFinal`: la etiqueta a la izquierda y el control a la derecha, a todo el ancho (filas de una lista de ajustes) |
| `CampoNumero` | `NumberField` (+ `DecrementButton` / `IncrementButton` con `conPasos`) | Cantidades y montos; el formulario recibe el texto del número (`"250.5"`) |
| `CampoBusqueda` | `SearchField` (`SearchIcon`, `Input`, `ClearButton`) | Lupa y «X» de Lucide; Escape borra |
| `SelectorArchivo` | — (`<input type="file">` oculto en su `<label>`, como `Camara`) | Adjuntar sin cámara (compositor de la bandeja); clip de Lucide; se limpia tras elegir |
| `AvatarUsuario` | `Avatar`, `Avatar.Fallback` | Iniciales (decorativo: el nombre va al lado) |
| `Entrada`, `Campo` | `Input` | Un `<input>` nativo: sirve con `register` |
| `CampoSecreto` | `Input` | Credencial que se pega una vez (token, app secret), con el ojo «Mostrar …», monoespaciada y sin corrector. Para que el gestor de contraseñas no ofrezca guardarla: `type="text"` con `-webkit-text-security: disc` donde el navegador lo soporta y, si no, `type="password"`; siempre `autocomplete="new-password"`, `data-1p-ignore` y `data-lpignore`. Sirve con `register` |
| `AreaTexto`, `CampoArea` | `TextArea` | |
| `Etiqueta` | `Label` | |
| `ProveedorRutas` | `RouterProvider` de React Aria | |
| `BarraGuardar`, `FormularioPagina`, `SeccionFormulario` | — | Composición propia de Iris con `Boton`. `BarraGuardar` acepta `nota` (una línea a la izquierda: «Al guardar, Iris prueba la conexión con Meta.»). `FormularioPagina` acepta `accionTitulo` (a la derecha del título: el interruptor que enciende lo que la pantalla configura) |

`useCambiosSinGuardar(isDirty, isSubmitting)` sirve igual dentro de `DialogoProtegido` que dentro de
`FormularioPagina`.

### Lo que no se usa de HeroUI (y por qué)

| Envoltorio o componente | Por qué |
| --- | --- |
| `Table.Content`, `Table.Header`, `Table.Row`, `Table.Cell` (la rejilla de React Aria) | Es una colección con `role="grid"` y teclado propio: las filas de Iris llevan enlaces y botones libres y se arman con condiciones. Se usan `Table` y `Table.ScrollContainer` y las piezas de estilo; la rejilla queda para cuando una pantalla necesite orden o selección |
| `CloseButton` y los íconos por defecto de `Alert`, `Checkbox`, `Toast`… | Traen sus propios SVG; la identidad pide solo íconos de Lucide. Se pasan los de Lucide (`X`, `Check`, `CircleCheck`, los del tono) |
| `Pagination` | Las listas de Iris paginan por cursor (`?cursor=&limit=`, regla 7): no hay número de páginas. Sigue «Cargar más» (`PieLista`) |
| `SearchField` de HeroUI en el buscador global | El buscador es un `Autocomplete` de React Aria con foco virtual; usa el `SearchField` de React Aria que conecta con él. Cambiarlo no mejora nada visible |
| `Accordion` / `Disclosure` | El único «ver más» del panel («Ver revisiones» de la cotización) ya es un `Boton` con `aria-expanded` y el contenido abajo de la fila de acciones; un `Disclosure` pondría el contenido pegado al botón |
| `ProgressBar`, `Slider`, `InputOTP`… | No reemplazan nada hecho a mano que lo pida (el avance del técnico en Inicio es una barra de 6 px con `role="progressbar"`; el código MFA es un `Campo` de 6 cifras). Están disponibles para agregarlos a `packages/ui` con nombre en español cuando una pantalla los pida |
| `Camara`, `VisorEscaner`, `LienzoFirma`, `LogoIris`, `MarcaIris` | No tienen equivalente en HeroUI (cámara con `<input capture>`, escáner con `BarcodeDetector`/ZXing, firma con pointer events, logo). Siguen como estaban, con los tokens del tema y `Boton` por dentro |
| Animación de salida de modal, hoja y confirmación | Se desmontan en el acto: con salida animada, un Escape o un clic inmediato le llegaría a la capa que se está yendo |

## Auditoría de controles (nov. 2026)

Pedido del PM: «¿toda la versión web usa ya los componentes de HeroUI?». Se buscó en `apps/web`
(sin pruebas) `<select>`, `<input>`, `<button>`, `<textarea>`, `<dialog>`, `role="dialog"`,
`role="combobox"` y `role="listbox"`, y los `<input type="date|time">` de `Campo`. Lo que quedaba
se pasó a HeroUI:

| Antes | Ahora (`@iris/ui`) | Pieza de HeroUI | Dónde |
| --- | --- | --- | --- |
| `<select>` nativo con estilo de campo | `Selector`, `CampoSelector` | `Select` + `ListBox` | Unas 47 pantallas con listas (filtros, formularios, diálogos, chips de la bandeja, empresa activa) y, por ser el mismo envoltorio, la app de campo |
| `<Campo type="date">` / `<Entrada type="date">` | `CampoFecha` | `DatePicker` | Reportes, encuestas, historial de accesos, tablero de órdenes, despacho (día), programar orden, crear orden desde ticket, mover cita, ausencia, próxima acción y vigencia de la cotización |
| `<Campo type="time">` | `CampoHora` | `TimeField` | Programar orden, crear orden desde ticket, mover cita, ausencia y próxima acción |
| Combobox y listas hechos a mano en el selector de cliente (`role="combobox"`, `role="listbox"`, `role="option"`, teclado propio) | `Autocompletar` con búsqueda en el servidor + `ListaOpciones` | `ComboBox` + `ListBox` | Alta rápida de ticket y nueva orden. Ya no abre un diálogo: la lista sale bajo el campo (cabe en 390 px) |
| Lupa encima de una `Entrada type="search"` / `Campo type="search"` | `CampoBusqueda` | `SearchField` | Bandeja de tickets, tablero de órdenes, comercial, técnicos, buscar usuario (nuevo técnico) y buscar permiso (roles) |
| `Casilla` para prender o apagar un ajuste | `Interruptor` | `Switch` | «Enviar las respuestas por correo» (buzón), «Todo el día» (ausencia), «Pide la firma del cliente» (plantilla) |
| `Campo inputMode="decimal"` para cantidades y montos | `CampoNumero` | `NumberField` | Cotización: cantidad (con − y +), precio unitario y monto a restar |
| Círculo de iniciales hecho a mano | `AvatarUsuario` | `Avatar` | Cuenta al pie de la barra lateral y «Equipo hoy» en Inicio |

**Quedan, con su motivo:**

| Qué | Dónde | Por qué |
| --- | --- | --- |
| Visor del escáner (`role="dialog"` propio) | `app/campo/_componentes/escaner-equipo.tsx` (I4) | Cámara en vivo con recuadro y `BarcodeDetector`/ZXing: no hay pieza de HeroUI. Por dentro usa `Boton` y `Hoja` |
| `Camara`, `LienzoFirma`, `VisorEscaner` | `packages/ui` | `<input capture>`, lienzo con pointer events y video: sin equivalente en HeroUI |
| Turnos «Desde» / «Hasta» como `Campo` de texto (`08:00`) | `componentes/equipo/formulario-turnos.tsx` | Un turno puede terminar a las `24:00`, y `TimeField` solo llega a 23:59 |
| Puertos, ids de zona o de bodega, latitud y longitud como `Campo` | Buzón, tablero, técnico, crear orden, prospecto | Son identificadores o coordenadas con su propio formato, no cantidades: `NumberField` los formatearía |
| «Ver revisiones» | Cotización | Ya es un `Boton` con `aria-expanded` (ver arriba) |
| `<label>` del chip de filtro | Bandeja de tickets | Solo etiqueta al `Selector` del chip (`htmlFor`); el `Selector` la suma a su nombre |
| Puntos de técnicos y órdenes en el mapa | `componentes/despacho/mapa.tsx`, `app/(panel)/mapa` | Son marcadores de MapLibre dibujados en el mapa, no controles de formulario |

En las pruebas, `e2e/controles.ts` trae las ayudas para estas piezas: `elegirOpcion` (en lugar
de `selectOption`), `esperarOpcion`, `textosOpciones` y `abrirOpciones` para el `Select`, y
`escribirFecha`, `escribirHora` y `esperarValorCampo` para los segmentos de fecha y hora, y
`elegirSugerencia` para tocar una opción del `Autocompletar` (su lista no es modal y React Aria la
cierra si la página se desplaza: espera a que la opción se vea entera).

Un `CampoFecha` avisa cada vez que una parte (día, mes o año) deja una fecha válida, no solo al
terminar como el `<input type="date">`: un filtro que lleva la fecha a la URL no debe volver a
montar el campo con cada cambio (perdería el foco a medio escribir; ver `Periodo` en Reportes).

## Nuevo diseño del panel (nov. 2026, hecho)

Diseño aprobado por el PM («Web-App»): una sola estructura para todo el panel y estilo sobrio
(texto de 13–14 px, bordes de 1 px, radio de 8–12 px; el color solo para acciones y estados).
Reemplaza lo que dicen las fases 1 y 3 donde no coincida.

- **Colores del diseño → tokens existentes** (sin colores nuevos): lienzo `bg-background-secondary`
  (`--hx-bg-muted`), bloques `bg-card` con `border-border` y `rounded-xl` (12 px), relleno tenue
  `bg-elevated` (`--hx-bg-elevated`), marca `primary` (teal 700 / 400), estados `success`,
  `warning`, `error`, `info` con fondo al 10 %. Bloques con el componente `Bloque`
  (`componentes/bloque.tsx`).
- **Barra lateral** (`lib/menu.ts`): la operación arriba sin título visible (Inicio, Tickets,
  Órdenes y tareas, Agenda, Mapa, Técnicos), después **Negocio** (Comercial, Encuestas, Reportes),
  **Administración** (Usuarios, Administración) y **Mis vistas** (las vistas guardadas de la
  bandeja, `/tickets?vista=`). Tickets lleva su contador (`GET /tickets/resumen`, cada minuto). La
  empresa va arriba y la cuenta (iniciales, nombre y rol), el tema y «Contraer» al pie. Despacho no
  tiene entrada: se abre desde el Mapa («Rutas y despacho») y con el buscador.
- **Barra superior:** migas a la izquierda (en escritorio), buscador global (Ctrl/⌘ + K), «Avisos»
  (punto rojo solo con SLA vencido o en riesgo; el panel lista lo urgente) y «Crear» (menú con
  Nuevo ticket, Nueva orden y Nuevo prospecto, según permisos).
- **Teléfono:** la barra inferior sigue (Inicio, Tickets, Órdenes, Agenda, Más) y «Más» es la
  barra lateral entera en una hoja, con sus contadores y «Mis vistas».
- **Inicio:** 4 indicadores con tendencia de 7 días (Tickets activos, SLA en riesgo, Órdenes de
  hoy, SLA cumplido), «Necesita tu atención» (tickets con SLA en riesgo o vencido, con «Asignar» o
  «Abrir», y órdenes sin técnico con «Programar»), «Equipo hoy» (avance y estado de cada técnico:
  `lib/inicio/equipo.ts`), «Tickets por día» y «SLA por categoría» (`reportes.ver`). Cada bloque
  aparece solo con su permiso y, si su lectura falla, dice «Reintentar» sin números.
- **Tickets:** lista a todo el ancho; pestañas de vistas con contador (Activos, Asignados a mí, Sin
  asignar, En espera, Todos y las propias), filtros como chips (listas `Selector`) y, con
  `tickets.asignar`, selección múltiple con «Asignar…» (una acción por ticket, con su `version`; si
  alguno falla, se dice cuál).
- **Ficha del ticket:** barra de acciones (`toolbar`): Asignar, Escalar, Crear orden, Resolver
  (principal) y «⋯» (poner en espera, reanudar, reabrir, fusionar, cerrar). Cada acción es un
  **diálogo corto** (`DialogoProtegido`) con telón. Pestañas **Conversación · Órdenes · Historial**;
  responder y la nota interna van en el redactor al pie de la conversación (pregunta antes de
  salir con un mensaje sin enviar). A la derecha, solo «Detalle» (responsable, categoría, SLA…) y el
  cliente. `?accion=asignar` abre el diálogo al entrar (desde Inicio).
- **Agenda:** pestañas Día por técnico · Semana · Mes. El mes marca hoy, dice por día cuántas
  órdenes hay, conflictos, ausencias y órdenes sin técnico; al elegir un día su resumen aparece al
  lado (en el teléfono, arriba) con «Abrir el día» y «Programar». Día y semana no cambian
  (arrastrar para programar, «Mover a…»).
- **Mapa** (`/mapa`, `agenda.ver` o `campo.ver_posiciones`): MapLibre con teselas OSM, lista con
  Técnicos · Órdenes · Red, leyenda (en sitio, en camino, sin señal, orden sin asignar) y tarjeta
  del técnico (orden actual, siguiente, «Asignar orden» y «Ruta»). «Red» queda vacía hasta que haya
  una lectura de la red de Horus (`.claude/integration/i3.md`).

## Bandeja de conversaciones (nov. 2026)

Diseño aprobado por el PM («compactar menús, mejor integración y que nada se vea cortado»):

- **Ancho completo:** en `/conversaciones` la barra lateral pasa a riel de íconos (sin tocar la
  preferencia guardada; «Expandir menú» la abre mientras sigas ahí) y la bandeja ocupa todo el
  alto bajo la barra superior (`esAnchoCompleto` en `componentes/panel.tsx`).
- **Tres columnas** con consultas de contenedor (`@container`), no de ventana: lista · conversación
  · ficha desde 1100 px de bandeja; debajo, la ficha va en una hoja; debajo de 700 px, lista **o**
  conversación (con «Atrás») y la barra inferior se oculta dentro de la conversación.
- **Lista:** dos líneas por fila con elipsis, punto de color del número sobre el avatar
  (`tonoDeCanal`: soporte = marca, cobros = alerta, avisos = info, ventas = éxito), punto de SLA,
  iniciales de quien la tiene y no leídos. Filtros en **un solo botón con contador**
  (`PanelEmergente`); segmentado Mías / Sin asignar / Todas con su cifra (`GrupoSegmentado` con
  `contador`). Todo va en la URL (`?c=41&vista=mias&canal=4`).
- **Conversación:** cabecera de una línea con el grupo [Asignado ▾ | Ayuda ▾ | Resolver | ⋯]; en
  el teléfono solo [Asignado][⋯]. Burbujas, notas internas en amarillo punteado y eventos de sistema
  centrados. Compositor único (Responder / Nota interna, tiempo de la ventana de 24 h, adjuntar,
  manuales, plantillas, respuestas rápidas y emoji, «Enviar» en tinta). Enter envía, Mayús + Enter
  baja de línea. Fuera de la ventana solo ofrece plantillas aprobadas, explicado en una línea.
- **Tiempo real:** `lib/conversaciones/tiempo-real.ts` (flujo SSE leído con `fetch` y sondeo con
  el mismo cursor). **Antes de abrir el flujo, de sondear, de guardar un borrador y de enviar se
  verifica con `GET /yo` que la sesión del navegador sigue siendo de quien está en pantalla**
  (`lib/conversaciones/sesion.ts`); si cambió, nada sale y la pantalla pide recargar.
- **Fase 1.1 (nov. 2026, contrato §14):**
    - **Disponibilidad:** la cuenta al pie de la barra lateral (o el riel) lleva el punto verde o
      gris y abre un panel con Disponible / No disponible (`GrupoSegmentado`), la línea «Pasas a
      No disponible tras N min sin actividad, al cerrar sesión o fuera de tu horario» y las
      opciones de la cuenta. En el teléfono va arriba de «Más». Mientras estás disponible y activa
      (pestaña a la vista y teclado, puntero o desplazamiento en los últimos 90 s) el panel manda
      un latido como mucho cada 60 s (`lib/conversaciones/latido.ts`); al cerrar sesión, «No
      disponible» en el servidor antes de cerrarla.
    - **«¿Alguien la atiende?»:** aviso naranja con la cuenta regresiva hasta que contesta el bot
      y «La atiendo» (`POST …/tomar`), en la fila de la lista y arriba de la conversación. Cada
      conversación que espera dice «2 disponibles en Soporte» o, en naranja, «Nadie disponible en
      Cobros». Los eventos del bot van en el hilo con el icono `Bot` y los mensajes del bot con
      «Bot · mensaje automático» y borde punteado de información.
    - **«Pasar a…»** (menú «⋯», para quien la tiene y para supervisores): diálogo con los
      compañeros de la cola (punto, «N conversaciones abiertas») y una nota opcional. Alguien «No
      disponible» se puede elegir: se avisa «La verá cuando vuelva a estar disponible».
    - **«Nueva conversación»** (`/conversaciones/nueva`, «Nueva» en la cabecera de la lista):
      contacto (clientes de Argos y contactos de Iris juntos; sin permiso de mensajes, deshabilitado
      con el motivo), número de salida de tus colas, plantilla aprobada y sus variables, vista
      previa en vivo y «Enviar y abrir conversación» con id de envío idempotente (`useIdEnvio`).
    - **`ListaEleccion`** (`@iris/ui`): radios en filas con contenido propio (persona con su
      disponibilidad, contacto con su permiso), deshabilitadas o atenuadas.
- **Pruebas de «nada cortado»:** `e2e/desbordes.ts` (`esperarSinDesbordes`) falla si la página
  se desplaza a lo ancho, si algo queda fuera de la ventana o si un texto se sale de su caja sin
  elipsis.

## Chat del equipo en el panel (nov. 2026)

Diseño aprobado por el PM (combinación de las opciones A y B; `contratos/chat.md`, parte del panel):

- **Tres lugares, un solo chat:** el mismo `MotorChat` de `@iris/sync/chat` (caché en memoria,
  flujo SSE leído con `fetch` para mandar `X-Iris-Empresa`, sondeo de `/cambios` si el flujo no se
  sostiene) y los mismos componentes de hilo y compositor (`componentes/chat/`):
    - **ícono en la barra superior** (burbuja redonda) con el número de lo que pide actuar
      (`GET /resumen`: directas sin leer + menciones) o un punto si solo hay mensajes nuevos; abre el
      **`PanelLateral`** flotante (lista y conversación se turnan con «Atrás»; «Abrir en página»
      lleva a `/equipo?c=<id>` con la misma conversación). Cerrarlo y volver a abrirlo conserva la
      conversación, el filtro, la búsqueda y lo escrito sin enviar (borrador por conversación, en
      memoria). Lo prueban `e2e/chat-equipo.spec.ts` («panel flotante»: ancho de la página igual,
      clic en la agenda de atrás, «Cerrar» a la vista a 1280 y 1440 px) y las capturas sobre agenda,
      mapa y órdenes (`e2e/capturas-chat.spec.ts`);
    - **página «Equipo»** (`/equipo`, en el riel con su icono, `chat.ver`): ancho completo como la
      bandeja, lista con Todas / Directas / Cuadrillas / Órdenes (con su contador), búsqueda y el
      hilo; en el teléfono, lista **o** hilo;
    - **pestaña «Chat del equipo»** en la ficha de la orden: lee el hilo sin unirse
      (`GET /ordenes/{id}/conversacion`); si no existe, «Abrir el chat de la orden». A la derecha,
      «En este chat» (miembros con su motivo) y «Agregar persona» (`chat.supervisar`, diálogo de un
      campo).
- **Se distingue de la bandeja de clientes:** burbujas propias en teal relleno; avatares cuadrados
  con icono para cuadrillas y órdenes; la franja «Chat interno: el cliente no lo ve»; nombre y rol
  de quien escribe; «visto» (no «leído»).
- **Insignias (§10):** número solo en directas y en menciones; una cuadrilla con mensajes nuevos es
  un punto.
- **«En vivo»** es un punto y dos palabras. Si conectar tarda más de 4 s: «Reconectando…» y un aviso
  «Sin conexión en vivo» con «Reintentar».
- **Menciones con @:** sugerencias de los miembros (`ListaSugerencias`); el mensaje sale por la cola
  del motor con `menciones: [ids]` (los `@Nombre` que siguen en el texto). Los `@Nombre` de los
  miembros se resaltan; el tuyo, con fondo.
- **Eliminar un mensaje propio:** sin confirmación; se ve eliminado y el aviso trae «Deshacer» 5 s.
  La llamada sale a los 5 s por la cola del motor (que verifica la sesión). Al ocultar la pestaña o
  al «Cerrar sesión» (que la espera hasta 3 s) sale ya por la misma cola; solo si la página se
  descarga va con `keepalive` y la empresa del chat (sin poder comprobar `GET /yo`: si otra persona
  entró, el servidor la rechaza porque solo el autor elimina). El servidor no restaura (§3.1). El
  plazo de 15 min (menos 10 s) se revisa al pedir y al salir. Solo se resaltan las menciones que trae
  el mensaje, y la foto se pide siempre por la ruta armada con su id (`rutaDescargaAdjunto`).
- **Orden cerrada, fallida o cancelada:** solo lectura (`solo_lectura`), con el motivo en lugar del
  compositor y sin «Agregar persona».
- **Foto y ubicación:** en escritorio, deshabilitadas («Foto y ubicación, desde la app de campo»);
  en el ancho del teléfono, habilitadas (cámara del navegador y geolocalización, por la cola).
- **Nuevo mensaje:** diálogo con Persona / Cuadrilla, búsqueda en el servidor y «Ya existe».

## Manuales de inicio (nov. 2026)

Pedido del PM: la biblioteca trae ejemplos para un ISP con fibra (FTTH) e inalámbrico (WISP).

- «Cargar manuales de inicio» (`manuales.editar`) en `/administracion/manuales` abre un diálogo
  corto con las plantillas marcadas; las que ya están en la biblioteca (mismo título, sin importar
  mayúsculas ni acentos) salen deshabilitadas con «Ya está en la biblioteca».
- Se crean con el `POST /mensajeria/manuales` de siempre, uno por llamada, con
  `Idempotency-Key: manual-inicio:<empresa>:<código>:r<revisión>`; antes de crear se vuelven a leer
  los títulos. Avance con barra y «Creando 3 de 14…»; si algo falla, «Reintentar» sigue donde quedó.
  Al final, aviso con los creados y los omitidos.
- Los textos viven en `apps/web/lib/manuales/inicio/` como datos tipados (`PlantillaManual`): de
  «usted», cortos, con «Cuándo llamar a soporte», sin precios ni plazos y con los datos de la
  empresa entre corchetes («[Nombre del ISP]», «[número de soporte]»). La tecnología va en el título
  y como primera etiqueta (`general`, `ftth`, `wisp`). Si cambia un texto, sube su `revision`.
  `inicio.test.ts` los valida contra el esquema del formulario, los límites del servidor y la vista
  segura del Markdown.
- **Se crean «Del equipo»** (interna): primero se completan los corchetes y luego se pasan a «Para
  el cliente» desde «Editar». Mientras el cuerpo tenga marcadores `[…]` (`lib/manuales/marcadores.ts`;
  un enlace `[texto](url)` no cuenta):
    - la bandeja **no lo envía al cliente**: lee el cuerpo antes de «Enviar al cliente» y dice qué
      falta; «Usar en la respuesta» avisa qué cambiar en el texto;
    - la biblioteca lo marca «Falta completar datos de tu empresa» (lee cada cuerpo, de 4 en 4: el
      resumen de la lista no lo trae) y el formulario lo dice junto a «Quién lo ve».
- Con la carga empezada (o detenida por un error) la selección queda congelada. La biblioteca pide
  hasta 100 manuales; si llegan 100, dice «Se muestran los primeros 100» y pide usar la búsqueda.

## Números de WhatsApp (nov. 2026)

Diseño aprobado por el PM («Números de WhatsApp», contrato §1.1):

- **Lista:** columna «Conexión» tal como la calcula el servidor (`Canal.conexion`), con icono +
  texto + color: «Conectado» es solo un punto; «Token vencido», «Número no coincide» y «Sin permiso
  en Meta» en rojo (y «Credenciales» con borde rojo); «Webhook sin verificar» y «Meta no respondió»
  en naranja; «Sin probar» y «Desactivado» neutros. En el teléfono, tarjetas.
- **Agregar número** en una página con tres pasos numerados (en Meta · datos · webhook) y «Guardar
  y probar». Las casillas de «En Meta» son una ayuda y no cuentan como cambios. Los enlaces a la
  guía desde un formulario abren otra pestaña (no se pierde lo escrito).
- **Secretos:** `CampoSecreto` para pegarlos; guardados, solo «•••• guardado el …» con «Cambiar»
  (vacío = se queda el guardado). El verify token que genera Iris se ve una vez, en «Conectar»; luego,
  «Generar otro» (con confirmación: deja el webhook sin verificar).
- **Guía «Cómo conectar WhatsApp»** dentro del panel (`/administracion/numeros/guia`), en español
  llano y diciendo dónde pueden variar los menús de Meta.
- **Migas:** el número no tiene pantalla propia; su miga lleva el nombre sin enlace
  («Números de WhatsApp › Avisos › Credenciales») y «‹» en el teléfono vuelve a la lista.

## Conexión con Meta, QR y préstamos (nov. 2026)

Diseño aprobado por el PM («Conexión con Meta», contrato §15):

- **Conexión con Meta** (`/administracion/conexion-meta`, solo `mensajeria.conexion_meta`): tres
  modos. «App propia» (datos de la app, «Dominio permitido» para copiar, tarjeta de comprobaciones
  con «Volver a probar» y webhook de la app con el verify token oculto tras «Mostrar»); «Servicio
  central» (Iris Cloud, `https://cloud.iris.kns.gt`: hoy la tarjeta dice «Servicio central no
  disponible todavía»); «Gestionada» (solo lectura). Guardar y probar son llamadas separadas.
- **Agregar número** ahora elige primero: «Con QR (WhatsApp Business)» o «Con datos de la API»
  (`/nuevo/qr`, `/nuevo/api`).
- **QR:** el SDK de Facebook se carga solo en `/nuevo/qr` (`es_LA`) y se precarga para que
  `FB.login` corra dentro del clic. Los mensajes `WA_EMBEDDED_SIGNUP` solo se aceptan de
  `https://facebook.com`, `www.` y `web.`; el código se manda en cuanto llegan los ids (vive ~30 s).
  Pantallas: inicio, esperando a Meta, creando, error (cada motivo con su texto, «Reintentar» y, si
  sirve, «Usar datos de la API») y «Número conectado» con «Traer historial» (plazo de 24 h) y
  «Terminar de configurar» (un pedido sin respuesta de Meta en 10 minutos se puede repetir). Sin
  `IRIS_BASE_URL` (servicio central o gestionada), el error lo dice. No hace falta tocar la CSP (solo `frame-ancestors`, sin COOP).
- **Lista:** insignias «QR · WhatsApp Business» / «API», «Compartido con N», «Prestado por X»; aviso
  de días sin abrir la app en el teléfono y «Desconectado por Meta». Con `empresas.todas` el número
  prestado no se repite (se queda la entrada de la empresa activa).
- **Compartir** (`/[id]/compartir`): empresas con sus usos y cola; los cambios se guardan juntos;
  «Dejar de compartir» con confirmación y motivo, explicando adónde van las conversaciones.
- **Bandeja:** «Desde el teléfono» en los mensajes con `origen` telefono; «Pasar a otra empresa» solo
  para la dueña y solo a empresas con el número prestado (pasan los mensajes con el cliente, no las
  notas internas; quien la recibe ve «Pasada por <empresa dueña>»); conversación revocada = «Solo
  lectura». Al iniciar, un contacto con conversación abierta en otra empresa del número sale
  deshabilitado: «La atiende otra empresa que usa este número».

## Asistente de IA (nov. 2026)

Diseño aprobado por el PM («Asistente de IA», contrato `conversaciones.md` §16). **El asistente nunca
envía:** sugiere y la persona revisa y envía.

- **Tarjeta «Sugerencia del asistente»** encima del compositor, en escritorio y teléfono: icono
  `Sparkles`, solo teal (sin morados), fondo de marca al 6 % sobre la tarjeta y borde de marca al
  30 %. Lleva el texto, las fuentes como chips con icono + texto (Horus `WifiOff` en rojo, Argos
  `CircleCheck` en verde, manual `BookOpen`, historial `History`), «Usar» (primaria), «Otra» y, si
  viene, «Etiquetar «X»»; arriba a la derecha 👍/👎 (`ThumbsUp`/`ThumbsDown`, `aria-pressed`) y
  ocultar (`X`). Nota fija: «Solo la ves tú. «Usar» la pasa a tu caja de texto: la revisas y la
  envías tú.»
- **Estados:** `generando` (esqueleto de dos líneas, «Preparando sugerencia…»), `lista`, `error`
  (texto del servidor + «Reintentar», salvo tope y sin configurar, que solo explican). Una vencida,
  usada o descartada desaparece; en modo automático la nueva llega por el feed.
- **No tapa el hilo:** la cabecera de la tarjeta la pliega (`aria-expanded`) y el cuerpo se desplaza
  dentro (máx. 38 dvh), así en 390 px con el teclado abierto se sigue viendo el último mensaje. En el
  teléfono «Usar» y «Otra» van a medias y «Etiquetar» a lo ancho.
- **Modo botón:** «Sugerir» (`Sparkles`, fantasma en teal) junto a «Enviar» en lugar de la tarjeta.
  En pausa por el tope, una línea discreta y ningún botón.
- **Ficha del cliente:** «Resumen del asistente» arriba de Resumen, a pedido («Resumir»), con
  `Cargando` «Generando resumen…» y error con «Reintentar».
- **Administración › Asistente de IA › Conexión con la IA** (`/administracion/asistente/conexion`;
  desde la fase 3 la pantalla principal son las tres preguntas, ver más abajo): `FormularioPagina` con proveedor
  en `ListaEleccion` («Recomendado» en Ollama), clave como en las credenciales de números
  (`SecretoCambiable` con `femenino`: «•••• guardada el …»), modelo en `CampoSelector` con «Escribir
  otro modelo» (y a mano si la lista falla), «Probar conexión» con el resultado en una línea (icono +
  texto + color), `Aviso` informativo de privacidad con un proveedor en la nube, «Nunca» con `Ban` en
  rojo y el uso del mes con barra (`role="progressbar"`) que pasa a naranja al 80 % y a rojo en pausa.
- **Clave que no viaja (revisión de seguridad):** con una clave guardada, cambiar el proveedor o la
  dirección abre «Cambiar» solo y dice «Con otro proveedor u otra dirección hay que escribir la clave
  de nuevo» (en Ollama, «… o quitarla», con «Quitar clave», que manda `clave: ""`). Si el servidor
  responde 422 `clave_requerida` con el campo oculto, primero lo abre y después marca el error. El
  `detail` de 422 `url_no_permitida` va en «Dirección».
- **Modelos:** la lista de lo guardado se lee al abrir (`GET`); otra dirección o clave se consulta por
  `POST` al salir de «Dirección» o con «Cargar modelos», nunca con cada tecla. Si falla, dice por qué
  (el `detail` del servidor) con «Volver a cargar los modelos» y deja escribirlo a mano.
- **Demasiados pedidos (429):** la sugerencia y el resumen dicen «Demasiados pedidos. Vuelve a
  intentar en N s» (`Retry-After`) con la cuenta regresiva y «Reintentar» deshabilitado hasta
  entonces (`componentes/conversaciones/error-asistente.tsx`); «Reintentar» repite el mismo pedido
  («Otra» o «Sugerir»). Tras pasar la conversación a otra empresa, el 404 de la sugerencia es «sin
  sugerencia»: la tarjeta se va sin error.
- Pruebas: `e2e/asistente.spec.ts` (4 proyectos) y `lib/asistente/presentacion.test.ts`.

### Entrega 2.1: tope en la campana, saldo y diagnóstico del conector

- **Campana con avisos personales** (`GET /yo/avisos`, §16.9; hoy el tope del asistente para quien
  tiene `mensajeria.asistente_configurar`): arriba van los avisos con su icono (`Sparkles` para
  `asistente_tope`) y el `titulo` del servidor; `alerta` (80 %) en naranja, `peligro` (100 %) en rojo
  con la línea «Pausado hasta el 1 de <mes siguiente> o hasta que subas el tope.». Debajo, lo de
  tickets. Sin leer en negrita; abrirlo lo marca leído (`POST …/leer`) y lleva a `enlace` (solo rutas
  internas). «Marcar todo como leído» (`leer-todos`) con aviso emergente. El punto aparece con avisos
  sin leer y **crece** (`data-actuar="si"`, rojo con anillo) solo con un `peligro` sin leer. Si no se
  pudieron leer, lo dice con «Reintentar» y no pinta punto por ellos. Se relee cada minuto, al abrir y
  al cambiar de empresa. En error deja de sondear hasta «Reintentar»; con la sesión vencida (401)
  manda a entrar como «sesión expirada». El `enlace` se resuelve como el navegador y debe quedar en
  el mismo origen (sin caracteres de control ni `\`). Lógica: `lib/campana.ts`.
- **Uso del mes en pausa:** el aviso dice «Pausado hasta el 1 de <mes siguiente> o hasta que subas el
  tope.» (`textoPausado`).
- **Saldo de Argos en la sugerencia:** «Servicio activo, al día · Argos» en verde con `CircleCheck`;
  «Con saldo vencido (N facturas) · Argos» en rojo con `CircleAlert` (`tonoFuente`). Nunca montos.
- **Diagnóstico del conector de Argos** (Administración › Integraciones, en la tarjeta del conector
  de Argos activo, ancla `#diagnostico-argos`): «Versión de Argos» (`versionRemota` o «Sin leer»),
  «Última revisión» (hora de Guatemala), el resultado con `InsigniaEstado` (icono + texto + color:
  `ok` verde, `ruta_no_existe` naranja, `sin_permiso`/`no_responde`/`error` rojo, sin revisar neutro)
  y el `versionDetalle` del servidor en una línea. «Revisar ahora» (`integraciones.revisar_version`)
  manda `Idempotency-Key`; el servidor responde 202 y lee la versión en segundo plano, así que el
  panel avisa «Revisando la versión de Argos» y vuelve a leer la lista enseguida, a los 3 s y a los
  8 s. `conector_apagado` y `sistema_sin_version` se explican en palabras.
- **«Ver el conector de Argos»** (`componentes/equipo/enlace-conector-argos.tsx`, con `Stethoscope`)
  va dentro del aviso «Argos < 1.6.0: inventario por técnico no disponible» en técnicos sin ficha, en
  el selector de bodega y en el vínculo de la ficha. Sin `integraciones.ver` no aparece.
- Pruebas: `e2e/asistente-21.spec.ts` (4 proyectos), `lib/campana.test.ts` y `lib/integraciones/presentacion.test.ts`.

### Fase 3: el asistente en tres preguntas, categorías y boletas (nov. 2026)

Diseño «más amigable y práctico» del PM (contrato `conversaciones.md` §17 y §6.4; Argos 0.7.0 §7.7).

- **Administración › Asistente de IA** (`/administracion/asistente`) es una sola pantalla con tres
  preguntas en lenguaje llano y el interruptor de encendido a la derecha del título
  (`FormularioPagina` con `accionTitulo`):
    1. **¿Cuándo atiende solo?** `ListaEleccion` con icono de Lucide: «Nunca» (`Hand`), «Cuando no
       hay nadie» (`Moon`, píldora «Recomendado») y «Siempre primero» (`Zap`). Con «Cuando no hay
       nadie» aparece «Esperar a un agente: N min». Mientras el servidor diga
       `autonomoDisponible: false`, las dos últimas van deshabilitadas con la píldora «Próximamente».
    2. **¿Qué puede hacer solo?** Una fila por acción: icono, título, nota corta y el `Interruptor`
       a la derecha (`alFinal`). Con `autonomoDisponible: false` lleva la nota «Se aplicará cuando
       el asistente atienda solo». Debajo, «Ajustar límites ›» y la línea fija «Siempre pasa a una
       persona si el cliente lo pide, se enoja o no sabe la respuesta».
    3. **¿Qué hace con cada tipo de mensaje?** Una fila por categoría activa: su chip y un
       `Selector` «Responde solo / Sugiere al agente». Debajo, «Agregar tipo · Colas y tiempos de
       respuesta ›».
- **«Pruébalo antes de guardar»:** caja de marca al 6 % con `Sparkles`. Manda lo que hay en el
  formulario **sin guardar** y muestra el chip de la categoría, qué haría («Respondería solo», «Le
  sugeriría al agente», «Pasaría a una persona», con sus fuentes), el porqué cuando el servidor lo
  da, la respuesta en una burbuja y las acciones que tomaría, descritas. Enter prueba (no guarda).
  Estados: «El asistente está leyendo el mensaje…», error con «Reintentar» y 429 con la cuenta
  regresiva (`ErrorAsistente`). No envía nada a ningún cliente, y lo dice.
- **Sin proveedor conectado:** aviso arriba «Falta conectar la IA» con el botón «Conectar la IA»
  (lleva a `/conexion`); el interruptor, las tres preguntas, «Pruébalo» y «Guardar» quedan
  deshabilitados.
- **«Efecto en el SLA (este mes)»:** primera respuesta promedio (con «antes N» si el servidor lo
  sabe) y el % resuelto sin persona, con «(N de M)». El % no se calcula sin conversaciones («—»). Si
  no se pudo leer: «Reintentar», sin cifras.
- **Lo avanzado, cada cosa en su ruta** (`FormularioPagina`, migas, salir con cambios pregunta):
    - `/administracion/asistente/conexion`: proveedor, clave, modelo, probar conexión, fuentes, uso
      y tope (lo de la fase 2);
    - `/administracion/asistente/limites`: prioridad máxima del ticket, días adelante para visitas,
      mensajes antes de pasar a una persona y «Apagado por cola» (una casilla por acción: «No
      agendar visitas»). Los perfiles por horario que existan se listan y se conservan al guardar;
    - `/administracion/asistente/categorias`: lista con cola, tiempo de primera respuesta, manuales
      base y modo; «Agregar categoría» y «Editar» en su propia ruta (nombre, descripción, color del
      chip con vista previa, cola, tiempo, modo, condición, manuales base y activa). «Otro» no se
      desactiva.
  Guardar en una de estas pantallas no pisa lo de las otras: cada una manda solo lo suyo.
- **Chip de categoría** (`componentes/asistente/chip-categoria.tsx`): icono `Tag` + nombre + color.
  Los tonos del servidor van a los colores de Horus: `info` azul, `error` rojo, `ok` verde, `aviso`
  naranja, `neutro` gris. Un tono que el panel no conoce se ve gris.
- **Bandeja:**
    - arriba de la lista, **filtros por categoría** con su cifra: «Todas» primero y la elegida en
      teal con `CircleCheck` (`aria-pressed`). Es una tira que se desliza a lo ancho si no cabe. El
      filtro va en la URL (`?categoria=2`) y «Quitar filtros» del popover no lo borra. La tira solo
      aparece cuando alguna conversación ya tiene categoría (sin asistente no hay categorías);
    - cada fila suma una tercera línea con el **chip** y la **actividad del asistente** (`Sparkles`
      en teal + el texto corto que escribe el servidor);
    - bajo la cabecera de la conversación, la categoría con **«Cambiar»** (menú corto; con
      `conversaciones.categorizar`) y aviso emergente al cambiarla. Si otra persona cambió la
      conversación (409), se relee y dice «Alguien la cambió; inténtalo de nuevo» (igual en «Tomar»). Sin categoría y con el asistente
      activo dice «Sin categoría · Poner categoría»; con el asistente apagado no aparece nada;
    - **«El asistente está atendiendo»**: franja discreta en teal con `Sparkles` y el botón
      «Tomar» (la acción de tomar de siempre: al tomarla una persona, el asistente se detiene);
    - los mensajes del asistente llevan la etiqueta **«Asistente»** con `Sparkles` en teal y borde
      de marca (distintos de los de una persona y de «Bot · mensaje automático»); su nota al pasar
      a una persona es una nota interna que dice «Nota interna del asistente».
- **Boletas en la ficha del cliente** (sección «Boletas de pago», arriba de «Creado desde aquí»):
  monto, referencia, estado con `InsigniaEstado` (icono + texto + color), quién la decidió y, si no
  se le pudo avisar al cliente, lo dice. Pendiente de revisar y con permiso: «Aprobar y registrar
  pago» (diálogo que confirma el monto, con nota opcional) y «Rechazar» (diálogo con el motivo y la
  causa). Cada error dice qué hacer:
    - sin vínculo: «Vincula tu usuario de Argos para aprobar pagos» con el botón «Vincular mi
      usuario de Argos» y, después, «Reintentar» (misma clave);
    - sin permiso, sin empresa o usuario inactivo en Argos: se explican, sin «Reintentar»;
    - ya la decidió otra persona: se relee y la tarjeta dice quién;
    - Argos no respondió: «Puede haberse aplicado en Argos: vuelve a intentar para confirmarlo» (nunca
      «no quedó nada a medias»); Argos viejo: «Actualiza Argos a 1.15.0 o posterior»;
    - **todo 409 relee la boleta** antes de decir nada. Si no se pudo leer, la tarjeta no muestra un
      estado: dice «Cambió en Argos y no se pudo leer cómo quedó», con «Reintentar», y los botones
      de decidir quedan deshabilitados hasta leerla;
    - al aprobar viaja el monto que se ve en el diálogo (`montoEsperado`). Si cambió, el servidor no
      aprueba: el diálogo sigue abierto con el monto nuevo y pide confirmar otra vez. El aviso de
      éxito usa el monto que devolvió el servidor;
    - el diálogo de rechazar lleva la nota «Al cliente le llega un texto fijo según la causa. Tu
      motivo se agrega solo si no lleva números, puntos ni enlaces.»;
    - si Argos avisó y todavía se confirma: «Confirmando con Argos…» (discreto). Si no se pudo:
      «No se pudo confirmar con Argos», con icono de aviso;
    - cerrar el diálogo después de un error vuelve a leer las boletas. Un refresco que falla no tapa
      lo leído: se marca «No se pudieron actualizar» con «Reintentar».
- **«Abrir en Argos»** (con `integraciones.abrir_argos`) reemplaza a «Ver en Argos» en la ficha del
  cliente de tickets y órdenes: entra a Argos ya con la sesión. La pestaña se abre en el mismo clic
  (en blanco, sin `opener`) y se manda al enlace cuando llega; así el navegador no la bloquea. El
  enlace lleva un ticket de sesión: **solo se abre por `https` y en el origen del panel de Argos del
  conector** (`urlPublica`); si no, la pestaña se cierra y lo dice. Con las ventanas emergentes
  bloqueadas no se pide el ticket: «Permite las ventanas emergentes para abrir Argos». En una sesión
  abierta desde Argos pide antes la contraseña de Iris (el diálogo «Confirma tu contraseña» de
  siempre) y después abre Argos solo. Si falla, lo dice y deja el enlace simple de respaldo. **«Vincular mi usuario de Argos»** está en el
  menú de la cuenta y en «Mi perfil» (`/cuenta#argos`).
- Pruebas: `e2e/asistente-3.spec.ts` y `e2e/boletas.spec.ts` (4 proyectos),
  `lib/asistente/fase3.test.ts` y `lib/conversaciones/boletas.test.ts`.

### Fase 3b: el asistente atiende solo (nov. 2026)

Contrato: `.claude/integration/contratos/conversaciones.md` §17.8, §17.8.1 y §17.8.2. Lógica pura en
`apps/web/lib/asistente/fase3b.ts`.

- **Pregunta 2 en dos bloques** (`componentes/asistente/acciones.tsx`, la usan la pantalla simple y
  el editor de perfiles): primero lo de todos los días y, aparte, «Con dinero o con el servicio»
  (recibir boletas, dar el saldo, promesas, reconexión). Lo de dinero viene **apagado** y lleva un
  `Aviso` de información que dice en llano que antes se verifica la identidad (DPI o NIT con
  Argos). Debajo de cada acción va lo que se le pide al cliente (`requisitoDe`). Solo «Dar el saldo»
  se puede bajar a «con solo su número» (una `Casilla` que aparece al encenderlo:
  `exigeVerificacionFuerte.dar_saldo: false`).
- **Lo avanzado va plegado.** En «Límites», la sección «Avanzado» es un botón con `aria-expanded`
  que resume los valores («Gasta hasta el 70 % del tope… · la identidad vale 1 h») y se abre sola si
  un campo suyo tiene un error. Cada campo trae su ayuda y su valor de fábrica.
- **Perfiles por horario** (`/administracion/asistente/perfiles`, `/nuevo`, `/[codigo]/editar`):
  lista con «El de siempre», resumen («4 acciones encendidas · 1 de dinero») y dónde se usa; el
  formulario va en su pantalla. El código lo arma el panel con el nombre (`codigoDePerfil`): la
  persona no lo ve ni lo escribe, y no cambia al editar. El general no se quita. Quitar un perfil
  pide confirmación y dice qué horarios lo usan.
- **Horarios:** cada franja (y cada feriado abierto) elige «qué hace el asistente»: lo de siempre,
  solo sugerir u otro perfil. El selector solo aparece si el asistente atiende solo, hay más de un
  perfil o la franja ya tiene uno; sin permiso para leer los ajustes, `perfil` se conserva tal cual.
- **Verificación del cliente** (`componentes/conversaciones/verificacion.tsx`): punto + icono +
  texto, en la cabecera de la conversación y en la ficha. `fuerte` = «Identidad verificada» (verde),
  `numero` = «Su número coincide» (gris), `null` = «Sin verificar» (ámbar). **Sale solo de
  `contacto.verificacion`**: el panel nunca lo deduce de una acción propia (baja sola de `fuerte` a
  `numero`, y confirmar el cliente de una conversación resuelta no lo sube). Si el servidor no manda
  el campo no se muestra nada. En el teléfono el indicador ocupa el lugar del número (queda en la
  ficha).
- **Relevo a una persona:** la nota interna del asistente lleva antes una píldora teal «El asistente
  la pasó a una persona · hora», y la franja de la conversación dice «El asistente la pasó a una
  persona: espera en la cola» con «Tomar» mientras nadie la tenga.
- **Mensajes de sistema nuevos:** `reconexion_pedida` («Reconexión temporal: Argos la aceptó (la
  pidió …)», con lo que decidió Argos primero porque la línea se corta por el final) e
  `identidad_verificada`, con texto fijo por `detalle` (`textoIdentidad`). El escudo con la marca
  (`ShieldCheck`) solo sale con `coincide` y `supervisor`; un resultado o un tipo que el panel no
  conoce se pinta neutro: el panel no afirma nada que no venga en el dato.
- **Motivo del relevo:** con `asistente.motivoPaso` la franja dice «…la pasó a una persona porque
  <motivo en llano>» (`textoMotivoPaso`); un motivo desconocido o ausente deja el texto sin motivo.
- **Vigencia de la identidad:** con `contacto.verificacionVenceEn` el indicador baja a esa hora en
  el acto (bajar es lo seguro) y se relee el detalle; además, mientras el nivel sea `fuerte`, la
  conversación abierta relee cada 30 s (de fondo: si falla conserva lo que hay; no pide nada con la
  pestaña oculta).

### El asistente agenda visitas (nov. 2026)

Contrato: `.claude/integration/contratos/conversaciones.md` §17.10. Lógica pura en
`apps/web/lib/asistente/visitas.ts`.

- **Pantalla simple:** «Agendar visitas» ya no dice «Próximamente» y viene apagado de fábrica. Al
  encenderlo aparece debajo el bloque «Cómo agenda» (`resumenVisitas`): tres líneas en llano con lo
  guardado (qué agenda y en qué horarios, con cuánta anticipación y hasta cuántos días, y las
  reglas) y el enlace «Cambiar cómo agenda». El resumen cuenta los días que ofrece **de verdad**
  (los acorta «Días adelante» de Límites).
- **Cómo agenda** (`/administracion/asistente/visitas`, pantalla propia): «Qué visitas agenda»
  (casillas), «Horarios que ofrece» (de 1 a 6, con anticipación y días adelante), «Reglas» (tres
  interruptores con su consecuencia debajo: «Apagado: …») y «Avanzado» plegado con los cuatro topes,
  cada uno con ayuda corta y su valor de fábrica. El 422 del servidor va junto al campo que nombra
  (`campoDeErrorVisitas`) y abre lo avanzado si es de ahí.
- **El bloque `visitas` se guarda completo** y solo desde esa pantalla. Si el servidor no lo manda,
  ni el bloque ni el formulario aparecen: el panel no inventa valores. Las demás pantallas (simple,
  perfiles) conservan `exigeVerificacionFuerte.agendar_visita`. «Pedir DPI o NIT antes de agendar»
  es una regla de la empresa: se guarda en **todos** los perfiles y un perfil nuevo la hereda.
- **Orden creada por el asistente:** en la ficha de la orden, la insignia neutra «Agendada por el
  asistente» (`InsigniaOrigenOrden`); los demás orígenes no llevan marca. En la conversación, el
  mensaje de sistema «Asistente de IA creó OT-…» y el vínculo a la orden en la ficha; en la lista,
  la actividad «Agendó la visita OT-…» (texto del servidor: nunca el técnico).

## Correo del sistema (nov. 2026)

Contrato: `.claude/integration/contratos/correo-sistema.md`. Pantalla: `/administracion/correo`
(`correo_sistema.ver` / `.configurar`). Lógica pura en `apps/web/lib/correo/correo.ts`.

- **Arriba, qué se usa hoy** (`textoOrigen`): un `Aviso` con el origen en llano (el de esta
  empresa, el general, el del servidor o «Iris no envía correos todavía; los avisos llegan por la
  campana»). «Ninguno» es un aviso, no un error. En el mismo aviso van «Encendido / Apagado» y su
  botón, en su propia fila para que el título no se apriete en el teléfono.
- **Atajos de proveedor** (Google Workspace, Microsoft 365, Otro): botones que **solo rellenan**
  servidor, puerto y seguridad; se marca el que coincide con lo escrito y debajo va una nota corta
  (p. ej. «usa una contraseña de aplicación»).
- **Contraseña:** nunca viene del servidor. Con `tieneContrasena`, el campo va vacío con «Guardada.
  Escribe otra para cambiarla» y una casilla «Quitar la contraseña guardada». Sin tocarla no viaja.
  La guardada no se manda a otro servidor: si cambian el servidor, el puerto o el usuario, el panel
  pide escribirla otra vez **antes** de guardar o probar (`faltaContrasena`). Sin usuario, avisa
  junto al campo que la guardada se borra al guardar.
- **Reglas del servidor (422) en llano** (`reglaDeCorreo`): cada código conocido va con las palabras
  del panel junto a su campo; lo que no es de un campo (falta la clave de cifrado de la instalación)
  va como aviso general, sin nombres de variables del servidor.
- **Contrato 1.1.0:** «Sin cifrado» solo se ofrece en el correo general (`seguridadesDe`); un 429
  al guardar o al probar dice por qué y cuánto esperar (`textoDemasiados`); mientras una prueba
  tarda (un servidor que no contesta), el diálogo dice «Probando…» y no se corta.
- **Detalles que escribe el servidor se muestran tal cual:** `origenDetalle` bajo el título del
  origen y `detalle` del resultado de la prueba.
- **Correo de prueba:** un diálogo corto (un campo: el destinatario, por defecto el correo de quien
  prueba) que manda lo del formulario **sin guardar**. Si el formulario no es válido, el diálogo no
  se abre y se marcan los campos. El resultado va con icono + texto + color y la causa en llano
  (`textoResultado`); con 429, cuánto esperar.
- **Encender y apagar** piden confirmación y dicen qué pasa después (por dónde saldrán los avisos).
  Con cambios sin guardar el botón queda deshabilitado.
- **Ámbito:** quien no tiene límite de empresa ve un `GrupoSegmentado` «General» / «Solo <empresa>»
  (`?ambito=empresa`), deshabilitado mientras haya cambios sin guardar; los demás ven solo el de su
  empresa, diga lo que diga la URL.
- **Errores:** 409 con «Recargar» (trae lo guardado), 422 junto al campo, lectura fallida con
  «Reintentar» y sin formulario. Con solo `correo_sistema.ver`, todo deshabilitado y sin prueba.

## Llamadas de WhatsApp en el panel (voz E1, nov. 2026)

Contrato: `.claude/integration/contratos/voz.md` §3. Código: `apps/web/lib/voz` (máquina de estados,
API, WebRTC) y `apps/web/componentes/voz`. El proveedor (`ProveedorVoz`) va en el layout del panel,
solo para quien tiene `llamadas.contestar`: el aviso y la barra aparecen en cualquier pantalla.

- **«Recibir llamadas»** es un interruptor por persona **y navegador**: en escritorio, el teléfono
  de la barra superior (un punto verde si está encendido; crece en ámbar solo si hay que actuar);
  en el teléfono, arriba de «Más». Al encenderlo, Iris explica **antes** para qué es el micrófono y
  después el navegador pregunta. Estados que se dicen en llano: micrófono bloqueado (cómo
  permitirlo), sin micrófono, sin conexión en vivo, «No disponible» y «este navegador dejó de
  recibir» (el servidor guarda 10 por persona) con «Volver a encender».
- **Llamada entrante:** tarjeta flotante bajo la barra superior (ancho completo en 390 px) con el
  cliente o «Número desconocido» (nunca un nombre inventado), el número al que llaman, su servicio
  y el ticket de la conversación si el servidor los manda, «Rechazar» y «Contestar». Suena un
  timbre (WebAudio). «Contestar» abre la conversación del cliente, salvo que estés en un
  formulario (no se pierde lo escrito: queda «Ver cliente» en la barra).
- **Si no se pudo contestar** (otra persona la tomó, venció el tiempo, micrófono, red, ya tienes
  otra llamada): un mensaje llano que se va solo a los 8 s. Si la tienes en otra pestaña, el aviso
  solo se cierra. Nunca un código ni un error técnico.
- **Llamada en curso:** barra bajo la barra superior (`data-slot="barra-llamada"`), en todas las
  pantallas: punto, nombre, tiempo, «Silenciar» y «Colgar» (rojo). Las pantallas de alto completo
  le ceden su alto (`--alto-llamada`).
- **Al terminar:** diálogo corto con la nota (opcional, queda como nota interna), «¿Cómo quedó?» y
  los accesos «Crear ticket» y «Agendar visita». «Ahora no» cierra. Si se cortó, lo dice.
- **En el hilo:** la llamada es una tarjeta centrada con icono + texto + color: contestada (verde),
  rechazada (neutro), fuera de horario y fallida (ámbar), perdida (rojo, la única que pide actuar:
  «Escribirle» y, si sigue pendiente, «Descartar»). El audio del buzón lleva «Mensaje de voz del
  buzón». No hay «Devolver la llamada» hasta que existan las salientes (E1b).
- **En la lista:** la fila con una llamada por devolver lleva el chip rojo «Llamada perdida ·
  escríbele», y arriba se juntan como «N llamadas por devolver» (plegado es una línea).
- **Ajustes:** `/administracion/numeros/[id]/llamadas` (requisitos de Meta como «Listo / Falta /
  Recomendado / Sin comprobar», «pendiente de aplicar en WhatsApp» con «Reintentar», a quién le
  suena, horario y llamadas perdidas; un número por QR solo muestra por qué no puede) y
  `/administracion/llamadas` (de la empresa). **La grabación no se ofrece.**
- **Pruebas:** `lib/voz/*.test.ts` (máquina, textos, SDP, ajustes) y `e2e/voz.spec.ts` con
  `e2e/api-voz.ts`, `e2e/doble-webrtc.ts` (micrófono y `RTCPeerConnection` de mentira) y
  `page.clock` para los plazos de 12 s y 5 s.

## Navegación del panel (fase 1, hecha)

- **Barra lateral por grupos:**
    - Operación: Inicio, Tickets, Órdenes, Agenda, Despacho;
    - Equipo: Técnicos, Usuarios;
    - Ventas: Comercial, Encuestas;
    - Análisis: Reportes;
    - Administración.

  Se filtra por permiso (`lib/menu.ts`, `menuAgrupado`). «Contraer menú» la deja en solo íconos,
  con un globo en cada uno; la preferencia se guarda en este navegador (`localStorage`, con
  `try/catch`).
- **Migas en cada pantalla** (`lib/migas.ts` + `componentes/migas-panel.tsx`):
    - salen de la ruta, con el nombre del registro cuando la ficha ya lo leyó
      (`useNombreEnMigas("OT-000042")`);
    - reemplazaron los enlaces «← Volver»;
    - una pantalla nueva agrega su etiqueta en `lib/migas.ts`.
- **Buscador global**, con Ctrl/⌘ + K o el botón «Buscador global» de la barra superior:
    - busca pantallas por permiso;
    - busca tickets, órdenes y prospectos desde 2 letras, y clientes de Argos desde 3;
    - usa los listados de siempre con `q` y `limit`, sin endpoints nuevos;
    - un cliente lleva a sus tickets (`/tickets?clienteId=`); la bandeja lee el nombre de la API y muestra «Cliente #id» mientras tanto o si falla. Las respuestas se guardan por empresa y se olvidan al cerrar el buscador.
- **Avisos emergentes:**
    - van después de cada acción que cambia algo, con el texto en `lib/avisos.ts`
      («Orden OT-000042 asignada a Luis Pérez»);
    - salen abajo a la derecha en escritorio; en el teléfono, dentro de la barra superior (hasta
      dos líneas, sin tapar el buscador), para no tapar la miga «‹ …», la barra inferior,
      «Guardar» ni las hojas;
    - donde la pantalla ya confirmaba en línea con detalle (mover una cita, publicar una ruta,
      guardar un rol o una plantilla, conectar Argos), se queda esa confirmación y no se duplica;
    - los errores siguen en la pantalla.
- **Teléfono (debajo de `lg`):**
    - barra inferior con Inicio, Tickets, Órdenes, Agenda y «Más», con el área segura;
    - «Más» abre una hoja con el resto del menú, la empresa, la cuenta (como menú) y el tema;
    - ya no hay hamburguesa.

## Formularios en pantalla propia (fase 2, hecha)

- **Cada formulario de más de 3 campos tiene su ruta** y usa `FormularioPagina`:

  | Antes en diálogo | Ahora | Permiso |
  | --- | --- | --- |
  | Nuevo ticket | `/tickets/nuevo` | `tickets.crear` |
  | Nueva orden | `/ordenes/nueva` | `ordenes.crear` |
  | Crear orden (y asignar) desde el ticket | `/ordenes/nueva?ticket=<id>` | `ordenes.crear` (y `tickets.crear_orden` para asignar) |
  | Nuevo usuario · Editar usuario | `/usuarios/nuevo` · `/usuarios/[id]/editar` | `usuarios.crear` · `usuarios.editar` |
  | Nuevo técnico · Editar técnico | `/tecnicos/nuevo` · `/tecnicos/[id]/editar` | `agenda.crear_tecnico` · `agenda.editar_tecnico` |
  | Ausencia (7 campos) | `/tecnicos/[id]/ausencias/nueva` | `agenda.registrar_ausencia` |
  | Nuevo prospecto · Cotización | `/comercial/nuevo` · `/comercial/[id]/cotizacion` | `prospectos.crear` · `cotizaciones.crear` |
  | Buzón de correo | `/administracion/buzones/nuevo` · `/[id]/editar`, en dos pasos (Recibir · Enviar) | `buzones.crear` · `buzones.editar` |

- **Qué trae cada pantalla:**
    - se llega por un enlace que se ve como botón, con el mismo texto de antes («Nuevo ticket»);
    - el formulario se nombra con su título (`getByRole("form", { name: "Nuevo ticket" })`);
    - las migas salen de la ruta (`lib/migas.ts`); en editar, la ficha lleva el nombre del registro
      («Usuarios › Ana López › Editar»);
    - «Guardar» siempre visible al pie; si falta una selección obligatoria se deshabilita y dice qué
      falta («Falta elegir el técnico o la cuadrilla para guardar.»). Los textos obligatorios se
      validan al enviar, junto al campo;
    - un 422 se muestra junto a su campo (`erroresACampos`); lo que no tiene campo va arriba;
    - al guardar: aviso emergente y la ficha del registro (`router.replace`). Un buzón no tiene
      ficha: vuelve a la lista;
    - salir con cambios pregunta antes: enlaces y migas, «Cancelar», «Atrás» y recargar. Al
      guardar, `liberarProteccion()` quita la entrada propia del historial antes de navegar, así
      que «Atrás» desde la ficha no vuelve a un formulario vacío;
    - un error que aparece (o desaparece) al salir de un campo no mueve lo que está bajo el
      puntero mientras está presionado (`Campo`, `CampoSelector` y `CampoArea` lo esperan hasta
      después del clic): el clic en el botón siguiente siempre llega.
- **Lo que llega por la URL nunca se muestra tal cual:** `?ticket=`, `?usuario=` y los `[id]` se
  aceptan solo como id numérico (`lib/parametros.ts`) y el registro se lee de la API. La orden
  desde un ticket muestra arriba ese ticket (número enlazado, cliente, servicio y dirección), así
  que un enlace con otro ticket se nota antes de guardar.
- **Nuevo usuario** muestra la contraseña temporal una sola vez en la misma pantalla (nunca en la
  URL ni en el historial). «Atrás» desde ahí sale a la lista; al ocultar la página (`pagehide`,
  bfcache) se borra; el campo usa `autocomplete="new-password"`. Desde «Nuevo técnico», «Crear usuario» lleva a
  `/usuarios/nuevo?para=tecnico` y «Continuar con la ficha» vuelve a `/tecnicos/nuevo?usuario=<id>`
  con ese usuario elegido.
- **Buzón:** si al guardar hay un error en el otro paso, se pasa a ese paso (`lib/buzones/pasos.ts`).
- **Teléfono:** mientras hay un formulario en pantalla, la barra inferior de navegación se oculta
  para no tapar «Guardar».
- **Siguen en diálogo:** confirmaciones, cambiar contraseña, reautenticar, «Mover a…», cuadrilla y
  zona, guardar vista, turnos, firmar contrato y las acciones del ticket y de la orden (pasan a un
  panel lateral en la fase 3).

## Fichas con pestañas y acciones en panel lateral (fase 3, hecha)

- **Pestañas en la URL** (`lib/pestanas.ts`, `usePestanaUrl`): `?pestana=` validada contra una
  lista fija; un enlace o recargar abren la misma, cambiar de pestaña reemplaza la entrada del
  historial y la primera no se escribe. Una pestaña elegida que no está (sin permiso) cae en la
  primera.

  | Ficha | Pestañas |
  | --- | --- |
  | Ticket | Resumen · Conversación · Órdenes · Historial (el cliente sigue a la derecha) |
  | Orden | Resumen · Evidencias · Formularios · Argos (con el número de pendientes abiertos) |
  | Técnico | Datos (con el vínculo con Argos) · Agenda · Ausencias |
  | Prospecto | Resumen · Factibilidad · Cotización · Contrato · Actividad |
  | Usuario | Datos · Roles · Empresas · Sesiones · Accesos |

  Volver de un formulario lleva a la pestaña de donde salió (cotización → «Cotización»; ausencia →
  «Ausencias»).
- **Acciones del ticket y de la orden en un panel lateral** (`PanelProtegido`): a la derecha,
  panel sólido sobre un telón que oscurece la ficha (antes iba sin velo y se confundía con la
  página); en el teléfono ocupa el ancho. Misma validación, «Falta…»,
  protección de cambios (Escape, tocar fuera, la X y «Cancelar» preguntan) y aviso al guardar.
- **Argos 0.5.0 (#81):**
    - técnico: «Vincular con Argos» (`agenda.vincular_argos`, selector de
      `GET /integraciones/argosisp/tecnicos`: solo los de la empresa y libres) y «Desvincular»
      (`agenda.desvincular_argos`). Sin la capacidad `inventario_por_tecnico` dice «Argos < 1.6.0:
      inventario por técnico no disponible» y no ofrece vincular;
    - orden, pestaña «Argos»: código del error, errores corregibles desde Iris con «Reintentar»,
      cargos que Argos emitió (con la factura) y «Resolver a mano»
      (`integraciones.resolver_pendiente`) con motivo; si responde `decision_cargo_requerida`,
      pregunta «¿Argos ya cobró el cargo?» (Sí → `resolver_tambien`, No → `liberar`);
    - modalidad de cada material («Se cobra al cliente», «Comodato: regístralo como equipo»,
      «Gasto»), referencia del cliente y del servicio y contacto en sitio;
    - «Dato de Argos cambió» en el tablero, en la cabecera de la orden y en su pestaña «Argos», con
      qué cambió. «Aplicar» (`ordenes.aplicar_datos_argos`) deja elegir dirección, coordenadas,
      referencia y contacto (marcado lo que cambió) y «Descartar» (`ordenes.descartar_datos_argos`)
      pide un motivo opcional. Ambos mandan `version` y `avisoEn` (el `datosArgosCambiados.en`
      leído): un 409 `aviso_argos_cambiado` recarga la orden y muestra el aviso nuevo; un 422
      `instantanea_no_coincide` dice «El servicio en Argos ya no es de este cliente; revisa la orden
      a mano» (y `sin_instantanea_argos`, algo parecido).

## App de campo en el navegador (fase 4, hecha)

- **Barra inferior** Hoy · Por recibir · Pendientes · Yo (zonas de 56 px, con contador de lo que
  falta por enviar y por recibir). «Mapa» no entra: la app no tiene vista de mapa y «Cómo llegar»
  ya abre la app de mapas del teléfono. Cada vista tiene su URL (`@iris/sync/campo`, `vista.ts`, compartido con la app nativa):
  `?orden=N`, `?orden=N&paso=…`, `?vista=pendientes|recibir|yo|bodega`, `?traslado=N`.
- **La orden paso a paso** (`@iris/sync/campo` `estadoPasos`): resumen (cliente, avisos, pausa,
  avance, notas, «No se pudo») → «Empezar trabajo» → **Llegada · Checklist · Materiales y equipos ·
  Fotos**. Cada paso es su propia vista, se guarda solo en el teléfono al tocar, se puede volver a
  cualquiera (avance con enlaces, «Anterior» / «Siguiente» y «Atrás» del navegador) y la barra de
  progreso dice cuántos están listos.
- **«Firma del cliente y cierre», paso final y único:** solo se abre con lo obligatorio listo (si
  no, dice qué falta y lleva ahí); muestra al cliente el resumen (checklist, fotos, materiales y
  equipos), el cliente firma y «Firmar y cerrar orden» guarda la firma y el cierre juntos. La firma
  no aparece en otro lugar.
- **Barra de estado discreta:** con conexión y todo enviado es un punto junto a «Iris Campo»
  (`role="status"`, «En línea, todo enviado»). Crece con texto y acción solo si hay algo por
  enviar (sin señal o enviando), un rechazo, la sesión venció o la empresa ya no vale.
- **«Por recibir»** (contrato 0.5.0, #83): traslados enviados a la bodega del técnico; al abrir
  uno marca lo que faltó (cantidad o series) y firma; la recepción va a la cola
  (`motor.recibirTraslado`, agrupada por traslado con la clave `-trasladoId`) con su firma de la
  entidad `traslado` (si el alta da 404 porque el traslado aún no llega, se reintenta con espera).
  `traslado_no_encontrado` solo se descarta; un rechazo tras 24 h sin respuesta de Argos se rehace.
- **«Mi bodega»** dentro de «Yo»: existencias con modalidad, equipos con serie y movimientos
  recientes, solo lectura, con «Reintentar» si no se pudo leer.
- **Botones de 44 px o más** en toda la app de campo (`tamano="lg"`; `Casilla tamano="lg"`).

## App de campo: diseño «combinada» (nov 2026, web y nativa)

Reemplaza la barra inferior y la barra de estado de la fase 4; las reglas de arriba siguen.

- **Encabezado que no queda fijo:** fecha con el punto de conexión, «Hola, <nombre>», buscar y la
  cuenta, y en Hoy, Mañana y Semana los chips con su contador. Se va al bajar y vuelve como barra
  compacta al subir.
- **Isla de menú flotante** (píldora centrada abajo, sólida): Hoy · Por recibir · «+» en teal ·
  Agenda · Yo, zonas de 54 px (44 encogida al bajar). Lo que pide atención va como píldora encima
  («Sin conexión · 2 pendientes por enviar», un rechazo) y lleva a «Por enviar».
- **«+»:** hoja de acciones rápidas con lo que tiene API (agregar formulario, con «Deshacer»); lo
  que no la tiene va deshabilitado con «Próximamente».
- **Tarjetas de 28 px de radio, botones en píldora, avisos que entran desde arriba con su acción.**
  Colores: los `--hx-*` y sus mezclas (`apps/web/app/campo/campo.css`, `coloresCampo` en la app
  nativa); un solo teal.
- La lógica de pantalla es compartida: `@iris/sync/campo` `inicio.ts`.

## Escáner de serie y MAC (igual en la web y en la app nativa)

Las etiquetas de ONU, routers y antenas traen varios códigos (S/N, MAC, GPON SN, modelo, EAN, a
veces un QR). El escáner nunca acepta en silencio el primero que ve:

- **Por campo:** «Número de serie» y «MAC» llevan cada uno su botón (`ScanLine`, «Escanear número
  de serie» / «Escanear MAC», 44 px o más). El visor abre con su objetivo: «Escaneando: **Número de
  serie**» y «Apunta solo al código junto a S/N» (o «junto a MAC»).
- **Visor:** la cámara se oscurece salvo un recuadro al centro con esquinas teal; solo cuentan los
  códigos cuyo centro cae dentro (`bounds`/`cornerPoints` de expo-camera, `boundingBox` de
  `BarcodeDetector`; con ZXing, sin posición, cuentan todos). Debajo, «Detectados: N». Linterna si
  la cámara la tiene. Vibración corta con el primer código (`Vibration` en nativa,
  `navigator.vibrate` en la web).
- **Ventana de ~800 ms** desde el primer código; sin códigos sigue esperando. Luego, una hoja
  inferior con lo detectado: tipo («QR» o «Código de barras» con su formato), valor en monoespaciada
  y lo que parece («Parece serie», «Parece MAC», «QR con serie y MAC», «Parece código de producto
  (EAN/UPC)», «No parece serie»; para la MAC, lo demás es «No es una MAC» y no se elige). Primero lo
  que encaja; un EAN/UPC nunca va primero para la serie.
- **Un solo candidato que encaja** se muestra como confirmación («¿Usar ABC123 como número de
  serie?», «Usar» y «Seguir escaneando»), con los demás debajo. Un QR con serie y MAC llena los dos.
  Siempre hay «Seguir escaneando» y «Escribir a mano».
- **Después de elegir:** aviso emergente («Serie: ABC123»), el campo con la insignia «Escaneado»
  (se quita al editarlo). Si la serie o la MAC ya están en otro equipo de la orden, la hoja lo
  avisa antes de usarla.
- **Lógica compartida:** `@iris/sync/campo` (`escaneo.ts`: `clasificarCodigo`, `candidatosPara`,
  `dentroDelRecuadro`, `recuadroEnFuente`, `SesionEscaneo`). La web dibuja con `VisorEscaner` de
  `@iris/ui` (`app/campo/_componentes/escaner-equipo.tsx`); la nativa con expo-camera
  (`src/pantallas/orden/Escaner.tsx`, lógica en `src/campo/escaner.ts`).

## Plan por fases

Cada fase:

- sale en su propia PR;
- pasa Vitest y Playwright en escritorio y teléfono, en claro y oscuro;
- llega al PM con capturas antes de publicarse.

| Fase | Qué | Quién |
| --- | --- | --- |
| 0 · Base | HeroUI instalado, tema Horus, interior de `packages/ui` en HeroUI con los mismos nombres, componentes nuevos y esta guía | I3 |
| 1 · Navegación del panel (hecha) | Barra lateral agrupada (Operación, Equipo, Ventas, Análisis, Administración; contraíble), migas, buscador Ctrl + K, avisos emergentes al guardar y barra inferior en el teléfono (Inicio, Tickets, Órdenes, Agenda, Más) | I3 |
| 2 · Formularios a pantalla propia (hecha) | Tickets, órdenes, usuarios, técnicos y ausencias, comercial (prospecto y cotización) y buzón en dos pasos | I3 |
| 3 · Fichas con pestañas (hecha) | Ticket, orden, técnico, prospecto y usuario; acciones en un panel lateral; Argos 0.5.0 en el panel | I3 |
| 4 · App de campo en el navegador (hecha) | Barra inferior, pasos independientes, firma y cierre como paso final, barra de estado discreta, botones grandes, «Por recibir» y «Mi bodega» | I3 |
| 5 · App nativa | Las pantallas de la fase 4 con HeroUI Native | I4 |
| Voz E1 (hecha) | Llamadas de WhatsApp contestadas en el panel: recibir, contestar, barra en curso, nota, hilo, perdidas y ajustes | I3 |
| 6 · Portal de clientes | Mis casos, nuevo caso y conversación con el mismo estilo | I3 |

## Uso rápido

```tsx
// Layout del panel (fase 1): una vez.
<ProveedorRutas navegar={router.push}>
    {children}
    <ProveedorAvisosEmergentes />
</ProveedorRutas>

// Pantalla de formulario (fase 2). En el panel las migas las pone el layout: no se pasan.
<FormularioPagina
    titulo="Nueva orden"
    alEnviar={form.handleSubmit(guardar)}
    navegar={router.push}
    volverA="/ordenes"
    sucio={form.formState.isDirty}
    barra={{ textoGuardar: "Crear orden", enviando: form.formState.isSubmitting, faltan }}
>
    <SeccionFormulario titulo="1. Cliente y servicio">…</SeccionFormulario>
</FormularioPagina>

// Después de guardar.
avisar({ titulo: "Orden ORD-1042 asignada a Luis", accion: { texto: "Deshacer", alPresionar: deshacer } });
```
