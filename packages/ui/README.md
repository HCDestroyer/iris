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
`Tabla*`, `LogoIris`, `MarcaIris`, `cn`, y el patrón de formularios en modales (abajo).

App de campo: `Camara` (botón que abre la cámara del teléfono con `<input type="file"
capture>`, oculto pero enfocable), `LienzoFirma` (firma con el dedo por pointer events, tinta del
tema en pantalla) con `trazosASvg` (≤ 200 000 caracteres, el tope de la API) y `trazosAPng`
(negro sobre transparente), y `Escaner` (cámara trasera + `BarcodeDetector` nativo; si no
existe, ZXing cargado bajo demanda; sin cámara o sin permiso explica y deja escribir a mano). Tonos: `neutral | info | success | warning | error`
(nunca `primary` como estado). Íconos solo de Lucide.

## Formularios en modales (`formulario-modal.tsx`)

Patrón obligatorio para todo formulario en un diálogo del panel (pedido del PM: «los modales
deben alertar si no hay selección o impedir guardar»). No depende de react-hook-form: la app le
pasa `isDirty`, lo que falta y `setError`.

- **`DialogoProtegido`**: reemplaza a `Dialogo` (controlado: `open` + `onOpenChange`). Si hay
  cambios sin guardar, Escape, tocar fuera, la X o un «Cancelar» envuelto en `DialogoCerrar`
  piden «¿Descartar los cambios?» antes de cerrar. Cerrar por código (tras guardar) no pregunta.
  Los cambios llegan por `sucio` (cuando el formulario vive en el mismo componente que el
  diálogo) o con **`useCambiosSinGuardar(form.formState.isDirty)`** desde un formulario hijo.
  `ocupado` impide cerrar mientras se envía.
- **`BotonEnviar`**: `type="submit"`, deshabilitado mientras `enviando` o mientras `faltan` tenga
  selecciones obligatorias (`["el técnico"]` → «Falta elegir el técnico para guardar.», o
  `motivo` propio), con el mensaje visible junto al botón y enlazado por `aria-describedby`. El
  campo lleva además su `ayuda`/`error` («Elige el formulario que llenará el técnico»).
- **`DialogoConfirmar`** (`AlertDialog`): confirmación de acciones destructivas (`peligro`:
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

- `@zxing/browser` + `@zxing/library`: respaldo del escáner de serie/MAC donde no hay
  `BarcodeDetector` (Safari en iPhone, Firefox). Import dinámico: solo se descarga al escanear.
