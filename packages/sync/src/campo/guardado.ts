// La isla de los pasos de la orden (PWA y app nativa). Cada cambio del técnico sigue entrando a la
// cola sin conexión al hacerlo (marcar, elegir, tomar una foto) o al salir del campo (texto, número,
// medición). Lo que todavía se está escribiendo es un borrador: sin borradores la isla no ofrece
// «Guardar» (no hay nada que guardar) y su botón primario solo avanza; con borradores, el primario es
// «Guardar y seguir» con el contador, que los guarda todos y avanza (si algo no valida, se queda y lo
// dice). Al guardar, la isla confirma «Guardado» un momento. Sin DOM ni React.
import { TITULOS_PASO, type PasoOrden } from "./pasos";
import { vecinosPaso } from "./vista";

/** Espera de la búsqueda en vivo: filtra al dejar de escribir este tiempo (ms). */
export const ESPERA_BUSQUEDA_MS = 200;

/** Resultado de guardar un borrador: `true` si entró a la cola; si no, el mensaje del error. */
export type ResultadoBorrador = true | string;

export interface Borrador {
    /** Nombre del campo («Potencia óptica»): va en el aviso si no se pudo guardar. */
    etiqueta: string;
    /** ¿Hay algo escrito que todavía no está en la cola? */
    pendiente: () => boolean;
    /**
     * Lo valida y lo pone en la cola. `silencioso`: sin el aviso de éxito del campo (lo guarda la
     * isla, que confirma «Guardado»); los errores se dicen igual.
     */
    guardar: (opciones?: { silencioso?: boolean }) => Promise<ResultadoBorrador>;
}

export interface ResumenGuardado {
    /** Borradores que entraron a la cola en esta pasada. */
    guardados: number;
    /** Los que no pasaron la validación (quedan escritos para corregirlos). */
    errores: { etiqueta: string; mensaje: string }[];
}

/**
 * Borradores del paso abierto. Cada campo se registra al montarse (y se quita al desmontarse);
 * `avisar` lo llama el campo cuando cambia si está pendiente, para el contador de «Guardar y seguir».
 * También lleva la confirmación «Guardado» de la isla (`guardado`), que dura `MS_GUARDADO` y sigue
 * al pasar de paso (el registro es de toda la orden, no del paso).
 */
export class RegistroBorradores {
    private readonly borradores = new Map<string, Borrador>();
    private readonly oyentes = new Set<() => void>();
    private cuenta = 0;
    private recien: string | null = null;
    private reloj: ReturnType<typeof setTimeout> | null = null;

    registrar(clave: string, borrador: Borrador): () => void {
        this.borradores.set(clave, borrador);
        this.avisar();
        return () => {
            if (this.borradores.get(clave) === borrador) {
                this.borradores.delete(clave);
                this.avisar();
            }
        };
    }

    /** Cuántos campos tienen algo escrito sin guardar. */
    pendientes = (): number => this.cuenta;

    suscribir = (oyente: () => void): (() => void) => {
        this.oyentes.add(oyente);
        return () => {
            this.oyentes.delete(oyente);
        };
    };

    /** Lo que oye el lector de pantalla mientras se ve «Guardado» en la isla; `null` si no se ve. */
    guardado = (): string | null => this.recien;

    avisar(): void {
        const cuenta = [...this.borradores.values()].filter((b) => b.pendiente()).length;
        if (cuenta !== this.cuenta) {
            this.cuenta = cuenta;
            this.emitir();
        }
    }

    private emitir(): void {
        for (const o of this.oyentes) {
            o();
        }
    }

    /** Guarda todos los pendientes, en el orden en que se registraron. Nunca lanza. */
    async guardarTodo(): Promise<ResumenGuardado> {
        return this.guardarLos([...this.borradores.values()]);
    }

    /**
     * Guarda un solo campo (al salir de él), también en silencio: lo confirma la isla con
     * «Guardado». Así, tocar «Guardar y seguir» con el teclado abierto (el campo se guarda al perder
     * el foco, justo antes del toque) dice lo mismo que guardarlo con el botón.
     */
    async guardarUno(clave: string): Promise<ResumenGuardado> {
        const b = this.borradores.get(clave);
        return this.guardarLos(b ? [b] : []);
    }

    private async guardarLos(borradores: Borrador[]): Promise<ResumenGuardado> {
        const resumen: ResumenGuardado = { guardados: 0, errores: [] };
        for (const b of borradores) {
            if (!b.pendiente()) {
                continue;
            }
            let r: ResultadoBorrador;
            try {
                r = await b.guardar({ silencioso: true });
            } catch (e) {
                r = e instanceof Error && e.message ? e.message : "No se pudo guardar en el teléfono.";
            }
            if (r === true) {
                resumen.guardados += 1;
            } else {
                resumen.errores.push({ etiqueta: b.etiqueta, mensaje: r });
            }
        }
        this.avisar();
        return resumen;
    }

