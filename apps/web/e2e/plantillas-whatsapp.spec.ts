import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, EMPRESAS, simularApi, SOPORTE } from "./api-falsa";
import { WABA, type OpcionesPlantillasWA } from "./api-plantillas-wa";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Plantillas de WhatsApp (`.claude/integration/contratos/plantillas-whatsapp.md`), pantallas a–e de la
 * maqueta: la lista con la cuenta y «Sincronizar con Meta» (202 y relectura de la cuenta; 429), la
 * ficha (rechazo, usos, borrar con el aviso de los 30 días), el formulario con las reglas de Meta al
 * escribir y las sugeridas. En los 4 proyectos y sin desbordes en 390 px ni en 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-plantillas-wa");
const RUTA = "/administracion/plantillas-whatsapp";
const UNA = [EMPRESAS[0] ?? { id: 10, nombre: "Kingnet Solution" }];

async function abrir(page: Page, ruta = RUTA, opciones: OpcionesPlantillasWA = {}, usuario = ADMIN) {
    const api = await simularApi(page, { plantillasWA: opciones, empresas: UNA });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", usuario.correo);
    await page.goto(ruta);
    return api;
}

const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: true });
const fila = (page: Page, nombre: string) => page.locator(`[data-plantilla="${nombre}"]`).filter({ visible: true });

