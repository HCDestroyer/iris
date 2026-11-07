"use client";

import { Aviso, Boton, cn, inicialesDe, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento } from "@iris/ui";
import type { ConversacionVista } from "@iris/sync/chat";
import { ArrowLeft, Ellipsis, ExternalLink, UserPlus, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { soloLecturaDe, pedirMiembros, type MiembroChat } from "@/lib/chat/api";
import { textoMotivo, tituloChat } from "@/lib/chat/presentacion";
import { AvatarChat, AvisoTiempoReal, FranjaInterno } from "./comunes";
import { CompositorChat } from "./compositor";
import { DialogoAgregarPersona } from "./dialogos";
import { HiloChat } from "./hilo";
import { useChatEquipo, useConversacionChat } from "./proveedor";

export type Miembros = { estado: "cargando" } | { estado: "error" } | { estado: "listo"; datos: MiembroChat[] };

/** Miembros de la conversación (§2.2): roles junto al nombre y candidatos para `@`. */
export function useMiembros(conversacionId: number, version: string): [Miembros, (lista: MiembroChat[]) => void, () => void] {
    const [m, setM] = useState<Miembros>({ estado: "cargando" });
    const [intento, setIntento] = useState(0);
    useEffect(() => {
        const c = new AbortController();
        pedirMiembros(conversacionId, c.signal).then(
            (datos) => setM({ estado: "listo", datos }),
            () => {
                if (!c.signal.aborted) setM((previo) => (previo.estado === "listo" ? previo : { estado: "error" }));
            },
        );
        return () => c.abort();
    }, [conversacionId, version, intento]);
    const fijar = useCallback((datos: MiembroChat[]) => setM({ estado: "listo", datos }), []);
    return [m, fijar, () => setIntento((i) => i + 1)];
}

const lista = (m: Miembros) => (m.estado === "listo" ? m.datos : []);

/** «Mario Ixcot, Laura Méndez y tú». */
function subtitulo(c: ConversacionVista, miembros: readonly MiembroChat[], yoId: number): string {
    if (c.tipo === "directa") return "Mensaje directo";
    const otros = miembros.filter((m) => m.usuario_id !== yoId).map((m) => m.nombre);
    if (otros.length === 0) return c.tipo === "orden" ? "Chat de la orden" : "Chat de la cuadrilla";
    const vistos = otros.slice(0, 3);
    const resto = otros.length - vistos.length;
    return `${vistos.join(", ")}${resto > 0 ? ` y ${resto} más` : ""} y tú`;
}

type Props = {
    id: number;
    /** `pagina` (Equipo), `panel` (lateral, angosto) u `orden` (pestaña de la ficha). */
    variante: "pagina" | "panel" | "orden";
    alAtras?: () => void;
    /** Acciones de la cabecera (Abrir en página, Cerrar…). */
    acciones?: ReactNode;
    className?: string;
};

/** Una conversación del equipo: cabecera, franja «interno», hilo y compositor. */
export function ConversacionEquipo({ id, variante, alAtras, acciones, className }: Props) {
    const chat = useChatEquipo();
    if (!chat) return null;
    return <Conversacion key={id} id={id} variante={variante} {...(alAtras ? { alAtras } : {})} acciones={acciones} {...(className ? { className } : {})} />;
}

function Conversacion({ id, variante, alAtras, acciones, className }: Props) {
    const chat = useChatEquipo();
    if (!chat) throw new Error("ConversacionEquipo fuera del chat");
    const { motor, yoId, eliminando, eliminar, puedeEnviar, puedeSupervisar, enVivo, reintentar, estado } = chat;
    const vista = useConversacionChat(motor, id);
    const c = vista.conversacion;
    const [miembros, fijarMiembros, releerMiembros] = useMiembros(id, c?.ultima_actividad ?? "");
    const [agregando, setAgregando] = useState(false);
    const router = useRouter();

    useEffect(() => {
        void motor.cargarConversacion(id);
    }, [motor, id]);

    if (!c) {
        return (
            <div className={cn("grid min-h-0 flex-1 place-content-center gap-3 p-6 text-center", className)}>
                {vista.carga === "error" ? (
                    <Aviso tono="error" titulo="No se pudo abrir la conversación" accion={<Boton variante="secundario" tamano="sm" onClick={() => void motor.cargarConversacion(id)}>Reintentar</Boton>}>
                        {vista.error?.mensaje ?? "Revisa tu conexión."}
                    </Aviso>
                ) : (
                    <p className="text-sm text-muted-foreground">Abriendo la conversación…</p>
                )}
            </div>
        );
    }

    const titulo = tituloChat(c);
    const soloLectura = soloLecturaDe(c);
    const puedeAgregar = puedeSupervisar && c.tipo === "orden" && !soloLectura;
    const bloqueado = soloLectura
        ? `La orden ${c.titulo} está ${c.orden_estado === "fallida" ? "fallida" : c.orden_estado === "cancelada" ? "cancelada" : "cerrada"}: el chat queda de solo lectura.`
        : !puedeEnviar
          ? "Puedes leer este chat, pero no escribir en él."
          : null;
    const aviso = estado.avisos.at(-1);
    const verOrden = c.tipo === "orden" && c.orden_id !== null && variante !== "orden" ? `/ordenes/${c.orden_id}` : null;

    const cuerpo = (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <FranjaInterno estado={enVivo} texto={variante === "orden" ? "Chat interno de la orden: el cliente no lo ve. El técnico lo tiene en la app de campo." : "Chat interno: el cliente no lo ve"} />
            <AvisoTiempoReal estado={enVivo} reintentar={reintentar} className="mx-3 mt-3 w-auto" />
            {aviso ? (
                <Aviso
                    tono={aviso.tono}
                    titulo={aviso.texto}
                    className="mx-3 mt-3 w-auto"
                    accion={
                        <Boton variante="fantasma" tamano="sm" onClick={() => motor.descartarAviso(aviso.id)}>
                            Entendido
                        </Boton>
                    }
                />
            ) : null}
            <HiloChat motor={motor} vista={vista} miembros={lista(miembros)} yoId={yoId} eliminando={eliminando} puedeEliminar={puedeEnviar} alEliminar={eliminar} />
            <CompositorChat motor={motor} conversacionId={id} tipo={c.tipo} nombre={c.tipo === "directa" ? titulo : c.titulo} miembros={lista(miembros)} yoId={yoId} bloqueado={bloqueado} />
        </div>
    );

    const dialogo = puedeAgregar ? (
        <DialogoAgregarPersona
            abierto={agregando}
            alCerrar={() => setAgregando(false)}
            conversacionId={id}
            titulo={c.titulo}
            miembros={lista(miembros)}
            alAgregar={(l) => {
                fijarMiembros(l);
                releerMiembros();
            }}
        />
    ) : null;

    if (variante === "orden") {
        return (
            <div data-slot="chat-orden" className={cn("grid min-w-0 gap-4 lg:grid-cols-[minmax(0,1fr)_17rem]", className)}>
                <section aria-label={`Chat del equipo de ${c.titulo}`} className="flex h-[min(42rem,calc(100dvh-14rem))] min-h-[26rem] min-w-0 flex-col overflow-hidden rounded-xl border border-border bg-card">
                    {cuerpo}
                </section>
                <aside aria-label="En este chat" className="grid content-start gap-4 rounded-xl border border-border bg-card p-4">
                    <div className="grid gap-2">
                        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">En este chat</h3>
                        <ListaMiembros miembros={miembros} yoId={yoId} tipo={c.tipo} />
                        {puedeAgregar ? (
                            <Boton variante="contorno" tamano="sm" className="justify-self-start" onClick={() => setAgregando(true)}>
                                <UserPlus />
                                Agregar persona
                            </Boton>
                        ) : null}
                    </div>
                    <div className="grid gap-1.5 border-t border-border pt-3 text-sm text-muted-foreground">
                        <h3 className="text-xs font-semibold tracking-wide uppercase">Cómo funciona</h3>
                        <p>Entran solos el técnico asignado y quien despacha. Si se reasigna la orden, el técnico nuevo ve todo el historial.</p>
                        <p>Al cerrar la orden, el chat queda de solo lectura.</p>
                    </div>
                </aside>
                {dialogo}
            </div>
        );
    }

    return (
        <section aria-label={`Conversación: ${titulo}`} className={cn("flex min-h-0 min-w-0 flex-1 flex-col bg-background-secondary", className)}>
            <header className={cn("flex min-w-0 items-center gap-2.5 border-b border-border bg-card", variante === "panel" ? "px-2.5 py-2.5" : "px-4 py-3")}>
                {alAtras ? (
                    <Boton variante="fantasma" tamano="icono-sm" aria-label="Atrás" onClick={alAtras} data-foco-inicial className={variante === "pagina" ? "@min-[700px]/equipo:hidden" : undefined}>
                        <ArrowLeft />
                    </Boton>
                ) : null}
                <AvatarChat c={c} nombre={titulo} className={variante === "panel" ? "size-9" : undefined} />
                <div className="grid min-w-0 flex-1">
                    <h2 className="truncate text-[0.9375rem] font-semibold">{titulo}</h2>
                    <p className="truncate text-xs text-muted-foreground">{subtitulo(c, lista(miembros), yoId)}</p>
                </div>
                {verOrden && variante === "pagina" ? (
                    <Boton asChild variante="contorno" tamano="sm" className="hidden sm:inline-flex">
                        <Link href={verOrden}>
                            <ExternalLink />
                            Ver orden
                        </Link>
                    </Boton>
                ) : null}
                {verOrden || puedeAgregar ? (
                    <MenuDesplegable>
                        <MenuDesplegableDisparador>
                            <Boton variante="fantasma" tamano="icono-sm" aria-label="Más acciones de la conversación">
                                <Ellipsis />
                            </Boton>
                        </MenuDesplegableDisparador>
                        <MenuDesplegableContenido align="end">
                            {puedeAgregar ? (
                                <MenuDesplegableElemento icono={UserPlus} onSelect={() => setAgregando(true)}>
                                    Agregar persona
                                </MenuDesplegableElemento>
                            ) : null}
                            {verOrden ? (
                                <MenuDesplegableElemento icono={ExternalLink} onSelect={() => router.push(verOrden)}>
                                    Ver orden
                                </MenuDesplegableElemento>
                            ) : null}
                        </MenuDesplegableContenido>
                    </MenuDesplegable>
                ) : null}
                {acciones}
            </header>
            {cuerpo}
            {dialogo}
        </section>
    );
}

function ListaMiembros({ miembros, yoId, tipo }: { miembros: Miembros; yoId: number; tipo: ConversacionVista["tipo"] }) {
    if (miembros.estado === "cargando") return <p className="text-sm text-muted-foreground">Cargando…</p>;
    if (miembros.estado === "error") return <p className="text-sm text-muted-foreground">No se pudo leer quién está en el chat.</p>;
    return (
        <ul className="grid gap-2">
            {miembros.datos.map((m) => (
                <li key={m.usuario_id} className="flex min-w-0 items-center gap-2.5">
                    <span aria-hidden className="grid size-8 shrink-0 place-content-center rounded-full bg-elevated text-xs font-semibold">
                        {inicialesDe(m.nombre)}
                    </span>
                    <span className="grid min-w-0">
                        <span className="truncate text-sm font-medium">
                            {m.nombre}
                            {m.usuario_id === yoId ? " (tú)" : ""}
                        </span>
                        {textoMotivo(m.motivo, tipo) ? <span className="truncate text-xs text-muted-foreground">{textoMotivo(m.motivo, tipo)}</span> : null}
                    </span>
                </li>
            ))}
        </ul>
    );
}

/** Botón de cerrar del panel lateral. */
export function BotonCerrarPanel({ alCerrar }: { alCerrar: () => void }) {
    return (
        <Boton variante="fantasma" tamano="icono-sm" aria-label="Cerrar el chat del equipo" onClick={alCerrar}>
            <X />
        </Boton>
    );
}
