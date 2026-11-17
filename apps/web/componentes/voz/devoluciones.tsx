"use client";

import { avisar, Aviso, Boton, Campo, cn, Dialogo, DialogoContenido, DialogoPie } from "@iris/ui";
import { ChevronDown, MessageSquare, PhoneMissed, PhoneOutgoing, RotateCw } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { createContext, use, useEffect, useMemo, useState, type ReactNode } from "react";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import type { Conversacion } from "@/lib/conversaciones/tipos";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura, type Lectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { descartarDevolucion, leerDevoluciones } from "@/lib/voz/api";
import { escucharEventos } from "@/lib/voz/canal";
import { PERMISO_LLAMAR, type DevolucionLlamada } from "@/lib/voz/tipos";
import { useVoz } from "./proveedor";

/*
 * Devoluciones pendientes (contratos/voz.md §2.9): las llamadas perdidas que alguien tiene que
 * atender: se devuelve la llamada (E1b; quien puede llamar), se le escribe al cliente o se descarta
 * con un motivo. Si el cliente contesta la devolución, el servidor la da por hecha.
 */

type ContextoDevoluciones = {
    lectura: Lectura<DevolucionLlamada[]>;
    /** La devolución pendiente de una conversación, si la lectura salió bien y hay una. */
    deConversacion: (id: number) => DevolucionLlamada | undefined;
    deLlamada: (id: string) => DevolucionLlamada | undefined;
    puedeDescartar: boolean;
    descartar: (d: DevolucionLlamada) => void;
};

const Contexto = createContext<ContextoDevoluciones | null>(null);

/** Las devoluciones pendientes, o `null` si esta persona no ve llamadas. */
export function useDevoluciones(): ContextoDevoluciones | null {
    return use(Contexto);
}

export function ProveedorDevoluciones({ children }: { children: ReactNode }) {
    const { puede } = useSesion();
    if (!puede(["llamadas.ver"])) return children;
    return <Proveedor>{children}</Proveedor>;
}

function Proveedor({ children }: { children: ReactNode }) {
    const { puede } = useSesion();
    const lectura = useLectura(async (signal) => (await leerDevoluciones(signal)).data ?? []);
    const { refrescar, reemplazar } = lectura;
    const [descartando, setDescartando] = useState<DevolucionLlamada | null>(null);
    const datos = lectura.estado === "listo" ? lectura.datos : null;

    // Una llamada que se pierde, o una devolución que alguien cerró, cambia la lista.
    useEffect(
        () =>
            escucharEventos((evento) => {
                if (evento.tipo === "devolucion_actualizada" || evento.tipo === "llamada_terminada") void refrescar();
            }),
        [refrescar],
    );

    const valor = useMemo<ContextoDevoluciones>(
        () => ({
            lectura,
            deConversacion: (id) => datos?.find((d) => d.conversacionId === id),
            deLlamada: (id) => datos?.find((d) => d.llamadaId === id),
            puedeDescartar: puede(["llamadas.descartar_devolucion"]),
            descartar: setDescartando,
        }),
        [lectura, datos, puede],
    );
    return (
        <Contexto value={valor}>
            {children}
            {descartando ? (
                <DialogoDescartar
                    key={descartando.id}
                    devolucion={descartando}
                    cerrar={() => setDescartando(null)}
                    alHecho={(id) => {
                        if (datos) reemplazar(datos.filter((d) => d.id !== id));
                        void refrescar();
                    }}
                />
            ) : null}
        </Contexto>
    );
}

