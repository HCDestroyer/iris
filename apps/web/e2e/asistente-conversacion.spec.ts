import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { ADMIN, CONSULTA, conSesion, simularApi } from "./api-falsa";
import { esTelefono } from "./ayudas";
import { AHORA_CONV, detalle, mensaje } from "./datos-conversaciones";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";

/*
 * El asistente por conversación (contratos/conversaciones.md §17.11 y §16.4): quién atiende
 * (`atencionAsistente`), pausar y reanudar con su permiso (y el aviso antes de devolvérsela si el
 * cliente pidió una persona), el resumen guardado en la ficha y los eventos de sistema del asistente
 * en el hilo. Primero en el teléfono; en los 4 proyectos y sin desbordes.
 */

type Atencion = Schema<"AtencionAsistente">;
type Opciones = NonNullable<Parameters<typeof simularApi>[1]>;
type Api = Awaited<ReturnType<typeof simularApi>>;

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-asistente-conversacion");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });
const hace = (min: number) => new Date(AHORA_CONV - min * 60_000).toISOString();

const conv = (page: Page) => page.getByRole("region", { name: /^Conversación con / });
const franja = (page: Page) => conv(page).locator('[data-slot="franja-asistente"]');
const pedidos = (api: Api, fin: string) => api.conversaciones.enviados.filter((e) => e.ruta.endsWith(fin));

async function abrir(page: Page, preparar: (api: Api) => void, opciones: Opciones = { conversaciones: { asistente: { inicial: null } } }, correo = ADMIN.correo) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, opciones);
    preparar(api);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto("/conversaciones?c=41");
    await expect(conv(page)).toBeVisible();
    return api;
}

/** Pone `atencionAsistente` (y lo que haga falta) en la conversación 41. */
const con = (atencion: Atencion, extra: Record<string, unknown> = {}) => (api: Api) => {
    const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
    if (!c) return;
    Object.assign(c, { atencionAsistente: atencion, ...extra });
    // `asignado: null` en la prueba = sin asignar (el campo no viene).
    if ((c as { asignado?: unknown }).asignado === null) delete c.asignado;
};

