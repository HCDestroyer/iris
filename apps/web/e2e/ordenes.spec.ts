import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADJ } from "./datos-ordenes";
import { ADMIN, CONSULTA, DESPACHO, conSesion, simularApi } from "./api-falsa";
import { irA, pestana } from "./ayudas";

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

const tarjetas = (page: Page) => page.locator("[data-orden]").filter({ visible: true });
const orden = (page: Page, numero: string) => page.locator(`[data-orden="${numero}"]`).filter({ visible: true });
const hoyGuatemala = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala" }).format(new Date());

test.describe("tablero de órdenes", () => {
    test("menú → lista con estado, marca «Pendiente en Argos» y cursor", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await irA(page, /^Órdenes/);
        await expect(page).toHaveURL(/\/ordenes$/);
        await expect(page.getByRole("heading", { name: "Órdenes" })).toBeVisible();
        await expect(tarjetas(page)).toHaveCount(30);

        const uno = orden(page, "OT-000001");
        await expect(uno.locator('[data-estado-orden="cerrada"]')).toContainText("Cerrada");
        await expect(uno.locator('[data-estado-orden="cerrada"] svg')).toBeVisible();
        await expect(uno.getByTestId("pendiente-argos")).toHaveText("2 pendientes en Argos · rechazado");
        await expect(uno.getByTestId("pendiente-argos")).toHaveAttribute("data-tono", "error");
        await expect(orden(page, "OT-000008").getByTestId("pendiente-argos")).toHaveText("Conflicto en Argos");
        await expect(orden(page, "OT-000003").getByTestId("pendiente-argos")).toHaveCount(0);
        await expect(orden(page, "OT-000006").locator('[data-estado-orden="fallida"]')).toContainText("Fallida");
        await expect(orden(page, "OT-000004")).toContainText("Lucía Ramírez");
        await expect(orden(page, "OT-000002")).toContainText("Sin técnico");
        await page.screenshot({ path: path.join(carpeta, `20-ordenes-lista-${info.project.name}.png`), fullPage: true });

        await page.getByRole("button", { name: "Cargar más" }).click();
        await expect(tarjetas(page)).toHaveCount(35);
        await expect(page.getByText("No hay más órdenes.")).toBeVisible();
    });

    test("filtros (estado, tipo, técnico, fecha, zona) y vista por columnas", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        await expect(tarjetas(page)).toHaveCount(30);

        await page.getByLabel("Estado").selectOption("activas");
        await page.getByLabel("Tipo").selectOption("retiro");
        await expect(tarjetas(page)).toHaveCount(1);
        await expect(orden(page, "OT-000003")).toBeVisible();
        const ultima = api.peticiones.filter((p) => p.ruta === "/api/v1/ordenes").at(-1);
        expect(ultima?.query.getAll("estado")).toEqual(["pendiente,programada,en_camino,en_sitio,pausada"]);
        expect(ultima?.query.get("tipo")).toBe("retiro");

        await page.getByLabel("Tipo").selectOption("todos");
        await page.getByLabel("Estado").selectOption("todos");
        await page.getByLabel("Técnico").selectOption("sin");
        await expect(orden(page, "OT-000002")).toBeVisible();
        await expect(orden(page, "OT-000003")).toHaveCount(0);
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/ordenes").at(-1)?.query.get("sinTecnico")).toBe("true");

        await page.getByLabel("Técnico").selectOption({ label: "Lucía Ramírez" });
        await page.getByLabel("Fecha programada").fill(hoyGuatemala());
        await page.getByLabel("Zona").fill("4");
        await expect.poll(() => api.peticiones.filter((p) => p.ruta === "/api/v1/ordenes").at(-1)?.query.get("zonaId")).toBe("4");
        await expect(tarjetas(page)).toHaveCount(5);
        const conFecha = api.peticiones.filter((p) => p.ruta === "/api/v1/ordenes").at(-1);
        expect(conFecha?.query.get("tecnicoId")).toBe("3");
        expect(conFecha?.query.get("desde")).toBe(`${hoyGuatemala()}T06:00:00Z`);
        expect(conFecha?.query.get("zonaId")).toBe("4");

        await page.getByRole("radio", { name: "Columnas" }).click();
        const finalizadas = page.locator('[data-columna="finalizada"]');
        await expect(finalizadas.locator("[data-orden]")).toHaveCount(2);
        await expect(finalizadas).toContainText("OT-000001");
        await expect(finalizadas).toContainText("OT-000006");
        await expect(page.locator('[data-columna="en_camino"]')).toContainText("OT-000004");
        await expect(page.locator('[data-columna="en_sitio"]')).toContainText("OT-000005");
        await expect(page.locator('[data-columna="pendiente"]')).toContainText("Sin órdenes");
        await page.screenshot({ path: path.join(carpeta, `21-ordenes-columnas-${info.project.name}.png`), fullPage: true });

        // La vista se recuerda en este navegador.
        await page.reload();
        await expect(page.getByRole("radio", { name: "Columnas" })).toHaveAttribute("aria-checked", "true");
    });

    test("sin ordenes.ver ni ordenes.ver_propias: sin menú ni tablero", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/ordenes");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });

    test("si los pendientes en Argos no se leen, lo dice y no marca nada", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        api.responderProblema("/api/v1/integraciones/pendientes", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 6);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        await expect(page.getByText("No se pudo leer el estado de los pendientes en Argos")).toBeVisible();
        await expect(page.getByTestId("pendiente-argos").filter({ visible: true })).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(orden(page, "OT-000001").getByTestId("pendiente-argos")).toBeVisible();
    });
});

