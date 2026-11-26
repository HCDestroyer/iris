# Guía de interfaz de Iris

Decisiones aprobadas por el PM (propuesta «Nueva interfaz de Iris», 31 oct 2026, y el rediseño del 21 nov 2026) y cómo aplicarlas. La
siguen todos los frentes que tocan pantallas: panel (I3), app de campo (I4) y portal de soporte.

## Sistema visual (rediseño, nov. 2026)

Aprobado por el PM (maqueta del rediseño, 21 nov 2026). Iris toma la estructura y las reglas del
rediseño de ArgosISP con su kit: **HeroUI v3 (`@heroui/react` 3.2.x) envuelto en `@iris/ui`**.

- **Web (`apps/web`: panel, `/campo` y `/soporte`): HeroUI v3**, sobre React Aria, Tailwind 4 y
  React 19. **App nativa (`apps/movil`): HeroUI Native**, con los mismos colores
  (`apps/movil/src/tema/tokens.ts`).
- **Las pantallas importan de `@iris/ui`, nunca de `@heroui/react`.** Si falta una pieza, se agrega a
  `packages/ui` con nombre en español, su prueba y su entrada en el mapa de abajo.
- Nada de `<button>`, `<input>`, `<select>`, `<textarea>`, `<table>` ni `<dialog>` crudos fuera de
  `packages/ui`. El CSS propio que haga falta para lograr el diseño va dentro de los envoltorios.

### HeroUI primero (regla del PM, nov. 2026)

1. **Toda pieza de interfaz es un componente de HeroUI v3**, envuelto en `@iris/ui` con los tokens
   de Iris (`--iris-*`). Antes de escribir un control, se busca en HeroUI (`node scripts/list_components.mjs`
   del skill `heroui-react`) y se lee su documentación.
2. **Un control propio solo si HeroUI no lo trae** (cámara, escáner, firma, gráfica, logo…), y con
   su motivo escrito en la tabla «Lo que no se usa de HeroUI» de abajo o en
   `docs/ui/auditoria-heroui.md`. Por dentro usa las piezas de `@iris/ui` (`Boton`, `Aviso`…).
3. Barras, giros, esqueletos, desplegables, tablas, radios, menús y globos hechos a mano no
   valen: `BarraProgreso`, `Medidor`, `Cargando`, `Esqueleto`, `Desplegable`, `Tabla`,
   `GrupoSegmentado`, `MenuDesplegable`, `Globo`.
4. **ESLint lo cuida** (`apps/web/eslint.config.mjs`): prohíbe los elementos crudos de arriba y
   `<details>`, `<progress>` y `<meter>` (también con `createElement` o guardando la etiqueta en una
   variable), los `role` de barra, medidor, radio y diálogo hechos a mano, e importar (o `import()`)
   `@heroui/*`, `react-aria`, `react-aria-components`, `@react-aria/*` o `@react-stately/*` fuera de
   `packages/ui`. Un `eslint-disable` sin uso es error.
5. **Avisos emergentes y barra de guardar:** en escritorio, con `BarraGuardar` en pantalla, los
   avisos suben por encima de ella (`globals.css`): nunca tapan «Guardar» ni «Cancelar».

### Tokens `--iris-*` (`packages/ui/src/estilos/iris-tokens.css`)

`iris-tokens.css` es la única fuente de color (copia exacta de `docs/marca/iris-tokens.css`; una
prueba lo verifica). Los `--hx-*` de Horus quedan como **alias** de los `--iris-*` (todo el código que
los usa toma los colores nuevos) y **ningún teal queda en la interfaz** (lo verifica
`heroui.test.tsx`). `horus-tokens.css` sigue cargado solo por su base (foco, preferencias del
sistema).

| Token | Claro | Oscuro | Uso |
| --- | --- | --- | --- |
| `--iris-primario` | `#4F46E5` | `#818CF8` | acción principal, foco, selección |
| `--iris-sobre-primario` | `#FFFFFF` | `#0B0F1A` | texto sobre el primario (el blanco sobre `#818CF8` no llega a 4,5:1) |
| `--iris-primario-suave` / `-texto` | `#EEF2FF` / `#4338CA` | índigo al 14 % / `#A5B4FC` | relleno de lo elegido y botón `suave` |
| `--iris-secundario` | `#06B6D4` | `#22D3EE` | solo en el degradado y detalles; nunca un estado |
| `--iris-gradiente` | `#4F46E5 → #06B6D4` (135°) | igual | sin texto encima (logo, franjas) |
| `--iris-gradiente-texto` | `#4F46E5 → #4338CA → #0E7490` | igual | con texto blanco encima |
| `--iris-fondo` | `#F8F9FA` | `#0B0F1A` | lienzo (azul noche, nunca negro) |
| `--iris-tarjeta` | `#FFFFFF` | `#121828` | tarjetas e islas |
| `--iris-flotante` | `#FFFFFF` | `#18203A` | menús, vista rápida, paneles, modales: sólidos |
| `--iris-relleno` | `#F1F5F9` | `#1A2236` | rellenos tenues, riel de las pestañas |
| `--iris-borde` | `#E7EAEE` | `#222A40` | bordes de 1 px |
| `--iris-texto` / `-tenue` / `-apagado` | `#0F172A` / `#475569` / `#5F6B7F` | `#E2E8F0` / `#A3B0C2` / `#8A96AA` | texto principal, secundario y terciario |
| `--iris-exito/atencion/peligro/info` (+ `-fondo`, `-punto`) | `#166534`, `#92400E`, `#B91C1C`, `#1D4ED8` sobre su fondo | `#4ADE80`, `#FBBF24`, `#F87171`, `#60A5FA` al 13–14 % | estados: siempre con ícono y texto |
| `--iris-grafica-1…5` | índigo `#4F46E5`, cian `#0891B2`, fucsia `#C026D3`, naranja `#C2410C`, amarillo `#A16207` | `#818CF8`, `#22D3EE`, `#E879F9`, `#FB923C`, `#FACC15` | series de gráficas; nunca un color de estado ni teal; siempre con leyenda |
| Radios | control 10 px, tarjeta 12 px, modal 16 px, isla 20 px, isla grande 22 px (app de campo), botón píldora | | |
| Sombras | `--iris-sombra-tarjeta` suave; `--iris-sombra-flotante` y `--iris-sombra-modal` marcadas; `--iris-telon` | | |

**Cambios respecto a la maqueta:** el texto apagado pasa de `#64748B` a `#5F6B7F` (el de la maqueta
daba 4,34:1 sobre el relleno) y la 4.ª serie de las gráficas pasa de teal a naranja 700 (ningún teal
en la interfaz).

`tema-heroui.css` apunta cada variable de HeroUI a un `--iris-*`: `accent` → primario, `accent-soft`
→ primario suave, `background` → fondo, `surface` → tarjeta (sombra de tarjeta), `overlay` →
flotante (sombra flotante, sin vidrio), `backdrop` → telón, `success/warning/danger` → estados,
`field-*` → tarjeta + borde con radio de 10 px. Los botones son píldoras (`Boton` pone
`rounded-full`; quien necesita otra forma pasa su `rounded-*`). Modales y confirmaciones con radio de
16 px y la sombra de lo modal.

- HeroUI no tiene «info»: el azul es la utilidad `info` de Iris (`text-info`, `bg-info-fondo`).
  `accent` nunca es un estado.
- El modo oscuro va por la clase `.dark` (next-themes) o por `[data-theme]`.
- Lo flotante es sólido, con borde y sombra; toda capa modal lleva su telón
  (`e2e/capas-flotantes.spec.ts`). Con `prefers-reduced-motion` no hay animaciones.

### Contraste (WCAG 2.2 AA)

Medido con la fórmula de WCAG sobre los valores de arriba (texto normal: 4,5:1; elementos gráficos y
series: 3:1). Todos los pares de texto pasan.

| Par | Claro | Oscuro |
| --- | --- | --- |
| texto / tarjeta · fondo · relleno | 17,85 · 16,94 · 16,30 | 14,35 · 15,52 · 12,85 |
| texto tenue / tarjeta · fondo · relleno | 7,58 · 7,19 · 6,92 | 8,04 · 8,70 · 7,20 |
| texto apagado / tarjeta · fondo · relleno | 5,39 · 5,11 · 4,92 | 5,92 · 6,40 · 5,30 |
| primario (como texto o enlace) / tarjeta · fondo | 6,29 · 5,96 | 5,93 · 6,41 |
| sobre-primario / primario (botón principal) | 6,29 (blanco) | 6,41 (`#0B0F1A`; el blanco daría 2,98) |
| primario-texto / primario-suave (botón suave, lo elegido) | 7,07 | 7,23 |
| éxito · atención · peligro · info sobre su `-fondo` | 6,49 · 6,37 · 5,30 · 5,49 | 7,80 · 8,14 · 5,29 · 5,51 |
| neutro / neutro-fondo | 9,45 | 9,85 |
| blanco / botón peligro (`-punto`) | 4,83 | 6,40 (`#121828` sobre `#F87171`) |
| blanco / degradado con texto (`#4F46E5` · `#4338CA` · `#0E7490`) | 6,29 · 7,90 · 5,36 | igual |
| blanco / cian `#06B6D4` | 2,43: **no apto** para texto (por eso existe el degradado con texto) | — |
| series 1…5 / tarjeta | 6,29 · 3,68 · 4,71 · 5,18 · 4,92 | 5,93 · 9,79 · 7,19 · 7,82 · 11,55 |

