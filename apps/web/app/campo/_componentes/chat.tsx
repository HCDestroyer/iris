"use client";

import {
    elementosHilo,
    enlaceUbicacion,
    ErrorChatLocal,
    estadoEnvio,
    horaLista,
    horaMensaje,
    puedeEliminar,
    RESPUESTAS_RAPIDAS,
    segmentosTexto,
    subtituloConversacion,
    TEXTO_ELIMINADO,
    textoContador,
    textoValido,
    tituloConversacion,
    vistaPrevia,
    type ConversacionVista,
    type MensajeVista,
} from "@iris/sync/chat";
import { iniciales, urlDeVista, type Vista } from "@iris/sync/campo";
import { AreaTexto, Aviso, Boton, Camara, Cargando, EstadoVacio, cn } from "@iris/ui";
import { ArrowLeft, Ban, ChevronRight, ClipboardList, Copy, MapPin, MessageCircle, MessageSquarePlus, RefreshCw, RotateCcw, Send, Trash2, TriangleAlert, Users, WifiOff } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type MouseEvent, type PointerEvent, type ReactNode } from "react";
import { ErrorTransporte } from "@iris/sync";
import { useAhora } from "@/componentes/tickets/insignias";
import { leerExif } from "@/lib/campo/exif";
import { comprimirFoto } from "@/lib/campo/imagen";
import { useChat, useChatOpcional, useConversacion, useFotoLocal } from "./chat-datos";
import { useCampo } from "./contexto";
import { HojaCampo } from "./hoja";
import { useUbicacion } from "./ubicacion";

/* ----------------------------------------------------------------------------------------------
 * Punto de entrada: un solo componente, para ponerlo luego en la isla o en el encabezado.
 * --------------------------------------------------------------------------------------------*/

/**
 * Entrada al chat con el punto de no leídos (discreto; con número cuando hay). `fila`: el botón
 * ancho de «Yo»; `redondo`: 48 px, para la isla o el encabezado cuando el PM decida su lugar.
 */
export function EntradaChat({ variante = "fila", className }: { variante?: "fila" | "redondo"; className?: string }) {
    const contexto = useChatOpcional();
    const { navegar } = useCampo();
    if (!contexto) {
        return null;
    }
    const n = contexto.estado.noLeidos;
    const etiqueta = n > 0 ? `Chat del equipo (${n} sin leer)` : "Chat del equipo";
    const punto =
        n > 0 ? (
            <span aria-hidden data-slot="no-leidos-chat" className="grid min-w-5 place-content-center rounded-full bg-primary px-1.5 text-[0.6875rem] leading-5 font-bold text-primary-foreground tabular-nums">
                {textoContador(n)}
            </span>
        ) : null;
    if (variante === "redondo") {
        return (
            <Boton variante="secundario" aria-label={etiqueta} title="Chat del equipo" className={cn("relative size-12 shrink-0 rounded-full p-0 md:size-12 [&_svg]:size-5", className)} onClick={() => navegar({ tipo: "chat" })}>
                <MessageCircle aria-hidden />
                {punto ? <span className="absolute -top-1 -right-1">{punto}</span> : null}
            </Boton>
        );
    }
    return (
        <Boton variante="secundario" tamano="lg" aria-label={etiqueta} className={cn("h-auto min-h-14 justify-start py-3", className)} onClick={() => navegar({ tipo: "chat" })}>
            <MessageCircle aria-hidden />
            <span className="flex-1 text-left">Chat del equipo</span>
            {punto}
            <ChevronRight aria-hidden className="text-muted-foreground" />
        </Boton>
    );
}

/** «Abrir chat de la orden» (`POST /chat/ordenes/{id}/abrir`); sin señal, el hilo ya guardado. */
/**
 * Abre (o crea) la conversación de la orden: con la conocida y sin señal va directo; si no, la pide
 * al servidor. `null` sin chat, o sin `chat.enviar` y sin conversación conocida. Lo usan el botón
 * del detalle y «Chat de la orden» del menú «⋯».
 */
export function useAbrirChatOrden(ordenId: number): { abrir: () => Promise<void>; abriendo: boolean; noLeidos: number } | null {
    const contexto = useChatOpcional();
    const { navegar, notificar } = useCampo();
    const [abriendo, setAbriendo] = useState(false);
    if (!contexto || (!contexto.puedeEnviar && !contexto.chat.conversacionDeOrden(ordenId))) {
        return null;
    }
    const { chat } = contexto;
    const conocida = chat.conversacionDeOrden(ordenId);
    const abrir = async () => {
        if (conocida && !navigator.onLine) {
            navegar({ tipo: "conversacion", id: conocida.id });
            return;
        }
        setAbriendo(true);
        try {
            const c = await chat.abrirOrden(ordenId);
            navegar({ tipo: "conversacion", id: c.id });
        } catch (e) {
            if (conocida) {
                navegar({ tipo: "conversacion", id: conocida.id });
            } else {
                notificar({ tono: "error", texto: e instanceof ErrorTransporte && e.tipo === "red" ? "Sin señal: el chat de la orden se abre al volver la señal." : "No se pudo abrir el chat de la orden." });
            }
        } finally {
            setAbriendo(false);
        }
    };
    return { abrir, abriendo, noLeidos: conocida?.no_leidos ?? 0 };
}