test.describe("asistente por conversación › quién atiende, pausar y reanudar", () => {
    test("el asistente atiende: «Pausar» de un toque lo deja en pausa (con quién y cuándo) y «Reanudar» se la devuelve", async ({ page }) => {
        const api = await abrir(page, con({ atiende: true, motivo: null, reanudable: false }, { asignado: null }));
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        await expect(franja(page)).toContainText("El asistente atiende esta conversación");
        await esperarSinDesbordes(page);
        await captura(page, "franja-atiende");

        await franja(page).getByRole("button", { name: /^Pausar el asistente/ }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "pausado");
        await expect(franja(page)).toContainText("Asistente en pausa · Ana");
        await expect(page.getByText("Asistente en pausa en esta conversación")).toBeVisible();
        // Un toque: sin motivo, con la versión y su clave de idempotencia.
        const pausa = pedidos(api, "/41/asistente/pausar")[0];
        expect(pausa?.cuerpo).toEqual({ version: expect.any(Number) });
        expect(pausa?.idempotencia).toBeTruthy();
        await esperarSinDesbordes(page);
        await captura(page, "franja-pausado");

        await franja(page).getByRole("button", { name: /^Reanudar/ }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        await expect(page.getByText("El asistente vuelve a atender")).toBeVisible();
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(1);
    });

    test("pausar diciendo por qué: el motivo viaja y se lee en la franja", async ({ page }) => {
        const api = await abrir(page, con({ atiende: true, motivo: null, reanudable: false }, { asignado: null }));
        await franja(page).getByRole("button", { name: "Pausar diciendo por qué" }).click();
        await page.getByRole("menuitem", { name: "Es un caso delicado" }).click();
        await expect(franja(page)).toContainText("caso delicado");
        expect(pedidos(api, "/41/asistente/pausar")[0]?.cuerpo).toMatchObject({ motivo: "caso_delicado" });
    });

    test("nadie la atiende (el asistente se apartó): se nota, en ámbar, con «Que siga el asistente» y «Tomar»", async ({ page }) => {
        const api = await abrir(page, con({ atiende: false, motivo: "persona_presente", reanudable: true }, { asignado: null }));
        await expect(franja(page)).toHaveAttribute("data-tono", "aviso");
        await expect(franja(page)).toContainText("Nadie la está atendiendo: el asistente se apartó cuando una persona la tomó");
        await expect(franja(page).getByRole("button", { name: /^Tomar/ })).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "franja-nadie-atiende");
        // En la lista, el punto ámbar con su texto.
        if (esTelefono(page)) await page.getByRole("link", { name: "Volver a las conversaciones" }).click();
        await expect(page.locator('[data-conversacion="41"] [data-atencion="persona_presente"]')).toContainText("Nadie la atiende");
        await captura(page, "lista-nadie-atiende");
        if (esTelefono(page)) await page.locator('[data-conversacion="41"]').click();
        await franja(page).getByRole("button", { name: /^Que siga el asistente/ }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(1);
    });

    test("el cliente pidió una persona: antes de devolvérsela al asistente se avisa; «Dejarla como está» no hace nada", async ({ page }) => {
        const api = await abrir(
            page,
            con({ atiende: false, motivo: "pausado", reanudable: true, pausa: { en: hace(5), motivo: "cliente_lo_pidio", por: { id: 2, nombre: "Andrea Castillo" } } }, { asignado: null, asistente: { estado: "detenido", motivoPaso: "cliente_lo_pidio" } as never }),
        );
        await expect(franja(page)).toContainText("Asistente en pausa · Andrea · lo pidió el cliente · hace 5 min");
        // Solo el estado es región viva: el tiempo relativo (que cambia solo) queda fuera y no se re-anuncia.
        await expect(franja(page).getByRole("status")).toHaveText("Asistente en pausa · Andrea · lo pidió el cliente");
        expect(await franja(page).getAttribute("role")).toBeNull();
        await franja(page).getByRole("button", { name: /^Reanudar/ }).click();
        const aviso = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "El cliente pidió hablar con una persona" });
        await expect(aviso).toContainText("¿Devolverle la conversación al asistente de todos modos?");
        // El diálogo entra con animación: se espera a que quede en su sitio.
        await expect.poll(async () => (await buscarDesbordes(page)).length, { timeout: 5_000 }).toBe(0);
        await esperarSinDesbordes(page);
        await captura(page, "reanudar-aviso");
        await aviso.getByRole("button", { name: "Dejarla como está" }).click();
        await expect(aviso).toHaveCount(0);
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(0);
        await franja(page).getByRole("button", { name: /^Reanudar/ }).click();
        await aviso.getByRole("button", { name: "Devolverla al asistente" }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(1);
    });

    test("con una persona asignada no hay franja: «Pausar el asistente aquí» está en el menú «⋯»", async ({ page }) => {
        const api = await abrir(page, con({ atiende: false, motivo: "persona_asignada", reanudable: false }));
        await expect(franja(page)).toHaveCount(0);
        await conv(page).getByRole("button", { name: "Más acciones" }).click();
        await page.getByRole("menuitem", { name: "Pausar el asistente aquí" }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "pausado");
        expect(pedidos(api, "/41/asistente/pausar")).toHaveLength(1);
    });

    test("al quitar la asignación de una conversación que el asistente atendía se pregunta si sigue él: «Sí, que siga» se la devuelve", async ({ page }) => {
        const api = await abrir(page, con({ atiende: false, motivo: "persona_asignada", reanudable: false }));
        await conv(page).getByRole("button", { name: /^Asignada a / }).click();
        await page.getByRole("menuitem", { name: "Quitar asignación" }).click();
        const pregunta = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "¿Que la siga atendiendo el asistente?" });
        await expect(pregunta).toBeVisible();
        await expect.poll(async () => (await buscarDesbordes(page)).length, { timeout: 5_000 }).toBe(0);
        await captura(page, "quitar-asignacion-pregunta");
        // Mientras se decide, se nota que nadie la atiende.
        await expect(franja(page)).toHaveAttribute("data-estado", "persona_presente");
        await pregunta.getByRole("button", { name: "Sí, que siga" }).click();
        await expect(pregunta).toHaveCount(0);
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(1);
    });

    test("«No, queda en cola» no reanuda; y sin el asistente de por medio no se pregunta nada", async ({ page }) => {
        const api = await abrir(page, con({ atiende: false, motivo: "persona_asignada", reanudable: false }));
        await conv(page).getByRole("button", { name: /^Asignada a / }).click();
        await page.getByRole("menuitem", { name: "Quitar asignación" }).click();
        const pregunta = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "¿Que la siga atendiendo el asistente?" });
        await pregunta.getByRole("button", { name: "No, queda en cola" }).click();
        await expect(pregunta).toHaveCount(0);
        await expect(franja(page)).toHaveAttribute("data-estado", "persona_presente");
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(0);

        // Otra conversación, sin `atencionAsistente`: quitar la asignación no pregunta.
        const otra = await page.context().newPage();
        await abrir(otra, () => undefined);
        await otra.getByRole("region", { name: /^Conversación con / }).getByRole("button", { name: /^Asignada a / }).click();
        await otra.getByRole("menuitem", { name: "Quitar asignación" }).click();
        await expect(otra.getByText(/: sin asignar/)).toBeVisible();
        await expect(otra.getByText("¿Que la siga atendiendo el asistente?")).toHaveCount(0);
        await otra.close();
    });

    test("tope, bot y «solo sugiere» informan sin botones; sin el campo no se pinta nada", async ({ page }) => {
        await abrir(page, con({ atiende: false, motivo: "tope", reanudable: false }, { asignado: null }));
        await expect(franja(page)).toContainText("El asistente está en pausa: se llegó al tope del mes");
        await expect(franja(page).getByRole("button")).toHaveCount(0);
        await page.unrouteAll({ behavior: "ignoreErrors" });
        const otra = await page.context().newPage();
        await abrir(otra, () => undefined);
        await expect(otra.locator('[data-slot="franja-asistente"]')).toHaveCount(0);
        await otra.close();
    });

    test("sin el permiso de pausar (solo consulta) se ve quién atiende, sin botones", async ({ page }) => {
        await abrir(page, con({ atiende: true, motivo: null, reanudable: false }, { asignado: null }), { conversaciones: { asistente: { inicial: null } } }, CONSULTA.correo);
        await expect(franja(page)).toContainText("El asistente atiende esta conversación");
        await expect(franja(page).getByRole("button")).toHaveCount(0);
    });

    test("otra persona la cambió mientras tanto (409): se dice, se relee y el segundo intento lleva la versión del servidor", async ({ page }) => {
        const api = await abrir(page, con({ atiende: true, motivo: null, reanudable: false }, { asignado: null }));
        await expect(franja(page)).toBeVisible();
        // Cambia la versión en el servidor sin que el panel se entere.
        const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
        const vista = c?.version ?? 0;
        if (c) c.version = vista + 5;
        const lecturas = () => api.peticiones.filter((p) => p.metodo === "GET" && p.ruta === "/api/v1/mensajeria/conversaciones/41").length;
        const antes = lecturas();
        await franja(page).getByRole("button", { name: /^Pausar el asistente/ }).click();
        await expect(franja(page).getByRole("alert")).toHaveText("Alguien la cambió; inténtalo de nuevo");
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        await expect.poll(lecturas).toBeGreaterThan(antes);
        await franja(page).getByRole("button", { name: /^Pausar el asistente/ }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "pausado");
        expect(pedidos(api, "/41/asistente/pausar").map((e) => (e.cuerpo as { version: number }).version)).toEqual([vista, vista + 5]);
    });

    test("con una pregunta abierta, si la conversación cambia (otra persona la tomó) la pregunta se cierra sola y no se reanuda", async ({ page }) => {
        test.setTimeout(90_000);
        const api = await abrir(page, con({ atiende: false, motivo: "persona_asignada", reanudable: false }));
        await conv(page).getByRole("button", { name: /^Asignada a / }).click();
        await page.getByRole("menuitem", { name: "Quitar asignación" }).click();
        const pregunta = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "¿Que la siga atendiendo el asistente?" });
        await expect(pregunta).toBeVisible();
        // Mientras tanto otra persona la toma: llega por el flujo.
        const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
        if (c) Object.assign(c, { asignado: { id: 3, nombre: "Laura Méndez" }, atencionAsistente: { atiende: false, motivo: "persona_asignada", reanudable: false }, version: c.version + 1 });
        api.conversaciones.mensajeDelCliente(41, "¿Siguen ahí?");
        await expect(pregunta).toHaveCount(0, { timeout: 60_000 });
        await expect(franja(page)).toHaveCount(0);
        expect(pedidos(api, "/41/asistente/reanudar")).toHaveLength(0);
    });

    test("si cambió sin que el panel se enterara, confirmar manda la versión de cuando se preguntó: 409 y no se reanuda", async ({ page }) => {
        const api = await abrir(page, con({ atiende: false, motivo: "persona_asignada", reanudable: false }));
        await conv(page).getByRole("button", { name: /^Asignada a / }).click();
        await page.getByRole("menuitem", { name: "Quitar asignación" }).click();
        const pregunta = page.getByRole("alertdialog").or(page.getByRole("dialog")).filter({ hasText: "¿Que la siga atendiendo el asistente?" });
        await expect(pregunta).toBeVisible();
        const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
        const alPreguntar = c?.version ?? 0;
        // El cliente acaba de pedir una persona; el aviso todavía no llegó a este navegador.
        if (c) Object.assign(c, { version: alPreguntar + 1, asistente: { estado: "detenido", motivoPaso: "cliente_lo_pidio" } });
        await pregunta.getByRole("button", { name: "Sí, que siga" }).click();
        await expect(pregunta).toHaveCount(0);
        await expect.poll(() => pedidos(api, "/41/asistente/reanudar").length).toBe(1);
        expect((pedidos(api, "/41/asistente/reanudar")[0]?.cuerpo as { version: number }).version).toBe(alPreguntar);
        // No se reanudó: sigue sin que el asistente atienda.
        await expect(franja(page)).not.toHaveAttribute("data-estado", "atendiendo");
        expect(c?.atencionAsistente?.atiende).toBe(false);
    });

    test("reanudar con alguien asignado no pone al asistente a atender (se queda apartado)", async ({ page }) => {
        const api = await abrir(page, con({ atiende: false, motivo: "pausado", reanudable: true, pausa: { en: hace(3), motivo: "manual", por: { id: 1, nombre: "Ana López" } } }));
        await franja(page).getByRole("button", { name: /^Reanudar/ }).click();
        await expect(page.getByText("El asistente vuelve a atender")).toBeVisible();
        // Con una persona asignada no hay franja (ya se ve quién la tiene) y el asistente no atiende.
        await expect(franja(page)).toHaveCount(0);
        expect(api.conversaciones.conversaciones.find((x) => x.id === 41)?.atencionAsistente).toMatchObject({ atiende: false, motivo: "persona_asignada" });
    });
});

