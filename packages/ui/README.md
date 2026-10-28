# @iris/ui

Componentes compartidos (shadcn/ui adaptado) e identidad Horus «vidrio y luz» de Iris. Fuente
TS sin build: la app lo transpila (`transpilePackages: ["@iris/ui"]`).

## Estilos

```css
@import "tailwindcss";
@import "@iris/ui/estilos.css";
@source "../../../packages/ui/src";   /* ruta relativa al CSS de la app */
```

`src/estilos/horus-tokens.css` y `src/marca/logo-mark.svg` son copia exacta de `docs/marca/`
(una prueba lo verifica; si cambia la marca, copia los dos). `estilos.css` expone los tokens
`--hx-*` como utilidades: `bg-background`, `text-foreground`, `text-muted-foreground`,
`text-dimmed`, `bg-elevated`, `bg-card`, `border-border`, `bg-primary`/`text-primary-foreground`,
`success`, `info`, `warning`, `error`, `rounded-card`, `shadow-glow`. Materiales: `glass-material`
(barras), `glass-strong` (menús, hojas), `surface-card` (contenido), `app-aura` (fondo). Tema
oscuro con la clase `.dark` en `<html>` (variante `dark:`).

## Componentes

`Boton` (primario con halo, secundario, contorno, fantasma, enlace; `asChild`), `Entrada`,
`Etiqueta`, `Campo` (etiqueta + entrada + error con `aria-describedby`), `AreaTexto` / `CampoArea`,
`Selector` / `CampoSelector` (`<select>` nativo: en el teléfono abre el selector del sistema),
`Dialogo*` (modal en vidrio fuerte; en el teléfono sube desde abajo, `DialogoPie`), `Tarjeta*`, `Aviso`
(tono + icono + título + acción, p. ej. «Reintentar»), `InsigniaEstado` (icono + texto + color),
`EstadoVacio`, `Esqueleto`, `Cargando`, `GrupoSegmentado` (radiogroup), `Casilla` (checkbox con etiqueta), `Hoja*` (panel lateral),
`Tabla*`, `LogoIris`, `MarcaIris`, `cn`.

App de campo: `Camara` (botón que abre la cámara del teléfono con `<input type="file"
capture>`, oculto pero enfocable), `LienzoFirma` (firma con el dedo por pointer events, tinta del
tema en pantalla) con `trazosASvg` (≤ 200 000 caracteres, el tope de la API) y `trazosAPng`
(negro sobre transparente), y `Escaner` (cámara trasera + `BarcodeDetector` nativo; si no
existe, ZXing cargado bajo demanda; sin cámara o sin permiso explica y deja escribir a mano). Tonos: `neutral | info | success | warning | error`
(nunca `primary` como estado). Íconos solo de Lucide.

No emite ni consume eventos.

## Dependencias (motivo)

- `@zxing/browser` + `@zxing/library`: respaldo del escáner de serie/MAC donde no hay
  `BarcodeDetector` (Safari en iPhone, Firefox). Import dinámico: solo se descarga al escanear.
