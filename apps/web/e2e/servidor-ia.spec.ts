import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, EMPRESAS, LECTOR, simularApi, SUPERVISOR_ZONA, type OpcionesApi } from "./api-falsa";
import { AHORA_SIA, CODIGO_ENLACE, HUELLA, HUELLA_OTRA, MODELOS_SIA, type OpcionesServidorIa } from "./api-servidor-ia";
import { confirmacion, esTelefono, irA } from "./ayudas";
import { elegirOpcion, textosOpciones } from "./controles";
import { esperarSinDesbordes } from "./desbordes";
import type { AjustesAsistente } from "../lib/asistente/tipos";

/*
 * Servidor de IA (contratos/voz.md §4.8, §4.11 y §4.12): enlazar con código de un solo uso y
 * huella, el tablero con sus rangos y su refresco, caído e incompatible sin cifras viejas, avisos y
 * umbrales, mantenimiento, lo que ve el admin de una empresa y el proveedor en el asistente. Con
 * reloj simulado, en los 4 proyectos y sin desbordes en 390 px ni en 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-servidor-ia");
const RUTA = "/administracion/servidor-ia";
const UNA = [EMPRESAS[0] ?? { id: 10, nombre: "Cable Norte" }];

async function abrir(page: Page, ruta: string, servidorIa: OpcionesServidorIa = {}, usuario = ADMIN, extra: Partial<OpcionesApi> = {}) {
    await page.clock.install({ time: AHORA_SIA });
    const api = await simularApi(page, { servidorIa, empresas: UNA, ...extra });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", usuario.correo);
    await page.goto(ruta);
    return api;
}
type Api = Awaited<ReturnType<typeof abrir>>;

const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: true });
const slot = (page: Page, nombre: string) => page.locator(`[data-slot="${nombre}"]`);
const estado = (page: Page) => slot(page, "estado-servidor");
const rango = (page: Page, nombre: string) => page.getByRole("radiogroup", { name: "Periodo" }).getByRole("radio", { name: nombre });

/** Adelanta el reloj de a poco hasta que se cumpla. */
const adelantarHasta = (page: Page, listo: () => boolean, paso = 2_000) =>
    expect
        .poll(
            async () => {
                if (listo()) return true;
                await page.clock.fastForward(paso);
                return listo();
            },
            { timeout: 25_000, intervals: [150] },
        )
        .toBe(true);

async function pestanaVisible(page: Page, visible: boolean) {
    await page.evaluate((v) => {
        Object.defineProperty(document, "visibilityState", { configurable: true, get: () => (v ? "visible" : "hidden") });
        document.dispatchEvent(new Event("visibilitychange"));
    }, visible);
}

