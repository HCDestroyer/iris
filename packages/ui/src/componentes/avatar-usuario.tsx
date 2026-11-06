"use client";

import { Avatar } from "@heroui/react";
import { cn } from "../utilidades";

/** Iniciales de un nombre («Ana López» → «AL», «Iris» → «I»). */
export function inicialesDe(nombre: string): string {
    const partes = nombre.trim().split(/\s+/).filter(Boolean);
    const letras = partes.length >= 2 ? [partes[0], partes[partes.length - 1]] : partes;
    return letras.map((p) => p?.[0]?.toUpperCase() ?? "").join("") || "?";
}

export type PropsAvatarUsuario = {
    nombre: string;
    tamano?: "sm" | "md";
    className?: string;
};

/**
 * Círculo con las iniciales de una persona (HeroUI `Avatar` + `Avatar.Fallback`). Es decorativo:
 * el nombre siempre va escrito al lado o en la etiqueta del botón que lo contiene.
 */
export function AvatarUsuario({ nombre, tamano = "sm", className }: PropsAvatarUsuario) {
    return (
        <Avatar aria-hidden size={tamano} className={cn("shrink-0 rounded-full", tamano === "sm" ? "size-8" : "size-10", className)} data-slot="avatar-usuario">
            <Avatar.Fallback className="bg-elevated text-[0.6875rem] font-semibold text-foreground">{inicialesDe(nombre)}</Avatar.Fallback>
        </Avatar>
    );
}
