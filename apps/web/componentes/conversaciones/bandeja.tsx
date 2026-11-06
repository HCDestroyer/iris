"use client";

import { avisar, cn } from "@iris/ui";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { abrirFlujo, leerCambios, leerContadores, leerEventoFlujo, leerLista } from "@/lib/conversaciones/api";
import { aplicarConversacion, filtrosDeUrl, unirConversaciones, urlDeFiltros, type Filtros } from "@/lib/conversaciones/estado";
import { esSesionCambiada, verificarSesion, type Identidad } from "@/lib/conversaciones/sesion";
import { iniciarFeed, type EstadoFeed } from "@/lib/conversaciones/tiempo-real";
import type { Contadores, Conversacion, EventoBandeja } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { ProveedorCatalogos } from "./catalogos";
import { PanelConversacion } from "./conversacion";
import { ListaConversaciones } from "./lista";

/** Lo que el feed avisa a la conversación abierta: un evento, o «recargar» (cursor vencido). */
export type SenalFeed = { tipo: "evento"; evento: EventoBandeja } | { tipo: "recargar" };
export type OyenteFeed = (senal: SenalFeed) => void;

type EstadoLista = { estado: "cargando" } | { estado: "error"; error: unknown } | { estado: "listo"; items: Conversacion[]; cursor: string | null };

/**
 * «Conversaciones» (fase 1, sin IA): lista, conversación y ficha del cliente en tres columnas.
 * La conversación abierta y los filtros van en la URL (`?c=41&vista=mias…`): un enlace o recargar
 * abren lo mismo. Con menos ancho la ficha pasa a una hoja y, en el teléfono, se ve la lista o la
 * conversación (con «Atrás»).
 */
