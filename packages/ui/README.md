# @iris/ui

Componentes compartidos de Iris sobre **HeroUI v3** (React Aria + Tailwind 4) con la identidad
Horus «vidrio y luz». Fuente TS sin build: la app lo transpila (`transpilePackages: ["@iris/ui"]`).
Las pantallas importan de aquí, nunca de `@heroui/react`. Decisiones, mapa de componentes y plan
por fases: [`docs/ui/guia.md`](../../docs/ui/guia.md).

## Estilos y tema

```css
@import "tailwindcss";
@import "@iris/ui/estilos.css";
@source "../../../packages/ui/src";   /* ruta relativa al CSS de la app */
```

`estilos.css` importa, en este orden:

1. `@heroui/styles`, con los componentes y su tema por defecto;
2. `horus-tokens.css`, copia exacta de `docs/marca/horus-tokens.css` (una prueba lo verifica; si
   cambia la marca, copia también `src/marca/logo-mark.svg`);
3. `tema-heroui.css`, que apunta cada variable de HeroUI a un token `--hx-*`, sin colores nuevos:
    - `accent` y `focus` → teal;
    - `success`, `warning` y `danger` → los de Horus;
    - `surface` → tarjeta casi sólida;
    - `overlay` → vidrio fuerte (menús, popovers, modales y avisos, con desenfoque);
    - radios: 6 px en controles y 14 px en tarjetas y capas.

Utilidades:

- de HeroUI: `bg-background`, `text-foreground`, `text-muted` (texto atenuado), `bg-default`,
  `bg-surface`, `bg-background-secondary` (el fondo tenue; antes `bg-muted`), `success`, `warning`,
  `danger`, `border-border`;
- de Iris: `text-muted-foreground`, `text-dimmed`, `bg-elevated`, `bg-card`,
  `bg-primary`/`text-primary-foreground`, `info`, `error`, `rounded-card`, `shadow-glow`.

Materiales: `glass-material` (barras), `glass-strong` (menús, hojas), `surface-card` (contenido) y
`app-aura` (fondo).

El tema oscuro va por la clase `.dark` en `<html>` (next-themes) o por `[data-theme="dark"]`, con la
variante `dark:`. La variante de Iris reemplaza la de HeroUI, que también seguía
`prefers-color-scheme`.

## Componentes

Mismos nombres y props de siempre; por dentro, HeroUI:

- `Boton`: `<button>` nativo con `buttonVariants`: primario con halo, secundario, contorno,
  fantasma, peligro y enlace; `asChild`.
- `Entrada`, `Campo`: HeroUI `Input`.
- `AreaTexto`, `CampoArea`: `TextArea`.
- `Etiqueta`: `Label`.
- `Selector`, `CampoSelector`: `<select>` nativo con el estilo de campo de HeroUI; en el teléfono
  abre el selector del sistema.
- `Dialogo*`: `Modal` de React Aria, en vidrio fuerte; en el teléfono sube desde abajo.
  `DialogoContenido cerrable={false}` no se cierra con Escape ni tocando fuera, y no tiene «X»
  (`DialogoPie`).
- `Hoja*`: `Drawer`.
- `MenuDesplegable*`: `Dropdown`.
- `Tarjeta*`: piezas de `Card`.
- `Aviso`: piezas de `Alert`, con tono + icono + título + acción (p. ej. «Reintentar»).
- `InsigniaEstado`: `Chip` suave, con icono + texto + color.
- `Tabla*`: `<table>` con las piezas de `Table`.
- `EstadoVacio`: `EmptyState`.
- `Esqueleto`: `Skeleton`.
- `Cargando`: estilo de `Spinner`.

Siguen como estaban:

- `GrupoSegmentado` (radiogroup);
- `Casilla` (checkbox con etiqueta);
- `GrupoOpciones`;
- `LogoIris`, `MarcaIris`;
- `cn`.

Nuevos (fases 1 a 3). Ya en el panel (fase 1): `Migas`, `ProveedorRutas`, avisos emergentes,
`Globo`, `PaletaBusqueda` y `MenuEnLinea`; los demás llegan con las fases 2 y 3:

- `Pestanas`: HeroUI `Tabs`. Las pestañas pueden tener `href`, para fichas con URL; controladas
  con `seleccionada`/`alCambiar` (la pestaña en `?pestana=`). Si la elegida no está, abre la primera.
