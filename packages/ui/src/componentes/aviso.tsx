import type { LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";
import { AlertaAviso } from "./aviso-alerta";
import { estadoHeroui, iconoPorTono, superficiePorTono, textoPorTono, type Tono } from "./tonos";

export type PropsAviso = Omit<ComponentProps<"div">, "title"> & {
    tono?: Tono;
    titulo: ReactNode;
    icono?: LucideIcon;
    /** Acción del aviso, p. ej. el botón «Reintentar». */
    accion?: ReactNode;
};

/**
 * Aviso en la página con icono + título + texto: el `Alert` de HeroUI (`Alert`, `Alert.Indicator`,
 * `Alert.Content`, `Alert.Title` y la pieza `alert__description`) con el fondo tintado y sólido del
 * tono de Horus (nunca transparente) y su borde. Los de tono `error` se anuncian
 * (`role="alert"`); el resto como `status`. Sirve también desde un componente de servidor.
 */
export function Aviso({ tono = "info", titulo, icono, accion, className, children, ...props }: PropsAviso) {
    const Icono = icono ?? iconoPorTono[tono];
    return (
        <AlertaAviso
            data-slot="aviso"
            data-tono={tono}
            role={tono === "error" ? "alert" : "status"}
            {...props}
            status={estadoHeroui[tono]}
            indicador={<Icono aria-hidden className="size-4" />}
            titulo={titulo}
            claseTitulo={textoPorTono[tono]}
            accion={accion}
            className={cn("flex-col items-stretch justify-normal gap-3 rounded-lg border p-3.5 text-sm shadow-none sm:flex-row sm:items-start", superficiePorTono[tono], className)}
        >
            {children}
        </AlertaAviso>
    );
}
