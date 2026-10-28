import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, simularApi, SUPERVISOR_ZONA } from "./api-falsa";

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

const filas = (page: Page) => page.locator("[data-ticket]").filter({ visible: true });
const fila = (page: Page, numero: string) => page.locator(`[data-ticket="${numero}"]`).filter({ visible: true });

test.describe("bandeja de tickets", () => {
    test("SLA visible con icono + texto y paginación por cursor", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(filas(page)).toHaveCount(25);
        // Los estados van separados por coma en un solo parámetro (como los lee la API).
        const primera = api.peticiones.find((p) => p.ruta === "/api/v1/tickets");
        expect(primera?.query.getAll("estado")).toEqual(["nuevo,abierto,en_espera"]);

        await expect(fila(page, "TK-000101").locator('[data-sla="en_riesgo"]')).toContainText("En riesgo");
        await expect(fila(page, "TK-000101")).toContainText(/vence en (1\d|20) min/);
        await expect(fila(page, "TK-000102").locator('[data-sla="vencido"]')).toContainText("Vencido");
        await expect(fila(page, "TK-000102")).toContainText(/venció hace 4\d min/);
        await expect(fila(page, "TK-000103").locator('[data-sla="pausado"]')).toContainText("Pausado");
        await expect(fila(page, "TK-000105").locator('[data-sla="en_tiempo"]')).toContainText("En tiempo");
        await expect(fila(page, "TK-000105")).toContainText(/vence en 2 h 1\d min/);
        await expect(fila(page, "TK-000101").locator('[data-prioridad="critica"]')).toContainText("Crítica");
        await expect(fila(page, "TK-000101").locator('[data-sla="en_riesgo"] svg').first()).toBeVisible();

        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(filas(page)).toHaveCount(29);
        await expect(page.getByText("No hay más tickets.")).toBeVisible();
    });

    test("filtros y vistas guardadas", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await expect(filas(page)).toHaveCount(25);

        await page.getByLabel("Prioridad").selectOption("critica");
        await expect(filas(page)).toHaveCount(1);
        await expect(fila(page, "TK-000101")).toBeVisible();

        await page.getByLabel("Estado").selectOption("resuelto");
        await page.getByLabel("Prioridad").selectOption("todas");
        await expect(filas(page)).toHaveCount(1);
        await expect(fila(page, "TK-000104")).toBeVisible();

        const vistas = page.getByRole("group", { name: "Vistas" });
        await vistas.getByRole("button", { name: "Asignados a mí" }).click();
        await expect(vistas.getByRole("button", { name: "Asignados a mí" })).toHaveAttribute("aria-pressed", "true");
        await expect(filas(page)).toHaveCount(1);
        await expect(fila(page, "TK-000102")).toBeVisible();

        await vistas.getByRole("button", { name: "Sin asignar" }).click();
        await expect(fila(page, "TK-000102")).toHaveCount(0);
        await expect(fila(page, "TK-000101")).toBeVisible();

        await page.getByLabel("Buscar").fill("intermitencia");
        await expect(filas(page)).toHaveCount(0);
        await expect(page.getByText("Ningún ticket coincide con los filtros")).toBeVisible();
        await page.getByLabel("Buscar").fill("");

        // Vista propia: se guarda en este navegador.
        await vistas.getByRole("button", { name: "Activos" }).click();
        await page.getByLabel("Prioridad").selectOption("alta");
        await vistas.getByRole("button", { name: "Guardar vista" }).click();
        await page.getByRole("dialog").getByLabel("Nombre de la vista").fill("Altas");
        await page.getByRole("dialog").getByRole("button", { name: "Guardar vista" }).click();
        await expect(vistas.getByRole("button", { name: "Altas", exact: true })).toHaveAttribute("aria-pressed", "true");
        await page.reload();
        await vistas.getByRole("button", { name: "Altas", exact: true }).click();
        await expect(page.getByLabel("Prioridad")).toHaveValue("alta");
        await expect(fila(page, "TK-000102")).toBeVisible();
        await vistas.getByRole("button", { name: "Eliminar la vista «Altas»" }).click();
        await expect(vistas.getByRole("button", { name: "Altas", exact: true })).toHaveCount(0);
    });
});

