import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { OrdenCampo } from "@iris/sync";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { avisoCampo } from "./ayudas-campo";
import { ordenesDelDia, TECNICO } from "./datos-campo";

/*
 * Acciones de la orden y notas editables (decisión del PM, nov 2026): encabezado ← | OT con
 * «● Estado · Tipo · ventana» | «⋯»; la acción principal en «tinta» con [Pausar | No se pudo] (o
 * [En camino | No se pudo] si está programada); en pausa, el aviso naranja y solo «Reanudar»; el
 * cliente con «Cómo llegar» y «Llamar»; las notas con «Agregar», ver, editar (contador /4000) y
 * eliminar (pregunta), y «Atrás» del navegador cierra cada hoja. A 390 px y 1280 px, claro y oscuro.
 * Con IRIS_CAPTURAS_ACCIONES=<carpeta> deja las capturas para el PM.
 */

test.beforeEach(async ({ context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "");
});

const hoy = () => new Date().toISOString().slice(0, 10);
/** Hace `horas` horas (las notas de prueba, para que digan «hoy» o «ayer» según la hora en Guatemala). */
const hace = (horas: number) => new Date(Date.now() - horas * 3_600_000).toISOString();
const CUANDO = String.raw`(hoy|ayer) \d{2}:\d{2}`;

const DE_DESPACHO = { id: "0192f0a0-0000-7000-8000-0000000000d1", texto: "Portón negro, tocar el timbre de la izquierda. Perro en el patio.", autorId: 9, autorNombre: "Despacho", canal: "panel", ocurridoEn: hace(3), version: 1, editadaEn: null };
const MIA = { id: "0192f0a0-0000-7000-8000-0000000000d2", texto: "Cliente pide dejar el router en la sala, junto a la TV. Cable de 15 m.", autorId: TECNICO.id, autorNombre: TECNICO.nombre, canal: "campo", ocurridoEn: hace(1), version: 1, editadaEn: null };

function conEstado(estado: OrdenCampo["estado"], extra: Partial<OrdenCampo> = {}): OrdenCampo[] {
    return ordenesDelDia().map((o) =>
        o.id === 101
            ? {
                  ...o,
                  estado,
                  cita: { tecnicoId: TECNICO.id, inicio: `${hoy()}T14:00:00Z`, fin: `${hoy()}T16:30:00Z`, ventanaInicio: `${hoy()}T14:30:00Z`, ventanaFin: `${hoy()}T16:00:00Z` },
                  notas: [DE_DESPACHO, MIA],
                  ...(estado === "en_sitio" ? { enCaminoEn: `${hoy()}T14:10:00Z`, enSitioEn: `${hoy()}T14:35:00Z` } : {}),
                  ...extra,
              }
            : o,
    );
}

const PAUSADA: Partial<OrdenCampo> = {
    enCaminoEn: `${hoy()}T14:10:00Z`,
    enSitioEn: `${hoy()}T14:35:00Z`,
    pausadaDesde: "en_sitio",
    pausadaEn: `${hoy()}T18:10:00Z`,
    pausas: [{ id: 1, motivo: "otro", nota: "Almuerzo", desdeEstado: "en_sitio", inicio: `${hoy()}T18:10:00Z`, fin: null, duracionSeg: null, cierre: null, pausadaPor: TECNICO.id, cerradaPor: null }],
};

const encabezado = (page: Page) => page.locator('[data-slot="encabezado-orden"]');
const notas = (page: Page) => page.locator('[data-slot="notas-orden"]');
const hoja = (page: Page, nombre: string | RegExp) => page.getByRole("dialog", { name: nombre });

async function abrir(page: Page, ordenes: OrdenCampo[]) {
    const api = await simularApiCampo(page, { ordenes });
    await page.goto("/campo?orden=101");
    await expect(encabezado(page).getByRole("heading", { level: 1, name: "OT-000101" })).toBeVisible();
    return api;
}

