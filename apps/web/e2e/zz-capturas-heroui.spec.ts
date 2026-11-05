import { expect, test } from "@playwright/test";
import { ADMIN, EMPRESAS, conSesion, simularApi } from "./api-falsa";
import { buscarSugerencias } from "./controles";

// Capturas para el PM (temporal, no se commitea).
const carpeta = "/tmp/claude-0/-home-user-iris/d513675a-d7ae-5211-aa74-656ca7e704cf/scratchpad/heroui-completo";

test("capturas: Select, selector de cliente y fecha", async ({ page, context, baseURL }, info) => {
    const n = (que: string) => `${carpeta}/${que}-${info.project.name}.png`;
    await simularApi(page, { empresas: EMPRESAS });
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/ordenes");
    await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
    await page.getByRole("link", { name: "Nueva orden" }).click();
    const form = page.getByRole("form", { name: "Nueva orden" });
    await expect(form).toBeVisible();
    await form.getByRole("button", { name: /Tipo/ }).first().click();
    await expect(page.getByRole("listbox").filter({ visible: true })).toBeVisible();
    await page.waitForTimeout(400);
    await page.screenshot({ path: n("01-select") });
    await page.keyboard.press("Escape");
    await buscarSugerencias(form.getByRole("combobox", { name: "Cliente en Argos" }), "Ana");
    await expect(page.getByRole("option", { name: /Ana Morales/ })).toBeVisible();
    await page.waitForTimeout(400);
    await page.screenshot({ path: n("02-selector-cliente") });
    await page.keyboard.press("Escape");

    await page.goto("/ordenes/nueva?ticket=1");
    const desdeTicket = page.getByRole("form").first();
    await expect(desdeTicket.getByRole("group", { name: "Fecha", exact: true }).first()).toBeVisible();
    await desdeTicket.getByRole("button", { name: /Abrir calendario/ }).first().click();
    await expect(page.getByRole("dialog").filter({ visible: true }).last()).toBeVisible();
    await page.waitForTimeout(400);
    await page.screenshot({ path: n("03-fecha") });
});
