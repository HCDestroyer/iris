"use client";

import { Aviso, Boton, Cargando, cn, Esqueleto, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento } from "@iris/ui";
import { elementosHilo, enlaceUbicacion, estadoEnvio, horaMensaje, TEXTO_ELIMINADO, type MensajeVista, type MotorChat, type VistaConversacion } from "@iris/sync/chat";
import { Ban, Check, CheckCheck, Clock, Ellipsis, ImageIcon, Map as IconoMapa, MapPin, RotateCcw, RotateCw, Trash2, TriangleAlert } from "lucide-react";
import { useEffect, useLayoutEffect, useRef } from "react";
import { useAhora } from "@/componentes/tickets/insignias";
import { sePuedeEliminar } from "@/lib/chat/eliminar-diferido";
import { mencionadosCon, segmentosChat, textoMotivo } from "@/lib/chat/presentacion";
import { rutaDescargaAdjunto } from "@/lib/ordenes/adjuntos";
import type { MiembroChat } from "@/lib/chat/api";

type Props = {
    motor: MotorChat;
    vista: VistaConversacion;
    miembros: readonly MiembroChat[];
    yoId: number;
    eliminando: ReadonlySet<string>;
    puedeEliminar: boolean;
    alEliminar: (m: MensajeVista) => void;
    className?: string;
};

/** Mensajes de una conversación del equipo, del más viejo al más nuevo, con su estado. */
export function HiloChat({ motor, vista, miembros, yoId, eliminando, puedeEliminar, alEliminar, className }: Props) {
    const ahora = useAhora(15_000);
    const caja = useRef<HTMLDivElement>(null);
    const pegado = useRef(true);
    const tipo = vista.conversacion?.tipo ?? "directa";
    const mensajes = vista.mensajes.map((m) => (eliminando.has(m.id) && !m.eliminado ? { ...m, eliminado: true, texto: null, foto: null, ubicacion: null, menciones: [] } : m));
    const elementos = elementosHilo(mensajes, true, ahora);
    const ultimo = vista.mensajes.at(-1);

    // Pegado al final mientras quien lee esté abajo (al llegar o al escribir algo nuevo).
    useLayoutEffect(() => {
        const el = caja.current;
        if (el && pegado.current) el.scrollTop = el.scrollHeight;
    }, [ultimo?.id, ultimo?.envio, vista.carga]);

    // Si la caja cambia de alto (un aviso arriba, el teclado del teléfono) sigue pegada al final.
    useEffect(() => {
        const el = caja.current;
        if (!el || typeof ResizeObserver === "undefined") return undefined;
        const o = new ResizeObserver(() => {
            if (pegado.current) el.scrollTop = el.scrollHeight;
        });
        o.observe(el);
        return () => o.disconnect();
    }, []);

    // Leído: con el último mensaje a la vista y la pestaña visible.
    useEffect(() => {
        if (!ultimo || ultimo.seq === null || document.visibilityState !== "visible") return;
        void motor.marcarLeido(vista.id);
    }, [motor, vista.id, ultimo?.id, ultimo?.seq, ultimo]);

    const roles = new Map(miembros.map((m) => [m.usuario_id, textoMotivo(m.motivo, tipo)]));

    return (
        <div
            ref={caja}
            className={cn("min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 py-3 sm:px-4", className)}
            data-slot="hilo-chat"
            onScroll={(e) => {
                const el = e.currentTarget;
                pegado.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
            }}
        >
            {vista.hayAnteriores ? (
                <div className="mb-3 flex justify-center">
                    <Boton variante="secundario" tamano="sm" disabled={vista.cargandoAnteriores} onClick={() => void motor.cargarAnteriores(vista.id)}>
                        {vista.cargandoAnteriores ? <Cargando texto="Cargando…" /> : "Ver mensajes anteriores"}
                    </Boton>
                </div>
            ) : null}
            {vista.carga === "error" && vista.mensajes.length === 0 ? (
                <Aviso
                    tono="error"
                    titulo="No se pudieron leer los mensajes"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={() => void motor.cargarConversacion(vista.id)}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {vista.error?.mensaje ?? "Revisa tu conexión."}
                </Aviso>
            ) : vista.carga === "cargando" && vista.mensajes.length === 0 ? (
                <div aria-busy="true" className="grid gap-3">
                    <span className="sr-only" role="status">
                        Cargando mensajes…
                    </span>
                    <Esqueleto className="h-14 w-2/3" />
                    <Esqueleto className="ml-auto h-14 w-1/2" />
                    <Esqueleto className="h-10 w-3/5" />
                </div>
            ) : vista.mensajes.length === 0 ? (
                <p className="py-10 text-center text-sm text-muted-foreground">Aún no hay mensajes. Escribe el primero.</p>
            ) : (
                <ol aria-label="Mensajes" className="grid gap-2">
                    {elementos.map((e) =>
                        e.tipo === "dia" ? (
                            <li key={e.clave} className="my-1 flex justify-center">
                                <span className="rounded-full bg-elevated px-2.5 py-0.5 text-xs text-muted-foreground">{e.texto}</span>
                            </li>
                        ) : (
                            <Burbuja
                                key={e.clave}
                                m={e.mensaje}
                                conAutor={e.conAutor}
                                rol={roles.get(e.mensaje.autorId) ?? null}
                                miembros={miembros}
                                yoId={yoId}
                                motor={motor}
                                eliminable={puedeEliminar && !eliminando.has(e.mensaje.id) && sePuedeEliminar(e.mensaje, ahora)}
                                alEliminar={alEliminar}
                            />
                        ),
                    )}
                </ol>
            )}
        </div>
    );
}

