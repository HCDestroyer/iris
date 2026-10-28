import { describe, expect, it } from "vitest";
import {
    agruparSecreto,
    ALFABETO_TEMPORAL,
    cuerpoCambiarContrasena,
    cuerpoReiniciarMfa,
    esquemaCambiarContrasena,
    esquemaReiniciarMfa,
    generarContrasenaTemporal,
    longitud,
} from "./esquemas";
import { trazoQr } from "./qr";
import { esRutaCampo, puedeReiniciarMfa, requiereCambio, rutaCambiarContrasena, rutaConfigurarMfa, siguienteSeguro } from "./sesion";

const ORIGEN = "http://panel.test";

function mensajes(resultado: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) {
    return Object.fromEntries((resultado.error?.issues ?? []).map((i) => [i.path.join("."), i.message]));
}

describe("cambiar contraseña (espejo de yo.cambiar_contrasena)", () => {
    const valida = { actual: "temporal-1234", nueva: "una frase bien larga", confirmar: "una frase bien larga" };

    it("acepta 12+ caracteres, distinta y confirmada", () => {
        expect(esquemaCambiarContrasena.safeParse(valida).success).toBe(true);
        expect(cuerpoCambiarContrasena(valida)).toEqual({ actual: "temporal-1234", nueva: "una frase bien larga" });
    });

    it("exige la actual, 12+ en la nueva y la confirmación", () => {
        const r = esquemaCambiarContrasena.safeParse({ actual: "", nueva: "corta", confirmar: "" });
        expect(mensajes(r)).toEqual({
            actual: "Escribe tu contraseña actual",
            nueva: "Usa al menos 12 caracteres",
            confirmar: "Repite la contraseña nueva",
        });
    });

    it("la nueva debe ser distinta de la actual y coincidir con la confirmación", () => {
        expect(mensajes(esquemaCambiarContrasena.safeParse({ ...valida, nueva: valida.actual, confirmar: valida.actual }))).toEqual({
            nueva: "Debe ser distinta de la actual",
        });
        expect(mensajes(esquemaCambiarContrasena.safeParse({ ...valida, confirmar: "otra cosa distinta" }))).toEqual({
            confirmar: "No coincide con la contraseña nueva",
        });
    });

    it("cuenta caracteres como Go (runas), no unidades UTF-16", () => {
        // 11 emojis = 22 unidades UTF-16 pero 11 runas: la API lo rechaza.
        const once = "🔑".repeat(11);
        expect(once.length).toBe(22);
        expect(longitud(once)).toBe(11);
        expect(esquemaCambiarContrasena.safeParse({ ...valida, nueva: once, confirmar: once }).success).toBe(false);
        const doce = "ñ".repeat(12);
        expect(esquemaCambiarContrasena.safeParse({ ...valida, nueva: doce, confirmar: doce }).success).toBe(true);
        const larga = "a".repeat(201);
        expect(mensajes(esquemaCambiarContrasena.safeParse({ ...valida, nueva: larga, confirmar: larga })).nueva).toBe("Admite hasta 200 caracteres");
    });
});

