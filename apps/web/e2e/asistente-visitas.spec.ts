import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import type { OpcionesAsistente } from "./api-asistente";
import { VISITAS_DE_FABRICA, type OpcionesFase3 } from "./api-asistente3";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * El asistente agenda visitas (contratos/conversaciones.md §17.10): «Agendar visitas» sin
 * «Próximamente», el bloque «Cómo agenda» en la pantalla simple, su pantalla con las reglas en llano
 * y lo avanzado plegado (espejo del servidor y su 422 junto al campo), «Pruébalo», y en la bandeja y
 * en la orden lo que agendó el asistente. En los 4 proyectos y sin desbordes en 390 px ni 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-asistente-visitas");
const BASE = "/administracion/asistente";
const TRES_B: OpcionesFase3 = { autonomoDisponible: true, ajustes: { modoAutonomo: "sin_nadie" } };

async function abrir(page: Page, ruta: string, fase3: OpcionesFase3 = TRES_B, asistente: OpcionesAsistente = {}) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { conversaciones: { asistente: { ...asistente, fase3 } } });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

type Api = Awaited<ReturnType<typeof abrir>>;
const guardado = (api: Api, n = 0) => api.conversaciones.asistente.guardados[n] as Record<string, unknown>;
const captura = (page: Page, nombre: string, completa = true) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: completa });
const principal = (page: Page) => page.getByRole("form", { name: "Asistente", exact: true });
const formulario = (page: Page) => page.getByRole("form", { name: "Cómo agenda visitas" });