export function AbrirChatOrden({ ordenId }: { ordenId: number }) {
    const chatOrden = useAbrirChatOrden(ordenId);
    if (!chatOrden) {
        return null;
    }
    const { abrir, abriendo, noLeidos } = chatOrden;
    return (
        <Boton variante="secundario" tamano="lg" className="h-12 w-full justify-start rounded-full md:h-12" disabled={abriendo} onClick={() => void abrir()}>
            <MessageCircle aria-hidden />
            <span className="flex-1 text-left">{abriendo ? "Abriendo chat…" : "Abrir chat de la orden"}</span>
            {noLeidos > 0 ? <span className="text-sm font-semibold text-primary tabular-nums">{textoContador(noLeidos)} sin leer</span> : null}
        </Boton>
    );
}

/* ----------------------------------------------------------------------------------------------
 * Lista de conversaciones
 * --------------------------------------------------------------------------------------------*/

const ICONO_TIPO = { orden: ClipboardList, cuadrilla: Users, directa: MessageCircle } as const;

function Avatar({ c }: { c: ConversacionVista }) {
    if (c.tipo === "directa") {
        return (
            <span aria-hidden className="grid size-12 shrink-0 place-content-center rounded-full bg-[var(--campo-marca-suave)] font-bold text-[var(--campo-marca-suave-texto)]">
                {iniciales(tituloConversacion(c)) || "?"}
            </span>
        );
    }
    const Icono = ICONO_TIPO[c.tipo];
    return (
        <span aria-hidden className="grid size-12 shrink-0 place-content-center rounded-full bg-[var(--campo-panel-2)] text-foreground">
            <Icono className="size-5" />
        </span>
    );
}

/** Al tirar hacia abajo desde arriba (teléfono), `alSoltar`. Devuelve cuánto se tiró (px). */
function useTirarParaActualizar(alSoltar: () => void) {
    const inicio = useRef<number | null>(null);
    const [tiron, setTiron] = useState(0);
    useEffect(() => {
        const empezar = (e: TouchEvent) => {
            inicio.current = window.scrollY <= 0 ? (e.touches[0]?.clientY ?? null) : null;
        };
        const mover = (e: TouchEvent) => {
            if (inicio.current === null) {
                return;
            }
            setTiron(Math.max(0, Math.min(96, (e.touches[0]?.clientY ?? 0) - inicio.current)));
        };
        const soltar = () => {
            setTiron((t) => {
                if (t >= 72) {
                    alSoltar();
                }
                return 0;
            });
            inicio.current = null;
        };
        window.addEventListener("touchstart", empezar, { passive: true });
        window.addEventListener("touchmove", mover, { passive: true });
        window.addEventListener("touchend", soltar);
        return () => {
            window.removeEventListener("touchstart", empezar);
            window.removeEventListener("touchmove", mover);
            window.removeEventListener("touchend", soltar);
        };
    }, [alSoltar]);
    return tiron;
}

function enlace(navegar: (v: Vista) => void, v: Vista) {
    return {
        href: urlDeVista(v),
        onClick: (e: MouseEvent<HTMLAnchorElement>) => {
            e.preventDefault();
            navegar(v);
        },
    };
}

