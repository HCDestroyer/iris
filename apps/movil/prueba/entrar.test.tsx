// Inicio de sesión con MFA (mismo flujo que la PWA): el CSRF del cuerpo va al llavero y sale en
// las peticiones que cambian estado; 429 bloquea con cuenta regresiva.
import type { Schema } from "@iris/contratos";
import { fireEvent, render, screen, waitFor } from "@testing-library/react-native";
import { entorno } from "../src/entorno";
import { Entrar } from "../src/pantallas/Entrar";
import { CLAVE_CSRF, leerCsrf, verificarDueno } from "../src/sesion/sesion";
import { ProveedorTema } from "../src/tema/tema";
import { ApiFalsa, problema, usuario, yo } from "./api-falsa";

const sesion = (csrfToken: string, mfaRequerido: boolean): Schema<"SesionIniciada"> => ({
    csrfToken,
    mfaRequerido,
    expiraEn: "2026-10-29T15:00:00Z",
    ...(mfaRequerido ? {} : { usuario: usuario() }),
});

async function pantalla(alEntrar = jest.fn()) {
    await render(
        <ProveedorTema esquema="dark">
            <Entrar alEntrar={alEntrar} />
        </ProveedorTema>,
    );
    return alEntrar;
}

async function escribirCredenciales() {
    await fireEvent.changeText(screen.getByLabelText("Correo"), "ana@isp.gt");
    await fireEvent.changeText(screen.getByLabelText("Contraseña"), "secreta");
    await fireEvent.press(screen.getByRole("button", { name: /Entrar/ }));
}

describe("Entrar", () => {
    it("correo y contraseña → código TOTP → sesión; el CSRF nuevo queda en el llavero y viaja en la cabecera", async () => {
        const api = new ApiFalsa()
            .en("POST", "/api/v1/sesion", () => ({ status: 200, cuerpo: sesion("csrf-pendiente", true) }))
            .en("POST", "/api/v1/sesion/mfa", () => ({ status: 200, cuerpo: sesion("csrf-final", false) }))
            .instalar();
        const alEntrar = await pantalla();
        await escribirCredenciales();
        expect(await screen.findByText("Verificación en dos pasos")).toBeTruthy();
        expect(api.peticiones[0]?.cuerpo).toEqual({ correo: "ana@isp.gt", contrasena: "secreta" });

        await fireEvent.changeText(screen.getByLabelText("Código de verificación"), "123 456");
        await fireEvent.press(screen.getByRole("button", { name: /Verificar/ }));
        await waitFor(() => expect(alEntrar).toHaveBeenCalledWith(expect.objectContaining({ id: 5 })));
        const mfa = api.peticiones[1];
        expect(mfa?.cuerpo).toEqual({ codigo: "123456" });
        expect(mfa?.cabeceras.get("X-CSRF-Token")).toBe("csrf-pendiente");
        expect(mfa?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        expect(leerCsrf()).toBe("csrf-final");
        expect(await entorno().secreto.leer(CLAVE_CSRF)).toBe("csrf-final");
    });

    it("código incorrecto: lo dice en el campo y deja reintentar", async () => {
        new ApiFalsa()
            .en("POST", "/api/v1/sesion", () => ({ status: 200, cuerpo: sesion("c1", true) }))
            .en("POST", "/api/v1/sesion/mfa", () => problema(401, "codigo_invalido"))
            .instalar();
        const alEntrar = await pantalla();
        await escribirCredenciales();
        await fireEvent.changeText(await screen.findByLabelText("Código de verificación"), "000000");
        await fireEvent.press(screen.getByRole("button", { name: /Verificar/ }));
        expect(await screen.findByText(/Código incorrecto o ya usado/)).toBeTruthy();
        expect(alEntrar).not.toHaveBeenCalled();
    });

    it("credenciales inválidas y validación local (zod) antes de llamar a la API", async () => {
        const api = new ApiFalsa().en("POST", "/api/v1/sesion", () => problema(401, "credenciales_invalidas")).instalar();
        await pantalla();
        await fireEvent.press(screen.getByRole("button", { name: /Entrar/ }));
        expect(await screen.findByText("Escribe tu correo")).toBeTruthy();
        expect(api.peticiones).toHaveLength(0);
        await escribirCredenciales();
        expect(await screen.findByText("Correo o contraseña incorrectos")).toBeTruthy();
    });

    it("429: bloquea el botón con la espera de Retry-After", async () => {
        new ApiFalsa().en("POST", "/api/v1/sesion", () => ({ ...problema(429, "demasiados_intentos"), cabeceras: { "Retry-After": "120", "Content-Type": "application/problem+json" } })).instalar();
        await pantalla();
        await escribirCredenciales();
        expect(await screen.findByText("Demasiados intentos, espera 2 min")).toBeTruthy();
        expect(screen.getByRole("button", { name: /Entrar/ })).toBeDisabled();
    });

    it("sin señal: lo explica", async () => {
        const api = new ApiFalsa().instalar();
        api.sinRed = true;
        await pantalla();
        await escribirCredenciales();
        expect(await screen.findByText("No pudimos contactar a Iris")).toBeTruthy();
    });
});

describe("verificarDueno (antes de cada envío del motor)", () => {
    it("con la sesión de otra persona lanza sesion_ajena y no deja salir nada", async () => {
        new ApiFalsa().en("GET", "/api/v1/yo", () => ({ status: 200, cuerpo: yo(9) })).instalar();
        await expect(verificarDueno(5, 1)).rejects.toMatchObject({ codigo: "sesion_ajena" });
    });
    it("con la del dueño y su empresa pasa (y manda la empresa en la cabecera)", async () => {
        const api = new ApiFalsa().en("GET", "/api/v1/yo", () => ({ status: 200, cuerpo: yo(5) })).instalar();
        await expect(verificarDueno(5, 1)).resolves.toBeUndefined();
        expect(api.peticiones[0]?.cabeceras.get("X-Iris-Empresa")).toBe("1");
    });
    it("sin señal es error de red (el motor reintenta)", async () => {
        const api = new ApiFalsa().instalar();
        api.sinRed = true;
        await expect(verificarDueno(5, 1)).rejects.toMatchObject({ tipo: "red" });
    });
});
