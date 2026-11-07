"use client";

import { Boton, cn, Esqueleto, Globo } from "@iris/ui";
import { MessageCircle, SquarePen } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { IndicadorEnVivo } from "@/componentes/chat/comunes";
import { ConversacionEquipo } from "@/componentes/chat/conversacion";
import { DialogoNuevoMensaje } from "@/componentes/chat/dialogos";
import { ListaChat } from "@/componentes/chat/lista";
import { useChatEquipo } from "@/componentes/chat/proveedor";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { filtroDeParametro, type FiltroChat } from "@/lib/chat/presentacion";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

/** URL de la página con su filtro y conversación (`/equipo?filtro=ordenes&c=12`). */
function urlEquipo(ruta: string, filtro: FiltroChat, c: number | null): string {
    const q = new URLSearchParams();
    if (filtro !== "todas") q.set("filtro", filtro);
    if (c !== null) q.set("c", String(c));
    const t = q.toString();
    return t ? `${ruta}?${t}` : ruta;
}

/** «Equipo»: el chat interno a página completa (lista con filtros y la conversación). */
export function PantallaEquipo() {
    const { puede } = useSesion();
    if (!puede(["chat.ver"])) {
        return (
            <div className="p-4 sm:p-6">
                <RequierePermiso permisos={["chat.ver"]}>{null}</RequierePermiso>
            </div>
        );
    }
    return <Equipo />;
}

function Equipo() {
    const chat = useChatEquipo();
    const params = useSearchParams();
    const ruta = usePathname();
    const router = useRouter();
    const filtro = filtroDeParametro(params.get("filtro"));
    const abierta = idDeParametro(params.get("c"));
    const [busqueda, setBusqueda] = useState("");
    const [nuevo, setNuevo] = useState(false);
    const vista = abierta === null ? "lista" : "conversacion";

    if (!chat) {
        return (
            <div data-slot="equipo" data-vista="lista" aria-busy="true" className="grid gap-3 p-4">
                <span className="sr-only" role="status">
                    Cargando el chat del equipo…
                </span>
                <Esqueleto className="h-8 w-40" />
                <Esqueleto className="h-40" />
            </div>
        );
    }

    return (
        <div data-slot="equipo" data-vista={vista} className="@container/equipo min-h-0 overflow-hidden bg-background-secondary">
            <div className="grid h-full min-h-0 @min-[700px]/equipo:grid-cols-[minmax(17rem,22rem)_minmax(0,1fr)]">
                <ListaChat
                    className={cn("border-r border-border", vista === "lista" ? "flex" : "hidden @min-[700px]/equipo:flex")}
                    seleccionada={abierta}
                    filtro={filtro}
                    alFiltrar={(f) => router.replace(urlEquipo(ruta, f, abierta), { scroll: false })}
                    busqueda={busqueda}
                    alBuscar={setBusqueda}
                    hrefDe={(id) => urlEquipo(ruta, filtro, id)}
                    encabezado={
                        <div className="flex min-w-0 items-center gap-2">
                            <h1 className="text-xl font-bold">Equipo</h1>
                            <IndicadorEnVivo estado={chat.enVivo} />
                            <span className="flex-1" />
                            {chat.puedeEnviar ? (
                                <Globo texto="Nuevo mensaje">
                                    <Boton variante="contorno" tamano="icono-sm" aria-label="Nuevo mensaje" onClick={() => setNuevo(true)}>
                                        <SquarePen />
                                    </Boton>
                                </Globo>
                            ) : null}
                        </div>
                    }
                />
                {abierta === null ? (
                    <section aria-label="Conversación" className="hidden min-h-0 items-center justify-center p-6 text-center @min-[700px]/equipo:flex">
                        <div className="grid max-w-xs justify-items-center gap-2">
                            <MessageCircle aria-hidden className="size-8 text-muted-foreground" />
                            <p className="font-medium">Elige una conversación</p>
                            <p className="text-sm text-muted-foreground">Chats directos, de cuadrilla y de órdenes con tu equipo. El cliente no los ve.</p>
                        </div>
                    </section>
                ) : (
                    <ConversacionEquipo
                        id={abierta}
                        variante="pagina"
                        className="min-h-0"
                        // En el teléfono la lista y la conversación se turnan: «Atrás» vuelve a la lista.
                        alAtras={() => router.push(urlEquipo(ruta, filtro, null))}
                    />
                )}
            </div>
            <DialogoNuevoMensaje
                abierto={nuevo}
                alCerrar={() => setNuevo(false)}
                alAbrir={(id) => {
                    setNuevo(false);
                    router.push(urlEquipo(ruta, filtro, id));
                }}
            />
        </div>
    );
}