export function ListaChat() {
    const { chat, estado, puedeEnviar } = useChat();
    const { navegar, sesion } = useCampo();
    const [actualizando, setActualizando] = useState(false);
    const actualizar = useCallback(() => {
        setActualizando(true);
        void chat.recargar().finally(() => setActualizando(false));
    }, [chat]);
    const tiron = useTirarParaActualizar(actualizar);
    // Al entrar, la lista al día (con señal).
    useEffect(() => {
        if (navigator.onLine) {
            void chat.recargar();
        }
    }, [chat]);
    const ahora = useAhora(30_000);
    const sinDatos = estado.conversaciones.length === 0;
    const error = estado.lista.estado === "error" ? estado.lista.error : null;
    return (
        <section aria-labelledby="titulo-chat" className="grid grid-cols-[minmax(0,1fr)] gap-4" data-slot="lista-chat">
            {/* Solo ocupa lugar mientras se tira o se actualiza (si no, sobra un hueco bajo el encabezado). */}
            {tiron > 0 || actualizando ? (
                <div aria-hidden className="grid place-content-center overflow-hidden text-muted-foreground transition-[height]" style={{ height: Math.max(tiron, actualizando ? 40 : 0) }}>
                    <RefreshCw className={cn("size-5", actualizando && "animate-spin")} />
                </div>
            ) : null}
            <div className="flex items-center justify-between gap-3">
                <div className="grid min-w-0 gap-0.5">
                    <h1 id="titulo-chat" className="text-2xl font-semibold tracking-[-0.015em]">
                        Chat del equipo
                    </h1>
                    <p className="text-sm text-muted-foreground">Despacho, tu cuadrilla y tus órdenes. El cliente no lo ve.</p>
                </div>
                <Boton variante="secundario" aria-label="Actualizar conversaciones" title="Actualizar" className="size-12 shrink-0 rounded-full p-0 md:size-12" disabled={actualizando} onClick={actualizar}>
                    <RefreshCw aria-hidden className={cn(actualizando && "animate-spin")} />
                </Boton>
            </div>
            {puedeEnviar ? (
                <Boton tamano="lg" className="h-12 w-full rounded-full md:h-12" onClick={() => navegar({ tipo: "chat_nuevo" })}>
                    <MessageSquarePlus aria-hidden />
                    Nuevo mensaje
                </Boton>
            ) : null}
            {!estado.enLinea ? (
                <p className="flex items-center gap-2 text-sm text-warning" data-slot="chat-sin-senal">
                    <WifiOff aria-hidden className="size-4" />
                    Sin señal: ves lo guardado en el teléfono; lo que escribas se envía al volver la señal.
                </p>
            ) : null}
            {estado.sesionCerrada ? (
                <Aviso tono="error" titulo="Tu sesión terminó">
                    Entra de nuevo para leer y enviar mensajes. Lo que escribiste sigue guardado.
                </Aviso>
            ) : null}
            {error && sinDatos ? (
                <Aviso
                    tono="error"
                    titulo="No se pudieron leer tus conversaciones"
                    accion={
                        <Boton variante="secundario" tamano="lg" onClick={actualizar}>
                            Reintentar
                        </Boton>
                    }
                >
                    {estado.enLinea ? "Inténtalo de nuevo en un momento." : "Sin señal. Se intentará al volver la señal."}
                </Aviso>
            ) : error ? (
                <p className="flex items-center gap-2 text-sm text-muted-foreground">
                    <TriangleAlert aria-hidden className="size-4 text-warning" />
                    No se pudo actualizar; ves lo guardado.
                    <Boton variante="enlace" onClick={actualizar}>
                        Reintentar
                    </Boton>
                </p>
            ) : null}
            {sinDatos && !error ? (
                estado.lista.estado === "cargando" || actualizando ? (
                    <Cargando visible texto="Leyendo tus conversaciones…" />
                ) : (
                    <EstadoVacio icono={MessageCircle} titulo="Todavía no tienes conversaciones" descripcion="Abre el chat desde una orden o escribe un «Nuevo mensaje» a alguien de tu equipo." />
                )
            ) : (
                <ul className="grid grid-cols-[minmax(0,1fr)] gap-2" aria-label="Conversaciones">
                    {estado.conversaciones.map((c) => {
                        const titulo = tituloConversacion(c);
                        const etiqueta = [titulo, c.no_leidos > 0 ? `${c.no_leidos} sin leer` : null, c.fallidos > 0 ? "con mensajes no enviados" : null].filter(Boolean).join(", ");
                        return (
                            <li key={c.id}>
                                <a
                                    {...enlace(navegar, { tipo: "conversacion", id: c.id })}
                                    aria-label={etiqueta}
                                    data-slot="fila-conversacion"
                                    className="flex min-h-[4.5rem] items-center gap-3 rounded-[1.375rem] border border-border bg-[var(--campo-panel)] p-3 focus-visible:outline-2 focus-visible:outline-primary"
                                >
                                    <Avatar c={c} />
                                    <span className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)] gap-0.5">
                                        <span className="flex items-baseline justify-between gap-2">
                                            <span className={cn("truncate", c.no_leidos > 0 ? "font-bold" : "font-semibold")}>{titulo}</span>
                                            <span className={cn("shrink-0 text-xs tabular-nums", c.no_leidos > 0 ? "font-semibold text-primary" : "text-muted-foreground")}>
                                                {horaLista(c.ultimo_mensaje?.enviado_en ?? c.ultima_actividad, ahora)}
                                            </span>
                                        </span>
                                        <span className="flex items-center justify-between gap-2">
                                            <span className={cn("truncate text-sm", c.no_leidos > 0 ? "text-foreground" : "text-muted-foreground")}>
                                                {c.fallidos > 0 ? (
                                                    <span className="font-semibold text-error">No enviado · </span>
                                                ) : c.pendientes > 0 ? (
                                                    <span className="text-warning">Por enviar · </span>
                                                ) : null}
                                                {vistaPrevia(c, sesion.usuarioId)}
                                            </span>
                                            {c.no_leidos > 0 ? (
                                                <span aria-hidden className="grid min-w-5 shrink-0 place-content-center rounded-full bg-primary px-1.5 text-[0.6875rem] leading-5 font-bold text-primary-foreground tabular-nums">
                                                    {textoContador(c.no_leidos)}
                                                </span>
                                            ) : null}
                                        </span>
                                    </span>
                                </a>
                            </li>
                        );
                    })}
                </ul>
            )}
        </section>
    );
}

