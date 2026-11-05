// «Yo» (diseño aprobado por el PM, nov 2026) en nativo, en claro y oscuro: encabezado con el rol y la
// empresa de `GET /yo`, Apariencia (Teléfono · Claro · Oscuro, guardada y aplicada al instante),
// «Cambiar contraseña» (validación espejo de la API, 422 por campo, 429 que cierra la sesión, sin
// señal, salir con cambios pregunta) y la verificación en dos pasos completa (`POST /yo/mfa` una sola
// vez por intento, «Abrir app», la clave en grupos de 4 y `activar`). API simulada con los tipos de
// `@iris/contratos`.
import type { Schema } from "@iris/contratos";
import { CLAVE_APARIENCIA } from "@iris/sync/campo";
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import { BackHandler, Linking } from "react-native";
import { Uniwind } from "uniwind";
import { entorno } from "../src/entorno";
import { coloresCampo, CLARO, OSCURO } from "../src/tema/tokens";
import { ApiFalsa, problema, usuario, yo } from "./api-falsa";
import { abrirCampo, avisoArriba } from "./arnes";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));
jest.mock("../src/pantallas/orden/LienzoFirma", () => jest.requireActual("./firma-falsa"));

const SECRETO = "JBSWY3DPEHPK3PXP";
const URL_OTP = `otpauth://totp/Iris:ana@isp.gt?algorithm=SHA1&digits=6&issuer=Iris&period=30&secret=${SECRETO}`;

/** `GET /yo` de una técnica de «ISP Norte» (con o sin MFA). */
function yoTecnica(mfaActivo: boolean): Schema<"Yo"> {
    const y = yo();
    return { ...y, usuario: { ...usuario(), mfaActivo, roles: [{ rol: "tecnico", cuadrillaId: 4 }] } };
}

function api(mfaActivo = false): ApiFalsa & { estado: { mfa: boolean } } {
    const estado = { mfa: mfaActivo };
    const a = new ApiFalsa().en("GET", "/api/v1/yo", () => ({ status: 200, cuerpo: yoTecnica(estado.mfa) })).instalar();
    return Object.assign(a, { estado });
}

const lienzo = (oscuro: boolean) => coloresCampo(oscuro ? OSCURO : CLARO, oscuro).lienzo;

/** Captura el «Atrás» de Android: llama al último oyente registrado (el de la pantalla actual). */
function atrasAndroid(): () => Promise<boolean> {
    const espia = jest.spyOn(BackHandler, "addEventListener");
    return async () => {
        const llamadas = espia.mock.calls.filter((c) => c[0] === "hardwareBackPress");
        let r = false;
        await act(async () => {
            for (const [, fn] of [...llamadas].reverse()) {
                if (fn({} as never) === true) {
                    r = true;
                    return;
                }
            }
        });
        return r;
    };
}

afterEach(() => {
    jest.restoreAllMocks();
    entorno().claves.borrar(CLAVE_APARIENCIA);
});