test.describe("asistente por conversación › resumen guardado en la ficha", () => {
    const ficha = async (page: Page) => {
        if (esTelefono(page)) await conv(page).getByRole("button", { name: "Ver la ficha del cliente" }).click();
        return (esTelefono(page) ? page.getByRole("dialog", { name: "Ficha del cliente" }) : page.getByRole("complementary", { name: "Ficha del cliente" })).getByRole("region", { name: "Resumen del asistente" });
    };

    test("si hay uno guardado se lee al abrir (sin gastar): texto, quién, cuándo y modelo", async ({ page }) => {
        const api = await abrir(page, () => undefined, { conversaciones: { asistente: { inicial: null, resumenGuardado: {} } } });
        const resumen = await ficha(page);
        await expect(resumen).toContainText("Sin internet desde las 7; hay una falla en su zona");
        await expect(resumen.locator('[data-slot="resumen-pie"]')).toContainText(/Generado por Andrea · .+ · qwen3:32b/);
        await expect(resumen.getByRole("button", { name: "Volver a resumir" })).toBeVisible();
        await expect(resumen.locator('[data-slot="resumen-desactualizado"]')).toHaveCount(0);
        // Leerlo no pidió generar nada.
        expect(pedidos(api, "/41/resumen")).toHaveLength(0);
        await captura(page, "resumen-guardado");
    });

    test("con mensajes nuevos lo dice y «Actualizar resumen» lo regenera; si falla, el anterior sigue a la vista", async ({ page }) => {
        const api = await abrir(page, () => undefined, { conversaciones: { asistente: { inicial: null, resumenGuardado: { desactualizado: true, mensajesNuevos: 2 } } } });
        const resumen = await ficha(page);
        await expect(resumen.locator('[data-slot="resumen-desactualizado"]')).toHaveText("Hay 2 mensajes nuevos desde este resumen");
        await captura(page, "resumen-desactualizado");
        await resumen.getByRole("button", { name: "Actualizar resumen" }).click();
        await expect(resumen.getByRole("status")).toContainText("Actualizando el resumen…");
        // Mientras se genera, el anterior no desaparece.
        await expect(resumen).toContainText("Sin internet desde las 7");
        await expect(resumen).toContainText("Cliente sin internet desde la mañana");
        await expect(resumen.locator('[data-slot="resumen-pie"]')).toContainText("Generado por Ana");
        await expect(resumen.locator('[data-slot="resumen-desactualizado"]')).toHaveCount(0);
        expect(pedidos(api, "/41/resumen")).toHaveLength(1);
    });

    test("si actualizar falla, el resumen que había no se borra", async ({ page }) => {
        await abrir(page, () => undefined, { conversaciones: { asistente: { inicial: null, resumen: "error", resumenGuardado: { desactualizado: true, mensajesNuevos: 1 } } } });
        const resumen = await ficha(page);
        await expect(resumen.locator('[data-slot="resumen-desactualizado"]')).toHaveText("Hay 1 mensaje nuevo desde este resumen");
        await resumen.getByRole("button", { name: "Actualizar resumen" }).click();
        await expect(resumen.getByRole("button", { name: "Reintentar" })).toBeVisible();
        await expect(resumen).toContainText("Sin internet desde las 7; hay una falla en su zona");
    });

    test("cuando el resumen deja de venir en el detalle (se reabrió o cambió de cliente) vuelve «Resumir»", async ({ page }) => {
        const api = await abrir(page, () => undefined, { conversaciones: { asistente: { inicial: null, resumenGuardado: {} } } });
        const resumen = await ficha(page);
        await expect(resumen).toContainText("Sin internet desde las 7");
        // El servidor deja de mandarlo y avisa por el flujo.
        const c = api.conversaciones.conversaciones.find((x) => x.id === 41) as { resumen?: unknown } | undefined;
        if (c) delete c.resumen;
        api.conversaciones.mensajeDelCliente(41, "Hola, otra vez yo");
        await expect(resumen.getByRole("button", { name: "Resumir", exact: true })).toBeVisible({ timeout: 20_000 });
        await expect(resumen).not.toContainText("Sin internet desde las 7");
    });

    test("sin resumen guardado: el botón «Resumir» de siempre, y lo generado queda con su pie", async ({ page }) => {
        await abrir(page, () => undefined);
        const resumen = await ficha(page);
        await resumen.getByRole("button", { name: "Resumir", exact: true }).click();
        await expect(resumen).toContainText("Cliente sin internet desde la mañana");
        await expect(resumen.locator('[data-slot="resumen-pie"]')).toContainText(/Generado por Ana · .+ · qwen3:32b/);
    });
});

