import { expect, test, type Locator, type Page } from "@playwright/test";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { ordenesDelDia } from "./datos-campo";

/*
 * Reporte del PM (nov 2026): «cuando se va a firmar, el botón lo tiene hasta abajo y no está
 * flotante». «Firmar y cerrar orden» y «Confirmar recepción» van en una isla fija abajo (como la de
 * los pasos), siempre a la vista sin bajar, sin tapar el recuadro de la firma, y firmar no desplaza
 * la pantalla. Escritorio y teléfono, claro y oscuro.
 */

test.beforeEach(async ({ context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "");
});

/** OT-000102 en sitio, con todo lo obligatorio listo y firma obligatoria. */
function listaParaFirmar() {
    return ordenesDelDia().map((o) =>
        o.id === 102
            ? {
                  ...o,
                  estado: "en_sitio" as const,
                  enSitioEn: new Date().toISOString(),
                  requiereFirma: true,
                  checklist: (o.checklist ?? []).map((i) => ({ ...i, valor: true as never, ocurridoEn: new Date().toISOString() })),
              }
            : o,
    );
}

/** Captura a 390 px para el PM (solo con `CAPTURAS_FIRMA=<carpeta>`). */
async function capturar(page: Page, nombre: string, proyecto: string) {
    const carpeta = process.env.CAPTURAS_FIRMA;
    if (carpeta && proyecto.startsWith("telefono")) {
        await page.screenshot({ path: `${carpeta}/${nombre}-${proyecto}.png` });
    }
}

/** El botón a la vista en la pantalla, sin desplazar; la isla, sólida y separada del borde. */
async function comprobarIsla(page: Page, isla: Locator, boton: Locator) {
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
    await expect(boton).toBeInViewport({ ratio: 1 });
    // Es la única isla: la de menú y la de pasos no se dibujan encima ni debajo.
    await expect(page.locator('[data-slot="isla-menu"]')).toHaveCount(0);
    await expect(page.locator('[data-slot="barra-pasos"]')).toHaveCount(0);
    const estilo = await isla.evaluate((n) => {
        const s = getComputedStyle(n);
        return { posicion: s.position, desenfoque: s.backdropFilter, fondo: s.backgroundColor };
    });
    expect(estilo.posicion).toBe("fixed");
    // Capa flotante sólida (docs/ui/guia.md): sin vidrio.
    expect(estilo.desenfoque === "none" || estilo.desenfoque === "").toBe(true);
    expect(estilo.fondo).not.toMatch(/rgba\(.*, 0(\.\d+)?\)$/);
    const caja = await isla.boundingBox();
    const alto = page.viewportSize()?.height ?? 0;
    expect(caja).not.toBeNull();
    expect(alto - ((caja?.y ?? 0) + (caja?.height ?? 0))).toBeGreaterThanOrEqual(16);
}

/** Al final de la página, lo último del contenido queda por encima de la isla (no lo tapa). */
async function nadaQuedaDebajo(page: Page, isla: Locator, ultimo: Locator) {
    await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
    const caja = await ultimo.boundingBox();
    const cajaIsla = await isla.boundingBox();
    expect((caja?.y ?? 0) + (caja?.height ?? 0)).toBeLessThanOrEqual(cajaIsla?.y ?? 0);
}

