import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, conSesion, simularApi } from "./api-falsa";
import { DETALLE_PROVEEDOR, TEXTO_OTRA, TEXTO_SUGERIDO, type OpcionesAsistente } from "./api-asistente";
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

    test("429 al pedir «Otra»: cuánto esperar, «Reintentar» deshabilitado y después repite el mismo pedido", async ({ page }, info) => {
        const api = await abrir(page, { limite429: "sugerencia" });
        const t = tarjeta(page);
        await t.getByRole("button", { name: "Otra" }).click();
        await expect(t.getByRole("alert")).toContainText("Demasiados pedidos. Vuelve a intentar en 5 s.");
        await expect(t.getByRole("button", { name: "Reintentar" })).toBeDisabled();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `bandeja-429-${info.project.name}.png`) });
        await page.clock.setFixedTime(new Date(AHORA_CONV + 6_000));
        await expect(t.getByRole("button", { name: "Reintentar" })).toBeEnabled();
        await t.getByRole("button", { name: "Reintentar" }).click();
        await expect(t).toContainText(TEXTO_OTRA);
        expect(rutas(api, "/sugerencias").map((e) => e.cuerpo)).toEqual([{ otra: true }, { otra: true }]);
    });

    test("429 al resumir: espera y «Reintentar» deshabilitado hasta entonces", async ({ page }) => {
        await abrir(page, { inicial: null, limite429: "resumen" });
        let ficha = page.getByRole("complementary", { name: "Ficha del cliente" });
        if (esTelefono(page)) {
            await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
            ficha = page.getByRole("dialog", { name: "Ficha del cliente" });
        }
        const resumen = ficha.getByRole("region", { name: "Resumen del asistente" });
        await resumen.getByRole("button", { name: "Resumir" }).click();
        await expect(resumen.getByRole("alert")).toContainText("Demasiados pedidos. Vuelve a intentar en 5 s.");
        await expect(resumen.getByRole("button", { name: "Reintentar" })).toBeDisabled();
        await page.clock.setFixedTime(new Date(AHORA_CONV + 6_000));
        await resumen.getByRole("button", { name: "Reintentar" }).click();
        await expect(resumen).toContainText("Cliente sin internet desde la mañana");
    });

    test("la conversación pasó a otra empresa: el 404 de la sugerencia es «sin sugerencia», no un error", async ({ page }) => {
        const api = await abrir(page);
        await expect(tarjeta(page)).toContainText(TEXTO_SUGERIDO);
        api.conversaciones.asistente.prestar();
        await tarjeta(page).getByRole("button", { name: "Usar" }).click();
        await expect(tarjeta(page)).toHaveCount(0);
        await expect(compositor(page)).toHaveValue("");
        await expect(page.getByRole("alert").filter({ hasText: /No se pudo usar/ })).toHaveCount(0);

        await page.unrouteAll({ behavior: "ignoreErrors" });
        await abrir(page, { prestada: true });
        await expect(page.getByRole("log", { name: "Mensajes" })).toContainText("Ya lo reinicié");
        await expect(tarjeta(page)).toHaveCount(0);
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
        // Nada con cada tecla: la lista se pide al salir del campo, por POST.
        expect(api.conversaciones.asistente.consultasModelos).toHaveLength(0);
        await f.getByLabel("Dirección").blur();
        await expect.poll(() => api.conversaciones.asistente.consultasModelos).toEqual([{ proveedor: "openai_compatible", url: "https://openrouter.ai/api/v1" }]);
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
        const campo = f.locator(`input[name="clave"]`);
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
        await expect(f.locator('[data-prueba="error"]')).toContainText(DETALLE_PROVEEDOR);
        // La lista no cae en silencio a la entrada manual: dice por qué.
        await expect(f.locator('[data-slot="error-modelos"]')).toContainText(`No se pudo leer la lista de modelos: ${DETALLE_PROVEEDOR}`);
        await expect(f.getByRole("button", { name: "Volver a cargar los modelos" })).toBeVisible();
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
        await expect(f.getByText("Una dirección http:// o https:// sin usuario, contraseña ni parámetros")).toBeVisible();
        await page.getByRole("button", { name: "Cancelar" }).click();
        await expect(page.getByRole("alertdialog")).toBeVisible();
    });

    const conClave = { proveedor: "openai_compatible", url: "https://openrouter.ai/api/v1", modelo: "llama3.1:8b", claveGuardadaEn: "2026-10-20T15:00:00Z" } as const;

    test("otro proveedor u otra dirección con clave guardada: abre «Cambiar» solo y la pide de nuevo", async ({ page }, info) => {
        const api = await abrir(page, { ajustes: conClave }, "/administracion/asistente");
        const f = formulario(page);
        await expect(f.getByRole("group", { name: "Clave" })).toContainText("guardada el");
        // La misma dirección normalizada (sin /v1) no cuenta como cambio.
        await f.getByLabel("Dirección").fill("https://openrouter.ai/api");
        await expect(f.locator('[data-slot="clave-requerida"]')).toHaveCount(0);
        await f.getByLabel("Dirección").fill("https://api.groq.com/openai/v1");
        await expect(f.locator('[data-slot="clave-requerida"]')).toHaveText("Con otro proveedor u otra dirección hay que escribir la clave de nuevo.");
        const campo = f.locator(`input[name="clave"]`);
        await expect(campo).toBeVisible();
        await esperarSinDesbordes(page);
        await page.screenshot({ path: path.join(carpeta, `ajustes-clave-requerida-${info.project.name}.png`) });
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Con otro proveedor u otra dirección hay que escribir la clave de nuevo").last()).toBeVisible();
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
        await campo.fill("gsk-nueva");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page).toHaveURL(/\/administracion$/);
        expect(api.conversaciones.asistente.guardados[0]).toMatchObject({ url: "https://api.groq.com/openai/v1", clave: "gsk-nueva" });
    });

    test("422 `clave_requerida` del servidor con la clave oculta: abre el campo y marca el error", async ({ page }) => {
        const api = await abrir(page, { ajustes: conClave, claveRequerida: true }, "/administracion/asistente");
        const f = formulario(page);
        await expect(f.getByRole("group", { name: "Clave" })).toContainText("guardada el");
        await expect(f.locator(`input[name="clave"]`)).toHaveCount(0);
        await f.locator(`[data-opcion="boton"]`).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        const campo = f.locator(`input[name="clave"]`);
        await expect(campo).toBeVisible();
        await expect(campo).toHaveAttribute("aria-invalid", "true");
        await expect(f.getByText("Con otro proveedor u otra dirección hay que escribir la clave de nuevo")).toBeVisible();
        expect(api.conversaciones.asistente.guardados).toHaveLength(1);
    });

    test("Ollama: «Quitar clave» manda la clave vacía", async ({ page }) => {
        const api = await abrir(page, { ajustes: { ...conClave, proveedor: "ollama", url: "http://10.0.0.20:11434" } }, "/administracion/asistente");
        const f = formulario(page);
        await f.getByLabel("Dirección").fill("http://10.0.0.21:11434");
        await expect(f.locator('[data-slot="clave-requerida"]')).toHaveText("Con otro proveedor u otra dirección hay que escribir la clave de nuevo o quitarla.");
        await f.getByRole("button", { name: "Quitar clave" }).click();
        await expect(f.locator('[data-slot="clave-quitada"]')).toContainText("La clave guardada se quitará al guardar.");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page).toHaveURL(/\/administracion$/);
        expect(api.conversaciones.asistente.guardados[0]).toMatchObject({ url: "http://10.0.0.21:11434", clave: "" });
    });

    test("422 `url_no_permitida`: el detalle va en el campo Dirección; «Cargar modelos» por POST", async ({ page }) => {
        const api = await abrir(page, { ajustes: { url: "", modelo: "", activo: false } }, "/administracion/asistente");
        const f = formulario(page);
        await expect(f.locator('[data-lista-modelos="inicial"]')).toBeVisible();
        await f.getByLabel("Dirección").fill("http://10.0.0.30:11434");
        await f.getByRole("button", { name: "Cargar modelos" }).click();
        await expect(f.locator('[data-lista-modelos="listo"]')).toBeVisible();
        expect(api.conversaciones.asistente.consultasModelos[0]).toEqual({ proveedor: "ollama", url: "http://10.0.0.30:11434" });
        await f.getByLabel("Dirección").fill("http://localhost:11434");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("No se permiten direcciones de este equipo ni de metadatos")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
    });
});
