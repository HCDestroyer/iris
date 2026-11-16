import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, AGENTE_CONV, CONSULTA, CONTRASENA, conSesion, simularApi } from "./api-falsa";
import { BOLETA_PENDIENTE, URL_ARGOS, type OpcionesPagos } from "./api-pagos";
import { abrirCuenta, esTelefono } from "./ayudas";
import { elegirOpcion } from "./controles";
import { AHORA_CONV } from "./datos-conversaciones";
import { buscarDesbordes, esperarSinDesbordes } from "./desbordes";

/*
 * Pedido B3 (contratos/conversaciones.md §6.4; contrato con Argos 0.7.0 §7.7): aprobar y rechazar
 * la boleta desde la conversación, con cada error diciendo qué hacer, «Vincular mi usuario de
 * Argos» y «Abrir en Argos» con autologin. En los 4 proyectos y sin desbordes.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-boletas");
const captura = (page: Page, nombre: string) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`) });

async function abrir(page: Page, pagos: OpcionesPagos = {}, ruta = "/conversaciones?c=42", correo = ADMIN.correo, conConector = true) {
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    // Argos se abre en otra pestaña: aquí responde una página de prueba, sin salir a la red.
    await page.context().route(`${URL_ARGOS}/**`, (r) => r.fulfill({ contentType: "text/html", body: "<title>Argos</title>Argos" }));
    const api = await simularApi(page, { pagos, enlaces: conConector ? { argosisp: { conectorId: 1, urlPublica: `${URL_ARGOS}/` } } : {} });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", correo);
    await page.goto(ruta);
    return api;
}

/** La ficha del cliente: al lado en escritorio, en una hoja en el teléfono. */
async function ficha(page: Page) {
    if (!esTelefono(page)) return page.getByRole("complementary", { name: "Ficha del cliente" });
    await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
    return page.getByRole("dialog", { name: "Ficha del cliente" });
}

const pendiente = (zona: ReturnType<Page["locator"]>) => zona.locator(`[data-boleta="${BOLETA_PENDIENTE}"]`);

/** Con un diálogo recién abierto: espera a que termine de entrar (se agranda un poco al aparecer). */
const sinDesbordesAlAsentarse = (page: Page) => expect.poll(() => buscarDesbordes(page), "nada se desborda ni queda fuera de pantalla").toEqual([]);

/** Cierra la pestaña de Argos cuando ya llegó a su enlace (cerrarla a media navegación deja colgada a la de Iris). */
async function cerrarPestana(pestana: Page) {
    await pestana.waitForURL(`${URL_ARGOS}/sso/entrar**`);
    await pestana.close();
}

