import type { Schema } from "@iris/contratos";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { ficha } from "./datos-tickets";
import { buscarDesbordes } from "./desbordes";

/*
 * Tipo de servicio (contrato con Argos 0.15.0 §13.1, B33): la ficha y la orden muestran el tipo con
 * ícono y texto, y un servicio de cable no muestra router. Sin el tipo (Argos anterior), como hoy.
 * En los 4 proyectos (escritorio y teléfono, claro y oscuro) y sin desbordes.
 */

/** La ficha de María (cliente 501) con su servicio de cable, sin router (como la manda la API). */
async function conCable(page: Page) {
    const base = ficha(501, false, new Date().toISOString());
    if (!base) throw new Error("falta el cliente 501");
    // Un servicio de cable llega sin router ni IP (router_id null en Argos).
    const servicios: Schema<"Servicio">[] = (base.servicios ?? []).map((s) => {
        const cable: Schema<"Servicio"> = { ...s, planNombre: "TV Básica", tipoServicio: "cable", componentes: ["cable"], sinRouter: true };
        delete cable.routerId;
        delete cable.routerNombre;
        delete cable.ip;
        return cable;
    });
    await page.route("**/api/v1/clientes/501*", (r) => (r.request().method() === "GET" ? r.fulfill({ json: { ...base, servicios } }) : r.fallback()));
}

test.describe("tipo de servicio", () => {
    test("en la orden: «Cable (TV)» con ícono y sin router", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conCable(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1");
        const servicio = page.getByTestId("ficha-cliente").getByTestId("datos-servicio");
        await expect(servicio).toContainText("TV Básica");
        await expect(servicio.getByTestId("linea-servicio-tipo")).toHaveText("Tipo Cable (TV)");
        await expect(servicio).not.toContainText("RB-Norte-01");
        await expect(servicio.getByTestId("linea-servicio-router")).toHaveCount(0);
        // Solo la línea del servicio (las pestañas de la orden se deslizan a lo ancho a propósito en el teléfono).
        await expect.poll(() => buscarDesbordes(page, '[data-testid="ficha-cliente"] [data-testid="datos-servicio"]'), "nada se desborda").toEqual([]);
    });

    test("sin tipo (Argos anterior a 0.15.0): como hoy, con router y sin tipo", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1");
        const servicio = page.getByTestId("ficha-cliente").getByTestId("datos-servicio");
        await expect(servicio).toContainText("RB-Norte-01");
        await expect(servicio.getByTestId("linea-servicio-tipo")).toHaveCount(0);
    });
});
