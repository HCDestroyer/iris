import path from "node:path";
import { expect, test } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";

/*
 * Ticket y orden de la mano (pedido 3 del PM): «Crear orden y asignar» desde el ticket, igualar
 * la asignación con un botón (nunca sola) y la propuesta de resolver al cerrarse la orden. En
 * escritorio y teléfono, claro y oscuro (proyectos de playwright.config.ts).
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const LUCIA = 3; // técnico del escenario (rol tecnico)

test.describe("ticket y orden de la mano", () => {
    test("crear orden y asignar: no deja guardar sin técnico ni cuadrilla, crea, programa e iguala el ticket", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        await page.getByRole("link", { name: "Crear orden y asignar" }).click();
        await expect(page).toHaveURL(/\/ordenes\/nueva\?ticket=1$/);
        const dialogo = page.getByRole("form", { name: "Crear orden y asignar" });
        await expect(dialogo.getByLabel("Tipo")).toHaveValue("visita");
        await expect(dialogo.getByLabel("Trabajo a realizar")).toHaveValue("Sin internet desde la mañana");
        await expect(dialogo.getByLabel("Técnico")).toHaveValue("");
        await expect(dialogo.getByLabel("Cuadrilla")).toHaveValue("");

        // Sin técnico ni cuadrilla: no deja guardar y lo explica junto al botón.
        await expect(dialogo.getByRole("button", { name: "Crear y asignar" })).toBeDisabled();
        await expect(dialogo.getByRole("button", { name: "Crear y asignar" })).toHaveAccessibleDescription("Falta elegir el técnico o la cuadrilla para guardar.");
        await page.screenshot({ path: path.join(carpeta, `25-orden-ticket-sin-asignacion-${info.project.name}.png`) });
        expect(api.peticiones.some((p) => p.metodo === "POST" && p.ruta.endsWith("/crear-orden"))).toBe(false);

        // Con fecha pero sin técnico, tampoco.
        await dialogo.getByLabel("Cuadrilla").selectOption("1");
        await dialogo.getByLabel("Fecha").fill("2026-11-20");
        await dialogo.getByLabel("Hora").fill("10:30");
        await dialogo.getByRole("button", { name: "Crear y asignar" }).click();
        await expect(dialogo.getByText("Para programar la visita elige el técnico")).toBeVisible();

        await dialogo.getByLabel("Técnico").selectOption(String(LUCIA));
        await page.screenshot({ path: path.join(carpeta, `25-orden-desde-ticket-${info.project.name}.png`) });
        await dialogo.getByRole("button", { name: "Crear y asignar" }).click();
        await expect(page).toHaveURL(/\/ordenes\/1000$/);
        const post = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/tickets/1/crear-orden");
        expect(post?.cuerpo).toEqual({ tipo: "visita", tecnicoId: LUCIA, cuadrillaId: 1, inicio: "2026-11-20T16:30:00Z", descripcion: "Sin internet desde la mañana" });
        expect(post?.idempotencia).toBeTruthy();
        await expect(page.getByTestId("tecnico")).toHaveText("Lucía Ramírez");

        // Desde la orden se ve el ticket ligado y que no tiene la misma asignación: se iguala con un botón.
        const vinculado = page.getByTestId("ticket-vinculado");
        await expect(vinculado.getByRole("link", { name: "TK-000101" })).toBeVisible();
        await expect(vinculado.getByTestId("ticket-asignado")).toHaveText("Responsable del ticket: sin asignar");
        await expect(vinculado.getByRole("button", { name: "Asignar la orden como el ticket" })).toHaveCount(0);
        await page.screenshot({ path: path.join(carpeta, `26-orden-igualar-ticket-${info.project.name}.png`), fullPage: true });
        await vinculado.getByRole("button", { name: "Asignar el ticket como la orden" }).click();
        await expect(vinculado.getByTestId("ticket-asignado")).toHaveText("Responsable del ticket: Lucía Ramírez · Cuadrilla 1");
        await expect(vinculado.getByRole("button", { name: "Asignar el ticket como la orden" })).toHaveCount(0);
        const igualar = api.peticiones.find((p) => p.ruta === "/api/v1/ordenes/1000/igualar-asignacion");
        expect(igualar?.cuerpo).toEqual({ desde: "orden", version: 3, versionTicket: 1 });
        expect(igualar?.idempotencia).toBeTruthy();

        // Desde el ticket: sus órdenes con estado, técnico y fecha, ya sin diferencia.
        await page.goto("/tickets/1?pestana=ordenes");
        const fila = page.getByTestId("ordenes-del-ticket").locator('[data-orden="1000"]');
        await expect(fila).toContainText("OT-001000");
        await expect(fila).toContainText("Programada");
        await expect(fila.getByTestId("orden-tecnico")).toHaveText("Lucía Ramírez · Cuadrilla 1");
        await expect(fila).toContainText("20/11/2026");
        await expect(page.getByTestId("igualar-1000")).toHaveCount(0);
    });

    test("igualar la orden al ticket desde la lista del ticket; un 409 pide recargar", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        const ticket = api.tickets.find((t) => t.id === 1);
        if (!ticket) throw new Error("falta el ticket 1");
        ticket.asignadoA = LUCIA;
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");

        // El modal propone el técnico del ticket; se deja solo la cuadrilla.
        await page.getByRole("link", { name: "Crear orden y asignar" }).click();
        await expect(page).toHaveURL(/\/ordenes\/nueva\?ticket=1$/);
        const dialogo = page.getByRole("form", { name: "Crear orden y asignar" });
        await expect(dialogo.getByLabel("Técnico")).toHaveValue(String(LUCIA));
        await dialogo.getByLabel("Técnico").selectOption("");
        await dialogo.getByLabel("Cuadrilla").selectOption("2");
        await dialogo.getByRole("button", { name: "Crear y asignar" }).click();
        await expect(page).toHaveURL(/\/ordenes\/1000$/);
        await expect(page.getByTestId("tecnico")).toHaveText("Sin técnico");

        await page.goto("/tickets/1?pestana=ordenes");
        const aviso = page.getByTestId("igualar-1000");
        await expect(aviso).toContainText("Ticket: Lucía Ramírez · Orden: Cuadrilla 2");
        await page.screenshot({ path: path.join(carpeta, `27-ticket-igualar-orden-${info.project.name}.png`), fullPage: true });

        // Otra persona cambió el ticket: 409 y «Recargar».
        api.cambiarVersion(1);
        await aviso.getByRole("button", { name: "Asignar la orden como el ticket" }).click();
        await expect(aviso.getByText("Alguien más cambió el ticket o la orden")).toBeVisible();
        await aviso.getByRole("button", { name: "Recargar" }).click();
        await expect(aviso.getByText("Alguien más cambió el ticket o la orden")).toHaveCount(0);

        await page.reload();
        await page.getByTestId("igualar-1000").getByRole("button", { name: "Asignar la orden como el ticket" }).click();
        await expect(page.getByTestId("igualar-1000")).toHaveCount(0);
        await expect(page.getByTestId("ordenes-del-ticket").locator('[data-orden="1000"]').getByTestId("orden-tecnico")).toHaveText("Lucía Ramírez");
        const igualar = api.peticiones.filter((p) => p.ruta === "/api/v1/ordenes/1000/igualar-asignacion").at(-1);
        expect(igualar?.cuerpo).toMatchObject({ desde: "ticket" });
    });

    test("al cerrarse la orden, el ticket muestra la propuesta de resolver (no se resuelve solo)", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        const ticket = api.tickets.find((t) => t.id === 1);
        if (!ticket) throw new Error("falta el ticket 1");
        ticket.propuesta = { accion: "resolver", en: new Date().toISOString(), motivo: "La orden OT-000001 se cerró (exitosa).", origen: "orden", ordenId: 1 };
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        const propuesta = page.getByTestId("propuesta-resolucion");
        await expect(propuesta).toContainText("Se propone resolver el ticket");
        await expect(propuesta).toContainText("La orden OT-000001 se cerró (exitosa).");
        await expect(propuesta).toContainText("confírmalo con «Resolver»");
        await expect(propuesta.getByRole("link", { name: "Ver la orden" })).toHaveAttribute("href", "/ordenes/1");
        await expect(page.getByText("Nuevo").first()).toBeVisible();
        await page.screenshot({ path: path.join(carpeta, `28-ticket-propuesta-orden-${info.project.name}.png`) });
    });
});
