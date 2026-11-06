import { expect, type Locator, type Page } from "@playwright/test";

/*
 * Ayudas para los controles de HeroUI (React Aria) que reemplazaron a los nativos:
 * - `Selector` / `CampoSelector`: el `Select` de HeroUI (un botón que abre una lista). Reemplaza a
 *   `selectOption`, que solo sirve con un `<select>` nativo. El botón lleva el valor elegido en
 *   `data-valor` (lo que antes era `toHaveValue`).
 * - `CampoFecha` / `CampoHora`: `DatePicker` y `TimeField` de HeroUI, con segmentos (día, mes,
 *   año; hora, minutos) en vez de un `<input type="date|time">`. Reemplaza a `fill("AAAA-MM-DD")`.
 */

type Opcion = string | RegExp | { label: string | RegExp } | { value: string };

/**
 * El botón del `Select`. `getByLabel` también puede dar con el `<select>` oculto que React Aria
 * deja para el autocompletado del navegador (si el texto de una opción contiene la etiqueta).
 */
export function boton(control: Locator): Locator {
    return control.and(control.page().locator('[data-slot="select-trigger"]'));
}

function lista(disparador: Locator): Locator {
    return disparador.page().locator('[role="listbox"]').filter({ visible: true }).last();
}

/** Elige una opción del `Select`, como `selectOption`: un texto busca primero por valor y luego por el texto exacto. */
export async function elegirOpcion(control: Locator, opcion: Opcion): Promise<void> {
    const disparador = boton(control);
    await disparador.click();
    const caja = lista(disparador);
    await expect(caja).toBeVisible();
    let elegida: Locator;
    if (typeof opcion === "string") {
        elegida = caja.locator(`[role="option"][data-valor="${opcion.replace(/"/g, '\\"')}"]`).or(caja.getByRole("option", { name: opcion, exact: true }));
    } else if (opcion instanceof RegExp) {
        elegida = caja.getByRole("option", { name: opcion });
    } else if ("value" in opcion) {
        elegida = caja.locator(`[role="option"][data-valor="${opcion.value.replace(/"/g, '\\"')}"]`);
    } else {
        elegida = caja.getByRole("option", { name: opcion.label, exact: typeof opcion.label === "string" });
    }
    await elegida.first().click();
    await expect(caja).toBeHidden();
}

/** El valor elegido en un `Select` (antes `toHaveValue` del `<select>`). */
export async function esperarOpcion(control: Locator, valor: string): Promise<void> {
    await expect(boton(control)).toHaveAttribute("data-valor", valor);
}

/** Textos de las opciones de un `Select` (abre la lista, los lee y la cierra con Escape). */
export async function textosOpciones(control: Locator): Promise<string[]> {
    const disparador = boton(control);
    await disparador.click();
    const caja = lista(disparador);
    await expect(caja).toBeVisible();
    const textos = (await caja.getByRole("option").allTextContents()).map((t) => t.trim());
    await disparador.page().keyboard.press("Escape");
    await expect(caja).toBeHidden();
    return textos;
}

/** El grupo de segmentos de un `CampoFecha` / `CampoHora` por su etiqueta. */
export function campoSegmentos(ambito: Page | Locator, etiqueta: string | RegExp): Locator {
    return ambito.getByRole("group", { name: etiqueta, exact: typeof etiqueta === "string" }).first();
}

async function escribirSegmentos(grupo: Locator, partes: readonly string[]): Promise<void> {
    const segmentos = grupo.getByRole("spinbutton");
    if (partes.length === 0) {
        // Vaciar: Retroceso en cada segmento, del último al primero.
        const n = await segmentos.count();
        for (let i = n - 1; i >= 0; i--) {
            const s = segmentos.nth(i);
            await s.click();
            for (let k = 0; k < 5; k++) await s.press("Backspace");
        }
        return;
    }
    await segmentos.first().click();
    for (const parte of partes) await grupo.page().keyboard.type(parte);
}

/** Escribe una fecha `AAAA-MM-DD` en un `CampoFecha` (en es-GT: día, mes, año). `""` la vacía. */
export async function escribirFecha(ambito: Page | Locator, etiqueta: string | RegExp, valor: string): Promise<void> {
    const grupo = campoSegmentos(ambito, etiqueta);
    if (valor === "") return escribirSegmentos(grupo, []);
    const [anio, mes, dia] = valor.split("-");
    await escribirSegmentos(grupo, [dia ?? "", mes ?? "", anio ?? ""]);
}

