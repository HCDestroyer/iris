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
    /** 409 `llamada_en_curso`: ya tengo otra llamada viva (en otra pestaña o navegador). */
    | "otra_llamada"
    /** 409 `llamada_ya_contestada` (`otro_navegador`): la reserva es de otro navegador mío. */
    | "otro_navegador"
    // --- Salientes (E1b) ---
    /** El cliente no contestó (o rechazó) en el minuto que deja WhatsApp. */
    | "no_contesto"
    /** El contacto no ha dado permiso (o WhatsApp no deja llamarlo ahora). */
    | "sin_permiso"
    /** El número no tiene las llamadas encendidas para esta empresa (o es prestado). */
    | "llamadas_inactivas"
    /** Límite de llamadas del día (de WhatsApp o de la empresa). */
    | "limite_llamadas"
    /** Se colgaron varias llamadas a este contacto antes de que contestara: hasta mañana. */
    | "limite_colgadas"
    /** La empresa llegó a su tope de minutos de salientes del mes. */
    | "tope_alcanzado"
    /** La llamada por devolver ya la cerró otra persona. */
    | "devolucion_cerrada"
    | "error";

export type EstadoVoz =
    | { fase: "libre" }
    | { fase: "sonando"; llamada: Llamada }
    | { fase: "contestando"; llamada: Llamada; paso: PasoContestar; venceEn: number | null }
    /**
     * Saliente (E1b): `preparando` (micrófono y oferta, aún no se pidió nada al servidor) y
     * `marcando` (el servidor ya tiene la llamada y WhatsApp le suena al cliente).
     */
    | { fase: "llamando"; paso: "preparando" | "marcando"; conversacionId: number; titulo: string; llamada: Llamada | null }
    | { fase: "en_curso"; llamada: Llamada; desde: number; silenciada: boolean }
    /** Terminó una llamada que sí se habló: se ofrece la nota. `corte`: se cayó el medio. */
    | { fase: "terminada"; llamada: Llamada; duracionSeg: number; corte: boolean }
    /** No se llegó a hablar: un mensaje llano y se cierra. */
    | { fase: "aviso"; motivo: MotivoAviso; quien?: string | undefined; titulo: string; detalle?: string | undefined; saliente?: true };

export type EventoVoz =
    /** Feed `llamada_entrante`, o la llamada sigue sonando al abrir el panel. */
    | { tipo: "entrante"; llamada: Llamada }
    /** Feed `llamada_actualizada` / `llamada_terminada`, o una relectura. `ahora`: para contar desde que contesta. */
    | { tipo: "actualizada"; llamada: Llamada; ahora?: number }
    /** Saliente: la persona tocó «Llamar». */
    | { tipo: "llamar"; conversacionId: number; titulo: string }
    /** Saliente: el servidor aceptó `llamar` y la llamada está `marcando`. */
    | { tipo: "marcando"; llamada: Llamada }
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
    /** El timbre llegó a su tope sin noticias de la llamada: deja de sonar aquí. */
    | { tipo: "caducar" }
    /** La persona se va (cierra sesión, cambia de empresa) con una llamada viva o sonando: se deja. */
    | { tipo: "abandonar" }
    /** Se cerró el aviso o la nota. */
    | { tipo: "cerrar" };

export const LIBRE: EstadoVoz = { fase: "libre" };

const FINALES: ReadonlySet<Llamada["estado"]> = new Set(["terminada", "perdida", "rechazada", "fuera_de_horario", "fallida"]);
export const esFinal = (l: Pick<Llamada, "estado">): boolean => FINALES.has(l.estado);

const aviso = (motivo: MotivoAviso, titulo: string, quien?: string, detalle?: string): EstadoVoz => ({ fase: "aviso", motivo, titulo, quien, detalle });
const segundos = (desde: number, ahora: number) => Math.max(0, Math.round((ahora - desde) / 1000));

/** La llamada en la que está esta pestaña, si hay una. */
export function llamadaDe(e: EstadoVoz): Llamada | null {
    return e.fase === "sonando" || e.fase === "contestando" || e.fase === "en_curso" || e.fase === "terminada" || e.fase === "llamando" ? e.llamada : null;
}

/** ¿Esta pestaña está ocupada con una llamada (contestándola, llamando o hablando)? */
export const ocupada = (e: EstadoVoz): boolean => e.fase === "contestando" || e.fase === "en_curso" || e.fase === "llamando";

