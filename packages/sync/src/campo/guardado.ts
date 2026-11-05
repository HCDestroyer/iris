// Un solo «Guardar» en los pasos de la orden (PWA y app nativa). Cada cambio del técnico sigue
// entrando a la cola sin conexión al hacerlo (marcar, elegir, tomar una foto) o al salir del campo
// (texto, número, medición). Lo que todavía se está escribiendo es un borrador: el «Guardar» de la
// isla los guarda todos de una vez y dice qué pasó. Sin DOM ni React.

/** Espera de la búsqueda en vivo: filtra al dejar de escribir este tiempo (ms). */
export const ESPERA_BUSQUEDA_MS = 200;

/** Resultado de guardar un borrador: `true` si entró a la cola; si no, el mensaje del error. */
export type ResultadoBorrador = true | string;

export interface Borrador {
    /** Nombre del campo («Potencia óptica»): va en el aviso si no se pudo guardar. */
    etiqueta: string;
    /** ¿Hay algo escrito que todavía no está en la cola? */
    pendiente: () => boolean;
    /** Lo valida y lo pone en la cola. */
    guardar: () => Promise<ResultadoBorrador>;
}

export interface ResumenGuardado {
    /** Borradores que entraron a la cola en esta pasada. */
    guardados: number;
    /** Los que no pasaron la validación (quedan escritos para corregirlos). */
    errores: { etiqueta: string; mensaje: string }[];
}

/**
 * Borradores del paso abierto. Cada campo se registra al montarse (y se quita al desmontarse);
 * `avisar` lo llama el campo cuando cambia si está pendiente, para el contador del «Guardar».
 */
export class RegistroBorradores {
    private readonly borradores = new Map<string, Borrador>();
    private readonly oyentes = new Set<() => void>();
    private cuenta = 0;

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

    avisar(): void {
        const cuenta = [...this.borradores.values()].filter((b) => b.pendiente()).length;
        if (cuenta !== this.cuenta) {
            this.cuenta = cuenta;
            for (const o of this.oyentes) {
                o();
            }
        }
    }

    /** Guarda todos los pendientes, en el orden en que se registraron. Nunca lanza. */
    async guardarTodo(): Promise<ResumenGuardado> {
        const resumen: ResumenGuardado = { guardados: 0, errores: [] };
        for (const b of [...this.borradores.values()]) {
            if (!b.pendiente()) {
                continue;
            }
            let r: ResultadoBorrador;
            try {
                r = await b.guardar();
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
}

/**
 * Aviso tras tocar «Guardar»: lo que se guardó ahora, lo que falló (el campo y su error) o, si no
 * había nada escrito, que todo ya estaba guardado y cuánto falta por enviar.
 */
export function avisoGuardado(r: ResumenGuardado, envio: { porEnviar: number; enLinea: boolean }): { tono: "success" | "error" | "info"; texto: string } {
    const primero = r.errores[0];
    if (primero) {
        const otros = r.errores.length - 1;
        return { tono: "error", texto: `No se guardó «${primero.etiqueta}»: ${primero.mensaje}${otros > 0 ? ` (y ${otros} más)` : ""}` };
    }
    const destino = envio.enLinea ? "Enviando…" : "Se enviarán al volver la señal.";
    if (r.guardados > 0) {
        return { tono: "success", texto: `${r.guardados === 1 ? "Cambio guardado" : `${r.guardados} cambios guardados`} en el teléfono. ${destino}` };
    }
    if (envio.porEnviar > 0) {
        return { tono: "info", texto: `Todo está guardado en el teléfono · ${envio.porEnviar} por enviar. ${destino}` };
    }
    return { tono: "success", texto: "Todo está guardado y enviado." };
}

/** Texto del aviso de pausa en los pasos (los dos lados dicen lo mismo). */
export const TEXTO_EN_PAUSA = "Con la orden en pausa no se registra trabajo: el checklist, los formularios, los materiales, los equipos, las fotos y la firma quedan de solo lectura. Reanúdala para seguir.";
