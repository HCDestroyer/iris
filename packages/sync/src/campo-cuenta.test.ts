import { describe, expect, it } from "vitest";
import {
    agruparSecreto,
    cuerpoContrasenaPropia,
    enlaceOtpauth,
    esApariencia,
    esOscuro,
    esquemaCodigoActivar,
    esquemaContrasenaPropia,
    estadoDosPasos,
    lineaPerfil,
    mismaVista,
    pestanaDe,
    reglasContrasena,
    urlDeVista,
    vistaDeUrl,
    type Vista,
} from "./campo";

describe("«Yo» de la app de campo", () => {
    it("apariencia: «Teléfono» sigue al sistema; claro y oscuro lo fuerzan", () => {
        expect(esOscuro("telefono", true)).toBe(true);
        expect(esOscuro("telefono", false)).toBe(false);
        expect(esOscuro("claro", true)).toBe(false);
        expect(esOscuro("oscuro", false)).toBe(true);
        expect(esApariencia("oscuro")).toBe(true);
        expect(esApariencia("system")).toBe(false);
        expect(esApariencia(null)).toBe(false);
    });

    it("línea del perfil con lo que dé /yo (sin inventar la cuadrilla)", () => {
        expect(lineaPerfil([{ rol: "tecnico", cuadrillaId: 3 }], "Fibra GT")).toBe("Técnico · Fibra GT");
        expect(lineaPerfil([{ rol: "supervisor" }, { rol: "tecnico" }, { rol: "tecnico", zonaId: 2 }], null)).toBe("Técnico · Supervisor");
        expect(lineaPerfil([], "Fibra GT")).toBe("Fibra GT");
        expect(lineaPerfil(null, undefined)).toBe("");
        expect(lineaPerfil([{ rol: "bodeguero" }], null)).toBe("bodeguero");
    });

    it("contraseña: espejo de la API (12+ en runas, distinta de la actual, coinciden)", () => {
        const ok = esquemaContrasenaPropia.safeParse({ actual: "vieja", nueva: "una frase larga", repetir: "una frase larga" });
        expect(ok.success).toBe(true);
        if (ok.success) {
            expect(cuerpoContrasenaPropia(ok.data)).toEqual({ actual: "vieja", nueva: "una frase larga" });
        }
        const errores = (d: { actual: string; nueva: string; repetir: string }) => {
            const r = esquemaContrasenaPropia.safeParse(d);
            return r.success ? {} : Object.fromEntries(r.error.issues.map((i) => [String(i.path[0]), i.message]));
        };
        expect(errores({ actual: "", nueva: "corta", repetir: "" })).toEqual({ actual: "Escribe tu contraseña actual", nueva: "Usa 12 caracteres o más", repetir: "Repite la contraseña nueva" });
        expect(errores({ actual: "misma frase larga", nueva: "misma frase larga", repetir: "misma frase larga" })).toEqual({ nueva: "Debe ser distinta de la actual" });
        expect(errores({ actual: "x", nueva: "una frase larga", repetir: "otra cosa" })).toEqual({ repetir: "No coincide con la nueva" });
        // 12 emojis son 12 caracteres para Go (24 unidades UTF-16).
        expect(errores({ actual: "x", nueva: "😀".repeat(12), repetir: "😀".repeat(12) })).toEqual({});
        expect(errores({ actual: "x", nueva: "😀".repeat(11), repetir: "😀".repeat(11) })).toEqual({ nueva: "Usa 12 caracteres o más" });
    });

    it("ayudas en verde solo cuando la regla se cumple", () => {
        expect(reglasContrasena({ actual: "", nueva: "", repetir: "" })).toEqual({ largo: false, distinta: false, coinciden: false });
        expect(reglasContrasena({ actual: "vieja", nueva: "una frase larga", repetir: "una frase larga" })).toEqual({ largo: true, distinta: true, coinciden: true });
        expect(reglasContrasena({ actual: "una frase larga", nueva: "una frase larga", repetir: "x" })).toEqual({ largo: true, distinta: false, coinciden: false });
    });

    it("dos pasos: clave en grupos de 4, enlace otpauth y código de 6 dígitos", () => {
        expect(agruparSecreto("JBSWY3DPEHPK3PXP")).toBe("JBSW Y3DP EHPK 3PXP");
        expect(agruparSecreto("JBSW Y3DP EH")).toBe("JBSW Y3DP EH");
        const url = "otpauth://totp/Iris:ana@isp.gt?algorithm=SHA1&digits=6&issuer=Iris&period=30&secret=JBSWY3DPEHPK3PXP";
        expect(enlaceOtpauth({ secreto: "JBSWY3DPEHPK3PXP", url }, "ana@isp.gt")).toBe(url);
        expect(enlaceOtpauth({ secreto: "JBSWY3DPEHPK3PXP" }, "ana@isp.gt")).toBe("otpauth://totp/ana%40isp.gt?algorithm=SHA1&digits=6&period=30&secret=JBSWY3DPEHPK3PXP");
        expect(esquemaCodigoActivar.safeParse({ codigo: "123 456" })).toEqual({ success: true, data: { codigo: "123456" } });
        expect(esquemaCodigoActivar.safeParse({ codigo: "12345" }).success).toBe(false);
        expect(estadoDosPasos(true)).toEqual({ texto: "Activa", tono: "success" });
        expect(estadoDosPasos(false)).toEqual({ texto: "Apagada", tono: "neutral" });
        expect(estadoDosPasos(null)).toBeNull();
    });

    it("las pantallas de «Yo» tienen su dirección y son de la pestaña «Yo»", () => {
        const vistas: Vista[] = [{ tipo: "yo" }, { tipo: "yo", seccion: "contrasena" }, { tipo: "yo", seccion: "dos_pasos" }];
        for (const v of vistas) {
            expect(vistaDeUrl(urlDeVista(v).replace("/campo", ""))).toEqual(v);
            expect(pestanaDe(v)).toBe("yo");
        }
        expect(urlDeVista({ tipo: "yo" })).toBe("/campo?vista=yo");
        expect(urlDeVista({ tipo: "yo", seccion: "contrasena" })).toBe("/campo?vista=yo&seccion=contrasena");
        expect(vistaDeUrl("?vista=yo&seccion=otra")).toEqual({ tipo: "yo" });
        expect(mismaVista({ tipo: "yo" }, { tipo: "yo", seccion: "dos_pasos" })).toBe(false);
    });
});