/** Escribe una hora `HH:MM` (24 h) en un `CampoHora`. `""` la vacía. */
export async function escribirHora(ambito: Page | Locator, etiqueta: string | RegExp, valor: string): Promise<void> {
    const grupo = campoSegmentos(ambito, etiqueta);
    if (valor === "") return escribirSegmentos(grupo, []);
    const [horas, minutos] = valor.split(":");
    await escribirSegmentos(grupo, [horas ?? "", minutos ?? ""]);
}

/** El valor de un `CampoFecha` / `CampoHora` (`AAAA-MM-DD` / `HH:MM`, antes `toHaveValue`). */
export async function esperarValorCampo(ambito: Page | Locator, etiqueta: string | RegExp, valor: string): Promise<void> {
    await expect(campoSegmentos(ambito, etiqueta).locator("xpath=ancestor::*[@data-valor][1]")).toHaveAttribute("data-valor", valor);
}

/** Abre la lista de un `Select` y la devuelve (para mirar sus opciones). Se cierra con `cerrarOpciones`. */
export async function abrirOpciones(control: Locator): Promise<Locator> {
    const disparador = boton(control);
    await disparador.click();
    const caja = lista(disparador);
    await expect(caja).toBeVisible();
    return caja;
}

/** Cierra la lista abierta con Escape. */
export async function cerrarOpciones(page: Page): Promise<void> {
    await page.keyboard.press("Escape");
    await expect(page.locator('[role="listbox"]').filter({ visible: true })).toHaveCount(0);
}

/*
 * `Autocompletar` (`ComboBox` de HeroUI). Su lista no es modal y React Aria la cierra si la página
 * se desplaza. Enfocar el campo desplaza la página para mostrarlo (en el teléfono, casi siempre), y
 * el clic de Playwright también desplaza para «ver» una opción mientras el panel crece con los
 * resultados: la lista se cerraba sola en las pruebas. Estas ayudas la vuelven a abrir con flecha
 * abajo (como haría la persona) y eligen con el teclado, que no desplaza nada.
 */

/** Abre la lista de un `Autocompletar` si está cerrada (flecha abajo) y la devuelve. */
export async function abrirSugerencias(campo: Locator): Promise<Locator> {
    await expect(async () => {
        if ((await campo.getAttribute("aria-expanded")) !== "true") await campo.press("ArrowDown");
        expect(await campo.getAttribute("aria-expanded")).toBe("true");
    }).toPass({ intervals: [100, 200, 400] });
    const idLista = await campo.getAttribute("aria-controls");
    return campo.page().locator(`[id="${idLista ?? ""}"]`);
}

/** Escribe en un `Autocompletar` y deja su lista abierta (con los resultados que lleguen). */
export async function buscarSugerencias(campo: Locator, texto: string): Promise<Locator> {
    await campo.fill(texto);
    return abrirSugerencias(campo);
}

/**
 * Elige la opción cuyo texto coincide. El clic va como clic «virtual» (`dispatchEvent`, el de un
 * lector de pantalla, que React Aria toma como un toque): no desplaza la página y, si la lista se
 * cerró en el camino, no cae en otro control (un Enter con la lista cerrada enviaría el formulario).
 * Se da por elegida cuando el campo se va (el selector de cliente pasa a confirmar) o queda con el
 * texto de la opción; si no, vuelve a abrir la lista y repite.
 */
export async function elegirSugerencia(campo: Locator, nombre: RegExp): Promise<void> {
    const elegida = async () => (await campo.count()) === 0 || nombre.test(await campo.inputValue());
    await expect(async () => {
        if (await elegida()) return;
        const caja = await abrirSugerencias(campo);
        await caja.getByRole("option", { name: nombre }).first().dispatchEvent("click", undefined, { timeout: 2000 });
        await expect.poll(elegida, { timeout: 1500 }).toBe(true);
    }).toPass({ intervals: [100, 200, 400] });
}

/**
 * Espera a que la opción quede resaltada en la lista abierta (con búsqueda en el servidor, la
 * primera se resalta sola al llegar los resultados), para elegirla con Enter. Si un desplazamiento
 * cerró la lista, la vuelve a abrir.
 */
export async function esperarResaltada(campo: Locator, nombre: RegExp): Promise<void> {
    await expect(async () => {
        const caja = await abrirSugerencias(campo);
        await expect(caja.getByRole("option", { name: nombre }).first()).toHaveAttribute("data-focused", "true", { timeout: 1500 });
    }).toPass({ intervals: [100, 200, 400] });
}
