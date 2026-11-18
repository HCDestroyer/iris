"use client";

import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";

export type PropsListaRecortada = Omit<ComponentProps<"span">, "children"> & {
    /** Cada elemento con su clave (chips, píldoras…), de hasta 1.375rem de alto. */
    elementos: readonly { clave: string | number; nodo: ReactNode }[];
    /** Clases del «+N». */
    claseMas?: string;
};

/**
 * Una sola línea con los elementos que caben y «+N» por los que no: nunca se desborda ni baja de
 * línea. Los que no caben pasan a una segunda línea que queda recortada (y marcada como oculta);
 * se cuentan con el ancho real, y se recuenta al cambiar el ancho o la lista. Si ni el primero
 * cabe, ese se encoge (el elemento debe saber recortarse: `min-w-0` + `truncate`). Quien la usa
 * pone el nombre completo de todos en `title` o en un texto para lectores de pantalla.
 */
export function ListaRecortada({ elementos, ...props }: PropsListaRecortada) {
    // Con otra lista se vuelve a contar desde cero.
    return <Recortada key={elementos.map((e) => e.clave).join("|")} elementos={elementos} {...props} />;
}

function Recortada({ elementos, claseMas, className, ...props }: PropsListaRecortada) {
    const caja = useRef<HTMLSpanElement>(null);
    const total = elementos.length;
    // Cuántos caben en la primera línea y dónde termina el último (ahí va el «+N»).
    const [medida, setMedida] = useState({ visibles: total, fin: 0 });
    const visibles = Math.min(total, medida.visibles);

    useEffect(() => {
        const el = caja.current;
        // Sin `ResizeObserver` (pruebas sin diseño) se muestran todos.
        if (!el || typeof ResizeObserver === "undefined") return;
        const contar = () => {
            const hijos = [...el.querySelectorAll<HTMLElement>(":scope > [data-elemento]")];
            const arriba = hijos[0]?.offsetTop ?? 0;
            const primera = hijos.filter((h) => h.offsetTop <= arriba + 1);
            const ultimo = primera.at(-1);
            const nueva = { visibles: Math.max(1, primera.length), fin: ultimo ? ultimo.offsetLeft + ultimo.offsetWidth : 0 };
            setMedida((m) => (m.visibles === nueva.visibles && m.fin === nueva.fin ? m : nueva));
        };
        const observador = new ResizeObserver(contar);
        observador.observe(el);
        return () => observador.disconnect();
    }, []);

    const ocultos = total - visibles;
    return (
        <span
            ref={caja}
            data-slot="lista-recortada"
            {...props}
            // El «+N» va justo después del último que cabe: se le reserva su sitio a la derecha.
            className={cn("relative flex h-[1.375rem] max-w-full min-w-0 flex-wrap items-start gap-x-1 gap-y-3 overflow-hidden", ocultos > 0 && "pr-9", className)}
        >
            {elementos.map((e, i) => (
                <span key={e.clave} data-elemento="" data-oculto={i >= visibles ? "" : undefined} className={cn("flex max-w-full min-w-0", i >= visibles && "invisible")}>
                    {e.nodo}
                </span>
            ))}
            {ocultos > 0 ? (
                <span data-slot="lista-mas" style={{ left: medida.fin + 4 }} className={cn("absolute top-0 inline-flex h-[1.375rem] items-center rounded-md bg-elevated px-1.5 text-xs font-semibold text-muted-foreground tabular-nums", claseMas)}>
                    +{ocultos}
                </span>
            ) : null}
        </span>
    );
}
