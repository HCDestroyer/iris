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
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Esqueleto,
    EstadoVacio,
    FilaLista,
    ListaFilas,
    MenuAcciones,
    useCambiosSinGuardar,
    VistaRapida,
    type TonoIconoFila,
} from "@iris/ui";
import { Eye, Inbox, SquareArrowOutUpRight, Ticket as IconoTicket, UserRoundPlus, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { pieListaFilas } from "@/componentes/pie-lista";
import { nombreCategoria, nombreUsuario, type Categoria, type Usuario } from "@/componentes/tickets/catalogos";
import { InsigniaEstadoTicket, InsigniaPrioridad, SlaVisible, useAhora } from "@/componentes/tickets/insignias";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { esquemaAsignar } from "@/lib/tickets/esquemas";
import type { ConsultaTickets } from "@/lib/tickets/filtros";
import { enLote, textoLoteAsignado, type ResultadoLote } from "@/lib/tickets/lote";
import type { Prioridad } from "@/lib/tickets/presentacion";
import { relojPrincipal, type Ticket } from "@/lib/tickets/sla";

const TAMANO_PAGINA = 25;

/** La ficha de color del ícono sigue a la prioridad (la urgencia); el estado de flujo va en texto. */
const TONO_PRIORIDAD: Record<Prioridad, TonoIconoFila> = { critica: "peligro", alta: "peligro", media: "atencion", baja: "neutro" };

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
    /** Buscador y filtros: la cabecera de la lista. */
    cabecera?: ReactNode;
    /** Una acción en lote cambió tickets (para refrescar los contadores). */
    alCambiar?: () => void;
};

/**
 * Lista de la bandeja (`ListaFilas`) con cursor y selección múltiple: con `tickets.asignar`, cada
 * fila tiene su casilla y al elegir alguna aparece la barra de acciones («Asignar…»). Cada fila
 * abre la ficha; su menú `⋯` tiene la vista rápida, abrir la ficha y asignar. Al cambiar los
 * filtros vuelve a leer desde la primera página sin remontarse: la cabecera (buscador y filtros)
 * es parte de la lista y no debe perder el foco.
 */