test.describe("boletas en la conversación", () => {
    test("aprobar y registrar pago: confirma el monto, manda la nota y avisa", async ({ page }) => {
        const api = await abrir(page);
        const f = await ficha(page);
        const b = pendiente(f);
        await expect(b).toContainText("Q 250.00");
        await expect(b).toContainText("Boleta BI-778812");
        await expect(b).toContainText("Pendiente de revisar");
        // La que ya aprobó caja dice quién, y no ofrece decidir.
        const vieja = f.locator('[data-estado="aprobada"]');
        await expect(vieja).toContainText("La aprobó Marta Caal.");
        await expect(vieja.getByRole("button")).toHaveCount(0);
        await sinDesbordesAlAsentarse(page);
        await captura(page, "boleta-pendiente");

        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await expect(d).toContainText("María López · boleta BI-778812 · Q 250.00");
        await expect(d).toContainText("Argos registra un pago de Q 250.00 a tu nombre");
        await d.getByLabel("Nota para caja (opcional)").fill("Depósito del lunes");
        await sinDesbordesAlAsentarse(page);
        await captura(page, "boleta-aprobar");
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(page.getByText("Boleta BI-778812 aprobada: pago de Q 250.00 registrado en Argos")).toBeVisible();
        await expect(d).toHaveCount(0);
        await expect(b).toContainText("Aprobada");
        await expect(b).toContainText("La aprobó Ana López (Argos).");
        await expect(b.getByRole("button")).toHaveCount(0);
        expect(api.pagos.decisiones).toHaveLength(1);
        // Va el monto que la persona vio en el diálogo.
        expect(api.pagos.decisiones[0]).toMatchObject({ ruta: `/api/v1/mensajeria/boletas/${BOLETA_PENDIENTE}/aprobar`, cuerpo: { nota: "Depósito del lunes", montoEsperado: "250.00" } });
        expect(api.pagos.decisiones[0]?.idempotencia).toBeTruthy();
    });

    test("rechazar: el motivo es obligatorio (3 a 500), con causa, y avisa", async ({ page }) => {
        const api = await abrir(page);
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Rechazar" }).click();
        const d = page.getByRole("dialog", { name: "Rechazar boleta" });
        await d.getByRole("button", { name: "Rechazar boleta" }).click();
        await expect(d.getByText("Escribe el motivo (al menos 3 letras)")).toBeVisible();
        await expect(d.locator('[data-slot="nota-rechazo"]')).toHaveText("Al cliente le llega un texto fijo según la causa. Tu motivo se agrega solo si no lleva números, puntos ni enlaces.");
        expect(api.pagos.decisiones).toHaveLength(0);
        await d.getByRole("textbox", { name: "Motivo" }).fill("El depósito es por Q 200, no por Q 250.");
        await elegirOpcion(d.getByLabel("Causa"), "monto_distinto");
        await sinDesbordesAlAsentarse(page);
        await captura(page, "boleta-rechazar");
        await d.getByRole("button", { name: "Rechazar boleta" }).click();
        await expect(page.getByText("Boleta BI-778812 rechazada")).toBeVisible();
        await expect(b).toContainText("Rechazada");
        await expect(b).toContainText("Motivo: El depósito es por Q 200, no por Q 250.");
        expect(api.pagos.decisiones[0]).toMatchObject({ ruta: `/api/v1/mensajeria/boletas/${BOLETA_PENDIENTE}/rechazar`, cuerpo: { motivo: "El depósito es por Q 200, no por Q 250.", causa: "monto_distinto" } });
    });

    test("sin vínculo con Argos: «Vincular mi usuario de Argos» abre Argos en otra pestaña y «Reintentar» repite con la misma clave", async ({ page }) => {
        const api = await abrir(page, { sinVinculo: true });
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const aviso = d.locator('[data-error-boleta="vincular"]');
        await expect(aviso).toContainText("Vincula tu usuario de Argos para aprobar pagos");
        await sinDesbordesAlAsentarse(page);
        await captura(page, "boleta-vincular");

        const [pestana] = await Promise.all([page.waitForEvent("popup"), aviso.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click()]);
        await pestana.waitForURL(`${URL_ARGOS}/sso/entrar#ticket-1`);
        // Sin `opener`: Argos no puede tocar la pestaña de Iris.
        expect(await pestana.evaluate(() => window.opener)).toBeNull();
        await cerrarPestana(pestana);
        await expect(aviso.getByRole("status")).toContainText("Argos se abrió en otra pestaña");
        // «Vincular» va sin destino y con su propia clave.
        expect(api.pagos.aperturas).toHaveLength(1);
        expect(api.pagos.aperturas[0]?.cuerpo).toEqual({});
        expect(api.pagos.aperturas[0]?.idempotencia).toBeTruthy();

        await d.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText("Boleta BI-778812 aprobada: pago de Q 250.00 registrado en Argos")).toBeVisible();
        expect(api.pagos.decisiones).toHaveLength(2);
        expect(api.pagos.decisiones[1]?.idempotencia).toBe(api.pagos.decisiones[0]?.idempotencia);
    });

    for (const [codigo, titulo] of [
        ["argos_sin_permiso", "Tu usuario de Argos no tiene permiso de caja"],
        ["argos_sin_empresa", "Tu usuario de Argos no trabaja con esta empresa"],
        ["argos_usuario_inactivo", "Tu usuario de Argos está inactivo"],
        ["argos_sin_aprobacion", "Actualiza Argos a 1.15.0 o posterior"],
    ] as const) {
        test(`${codigo}: se explica y no ofrece «Reintentar»`, async ({ page }) => {
            const api = await abrir(page, { errorDecidir: codigo });
            const b = pendiente(await ficha(page));
            await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
            const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
            await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
            await expect(d.locator('[data-error-boleta="explicar"]')).toContainText(titulo);
            await expect(d.getByRole("button", { name: "Reintentar" })).toHaveCount(0);
            await expect(d.getByRole("button", { name: "Aprobar y registrar pago" })).toHaveCount(0);
            await sinDesbordesAlAsentarse(page);
            await d.getByRole("button", { name: "Cerrar", exact: true }).last().click();
            await expect(d).toHaveCount(0);
            await expect(b).toContainText("Pendiente de revisar");
            expect(api.pagos.decisiones).toHaveLength(1);
        });
    }

    test("otra persona la decidió: relee la boleta y dice quién", async ({ page }) => {
        await abrir(page, { errorDecidir: "otra_persona_decidio" });
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Rechazar" }).click();
        const d = page.getByRole("dialog", { name: "Rechazar boleta" });
        await d.getByRole("textbox", { name: "Motivo" }).fill("No aparece el depósito");
        await d.getByRole("button", { name: "Rechazar boleta" }).click();
        await expect(d).toHaveCount(0);
        await expect(b.locator('[data-slot="decidida-por-otro"]')).toContainText("Ya la decidió otra persona.");
        await expect(b).toContainText("Aprobada");
        await expect(b).toContainText("La aprobó Marta Caal.");
        await expect(b.getByRole("button")).toHaveCount(0);
        await esperarSinDesbordes(page);
    });

    test("Argos no respondió (502): no afirma que no pasó nada; «Reintentar» lo confirma", async ({ page }) => {
        const api = await abrir(page, { errorDecidir: "argos_no_disponible" });
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(d.locator('[data-error-boleta="reintentar"]')).toContainText("Argos no respondió");
        await expect(d.locator('[data-error-boleta="reintentar"]')).toContainText("Puede haberse aplicado en Argos: vuelve a intentar para confirmarlo.");
        await d.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByText("Boleta BI-778812 aprobada: pago de Q 250.00 registrado en Argos")).toBeVisible();
        expect(api.pagos.decisiones).toHaveLength(2);
    });

    test("cerrar el diálogo tras un error vuelve a leer las boletas (no se sabe cómo quedó en Argos)", async ({ page }) => {
        const api = await abrir(page, { errorDecidir: "argos_no_disponible" });
        const b = pendiente(await ficha(page));
        const lecturas = () => api.peticiones.filter((p) => p.metodo === "GET" && p.ruta === "/api/v1/mensajeria/conversaciones/42/boletas").length;
        await expect(b).toContainText("Pendiente de revisar");
        const antes = lecturas();
        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(d.locator('[data-error-boleta="reintentar"]')).toBeVisible();
        // Mientras tanto Argos sí la registró: al cerrar, la tarjeta lo muestra.
        const boleta = api.pagos.boletas.find((x) => x.id === BOLETA_PENDIENTE);
        if (boleta) Object.assign(boleta, { estado: "aprobada", puedeDecidir: false, verificadoPorNombre: "Ana López (Argos)" });
        await d.getByRole("button", { name: "Cancelar" }).click();
        await expect(d).toHaveCount(0);
        await expect.poll(lecturas).toBeGreaterThan(antes);
        await expect(b).toContainText("Aprobada");
        await expect(b.getByRole("button")).toHaveCount(0);
    });

    test("un 409 cuya relectura falla: no muestra un estado sin confirmar, deshabilita decidir y ofrece «Reintentar»", async ({ page }) => {
        await abrir(page, { errorDecidir: "comprobante_no_es_de_iris", relecturaFalla: 2 });
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(d).toHaveCount(0);
        const duda = b.locator('[data-slot="boleta-sin-confirmar"]');
        await expect(duda).toContainText("Cambió en Argos y no se pudo leer cómo quedó.");
        await expect(b).not.toContainText("Pendiente de revisar");
        await expect(b.getByRole("button", { name: "Aprobar y registrar pago" })).toBeDisabled();
        await expect(b.getByRole("button", { name: "Rechazar" })).toBeDisabled();
        await esperarSinDesbordes(page);
        // La primera relectura a mano también falla: sigue igual.
        await duda.getByRole("button", { name: "Reintentar" }).click();
        await expect(duda.getByRole("button", { name: "Reintentar" })).toBeEnabled();
        await expect(b.getByRole("button", { name: "Aprobar y registrar pago" })).toBeDisabled();
        await duda.getByRole("button", { name: "Reintentar" }).click();
        await expect(duda).toHaveCount(0);
        await expect(b).toContainText("Pendiente de revisar");
        await expect(b.getByRole("button", { name: "Aprobar y registrar pago" })).toBeEnabled();
    });

    test("el monto cambió mientras se confirmaba: muestra el nuevo, pide confirmar otra vez y el aviso usa el del servidor", async ({ page }) => {
        const api = await abrir(page, { errorDecidir: "monto_cambio" });
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await expect(d.locator('[data-slot="monto-a-aprobar"]')).toHaveText("Q 250.00");
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(d.locator('[data-error-boleta="confirmar"]')).toContainText("El monto de la boleta cambió");
        // El diálogo sigue abierto, ya con el monto actual.
        await expect(d.locator('[data-slot="monto-a-aprobar"]')).toHaveText("Q 275.00");
        await sinDesbordesAlAsentarse(page);
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(page.getByText("Boleta BI-778812 aprobada: pago de Q 275.00 registrado en Argos")).toBeVisible();
        await expect(b).toContainText("Q 275.00");
        expect(api.pagos.decisiones.map((x) => (x.cuerpo as { montoEsperado: string }).montoEsperado)).toEqual(["250.00", "275.00"]);
    });

    test("el monto que se confirmó ya no es el de la boleta: el servidor no aprueba y se pide confirmar el nuevo", async ({ page }) => {
        const api = await abrir(page);
        const b = pendiente(await ficha(page));
        await b.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        const d = page.getByRole("dialog", { name: "Aprobar y registrar pago" });
        await expect(d.locator('[data-slot="monto-a-aprobar"]')).toHaveText("Q 250.00");
        // Caja corrige el comprobante en Argos con el diálogo abierto.
        const boleta = api.pagos.boletas.find((x) => x.id === BOLETA_PENDIENTE);
        if (boleta) boleta.monto = "300.00";
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(d.locator('[data-error-boleta="confirmar"]')).toBeVisible();
        await expect(d.locator('[data-slot="monto-a-aprobar"]')).toHaveText("Q 300.00");
        expect(boleta?.estado).toBe("registrada");
        await d.getByRole("button", { name: "Aprobar y registrar pago" }).click();
        await expect(page.getByText("Boleta BI-778812 aprobada: pago de Q 300.00 registrado en Argos")).toBeVisible();
    });

    test("Argos no confirmó la boleta: «No se pudo confirmar con Argos», con icono de aviso", async ({ page }) => {
        await abrir(page, { confirmacionArgos: "agotada" });
        const b = pendiente(await ficha(page));
        const aviso = b.locator('[data-confirmacion="agotada"]');
        await expect(aviso).toContainText("No se pudo confirmar con Argos");
        await expect(aviso.locator("svg")).toBeVisible();
        // Decidir desde aquí la sincroniza: los botones siguen.
        await expect(b.getByRole("button", { name: "Aprobar y registrar pago" })).toBeEnabled();
        await sinDesbordesAlAsentarse(page);
        await captura(page, "boleta-sin-confirmar-argos");
    });

    test("sin los permisos de boletas se ve el estado, pero no los botones", async ({ page }) => {
        await abrir(page, { sinPermisos: ["boletas.aprobar", "boletas.rechazar"] });
        const b = pendiente(await ficha(page));
        await expect(b).toContainText("Pendiente de revisar");
        await expect(b.getByRole("button")).toHaveCount(0);
    });

    test("solo `boletas.rechazar`: ofrece rechazar y no aprobar", async ({ page }) => {
        await abrir(page, { sinPermisos: ["boletas.aprobar"] });
        const b = pendiente(await ficha(page));
        await expect(b.getByRole("button", { name: "Rechazar" })).toBeVisible();
        await expect(b.getByRole("button", { name: "Aprobar y registrar pago" })).toHaveCount(0);
    });

    test("si las boletas no se pudieron leer: «Reintentar», sin tarjetas", async ({ page }) => {
        await abrir(page, { lecturaFalla: true });
        const f = await ficha(page);
        const zona = f.getByRole("region", { name: "Boletas" });
        await expect(zona.getByRole("alert")).toContainText("No se pudieron leer las boletas.");
        await expect(zona.locator("[data-boleta]")).toHaveCount(0);
        await zona.getByRole("button", { name: "Reintentar" }).click();
        await expect(pendiente(f)).toContainText("Q 250.00");
    });

    test("una conversación sin boletas no muestra la sección", async ({ page }) => {
        await abrir(page, {}, "/conversaciones?c=41");
        const f = await ficha(page);
        await expect(f.getByText("Creado desde aquí")).toBeVisible();
        await expect(f.getByRole("region", { name: "Boletas" })).toHaveCount(0);
    });
});

