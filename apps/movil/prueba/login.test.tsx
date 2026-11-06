// Entrada del técnico (opción B del PM, 0.17), en claro y oscuro: logo centrado con «Iris Campo» y
// «Entra con tu cuenta de técnico.», «Entrar» en tinta, usuario recordado con «¿No eres tú?», el
// ojo de la contraseña, el error en línea (los intentos solo si la API los dice), «¿Olvidaste tu contraseña?», «Usar huella» solo con sesión
// guardada y biometría, el aviso «Sin conexión» y el código de 6 casillas sin «Recordar este
// teléfono» ni «código de respaldo». Y cuándo se pide la huella (`pedirHuella`).
import type { Schema } from "@iris/contratos";
import { avisoSinConexion } from "@iris/sync/campo";
import { act, fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react-native";
import { AUSENCIA_MAX_MS, ausenciaLarga, useVolverTrasAusencia } from "../src/campo/bloqueo";
import { AppState, StyleSheet, type AppStateStatus } from "react-native";
import { configurarEntorno, entorno, type Biometria } from "../src/entorno";
import { Entrar, type PropsEntrar } from "../src/pantallas/Entrar";
import {
    abrirApp,
    activarHuella,
    autenticarConHuella,
    cargarCsrf,
    CLAVE_CSRF,
    debeOfrecerHuella,
    guardarSesion,
    hayCierrePendiente,
    huellaActiva,
    leerCsrf,
    noSoyYo,
    reintentarCierrePendiente,
    marcarEntradaConContrasena,
    olvidarUsuarioRecordado,
    pedirHuella,
    recordarUsuario,
    sesionGuardada,
    usuarioRecordado,
    type SesionCampo,
} from "../src/sesion/sesion";
import { ApiFalsa, problema, usuario } from "./api-falsa";
import { abrirCampo, avisoArriba } from "./arnes";
import { montar } from "@iris/sync/pruebas";
import { crearTema } from "../src/tema/tokens";
import { TemaDePrueba } from "./raiz";

const TOMAS = { nombre: "Tomás Herrera", correo: "tomas.herrera@redcom.gt" };

const sesionIniciada = (mfaRequerido: boolean): Schema<"SesionIniciada"> => ({
    csrfToken: "c1",
    mfaRequerido,
    expiraEn: "2026-11-12T15:00:00Z",
    ...(mfaRequerido ? {} : { usuario: usuario() }),
});

const alto = (el: { props: { style?: Parameters<typeof StyleSheet.flatten>[0] } }) => (StyleSheet.flatten(el.props.style) as { minHeight?: number }).minHeight ?? 0;

describe.each(["light", "dark"] as const)("entrada del técnico (%s)", (esquema) => {
    const pantalla = async (props: Partial<PropsEntrar> = {}) => {
        const alEntrar = jest.fn();
        await render(
            <TemaDePrueba esquema={esquema}>
                <Entrar alEntrar={alEntrar} {...props} />
            </TemaDePrueba>,
        );
        return alEntrar;
    };

    it("sin recordar: logo centrado con «Iris Campo» (opción B), correo y contraseña con su teclado, el ojo y sin huella ni Argos", async () => {
        await pantalla();
        expect(within(screen.getByTestId("marca-entrar")).getByLabelText("Iris")).toBeTruthy();
        expect(StyleSheet.flatten(screen.getByTestId("marca-entrar").props.style)).toMatchObject({ alignItems: "center" });
        expect(screen.getByRole("header", { name: "Iris Campo" })).toBeTruthy();
        expect(screen.getByText("Entra con tu cuenta de técnico.")).toBeTruthy();
        expect(screen.queryByText("Hola, técnico")).toBeNull();
        // «Entrar» en tinta (la acción principal de la app de campo), con la esquina de Horus, no píldora.
        const tema = crearTema(esquema === "dark");
        expect(StyleSheet.flatten(screen.getByTestId("boton-entrar").props.style)).toMatchObject({ backgroundColor: tema.campo.tinta, borderRadius: 6 });
        // Correo y contraseña: el icono y el ojo dentro de su caja.
        expect(within(screen.getByTestId("tarjeta-entrar")).getAllByLabelText(/^(Correo|Contraseña)$/)).toHaveLength(2);
        const correo = screen.getByLabelText("Correo");
        expect(correo).toHaveProp("keyboardType", "email-address");
        expect(correo).toHaveProp("autoCapitalize", "none");
        const contrasena = screen.getByLabelText("Contraseña");
        expect(contrasena).toHaveProp("autoComplete", "current-password");
        expect(contrasena).toHaveProp("secureTextEntry", true);
        await fireEvent.press(screen.getByRole("button", { name: "Mostrar contraseña" }));
        expect(screen.getByLabelText("Contraseña")).toHaveProp("secureTextEntry", false);
        await fireEvent.press(screen.getByRole("button", { name: "Ocultar contraseña" }));
        expect(screen.getByLabelText("Contraseña")).toHaveProp("secureTextEntry", true);
        // Todo lo que se toca: 48 px o más.
        for (const nombre of [/^Entrar/, "¿Olvidaste tu contraseña?", "Mostrar contraseña"]) {
            expect(alto(screen.getByRole("button", { name: nombre }))).toBeGreaterThanOrEqual(48);
        }
        expect(screen.queryByRole("button", { name: "Usar huella" })).toBeNull();
        expect(screen.queryByRole("button", { name: /Entrar con Argos/ })).toBeNull();
        expect(screen.queryByTestId("aviso-sin-conexion")).toBeNull();
    });

    it("usuario recordado: su fila con iniciales y solo la contraseña; «¿No eres tú?» lo olvida y pide el correo", async () => {
        const olvidar = jest.fn();
        const api = new ApiFalsa().en("POST", "/api/v1/sesion", () => ({ status: 200, cuerpo: sesionIniciada(false) })).instalar();
        const alEntrar = await pantalla({ recordado: TOMAS, alOlvidarRecordado: olvidar });
        const fila = screen.getByTestId("usuario-recordado");
        expect(fila).toHaveTextContent(/TH/);
        expect(fila).toHaveTextContent(/Tomás Herrera/);
        expect(fila).toHaveTextContent(/tomas\.herrera@redcom\.gt/);
        expect(screen.queryByLabelText("Correo")).toBeNull();
        // Entra con el correo recordado y la contraseña.
        await fireEvent.changeText(screen.getByLabelText("Contraseña"), "secreta");
        await fireEvent.press(screen.getByRole("button", { name: /^Entrar/ }));
        await waitFor(() => expect(alEntrar).toHaveBeenCalled());
        expect(api.peticiones[0]?.cuerpo).toEqual({ correo: TOMAS.correo, contrasena: "secreta" });

        await fireEvent.press(within(fila).getByRole("button", { name: "¿No eres tú?" }));
        expect(olvidar).toHaveBeenCalled();
        expect(screen.queryByTestId("usuario-recordado")).toBeNull();
        expect(screen.getByLabelText("Correo")).toHaveProp("value", "");
    });

    it("error en línea bajo la contraseña; los intentos que quedan solo si la API los informa", async () => {
        let intentos: number | null = null;
        new ApiFalsa()
            .en("POST", "/api/v1/sesion", () => {
                const r = problema(401, "credenciales_invalidas");
                return intentos === null ? r : { ...r, cuerpo: { ...(r.cuerpo as object), intentosRestantes: intentos } };
            })
            .instalar();
        await pantalla({ recordado: TOMAS });
        await fireEvent.changeText(screen.getByLabelText("Contraseña"), "mala");
        await fireEvent.press(screen.getByRole("button", { name: /^Entrar/ }));
        const error = await screen.findByRole("alert");
        expect(error).toHaveTextContent("Correo o contraseña incorrectos.");
        expect(screen.queryByText(/intento/)).toBeNull();
        // Arriba no hay aviso: el error es del campo.
        expect(screen.queryByTestId("aviso-entrar")).toBeNull();

        intentos = 4;
        await fireEvent.press(screen.getByRole("button", { name: /^Entrar/ }));
        expect(await screen.findByText("Correo o contraseña incorrectos. Te quedan 4 intentos.")).toBeTruthy();
    });

    it("«¿Olvidaste tu contraseña?» dice cómo pedírsela al administrador (no hay recuperación por correo)", async () => {
        await pantalla();
        await fireEvent.press(screen.getByRole("button", { name: "¿Olvidaste tu contraseña?" }));
        expect(await screen.findByText(/contraseña temporal/)).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Cerrar" }));
        await waitFor(() => expect(screen.queryByText(/contraseña temporal/)).toBeNull());
    });

    it("«Usar huella» solo si la app la ofrece; el aviso «Sin conexión»", async () => {
        const huella = jest.fn(async () => undefined);
        await pantalla({ recordado: TOMAS, huella, sinConexion: avisoSinConexion(true) });
        const boton = screen.getByRole("button", { name: "Usar huella" });
        expect(alto(boton)).toBeGreaterThanOrEqual(48);
        await fireEvent.press(boton);
        await waitFor(() => expect(huella).toHaveBeenCalledTimes(1));
        expect(screen.getByTestId("aviso-sin-conexion")).toHaveTextContent("Sin conexión.Entrarás con la sesión guardada en este teléfono.");
    });

    it("código: 6 casillas (HeroUI InputOTP), «Verificar» y volver; sin «Recordar este teléfono» ni código de respaldo", async () => {
        const api = new ApiFalsa()
            .en("POST", "/api/v1/sesion", () => ({ status: 200, cuerpo: sesionIniciada(true) }))
            .en("POST", "/api/v1/sesion/mfa", () => problema(401, "codigo_invalido"))
            .instalar();
        await pantalla({ recordado: TOMAS });
        await fireEvent.changeText(screen.getByLabelText("Contraseña"), "secreta");
        await fireEvent.press(screen.getByRole("button", { name: /^Entrar/ }));
        expect(await screen.findByRole("header", { name: "Código de verificación" })).toBeTruthy();
        expect(screen.getByText("Solo te lo pedimos porque tienes la verificación en dos pasos activa.")).toBeTruthy();
        const codigo = screen.getByLabelText("Escribe los 6 dígitos de tu app de autenticación");
        expect(codigo).toHaveProp("maxLength", 6);
        expect(codigo).toHaveProp("autoComplete", "one-time-code");
        expect(screen.queryByText(/Recordar este teléfono/)).toBeNull();
        expect(screen.queryByText(/código de respaldo/i)).toBeNull();
        await fireEvent.changeText(codigo, "12a3456");
        await fireEvent.press(screen.getByRole("button", { name: /Verificar/ }));
        expect(await screen.findByText("Código incorrecto o ya usado. Revisa tu app de autenticación.")).toBeTruthy();
        expect(api.peticiones[1]?.cuerpo).toEqual({ codigo: "123456" });
        await fireEvent.press(screen.getByRole("button", { name: "Volver" }));
        expect(await screen.findByRole("header", { name: "Iris Campo" })).toBeTruthy();
    });
});

describe("huella: solo desbloquea la sesión guardada del usuario recordado", () => {
    const sesion: SesionCampo = { usuarioId: 5, nombre: TOMAS.nombre, correo: TOMAS.correo, empresas: [], modoEmpresa: "sin_conector", empresaActiva: null, verificadoEn: Date.now() };
    let disponible = true;
    let confirma = true;
    const biometria: Biometria = { disponible: async () => disponible, autenticar: async () => confirma };

    beforeEach(async () => {
        disponible = true;
        confirma = true;
        configurarEntorno({ ...entorno(), biometria });
        entorno().claves.borrar("iris-campo-huella");
        activarHuella(true);
        guardarSesion(sesion);
        recordarUsuario(TOMAS);
        await entorno().secreto.escribir(CLAVE_CSRF, "csrf-guardado");
        await cargarCsrf();
    });

    afterEach(() => {
        const resto = { ...entorno() };
        delete resto.biometria;
        configurarEntorno(resto);
    });

    it("apagada por defecto: sin encender «Desbloquear con huella» la app abre como antes (no se pide)", async () => {
        entorno().claves.borrar("iris-campo-huella");
        expect(huellaActiva()).toBe(false);
        expect(await pedirHuella(sesionGuardada())).toBe(false);
        activarHuella(false);
        expect(await pedirHuella(sesionGuardada())).toBe(false);
        activarHuella(true);
        expect(await pedirHuella(sesionGuardada())).toBe(true);
    });

    it("con biometría y la sesión con su token guardados (con o sin usuario recordado)", async () => {
        expect(await pedirHuella(sesionGuardada())).toBe(true);
        expect(await autenticarConHuella()).toBe(true);
        confirma = false;
        expect(await autenticarConHuella()).toBe(false);
    });

    it("no se pide sin biometría, sin sesión o sin token; olvidar al recordado no la apaga", async () => {
        disponible = false;
        expect(await pedirHuella(sesionGuardada())).toBe(false);
        disponible = true;
        expect(await pedirHuella(null)).toBe(false);
        // El usuario recordado no cuenta: olvidarlo (o que sea otro) no salta el bloqueo.
        recordarUsuario({ nombre: "Ana", correo: "ana@redcom.gt" });
        expect(await pedirHuella(sesionGuardada())).toBe(true);
        olvidarUsuarioRecordado();
        expect(usuarioRecordado()).toBeNull();
        expect(await pedirHuella(sesionGuardada())).toBe(true);
        recordarUsuario(TOMAS);
        await entorno().secreto.borrar(CLAVE_CSRF);
        await act(async () => cargarCsrf());
        expect(await pedirHuella(sesionGuardada())).toBe(false);
    });

    it("ataque: huella encendida → «¿No eres tú?» → reabrir: no entra a la sesión (con señal)", async () => {
        const api = new ApiFalsa()
            .en("DELETE", "/api/v1/sesion", () => ({ status: 204, cuerpo: null }))
            .en("GET", "/api/v1/yo", () => problema(401, "no_autenticado"))
            .instalar();
        expect(await abrirApp(false)).toMatchObject({ tipo: "huella" });
        await noSoyYo();
        expect(api.peticiones.some((p) => p.metodo === "DELETE")).toBe(true);
        expect(leerCsrf()).toBeNull();
        expect(await entorno().secreto.leer(CLAVE_CSRF)).toBeNull();
        expect(sesionGuardada()).toBeNull();
        expect(huellaActiva()).toBe(false);
        expect(usuarioRecordado()).toBeNull();
        // Reabrir: nada de entrar solo (la API ya cerró la sesión).
        const a = await abrirApp(false);
        expect(a.tipo).toBe("entrar");
    });

    it("ataque sin señal: «¿No eres tú?» deja el cierre pendiente y al reabrir no llama a GET /yo ni abre la guardada", async () => {
        const api = new ApiFalsa().en("GET", "/api/v1/yo", () => ({ status: 200, cuerpo: {} })).instalar();
        api.sinRed = true;
        await noSoyYo();
        expect(hayCierrePendiente()).toBe(true);
        expect(sesionGuardada()).toBeNull();
        expect(await abrirApp(false)).toEqual({ tipo: "entrar", motivo: "salida" });
        expect(api.peticiones.some((p) => p.ruta === "/api/v1/yo")).toBe(false);
        api.sinRed = false;
        await reintentarCierrePendiente();
    });

    it("nunca guarda la contraseña: en el teléfono solo nombre y correo", () => {
        const guardados = [...(entorno().claves as unknown as { datos: Map<string, string> }).datos.values()].join(" ");
        expect(guardados).not.toMatch(/secreta|contrasena/);
    });
});

describe.each(["light", "dark"] as const)("huella opcional en la app (%s)", (esquema) => {
    let disponible = true;
    const biometria: Biometria = { disponible: async () => disponible, autenticar: async () => true };

    beforeEach(() => {
        disponible = true;
        configurarEntorno({ ...entorno(), biometria });
        entorno().claves.borrar("iris-campo-huella");
        entorno().claves.borrar("iris-campo-entro-con-contrasena");
    });

    afterEach(() => {
        const resto = { ...entorno() };
        delete resto.biometria;
        configurarEntorno(resto);
    });

    it("tras entrar con contraseña ofrece la huella una sola vez; «Usar huella» la enciende", async () => {
        marcarEntradaConContrasena();
        const m = await montar();
        await abrirCampo(m, { esquema });
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/¿Usar tu huella la próxima vez\?/));
        await fireEvent.press(within(avisoArriba()).getByRole("button", { name: "Usar huella" }));
        expect(huellaActiva()).toBe(true);
        // Ya decidido: no vuelve a ofrecerse.
        marcarEntradaConContrasena();
        expect(await debeOfrecerHuella()).toBe(false);
    });

    it("si no la acepta (cierra o ignora el aviso), queda en «no» y no vuelve a salir", async () => {
        marcarEntradaConContrasena();
        const m = await montar();
        await abrirCampo(m, { esquema });
        await waitFor(() => expect(avisoArriba()).toHaveTextContent(/¿Usar tu huella la próxima vez\?/));
        expect(huellaActiva()).toBe(false);
        expect(entorno().claves.leer("iris-campo-huella")).toBe("no");
        marcarEntradaConContrasena();
        expect(await debeOfrecerHuella()).toBe(false);
    });

    it("sin entrar con contraseña o sin biometría, no se ofrece", async () => {
        expect(await debeOfrecerHuella()).toBe(false);
        disponible = false;
        marcarEntradaConContrasena();
        expect(await debeOfrecerHuella()).toBe(false);
    });

    it("«Yo»: interruptor «Desbloquear con huella», apagado por defecto, que la enciende y apaga", async () => {
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        const interruptor = await screen.findByRole("switch", { name: "Desbloquear con huella" });
        expect(interruptor).not.toBeChecked();
        await fireEvent.press(interruptor);
        await waitFor(() => expect(huellaActiva()).toBe(true));
        expect(screen.getByRole("switch", { name: "Desbloquear con huella" })).toBeChecked();
        await fireEvent.press(screen.getByRole("switch", { name: "Desbloquear con huella" }));
        await waitFor(() => expect(huellaActiva()).toBe(false));
    });

    it("«Yo» sin biometría en el teléfono: no hay interruptor", async () => {
        disponible = false;
        const m = await montar();
        await abrirCampo(m, { esquema, vista: { tipo: "yo" } });
        expect(await screen.findByRole("button", { name: /Cerrar sesión/ })).toBeTruthy();
        expect(screen.queryByRole("switch", { name: "Desbloquear con huella" })).toBeNull();
    });
});

