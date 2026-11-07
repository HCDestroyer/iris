"use client";

import { Aviso, Boton, cn, inicialesDe } from "@iris/ui";
import { textoContador, type ConversacionVista } from "@iris/sync/chat";
import { Lock, RotateCw, Users, WifiOff, Wrench } from "lucide-react";
import { insigniaDe } from "@/lib/chat/presentacion";
import type { EstadoEnVivo } from "./proveedor";

/**
 * Avatar de una conversación del equipo. Directa: círculo con iniciales. Cuadrilla y orden:
 * cuadrado con su icono (así no se confunden con un cliente de la bandeja).
 */
export function AvatarChat({ c, nombre, className }: { c: Pick<ConversacionVista, "tipo">; nombre: string; className?: string }) {
    if (c.tipo === "directa") {
        return (
            <span aria-hidden className={cn("grid size-10 shrink-0 place-content-center rounded-full bg-elevated text-[0.8125rem] font-semibold", className)}>
                {inicialesDe(nombre) || "?"}
            </span>
        );
    }
    const Icono = c.tipo === "orden" ? Wrench : Users;
    return (
        <span
            aria-hidden
            data-avatar-tipo={c.tipo}
            className={cn("grid size-10 shrink-0 place-content-center rounded-lg border border-border", c.tipo === "orden" ? "bg-elevated text-foreground" : "bg-primary/10 text-primary", className)}
        >
            <Icono className="size-[1.125rem]" />
        </span>
    );
}

/** Insignia de la fila: número (directas, menciones) o punto (lo demás nuevo); nada si está al día. */
export function InsigniaChat({ c }: { c: ConversacionVista }) {
    const { numero, punto } = insigniaDe(c);
    if (numero > 0) {
        return (
            <span data-slot="insignia-chat" className="min-w-[1.25rem] rounded-full bg-primary px-1.5 text-center text-[0.6875rem] leading-5 font-bold text-primary-foreground tabular-nums">
                {textoContador(numero)}
                <span className="sr-only">{c.tipo === "directa" ? " sin leer" : numero === 1 ? " mención" : " menciones"}</span>
            </span>
        );
    }
    if (punto) {
        return (
            <span data-slot="punto-chat" className="size-2 rounded-full bg-primary">
                <span className="sr-only">Mensajes nuevos</span>
            </span>
        );
    }
    return null;
}

/** «En vivo» discreto (un punto y dos palabras); crece solo si hay que saberlo. */
export function IndicadorEnVivo({ estado, className }: { estado: EstadoEnVivo; className?: string }) {
    if (estado === "sin_conexion" || estado === "reconectando" || estado === "sesion") {
        const texto = estado === "sesion" ? "Sesión cerrada" : estado === "sin_conexion" ? "Sin conexión" : "Reconectando…";
        return (
            <span role="status" className={cn("inline-flex items-center gap-1 text-xs font-medium whitespace-nowrap text-warning", className)}>
                <WifiOff aria-hidden className="size-3.5" />
                {texto}
            </span>
        );
    }
    const vivo = estado === "en_vivo";
    return (
        <span role="status" className={cn("inline-flex items-center gap-1.5 text-xs whitespace-nowrap text-muted-foreground", className)} data-en-vivo={vivo || undefined}>
            <span aria-hidden className={cn("size-2 rounded-full", vivo ? "bg-success" : "bg-border")} />
            {vivo ? "En vivo" : "Conectando…"}
        </span>
    );
}

/** Aviso con «Reintentar» cuando el tiempo real se cayó (o la sesión se cerró). */
export function AvisoTiempoReal({ estado, reintentar, className }: { estado: EstadoEnVivo; reintentar: () => void; className?: string }) {
    if (estado === "sesion") {
        return (
            <Aviso
                tono="warning"
                titulo="Tu sesión se cerró"
                className={className}
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={() => window.location.reload()}>
                        <RotateCw />
                        Recargar
                    </Boton>
                }
            >
                Vuelve a entrar para seguir en el chat. No se envió nada.
            </Aviso>
        );
    }
    if (estado !== "reconectando" && estado !== "sin_conexion") return null;
    return (
        <Aviso
            tono="warning"
            titulo="Sin conexión en vivo"
            className={className}
            accion={
                <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {estado === "sin_conexion" ? "Sin internet: lo que escribas sale al volver la conexión." : "Reconectando… Los mensajes nuevos llegan al volver."}
        </Aviso>
    );
}

/** Franja que distingue el chat interno de la bandeja de clientes. */
export function FranjaInterno({ texto = "Chat interno: el cliente no lo ve", estado }: { texto?: string; estado: EstadoEnVivo }) {
    return (
        <div data-slot="franja-interno" className="flex min-w-0 items-center justify-center gap-1.5 border-b border-border bg-background-secondary px-3 py-1.5 text-xs text-muted-foreground">
            <Lock aria-hidden className="size-3.5 shrink-0" />
            <span className="min-w-0 truncate">{texto}</span>
            <span aria-hidden>·</span>
            <IndicadorEnVivo estado={estado} />
        </div>
    );
}
