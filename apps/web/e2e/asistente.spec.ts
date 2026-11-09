import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, conSesion, simularApi } from "./api-falsa";
import { TEXTO_OTRA, TEXTO_SUGERIDO, type OpcionesAsistente } from "./api-asistente";
import { abrirMenu, esTelefono, hojaMas } from "./ayudas";
import { elegirOpcion } from "./controles";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Asistente de IA (contratos/conversaciones.md §16; diseño del PM): la tarjeta «Sugerencia del
 * asistente» en cada estado, «Usar» → compositor → enviar con `sugerenciaId`, «Otra», 👍/👎,
 * ocultar, error y «Reintentar», modo botón, pausa, vencida, etiquetar, el resumen en la ficha y
 * la pantalla «Administración › Asistente de IA». En los 4 proyectos y sin desbordes en 390 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-asistente");
const compositor = (page: Page) => page.getByRole("textbox", { name: /Mensaje para/ });
const tarjeta = (page: Page) => page.getByRole("region", { name: "Sugerencia del asistente" });

async function abrir(page: Page, asistente: OpcionesAsistente = {}, ruta = "/conversaciones?c=41", correo = ADMIN.correo) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { conversaciones: { asistente } });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}

const rutas = (api: Awaited<ReturnType<typeof abrir>>, fin: string) => api.conversaciones.enviados.filter((e) => e.ruta.endsWith(fin));

