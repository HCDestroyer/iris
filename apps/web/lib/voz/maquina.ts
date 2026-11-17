import type { Llamada } from "./tipos";

/*
 * Máquina de estados de una llamada entrante en el panel (contratos/voz.md §3.3 y §3.4). Es pura:
 * recibe lo que pasa (el feed, lo que toca la persona, las respuestas del servidor, el medio de
 * WebRTC, los plazos) y devuelve el estado siguiente. Los efectos (pedir el micrófono, llamar a la
 * API, el timbre) los hace el proveedor mirando la transición.
 *
 * Una pestaña atiende una sola llamada a la vez: mientras contesta o habla, otra entrante no la
 * interrumpe (el servidor tampoco se la manda: `llamada_en_curso`).
 */

/** Pasos de «Contestar», en orden: `reclamar` → micrófono → `contestar` (SDP) → `confirmar-medio`. */
export type PasoContestar = "reclamando" | "microfono" | "negociando" | "confirmando";

/** Por qué se cerró sin llegar a hablar (o por qué se cortó), para decirlo en llano. */
export type MotivoAviso =
    | "otro_la_contesto"
    | "reserva_vencida"
    | "termino_antes"
    | "sin_microfono"
    | "microfono_denegado"
    | "no_recibe_llamadas"
    | "sin_red"
    | "medio_fallido"
    | "meta_rechazo"
    | "error";

export type EstadoVoz =
    | { fase: "libre" }
    | { fase: "sonando"; llamada: Llamada }
    | { fase: "contestando"; llamada: Llamada; paso: PasoContestar; venceEn: number | null }
    | { fase: "en_curso"; llamada: Llamada; desde: number; silenciada: boolean }
    /** Terminó una llamada que sí se habló: se ofrece la nota. `corte`: se cayó el medio. */
    | { fase: "terminada"; llamada: Llamada; duracionSeg: number; corte: boolean }
    /** No se llegó a hablar: un mensaje llano y se cierra. */
    | { fase: "aviso"; motivo: MotivoAviso; quien?: string | undefined; titulo: string; detalle?: string | undefined };

export type EventoVoz =
    /** Feed `llamada_entrante`, o la llamada sigue sonando al abrir el panel. */
    | { tipo: "entrante"; llamada: Llamada }
    /** Feed `llamada_actualizada` / `llamada_terminada`, o una relectura. */
    | { tipo: "actualizada"; llamada: Llamada }
    | { tipo: "contestar" }
    | { tipo: "rechazar" }
    | { tipo: "reclamada"; llamada: Llamada; venceEn: number }
    | { tipo: "microfono_listo" }
    | { tipo: "respuesta_enviada"; llamada: Llamada }
    | { tipo: "medio_confirmado"; llamada: Llamada; ahora: number }
    /** Pasó `venceEn` (12 s de reserva, o los 5 s para confirmar) sin llegar a `en_curso`. */
    | { tipo: "plazo_vencido" }
    /** `detalle`: lo que dijo el servidor en llano (p. ej. quién la tiene), si lo dijo. */
    | { tipo: "fallo"; motivo: MotivoAviso; quien?: string | undefined; detalle?: string | undefined }
    /** El 409 `misma_persona`: otra pestaña mía la tiene; aquí solo se cierra el aviso. */
    | { tipo: "otra_pestana" }
    | { tipo: "silenciar"; silenciada: boolean }
    | { tipo: "colgar"; ahora: number }
    /** El medio se cayó a mitad de la llamada (red, pestaña suspendida). */
    | { tipo: "medio_caido"; ahora: number }
    /** Se cerró el aviso o la nota. */
    | { tipo: "cerrar" };

export const LIBRE: EstadoVoz = { fase: "libre" };

const FINALES: ReadonlySet<Llamada["estado"]> = new Set(["terminada", "perdida", "rechazada", "fuera_de_horario", "fallida"]);
export const esFinal = (l: Pick<Llamada, "estado">): boolean => FINALES.has(l.estado);

const aviso = (motivo: MotivoAviso, titulo: string, quien?: string, detalle?: string): EstadoVoz => ({ fase: "aviso", motivo, titulo, quien, detalle });
const segundos = (desde: number, ahora: number) => Math.max(0, Math.round((ahora - desde) / 1000));

/** La llamada en la que está esta pestaña, si hay una. */
export function llamadaDe(e: EstadoVoz): Llamada | null {
    return e.fase === "sonando" || e.fase === "contestando" || e.fase === "en_curso" || e.fase === "terminada" ? e.llamada : null;
}

/** ¿Esta pestaña está ocupada con una llamada (contestándola o hablando)? */
export const ocupada = (e: EstadoVoz): boolean => e.fase === "contestando" || e.fase === "en_curso";

