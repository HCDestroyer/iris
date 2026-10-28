import { deflateSync, crc32 } from "node:zlib";
import type { Page } from "@playwright/test";

/*
 * Teselas de OpenStreetMap simuladas: el mapa del despacho nunca sale a la red en las pruebas.
 * Cada tesela es un PNG de 256 × 256 de un solo color generado aquí.
 */

function trozo(tipo: string, datos: Buffer): Buffer {
    const largo = Buffer.alloc(4);
    largo.writeUInt32BE(datos.length);
    const cuerpo = Buffer.concat([Buffer.from(tipo, "ascii"), datos]);
    const crc = Buffer.alloc(4);
    crc.writeUInt32BE(crc32(cuerpo));
    return Buffer.concat([largo, cuerpo, crc]);
}

function pngLiso(lado: number, [r, g, b]: [number, number, number]): Buffer {
    const cabecera = Buffer.alloc(13);
    cabecera.writeUInt32BE(lado, 0);
    cabecera.writeUInt32BE(lado, 4);
    cabecera.set([8, 2, 0, 0, 0], 8); // 8 bits, RGB, sin entrelazado
    const fila = Buffer.alloc(1 + lado * 3);
    for (let x = 0; x < lado; x++) fila.set([r, g, b], 1 + x * 3);
    const crudo = Buffer.concat(Array.from({ length: lado }, () => fila));
    return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), trozo("IHDR", cabecera), trozo("IDAT", deflateSync(crudo)), trozo("IEND", Buffer.alloc(0))]);
}

const TESELA = pngLiso(256, [226, 232, 225]);

/** Intercepta las teselas de OSM; devuelve cuántas se pidieron. */
export async function simularTeselas(page: Page): Promise<{ pedidas: () => number }> {
    let pedidas = 0;
    await page.route("https://tile.openstreetmap.org/**", (route) => {
        pedidas++;
        return route.fulfill({ status: 200, contentType: "image/png", body: TESELA });
    });
    return { pedidas: () => pedidas };
}
