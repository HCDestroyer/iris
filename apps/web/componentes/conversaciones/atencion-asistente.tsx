"use client";

import { avisar, Boton, Cargando, cn, DialogoConfirmar, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento, MenuDesplegableEtiqueta } from "@iris/ui";
import { ChevronDown, CirclePause, CirclePlay, Sparkles, TriangleAlert, UserCheck, UserRound } from "lucide-react";
import { useState, type ReactNode } from "react";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { pausarAsistente, reanudarAsistente } from "@/lib/asistente/api";
import { franjaAtencion, MOTIVOS_PAUSA, pideConfirmarReanudar, type MotivoPausa } from "@/lib/asistente/atencion";
import { PERMISO_PAUSAR_ASISTENTE } from "@/lib/asistente/permisos";
import type { Conversacion } from "@/lib/conversaciones/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { TEXTO_CAMBIO, useAtender } from "./atencion";

/*
 * Quién atiende la conversación y el botón para pausar o reanudar el asistente en ella
 * (contratos/conversaciones.md §17.11). La pausa es por conversación: el asistente deja de
 * contestarle solo al cliente hasta que alguien lo reanude; las sugerencias al agente siguen.
 */

type Pausa = {
    /** Un toque: sin motivo. Con motivo, desde el menú. */
    pausar: (motivo?: Exclude<MotivoPausa, "manual">) => void;
    /** Reanuda; si el cliente pidió una persona, pregunta antes. */
    reanudar: () => void;
    ocupado: "pausar" | "reanudar" | null;
    error: string | null;
    /** El aviso previo a reanudar (se monta una vez, donde se use el hook). */
    dialogo: ReactNode;
};

/** Pausar y reanudar el asistente en una conversación, con su aviso y sus errores en llano. */
export function usePausaAsistente(conv: Conversacion, alHecho: (c: Conversacion) => void, recargar: () => Promise<void>): Pausa {
    const clave = useClaveIdempotencia();
    const [ocupado, setOcupado] = useState<Pausa["ocupado"]>(null);
    const [error, setError] = useState<string | null>(null);
    const [preguntando, setPreguntando] = useState(false);

    async function hacer(que: "pausar" | "reanudar", motivo?: Exclude<MotivoPausa, "manual">) {
        setOcupado(que);
        setError(null);
        try {
            const c = que === "pausar" ? await pausarAsistente(conv.id, conv.version, motivo, clave.actual()) : await reanudarAsistente(conv.id, conv.version, clave.actual());
            clave.renovar();
            alHecho(c);
            avisar({ titulo: que === "pausar" ? "Asistente en pausa en esta conversación" : "El asistente vuelve a atender" });
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 409) {
                // Otra persona (o el asistente) la cambió: se relee y el siguiente intento lleva la versión nueva.
                void recargar();
                setError(TEXTO_CAMBIO);
            } else if (e instanceof ErrorApi && e.codigo === "conversacion_resuelta") {
                void recargar();
                setError("La conversación ya está resuelta: el asistente no atiende conversaciones resueltas.");
            } else {
                setError(`No se pudo ${que === "pausar" ? "pausar" : "reanudar"}: ${mensajeDeError(e).titulo}`);
            }
        } finally {
            setOcupado(null);
        }
    }

    return {
        pausar: (motivo) => void hacer("pausar", motivo),
        reanudar: () => (pideConfirmarReanudar(conv) ? setPreguntando(true) : void hacer("reanudar")),
        ocupado,
        error,
        dialogo: (
            <DialogoConfirmar
                open={preguntando}
                onOpenChange={setPreguntando}
                titulo="El cliente pidió hablar con una persona"
                descripcion="¿Devolverle la conversación al asistente de todos modos? Queda registrado quién lo hizo."
                textoCancelar="Dejarla como está"
                textoConfirmar="Devolverla al asistente"
                alConfirmar={() => {
                    setPreguntando(false);
                    void hacer("reanudar");
                }}
            />
        ),
    };
}

const CLASE: Record<"marca" | "aviso" | "neutro", { caja: string; texto: string }> = {
    marca: { caja: "border-primary/20 bg-primary/[0.06]", texto: "text-primary" },
    aviso: { caja: "border-warning/30 bg-warning/10", texto: "text-warning" },
    neutro: { caja: "border-border bg-background-secondary", texto: "text-muted-foreground" },
};

type PropsFranja = {
    conv: Conversacion;
    ahora: number;
    alHecho: (c: Conversacion) => void;
    recargar: () => Promise<void>;
    /** La pantalla está detenida o la conversación es de solo lectura: se informa, sin botones. */
    bloqueada: boolean;
};

