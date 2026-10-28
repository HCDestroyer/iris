import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, CSRF, LECTOR, VENDEDORA, conSesion, simularApi, type ApiFalsa } from "./api-falsa";
import { avisoError } from "./ayudas";
import { simularTeselas } from "./teselas";

/*
 * Panel comercial (Fase 4.2): embudo, lista y mapa; alta rápida; detalle con factibilidad,
 * cotización, contrato firmado y ganar, con `version` e `Idempotency-Key`; 409 y 422.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const esTelefono = (nombre: string) => nombre.startsWith("telefono");

async function firmar(page: Page) {
    const lienzo = page.getByRole("img", { name: /Firma del cliente/ });
    await lienzo.evaluate((el) => el.scrollIntoView({ block: "center" }));
    const caja = await lienzo.boundingBox();
    if (!caja) throw new Error("sin lienzo");
    await page.mouse.move(caja.x + 30, caja.y + caja.height * 0.7);
    await page.mouse.down();
    for (let i = 1; i <= 12; i += 1) {
        await page.mouse.move(caja.x + 30 + i * 22, caja.y + caja.height * (0.7 - Math.sin(i / 2) * 0.35));
    }
    await page.mouse.up();
    await expect(lienzo).toHaveAccessibleName("Firma del cliente: firmada");
}

const posts = (api: ApiFalsa, sufijo: string) => api.peticiones.filter((p) => p.metodo === "POST" && p.ruta.endsWith(sufijo));

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("embudo y lista", () => {
    test("escritorio: columnas por estado con conteo, «Cargar más», lista con filtros en la URL y abrir el detalle", async ({ page, context, baseURL }, info) => {
        test.skip(esTelefono(info.project.name), "El embudo es de pantalla ancha.");
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial");
        const embudo = page.getByRole("region", { name: "Embudo comercial" });
        const nuevo = embudo.locator('[data-columna="nuevo"]');
        await expect(nuevo.getByTestId("conteo")).toHaveText("20+");
        await expect(embudo.locator('[data-columna="cotizado"]').getByTestId("conteo")).toHaveText("1");
        await expect(embudo.locator('[data-columna="perdido"]').getByText("Gloria Ríos")).toBeVisible();
        await nuevo.getByRole("button", { name: "Cargar más" }).click();
        await expect(nuevo.getByTestId("conteo")).toHaveText("23");
        await sinDesbordar(page);

        await page.getByRole("radio", { name: "Lista" }).click();
        await expect(page).toHaveURL(/vista=lista/);
        await page.getByLabel("Vendedor").selectOption("mios");
        await expect(page).toHaveURL(/responsable=mios/);
        const tabla = page.getByRole("table", { name: "Prospectos" });
        await expect(tabla.getByRole("row")).toHaveCount(4);
        await expect(tabla.getByText("Bruno Díaz")).toBeVisible();
        await page.getByLabel("Buscar").fill("Diego");
        await expect(page).toHaveURL(/q=Diego/);
        await expect(tabla.getByRole("row")).toHaveCount(2);
        await tabla.getByRole("link", { name: /Diego López/ }).click();
        await expect(page).toHaveURL(/\/comercial\/4$/);
        await expect(page.getByRole("heading", { name: "Diego López" })).toBeVisible();
        await page.goBack();
        await expect(page.getByLabel("Buscar")).toHaveValue("Diego");
    });

    test("teléfono: lista con filtro de estado", async ({ page, context, baseURL }, info) => {
        test.skip(!esTelefono(info.project.name), "Vista de teléfono.");
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial");
        await expect(page.getByRole("radiogroup", { name: "Vista" })).toHaveCount(0);
        await expect(page.getByRole("list", { name: "Prospectos" }).getByRole("link")).toHaveCount(25);
        await page.getByLabel("Estado").selectOption("cotizado");
        const lista = page.getByRole("list", { name: "Prospectos" });
        await expect(lista.getByRole("link")).toHaveCount(1);
        await expect(lista.getByText("Cotizado")).toBeVisible();
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/prospectos" && p.query.get("estado") === "cotizado")).toBe(true);
        await sinDesbordar(page);
        await lista.getByRole("link", { name: /Diego López/ }).click();
        await expect(page.getByRole("heading", { name: "Diego López" })).toBeVisible();
        await sinDesbordar(page);
    });

    test("error al leer los prospectos: «Reintentar» y sin datos a medias", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        api.responderProblema("/api/v1/prospectos", { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 }, 1);
        await page.goto("/comercial?vista=lista");
        await expect(avisoError(page).first()).toContainText("No se pudo cargar");
        await expect(page.getByText("Interesado 22")).toHaveCount(0);
        await avisoError(page).first().getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText("Interesado 22").first()).toBeVisible();
    });

    test("sin prospectos.ver no hay Comercial; con solo lectura, el detalle no ofrece acciones", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", LECTOR.correo);
        await page.goto("/comercial");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", CONSULTA.correo);
        await page.goto("/comercial/4");
        await expect(page.getByRole("heading", { name: "Diego López" })).toBeVisible();
        await expect(page.getByRole("group", { name: "Acciones del prospecto" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: "El cliente la aceptó" })).toHaveCount(0);
        await expect(page.getByRole("button", { name: /Nueva revisión|Cotizar/ })).toHaveCount(0);
        await expect(page.getByText("Descargar el PDF requiere el permiso de ver adjuntos.")).toBeVisible();
        await expect(page.getByRole("button", { name: "Nuevo prospecto" })).toHaveCount(0);
    });

    test("un prospecto inexistente o fuera de alcance", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/999");
        await expect(page.getByText("No encontramos este prospecto")).toBeVisible();
    });
});

test.describe("alta rápida", () => {
    test("valida como la API, muestra el 422 en su campo y abre el prospecto creado", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", VENDEDORA.correo);
        await page.goto("/comercial");
        await page.getByRole("button", { name: "Nuevo prospecto" }).click();
        const dialogo = page.getByRole("dialog", { name: "Nuevo prospecto" });
        await dialogo.getByRole("button", { name: "Crear prospecto" }).click();
        await expect(dialogo.getByText("Escribe el nombre")).toBeVisible();
        await expect(dialogo.getByText("Escribe la dirección")).toBeVisible();
        await dialogo.getByLabel("Nombre").fill("Hugo duplicado");
        await dialogo.getByLabel("Dirección").fill("Zona 10");
        await dialogo.getByLabel("Latitud").fill("14.6");
        await dialogo.getByRole("button", { name: "Crear prospecto" }).click();
        await expect(dialogo.getByText("Indica latitud y longitud juntas")).toBeVisible();
        await dialogo.getByLabel("Longitud").fill("-90.5");
        await dialogo.getByLabel("Teléfono").fill("5555-9999");
        await dialogo.getByLabel("Zona").selectOption({ label: "Centro" });
        await dialogo.getByRole("button", { name: "Crear prospecto" }).click();
        await expect(dialogo.getByText("Ya hay un prospecto con ese teléfono")).toBeVisible();
        await dialogo.getByLabel("Nombre").fill("Hugo Sánchez");
        await dialogo.getByRole("button", { name: "Crear prospecto" }).click();
        await expect(page).toHaveURL(/\/comercial\/122$/);
        await expect(page.getByRole("heading", { name: "Hugo Sánchez" })).toBeVisible();
        const altas = posts(api, "/api/v1/prospectos");
        expect(altas).toHaveLength(2);
        expect(altas[1]?.cuerpo).toEqual({ nombre: "Hugo Sánchez", direccion: "Zona 10", origen: "panel", telefono: "5555-9999", zonaId: 4, lat: 14.6, lon: -90.5 });
        expect(altas[1]?.idempotencia).toBeTruthy();
        expect(altas[1]?.idempotencia).not.toBe(altas[0]?.idempotencia);
    });
});

test.describe("del prospecto al cliente", () => {
    test("sin conector: factibilidad indeterminada → visita técnica → cotizar a mano → aceptar → contrato → firmar → ganar con alta local", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/2");
        await expect(page.getByRole("heading", { name: "Bruno Díaz" })).toBeVisible();
        await expect(page.getByText("Próxima: Evaluar factibilidad")).toBeVisible();

        const fact = page.getByTestId("factibilidad");
        await fact.getByRole("button", { name: "Evaluar" }).click();
        await expect(fact.getByTestId("motivo-indeterminado")).toContainText("no está conectado con Argos");
        await expect(fact.getByText("Indeterminado")).toBeVisible();
        await fact.getByRole("button", { name: "Registrar visita técnica" }).click();
        const visita = page.getByRole("dialog", { name: "Visita técnica" });
        await visita.getByLabel("Notas").fill("ok");
        await visita.getByRole("button", { name: "Guardar resultado" }).click();
        await expect(visita.getByText("Describe la visita (al menos 20 caracteres)")).toBeVisible();
        await visita.getByLabel("Notas").fill("Poste con caja a 80 m, hay puertos libres");
        await visita.getByRole("button", { name: "Guardar resultado" }).click();
        await expect(fact.getByText("Viable", { exact: true })).toBeVisible();
        await expect(fact.getByText("Visita técnica", { exact: true })).toBeVisible();
        const factibilidades = posts(api, "/factibilidad");
        expect(factibilidades[0]?.cuerpo).toEqual({ version: 1 });
        expect(factibilidades).toHaveLength(1);
        expect(posts(api, "/factibilidad-manual")[0]?.cuerpo).toEqual({ version: 2, resultado: "viable", notas: "Poste con caja a 80 m, hay puertos libres" });

        const cot = page.getByTestId("cotizacion");
        await cot.getByRole("button", { name: "Cotizar" }).click();
        const dCot = page.getByRole("dialog", { name: "Cotizar" });
        await expect(dCot.getByText("Sin conexión con Argos")).toBeVisible();
        await dCot.getByRole("button", { name: "Instalación" }).click();
        await dCot.getByRole("button", { name: "Guardar cotización" }).click();
        await expect(dCot.getByText("Es obligatoria").first()).toBeVisible();
        const lineas = dCot.getByRole("list", { name: "Líneas" }).getByRole("listitem");
        await lineas.nth(0).getByLabel("Descripción").fill("Fibra 50");
        await lineas.nth(0).getByLabel("Precio unitario").fill("199.99");
        await lineas.nth(1).getByLabel("Descripción").fill("Instalación");
        await lineas.nth(1).getByLabel("Precio unitario").fill("150");
        await expect(dCot.getByText("Los totales los calcula Iris al guardar.")).toBeVisible();
        await dCot.getByRole("button", { name: "Guardar cotización" }).click();
        await expect(cot.getByTestId("total-mensual")).toContainText("199.99");
        await expect(cot.getByTestId("total-unico")).toContainText("150.00");
        expect(posts(api, "/cotizaciones")[0]?.cuerpo).toEqual({
            version: 3,
            lineas: [
                { tipo: "plan", descripcion: "Fibra 50", precioUnitario: "199.99" },
                { tipo: "instalacion", descripcion: "Instalación", precioUnitario: "150" },
            ],
        });

        await cot.getByRole("button", { name: "El cliente la aceptó" }).click();
        await expect(cot.getByText("Aceptada", { exact: true })).toBeVisible();
        const contrato = page.getByTestId("contrato");
        await contrato.getByRole("button", { name: "Generar contrato" }).click();
        await expect(contrato.getByText("Borrador", { exact: true })).toBeVisible();
        await contrato.getByRole("button", { name: "Firmar con el cliente" }).click();
        const dFirma = page.getByRole("dialog", { name: /Firmar CTR-/ });
        await expect(dFirma.getByLabel("Nombre de quien firma")).toHaveValue("Bruno Díaz");
        await dFirma.getByRole("button", { name: "Guardar firma" }).click();
        await expect(dFirma.getByText("Falta la firma del cliente")).toBeVisible();
        await firmar(page);
        await dFirma.getByRole("button", { name: "Guardar firma" }).click();
        await expect(contrato.getByText("Firmado", { exact: true })).toBeVisible();
        const firma = posts(api, "/firmar")[0]?.cuerpo as { version: number; firmanteNombre: string; svg: string };
        expect(firma.version).toBe(1);
        expect(firma.firmanteNombre).toBe("Bruno Díaz");
        expect(firma.svg).toMatch(/^<svg [^>]*width="[\d.]+" height="[\d.]+"/);
        expect(firma.svg).toContain('<path d="M');

        await page.getByRole("button", { name: "Ganar" }).click();
        const dGanar = page.getByRole("dialog", { name: "Ganar el prospecto" });
        await expect(dGanar.getByRole("list", { name: "Requisitos para ganar" }).getByRole("listitem")).toHaveCount(3);
        await dGanar.getByRole("button", { name: "Ganar" }).click();
        await expect(page.getByTestId("alta")).toContainText("Alta local");
        await expect(page.getByRole("link", { name: /Orden de instalación OT-000045/ })).toHaveAttribute("href", "/ordenes/45");
        await expect(page.locator("header").getByText("Ganado").first()).toBeVisible();

        // Toda acción con CSRF e Idempotency-Key.
        const acciones = api.peticiones.filter((p) => p.metodo === "POST" && /prospectos\/2|cotizaciones\/|contratos\//.test(p.ruta));
        expect(acciones.length).toBeGreaterThanOrEqual(7);
        expect(acciones.every((p) => p.idempotencia)).toBe(true);
        expect(api.mutaciones.filter((m) => m.metodo === "POST").every((m) => m.csrf === CSRF)).toBe(true);
    });

    test("con conector: factibilidad viable con router y distancia, planes de Argos y el precio del plan no se manda", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { comercialConector: true });
        await conSesion(context, baseURL ?? "", VENDEDORA.correo);
        await page.goto("/comercial/2");
        const fact = page.getByTestId("factibilidad");
        await fact.getByRole("button", { name: "Evaluar" }).click();
        await expect(fact.getByText("R-Norte")).toBeVisible();
        await expect(fact.getByTestId("distancia")).toHaveText("182 m");
        await expect(fact.getByText("viable hasta 300 m, con obra hasta 1 km")).toBeVisible();
        await page.getByTestId("cotizacion").getByRole("button", { name: "Cotizar" }).click();
        const dCot = page.getByRole("dialog", { name: "Cotizar" });
        await expect(dCot.getByLabel("Plan").locator("option", { hasText: "descontinuado" })).toHaveCount(0);
        await dCot.getByLabel("Plan").selectOption("72");
        await expect(dCot.getByText(/Precio de Argos: Q\s?400\.00 al mes/)).toBeVisible();
        await dCot.getByRole("button", { name: "Descuento" }).click();
        const lineas = dCot.getByRole("list", { name: "Líneas" }).getByRole("listitem");
        await lineas.nth(1).getByLabel("Descripción").fill("Promoción");
        await lineas.nth(1).getByLabel("Monto a restar").fill("50");
        await lineas.nth(1).getByLabel("Se cobra cada mes").check();
        await dCot.getByRole("button", { name: "Guardar cotización" }).click();
        await expect(page.getByTestId("total-mensual")).toContainText("350.00");
        // Los planes se piden con el router de la factibilidad; el descontinuado no se ofrece.
        expect(api.peticiones.find((p) => p.ruta === "/api/v1/comercial/planes")?.query.get("routerId")).toBe("501");
        expect(posts(api, "/cotizaciones")[0]?.cuerpo).toMatchObject({ lineas: [{ tipo: "plan", planId: 72 }, { tipo: "descuento", descripcion: "Promoción", precioUnitario: "50", recurrente: true }] });
    });

    test("la visita técnica: ventas no la ofrece; una que contradice la automática «no viable» pide el motivo y lo muestra", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", VENDEDORA.correo);
        await page.goto("/comercial/3");
        const fact = page.getByTestId("factibilidad");
        await expect(fact.getByTestId("motivo-indeterminado")).toContainText("Pide a un supervisor o a un técnico");
        await expect(fact.getByRole("button", { name: "Registrar visita técnica" })).toHaveCount(0);
        await expect(fact.getByRole("button", { name: "Volver a evaluar" })).toBeVisible();

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/8");
        await expect(fact.getByText("No viable", { exact: true })).toBeVisible();
        await fact.getByRole("button", { name: "Registrar visita técnica" }).click();
        const visita = page.getByRole("dialog", { name: "Visita técnica" });
        await visita.getByLabel("Notas").fill("Hay una caja nueva a 120 m del domicilio");
        await visita.getByRole("button", { name: "Guardar resultado" }).click();
        await expect(visita.getByText("Explica por qué la visita contradice la evaluación automática")).toBeVisible();
        await visita.getByLabel("Motivo de la anulación").fill("Caja instalada la semana pasada; el catálogo no se ha sincronizado");
        await visita.getByRole("button", { name: "Guardar resultado" }).click();
        await expect(fact.getByTestId("anula")).toContainText("Caja instalada la semana pasada");
        await expect(fact.getByText("Viable", { exact: true })).toBeVisible();
        expect(posts(api, "/factibilidad-manual")[0]?.cuerpo).toMatchObject({ resultado: "viable", motivoAnulacion: "Caja instalada la semana pasada; el catálogo no se ha sincronizado" });
    });

    test("con conector y sin router sugerido, ganar pide elegir el router de la cobertura", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { comercialConector: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/10");
        await page.getByRole("button", { name: "Ganar" }).click();
        const dGanar = page.getByRole("dialog", { name: "Ganar el prospecto" });
        await dGanar.getByRole("button", { name: "Ganar" }).click();
        await expect(dGanar.getByText("Elige el router desde donde se atenderá")).toBeVisible();
        expect(posts(api, "/ganar")).toHaveLength(0);
        await dGanar.getByLabel("Router del servicio").selectOption({ label: "R-Norte" });
        await dGanar.getByRole("button", { name: "Ganar" }).click();
        await expect(page.getByTestId("alta").getByText("Creando el cliente en Argos")).toBeVisible();
        expect(posts(api, "/ganar")[0]?.cuerpo).toEqual({ version: 1, routerId: 501 });
    });

    test("un alta detenida en Argos se ve como error", async ({ page, context, baseURL }) => {
        await simularApi(page, { comercialConector: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/11");
        await expect(page.getByTestId("alta-error")).toContainText("no corresponde a este prospecto");
        await expect(page.getByText("La orden de instalación se crea cuando Argos confirme")).toHaveCount(0);
    });

    test("ganar con requiere obra pide confirmarla; con conector el alta queda en Argos", async ({ page, context, baseURL }) => {
        const api = await simularApi(page, { comercialConector: true });
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/5");
        await expect(page.getByTestId("factibilidad").getByText("Requiere obra")).toBeVisible();
        await page.getByTestId("contrato").getByRole("button", { name: "Firmar con el cliente" }).click();
        await firmar(page);
        await page.getByRole("dialog", { name: /Firmar CTR-/ }).getByRole("button", { name: "Guardar firma" }).click();
        await expect(page.getByTestId("contrato").getByText("Firmado", { exact: true })).toBeVisible();
        await page.getByRole("button", { name: "Ganar" }).click();
        const dGanar = page.getByRole("dialog", { name: "Ganar el prospecto" });
        await expect(dGanar.getByText("La factibilidad requiere obra")).toBeVisible();
        await dGanar.getByRole("button", { name: "Ganar" }).click();
        await expect(dGanar.getByText("Confirma la obra para ganar")).toBeVisible();
        expect(posts(api, "/ganar")).toHaveLength(0);
        await dGanar.getByLabel(/Confirmo que la instalación requiere obra/).check();
        await dGanar.getByRole("button", { name: "Ganar" }).click();
        const alta = page.getByTestId("alta");
        await expect(alta.getByText("Creando el cliente en Argos")).toBeVisible();
        await expect(alta.getByText("Crear el cliente en Argos")).toBeVisible();
        await expect(alta.getByText("La orden de instalación se crea cuando Argos confirme")).toBeVisible();
        expect(posts(api, "/ganar")[0]?.cuerpo).toEqual({ version: 2, confirmarObra: true });
    });

    test("alta en Argos con un pendiente agotado: «Reintentar» (supervisor) y luego la orden de instalación", async ({ page, context, baseURL }) => {
        await simularApi(page, { comercialConector: true });
        await conSesion(context, baseURL ?? "", VENDEDORA.correo);
        await page.goto("/comercial/6");
        const alta = page.getByTestId("alta");
        await expect(alta.getByText("Sin respuesta de Argos")).toBeVisible();
        await expect(alta.getByText("Reintentar requiere el permiso de reintentar integraciones (supervisor).")).toBeVisible();

        await context.clearCookies();
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.reload();
        await alta.getByRole("button", { name: "Reintentar" }).click();
        await expect(alta.getByText("Alta completa")).toBeVisible();
        await expect(alta.getByRole("link", { name: /OT-000044/ })).toHaveAttribute("href", "/ordenes/44");
    });

    test("409 en una acción: «Recargar» y reenviar con la versión nueva; un 422 muestra el detail sin perder lo escrito", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/1");
        await expect(page.getByRole("heading", { name: "Ana Gómez" })).toBeVisible();
        api.comercial.cambiarVersion(1);
        await page.getByRole("button", { name: "Marcar contactado" }).click();
        const dialogo = page.getByRole("dialog", { name: "Marcar como contactado" });
        await dialogo.getByLabel("Nota (opcional)").fill("Respondió por WhatsApp");
        await dialogo.getByRole("button", { name: "Marcar contactado" }).click();
        await expect(dialogo.getByText("Alguien más cambió este prospecto")).toBeVisible();
        await expect(dialogo.getByRole("button", { name: "Marcar contactado" })).toBeDisabled();
        await dialogo.getByRole("button", { name: "Recargar" }).click();
        await expect(dialogo.getByLabel("Nota (opcional)")).toHaveValue("Respondió por WhatsApp");
        await dialogo.getByRole("button", { name: "Marcar contactado" }).click();
        await expect(dialogo).toHaveCount(0);
        await expect(page.locator("header").getByText("Contactado").first()).toBeVisible();
        const envios = posts(api, "/contactar");
        expect(envios.map((e) => (e.cuerpo as { version: number }).version)).toEqual([1, 2]);
        expect(envios[1]?.idempotencia).not.toBe(envios[0]?.idempotencia);

        api.responderProblema("/api/v1/prospectos/1/perder", { type: "urn:iris:problema:transicion_invalida", title: "Regla de negocio", status: 422, detail: "No se puede perder un prospecto con contrato firmado" }, 1);
        await page.getByRole("button", { name: "Perder" }).click();
        const perder = page.getByRole("dialog", { name: "Marcar como perdido" });
        await perder.getByLabel("Motivo").fill("Precio");
        await perder.getByRole("button", { name: "Marcar perdido" }).click();
        await expect(perder.getByText("No se puede perder un prospecto con contrato firmado")).toBeVisible();
        await expect(perder.getByLabel("Motivo")).toHaveValue("Precio");
    });

    test("actividad con próxima acción e historial", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", VENDEDORA.correo);
        await page.goto("/comercial/1");
        const actividades = page.getByTestId("actividades");
        await actividades.getByRole("button", { name: "Registrar" }).click();
        const form = page.getByRole("form", { name: "Nueva actividad" });
        await form.getByLabel("Tipo").selectOption("visita");
        await form.getByLabel("Qué pasó").fill("Visité la casa, hay poste enfrente");
        await form.getByLabel("Próxima acción (opcional)").fill("Enviar cotización");
        await form.getByLabel("Día de la próxima acción").fill("2026-11-03");
        await form.getByLabel("Hora (opcional)").fill("10:30");
        await form.getByRole("button", { name: "Guardar actividad" }).click();
        await expect(actividades.getByText("Visité la casa, hay poste enfrente")).toBeVisible();
        await expect(actividades.getByText("Próxima: Enviar cotización")).toBeVisible();
        expect(posts(api, "/actividades")[0]?.cuerpo).toEqual({ tipo: "visita", descripcion: "Visité la casa, hay poste enfrente", proximaAccion: "Enviar cotización", proximaFecha: "2026-11-03T16:30:00Z" });
        await expect(page.getByTestId("historial").getByText("Prospecto creado")).toBeVisible();
    });

    test("«Asignar vendedor» no aparece en un prospecto ganado ni perdido", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/1");
        await expect(page.getByRole("button", { name: "Asignar vendedor" })).toBeVisible();
        for (const [id, nombre] of [
            [6, "Fernando Paz"],
            [7, "Gloria Ríos"],
        ] as const) {
            await page.goto(`/comercial/${id}`);
            await expect(page.getByRole("heading", { name: nombre })).toBeVisible();
            await expect(page.getByRole("button", { name: "Asignar vendedor" })).toHaveCount(0);
        }
        await expect(page.getByRole("button", { name: "Reabrir" })).toBeVisible();
    });

    test("«Generar PDF» del contrato manda Idempotency-Key y luego permite descargarlo", async ({ page, context, baseURL }) => {
        const api = await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/5");
        const contrato = page.getByTestId("contrato");
        await expect(contrato.getByText("El PDF no se pudo guardar.")).toBeVisible();
        await contrato.getByRole("button", { name: "Generar PDF" }).click();
        await expect(contrato.getByRole("button", { name: "Descargar borrador" })).toBeVisible();
        const envio = posts(api, "/contratos/52/pdf");
        expect(envio).toHaveLength(1);
        expect(envio[0]?.idempotencia).toBeTruthy();
        expect(api.mutaciones).toContainEqual({ metodo: "POST", ruta: "/api/v1/contratos/52/pdf", csrf: CSRF });
    });

    test("descargar el PDF de la cotización con la sesión", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial/4");
        const descarga = page.waitForEvent("download");
        await page.getByTestId("cotizacion").getByRole("button", { name: "Descargar PDF" }).click();
        expect((await descarga).suggestedFilename()).toBe("COT-000041-r1.pdf");
    });
});

test.describe("mapa de prospectos y cobertura", () => {
    test("con conector: prospectos por estado, router y círculos de cobertura, sin salir a la red", async ({ page, context, baseURL }, info) => {
        test.skip(esTelefono(info.project.name), "El mapa es de pantalla ancha.");
        await simularApi(page, { comercialConector: true });
        const teselas = await simularTeselas(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial?vista=mapa");
        const mapa = page.getByTestId("mapa");
        await expect(mapa).toHaveAttribute("data-listo", "si");
        await expect(page.locator("[data-marcador-prospecto]")).toHaveCount(5);
        await expect(page.locator('[data-marcador-prospecto="PR-000004"]')).toHaveAttribute("data-estado", "cotizado");
        await expect(page.getByRole("img", { name: /Router R-Norte/ })).toBeVisible();
        await expect(mapa).toHaveAttribute("data-cobertura", "2");
        await expect(page.getByText("Viable hasta 300 m")).toBeVisible();
        expect(teselas.pedidas()).toBeGreaterThan(0);
        await page.locator('[data-marcador-prospecto="PR-000004"]').click();
        await expect(page).toHaveURL(/\/comercial\/4$/);
    });

    test("sin conector: lo dice con el motivo y no dibuja cobertura", async ({ page, context, baseURL }, info) => {
        test.skip(esTelefono(info.project.name), "El mapa es de pantalla ancha.");
        await simularApi(page);
        await simularTeselas(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.goto("/comercial?vista=mapa");
        await expect(page.getByText("Sin datos de cobertura")).toBeVisible();
        await expect(page.getByText(/no está conectado con Argos/)).toBeVisible();
        await expect(page.getByTestId("mapa")).toHaveAttribute("data-cobertura", "0");
        await expect(page.locator("[data-marcador-router]")).toHaveCount(0);
        await expect(page.locator("[data-marcador-prospecto]")).toHaveCount(5);
    });
});

test("capturas del panel comercial", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `comercial-${que}-${info.project.name}.png`);
    await simularApi(page, { comercialConector: true });
    await simularTeselas(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/comercial");
    await expect(page.getByText("Interesado 22").first()).toBeVisible();
    await page.screenshot({ path: nombre("01-embudo"), fullPage: true });
    if (!esTelefono(info.project.name)) {
        await page.goto("/comercial?vista=lista");
        await expect(page.getByRole("table", { name: "Prospectos" })).toBeVisible();
        await page.screenshot({ path: nombre("02-lista"), fullPage: true });
        await page.goto("/comercial?vista=mapa");
        await expect(page.getByTestId("mapa")).toHaveAttribute("data-listo", "si");
        await expect(page.locator("[data-marcador-prospecto]")).toHaveCount(5);
        await page.screenshot({ path: nombre("03-mapa"), fullPage: true });
    }
    await page.goto("/comercial/4");
    await expect(page.getByTestId("cotizacion").getByTestId("total-mensual")).toBeVisible();
    await page.screenshot({ path: nombre("04-detalle-cotizado"), fullPage: true });
    await page.goto("/comercial/3");
    await expect(page.getByTestId("motivo-indeterminado")).toBeVisible();
    await page.screenshot({ path: nombre("05-indeterminado"), fullPage: true });
    await page.goto("/comercial/2");
    await page.getByTestId("factibilidad").getByRole("button", { name: "Evaluar" }).click();
    await expect(page.getByTestId("distancia")).toBeVisible();
    await page.getByTestId("cotizacion").getByRole("button", { name: "Cotizar" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Instalación" }).click();
    await page.screenshot({ path: nombre("06-cotizar") });
    await page.keyboard.press("Escape");
    await page.goto("/comercial/5");
    await page.getByTestId("contrato").getByRole("button", { name: "Firmar con el cliente" }).click();
    await firmar(page);
    await page.screenshot({ path: nombre("07-firmar") });
    await page.getByRole("dialog").getByRole("button", { name: "Guardar firma" }).click();
    await page.getByRole("button", { name: "Ganar" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Ganar" }).click();
    await page.screenshot({ path: nombre("08-ganar-obra") });
    await page.goto("/comercial/6");
    await expect(page.getByTestId("alta")).toBeVisible();
    await page.screenshot({ path: nombre("09-alta-argos"), fullPage: true });
});
