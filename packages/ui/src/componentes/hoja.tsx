"use client";

import { Drawer } from "@heroui/react";
import { X } from "lucide-react";
import { createContext, use, useId, type ComponentProps, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { useEstadoApertura, type PropsDialogo } from "./dialogo";
import { Ranura } from "./ranura";

/*
 * Panel lateral: el `Drawer` de HeroUI (`Drawer.Backdrop` + `Drawer.Content` + `Drawer.Dialog` +
 * `Drawer.Heading`, sobre React Aria). Panel de color sólido (la capa flotante del tema) con borde
 * y sombra, siempre sobre un telón que oscurece lo de atrás. Para el menú «Más» del teléfono y las
 * acciones de una ficha (`PanelProtegido`). Se puede cerrar arrastrándolo hacia su lado.
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
    /**
     * Se acepta por compatibilidad y ya no cambia nada: toda hoja lleva su telón oscurecido (sin
     * telón, el panel se confundía con la página de atrás).
     */
    fondo?: "opaco" | "transparente";
};

export function HojaContenido({ titulo, descripcion, etiquetaCerrar = "Cerrar", lado = "izquierda", fondo, className, children, ...props }: PropsHojaContenido) {
    void fondo; // ya no cambia nada: ver `PropsHojaContenido.fondo`.
    const { abierto, cambiar } = useHoja();
    const idDescripcion = useId();
    const izquierda = lado === "izquierda";
    return (
        // Sin animación de salida (como `Dialogo`): la capa se desmonta en el acto al cerrar.
        <Drawer.Backdrop isOpen={abierto} onOpenChange={cambiar} isDismissable variant="opaque" className="data-exiting:transition-none">
            <Drawer.Content placement={izquierda ? "left" : "right"}>
                <Drawer.Dialog
                    className={cn(
                        "flex min-h-0 w-[min(20rem,85vw)] flex-col gap-2 overflow-y-auto p-4 sm:w-[min(20rem,85vw)]",
                        "rounded-[var(--hx-radius-card)]",
                        izquierda ? "rounded-l-none" : "rounded-r-none",
                        className,
                    )}
                    // El arrastre para cerrar es horizontal: el contenido sigue desplazándose en vertical.
                    style={{ touchAction: "pan-y" }}
                    {...(descripcion ? { "aria-describedby": idDescripcion } : {})}
                    {...props}
                >
                    <div className="flex items-start justify-between gap-2">
                        <div className="grid min-w-0 gap-0.5">
                            <Drawer.Heading className="text-base font-semibold">{titulo}</Drawer.Heading>
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
                </Drawer.Dialog>
            </Drawer.Content>
        </Drawer.Backdrop>
    );
}
