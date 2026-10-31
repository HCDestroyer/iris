import { expect, test, type Page } from "@playwright/test";
import { simularAccesos } from "./api-accesos";
import { ADMIN, CON_MFA, DESPACHO, ELENA, LECTOR, conSesion, simularApi } from "./api-falsa";
import { simularUsuarios } from "./api-usuarios";
import { pestana } from "./ayudas";

/*
 * Fase 3 de la interfaz: fichas con pestañas (la elegida en la URL), acciones del ticket y de la
 * orden en un panel lateral con la ficha detrás, y lo de Argos 0.5.0 (#81): vínculo del técnico,
 * «Resolver a mano», errores corregibles, cargos, «Dato de Argos cambió», modalidad del material,
 * referencia y contacto en sitio.
 */

const avisos = (page: Page) => page.getByRole("region", { name: /notificaci/ });
const elegida = (page: Page) => page.getByRole("tab", { selected: true });

test.describe("pestañas en la URL", () => {
    test("ticket: cambiar de pestaña escribe ?pestana=, recargar la conserva y un enlace abre la misma", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        await expect(page.getByRole("tablist", { name: "Secciones del ticket" }).getByRole("tab")).toHaveText(["Resumen", "Conversación", "Órdenes", "Historial"]);
        await expect(elegida(page)).toHaveText("Resumen");
        await expect(page).toHaveURL(/\/tickets\/1$/);

        await pestana(page, "Conversación");
        await expect(page).toHaveURL(/\/tickets\/1\?pestana=conversacion$/);
        await expect(page.getByRole("list", { name: "Mensajes" })).toBeVisible();
        await page.reload();
        await expect(elegida(page)).toHaveText("Conversación");
        await expect(page.getByRole("list", { name: "Mensajes" })).toBeVisible();

        // Cambiar de pestaña no apila historial: «Atrás» sale de la ficha.
        await page.goto("/tickets");
        await page.goto("/tickets/1?pestana=historial");
        await expect(elegida(page)).toHaveText("Historial");
        await pestana(page, "Órdenes");
        await pestana(page, "Resumen");
        await expect(page).toHaveURL(/\/tickets\/1$/);
        await page.goBack();
        await expect(page).toHaveURL(/\/tickets$/);

        // Un valor que no está en la lista abre la primera.
        await page.goto("/tickets/1?pestana=%3Cscript%3E");
        await expect(elegida(page)).toHaveText("Resumen");
    });

    test("cada ficha abre la pestaña del enlace", async ({ page, context, baseURL }) => {
        await simularApi(page, { despacho: true });
        await simularAccesos(page, [ADMIN, CON_MFA, LECTOR, ELENA]);
        await simularUsuarios(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        for (const [ruta, lista, nombre] of [
            ["/tickets/1?pestana=historial", "Secciones del ticket", "Historial"],
            ["/ordenes/1", "Secciones de la orden", "Resumen"],
            ["/ordenes/1?pestana=argos", "Secciones de la orden", "Argos"],
            ["/ordenes/1?pestana=evidencias", "Secciones de la orden", "Evidencias"],
            ["/tecnicos/21?pestana=ausencias", "Secciones del técnico", "Ausencias"],
            ["/comercial/5?pestana=contrato", "Secciones del prospecto", "Contrato"],
            ["/usuarios/1?pestana=empresas", "Secciones del usuario", "Empresas"],
        ] as const) {
            await page.goto(ruta);
            await expect(page.getByRole("tablist", { name: lista })).toBeVisible();
            // Las pestañas se desplazan dentro de su lista: la página no se ensancha (teléfono).
            expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
            // (La pestaña puede llevar un contador: «Argos 2».)
            await expect(elegida(page)).toHaveText(new RegExp(`^${nombre}\\d*$`));
        }
        await expect(page.getByRole("tablist", { name: "Secciones del usuario" }).getByRole("tab")).toHaveText(["Datos", "Roles", "Empresas", "Sesiones", "Accesos"]);
        await page.goto("/comercial/5");
        await expect(page.getByRole("tablist", { name: "Secciones del prospecto" }).getByRole("tab")).toHaveText(["Resumen", "Factibilidad", "Cotización", "Contrato", "Actividad"]);
        await page.goto("/tecnicos/21");
        await expect(page.getByRole("tablist", { name: "Secciones del técnico" }).getByRole("tab")).toHaveText(["Datos", "Agenda", "Ausencias"]);
    });
});