test.describe("detalle de orden", () => {
    test("datos, cliente, ticket, checklist, galería con visor, firma, GPS, materiales y equipos", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1");
        await expect(page.getByRole("heading", { name: "4a calle 3-21, zona 1" })).toBeVisible();
        await expect(page.getByTestId("tecnico")).toHaveText("Lucía Ramírez");
        await expect(page.getByTestId("ficha-cliente")).toContainText("María Castillo");
        await expect(page.getByTestId("ticket-vinculado").getByRole("link", { name: "TK-000101" })).toHaveAttribute("href", "/tickets/1");
        // Resumen: materiales y equipos.
        await expect(page.getByRole("list", { name: "Materiales" })).toContainText("Más de lo que había en bodega");
        await expect(page.getByRole("list", { name: "Equipos" })).toContainText("AA:BB:CC:DD:EE:01");
        await expect(page.getByRole("list", { name: "Equipos" })).toContainText("ZTEG12345678");

        // Formularios: el checklist.
        await pestana(page, "Formularios");
        await expect(page.locator('[data-item="potencia"]')).toContainText("-19.4 dBm");
        await expect(page.locator('[data-item="navega"]')).toContainText("Sí");
        await expect(page.locator('[data-item="comentario"]')).toContainText("Sin marcar");

        // Evidencias. Galería: miniaturas descargadas del endpoint autenticado.
        await pestana(page, "Evidencias");
        const galeria = page.getByRole("list", { name: "Fotos de la orden" });
        await expect(galeria.getByRole("img")).toHaveCount(2);
        await expect(galeria.getByRole("img", { name: "Foto de la roseta" })).toHaveJSProperty("complete", true);
        expect(await galeria.getByRole("img", { name: "Foto de la roseta" }).evaluate((i: HTMLImageElement) => i.naturalWidth)).toBe(160);
        expect(await galeria.getByRole("img", { name: "Foto de la roseta" }).getAttribute("src")).toMatch(/^blob:/);
        await expect(page.getByText("1 foto se está subiendo desde el teléfono.")).toBeVisible();
        expect(api.peticiones.some((p) => p.ruta === `/api/v1/adjuntos/${ADJ.fotoAntena}`)).toBe(true);
        expect(api.peticiones.some((p) => p.ruta === `/api/v1/adjuntos/${ADJ.fotoSubiendo}`)).toBe(false);

        // Firma (PNG del adjunto).
        await expect(page.getByTestId("firma").getByRole("img", { name: "Firma de María Castillo" })).toHaveJSProperty("complete", true);

        // GPS con hora y enlace a mapa externo; materiales y equipos.
        const gps = page.getByRole("list", { name: "Posiciones GPS" });
        await expect(gps.getByRole("listitem")).toHaveCount(3);
        await expect(gps).toContainText("Llegó al sitio");
        await expect(gps.getByRole("link").first()).toHaveAttribute("href", /openstreetmap\.org/);
        await expect(gps.getByRole("link").first()).toHaveAttribute("rel", "noopener noreferrer");
        await page.screenshot({ path: path.join(carpeta, `22-orden-detalle-${info.project.name}.png`), fullPage: true });

        // Visor dentro del panel: nunca otra pestaña con el contenido crudo.
        await galeria.getByRole("button", { name: "Ver foto: Foto de la roseta" }).click();
        const visor = page.getByRole("dialog", { name: "Foto de la roseta" });
        await expect(visor.getByTestId("visor-foto").getByRole("img")).toHaveJSProperty("complete", true);
        await expect(visor).toContainText("Foto 1 de 2");
        await expect(page).toHaveURL(/\/ordenes\/1\?pestana=evidencias$/);
        expect(context.pages()).toHaveLength(1);
        await page.screenshot({ path: path.join(carpeta, `23-orden-visor-${info.project.name}.png`) });
        await visor.getByRole("button", { name: "Siguiente" }).click();
        await expect(page.getByRole("dialog", { name: "ONT instalada" })).toBeVisible();
        await page.keyboard.press("Escape");
        await expect(page.getByRole("dialog")).toHaveCount(0);
    });

    test("pendientes en Argos: estado, error resumido, conflicto y «Reintentar» (con 409)", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/1?pestana=argos");
        const tarjeta = page.getByTestId("pendientes-argos");
        const consumo = tarjeta.locator('[data-pendiente="consumo"]');
        await expect(consumo).toContainText("Descontar materiales");
        await expect(consumo).toContainText("Argos lo rechazó");
        await expect(consumo.getByTestId("ultimo-error")).toContainText("HTTP 422");
        await expect(consumo.getByTestId("ultimo-error")).toContainText("…");
        await expect(tarjeta.locator('[data-pendiente="asignar_comodato"]')).toContainText("Espera al pendiente 102, que falló");
        await expect(tarjeta.locator('[data-pendiente="asignar_comodato"]').getByRole("button", { name: "Reintentar" })).toHaveCount(0);
        await expect(tarjeta.locator('[data-pendiente="activar_instalacion"]')).toContainText("Hecho");
        await page.screenshot({ path: path.join(carpeta, `24-orden-pendientes-${info.project.name}.png`), fullPage: true });

        // El worker lo tocó mientras se veía: 409 → «Recargar» → reintentar.
        api.cambiarVersionPendiente(102);
        await consumo.getByRole("button", { name: "Reintentar" }).click();
        await expect(consumo.getByText("El pendiente cambió mientras lo veías")).toBeVisible();
        await consumo.getByRole("button", { name: "Recargar" }).click();
        await expect(consumo.getByText("El pendiente cambió mientras lo veías")).toHaveCount(0);
        await consumo.getByRole("button", { name: "Reintentar" }).click();
        await expect(consumo).toContainText("En cola");
        const posts = api.peticiones.filter((p) => p.metodo === "POST" && p.ruta === "/api/v1/integraciones/pendientes/102/reintentar");
        expect(posts).toHaveLength(2);
        expect(posts[1]?.cuerpo).toEqual({ version: 3 });
        expect(posts[1]?.idempotencia).toBeTruthy();
        expect(posts[1]?.idempotencia).not.toBe(posts[0]?.idempotencia);
    });

    test("sin integraciones.reintentar se ve el pendiente pero no «Reintentar»", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", DESPACHO.correo);
        await page.goto("/ordenes/1?pestana=argos");
        const consumo = page.getByTestId("pendientes-argos").locator('[data-pendiente="consumo"]');
        await expect(consumo).toContainText("Argos lo rechazó");
        await expect(consumo.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
        await expect(consumo).toContainText("Reintentar requiere el permiso");
        expect(api.mutaciones).toHaveLength(0);
    });

    test("orden con conflicto de cierre repetido", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/8?pestana=argos");
        await expect(page.getByTestId("pendiente-argos")).toHaveText("Conflicto en Argos");
        await expect(page.getByTestId("pendientes-argos")).toContainText("Conflicto: corrígelo a mano en Argos");
    });

    test("404 amable", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/999");
        await expect(page.getByText("No encontramos esta orden")).toBeVisible();
    });
});