describe.each(["light", "dark"] as const)("«Yo» (%s)", (esquema) => {
    const oscuro = esquema === "dark";

    it("encabezado con «Rol · Empresa», secciones, punto de dos pasos y «Cerrar sesión»", async () => {
        api(false);
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        expect(within(screen.getByTestId("perfil-yo")).getByRole("header", { name: "Téc. Ana" })).toBeTruthy();
        expect(await screen.findByText("Técnico · ISP Norte")).toBeTruthy();
        for (const t of ["APARIENCIA", "SEGURIDAD", "ESTE TELÉFONO"]) {
            expect(screen.getByRole("header", { name: t })).toBeTruthy();
        }
        expect(await screen.findByRole("button", { name: "Verificación en dos pasos: Apagada" })).toBeTruthy();
        expect(within(screen.getByTestId("estado-dos-pasos")).getByText("Apagada")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Cambiar contraseña" })).toBeTruthy();
        // No hay «Cambiada hace…»: la API no da ese dato.
        expect(screen.queryByText(/Cambiada hace/)).toBeNull();
        expect(screen.getByText("Todo enviado")).toBeTruthy();
        expect(screen.getByRole("button", { name: "Mi bodega" })).toBeTruthy();
        expect(screen.getByRole("button", { name: "Cerrar sesión" })).toBeTruthy();
    });

    it("dos pasos activa: punto verde «Activa»", async () => {
        api(true);
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        expect(await screen.findByRole("button", { name: "Verificación en dos pasos: Activa" })).toBeTruthy();
        expect(screen.getByText("Te pide un código al entrar")).toBeTruthy();
    });

    it("sin poder leer /yo no muestra un estado de dos pasos inventado", async () => {
        new ApiFalsa().en("GET", "/api/v1/yo", () => problema(500, "interno")).instalar();
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        expect(await screen.findByText("No se pudo leer si está activa.")).toBeTruthy();
        expect(screen.queryByTestId("estado-dos-pasos")).toBeNull();
        expect(screen.queryByText("Técnico · ISP Norte")).toBeNull();
    });

    it("Apariencia: los tres modos se aplican al instante y se guardan en el teléfono", async () => {
        api();
        const tema = jest.spyOn(Uniwind, "setTheme");
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        const grupo = screen.getByTestId("apariencia");
        expect(grupo.props.accessibilityRole).toBe("radiogroup");
        expect(within(grupo).getAllByRole("radio").map((r) => r.props.accessibilityLabel)).toEqual(["Teléfono", "Claro", "Oscuro"]);
        expect(screen.getByRole("radio", { name: "Teléfono" })).toBeChecked();
        expect(screen.getByTestId("franja-superior")).toHaveStyle({ backgroundColor: lienzo(oscuro) });

        await fireEvent.press(screen.getByRole("radio", { name: "Oscuro" }));
        expect(screen.getByRole("radio", { name: "Oscuro" })).toBeChecked();
        expect(screen.getByTestId("franja-superior")).toHaveStyle({ backgroundColor: lienzo(true) });
        expect(entorno().claves.leer(CLAVE_APARIENCIA)).toBe("oscuro");
        expect(tema).toHaveBeenLastCalledWith("dark");

        await fireEvent.press(screen.getByRole("radio", { name: "Claro" }));
        expect(screen.getByTestId("franja-superior")).toHaveStyle({ backgroundColor: lienzo(false) });
        expect(entorno().claves.leer(CLAVE_APARIENCIA)).toBe("claro");
        expect(tema).toHaveBeenLastCalledWith("light");

        // «Teléfono» vuelve a seguir al sistema (aquí, el esquema simulado).
        await fireEvent.press(screen.getByRole("radio", { name: "Teléfono" }));
        expect(screen.getByTestId("franja-superior")).toHaveStyle({ backgroundColor: lienzo(oscuro) });
        expect(entorno().claves.leer(CLAVE_APARIENCIA)).toBe("telefono");
        expect(tema).toHaveBeenLastCalledWith(esquema);
    });

    it("la apariencia guardada manda al abrir, sobre el tema del teléfono", async () => {
        api();
        entorno().claves.escribir(CLAVE_APARIENCIA, oscuro ? "claro" : "oscuro");
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        expect(screen.getByRole("radio", { name: oscuro ? "Claro" : "Oscuro" })).toBeChecked();
        expect(screen.getByTestId("franja-superior")).toHaveStyle({ backgroundColor: lienzo(!oscuro) });
    });

    it("cambiar contraseña: pantalla propia, ayudas en verde, espejo de la API y aviso al guardar", async () => {
        const a = api();
        let cuerpo: unknown = null;
        a.en("POST", "/api/v1/yo/contrasena", (p) => {
            cuerpo = p.cuerpo;
            return { status: 200, cuerpo: usuario() };
        });
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        await fireEvent.press(screen.getByRole("button", { name: "Cambiar contraseña" }));
        expect(await screen.findByRole("header", { name: "Cambiar contraseña" })).toBeTruthy();
        expect(screen.getByText("Se cierran tus sesiones en otros equipos. En este teléfono sigues dentro.")).toBeTruthy();

        // Vacío: errores en línea, sin llamar a la API.
        await fireEvent.press(screen.getByTestId("guardar-contrasena"));
        expect(await screen.findByText("Escribe tu contraseña actual")).toBeTruthy();
        expect(screen.getByText("Usa 12 caracteres o más")).toBeTruthy();
        expect(a.peticiones.some((p) => p.ruta === "/api/v1/yo/contrasena")).toBe(false);

        await fireEvent.changeText(screen.getByTestId("campo-actual"), "vieja-de-12-caracteres");
        await fireEvent.changeText(screen.getByTestId("campo-nueva"), "corta");
        expect(screen.queryByTestId("ayuda-largo")).toBeNull();
        // Igual a la actual: «Debe ser distinta».
        await fireEvent.changeText(screen.getByTestId("campo-nueva"), "vieja-de-12-caracteres");
        expect(await screen.findByText("Debe ser distinta de la actual")).toBeTruthy();
        await fireEvent.changeText(screen.getByTestId("campo-nueva"), "una frase bastante larga");
        expect(await screen.findByTestId("ayuda-largo")).toHaveTextContent("12 caracteres o más");
        await fireEvent.changeText(screen.getByTestId("campo-repetir"), "una frase bastante");
        expect(await screen.findByText("No coincide con la nueva")).toBeTruthy();
        await fireEvent.changeText(screen.getByTestId("campo-repetir"), "una frase bastante larga");
        expect(await screen.findByTestId("ayuda-coinciden")).toHaveTextContent("Coinciden");
        // El ojo muestra lo escrito.
        await fireEvent.press(screen.getAllByRole("button", { name: "Mostrar contraseña" })[0] as never);
        expect(screen.getByTestId("campo-actual").props.secureTextEntry).toBe(false);

        await fireEvent.press(screen.getByTestId("guardar-contrasena"));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent("Contraseña cambiada"));
        expect(cuerpo).toEqual({ actual: "vieja-de-12-caracteres", nueva: "una frase bastante larga" });
        const post = a.peticiones.find((p) => p.ruta === "/api/v1/yo/contrasena");
        expect(post?.cabeceras.get("Idempotency-Key")).toMatch(/^[0-9a-f-]{36}$/);
        expect(post?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        // Vuelve a «Yo». Nada fue a la cola sin conexión.
        expect(await screen.findByTestId("apariencia")).toBeTruthy();
        expect(m.motor.obtenerEstado().cola).toHaveLength(0);
    });

    it("422 de la contraseña actual va a su campo (y lo vacía)", async () => {
        const a = api();
        a.en("POST", "/api/v1/yo/contrasena", () => ({
            status: 422,
            cuerpo: { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "actual", message: "La contraseña actual no es correcta" }] } satisfies Schema<"Problema">,
            cabeceras: { "Content-Type": "application/problem+json" },
        }));
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo", seccion: "contrasena" } });
        await fireEvent.changeText(screen.getByTestId("campo-actual"), "equivocada");
        await fireEvent.changeText(screen.getByTestId("campo-nueva"), "una frase bastante larga");
        await fireEvent.changeText(screen.getByTestId("campo-repetir"), "una frase bastante larga");
        await fireEvent.press(screen.getByTestId("guardar-contrasena"));
        expect(await screen.findByText("La contraseña actual no es correcta")).toBeTruthy();
        expect(screen.getByTestId("campo-actual").props.value).toBe("");
    });

    it("429 `demasiados_intentos`: explica la espera y lleva a entrar (la API cerró la sesión)", async () => {
        const a = api();
        a.en("POST", "/api/v1/yo/contrasena", () => ({ ...problema(429, "demasiados_intentos", "Demasiados intentos"), cabeceras: { "Content-Type": "application/problem+json", "Retry-After": "900" } }));
        const cerrarSesion = jest.fn(async () => undefined);
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo", seccion: "contrasena" }, cerrarSesion });
        await fireEvent.changeText(screen.getByTestId("campo-actual"), "equivocada");
        await fireEvent.changeText(screen.getByTestId("campo-nueva"), "una frase bastante larga");
        await fireEvent.changeText(screen.getByTestId("campo-repetir"), "una frase bastante larga");
        await fireEvent.press(screen.getByTestId("guardar-contrasena"));
        const aviso = await screen.findByTestId("aviso-sesion-cerrada");
        expect(aviso).toHaveTextContent(/Demasiados intentos: vuelve a entrar en 15 min/);
        // El formulario ya no está: con esta sesión no hay nada más que hacer.
        expect(screen.queryByTestId("guardar-contrasena")).toBeNull();
        await fireEvent.press(within(aviso).getByRole("button", { name: "Ir a iniciar sesión" }));
        expect(cerrarSesion).toHaveBeenCalledWith({ conservarPendientes: true });
    });

    it("sin señal: lo dice y no intenta (no va a la cola); si se cae al enviar, «Reintentar»", async () => {
        const a = api();
        let intentos = 0;
        a.en("POST", "/api/v1/yo/contrasena", () => {
            intentos += 1;
            return { status: 200, cuerpo: usuario() };
        });
        const m = await montar();
        m.transporte.sinRed = true;
        m.motor.establecerConexion(false);
        await abrirCampo(m, { esquema, vista: { tipo: "yo", seccion: "contrasena" } });
        expect(await screen.findByTestId("aviso-sin-senal")).toHaveTextContent(/necesita señal/);
        expect(screen.getByTestId("guardar-contrasena").props.accessibilityState).toMatchObject({ disabled: true });

        m.transporte.sinRed = false;
        await act(async () => m.motor.establecerConexion(true));
        await waitFor(() => expect(screen.queryByTestId("aviso-sin-senal")).toBeNull());
        await fireEvent.changeText(screen.getByTestId("campo-actual"), "vieja");
        await fireEvent.changeText(screen.getByTestId("campo-nueva"), "una frase bastante larga");
        await fireEvent.changeText(screen.getByTestId("campo-repetir"), "una frase bastante larga");
        a.sinRed = true;
        await fireEvent.press(screen.getByTestId("guardar-contrasena"));
        const aviso = await screen.findByTestId("aviso-error-cuenta");
        expect(aviso).toHaveTextContent(/Sin señal/);
        const claves = a.peticiones.filter((p) => p.ruta === "/api/v1/yo/contrasena").map((p) => p.cabeceras.get("Idempotency-Key"));
        a.sinRed = false;
        await fireEvent.press(within(aviso).getByRole("button", { name: "Reintentar" }));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent("Contraseña cambiada"));
        expect(intentos).toBe(1);
        // Sin respuesta, el reintento usa la misma clave (la API no llegó a guardar nada).
        const todas = a.peticiones.filter((p) => p.ruta === "/api/v1/yo/contrasena").map((p) => p.cabeceras.get("Idempotency-Key"));
        expect(new Set([...claves, ...todas]).size).toBe(1);
        expect(m.motor.obtenerEstado().cola).toHaveLength(0);
    });

    it("salir con cambios pregunta (botón «Atrás» y «Atrás» de Android); sin cambios vuelve directo", async () => {
        api();
        const atras = atrasAndroid();
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        await fireEvent.press(await screen.findByRole("button", { name: "Cambiar contraseña" }));
        await fireEvent.press(await screen.findByRole("button", { name: "Atrás" }));
        expect(await screen.findByTestId("apariencia")).toBeTruthy();

        await fireEvent.press(screen.getByRole("button", { name: "Cambiar contraseña" }));
        await fireEvent.changeText(await screen.findByTestId("campo-actual"), "algo");
        await fireEvent.press(screen.getByRole("button", { name: "Atrás" }));
        expect(await screen.findByText("¿Salir sin guardar?")).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Seguir editando" }));
        expect(screen.getByTestId("campo-actual").props.value).toBe("algo");

        expect(await atras()).toBe(true);
        expect(await screen.findByText("¿Salir sin guardar?")).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Salir sin guardar" }));
        expect(await screen.findByTestId("apariencia")).toBeTruthy();
    });

    it("dos pasos: flujo completo (una sola clave por intento, «Abrir app», código y activar)", async () => {
        const a = api(false);
        const claves: (string | null)[] = [];
        a.en("POST", "/api/v1/yo/mfa", (p) => {
            claves.push(p.cabeceras.get("Idempotency-Key"));
            // El primero falla: «Reintentar» pide otro con una clave nueva.
            return claves.length === 1 ? problema(500, "interno", "Error interno") : { status: 200, cuerpo: { secreto: SECRETO, url: URL_OTP } satisfies Schema<"MFAConfigurado"> };
        });
        let codigos = 0;
        a.en("POST", "/api/v1/yo/mfa/activar", (p) => {
            codigos += 1;
            const { codigo } = p.cuerpo as Schema<"EntradaActivarMFA">;
            if (codigo !== "482913") {
                return {
                    status: 422,
                    cuerpo: { type: "urn:iris:problema:codigo_invalido", title: "Regla de negocio", status: 422, detail: "El código no coincide; revisa la hora del teléfono" } satisfies Schema<"Problema">,
                    cabeceras: { "Content-Type": "application/problem+json" },
                };
            }
            a.estado.mfa = true;
            return { status: 200, cuerpo: { ...usuario(), mfaActivo: true } };
        });
        const abrir = jest.spyOn(Linking, "openURL").mockResolvedValue(undefined);
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        await fireEvent.press(await screen.findByRole("button", { name: "Verificación en dos pasos: Apagada" }));
        expect(await screen.findByRole("header", { name: "Verificación en dos pasos" })).toBeTruthy();

        const error = await screen.findByTestId("aviso-error-cuenta");
        expect(error).toHaveTextContent(/No se pudo preparar la verificación/);
        await fireEvent.press(within(error).getByRole("button", { name: "Reintentar" }));
        expect(await screen.findByTestId("clave-dos-pasos")).toHaveTextContent("JBSW Y3DP EHPK 3PXP");
        expect(claves).toHaveLength(2);
        expect(claves[0]).not.toBe(claves[1]);

        await fireEvent.press(screen.getByRole("button", { name: "Abrir app" }));
        expect(abrir).toHaveBeenCalledWith(URL_OTP);
        expect(screen.getByRole("button", { name: "Copiar clave" })).toBeTruthy();
        expect(screen.getByText(/Desde ahora, al entrar en un equipo nuevo te pide este código/)).toBeTruthy();

        // Código equivocado: error en línea y el campo se vacía.
        await fireEvent.changeText(screen.getByTestId("campo-codigo"), "111111");
        await fireEvent.press(screen.getByTestId("activar-dos-pasos"));
        expect(await screen.findByText("El código no coincide; revisa la hora del teléfono")).toBeTruthy();
        await fireEvent.changeText(screen.getByTestId("campo-codigo"), "482913");
        await fireEvent.press(screen.getByTestId("activar-dos-pasos"));
        await waitFor(() => expect(avisoArriba()).toHaveTextContent("Verificación en dos pasos activada"));
        expect(codigos).toBe(2);
        // Ningún render volvió a pedir el secreto.
        expect(claves).toHaveLength(2);
        // De vuelta en «Yo», ya activa.
        expect(await screen.findByRole("button", { name: "Verificación en dos pasos: Activa" })).toBeTruthy();
    });

    it("dos pasos activa: solo informa (no hay desactivar) y no pide un secreto", async () => {
        const a = api(true);
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo", seccion: "dos_pasos" } });
        expect(await screen.findByTestId("dos-pasos-activa")).toBeTruthy();
        expect(screen.getByText("Para cambiar de teléfono, pide a un administrador que la reinicie.")).toBeTruthy();
        expect(screen.queryByRole("button", { name: /Desactivar/ })).toBeNull();
        expect(a.peticiones.some((p) => p.ruta === "/api/v1/yo/mfa")).toBe(false);
    });
});