function DialogoDescartar({ devolucion, cerrar, alHecho }: { devolucion: DevolucionLlamada; cerrar: () => void; alHecho: (id: number) => void }) {
    const clave = useClaveIdempotencia();
    const [motivo, setMotivo] = useState("");
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const texto = motivo.trim();
    const errorMotivo = error instanceof ErrorApi ? error.errorDeCampo("motivo") : undefined;

    async function enviar() {
        if (!texto) return;
        setEnviando(true);
        setError(null);
        try {
            await descartarDevolucion(devolucion.id, texto, clave.actual());
            avisar({ titulo: "Llamada por devolver descartada" });
            alHecho(devolucion.id);
            cerrar();
        } catch (e) {
            // Otra persona ya la cerró (la devolvió o la descartó): no hay nada más que hacer.
            if (e instanceof ErrorApi && e.codigo === "devolucion_cerrada") {
                avisar({ tono: "info", titulo: "Ya estaba cerrada", descripcion: "Otra persona la atendió o la descartó." });
                alHecho(devolucion.id);
                cerrar();
                return;
            }
            clave.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }
    return (
        <Dialogo open onOpenChange={(a) => !a && !enviando && cerrar()}>
            <DialogoContenido titulo="Descartar la llamada por devolver" descripcion="Deja de aparecer como pendiente. Queda registrado quién la descartó y por qué.">
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={(e) => {
                        e.preventDefault();
                        void enviar();
                    }}
                >
                    <Campo etiqueta="Motivo" placeholder="Ya le escribí, número equivocado…" maxLength={200} autoComplete="off" value={motivo} onChange={(e) => setMotivo(e.target.value)} error={errorMotivo} autoFocus />
                    {error && !errorMotivo ? (
                        <Aviso tono="error" titulo="No se pudo descartar">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <Boton type="button" variante="contorno" disabled={enviando} onClick={cerrar}>
                            Cancelar
                        </Boton>
                        <Boton type="submit" variante="peligro" disabled={enviando || !texto}>
                            {enviando ? "Descartando…" : "Descartar"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}

/** «Llamada perdida» en la fila de la lista: el punto que crece porque pide actuar. */
export function ChipPerdida({ conversacionId }: { conversacionId: number }) {
    const d = useDevoluciones()?.deConversacion(conversacionId);
    if (!d) return null;
    return (
        <span data-slot="chip-perdida" className="col-span-2 col-start-2 inline-flex w-fit max-w-full items-center gap-1 rounded-md border border-error/40 bg-error/10 px-1.5 py-0.5 text-[0.71875rem] font-semibold text-error">
            <PhoneMissed aria-hidden className="size-3 shrink-0" />
            <span className="truncate">Llamada perdida · escríbele</span>
        </span>
    );
}

/**
 * Arriba de la lista: cuántas llamadas hay por devolver. Plegado es una línea; abierto, cada una
 * con «Escribirle» y «Descartar». Si la lectura falló, lo dice con «Reintentar» (nunca un cero).
 */
export function PendientesDevolver({ conversaciones, hrefDe, ahora }: { conversaciones: readonly Conversacion[]; hrefDe: (id: number) => string; ahora: number }) {
    const dev = useDevoluciones();
    const voz = useVoz();
    const router = useRouter();
    const { puede } = useSesion();
    const [abierto, setAbierto] = useState(false);
    if (!dev) return null;
    const puedeLlamar = voz !== null && puede([PERMISO_LLAMAR]);
    const vozOcupada = voz !== null && voz.estado.fase !== "libre" && voz.estado.fase !== "aviso";
    const { lectura } = dev;
    if (lectura.estado === "cargando") return null;
    if (lectura.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudieron leer las llamadas por devolver"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(lectura.error).titulo}
            </Aviso>
        );
    }
    const pendientes = lectura.datos;
    if (pendientes.length === 0) return null;
    const idPanel = "llamadas-por-devolver";
    return (
        <section aria-label="Llamadas por devolver" data-slot="por-devolver" className="grid gap-1.5 rounded-lg border border-error/30 bg-error/[0.06] p-1.5">
            <Boton variante="fantasma" tamano="sm" className="h-8 w-full justify-start gap-2 px-2 text-[0.8125rem] font-semibold text-error" aria-expanded={abierto} aria-controls={idPanel} onClick={() => setAbierto((a) => !a)}>
                <PhoneMissed />
                <span className="min-w-0 flex-1 truncate text-left">{pendientes.length === 1 ? "1 llamada por devolver" : `${pendientes.length} llamadas por devolver`}</span>
                <ChevronDown className={cn("transition-transform", abierto && "rotate-180")} />
            </Boton>
            {abierto ? (
                <ul id={idPanel} className="grid gap-1">
                    {pendientes.map((d) => {
                        const conv = conversaciones.find((c) => c.id === d.conversacionId);
                        return (
                            <li key={d.id} data-devolucion={d.id} className="grid gap-1.5 rounded-md bg-card p-2">
                                <div className="grid min-w-0">
                                    <span className="truncate text-[0.8125rem] font-semibold">{d.titulo || conv?.titulo || d.numero}</span>
                                    <span className="truncate text-xs text-muted-foreground">
                                        Llamó {haceCuanto(d.creadaEn, ahora)}
                                        {d.pedidaPor === "cliente" ? " · pidió que lo llamen" : d.pedidaPor === "asistente" ? " · lo anotó el asistente" : ""}
                                    </span>
                                </div>
                                <div className="flex flex-wrap gap-1.5">
                                    {puedeLlamar ? (
                                        <Boton
                                            tamano="sm"
                                            className="h-7 px-2 text-xs"
                                            disabled={vozOcupada}
                                            onClick={() => {
                                                voz.llamar({ id: d.conversacionId, titulo: d.titulo || conv?.titulo || d.numero }, { devolucionId: d.id });
                                                // La ficha del cliente a la vista mientras suena.
                                                router.push(hrefDe(d.conversacionId) as Route, { scroll: false });
                                            }}
                                        >
                                            <PhoneOutgoing />
                                            Devolver la llamada
                                        </Boton>
                                    ) : null}
                                    <Boton asChild variante="secundario" tamano="sm" className="h-7 px-2 text-xs">
                                        <Link href={hrefDe(d.conversacionId)} scroll={false}>
                                            <MessageSquare />
                                            Escribirle
                                        </Link>
                                    </Boton>
                                    {dev.puedeDescartar ? (
                                        <Boton variante="fantasma" tamano="sm" className="h-7 px-2 text-xs" onClick={() => dev.descartar(d)}>
                                            Descartar
                                        </Boton>
                                    ) : null}
                                </div>
                            </li>
                        );
                    })}
                </ul>
            ) : null}
        </section>
    );
}