describe("vuelve a pedir la huella tras más de 5 min en segundo plano", () => {
    it("solo con una ausencia larga, y no sin sesión abierta", async () => {
        let oyente: ((e: AppStateStatus) => void) | null = null;
        const espia = jest.spyOn(AppState, "addEventListener").mockImplementation(((_: string, fn: (e: AppStateStatus) => void) => {
            oyente = fn;
            return { remove: jest.fn() };
        }) as unknown as typeof AppState.addEventListener);
        let ahora = 1_000_000;
        const alVolver = jest.fn();
        try {
            const { rerender } = await renderHook(({ activo }: { activo: boolean }) => useVolverTrasAusencia(activo, alVolver, () => ahora), { initialProps: { activo: true } });
            const cambiar = (e: AppStateStatus) => act(() => (oyente as ((e: AppStateStatus) => void) | null)?.(e));
            await cambiar("background");
            ahora += 2 * 60_000;
            await cambiar("active");
            expect(alVolver).not.toHaveBeenCalled();
            await cambiar("background");
            ahora += AUSENCIA_MAX_MS + 1;
            await cambiar("active");
            expect(alVolver).toHaveBeenCalledTimes(1);
            expect(ausenciaLarga(null, ahora)).toBe(false);
            oyente = null;
            await rerender({ activo: false });
            expect(oyente).toBeNull();
        } finally {
            espia.mockRestore();
        }
    });
});
