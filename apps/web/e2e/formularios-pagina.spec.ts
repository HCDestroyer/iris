import { expect, test, type Locator, type Page } from "@playwright/test";
import { simularAccesos } from "./api-accesos";
import { ADMIN, CON_MFA, ELENA, LECTOR, VENDEDORA, conSesion, simularApi } from "./api-falsa";
import { simularUsuarios } from "./api-usuarios";

/*
 * Fase 2 de la interfaz: los formularios de más de 3 campos van en pantalla propia
 * (`FormularioPagina`). Por cada pantalla: se llega por un enlace, tiene migas, «Guardar» se
 * explica cuando falta algo, salir con cambios pregunta (con «Atrás» y con una miga), un 422 se
 * muestra junto a su campo y al guardar hay un aviso y se abre el registro.
 */

const migas = (page: Page) => page.getByRole("navigation", { name: "Ruta de navegación" }).filter({ visible: true });
const avisos = (page: Page) => page.getByRole("region", { name: /notificaci/ });
const descartar = (page: Page) => page.getByRole("alertdialog", { name: "¿Descartar los cambios?" });

/** La próxima petición `POST` a `ruta` responde 422 con un error en `campo`. */
async function un422(page: Page, ruta: string, campo: string, mensaje: string) {
    let hecho = false;
    await page.route(`**${ruta}`, async (route) => {
        if (hecho || route.request().method() !== "POST") return route.fallback();
        hecho = true;
        await route.fulfill({
            status: 422,
            contentType: "application/problem+json",
            body: JSON.stringify({ type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: campo, message: mensaje }] }),
        });
    });
}

/**
 * Con cambios sin guardar: «Atrás» del navegador y la miga `miga` preguntan antes de salir;
 * «Seguir editando» conserva lo escrito en `campo`.
 */
async function protegeLaSalida(page: Page, campo: Locator, valor: string, miga: string) {
    await page.goBack();
    await expect(descartar(page)).toBeVisible();
    await descartar(page).getByRole("button", { name: "Seguir editando" }).click();
    await expect(descartar(page)).toHaveCount(0);
    await expect(campo).toHaveValue(valor);

    await migas(page).getByRole("link", { name: miga }).click();
    await expect(descartar(page)).toBeVisible();
    await descartar(page).getByRole("button", { name: "Seguir editando" }).click();
    await expect(campo).toHaveValue(valor);
}

