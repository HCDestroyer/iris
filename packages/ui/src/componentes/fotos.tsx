"use client";

import { ImageOff, RotateCw } from "lucide-react";
import { useState, type CSSProperties, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";

/*
 * Fotos en un hilo de mensajes (pedido del PM: «que se sienta natural», como WhatsApp o Telegram):
 * la burbuja toma la proporción de la foto con máximos de ancho y alto, esquinas redondeadas, la
 * hora sobre la foto y el espacio reservado desde antes de que cargue (sin saltos). Si no se pudo
 * cargar, lo dice con icono + texto y «Reintentar». Tocar la foto la abre en el visor.
 */

/** Ancho máximo de una foto en la burbuja (px). El ancho real nunca pasa del 100 % de la burbuja. */
export const ANCHO_MAX_FOTO = 320;
/** Alto máximo de una foto en la burbuja (px). */
export const ALTO_MAX_FOTO = 340;
/** Ancho mínimo: una foto diminuta se centra sobre el fondo, sin estirarse. */
export const ANCHO_MIN_FOTO = 150;
/** Sin medidas (WebP, mensaje antiguo, cabecera ilegible): se reserva 4:3. */
export const PROPORCION_POR_DEFECTO = 4 / 3;

export type MedidasFoto = {
    /** Ancho de la caja en px (luego acotado al 100 % de la burbuja). */
    ancho: number;
    /** ancho / alto de la caja: la de la foto, acotada entre 1:2 y 2:1. */
    proporcion: number;
    /** La foto es más chica que la caja: se muestra centrada a su tamaño, sin recortar ni estirar. */
    contener: boolean;
    /** Se reservó 4:3 porque no había medidas. */
    supuesta: boolean;
};

const valida = (n: number | null | undefined): n is number => typeof n === "number" && Number.isFinite(n) && n > 0;

/**
 * Medidas de la caja de una foto: su proporción acotada entre 1:2 y 2:1 (lo que sobra se recorta
 * centrado; el visor la muestra entera), ancho hasta `ANCHO_MAX_FOTO` (o el de la foto, si es
 * menor) sin que el alto pase de `ALTO_MAX_FOTO`, y nunca menos de `ANCHO_MIN_FOTO`.
 */
export function medidasFoto(ancho: number | null | undefined, alto: number | null | undefined): MedidasFoto {
    const conMedidas = valida(ancho) && valida(alto);
    const proporcion = conMedidas ? Math.min(2, Math.max(0.5, ancho / alto)) : PROPORCION_POR_DEFECTO;
    // Una foto más chica que el máximo no se agranda: su ancho (y nunca menos del mínimo).
    const caja = Math.max(ANCHO_MIN_FOTO, Math.min(ANCHO_MAX_FOTO, ALTO_MAX_FOTO * proporcion, conMedidas ? ancho : Infinity));
    const contener = conMedidas && ancho < ANCHO_MIN_FOTO && alto < ANCHO_MIN_FOTO / proporcion;
    return { ancho: Math.round(caja), proporcion: Math.round(proporcion * 1000) / 1000, contener, supuesta: !conMedidas };
}

export type EstadoFoto = "lista" | "pendiente" | "rechazada";

export type PropsFotoMensaje = {
    /** Ruta de la foto (armada desde el id validado). Sin ella, el marcador de su estado. */
    src?: string | undefined;
    ancho?: number | null | undefined;
    alto?: number | null | undefined;
    /** lista (se puede pedir), pendiente (todavía se descarga o sube) o rechazada (no llegará). */
    estado: EstadoFoto;
    /** Nombre accesible del botón que abre el visor: «Ver foto de Carlos Ruiz, 06:49». */
    etiqueta: string;
    alAbrir?: (() => void) | undefined;
    /** Sobre la foto, abajo a la derecha (la hora y la entrega). */
    superpuesto?: ReactNode;
    /** Texto del marcador mientras está pendiente. */
    textoPendiente?: string;
    /** Texto si no llegará (rechazada). */
    textoRechazada?: string;
    /** Celda de una cuadrícula: cuadrada y del ancho de su columna. */
    enCuadricula?: boolean;
    /** «+N» encima (la última celda de una cuadrícula con más fotos). */
    mas?: number;
    className?: string;
};

/**
 * Una foto en la burbuja, con su proporción reservada antes de cargar (aspect-ratio). Mientras
 * carga, un marcador tenue; si la descarga falla, icono + «No se pudo cargar la foto» +
 * «Reintentar» (vuelve a pedirla). Sin medidas reserva 4:3 y, al cargar, toma la proporción real.
 */
export function FotoMensaje({ src, ancho, alto, estado, etiqueta, alAbrir, superpuesto, textoPendiente = "Cargando foto…", textoRechazada = "La foto no está disponible", enCuadricula = false, mas, className }: PropsFotoMensaje) {
    const [carga, setCarga] = useState<"cargando" | "lista" | "error">("cargando");
    const [intento, setIntento] = useState(0);
    const [natural, setNatural] = useState<{ ancho: number; alto: number } | null>(null);
    const m = medidasFoto(ancho ?? natural?.ancho, alto ?? natural?.alto);
    const estilo: CSSProperties = enCuadricula ? { aspectRatio: "1" } : { width: `${m.ancho}px`, aspectRatio: String(m.proporcion) };
    const comun = cn("relative block overflow-hidden rounded-[0.6875rem] bg-elevated", enCuadricula ? "w-full rounded-[0.25rem]" : "max-w-full", className);
    const pie = superpuesto ? (
        <span data-slot="hora-foto" className="pointer-events-none absolute right-1.5 bottom-1.5 inline-flex items-center gap-1 rounded-full bg-black/55 px-2 py-px text-[0.6875rem] text-white tabular-nums [&_svg]:size-3.5">
            {superpuesto}
        </span>
    ) : null;

    if (estado !== "lista" || !src) {
        const rechazada = estado === "rechazada";
        return (
            <span role="img" aria-label={rechazada ? textoRechazada : textoPendiente} data-slot="foto-mensaje" data-estado-foto={rechazada ? "rechazada" : "pendiente"} className={comun} style={estilo}>
                <span className={cn("absolute inset-0 grid place-content-center justify-items-center gap-1.5 p-3 text-center text-xs", rechazada ? "text-error" : "text-muted-foreground")}>
                    {rechazada ? <ImageOff aria-hidden className="size-5" /> : <span aria-hidden className="size-5 animate-spin rounded-full border-2 border-current border-t-transparent motion-reduce:animate-none" />}
                    <span>{rechazada ? textoRechazada : textoPendiente}</span>
                </span>
                {pie}
            </span>
        );
    }

    if (carga === "error") {
        return (
            <span data-slot="foto-mensaje" data-estado-foto="error" className={comun} style={estilo}>
                <span role="alert" className="absolute inset-0 grid place-content-center justify-items-center gap-1.5 p-3 text-center text-xs text-error">
                    <ImageOff aria-hidden className="size-5" />
                    <span>No se pudo cargar la foto</span>
                    <Boton
                        variante="secundario"
                        tamano="sm"
                        className="h-8 px-2.5"
                        onClick={() => {
                            setCarga("cargando");
                            setIntento((n) => n + 1);
                        }}
                    >
                        <RotateCw />
                        Reintentar
                    </Boton>
                </span>
                {pie}
            </span>
        );
    }

    return (
        <button
            type="button"
            data-slot="foto-mensaje"
            data-estado-foto={carga}
            data-proporcion={m.proporcion}
            aria-label={etiqueta}
            onClick={alAbrir}
            className={cn(comun, "cursor-zoom-in border-0 p-0 outline-offset-2 focus-visible:outline-2 focus-visible:outline-primary", m.contener && "grid place-items-center")}
            style={estilo}
        >
            {carga === "cargando" ? <span aria-hidden className="absolute inset-0 animate-pulse bg-elevated motion-reduce:animate-none" /> : null}
            {/* Adjunto del mismo origen, servido con la cookie: sin el optimizador de Next. */}
            <img
                key={intento}
                src={intento > 0 ? `${src}#reintento-${intento}` : src}
                alt=""
                loading="lazy"
                decoding="async"
                draggable={false}
                width={valida(ancho) ? ancho : undefined}
                height={valida(alto) ? alto : undefined}
                onLoad={(e) => {
                    const im = e.currentTarget;
                    if (!valida(ancho) || !valida(alto)) setNatural({ ancho: im.naturalWidth, alto: im.naturalHeight });
                    setCarga("lista");
                }}
                onError={() => setCarga("error")}
                className={cn(m.contener ? "max-h-full max-w-full object-contain" : "absolute inset-0 size-full object-cover", carga === "cargando" && "opacity-0")}
                style={m.contener && valida(ancho) && valida(alto) ? { width: ancho, height: alto } : undefined}
            />
            {mas && mas > 0 ? (
                <span aria-hidden className="absolute inset-0 grid place-content-center bg-black/45 text-xl font-semibold text-white">
                    +{mas}
                </span>
            ) : null}
            {pie}
        </button>
    );
}

export type FotoDeAlbum = Omit<PropsFotoMensaje, "enCuadricula" | "mas" | "superpuesto" | "className"> & { id: string };

export type PropsAlbumFotos = {
    fotos: readonly FotoDeAlbum[];
    /** Sobre la última celda visible (hora y entrega del último mensaje). */
    superpuesto?: ReactNode;
    /** Nombre accesible del grupo: «4 fotos de Carlos Ruiz». */
    etiqueta: string;
    className?: string;
};

/** Cuántas celdas muestra una cuadrícula; el resto va como «+N» en la última. */
export const CELDAS_ALBUM = 4;

/**
 * Varias fotos seguidas en cuadrícula (como un álbum de WhatsApp): 2 columnas de celdas cuadradas;
 * con tres, la tercera ocupa el ancho; con más de cuatro, la cuarta lleva «+N». Cada celda abre su
 * foto en el visor.
 */
export function AlbumFotos({ fotos, superpuesto, etiqueta, className }: PropsAlbumFotos) {
    const visibles = fotos.slice(0, CELDAS_ALBUM);
    const resto = fotos.length - visibles.length;
    return (
        <div role="group" aria-label={etiqueta} data-slot="album-fotos" data-fotos={fotos.length} className={cn("grid w-72 max-w-full grid-cols-2 gap-0.5 overflow-hidden rounded-[0.6875rem]", className)}>
            {visibles.map((f, i) => {
                const ultima = i === visibles.length - 1;
                const ancha = visibles.length === 3 && i === 2;
                return (
                    <FotoMensaje
                        key={f.id}
                        {...f}
                        enCuadricula
                        className={cn(ancha && "col-span-2 aspect-[2/1]!")}
                        mas={ultima && resto > 0 ? resto : undefined}
                        etiqueta={ultima && resto > 0 ? `${f.etiqueta} (y ${resto} más)` : f.etiqueta}
                        superpuesto={ultima ? superpuesto : undefined}
                    />
                );
            })}
        </div>
    );
}
