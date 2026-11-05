"use client";

import { Flashlight, FlashlightOff, Keyboard, X } from "lucide-react";
import { useEffect, useId, useRef, useState, useSyncExternalStore, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cn } from "../utilidades";
import { Aviso } from "./aviso";
import { Boton } from "./boton";

/** Un código que vio el lector. `caja` en píxeles del cuadro de la cámara (`videoWidth` × `videoHeight`). */
export interface CodigoLeido {
    valor: string;
    /** Formato del lector (`qr_code`, `code_128`, `EAN_13`…). */
    formato: string;
    caja?: { x: number; y: number; ancho: number; alto: number } | null;
}

/** Lector de códigos sobre un `<video>`: todos los que ve en el cuadro (puede ser ninguno). */
export type Lector = { leer: (video: HTMLVideoElement) => Promise<CodigoLeido[]>; liberar?: () => void };

interface CodigoDetectado {
    rawValue: string;
    format?: string;
    boundingBox?: { x: number; y: number; width: number; height: number };
}
interface DetectorDeCodigos {
    detect(fuente: HTMLVideoElement): Promise<CodigoDetectado[]>;
}
type ConstructorDetector = new (opciones?: { formats?: string[] }) => DetectorDeCodigos;

/** Formatos habituales en etiquetas de ONU, routers y antenas (serie, MAC, modelo, EAN y QR). */
export const FORMATOS_CODIGO = ["code_128", "code_39", "code_93", "codabar", "qr_code", "data_matrix", "ean_13", "ean_8", "itf", "upc_a", "upc_e"];

/** `BarcodeDetector` nativo (Chrome en Android) si existe, con la posición de cada código; si no, `null`. */
export function lectorNativo(): Lector | null {
    const Detector = (globalThis as { BarcodeDetector?: ConstructorDetector }).BarcodeDetector;
    if (!Detector) {
        return null;
    }
    const detector = new Detector({ formats: FORMATOS_CODIGO });
    return {
        leer: async (video) =>
            (await detector.detect(video)).map((c) => ({
                valor: c.rawValue,
                formato: c.format ?? "",
                caja: c.boundingBox ? { x: c.boundingBox.x, y: c.boundingBox.y, ancho: c.boundingBox.width, alto: c.boundingBox.height } : null,
            })),
    };
}

/**
 * Respaldo con ZXing (Safari en iPhone y navegadores sin `BarcodeDetector`). Se carga solo
 * cuando hace falta (import dinámico: no pesa en el resto de la app). Lee un código por cuadro y
 * sin posición: cuentan todos (el recuadro no filtra).
 */
export async function lectorZxing(): Promise<Lector> {
    const [{ BrowserMultiFormatReader }, { BarcodeFormat }] = await Promise.all([import("@zxing/browser"), import("@zxing/library")]);
    const lector = new BrowserMultiFormatReader();
    return {
        leer: (video) => {
            try {
                const r = lector.decode(video);
                return Promise.resolve([{ valor: r.getText(), formato: BarcodeFormat[r.getBarcodeFormat()] ?? "" }]);
            } catch {
                // Sin código en este cuadro (NotFoundException): se reintenta en el siguiente.
                return Promise.resolve([]);
            }
        },
    };
}

export interface DependenciasEscaner {
    /** `getUserMedia` con la cámara trasera. */
    obtenerFlujo: () => Promise<MediaStream>;
    /** Conecta el flujo al `<video>` y lo reproduce. */
    conectar: (flujo: MediaStream) => Promise<void>;
    crearLector: () => Promise<Lector>;
}

/**
 * Abre la cámara y el lector. Si el escáner se cerró (`sigue()` es falso) durante cualquier
 * espera —permiso, `play()` o la carga del lector— o algo falla, apaga las pistas de la cámara
 * (y libera el lector) antes de salir: la cámara nunca queda encendida.
 */
