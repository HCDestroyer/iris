import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { avisoCampo, guardarYSeguir } from "./ayudas-campo";
import { ordenesDelDia, PERMISOS_TECNICO, TECNICO } from "./datos-campo";

/*
 * Reporte del PM sobre la app «combinada» (0.12.0), en la PWA, en teléfono y escritorio, claro y
 * oscuro: el encabezado sólido (franja bajo la barra de estado; la barra compacta solo se desliza,
 * nunca se transparenta), la orden en pausa de solo lectura con «Reanudar», la isla de los pasos (sin
 * «Guardar» si no hay cambios; «Guardar y seguir» con el contador si los hay; «Guardado» al
 * guardar), los avisos con el `Toast` de HeroUI bajo el margen seguro, las hojas con «X», Escape y «Atrás» del
 * navegador (y la pregunta si hay cambios) y la búsqueda en vivo por encima del teclado (simulado
 * con `visualViewport`). Con IRIS_CAPTURAS=<carpeta> deja las capturas a 390 px.
 */

type OrdenCampo = Schema<"OrdenCampo">;
const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

test.beforeEach(async ({ context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "");
});

const hace = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

/** OT-000101 (instalación con sí/no, medición, foto y texto) en sitio. */
function enSitio(): OrdenCampo[] {
    return ordenesDelDia().map((o) => (o.id === 101 ? { ...o, estado: "en_sitio", enCaminoEn: hace(40), enSitioEn: hace(30) } : o));
}

/** OT-000101 pausada en sitio por mal clima hace 10 min. */
function pausada(): OrdenCampo[] {
    return ordenesDelDia().map((o) =>
        o.id === 101
            ? {
                  ...o,
                  estado: "pausada",
                  enCaminoEn: hace(40),
                  enSitioEn: hace(30),
                  pausadaDesde: "en_sitio",
                  pausadaEn: hace(10),
                  tiempoPausadoSeg: 0,
                  pausas: [{ id: 1, motivo: "clima", nota: null, desdeEstado: "en_sitio", inicio: hace(10), fin: null, duracionSeg: null, cierre: null, pausadaPor: TECNICO.id, cerradaPor: null }],
              }
            : o,
    );
}

async function captura(page: Page, que: string, proyecto: string) {
    if (proyecto.startsWith("telefono")) {
        await page.screenshot({ path: path.join(carpeta, `campo-pm-${que}-${proyecto}.png`) });
    }
}

const islaPasos = (page: Page) => page.getByRole("navigation", { name: "Pasos", exact: true });

test("encabezado: franja sólida bajo la barra de estado; la barra compacta es sólida y solo se desliza", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: enSitio() });
    await page.goto("/campo");
    await expect(page.getByRole("region", { name: "Siguiente orden" })).toBeVisible();
    const franja = page.locator('[data-slot="franja-superior"]');
    await expect(franja).toHaveCSS("position", "fixed");
    const compacta = page.locator('[data-slot="barra-compacta"]');
    // Nunca anima ni cambia la opacidad: solo se mueve (sin un estado a medio transparentar).
    const estilo = () =>
        compacta.evaluate((el) => {
            const s = getComputedStyle(el);
            const caja = el.getBoundingClientRect();
            return { opacidad: s.opacity, transicion: s.transitionProperty, fondo: s.backgroundColor, abajo: caja.bottom };
        });
    let s = await estilo();
    expect(s.opacidad).toBe("1");
    expect(s.transicion).not.toContain("opacity");
    expect(s.abajo).toBeLessThanOrEqual(0);
    await page.evaluate(() => document.querySelector("main")?.insertAdjacentHTML("beforeend", '<div style="height:2400px"></div>'));
    await page.mouse.wheel(0, 900);
    await page.waitForTimeout(300);
    await captura(page, "01-encabezado-oculto", info.project.name);
    await page.mouse.wheel(0, -300);
    await expect(compacta).toHaveAttribute("data-visible", "true");
    await expect.poll(async () => (await estilo()).abajo).toBeGreaterThan(40);
    s = await estilo();
    expect(s.opacidad).toBe("1");
    // Fondo sólido del tema (sin alfa): el contenido no se ve a través.
    expect(s.fondo).toMatch(/^rgb\(\d+, \d+, \d+\)$/);
    await expect(compacta.getByRole("button", { name: "Tu cuenta" })).toBeVisible();
    await captura(page, "02-encabezado-compacto", info.project.name);
    // Al bajar otra vez se va entera, sin quedar encima del contenido.
    await page.mouse.wheel(0, 400);
    await expect(compacta).toHaveAttribute("data-visible", "false");
    await expect.poll(async () => (await estilo()).abajo).toBeLessThanOrEqual(0);
});

