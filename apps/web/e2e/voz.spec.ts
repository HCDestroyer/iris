import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, CONSULTA, EMPRESAS, conSesion, simularApi } from "./api-falsa";
import type { OpcionesVoz } from "./api-voz";
import { abrirMenu, esTelefono, hojaMas, selectorEmpresa } from "./ayudas";
import { elegirOpcion } from "./controles";
import { mensaje } from "./datos-conversaciones";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";
import { ajustarVoz, instalarDobleVoz, type ControlVoz } from "./doble-webrtc";

/*
 * Voz E1 (contratos/voz.md §3): personas contestan llamadas de WhatsApp en el panel. «Recibir
 * llamadas» con el permiso del micrófono, la llamada entrante (contestar, rechazar, perder la
 * carrera, plazos de 12 s y 5 s, varias pestañas), la llamada en curso (barra que sigue al navegar,
 * silenciar, colgar, corte, cierre de pestaña), la nota, la llamada en el hilo, las perdidas y sus
 * devoluciones, y los ajustes. API y feed falsos tipados con packages/contratos, WebRTC y micrófono
 * con dobles, y reloj simulado para los plazos. En los 4 proyectos y sin desbordes.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-voz");
type Doble = Partial<Pick<ControlVoz, "microfono" | "permiso" | "medio">>;
type Extra = { voz?: OpcionesVoz; correo?: string; doble?: Doble; opciones?: Parameters<typeof simularApi>[1] };

async function abrir(page: Page, ruta: string, { voz = { recibe: true }, correo = ADMIN.correo, doble = { permiso: "granted" }, opciones = {} }: Extra = {}) {
    // Reloj simulado que corre solo: los plazos se adelantan con `fastForward`.
    await page.clock.install();
    await instalarDobleVoz(page, doble);
    const api = await simularApi(page, { ...opciones, conversaciones: { flujoLargo: true, ...opciones.conversaciones }, voz });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}
type Api = Awaited<ReturnType<typeof abrir>>;

/**
 * Deja el flujo de eventos abierto y esperando: lo que se emita después llega al instante. Tras
 * cada entrega el panel espera antes de reconectar; aquí se adelanta esa espera.
 */
async function feedListo(page: Page, api: Api) {
    await expect
        .poll(
            async () => {
                if (api.flujoAbierto()) return true;
                // De a poco: un salto largo dejaría caducar el timbre de una llamada que suena.
                await page.clock.fastForward(7_000);
                return api.flujoAbierto();
            },
            { timeout: 20_000, intervals: [200, 400, 800] },
        )
        .toBe(true);
}

/** Adelanta el reloj de a poco hasta que se cumpla (los reintentos esperan 1, 2, 4, 8 y 8 s tras cada respuesta). */
const adelantarHasta = (page: Page, listo: () => boolean | undefined) =>
    expect
        .poll(
            async () => {
                if (listo()) return true;
                await page.clock.fastForward(2_000);
                return Boolean(listo());
            },
            { timeout: 25_000, intervals: [150] },
        )
        .toBe(true);

const aviso = (page: Page) => page.locator('[data-slot="aviso-llamada"]');
const barra = (page: Page) => page.getByRole("region", { name: "Llamada en curso" });
const nota = (page: Page) => page.getByRole("dialog", { name: "Terminó la llamada" });
const acciones = (api: Api, accion: string) => api.voz.recibido.acciones.filter((a) => a.accion === accion);
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });

/**
 * Sin desbordes, una vez asentado lo que entra con animación (el diálogo de la nota, la hoja «Más»):
 * se espera hasta 5 s a que quede en su sitio; si algo sigue fuera, falla diciendo qué.
 */
async function sinDesbordes(page: Page) {
    await expect.poll(async () => (await buscarDesbordes(page)).length, { timeout: 5_000 }).toBe(0).catch(() => undefined);
    await esperarSinDesbordes(page);
}

/** «Recibir llamadas»: en escritorio, el teléfono de la barra superior; en el teléfono, arriba de «Más». */
async function abrirRecibir(page: Page) {
    if (esTelefono(page)) {
        await abrirMenu(page);
        return hojaMas(page).getByRole("region", { name: "Llamadas" });
    }
    await page.getByRole("button", { name: /^Recibir llamadas/ }).click();
    return page.getByRole("dialog", { name: "Llamadas" });
}

/** Suena una llamada del cliente de la conversación 41 (Carlos Ruiz) y se espera el aviso. */
async function suena(page: Page, api: Api, conversacionId = 41) {
    await feedListo(page, api);
    const llamada = api.voz.entrante(conversacionId);
    await expect(aviso(page)).toHaveAttribute("data-fase", "sonando");
    return llamada;
}

test.describe("voz › recibir llamadas", () => {
    test("encender explica para qué es el micrófono, pide el permiso y queda guardado por navegador", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: {}, doble: { permiso: "prompt" } });
        const panel = await abrirRecibir(page);
        const interruptor = panel.getByRole("switch", { name: "Recibir llamadas en este navegador" });
        await expect(interruptor).not.toBeChecked();
        await expect(panel).toContainText("Apagado: las llamadas no suenan en este navegador.");
        await interruptor.click();
        // Antes de que el navegador pregunte, Iris dice por qué.
        await expect(panel).toContainText("Iris necesita tu micrófono");
        await expect(panel).toContainText("solo se usa mientras estás en una llamada");
        expect(await page.evaluate(() => window.__voz.pedidos)).toBe(0);
        await sinDesbordes(page);
        await captura(page, "recibir-explica");
        await panel.getByRole("button", { name: "Permitir micrófono" }).click();
        await expect(interruptor).toBeChecked();
        await expect(page.getByText("Este navegador ya recibe llamadas")).toBeVisible();
        await expect(panel).toContainText("Te suenan las llamadas de WhatsApp de tus colas");
        // El micrófono solo se probó: no queda abierto.
        expect(await page.evaluate(() => window.__voz.microfonoAbierto())).toBe(false);
        const navegador = api.voz.recibido.recibir[0]?.navegador ?? "";
        expect(api.voz.recibido.recibir).toEqual([{ accion: "encender", navegador }]);
        expect(navegador).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
        // Al leer, el id va en la cabecera `X-Iris-Navegador`, nunca en la URL (no queda en los registros de acceso).
        const lecturas = api.peticiones.filter((p) => p.metodo === "GET" && p.ruta.endsWith("/llamadas/recibir"));
        expect(lecturas.length).toBeGreaterThan(0);
        expect(lecturas.every((p) => p.query.get("navegador") === null)).toBe(true);
        // Al recargar sigue encendido: el id del navegador se guardó.
        await page.reload();
        const otra = await abrirRecibir(page);
        await expect(otra.getByRole("switch", { name: "Recibir llamadas en este navegador" })).toBeChecked();
        await otra.getByRole("switch", { name: "Recibir llamadas en este navegador" }).click();
        await expect(page.getByText("Este navegador ya no recibe llamadas")).toBeVisible();
        expect(api.voz.recibido.recibir.at(-1)).toEqual({ accion: "apagar", navegador });
    });

    test("si el navegador niega el micrófono o no hay ninguno, lo dice y no se enciende", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: {}, doble: { permiso: "prompt", microfono: "denegado" } });
        const panel = await abrirRecibir(page);
        const interruptor = panel.getByRole("switch", { name: "Recibir llamadas en este navegador" });
        await interruptor.click();
        await panel.getByRole("button", { name: "Permitir micrófono" }).click();
        await expect(panel).toContainText("El navegador bloqueó el micrófono");
        await expect(panel).toContainText("Toca el candado de la barra de direcciones");
        await expect(interruptor).not.toBeChecked();
        expect(api.voz.recibido.recibir).toEqual([]);
        await sinDesbordes(page);
        await captura(page, "recibir-microfono-bloqueado");
        // Sin micrófono conectado.
        await ajustarVoz(page, { microfono: "sin_microfono" });
        await panel.getByRole("button", { name: "Volver a intentar" }).click();
        await expect(panel).toContainText("No se encontró un micrófono");
        expect(api.voz.recibido.recibir).toEqual([]);
        // Lo conecta y vuelve a intentar: se enciende.
        await ajustarVoz(page, { microfono: "ok" });
        await panel.getByRole("button", { name: "Volver a intentar" }).click();
        await expect(interruptor).toBeChecked();
        expect(api.voz.recibido.recibir.map((r) => r.accion)).toEqual(["encender"]);
    });

    test("encendido en el servidor pero con el micrófono bloqueado: se apaga y pide actuar; si la lectura falla, «Reintentar»", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { doble: { permiso: "denied" } });
        if (!esTelefono(page)) await expect(page.getByRole("button", { name: "Recibir llamadas: necesita tu atención" })).toBeVisible();
        const panel = await abrirRecibir(page);
        await expect(panel).toContainText("El navegador bloqueó el micrófono");
        await expect(panel.getByRole("switch", { name: "Recibir llamadas en este navegador" })).not.toBeChecked();
        await expect.poll(() => api.voz.recibido.recibir.map((r) => r.accion)).toEqual(["apagar"]);
        // Con el interruptor apagado no suena nada aunque el servidor mande la llamada.
        api.voz.entrante(41);
        await page.waitForTimeout(600);
        await expect(aviso(page)).toHaveCount(0);

        // Lectura fallida: ni «encendido» ni «apagado» inventados.
        await page.route("**/api/v1/mensajeria/llamadas/recibir", (r) => r.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error del servidor", status: 500 }) }), { times: 1 });
        await page.reload();
        const otra = await abrirRecibir(page);
        await expect(otra).toContainText("No se pudo leer si recibes llamadas aquí");
        await expect(otra.getByRole("switch")).toHaveCount(0);
        await otra.getByRole("button", { name: "Reintentar" }).click();
        await expect(otra.getByRole("switch", { name: "Recibir llamadas en este navegador" })).toBeVisible();
    });

    test("el servidor apagó «Recibir» en este navegador (más de 10 por persona): lo dice y deja volver a encender", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        if (!esTelefono(page)) await expect(page.getByRole("button", { name: "Recibir llamadas: encendido" })).toBeVisible();
        else await feedListo(page, api);
        api.voz.forzar.desplazado = "tope";
        await page.clock.fastForward(61_000);
        if (!esTelefono(page)) await expect(page.getByRole("button", { name: "Recibir llamadas: necesita tu atención" })).toBeVisible();
        const panel = await abrirRecibir(page);
        await expect(panel).toContainText("Este navegador dejó de recibir llamadas");
        await expect(panel.locator('[data-slot="recibir-apagado"]')).toHaveAttribute("data-motivo", "tope");
        await expect(panel).toContainText("Lo tienes encendido en más de 10 navegadores");
        await expect(panel.getByRole("switch", { name: "Recibir llamadas en este navegador" })).not.toBeChecked();
        await sinDesbordes(page);
        await captura(page, "recibir-desplazado");
        await panel.getByRole("button", { name: "Volver a encender" }).click();
        await expect(panel.getByRole("switch", { name: "Recibir llamadas en este navegador" })).toBeChecked();
        await expect(panel).not.toContainText("Este navegador dejó de recibir llamadas");
        expect(api.voz.recibido.recibir.map((r) => r.accion)).toEqual(["encender"]);
    });

    test("apagado por 30 días sin abrir este navegador: al entrar lo dice con ese motivo", async ({ page }) => {
        await page.clock.install();
        await instalarDobleVoz(page, { permiso: "granted" });
        const api = await simularApi(page, { conversaciones: { flujoLargo: true }, voz: {} });
        api.voz.forzar.desplazado = "inactivo";
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
        await page.goto("/cuenta");
        const panel = await abrirRecibir(page);
        await expect(panel.locator('[data-slot="recibir-apagado"]')).toHaveAttribute("data-motivo", "inactivo");
        await expect(panel).toContainText("más de 30 días sin abrir Iris en este navegador");
        await panel.getByRole("button", { name: "Volver a encender" }).click();
        await expect(panel.getByRole("switch", { name: "Recibir llamadas en este navegador" })).toBeChecked();
    });

    test("sin `llamadas.contestar` no hay interruptor, ni aviso, ni se abre el flujo", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { correo: CONSULTA.correo });
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expect(page.getByRole("button", { name: /^Recibir llamadas/ })).toHaveCount(0);
        if (esTelefono(page)) {
            await abrirMenu(page);
            await expect(hojaMas(page).getByRole("region", { name: "Llamadas" })).toHaveCount(0);
        }
        api.voz.entrante(41);
        await page.waitForTimeout(600);
        await expect(aviso(page)).toHaveCount(0);
        expect(api.peticiones.filter((p) => p.ruta.includes("/llamadas/"))).toEqual([]);
    });
});