    /**
     * Lo que se dice tras guardar (`avisoGuardado`): si fue a la isla, muestra «Guardado» por
     * `MS_GUARDADO`. Devuelve el aviso para que quien guardó muestre el error arriba.
     */
    confirmar(r: ResumenGuardado, envio: { enLinea: boolean }): AvisoGuardado {
        const aviso = avisoGuardado(r, envio);
        if (aviso?.en === "isla") {
            if (this.reloj) {
                clearTimeout(this.reloj);
            }
            this.recien = aviso.detalle;
            this.emitir();
            this.reloj = setTimeout(() => {
                this.reloj = null;
                this.recien = null;
                this.emitir();
            }, MS_GUARDADO);
        }
        return aviso;
    }
}

/** Lo que dura la confirmación «Guardado» en la isla (ms): breve, sin tapar nada. */
export const MS_GUARDADO = 1500;

/**
 * Qué se dice tras guardar los borradores del paso: un error va arriba, como aviso emergente (el
 * campo y su error, y el técnico se queda en el paso); lo guardado se confirma en la isla con
 * «Guardado» (`detalle` es lo que oye el lector de pantalla: cuántos y cuándo se envían). Sin nada
 * guardado ni errores no se dice nada.
 */
export type AvisoGuardado = { en: "aviso"; tono: "error"; texto: string } | { en: "isla"; tono: "success"; texto: "Guardado"; detalle: string } | null;

export function avisoGuardado(r: ResumenGuardado, envio: { enLinea: boolean }): AvisoGuardado {
    const primero = r.errores[0];
    if (primero) {
        const otros = r.errores.length - 1;
        return { en: "aviso", tono: "error", texto: `No se guardó «${primero.etiqueta}»: ${primero.mensaje}${otros > 0 ? ` (y ${otros} más)` : ""}` };
    }
    if (r.guardados === 0) {
        return null;
    }
    const destino = envio.enLinea ? "Enviando…" : "Se enviarán al volver la señal.";
    return { en: "isla", tono: "success", texto: "Guardado", detalle: `Guardado: ${r.guardados === 1 ? "1 cambio" : `${r.guardados} cambios`} en el teléfono. ${destino}` };
}

/** Botón primario de la isla de pasos. */
export interface PrimarioIsla {
    /** `navegar`: solo pasa de paso. `guardar_y_seguir`: guarda los borradores y, si validan, pasa. */
    accion: "navegar" | "guardar_y_seguir";
    /** Lo que se lee en el botón: «Siguiente», «Ir a firma», «Terminar» o «Guardar y seguir». */
    texto: string;
    /** Nombre accesible (empieza con el texto visible): «Siguiente: fotos», «Guardar y seguir (2 sin guardar)». */
    etiqueta: string;
    icono: "siguiente" | "firma" | "terminar" | "guardar";
    /** Campos sin guardar (el contador del botón), solo con `guardar_y_seguir`. */
    cuenta: number | null;
    /** A qué paso lleva; `null`: al resumen de la orden. */
    destino: PasoOrden | null;
}

export interface EstadoIslaPasos {
    anterior: { texto: "Anterior" | "Resumen"; destino: PasoOrden | null };
    primario: PrimarioIsla;
    /** ¿Se ve la confirmación «Guardado»? (solo sin borradores nuevos). */
    guardado: boolean;
}

/**
 * Estado de la isla de los pasos: «Anterior» y un primario que avanza. Sin cambios por guardar, el
 * primario es «Siguiente» (en el último paso antes de la firma, «Ir a firma»; si no hay más pasos,
 * «Terminar», que vuelve al resumen) y no hay «Guardar». Con cambios, «Guardar y seguir» con el
 * contador. Con la orden en pausa todo es de solo lectura: nunca guarda, solo navega.
 */
export function estadoIslaPasos(e: { paso: PasoOrden; pendientes: number; enPausa: boolean; recienGuardado: boolean }): EstadoIslaPasos {
    const { anterior, siguiente } = vecinosPaso(e.paso);
    const guardar = !e.enPausa && e.pendientes > 0;
    let primario: PrimarioIsla;
    if (guardar) {
        primario = { accion: "guardar_y_seguir", texto: "Guardar y seguir", etiqueta: `Guardar y seguir (${e.pendientes} sin guardar)`, icono: "guardar", cuenta: e.pendientes, destino: siguiente };
    } else if (siguiente === "cierre") {
        primario = { accion: "navegar", texto: "Ir a firma", etiqueta: "Ir a firma y cierre", icono: "firma", cuenta: null, destino: siguiente };
    } else if (siguiente) {
        primario = { accion: "navegar", texto: "Siguiente", etiqueta: `Siguiente: ${TITULOS_PASO[siguiente].toLowerCase()}`, icono: "siguiente", cuenta: null, destino: siguiente };
    } else {
        primario = { accion: "navegar", texto: "Terminar", etiqueta: "Terminar: volver al resumen", icono: "terminar", cuenta: null, destino: null };
    }
    return {
        anterior: anterior ? { texto: "Anterior", destino: anterior } : { texto: "Resumen", destino: null },
        primario,
        guardado: e.recienGuardado && !guardar,
    };
}

/** Texto del aviso de pausa en los pasos (los dos lados dicen lo mismo). */
export const TEXTO_EN_PAUSA = "Con la orden en pausa no se registra trabajo: el checklist, los formularios, los materiales, los equipos, las fotos y la firma quedan de solo lectura. Reanúdala para seguir.";