- `PanelProtegido`: acción en un panel lateral a la derecha (`Hoja` sin velo, la ficha a la vista),
  con la misma protección de cambios que `DialogoProtegido`; por dentro, `DialogoPie` y
  `DialogoCerrar`. `HojaContenido` acepta `descripcion` y `fondo="transparente"`.
- `ProveedorAvisosEmergentes` + `avisar` / `useAvisoEmergente` / `cerrarAviso`: HeroUI `Toast`, en una
  cola fuera de React. Duran 5 s, u 8 s si llevan «Deshacer». `className` permite moverlos (el
  panel los pone arriba en el teléfono). HeroUI anuncia cada aviso como `alertdialog` no modal: en
  las pruebas, las confirmaciones se buscan sin contarlos (`confirmacion(page)` en
  `apps/web/e2e/ayudas.ts`).
- `Migas`: `Breadcrumbs` dentro de `<nav aria-label="Ruta de navegación">`. La última miga es la
  pantalla actual. En el teléfono solo queda el enlace a la anterior («‹ Órdenes»), y la actual
  sigue para lectores de pantalla.
- `ProveedorRutas`: el `RouterProvider` de React Aria. Va en el layout con `navegar={router.push}`.
- `SelectorFecha`, `SelectorRangoFechas`: `DatePicker` / `DateRangePicker`. Valores `AAAA-MM-DD`,
  formato `es-GT`, y `hoyEnGuatemala()` para «hoy».
- `Autocompletar`: `ComboBox`. Filtra en el navegador, o busca en el servidor con `alBuscar`.
- `Globo`: `Tooltip`. Usa al hijo como disparador (`Focusable`).
- `PanelEmergente`: `Popover`, un diálogo no modal junto a su disparador.
- `PaletaBusqueda`: el buscador global (Ctrl/⌘ + K), con el `Autocomplete` de React Aria. El foco
  queda en el campo y las flechas recorren los resultados por secciones (foco virtual); Enter elige.
  Debajo muestra el estado («Buscando…», errores con «Reintentar»). Quien lo usa decide qué buscar.
- `MenuEnLinea`: las mismas opciones de `MenuDesplegable`, siempre a la vista (p. ej. la cuenta en
  «Más» del teléfono), con `role="menu"`.
- `BarraGuardar`: pie fijo con «Cancelar» y «Guardar», con el área segura del teléfono.
- `FormularioPagina` + `SeccionFormulario`: formulario en pantalla propia, con título (que nombra
  al `<form>`), secciones, barra y migas propias opcionales (en el panel las pone el layout). Al
  salir con cambios sin guardar (enlaces, «Cancelar», «Atrás» o recargar) pregunta antes, con
  `useProteccionSalida`; la protección sigue mientras se envía, por si el envío falla.

App de campo: `Camara` (botón que abre la cámara del teléfono con `<input type="file"
capture>`, oculto pero enfocable), `LienzoFirma` (firma con el dedo por pointer events, tinta del
tema en pantalla) con `trazosASvg` (≤ 200 000 caracteres, el tope de la API) y `trazosAPng`
(negro sobre transparente), y `Escaner` (cámara trasera + `BarcodeDetector` nativo; si no
existe, ZXing cargado bajo demanda; sin cámara o sin permiso explica y deja escribir a mano). No
tienen equivalente en HeroUI: siguen igual, con los tokens del tema. Tonos: `neutral | info |
success | warning | error` (nunca `primary` como estado). Íconos solo de Lucide.

## Formularios en modales (`formulario-modal.tsx`)

Solo para acciones cortas (1 a 3 campos); los formularios largos van en `FormularioPagina`.

Patrón obligatorio para todo formulario en un diálogo del panel (pedido del PM: «los modales
deben alertar si no hay selección o impedir guardar»). No depende de react-hook-form: la app le
pasa `isDirty`, lo que falta y `setError`.

