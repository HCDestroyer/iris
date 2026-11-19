import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import type { OpcionesVoz } from "./api-voz";
import { abrirMenu, esTelefono, hojaMas } from "./ayudas";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";
import { instalarDobleVoz, type ControlVoz } from "./doble-webrtc";

/*
 * Prueba con Meta real (0.29.x): las llamadas entraban y a nadie le sonaban (nadie «Disponible» con
 * «Recibir llamadas» encendido) y el panel no decía por qué ni dónde se enciende. La ráfaga rechazaba
 * sola sin decirlo. Aquí: el paso que falta, dicho en la tarjeta con su acción en un toque, el aviso de
 * la primera vez y el motivo de la ráfaga con «Dejar que vuelva a llamar».
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-voz");
type Extra = { voz?: OpcionesVoz; doble?: Partial<Pick<ControlVoz, "microfono" | "permiso" | "medio">>; opciones?: Parameters<typeof simularApi>[1] };

async function abrir(page: Page, ruta: string, { voz = { recibe: true }, doble = { permiso: "granted" }, opciones = {} }: Extra = {}) {
    await page.clock.install();
    await instalarDobleVoz(page, doble);
    const api = await simularApi(page, { ...opciones, conversaciones: { flujoLargo: true, ...opciones.conversaciones }, voz });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}
type Api = Awaited<ReturnType<typeof abrir>>;

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

const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });
const hilo = (page: Page) => page.getByRole("log", { name: "Mensajes" });
const tarjeta = (page: Page, estado: string) => hilo(page).locator(`[data-slot="llamada-hilo"][data-estado="${estado}"]`);

async function sinDesbordes(page: Page) {
    await expect.poll(async () => (await buscarDesbordes(page)).length, { timeout: 5_000 }).toBe(0).catch(() => undefined);
    await esperarSinDesbordes(page);
}

async function abrirRecibir(page: Page) {
    if (esTelefono(page)) {
        await abrirMenu(page);
        return hojaMas(page).getByRole("region", { name: "Llamadas" });
    }
    await page.getByRole("button", { name: /^Recibir llamadas/ }).click();
    return page.getByRole("dialog", { name: "Llamadas" });
}

test.describe("voz › cuando no suena: el paso que falta, en un toque", () => {
    test("la perdida dice que no te sonó porque «Recibir llamadas» está apagado y lo enciende desde la tarjeta; la primera vez se ofrece en un aviso", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: {} });
        // La primera vez, un aviso discreto lo ofrece (una sola vez por persona y navegador).
        await expect(page.getByText("Puedes contestar llamadas de WhatsApp aquí")).toBeVisible();
        api.voz.historica(41, "perdida", { duracionSeg: 25, motivoFin: "nadie_contesto" });
        await page.goto("/conversaciones?c=41");
        await expect(page.getByText("Puedes contestar llamadas de WhatsApp aquí")).toHaveCount(0);
        const paso = tarjeta(page, "perdida").locator('[data-slot="paso-que-falta"]');
        await expect(paso).toHaveAttribute("data-paso", "recibir");
        await expect(paso).toContainText("No te sonó porque «Recibir llamadas» está apagado en este navegador.");
        await sinDesbordes(page);
        await captura(page, "perdida-paso-que-falta");
        await paso.getByRole("button", { name: "Encender recibir llamadas" }).click();
        await expect(page.getByText("Este navegador ya recibe llamadas")).toBeVisible();
        await expect(paso).toHaveCount(0);
        expect(api.voz.recibido.recibir.map((r) => r.accion)).toEqual(["encender"]);
    });

    test("sin ningún número con llamadas no se ofrece nada", async ({ page }) => {
        await abrir(page, "/cuenta", { voz: { sinNumeroConLlamadas: true } });
        await expect(page.getByRole("button", { name: /^Recibir llamadas|^Más/ }).first()).toBeVisible();
        await page.clock.fastForward(2_000);
        await expect(page.getByText("Puedes contestar llamadas de WhatsApp aquí")).toHaveCount(0);
    });

    test("encendido pero «No disponible»: la tarjeta y el panel de llamadas ofrecen «Ponerme disponible»", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: { recibe: true }, opciones: { conversaciones: { disponibilidad: "no_disponible" } } });
        api.voz.historica(41, "perdida", { duracionSeg: 25, motivoFin: "nadie_contesto" });
        await page.goto("/conversaciones?c=41");
        const paso = tarjeta(page, "perdida").locator('[data-slot="paso-que-falta"]');
        await expect(paso).toHaveAttribute("data-paso", "disponible");
        await expect(paso).toContainText("No te sonó porque estás «No disponible».");
        // El panel de «Llamadas» (en el teléfono, arriba de «Más») lo ofrece también.
        await page.goto("/cuenta");
        const panel = await abrirRecibir(page);
        await expect(panel.locator('[data-slot="recibir-no-disponible"]')).toContainText("Estás «No disponible»");
        await sinDesbordes(page);
        await captura(page, "recibir-no-disponible");
        await panel.getByRole("button", { name: "Ponerme disponible" }).click();
        await expect.poll(() => api.conversaciones.enviados.find((e) => e.ruta.endsWith("/conversaciones/disponibilidad"))?.cuerpo).toMatchObject({ estado: "disponible" });
        await page.goto("/conversaciones?c=41");
        await expect(tarjeta(page, "perdida")).toBeVisible();
        await expect(paso).toHaveCount(0);
    });

    test("con todo listo, la tarjeta de la llamada que suena también tiene «Contestar»", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        await feedListo(page, api);
        api.voz.entrante(41);
        const aviso = page.locator('[data-slot="aviso-llamada"]');
        await expect(aviso).toHaveAttribute("data-fase", "sonando");
        // El hilo trae la tarjeta al leerse (la llamada sigue sonando al recargar).
        await page.reload();
        await expect(aviso).toHaveAttribute("data-fase", "sonando");
        const sonando = tarjeta(page, "sonando");
        await expect(sonando).toBeVisible();
        await expect(sonando.locator('[data-slot="paso-que-falta"]')).toHaveCount(0);
        await sonando.getByRole("button", { name: "Contestar" }).click();
        await expect(aviso).toHaveAttribute("data-fase", "contestando");
    });

    test("rechazada por ráfaga: a quien configura le dice a qué hora vuelve a sonar y deja que vuelva a llamar, con confirmación", async ({ page }) => {
        const api = await abrir(page, "/cuenta");
        const hasta = new Date(Date.now() + 7 * 60_000);
        api.voz.rechazadaPorRafaga(41, hasta, 2);
        await page.goto("/conversaciones?c=41");
        const rechazada = tarjeta(page, "rechazada");
        const hhmm = new Intl.DateTimeFormat("es-GT", { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: "America/Guatemala" }).format(hasta);
        await expect(rechazada).toContainText(`Demasiadas llamadas seguidas de este número; vuelve a sonar a las ${hhmm} · 2 intentos más`);
        await sinDesbordes(page);
        await captura(page, "rechazada-rafaga");
        await rechazada.getByRole("button", { name: "Dejar que vuelva a llamar" }).click();
        const dialogo = page.getByRole("alertdialog", { name: "¿Dejar que este número vuelva a llamar?" });
        await dialogo.getByRole("button", { name: "Dejar que vuelva a llamar" }).click();
        await expect(page.getByText("Listo: su próxima llamada vuelve a sonar")).toBeVisible();
        expect(api.voz.recibido.desbloqueos).toEqual([41]);
        await expect(rechazada).toContainText("Ya se dejó que vuelva a llamar");
        await expect(rechazada.getByRole("button", { name: "Dejar que vuelva a llamar" })).toHaveCount(0);
    });

    test("rechazada por ráfaga, vista por quien solo atiende: dice que fue automático, sin hora ni botón", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: { recibe: true, soloAtender: true } });
        api.voz.rechazadaPorRafaga(41, new Date(Date.now() + 7 * 60_000));
        await page.goto("/conversaciones?c=41");
        const rechazada = tarjeta(page, "rechazada");
        await expect(rechazada).toContainText("Iris la rechazó sola: demasiadas llamadas seguidas");
        await expect(rechazada.getByRole("button", { name: "Dejar que vuelva a llamar" })).toHaveCount(0);
    });

    test("el botón depende solo de `llamadas.desbloquear_numero`: sin `llamadas.configurar` no ve la hora, pero sí puede dejar que vuelva a llamar", async ({ page }) => {
        const api = await abrir(page, "/cuenta", { voz: { recibe: true, soloAtender: true, desbloquear: true } });
        api.voz.rechazadaPorRafaga(41, new Date(Date.now() + 7 * 60_000));
        await page.goto("/conversaciones?c=41");
        const rechazada = tarjeta(page, "rechazada");
        await expect(rechazada).toContainText("Iris la rechazó sola: demasiadas llamadas seguidas");
        await rechazada.getByRole("button", { name: "Dejar que vuelva a llamar" }).click();
        await page.getByRole("alertdialog", { name: "¿Dejar que este número vuelva a llamar?" }).getByRole("button", { name: "Dejar que vuelva a llamar" }).click();
        await expect(page.getByText("Listo: su próxima llamada vuelve a sonar")).toBeVisible();
        expect(api.voz.recibido.desbloqueos).toEqual([41]);
    });
});
