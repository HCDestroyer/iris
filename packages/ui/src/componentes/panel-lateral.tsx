"use client";

import { useEffect, useRef, type ComponentProps, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "../utilidades";

/*
 * Panel lateral flotante sin telón (excepción aprobada por el PM a la regla de `Hoja`, nov. 2026):
 * una tarjeta elevada a la derecha, bajo la barra superior, que **flota sobre la pantalla** y la deja
 * a la vista y usable. La página de atrás no se corre ni se reacomoda al abrirlo (pedido del PM:
 * la agenda quedaba apretada) y los clics fuera del panel le llegan a ella. Lo usa el chat del
 * equipo del panel.
 *
 * - No es modal: no atrapa el foco ni vuelve inerte la página (`role="dialog"` con
 *   `aria-modal="false"` y su nombre).
 * - Al abrir, el foco entra al panel (al elemento con `data-foco-inicial` o al panel mismo); al
 *   cerrar, vuelve a donde estaba (el disparador), si sigue en la página.
 * - Escape lo cierra mientras el foco está dentro, salvo que algo de adentro ya lo haya usado
 *   (una lista abierta, un diálogo encima).
 * - Panel sólido (la capa flotante del tema, `--overlay`), con borde, sombra y esquinas redondeadas,
 *   separado 12 px de los bordes (desde `sm`, 380 px de ancho). En el teléfono ocupa toda la
 *   pantalla bajo la barra superior, sin esquinas. La cabecera de quien lo usa (con «Cerrar») va
 *   fuera de lo que se desplaza: siempre hay salida.
 */

export type PropsPanelLateral = Omit<ComponentProps<"section">, "role" | "title"> & {
    abierto: boolean;
    alCerrar: () => void;
    /** Nombre accesible del panel (la cabecera visible la pone quien lo usa). */
    etiqueta: string;
    children: ReactNode;
};

const FOCO_INICIAL = "[data-foco-inicial]";

export function PanelLateral({ abierto, alCerrar, etiqueta, className, children, onKeyDown, ...props }: PropsPanelLateral) {
    const panel = useRef<HTMLElement>(null);
    const previo = useRef<HTMLElement | null>(null);

    useEffect(() => {
        if (!abierto) return undefined;
        const activo = document.activeElement;
        previo.current = activo instanceof HTMLElement && activo !== document.body ? activo : null;
        const nodo = panel.current;
        const destino = nodo?.querySelector<HTMLElement>(FOCO_INICIAL) ?? nodo;
        destino?.focus({ preventScroll: true });
        return () => {
            const volver = previo.current;
            previo.current = null;
            // Solo si el foco seguía dentro del panel (o se perdió): no se le roba a otra parte.
            const actual = document.activeElement;
            const dentro = actual === null || actual === document.body || !actual.isConnected || (nodo?.contains(actual) ?? true);
            if (volver?.isConnected && dentro) volver.focus({ preventScroll: true });
        };
    }, [abierto]);

    if (!abierto) return null;

    const alTeclear = (evento: KeyboardEvent<HTMLElement>) => {
        onKeyDown?.(evento);
        if (evento.key !== "Escape" || evento.defaultPrevented || evento.nativeEvent.defaultPrevented) return;
        // Un Escape que viene de una capa portada (diálogo, lista) no es del panel.
        if (!(evento.target instanceof Node) || !panel.current?.contains(evento.target)) return;
        evento.preventDefault();
        alCerrar();
    };

    return (
        <section
            ref={panel}
            role="dialog"
            aria-modal="false"
            aria-label={etiqueta}
            tabIndex={-1}
            data-slot="panel-lateral"
            className={cn(
                // Teléfono: a pantalla completa bajo la barra superior.
                "fixed inset-x-0 top-14 bottom-0 z-40 flex min-w-0 flex-col overflow-hidden outline-none",
                "border-t border-border bg-[var(--overlay)] text-[var(--overlay-foreground)] shadow-[var(--overlay-shadow)]",
                // Desde `sm`: tarjeta flotante de 380 px, a 12 px de la barra y de los bordes.
                "sm:top-[calc(3.5rem+0.75rem)] sm:right-3 sm:bottom-3 sm:left-auto sm:w-[min(23.75rem,calc(100vw-1.5rem))]",
                "sm:rounded-[var(--hx-radius-card)] sm:border",
                className,
            )}
            onKeyDown={alTeclear}
            {...props}
        >
            {children}
        </section>
    );
}