test.describe("Asistente › agenda visitas: pantalla simple", () => {
    test("«Agendar visitas» ya no dice «Próximamente»; encendido muestra «Cómo agenda» en llano y lleva a su pantalla", async ({ page }) => {
        await abrir(page, BASE);
        const f = principal(page);
        const fila = f.locator('[data-accion="agendar_visita"]');
        await expect(fila.getByRole("switch", { name: /Agendar visitas/ })).toBeChecked();
        await expect(fila).not.toContainText("Próximamente");
        const bloque = fila.locator('[data-slot="como-agenda"]');
        await expect(bloque).toContainText("Agenda visita técnica, de 8 a 12 y de 13 a 17.");
        // «Días adelante» de Límites (2) acorta los 7 de fábrica.
        await expect(bloque).toContainText("Ofrece con 12 horas de anticipación y hasta 2 días adelante.");
        await expect(bloque).toContainText("Crea un ticket antes si no hay. Si el servicio está cortado por mora, lo pasa a una persona.");
        await esperarSinDesbordes(page);
        await captura(page, "visitas-principal");
        // Apagado, el bloque se va.
        await fila.getByRole("switch", { name: /Agendar visitas/ }).click();
        await expect(bloque).toHaveCount(0);
        await fila.getByRole("switch", { name: /Agendar visitas/ }).click();
        await bloque.getByRole("link", { name: "Cambiar cómo agenda" }).click();
        await expect(page).toHaveURL(/\/asistente\/visitas$/);
        await expect(formulario(page)).toBeVisible();
    });

    test("guardar la pantalla simple no manda `visitas` ni borra «pedir documento antes de agendar»", async ({ page }) => {
        const general = { codigo: "defecto", nombre: "General", acciones: { responder_dudas: true, avisar_fallas: true, dar_saldo: false, crear_ticket: true, agendar_visita: false, recibir_boleta: false, promesa_pago: false, reconexion_temporal: false, pasar_a_persona: true }, limites: { prioridadMaxima: "alta" as const, diasAdelanteMax: 7 }, exigeVerificacionFuerte: { dar_saldo: true, agendar_visita: true } };
        const api = await abrir(page, BASE, { ...TRES_B, ajustes: { modoAutonomo: "sin_nadie", perfiles: [general] } });
        const f = principal(page);
        // De fábrica viene apagado: sin bloque.
        const fila = f.locator('[data-accion="agendar_visita"]');
        await expect(fila.getByRole("switch", { name: /Agendar visitas/ })).not.toBeChecked();
        await expect(fila.locator('[data-slot="como-agenda"]')).toHaveCount(0);
        await fila.getByRole("switch", { name: /Agendar visitas/ }).click();
        await expect(fila.locator('[data-slot="como-agenda"]')).toContainText("Antes pide DPI o NIT.");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Asistente guardado y encendido")).toBeVisible();
        const cuerpo = guardado(api);
        expect(cuerpo).not.toHaveProperty("visitas");
        expect(cuerpo).toMatchObject({ perfiles: [{ codigo: "defecto", acciones: { agendar_visita: true }, exigeVerificacionFuerte: { dar_saldo: true, agendar_visita: true } }] });
    });

    test("«Pruébalo» refleja la acción encendida y manda la verificación del formulario", async ({ page }) => {
        const api = await abrir(page, BASE, { ...TRES_B, ajustes: { modoAutonomo: "siempre_primero" } });
        const f = principal(page);
        const probar = async () => {
            await f.getByRole("textbox", { name: "Mensaje de prueba" }).fill("¿Cuánto cuesta el plan de 100 megas?");
            await f.getByRole("button", { name: "Probar" }).click();
            await expect(f.getByRole("status", { name: "Resultado de la prueba" })).toHaveAttribute("data-haria", "responde_solo");
        };
        await probar();
        await expect(f.getByRole("list", { name: "Lo que haría" })).toContainText("Propondría hasta 3 espacios para agendar la visita");
        expect(api.conversaciones.fase3.pruebas.at(-1)?.ajustes?.perfil).toMatchObject({ acciones: { agendar_visita: true }, exigeVerificacionFuerte: { dar_saldo: true } });
        // Apagada (sin guardar), la prueba ya no la propone.
        await f.getByRole("switch", { name: /Agendar visitas/ }).click();
        await probar();
        await expect(f.getByRole("list", { name: "Lo que haría" })).toHaveCount(0);
        expect(api.conversaciones.fase3.pruebas.at(-1)?.ajustes?.perfil).toMatchObject({ acciones: { agendar_visita: false } });
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
    });

    test("un servidor que no manda `visitas`: sin bloque, y su pantalla dice que todavía no está", async ({ page }) => {
        const api = await abrir(page, BASE, { ...TRES_B, sinVisitas: true });
        const f = principal(page);
        await expect(f.getByRole("switch", { name: /Agendar visitas/ })).toBeChecked();
        await expect(f.locator('[data-slot="como-agenda"]')).toHaveCount(0);
        await expect(page.getByRole("link", { name: "Cómo agenda visitas" })).toHaveCount(0);
        await page.goto(`${BASE}/visitas`);
        await expect(page.getByTestId("sin-visitas")).toContainText("Todavía no disponible");
        await expect(page.getByRole("form")).toHaveCount(0);
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
    });
});

