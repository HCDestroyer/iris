# Auditoría: todo con componentes de HeroUI (nov. 2026)

Regla del PM: **toda la interfaz usa componentes de HeroUI v3, siempre envueltos en `@iris/ui`**
(`packages/ui`). Las pantallas importan de `@iris/ui`, nunca de `@heroui/react`. Un control propio
solo existe si HeroUI no lo trae, y con su motivo escrito (ver «HeroUI primero» en `guia.md`).

Esta auditoría sigue a la de controles de formulario (`guia.md`, «Auditoría de controles»), que ya
había pasado a HeroUI los `<select>`, las fechas y horas, los combobox, la búsqueda, los
interruptores, los números y los avatares.

## Cómo se buscó

Sobre `apps/web` (sin pruebas) y `packages/ui/src/componentes`:

- **Elementos crudos:** `<button>`, `<input>`, `<select>`, `<textarea>`, `<table>`, `<dialog>`,
  `<details>`/`<summary>`, `<progress>`, `<meter>`, `<a>` con `onClick` o `role="button"`.
- **Controles hechos a mano:** `role="switch|tab|tablist|menu|menuitem|tooltip|progressbar|meter|radio|radiogroup|checkbox|listbox|option|combobox|dialog|alertdialog|grid"`,
  `animate-spin` y `animate-pulse` (giros y esqueletos), `aria-expanded` (desplegables), círculos
  de iniciales, portales y `onMouseEnter` (globos).
- **Imports directos:** `@heroui/*` y `react-aria-components` fuera de `packages/ui`: **ninguno**.
- **Envoltorios de `packages/ui` que no importan `@heroui`:** uno por uno, para ver si componen
  otro envoltorio que sí lo usa o si dibujan un control propio.

## Resultado

### Migrado

| Archivo | Qué era | Componente de HeroUI | Decisión |
| --- | --- | --- | --- |
| `apps/web/app/(panel)/tickets/lista-tickets.tsx` | `<table>` crudo de la bandeja (escritorio) | `Table` + `Table.ScrollContainer` (por `Tabla*`) | Migrar. Mismas columnas, `aria-label="Tickets"`, `data-ticket`, `aria-selected` y casillas |
| `apps/web/app/(panel)/administracion/integraciones/argos/estado-argos.tsx` (2) | `<table>` crudas de «Datos de Argos en Iris» y «Qué falló» | `Table` (por `Tabla*`) | Migrar. Filas de dato y cifra sin realce al pasar el puntero |
| `…/argos/estado-argos.tsx` (etapa en curso), `componentes/despacho/mover-orden.tsx`, `componentes/comercial/comun.tsx`, `app/(panel)/administracion/numeros/nuevo/qr/qr.tsx` (2) | Icono `Loader`/`LoaderCircle` con `animate-spin` | `Spinner` (por `Cargando`; `decorativo` si el texto ya está al lado) | Migrar |
| `app/(panel)/ordenes/[id]/evidencias.tsx` (2) | `<span>` con `animate-pulse` mientras cargan fotos y posiciones | `Skeleton` (por `Esqueleto`) | Migrar |
| `app/(panel)/inicio.tsx` (avance del técnico), `app/(panel)/administracion/asistente/conexion/formulario.tsx` (tokens del mes), `app/(panel)/administracion/numeros/[id]/conectado/conectado.tsx` (historial de WhatsApp), `componentes/manuales/cargar-inicio.tsx` (carga de manuales), `app/campo/_componentes/detalle-orden.tsx` (pasos de la orden) | `role="progressbar"` hecho a mano | `ProgressBar` (nuevo `BarraProgreso`; `pasos` para los segmentos de la orden) | Migrar. Mismo nombre, `aria-valuenow` y `aria-valuetext` |
| `app/(panel)/inicio.tsx` (SLA por categoría) | `role="meter"` hecho a mano | `Meter` (por `Medidor`) | Migrar |
| `packages/ui/src/componentes/grafica-serie.tsx` → `barra-progreso.tsx` | `Medidor`: `role="meter"` propio | `Meter` | Migrar. Sin dato (`parte: null`) dibuja el riel de `Meter` sin `aria-valuenow`: el `Meter` de React Aria siempre anuncia un valor y aquí no se inventa un cero |
| `packages/ui/src/componentes/vista-previa-archivos.tsx` | Barra de subida `role="progressbar"` | `ProgressBar` (por `BarraProgreso`) | Migrar |
| `app/campo/_componentes/tareas.tsx` | `role="radiogroup"` con `Boton role="radio"` (duración de la tarea) | `ToggleButtonGroup` + `ToggleButton` (por `GrupoSegmentado`) | Migrar. Sigue siendo `radiogroup`/`radio` con `aria-checked`, con el aspecto de la app de campo |
| `app/(panel)/administracion/integraciones/centralita/conexion/formulario.tsx` (2), `…/centralita/telefono/formulario.tsx`, `componentes/telefono/indicador.tsx` | `<details>`/`<summary>` | `Disclosure` (nuevo `Desplegable`, chevron de Lucide) | Migrar |
| `packages/ui/src/componentes/teclado-telefono.tsx` | `<button>` crudos (12 teclas y controles de la llamada) | `Button` (por `Boton`) | Migrar. `aria-pressed` en silenciar y en espera, `data-activo`, 56 px |
| `packages/ui/src/componentes/fotos.tsx` | `<button>` crudo que abre la foto; giro hecho a mano de la foto pendiente | `Button` (por `Boton`); `Spinner` | Migrar. Mismo `data-slot="foto-mensaje"`, `data-estado-foto` y nombre accesible |

