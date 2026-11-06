"use client";

import { Aviso, AvatarUsuario, Boton, cn, Esqueleto, GrupoSegmentado } from "@iris/ui";
import { Circle, Info, RotateCw } from "lucide-react";
import { mensajeDeError } from "@/lib/api/errores";
import type { EstadoDisponibilidad } from "@/lib/conversaciones/contrato-11";
import { textoExplicacion, textoMotivoPropio, useDisponibilidad } from "@/lib/conversaciones/disponibilidad";

/** Punto de disponibilidad: verde si está disponible, gris si no. Decorativo: el texto va al lado. */
export function PuntoDisponible({ disponible, className }: { disponible: boolean; className?: string }) {
    return <span aria-hidden data-disponible={disponible || undefined} className={cn("inline-block size-2 shrink-0 rounded-full", disponible ? "bg-success" : "bg-muted-foreground/60", className)} />;
}

/** Iniciales con el punto de disponibilidad abajo a la derecha. */
export function AvatarDisponible({ nombre, disponible, tamano = "sm", className }: { nombre: string; disponible: boolean | null; tamano?: "sm" | "md"; className?: string }) {
    return (
        <span className={cn("relative inline-flex shrink-0", className)}>
            <AvatarUsuario nombre={nombre} tamano={tamano} />
            {disponible === null ? null : <PuntoDisponible disponible={disponible} className="absolute -right-0.5 -bottom-0.5 size-2.5 ring-2 ring-card" />}
        </span>
    );
}

/** «● Disponible» / «● No disponible» en texto, con su color. */
export function EstadoDisponible({ disponible, className }: { disponible: boolean; className?: string }) {
    return (
        <span className={cn("inline-flex items-center gap-1.5 text-xs font-semibold whitespace-nowrap", disponible ? "text-success" : "text-muted-foreground", className)}>
            <PuntoDisponible disponible={disponible} />
            {disponible ? "Disponible" : "No disponible"}
        </span>
    );
}

const OPCIONES: { valor: EstadoDisponibilidad; etiqueta: string; icono: typeof Circle }[] = [
    { valor: "disponible", etiqueta: "Disponible", icono: Circle },
    { valor: "no_disponible", etiqueta: "No disponible", icono: Circle },
];

/**
 * Disponible / No disponible, con la línea que explica cuándo pasas a «No disponible». Va arriba
 * del menú de la cuenta (barra lateral) y de «Más» (teléfono).
 */
export function ControlDisponibilidad({ className }: { className?: string }) {
    const d = useDisponibilidad();
    if (!d) return null;
    const { lectura, cambiar, cambiando, error } = d;
    if (lectura.estado === "cargando") {
        return (
            <div className={cn("grid gap-2", className)} aria-busy="true">
                <span className="sr-only" role="status">
                    Leyendo tu disponibilidad…
                </span>
                <Esqueleto className="h-8" />
            </div>
        );
    }
    if (lectura.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudo leer tu disponibilidad"
                className={className}
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(lectura.error).titulo}
            </Aviso>
        );
    }
    const datos = lectura.datos;
    const motivo = textoMotivoPropio(datos);
    return (
        <div className={cn("grid gap-2", className)} data-slot="control-disponibilidad" data-estado={datos.estado}>
            <GrupoSegmentado
                etiqueta="Tu disponibilidad en Conversaciones"
                className="flex w-full flex-nowrap"
                claseOpcion="h-8 flex-1 min-w-0 text-[0.8125rem] [&_svg]:size-2 [&_svg]:fill-current first:[&_svg]:text-success last:[&_svg]:text-muted-foreground"
                valor={datos.estado}
                deshabilitado={cambiando}
                alCambiar={(v) => void cambiar(v)}
                opciones={OPCIONES}
            />
            {motivo ? <p className="text-xs font-medium text-warning">{motivo}</p> : null}
            <p className="flex gap-1.5 text-xs text-muted-foreground">
                <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                <span>{textoExplicacion(datos)}</span>
            </p>
            {error ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo cambiar"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={() => void cambiar(datos.estado === "disponible" ? "no_disponible" : "disponible")}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
        </div>
    );
}

/** Mi estado como texto corto bajo el nombre en la barra lateral («● Disponible»). */
export function MiEstadoCorto() {
    const d = useDisponibilidad();
    if (!d || d.lectura.estado !== "listo") return null;
    return <EstadoDisponible disponible={d.lectura.datos.estado === "disponible"} />;
}

/** ¿Estoy disponible? null sin bandeja o sin leer. */
export function useEstoyDisponible(): boolean | null {
    const d = useDisponibilidad();
    return d && d.lectura.estado === "listo" ? d.lectura.datos.estado === "disponible" : null;
}
