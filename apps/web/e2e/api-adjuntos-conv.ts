import { deflateSync } from "node:zlib";
import type { Page } from "@playwright/test";

/*
 * Adjuntos del agente en la API falsa (contratos/conversaciones.md §7.2.1 y §8): alta
 * (`POST /adjuntos`, entidad `conversacion`), subida por partes tus (`PATCH /adjuntos/{id}/subida`
 * con `Upload-Offset`) y descarga (`GET /adjuntos/{id}`). Con los límites reales del servidor:
 * JPG, PNG, WebP y PDF (`IRIS_ADJUNTOS_MIME` de fábrica, sin video ni audio), un adjunto por
 * mensaje y el tope de tamaño del módulo.
 */

type Registro = { mime: string; tamano: number; recibido: number; entidadId: string };

/** Lo subido en esta ejecución: la API falsa de conversaciones lo consulta al mandar un mensaje con `adjuntoId`. */
export const ADJUNTOS_SUBIDOS = new Map<string, Registro>();

const MIME_SERVIDOR = ["image/jpeg", "image/png", "image/webp", "image/svg+xml", "application/pdf"];
const MAX_SERVIDOR = 25 * 1024 * 1024;

function crc32(datos: Buffer): number {
    let c = ~0;
    for (const b of datos) {
        c ^= b;
        for (let k = 0; k < 8; k++) c = (c >>> 1) ^ (0xedb88320 & -(c & 1));
    }
    return ~c >>> 0;
}

function trozo(tipo: string, datos: Buffer): Buffer {
    const cabeza = Buffer.alloc(4);
    cabeza.writeUInt32BE(datos.length);
    const cuerpo = Buffer.concat([Buffer.from(tipo, "ascii"), datos]);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(cuerpo));
    return Buffer.concat([cabeza, cuerpo, crc]);
}

/** Un PNG de verdad (de un color), del tamaño pedido: sirve para medir cómo cambia la altura del hilo. */
export function png(ancho: number, alto: number, rgb: [number, number, number] = [42, 120, 214]): Buffer {
    const cabecera = Buffer.alloc(13);
    cabecera.writeUInt32BE(ancho, 0);
    cabecera.writeUInt32BE(alto, 4);
    cabecera.set([8, 2, 0, 0, 0], 8);
    const fila = Buffer.alloc(1 + ancho * 3);
    for (let x = 0; x < ancho; x++) fila.set(rgb, 1 + x * 3);
    const crudo = Buffer.concat(Array.from({ length: alto }, () => fila));
    return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), trozo("IHDR", cabecera), trozo("IDAT", deflateSync(crudo)), trozo("IEND", Buffer.alloc(0))]);
}

/** Un PDF mínimo válido por su firma. */
export const PDF = Buffer.from("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n", "latin1");

export type OpcionesAdjuntos = {
    /** La descarga de una imagen tarda esto (la foto «carga tarde»). */
    demoraDescargaMs?: number;
    /** Cada parte de la subida tarda esto (para ver el progreso). */
    demoraParteMs?: number;
    /** Las primeras `n` partes que se suben fallan con 500. */
    fallarPartes?: number;
    /**
     * Las partes no se contestan hasta `soltarPartes()`: la subida sigue «en curso» todo lo que la
     * prueba necesite, sin depender de cuánto tarde la máquina.
     */
    retenerPartes?: boolean;
    /** Las primeras `n` altas (`POST /adjuntos`) fallan con 500. */
    fallarAltas?: number;
};

export type AdjuntosFalsos = {
    /** Altas recibidas, en orden. */
    altas: { id: string; mime: string; tamano: number; tipo: string; entidad: string; entidadId: string }[];
    /** Partes recibidas: id, desde qué offset y cuántos bytes. */
    partes: { id: string; offset: number; bytes: number }[];
    descargas: string[];
    /** Contesta las partes retenidas (`retenerPartes`) y deja de retener. */
    soltarPartes: () => void;
};

