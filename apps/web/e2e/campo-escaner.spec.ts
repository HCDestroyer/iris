import path from "node:path";
import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { conSesionTecnico, simularApiCampo } from "./api-campo";
import { irAPaso } from "./ayudas-campo";
import { ordenesDelDia } from "./datos-campo";

/*
 * Escáner de serie y MAC por campo (/campo, paso «Materiales y equipos»), en escritorio y teléfono,
 * claro y oscuro. La cámara es un lienzo (`captureStream`) y el lector un `BarcodeDetector` simulado
 * que «ve» los códigos de `window.__codigos`: cada uno dentro o fuera del recuadro del visor (con su
 * `boundingBox` en píxeles de la cámara, como el de Chrome). La linterna se simula en la pista.
 */

interface CodigoSimulado {
    valor: string;
    formato: string;
    /** Dentro del recuadro (por defecto) o en una esquina de la cámara, fuera de él. */
    fuera?: boolean;
}

declare global {
    interface Window {
        __codigos: CodigoSimulado[];
        __linterna: boolean[];
        __sinPermiso: boolean;
        __vibraciones: number;
    }
}

async function simularCamara(page: Page, { sinPermiso = false } = {}) {
    await page.addInitScript((sinPermisoInicial: boolean) => {
        window.__codigos = [];
        window.__linterna = [];
        window.__sinPermiso = sinPermisoInicial;
        window.__vibraciones = 0;
        navigator.vibrate = () => {
            window.__vibraciones += 1;
            return true;
        };
        const ANCHO = 640;
        const ALTO = 480;
        navigator.mediaDevices.getUserMedia = () => {
            if (window.__sinPermiso) {
                return Promise.reject(new DOMException("Permiso negado", "NotAllowedError"));
            }
            const lienzo = document.createElement("canvas");
            lienzo.width = ANCHO;
            lienzo.height = ALTO;
            const ctx = lienzo.getContext("2d");
            const pintar = () => {
                if (ctx) {
                    ctx.fillStyle = "#3f3f46";
                    ctx.fillRect(0, 0, ANCHO, ALTO);
                    ctx.fillStyle = "#e4e4e7";
                    ctx.fillRect(180, 200, 280, 80);
                }
            };
            pintar();
            setInterval(pintar, 100);
            const flujo = lienzo.captureStream(10);
            const pista = flujo.getVideoTracks()[0] as MediaStreamTrack & { getCapabilities: () => object };
            pista.getCapabilities = () => ({ torch: true });
            pista.applyConstraints = (c?: MediaTrackConstraints) => {
                const avanzado = (c?.advanced?.[0] ?? {}) as { torch?: boolean };
                window.__linterna.push(avanzado.torch === true);
                return Promise.resolve();
            };
            return Promise.resolve(flujo);
        };
        // BarcodeDetector simulado: el centro del recuadro del visor pasado a píxeles de la cámara
        // (`object-fit: cover`), o una esquina de la cámara para los que van fuera.
        class DetectorSimulado {
            detect(video: HTMLVideoElement) {
                const r = document.querySelector('[data-slot="recuadro"]')?.getBoundingClientRect();
                const v = video.getBoundingClientRect();
                if (!r) {
                    return Promise.resolve([]);
                }
                const escala = Math.max(v.width / ANCHO, v.height / ALTO);
                const cx = (r.left - v.left + r.width / 2 + (ANCHO * escala - v.width) / 2) / escala;
                const cy = (r.top - v.top + r.height / 2 + (ALTO * escala - v.height) / 2) / escala;
                return Promise.resolve(
                    window.__codigos.map((c, i) => ({
                        rawValue: c.valor,
                        format: c.formato,
                        boundingBox: c.fuera ? { x: 0, y: 0, width: 20, height: 10 } : { x: cx - 40, y: cy - 10 + (i % 3), width: 80, height: 20 },
                    })),
                );
            }
        }
        (window as unknown as { BarcodeDetector: unknown }).BarcodeDetector = DetectorSimulado;
    }, sinPermiso);
}

/** Con IRIS_CAPTURAS=<carpeta>, captura del visor para revisión visual (teléfono y escritorio, claro y oscuro). */
async function captura(page: Page, info: TestInfo, que: string) {
    if (process.env.IRIS_CAPTURAS) {
        await page.screenshot({ path: path.join(process.env.IRIS_CAPTURAS, `escaner-${que}-${info.project.name}.png`) });
    }
}

const verCodigos = (page: Page, codigos: CodigoSimulado[]) => page.evaluate((c) => (window.__codigos = c), codigos);
const visor = (page: Page) => page.getByRole("dialog", { name: /^Escaneando:/ });
const hoja = (page: Page) => page.getByRole("dialog", { name: "Códigos detectados" });
const notificacion = (page: Page) => page.locator('[data-slot="notificacion"]');

