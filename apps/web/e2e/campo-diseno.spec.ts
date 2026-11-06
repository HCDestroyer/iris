import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { avisoCampo, elegirMaterial, irAPaso, pestanaCampo } from "./ayudas-campo";
import { ordenesDelDia } from "./datos-campo";

/*
 * Diseño «combinada» de la app del técnico (nov 2026), en el teléfono (390 px) claro y oscuro:
 * encabezado que se va al bajar y vuelve compacto al subir, «Hoy» con la siguiente orden en grande,
 * mosaicos y «Después», la isla de menú con la píldora de estado (la hoja del «+» está en
 * campo-mas.spec.ts), «Mañana» y «Semana» con la tira de 7
 * días, y los pasos con chips, − / + en materiales y los avisos que entran desde arriba.
 * Con IRIS_CAPTURAS=<carpeta> deja las capturas para el PM.
 */

type OrdenCampo = Schema<"OrdenCampo">;
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

const isla = (page: Page) => page.getByRole("navigation", { name: "Secciones de la app" });
const notificacion = (page: Page) => avisoCampo(page);

/** Día `n` días después de hoy en Guatemala, a las `hora` (UTC) de ese día. */
function diaMas(n: number, hora: string, ahora = Date.now()): string {
    const hoy = new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala", year: "numeric", month: "2-digit", day: "2-digit" }).format(ahora);
    const d = new Date(Date.parse(`${hoy}T00:00:00Z`) + n * 86_400_000).toISOString().slice(0, 10);
    return `${d}T${hora}Z`;
}

/** Mañana: una instalación con formulario y un cambio de equipo sin formulario; pasado mañana, un retiro. */
function ordenesFuturas(ahora = Date.now()): OrdenCampo[] {
    const [base] = ordenesDelDia();
    if (!base) {
        throw new Error("sin órdenes de prueba");
    }
    // Sin la instantánea de Argos de la base: el nombre y la zona son los del contacto y la orden.
    const sinCliente = structuredClone(base);
    delete sinCliente.cliente;
    const otra = (id: number, parcial: Partial<OrdenCampo>): OrdenCampo => ({ ...sinCliente, id, numero: `OT-000${id}`, estado: "programada", ...parcial });
    return [
        otra(118, {
            tipo: "cambio_equipo",
            contacto: { nombre: "Rosa Lima", telefono: "5555-1118" },
            direccion: "Zona 7",
            formularios: [],
            programadaInicio: diaMas(1, "17:00:00", ahora),
            programadaFin: diaMas(1, "18:00:00", ahora),
        }),
        otra(117, {
            tipo: "instalacion",
            contacto: { nombre: "Luis Ortiz", telefono: "5555-1117" },
            direccion: "Mixco",
            cita: { tecnicoId: base.tecnicoId ?? 51, inicio: diaMas(1, "14:30:00", ahora), fin: diaMas(1, "16:30:00", ahora), ventanaInicio: diaMas(1, "14:00:00", ahora), ventanaFin: diaMas(1, "16:00:00", ahora) },
        }),
        otra(119, { tipo: "retiro", contacto: { nombre: "Ana Pérez", telefono: "5555-1119" }, programadaInicio: diaMas(2, "15:00:00", ahora), programadaFin: diaMas(2, "16:00:00", ahora) }),
    ];
}

const PRUEBA_VELOCIDAD: Schema<"PlantillaFormulario"> = {
    id: 9,
    codigo: "velocidad",
    nombre: "Prueba de velocidad",
    descripcion: "",
    empresaId: null,
    activa: true,
    porDefecto: false,
    requiereFirma: false,
    tipo: null,
    version: 1,
    versionActual: 1,
    versionId: 91,
    numeroVersion: 1,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-20T00:00:00Z",
    items: [{ id: 901, codigo: "bajada", etiqueta: "Bajada (Mbps)", tipo: "numero", obligatorio: false, unidad: "Mbps", posicion: 1 }],
};

/** OT-000102 en sitio (la siguiente orden: va en grande). */
function conUnaEnSitio(): OrdenCampo[] {
    return ordenesDelDia().map((o) => (o.id === 102 ? { ...o, estado: "en_sitio", enCaminoEn: diaMas(0, "14:00:00"), enSitioEn: diaMas(0, "14:20:00") } : o));
}

async function captura(page: Page, que: string, proyecto: string) {
    await page.screenshot({ path: path.join(carpeta, `campo-diseno-${que}-${proyecto}.png`) });
}

