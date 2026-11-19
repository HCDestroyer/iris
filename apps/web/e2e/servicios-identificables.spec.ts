import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, CONSULTA, conSesion, simularApi } from "./api-falsa";
import { esTelefono } from "./ayudas";
import { AHORA_CONV, CONVERSACIONES, detalle } from "./datos-conversaciones";
import { buscarDesbordes } from "./desbordes";
import { recortarServicio } from "./recorte-servicio";

/*
 * Servicios identificables (contrato con Argos 0.8.0 §2.4; pedido del PM: «solo muestra el nombre y
 * no el ip, precio, ubicación u router»). La línea de servicio en la ficha de la conversación (con
 * uno y con varios servicios), el selector al crear una orden y lo que ve un rol sin
 * `servicios.ver_red` ni `servicios.ver_ubicacion`. En los 4 proyectos y sin desbordes.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas-servicios");
const captura = (page: Page, nombre: string, completa = true) => page.screenshot({ path: path.join(carpeta, `${nombre}-${test.info().project.name}.png`), fullPage: completa });

/** Nada de `elemento` queda debajo de la barra de guardar (fija al pie). */
async function noTapado(page: Page, elemento: ReturnType<Page["locator"]>, que: string) {
    const barra = await page.locator('[data-slot="barra-guardar"]').boundingBox();
    const caja = await elemento.boundingBox();
    expect(barra && caja, que).toBeTruthy();
    if (barra && caja) expect(caja.y + caja.height, `${que}: no queda debajo de la barra de guardar`).toBeLessThanOrEqual(barra.y + 1);
}

type ServicioConv = Schema<"ServicioResumenConversacion">;

/** La hoja del teléfono entra deslizándose: se espera a que se asiente. */
const sinDesbordes = (page: Page) => expect.poll(() => buscarDesbordes(page), "nada se desborda ni queda fuera de pantalla").toEqual([]);

const base = { empresaId: 1, clienteId: 4102, zonaId: 7, sincronizadoEn: "2026-10-28T16:40:00Z" };
const linea = (s: Omit<ServicioConv, "empresaId" | "clienteId" | "zonaId" | "sincronizadoEn" | "plan" | "zona">): ServicioConv => ({ ...base, ...s, plan: s.planNombre, zona: s.zonaNombre });

const TRES: ServicioConv[] = [
    linea({ id: 41019, numero: 1, codigo: "S-4410", planId: 12, planNombre: "Chitá Fibra 50 Mbps residencial", zonaNombre: "Chitá", estado: "activo", precioMensual: "250.00", moneda: "GTQ", routerNombre: "Nodo Centro", ip: "100.64.12.34", direccion: "3a calle 4-56 zona 1, Chitá" }),
    linea({ id: 41020, numero: 2, codigo: "S-4518", planId: 13, planNombre: "Chitá Fibra 100 Mbps", zonaNombre: "Chitá", estado: "suspendido", precioMensual: "350.00", moneda: "GTQ", routerNombre: "Nodo Centro", ip: "100.64.12.90", direccion: "Lote 12, colonia Las Flores" }),
    // Sin precio (Argos anterior a 1.18.0) y sin IP todavía.
    linea({ id: 41021, numero: 3, codigo: "S-4702", planId: 20, planNombre: "Negocio 30 Mbps", zonaNombre: "San Pedro", estado: "pendiente_instalacion", routerNombre: "Torre San Pedro", direccion: "Km 18 carretera a San Pedro, local 4" }),
];

const CONV = CONVERSACIONES.find((c) => c.id === 42);

/** Abre la conversación 42 (María López) con los servicios dados, recortados como el servidor para ese usuario. */
async function abrir(page: Page, servicios: ServicioConv[], usuario = ADMIN, permisos: string[] = ["servicios.ver_red", "servicios.ver_ubicacion"], ruta = "/conversaciones?c=42") {
    if (!CONV) throw new Error("falta la conversación 42");
    await page.clock.setFixedTime(new Date(AHORA_CONV));
    const api = await simularApi(page, { enlaces: { argosisp: { conectorId: 1, urlPublica: "https://argos.example/" } } });
    const d = detalle(CONV);
    const resumen = d.clienteResumen;
    if (!resumen) throw new Error("la conversación 42 no tiene cliente");
    const puede = (p: string) => permisos.includes(p);
    await page.route("**/api/v1/mensajeria/conversaciones/42", (r) =>
        r.request().method() === "GET"
            ? r.fulfill({ json: { ...d, clienteResumen: { ...resumen, servicios: servicios.map((s) => recortarServicio(s, puede)) } } })
            : r.fallback(),
    );
    await conSesion(page.context(), test.info().project.use.baseURL ?? "", usuario.correo);
    await page.goto(ruta);
    return api;
}

async function ficha(page: Page) {
    if (!esTelefono(page)) return page.getByRole("complementary", { name: "Ficha del cliente" });
    await page.getByRole("button", { name: "Ver la ficha del cliente" }).click();
    return page.getByRole("dialog", { name: "Ficha del cliente" });
}

