/*
 * Re-autenticación de las sesiones abiertas desde Argos (origen `sso`): las acciones sensibles
 * responden 403 `reautenticacion_requerida`. El cliente HTTP (cliente.ts) pide la contraseña con el
 * diálogo que registre el panel y, si se confirma, repite la petición. Si no hay diálogo registrado
 * (p. ej. fuera del panel) o se cancela, la acción ve el 403 tal cual.
 */

export const CODIGO_REAUTENTICACION = "reautenticacion_requerida";
export const RUTA_REAUTENTICAR = "/api/v1/sesion/reautenticar";

/** Abre el diálogo y resuelve `true` si la contraseña se confirmó, `false` si se canceló. */
export type PedirReautenticacion = () => Promise<boolean>;

let manejador: PedirReautenticacion | null = null;
let enCurso: Promise<boolean> | null = null;

/** Registra el diálogo del panel. Devuelve la función para quitarlo. */
export function registrarReautenticacion(pedir: PedirReautenticacion): () => void {
    manejador = pedir;
    return () => {
        if (manejador === pedir) manejador = null;
    };
}

/** Pide la contraseña una sola vez aunque varias acciones respondan 403 a la vez. */
export function solicitarReautenticacion(): Promise<boolean> {
    if (!manejador) return Promise.resolve(false);
    enCurso ??= manejador().finally(() => {
        enCurso = null;
    });
    return enCurso;
}