test("Hoy: la siguiente orden en grande, mosaicos con datos reales y «Después»", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: conUnaEnSitio(), plantillas: [PRUEBA_VELOCIDAD] });
    await page.goto("/campo");
    await expect(page.getByRole("heading", { level: 1, name: "Hola, Tomás" })).toBeVisible();
    const siguiente = page.getByRole("region", { name: "Siguiente orden" });
    // La en curso va primero aunque su cita sea después.
    await expect(siguiente.getByRole("link", { name: /^OT-000102, / })).toBeVisible();
    await expect(siguiente).toContainText("En sitio");
    await expect(siguiente.getByRole("img", { name: /de 4 pasos listos$/ })).toBeVisible();
    await expect(siguiente.getByRole("button", { name: "Continuar trabajo" })).toBeVisible();
    await expect(siguiente.getByRole("link", { name: "Cómo llegar" })).toHaveAttribute("href", /google\.com\/maps/);
    await expect(siguiente.getByRole("link", { name: "Llamar al cliente" })).toHaveAttribute("href", /^tel:/);
    // Mosaicos: con el paquete y los traslados leídos (2 enviados).
    await expect(page.locator('[data-slot="mosaico-ruta"]')).toContainText("3 órdenes");
    await expect(page.locator('[data-slot="mosaico-recibir"]')).toContainText("2 traslados");
    // «Por recibir» ya no está en la isla: el mosaico lleva su contador.
    await expect(isla(page).getByRole("link", { name: /Por recibir/ })).toHaveCount(0);
    const despues = page.getByRole("list", { name: "Después" }).getByRole("link");
    await expect(despues).toHaveCount(2);
    await expect(despues.nth(0)).toContainText("OT-000101");
    // Barra segmentada: «Hoy · 3» marcado.
    await expect(page.getByRole("radiogroup", { name: "Día" }).getByRole("radio", { name: "Hoy · 3" })).toBeChecked();
    // Sin desbordar a lo ancho.
    expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0);
    await captura(page, "01-hoy", info.project.name);

    await siguiente.getByRole("button", { name: "Continuar trabajo" }).click();
    await expect(page).toHaveURL(/orden=102&paso=checklist$/);
});