test.describe("formularios en pantalla propia", () => {
    test("Nuevo ticket: enlace, migas, validación, salida protegida, 422 en su campo y ficha", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await page.getByRole("link", { name: "Nuevo ticket" }).click();
        await expect(page).toHaveURL(/\/tickets\/nuevo$/);
        const f = page.getByRole("form", { name: "Nuevo ticket" });
        await expect(migas(page)).toContainText("Tickets");

        // El asunto es texto: se valida al enviar, junto al campo (sin selecciones obligatorias).
        await f.getByRole("button", { name: "Crear ticket" }).click();
        await expect(f.getByText("Escribe el asunto")).toBeVisible();

        await f.getByLabel("Asunto").fill("Sin señal en la colonia");
        await protegeLaSalida(page, f.getByLabel("Asunto"), "Sin señal en la colonia", "Tickets");

        await un422(page, "/api/v1/tickets", "body.asunto", "Ya hay un ticket abierto con ese asunto");
        await f.getByRole("button", { name: "Crear ticket" }).click();
        await expect(f.getByText("Ya hay un ticket abierto con ese asunto")).toBeVisible();
        await expect(f.getByLabel("Asunto")).toHaveAttribute("aria-invalid", "true");

        await f.getByRole("button", { name: "Crear ticket" }).click();
        await expect(page).toHaveURL(/\/tickets\/1000$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: /Ticket .+ creado/ })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Sin señal en la colonia" })).toBeVisible();
    });

    test("Nueva orden: «Falta…», salida protegida, 422 en su campo y ficha", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        await page.getByRole("link", { name: "Nueva orden" }).click();
        await expect(page).toHaveURL(/\/ordenes\/nueva$/);
        const f = page.getByRole("form", { name: "Nueva orden" });
        const crear = f.getByRole("button", { name: "Crear orden" });
        await expect(crear).toBeDisabled();
        await expect(f.getByText("Elige el cliente o escribe la dirección para crear la orden.")).toBeVisible();

        await f.getByLabel("Dirección").fill("Lote 5, aldea El Carmen");
        await expect(crear).toBeEnabled();
        await protegeLaSalida(page, f.getByLabel("Dirección"), "Lote 5, aldea El Carmen", "Órdenes");

        await un422(page, "/api/v1/ordenes", "body.direccion", "La dirección es demasiado corta");
        await crear.click();
        await expect(f.getByText("La dirección es demasiado corta")).toBeVisible();
        await crear.click();
        await expect(page).toHaveURL(/\/ordenes\/1000$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: /Orden .+ creada/ })).toBeVisible();
    });

    test("Nueva orden desde un ticket: el ticket se lee de la API y «Cancelar» vuelve a él", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        // Un `ticket` que no es un id numérico se ignora: orden desde cero, sin leer nada.
        await page.goto("/ordenes/nueva?ticket=1%20OR%201");
        await expect(page.getByRole("form", { name: "Nueva orden" })).toBeVisible();
        expect(api.peticiones.some((p) => p.ruta.startsWith("/api/v1/tickets/"))).toBe(false);
        // Un ticket que no existe: lo dice, sin formulario.
        await page.goto("/ordenes/nueva?ticket=999999");
        await expect(page.getByText("Ticket no encontrado")).toBeVisible();
        await expect(page.getByRole("form")).toHaveCount(0);

        await page.goto("/tickets/1");
        await page.getByRole("link", { name: "Crear orden y asignar" }).click();
        await expect(page).toHaveURL(/\/ordenes\/nueva\?ticket=1$/);
        const f = page.getByRole("form", { name: "Crear orden y asignar" });
        // Los datos del ticket vienen de la API (`GET /tickets/1`), no de la URL.
        await expect(f).toHaveAccessibleDescription("TK-000101 · Sin internet desde la mañana");
        expect(api.peticiones.some((p) => p.metodo === "GET" && p.ruta === "/api/v1/tickets/1")).toBe(true);
        await expect(f.getByRole("button", { name: "Crear y asignar" })).toBeDisabled();
        await expect(f.getByText("Falta elegir el técnico o la cuadrilla para guardar.")).toBeVisible();

        await f.getByLabel("Cuadrilla").selectOption("1");
        await f.getByRole("button", { name: "Cancelar" }).click();
        await descartar(page).getByRole("button", { name: "Descartar cambios" }).click();
        await expect(page).toHaveURL(/\/tickets\/1$/);
    });

    test("Nuevo usuario y Editar usuario: salida protegida, 422 en su campo, aviso y ficha", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await simularAccesos(page, [ADMIN, CON_MFA, LECTOR, ELENA]);
        await simularUsuarios(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);

        await page.goto("/usuarios");
        await page.getByRole("link", { name: "Nuevo usuario" }).click();
        await expect(page).toHaveURL(/\/usuarios\/nuevo$/);
        const f = page.getByRole("form", { name: "Nuevo usuario" });
        // (Primero el nombre: salir del campo vacío lo marca en rojo y movería el botón bajo el clic.)
        await f.getByLabel("Nombre", { exact: true }).fill("Inés Ajú");
        await f.getByRole("button", { name: "Agregar rol" }).click();
        await expect(f.getByRole("button", { name: "Crear usuario" })).toBeDisabled();
        await expect(f.getByText("Falta elegir el rol de cada fila para guardar.")).toBeVisible();
        await f.getByLabel("Rol 1", { exact: true }).selectOption({ label: "Mesa de ayuda" });
        await expect(f.getByRole("button", { name: "Crear usuario" })).toBeEnabled();
        await protegeLaSalida(page, f.getByLabel("Nombre", { exact: true }), "Inés Ajú", "Usuarios");
        await f.getByLabel("Correo", { exact: true }).fill("ines@iris.test");
        await f.getByLabel("Contraseña temporal").fill("una-clave-temporal");
        await un422(page, "/api/v1/usuarios", "body.nombre", "Ese nombre ya lo usa otra persona");
        await f.getByRole("button", { name: "Crear usuario" }).click();
        await expect(f.getByText("Ese nombre ya lo usa otra persona")).toBeVisible();
        await f.getByRole("button", { name: "Crear usuario" }).click();
        // La contraseña se muestra una sola vez, en la misma pantalla (nunca en la URL).
        await expect(page.getByTestId("contrasena-creada")).toHaveText("una-clave-temporal");
        await expect(page).toHaveURL(/\/usuarios\/nuevo$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Usuario Inés Ajú creado" })).toBeVisible();
        await page.getByRole("button", { name: "Ver usuario" }).click();
        await expect(page).toHaveURL(/\/usuarios\/500$/);

        await page.getByRole("link", { name: "Editar" }).click();
        await expect(page).toHaveURL(/\/usuarios\/500\/editar$/);
        const e = page.getByRole("form", { name: "Editar a Inés Ajú" });
        await expect(migas(page)).toContainText("Inés Ajú");
        await expect(e.getByRole("button", { name: "Guardar cambios" })).toBeDisabled();
        await expect(e.getByText("Cambia el nombre o el correo para guardar.")).toBeVisible();
        await e.getByLabel("Nombre", { exact: true }).fill("Inés Ajú Tzul");
        await protegeLaSalida(page, e.getByLabel("Nombre", { exact: true }), "Inés Ajú Tzul", "Inés Ajú");
        await un422(page, "/api/v1/usuarios/500/editar", "body.nombre", "El nombre es demasiado largo");
        await e.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(e.getByText("El nombre es demasiado largo")).toBeVisible();
        await e.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page).toHaveURL(/\/usuarios\/500$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Usuario Inés Ajú Tzul guardado" })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Inés Ajú Tzul" })).toBeVisible();
    });

    test("Nuevo técnico, Editar técnico y Nueva ausencia", async ({ page, context, baseURL }) => {
        await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);

        await page.goto("/tecnicos");
        await page.getByRole("link", { name: "Nuevo técnico" }).click();
        const f = page.getByRole("form", { name: "Nuevo técnico" });
        await expect(f.getByRole("button", { name: "Crear técnico" })).toHaveAccessibleDescription("Falta elegir el usuario y la cuadrilla para guardar.");
        await f.getByLabel("Usuario", { exact: true }).selectOption({ label: "Pablo Xol · pablo@iris.test" });
        await f.getByLabel("Cuadrilla", { exact: true }).selectOption({ label: "Cuadrilla Sur" });
        await f.getByLabel("Teléfono", { exact: true }).fill("5555 1234");
        await protegeLaSalida(page, f.getByLabel("Teléfono", { exact: true }), "5555 1234", "Técnicos");
        await un422(page, "/api/v1/tecnicos", "body.telefono", "Ese teléfono ya es de otro técnico");
        await f.getByRole("button", { name: "Crear técnico" }).click();
        await expect(f.getByText("Ese teléfono ya es de otro técnico")).toBeVisible();
        await f.getByRole("button", { name: "Crear técnico" }).click();
        await expect(page).toHaveURL(/\/tecnicos\/\d+$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Técnico Pablo Xol creado" })).toBeVisible();

        // Editar desde la ficha: «Cancelar» vuelve a la ficha.
        await page.goto("/tecnicos/21");
        await page.getByRole("link", { name: "Editar", exact: true }).click();
        await expect(page).toHaveURL(/\/tecnicos\/21\/editar$/);
        const e = page.getByRole("form", { name: "Editar a Lucía Ramírez" });
        await expect(e.getByRole("button", { name: "Guardar cambios" })).toHaveAccessibleDescription("Cambia algún dato para guardar.");
        await e.getByLabel("Teléfono", { exact: true }).fill("4444 0000");
        await protegeLaSalida(page, e.getByLabel("Teléfono", { exact: true }), "4444 0000", "Lucía Ramírez");
        await un422(page, "/api/v1/tecnicos/21/editar", "body.telefono", "Teléfono inválido para Guatemala");
        await e.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(e.getByText("Teléfono inválido para Guatemala")).toBeVisible();
        await e.getByRole("button", { name: "Cancelar" }).click();
        await descartar(page).getByRole("button", { name: "Descartar cambios" }).click();
        await expect(page).toHaveURL(/\/tecnicos\/21$/);

        // Nueva ausencia.
        await page.getByRole("link", { name: "Registrar ausencia" }).click();
        await expect(page).toHaveURL(/\/tecnicos\/21\/ausencias\/nueva$/);
        const a = page.getByRole("form", { name: "Ausencia de Lucía Ramírez" });
        await expect(migas(page)).toContainText("Lucía Ramírez");
        await expect(a.getByRole("button", { name: "Registrar ausencia" })).toHaveAccessibleDescription("Falta elegir el motivo para guardar.");
        await a.getByLabel("Motivo", { exact: true }).selectOption({ label: "Vacaciones" });
        await a.getByLabel("Detalle (opcional)").fill("Viaje familiar");
        await protegeLaSalida(page, a.getByLabel("Detalle (opcional)"), "Viaje familiar", "Lucía Ramírez");
        await un422(page, "/api/v1/tecnicos/21/ausencias", "body.detalle", "El detalle no puede llevar enlaces");
        await a.getByRole("button", { name: "Registrar ausencia" }).click();
        await expect(a.getByText("El detalle no puede llevar enlaces")).toBeVisible();
        await a.getByRole("button", { name: "Registrar ausencia" }).click();
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Ausencia de Lucía Ramírez registrada" })).toBeVisible();
        // Con citas dentro de la ausencia, la pantalla las lista; «Entendido» vuelve a la ficha.
        await page.getByTestId("ausencia-conflictos").getByRole("button", { name: "Entendido" }).click();
        await expect(page).toHaveURL(/\/tecnicos\/21$/);
    });

    test("Nuevo prospecto y Cotización", async ({ page, context, baseURL }) => {
        await simularApi(page, { comercialConector: true });
        await conSesion(context, baseURL ?? "", VENDEDORA.correo);

        await page.goto("/comercial");
        await page.getByRole("link", { name: "Nuevo prospecto" }).click();
        await expect(page).toHaveURL(/\/comercial\/nuevo$/);
        const f = page.getByRole("form", { name: "Nuevo prospecto" });
        await f.getByLabel("Nombre").fill("Rosa Pérez");
        await protegeLaSalida(page, f.getByLabel("Nombre"), "Rosa Pérez", "Comercial");
        await f.getByLabel("Dirección").fill("4a calle, zona 1");
        await un422(page, "/api/v1/prospectos", "body.telefono", "Ya hay un prospecto con ese teléfono");
        await f.getByRole("button", { name: "Crear prospecto" }).click();
        await expect(f.getByText("Ya hay un prospecto con ese teléfono")).toBeVisible();
        await f.getByRole("button", { name: "Crear prospecto" }).click();
        await expect(page).toHaveURL(/\/comercial\/\d+$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: /Prospecto .+ creado/ })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Rosa Pérez" })).toBeVisible();

        // Cotización con los planes de Argos.
        await page.goto("/comercial/2");
        await page.getByTestId("factibilidad").getByRole("button", { name: "Evaluar" }).click();
        await expect(page.getByTestId("distancia")).toBeVisible();
        await page.getByTestId("cotizacion").getByRole("link", { name: "Cotizar" }).click();
        await expect(page).toHaveURL(/\/comercial\/2\/cotizacion$/);
        const c = page.getByRole("form", { name: "Cotizar" });
        await expect(migas(page)).toContainText("Bruno Díaz");
        await expect(c.getByRole("button", { name: "Guardar cotización" })).toBeDisabled();
        await expect(c.getByText("Falta elegir el plan para guardar.")).toBeVisible();
        await c.getByLabel("Plan").selectOption("72");
        await expect(c.getByRole("button", { name: "Guardar cotización" })).toBeEnabled();
        await protegeLaSalida(page, c.getByLabel("Plan"), "72", "Bruno Díaz");
        await un422(page, "/api/v1/prospectos/2/cotizaciones", "body.notas", "Las notas no pueden pasar de 2000 caracteres");
        await c.getByRole("button", { name: "Guardar cotización" }).click();
        await expect(c.getByText("Las notas no pueden pasar de 2000 caracteres")).toBeVisible();
        await c.getByRole("button", { name: "Guardar cotización" }).click();
        await expect(page).toHaveURL(/\/comercial\/2$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: /Cotización .+ creada/ })).toBeVisible();
        await expect(page.getByTestId("total-mensual")).toContainText("400.00");
    });

    test("Buzón en dos pasos: el error del otro paso lo abre, 422 en su campo y vuelta a la lista", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion/buzones");
        await page.getByRole("link", { name: "Nuevo buzón" }).click();
        await expect(page).toHaveURL(/\/administracion\/buzones\/nuevo$/);
        const f = page.getByRole("form", { name: "Nuevo buzón" });
        await expect(migas(page)).toContainText("Buzones de correo");
        await expect(f.getByRole("tab", { name: "1. Recibir" })).toHaveAttribute("aria-selected", "true");

        await f.getByLabel("Nombre").fill("Ventas");
        await f.getByLabel("Dirección").fill("ventas@isp.gt");
        await f.getByLabel("Servidor IMAP").fill("imap.isp.gt");
        await f.getByLabel("Usuario IMAP").fill("ventas@isp.gt");
        await f.getByLabel("Contraseña IMAP").fill("clave-imap");
        await f.getByLabel("authserv-id del servidor de correo").fill("mx.isp.gt");
        await protegeLaSalida(page, f.getByLabel("Nombre"), "Ventas", "Buzones de correo");

        // Falta el SMTP (paso 2): al guardar desde el paso 1 se pasa al 2 con el error a la vista.
        await f.getByRole("button", { name: "Crear buzón" }).click();
        await expect(f.getByRole("tab", { name: "2. Enviar" })).toHaveAttribute("aria-selected", "true");
        await expect(f.getByLabel("Servidor SMTP")).toHaveAttribute("aria-invalid", "true");
        await f.getByLabel("Servidor SMTP").fill("smtp.isp.gt");
        await f.getByLabel("Usuario SMTP").fill("ventas@isp.gt");
        await f.getByLabel("Contraseña SMTP").fill("clave-smtp");

        // Un 422 de un campo del paso 1 vuelve a ese paso.
        await un422(page, "/api/v1/buzones", "body.imap.host", "No se pudo resolver el servidor");
        await f.getByRole("button", { name: "Crear buzón" }).click();
        await expect(f.getByRole("tab", { name: "1. Recibir" })).toHaveAttribute("aria-selected", "true");
        await expect(f.getByText("No se pudo resolver el servidor")).toBeVisible();

        await f.getByRole("button", { name: "Crear buzón" }).click();
        await expect(page).toHaveURL(/\/administracion\/buzones$/);
        await expect(avisos(page).getByRole("alertdialog").filter({ hasText: "Buzón «Ventas» creado" })).toBeVisible();
        await expect(page.locator("li").filter({ hasText: "ventas@isp.gt" }).first()).toContainText("Inactivo");
    });

    test("sin el permiso de la acción, la pantalla del formulario no se abre", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        for (const ruta of ["/tickets/nuevo", "/ordenes/nueva", "/comercial/nuevo", "/administracion/buzones/nuevo"]) {
            await page.goto(ruta);
            await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
            await expect(page.getByRole("form")).toHaveCount(0);
        }
    });
});