test.describe("Servidor de IA › tablero", () => {
    test("línea de estado en llano, cifras de ahora, rangos, series con tabla, modelos, uso por empresa y errores", async ({ page }) => {
        await abrir(page, RUTA);
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        await expect(estado(page)).toContainText("En línea · versión 0.1.0 · compatible con esta versión de Iris · encendido hace 6 días");
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Servidor de IA");

        const cifras = slot(page, "cifras");
        await expect(cifras).toHaveAttribute("data-vigentes", "si");
        await expect(slot(page, "cifra-uso")).toContainText("63");
        await expect(slot(page, "cifra-uso")).toContainText("NVIDIA RTX 4090 · 310 W");
        // El resto del servidor: procesador, memoria y el disco de los modelos.
        await expect(slot(page, "cifra-cpu")).toContainText("12");
        await expect(slot(page, "cifra-cpu")).toContainText("16 núcleos");
        await expect(slot(page, "cifra-memoria")).toContainText("21.0");
        await expect(slot(page, "cifra-disco")).toContainText("60.0");
        await expect(slot(page, "cifra-disco")).toContainText("30 % · avisa al 90 %");
        await expect(slot(page, "plazas-insuficientes")).toHaveCount(0);
        await expect(slot(page, "cifra-vram")).toContainText("14.0");
        await expect(slot(page, "cifra-vram")).toContainText("58 %");
        await expect(slot(page, "cifra-temperatura")).toContainText("71");
        await expect(slot(page, "cifra-peticiones")).toContainText("de 4 a la vez");
        await expect(slot(page, "cifra-peticiones")).toContainText("Ninguna rechazada por saturación");
        // Sin cola: la palabra no aparece en el tablero.
        await expect(slot(page, "tablero-servidor-ia")).not.toContainText(/en cola|esperando/i);

        // Modelos: instalados, cargados y lo que ocupan.
        const modelos = slot(page, "modelos");
        await expect(modelos.locator('[data-modelo="qwen2.5:14b"]')).toContainText("En memoria · ocupa 10.0 GB");
        await expect(modelos.locator('[data-modelo="gemma3:27b"]')).toContainText("Sin cargar");
        await expect(modelos.locator('[data-modelo="gemma3:27b"]')).toContainText("no permitido");
        await expect(slot(page, "errores-recientes")).toContainText("Sin errores");
        await captura(page, "tablero-ahora");

        await rango(page, "24 horas").click();
        const series = slot(page, "series");
        await expect(series.getByRole("heading", { name: "Uso y memoria de video" })).toBeVisible();
        await expect(series.getByRole("heading", { name: "Peticiones por minuto" })).toBeVisible();
        await expect(series.getByRole("heading", { name: "Tiempo de respuesta" })).toBeVisible();
        await expect(series).toContainText("El 95 % tarda menos de");
        // El tramo en que no respondió se ve como tal, no como una línea en cero.
        await expect(series.locator('[data-slot="tramo-sin-datos"]').first()).toBeAttached();
        await series.getByRole("button", { name: "Ver tabla" }).first().click();
        await expect(series.locator('[data-slot="grafica-tabla"]').first()).toBeVisible();
        await series.getByRole("button", { name: "Ver gráfica" }).first().click();

        const uso = slot(page, "uso");
        await expect(uso.locator('[data-empresa="10"]')).toContainText("Cable Norte");
        await expect(uso.locator('[data-empresa="10"]')).toContainText("10,613");
        await expect(slot(page, "errores-recientes").locator('[data-error="tiempo_agotado"]')).toContainText("4 veces");
        await esperarSinDesbordes(page);
        await captura(page, "tablero-24h");
    });

    test("se refresca solo sin parpadear, se pausa con la pestaña oculta y vuelve al mostrarla", async ({ page }) => {
        const api = await abrir(page, RUTA);
        await expect(slot(page, "cifras")).toBeVisible();
        const antes = api.servidorIa.lecturas();
        // Una marca en el DOM: si el tablero pasara por «cargando», se perdería.
        await slot(page, "tablero-servidor-ia").evaluate((el) => el.setAttribute("data-marca", "1"));
        await adelantarHasta(page, () => api.servidorIa.lecturas() > antes);
        await expect(slot(page, "tablero-servidor-ia")).toHaveAttribute("data-marca", "1");
        await expect(page.locator('[aria-busy="true"]')).toHaveCount(0);

        await pestanaVisible(page, false);
        const oculta = api.servidorIa.lecturas();
        await page.clock.fastForward(60_000);
        expect(api.servidorIa.lecturas()).toBe(oculta);
        await pestanaVisible(page, true);
        await expect.poll(() => api.servidorIa.lecturas()).toBeGreaterThan(oculta);
        await expect(slot(page, "tablero-servidor-ia")).toHaveAttribute("data-marca", "1");
    });

    test("si una relectura falla no se quedan las cifras de antes: error con «Reintentar»", async ({ page }) => {
        const api = await abrir(page, RUTA);
        await expect(slot(page, "cifra-uso")).toContainText("63");
        api.servidorIa.fallarTablero(1);
        const leidas = api.servidorIa.recibido.tableros.length;
        await adelantarHasta(page, () => api.servidorIa.recibido.tableros.length > leidas);
        await expect(page.getByText("No se pudo cargar el servidor de IA")).toBeVisible();
        await expect(slot(page, "cifras")).toHaveCount(0);
        await expect(estado(page)).toHaveCount(0);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(slot(page, "cifra-uso")).toContainText("63");
    });

    test("dos tarjetas gráficas: cada una con sus cifras", async ({ page }) => {
        await abrir(page, RUTA, { gpus: 2 });
        await expect(slot(page, "cifras").locator("[data-gpu]")).toHaveCount(2);
        await expect(slot(page, "cifras")).toContainText("Tarjeta 1 · NVIDIA RTX 4090");
        await expect(slot(page, "cifras")).toContainText("Tarjeta 2 · NVIDIA RTX 3090");
        await expect(slot(page, "cifra-peticiones")).toHaveCount(1);
        await esperarSinDesbordes(page);
        await captura(page, "tablero-dos-tarjetas");
    });

    test("tarjeta gráfica «omitida»: «Sin datos» y un texto neutro; no afirma que no hay tarjeta", async ({ page }) => {
        await abrir(page, RUTA, { gpus: 0 });
        // `omitido` es «sin tarjeta o sin lectura»: no se sabe cuál.
        await expect(slot(page, "gpu-sin-lectura")).toHaveText("El servidor no reportó la tarjeta gráfica en esta lectura.");
        for (const cifra of ["cifra-uso", "cifra-vram", "cifra-temperatura"]) {
            await expect(slot(page, cifra)).toContainText("Sin datos");
            await expect(slot(page, cifra)).not.toContainText(/\d/);
        }
        await expect(slot(page, "sin-tarjeta-grafica")).toHaveCount(0);
        await expect(slot(page, "tablero-servidor-ia")).not.toContainText(/No hay tarjeta gráfica|sin tarjeta gráfica/i);
        // Lo que sí se leyó se sigue mostrando.
        await expect(slot(page, "cifra-peticiones")).toContainText("de 4 a la vez");
        await expect(slot(page, "cifra-cpu")).toContainText("12");
        await rango(page, "1 hora").click();
        await expect(slot(page, "series").getByRole("heading", { name: "Peticiones por minuto" })).toBeVisible();
        await expect(slot(page, "series").getByRole("heading", { name: "Uso y memoria de video" })).toHaveCount(0);
        await captura(page, "tablero-sin-tarjeta");
    });

    test("`sin_gpu`: el servidor respondió que no tiene tarjeta y entonces sí se dice", async ({ page }) => {
        await abrir(page, RUTA, { gpus: 0, sinGpu: true });
        await expect(slot(page, "sin-tarjeta-grafica")).toContainText("No hay tarjeta gráfica detectada");
        await expect(slot(page, "gpu-sin-lectura")).toHaveCount(0);
        await expect(slot(page, "cifra-uso")).toHaveCount(0);
        await expect(slot(page, "cifra-peticiones")).toContainText("de 4 a la vez");
        await expect(slot(page, "cifra-cpu")).toContainText("12");
        await esperarSinDesbordes(page);
        await captura(page, "tablero-sin-gpu");
    });

    test("saturación: rechazadas en la tarjeta, en el uso y en los avisos (no hay cola)", async ({ page }) => {
        await abrir(page, RUTA, { rechazadas: 12 });
        await expect(slot(page, "cifra-peticiones")).toContainText("12 rechazadas por saturación");
        await rango(page, "1 hora").click();
        await expect(slot(page, "uso").locator('[data-empresa="10"] [data-dato="Rechazadas"]')).toContainText("12");
        await page.getByRole("link", { name: "Avisos" }).click();
        const aviso = slot(page, "lista-avisos").locator('[data-aviso="saturacion"]');
        await expect(aviso).toContainText("Saturación");
        await expect(aviso).toContainText("Se rechazaron 12 peticiones");
        await expect(aviso).toContainText("Activo ahora");
    });

    test("más empresas que turnos: lo avisa en llano y lleva a Mantenimiento", async ({ page }) => {
        await abrir(page, RUTA, { plazasInsuficientes: true });
        const aviso = slot(page, "plazas-insuficientes");
        await expect(aviso).toContainText("Hay más empresas usando el servidor que turnos disponibles (6 empresas y 4 turnos)");
        await captura(page, "tablero-plazas");
        await aviso.getByRole("link", { name: "Ir a Mantenimiento" }).click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}/mantenimiento$`));
        await expect(slot(page, "quien-puede").locator('[data-slot="plazas-insuficientes"]')).toBeVisible();
    });

    test("«Probar» con el servidor en línea: los pasos en llano", async ({ page }) => {
        const api = await abrir(page, RUTA);
        await page.getByRole("button", { name: "Probar", exact: true }).click();
        const r = slot(page, "resultado-prueba");
        await expect(r).toHaveAttribute("data-estado", "ok");
        await expect(r.locator('[data-paso="conexion"]')).toContainText("Bien");
        await expect(r.locator('[data-paso="stt"]')).toContainText("No aplica");
        expect(api.servidorIa.recibido.pruebas).toBe(1);
    });
});

test.describe("Servidor de IA › caído o incompatible", () => {
    test("caído: «Sin datos» en las cifras, visto por última vez, qué pasa con el asistente y «Probar ahora»", async ({ page }) => {
        const api = await abrir(page, RUTA, { motor: "no_disponible" });
        const fuera = slot(page, "servidor-fuera");
        await expect(fuera).toHaveAttribute("data-estado", "no_disponible");
        await expect(fuera).toContainText("No responde");
        await expect(fuera).toContainText("Visto por última vez hoy a las 10:42 (hace 20 min)");
        const cifras = slot(page, "cifras");
        await expect(cifras).toHaveAttribute("data-vigentes", "no");
        await expect(cifras.getByText("Sin datos")).toHaveCount(4);
        // Ninguna cifra de «ahora»: ni uso, ni memoria, ni temperatura.
        await expect(cifras).not.toContainText(/\d/);
        await expect(slot(page, "modelos")).toHaveCount(0);
        await expect(slot(page, "mientras-tanto")).toContainText("Qué pasa con el asistente mientras tanto");
        await expect(slot(page, "mientras-tanto")).toContainText("Cable Norte");
        await expect(slot(page, "que-revisar")).toContainText("sudo iris-voz status");

        await page.getByRole("button", { name: "Probar ahora" }).click();
        await expect(slot(page, "resultado-prueba")).toHaveAttribute("data-estado", "error");
        await expect(slot(page, "resultado-prueba")).toContainText("El servidor no respondió en 2 segundos.");
        expect(api.servidorIa.recibido.pruebas).toBe(1);
        await esperarSinDesbordes(page);
        await captura(page, "caido");
    });

    test("se cae mientras lo miras: las cifras de antes desaparecen en la siguiente lectura", async ({ page }) => {
        const api = await abrir(page, RUTA);
        await expect(slot(page, "cifra-uso")).toContainText("63");
        api.servidorIa.caer();
        const leidas = api.servidorIa.lecturas();
        await adelantarHasta(page, () => api.servidorIa.lecturas() > leidas);
        await expect(slot(page, "servidor-fuera")).toBeVisible();
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await expect(slot(page, "cifra-uso")).not.toContainText("63");
    });

    test("incompatible porque el servidor es antiguo: dice que se actualiza en el servidor, con su comando", async ({ page }) => {
        await abrir(page, RUTA, { motor: "incompatible" });
        const fuera = slot(page, "servidor-fuera");
        await expect(fuera).toContainText("Versión incompatible");
        await expect(fuera).toContainText("Hay que actualizar Iris Voz en el servidor");
        await expect(fuera.locator('[data-slot="comando"]')).toContainText("sudo iris-voz upgrade");
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await captura(page, "incompatible");
    });

    test("incompatible por el protocolo o porque Iris es más nuevo: también se actualiza el servidor, con su porqué", async ({ page }) => {
        await abrir(page, RUTA, { motor: "incompatible_protocolo" });
        await expect(slot(page, "servidor-fuera")).toContainText("no hablan la misma versión del protocolo");
        await expect(slot(page, "servidor-fuera").locator('[data-slot="comando"]')).toContainText("sudo iris-voz upgrade");
        // Sin `versionDisponible` no se dice a qué versión.
        await expect(slot(page, "servidor-fuera")).not.toContainText("o una más nueva");
    });

    test("incompatible porque Iris es antiguo: dice que hay que actualizar Iris y no da comando", async ({ page }) => {
        await abrir(page, RUTA, { motor: "incompatible_iris" });
        const fuera = slot(page, "servidor-fuera");
        await expect(fuera).toContainText("Hay que actualizar Iris.");
        await expect(fuera).toContainText("en el servidor no hay que hacer nada");
        await expect(fuera.locator('[data-slot="comando"]')).toHaveCount(0);
    });

    test("en línea pero la última lectura falló: lo avisa y no muestra cifras de ahora", async ({ page }) => {
        await abrir(page, RUTA, { lecturaFallida: true });
        await expect(slot(page, "lectura-fallida")).toContainText("La última lectura buena fue hoy a las 10:59");
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await expect(slot(page, "cifras").getByText("Sin datos")).toHaveCount(4);
    });

    test("lectura «buena» pero de hace 5 minutos: tampoco se muestra como actual", async ({ page }) => {
        await abrir(page, RUTA, { lecturaVieja: true });
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        await expect(slot(page, "lectura-fallida")).toContainText("La última lectura del servidor ya es vieja");
        await expect(slot(page, "lectura-fallida")).toContainText("(hace 5 min)");
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await expect(slot(page, "cifras").getByText("Sin datos")).toHaveCount(4);
        await expect(slot(page, "cifras")).not.toContainText(/\d/);
        await expect(slot(page, "modelos")).toHaveCount(0);
    });

    test("con el reloj del equipo adelantado 2 minutos, una lectura fresca sigue a la vista (se mide con la hora del servidor)", async ({ page }) => {
        await abrir(page, RUTA, { relojAdelantado: 120_000 });
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "si");
        await expect(slot(page, "cifra-uso")).toContainText("63");
        await expect(slot(page, "lectura-fallida")).toHaveCount(0);
        await expect(slot(page, "modelos")).toBeVisible();
    });

    test("con el reloj adelantado, una lectura de verdad vieja se sigue marcando", async ({ page }) => {
        await abrir(page, RUTA, { relojAdelantado: 120_000, lecturaVieja: true });
        await expect(slot(page, "lectura-fallida")).toContainText("La última lectura del servidor ya es vieja");
        await expect(slot(page, "lectura-fallida")).toContainText("(hace 5 min)");
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await expect(slot(page, "cifras")).not.toContainText(/\d/);
    });

    test("degradado: quien administra la instalación lee el detalle técnico del servidor", async ({ page }) => {
        await abrir(page, RUTA, { motor: "degradado", detalle: "texto-secreto" });
        await expect(estado(page)).toHaveAttribute("data-estado", "degradado");
        await expect(estado(page)).toContainText("Con problemas");
        await expect(estado(page)).toContainText("texto-secreto");
    });
});

test.describe("Servidor de IA › sondeo", () => {
    const TABLERO = "/api/v1/mensajeria/servidor-ia/tablero";
    const lecturas = (api: Api) => api.peticiones.filter((p) => p.ruta === TABLERO).length;

    test("con 404 no insiste: una sola petición hasta que se pulsa «Reintentar»", async ({ page }) => {
        const api = await abrir(page, "/administracion");
        api.responderProblema(TABLERO, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 }, 1);
        await page.goto(RUTA);
        await expect(page.getByText("No se pudo cargar el servidor de IA")).toBeVisible();
        expect(lecturas(api)).toBe(1);
        await page.clock.fastForward(180_000);
        // Volver a la pestaña tampoco lo reintenta solo.
        await pestanaVisible(page, false);
        await pestanaVisible(page, true);
        await page.clock.fastForward(30_000);
        expect(lecturas(api)).toBe(1);
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(slot(page, "cifras")).toBeVisible();
        expect(lecturas(api)).toBe(2);
    });

    test("con 500 reintenta cada vez más espaciado y vuelve a su ritmo al primer éxito", async ({ page }) => {
        const api = await abrir(page, "/administracion");
        api.responderProblema(TABLERO, { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 4);
        await page.goto(RUTA);
        await expect(page.getByText("No se pudo cargar el servidor de IA")).toBeVisible();
        expect(lecturas(api)).toBe(1);
        // Fallos en 0 s, +10 s y +30 s: en 65 s son 3 peticiones (a ritmo fijo de 10 s serían 7).
        // (De a 5 s: `fastForward` dispara cada temporizador una sola vez por salto.)
        for (let i = 0; i < 13; i++) {
            await page.clock.fastForward(5_000);
            await page.waitForTimeout(60);
        }
        expect(lecturas(api)).toBe(3);
        // El cuarto fallo (+70 s) y el primer éxito (+130 s).
        await adelantarHasta(page, () => api.servidorIa.lecturas() >= 1, 10_000);
        await expect(slot(page, "cifras")).toBeVisible();
        const tras = api.servidorIa.lecturas();
        await adelantarHasta(page, () => api.servidorIa.lecturas() > tras);
    });
});

test.describe("Servidor de IA › enlazar", () => {
    const pasos = (page: Page) => slot(page, "pasos-enlace");

    test("de «Sin servidor» a activado: dirección, código de un solo uso, código incorrecto, huella con «Coincide» y «Activar»", async ({ page }) => {
        const api = await abrir(page, RUTA, { motor: "ninguno", conectar: ["fallo", "ok"] });
        await expect(slot(page, "sin-servidor")).toContainText("Sin servidor de IA");
        await captura(page, "sin-servidor");
        await page.getByRole("link", { name: "Enlazar servidor" }).click();
        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "direccion");
        await expect(pasos(page)).toContainText("sudo iris-voz pair");
        // Los comandos van en inglés, como el instalador: ninguno en español.
        await expect(pasos(page)).not.toContainText(/iris-voz (enlazar|desenlazar|estado|actualizar|modelo)/);

        // La dirección tiene que ser https.
        await page.getByLabel("Dirección").fill("http://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await expect(pasos(page)).toContainText("Tiene que ser como https://10.0.0.5:8443");
        expect(api.servidorIa.recibido.enlaces).toHaveLength(0);
        await page.getByLabel("Dirección").fill("https://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();

        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "codigo");
        await expect(slot(page, "codigo-enlace")).toContainText(CODIGO_ENLACE);
        await expect(slot(page, "codigo-vence")).toContainText("Vence a las 11:17");
        await expect(pasos(page)).toContainText("no se ve mientras se escribe");
        expect(api.servidorIa.recibido.enlaces[0]?.cuerpo).toEqual({ nombre: "Servidor de IA", url: "https://10.0.0.5:8443" });
        expect(api.servidorIa.recibido.enlaces[0]?.idempotencia).toBeTruthy();
        await captura(page, "enlazar-codigo");

        // Código mal escrito en el servidor (o usado): el servidor responde lo mismo que si no estuviera; se puede reintentar.
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();
        await expect(slot(page, "error-enlace")).toContainText("Puede que el código esté mal escrito allá o ya se haya usado");
        await expect(slot(page, "codigo-enlace")).toContainText(CODIGO_ENLACE);
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();

        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "huella");
        // El código ya no está en pantalla.
        await expect(page.getByText(CODIGO_ENLACE)).toHaveCount(0);
        await expect(slot(page, "huella")).toContainText("sha256:3fa91c07");
        await expect(slot(page, "enlace-caduca")).toContainText("caduca el");
        await expect(pasos(page)).toContainText("no actives");
        const activar = page.getByRole("button", { name: "Activar", exact: true });
        await expect(activar).toBeDisabled();
        await esperarSinDesbordes(page);
        await captura(page, "enlazar-huella");
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await activar.click();

        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        await expect(page.getByText("Servidor de IA activado")).toBeVisible();
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        expect(api.servidorIa.recibido.conexiones).toBe(2);
        // Va la huella que estaba en pantalla cuando se marcó «Coincide».
        expect(api.servidorIa.recibido.activaciones).toEqual([{ huellaTls: HUELLA }]);
    });

    test("si el servidor cambió después de comparar (409 `huella_cambiada`): lo dice, desmarca y enseña la huella nueva; no reintenta solo", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/enlazar`, { motor: "con_huella", activar: ["huella_cambiada", "ok"] });
        await expect(slot(page, "huella")).toContainText("sha256:3fa91c07");
        const coincide = page.getByRole("checkbox", { name: "Coincide con la del instalador" });
        await coincide.check();
        await page.getByRole("button", { name: "Activar", exact: true }).click();
        const error = slot(page, "error-enlace");
        await expect(error).toHaveAttribute("data-codigo", "huella_cambiada");
        await expect(error).toContainText("El servidor cambió desde que lo comparaste; vuelve a conectar y compara de nuevo.");
        // Se vuelve a comparar: la casilla queda sin marcar, con la huella de ahora, y «Activar» apagado.
        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "huella");
        await expect(coincide).not.toBeChecked();
        await expect(slot(page, "huella")).toContainText("sha256:9b04e2a1");
        await expect(slot(page, "huella")).not.toContainText("sha256:3fa91c07");
        await expect(page.getByRole("button", { name: "Activar", exact: true })).toBeDisabled();
        await page.clock.fastForward(30_000);
        expect(api.servidorIa.recibido.activaciones).toHaveLength(1);
        await captura(page, "enlazar-huella-cambiada");

        // Comparada la nueva, se activa con esa.
        await coincide.check();
        await page.getByRole("button", { name: "Activar", exact: true }).click();
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        expect(api.servidorIa.recibido.activaciones).toEqual([{ huellaTls: HUELLA }, { huellaTls: HUELLA_OTRA }]);
    });

    test("mientras se relee la huella tras el 409 la casilla no se puede marcar: no se activa con una huella que nadie comparó", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/enlazar`, { motor: "con_huella", activar: ["huella_cambiada"] });
        const coincide = page.getByRole("checkbox", { name: "Coincide con la del instalador" });
        const activar = page.getByRole("button", { name: "Activar", exact: true });
        await expect(slot(page, "huella")).toContainText("sha256:3fa91c07");
        // A partir de aquí, la relectura de la tarjeta se queda esperando.
        let soltar: () => void = () => undefined;
        const espera = new Promise<void>((resolver) => (soltar = resolver));
        await page.route("**/api/v1/mensajeria/voz/motores", async (route) => {
            await espera;
            await route.fallback();
        });
        await coincide.check();
        await activar.click();
        await expect(slot(page, "error-enlace")).toHaveAttribute("data-codigo", "huella_cambiada");
        // La huella vieja sigue en pantalla mientras llega la nueva: la casilla está apagada y un clic no la marca.
        await expect(slot(page, "huella")).toContainText("sha256:3fa91c07");
        await expect(coincide).toBeDisabled();
        await expect(coincide).not.toBeChecked();
        await coincide.click({ force: true, timeout: 2_000 }).catch(() => undefined);
        await expect(coincide).not.toBeChecked();
        // (El botón dice «Activando…» mientras tanto.)
        await expect(page.getByRole("button", { name: /^Activ/ })).toBeDisabled();
        soltar();
        await expect(slot(page, "huella")).toContainText("sha256:9b04e2a1");
        await expect(coincide).toBeEnabled();
        await expect(coincide).not.toBeChecked();
        await expect(activar).toBeDisabled();
        expect(api.servidorIa.recibido.activaciones).toEqual([{ huellaTls: HUELLA }]);
    });

    test("«Conectar» dice que el servidor pide un Iris más nuevo: se avisa en la huella y no se deja activar", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/enlazar`, { motor: "ninguno", conectar: ["incompatible_iris"] });
        await page.getByLabel("Dirección").fill("https://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();
        const aviso = slot(page, "enlace-incompatible");
        await expect(aviso).toContainText("Este servidor pide una versión de Iris más nueva; actualiza Iris");
        await expect(aviso.locator('[data-slot="comando"]')).toHaveCount(0);
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await expect(page.getByRole("button", { name: "Activar", exact: true })).toBeDisabled();
        expect(api.servidorIa.recibido.activaciones).toHaveLength(0);
        await esperarSinDesbordes(page);
        await captura(page, "enlazar-incompatible");
    });

    test("«Conectar» dice que el servidor es antiguo: avisa con el comando para actualizarlo y no deja activar", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`, { motor: "ninguno", conectar: ["incompatible_servidor"] });
        await page.getByLabel("Dirección").fill("https://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();
        const aviso = slot(page, "enlace-incompatible");
        await expect(aviso).toContainText("actualiza el servidor");
        await expect(aviso.locator('[data-slot="comando"]')).toContainText("sudo iris-voz upgrade");
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await expect(page.getByRole("button", { name: "Activar", exact: true })).toBeDisabled();
    });

    test("si la API rechaza «Activar» por incompatible (409), lo dice en llano", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`, { motor: "con_huella", activar: ["incompatible"] });
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await page.getByRole("button", { name: "Activar", exact: true }).click();
        await expect(slot(page, "error-enlace")).toHaveAttribute("data-codigo", "incompatible");
        await expect(slot(page, "error-enlace")).toContainText("no son compatibles");
        await expect(page).toHaveURL(new RegExp(`${RUTA}/enlazar$`));
    });

    test("la huella se muestra como la imprime el servidor (`sha256:` y minúsculas seguidas), y aparte en grupos", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`, { motor: "con_huella" });
        await expect(slot(page, "huella")).toHaveText(HUELLA);
        await expect(slot(page, "huella-grupos")).toContainText("3fa9");
        await expect(slot(page, "pasos-enlace")).toContainText("los espacios y las mayúsculas no cuentan");
        await expect(slot(page, "pasos-enlace")).not.toContainText("letra por letra");
    });

    test("activar con un cambio de llave en curso (409): pide esperar y deja reintentar a mano", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/enlazar`, { motor: "con_huella", activar: ["rotacion_en_curso", "ok"] });
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await page.getByRole("button", { name: "Activar", exact: true }).click();
        await expect(slot(page, "error-enlace")).toHaveAttribute("data-codigo", "rotacion_en_curso");
        await expect(slot(page, "error-enlace")).toContainText("Ya hay un cambio de llave en curso; espera unos segundos.");
        // Sigue en el mismo paso, con la casilla marcada.
        await expect(page.getByRole("checkbox", { name: "Coincide con la del instalador" })).toBeChecked();
        await page.getByRole("button", { name: "Activar", exact: true }).click();
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        expect(api.servidorIa.recibido.activaciones).toHaveLength(2);
    });

    test("la ventana del código vence a los 15 minutos: no deja conectar y ofrece generar otro", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/enlazar`, { motor: "ninguno" });
        await page.getByLabel("Dirección").fill("https://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await expect(slot(page, "codigo-vence")).toBeVisible();
        await page.clock.fastForward(15 * 60_000 + 2_000);
        await expect(slot(page, "codigo-vencido")).toContainText("El código venció");
        await expect(page.getByRole("button", { name: "Ya lo escribieron: conectar" })).toBeDisabled();
        expect(api.servidorIa.recibido.conexiones).toBe(0);
        await slot(page, "codigo-vencido").getByRole("button", { name: "Generar otro" }).click();
        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "direccion");
    });

    test("si el servidor dice que la ventana ya venció (409), se ofrece generar otro código", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`, { motor: "ninguno", conectar: ["vencido"] });
        await page.getByLabel("Dirección").fill("https://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();
        await expect(slot(page, "codigo-vencido")).toContainText("El código venció");
        await expect(slot(page, "error-enlace")).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Ya lo escribieron: conectar" })).toBeDisabled();
    });

    test("errores del servidor al empezar: dirección no permitida junto al campo y servidor que no responde", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`, { motor: "ninguno" });
        await page.getByLabel("Dirección").fill("https://127.0.0.1:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await expect(pasos(page)).toContainText("Esa dirección no se puede usar");
        await page.getByLabel("Dirección").fill("https://10.9.9.9:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await expect(slot(page, "error-enlace")).toContainText("Iris no pudo llegar al servidor en esa dirección");
    });

    test("enlace sin activar: se retoma en la huella; si ya caducó, no deja activar", async ({ page }) => {
        await abrir(page, RUTA, { motor: "con_huella", enlaceCaducado: true });
        await expect(page.getByText("El servidor está enlazado pero falta activarlo")).toBeVisible();
        await page.getByRole("link", { name: "Terminar de enlazar" }).click();
        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "huella");
        await expect(slot(page, "enlace-caducado")).toContainText("Este enlace caducó sin activarse");
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await expect(page.getByRole("button", { name: "Activar", exact: true })).toBeDisabled();
        await slot(page, "enlace-caducado").getByRole("button", { name: "Empezar de nuevo" }).click();
        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "direccion");
    });

    test("enlace a medias sin huella: el código no se vuelve a mostrar, hay que generar otro", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`, { motor: "a_medias" });
        await expect(page.getByText("Quedó un enlace a medias")).toBeVisible();
        await expect(page.getByLabel("Dirección")).toHaveValue("https://10.0.0.5:8443");
    });

    test("con un servidor ya enlazado no se enlaza otro", async ({ page }) => {
        await abrir(page, `${RUTA}/enlazar`);
        await expect(page.getByText("Ya hay un servidor de IA enlazado")).toBeVisible();
        await expect(pasos(page)).toHaveCount(0);
    });
});

