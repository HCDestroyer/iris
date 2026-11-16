import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, CONSULTA, conSesion, simularApi } from "./api-falsa";
import type { OpcionesAsistente } from "./api-asistente";
import { RESPUESTA_PRUEBA, type OpcionesFase3 } from "./api-asistente3";
import { esTelefono } from "./ayudas";
import { elegirOpcion, esperarOpcion } from "./controles";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Fase 3 del asistente (contratos/conversaciones.md §17; diseño «más amigable y práctico» del PM):
 * la pantalla de las tres preguntas con «Pruébalo antes de guardar», lo avanzado en sus rutas
 * (conexión, límites, categorías) y la bandeja con categorías, actividad del asistente, mensajes
 * del asistente y «Tomar». En los 4 proyectos y sin desbordes en 390 px ni en 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-asistente-3");
const BASE = "/administracion/asistente";

async function abrir(page: Page, ruta: string, fase3: OpcionesFase3 = {}, asistente: OpcionesAsistente = {}, correo = ADMIN.correo) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { conversaciones: { asistente: { ...asistente, fase3 } } });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}

type Api = Awaited<ReturnType<typeof abrir>>;
const rutas = (api: Api, fin: string) => api.conversaciones.enviados.filter((e) => e.ruta.endsWith(fin));
const principal = (page: Page) => page.getByRole("form", { name: "Asistente", exact: true });
const captura = (page: Page, nombre: string, completa = true) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: completa });

