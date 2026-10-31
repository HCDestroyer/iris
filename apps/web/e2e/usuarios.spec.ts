import path from "node:path";
import { expect, test, type BrowserContext, type Page } from "@playwright/test";
import { simularAccesos } from "./api-accesos";
import { ADMIN, CON_MFA, CONSULTA, conSesion, ELENA, EMPRESAS, LECTOR, simularApi, type OpcionesApi } from "./api-falsa";
import { simularUsuarios, type OpcionesUsuarios } from "./api-usuarios";
import { confirmacion } from "./ayudas";

// «Aún no puedo crear usuarios» (PM): «Nuevo usuario» en /usuarios; en la ficha, «Editar»,
// «Desactivar» y empresas; y «Crear usuario» desde «Nuevo técnico». En escritorio y teléfono,
// claro y oscuro.

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function preparar(
    page: Page,
    context: BrowserContext,
    baseURL: string | undefined,
    { correo = ADMIN.correo, api = {}, usuarios = {} }: { correo?: string; api?: OpcionesApi; usuarios?: OpcionesUsuarios } = {},
) {
    const general = await simularApi(page, api);
    await simularAccesos(page, [ADMIN, CON_MFA, LECTOR, ELENA]);
    const admin = await simularUsuarios(page, { extra: [CONSULTA], ...usuarios });
    await conSesion(context, baseURL ?? "", correo);
    return { general, admin };
}