function Burbuja({
    m,
    conAutor,
    rol,
    miembros,
    yoId,
    motor,
    eliminable,
    alEliminar,
}: {
    m: MensajeVista;
    conAutor: boolean;
    rol: string | null;
    miembros: readonly MiembroChat[];
    yoId: number;
    motor: MotorChat;
    eliminable: boolean;
    alEliminar: (m: MensajeVista) => void;
}) {
    const estado = estadoEnvio(m);
    const hora = horaMensaje(m);
    const propio = m.propio;
    if (m.eliminado) {
        return (
            <li className={cn("flex", propio ? "justify-end" : "justify-start")} data-mensaje={m.id} data-eliminado="true">
                <div className="grid gap-0.5 rounded-xl border border-dashed border-border px-3 py-2 text-sm text-muted-foreground italic">
                    <span className="inline-flex items-center gap-1.5">
                        <Ban aria-hidden className="size-4" />
                        {TEXTO_ELIMINADO}
                    </span>
                    <span className="justify-self-end text-xs not-italic tabular-nums">{hora}</span>
                </div>
            </li>
        );
    }
    const pie = (
        <span className={cn("flex items-center justify-end gap-1 text-[0.6875rem] tabular-nums", propio ? "text-primary-foreground/85" : "text-muted-foreground")}>
            {hora}
            {estado ? (
                <>
                    <span aria-hidden>·</span>
                    {estado.texto === "visto" ? (
                        <CheckCheck aria-hidden className="size-3.5" />
                    ) : estado.texto === "enviado" ? (
                        <Check aria-hidden className="size-3.5" />
                    ) : estado.tono === "error" ? (
                        <TriangleAlert aria-hidden className="size-3.5" />
                    ) : (
                        <Clock aria-hidden className="size-3.5" />
                    )}
                    <span data-slot="estado-envio">{estado.texto === "enviando" ? "enviando…" : estado.texto}</span>
                </>
            ) : null}
        </span>
    );
    return (
        <li className={cn("group flex min-w-0 flex-col", propio ? "items-end" : "items-start")} data-mensaje={m.id}>
            {conAutor ? (
                <span className="mb-0.5 px-1 text-xs text-muted-foreground">
                    <span className="font-semibold text-foreground">{m.autorNombre}</span>
                    {rol ? ` · ${rol}` : null}
                </span>
            ) : null}
            <div className={cn("flex max-w-[min(34rem,88%)] min-w-0 items-start gap-1", propio && "flex-row-reverse")}>
                <div
                    className={cn(
                        "grid min-w-0 gap-1 rounded-2xl px-3.5 py-2 text-[0.875rem] break-words whitespace-pre-wrap",
                        propio ? "rounded-br-md bg-primary text-primary-foreground" : "rounded-bl-md border border-border bg-card",
                        m.envio === "fallido" && "ring-2 ring-error",
                    )}
                >
                    {m.foto ? <FotoMensaje m={m} /> : null}
                    {ubicacionValida(m) ? <UbicacionMensaje m={m} /> : null}
                    {m.texto ? <TextoMensaje texto={m.texto} mencionados={mencionadosCon(m.menciones, miembros)} yoId={yoId} propio={propio} /> : null}
                    {pie}
                </div>
                {eliminable ? (
                    <MenuDesplegable>
                        <MenuDesplegableDisparador>
                            <Boton variante="fantasma" tamano="icono-sm" className="mt-1 opacity-70 hover:opacity-100 focus-visible:opacity-100" aria-label="Opciones del mensaje">
                                <Ellipsis />
                            </Boton>
                        </MenuDesplegableDisparador>
                        <MenuDesplegableContenido align={propio ? "end" : "start"}>
                            <MenuDesplegableElemento icono={Trash2} onSelect={() => alEliminar(m)}>
                                Eliminar
                            </MenuDesplegableElemento>
                        </MenuDesplegableContenido>
                    </MenuDesplegable>
                ) : null}
            </div>
            {m.envio === "fallido" ? (
                <div className="mt-1 flex flex-wrap items-center justify-end gap-1.5 text-xs text-error">
                    <span>{m.error?.codigo === "conversacion_cerrada" ? "La orden se cerró: el chat ya no recibe mensajes." : (m.error?.mensaje ?? "No se envió.")}</span>
                    {m.error?.codigo !== "conversacion_cerrada" ? (
                        <Boton variante="secundario" tamano="sm" className="h-7" onClick={() => void motor.reintentar(m.id)}>
                            <RotateCcw />
                            Reintentar
                        </Boton>
                    ) : null}
                    <Boton variante="fantasma" tamano="sm" className="h-7" onClick={() => void motor.descartar(m.id)}>
                        Descartar
                    </Boton>
                </div>
            ) : null}
        </li>
    );
}

