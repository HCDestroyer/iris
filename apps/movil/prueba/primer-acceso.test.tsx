// Reporte del PM (APK 0.18.0, Android): al entrar con una contraseña temporal la app decía «Entra al
// panel de Iris desde un navegador…» con solo «Reintentar», y un técnico con solo el teléfono quedaba
// atrapado. Ahora la cambia ahí mismo («Crea tu contraseña», opción B del login) con
// `POST /yo/contrasena` (cookie + CSRF de la sesión limitada) y entra directo; y toda pantalla de
// bloqueo del arranque tiene «Usar otra cuenta» y el «Atrás» de Android lleva al login.
import type { Schema } from "@iris/contratos";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react-native";
import { BackHandler, StyleSheet } from "react-native";
import App from "../src/App";
import { configurarEntorno, entorno } from "../src/entorno";
import { CLAVE_CSRF } from "../src/sesion/sesion";
import { crearTema } from "../src/tema/tokens";
import { ErrorTransporte } from "@iris/sync";
import { montar } from "@iris/sync/pruebas";
import { ApiFalsa, problema, usuario, yo } from "./api-falsa";
import { abrirCampo } from "./arnes";
import { clavesMemoria, secretoMemoria } from "./preparar";

// Uniwind compila el CSS en Metro; en Jest no hace falta.
jest.mock("../src/global.css", () => ({}));
jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

const NUEVA = "una frase bastante larga";

const yoTemporal = (empresas?: Schema<"EmpresaResumen">[]): Schema<"Yo"> => {
    const y = yo(5, empresas);
    return { ...y, usuario: { ...y.usuario, nombre: "Ana Pérez", cambioContrasenaRequerido: true } };
};
/** Dos empresas: tras cambiarla, la app pide elegir (no abre el motor en esta prueba). */
const DOS = [
    { id: 1, nombre: "ISP Norte" },
    { id: 2, nombre: "ISP Sur" },
];

/** API con la sesión limitada: `GET /yo` pide el cambio hasta que `POST /yo/contrasena` responde 200. */
function api(): ApiFalsa & { estado: { cambiada: boolean } } {
    const estado = { cambiada: false };
    const a = new ApiFalsa()
        .en("GET", "/api/v1/yo", () => ({ status: 200, cuerpo: estado.cambiada ? yo(5, DOS) : yoTemporal(DOS) }))
        .en("DELETE", "/api/v1/sesion", () => ({ status: 204 }))
        .instalar();
    return Object.assign(a, { estado });
}

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

beforeEach(async () => {
    configurarEntorno({ ...entorno(), claves: clavesMemoria(), secreto: secretoMemoria() });
    // Sesión limitada abierta: el CSRF que dio `POST /sesion` está en el llavero.
    await entorno().secreto.escribir(CLAVE_CSRF, "csrf-temporal");
});

afterEach(() => {
    jest.restoreAllMocks();
});

async function abrir() {
    await render(<App />);
    return screen.findByTestId("crear-contrasena");
}

async function escribir(actual: string, nueva: string, repetir = nueva) {
    await fireEvent.changeText(screen.getByTestId("campo-actual"), actual);
    await fireEvent.changeText(screen.getByTestId("campo-nueva"), nueva);
    await fireEvent.changeText(screen.getByTestId("campo-repetir"), repetir);
    await fireEvent.press(screen.getByTestId("guardar-contrasena"));
}