test("orden en pausa: pasos de solo lectura con «Orden en pausa» y «Reanudar»; reanudada se trabaja", async ({ page }, info) => {
    const api = await simularApiCampo(page, { ordenes: pausada(), permisos: PERMISOS_TECNICO });
    await page.goto("/campo?orden=101&paso=checklist");
    const aviso = page.getByTestId("orden-pausada");
    await expect(aviso).toContainText("Orden en pausa: Mal clima");
    await expect(aviso).toContainText("no se registra trabajo");
    await expect(page.getByRole("radiogroup", { name: "La ONU enciende" }).getByRole("radio", { name: "Sí" })).toBeDisabled();
    await expect(page.getByRole("textbox", { name: "Potencia óptica" })).toBeDisabled();
    // Solo lectura: nada que guardar; «Siguiente» solo navega.
    await expect(islaPasos(page).getByRole("button", { name: /Guardar/ })).toHaveCount(0);
    await expect(page.getByLabel("Tomar foto: Foto de la fachada")).toHaveCount(0);
    await captura(page, "03-orden-en-pausa", info.project.name);
    await islaPasos(page).getByRole("button", { name: "Siguiente: materiales y equipos" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Materiales y equipos" })).toBeVisible();
    await expect(islaPasos(page).getByRole("button", { name: /Guardar/ })).toHaveCount(0);
    await islaPasos(page).getByRole("button", { name: "Anterior" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();

    await aviso.getByRole("button", { name: "Reanudar" }).click();
    await expect(page.getByTestId("orden-pausada")).toHaveCount(0);
    await expect(page.getByRole("radiogroup", { name: "La ONU enciende" }).getByRole("radio", { name: "Sí" })).toBeEnabled();
    await expect.poll(() => api.lotes.flatMap((l) => (l.operaciones ?? []).map((o) => o.tipo))).toContain("orden.reanudar");
});

test("orden en pausa sin `ordenes.reanudar`: sin «Reanudar», dice a quién pedirlo", async ({ page }) => {
    await simularApiCampo(page, { ordenes: pausada(), permisos: PERMISOS_TECNICO.filter((p) => p !== "ordenes.reanudar") });
    await page.goto("/campo?orden=101&paso=materiales");
    const aviso = page.getByTestId("orden-pausada");
    await expect(aviso).toContainText("pídeselo a despacho");
    await expect(aviso.getByRole("button", { name: "Reanudar" })).toHaveCount(0);
});

test("pasos: sin cambios no hay «Guardar»; con cambios, «Guardar y seguir» con el contador; al guardar, «Guardado»", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: enSitio() });
    await page.goto("/campo");
    const menu = await page.getByRole("navigation", { name: "Secciones de la app" }).evaluate((el) => {
        const s = getComputedStyle(el);
        return { fondo: s.backgroundColor, radio: s.borderTopLeftRadius, borde: s.borderTopWidth };
    });
    await page.goto("/campo?orden=101&paso=checklist");
    await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();
    const isla = islaPasos(page);
    // La misma píldora que la isla de menú.
    expect(await isla.evaluate((el) => ({ fondo: getComputedStyle(el).backgroundColor, radio: getComputedStyle(el).borderTopLeftRadius, borde: getComputedStyle(el).borderTopWidth }))).toEqual(menu);
    // Sin cambios: ningún «Guardar» (ni en la isla ni por campo); el primario es «Siguiente».
    await expect(isla.getByRole("button", { name: "Anterior" })).toBeVisible();
    const siguiente = isla.getByRole("button", { name: "Siguiente: materiales y equipos" });
    await expect(siguiente).toHaveText("Siguiente");
    await expect(page.getByRole("button", { name: /Guardar/ })).toHaveCount(0);
    await captura(page, "04-paso-sin-cambios", info.project.name);

    // Lo que no valida: «Guardar y seguir» con el contador se queda en el paso y dice qué campo falló.
    await page.getByRole("textbox", { name: "Potencia óptica" }).fill("abc");
    const guardarSeguir = isla.getByRole("button", { name: "Guardar y seguir (1 sin guardar)" });
    await expect(guardarSeguir).toBeVisible();
    await expect(guardarSeguir).toHaveText("Guardar y seguir1");
    await expect(isla.getByRole("button", { name: /^Siguiente/ })).toHaveCount(0);
    await guardarYSeguir(page);
    await expect(avisoCampo(page)).toContainText("No se guardó «Potencia óptica»");
    await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();
    await expect(guardarSeguir).toBeVisible();
    await avisoCampo(page).hover();
    await avisoCampo(page).locator('[data-slot="toast-close"]').click();
    await expect(avisoCampo(page)).toHaveCount(0);

    // Válido: guarda, avanza y confirma «Guardado» en la isla (sin aviso encima).
    await page.getByRole("textbox", { name: "Potencia óptica" }).fill("-18,5");
    await captura(page, "05-paso-con-cambios", info.project.name);
    await guardarYSeguir(page);
    await expect(page.getByRole("heading", { level: 1, name: "Materiales y equipos" })).toBeVisible();
    const guardado = isla.locator('[data-slot="guardado-paso"]');
    await expect(guardado).toContainText("Guardado");
    await expect(guardado).toHaveAttribute("role", "status");
    await expect(page.getByRole("button", { name: /Guardar/ })).toHaveCount(0);
    await expect(isla.getByRole("button", { name: "Siguiente: fotos" })).toBeVisible();
    await expect(avisoCampo(page)).toHaveCount(0);
    await captura(page, "08-recien-guardado", info.project.name);
    // Es breve: se va solo (~1,5 s).
    await expect(guardado).not.toContainText("Guardado", { timeout: 4000 });

    // De vuelta, sin cambios: «Anterior» no guarda nada; al salir del campo se guarda solo.
    await isla.getByRole("button", { name: "Anterior" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();
    await expect(page.getByRole("button", { name: /Guardar/ })).toHaveCount(0);
    await page.getByRole("textbox", { name: "Observaciones" }).fill("Cliente conforme");
    await expect(isla.getByRole("button", { name: "Guardar y seguir (1 sin guardar)" })).toBeVisible();
    await page.getByRole("radiogroup", { name: "La ONU enciende" }).getByRole("radio", { name: "Sí" }).click();
    await expect(page.getByRole("button", { name: /Guardar/ })).toHaveCount(0);
    await expect(avisoCampo(page)).toContainText("«La ONU enciende» guardado");
    // Antes de la firma, el primario dice «Ir a firma».
    await siguiente.click();
    await isla.getByRole("button", { name: "Siguiente: fotos" }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Fotos" })).toBeVisible();
    await expect(isla.getByRole("button", { name: "Ir a firma y cierre" })).toHaveText("Ir a firma");
});

test("avisos: el `Toast` de HeroUI queda bajo el margen seguro (la barra de estado y la franja sólida)", async ({ page }, info) => {
    // Un teléfono con muesca: 47 px de margen arriba (`env(safe-area-inset-top)`, con `viewport-fit=cover`).
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Emulation.setSafeAreaInsetsOverride", { insets: { top: 47, topMax: 47, bottom: 34, bottomMax: 34 } });
    await simularApiCampo(page, { ordenes: enSitio() });
    await page.goto("/campo?orden=101&paso=checklist");
    await expect(page.getByRole("heading", { level: 1, name: "Checklist" })).toBeVisible();
    const franja = page.locator('[data-slot="franja-superior"]');
    await expect.poll(async () => (await franja.boundingBox())?.height).toBe(47);
    await page.getByRole("radiogroup", { name: "La ONU enciende" }).getByRole("radio", { name: "Sí" }).click();
    await expect(avisoCampo(page)).toContainText("«La ONU enciende» guardado");
    // Debajo de la franja (el margen seguro) con el mismo aire de antes (1 rem), nunca bajo la hora.
    const region = page.locator(".campo-avisos");
    await expect.poll(async () => (await region.boundingBox())?.y).toBe(47 + 16);
    await expect.poll(async () => (await avisoCampo(page).boundingBox())?.y ?? 0).toBeGreaterThanOrEqual(47 + 16);
    await captura(page, "09-aviso-arriba", info.project.name);
});

test("hojas: «X», Escape y «Atrás» del navegador las cierran; con cambios, preguntan antes", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: enSitio() });
    await page.goto("/campo");
    await page.getByRole("link", { name: /^OT-000101, / }).first().click();
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();
    const pausar = page.getByRole("dialog", { name: "Pausar OT-000101" });

    await page.getByRole("button", { name: "Pausar", exact: true }).click();
    await expect(pausar).toBeVisible();
    const x = pausar.getByRole("button", { name: "Cerrar" });
    await expect(x).toBeVisible();
    const caja = await x.boundingBox();
    expect(Math.min(caja?.width ?? 0, caja?.height ?? 0)).toBeGreaterThanOrEqual(44);
    await captura(page, "06-hoja-con-x", info.project.name);
    await x.click();
    await expect(pausar).toHaveCount(0);

    await page.getByRole("button", { name: "Pausar", exact: true }).click();
    await page.keyboard.press("Escape");
    await expect(pausar).toHaveCount(0);

    // «Atrás» del navegador cierra la hoja y se queda en la orden.
    await page.getByRole("button", { name: "Pausar", exact: true }).click();
    await expect(pausar).toBeVisible();
    await page.goBack();
    await expect(pausar).toHaveCount(0);
    await expect(page).toHaveURL(/\?orden=101$/);
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();

    // Con un motivo elegido: «Atrás» pregunta; «Seguir editando» se queda; la «X» pregunta y «Descartar» cierra.
    await page.getByRole("button", { name: "Pausar", exact: true }).click();
    await pausar.getByLabel("Motivo").selectOption("clima");
    await page.goBack();
    const confirmar = page.getByRole("alertdialog", { name: "¿Descartar los cambios?" });
    await expect(confirmar).toBeVisible();
    await confirmar.getByRole("button", { name: "Seguir editando" }).click();
    await expect(pausar).toBeVisible();
    await expect(page).toHaveURL(/\?orden=101$/);
    await pausar.getByRole("button", { name: "Cerrar" }).click();
    await page.getByRole("alertdialog", { name: "¿Descartar los cambios?" }).getByRole("button", { name: "Descartar" }).click();
    await expect(pausar).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();
    // Sigue en la misma orden y «Atrás» ahora sí vuelve a Hoy (no quedó ninguna entrada de la hoja).
    await page.goBack();
    await expect(page).toHaveURL(/\/campo$/);
});

test("buscar: filtra en vivo y la hoja queda por encima del teclado (simulado)", async ({ page }, info) => {
    await simularApiCampo(page, { ordenes: enSitio() });
    await page.goto("/campo");
    await expect(page.getByRole("region", { name: "Siguiente orden" })).toBeVisible();
    await page.getByRole("button", { name: "Buscar orden" }).first().click();
    const hoja = page.getByRole("dialog", { name: "Buscar orden" });
    const buscar = hoja.getByRole("searchbox", { name: "Buscar" });
    await expect(buscar).toBeFocused();
    await buscar.pressSequentially("maría", { delay: 30 });
    const resultados = hoja.getByRole("list", { name: "Órdenes encontradas" });
    await expect(resultados).toContainText("1 orden");
    await expect(resultados).toContainText("OT-000101");
    await buscar.pressSequentially("x");
    await expect(hoja.getByText("Ninguna orden del teléfono coincide con «maríax».")).toBeVisible();
    await buscar.fill("OT-0001");
    await expect(resultados).toContainText("3 órdenes");

    // Teclado abierto: la ventana visible se acorta 320 px (como en Safari o Chrome sin encoger).
    const alto = await page.evaluate(() => {
        const vv = window.visualViewport;
        if (!vv) {
            return null;
        }
        const visible = window.innerHeight - 320;
        Object.defineProperty(vv, "height", { configurable: true, get: () => visible });
        vv.dispatchEvent(new Event("resize"));
        return visible;
    });
    expect(alto).not.toBeNull();
    await expect.poll(async () => (await hoja.boundingBox())?.y ?? 0).toBeGreaterThanOrEqual(0);
    await expect.poll(async () => {
        const c = await hoja.boundingBox();
        return c ? c.y + c.height : Infinity;
    }).toBeLessThanOrEqual((alto ?? 0) + 1);
    const campo = await buscar.boundingBox();
    expect((campo?.y ?? 0) + (campo?.height ?? 0)).toBeLessThanOrEqual(alto ?? 0);
    // Para la captura: el teclado simulado ocupa lo que tapa.
    await page.evaluate(() =>
        document.body.insertAdjacentHTML(
            "beforeend",
            '<div data-teclado-simulado style="position:fixed;left:0;right:0;bottom:0;height:320px;z-index:2147483647;background:#9ca3af;color:#111;display:grid;place-content:center;font:600 16px system-ui">Teclado (simulado)</div>',
        ),
    );
    await captura(page, "07-buscar-con-teclado", info.project.name);
    await page.evaluate(() => document.querySelector("[data-teclado-simulado]")?.remove());
    await resultados.getByRole("button", { name: /OT-000101/ }).click();
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();
});
