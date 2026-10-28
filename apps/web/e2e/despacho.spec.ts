import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { ADMIN, CONSULTA, LECTOR, conSesion, simularApi, type ApiFalsa } from "./api-falsa";
import { abrirMenu, avisoError, menu } from "./ayudas";
import { JORGE, LUCIA, ROSA } from "./datos-agenda";
import { simularTeselas } from "./teselas";

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const captura = (page: Page, info: TestInfo, que: string) => page.screenshot({ path: path.join(carpeta, `${que}-${info.project.name}.png`), fullPage: true });

const enTelefono = (info: TestInfo) => info.project.name.startsWith("telefono");
const hoyGt = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala" }).format(new Date());
/** Día y hora de Guatemala (UTC−6) → ISO UTC. */
const utc = (dia: string, hora: string) => new Date(Date.parse(`${dia}T${hora}:00Z`) + 6 * 3_600_000).toISOString().replace(".000Z", "Z");

const cita = (page: Page, numero: string) => page.locator(`[data-cita="${numero}"]`).filter({ visible: true });
const fila = (page: Page, usuarioId: number) => page.locator(`[data-fila-tecnico="${usuarioId}"]`).filter({ visible: true });
const dialogo = (page: Page) => page.getByRole("dialog");
const ultima = (api: ApiFalsa, ruta: string, metodo = "GET") => api.peticiones.filter((p) => p.ruta === ruta && p.metodo === metodo).at(-1);

/** Sin desbordamiento horizontal de la página (390 px en teléfono). */
async function sinDesborde(page: Page) {
    const ancho = page.viewportSize()?.width ?? 0;
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(ancho);
}

/** Horario de la vista de día del escenario (06:00–20:00) → x dentro de la línea del técnico. */
async function soltarEn(page: Page, origen: ReturnType<typeof cita>, usuarioId: number, hora: string) {
    const pista = page.locator(`[data-pista="${usuarioId}"]`).filter({ visible: true });
    const caja = await pista.boundingBox();
    if (!caja) throw new Error("sin línea del técnico");
    const [h, m] = hora.split(":").map(Number);
    const x = (((h ?? 0) * 60 + (m ?? 0) - 360) / 840) * caja.width;
    await origen.dragTo(pista, { targetPosition: { x, y: 10 } });
}

/** Abre «Mover a…» de una cita: en escritorio el bloque, en teléfono el botón de la lista. */
async function abrirMover(page: Page, info: TestInfo, numero: string) {
    if (enTelefono(info)) {
        await page.getByRole("button", { name: `Mover a… ${numero}` }).click();
    } else {
        await cita(page, numero).click();
    }
    await expect(dialogo(page)).toBeVisible();
}

async function entrarAgenda(page: Page, baseURL: string | undefined, correo = ADMIN.correo) {
    await page.context().clearCookies();
    await conSesion(page.context(), baseURL ?? "", correo);
    await page.goto("/agenda");
    await expect(cita(page, "OT-000003")).toBeVisible();
}