function TextoMensaje({ texto, mencionados, yoId, propio }: { texto: string; mencionados: readonly { id: number; nombre: string }[]; yoId: number; propio: boolean }) {
    return (
        <p>
            {segmentosChat(texto, mencionados, yoId).map((s, i) =>
                s.tipo === "texto" ? (
                    <span key={i}>{s.texto}</span>
                ) : s.tipo === "enlace" ? (
                    <a key={i} href={s.url} target="_blank" rel="noopener noreferrer nofollow" className="underline underline-offset-2">
                        {s.texto}
                    </a>
                ) : (
                    <span key={i} data-mencion={s.propia ? "propia" : "otra"} className={cn("font-semibold underline underline-offset-2", !propio && "text-primary", s.propia && !propio && "rounded bg-primary/10 px-0.5")}>
                        {s.texto}
                    </span>
                ),
            )}
        </p>
    );
}

function FotoMensaje({ m }: { m: MensajeVista }) {
    const foto = m.foto;
    if (!foto) return null;
    // La ruta se arma siempre desde el id validado: nunca se sigue la URL que viene en los datos.
    const url = foto.estado === "completo" ? rutaDescargaAdjunto(foto.id) : undefined;
    if (foto.estado === "completo" && !url) return null;
    if (url) {
        return (
            <a href={url} target="_blank" rel="noopener noreferrer" className="block overflow-hidden rounded-lg">
                {/* eslint-disable-next-line @next/next/no-img-element -- adjunto privado servido por la API con la cookie */}
                <img src={url} alt={`Foto de ${m.autorNombre || "tu mensaje"}`} className="max-h-64 w-full max-w-72 object-cover" loading="lazy" />
            </a>
        );
    }
    return (
        <span className="inline-flex items-center gap-1.5 text-xs opacity-80">
            <ImageIcon aria-hidden className="size-4" />
            {foto.estado === "rechazado" ? "Foto no disponible" : "Foto subiendo…"}
        </span>
    );
}

/** Solo coordenadas finitas y en rango: lo demás no se muestra (ni arma un enlace al mapa). */
function ubicacionValida(m: MensajeVista): boolean {
    const u = m.ubicacion;
    return u !== null && Number.isFinite(u.lat) && Number.isFinite(u.lon) && Math.abs(u.lat) <= 90 && Math.abs(u.lon) <= 180;
}

function UbicacionMensaje({ m }: { m: MensajeVista }) {
    if (!m.ubicacion || !ubicacionValida(m)) return null;
    const { lat, lon } = m.ubicacion;
    return (
        <span className="grid gap-0.5">
            <span className="inline-flex items-center gap-1.5 font-semibold">
                <MapPin aria-hidden className="size-4" />
                {m.propio ? "Tu ubicación" : `Ubicación de ${m.autorNombre.split(" ")[0] ?? ""}`}
            </span>
            <span className="text-xs tabular-nums opacity-80">
                {lat.toFixed(5)}, {lon.toFixed(5)}
            </span>
            <a href={enlaceUbicacion(lat, lon)} target="_blank" rel="noopener noreferrer" className={cn("inline-flex items-center gap-1 text-sm font-semibold", m.propio ? "underline" : "text-primary")}>
                <IconoMapa aria-hidden className="size-4" />
                Ver en el mapa
            </a>
        </span>
    );
}