### Marco del panel

- **Menú isla** (`MenuIsla`): flota a 16 px de los bordes, radio 20 px, sólido. Compacto (80 px:
  ícono + nombre corto; Negocio y Administración abren su grupo al lado) o expandido (248 px,
  grupos que se abren —Disclosure de HeroUI— y «Mis vistas»). El botón `‹ ›` de 28 px va sobre el
  borde derecho a la altura del logo, en el mismo lugar en los dos modos; la elección se recuerda en
  el navegador. Mantiene los nombres de siempre para lectores y pruebas: «Barra lateral»,
  «Principal», «Contraer menú» / «Expandir menú».
- **Encabezado sin barra** (`EncabezadoPanel`): botones sueltos de 44 px, cada uno una isla
  (`BotonFlotante`), a 16 px del borde de arriba. Izquierda: Buscar (`BotonBuscar`, Ctrl K), los
  accesos rápidos «Ticket» y «Orden» y «Más» (todo lo que se puede crear). Derecha: el teléfono de
  Iris, llamadas y chat del equipo cuando aplican, la conexión con Argos y Horus (`IndicadorEstado`:
  un punto; gris si no se pudo leer, nunca un estado inventado), avisos y la cuenta (`MenuCuenta`:
  nombre, rol y empresa; dentro, disponibilidad en la bandeja, empresa activa si hay varias, tema,
  Mi perfil, contraseña, dos pasos, «Manual y atajos» y «Cerrar sesión» al final en rojo).
- **Área de trabajo** (`#contenido`): empieza bajo el encabezado y se desplaza por dentro; la
  página nunca se desplaza. Migas, título con su «?» y la acción principal; debajo, el contenido en
  tarjetas.
- **Teléfono:** isla de menú abajo (`IslaInferior`: Inicio · Tickets · Órdenes · Agenda · Más) y el
  botón redondo «Crear». «Más» es una hoja con el resto del menú, la empresa, la cuenta y el tema. En
  fichas y formularios la isla cede su lugar a la acción fija (`AccionFija`).

### Reglas

1. Solo componentes del kit; nada crudo fuera de `packages/ui` (ESLint). La personalización va en
   los envoltorios.
2. Claro y oscuro, revisados en 390 y 1280 px, sin desbordes.
3. Fidelidad al diseño aprobado: si algo no puede quedar igual en código, se corrige el diseño y se
   dice. Nunca una diferencia callada.
4. Menú isla (arriba). 5. Encabezado sin barra (arriba).
6. Íconos de Lucide en todo; estado = ícono + texto + color (`InsigniaEstado`, píldora con el
   `-fondo` del estado); pestañas segmentadas (`PestanasSegmentadas`) dentro de la tarjeta de su
   contenido, con contador y su URL (`?pestana=`).
7. Listas (`ListaFilas`, `FilaLista`) en lugar de cuadros, con `CampoBusqueda`, vistas con contador
   y «Filtros (n)»; vista rápida flotante (`VistaRapida`) y menú `⋯` por fila (`MenuAcciones`).
8. Contenedores: modal (1 a 3 campos, nunca uno encima de otro), panel lateral (`PanelLateral`:
   registrar o ver sin perder la lista; con `telon` si es un formulario), vista rápida (sin telón, no
   quita ancho) y página propia (`FormularioPagina`, más de 3 campos o con pasos).
9. Un solo campo destacado a la vez (el que tiene el foco o el error).
10. El scroll va dentro del contenedor.
11. Un «?» del manual (`BotonManual`) junto a cada título; abre el manual en la sección de esa
    pantalla. Las claves son las de `docs/manual/claves.md` (las pone el manual); el panel las saca de
    la ruta (`apps/web/lib/manual/claves.ts`) y las muestran `EncabezadoPagina`, `FormularioPagina` y
    los títulos propios con `BotonManualPantalla`. Dentro de `/campo` el «?» va a
    `/campo/manual/<pantalla>` (`base="/campo/manual"`): el service worker solo controla `/campo` y
    así abre sin señal.
12. En el teléfono, pantalla completa y acción principal fija abajo (`AccionFija`).
13. Un dato, una vez por pantalla.
14. Errores con «Reintentar»; sin totales sobre datos que no se leyeron.

Además se mantienen las reglas de siempre:

1. **Formularios de más de 3 campos van en pantalla propia** (`FormularioPagina`, con secciones,
   «Guardar» siempre visible al pie con `BarraGuardar` y su enlace). Un formulario nunca se abre
   encima de otro; si sales con cambios sin guardar, Iris pregunta antes de descartar.
2. **Los diálogos son solo para confirmar y para acciones cortas (1 a 3 campos).** Las
   confirmaciones destructivas usan `DialogoConfirmar peligro`.
3. **Migas en cada pantalla del panel** (`Migas`). **Aviso emergente después de cada acción**
   (`avisar`), con «Deshacer» cuando se puede; los errores que piden actuar van como `Aviso` en la
   pantalla, con «Reintentar».
4. **App de campo:** cada paso de trabajo es una vista independiente que se guarda sola en el
   teléfono (llegada, checklist, materiales y equipos, fotos); la **firma del cliente es el paso
   final y único, junto con el cierre**.
5. react-hook-form + zod.

## Mapa de componentes

Los nombres y las props de `@iris/ui` no cambiaron: las pantallas siguen igual y por dentro usan
**los componentes de HeroUI v3** (`@heroui/react` 3.2.6, sus piezas compuestas: `Modal.Backdrop`,
`Card.Header`…), con los tokens de Iris (`--iris-*`). React Aria da el comportamiento: teclado, foco y lectores
de pantalla. Los envoltorios que reciben un icono de Lucide como componente (`Aviso`,
`InsigniaEstado`, `EstadoVacio`) siguen sirviendo desde un componente de servidor: el icono se
dibuja afuera y la pieza de HeroUI va en un archivo `"use client"` aparte (`aviso-alerta.tsx`,
`chip-estado.tsx`, `raiz-vacio.tsx`), porque `@heroui/react` solo corre en el cliente.

