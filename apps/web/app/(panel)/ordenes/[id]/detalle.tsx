"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Esqueleto, EstadoVacio, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowLeft, LifeBuoy, SearchX } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { Contacto, FichaArgos } from "@/componentes/clientes/ficha-cliente";
import { useEnlacesExternos } from "@/componentes/enlaces-externos";
import { TarjetaEncuesta } from "@/componentes/encuestas/tarjeta-encuesta";
import { ErrorLectura } from "@/componentes/error-lectura";
import { IgualarAsignacion } from "@/componentes/ordenes/asignacion-ticket-orden";
import { InsigniaEstadoOrden, InsigniaTipoOrden, MarcaPendienteArgos } from "@/componentes/ordenes/insignias";
import { PieLista } from "@/componentes/pie-lista";
import { nombreUsuario, useUsuarios, type Usuario } from "@/componentes/tickets/catalogos";
import { InsigniaEstadoTicket, useAhora } from "@/componentes/tickets/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { enlaceArgosCliente } from "@/lib/enlaces";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import type { Adjunto } from "@/lib/ordenes/adjuntos";
import { describirEventoOrden, type EventoOrden } from "@/lib/ordenes/historial";
import { RESULTADOS, resumirPendientes, textoMotivoFalla, type OrdenDetalle, type Pendiente } from "@/lib/ordenes/presentacion";
import { usePaginado, type Paginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";
import { BarraAccionesOrden } from "./acciones";
import { Firma, Fotos, Gps, MaterialesYEquipos, Notas } from "./evidencias";
import { Formularios, Pausas } from "./formularios";
import { PendientesArgos } from "./pendientes-argos";

function Volver() {
    return (
        <Link href="/ordenes" className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft aria-hidden className="size-4" />
            Órdenes
        </Link>
    );
}

export function DetalleOrden() {
    const params = useParams<{ id: string }>();
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id < 1) {
        return <NoEncontrada />;
    }
    return <Detalle key={id} id={id} />;
}

function NoEncontrada() {
    return (
        <>
            <Volver />
            <Tarjeta>
                <EstadoVacio
                    icono={SearchX}
                    titulo="No encontramos esta orden"
                    descripcion="Puede que no exista, que sea de otra empresa o que esté fuera de tu alcance."
                    accion={
                        <Boton asChild variante="secundario">
                            <Link href="/ordenes">Ir al tablero</Link>
                        </Boton>
                    }
                />
            </Tarjeta>
        </>
    );
}