test.describe("Servidor de IA › avisos y umbrales", () => {
    const form = (page: Page) => page.getByRole("form", { name: "Avisos del servidor de IA" });
    const abrirAvanzado = (page: Page) => page.getByRole("button", { name: /Avanzado: cuándo avisar/ }).click();

    test("avisos de la semana, umbrales de fábrica en «Avanzado», validación y guardar con version e Idempotency-Key", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/avisos`);
        const f = form(page);
        await expect(slot(page, "lista-avisos").locator('[data-aviso="temperatura"]')).toContainText("La tarjeta gráfica llegó a 88 °C");
        await expect(slot(page, "lista-avisos").locator('[data-aviso="temperatura"]')).toContainText("Resuelto");
        await expect(f).toContainText("«Servidor caído» y «Versión incompatible» avisan siempre");
        await expect(slot(page, "avanzado")).toContainText("Con los valores de fábrica");
        await expect(f.getByLabel("Temperatura: avisa si pasa de (°C)")).toHaveCount(0);
        await abrirAvanzado(page);
        await expect(f.getByLabel("Memoria de video: avisa si pasa de (%)")).toHaveValue("95");
        await expect(f.getByLabel("Disco de los modelos: avisa si pasa de (%)")).toHaveValue("90");
        await expect(f.getByLabel("Temperatura: avisa si pasa de (°C)")).toHaveValue("85");
        await expect(f.getByLabel("Saturación: avisa si se rechazan más de (peticiones)")).toHaveValue("10");
        await expect(f.getByLabel("Saturación: en (minutos)")).toHaveValue("5");
        await expect(f.getByLabel("Historial detallado: guardar (días)")).toHaveValue("7");
        await expect(f.getByLabel("Historial por hora: guardar (días)")).toHaveValue("90");

        await f.getByLabel("Temperatura: avisa si pasa de (°C)").fill("120");
        // El campo no deja pasar del rango del contrato (60 a 100 °C).
        await page.keyboard.press("Tab");
        await expect(f.getByLabel("Temperatura: avisa si pasa de (°C)")).toHaveValue("100");
        await f.getByLabel("Temperatura: avisa si pasa de (°C)").fill("80");
        await expect(slot(page, "avanzado")).toContainText("Con valores cambiados");
        await esperarSinDesbordes(page);
        await captura(page, "avisos-avanzado");
        await f.getByRole("button", { name: "Guardar" }).click();

        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        await expect(page.getByText("Avisos del servidor de IA guardados")).toBeVisible();
        expect(api.servidorIa.recibido.umbrales[0]).toMatchObject({ version: 2, temperaturaC: 80, vramPct: 95, saturacionRechazos: 10, retencionDetalleDias: 7, retencionHoraDias: 90 });
        expect((api.servidorIa.recibido.umbrales[0] as { idempotencia?: string }).idempotencia).toBeTruthy();
        // El tablero ya usa el umbral nuevo.
        await expect(slot(page, "cifra-temperatura")).toContainText("80 °C");
    });

    test("si el tablero no trae la lista de avisos, no dice «ninguno»: «Sin datos»", async ({ page }) => {
        await abrir(page, `${RUTA}/avisos`, { avisos: null });
        await expect(slot(page, "avisos-sin-datos")).toContainText("Sin datos");
        await expect(slot(page, "sin-avisos")).toHaveCount(0);
    });

    test("con la lista leída y vacía sí dice que no hubo ninguno", async ({ page }) => {
        await abrir(page, `${RUTA}/avisos`, { avisos: [] });
        await expect(slot(page, "sin-avisos")).toContainText("Ninguno en los últimos 7 días.");
        await expect(slot(page, "avisos-sin-datos")).toHaveCount(0);
    });

    test("«Valores de fábrica» los devuelve; 409 si alguien más los cambió; 422 del servidor junto al campo", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/avisos`, { conflicto: true });
        const f = form(page);
        await abrirAvanzado(page);
        await f.getByLabel("Saturación: avisa si se rechazan más de (peticiones)").fill("999");
        await f.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Alguien más cambió los avisos", { exact: true })).toBeVisible();
        await page.reload();
        await abrirAvanzado(page);
        await form(page).getByLabel("Saturación: avisa si se rechazan más de (peticiones)").fill("999");
        await form(page).getByRole("button", { name: "Guardar" }).click();
        await expect(form(page)).toContainText("Demasiado alto para este servidor");
        await form(page).getByRole("button", { name: "Valores de fábrica" }).click();
        await expect(form(page).getByLabel("Saturación: avisa si se rechazan más de (peticiones)")).toHaveValue("10");
        expect(api.servidorIa.recibido.umbrales).toHaveLength(2);
    });
});

