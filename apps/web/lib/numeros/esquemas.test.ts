import { describe, expect, it } from "vitest";
import { esquemaCambiarToken, esquemaCredenciales, esquemaNuevoNumero, normalizarNumero } from "./esquemas";

const VALIDO = {
    nombre: "Soporte técnico",
    numero: "+50222000040",
    phoneNumberId: "109876543210987",
    wabaId: "",
    token: "EAAG".padEnd(40, "x"),
    appSecret: "0123456789abcdef",
    usos: ["soporte"] as ("soporte" | "cobros" | "ventas" | "avisos")[],
    avisosEmpresa: false,
    colaId: "2",
};

function errores(r: { success: boolean; error?: { issues: { path: PropertyKey[]; message: string }[] } }) {
    const salida: Record<string, string> = {};
    for (const i of r.error?.issues ?? []) salida[i.path.join(".")] ??= i.message;
    return salida;
}

describe("Agregar número (espejo de EntradaCrearCanal)", () => {
    it("acepta lo válido y normaliza el número", () => {
        const r = esquemaNuevoNumero.safeParse({ ...VALIDO, numero: "+502 2200-0040", token: `  ${VALIDO.token}\n` });
        expect(r.success).toBe(true);
        expect(r.data?.numero).toBe("+50222000040");
        expect(r.data?.token).toBe(VALIDO.token);
    });

    it("número E.164: con + y de 8 a 15 dígitos", () => {
        expect(normalizarNumero("+502 (2200) 00-40")).toBe("+50222000040");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, numero: "22000040" })).numero).toMatch(/E\.164/);
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, numero: "+0222000040" })).numero).toMatch(/E\.164/);
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, numero: "" })).numero).toBe("Escribe el número");
    });

    it("ids de Meta solo con dígitos; el WABA ID es opcional", () => {
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, phoneNumberId: "10987-65" })).phoneNumberId).toBe("Solo números (hasta 30 dígitos)");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, phoneNumberId: "" })).phoneNumberId).toBe("Pega el Phone number ID");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, wabaId: "abc" })).wabaId).toBe("Solo números (hasta 30 dígitos)");
        expect(esquemaNuevoNumero.safeParse({ ...VALIDO, wabaId: "102345678901234" }).success).toBe(true);
    });

    it("token de 20 a 1024 y app secret de 16 a 256", () => {
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, token: "corto" })).token).toBe("El token tiene entre 20 y 1024 caracteres");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, token: "x".repeat(1025) })).token).toBe("El token tiene entre 20 y 1024 caracteres");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, token: "" })).token).toBe("Pega el token de acceso");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, appSecret: "x".repeat(15) })).appSecret).toBe("El app secret tiene entre 16 y 256 caracteres");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, appSecret: "x".repeat(257) })).appSecret).toBe("El app secret tiene entre 16 y 256 caracteres");
        expect(esquemaNuevoNumero.safeParse({ ...VALIDO, token: "x".repeat(1024), appSecret: "x".repeat(256) }).success).toBe(true);
    });

    it("al menos un uso, y el número de avisos pide el uso «Avisos»", () => {
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, usos: [] })).usos).toBe("Elige al menos un uso");
        expect(errores(esquemaNuevoNumero.safeParse({ ...VALIDO, avisosEmpresa: true })).avisosEmpresa).toBe("Marca también el uso «Avisos»");
        expect(esquemaNuevoNumero.safeParse({ ...VALIDO, avisosEmpresa: true, usos: ["avisos"] }).success).toBe(true);
    });
});

describe("Credenciales: vacío = se queda el guardado", () => {
    const base = { nombre: "Avisos", numero: "+50222000005", phoneNumberId: "104455667788990", wabaId: "102345678901234" };

    it("token y app secret vacíos son válidos", () => {
        expect(esquemaCredenciales.safeParse({ ...base, token: "", appSecret: "   " }).success).toBe(true);
    });

    it("si se escriben, con las mismas medidas que al crear", () => {
        expect(errores(esquemaCredenciales.safeParse({ ...base, token: "corto", appSecret: "" })).token).toBe("El token tiene entre 20 y 1024 caracteres");
        expect(errores(esquemaCredenciales.safeParse({ ...base, token: "", appSecret: "corto" })).appSecret).toBe("El app secret tiene entre 16 y 256 caracteres");
    });

    it("cambiar token en «Conectar»: el token es obligatorio, el app secret no", () => {
        expect(errores(esquemaCambiarToken.safeParse({ token: "", appSecret: "" })).token).toBe("Pega el token nuevo");
        expect(esquemaCambiarToken.safeParse({ token: "x".repeat(20), appSecret: "" }).success).toBe(true);
    });
});
