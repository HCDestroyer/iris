"use client";

import { cn, GrupoOpciones, InsigniaEstado } from "@iris/ui";
import { Lock, Map as IconoMapa, MapPin, MessageCircle, Network, Router } from "lucide-react";
import { presentacionEstado } from "@/lib/datos/presentacion";
import { textoPrecio, type ServicioLinea } from "@/lib/datos/servicio";
import { useSesion } from "@/lib/sesion";

export type { ServicioLinea };

/*
 * «Línea de servicio» (contrato con Argos 0.8.0 §2.4; pedido del PM: «no solo el nombre»): el único
 * componente que identifica un servicio de Argos de un vistazo, en fichas, selectores y listas.
 *
 *  - Primera línea: el plan completo (envuelve, nunca se recorta) y el estado con icono, texto y color.
 *  - Segunda línea, discreta: precio mensual, zona, router, IP, dirección y código.
 *
 * El servidor OMITE el router y la IP sin `servicios.ver_red`, y la dirección sin
 * `servicios.ver_ubicacion`: aquí solo se muestra lo que llegó (sin IP no se dice «sin IP»: el
 * técnico solo ve la red de sus órdenes y no se distingue). Sin precio (Argos anterior a 1.18.0) no se muestra nada: nunca «Q0».
 */

type Props = {
    servicio: ServicioLinea;
    /** completa: dos líneas (fichas y selectores). compacta: una línea que envuelve (listas, chips). */
    variante?: "completa" | "compacta";
    /** Muestra el número del servicio (con varios servicios). */
    numerado?: boolean;
    /** Marca «De esta conversación» (la ficha de la conversación con varios servicios). */
    marcado?: boolean;
    textoMarca?: string;
    /** No repetir la dirección (la ficha completa la muestra aparte, con referencia y coordenadas). */
    sinDireccion?: boolean;
    className?: string;
};

export function LineaServicio({ servicio: s, variante = "completa", numerado = false, marcado = false, textoMarca = "De esta conversación", sinDireccion = false, className }: Props) {
    const estado = presentacionEstado(s.estado);
    const precio = textoPrecio(s);
    const plan = s.planNombre?.trim() || "Sin plan";
    if (variante === "compacta") {
        return (
            <span className={cn("inline-flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5", className)} data-testid="linea-servicio" data-variante="compacta">
                {numerado && s.numero ? <span className="text-xs font-semibold text-muted-foreground">{s.numero}.</span> : null}
                <span className="font-medium break-words">{plan}</span>
                {estado ? <InsigniaEstado tono={estado.tono} icono={estado.icono} texto={estado.texto} /> : null}
                {precio ? <span className="text-xs tabular-nums">{precio}</span> : null}
                {s.zonaNombre ? <span className="text-xs text-muted-foreground">{s.zonaNombre}</span> : null}
                {s.ip ? <span className="font-mono text-xs text-muted-foreground">{s.ip}</span> : null}
            </span>
        );
    }
    return (
        <div className={cn("grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-2.5 gap-y-0.5", className)} data-testid="linea-servicio" data-variante="completa" data-servicio={s.id}>
            {numerado && s.numero ? (
                <span aria-label={`Servicio ${s.numero}`} className="row-span-2 mt-px flex size-5.5 items-center justify-center rounded-full bg-elevated text-xs font-semibold">
                    {s.numero}
                </span>
            ) : (
                <span aria-hidden className="row-span-2" />
            )}
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-x-2.5 gap-y-1">
                <span className="text-sm font-semibold break-words" data-testid="linea-servicio-plan">
                    {plan}
                </span>
                {estado ? <InsigniaEstado tono={estado.tono} icono={estado.icono} texto={estado.texto} /> : null}
            </div>
            <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-0.5 text-[0.78125rem] text-muted-foreground" data-testid="linea-servicio-detalle">
                {precio ? <span className="text-foreground tabular-nums">{precio}</span> : null}
                {s.zonaNombre ? (
                    <span className="inline-flex items-center gap-1">
                        <IconoMapa aria-hidden className="size-3 shrink-0" />
                        <span className="sr-only">Zona </span>
                        {s.zonaNombre}
                    </span>
                ) : null}
                {s.routerNombre ? (
                    <span className="inline-flex items-center gap-1" data-testid="linea-servicio-router">
                        <Router aria-hidden className="size-3 shrink-0" />
                        <span className="sr-only">Router </span>
                        {s.routerNombre}
                    </span>
                ) : null}
                {s.ip ? (
                    <span className="inline-flex items-center gap-1 font-mono" data-testid="linea-servicio-ip">
                        <Network aria-hidden className="size-3 shrink-0" />
                        <span className="sr-only">IP </span>
                        {s.ip}
                    </span>
                ) : null}
                {s.direccion && !sinDireccion ? (
                    <span className="inline-flex min-w-0 items-center gap-1 break-words" data-testid="linea-servicio-direccion">
                        <MapPin aria-hidden className="size-3 shrink-0" />
                        <span className="sr-only">Dirección </span>
                        {s.direccion}
                    </span>
                ) : null}
                {s.codigo ? <span className="font-mono">{s.codigo}</span> : null}
            </div>
            {marcado ? (
                <span className="col-start-2 mt-0.5 inline-flex items-center gap-1 text-[0.71875rem] font-semibold text-primary" data-testid="linea-servicio-marca">
                    <MessageCircle aria-hidden className="size-3" />
                    {textoMarca}
                </span>
            ) : null}
        </div>
    );
}

