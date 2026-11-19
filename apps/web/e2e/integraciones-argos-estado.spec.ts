import path from "node:path";
import type { Schema } from "@iris/contratos";
import { expect, test, type Page } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";
import { CONECTORES_INICIALES } from "./api-sso";

/*
 * Administración → Integraciones → Argos (contrato con Argos 0.8.0 §2.5 d): la primera
 * sincronización por recurso, la conexión con las versiones y lo que queda apagado, los datos en
 * Iris, lo que falló, las empresas y el certificado de Argos (solo informa). Errores con
 * «Reintentar»; «Reanudar» avisa y muestra lo que devolvió la API.
 */

type Estado = Schema<"EstadoConexionArgos">;
type Sincronizacion = Schema<"Sincronizacion">;
type Etapa = Schema<"EtapaSincronizacion">;

const carpeta = process.env.IRIS_CAPTURAS ?? path.join("test-results", "capturas");
const RUTA_ESTADO = "**/api/v1/integraciones/argosisp/estado";

const etapa = (recurso: Etapa["recurso"], estado: Etapa["estado"], procesados: number, extra: Partial<Etapa> = {}): Etapa => ({
    recurso,
    estado,
    procesados,
    omitidos: 0,
    total: null,
    iniciadaEn: estado === "pendiente" ? null : "2026-11-19T20:21:00Z",
    terminadaEn: estado === "completa" || estado === "con_errores" ? "2026-11-19T20:22:00Z" : null,
    error: null,
    ...extra,
});

const sincronizacion = (estado: Sincronizacion["estado"], etapas: Etapa[]): Sincronizacion => ({
    id: "019a9c3e-0000-7000-8000-000000000001",
    conectorId: 1,
    estado,
    origen: "activacion",
    excluirRetirados: true,
    contratoArgos: "0.8.0",
    iniciadaEn: "2026-11-19T20:21:00Z",
    actualizadaEn: "2026-11-19T20:24:00Z",
    terminadaEn: estado === "en_curso" ? null : "2026-11-19T20:30:00Z",
    informeEstado: "informado",
    informadoEn: "2026-11-19T20:24:10Z",
    informeDetalle: null,
    etapas,
});

const EN_CURSO = sincronizacion("en_curso", [
    etapa("empresas", "completa", 2, { total: 2 }),
    etapa("zonas", "completa", 14),
    etapa("planes", "completa", 9),
    etapa("routers", "completa", 31),
    etapa("cuentas_bancarias", "completa", 6, { total: 6 }),
    etapa("bancos", "completa", 5, { total: 5 }),
    etapa("clientes", "en_curso", 1200, { error: "Argos no respondió; se reintenta y se sigue desde donde quedó (HTTP 503)" }),
    etapa("servicios", "pendiente", 0),
]);

const CON_ERRORES = sincronizacion("con_errores", [
    etapa("empresas", "completa", 2, { total: 2 }),
    etapa("zonas", "completa", 14),
    etapa("planes", "completa", 9),
    etapa("routers", "completa", 31),
    etapa("cuentas_bancarias", "con_errores", 0, { error: "Argos anterior a 0.8.0" }),
    etapa("bancos", "con_errores", 0, { error: "Argos anterior a 0.8.0" }),
    etapa("clientes", "completa", 1530),
    etapa("servicios", "completa", 1702, { omitidos: 3 }),
]);

function estado(s: Sincronizacion | undefined, certificadoCambiado: boolean): Estado {
    const conector = CONECTORES_INICIALES()[0];
    return {
        conector: { ...conector, versionRemota: "1.18.0", contratoRemoto: "0.8.0" },
        activacion: { en: "2026-11-19T20:20:00Z", usuarioId: 1, actorNombre: ADMIN.nombre },
        contratoIris: "0.8.0",
        contratoArgos: "0.8.0",
        versionArgos: "1.18.0",
        funcionesApagadas: [{ codigo: "buscar_sin_red", desde: "Argos 1.21.0", texto: "La búsqueda de clientes todavía encuentra por usuario PPPoE a quien no puede ver los datos de red" }],
        sincronizacion: s,
        instantaneas: [
            { recurso: "clientes", cuantos: 1200, ultimoSincronizadoEn: "2026-11-19T20:24:00Z" },
            { recurso: "servicios", cuantos: 1347, ultimoSincronizadoEn: "2026-11-18T18:02:00Z" },
            { recurso: "zonas", cuantos: 14, ultimoSincronizadoEn: "2026-11-19T20:21:30Z" },
            { recurso: "routers", cuantos: 31, ultimoSincronizadoEn: "2026-11-19T20:21:40Z" },
        ],
        fallos: { webhooksConError: 0, ultimoWebhookErrorEn: null, webhooksRechazados: 0, pendientes: [{ estado: "pendiente", cuantos: 2 }] },
        empresas: [
            { id: 10, nombre: "Fibra Norte", nit: "123" },
            { id: 11, nombre: "Fibra Sur", nit: "456" },
        ],
        alcanceGlobal: true,
        certificado: {
            huellaSha256: "5f2a9c".padEnd(64, "0"),
            emisor: "CN=R11,O=Let's Encrypt,C=US",
            sujeto: "CN=argos.fibra.test",
            host: "argos.fibra.test",
            venceEn: "2027-02-17T00:00:00Z",
            vistoEn: "2026-11-19T15:12:00Z",
            ultimoVistoEn: "2026-11-19T20:24:00Z",
            cambiado: certificadoCambiado,
            anterior: certificadoCambiado ? { huellaSha256: "aa".repeat(32), emisor: "CN=R10,O=Let's Encrypt,C=US", sujeto: "CN=argos.fibra.test", ultimoVistoEn: "2026-11-19T15:00:00Z" } : undefined,
        },
    };
}

