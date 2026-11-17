import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, conSesion, simularApi } from "./api-falsa";
import type { OpcionesVoz } from "./api-voz";
import { abrirMenu, esTelefono, hojaMas } from "./ayudas";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";
import { ajustarVoz, instalarDobleVoz, type ControlVoz } from "./doble-webrtc";

/*
 * Voz E1b (contratos/voz.md): llamadas salientes de WhatsApp desde el panel. El permiso del
 * contacto (verlo, pedirlo y sus 422), «Llamar» (oferta SDP → «Llamando…» → respuesta SDP → en
 * curso), sin contestar, el aviso de riesgo, los límites, la regla de oro (nunca una llamada viva
 * sin audio ni sin colgar), «Devolver la llamada», el hilo y los ajustes. API y feed falsos tipados
 * con packages/contratos, WebRTC y micrófono con dobles y reloj simulado. En los 4 proyectos.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-voz");
type Doble = Partial<Pick<ControlVoz, "microfono" | "permiso" | "medio">>;
type Extra = { voz?: OpcionesVoz; correo?: string; doble?: Doble };

const EN_7_DIAS = "2026-11-24T18:00:00Z";
/** Carlos Ruiz (conversación 41) ya dio permiso para que lo llamen. */
const CON_PERMISO: OpcionesVoz = { salientes: true, permisos: { 41: { estado: "temporal", venceEn: EN_7_DIAS } } };

async function abrir(page: Page, ruta: string, { voz = CON_PERMISO, correo = ADMIN.correo, doble = { permiso: "granted" } }: Extra = {}) {
    await page.clock.install();
    await instalarDobleVoz(page, doble);
    const api = await simularApi(page, { conversaciones: { flujoLargo: true }, voz });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}
type Api = Awaited<ReturnType<typeof abrir>>;

/** Deja el flujo de eventos abierto y esperando: lo que se emita después llega al instante. */
async function feedListo(page: Page, api: Api) {
    await expect
        .poll(
            async () => {
                if (api.flujoAbierto()) return true;
                await page.clock.fastForward(7_000);
                return api.flujoAbierto();
            },
            { timeout: 20_000, intervals: [200, 400, 800] },
        )
        .toBe(true);
}

/** Adelanta el reloj de a poco hasta que se cumpla. */
const adelantarHasta = (page: Page, listo: () => boolean | undefined, paso = 2_000) =>
    expect
        .poll(
            async () => {
                if (listo()) return true;
                await page.clock.fastForward(paso);
                return Boolean(listo());
            },
            { timeout: 25_000, intervals: [150] },
        )
        .toBe(true);

const aviso = (page: Page) => page.locator('[data-slot="aviso-llamada"]');
const llamando = (page: Page) => page.getByRole("region", { name: "Llamando" });
const enCurso = (page: Page) => page.getByRole("region", { name: "Llamada en curso" });
const nota = (page: Page) => page.getByRole("dialog", { name: "Terminó la llamada" });
const riesgo = (page: Page) => page.getByRole("alertdialog", { name: "Ya van tres llamadas sin contestar" });
const colgadas = (api: Api) => api.voz.recibido.acciones.filter((a) => a.accion === "colgar");
const ultima = (api: Api) => [...api.voz.llamadas.values()].at(-1);
const microfonoAbierto = (page: Page) => page.evaluate(() => window.__voz.microfonoAbierto());
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `saliente-${nombre}-${test.info().project.name}.png`) });

async function sinDesbordes(page: Page) {
    await expect.poll(async () => (await buscarDesbordes(page)).length, { timeout: 5_000 }).toBe(0).catch(() => undefined);
    await esperarSinDesbordes(page);
}

/** Abre «Llamada» en la cabecera de la conversación y devuelve su panel. */
async function abrirPermiso(page: Page) {
    await page.getByRole("button", { name: "Llamada", exact: true }).click();
    const panel = page.getByRole("dialog", { name: "Llamar por WhatsApp" });
    await expect(panel.locator('[data-slot="permiso-llamada"]')).toBeVisible();
    return panel;
}

/** Toca «Llamar» y espera a que el servidor tenga la llamada marcando. */
async function llamar(page: Page, api: Api) {
    const antes = api.voz.recibido.llamar.length;
    const panel = await abrirPermiso(page);
    await panel.getByRole("button", { name: "Llamar", exact: true }).click();
    await expect.poll(() => api.voz.recibido.llamar.length).toBe(antes + 1);
    await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
    const l = ultima(api);
    if (!l) throw new Error("El servidor no creó la llamada");
    return l;
}

async function cerrarSesion(page: Page) {
    await page.getByRole("link", { name: "Iris, inicio" }).click();
    await expect(page).toHaveURL(/\/$/);
    if (esTelefono(page)) {
        await abrirMenu(page);
        await hojaMas(page).getByRole("menuitem", { name: "Cerrar sesión" }).click();
    } else {
        await page.getByRole("button", { name: /^Tu cuenta/ }).click();
        await page.getByRole("menuitem", { name: "Cerrar sesión" }).click();
    }
}

