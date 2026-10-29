// Implementaciones nativas del entorno: kv-store de expo-sqlite (síncrono) y SecureStore.
import Storage from "expo-sqlite/kv-store";
import * as SecureStore from "expo-secure-store";
import type { Entorno } from "./entorno";

const URL_POR_DEFECTO = "http://localhost:8080";

export function entornoNativo(): Entorno {
    return {
        baseUrl: (process.env.EXPO_PUBLIC_IRIS_API_URL ?? URL_POR_DEFECTO).replace(/\/+$/, ""),
        claves: {
            leer: (clave) => Storage.getItemSync(clave),
            escribir: (clave, valor) => Storage.setItemSync(clave, valor),
            borrar: (clave) => {
                Storage.removeItemSync(clave);
            },
        },
        secreto: {
            leer: (clave) => SecureStore.getItemAsync(clave),
            escribir: (clave, valor) => SecureStore.setItemAsync(clave, valor),
            borrar: (clave) => SecureStore.deleteItemAsync(clave),
        },
        fetch: (...args) => globalThis.fetch(...args),
    };
}