test.describe("voz › llamada entrante", () => {
    test("suena con el cliente y sus datos; contestar conecta, abre su conversación y la barra sigue al navegar; colgar y anotar", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        const tarjeta = aviso(page);
        await expect(tarjeta).toHaveAccessibleName("Llamada entrante de Carlos Ruiz");
        await expect(tarjeta).toContainText("Llamada de WhatsApp · +50255120041");
        await expect(tarjeta).toContainText("Al número Soporte");
        // Servicio y ticket abierto: lo que manda el servidor en la propia llamada (`senales`).
        const datos = tarjeta.getByRole("list", { name: "Datos del cliente" });
        await expect(datos).toContainText("Fibra 100 Mb · activo");
        await expect(datos).toContainText("Ticket TK-004812");
        // Sin `saldoVencido: true` no se dice nada del saldo (ni «al día»).
        await expect(datos).not.toContainText(/[Ss]aldo|al día/);
        expect(api.peticiones.some((p) => p.metodo === "GET" && p.ruta === "/api/v1/mensajeria/conversaciones/41")).toBe(false);
        // `reclamar`, `contestar` y `confirmar-medio` llevan el id de este navegador en la cabecera.
        await expect(tarjeta).toHaveAttribute("data-timbre", "sonando");
        await sinDesbordes(page);
        await captura(page, "entrante");

        await tarjeta.getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        await expect(tarjeta).toHaveCount(0);
        // reclamar → contestar (con la respuesta SDP) → confirmar-medio, una vez cada uno.
        expect(api.voz.recibido.acciones.map((a) => a.accion)).toEqual(["reclamar", "contestar", "confirmar-medio"]);
        expect(new Set(api.voz.recibido.acciones.map((a) => a.navegador)).size).toBe(1);
        expect(api.voz.recibido.acciones[0]?.navegador).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
        // La respuesta SDP del navegador viaja tal cual: quien la sanea es el servidor.
        const sdp = String(acciones(api, "contestar")[0]?.cuerpo.sdpRespuesta);
        expect(sdp).toBe(await page.evaluate(() => window.__voz.respuesta));
        expect(await page.evaluate(() => window.__voz.conexiones.map((c) => c.oferta?.includes("m=audio")))).toEqual([true]);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("en_curso");
        // La ficha del cliente a la vista: se abrió su conversación.
        await expect(page).toHaveURL(/\/conversaciones\?c=41/);
        await expect(page.getByRole("region", { name: "Conversación con Carlos Ruiz" })).toBeVisible();
        await expect(barra(page)).toContainText("Carlos Ruiz");
        await page.clock.fastForward(72_000);
        await expect(barra(page).getByRole("timer")).toHaveText(/^01:1\d$/);
        await sinDesbordes(page);
        await captura(page, "en-curso");

        // Silenciar apaga la pista del micrófono, no la llamada.
        await barra(page).getByRole("button", { name: "Silenciar" }).click();
        await expect(barra(page).getByRole("button", { name: "Activar micrófono" })).toHaveAttribute("aria-pressed", "true");
        expect(await page.evaluate(() => window.__voz.pistasActivas())).toBe(false);
        await barra(page).getByRole("button", { name: "Activar micrófono" }).click();
        expect(await page.evaluate(() => window.__voz.pistasActivas())).toBe(true);
    });

    test("la barra de la llamada persiste al cambiar de pantalla sin recargar; colgar ofrece la nota con «Crear ticket» y «Agendar visita»", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        await expect(page.getByRole("region", { name: "Conversación con Carlos Ruiz" })).toBeVisible();
        const llamada = await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        // Navegación del panel (sin recargar): otra pantalla y la barra sigue ahí.
        await page.getByRole("link", { name: "Iris, inicio" }).click();
        await expect(page).toHaveURL(/\/$/);
        await expect(barra(page)).toBeVisible();
        await expect(barra(page).getByRole("link", { name: "Ver cliente" })).toHaveAttribute("href", "/conversaciones?c=41");
        expect(await page.evaluate(() => window.__voz.conexiones.filter((c) => !c.cerrada).length)).toBe(1);
        await sinDesbordes(page);
        await captura(page, "en-curso-otra-pantalla");

        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await expect(barra(page)).toHaveCount(0);
        const dialogo = nota(page);
        await expect(dialogo).toContainText("Carlos Ruiz · duró");
        await expect.poll(() => acciones(api, "colgar").length).toBe(1);
        // Al colgar se cierran la conexión y el micrófono.
        expect(await page.evaluate(() => ({ abiertas: window.__voz.conexiones.filter((c) => !c.cerrada).length, microfono: window.__voz.microfonoAbierto() }))).toEqual({ abiertas: 0, microfono: false });
        await expect(dialogo.getByRole("button", { name: "Crear ticket" })).toBeVisible();
        await expect(dialogo.getByRole("button", { name: "Agendar visita" })).toBeVisible();
        await expect(dialogo.getByRole("button", { name: "Guardar nota" })).toBeDisabled();
        await dialogo.getByLabel("Nota (opcional)").fill("Reinició el router con ayuda; quedó navegando.");
        await elegirOpcion(dialogo.getByLabel("¿Cómo quedó? (opcional)"), "Quedó resuelto");
        await sinDesbordes(page);
        await captura(page, "nota");
        // El servidor falla una vez: la nota no se pierde y se puede reintentar.
        api.voz.forzar.anotar = "error";
        await dialogo.getByRole("button", { name: "Guardar nota" }).click();
        await expect(dialogo).toContainText("No se pudo guardar la nota");
        await expect(dialogo.getByLabel("Nota (opcional)")).toHaveValue("Reinició el router con ayuda; quedó navegando.");
        await dialogo.getByRole("button", { name: "Guardar nota" }).click();
        await expect(page.getByText("Nota de la llamada guardada")).toBeVisible();
        await expect(dialogo).toHaveCount(0);
        expect(acciones(api, "anotar").at(-1)?.cuerpo).toEqual({ nota: "Reinició el router con ayuda; quedó navegando.", resultado: "resuelto" });
        expect(api.voz.llamadas.get(llamada.id)?.nota).toBe("Reinició el router con ayuda; quedó navegando.");
    });

    test("«Agendar visita» al terminar guarda la nota y abre la orden desde la conversación; «Ahora no» cierra sin anotar", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await nota(page).getByLabel("Nota (opcional)").fill("Pide visita: la luz PON parpadea.");
        await nota(page).getByRole("button", { name: "Agendar visita" }).click();
        await expect(page).toHaveURL(/\/conversaciones\/41\/orden$/);
        expect(acciones(api, "anotar").map((a) => a.cuerpo)).toEqual([{ nota: "Pide visita: la luz PON parpadea." }]);
        await expect(nota(page)).toHaveCount(0);

        // Otra llamada: «Ahora no».
        await page.goto("/cuenta");
        await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await nota(page).getByRole("button", { name: "Ahora no" }).click();
        await expect(nota(page)).toHaveCount(0);
        expect(acciones(api, "anotar")).toHaveLength(1);
    });

    test("número desconocido: lo dice así, sin inventar un nombre; rechazar deja de sonar aquí", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await feedListo(page, api);
        const desconocida = api.conversaciones.conversaciones.find((c) => !c.contacto.clienteId);
        expect(desconocida).toBeTruthy();
        const llamada = api.voz.entrante(desconocida?.id ?? 0);
        const tarjeta = aviso(page);
        await expect(tarjeta).toHaveAccessibleName("Llamada entrante de Número desconocido");
        await expect(tarjeta).toContainText(`Llamada de WhatsApp · ${llamada.numero}`);
        await sinDesbordes(page);
        await captura(page, "entrante-desconocido");
        await tarjeta.getByRole("button", { name: "Rechazar", exact: true }).click();
        await expect(tarjeta).toHaveCount(0);
        await expect.poll(() => acciones(api, "rechazar").map((a) => a.cuerpo)).toEqual([{}]);
        // Solo dejó de sonarme a mí: la llamada sigue sonando en el servidor.
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("sonando");
    });

    test("saldo vencido: se pide el detalle al sonar y solo se muestra si el servidor dice `true`", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: { recibe: true, saldoVencido: [41] } });
        const llamada = await suena(page, api);
        await expect(aviso(page).locator('[data-slot="saldo-vencido"]')).toHaveText("Saldo vencido");
        expect(api.peticiones.some((p) => p.metodo === "GET" && p.ruta.endsWith(`/llamadas/${llamada.id}`))).toBe(true);
        await sinDesbordes(page);
        await captura(page, "entrante-saldo-vencido");
        await aviso(page).getByRole("button", { name: "Rechazar", exact: true }).click();
        // Otro cliente, sin el dato: nada sobre el saldo.
        await suena(page, api, 42);
        await expect(aviso(page).getByRole("list", { name: "Datos del cliente" })).toContainText("Fibra 100 Mb");
        await page.waitForTimeout(400);
        await expect(aviso(page).locator('[data-slot="saldo-vencido"]')).toHaveCount(0);
        // Si el detalle no se puede leer, tampoco se inventa.
        await aviso(page).getByRole("button", { name: "Rechazar", exact: true }).click();
        await page.route("**/api/v1/mensajeria/llamadas/0199c0de-*", (r) => (r.request().method() === "GET" ? r.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error del servidor", status: 500 }) }) : r.fallback()));
        await suena(page, api);
        await page.waitForTimeout(400);
        await expect(aviso(page).locator('[data-slot="saldo-vencido"]')).toHaveCount(0);
    });

    test("quien supervisa puede «Rechazar para todos», con confirmación; quien solo atiende no lo ve", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        await aviso(page).getByRole("button", { name: "Rechazar para todos" }).click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Rechazar la llamada para todos?" });
        await expect(dialogo).toContainText("Deja de sonarles a todas las personas");
        await sinDesbordes(page);
        await captura(page, "rechazar-para-todos");
        await dialogo.getByRole("button", { name: "Dejar que suene" }).click();
        await expect(aviso(page)).toHaveAttribute("data-fase", "sonando");
        expect(acciones(api, "rechazar")).toHaveLength(0);
        await aviso(page).getByRole("button", { name: "Rechazar para todos" }).click();
        await dialogo.getByRole("button", { name: "Rechazar para todos" }).click();
        await expect(page.getByText("Llamada de Carlos Ruiz rechazada para todos")).toBeVisible();
        await expect(aviso(page)).toHaveCount(0);
        expect(acciones(api, "rechazar").map((a) => a.cuerpo)).toEqual([{ paraTodos: true }]);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("rechazada");
    });

    test("la reserva es de otro navegador mío (409 `otro_navegador`): se dice y aquí no se suelta ni se cuelga", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        api.voz.forzar.contestar = "otro_navegador";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "otro_navegador");
        await expect(aviso(page)).toContainText("La estás contestando en otro navegador");
        await page.clock.fastForward(5_000);
        await page.waitForTimeout(300);
        expect(acciones(api, "soltar")).toHaveLength(0);
        expect(acciones(api, "colgar")).toHaveLength(0);
        // La reserva (del otro navegador) sigue intacta.
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("conectando");
        expect(await page.evaluate(() => window.__voz.microfonoAbierto())).toBe(false);
        await sinDesbordes(page);
    });

    test("otra persona la contesta mientras suena: deja de sonar y dice quién", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        await feedListo(page, api);
        api.voz.contestaOtro(llamada.id);
        await expect(aviso(page)).toHaveAttribute("data-fase", "aviso");
        await expect(aviso(page)).toContainText("La contestó Rosa Sánchez");
        await expect(aviso(page).getByRole("button", { name: "Contestar" })).toHaveCount(0);
        await sinDesbordes(page);
        await captura(page, "otro-la-contesto");
        // El mensaje se va solo.
        await page.clock.fastForward(8_500);
        await expect(aviso(page)).toHaveCount(0);
        expect(acciones(api, "reclamar")).toHaveLength(0);
    });

    test("pierdo la carrera al contestar (409 `llamada_ya_contestada`): mensaje llano, sin micrófono abierto ni reserva que soltar", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        api.voz.forzar.reclamar = "otro";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "otro_la_contesto");
        await expect(aviso(page)).toContainText("La contestó Rosa Sánchez.");
        await expect(barra(page)).toHaveCount(0);
        expect(acciones(api, "soltar")).toHaveLength(0);
        expect(await page.evaluate(() => ({ pedidos: window.__voz.pedidos, conexiones: window.__voz.conexiones.length }))).toEqual({ pedidos: 0, conexiones: 0 });
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();
        await expect(aviso(page)).toHaveCount(0);
    });

    test("WhatsApp aún responde a otra acción (409 `accion_en_curso`): se reintenta sin alarmar y conecta; ya tengo otra llamada: se dice", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        api.voz.forzar.enCurso = { reclamar: 2 };
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "reclamar").length).toBe(1);
        // Mientras reintenta sigue diciendo «Contestando…», sin error.
        await expect(aviso(page)).toContainText("Contestando…");
        await page.clock.fastForward(1_600);
        await expect.poll(() => acciones(api, "reclamar").length).toBe(2);
        await page.clock.fastForward(1_600);
        await expect(barra(page)).toBeVisible();
        expect(acciones(api, "reclamar")).toHaveLength(3);
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await nota(page).getByRole("button", { name: "Ahora no" }).click();

        // 409 `llamada_en_curso`: ya tengo otra viva en otro navegador.
        await suena(page, api, 42);
        api.voz.forzar.reclamar = "otra_llamada";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "otra_llamada");
        await expect(aviso(page)).toContainText("Ya tienes otra llamada");
        expect(acciones(api, "soltar")).toHaveLength(0);
        await sinDesbordes(page);
    });

    test("la tengo en otra pestaña (regla `misma_persona`): aquí solo se cierra el aviso", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        api.voz.forzar.reclamar = "misma_persona";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveCount(0);
        await expect(barra(page)).toHaveCount(0);
        expect(acciones(api, "reclamar")).toHaveLength(1);
        expect(acciones(api, "soltar")).toHaveLength(0);
    });

    test("suena en dos pestañas del mismo usuario y solo una la contesta", async ({ page, context }) => {
        const api = await abrir(page, "/cuenta");
        const segunda = await context.newPage();
        await segunda.clock.install();
        await instalarDobleVoz(segunda, { permiso: "granted", medio: "nunca" });
        const api2 = await simularApi(segunda, { conversaciones: { flujoLargo: true }, voz: { recibe: true } });
        await segunda.goto("/cuenta");
        // Las dos pestañas hablan con el mismo «servidor»: la llamada entra en los dos.
        await feedListo(page, api);
        await feedListo(segunda, api2);
        const llamada = api.voz.entrante(41);
        const gemela = api2.voz.entrante(41);
        await expect(aviso(page)).toHaveAttribute("data-fase", "sonando");
        await expect(aviso(segunda)).toHaveAttribute("data-fase", "sonando");
        expect(gemela.id).toBe(llamada.id);
        // En la primera el medio no conecta todavía: se queda contestando, con el candado tomado.
        await ajustarVoz(page, { medio: "nunca" });
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-fase", "contestando");
        await expect.poll(() => acciones(api, "contestar").length).toBe(1);
        // La segunda intenta contestar la misma llamada: el candado del navegador no la deja.
        await aviso(segunda).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(segunda)).toHaveCount(0);
        expect(acciones(api2, "reclamar")).toHaveLength(0);
        expect(await segunda.evaluate(() => window.__voz.conexiones.length)).toBe(0);
        await segunda.close();
    });
});

