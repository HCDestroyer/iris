// Datos del titular en la cola del teléfono (contrato argosisp.md 0.10.0 §3.4.8): nunca se quedan sin
// la sesión de su técnico. Las reglas viven aquí (sin React) para probarlas con Vitest.
import type { EstadoSync } from "@iris/sync";

/** Lo que el motor deja ver para vigilar la sesión. */
export interface MotorVigilado {
    suscribir: (oyente: () => void) => () => void;
    obtenerEstado: () => Pick<EstadoSync, "conexion" | "ultimoError">;
}

/** Cómo se olvidan: lo de esta base y lo de todas las bases de campo del navegador. */
export interface OlvidarSensibles {
    propias: () => Promise<void>;
    todas: () => Promise<unknown>;
}

/** La sesión se perdió por un 401 (vencida o revocada); un 403 no cuenta. */
export function sesionPerdidaPor401(estado: Pick<EstadoSync, "conexion" | "ultimoError">): boolean {
    return estado.conexion === "sin_sesion" && estado.ultimoError?.status === 401;
}

/**
 * Con la app abierta: cuando el motor pasa a `sin_sesion` por un 401, olvida en el momento lo
 * sensible de esta base y de todas las demás (una vez por cada pérdida de la sesión). Devuelve la
 * función para dejar de vigilar.
 */
export function vigilarSesionPerdida(motor: MotorVigilado, olvidar: OlvidarSensibles): () => void {
    let perdida = false;
    const revisar = () => {
        const ahora = sesionPerdidaPor401(motor.obtenerEstado());
        if (ahora && !perdida) {
            void olvidar.propias().catch(() => undefined);
            void Promise.resolve(olvidar.todas()).catch(() => undefined);
        }
        perdida = ahora;
    };
    revisar();
    return motor.suscribir(revisar);
}

/**
 * Al cerrar la sesión (con o sin señal): antes de salir, lo sensible de todas las bases de campo
 * del navegador (también las de otras personas y empresas).
 */
export async function olvidarAlCerrarSesion(olvidar: Pick<OlvidarSensibles, "todas">): Promise<void> {
    try {
        await olvidar.todas();
    } catch {
        // Lo que no se pudo borrar ahora se borra al arrancar sin sesión.
    }
}
