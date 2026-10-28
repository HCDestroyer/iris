"use client";

import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

export type Categoria = Schema<"Categoria">;
export type Usuario = Schema<"Usuario">;

/** Categorías de ticket (activas). */
export function useCategorias() {
    return useLectura(async (signal) => {
        const pagina = await resolver(api().GET("/api/v1/categorias", { signal }));
        return pagina.data ?? [];
    });
}

/**
 * Usuarios activos para nombres y para asignar (`GET /usuarios`, acotado por empresa en la API).
 * Sin `usuarios.ver` no se pide: los asignados se muestran por número. Falta un endpoint de
 * «asignables» (pedido en .claude/integration/i3.md); la API valida al asignar.
 */
export function useUsuarios() {
    const { puede } = useSesion();
    const permitido = puede(["usuarios.ver"]);
    return useLectura<Usuario[] | null>(async (signal) => {
        if (!permitido) {
            return null;
        }
        const pagina = await resolver(api().GET("/api/v1/usuarios", { params: { query: { limit: 200, activo: "true" } }, signal }));
        return pagina.data ?? [];
    });
}

export function nombreCategoria(categorias: readonly Categoria[] | undefined, id: number | null): string | undefined {
    if (id === null) {
        return undefined;
    }
    return categorias?.find((c) => c.id === id)?.nombre ?? `Categoría ${id}`;
}

export function nombreUsuario(
    usuarios: readonly Usuario[] | null | undefined,
    id: number | null,
    yo: { id: number; nombre: string },
): string | undefined {
    if (id === null) {
        return undefined;
    }
    if (id === yo.id) {
        return `${yo.nombre} (tú)`;
    }
    return usuarios?.find((u) => u.id === id)?.nombre ?? `Usuario ${id}`;
}