test.describe("Servidor de IA › mantenimiento", () => {
    test("modelos permitidos: de los que declara el servidor se eligen los que ven las empresas", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`);
        const modelos = slot(page, "modelos-permitidos");
        await expect(modelos.getByRole("radio", { name: /Todos los del servidor/ })).toBeChecked();
        await expect(modelos.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await modelos.getByRole("radio", { name: /Solo algunos/ }).click();
        // «Solo algunos» empieza con ninguno marcado, y no deja guardar vacío sin avisar.
        await expect(modelos.getByRole("checkbox", { name: "llama3.1:8b" })).not.toBeChecked();
        await expect(modelos.getByRole("checkbox", { name: "qwen2.5:14b" })).not.toBeChecked();
        await modelos.getByRole("button", { name: "Guardar" }).click();
        await expect(modelos).toContainText("Elige al menos un modelo");
        expect(api.servidorIa.recibido.modelos).toHaveLength(0);
        await modelos.getByRole("checkbox", { name: "qwen2.5:14b" }).check();
        await captura(page, "mantenimiento-modelos");
        await modelos.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Guardados los modelos que pueden elegir las empresas")).toBeVisible();
        expect(api.servidorIa.recibido.modelos[0]).toMatchObject({ modelos: ["qwen2.5:14b"], version: 4 });
        expect((api.servidorIa.recibido.modelos[0] as { idempotencia?: string }).idempotencia).toBeTruthy();

        // De vuelta a «todos» se manda `null`, con la versión nueva.
        await modelos.getByRole("radio", { name: /Todos los del servidor/ }).click();
        await modelos.getByRole("button", { name: "Guardar" }).click();
        await expect.poll(() => api.servidorIa.recibido.modelos.length).toBe(2);
        expect(api.servidorIa.recibido.modelos[1]).toMatchObject({ modelos: null, version: 5 });
    });

    test("modelos permitidos: si el servidor ya no tiene uno de los elegidos (422), lo dice", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`, { modelosPermitidos: ["qwen2.5:14b"] });
        const modelos = slot(page, "modelos-permitidos");
        await expect(modelos.getByRole("radio", { name: /Solo algunos/ })).toBeChecked();
        await expect(modelos.getByRole("checkbox", { name: "llama3.1:8b" })).not.toBeChecked();
        api.servidorIa.quitarDelCatalogo("llama3.1:8b");
        await modelos.getByRole("checkbox", { name: "llama3.1:8b" }).check();
        await modelos.getByRole("button", { name: "Guardar" }).click();
        await expect(slot(page, "error-modelos-permitidos")).toContainText("ya no está en el servidor");
    });

    test("quién puede usarlo y simultáneas por empresa; modelos permitidos con sus comandos", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`);
        const quien = slot(page, "quien-puede");
        await expect(quien.getByRole("radio", { name: /Todas las empresas/ })).toBeChecked();
        await quien.getByRole("radio", { name: /Solo ciertas empresas/ }).click();
        await quien.getByRole("button", { name: "Guardar" }).click();
        await expect(quien).toContainText("Elige al menos una empresa");
        await quien.getByRole("checkbox", { name: "Cable Norte" }).check();
        await quien.getByLabel("Peticiones a la vez por empresa, como mucho").fill("3");
        await quien.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Guardado quién puede usar el servidor de IA")).toBeVisible();
        expect(api.servidorIa.recibido.empresas[0]).toMatchObject({ empresas: [10], simultaneasPorEmpresa: 3, version: 4 });
        expect((api.servidorIa.recibido.empresas[0] as { idempotencia?: string }).idempotencia).toBeTruthy();

        const modelos = slot(page, "modelos-permitidos");
        for (const m of MODELOS_SIA) await expect(modelos).toContainText(m);
        await expect(modelos).toContainText("sudo iris-voz model pull <nombre>");
        await expect(modelos).toContainText("sudo iris-voz model allow <nombre>");
        await expect(modelos).toContainText("sudo iris-voz model remove <nombre>");
        await expect(page.locator("main")).not.toContainText(/iris-voz (enlazar|desenlazar|estado|actualizar|modelo)/);
        await esperarSinDesbordes(page);
        await captura(page, "mantenimiento");
    });

    test("rotar con otro cambio de llave en curso (409): pide esperar, sin error grave", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`, { rotarEnCurso: 1 });
        const cred = slot(page, "credenciales");
        await cred.getByRole("button", { name: "Rotar credenciales" }).click();
        await confirmacion(page).getByRole("button", { name: "Rotar", exact: true }).click();
        await expect(slot(page, "rotacion-en-curso")).toContainText("Ya hay un cambio de llave en curso; espera unos segundos.");
        await expect(slot(page, "error-credenciales")).toHaveCount(0);
        await expect(slot(page, "ultima-rotacion")).toContainText("2 de noviembre");
        await cred.getByRole("button", { name: "Rotar credenciales" }).click();
        await confirmacion(page).getByRole("button", { name: "Rotar", exact: true }).click();
        await expect(page.getByText("Credenciales rotadas")).toBeVisible();
        await expect(slot(page, "rotacion-en-curso")).toHaveCount(0);
        expect(api.servidorIa.recibido.rotaciones).toHaveLength(2);
    });

    test("rotar sin respuesta: no afirma si se cambió, relee la tarjeta y el reintento repite la misma clave", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`, { rotarSinRespuesta: 1 });
        const cred = slot(page, "credenciales");
        await expect(slot(page, "ultima-rotacion")).toContainText("2 de noviembre");
        await cred.getByRole("button", { name: "Rotar credenciales" }).click();
        await confirmacion(page).getByRole("button", { name: "Rotar", exact: true }).click();
        const error = slot(page, "error-credenciales");
        await expect(error).toHaveAttribute("data-sin-confirmar", "true");
        await expect(error).toContainText("No se pudo confirmar el cambio. Iris recupera la conexión sola; no hace falta enlazar de nuevo.");
        await expect(error).not.toContainText(/no se cambió nada|sigue valiendo la llave actual/i);
        // Lo releído: el servidor sí la había rotado.
        await expect(slot(page, "ultima-rotacion")).toContainText("17 de noviembre");
        await expect(page.getByText("Credenciales rotadas")).toHaveCount(0);

        await cred.getByRole("button", { name: "Rotar credenciales" }).click();
        await confirmacion(page).getByRole("button", { name: "Rotar", exact: true }).click();
        await expect(page.getByText("Credenciales rotadas")).toBeVisible();
        await expect(error).toHaveCount(0);
        const claves = api.servidorIa.recibido.clavesRotar;
        expect(claves).toHaveLength(2);
        expect(claves[0]).toBeTruthy();
        expect(claves[1]).toBe(claves[0]);
    });

    test("rotar credenciales (convive 24 h) y «Revocar ahora» con confirmación que dice lo que pasa", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`);
        const cred = slot(page, "credenciales");
        await expect(cred).toContainText("Mientras tanto");
        // Con fecha y hora.
        await expect(slot(page, "ultima-rotacion")).toHaveText("Última rotación: 2 de noviembre, 09:00.");
        await cred.getByRole("button", { name: "Rotar credenciales" }).click();
        await expect(confirmacion(page)).toContainText("La anterior sigue valiendo 24 horas");
        await confirmacion(page).getByRole("button", { name: "Rotar", exact: true }).click();
        await expect(page.getByText("Credenciales rotadas")).toBeVisible();
        await cred.getByRole("button", { name: "Revocar ahora" }).click();
        await expect(confirmacion(page)).toContainText("dejan de valer en el acto");
        await captura(page, "mantenimiento-revocar");
        await confirmacion(page).getByRole("button", { name: "Revocar ahora" }).click();
        await expect(page.getByText("Credenciales revocadas y cambiadas")).toBeVisible();
        expect(api.servidorIa.recibido.rotaciones).toEqual([{}, { revocar: true }]);
    });

    test("ciclo completo: activo → desactivar → «Enlazar de nuevo» → activo; desactivado tiene salida en el tablero y en Mantenimiento", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`);
        const seccion = slot(page, "desactivar");
        // Dice la verdad: Iris deja de usarlo; el enlace del lado del servidor se borra allá.
        await expect(seccion).toContainText("Iris deja de usarlo en el acto");
        await expect(seccion.locator('[data-slot="comando"]')).toContainText("sudo iris-voz unpair");
        await expect(page.getByRole("button", { name: /Desenlazar/ })).toHaveCount(0);
        await seccion.getByRole("button", { name: "Desactivar…" }).click();
        await expect(confirmacion(page)).toContainText("lo que atendía solo pasa a personas");
        await captura(page, "mantenimiento-desactivar");
        await confirmacion(page).getByRole("button", { name: "Dejarlo activo" }).click();
        expect(api.servidorIa.recibido.desactivaciones).toHaveLength(0);
        await seccion.getByRole("button", { name: "Desactivar…" }).click();
        await confirmacion(page).getByRole("button", { name: "Desactivar", exact: true }).click();
        await expect.poll(() => api.servidorIa.recibido.desactivaciones.length).toBe(1);
        expect(api.servidorIa.recibido.desactivaciones[0]).toMatchObject({ motivo: "desactivado_desde_el_panel" });

        // Tablero: el estado dicho con claridad, sin «Probar», y una salida.
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        const fuera = slot(page, "servidor-fuera");
        await expect(fuera).toHaveAttribute("data-estado", "desactivado");
        await expect(fuera).toContainText("Desactivado");
        await expect(fuera).toContainText("Para volver a usarlo hay que enlazarlo de nuevo");
        await expect(page.getByRole("button", { name: /^Probar/ })).toHaveCount(0);
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await esperarSinDesbordes(page);
        await captura(page, "desactivado");

        // Mantenimiento ya no ofrece desactivar otra vez: ofrece enlazar de nuevo.
        await page.getByRole("link", { name: "Mantenimiento" }).click();
        await expect(slot(page, "servidor-desactivado")).toContainText("El servidor está desactivado");
        await expect(slot(page, "desactivar")).toHaveCount(0);
        await slot(page, "servidor-desactivado").getByRole("link", { name: "Enlazar de nuevo" }).click();

        // `/enlazar` acepta el servidor desactivado y recuerda su dirección.
        await expect(slot(page, "pasos-enlace")).toHaveAttribute("data-paso-actual", "direccion");
        await expect(slot(page, "reenlace")).toContainText("sudo iris-voz unpair");
        await expect(page.getByText("Ya hay un servidor de IA enlazado")).toHaveCount(0);
        await expect(page.getByLabel("Dirección")).toHaveValue("https://10.0.0.5:8443");
        await page.getByRole("button", { name: "Generar el código" }).click();
        await expect(slot(page, "codigo-enlace")).toContainText(CODIGO_ENLACE);
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();
        await expect(slot(page, "huella")).toContainText("sha256:3fa91c07");
        await page.getByRole("checkbox", { name: "Coincide con la del instalador" }).check();
        await page.getByRole("button", { name: "Activar", exact: true }).click();
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        await expect(slot(page, "cifra-uso")).toContainText("63");
        expect(api.servidorIa.recibido.enlaces).toHaveLength(1);
        expect(api.servidorIa.recibido.activaciones).toHaveLength(1);
    });

    test("desactivado: el tablero lleva a «Enlazar de nuevo» y, si la API rechaza «Probar» (409), el texto es llano", async ({ page }) => {
        const api = await abrir(page, RUTA);
        await expect(estado(page)).toHaveAttribute("data-estado", "listo");
        // Otra persona lo desactiva mientras tanto: «Probar» sigue en pantalla hasta la siguiente lectura.
        api.servidorIa.desactivar();
        await page.getByRole("button", { name: "Probar", exact: true }).click();
        await expect(slot(page, "error-prueba")).toContainText("El servidor está desactivado: Iris no lo usa.");
        const leidas = api.servidorIa.lecturas();
        await adelantarHasta(page, () => api.servidorIa.lecturas() > leidas);
        await expect(slot(page, "servidor-fuera")).toHaveAttribute("data-estado", "desactivado");
        await slot(page, "servidor-fuera").getByRole("link", { name: "Enlazar de nuevo" }).click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}/enlazar$`));
    });
});

