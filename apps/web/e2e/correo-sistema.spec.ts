import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, EMPRESAS, LECTOR, simularApi, SUPERVISOR_ZONA } from "./api-falsa";
import { CORREO_GLOBAL, type OpcionesCorreo } from "./api-correo";
import { esperarOpcion } from "./controles";
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

const formulario = (page: Page) => page.getByRole("form", { name: "Correo", exact: true });
const origen = (page: Page) => page.getByTestId("origen-correo");
const captura = (page: Page, nombre: string, completa = true) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: completa });

test.describe("Administración › Correo", () => {
    test("sin nada configurado: lo dice en llano, los atajos rellenan el servidor y guardar lo deja encendido", async ({ page }) => {
        const api = await abrir(page);
        const f = formulario(page);
        await expect(origen(page)).toHaveAttribute("data-origen", "ninguno");
        await expect(origen(page)).toContainText("Iris no envía correos todavía");
        await expect(origen(page)).toContainText("Los avisos llegan por la campana");
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

        // «Quitar la contraseña guardada» manda la contraseña vacía.
        await formulario(page).getByRole("checkbox", { name: /Quitar la contraseña guardada/ }).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect.poll(() => api.correo.guardados.length).toBe(2);
        expect(api.correo.guardados[1]?.cuerpo).toMatchObject({ version: 4, contrasena: "" });
        await expect(formulario(page).getByRole("checkbox", { name: /Quitar la contraseña guardada/ })).toHaveCount(0);
    });

    test("correo de prueba: usa lo del formulario sin guardar, propone mi correo y dice el resultado", async ({ page }) => {
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL } });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Servidor" }).fill("smtp.otro.example");
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
        // Lo que viajó: el servidor nuevo (sin guardar), sin contraseña (usa la guardada), sin `version`.
        expect(api.correo.pruebas[0]?.cuerpo).toEqual({ host: "smtp.otro.example", puerto: 587, seguridad: "starttls", usuario: "avisos@isp.example", remitenteNombre: "Iris", remitenteCorreo: "no-responder@isp.example", para: "ana@isp.example" });
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
        const api = await abrir(page, { guardados: { global: CORREO_GLOBAL }, rechaza: { field: "host", message: "Ese servidor no se puede usar desde Iris" } });
        const f = formulario(page);
        await f.getByRole("textbox", { name: "Servidor" }).fill("10.0.0.5");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Ese servidor no se puede usar desde Iris")).toBeVisible();
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
        await expect(origen(page)).toContainText("Esta empresa no tiene uno propio encendido");
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
