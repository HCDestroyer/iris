import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import { cambiosDeArgos, camposIniciales, cuerpoAplicar, cuerpoDescartar, errorDatosArgos, esquemaAplicarDatosArgos, textoCamposArgos } from "./datos-argos";

describe("dato de Argos cambió", () => {
    it("solo con la forma esperada", () => {
        expect(cambiosDeArgos({})).toBeUndefined();
        expect(cambiosDeArgos({ datosArgosCambiados: null })).toBeUndefined();
        expect(cambiosDeArgos({ datosArgosCambiados: { campos: ["x"] } })).toBeUndefined();
        expect(cambiosDeArgos({ datosArgosCambiados: { en: "2026-10-31T10:00:00Z", campos: ["servicio.direccion", 3] } })).toEqual({
            en: "2026-10-31T10:00:00Z",
            campos: ["servicio.direccion"],
        });
    });

    it("dice qué cambió en palabras, sin repetir", () => {
        expect(textoCamposArgos(["servicio.lat", "servicio.lon", "servicio.contacto_telefono"])).toBe("ubicación del servicio, teléfono de contacto en sitio");
        expect(textoCamposArgos(["otro.campo"])).toBe("otro.campo");
        expect(textoCamposArgos(null)).toBe("");
    });
});

describe("aplicar y descartar datos de Argos", () => {
    const aviso = { en: "2026-10-31T10:00:00Z", campos: ["servicio.direccion", "servicio.lat", "servicio.contacto_telefono"] };

    it("marca al abrir lo que cambió (o todo si no se reconoce)", () => {
        expect(camposIniciales(aviso)).toEqual(["direccion", "coordenadas", "contacto"]);
        expect(camposIniciales({ en: aviso.en, campos: ["otro.campo"] })).toEqual(["direccion", "coordenadas", "referencia", "contacto"]);
    });

    it("manda la versión y el aviso leído; con todo marcado, sin campos", () => {
        expect(cuerpoAplicar({ campos: ["contacto", "direccion"] }, 4, aviso)).toEqual({ version: 4, avisoEn: aviso.en, campos: ["direccion", "contacto"] });
        expect(cuerpoAplicar({ campos: ["direccion", "coordenadas", "referencia", "contacto"] }, 4, aviso)).toEqual({ version: 4, avisoEn: aviso.en });
        expect(cuerpoDescartar({ motivo: "  " }, 4, aviso)).toEqual({ version: 4, avisoEn: aviso.en });
        expect(cuerpoDescartar({ motivo: " El cliente lo confirmó " }, 4, aviso)).toEqual({ version: 4, avisoEn: aviso.en, motivo: "El cliente lo confirmó" });
        expect(esquemaAplicarDatosArgos.safeParse({ campos: [] }).success).toBe(false);
    });

    it("traduce los errores de la API", () => {
        const e = (estado: number, codigo: string) => new ErrorApi({ estado, codigo, titulo: codigo });
        expect(errorDatosArgos(e(409, "aviso_argos_cambiado"))).toEqual({ tipo: "aviso_nuevo" });
        expect(errorDatosArgos(e(409, "conflicto_version"))).toEqual({ tipo: "version" });
        expect(errorDatosArgos(e(422, "instantanea_no_coincide"))).toMatchObject({ tipo: "mensaje", texto: "El servicio en Argos ya no es de este cliente; revisa la orden a mano." });
        expect(errorDatosArgos(e(422, "sin_instantanea_argos"))).toMatchObject({ tipo: "mensaje" });
        expect(errorDatosArgos(e(500, "interno"))).toEqual({ tipo: "otro" });
    });
});
