// «Por recibir» (contrato Iris ↔ ArgosISP 0.5.0 §3.3): qué traslados le faltan por recibir al
// técnico y cómo va cada uno, y el formulario de recepción (lo que llegó por línea y por serie).
// Compartido con la app nativa.
import { z } from "zod";
import type { LineaRecepcion, Traslado } from "../contrato";
import { codigoProblema, ErrorTransporte } from "../errores";
import type { OperacionTrasladoLocal } from "../modelo";
import type { TonoEstado } from "./textos";

/**
 * - `por_recibir`: nadie lo recibió desde Iris (o Argos rechazó la recepción anterior).
 * - `en_telefono`: recibido en este teléfono, por enviar (o esperando su firma).
 * - `rechazado_telefono`: la API rechazó la recepción de este teléfono: Descartar o Rehacer.
 * - `enviando`: la API la tiene y la manda a Argos.
 * - `recibido`: Argos la aceptó.
 * - `rechazado_argos`: Argos la rechazó (código): se puede volver a recibir.
 * - `agotado`: Argos no respondió; lo ve el supervisor.
 */
export type SituacionTraslado = "por_recibir" | "en_telefono" | "rechazado_telefono" | "enviando" | "recibido" | "rechazado_argos" | "agotado";

export function situacionTraslado(traslado: Pick<Traslado, "id" | "recepcion">, recepciones: readonly OperacionTrasladoLocal[]): SituacionTraslado {
    const local = recepciones.find((op) => op.datos.trasladoId === traslado.id);
    if (local) {
        return local.estado === "rechazada" ? "rechazado_telefono" : "en_telefono";
    }
    switch (traslado.recepcion?.estado) {
        case "esperando_firma":
        case "enviando":
            return "enviando";
        case "recibida":
            return "recibido";
        case "rechazada":
            return "rechazado_argos";
        case "agotada":
            return "agotado";
        default:
            return "por_recibir";
    }
}

/** Traslados que el técnico todavía tiene que recibir (para el contador de «Por recibir»). */
export function porRecibir(traslados: readonly Traslado[], recepciones: readonly OperacionTrasladoLocal[]): Traslado[] {
    return traslados.filter((t) => t.estado === "enviado" && ["por_recibir", "rechazado_argos"].includes(situacionTraslado(t, recepciones)));
}

/**
 * Qué hacer con una recepción rechazada en el teléfono: `traslado_no_encontrado` (Argos no tiene
 * ese traslado para él) solo se descarta; el resto (p. ej. 24 h sin respuesta de Argos) se rehace.
 */
export function sePuedeRehacerRecepcion(op: Pick<OperacionTrasladoLocal, "error">): boolean {
    return codigoProblema(op.error) !== "traslado_no_encontrado";
}

/** Formulario de recepción: por línea, si llegó completa y, si no, cuánto llegó y qué series. */
export const esquemaFormularioRecepcion = z.object({
    lineas: z.array(
        z
            .object({
                total: z.number(),
                falta: z.boolean(),
                cantidad: z.string(),
                /** Por serie del traslado: ¿llegó? */
                series: z.array(z.boolean()),
            })
            .superRefine((l, ctx) => {
                if (!l.falta || l.series.length > 0) {
                    return;
                }
                const n = Number(l.cantidad.replace(",", "."));
                if (l.cantidad.trim() === "" || !Number.isFinite(n) || n < 0) {
                    ctx.addIssue({ code: "custom", path: ["cantidad"], message: "Escribe cuánto llegó" });
                } else if (n >= l.total) {
                    ctx.addIssue({ code: "custom", path: ["cantidad"], message: `Si faltó algo, es menos de ${l.total}` });
                }
            }),
    ),
});
export type FormularioRecepcion = z.infer<typeof esquemaFormularioRecepcion>;