test.describe("asistente por conversación › relectura de fondo del detalle", () => {
    const conResumen: Opciones = { conversaciones: { asistente: { inicial: null, resumenGuardado: {} } } };
    const RUTA = "**/api/v1/mensajeria/conversaciones/41";
    // El flujo falso se cierra tras cada entrega y el panel espera antes de reabrirlo: los avisos pueden tardar.
    test.setTimeout(120_000);

    test("si la relectura de fondo falla, el panel y el compositor siguen (con lo escrito) y no aparece ningún error", async ({ page }) => {
        const api = await abrir(page, con({ atiende: true, motivo: null, reanudable: false }, { asignado: null }), conResumen);
        const caja = conv(page).locator('[data-slot="compositor"]').getByRole("textbox");
        await caja.fill("Un mensaje a medio escribir");
        // Desde ahora, leer el detalle falla.
        let fallos = 0;
        await page.route(RUTA, (route) => {
            if (route.request().method() !== "GET") return route.fallback();
            fallos++;
            return route.fulfill({ status: 500, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }) });
        });
        // Llega un mensaje habiendo resumen: se relee el detalle de fondo.
        api.conversaciones.mensajeDelCliente(41, "Sigo sin internet");
        await expect(conv(page).getByRole("log", { name: "Mensajes" }).getByText("Sigo sin internet")).toBeVisible({ timeout: 60_000 });
        await expect.poll(() => fallos).toBeGreaterThan(0);
        // Nada se desmontó ni se cambió por un error.
        await expect(caja).toHaveValue("Un mensaje a medio escribir");
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        await expect(page.getByText(/No se pudo leer la conversación|No se pudieron leer/)).toHaveCount(0);
        await expect(conv(page).getByRole("button", { name: "Reintentar" })).toHaveCount(0);
    });

    test("una respuesta vieja de la relectura que llega tarde no pisa lo que ya trajo una acción", async ({ page }) => {
        const api = await abrir(page, con({ atiende: true, motivo: null, reanudable: false }, { asignado: null }), conResumen);
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
        // La relectura de fondo se queda esperando con la foto de ANTES de pausar.
        let soltar: (() => void) | null = null;
        let retenidas = 0;
        await page.route(RUTA, async (route) => {
            if (route.request().method() !== "GET" || retenidas > 0) return route.fallback();
            retenidas++;
            const c = api.conversaciones.conversaciones.find((x) => x.id === 41);
            const vieja = JSON.stringify(detalle(structuredClone(c) as never));
            await new Promise<void>((r) => (soltar = r));
            await route.fulfill({ status: 200, contentType: "application/json", body: vieja });
        });
        api.conversaciones.mensajeDelCliente(41, "Hola");
        await expect(conv(page).getByRole("log", { name: "Mensajes" }).getByText("Hola", { exact: true })).toBeVisible({ timeout: 60_000 });
        await expect.poll(() => retenidas, { timeout: 60_000 }).toBe(1);
        // Mientras tanto se pausa: el panel ya tiene una versión más nueva.
        await franja(page).getByRole("button", { name: /^Pausar el asistente/ }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "pausado");
        // Llega la respuesta vieja («el asistente atiende»): no debe volver atrás.
        (soltar as (() => void) | null)?.();
        await page.waitForTimeout(800);
        await expect(franja(page)).toHaveAttribute("data-estado", "pausado");
        // Y la acción siguiente no da 409 por una versión atrasada.
        await franja(page).getByRole("button", { name: /^Reanudar/ }).click();
        await expect(franja(page)).toHaveAttribute("data-estado", "atendiendo");
    });
});

