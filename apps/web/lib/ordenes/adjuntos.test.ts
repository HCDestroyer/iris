import { describe, expect, it } from "vitest";
import { enlaceMapa, esIdAdjunto, esImagenMostrable, rutaDescargaAdjunto, tipoImagen, urlDescargaConfiable } from "./adjuntos";

const ID = "01936f3e-8a4b-7c2d-9e1f-0a1b2c3d4e5f";

describe("URLs de adjuntos", () => {
    it("arma la ruta de descarga solo desde un uuid válido", () => {
        expect(rutaDescargaAdjunto(ID)).toBe(`/api/v1/adjuntos/${ID}`);
        expect(rutaDescargaAdjunto(ID.toUpperCase())).toBe(`/api/v1/adjuntos/${ID}`);
        expect(rutaDescargaAdjunto("../sesion")).toBeUndefined();
        expect(rutaDescargaAdjunto(`${ID}/../../yo`)).toBeUndefined();
        expect(esIdAdjunto("")).toBe(false);
    });

    it("no sigue una urlDescarga distinta de la esperada", () => {
        expect(urlDescargaConfiable({ id: ID, urlDescarga: `/api/v1/adjuntos/${ID}` })).toBe(`/api/v1/adjuntos/${ID}`);
        expect(urlDescargaConfiable({ id: ID, urlDescarga: `https://otro.example/api/v1/adjuntos/${ID}` })).toBeUndefined();
        expect(urlDescargaConfiable({ id: ID, urlDescarga: "javascript:alert(1)" })).toBeUndefined();
    });

    it("solo muestra imágenes completas de tipos en línea (nunca SVG ni PDF)", () => {
        expect(esImagenMostrable({ id: ID, estado: "completo", mime: "image/jpeg" })).toBe(true);
        expect(esImagenMostrable({ id: ID, estado: "subiendo", mime: "image/jpeg" })).toBe(false);
        expect(esImagenMostrable({ id: ID, estado: "completo", mime: "image/svg+xml" })).toBe(false);
        expect(esImagenMostrable({ id: ID, estado: "completo", mime: "application/pdf" })).toBe(false);
        expect(tipoImagen("image/png; charset=binary")).toBe("image/png");
        expect(tipoImagen("text/html")).toBeUndefined();
        expect(tipoImagen(null)).toBeUndefined();
    });

    it("enlace a OpenStreetMap con coordenadas válidas", () => {
        expect(enlaceMapa(14.634915, -90.506882)).toBe("https://www.openstreetmap.org/?mlat=14.634915&mlon=-90.506882#map=18/14.634915/-90.506882");
        expect(enlaceMapa(95, 0)).toBeUndefined();
        expect(enlaceMapa(Number.NaN, 0)).toBeUndefined();
    });
});
