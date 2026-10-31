import { expect, test, type Page } from "@playwright/test";
import { conSesionTecnico, problema, simularApiCampo, type ApiCampo } from "./api-campo";
import { firmarEn, pestanaCampo } from "./ayudas-campo";

/*
 * «Por recibir» y «Mi bodega» en la app de campo (contrato Iris ↔ ArgosISP 0.5.0, teléfono):
 * recibir un traslado con faltantes y la firma del técnico, sin señal, rechazos (Descartar /
 * Rehacer) y la bodega del técnico en solo lectura.
 */

test.beforeEach(async ({ context, baseURL }, info) => {
    test.skip(!info.project.name.startsWith("telefono"), "La app de campo es para el teléfono (390 px).");
    await conSesionTecnico(context, baseURL ?? "");
});

async function abrirPorRecibir(page: Page) {
    await page.goto("/campo");
    await expect(page.getByRole("link", { name: /OT-000101/ })).toBeVisible();
    await pestanaCampo(page, "Por recibir");
    await expect(page.getByRole("heading", { name: "Por recibir" })).toBeVisible();
}

async function sinSenal(page: Page, api: ApiCampo, sin: boolean) {
    api.sinRed = sin;
    await page.context().setOffline(sin);
}

test("recibe un traslado con lo que faltó y la firma del técnico", async ({ page }) => {
    const api = await simularApiCampo(page);
    await abrirPorRecibir(page);
    const lista = page.getByRole("list", { name: "Traslados" });
    await expect(lista.getByRole("link")).toHaveCount(2);
    await expect(lista.getByRole("link", { name: "Traslado 31, Por recibir" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Por recibir (2)" })).toBeVisible();

    await lista.getByRole("link", { name: /Traslado 31/ }).click();
    await expect(page).toHaveURL(/\/campo\?traslado=31$/);
    await expect(page.getByRole("list", { name: "Productos del traslado" })).toContainText("Cable drop 1 hilo");
    await expect(page.getByRole("list", { name: "Productos del traslado" })).toContainText("ZTEGC8F10001 · ZTEGC8F10002");
    // Faltó una ONT: se desmarca su serie.
    await page.locator('[data-linea="2"]').getByRole("radio", { name: "Faltó algo" }).click();
    await page.getByRole("group", { name: /Series que llegaron/ }).getByLabel("ZTEGC8F10002").uncheck();
    await page.getByRole("button", { name: "Confirmar recepción" }).click();
    await expect(page.getByText("Firma en el recuadro para confirmar que recibiste")).toBeVisible();
    await firmarEn(page, /Firma del técnico/);
    await page.getByRole("button", { name: "Confirmar recepción" }).click();

    await expect.poll(() => api.recepciones.length, { timeout: 15_000 }).toBe(1);
    expect(api.recepciones[0]).toMatchObject({ trasladoId: 31, lineas: [{ lineaId: 2, cantidadRecibida: 1, seriesFaltantes: ["ZTEGC8F10002"] }] });
    const subida = [...api.subidas.values()].find((s) => s.entrada.entidad === "traslado");
    expect(subida?.entrada).toMatchObject({ entidadId: "31", tipo: "firma", mime: "image/png" });
    expect(api.recepciones[0]?.firmaAdjuntoId).toBe(subida?.entrada.id);
    // La lista se relee: ahora va hacia Argos.
    await expect(page.getByRole("article").getByText("Enviando a Argos")).toBeVisible({ timeout: 15_000 });
    await page.getByRole("button", { name: "Por recibir" }).click();
    await expect(page.getByRole("link", { name: "Por recibir (1)" })).toBeVisible();
});

test("sin señal: la recepción queda en el teléfono y sale al volver; la firma se reintenta si el traslado aún no llega", async ({ page }) => {
    const api = await simularApiCampo(page);
    await abrirPorRecibir(page);
    await page.getByRole("link", { name: /Traslado 32/ }).click();
    await expect(page.getByRole("list", { name: "Productos del traslado" })).toContainText("Conector SC/APC");
    await sinSenal(page, api, true);
    await firmarEn(page, /Firma del técnico/);
    await page.getByRole("button", { name: "Confirmar recepción" }).click();
    await expect(page.getByTestId("recepcion-en-telefono")).toContainText("Se enviará al volver la señal.");
    await expect(page.locator('[data-slot="barra-conexion"]')).toContainText("1 pendiente por enviar");
    await expect(page.getByRole("button", { name: "Confirmar recepción" })).toHaveCount(0);

    // Vuelve la señal, pero Iris aún no tiene el traslado: la firma espera y se reintenta sola.
    api.firmaTrasladoNoLlega = 1;
    await sinSenal(page, api, false);
    await expect.poll(() => api.recepciones.length, { timeout: 30_000 }).toBe(1);
    expect(api.recepciones[0]).toMatchObject({ trasladoId: 32 });
    expect(api.recepciones[0]?.lineas).toBeUndefined();
    expect(api.peticiones.filter((p) => p.metodo === "POST" && p.ruta === "/api/v1/adjuntos").length).toBeGreaterThanOrEqual(2);
});

test("rechazos: traslado_no_encontrado solo se descarta; sin respuesta de Argos por 24 h se rehace", async ({ page }) => {
    await page.clock.install();
    const api = await simularApiCampo(page);
    api.rechazar((op) => op.tipo === "traslado.recibir", problema("traslado_no_encontrado", 404, "Traslado no encontrado"));
    await abrirPorRecibir(page);
    await page.getByRole("link", { name: /Traslado 31/ }).click();
    await firmarEn(page, /Firma del técnico/);
    await page.getByRole("button", { name: "Confirmar recepción" }).click();
    const rechazo = page.getByTestId("recepcion-rechazada");
    await expect(rechazo).toContainText("Argos no tiene ese traslado para ti", { timeout: 15_000 });
    await expect(rechazo.getByRole("button", { name: "Rehacer" })).toHaveCount(0);
    // También en «Pendientes», con el traslado y sin «Rehacer».
    await pestanaCampo(page, "Pendientes");
    const tarjeta = page.getByTestId("rechazo");
    await expect(tarjeta).toContainText("Recepción de traslado · Traslado 31");
    await expect(tarjeta.getByRole("button", { name: "Rehacer" })).toHaveCount(0);
    await tarjeta.getByRole("button", { name: "Descartar" }).click();
    await expect(page.getByText("No queda nada por enviar desde este teléfono.")).toBeVisible();

    // Argos no responde (espera propia) y a las 24 h queda rechazada con «Rehacer».
    api.trasladoSinConfirmar = true;
    await pestanaCampo(page, "Por recibir");
    await page.getByRole("link", { name: /Traslado 31/ }).click();
    await firmarEn(page, /Firma del técnico/);
    await page.getByRole("button", { name: "Confirmar recepción" }).click();
    await expect.poll(() => api.resultados.filter((r) => r.error?.type === "urn:iris:problema:traslado_sin_confirmar").length, { timeout: 15_000 }).toBeGreaterThan(0);
    await expect(page.getByTestId("recepcion-en-telefono")).toBeVisible();
    await page.clock.fastForward("25:00:00");
    await pestanaCampo(page, "Pendientes");
    await page.getByRole("button", { name: "Reintentar", exact: true }).click();
    await expect(page.getByTestId("rechazo")).toContainText("Argos no respondió durante 24 h", { timeout: 15_000 });
    api.trasladoSinConfirmar = false;
    await page.getByTestId("rechazo").getByRole("button", { name: "Rehacer" }).click();
    await expect.poll(() => api.recepciones.length, { timeout: 15_000 }).toBe(1);
});

test("Mi bodega: existencias, equipos y movimientos de Argos (solo lectura); errores con «Reintentar»", async ({ page }) => {
    const api = await simularApiCampo(page);
    api.fallarBodega = problema("tecnico_sin_vincular", 422, "Sin vincular");
    await page.goto("/campo");
    await pestanaCampo(page, "Yo");
    await page.getByRole("button", { name: "Mi bodega" }).click();
    await expect(page).toHaveURL(/vista=bodega$/);
    await expect(page.getByText("Tu ficha no está vinculada con tu usuario de Argos.")).toBeVisible();
    api.fallarBodega = null;
    await page.getByRole("button", { name: "Reintentar" }).click();
    await expect(page.getByText("Bodega móvil · Tomás")).toBeVisible();
    const existencias = page.getByRole("list", { name: "Existencias" });
    await expect(existencias).toContainText("Cable drop 1 hilo");
    await expect(existencias).toContainText("120 m");
    await expect(existencias).toContainText("Se cobra al cliente");
    await expect(page.getByRole("list", { name: "Equipos con serie" })).toContainText("ZTEGC8F09999");
    const kardex = page.getByRole("list", { name: "Movimientos recientes" });
    await expect(kardex).toContainText("Consumo · OT-000099");
    await expect(kardex).toContainText("-35");
    await expect(page.getByText("Tienes 2 traslados por recibir")).toBeVisible();
    // Solo lectura: nada que editar.
    await expect(page.getByRole("textbox")).toHaveCount(0);
    await expect(page.getByRole("link", { name: "Yo" })).toHaveAttribute("aria-current", "page");
});
