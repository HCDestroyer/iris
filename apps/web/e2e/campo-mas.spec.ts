import path from "node:path";
import { expect, test, type Page } from "@playwright/test";
import type { Schema } from "@iris/contratos";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { avisoCampo } from "./ayudas-campo";
import { ordenesDelDia, PERMISOS_TECNICO } from "./datos-campo";

/*
 * Botón «+» de la app de campo (diseño aprobado por el PM, nov 2026): la hoja con «Agendar tarea
 * propia» y «Tomar una orden libre» (con su contador; oculta si la empresa no lo permite), la
 * pantalla «Nueva tarea» (también sin señal), la tarea en Hoy con borde punteado y «Hecha», y
 * «Órdenes libres» con «Tomarla», la confirmación y el aviso si otro la tomó primero.
 * Con IRIS_CAPTURAS=<carpeta> deja las capturas para el PM.
 */

const carpeta =
    process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");

test.beforeEach(async ({ context, baseURL }) => {
    await conSesionTecnico(context, baseURL ?? "");
});

const isla = (page: Page) =>
    page.getByRole("navigation", { name: "Secciones de la app" });

async function captura(page: Page, que: string, proyecto: string) {
    await page.screenshot({
        path: path.join(carpeta, `campo-mas-${que}-${proyecto}.png`),
    });
}

/** Día de hoy en Guatemala (`AAAA-MM-DD`). */
function hoyGuatemala(): string {
    return new Intl.DateTimeFormat("en-CA", {
        timeZone: "America/Guatemala",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
    }).format(Date.now());
}

/** Las órdenes de prueba con su hora en el día de Guatemala (no el UTC). */
function ordenesDeHoyGuatemala() {
    const hoy = hoyGuatemala();
    const enGuatemala = (iso: string | null | undefined) =>
        iso ? `${hoy}T${iso.slice(11)}` : iso;
    return ordenesDelDia().map((o) => ({
        ...o,
        programadaInicio: enGuatemala(o.programadaInicio) ?? null,
        programadaFin: enGuatemala(o.programadaFin) ?? null,
    }));
}

function libres(): Schema<"OrdenLibre">[] {
    const [base] = ordenesDelDia();
    if (!base) {
        throw new Error("sin órdenes de prueba");
    }
    const libre = (
        id: number,
        nombre: string,
        tipo: Schema<"OrdenLibre">["tipo"],
        distanciaM: number,
        programadaInicio: string | null,
    ): Schema<"OrdenLibre"> => ({
        ...structuredClone(base),
        id,
        numero: `OT-000${id}`,
        tipo,
        estado: "pendiente",
        tecnicoId: null,
        programadaInicio,
        programadaFin: null,
        clienteNombre: nombre,
        distanciaM,
    });
    const hoy = new Intl.DateTimeFormat("en-CA", {
        timeZone: "America/Guatemala",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
    }).format(Date.now());
    return [
        libre(120, "Luis Herrera", "visita", 1200, `${hoy}T22:00:00Z`),
        libre(118, "Carmen Ruiz", "instalacion", 2800, null),
        libre(115, "Pedro Gómez", "cambio_equipo", 4100, null),
    ];
}

test("«+»: tarea propia y orden libre con su contador; sin el ajuste, solo la tarea", async ({
    page,
}, info) => {
    await simularApiCampo(page, { tomarLibres: true, libres: libres() });
    await page.goto("/campo");
    await isla(page).getByRole("button", { name: "Acciones rápidas" }).click();
    const hoja = page.getByRole("dialog", { name: "¿Qué quieres hacer?" });
    await expect(
        hoja.getByRole("button", { name: "Agendar tarea propia" }),
    ).toBeVisible();
    await expect(
        hoja.getByRole("button", { name: "Tomar una orden libre (3)" }),
    ).toBeVisible();
    // «Agregar formulario» ya no está en el «+» (vive en el menú «⋯» de la orden).
    await expect(hoja.getByText("Agregar formulario")).toHaveCount(0);
    await captura(page, "1-mas", info.project.name);
});

test("«+» con el ajuste apagado o sin ordenes.tomar: solo «Agendar tarea propia»", async ({
    page,
}) => {
    await simularApiCampo(page, { tomarLibres: false });
    await page.goto("/campo");
    await isla(page).getByRole("button", { name: "Acciones rápidas" }).click();
    const hoja = page.getByRole("dialog", { name: "¿Qué quieres hacer?" });
    await expect(
        hoja.getByRole("button", { name: "Agendar tarea propia" }),
    ).toBeVisible();
    await expect(hoja.locator('[data-slot="accion-libres"]')).toHaveCount(0);
});

