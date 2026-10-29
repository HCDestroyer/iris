// Preparación común de Jest: área segura simulada y un entorno en memoria (claves, llavero y
// `fetch` que cada prueba reemplaza).
import { configurarEntorno, type AlmacenClaves, type AlmacenSecreto } from "../src/entorno";

jest.mock("react-native-safe-area-context", () => jest.requireActual("react-native-safe-area-context/jest/mock").default);

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