test.describe("acciones del panel", () => {
    test("crear orden desde cero valida la dirección", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes");
        await page.getByRole("link", { name: "Nueva orden" }).click();
        await expect(page).toHaveURL(/\/ordenes\/nueva$/);
        const dialogo = page.getByRole("form", { name: "Nueva orden" });
        // Sin cliente ni dirección no se puede guardar, y se explica junto al botón.
        await expect(dialogo.getByRole("button", { name: "Crear orden" })).toBeDisabled();
        await expect(dialogo.getByRole("button", { name: "Crear orden" })).toHaveAccessibleDescription("Elige el cliente o escribe la dirección para crear la orden.");
        await dialogo.getByLabel("Dirección").fill("Lote 5, aldea El Carmen");
        await expect(dialogo.getByRole("button", { name: "Crear orden" })).toBeEnabled();
        await dialogo.getByLabel("Latitud").fill("14.5");
        await dialogo.getByRole("button", { name: "Crear orden" }).click();
        await expect(dialogo.getByText("Escribe la latitud y la longitud, o ninguna")).toBeVisible();
        await dialogo.getByLabel("Longitud").fill("-90.4");
        await dialogo.getByLabel("Nombre").fill("Pedro Ixcot");
        await dialogo.getByRole("button", { name: "Crear orden" }).click();
        await expect(page).toHaveURL(/\/ordenes\/1000$/);
        const post = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/ordenes");
        expect(post?.cuerpo).toEqual({ tipo: "instalacion", direccion: "Lote 5, aldea El Carmen", lat: 14.5, lon: -90.4, contacto: { nombre: "Pedro Ixcot" } });
    });

    test("programar con técnico, fecha y hora de Guatemala", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/2");
        await page.getByRole("group", { name: "Acciones de la orden" }).getByRole("button", { name: "Programar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Programar visita" });
        // Sin técnico ni fecha no se puede programar: botón deshabilitado con el motivo.
        await expect(dialogo.getByRole("button", { name: "Programar" })).toBeDisabled();
        await expect(dialogo.getByRole("button", { name: "Programar" })).toHaveAccessibleDescription(/Falta elegir el técnico, la fecha/);
        await dialogo.getByLabel("Técnico").selectOption({ label: "Lucía Ramírez" });
        await dialogo.getByLabel("Fecha").fill("2026-11-03");
        await dialogo.getByLabel("Hora").fill("14:30");
        await page.screenshot({ path: path.join(carpeta, `26-orden-programar-${info.project.name}.png`) });
        await dialogo.getByRole("button", { name: "Programar" }).click();
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(page.locator('[data-estado-orden="programada"]')).toBeVisible();
        await expect(page.getByTestId("cita")).toHaveText("03/11/2026 14:30 – 15:30");
        await expect(page.getByTestId("tecnico")).toHaveText("Lucía Ramírez");
        const post = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/ordenes/2/programar");
        expect(post?.cuerpo).toEqual({ version: 1, inicio: "2026-11-03T20:30:00Z", tecnicoId: 3 });
        expect(post?.idempotencia).toBeTruthy();
        await expect(page.getByRole("list", { name: "Historial" })).toContainText("Programó para el 03/11/2026 14:30");
    });

    test("409 al cancelar → «Recargar» conserva lo escrito y luego cancela", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/3");
        await expect(page.locator('[data-estado-orden="programada"]')).toBeVisible();
        api.cambiarVersionOrden(3);
        await page.getByRole("group", { name: "Acciones de la orden" }).getByRole("button", { name: "Cancelar orden" }).click();
        const dialogo = page.getByRole("dialog", { name: "Cancelar orden" });
        await dialogo.getByLabel("Por qué se cancela").fill("El cliente se mudó");
        await dialogo.getByRole("button", { name: "Cancelar orden" }).click();
        await expect(dialogo.getByText("Alguien más cambió esta orden")).toBeVisible();
        await page.screenshot({ path: path.join(carpeta, `27-orden-409-${info.project.name}.png`) });
        await dialogo.getByRole("button", { name: "Recargar" }).click();
        await expect(dialogo.getByText("Alguien más cambió esta orden")).toHaveCount(0);
        await expect(dialogo.getByLabel("Por qué se cancela")).toHaveValue("El cliente se mudó");
        await dialogo.getByRole("button", { name: "Cancelar orden" }).click();
        await expect(page.getByRole("dialog")).toHaveCount(0);
        await expect(page.locator('[data-estado-orden="cancelada"]')).toBeVisible();
        const posts = api.peticiones.filter((p) => p.metodo === "POST" && p.ruta === "/api/v1/ordenes/3/cancelar");
        expect(posts.map((p) => (p.cuerpo as { version: number }).version)).toEqual([3, 4]);
        // Cancelada: ahora se puede reabrir; reprogramar no aplica.
        const acciones = page.getByRole("group", { name: "Acciones de la orden" });
        await expect(acciones.getByRole("button", { name: "Reabrir" })).toBeVisible();
        await expect(acciones.getByRole("button", { name: "Reprogramar" })).toHaveCount(0);
    });

    test("reprogramar una fallida exige motivo", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/ordenes/6");
        await expect(page.getByText("Fallida: Cliente ausente")).toBeVisible();
        await page.getByRole("group", { name: "Acciones de la orden" }).getByRole("button", { name: "Reprogramar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Reprogramar visita" });
        await dialogo.getByLabel("Fecha").fill("2026-11-04");
        await dialogo.getByLabel("Hora").fill("09:00");
        await dialogo.getByRole("button", { name: "Reprogramar" }).click();
        await expect(dialogo.getByText("Escribe el motivo")).toBeVisible();
        await dialogo.getByLabel("Motivo").fill("El cliente estará el martes");
        await dialogo.getByRole("button", { name: "Reprogramar" }).click();
        await expect(page.locator('[data-estado-orden="programada"]')).toBeVisible();
        const post = api.peticiones.find((p) => p.ruta === "/api/v1/ordenes/6/reprogramar");
        // Se manda el técnico que ya tenía (la API solo reasigna si es otro).
        expect(post?.cuerpo).toEqual({ version: 6, inicio: "2026-11-04T15:00:00Z", tecnicoId: 3, motivo: "El cliente estará el martes" });
    });
});
