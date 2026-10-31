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
| `overlay` (menús, popovers, modales, avisos) | `--hx-glass-strong`: vidrio fuerte con desenfoque de 40 px |
| `field-*` | fondo del tema, borde de 1 px `--hx-border`, radio 6 px, foco teal |
| `radius` | `--hx-radius` (6 px): botones con esquina de Horus, no píldora |

- HeroUI no tiene «info»: el azul de Horus es la utilidad `info` de Iris (`text-info`, `bg-info/10`).
  `accent` nunca es un estado.
- El modo oscuro va por la clase `.dark` (next-themes) o por `[data-theme]`. La variante `dark:` de
  Iris reemplaza la de HeroUI, que además seguía `prefers-color-scheme` aunque se eligiera «Claro».
- `muted` en HeroUI es texto atenuado. El fondo tenue de Iris es `bg-background-secondary`, antes
  `bg-muted`.
- Con `prefers-reduced-transparency` no hay vidrio, y con `prefers-reduced-motion` no hay
  animaciones.

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
HeroUI. Las «piezas de HeroUI» son sus clases (`button`, `card__title`, `chip--danger`…, de
`@heroui/styles`), que dan el aspecto. React Aria da el comportamiento: teclado, foco y
lectores de pantalla.

| `@iris/ui` | Antes | Ahora |
| --- | --- | --- |
| `Boton`, `BotonEnviar` | `<button>` + cva | `<button>` nativo con `buttonVariants` de HeroUI (`primary`, `tertiary`, `outline`, `ghost`) y el radio y halo de Horus; `asChild` con `Ranura` |
| `Entrada`, `Campo` | `<input>` | HeroUI `Input` (un `<input>` nativo de React Aria): sirve con `register` |
| `AreaTexto`, `CampoArea` | `<textarea>` | HeroUI `TextArea` |
| `Etiqueta` | Radix `Label` | HeroUI `Label` |
| `Selector`, `CampoSelector` | `<select>` | `<select>` nativo con el estilo de campo de HeroUI (`input`), ver «Lo que no cambió» |
| `Dialogo*`, `DialogoProtegido` | Radix `Dialog` | HeroUI `Modal` (`ModalOverlay` + `Modal` + `Dialog` de React Aria), fondo `modal__backdrop`, vidrio fuerte, hoja inferior en el teléfono |
| `DialogoConfirmar` | Radix `AlertDialog` | HeroUI `AlertDialog` (`role="alertdialog"`), foco inicial en «Cancelar», no se cierra tocando fuera |
| `Hoja*` | Radix `Dialog` | HeroUI `Drawer` (fondo `drawer__backdrop`) |
| `MenuDesplegable*` | Radix `DropdownMenu` | HeroUI `Dropdown` (`MenuTrigger` + `Popover` + `Menu`); `onSelect` → `onAction` |
| `Tarjeta*` | `div.surface-card` | piezas de HeroUI `Card` + `surface-card` |
| `Aviso`, `ErrorFormulario` | `div` | piezas de HeroUI `Alert` con el fondo tintado de Horus |
| `InsigniaEstado` | `span` | HeroUI `Chip` suave (`chip--success`, `--warning`, `--danger`; `info` y `neutral` con color de Iris) |
| `Tabla*` | `<table>` | `<table>` nativa con las piezas de HeroUI `Table` |
| `Cargando` | Lucide | estilo de HeroUI `Spinner` + icono de Lucide, `role="status"` |
| `Esqueleto` | `div` | HeroUI `Skeleton` (pulso) |
| `EstadoVacio` | `div` | HeroUI `EmptyState` |

### Componentes nuevos

| `@iris/ui` | HeroUI | Para |
| --- | --- | --- |
| `Pestanas` | `Tabs` | Fichas con pestañas (fase 3); con `href`, cada pestaña tiene URL |
| `ProveedorAvisosEmergentes`, `avisar`, `useAvisoEmergente`, `cerrarAviso` | `Toast` | Aviso breve tras una acción, con «Deshacer» opcional |
| `Migas` | `Breadcrumbs` | Ruta en cada pantalla del panel |
| `ProveedorRutas` | `RouterProvider` de React Aria | Que migas, pestañas y menús naveguen con `router.push`, sin recargar |
| `SelectorFecha`, `SelectorRangoFechas`, `hoyEnGuatemala` | `DatePicker`, `DateRangePicker` | Fechas `AAAA-MM-DD` en formato `es-GT`; «hoy» es el de `America/Guatemala` |
| `Autocompletar` | `ComboBox` | Listas largas o búsqueda en el servidor (`alBuscar`) |
| `Globo` | `Tooltip` | Ayuda al pasar el puntero o al enfocar; no reemplaza `aria-label` |
| `PanelEmergente` | `Popover` | Filtros rápidos o detalle junto a un botón (no modal) |
| `BarraGuardar` | — | Pie fijo con «Cancelar» y «Guardar», con el área segura del teléfono |
| `FormularioPagina`, `SeccionFormulario`, `useProteccionSalida` | — | Formulario en pantalla propia: migas, título, secciones, barra y aviso al salir con cambios |

`useCambiosSinGuardar(isDirty, isSubmitting)` sirve igual dentro de `DialogoProtegido` que dentro de
`FormularioPagina`.

### Lo que no cambió (y por qué)

- **`Selector`** sigue siendo un `<select>` nativo en la fase 0. En el teléfono abre el selector del
  sistema, `register` de react-hook-form lo usa tal cual y unas 80 pruebas usan `selectOption`. Se
  revisa pantalla por pantalla en la fase 2: `Autocompletar` para listas largas y el `Select` de
  HeroUI donde haga falta.
- **`Casilla`, `GrupoOpciones` y `GrupoSegmentado`** siguen nativos (checkbox, radios y
  `radiogroup`). Los `Checkbox` y `RadioGroup` de HeroUI entregan un booleano y no el evento que
  espera `register`.
- **`Camara`, `Escaner`, `LienzoFirma`, `LogoIris` y `MarcaIris`** no tienen equivalente en HeroUI.
  Siguen como estaban, con los tokens del tema.
- **`Boton` no usa el `Button` de React Aria**, porque ese filtra `title`, frena la propagación del
  clic y cambia `onClick` por `onPress`. Usa sus clases. Dentro de un disparador de React Aria (menú,
  popover, tooltip) recibe las props por `Pressable` o `Focusable`.
- **Sin animación de salida en diálogos y hojas:** se desmontan en el acto, como antes. Con salida
  animada, un Escape o un clic inmediato le llegaría a la capa que se está yendo.

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
| 3 · Fichas con pestañas | Ticket, orden, técnico, prospecto y usuario; acciones en un panel lateral | I3 |
| 4 · App de campo en el navegador | Barra inferior, pasos independientes, firma y cierre como paso final, barra de estado discreta, botones grandes | I4 |
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
