// Preparación común de Jest: área segura simulada, Gesture Handler con sus simulacros oficiales
// (Reanimated y Worklets van en su versión de JS por `prueba/resolver.js`; en Jest no hay hilo de
// UI) y un entorno en memoria (claves, llavero y `fetch` que cada prueba reemplaza).
import "react-native-gesture-handler/jestSetup";
import { configurarEntorno, type AlmacenClaves, type AlmacenSecreto } from "../src/entorno";

jest.mock("react-native-safe-area-context", () => jest.requireActual("react-native-safe-area-context/jest/mock").default);
// El simulacro oficial de Worklets: Gesture Handler 3 pide al cargar el runtime de UI, que la
// versión web de Worklets no tiene.
jest.mock("react-native-worklets", () => jest.requireActual("react-native-worklets/src/mock"));
// El simulacro oficial de react-native-keyboard-controller (sin módulo nativo): `KeyboardAvoidingView`
// es un `View` que conserva sus props (`behavior`) y `useKeyboardState` lee `KeyboardController.state()`.
jest.mock("react-native-keyboard-controller", () => jest.requireActual("react-native-keyboard-controller/jest"));

// En Jest no corre el compilador de Uniwind (lo hace Metro al empaquetar): las clases no dan
// estilos y Uniwind avisa una vez que no encuentra `--theme` (el tema de HeroUI). Solo ese aviso se
// calla; las medidas que se prueban (zonas táctiles) van en `style`.
const avisar = console.warn.bind(console);
console.warn = (...args: unknown[]) => {
    if (typeof args[0] === "string" && args[0].includes("Uniwind - We couldn't find your variable --theme")) {
        return;
    }
    avisar(...args);
};

export const clavesMemoria = (): AlmacenClaves & { datos: Map<string, string> } => {
    const datos = new Map<string, string>();
    return {
        datos,
        leer: (c) => datos.get(c) ?? null,
        escribir: (c, v) => {
            datos.set(c, v);
        },
        borrar: (c) => {
            datos.delete(c);
        },
    };
};

export const secretoMemoria = (): AlmacenSecreto & { datos: Map<string, string> } => {
    const datos = new Map<string, string>();
    return {
        datos,
        leer: async (c) => datos.get(c) ?? null,
        escribir: async (c, v) => {
            datos.set(c, v);
        },
        borrar: async (c) => {
            datos.delete(c);
        },
    };
};

configurarEntorno({
    baseUrl: "http://iris.prueba",
    claves: clavesMemoria(),
    secreto: secretoMemoria(),
    fetch: () => Promise.reject(new Error("fetch sin configurar en esta prueba")),
});