export function ListaTickets({ consulta, categorias, usuarios, yo, hayFiltros, cabecera, alCambiar }: Props) {
    const { puede } = useSesion();
    const router = useRouter();
    const primera = useLectura((signal) => leerPagina(consulta, undefined, signal));
    const [extra, setExtra] = useState<{ tickets: Ticket[]; cursor: string | null } | null>(null);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const [elegidos, setElegidos] = useState<ReadonlySet<number>>(new Set());
    const [asignando, setAsignando] = useState<readonly Ticket[] | null>(null);
    const [vistaId, setVistaId] = useState<number | null>(null);
    const [fallidos, setFallidos] = useState<ResultadoLote["fallidos"]>([]);
    const ahora = useAhora();
    const seleccionable = puede(["tickets.asignar"]);
    const clave = JSON.stringify(consulta);
    const claveLeida = useRef(clave);
    // «Cargar más» en curso: se cancela si cambian los filtros (su página sería de otra consulta).
    const [, setEnCurso] = useState<AbortController | null>(null);
    const { reintentar } = primera;

    useEffect(() => {
        if (claveLeida.current === clave) return;
        claveLeida.current = clave;
        setEnCurso((c) => {
            c?.abort();
            return null;
        });
        setExtra(null);
        setErrorMas(null);
        setCargandoMas(false);
        setElegidos(new Set());
        setVistaId(null);
        setFallidos([]);
        reintentar();
    }, [clave, reintentar]);

    // Cargando, error y vacío se muestran dentro de la misma lista: la cabecera (buscador y filtros)
    // no se remonta al volver a leer.
    const leyendo =
        primera.estado === "cargando" ? (
            <div className="grid gap-3" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando tickets…
                </span>
                {Array.from({ length: 5 }, (_, i) => (
                    <Esqueleto key={i} className="h-11" />
                ))}
            </div>
        ) : primera.estado === "error" ? (
            <ErrorLectura
                error={primera.error}
                que="los tickets"
                reintentar={() => {
                    setExtra(null);
                    primera.reintentar();
                }}
            />
        ) : null;

    const tickets = primera.estado === "listo" ? [...(primera.datos.data ?? []), ...(extra?.tickets ?? [])] : [];
    const siguiente = primera.estado !== "listo" ? null : extra ? extra.cursor : primera.datos.nextCursor;
    const asignables = tickets.filter((t) => t.estado !== "cerrado");
    const seleccion = asignables.filter((t) => elegidos.has(t.id));
    const enVista = tickets.find((t) => t.id === vistaId) ?? null;

    async function cargarMas() {
        if (!siguiente) {
            return;
        }
        const control = new AbortController();
        setEnCurso(control);
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const pagina = await leerPagina(consulta, siguiente, control.signal);
            if (!control.signal.aborted) setExtra((prev) => ({ tickets: [...(prev?.tickets ?? []), ...(pagina.data ?? [])], cursor: pagina.nextCursor }));
        } catch (error) {
            if (!control.signal.aborted) setErrorMas(error);
        } finally {
            if (!control.signal.aborted) {
                setCargandoMas(false);
                setEnCurso(null);
            }
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

    const asignado = (t: Ticket) => nombreUsuario(usuarios, t.asignadoA, yo);
    const todos = asignables.length > 0 && seleccion.length === asignables.length;
    const pie = pieListaFilas({ cuantos: tickets.length, siguiente, cargarMas, cargandoMas, errorMas }, "tickets");

    const casillaTodos = (
        <Casilla
            etiqueta={<span className="sr-only">Seleccionar todos los tickets</span>}
            checked={todos}
            disabled={asignables.length === 0}
            onChange={(e) => setElegidos(e.target.checked ? new Set(asignables.map((t) => t.id)) : new Set())}
        />
    );
    const barraSeleccion =
        seleccion.length > 0 ? (
            <div role="toolbar" aria-label="Acciones con la selección" className="flex w-full flex-wrap items-center gap-2 text-[0.8125rem]" data-testid="barra-seleccion">
                {casillaTodos}
                <span className="mr-auto font-semibold tabular-nums" aria-live="polite">
                    {seleccion.length === 1 ? "1 ticket seleccionado" : `${seleccion.length} tickets seleccionados`}
                </span>
                <Boton tamano="sm" className="rounded-lg" onClick={() => setAsignando(seleccion)}>
                    <UserRoundPlus />
                    Asignar…
                </Boton>
                <Boton variante="fantasma" tamano="sm" onClick={() => setElegidos(new Set())}>
                    <X />
                    Quitar selección
                </Boton>
            </div>
        ) : null;
    // Sin selección, «Seleccionar todos» va al final de la cabecera; con selección, en su barra.
    // Misma forma siempre (la casilla va o no en el segundo hueco): si cambiara, React remontaría
    // el buscador y los filtros, que perderían el foco y la lista abierta.
    const cabeceraLista = (
        <>
            {cabecera}
            {seleccionable && seleccion.length === 0 && asignables.length > 0 ? <span className="ml-auto">{casillaTodos}</span> : null}
        </>
    );

    return (
        <>
            <ListaFilas
                etiqueta="Tickets"
                cabecera={cabeceraLista}
                seleccion={barraSeleccion}
                {...(leyendo ? {} : pie)}
                vacio={
                    leyendo ?? (
                        <EstadoVacio
                            icono={Inbox}
                            titulo={hayFiltros ? "Ningún ticket coincide con los filtros" : "No hay tickets"}
                            descripcion={hayFiltros ? "Prueba con otra vista o quita algún filtro." : "Cuando entren tickets aparecerán aquí."}
                        />
                    )
                }
            >
                {fallidos.length > 0 ? (
                    <li className="p-3">
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
                    </li>
                ) : null}
                {tickets.map((t) => {
                    const { tipo, reloj } = relojPrincipal(t);
                    const marcado = elegidos.has(t.id);
                    const categoria = t.categoriaId !== null ? nombreCategoria(categorias, t.categoriaId) : undefined;
                    return (
                        <FilaLista
                            key={t.id}
                            data-ticket={t.numero}
                            icono={IconoTicket}
                            tonoIcono={TONO_PRIORIDAD[t.prioridad]}
                            titulo={t.asunto}
                            detalle={
                                <>
                                    <span className="font-mono">{t.numero}</span>
                                    {[categoria, t.contacto.nombre, asignado(t) ?? "Sin asignar", haceCuanto(t.actualizadoEn, ahora)]
                                        .filter(Boolean)
                                        .map((parte) => ` · ${parte}`)
                                        .join("")}
                                </>
                            }
                            href={`/tickets/${t.id}`}
                            activa={marcado || vistaId === t.id}
                            casilla={
                                seleccionable && t.estado !== "cerrado" ? (
                                    <Casilla etiqueta={<span className="sr-only">Seleccionar {t.numero}</span>} checked={marcado} onChange={(e) => alternar(t.id, e.target.checked)} />
                                ) : undefined
                            }
                            extremo={
                                <>
                                    <InsigniaEstadoTicket estado={t.estado} />
                                    <InsigniaPrioridad prioridad={t.prioridad} />
                                    <SlaVisible reloj={reloj} tipo={tipo} ahora={ahora} compacto />
                                </>
                            }
                            extremoTelefono="debajo"
                            menu={
                                <MenuAcciones
                                    etiqueta={`Acciones de ${t.numero}`}
                                    acciones={[
                                        { id: "vista", texto: "Vista rápida", icono: Eye, alElegir: () => setVistaId(t.id) },
                                        { id: "abrir", texto: "Abrir ficha", icono: SquareArrowOutUpRight, alElegir: () => router.push(`/tickets/${t.id}`) },
                                        {
                                            id: "asignar",
                                            texto: "Asignar…",
                                            icono: UserRoundPlus,
                                            permitida: seleccionable && t.estado !== "cerrado",
                                            alElegir: () => setAsignando([t]),
                                        },
                                    ]}
                                />
                            }
                        />
                    );
                })}
            </ListaFilas>

            {enVista ? (
                <VistaTicket
                    ticket={enVista}
                    categoria={enVista.categoriaId !== null ? nombreCategoria(categorias, enVista.categoriaId) : undefined}
                    responsable={asignado(enVista) ?? "Sin asignar"}
                    ahora={ahora}
                    puedeAsignar={seleccionable && enVista.estado !== "cerrado"}
                    alAsignar={() => setAsignando([enVista])}
                    alCerrar={() => setVistaId(null)}
                />
            ) : null}

            {asignando ? (
                <DialogoAsignarVarios
                    tickets={asignando}
                    usuarios={usuarios}
                    alCerrar={() => setAsignando(null)}
                    alTerminar={(r) => {
                        setAsignando(null);
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

/** Vista rápida de un ticket: lo justo para decidir sin salir de la bandeja. */
function VistaTicket({
    ticket: t,
    categoria,
    responsable,
    ahora,
    puedeAsignar,
    alAsignar,
    alCerrar,
}: {
    ticket: Ticket;
    categoria: string | undefined;
    responsable: string;
    ahora: number;
    puedeAsignar: boolean;
    alAsignar: () => void;
    alCerrar: () => void;
}) {
    const { tipo, reloj } = relojPrincipal(t);
    const datos: [string, ReactNode][] = [
        ["Cliente", t.contacto.nombre ?? "—"],
        ["Categoría", categoria ?? "Sin categoría"],
        ["Responsable", responsable],
        ["Creado", formatearFecha(t.creadoEn) ?? "—"],
        ["Actualizado", haceCuanto(t.actualizadoEn, ahora)],
    ];
    return (
        <VistaRapida
            abierto
            alCerrar={alCerrar}
            titulo={t.asunto}
            codigo={t.numero}
            insignias={
                <>
                    <InsigniaEstadoTicket estado={t.estado} />
                    <InsigniaPrioridad prioridad={t.prioridad} />
                </>
            }
            pie={
                <>
                    <Boton asChild>
                        <Link href={`/tickets/${t.id}`}>
                            <SquareArrowOutUpRight />
                            Abrir ficha
                        </Link>
                    </Boton>
                    {puedeAsignar ? (
                        <Boton variante="contorno" onClick={alAsignar}>
                            <UserRoundPlus />
                            Asignar…
                        </Boton>
                    ) : null}
                </>
            }
        >
            <SlaVisible reloj={reloj} tipo={tipo} ahora={ahora} />
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                {datos.map(([etiqueta, valor]) => (
                    <div key={etiqueta} className="contents">
                        <dt className="text-muted-foreground">{etiqueta}</dt>
                        <dd className="min-w-0 truncate">{valor}</dd>
                    </div>
                ))}
            </dl>
        </VistaRapida>
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