test("encabezado: ←, número con «● Estado · Tipo · ventana» y «⋯» con sus acciones", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await abrir(page, conEstado("en_sitio"));
    await expect(encabezado(page).locator('[data-slot="linea-estado"]')).toHaveText("En sitio · Instalación · 08:30–10:00");
    await expect(encabezado(page).getByRole("button", { name: "Mis órdenes" })).toBeVisible();
    await encabezado(page).getByRole("button", { name: "Más acciones de la orden" }).click();
    const menu = page.getByRole("menu");
    await expect(menu.getByRole("menuitem")).toHaveText(["Agregar nota", "Historial del cliente", "Copiar dirección"]);
    await menu.getByRole("menuitem", { name: "Copiar dirección" }).click();
    await expect(avisoCampo(page)).toContainText("Dirección copiada");
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("6a avenida 12-34, zona 1, Guatemala");

    // «Agregar nota» desde el menú abre la misma hoja que la tarjeta.
    await encabezado(page).getByRole("button", { name: "Más acciones de la orden" }).click();
    await page.getByRole("menuitem", { name: "Agregar nota" }).click();
    await expect(hoja(page, "Nueva nota")).toBeVisible();
    await page.goBack();
    await expect(hoja(page, "Nueva nota")).toHaveCount(0);
    await expect(page).toHaveURL(/orden=101/);

    // Historial del cliente (con señal): sus órdenes que fueron del técnico.
    await encabezado(page).getByRole("button", { name: "Más acciones de la orden" }).click();
    await page.getByRole("menuitem", { name: "Historial del cliente" }).click();
    await expect(hoja(page, "Historial del cliente")).toBeVisible();
    await expect(hoja(page, "Historial del cliente")).toContainText("No hay otras órdenes tuyas con este cliente.");
    await page.goBack();
    await expect(hoja(page, "Historial del cliente")).toHaveCount(0);
});

test("en sitio: «Continuar trabajo» en tinta y [Pausar | No se pudo]; el cliente con «Cómo llegar» y «Llamar»", async ({ page }) => {
    await abrir(page, conEstado("en_sitio"));
    const principal = page.locator('[data-slot="accion-principal"]');
    await expect(principal).toHaveText("Continuar trabajo");
    const box = await principal.boundingBox();
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(48);
    // Tinta: el fondo es el color del texto de la página (negro en claro, casi blanco en oscuro), no el teal.
    const colores = await principal.evaluate((b) => ({ fondo: getComputedStyle(b).backgroundColor, texto: getComputedStyle(document.body).color }));
    const oscuro = (rgb: string) => (rgb.match(/\d+/g) ?? []).slice(0, 3).reduce((s, v) => s + Number(v), 0) < 3 * 128;
    expect(oscuro(colores.fondo)).toBe(oscuro(colores.texto));
    const grupo = page.getByRole("group", { name: "Más acciones de la orden" });
    await expect(grupo.getByRole("button")).toHaveText(["Pausar", "No se pudo"]);
    for (const b of await grupo.getByRole("button").all()) {
        expect((await b.boundingBox())?.height ?? 0).toBeGreaterThanOrEqual(48);
    }
    const cliente = page.locator('[data-slot="cliente-orden"]');
    await expect(cliente.getByRole("link", { name: "Cómo llegar" })).toHaveAttribute("href", /14\.6407/);
    await expect(cliente.getByRole("link", { name: "Llamar" })).toHaveAttribute("href", /^tel:/);

    // «Pausar» abre su hoja; «Atrás» la cierra sin salir de la orden.
    await grupo.getByRole("button", { name: "Pausar" }).click();
    await expect(hoja(page, "Pausar OT-000101")).toBeVisible();
    await page.goBack();
    await expect(hoja(page, "Pausar OT-000101")).toHaveCount(0);
    await grupo.getByRole("button", { name: "No se pudo" }).click();
    await expect(hoja(page, "No se pudo hacer OT-000101")).toBeVisible();
    await page.getByRole("dialog").getByRole("button", { name: "Cerrar" }).click();
    await expect(page).toHaveURL(/orden=101/);
});