test.describe("voz › plazos, red y cierre", () => {
    test("pasan los 5 s para confirmar el medio: se suelta la reserva y se dice en llano", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { doble: { permiso: "granted", medio: "nunca" } });
        await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toContainText("Conectando el audio…");
        await expect.poll(() => acciones(api, "contestar").length).toBe(1);
        // A mitad del plazo sigue intentando; pasado, se rinde.
        await page.clock.fastForward(2_500);
        await expect(aviso(page)).toHaveAttribute("data-fase", "contestando");
        await page.clock.fastForward(2_700);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "reserva_vencida");
        await expect(aviso(page)).toContainText("No se alcanzó a conectar");
        await expect.poll(() => acciones(api, "soltar").length).toBe(1);
        expect(acciones(api, "confirmar-medio")).toHaveLength(0);
        expect(await page.evaluate(() => ({ abiertas: window.__voz.conexiones.filter((c) => !c.cerrada).length, microfono: window.__voz.microfonoAbierto() }))).toEqual({ abiertas: 0, microfono: false });
        await sinDesbordes(page);
    });

    test("al soltar, WhatsApp aún no responde (409 `accion_en_curso`): se reintenta solo hasta soltarla", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { doble: { permiso: "granted", medio: "falla" } });
        const llamada = await suena(page, api);
        api.voz.forzar.enCurso = { soltar: 1 };
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        await expect.poll(() => acciones(api, "soltar").length).toBe(1);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("conectando");
        await page.clock.fastForward(2_100);
        await expect.poll(() => acciones(api, "soltar").length).toBe(2);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("sonando");
        // A la persona solo se le dijo lo primero: el reintento no agrega otro error.
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        await expect(page.getByText(/WhatsApp aún no responde|accion_en_curso/)).toHaveCount(0);
    });

    test("pasan los 12 s de la reserva sin llegar a responder (el micrófono no contesta): se suelta", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        await ajustarVoz(page, { microfono: "colgado" });
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toContainText("Abriendo el micrófono…");
        await captura(page, "conectando");
        await page.clock.fastForward(5_000);
        await expect(aviso(page)).toHaveAttribute("data-fase", "contestando");
        await page.clock.fastForward(7_200);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "reserva_vencida");
        await expect.poll(() => acciones(api, "soltar").length).toBe(1);
        expect(acciones(api, "contestar")).toHaveLength(0);
    });

    test("el servidor dice que la reserva venció, el micrófono se niega al contestar, el medio falla o no hay red: cada uno con su mensaje", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        // 409 `reserva_vencida` en `contestar`.
        await suena(page, api);
        api.voz.forzar.contestar = "reserva_vencida";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "reserva_vencida");
        await expect.poll(() => acciones(api, "soltar").length).toBe(1);
        delete api.voz.forzar.contestar;
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();

        // El micrófono se niega justo al contestar.
        await suena(page, api, 42);
        await ajustarVoz(page, { microfono: "denegado" });
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "microfono_denegado");
        await expect(aviso(page)).toContainText("El navegador no dio permiso para el micrófono");
        await expect.poll(() => acciones(api, "soltar").length).toBe(2);
        await ajustarVoz(page, { microfono: "ok" });
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();

        // El medio no logra conectar (red que bloquea el audio).
        await suena(page, api, 43);
        await ajustarVoz(page, { medio: "falla" });
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        await expect(aviso(page)).toContainText("No se pudo conectar el audio");
        await expect.poll(() => acciones(api, "soltar").length).toBe(3);
        await ajustarVoz(page, { medio: "conecta" });
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();

        // Sin red al reclamar: no hay reserva que soltar.
        await suena(page, api, 44);
        api.voz.forzar.reclamar = "sin_red";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_red");
        await expect(aviso(page)).toContainText("Se perdió la conexión");
        expect(acciones(api, "soltar")).toHaveLength(3);
        await sinDesbordes(page);
    });

    test("se cae el medio a mitad de la llamada: termina, lo dice y avisa al servidor", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        await page.evaluate(() => window.__voz.cortar());
        await expect(barra(page)).toHaveCount(0);
        await expect(nota(page)).toContainText("La llamada se cortó");
        await expect.poll(() => acciones(api, "colgar").length).toBe(1);
        await sinDesbordes(page);
        await captura(page, "corte");
    });

    test("el cliente cuelga: la barra se va y se ofrece la nota; si colgar falla en el servidor, «Reintentar»", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        await feedListo(page, api);
        api.voz.cuelgaCliente(llamada.id);
        await expect(barra(page)).toHaveCount(0);
        await expect(nota(page)).toBeVisible();
        expect(acciones(api, "colgar")).toHaveLength(0);
        await nota(page).getByRole("button", { name: "Ahora no" }).click();

        // Otra: cuelgo yo y el servidor falla. Se reintenta solo (1, 2, 4, 8 y 8 s) sin decir nada…
        await suena(page, api, 42);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        api.voz.forzar.colgar = 2;
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await expect(nota(page)).toBeVisible();
        await expect.poll(() => acciones(api, "colgar").length).toBe(1);
        await adelantarHasta(page, () => api.voz.llamadas.get([...api.voz.llamadas.keys()].at(-1) ?? "")?.estado === "terminada");
        expect(acciones(api, "colgar")).toHaveLength(3);
        await expect(nota(page)).not.toContainText("No se pudo avisar que colgaste");
        await nota(page).getByRole("button", { name: "Ahora no" }).click();

        // …y si no hay manera (6 intentos), lo dice con «Reintentar».
        await suena(page, api, 43);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        api.voz.forzar.colgar = 6;
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await expect(nota(page)).toBeVisible();
        const antes = acciones(api, "colgar").length;
        await adelantarHasta(page, () => api.voz.forzar.colgar === 0);
        expect(acciones(api, "colgar").length - antes).toBeLessThanOrEqual(6);
        await expect(nota(page)).toContainText("No se pudo avisar que colgaste");
        await nota(page).getByRole("button", { name: "Reintentar" }).click();
        await expect(nota(page)).not.toContainText("No se pudo avisar que colgaste");
        await expect.poll(() => api.voz.llamadas.get([...api.voz.llamadas.keys()].at(-1) ?? "")?.estado).toBe("terminada");
    });

    test("al cerrarse la pestaña: `colgar` si estoy hablando y `soltar` si aún no conectaba (con keepalive)", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
        await expect.poll(() => acciones(api, "colgar").length).toBe(1);
        expect(acciones(api, "colgar")[0]?.cuerpo).toEqual({});

        await page.reload();
        await suena(page, api, 42);
        await ajustarVoz(page, { medio: "nunca" });
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        // La respuesta SDP de esta segunda llamada ya salió: la reserva es mía y aún no conecta.
        await expect.poll(() => acciones(api, "contestar").length).toBe(2);
        await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
        await expect.poll(() => acciones(api, "soltar").map((a) => a.cuerpo)).toEqual([{ motivo: "pestana_cerrada" }]);
    });

    test("al volver a abrir el panel con una llamada mía viva: lo dice y ofrece colgarla (el audio no se retoma)", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        // Se cayó el navegador (no alcanzó a avisar al cerrarse): el servidor aún la tiene en curso.
        await page.evaluate(() => window.addEventListener("pagehide", (e) => e.stopImmediatePropagation(), true));
        await page.reload();
        expect(acciones(api, "colgar")).toHaveLength(0);
        await expect(page.getByText("Tenías una llamada con Carlos Ruiz")).toBeVisible();
        await expect(barra(page)).toHaveCount(0);
        await page.getByRole("button", { name: "Colgar" }).click();
        await expect.poll(() => api.voz.llamadas.get(llamada.id)?.estado).toBe("terminada");
    });
});

