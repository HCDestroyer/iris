import { describe, expect, it } from "vitest";
import { pasoConError } from "./pasos";

const error = { type: "servidor", message: "x" } as const;

describe("pasoConError", () => {
    it("sin errores no cambia de paso", () => {
        expect(pasoConError({})).toBeNull();
    });

    it("un error de entrada, verificación o tickets nuevos lleva a «Recibir» (aunque también haya de SMTP)", () => {
        expect(pasoConError({ imapHost: error })).toBe("recibir");
        expect(pasoConError({ authservId: error })).toBe("recibir");
        expect(pasoConError({ smtpHost: error, nombre: error })).toBe("recibir");
    });

    it("solo errores de SMTP llevan a «Enviar»", () => {
        expect(pasoConError({ smtpHost: error, smtpContrasena: error })).toBe("enviar");
    });
});