/* ----------------------------------------------------------------------------------------------
 * Nuevo mensaje (directa)
 * --------------------------------------------------------------------------------------------*/

export function NuevoChat() {
    const { chat } = useChat();
    const { navegar, notificar } = useCampo();
    const [abriendo, setAbriendo] = useState<number | null>(null);
    const contactos = chat.contactosConocidos();
    const abrir = async (id: number) => {
        setAbriendo(id);
        try {
            const c = await chat.abrirDirecta(id);
            navegar({ tipo: "conversacion", id: c.id });
        } catch (e) {
            const codigo = e instanceof ErrorTransporte ? e.codigo : null;
            notificar({
                tono: "error",
                texto: codigo === "usuario_no_disponible" ? "Esa persona ya no está disponible para el chat." : e instanceof ErrorTransporte && e.tipo === "red" ? "Sin señal: para empezar una conversación hace falta señal." : "No se pudo abrir la conversación.",
            });
        } finally {
            setAbriendo(null);
        }
    };
    return (
        <section aria-labelledby="titulo-nuevo-chat" className="grid grid-cols-[minmax(0,1fr)] gap-4">
            <div className="flex items-center gap-3">
                <Boton variante="secundario" aria-label="Volver al chat" className="size-12 shrink-0 rounded-full p-0 md:size-12" onClick={() => navegar({ tipo: "chat" })}>
                    <ArrowLeft aria-hidden />
                </Boton>
                <h1 id="titulo-nuevo-chat" className="text-2xl font-semibold tracking-[-0.015em]">
                    Nuevo mensaje
                </h1>
            </div>
            <p className="text-sm text-muted-foreground">Personas de tu equipo con las que ya coincidiste en el chat (tu cuadrilla, despacho y tus órdenes).</p>
            {contactos.length === 0 ? (
                <EstadoVacio icono={Users} titulo="Aún no hay personas para elegir" descripcion="Cuando alguien de despacho o de tu cuadrilla te escriba, aparecerá aquí. Mientras, usa el chat de tu cuadrilla o el de una orden." />
            ) : (
                <ul className="grid grid-cols-[minmax(0,1fr)] gap-2" aria-label="Personas">
                    {contactos.map((p) => (
                        <li key={p.id}>
                            <Boton variante="secundario" tamano="lg" className="h-auto min-h-14 w-full justify-start py-2" disabled={abriendo !== null} onClick={() => void abrir(p.id)}>
                                <span aria-hidden className="grid size-10 shrink-0 place-content-center rounded-full bg-[var(--campo-marca-suave)] text-sm font-bold text-[var(--campo-marca-suave-texto)]">
                                    {iniciales(p.nombre) || "?"}
                                </span>
                                <span className="flex-1 text-left">{abriendo === p.id ? "Abriendo…" : p.nombre}</span>
                                <ChevronRight aria-hidden className="text-muted-foreground" />
                            </Boton>
                        </li>
                    ))}
                </ul>
            )}
        </section>
    );
}

/* ----------------------------------------------------------------------------------------------
 * Conversación
 * --------------------------------------------------------------------------------------------*/

/** Alto visible (con el teclado abierto en iOS la ventana no se encoge: se usa `visualViewport`). */
function useAltoVisible(): number | null {
    const [alto, setAlto] = useState<number | null>(null);
    useEffect(() => {
        const vv = window.visualViewport;
        if (!vv) {
            return undefined;
        }
        const medir = () => setAlto(Math.round(vv.height));
        medir();
        vv.addEventListener("resize", medir);
        return () => vv.removeEventListener("resize", medir);
    }, []);
    return alto;
}

function TextoPlano({ texto }: { texto: string }) {
    // Solo texto: React lo escapa; los enlaces http(s) van con `rel` seguro (nada de HTML).
    return (
        <>
            {segmentosTexto(texto).map((s, i) =>
                s.tipo === "enlace" ? (
                    <a key={i} href={s.url} target="_blank" rel="noopener noreferrer nofollow" className="break-all underline underline-offset-2">
                        {s.texto}
                    </a>
                ) : (
                    <span key={i}>{s.texto}</span>
                ),
            )}
        </>
    );
}

function FotoBurbuja({ m }: { m: MensajeVista }) {
    const local = useFotoLocal(m.foto && m.foto.estado !== "completo" ? m.foto.id : null);
    const url = m.foto?.url ?? local;
    if (!m.foto) {
        return null;
    }
    return url ? (
        // eslint-disable-next-line @next/next/no-img-element -- foto del chat (blob: local o la descarga autenticada).
        <img src={url} alt={m.propio ? "Foto que enviaste" : `Foto de ${m.autorNombre}`} className="max-h-64 w-56 max-w-full rounded-[0.875rem] object-cover" />
    ) : (
        <span role="img" aria-label="Foto subiéndose" className="grid h-32 w-56 max-w-full place-content-center rounded-[0.875rem] bg-[var(--campo-panel-2)] text-xs text-muted-foreground">
            Foto subiéndose…
        </span>
    );
}