test("Nueva tarea sin señal: aparece en Hoy con borde punteado, se envía al volver y se marca hecha", async ({
    page,
    context,
}, info) => {
    // Las órdenes de prueba van por día UTC: aquí, por día de Guatemala (08:30, 11:00 y 13:00),
    // para que la tarea de las 12:00 caiga entre las dos de «Después».
    const api = await simularApiCampo(page, {
        permisos: PERMISOS_TECNICO,
        ordenes: ordenesDeHoyGuatemala(),
    });
    await page.goto("/campo");
    await expect(
        page.getByRole("heading", { level: 1, name: "Hola, Tomás" }),
    ).toBeVisible();
    await isla(page).getByRole("button", { name: "Acciones rápidas" }).click();
    await page
        .getByRole("dialog", { name: "¿Qué quieres hacer?" })
        .getByRole("button", { name: "Agendar tarea propia" })
        .click();
    await expect(page).toHaveURL(/vista=nueva_tarea$/);
    await expect(
        page.getByRole("heading", { name: "Nueva tarea" }),
    ).toBeVisible();
    // Sin «qué», el formulario avisa (espejo de la API).
    await page.getByRole("button", { name: "Agendar" }).click();
    await expect(page.getByText("Escribe qué vas a hacer")).toBeVisible();
    await page
        .getByLabel("Qué vas a hacer")
        .fill("Recoger material en bodega central");
    // Hora de 24 h en segmentos (es-GT): hora y minuto (`CampoHora`).
    const hora = page.locator('[data-slot="campo-hora"]');
    await expect(hora).toContainText(/^Hora/);
    await hora.getByRole("spinbutton").first().click();
    await page.keyboard.type("1200");
    await expect(
        page
            .locator('[data-slot="selector-fecha"]')
            .getByRole("spinbutton")
            .first(),
    ).toBeVisible();
    await page.getByRole("radio", { name: "30 min" }).click();
    await expect(page.getByRole("radio", { name: "30 min" })).toHaveAttribute(
        "aria-checked",
        "true",
    );
    await captura(page, "2-nueva-tarea", info.project.name);

    api.sinRed = true;
    await context.setOffline(true);
    await page.getByRole("button", { name: "Agendar" }).click();
    await expect(avisoCampo(page)).toContainText("Tarea agendada");
    await expect(page).toHaveURL(/\/campo$/);
    const tarjeta = page.locator('[data-slot="tarea-propia"]');
    await expect(tarjeta).toContainText("Recoger material en bodega central");
    await expect(tarjeta).toContainText("Tarea propia · 30 min");
    await expect(tarjeta).toContainText("12:00");
    // Intercalada estrictamente por hora: 11:00 · tarea 12:00 · 13:00.
    await expect(
        page.locator('ol[aria-labelledby="titulo-despues"] > li'),
    ).toHaveText([/11:00/, /12:00.*Recoger material/, /13:00/]);
    // La tarjeta a media pantalla (encima de la isla) para la captura.
    await tarjeta.evaluate((el) => el.scrollIntoView({ block: "center" }));
    await captura(page, "3-hoy-con-tarea", info.project.name);

    api.sinRed = false;
    await context.setOffline(false);
    await expect
        .poll(
            () =>
                api.lotes
                    .flatMap((l) => l.operaciones ?? [])
                    .map((o) => o.tipo),
            { timeout: 20_000 },
        )
        .toContain("tarea.crear");
    const [crear] = api.lotes
        .flatMap((l) => l.operaciones ?? [])
        .filter((o) => o.tipo === "tarea.crear");
    expect(crear).toMatchObject({
        datos: { que: "Recoger material en bodega central", duracionMin: 30 },
    });
    expect(crear).not.toHaveProperty("ordenId");

    await tarjeta.getByRole("button", { name: "Hecha" }).click();
    await expect(avisoCampo(page)).toContainText("Tarea hecha");
    await expect
        .poll(
            () =>
                api.lotes
                    .flatMap((l) => l.operaciones ?? [])
                    .map((o) => o.tipo),
            { timeout: 20_000 },
        )
        .toContain("tarea.completar");
    await expect(tarjeta).toContainText("Hecha · 30 min");
});

test("Órdenes libres: tomarla con confirmación y aviso; si otro la tomó, se dice quién y sale de la lista", async ({
    page,
}, info) => {
    const lista = libres().map((o) =>
        o.id === 118 ? { ...o, tomadaPor: "Andrea López" } : o,
    );
    const api = await simularApiCampo(page, {
        tomarLibres: true,
        libres: lista,
    });
    await page.goto("/campo?vista=libres");
    await expect(
        page.getByRole("heading", { name: "Órdenes libres" }),
    ).toBeVisible();
    await expect(
        page.getByText(
            "Zona Norte · sin técnico asignado · más cercanas primero",
        ),
    ).toBeVisible();
    const filas = page.locator('[data-slot="orden-libre"]');
    await expect(filas).toHaveCount(3);
    await expect(filas.first()).toContainText("Luis Herrera");
    await expect(filas.first()).toContainText("1,2 km");
    await captura(page, "4-ordenes-libres", info.project.name);

    await filas.first().getByRole("button", { name: "Tomarla" }).click();
    const confirmar = page.getByRole("dialog", {
        name: "¿Tomar OT-000120?",
    });
    await expect(confirmar).toContainText(
        "Queda a tu nombre y despacho recibe el aviso.",
    );
    await captura(page, "5-confirmar", info.project.name);
    await confirmar.getByRole("button", { name: "Tomarla" }).click();
    await expect(avisoCampo(page)).toContainText("OT-000120 quedó a tu nombre");
    expect(api.tomadas).toEqual([120]);
    await expect(filas).toHaveCount(2);

    // Otro técnico tomó OT-000118 primero: 409 con su nombre.
    await filas.first().getByRole("button", { name: "Tomarla" }).click();
    await page
        .getByRole("dialog", { name: "¿Tomar OT-000118?" })
        .getByRole("button", { name: "Tomarla" })
        .click();
    await expect(page.locator('[data-slot="ya-tomada"]')).toHaveText(
        "OT-000118 ya la tomó Andrea López. La quitamos de la lista.",
    );
    await expect(filas).toHaveCount(1);
    await captura(page, "6-ya-tomada", info.project.name);
});
