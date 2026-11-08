import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, conSesion, simularApi } from "./api-falsa";
import { abrirMenu, esTelefono, hojaMas } from "./ayudas";
import { elegirOpcion } from "./controles";
import { AHORA_CONV } from "./datos-conversaciones";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Bandeja 1.1 (contrato §14): disponibilidad junto a la cuenta, «¿Alguien la atiende?» con «La
 * atiendo», eventos del bot, «Pasar a…» (también a alguien no disponible), «Nueva conversación»
 * (contacto sin permiso deshabilitado y reintento idempotente), colas con supervisores, el bot del
 * número (máximo 9 opciones) y los ajustes de la bandeja. En los 4 proyectos (escritorio y
 * teléfono, claro y oscuro) y sin desbordes.
 */

const fila = (page: Page, id: number) => page.locator(`li:has([data-conversacion="${id}"])`);

async function abrir(page: Page, ruta: string, correo = ADMIN.correo, opciones: Parameters<typeof simularApi>[1] = {}) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, opciones);
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}

/** El control de disponibilidad: en escritorio, en el panel de la cuenta; en el teléfono, arriba de «Más». */
async function abrirDisponibilidad(page: Page) {
    if (esTelefono(page)) {
        await abrirMenu(page);
        return hojaMas(page).getByRole("region", { name: "Tu disponibilidad" });
    }
    await page.getByRole("button", { name: /^Tu cuenta/ }).click();
    return page.getByRole("dialog", { name: "Tu cuenta y disponibilidad" });
}