test.describe("voz salientes › permiso de llamada", () => {
    test("sin pedir: explica por qué WhatsApp lo exige, se pide, queda en el hilo y, cuando el cliente acepta, se puede llamar", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { voz: { salientes: true } });
        const panel = await abrirPermiso(page);
        const permiso = panel.locator('[data-slot="permiso-llamada"]');
        await expect(permiso).toHaveAttribute("data-estado", "sin_pedir");
        await expect(permiso).toContainText("Sin permiso todavía");
        await expect(permiso).toContainText("WhatsApp solo deja que una empresa llame a quien aceptó recibir sus llamadas.");
        await expect(permiso.locator('[data-etiqueta="en-pruebas"]')).toBeVisible();
        // Sin permiso no se ofrece llamar.
        await expect(panel.getByRole("button", { name: "Llamar", exact: true })).toHaveCount(0);
        await sinDesbordes(page);
        await captura(page, "permiso-sin-pedir");

        await panel.getByRole("button", { name: "Pedir permiso para llamarle" }).click();
        await expect(page.getByText("Se le pidió permiso para llamarle")).toBeVisible();
        expect(api.voz.recibido.solicitudes).toEqual([41]);
        expect(api.peticiones.find((p) => p.ruta.endsWith("/41/solicitar-permiso-llamada"))?.idempotencia).toBeTruthy();
        await expect(permiso).toHaveAttribute("data-estado", "pedido");
        await expect(permiso).toContainText("Permiso pedido");
        await expect(permiso).toContainText("Falta que el cliente toque «Permitir»");
        // Ya se pidió: no se ofrece otra vez y dice desde cuándo se puede.
        await expect(panel.getByRole("button", { name: "Pedir permiso para llamarle" })).toHaveCount(0);
        await expect(permiso).toContainText("Se le puede volver a pedir a partir del");
        await captura(page, "permiso-pedido");

        // El cliente acepta: el panel se entera por el flujo y ofrece llamar.
        await feedListo(page, api);
        api.voz.respondePermiso(41, "acepta");
        await expect(permiso).toHaveAttribute("data-estado", "temporal");
        await expect(permiso).toContainText(/Permiso concedido hasta el \d+ de/);
        await expect(panel.getByRole("button", { name: "Llamar", exact: true })).toBeEnabled();
        await captura(page, "permiso-concedido");
        await page.keyboard.press("Escape");

        // La solicitud quedó en el hilo con icono + texto + color.
        const etiqueta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="etiqueta-permiso-llamada"]');
        await expect(etiqueta).toHaveText("Solicitud de permiso para llamar");
        await etiqueta.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "hilo-solicitud");
    });

    test("los 422 de «Pedir permiso», en llano: ya vigente, límite con desde cuándo, sin plantilla y BAJA", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=42", { voz: { salientes: true } });
        const panel = await abrirPermiso(page);
        const error = panel.locator('[data-slot="error-permiso"]');
        const pedir = panel.getByRole("button", { name: "Pedir permiso para llamarle" });

        api.voz.forzar.solicitar = "sin_plantilla_permiso";
        await pedir.click();
        await expect(error).toContainText("falta elegir la plantilla de permiso en los ajustes de llamadas del número");

        api.voz.forzar.solicitar = "baja";
        await pedir.click();
        await expect(error).toContainText("El cliente pidió no recibir mensajes por WhatsApp");

        api.voz.forzar.solicitar = "limite_solicitudes";
        await pedir.click();
        await expect(error).toContainText("WhatsApp deja pedirlo una vez cada 24 horas y dos veces cada 7 días. Se le puede volver a pedir a partir del 19 de noviembre a las 14:30.");
        await sinDesbordes(page);
        await captura(page, "permiso-limite");
        // El servidor ya no deja pedirlo: el botón se va.
        await expect(pedir).toHaveCount(0);

        // Otra conversación: el contacto ya había aceptado y esta pantalla no lo sabía.
        await page.keyboard.press("Escape");
        await page.goto("/conversaciones?c=43");
        const otro = await abrirPermiso(page);
        api.voz.forzar.solicitar = "permiso_ya_vigente";
        await otro.getByRole("button", { name: "Pedir permiso para llamarle" }).click();
        await expect(otro.locator('[data-slot="error-permiso"]')).toContainText("Este contacto ya dio permiso: ya se le puede llamar.");
        // …y se relee: ahora ofrece llamar.
        await expect(otro.getByRole("button", { name: "Llamar", exact: true })).toBeVisible();
        expect(api.voz.recibido.solicitudes).toEqual([42, 42, 42, 43]);
    });

    test("cada solicitud de permiso es una operación distinta: tras pedirlo con éxito, la siguiente lleva otra `Idempotency-Key`", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=42", { voz: { salientes: true } });
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Pedir permiso para llamarle" }).click();
        await expect(panel.locator('[data-slot="permiso-llamada"]')).toHaveAttribute("data-estado", "pedido");
        // El cliente dijo que no y, días después, se le puede volver a pedir (misma pantalla, sin recargar).
        await panel.press("Escape");
        await expect(panel).toHaveCount(0);
        Object.assign(api.voz.permisoDe(42), { estado: "negado", puedeSolicitar: true });
        const otraVez = await abrirPermiso(page);
        await expect(otraVez).toContainText("El cliente dijo que no");
        await otraVez.getByRole("button", { name: "Pedir permiso para llamarle" }).click();
        await expect.poll(() => api.voz.recibido.solicitudes).toEqual([42, 42]);
        const claves = api.peticiones.filter((p) => p.ruta.endsWith("/42/solicitar-permiso-llamada")).map((p) => p.idempotencia);
        expect(claves).toHaveLength(2);
        expect(claves[0]).toBeTruthy();
        expect(claves[1]).not.toBe(claves[0]);
    });

    test("un permiso temporal que venció tiene su texto, con la fecha, y se puede volver a pedir", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41", { voz: { salientes: true, permisos: { 41: { estado: "vencido", venceEn: "2026-11-10T18:00:00Z", puedeSolicitar: true } } } });
        const panel = await abrirPermiso(page);
        await expect(panel.locator('[data-slot="permiso-llamada"]')).toHaveAttribute("data-estado", "vencido");
        await expect(panel).toContainText("El permiso venció el 10 de noviembre");
        await expect(panel).toContainText("Hay que pedírselo de nuevo.");
        await expect(panel.getByRole("button", { name: "Llamar", exact: true })).toHaveCount(0);
        await expect(panel.getByRole("button", { name: "Pedir permiso para llamarle" })).toBeVisible();
        await sinDesbordes(page);
        await captura(page, "permiso-vencido");
    });

    test("si el permiso no se pudo leer: error con «Reintentar», sin ofrecer llamar ni pedir", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41");
        let fallar = true;
        await page.route("**/api/v1/mensajeria/conversaciones/41/permiso-llamada", (route) => (fallar ? route.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error del servidor", status: 500 }) }) : route.fallback()));
        await page.getByRole("button", { name: "Llamada", exact: true }).click();
        const panel = page.getByRole("dialog", { name: "Llamar por WhatsApp" });
        await expect(panel).toContainText("No se pudo leer el permiso de llamada");
        await expect(panel.getByRole("button", { name: "Llamar", exact: true })).toHaveCount(0);
        await expect(panel.getByRole("button", { name: "Pedir permiso para llamarle" })).toHaveCount(0);
        fallar = false;
        await panel.getByRole("button", { name: "Reintentar" }).click();
        await expect(panel.locator('[data-slot="permiso-llamada"]')).toContainText(/Permiso concedido hasta el 24 de noviembre/);
    });

    test("quien atiende sin `llamadas.llamar` ve el permiso y puede pedirlo, pero no llama; sin ninguno de los dos permisos no hay botón", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { correo: AGENTE_CONV.correo });
        const panel = await abrirPermiso(page);
        await expect(panel).toContainText(/Permiso concedido hasta el 24 de noviembre/);
        await expect(panel).toContainText("Tu usuario no puede llamar a clientes; sí puedes pedirle el permiso.");
        await expect(panel.getByRole("button", { name: "Llamar", exact: true })).toHaveCount(0);
        await sinDesbordes(page);
        await page.keyboard.press("Escape");
        expect(api.voz.recibido.llamar).toHaveLength(0);

        // Sin los permisos de E1b (los de fábrica de la bandeja): no existe el acceso ni se lee nada.
        const otra = await page.context().newPage();
        const sin = await abrir(otra, "/conversaciones?c=41", { voz: { recibe: false } });
        await expect(otra.getByRole("log", { name: "Mensajes" })).toBeVisible();
        await expect(otra.getByRole("button", { name: "Llamada", exact: true })).toHaveCount(0);
        expect(sin.peticiones.some((p) => p.ruta.endsWith("/permiso-llamada"))).toBe(false);
    });

    test("el número no tiene las llamadas encendidas para esta empresa: se dice y no se ofrece nada", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41", { voz: { salientes: true, permisos: { 41: { estado: "sin_pedir", motivo: "llamadas_inactivas" } } } });
        const panel = await abrirPermiso(page);
        await expect(panel).toContainText("Este número no tiene las llamadas de WhatsApp encendidas para tu empresa.");
        await expect(panel.getByRole("button", { name: "Llamar", exact: true })).toHaveCount(0);
        await expect(panel.getByRole("button", { name: "Pedir permiso para llamarle" })).toHaveCount(0);
    });
});