test.describe("Asistente › agenda visitas: cómo agenda", () => {
    test("trae lo guardado, reglas en llano, lo avanzado plegado y guarda el bloque completo", async ({ page }) => {
        const api = await abrir(page, `${BASE}/visitas`);
        const f = formulario(page);
        await expect(f.getByRole("checkbox", { name: /Visita técnica/ })).toBeChecked();
        await expect(f.getByRole("checkbox", { name: /Mantenimiento/ })).not.toBeChecked();
        await expect(f.getByRole("list", { name: "Horarios que ofrece" }).getByRole("listitem")).toHaveCount(2);
        await expect(f.getByRole("textbox", { name: "Horas de anticipación" })).toHaveValue("12");
        await expect(f.getByRole("textbox", { name: /Días adelante/ })).toHaveValue("7");
        // El límite del perfil (2 días) acorta lo que ofrece: se dice junto al campo.
        await expect(f.getByText("Hoy ofrece hasta 2 días: lo acorta «Días adelante» de Límites.")).toBeVisible();
        await expect(f.getByRole("switch", { name: /Crear un ticket antes si no hay/ })).toBeChecked();
        await expect(f.getByRole("switch", { name: /Agendar aunque el servicio esté cortado por mora/ })).not.toBeChecked();
        await expect(f.getByRole("switch", { name: /Pedir DPI o NIT antes de agendar/ })).not.toBeChecked();
        const avanzado = f.locator('[data-slot="avanzado"]');
        const abrirlo = avanzado.getByRole("button", { name: /Avanzado/ });
        await expect(abrirlo).toHaveAttribute("aria-expanded", "false");
        await expect(abrirlo).toContainText("Hasta 1 visita abierta por cliente · 20 al día en toda la empresa");
        await expect(avanzado.getByRole("textbox")).toHaveCount(0);
        await esperarSinDesbordes(page);
        await captura(page, "visitas-como-agenda");

        await f.getByRole("checkbox", { name: /Mantenimiento/ }).click();
        await f.getByRole("switch", { name: /Agendar aunque el servicio esté cortado por mora/ }).click();
        await f.getByRole("switch", { name: /Pedir DPI o NIT antes de agendar/ }).click();
        await f.getByRole("textbox", { name: "Horas de anticipación" }).fill("24");
        await expect(f.getByText("Ofrece con 1 día de anticipación: da tiempo a organizar al técnico.")).toBeVisible();
        await abrirlo.click();
        await expect(avanzado.getByRole("textbox")).toHaveCount(4);
        await avanzado.getByRole("textbox", { name: "Visitas abiertas por cliente" }).fill("2");
        await esperarSinDesbordes(page);
        await captura(page, "visitas-avanzado");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Cómo agenda el asistente: guardado")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
        const cuerpo = guardado(api);
        // El bloque viaja completo (el POST lo reemplaza), con lo que no se tocó tal como vino.
        expect(cuerpo.visitas).toEqual({ ...VISITAS_DE_FABRICA, tiposPermitidos: ["visita", "mantenimiento"], permitirConMora: true, anticipacionHoras: 24, maxAbiertasPorCliente: 2 });
        expect(cuerpo).toMatchObject({ perfiles: [{ codigo: "defecto", exigeVerificacionFuerte: { dar_saldo: true, agendar_visita: true } }, { codigo: "noche" }] });
        expect(cuerpo).not.toHaveProperty("modoAutonomo");
        // De vuelta en la pantalla simple, el resumen ya lo dice.
        await expect(principal(page).locator('[data-slot="como-agenda"]')).toContainText("Agenda aunque el servicio esté cortado por mora. Antes pide DPI o NIT.");
    });

    test("validación espejo: al menos un tipo, horarios en orden y rangos; nada se manda", async ({ page }) => {
        const api = await abrir(page, `${BASE}/visitas`);
        const f = formulario(page);
        await f.getByRole("checkbox", { name: /Visita técnica/ }).click();
        await f.getByRole("textbox", { name: /Días adelante/ }).fill("");
        await f.getByRole("button", { name: "Quitar el horario 1" }).click();
        await f.getByRole("button", { name: "Agregar horario" }).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Elige al menos un tipo de visita")).toBeVisible();
        await expect(f.getByText("Entre 1 y 30 días")).toBeVisible();
        await expect(f.locator('[data-bloque="2"]').getByText("Hora inválida (HH:MM)").first()).toBeVisible();
        await esperarSinDesbordes(page);
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
        // Con un solo horario no se puede quitar: siempre queda al menos uno.
        await f.getByRole("button", { name: "Quitar el horario 2" }).click();
        await expect(f.getByRole("button", { name: /Quitar el horario/ })).toHaveCount(0);
    });

    test("el tope del servidor (422) sale junto al campo y abre lo avanzado", async ({ page }) => {
        const api = await abrir(page, `${BASE}/visitas`, TRES_B, { topeVisitasEmpresa: 100 });
        const f = formulario(page);
        const avanzado = f.locator('[data-slot="avanzado"]');
        await avanzado.getByRole("button", { name: /Avanzado/ }).click();
        await avanzado.getByRole("textbox", { name: /al día en toda la empresa/ }).fill("300");
        // Se pliega antes de guardar: el error del servidor lo vuelve a abrir.
        await avanzado.getByRole("button", { name: /Avanzado/ }).click();
        await expect(avanzado.getByRole("textbox")).toHaveCount(0);
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(avanzado.getByText("Entre 1 y 100")).toBeVisible();
        await expect(avanzado.getByRole("textbox", { name: /al día en toda la empresa/ })).toHaveValue("300");
        await expect(page).toHaveURL(/\/asistente\/visitas$/);
        expect(api.conversaciones.asistente.guardados).toHaveLength(1);
        await esperarSinDesbordes(page);
        // Corregido, guarda.
        await avanzado.getByRole("textbox", { name: /al día en toda la empresa/ }).fill("80");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Cómo agenda el asistente: guardado")).toBeVisible();
        expect((guardado(api, 1).visitas as { visitasPorDiaEmpresa: number }).visitasPorDiaEmpresa).toBe(80);
    });

    test("con «Agendar visitas» apagado se puede dejar listo, y lo dice", async ({ page }) => {
        const general = { codigo: "defecto", nombre: "General", acciones: { responder_dudas: true, avisar_fallas: true, dar_saldo: false, crear_ticket: true, agendar_visita: false, recibir_boleta: false, promesa_pago: false, reconexion_temporal: false, pasar_a_persona: true }, limites: { prioridadMaxima: "alta" as const, diasAdelanteMax: 7 } };
        await abrir(page, `${BASE}/visitas`, { ...TRES_B, ajustes: { modoAutonomo: "sin_nadie", perfiles: [general] } });
        await expect(page.getByTestId("visitas-apagado")).toContainText("«Agendar visitas» está apagado");
        await expect(formulario(page).getByText("Hasta qué día ofrece horarios, contando desde hoy.")).toBeVisible();
    });
});