describe("contraseña temporal al entrar", () => {
    it("«Crea tu contraseña» (opción B: logo, título, explicación, «Guardar y entrar» en tinta) y al guardar entra directo", async () => {
        const a = api();
        let cuerpo: unknown = null;
        a.en("POST", "/api/v1/yo/contrasena", (p) => {
            cuerpo = p.cuerpo;
            a.estado.cambiada = true;
            return { status: 200, cuerpo: usuario() };
        });
        const marca = await abrir();
        expect(within(marca).getByLabelText("Iris")).toBeTruthy();
        expect(StyleSheet.flatten(marca.props.style)).toMatchObject({ alignItems: "center" });
        expect(screen.getByRole("header", { name: "Crea tu contraseña" })).toBeTruthy();
        expect(screen.getByText(/Hola, Ana\. Entraste con una contraseña temporal/)).toBeTruthy();
        // Nunca más «entra al panel desde un navegador».
        expect(screen.queryByText(/navegador/)).toBeNull();
        expect(screen.getByLabelText("Contraseña temporal")).toBeTruthy();
        expect(screen.getByTestId("guardar-contrasena")).toHaveStyle({ backgroundColor: crearTema(false).campo.tinta });
        expect(screen.getByTestId("usar-otra-cuenta")).toHaveTextContent("Usar otra cuenta");

        await escribir("temporal-del-admin", NUEVA);
        // Entra directo: vuelve a leer /yo y sigue (aquí, a elegir empresa), con el aviso.
        expect(await screen.findByRole("header", { name: "¿Con qué empresa trabajas?" })).toBeTruthy();
        expect((await screen.findAllByTestId("notificacion"))[0]).toHaveTextContent("Contraseña guardada");
        expect(cuerpo).toEqual({ actual: "temporal-del-admin", nueva: NUEVA });
        const post = a.peticiones.find((p) => p.ruta === "/api/v1/yo/contrasena");
        // Con la sesión del teléfono: CSRF del llavero, canal campo e Idempotency-Key.
        expect(post?.cabeceras.get("X-CSRF-Token")).toBe("csrf-temporal");
        expect(post?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        expect(post?.cabeceras.get("Idempotency-Key")).toMatch(/^[0-9a-f-]{36}$/);
        expect(a.peticiones.filter((p) => p.metodo === "GET" && p.ruta === "/api/v1/yo")).toHaveLength(2);
    });

    it("contraseña débil: la regla de la API (12 caracteres) en línea sin llamar; y el 422 de la API en su campo", async () => {
        const a = api();
        a.en("POST", "/api/v1/yo/contrasena", () => ({
            status: 422,
            cuerpo: { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "nueva", message: "Mínimo 12 caracteres", rule: "minLength" }] } satisfies Schema<"Problema">,
            cabeceras: { "Content-Type": "application/problem+json" },
        }));
        await abrir();
        await escribir("temporal-del-admin", "corta");
        expect(await screen.findByText("Usa 12 caracteres o más")).toBeTruthy();
        expect(a.peticiones.some((p) => p.ruta === "/api/v1/yo/contrasena")).toBe(false);
        // Repetida distinta: tampoco sale.
        await escribir("temporal-del-admin", NUEVA, "otra frase bastante larga");
        expect(await screen.findByText("No coincide con la nueva")).toBeTruthy();
        expect(a.peticiones.some((p) => p.ruta === "/api/v1/yo/contrasena")).toBe(false);
        // La ayuda en verde al cumplir la regla.
        await fireEvent.changeText(screen.getByTestId("campo-repetir"), NUEVA);
        expect(await screen.findByTestId("ayuda-largo")).toHaveTextContent("12 caracteres o más");
        // Si la API la rechaza igual, el mensaje va al campo y sigue en la pantalla.
        await fireEvent.press(screen.getByTestId("guardar-contrasena"));
        expect(await screen.findByText("Mínimo 12 caracteres")).toBeTruthy();
        expect(screen.getByTestId("crear-contrasena")).toBeTruthy();
    });

    it("temporal incorrecta: 422 en «Contraseña temporal» (vaciado) y no entra", async () => {
        const a = api();
        a.en("POST", "/api/v1/yo/contrasena", () => ({
            status: 422,
            cuerpo: { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "actual", message: "La contraseña actual no es correcta", rule: "incorrecta" }] } satisfies Schema<"Problema">,
            cabeceras: { "Content-Type": "application/problem+json" },
        }));
        await abrir();
        await escribir("equivocada", NUEVA);
        expect(await screen.findByText("La contraseña actual no es correcta")).toBeTruthy();
        expect(screen.getByTestId("campo-actual").props.value).toBe("");
        expect(screen.getByTestId("crear-contrasena")).toBeTruthy();
        expect(a.peticiones.filter((p) => p.metodo === "GET" && p.ruta === "/api/v1/yo")).toHaveLength(1);
    });

    it("sin señal al guardar: «Reintentar» con la misma clave; «Usar otra cuenta» sigue a la vista", async () => {
        const a = api();
        a.en("POST", "/api/v1/yo/contrasena", () => {
            a.estado.cambiada = true;
            return { status: 200, cuerpo: usuario() };
        });
        await abrir();
        a.sinRed = true;
        await escribir("temporal-del-admin", NUEVA);
        const aviso = await screen.findByTestId("aviso-error-contrasena");
        expect(aviso).toHaveTextContent(/Sin señal/);
        expect(screen.getByTestId("usar-otra-cuenta")).toBeTruthy();
        a.sinRed = false;
        await fireEvent.press(within(aviso).getByRole("button", { name: "Reintentar" }));
        expect(await screen.findByRole("header", { name: "¿Con qué empresa trabajas?" })).toBeTruthy();
        const claves = new Set(a.peticiones.filter((p) => p.ruta === "/api/v1/yo/contrasena").map((p) => p.cabeceras.get("Idempotency-Key")));
        expect(claves.size).toBe(1);
    });

    it("429 por intentos: la API cerró la sesión; «Ir a iniciar sesión» lleva al login", async () => {
        const a = api();
        a.en("POST", "/api/v1/yo/contrasena", () => ({ ...problema(429, "demasiados_intentos"), cabeceras: { "Content-Type": "application/problem+json", "Retry-After": "900" } }));
        await abrir();
        await escribir("equivocada", NUEVA);
        const aviso = await screen.findByTestId("aviso-sesion-cerrada");
        expect(aviso).toHaveTextContent(/Demasiados intentos: vuelve a entrar en 15 min/);
        expect(screen.queryByTestId("guardar-contrasena")).toBeNull();
        expect(screen.getByTestId("usar-otra-cuenta")).toBeTruthy();
        await fireEvent.press(within(aviso).getByRole("button", { name: "Ir a iniciar sesión" }));
        expect(await screen.findByRole("header", { name: "Iris Campo" })).toBeTruthy();
    });
});