test.describe("asistente por conversación › eventos en el hilo", () => {
    test("pausado, reanudado y «el bot no pudo contestar» se leen en llano; el último, en ámbar y entero", async ({ page }) => {
        await abrir(page, (api) => {
            const lista = api.conversaciones.mensajes.get(41) ?? [];
            lista.push(
                mensaje(41, { direccion: "sistema", evento: { tipo: "asistente_pausado", actorNombre: "Andrea", detalle: "respuesta_incorrecta" } as never }),
                mensaje(41, { direccion: "sistema", evento: { tipo: "asistente_reanudado", actorNombre: "Andrea", detalle: "pausado" } as never }),
                mensaje(41, { direccion: "sistema", evento: { tipo: "bot_sin_respuesta", minutos: 30 } as never }),
            );
        });
        const hilo = conv(page).getByRole("log", { name: "Mensajes" });
        await expect(hilo.locator('[data-evento="asistente_pausado"]')).toContainText("Andrea pausó el asistente: respondió mal");
        await expect(hilo.locator('[data-evento="asistente_reanudado"]')).toContainText("Andrea le devolvió la conversación al asistente");
        const sinRespuesta = hilo.locator('[data-evento="bot_sin_respuesta"]');
        await expect(sinRespuesta).toContainText("Nadie la tomó en 30 min y el bot no pudo contestar: el cliente no acepta mensajes o pasaron 24 h");
        // No dice que contestó, y se lee entero (sin puntos suspensivos) también en el teléfono.
        await expect(sinRespuesta).not.toContainText("contestó el bot");
        expect(await sinRespuesta.locator("span").nth(0).evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
        await sinRespuesta.scrollIntoViewIfNeeded();
        await esperarSinDesbordes(page);
        await captura(page, "hilo-eventos-asistente");
    });
});
