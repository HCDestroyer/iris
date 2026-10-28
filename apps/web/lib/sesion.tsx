"use client";

import type { Schema } from "@iris/contratos";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, type ReactNode } from "react";
import { api, resolver } from "./api/cliente";
import { ErrorApi } from "./api/errores";
import { useLectura, type Lectura } from "./lectura";
import { tieneAlguno, type Permiso } from "./menu";

export type Yo = Schema<"Yo">;
export type EmpresaResumen = Schema<"EmpresaResumen">;

type ContextoSesion = {
    yo: Yo;
    permisos: readonly Permiso[];
    puede: (requeridos: readonly Permiso[] | null) => boolean;
    /** Empresas del selector (las de `GET /yo`). */
    empresas: readonly EmpresaResumen[];
    /** Cambia la empresa activa: la guarda, relee `/yo` y recarga los datos de la pantalla. */
    elegirEmpresa: (id: number) => void;
    /** Abre la elección de empresa (p. ej. tras un 422 `empresa_requerida`). */
    pedirEmpresa: () => void;
    /** Relee `GET /yo` sin pasar por «cargando». */
    refrescarSesion: () => void;
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
export function useYo(): Lectura<Yo> {
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

type PropsProveedor = {
    yo: Yo;
    elegirEmpresa?: (id: number) => void;
    pedirEmpresa?: () => void;
    refrescarSesion?: () => void;
    children: ReactNode;
};

const sinAccion = () => undefined;

export function ProveedorSesion({ yo, elegirEmpresa = sinAccion, pedirEmpresa = sinAccion, refrescarSesion = sinAccion, children }: PropsProveedor) {
    const valor = useMemo<ContextoSesion>(() => {
        const permisos = yo.permisos ?? [];
        return {
            yo,
            permisos,
            puede: (requeridos) => tieneAlguno(permisos, requeridos),
            empresas: yo.empresas ?? [],
            elegirEmpresa,
            pedirEmpresa,
            refrescarSesion,
        };
    }, [yo, elegirEmpresa, pedirEmpresa, refrescarSesion]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}

/** Sesión si hay (fuera del panel autenticado, `null`). */
export function useSesionOpcional(): ContextoSesion | null {
    return useContext(Contexto);
}

export function useSesion(): ContextoSesion {
    const ctx = useContext(Contexto);
    if (!ctx) {
        throw new Error("useSesion fuera de ProveedorSesion");
    }
    return ctx;
}
