"use client";

import { Aviso, Boton, cn, Esqueleto, Hoja, HojaContenido } from "@iris/ui";
import { ArrowLeft, PanelRight, RotateCw } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api } from "@/lib/api/cliente";
import { leerDetalle, leerMensajes } from "@/lib/conversaciones/api";
import { fusionarMensajes, pendientesSinConfirmar, type Pendiente } from "@/lib/conversaciones/estado";
import { CLASE_PUNTO, formatearNumero, iniciales, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import type { Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, ConversacionDetalle, Mensaje } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { ErrorLectura } from "@/componentes/error-lectura";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { AccionesConversacion } from "./acciones";
import type { OyenteFeed } from "./bandeja";
import { Compositor } from "./compositor";
import { PildoraCanal } from "./filtros";
import { FichaCliente, type PestanaFicha } from "./ficha";
import { Hilo } from "./hilo";
import { useAhora, useAncho } from "./reloj";

type Props = {
    id: number;
    identidad: Identidad;
    suscribir: (o: OyenteFeed) => () => void;
    alActualizar: (c: Conversacion) => void;
    hrefLista: string;
    detenido: "sin_sesion" | "sin_permiso" | "sesion_cambiada" | null;
};

/** Ancho a partir del cual la ficha va al lado (si no, en una hoja). */
const ANCHO_FICHA = 1100;
const CLAVE_FICHA = "iris-bandeja-ficha";

function leerFichaVisible(): boolean {
    try {
        return window.localStorage.getItem(CLAVE_FICHA) !== "0";
    } catch {
        return true;
    }
}

/** ¿Hay espacio para la ficha al lado? (ancho de la bandeja, que ya descuenta el menú) */
function useFichaAlLado(): boolean {
    return useSyncExternalStore(
        (avisar) => {
            const el = document.querySelector('[data-slot="bandeja"]');
            if (!el) return () => undefined;
            const obs = new ResizeObserver(avisar);
            obs.observe(el);
            return () => obs.disconnect();
        },
        () => (document.querySelector('[data-slot="bandeja"]')?.clientWidth ?? window.innerWidth) >= ANCHO_FICHA,
        () => true,
    );
}

/** Conversación abierta: cabecera, hilo, compositor y ficha del cliente (al lado o en una hoja). */
export function PanelConversacion({ id, identidad, suscribir, alActualizar, hrefLista, detenido }: Props) {
    const { puede } = useSesion();
    const ahora = useAhora(30_000);
    const detalle = useLectura<ConversacionDetalle>((signal) => leerDetalle(id, signal));
    const conv = detalle.estado === "listo" ? detalle.datos : null;
    useNombreEnMigas(conv?.titulo);
    const reemplazarDetalle = detalle.reemplazar;
    const refrescarDetalle = detalle.refrescar;

    // Mensajes: la última página y las anteriores a pedido; el feed agrega y actualiza.
    const [mensajes, setMensajes] = useState<Mensaje[]>([]);
    const [estadoMensajes, setEstadoMensajes] = useState<"cargando" | "listo" | unknown>("cargando");
    const [cursorAnteriores, setCursorAnteriores] = useState<string | null>(null);
    const [cargandoAnteriores, setCargandoAnteriores] = useState(false);
    const [errorAnteriores, setErrorAnteriores] = useState(false);
    const [pendientes, setPendientes] = useState<Pendiente[]>([]);
    const [intento, setIntento] = useState(0);

    useEffect(() => {
        const control = new AbortController();
        setEstadoMensajes("cargando");
        leerMensajes(id, undefined, control.signal).then(
            (p) => {
                if (control.signal.aborted) return;
                setMensajes((m) => fusionarMensajes(m, p.data ?? []));
                setCursorAnteriores(p.nextCursor ?? null);
                setEstadoMensajes("listo");
            },
            (e: unknown) => !control.signal.aborted && setEstadoMensajes(e ?? new Error("error")),
        );
        return () => control.abort();
    }, [id, intento]);

    async function cargarAnteriores() {
        if (!cursorAnteriores) return;
        setCargandoAnteriores(true);
        setErrorAnteriores(false);
        try {
            const p = await leerMensajes(id, cursorAnteriores);
            setMensajes((m) => fusionarMensajes(m, p.data ?? []));
            setCursorAnteriores(p.nextCursor ?? null);
        } catch {
            setErrorAnteriores(true);
        } finally {
            setCargandoAnteriores(false);
        }
    }

    const aplicarConversacion = useCallback(
        (c: Conversacion) => {
            alActualizar(c);
            if (c.id !== id) return;
            if (detalle.estado === "listo" && detalle.datos.version > c.version) return;
            // El detalle conserva sus vínculos y su resumen hasta releerlo.
            if (detalle.estado === "listo") reemplazarDetalle({ ...detalle.datos, ...c });
        },
        [alActualizar, detalle, id, reemplazarDetalle],
    );
    const aplicarRef = useRef(aplicarConversacion);
    aplicarRef.current = aplicarConversacion;

    useEffect(
        () =>
            suscribir((s) => {
                if (s.tipo === "recargar") {
                    void refrescarDetalle();
                    setIntento((n) => n + 1);
                    return;
                }
                const e = s.evento;
                if (e.conversacionId !== id) return;
                if (e.mensaje) {
                    const m = e.mensaje;
                    setMensajes((actual) => fusionarMensajes(actual, [m]));
                    setPendientes((p) => p.filter((x) => x.id !== m.id));
                    // Un evento de sistema puede traer un vínculo nuevo (ticket, orden…).
                    if (m.direccion === "sistema") void refrescarDetalle();
                }
                if (e.conversacion && e.conversacion.id === id) aplicarRef.current(e.conversacion);
            }),
        [id, suscribir, refrescarDetalle],
    );

    // Leer: avanza la lectura hasta el último mensaje del cliente que se ve (nunca retrocede).
    const ultimoEntrante = [...mensajes].reverse().find((m) => m.direccion === "entrante")?.id;
    const leido = useRef<string | null>(null);
    useEffect(() => {
        if (!ultimoEntrante || leido.current === ultimoEntrante || !conv || conv.noLeidos === 0 || detenido) return;
        if (typeof document !== "undefined" && document.visibilityState !== "visible") return;
        leido.current = ultimoEntrante;
        void api()
            .POST("/api/v1/mensajeria/conversaciones/{id}/leer", { params: { path: { id } }, body: { hastaMensajeId: ultimoEntrante } })
            .then(({ data }) => {
                if (data && conv) aplicarRef.current({ ...conv, noLeidos: data.noLeidos });
            })
            .catch(() => {
                leido.current = null;
            });
    }, [ultimoEntrante, conv, id, detenido]);

    // Ficha: al lado con espacio (se puede ocultar y se recuerda); si no, en una hoja.
    const alLado = useFichaAlLado();
    const [fichaVisible, setFichaVisible] = useState(leerFichaVisible);
    const [hojaAbierta, setHojaAbierta] = useState(false);
    const [pestana, setPestana] = useState<PestanaFicha>("resumen");
    function alternarFicha() {
        if (!alLado) {
            setHojaAbierta(true);
            return;
        }
        setFichaVisible((v) => {
            try {
                window.localStorage.setItem(CLAVE_FICHA, v ? "0" : "1");
            } catch {
                // Sin almacenamiento: dura lo que la página.
            }
            return !v;
        });
    }
    function abrirAyuda() {
        setPestana("ayuda");
        if (alLado) setFichaVisible(true);
        else setHojaAbierta(true);
    }

    const [refSeccion, anchoSeccion] = useAncho<HTMLElement>();
    const visiblesPendientes = pendientesSinConfirmar(pendientes, mensajes);
    // Puentes hacia el compositor: reintentar un pendiente desde el hilo y usar un manual de la ficha.
    const reintentar = useRef<((p: Pendiente) => void) | null>(null);
    const usarTexto = useRef<((t: string) => void) | null>(null);

    if (detalle.estado === "error") {
        return (
            <section aria-label="Conversación" className="min-h-0 overflow-y-auto p-4 @min-[1100px]:col-span-2">
                <Boton asChild variante="fantasma" tamano="sm" className="mb-3 @min-[700px]:hidden">
                    <Link href={hrefLista} scroll={false}>
                        <ArrowLeft />
                        Conversaciones
                    </Link>
                </Boton>
                <ErrorLectura error={detalle.error} reintentar={detalle.reintentar} que="la conversación" />
            </section>
        );
    }
    if (!conv) {
        return (
            <section aria-label="Conversación" aria-busy="true" className="flex min-h-0 flex-col gap-3 p-4 @min-[1100px]:col-span-2">
                <span className="sr-only" role="status">
                    Cargando la conversación…
                </span>
                <Esqueleto className="h-10 w-1/2" />
                <Esqueleto className="h-16 w-2/3" />
                <Esqueleto className="ml-auto h-16 w-2/3" />
            </section>
        );
    }

    const tono = tonoDeCanal(conv.canal);
    const ficha = (
        <FichaCliente
            conv={conv}
            pestana={pestana}
            alCambiarPestana={setPestana}
            alActualizar={(c) => {
                aplicarConversacion(c);
                void refrescarDetalle();
            }}
            recargar={refrescarDetalle}
            alUsarTexto={(t) => usarTexto.current?.(t)}
        />
    );
    return (
        <>
            <section ref={refSeccion} aria-label={`Conversación con ${conv.titulo}`} className="@container/conv relative flex min-h-0 min-w-0 flex-col">
                <header className="flex h-[3.75rem] shrink-0 items-center gap-2 border-b border-border bg-card px-3 @min-[700px]/conv:gap-2.5 @min-[700px]/conv:px-3.5">
                    <Boton asChild variante="fantasma" tamano="icono-sm" className="@min-[700px]:hidden">
                        <Link href={hrefLista} scroll={false} aria-label="Volver a las conversaciones" title="Volver a las conversaciones">
                            <ArrowLeft />
                        </Link>
                    </Boton>
                    <span aria-hidden className="relative flex size-9 shrink-0 items-center justify-center rounded-full bg-elevated text-[0.8125rem] font-semibold">
                        {iniciales(conv.titulo)}
                        <span className={cn("absolute -right-0.5 -bottom-0.5 size-3.5 rounded-full ring-2 ring-card", CLASE_PUNTO[tono])} />
                    </span>
                    <div className="flex min-w-0 flex-1 flex-col">
                        <h2 className="truncate text-[0.9375rem] font-semibold">{conv.titulo}</h2>
                        <span className="flex min-w-0 items-center gap-1.5 text-[0.78125rem] text-muted-foreground">
                            <PildoraCanal canal={conv.canal} className="shrink-0" />
                            <span className="truncate tabular-nums">{formatearNumero(conv.numero)}</span>
                        </span>
                    </div>
                    <AccionesConversacion conv={conv} estrecho={anchoSeccion > 0 && anchoSeccion < 576} alActualizar={aplicarConversacion} recargar={refrescarDetalle} abrirAyuda={abrirAyuda} />
                    <Boton
                        variante="contorno"
                        tamano="icono-sm"
                        aria-label={alLado && fichaVisible ? "Ocultar la ficha del cliente" : "Ver la ficha del cliente"}
                        title={alLado && fichaVisible ? "Ocultar la ficha del cliente" : "Ver la ficha del cliente"}
                        aria-expanded={alLado ? fichaVisible : hojaAbierta}
                        onClick={alternarFicha}
                        className="shrink-0"
                    >
                        <PanelRight />
                    </Boton>
                </header>
                {detenido ? (
                    <Aviso
                        tono="warning"
                        titulo={detenido === "sesion_cambiada" ? "La sesión de este navegador cambió" : "Tu sesión se cerró"}
                        className="m-3 mb-0"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={() => window.location.reload()}>
                                <RotateCw />
                                Recargar
                            </Boton>
                        }
                    >
                        No se enviará nada desde esta pestaña hasta recargar.
                    </Aviso>
                ) : null}
                {estadoMensajes === "cargando" ? (
                    <div className="flex flex-1 flex-col gap-2 p-4" aria-busy="true">
                        <Esqueleto className="h-14 w-2/3" />
                        <Esqueleto className="ml-auto h-14 w-2/3" />
                    </div>
                ) : estadoMensajes !== "listo" ? (
                    <div className="flex-1 p-4">
                        <ErrorLectura error={estadoMensajes} reintentar={() => setIntento((n) => n + 1)} que="los mensajes" />
                    </div>
                ) : (
                    <Hilo
                        mensajes={mensajes}
                        pendientes={visiblesPendientes}
                        ahora={ahora}
                        hayAnteriores={cursorAnteriores !== null}
                        cargandoAnteriores={cargandoAnteriores}
                        errorAnteriores={errorAnteriores}
                        cargarAnteriores={cargarAnteriores}
                        reintentarPendiente={(p) => reintentar.current?.(p)}
                    />
                )}
                {puede(["conversaciones.responder"]) && !detenido ? (
                    <Compositor
                        conv={conv}
                        identidad={identidad}
                        ahora={ahora}
                        alPendiente={(p) => setPendientes((l) => [...l.filter((x) => x.id !== p.id), p])}
                        alConfirmado={(m) => {
                            setMensajes((l) => fusionarMensajes(l, [m]));
                            setPendientes((l) => l.filter((x) => x.id !== m.id));
                        }}
                        alActualizar={aplicarConversacion}
                        recargar={refrescarDetalle}
                        abrirAyuda={abrirAyuda}
                        registrarReintento={(f) => (reintentar.current = f)}
                        registrarUsarTexto={(f) => (usarTexto.current = f)}
                    />
                ) : (
                    <p className="border-t border-border bg-card px-4 py-3 text-sm text-muted-foreground">
                        {detenido ? "Recarga la página para seguir." : "Puedes ver esta conversación, pero no responderla."}
                    </p>
                )}
            </section>
            {alLado ? (
                fichaVisible ? (
                    <aside aria-label="Ficha del cliente" className="hidden min-h-0 w-[18.75rem] flex-col overflow-y-auto border-l border-border bg-card @min-[1100px]:flex">
                        {ficha}
                    </aside>
                ) : null
            ) : (
                <Hoja open={hojaAbierta} onOpenChange={setHojaAbierta}>
                    <HojaContenido titulo="Ficha del cliente" lado="derecha" etiquetaCerrar="Cerrar la ficha" className="w-[min(22rem,92vw)] p-0 sm:w-[min(22rem,92vw)] [&>div:first-child]:px-4 [&>div:first-child]:pt-4">
                        {ficha}
                    </HojaContenido>
                </Hoja>
            )}
        </>
    );
}
