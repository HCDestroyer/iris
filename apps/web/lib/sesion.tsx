"use client";

import type { Schema } from "@iris/contratos";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, type ReactNode } from "react";
import { api, resolver } from "./api/cliente";
import { ErrorApi } from "./api/errores";
import { useLectura, type EstadoLectura } from "./lectura";
import { tieneAlguno, type Permiso } from "./menu";

export type Yo = Schema<"Yo">;

type ContextoSesion = {
    yo: Yo;
    permisos: readonly Permiso[];
    puede: (requeridos: readonly Permiso[] | null) => boolean;
};

const Contexto = createContext<ContextoSesion | null>(null);

/** Ruta de inicio de sesión que vuelve a `ruta` al entrar. */
export function rutaEntrar(ruta: string, motivo?: "expirada"): string {
    const params = new URLSearchParams();
    if (ruta && ruta !== "/") {
        params.set("siguiente", ruta);
    }
    if (motivo) {
        params.set("motivo", motivo);
    }
    const q = params.toString();
    return q ? `/entrar?${q}` : "/entrar";
}

/** Lee `GET /yo` una vez para el layout autenticado. Un 401 manda al inicio de sesión. */
export function useYo(): EstadoLectura<Yo> & { reintentar: () => void } {
    const router = useRouter();
    const ruta = usePathname();
    const lectura = useLectura((signal) => resolver(api().GET("/api/v1/yo", { signal })));
    const noAutenticado = lectura.estado === "error" && lectura.error instanceof ErrorApi && lectura.error.estado === 401;

    useEffect(() => {
        if (noAutenticado) {
            router.replace(rutaEntrar(ruta, "expirada"));
        }
    }, [noAutenticado, router, ruta]);

    return lectura;
}

export function ProveedorSesion({ yo, children }: { yo: Yo; children: ReactNode }) {
    const valor = useMemo<ContextoSesion>(() => {
        const permisos = yo.permisos ?? [];
        return { yo, permisos, puede: (requeridos) => tieneAlguno(permisos, requeridos) };
    }, [yo]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}

export function useSesion(): ContextoSesion {
    const ctx = useContext(Contexto);
    if (!ctx) {
        throw new Error("useSesion fuera de ProveedorSesion");
    }
    return ctx;
}
