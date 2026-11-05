"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import {
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    CampoSelector,
    Casilla,
    cn,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Esqueleto,
    EstadoVacio,
    useCambiosSinGuardar,
} from "@iris/ui";
import { ChevronDown, Inbox, RotateCw, UserRoundPlus, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nombreCategoria, nombreUsuario, type Categoria, type Usuario } from "@/componentes/tickets/catalogos";
import { InsigniaEstadoTicket, InsigniaPrioridad, SlaVisible, useAhora } from "@/componentes/tickets/insignias";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { esquemaAsignar } from "@/lib/tickets/esquemas";
import type { ConsultaTickets } from "@/lib/tickets/filtros";
import { enLote, textoLoteAsignado, type ResultadoLote } from "@/lib/tickets/lote";
import { relojPrincipal, type Ticket } from "@/lib/tickets/sla";

const TAMANO_PAGINA = 25;

function leerPagina(consulta: ConsultaTickets, cursor: string | undefined, signal?: AbortSignal): Promise<Schema<"PaginaTicket">> {
    return resolver(
        api().GET("/api/v1/tickets", {
            params: { query: { ...consulta, limit: TAMANO_PAGINA, ...(cursor ? { cursor } : {}) } },
            ...(signal ? { signal } : {}),
        }),
    );
}

type Props = {
    consulta: ConsultaTickets;
    categorias: readonly Categoria[] | undefined;
    usuarios: readonly Usuario[] | null | undefined;
    yo: { id: number; nombre: string };
    hayFiltros: boolean;
    /** Una acción en lote cambió tickets (para refrescar los contadores). */
    alCambiar?: () => void;
};

/**
 * Lista de la bandeja a todo el ancho, con cursor y selección múltiple: con `tickets.asignar`, cada
 * fila tiene su casilla y al elegir alguna aparece la barra de acciones («Asignar…»). Se remonta
 * (key) al cambiar los filtros.
 */