/** Valores iniciales: todo llegó. */
export function formularioRecepcionInicial(traslado: Pick<Traslado, "lineas">): FormularioRecepcion {
    return {
        lineas: (traslado.lineas ?? []).map((l) => ({ total: l.cantidad, falta: false, cantidad: "", series: (l.series ?? []).map(() => true) })),
    };
}

/**
 * `lineas` de `traslado.recibir`: solo las que faltaron (si llegó todo, ninguna). Con series, lo
 * recibido es cuántas llegaron y las que no van en `seriesFaltantes`.
 */
export function lineasDeRecepcion(traslado: Pick<Traslado, "lineas">, datos: FormularioRecepcion): LineaRecepcion[] {
    const lineas: LineaRecepcion[] = [];
    (traslado.lineas ?? []).forEach((linea, i) => {
        const f = datos.lineas[i];
        if (!f?.falta || linea.lineaId <= 0) {
            return;
        }
        const series = linea.series ?? [];
        if (series.length > 0) {
            const faltantes = series.filter((_, j) => f.series[j] === false);
            if (faltantes.length === 0) {
                return;
            }
            lineas.push({ lineaId: linea.lineaId, cantidadRecibida: series.length - faltantes.length, seriesFaltantes: faltantes });
            return;
        }
        lineas.push({ lineaId: linea.lineaId, cantidadRecibida: Number(f.cantidad.replace(",", ".")) });
    });
    return lineas;
}

/** Situación de un traslado en palabras del técnico y su tono (el icono lo pone cada app). */
export const SITUACIONES_TRASLADO: Record<SituacionTraslado, { tono: TonoEstado; texto: string }> = {
    por_recibir: { tono: "neutral", texto: "Por recibir" },
    en_telefono: { tono: "warning", texto: "Recibido · por enviar" },
    rechazado_telefono: { tono: "error", texto: "Rechazado" },
    enviando: { tono: "info", texto: "Enviando a Argos" },
    recibido: { tono: "success", texto: "Recibido" },
    rechazado_argos: { tono: "error", texto: "Argos lo rechazó" },
    agotado: { tono: "warning", texto: "Argos no respondió" },
};

/** Situación de una recepción que solo está en el teléfono (la lista de Argos no la trae). */
export function situacionLocal(op: Pick<OperacionTrasladoLocal, "estado">): SituacionTraslado {
    return op.estado === "rechazada" ? "rechazado_telefono" : "en_telefono";
}

/** Problemas al leer el inventario de Argos («Por recibir», «Mi bodega») en palabras del técnico. */
export const PROBLEMAS_INVENTARIO: Record<string, string> = {
    tecnico_sin_vincular: "Tu ficha no está vinculada con tu usuario de Argos. Pide a tu supervisor que la vincule.",
    argos_sin_inventario_tecnico: "Argos todavía no maneja inventario por técnico.",
    tecnico_sin_bodega: "No tienes bodega en Argos. Pide a bodega que te asigne una.",
    tecnico_ambiguo: "Argos tiene más de un usuario para ti. Pide a tu supervisor que revise el vínculo.",
    conector_apagado: "Iris no está conectado a Argos en este momento.",
    empresa_requerida: "Elige la empresa en la que trabajas.",
};

/** Error de una lectura del inventario: sin señal (aviso) o un problema de Argos (error). */
export function textoErrorLectura(error: unknown, que: string): { sinSenal: boolean; texto: string } {
    if (error instanceof ErrorTransporte && error.tipo === "red") {
        return { sinSenal: true, texto: `Sin señal: conéctate para ver ${que}.` };
    }
    const codigo = error instanceof ErrorTransporte ? error.codigo : null;
    return { sinSenal: false, texto: (codigo ? PROBLEMAS_INVENTARIO[codigo] : undefined) ?? `No se pudo leer ${que}.` };
}

/** Modalidad de una existencia de «Mi bodega». */
export const MODALIDADES_BODEGA: Record<string, string> = {
    comodato: "Comodato",
    consumible_cobro: "Se cobra al cliente",
    consumible_gasto: "Gasto",
};
