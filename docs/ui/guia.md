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
| `SelectorFecha`, `SelectorRangoFechas` | `DatePicker`, `DateRangePicker` | |
| `Autocompletar` | `ComboBox` | |
| `Entrada`, `Campo` | `Input` | Un `<input>` nativo: sirve con `register` |
| `AreaTexto`, `CampoArea` | `TextArea` | |
| `Etiqueta` | `Label` | |
| `ProveedorRutas` | `RouterProvider` de React Aria | |
| `BarraGuardar`, `FormularioPagina`, `SeccionFormulario` | — | Composición propia de Iris con `Boton` |

`useCambiosSinGuardar(isDirty, isSubmitting)` sirve igual dentro de `DialogoProtegido` que dentro de
`FormularioPagina`.

### Lo que no se usa de HeroUI (y por qué)

| Envoltorio o componente | Por qué |
| --- | --- |
| `Selector` y `CampoSelector` siguen siendo un `<select>` nativo (con el estilo de campo de HeroUI), no el `Select` | El `Select` de HeroUI dibuja un botón y una lista propia y deja un `<select>` oculto: `register` de react-hook-form dejaría de recibir el evento nativo y `reset` no movería la opción elegida; unas 80 pruebas usan `selectOption` sobre el control nativo; y en el teléfono el `<select>` nativo abre el selector del sistema, que es más cómodo para el técnico. Para listas largas o búsqueda está `Autocompletar` (`ComboBox`) |
| `Table.Content`, `Table.Header`, `Table.Row`, `Table.Cell` (la rejilla de React Aria) | Es una colección con `role="grid"` y teclado propio: las filas de Iris llevan enlaces y botones libres y se arman con condiciones. Se usan `Table` y `Table.ScrollContainer` y las piezas de estilo; la rejilla queda para cuando una pantalla necesite orden o selección |
| `CloseButton` y los íconos por defecto de `Alert`, `Checkbox`, `Toast`… | Traen sus propios SVG; la identidad pide solo íconos de Lucide. Se pasan los de Lucide (`X`, `Check`, `CircleCheck`, los del tono) |
| `Pagination` | Las listas de Iris paginan por cursor (`?cursor=&limit=`, regla 7): no hay número de páginas. Sigue «Cargar más» (`PieLista`) |
| `SearchField` de HeroUI en el buscador global | El buscador es un `Autocomplete` de React Aria con foco virtual; usa el `SearchField` de React Aria que conecta con él. Cambiarlo no mejora nada visible |
| `Switch`, `Avatar`, `ProgressBar`, `Accordion`, `Slider`, `NumberField`, `InputOTP`… | Hoy ninguna pantalla del panel tiene esa pieza (no hay interruptores, fotos de perfil, barras de progreso ni acordeones): usarlos sería rediseñar pantallas. Están disponibles para agregarlos a `packages/ui` con nombre en español cuando una pantalla los pida |
| `Camara`, `VisorEscaner`, `LienzoFirma`, `LogoIris`, `MarcaIris` | No tienen equivalente en HeroUI (cámara con `<input capture>`, escáner con `BarcodeDetector`/ZXing, firma con pointer events, logo). Siguen como estaban, con los tokens del tema y `Boton` por dentro |
| Animación de salida de modal, hoja y confirmación | Se desmontan en el acto: con salida animada, un Escape o un clic inmediato le llegaría a la capa que se está yendo |

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
  asignar, En espera, Todos y las propias), filtros como chips (listas nativas) y, con
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
