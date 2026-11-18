import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { ADMIN, AGENTE_CONV, conSesion, simularApi } from "./api-falsa";
import { esTelefono } from "./ayudas";
import { AHORA_CONV, mensaje } from "./datos-conversaciones";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";

/*
 * Rechazo de Meta a la vista (contratos/conversaciones.md §1.1 y voz.md): por qué no se envió un
 * mensaje, en llano y con el camino para quien administra los números; el número marcado por un
 * envío rechazado y su vuelta a «Conectado»; y el aviso de llamadas rechazadas con las llamadas
 * apagadas. En los 4 proyectos y sin desbordes. Capturas a escala 2.
 */
test.use({ deviceScaleFactor: 2 });

type Api = Awaited<ReturnType<typeof simularApi>>;
type Mensaje = Schema<"MensajeConversacion">;
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-rechazo-meta");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });
/** Sin desbordes, una vez asentado lo que entra con animación (avisos emergentes). */
async function sinDesbordes(page: Page) {
    await expect.poll(async () => (await buscarDesbordes(page)).length, { timeout: 5_000 }).toBe(0).catch(() => undefined);
    await esperarSinDesbordes(page);
}

const fallido = (motivoFalla: string, texto: string, codigoFalla?: Schema<"CodigoFalla">): Mensaje => mensaje(41, { direccion: "saliente", texto, estadoEntrega: "fallido", motivoFalla, ...(codigoFalla ? { codigoFalla } : {}) });

async function abrirHilo(page: Page, correo: string, mensajes: (admin: boolean) => Mensaje[]) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page);
    // El servidor solo manda `codigoFalla` a quien administra los números.
    api.conversaciones.mensajes.get(41)?.push(...mensajes(correo === ADMIN.correo));
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto("/conversaciones?c=41");
    const hilo = page.getByRole("region", { name: /^Conversación con / }).getByRole("log", { name: "Mensajes" });
    await expect(hilo).toBeVisible();
    return { api, hilo };
}

const TRES = (admin: boolean) => [
    fallido("token_invalido", "Le confirmo la visita de mañana.", admin ? { status: 401, codigo: 190, subcodigo: 463 } : undefined),
    fallido("sin_permiso_meta", "¿Sigue sin servicio?", admin ? { status: 403, codigo: 200, subcodigo: null } : undefined),
    fallido("rechazado_por_canal", "Gracias por avisar.", admin ? { status: 403, codigo: 131031, subcodigo: null } : undefined),
];

test.describe("rechazo de Meta › por qué no se envió un mensaje", () => {
    test("quien administra los números ve el motivo, el código de WhatsApp y el enlace al número", async ({ page }) => {
        const { hilo } = await abrirHilo(page, ADMIN.correo, TRES);
        const token = hilo.locator('[data-slot="falla-mensaje"][data-motivo="token_invalido"]');
        await expect(token).toContainText("No se envió: el token de WhatsApp de este número venció o no sirve. Revísalo en Números de WhatsApp.");
        await expect(token.locator('[data-slot="codigo-falla"]')).toHaveText("Código de WhatsApp: 190 (HTTP 401), subcódigo 463");
        const permiso = hilo.locator('[data-slot="falla-mensaje"][data-motivo="sin_permiso_meta"]');
        await expect(permiso).toContainText("No se envió: al token de WhatsApp de este número le falta un permiso.");
        await expect(permiso.locator('[data-slot="codigo-falla"]')).toHaveText("Código de WhatsApp: 200 (HTTP 403)");
        // Rechazado por otra causa (cuenta, destinatario): el código, pero sin mandar a revisar el número.
        const otro = hilo.locator('[data-slot="falla-mensaje"][data-motivo="rechazado_por_canal"]');
        await expect(otro).toContainText("WhatsApp no aceptó el mensaje.");
        await expect(otro.locator('[data-slot="codigo-falla"]')).toHaveText("Código de WhatsApp: 131031 (HTTP 403)");
        await expect(otro.getByRole("link")).toHaveCount(0);
        // El pie de la burbuja ya no dice «token invalido».
        await expect(hilo).not.toContainText(/token invalido|sin permiso meta/);
        await otro.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "hilo-fallidos-administrador");
        // El enlace lleva a las credenciales de ESE número.
        await expect(token.getByRole("link", { name: "Revisar el número" })).toHaveAttribute("href", /\/administracion\/numeros\/\d+\/credenciales$/);
        await token.getByRole("link", { name: "Revisar el número" }).click();
        await expect(page).toHaveURL(/\/administracion\/numeros\/\d+\/credenciales$/);
    });

    test("un agente sin ese permiso ve qué pasó y a quién avisar, sin enlace ni código", async ({ page }) => {
        const { hilo } = await abrirHilo(page, AGENTE_CONV.correo, TRES);
        const token = hilo.locator('[data-slot="falla-mensaje"][data-motivo="token_invalido"]');
        await expect(token).toContainText("No se envió: WhatsApp rechazó la conexión de este número. Avisa a un administrador.");
        await expect(hilo.locator('[data-slot="falla-mensaje"][data-motivo="sin_permiso_meta"]')).toContainText("a la conexión de este número le falta un permiso en WhatsApp. Avisa a un administrador.");
        await expect(hilo.locator('[data-slot="falla-mensaje"][data-motivo="rechazado_por_canal"]')).toContainText("WhatsApp no aceptó el mensaje.");
        await expect(hilo.locator('[data-slot="falla-mensaje"]').getByRole("link")).toHaveCount(0);
        await expect(hilo.locator('[data-slot="codigo-falla"]')).toHaveCount(0);
        await token.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "hilo-fallidos-agente");
    });

    test("los demás fallos se quedan con su pie de siempre y un mensaje enviado no dice nada", async ({ page }) => {
        const { hilo } = await abrirHilo(page, ADMIN.correo, () => [fallido("numero_invalido", "Hola"), mensaje(41, { direccion: "saliente", texto: "Sí salió", estadoEntrega: "entregado" })]);
        await expect(hilo.getByText("no se envió (número inválido)")).toBeVisible();
        await expect(hilo.locator('[data-slot="falla-mensaje"]')).toHaveCount(0);
    });
});