function Detalle({ id }: { id: number }) {
    const { yo, puede } = useSesion();
    const ahora = useAhora();
    const verAdjuntos = puede(["adjuntos.ver"]);
    const verPendientes = puede(["integraciones.ver_pendientes"]);
    const orden = useLectura<OrdenDetalle>((signal) => resolver(api().GET("/api/v1/ordenes/{id}", { params: { path: { id } }, signal })));
    const eventos = usePaginado<EventoOrden>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/ordenes/{id}/eventos", {
                params: { path: { id }, query: { limit: 100, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const adjuntos = useLectura<Adjunto[] | null>(async (signal) => {
        if (!verAdjuntos) return null;
        const pagina = await resolver(api().GET("/api/v1/adjuntos", { params: { query: { entidad: "orden", entidadId: String(id), limit: 200 } }, signal }));
        return pagina.data ?? [];
    });
    // Marca de la cabecera: los pendientes no hechos de esta orden (la lista completa va en su tarjeta).
    const pendientes = useLectura<Pendiente[] | null>(async (signal) => {
        if (!verPendientes) return null;
        const pagina = await resolver(api().GET("/api/v1/integraciones/pendientes", { params: { query: { entidad: "orden", entidadId: String(id), limit: 200 } }, signal }));
        return pagina.data ?? [];
    });
    const usuarios = useUsuarios();
    const enlaces = useEnlacesExternos();

    if (orden.estado === "cargando") {
        return (
            <div aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando la orden…
                </span>
                <Volver />
                <Esqueleto className="mb-3 h-8 w-2/3" />
                <Esqueleto className="mb-4 h-32" />
                <Esqueleto className="h-64" />
            </div>
        );
    }
    if (orden.estado === "error") {
        if (orden.error instanceof ErrorApi && orden.error.estado === 404) {
            return <NoEncontrada />;
        }
        return (
            <>
                <Volver />
                <ErrorLectura error={orden.error} que="la orden" reintentar={orden.reintentar} />
            </>
        );
    }

    const o = orden.datos;
    const listaUsuarios = usuarios.estado === "listo" ? usuarios.datos : undefined;
    const yoCorto = { id: yo.usuario.id, nombre: yo.usuario.nombre };
    const tecnico = nombreUsuario(listaUsuarios, o.tecnicoId, yoCorto);
    const resumen = pendientes.estado === "listo" && pendientes.datos ? resumirPendientes(pendientes.datos) : undefined;
    const recargar = async () => {
        await Promise.all([orden.refrescar(), eventos.refrescar(), adjuntos.refrescar(), pendientes.refrescar()]);
    };

    return (
        <>
            <Volver />
            <header className="mb-4 grid gap-3">
                <div className="grid gap-1">
                    <span className="font-mono text-sm text-muted-foreground">{o.numero}</span>
                    <h1 className="text-2xl font-semibold tracking-[-0.015em] break-words">{o.direccion || "Orden sin dirección"}</h1>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                    <InsigniaEstadoOrden estado={o.estado} />
                    <InsigniaTipoOrden tipo={o.tipo} />
                    {resumen ? <MarcaPendienteArgos resumen={resumen} /> : null}
                </div>
                {o.estado === "fallida" ? (
                    <Aviso tono="error" titulo={`Fallida: ${textoMotivoFalla(o.motivoFalla) ?? "sin motivo"}`}>
                        {o.motivo ?? "El técnico no dejó detalle."}
                    </Aviso>
                ) : o.estado === "cancelada" && o.motivo ? (
                    <Aviso tono="neutral" titulo="Cancelada">
                        {o.motivo}
                    </Aviso>
                ) : null}
                <BarraAccionesOrden
                    orden={o}
                    usuarios={listaUsuarios}
                    alHecho={() => {
                        void recargar();
                    }}
                    recargar={recargar}
                />
            </header>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
                <div className="grid min-w-0 content-start gap-4">
                    <Tarjeta>
                        <TarjetaContenido className="grid gap-4 pt-5">
                            {o.descripcion ? <p className="text-sm break-words whitespace-pre-wrap">{o.descripcion}</p> : null}
                            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm" data-testid="datos-orden">
                                <dt className="text-muted-foreground">Técnico</dt>
                                <dd data-testid="tecnico">{tecnico ?? "Sin técnico"}</dd>
                                <dt className="text-muted-foreground">Cita</dt>
                                <dd className="tabular-nums" data-testid="cita">
                                    {o.programadaInicio ? `${formatearFecha(o.programadaInicio)}${o.programadaFin ? ` – ${formatearFecha(o.programadaFin)?.slice(-5) ?? ""}` : ""}` : "Sin programar"}
                                </dd>
                                {o.cuadrillaId !== null ? (
                                    <>
                                        <dt className="text-muted-foreground">Cuadrilla</dt>
                                        <dd>Cuadrilla {o.cuadrillaId}</dd>
                                    </>
                                ) : null}
                                {o.zonaId !== null ? (
                                    <>
                                        <dt className="text-muted-foreground">Zona</dt>
                                        <dd>Zona {o.zonaId}</dd>
                                    </>
                                ) : null}
                                {o.resultado ? (
                                    <>
                                        <dt className="text-muted-foreground">Resultado</dt>
                                        <dd>{RESULTADOS[o.resultado]}</dd>
                                    </>
                                ) : null}
                                {o.enCaminoEn ? (
                                    <>
                                        <dt className="text-muted-foreground">Salió</dt>
                                        <dd className="tabular-nums">{formatearFecha(o.enCaminoEn)}</dd>
                                    </>
                                ) : null}
                                {o.enSitioEn ? (
                                    <>
                                        <dt className="text-muted-foreground">Llegó</dt>
                                        <dd className="tabular-nums">{formatearFecha(o.enSitioEn)}</dd>
                                    </>
                                ) : null}
                                {o.finalizadaEn ? (
                                    <>
                                        <dt className="text-muted-foreground">Finalizada</dt>
                                        <dd className="tabular-nums">{formatearFecha(o.finalizadaEn)}</dd>
                                    </>
                                ) : null}
                                <dt className="text-muted-foreground">Creada</dt>
                                <dd className="tabular-nums">{formatearFecha(o.creadoEn)}</dd>
                            </dl>
                        </TarjetaContenido>
                    </Tarjeta>

                    <Pausas orden={o} ahora={ahora} />
                    <Formularios orden={o} usuarios={listaUsuarios} yo={yoCorto} recargar={recargar} />
                    <Fotos orden={o} adjuntos={adjuntos} />
                    <Firma orden={o} verAdjuntos={verAdjuntos} />
                    <MaterialesYEquipos orden={o} />
                    <Notas orden={o} />
                    <Historial paginado={eventos} ahora={ahora} />
                </div>
                <aside className="grid content-start gap-4" aria-label="Cliente y Argos">
                    {verPendientes ? <PendientesArgos ordenId={o.id} /> : null}
                    {o.ticketId !== null ? <TicketVinculado ticketId={o.ticketId} orden={o} usuarios={listaUsuarios} recargarOrden={recargar} /> : null}
                    {o.clienteId !== null && puede(["clientes.ver"]) ? (
                        <FichaArgos clienteId={o.clienteId} servicioId={o.servicioId} contacto={o.contacto} verArgos={enlaceArgosCliente(enlaces, o.clienteId)} origen="orden" />
                    ) : (
                        <Tarjeta>
                            <TarjetaEncabezado>
                                <TarjetaTitulo>{o.clienteId !== null ? `Cliente ${o.clienteId} de Argos` : "Contacto"}</TarjetaTitulo>
                            </TarjetaEncabezado>
                            <TarjetaContenido>
                                <Contacto contacto={o.contacto} vacio={o.clienteId !== null ? "No tienes permiso para ver la ficha del cliente." : "Sin datos de contacto."} />
                            </TarjetaContenido>
                        </Tarjeta>
                    )}
                    <Gps orden={o} eventos={eventos} />
                    {puede(["encuestas.ver"]) ? <TarjetaEncuesta key={o.estado} filtro={{ ordenId: o.id }} /> : null}
                </aside>
            </div>
        </>
    );
}

function TicketVinculado({
    ticketId,
    orden,
    usuarios,
    recargarOrden,
}: {
    ticketId: number;
    orden: OrdenDetalle;
    usuarios: readonly Usuario[] | null | undefined;
    recargarOrden: () => Promise<void>;
}) {
    const { puede } = useSesion();
    const ve = puede(["tickets.ver"]);
    const ticket = useLectura<Schema<"Ticket"> | null>(async (signal) => (ve ? resolver(api().GET("/api/v1/tickets/{id}", { params: { path: { id: ticketId } }, signal })) : null));
    return (
        <Tarjeta data-testid="ticket-vinculado">
            <TarjetaEncabezado>
                <TarjetaTitulo>Ticket de origen</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-2 text-sm">
                {ticket.estado === "cargando" ? (
                    <Esqueleto className="h-10" />
                ) : ticket.estado === "error" ? (
                    ticket.error instanceof ErrorApi && ticket.error.estado === 404 ? (
                        <p className="text-muted-foreground">Ticket {ticketId}: no está visible para ti.</p>
                    ) : (
                        <ErrorLectura error={ticket.error} que="el ticket" reintentar={ticket.reintentar} />
                    )
                ) : ticket.datos === null ? (
                    <p className="flex items-center gap-1.5">
                        <LifeBuoy aria-hidden className="size-4 text-muted-foreground" />
                        Ticket {ticketId}
                    </p>
                ) : (
                    <>
                        <Link className="flex items-center gap-1.5 font-mono text-primary underline-offset-4 hover:underline" href={`/tickets/${ticketId}`}>
                            <LifeBuoy aria-hidden className="size-4" />
                            {ticket.datos.numero}
                        </Link>
                        <span className="break-words">{ticket.datos.asunto}</span>
                        <InsigniaEstadoTicket estado={ticket.datos.estado} />
                        <p className="text-xs text-muted-foreground" data-testid="ticket-asignado">
                            Responsable del ticket:{" "}
                            {[
                                ticket.datos.asignadoA !== null ? (usuarios?.find((u) => u.id === ticket.datos?.asignadoA)?.nombre ?? `Usuario ${ticket.datos.asignadoA}`) : null,
                                ticket.datos.cuadrillaId !== null ? `Cuadrilla ${ticket.datos.cuadrillaId}` : null,
                            ]
                                .filter(Boolean)
                                .join(" · ") || "sin asignar"}
                        </p>
                        <IgualarAsignacion
                            ticket={ticket.datos}
                            orden={orden}
                            usuarios={usuarios}
                            alHecho={(r) => {
                                ticket.reemplazar(r.ticket);
                                void recargarOrden();
                            }}
                            recargar={() => {
                                ticket.reintentar();
                                void recargarOrden();
                            }}
                        />
                    </>
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function Historial({ paginado, ahora }: { paginado: Paginado<EventoOrden>; ahora: number }) {
    const { primera, items } = paginado;
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Historial</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {primera.estado === "cargando" ? (
                    <Esqueleto className="h-20" />
                ) : primera.estado === "error" ? (
                    <ErrorLectura error={primera.error} que="el historial" reintentar={primera.reintentar} />
                ) : items.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin eventos.</p>
                ) : (
                    <ol className="grid gap-2 border-l pl-4" aria-label="Historial">
                        {items.map((e) => (
                            <li key={e.id} className="relative grid gap-0.5 text-sm" data-evento={e.tipo}>
                                <span aria-hidden className="absolute top-1.5 -left-[1.3rem] size-2 rounded-full bg-border" />
                                <span>
                                    {describirEventoOrden(e)}
                                    {e.motivo ? <span className="text-muted-foreground"> · {e.motivo}</span> : null}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                    {e.actorNombre || "Sistema"}
                                    {e.canal === "campo" ? " · desde el teléfono" : ""} ·{" "}
                                    <time dateTime={e.ocurridoEn} title={formatearFecha(e.ocurridoEn)}>
                                        {haceCuanto(e.ocurridoEn, ahora)}
                                    </time>
                                </span>
                            </li>
                        ))}
                    </ol>
                )}
                <PieLista paginado={paginado} que="eventos" />
            </TarjetaContenido>
        </Tarjeta>
    );
}