export async function abrirEscaner(deps: DependenciasEscaner, sigue: () => boolean): Promise<{ flujo: MediaStream; lector: Lector } | null> {
    const flujo = await deps.obtenerFlujo();
    const apagar = (lector?: Lector) => {
        flujo.getTracks().forEach((t) => t.stop());
        lector?.liberar?.();
    };
    try {
        if (!sigue()) {
            apagar();
            return null;
        }
        await deps.conectar(flujo);
        if (!sigue()) {
            apagar();
            return null;
        }
        const lector = await deps.crearLector();
        if (!sigue()) {
            apagar(lector);
            return null;
        }
        return { flujo, lector };
    } catch (e) {
        apagar();
        throw e;
    }
}

async function lectorPorDefecto(): Promise<Lector> {
    return lectorNativo() ?? (await lectorZxing());
}

/** Pista de video con linterna (`torch`), si el navegador la ofrece. */
interface PistaConLinterna {
    getCapabilities?: () => { torch?: boolean };
    applyConstraints: (c: { advanced: { torch: boolean }[] }) => Promise<void>;
}

/** `true` si la pista admite linterna (`MediaStreamTrack.getCapabilities().torch`). */
export function admiteLinterna(pista: MediaStreamTrack | undefined): boolean {
    const p = pista as unknown as PistaConLinterna | undefined;
    try {
        return p?.getCapabilities?.().torch === true;
    } catch {
        return false;
    }
}

const sinSuscripcion = () => () => undefined;
const enCliente = () => true;
const enServidor = () => false;

/** Geometría del cuadro leído: la vista (`<video>`, `object-fit: cover`), la cámara y el recuadro en la vista. */
export interface GeometriaVisor {
    vista: { ancho: number; alto: number };
    fuente: { ancho: number; alto: number };
    recuadro: { x: number; y: number; ancho: number; alto: number };
}

export type PropsVisorEscaner = {
    /** Lo que se escanea, en grande («Número de serie», «MAC»). */
    objetivo: string;
    /** Ayuda corta bajo el encabezado («Apunta solo al código junto a S/N»). */
    ayuda: string;
    /** Códigos distintos que ya cuentan (dentro del recuadro): «Detectados: N». */
    detectados: number;
    /** Con la hoja de códigos abierta, el lector se detiene (la cámara sigue encendida). */
    pausado?: boolean;
    /** Cada cuadro con códigos, con la geometría para decidir cuáles caen dentro del recuadro. */
    alDetectar: (codigos: CodigoLeido[], geometria: GeometriaVisor) => void;
    alCerrar: () => void;
    alEscribirAMano: () => void;
    /** Para pruebas o para inyectar otro lector; por defecto, nativo y si no, ZXing. */
    crearLector?: () => Promise<Lector>;
    /** La hoja de códigos detectados (va encima del visor). */
    children?: ReactNode;
    className?: string;
};

/**
 * Visor del escáner a pantalla completa: cámara trasera oscurecida con un recuadro de esquinas teal
 * al centro, encabezado «Escaneando: …», «Detectados: N», linterna (si la cámara la tiene),
 * «Escribir a mano» y «Cerrar». Lee cada 200 ms y entrega todo lo que ve con su geometría: qué
 * cuenta, cuándo se muestra la lista y qué se llena lo decide quien lo usa (`@iris/sync/campo`).
 * Sin cámara o sin permiso explica el motivo y deja escribir a mano.
 */