| `@iris/ui` | Componente de HeroUI | Notas |
| --- | --- | --- |
| `Boton`, `BotonEnviar` | `Button` | Variantes de Iris → `primary`, `tertiary`, `outline`, `ghost`; medidas y radio de Iris. `disabled` → `isDisabled`; `onClick`, `type="submit"`, `data-*` y `aria-*` como siempre. Lo que el `Button` de React Aria filtra (`title`, `role`, `tabIndex`, `aria-invalid`, `aria-keyshortcuts`, el arrastre nativo `draggable`/`onDrag*` del calendario de despacho) va directo al `<button>` por `render`. Como todo `Button` de React Aria, el clic no sigue subiendo a los padres. `asChild` (un `<Link>` con aspecto de botón) usa `Ranura` con las mismas clases |
| `Tarjeta*` | `Card`, `Card.Header`, `Card.Title` (como `<h2>`), `Card.Description`, `Card.Content`, `Card.Footer` | Acomodo neutro: cada pantalla manda con sus clases; material `surface-card` |
| `Aviso`, `ErrorFormulario` | `Alert`, `Alert.Indicator`, `Alert.Content`, `Alert.Title` y la pieza `alert__description` | Fondo tintado opaco por tono, borde, icono de Lucide + título + texto; `role="alert"` en `error`. La descripción es un `<div>` (`Alert.Description` es un `<span>` y aquí caben párrafos y listas) |
| `Dialogo*`, `DialogoProtegido` | `Modal.Backdrop` + `Modal.Container` + `Modal.Dialog` + `Modal.Heading` | Panel sólido sobre telón; en el teléfono es una hoja inferior a lo ancho. La «X» es un `Boton` con el icono `X` de Lucide (no el `CloseButton` de HeroUI, que trae su propio icono) |
| `DialogoConfirmar` | `AlertDialog.Backdrop` + `AlertDialog.Container` + `AlertDialog.Dialog` + `AlertDialog.Heading` | `role="alertdialog"`, foco inicial en «Cancelar», no se cierra tocando fuera |
| `Hoja*`, `PanelProtegido` | `Drawer.Backdrop` + `Drawer.Content` + `Drawer.Dialog` + `Drawer.Heading` | Siempre con telón oscurecido: `fondo="transparente"` se acepta pero ya no quita el telón. Se cierra también arrastrando hacia su lado; el contenido se desplaza en vertical (`touch-action: pan-y`) |
| `PanelEmergente` | `Popover`, `Popover.Content`, `Popover.Dialog`, `Popover.Heading` | Diálogo no modal junto al disparador |
| `PantallaCompleta` | `Modal.Backdrop`, `Modal.Container`, `Modal.Dialog` | Pantalla modal que tapa toda la app y no se cierra tocando fuera ni con Escape (la llamada en curso de `/campo`) |
| `PanelLateral` | — sin telón (`<section role="dialog" aria-modal="false">` propio); con `telon`, `Drawer` | Isla flotante a 16 px de los bordes, radio 20 px, 470 px desde `sm`, sombra de lo modal; en el teléfono a pantalla completa. **Sin telón (excepción aprobada por el PM a la regla de `Hoja`):** flota sobre la pantalla, la página no se corre ni cambia de ancho y los clics fuera le llegan a ella; no atrapa el foco (lo lleva a `data-foco-inicial` al abrir y lo devuelve al cerrar); Escape lo cierra con el foco adentro. Lo usan el chat del equipo y `VistaRapida`. **Con `telon`:** el `Drawer` de HeroUI con la misma isla (modal, telón oscurecido) para registrar algo sin perder la lista. En escritorio los avisos emergentes se corren a su izquierda (`globals.css`) |
| `ListaSugerencias`, `teclasSugerencias`, `idOpcion` | — (combobox de ARIA con foco virtual) | Sugerencias bajo un `AreaTexto` que sigue con el foco: las `@menciones` del chat. El área lleva `role="combobox"`, `aria-controls` y `aria-activedescendant`; flechas, Enter/Tab y Escape. `Autocompletar` no sirve: reemplaza todo el valor de un campo de una línea |
| `MenuDesplegable*`, `MenuEnLinea` | `Dropdown`, `Dropdown.Popover`, `Dropdown.Menu`, `Dropdown.Section`, `Dropdown.Item`, `Menu` y `Separator` | `onSelect` → `onAction`. Un `Boton` como disparador se conecta solo; otro elemento, por `Pressable` |
| `Globo` | `Tooltip`, `Tooltip.Content` | Un `Boton` se conecta solo; otro elemento, por `Focusable` |
| `GrupoSegmentado` | `ToggleButtonGroup` + `ToggleButton` (selección única) | `role="radiogroup"` y `role="radio"`; como un grupo de radios, las flechas mueven y eligen |
| `GrupoOpciones` | `RadioGroup`, `Label`, `Radio`, `Radio.Content`, `Radio.Indicator` | Radios reales; tarjetas grandes con la marca `CircleCheck` de Lucide |
| `Casilla` | `Checkbox`, `Checkbox.Content`, `Checkbox.Control`, `Checkbox.Indicator` | Sirve con `register` y controlada: el `ref` llega al `<input>` (`inputRef`), `onChange` recibe un evento con `target` = ese input, y la marca (`Check` de Lucide) se dibuja desde `:checked`, porque react-hook-form escribe `checked` directo en el input (valores iniciales, `reset`, `setValue`) |
| `Tabla*` | `Table` (variante `secondary`) + `Table.ScrollContainer`, con una `<table>` semántica y las piezas `table__row`, `table__cell`… | Ver «Lo que no se usa» (`Table.Content`) |
| `Esqueleto` | `Skeleton` (pulso) | |
| `Cargando` | `Spinner` | Es un indicador animado, no un icono; `role="status"` con su texto. `tamano` (`sm` 16 px, `md` 20 px, `lg` 28 px) y `decorativo` (oculto para lectores) cuando el texto ya está al lado o dentro de un botón que dice qué hace |
| `BarraProgreso` | `ProgressBar`, `ProgressBar.Track`, `ProgressBar.Fill` | Avance sin rótulo visible (el texto lo pone la pantalla): `valor`, `maximo`, `etiqueta`, `textoValor` (→ `aria-valuetext`), `tono` (`normal`, `aviso`, `error`). `pasos`: un segmento por paso (los pasos de la orden en la app de campo). Sin rango (`maximo` 0) queda vacía, sin dividir por cero |
| `Desplegable` | `Disclosure`, `Disclosure.Heading`, `Disclosure.Trigger`, `Disclosure.Indicator`, `Disclosure.Content` | Reemplaza a `<details>`: ayudas y ajustes «Avanzado». Cerrado por omisión (`abierto`), encabezado `h3` (`nivel`), chevron `ChevronDown` de Lucide |
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
| `SelectorArchivo` | — (`<input type="file">` oculto en su `<label>`, como `Camara`) | Elegir un archivo sin cámara; clip de Lucide; se limpia tras elegir |
| `MenuArchivos` | `Dropdown` (por `MenuDesplegable`) + `<input type="file">` ocultos | El clip con menú (Fotos, Documento, Cámara): cada opción abre el selector del sistema con sus tipos; `varios` y `camara` por opción. Con una sola opción no hay menú |
| `VistaPreviaArchivos` | — (`Boton`, `Dialogo` y `BarraProgreso` por dentro) | Lo elegido antes de enviarlo, en una **tira de miniaturas de 64 px** que se desliza a lo ancho (no crece hacia arriba): imagen ampliable (solo con la URL de objeto que le pasan) o icono con el nombre corto (documento, video), su «X» y el estado con icono + texto + color (no sirve o falló: borde y franja en rojo y «Reintentar»; subiendo: barra). El nombre completo y el detalle van en `title` y en texto para lectores; lo que no sirve se explica en una línea bajo la tira. Solo presentación |
| `FotoMensaje`, `AlbumFotos`, `medidasFoto` | `Button` (por `Boton`) con la `<img>` adentro; `Spinner` mientras está pendiente | Foto en un hilo (bandeja y chat del equipo): la caja toma la proporción de la foto (acotada entre 1:2 y 2:1) con máximo de 320 × 340 px y nunca más ancha que la burbuja; sin medidas reserva 4:3 y toma la real al cargar. El espacio queda reservado antes de cargar (sin saltos); la hora va sobre la foto; si falla, icono + «No se pudo cargar la foto» + «Reintentar». `AlbumFotos`: fotos seguidas en cuadrícula de 2 columnas (la tercera a lo ancho, «+N» en la cuarta) |
| `VisorFotos` | `Modal.Backdrop` + `Modal.Container` + `Modal.Dialog` (nombre accesible «Foto 2 de 5: …» por `aria-label`) | A pantalla completa sobre fondo oscuro: la foto entera, quién y cuándo, «2 de 5», flechas (y ← →), deslizar en el teléfono, doble toque para acercar, pie y «Descargar» (la ruta autenticada del adjunto). Foco atrapado; Escape, la «X» y «Atrás» del navegador o del teléfono cierran sin salir de la pantalla (una entrada propia en el historial, que se consume al cerrar) |
| `ListaRecortada` | — | Una línea con los chips que caben y «+N»; nunca desborda ni baja de línea. El nombre completo de todos lo pone quien la usa (`title` y texto para lectores) |
| `AvatarUsuario` | `Avatar`, `Avatar.Fallback` | Iniciales (decorativo: el nombre va al lado) |
| `Entrada` | `Input` | Un `<input>` nativo: sirve con `register` |
| `Campo` | `TextField` + `Label` + `Input` | Etiqueta, ayuda y error enlazados por React Aria (`isInvalid`). Sirve igual que un `<input>`: con `register` el `ref` recibe el propio `<input>` envuelto (`conAvisoValor`), así `reset` y `setValue` mueven el `TextField`; controlado con `value` + `onChange(e)`; el resto de las props va al `<input>` |
| `CampoSecreto` | `Input` | Credencial que se pega una vez (token, app secret), con el ojo «Mostrar …», monoespaciada y sin corrector. Para que el gestor de contraseñas no ofrezca guardarla: `type="text"` con `-webkit-text-security: disc` donde el navegador lo soporta y, si no, `type="password"`; siempre `autocomplete="new-password"`, `data-1p-ignore` y `data-lpignore`. Sirve con `register` |
| `AreaTexto`, `CampoArea` | `TextArea` | |
| `Etiqueta` | `Label` | |
| `ProveedorRutas` | `RouterProvider` de React Aria | |
| `GraficaSerie` | — | Propia de Iris (SVG, sin librería). Una a dos series en un solo eje, con leyenda si hay dos, el último valor rotulado, el tramo sin datos en gris (nunca una línea en cero), cursor al pasar o con las flechas y «Ver tabla». Colores `--iris-grafica-1` y `--iris-grafica-2` (por sus alias `--hx-grafico-*`); el texto va en color de texto, no de la serie |
| `Medidor` | `Meter`, `Meter.Track`, `Meter.Fill` | «Cuánto de cuánto» (memoria, SLA cumplido) con tono `normal`, `aviso` o `error` y `claseRiel` para el alto. Con `parte: null` dibuja el riel de `Meter` con `role="meter"` y sin `aria-valuenow` (el `Meter` de React Aria siempre anuncia un valor; aquí no se inventa un cero) |
| `TecladoTelefono`, `ControlLlamada` | `Button` (por `Boton`) | Teclas de 56 px y controles redondos de la llamada; `aria-pressed` en los que se prenden y apagan |
| `BarraGuardar`, `FormularioPagina`, `SeccionFormulario` | — | Composición propia de Iris con `Boton`. `BarraGuardar` acepta `nota` (una línea a la izquierda: «Al guardar, Iris prueba la conexión con Meta.»). `FormularioPagina` acepta `accionTitulo` (a la derecha del título: el interruptor que enciende lo que la pantalla configura) |
| `MenuIsla`, `IslaInferior` | `Disclosure` (grupos), `Popover` (grupos en la compacta), `Tooltip`, `Button`; enlaces de React Aria (`Link`) | Menú del panel como isla (ver «Marco del panel»). Las entradas llevan `aria-current`; en la compacta el contador es un punto y el número va en el nombre accesible. `IslaInferior`: «Accesos rápidos» del teléfono con «Más» y el botón redondo de la acción principal |
| `EncabezadoPanel`, `BotonFlotante`, `BotonBuscar`, `IndicadorEstado` | `Button`, `Kbd`, `Tooltip` | Encabezado sin barra: islas de 44 px. `IndicadorEstado`: un punto; crece con texto (atención o peligro) cuando hay que actuar; el estado siempre en el nombre accesible y en el globo |
| `MenuCuenta` | `Popover` (por `PanelEmergente`), `Menu` (por `MenuEnLinea`), `Avatar` | La cuenta: isla con avatar, nombre y «rol · empresa»; dentro, los controles (disponibilidad, empresa, tema), las opciones, «Manual y atajos» y «Cerrar sesión» al final en rojo |
| `MenuAcciones` | `Dropdown`, `Dropdown.Section`, `Separator`, `Kbd` | Menú `⋯`: frecuentes primero (por sección), peligrosas al final, separadas y en rojo; `permitida: false` la quita; sin acciones visibles no se dibuja. `gruposMenuAcciones` arma el orden |
| `VistaRapida` | — (por `PanelLateral` sin telón) | Flota sobre la lista sin quitarle ancho: «Vista rápida», código, título, estados, contenido que se desplaza y acciones al pie |
| `ListaFilas`, `FilaLista` | — (lista semántica `<ul>` con nombre; `Button` / `Link` de React Aria por fila) | Reemplazan a `Tabla`: cabecera (buscador, vistas, filtros), barra de selección, filas con casilla, ícono en su ficha de color, título y detalle, estados y menú `⋯`; pie con «Mostrando n de m» y «Cargar más». `ListBox` no sirve: las filas llevan casillas y menús |
| `FranjaFicha` | `Card` (por `surface-card`) | Identidad (`<h1>`), estados, el «?», contadores, una acción principal y «Más», en una fila |
| `PestanasSegmentadas` | `Tabs` (el `Tabs.Indicator` es la píldora que se desliza) | Riel de relleno con radio de 14 px, ícono + texto + contador (tono índigo, ámbar o rojo); `ancho` reparte las opciones; con `href`, cada pestaña tiene URL |
| `CampoMoneda` | `NumberField` con `formatOptions` de moneda (GTQ) | Montos en quetzales (`Q150.00`) con − / + que dicen su paso («Sumar Q 25.00»); el formulario recibe el número como texto (`"150"`). Sirve con `register` (puente `control-nativo`) |
| `AccionFija` | — (composición con `Boton`) | Barra fija abajo con el área segura: en el teléfono (`telefono`, en su lugar desde `lg`) o siempre (`siempre`, columna de la app de campo) |
| `BotonManual`, `BotonManualPantalla`, `ProveedorManual` | `Link` de React Aria con las clases de `Button`, `Tooltip` | El «?»: enlace a `/manual/<clave>` (o `base="/campo/manual"`), nombre «Manual de esta pantalla», globo y cursor de ayuda. `esClaveManual` valida `modulo/pantalla`. `BotonManualPantalla` usa la clave del `ProveedorManual` (el marco la saca de la ruta) |

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
| `Slider`, `InputOTP`… | No reemplazan nada hecho a mano que lo pida (el código MFA es un `Campo` de 6 cifras). Están disponibles para agregarlos a `packages/ui` con nombre en español cuando una pantalla los pida |
| `Camara`, `VisorEscaner`, `LienzoFirma`, `LogoIris`, `MarcaIris`, `SelectorArchivo` | No tienen equivalente en HeroUI (cámara con `<input capture>`, escáner con `BarcodeDetector`/ZXing, firma con pointer events, logo, `<input type="file">`). Siguen como estaban, con los tokens del tema y `Boton` por dentro |
| `Button isPending` en «Actualizar», «Probar» o «Sincronizar» | Cambia el icono por un `Spinner` y bloquea el botón; en Iris el icono de la acción (`RefreshCw`) gira y el botón dice «Probando…» |
| Agenda de despacho y vista de mes (`role="grid"` propio) | `Calendar` es un selector de fechas; la agenda muestra citas, se arrastra para programar y cada celda es un `Boton` |
| Aviso de llamada entrante (`role="alertdialog"` flotante) | `AlertDialog` es modal y `Toast` se va solo: la llamada suena y queda a la vista sin bloquear la pantalla |
| Animación de salida de modal, hoja y confirmación | Se desmontan en el acto: con salida animada, un Escape o un clic inmediato le llegaría a la capa que se está yendo |

