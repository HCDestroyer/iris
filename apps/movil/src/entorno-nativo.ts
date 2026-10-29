// Implementaciones nativas del entorno: kv-store de expo-sqlite (síncrono) y SecureStore.
import Storage from "expo-sqlite/kv-store";
import * as SecureStore from "expo-secure-store";
import type { Entorno } from "./entorno";

const URL_POR_DEFECTO = "http://localhost:8080";

/**
 * Origen de la API sin `/` final. Fuera de desarrollo exige HTTPS: la cookie de sesión y el CSRF
 * nunca viajan en claro (falla al iniciar con un mensaje que dice qué configurar).
 */
export function validarUrlApi(url: string | undefined, desarrollo: boolean): string {
    const limpia = (url ?? URL_POR_DEFECTO).trim().replace(/\/+$/, "");
    if (!desarrollo && !limpia.startsWith("https://")) {
        throw new Error(`EXPO_PUBLIC_IRIS_API_URL debe empezar con https:// en una compilación de producción (vale «${limpia}»).`);
    }
    return limpia;
}

export function entornoNativo(): Entorno {
    return {
        baseUrl: validarUrlApi(process.env.EXPO_PUBLIC_IRIS_API_URL, __DEV__),
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
