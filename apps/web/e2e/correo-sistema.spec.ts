import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, EMPRESAS, LECTOR, simularApi, SUPERVISOR_ZONA } from "./api-falsa";
import { CORREO_GLOBAL, type OpcionesCorreo } from "./api-correo";
import { esperarOpcion } from "./controles";
import type { Locator } from "@playwright/test";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";

/*
 * Correo del sistema (`.claude/integration/contratos/correo-sistema.md`): Administración › Correo.
 * Qué correo se usa hoy, el formulario con atajos de proveedor, la contraseña que nunca se muestra,
 * el correo de prueba sin guardar, encender y apagar con confirmación, el ámbito (general o solo
 * esta empresa) y los errores (409 con «Recargar», 422 junto al campo, lectura con «Reintentar»).
 * En los 4 proyectos y sin desbordes en 390 px ni en 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-correo");
const RUTA = "/administracion/correo";
const UNA = [EMPRESAS[0] ?? { id: 10, nombre: "Cable Norte" }];

async function abrir(page: Page, correo: OpcionesCorreo = {}, usuario = ADMIN, empresas = UNA, ruta = RUTA) {
    const api = await simularApi(page, { correo, empresas });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", usuario.correo);
    await page.goto(ruta);
    return api;
}

/** Elige la seguridad en su selector (un `Select` de React Aria: botón que abre una lista). */
async function elegirSeguridad(control: Locator, valor: string) {
    const disparador = control.and(control.page().locator('[data-slot="select-trigger"]'));
    await disparador.click();
    await control.page().locator('[role="listbox"]').filter({ visible: true }).last().locator(`[role="option"][data-valor="${valor}"]`).click();
    await esperarOpcion(control, valor);
}

const formulario = (page: Page) => page.getByRole("form", { name: "Correo", exact: true });
const origen = (page: Page) => page.getByTestId("origen-correo");
const captura = (page: Page, nombre: string, completa = true) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: completa });