export function ListaTickets({ consulta, categorias, usuarios, yo, hayFiltros, alCambiar }: Props) {
    const { puede } = useSesion();
    const primera = useLectura((signal) => leerPagina(consulta, undefined, signal));
    const [extra, setExtra] = useState<{ tickets: Ticket[]; cursor: string | null } | null>(null);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const [elegidos, setElegidos] = useState<ReadonlySet<number>>(new Set());
    const [asignando, setAsignando] = useState(false);
    const [fallidos, setFallidos] = useState<ResultadoLote["fallidos"]>([]);
    const ahora = useAhora();
    const seleccionable = puede(["tickets.asignar"]);

    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-3 p-4" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando tickets…
                </span>
                {Array.from({ length: 5 }, (_, i) => (
                    <Esqueleto key={i} className="h-11" />
                ))}
            </div>
        );
    }
    if (primera.estado === "error") {
        return (
            <div className="p-4">
                <ErrorLectura
                    error={primera.error}
                    que="los tickets"
                    reintentar={() => {
                        setExtra(null);
                        primera.reintentar();
                    }}
                />
            </div>
        );
    }

    const tickets = [...(primera.datos.data ?? []), ...(extra?.tickets ?? [])];
    const siguiente = extra ? extra.cursor : primera.datos.nextCursor;
    const asignables = tickets.filter((t) => t.estado !== "cerrado");
    const seleccion = asignables.filter((t) => elegidos.has(t.id));

    async function cargarMas() {
        if (!siguiente) {
            return;
        }
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const pagina = await leerPagina(consulta, siguiente);
            setExtra((prev) => ({ tickets: [...(prev?.tickets ?? []), ...(pagina.data ?? [])], cursor: pagina.nextCursor }));
        } catch (error) {
            setErrorMas(error);
        } finally {
            setCargandoMas(false);
        }
    }

    function alternar(id: number, marcar: boolean) {
        setElegidos((prev) => {
            const nuevo = new Set(prev);
            if (marcar) nuevo.add(id);
            else nuevo.delete(id);
            return nuevo;
        });
    }

    /** Reemplaza en la lista los tickets que devolvió la API. */
    function reemplazar(nuevos: readonly Ticket[]) {
        const porId = new Map(nuevos.map((t) => [t.id, t]));
        const pagina = primera.estado === "listo" ? primera.datos : null;
        if (pagina) primera.reemplazar({ ...pagina, data: (pagina.data ?? []).map((t) => porId.get(t.id) ?? t) });
        setExtra((prev) => (prev ? { ...prev, tickets: prev.tickets.map((t) => porId.get(t.id) ?? t) } : prev));
    }

    if (tickets.length === 0) {
        return (
            <EstadoVacio
                icono={Inbox}
                titulo={hayFiltros ? "Ningún ticket coincide con los filtros" : "No hay tickets"}
                descripcion={hayFiltros ? "Prueba con otra vista o quita algún filtro." : "Cuando entren tickets aparecerán aquí."}
            />
        );
    }

    const asignado = (t: Ticket) => nombreUsuario(usuarios, t.asignadoA, yo);
    const todos = asignables.length > 0 && seleccion.length === asignables.length;

    return (
        <>
            {seleccion.length > 0 ? (
                <div
                    role="toolbar"
                    aria-label="Acciones con la selección"
                    className="flex flex-wrap items-center gap-2 border-b border-border bg-primary/5 px-3 py-2 text-[0.8125rem]"
                    data-testid="barra-seleccion"
                >
                    <span className="mr-auto font-semibold tabular-nums" aria-live="polite">
                        {seleccion.length === 1 ? "1 ticket seleccionado" : `${seleccion.length} tickets seleccionados`}
                    </span>
                    <Boton tamano="sm" className="rounded-lg" onClick={() => setAsignando(true)}>
                        <UserRoundPlus />
                        Asignar…
                    </Boton>
                    <Boton variante="fantasma" tamano="sm" onClick={() => setElegidos(new Set())}>
                        <X />
                        Quitar selección
                    </Boton>
                </div>
            ) : null}
            {fallidos.length > 0 ? (
                <div className="border-b border-border p-3">
                    <Aviso
                        tono="error"
                        titulo={fallidos.length === 1 ? "1 ticket no se pudo asignar" : `${fallidos.length} tickets no se pudieron asignar`}
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={() => setFallidos([])}>
                                Entendido
                            </Boton>
                        }
                    >
                        <ul className="grid gap-0.5">
                            {fallidos.map((f) => (
                                <li key={f.ticket.id}>
                                    {f.ticket.numero}: {f.motivo}
                                </li>
                            ))}
                        </ul>
                    </Aviso>
                </div>
            ) : null}

            {/* Teléfono: tarjetas. */}
            <ul className="divide-y divide-border md:hidden" aria-label="Tickets">
                {tickets.map((t) => {
                    const { tipo, reloj } = relojPrincipal(t);
                    return (
                        <li key={t.id} data-ticket={t.numero} className={cn("flex items-start gap-1", elegidos.has(t.id) && "bg-primary/5")}>
                            {seleccionable && t.estado !== "cerrado" ? (
                                <Casilla
                                    className="shrink-0 pt-4 pl-3"
                                    etiqueta={<span className="sr-only">Seleccionar {t.numero}</span>}
                                    checked={elegidos.has(t.id)}
                                    onChange={(e) => alternar(t.id, e.target.checked)}
                                />
                            ) : null}
                            <Link href={`/tickets/${t.id}`} className="grid min-w-0 flex-1 gap-2 p-4 pl-3 transition-colors hover:bg-background-secondary/60">
                                <span className="flex items-baseline justify-between gap-2">
                                    <span className="font-mono text-xs text-muted-foreground">{t.numero}</span>
                                    <span className="text-xs text-muted-foreground">{haceCuanto(t.actualizadoEn, ahora)}</span>
                                </span>
                                <span className="line-clamp-2 text-sm font-semibold">{t.asunto}</span>
                                <span className="flex flex-wrap items-center gap-1.5">
                                    <InsigniaEstadoTicket estado={t.estado} />
                                    <InsigniaPrioridad prioridad={t.prioridad} />
                                </span>
                                <SlaVisible reloj={reloj} tipo={tipo} ahora={ahora} compacto />
                                <span className="truncate text-[0.8125rem] text-muted-foreground">
                                    {[t.contacto.nombre, asignado(t) ?? "Sin asignar"].filter(Boolean).join(" · ")}
                                </span>
                            </Link>
                        </li>
                    );
                })}
            </ul>

            {/* Escritorio: tabla a todo el ancho. */}
            <div className="hidden overflow-x-auto md:block">
                <table aria-label="Tickets" className="w-full min-w-[46rem] border-collapse text-[0.8125rem]">
                    <thead>
                        <tr className="bg-background-secondary text-left text-xs font-semibold text-muted-foreground">
                            {seleccionable ? (
                                <th scope="col" className="w-10 py-2 pl-3">
                                    <Casilla
                                        etiqueta={<span className="sr-only">Seleccionar todos los tickets</span>}
                                        checked={todos}
                                        disabled={asignables.length === 0}
                                        onChange={(e) => setElegidos(e.target.checked ? new Set(asignables.map((t) => t.id)) : new Set())}
                                    />
                                </th>
                            ) : null}
                            <th scope="col" className={cn("py-2 pr-2", seleccionable ? "pl-1" : "pl-3")}>
                                Ticket
                            </th>
                            <th scope="col" className="px-2 py-2">
                                Cliente
                            </th>
                            <th scope="col" className="px-2 py-2">
                                Estado
                            </th>
                            <th scope="col" className="px-2 py-2">
                                Prioridad
                            </th>
                            <th scope="col" className="px-2 py-2">
                                SLA
                            </th>
                            <th scope="col" className="py-2 pr-3 pl-2">
                                Responsable
                            </th>
                        </tr>
                    </thead>
                    <tbody>
                        {tickets.map((t) => {
                            const { tipo, reloj } = relojPrincipal(t);
                            const marcado = elegidos.has(t.id);
                            return (
                                <tr
                                    key={t.id}
                                    data-ticket={t.numero}
                                    aria-selected={seleccionable ? marcado : undefined}
                                    className={cn(
                                        "border-t border-border align-middle transition-colors hover:bg-background-secondary/60",
                                        marcado && "bg-primary/5 shadow-[inset_3px_0_0_var(--hx-primary)]",
                                    )}
                                >
                                    {seleccionable ? (
                                        <td className="py-2.5 pl-3">
                                            {t.estado !== "cerrado" ? (
                                                <Casilla
                                                    etiqueta={<span className="sr-only">Seleccionar {t.numero}</span>}
                                                    checked={marcado}
                                                    onChange={(e) => alternar(t.id, e.target.checked)}
                                                />
                                            ) : null}
                                        </td>
                                    ) : null}
                                    <td className={cn("max-w-[22rem] py-2.5 pr-2", seleccionable ? "pl-1" : "pl-3")}>
                                        <Link href={`/tickets/${t.id}`} className="grid min-w-0 gap-0.5 rounded-sm hover:text-primary">
                                            <span className="truncate font-semibold">{t.asunto}</span>
                                            <span className="truncate font-mono text-[0.6875rem] text-muted-foreground">
                                                {t.numero}
                                                {t.categoriaId !== null ? (
                                                    <span className="font-sans"> · {nombreCategoria(categorias, t.categoriaId)}</span>
                                                ) : null}
                                            </span>
                                        </Link>
                                    </td>
                                    <td className="max-w-[11rem] truncate px-2 py-2.5">
                                        {t.contacto.nombre ?? <span className="text-muted-foreground">—</span>}
                                    </td>
                                    <td className="px-2 py-2.5">
                                        <InsigniaEstadoTicket estado={t.estado} />
                                    </td>
                                    <td className="px-2 py-2.5">
                                        <InsigniaPrioridad prioridad={t.prioridad} />
                                    </td>
                                    <td className="px-2 py-2.5">
                                        <SlaVisible reloj={reloj} tipo={tipo} ahora={ahora} compacto />
                                    </td>
                                    <td className="max-w-[12rem] truncate py-2.5 pr-3 pl-2 text-muted-foreground">{asignado(t) ?? "Sin asignar"}</td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table>
            </div>

            <div className="grid justify-items-center gap-3 border-t border-border p-3">
                {errorMas ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudo cargar la página siguiente"
                        className="w-full"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={cargarMas}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        {mensajeDeError(errorMas).detalle}
                    </Aviso>
                ) : siguiente ? (
                    <Boton variante="contorno" tamano="sm" onClick={cargarMas} disabled={cargandoMas}>
                        <ChevronDown />
                        {cargandoMas ? "Cargando…" : "Cargar más"}
                    </Boton>
                ) : (
                    <p className="text-[0.8125rem] text-muted-foreground">No hay más tickets.</p>
                )}
            </div>

            {asignando ? (
                <DialogoAsignarVarios
                    tickets={seleccion}
                    usuarios={usuarios}
                    alCerrar={() => setAsignando(false)}
                    alTerminar={(r) => {
                        setAsignando(false);
                        reemplazar(r.hechos);
                        setFallidos(r.fallidos);
                        setElegidos(new Set(r.fallidos.map((f) => f.ticket.id)));
                        alCambiar?.();
                    }}
                />
            ) : null}
        </>
    );
}

/** Diálogo corto (1 campo): el responsable de todos los tickets elegidos. */
function DialogoAsignarVarios({
    tickets,
    usuarios,
    alCerrar,
    alTerminar,
}: {
    tickets: readonly Ticket[];
    usuarios: readonly Usuario[] | null | undefined;
    alCerrar: () => void;
    alTerminar: (r: ResultadoLote) => void;
}) {
    const { yo } = useSesion();
    const form = useForm({ resolver: zodResolver(esquemaAsignar), defaultValues: { usuarioId: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const otros = (usuarios ?? []).filter((u) => u.tipo === "persona" && u.activo && u.id !== yo.usuario.id);
    const nombreDe = (id: number | undefined) =>
        id === undefined ? null : id === yo.usuario.id ? yo.usuario.nombre : (otros.find((u) => u.id === id)?.nombre ?? `Usuario ${id}`);
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido
                titulo={tickets.length === 1 ? "Asignar 1 ticket" : `Asignar ${tickets.length} tickets`}
                descripcion="Cada ticket se asigna con su propia acción; si alguno falla, se dice cuál."
            >
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        const r = await enLote(tickets, (t) =>
                            resolver(
                                api().POST("/api/v1/tickets/{id}/asignar", {
                                    params: { path: { id: t.id }, header: { "Idempotency-Key": nuevaClaveIdempotencia() } },
                                    body: { version: t.version, ...(d.usuarioId !== undefined ? { usuarioId: d.usuarioId } : {}) },
                                }),
                            ),
                        );
                        if (r.hechos.length > 0) avisar({ titulo: textoLoteAsignado(r.hechos.length, nombreDe(d.usuarioId)) });
                        alTerminar(r);
                    })}
                >
                    <CampoSelector
                        etiqueta="Responsable"
                        autoFocus
                        ayuda={
                            usuarios === null
                                ? "No tienes acceso a la lista de usuarios: puedes asignártelos o dejarlos sin asignar."
                                : "Debe poder ver los tickets y operar en esta empresa."
                        }
                        error={form.formState.errors.usuarioId?.message}
                        {...form.register("usuarioId")}
                    >
                        <option value="">Sin asignar</option>
                        <option value={yo.usuario.id}>{yo.usuario.nombre} (yo)</option>
                        {otros.map((u) => (
                            <option key={u.id} value={u.id}>
                                {u.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Asignando…">
                            Asignar
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
