/*
 * Latido de disponibilidad de la bandeja (fase 1.1, contrato §14.5). Mientras la persona está
 * activa (pestaña a la vista y algo de teclado, puntero o desplazamiento hace poco) el panel avisa
 * al servidor como mucho una vez por intervalo. Con la pestaña oculta o sin actividad no se manda
 * nada: el servidor la pasa a «No disponible» al cumplirse los minutos de inactividad de la empresa.
 *
 * El intervalo sale de esos minutos (`tiemposLatido`): con 15 min basta uno por minuto, pero con
 * 1 min hacen falta varios dentro del minuto para que una persona activa no caiga.
 *
 * Tras un 5xx, un 429 o un corte de red se espera cada vez más (exponencial, hasta 5 min); un
 * resultado «detener» (401: la sesión ya no existe) lo apaga.
 *
 * La lógica no toca el DOM ni el reloj real (todo se inyecta), para probarla con Vitest.
 */

export type TrasFallo = "esperar" | "detener" | "seguir";

export type OpcionesLatido = {
    /** Manda el latido. */
    enviar: () => Promise<unknown>;
    /** Hora actual en ms (inyectable en pruebas). */
    ahora?: () => number;
    /** Mínimo entre dos latidos. */
    intervaloMs?: number;
    /** Sin actividad durante este tiempo, la persona deja de contar como activa. */
    inactivoTrasMs?: number;
    /** ¿La pestaña está a la vista al empezar? */
    visible?: boolean;
    /** Qué hacer tras un fallo: esperar (5xx, 429, red), detener (401) o seguir como siempre. */
    trasFallo?: (error: unknown) => TrasFallo;
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
    /** ¿Se detuvo? (para ignorar la respuesta de un latido que llegó tarde). */
    detenido: () => boolean;
};

export const INTERVALO_LATIDO_MS = 60_000;
export const INACTIVO_TRAS_MS = 90_000;
export const ESPERA_MAXIMA_MS = 5 * 60_000;

/**
 * Tiempos del latido para unos minutos de inactividad: intervalo = mín(60 s, minutos × 60 / 3 s),
 * es decir, al menos tres latidos dentro de la ventana; la revisión, con la misma frecuencia o más;
 * y «activa» si hubo actividad en el último intervalo y medio (sin pasar de 90 s).
 */
export function tiemposLatido(minutosInactividad: number): { intervaloMs: number; revisionMs: number; inactivoTrasMs: number } {
    const minutos = Number.isFinite(minutosInactividad) && minutosInactividad > 0 ? minutosInactividad : 15;
    const intervaloMs = Math.min(INTERVALO_LATIDO_MS, Math.floor((minutos * 60_000) / 3));
    return { intervaloMs, revisionMs: Math.min(15_000, Math.floor(intervaloMs / 2)), inactivoTrasMs: Math.min(INACTIVO_TRAS_MS, Math.floor(intervaloMs * 1.5)) };
}

export function crearLatido({
    enviar,
    ahora = Date.now,
    intervaloMs = INTERVALO_LATIDO_MS,
    inactivoTrasMs = INACTIVO_TRAS_MS,
    visible = true,
    trasFallo = () => "seguir",
}: OpcionesLatido): Latido {
    let ultimaActividad = Number.NEGATIVE_INFINITY;
    let ultimoEnvio = Number.NEGATIVE_INFINITY;
    let aLaVista = visible;
    let enCurso = false;
    let parado = false;
    let fallos = 0;
    let esperarHasta = Number.NEGATIVE_INFINITY;

    const activa = () => !parado && aLaVista && ahora() - ultimaActividad <= inactivoTrasMs;

    function intentar() {
        if (enCurso || !activa() || ahora() - ultimoEnvio < intervaloMs || ahora() < esperarHasta) return;
        ultimoEnvio = ahora();
        enCurso = true;
        enviar()
            .then(
                () => {
                    fallos = 0;
                },
                (error: unknown) => {
                    const que = trasFallo(error);
                    if (que === "detener") {
                        parado = true;
                    } else if (que === "esperar") {
                        fallos += 1;
                        esperarHasta = ahora() + Math.min(ESPERA_MAXIMA_MS, intervaloMs * 2 ** fallos);
                    }
                },
            )
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
            parado = true;
        },
        activa,
        detenido: () => parado,
    };
}

/** Espera de la relectura tras `fallos` seguidos (exponencial, hasta 5 min). */
export function esperaTrasFallos(baseMs: number, fallos: number): number {
    return Math.min(ESPERA_MAXIMA_MS, baseMs * 2 ** Math.max(0, fallos));
}