test.describe("voz salientes › llamar", () => {
    test("oferta SDP con el id del navegador → «Llamando…» con Colgar → respuesta SDP → el cliente contesta → en curso; colgar y anotar", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        // La oferta la crea el navegador y viaja con `X-Iris-Navegador` e idempotencia.
        const pedido = api.voz.recibido.llamar[0];
        expect(pedido?.conversacionId).toBe(41);
        expect(pedido?.navegador).toMatch(/^[A-Za-z0-9_-]{8,64}$/);
        // …y con el id de esta pestaña (128 bits), distinto del de navegador: el latido es de la pestaña.
        expect(pedido?.pestana).toMatch(/^[0-9a-f]{32}$/);
        expect(pedido?.pestana).not.toBe(pedido?.navegador);
        expect(String(pedido?.cuerpo.sdpOferta)).toContain("a=setup:actpass");
        expect(pedido?.cuerpo).not.toHaveProperty("confirmarRiesgo");
        expect(pedido?.cuerpo).not.toHaveProperty("devolucionId");
        expect(api.peticiones.find((p) => p.ruta.endsWith("/41/llamar"))?.idempotencia).toBeTruthy();
        await expect(llamando(page)).toContainText("Llamando a Carlos Ruiz…");
        await expect(llamando(page).getByRole("button", { name: "Colgar" })).toBeVisible();
        await expect(enCurso(page)).toHaveCount(0);
        // En el hilo queda como saliente, marcando.
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-direccion="saliente"]');
        await expect(tarjeta).toContainText("Llamando al cliente…");
        await sinDesbordes(page);
        await captura(page, "llamando");

        // WhatsApp manda la respuesta: el feed avisa (sin SDP) y se lee; la primera vez aún no está.
        await feedListo(page, api);
        api.voz.forzar.sinRespuesta = 1;
        await feedListo(page, api);
        api.voz.respondeMeta(l.id);
        await adelantarHasta(page, () => api.voz.recibido.respuestas.length >= 2, 400);
        expect(api.voz.recibido.respuestas.every((r) => r.navegador === pedido?.navegador && r.pestana === pedido?.pestana)).toBe(true);
        await expect.poll(() => page.evaluate(() => window.__voz.conexiones.at(-1)?.respuesta ?? "")).toContain("a=setup:active");
        // Con la respuesta aplicada sigue «Llamando…»: se habla cuando el cliente contesta.
        await expect(llamando(page)).toBeVisible();

        await feedListo(page, api);
        api.voz.contestaCliente(l.id);
        await expect(enCurso(page)).toBeVisible();
        await expect(enCurso(page)).toContainText("Carlos Ruiz");
        await expect(llamando(page)).toHaveCount(0);
        expect(await microfonoAbierto(page)).toBe(true);
        await expect(tarjeta).toContainText("En llamada");
        await sinDesbordes(page);
        await captura(page, "en-curso");

        await enCurso(page).getByRole("button", { name: "Colgar" }).click();
        await expect(nota(page)).toBeVisible();
        await expect.poll(() => api.voz.llamadas.get(l.id)?.estado).toBe("terminada");
        expect(await microfonoAbierto(page)).toBe(false);
        await nota(page).getByLabel("Nota (opcional)").fill("Se le explicó el corte.");
        await nota(page).getByRole("button", { name: "Guardar nota" }).click();
        await expect(page.getByText("Nota de la llamada guardada")).toBeVisible();
        await feedListo(page, api);
        await expect(tarjeta).toContainText("Llamada al cliente");
        await expect(tarjeta).toContainText("La hizo Ana");
        await expect(tarjeta).toHaveAttribute("data-tono", "ok");
        await tarjeta.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "hilo-contestada");
    });

    test("el flujo de eventos va siempre con el id de este navegador y sigue abierto fuera de la bandeja mientras dure la saliente, aunque no reciba llamadas", async ({ page }) => {
        const flujos: (string | undefined)[] = [];
        const pestanas: (string | undefined)[] = [];
        page.on("request", (r) => {
            if (new URL(r.url()).pathname !== "/api/v1/mensajeria/conversaciones/eventos") return;
            flujos.push(r.headers()["x-iris-navegador"]);
            pestanas.push(r.headers()["x-iris-pestana"]);
        });
        // «Recibir llamadas» apagado: fuera de la bandeja este navegador no tendría el flujo abierto.
        const api = await abrir(page, "/conversaciones?c=41", { voz: { ...CON_PERMISO, recibe: false } });
        const l = await llamar(page, api);
        const navegador = api.voz.recibido.llamar[0]?.navegador;
        await page.getByRole("link", { name: "Iris, inicio" }).click();
        await expect(page).toHaveURL(/\/$/);
        await expect(llamando(page)).toBeVisible();
        // La bandeja cerró su flujo; el proveedor tiene el suyo abierto porque hay una llamada (es su
        // latido): sin «Recibir» y sin llamada no lo estaría (se comprueba al final).
        await feedListo(page, api);
        expect(flujos.length).toBeGreaterThan(0);
        expect(flujos.every((n) => n !== undefined && n === navegador)).toBe(true);
        // El flujo lleva también el id de la pestaña que llamó: es su latido, no el de otra pestaña.
        expect(pestanas.every((p) => p !== undefined && p === api.voz.recibido.llamar[0]?.pestana)).toBe(true);
        // Y por ese flujo llega que el cliente contestó (el aviso `llamada_respuesta` se perdió: se lee igual).
        await feedListo(page, api);
        api.voz.contestaCliente(l.id, false);
        await expect(enCurso(page)).toBeVisible();
        await expect.poll(() => page.evaluate(() => window.__voz.conexiones.at(-1)?.respuesta ?? "")).toContain("a=setup:active");
        await enCurso(page).getByRole("button", { name: "Colgar" }).click();
        await expect(nota(page)).toBeVisible();
        await nota(page).getByRole("button", { name: "Ahora no" }).click();
        // Terminó: sin «Recibir» ya no hace falta el flujo.
        await expect.poll(() => api.flujoAbierto()).toBe(false);
    });

    test("no contesta o rechaza: «No contestó», queda en el hilo en ámbar y cuenta; si el aviso no llega, a los 70 s se cuelga", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        await feedListo(page, api);
        api.voz.noContesta(l.id);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "no_contesto");
        await expect(aviso(page)).toContainText("No contestó");
        await expect(aviso(page)).toContainText("Quedó en la conversación. Puedes escribirle.");
        await expect(llamando(page)).toHaveCount(0);
        expect(await microfonoAbierto(page)).toBe(false);
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-direccion="saliente"]');
        await expect(tarjeta).toContainText("Se le llamó y no contestó");
        await expect(tarjeta).toHaveAttribute("data-tono", "aviso");
        // Una saliente perdida no deja nada por devolver: solo se le escribe.
        await expect(tarjeta.getByRole("button", { name: "Escribirle" })).toBeVisible();
        await expect(tarjeta.getByRole("button", { name: /Devolver/ })).toHaveCount(0);
        await sinDesbordes(page);
        await captura(page, "no-contesto");
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();
        // El panel lo cuenta: a la cuarta WhatsApp quita el permiso.
        const panel = await abrirPermiso(page);
        await expect(panel.locator('[data-slot="sin-contestar"]')).toContainText("Va 1 llamada sin contestar. A la cuarta, WhatsApp quita el permiso.");
        await page.keyboard.press("Escape");

        // Otra: el servidor nunca avisa. A los 70 s se deja de esperar y se cuelga (no queda marcando).
        const otra = await llamar(page, api);
        // De a 3 s: el aviso se cierra solo a los 8 s y un salto largo se lo llevaría por delante.
        await expect
            .poll(
                async () => {
                    if (!colgadas(api).some((a) => a.llamadaId === otra.id)) await page.clock.fastForward(3_000);
                    return colgadas(api).some((a) => a.llamadaId === otra.id);
                },
                { timeout: 25_000, intervals: [100] },
            )
            .toBe(true);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "no_contesto");
        expect(api.voz.llamadas.get(otra.id)?.estado).toBe("terminada");
        expect(await microfonoAbierto(page)).toBe(false);
    });

    test("tras tres sin contestar: explica que a la cuarta WhatsApp quita el permiso; «No llamar» no llama y «Llamar de todos modos» manda `confirmarRiesgo`", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { voz: { salientes: true, permisos: { 41: { estado: "temporal", venceEn: EN_7_DIAS, sinContestar: 3 } } } });
        const panel = await abrirPermiso(page);
        await expect(panel.locator('[data-slot="sin-contestar"]')).toContainText("Van 3 llamadas seguidas sin contestar.");
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(riesgo(page)).toContainText("Si Carlos Ruiz tampoco contesta esta, WhatsApp le quita a la empresa el permiso para llamarle");
        await sinDesbordes(page);
        await captura(page, "riesgo");
        await riesgo(page).getByRole("button", { name: "No llamar" }).click();
        await expect(riesgo(page)).toHaveCount(0);
        expect(api.voz.recibido.llamar).toHaveLength(0);
        expect(await page.evaluate(() => window.__voz.pedidos)).toBe(0);

        const otra = await abrirPermiso(page);
        await otra.getByRole("button", { name: "Llamar", exact: true }).click();
        await riesgo(page).getByRole("button", { name: "Llamar de todos modos" }).click();
        await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
        expect(api.voz.recibido.llamar.map((p) => p.cuerpo.confirmarRiesgo)).toEqual([true]);

        // La cuarta sin contestar: WhatsApp quita el permiso y el panel lo dice.
        const l = ultima(api);
        await feedListo(page, api);
        if (l) api.voz.noContesta(l.id);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "no_contesto");
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();
        const revocado = await abrirPermiso(page);
        await expect(revocado.locator('[data-slot="permiso-llamada"]')).toHaveAttribute("data-estado", "revocado");
        await expect(revocado).toContainText("WhatsApp quitó el permiso");
        await expect(revocado.getByRole("button", { name: "Llamar", exact: true })).toHaveCount(0);
        await expect(revocado.getByRole("button", { name: "Pedir permiso para llamarle" })).toBeVisible();
        await captura(page, "permiso-revocado");
    });

    test("el servidor pide confirmar el riesgo (422 `confirmar_riesgo`) sin que esta pantalla lo supiera: pregunta y, si sigue, llama", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const panel = await abrirPermiso(page);
        // Otra persona acumuló las tres desde que se leyó el permiso.
        api.voz.permisoDe(41).sinContestar = 3;
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(riesgo(page)).toBeVisible();
        await expect(llamando(page)).toHaveCount(0);
        expect(await microfonoAbierto(page)).toBe(false);
        await riesgo(page).getByRole("button", { name: "Llamar de todos modos" }).click();
        await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
        expect(api.voz.recibido.llamar.map((p) => p.cuerpo.confirmarRiesgo ?? false)).toEqual([false, true]);
    });

    test("los errores de `llamar`, cada uno en llano, sin micrófono abierto ni llamada que colgar", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const casos = [
            ["limite_llamadas", "limite_llamadas", "Se llegó al límite de llamadas de hoy"],
            ["colgadas_marcando", "limite_colgadas", "Se colgaron varias llamadas a este contacto antes de que contestara"],
            ["tope_alcanzado", "tope_alcanzado", "La empresa llegó a su tope de minutos de llamadas"],
            ["llamada_en_curso", "otra_llamada", "Ya hay una llamada en curso"],
            ["meta_rechazo", "meta_rechazo", "WhatsApp no aceptó la llamada"],
            ["sin_permiso_llamada", "sin_permiso", "Este contacto no ha dado permiso para que lo llamen"],
            ["llamadas_inactivas", "llamadas_inactivas", "Este número no puede llamar"],
        ] as const;
        for (const [forzar, motivo, texto] of casos) {
            api.voz.forzar.llamar = forzar;
            const panel = await abrirPermiso(page);
            await panel.getByRole("button", { name: "Llamar", exact: true }).click();
            await expect(aviso(page)).toHaveAttribute("data-motivo", motivo);
            await expect(aviso(page)).toHaveAttribute("data-saliente", "true");
            await expect(aviso(page)).toContainText(texto);
            await expect(llamando(page)).toHaveCount(0);
            expect(await microfonoAbierto(page)).toBe(false);
            if (motivo === "limite_colgadas") {
                await sinDesbordes(page);
                await captura(page, "limite-colgadas");
            }
            await aviso(page).getByRole("button", { name: "Cerrar" }).click();
        }
        expect(api.voz.llamadas.size).toBe(0);
        expect(colgadas(api)).toHaveLength(0);
        // Sin red: no se sabe si el servidor la creó → se busca mi llamada activa para colgarla (no había).
        api.voz.forzar.llamar = "sin_red";
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_red");
        await expect(aviso(page)).toContainText("No se pudo hacer la llamada");
        const antes = api.voz.recibido.activas.length;
        await adelantarHasta(page, () => api.voz.recibido.activas.length >= antes + 2);
        expect(colgadas(api)).toHaveLength(0);
    });

    test("el micrófono se niega: se dice y no se le pide nada al servidor", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { doble: { permiso: "prompt", microfono: "denegado" } });
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "microfono_denegado");
        await expect(aviso(page)).toContainText("El navegador no dio permiso para el micrófono");
        expect(api.voz.recibido.llamar).toHaveLength(0);
    });

    test("la empresa llega a su tope de minutos con la llamada en curso: el servidor la corta y se explica", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        await feedListo(page, api);
        api.voz.contestaCliente(l.id);
        await expect(enCurso(page)).toBeVisible();
        await feedListo(page, api);
        api.voz.cortaPorTope(l.id);
        await expect(nota(page)).toBeVisible();
        await expect(nota(page).locator('[data-slot="corte-tope"]')).toContainText("La llamada se cortó por el tope de minutos");
        await expect(nota(page).locator('[data-slot="corte-tope"]')).toContainText("Un administrador puede subirlo");
        expect(await microfonoAbierto(page)).toBe(false);
        // Ya la terminó el servidor: no se manda colgar.
        expect(colgadas(api)).toHaveLength(0);
        await sinDesbordes(page);
        await captura(page, "tope");
    });
});