async function sinDesborde(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("administración de usuarios", () => {
    test("«Nuevo usuario»: contraseña generada, rol y empresa; la contraseña se muestra una vez", async ({ page, context, baseURL }, info) => {
        const { admin } = await preparar(page, context, baseURL, { api: { empresas: EMPRESAS } });
        await page.goto("/usuarios");
        await page.getByRole("link", { name: "Nuevo usuario" }).click();
        await expect(page).toHaveURL(/\/usuarios\/nuevo$/);
        const d = page.getByTestId("crear-usuario");
        await expect(d).toBeVisible();
        await expect(d.getByRole("radio", { name: "Persona" })).toHaveAttribute("aria-checked", "true");

        // Sin datos: los errores salen junto a cada campo.
        await d.getByRole("button", { name: "Crear usuario" }).click();
        await expect(d.getByText("Escribe el nombre")).toBeVisible();

        await d.getByLabel("Nombre", { exact: true }).fill("Marta Xol");
        await d.getByLabel("Correo", { exact: true }).fill(" Marta.Xol@IRIS.test ");
        await d.getByRole("button", { name: "Generar" }).click();
        const campo = d.getByLabel("Contraseña temporal");
        const contrasena = await campo.inputValue();
        expect(contrasena).toMatch(/^[2-9a-zA-Z]{4}(-[2-9a-zA-Z]{4}){3}$/);
        // Oculta salvo «Mostrar», y sin autocompletar del navegador.
        await expect(campo).toHaveAttribute("type", "password");
        await expect(campo).toHaveAttribute("autocomplete", "new-password");
        await d.getByRole("button", { name: "Mostrar" }).click();
        await expect(campo).toHaveAttribute("type", "text");
        await d.getByRole("button", { name: "Ocultar" }).click();
        await expect(campo).toHaveAttribute("type", "password");

        // Un rol sin elegir impide crear y dice qué falta.
        await d.getByRole("button", { name: "Agregar rol" }).click();
        await expect(d.getByRole("button", { name: "Crear usuario" })).toBeDisabled();
        await expect(d.getByText("Falta elegir el rol de cada fila para guardar.")).toBeVisible();
        await d.getByLabel("Rol 1", { exact: true }).selectOption({ label: "Mesa de ayuda" });
        await d.getByRole("checkbox", { name: "Cable Norte" }).check();
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `usuarios-nuevo-${info.project.name}.png`), fullPage: true });
        await d.getByRole("button", { name: "Crear usuario" }).click();

        const r = page.getByTestId("usuario-creado");
        await expect(r).toContainText("Marta Xol");
        await expect(r.getByTestId("contrasena-creada")).toHaveText(contrasena);
        await expect(r.getByRole("button", { name: "Copiar" })).toBeVisible();
        await expect(r.getByText("Al entrar, Iris le pedirá cambiarla")).toBeVisible();
        const alta = admin.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/usuarios");
        expect(alta?.cuerpo).toEqual({ nombre: "Marta Xol", correo: "marta.xol@iris.test", tipo: "persona", contrasena, roles: [{ rol: "mesa_ayuda" }], empresas: [10] });
        expect(alta?.idempotencia).toBeTruthy();

        await r.getByRole("button", { name: "Ver usuario" }).click();
        await expect(page).toHaveURL(/\/usuarios\/500$/);
        await expect(page.getByRole("heading", { name: "Marta Xol" })).toBeVisible();
        await expect(page.locator('[data-seccion="empresas"]')).toContainText("Cable Norte");
        await sinDesborde(page);
    });

    test("«Nuevo usuario»: errores del servidor junto a su campo (empresas obligatorias, correo en uso) e integración sin contraseña", async ({ page, context, baseURL }) => {
        const { admin } = await preparar(page, context, baseURL, { api: { empresas: EMPRESAS }, usuarios: { empresasObligatorias: true } });
        await page.goto("/usuarios");
        await page.getByRole("link", { name: "Nuevo usuario" }).click();
        await expect(page).toHaveURL(/\/usuarios\/nuevo$/);
        const d = page.getByTestId("crear-usuario");
        await d.getByLabel("Nombre", { exact: true }).fill("Otra Ana");
        await d.getByLabel("Correo", { exact: true }).fill("ana@iris.test");
        await d.getByLabel("Contraseña temporal").fill("corta");
        await d.getByLabel("Nombre", { exact: true }).click();
        await expect(d.getByText("Usa al menos 12 caracteres")).toBeVisible();
        await d.getByLabel("Contraseña temporal").fill("una-clave-de-prueba");
        await d.getByRole("button", { name: "Crear usuario" }).click();
        await expect(d.getByText("Indica al menos una empresa en que operará")).toBeVisible();

        await d.getByRole("checkbox", { name: "Fibra Sur" }).check();
        await d.getByRole("button", { name: "Crear usuario" }).click();
        await expect(d.getByText("Ya existe un usuario con el correo ana@iris.test")).toBeVisible();

        // Integración: sin contraseña (entra con token) y solo con su rol de contrato.
        await d.getByRole("radio", { name: "Integración" }).click();
        await expect(d.getByLabel("Contraseña temporal")).toHaveCount(0);
        await expect(d.getByLabel("Rol 1", { exact: true }).locator("option")).toHaveText(["Elige un rol…", "Integración Argos", "Integración Horus"]);
        await expect(d.getByLabel("Rol 1", { exact: true })).toHaveValue("integracion_argos");
        await expect(d.getByText("Una integración entra solo con un token de API")).toBeVisible();
        await d.getByLabel("Correo", { exact: true }).fill("script@iris.test");
        await d.getByRole("button", { name: "Crear usuario" }).click();
        await expect(page.getByTestId("usuario-creado")).toContainText("recibir un token de API");
        await expect(page.getByTestId("contrasena-creada")).toHaveCount(0);
        const altas = admin.peticiones.filter((p) => p.metodo === "POST" && p.ruta === "/api/v1/usuarios");
        expect(altas.at(-1)?.cuerpo).toEqual({ nombre: "Otra Ana", correo: "script@iris.test", tipo: "integracion", roles: [{ rol: "integracion_argos" }], empresas: [20] });
        // La contraseña no queda en la URL y, de vuelta en la lista, el recién creado ya está.
        await expect(page).toHaveURL(/\/usuarios\/nuevo$/);
        // (En el teléfono el aviso «Usuario … creado» sale arriba, sobre la miga: se entra por la URL.)
        await page.goto("/usuarios");
        await expect(page.getByTestId("usuario-creado")).toHaveCount(0);
        await expect(page.getByRole("link", { name: "Otra Ana" }).first()).toBeAttached();
    });

    test("ficha: «Editar» avisa del autologin, 409 con «Recargar» y guarda", async ({ page, context, baseURL }, info) => {
        const { admin } = await preparar(page, context, baseURL);
        await page.goto(`/usuarios/${CON_MFA.id}`);
        await expect(page.getByRole("heading", { name: CON_MFA.nombre })).toBeVisible();
        await page.getByRole("link", { name: "Editar" }).click();
        await expect(page).toHaveURL(new RegExp(`/usuarios/${CON_MFA.id}/editar$`));
        const d = page.getByTestId("editar-usuario");
        await expect(d.getByRole("button", { name: "Guardar cambios" })).toBeDisabled();
        await d.getByLabel("Correo", { exact: true }).fill("mario.perez@iris.test");
        await expect(d.getByText("Cambiar el correo cierra sus sesiones y su autologin")).toBeVisible();
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `usuarios-editar-${info.project.name}.png`), fullPage: true });

        admin.cambiarVersion(CON_MFA.id);
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d.getByText("Alguien más cambió este usuario mientras lo veías")).toBeVisible();
        await d.getByRole("button", { name: "Recargar" }).click();
        await expect(d.getByText("Alguien más cambió este usuario mientras lo veías")).toHaveCount(0);
        await expect(d.getByLabel("Correo", { exact: true })).toHaveValue("mario.perez@iris.test");
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d).toHaveCount(0);
        await expect(page).toHaveURL(new RegExp(`/usuarios/${CON_MFA.id}$`));
        await expect(page.getByText("mario.perez@iris.test")).toBeVisible();
        const ediciones = admin.peticiones.filter((p) => p.ruta === `/api/v1/usuarios/${CON_MFA.id}/editar`);
        expect(ediciones.map((p) => p.cuerpo)).toEqual([
            { version: 1, correo: "mario.perez@iris.test" },
            { version: 2, correo: "mario.perez@iris.test" },
        ]);
    });

    test("ficha: «Desactivar» pide motivo, protege al último administrador y desactiva", async ({ page, context, baseURL }, info) => {
        const { admin } = await preparar(page, context, baseURL, { usuarios: { ultimosAdmin: [ELENA.id] } });
        await page.goto(`/usuarios/${ELENA.id}`);
        await page.getByRole("button", { name: "Desactivar" }).click();
        const c = confirmacion(page);
        await expect(c).toContainText(`¿Desactivar a ${ELENA.nombre}?`);
        await c.getByRole("button", { name: "Desactivar" }).click();
        await expect(c.getByText("Escribe el motivo")).toBeVisible();
        await c.getByLabel("Motivo").fill("Cambio de puesto");
        await c.getByRole("button", { name: "Desactivar" }).click();
        await expect(c.getByText("Es el último administrador activo")).toBeVisible();
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `usuarios-ultimo-admin-${info.project.name}.png`), fullPage: true });
        await c.getByRole("button", { name: "Cancelar" }).click();
        await expect(page.getByText("Activo", { exact: true })).toBeVisible();

        await page.goto(`/usuarios/${LECTOR.id}`);
        await page.getByRole("button", { name: "Desactivar" }).click();
        await confirmacion(page).getByLabel("Motivo").fill("Ya no trabaja en la empresa");
        await confirmacion(page).getByRole("button", { name: "Desactivar" }).click();
        await expect(confirmacion(page)).toHaveCount(0);
        await expect(page.getByText("Inactivo", { exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Desactivar" })).toHaveCount(0);
        expect(admin.peticiones.find((p) => p.ruta === `/api/v1/usuarios/${LECTOR.id}/desactivar`)?.cuerpo).toEqual({ version: 1, motivo: "Ya no trabaja en la empresa" });
    });

    test("ficha: empresas, asignar y quitar (con confirmación)", async ({ page, context, baseURL }, info) => {
        const { admin } = await preparar(page, context, baseURL, { api: { empresas: EMPRESAS } });
        await page.goto(`/usuarios/${LECTOR.id}`);
        const s = page.locator('[data-seccion="empresas"]');
        await expect(s).toContainText("Sin empresas");
        const asignar = s.getByRole("button", { name: "Asignar empresa" });
        await expect(asignar).toBeDisabled();
        await expect(s.getByText("Falta elegir la empresa para guardar.")).toBeVisible();
        await s.getByLabel("Empresa", { exact: true }).selectOption({ label: "Fibra Sur" });
        await asignar.click();
        await expect(s.getByRole("list", { name: "Empresas asignadas" })).toContainText("Fibra Sur");
        await expect(s.getByLabel("Empresa", { exact: true }).locator("option")).toHaveText(["Elige la empresa…", "Cable Norte"]);
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `usuarios-empresas-${info.project.name}.png`), fullPage: true });

        await s.getByRole("button", { name: "Quitar Fibra Sur" }).click();
        const c = confirmacion(page);
        await expect(c).toContainText(`¿Quitar Fibra Sur a ${LECTOR.nombre}?`);
        await c.getByRole("button", { name: "Quitar" }).click();
        await expect(c).toHaveCount(0);
        await expect(s).toContainText("Sin empresas");
        expect(admin.peticiones.filter((p) => p.ruta.includes("-empresa")).map((p) => [p.ruta, p.cuerpo])).toEqual([
            [`/api/v1/usuarios/${LECTOR.id}/asignar-empresa`, { empresaId: 20 }],
            [`/api/v1/usuarios/${LECTOR.id}/quitar-empresa`, { empresaId: 20 }],
        ]);
    });

    test("sin permisos de administración: ni «Nuevo usuario», ni «Editar», ni «Desactivar», ni asignar empresas", async ({ page, context, baseURL }) => {
        await preparar(page, context, baseURL, { correo: CONSULTA.correo, api: { empresas: EMPRESAS } });
        await page.goto("/usuarios");
        await expect(page.getByRole("heading", { name: "Usuarios" })).toBeVisible();
        await expect(page.getByRole("link", { name: ADMIN.nombre }).first()).toBeAttached();
        await expect(page.getByRole("link", { name: "Nuevo usuario" })).toHaveCount(0);
        await page.goto(`/usuarios/${CON_MFA.id}`);
        await expect(page.getByRole("heading", { name: CON_MFA.nombre })).toBeVisible();
        await expect(page.locator('[data-seccion="empresas"]')).toBeVisible();
        await expect(page.getByRole("link", { name: "Editar" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Desactivar" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Asignar empresa" })).toHaveCount(0);
        // Ni entrando directo a las pantallas de formulario.
        await page.goto("/usuarios/nuevo");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        await expect(page.getByTestId("crear-usuario")).toHaveCount(0);
        await page.goto(`/usuarios/${CON_MFA.id}/editar`);
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });
});