## Auditoría «todo con HeroUI» (nov. 2026)

La tabla completa (qué se migró, qué se conserva y por qué) está en `docs/ui/auditoria-heroui.md`.
En resumen: las `<table>` crudas de Tickets y del estado de Argos pasaron a `Tabla`; las barras de
avance y medidores hechos a mano, a `BarraProgreso` y `Medidor`; los giros y pulsos de carga, a
`Cargando` y `Esqueleto`; los `<details>`, a `Desplegable`; la duración de la tarea (campo), a
`GrupoSegmentado`; y los `<button>` crudos del teclado del teléfono y de la foto en el hilo, a
`Boton`.

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
  `bg-elevated` (`--hx-bg-elevated`), marca `primary` (índigo `#4F46E5` / `#818CF8`), estados `success`,
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
- **Pulido tras el uso real (nov. 2026, `e2e/bandeja-pulido.spec.ts`):**
    - **El hilo abre y se queda en el último mensaje.** Mientras estés abajo sigue abajo aunque
      cambie la altura sin que llegue nada (una foto que carga tarde, la sugerencia o el compositor
      que crecen, el teclado del teléfono: `ResizeObserver` del hilo y de su contenido). Solo subir
      a mano te saca de «abajo»; entonces lo que llega no mueve la vista y aparece la píldora
      «Mensajes nuevos ↓» (con la cuenta si son varios), que baja al pulsarla. Lo que envías tú te
      lleva abajo siempre. Al cargar anteriores, lo que leías se queda en su sitio.
    - **Compositor:** el texto crece hasta `max-h-40` y luego se desplaza por dentro; la barra de
      botones envuelve en vez de cortarse y «Sugerir» queda en icono cuando la conversación es
      estrecha (`@max-[26rem]/conv`).
    - **Adjuntar como WhatsApp Web:** el clip (`MenuArchivos`) ofrece Fotos, Documento (PDF) y, en
      pantalla táctil, Cámara; también se pega una imagen en el texto o se sueltan archivos sobre
      la conversación (aviso «Suelta aquí para adjuntar»). Nada se envía solo: lo elegido queda en
      `VistaPreviaArchivos` sobre el texto, que hace de comentario. Se valida antes de subir (tipo
      y 16 MB, con el motivo a la vista), se puede quitar o ampliar, la subida va por partes con su
      barra y, si falla, «Reintentar» sigue donde quedó. **La API admite un adjunto por mensaje**:
      cada archivo sale en su mensaje, en orden, y el comentario va con el primero. Hasta 10 por
      vez. Sin video (el módulo de adjuntos no lo admite). Seguridad: los nombres van como texto,
      la miniatura es una URL de objeto que se libera al quitar o cerrar, y un SVG o un HTML nunca
      se incrustan (van con icono de documento).
    - **Etiquetas y chips, nunca desbordados** (`componentes/conversaciones/etiquetas.tsx`): en la
      fila de la lista, una línea con las que caben y «+N» (`ListaRecortada`, todas en el título);
      bajo la cabecera del hilo y en la ficha, envueltas en varias líneas; el nombre largo se
      recorta con puntos y el completo va en `title`. En filtros y en el editor, `Casilla` ya no
      ensancha su contenedor (recorta o parte el texto).
    - **«Sin conexión» solo cuando lo es:** un corte del flujo (un proxy que cierra conexiones, el
      servidor que reinicia) reconecta al momento con `Last-Event-ID`, sin indicador y sin perder
      ni repetir eventos. El indicador aparece tras dos intentos fallidos seguidos o 9 s sin poder
      conectar (primer reintento a 1 s; luego 3, 6, 12… hasta 30 s). Vale igual para el aviso de
      llamadas (usa el mismo `iniciarFeed`).
      Excepción: si el flujo abre y se corta de golpe en menos de 3 s dos veces seguidas, sí se
      avisa, y el aviso se queda hasta que un flujo dure.
    - **Revisión de la PR (adjuntos):** el comentario se toma una vez al pulsar «Enviar» (la caja
      queda vacía; lo que se escriba mientras suben es otro mensaje) y va con el primer archivo
      junto con la respuesta rápida y la sugerencia; si la subida falla, vuelve a la caja. Con
      archivos por enviar, cambiar de conversación pregunta («Seguir aquí» / «Salir») y al salir
      el envío se corta. Soltar un archivo fuera de la conversación no hace nada (el navegador
      no lo abre). Pegar texto con imagen (Excel, Word) pega el texto. Ante un 409 de la subida
      se sigue desde el `Upload-Offset` del servidor.
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
- **Se distingue de la bandeja de clientes:** burbujas propias en índigo relleno; avatares cuadrados
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
  compositor y sin «Agregar persona». En la app de campo (PWA y nativa), en lugar del compositor va
  un `Aviso` informativo con candado (`Lock`): «Orden cerrada: ya no se pueden enviar mensajes»
  (o «fallida»/«cancelada», de `textoSoloLectura`), y la fila de la lista dice «Cerrada ·». Si la
  orden se cerró mientras escribía, la burbuja queda «no enviado» con el motivo en llano
  (`motivoNoEnviado`) y solo «Descartar» (sin «Reintentar» mientras siga cerrada).
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
  `Sparkles`, solo índigo (sin morados), fondo de marca al 6 % sobre la tarjeta y borde de marca al
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
- **Modo botón:** «Sugerir» (`Sparkles`, fantasma en índigo) junto a «Enviar» en lugar de la tarjeta.
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
      índigo con `CircleCheck` (`aria-pressed`). Es una tira que se desliza a lo ancho si no cabe. El
      filtro va en la URL (`?categoria=2`) y «Quitar filtros» del popover no lo borra. La tira solo
      aparece cuando alguna conversación ya tiene categoría (sin asistente no hay categorías);
    - cada fila suma una tercera línea con el **chip** y la **actividad del asistente** (`Sparkles`
      en índigo + el texto corto que escribe el servidor);
    - bajo la cabecera de la conversación, la categoría con **«Cambiar»** (menú corto; con
      `conversaciones.categorizar`) y aviso emergente al cambiarla. Si otra persona cambió la
      conversación (409), se relee y dice «Alguien la cambió; inténtalo de nuevo» (igual en «Tomar»). Sin categoría y con el asistente
      activo dice «Sin categoría · Poner categoría»; con el asistente apagado no aparece nada;
    - **«El asistente está atendiendo»**: franja discreta en índigo con `Sparkles` y el botón
      «Tomar» (la acción de tomar de siempre: al tomarla una persona, el asistente se detiene);
    - los mensajes del asistente llevan la etiqueta **«Asistente»** con `Sparkles` en índigo y borde
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
- **Relevo a una persona:** la nota interna del asistente lleva antes una píldora índigo «El asistente
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

