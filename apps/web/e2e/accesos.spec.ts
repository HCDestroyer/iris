import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect, test, type Page } from "@playwright/test";
import { simularAccesos } from "./api-accesos";
import { ADMIN, CONSULTA, CONTRASENA, conSesion, simularApi, SOPORTE } from "./api-falsa";
import { irA, pestana } from "./ayudas";

// Pedido 10 del PM: Administración → Accesos, Actividad de la app y Roles; y en el usuario, sus
// roles con alcance, sesiones activas y accesos recientes. En escritorio y teléfono, claro y oscuro.

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function preparar(page: Page, context: Parameters<typeof conSesion>[0], baseURL: string | undefined, correo = ADMIN.correo) {
    await simularApi(page);
    const accesos = await simularAccesos(page);
    await conSesion(context, baseURL ?? "", correo);
    return accesos;
}

async function sinDesborde(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("accesos, actividad de la app y roles", () => {
    test("Administración ofrece las tres pantallas según el permiso", async ({ page, context, baseURL }, info) => {
        await preparar(page, context, baseURL);
        await page.goto("/");
        await irA(page, /Administración/);
        const secciones = page.getByRole("list", { name: "Secciones de administración" });
        await expect(secciones.getByRole("link", { name: /Accesos/ })).toBeVisible();
        await expect(secciones.getByRole("link", { name: /Actividad de la app/ })).toBeVisible();
        await expect(secciones.getByRole("link", { name: /Roles y permisos/ })).toBeVisible();
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `administracion-${info.project.name}.png`), fullPage: true });
    });

    test("sin accesos.ver ni campo.ver_actividad: «Sin permiso»", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL, CONSULTA.correo);
        for (const ruta of ["/administracion/accesos", "/administracion/actividad-app", "/administracion/roles/nuevo"]) {
            await page.goto(ruta);
            await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        }
    });

    test("historial: filtros, rango inválido, intento sin usuario y «Cargar más»", async ({ page, context, baseURL }, info) => {
        const api = await preparar(page, context, baseURL);
        await page.goto("/administracion/accesos");
        await expect(page.getByRole("heading", { name: "Accesos" })).toBeVisible();
        const lista = page.getByRole("list", { name: "Accesos" });
        await expect(lista.locator("[data-acceso]")).toHaveCount(50);
        // El intento con un correo inexistente aparece sin usuario (el correo no se guarda).
        await expect(page.locator('[data-acceso="57"]')).toContainText("Correo que no existe");
        await expect(page.locator('[data-acceso="59"]')).toContainText("App de campo (Android)");
        await expect(page.locator('[data-acceso="59"]')).toContainText("App de campo 1.4.0 · Android");
        await expect(page.locator('[data-acceso="55"]')).toContainText("Código MFA fallido");
        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(lista.locator("[data-acceso]")).toHaveCount(57);
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `accesos-${info.project.name}.png`), fullPage: false });

        // Rango al revés: no se aplica y se dice por qué.
        const filtros = page.getByRole("form", { name: "Filtros del historial" });
        await filtros.getByLabel("Desde").fill("2026-10-30");
        await filtros.getByLabel("Hasta").fill("2026-10-29");
        await filtros.getByRole("button", { name: "Aplicar filtros" }).click();
        await expect(filtros.getByText("«Hasta» debe ser igual o posterior a «Desde»")).toBeVisible();

        await filtros.getByRole("button", { name: "Limpiar" }).click();
        await filtros.getByLabel("Usuario").selectOption({ label: SOPORTE.nombre });
        await filtros.getByLabel("Resultado").selectOption({ label: "Código MFA fallido" });
        await filtros.getByRole("button", { name: "Aplicar filtros" }).click();
        await expect(lista.locator("[data-acceso]")).toHaveCount(1);
        await expect(page.locator('[data-acceso="55"]')).toContainText("Código MFA fallido");
        const ultima = api.peticiones.filter((p) => p.ruta === "/api/v1/accesos").at(-1);
        expect(ultima?.query.get("usuarioId")).toBe(String(SOPORTE.id));
        expect(ultima?.query.get("resultado")).toBe("mfa_fallido");
    });

    test("actividad de la app: indicador por técnico y filtro por estado", async ({ page, context, baseURL }, info) => {
        await preparar(page, context, baseURL);
        await page.goto("/administracion/actividad-app");
        await expect(page.getByRole("heading", { name: "Actividad de la app" })).toBeVisible();
        const jorge = page.locator('[data-tecnico="31"]');
        const rosa = page.locator('[data-tecnico="32"]');
        const luis = page.locator('[data-tecnico="33"]');
        await expect(jorge).toContainText("Al día");
        await expect(jorge).toContainText("1.4.0 · Android");
        await expect(jorge).toContainText("14.63491, -90.50688");
        await expect(rosa).toContainText("Atrasado · 3 sin enviar");
        await expect(rosa).toContainText("0.9.2 · PWA");
        await expect(luis).toContainText("Sin conexión hace 2 h");
        await expect(luis).toContainText("Android · versión sin informar");
        await expect(luis).toContainText("Sin ubicación");
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `actividad-app-${info.project.name}.png`), fullPage: true });

        await page.getByRole("radio", { name: /Atrasado \(1\)/ }).click();
        await expect(page.locator("[data-tecnico]")).toHaveCount(1);
        await expect(rosa).toBeVisible();
    });

    test("nuevo rol: guardar deshabilitado sin permisos y re-autenticación por un permiso sensible", async ({ page, context, baseURL }, info) => {
        const api = await preparar(page, context, baseURL);
        await page.goto("/administracion/roles");
        await expect(page.locator('[data-rol="admin"]')).toContainText("Rol base");
        await expect(page.locator('[data-rol="temporada"]')).toContainText("Desactivado");
        await page.getByRole("link", { name: "Nuevo rol" }).click();
        await expect(page.getByRole("heading", { name: "Nuevo rol" })).toBeVisible();

        const guardar = page.getByRole("button", { name: "Crear rol" });
        await expect(guardar).toBeDisabled();
        await expect(page.getByText("Elige al menos un permiso para guardar el rol.")).toBeVisible();
        await page.getByLabel("Código").fill("Jefe Buzones");
        await page.getByLabel("Nombre").fill("Jefe de buzones");
        // Agrupados por módulo con descripción legible.
        const soporte = page.locator('[data-modulo="soporte"]');
        await expect(soporte).toContainText("Soporte (tickets)");
        await soporte.getByLabel("Ver tickets, su conversación, historial, categorías y SLA").check();
        await soporte.getByLabel(/Registrar buzones de correo/).check();
        await expect(page.getByText("2 permisos elegidos")).toBeVisible();
        await expect(page.getByText("Incluye permisos sensibles")).toBeVisible();
        await expect(guardar).toBeEnabled();
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `rol-nuevo-${info.project.name}.png`), fullPage: false });

        await guardar.click();
        await expect(page.getByText(/Solo minúsculas, números y guion bajo/)).toBeVisible();
        await page.getByLabel("Código").fill("jefe_buzones");
        await guardar.click();
        // La API pide confirmar la contraseña; el panel la pide y repite la acción.
        const dialogo = page.getByRole("dialog", { name: "Confirma tu contraseña" });
        await expect(dialogo).toBeVisible();
        await dialogo.getByLabel("Contraseña").fill(CONTRASENA);
        await dialogo.getByRole("button", { name: "Confirmar y seguir" }).click();
        await expect(page).toHaveURL(/\/administracion\/roles\/\d+$/);
        await expect(page.getByRole("heading", { name: "Jefe de buzones" })).toBeVisible();
        expect(api.reautenticaciones()).toBe(1);
        const altas = api.peticiones.filter((p) => p.metodo === "POST" && p.ruta === "/api/v1/roles");
        expect(altas.length).toBe(2);
        expect(altas[1]?.cuerpo).toEqual({ codigo: "jefe_buzones", nombre: "Jefe de buzones", permisos: ["buzones.crear", "tickets.ver"] } satisfies Schema<"EntradaCrearRol">);
        expect(altas[1]?.idempotencia).toMatch(/:reaut$/);
    });

    test("rol base de solo lectura; rol propio: editar, desactivar y activar", async ({ page, context, baseURL }) => {
        const api = await preparar(page, context, baseURL);
        await page.goto("/administracion/roles/7");
        await expect(page.getByText("Lo mantiene Iris")).toBeVisible();
        await expect(page.getByRole("button", { name: "Guardar cambios" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: /Desactivar rol/ })).toHaveCount(0);
        await expect(page.getByRole("region", { name: "Soporte (tickets)" })).toContainText("tickets.ver");

        await page.goto("/administracion/roles/20");
        await expect(page.getByLabel("Código")).toBeDisabled();
        await page.getByLabel("Ver tickets, su conversación, historial, categorías y SLA").uncheck();
        await page.getByLabel("Responder al cliente en un ticket (mensaje público)").uncheck();
        await expect(page.getByRole("button", { name: "Guardar cambios" })).toBeDisabled();
        await expect(page.getByText("Elige al menos un permiso para guardar el rol.")).toBeVisible();
        await page.getByLabel("Ver órdenes de trabajo (según el alcance) y su historial").check();
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Rol guardado")).toBeVisible();
        expect(api.peticiones.find((p) => p.ruta === "/api/v1/roles/20/editar")?.cuerpo).toMatchObject({ version: 2, permisos: ["ordenes.ver"] });

        await page.getByRole("button", { name: "Desactivar rol" }).click();
        const dialogo = page.getByRole("dialog");
        await expect(dialogo).toContainText("1 usuario pierde los permisos");
        await dialogo.getByLabel("Motivo (opcional)").fill("Fin de la campaña");
        await dialogo.getByRole("button", { name: "Desactivar" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.getByText("Rol desactivado")).toBeVisible();
        expect(api.peticiones.find((p) => p.ruta === "/api/v1/roles/20/desactivar")?.cuerpo).toEqual({ version: 3, motivo: "Fin de la campaña" });
        await page.getByRole("button", { name: "Activar rol" }).click();
        await page.getByRole("dialog").getByRole("button", { name: "Activar" }).click();
        await expect(page.getByText("Rol desactivado")).toHaveCount(0);
    });

    test("usuario: asignar un rol con alcance, quitarlo, cerrar sesiones y ver sus accesos", async ({ page, context, baseURL }, info) => {
        const api = await preparar(page, context, baseURL);
        await page.goto(`/usuarios/${CONSULTA.id}?pestana=roles`);
        const roles = page.locator('[data-seccion="roles"]');
        await expect(roles.getByRole("list", { name: "Roles asignados" })).toContainText("Lectura");

        const asignar = roles.getByRole("form", { name: "Asignar rol" });
        const boton = asignar.getByRole("button", { name: "Asignar rol" });
        await expect(boton).toBeDisabled();
        await expect(asignar.getByText("Elige un rol para poder asignarlo.")).toBeVisible();
        // Solo los roles activos.
        await expect(asignar.getByLabel("Rol").locator("option", { hasText: "Temporada alta" })).toHaveCount(0);
        await asignar.getByLabel("Rol").selectOption({ label: "Mesa de ayuda" });
        await asignar.getByRole("radio", { name: "Una zona" }).click();
        await expect(boton).toBeDisabled();
        await expect(asignar.getByText("Elige la zona del alcance.")).toBeVisible();
        await asignar.getByLabel("Zona").selectOption({ label: "Mixco" });
        await expect(boton).toBeEnabled();
        await boton.click();
        await expect(roles.locator('[data-asignacion="mesa_ayuda:5:0"]')).toContainText("Zona Mixco");
        expect(api.peticiones.find((p) => p.ruta === `/api/v1/usuarios/${CONSULTA.id}/asignar-rol`)?.cuerpo).toEqual({ rol: "mesa_ayuda", zonaId: 5 });
        await roles.getByRole("button", { name: "Quitar Mesa de ayuda (Zona Mixco)" }).click();
        await expect(roles.getByRole("list", { name: "Roles asignados" })).not.toContainText("Mesa de ayuda");

        await pestana(page, "Sesiones");
        const sesiones = page.locator('[data-seccion="sesiones"]');
        await expect(sesiones.locator("[data-sesion]")).toHaveCount(2);
        await expect(sesiones.locator('[data-sesion="901"]')).toContainText("App de campo (Android)");
        // «Cerrar todas»: los tokens no son sesiones; ofrece revocarlos (ADMIN tiene tokens.revocar).
        await sesiones.getByRole("button", { name: "Cerrar todas las sesiones" }).click();
        const todas = page.getByRole("dialog", { name: "¿Cerrar todas sus sesiones?" });
        await expect(todas.getByText("Los tokens no son sesiones: si no los revocas, siguen valiendo.")).toBeVisible();
        await todas.getByRole("button", { name: "Cancelar" }).click();
        await expect(todas).toHaveCount(0);
        await sesiones.locator('[data-sesion="900"]').getByRole("button", { name: "Cerrar" }).click();
        const dialogo = page.getByRole("dialog", { name: "¿Cerrar esta sesión?" });
        await dialogo.getByLabel("Motivo (opcional)").fill("Equipo prestado");
        await dialogo.getByRole("button", { name: "Cerrar sesión" }).click();
        await expect(sesiones.locator("[data-sesion]")).toHaveCount(1);
        expect(api.peticiones.find((p) => p.ruta === `/api/v1/usuarios/${CONSULTA.id}/sesiones/900/cerrar`)?.cuerpo).toEqual({ motivo: "Equipo prestado" });

        await pestana(page, "Accesos");
        const accesos = page.locator('[data-seccion="accesos"]');
        await expect(accesos.locator("[data-acceso]")).toHaveCount(3);
        await expect(accesos.locator('[data-acceso="54"]')).toContainText("Bloqueado por intentos");
        await expect(accesos.locator('[data-acceso="54"]')).toContainText("× 3 intentos");
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `usuario-accesos-${info.project.name}.png`), fullPage: true });
        await accesos.getByRole("link", { name: "Ver todo el historial" }).click();
        await expect(page).toHaveURL(new RegExp(`/administracion/accesos\\?usuarioId=${CONSULTA.id}$`));
        await expect(page.getByRole("form", { name: "Filtros del historial" }).getByLabel("Usuario")).toHaveValue(String(CONSULTA.id));
    });
});
