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