test.describe("«Abrir en Argos» y «Vincular mi usuario de Argos»", () => {
    test("ficha del cliente del ticket: «Abrir en Argos» pide el enlace con el destino y lo abre en otra pestaña", async ({ page }) => {
        const api = await abrir(page, {}, "/tickets/33");
        await expect(page.getByRole("heading", { name: "Router RB-Norte-01 caído" })).toBeVisible();
        const boton = page.getByRole("button", { name: /Abrir en Argos/ });
        const [pestana] = await Promise.all([page.waitForEvent("popup"), boton.click()]);
        await pestana.waitForURL(/argos\.isp\.gt\/sso\/entrar#ticket-1-cliente-\d+$/);
        expect(await pestana.evaluate(() => window.opener)).toBeNull();
        await cerrarPestana(pestana);
        const cuerpo = api.pagos.aperturas[0]?.cuerpo as { destino: { tipo: string; idExterno: string } };
        expect(cuerpo.destino.tipo).toBe("cliente");
        expect(cuerpo.destino.idExterno).toMatch(/^\d+$/);
        // Cada clic lleva una clave nueva (el enlace es de un solo uso).
        const [otra] = await Promise.all([page.waitForEvent("popup"), boton.click()]);
        await cerrarPestana(otra);
        expect(api.pagos.aperturas).toHaveLength(2);
        expect(api.pagos.aperturas[1]?.idempotencia).not.toBe(api.pagos.aperturas[0]?.idempotencia);
        await esperarSinDesbordes(page);
    });

    test("si el autologin falla, lo dice y deja el enlace simple de respaldo", async ({ page }) => {
        await abrir(page, { errorAbrir: "conector_apagado" }, "/tickets/33");
        const zona = page.locator('[data-slot="abrir-argos"]');
        await zona.getByRole("button", { name: /Abrir en Argos/ }).click();
        await expect(zona.getByRole("alert")).toContainText("El conector con Argos está apagado");
        const respaldo = zona.getByRole("link", { name: /Ver en Argos/ });
        await expect(respaldo).toHaveAttribute("href", URL_ARGOS);
        await expect(respaldo).toHaveAttribute("rel", "noopener noreferrer");
        // No se arregla reintentando: el botón se va.
        await expect(zona.getByRole("button")).toHaveCount(0);
        await esperarSinDesbordes(page);
    });

    for (const [enlace, caso] of [
        ["otro_sitio", "de otro sitio"],
        ["http", "por http"],
    ] as const) {
        test(`un enlace ${caso} no se abre: la pestaña se cierra y lo dice`, async ({ page }) => {
            const api = await abrir(page, { enlace }, "/cuenta");
            const tarjeta = page.locator("#argos");
            const abierta = page.waitForEvent("popup");
            await tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click();
            const pestana = await abierta;
            await expect(tarjeta.getByRole("alert")).toContainText("El enlace no es del panel de Argos de tu conector, así que Iris no lo abrió.");
            await expect.poll(() => pestana.isClosed()).toBe(true);
            expect(api.pagos.aperturas).toHaveLength(1);
            await esperarSinDesbordes(page);
        });
    }

    test("ventanas emergentes bloqueadas: lo dice y no pide el ticket", async ({ page }) => {
        await page.addInitScript(() => {
            window.open = () => null;
        });
        const api = await abrir(page, {}, "/cuenta");
        const tarjeta = page.locator("#argos");
        await tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click();
        await expect(tarjeta.getByRole("alert")).toHaveText("Permite las ventanas emergentes para abrir Argos.");
        expect(api.pagos.aperturas).toHaveLength(0);
    });

    test("quien atiende, sin `clientes.ver` ni `boletas.*`, también puede vincular (los enlaces valen con `integraciones.abrir_argos`)", async ({ page }) => {
        const api = await abrir(page, {}, "/cuenta", AGENTE_CONV.correo);
        const tarjeta = page.locator("#argos");
        const [pestana] = await Promise.all([page.waitForEvent("popup"), tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click()]);
        await cerrarPestana(pestana);
        await expect(tarjeta.getByRole("status")).toContainText("Argos se abrió en otra pestaña");
        expect(api.pagos.aperturas).toHaveLength(1);
    });

    test("quien atiende ve la boleta, pero no la decide: `boletas.*` son de admin y caja", async ({ page }) => {
        await abrir(page, {}, "/conversaciones?c=42", AGENTE_CONV.correo);
        const b = pendiente(await ficha(page));
        await expect(b).toContainText("Pendiente de revisar");
        await expect(b.getByRole("button")).toHaveCount(0);
    });

    test("sin un conector con Argos activo no abre ni pide nada", async ({ page }) => {
        const api = await abrir(page, {}, "/cuenta", ADMIN.correo, false);
        const tarjeta = page.locator("#argos");
        await tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click();
        await expect(tarjeta.getByRole("alert")).toContainText("No hay un conector con Argos activo");
        expect(api.pagos.aperturas).toHaveLength(0);
    });

    test("sesión abierta desde Argos: pide confirmar la contraseña de Iris y después abre Argos", async ({ page }) => {
        const api = await abrir(page, { sesionSso: true }, "/cuenta");
        const tarjeta = page.locator("#argos");
        const boton = tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ });
        const reaut = page.getByRole("dialog", { name: "Confirma tu contraseña" });
        // Cancelar: no abre y lo explica.
        const primera = page.waitForEvent("popup");
        await boton.click();
        await expect.poll(async () => (await primera).isClosed()).toBe(true);
        await expect(reaut).toBeVisible();
        await reaut.getByRole("button", { name: "Cancelar" }).click();
        await expect(tarjeta.getByRole("alert")).toContainText("Entraste desde Argos: para abrir Argos desde aquí confirma tu contraseña de Iris.");
        expect(api.pagos.aperturas).toHaveLength(0);
        // Confirmar: reintenta solo y abre Argos.
        await boton.click();
        await expect(reaut).toBeVisible();
        await reaut.getByLabel("Contraseña").fill(CONTRASENA);
        const [pestana] = await Promise.all([page.waitForEvent("popup", { predicate: (p) => !p.isClosed() }), reaut.getByRole("button", { name: "Confirmar y seguir" }).click()]);
        await cerrarPestana(pestana);
        await expect(tarjeta.getByRole("status")).toContainText("Argos se abrió en otra pestaña");
        expect(api.pagos.aperturas).toHaveLength(1);
        expect(api.pagos.rechazadasPorSesion()).toBe(2);
        expect(api.sso.reautenticaciones()).toBe(1);
    });

    test("sin `integraciones.abrir_argos` queda el enlace simple de siempre", async ({ page }) => {
        await abrir(page, { sinPermisos: ["integraciones.abrir_argos"] }, "/tickets/33");
        await expect(page.getByRole("link", { name: /Ver en Argos/ })).toHaveAttribute("href", URL_ARGOS);
        await expect(page.getByRole("button", { name: /Abrir en Argos/ })).toHaveCount(0);
    });

    test("menú de la cuenta → Mi perfil: «Vincular mi usuario de Argos»", async ({ page }) => {
        const api = await abrir(page, {}, "/");
        await abrirCuenta(page);
        await page.getByRole("menuitem", { name: "Vincular mi usuario de Argos" }).click();
        await expect(page).toHaveURL(/\/cuenta#argos$/);
        const tarjeta = page.locator("#argos");
        await expect(tarjeta).toContainText("Mi usuario de Argos");
        await esperarSinDesbordes(page);
        await captura(page, "perfil-vincular-argos");
        const [pestana] = await Promise.all([page.waitForEvent("popup"), tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click()]);
        await pestana.waitForURL(`${URL_ARGOS}/sso/entrar#ticket-1`);
        await cerrarPestana(pestana);
        await expect(tarjeta.getByRole("status")).toContainText("Argos se abrió en otra pestaña");
        expect(api.pagos.aperturas[0]?.cuerpo).toEqual({});
    });

    test("demasiados intentos al vincular: dice cuánto esperar", async ({ page }) => {
        await abrir(page, { errorAbrir: "demasiados_intentos" }, "/cuenta");
        const tarjeta = page.locator("#argos");
        await tarjeta.getByRole("button", { name: /Vincular mi usuario de Argos/ }).click();
        await expect(tarjeta.getByRole("alert")).toContainText("Demasiados intentos. Vuelve a probar en 40 s.");
    });

    test("sin el permiso, ni el menú ni el perfil ofrecen vincular", async ({ page }) => {
        await abrir(page, {}, "/cuenta", CONSULTA.correo);
        await expect(page.getByRole("heading", { level: 1, name: "Mi perfil" })).toBeVisible();
        await expect(page.locator("#argos")).toHaveCount(0);
        await abrirCuenta(page);
        await expect(page.getByRole("menuitem", { name: "Vincular mi usuario de Argos" })).toHaveCount(0);
    });
});