test.describe("detalle del ticket", () => {
    test("con tickets.ver_notas se ven las notas internas diferenciadas", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        await expect(page.getByRole("heading", { name: "Sin internet desde la mañana" })).toBeVisible();
        const nota = page.locator('[data-publico="false"]');
        await expect(nota).toContainText("Nota interna");
        await expect(nota).toContainText("Revisar la ONT antes de mandar cuadrilla.");
        await expect(page.locator('[data-reloj="respuesta"]')).toContainText("En riesgo");
        await expect(page.getByRole("group", { name: "Acciones del ticket" }).getByRole("button", { name: "Responder" })).toBeVisible();
        await expect(page.getByTestId("ficha-cliente")).toContainText("María Castillo");
        await expect(page.getByTestId("ficha-cliente")).toContainText("GTQ 150.00");
    });

    test("sin tickets.ver_notas no se piden ni se ven notas, ni hay acciones", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/tickets/1");
        await expect(page.getByRole("heading", { name: "Sin internet desde la mañana" })).toBeVisible();
        await expect(page.getByText("Sin acceso a notas internas")).toBeVisible();
        await expect(page.getByText("Hola, sin internet desde la mañana.")).toBeVisible();
        await expect(page.getByText("Revisar la ONT antes de mandar cuadrilla.")).toHaveCount(0);
        await expect(page.getByRole("group", { name: "Acciones del ticket" })).toHaveCount(0);
        const lectura = api.peticiones.find((p) => p.ruta === "/api/v1/tickets/1/mensajes");
        expect(lectura?.query.get("soloPublicos")).toBe("true");
    });

    test("responder manda version e Idempotency-Key y actualiza el ticket", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        await page.getByRole("group", { name: "Acciones del ticket" }).getByRole("button", { name: "Responder" }).click();
        const dialogo = page.getByRole("dialog");
        await dialogo.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(dialogo.getByText("Escribe la respuesta")).toBeVisible();
        await dialogo.getByLabel("Respuesta").fill("Ya revisamos la línea, ¿puede reiniciar el equipo?");
        await dialogo.getByRole("button", { name: "Enviar respuesta" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.getByText("Ya revisamos la línea, ¿puede reiniciar el equipo?")).toBeVisible();
        await expect(page.locator("header [data-estado]")).toContainText("Abierto");
        await expect(page.locator('[data-reloj="respuesta"]')).toContainText("Cumplido");
        const envio = api.peticiones.find((p) => p.ruta === "/api/v1/tickets/1/responder");
        expect(envio?.idempotencia).toBeTruthy();
        expect(envio?.cuerpo).toMatchObject({ version: 1 });
    });

    test("409 por versión: aviso con «Recargar» y reintento", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/5");
        await expect(page.getByRole("heading", { name: "Router reinicia solo" })).toBeVisible();
        api.cambiarVersion(5);
        await page.getByRole("group", { name: "Acciones del ticket" }).getByRole("button", { name: "Escalar" }).click();
        const dialogo = page.getByRole("dialog");
        await dialogo.getByLabel("Motivo").fill("El cliente reporta pérdidas");
        await dialogo.getByRole("button", { name: "Escalar" }).click();
        await expect(dialogo.getByText("Alguien más cambió este ticket")).toBeVisible();
        await dialogo.getByRole("button", { name: "Recargar" }).click();
        await expect(dialogo.getByText("Alguien más cambió este ticket")).toHaveCount(0);
        // Lo escrito se conserva y ya se envía con la versión nueva.
        await expect(dialogo.getByLabel("Motivo")).toHaveValue("El cliente reporta pérdidas");
        await dialogo.getByRole("button", { name: "Escalar" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.getByText("Escalado 1 vez")).toBeVisible();
        const envios = api.peticiones.filter((p) => p.ruta === "/api/v1/tickets/5/escalar");
        expect(envios.map((e) => (e.cuerpo as { version: number }).version)).toEqual([1, 2]);
        expect(envios[0]?.idempotencia).not.toBe(envios[1]?.idempotencia);
    });

    test("ficha de cliente desactualizada (stale)", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/2");
        const ficha = page.getByTestId("ficha-cliente");
        await expect(ficha).toContainText("Ferretería El Tornillo");
        await expect(ficha).toContainText("Desactualizado · 27/10/2026 14:15");
        await expect(ficha).toContainText("Argos no respondió");
    });

    test("ficha de cliente con el conector apagado", async ({ page, context, baseURL }) => {
        await simularApi(page, { conectorApagado: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/1");
        await expect(page.getByText("Conector con Argos apagado")).toBeVisible();
        await expect(page.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
    });

    test("un ticket que no existe o es de otra empresa", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/999");
        await expect(page.getByRole("heading", { name: "No encontramos este ticket" })).toBeVisible();
    });
});

