import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, CONSULTA, DESPACHO, conSesion, simularApi, type ApiFalsa } from "./api-falsa";
import { abrirMenu, menu } from "./ayudas";
import { JORGE, LUCIA } from "./datos-agenda";

/*
 * Agenda de mes (conteos por estado, conflictos, filtros y detalle del día) y la administración de
 * técnicos, cuadrillas y zonas, en escritorio (1280 px) y teléfono (390 px), claro y oscuro.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const captura = (page: Page, info: TestInfo, que: string) => page.screenshot({ path: path.join(carpeta, `${que}-${info.project.name}.png`), fullPage: true });
const enTelefono = (info: TestInfo) => info.project.name.startsWith("telefono");
const hoyGt = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala" }).format(new Date());
const utc = (dia: string, hora: string) => new Date(Date.parse(`${dia}T${hora}:00Z`) + 6 * 3_600_000).toISOString().replace(".000Z", "Z");
const dialogo = (page: Page) => page.getByRole("dialog").filter({ has: page.locator("form") });
const ultima = (api: ApiFalsa, ruta: string, metodo = "GET") => api.peticiones.filter((p) => p.ruta === ruta && p.metodo === metodo).at(-1);
const visible = (page: Page, selector: string) => page.locator(selector).filter({ visible: true });

async function sinDesborde(page: Page) {
    const ancho = page.viewportSize()?.width ?? 0;
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(ancho);
}

test.describe("agenda de mes", () => {
    test("mes con órdenes por estado y conflictos; tocar un día abre su detalle y programa ahí", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        const hoy = hoyGt();
        // Lucía se ausenta de 09:30 a 10:30: su cita OT-000003 (10:00–11:00) queda en conflicto.
        api.agenda.ausencias.push({ id: 777, tecnicoId: LUCIA, fichaId: 21, motivo: "permiso", inicio: utc(hoy, "09:30"), fin: utc(hoy, "10:30") });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/agenda");

        await expect(page.getByRole("radio", { name: "Mes" })).toBeChecked();
        const celda = visible(page, `[data-dia="${hoy}"]`);
        await expect(celda).toBeVisible();
        await expect(celda).toContainText(/\d+ órdenes/);
        await expect(celda.locator('[data-estado-dia="programada"]')).toContainText("programada");
        await expect(celda.locator('[data-estado-dia="en_camino"]')).toContainText("1 en camino");
        await expect(celda.locator("[data-conflictos]")).toContainText("1 conflicto");
        await expect(celda).toHaveAttribute("aria-label", /1 con conflicto.*Ver el día$/);
        await expect(page.getByTestId("resumen-mes")).toContainText(/\d+ órdenes con cita este mes\..*con conflicto/);
        // El mes visible se pide en trozos de hasta 31 días.
        const pedidos = api.peticiones.filter((p) => p.ruta === "/api/v1/agenda");
        expect(pedidos.length).toBeGreaterThanOrEqual(2);
        for (const p of pedidos) {
            expect(Date.parse(p.query.get("hasta") ?? "") - Date.parse(p.query.get("desde") ?? "")).toBeLessThanOrEqual(31 * 86_400_000);
        }
        if (enTelefono(info)) await expect(page.getByTestId("lista-mes")).toBeVisible();
        else await expect(page.getByTestId("cuadricula-mes")).toBeVisible();
        await sinDesborde(page);
        await captura(page, info, "50-agenda-mes");

        // Filtro por técnico: va a la URL y a GET /agenda.
        await page.getByLabel("Técnico", { exact: true }).selectOption(String(JORGE));
        await expect(page).toHaveURL(new RegExp(`tecnico=${JORGE}`));
        await expect.poll(() => ultima(api, "/api/v1/agenda")?.query.get("tecnicoId")).toBe(String(JORGE));
        await expect(visible(page, `[data-dia="${hoy}"]`).locator("[data-conflictos]")).toHaveCount(0);
        await page.getByLabel("Técnico", { exact: true }).selectOption("");

        // Detalle del día: conflictos explicados y la cita marcada.
        await visible(page, `[data-dia="${hoy}"]`).click();
        await expect(page).toHaveURL(/vista=dia/);
        await expect(page.getByRole("radio", { name: "Día" })).toBeChecked();
        await expect(visible(page, '[data-cita="OT-000003"]')).toBeVisible();
        await expect(page.getByTestId("conflictos")).toContainText("OT-000003");
        await expect(page.getByTestId("conflictos")).toContainText("cae en una ausencia del técnico (Permiso");
        await expect(visible(page, '[data-cita="OT-000003"]')).toHaveAttribute("data-conflicto", "");
        await captura(page, info, "51-agenda-dia-conflictos");

        // Programar una pendiente desde el día: el día viene puesto; sin técnico ni hora no se puede.
        await page.getByTestId("sin-programar").getByRole("button", { name: "Programar OT-000002" }).click();
        const d = dialogo(page);
        await expect(d.getByLabel("Día", { exact: true })).toHaveValue(hoy);
        const programar = d.getByRole("button", { name: "Programar" });
        await expect(programar).toBeDisabled();
        await expect(d.locator("p").getByText("Elige el técnico")).toBeVisible();
        await expect(d.locator("p").getByText("Elige la hora")).toBeVisible();
        await d.getByLabel("Técnico", { exact: true }).selectOption(String(JORGE));
        await d.getByLabel("Hora", { exact: true }).fill("10:00");
        await expect(d.getByTestId("disponibilidad")).toContainText("Jorge Méndez tiene libre ese horario");
        await expect(programar).toBeEnabled();
        await captura(page, info, "52-agenda-programar-dia");

        // Cerrar con cambios pide confirmación; «Seguir editando» conserva lo elegido.
        await page.keyboard.press("Escape");
        const confirmar = page.getByTestId("confirmar-descartar");
        await expect(confirmar).toBeVisible();
        await expect(confirmar).toContainText("¿Descartar los cambios?");
        await confirmar.getByRole("button", { name: "Seguir editando" }).click();
        await expect(d.getByLabel("Hora", { exact: true })).toHaveValue("10:00");

        await programar.click();
        await expect(page.getByTestId("movida")).toContainText("OT-000002 programada: Jorge Méndez");
        expect(ultima(api, "/api/v1/ordenes/2/programar", "POST")?.cuerpo).toEqual({ version: 1, inicio: utc(hoy, "10:00"), tecnicoId: JORGE });

        // Volver atrás regresa al mes.
        await page.goBack();
        await expect(page.getByRole("radio", { name: "Mes" })).toBeChecked();
    });

    test("si la API corta la respuesta: aviso y ningún total ni día «Sin órdenes»", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        api.agenda.truncado = true;
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/agenda");
        await expect(page.getByTestId("agenda-truncada")).toContainText("Demasiadas citas");
        await expect(page.getByTestId("agenda-truncada")).toContainText("Filtra por técnico, cuadrilla o zona");
        await expect(page.locator("[data-dia]")).toHaveCount(0);
        await expect(page.getByTestId("resumen-mes")).toHaveCount(0);
        await expect(page.getByText("Sin órdenes")).toHaveCount(0);
        await captura(page, info, "60-agenda-truncada");
        await page.goto("/agenda?vista=dia");
        await expect(page.getByTestId("agenda-truncada")).toBeVisible();
        await expect(page.locator("[data-cita]")).toHaveCount(0);
        await expect(page.locator("[data-fila-tecnico]")).toHaveCount(0);
    });

    test("navegación por mes, semana y día", async ({ page, context, baseURL }) => {
        await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/agenda?dia=2026-01-31");
        await expect(page.getByRole("heading", { name: "enero de 2026" })).toBeVisible();
        await page.getByRole("button", { name: "Mes siguiente" }).click();
        await expect(page.getByRole("heading", { name: "febrero de 2026" })).toBeVisible();
        await expect(page).toHaveURL(/dia=2026-02-28/);
        await page.getByRole("radio", { name: "Semana" }).click();
        await expect(page.getByRole("heading", { name: /^Semana del lun 23\/02 al dom 01\/03/ })).toBeVisible();
        await page.getByRole("radio", { name: "Día" }).click();
        await expect(page.getByRole("heading", { name: "sábado 28 de febrero" })).toBeVisible();
    });
});

test.describe("técnicos", () => {
    test("alta: sin usuario ni cuadrilla no se guarda; con cambios pide confirmar; crea con teléfono", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await abrirMenu(page);
        await menu(page).getByRole("link", { name: /^Técnicos/ }).click();
        await expect(page.getByRole("heading", { name: "Técnicos", exact: true })).toBeVisible();
        await expect(page.locator("[data-tecnico]")).toHaveCount(3);
        await expect(page.locator("[data-tecnico]").first()).toContainText("Cuadrilla");
        await sinDesborde(page);
        await captura(page, info, "53-tecnicos");

        await page.getByRole("button", { name: "Nuevo técnico" }).click();
        const d = dialogo(page);
        const crear = d.getByRole("button", { name: "Crear técnico" });
        await expect(crear).toBeDisabled();
        await expect(d.getByText("Elige el usuario del técnico")).toBeVisible();
        await expect(d.getByText("Elige la cuadrilla: define las zonas que atiende")).toBeVisible();
        // El usuario se busca en el servidor con `q`.
        await d.getByLabel("Buscar usuario", { exact: true }).fill("marta");
        await expect.poll(() => ultima(api, "/api/v1/tecnicos/candidatos")?.query.get("q")).toBe("marta");
        await expect(d.getByLabel("Usuario", { exact: true }).locator("option")).toHaveText(["Elige el usuario", "Marta Cux · marta@iris.test"]);
        await d.getByLabel("Buscar usuario", { exact: true }).fill("");
        await expect(d.getByLabel("Usuario", { exact: true }).locator("option")).toHaveCount(3);
        await d.getByLabel("Usuario", { exact: true }).selectOption({ label: "Pablo Xol · pablo@iris.test" });
        await expect(crear).toBeDisabled();
        await d.getByLabel("Cuadrilla", { exact: true }).selectOption({ label: "Cuadrilla Sur" });
        await expect(d.getByText("Atiende: Centro, Mixco")).toBeVisible();
        await d.getByLabel("Teléfono", { exact: true }).fill("llámame");
        await d.getByLabel("Habilidades", { exact: true }).click();
        await expect(d.getByText("Usa solo dígitos, espacios, +, -, paréntesis y puntos")).toBeVisible();
        await expect(crear).toBeDisabled();
        await d.getByLabel("Teléfono", { exact: true }).fill("+502 5555-1234");
        await d.getByLabel("Habilidades", { exact: true }).fill("Fibra, altura");
        await expect(crear).toBeEnabled();
        await captura(page, info, "54-tecnico-nuevo");

        // Cerrar con cambios: confirmar; «Descartar» cierra sin crear.
        await d.getByRole("button", { name: "Volver" }).click();
        await page.getByTestId("confirmar-descartar").getByRole("button", { name: "Descartar cambios" }).click();
        await expect(dialogo(page)).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/tecnicos" && p.metodo === "POST")).toBe(false);

        // Sin cambios, cerrar no pregunta.
        await page.getByRole("button", { name: "Nuevo técnico" }).click();
        await page.keyboard.press("Escape");
        await expect(page.getByTestId("confirmar-descartar")).toHaveCount(0);
        await expect(dialogo(page)).toHaveCount(0);

        await page.getByRole("button", { name: "Nuevo técnico" }).click();
        await d.getByLabel("Usuario", { exact: true }).selectOption({ label: "Pablo Xol · pablo@iris.test" });
        await d.getByLabel("Cuadrilla", { exact: true }).selectOption({ label: "Cuadrilla Sur" });
        await d.getByLabel("Teléfono", { exact: true }).fill("+502 5555-1234");
        await d.getByLabel("Habilidades", { exact: true }).fill("Fibra, altura");
        await crear.click();
        await expect(dialogo(page)).toHaveCount(0);
        const post = ultima(api, "/api/v1/tecnicos", "POST");
        expect(post?.cuerpo).toEqual({ usuarioId: 14, cuadrillaId: 2, habilidades: ["altura", "fibra"], telefono: "+502 5555-1234" });
        expect(post?.idempotencia).toBeTruthy();
        await expect(page.locator("[data-tecnico]").filter({ hasText: "Pablo Xol" })).toContainText("+502 5555-1234");
    });

    test("editar, desactivar y la ficha con turnos y ausencias", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos");
        const lucia = page.locator('[data-tecnico="21"]');
        await lucia.getByRole("button", { name: "Editar a Lucía Ramírez" }).click();
        const d = dialogo(page);
        const guardar = d.getByRole("button", { name: "Guardar cambios" });
        await expect(guardar).toBeDisabled();
        await d.getByLabel("Teléfono", { exact: true }).fill("4444 0000");
        await guardar.click();
        await expect(dialogo(page)).toHaveCount(0);
        expect(ultima(api, "/api/v1/tecnicos/21/editar", "POST")?.cuerpo).toMatchObject({ version: 1, cuadrillaId: 1, telefono: "4444 0000", habilidades: ["fibra"], bodegaId: 30 });
        await expect(lucia).toContainText("4444 0000");

        // Desactivar pide confirmación y manda la versión.
        await page.locator('[data-tecnico="22"]').getByRole("button", { name: "Desactivar Jorge Méndez" }).click();
        await expect(page.getByRole("alertdialog")).toContainText("Deja de ser asignable");
        await page.getByRole("alertdialog").getByRole("button", { name: "Desactivar" }).click();
        await expect(page.getByRole("alertdialog")).toHaveCount(0);
        expect(ultima(api, "/api/v1/tecnicos/22/desactivar", "POST")?.cuerpo).toEqual({ version: 1 });
        await expect(page.locator('[data-tecnico="22"]')).toHaveCount(0);
        await page.getByRole("radio", { name: "Inactivos" }).click();
        await expect(page.locator('[data-tecnico="22"]')).toContainText("Inactivo");

        // Ficha: turnos.
        await page.goto("/tecnicos/21");
        await expect(page.getByRole("heading", { name: "Lucía Ramírez" })).toBeVisible();
        await expect(page.getByRole("list", { name: "Turnos" })).toContainText("Lunes");
        await sinDesborde(page);
        await captura(page, info, "55-ficha-tecnico");
        await page.getByRole("button", { name: "Editar turnos" }).click();
        const t = dialogo(page);
        await t.getByRole("button", { name: "Lunes a viernes, 08:00–17:00" }).click();
        await t.getByRole("button", { name: "Agregar turno" }).click();
        const guardarTurnos = t.getByRole("button", { name: "Guardar turnos" });
        await expect(t.getByText("Elige el día")).toBeVisible();
        await expect(guardarTurnos).toBeDisabled();
        await t.locator('[data-turno="5"]').getByLabel("Día").selectOption({ label: "Lunes" });
        await expect(t.getByText("Se cruza con otro turno del lunes")).toBeVisible();
        await expect(guardarTurnos).toBeDisabled();
        await t.locator('[data-turno="5"]').getByLabel("Día").selectOption({ label: "Sábado" });
        await t.locator('[data-turno="5"]').getByLabel("Hasta").fill("12:00");
        await expect(guardarTurnos).toBeEnabled();
        await captura(page, info, "56-turnos");
        await guardarTurnos.click();
        await expect(dialogo(page)).toHaveCount(0);
        const turnos = (ultima(api, "/api/v1/tecnicos/21/turnos", "POST")?.cuerpo as { turnos: unknown[]; version: number }) ?? { turnos: [], version: 0 };
        expect(turnos.version).toBe(2);
        expect(turnos.turnos).toHaveLength(6);
        expect(turnos.turnos.at(-1)).toEqual({ dia: 6, inicio: "08:00", fin: "12:00" });
        await expect(page.getByRole("list", { name: "Turnos" })).toContainText("Sábado");

        // Ausencia: sin motivo no se registra; las citas que caen dentro se listan.
        await page.getByRole("button", { name: "Registrar ausencia" }).click();
        const a = dialogo(page);
        const registrar = a.getByRole("button", { name: "Registrar ausencia" });
        await expect(registrar).toBeDisabled();
        await expect(a.getByText("Elige el motivo de la ausencia")).toBeVisible();
        await a.getByLabel("Motivo", { exact: true }).selectOption({ label: "Enfermedad" });
        await expect(registrar).toBeEnabled();
        await captura(page, info, "57-ausencia");
        await registrar.click();
        const hoy = hoyGt();
        expect(ultima(api, "/api/v1/tecnicos/21/ausencias", "POST")?.cuerpo).toEqual({ motivo: "enfermedad", inicio: utc(hoy, "00:00"), fin: new Date(Date.parse(utc(hoy, "00:00")) + 86_400_000).toISOString().replace(".000Z", "Z") });
        await expect(page.getByTestId("ausencia-conflictos")).toContainText("OT-000003");
        await page.getByTestId("ausencia-conflictos").getByRole("button", { name: "Entendido" }).click();
        const fila = page.locator("[data-ausencia]").filter({ hasText: "Enfermedad" });
        await expect(fila).toBeVisible();

        // Anular la ausencia.
        await fila.getByRole("button", { name: /Anular la ausencia/ }).click();
        await page.getByRole("alertdialog").getByRole("button", { name: "Anular ausencia" }).click();
        await expect(page.locator("[data-ausencia]").filter({ hasText: "Enfermedad" })).toHaveCount(0);
    });

    test("un 409 al editar se explica y «Recargar» toma la versión nueva", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tecnicos");
        await page.locator('[data-tecnico="21"]').getByRole("button", { name: "Editar a Lucía Ramírez" }).click();
        const d = dialogo(page);
        await d.getByLabel("Teléfono", { exact: true }).fill("4444 0000");
        api.equipo.cambiarVersionTecnico(21);
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(d.getByText("Alguien más cambió esta ficha")).toBeVisible();
        await d.getByRole("button", { name: "Recargar" }).click();
        await expect(d.getByText("Alguien más cambió esta ficha")).toHaveCount(0);
        await expect(d.getByLabel("Teléfono", { exact: true })).toHaveValue("4444 0000");
        await d.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(dialogo(page)).toHaveCount(0);
        expect((ultima(api, "/api/v1/tecnicos/21/editar", "POST")?.cuerpo as { version: number }).version).toBe(2);
    });

    test("permisos: despacho ve Técnicos sin alta; con solo agenda.ver no hay menú ni pantalla", async ({ page, context, baseURL }) => {
        await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", DESPACHO.correo);
        await page.goto("/tecnicos");
        await expect(page.getByRole("heading", { name: "Técnicos", exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Nuevo técnico" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: /^Editar a/ })).toHaveCount(0);
        await expect(page.getByRole("link", { name: "Cuadrillas" })).toHaveCount(0);

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/");
        await abrirMenu(page);
        await expect(menu(page).getByRole("link", { name: /^Agenda/ })).toBeVisible();
        await expect(menu(page).getByRole("link", { name: /^Técnicos/ })).toHaveCount(0);
        await page.goto("/tecnicos");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });
});

test.describe("cuadrillas y zonas", () => {
    test("crear una zona local, una cuadrilla que la atiende y desactivarla", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/administracion");
        await page.getByRole("link", { name: /Zonas/ }).click();
        await expect(page.getByRole("heading", { name: "Zonas", exact: true })).toBeVisible();
        await page.getByRole("button", { name: "Nueva zona" }).click();
        const z = dialogo(page);
        await expect(z.getByRole("button", { name: "Crear zona" })).toBeDisabled();
        await z.getByLabel("Nombre", { exact: true }).fill("Villa Nueva");
        await z.getByRole("button", { name: "Crear zona" }).click();
        await expect(page.locator("[data-zona]").filter({ hasText: "Villa Nueva" })).toContainText("Local");
        await sinDesborde(page);
        await captura(page, info, "58-zonas");

        await page.goto("/administracion/cuadrillas");
        await page.getByRole("button", { name: "Nueva cuadrilla" }).click();
        const c = dialogo(page);
        await c.getByLabel("Nombre", { exact: true }).fill("Cuadrilla Oeste");
        await c.getByLabel("Villa Nueva").check();
        await c.getByLabel("Mixco").check();
        await captura(page, info, "59-cuadrilla-nueva");
        await c.getByRole("button", { name: "Crear cuadrilla" }).click();
        await expect(dialogo(page)).toHaveCount(0);
        const zona = api.equipo.zonas.find((x) => x.nombre === "Villa Nueva");
        expect(ultima(api, "/api/v1/cuadrillas", "POST")?.cuerpo).toEqual({ nombre: "Cuadrilla Oeste", zonaIds: [5, zona?.id] });
        const tarjeta = page.locator("[data-cuadrilla]").filter({ hasText: "Cuadrilla Oeste" });
        await expect(tarjeta).toContainText("Atiende: Mixco, Villa Nueva");

        await tarjeta.getByRole("button", { name: "Desactivar Cuadrilla Oeste" }).click();
        await page.getByRole("alertdialog").getByRole("button", { name: "Desactivar" }).click();
        await expect(tarjeta).toContainText("Inactiva");
    });
});