/** Etiqueta de una ONU Huawei: S/N, GPON SN, MAC, EAN-13 y, fuera del recuadro, el modelo. */
const HUAWEI: CodigoSimulado[] = [
    { valor: "6901443254538", formato: "ean_13" },
    { valor: "2102311TUN10K5000123", formato: "code_128" },
    { valor: "48575443A1B2C3D4", formato: "code_128" },
    { valor: "A0B1C2D3E4F5", formato: "code_128" },
    { valor: "EG8145V5", formato: "code_128", fuera: true },
];

test.beforeEach(async ({ context, baseURL, page }) => {
    await conSesionTecnico(context, baseURL ?? "");
    const hoy = new Date().toISOString();
    await simularApiCampo(page, { ordenes: ordenesDelDia().map((o) => (o.id === 101 ? { ...o, estado: "en_sitio", enCaminoEn: hoy, enSitioEn: hoy } : o)) });
});

async function abrirEquipos(page: Page) {
    await page.goto("/campo");
    await page.getByRole("link", { name: /OT-000101/ }).click();
    await expect(page.getByRole("heading", { name: "OT-000101" })).toBeVisible();
    await irAPaso(page, "Materiales y equipos");
    await expect(page.getByRole("button", { name: "Escanear número de serie" })).toBeVisible();
}

test("varios códigos: no acepta el primero, lista lo del recuadro con lo que parece y llena solo la serie", async ({ page }, info) => {
    await simularCamara(page);
    await abrirEquipos(page);
    // Cada campo tiene su botón (44 px o más) y el escáner abre sabiendo qué escanea.
    const boton = page.getByRole("button", { name: "Escanear número de serie" });
    expect((await boton.boundingBox())?.height).toBeGreaterThanOrEqual(44);
    await expect(page.getByRole("button", { name: "Escanear MAC" })).toBeVisible();
    await boton.click();
    await expect(visor(page)).toBeVisible();
    await expect(visor(page).getByRole("heading", { name: "Escaneando: Número de serie" })).toBeVisible();
    await expect(visor(page)).toContainText("Apunta solo al código junto a S/N");
    await expect(visor(page).getByRole("status")).toHaveText("Detectados: 0");
    await captura(page, info, "visor");

    // Sin códigos sigue esperando: nunca se cierra solo.
    await page.waitForTimeout(1200);
    await expect(hoja(page)).toBeHidden();

    // Un cuadro con un solo código no se acepta: se juntan durante la ventana.
    await verCodigos(page, [HUAWEI[0] as CodigoSimulado]);
    await expect(visor(page).getByRole("status")).toHaveText("Detectados: 1");
    await expect(hoja(page)).toBeHidden();
    await verCodigos(page, HUAWEI);
    await expect(visor(page).getByRole("status")).toHaveText("Detectados: 4");
    await expect(hoja(page)).toBeVisible();
    expect(await page.evaluate(() => window.__vibraciones)).toBe(1);

    // La lista: lo que encaja primero, el EAN al final y el modelo (fuera del recuadro) no está.
    await expect(hoja(page).getByRole("heading", { name: "Elige el código del número de serie" })).toBeVisible();
    const filas = hoja(page).getByRole("list", { name: "Códigos detectados" }).getByRole("listitem");
    await expect(filas).toHaveCount(4);
    await expect(filas.nth(0)).toContainText("Código de barras");
    await expect(filas.nth(0)).toContainText("Code 128");
    await expect(filas.nth(0)).toContainText("2102311TUN10K5000123");
    await expect(filas.nth(0)).toContainText("Parece serie");
    await expect(filas.nth(1)).toContainText("48575443A1B2C3D4");
    await expect(filas.nth(2)).toContainText("Parece MAC");
    await expect(filas.nth(3)).toContainText("6901443254538");
    await expect(filas.nth(3)).toContainText("Parece código de producto (EAN/UPC)");
    await expect(hoja(page)).not.toContainText("EG8145V5");
    await expect(hoja(page).getByRole("button", { name: "Seguir escaneando" })).toBeVisible();
    await expect(hoja(page).getByRole("button", { name: "Escribir a mano" })).toBeVisible();
    await captura(page, info, "varios-codigos");

    // Un toque elige: se llena solo la serie, con la insignia «Escaneado», y se puede editar.
    await filas.nth(0).getByRole("button").click();
    await expect(visor(page)).toBeHidden();
    await expect(page.getByLabel("Número de serie")).toHaveValue("2102311TUN10K5000123");
    await expect(page.getByLabel("MAC")).toHaveValue("");
    await expect(notificacion(page)).toContainText("Serie: 2102311TUN10K5000123");
    await captura(page, info, "serie-escaneada");
    const serie = page.getByLabel("Número de serie");
    await expect(serie).toHaveAccessibleDescription(/Escaneado/);
    await serie.fill("2102311TUN10K5000124");
    await expect(serie).not.toHaveAccessibleDescription(/Escaneado/);
});

