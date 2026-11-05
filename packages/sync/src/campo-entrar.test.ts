// Entrada del técnico (PWA y app nativa): usuario recordado, mensajes de error, intentos solo si la
// API los informa, cuándo se ofrece la huella y el aviso «Sin conexión».
import { describe, expect, it } from "vitest";
import { avisoEntrar, avisoSinConexion, intentosRestantes, leerUsuarioRecordado, ofrecerHuella, textoUsuarioRecordado } from "./campo";

describe("usuario recordado", () => {
    it("guarda solo nombre y correo y lee lo válido", () => {
        const texto = textoUsuarioRecordado({ nombre: "Tomás Herrera", correo: " Tomas.Herrera@Redcom.gt " });
        expect(JSON.parse(texto)).toEqual({ nombre: "Tomás Herrera", correo: "tomas.herrera@redcom.gt" });
        expect(leerUsuarioRecordado(texto)).toEqual({ nombre: "Tomás Herrera", correo: "tomas.herrera@redcom.gt" });
        expect(leerUsuarioRecordado(null)).toBeNull();
        expect(leerUsuarioRecordado("{roto")).toBeNull();
        expect(leerUsuarioRecordado(JSON.stringify({ nombre: "x" }))).toBeNull();
        expect(leerUsuarioRecordado(JSON.stringify({ nombre: "", correo: "a@b.gt" }))).toEqual({ nombre: "a@b.gt", correo: "a@b.gt" });
    });
});

describe("avisoEntrar", () => {
    it("contraseña incorrecta va en línea; los intentos solo si la API los dice", () => {
        expect(avisoEntrar({ estado: 401, codigo: "credenciales_invalidas" })).toEqual({ tono: "error", titulo: "Correo o contraseña incorrectos.", campo: "contrasena" });
        expect(avisoEntrar({ estado: 401, codigo: "credenciales_invalidas", problema: { intentosRestantes: 4 } }).titulo).toBe("Correo o contraseña incorrectos. Te quedan 4 intentos.");
        expect(avisoEntrar({ estado: 401, codigo: "credenciales_invalidas", problema: { intentos_restantes: 1 } }).titulo).toBe("Correo o contraseña incorrectos. Te queda 1 intento.");
        expect(intentosRestantes({ intentosRestantes: "4" })).toBeNull();
        expect(intentosRestantes({ intentosRestantes: -1 })).toBeNull();
        expect(intentosRestantes(null)).toBeNull();
    });

    it("código, sesión y red", () => {
        expect(avisoEntrar({ estado: 401, codigo: "codigo_invalido" }).campo).toBe("codigo");
        expect(avisoEntrar({ estado: 401, codigo: "mfa_bloqueado" })).toMatchObject({ campo: null, titulo: "Demasiados códigos inválidos" });
        expect(avisoEntrar({ estado: 401, codigo: "no_autenticado" })).toMatchObject({ tono: "warning", campo: null });
        expect(avisoEntrar({ estado: 0 })).toMatchObject({ titulo: "No pudimos contactar a Iris", campo: null });
        expect(avisoEntrar({ estado: 500, titulo: "Error interno", detalle: "x" })).toEqual({ tono: "error", titulo: "Error interno", detalle: "x", campo: null });
    });
});

describe("huella y sin conexión", () => {
    const recordado = { nombre: "Tomás", correo: "tomas@redcom.gt" };
    it("solo con biometría, sesión guardada con su token y del mismo usuario recordado", () => {
        const base = { biometria: true, sesionGuardada: { correo: "Tomas@redcom.gt" }, tokenGuardado: true, recordado };
        expect(ofrecerHuella(base)).toBe(true);
        expect(ofrecerHuella({ ...base, biometria: false })).toBe(false);
        expect(ofrecerHuella({ ...base, sesionGuardada: null })).toBe(false);
        expect(ofrecerHuella({ ...base, tokenGuardado: false })).toBe(false);
        expect(ofrecerHuella({ ...base, recordado: null })).toBe(false);
        expect(ofrecerHuella({ ...base, recordado: { nombre: "Ana", correo: "ana@redcom.gt" } })).toBe(false);
    });

    it("el aviso dice si se entra con la sesión guardada o hay que conectarse", () => {
        expect(avisoSinConexion(true).detalle).toBe("Entrarás con la sesión guardada en este teléfono.");
        expect(avisoSinConexion(false).detalle).toMatch(/^Conéctate para entrar/);
    });
});
