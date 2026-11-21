"use client";

import { Drawer } from "@heroui/react";
import { useEffect, useRef, type ComponentProps, type KeyboardEvent, type ReactNode } from "react";
import { cn } from "../utilidades";

/*
 * Panel lateral flotante (rediseño, nov. 2026): una isla a 16 px de los bordes, radio de 20 px y
 * 470 px de ancho, a la derecha. Dos formas:
 *
 * - **Sin telón** (por omisión; excepción aprobada por el PM a la regla de `Hoja`): **flota sobre la
 *   pantalla** y la deja a la vista y usable. La página de atrás no se corre ni se reacomoda al abrirlo (pedido del PM:
 * la agenda quedaba apretada) y los clics fuera del panel le llegan a ella. Lo usa el chat del
 * equipo del panel.
 *
 * - No es modal: no atrapa el foco ni vuelve inerte la página (`role="dialog"` con
 *   `aria-modal="false"` y su nombre).
 * - Al abrir, el foco entra al panel (al elemento con `data-foco-inicial` o al panel mismo); al
 *   cerrar, vuelve a donde estaba (el disparador), si sigue en la página.
 * - Escape lo cierra mientras el foco está dentro, salvo que algo de adentro ya lo haya usado
 *   (una lista abierta, un diálogo encima).
 * - Panel sólido (la capa flotante del tema, `--overlay`), con borde, sombra marcada y esquinas de
 *   20 px, separado 16 px de los bordes (desde `sm`, 470 px de ancho). En el teléfono ocupa toda la
 *   pantalla. La cabecera de quien lo usa (con «Cerrar») va fuera de lo que se desplaza: siempre
 *   hay salida.
 *
 * - **Con telón** (`telon`): para registrar o editar algo sin perder la lista de fondo. Es el
 *   `Drawer` de HeroUI (modal: atrapa el foco, Escape y tocar el telón lo cierran) con la misma
 *   isla flotante; lo de atrás se oscurece.
 */

/** Geometría de la isla: a pantalla completa en el teléfono; desde `sm`, 470 px a 16 px de los bordes. */
const ISLA_PANEL = [
    "flex min-w-0 flex-col overflow-hidden outline-none",
    "bg-[var(--overlay)] text-[var(--overlay-foreground)] shadow-[0_0_0_1px_var(--iris-borde),var(--iris-sombra-modal)]",
    "sm:rounded-[var(--iris-radio-isla)] sm:border sm:border-border",
].join(" ");

export type PropsPanelLateral = Omit<ComponentProps<"section">, "role" | "title"> & {
    abierto: boolean;
    alCerrar: () => void;
    /** Nombre accesible del panel (la cabecera visible la pone quien lo usa). */
    etiqueta: string;
    /** Con telón oscurecido y modal (un formulario corto sin perder la lista); por omisión, sin telón. */
    telon?: boolean;
    children: ReactNode;
};

const FOCO_INICIAL = "[data-foco-inicial]";

export function PanelLateral({ telon = false, ...props }: PropsPanelLateral) {
    return telon ? <PanelConTelon {...props} /> : <PanelSinTelon {...props} />;
}

/** Con telón: el `Drawer` de HeroUI con la geometría de la isla. */
function PanelConTelon({ abierto, alCerrar, etiqueta, className, children }: Omit<PropsPanelLateral, "telon">) {
    return (
        <Drawer.Backdrop isOpen={abierto} onOpenChange={(a) => (a ? undefined : alCerrar())} isDismissable variant="opaque" className="data-exiting:transition-none">
            <Drawer.Content placement="right" className="sm:p-4">
                <Drawer.Dialog
                    aria-label={etiqueta}
                    data-slot="panel-lateral"
                    data-telon="true"
                    className={cn(ISLA_PANEL, "h-dvh w-screen p-0 sm:h-full sm:w-[min(29.375rem,calc(100vw-2rem))]", className)}
                >
                    {children}
                </Drawer.Dialog>
            </Drawer.Content>
        </Drawer.Backdrop>
    );
}

function PanelSinTelon({ abierto, alCerrar, etiqueta, className, children, onKeyDown, ...props }: Omit<PropsPanelLateral, "telon">) {
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
                // Teléfono: a pantalla completa.
                "fixed inset-0 z-40",
                ISLA_PANEL,
                // Desde `sm`: isla flotante de 470 px, a 16 px de los bordes.
                "sm:top-4 sm:right-4 sm:bottom-4 sm:left-auto sm:w-[min(29.375rem,calc(100vw-2rem))]",
                className,
            )}
            onKeyDown={alTeclear}
            {...props}
        >
            {children}
        </section>
    );
}