test.describe("acciones en un panel lateral", () => {
    test("responder un ticket: el panel sale a la derecha con la ficha a la vista, protege los cambios y avisa al guardar", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        await page.getByRole("group", { name: "Acciones del ticket" }).getByRole("button", { name: "Responder" }).click();
        const panel = page.getByRole("dialog", { name: "Responder al cliente" });
        await expect(panel).toBeVisible();
        await expect(panel).toHaveAccessibleDescription("TK-000101 · Sin internet desde la mañana");
        // La ficha sigue a la vista detrás (sin velo) y el panel va pegado a la derecha.
        await expect(page.getByRole("heading", { name: "Sin internet desde la mañana", level: 1 })).toBeVisible();
        const caja = await panel.boundingBox();
        const vista = page.viewportSize();
        expect(caja && vista && Math.round(caja.x + caja.width) >= vista.width - 1).toBe(true);
        if (!info.project.name.startsWith("telefono")) expect(caja && caja.x > 0).toBe(true);

        // Con cambios, cerrar pregunta.
        await panel.getByLabel("Respuesta").fill("Ya revisamos la línea.");
        await page.keyboard.press("Escape");
        const descartar = page.getByRole("alertdialog", { name: "¿Descartar los cambios?" });
        await expect(descartar).toBeVisible();
        await descartar.getByRole("button", { name: "Seguir editando" }).click();
        await expect(panel.getByLabel("Respuesta")).toHaveValue("Ya revisamos la línea.");

        await panel.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(panel).toHaveCount(0);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Respuesta enviada en el ticket TK-000101" })).toBeVisible();
    });

    test("programar una orden en el panel: misma validación («Falta…») y «Volver» sin cambios cierra", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/2");
        await page.getByRole("group", { name: "Acciones de la orden" }).getByRole("button", { name: "Programar" }).click();
        const panel = page.getByRole("dialog", { name: "Programar visita" });
        await expect(panel).toBeVisible();
        await expect(panel.getByRole("button", { name: "Programar" })).toBeDisabled();
        await expect(panel.getByRole("button", { name: "Programar" })).toHaveAccessibleDescription(/Falta elegir el técnico, la fecha/);
        await panel.getByRole("button", { name: "Volver" }).click();
        await expect(panel).toHaveCount(0);
    });
});

test.describe("vínculo del técnico con Argos", () => {
    test("sin la capacidad: «Argos < 1.6.0: inventario por técnico no disponible» y no ofrece vincular", async ({ page, context, baseURL }) => {
        await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/21");
        const vinculo = page.getByTestId("vinculo-argos");
        await expect(vinculo).toContainText("Sin vincular: Argos descuenta de la bodega de la ficha (30)");
        await expect(vinculo).toContainText("Argos < 1.6.0: inventario por técnico no disponible");
        await expect(page.getByRole("button", { name: "Vincular con Argos" })).toHaveCount(0);
    });

    test("con la capacidad: elige el usuario de Argos (solo los de la empresa y libres), vincula y desvincula", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { despacho: true });
        const conector = api.sso.conectores.find((c) => c.id === 1);
        if (conector) conector.capacidades = ["inventario_por_tecnico"];
        api.equipo.argos.inventarioPorTecnico = true;
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/21");
        await expect(page.getByTestId("vinculo-argos")).not.toContainText("Argos < 1.6.0");
        await page.getByRole("button", { name: "Vincular con Argos" }).click();
        const dialogo = page.getByRole("dialog", { name: "Vincular con Argos" });
        const usuario = dialogo.getByLabel("Usuario de Argos");
        await expect(usuario.locator("option")).toHaveText([
            "Elige el usuario…",
            "Lucía Ramírez · lucia@argos.test · bodega 30",
            "Jorge Méndez · jorge@argos.test · bodega 31 · ya vinculado a otra ficha",
            "Pedro Sic · pedro@argos.test · bodega 40 · sin bodega en esta empresa",
        ]);
        await expect(usuario.locator("option", { hasText: "Jorge Méndez" })).toBeDisabled();
        await expect(usuario.locator("option", { hasText: "Pedro Sic" })).toBeDisabled();
        await expect(dialogo.getByRole("button", { name: "Vincular" })).toHaveAccessibleDescription("Falta elegir el usuario de Argos para guardar.");
        await usuario.selectOption("77");
        await dialogo.getByRole("button", { name: "Vincular" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Lucía Ramírez quedó vinculado con Argos" })).toBeVisible();
        await expect(page.getByTestId("vinculo-argos")).toContainText("Vinculado con Lucía Ramírez (lucia@argos.test) · usuario 77 de Argos");
        const vincular = api.peticiones.find((p) => p.ruta === "/api/v1/tecnicos/21/vincular-argos");
        expect(vincular?.cuerpo).toEqual({ version: 1, idExterno: "77" });
        expect(vincular?.idempotencia).toBeTruthy();

        await page.getByRole("button", { name: "Desvincular" }).click();
        const quitar = page.getByRole("dialog", { name: "¿Desvincular de Argos?" });
        await quitar.getByLabel("Motivo (opcional)").fill("Cambió de usuario en Argos");
        await quitar.getByRole("button", { name: "Desvincular" }).click();
        await expect(page.getByTestId("vinculo-argos")).toContainText("Sin vincular");
        expect(api.peticiones.find((p) => p.ruta === "/api/v1/tecnicos/21/desvincular-argos")?.cuerpo).toEqual({ version: 2, motivo: "Cambió de usuario en Argos" });
    });

    test("si Argos responde que no tiene inventario por técnico, el selector lo dice", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { despacho: true });
        const conector = api.sso.conectores.find((c) => c.id === 1);
        if (conector) conector.capacidades = ["inventario_por_tecnico"];
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos/21");
        await page.getByRole("button", { name: "Vincular con Argos" }).click();
        const dialogo = page.getByRole("dialog", { name: "Vincular con Argos" });
        await expect(dialogo.getByText("Argos < 1.6.0: inventario por técnico no disponible")).toBeVisible();
        await expect(dialogo.getByRole("button", { name: "Vincular" })).toBeDisabled();
    });
});