### Se conserva (con su motivo)

| Archivo | Qué es | Equivalente de HeroUI | Decisión |
| --- | --- | --- | --- |
| `packages/ui/src/componentes/camara.tsx` | `<input type="file" capture>` oculto en un `<label>` con aspecto de botón | — | Conservar: HeroUI no abre la cámara del teléfono. El aspecto viene de `variantesBoton` (clases del `Button` de HeroUI) |
| `packages/ui/src/componentes/selector-archivo.tsx`, `menu-archivos.tsx` | `<input type="file">` ocultos | — (el menú ya es `Dropdown`) | Conservar: HeroUI no trae selector de archivos; el disparador es `Boton`/`variantesBoton` |
| `packages/ui/src/componentes/escaner.tsx` (`VisorEscaner`, `HojaEscaner`) | Visor de cámara (`role="dialog"` propio) con `BarcodeDetector`/ZXing, y la hoja de códigos detectados sobre el video | — | Conservar: video en vivo con recuadro de lectura; la hoja es parte del visor (otro `Drawer` modal con su telón taparía el video). La hoja estaba en `app/campo/_componentes/escaner-equipo.tsx` y pasó a `packages/ui` como `HojaEscaner` |
| `packages/ui/src/componentes/lienzo-firma.tsx`, `firma.ts` | Lienzo de firma con pointer events | — | Conservar: HeroUI no tiene firma |
| `packages/ui/src/componentes/grafica-serie.tsx` | Gráfica SVG propia | — | Conservar: HeroUI no tiene gráficas (la tabla «Ver tabla» ya es `Tabla`) |
| `packages/ui/src/componentes/marca.tsx` | Logo de Iris en SVG | — | Conservar: identidad, no un control |
| `packages/ui/src/componentes/control-nativo.tsx` | `<input>` oculto que conecta `register` de react-hook-form con `Select`, `DatePicker`… | — | Conservar: es el puente con el formulario, no un control visible |
| `packages/ui/src/componentes/campo.tsx`, `campo-secreto.tsx`, `aviso.tsx`, `insignia-estado.tsx`, `estado-vacio.tsx`, `formulario-pagina.tsx`, `vista-previa-archivos.tsx` | Composición de otros envoltorios | `Input`, `Alert`, `Chip`, `EmptyState`, `Button`, `Modal` (por `Entrada`, `AlertaAviso`, `ChipEstado`, `RaizVacio`, `Boton`, `Dialogo`) | Conservar: ya usan HeroUI por dentro (los de servidor llevan la pieza de HeroUI en un archivo `"use client"` aparte) |
| `packages/ui/src/componentes/ranura.tsx` | `asChild` (pasa clases y eventos al hijo) | — | Conservar: utilidad de composición, no dibuja nada |
| `packages/ui/src/componentes/panel-lateral.tsx` | Panel flotante no modal (`role="dialog" aria-modal="false"`) | `Drawer` es modal | Conservar: excepción aprobada por el PM (la página de atrás sigue usable; ver `guia.md`) |
| `packages/ui/src/componentes/lista-sugerencias.tsx` y `apps/web/componentes/chat/compositor.tsx` (`role="combobox"`) | Sugerencias de `@menciones` bajo un área de texto | `ComboBox`/`Autocomplete` reemplazan el valor de un campo de una línea | Conservar: el foco sigue en el `AreaTexto` (foco virtual con `aria-activedescendant`) |
| `packages/ui/src/componentes/lista-recortada.tsx` | Una línea de chips con «+N» | `TagGroup` no recorta ni cuenta | Conservar: solo acomodo; los chips son `InsigniaEstado` (`Chip`) |
| `packages/ui/src/componentes/fotos.tsx` (pulso mientras carga) | `<span>` con `animate-pulse` dentro del botón de la foto | `Skeleton` | Conservar: `Skeleton` es un `<div>` y dentro de un `<button>` solo cabe contenido en línea |
| `apps/web/componentes/despacho/calendario.tsx`, `componentes/agenda/vista-mes.tsx` | `role="grid"` de la agenda (arrastrar para programar, «Mover a…») | `Calendar` elige fechas, no muestra citas | Conservar: es una agenda, no un selector de fecha; las celdas usan `Boton` |
| `apps/web/componentes/voz/llamada.tsx`, `componentes/telefono/llamada.tsx` | Aviso de llamada entrante (`section role="alertdialog"` flotante, no modal) | `AlertDialog` es modal; `Toast` se va solo | Conservar: debe sonar y quedarse a la vista sin bloquear la pantalla; botones `Boton` |
| `app/(panel)/tecnicos/[id]/ficha-tecnico.tsx`, `componentes/equipo/cambiar-estado.tsx`, `componentes/cierre-seguro.tsx` | `DialogoContenido role="alertdialog"` | `Modal` (ya es HeroUI) | Conservar: ya es el `Modal` de HeroUI; solo cambia el rol |
| Botones «Avanzado» / «Ver revisiones» con `aria-expanded` (`asistente/limites`, `asistente/visitas`, `llamadas/ajustes`, `servidor-ia/avisos`, `numeros/checklist-meta`, `voz/devoluciones`, `comercial/[id]/cotizacion`) | `Boton` que muestra un bloque más abajo | `Disclosure` | Conservar: ya son `Button` de HeroUI y el contenido no va pegado al botón (ver «Lo que no se usa») |
| Iconos `RefreshCw` que giran en «Actualizar», «Probar», «Sincronizar» | Icono de la acción animado dentro de un `Boton` | `Button isPending` | Conservar: `isPending` cambia el icono por un `Spinner` y bloquea el botón (otro comportamiento); el giro es del icono de Lucide |
| Puntos con `animate-pulse` (en vivo, sonando, sin señal) | Indicador de estado de un punto | — | Conservar: es el punto discreto de la regla 6, no un esqueleto |
| Marcadores de MapLibre (`componentes/despacho/mapa.tsx`, `comercial/mapa-comercial.tsx`) | Portales dentro del mapa | — | Conservar: los dibuja MapLibre |
| `<img>` de fotos y adjuntos | Imágenes | — | Conservar: no son controles |
| Paginación | «Cargar más» (`PieLista`) | `Pagination` | Conservar: la API pagina por cursor, sin número de páginas |

## ESLint

`apps/web/eslint.config.mjs`:

- `no-restricted-syntax` prohíbe `<button>`, `<input>`, `<select>`, `<textarea>`, **`<table>`** y
  **`<dialog>`** crudos, y el mensaje dice qué envoltorio usar.
- También prohíbe `<details>`, `<summary>`, `<progress>` y `<meter>`; `createElement("button" | "input" |
  "select" | "textarea" | "table" | "dialog" | "details")`; guardar una de esas etiquetas en una
  variable para dibujarla después; `role` con `progressbar`, `meter`, `radiogroup`, `radio` o
  `dialog`; y `import()` de HeroUI o React Aria. Cada mensaje nombra el envoltorio de `@iris/ui`.
- `no-restricted-imports` prohíbe **`@heroui/*`**, **`react-aria`**, **`react-aria-components`**,
  **`@react-aria/*`** y **`@react-stately/*`** fuera de `packages/ui`.
- `reportUnusedDisableDirectives: "error"`: un `eslint-disable` que ya no tapa nada no se queda.
- No hay excepciones: todo `apps/web` pasa sin ninguna.

## Pendiente

Nada de lo marcado como «migrar» quedó sin hacer.