test.describe("rechazo de Meta › el número queda marcado y vuelve solo", () => {
    test("un envío rechazado marca el número sin pulsar «Probar»; el primero que Meta acepta lo devuelve a «Conectado»", async ({ page }) => {
        const api: Api = await simularApi(page);
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/numeros");
        const fila = page.locator('[data-numero="3"]');
        await expect(fila.locator("[data-conexion]")).toHaveAttribute("data-conexion", "conectado");
        await expect(fila.locator('[data-slot="consecuencia-conexion"]')).toHaveCount(0);
        const volverALaPestana = () =>
            page.evaluate(() => {
                document.dispatchEvent(new Event("visibilitychange"));
            });

        // Meta rechaza el token al enviar: al volver a la pestaña, la lista ya lo dice.
        api.numeros.marcarPorEnvio(3, "token_invalido");
        await volverALaPestana();
        await expect(fila.locator("[data-conexion]")).toHaveAttribute("data-conexion", "token_invalido");
        await expect(fila).toContainText("Token vencido");
        await expect(fila.locator('[data-slot="consecuencia-conexion"]')).toHaveText("WhatsApp está rechazando los mensajes de este número.");
        await fila.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "numero-marcado-por-envio");

        // Le falta un permiso: también se marca.
        api.numeros.marcarPorEnvio(3, "sin_permiso");
        await volverALaPestana();
        await expect(fila).toContainText("Sin permiso en Meta");

        // El siguiente envío sale bien: vuelve a «Conectado» sola.
        api.numeros.marcarPorEnvio(3, "conectado");
        await volverALaPestana();
        await expect(fila.locator("[data-conexion]")).toHaveAttribute("data-conexion", "conectado");
        await expect(fila.locator('[data-slot="consecuencia-conexion"]')).toHaveCount(0);
    });

    test("si releer la lista falla al volver a la pestaña, lo que había se queda (sin error en pantalla)", async ({ page }) => {
        await simularApi(page);
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/numeros");
        const fila = page.locator('[data-numero="3"]');
        await expect(fila).toBeVisible();
        let fallos = 0;
        await page.route("**/api/v1/mensajeria/canales", (route) => {
            if (route.request().method() !== "GET") return route.fallback();
            fallos++;
            return route.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }) });
        });
        await page.evaluate(() => {
            document.dispatchEvent(new Event("visibilitychange"));
        });
        await expect.poll(() => fallos).toBeGreaterThan(0);
        await expect(fila).toBeVisible();
        await expect(page.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
    });
});