function Burbuja({ m, conAutor, alMenu, alReintentar }: { m: MensajeVista; conAutor: boolean; alMenu: (m: MensajeVista) => void; alReintentar: (m: MensajeVista) => void }) {
    const pulsacion = useRef<ReturnType<typeof setTimeout> | null>(null);
    const estado = estadoEnvio(m);
    const cancelar = () => {
        if (pulsacion.current) {
            clearTimeout(pulsacion.current);
            pulsacion.current = null;
        }
    };
    const abajo = (e: PointerEvent<HTMLDivElement>) => {
        if (e.pointerType === "mouse" && e.button !== 0) {
            return;
        }
        cancelar();
        pulsacion.current = setTimeout(() => alMenu(m), 500);
    };
    const tecla = (e: KeyboardEvent<HTMLDivElement>) => {
        if (e.key === "Enter" || e.key === " " || e.key === "ContextMenu") {
            e.preventDefault();
            alMenu(m);
        }
    };
    const hora = horaMensaje(m);
    return (
        <li className={cn("flex max-w-[80%] flex-col gap-1", m.propio ? "self-end items-end" : "self-start items-start")} data-slot="mensaje-chat" data-propio={m.propio ? "true" : "false"} data-envio={m.envio}>
            {conAutor ? <span className="px-1 text-xs font-semibold text-muted-foreground">{m.autorNombre}</span> : null}
            <div
                tabIndex={0}
                aria-haspopup="dialog"
                aria-label={`${m.propio ? "Tú" : m.autorNombre}, ${hora}${estado ? `, ${estado.texto}` : ""}. Mantén presionado para más opciones.`}
                onPointerDown={abajo}
                onPointerUp={cancelar}
                onPointerLeave={cancelar}
                onPointerCancel={cancelar}
                onContextMenu={(e) => {
                    e.preventDefault();
                    cancelar();
                    alMenu(m);
                }}
                onKeyDown={tecla}
                className={cn(
                    "grid gap-1.5 px-3 py-2.5 text-[0.9375rem] leading-[1.45] break-words whitespace-pre-wrap select-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary [-webkit-touch-callout:none]",
                    m.eliminado
                        ? "rounded-[1.125rem] border border-dashed border-border bg-transparent text-muted-foreground italic"
                        : m.propio
                          ? "rounded-[1.125rem_1.125rem_0.375rem_1.125rem] bg-primary text-primary-foreground"
                          : "rounded-[1.125rem_1.125rem_1.125rem_0.375rem] border border-border bg-[var(--campo-panel)] text-foreground",
                    m.envio === "fallido" && "ring-2 ring-error",
                )}
            >
                {m.eliminado ? (
                    <span className="flex items-center gap-1.5">
                        <Ban aria-hidden className="size-4" />
                        {TEXTO_ELIMINADO}
                    </span>
                ) : (
                    <>
                        <FotoBurbuja m={m} />
                        {m.ubicacion ? (
                            <a href={enlaceUbicacion(m.ubicacion.lat, m.ubicacion.lon)} target="_blank" rel="noopener noreferrer" className="flex items-center gap-1.5 font-semibold underline underline-offset-2">
                                <MapPin aria-hidden className="size-4" />
                                Ubicación compartida
                            </a>
                        ) : null}
                        {m.texto ? (
                            <span>
                                <TextoPlano texto={m.texto} />
                            </span>
                        ) : null}
                    </>
                )}
                <span className={cn("text-[0.6875rem] font-medium not-italic", m.propio && !m.eliminado ? "opacity-85" : "text-muted-foreground")}>
                    {hora}
                    {estado ? ` · ${estado.texto}` : ""}
                </span>
            </div>
            {m.envio === "fallido" ? (
                <Boton variante="enlace" className="min-h-12 px-1 text-sm text-error [--button-fg:var(--hx-error)]" onClick={() => alReintentar(m)}>
                    <TriangleAlert aria-hidden />
                    No enviado — Reintentar
                </Boton>
            ) : null}
        </li>
    );
}

