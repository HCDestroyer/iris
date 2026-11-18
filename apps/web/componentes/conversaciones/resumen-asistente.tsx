"use client";

import type { Schema } from "@iris/contratos";
import { Boton, Cargando } from "@iris/ui";
import { History, RotateCw, Sparkles } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { resumirConversacion, verResumen } from "@/lib/asistente/api";
import { explicarErrorDeApi, type ExplicacionError } from "@/lib/asistente/presentacion";
import type { EstadoAsistente, ResumenConversacion } from "@/lib/asistente/tipos";
import { horaLista } from "@/lib/conversaciones/presentacion";
import { ErrorAsistente } from "./error-asistente";
import { useAhora } from "./reloj";

function explicar(e: unknown): ExplicacionError {
    // La espera de un 429 se fija al llegar el error.
    if (e instanceof ErrorApi) return explicarErrorDeApi(e, "resumen", Date.now());
    return { titulo: "No se pudo resumir", texto: mensajeDeError(e).titulo, reintentar: true };
}

/** «Hay 2 mensajes nuevos desde este resumen». */
export function textoMensajesNuevos(n: number): string {
    return n === 1 ? "Hay 1 mensaje nuevo desde este resumen" : n > 1 ? `Hay ${n} mensajes nuevos desde este resumen` : "Hay mensajes nuevos desde este resumen";
}

/** «Generado por Andrea · hoy 10:42 · llama3.1:8b». */
export function pieDelResumen(r: Pick<ResumenConversacion, "generadoEn" | "modelo" | "pedidoPor">, ahora: number): string {
    return [`Generado${r.pedidoPor ? ` por ${r.pedidoPor.nombre.split(" ")[0]}` : ""}`, horaLista(r.generadoEn, ahora), r.modelo].filter(Boolean).join(" · ");
}

type Props = {
    conversacionId: number;
    estado: EstadoAsistente;
    /** El resumen guardado, como viene en el detalle (`resumen`); ausente si no hay uno vigente. */
    guardado?: Schema<"ResumenGuardadoRef"> | undefined;
};

/**
 * «Resumen del asistente» en la ficha del cliente (contratos/conversaciones.md §16.4). El resumen
 * **se guarda en la conversación**: si el detalle dice que hay uno, se lee al abrir (no gasta
 * tokens) con quién lo pidió, cuándo y con qué modelo; si hay mensajes después, lo dice y ofrece
 * «Actualizar resumen». Sin resumen (o cuando deja de venir: la conversación se reabrió o cambió
 * de cliente), el botón «Resumir». Generarlo es síncrono y puede tardar (hasta 90 s); si falla, el
 * resumen que había sigue a la vista.
 */
