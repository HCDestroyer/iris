import { expect, test } from "@playwright/test";
import { ADMIN, EMPRESAS, conSesion, simularApi } from "./api-falsa";

const lecturasTickets = (api: Awaited<ReturnType<typeof simularApi>>) => api.peticiones.filter((p) => p.metodo === "GET" && p.ruta === "/api/v1/tickets");

test.describe("empresa activa", () => {
    test("sin conector no hay selector y no se manda X-Iris-Empresa", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(page.locator("[data-ticket]").filter({ visible: true }).first()).toBeVisible();
        await expect(page.getByLabel("Empresa activa")).toHaveCount(0);
        await expect(page.getByTestId("empresa-activa")).toHaveCount(0);
        expect(api.peticiones.every((p) => p.empresa === undefined)).toBe(true);
    });

    test("con una sola empresa muestra su nombre y no pide elegir", async ({ page, context, baseURL }) => {
        await simularApi(page, { empresas: [EMPRESAS[0] ?? { id: 10, nombre: "Cable Norte" }] });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(page.getByTestId("empresa-activa")).toHaveText("Cable Norte");
        await expect(page.getByText("Sin internet desde la mañana").filter({ visible: true })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Elige la empresa" })).toHaveCount(0);
    });

    test("con varias pide elegir, manda X-Iris-Empresa y recarga al cambiar", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { empresas: EMPRESAS });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(page.getByRole("heading", { name: "Elige la empresa" })).toBeVisible();
        await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
        await expect(page.getByText("Sin internet desde la mañana").filter({ visible: true })).toBeVisible();
        expect(lecturasTickets(api).at(-1)?.empresa).toBe("10");

        await page.getByLabel("Empresa activa").selectOption({ label: "Fibra Sur" });
        await expect(page.getByText("Antena desalineada (Fibra Sur)").filter({ visible: true })).toBeVisible();
        await expect(page.getByText("Sin internet desde la mañana").filter({ visible: true })).toHaveCount(0);
        expect(lecturasTickets(api).at(-1)?.empresa).toBe("20");

        // La elección se recuerda en este navegador.
        await page.reload();
        await expect(page.getByLabel("Empresa activa")).toHaveValue("20");
        await expect(page.getByText("Antena desalineada (Fibra Sur)").filter({ visible: true })).toBeVisible();
    });

    test("403 empresa_no_permitida olvida la elegida y vuelve a pedirla", async ({ page, context, baseURL }) => {
        await page.addInitScript(() => {
            if (!sessionStorage.getItem("iris-prueba")) {
                sessionStorage.setItem("iris-prueba", "1");
                localStorage.setItem("iris-empresa", "99");
            }
        });
        await simularApi(page, { empresas: EMPRESAS });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(page.getByText("La empresa que tenías elegida ya no está disponible")).toBeVisible();
        await expect(page.getByRole("heading", { name: "Elige la empresa" })).toBeVisible();
        expect(await page.evaluate(() => localStorage.getItem("iris-empresa"))).toBeNull();
        await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Fibra Sur" }).click();
        await expect(page.getByText("Antena desalineada (Fibra Sur)").filter({ visible: true })).toBeVisible();
    });

    test("422 empresa_requerida en una lectura abre la elección", async ({ page, context, baseURL }) => {
        await page.addInitScript(() => localStorage.setItem("iris-empresa", "10"));
        const api = await simularApi(page, { empresas: EMPRESAS });
        api.responderProblema("/api/v1/tickets", { type: "urn:iris:problema:empresa_requerida", title: "Elige una empresa", status: 422 }, 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(page.getByRole("heading", { name: "Elige la empresa" })).toBeVisible();
        await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
        await expect(page.getByText("Sin internet desde la mañana").filter({ visible: true })).toBeVisible();
    });

    test("403 sin_empresa muestra un estado vacío explicativo", async ({ page, context, baseURL }) => {
        await simularApi(page, { sinEmpresa: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(page.getByRole("heading", { name: "No tienes empresas asignadas" })).toBeVisible();
        await expect(page.locator("[data-ticket]")).toHaveCount(0);
    });

    test("un admin con datos sin empresa los adopta con confirmación (sin elegir empresa antes)", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { empresas: EMPRESAS, hayDatosSinEmpresa: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await expect(page.getByText("Hay datos de antes del conector sin empresa")).toBeVisible();
        await page.getByRole("link", { name: "Asignar a una empresa…" }).click();
        await expect(page.getByRole("heading", { name: "Datos sin empresa" })).toBeVisible();
        await page.getByLabel("Empresa que recibe los datos").selectOption({ label: "Fibra Sur" });
        await page.getByRole("button", { name: "Asignar datos…" }).click();
        const dialogo = page.getByRole("dialog");
        await expect(dialogo).toContainText("¿Asignar los datos sin empresa?");
        await dialogo.getByRole("button", { name: "Asignar a Fibra Sur" }).click();
        await expect(page.getByText("Datos asignados a Fibra Sur")).toBeVisible();
        await expect(page.getByText("Se asignaron 83 registros.")).toBeVisible();
        const adopcion = api.peticiones.find((p) => p.ruta === "/api/v1/empresas/20/adoptar-sin-empresa");
        expect(adopcion?.idempotencia).toBeTruthy();
        await page.goto("/");
        await expect(page.getByRole("heading", { name: "Elige la empresa" })).toBeVisible();
        await expect(page.getByText("Hay datos de antes del conector sin empresa")).toHaveCount(0);
    });
});
