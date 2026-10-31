"use client";

import { drawerVariants } from "@heroui/styles";
import { X } from "lucide-react";
import { createContext, use, useId, type ComponentProps, type ReactNode } from "react";
import { Dialog, Heading, Modal, ModalOverlay } from "react-aria-components";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { animacionPanel, useEstadoApertura, type PropsDialogo } from "./dialogo";
import { Ranura } from "./ranura";

/*
 * Panel lateral sobre HeroUI `Drawer` (React Aria `ModalOverlay` + `Modal` + `Dialog`) en vidrio
 * fuerte. Para el menú en teléfono.
 */

type ContextoHoja = ReturnType<typeof useEstadoApertura>;

const ContextoHoja = createContext<ContextoHoja | null>(null);

function useHoja(): ContextoHoja {
    const contexto = use(ContextoHoja);
    if (!contexto) throw new Error("HojaContenido, HojaDisparador y HojaCerrar van dentro de <Hoja>.");
    return contexto;
}

export function Hoja({ children, ...props }: PropsDialogo) {
    const estado = useEstadoApertura(props);
    return <ContextoHoja value={estado}>{children}</ContextoHoja>;
}

type PropsBotonHoja = ComponentProps<"button"> & { asChild?: boolean };

function Interruptor({ abrir, asChild, onClick, children, ...props }: PropsBotonHoja & { abrir: boolean }) {
    const { abierto, cambiar } = useHoja();
    const alClic = (evento: React.MouseEvent<HTMLButtonElement>) => {
        onClick?.(evento);
        if (!evento.defaultPrevented) cambiar(abrir);
    };
    const extra = abrir ? { "aria-haspopup": "dialog" as const, "aria-expanded": abierto } : {};
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

export function HojaDisparador(props: PropsBotonHoja) {
    return <Interruptor abrir {...props} />;
}

export function HojaCerrar(props: PropsBotonHoja) {
    return <Interruptor abrir={false} {...props} />;
}

export type PropsHojaContenido = Omit<ComponentProps<"div">, "title" | "role"> & {
    titulo: ReactNode;
    /** Bajo el título (p. ej. «TK-000101 · Sin internet»); describe el panel. */
    descripcion?: ReactNode;
    /** Texto del botón de cerrar para lectores de pantalla. */
    etiquetaCerrar?: string;
    lado?: "izquierda" | "derecha";
    /** `transparente`: lo de atrás se ve tal cual (p. ej. la ficha mientras se hace una acción). */
    fondo?: "opaco" | "transparente";
};

export function HojaContenido({ titulo, descripcion, etiquetaCerrar = "Cerrar", lado = "izquierda", fondo = "opaco", className, children, ...props }: PropsHojaContenido) {
    const { abierto, cambiar } = useHoja();
    const piezas = drawerVariants({ placement: lado === "izquierda" ? "left" : "right", variant: "opaque" });
    const idDescripcion = useId();
    return (
        <ModalOverlay
            isOpen={abierto}
            onOpenChange={cambiar}
            isDismissable
            className={piezas.backdrop({ className: cn("data-exiting:animate-none", fondo === "transparente" && "bg-transparent backdrop-blur-none") })}
        >
            <Modal
                className={cn(
                    "glass-strong fixed inset-y-0 z-(--z-index-overlay) flex w-[min(20rem,85vw)] flex-col shadow-xl",
                    lado === "izquierda" ? "left-0 rounded-l-none data-entering:slide-in-from-left-8" : "right-0 rounded-r-none data-entering:slide-in-from-right-8",
                    animacionPanel,
                    className,
                )}
            >
                <Dialog className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto p-4 outline-none" {...(descripcion ? { "aria-describedby": idDescripcion } : {})} {...props}>
                    <div className="flex items-start justify-between gap-2">
                        <div className="grid min-w-0 gap-0.5">
                            <Heading slot="title" className="text-base font-semibold">
                                {titulo}
                            </Heading>
                            {descripcion ? (
                                <p id={idDescripcion} className="text-sm break-words text-muted-foreground">
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