test.describe("alta rápida", () => {
    test("valida, busca el cliente en Argos y abre el ticket creado", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await page.getByRole("button", { name: "Nuevo ticket" }).click();
        const dialogo = page.getByRole("dialog");
        await dialogo.getByRole("button", { name: "Crear ticket" }).click();
        await expect(dialogo.getByText("Escribe el asunto")).toBeVisible();
        await dialogo.getByLabel("Asunto").fill("Sin señal en todo el sector");
        await dialogo.getByLabel("Categoría").selectOption({ label: "Sin servicio" });
        await dialogo.getByLabel("Prioridad").selectOption("alta");
        await dialogo.getByLabel("Cliente en Argos").fill("Castillo");
        await dialogo.getByRole("button", { name: /María Castillo/ }).click();
        await expect(dialogo.getByTestId("cliente-elegido")).toContainText("María Castillo");
        await dialogo.getByRole("button", { name: "Crear ticket" }).click();
        await expect(page).toHaveURL(/\/tickets\/1000$/);
        await expect(page.getByRole("heading", { name: "Sin señal en todo el sector" })).toBeVisible();
        const alta = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/tickets");
        expect(alta?.cuerpo).toEqual({ asunto: "Sin señal en todo el sector", prioridad: "alta", categoriaId: 1, clienteId: 501 });
        expect(alta?.idempotencia).toBeTruthy();
    });

    test("con el conector apagado se capturan los datos de contacto a mano", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { conectorApagado: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets");
        await page.getByRole("button", { name: "Nuevo ticket" }).click();
        const dialogo = page.getByRole("dialog");
        await dialogo.getByLabel("Asunto").fill("Cliente sin registro");
        await dialogo.getByLabel("Cliente en Argos").fill("Pérez");
        await expect(dialogo.getByText("Conector con Argos apagado")).toBeVisible();
        await expect(dialogo.getByRole("group", { name: "Datos de contacto" })).toBeVisible();
        await dialogo.getByLabel("Nombre").fill("Pedro Pérez");
        await dialogo.getByLabel("Correo").fill("no-es-correo");
        await dialogo.getByRole("button", { name: "Crear ticket" }).click();
        await expect(dialogo.getByText("Escribe un correo válido")).toBeVisible();
        await dialogo.getByLabel("Correo").fill("pedro@correo.test");
        await dialogo.getByLabel("Teléfono").fill("5555 0909");
        await dialogo.getByRole("button", { name: "Crear ticket" }).click();
        await expect(page).toHaveURL(/\/tickets\/1000$/);
        const alta = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/tickets");
        expect(alta?.cuerpo).toEqual({
            asunto: "Cliente sin registro",
            prioridad: "media",
            contacto: { nombre: "Pedro Pérez", telefono: "5555 0909", correo: "pedro@correo.test" },
        });
    });
});