### El asistente por conversación: quién atiende, pausa y resumen guardado (nov. 2026)

Contrato: `conversaciones.md` §17.11 (pausa), §16.4 (resumen), §17.4 (indicadores). Primero en el
teléfono; `e2e/asistente-conversacion.spec.ts` en los 4 proyectos.

- **Quién atiende** (`atencionAsistente`; `lib/asistente/atencion.ts`, `FranjaAtencion`): una franja
  bajo la cabecera, con icono + texto + color. Teal: «El asistente atiende esta conversación».
  Neutro: «Asistente en pausa · Andrea, hace 5 min · caso delicado», tope, bot de menú, «solo
  sugiere». Ámbar (hay que actuar): «Nadie la está atendiendo: el asistente se apartó cuando una
  persona la tomó» y «El asistente la pasó a una persona porque…». Sin el campo no se pinta nada
  (con un servidor anterior queda la franja de siempre, por `asistente.estado`). En el teléfono el
  texto va arriba, completo, y los botones debajo.
- **Pausar / reanudar** (permiso `conversaciones.pausar_asistente`): «Pausar» de un toque (sin
  motivo) y, al lado, el menú «Pausar porque…» (caso delicado, respondió mal, lo pidió el
  cliente). «Reanudar» tras una pausa; «Que siga el asistente» cuando nadie la atiende. Con una
  persona asignada no hay franja: «Pausar el asistente aquí» va en «⋯»; cuando espera a una
  persona, «Que siga el asistente» también va en «⋯» (lo principal ahí es «Tomar»). **Antes de
  devolvérsela al asistente se avisa si el cliente pidió una persona o está molesto** («Dejarla
  como está» / «Devolverla al asistente»). 409: se dice y se relee; 422 `conversacion_resuelta`,
  en llano. Aviso después de cada acción.
- **Al quitar la asignación** de una conversación que el asistente atendía antes de que la persona
  la tomara: «¿Que la siga atendiendo el asistente?» («Sí, que siga» / «No, queda en cola»). No se
  ofrece si el cliente pidió una persona (`ofrecerQueSigaElAsistente`).
- **En la lista**: punto discreto «Asistente en pausa» y punto ámbar «Nadie la atiende», en la
  línea de la categoría.
- **Resumen guardado** (`ResumenAsistente`): si el detalle trae `resumen`, se lee al abrir la ficha
  (`GET …/resumen`, no gasta tokens) con «Generado por Andrea · hoy 10:42 · modelo». Con mensajes
  posteriores: «Hay 2 mensajes nuevos desde este resumen» y «Actualizar resumen». Si actualizar
  falla, el anterior sigue a la vista. Cuando `resumen` deja de venir (se reabrió, cambió de
  cliente), vuelve «Resumir». El resumen solo viene en el detalle: el panel lo relee cuando el
  flujo avisa un cambio de la conversación sin mensaje, o un mensaje habiendo resumen.
- **En el hilo**: «Andrea pausó el asistente: respondió mal», «Andrea le devolvió la conversación
  al asistente» y, en ámbar y sin recortar, «Nadie la tomó en 30 min y el bot no pudo contestar: el
  cliente no acepta mensajes o pasaron 24 h» (nunca «contestó el bot» cuando no contestó).

## Rechazo de Meta a la vista (nov. 2026)

Contrato: `conversaciones.md` §1.1 y `voz.md`. `e2e/rechazo-meta.spec.ts` en los 4 proyectos.

- **Mensaje que no se envió** (`FallaMensaje`, `lib/conversaciones/falla.ts`): bajo la burbuja, con
  icono + texto + color, por qué no salió. Si Meta rechazó el token o los permisos del número, al
  agente se le dice que avise a un administrador; a quien tiene `mensajeria.configurar`, que lo
  revise, con el enlace «Revisar el número» (a las credenciales de ese número). El código de Meta
  («Código de WhatsApp: 190 (HTTP 401)») va discreto y solo si el servidor lo manda.
- **Números de WhatsApp**: un envío rechazado marca el número y el primero aceptado lo devuelve a
  «Conectado» sin pulsar «Probar»; la lista se relee al volver a la pestaña, en silencio.
- **Llamadas del número**: aviso «Se están rechazando llamadas de clientes» (cuántas hoy y cuándo
  la última) con «Encender las llamadas»; el 409 al guardar ofrece «Recargar» y reintentar.

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

## Páginas legales y datos legales (nov. 2026)

Contrato: `.claude/integration/contratos/legal.md`. Lógica y texto en `apps/web/lib/legal/`.

- **Páginas públicas** (`/privacidad`, `/terminos`, `/eliminacion-de-datos` y `/en/privacy`,
  `/en/terms`, `/en/data-deletion`): `PaginaLegal` (`componentes/legal/pagina-legal.tsx`), un
  componente de servidor. Sin sesión, sin menú y **sin migas**: logo, título, titular, fecha de
  última actualización, índice (a la izquierda y fijo en escritorio; arriba en el teléfono), texto
  y pie con los otros dos documentos. El texto va en el HTML: se lee sin JavaScript.
- **Sin datos no hay huecos:** el texto usa un nombre genérico y las líneas de contacto vacías no
  se dibujan. Nunca «undefined», comillas vacías ni un error.
- **El texto no lleva datos de ningún titular:** llegan por parámetro (`titularDe`). Al cambiar el
  texto se sube `TEXTO_REVISADO_EN`.
- **Pie legal** (`PieLegal`): enlaces a los tres documentos en el pie del inicio de sesión y del
  portal de soporte.
- **Pantalla** `/administracion/datos-legales` (`datos_legales.ver` / `.configurar`):
    - arriba, un `Aviso`: verde «Datos completos…» o ámbar «Faltan datos legales: las páginas
      públicas no están listas para Meta», con la lista de lo que falta en palabras del formulario;
    - se puede guardar a medias (todo es opcional al guardar); el correo y el teléfono se validan;
    - «Enlaces para Meta»: las seis URL con el dominio del panel (`CampoCopiar`), «Abrir» en otra
      pestaña, dónde se pega cada una en Meta y un indicador por enlace (icono + texto + color):
      «Comprobando…», «Responde bien», «Responde, pero faltan datos» o «No responde». La comprobación
      pide la página sin cookies y exige la marca `data-pagina-legal`; «Volver a comprobar» la repite;
    - sin `https` avisa que Meta no acepta esos enlaces;
    - 409 con «Recargar», 422 junto al campo, lectura fallida con «Reintentar» y sin formulario. Con
      solo `.ver`, todo deshabilitado (los enlaces sí se copian). Quien no opera en todas las
      empresas ve un aviso: son datos de la instalación.

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
  el ticket abierto y «Saldo vencido» solo si el servidor lo dice (ausente no se muestra nada,
  nunca «al día»), «Rechazar» y «Contestar». Quien supervisa ve además «Rechazar para todos» (con
  confirmación) y, en el hilo, «Colgar la llamada» de otra persona. Suena un
  timbre (WebAudio). «Contestar» abre la conversación del cliente, salvo que estés en un
  formulario (no se pierde lo escrito: queda «Ver cliente» en la barra).
- **Si no se pudo contestar** (otra persona la tomó, venció el tiempo, micrófono, red, ya tienes
  otra llamada): un mensaje llano que se va solo a los 8 s. Si la tienes en otra pestaña, el aviso
  solo se cierra. Nunca un código ni un error técnico.