/** Una saliente que terminó sin hablarse: qué decirle a quien llamó (o nada, si la colgó él). */
function finDeSaliente(l: Llamada): EstadoVoz {
    if (l.motivoFin === "tope_alcanzado") return deSaliente(aviso("tope_alcanzado", l.titulo));
    if (l.estado === "perdida" || l.estado === "rechazada") return deSaliente(aviso("no_contesto", l.titulo));
    if (l.estado === "fallida") return deSaliente(aviso("meta_rechazo", l.titulo));
    return LIBRE;
}

/** Marca el aviso como de una saliente: los textos hablan de llamar, no de contestar. */
const deSaliente = (e: EstadoVoz): EstadoVoz => (e.fase === "aviso" ? { ...e, saliente: true } : e);

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
            if (e.fase === "llamando") {
                // El cliente contestó: se habla. `marcando` → `en_curso` es con el `ACCEPTED` de WhatsApp.
                if (l.estado === "en_curso") return { fase: "en_curso", llamada: l, desde: ev.ahora ?? (Date.parse(l.contestadaEn ?? "") || Date.now()), silenciada: false };
                if (esFinal(l)) return finDeSaliente(l);
                return { ...e, llamada: l };
            }
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
        case "llamar":
            // Con una llamada entrando, viva o sin anotar no se empieza otra.
            return e.fase === "libre" || e.fase === "aviso" ? { fase: "llamando", paso: "preparando", conversacionId: ev.conversacionId, titulo: ev.titulo, llamada: null } : e;
        case "marcando":
            return e.fase === "llamando" && e.paso === "preparando" ? { ...e, paso: "marcando", llamada: ev.llamada } : e;
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
            // Saliente: pasó el minuto de WhatsApp (y un margen) sin que contestara.
            if (e.fase === "llamando") return deSaliente(aviso("no_contesto", e.titulo));
            return e.fase === "contestando" ? aviso("reserva_vencida", e.llamada.titulo) : e;
        case "fallo":
            if (e.fase === "llamando") return deSaliente(aviso(ev.motivo, e.titulo, ev.quien, ev.detalle));
            if (e.fase === "contestando" || e.fase === "sonando") return aviso(ev.motivo, e.llamada.titulo, ev.quien, ev.detalle);
            return e;
        case "otra_pestana":
            return e.fase === "contestando" || e.fase === "sonando" ? LIBRE : e;
        case "silenciar":
            return e.fase === "en_curso" ? { ...e, silenciada: ev.silenciada } : e;
        case "colgar":
            // Colgar mientras marca: no hubo llamada que anotar.
            if (e.fase === "llamando") return LIBRE;
            return e.fase === "en_curso" ? { fase: "terminada", llamada: e.llamada, duracionSeg: segundos(e.desde, ev.ahora), corte: false } : e;
        case "medio_caido":
            return e.fase === "en_curso" ? { fase: "terminada", llamada: e.llamada, duracionSeg: segundos(e.desde, ev.ahora), corte: true } : e;
        case "caducar":
            return e.fase === "sonando" ? LIBRE : e;
        case "abandonar":
            return e.fase === "sonando" || ocupada(e) ? LIBRE : e;
        case "cerrar":
            return e.fase === "aviso" || e.fase === "terminada" ? LIBRE : e;
    }
}

/** La reserva de `reclamar` (§3.3, paso 4) y el plazo para `confirmar-medio` (paso 6). */
export const RESERVA_MS = 12_000;
export const CONFIRMAR_MS = 5_000;

/*
 * Los plazos se cuentan desde que llega la respuesta de `reclamar` (o el evento de la llamada), no
 * con `venceEn` / `suenaHasta`: el reloj de este equipo puede ir adelantado o atrasado respecto al
 * servidor. El servidor sigue mandando: si para él la reserva ya venció responde 409
 * `reserva_vencida`, y cuando la llamada deja de sonar lo dice por el feed.
 */

// --- Qué hace falta al pasar de un estado a otro (lo ejecuta el proveedor) ----------------------

/** ¿Debe sonar el timbre? Solo mientras la llamada espera a que alguien la conteste aquí. */
export const debeSonar = (e: EstadoVoz): boolean => e.fase === "sonando";

/**
 * Hasta dónde pudo llegar el servidor con este intento de contestar. No es lo que se ve en pantalla
 * (`paso`): cambia **antes** de mandar cada petición, porque con la petición en vuelo el servidor
 * puede haberla aplicado ya aunque la respuesta no haya llegado.
 */