test.describe("voz salientes › nunca una llamada viva sin audio ni sin colgar", () => {
    test("«Colgar» mientras marca la termina en el servidor; al cerrar la pestaña marcando, `colgar?propia=true`", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        await llamando(page).getByRole("button", { name: "Colgar" }).click();
        await expect(llamando(page)).toHaveCount(0);
        await expect.poll(() => api.voz.llamadas.get(l.id)?.motivoFin).toBe("agente_colgo");
        expect(await microfonoAbierto(page)).toBe(false);
        // Colgar marcando no cuenta como sin contestar ni deja nota.
        expect(api.voz.permisoDe(41).sinContestar).toBe(0);
        await expect(nota(page)).toHaveCount(0);
        await expect(aviso(page)).toHaveCount(0);

        const otra = await llamar(page, api);
        await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
        await expect.poll(() => api.voz.llamadas.get(otra.id)?.estado).toBe("terminada");
        const cierre = api.peticiones.filter((p) => p.ruta.endsWith(`/${otra.id}/colgar`)).at(0);
        expect(cierre?.query.get("propia")).toBe("true");
    });

    test("se cuelga con `llamar` todavía en vuelo: cuando el servidor responde 201, la llamada se cuelga (no queda sonándole al cliente)", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const soltar = api.voz.retener("llamar");
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect.poll(() => api.voz.recibido.llamar.length).toBe(1);
        await expect(llamando(page)).toHaveAttribute("data-paso", "preparando");
        await llamando(page).getByRole("button", { name: "Colgar" }).click();
        await expect(llamando(page)).toHaveCount(0);
        expect(await microfonoAbierto(page)).toBe(false);
        expect(colgadas(api)).toHaveLength(0);
        soltar();
        await expect.poll(() => ultima(api)?.estado).toBe("terminada");
        expect(colgadas(api)).toHaveLength(1);
    });

    test("se pierde la respuesta de `llamar` (el servidor sí la creó): se busca mi llamada viva y se cuelga", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        api.voz.forzar.llamar = "perdida";
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_red");
        expect(await microfonoAbierto(page)).toBe(false);
        await expect.poll(() => ultima(api)?.estado).toBe("terminada");
        expect(ultima(api)?.motivoFin).toBe("agente_colgo");
    });

    test("se pierde la respuesta de `llamar` y el servidor tarda en crearla: se sigue buscando hasta pasar su plazo (no dos lecturas) y se cuelga", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const alAbrir = api.voz.recibido.activas.length;
        api.voz.forzar.llamar = "tardia";
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_red");
        // Las dos primeras lecturas dicen «no hay ninguna»: no basta, el servidor aún puede crearla.
        await adelantarHasta(page, () => api.voz.recibido.activas.length >= alAbrir + 2);
        expect(api.voz.llamadas.size).toBe(0);
        await adelantarHasta(page, () => ultima(api)?.estado === "terminada");
        expect(ultima(api)?.motivoFin).toBe("agente_colgo");
        expect(colgadas(api)).toHaveLength(1);
    });

    test("al buscar la saliente perdida no se cuelga la de otra pestaña: es de otra conversación", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        await expect(page.getByRole("log", { name: "Mensajes" })).toBeVisible();
        const alAbrir = api.voz.recibido.activas.length;
        // En otra pestaña estoy llamando a María (42); aquí `llamar` a Carlos no llega al servidor.
        const ajena = api.voz.salienteAjena(42);
        api.voz.forzar.llamar = "sin_red";
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_red");
        // Se busca durante todo el plazo del servidor (10 s) y `activa` siempre devuelve la de María.
        await adelantarHasta(page, () => api.voz.recibido.activas.length >= alAbrir + 4);
        await page.clock.fastForward(15_000);
        expect(colgadas(api)).toHaveLength(0);
        expect(api.voz.llamadas.get(ajena.id)?.estado).toBe("marcando");
    });

    test("al abrir el panel con una saliente mía viva de otra pestaña (aunque no reciba llamadas ni conteste): lo dice y ofrece colgarla", async ({ page }) => {
        // Solo llama: sin `llamadas.contestar` y, por tanto, sin «Recibir llamadas».
        const api = await abrir(page, "/cuenta", { voz: { ...CON_PERMISO, soloLlama: true } });
        await expect.poll(() => api.voz.recibido.activas.length).toBeGreaterThan(0);
        expect(api.voz.recibido.activas.every((p) => /^[0-9a-f]{32}$/.test(p))).toBe(true);
        await expect(page.getByText(/Tenías una llamada/)).toHaveCount(0);
        // La dejé marcando en otra pestaña (o recargué): al volver a cargar, se lee y se avisa.
        const ajena = api.voz.salienteAjena(41);
        await page.reload();
        await expect(page.getByText("Tenías una llamada con Carlos Ruiz")).toBeVisible();
        await expect(llamando(page)).toHaveCount(0);
        await sinDesbordes(page);
        await captura(page, "huerfana");
        await page.getByRole("button", { name: "Colgar" }).click();
        await expect.poll(() => api.voz.llamadas.get(ajena.id)?.estado).toBe("terminada");
        // No es de esta pestaña: se cuelga con `?propia=true`.
        expect(api.peticiones.find((p) => p.ruta.endsWith(`/${ajena.id}/colgar`))?.query.get("propia")).toBe("true");
    });

    test("se cierra la pestaña con `llamar` en vuelo: deja una marca y la carga siguiente busca esa saliente y la cuelga", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const soltar = api.voz.retener("llamar");
        const panel = await abrirPermiso(page);
        await panel.getByRole("button", { name: "Llamar", exact: true }).click();
        await expect.poll(() => api.voz.recibido.llamar.length).toBe(1);
        // Se cierra ya: no hay id que colgar; queda la marca con la conversación.
        await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
        const marca = await page.evaluate(() => window.localStorage.getItem("iris-llamar-en-vuelo"));
        expect(JSON.parse(marca ?? "{}")).toMatchObject({ conversacionId: 41, pestana: api.voz.recibido.llamar[0]?.pestana });
        expect(colgadas(api)).toHaveLength(0);
        await page.goto("about:blank");
        // El servidor sí la creó, después de que la pestaña ya no estaba.
        soltar();
        await expect.poll(() => ultima(api)?.estado).toBe("marcando");
        // La carga siguiente (otra pestaña: otro id) toma la marca, la busca y la cuelga.
        await page.goto("/cuenta");
        await adelantarHasta(page, () => ultima(api)?.estado === "terminada");
        expect(await page.evaluate(() => window.localStorage.getItem("iris-llamar-en-vuelo"))).toBeNull();
        expect(new Set(api.voz.recibido.activas).size).toBeGreaterThan(1);
    });

    test("otra pestaña abierta atiende la marca en cuanto aparece (evento `storage`) y cuelga la saliente", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        await expect.poll(() => api.voz.recibido.activas.length).toBeGreaterThan(0);
        const perdida = api.voz.salienteAjena(41);
        // La pestaña que llamaba se cerró con `llamar` en vuelo y dejó su marca.
        const empresa = api.peticiones.find((p) => p.empresa)?.empresa;
        await page.evaluate((empresaId) => {
            const valor = JSON.stringify({ empresa: empresaId, conversacionId: 41, desde: Date.now(), pestana: "0".repeat(32) });
            window.localStorage.setItem("iris-llamar-en-vuelo", valor);
            window.dispatchEvent(new StorageEvent("storage", { key: "iris-llamar-en-vuelo", newValue: valor }));
        }, empresa ? Number(empresa) : null);
        await adelantarHasta(page, () => api.voz.llamadas.get(perdida.id)?.estado === "terminada");
        expect(await page.evaluate(() => window.localStorage.getItem("iris-llamar-en-vuelo"))).toBeNull();
    });

    test("la respuesta de WhatsApp llegó antes de que `llamar` respondiera (su aviso se perdió): se lee al marcar, sin esperar otro aviso", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        api.voz.forzar.respuestaLista = true;
        await llamar(page, api);
        await expect.poll(() => page.evaluate(() => window.__voz.conexiones.at(-1)?.respuesta ?? "")).toContain("a=setup:active");
        expect(api.voz.recibido.respuestas).toHaveLength(1);
        await expect(llamando(page)).toBeVisible();

        // Y si al marcar todavía no hay respuesta, esa lectura no cuenta como fallo: se espera al aviso.
        await llamando(page).getByRole("button", { name: "Colgar" }).click();
        await expect(llamando(page)).toHaveCount(0);
        await expect.poll(() => ultima(api)?.estado).toBe("terminada");
        api.voz.forzar.respuestaLista = false;
        const otra = await llamar(page, api);
        await expect.poll(() => api.voz.recibido.respuestas.filter((r) => r.llamadaId === otra.id).length).toBe(1);
        await page.clock.fastForward(8_000);
        await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
        await expect(aviso(page)).toHaveCount(0);
    });

    test("el servidor corta la saliente mientras marca: se le explica a quien llamaba y no queda el micrófono abierto", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        await feedListo(page, api);
        api.voz.cortaServidor(l.id);
        // 55 s sin el latido de esta pestaña: `terminada` con `error_medio`. Se dice en llano.
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_latido");
        await expect(aviso(page)).toContainText("La llamada se cortó: se perdió la conexión con Iris");
        await expect(aviso(page)).toContainText("No cuenta como llamada sin contestar");
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-direccion="saliente"]');
        await expect(tarjeta).toContainText("La llamada al cliente se cortó");
        await expect(tarjeta).toHaveAttribute("data-tono", "aviso");
        await sinDesbordes(page);
        await captura(page, "cortada");
        await expect(llamando(page)).toHaveCount(0);
        expect(await microfonoAbierto(page)).toBe(false);
    });

    test("`respuesta-sdp` dice que la llamada es de otra pestaña (409 `otra_pestana`): no hay audio aquí, así que se cuelga la propia y se dice", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        api.voz.forzar.respuesta = "otra_pestana";
        await feedListo(page, api);
        api.voz.respondeMeta(l.id);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "otro_navegador");
        await expect(aviso(page)).toContainText("La llamada quedó atada a otra pestaña o navegador y se colgó");
        await expect.poll(() => api.voz.llamadas.get(l.id)?.estado).toBe("terminada");
        expect(await microfonoAbierto(page)).toBe(false);
    });

    test("el cliente contesta pero el audio no conecta: a los 15 s se cuelga y se dice; si el medio falla marcando, también", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { doble: { permiso: "granted", medio: "nunca" } });
        const l = await llamar(page, api);
        await feedListo(page, api);
        api.voz.contestaCliente(l.id);
        await expect(enCurso(page)).toBeVisible();
        await adelantarHasta(page, () => api.voz.llamadas.get(l.id)?.estado === "terminada", 5_000);
        await expect(nota(page)).toContainText("La llamada se cortó");
        expect(await microfonoAbierto(page)).toBe(false);
        await nota(page).getByRole("button", { name: "Ahora no" }).click();

        // Marcando y la respuesta llega, pero el medio falla: no se espera a que conteste a una línea muerta.
        await ajustarVoz(page, { medio: "falla" });
        const otra = await llamar(page, api);
        await feedListo(page, api);
        api.voz.respondeMeta(otra.id);
        await expect(aviso(page)).toHaveAttribute("data-motivo", "medio_fallido");
        await expect.poll(() => api.voz.llamadas.get(otra.id)?.estado).toBe("terminada");
        expect(await microfonoAbierto(page)).toBe(false);
    });

    test("cerrar sesión llamando: pregunta antes; «Quedarme» sigue llamando y «Colgar y seguir» cuelga ANTES de cerrar la sesión", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        await cerrarSesion(page);
        const dialogo = page.getByRole("alertdialog", { name: "Estás llamando a Carlos Ruiz" });
        await expect(dialogo).toContainText("Si sigues, la llamada se cuelga.");
        await sinDesbordes(page);
        await captura(page, "salir-llamando");
        await dialogo.getByRole("button", { name: "Quedarme" }).click();
        await expect(llamando(page)).toBeVisible();
        expect(api.voz.llamadas.get(l.id)?.estado).toBe("marcando");

        if (esTelefono(page)) {
            await abrirMenu(page);
            await hojaMas(page).getByRole("menuitem", { name: "Cerrar sesión" }).click();
        } else {
            await page.getByRole("button", { name: /^Tu cuenta/ }).click();
            await page.getByRole("menuitem", { name: "Cerrar sesión" }).click();
        }
        await dialogo.getByRole("button", { name: "Colgar y seguir" }).click();
        await expect(page).toHaveURL(/\/entrar/);
        expect(api.voz.llamadas.get(l.id)?.estado).toBe("terminada");
        const rutas = api.peticiones.map((p) => `${p.metodo} ${p.ruta}`);
        const colgar = rutas.findIndex((r) => r.startsWith("POST") && r.endsWith("/colgar"));
        expect(colgar).toBeGreaterThan(-1);
        expect(rutas.indexOf("DELETE /api/v1/sesion")).toBeGreaterThan(colgar);
    });

    test("un «Colgar» que sigue en vuelo no se descarta al irse: cerrar sesión lo espera y, si no termina, lo dice; «Salir de todos modos» no reabre el diálogo", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const l = await llamar(page, api);
        // El servidor no deja colgar: la llamada ya no está en pantalla, pero sigue viva allá.
        api.voz.forzar.colgar = 99;
        await llamando(page).getByRole("button", { name: "Colgar" }).click();
        await expect(llamando(page)).toHaveCount(0);
        await expect.poll(() => colgadas(api).length).toBeGreaterThan(0);
        await cerrarSesion(page);
        // No hay llamada en pantalla y aun así no se cierra la sesión en silencio.
        const sinColgar = page.getByRole("alertdialog", { name: "No se pudo colgar la llamada" });
        await page.clock.fastForward(7_000);
        await expect(sinColgar).toBeVisible();
        expect(api.peticiones.some((p) => p.metodo === "DELETE" && p.ruta === "/api/v1/sesion")).toBe(false);
        expect(api.voz.llamadas.get(l.id)?.estado).toBe("marcando");
        // «Reintentar» sigue sin lograrlo; mientras reintenta, «Salir de todos modos» sale y el diálogo no vuelve.
        await sinColgar.getByRole("button", { name: "Reintentar" }).click();
        await sinColgar.getByRole("button", { name: "Salir de todos modos" }).click();
        await expect(page).toHaveURL(/\/entrar/);
        await page.clock.fastForward(8_000);
        await expect(sinColgar).toHaveCount(0);
    });
});