- **Nunca una llamada viva sin audio:** si esta pestaña deja una llamada que el servidor pudo
  aceptar, la cuelga (con reintentos) y se lo dice al agente («La llamada se conectó sin audio y se
  colgó»); si no logra colgarla, deja un aviso fijo con «Reintentar».
- **Irse con una llamada:** cerrar sesión o cambiar de empresa con una llamada viva o sonando
  pregunta antes («Tienes una llamada con…» · «Quedarme» / «Colgar y seguir») y la termina primero.
- **Llamada en curso:** barra bajo la barra superior (`data-slot="barra-llamada"`), en todas las
  pantallas: punto, nombre, tiempo, «Silenciar» y «Colgar» (rojo). Las pantallas de alto completo
  le ceden su alto (`--alto-llamada`).
- **Al terminar:** diálogo corto con la nota (opcional, queda como nota interna), «¿Cómo quedó?» y
  los accesos «Crear ticket» y «Agendar visita». «Ahora no» cierra. Si se cortó, lo dice.
- **En el hilo:** la llamada es una tarjeta centrada con icono + texto + color: contestada (verde),
  rechazada (neutro), fuera de horario y fallida (ámbar), perdida (rojo, la única que pide actuar:
  «Escribirle» y, si sigue pendiente, «Descartar»). El audio del buzón lleva «Mensaje de voz del
  buzón». Con las salientes (abajo), la perdida pendiente suma «Devolver la llamada».
- **En la lista:** la fila con una llamada por devolver lleva el chip rojo «Llamada perdida ·
  escríbele», y arriba se juntan como «N llamadas por devolver» (plegado es una línea).
- **Ajustes:** `/administracion/numeros/[id]/llamadas` (requisitos de Meta como «Listo / Falta /
  Recomendado / Sin comprobar», «pendiente de aplicar en WhatsApp» con «Reintentar», a quién le
  suena, horario y llamadas perdidas; un número por QR solo muestra por qué no puede) y
  `/administracion/llamadas` (de la empresa). **La grabación no se ofrece.**
- **«En pruebas»:** hasta validar las llamadas con Meta, los ajustes llevan arriba la franja «En
  pruebas» y los interruptores, la etiqueta. Se quita con una sola constante:
  `LLAMADAS_EN_PRUEBAS` en `componentes/voz/en-pruebas.tsx`.
- **Pruebas:** `lib/voz/*.test.ts` (máquina, textos, ajustes) y `e2e/voz.spec.ts` con
  `e2e/api-voz.ts`, `e2e/doble-webrtc.ts` (micrófono y `RTCPeerConnection` de mentira) y
  `page.clock` para los plazos de 12 s y 5 s.

### Llamar al cliente (salientes, voz E1b)

Contrato: `voz.md`, bloques «E1b hecha en el servidor» y «Revisión de seguridad de E1b». Todo lleva
la marca «En pruebas». El proveedor se monta también para quien solo llama (`llamadas.llamar` sin
`llamadas.contestar`): esa persona no tiene «Recibir llamadas».

- **Acceso:** el teléfono de la cabecera de la conversación (`BotonLlamada`, panel emergente
  «Llamar por WhatsApp»), solo con `llamadas.llamar` o `llamadas.solicitar_permiso`. El permiso se
  lee al abrir y se relee cuando el flujo trae algo de esa conversación.
- **Permiso del contacto, en llano (icono + texto + color):** «Sin permiso todavía», «El permiso venció el 10 de noviembre», «Permiso pedido», «Permiso concedido
  hasta el 24 de noviembre», «El cliente dijo que no», «WhatsApp quitó el permiso». Abajo, una
  línea fija: «WhatsApp solo deja que una empresa llame a quien aceptó recibir sus llamadas.»
  `motivo` se dice aparte (`llamadas_inactivas`, `no_autorizado`). Si la lectura falla: error con
  «Reintentar» y no se ofrece llamar ni pedir.
- **«Pedir permiso para llamarle»** (`llamadas.solicitar_permiso` y `puedeSolicitar`): el mensaje
  queda en el hilo con la etiqueta «Solicitud de permiso para llamar». Sus 422 en llano junto al
  botón: ya vigente, límite (con «a partir del 19 de noviembre a las 14:30»), falta la plantilla;
  BAJA y lo demás, con las palabras del servidor.
- **«Llamar»** (`llamadas.llamar` y `puedeLlamar`): la barra de la llamada dice «Preparando la
  llamada a…» y «Llamando a…» con «Colgar»; pasa a la barra de llamada en curso cuando el cliente
  contesta. Si no contesta (o rechaza) queda «No contestó» y en el hilo «Se le llamó y no contestó»
  en ámbar (no hay nada que devolver). Con una llamada en marcha no se empieza otra.
- **Tres sin contestar:** antes de la cuarta, un diálogo explica que si tampoco contesta WhatsApp
  quita el permiso («Llamar de todos modos» / «No llamar»). El panel también lo cuenta («Van 3
  llamadas seguidas sin contestar»).
- **Límites, en llano:** límite del día, «Hoy ya no se le puede llamar» (se colgaron varias antes
  de que contestara), tope de minutos del mes (también si corta una llamada en curso: se explica en
  la nota), ya hay una llamada, la devolución ya la cerró otra persona, WhatsApp no la aceptó.
- **Regla de oro, igual que en entrantes:** desde que sale `llamar` la llamada puede existir en el
  servidor, así que dejarla (colgar, error, irse) es colgarla, también si la respuesta se perdió.
  Contestada sin audio en 15 s, se cuelga. Cerrar la pestaña marcando manda `colgar?propia=true`.
  El flujo de eventos va siempre con el id del navegador **y el de la pestaña** (`X-Iris-Pestana`,
  128 bits en memoria) y sigue abierto mientras dure la saliente: el latido es de la pestaña que
  llamó, otra pestaña abierta no la sostiene. Al abrir el panel se lee siempre la llamada activa:
  una llamada propia viva que no es de esta pestaña se dice («Tienes una llamada abierta en otra
  pestaña, o que quedó sin audio») y «Colgar» pide confirmación («Si estás hablando en otra
  pestaña, se cortará» · «Dejarla»). Mientras se comprueba si quedó viva una llamada cuya respuesta
  se perdió, «Llamar» a ese contacto dice «Comprobando la llamada anterior…» y no deja llamar. Si la pestaña se cierra con `llamar` en vuelo deja una marca y otra pestaña, o la carga
  siguiente, busca esa saliente y la cuelga. Si el servidor la corta marcando (55 s sin el latido
  de su pestaña), se explica: «La llamada se cortó: se perdió la conexión con Iris».
- **«Devolver la llamada»:** en «N llamadas por devolver» y en la tarjeta de la perdida, junto a
  «Escribirle», solo para quien puede llamar. Lee el permiso **antes** de pedir el micrófono: sin
  permiso lo dice y no llama. Si el cliente contesta, deja de estar pendiente.
- **Ajustes:** en `/administracion/llamadas`, sección plegada «Avanzado» con los tres límites de
  las salientes y la nota de que se cobran por minuto (en las colgadas, 0 = sin límite; en los
  otros dos, 0 apaga las salientes); en `/administracion/numeros/[id]/llamadas`, «Permiso para
  llamar al cliente» con la plantilla y su idioma.
- **Consumo junto a los topes** (en «Avanzado»): «Llevas 120 de 300 minutos este mes» y «8 de 50
  llamadas hoy», con un punto que crece en ámbar al pasar del 80 % y en rojo al llegar al tope. Con
  el tope en 0 o sin el dato no se muestra nada.
- **Solicitud sin confirmar:** «Se envió la solicitud, pero WhatsApp aún no confirma que la
  recibió»; sigue contando y no se ofrece pedir otra.
- **Entrante que se dejó mientras conectaba** (`agente_salio`): es una perdida por devolver; en el
  hilo dice «… empezó a contestarla y se cortó antes de conectar» y en la lista «Se cortó al
  contestar · escríbele» (esto último solo mientras se sabe por el flujo en vivo).
- **Pruebas:** `lib/voz/saliente.test.ts` y `e2e/voz-salientes.spec.ts` (mismos dobles y reloj).

## Centralita Issabel (voz E4a, nov. 2026)

Contrato: `voz.md` §3.12 («E4a hecha»). Código: `lib/centralita`, `componentes/voz/centralita.tsx`,
`componentes/clientes/llamadas-centralita.tsx` y `app/(panel)/administracion/integraciones/centralita`.

- **Administración › Integraciones › Centralita:** sin centralita, «Conecta tu Issabel». Con ella: el
  estado (un punto verde si está conectada; crece en ámbar o rojo con lo que hay que hacer: sin
  conexión desde…, certificado cambiado con «Comparar y confirmar», permisos de más), dirección,
  usuario, contraseña guardada (nunca se muestra), «Probar conexión», encender o apagar (con
  confirmación) y las extensiones (asignar en un diálogo de 2 campos; quitar con confirmación).
- **Conexión con Issabel** (pantalla propia): nombre, dirección, usuario y contraseña; el puerto 5039
  en «Avanzado». **Probar antes de guardar:** «Guardar» queda deshabilitado hasta que la prueba pasa
  con los datos actuales y se marca «Es la misma que muestra mi Issabel» junto a la huella. Al guardar
  se prueba otra vez y se enciende. Cambiar dirección o usuario pide la contraseña de nuevo.