test("MAC: un solo candidato se confirma, lo demás es «No es una MAC»; «Seguir escaneando» y QR con serie y MAC", async ({ page }, info) => {
    await simularCamara(page);
    await abrirEquipos(page);
    await page.getByRole("button", { name: "Escanear MAC" }).click();
    await expect(visor(page).getByRole("heading", { name: "Escaneando: MAC" })).toBeVisible();
    await expect(visor(page)).toContainText("Apunta solo al código junto a MAC");

    // Linterna: se ofrece porque la pista la admite.
    await visor(page).getByRole("button", { name: "Encender linterna" }).click();
    await expect(visor(page).getByRole("button", { name: "Apagar linterna" })).toHaveAttribute("aria-pressed", "true");
    expect(await page.evaluate(() => window.__linterna)).toEqual([true]);

    await verCodigos(page, [
        { valor: "a0:b1:c2:d3:e4:f6", formato: "code_128" },
        { valor: "6972632140024", formato: "ean_13" },
    ]);
    await expect(hoja(page).getByRole("heading", { name: "¿Usar A0:B1:C2:D3:E4:F6 como MAC?" })).toBeVisible();
    await expect(hoja(page)).toContainText("Parece MAC");
    const otros = hoja(page).getByRole("list", { name: "Otros códigos detectados" });
    await expect(otros).toContainText("6972632140024");
    await expect(otros).toContainText("No es una MAC");
    await expect(otros.getByRole("button")).toBeDisabled();
    await captura(page, info, "confirmacion-mac");

    // «Seguir escaneando»: se cierra la hoja y empieza otra ventana.
    await verCodigos(page, []);
    await hoja(page).getByRole("button", { name: "Seguir escaneando" }).click();
    await expect(hoja(page)).toBeHidden();
    await expect(visor(page).getByRole("status")).toHaveText("Detectados: 0");

    // QR con serie y MAC: se muestran las dos y se llenan los dos campos.
    await verCodigos(page, [{ valor: "SN:ZTEGC8F1A2B3;MAC:A0B1C2D3E4F6", formato: "qr_code" }]);
    await expect(hoja(page).getByRole("heading", { name: "¿Usar la serie ZTEGC8F1A2B3 y la MAC A0:B1:C2:D3:E4:F6?" })).toBeVisible();
    await expect(hoja(page)).toContainText("QR con serie y MAC");
    await expect(hoja(page)).toContainText("S/N ZTEGC8F1A2B3");
    await captura(page, info, "qr-combinado");
    await hoja(page).getByRole("button", { name: "Usar" }).click();
    await expect(page.getByLabel("Número de serie")).toHaveValue("ZTEGC8F1A2B3");
    await expect(page.getByLabel("MAC")).toHaveValue("A0:B1:C2:D3:E4:F6");
    await expect(notificacion(page)).toContainText("Serie: ZTEGC8F1A2B3 · MAC: A0:B1:C2:D3:E4:F6");

    // Ya registrada en esta orden: se avisa antes de usarla otra vez.
    await page.getByRole("button", { name: "Registrar equipo instalado" }).click();
    await expect(page.getByText("A0:B1:C2:D3:E4:F6", { exact: true })).toBeVisible();
    await verCodigos(page, [{ valor: "ZTEGC8F1A2B3", formato: "code_128" }]);
    await page.getByRole("button", { name: "Escanear número de serie" }).click();
    await expect(hoja(page).getByRole("heading", { name: "¿Usar ZTEGC8F1A2B3 como número de serie?" })).toBeVisible();
    await expect(hoja(page)).toContainText("Esta serie ya está en otro equipo de esta orden");
    await captura(page, info, "confirmacion-repetida");
    // «Escribir a mano» cierra el escáner y deja el foco en el campo.
    await hoja(page).getByRole("button", { name: "Escribir a mano" }).click();
    await expect(visor(page)).toBeHidden();
    await expect(page.getByLabel("Número de serie")).toBeFocused();
    await expect(page.getByLabel("Número de serie")).toHaveValue("");
});

test("sin permiso de cámara: lo explica y se escribe a mano", async ({ page }) => {
    await simularCamara(page, { sinPermiso: true });
    await abrirEquipos(page);
    await page.getByRole("button", { name: "Escanear MAC" }).click();
    await expect(visor(page)).toContainText("No diste permiso para usar la cámara. Escribe el dato a mano.");
    await visor(page).getByRole("button", { name: "Escribir a mano" }).click();
    await expect(page.getByLabel("MAC")).toBeFocused();
});
