import { describe, expect, it } from "vitest";
import { esquemaCodigoMFA, esquemaInicioSesion } from "./esquemas";

describe("esquemaInicioSesion (espejo de EntradaIniciarSesion)", () => {
    it("acepta un correo y contraseña válidos y recorta espacios del correo", () => {
        expect(esquemaInicioSesion.parse({ correo: "  ana@iris.test ", contrasena: "secreta" })).toEqual({
            correo: "ana@iris.test",
            contrasena: "secreta",
        });
    });

    it("exige correo con @, y contraseña de 1 a 200", () => {
        const r = esquemaInicioSesion.safeParse({ correo: "ana", contrasena: "" });
        expect(r.success).toBe(false);
        const campos = r.error?.issues.map((i) => i.path.join(".")).sort();
        expect(campos).toEqual(["contrasena", "correo"]);
        expect(esquemaInicioSesion.safeParse({ correo: "a@b", contrasena: "x".repeat(201) }).success).toBe(false);
        expect(esquemaInicioSesion.safeParse({ correo: "a@b", contrasena: "x".repeat(200) }).success).toBe(true);
    });

    it("limita el correo a 254 caracteres", () => {
        const largo = `${"a".repeat(250)}@b.cd`;
        expect(esquemaInicioSesion.safeParse({ correo: largo, contrasena: "x" }).success).toBe(false);
    });
});

describe("esquemaCodigoMFA (espejo de EntradaVerificarMFA)", () => {
    it("exige 6 dígitos y quita espacios", () => {
        expect(esquemaCodigoMFA.parse({ codigo: "123 456" })).toEqual({ codigo: "123456" });
        expect(esquemaCodigoMFA.safeParse({ codigo: "12345" }).success).toBe(false);
        expect(esquemaCodigoMFA.safeParse({ codigo: "12345a" }).success).toBe(false);
        expect(esquemaCodigoMFA.safeParse({ codigo: "1234567" }).success).toBe(false);
    });
});