test.describe("voz salientes › devolver la llamada y el hilo", () => {
    test("«Devolver la llamada» desde las pendientes: llama con `devolucionId`, abre la conversación y, si contesta, deja de estar por devolver", async ({ page }) => {
        const api = await abrir(page, "/conversaciones", { voz: { ...CON_PERMISO, devoluciones: [41] } });
        const porDevolver = page.getByRole("region", { name: "Llamadas por devolver" });
        await porDevolver.getByRole("button", { name: "1 llamada por devolver" }).click();
        const fila = porDevolver.locator("li", { hasText: "Carlos Ruiz" });
        // Junto a «Escribirle».
        await expect(fila.getByRole("link", { name: "Escribirle" })).toBeVisible();
        await sinDesbordes(page);
        await captura(page, "devolver-lista");
        await fila.getByRole("button", { name: "Devolver la llamada" }).click();
        await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
        await expect(page).toHaveURL(/c=41/);
        const devolucion = api.voz.devoluciones[0];
        expect(api.voz.recibido.llamar.map((p) => p.cuerpo.devolucionId)).toEqual([devolucion?.id]);
        const l = ultima(api);
        await feedListo(page, api);
        if (l) api.voz.contestaCliente(l.id);
        await expect(enCurso(page)).toBeVisible();
        expect(devolucion?.estado).toBe("hecha");
        await expect(porDevolver).toHaveCount(0);
    });

    test("«Devolver la llamada» en la tarjeta de la perdida, junto a «Escribirle»; si otra persona ya la cerró, se dice", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { voz: { ...CON_PERMISO, devoluciones: [41] } });
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-estado="perdida"]');
        await expect(tarjeta.getByRole("button", { name: "Escribirle" })).toBeVisible();
        await tarjeta.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "devolver-tarjeta");
        api.voz.forzar.llamar = "devolucion_cerrada";
        await tarjeta.getByRole("button", { name: "Devolver la llamada" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "devolucion_cerrada");
        await expect(aviso(page)).toContainText("Esta llamada ya no está por devolver");
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();
        delete api.voz.forzar.llamar;
        await tarjeta.getByRole("button", { name: "Devolver la llamada" }).click();
        await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
        expect(api.voz.recibido.llamar.at(-1)?.cuerpo.devolucionId).toBe(api.voz.devoluciones[0]?.id);
        // Con una llamada en marcha no se empieza otra.
        await expect(tarjeta.getByRole("button", { name: "Devolver la llamada" })).toBeDisabled();
    });

    test("«Devolver la llamada» lee el permiso ANTES de pedir el micrófono: sin permiso, o con el número sin llamadas, lo dice y no llama", async ({ page }) => {
        // Carlos tiene una llamada por devolver, pero nunca dio permiso.
        const api = await abrir(page, "/conversaciones?c=41", { voz: { salientes: true, devoluciones: [41] }, doble: { permiso: "prompt" } });
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-estado="perdida"]');
        await tarjeta.getByRole("button", { name: "Devolver la llamada" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "sin_permiso");
        await expect(aviso(page)).toContainText("Este contacto no ha dado permiso para que lo llamen");
        expect(await page.evaluate(() => window.__voz.pedidos)).toBe(0);
        expect(api.voz.recibido.llamar).toHaveLength(0);
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();

        api.voz.permisoDe(41).motivo = "llamadas_inactivas";
        await tarjeta.getByRole("button", { name: "Devolver la llamada" }).click();
        await expect(aviso(page)).toHaveAttribute("data-motivo", "llamadas_inactivas");
        await aviso(page).getByRole("button", { name: "Cerrar" }).click();

        // Con permiso y tres sin contestar: el aviso de riesgo sale del permiso leído, también antes del micrófono.
        Object.assign(api.voz.permisoDe(41), { estado: "temporal", venceEn: EN_7_DIAS, sinContestar: 3, motivo: undefined });
        await tarjeta.getByRole("button", { name: "Devolver la llamada" }).click();
        await expect(riesgo(page)).toBeVisible();
        expect(await page.evaluate(() => window.__voz.pedidos)).toBe(0);
        expect(api.voz.recibido.llamar).toHaveLength(0);
        await riesgo(page).getByRole("button", { name: "Llamar de todos modos" }).click();
        await expect(llamando(page)).toHaveAttribute("data-paso", "marcando");
        expect(api.voz.recibido.llamar.map((p) => [p.cuerpo.confirmarRiesgo, p.cuerpo.devolucionId])).toEqual([[true, api.voz.devoluciones[0]?.id]]);
    });

    test("quien no puede llamar no ve «Devolver la llamada»: le escribe o la descarta", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41", { voz: { ...CON_PERMISO, devoluciones: [41] }, correo: AGENTE_CONV.correo });
        const tarjeta = page.getByRole("log", { name: "Mensajes" }).locator('[data-slot="llamada-hilo"][data-estado="perdida"]');
        await expect(tarjeta.getByRole("button", { name: "Escribirle" })).toBeVisible();
        await expect(page.getByRole("button", { name: "Devolver la llamada" })).toHaveCount(0);
    });

    test("quien solo llama (sin `llamadas.contestar`): no tiene «Recibir llamadas» ni se lee, y puede llamar", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { voz: { ...CON_PERMISO, soloLlama: true } });
        await expect(page.getByRole("log", { name: "Mensajes" })).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("button", { name: /^Recibir llamadas/ })).toHaveCount(0);
        const l = await llamar(page, api);
        expect(api.peticiones.some((p) => p.ruta.endsWith("/llamadas/recibir"))).toBe(false);
        await feedListo(page, api);
        api.voz.contestaCliente(l.id);
        await expect(enCurso(page)).toBeVisible();
        // Una entrante no le suena: no contesta llamadas.
        await feedListo(page, api);
        api.voz.entrante(42);
        await expect(aviso(page)).toHaveCount(0);
    });
});

