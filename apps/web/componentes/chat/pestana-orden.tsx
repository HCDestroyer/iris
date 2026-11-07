"use client";

import { Aviso, avisar, Boton, Cargando, EstadoVacio, Tarjeta } from "@iris/ui";
import { MessageCircle, RotateCw } from "lucide-react";
import { useEffect, useState } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import { hiloDeOrden } from "@/lib/chat/api";
import { ConversacionEquipo } from "./conversacion";
import { useChatEquipo } from "./proveedor";

/** No leídos del hilo de la orden (para el contador de la pestaña), si ya se conoce. */
export function useNoLeidosOrden(ordenId: number): number {
    const chat = useChatEquipo();
    const c = chat?.estado.conversaciones.find((x) => x.tipo === "orden" && x.orden_id === ordenId);
    return c ? Math.max(0, c.no_leidos) : 0;
}

type Busqueda = { estado: "buscando" } | { estado: "no_existe" } | { estado: "error"; error: unknown };

/**
 * Pestaña «Chat del equipo» de la orden: el mismo hilo y compositor que el panel lateral y la página
 * Equipo. Lee el hilo sin crearlo ni unirse (`GET /ordenes/{id}/conversacion`); si no existe o no
 * estás, «Abrir el chat de la orden» lo crea (y te une si despachas).
 */
export function ChatDeOrden({ ordenId, numero }: { ordenId: number; numero: string }) {
    const chat = useChatEquipo();
    const conocida = chat?.estado.conversaciones.find((x) => x.tipo === "orden" && x.orden_id === ordenId) ?? null;
    const [busqueda, setBusqueda] = useState<Busqueda>({ estado: "buscando" });
    const [intento, setIntento] = useState(0);
    const [abriendo, setAbriendo] = useState(false);
    const [errorAbrir, setErrorAbrir] = useState<unknown>(null);
    const motor = chat?.motor;

    useEffect(() => {
        if (!motor || conocida) return undefined;
        const c = new AbortController();
        setBusqueda({ estado: "buscando" });
        hiloDeOrden(ordenId, c.signal).then(
            async (id) => {
                if (id === null) setBusqueda({ estado: "no_existe" });
                else await motor.cargarConversacion(id);
            },
            (error: unknown) => {
                if (!c.signal.aborted) setBusqueda({ estado: "error", error });
            },
        );
        return () => c.abort();
    }, [motor, ordenId, conocida, intento]);

    if (!chat) return null;
    if (conocida) return <ConversacionEquipo id={conocida.id} variante="orden" />;

    if (busqueda.estado === "buscando") return <Cargando texto="Buscando el chat de la orden…" />;
    if (busqueda.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudo leer el chat de la orden"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={() => setIntento((i) => i + 1)}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(busqueda.error).titulo}
            </Aviso>
        );
    }
    const abrir = async () => {
        setAbriendo(true);
        setErrorAbrir(null);
        try {
            const c = await chat.motor.abrirOrden(ordenId);
            await chat.motor.cargarConversacion(c.id);
            avisar({ titulo: `Chat de ${numero} abierto` });
        } catch (e) {
            setErrorAbrir(e);
        } finally {
            setAbriendo(false);
        }
    };
    return (
        <Tarjeta>
            <EstadoVacio
                icono={MessageCircle}
                titulo="Esta orden aún no tiene chat del equipo"
                descripcion={chat.puedeEnviar ? "Ábrelo para hablar con el técnico asignado y despacho. El cliente no lo ve." : "Cuando el técnico o despacho escriban, lo verás aquí."}
                accion={
                    chat.puedeEnviar ? (
                        <Boton onClick={() => void abrir()} disabled={abriendo}>
                            <MessageCircle />
                            {abriendo ? "Abriendo…" : "Abrir el chat de la orden"}
                        </Boton>
                    ) : undefined
                }
            />
            {errorAbrir ? (
                <Aviso tono="error" titulo="No se pudo abrir el chat" className="m-4 mt-0">
                    {mensajeDeError(errorAbrir).titulo}
                </Aviso>
            ) : null}
        </Tarjeta>
    );
}
