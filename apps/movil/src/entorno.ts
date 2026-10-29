// Dependencias del teléfono que la app inyecta (y las pruebas reemplazan): origen de la API,
// almacén de claves (lo no sensible: sesión guardada, empresa elegida, rastreo), almacén seguro (el
// token CSRF de la sesión) y `fetch`. Las implementaciones nativas están en `entorno-nativo.ts`.

/** Clave-valor síncrono (`expo-sqlite/kv-store`): solo datos no sensibles. */
export interface AlmacenClaves {
    leer(clave: string): string | null;
    escribir(clave: string, valor: string): void;
    borrar(clave: string): void;
}

/** Llavero del teléfono (`expo-secure-store`): el token CSRF de la sesión. */
export interface AlmacenSecreto {
    leer(clave: string): Promise<string | null>;
    escribir(clave: string, valor: string): Promise<void>;
    borrar(clave: string): Promise<void>;
}

export interface Entorno {
    /** Origen de la API, sin `/api/v1` (`EXPO_PUBLIC_IRIS_API_URL`). */
    baseUrl: string;
    claves: AlmacenClaves;
    secreto: AlmacenSecreto;
    fetch: typeof globalThis.fetch;
}

let actual: Entorno | null = null;

export function configurarEntorno(e: Entorno): void {
    actual = e;
}

export function entorno(): Entorno {
    if (!actual) {
        throw new Error("Entorno sin configurar: llama configurarEntorno() al iniciar la app");
    }
    return actual;
}

/** Lee JSON del almacén de claves; si no está o no tiene la forma esperada, `null`. */
export function leerJson<T>(clave: string, esValido: (v: unknown) => v is T): T | null {
    const texto = entorno().claves.leer(clave);
    if (texto === null) {
        return null;
    }
    try {
        const valor: unknown = JSON.parse(texto);
        return esValido(valor) ? valor : null;
    } catch {
        return null;
    }
}