test("programada: «Empezar trabajo» y [En camino | No se pudo]; «En camino» se registra", async ({ page }) => {
    const api = await abrir(page, conEstado("programada"));
    await expect(page.locator('[data-slot="accion-principal"]')).toHaveText("Empezar trabajo");
    const grupo = page.getByRole("group", { name: "Más acciones de la orden" });
    await expect(grupo.getByRole("button")).toHaveText(["En camino", "No se pudo"]);
    await grupo.getByRole("button", { name: "En camino" }).click();
    await expect(encabezado(page).locator('[data-slot="linea-estado"]')).toHaveText(/^En camino · /);
    await expect.poll(() => api.lotes.flatMap((l) => l.operaciones ?? []).map((o) => o.tipo)).toContain("orden.iniciar_camino");
    await expect(grupo.getByRole("button")).toHaveText(["Pausar", "No se pudo"]);
});

test("pausada: solo el aviso naranja «En pausa desde HH:MM · motivo» y «Reanudar»", async ({ page }) => {
    await abrir(page, conEstado("pausada", PAUSADA));
    await expect(page.getByTestId("pausa-orden")).toHaveText("En pausa desde 12:10 · Almuerzo");
    await expect(page.locator('[data-slot="accion-principal"]')).toHaveCount(0);
    await expect(page.getByRole("group", { name: "Más acciones de la orden" })).toHaveCount(0);
    await page.getByRole("button", { name: "Reanudar" }).click();
    await expect(page.getByTestId("pausa-orden")).toHaveCount(0);
    await expect(page.locator('[data-slot="accion-principal"]')).toHaveText("Continuar trabajo");
});

test("notas: agregar, ver, editar con contador, eliminar con pregunta; «Atrás» cierra cada hoja", async ({ page }) => {
    const api = await abrir(page, conEstado("en_sitio"));
    const lista = notas(page).getByRole("list", { name: "Notas de la orden" });
    await expect(lista.locator('[data-slot="nota"]')).toHaveCount(2);
    await expect(lista.locator('[data-slot="nota"]').first()).toContainText(new RegExp(`Despacho · ${CUANDO}`));
    await expect(lista.locator('[data-slot="nota"]').last()).toContainText(new RegExp(`Tú · ${CUANDO}`));

    // La de despacho: se ve, sin «Editar» ni «Eliminar».
    await lista.locator('[data-slot="nota"]').first().click();
    const ver = hoja(page, "Nota");
    await expect(ver).toContainText(new RegExp(`Despacho · ${CUANDO} · desde el panel`));
    await expect(ver.getByRole("button", { name: "Editar" })).toHaveCount(0);
    await page.goBack();
    await expect(ver).toHaveCount(0);

    // La propia: Editar con contador; «Guardar» solo con cambios.
    await lista.locator('[data-slot="nota"]').last().click();
    await expect(hoja(page, "Nota")).toContainText(new RegExp(`Tú · ${CUANDO} · desde el teléfono`));
    await hoja(page, "Nota").getByRole("button", { name: "Editar" }).click();
    const editar = hoja(page, "Editar nota");
    await expect(editar.locator('[data-slot="contador-nota"]')).toHaveText("70 / 4000");
    await expect(editar.getByRole("button", { name: "Guardar" })).toBeDisabled();
    await editar.getByRole("textbox", { name: "Texto de la nota" }).fill("Router en la sala, junto a la TV.");
    await expect(editar.locator('[data-slot="contador-nota"]')).toHaveText("33 / 4000");
    // «Atrás» con cambios pregunta antes de cerrar.
    await page.goBack();
    await expect(page.getByRole("alertdialog", { name: "¿Descartar los cambios?" })).toBeVisible();
    await page.getByRole("button", { name: "Seguir editando" }).click();
    await editar.getByRole("button", { name: "Guardar" }).click();
    await expect(avisoCampo(page)).toContainText("Nota editada");
    await expect(lista.locator('[data-slot="nota"]').last()).toContainText("Router en la sala, junto a la TV.");
    await expect(lista.locator('[data-slot="nota"]').last()).toContainText("editada");
    await expect.poll(() => api.lotes.flatMap((l) => l.operaciones ?? []).filter((o) => o.tipo === "nota.editar").map((o) => o.datos)).toEqual([
        { notaId: MIA.id, versionNota: 1, texto: "Router en la sala, junto a la TV." },
    ]);

    // Eliminar pregunta; «Atrás» cierra la pregunta y la nota sigue.
    await lista.locator('[data-slot="nota"]').last().click();
    await hoja(page, "Nota").getByRole("button", { name: "Eliminar" }).click();
    const pregunta = page.getByRole("alertdialog", { name: "¿Eliminar la nota?" });
    await expect(pregunta).toContainText("Queda registrada en el historial de la orden.");
    await page.goBack();
    await expect(pregunta).toHaveCount(0);
    await expect(lista.locator('[data-slot="nota"]')).toHaveCount(2);
    await lista.locator('[data-slot="nota"]').last().click();
    await hoja(page, "Nota").getByRole("button", { name: "Eliminar" }).click();
    await pregunta.getByRole("button", { name: "Eliminar" }).click();
    await expect(lista.locator('[data-slot="nota"]')).toHaveCount(1);
    await expect.poll(() => api.lotes.flatMap((l) => l.operaciones ?? []).filter((o) => o.tipo === "nota.eliminar").map((o) => o.datos)).toEqual([{ notaId: MIA.id, versionNota: 2 }]);

    // Agregar desde la tarjeta.
    await notas(page).getByRole("button", { name: "Agregar nota" }).click();
    await hoja(page, "Nueva nota").getByLabel("Nueva nota").fill("Medí -18,5 dBm en la roseta.");
    await hoja(page, "Nueva nota").getByRole("button", { name: "Agregar nota" }).click();
    await expect(lista.locator('[data-slot="nota"]')).toHaveCount(2);
});

