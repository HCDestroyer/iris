import { cn } from "@iris/ui";
import { Phone, ShieldCheck, ShieldQuestion, type LucideIcon } from "lucide-react";
import { nivelVerificacion, TEXTO_VERIFICACION, type NivelVerificacion } from "@/lib/asistente/fase3b";
import type { Conversacion } from "@/lib/conversaciones/tipos";

const VISTA: Record<NivelVerificacion | "ninguna", { icono: LucideIcon; punto: string; texto: string }> = {
    fuerte: { icono: ShieldCheck, punto: "bg-success", texto: "text-success" },
    numero: { icono: Phone, punto: "bg-muted-foreground", texto: "text-muted-foreground" },
    ninguna: { icono: ShieldQuestion, punto: "bg-warning", texto: "text-warning" },
};

/**
 * Qué tan verificado está el cliente de la conversación (contratos/conversaciones.md §17.8.2):
 * un punto, un icono y un texto corto. Sale **solo** de `contacto.verificacion`: el panel nunca lo
 * deduce de una acción propia (el servidor lo baja solo de «identidad verificada» a «su número
 * coincide» al vencer, y confirmar el cliente de una conversación resuelta no lo sube). Si el
 * servidor no manda el campo, o el contacto no es un cliente, no se muestra nada.
 */
export function IndicadorVerificacion({ contacto, detalle = false, className }: { contacto: Conversacion["contacto"]; detalle?: boolean; className?: string }) {
    const nivel = nivelVerificacion(contacto);
    if (nivel === undefined || contacto.tipo !== "cliente") return null;
    const clave = nivel ?? "ninguna";
    const v = VISTA[clave];
    const t = TEXTO_VERIFICACION[clave];
    return (
        <span data-slot="verificacion" data-verificacion={clave} title={t.largo} className={cn("inline-flex min-w-0 items-center gap-1 text-[0.78125rem] font-medium", v.texto, className)}>
            <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", v.punto)} />
            <v.icono aria-hidden className="size-3.5 shrink-0" />
            <span className="truncate">{t.corto}</span>
            {detalle ? <span className="sr-only">. {t.largo}</span> : null}
        </span>
    );
}