test.describe("técnico sin usuario", () => {
    test("«Nuevo técnico» → «Crear usuario» con el rol Técnico y vuelve con el usuario elegido", async ({ page, context, baseURL }, info) => {
        const { general, admin } = await preparar(page, context, baseURL, { api: { despacho: true } });
        admin.alCrear((u) => general.equipo.candidatos.push({ usuarioId: u.id, nombre: u.nombre, correo: u.correo }));
        await page.goto("/tecnicos");
        await page.getByRole("link", { name: "Nuevo técnico" }).click();
        const f = page.getByTestId("formulario-tecnico");
        await f.getByRole("link", { name: "Crear usuario" }).click();
        await expect(page).toHaveURL(/\/usuarios\/nuevo\?para=tecnico$/);

        const d = page.getByTestId("crear-usuario");
        await expect(d).toContainText("Se crea con el rol Técnico");
        await expect(d.getByRole("radio", { name: "Integración" })).toHaveCount(0);
        await d.getByLabel("Nombre", { exact: true }).fill("Rubén Chávez");
        await d.getByLabel("Correo", { exact: true }).fill("ruben@iris.test");
        await d.getByRole("button", { name: "Generar" }).click();
        await sinDesborde(page);
        await page.screenshot({ path: path.join(carpeta, `usuarios-tecnico-${info.project.name}.png`), fullPage: true });
        await d.getByRole("button", { name: "Crear usuario" }).click();
        await expect(page.getByTestId("usuario-creado")).toContainText("Rubén Chávez");
        expect(admin.peticiones.find((p) => p.ruta === "/api/v1/usuarios")?.cuerpo).toMatchObject({ roles: [{ rol: "tecnico" }], tipo: "persona" });
        await page.getByRole("button", { name: "Continuar con la ficha" }).click();

        // De vuelta en «Nuevo técnico» con el usuario elegido (leído de la API por su id).
        await expect(page).toHaveURL(/\/tecnicos\/nuevo\?usuario=500$/);
        await expect(page.getByTestId("crear-usuario")).toHaveCount(0);
        await expect(page.getByTestId("usuario-creado")).toHaveCount(0);
        const usuario = f.getByLabel("Usuario", { exact: true });
        await expect(usuario).toHaveValue("500");
        await f.getByLabel("Cuadrilla", { exact: true }).selectOption({ label: "Cuadrilla Sur" });
        await f.getByRole("button", { name: "Crear técnico" }).click();
        await expect(f).toHaveCount(0);
        await expect(page).toHaveURL(/\/tecnicos\/\d+$/);
        const alta = general.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/tecnicos");
        expect(alta?.cuerpo).toMatchObject({ usuarioId: 500 });
    });
});
