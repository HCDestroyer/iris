import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { simularAdjuntos } from "./api-adjuntos-conv";
import { AHORA_CONV } from "./datos-conversaciones";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";

/*
 * Cuentas bancarias de Argos (contrato con Argos 0.8.0 §7.8, B5): la boleta elige la cuenta con su
 * banco, tipo, alias, número y titular; «Enviar cuentas» manda el texto fijo que arma el servidor; el
 * admin ve los textos con números de cuenta escritos a mano. En los 4 proyectos y sin desbordes.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-cuentas");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });

async function abrir(page: Page, ruta: string) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page);
    await simularAdjuntos(page);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

test.describe("cuentas bancarias de Argos", () => {
    test("la boleta muestra banco, tipo, alias, número y titular de cada cuenta", async ({ page }) => {
        const pedida = page.waitForRequest((r) => r.url().includes("/conversaciones/cuentas-bancarias"));
        await abrir(page, "/conversaciones/41/boleta");
        expect(new URL((await pedida).url()).searchParams.get("conversacionId")).toBe("41");
        const cuentas = page.getByRole("radiogroup", { name: "Cuenta de la empresa" });
        await expect(cuentas.getByRole("radio")).toHaveCount(3);
        const bi = page.locator('[data-cuenta="1"]');
        await expect(bi).toContainText("Banco Industrial · Monetaria");
        await expect(bi).toContainText("Principal");
        await expect(bi).toContainText("000-123456-7");
        await expect(bi).toContainText("A nombre de Internet del Norte, S.A.");
        await expect(page.locator('[data-cuenta="3"]')).toContainText("Sin titular en Argos");
        await cuentas.getByRole("radio", { name: /Banrural · Ahorro/ }).check({ force: true });
        await expect(cuentas.getByRole("radio", { name: /Banrural · Ahorro/ })).toBeChecked();
        await esperarSinDesbordes(page);
        await captura(page, "boleta-cuentas");
    });

    test("«Enviar cuentas» muestra el texto fijo y lo manda al cliente", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        // En el teléfono las herramientas del compositor van detrás de «+».
        const mas = page.getByRole("button", { name: "Adjuntar y más herramientas" });
        await expect(page.getByRole("button", { name: "Cuentas bancarias" }).or(mas).first()).toBeVisible();
        if (await mas.isVisible()) await mas.click();
        await page.getByRole("button", { name: "Cuentas bancarias" }).click();
        const d = page.getByRole("dialog", { name: "Enviar cuentas" });
        const texto = d.getByTestId("texto-cuentas");
        await expect(texto).toContainText("Cuentas para depositar o transferir");
        await expect(texto).toContainText("No. 000-123456-7");
        await expect(texto).toContainText("A nombre de: Internet del Norte, S.A.");
        await expect(texto).not.toContainText("90-1234567-8");
        await expect(d).toContainText("1 cuenta no va porque no tiene titular en Argos.");
        // El diálogo se agranda un poco al entrar: se espera a que se asiente.
        await expect.poll(() => buscarDesbordes(page), "nada se desborda ni queda fuera de pantalla").toEqual([]);
        await captura(page, "enviar-cuentas");
        await d.getByRole("button", { name: "Enviar al cliente" }).click();
        await expect(d).toBeHidden();
        await expect(page.getByText("Cuentas enviadas al cliente")).toBeVisible();
        expect(api.conversaciones.enviadosCuentas).toHaveLength(1);
    });

    test("el admin ve los textos con números de cuenta escritos a mano", async ({ page }) => {
        await page.clock.setFixedTime(new Date(AHORA_CONV));
        const api = await simularApi(page);
        const base = api.conversaciones.respuestas[0];
        if (!base) throw new Error("sin respuestas rápidas de ejemplo");
        api.conversaciones.respuestas.push({ ...base, id: 990, atajo: "/cuentas", titulo: "Cuentas", texto: "Nuestra cuenta monetaria BI 000-123456-7", activa: true });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion");
        const aviso = page.getByTestId("textos-con-cuentas");
        await expect(aviso).toContainText("1 texto parece tener un número de cuenta escrito a mano");
        await expect(aviso.getByRole("link", { name: /Respuesta rápida \/cuentas/ })).toHaveAttribute("href", "/administracion/respuestas-rapidas/990/editar");
        await esperarSinDesbordes(page);
        await captura(page, "textos-con-cuentas");
    });
});
