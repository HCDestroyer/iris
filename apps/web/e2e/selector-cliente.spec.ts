import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, EMPRESAS, conSesion, simularApi } from "./api-falsa";

/*
 * Pedido del PM (4) y (9): selector de cliente único (búsqueda mientras se escribe, servicios con
 * estado, empresa, recientes, teclado, IP, vacío y error con «Reintentar», hoja a pantalla
 * completa en el teléfono, pide el servicio) y modales que avisan antes de perder cambios.
 */

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

async function abrirNuevoTicket(page: Page) {
    await page.goto("/tickets");
    await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
    await page.getByRole("button", { name: "Nuevo ticket" }).click();
    return page.getByRole("dialog", { name: "Nuevo ticket" });
}

test("buscar, ver servicios y empresa, elegir el servicio con el teclado y crear el ticket", async ({ page, context, baseURL }, info) => {
    const nombre = (que: string) => path.join(carpeta, `selector-${que}-${info.project.name}.png`);
    const api = await simularApi(page, { empresas: EMPRESAS });
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    const dialogo = await abrirNuevoTicket(page);
    await dialogo.getByLabel("Asunto").fill("Lentitud en la bodega");

    await dialogo.getByLabel("Cliente en Argos").click();
    const hoja = page.getByTestId("dialogo-cliente");
    await expect(hoja.getByText("Escribe al menos 2 caracteres")).toBeVisible();
    if (info.project.name.startsWith("telefono")) {
        // En el teléfono: hoja a pantalla completa.
        const caja = await hoja.boundingBox();
        const vista = page.viewportSize();
        expect(caja && vista && Math.round(caja.height) >= vista.height - 1 && Math.round(caja.width) >= vista.width - 1).toBe(true);
    }
    const buscar = page.getByRole("combobox", { name: "Buscar cliente" });
    await expect(buscar).toBeFocused();
    await buscar.fill("Ana");
    const opcion = page.getByRole("option", { name: /Ana Morales/ });
    await expect(opcion).toContainText("CL-0503");
    await expect(opcion).toContainText("6a avenida 12-40, zona 10");
    await expect(opcion).toContainText("5555 0303");
    await expect(opcion).toContainText("Cable Norte");
    await expect(opcion).toContainText("Fibra 100 Mb/s · Activo");
    await expect(opcion).toContainText("Radio 10 Mb/s · Suspendido");
    await expect(opcion).toHaveAttribute("aria-selected", "true");
    await page.screenshot({ path: nombre("01-resultados") });

    // Enter elige el cliente; con dos servicios pide cuál.
    await buscar.press("Enter");
    const servicios = page.getByRole("listbox", { name: "Servicios del cliente" });
    await expect(servicios).toBeFocused();
    await expect(servicios.getByRole("option")).toHaveCount(2);
    await page.screenshot({ path: nombre("02-servicio") });
    await servicios.press("ArrowDown");
    await servicios.press("Enter");

    await expect(hoja).toHaveCount(0);
    await expect(dialogo.getByTestId("cliente-elegido")).toContainText("Ana Morales");
    await expect(dialogo.getByTestId("servicio-elegido")).toContainText("SV-503-B");
    await expect(dialogo.getByTestId("servicio-elegido")).toContainText("Suspendido");
    await page.screenshot({ path: nombre("03-elegido") });
    await dialogo.getByRole("button", { name: "Crear ticket" }).click();
    await expect(page).toHaveURL(/\/tickets\/1000$/);
    const alta = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/tickets");
    expect(alta?.cuerpo).toEqual({ asunto: "Lentitud en la bodega", prioridad: "media", clienteId: 503, servicioId: 5031 });
    expect(api.peticiones.some((p) => p.ruta === "/api/v1/clientes" && p.query.get("servicios") === "true")).toBe(true);

    // Recientes: al volver a abrir aparece el último elegido.
    await page.goto("/tickets");
    await page.getByRole("button", { name: "Nuevo ticket" }).click();
    await page.getByRole("dialog", { name: "Nuevo ticket" }).getByLabel("Cliente en Argos").click();
    const recientes = page.getByRole("listbox", { name: "Clientes recientes" });
    await expect(recientes.getByRole("option", { name: /Ana Morales/ })).toBeVisible();
});