test.describe("Servidor de IA › quién lo ve", () => {
    test("el admin de una empresa ve el estado general y solo el uso de su empresa", async ({ page }) => {
        const api = await abrir(page, RUTA, {}, SUPERVISOR_ZONA);
        await expect(estado(page)).toContainText("En línea");
        // `encendidoSeg` le llega nulo: la línea no inventa cuánto lleva encendido.
        await expect(estado(page)).toHaveText("En línea · versión 0.1.0 · compatible con esta versión de Iris");
        await expect(page.getByRole("navigation", { name: "Secciones del Servidor de IA" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "Probar", exact: true })).toHaveCount(0);
        await expect(slot(page, "cifras")).toHaveCount(0);
        await expect(slot(page, "modelos")).toHaveCount(0);
        await expect(slot(page, "uso")).toHaveCount(0);
        await rango(page, "24 horas").click();
        const uso = slot(page, "uso-empresa");
        await expect(uso).toContainText("Uso de tu empresa en las últimas 24 horas");
        await expect(uso.locator('[data-slot="cifra-peticiones"]')).toContainText("10,613");
        await expect(uso.locator('[data-slot="cifra-errores"]')).toContainText("4");
        // Sus series: peticiones y tiempos. Nada de la tarjeta gráfica.
        await expect(slot(page, "series").getByRole("heading", { name: "Peticiones por minuto" })).toBeVisible();
        await expect(slot(page, "series")).toContainText("De tu empresa");
        await expect(slot(page, "series").getByRole("heading", { name: "Uso y memoria de video" })).toHaveCount(0);
        await expect(slot(page, "tablero-servidor-ia")).not.toContainText(/Memoria de video|Temperatura|saturación/);
        await expect(slot(page, "nota-admin-empresa")).toContainText("Como administrador de una empresa");
        expect(api.servidorIa.recibido.tableros).toContain("24h");
        await esperarSinDesbordes(page);
        await captura(page, "admin-empresa");

        // Lo de quien administra la instalación no se le abre.
        await page.goto(`${RUTA}/mantenimiento`);
        await expect(slot(page, "quien-puede")).toHaveCount(0);
        await expect(slot(page, "credenciales")).toHaveCount(0);
    });

    test("campana: caído, incompatible y umbral, cada uno con su texto, y llevan al tablero", async ({ page }) => {
        type AvisoPanel = NonNullable<OpcionesApi["avisosPanel"]>[number];
        const aviso = (id: number, tipo: AvisoPanel["tipo"], titulo: string, datos: AvisoPanel["datos"] = {}): AvisoPanel => ({
            id,
            tipo,
            nivel: tipo === "servidor_ia_umbral" ? "alerta" : "peligro",
            titulo,
            enlace: RUTA,
            datos,
            leido: false,
            leidoEn: null,
            creadoEn: "2026-11-17T16:42:00Z",
            empresaId: null,
        });
        await abrir(page, "/administracion", {}, ADMIN, {
            avisosPanel: [
                aviso(31, "servidor_ia_caido", "El servidor de IA no responde"),
                aviso(32, "servidor_ia_incompatible", "El servidor de IA no es compatible con esta versión de Iris"),
                aviso(33, "servidor_ia_umbral", "Temperatura alta en el servidor de IA", { tipo: "temperatura", valor: 88, umbral: 85, veces: 2 }),
            ],
        });
        await page.getByRole("button", { name: /^Avisos/ }).click();
        const panel = page.getByRole("dialog", { name: "Avisos" });
        await expect(panel.locator('[data-aviso="servidor_ia_caido"]')).toContainText("El servidor de IA no responde");
        await expect(panel.locator('[data-aviso="servidor_ia_caido"]')).toContainText("pasa a personas hasta que vuelva");
        await expect(panel.locator('[data-aviso="servidor_ia_incompatible"]')).toContainText("El tablero dice qué hay que actualizar");
        await expect(panel.locator('[data-aviso="servidor_ia_umbral"]')).toContainText("La tarjeta gráfica llegó a 88 °C");
        // El que se repitió en la hora dice cuántas veces; el que pasó una sola vez, no.
        await expect(panel.locator('[data-aviso="servidor_ia_umbral"]')).toContainText("Ya van 2 veces en esta hora.");
        await expect(panel.locator('[data-aviso="servidor_ia_caido"]')).not.toContainText("veces");
        await captura(page, "campana");
        await panel.locator('[data-aviso="servidor_ia_caido"]').getByRole("link").first().click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
    });

    test("el admin de una empresa no lee el detalle técnico del servidor aunque llegue", async ({ page }) => {
        await abrir(page, RUTA, { motor: "degradado", detalle: "texto-secreto" }, SUPERVISOR_ZONA);
        await expect(estado(page)).toHaveAttribute("data-estado", "degradado");
        await expect(estado(page)).toContainText("Con problemas");
        await expect(page.locator("body")).not.toContainText("texto-secreto");
        await rango(page, "24 horas").click();
        await expect(slot(page, "uso-empresa")).toBeVisible();
        await expect(page.locator("body")).not.toContainText("texto-secreto");
    });

    test("con `servidor_ia.administrar` pero sin `empresas.todas`: «Sin permiso» en las cuatro pantallas y ninguna petición", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`, { zonaAdministra: true }, SUPERVISOR_ZONA);
        for (const ruta of [`${RUTA}/mantenimiento`, `${RUTA}/enlazar`, `${RUTA}/avisos`, RUTA]) {
            await page.goto(ruta);
            await expect(slot(page, "sin-permiso")).toContainText("No tienes permiso para ver esta sección");
            await expect(page.getByRole("navigation", { name: "Secciones del Servidor de IA" })).toHaveCount(0);
        }
        await page.goto("/administracion/integraciones");
        await expect(page.getByRole("heading", { name: "Integraciones" }).first()).toBeVisible();
        await expect(slot(page, "tarjeta-servidor-ia")).toHaveCount(0);
        expect(api.peticiones.filter((p) => p.ruta.includes("/voz/motores") || p.ruta.includes("/servidor-ia/"))).toHaveLength(0);
    });

    test("con solo `servidor_ia.ver_uso` hay un camino visible: Administración → Servidor de IA → su tablero de uso", async ({ page }) => {
        await abrir(page, "/", { soloUso: true }, LECTOR);
        await irA(page, "Administración");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toHaveCount(0);
        await page.getByRole("link", { name: /Servidor de IA/ }).click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        await expect(estado(page)).toContainText("En línea");
        await expect(slot(page, "uso-empresa")).toBeVisible();
        await expect(slot(page, "nota-admin-empresa")).toBeVisible();
    });

    test("campana sin empresa elegida: los avisos de la instalación se ven igual, sin error", async ({ page }) => {
        // Dos empresas y ninguna elegida: `/yo/avisos` no depende de la empresa activa.
        const api = await abrir(page, "/administracion", {}, ADMIN, {
            empresas: EMPRESAS,
            avisosPanel: [{ id: 41, tipo: "servidor_ia_caido", nivel: "peligro", titulo: "El servidor de IA no responde", enlace: RUTA, datos: { veces: 1 }, leido: false, leidoEn: null, creadoEn: "2026-11-17T16:42:00Z", empresaId: null }],
        });
        const campana = page.getByRole("button", { name: /^Avisos/ });
        await expect(campana).toHaveAccessibleName(/1 aviso sin leer/);
        await campana.click();
        const panel = page.getByRole("dialog", { name: "Avisos" });
        await expect(panel.locator('[data-aviso="servidor_ia_caido"]')).toContainText("El servidor de IA no responde");
        await expect(panel.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
        const pedidos = api.peticiones.filter((p) => p.ruta === "/api/v1/yo/avisos");
        expect(pedidos.length).toBeGreaterThan(0);
        expect(pedidos.every((p) => p.empresa === undefined)).toBe(true);
    });

    test("en Integraciones, la tarjeta del servidor solo la ve quien administra la instalación", async ({ page }) => {
        await abrir(page, "/administracion/integraciones");
        const tarjeta = slot(page, "tarjeta-servidor-ia");
        await expect(tarjeta).toContainText("En línea");
        await expect(tarjeta).toContainText("versión 0.1.0");
        await expect(tarjeta).toContainText("visto por última vez hoy a las 11:01");
        await tarjeta.getByRole("link", { name: "Abrir el tablero" }).click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
    });

    test("en Integraciones, el admin de una empresa no ve la tarjeta; en Administración sí tiene la entrada", async ({ page }) => {
        await abrir(page, "/administracion/integraciones", {}, SUPERVISOR_ZONA);
        await expect(page.getByRole("heading", { name: "Integraciones" }).first()).toBeVisible();
        await expect(slot(page, "tarjeta-servidor-ia")).toHaveCount(0);
        await page.goto("/administracion");
        await page.getByRole("link", { name: /Servidor de IA/ }).click();
        await expect(estado(page)).toBeVisible();
    });
});

test.describe("Servidor de IA › en el asistente de cada empresa", () => {
    const f = (page: Page) => page.getByRole("form", { name: "Conexión con la IA" });
    const CONEXION = "/administracion/asistente/conexion";
    const con = (ajustes: Partial<AjustesAsistente>, servidorIa: { disponible: boolean; modelos: string[] }): Partial<OpcionesApi> => ({
        conversaciones: { asistente: { ajustes: { ...ajustes, servidorIa } } },
    });
    const guardados = (api: Api) => api.conversaciones.asistente.guardados as Record<string, unknown>[];

    test("se elige con un modelo de los permitidos, sin dirección ni clave; al venir de un proveedor con clave se manda clave vacía", async ({ page }) => {
        const api = await abrir(page, CONEXION, {}, ADMIN, con({ proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: "2026-10-01T15:00:00Z" }, { disponible: true, modelos: MODELOS_SIA }));
        await f(page).locator('[data-opcion="servidor_ia"]').click();
        await expect(f(page).getByRole("radio", { name: "Servidor de IA" })).toBeChecked();
        await expect(slot(page, "servidor-ia-disponible")).toContainText("Disponible");
        await expect(f(page).getByLabel("Dirección")).toHaveCount(0);
        await expect(f(page).getByLabel("Clave")).toHaveCount(0);
        await expect(slot(page, "clave-se-borra")).toContainText("se borra al guardar");
        await expect(f(page)).toContainText("Pruébalo antes de guardar");
        await expect(f(page)).toContainText("Elige un modelo para probarlo.");
        const modelo = f(page).getByLabel("Modelo que usa el asistente");
        expect(await textosOpciones(modelo)).toEqual(["Elegir…", ...MODELOS_SIA]);
        await elegirOpcion(modelo, "qwen2.5:14b");
        await f(page).getByRole("button", { name: "Probar", exact: true }).click();
        await expect(f(page).locator('[data-prueba="ok"]')).toContainText("Respondió en 1.8 s");
        expect(api.conversaciones.asistente.pruebas[0]).toEqual({ proveedor: "servidor_ia", url: "", modelo: "qwen2.5:14b" });
        await esperarSinDesbordes(page);
        await captura(page, "asistente-servidor-ia");
        await f(page).getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Conexión con la IA guardada")).toBeVisible();
        expect(guardados(api)[0]).toMatchObject({ proveedor: "servidor_ia", url: "", modelo: "qwen2.5:14b", clave: "" });
    });

    test("sin clave guardada no se manda clave; si el servidor responde 422, el mensaje es llano", async ({ page }) => {
        const api = await abrir(page, CONEXION, {}, ADMIN, con({ claveGuardadaEn: null }, { disponible: true, modelos: MODELOS_SIA }));
        await f(page).locator('[data-opcion="servidor_ia"]').click();
        await expect(slot(page, "clave-se-borra")).toHaveCount(0);
        await elegirOpcion(f(page).getByLabel("Modelo que usa el asistente"), "llama3.1:8b");
        api.responderProblema("/api/v1/mensajeria/asistente/ajustes/actualizar", { type: "urn:iris:problema:servidor_ia_no_disponible", title: "Servidor de IA no disponible", status: 422 }, 1);
        await f(page).getByRole("button", { name: "Guardar" }).click();
        await expect(slot(page, "error-guardar")).toContainText("El Servidor de IA no está disponible");
        await expect(slot(page, "error-guardar")).toContainText("la empresa no está entre las permitidas");
        await f(page).getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Conexión con la IA guardada")).toBeVisible();
        expect(guardados(api)[0]).toMatchObject({ proveedor: "servidor_ia", modelo: "llama3.1:8b" });
        expect(guardados(api)[0]).not.toHaveProperty("clave");
    });

    test("recorrido real: Conexión → guardar «Servidor de IA» → pantalla principal sin «Falta conectar la IA» → encender → «Pruébalo»", async ({ page }) => {
        // Como lo deja la API real: con `servidor_ia` la dirección queda `""` y no hay clave.
        const api = await abrir(page, CONEXION, {}, ADMIN, con({ activo: false, proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: null }, { disponible: true, modelos: MODELOS_SIA }));
        await f(page).locator('[data-opcion="servidor_ia"]').click();
        await elegirOpcion(f(page).getByLabel("Modelo que usa el asistente"), "qwen2.5:14b");
        await f(page).getByRole("button", { name: "Guardar" }).click();
        expect(guardados(api)[0]).toMatchObject({ proveedor: "servidor_ia", url: "", modelo: "qwen2.5:14b" });
        expect(guardados(api)[0]).not.toHaveProperty("clave");

        // De vuelta en la pantalla principal: conectado, nada apagado.
        const principal = page.getByRole("form", { name: "Asistente", exact: true });
        await expect(principal).toBeVisible();
        await expect(page.getByText("Falta conectar la IA")).toHaveCount(0);
        const encendido = page.getByRole("switch", { name: "Asistente encendido" });
        await expect(encendido).toBeEnabled();
        await expect(encendido).not.toBeChecked();
        await encendido.click();
        await expect(encendido).toBeChecked();

        const caja = principal.getByRole("textbox", { name: "Mensaje de prueba" });
        await expect(caja).toBeEnabled();
        await caja.fill("No tengo internet desde la mañana");
        await principal.locator('[data-slot="pruebalo"]').getByRole("button", { name: "Probar" }).click();
        await expect(principal.getByRole("status", { name: "Resultado de la prueba" })).toBeVisible();

        await principal.getByRole("button", { name: "Guardar" }).click();
        await expect.poll(() => guardados(api).length).toBe(2);
        expect(guardados(api)[1]).toMatchObject({ activo: true, proveedor: "servidor_ia", url: "", modelo: "qwen2.5:14b" });
        await captura(page, "asistente-principal-servidor-ia");
    });

    test("«Pruébalo» con el Servidor de IA caído: texto llano propio, con «Reintentar»", async ({ page }) => {
        const api = await abrir(page, "/administracion/asistente", {}, ADMIN, con({ proveedor: "servidor_ia", url: "", modelo: "qwen2.5:14b", claveGuardadaEn: null }, { disponible: false, modelos: [] }));
        const principal = page.getByRole("form", { name: "Asistente", exact: true });
        await expect(page.getByText("Falta conectar la IA")).toHaveCount(0);
        api.responderProblema("/api/v1/mensajeria/asistente/probar-mensaje", { type: "urn:iris:problema:servidor_ia_no_disponible", title: "servidor_ia_no_disponible", status: 422, detail: "servidor_ia_no_disponible" }, 1);
        await principal.getByRole("textbox", { name: "Mensaje de prueba" }).fill("Hola");
        const zona = principal.locator('[data-slot="pruebalo"]');
        await zona.getByRole("button", { name: "Probar" }).click();
        await expect(zona).toContainText("El Servidor de IA no está disponible");
        await expect(zona).toContainText("Avisa a quien administra la instalación");
        await expect(zona).not.toContainText("servidor_ia_no_disponible");
        await expect(zona.getByRole("button", { name: "Reintentar" })).toBeVisible();
    });

    test("si no está enlazado o la empresa no está permitida, lo dice en llano antes de guardar", async ({ page }) => {
        await abrir(page, CONEXION, {}, ADMIN, con({}, { disponible: false, modelos: [] }));
        await expect(slot(page, "servidor-ia-disponible")).toContainText("No disponible ahora");
        await f(page).locator('[data-opcion="servidor_ia"]').click();
        await expect(slot(page, "servidor-ia-no-disponible")).toContainText("Pídele a quien administra la instalación que lo revise");
        await captura(page, "asistente-no-disponible");
    });

    test("si la API todavía no manda `servidorIa`, la opción no se ofrece", async ({ page }) => {
        await abrir(page, CONEXION, {}, ADMIN);
        await expect(f(page).getByRole("radio", { name: "Ollama · servidor propio" })).toBeVisible();
        await expect(f(page).locator('[data-opcion="servidor_ia"]')).toHaveCount(0);
    });
});
