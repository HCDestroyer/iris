import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, EMPRESAS, simularApi, SUPERVISOR_ZONA, type OpcionesApi } from "./api-falsa";
import { AHORA_SIA, CODIGO_ENLACE, MODELOS_SIA, type OpcionesServidorIa } from "./api-servidor-ia";
import { confirmacion, esTelefono } from "./ayudas";
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
        await expect(slot(page, "cifra-uso")).toContainText("NVIDIA RTX 4090");
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

    test("sin tarjeta gráfica: lo dice y no inventa cifras de GPU", async ({ page }) => {
        await abrir(page, RUTA, { gpus: 0 });
        await expect(slot(page, "sin-tarjeta-grafica")).toContainText("No hay tarjeta gráfica detectada");
        await expect(slot(page, "cifra-uso")).toHaveCount(0);
        await expect(slot(page, "cifra-peticiones")).toBeVisible();
        await rango(page, "1 hora").click();
        await expect(slot(page, "series").getByRole("heading", { name: "Peticiones por minuto" })).toBeVisible();
        await expect(slot(page, "series").getByRole("heading", { name: "Uso y memoria de video" })).toHaveCount(0);
        await captura(page, "tablero-sin-tarjeta");
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
        await expect(slot(page, "que-revisar")).toContainText("sudo iris-voz estado");

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
        await expect(fuera.locator('[data-slot="comando"]')).toContainText("sudo iris-voz actualizar");
        await expect(slot(page, "cifras")).toHaveAttribute("data-vigentes", "no");
        await captura(page, "incompatible");
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
});

test.describe("Servidor de IA › enlazar", () => {
    const pasos = (page: Page) => slot(page, "pasos-enlace");

    test("de «Sin servidor» a activado: dirección, código de un solo uso, código incorrecto, huella con «Coincide» y «Activar»", async ({ page }) => {
        const api = await abrir(page, RUTA, { motor: "ninguno", conectar: ["401", "ok"] });
        await expect(slot(page, "sin-servidor")).toContainText("Sin servidor de IA");
        await captura(page, "sin-servidor");
        await page.getByRole("link", { name: "Enlazar servidor" }).click();
        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "direccion");
        await expect(pasos(page)).toContainText("sudo iris-voz enlazar");

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

        // Código mal escrito en el servidor (o vencido, o usado): 401 sin detalle; se puede reintentar.
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();
        await expect(slot(page, "error-enlace")).toContainText("El servidor no aceptó el código");
        await expect(slot(page, "codigo-enlace")).toContainText(CODIGO_ENLACE);
        await page.getByRole("button", { name: "Ya lo escribieron: conectar" }).click();

        await expect(pasos(page)).toHaveAttribute("data-paso-actual", "huella");
        // El código ya no está en pantalla.
        await expect(page.getByText(CODIGO_ENLACE)).toHaveCount(0);
        await expect(slot(page, "huella")).toContainText("3FA9");
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
        expect(api.servidorIa.recibido.activaciones).toEqual([{ version: 2 }]);
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
        expect(api.servidorIa.recibido.umbrales[0]).toMatchObject({ version: 2, temperaturaC: 80, vramPct: 95, saturacionRechazos: 10 });
        expect((api.servidorIa.recibido.umbrales[0] as { idempotencia?: string }).idempotencia).toBeTruthy();
        // El tablero ya usa el umbral nuevo.
        await expect(slot(page, "cifra-temperatura")).toContainText("80 °C");
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
        await expect(modelos).toContainText("sudo iris-voz modelo descargar");
        await expect(modelos).toContainText("sudo iris-voz modelo permitir");
        await esperarSinDesbordes(page);
        await captura(page, "mantenimiento");
    });

    test("rotar credenciales (convive 24 h) y «Revocar ahora» con confirmación que dice lo que pasa", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`);
        const cred = slot(page, "credenciales");
        await expect(cred).toContainText("Mientras tanto");
        await expect(slot(page, "ultima-rotacion")).toContainText("Última rotación");
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

    test("desenlazar pide confirmación, dice qué pasa con el asistente y vuelve a «Sin servidor»", async ({ page }) => {
        const api = await abrir(page, `${RUTA}/mantenimiento`);
        await slot(page, "desenlazar").getByRole("button", { name: "Desenlazar…" }).click();
        await expect(confirmacion(page)).toContainText("lo que atendía solo pasa a personas");
        await captura(page, "mantenimiento-desenlazar");
        await confirmacion(page).getByRole("button", { name: "Dejarlo enlazado" }).click();
        expect(api.servidorIa.recibido.desactivaciones).toHaveLength(0);
        await slot(page, "desenlazar").getByRole("button", { name: "Desenlazar…" }).click();
        await confirmacion(page).getByRole("button", { name: "Desenlazar", exact: true }).click();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        await expect(slot(page, "sin-servidor")).toBeVisible();
        expect(api.servidorIa.recibido.desactivaciones[0]).toMatchObject({ version: 4 });
    });
});

test.describe("Servidor de IA › quién lo ve", () => {
    test("el admin de una empresa ve el estado general y solo el uso de su empresa", async ({ page }) => {
        const api = await abrir(page, RUTA, {}, SUPERVISOR_ZONA);
        await expect(estado(page)).toContainText("En línea");
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

    test("en Integraciones, la tarjeta del servidor solo la ve quien administra la instalación", async ({ page }) => {
        await abrir(page, "/administracion/integraciones");
        const tarjeta = slot(page, "tarjeta-servidor-ia");
        await expect(tarjeta).toContainText("En línea");
        await expect(tarjeta).toContainText("versión 0.1.0");
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
    // PROVISIONAL: `servidorIa` aún no está en el tipo generado de los ajustes.
    const con = (ajustes: Partial<AjustesAsistente>, servidorIa: { disponible: boolean; modelos: string[] }): Partial<OpcionesApi> => ({
        conversaciones: { asistente: { ajustes: { ...ajustes, servidorIa } as Partial<AjustesAsistente> } },
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