test.describe("Administración › Asistente de IA: las tres preguntas", () => {
    test("servidor que aún no actúa solo: «Próximamente», la nota de la pregunta 2 y lo demás se guarda", async ({ page }) => {
        const api = await abrir(page, BASE);
        const f = principal(page);
        await expect(f).toBeVisible();
        await expect(page.getByRole("heading", { level: 1, name: "Asistente" })).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Asistente de IA");
        await expect(page.getByText("Tres preguntas y listo. Puedes cambiarlo cuando quieras.")).toBeVisible();
        await expect(page.getByRole("switch", { name: "Asistente encendido" })).toBeChecked();

        // 1 · Solo «Nunca» se puede elegir mientras el servidor no actúa solo.
        await expect(f.getByRole("radio", { name: "Nunca" })).toBeChecked();
        await expect(f.getByRole("radio", { name: "Cuando no hay nadie (próximamente)" })).toBeDisabled();
        await expect(f.getByRole("radio", { name: "Siempre primero (próximamente)" })).toBeDisabled();
        await expect(f.getByText("Próximamente")).toHaveCount(2);
        await expect(f.getByText("Recomendado")).toHaveCount(0);
        // 2 · Los interruptores se guardan, con la nota.
        await expect(f.locator('[data-slot="nota-autonomo"]')).toHaveText("Se aplicará cuando el asistente atienda solo.");
        await expect(f.getByRole("switch", { name: /Responder dudas/ })).toBeChecked();
        await expect(f.getByRole("switch", { name: /Promesas de pago/ })).not.toBeChecked();
        await expect(f.getByText("Siempre pasa a una persona si el cliente lo pide, se enoja o no sabe la respuesta.")).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "principal-proximamente");

        await f.getByRole("switch", { name: /Promesas de pago/ }).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Asistente guardado y encendido")).toBeVisible();
        const cuerpo = api.conversaciones.asistente.guardados[0] as Record<string, unknown>;
        // Lo de la conexión viaja igual que estaba; cambia solo lo de esta pantalla.
        expect(cuerpo).toMatchObject({ activo: true, proveedor: "ollama", url: "http://10.0.0.20:11434", modelo: "qwen3:32b", modoAutonomo: "nunca", esperaMin: 3, version: 2 });
        expect((cuerpo.acciones as Record<string, boolean>).promesa_pago).toBe(true);
        expect(cuerpo).not.toHaveProperty("clave");
        expect(rutas(api, "/ajustes/actualizar")[0]?.idempotencia).toBeTruthy();
        // Se queda en la pantalla, ya sin cambios pendientes.
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
        await expect(principal(page).getByRole("switch", { name: /Promesas de pago/ })).toBeChecked();
    });

    test("las tres preguntas con el servidor que ya atiende solo: modo, espera, acciones y tipos de mensaje", async ({ page }) => {
        const api = await abrir(page, BASE, { autonomoDisponible: true, ajustes: { modoAutonomo: "sin_nadie" } });
        const f = principal(page);
        await expect(f.getByRole("radio", { name: "Cuando no hay nadie" })).toBeChecked();
        await expect(f.getByText("Recomendado")).toBeVisible();
        await expect(f.getByText("Próximamente")).toHaveCount(0);
        await expect(f.locator('[data-slot="nota-autonomo"]')).toHaveCount(0);
        const espera = f.getByRole("button", { name: /Esperar a un agente/ });
        await esperarOpcion(espera, "3");

        const tipos = f.getByRole("list", { name: "Tipos de mensaje" });
        await expect(tipos.getByRole("listitem")).toHaveCount(6);
        await expect(tipos.locator('[data-categoria="2"] [data-slot="chip-categoria"]')).toHaveText("Sin internet");
        await expect(tipos.locator('[data-categoria="2"] [data-slot="chip-categoria"]')).toHaveAttribute("data-tono", "peligro");
        await esperarOpcion(f.getByRole("button", { name: /Qué hace con «Planes y precios»/ }), "responde_solo");
        await esperarOpcion(f.getByRole("button", { name: /Qué hace con «Cambio de plan»/ }), "sugiere");
        await esperarSinDesbordes(page);
        await captura(page, "principal");

        await elegirOpcion(espera, "5");
        await f.locator('[data-opcion="siempre_primero"]').click();
        await expect(f.locator('[data-slot="espera-agente"]')).toHaveCount(0);
        await f.locator('[data-opcion="sin_nadie"]').click();
        await f.getByRole("switch", { name: /Reconexión temporal/ }).click();
        await elegirOpcion(f.getByRole("button", { name: /Qué hace con «Cambio de plan»/ }), "responde_solo");
        await elegirOpcion(f.getByRole("button", { name: /Qué hace con «Sin internet»/ }), "sugiere");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Asistente guardado y encendido")).toBeVisible();

        expect(api.conversaciones.asistente.guardados[0]).toMatchObject({ modoAutonomo: "sin_nadie", esperaMin: 5, acciones: { reconexion_temporal: true, responder_dudas: true } });
        const cambios = rutas(api, "/actualizar").filter((e) => e.ruta.includes("/categorias/"));
        expect(cambios.map((e) => e.ruta.replace(/^.*categorias\//, ""))).toEqual(["2/actualizar", "4/actualizar"]);
        // «Sugiere» quita la condición; lo demás de la categoría viaja igual.
        expect(cambios[0]?.cuerpo).toMatchObject({ nombre: "Sin internet", modo: "sugiere", condicion: null, colaId: 2, slaPrimeraRespuestaMin: 10, manualIds: [1], version: 1 });
        expect(cambios[1]?.cuerpo).toMatchObject({ nombre: "Cambio de plan", modo: "responde_solo", version: 1 });
        await esperarOpcion(principal(page).getByRole("button", { name: /Qué hace con «Sin internet»/ }), "sugiere");
    });

    test("«Pruébalo antes de guardar» usa los ajustes sin guardar y muestra categoría, qué haría, respuesta y acciones", async ({ page }) => {
        const api = await abrir(page, BASE, { autonomoDisponible: true, ajustes: { modoAutonomo: "sin_nadie" } });
        const f = principal(page);
        const caja = f.getByRole("textbox", { name: "Mensaje de prueba" });
        const probar = f.getByRole("button", { name: "Probar" });
        await expect(probar).toBeDisabled();
        await caja.fill("¿Cuánto cuesta el plan de 100 megas?");
        await probar.click();
        const r = f.getByRole("status", { name: "Resultado de la prueba" });
        await expect(r).toHaveAttribute("data-haria", "responder_solo");
        await expect(r.locator('[data-slot="chip-categoria"]')).toHaveText("Planes y precios");
        await expect(r).toContainText("Respondería solo, con Manual «Planes FTTH y WISP»:");
        await expect(r.locator('[data-slot="respuesta-prueba"]')).toHaveText(RESPUESTA_PRUEBA);
        await r.scrollIntoViewIfNeeded();
        await esperarSinDesbordes(page);
        await captura(page, "pruebalo");

        // Cambio sin guardar: la prueba ya lo usa. Enter prueba (no guarda el formulario).
        await elegirOpcion(f.getByRole("button", { name: /Qué hace con «Planes y precios»/ }), "sugiere");
        await caja.press("Enter");
        await expect(r).toHaveAttribute("data-haria", "sugerir");
        await expect(r).toContainText("Le sugeriría al agente, con Manual «Planes FTTH y WISP»:");
        const enviadas = api.conversaciones.fase3.pruebas as { texto: string; ajustes: { modoAutonomo: string; categorias: { id: number; modo: string }[] } }[];
        expect(enviadas).toHaveLength(2);
        expect(enviadas[1]?.texto).toBe("¿Cuánto cuesta el plan de 100 megas?");
        expect(enviadas[1]?.ajustes.modoAutonomo).toBe("sin_nadie");
        expect(enviadas[1]?.ajustes.categorias.find((c) => c.id === 1)?.modo).toBe("sugiere");
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);

        await caja.fill("Ya pagué, les mando la boleta");
        await probar.click();
        await expect(r.locator('[data-slot="chip-categoria"]')).toHaveText("Pagos y saldo");
        await expect(r.getByRole("list", { name: "Lo que haría" })).toContainText("Recibiría la boleta y la mandaría a Argos para que caja la apruebe");
        await caja.fill("Quiero hablar con una persona");
        await probar.click();
        await expect(r).toHaveAttribute("data-haria", "pasar_a_persona");
        await expect(r).toContainText("Pasaría a una persona.");
        await expect(r.locator('[data-slot="respuesta-prueba"]')).toHaveCount(0);
    });

    test("«Pruébalo»: carga, error del proveedor con «Reintentar» y 429 con la espera", async ({ page }) => {
        await abrir(page, BASE, { probar: "error" });
        const f = principal(page);
        const zona = f.locator('[data-slot="pruebalo"]');
        await f.getByRole("textbox", { name: "Mensaje de prueba" }).fill("No tengo internet");
        await f.getByRole("button", { name: "Probar" }).click();
        await expect(zona.getByText("El asistente está leyendo el mensaje…")).toBeVisible();
        await expect(zona.getByRole("alert")).toContainText("No se pudo probar. El servidor de IA no respondió a tiempo");
        await zona.getByRole("button", { name: "Reintentar" }).click();
        await expect(f.getByRole("status", { name: "Resultado de la prueba" }).locator('[data-slot="chip-categoria"]')).toHaveText("Sin internet");
    });

    test("«Pruébalo» con 429: dice cuánto esperar y «Reintentar» queda deshabilitado hasta entonces", async ({ page }) => {
        await abrir(page, BASE, { probar: "429" });
        const zona = principal(page).locator('[data-slot="pruebalo"]');
        await zona.getByRole("textbox", { name: "Mensaje de prueba" }).fill("No tengo internet");
        await zona.getByRole("button", { name: "Probar" }).click();
        await expect(zona.getByRole("alert")).toContainText("Demasiados pedidos. Vuelve a intentar en 5 s.");
        await expect(zona.getByRole("button", { name: "Reintentar" })).toBeDisabled();
        await esperarSinDesbordes(page);
        await page.clock.setFixedTime(new Date(AHORA_CONV + 6_000));
        await expect(zona.getByRole("button", { name: "Reintentar" })).toBeEnabled();
        await zona.getByRole("button", { name: "Reintentar" }).click();
        await expect(zona.getByRole("status", { name: "Resultado de la prueba" })).toBeVisible();
    });

    test("sin proveedor conectado: lo dice arriba con «Conectar la IA» y las tres preguntas quedan deshabilitadas", async ({ page }) => {
        await abrir(page, BASE, { autonomoDisponible: true }, { ajustes: { url: "", modelo: "", activo: false }, estado: { activo: false } });
        const f = principal(page);
        const aviso = page.getByTestId("sin-conexion");
        await expect(aviso).toContainText("Falta conectar la IA");
        await expect(page.getByRole("switch", { name: "Asistente encendido" })).toBeDisabled();
        for (const nombre of ["Nunca", "Cuando no hay nadie", "Siempre primero"]) await expect(f.getByRole("radio", { name: nombre })).toBeDisabled();
        await expect(f.getByRole("switch", { name: /Responder dudas/ })).toBeDisabled();
        await expect(f.getByRole("button", { name: /Qué hace con «Sin internet»/ })).toBeDisabled();
        await expect(f.getByRole("textbox", { name: "Mensaje de prueba" })).toBeDisabled();
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await esperarSinDesbordes(page);
        await captura(page, "principal-sin-conexion");
        await aviso.getByRole("link", { name: "Conectar la IA" }).click();
        await expect(page).toHaveURL(/\/administracion\/asistente\/conexion$/);
        await expect(page.getByRole("form", { name: "Conexión con la IA" })).toBeVisible();
    });

    test("efecto en el SLA: cifras con el antes; si no se pudo leer, «Reintentar» y ninguna cifra", async ({ page }) => {
        await abrir(page, BASE, { efecto: "error" });
        const f = principal(page);
        const efecto = f.locator('[data-slot="seccion-formulario"]').filter({ hasText: "Efecto en el SLA (este mes)" });
        await expect(efecto.getByText("No se pudo cargar el efecto del mes")).toBeVisible();
        await expect(efecto.locator('[data-slot="efecto-asistente"]')).toHaveCount(0);
        await expect(efecto).not.toContainText("%");
        await efecto.getByRole("button", { name: "Reintentar" }).click();
        await expect(efecto.locator('[data-slot="efecto-asistente"]')).toContainText("1 min 40 s");
        await expect(efecto).toContainText("primera respuesta promedio (antes 14 min)");
        await expect(efecto).toContainText("41 %");
        await expect(efecto).toContainText("resueltas por el asistente sin pasar a una persona");
    });

    test("efecto sin el «antes» y 409 al guardar; salir con cambios pregunta", async ({ page }) => {
        await abrir(page, BASE, { efecto: "sin_antes" }, { conflicto: true });
        const f = principal(page);
        await expect(f.locator('[data-slot="efecto-asistente"]')).toContainText("primera respuesta promedio");
        await expect(f.locator('[data-slot="efecto-asistente"]')).not.toContainText("antes");
        await f.getByRole("switch", { name: /Crear tickets/ }).click();
        await page.getByRole("link", { name: "Ajustar límites" }).click();
        await expect(page.getByRole("alertdialog")).toContainText("¿Descartar los cambios?");
        await page.getByRole("button", { name: "Seguir editando" }).click();
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Alguien más cambió los ajustes").first()).toBeVisible();
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
    });
});

test.describe("Asistente de IA: lo avanzado en su ruta", () => {
    test("límites: validación espejo, guardar con aviso y volver a la principal", async ({ page }) => {
        const api = await abrir(page, BASE);
        await page.getByRole("link", { name: "Ajustar límites" }).click();
        await expect(page).toHaveURL(/\/asistente\/limites$/);
        const f = page.getByRole("form", { name: "Límites del asistente" });
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Límites");
        await expect(f.getByText("Se aplicarán cuando el asistente atienda solo")).toBeVisible();
        await esperarOpcion(f.getByLabel("Prioridad máxima"), "alta");
        const dias = f.getByRole("textbox", { name: /Días hábiles adelante/ });
        await expect(dias).toHaveValue("2");
        await dias.fill("");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(f.getByText("Entre 1 y 30 días")).toBeVisible();
        expect(api.conversaciones.asistente.guardados).toHaveLength(0);
        await dias.fill("3");
        await elegirOpcion(f.getByLabel("Prioridad máxima"), "media");
        await f.getByRole("textbox", { name: /Mensajes del asistente/ }).fill("6");
        await esperarSinDesbordes(page);
        await captura(page, "limites");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Límites del asistente guardados")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/asistente$/);
        expect(api.conversaciones.asistente.guardados[0]).toMatchObject({ limites: { prioridadMaxima: "media", diasAdelanteMax: 3, maxTurnos: 6 }, modoAutonomo: "nunca", proveedor: "ollama" });
    });

    test("categorías: lista con cola, tiempo, manuales y modo; desactivar con confirmación", async ({ page }) => {
        const api = await abrir(page, `${BASE}/categorias`);
        await expect(page.getByRole("heading", { level: 1, name: "Categorías de mensajes" })).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Categorías");
        const lista = page.getByRole("list", { name: "Categorías" });
        const sinInternet = lista.locator('[data-categoria="2"]');
        await expect(sinInternet).toContainText("Cola Soporte · Primera respuesta en 10 min");
        await expect(sinInternet).toContainText("Base: Manual «Sin servicio en fibra: diagnóstico»");
        await expect(sinInternet).toContainText("Responde solo si hay falla; si no, a la cola");
        await expect(lista.locator('[data-categoria="4"]')).toContainText("Primera respuesta en 2 h");
        await expect(lista.locator('[data-categoria="3"]')).toContainText("Responde solo a verificados");
        await esperarSinDesbordes(page);
        await captura(page, "categorias");

        await lista.getByRole("button", { name: "Desactivar Otro" }).click();
        const dialogo = page.getByRole("alertdialog").filter({ hasText: "¿Desactivar «Otro»?" });
        await dialogo.getByRole("button", { name: "Desactivar" }).click();
        await expect(page.getByText("Categoría «Otro» desactivada")).toBeVisible();
        await expect(lista.locator('[data-categoria="6"]')).toContainText("Inactiva");
        expect(rutas(api, "/categorias/6/desactivar")[0]?.cuerpo).toEqual({ version: 1 });
        await lista.getByRole("button", { name: "Activar Otro" }).click();
        await expect(page.getByText("Categoría «Otro» activada")).toBeVisible();
    });

    test("nueva categoría: pantalla propia, validación, 422 junto al campo y crear", async ({ page }) => {
        const api = await abrir(page, BASE);
        await page.getByRole("link", { name: "Agregar tipo" }).click();
        await expect(page).toHaveURL(/\/asistente\/categorias\/nueva$/);
        const f = page.getByRole("form", { name: "Nueva categoría" });
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Nueva categoría");
        await page.getByRole("button", { name: "Crear categoría" }).click();
        await expect(f.getByText("Escribe el nombre de la categoría")).toBeVisible();
        await f.getByLabel("Nombre").fill("Otro");
        await f.getByLabel("Descripción").fill("Quiere que le lleguen las facturas por correo.");
        await elegirOpcion(f.getByLabel("Color del chip"), "alerta");
        await expect(f.locator('[data-slot="vista-chip"] [data-slot="chip-categoria"]')).toHaveAttribute("data-tono", "alerta");
        await elegirOpcion(f.getByLabel("Cola"), { label: "Caja" });
        await f.getByRole("textbox", { name: /Primera respuesta en/ }).fill("45");
        await f.locator('[data-opcion="responde_solo"]').click();
        await elegirOpcion(f.getByLabel("Cuándo responde solo"), "solo_verificados");
        await f.getByRole("checkbox", { name: "Reiniciar la ONU ZTE F670L" }).click();
        await esperarSinDesbordes(page);
        await captura(page, "categoria-nueva");
        await page.getByRole("button", { name: "Crear categoría" }).click();
        await expect(f.getByText("Ya hay una categoría con ese nombre")).toBeVisible();
        await f.getByLabel("Nombre").fill("Facturas");
        await page.getByRole("button", { name: "Crear categoría" }).click();
        await expect(page.getByText("Categoría «Facturas» creada")).toBeVisible();
        await expect(page).toHaveURL(/\/asistente\/categorias$/);
        expect(rutas(api, "/asistente/categorias").at(-1)?.cuerpo).toEqual({
            nombre: "Facturas",
            descripcion: "Quiere que le lleguen las facturas por correo.",
            tono: "alerta",
            colaId: 3,
            slaPrimeraRespuestaMin: 45,
            modo: "responde_solo",
            condicion: "solo_verificados",
            manualIds: [2],
        });
        await expect(page.getByRole("list", { name: "Categorías" })).toContainText("Facturas");
    });

    test("editar categoría: trae lo guardado, «Sugiere» quita la condición, desactiva y 409", async ({ page }) => {
        const api = await abrir(page, `${BASE}/categorias`, { conflictoCategoria: true });
        await page.getByRole("link", { name: "Editar Sin internet" }).click();
        await expect(page).toHaveURL(/\/asistente\/categorias\/2\/editar$/);
        const f = page.getByRole("form", { name: "Editar categoría «Sin internet»" });
        if (!esTelefono(page)) await expect(page.getByRole("list", { name: "Migas" })).toContainText("Editar categoría");
        await expect(f.getByLabel("Nombre")).toHaveValue("Sin internet");
        await esperarOpcion(f.getByLabel("Cuándo responde solo"), "si_hay_falla");
        await expect(f.getByRole("checkbox", { name: "Sin servicio en fibra: diagnóstico" })).toBeChecked();
        await f.locator('[data-opcion="sugiere"]').click();
        await expect(f.getByLabel("Cuándo responde solo")).toHaveCount(0);
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Alguien más cambió esta categoría").first()).toBeVisible();
        expect(rutas(api, "/categorias/2/actualizar")[0]?.cuerpo).toMatchObject({ modo: "sugiere", condicion: null, version: 1 });
        await expect(page).toHaveURL(/\/categorias\/2\/editar$/);
    });

    test("categoría que no existe o id inválido: «no encontrada», sin formulario", async ({ page }) => {
        await abrir(page, `${BASE}/categorias/999/editar`);
        await expect(page.getByText("Categoría no encontrada")).toBeVisible();
        await page.goto(`${BASE}/categorias/abc/editar`);
        await expect(page.getByText("Categoría no encontrada")).toBeVisible();
    });

    test("sin el permiso de configurar no se ve ninguna de las pantallas", async ({ page }) => {
        await abrir(page, `${BASE}/limites`, {}, {}, AGENTE_CONV.correo);
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        await page.goto(`${BASE}/categorias`);
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
        await page.goto(`${BASE}/categorias/nueva`);
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });
});

test.describe("Bandeja con categorías y asistente", () => {
    const lista = (page: Page) => page.getByRole("list", { name: "Lista de conversaciones" });
    const filtros = (page: Page) => page.getByRole("group", { name: "Filtrar por categoría" });

    test("lista: chip de categoría y actividad del asistente en cada fila; filtros con conteos y «Todas» primero", async ({ page }) => {
        const api = await abrir(page, "/conversaciones", { bandeja: true });
        const f = filtros(page);
        await expect(f.getByRole("button").first()).toHaveText(/Todas\s*6/);
        await expect(f.getByRole("button", { name: /Todas/ })).toHaveAttribute("aria-pressed", "true");
        await expect(f.getByRole("button", { name: /Sin internet/ })).toHaveText(/Sin internet\s*2/);
        await expect(f.getByRole("button", { name: /Planes y precios/ })).toHaveText(/Planes y precios\s*1/);
        const carlos = lista(page).locator('[data-conversacion="41"]');
        await expect(carlos.locator('[data-slot="chip-categoria"]')).toHaveText("Sin internet");
        await expect(carlos.locator("[data-actividad]")).toContainText("Asistente respondió: falla en Zona 4");
        await expect(lista(page).locator('[data-conversacion="46"] [data-actividad]')).toContainText("Espera a una persona · vence en 3 h 50 min");
        // Sin categoría: la fila queda como siempre.
        await expect(lista(page).locator('[data-conversacion="44"] [data-slot="linea-categoria"]')).toHaveCount(0);
        await esperarSinDesbordes(page);
        await captura(page, "bandeja-categorias", false);

        await f.getByRole("button", { name: /Sin internet/ }).click();
        await expect(page).toHaveURL(/categoria=2/);
        await expect(f.getByRole("button", { name: /Sin internet/ })).toHaveAttribute("aria-pressed", "true");
        await expect(lista(page).locator("[data-conversacion]")).toHaveCount(2);
        await expect(lista(page).locator('[data-conversacion="42"]')).toHaveCount(0);
        // El filtro viaja al servidor y sobrevive a recargar.
        await page.reload();
        await expect(lista(page).locator("[data-conversacion]")).toHaveCount(2);
        await filtros(page).getByRole("button", { name: /Todas/ }).click();
        await expect(page).not.toHaveURL(/categoria=/);
        await expect(lista(page).locator("[data-conversacion]")).toHaveCount(6);
        expect(api.conversaciones.fase3.bandeja).toBe(true);
    });

    test("servidor sin categorías: la bandeja queda como antes (sin filtros ni chips)", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41");
        await expect(lista(page).locator('[data-conversacion="41"]')).toBeVisible();
        await expect(filtros(page)).toHaveCount(0);
        await expect(page.locator('[data-slot="chip-categoria"]')).toHaveCount(0);
        await expect(page.locator('[data-slot="barra-categoria"]')).toHaveCount(0);
    });

    test("conversación: la categoría con «Cambiar» llama a categorizar y avisa", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41", { bandeja: true });
        const barra = page.locator('[data-slot="barra-categoria"]');
        await expect(barra.locator('[data-slot="chip-categoria"]')).toHaveText("Sin internet");
        await barra.getByRole("button", { name: "Cambiar la categoría" }).click();
        await page.getByRole("menuitem", { name: "Pagos y saldo" }).click();
        await expect(page.getByText("Categoría «Pagos y saldo» puesta en Carlos Ruiz")).toBeVisible();
        await expect(barra.locator('[data-slot="chip-categoria"]')).toHaveText("Pagos y saldo");
        const envio = rutas(api, "/41/categorizar")[0];
        expect(envio?.cuerpo).toEqual({ categoriaId: 3, version: 7 });
        expect(envio?.idempotencia).toBeTruthy();
        await esperarSinDesbordes(page);
        await captura(page, "conversacion-categoria", false);
    });

    test("«Cambiar» con 409: lo dice junto a la categoría y no la cambia", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41", { bandeja: true, conflictoCategorizar: true });
        const barra = page.locator('[data-slot="barra-categoria"]');
        await barra.getByRole("button", { name: "Cambiar la categoría" }).click();
        await page.getByRole("menuitem", { name: "Otro" }).click();
        await expect(barra.getByRole("alert")).toContainText("No se pudo cambiar");
        await expect(barra.locator('[data-slot="chip-categoria"]')).toHaveText("Sin internet");
    });

    test("sin permiso de categorizar se ve la categoría, pero no «Cambiar»", async ({ page }) => {
        await abrir(page, "/conversaciones?c=41", { bandeja: true }, {}, CONSULTA.correo);
        const barra = page.locator('[data-slot="barra-categoria"]');
        await expect(barra.locator('[data-slot="chip-categoria"]')).toHaveText("Sin internet");
        await expect(barra.getByRole("button", { name: "Cambiar la categoría" })).toHaveCount(0);
    });

    test("el asistente atiende solo: su mensaje lleva «Asistente», la franja ofrece «Tomar» y al tomar se detiene", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=43", { bandeja: true, autonomoDisponible: true });
        const hilo = page.getByRole("log", { name: "Mensajes" });
        const suyo = hilo.locator('[data-autor="asistente_ia"]');
        await expect(suyo).toHaveCount(1);
        await expect(suyo.locator('[data-slot="etiqueta-asistente"]')).toHaveText("Asistente");
        await expect(suyo).toContainText("Soy el asistente virtual de Red Sur");
        // No es el bot de menú ni una persona.
        await expect(suyo.locator('[data-slot="etiqueta-bot"]')).toHaveCount(0);
        await expect(suyo).not.toContainText("Respuesta automática");
        const franja = page.locator('[data-slot="franja-asistente"]');
        await expect(franja).toContainText("El asistente está atendiendo");
        await esperarSinDesbordes(page);
        await captura(page, "conversacion-asistente-atiende", false);

        await franja.getByRole("button", { name: /Tomar la conversación/ }).click();
        await expect(page.getByText("Atiendes a +50255440018")).toBeVisible();
        await expect(franja).toHaveCount(0);
        expect(rutas(api, "/43/tomar")[0]?.cuerpo).toEqual({ version: 1 });
        expect(api.conversaciones.conversaciones.find((c) => c.id === 43)?.asistenteAtiende).toBe(false);
    });

    test("la nota de handoff del asistente se ve como nota interna, marcada como suya", async ({ page }) => {
        await abrir(page, "/conversaciones?c=46", { bandeja: true });
        const nota = page.getByRole("log", { name: "Mensajes" }).locator('[data-direccion="nota"][data-autor="asistente_ia"]');
        await expect(nota).toContainText("Nota interna del asistente");
        await expect(nota).toContainText("la dejo en la cola de Soporte");
        await expect(page.locator('[data-slot="franja-asistente"]')).toHaveCount(0);
        await esperarSinDesbordes(page);
    });
});