describe("reiniciar MFA (espejo de EntradaReiniciarMFA)", () => {
    it("exige motivo y una temporal de 12+; revocar tokens va en el cuerpo con la versión", () => {
        expect(mensajes(esquemaReiniciarMfa.safeParse({ motivo: "  ", contrasenaTemporal: "corta", revocarTokens: true }))).toEqual({
            motivo: "Escribe el motivo",
            contrasenaTemporal: "La contraseña temporal necesita al menos 12 caracteres",
        });
        const datos = esquemaReiniciarMfa.parse({ motivo: " Cambió de teléfono ", contrasenaTemporal: "abcd-efgh-jkmn-pqrs", revocarTokens: false });
        expect(cuerpoReiniciarMfa(datos, 7)).toEqual({ version: 7, motivo: "Cambió de teléfono", contrasenaTemporal: "abcd-efgh-jkmn-pqrs", revocarTokens: false });
    });

    it("genera temporales de 4 grupos de 4 con el alfabeto sin ambiguos, que pasan la validación", () => {
        const vistas = new Set<string>();
        for (let i = 0; i < 200; i++) {
            const t = generarContrasenaTemporal();
            expect(t).toMatch(/^[^-]{4}-[^-]{4}-[^-]{4}-[^-]{4}$/);
            for (const c of t.replaceAll("-", "")) {
                expect(ALFABETO_TEMPORAL).toContain(c);
            }
            expect(t).not.toMatch(/[01lIO]/);
            expect(esquemaReiniciarMfa.shape.contrasenaTemporal.safeParse(t).success).toBe(true);
            vistas.add(t);
        }
        expect(vistas.size).toBe(200);
    });

    it("descarta los bytes por encima del último múltiplo del alfabeto (sin sesgo)", () => {
        const n = ALFABETO_TEMPORAL.length;
        const limite = 256 - (256 % n);
        // Primero solo bytes que deben descartarse, luego índices 0, 1, 2…
        let llamada = 0;
        const t = generarContrasenaTemporal((bytes) => {
            bytes.fill(llamada === 0 ? limite : 0);
            if (llamada > 0) {
                bytes.forEach((_, i) => (bytes[i] = i % n));
            }
            llamada++;
        });
        expect(llamada).toBe(2);
        expect(t.replaceAll("-", "")).toBe(ALFABETO_TEMPORAL.slice(0, 16));
    });
});

describe("rutas de la cuenta", () => {
    it("siguiente seguro: nada externo, ni el login ni la propia pantalla", () => {
        expect(siguienteSeguro("/tickets?vista=mias", ORIGEN)).toBe("/tickets?vista=mias");
        expect(siguienteSeguro("//otro.sitio", ORIGEN)).toBe("/");
        expect(siguienteSeguro("/entrar", ORIGEN)).toBe("/");
        expect(siguienteSeguro("/cambiar-contrasena?siguiente=/x", ORIGEN)).toBe("/");
        expect(siguienteSeguro(null, ORIGEN)).toBe("/");
    });

    it("arma /cambiar-contrasena y /cuenta/mfa con la ruta de vuelta", () => {
        expect(rutaCambiarContrasena("/")).toBe("/cambiar-contrasena");
        expect(rutaCambiarContrasena("/usuarios")).toBe("/cambiar-contrasena?siguiente=%2Fusuarios");
        expect(rutaCambiarContrasena("/campo")).toBe("/cambiar-contrasena?siguiente=%2Fcampo");
        expect(rutaConfigurarMfa("/ordenes/5")).toBe("/cuenta/mfa?siguiente=%2Fordenes%2F5");
        expect(rutaConfigurarMfa("/")).toBe("/cuenta/mfa");
    });

    it("reconoce la app de campo y la contraseña temporal", () => {
        expect(esRutaCampo("/campo")).toBe(true);
        expect(esRutaCampo("/campo/orden/3")).toBe(true);
        expect(esRutaCampo("/campos")).toBe(false);
        expect(requiereCambio({ cambioContrasenaRequerido: true })).toBe(true);
        expect(requiereCambio({})).toBe(false);
        expect(requiereCambio(undefined)).toBe(false);
    });

    it("«Reiniciar MFA» solo con el permiso, sobre otro y con MFA activo", () => {
        expect(puedeReiniciarMfa(["usuarios.reiniciar_mfa"], 1, { id: 2, mfaActivo: true })).toBe(true);
        expect(puedeReiniciarMfa(["usuarios.reiniciar_mfa"], 2, { id: 2, mfaActivo: true })).toBe(false);
        expect(puedeReiniciarMfa(["usuarios.reiniciar_mfa"], 1, { id: 2, mfaActivo: false })).toBe(false);
        expect(puedeReiniciarMfa(["usuarios.editar", "usuarios.ver"], 1, { id: 2, mfaActivo: true })).toBe(false);
    });
});

describe("MFA propio", () => {
    it("agrupa el secreto en bloques de 4", () => {
        expect(agruparSecreto("JBSWY3DPEHPK3PXP")).toBe("JBSW Y3DP EHPK 3PXP");
        expect(agruparSecreto("ABCDE")).toBe("ABCD E");
    });

    it("dibuja un cuadrado por módulo oscuro del QR", () => {
        expect(
            trazoQr([
                [true, false],
                [false, true],
            ]),
        ).toBe("M0 0h1v1h-1zM1 1h1v1h-1z");
    });
});