test.describe("Administración › Correo", () => {
    test("sin nada configurado: lo dice en llano, los atajos rellenan el servidor y guardar lo deja encendido", async ({ page }) => {
        const api = await abrir(page);
        const f = formulario(page);
        await expect(origen(page)).toHaveAttribute("data-origen", "ninguno");
        await expect(origen(page)).toContainText("Iris no envía correos todavía");
        // El detalle es la frase del servidor (`origenDetalle`), tal cual.
        await expect(origen(page)).toContainText("No hay correo configurado: los avisos llegan solo por la campana.");
        // Sin ajuste guardado todavía no hay nada que encender o apagar.
        await expect(page.locator('[data-slot="encendido-correo"]')).toHaveCount(0);

        await f.locator('[data-atajo="google"]').click();
        await expect(f.getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.gmail.com");
        await esperarOpcion(f.getByLabel("Puerto"), "587");
        await esperarOpcion(f.getByLabel("Seguridad"), "starttls");
        await expect(f.locator('[data-atajo="google"]')).toHaveAttribute("aria-pressed", "true");
        await expect(f.locator('[data-slot="nota-atajo"]')).toContainText("contraseña de aplicación");
        await f.locator('[data-atajo="microsoft"]').click();
        await expect(f.getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.office365.com");
        // Solo rellenan: se puede editar, y entonces deja de ser el atajo.
        await f.getByRole("textbox", { name: "Servidor" }).fill("smtp.miproveedor.com");
        await expect(f.locator('[data-atajo="otro"]')).toHaveAttribute("aria-pressed", "true");

        // Validación espejo: falta el remitente.
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Escribe un solo correo, como avisos@tuempresa.com")).toBeVisible();
        expect(api.correo.guardados).toHaveLength(0);

        await f.getByRole("textbox", { name: "Usuario" }).fill("avisos@isp.example");
        await f.getByLabel("Contraseña", { exact: true }).fill("clave-de-aplicacion");
        await f.getByRole("textbox", { name: "Nombre" }).fill("Iris · Cable Norte");
        await f.getByRole("textbox", { name: "Correo", exact: true }).fill("no-responder@isp.example");
        await esperarSinDesbordes(page);
        await captura(page, "correo-nuevo");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Correo guardado")).toBeVisible();

        const guardado = api.correo.guardados[0];
        expect(guardado?.ambito).toBe("global");
        expect(guardado?.idempotencia).toBeTruthy();
        expect(guardado?.cuerpo).toEqual({
            version: 0,
            activo: true,
            host: "smtp.miproveedor.com",
            puerto: 587,
            seguridad: "starttls",
            usuario: "avisos@isp.example",
            contrasena: "clave-de-aplicacion",
            remitenteNombre: "Iris · Cable Norte",
            remitenteCorreo: "no-responder@isp.example",
        });
        // Se queda en la pantalla con lo guardado: ya sale por el general y la contraseña no se muestra.
        await expect(origen(page)).toHaveAttribute("data-origen", "global");
        await expect(formulario(page).getByLabel("Contraseña", { exact: true })).toHaveValue("");
        await expect(formulario(page).getByText("Guardada. Escribe otra para cambiarla. Nunca se vuelve a mostrar.")).toBeVisible();
        await expect(page.locator('[data-slot="encendido-correo"] [data-activo="true"]')).toHaveText("Encendido");
    });

    test("con uno guardado: la contraseña nunca viene, no viaja si no se toca y se puede quitar", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL } });
        const f = formulario(page);
        await expect(origen(page)).toContainText("Hoy los correos salen por el correo general");
        await expect(f.getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.proveedor.example");
        const clave = f.getByLabel("Contraseña", { exact: true });
        await expect(clave).toHaveValue("");
        await expect(clave).toHaveAttribute("placeholder", "Guardada");
        await expect(f.getByText("Guardada. Escribe otra para cambiarla. Nunca se vuelve a mostrar.")).toBeVisible();
        // Ninguna respuesta de la API trae la contraseña.
        await expect(page.locator("body")).not.toContainText("secreta-guardada");
        await expect(f.locator('[data-slot="ultima-prueba"]')).toContainText("La última prueba salió bien.");
        await esperarSinDesbordes(page);
        await captura(page, "correo-guardado");

        await f.getByRole("textbox", { name: "Nombre" }).fill("Iris · Avisos");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Correo guardado")).toBeVisible();
        expect(api.correo.guardados[0]?.cuerpo).toMatchObject({ version: 3, activo: true, remitenteNombre: "Iris · Avisos" });
        expect(api.correo.guardados[0]?.cuerpo).not.toHaveProperty("contrasena");
        expect(api.correo.filas.get("global")?.contrasena).toBe("secreta-guardada");

        // Quitar la contraseña dejando el usuario no vale: con usuario hace falta una. No se manda nada.
        const g = formulario(page);
        await g.getByRole("checkbox", { name: /Quitar la contraseña guardada/ }).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(g.getByText("Con usuario hace falta la contraseña: escríbela.")).toBeVisible();
        expect(api.correo.guardados).toHaveLength(1);
        // Sin usuario la contraseña guardada se borra al guardar: se avisa junto al campo, antes.
        await g.getByRole("textbox", { name: "Usuario" }).fill("");
        await expect(g.locator('[data-slot="aviso-borra-contrasena"]')).toHaveText("Sin usuario, la contraseña guardada se borra al guardar.");
        await expect(g.getByRole("checkbox", { name: /Quitar la contraseña guardada/ })).toHaveCount(0);
        await esperarSinDesbordes(page);
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect.poll(() => api.correo.guardados.length).toBe(2);
        expect(api.correo.guardados[1]?.cuerpo).toMatchObject({ version: 4, usuario: "" });
        expect(api.correo.filas.get("global")?.contrasena).toBe("");
        await expect(formulario(page).getByText("Se guarda cifrada y nunca se vuelve a mostrar.")).toBeVisible();
        await expect(formulario(page).locator('[data-slot="aviso-borra-contrasena"]')).toHaveCount(0);
    });

    test("la contraseña guardada no va a otro servidor: al cambiar el servidor hay que escribirla (panel y servidor)", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, rechaza: { codigo: "contrasena_requerida", field: "contrasena", message: "Vuelve a escribir la contraseña" } });
        const f = formulario(page);
        const TEXTO = "Cambiaste el servidor, el puerto o el usuario: escribe la contraseña otra vez.";
        await f.getByRole("textbox", { name: "Servidor" }).fill("smtp.otro.example");
        // Ni guardar ni probar mandan nada: el panel lo pide antes.
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText(TEXTO)).toBeVisible();
        await f.getByRole("button", { name: "Enviar correo de prueba" }).click();
        await expect(page.getByRole("dialog", { name: "Enviar correo de prueba" })).toHaveCount(0);
        expect(api.correo.guardados).toHaveLength(0);
        expect(api.correo.pruebas).toHaveLength(0);
        await esperarSinDesbordes(page);
        // Si aun así el servidor lo rechaza (422 `contrasena_requerida`), el mismo texto llano junto al campo.
        await f.getByRole("textbox", { name: "Servidor" }).fill("smtp.proveedor.example");
        await f.getByRole("textbox", { name: "Nombre" }).fill("Iris · Avisos");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect.poll(() => api.correo.guardados.length).toBe(1);
        await expect(f.getByText(TEXTO)).toBeVisible();
        await expect(f.getByText("Vuelve a escribir la contraseña", { exact: true })).toHaveCount(0);
        await expect(page.getByTestId("error-correo")).toHaveCount(0);
        // Escrita, guarda.
        await f.getByLabel("Contraseña", { exact: true }).fill("otra-clave");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Correo guardado")).toBeVisible();
        expect(api.correo.guardados[1]?.cuerpo).toMatchObject({ contrasena: "otra-clave", remitenteNombre: "Iris · Avisos" });
    });

    test("sin cifrado no hay credenciales, y la falta de la clave de cifrado del servidor se dice en llano", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, rechaza: { codigo: "cifrado_no_configurado", message: "Falta IRIS_CLAVE_CIFRADO: no se puede guardar la contraseña" } });
        const f = formulario(page);
        await elegirSeguridad(f.getByLabel("Seguridad"), "ninguna");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Sin cifrado no se mandan usuario ni contraseña: elige STARTTLS o TLS, o deja el usuario vacío.")).toBeVisible();
        expect(api.correo.guardados).toHaveLength(0);
        await elegirSeguridad(f.getByLabel("Seguridad"), "tls");
        await page.getByRole("button", { name: "Guardar" }).click();
        const aviso = page.getByTestId("error-correo");
        await expect(aviso).toContainText("Al servidor de Iris le falta su clave de cifrado");
        // Nunca el nombre de la variable del servidor.
        await expect(aviso).not.toContainText("IRIS_CLAVE_CIFRADO");
        await esperarSinDesbordes(page);
    });

    test("correo de prueba: usa lo del formulario sin guardar, propone mi correo y dice el resultado", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL } });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Servidor" }).fill("smtp.otro.example");
        await f.getByLabel("Contraseña", { exact: true }).fill("clave-del-otro");
        await f.getByRole("button", { name: "Enviar correo de prueba" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enviar correo de prueba" });
        await expect(dialogo).toContainText("Sale por smtp.otro.example con lo que hay en el formulario");
        const para = dialogo.getByRole("textbox", { name: "¿A qué correo lo mandamos?" });
        await expect(para).toHaveValue(ADMIN.correo);
        await para.fill("no es un correo");
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        await expect(dialogo.getByText("Escribe un solo correo de destino")).toBeVisible();
        expect(api.correo.pruebas).toHaveLength(0);
        await para.fill("ana@isp.example");
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        const resultado = dialogo.locator('[data-slot="resultado-prueba"]');
        await expect(resultado).toHaveAttribute("data-estado", "ok");
        await expect(resultado).toContainText("El correo de prueba salió");
        await expect(resultado).toContainText("Revisa la bandeja de ana@isp.example.");
        await expect.poll(() => buscarDesbordes(page)).toEqual([]);
        await captura(page, "correo-prueba-ok", false);
        // Lo que viajó: el servidor nuevo (sin guardar) con su contraseña, sin `version` ni `activo`.
        expect(api.correo.pruebas[0]?.cuerpo).toEqual({ host: "smtp.otro.example", puerto: 587, seguridad: "starttls", usuario: "avisos@isp.example", contrasena: "clave-del-otro", remitenteNombre: "Iris", remitenteCorreo: "no-responder@isp.example", para: "ana@isp.example" });
        expect(api.correo.guardados).toHaveLength(0);
        await dialogo.getByRole("button", { name: "Cerrar" }).last().click();
        await expect(dialogo).toHaveCount(0);
        // El formulario sigue con el cambio sin guardar.
        await expect(f.getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.otro.example");
    });

    test("correo de prueba que falla: la causa en llano, con icono y color; con el formulario inválido no se abre", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, prueba: "credenciales" });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Servidor" }).fill("https://smtp.mal");
        await f.getByRole("button", { name: "Enviar correo de prueba" }).click();
        await expect(f.getByText("Solo el nombre del servidor: sin «https://», sin ruta y sin puerto")).toBeVisible();
        await expect(page.getByRole("dialog", { name: "Enviar correo de prueba" })).toHaveCount(0);
        await f.getByRole("textbox", { name: "Servidor" }).fill("smtp.proveedor.example");
        await f.getByRole("button", { name: "Enviar correo de prueba" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enviar correo de prueba" });
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        const resultado = dialogo.locator('[data-slot="resultado-prueba"]');
        await expect(resultado).toHaveAttribute("data-estado", "error");
        await expect(resultado).toHaveAttribute("data-causa", "credenciales");
        await expect(resultado).toContainText("No se pudo enviar");
        await expect(resultado).toContainText("El usuario o la contraseña no son correctos.");
        await expect(dialogo.getByRole("button", { name: "Enviar otra vez" })).toBeVisible();
        await expect.poll(() => buscarDesbordes(page)).toEqual([]);
        await captura(page, "correo-prueba-error", false);
        expect(api.correo.pruebas).toHaveLength(1);
    });

    test("correo de prueba: una causa que el panel no conoce, y 429 con cuánto esperar", async ({ page }) => {
        await abrir(page, { guardados: { global: CORREO_GLOBAL }, prueba: "causa_nueva" });
        await formulario(page).getByRole("button", { name: "Enviar correo de prueba" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enviar correo de prueba" });
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        await expect(dialogo.locator('[data-slot="resultado-prueba"]')).toContainText("El servidor rechazó el envío.");

        await abrir(page, { guardados: { global: CORREO_GLOBAL }, prueba: "429" });
        await formulario(page).getByRole("button", { name: "Enviar correo de prueba" }).click();
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        await expect(page.getByTestId("prueba-espera")).toContainText("Inténtalo más tarde");
        await expect(page.getByTestId("prueba-espera")).toContainText("Ya se hicieron muchas pruebas seguidas. Inténtalo en 12 min.");
    });

    test("apagar y encender piden confirmación, dicen qué pasa después y avisan", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, entorno: true });
        const estado = page.locator('[data-slot="encendido-correo"]');
        await expect(estado.locator("[data-activo]")).toHaveText("Encendido");
        await estado.getByRole("button", { name: "Apagar" }).click();
        const dialogo = page.getByRole("alertdialog").filter({ hasText: "¿Apagar este correo?" });
        await expect(dialogo).toContainText("Los avisos saldrán por el correo del servidor si hay uno; si no, solo llegarán por la campana.");
        await expect.poll(() => buscarDesbordes(page)).toEqual([]);
        await dialogo.getByRole("button", { name: "Apagar" }).click();
        await expect(page.getByText("Correo apagado")).toBeVisible();
        expect(api.correo.cambios[0]).toMatchObject({ ambito: "global", accion: "desactivar", version: 3 });
        expect(api.correo.cambios[0]?.idempotencia).toBeTruthy();
        // Apagado, se usa el del servidor: lo dice la línea de arriba.
        await expect(estado.locator("[data-activo]")).toHaveText("Apagado");
        await expect(origen(page)).toHaveAttribute("data-origen", "entorno");
        await expect(origen(page)).toContainText("Hoy los correos salen por el correo del servidor");
        await esperarSinDesbordes(page);
        await captura(page, "correo-apagado");

        await estado.getByRole("button", { name: "Encender" }).click();
        await page.getByRole("alertdialog").filter({ hasText: "¿Encender este correo?" }).getByRole("button", { name: "Encender" }).click();
        await expect(page.getByText("Correo encendido")).toBeVisible();
        expect(api.correo.cambios[1]).toMatchObject({ accion: "activar", version: 4 });
        await expect(origen(page)).toHaveAttribute("data-origen", "global");
    });

    test("409 al guardar: lo dice con «Recargar» y trae lo que quedó guardado", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, conflicto: true });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Nombre" }).fill("Otro nombre");
        await page.getByRole("button", { name: "Guardar" }).click();
        const aviso = page.getByTestId("error-correo");
        await expect(aviso).toContainText("Alguien más cambió el correo");
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        // Mientras tanto, otra persona lo dejó distinto.
        const fila = api.correo.filas.get("global");
        if (!fila) throw new Error("falta el ajuste global");
        api.correo.filas.set("global", { ...fila, remitenteNombre: "Avisos de Iris", version: 4 });
        await aviso.getByRole("button", { name: "Recargar" }).click();
        await expect(formulario(page).getByRole("textbox", { name: "Nombre" })).toHaveValue("Avisos de Iris");
        await expect(page.getByTestId("error-correo")).toHaveCount(0);
    });

    test("422 del servidor junto al campo; y si no se pudo leer, «Reintentar» y ningún dato inventado", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, rechaza: { codigo: "destino_no_permitido", field: "host", message: "Destino no permitido" } });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Servidor" }).fill("10.0.0.5");
        await f.getByLabel("Contraseña", { exact: true }).fill("clave");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Ese servidor no se puede usar desde Iris (es de una red interna o no permitida).")).toBeVisible();
        await expect(page.getByTestId("error-correo")).toHaveCount(0);
        expect(api.correo.guardados).toHaveLength(1);
        await esperarSinDesbordes(page);

        await abrir(page, { guardados: { global: CORREO_GLOBAL }, fallaLeer: true });
        await expect(page.getByRole("button", { name: "Reintentar" })).toBeVisible();
        await expect(page.getByRole("form")).toHaveCount(0);
        await expect(page.getByTestId("origen-correo")).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(formulario(page).getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.proveedor.example");
    });
});

