"use client";

import { Boton, cn, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento, PanelEmergente } from "@iris/ui";
import { Bell, ClipboardPlus, Handshake, LifeBuoy, Plus, RotateCw, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import type { Permiso } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";
import { useResumenTickets } from "@/lib/tickets/resumen";

type Creable = { href: string; etiqueta: string; icono: LucideIcon; permiso: Permiso };

/** Lo que se puede crear desde la barra superior: cada uno con su pantalla propia (§10). */
export const CREABLES: readonly Creable[] = [
    { href: "/tickets/nuevo", etiqueta: "Nuevo ticket", icono: LifeBuoy, permiso: "tickets.crear" },
    { href: "/ordenes/nueva", etiqueta: "Nueva orden", icono: ClipboardPlus, permiso: "ordenes.crear" },
    { href: "/comercial/nuevo", etiqueta: "Nuevo prospecto", icono: Handshake, permiso: "prospectos.crear" },
];

/** «Crear»: un menú con lo que la persona puede crear; sin permiso para nada, no aparece. */
export function BotonCrear({ className }: { className?: string }) {
    const { puede } = useSesion();
    const router = useRouter();
    const opciones = CREABLES.filter((c) => puede([c.permiso]));
    if (opciones.length === 0) return null;
    return (
        <MenuDesplegable>
            <MenuDesplegableDisparador>
                <Boton tamano="sm" className={cn("h-9 rounded-lg px-3.5 text-[0.8125rem] font-semibold", className)} aria-label="Crear">
                    <Plus />
                    <span className="hidden sm:inline">Crear</span>
                </Boton>
            </MenuDesplegableDisparador>
            <MenuDesplegableContenido align="end">
                {opciones.map((c) => (
                    <MenuDesplegableElemento key={c.href} icono={c.icono} onSelect={() => router.push(c.href)}>
                        {c.etiqueta}
                    </MenuDesplegableElemento>
                ))}
            </MenuDesplegableContenido>
        </MenuDesplegable>
    );
}

type Pendiente = { id: string; texto: string; href: string; tono: "error" | "warning" | "info" };

/**
 * Avisos de la barra superior: lo que pide actuar según el resumen de la bandeja (SLA vencido, en
 * riesgo, sin asignar). Es un punto discreto (§10): solo aparece si hay algo vencido o en riesgo.
 * Sin `tickets.ver` no hay avisos que mostrar y el botón no aparece.
 */
export function BotonAvisos() {
    const resumen = useResumenTickets();
    const [abierto, setAbierto] = useState(false);
    if (resumen === null) return null;
    const datos = resumen.estado === "listo" ? resumen.datos : null;
    const pendientes: Pendiente[] = datos
        ? [
              datos.slaVencidos > 0
                  ? {
                        id: "vencidos",
                        texto: datos.slaVencidos === 1 ? "1 ticket con el SLA vencido" : `${datos.slaVencidos} tickets con el SLA vencido`,
                        href: "/tickets",
                        tono: "error" as const,
                    }
                  : null,
              datos.slaEnRiesgo > 0
                  ? {
                        id: "riesgo",
                        texto: datos.slaEnRiesgo === 1 ? "1 ticket con el SLA en riesgo" : `${datos.slaEnRiesgo} tickets con el SLA en riesgo`,
                        href: "/tickets",
                        tono: "warning" as const,
                    }
                  : null,
              datos.sinAsignar > 0
                  ? {
                        id: "sin-asignar",
                        texto: datos.sinAsignar === 1 ? "1 ticket sin asignar" : `${datos.sinAsignar} tickets sin asignar`,
                        href: "/tickets?vista=sin_asignar",
                        tono: "info" as const,
                    }
                  : null,
          ].filter((p): p is Pendiente => p !== null)
        : [];
    const urgentes = datos ? datos.slaVencidos + datos.slaEnRiesgo : 0;
    const nombre = urgentes > 0 ? `Avisos: ${urgentes === 1 ? "1 ticket necesita atención" : `${urgentes} tickets necesitan atención`}` : "Avisos";
    return (
        <PanelEmergente
            titulo="Avisos"
            tituloVisible
            lado="bottom end"
            abierto={abierto}
            alCambiar={setAbierto}
            className="w-80"
            disparador={
                <Boton variante="secundario" tamano="icono-sm" className="relative size-9 rounded-lg" aria-label={nombre} title="Avisos">
                    <Bell />
                    {urgentes > 0 ? (
                        <span aria-hidden data-testid="punto-avisos" className="absolute top-2 right-2 size-[0.4375rem] rounded-full bg-error" />
                    ) : null}
                </Boton>
            }
        >
            {resumen.estado === "error" ? (
                <div className="grid gap-2 text-sm">
                    <p className="text-muted-foreground">No se pudieron leer los avisos.</p>
                    <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={resumen.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                </div>
            ) : resumen.estado === "cargando" ? (
                <p className="text-sm text-muted-foreground">Cargando…</p>
            ) : pendientes.length === 0 ? (
                <p className="text-sm text-muted-foreground">Todo al día: nada vencido, en riesgo ni sin asignar.</p>
            ) : (
                <ul className="grid gap-1" aria-label="Pendientes">
                    {pendientes.map((p) => (
                        <li key={p.id}>
                            <Link
                                href={p.href}
                                onClick={() => setAbierto(false)}
                                className="flex items-center gap-2.5 rounded-md px-2 py-1.5 text-[0.8125rem] hover:bg-elevated"
                            >
                                <span
                                    aria-hidden
                                    className={cn(
                                        "size-2 shrink-0 rounded-full",
                                        p.tono === "error" ? "bg-error" : p.tono === "warning" ? "bg-warning" : "bg-info",
                                    )}
                                />
                                {p.texto}
                            </Link>
                        </li>
                    ))}
                </ul>
            )}
        </PanelEmergente>
    );
}