- **Aviso cuando suena tu extensión:** el mismo aviso de llamada de WhatsApp (`CabeceraLlamada`,
  `Senales`, `MARCO_AVISO`) con «Suena tu extensión 204 · número», **sin «Contestar»** (se contesta en el
  teléfono; E4c lo sumará). Reconocido: línea de servicio y ticket abierto con «Agregar a #…» y «Crear
  ticket». Varios posibles: «¿Quién llama?» y «Es este». Desconocido u oculto: «Buscar cliente» y
  «Crear ticket» (abre «Nuevo ticket» y, al crear, agrega la llamada). Contestó otra persona: «Contestó
  Luis (ext. 205)» y se cierra sola. Al colgar (si la contestaste tú): «¿Cómo quedó?» y una nota
  opcional. Nunca un nombre inventado: el CallerIDName no se usa.
- **Ficha y ticket:** «Llamadas de la centralita» bajo los datos del cliente y «Llamadas de este
  ticket» en la pestaña «Historial» (quién contestó, duración, extensión, ticket, resultado y nota).
- **Pruebas:** `lib/centralita/presentacion.test.ts` y `e2e/centralita.spec.ts` (con
  `e2e/api-centralita.ts`), en los 4 proyectos.

## Servidor de IA (nov. 2026)

`/administracion/servidor-ia` (contratos/voz.md §4.8, §4.11 y §4.12). Entrada en Administración y
tarjeta en Integraciones (junto a Argos y Horus). Código en `componentes/servidor-ia/` y
`lib/servidor-ia/`.

- **Quién ve qué.** Quien administra la instalación (`servidor_ia.ver` con `empresas.todas`) ve
  todo; con `servidor_ia.administrar`, además «Probar», Avisos y Mantenimiento. El admin de una
  empresa (`servidor_ia.ver_uso`) ve la línea de estado y **solo el uso de su empresa** (cuatro
  cifras, peticiones por minuto, tiempos y errores): nada de la tarjeta gráfica, los modelos, la
  saturación ni las otras empresas, y una nota que lo dice.
- **Tablero.** Línea de estado en llano («En línea · versión… · compatible… · encendido hace 6
  días»), periodos Ahora / 1 hora / 24 horas / 7 días, cuatro cifras con `Medidor` (uso, memoria de
  video, temperatura, peticiones en curso con las **rechazadas por saturación**: no hay cola),
  «El resto del servidor» (procesador, memoria del equipo y disco de los modelos; la potencia va
  junto al nombre de cada tarjeta), tres `GraficaSerie`, modelos, uso por empresa y errores
  recientes. Con varias tarjetas gráficas, un bloque por tarjeta. Con `gpus.estado: "omitido"`
  («sin tarjeta o sin lectura», no se sabe cuál) las cifras de GPU van en «Sin datos» con el texto
  neutro «El servidor no reportó la tarjeta gráfica en esta lectura»; «No hay tarjeta gráfica
  detectada» solo se dice con `gpus.estado: "sin_gpu"`. Modelos y uso son listas, no tablas: caben en
  390 px sin desplazarse a lo ancho. Si hay más empresas activas que turnos
  (`estado.plazasInsuficientes`), un aviso llano con enlace a Mantenimiento.
- **Quién entra.** Cada página pasa por `PuertaServidorIa` (permiso **y** `empresas.todas`, como la
  API): quien tiene `servidor_ia.administrar` sin `empresas.todas` ve «Sin permiso» y la pantalla no
  llega a pedir nada. Avisos y Mantenimiento solo aparecen en la navegación para quien administra.
  El `detalle` técnico del servidor solo se pinta a quien administra la instalación.
- **Refresco.** `useTablero` relee cada 10 s en «Ahora» (30 s y 60 s en los otros), sin pasar por
  «cargando», en pausa con la pestaña oculta y al instante al volver. Si falla, espera cada vez más
  (10 → 20 → 40 → 60 s) y vuelve a su ritmo al primer éxito; con 401, 403 o 404 no insiste: queda
  «Reintentar».
- **Nunca cifras viejas.** Si una relectura falla, las cifras se quitan y queda el error con
  «Reintentar». Una lectura de más de 60 s tampoco es «ahora», aunque llegue con `lecturaOk: true`;
  la antigüedad se mide contra la hora del servidor (cabecera `Date` de la respuesta), no contra el
  reloj del equipo; sin esa cabecera la tolerancia sube a 5 minutos. Caído, incompatible o con la última lectura fallida: las cifras de ahora dicen «Sin
  datos», «Visto por última vez hoy a las 10:42 (hace 20 min)», qué pasa con el asistente mientras
  tanto, qué revisar en el servidor y «Probar ahora». Incompatible dice **qué** actualizar según
  `motivoIncompatible`: Iris (`iris_antiguo`) o el servidor, con su comando y su porqué
  (`protocolo`, `iris_nuevo`, `paquete_antiguo`). Sin `versionDisponible` no se nombra versión.
- **Desactivado.** «Desactivar» (antes «Desenlazar») dice la verdad: Iris deja de usarlo y el enlace
  se conserva. En ese estado el tablero y Mantenimiento muestran `ReactivarServidorIa`: la salida
  principal es «Activar de nuevo» (`POST …/activar` con la huella guardada, que se confirma con la
  casilla; sin tocar el servidor) y la secundaria «Enlazar de nuevo» (si se cambió de servidor o se
  reinstaló). `/enlazar` acepta el estado y no hay «Probar». Para enlazar de nuevo basta
  `sudo iris-voz pair`; `unpair` es solo una nota opcional.
- **Enlazar** (`/enlazar`): cuatro pasos en una pantalla. Dirección → código de un solo uso (se
  muestra una vez, vence a los 15 minutos, se escribe en el servidor sin eco) → huella con la
  casilla «Coincide con la del instalador» → «Activar» con esa misma huella (un enlace sin activar
  caduca a las 24 h). Si el servidor cambió entretanto (409 `huella_cambiada`) se desmarca la
  casilla y se enseña la huella de ahora; nunca se reintenta solo. La marca guarda **qué huella**
  se comparó (no un sí/no) y la casilla se apaga mientras hay una petición en curso. 409 `rotacion_en_curso`, aquí y
  al rotar, es un aviso de «espera unos segundos», no un error. La huella se escribe **como la
  imprime el servidor** (`sha256:` y minúsculas seguidas) y, aparte, en grupos de cuatro para
  leerla. Si «Conectar» ya dijo que no son compatibles, se avisa en ese paso qué actualizar y
  «Activar» queda apagado.
  Si «conectar» falla, el servidor responde lo mismo (502) para un código usado o mal escrito que
  para un servidor fuera de alcance: el texto lo dice así, sin afirmar cuál fue. Un paso
  vencido (409 `enlace_vencido`, el mismo para el código y para activar) ofrece empezar de nuevo.
- **Comandos del servidor: siempre en inglés** (regla del PM; nunca en español). Viven en un solo
  sitio, `COMANDO` de `lib/servidor-ia/presentacion.ts`: `iris-voz pair`, `unpair`, `status`,
  `upgrade` y `model pull|allow|remove <nombre>`. Solo el texto que se copia; los rótulos y las
  explicaciones del panel siguen en español.
- **Avisos** (`/avisos`, `FormularioPagina`): los de la semana y, plegado en «Avanzado», los
  umbrales y cuántos días de historial se guardan, con «Valores de fábrica». «Servidor caído» y
  «Versión incompatible» no tienen umbral. «Ninguno en los últimos 7 días» solo se dice si la lista
  llegó; si no vino, «Sin datos». Un tipo de aviso desconocido se pinta genérico, no rompe.
- **Mantenimiento** (`/mantenimiento`): quién puede usarlo y simultáneas por empresa, modelos
  permitidos en dos niveles (el servidor declara los suyos; aquí se eligen «Todos los del servidor»
  o «Solo algunos»; descargar uno nuevo es un comando en el servidor, que se puede copiar),
  «Rotar credenciales» y «Revocar ahora» (cada confirmación dice qué pasa mientras tanto; si no
  llega respuesta: «No se pudo confirmar el cambio. Iris recupera la conexión sola…», se relee la tarjeta y el reintento repite la misma clave) y
  «Desenlazar» con confirmación.
- **Camino para `ver_uso`.** `servidor_ia.ver` y `.ver_uso` abren «Administración» en el menú: quien
  solo tiene `ver_uso` llega por ahí a su tablero de uso.
- **En el asistente de cada empresa** (`/administracion/asistente/conexion`): el proveedor
  «Servidor de IA» aparece si los ajustes traen `servidorIa`; se elige un modelo de los permitidos,
  sin dirección ni clave, con «Pruébalo antes de guardar». Si había una clave guardada de otro
  proveedor se avisa que se borra y se manda `clave: ""`. Si no está disponible, lo dice antes de
  guardar y ante el 422 `servidor_ia_no_disponible`. Con `servidor_ia` la API guarda `url: ""` y
  sin clave: «conectado» es proveedor + modelo (`proveedorConectado`), y los fixtures lo reflejan.