test.describe("agenda", () => {
    test("menú → calendario del día con citas, ventana, ausencia y órdenes sin programar", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await abrirMenu(page);
        await expect(menu(page).getByRole("link", { name: /^Agenda$/ })).toBeVisible();
        await menu(page).getByRole("link", { name: /^Agenda/ }).click();
        await expect(page).toHaveURL(/\/agenda$/);
        await expect(page.getByRole("heading", { name: "Agenda" })).toBeVisible();

        const hoy = hoyGt();
        const consulta = ultima(api, "/api/v1/agenda")?.query;
        expect(consulta?.get("desde")).toBe(utc(hoy, "00:00"));
        expect(consulta?.get("hasta")).toBe(new Date(Date.parse(utc(hoy, "00:00")) + 86_400_000).toISOString().replace(".000Z", "Z"));

        await expect(fila(page, LUCIA)).toContainText("Lucía Ramírez");
        await expect(fila(page, LUCIA)).toContainText("Cuadrilla Norte");
        for (const n of ["OT-000001", "OT-000003", "OT-000004", "OT-000005", "OT-000006"]) {
            await expect(fila(page, LUCIA).locator(`[data-cita="${n}"]`).filter({ visible: true })).toBeVisible();
        }
        await expect(cita(page, "OT-000004")).toContainText("En camino");
        await expect(cita(page, "OT-000004")).toHaveAttribute("data-estado", "en_camino");
        await expect(fila(page, JORGE).locator('[data-cita="OT-000040"]').filter({ visible: true })).toBeVisible();
        await expect(fila(page, ROSA)).toContainText("Vacaciones");
        if (enTelefono(info)) {
            await expect(cita(page, "OT-000040")).toContainText("ventana 13:00–16:00");
        } else {
            await expect(page.locator('[data-ventana="OT-000040"]')).toBeVisible();
            await expect(cita(page, "OT-000040")).toHaveAttribute("title", /ventana 13:00–16:00/);
        }
        // En sitio y cerradas no se mueven: enlazan a la orden.
        if (!enTelefono(info)) {
            await expect(cita(page, "OT-000005")).toHaveAttribute("href", "/ordenes/5");
            await expect(cita(page, "OT-000003")).toHaveAttribute("draggable", "true");
        }

        const bandeja = page.getByTestId("sin-programar");
        await expect(bandeja.locator('[data-sin-programar="OT-000002"]')).toBeVisible();
        await expect(bandeja.locator("[data-sin-programar]")).toHaveCount(9);
        await bandeja.getByRole("button", { name: /^Ver las \d+$/ }).click();
        await expect(bandeja.locator("[data-sin-programar]")).not.toHaveCount(9);
        expect(ultima(api, "/api/v1/ordenes")?.query.get("estado")).toBe("pendiente");
        await sinDesborde(page);
        await captura(page, info, "40-agenda-dia");
    });

    test("semana desde el lunes y filtros por cuadrilla y zona", async ({ page, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await entrarAgenda(page, baseURL);
        await page.getByRole("radio", { name: "Semana" }).click();
        const hoy = hoyGt();
        const semana = new Date(`${hoy}T00:00:00Z`).getUTCDay();
        const lunes = new Date(Date.parse(`${hoy}T00:00:00Z`) - ((semana + 6) % 7) * 86_400_000).toISOString().slice(0, 10);
        await expect.poll(() => ultima(api, "/api/v1/agenda")?.query.get("desde")).toBe(utc(lunes, "00:00"));
        const q = ultima(api, "/api/v1/agenda")?.query;
        expect(Date.parse(q?.get("hasta") ?? "") - Date.parse(q?.get("desde") ?? "")).toBe(7 * 86_400_000);
        await expect(page.getByRole("heading", { name: /^Semana del/ })).toBeVisible();
        await expect(cita(page, "OT-000003")).toBeVisible();
        await captura(page, info, "41-agenda-semana");

        await page.getByLabel("Cuadrilla").selectOption({ label: "Cuadrilla Sur" });
        await expect.poll(() => ultima(api, "/api/v1/agenda")?.query.get("cuadrillaId")).toBe("2");
        expect(ultima(api, "/api/v1/tecnicos")?.query.get("cuadrillaId")).toBe("2");
        await expect(fila(page, ROSA).first()).toBeVisible();
        await expect(fila(page, LUCIA)).toHaveCount(0);
        await expect(cita(page, "OT-000003")).toHaveCount(0);

        await page.getByLabel("Cuadrilla").selectOption({ label: "Todas" });
        await page.getByLabel("Zona").selectOption({ label: "Mixco" });
        await expect.poll(() => ultima(api, "/api/v1/agenda")?.query.get("zonaId")).toBe("5");
        expect(ultima(api, "/api/v1/agenda")?.query.get("cuadrillaId")).toBeNull();
        await expect(page.getByText("No hay técnicos en este filtro")).toHaveCount(0);
        await expect(cita(page, "OT-000003")).toHaveCount(0);
    });

    test("arrastrar (o «Mover a…» en el teléfono) reprograma con otro técnico y hora, con version", async ({ page, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await entrarAgenda(page, baseURL);
        const hoy = hoyGt();
        if (enTelefono(info)) {
            await abrirMover(page, info, "OT-000003");
            await dialogo(page).getByLabel("Técnico", { exact: true }).selectOption(String(JORGE));
            await dialogo(page).getByLabel("Hora", { exact: true }).fill("15:30");
        } else {
            await soltarEn(page, cita(page, "OT-000003"), JORGE, "15:30");
            await expect(dialogo(page)).toBeVisible();
            await expect(dialogo(page).getByLabel("Técnico", { exact: true })).toHaveValue(String(JORGE));
            await expect(dialogo(page).getByLabel("Hora", { exact: true })).toHaveValue("15:30");
            await expect(dialogo(page).getByLabel("Día", { exact: true })).toHaveValue(hoy);
        }
        await expect(dialogo(page).getByRole("heading", { name: "Mover a… · OT-000003" })).toBeVisible();
        await expect(dialogo(page).getByTestId("disponibilidad")).toContainText("Jorge Méndez tiene libre ese horario (60 min).");
        const asignables = ultima(api, "/api/v1/tecnicos/asignables")?.query;
        expect(asignables?.get("fecha")).toBe(hoy);
        expect(asignables?.get("tipo")).toBe("retiro");
        expect(asignables?.get("zonaId")).toBe("4");
        expect(asignables?.get("ordenId")).toBe("3");

        await dialogo(page).getByRole("button", { name: "Reprogramar" }).click();
        await expect(dialogo(page).getByText("Escribe el motivo")).toBeVisible();
        await dialogo(page).getByLabel("Motivo", { exact: true }).fill("Lucía no llega a tiempo");
        await captura(page, info, "42-mover-a");
        await dialogo(page).getByRole("button", { name: "Reprogramar" }).click();
        await expect(page.getByTestId("movida")).toContainText("OT-000003 reprogramada: Jorge Méndez");
        const post = ultima(api, "/api/v1/ordenes/3/reprogramar", "POST");
        expect(post?.cuerpo).toEqual({ version: 3, inicio: utc(hoy, "15:30"), fin: utc(hoy, "16:30"), tecnicoId: JORGE, motivo: "Lucía no llega a tiempo" });
        expect(post?.idempotencia).toBeTruthy();
        await expect(fila(page, JORGE).locator('[data-cita="OT-000003"]').filter({ visible: true })).toBeVisible();
        await expect(fila(page, LUCIA).locator('[data-cita="OT-000003"]').filter({ visible: true })).toHaveCount(0);
    });

    test("comprueba contra asignables y muestra el 422 de la API sin perder lo elegido", async ({ page, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await entrarAgenda(page, baseURL);
        await abrirMover(page, info, "OT-000003");
        const d = dialogo(page);
        const enviar = d.getByRole("button", { name: "Reprogramar" });
        await d.getByLabel("Motivo", { exact: true }).fill("Pedido del cliente");

        await d.getByLabel("Técnico", { exact: true }).selectOption(String(ROSA));
        await expect(d.getByTestId("disponibilidad")).toContainText("Rosa Ajú: Está ausente ese día.");
        await expect(d.getByLabel("Técnico", { exact: true }).locator(`option[value="${ROSA}"]`)).toContainText("Está ausente ese día");
        await expect(enviar).toBeDisabled();

        await d.getByLabel("Técnico", { exact: true }).selectOption(String(JORGE));
        await d.getByLabel("Hora", { exact: true }).fill("14:30");
        await expect(d.getByTestId("disponibilidad")).toContainText("Ese horario no está libre para Jorge Méndez (60 min). Libre: 07:00–14:00 y 15:00–17:00.");
        await expect(enviar).toBeDisabled();
        await captura(page, info, "43-mover-no-disponible");

        await d.getByLabel("Hora", { exact: true }).fill("15:00");
        await expect(enviar).toBeEnabled();
        api.responderProblema(
            "/api/v1/ordenes/3/reprogramar",
            { type: "urn:iris:problema:capacidad_excedida", title: "Capacidad excedida", status: 422, detail: "Jorge Méndez ya tiene 6 retiros ese día (máximo 6)" },
            1,
        );
        await enviar.click();
        await expect(d.getByRole("alert")).toContainText("No se pudo reprogramar la orden");
        await expect(d.getByRole("alert")).toContainText("Jorge Méndez ya tiene 6 retiros ese día (máximo 6)");
        await expect(d.getByLabel("Técnico", { exact: true })).toHaveValue(String(JORGE));
        await expect(d.getByLabel("Hora", { exact: true })).toHaveValue("15:00");
        await expect(d.getByLabel("Motivo", { exact: true })).toHaveValue("Pedido del cliente");
        await captura(page, info, "44-mover-422");
        const primera = ultima(api, "/api/v1/ordenes/3/reprogramar", "POST")?.idempotencia;

        await enviar.click();
        await expect(page.getByTestId("movida")).toContainText("OT-000003 reprogramada");
        const segunda = ultima(api, "/api/v1/ordenes/3/reprogramar", "POST")?.idempotencia;
        expect(segunda).toBeTruthy();
        expect(segunda).not.toBe(primera);
    });

    test("un 422 de la agenda (fuera de turno) y un 409 se explican; «Recargar» trae la versión nueva", async ({ page, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await entrarAgenda(page, baseURL);
        // La disponibilidad no se pudo leer: se puede enviar igual y la API decide.
        api.responderProblema("/api/v1/tecnicos/asignables", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await abrirMover(page, info, "OT-000003");
        const d = dialogo(page);
        await expect(d.getByText("No se pudo comprobar la disponibilidad")).toBeVisible();
        await d.getByLabel("Motivo", { exact: true }).fill("Reacomodo de ruta");
        await d.getByLabel("Hora", { exact: true }).fill("18:00");
        await d.getByRole("button", { name: "Reprogramar" }).click();
        await expect(d.getByRole("alert")).toContainText("La visita (18:00–19:00) queda fuera del turno de Lucía Ramírez (07:00–17:00)");
        await expect(d.getByLabel("Hora", { exact: true })).toHaveValue("18:00");
        await d.getByRole("button", { name: "Reintentar" }).click();
        await expect(d.getByTestId("disponibilidad")).toContainText("Ese horario no está libre para Lucía Ramírez");

        await d.getByLabel("Hora", { exact: true }).fill("15:00");
        await expect(d.getByTestId("disponibilidad")).toContainText("tiene libre ese horario");
        api.cambiarVersionOrden(3);
        await d.getByRole("button", { name: "Reprogramar" }).click();
        await expect(d.getByText("Alguien más cambió esta orden")).toBeVisible();
        await expect(d.getByRole("button", { name: "Reprogramar" })).toBeDisabled();
        await captura(page, info, "45-mover-409");
        await d.getByRole("button", { name: "Recargar" }).click();
        await expect(d.getByText("Alguien más cambió esta orden")).toHaveCount(0);
        await expect(d.getByLabel("Motivo", { exact: true })).toHaveValue("Reacomodo de ruta");
        await d.getByRole("button", { name: "Reprogramar" }).click();
        await expect(page.getByTestId("movida")).toContainText("OT-000003 reprogramada");
        const posts = api.peticiones.filter((p) => p.ruta === "/api/v1/ordenes/3/reprogramar" && p.metodo === "POST");
        expect((posts.at(-2)?.cuerpo as { version: number }).version).toBe(3);
        expect((posts.at(-1)?.cuerpo as { version: number }).version).toBe(4);
    });

    test("programar una pendiente de la bandeja y asignar otra sin hora", async ({ page, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await entrarAgenda(page, baseURL);
        const hoy = hoyGt();
        const bandeja = page.getByTestId("sin-programar");
        if (enTelefono(info)) {
            await bandeja.getByRole("button", { name: "Programar OT-000002" }).click();
            await dialogo(page).getByLabel("Técnico", { exact: true }).selectOption(String(LUCIA));
            await dialogo(page).getByLabel("Hora", { exact: true }).fill("15:00");
        } else {
            await soltarEn(page, bandeja.locator('[data-sin-programar="OT-000002"]'), LUCIA, "15:00");
            await expect(dialogo(page).getByLabel("Técnico", { exact: true })).toHaveValue(String(LUCIA));
            await expect(dialogo(page).getByLabel("Hora", { exact: true })).toHaveValue("15:00");
        }
        await expect(dialogo(page).getByLabel("Motivo", { exact: true })).toHaveCount(0);
        await expect(dialogo(page).getByTestId("disponibilidad")).toContainText("Lucía Ramírez tiene libre ese horario");
        await dialogo(page).getByRole("button", { name: "Programar" }).click();
        await expect(page.getByTestId("movida")).toContainText("OT-000002 programada: Lucía Ramírez");
        expect(ultima(api, "/api/v1/ordenes/2/programar", "POST")?.cuerpo).toEqual({ version: 1, inicio: utc(hoy, "15:00"), tecnicoId: LUCIA });
        await expect(fila(page, LUCIA).locator('[data-cita="OT-000002"]').filter({ visible: true })).toBeVisible();
        await expect(bandeja.locator('[data-sin-programar="OT-000002"]')).toHaveCount(0);

        if (enTelefono(info)) {
            await bandeja.getByRole("button", { name: "Programar OT-000010" }).click();
            await dialogo(page).getByLabel("Asignar el técnico sin programar la visita").check();
            await dialogo(page).getByLabel("Técnico", { exact: true }).selectOption(String(JORGE));
        } else {
            // Soltar en el nombre del técnico: solo asignar.
            await bandeja.locator('[data-sin-programar="OT-000010"]').dragTo(page.locator(`[data-cabecera-tecnico="${JORGE}"]`).filter({ visible: true }));
            await expect(dialogo(page).getByLabel("Asignar el técnico sin programar la visita")).toBeChecked();
            await expect(dialogo(page).getByLabel("Técnico", { exact: true })).toHaveValue(String(JORGE));
        }
        await expect(dialogo(page).getByLabel("Hora", { exact: true })).toHaveCount(0);
        await dialogo(page).getByRole("button", { name: "Asignar" }).click();
        await expect(page.getByTestId("movida")).toContainText("OT-000010 asignada: Jorge Méndez");
        expect(ultima(api, "/api/v1/ordenes/10/asignar", "POST")?.cuerpo).toEqual({ version: 1, tecnicoId: JORGE, cuadrillaId: 1 });
    });

    test("si la agenda no se lee: error con «Reintentar» y ningún calendario a medias", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { despacho: true });
        api.responderProblema("/api/v1/agenda", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/agenda");
        await expect(avisoError(page)).toContainText("No se pudo cargar la agenda");
        await expect(page.locator("[data-cita]")).toHaveCount(0);
        await expect(page.locator("[data-fila-tecnico]")).toHaveCount(0);
        await avisoError(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(cita(page, "OT-000003")).toBeVisible();
    });

    test("sin permisos de órdenes: citas de solo lectura y sin bandeja; sin agenda.ver, sin acceso", async ({ page, context, baseURL }, info) => {
        await simularApi(page, { despacho: true });
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/agenda");
        await expect(cita(page, "OT-000003")).toBeVisible();
        await expect(page.getByRole("button", { name: /Mover a…/ })).toHaveCount(0);
        if (!enTelefono(info)) await expect(cita(page, "OT-000003")).toHaveAttribute("href", "/ordenes/3");
        await expect(page.getByTestId("sin-programar")).toHaveCount(0);

        await page.goto("/despacho");
        await expect(page.getByText("Sin permiso para ver ubicaciones")).toBeVisible();

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        for (const ruta of ["/agenda", "/despacho"]) {
            await page.goto(ruta);
            await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        }
    });
});

test.describe("despacho", () => {
    test("mapa con órdenes y técnicos en vivo (antigüedad visible), teselas sin red y ruta real", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        const teselas = await simularTeselas(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/");
        await abrirMenu(page);
        await menu(page).getByRole("link", { name: /^Despacho/ }).click();
        await expect(page).toHaveURL(/\/despacho$/);

        await expect(page.getByTestId("mapa")).toHaveAttribute("data-listo", "si", { timeout: 20_000 });
        for (const n of ["OT-000001", "OT-000003", "OT-000004", "OT-000005", "OT-000040"]) {
            await expect(page.locator(`[data-marcador-orden="${n}"]`)).toBeAttached();
        }
        await expect(page.locator('[data-marcador-orden="OT-000006"]')).toHaveCount(0);
        await expect(page.getByTestId("leyenda-mapa")).toHaveText("5 órdenes en el mapa · 1 sin coordenadas · 3 técnicos con posición");
        await expect(page.locator(`[data-marcador-tecnico="${LUCIA}"]`)).toHaveAttribute("data-frescura", "en_vivo");
        await expect(page.locator(`[data-marcador-tecnico="${JORGE}"]`)).toHaveAttribute("data-frescura", "retrasada");
        await expect(page.locator(`[data-marcador-tecnico="${ROSA}"]`)).toHaveAttribute("aria-label", /Rosa Ajú · Sin señal reciente · hace 5 h/);
        expect(teselas.pedidas()).toBeGreaterThan(0);

        const vivo = page.getByTestId("seccion-en-vivo");
        await expect(vivo.locator(`[data-posicion="${LUCIA}"]`)).toContainText(/En vivo · hace \d+ s/);
        await expect(vivo.locator(`[data-posicion="${LUCIA}"]`)).toContainText("±12 m · 29 km/h");
        await expect(vivo.locator(`[data-posicion="${JORGE}"]`)).toContainText("Retrasada · hace 9 min");
        await expect(vivo.locator(`[data-posicion="${ROSA}"]`)).toContainText("Sin señal reciente · hace 5 h");
        await expect(vivo.getByTestId("posiciones-leidas")).toContainText("se actualizan cada 30 s");
        await vivo.getByRole("button", { name: "Ver a Lucía Ramírez en el mapa" }).click();

        const ruta = page.getByTestId("seccion-ruta");
        await expect(ruta.getByTestId("ruta-propuesta-pendiente")).toContainText("Ruta propuesta: todavía no disponible");
        await ruta.getByLabel("Técnico de la ruta").selectOption({ label: "Lucía Ramírez" });
        await expect(ruta.locator("[data-parada]")).toHaveCount(2);
        await expect(ruta.locator("[data-parada]").nth(0)).toContainText("OT-000001");
        await expect(ruta.locator("[data-parada]").nth(0)).toContainText("llegó 08:30");
        await expect(ruta.locator("[data-parada]").nth(1)).toContainText("OT-000005");
        await expect(ruta.locator("[data-parada]").nth(1)).toContainText("Sin propuesta");
        expect(ultima(api, "/api/v1/ordenes")?.query.get("tecnicoId")).toBe(String(LUCIA));

        await expect(page.getByTestId("seccion-calendario").locator('[data-cita="OT-000040"]').filter({ visible: true })).toBeVisible();
        await sinDesborde(page);
        await page.waitForTimeout(500);
        await captura(page, info, "46-despacho");
    });

    test("si las posiciones no se leen: error con «Reintentar» y ningún técnico en el mapa", async ({ page, context, baseURL }, info) => {
        const api = await simularApi(page, { despacho: true });
        await simularTeselas(page);
        api.responderProblema("/api/v1/campo/posiciones/ultimas", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/despacho");
        const vivo = page.getByTestId("seccion-en-vivo");
        await expect(vivo.getByRole("alert")).toContainText("No se pudo cargar las posiciones de los técnicos");
        await expect(page.getByTestId("mapa")).toHaveAttribute("data-listo", "si", { timeout: 20_000 });
        await expect(page.locator(`[data-marcador-orden="OT-000003"]`)).toBeAttached();
        await expect(page.locator("[data-marcador-tecnico]")).toHaveCount(0);
        await expect(page.getByTestId("leyenda-mapa")).not.toContainText("técnicos con posición");
        await captura(page, info, "47-despacho-error-posiciones");
        await vivo.getByRole("button", { name: "Reintentar" }).click();
        await expect(vivo.locator(`[data-posicion="${LUCIA}"]`)).toBeVisible();
        await expect(page.locator("[data-marcador-tecnico]")).toHaveCount(3);
    });

    test("la antigüedad avanza entre lecturas y las posiciones se vuelven a pedir cada 30 s", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { despacho: true });
        await simularTeselas(page);
        await page.clock.install();
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/despacho");
        const vivo = page.getByTestId("seccion-en-vivo");
        const lucia = vivo.locator(`[data-posicion="${LUCIA}"]`);
        await expect(lucia).toContainText(/En vivo · hace 4\d s/);
        const lecturas = () => api.peticiones.filter((p) => p.ruta === "/api/v1/campo/posiciones/ultimas").length;
        const antes = lecturas();
        // 25 s después, sin leer otra vez: la antigüedad leída más lo que pasó en el navegador.
        await page.clock.fastForward("00:25");
        await expect(lucia).toContainText("En vivo · hace 1 min");
        expect(lecturas()).toBe(antes);
        await page.clock.fastForward("00:10");
        await expect.poll(lecturas).toBeGreaterThan(antes);
        await expect(lucia).toContainText(/En vivo · hace 4\d s/);
    });
});