test("capturas de la orden (390 px y 1280 px)", async ({ page }, info) => {
    const carpeta = process.env.IRIS_CAPTURAS_ACCIONES ?? path.join("test-results", "capturas-acciones");
    const tema = info.project.name.endsWith("oscuro") ? "oscuro" : "claro";
    const ancho = info.project.name.startsWith("telefono") ? "390" : "1280";
    const nombre = (n: string) => path.join(carpeta, `${n}-${ancho}-${tema}.png`);
    await abrir(page, conEstado("en_sitio"));
    await page.screenshot({ path: nombre("01-orden"), fullPage: true });
    await encabezado(page).getByRole("button", { name: "Más acciones de la orden" }).click();
    await expect(page.getByRole("menu")).toBeVisible();
    await page.screenshot({ path: nombre("02-menu") });
    await page.keyboard.press("Escape");
    await notas(page).locator('[data-slot="nota"]').last().click();
    await expect(hoja(page, "Nota")).toBeVisible();
    await page.screenshot({ path: nombre("03-nota") });
    await hoja(page, "Nota").getByRole("button", { name: "Editar" }).click();
    await expect(hoja(page, "Editar nota")).toBeVisible();
    await page.screenshot({ path: nombre("04-editar") });
    await hoja(page, "Editar nota").getByRole("button", { name: "Cancelar" }).click();
    await notas(page).locator('[data-slot="nota"]').last().click();
    await hoja(page, "Nota").getByRole("button", { name: "Eliminar" }).click();
    await expect(page.getByRole("alertdialog", { name: "¿Eliminar la nota?" })).toBeVisible();
    await page.screenshot({ path: nombre("05-eliminar") });
    await page.getByRole("alertdialog").getByRole("button", { name: "Cancelar" }).click();
    await page.goto("/campo?orden=101");
    await page.unrouteAll({ behavior: "ignoreErrors" });
    await abrir(page, conEstado("pausada", PAUSADA));
    await expect(page.getByTestId("pausa-orden")).toBeVisible();
    await page.screenshot({ path: nombre("06-pausada"), fullPage: true });
});