export type Etapa =
    /** `reclamar` salió y no ha respondido: puede que la reserva ya sea mía. */
    | "reclamando"
    /** `reclamar` respondió 200: la reserva es mía. */
    | "reservada"
    /** `contestar` salió: WhatsApp puede estar en `pre_accept`. */
    | "respondida"
    /** `confirmar-medio` salió: el servidor puede haber aceptado ya (llamada `en_curso`). */
    | "confirmando"
    | "en_curso"
    /** Saliente: micrófono y oferta; aún no salió `llamar` (o el servidor dijo que no la creó). */
    | "preparando"
    /** Saliente: `llamar` salió; el servidor puede tener ya la llamada marcando, haya respondido o no. */
    | "llamando";

/**
 * Al dejar de atender una llamada en esta pestaña (se cierra el audio local), qué hay que asegurar
 * en el servidor. Regla: **nunca se cierra el audio sin asegurar que el servidor termina la
 * llamada** si pudo haberla aceptado.
 *
 * - `colgar`: estaba en curso, o `confirmar-medio` ya había salido (pudo aceptarse sin audio).
 * - `soltar`: la reserva es mía y no llegó a aceptarse. Solo con `reservaSegura` (el candado del
 *   navegador garantiza que el `reclamar` que ganó fue el de esta pestaña): sin él, soltar podría
 *   liberar la reserva de otra pestaña o navegador de la misma persona; se deja vencer sola.
 * - `null`: nada mío que terminar (el servidor ya la terminó, la tiene otra persona, o `reclamar`
 *   sigue en vuelo: se suelta cuando llegue su respuesta).
 */
export function alSalir(antes: EstadoVoz, despues: EstadoVoz, etapa: Etapa, reservaSegura: boolean): "soltar" | "colgar" | null {
    if (ocupada(despues)) return null;
    if (antes.fase === "en_curso") {
        // Llegó por el feed ya terminada: no hay nada que colgar.
        return despues.fase === "terminada" && esFinal(despues.llamada) ? null : "colgar";
    }
    // Saliente: desde que sale `llamar` puede existir en el servidor (y sonarle al cliente): se cuelga
    // siempre, también si la respuesta se perdió. Colgar una que ya terminó no hace nada.
    if (antes.fase === "llamando") {
        if (despues.fase === "aviso" && despues.motivo === "otro_navegador") return null;
        return etapa === "preparando" ? null : "colgar";
    }
    if (antes.fase !== "contestando") return null;
    // La tengo en otro navegador mío, con su audio: aquí no se suelta ni se cuelga nada.
    if (despues.fase === "aviso" && despues.motivo === "otro_navegador") return null;
    if (etapa === "confirmando" || etapa === "en_curso") return "colgar";
    const ajena = despues.fase === "aviso" && (despues.motivo === "otro_la_contesto" || despues.motivo === "termino_antes");
    if (ajena || !reservaSegura || etapa === "reclamando") return null;
    return "soltar";
}

/**
 * Qué avisarle al servidor si la pestaña se cierra ahora (§3.4), con `keepalive`: `colgar` si hablo
 * o pudo aceptarse; `soltar` si la reserva es (o puede ser) mía; nada si solo suena.
 */
export function alCerrarPestana(e: EstadoVoz, etapa: Etapa, reservaSegura: boolean): { accion: "soltar" | "colgar"; id: string; propia?: true } | null {
    if (e.fase === "en_curso") return { accion: "colgar", id: e.llamada.id };
    // Saliente marcando: se cuelga la mía (sin el id, porque `llamar` no ha respondido, no se puede:
    // el servidor la corta sola a los 55 s sin el latido de este navegador).
    if (e.fase === "llamando") return e.llamada ? { accion: "colgar", id: e.llamada.id, propia: true } : null;
    if (e.fase !== "contestando") return null;
    // `confirmar-medio` salió y no se sabe si se aceptó: al cerrar no hay tiempo de leer de quién es
    // la llamada, así que se cuelga con `propia` (solo si la tengo yo: quien supervisa no cuelga por
    // accidente la de un compañero que la tomó después).
    if (etapa === "confirmando" || etapa === "en_curso") return { accion: "colgar", id: e.llamada.id, propia: true };
    // También con `reclamar` en vuelo: si no llegó a reservarse, el servidor responde 404 y no pasa nada.
    return reservaSegura ? { accion: "soltar", id: e.llamada.id } : null;
}

/**
 * Saliente: WhatsApp deja sonar 60 s y el servidor la da por perdida; si el aviso no llega, a los 70 s
 * se deja de esperar aquí (y se cuelga).
 */
export const MARCANDO_MAX_MS = 70_000;
/** Saliente contestada: cuánto se espera a que conecte el audio antes de colgarla (nunca `en_curso` sin audio). */
export const MEDIO_SALIENTE_MS = 15_000;

/** Tope del timbre: sin noticias de la llamada en este tiempo, deja de sonar aquí. */
export const TIMBRE_MAX_MS = 60_000;

