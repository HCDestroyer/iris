import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { colgarLlamada, leerLlamada, soltarLlamada } from "./api";
import { esFinal } from "./maquina";
import type { Llamada } from "./tipos";

/*
 * Asegurar en el servidor el final de una llamada que esta pestaña deja de atender. Regla: nunca se
 * cierra el audio local dejando al cliente en una línea muerta. Si la llamada pudo aceptarse, se
 * cuelga; si solo estaba reservada, se suelta; y si soltar no sale, se comprueba y se cuelga.
 */

export type Contexto = {
    /** La empresa de la llamada (no la activa del panel, que puede haber cambiado). */
    empresa: number | undefined;
    yoId: number;
    /** Para pruebas. */
    esperar?: (ms: number) => Promise<void>;
};

export type Fin =
    /** El servidor la colgó ahora: estaba viva. */
    | { tipo: "colgada"; llamada: Llamada }
    /** Se soltó la reserva: vuelve a sonarles a los demás. */
    | { tipo: "soltada" }
    /** No había nada mío que terminar (ya terminó, o la tiene otra persona). */
    | { tipo: "nada" }
    /** No se pudo asegurar: hay que decirlo y ofrecer reintentar. */
    | { tipo: "fallo"; error: unknown };

const pausa = (ms: number) => new Promise<void>((resolver) => window.setTimeout(resolver, ms));
const accionEnCurso = (e: unknown) => e instanceof ErrorApi && e.codigo === "accion_en_curso";
/** El servidor dice que ya no hay llamada mía que colgar. */
const yaNoEsta = (e: unknown) => e instanceof ErrorApi && (e.estado === 404 || e.codigo === "llamada_terminada");

/** Esperas entre intentos de colgar (red caída, 5xx, WhatsApp aún respondiendo): unos 23 s en total. */
export const ESPERAS_COLGAR = [0, 1000, 2000, 4000, 8000, 8000] as const;
export const ESPERA_SOLTAR_MS = 2000;

/** Mientras la llamada sigue `conectando` a mi nombre se relee cada segundo, como mucho este número de veces. */
export const ESPERA_ACEPTAR_MS = 1000;
export const LECTURAS_CONECTANDO = 30;

/**
 * Cuelga con reintentos. `soloSiEsMia`: antes comprueba que la llamada sigue viva **a mi nombre**
 * (quien supervisa puede colgar la de otra persona: nunca se cuelga a ciegas la de un compañero) y
 * la cuelga con `?propia=true`, que el servidor solo acepta de quien la tiene.
 *
 * Con `confirmar-medio` en vuelo la llamada está `conectando`, reservada a mi nombre. El servidor
 * la termina con `propia` (200 con `perdida` o `terminada`: final). Uno anterior responde 404, y
 * ese 404 **no es el final**: si el `accept`
 * termina después, queda `en_curso` sin audio. Mientras la lectura diga que sigue reservada a mi
 * nombre se espera y se relee, hasta que pase a `en_curso` (y entonces se cuelga) o termine o
 * vuelva a sonar (la reserva venció: no hay nada mío).
 */
export async function colgarSeguro(id: string, ctx: Contexto, soloSiEsMia: boolean): Promise<Fin> {
    const esperar = ctx.esperar ?? pausa;
    let ultimo: unknown = null;
    if (!soloSiEsMia) {
        for (const espera of ESPERAS_COLGAR) {
            if (espera) await esperar(espera);
            try {
                return { tipo: "colgada", llamada: await colgarLlamada(id, nuevaClaveIdempotencia(), ctx.empresa) };
            } catch (e) {
                if (yaNoEsta(e)) return { tipo: "nada" };
                ultimo = e;
            }
        }
        return { tipo: "fallo", error: ultimo };
    }
    let fallos = 0;
    let conectando = 0;
    while (fallos < ESPERAS_COLGAR.length && conectando < LECTURAS_CONECTANDO) {
        let l: Llamada;
        try {
            l = await leerLlamada(id, undefined, ctx.empresa);
        } catch (e) {
            // No existe (o no la veo): no hay nada mío.
            if (yaNoEsta(e)) return { tipo: "nada" };
            ultimo = e;
            fallos += 1;
            await esperar(ESPERAS_COLGAR[fallos] ?? 8000);
            continue;
        }
        if (esFinal(l) || l.estado === "sonando" || l.usuario?.id !== ctx.yoId) return { tipo: "nada" };
        if (l.estado === "conectando") {
            // Reservada a mi nombre y aún sin aceptar. El servidor nuevo la termina con `propia`
            // (200: queda `perdida` con `agente_salio`, o `terminada`): eso es el final. El anterior
            // responde 404: no es el final (puede aceptarse en cualquier momento); se espera y se relee.
            try {
                return { tipo: "colgada", llamada: await colgarLlamada(id, nuevaClaveIdempotencia(), ctx.empresa, true) };
            } catch (e) {
                if (e instanceof ErrorApi && e.codigo === "llamada_terminada") return { tipo: "nada" };
                ultimo = e;
            }
            conectando += 1;
            await esperar(ESPERA_ACEPTAR_MS);
            continue;
        }
        try {
            return { tipo: "colgada", llamada: await colgarLlamada(id, nuevaClaveIdempotencia(), ctx.empresa, true) };
        } catch (e) {
            if (e instanceof ErrorApi && e.codigo === "llamada_terminada") return { tipo: "nada" };
            // Un 404 aquí no es final: la lectura acaba de decir que sigue viva a mi nombre.
            ultimo = e;
            fallos += 1;
            await esperar(ESPERAS_COLGAR[fallos] ?? 8000);
        }
    }
    return { tipo: "fallo", error: ultimo };
}

/**
 * Suelta la reserva. 409 `accion_en_curso` (WhatsApp aún responde a un `pre_accept` o `accept`): se
 * reintenta a los pocos segundos. Si soltar falla por otra cosa, la llamada pudo haberse aceptado:
 * se comprueba y, si sigue a mi nombre, se cuelga.
 */
export async function soltarSeguro(id: string, motivo: string, ctx: Contexto): Promise<Fin> {
    const esperar = ctx.esperar ?? pausa;
    for (let vez = 0; vez < 4; vez++) {
        try {
            await soltarLlamada(id, motivo, ctx.empresa);
            return { tipo: "soltada" };
        } catch (e) {
            // La reserva es de otro navegador mío (o de otra pestaña): no es de aquí y no se toca.
            if (e instanceof ErrorApi && e.codigo === "llamada_ya_contestada" && (e.errores[0]?.rule === "otro_navegador" || e.errores[0]?.rule === "misma_persona")) return { tipo: "nada" };
            if (!accionEnCurso(e)) break;
            await esperar(ESPERA_SOLTAR_MS);
        }
    }
    return colgarSeguro(id, ctx, true);
}