describe("salida de las pantallas de bloqueo", () => {
    it("«Usar otra cuenta» cierra la sesión limitada (DELETE /sesion con su CSRF) y lleva al login", async () => {
        const a = api();
        await abrir();
        await fireEvent.press(screen.getByTestId("usar-otra-cuenta"));
        expect(await screen.findByRole("header", { name: "Iris Campo" })).toBeTruthy();
        const borrar = a.peticiones.find((p) => p.metodo === "DELETE" && p.ruta === "/api/v1/sesion");
        expect(borrar?.cabeceras.get("X-CSRF-Token")).toBe("csrf-temporal");
        // Pide correo (otra cuenta), no recuerda a nadie.
        expect(screen.getByLabelText("Correo")).toBeTruthy();
    });

    it("«Atrás» de Android en «Crea tu contraseña» lleva al login (no deja la app atrapada)", async () => {
        const atras = atrasAndroid();
        const a = api();
        await abrir();
        expect(await atras()).toBe(true);
        expect(await screen.findByRole("header", { name: "Iris Campo" })).toBeTruthy();
        expect(a.peticiones.some((p) => p.metodo === "DELETE" && p.ruta === "/api/v1/sesion")).toBe(true);
    });

    it("sin empresa asignada: el mismo diseño con «Usar otra cuenta» y «Atrás» al login", async () => {
        const atras = atrasAndroid();
        new ApiFalsa()
            .en("GET", "/api/v1/yo", () => ({ status: 200, cuerpo: { ...yo(5, []), modoEmpresa: "sin_empresa" } }))
            .en("DELETE", "/api/v1/sesion", () => ({ status: 204 }))
            .instalar();
        await render(<App />);
        expect(await screen.findByRole("header", { name: "Sin empresa asignada" })).toBeTruthy();
        expect(within(screen.getByTestId("sin-empresa")).getByLabelText("Iris")).toBeTruthy();
        expect(screen.getByTestId("usar-otra-cuenta")).toBeTruthy();
        expect(await atras()).toBe(true);
        expect(await screen.findByRole("header", { name: "Iris Campo" })).toBeTruthy();
    });

    it("elegir empresa también tiene «Usar otra cuenta»", async () => {
        const a = api();
        a.estado.cambiada = true;
        await render(<App />);
        expect(await screen.findByRole("header", { name: "¿Con qué empresa trabajas?" })).toBeTruthy();
        await fireEvent.press(screen.getByTestId("usar-otra-cuenta"));
        expect(await screen.findByRole("header", { name: "Iris Campo" })).toBeTruthy();
    });
});

describe("contraseña temporal con la app abierta", () => {
    it("si la API pide el cambio al bajar las órdenes, va a «Crea tu contraseña» (no al panel)", async () => {
        const m = await montar();
        m.transporte.paquete = () => {
            throw new ErrorTransporte("http", "Cambio de contraseña requerido", 403, { type: "urn:iris:problema:cambio_contrasena_requerido", title: "Cambio de contraseña requerido", status: 403 });
        };
        const alExigirContrasena = jest.fn();
        await abrirCampo(m, { alExigirContrasena });
        await waitFor(() => expect(alExigirContrasena).toHaveBeenCalled());
        expect(screen.queryByText(/panel de Iris/)).toBeNull();
    });
});