/** Nota discreta cuando el rol no ve la red o la ubicación del servicio (el servidor las omite). */
export function NotaPermisosServicio({ className }: { className?: string }) {
    const { puede } = useSesion();
    const red = puede(["servicios.ver_red"]);
    const ubicacion = puede(["servicios.ver_ubicacion"]);
    if (red && ubicacion) return null;
    const texto = !red && !ubicacion ? "la dirección ni los datos de red" : !red ? "los datos de red (IP y router)" : "la dirección";
    return (
        <p className={cn("flex items-start gap-1.5 text-xs text-muted-foreground", className)} data-testid="nota-permisos-servicio">
            <Lock aria-hidden className="mt-0.5 size-3 shrink-0" />
            <span>Tu rol no ve {texto} del servicio.</span>
        </p>
    );
}

/** Varios servicios en una tarjeta, uno por línea, con la marca del elegido. */
export function ListaServicios({ servicios, marcadoId, textoMarca, className }: { servicios: readonly ServicioLinea[]; marcadoId?: number | null; textoMarca?: string; className?: string }) {
    const varios = servicios.length > 1;
    return (
        <ul className={cn("overflow-hidden rounded-xl border border-border bg-card", className)} data-testid="lista-servicios">
            {servicios.map((s) => {
                const marcado = varios && s.id === marcadoId;
                return (
                    <li key={s.id} className={cn("border-border px-3 py-2.5 [&+&]:border-t", marcado && "bg-primary/5 shadow-[inset_3px_0_0_var(--hx-primary)]")}>
                        <LineaServicio servicio={s} numerado={varios} marcado={marcado} {...(textoMarca ? { textoMarca } : {})} />
                    </li>
                );
            })}
        </ul>
    );
}

/** Elegir un servicio en un formulario: tarjetas con la línea completa (radios reales). */
export function SelectorServicio({
    servicios,
    valor,
    alCambiar,
    leyenda = "Servicio",
    error,
    ayuda,
    disabled,
}: {
    servicios: readonly ServicioLinea[];
    valor: number | undefined;
    alCambiar: (id: number) => void;
    leyenda?: string;
    error?: string | undefined;
    ayuda?: string;
    disabled?: boolean;
}) {
    const varios = servicios.length > 1;
    return (
        <GrupoOpciones<number>
            leyenda={leyenda}
            nombre="servicioId"
            apiladas
            valor={valor}
            alCambiar={alCambiar}
            error={error}
            {...(ayuda ? { ayuda } : {})}
            {...(disabled ? { disabled } : {})}
            opciones={servicios.map((s) => ({
                valor: s.id,
                etiqueta: [s.numero && varios ? `Servicio ${s.numero}` : null, s.planNombre || "Sin plan", s.zonaNombre || null, presentacionEstado(s.estado)?.texto ?? null]
                    .filter(Boolean)
                    .join(" · "),
                contenido: <LineaServicio servicio={s} numerado={varios} />,
            }))}
        />
    );
}
