"use client";

import { ScanLine, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { cn } from "../utilidades";
import { Aviso } from "./aviso";
import { Boton } from "./boton";

/** Lector de códigos sobre un `<video>`: devuelve el texto leído o `null` si aún no hay nada. */
export type Lector = { leer: (video: HTMLVideoElement) => Promise<string | null>; liberar?: () => void };

interface CodigoDetectado {
    rawValue: string;
}
interface DetectorDeCodigos {
    detect(fuente: HTMLVideoElement): Promise<CodigoDetectado[]>;
}
type ConstructorDetector = new (opciones?: { formats?: string[] }) => DetectorDeCodigos;

/** Formatos habituales en etiquetas de ONU, routers y antenas (serie y MAC). */
export const FORMATOS_CODIGO = ["code_128", "code_39", "code_93", "qr_code", "data_matrix", "ean_13", "ean_8", "itf", "upc_a"];

/** `BarcodeDetector` nativo (Chrome en Android) si existe; si no, `null`. */
export function lectorNativo(): Lector | null {
    const Detector = (globalThis as { BarcodeDetector?: ConstructorDetector }).BarcodeDetector;
    if (!Detector) {
        return null;
    }
    const detector = new Detector({ formats: FORMATOS_CODIGO });
    return {
        leer: async (video) => (await detector.detect(video))[0]?.rawValue ?? null,
    };
}

/**
 * Respaldo con ZXing (Safari en iPhone y navegadores sin `BarcodeDetector`). Se carga solo
 * cuando hace falta (import dinámico: no pesa en el resto de la app).
 */
export async function lectorZxing(): Promise<Lector> {
    const { BrowserMultiFormatReader } = await import("@zxing/browser");
    const lector = new BrowserMultiFormatReader();
    return {
        leer: (video) => {
            try {
                return Promise.resolve(lector.decode(video).getText());
            } catch {
                // Sin código en este cuadro (NotFoundException): se reintenta en el siguiente.
                return Promise.resolve(null);
            }
        },
    };
}

export type PropsEscaner = {
    /** Texto leído (serie, MAC o QR). El escáner se detiene al leer. */
    alLeer: (texto: string) => void;
    alCerrar: () => void;
    /** Para pruebas o para inyectar otro lector; por defecto, nativo y si no, ZXing. */
    crearLector?: () => Promise<Lector>;
    className?: string;
};

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

/**
 * Escáner de código de barras o QR con la cámara trasera. Si no hay cámara o el permiso se niega,
 * muestra el motivo y el técnico escribe el dato a mano (el formulario sigue disponible).
 */
export function Escaner({ alLeer, alCerrar, crearLector = lectorPorDefecto, className }: PropsEscaner) {
    const video = useRef<HTMLVideoElement>(null);
    const [error, setError] = useState<string | null>(null);
    const [listo, setListo] = useState(false);
    const alLeerRef = useRef(alLeer);
    useEffect(() => {
        alLeerRef.current = alLeer;
    }, [alLeer]);

    useEffect(() => {
        let activo = true;
        let flujo: MediaStream | null = null;
        let lector: Lector | null = null;
        const detener = () => {
            activo = false;
            flujo?.getTracks().forEach((t) => t.stop());
            lector?.liberar?.();
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
            setListo(true);
            while (activo) {
                const texto = v.readyState >= 2 ? await lector.leer(v).catch(() => null) : null;
                if (texto && activo) {
                    detener();
                    alLeerRef.current(texto.trim());
                    return;
                }
                await new Promise((r) => setTimeout(r, 250));
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
    }, [crearLector]);

    return (
        <div data-slot="escaner" className={cn("grid gap-2", className)}>
            {error ? (
                <Aviso tono="warning" titulo="Sin escáner">
                    {error}
                </Aviso>
            ) : (
                <div className="relative overflow-hidden rounded-lg border border-border bg-black">
                    <video ref={video} muted playsInline aria-label="Vista de la cámara para escanear" className="aspect-[4/3] w-full object-cover" />
                    <div aria-hidden className="pointer-events-none absolute inset-x-6 top-1/2 h-0.5 -translate-y-1/2 bg-primary/80" />
                    <p role="status" className="absolute inset-x-0 bottom-0 flex items-center justify-center gap-1.5 bg-black/60 p-2 text-sm text-white">
                        <ScanLine aria-hidden className="size-4" />
                        {listo ? "Apunta al código de barras o QR" : "Abriendo la cámara…"}
                    </p>
                </div>
            )}
            <Boton variante="secundario" onClick={alCerrar}>
                <X aria-hidden />
                Cerrar escáner
            </Boton>
        </div>
    );
}