test.describe("Argos 0.5.0 en la orden", () => {
    test("«Resolver a mano» un consumo: pregunta si Argos ya cobró el cargo y lo resuelve también", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { argos050: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1?pestana=argos");
        const tarjeta = page.getByTestId("pendientes-argos");
        const consumo = tarjeta.locator('[data-pendiente="consumo"]');
        await expect(consumo.getByTestId("ultimo-error")).toContainText("existencia_insuficiente");
        await consumo.getByRole("button", { name: "Resolver a mano" }).click();
        const dialogo = page.getByRole("dialog", { name: "Resolver a mano" });
        await dialogo.getByRole("button", { name: "Resolver a mano" }).click();
        await expect(dialogo.getByText("Cuenta qué se hizo en Argos")).toBeVisible();
        await dialogo.getByLabel("Qué se hizo en Argos").fill("Consumo rehecho en Argos con el origen de la orden");
        await dialogo.getByRole("button", { name: "Resolver a mano" }).click();

        // 422 decision_cargo_requerida: la pregunta del cargo.
        const pregunta = dialogo.getByRole("group", { name: "¿Argos ya cobró el cargo?" });
        await expect(pregunta).toBeVisible();
        await expect(dialogo.getByRole("button", { name: "Resolver a mano" })).toHaveAccessibleDescription("Falta elegir si Argos ya cobró el cargo para guardar.");
        await pregunta.getByRole("radio", { name: /^Sí/ }).check();
        await dialogo.getByRole("button", { name: "Resolver a mano" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "resuelto a mano" })).toBeVisible();

        await expect(consumo.getByTestId("resuelto-manual")).toContainText("Consumo rehecho en Argos con el origen de la orden");
        await expect(tarjeta.locator('[data-pendiente="cargo"]')).toHaveAttribute("data-estado", "resuelto_manual");
        // Lo que esperaba su respuesta queda rechazado e incompleto, para resolverlo también.
        await expect(tarjeta.locator('[data-pendiente="asignar_comodato"]')).toHaveAttribute("data-estado", "fallido_negocio");
        const envios = api.peticiones.filter((p) => p.ruta === "/api/v1/integraciones/pendientes/102/resolver-manual");
        expect(envios.map((p) => p.cuerpo)).toEqual([
            { version: 2, motivo: "Consumo rehecho en Argos con el origen de la orden" },
            { version: 2, motivo: "Consumo rehecho en Argos con el origen de la orden", cargo: "resolver_tambien" },
        ]);
        expect(envios.every((p) => p.idempotencia)).toBe(true);
    });

    test("«No» libera el cargo hacia Argos", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { argos050: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1?pestana=argos");
        const tarjeta = page.getByTestId("pendientes-argos");
        await tarjeta.locator('[data-pendiente="consumo"]').getByRole("button", { name: "Resolver a mano" }).click();
        const dialogo = page.getByRole("dialog", { name: "Resolver a mano" });
        await dialogo.getByLabel("Qué se hizo en Argos").fill("Se registró el consumo directo en Argos");
        await dialogo.getByRole("button", { name: "Resolver a mano" }).click();
        await dialogo.getByRole("group", { name: "¿Argos ya cobró el cargo?" }).getByRole("radio", { name: /^No/ }).check();
        await dialogo.getByRole("button", { name: "Resolver a mano" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(tarjeta.locator('[data-pendiente="cargo"]')).toHaveAttribute("data-estado", "pendiente");
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/integraciones/pendientes/102/resolver-manual").at(-1)?.cuerpo).toMatchObject({ cargo: "liberar" });
    });

    test("sin integraciones.resolver_pendiente no se ofrece «Resolver a mano»", async ({ page, context, baseURL }) => {
        await simularApi(page, { argos050: true });
        await conSesion(context, baseURL ?? "", DESPACHO.correo);
        await page.goto("/ordenes/1?pestana=argos");
        await expect(page.getByTestId("pendientes-argos").locator('[data-pendiente="consumo"]')).toBeVisible();
        await expect(page.getByRole("button", { name: "Resolver a mano" })).toHaveCount(0);
    });

    test("errores corregibles, cargos emitidos, modalidad del material, referencia y contacto en sitio", async ({ page, context, baseURL }) => {
        await simularApi(page, { argos050: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/8?pestana=argos");
        const tarjeta = page.getByTestId("pendientes-argos");
        const corregible = tarjeta.locator('[data-pendiente="consumo"][data-estado="fallido_negocio"]');
        await expect(corregible.getByTestId("corregible")).toContainText("Argos no encontró al técnico: vincúlalo con su usuario de Argos en su ficha y reintenta.");
        await expect(corregible.getByRole("button", { name: "Reintentar" })).toBeVisible();
        const hecho = tarjeta.locator('[data-pendiente="consumo"][data-estado="hecho"]');
        await expect(hecho.getByTestId("cargos-argos")).toContainText("Cargos que Argos emitió (factura 7001)");
        await expect(hecho.getByRole("list", { name: "Cargos de Argos" }).getByRole("listitem")).toHaveText(["Producto 802Q 45.00", "Producto 805Q 120.50"]);

        await page.goto("/ordenes/1");
        await expect(page.locator('[data-material="802"]')).toContainText("Se cobra al cliente");
        await expect(page.locator('[data-material="801"]')).toContainText("Gasto");
        await expect(page.getByTestId("referencia-cliente")).toHaveText("Referencia: Frente a la iglesia de San José");
        await expect(page.getByTestId("referencia-servicio")).toHaveText("Referencia: Portón negro, segundo nivel");
        await expect(page.getByTestId("contacto-en-sitio")).toContainText("Rosa Castillo");
        await expect(page.getByTestId("contacto-en-sitio").getByRole("link", { name: "+502 5555 0909" })).toHaveAttribute("href", "tel:+50255550909");
    });

    test("«Dato de Argos cambió» en el tablero (lista y columnas)", async ({ page, context, baseURL }, info) => {
        await simularApi(page, { argos050: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        const dos = page.locator('[data-orden="OT-000002"]').filter({ visible: true });
        await expect(dos.getByTestId("datos-argos-cambiados")).toHaveText("Dato de Argos cambió");
        await expect(dos.getByTestId("datos-argos-cambiados")).toHaveAttribute("title", "Cambió en Argos: dirección del servicio, teléfono de contacto en sitio");
        await expect(page.locator('[data-orden="OT-000003"]').filter({ visible: true }).getByTestId("datos-argos-cambiados")).toHaveCount(0);
        if (!info.project.name.startsWith("telefono")) {
            await page.getByRole("radio", { name: "Columnas" }).click();
            await expect(page.locator('[data-orden="OT-000002"]').filter({ visible: true }).getByTestId("datos-argos-cambiados")).toBeVisible();
        }
    });
});
