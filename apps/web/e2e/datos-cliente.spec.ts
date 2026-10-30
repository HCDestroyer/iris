import { expect, test, type Locator } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";

/*
 * «Datos del cliente / servicio» (pedido 5 del PM: los datos de Argos coherentes): el mismo
 * componente y el mismo formato en el ticket, la orden y comercial. Corre en escritorio (1280 px) y
 * teléfono (390 px), claro y oscuro (playwright.config.ts).
 */

/** Lo operativo, igual en la ficha y en la copia guardada. */
async function operativoDeMaria(ficha: Locator) {
    await expect(ficha).toContainText("María Castillo");
    await expect(ficha).toContainText("CL-0501");
    // Teléfono principal de Argos con formato +502 y enlace para llamar en E.164.
    await expect(ficha.getByRole("link", { name: "+502 4455 6677" })).toHaveAttribute("href", "tel:+50244556677");
    // La dirección del cliente y la de instalación del servicio, separadas.
    await expect(ficha).toContainText("Dirección del cliente4a calle 3-21, zona 1");
    await expect(ficha.getByTestId("datos-servicio")).toContainText("Dirección de instalaciónInstalación: 4a calle 3-21, zona 1");
    await expect(ficha.getByTestId("datos-servicio")).toContainText("Fibra 100 Mb/s");
    await expect(ficha.getByTestId("datos-servicio")).toContainText("10.20.30.40");
    await expect(ficha.getByTestId("datos-servicio")).toContainText("RB-Norte-01");
    await expect(ficha.getByTestId("datos-servicio")).toContainText("Activo");
    await expect(ficha).toContainText("Zona Zona norte");
}

/** La ficha completa: además, documento fiscal, el otro teléfono y el correo. */
async function datosDeMaria(ficha: Locator) {
    await operativoDeMaria(ficha);
    await expect(ficha).toContainText("NIT 1234567-8");
    await expect(ficha.getByRole("link", { name: "+502 5555 0101" })).toHaveAttribute("href", "tel:+50255550101");
    await expect(ficha.getByRole("link", { name: "maria@ejemplo.gt" })).toHaveAttribute("href", "mailto:maria%40ejemplo.gt");
}

test.describe("datos del cliente y del servicio", () => {
    test("en el ticket: formato único, sincronizado hace X y refrescar", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        const ficha = page.getByTestId("ficha-cliente");
        await datosDeMaria(ficha);
        // La instantánea de la API falsa es de hace días: se avisa.
        await expect(ficha.getByTestId("sincronizado")).toContainText(/Sincronizado con Argos hace \d+ días/);
        await ficha.getByRole("button", { name: "Refrescar" }).click();
        await expect(ficha.getByTestId("sincronizado")).toContainText("Sincronizado con Argos hace un momento");
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/clientes/501" && p.query.get("refrescar") === "true")).toBe(true);
    });

    test("en la orden: el mismo componente con los mismos datos", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1");
        await datosDeMaria(page.getByTestId("ficha-cliente"));
    });

    test("conector apagado: la copia guardada al crear el ticket, con el mismo formato", async ({ page, context, baseURL }) => {
        await simularApi(page, { conectorApagado: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        const ficha = page.getByTestId("ficha-cliente");
        await expect(ficha).toContainText("Conector con Argos apagado");
        await expect(ficha).toContainText("Se muestran los datos que se copiaron al crear el ticket.");
        await operativoDeMaria(ficha);
        // La copia no guarda documento fiscal ni correo.
        await expect(ficha).not.toContainText("1234567-8");
        await expect(ficha).not.toContainText("maria@ejemplo.gt");
        await expect(ficha.getByTestId("sincronizado")).toContainText("Copia guardada al crear el ticket (27/10/2026 14:20)");
        await expect(ficha.getByRole("button", { name: "Refrescar" })).toHaveCount(0);
    });

    test("refrescar sin llamar a Argos por la cuota: se avisa", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/2");
        const ficha = page.getByTestId("ficha-cliente");
        await expect(ficha).toContainText("Ferretería El Tornillo");
        await ficha.getByRole("button", { name: "Refrescar" }).click();
        await expect(ficha.getByRole("status").filter({ hasText: "demasiadas consultas a Argos" })).toBeVisible();
    });
});
