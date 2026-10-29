import { base64ABytes } from "../src/campo/base64";
import { BYTES_OBJETIVO, CARPETAS_CACHE_FOTOS, comprimirConTope, datosDeExif, dimensionesObjetivo, sobrantes, vaciarCacheFotos, type FotoComprimida } from "../src/campo/foto";
import { validarUrlApi } from "../src/entorno-nativo";

jest.mock("expo-image-manipulator", () => ({ manipulateAsync: jest.fn(), SaveFormat: { JPEG: "jpeg" } }));
const borrados: string[] = [];
jest.mock("expo-file-system", () => ({
    File: jest.fn(),
    Paths: { cache: "cache" },
    Directory: jest.fn().mockImplementation((_base: string, nombre: string) => ({ exists: true, delete: () => borrados.push(nombre) })),
}));
jest.mock("expo-secure-store", () => ({}));
jest.mock("expo-sqlite/kv-store", () => ({}));
jest.mock("expo-image-picker", () => ({}));

const foto = (bytes: number): FotoComprimida => ({ bytes: new Uint8Array(bytes), mime: "image/jpeg", ancho: 1, alto: 1 });

describe("compresión de fotos (≤ 1600 px y ~300 KB, como la PWA)", () => {
    it("medidas conservando la proporción y sin agrandar", () => {
        expect(dimensionesObjetivo(4000, 3000)).toEqual({ ancho: 1600, alto: 1200 });
        expect(dimensionesObjetivo(3000, 4000)).toEqual({ ancho: 1200, alto: 1600 });
        expect(dimensionesObjetivo(800, 600)).toEqual({ ancho: 800, alto: 600 });
    });

    it("baja la calidad hasta caber y se queda con la primera que cabe", async () => {
        const intentos: [number, number][] = [];
        const r = await comprimirConTope(async (lado, calidad) => {
            intentos.push([lado, calidad]);
            return foto(calidad > 0.6 ? 500_000 : 200_000);
        }, 1600);
        expect(r.bytes.byteLength).toBe(200_000);
        expect(intentos).toEqual([
            [1600, 0.82],
            [1600, 0.72],
            [1600, 0.62],
            [1600, 0.52],
        ]);
    });

    it("si nada cabe, achica el lado y al final devuelve la más chica", async () => {
        const r = await comprimirConTope(async (lado, calidad) => foto(Math.round(BYTES_OBJETIVO * 2 + lado * 10 + calidad * 100)), 1600);
        expect(r.bytes.byteLength).toBe(Math.round(BYTES_OBJETIVO * 2 + 1024 * 10 + 0.42 * 100));
    });
});

describe("EXIF de expo-image-picker", () => {
    it("Android (plano): GPS en grados decimales con referencia y hora con desfase", () => {
        expect(
            datosDeExif({ GPSLatitude: 14.6349, GPSLatitudeRef: "N", GPSLongitude: 90.5069, GPSLongitudeRef: "W", DateTimeOriginal: "2026:10:28 09:15:00", OffsetTimeOriginal: "-06:00" }),
        ).toEqual({ lat: 14.6349, lon: -90.5069, tomadoEn: "2026-10-28T15:15:00.000Z" });
    });

    it("Android con racionales «grados/1,minutos/1,segundos/100»", () => {
        const d = datosDeExif({ GPSLatitude: "14/1,38/1,563/100", GPSLatitudeRef: "N", GPSLongitude: "90/1,30/1,2484/100", GPSLongitudeRef: "W" });
        expect(d.lat).toBeCloseTo(14.634897, 5);
        expect(d.lon).toBeCloseTo(-90.5069, 4);
    });

    it("iOS (anidado en {GPS} y {Exif})", () => {
        expect(
            datosDeExif({
                "{GPS}": { Latitude: 14.6349, LatitudeRef: "N", Longitude: 90.5069, LongitudeRef: "W" },
                "{Exif}": { DateTimeOriginal: "2026:10:28 09:15:00", OffsetTimeOriginal: "-06:00" },
            }),
        ).toEqual({ lat: 14.6349, lon: -90.5069, tomadoEn: "2026-10-28T15:15:00.000Z" });
    });

    it("sin EXIF, con 0,0 o con basura no inventa datos", () => {
        expect(datosDeExif(null)).toEqual({});
        expect(datosDeExif({ GPSLatitude: 0, GPSLongitude: 0 })).toEqual({});
        expect(datosDeExif({ GPSLatitude: "x", DateTimeOriginal: "ayer" })).toEqual({});
    });
});

describe("base64ABytes (PNG de la firma)", () => {
    it("decodifica con o sin prefijo data: y relleno", () => {
        expect(base64ABytes("aGVsbG8=")).toEqual(new Uint8Array([104, 101, 108, 108, 111]));
        expect(base64ABytes("data:image/png;base64,iVBORw0KGgo=")).toEqual(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));
        expect(() => base64ABytes("no*válido")).toThrow();
    });
});

describe("temporales de la cámara", () => {
    it("tras comprimir sobran todas las variantes menos la elegida", () => {
        expect(sobrantes(["a.jpg", "b.jpg", "c.jpg"], "b.jpg")).toEqual(["a.jpg", "c.jpg"]);
        expect(sobrantes(["a.jpg"], undefined)).toEqual(["a.jpg"]);
    });
    it("al cerrar sesión vacía las cachés de la cámara y del compresor", () => {
        vaciarCacheFotos();
        expect(borrados).toEqual([...CARPETAS_CACHE_FOTOS]);
    });
});

describe("URL de la API", () => {
    it("en producción exige HTTPS con un mensaje claro; en desarrollo acepta HTTP", () => {
        expect(validarUrlApi("https://iris.isp.gt/", false)).toBe("https://iris.isp.gt");
        expect(() => validarUrlApi("http://iris.isp.gt", false)).toThrow(/https:\/\/ en una compilación de producción/);
        expect(() => validarUrlApi(undefined, false)).toThrow(/EXPO_PUBLIC_IRIS_API_URL/);
        expect(validarUrlApi("http://192.168.1.10:8080", true)).toBe("http://192.168.1.10:8080");
    });
});