test.describe("voz › nunca una llamada viva sin audio (revisión de seguridad)", () => {
    const ultima = (api: Api) => api.voz.llamadas.get([...api.voz.llamadas.keys()].at(-1) ?? "");
    const medios = (page: Page) => page.evaluate(() => ({ abiertas: window.__voz.conexiones.filter((c) => !c.cerrada).length, microfono: window.__voz.microfonoAbierto() }));

    test("con el medio conectado, el plazo de 5 s ya no corta `confirmar-medio` en vuelo: al responder, la llamada queda en curso con audio", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        const liberar = api.voz.retener("confirmar-medio");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "confirmar-medio").length).toBe(1);
        // Pasan los 5 s (y algo más) con la confirmación sin responder: no se cierra nada.
        await page.clock.fastForward(6_000);
        await expect(aviso(page)).toHaveAttribute("data-fase", "contestando");
        expect(await medios(page)).toEqual({ abiertas: 1, microfono: true });
        expect(acciones(api, "soltar")).toHaveLength(0);
        liberar();
        await expect(barra(page)).toBeVisible();
        expect(ultima(api)?.estado).toBe("en_curso");
        expect(await medios(page)).toEqual({ abiertas: 1, microfono: true });
    });

    test("`confirmar-medio` no responde (límite propio de 8 s): se cierra el audio y se CUELGA en el servidor; si el 200 llega después, la llamada ya no queda viva", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        const liberar = api.voz.retener("confirmar-medio");
        // El primer `colgar` falla: el servidor aún no la había aceptado del todo.
        api.voz.forzar.colgar = 1;
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "confirmar-medio").length).toBe(1);
        await page.clock.fastForward(8_200);
        await expect(aviso(page)).toHaveAttribute("data-fase", "aviso");
        expect(await medios(page)).toEqual({ abiertas: 0, microfono: false });
        // No se manda `soltar` (el servidor lo rechazaría con la llamada ya aceptada): se cuelga.
        await expect.poll(() => acciones(api, "colgar").length).toBe(1);
        expect(acciones(api, "soltar")).toHaveLength(0);
        // Ahora sí responde el 200 de la confirmación: la llamada queda `en_curso` sin nadie al audio…
        liberar();
        await expect.poll(() => ultima(api)?.estado).toBe("en_curso");
        // …y el reintento de colgar la termina. Se le dice al agente.
        await adelantarHasta(page, () => ultima(api)?.estado === "terminada");
        await expect(page.getByText("La llamada se conectó sin audio y se colgó")).toBeVisible();
        await expect(barra(page)).toHaveCount(0);
    });

    test("se pierde la respuesta de `confirmar-medio` (el servidor sí aceptó): se cuelga y se dice", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        api.voz.forzar.confirmar = "perdida";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-fase", "aviso");
        await expect.poll(() => ultima(api)?.estado).toBe("terminada");
        expect(acciones(api, "colgar")).toHaveLength(1);
        expect(acciones(api, "soltar")).toHaveLength(0);
        expect(await medios(page)).toEqual({ abiertas: 0, microfono: false });
        await expect(page.getByText("La llamada se conectó sin audio y se colgó")).toBeVisible();
    });

    test("`soltar` falla sin ser `accion_en_curso`: se comprueba que la llamada sigue a mi nombre y se cuelga; si no se logra, aviso fijo con «Reintentar»", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { doble: { permiso: "granted", medio: "falla" } });
        await suena(page, api);
        api.voz.forzar.soltar = "error";
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        await expect.poll(() => ultima(api)?.estado).toBe("terminada");
        expect(acciones(api, "soltar")).toHaveLength(1);
        expect(acciones(api, "colgar")).toHaveLength(1);
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();

        // Ni soltar ni colgar salen: no se calla.
        await suena(page, api, 42);
        api.voz.forzar.colgar = 99;
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        // 6 intentos y se rinde (1 de la primera llamada + 6).
        await adelantarHasta(page, () => acciones(api, "colgar").length === 7);
        const fijo = page.getByText("No se pudo colgar una llamada que quedó abierta");
        await expect(fijo).toBeVisible();
        api.voz.forzar.colgar = 0;
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect.poll(() => ultima(api)?.estado).toBe("terminada");
    });

    test("`soltar` de una reserva que ya tomó otra persona no cuelga la llamada de un compañero (aunque yo supervise)", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { doble: { permiso: "granted", medio: "falla" } });
        const llamada = await suena(page, api);
        api.voz.forzar.soltar = "error";
        // Entre mi reserva y mi `soltar`, la reserva venció y la contestó Rosa.
        const liberar = api.voz.retener("contestar");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "contestar").length).toBe(1);
        const real = api.voz.llamadas.get(llamada.id);
        if (real) Object.assign(real, { estado: "en_curso", usuario: { id: 9, nombre: "Rosa Sánchez" }, contestadaEn: new Date().toISOString() });
        liberar();
        await expect(aviso(page)).toHaveAttribute("data-fase", "aviso");
        await page.clock.fastForward(30_000);
        await page.waitForTimeout(300);
        expect(acciones(api, "colgar")).toHaveLength(0);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("en_curso");
    });

    test("`reclamar` no responde (límite de 8 s): se deja de esperar; si después responde 200, se suelta la reserva. Y al cerrar la pestaña con `reclamar` en vuelo, `soltar`", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        const liberar = api.voz.retener("reclamar");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "reclamar").length).toBe(1);
        await page.clock.fastForward(7_000);
        await expect(aviso(page)).toContainText("Contestando…");
        await page.clock.fastForward(1_200);
        await expect(aviso(page)).toHaveAttribute("data-fase", "aviso");
        expect(acciones(api, "soltar")).toHaveLength(0);
        // El 200 llega tarde: la reserva quedó a mi nombre sin nadie esperándola → se suelta.
        liberar();
        await expect.poll(() => acciones(api, "soltar").length).toBe(1);
        await expect.poll(() => api.voz.llamadas.get(llamada.id)?.estado).toBe("sonando");
        expect(await page.evaluate(() => window.__voz.pedidos)).toBe(0);

        // Cierre de pestaña con `reclamar` en vuelo.
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();
        await suena(page, api, 42);
        api.voz.retener("reclamar");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "reclamar").length).toBe(2);
        await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
        await expect.poll(() => acciones(api, "soltar").at(-1)?.cuerpo).toEqual({ motivo: "pestana_cerrada" });
        expect(acciones(api, "soltar")).toHaveLength(2);
    });

    test("al cerrar la pestaña con `confirmar-medio` en vuelo se manda `colgar`, no `soltar`", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        api.voz.retener("confirmar-medio");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "confirmar-medio").length).toBe(1);
        await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
        await expect.poll(() => acciones(api, "colgar").length).toBe(1);
        expect(acciones(api, "soltar")).toHaveLength(0);
    });

    test("sin candado del navegador (`navigator.locks`) no se manda `soltar`: no se sabe si mi `reclamar` fue el que ganó", async ({ page }) => {
        await page.addInitScript(() => Object.defineProperty(navigator, "locks", { configurable: true, value: undefined }));
        const api = await abrir(page, "/cuenta", { doble: { permiso: "granted", medio: "falla" } });
        await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        await page.clock.fastForward(10_000);
        await page.waitForTimeout(300);
        expect(acciones(api, "soltar")).toHaveLength(0);
        expect(acciones(api, "colgar")).toHaveLength(0);
        expect(await medios(page)).toEqual({ abiertas: 0, microfono: false });
    });

    test("el medio se cae mientras se confirma: se deja la llamada y se cuelga", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await suena(page, api);
        const liberar = api.voz.retener("confirmar-medio");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect.poll(() => acciones(api, "confirmar-medio").length).toBe(1);
        await page.evaluate(() => window.__voz.cortar());
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        liberar();
        await adelantarHasta(page, () => ultima(api)?.estado === "terminada");
        expect(acciones(api, "soltar")).toHaveLength(0);
    });

    test("cerrar sesión con una llamada sonando: pregunta antes; «Quedarme» no hace nada y «Seguir» deja de sonarme ANTES de cerrar la sesión", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const cerrarSesion = async () => {
            if (esTelefono(page)) {
                await abrirMenu(page);
                await hojaMas(page).getByRole("menuitem", { name: "Cerrar sesión" }).click();
            } else {
                await page.getByRole("button", { name: /^Tu cuenta/ }).click();
                await page.getByRole("menuitem", { name: "Cerrar sesión" }).click();
            }
        };
        // Sonando: deja de sonarme a mí (rechazar) antes de salir… pero primero «Quedarme».
        const llamada = await suena(page, api);
        await cerrarSesion();
        const sonando = page.getByRole("alertdialog", { name: "Tienes una llamada entrando" });
        await sinDesbordes(page);
        await captura(page, "salir-con-llamada");
        await sonando.getByRole("button", { name: "Quedarme" }).click();
        await expect(sonando).toHaveCount(0);
        await expect(aviso(page)).toHaveAttribute("data-fase", "sonando");
        expect(api.peticiones.some((p) => p.metodo === "DELETE" && p.ruta === "/api/v1/sesion")).toBe(false);
        // Ahora sí: deja de sonarme (rechazar) y después se cierra la sesión.
        await cerrarSesion();
        await sonando.getByRole("button", { name: "Seguir" }).click();
        await expect(page).toHaveURL(/\/entrar/);
        const rutas = api.peticiones.map((p) => `${p.metodo} ${p.ruta}`);
        const rechazo = rutas.findIndex((r) => r.endsWith(`/llamadas/${llamada.id}/rechazar`));
        expect(rechazo).toBeGreaterThan(-1);
        expect(rutas.indexOf("DELETE /api/v1/sesion")).toBeGreaterThan(rechazo);
        // Solo dejó de sonarme a mí: la llamada sigue para los demás.
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("sonando");
    });

    test("cerrar sesión hablando: «Colgar y seguir» manda `colgar` antes del `DELETE /sesion`", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const llamada = await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        // Al contestar se abrió la conversación; se vuelve al inicio (en el teléfono, ahí está «Más»).
        await page.getByRole("link", { name: "Iris, inicio" }).click();
        await expect(page).toHaveURL(/\/$/);
        if (esTelefono(page)) {
            await abrirMenu(page);
            await hojaMas(page).getByRole("menuitem", { name: "Cerrar sesión" }).click();
        } else {
            await page.getByRole("button", { name: /^Tu cuenta/ }).click();
            await page.getByRole("menuitem", { name: "Cerrar sesión" }).click();
        }
        const dialogo = page.getByRole("alertdialog", { name: "Tienes una llamada con Carlos Ruiz" });
        await expect(dialogo).toContainText("Si sigues, la llamada se cuelga.");
        await dialogo.getByRole("button", { name: "Colgar y seguir" }).click();
        await expect(page).toHaveURL(/\/entrar/);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("terminada");
        const rutas = api.peticiones.map((p) => `${p.metodo} ${p.ruta}`);
        const colgar = rutas.findIndex((r) => r.startsWith("POST") && r.endsWith("/colgar"));
        const salir = rutas.indexOf("DELETE /api/v1/sesion");
        expect(colgar).toBeGreaterThan(-1);
        expect(salir).toBeGreaterThan(colgar);
    });

    test("cambiar de empresa hablando: pregunta antes y `colgar` viaja con la empresa de la llamada, no con la nueva", async ({ page }) => {
        const api = await abrir(page, "/tickets", { opciones: { empresas: EMPRESAS } });
        await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
        await expect(page.getByText("Sin internet desde la mañana").filter({ visible: true })).toBeVisible();
        const llamada = await suena(page, api);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        expect(api.peticiones.find((p) => p.ruta.endsWith("/reclamar"))?.empresa).toBe("10");
        // Al contestar se abrió la conversación; se vuelve al inicio (en el teléfono, ahí está «Más»).
        await page.getByRole("link", { name: "Iris, inicio" }).click();
        await expect(page).toHaveURL(/\/$/);
        await expect(barra(page)).toBeVisible();

        await elegirOpcion(await selectorEmpresa(page), { label: "Fibra Sur" });
        const dialogo = page.getByRole("alertdialog", { name: "Tienes una llamada con Carlos Ruiz" });
        await dialogo.getByRole("button", { name: "Quedarme" }).click();
        await expect(barra(page)).toBeVisible();
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("en_curso");
        expect(api.peticiones.some((p) => p.empresa === "20")).toBe(false);

        await elegirOpcion(await selectorEmpresa(page), { label: "Fibra Sur" });
        await dialogo.getByRole("button", { name: "Colgar y seguir" }).click();
        await expect(barra(page)).toHaveCount(0);
        await expect.poll(() => api.peticiones.some((p) => p.empresa === "20")).toBe(true);
        expect(api.voz.llamadas.get(llamada.id)?.estado).toBe("terminada");
        const colgar = api.peticiones.findIndex((p) => p.ruta.endsWith("/colgar"));
        expect(api.peticiones[colgar]?.empresa).toBe("10");
        expect(colgar).toBeLessThan(api.peticiones.findIndex((p) => p.empresa === "20"));
    });

    test("el timbre tiene tope: sin noticias de la llamada, a los 60 s deja de sonar aquí", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await feedListo(page, api);
        // Una llamada sin `suenaHasta` creíble (reloj del equipo): vale el tope de 60 s.
        api.voz.entrante(41, { suenaHasta: new Date(Date.now() + 3_600_000).toISOString() });
        await expect(aviso(page)).toHaveAttribute("data-timbre", "sonando");
        await page.clock.fastForward(59_000);
        await expect(aviso(page)).toHaveAttribute("data-fase", "sonando");
        await page.clock.fastForward(1_500);
        await expect(aviso(page)).toHaveCount(0);
        expect(acciones(api, "reclamar")).toHaveLength(0);
    });

    test("con el permiso del micrófono en «preguntar», el aviso de la llamada lo anticipa", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { doble: { permiso: "prompt" } });
        await suena(page, api);
        await expect(aviso(page).locator('[data-slot="aviso-permiso"]')).toContainText("el navegador te va a pedir permiso para usar el micrófono");
        await sinDesbordes(page);
        await captura(page, "entrante-permiso");
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await nota(page).getByRole("button", { name: "Ahora no" }).click();
        // Ya concedido: la siguiente llamada no lo repite.
        await suena(page, api, 42);
        await expect(aviso(page).locator('[data-slot="aviso-permiso"]')).toHaveCount(0);
    });
});