/**
 * La franja bajo la cabecera: quién atiende, en una línea, y lo que se puede hacer. En el teléfono
 * el texto va arriba y los botones debajo, a lo ancho; nada se corta.
 */
export function FranjaAtencion({ conv, ahora, alHecho, recargar, bloqueada }: PropsFranja) {
    const { puede } = useSesion();
    const franja = franjaAtencion(conv, ahora);
    const pausa = usePausaAsistente(conv, alHecho, recargar);
    const { atender, atendiendo, error: errorTomar } = useAtender(conv, alHecho, () => void recargar());
    if (!franja) return null;
    const puedePausar = puede([PERMISO_PAUSAR_ASISTENTE]) && !bloqueada;
    const puedeTomar = puede(["conversaciones.tomar"]) && !bloqueada;
    const acciones = franja.acciones.filter((a) => (a === "tomar" ? puedeTomar : puedePausar));
    const ocupada = pausa.ocupado !== null || atendiendo;
    const Icono = franja.tono === "aviso" ? (franja.estado === "espera_persona" ? UserRound : TriangleAlert) : franja.estado === "pausado" || franja.estado === "tope" ? CirclePause : Sparkles;
    const error = pausa.error ?? errorTomar;
    return (
        <div
            role="status"
            data-slot="franja-asistente"
            data-estado={franja.estado === "atiende" ? "atendiendo" : franja.estado}
            data-tono={franja.tono}
            className={cn("flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1.5 border-t px-3 py-1.5 @min-[700px]/conv:px-3.5", CLASE[franja.tono].caja)}
        >
            <span className={cn("flex min-w-0 flex-1 basis-56 items-start gap-2 text-[0.8125rem] font-medium", CLASE[franja.tono].texto)}>
                <Icono aria-hidden className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 [overflow-wrap:anywhere]">{franja.texto}</span>
            </span>
            {acciones.length > 0 ? (
                <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                    {acciones.map((a) =>
                        a === "pausar" ? (
                            <span key={a} className="flex items-center">
                                <Boton variante="contorno" tamano="sm" className="h-8 shrink-0 rounded-r-none" onClick={() => pausa.pausar()} disabled={ocupada} aria-label={`Pausar el asistente en la conversación con ${conv.titulo}`}>
                                    {pausa.ocupado === "pausar" ? <Cargando texto="Pausando…" /> : <CirclePause />}
                                    {pausa.ocupado === "pausar" ? null : "Pausar"}
                                </Boton>
                                <MenuDesplegable>
                                    <MenuDesplegableDisparador>
                                        <Boton variante="contorno" tamano="sm" className="h-8 w-8 shrink-0 rounded-l-none border-l-0 px-0" disabled={ocupada} aria-label="Pausar diciendo por qué" title="Pausar diciendo por qué">
                                            <ChevronDown />
                                        </Boton>
                                    </MenuDesplegableDisparador>
                                    <MenuDesplegableContenido align="end">
                                        <MenuDesplegableEtiqueta>Pausar porque…</MenuDesplegableEtiqueta>
                                        {MOTIVOS_PAUSA.map((m) => (
                                            <MenuDesplegableElemento key={m.valor} onSelect={() => pausa.pausar(m.valor)}>
                                                {m.etiqueta}
                                            </MenuDesplegableElemento>
                                        ))}
                                    </MenuDesplegableContenido>
                                </MenuDesplegable>
                            </span>
                        ) : a === "reanudar" ? (
                            <Boton key={a} variante="contorno" tamano="sm" className="h-8 shrink-0" onClick={pausa.reanudar} disabled={ocupada} aria-label={`${franja.textoReanudar}: conversación con ${conv.titulo}`}>
                                {pausa.ocupado === "reanudar" ? <Cargando texto="Reanudando…" /> : <CirclePlay />}
                                {pausa.ocupado === "reanudar" ? null : franja.textoReanudar}
                            </Boton>
                        ) : (
                            <Boton key={a} variante="contorno" tamano="sm" className="h-8 shrink-0" onClick={() => void atender()} disabled={ocupada} aria-label={`Tomar la conversación con ${conv.titulo}`}>
                                {atendiendo ? <Cargando texto="Tomando…" /> : <UserCheck />}
                                {atendiendo ? null : "Tomar"}
                            </Boton>
                        ),
                    )}
                </span>
            ) : null}
            {error ? (
                <span role="alert" className="basis-full text-xs font-medium text-error [overflow-wrap:anywhere]">
                    {error}
                </span>
            ) : null}
            {pausa.dialogo}
        </div>
    );
}