export function Bandeja() {
    const { yo } = useSesion();
    const identidad = useMemo<Identidad>(() => ({ usuarioId: yo.usuario.id, empresaId: yo.empresaActiva?.id }), [yo.usuario.id, yo.empresaActiva?.id]);
    const params = useSearchParams();
    const router = useRouter();
    const ruta = usePathname();
    const filtros = useMemo(() => filtrosDeUrl(new URLSearchParams(params.toString())), [params]);
    const abierta = useMemo(() => {
        const n = Number(params.get("c"));
        return Number.isSafeInteger(n) && n > 0 ? n : null;
    }, [params]);

    const navegar = useCallback(
        (f: Filtros, c: number | null, reemplazar = false) => {
            const destino = `${ruta}${urlDeFiltros(f, c)}`;
            if (reemplazar) router.replace(destino, { scroll: false });
            else router.push(destino, { scroll: false });
        },
        [router, ruta],
    );

    // Lista: primera página por filtros; el feed la mantiene al día.
    const [lista, setLista] = useState<EstadoLista>({ estado: "cargando" });
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const [intento, setIntento] = useState(0);
    const filtrosRef = useRef(filtros);
    filtrosRef.current = filtros;
    const claveFiltros = urlDeFiltros(filtros, null);

    useEffect(() => {
        const control = new AbortController();
        setLista({ estado: "cargando" });
        setErrorMas(null);
        leerLista(filtrosRef.current, undefined, control.signal).then(
            (p) => !control.signal.aborted && setLista({ estado: "listo", items: unirConversaciones(p.data ?? []), cursor: p.nextCursor ?? null }),
            (error: unknown) => !control.signal.aborted && setLista({ estado: "error", error }),
        );
        return () => control.abort();
    }, [claveFiltros, intento]);

    /** Relee la primera página sin pasar por «cargando» (tras volver la conexión o recargar el feed). */
    const refrescarLista = useCallback(async () => {
        try {
            const p = await leerLista(filtrosRef.current, undefined);
            setLista((l) => (l.estado === "listo" ? { ...l, items: unirConversaciones(p.data ?? [], l.items) } : { estado: "listo", items: unirConversaciones(p.data ?? []), cursor: p.nextCursor ?? null }));
        } catch {
            // Lo leído sigue a la vista; el indicador del feed dice si no hay conexión.
        }
    }, []);

    async function cargarMas() {
        if (lista.estado !== "listo" || !lista.cursor) return;
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const p = await leerLista(filtros, lista.cursor);
            setLista((l) => (l.estado === "listo" ? { estado: "listo", items: unirConversaciones(l.items, p.data ?? []), cursor: p.nextCursor ?? null } : l));
        } catch (e) {
            setErrorMas(e);
        } finally {
            setCargandoMas(false);
        }
    }

    const contadores = useLectura<Contadores>((signal) => leerContadores(signal));
    const refrescarContadores = contadores.refrescar;
    // Los contadores se releen como mucho cada 3 s mientras llegan eventos.
    const contadoresPendiente = useRef<ReturnType<typeof setTimeout> | null>(null);
    const pedirContadores = useCallback(() => {
        if (contadoresPendiente.current) return;
        contadoresPendiente.current = setTimeout(() => {
            contadoresPendiente.current = null;
            void refrescarContadores();
        }, 3000);
    }, [refrescarContadores]);
    useEffect(() => () => void (contadoresPendiente.current && clearTimeout(contadoresPendiente.current)), []);

    /** Una conversación que devolvió una acción o llegó por el feed. */
    const aplicar = useCallback(
        (c: Conversacion) => {
            setLista((l) => (l.estado === "listo" ? { ...l, items: aplicarConversacion(l.items, c, filtrosRef.current, identidad.usuarioId) } : l));
            pedirContadores();
        },
        [identidad.usuarioId, pedirContadores],
    );

    // Tiempo real.
    const oyentes = useRef(new Set<OyenteFeed>());
    const suscribir = useCallback((o: OyenteFeed) => {
        oyentes.current.add(o);
        return () => void oyentes.current.delete(o);
    }, []);
    const [estadoFeed, setEstadoFeed] = useState<EstadoFeed>("conectando");
    const [detenido, setDetenido] = useState<"sin_sesion" | "sin_permiso" | "sesion_cambiada" | null>(null);
    const aplicarRef = useRef(aplicar);
    aplicarRef.current = aplicar;

    useEffect(() => {
        let primera = true;
        const cerrar = iniciarFeed<EventoBandeja>(
            {
                abrirFlujo,
                leerCambios,
                verificarSesion: () => verificarSesion(identidad),
                esSesionAjena: esSesionCambiada,
                leerEvento: leerEventoFlujo,
                alEvento: (evento) => {
                    if (evento.conversacion) aplicarRef.current(evento.conversacion);
                    if (evento.tipo === "mencion" || evento.tipo === "guardia") {
                        const titulo = evento.conversacion?.titulo ?? "una conversación";
                        avisar({ titulo: evento.tipo === "mencion" ? `Te mencionaron en ${titulo}` : `Guardia: te avisaron por ${titulo}` });
                    }
                    for (const o of oyentes.current) o({ tipo: "evento", evento });
                },
                alEstado: setEstadoFeed,
                alCursorVencido: () => {
                    void refrescarLista();
                    for (const o of oyentes.current) o({ tipo: "recargar" });
                },
                alDetenido: setDetenido,
            },
            () => {
                // Con la cabeza leída: lo que pasó entre la primera lectura y el flujo se recupera.
                if (!primera) return;
                primera = false;
                void refrescarLista();
            },
        );
        return cerrar;
    }, [identidad, refrescarLista]);

    const listaVisible = abierta === null;
    return (
        <ProveedorCatalogos>
        <div data-slot="bandeja" data-vista={listaVisible ? "lista" : "conversacion"} className="@container min-h-0 overflow-hidden bg-background-secondary">
            <div
                className={cn(
                    "grid h-full min-h-0",
                    "@min-[700px]:grid-cols-[minmax(16.5rem,20rem)_minmax(0,1fr)]",
                    "@min-[1100px]:grid-cols-[minmax(18rem,21rem)_minmax(0,1fr)_auto]",
                )}
            >
                <ListaConversaciones
                    className={cn(listaVisible ? "flex" : "hidden @min-[700px]:flex")}
                    filtros={filtros}
                    abierta={abierta}
                    lista={lista}
                    contadores={contadores}
                    cargarMas={cargarMas}
                    cargandoMas={cargandoMas}
                    errorMas={errorMas}
                    reintentar={() => setIntento((n) => n + 1)}
                    alFiltrar={(f) => navegar(f, abierta, true)}
                    hrefDe={(id) => `${ruta}${urlDeFiltros(filtros, id)}`}
                    estadoFeed={estadoFeed}
                    detenido={detenido}
                />
                {abierta === null ? (
                    <VacioConversacion />
                ) : (
                    <PanelConversacion
                        key={abierta}
                        id={abierta}
                        identidad={identidad}
                        suscribir={suscribir}
                        alActualizar={aplicar}
                        hrefLista={`${ruta}${urlDeFiltros(filtros, null)}`}
                        detenido={detenido}
                    />
                )}
            </div>
        </div>
        </ProveedorCatalogos>
    );
}

function VacioConversacion() {
    return (
        <section aria-label="Conversación" className="hidden min-h-0 items-center justify-center p-6 text-center @min-[700px]:flex">
            <div className="grid max-w-xs gap-1">
                <p className="font-medium">Elige una conversación</p>
                <p className="text-sm text-muted-foreground">Las que llegan por los números de WhatsApp de tu empresa aparecen a la izquierda.</p>
            </div>
        </section>
    );
}