test.describe("voz › en el hilo y en la lista", () => {
    test("cada llamada del hilo con su icono, texto y color; solo la perdida pide actuar", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: {} });
        api.voz.historica(41, "terminada", { atendidaPor: "persona", usuario: { id: 9, nombre: "Rosa Sánchez" }, duracionSeg: 185, nota: "Quedó navegando." });
        api.voz.historica(41, "rechazada", { usuario: { id: 9, nombre: "Rosa Sánchez" } });
        api.voz.historica(41, "fuera_de_horario");
        api.voz.historica(41, "fallida");
        const perdida = api.voz.historica(41, "perdida", { duracionSeg: 25 });
        // El recado que dejó en el buzón de voz al llamar fuera de horario.
        api.conversaciones.mensajes.get(41)?.push(mensaje(41, { direccion: "entrante", tipo: "audio", texto: null, buzonDeVoz: true, creadoEn: new Date().toISOString() }));
        await page.goto("/conversaciones?c=41");
        const hilo = page.getByRole("log", { name: "Mensajes" });
        const tarjeta = (estado: string) => hilo.locator(`[data-slot="llamada-hilo"][data-estado="${estado}"]`);
        await expect(tarjeta("terminada")).toContainText("Llamada de WhatsApp");
        await expect(tarjeta("terminada")).toContainText("La atendió Rosa Sánchez · 3 min 5 s");
        await expect(tarjeta("terminada")).toHaveAttribute("data-tono", "ok");
        await expect(tarjeta("terminada").getByText("Tiene una nota")).toBeAttached();
        await expect(tarjeta("rechazada")).toContainText("La rechazó Rosa Sánchez");
        await expect(tarjeta("fuera_de_horario")).toContainText("Llamó fuera de horario");
        await expect(tarjeta("fuera_de_horario")).toHaveAttribute("data-tono", "aviso");
        await expect(tarjeta("fallida")).toContainText("La llamada no se pudo conectar");
        await expect(hilo.locator('[data-slot="etiqueta-buzon"]')).toHaveText("Mensaje de voz del buzón");
        await expect(tarjeta("perdida")).toContainText("Llamada perdida de WhatsApp");
        await expect(tarjeta("perdida")).toContainText("Sonó 25 s y nadie contestó");
        await expect(tarjeta("perdida")).toHaveAttribute("data-tono", "error");
        await expect(hilo.locator('[data-slot="llamada-hilo"][data-tono="error"]')).toHaveCount(1);
        // «Devolver la llamada» llega con las salientes: aquí se le escribe.
        await expect(hilo.getByRole("button", { name: /Devolver/ })).toHaveCount(0);
        await tarjeta("perdida").getByRole("button", { name: "Escribirle" }).click();
        // El cliente no escribe hace más de 24 h (una llamada no abre la ventana): el foco va a la plantilla.
        expect(await page.evaluate(() => Boolean(document.activeElement?.closest('[data-slot="compositor"] [data-slot="solo-plantilla"]')))).toBe(true);
        expect(perdida.estado).toBe("perdida");
        await tarjeta("perdida").scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "hilo-llamadas");
    });

    test("quien supervisa cuelga la llamada viva de otra persona desde el hilo, con confirmación; quien solo atiende no ve el botón", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: {} });
        const deRosa = api.voz.historica(41, "en_curso", { atendidaPor: "persona", usuario: { id: 9, nombre: "Rosa Sánchez" }, contestadaEn: new Date().toISOString() });
        delete api.voz.llamadas.get(deRosa.id)?.terminadaEn;
        await page.goto("/conversaciones?c=41");
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-estado="en_curso"]');
        await expect(tarjeta).toContainText("Con Rosa Sánchez");
        await tarjeta.getByRole("button", { name: "Colgar la llamada" }).click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Colgar la llamada de Rosa Sánchez?" });
        await sinDesbordes(page);
        await dialogo.getByRole("button", { name: "No colgar" }).click();
        expect(acciones(api, "colgar")).toHaveLength(0);
        await tarjeta.getByRole("button", { name: "Colgar la llamada" }).click();
        await dialogo.getByRole("button", { name: "Colgar la llamada" }).click();
        await expect(page.getByText("Llamada colgada")).toBeVisible();
        expect(api.voz.llamadas.get(deRosa.id)?.estado).toBe("terminada");

        // Quien solo atiende (sin `conversaciones.supervisar`): sin botón.
        const otra = api.voz.historica(42, "en_curso", { atendidaPor: "persona", usuario: { id: 9, nombre: "Rosa Sánchez" } });
        await conSesion(page.context(), test.info().project.use.baseURL ?? "", AGENTE_CONV.correo);
        await page.goto("/conversaciones?c=42");
        const ajena = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-estado="en_curso"]');
        await expect(ajena).toContainText("Con Rosa Sánchez");
        await expect(ajena.getByRole("button", { name: "Colgar la llamada" })).toHaveCount(0);
        expect(api.voz.llamadas.get(otra.id)?.estado).toBe("en_curso");
    });

    test("llamada perdida: la fila la marca, se junta arriba como «por devolver» y se descarta con motivo", async ({ page }) => {
        const api = await abrir(page, "/conversaciones", { voz: { devoluciones: [41, 42] } });
        const fila = (id: number) => page.locator(`li:has([data-conversacion="${id}"])`);
        await expect(fila(41).locator('[data-slot="chip-perdida"]')).toContainText("Llamada perdida");
        await expect(fila(42).locator('[data-slot="chip-perdida"]')).toBeVisible();
        await expect(fila(43).locator('[data-slot="chip-perdida"]')).toHaveCount(0);
        const porDevolver = page.getByRole("region", { name: "Llamadas por devolver" });
        await porDevolver.getByRole("button", { name: "2 llamadas por devolver" }).click();
        await expect(porDevolver.getByRole("listitem")).toHaveCount(2);
        await expect(porDevolver).toContainText("Carlos Ruiz");
        await expect(porDevolver.getByRole("link", { name: "Escribirle" }).first()).toHaveAttribute("href", /c=41/);
        await sinDesbordes(page);
        await captura(page, "perdidas-lista");

        await porDevolver.locator("li", { hasText: "Carlos Ruiz" }).getByRole("button", { name: "Descartar" }).click();
        const dialogo = page.getByRole("dialog", { name: "Descartar la llamada por devolver" });
        await expect(dialogo.getByRole("button", { name: "Descartar" })).toBeDisabled();
        await dialogo.getByLabel("Motivo").fill("Ya le escribí por WhatsApp");
        await dialogo.getByRole("button", { name: "Descartar" }).click();
        await expect(page.getByText("Llamada por devolver descartada")).toBeVisible();
        expect(api.voz.recibido.descartadas.map((d) => d.motivo)).toEqual(["Ya le escribí por WhatsApp"]);
        await expect(porDevolver.getByRole("button", { name: "1 llamada por devolver" })).toBeVisible();
        await expect(fila(41).locator('[data-slot="chip-perdida"]')).toHaveCount(0);

        // La otra ya la cerró alguien más (409 `devolucion_cerrada`): se dice y sale de la lista.
        api.voz.forzar.descartar = "cerrada";
        await porDevolver.getByRole("button", { name: "Descartar" }).click();
        await dialogo.getByLabel("Motivo").fill("Número equivocado");
        await dialogo.getByRole("button", { name: "Descartar" }).click();
        await expect(page.getByText("Ya estaba cerrada")).toBeVisible();
        await expect(porDevolver).toHaveCount(0);
    });

    test("en la conversación, la perdida pendiente se descarta desde su tarjeta; sin `llamadas.ver` no se lee nada de devoluciones", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { voz: { devoluciones: [41] } });
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-estado="perdida"]');
        await tarjeta.getByRole("button", { name: "Descartar" }).click();
        await page.getByRole("dialog", { name: "Descartar la llamada por devolver" }).getByLabel("Motivo").fill("Ya se resolvió");
        await page.getByRole("dialog", { name: "Descartar la llamada por devolver" }).getByRole("button", { name: "Descartar" }).click();
        await expect(tarjeta.getByRole("button", { name: "Descartar" })).toHaveCount(0);
        await expect(tarjeta.getByRole("button", { name: "Escribirle" })).toBeVisible();
        expect(api.voz.recibido.descartadas).toHaveLength(1);

        // Si la lectura de devoluciones falla: error con «Reintentar», nunca «0 por devolver».
        await page.route("**/api/v1/mensajeria/llamadas/devoluciones", (r) => r.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error del servidor", status: 500 }) }), { times: 1 });
        await page.goto("/conversaciones");
        await expect(page.getByText("No se pudieron leer las llamadas por devolver")).toBeVisible();
        await page.getByRole("region", { name: "Conversaciones", exact: true }).getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText("No se pudieron leer las llamadas por devolver")).toHaveCount(0);
    });

    test("quien atiende sin administrar (agente) recibe y contesta; no ve los ajustes", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { correo: AGENTE_CONV.correo });
        await suena(page, api, 42);
        // Sin `conversaciones.supervisar` no hay «Rechazar para todos».
        await expect(aviso(page).getByRole("button", { name: "Rechazar para todos" })).toHaveCount(0);
        await aviso(page).getByRole("button", { name: "Contestar" }).click();
        await expect(barra(page)).toBeVisible();
        await barra(page).getByRole("button", { name: "Colgar" }).click();
        await nota(page).getByRole("button", { name: "Ahora no" }).click();
        await page.goto("/administracion/llamadas");
        await expect(page.getByRole("heading", { name: "Ajustes de llamadas" })).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta.includes("/voz/ajustes"))).toBe(false);
    });
});

