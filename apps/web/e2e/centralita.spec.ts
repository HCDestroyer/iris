import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, EMPRESAS, simularApi, type OpcionesApi } from "./api-falsa";
import { HUELLA_NUEVA, llamadaCentralita, type OpcionesCentralita } from "./api-centralita";
import { elegirOpcion } from "./controles";
import { esperarSinDesbordes } from "./desbordes";

/*
 * Centralita Issabel, E4a (contratos/voz.md §3.12): conectar con prueba antes de guardar (huella y
 * solo lectura), el estado de la conexión, las extensiones, el aviso cuando suena tu extensión
 * (reconocido, varios posibles, desconocido, contestó otra persona, al colgar la nota) y las
 * llamadas en el ticket. En los 4 proyectos y sin desbordes en 390 px ni en 1280 px.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-centralita");
const RUTA = "/administracion/integraciones/centralita";
const UNA = [EMPRESAS[0] ?? { id: 10, nombre: "Cable Norte" }];

async function abrir(page: Page, ruta: string, centralita: OpcionesCentralita = {}, extra: Partial<OpcionesApi> = {}) {
    const api = await simularApi(page, { centralita, empresas: UNA, conversaciones: { flujoLargo: true }, ...extra });
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", ADMIN.correo);
    await page.goto(ruta);
    return api;
}

// Las de la ventana, sin la página entera de fondo (la ventana flota arriba).
const captura = (page: Page, nombre: string) =>
    page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: !/^[4-7]/.test(nombre) });
const slot = (page: Page, nombre: string) => page.locator(`[data-slot="${nombre}"]`);

test.describe("Centralita: administración", () => {
    test("conectar: probar, comparar la huella, guardar y queda encendida", async ({ page }) => {
        const api = await abrir(page, RUTA);
        await expect(page.getByText("Conecta tu Issabel")).toBeVisible();
        await esperarSinDesbordes(page);
        await captura(page, "1-sin-centralita");
        await page.getByRole("link", { name: "Conectar centralita" }).click();
        await expect(page.getByRole("heading", { name: "Conexión con Issabel" })).toBeVisible();
        await page.getByLabel("Nombre").fill("Oficina central");
        await page.getByLabel("Dirección").fill("10.20.0.5");
        await page.getByLabel("Usuario").fill("iris_lectura");
        await page.getByLabel("Contraseña", { exact: true }).fill("una-contrasena-larga");
        // Sin probar no se guarda.
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await page.getByRole("button", { name: "Probar conexión" }).click();
        await expect(slot(page, "resultado-prueba")).toHaveAttribute("data-estado", "ok");
        await expect(slot(page, "huella")).toContainText("4F:9A:21:C0");
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await page.getByText("Es la misma que muestra mi Issabel").click();
        await esperarSinDesbordes(page);
        await captura(page, "2-conexion-probada");
        await page.getByRole("button", { name: "Guardar" }).click();
        await expect(page.getByText("Centralita guardada y encendida")).toBeVisible();
        await expect(page).toHaveURL(new RegExp(`${RUTA}$`));
        await expect(slot(page, "estado-centralita")).toHaveAttribute("data-estado", "conectada");
        expect(api.centralita.recibido.creadas).toHaveLength(1);
        expect(api.centralita.recibido.creadas[0]).toMatchObject({ host: "10.20.0.5", usuario: "iris_lectura", secreto: "una-contrasena-larga" });
        expect(api.centralita.recibido.activaciones).toBe(1);
        // Cambiar un dato después de probar obliga a probar otra vez.
    });

    test("un usuario con permisos de escritura no se puede guardar", async ({ page }) => {
        await abrir(page, `${RUTA}/conexion`, { prueba: "permisos_de_mas" });
        await page.getByLabel("Nombre").fill("Oficina");
        await page.getByLabel("Dirección").fill("10.20.0.5");
        await page.getByLabel("Usuario").fill("admin");
        await page.getByLabel("Contraseña", { exact: true }).fill("una-contrasena-larga");
        await page.getByRole("button", { name: "Probar conexión" }).click();
        await expect(slot(page, "resultado-prueba")).toContainText("Este usuario puede hacer cambios");
        await expect(slot(page, "resultado-prueba")).toContainText("Originate");
        await expect(page.getByRole("button", { name: "Guardar" })).toBeDisabled();
        await captura(page, "2b-permisos-de-mas");
    });

    test("conectada: estado, extensiones, asignar y quitar", async ({ page }) => {
        const api = await abrir(page, RUTA, { conectada: true, extensiones: { 14: "205" } });
        await expect(slot(page, "estado-centralita")).toHaveAttribute("data-estado", "conectada");
        await expect(slot(page, "extensiones-centralita")).toContainText("Rosa Pop");
        await esperarSinDesbordes(page);
        await captura(page, "3-conectada");
        await page.getByRole("button", { name: "Asignar extensión" }).click();
        const dialogo = page.getByRole("dialog", { name: "Asignar extensión" });
        await elegirOpcion(dialogo.getByLabel("Persona"), "Luis Chávez");
        await dialogo.getByLabel("Extensión").fill("206");
        await dialogo.getByRole("button", { name: "Asignar" }).click();
        await expect(page.getByText("Extensión 206 asignada a Luis Chávez")).toBeVisible();
        await expect(slot(page, "extensiones-centralita")).toContainText("206");
        await page.getByRole("button", { name: "Quitar la extensión 205 de Rosa Pop" }).click();
        await page.getByRole("alertdialog").getByRole("button", { name: "Quitar" }).click();
        await expect(slot(page, "extensiones-centralita")).not.toContainText("Rosa Pop");
        expect(api.centralita.recibido.asignaciones).toEqual([{ usuarioId: 15, extension: "206" }]);
        expect(api.centralita.recibido.quitadas).toBe(1);
    });

    test("sin conexión y certificado cambiado: lo dice en llano y se confirma la huella", async ({ page }) => {
        await abrir(page, RUTA, { conectada: true, estado: "desconectada" });
        await expect(slot(page, "aviso-centralita-estado")).toContainText("las llamadas de ese rato no quedan en Iris");
        await captura(page, "3b-sin-conexion");
        await page.unrouteAll({ behavior: "ignoreErrors" });
        await abrir(page, RUTA, { conectada: true, estado: "huella_cambiada" });
        await expect(slot(page, "aviso-centralita-estado")).toContainText("El certificado de la centralita cambió");
        await captura(page, "3c-huella-cambiada");
        await page.getByRole("button", { name: "Comparar y confirmar" }).click();
        await expect(slot(page, "huella-nueva")).toContainText(HUELLA_NUEVA.slice(0, 2).toUpperCase());
        await expect(page.getByRole("button", { name: "Confirmar y conectar" })).toBeDisabled();
        await page.getByText("Es la nueva huella de mi Issabel").click();
        await page.getByRole("button", { name: "Confirmar y conectar" }).click();
        await expect(slot(page, "estado-centralita")).toHaveAttribute("data-estado", "conectada");
    });
});

test.describe("Centralita: cuando suena tu extensión", () => {
    test("cliente reconocido: el mismo aviso de llamada, sin «Contestar», y agregar al ticket", async ({ page }) => {
        const api = await abrir(page, "/tickets", { conectada: true, extensiones: { 1: "204" } });
        await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
        // El panel abrió el flujo de la bandeja (con su extensión): lo que suene desde aquí le llega.
        await expect.poll(() => api.flujoAbierto(), { timeout: 15_000 }).toBe(true);
        api.centralita.sonar(llamadaCentralita());
        const aviso = slot(page, "aviso-centralita");
        await expect(aviso).toContainText("María Fernanda Pérez López");
        await expect(aviso).toContainText("Suena tu extensión 204");
        await expect(aviso.locator('[data-slot="servicio-llamada"]')).toContainText("Fibra 50 Mbps");
        await expect(aviso.getByRole("button", { name: "Contestar" })).toHaveCount(0);
        await esperarSinDesbordes(page, '[data-slot="aviso-centralita"]');
        await captura(page, "4-suena-reconocido");
        await aviso.getByRole("button", { name: "Agregar a #4812" }).click();
        await expect(page.getByText("Llamada agregada al ticket #4812")).toBeVisible();
        expect(api.centralita.recibido.vinculos).toEqual([{ ticketId: 4812 }]);
        // Contestó: en llamada; al colgar, «¿Cómo quedó?» y la nota.
        const base = llamadaCentralita({ ticketId: 4812 });
        api.centralita.actualizar({ ...base, estado: "en_curso", contestadaEn: new Date().toISOString(), usuario: { id: 1, nombre: "Ana" } });
        await expect(aviso).toContainText("En llamada", { timeout: 15_000 });
        api.centralita.actualizar({ ...base, estado: "terminada", duracionSeg: 240, usuario: { id: 1, nombre: "Ana" } }, "llamada_terminada");
        const nota = slot(page, "nota-centralita");
        await expect(nota).toBeVisible({ timeout: 15_000 });
        await captura(page, "4b-al-colgar");
        await nota.getByRole("radio", { name: "Resuelto" }).check({ force: true });
        await nota.getByLabel("Nota (opcional)").fill("Se reinició la ONT");
        await page.getByRole("dialog").getByRole("button", { name: "Guardar", exact: true }).click();
        await expect(page.getByText("Nota guardada")).toBeVisible();
        expect(api.centralita.recibido.anotaciones[0]).toMatchObject({ resultado: "resuelto", nota: "Se reinició la ONT" });
    });

    test("varios posibles: elegir quién llama", async ({ page }) => {
        const api = await abrir(page, "/tickets", { conectada: true, extensiones: { 1: "204" } });
        await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
        // El panel abrió el flujo de la bandeja (con su extensión): lo que suene desde aquí le llega.
        await expect.poll(() => api.flujoAbierto(), { timeout: 15_000 }).toBe(true);
        api.centralita.sonar(
            llamadaCentralita({
                titulo: "+50222451100",
                numero: "+50222451100",
                contacto: { tipo: "desconocido" },
                senales: undefined,
                centralita: {
                    id: 7, tuExtension: "204", receptores: [{ usuarioId: 1, nombre: "Ana", extension: "204" }], coincidencias: 2, asociar: true,
                    candidatos: [{ clienteId: 51, nombre: "Comercial El Roble, S.A.", codigo: "C-51" }, { clienteId: 52, nombre: "Roberto Gómez", codigo: "C-52" }],
                },
            }),
        );
        const aviso = slot(page, "aviso-centralita");
        await expect(aviso.getByRole("radiogroup", { name: "¿Quién llama?" })).toBeVisible();
        await esperarSinDesbordes(page, '[data-slot="aviso-centralita"]');
        await captura(page, "5-suena-varios");
        await aviso.getByRole("radio", { name: "Comercial El Roble, S.A." }).check({ force: true });
        await aviso.getByRole("button", { name: "Es este" }).click();
        await expect(page.getByText("La llamada quedó en el cliente")).toBeVisible();
        expect(api.centralita.recibido.asociaciones).toEqual([{ clienteId: 51 }]);
    });

    test("número desconocido: buscar o crear ticket con la llamada", async ({ page }) => {
        const api = await abrir(page, "/tickets", { conectada: true, extensiones: { 1: "204" } });
        await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
        // El panel abrió el flujo de la bandeja (con su extensión): lo que suene desde aquí le llega.
        await expect.poll(() => api.flujoAbierto(), { timeout: 15_000 }).toBe(true);
        api.centralita.sonar(llamadaCentralita({ titulo: "+50244107788", numero: "+50244107788", contacto: { tipo: "desconocido" }, senales: undefined,
            centralita: { id: 7, tuExtension: "204", receptores: [], coincidencias: 0, candidatos: [], asociar: true } }));
        const aviso = slot(page, "aviso-centralita");
        await expect(aviso).toContainText("Número desconocido");
        await expect(aviso.getByRole("button", { name: "Buscar cliente" })).toBeVisible();
        await esperarSinDesbordes(page, '[data-slot="aviso-centralita"]');
        await captura(page, "6-suena-desconocido");
        await aviso.getByRole("button", { name: "Crear ticket" }).click();
        await expect(page).toHaveURL(/\/tickets\/nuevo\?llamada=/);
        await expect(slot(page, "ticket-desde-llamada")).toBeVisible();
    });

    test("contestó otra persona: lo dice y se cierra sola", async ({ page }) => {
        const api = await abrir(page, "/tickets", { conectada: true, extensiones: { 1: "204" } });
        await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
        // El panel abrió el flujo de la bandeja (con su extensión): lo que suene desde aquí le llega.
        await expect.poll(() => api.flujoAbierto(), { timeout: 15_000 }).toBe(true);
        const l = llamadaCentralita();
        api.centralita.sonar(l);
        await expect(slot(page, "aviso-centralita")).toBeVisible();
        const ext = { id: 7, tuExtension: "204", receptores: [], coincidencias: 1, candidatos: [], asociar: true, extension: "205" };
        api.centralita.actualizar({ ...l, estado: "en_curso", usuario: { id: 15, nombre: "Luis" }, centralita: ext });
        await expect(slot(page, "aviso-centralita")).toContainText("Contestó Luis (ext. 205)", { timeout: 15_000 });
        await captura(page, "7-contesto-otro");
    });

    test("sin extensión no se escucha nada", async ({ page }) => {
        const api = await abrir(page, "/tickets", { conectada: true, extensiones: { 14: "205" } });
        await expect(page.getByRole("heading", { name: "Tickets" })).toBeVisible();
        // Sin extensión el panel no abre el flujo de la centralita.
        await page.waitForTimeout(1000);
        expect(api.flujoAbierto()).toBe(false);
        api.centralita.sonar(llamadaCentralita());
        await page.waitForTimeout(1500);
        await expect(slot(page, "aviso-centralita")).toHaveCount(0);
    });
});