test.describe("Asistente › agenda visitas: bandeja y orden", () => {
    const BANDEJA: OpcionesFase3 = { ...TRES_B, bandeja: true, visitaAgendada: true };

    test("la fila dice que agendó la visita; en la conversación, el mensaje y el enlace a la orden", async ({ page }) => {
        await abrir(page, "/conversaciones?c=45", BANDEJA);
        const hilo = page.getByRole("log", { name: "Mensajes" });
        await expect(hilo.locator('[data-evento="orden_creada"]')).toContainText("Asistente de IA creó OT-000001");
        await esperarSinDesbordes(page);
        await captura(page, "visitas-conversacion", false);
        const ficha = page.locator('[data-slot="ficha-cliente"]').first();
        if (!(await ficha.isVisible())) await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        await expect(ficha.getByRole("link", { name: "OT-000001" }).first()).toHaveAttribute("href", "/ordenes/1");
        // En la lista, la actividad del asistente (texto del servidor: la orden, nunca el técnico).
        await page.goto("/conversaciones");
        const fila = page.locator('[data-actividad="visita_agendada"]');
        await expect(fila).toContainText("Agendó la visita OT-000001");
    });

    test("la orden que creó el asistente lleva su marca; las demás, ninguna", async ({ page }) => {
        const api = await abrir(page, "/tickets");
        const [primera, segunda] = api.ordenes.ordenes;
        if (!primera || !segunda) throw new Error("faltan órdenes en el escenario");
        primera.origen = "asistente";
        await page.goto(`/ordenes/${primera.id}`);
        await expect(page.locator('[data-origen-orden="asistente"]')).toHaveText("Agendada por el asistente");
        await esperarSinDesbordes(page);
        await captura(page, "visitas-orden", false);
        await page.goto(`/ordenes/${segunda.id}`);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expect(page.locator("[data-origen-orden]")).toHaveCount(0);
    });
});
