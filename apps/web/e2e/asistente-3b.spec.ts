import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { PERFIL_GENERAL, type OpcionesFase3 } from "./api-asistente3";
import { elegirOpcion, esperarOpcion } from "./controles";
import { AHORA_CONV } from "./datos-conversaciones";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";

/*
 * Fase 3b del asistente: atiende solo (contratos/conversaciones.md §17.8, §17.8.1 y §17.8.2). Las
 * acciones de dinero apagadas de fábrica con su aviso de identidad, lo avanzado plegado, los
 * perfiles por horario (lista, crear, editar, quitar y elegirlos en las franjas) y, en la bandeja,
 * la verificación del cliente, la reconexión pedida y el relevo del asistente a una persona. En los
 * 4 proyectos y sin desbordes en 390 px ni en 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-asistente-3b");
const BASE = "/administracion/asistente";
const TRES_B: OpcionesFase3 = { autonomoDisponible: true, ajustes: { modoAutonomo: "sin_nadie" } };

async function abrir(page: Page, ruta: string, fase3: OpcionesFase3 = TRES_B) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { conversaciones: { asistente: { fase3 } } });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

type Api = Awaited<ReturnType<typeof abrir>>;
const guardado = (api: Api, n = 0) => api.conversaciones.asistente.guardados[n] as Record<string, unknown>;
const captura = (page: Page, nombre: string, completa = true) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: completa });

test.describe("Asistente 3b › qué puede hacer solo", () => {
    test("lo de dinero viene apagado, dice en llano qué le pide al cliente y el saldo se puede bajar a «solo su número»", async ({ page }) => {
        const api = await abrir(page, BASE);
        const f = page.getByRole("form", { name: "Asistente", exact: true });
        // La pregunta 1 ya se puede elegir.
        await expect(f.getByRole("radio", { name: "Cuando no hay nadie" })).toBeChecked();
        await expect(f.getByRole("radio", { name: "Siempre primero" })).toBeEnabled();
        await expect(f.getByText("Próximamente", { exact: true })).toHaveCount(0);

        const dinero = f.getByRole("list", { name: /con dinero o con el servicio/ });
        await expect(dinero.getByRole("listitem")).toHaveCount(4);
        for (const nombre of [/Recibir boletas de pago/, /Dar el saldo/, /Promesas de pago/, /Reconexión temporal/]) await expect(dinero.getByRole("switch", { name: nombre })).not.toBeChecked();
        const aviso = page.getByTestId("aviso-dinero");
        await expect(aviso).toContainText("Vienen apagadas");
        await expect(aviso).toContainText("le pide su DPI o NIT");
        await expect(dinero.locator('[data-accion="promesa_pago"]')).toContainText("Antes le pide su DPI o NIT, siempre");
        await expect(dinero.locator('[data-accion="recibir_boleta"]')).toContainText("Basta con que escriba desde su número");
        // Las visitas todavía no las agenda solo.
        await expect(f.locator('[data-accion="agendar_visita"]')).toContainText("Próximamente");

        const saldo = dinero.locator('[data-accion="dar_saldo"]');
        await expect(saldo.getByRole("checkbox")).toHaveCount(0);
        await saldo.getByRole("switch", { name: /Dar el saldo/ }).click();
        await expect(saldo).toContainText("Antes le pide su DPI o NIT.");
        await saldo.getByRole("checkbox", { name: "Darlo con solo su número, sin pedirle documento" }).click();
        await expect(saldo).toContainText("Basta con que escriba desde su número.");
        await esperarSinDesbordes(page);
        await captura(page, "3b-principal");

        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Asistente guardado y encendido")).toBeVisible();
        expect(guardado(api)).toMatchObject({
            modoAutonomo: "sin_nadie",
            perfiles: [{ codigo: "defecto", acciones: { dar_saldo: true, recibir_boleta: false, promesa_pago: false, reconexion_temporal: false, pasar_a_persona: true }, exigeVerificacionFuerte: { dar_saldo: false } }, { codigo: "noche" }],
        });
    });

    test("límites › avanzado: plegado con su resumen; al abrirlo valida y guarda el presupuesto y la vigencia", async ({ page }) => {
        const api = await abrir(page, `${BASE}/limites`);
        const f = page.getByRole("form", { name: "Límites del asistente" });
        const avanzado = f.locator('[data-slot="avanzado"]');
        const abrirlo = avanzado.getByRole("button", { name: /Avanzado/ });
        await expect(abrirlo).toHaveAttribute("aria-expanded", "false");
        await expect(abrirlo).toContainText("Gasta hasta el 70 % del tope atendiendo solo · la identidad vale 1 h");
        await expect(avanzado.getByRole("textbox")).toHaveCount(0);
        await abrirlo.click();
        const presupuesto = avanzado.getByRole("textbox", { name: /Parte del tope del mes/ });
        const vigencia = avanzado.getByRole("textbox", { name: /Minutos que vale la verificación/ });
        await expect(presupuesto).toHaveValue("70");
        await expect(vigencia).toHaveValue("60");
        await expect(avanzado).toContainText("deja de atender solo");
        await vigencia.fill("");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(avanzado.getByText("Entre 5 y 1,440 minutos (un día)")).toBeVisible();
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
        await vigencia.fill("120");
        await presupuesto.fill("50");
        await presupuesto.blur();
        await expect(abrirlo).toContainText("Gasta hasta el 50 % del tope atendiendo solo · la identidad vale 2 h");
        await esperarSinDesbordes(page);
        await captura(page, "3b-limites-avanzado");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Límites del asistente guardados")).toBeVisible();
        expect(guardado(api)).toMatchObject({ presupuestoAutonomoPct: 50, verificacionFuerteMin: 120, maxTurnos: 8 });
    });
});

test.describe("Asistente 3b › perfiles por horario", () => {
    test("lista, perfil nuevo en su pantalla (nombre repetido junto al campo) y queda al final", async ({ page }) => {
        const api = await abrir(page, BASE);
        await page.getByRole("link", { name: "Distinto según el horario" }).click();
        await expect(page).toHaveURL(/\/asistente\/perfiles$/);
        const lista = page.getByRole("list", { name: "Perfiles", exact: true });
        await expect(lista.getByRole("listitem")).toHaveCount(2);
        await expect(lista.locator('[data-perfil="defecto"]')).toContainText("El de siempre");
        await expect(lista.locator('[data-perfil="defecto"]').getByRole("button", { name: /Quitar/ })).toHaveCount(0);
        await expect(lista.locator('[data-perfil="noche"]')).toContainText("4 acciones encendidas · 1 de dinero");
        await expect(lista.locator('[data-perfil="noche"]')).toContainText("Todavía no se usa");
        await expect(page.getByText("Un perfil se usa cuando lo eliges en un horario")).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "3b-perfiles");

        await page.getByRole("link", { name: "Agregar perfil" }).click();
        await expect(page).toHaveURL(/\/perfiles\/nuevo$/);
        const f = page.getByRole("form", { name: "Nuevo perfil" });
        await page.getByRole("button", { name: "Crear perfil" }).click();
        await expect(f.getByText("Escribe el nombre del perfil")).toBeVisible();
        await f.getByRole("textbox", { name: "Nombre" }).fill("noche");
        await page.getByRole("button", { name: "Crear perfil" }).click();
        await expect(f.getByText("Ya hay un perfil con ese nombre")).toBeVisible();
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
        await f.getByRole("textbox", { name: "Nombre" }).fill("Fin de semana");
        // Empieza como el de fábrica: nada de dinero.
        await expect(f.getByRole("switch", { name: /Dar el saldo/ })).not.toBeChecked();
        await expect(f.getByRole("switch", { name: /Responder dudas/ })).toBeChecked();
        await f.getByRole("switch", { name: /Promesas de pago/ }).click();
        await f.getByRole("switch", { name: /Crear tickets/ }).click();
        await elegirOpcion(f.getByLabel("Prioridad máxima de sus tickets"), "media");
        await esperarSinDesbordes(page);
        await captura(page, "3b-perfil-nuevo");
        await page.getByRole("button", { name: "Crear perfil" }).click();
        await expect(page.getByText("Perfil «Fin de semana» creado")).toBeVisible();
        await expect(page).toHaveURL(/\/asistente\/perfiles$/);
        const cuerpo = guardado(api);
        expect(cuerpo.perfiles).toHaveLength(3);
        expect(cuerpo).toMatchObject({
            perfiles: [
                { codigo: "defecto" },
                { codigo: "noche" },
                {
                    codigo: "fin_de_semana",
                    nombre: "Fin de semana",
                    acciones: { promesa_pago: true, crear_ticket: false, dar_saldo: false, pasar_a_persona: true },
                    exigeVerificacionFuerte: { dar_saldo: true },
                    limites: { prioridadMaxima: "media", diasAdelanteMax: 2 },
                },
            ],
        });
        // Solo viajan los perfiles: la pregunta 1 no es de esta pantalla.
        expect(cuerpo).not.toHaveProperty("modoAutonomo");
        await expect(page.getByRole("list", { name: "Perfiles", exact: true }).locator('[data-perfil="fin_de_semana"]')).toContainText("Fin de semana");
    });

    test("editar trae lo guardado y conserva el código; quitar pide confirmación", async ({ page }) => {
        const api = await abrir(page, `${BASE}/perfiles`);
        const lista = page.getByRole("list", { name: "Perfiles", exact: true });
        await lista.getByRole("link", { name: "Editar Noche" }).click();
        await expect(page).toHaveURL(/\/perfiles\/noche\/editar$/);
        const f = page.getByRole("form", { name: "Editar el perfil «Noche»" });
        await expect(f.getByRole("textbox", { name: "Nombre" })).toHaveValue("Noche");
        await expect(f.getByRole("switch", { name: /Promesas de pago/ })).toBeChecked();
        await esperarOpcion(f.getByLabel("Prioridad máxima de sus tickets"), "media");
        await f.getByRole("textbox", { name: "Nombre" }).fill("Madrugada");
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Perfil «Madrugada» guardado")).toBeVisible();
        expect(guardado(api)).toMatchObject({ perfiles: [{ codigo: "defecto" }, { codigo: "noche", nombre: "Madrugada", acciones: { promesa_pago: true } }] });

        await lista.getByRole("button", { name: "Quitar Madrugada" }).click();
        const dialogo = page.getByRole("alertdialog").filter({ hasText: "¿Quitar el perfil «Madrugada»?" });
        await expect(dialogo).toBeVisible();
        await expect.poll(() => buscarDesbordes(page)).toEqual([]);
        await dialogo.getByRole("button", { name: "Quitar" }).click();
        await expect(page.getByText("Perfil «Madrugada» quitado")).toBeVisible();
        expect((guardado(api, 1).perfiles as unknown[]).length).toBe(1);
        await expect(lista.getByRole("listitem")).toHaveCount(1);
    });

    test("un perfil que no existe: «no encontrado», sin formulario", async ({ page }) => {
        await abrir(page, `${BASE}/perfiles/no_existe/editar`);
        await expect(page.getByText("Perfil no encontrado")).toBeVisible();
        await expect(page.getByRole("form")).toHaveCount(0);
    });

    test("horarios: cada franja elige qué hace el asistente y viaja como `perfil`", async ({ page }) => {
        const api = await abrir(page, "/administracion/horarios/1/editar");
        const f = page.getByRole("form", { name: /Editar «Soporte · extendido»/ });
        await expect(f.locator('[data-slot="nota-perfil"]')).toContainText("En cada franja eliges qué hace el asistente");
        const selector = f.getByLabel("Franja 1: qué hace el asistente");
        await esperarOpcion(selector, "");
        await elegirOpcion(selector, "noche");
        await esperarSinDesbordes(page);
        await captura(page, "3b-horario-franja");
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Horario «Soporte · extendido» guardado")).toBeVisible();
        expect(api.conversaciones.horarios.find((h) => h.id === 1)).toMatchObject({ version: 4, franjas: [{ dias: [1, 2, 3, 4, 5, 6, 7], inicio: "06:00", fin: "22:00", perfil: "noche" }] });
        // La lista de perfiles ya dice dónde se usa.
        await page.goto(`${BASE}/perfiles`);
        await expect(page.locator('[data-perfil="noche"]')).toContainText("En uso en: Soporte · extendido");
    });

    test("sin modo autónomo ni otros perfiles, el horario queda como siempre (sin el selector)", async ({ page }) => {
        await abrir(page, "/administracion/horarios/1/editar", { ajustes: { modoAutonomo: "nunca", perfiles: [structuredClone(PERFIL_GENERAL)] } });
        const f = page.getByRole("form", { name: /Editar «Soporte · extendido»/ });
        await expect(f.locator('input[name="nombre"]')).toHaveValue("Soporte · extendido");
        await expect(f.locator('[data-slot="nota-perfil"]')).toHaveCount(0);
        await expect(f.getByLabel("Franja 1: qué hace el asistente")).toHaveCount(0);
    });
});

test.describe("Asistente 3b › bandeja", () => {
    const BANDEJA: OpcionesFase3 = { bandeja: true, autonomoDisponible: true, ajustes: { modoAutonomo: "sin_nadie" } };
    const indicador = (page: Page) => page.getByRole("region", { name: /^Conversación con/ }).locator("header").locator('[data-slot="verificacion"]');

    async function verFicha(page: Page) {
        const ficha = page.locator('[data-slot="ficha-cliente"]');
        if (!(await ficha.first().isVisible())) await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
        await expect(ficha.first()).toBeVisible();
        return ficha.first();
    }

    test("identidad verificada: indicador discreto y el mensaje de la reconexión pedida", async ({ page }) => {
        await abrir(page, "/conversaciones?c=42", BANDEJA);
        await expect(indicador(page)).toHaveAttribute("data-verificacion", "fuerte");
        await expect(indicador(page)).toContainText("Identidad verificada");
        const evento = page.getByRole("log", { name: "Mensajes" }).locator('[data-evento="reconexion_pedida"]');
        await expect(evento).toContainText("Reconexión temporal: Argos la aceptó (la pidió Asistente de IA)");
        await esperarSinDesbordes(page);
        await captura(page, "3b-conversacion-verificada", false);
    });

    test("«su número coincide»: quien supervisa confirma la identidad y el indicador sale de la respuesta del servidor", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", BANDEJA);
        await expect(indicador(page)).toHaveAttribute("data-verificacion", "numero");
        await expect(indicador(page)).toContainText("Su número coincide");
        const ficha = await verFicha(page);
        await expect(ficha.locator('[data-slot="verificacion"]')).toContainText("Su número coincide");
        await ficha.getByRole("button", { name: "Confirmar identidad" }).click();
        const dialogo = page.getByRole("alertdialog").filter({ hasText: "¿Confirmar que hablas con" });
        await expect(dialogo).toContainText("Hazlo solo si ya comprobaste quién es");
        await expect.poll(() => buscarDesbordes(page)).toEqual([]);
        await dialogo.getByRole("button", { name: "Confirmar identidad" }).click();
        await expect(page.getByText(/^Identidad de .* verificada$/)).toBeVisible();
        expect(api.conversaciones.enviados.some((e) => e.ruta.endsWith("/41/confirmar-cliente"))).toBe(true);
        await expect(page.locator('[data-slot="verificacion"][data-verificacion="fuerte"]').first()).toBeVisible();
        await expect(page.getByRole("button", { name: "Confirmar identidad" })).toHaveCount(0);
    });

    test("cliente sin confirmar: «Sin verificar»; y un servidor que no manda el nivel no muestra nada", async ({ page }) => {
        await abrir(page, "/conversaciones?c=44", BANDEJA);
        await expect(indicador(page)).toHaveAttribute("data-verificacion", "ninguna");
        await expect(indicador(page)).toContainText("Sin verificar");
        await esperarSinDesbordes(page);
        // Servidor anterior a 3b: sin `contacto.verificacion`, ni «verificado» ni «sin verificar».
        await abrir(page, "/conversaciones?c=41", { bandeja: true });
        await expect(page.getByRole("log", { name: "Mensajes" })).toBeVisible();
        await expect(page.locator('[data-slot="verificacion"]')).toHaveCount(0);
    });

    test("el asistente la pasó a una persona: la marca en el hilo, su nota y la franja con «Tomar»", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=46", BANDEJA);
        const hilo = page.getByRole("log", { name: "Mensajes" });
        await expect(hilo.locator('[data-slot="relevo-asistente"]')).toContainText("El asistente la pasó a una persona");
        await expect(hilo.locator('[data-direccion="nota"][data-autor="asistente_ia"]')).toContainText("Nota interna del asistente");
        const franja = page.locator('[data-slot="franja-asistente"]');
        await expect(franja).toHaveAttribute("data-estado", "espera_persona");
        await expect(franja).toContainText("El asistente la pasó a una persona: espera en la cola");
        await esperarSinDesbordes(page);
        await captura(page, "3b-conversacion-relevo", false);
        await franja.getByRole("button", { name: /Tomar la conversación/ }).click();
        await expect.poll(() => api.conversaciones.enviados.some((e) => e.ruta.endsWith("/46/tomar"))).toBe(true);
        await expect(franja).toHaveCount(0);
    });
});