- **Campana.** `servidor_ia_caido`, `servidor_ia_incompatible` y `servidor_ia_umbral` tienen su
  icono y su segunda línea en `lib/campana.ts`; el título lo pone el servidor. Si el problema se
  repite en la hora (`datos.veces` > 1), la segunda línea añade «Ya van N veces en esta hora».
- **Tipos.** Todo sale de `packages/contratos` (`lib/servidor-ia/tipos.ts` son solo alias).

## Línea de servicio (nov. 2026)

Pedido del PM: «solo muestra el nombre y no el ip, precio, ubicación u router». Contrato con Argos
0.8.0 §2.4. Un solo componente, `componentes/clientes/linea-servicio.tsx`, identifica un servicio
de Argos donde se muestra o se elige:

- **`LineaServicio`** (completa): primera línea, el plan entero (envuelve, nunca se recorta) y el
  estado con `InsigniaEstado`; segunda línea, discreta, el precio («Q250.00 al mes»), la zona, el
  router, la IP, la dirección y el código. **`variante="compacta"`**: una línea que envuelve, para
  listas y resultados de búsqueda. Con varios servicios, el número («Servicio 2», el mismo del
  asistente).
- **`ListaServicios`**: todos los servicios del cliente en una tarjeta; el de la conversación lleva
  borde índigo y «De esta conversación». **`SelectorServicio`**: `GrupoOpciones` con `apiladas` y
  `contenido` (radios reales con la línea completa). **`NotaPermisosServicio`**: «Tu rol no ve…».
- **Permisos.** El servidor omite la red sin `servicios.ver_red` y la ubicación sin
  `servicios.ver_ubicacion`; el componente muestra solo lo que llega y nunca escribe «Sin IP» (el
  técnico solo ve la red de sus órdenes). Sin precio (Argos anterior a 1.18.0), nada: nunca «Q0».
- **Dónde:** ficha de la conversación, crear ticket, promesa, orden y boleta desde la conversación,
  selector de cliente (alta de ticket, nueva orden, asociar), «Datos del cliente / servicio» (ficha
  de ticket, de orden y alta comercial), ticket de origen de una orden y la app de campo
  (`lineaDeServicioCampo`). Adaptadores en `lib/datos/servicio.ts`.

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
- **Botón de las pantallas con firma, en una isla fija** (reporte del PM, nov 2026: «el botón lo
  tiene hasta abajo y no está flotante»): «Firmar y cerrar orden» y «Confirmar recepción» van en la
  misma píldora sólida que la isla de pasos, fija abajo sobre el área segura, con «Anterior» (o «Por
  recibir») redondo a la izquierda; reemplaza a la isla de menú o de pasos (sobre la isla no flota
  nada). La pantalla la registra (`RegistroAccionPie` de `@iris/sync/campo`, `useAccionPie`) y el
  marco la dibuja; el contenido deja su espacio debajo y nunca tapa el recuadro. Firmar no desplaza
  la pantalla (`touch-action: none` en la web; en la nativa el lienzo se queda con el gesto y apaga
  el desplazamiento mientras dura el trazo).
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
- **Isla de menú flotante** (píldora centrada abajo, sólida): Hoy · Por recibir · «+» en índigo ·
  Agenda · Yo, zonas de 54 px (44 encogida al bajar). Lo que pide atención va como píldora encima
  («Sin conexión · 2 pendientes por enviar», un rechazo) y lleva a «Por enviar».
- **«+»:** hoja de acciones rápidas con lo que tiene API (agregar formulario, con «Deshacer»); lo
  que no la tiene va deshabilitado con «Próximamente».
- **Botones en píldora, avisos que entran desde arriba con su acción.** (Radios y colores: ver «App
  del técnico: rediseño».)
- La lógica de pantalla es compartida: `@iris/sync/campo` `inicio.ts`.

## App del técnico: rediseño (nov 2026, fase 1, web)

Capturas aprobadas «campo-hoy» y «campo-orden». Solo cambia la presentación: la lógica y el motor
de sincronización (`@iris/sync`) no cambian.

- **Colores:** `campo.css` apunta sus variables a los `--iris-*`; lo elegido (día, botón de la isla)
  va en índigo tenue y las acciones principales en índigo (`variante="primario"`; la «tinta» negra
  de la versión anterior se fue con el rediseño). Tarjetas de 22 px de radio, sólidas.
- **Encabezado:** fecha y, a la derecha, la nube y el GPS (discretos), el «?» del manual de la vista
  (`/campo/manual/<pantalla>`, abre sin señal), el chat del equipo (con su punto de no leídos;
  antes estaba en la isla) y buscar. Debajo, «Hola, <nombre>» y Hoy · Mañana · Semana como control
  segmentado (riel de relleno, la elegida en una píldora blanca con texto índigo).
- **Isla de menú** (píldora centrada abajo): Hoy · Por recibir · «+» (círculo índigo de 52 px) ·
  Agenda · Yo, cada una con su ícono y su nombre; la activa en índigo tenue. «Por recibir» también
  sigue en su mosaico de Hoy (con el contador leído).
- **Tarjeta de la orden en curso:** borde índigo de 1,5 px, «Cómo llegar» (contorno) y la acción
  principal («Empezar trabajo» / «Continuar trabajo») mitad y mitad, y «Llamar» redondo si hay
  teléfono. El mosaico «Por recibir» con traslados pendientes va en índigo tenue.
- **Orden:** la acción principal queda fija abajo (`AccionFija`, `data-slot="accion-fija-orden"`)
  en lugar de la isla; «Volver» está arriba (un dato, una vez).
- **Escritorio (1280 px):** una columna centrada de 480 px con bordes a los lados.
- **App nativa (`apps/movil`):** sus tokens (`src/tema/tokens.ts`) toman los mismos colores; su isla
  y sus pantallas no cambian en esta fase.

## Escáner de serie y MAC (igual en la web y en la app nativa)

Las etiquetas de ONU, routers y antenas traen varios códigos (S/N, MAC, GPON SN, modelo, EAN, a
veces un QR). El escáner nunca acepta en silencio el primero que ve:

- **Por campo:** «Número de serie» y «MAC» llevan cada uno su botón (`ScanLine`, «Escanear número
  de serie» / «Escanear MAC», 44 px o más). El visor abre con su objetivo: «Escaneando: **Número de
  serie**» y «Apunta solo al código junto a S/N» (o «junto a MAC»).
- **Visor:** la cámara se oscurece salvo un recuadro al centro con esquinas índigo; solo cuentan los
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

## Listas y fichas del rediseño (fase 2, nov. 2026)

- **Listas en `ListaFilas`:** tickets, órdenes (vista «Lista»; «Columnas» sigue con tarjetas por
  estado), usuarios, prospectos (lista, también en el teléfono), plantillas de formulario y
  plantillas de WhatsApp. Cada fila abre la ficha (enlace) y lleva su menú `⋯` («Acciones de
  <nombre o número>»); tickets, órdenes y prospectos tienen además «Vista rápida».
    - Cargando, error con «Reintentar» y vacío van dentro de la misma tarjeta (`vacio`); el pie dice
      «Mostrando n …» o «No hay más ….», con «Cargar más» o, si falló, el error con «Reintentar»
      (`errorMas`). En el panel lo arma `pieListaFilas` / `piePaginado` (`componentes/pie-lista.tsx`).
    - En el teléfono los estados que hay que ver (SLA, prioridad, pendientes en Argos) bajan a su
      línea bajo el título (`extremoTelefono="debajo"`); por omisión el extremo se oculta.
    - Tickets: el buscador y los filtros son la cabecera de la lista y la selección múltiple su
      barra; al cambiar los filtros la lista vuelve a leer sin remontarse (no se pierde el foco).
    - Usuarios: «Reiniciar MFA» y «Restablecer contraseña» pasan al menú `⋯` de la fila (sección
      «Acceso»); solo aparecen cuando se pueden hacer.
- **Fichas con `FranjaFicha` y `PestanasSegmentadas`:** ticket (la barra de acciones de siempre
  es la acción de la franja), orden, técnico, prospecto, usuario y plantilla de formulario. Lo que
  dice la franja no se repite en las pestañas (el MFA y el estado del usuario salen de «Datos»).
- **Siguen como `Tabla` (con motivo):** los reportes (cifras por columna que se comparan entre
  filas), las líneas de una cotización (documento con cantidades, precios y totales) y las cifras
  del estado de Argos (dato y número, sin filas que abrir). No son listas de cosas que se abren.

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
| Voz E1b (en revisión) | Llamar al cliente: permiso del contacto, «Llamar», «Devolver la llamada», límites y ajustes | I3 |
| 6 · Portal de clientes | Mis casos, nuevo caso y conversación con el mismo estilo | I3 |
| Rediseño 1 (esta PR) | Tokens `--iris-*` (índigo/cian), logo en degradado, piezas nuevas de `@iris/ui`, marco del panel (menú isla, encabezado sin barra, scroll por dentro, «?» del manual) y la app del técnico | rediseño |
| Rediseño 2 (en revisión) | Las listas a `ListaFilas` (con `VistaRapida` y `MenuAcciones`) y las fichas a `FranjaFicha` + `PestanasSegmentadas` (ver «Listas y fichas del rediseño») | rediseño |

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
