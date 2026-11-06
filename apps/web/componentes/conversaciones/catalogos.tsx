"use client";

import type { Schema } from "@iris/contratos";
import { createContext, use, useMemo, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import type { Cola, Etiqueta, RespuestaRapida } from "@/lib/conversaciones/tipos";
import { useLectura, type Lectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/*
 * Catálogos de la bandeja (contratos/conversaciones.md §7.2.1), leídos una vez por pantalla y
 * compartidos por la lista, los filtros, la conversación y los diálogos: números, agentes, colas,
 * etiquetas activas y respuestas rápidas (solo con `conversaciones.responder`).
 */
export type CanalBandeja = Schema<"NumeroConversaciones">;
export type Agente = Schema<"AgenteConversaciones">;

type Catalogos = {
    canales: Lectura<CanalBandeja[]>;
    agentes: Lectura<Agente[]>;
    colas: Lectura<Cola[]>;
    etiquetas: Lectura<Etiqueta[]>;
    respuestas: Lectura<RespuestaRapida[] | null>;
};

const Contexto = createContext<Catalogos | null>(null);

const M = "/api/v1/mensajeria";

export function ProveedorCatalogos({ children }: { children: ReactNode }) {
    const { puede } = useSesion();
    const responde = puede(["conversaciones.responder"]);
    const canales = useLectura(async (signal) => (await resolver(api().GET(`${M}/conversaciones/canales`, { signal }))).data ?? []);
    const agentes = useLectura(async (signal) => (await resolver(api().GET(`${M}/conversaciones/agentes`, { signal }))).data ?? []);
    const colas = useLectura(async (signal) => (await resolver(api().GET(`${M}/colas`, { signal }))).data ?? []);
    const etiquetas = useLectura(async (signal) => ((await resolver(api().GET(`${M}/etiquetas`, { signal }))).data ?? []).filter((e) => e.activa));
    const respuestas = useLectura<RespuestaRapida[] | null>(async (signal) =>
        responde ? ((await resolver(api().GET(`${M}/respuestas-rapidas`, { signal }))).data ?? []).filter((r) => r.activa) : null,
    );
    const valor = useMemo(() => ({ canales, agentes, colas, etiquetas, respuestas }), [canales, agentes, colas, etiquetas, respuestas]);
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
