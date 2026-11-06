import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { esTelefono } from "./ayudas";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Administración de la bandeja: números de WhatsApp (uso, cola, horario, fuera de horario),
 * horarios con franjas y feriados, biblioteca de manuales (vista segura) y respuestas rápidas; y la
 * pestaña «Ayuda» de la conversación con los manuales sugeridos.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function abrir(page: Page, ruta: string) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

test.describe("administración de la bandeja", () => {
    test("números de WhatsApp: lista y configurar en pantalla propia", async ({ page }, info) => {
        const api = await abrir(page, "/administracion/numeros");
        await expect(page.getByRole("heading", { name: "Números de WhatsApp" })).toBeVisible();
        const avisos = page.locator('[data-numero="2"]');
        await expect(avisos).toContainText("Avisos de la empresa");
        await expect(avisos).toContainText("Todos los días 6:00–22:00");
        await expect(page.locator('[data-numero="4"]')).toContainText("Lun–Vie 8:00–17:00 · Sáb 8:00–12:00");
        await expect(page.locator('[data-numero="3"]')).toContainText("Mensaje automático y guardia para fallas masivas");
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `numeros-${info.project.name}.png`), fullPage: true });

        await page.getByRole("link", { name: "Configurar Soporte" }).click();
        const form = page.getByRole("form", { name: "Configurar Soporte" });
        await expect(form).toBeVisible();
        await expect(form.getByRole("switch", { name: "Que atienda un asistente" })).toBeDisabled();
        await expect(form.getByText("Disponible en la fase 2.")).toBeVisible();
        // Avisos de la empresa sin el uso «Avisos»: el validador espejo lo dice.
        await form.getByRole("switch", { name: "Los avisos de la empresa salen por este número" }).check();
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(form.getByText("Marca también el uso «Avisos»")).toBeVisible();
        await form.getByRole("checkbox", { name: "Avisos" }).check();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `numero-configurar-${info.project.name}.png`), fullPage: true });
        await form.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Número «Soporte» guardado")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/numeros$/);
        const cuerpo = api.conversaciones.configurados.at(-1) as { usos: string[]; avisosEmpresa: boolean; fueraDeHorario: { asistente: { activo: boolean } } };
        expect(cuerpo.usos).toEqual(expect.arrayContaining(["soporte", "avisos"]));
        expect(cuerpo.avisosEmpresa).toBe(true);
        expect(cuerpo.fueraDeHorario.asistente.activo).toBe(false);
        await expect(page.locator('[data-numero="3"]')).toContainText("Avisos de la empresa");
        await expect(page.locator('[data-numero="2"]')).not.toContainText("Avisos de la empresa");
    });

    test("horarios: franjas que cruzan la medianoche se piden como dos; feriados", async ({ page }, info) => {
        await abrir(page, "/administracion/horarios");
        await page.getByRole("link", { name: "Editar Caja" }).click();
        const form = page.getByRole("form", { name: "Editar «Caja»" });
        await expect(form).toBeVisible();
        await form.getByRole("button", { name: "Agregar feriado" }).click();
        await form.getByRole("textbox", { name: "Nombre" }).last().fill("Navidad");
        await form.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(form.getByText("Elige la fecha")).toBeVisible();
        await form.getByRole("button", { name: "Quitar el feriado 1" }).click();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `horario-${info.project.name}.png`), fullPage: true });
        await form.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Horario «Caja» guardado")).toBeVisible();
    });

    test("manuales: búsqueda, vista segura sin HTML y visibilidad", async ({ page }, info) => {
        await abrir(page, "/administracion/manuales");
        await expect(page.locator('[data-manual="1"]')).toContainText("Del equipo");
        await expect(page.locator('[data-manual="2"]')).toContainText("Para el cliente");
        await page.getByRole("searchbox", { name: "Buscar en los manuales" }).fill("onu");
        await page.getByRole("searchbox", { name: "Buscar en los manuales" }).press("Enter");
        await expect(page).toHaveURL(/q=onu/);
        await expect(page.locator('[data-manual="1"]')).toHaveCount(1);
        await page.getByRole("searchbox", { name: "Buscar en los manuales" }).fill("");
        await page.locator('[data-manual="1"]').getByRole("button", { name: "Ver" }).click();
        const dialogo = page.getByRole("dialog", { name: "Sin servicio en fibra: diagnóstico" });
        await expect(dialogo.getByRole("listitem")).toHaveCount(3);
        await expect(dialogo.getByText("<b>no es HTML</b>")).toBeVisible();
        await expect(dialogo.locator("b")).toHaveCount(0);
        await page.keyboard.press("Escape");
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `manuales-${info.project.name}.png`), fullPage: true });

        await page.getByRole("link", { name: "Nuevo manual" }).click();
        const form = page.getByRole("form", { name: "Nuevo manual" });
        await form.getByRole("textbox", { name: "Título" }).fill("Cambio de contraseña del WiFi");
        await form.getByRole("textbox", { name: "Resumen" }).fill("Pasos para el cliente desde el teléfono.");
        await form.getByRole("textbox", { name: "Contenido en Markdown" }).fill("1. Entra a **192.168.1.1**\n2. Cambia la clave");
        await form.getByRole("radio", { name: "Vista previa" }).click();
        await expect(form.getByRole("region", { name: "Vista previa" }).locator("strong")).toHaveText("192.168.1.1");
        await form.getByRole("radio", { name: /Para el cliente/ }).check();
        await form.getByRole("button", { name: "Crear manual" }).click();
        await expect(page.getByText("Manual «Cambio de contraseña del WiFi» creado")).toBeVisible();
    });

    test("respuestas rápidas: atajo repetido junto al campo y alta", async ({ page }) => {
        await abrir(page, "/administracion/respuestas-rapidas/nueva");
        const form = page.getByRole("form", { name: "Nueva respuesta rápida" });
        await form.getByRole("textbox", { name: "Atajo" }).fill("/visita");
        await form.getByRole("textbox", { name: "Título" }).fill("Otra visita");
        await form.getByRole("textbox", { name: "Texto" }).fill("Hola {{nombre}}, vamos mañana.");
        await expect(form.getByText("Hola Carlos, vamos mañana.")).toBeVisible();
        await form.getByRole("button", { name: "Crear respuesta" }).click();
        await expect(form.getByText("Ya hay una respuesta con ese atajo")).toBeVisible();
        await form.getByRole("textbox", { name: "Atajo" }).fill("/manana");
        await form.getByRole("button", { name: "Crear respuesta" }).click();
        await expect(page.getByText("Respuesta «Otra visita» creada")).toBeVisible();
        await expect(page.locator('[data-respuesta="/manana"]')).toContainText("Todas las colas");
        await esperarSinDesbordes(page);
    });

    test("pestaña Ayuda: sugeridos con su motivo, usar en la respuesta y enviar al cliente", async ({ page }, info) => {
        const api = await abrir(page, "/conversaciones?c=41");
        if (esTelefono(page)) await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        else await page.getByRole("button", { name: "Manuales" }).click();
        const ficha = esTelefono(page) ? page.getByRole("dialog", { name: "Ficha del cliente" }) : page.getByRole("complementary", { name: "Ficha del cliente" });
        if (esTelefono(page)) await ficha.getByRole("button", { name: "Ayuda" }).click();
        const sugeridos = ficha.getByRole("region", { name: "Sugeridos para esta conversación" });
        await expect(sugeridos).toContainText("Comparte una etiqueta");
        await expect(sugeridos).toContainText("Equipo del servicio");
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-ayuda-${info.project.name}.png`) });
        await sugeridos.locator('[data-manual="2"]').getByRole("button", { name: "Enviar al cliente" }).click();
        await expect(page.getByText("Enviaste «Reiniciar la ONU ZTE F670L» a Carlos Ruiz")).toBeVisible();
        expect(api.conversaciones.enviados.some((e) => e.ruta.endsWith("/enviar-manual"))).toBe(true);
        await sugeridos.locator('[data-manual="1"]').getByRole("button", { name: "Usar en la respuesta" }).click();
        if (esTelefono(page)) await page.getByRole("button", { name: "Cerrar la ficha" }).click();
        await expect(page.getByRole("textbox", { name: /Mensaje para/ })).toHaveValue(/1\. Revisa si hay falla en la zona\./);
    });
});