test.describe("Administración › Plantillas de WhatsApp", () => {
    test("lista: la cuenta, los conteos, lo que hay que atender y sincronizar con Meta", async ({ page }) => {
        const api = await abrir(page);
        await expect(page.getByRole("heading", { name: "Plantillas de WhatsApp", level: 1 })).toBeVisible();
        await expect(page.locator('[data-slot="cuenta"]')).toContainText(`Cuenta de WhatsApp ${WABA}`);
        await expect(page.locator('[data-slot="cuenta"]')).toContainText("+502 2200-0040 (Avisos)");
        await expect(fila(page, "aviso_corte")).toContainText("Rechazada · corregir");
        await expect(fila(page, "recordatorio_pago")).toContainText("Aprobada");
        await expect(fila(page, "promo_fibra_300")).toContainText("Pausada hasta");
        await expect(fila(page, "bienvenida_v1")).toContainText("Borrada en Meta");
        await expect(page.getByRole("radio", { name: /Atender/ })).toContainText("3");
        await esperarSinDesbordes(page);
        await captura(page, "a-lista");

        // El botón encola (202), muestra «Sincronizando…» y relee la cuenta hasta ver el resultado.
        await page.getByRole("button", { name: "Sincronizar con Meta" }).click();
        await expect(page.getByRole("button", { name: "Sincronizando…" })).toBeVisible();
        const resultado = page.locator('[data-slot="resultado-sincronizacion"]');
        await expect(resultado).toContainText("Revisamos 5 plantillas", { timeout: 10_000 });
        await expect(resultado).toContainText("promo_fibra_300");
        await expect(resultado).toContainText("Ya no está en Meta");
        await expect(page.getByRole("button", { name: "Sincronizar con Meta" })).toBeEnabled();
        expect(api.plantillasWA.recibido.sincronizaciones).toBe(1);
        await esperarSinDesbordes(page);
    });

    test("sincronizar antes de 2 minutos: lo dice en llano (429)", async ({ page }) => {
        await abrir(page, RUTA, { sincronizadaHacePoco: true });
        await page.getByRole("button", { name: "Sincronizar con Meta" }).click();
        const aviso = page.locator('[data-error="sincronizada_hace_poco"]');
        await expect(aviso).toContainText("Ya se sincronizó hace un momento");
        await expect(aviso).toContainText("en 2 min");
    });

    test("la cuenta sin permiso en Meta: qué hacer, con el enlace a credenciales", async ({ page }) => {
        await abrir(page, RUTA, { errorCuenta: "meta_sin_permiso" });
        const aviso = page.locator('[data-error="meta_sin_permiso"]');
        await expect(aviso).toBeVisible();
        await expect(aviso.getByRole("link", { name: "Ir a credenciales" })).toHaveAttribute("href", "/administracion/numeros");
        await captura(page, "e-errores");
    });

    test("ficha de una rechazada: por qué, cómo arreglarla, usos y borrar con el aviso de 30 días", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/501`);
        await expect(page.getByRole("heading", { name: "Aviso de corte", level: 1 })).toBeVisible();
        const rechazo = page.locator('[data-slot="rechazo"]');
        await expect(rechazo).toContainText("dos datos pegados");
        await expect(rechazo).toContainText("Cómo arreglarlo:");
        await expect(rechazo.getByRole("link", { name: "Corregir y volver a enviar" })).toHaveAttribute("href", `${RUTA}/501/editar`);
        await expect(page.getByText("Mientras no esté aprobada, Iris no manda «Corte por falta de pago»")).toBeVisible();
        await expect(page.locator('[data-slot="historial"]')).toContainText("Enviada a revisión");
        await esperarSinDesbordes(page);
        await captura(page, "c-detalle");

        // Usos: sin pasar por Meta.
        const usos = page.locator('[data-slot="usos"]');
        await usos.getByText("Iniciar conversación", { exact: true }).click();
        await usos.getByRole("button", { name: "Guardar usos" }).click();
        await expect(page.getByText("Usos guardados")).toBeVisible();
        expect(api.plantillasWA.recibido.usos.at(-1)).toMatchObject({ usos: ["avisos", "conversaciones"], codigos: ["corte_por_falta_de_pago"] });

        await page.getByRole("button", { name: "Borrar" }).click();
        const dialogo = page.getByRole("dialog");
        await expect(dialogo).toContainText("no deja usar el nombre «aviso_corte» otra vez durante 30 días");
        await dialogo.getByRole("button", { name: "Borrar" }).click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        expect(api.plantillasWA.recibido.borradas).toEqual([501]);
    });

    test("nueva: nombre en Meta, datos amigables y las reglas de Meta al escribir", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/nueva?cuenta=${WABA}`);
        await expect(page.getByRole("heading", { name: "Nueva plantilla", level: 1 })).toBeVisible();
        await page.getByRole("textbox", { name: "Nombre", exact: true }).fill("Recordatorio de pago");
        await expect(page.getByText("recordatorio_de_pago")).toBeVisible();
        const mensaje = page.getByRole("textbox", { name: "Mensaje" });
        await mensaje.fill("Hola");
        await page.getByRole("group", { name: "Insertar dato" }).getByRole("button", { name: "Nombre del cliente" }).click();
        await expect(mensaje).toHaveValue("Hola {{cliente_nombre}}");
        await mensaje.press("End");
        await mensaje.pressSequentially(", vence el");
        await page.getByRole("group", { name: "Insertar dato" }).getByRole("button", { name: "Fecha de corte" }).click();
        // Termina con un dato: no se puede enviar todavía.
        await expect(page.locator('[data-regla="variables_bordes"]')).toHaveAttribute("data-ok", "false");
        await expect(page.locator('[data-accion="enviar-meta"]')).toBeDisabled();
        await expect(page.locator('[data-slot="vista-previa"]').first()).toContainText("María López");
        await esperarSinDesbordes(page);
        await captura(page, "b-nueva");

        await mensaje.press("End");
        await mensaje.pressSequentially(". ¡Gracias!");
        await expect(page.locator('[data-regla="variables_bordes"]')).toHaveAttribute("data-ok", "true");
        await page.locator('[data-accion="enviar-meta"]').click();
        await expect(page).toHaveURL(/\/administracion\/plantillas-whatsapp\/\d+$/);
        await expect(page.getByText("«Recordatorio de pago» enviada a Meta")).toBeVisible();
        expect(api.plantillasWA.recibido.creadas.at(-1)).toMatchObject({
            nombre: "recordatorio_de_pago",
            enviar: true,
            definicion: { cuerpo: "Hola {{cliente_nombre}}, vence el {{fecha_corte}}. ¡Gracias!" },
        });
    });

    test("sugeridas: las que ya tienes y crear una con un toque", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/sugeridas?cuenta=${WABA}`);
        await expect(page.getByRole("heading", { name: "Plantillas sugeridas para tu ISP", level: 1 })).toBeVisible();
        await expect(page.locator('[data-sugerida="aviso_corte"]')).toContainText("Ya la tienes · rechazada");
        await expect(page.locator('[data-sugerida="recordatorio_pago"]')).toContainText("Ya la tienes · aprobada");
        await expect(page.getByRole("button", { name: "Crear las 5 que faltan" })).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "d-sugeridas");

        await page.locator('[data-sugerida="tecnico_en_camino"]').getByRole("button", { name: "Crear" }).click();
        await expect(page.getByText("«Técnico en camino» enviada a Meta")).toBeVisible();
        await expect(page.locator('[data-sugerida="tecnico_en_camino"]')).toContainText("Ya la tienes · en revisión");
        expect(api.plantillasWA.recibido.sugeridas).toEqual(["tecnico_en_camino"]);
    });

    test("soporte ve y sincroniza, pero no crea ni borra", async ({ page }) => {
        await abrir(page, RUTA, {}, SOPORTE);
        await expect(fila(page, "aviso_corte")).toBeVisible();
        await expect(page.getByRole("button", { name: "Sincronizar con Meta" })).toBeVisible();
        await expect(page.getByRole("link", { name: "Nueva plantilla" })).toHaveCount(0);
        await page.goto(`${RUTA}/502`);
        await expect(page.getByRole("heading", { name: "Recordatorio de pago", level: 1 })).toBeVisible();
        await expect(page.getByRole("button", { name: "Borrar" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Guardar usos" })).toHaveCount(0);
    });
});