test.describe("rechazo de Meta › llamadas rechazadas con las llamadas apagadas", () => {
    const hace = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

    async function abrirLlamadas(page: Page, voz: NonNullable<Parameters<typeof simularApi>[1]>["voz"]) {
        const api = await simularApi(page, { voz: { recibe: true, ...voz } });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/numeros/3/llamadas");
        const f = page.getByRole("form", { name: "Llamadas de Soporte" });
        await expect(f).toBeVisible();
        return { api, f, aviso: f.locator('[data-slot="rechazadas-apagadas"]') };
    }

    test("dice cuántas y desde cuándo, y «Encender las llamadas» deja el interruptor encendido para guardar", async ({ page }) => {
        const { api, f, aviso } = await abrirLlamadas(page, { rechazadasApagadas: { ultimaEn: hace(12), hoy: 3 } });
        await expect(aviso).toContainText("Se están rechazando llamadas de clientes");
        await expect(aviso).toContainText("Llegaron llamadas a este número y se rechazaron porque las llamadas están apagadas en Iris. La última, hace 12 min; hoy van 3.");
        const interruptor = f.getByRole("switch", { name: "Este número recibe llamadas de WhatsApp" });
        await expect(interruptor).not.toBeChecked();
        await sinDesbordes(page);
        await captura(page, "llamadas-rechazadas-apagadas");
        await aviso.getByRole("button", { name: "Encender las llamadas" }).click();
        await expect(interruptor).toBeChecked();
        await expect(aviso).toContainText("Quedan encendidas al guardar.");
        await expect(aviso.getByRole("button", { name: "Encender las llamadas" })).toHaveCount(0);
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Soporte ya recibe llamadas")).toBeVisible();
        expect(api.voz.recibido.configurados.at(-1)?.cuerpo).toMatchObject({ llamadasActivas: true });
        // Ya encendidas, el aviso deja de venir.
        await page.goto("/administracion/numeros/3/llamadas");
        await expect(page.getByRole("form", { name: "Llamadas de Soporte" })).toBeVisible();
        await expect(page.locator('[data-slot="rechazadas-apagadas"]')).toHaveCount(0);
    });

    test("si hoy no llegó ninguna, solo la fecha de la última; sin `rechazadasApagadas` no hay aviso", async ({ page }) => {
        const { aviso } = await abrirLlamadas(page, { rechazadasApagadas: { ultimaEn: hace(3 * 60), hoy: 0 } });
        await expect(aviso).toContainText("La última, hace 3 h.");
        await expect(aviso).not.toContainText("hoy va");
        const otra = await page.context().newPage();
        const sin = await abrirLlamadas(otra, {});
        await expect(sin.aviso).toHaveCount(0);
        await otra.close();
    });

    test("si faltan requisitos de Meta, el aviso lo dice y no ofrece encender", async ({ page }) => {
        const { aviso } = await abrirLlamadas(page, { sinRequisitos: true, rechazadasApagadas: { ultimaEn: hace(5), hoy: 1 } });
        await expect(aviso).toContainText("hoy va 1.");
        await expect(aviso).toContainText("Para encenderlas, primero hay que cumplir los requisitos de Meta de abajo.");
        await expect(aviso.getByRole("button")).toHaveCount(0);
        await sinDesbordes(page);
    });

    test("encender responde 409 (los ajustes cambiaron mientras tanto): se dice y «Recargar» trae lo nuevo para reintentar", async ({ page }) => {
        const { api, f, aviso } = await abrirLlamadas(page, { rechazadasApagadas: { ultimaEn: hace(12), hoy: 3 } });
        await aviso.getByRole("button", { name: "Encender las llamadas" }).click();
        // Iris volvió a comprobar con Meta (u otra persona editó) justo antes de guardar.
        const canal = api.voz.canal(3);
        const abierta = canal.version;
        canal.version += 1;
        canal.segundosSonando = 40;
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f).toContainText("Los ajustes de este número cambiaron mientras tanto");
        await expect(f).toContainText("Lo que cambiaste aquí no se guardó.");
        await sinDesbordes(page);
        await captura(page, "llamadas-409");
        await f.getByRole("button", { name: "Recargar" }).click();
        // Se lee lo nuevo, sin recargar la página, y el segundo intento pasa.
        const g = page.getByRole("form", { name: "Llamadas de Soporte" });
        await expect(g.getByLabel("Segundos que suena")).toHaveValue("40");
        await expect(g).not.toContainText("cambiaron mientras tanto");
        await g.locator('[data-slot="rechazadas-apagadas"]').getByRole("button", { name: "Encender las llamadas" }).click();
        await g.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Soporte ya recibe llamadas")).toBeVisible();
        expect(api.voz.recibido.configurados.map((c) => (c.cuerpo as { version: number }).version)).toEqual([abierta, abierta + 1]);
    });
});

test("rechazo de Meta › en el teléfono el aviso del mensaje fallido se lee entero", async ({ page }) => {
    test.skip(!esTelefono(page), "Solo teléfono.");
    const { hilo } = await abrirHilo(page, ADMIN.correo, TRES);
    const token = hilo.locator('[data-slot="falla-mensaje"][data-motivo="token_invalido"]');
    await token.scrollIntoViewIfNeeded();
    const caja = await token.boundingBox();
    expect(caja && caja.x >= 0 && caja.x + caja.width <= 390).toBe(true);
    expect(await token.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
});
