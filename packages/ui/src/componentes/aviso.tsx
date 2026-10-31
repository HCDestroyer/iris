import { alertVariants } from "@heroui/styles";
import type { LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";
import { estadoHeroui, iconoPorTono, superficiePorTono, textoPorTono, type Tono } from "./tonos";

export type PropsAviso = Omit<ComponentProps<"div">, "title"> & {
    tono?: Tono;
    titulo: ReactNode;
    icono?: LucideIcon;
    /** Acción del aviso, p. ej. el botón «Reintentar». */
    accion?: ReactNode;
};

/**
 * Aviso con icono + título + texto, con las piezas de HeroUI `Alert` (`alert`, `alert__indicator`,
 * `alert__title`…) y el fondo tintado de Horus. Los de tono `error` se anuncian
 * (`role="alert"`); el resto como `status`.
 */
export function Aviso({ tono = "info", titulo, icono, accion, className, children, ...props }: PropsAviso) {
    const Icono = icono ?? iconoPorTono[tono];
    const piezas = alertVariants({ status: estadoHeroui[tono] });
    return (
        <div
            data-slot="aviso"
            data-tono={tono}
            role={tono === "error" ? "alert" : "status"}
            className={piezas.base({
                className: cn("flex-col items-stretch justify-normal gap-3 rounded-lg border p-3.5 text-sm shadow-none sm:flex-row sm:items-start", superficiePorTono[tono], className),
            })}
            {...props}
        >
            <div className="flex min-w-0 flex-1 gap-2.5">
                <Icono aria-hidden className={piezas.indicator({ className: cn("mt-0.5 box-content size-4 shrink-0 p-0", textoPorTono[tono]) })} />
                <div className={piezas.content({ className: "grid min-w-0 gap-0.5" })}>
                    <p className={piezas.title({ className: cn("leading-normal", textoPorTono[tono]) })}>{titulo}</p>
                    {children ? <div className="text-foreground">{children}</div> : null}
                </div>
            </div>
            {accion ? <div className="shrink-0 sm:ml-2">{accion}</div> : null}
        </div>
    );
}