export function ResumenAsistente({ conversacionId, estado, guardado }: Props) {
    const ahora = useAhora(60_000);
    const [resumen, setResumen] = useState<ResumenConversacion | null>(null);
    const [generando, setGenerando] = useState(false);
    const [error, setError] = useState<ExplicacionError | null>(null);
    const [errorLectura, setErrorLectura] = useState<string | null>(null);
    const [intento, setIntento] = useState(0);
    // El detalle decía que había uno con este id, pero el servidor ya no lo tiene (404).
    const [sinResumenPara, setSinResumenPara] = useState<string | null>(null);
    const pedido = useRef(0);

    // El resumen dejó de venir en el detalle: se vuelve al botón «Resumir».
    const idGuardado = guardado?.id;
    const [idVisto, setIdVisto] = useState(idGuardado);
    if (idVisto !== idGuardado) {
        setIdVisto(idGuardado);
        setErrorLectura(null);
        if (idGuardado === undefined) setResumen(null);
    }

    // Hay uno guardado que aún no se tiene (al abrir la ficha, o lo regeneró otra persona): se lee.
    const falta = idGuardado !== undefined && resumen?.id !== idGuardado && sinResumenPara !== idGuardado;
    useEffect(() => {
        if (!falta) return;
        let vivo = true;
        verResumen(conversacionId).then(
            (r) => {
                if (vivo) setResumen(r);
            },
            (e: unknown) => {
                if (!vivo) return;
                // 404 `sin_resumen`: ya no hay uno vigente.
                if (e instanceof ErrorApi && e.estado === 404) {
                    setResumen(null);
                    setSinResumenPara(idGuardado ?? null);
                }
                else setErrorLectura(mensajeDeError(e).titulo);
            },
        );
        return () => {
            vivo = false;
        };
    }, [falta, conversacionId, idGuardado, intento]);

    async function resumir() {
        const n = ++pedido.current;
        setGenerando(true);
        setError(null);
        try {
            const r = await resumirConversacion(conversacionId, nuevaClaveIdempotencia());
            if (n === pedido.current) setResumen(r);
        } catch (err) {
            // El resumen anterior, si lo había, sigue a la vista.
            if (n === pedido.current) setError(explicar(err));
        } finally {
            if (n === pedido.current) setGenerando(false);
        }
    }

    const leyendo = falta && !errorLectura && !generando && resumen === null;
    // Lo más al día: el detalle (llega por el flujo) si es de este mismo resumen.
    const alDia = resumen && guardado && guardado.id === resumen.id ? guardado : resumen;
    const desactualizado = alDia?.desactualizado ?? false;
    return (
        <section aria-labelledby={`resumen-ia-${conversacionId}`} data-slot="resumen-asistente" data-resumen={resumen?.id} className="grid min-w-0 gap-2 border-b border-border px-4 py-3">
            <h3 id={`resumen-ia-${conversacionId}`} className="flex items-center gap-1.5 text-[0.8125rem] font-semibold text-primary">
                <Sparkles aria-hidden className="size-4 shrink-0" />
                Resumen del asistente
            </h3>
            {resumen ? (
                <div className="grid min-w-0 gap-1.5">
                    <p className="text-[0.8125rem] leading-relaxed break-words whitespace-pre-wrap">{resumen.texto}</p>
                    <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]" data-slot="resumen-pie">
                        {pieDelResumen(resumen, ahora)}
                    </p>
                    {desactualizado ? (
                        <p className="flex items-start gap-1.5 text-xs font-medium text-warning" data-slot="resumen-desactualizado">
                            <History aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                            <span className="min-w-0">{textoMensajesNuevos(alDia?.mensajesNuevos ?? 0)}</span>
                        </p>
                    ) : null}
                </div>
            ) : leyendo ? (
                <Cargando texto="Leyendo el resumen guardado…" visible />
            ) : errorLectura ? (
                <div className="grid gap-1.5">
                    <p role="alert" className="text-[0.8125rem] text-error [overflow-wrap:anywhere]">
                        No se pudo leer el resumen guardado: {errorLectura}
                    </p>
                    <div>
                        <Boton
                            variante="secundario"
                            tamano="sm"
                            onClick={() => {
                                setErrorLectura(null);
                                setIntento((n) => n + 1);
                            }}
                        >
                            <RotateCw />
                            Reintentar
                        </Boton>
                    </div>
                </div>
            ) : generando ? null : (
                <p className="text-[0.8125rem] text-muted-foreground">Qué pasó en esta conversación, en dos o tres líneas.</p>
            )}
            {generando ? (
                <div className="grid gap-1">
                    <Cargando texto={resumen ? "Actualizando el resumen…" : "Generando resumen…"} visible />
                    <p className="text-xs text-muted-foreground">Puede tardar hasta un minuto y medio.</p>
                </div>
            ) : error ? (
                <ErrorAsistente explicacion={error} alReintentar={() => void resumir()} />
            ) : estado.pausado ? (
                <p className="text-[0.8125rem] text-muted-foreground">En pausa: se llegó al tope de uso del mes.</p>
            ) : leyendo || errorLectura ? null : resumen ? (
                <div>
                    {desactualizado ? (
                        <Boton variante="secundario" tamano="sm" onClick={() => void resumir()}>
                            <Sparkles />
                            Actualizar resumen
                        </Boton>
                    ) : (
                        <Boton variante="enlace" tamano="sm" className="h-auto px-0 text-xs" onClick={() => void resumir()}>
                            Volver a resumir
                        </Boton>
                    )}
                </div>
            ) : (
                <div>
                    <Boton variante="secundario" tamano="sm" onClick={() => void resumir()}>
                        <Sparkles />
                        Resumir
                    </Boton>
                </div>
            )}
        </section>
    );
}