function MenuMensaje({ m, alCerrar }: { m: MensajeVista | null; alCerrar: () => void }) {
    const { chat } = useChat();
    const { notificar } = useCampo();
    const ahora = useAhora(15_000);
    if (!m) {
        return null;
    }
    const eliminable = puedeEliminar(m, ahora);
    const correr = async (accion: () => Promise<void>, ok: string) => {
        alCerrar();
        try {
            await accion();
            notificar({ tono: "success", texto: ok });
        } catch (e) {
            notificar({ tono: "error", texto: e instanceof ErrorChatLocal ? e.message : "No se pudo guardar en el teléfono." });
        }
    };
    return (
        <HojaCampo abierto alCambiar={(a) => !a && alCerrar()} titulo="Mensaje" className="rounded-t-[2rem] sm:rounded-[2rem]">
            <div className="grid gap-2" data-slot="menu-mensaje">
                {m.texto ? (
                    <Boton
                        variante="secundario"
                        tamano="lg"
                        className="h-12 justify-start md:h-12"
                        onClick={() => {
                            alCerrar();
                            void navigator.clipboard?.writeText(m.texto ?? "").then(
                                () => notificar({ tono: "success", texto: "Texto copiado" }),
                                () => notificar({ tono: "error", texto: "No se pudo copiar." }),
                            );
                        }}
                    >
                        <Copy aria-hidden />
                        Copiar texto
                    </Boton>
                ) : null}
                {m.envio === "fallido" ? (
                    <>
                        <Boton variante="secundario" tamano="lg" className="h-12 justify-start md:h-12" onClick={() => void correr(() => chat.reintentar(m.id), "Se enviará de nuevo")}>
                            <RotateCcw aria-hidden />
                            Reintentar
                        </Boton>
                        <Boton variante="peligro" tamano="lg" className="h-12 justify-start md:h-12" onClick={() => void correr(() => chat.descartar(m.id), "Mensaje descartado")}>
                            <Trash2 aria-hidden />
                            Descartar
                        </Boton>
                    </>
                ) : null}
                {eliminable ? (
                    <Boton variante="peligro" tamano="lg" className="h-12 justify-start md:h-12" onClick={() => void correr(() => chat.eliminar(m.id), navigator.onLine ? "Mensaje eliminado" : "Se eliminará al volver la señal")}>
                        <Trash2 aria-hidden />
                        Eliminar para todos
                    </Boton>
                ) : m.propio && !m.eliminado && m.envio === "enviado" ? (
                    <p className="text-sm text-muted-foreground">Solo puedes eliminar un mensaje en los 15 minutos después de enviarlo.</p>
                ) : null}
                {!m.texto && m.envio !== "fallido" && !eliminable ? <p className="text-sm text-muted-foreground">No hay acciones para este mensaje.</p> : null}
            </div>
        </HojaCampo>
    );
}

function Compositor({ conversacionId, placeholder }: { conversacionId: number; placeholder: string }) {
    const { chat } = useChat();
    const { notificar } = useCampo();
    const { estado: ubicacion, posicionSiPermitida } = useUbicacion();
    const [texto, setTexto] = useState("");
    const [ocupado, setOcupado] = useState(false);
    const area = useRef<HTMLTextAreaElement | null>(null);
    const v = textoValido(texto);

    const enviar = async (entrada: Parameters<typeof chat.escribir>[1], limpiar: boolean) => {
        try {
            await chat.escribir(conversacionId, entrada);
            if (limpiar) {
                setTexto("");
                area.current?.focus();
            }
        } catch (e) {
            notificar({ tono: "error", texto: e instanceof ErrorChatLocal ? e.message : e instanceof Error && /quota/i.test(e.name + e.message) ? "El teléfono no tiene espacio para guardar el mensaje." : "No se pudo guardar el mensaje en el teléfono." });
        }
    };
    const foto = async (archivo: File) => {
        setOcupado(true);
        try {
            const original = new Uint8Array(await archivo.arrayBuffer());
            const exif = archivo.type === "image/jpeg" || /\.jpe?g$/i.test(archivo.name) ? leerExif(original) : {};
            const comprimida = await comprimirFoto(archivo);
            await enviar({ foto: { datos: comprimida.bytes, mime: comprimida.mime, tomadoEn: exif.tomadoEn ?? new Date().toISOString() }, ...(v.valido ? { texto } : {}) }, v.valido);
        } catch {
            notificar({ tono: "error", texto: "No se pudo preparar la foto." });
        } finally {
            setOcupado(false);
        }
    };
    const compartirUbicacion = async () => {
        setOcupado(true);
        const p = await posicionSiPermitida(8000);
        setOcupado(false);
        if (!p) {
            notificar({ tono: "warning", texto: "No se pudo leer tu ubicación ahora." });
            return;
        }
        await enviar({ ubicacion: { lat: p.lat, lon: p.lon } }, false);
    };
    const alTeclear = (e: KeyboardEvent<HTMLTextAreaElement>) => {
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && v.valido) {
            e.preventDefault();
            void enviar({ texto }, true);
        }
    };
    useLayoutEffect(() => {
        const el = area.current;
        if (el) {
            el.style.height = "auto";
            el.style.height = `${Math.min(el.scrollHeight, 132)}px`;
        }
    }, [texto]);
    return (
        <div className="grid gap-2.5 border-t border-border bg-[var(--campo-panel)] px-3.5 pt-2.5 pb-[calc(0.75rem+env(safe-area-inset-bottom))]" data-slot="compositor-chat">
            <div className="flex gap-2 overflow-x-auto [scrollbar-width:none]" role="group" aria-label="Respuestas rápidas">
                {RESPUESTAS_RAPIDAS.map((r) => (
                    <Boton key={r} variante="secundario" className="h-12 shrink-0 rounded-full bg-[var(--campo-panel-2)] px-4 text-[0.8125rem] font-medium md:h-12" onClick={() => void enviar({ texto: r }, false)}>
                        {r}
                    </Boton>
                ))}
            </div>
            <div className="flex items-end gap-2">
                <Camara aria-label="Tomar foto" disabled={ocupado} alElegir={(a) => void foto(a)} className="size-12 shrink-0 rounded-full p-0 md:size-12">
                    <span className="sr-only">Tomar foto</span>
                </Camara>
                {ubicacion.permiso === "concedido" ? (
                    <Boton variante="secundario" aria-label="Compartir mi ubicación" title="Compartir mi ubicación" disabled={ocupado} className="size-12 shrink-0 rounded-full p-0 md:size-12" onClick={() => void compartirUbicacion()}>
                        <MapPin aria-hidden />
                    </Boton>
                ) : null}
                <AreaTexto
                    ref={area}
                    rows={1}
                    aria-label="Mensaje"
                    placeholder={placeholder}
                    value={texto}
                    maxLength={4100}
                    onChange={(e) => setTexto(e.target.value)}
                    onKeyDown={alTeclear}
                    aria-invalid={v.motivo ? true : undefined}
                    className="max-h-[8.25rem] min-h-12 flex-1 resize-none rounded-[1.5rem] px-4 py-3 text-base"
                />
                <Boton aria-label="Enviar" title="Enviar" disabled={!v.valido} className="size-12 shrink-0 rounded-full p-0 md:size-12" onClick={() => void enviar({ texto }, true)}>
                    <Send aria-hidden />
                </Boton>
            </div>
            {v.motivo ? (
                <p role="alert" className="text-xs text-error">
                    {v.motivo}
                </p>
            ) : null}
        </div>
    );
}

