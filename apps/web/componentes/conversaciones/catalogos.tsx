"use client";

import type { Schema } from "@iris/contratos";
import { createContext, use, useEffect, useMemo, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { leerProvisional } from "@/lib/api/provisional";
import type { CategoriaRef } from "@/lib/asistente/fase3";
import { leerAgentes, leerColas } from "@/lib/conversaciones/api-equipo";
import type { Agente, Cola } from "@/lib/conversaciones/tipos";
import type { Etiqueta, RespuestaRapida } from "@/lib/conversaciones/tipos";
import { useLectura, type Lectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/*
 * Catálogos de la bandeja (contratos/conversaciones.md §7.2.1), leídos una vez por pantalla y
 * compartidos por la lista, los filtros, la conversación y los diálogos: números, agentes, colas,
 * etiquetas activas y respuestas rápidas (solo con `conversaciones.responder`).
 */
export type CanalBandeja = Schema<"NumeroConversaciones">;
/** Agente con su disponibilidad y sus conversaciones abiertas (fase 1.1). */
export type { Agente };

type Catalogos = {
    canales: Lectura<CanalBandeja[]>;
    agentes: Lectura<Agente[]>;
    colas: Lectura<Cola[]>;
    etiquetas: Lectura<Etiqueta[]>;
    respuestas: Lectura<RespuestaRapida[] | null>;
    /** Fase 3 del asistente (§17): categorías activas. Vacío si el servidor aún no las ofrece. */
    categorias: Lectura<CategoriaRef[]>;
};

const Contexto = createContext<Catalogos | null>(null);

const M = "/api/v1/mensajeria";

export function ProveedorCatalogos({ children }: { children: ReactNode }) {
    const { puede } = useSesion();
    const responde = puede(["conversaciones.responder"]);
    const canales = useLectura(async (signal) => (await resolver(api().GET(`${M}/conversaciones/canales`, { signal }))).data ?? []);
    const agentes = useLectura((signal) => leerAgentes(signal));
    const colas = useLectura((signal) => leerColas(signal));
    // La disponibilidad de los demás cambia sola: se relee cada minuto con la pestaña a la vista.
    const refrescarAgentes = agentes.refrescar;
    useEffect(() => {
        const id = window.setInterval(() => document.visibilityState === "visible" && void refrescarAgentes(), 60_000);
        return () => window.clearInterval(id);
    }, [refrescarAgentes]);
    const etiquetas = useLectura(async (signal) => ((await resolver(api().GET(`${M}/etiquetas`, { signal }))).data ?? []).filter((e) => e.activa));
    const respuestas = useLectura<RespuestaRapida[] | null>(async (signal) =>
        responde ? ((await resolver(api().GET(`${M}/respuestas-rapidas`, { signal }))).data ?? []).filter((r) => r.activa) : null,
    );
    // Provisional (§17): catálogo para quien ve la bandeja. Si el servidor no lo tiene, sin categorías.
    const categorias = useLectura<CategoriaRef[]>(async (signal) => {
        try {
            return (await leerProvisional<{ data: CategoriaRef[] | null }>(`${M}/conversaciones/categorias`, { signal })).data ?? [];
        } catch (e) {
            if (e instanceof ErrorApi && (e.estado === 404 || e.estado === 403)) return [];
            throw e;
        }
    });
    const valor = useMemo(() => ({ canales, agentes, colas, etiquetas, respuestas, categorias }), [canales, agentes, colas, etiquetas, respuestas, categorias]);
    return <Contexto value={valor}>{children}</Contexto>;
}

export function useCatalogos(): Catalogos {
    const c = use(Contexto);
    if (!c) throw new Error("useCatalogos va dentro de <ProveedorCatalogos>.");
    return c;
}

/** Agentes de una cola (sus miembros y quienes supervisan); sin cola, todos. */
export function agentesDeCola(agentes: readonly Agente[], colaId: number | null | undefined): Agente[] {
    const lista = colaId ? agentes.filter((a) => a.supervisa || (a.colas ?? []).some((c) => c.id === colaId)) : [...agentes];
    return lista.sort((a, b) => a.nombre.localeCompare(b.nombre, "es"));
}
