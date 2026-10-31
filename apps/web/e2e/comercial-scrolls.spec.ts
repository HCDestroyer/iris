import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { simularTeselas } from "./teselas";

/*
 * Pedido del PM (8): Comercial sin scrolls innecesarios. En 390 px y 1280 px, claro y oscuro,
 * la lista y el detalle no desbordan la página ni tienen contenedores con desplazamiento propio
 * (ni doble scroll): todo se ve completo y la página es lo único que se desplaza.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

/** Elementos con desplazamiento propio (contenido mayor que su caja y overflow auto/scroll). */
async function contenedoresConScroll(page: Page) {
    return page.evaluate(() => {
        const salida: string[] = [];
        for (const el of Array.from(document.querySelectorAll<HTMLElement>("main *"))) {
            const estilo = getComputedStyle(el);
            const y = /(auto|scroll)/.test(estilo.overflowY) && el.scrollHeight > el.clientHeight + 1;
            const x = /(auto|scroll)/.test(estilo.overflowX) && el.scrollWidth > el.clientWidth + 1;
            if ((x || y) && el.getClientRects().length > 0) {
                const etiqueta = el.getAttribute("aria-label") ?? el.dataset.slot ?? el.className.toString().slice(0, 60);
                salida.push(`${el.tagName.toLowerCase()}[${x ? "x" : ""}${y ? "y" : ""}] ${etiqueta}`);
            }
        }
        const desborde = document.documentElement.scrollWidth - document.documentElement.clientWidth;
        return { salida, desborde };
    });
}

async function revisar(page: Page, nombre: string) {
    await page.screenshot({ path: nombre, fullPage: true });
    const { salida, desborde } = await contenedoresConScroll(page);
    expect.soft(desborde, "la página no desborda en horizontal").toBeLessThanOrEqual(0);
    expect.soft(salida, `sin contenedores con scroll propio (${path.basename(nombre)})`).toEqual([]);
}

test("comercial sin scrolls innecesarios: lista, embudo y detalle", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `scrolls-${que}-${info.project.name}.png`);
    const telefono = info.project.name.startsWith("telefono");
    await simularApi(page, { comercialConector: true });
    await simularTeselas(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);

    await page.goto("/comercial");
    await expect(page.getByText("Interesado 22").first()).toBeVisible();
    await revisar(page, nombre("01-inicio"));

    if (!telefono) {
        await page.goto("/comercial?vista=lista");
        await expect(page.getByRole("table", { name: "Prospectos" })).toBeVisible();
        await revisar(page, nombre("02-lista"));
    }

    await page.goto("/comercial/4?pestana=cotizacion");
    await expect(page.getByTestId("cotizacion").getByTestId("total-mensual")).toBeVisible();
    await revisar(page, nombre("03-detalle-cotizado"));

    await page.goto("/comercial/6");
    await expect(page.getByTestId("alta")).toBeVisible();
    await revisar(page, nombre("04-detalle-alta"));

    await page.goto("/comercial/5?pestana=contrato");
    await expect(page.getByTestId("contrato")).toBeVisible();
    await revisar(page, nombre("05-detalle-contrato"));
});
