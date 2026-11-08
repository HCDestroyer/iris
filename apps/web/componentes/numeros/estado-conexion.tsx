import { cn, type Tono } from "@iris/ui";
import { CircleAlert, CircleDashed, CircleX, Clock, Power, type LucideIcon } from "lucide-react";
import { presentarConexion, type IconoConexion } from "@/lib/numeros/conexion";
import type { Conexion } from "@/lib/numeros/tipos";

const ICONOS: Record<Exclude<IconoConexion, "punto">, LucideIcon> = { reloj: Clock, x: CircleX, aviso: CircleAlert, apagado: Power, pendiente: CircleDashed };

const COLOR: Record<Tono, string> = {
    success: "text-success",
    warning: "text-warning",
    error: "text-error",
    info: "text-info",
    neutral: "text-muted-foreground",
};

/**
 * Columna «Conexión»: icono + texto + color. «Conectado» es solo un punto (todo bien); lo demás
 * lleva su icono porque pide actuar o explica por qué no funciona.
 */
export function EstadoConexion({ conexion, className }: { conexion: Conexion; className?: string }) {
    const p = presentarConexion(conexion);
    const Icono = p.icono === "punto" ? null : ICONOS[p.icono];
    return (
        <span data-conexion={conexion} className={cn("inline-flex min-w-0 items-start gap-1.5 text-sm font-medium", COLOR[p.tono], className)}>
            {Icono ? <Icono aria-hidden className="mt-0.5 size-4 shrink-0" /> : <span aria-hidden className="mt-1.5 size-2 shrink-0 rounded-full bg-current" />}
            <span className="min-w-0">{p.texto}</span>
        </span>
    );
}