test.describe("asistente de IA en la bandeja", () => {
    test("tarjeta lista: fuentes, nota, «Usar» pasa el texto a la caja y el envío lleva sugerenciaId", async ({ page }, info) => {
        const api = await abrir(page);
        const t = tarjeta(page);
        await expect(t).toContainText(TEXTO_SUGERIDO);
        const fuentes = t.getByRole("list", { name: "Fuentes de la sugerencia" });
        await expect(fuentes.locator('[data-fuente="horus"]')).toContainText("Falla en Zona 4 · Horus");
        await expect(fuentes.locator('[data-fuente="argos"]')).toContainText("Servicio activo, al día · Argos");
        await expect(fuentes.locator('[data-fuente="manual"]')).toContainText("Manual «Sin internet (FTTH)»");
        await expect(t).toContainText("Solo la ves tú. «Usar» la pasa a tu caja de texto: la revisas y la envías tú.");
        await expect(t.getByRole("button", { name: "Útil", exact: true })).toBeVisible();
        await expect(t.getByRole("button", { name: "No útil" })).toBeVisible();
        await expect(t.getByRole("button", { name: "Etiquetar «Falla masiva»" })).toBeVisible();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-sugerencia-${info.project.name}.png`) });

        await t.getByRole("button", { name: "Usar" }).click();
        await expect(compositor(page)).toHaveValue(TEXTO_SUGERIDO);
        await expect(t).toHaveCount(0);
        expect(rutas(api, "/usar")).toHaveLength(1);
        // Nada salió todavía: lo envía la persona.
        expect(rutas(api, "/mensajes")).toHaveLength(0);
        await compositor(page).fill(`${TEXTO_SUGERIDO} Gracias.`);
        await compositor(page).press("Enter");
        await expect(page.getByRole("log", { name: "Mensajes" }).locator('[data-direccion="saliente"]').last()).toContainText("Gracias.");
        const envio = rutas(api, "/mensajes")[0]?.cuerpo as { sugerenciaId?: string; texto?: string };
        expect(envio.sugerenciaId).toBe(api.conversaciones.asistente.sugerencias[0]?.id);
        expect(envio.texto).toBe(`${TEXTO_SUGERIDO} Gracias.`);
    });

    test("«Otra» pide otra (otra: true), muestra «Preparando sugerencia…» y llega la nueva", async ({ page }) => {
        const api = await abrir(page);
        const t = tarjeta(page);
        await t.getByRole("button", { name: "Otra" }).click();
        await expect(t).toContainText("Preparando sugerencia…");
        await expect(t).toHaveAttribute("data-estado", "generando");
        await expect(t).toContainText(TEXTO_OTRA);
        expect(rutas(api, "/sugerencias")[0]?.cuerpo).toEqual({ otra: true });
        expect(rutas(api, "/sugerencias")[0]?.idempotencia).toBeTruthy();
    });

    test("👍/👎 se marcan y se pueden cambiar; ocultar la descarta", async ({ page }) => {
        const api = await abrir(page);
        const t = tarjeta(page);
        await t.getByRole("button", { name: "Útil", exact: true }).click();
        await expect(t.getByRole("button", { name: "Útil", exact: true })).toHaveAttribute("aria-pressed", "true");
        await expect(page.getByText("Gracias: marcaste la sugerencia como útil")).toBeVisible();
        await t.getByRole("button", { name: "No útil" }).click();
        await expect(t.getByRole("button", { name: "No útil" })).toHaveAttribute("aria-pressed", "true");
        await expect(t.getByRole("button", { name: "Útil", exact: true })).toHaveAttribute("aria-pressed", "false");
        expect(rutas(api, "/calificar").map((e) => e.cuerpo)).toEqual([{ util: true }, { util: false }]);

        await t.getByRole("button", { name: "Ocultar la sugerencia" }).click();
        await expect(t).toHaveCount(0);
        await expect.poll(() => rutas(api, "/descartar").length).toBe(1);
        await expect.poll(() => api.conversaciones.asistente.sugerencias[0]?.estado).toBe("descartada");
    });

    test("«Etiquetar «X»» usa la acción de etiquetar con las etiquetas que ya tenía", async ({ page }) => {
        const api = await abrir(page);
        await tarjeta(page).getByRole("button", { name: "Etiquetar «Falla masiva»" }).click();
        await expect(page.getByText("Etiqueta «Falla masiva» puesta en Carlos Ruiz")).toBeVisible();
        await expect(tarjeta(page).getByRole("button", { name: "Ya tiene «Falla masiva»" })).toBeDisabled();
        const e = rutas(api, "/etiquetar")[0];
        expect(e?.cuerpo).toEqual({ version: 7, etiquetaIds: [3, 6] });
    });

    test("se pliega para no tapar el hilo y en 390 px no se desborda", async ({ page }, info) => {
        await abrir(page);
        const t = tarjeta(page);
        const plegar = t.getByRole("button", { name: /Sugerencia del asistente/ });
        await expect(plegar).toHaveAttribute("aria-expanded", "true");
        await plegar.click();
        await expect(plegar).toHaveAttribute("aria-expanded", "false");
        await expect(t.getByRole("button", { name: "Usar" })).toHaveCount(0);
        if (esTelefono(page)) {
            // El último mensaje del cliente sigue a la vista con la tarjeta plegada y la caja con foco.
            await compositor(page).focus();
            await expect(page.getByRole("log", { name: "Mensajes" }).getByText("Ya lo reinicié y sigue igual.").last()).toBeInViewport();
        }
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-plegada-${info.project.name}.png`) });
        await plegar.click();
        await expect(t.getByRole("button", { name: "Usar" })).toBeVisible();
    });

    test("error del proveedor con «Reintentar»; tope alcanzado lo explica sin reintentar", async ({ page }, info) => {
        await abrir(page, { inicial: "error" });
        const t = tarjeta(page);
        await expect(t).toHaveAttribute("data-estado", "error");
        await expect(t).toContainText("No se pudo preparar la sugerencia.");
        await expect(t).toContainText("El servidor de IA no respondió a tiempo");
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-error-${info.project.name}.png`) });
        await t.getByRole("button", { name: "Reintentar" }).click();
        await expect(t).toContainText(TEXTO_SUGERIDO);

        await page.unrouteAll({ behavior: "ignoreErrors" });
        await abrir(page, { inicial: "tope" });
        await expect(tarjeta(page)).toContainText("El asistente está en pausa este mes.");
        await expect(tarjeta(page).getByRole("button", { name: "Reintentar" })).toHaveCount(0);
    });

    test("modo botón: «Sugerir» junto al compositor en vez de la tarjeta", async ({ page }, info) => {
        const api = await abrir(page, { estado: { modo: "boton" }, inicial: null });
        await expect(page.getByRole("log", { name: "Mensajes" })).toContainText("Ya lo reinicié");
        await expect(tarjeta(page)).toHaveCount(0);
        const sugerir = page.getByRole("button", { name: "Sugerir" });
        await expect(sugerir).toBeVisible();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-boton-${info.project.name}.png`) });
        await sugerir.click();
        await expect(tarjeta(page)).toContainText(TEXTO_SUGERIDO);
        await expect(sugerir).toHaveCount(0);
        expect(rutas(api, "/sugerencias")[0]?.cuerpo).toEqual({});
    });

    test("pausado por el tope: solo la explicación, sin «Sugerir»; apagado: nada", async ({ page }) => {
        await abrir(page, { estado: { modo: "boton", pausado: true }, inicial: null });
        await expect(page.getByText("El asistente está en pausa: se llegó al tope de uso del mes.")).toBeVisible();
        await expect(page.getByRole("button", { name: "Sugerir" })).toHaveCount(0);

        await page.unrouteAll({ behavior: "ignoreErrors" });
        await abrir(page, { estado: { activo: false, modo: "boton" }, inicial: null });
        await expect(page.getByRole("log", { name: "Mensajes" })).toContainText("Ya lo reinicié");
        await expect(page.getByRole("button", { name: "Sugerir" })).toHaveCount(0);
        await expect(page.locator('[data-slot="asistente-pausado"]')).toHaveCount(0);
    });

    test("vencida: llega otro mensaje del cliente y, en automático, la nueva reemplaza a la vieja", async ({ page }) => {
        const api = await abrir(page);
        await expect(tarjeta(page)).toContainText(TEXTO_SUGERIDO);
        api.conversaciones.mensajeDelCliente(41, "¿Ya hay respuesta?");
        await expect(tarjeta(page)).toContainText(TEXTO_OTRA);
        expect(api.conversaciones.asistente.sugerencias[0]?.estado).toBe("vencida");
    });

    test("la sugerencia venció entre «Usar» y «Enviar»: el mensaje sale igual, sin ella", async ({ page }) => {
        const api = await abrir(page);
        await tarjeta(page).getByRole("button", { name: "Usar" }).click();
        await expect(compositor(page)).toHaveValue(TEXTO_SUGERIDO);
        const s = api.conversaciones.asistente.sugerencias[0];
        if (s) s.estado = "vencida";
        await compositor(page).press("Enter");
        await expect(page.getByRole("log", { name: "Mensajes" }).locator('[data-direccion="saliente"]').last()).toContainText("Buen día, don Carlos");
        const envios = rutas(api, "/mensajes").map((e) => e.cuerpo as { sugerenciaId?: string });
        expect(envios).toHaveLength(2);
        expect(envios[0]?.sugerenciaId).toBeTruthy();
        expect(envios[1]?.sugerenciaId).toBeUndefined();
    });

    test("resumen del asistente en la ficha: «Resumir», cargando, resultado y error con «Reintentar»", async ({ page }, info) => {
        const api = await abrir(page, { inicial: null });
        let ficha = page.getByRole("complementary", { name: "Ficha del cliente" });
        if (esTelefono(page)) {
            await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
            ficha = page.getByRole("dialog", { name: "Ficha del cliente" });
        }
        const resumen = ficha.getByRole("region", { name: "Resumen del asistente" });
        await resumen.getByRole("button", { name: "Resumir" }).click();
        await expect(resumen.getByRole("status")).toContainText("Generando resumen…");
        await expect(resumen).toContainText("Cliente sin internet desde la mañana");
        await expect(resumen.getByRole("button", { name: "Volver a resumir" })).toBeVisible();
        expect(rutas(api, "/resumen")).toHaveLength(1);
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `ficha-resumen-${info.project.name}.png`) });

        await page.unrouteAll({ behavior: "ignoreErrors" });
        await abrir(page, { inicial: null, resumen: "error" });
        ficha = page.getByRole("complementary", { name: "Ficha del cliente" });
        if (esTelefono(page)) {
            await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
            ficha = page.getByRole("dialog", { name: "Ficha del cliente" });
        }
        const r2 = ficha.getByRole("region", { name: "Resumen del asistente" });
        await r2.getByRole("button", { name: "Resumir" }).click();
        await expect(r2.getByRole("alert")).toContainText("El servidor de IA no respondió a tiempo");
        await expect(r2.getByRole("button", { name: "Reintentar" })).toBeVisible();
    });

    test("quien atiende sin permiso de configurar ve la sugerencia pero no la pantalla de ajustes", async ({ page }) => {
        await abrir(page, {}, "/conversaciones?c=41", AGENTE_CONV.correo);
        await expect(tarjeta(page)).toContainText(TEXTO_SUGERIDO);
        await page.goto("/administracion");
        await expect(page.getByRole("link", { name: /Asistente de IA/ })).toHaveCount(0);
        await page.goto("/administracion/asistente");
        await expect(page.getByRole("form", { name: "Asistente de IA" })).toHaveCount(0);
    });
});

