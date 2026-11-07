"use client";

import { Boton, cn, Globo, PanelLateral } from "@iris/ui";
import { textoContador } from "@iris/sync/chat";
import { Maximize2, MessageCircle, SquarePen } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import type { FiltroChat } from "@/lib/chat/presentacion";
import { IndicadorEnVivo } from "./comunes";
import { BotonCerrarPanel, ConversacionEquipo } from "./conversacion";
import { DialogoNuevoMensaje } from "./dialogos";
import { ListaChat } from "./lista";
import { useChatEquipo } from "./proveedor";

const ID_PANEL = "panel-chat-equipo";

/**
 * Ícono del chat del equipo en la barra superior, en todas las pantallas del panel. Número = lo que
 * pide actuar (directas sin leer y menciones, `GET /resumen`); un punto si solo hay mensajes nuevos.
 */
export function BotonChatEquipo() {
    const chat = useChatEquipo();
    const ruta = usePathname();
    if (!chat) return null;
    const n = chat.contadores.todas;
    const etiqueta = n > 0 ? `Chat del equipo: ${n === 1 ? "1 pide" : `${n} piden`} tu respuesta` : chat.punto ? "Chat del equipo: mensajes nuevos" : "Chat del equipo";
    // En la página Equipo el panel no hace falta: el ícono lleva a la misma.
    const enEquipo = ruta === "/equipo";
    return (
        <Globo texto="Chat del equipo">
            <Boton
                variante={chat.panel.abierto ? "contorno" : "secundario"}
                tamano="icono-sm"
                aria-label={etiqueta}
                aria-expanded={enEquipo ? undefined : chat.panel.abierto}
                aria-controls={chat.panel.abierto ? ID_PANEL : undefined}
                data-slot="boton-chat-equipo"
                className={cn("relative size-9 rounded-lg", chat.panel.abierto && "border-primary text-primary")}
                onClick={() => (chat.panel.abierto ? chat.cerrarPanel() : chat.abrirPanel())}
            >
                <MessageCircle />
                {n > 0 ? (
                    <span aria-hidden data-slot="insignia-chat-equipo" className="absolute -top-1.5 -right-1.5 min-w-[1.125rem] rounded-full bg-primary px-1 text-center text-[0.625rem] leading-[1.125rem] font-bold text-primary-foreground ring-2 ring-background tabular-nums">
                        {textoContador(n)}
                    </span>
                ) : chat.punto ? (
                    <span aria-hidden data-slot="punto-chat-equipo" className="absolute -top-0.5 -right-0.5 size-2.5 rounded-full bg-primary ring-2 ring-background" />
                ) : null}
            </Boton>
        </Globo>
    );
}

/** Panel lateral del chat (sin telón): la lista o una conversación, con «Atrás». */
export function PanelChatEquipo() {
    const chat = useChatEquipo();
    const router = useRouter();
    const ruta = usePathname();
    const [filtro, setFiltro] = useState<FiltroChat>("todas");
    const [busqueda, setBusqueda] = useState("");
    const [nuevo, setNuevo] = useState(false);

    // En la página Equipo el panel se cierra solo (la misma conversación ya está ahí).
    const cerrar = chat?.cerrarPanel;
    useEffect(() => {
        if (ruta === "/equipo") cerrar?.();
    }, [ruta, cerrar]);

    if (!chat) return null;
    const { panel } = chat;
    const abrirEnPagina = () => {
        chat.cerrarPanel();
        router.push(panel.conversacion !== null ? `/equipo?c=${panel.conversacion}` : "/equipo");
    };
    const botonPagina = (
        <Globo texto="Abrir en página">
            <Boton variante="fantasma" tamano="icono-sm" onClick={abrirEnPagina} aria-label="Abrir en página">
                <Maximize2 />
            </Boton>
        </Globo>
    );

    return (
        <>
            <PanelLateral id={ID_PANEL} abierto={panel.abierto} alCerrar={chat.cerrarPanel} etiqueta="Chat del equipo">
                {panel.conversacion === null ? (
                    <ListaChat
                        className="min-h-0 flex-1"
                        seleccionada={null}
                        filtro={filtro}
                        alFiltrar={setFiltro}
                        busqueda={busqueda}
                        alBuscar={setBusqueda}
                        alElegir={(id) => chat.elegirEnPanel(id)}
                        encabezado={
                            <div className="flex min-w-0 items-center gap-1.5">
                                <h2 className="text-lg font-bold" data-foco-inicial tabIndex={-1}>
                                    Equipo
                                </h2>
                                <IndicadorEnVivo estado={chat.enVivo} className="ml-1" />
                                <span className="flex-1" />
                                {chat.puedeEnviar ? (
                                    <Boton variante="fantasma" tamano="icono-sm" aria-label="Nuevo mensaje" onClick={() => setNuevo(true)}>
                                        <SquarePen />
                                    </Boton>
                                ) : null}
                                {botonPagina}
                                <BotonCerrarPanel alCerrar={chat.cerrarPanel} />
                            </div>
                        }
                    />
                ) : (
                    <ConversacionEquipo
                        id={panel.conversacion}
                        variante="panel"
                        alAtras={() => chat.elegirEnPanel(null)}
                        acciones={
                            <>
                                {botonPagina}
                                <BotonCerrarPanel alCerrar={chat.cerrarPanel} />
                            </>
                        }
                    />
                )}
            </PanelLateral>
            <DialogoNuevoMensaje abierto={nuevo} alCerrar={() => setNuevo(false)} alAbrir={(id) => {
                setNuevo(false);
                chat.elegirEnPanel(id);
            }} />
        </>
    );
}