test.describe("voz salientes › ajustes", () => {
    test("de la empresa: los límites de las salientes van en «Avanzado», plegado, con la nota de que se cobran por minuto", async ({ page }) => {
        const api = await abrir(page, "/administracion/llamadas", { voz: { recibe: false } });
        const f = page.getByRole("form", { name: "Ajustes de llamadas" });
        const avanzado = f.locator('[data-slot="avanzado"]');
        await expect(avanzado.getByRole("button", { name: /Avanzado/ })).toHaveAttribute("aria-expanded", "false");
        await expect(avanzado).toContainText("Hasta 50 llamadas salientes al día · 300 minutos al mes");
        await expect(f.getByLabel("Llamadas salientes al día, como mucho")).toHaveCount(0);
        await avanzado.getByRole("button", { name: /Avanzado/ }).click();
        await expect(avanzado.locator('[data-slot="nota-cobro"]')).toContainText("se cobra por minuto");
        await expect(avanzado.locator('[data-etiqueta="en-pruebas"]')).toBeVisible();
        await expect(f.getByLabel("Llamadas salientes al día, como mucho")).toHaveValue("50");
        await expect(f.getByLabel("Minutos de llamadas salientes al mes, como mucho")).toHaveValue("300");
        const colgadasDia = f.getByLabel("Llamadas a un mismo contacto que se pueden colgar antes de que conteste, por día");
        await expect(colgadasDia).toHaveValue("3");
        await expect(avanzado).toContainText("Con 0 no hay límite.");
        await f.getByLabel("Llamadas salientes al día, como mucho").fill("20");
        await f.getByLabel("Llamadas salientes al día, como mucho").blur();
        await f.getByLabel("Minutos de llamadas salientes al mes, como mucho").fill("120");
        await f.getByLabel("Minutos de llamadas salientes al mes, como mucho").blur();
        await colgadasDia.fill("0");
        await colgadasDia.blur();
        await expect(avanzado).toContainText("Hasta 20 llamadas salientes al día · 120 minutos al mes");
        await sinDesbordes(page);
        await captura(page, "ajustes-avanzado");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Ajustes de llamadas guardados")).toBeVisible();
        expect(api.voz.recibido.ajustes).toEqual([{ version: 3, duracionMaxPersonaMin: 60, segundosAsignado: 10, maxSalientesPorDia: 20, topeMinutosSalientesMes: 120, maxColgadasMarcandoDia: 0 }]);
    });

    test("por número: plantilla de permiso de llamada y su idioma; si WhatsApp no la tiene aprobada (422), se dice junto al campo", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/3/llamadas", { voz: { recibe: false } });
        const f = page.getByRole("form", { name: "Llamadas de Soporte" });
        const plantilla = f.getByLabel("Plantilla de permiso de llamada");
        const idioma = f.getByLabel("Idioma", { exact: true });
        await expect(plantilla).toHaveValue("");
        // Espejo del servidor: el nombre como en Meta y el código del idioma.
        await plantilla.fill("Permiso de Llamada");
        await idioma.fill("español");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f).toContainText("Solo minúsculas, números y guion bajo, como en Meta");
        expect(api.voz.recibido.configurados).toHaveLength(0);
        await idioma.fill("es");
        await plantilla.fill("no_existe");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f.locator('[data-slot="plantilla-permiso"]')).toContainText("WhatsApp no tiene aprobada una plantilla con ese nombre en ese idioma");
        await plantilla.scrollIntoViewIfNeeded();
        await sinDesbordes(page);
        await captura(page, "ajustes-plantilla");
        await plantilla.fill("permiso_de_llamada");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Llamadas de Soporte guardadas")).toBeVisible();
        expect(api.voz.recibido.configurados.at(-1)?.cuerpo).toMatchObject({ plantillaPermiso: "permiso_de_llamada", plantillaPermisoIdioma: "es" });
        expect(api.voz.canal(3)).toMatchObject({ plantillaPermiso: "permiso_de_llamada", plantillaPermisoIdioma: "es" });
    });
});
