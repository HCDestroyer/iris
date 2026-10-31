"use client";

import { modalVariants } from "@heroui/styles";
import { X } from "lucide-react";
import { createContext, use, useCallback, useEffect, useId, useMemo, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { Dialog, Heading, Modal, ModalOverlay } from "react-aria-components";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { Ranura } from "./ranura";

/*
 * Diálogo modal sobre HeroUI `Modal` (React Aria: `ModalOverlay` + `Modal` + `Dialog`): foco
 * contenido, Escape y tocar fuera cierran, el resto de la página queda inerte y el foco vuelve al
 * disparador. En vidrio fuerte; en el teléfono ocupa el ancho y sube desde abajo.
 */

type ContextoDialogo = { abierto: boolean; cambiar: (abierto: boolean) => void };

const ContextoDialogo = createContext<ContextoDialogo | null>(null);

/** Estado de apertura de un `Dialogo` o `Hoja` (controlado con `open` o interno). */
export function useEstadoApertura({ open, defaultOpen = false, onOpenChange }: PropsDialogo): ContextoDialogo {
    const [interno, setInterno] = useState(defaultOpen);
    const controlado = open !== undefined;
    const abierto = controlado ? open : interno;
    const cambiar = useCallback(
        (valor: boolean) => {
            if (!controlado) setInterno(valor);
            onOpenChange?.(valor);
        },
        [controlado, onOpenChange],
    );
    return useMemo(() => ({ abierto, cambiar }), [abierto, cambiar]);
}

export type PropsDialogo = {
    open?: boolean;
    defaultOpen?: boolean;
    onOpenChange?: (abierto: boolean) => void;
    children?: ReactNode;
};

export function Dialogo({ children, ...props }: PropsDialogo) {
    const estado = useEstadoApertura(props);
    return <ContextoDialogo value={estado}>{children}</ContextoDialogo>;
}

export function useDialogo(): ContextoDialogo {
    const contexto = use(ContextoDialogo);
    if (!contexto) throw new Error("DialogoContenido, DialogoDisparador y DialogoCerrar van dentro de <Dialogo>.");
    return contexto;
}

/** Abre o cierra el diálogo con un clic; con `asChild` usa al hijo (p. ej. un `Boton`). */
function Interruptor({ abrir, asChild, contexto, onClick, children, ...props }: ComponentProps<"button"> & { abrir: boolean; asChild?: boolean; contexto: ContextoDialogo }) {
    const alClic = (evento: React.MouseEvent<HTMLButtonElement>) => {
        onClick?.(evento);
        if (!evento.defaultPrevented) contexto.cambiar(abrir);
    };
    const extra = abrir ? { "aria-haspopup": "dialog" as const, "aria-expanded": contexto.abierto } : {};
    if (asChild) {
        return (
            <Ranura {...extra} {...(props as ComponentProps<typeof Ranura>)} onClick={alClic as ComponentProps<typeof Ranura>["onClick"]}>
                {children}
            </Ranura>
        );
    }
    return (
        <button type="button" {...extra} {...props} onClick={alClic}>
            {children}
        </button>
    );
}

export type PropsDisparador = ComponentProps<"button"> & { asChild?: boolean };

export function DialogoDisparador(props: PropsDisparador) {
    return <Interruptor abrir contexto={useDialogo()} {...props} />;
}

export function DialogoCerrar(props: PropsDisparador) {
    return <Interruptor abrir={false} contexto={useDialogo()} {...props} />;
}

/**
 * Emula `onOpenAutoFocus` (antes de Radix): al abrir, llama al manejador con un evento cuyo
 * `target` es el diálogo; si hace `preventDefault`, el foco queda donde el manejador lo puso.
 */
function FocoAlAbrir({ alAbrir }: { alAbrir: ((evento: Event) => void) | undefined }) {
    const ancla = useRef<HTMLSpanElement>(null);
    useEffect(() => {
        const dialogo = ancla.current?.closest<HTMLElement>('[role="dialog"], [role="alertdialog"]');
        if (!dialogo || !alAbrir) return;
        const evento = new Event("iris.foco-al-abrir", { cancelable: true });
        dialogo.addEventListener(evento.type, alAbrir, { once: true });
        dialogo.dispatchEvent(evento);
        // `alAbrir` cambia en cada render del padre: solo cuenta el de la apertura.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    return <span ref={ancla} hidden />;
}

/** Fondo oscurecido de HeroUI (`modal__backdrop`), por encima de todo lo de la página. */
/*
 * Solo hay animación de entrada. Al cerrar, la capa se desmonta en el acto (sin `data-exiting`): con
 * salida animada el foco seguiría ~100 ms dentro de la capa que se va y un Escape o un clic
 * inmediato le llegaría a ella y no a la de abajo.
 */
export const clasesFondo = modalVariants({ variant: "opaque" }).backdrop({ className: "data-exiting:animate-none" });

/** Entrada del panel (tw-animate, como HeroUI); sin movimiento si el sistema lo pide. */
export const animacionPanel = "data-entering:animate-in data-entering:fade-in-0 data-entering:duration-200 data-exiting:animate-none motion-reduce:animate-none";

export type PropsDialogoContenido = Omit<ComponentProps<"div">, "title" | "role"> & {
    titulo: ReactNode;
    descripcion?: ReactNode;
    etiquetaCerrar?: string;
    role?: "dialog" | "alertdialog";
    /** Al abrir: `preventDefault` deja el foco donde lo ponga el manejador (p. ej. en la búsqueda). */
    onOpenAutoFocus?: (evento: Event) => void;
};

export function DialogoContenido({ titulo, descripcion, etiquetaCerrar = "Cerrar", className, children, role, onOpenAutoFocus, ...props }: PropsDialogoContenido) {
    const { abierto, cambiar } = useDialogo();
    const idDescripcion = useId();
    return (
        <ModalOverlay isOpen={abierto} onOpenChange={cambiar} isDismissable className={clasesFondo}>
            <Modal
                className={cn(
                    "glass-strong fixed z-(--z-index-overlay) flex max-h-[92dvh] w-full flex-col overflow-y-auto shadow-xl",
                    "inset-x-0 bottom-0 rounded-t-[var(--hx-radius-card)] rounded-b-none data-entering:slide-in-from-bottom-4",
                    "sm:inset-auto sm:top-1/2 sm:left-1/2 sm:w-[min(36rem,calc(100vw-2rem))] sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-[var(--hx-radius-card)] sm:data-entering:slide-in-from-bottom-0 sm:data-entering:zoom-in-95",
                    animacionPanel,
                    className,
                )}
            >
                <Dialog
                    role={role ?? "dialog"}
                    aria-describedby={descripcion ? idDescripcion : undefined}
                    className="flex min-h-0 flex-1 flex-col gap-4 p-5 outline-none"
                    {...props}
                >
                    <FocoAlAbrir alAbrir={onOpenAutoFocus} />
                    <div className="flex items-start justify-between gap-3">
                        <div className="grid gap-1">
                            <Heading slot="title" className="text-lg font-semibold tracking-[-0.015em]">
                                {titulo}
                            </Heading>
                            {descripcion ? (
                                <p id={idDescripcion} className="text-sm text-muted-foreground">
                                    {descripcion}
                                </p>
                            ) : null}
                        </div>
                        <Boton variante="fantasma" tamano="icono-sm" aria-label={etiquetaCerrar} title={etiquetaCerrar} onClick={() => cambiar(false)}>
                            <X />
                        </Boton>
                    </div>
                    {children}
                </Dialog>
            </Modal>
        </ModalOverlay>
    );
}

/** Botones al pie del diálogo: en el teléfono, apilados con la acción principal arriba. */
export function DialogoPie({ className, ...props }: ComponentProps<"div">) {
    return <div className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)} {...props} />;
}