export async function simularAdjuntos(page: Page, opciones: OpcionesAdjuntos = {}): Promise<AdjuntosFalsos> {
    let soltar: () => void = () => undefined;
    let retenidas: Promise<void> | null = opciones.retenerPartes ? new Promise((r) => (soltar = r)) : null;
    const estado: AdjuntosFalsos = {
        altas: [],
        partes: [],
        descargas: [],
        soltarPartes: () => {
            retenidas = null;
            soltar();
        },
    };
    let fallarPartes = opciones.fallarPartes ?? 0;
    let fallarAltas = opciones.fallarAltas ?? 0;
    const foto = png(600, 800);
    const problema = (status: number, codigo: string, title: string) => ({ status, contentType: "application/problem+json", body: JSON.stringify({ type: `urn:iris:problema:${codigo}`, title, status }) });

    await page.route("**/api/v1/adjuntos", async (route) => {
        const req = route.request();
        if (req.method() !== "POST") return route.fallback();
        const cuerpo = req.postDataJSON() as { id: string; entidad: string; entidadId: string; tipo: string; mime: string; tamano: number };
        if (cuerpo.entidad !== "conversacion") return route.fallback();
        if (fallarAltas > 0) {
            fallarAltas--;
            return route.fulfill(problema(500, "error_interno", "Error interno"));
        }
        // Como el servidor: tipos de `IRIS_ADJUNTOS_MIME` y tope de tamaño del módulo.
        if (!MIME_SERVIDOR.includes(cuerpo.mime)) return route.fulfill(problema(422, "mime_no_permitido", `El tipo "${cuerpo.mime}" no está permitido`));
        if (cuerpo.tamano > MAX_SERVIDOR) return route.fulfill(problema(413, "tamano_excedido", "El archivo es demasiado grande"));
        estado.altas.push(cuerpo);
        ADJUNTOS_SUBIDOS.set(cuerpo.id, { mime: cuerpo.mime, tamano: cuerpo.tamano, recibido: 0, entidadId: cuerpo.entidadId });
        return route.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify({ id: cuerpo.id, estado: "pendiente", mime: cuerpo.mime, tamano: cuerpo.tamano }) });
    });

    await page.route(/\/api\/v1\/adjuntos\/[0-9a-f-]{36}\/subida$/, async (route) => {
        const req = route.request();
        if (req.method() !== "PATCH") return route.fallback();
        const id = /adjuntos\/([0-9a-f-]{36})\/subida/.exec(req.url())?.[1] ?? "";
        const registro = ADJUNTOS_SUBIDOS.get(id);
        if (!registro) return route.fulfill(problema(404, "no_encontrado", "No existe"));
        const offset = Number((await req.headerValue("upload-offset")) ?? "x");
        if (offset !== registro.recibido) return route.fulfill({ ...problema(409, "offset_distinto", "Offset distinto"), headers: { "Upload-Offset": String(registro.recibido) } });
        if (retenidas) await retenidas;
        if (opciones.demoraParteMs) await new Promise((r) => setTimeout(r, opciones.demoraParteMs));
        if (fallarPartes > 0) {
            fallarPartes--;
            return route.fulfill(problema(500, "error_interno", "Error interno"));
        }
        const bytes = req.postDataBuffer()?.length ?? 0;
        registro.recibido += bytes;
        estado.partes.push({ id, offset, bytes });
        return route.fulfill({ status: 204, headers: { "Upload-Offset": String(registro.recibido), "Tus-Resumable": "1.0.0" } });
    });

    await page.route(/\/api\/v1\/adjuntos\/[0-9a-f-]{36}$/, async (route) => {
        const req = route.request();
        if (req.method() !== "GET") return route.fallback();
        const id = /adjuntos\/([0-9a-f-]{36})$/.exec(req.url())?.[1] ?? "";
        estado.descargas.push(id);
        if (opciones.demoraDescargaMs) await new Promise((r) => setTimeout(r, opciones.demoraDescargaMs));
        const registro = ADJUNTOS_SUBIDOS.get(id);
        if (registro?.mime === "application/pdf") return route.fulfill({ status: 200, contentType: "application/pdf", body: PDF });
        return route.fulfill({ status: 200, contentType: "image/png", body: foto });
    });

    return estado;
}
