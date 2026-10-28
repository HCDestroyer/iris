"use client";

import type { Schema } from "@iris/contratos";
import { createContext, useContext } from "react";

export type SesionPortal = Schema<"SesionPortalActual">;

const Contexto = createContext<SesionPortal | null>(null);

export const ProveedorSesionPortal = Contexto.Provider;

/** Sesión del portal (cliente y vencimiento); solo dentro del marco con sesión. */
export function useSesionPortal(): SesionPortal {
    const s = useContext(Contexto);
    if (!s) {
        throw new Error("useSesionPortal fuera de ProveedorSesionPortal");
    }
    return s;
}