/** Vuelve a la lista del chat (o a donde estaba, con el «Atrás» del navegador). */
function useVolver() {
    const { navegar } = useCampo();
    return () => {
        if (window.history.length > 1) {
            window.history.back();
        } else {
            navegar({ tipo: "chat" });
        }
    };
}

export function ConversacionPantalla({ id }: { id: number }) {
    const { chat, estado, puedeEnviar } = useChat();
    const { navegar, notificar } = useCampo();
    const v = useConversacion(id);
    const volver = useVolver();
    const alto = useAltoVisible();
    const lista = useRef<HTMLDivElement | null>(null);
    const alFinal = useRef(true);
    const altoAntes = useRef<number | null>(null);
    const [menu, setMenu] = useState<MensajeVista | null>(null);
    const ahora = useAhora(60_000);

    useEffect(() => {
        void chat.cargarConversacion(id);
    }, [chat, id]);

    // Avisos del motor (eliminar tarde, sin acceso): entran desde arriba una vez.
    useEffect(() => {
        for (const a of estado.avisos) {
            notificar({ tono: a.tono, texto: a.texto });
            chat.descartarAviso(a.id);
        }
    }, [estado.avisos, chat, notificar]);

    // Al llegar mensajes: si estaba al final, sigue al final; al cargar anteriores, se conserva la vista.
    useLayoutEffect(() => {
        const el = lista.current;
        if (!el) {
            return;
        }
        if (altoAntes.current !== null) {
            el.scrollTop += el.scrollHeight - altoAntes.current;
            altoAntes.current = null;
        } else if (alFinal.current) {
            el.scrollTop = el.scrollHeight;
        }
    }, [v.mensajes]);

    // Con el último mensaje a la vista y la pestaña visible: leído.
    const ultimo = v.mensajes.at(-1)?.id;
    useEffect(() => {
        if (alFinal.current && document.visibilityState === "visible" && v.carga !== "cargando") {
            void chat.marcarLeido(id);
        }
    }, [chat, id, ultimo, v.carga, v.conversacion?.no_leidos]);

    const alDesplazar = () => {
        const el = lista.current;
        if (!el) {
            return;
        }
        alFinal.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
        if (alFinal.current) {
            void chat.marcarLeido(id);
        }
        if (el.scrollTop < 80 && v.hayAnteriores && !v.cargandoAnteriores) {
            altoAntes.current = el.scrollHeight;
            void chat.cargarAnteriores(id);
        }
    };

    const c = v.conversacion;
    const grupo = c?.tipo !== "directa";
    const otras = estado.conversaciones.filter((x) => x.id !== id).slice(0, 3);
    const titulo = c ? tituloConversacion(c) : "Conversación";
    const reintentar = async (m: MensajeVista) => {
        await chat.reintentar(m.id);
        notificar({ tono: "success", texto: "Se enviará de nuevo" });
    };

    let cuerpo: ReactNode;
    if (!c && v.carga === "error") {
        cuerpo = (
            <div className="p-4">
                <Aviso
                    tono="error"
                    titulo="No se pudo abrir la conversación"
                    accion={
                        <Boton variante="secundario" tamano="lg" onClick={() => void chat.cargarConversacion(id)}>
                            Reintentar
                        </Boton>
                    }
                >
                    {v.error?.status === 404 ? "Ya no tienes acceso a esta conversación." : "Revisa la señal e inténtalo de nuevo."}
                </Aviso>
            </div>
        );
    } else if (!c) {
        cuerpo = <Cargando visible texto="Abriendo la conversación…" />;
    } else {
        const elementos = elementosHilo(v.mensajes, grupo, ahora);
        cuerpo = (
            <ol className="flex flex-col gap-2.5 px-3.5 py-3" aria-label={`Mensajes de ${titulo}`} aria-live="polite" aria-relevant="additions">
                {v.cargandoAnteriores ? (
                    <li className="self-center text-xs text-muted-foreground">Cargando mensajes anteriores…</li>
                ) : v.hayAnteriores ? (
                    <li className="self-center">
                        <Boton variante="enlace" className="min-h-12 text-sm" onClick={() => void chat.cargarAnteriores(id)}>
                            Ver mensajes anteriores
                        </Boton>
                    </li>
                ) : null}
                {v.carga === "error" ? (
                    <li className="self-center">
                        <Boton variante="enlace" className="min-h-12 text-sm text-warning" onClick={() => void chat.cargarConversacion(id)}>
                            <TriangleAlert aria-hidden />
                            No se pudo actualizar · Reintentar
                        </Boton>
                    </li>
                ) : null}
                {elementos.length === 0 && v.carga !== "cargando" ? <li className="self-center py-8 text-sm text-muted-foreground">Sin mensajes todavía. Escribe el primero.</li> : null}
                {elementos.map((e) =>
                    e.tipo === "dia" ? (
                        <li key={e.clave} className="self-center rounded-full bg-[var(--campo-marca-suave)] px-3 py-1 text-xs font-semibold text-[var(--campo-marca-suave-texto)]" data-slot="separador-dia">
                            {e.texto}
                        </li>
                    ) : (
                        <Burbuja key={e.clave} m={e.mensaje} conAutor={e.conAutor} alMenu={setMenu} alReintentar={(m) => void reintentar(m)} />
                    ),
                )}
            </ol>
        );
    }

    return (
        <div className="campo-app fixed inset-x-0 top-0 z-20 mx-auto flex w-full max-w-2xl flex-col bg-[var(--campo-lienzo)]" style={{ height: alto ? `${alto}px` : "100dvh" }} data-slot="conversacion-chat">
            <header className="flex items-center gap-2.5 border-b border-border bg-[var(--campo-panel)] px-3.5 pt-[calc(env(safe-area-inset-top)+0.75rem)] pb-3">
                <Boton variante="secundario" aria-label="Volver" className="size-12 shrink-0 rounded-full bg-[var(--campo-panel-2)] p-0 md:size-12" onClick={volver}>
                    <ArrowLeft aria-hidden />
                </Boton>
                <span className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)] gap-0.5">
                    <h1 className="truncate text-base font-semibold">{titulo}</h1>
                    <span className="truncate text-xs text-muted-foreground">
                        {c ? subtituloConversacion(c) : ""}
                        {!estado.enLinea ? " · sin señal" : ""}
                    </span>
                </span>
            </header>
            {otras.length > 0 ? (
                <nav aria-label="Otras conversaciones" className="flex gap-2 overflow-x-auto px-3.5 py-2.5 [scrollbar-width:none]">
                    <span aria-current="page" className="flex h-12 shrink-0 items-center rounded-full bg-[var(--campo-tinta)] px-4 text-[0.8125rem] font-semibold text-[var(--campo-sobre-tinta)]">
                        {c?.tipo === "orden" ? "Esta orden" : titulo}
                    </span>
                    {otras.map((o) => (
                        <a
                            key={o.id}
                            {...enlace(navegar, { tipo: "conversacion", id: o.id })}
                            className="flex h-12 shrink-0 items-center rounded-full border border-border bg-[var(--campo-panel)] px-4 text-[0.8125rem] font-medium whitespace-nowrap focus-visible:outline-2 focus-visible:outline-primary"
                        >
                            {tituloConversacion(o)}
                            {o.no_leidos > 0 ? ` · ${textoContador(o.no_leidos)}` : ""}
                        </a>
                    ))}
                </nav>
            ) : null}
            <div ref={lista} onScroll={alDesplazar} className="min-h-0 flex-1 overflow-y-auto overscroll-contain" data-slot="hilo-chat">
                {cuerpo}
            </div>
            {c && puedeEnviar ? <Compositor conversacionId={id} placeholder={c.tipo === "orden" ? "Escribe a despacho…" : c.tipo === "cuadrilla" ? "Escribe a tu cuadrilla…" : `Escribe a ${tituloConversacion(c)}…`} /> : null}
            <MenuMensaje m={menu} alCerrar={() => setMenu(null)} />
        </div>
    );
}