test.describe("Administración › Correo: ámbito y permisos", () => {
    test("contrato 1.1.0: «Sin cifrado» solo se ofrece en el general; en una empresa, el 422 va junto al campo", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL, "10": { ...CORREO_GLOBAL, version: 1 } }, rechaza: { codigo: "sin_cifrado_solo_global", field: "seguridad", message: "Sin cifrado solo en el ajuste global" } });
        const opciones = async () => {
            const control = formulario(page).getByLabel("Seguridad");
            const disparador = control.and(page.locator('[data-slot="select-trigger"]'));
            await disparador.click();
            const lista = page.locator('[role="listbox"]').filter({ visible: true }).last();
            await expect(lista).toBeVisible();
            const valores = await lista.locator('[role="option"]').evaluateAll((os) => os.map((o) => o.getAttribute("data-valor")));
            await page.keyboard.press("Escape");
            await expect(lista).toBeHidden();
            return valores;
        };
        expect(await opciones()).toEqual(["starttls", "tls", "ninguna"]);
        await page.locator('[data-slot="ambito-correo"]').getByRole("radio", { name: "Solo Cable Norte" }).click();
        await expect(page).toHaveURL(/\?ambito=empresa$/);
        await expect(formulario(page).getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.proveedor.example");
        expect(await opciones()).toEqual(["starttls", "tls"]);
        // Si aun así el servidor lo rechaza, el texto llano junto a «Seguridad».
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Nombre" }).fill("Cable Norte");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("«Sin cifrado» solo se puede usar en el correo general: elige STARTTLS o TLS.")).toBeVisible();
        await expect(page.getByTestId("error-correo")).toHaveCount(0);
        expect(api.correo.guardados).toHaveLength(1);
        await esperarSinDesbordes(page);
    });

    test("429 `demasiados_rechazos` al guardar y al probar: lo dice en llano con cuánto esperar", async ({ page }) => {
        await abrir(page, { guardados: { global: CORREO_GLOBAL }, demasiadosRechazos: true, prueba: "demasiados_rechazos" });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Nombre" }).fill("Otro nombre");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByTestId("error-correo")).toContainText("Demasiados intentos con servidores no permitidos. Inténtalo en 10 min.");
        await esperarSinDesbordes(page);
        await f.getByRole("button", { name: "Enviar correo de prueba" }).click();
        await page.getByRole("dialog", { name: "Enviar correo de prueba" }).getByRole("button", { name: "Enviar prueba" }).click();
        await expect(page.getByTestId("prueba-espera")).toContainText("Demasiados intentos con servidores no permitidos. Inténtalo en 10 min.");
    });

    test("si Iris no contesta la prueba, a los 45 s se corta y lo dice", async ({ page }) => {
        // Reloj de Playwright: los 45 s se adelantan, no se esperan.
        await page.clock.install();
        const api = await simularApi(page, { correo: { guardados: { global: CORREO_GLOBAL }, prueba: "cuelga" }, empresas: UNA });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto(RUTA);
        await formulario(page).getByRole("button", { name: "Enviar correo de prueba" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enviar correo de prueba" });
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        await expect(dialogo.locator('[data-slot="probando"]')).toBeVisible();
        await expect.poll(() => api.correo.pruebas.length).toBe(1);
        // A los 30 s sigue probando (no se corta antes).
        await page.clock.fastForward(30_000);
        await expect(dialogo.locator('[data-slot="probando"]')).toBeVisible();
        await page.clock.fastForward(16_000);
        await expect(page.getByTestId("prueba-error")).toContainText("No hubo respuesta. Inténtalo otra vez.");
        await expect(dialogo.getByRole("button", { name: "Enviar otra vez" })).toBeEnabled();
    });

    test("tras probar, «la última prueba» de la pantalla se actualiza sin perder lo escrito", async ({ page }) => {
        await abrir(page, { guardados: { global: CORREO_GLOBAL }, prueba: "credenciales" });
        const f = formulario(page);
        await expect(f.locator('[data-slot="ultima-prueba"]')).toHaveAttribute("data-estado", "ok");
        await f.getByRole("textbox", { name: "Nombre" }).fill("Sin guardar");
        await f.getByRole("button", { name: "Enviar correo de prueba" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enviar correo de prueba" });
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        await expect(dialogo.locator('[data-slot="resultado-prueba"]')).toHaveAttribute("data-estado", "error");
        await dialogo.getByRole("button", { name: "Cerrar" }).last().click();
        await expect(f.locator('[data-slot="ultima-prueba"]')).toHaveAttribute("data-estado", "error");
        await expect(f.locator('[data-slot="ultima-prueba"]')).toContainText("La última prueba falló. El usuario o la contraseña no son correctos.");
        await expect(f.getByRole("textbox", { name: "Nombre" })).toHaveValue("Sin guardar");
    });

    test("instalación sin empresas: quien tiene el permiso configura el general; sin empresa asignada, «Sin permiso» claro", async ({ page }) => {
        // Sin conector con Argos no hay empresas: el único correo es el general, también para quien no opera «en todas».
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL } }, SUPERVISOR_ZONA, []);
        await expect(formulario(page).getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.proveedor.example");
        await expect(page.getByTestId("correo-sin-empresa")).toHaveCount(0);
        await expect(page.locator('[data-slot="ambito-correo"]')).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/correo-sistema/global")).toBe(true);
        // Hay empresas, pero esta persona no tiene ninguna: no hay correo que pueda ver, y se le dice.
        const otra = await simularApi(page, { correo: {}, sinEmpresa: true });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", SUPERVISOR_ZONA.correo);
        await page.goto(RUTA);
        await expect(page.getByTestId("correo-sin-permiso")).toContainText("Sin permiso para ver este correo");
        await expect(page.getByRole("form")).toHaveCount(0);
        expect(otra.peticiones.some((p) => p.ruta.startsWith("/api/v1/correo-sistema/"))).toBe(false);
        await esperarSinDesbordes(page);
    });

    test("una prueba lenta dice «Probando…» y espera el resultado sin cortar", async ({ page }) => {
        await abrir(page, { guardados: { global: CORREO_GLOBAL }, prueba: "no_conecta", demoraPrueba: 2500 });
        await formulario(page).getByRole("button", { name: "Enviar correo de prueba" }).click();
        const dialogo = page.getByRole("dialog", { name: "Enviar correo de prueba" });
        await dialogo.getByRole("button", { name: "Enviar prueba" }).click();
        await expect(dialogo.locator('[data-slot="probando"]')).toContainText("Probando…");
        await expect(dialogo.locator('button[type="submit"]')).toBeDisabled();
        const resultado = dialogo.locator('[data-slot="resultado-prueba"]');
        await expect(resultado).toHaveAttribute("data-causa", "no_conecta", { timeout: 10_000 });
        await expect(resultado).toContainText("No se pudo conectar: revisa el servidor y el puerto.");
        await expect(dialogo.locator('[data-slot="probando"]')).toHaveCount(0);
    });

    test("sin límite de empresa: elige entre «General» y «Solo esta empresa», cada uno con su ajuste", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL } });
        const ambito = page.locator('[data-slot="ambito-correo"]');
        await expect(ambito.getByRole("radio", { name: "General" })).toBeChecked();
        await expect(formulario(page).getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.proveedor.example");
        await ambito.getByRole("radio", { name: "Solo Cable Norte" }).click();
        await expect(page).toHaveURL(/\?ambito=empresa$/);
        // La empresa no tiene uno propio: el formulario va vacío y hoy usa el general.
        await expect(formulario(page).getByRole("textbox", { name: "Servidor" })).toHaveValue("");
        await expect(origen(page)).toHaveAttribute("data-origen", "global");
        await expect(origen(page)).toContainText("Hoy los correos salen por el correo general");
        await expect(origen(page)).toContainText("Los correos de esta instalación salen por el ajuste global.");
        await esperarSinDesbordes(page);
        await captura(page, "correo-empresa");

        const f = formulario(page);
        await f.locator('[data-atajo="microsoft"]').click();
        // Con cambios sin guardar no se pasa al otro ámbito.
        await expect(ambito.getByRole("radio", { name: "General" })).toBeDisabled();
        await expect(ambito).toContainText("Guarda o cancela los cambios para pasar al otro.");
        await f.getByRole("textbox", { name: "Correo", exact: true }).fill("avisos@cablenorte.example");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Correo guardado")).toBeVisible();
        expect(api.correo.guardados[0]).toMatchObject({ ambito: "10", cuerpo: { version: 0, activo: true, host: "smtp.office365.com" } });
        await expect(origen(page)).toHaveAttribute("data-origen", "empresa");
        // El general sigue como estaba.
        expect(api.correo.filas.get("global")?.host).toBe("smtp.proveedor.example");
    });

    test("con límite de empresa: solo ve el de su empresa, sin selector, aunque la URL diga otra cosa", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL } }, SUPERVISOR_ZONA, UNA, `${RUTA}?ambito=global`);
        await expect(formulario(page).getByRole("textbox", { name: "Servidor" })).toHaveValue("");
        await expect(page.locator('[data-slot="ambito-correo"]')).toHaveCount(0);
        await expect(origen(page)).toContainText("Hoy los correos salen por el correo general");
        const leidas = api.peticiones.filter((p) => p.ruta.startsWith("/api/v1/correo-sistema/")).map((p) => p.ruta);
        expect(leidas.length).toBeGreaterThan(0);
        expect(leidas.every((r) => r === "/api/v1/correo-sistema/10")).toBe(true);
    });

    test("solo con permiso de ver: lo ve, pero no puede cambiarlo ni probarlo", async ({ page }) => {
        const api = await abrir(page, { guardados: { "10": CORREO_GLOBAL } }, CONSULTA);
        const f = formulario(page);
        await expect(f.getByRole("textbox", { name: "Servidor" })).toHaveValue("smtp.proveedor.example");
        await expect(f.getByRole("textbox", { name: "Servidor" })).toBeDisabled();
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await expect(f.getByRole("button", { name: "Enviar correo de prueba" })).toHaveCount(0);
        await expect(page.locator('[data-slot="encendido-correo"]').getByRole("button")).toHaveCount(0);
        await expect(page.locator('[data-slot="encendido-correo"] [data-activo]')).toHaveText("Encendido");
        await esperarSinDesbordes(page);
        expect(api.correo.guardados).toHaveLength(0);
    });

    test("sin ninguno de los permisos no se ve la pantalla ni la tarjeta en Administración", async ({ page }) => {
        const api = await abrir(page, {}, LECTOR);
        await expect(page.getByRole("form")).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta.startsWith("/api/v1/correo-sistema/"))).toBe(false);
        // Quien sí puede, lo encuentra en el índice.
        await abrir(page, {}, ADMIN, UNA, "/administracion");
        await page.getByRole("list", { name: "Secciones de administración" }).getByRole("link", { name: /^Correo/ }).click();
        await expect(page).toHaveURL(/\/administracion\/correo$/);
        await expect(formulario(page)).toBeVisible();
    });
});