test("firma y cierre: «Firmar y cerrar orden» fijo abajo, siempre a la vista, y firmar no desplaza", async ({ page }, info) => {
    const api = await simularApiCampo(page, { ordenes: listaParaFirmar() });
    await page.goto("/campo?orden=102&paso=cierre");
    await expect(page.getByRole("form", { name: "Firma del cliente y cierre" })).toBeVisible();
    await capturar(page, "cierre", info.project.name);

    const isla = page.locator('[data-slot="isla-accion"]');
    const boton = isla.getByRole("button", { name: "Firmar y cerrar orden" });
    await comprobarIsla(page, isla, boton);
    // «Anterior» redondo, como en los pasos: vuelve a las fotos.
    await expect(isla.getByRole("button", { name: "Anterior" })).toBeVisible();

    // El recuadro no desplaza la página con el dedo, y la isla nunca lo tapa.
    const lienzo = page.getByRole("img", { name: /Firma del cliente/ });
    expect(await lienzo.evaluate((n) => getComputedStyle(n).touchAction)).toBe("none");
    await lienzo.evaluate((n) => n.scrollIntoView({ block: "center" }));
    // Que se asiente (la barra compacta y la isla encogida responden al desplazamiento).
    await page.waitForTimeout(400);
    const cajaLienzo = await lienzo.boundingBox();
    const cajaIsla = await isla.boundingBox();
    if (!cajaLienzo || !cajaIsla) {
        throw new Error("sin lienzo o sin isla");
    }
    expect(cajaLienzo.y + cajaLienzo.height).toBeLessThanOrEqual(cajaIsla.y);
    const antes = await page.evaluate(() => window.scrollY);
    // Un trazo vertical largo, de arriba abajo del recuadro: es firma, no desplazamiento.
    await page.mouse.move(cajaLienzo.x + 40, cajaLienzo.y + 10);
    await page.mouse.down();
    for (let i = 1; i <= 12; i += 1) {
        await page.mouse.move(cajaLienzo.x + 40 + i * 18, cajaLienzo.y + 10 + i * ((cajaLienzo.height - 20) / 12));
    }
    await page.mouse.up();
    await expect(lienzo).toHaveAccessibleName(/firmada/);
    // Ni el trazo ni el texto de debajo («Firma capturada») mueven la página.
    expect(await page.evaluate(() => window.scrollY)).toBe(antes);
    await nadaQuedaDebajo(page, isla, page.getByRole("radiogroup", { name: "Resultado" }));
    await expect(boton).toBeInViewport({ ratio: 1 });
    await capturar(page, "cierre-firmado-abajo", info.project.name);

    // Sigue cerrando la orden (el botón de la isla envía el formulario, con su validación).
    await page.getByLabel("Nombre de quien firma").fill("Jorge Méndez");
    await boton.click();
    await expect(page.getByText("Orden cerrada", { exact: true })).toBeVisible();
    await expect.poll(() => api.ordenes.get(102)?.estado, { timeout: 15_000 }).toBe("cerrada");
    // Cerrada, ya no hay acción al pie: vuelve la isla de menú.
    await expect(isla).toHaveCount(0);
});

test("firma y cierre: sin firma, el botón de la isla valida y dice qué falta", async ({ page }) => {
    await simularApiCampo(page, { ordenes: listaParaFirmar() });
    await page.goto("/campo?orden=102&paso=cierre");
    const isla = page.locator('[data-slot="isla-accion"]');
    await isla.getByRole("button", { name: "Firmar y cerrar orden" }).click();
    await expect(page.getByText("Pide al cliente que firme en el recuadro")).toBeVisible();
    await isla.getByRole("button", { name: "Anterior" }).click();
    await expect(page).toHaveURL(/paso=fotos/);
    await expect(isla).toHaveCount(0);
    await expect(page.locator('[data-slot="barra-pasos"]')).toBeVisible();
});

test("recepción de un traslado: «Confirmar recepción» fijo abajo y a la vista", async ({ page }, info) => {
    await simularApiCampo(page);
    await page.goto("/campo?traslado=31");
    await expect(page.getByRole("form", { name: "Confirmar recepción" })).toBeVisible();
    await capturar(page, "recepcion", info.project.name);

    const isla = page.locator('[data-slot="isla-accion"]');
    await comprobarIsla(page, isla, isla.getByRole("button", { name: "Confirmar recepción" }));
    await nadaQuedaDebajo(page, isla, page.getByRole("img", { name: /Firma del técnico/ }));
    await isla.getByRole("button", { name: "Confirmar recepción" }).click();
    await expect(page.getByText("Firma en el recuadro para confirmar que recibiste")).toBeVisible();
    await isla.getByRole("button", { name: "Por recibir" }).click();
    await expect(page).toHaveURL(/vista=recibir/);
    await expect(page.locator('[data-slot="isla-menu"]')).toBeVisible();
});
