"use client";

import { Modal } from "@heroui/react";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import { useEffect, useRef, useState, type PointerEvent } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";

/*
 * Visor de fotos a pantalla completa (como WhatsApp o Telegram): fondo oscuro, la foto entera
 * (sin recorte), quién y cuándo, «2 de 5», flechas (también ← y → del teclado), deslizar en el
 * teléfono, doble toque para acercar, el pie de la foto y «Descargar». Es un `Modal` de HeroUI
 * (React Aria): el foco queda dentro y vuelve a la foto al cerrar; Escape cierra. «Atrás» del
 * navegador o del teléfono también cierra, sin salir de la pantalla: al abrir se agrega una entrada
 * propia (misma URL) al historial, que se consume al cerrar.
 */

export type FotoEnVisor = {
    id: string;
    /** Ruta de la foto (del mismo origen, armada desde el id validado). */
    src: string;
    /** Texto alternativo: «Foto de Carlos Ruiz, hoy 06:49». */
    alt: string;
    /** Quién la mandó. */
    titulo: string;
    /** Cuándo (ya formateado). */
    detalle: string;
    pie?: string | null | undefined;
    /** Ruta de descarga (la misma ruta autenticada); el nombre lo pone el servidor. */
    descarga?: string | undefined;
};

export type PropsVisorFotos = {
    fotos: readonly FotoEnVisor[];
    /** Índice abierto; `null` = cerrado. */
    indice: number | null;
    alCambiar: (indice: number | null) => void;
};

const MARCA = "iris.visor-fotos";

/** Distancia mínima (px) para que deslizar pase a la foto siguiente o anterior. */
const DESLIZAR = 50;

export function VisorFotos({ fotos, indice, alCambiar }: PropsVisorFotos) {
    const abierto = indice !== null && fotos.length > 0;
    const i = abierto ? Math.min(Math.max(indice, 0), fotos.length - 1) : 0;
    const foto = abierto ? fotos[i] : undefined;
    const cambiar = useRef(alCambiar);
    useEffect(() => {
        cambiar.current = alCambiar;
    }, [alCambiar]);

    // «Atrás» cierra el visor: una entrada propia en el historial mientras está abierto.
    useEffect(() => {
        if (!abierto) return undefined;
        window.history.pushState({ ...(window.history.state as object | null), [MARCA]: true }, "", window.location.href);
        let porAtras = false;
        const alVolver = () => {
            porAtras = true;
            cambiar.current(null);
        };
        window.addEventListener("popstate", alVolver);
        return () => {
            window.removeEventListener("popstate", alVolver);
            // Se cerró con la «X» o Escape: se consume la entrada propia para que «Atrás» no quede doble.
            if (!porAtras && (window.history.state as Record<string, unknown> | null)?.[MARCA]) window.history.back();
        };
    }, [abierto]);

    const ir = (d: number) => {
        if (!abierto) return;
        const siguiente = i + d;
        if (siguiente >= 0 && siguiente < fotos.length) alCambiar(siguiente);
    };
    const irRef = useRef(ir);
    useEffect(() => {
        irRef.current = ir;
    });

    // ← y → pasan de foto, esté donde esté el foco dentro del visor.
    useEffect(() => {
        if (!abierto) return undefined;
        const alTeclear = (e: globalThis.KeyboardEvent) => {
            if (e.key === "ArrowRight") irRef.current(1);
            else if (e.key === "ArrowLeft") irRef.current(-1);
        };
        window.addEventListener("keydown", alTeclear);
        return () => window.removeEventListener("keydown", alTeclear);
    }, [abierto]);

    return (
        <Modal.Backdrop isOpen={abierto} onOpenChange={(v) => (v ? undefined : alCambiar(null))} isDismissable={false} variant="opaque" className="bg-black/90 data-exiting:animate-none">
            <Modal.Container placement="center" scroll="inside" className="size-full max-h-none max-w-none p-0 data-exiting:animate-none motion-reduce:animate-none">
                <Modal.Dialog
                    aria-label={foto ? `Foto ${i + 1} de ${fotos.length}: ${foto.alt}` : "Foto"}
                    data-slot="visor-fotos"
                    className="flex h-dvh max-h-none w-screen max-w-none flex-col gap-0 overflow-hidden rounded-none border-0 bg-transparent! p-0 text-white shadow-none!"
                >
                    <div className="flex size-full min-h-0 flex-col">
                        {foto ? <Contenido key={foto.id} foto={foto} i={i} total={fotos.length} ir={ir} cerrar={() => alCambiar(null)} fotos={fotos} elegir={alCambiar} /> : null}
                    </div>
                </Modal.Dialog>
            </Modal.Container>
        </Modal.Backdrop>
    );
}