async function sinDesbordar(page: Page) {
    const ancho = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(ancho).toBeLessThanOrEqual(0);
}

test.describe("Estado de la conexión con Argos", () => {
    test("muestra la sincronización en curso sin totales inventados, la versión y el certificado que cambió", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.route(RUTA_ESTADO, (r) => r.fulfill({ json: estado(EN_CURSO, true) }));
        await page.goto("/administracion/integraciones/argos");
        await expect(page.getByRole("heading", { name: "Conexión con Argos" })).toBeVisible();
        const sinc = page.getByTestId("sincronizacion");
        await expect(sinc.getByText("En curso").first()).toBeVisible();
        // Una lista por cursor no dice cuántas hay: nunca «x de y».
        await expect(sinc.locator('[data-recurso="clientes"]')).toContainText("1,200 clientes leídos hasta ahora");
        await expect(sinc.locator('[data-recurso="servicios"]')).toContainText("Espera a los clientes");
        await expect(sinc.getByTestId("corte")).toContainText("Iris sigue solo desde donde quedó");
        await expect(sinc.getByTestId("informe")).toContainText("Avance informado a Argos");
        // En curso no se ofrece reanudar (el job sigue solo).
        await expect(sinc.getByRole("button", { name: /Reanudar|Sincronizar/ })).toHaveCount(0);
        await expect(page.getByTestId("version-argos")).toHaveText("1.18.0 · contrato 0.8.0");
        await expect(page.getByTestId("funciones-apagadas")).toContainText("usuario PPPoE");
        await expect(page.getByTestId("empresas")).toContainText("Fibra Sur");
        await expect(page.getByTestId("certificado-cambio")).toContainText("Iris no detuvo nada");
        await expect(page.getByTestId("huella")).toContainText("5F:2A:9C");
        await expect(page.getByTestId("fallos")).toContainText("2 por revisar");
        await sinDesbordar(page);
        await page.screenshot({ path: path.join(carpeta, `argos-estado-01-en-curso-${info.project.name}.png`), fullPage: true });
    });

    test("con errores: dice qué falló y «Reanudar» avisa y muestra lo que devolvió la API", async ({ page, context, baseURL }, info) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        let datos = estado(CON_ERRORES, false);
        await page.route(RUTA_ESTADO, (r) => r.fulfill({ json: datos }));
        let idempotencia: string | null = null;
        await page.route("**/api/v1/integraciones/conectores/1/sincronizar", async (r) => {
            idempotencia = r.request().headers()["idempotency-key"] ?? null;
            const reabierta: Sincronizacion = { ...CON_ERRORES, estado: "en_curso", origen: "manual", terminadaEn: null, informeEstado: "pendiente",
                etapas: (CON_ERRORES.etapas ?? []).map((e) => (e.estado === "con_errores" ? { ...e, estado: "pendiente", error: null } : e)) };
            datos = { ...datos, sincronizacion: reabierta };
            await r.fulfill({ status: 202, json: reabierta });
        });
        await page.goto("/administracion/integraciones/argos");
        const sinc = page.getByTestId("sincronizacion");
        await expect(sinc.getByText("Con errores").first()).toBeVisible();
        await expect(sinc.locator('[data-recurso="cuentas_bancarias"]')).toContainText("Argos anterior a 0.8.0");
        await expect(sinc.locator('[data-recurso="servicios"]')).toContainText("3 sin guardar");
        await expect(page.getByTestId("certificado")).not.toContainText("Cambió");
        await page.screenshot({ path: path.join(carpeta, `argos-estado-02-con-errores-${info.project.name}.png`), fullPage: true });
        await sinc.getByRole("button", { name: "Reanudar" }).click();
        await expect(page.getByText("Sincronización reanudada")).toBeVisible();
        await expect(sinc.getByText("En curso").first()).toBeVisible();
        expect(idempotencia).toBeTruthy();
        await sinDesbordar(page);
    });

    test("si no se puede leer, no muestra datos y ofrece «Reintentar»", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        let falla = true;
        await page.route(RUTA_ESTADO, (r) =>
            falla
                ? r.fulfill({ status: 503, contentType: "application/problem+json", body: JSON.stringify({ type: "urn:iris:problema:no_disponible", title: "No disponible", status: 503 }) })
                : r.fulfill({ json: estado(undefined, false) }),
        );
        await page.goto("/administracion/integraciones/argos");
        await expect(page.getByRole("button", { name: "Reintentar" })).toBeVisible();
        await expect(page.getByTestId("instantaneas")).toHaveCount(0);
        falla = false;
        await page.getByRole("button", { name: "Reintentar" }).click();
        await expect(page.getByTestId("sincronizacion")).toContainText("Sin empezar");
    });

    test("desde Integraciones se llega con «Ver estado y sincronización»", async ({ page, context, baseURL }) => {
        await simularApi(page);
        await conSesion(context, baseURL ?? "", ADMIN.correo);
        await page.route(RUTA_ESTADO, (r) => r.fulfill({ json: estado(EN_CURSO, false) }));
        await page.goto("/administracion/integraciones");
        await page.getByTestId("ver-estado-argos").click();
        await expect(page).toHaveURL(/\/administracion\/integraciones\/argos$/);
        await expect(page.getByRole("heading", { name: "Conexión con Argos" })).toBeVisible();
    });
});