export function siguiente(e: EstadoVoz, ev: EventoVoz, yo: number): EstadoVoz {
    switch (ev.tipo) {
        case "entrante": {
            // Ocupada: no interrumpe. Sonando otra: se queda la primera (la segunda sonará al terminar, si sigue).
            if (ocupada(e) || esFinal(ev.llamada) || !ev.llamada.puede.contestar) return e;
            if (e.fase === "sonando" && e.llamada.id !== ev.llamada.id) return e;
            if (e.fase === "terminada") return e;
            return { fase: "sonando", llamada: ev.llamada };
        }
        case "actualizada": {
            const actual = llamadaDe(e);
            if (!actual || actual.id !== ev.llamada.id) return e;
            const l = ev.llamada;
            const deOtro = l.usuario !== undefined && l.usuario.id !== yo;
            if (e.fase === "sonando") {
                if (l.estado === "sonando") return l.puede.contestar ? { fase: "sonando", llamada: l } : LIBRE;
                // Otra persona la reclamó o ya habla: deja de sonar aquí, diciendo quién.
                if (l.estado === "conectando" || l.estado === "en_curso") return deOtro ? aviso("otro_la_contesto", l.titulo, l.usuario?.nombre) : LIBRE;
                // Terminó sin que nadie la contestara (colgó, se perdió): deja de sonar, sin más.
                return LIBRE;
            }
            if (e.fase === "contestando") {
                if (esFinal(l)) return aviso("termino_antes", l.titulo);
                if (deOtro && (l.estado === "conectando" || l.estado === "en_curso")) return aviso("otro_la_contesto", l.titulo, l.usuario?.nombre);
                // Volvió a `sonando`: el servidor soltó mi reserva (venció o Meta rechazó el `pre_accept`).
                if (l.estado === "sonando" && e.paso !== "reclamando") return aviso("reserva_vencida", l.titulo);
                return { ...e, llamada: l };
            }
            if (e.fase === "en_curso") {
                if (esFinal(l)) return { fase: "terminada", llamada: l, duracionSeg: l.duracionSeg ?? segundos(e.desde, Date.parse(l.terminadaEn ?? "") || e.desde), corte: l.motivoFin === "error_medio" };
                return { ...e, llamada: l };
            }
            if (e.fase === "terminada") return { ...e, llamada: l };
            return e;
        }
        case "contestar":
            return e.fase === "sonando" ? { fase: "contestando", llamada: e.llamada, paso: "reclamando", venceEn: null } : e;
        case "rechazar":
            return e.fase === "sonando" ? LIBRE : e;
        case "reclamada":
            return e.fase === "contestando" && e.paso === "reclamando" ? { fase: "contestando", llamada: ev.llamada, paso: "microfono", venceEn: ev.venceEn } : e;
        case "microfono_listo":
            return e.fase === "contestando" && e.paso === "microfono" ? { ...e, paso: "negociando" } : e;
        case "respuesta_enviada":
            return e.fase === "contestando" && e.paso === "negociando" ? { ...e, llamada: ev.llamada, paso: "confirmando" } : e;
        case "medio_confirmado":
            return e.fase === "contestando" && e.paso === "confirmando" ? { fase: "en_curso", llamada: ev.llamada, desde: ev.ahora, silenciada: false } : e;
        case "plazo_vencido":
            return e.fase === "contestando" ? aviso("reserva_vencida", e.llamada.titulo) : e;
        case "fallo":
            if (e.fase === "contestando" || e.fase === "sonando") return aviso(ev.motivo, e.llamada.titulo, ev.quien, ev.detalle);
            return e;
        case "otra_pestana":
            return e.fase === "contestando" || e.fase === "sonando" ? LIBRE : e;
        case "silenciar":
            return e.fase === "en_curso" ? { ...e, silenciada: ev.silenciada } : e;
        case "colgar":
            return e.fase === "en_curso" ? { fase: "terminada", llamada: e.llamada, duracionSeg: segundos(e.desde, ev.ahora), corte: false } : e;
        case "medio_caido":
            return e.fase === "en_curso" ? { fase: "terminada", llamada: e.llamada, duracionSeg: segundos(e.desde, ev.ahora), corte: true } : e;
        case "cerrar":
            return e.fase === "aviso" || e.fase === "terminada" ? LIBRE : e;
    }
}

/** La reserva de `reclamar` (§3.3, paso 4) y el plazo para `confirmar-medio` (paso 6). */
export const RESERVA_MS = 12_000;
export const CONFIRMAR_MS = 5_000;

/**
 * Cuánto esperar antes de dar la reserva por vencida. Se usa el `venceEn` del servidor solo si cae
 * dentro de la reserva; si el reloj de este equipo está desfasado (da un plazo ya pasado o más largo
 * que la reserva), valen los 12 s desde que llegó la respuesta. El servidor sigue mandando: si para
 * él ya venció, responde 409 `reserva_vencida`.
 */
export function plazoReserva(venceEn: string, ahora: number): number {
    const resto = Date.parse(venceEn) - ahora;
    return resto > 0 && resto <= RESERVA_MS ? resto : RESERVA_MS;
}

// --- Qué hace falta al pasar de un estado a otro (lo ejecuta el proveedor) ----------------------

/** ¿Debe sonar el timbre? Solo mientras la llamada espera a que alguien la conteste aquí. */
export const debeSonar = (e: EstadoVoz): boolean => e.fase === "sonando";

/**
 * Qué avisarle al servidor si la pestaña se cierra ahora (§3.4): `soltar` si ya la reclamé y no
 * llegué a hablar; `colgar` si estoy hablando; nada si solo suena (seguirá sonándoles a los demás).
 */
export function alCerrarPestana(e: EstadoVoz): { accion: "soltar" | "colgar"; id: string } | null {
    if (e.fase === "contestando" && e.paso !== "reclamando") return { accion: "soltar", id: e.llamada.id };
    if (e.fase === "en_curso") return { accion: "colgar", id: e.llamada.id };
    return null;
}

/** ¿Hay que soltar la reserva al llegar a este aviso? (la reclamé y no se pudo seguir) */
export function debeSoltar(antes: EstadoVoz, despues: EstadoVoz): string | null {
    if (antes.fase !== "contestando" || antes.paso === "reclamando" || despues.fase !== "aviso") return null;
    // Si otro ya la tiene o ya terminó, no hay reserva mía que soltar.
    return despues.motivo === "otro_la_contesto" || despues.motivo === "termino_antes" ? null : antes.llamada.id;
}
