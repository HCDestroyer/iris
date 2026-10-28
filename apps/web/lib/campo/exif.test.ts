import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { fechaExifAIso, leerExif } from "./exif";

/** JPEG mínimo con APP1/EXIF (big endian): fecha original, desfase y GPS. */
function jpegConExif(opciones: { fecha?: string; desfase?: string; gps?: { lat: [number, number, number]; latRef: string; lon: [number, number, number]; lonRef: string } }): Uint8Array {
    const tiff: number[] = [];
    const u16 = (v: number) => tiff.push((v >> 8) & 0xff, v & 0xff);
    const u32 = (v: number) => tiff.push((v >>> 24) & 0xff, (v >>> 16) & 0xff, (v >>> 8) & 0xff, v & 0xff);
    const ascii = (s: string) => [...s, "\0"].map((c) => c.charCodeAt(0));
    // Disposición fija: cabecera (8) · IFD0 (2 + 2·12 + 4 = 30) · ExifIFD (2 + 2·12 + 4 = 30) · GPS IFD (2 + 4·12 + 4 = 54) · datos.
    const ifd0 = 8;
    const exifIfd = ifd0 + 30;
    const gpsIfd = exifIfd + 30;
    let datos = gpsIfd + 54;
    const extra: number[] = [];
    const reservar = (bytes: number[]) => {
        const o = datos;
        extra.push(...bytes);
        datos += bytes.length;
        return o;
    };
    tiff.push(0x4d, 0x4d);
    u16(42);
    u32(ifd0);
    // IFD0: ExifIFD y GPS.
    u16(2);
    u16(0x8769);
    u16(4);
    u32(1);
    u32(exifIfd);
    u16(0x8825);
    u16(4);
    u32(1);
    u32(gpsIfd);
    u32(0);
    // ExifIFD: fecha original y desfase.
    const fecha = ascii(opciones.fecha ?? "2026:10:28 09:15:30");
    const desfase = ascii(opciones.desfase ?? "-06:00");
    u16(2);
    u16(0x9003);
    u16(2);
    u32(fecha.length);
    u32(reservar(fecha));
    u16(0x9011);
    u16(2);
    u32(desfase.length);
    u32(reservar(desfase));
    u32(0);
    // GPS.
    const g = opciones.gps;
    const racional = (vals: [number, number, number]) => vals.flatMap((v) => [(v * 100) >>> 24, ((v * 100) >>> 16) & 0xff, ((v * 100) >>> 8) & 0xff, (v * 100) & 0xff, 0, 0, 0, 100]);
    u16(g ? 4 : 0);
    if (g) {
        u16(1);
        u16(2);
        u32(2);
        tiff.push(g.latRef.charCodeAt(0), 0, 0, 0);
        u16(2);
        u16(5);
        u32(3);
        u32(reservar(racional(g.lat)));
        u16(3);
        u16(2);
        u32(2);
        tiff.push(g.lonRef.charCodeAt(0), 0, 0, 0);
        u16(4);
        u16(5);
        u32(3);
        u32(reservar(racional(g.lon)));
    } else {
        tiff.push(...new Array(48).fill(0));
    }
    u32(0);
    const cuerpo = [...tiff, ...extra];
    const app1 = [0x45, 0x78, 0x69, 0x66, 0, 0, ...cuerpo];
    const largo = app1.length + 2;
    return Uint8Array.from([0xff, 0xd8, 0xff, 0xe1, (largo >> 8) & 0xff, largo & 0xff, ...app1, 0xff, 0xd9]);
}

describe("leerExif", () => {
    it("lee la hora de la toma con su desfase y el GPS (sur/oeste negativos)", () => {
        const bytes = jpegConExif({
            fecha: "2026:10:28 09:15:30",
            desfase: "-06:00",
            gps: { lat: [14, 36, 18], latRef: "N", lon: [90, 30, 36], lonRef: "W" },
        });
        expect(leerExif(bytes)).toEqual({ tomadoEn: "2026-10-28T15:15:30.000Z", lat: 14.605, lon: -90.51 });
    });

    it("sin GPS solo da la hora; un archivo que no es JPEG no da nada", () => {
        expect(leerExif(jpegConExif({}))).toEqual({ tomadoEn: "2026-10-28T15:15:30.000Z" });
        expect(leerExif(Uint8Array.from([0x89, 0x50, 0x4e, 0x47]))).toEqual({});
        expect(leerExif(Uint8Array.from([0xff, 0xd8, 0xff, 0xe1, 0, 4, 1, 2]))).toEqual({});
    });

    it("fecha EXIF sin desfase: se toma la hora de Guatemala", () => {
        expect(fechaExifAIso("2026:10:28 23:59:59", "")).toBe("2026-10-29T05:59:59.000Z");
        expect(fechaExifAIso("no es fecha")).toBeUndefined();
    });
    it("lee una foto real (EXIF little endian escrito por Pillow, la de las pruebas e2e)", () => {
        const foto = readFileSync(fileURLToPath(new URL("../../e2e/fixtures/foto-fachada.jpg", import.meta.url)));
        expect(leerExif(new Uint8Array(foto))).toEqual({ tomadoEn: "2026-10-28T15:15:30.000Z", lat: 14.605, lon: -90.51 });
    });
});
