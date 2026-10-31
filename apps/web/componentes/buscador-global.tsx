"use client";

import { Boton, Cargando, PaletaBusqueda, type SeccionPaleta } from "@iris/ui";
import { RotateCw, Search } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import {
    filtrarPantallas,
    FUENTES,
    fuentesActivas,
    LIMITE_POR_FUENTE,
    pantallasVisibles,
    resultadoCliente,
    resultadoOrden,
    resultadoProspecto,
    resultadoTicket,
    type Fuente,
    type ResultadoBusqueda,
} from "@/lib/busqueda/global";
import { useDiferido } from "@/lib/diferido";
import { useSesion } from "@/lib/sesion";

type EstadoFuente = { estado: "cargando" } | { estado: "listo"; datos: ResultadoBusqueda[] } | { estado: "error"; error: unknown };

async function buscar(fuente: Fuente, q: string, signal: AbortSignal): Promise<ResultadoBusqueda[]> {
    const query = { q, limit: LIMITE_POR_FUENTE };
    switch (fuente) {
        case "tickets":
            return ((await resolver(api().GET("/api/v1/tickets", { params: { query }, signal }))).data ?? []).map(resultadoTicket);
        case "ordenes":
            return ((await resolver(api().GET("/api/v1/ordenes", { params: { query }, signal }))).data ?? []).map(resultadoOrden);
        case "prospectos":
            return ((await resolver(api().GET("/api/v1/prospectos", { params: { query }, signal }))).data ?? []).map(resultadoProspecto);
        case "clientes":
            return ((await resolver(api().GET("/api/v1/clientes", { params: { query }, signal }))).data ?? []).map(resultadoCliente);
    }
}

/** Argos apagado no es un error que se pueda reintentar: los clientes simplemente no están. */
const sinArgos = (e: unknown) => e instanceof ErrorApi && e.estado === 503 && e.codigo === "conector_apagado";

/**
 * Buscador global del panel: botón en la barra superior y Ctrl/⌘ + K desde cualquier pantalla.
 * Pantallas por permiso y tickets, órdenes, prospectos y clientes por número o texto.
 */
export function BuscadorGlobal() {
    const { permisos, yo } = useSesion();
    const empresa = yo.empresaActiva?.id ?? 0;
    const router = useRouter();
    const [abierta, setAbierta] = useState(false);
    const [texto, setTexto] = useState("");
    const q = useDiferido(texto.trim(), 300);
    /**
     * Respuestas por `empresa|fuente|texto|intento`: lo que no está aún es «buscando». La empresa
     * va en la clave porque el buscador vive fuera de lo que se remonta al cambiarla.
     */
    const [respuestas, setRespuestas] = useState<Readonly<Record<string, EstadoFuente>>>({});
    const [intento, setIntento] = useState(0);
    /** Copia para el efecto: no vuelve a pedir lo que ya respondió bien. */
    const respuestasRef = useRef(respuestas);
    useEffect(() => {
        respuestasRef.current = respuestas;
    });

    useEffect(() => {
        const alTeclear = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "k") {
                e.preventDefault();
                setAbierta(true);
            }
        };
        window.addEventListener("keydown", alTeclear);
        return () => window.removeEventListener("keydown", alTeclear);
    }, []);

    const fuentes = useMemo(() => (abierta ? fuentesActivas(permisos, q) : []), [abierta, permisos, q]);
    const clave = (f: Fuente) => `${empresa}|${f}|${q}|${intento}`;

    useEffect(() => {
        const control = new AbortController();
        for (const f of fuentes) {
            const k = `${empresa}|${f}|${q}|${intento}`;
            if (respuestasRef.current[k]?.estado === "listo") continue;
            buscar(f, q, control.signal).then(
                (datos) => !control.signal.aborted && setRespuestas((r) => ({ ...r, [k]: { estado: "listo", datos } })),
                (error: unknown) => !control.signal.aborted && setRespuestas((r) => ({ ...r, [k]: sinArgos(error) ? { estado: "listo", datos: [] } : { estado: "error", error } })),
            );
        }
        return () => control.abort();
    }, [empresa, fuentes, q, intento]);

    const resultados: Partial<Record<Fuente, EstadoFuente>> = Object.fromEntries(fuentes.map((f) => [f, respuestas[clave(f)] ?? { estado: "cargando" }]));

    const pantallas = useMemo(() => pantallasVisibles(permisos), [permisos]);
    const secciones: SeccionPaleta[] = [
        { id: "pantallas", titulo: "Pantallas", elementos: filtrarPantallas(pantallas, texto).slice(0, texto.trim() ? 6 : 12) },
        ...FUENTES.map((f) => {
            const r = resultados[f.id];
            return { id: f.id, titulo: f.titulo, elementos: r?.estado === "listo" ? r.datos : [] };
        }),
    ];
    const buscando = Object.values(resultados).some((r) => r?.estado === "cargando") || (texto.trim() !== q && texto.trim().length >= 2);
    const fallidas = FUENTES.filter((f) => resultados[f.id]?.estado === "error");
    const reintentar = useCallback(() => setIntento((n) => n + 1), []);

    const estado =
        buscando || fallidas.length > 0 ? (
            <>
                {buscando ? <Cargando texto="Buscando…" visible /> : null}
                {fallidas.map((f) => {
                    const r = resultados[f.id];
                    return (
                        <div key={f.id} role="alert" className="flex flex-wrap items-center justify-between gap-2 text-error">
                            <span>
                                No se pudo buscar en {f.titulo.toLowerCase()}
                                {r?.estado === "error" ? `: ${mensajeDeError(r.error).titulo}` : ""}
                            </span>
                            <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        </div>
                    );
                })}
            </>
        ) : null;

    function cambiarAbierta(valor: boolean) {
        setAbierta(valor);
        if (!valor) {
            setTexto("");
            // Al cerrar se olvida lo buscado: la próxima vez se lee de nuevo (y con la empresa de ese momento).
            setRespuestas({});
        }
    }

    return (
        <>
            {/* Una sola instancia: ícono en el teléfono, campo con atajo en escritorio. */}
            <Boton
                variante="fantasma"
                tamano="icono"
                className="lg:hidden"
                aria-label="Buscador global"
                title="Buscar (Ctrl + K)"
                aria-keyshortcuts="Control+K Meta+K"
                onClick={() => setAbierta(true)}
            >
                <Search />
            </Boton>
            <Boton
                variante="contorno"
                className="hidden w-56 justify-start gap-2 font-normal text-muted-foreground lg:inline-flex"
                aria-label="Buscador global"
                aria-keyshortcuts="Control+K Meta+K"
                onClick={() => setAbierta(true)}
            >
                <Search />
                <span className="flex-1 text-left">Buscar…</span>
                <kbd className="rounded-sm border px-1.5 text-xs text-dimmed">Ctrl K</kbd>
            </Boton>
            <PaletaBusqueda
                abierta={abierta}
                alCambiarAbierta={cambiarAbierta}
                texto={texto}
                alCambiarTexto={setTexto}
                secciones={secciones}
                estado={estado}
                sinResultados={texto.trim().length < 2 ? "Escribe al menos 2 letras para buscar tickets, órdenes y prospectos (3 para clientes)." : "Sin resultados"}
                alElegir={(e) => {
                    cambiarAbierta(false);
                    router.push(e.href);
                }}
            />
        </>
    );
}