const claseBotonVisor = "size-10 rounded-full bg-white/10 text-white [--button-fg:#fff] [--button-bg-hover:rgb(255_255_255/0.2)] hover:bg-white/20";

function Contenido({ foto, i, total, ir, cerrar, fotos, elegir }: { foto: FotoEnVisor; i: number; total: number; ir: (d: number) => void; cerrar: () => void; fotos: readonly FotoEnVisor[]; elegir: (i: number) => void }) {
    const [zoom, setZoom] = useState(false);
    const inicio = useRef<{ x: number; y: number } | null>(null);
    const alBajar = (e: PointerEvent) => {
        inicio.current = { x: e.clientX, y: e.clientY };
    };
    const alSubir = (e: PointerEvent) => {
        const s = inicio.current;
        inicio.current = null;
        if (!s || zoom) return;
        const dx = e.clientX - s.x;
        if (Math.abs(dx) > DESLIZAR && Math.abs(dx) > Math.abs(e.clientY - s.y)) ir(dx < 0 ? 1 : -1);
    };
    return (
        <>
            <div className="flex shrink-0 items-center gap-2 px-2 py-2 sm:px-3">
                <Boton variante="fantasma" tamano="icono" className={claseBotonVisor} aria-label="Cerrar" title="Cerrar (Esc)" onClick={cerrar}>
                    <X />
                </Boton>
                <span className="grid min-w-0 flex-1">
                    <h2 className="truncate text-sm font-semibold">{foto.titulo}</h2>
                    <span className="truncate text-xs text-white/75" data-slot="visor-posicion">
                        {foto.detalle} · {i + 1} de {total}
                    </span>
                </span>
                {foto.descarga ? (
                    <Boton asChild variante="fantasma" tamano="icono" className={claseBotonVisor}>
                        <a href={foto.descarga} download aria-label="Descargar" title="Descargar">
                            <Download />
                        </a>
                    </Boton>
                ) : null}
            </div>
            <div
                className="relative grid min-h-0 flex-1 touch-pan-y place-items-center overflow-hidden px-2 select-none"
                onPointerDown={alBajar}
                onPointerUp={alSubir}
                onDoubleClick={() => setZoom((z) => !z)}
            >
                {/* Adjunto del mismo origen, servido con la cookie: sin el optimizador de Next. */}
                <img
                    src={foto.src}
                    alt={foto.alt}
                    draggable={false}
                    data-slot="visor-imagen"
                    className={cn("max-h-full max-w-full rounded-sm object-contain transition-transform duration-200 motion-reduce:transition-none", zoom && "scale-200 cursor-zoom-out")}
                />
                {i > 0 ? (
                    <Boton variante="fantasma" tamano="icono" className={cn(claseBotonVisor, "absolute top-1/2 left-3 hidden -translate-y-1/2 sm:inline-flex")} aria-label="Foto anterior" onClick={() => ir(-1)}>
                        <ChevronLeft />
                    </Boton>
                ) : null}
                {i < total - 1 ? (
                    <Boton variante="fantasma" tamano="icono" className={cn(claseBotonVisor, "absolute top-1/2 right-3 hidden -translate-y-1/2 sm:inline-flex")} aria-label="Foto siguiente" onClick={() => ir(1)}>
                        <ChevronRight />
                    </Boton>
                ) : null}
            </div>
            <div className="grid shrink-0 gap-2 px-3 pt-2 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
                {foto.pie ? <p className="mx-auto max-h-24 max-w-2xl overflow-y-auto text-center text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{foto.pie}</p> : null}
                {total > 1 ? (
                    <div className="mx-auto flex max-w-full gap-1.5 overflow-x-auto" role="group" aria-label="Fotos de la conversación">
                        {fotos.map((f, k) => (
                            <button
                                key={f.id}
                                type="button"
                                aria-label={`Ver foto ${k + 1} de ${total}`}
                                aria-current={k === i ? "true" : undefined}
                                onClick={() => elegir(k)}
                                className={cn("size-11 shrink-0 overflow-hidden rounded-md border-2 p-0 outline-offset-2 focus-visible:outline-2 focus-visible:outline-white", k === i ? "border-primary" : "border-transparent opacity-60")}
                            >
                                <img src={f.src} alt="" loading="lazy" className="size-full object-cover" />
                            </button>
                        ))}
                    </div>
                ) : null}
                <p className="text-center text-xs text-white/60 sm:hidden">Desliza para ver las demás · toca dos veces para acercar</p>
            </div>
        </>
    );
}