test("por IP elige el servicio de esa IP; vacío y error con «Reintentar»", async ({ page, context, baseURL }) => {
    await simularApi(page, { empresas: EMPRESAS });
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    const dialogo = await abrirNuevoTicket(page);
    await dialogo.getByLabel("Cliente en Argos").click();
    const buscar = page.getByRole("combobox", { name: "Buscar cliente" });

    await buscar.fill("zzz");
    await expect(page.getByText("Ningún cliente coincide con «zzz»")).toBeVisible();

    await buscar.fill("falla");
    const aviso = page.getByRole("alert").filter({ hasText: "No se pudieron leer los clientes" });
    await expect(aviso).toBeVisible();
    await aviso.getByRole("button", { name: "Reintentar" }).click();
    await expect(page.getByText("Ningún cliente coincide con «falla»")).toBeVisible();

    await buscar.fill("10.20.31.9");
    await expect(page.getByRole("option", { name: /Ana Morales/ })).toBeVisible();
    await buscar.press("Enter");
    await expect(dialogo.getByTestId("servicio-elegido")).toContainText("SV-503-B");
});

test("cerrar un modal con cambios pide confirmación", async ({ page, context, baseURL }) => {
    await simularApi(page, { empresas: EMPRESAS });
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    const dialogo = await abrirNuevoTicket(page);
    // Sin cambios cierra directo.
    await page.keyboard.press("Escape");
    await expect(dialogo).toHaveCount(0);

    await page.getByRole("button", { name: "Nuevo ticket" }).click();
    await dialogo.getByLabel("Asunto").fill("Algo a medias");
    await dialogo.getByRole("button", { name: "Cancelar" }).click();
    const confirmar = page.getByRole("alertdialog", { name: "¿Descartar los cambios?" });
    await expect(confirmar).toBeVisible();
    await confirmar.getByRole("button", { name: "Seguir editando" }).click();
    await expect(confirmar).toHaveCount(0);
    await expect(dialogo.getByLabel("Asunto")).toHaveValue("Algo a medias");

    await page.keyboard.press("Escape");
    await confirmar.getByRole("button", { name: "Descartar cambios" }).click();
    await expect(dialogo).toHaveCount(0);
    await page.getByRole("button", { name: "Nuevo ticket" }).click();
    await expect(dialogo.getByLabel("Asunto")).toHaveValue("");
});

test("nueva orden con cliente de un solo servicio: lo toma sin preguntar", async ({ page, context, baseURL }) => {
    const api = await simularApi(page, { empresas: EMPRESAS });
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    await page.goto("/ordenes");
    await page.getByRole("list", { name: "Empresas" }).getByRole("button", { name: "Cable Norte" }).click();
    await page.getByRole("button", { name: "Nueva orden" }).click();
    const dialogo = page.getByRole("dialog", { name: "Nueva orden" });
    await expect(dialogo.getByRole("button", { name: "Crear orden" })).toBeDisabled();
    await dialogo.getByLabel("Cliente en Argos").click();
    await page.getByRole("combobox", { name: "Buscar cliente" }).fill("CL-0501");
    await page.getByRole("option", { name: /María Castillo/ }).click();
    await expect(dialogo.getByTestId("servicio-elegido")).toContainText("SV-501");
    await expect(dialogo.getByRole("button", { name: "Crear orden" })).toBeEnabled();
    await dialogo.getByRole("button", { name: "Crear orden" }).click();
    await expect(page).toHaveURL(/\/ordenes\/1000$/);
    const post = api.peticiones.find((p) => p.metodo === "POST" && p.ruta === "/api/v1/ordenes");
    expect(post?.cuerpo).toEqual({ tipo: "instalacion", clienteId: 501, servicioId: 5010 });
});