test.describe("bandeja 1.1", () => {
    test("disponibilidad junto a la cuenta: explica cuándo se pasa a No disponible y avisa al cambiar", async ({ page }) => {
        const api = await abrir(page, "/conversaciones");
        await expect(page.getByRole("heading", { name: "Conversaciones", level: 1 })).toBeVisible();
        if (!esTelefono(page)) await expect(page.getByRole("button", { name: /^Tu cuenta: Ana López, disponible/ })).toBeVisible();
        const panel = await abrirDisponibilidad(page);
        const grupo = panel.getByRole("radiogroup", { name: "Tu disponibilidad en Conversaciones" });
        await expect(grupo.getByRole("radio", { name: "Disponible", exact: true })).toBeChecked();
        await expect(panel).toContainText("Pasas a No disponible tras 15 min sin actividad, al cerrar sesión o fuera de tu horario (hoy hasta las 22:00).");
        // La hoja «Más» entra deslizándose: se espera a que llegue a su lugar.
        if (esTelefono(page)) await expect.poll(async () => Math.round((await hojaMas(page).boundingBox())?.x ?? 0) + Math.round((await hojaMas(page).boundingBox())?.width ?? 0)).toBeLessThanOrEqual(390);
        await esperarSinDesbordes(page);
        await grupo.getByRole("radio", { name: "No disponible" }).click();
        await expect(page.getByText("Estás No disponible: no te llegan conversaciones nuevas.")).toBeVisible();
        await expect(grupo.getByRole("radio", { name: "No disponible" })).toBeChecked();
        const cambio = api.conversaciones.enviados.find((e) => e.ruta.endsWith("/conversaciones/disponibilidad"));
        expect(cambio?.cuerpo).toEqual({ estado: "no_disponible", version: 1 });
        expect(cambio?.idempotencia).toBeTruthy();
        await grupo.getByRole("radio", { name: "Disponible", exact: true }).click();
        await expect(page.getByText("Estás Disponible: te llegan Soporte y Caja.")).toBeVisible();
    });

    test("con la pestaña activa manda el latido; al cerrar sesión pasa a No disponible en el servidor", async ({ page }) => {
        const api = await abrir(page, "/conversaciones");
        await expect(page.getByRole("heading", { name: "Conversaciones", level: 1 })).toBeVisible();
        await page.mouse.move(200, 200);
        await expect.poll(() => api.conversaciones.bandeja11.latidos.length).toBeGreaterThan(0);
        const antes = api.conversaciones.bandeja11.latidos.length;
        // Muchos movimientos seguidos no mandan más latidos (uno por minuto como mucho).
        for (let i = 0; i < 10; i++) await page.mouse.move(210 + i, 210);
        await page.keyboard.press("Shift");
        expect(api.conversaciones.bandeja11.latidos.length).toBe(antes);

        if (esTelefono(page)) {
            await abrirMenu(page);
            await hojaMas(page).getByRole("menuitem", { name: "Cerrar sesión" }).click();
        } else {
            await page.getByRole("button", { name: /^Tu cuenta/ }).click();
            await page.getByRole("dialog", { name: "Tu cuenta y disponibilidad" }).getByRole("menuitem", { name: "Cerrar sesión" }).click();
        }
        await expect(page).toHaveURL(/\/entrar/);
        expect(api.conversaciones.enviados.find((e) => e.ruta.endsWith("/conversaciones/disponibilidad"))?.cuerpo).toEqual({ estado: "no_disponible", version: 1 });
    });

    test("«¿Alguien la atiende?» en la lista con la cuenta regresiva, disponibles por cola y «La atiendo»", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?vista=sin_asignar");
        const julio = fila(page, 45);
        await expect(julio.getByRole("group", { name: "¿Alguien atiende a Julio Coy?" })).toContainText("Bot en 12 min");
        await expect(julio.locator('[data-slot="disponibles"]')).toHaveText(/2 disponibles en Soporte/);
        const maria = fila(page, 42).locator('[data-slot="disponibles"]');
        await expect(maria).toHaveText("Nadie disponible en Caja");
        await expect(maria).toHaveClass(/text-warning/);
        await expect(fila(page, 46)).toContainText("2 disponibles en Soporte");
        await expect(fila(page, 46).getByRole("group", { name: /Alguien atiende/ })).toHaveCount(0);
        await esperarSinDesbordes(page);

        await julio.getByRole("button", { name: "La atiendo: Julio Coy" }).click();
        await expect(page.getByText("Atiendes a Julio Coy")).toBeVisible();
        await expect(page).toHaveURL(/c=45/);
        expect(api.conversaciones.enviados.some((e) => e.ruta.endsWith("/conversaciones/45/tomar"))).toBe(true);
    });

    test("arriba de la conversación: aviso con «La atiendo» y los eventos del bot en el hilo", async ({ page }) => {
        await abrir(page, "/conversaciones?c=45");
        const conv = page.getByRole("region", { name: "Conversación con Julio Coy" });
        const aviso = conv.getByRole("group", { name: "¿Alguien atiende a Julio Coy?" });
        await expect(aviso).toContainText("El bot contestará en 12 min");
        await expect(aviso).toContainText("Espera hace 18 min");
        await expect(conv.getByRole("log", { name: "Mensajes" })).toContainText("Se avisó a 2 agentes de Soporte");
        await esperarSinDesbordes(page);
        await aviso.getByRole("button", { name: "La atiendo: Julio Coy" }).click();
        await expect(page.getByText("Atiendes a Julio Coy")).toBeVisible();
        await expect(aviso).toHaveCount(0);

        await page.goto("/conversaciones?c=46");
        const hilo = page.getByRole("region", { name: "Conversación con Diana García" }).getByRole("log", { name: "Mensajes" });
        await expect(hilo.locator('[data-evento="bot_atendio"]')).toContainText("Nadie la tomó en 30 min: contestó el bot");
        await expect(hilo.locator('[data-slot="etiqueta-bot"]')).toHaveText("Bot · mensaje automático");
        await esperarSinDesbordes(page);
    });

    test("«Pasar a…» a alguien no disponible: no está deshabilitado y avisa que la verá al volver", async ({ page }) => {
        const api = await abrir(page, "/conversaciones?c=41");
        const conv = page.getByRole("region", { name: "Conversación con Carlos Ruiz" });
        await conv.getByRole("button", { name: "Más acciones" }).click();
        await page.getByRole("menuitem", { name: "Pasar a…" }).click();
        const dialogo = page.getByRole("dialog", { name: "Pasar a…" });
        await expect(dialogo).toContainText("Carlos Ruiz sigue en Soporte y deja de ser tuya.");
        const grupo = dialogo.getByRole("radiogroup", { name: "Compañero de Soporte" });
        await expect(grupo.getByRole("radio", { name: "Rosa Sánchez, disponible" })).toBeEnabled();
        await expect(dialogo.locator('[data-opcion="9"]')).toContainText("3 conversaciones abiertas");
        const mario = grupo.getByRole("radio", { name: "Mario Ixcot, no disponible" });
        await expect(mario).toBeEnabled();
        await expect(dialogo.locator('[data-opcion="15"]')).toContainText("Fuera de su horario");
        await dialogo.locator('[data-opcion="15"]').click();
        await expect(dialogo).toContainText("La verá cuando vuelva a estar disponible.");
        await dialogo.getByLabel("Nota (opcional)").fill("Ya reinició el módem; falta agendar visita.");
        await esperarSinDesbordes(page);
        await dialogo.getByRole("button", { name: "Pasar a Mario" }).click();
        await expect(page.getByText("Carlos Ruiz: pasó a Mario Ixcot. La verá cuando vuelva a estar disponible")).toBeVisible();
        const envio = api.conversaciones.enviados.find((e) => e.ruta.endsWith("/conversaciones/41/pasar"));
        expect(envio?.cuerpo).toEqual({ version: 7, usuarioId: 15, nota: "Ya reinició el módem; falta agendar visita." });
        await expect(conv.getByRole("log", { name: "Mensajes" })).toContainText("Ana la pasó a Mario Ixcot (no disponible)");
    });

    test("«Nueva conversación»: contacto sin permiso deshabilitado, vista previa y reintento con el mismo id", async ({ page }) => {
        const api = await abrir(page, "/conversaciones", ADMIN.correo, { conversaciones: { iniciarFalla503: 1 } });
        await page.getByRole("link", { name: "Nueva conversación" }).click();
        await expect(page).toHaveURL(/\/conversaciones\/nueva$/);
        const form = page.getByRole("form", { name: "Nueva conversación" });
        if (!esTelefono(page)) await expect(page.locator('[data-slot="migas"]').filter({ visible: true }).first()).toContainText("Nueva conversación");
        await form.getByLabel("Contacto").fill("Carlos");
        const resultados = form.getByRole("radiogroup", { name: "Resultados" });
        await expect(resultados.getByRole("radio", { name: "Carlos Cifuentes, No acepta mensajes: se dio de baja" })).toBeDisabled();
        await expect(resultados.getByRole("radio", { name: "Carlos Pérez, Sin número de WhatsApp en Argos" })).toBeDisabled();
        await expect(resultados.locator('[data-opcion="cliente:3877"]')).toContainText("No acepta mensajes: se dio de baja");
        await expect(resultados.getByRole("radio", { name: "Carlos Toj, La atiende otra cola" })).toBeDisabled();
        await expect(resultados.getByRole("radio", { name: "Carlos Velásquez, La atiende otra empresa que usa este número" })).toBeDisabled();
        await resultados.locator('[data-opcion="cliente:4101"]').click();
        await expect(resultados.getByRole("radio", { name: "Carlos Ruiz, acepta mensajes" })).toBeChecked();
        await elegirOpcion(form.getByLabel("Plantilla aprobada"), { label: "Confirmar visita técnica" });
        await expect(form).toContainText("Aprobada");
        await expect(form).toContainText("Servicio");
        await expect(form.getByLabel("{{1}}")).toHaveValue("Carlos");
        await form.getByLabel("{{2}}").fill("hoy entre 13:30 y 15:00");
        const vista = page.getByRole("region", { name: "Vista previa" });
        await expect(vista).toContainText("Hola Carlos, te escribimos de Soporte. Queremos confirmar tu visita técnica de hoy entre 13:30 y 15:00. ¿Te queda bien?");
        await expect(vista).toContainText("Así lo verá Carlos en WhatsApp.");
        await esperarSinDesbordes(page);

        // 503: el mismo id al repetir; el servidor no duplica.
        await page.getByRole("button", { name: "Enviar y abrir conversación" }).click();
        await expect(page.getByRole("alert").filter({ hasText: "No se pudo enviar" })).toBeVisible();
        await page.getByRole("button", { name: "Enviar y abrir conversación" }).click();
        await expect(page.getByText("Conversación con Carlos Ruiz abierta")).toBeVisible();
        await expect(page).toHaveURL(/\/conversaciones\?c=70$/);
        const envios = api.conversaciones.enviados.filter((e) => e.ruta.endsWith("/conversaciones/iniciar"));
        expect(envios).toHaveLength(2);
        const [primero, segundo] = envios.map((e) => e.cuerpo as { id: string });
        expect(primero?.id).toBeTruthy();
        expect(segundo?.id).toBe(primero?.id);
        expect(envios[1]?.idempotencia).toBe(primero?.id);
        expect(envios[0]?.cuerpo).toMatchObject({ canalId: 2, clienteId: 4101, plantilla: { codigo: "confirmar_visita", variables: { "1": "Carlos", "2": "hoy entre 13:30 y 15:00" } } });
    });

    for (const caso of ["asociacion_requiere_permiso", "prohibido"] as const) {
        test(`«Nueva conversación»: un 403 ${caso} se explica sin ofrecer reintentar`, async ({ page }) => {
            await abrir(page, "/conversaciones/nueva", ADMIN.correo, { conversaciones: { iniciar403: caso } });
            const form = page.getByRole("form", { name: "Nueva conversación" });
            await form.getByLabel("Contacto").fill("Carlos");
            await form.locator('[data-opcion="cliente:4101"]').click();
            await elegirOpcion(form.getByLabel("Plantilla aprobada"), { label: "Confirmar visita técnica" });
            await form.getByLabel("{{2}}").fill("hoy");
            await page.getByRole("button", { name: "Enviar y abrir conversación" }).click();
            const alerta = page.getByRole("alert").filter({ hasText: caso === "asociacion_requiere_permiso" ? "No puedes cambiar el contacto de esa conversación" : "No se pudo enviar" });
            await expect(alerta).toBeVisible();
            // Solo el código propio lleva el mensaje de la asociación: el 403 genérico no, aunque su texto la nombre.
            if (caso === "prohibido") await expect(page.getByText("No puedes cambiar el contacto de esa conversación")).toHaveCount(0);
            await expect(alerta).not.toContainText("Vuelve a tocar");
            await esperarSinDesbordes(page);
        });
    }

    test("«Nueva conversación» sin el permiso: no hay botón y la pantalla lo dice", async ({ page }) => {
        await abrir(page, "/conversaciones", "carlos@iris.test");
        await expect(page.getByRole("heading", { name: "Conversaciones", level: 1 })).toBeVisible();
        await expect(page.getByRole("link", { name: "Nueva conversación" })).toHaveCount(0);
        await page.goto("/conversaciones/nueva");
        await expect(page.getByText("No tienes permiso para ver esta sección")).toBeVisible();
    });

    test("colas: miembros y supervisores con su disponibilidad, asistente de IA deshabilitado y minutos propios", async ({ page }) => {
        const api = await abrir(page, "/administracion/colas");
        const soporte = page.locator('[data-cola="2"]');
        await expect(soporte).toContainText("3 miembros · 2 supervisores");
        await esperarSinDesbordes(page);
        await soporte.getByRole("link", { name: "Editar Soporte" }).click();
        const form = page.getByRole("form", { name: "Editar cola «Soporte»" });
        await expect(form.getByRole("list", { name: "Miembros" })).toContainText("Mario Ixcot");
        await expect(form.getByRole("list", { name: "Supervisores" })).toContainText("Laura Méndez");
        await expect(form.getByRole("checkbox", { name: /Asistente de IA \(fase 2\)/ })).toBeDisabled();
        await form.getByRole("button", { name: "Quitar a Ana López" }).last().click();
        const agregar = form.getByRole("combobox", { name: "Agregar a supervisores" });
        await agregar.fill("Rosa S");
        // En el teléfono la lista queda junto a la ayuda del campo: el clic va directo a la opción.
        await page.getByRole("option", { name: /Rosa Sánchez/ }).dispatchEvent("click");
        // La lista sigue abierta para agregar a otra persona; Escape la cierra.
        await page.keyboard.press("Escape");
        await expect(form.getByRole("list", { name: "Supervisores" })).toContainText("Rosa Sánchez");
        await form.getByLabel("Minutos sin agentes disponibles").fill("5");
        await esperarSinDesbordes(page);
        await page.getByRole("button", { name: "Guardar cambios" }).click();
        await expect(page.getByText("Cola «Soporte» guardada")).toBeVisible();
        await expect(page).toHaveURL(/\/administracion\/colas$/);
        const enviados = api.conversaciones.enviados.filter((e) => e.ruta.includes("/colas/2/"));
        expect(enviados.map((e) => e.ruta.split("/").at(-1))).toEqual(["actualizar", "supervisores"]);
        expect(enviados[0]?.cuerpo).toMatchObject({ botSinConfirmacionMin: 30, botSinDisponiblesMin: 5 });
        expect(enviados[1]?.cuerpo).toEqual({ version: 2, usuarioIds: [12, 9] });
    });

    test("número: «Cuando no hay agentes» con el bot, agente autónomo deshabilitado y un menú de hasta 9 opciones", async ({ page }) => {
        const api = await abrir(page, "/administracion/numeros/3/configurar");
        const form = page.getByRole("form", { name: "Configurar Soporte" });
        await expect(form.getByRole("heading", { name: "Cuando no hay agentes" })).toBeVisible();
        await expect(form.getByRole("switch", { name: /Activar bot/ })).toBeChecked();
        await expect(form.locator('[data-slot="bot-detalle"]')).toHaveAttribute("data-activo", "true");
        await expect(form.getByRole("radio", { name: "Bot" })).toBeChecked();
        await expect(form.getByRole("radio", { name: "Agente autónomo (fase 3)" })).toBeDisabled();
        await expect(form.getByLabel("Minutos sin confirmación")).toHaveValue("30");
        await expect(form.getByLabel("Minutos sin agentes disponibles")).toHaveValue("3");
        await expect(form.locator('[data-opcion="2"]')).toBeVisible();
        const agregar = form.getByRole("button", { name: "Agregar opción" });
        for (let i = 3; i <= 9; i++) await agregar.click();
        await expect(agregar).toBeDisabled();
        await expect(form).toContainText("9 de 9 opciones");
        // Las opciones nuevas van vacías: el formulario pide texto y cola.
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(form.getByText("Escribe el texto de la opción").first()).toBeVisible();
        for (let i = 3; i <= 9; i++) {
            await form.getByLabel(`Texto de la opción ${i}`).fill(`Opción ${i}`);
            await elegirOpcion(form.getByLabel(`Cola de la opción ${i}`), { label: "Ventas" });
        }
        await expect(form.locator('[data-slot="vista-bot"]')).toContainText("9. Opción 9");
        await esperarSinDesbordes(page);
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Número «Soporte» guardado")).toBeVisible();
        const cuerpo = api.conversaciones.configurados.at(-1) as { bot: { menu: unknown[]; minutosSinDisponibles: number; atiende: string } };
        expect(cuerpo.bot.menu).toHaveLength(9);
        expect(cuerpo.bot.atiende).toBe("bot");
        expect(cuerpo.bot.minutosSinDisponibles).toBe(3);
    });

    test("ajustes de la bandeja: minutos de inactividad de la empresa", async ({ page }) => {
        const api = await abrir(page, "/administracion/bandeja");
        const campo = page.getByLabel("Minutos de inactividad para pasar a No disponible");
        await expect(campo).toHaveValue("15");
        await campo.fill("0");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Entre 1 y 480 minutos")).toBeVisible();
        await campo.fill("20");
        await esperarSinDesbordes(page);
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Inactividad: 20 min para pasar a No disponible")).toBeVisible();
        expect(api.conversaciones.bandeja11.ajustes()).toEqual({ minutosInactividad: 20, version: 2 });
    });

    test("un agente que no supervisa no ve «Pasar a…» en la conversación de otro", async ({ page }) => {
        await abrir(page, "/conversaciones?c=44", AGENTE_CONV.correo);
        const conv = page.getByRole("region", { name: /Conversación con Ferretería/ });
        await conv.getByRole("button", { name: "Más acciones" }).click();
        await expect(page.getByRole("menuitem", { name: "Pasar a…" })).toHaveCount(0);
        // Tiene `conversaciones.asignar`: «Asignar…» sí aparece.
        await expect(page.getByRole("menuitem", { name: "Asignar…" })).toHaveCount(1);
    });
});
