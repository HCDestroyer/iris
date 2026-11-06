/*
 * Latido de disponibilidad de la bandeja (fase 1.1). Mientras la persona está activa (pestaña a la
 * vista y algo de teclado, puntero o desplazamiento hace poco) el panel avisa al servidor como
 * mucho una vez por intervalo (~60 s). Con la pestaña oculta o sin actividad no se manda nada: el
 * servidor la pasa a «No disponible» al cumplirse los minutos de inactividad de la empresa.
 *
 * La lógica no toca el DOM ni el reloj real (todo se inyecta), para probarla con Vitest; el
 * gancho `useLatido` (latido-gancho.ts) la conecta con la página.
 */

export type OpcionesLatido = {
    /** Manda el latido. Si falla, el siguiente intento espera el intervalo igual. */
    enviar: () => Promise<unknown>;
    /** Hora actual en ms (inyectable en pruebas). */
    ahora?: () => number;
    /** Mínimo entre dos latidos. */
    intervaloMs?: number;
    /** Sin actividad durante este tiempo, la persona deja de contar como activa. */
    inactivoTrasMs?: number;
    /** ¿La pestaña está a la vista al empezar? */
    visible?: boolean;
};

export type Latido = {
    /** Hubo actividad (tecla, puntero, desplazamiento): puede mandar un latido si toca. */
    actividad: () => void;
    /** Cambió la visibilidad de la pestaña. Volver a verla cuenta como actividad. */
    visibilidad: (visible: boolean) => void;
    /** Revisión periódica: manda un latido si la persona sigue activa y ya pasó el intervalo. */
    revisar: () => void;
    /** Deja de mandar (al salir de la pantalla o cerrar sesión). */
    detener: () => void;
    /** ¿Cuenta como activa ahora? */
    activa: () => boolean;
};

export const INTERVALO_LATIDO_MS = 60_000;
export const INACTIVO_TRAS_MS = 90_000;

export function crearLatido({ enviar, ahora = Date.now, intervaloMs = INTERVALO_LATIDO_MS, inactivoTrasMs = INACTIVO_TRAS_MS, visible = true }: OpcionesLatido): Latido {
    let ultimaActividad = Number.NEGATIVE_INFINITY;
    let ultimoEnvio = Number.NEGATIVE_INFINITY;
    let aLaVista = visible;
    let enCurso = false;
    let detenido = false;

    const activa = () => !detenido && aLaVista && ahora() - ultimaActividad <= inactivoTrasMs;

    function intentar() {
        if (enCurso || !activa() || ahora() - ultimoEnvio < intervaloMs) return;
        ultimoEnvio = ahora();
        enCurso = true;
        enviar()
            .catch(() => undefined)
            .finally(() => {
                enCurso = false;
            });
    }

    return {
        actividad() {
            ultimaActividad = ahora();
            intentar();
        },
        visibilidad(v) {
            aLaVista = v;
            if (v) {
                ultimaActividad = ahora();
                intentar();
            }
        },
        revisar: intentar,
        detener() {
            detenido = true;
        },
        activa,
    };
}