export function VisorEscaner({ objetivo, ayuda, detectados, pausado = false, alDetectar, alCerrar, alEscribirAMano, crearLector = lectorPorDefecto, children, className }: PropsVisorEscaner) {
    const video = useRef<HTMLVideoElement>(null);
    const recuadro = useRef<HTMLDivElement>(null);
    const cerrar = useRef<HTMLButtonElement>(null);
    const pista = useRef<MediaStreamTrack | null>(null);
    const idTitulo = useId();
    // El portal va al `body` (fuera de tarjetas con desenfoque, que romperían `position: fixed`);
    // en el servidor no hay `document`.
    const montado = useSyncExternalStore(sinSuscripcion, enCliente, enServidor);
    const [error, setError] = useState<string | null>(null);
    const [listo, setListo] = useState(false);
    const [linterna, setLinterna] = useState<"no" | "apagada" | "encendida">("no");
    const alDetectarRef = useRef(alDetectar);
    const pausadoRef = useRef(pausado);
    const alCerrarRef = useRef(alCerrar);
    useEffect(() => {
        alDetectarRef.current = alDetectar;
        pausadoRef.current = pausado;
        alCerrarRef.current = alCerrar;
    }, [alDetectar, pausado, alCerrar]);

    useEffect(() => {
        if (!montado) {
            return;
        }
        cerrar.current?.focus();
        const desborde = document.body.style.overflow;
        document.body.style.overflow = "hidden";
        const alTeclear = (e: KeyboardEvent) => {
            if (e.key === "Escape") {
                e.preventDefault();
                alCerrarRef.current();
            }
        };
        document.addEventListener("keydown", alTeclear);
        return () => {
            document.body.style.overflow = desborde;
            document.removeEventListener("keydown", alTeclear);
        };
    }, [montado]);

    useEffect(() => {
        if (!montado) {
            return;
        }
        let activo = true;
        let flujo: MediaStream | null = null;
        let lector: Lector | null = null;
        const detener = () => {
            activo = false;
            flujo?.getTracks().forEach((t) => t.stop());
            lector?.liberar?.();
            pista.current = null;
            const v = video.current;
            if (v) {
                v.srcObject = null;
            }
        };
        (async () => {
            if (!navigator.mediaDevices?.getUserMedia) {
                throw new Error("Este navegador no da acceso a la cámara.");
            }
            const v = video.current;
            if (!v) {
                return;
            }
            const abierto = await abrirEscaner(
                {
                    obtenerFlujo: () => navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" }, audio: false }),
                    conectar: async (f) => {
                        v.srcObject = f;
                        await v.play();
                    },
                    crearLector,
                },
                () => activo,
            );
            if (!abierto) {
                return;
            }
            flujo = abierto.flujo;
            lector = abierto.lector;
            pista.current = flujo.getVideoTracks()[0] ?? null;
            setLinterna(admiteLinterna(pista.current ?? undefined) ? "apagada" : "no");
            setListo(true);
            while (activo) {
                if (!pausadoRef.current && v.readyState >= 2) {
                    const codigos = await lector.leer(v).catch(() => []);
                    const r = recuadro.current?.getBoundingClientRect();
                    const caja = v.getBoundingClientRect();
                    if (activo && !pausadoRef.current && codigos.length > 0 && r) {
                        alDetectarRef.current(codigos, {
                            vista: { ancho: caja.width, alto: caja.height },
                            fuente: { ancho: v.videoWidth, alto: v.videoHeight },
                            recuadro: { x: r.left - caja.left, y: r.top - caja.top, ancho: r.width, alto: r.height },
                        });
                    }
                }
                await new Promise((res) => setTimeout(res, 200));
            }
        })().catch((e: unknown) => {
            if (activo) {
                const nombre = e instanceof Error ? e.name : "";
                setError(
                    nombre === "NotAllowedError"
                        ? "No diste permiso para usar la cámara. Escribe el dato a mano."
                        : nombre === "NotFoundError"
                          ? "Este teléfono no tiene cámara disponible. Escribe el dato a mano."
                          : `No se pudo abrir la cámara (${e instanceof Error ? e.message : String(e)}). Escribe el dato a mano.`,
                );
            }
        });
        return detener;
    }, [montado, crearLector]);

    const cambiarLinterna = () => {
        const p = pista.current as unknown as PistaConLinterna | null;
        if (!p) {
            return;
        }
        const encender = linterna !== "encendida";
        void p
            .applyConstraints({ advanced: [{ torch: encender }] })
            .then(() => setLinterna(encender ? "encendida" : "apagada"))
            .catch(() => setLinterna("no"));
    };

    const contenido = (
        <div
            role="dialog"
            aria-modal="true"
            aria-labelledby={idTitulo}
            data-slot="escaner"
            className={cn("fixed inset-0 z-[60] flex flex-col overflow-hidden bg-black text-white", className)}
        >
            {error ? null : (
                <video ref={video} muted playsInline aria-label="Vista de la cámara para escanear" className="absolute inset-0 size-full object-cover" />
            )}
            <div className="relative z-10 flex items-start justify-between gap-2 bg-black/55 px-4 pt-[max(0.75rem,env(safe-area-inset-top))] pb-3">
                <div className="min-w-0">
                    <h2 id={idTitulo} className="text-2xl leading-tight font-normal">
                        Escaneando: <strong className="font-semibold">{objetivo}</strong>
                    </h2>
                    <p className="mt-1 text-sm text-white/80">{ayuda}</p>
                </div>
                <div className="flex shrink-0 gap-2">
                    {linterna !== "no" ? (
                        <Boton
                            variante="fantasma"
                            tamano="lg"
                            aria-pressed={linterna === "encendida"}
                            onClick={cambiarLinterna}
                            className="size-11 p-0 text-white [--button-fg:white] [--button-bg-hover:rgb(255_255_255/0.15)]"
                        >
                            {linterna === "encendida" ? <FlashlightOff aria-hidden /> : <Flashlight aria-hidden />}
                            <span className="sr-only">{linterna === "encendida" ? "Apagar linterna" : "Encender linterna"}</span>
                        </Boton>
                    ) : null}
                    <Boton ref={cerrar} variante="fantasma" tamano="lg" onClick={alCerrar} className="size-11 p-0 text-white [--button-fg:white] [--button-bg-hover:rgb(255_255_255/0.15)]">
                        <X aria-hidden />
                        <span className="sr-only">Cerrar escáner</span>
                    </Boton>
                </div>
            </div>
            <div className="relative z-0 flex flex-1 flex-col items-center justify-center gap-3 px-4">
                {error ? (
                    <Aviso tono="warning" titulo="Sin escáner" className="w-full max-w-md bg-background text-foreground">
                        {error}
                    </Aviso>
                ) : (
                    <>
                        {/* El recuadro: lo de afuera se oscurece con su sombra; esquinas en teal. */}
                        <div ref={recuadro} data-slot="recuadro" aria-hidden className="relative aspect-[5/3] w-[min(80%,340px)] rounded-lg shadow-[0_0_0_100vmax_rgb(0_0_0/0.6)]">
                            {(["top-0 left-0 border-t-4 border-l-4 rounded-tl-lg", "top-0 right-0 border-t-4 border-r-4 rounded-tr-lg", "bottom-0 left-0 border-b-4 border-l-4 rounded-bl-lg", "right-0 bottom-0 border-r-4 border-b-4 rounded-br-lg"] as const).map((esquina) => (
                                <span key={esquina} className={cn("absolute size-8 border-primary", esquina)} />
                            ))}
                        </div>
                        <p role="status" className="relative text-base font-medium">
                            {listo ? `Detectados: ${detectados}` : "Abriendo la cámara…"}
                        </p>
                    </>
                )}
            </div>
            <div className="relative z-10 flex justify-center gap-2 bg-black/55 px-4 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
                <Boton variante="secundario" tamano="lg" onClick={alEscribirAMano} className="min-w-44">
                    <Keyboard aria-hidden />
                    Escribir a mano
                </Boton>
            </div>
            {children}
        </div>
    );
    return montado ? createPortal(contenido, document.body) : null;
}