- **`DialogoProtegido`**: reemplaza a `Dialogo` (controlado: `open` + `onOpenChange`). Si hay
  cambios sin guardar, Escape, tocar fuera, la X o un «Cancelar» envuelto en `DialogoCerrar`
  piden «¿Descartar los cambios?» antes de cerrar. Cerrar por código (tras guardar) no pregunta.
  Los cambios llegan por `sucio` (cuando el formulario vive en el mismo componente que el
  diálogo) o con **`useCambiosSinGuardar(form.formState.isDirty)`** desde un formulario hijo.
  `ocupado` (o el segundo argumento del hook, `isSubmitting`) impide cerrar o descartar mientras
  se envía.
- **`BotonEnviar`**: `type="submit"`, deshabilitado mientras `enviando` o mientras `faltan` tenga
  selecciones obligatorias (`["el técnico"]` → «Falta elegir el técnico para guardar.», o
  `motivo` propio), con el mensaje visible junto al botón y enlazado por `aria-describedby`. El
  campo lleva además su `ayuda`/`error` («Elige el formulario que llenará el técnico»).
- **`DialogoConfirmar`** (`AlertDialog` de HeroUI / React Aria): confirmación de acciones destructivas (`peligro`:
  desactivar, quitar, descartar). Foco inicial en «Cancelar»; no se cierra tocando fuera; con
  `alConfirmar` asíncrono deshabilita los botones mientras corre.
- **`asignarErrores(errors, campos, setError)`**: pone cada `errors[]` del `problem+json` en su
  campo (`body.lineas[0].precio` → `lineas.0.precio`, `campoDelFormulario`) y devuelve los que no
  tienen campo; **`ErrorFormulario`** los muestra en el modal (`role="alert"`) con título y detalle.
- Botón `variante="peligro"` para la acción destructiva.

```tsx
<DialogoProtegido open={abierto} onOpenChange={setAbierto} sucio={form.formState.isDirty} ocupado={form.formState.isSubmitting}>
    <DialogoContenido titulo="Asignar técnico">
        <form noValidate onSubmit={form.handleSubmit(guardar)} className="grid gap-4">
            <CampoSelector etiqueta="Técnico" ayuda={sinTecnico ? "Elige quién hará la visita." : undefined} {...form.register("tecnicoId")}>…</CampoSelector>
            {error ? <ErrorFormulario titulo="No se pudo asignar" detalle={detalle} errores={restantes} /> : null}
            <DialogoPie>
                <DialogoCerrar asChild><Boton variante="contorno">Cancelar</Boton></DialogoCerrar>
                <BotonEnviar faltan={sinTecnico ? ["el técnico"] : []} enviando={form.formState.isSubmitting} textoEnviando="Asignando…">Asignar</BotonEnviar>
            </DialogoPie>
        </form>
    </DialogoContenido>
</DialogoProtegido>
```

En el panel, `erroresACampos` (`apps/web/lib/formularios.ts`) usa `asignarErrores` y
`mensajeDeError` agrega al detalle los `errors[]` que no quedaron en un campo.

No emite ni consume eventos.

## Dependencias (motivo)

- `@heroui/react` + `@heroui/styles` (3.2.6, fijas): librería de componentes aprobada por el PM.
  Da los estilos y el tema por variables, y los componentes que faltaban: pestañas, avisos
  emergentes, fechas, autocompletar, migas, tooltip y popover. 3.2.6 es la estable más nueva con al
  menos dos semanas de publicada (3.2.7 salió el 28 oct 2026).
- `react-aria-components`, `react-aria`, `@react-aria/ssr`, `@react-aria/utils` e
  `@internationalized/date`: dependencias *peer* de HeroUI, en las versiones con que se probó
  3.2.6. Además, los diálogos, menús, tooltip y popover las usan directamente para conservar el
  comportamiento de antes. `@internationalized/date` maneja las fechas de calendario de los
  selectores.
- Se quitaron `radix-ui` y `class-variance-authority`: ya no hacen falta.
- `pnpm-workspace.yaml` corta `@react-types/color` → `@react-spectrum/color` / `@react-spectrum/provider`
  (solo tipos; traían React Spectrum entero y una segunda copia de React Aria) y fija
  `react-stately` 3.50.0: queda una sola versión de `react-aria`, `react-aria-components` y
  `react-stately`.

- `@zxing/browser` + `@zxing/library`: respaldo del escáner de serie/MAC donde no hay
  `BarcodeDetector` (Safari en iPhone, Firefox). Import dinámico: solo se descarga al escanear.
