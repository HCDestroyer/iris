"use client";

import { Boton, Cargando } from "@iris/ui";
import { Sparkles } from "lucide-react";
import { useRef, useState } from "react";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { resumirConversacion } from "@/lib/asistente/api";
import { explicarErrorDeApi, type ExplicacionError } from "@/lib/asistente/presentacion";
import type { EstadoAsistente, ResumenConversacion } from "@/lib/asistente/tipos";
import { horaLista } from "@/lib/conversaciones/presentacion";
import { ErrorAsistente } from "./error-asistente";
import { useAhora } from "./reloj";

type Estado = { tipo: "inicio" } | { tipo: "generando" } | { tipo: "listo"; resumen: ResumenConversacion } | { tipo: "error"; explicacion: ExplicacionError };

function explicar(e: unknown): ExplicacionError {
    // La espera de un 429 se fija al llegar el error.
    if (e instanceof ErrorApi) return explicarErrorDeApi(e, "resumen", Date.now());
    return { titulo: "No se pudo resumir", texto: mensajeDeError(e).titulo, reintentar: true };
}

/**
 * «Resumen del asistente» en la ficha del cliente (contratos/conversaciones.md §16.4): a pedido,
 * con «Resumir». Es síncrono y puede tardar (hasta 90 s): mientras, «Generando resumen…». El
 * servidor no lo guarda: al cambiar de conversación se pide de nuevo.
 */
export function ResumenAsistente({ conversacionId, estado }: { conversacionId: number; estado: EstadoAsistente }) {
    const [e, setE] = useState<Estado>({ tipo: "inicio" });
    const ahora = useAhora(60_000);
    const pedido = useRef(0);

    async function resumir() {
        const n = ++pedido.current;
        setE({ tipo: "generando" });
        try {
            const r = await resumirConversacion(conversacionId, nuevaClaveIdempotencia());
            if (n === pedido.current) setE({ tipo: "listo", resumen: r });
        } catch (err) {
            if (n === pedido.current) setE({ tipo: "error", explicacion: explicar(err) });
        }
    }

    return (
        <section aria-labelledby={`resumen-ia-${conversacionId}`} data-slot="resumen-asistente" className="grid min-w-0 gap-2 border-b border-border px-4 py-3">
            <h3 id={`resumen-ia-${conversacionId}`} className="flex items-center gap-1.5 text-[0.8125rem] font-semibold text-primary">
                <Sparkles aria-hidden className="size-4 shrink-0" />
                Resumen del asistente
            </h3>
            {estado.pausado ? (
                <p className="text-[0.8125rem] text-muted-foreground">En pausa: se llegó al tope de uso del mes.</p>
            ) : e.tipo === "inicio" ? (
                <div className="grid gap-2">
                    <p className="text-[0.8125rem] text-muted-foreground">Qué pasó en esta conversación, en dos o tres líneas.</p>
                    <div>
                        <Boton variante="secundario" tamano="sm" onClick={() => void resumir()}>
                            <Sparkles />
                            Resumir
                        </Boton>
                    </div>
                </div>
            ) : e.tipo === "generando" ? (
                <div className="grid gap-1">
                    <Cargando texto="Generando resumen…" visible />
                    <p className="text-xs text-muted-foreground">Puede tardar hasta un minuto y medio.</p>
                </div>
            ) : e.tipo === "error" ? (
                <ErrorAsistente explicacion={e.explicacion} alReintentar={() => void resumir()} />
            ) : (
                <div className="grid gap-1.5">
                    <p className="text-[0.8125rem] leading-relaxed break-words whitespace-pre-wrap">{e.resumen.texto}</p>
                    <p className="flex min-w-0 flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                        <span>Generado {horaLista(e.resumen.generadoEn, ahora)}</span>
                        <Boton variante="enlace" tamano="sm" className="h-auto px-0 text-xs" onClick={() => void resumir()}>
                            Volver a resumir
                        </Boton>
                    </p>
                </div>
            )}
        </section>
    );
}
