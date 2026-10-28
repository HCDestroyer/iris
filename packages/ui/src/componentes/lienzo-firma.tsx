import { Eraser } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type PointerEvent } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { dibujarTrazos, type Trazo } from "./firma";

export type PropsLienzoFirma = {
    /** Trazos actuales (cada vez que el cliente levanta el dedo, o al borrar). */
    alCambiar: (trazos: Trazo[], tamano: { ancho: number; alto: number }) => void;
    /** Nombre accesible del área de firma. */
    etiqueta?: string;
    alto?: number;
    className?: string;
    disabled?: boolean;
};

/** Color del trazo en pantalla: el del texto del tema (se lee de la variable CSS). */
function colorTinta(el: HTMLElement): string {
    return getComputedStyle(el).color || "#111111";
}

/**
 * Lienzo para que el cliente firme con el dedo (pointer events: dedo, lápiz o ratón). Guarda los
 * trazos en coordenadas CSS; el SVG y el PNG se generan con `trazosASvg` / `trazosAPng`.
 * `touch-action: none` evita que el dedo desplace la página mientras firma.
 */
export function LienzoFirma({ alCambiar, etiqueta = "Firma del cliente", alto = 200, className, disabled }: PropsLienzoFirma) {
    const lienzo = useRef<HTMLCanvasElement>(null);
    const trazos = useRef<Trazo[]>([]);
    const actual = useRef<Trazo | null>(null);
    const [vacio, setVacio] = useState(true);

    const tamano = useCallback(() => {
        const c = lienzo.current;
        return { ancho: c?.clientWidth ?? 0, alto: c?.clientHeight ?? alto };
    }, [alto]);

    const redibujar = useCallback(() => {
        const c = lienzo.current;
        const ctx = c?.getContext("2d");
        if (!c || !ctx) {
            return;
        }
        const escala = window.devicePixelRatio || 1;
        const { ancho, alto: altoCss } = tamano();
        if (c.width !== Math.round(ancho * escala) || c.height !== Math.round(altoCss * escala)) {
            c.width = Math.round(ancho * escala);
            c.height = Math.round(altoCss * escala);
        }
        ctx.setTransform(escala, 0, 0, escala, 0, 0);
        ctx.clearRect(0, 0, ancho, altoCss);
        dibujarTrazos(ctx, trazos.current, colorTinta(c));
    }, [tamano]);

    useEffect(() => {
        redibujar();
        const c = lienzo.current;
        if (!c || typeof ResizeObserver === "undefined") {
            return;
        }
        const observador = new ResizeObserver(() => redibujar());
        observador.observe(c);
        return () => observador.disconnect();
    }, [redibujar]);

    const punto = (e: PointerEvent<HTMLCanvasElement>) => {
        const caja = e.currentTarget.getBoundingClientRect();
        return { x: e.clientX - caja.left, y: e.clientY - caja.top };
    };

    const empezar = (e: PointerEvent<HTMLCanvasElement>) => {
        if (disabled) {
            return;
        }
        e.currentTarget.setPointerCapture(e.pointerId);
        actual.current = [punto(e)];
        trazos.current = [...trazos.current, actual.current];
        redibujar();
    };
    const mover = (e: PointerEvent<HTMLCanvasElement>) => {
        if (!actual.current) {
            return;
        }
        actual.current.push(punto(e));
        redibujar();
    };
    const terminar = () => {
        if (!actual.current) {
            return;
        }
        actual.current = null;
        setVacio(false);
        alCambiar(trazos.current.map((t) => [...t]), tamano());
    };
    const borrar = () => {
        trazos.current = [];
        actual.current = null;
        setVacio(true);
        redibujar();
        alCambiar([], tamano());
    };

    return (
        <div data-slot="lienzo-firma" className={cn("grid gap-2", className)}>
            <canvas
                ref={lienzo}
                role="img"
                aria-label={vacio ? `${etiqueta}: vacía` : `${etiqueta}: firmada`}
                className="w-full touch-none rounded-lg border border-dashed border-border bg-background text-foreground"
                style={{ height: alto }}
                onPointerDown={empezar}
                onPointerMove={mover}
                onPointerUp={terminar}
                onPointerCancel={terminar}
            />
            <div className="flex items-center justify-between gap-2 text-sm text-muted-foreground">
                <span>{vacio ? "Firma con el dedo dentro del recuadro" : "Firma capturada"}</span>
                <Boton variante="fantasma" tamano="sm" onClick={borrar} disabled={vacio || disabled}>
                    <Eraser aria-hidden />
                    Borrar firma
                </Boton>
            </div>
        </div>
    );
}
