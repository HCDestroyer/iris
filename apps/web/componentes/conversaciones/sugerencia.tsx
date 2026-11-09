"use client";

import { avisar, Boton, cn, Esqueleto } from "@iris/ui";
import { BookOpen, Check, ChevronDown, CircleCheck, Eye, History, RefreshCw, RotateCw, Sparkles, Tag, ThumbsDown, ThumbsUp, WifiOff, X, type LucideIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { calificarSugerencia, descartarSugerencia, leerEstadoAsistente, leerSugerenciaActual, pedirSugerencia, usarSugerencia } from "@/lib/asistente/api";
import { aplicarSugerencia, explicarError, explicarErrorDeApi, type ExplicacionError } from "@/lib/asistente/presentacion";
import { ErrorAsistente } from "./error-asistente";
import { PERMISO_SUGERENCIAS } from "@/lib/asistente/permisos";
import type { EstadoAsistente, FuenteSugerencia, Sugerencia } from "@/lib/asistente/tipos";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import type { Conversacion } from "@/lib/conversaciones/tipos";
import { useSesion } from "@/lib/sesion";
import type { OyenteFeed } from "./bandeja";

/*
 * Asistente de IA en la conversación (contratos/conversaciones.md §16.2; diseño del PM): la
 * tarjeta «Sugerencia del asistente» encima del compositor y, en modo `boton`, «Sugerir» junto a
 * él. El asistente nunca envía: «Usar» pasa el texto a la caja y la persona lo revisa y lo envía
 * (con `sugerenciaId`). Solo la ve quien tiene `conversaciones.sugerencias`.
 */

export type Asistente = {
    /** Estado del asistente de la empresa; `null` sin permiso, leyendo o si no se pudo leer. */
    estado: EstadoAsistente | null;
    sugerencia: Sugerencia | null;
    /** Error al pedir una (422 `tope_alcanzado`, 429, sin red…): se muestra en lugar de la tarjeta. */
    errorPedido: ExplicacionError | null;
    pidiendo: boolean;
    pedir: (otra: boolean) => Promise<void>;
    /** Repite el último pedido («Sugerir» u «Otra»). */
    repetir: () => Promise<void>;
    aplicar: (s: Sugerencia) => void;
    ocultar: () => void;
    /**
     * La sugerencia ya no es de esta empresa (404 tras pasar la conversación con un préstamo,
     * §16.2): se quita de la vista como «sin sugerencia», sin llamar al servidor.
     */
    quitar: () => void;
};

/** Estado del asistente, la sugerencia vigente y sus cambios por el feed. */
export function useAsistente(conv: Pick<Conversacion, "id">, suscribir: (o: OyenteFeed) => () => void, activo: boolean): Asistente {
    const { puede } = useSesion();
    const permitido = activo && puede([PERMISO_SUGERENCIAS]);
    const [estado, setEstado] = useState<EstadoAsistente | null>(null);
    const [sugerencia, setSugerencia] = useState<Sugerencia | null>(null);
    const [errorPedido, setErrorPedido] = useState<ExplicacionError | null>(null);
    const ultimoOtra = useRef(false);
    const [pidiendo, setPidiendo] = useState(false);
    const [lectura, setLectura] = useState(0);
    const id = conv.id;

    useEffect(() => {
        if (!permitido) return;
        const control = new AbortController();
        leerEstadoAsistente(control.signal).then(
            (e) => !control.signal.aborted && setEstado(e),
            () => undefined,
        );
        return () => control.abort();
    }, [permitido]);

    // Las que la persona ocultó no vuelven aunque llegue tarde un evento suyo.
    const ocultas = useRef(new Set<string>());
    const aplicar = useCallback((s: Sugerencia) => {
        if (s.conversacionId !== id || ocultas.current.has(s.id)) return;
        setSugerencia((actual) => aplicarSugerencia(actual && actual.conversacionId === id ? actual : null, s));
        setErrorPedido(null);
    }, [id]);

    // La conversación abierta monta su propio panel (`key`): no hay sugerencia de otra que limpiar.
    useEffect(() => {
        if (!permitido || !estado?.activo) return;
        const control = new AbortController();
        leerSugerenciaActual(id, control.signal).then(
            (s) => !control.signal.aborted && aplicar(s),
            () => undefined, // 404 `sin_sugerencia` o sin red: no hay tarjeta; el feed traerá la próxima.
        );
        return () => control.abort();
    }, [id, permitido, estado?.activo, lectura, aplicar]);

    useEffect(() => {
        if (!permitido) return;
        return suscribir((s) => {
            if (s.tipo === "recargar") {
                setLectura((n) => n + 1);
                return;
            }
            const e = s.evento;
            if (e.tipo === "sugerencia" && e.sugerencia && e.conversacionId === id) aplicar(e.sugerencia);
        });
    }, [permitido, suscribir, id, aplicar]);

    const pedir = useCallback(
        async (otra: boolean) => {
            ultimoOtra.current = otra;
            setPidiendo(true);
            setErrorPedido(null);
            try {
                aplicar(await pedirSugerencia(id, otra, nuevaClaveIdempotencia()));
            } catch (e) {
                // La espera de un 429 se fija al llegar, no en cada dibujo.
                setErrorPedido(e instanceof ErrorApi ? explicarErrorDeApi(e, "sugerencia", Date.now()) : { titulo: "No se pudo pedir la sugerencia", texto: mensajeDeError(e).titulo, reintentar: true });
            } finally {
                setPidiendo(false);
            }
        },
        [id, aplicar],
    );

    const repetir = useCallback(() => pedir(ultimoOtra.current), [pedir]);

    const actual = useRef<Sugerencia | null>(null);
    useEffect(() => {
        actual.current = sugerencia;
    });
    const ocultar = useCallback(() => {
        const s = actual.current;
        setErrorPedido(null);
        setSugerencia(null);
        if (s) ocultas.current.add(s.id);
        // Se oculta en el acto; si el servidor no la descarta, el próximo cambio la vuelve a traer.
        if (s) void descartarSugerencia(s.id, nuevaClaveIdempotencia()).catch(() => undefined);
    }, []);

    const quitar = useCallback(() => {
        const s = actual.current;
        if (s) ocultas.current.add(s.id);
        setErrorPedido(null);
        setSugerencia(null);
    }, []);

    return { estado: permitido ? estado : null, sugerencia: permitido ? sugerencia : null, errorPedido, pidiendo, pedir, repetir, aplicar, ocultar, quitar };
}

const FUENTE: Record<FuenteSugerencia["tipo"], { icono: LucideIcon; clase: string }> = {
    horus: { icono: WifiOff, clase: "border-error/40 text-error" },
    argos: { icono: CircleCheck, clase: "border-success/40 text-success" },
    manual: { icono: BookOpen, clase: "border-border text-foreground" },
    historial: { icono: History, clase: "border-border text-muted-foreground" },
};

function ChipFuente({ f }: { f: FuenteSugerencia }) {
    const { icono: Icono, clase } = FUENTE[f.tipo] ?? FUENTE.historial;
    return (
        <li data-fuente={f.tipo} className={cn("inline-flex h-6 max-w-full min-w-0 items-center gap-1.5 rounded-full border bg-card px-2.5 text-xs font-medium", clase)}>
            <Icono aria-hidden className="size-3.5 shrink-0" />
            <span className="truncate">{f.etiqueta}</span>
        </li>
    );
}

type PropsTarjeta = {
    conv: Conversacion;
    asistente: Asistente;
    /** «Usar»: pasa el texto al compositor junto con el id de la sugerencia. */
    alUsar: (texto: string, sugerenciaId: string) => void;
    alActualizar: (c: Conversacion) => void;
};

/**
 * Va con `key` = id de la sugerencia: una nueva empieza desplegada y sin errores de la anterior.
 *
 * Tarjeta «Sugerencia del asistente» en sus estados: `generando` (esqueleto discreto), `lista`,
 * `error` (con «Reintentar» si sirve). Se pliega a su cabecera para no tapar el hilo en el teléfono
 * con el teclado abierto; el texto largo se desplaza dentro de la tarjeta.
 */
export function TarjetaSugerencia({ conv, asistente, alUsar, alActualizar }: PropsTarjeta) {
    const { puede } = useSesion();
    const { sugerencia: s, errorPedido, pidiendo } = asistente;
    const [plegada, setPlegada] = useState(false);
    const [ocupado, setOcupado] = useState<"usar" | "etiquetar" | "calificar" | null>(null);
    const [errorAccion, setErrorAccion] = useState<string | null>(null);
    const claveAccion = useRef<string | null>(null);

    if (!s && !errorPedido && !pidiendo) return null;

    const explicacion: ExplicacionError | null = errorPedido ?? (s?.estado === "error" ? explicarError(s.error?.codigo ?? "", s.error?.detalle, "sugerencia") : null);
    const generando = (pidiendo && !s) || s?.estado === "generando";

    async function usar() {
        if (!s?.texto) return;
        setOcupado("usar");
        setErrorAccion(null);
        try {
            asistente.aplicar(await usarSugerencia(s.id, nuevaClaveIdempotencia()));
            alUsar(s.texto, s.id);
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 404) asistente.quitar();
            else if (e instanceof ErrorApi && e.estado !== 0) {
                setErrorAccion(e.codigo === "sugerencia_no_vigente" ? "Esta sugerencia ya no está vigente: llegó otro mensaje del cliente." : `No se pudo usar: ${mensajeDeError(e).titulo}`);
            } else {
                // Sin red: el texto pasa igual y el envío lleva el id (el servidor la acepta en `lista`).
                alUsar(s.texto, s.id);
            }
        } finally {
            setOcupado(null);
        }
    }

    async function calificar(util: boolean) {
        if (!s) return;
        setOcupado("calificar");
        try {
            asistente.aplicar(await calificarSugerencia(s.id, util, nuevaClaveIdempotencia()));
            avisar({ titulo: util ? "Gracias: marcaste la sugerencia como útil" : "Gracias: marcaste la sugerencia como no útil" });
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 404) asistente.quitar();
            else setErrorAccion(`No se guardó tu opinión: ${mensajeDeError(e).titulo}`);
        } finally {
            setOcupado(null);
        }
    }

    const accion = s?.accion;
    const yaEtiquetada = accion ? (conv.etiquetas ?? []).some((e) => e.id === accion.etiquetaId) : false;
    async function etiquetar() {
        if (!accion) return;
        setOcupado("etiquetar");
        setErrorAccion(null);
        claveAccion.current ??= nuevaClaveIdempotencia();
        const ids = [...new Set([...(conv.etiquetas ?? []).map((e) => e.id), accion.etiquetaId])];
        try {
            const c = await resolver(
                api().POST("/api/v1/mensajeria/conversaciones/{id}/etiquetar", {
                    params: { path: { id: conv.id }, header: cabeceraIdem(claveAccion.current) },
                    body: { version: conv.version, etiquetaIds: ids },
                }),
            );
            claveAccion.current = null;
            alActualizar(c);
            avisar({ titulo: `Etiqueta «${accion.etiqueta}» puesta en ${c.titulo}` });
        } catch (e) {
            if (e instanceof ErrorApi && e.estado !== 0) claveAccion.current = null;
            setErrorAccion(
                e instanceof ErrorApi && e.estado === 409 ? "Alguien más cambió la conversación: vuelve a intentarlo." : `No se pudo etiquetar: ${mensajeDeError(e).titulo}`,
            );
        } finally {
            setOcupado(null);
        }
    }

    const titulo = generando ? "Preparando sugerencia…" : "Sugerencia del asistente";
    return (
        <section
            aria-label="Sugerencia del asistente"
            data-slot="sugerencia"
            data-estado={explicacion ? "error" : generando ? "generando" : (s?.estado ?? "lista")}
            className="mx-3 mt-2 shrink-0 rounded-xl border border-primary/30 bg-[color-mix(in_oklab,var(--hx-primary)_6%,var(--hx-card))] @min-[700px]/conv:mx-3.5"
        >
            <div className="flex min-w-0 items-center gap-1 py-1 pr-1 pl-3">
                <Boton
                    variante="fantasma"
                    tamano="sm"
                    className="h-8 min-w-0 flex-1 justify-start gap-2 px-0 text-sm font-semibold text-primary hover:bg-transparent"
                    aria-expanded={!plegada}
                    onClick={() => setPlegada((p) => !p)}
                    title={plegada ? "Mostrar la sugerencia" : "Plegar la sugerencia"}
                >
                    <Sparkles aria-hidden className={cn("shrink-0", generando && "motion-safe:animate-pulse")} />
                    <span className="truncate">{titulo}</span>
                    <ChevronDown aria-hidden className={cn("shrink-0 transition-transform motion-reduce:transition-none", plegada && "-rotate-90")} />
                </Boton>
                {s?.estado === "lista" ? (
                    <>
                        <Boton
                            variante="fantasma"
                            tamano="icono-sm"
                            aria-label="Útil"
                            title="Útil"
                            aria-pressed={s.calificacion === "util"}
                            disabled={ocupado !== null}
                            className={cn("text-muted-foreground", s.calificacion === "util" && "text-primary")}
                            onClick={() => void calificar(true)}
                        >
                            <ThumbsUp className={cn(s.calificacion === "util" && "fill-current")} />
                        </Boton>
                        <Boton
                            variante="fantasma"
                            tamano="icono-sm"
                            aria-label="No útil"
                            title="No útil"
                            aria-pressed={s.calificacion === "no_util"}
                            disabled={ocupado !== null}
                            className={cn("text-muted-foreground", s.calificacion === "no_util" && "text-primary")}
                            onClick={() => void calificar(false)}
                        >
                            <ThumbsDown className={cn(s.calificacion === "no_util" && "fill-current")} />
                        </Boton>
                    </>
                ) : null}
                <Boton variante="fantasma" tamano="icono-sm" aria-label="Ocultar la sugerencia" title="Ocultar la sugerencia" className="text-muted-foreground" onClick={asistente.ocultar}>
                    <X />
                </Boton>
            </div>
            {plegada ? null : (
                <div className="grid max-h-[38dvh] min-w-0 gap-2.5 overflow-y-auto px-3 pb-3" data-desborde-permitido>
                    {generando ? (
                        <div role="status" className="grid gap-1.5" aria-busy="true">
                            <span className="sr-only">Preparando sugerencia…</span>
                            <Esqueleto className="h-3.5 w-11/12" />
                            <Esqueleto className="h-3.5 w-3/4" />
                        </div>
                    ) : explicacion ? (
                        <ErrorAsistente explicacion={explicacion} alReintentar={() => void asistente.repetir()} ocupado={pidiendo} />
                    ) : s?.estado === "lista" && s.texto ? (
                        <>
                            <p className="text-sm leading-relaxed break-words whitespace-pre-wrap">{s.texto}</p>
                            {s.fuentes.length > 0 ? (
                                <ul aria-label="Fuentes de la sugerencia" className="flex min-w-0 flex-wrap gap-1.5">
                                    {s.fuentes.map((f, i) => (
                                        <ChipFuente key={`${f.tipo}-${f.entidadId ?? i}`} f={f} />
                                    ))}
                                </ul>
                            ) : null}
                            <div className="grid grid-cols-2 gap-2 @min-[30rem]/conv:flex @min-[30rem]/conv:flex-wrap">
                                <Boton tamano="sm" onClick={() => void usar()} disabled={ocupado !== null} className="w-full min-w-0 @min-[30rem]/conv:w-auto">
                                    <Check />
                                    {ocupado === "usar" ? "Usando…" : "Usar"}
                                </Boton>
                                <Boton variante="secundario" tamano="sm" onClick={() => void asistente.pedir(true)} disabled={ocupado !== null || pidiendo} className="w-full min-w-0 @min-[30rem]/conv:w-auto">
                                    <RefreshCw />
                                    Otra
                                </Boton>
                                {accion && puede(["conversaciones.etiquetar"]) ? (
                                    <Boton
                                        variante="secundario"
                                        tamano="sm"
                                        onClick={() => void etiquetar()}
                                        disabled={ocupado !== null || yaEtiquetada}
                                        className="col-span-2 h-auto min-h-8 w-full min-w-0 py-1 whitespace-normal @min-[30rem]/conv:w-auto"
                                    >
                                        <Tag />
                                        {yaEtiquetada ? `Ya tiene «${accion.etiqueta}»` : ocupado === "etiquetar" ? "Etiquetando…" : `Etiquetar «${accion.etiqueta}»`}
                                    </Boton>
                                ) : null}
                            </div>
                            {errorAccion ? (
                                <p role="alert" className="text-xs text-error">
                                    {errorAccion}
                                </p>
                            ) : null}
                            <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                                <Eye aria-hidden className="mt-px size-3.5 shrink-0" />
                                Solo la ves tú. «Usar» la pasa a tu caja de texto: la revisas y la envías tú.
                            </p>
                        </>
                    ) : null}
                </div>
            )}
        </section>
    );
}