test("el encabezado se va al bajar y vuelve compacto al subir; la isla se encoge", async ({ page }) => {
    await simularApiCampo(page, { ordenes: conUnaEnSitio() });
    await page.goto("/campo");
    await expect(page.getByRole("region", { name: "Siguiente orden" })).toBeVisible();
    const compacta = page.locator('[data-slot="barra-compacta"]');
    await expect(compacta).toHaveAttribute("aria-hidden", "true");
    // Contenido suficiente para desplazar.
    await page.evaluate(() => {
        document.querySelector("main")?.insertAdjacentHTML("beforeend", '<div style="height:2000px"></div>');
    });
    await page.mouse.wheel(0, 900);
    await expect(isla(page)).toHaveAttribute("data-encogida", "true");
    await expect(compacta).toHaveAttribute("aria-hidden", "true");
    // El encabezado no queda fijo: se fue con el contenido.
    await expect.poll(async () => (await page.locator('[data-slot="encabezado-campo"]').boundingBox())?.y ?? 0).toBeLessThan(-100);
    await page.mouse.wheel(0, -300);
    await expect(compacta).not.toHaveAttribute("aria-hidden", "true");
    // La compacta lleva los indicadores y buscar (sin avatar).
    await expect(compacta.getByRole("button", { name: "Todo enviado, toca para ver el estado" })).toBeVisible();
    await expect(compacta.getByRole("button", { name: "Buscar orden" })).toBeVisible();
    await expect(isla(page)).not.toHaveAttribute("data-encogida", "true");
    // Encogida o no, las zonas táctiles siguen siendo de 48 px o más.
    for (const enlace of await isla(page).getByRole("link").all()) {
        expect((await enlace.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(48);
    }
});

test("la nube del encabezado: sin señal con algo por enviar crece; su hoja lleva a «Por enviar»; nada sobre la isla", async ({ page, context }, info) => {
    const api = await simularApiCampo(page, { ordenes: conUnaEnSitio() });
    await page.goto("/campo?orden=101");
    await page.locator('[data-slot="notas-orden"]').getByRole("button", { name: "Agregar una nota" }).click();
    await page.getByLabel("Nueva nota").fill("Perro bravo en el portón");
    api.sinRed = true;
    await context.setOffline(true);
    await page.getByRole("button", { name: "Agregar nota" }).click();
    await page.getByRole("button", { name: "Mis órdenes" }).click();
    const nube = page.locator('[data-slot="barra-conexion"] [data-indicador="sincronizacion"]');
    await expect(nube).toHaveAccessibleName("Sin señal, 1 por enviar, toca para ver el estado");
    await expect(page.getByTestId("estado-conexion")).toHaveText("Sin conexión");
    await expect(page.getByTestId("pendientes-por-enviar")).toHaveText("1 pendiente por enviar");
    // Va arriba, junto a buscar: sobre la isla no flota nada.
    const cajaNube = await nube.boundingBox();
    const cajaIsla = await isla(page).boundingBox();
    expect((cajaNube?.y ?? 0) + (cajaNube?.height ?? 0)).toBeLessThan((cajaIsla?.y ?? 0) - 200);
    await captura(page, "02-isla-aviso", info.project.name);
    await nube.click();
    await page.getByRole("dialog", { name: "Conexión y envíos" }).getByRole("button", { name: "Ver «Por enviar»" }).click();
    await expect(page.getByRole("heading", { name: "Por enviar", exact: true })).toBeVisible();
    api.sinRed = false;
    await context.setOffline(false);
    await expect(nube).toHaveAttribute("data-forma", "al_dia", { timeout: 15_000 });
});

test("Mañana y Semana: tira de 7 días y citas en bloques, leídas con señal", async ({ page }, info) => {
    // Sin las órdenes de hoy de los datos de prueba (van por día UTC: cerca de la medianoche de
    // Guatemala caerían en «mañana»); solo las de la semana, por día de Guatemala.
    const api = await simularApiCampo(page, { ordenes: [], ordenesFuturas: ordenesFuturas() });
    await page.goto("/campo");
    await page.getByRole("radiogroup", { name: "Día" }).getByRole("radio", { name: /^Mañana/ }).click();
    await expect(page).toHaveURL(/vista=agenda$/);
    await expect(page.getByRole("heading", { name: /^Mañana, / })).toBeVisible();
    const semana = page.getByRole("navigation", { name: "Semana" }).getByRole("link");
    await expect(semana).toHaveCount(7);
    await expect(semana.nth(1)).toHaveAttribute("aria-current", "date");
    await expect(semana.nth(1)).toHaveAccessibleName(/: 2 citas$/);
    await expect(page.getByTestId("resumen-dia")).toHaveText("2 citas · 3 h");
    const citas = page.locator('[data-slot="agenda-dia"] ol > li');
    await expect(citas).toHaveCount(2);
    await expect(citas.nth(0)).toContainText("Instalación · Luis Ortiz");
    await expect(citas.nth(1)).toContainText("Cambio de equipo · Rosa Lima");
    await expect(citas.nth(1)).toContainText("Sin formulario");
    // «Mañana» ya tiene contador (dato leído) y la isla marca «Agenda».
    await expect(page.getByRole("radiogroup", { name: "Día" }).getByRole("radio", { name: "Mañana · 2" })).toBeChecked();
    await expect(isla(page).getByRole("link", { name: "Agenda" })).toHaveAttribute("aria-current", "page");
    // La agenda del técnico es GET /campo/paquete del último día de la semana (no GET /agenda).
    expect(api.peticiones.some((p) => p.ruta === "/api/v1/agenda")).toBe(false);
    await captura(page, "04-manana", info.project.name);

    await semana.nth(2).click();
    await expect(page).toHaveURL(/vista=agenda&dia=\d{4}-\d{2}-\d{2}$/);
    await expect(citas).toHaveCount(1);
    await expect(citas.nth(0)).toContainText("Retiro · Ana Pérez");
    await page.getByRole("radiogroup", { name: "Día" }).getByRole("radio", { name: "Semana" }).click();
    await expect(page.getByRole("heading", { name: "Esta semana" })).toBeVisible();
    await expect(page.locator('[data-slot="agenda-semana"]')).toContainText("Luis Ortiz");
});

test("Mañana sin señal: dice que no se pudo leer, con «Reintentar» y sin contadores", async ({ page, context }) => {
    const api = await simularApiCampo(page, { ordenes: [], ordenesFuturas: ordenesFuturas() });
    await page.goto("/campo");
    await expect(page.getByText("No tienes órdenes para hoy")).toBeVisible();
    api.sinRed = true;
    await context.setOffline(true);
    await pestanaCampo(page, "Agenda");
    await expect(page.getByRole("button", { name: "Reintentar" })).toBeVisible();
    await expect(page.getByTestId("resumen-dia")).toHaveCount(0);
    await expect(page.getByRole("radiogroup", { name: "Día" }).getByRole("radio", { name: "Mañana" })).toBeVisible();
    // Al volver la señal se lee sola.
    api.sinRed = false;
    await context.setOffline(false);
    await expect(page.getByTestId("resumen-dia")).toHaveText("2 citas · 3 h");
});

// Con el reloj fijo en las dos franjas: 01:30 UTC (19:30 del jueves en Guatemala, en UTC ya es
// viernes) y 23:30 UTC (17:30 en Guatemala, mismo día). «Mañana» es el viernes en las dos.
for (const [franja, instante] of [
    ["01:30 UTC (19:30 GT)", "2026-11-06T01:30:00Z"],
    ["23:30 UTC (17:30 GT)", "2026-11-05T23:30:00Z"],
] as const) {
    test(`Mañana a las ${franja}: el viernes 6 con sus citas y el sábado 7 con el retiro`, async ({ page }) => {
        const ahora = Date.parse(instante);
        await page.clock.setFixedTime(ahora);
        const api = await simularApiCampo(page, { ordenes: [], ordenesFuturas: ordenesFuturas(ahora) });
        await page.goto("/campo");
        await expect(page.getByText("Jueves 5 de noviembre")).toBeVisible();
        await page.getByRole("radiogroup", { name: "Día" }).getByRole("radio", { name: /^Mañana/ }).click();
        await expect(page.getByRole("heading", { name: "Mañana, vie 6" })).toBeVisible();
        await expect(page.getByTestId("resumen-dia")).toHaveText("2 citas · 3 h");
        // El sábado 7 (pasado mañana) tiene el retiro: la semana se pidió por día de Guatemala.
        await page.getByRole("navigation", { name: "Semana" }).getByRole("link", { name: "sábado, 7 de noviembre" }).click();
        await expect(page.locator('[data-slot="agenda-dia"] ol > li')).toHaveCount(1);
        await expect(page.locator('[data-slot="agenda-dia"] ol > li')).toContainText("Retiro · Ana Pérez");
        expect(api.peticiones.filter((p) => p.ruta === "/api/v1/agenda")).toHaveLength(0);
    });
}

test("pasos: chips, tarjetas de 28 px, − / + en materiales y el aviso desde arriba", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: conUnaEnSitio() });
    await page.goto("/campo?orden=102&paso=materiales");
    await expect(page.getByRole("heading", { level: 1, name: "Materiales y equipos" })).toBeVisible();
    const pasos = page.getByRole("navigation", { name: "Pasos de la orden" });
    await expect(pasos.getByRole("link", { name: /^3\. Materiales y equipos:/ })).toHaveAttribute("aria-current", "step");
    await expect(pasos.getByRole("link", { name: /^1\. Llegada: listo$/ })).toBeVisible();
    // Sin isla en los pasos: la barra «Anterior» / «Siguiente» va abajo.
    await expect(isla(page)).toHaveCount(0);
    await expect(page.getByRole("navigation", { name: "Pasos", exact: true }).getByRole("button", { name: "Siguiente: fotos" })).toBeVisible();
    const radio = await page.locator('[data-slot="tarjeta"]').first().evaluate((el) => getComputedStyle(el).borderTopLeftRadius);
    expect(radio).toBe("28px");

    await elegirMaterial(page, "Cable drop 1 hilo");
    // Al elegir queda en 1; − / + suman o restan desde ahí.
    await expect(page.getByLabel("Cantidad")).toHaveValue("1");
    await page.getByRole("button", { name: "Agregar uno" }).click();
    await page.getByRole("button", { name: "Agregar uno" }).click();
    await page.getByRole("button", { name: "Agregar uno" }).click();
    await page.getByRole("button", { name: "Quitar uno" }).click();
    await expect(page.getByLabel("Cantidad")).toHaveValue("3");
    await page.getByRole("button", { name: "Registrar material" }).click();
    const aviso = notificacion(page);
    await expect(aviso).toContainText("Material registrado");
    // Entra desde arriba: pegado al borde superior de la pantalla.
    expect((await aviso.boundingBox())?.y ?? 999).toBeLessThan(40);
    await page.evaluate(() => window.scrollTo({ top: 0 }));
    await captura(page, "05-paso-materiales", info.project.name);
    await irAPaso(page, "Fotos");
    await expect(pasos.getByRole("link", { name: /^4\. Fotos:/ })).toHaveAttribute("aria-current", "step");
});