test.describe("voz › ajustes", () => {
    test("por número: requisitos sin cumplir en llano, comprobar, encender y guardar lo que el servidor espera", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros", { voz: { sinRequisitos: true } });
        await expect(page.getByRole("link", { name: "Llamadas de Soporte" })).toBeVisible();
        await sinDesbordes(page);
        await captura(page, "numeros-lista");
        await page.getByRole("link", { name: "Llamadas de Soporte" }).click();
        await expect(page).toHaveURL(/\/administracion\/numeros\/3\/llamadas$/);
        const f = page.getByRole("form", { name: "Llamadas de Soporte" });
        const requisitos = f.getByRole("list", { name: "Requisitos" });
        await expect(requisitos.locator('[data-requisito="limite_mensajeria"]')).toContainText("Falta: Puede escribirle a 2,000 personas al día o más");
        await expect(requisitos.locator('[data-requisito="limite_mensajeria"]')).toContainText("Hoy puede escribirle a 250 personas al día");
        await expect(requisitos.locator('[data-requisito="conexion_api"]')).toContainText("Listo:");
        // Lo que Iris no puede leer de Meta nunca se muestra como cumplido.
        await expect(requisitos.locator('[data-requisito="webhook_calls"]')).toContainText("Sin comprobar: Meta le avisa a Iris de las llamadas");
        await expect(requisitos.locator('[data-requisito="webhook_calls"]')).toContainText("No impide encender");
        await expect(requisitos.locator('[data-requisito="plantilla_permiso"]')).toContainText("Recomendado:");
        const encender = f.getByRole("switch", { name: "Este número recibe llamadas de WhatsApp" });
        await expect(encender).toBeDisabled();
        await expect(f).toContainText("Falta cumplir los requisitos de Meta de arriba.");
        // La grabación no se ofrece.
        await expect(f).not.toContainText(/[Gg]rab/);
        await sinDesbordes(page);
        await captura(page, "ajustes-numero-requisitos");

        await f.getByRole("button", { name: "Comprobar otra vez" }).click();
        await expect(page.getByText("El número cumple los requisitos")).toBeVisible();
        await expect(requisitos).toHaveAttribute("data-cumple", "si");
        await expect(encender).toBeEnabled();
        await encender.click();
        await elegirOpcion(f.getByLabel("Cola que contesta"), "Soporte");
        await f.getByLabel("Segundos que suena").fill("20");
        await f.getByLabel("Segundos que suena").blur();
        await elegirOpcion(f.getByLabel("Si llaman fuera de horario"), "Que deje un mensaje en el buzón de voz de WhatsApp");
        // Con cambios sin guardar no se consulta a Meta (se perderían).
        await expect(f.getByRole("button", { name: "Leer ajustes de Meta" })).toBeDisabled();
        await sinDesbordes(page);
        await captura(page, "ajustes-numero");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Soporte ya recibe llamadas")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/numeros$/);
        expect(api.voz.recibido.configurados).toEqual([
            {
                canalId: 3,
                cuerpo: {
                    version: 2,
                    llamadasActivas: true,
                    colaId: 2,
                    segundosSonando: 20,
                    fueraDeHorario: "buzon",
                    iconoVisible: true,
                    devolucion: false,
                    mensajePerdida: { activo: true, texto: "No pudimos contestarte. Escríbenos por aquí y te atendemos." },
                },
            },
        ]);
    });

    test("por número: validación espejo, 422 de requisitos del servidor, 409 de versión y leer de Meta", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/3/llamadas");
        const f = page.getByRole("form", { name: "Llamadas de Soporte" });
        const mensaje = f.getByRole("textbox", { name: "Mensaje", exact: true });
        // El campo no deja pasar de 45 s (el servidor tampoco).
        await f.getByLabel("Segundos que suena").fill("90");
        await f.getByLabel("Segundos que suena").blur();
        await expect(f.getByLabel("Segundos que suena")).toHaveValue("45");
        await mensaje.fill("");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f).toContainText("Escribe el mensaje");
        expect(api.voz.recibido.configurados).toHaveLength(0);
        await mensaje.fill("Te devolvemos el mensaje pronto.");

        // Meta dejó de cumplir entre que se abrió y se guardó: el servidor lo dice con cada requisito.
        const canal = api.voz.canal(3);
        canal.requisitos = { cumple: false, requisitos: [{ codigo: "calling_habilitado", cumple: false, aviso: false, detalle: "Enciende las llamadas del número en WhatsApp Manager." }] };
        await f.getByRole("switch", { name: "Este número recibe llamadas de WhatsApp" }).click();
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f).toContainText("Todavía no se pueden encender las llamadas");
        await expect(f).toContainText("Enciende las llamadas del número en WhatsApp Manager.");
        await f.getByRole("switch", { name: "Este número recibe llamadas de WhatsApp" }).click();

        // Otra persona lo cambió: 409.
        canal.version += 1;
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f).toContainText("Alguien más cambió las llamadas de este número");
        await sinDesbordes(page);

        // Al reabrir, «Leer ajustes de Meta» trae lo que hay allá (el botón de llamar, apagado).
        await page.reload();
        const g = page.getByRole("form", { name: "Llamadas de Soporte" });
        await expect(g.getByRole("switch", { name: "Mostrar el botón de llamar en WhatsApp" })).toBeChecked();
        await g.getByRole("button", { name: "Leer ajustes de Meta" }).click();
        await expect(page.getByText("Ajustes leídos de Meta", { exact: true })).toBeVisible();
        await expect(g.getByRole("switch", { name: "Mostrar el botón de llamar en WhatsApp" })).not.toBeChecked();
    });

    test("lo guardado aún no está en WhatsApp: «pendiente de aplicar» y, agotados los reintentos, error con «Reintentar»", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/3/llamadas", { voz: { recibe: true, whatsapp: "pendiente" } });
        const f = page.getByRole("form", { name: "Llamadas de Soporte" });
        const estado = f.locator('[data-slot="en-whatsapp"]');
        await expect(estado).toHaveAttribute("data-estado", "pendiente");
        await expect(estado).toContainText("Pendiente de aplicar en WhatsApp");
        await expect(estado).toContainText("WhatsApp no respondió. Iris lo vuelve a intentar solo en 4 minutos.");
        await expect(f.locator('[data-slot="horas-whatsapp"]')).toContainText("el horario lo aplica Iris");
        await sinDesbordes(page);
        await captura(page, "ajustes-numero-pendiente");
        // Al intentarlo ya, WhatsApp lo rechaza: error con «Reintentar», sin perder lo guardado.
        api.voz.forzar.aplicar = "error";
        await estado.getByRole("button", { name: "Aplicar ahora" }).click();
        await expect(page.getByText("WhatsApp todavía no los aplica")).toBeVisible();
        const error = page.getByRole("form", { name: "Llamadas de Soporte" }).locator('[data-slot="en-whatsapp"]');
        await expect(error).toHaveAttribute("data-estado", "error");
        await expect(error).toContainText("No se pudo aplicar en WhatsApp");
        await expect(error).toContainText("WhatsApp rechazó los ajustes del número. Lo guardado en Iris no se pierde.");
        await sinDesbordes(page);
        delete api.voz.forzar.aplicar;
        await error.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText("Ajustes aplicados en WhatsApp")).toBeVisible();
        await expect(page.locator('[data-slot="en-whatsapp"]')).toHaveCount(0);
    });

    test("un número por QR no puede recibir llamadas: se dice y no hay formulario", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/10/llamadas", { opciones: { meta: { qr: true } } });
        await expect(page.getByRole("heading", { name: "Llamadas de Ventas tienda" })).toBeVisible();
        await expect(page.getByText("Este número no puede recibir llamadas")).toBeVisible();
        await expect(page.getByText("Está conectado por QR")).toBeVisible();
        await expect(page.getByRole("switch")).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Guardar" })).toHaveCount(0);
        await sinDesbordes(page);
        await captura(page, "ajustes-numero-qr");
        await page.getByRole("link", { name: "Volver a los números" }).click();
        await expect(page).toHaveURL(/\/administracion\/numeros$/);
        expect(api.voz.recibido.configurados).toHaveLength(0);
    });

    test("de la empresa: a quién le suena primero y duración; la grabación no se ofrece ni se manda", async ({ page }) => {
        const api = await abrir(page, "/administracion");
        await page.getByRole("link", { name: /^Llamadas/ }).click();
        await expect(page).toHaveURL(/\/administracion\/llamadas$/);
        const f = page.getByRole("form", { name: "Ajustes de llamadas" });
        await expect(f.getByLabel("Segundos que suena solo a quien tiene la conversación")).toHaveValue("10");
        await expect(f.locator('[data-slot="grabacion-no-disponible"]')).toContainText("la grabación todavía no está disponible.");
        await expect(f.getByRole("switch")).toHaveCount(0);
        // El campo no deja pasar del máximo de fábrica (120 min).
        await f.getByLabel("Minutos que puede durar una llamada, como mucho").fill("500");
        await f.getByLabel("Minutos que puede durar una llamada, como mucho").blur();
        await expect(f.getByLabel("Minutos que puede durar una llamada, como mucho")).toHaveValue("120");
        await f.getByLabel("Minutos que puede durar una llamada, como mucho").fill("45");
        await f.getByLabel("Minutos que puede durar una llamada, como mucho").blur();
        await f.getByLabel("Segundos que suena solo a quien tiene la conversación").fill("0");
        await f.getByLabel("Segundos que suena solo a quien tiene la conversación").blur();
        await sinDesbordes(page);
        await captura(page, "ajustes-empresa");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Ajustes de llamadas guardados")).toBeVisible();
        // La grabación no existe todavía: ni se ofrece ni se manda.
        expect(api.voz.recibido.ajustes).toEqual([{ version: 3, duracionMaxPersonaMin: 45, segundosAsignado: 0 }]);
    });

    test("sin `llamadas.configurar` no hay «Llamadas» en los números ni se entra por la URL", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros", { voz: { soloAtender: true } });
        await expect(page.getByRole("link", { name: "Configurar Soporte" })).toBeVisible();
        await expect(page.getByRole("link", { name: "Llamadas de Soporte" })).toHaveCount(0);
        await page.goto("/administracion/numeros/3/llamadas");
        await expect(page.getByRole("form", { name: "Llamadas de Soporte" })).toHaveCount(0);
        expect(api.peticiones.some((p) => p.ruta.includes("/canales/3/llamadas"))).toBe(false);
    });
});
