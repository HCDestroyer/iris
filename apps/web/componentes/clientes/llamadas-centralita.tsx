"use client";

import { Boton, Esqueleto, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { PhoneIncoming, PhoneMissed, PhoneOutgoing } from "lucide-react";
import { useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { listarLlamadasCentralita, type FiltroLlamadas } from "@/lib/centralita/api";
import { lineaLlamada } from "@/lib/centralita/presentacion";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import type { Llamada } from "@/lib/voz/tipos";

/*
 * Las llamadas de la centralita en la ficha del cliente y en el ticket (E4a, contratos/voz.md §3.12):
 * quién contestó, cuánto duró, en qué extensión y su ticket. Solo con `llamadas.ver`.
 */

const ICONO = { entrante: PhoneIncoming, saliente: PhoneOutgoing, perdida: PhoneMissed } as const;
const COLOR = { entrante: "text-success", saliente: "text-muted-foreground", perdida: "text-error" } as const;

function Lista(props: { filtro: FiltroLlamadas; titulo: string; vacio: string }) {
    const [todas, setTodas] = useState(false);
    return <ListaLeida key={todas ? "todas" : "pocas"} {...props} todas={todas} verMas={() => setTodas(true)} />;
}

function ListaLeida({ filtro, titulo, vacio, todas, verMas }: { filtro: FiltroLlamadas; titulo: string; vacio: string; todas: boolean; verMas: () => void }) {
    const limite = todas ? 50 : 5;
    const lectura = useLectura((signal) => listarLlamadasCentralita({ ...filtro, limit: limite }, signal));
    const datos: Llamada[] = lectura.estado === "listo" ? (lectura.datos.data ?? []).filter((l) => l.origen === "centralita") : [];
    return (
        <Tarjeta data-slot="llamadas-centralita">
            <TarjetaEncabezado>
                <TarjetaTitulo className="flex items-center gap-2">
                    <PhoneIncoming aria-hidden className="size-4 text-muted-foreground" />
                    {titulo}
                </TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-2">
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-16" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las llamadas" />
                ) : datos.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{vacio}</p>
                ) : (
                    <ul className="grid divide-y divide-border" aria-label={titulo}>
                        {datos.map((l) => {
                            const x = lineaLlamada(l);
                            const Icono = ICONO[x.tipo];
                            return (
                                <li key={l.id} className="flex min-w-0 items-start gap-2.5 py-2" data-llamada={l.id} data-tipo={x.tipo}>
                                    <Icono aria-hidden className={`mt-0.5 size-4 shrink-0 ${COLOR[x.tipo]}`} />
                                    <div className="grid min-w-0 flex-1 gap-0.5">
                                        <span className="text-sm font-medium">{x.titulo}</span>
                                        <span className="text-xs text-muted-foreground">{x.detalle}</span>
                                        {l.nota ? <span className="text-xs whitespace-pre-line">«{l.nota}»</span> : null}
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                )}
                {lectura.estado === "listo" && !todas && lectura.datos.nextCursor ? (
                    <Boton variante="enlace" tamano="sm" className="justify-self-start px-0" onClick={verMas}>
                        Ver más
                    </Boton>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

/** En la ficha del cliente. */
export function LlamadasDelCliente({ clienteId }: { clienteId: number }) {
    const { puede } = useSesion();
    if (!puede(["llamadas.ver"])) return null;
    return <Lista key={`c${clienteId}`} filtro={{ clienteId }} titulo="Llamadas de la centralita" vacio="Sin llamadas de la centralita." />;
}

/** En el historial del ticket. */
export function LlamadasDelTicket({ ticketId }: { ticketId: number }) {
    const { puede } = useSesion();
    if (!puede(["llamadas.ver"])) return null;
    return <Lista key={`t${ticketId}`} filtro={{ ticketId }} titulo="Llamadas de este ticket" vacio="Ninguna llamada de la centralita agregada a este ticket." />;
}