test.describe("«Ver en Argos/Horus» y contacto sin verificar", () => {
    const ENLACES = {
        argosisp: { conectorId: 1, urlPublica: "https://argos.isp.gt/" },
        horus: { conectorId: 2, urlPublica: "https://horus.isp.gt" },
    };

    test("con clientes.ver y conectores: botones a la base de cada sistema en otra pestaña", async ({ page, context, baseURL }) => {
        await simularApi(page, { enlaces: ENLACES });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/33");
        await expect(page.getByRole("heading", { name: "Router RB-Norte-01 caído" })).toBeVisible();
        const argos = page.getByRole("link", { name: /Ver en Argos/ });
        await expect(argos).toHaveAttribute("href", "https://argos.isp.gt");
        await expect(argos).toHaveAttribute("target", "_blank");
        await expect(argos).toHaveAttribute("rel", "noopener noreferrer");
        const horus = page.getByRole("link", { name: /Ver en Horus/ });
        await expect(horus).toHaveAttribute("href", "https://horus.isp.gt");
        await expect(horus).toHaveAttribute("rel", "noopener noreferrer");
        // Incidente maestro: lista sus tickets hijos.
        await expect(page.getByRole("list", { name: "Tickets del incidente" })).toContainText("Sin servicio en zona norte");

        // Un ticket que no viene de Horus no ofrece «Ver en Horus».
        await page.goto("/tickets/1");
        await expect(page.getByRole("link", { name: /Ver en Argos/ })).toBeVisible();
        await expect(page.getByRole("link", { name: /Ver en Horus/ })).toHaveCount(0);

        await page.goto("/tickets/34");
        await page.getByRole("link", { name: "Ver el incidente maestro" }).click();
        await expect(page.getByRole("heading", { name: "Router RB-Norte-01 caído" })).toBeVisible();
    });

    test("sin conectores activos no hay botones", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/33");
        await expect(page.getByTestId("ficha-cliente")).toContainText("María Castillo");
        await expect(page.getByRole("link", { name: /Ver en (Argos|Horus)/ })).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/integraciones/enlaces")).toBe(true);
    });

    test("sin clientes.ver no se piden los enlaces ni hay botones", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { enlaces: ENLACES });
        await conSesion(context, baseURL ?? "", SUPERVISOR_ZONA.correo);
        await page.goto("/tickets/33");
        await expect(page.getByRole("heading", { name: "Router RB-Norte-01 caído" })).toBeVisible();
        await expect(page.getByRole("link", { name: /Ver en (Argos|Horus)/ })).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/integraciones/enlaces")).toBe(false);
    });

    test("contacto sin verificar e insignia del correo retenido", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/tickets/32");
        const insignia = page.getByTestId("contacto-sin-verificar");
        await expect(insignia).toContainText("Contacto sin verificar");
        await expect(insignia.locator("svg")).toBeVisible();
        await expect(page.getByRole("complementary", { name: "Cliente" })).toContainText("no se pudo autenticar (DMARC/DKIM)");
        const retenido = page.locator('[data-correo-retenido="remitente_no_verificado"]');
        await expect(retenido).toContainText("Correo sin verificar");
        await expect(retenido).toContainText("no cambió el estado del ticket");
        await expect(retenido).toContainText("factura de octubre");

        // Un ticket con contacto verificado no la muestra.
        await page.goto("/tickets/3");
        await expect(page.getByRole("heading", { name: "Lentitud en horas pico" })).toBeVisible();
        await expect(page.getByTestId("contacto-sin-verificar")).toHaveCount(0);
    });
});

test("capturas de enlaces y contacto sin verificar", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `${que}-${info.project.name}.png`);
    await simularApi(page, { enlaces: { argosisp: { conectorId: 1, urlPublica: "https://argos.isp.gt" }, horus: { conectorId: 2, urlPublica: "https://horus.isp.gt" } } });
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/tickets/33");
    await expect(page.getByRole("link", { name: /Ver en Horus/ })).toBeVisible();
    await page.screenshot({ path: nombre("21-ver-en-argos-horus"), fullPage: true });
    await page.goto("/tickets/32");
    await expect(page.getByTestId("contacto-sin-verificar")).toBeVisible();
    await page.screenshot({ path: nombre("23-contacto-sin-verificar"), fullPage: true });
});