test.describe("administración › asistente de IA", () => {
    const formulario = (page: Page) => page.getByRole("form", { name: "Asistente de IA" });

    test("se llega desde Administración; proveedor, modelo, probar conexión ok y guardar con aviso", async ({ page }, info) => {
        const api = await abrir(page, {}, "/administracion");
        if (esTelefono(page)) {
            await abrirMenu(page);
            await expect(hojaMas(page).getByRole("link", { name: "Administración" })).toBeVisible();
            await page.keyboard.press("Escape");
        }
        await page.getByRole("link", { name: /Asistente de IA/ }).click();
        const f = formulario(page);
        await expect(f).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Asistente de IA");
        await expect(f.getByRole("switch", { name: "Asistente activo" })).toBeChecked();
        await expect(f.getByRole("radio", { name: "Ollama · servidor propio" })).toBeChecked();
        await expect(f.getByText("Recomendado")).toBeVisible();
        await expect(f.getByLabel("Dirección")).toHaveValue("http://10.0.0.20:11434");
        await expect(f.getByText("Los mensajes salen hacia el proveedor")).toHaveCount(0);
        await expect(f).toContainText("2.1 millones");
        await expect(f).toContainText("68 %");
        await expect(f.getByRole("progressbar", { name: "Tokens usados del tope del mes" })).toHaveAttribute("aria-valuenow", "42");
        await expect(f).toContainText("Envía un mensaje sin que lo revise una persona");

        await f.getByRole("button", { name: "Probar conexión" }).click();
        await expect(f.locator('[data-prueba="ok"]')).toHaveText("Respondió en 1.8 s · 28 palabras por segundo");
        expect(api.conversaciones.asistente.pruebas[0]).toEqual({ proveedor: "ollama", url: "http://10.0.0.20:11434", modelo: "qwen3:32b" });

        await f.locator(`[data-opcion="openai_compatible"]`).click();
        await expect(f.getByRole("radio", { name: "Compatible con OpenAI" })).toBeChecked();
        await expect(f.getByText("Los mensajes salen hacia el proveedor")).toBeVisible();
        await f.getByLabel("Dirección").fill("https://openrouter.ai/api/v1");
        await elegirOpcion(f.getByLabel("Modelo"), { label: "llama3.1:8b" });
        await f.locator(`[data-opcion="boton"]`).click();
        await f.getByRole("checkbox", { name: /Fallas de red de Horus/ }).click();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `ajustes-${info.project.name}.png`), fullPage: true });
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Asistente de IA guardado y activo")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion$/);
        const cuerpo = api.conversaciones.asistente.guardados[0] as Record<string, unknown>;
        expect(cuerpo).toMatchObject({ activo: true, proveedor: "openai_compatible", url: "https://openrouter.ai/api/v1", modelo: "llama3.1:8b", modo: "boton", version: 2, topeTokensMes: 5_000_000 });
        expect(cuerpo.fuentes).toEqual({ manuales: true, argos: true, horus: false, historial: true });
        expect(cuerpo).not.toHaveProperty("clave");
    });

    test("clave guardada: «•••• guardada el …» y «Cambiar»; nunca vuelve; los gestores no la guardan", async ({ page }) => {
        const api = await abrir(page, { ajustes: { proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: "2026-10-20T15:00:00Z" } }, "/administracion/asistente");
        const f = formulario(page);
        await expect(f.getByRole("group", { name: "Clave" })).toContainText("guardada el");
        await expect(f.getByText("Los mensajes salen hacia el proveedor")).toBeVisible();
        await f.getByRole("button", { name: "Cambiar: clave" }).click();
        const campo = f.getByLabel("Clave", { exact: true });
        await expect(campo).toHaveAttribute("autocomplete", "new-password");
        await expect(campo).toHaveAttribute("data-1p-ignore", /.*/);
        await expect(campo).toHaveValue("");
        await campo.fill("sk-nueva-123");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page).toHaveURL(/\/administracion$/);
        expect(api.conversaciones.asistente.guardados[0]).toMatchObject({ clave: "sk-nueva-123", proveedor: "anthropic" });
    });

    test("probar conexión con error, modelos que no cargan (entrada manual) y 409 al guardar", async ({ page }) => {
        const api = await abrir(page, { probar: "error", modelosFalla: true, conflicto: true }, "/administracion/asistente");
        const f = formulario(page);
        await f.getByRole("button", { name: "Probar conexión" }).click();
        await expect(f.locator('[data-prueba="error"]')).toContainText("El servidor no respondió a tiempo");
        await expect(f).toContainText("No se pudo leer la lista de modelos");
        const modelo = f.getByRole("textbox", { name: "Modelo" });
        await expect(modelo).toHaveValue("qwen3:32b");
        await modelo.fill("mistral-small");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Alguien más cambió los ajustes").first()).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
        expect(api.conversaciones.asistente.guardados[0]).toMatchObject({ modelo: "mistral-small" });
    });

    test("validación espejo del servidor y salir con cambios pregunta", async ({ page }) => {
        await abrir(page, {}, "/administracion/asistente");
        const f = formulario(page);
        await f.getByLabel("Dirección").fill("ftp://servidor");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Una dirección http:// o https:// sin usuario ni contraseña")).toBeVisible();
        await page.getByRole("button", { name: "Cancelar" }).click();
        await expect(page.getByRole("alertdialog")).toBeVisible();
    });
});