/** «Sugerir» junto al compositor (modo `boton`), o la línea de pausa por tope. */
export function BotonSugerir({ asistente }: { asistente: Asistente }) {
    const { estado, sugerencia, pidiendo } = asistente;
    if (!estado?.activo || estado.pausado || estado.modo !== "boton" || sugerencia) return null;
    return (
        <Boton variante="fantasma" tamano="sm" className="h-8 px-2 text-primary" onClick={() => void asistente.pedir(false)} disabled={pidiendo} title="Pedir una sugerencia al asistente">
            <Sparkles />
            {pidiendo ? "Pidiendo…" : "Sugerir"}
        </Boton>
    );
}

/** Asistente en pausa por el tope del mes: solo la explicación, sin botón. */
export function PausaAsistente({ asistente }: { asistente: Asistente }) {
    if (!asistente.estado?.activo || !asistente.estado.pausado) return null;
    return (
        <p data-slot="asistente-pausado" className="mx-3 mt-2 flex items-start gap-1.5 text-xs text-muted-foreground @min-[700px]/conv:mx-3.5">
            <Sparkles aria-hidden className="mt-px size-3.5 shrink-0" />
            El asistente está en pausa: se llegó al tope de uso del mes. Vuelve el mes que viene o cuando un administrador suba el tope.
        </p>
    );
}