test.describe("servicios identificables", () => {
    test("con un servicio: plan entero, estado, precio, zona, router, IP y dirección", async ({ page }) => {
        await abrir(page, [{ ...TRES[1], numero: 1 } as ServicioConv]);
        const f = await ficha(page);
        const l = f.getByTestId("linea-servicio");
        await expect(l).toHaveCount(1);
        await expect(l).toContainText("Chitá Fibra 100 Mbps");
        await expect(l).toContainText("Suspendido");
        await expect(l).toContainText("Q350.00 al mes");
        await expect(l).toContainText("Nodo Centro");
        await expect(l).toContainText("100.64.12.90");
        await expect(l).toContainText("Lote 12, colonia Las Flores");
        // Un solo servicio: sin número ni marca, y el plan no se recorta.
        await expect(f.getByTestId("linea-servicio-marca")).toHaveCount(0);
        const plan = l.getByTestId("linea-servicio-plan");
        expect(await plan.evaluate((e) => e.scrollWidth <= e.clientWidth + 1)).toBe(true);
        await expect(f.getByTestId("nota-permisos-servicio")).toHaveCount(0);
        await sinDesbordes(page);
        await captura(page, "ficha-1-servicio");
    });

    test("con tres servicios: los lista todos, numerados, y marca el de la conversación", async ({ page }) => {
        await abrir(page, TRES);
        const f = await ficha(page);
        const lineas = f.getByTestId("lista-servicios").getByTestId("linea-servicio");
        await expect(lineas).toHaveCount(3);
        await expect(f.getByText("Servicios (3)")).toBeVisible();
        await expect(lineas.nth(0)).toContainText("Chitá Fibra 50 Mbps residencial");
        await expect(lineas.nth(0)).toContainText("Q250.00 al mes");
        // El de la conversación (servicioId 41020) lleva la marca.
        await expect(f.getByTestId("linea-servicio-marca")).toHaveCount(1);
        await expect(lineas.nth(1)).toContainText("De esta conversación");
        await expect(lineas.nth(1).getByLabel("Servicio 2")).toBeVisible();
        // Sin precio en Argos: ningún monto inventado.
        await expect(lineas.nth(2)).not.toContainText("Q");
        await expect(lineas.nth(2)).toContainText("Pendiente de instalación");
        await sinDesbordes(page);
        await captura(page, "ficha-3-servicios");
    });

    test("al crear una orden se elige el servicio por su línea completa", async ({ page }) => {
        const api = await abrir(page, TRES, ADMIN, ["servicios.ver_red", "servicios.ver_ubicacion"], "/conversaciones/42/orden");
        const grupo = page.getByRole("radiogroup", { name: "¿En cuál servicio?" });
        await expect(grupo.getByRole("radio")).toHaveCount(3);
        // Viene elegido el de la conversación.
        await expect(grupo.getByRole("radio", { name: /Servicio 2 · Chitá Fibra 100 Mbps · Chitá/ })).toBeChecked();
        await expect(grupo).toContainText("100.64.12.34");
        await expect(grupo).toContainText("Km 18 carretera a San Pedro");
        await sinDesbordes(page);
        // Viewport real (sin página completa): el selector arriba, la barra fija al pie.
        await grupo.evaluate((e) => {
            e.scrollIntoView({ block: "start" });
            window.scrollBy(0, -96);
        });
        await captura(page, "orden-selector", false);
        // La tarjeta elegida, centrada en la pantalla, no queda tapada por la barra.
        const elegida = grupo.locator('[data-testid="linea-servicio"][data-servicio="41020"]');
        await elegida.evaluate((e) => e.scrollIntoView({ block: "center" }));
        await noTapado(page, elegida, "la tarjeta elegida");
        // Al final del scroll, el último campo queda por encima de la barra (hay espacio al pie).
        await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
        await noTapado(page, page.getByRole("combobox", { name: "Zona (opcional)" }).or(page.getByLabel("Zona (opcional)")).first(), "el último campo");
        await captura(page, "orden-final", false);
        await grupo.getByRole("radio", { name: /Servicio 3 · Negocio 30 Mbps/ }).click();
        await expect(grupo.getByRole("radio", { name: /Servicio 3 · Negocio 30 Mbps/ })).toBeChecked();
        await page.getByRole("button", { name: "Crear orden" }).click();
        await expect.poll(() => api.conversaciones.enviados.find((e) => e.ruta.endsWith("/crear-orden"))?.cuerpo).toMatchObject({ servicioId: 41021 });
    });

    test("sin ver_red ni ver_ubicacion: ni IP, ni router, ni dirección (los omite el servidor) y lo dice", async ({ page }) => {
        await abrir(page, TRES, CONSULTA, []);
        const f = await ficha(page);
        const lista = f.getByTestId("lista-servicios");
        await expect(lista.getByTestId("linea-servicio")).toHaveCount(3);
        await expect(lista).toContainText("Q250.00 al mes");
        await expect(lista).toContainText("Chitá Fibra 100 Mbps");
        await expect(lista.getByTestId("linea-servicio-ip")).toHaveCount(0);
        await expect(lista.getByTestId("linea-servicio-router")).toHaveCount(0);
        await expect(lista.getByTestId("linea-servicio-direccion")).toHaveCount(0);
        await expect(lista).not.toContainText("100.64");
        await expect(f.getByTestId("nota-permisos-servicio")).toContainText("Tu rol no ve la dirección ni los datos de red del servicio.");
        await sinDesbordes(page);
        await captura(page, "sin-permisos");
    });
});
