"use client";

import type { Schema } from "@iris/contratos";
import {
    Aviso,
    Boton,
    cn,
    Pestanas,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { ClipboardPlus, GitMerge, Lock, MailWarning, MessageSquare, Network, SearchX, ShieldAlert, Tag, TrendingUp } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { FichaCliente } from "@/componentes/clientes/ficha-cliente";
import { BotonVerEn, useEnlacesExternos } from "@/componentes/enlaces-externos";
import { TarjetaEncuesta } from "@/componentes/encuestas/tarjeta-encuesta";
import { ErrorLectura } from "@/componentes/error-lectura";
import { OrdenesDelTicket } from "@/componentes/ordenes/ordenes-ticket";
import { nombreCategoria, nombreUsuario, useCategorias, useUsuarios } from "@/componentes/tickets/catalogos";
import { PropuestaResolucion } from "@/componentes/tickets/propuesta-resolucion";
import { InsigniaEstadoTicket, InsigniaPrioridad, SlaVisible, useAhora } from "@/componentes/tickets/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { ErrorApi } from "@/lib/api/errores";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { enlaceHorusTicket } from "@/lib/enlaces";
import { useLectura, type Lectura } from "@/lib/lectura";
import { usePaginado, type Paginado } from "@/lib/paginado";
import { usePestanaUrl } from "@/lib/pestanas";
import { PieLista } from "@/componentes/pie-lista";
import { useSesion } from "@/lib/sesion";
import { ESTADOS, ORIGENES, PRIORIDADES, type EstadoTicket, type Prioridad } from "@/lib/tickets/presentacion";
import { correoRetenido } from "@/lib/tickets/mensajes";
import type { Ticket } from "@/lib/tickets/sla";
import { BarraAcciones } from "./acciones";

type Mensaje = Schema<"Mensaje">;
type Evento = Schema<"EventoTicket">;

const PAGINA = 100;


export function DetalleTicket() {
    const params = useParams<{ id: string }>();
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id < 1) {
        return <NoEncontrado />;
    }
    return <Detalle key={id} id={id} />;
}

function NoEncontrado() {
    return (
        <>
            <Tarjeta>
                <EstadoVacio
                    icono={SearchX}
                    titulo="No encontramos este ticket"
                    descripcion="Puede que no exista, que sea de otra empresa o que esté fuera de tu zona."
                    accion={
                        <Boton asChild variante="secundario">
                            <Link href="/tickets">Ir a la bandeja</Link>
                        </Boton>
                    }
                />
            </Tarjeta>
        </>
    );
}

/** Pestañas de la ficha del ticket (`?pestana=`). */
const PESTANAS_TICKET = ["resumen", "conversacion", "ordenes", "historial"] as const;
type PestanaTicket = (typeof PESTANAS_TICKET)[number];

function Detalle({ id }: { id: number }) {
    const { yo, puede } = useSesion();
    const ahora = useAhora();
    const veNotas = puede(["tickets.ver_notas"]);
    const ticket: Lectura<Ticket> = useLectura((signal) => resolver(api().GET("/api/v1/tickets/{id}", { params: { path: { id } }, signal })));
    useNombreEnMigas(ticket.estado === "listo" ? ticket.datos?.numero : null);
    const mensajes = usePaginado<Mensaje>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/tickets/{id}/mensajes", {
                params: { path: { id }, query: { limit: PAGINA, ...(cursor ? { cursor } : {}), ...(veNotas ? {} : { soloPublicos: true }) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const eventos = usePaginado<Evento>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/tickets/{id}/eventos", {
                params: { path: { id }, query: { limit: PAGINA, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const categorias = useCategorias();
    const usuarios = useUsuarios();
    const enlaces = useEnlacesExternos();
    const [pestana, setPestana] = usePestanaUrl(PESTANAS_TICKET, "resumen");

    if (ticket.estado === "cargando") {
        return (
            <div aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando el ticket…
                </span>
                <Esqueleto className="mb-3 h-8 w-2/3" />
                <Esqueleto className="mb-4 h-32" />
                <Esqueleto className="h-64" />
            </div>
        );
    }
    if (ticket.estado === "error") {
        if (ticket.error instanceof ErrorApi && ticket.error.estado === 404) {
            return <NoEncontrado />;
        }
        return (
            <>
                <ErrorLectura error={ticket.error} que="el ticket" reintentar={ticket.reintentar} />
            </>
        );
    }

    const t = ticket.datos;
    const listaUsuarios = usuarios.estado === "listo" ? usuarios.datos : undefined;
    const refrescarTodo = async () => {
        await Promise.all([ticket.refrescar(), mensajes.refrescar(), eventos.refrescar()]);
    };
    const categoria = nombreCategoria(categorias.estado === "listo" ? categorias.datos : undefined, t.categoriaId);
    const asignado = nombreUsuario(listaUsuarios, t.asignadoA, { id: yo.usuario.id, nombre: yo.usuario.nombre });
    const verHorus = enlaceHorusTicket(enlaces, t.origen);

    return (
        <>
            <header className="mb-4 grid gap-3">
                <div className="grid gap-1">
                    <span className="font-mono text-sm text-muted-foreground">{t.numero}</span>
                    <h1 className="text-2xl font-semibold tracking-[-0.015em] break-words">{t.asunto}</h1>
                </div>
                <div className="flex flex-wrap items-center gap-1.5">
                    <InsigniaEstadoTicket estado={t.estado} />
                    <InsigniaPrioridad prioridad={t.prioridad} />
                    {categoria ? <InsigniaEstado tono="neutral" icono={Tag} texto={categoria} /> : null}
                    {t.nivelEscalamiento > 0 ? (
                        <InsigniaEstado tono="neutral" icono={TrendingUp} texto={`Escalado ${t.nivelEscalamiento === 1 ? "1 vez" : `${t.nivelEscalamiento} veces`}`} />
                    ) : null}
                    {t.contacto.verificado ? null : (
                        <InsigniaEstado
                            tono="warning"
                            icono={ShieldAlert}
                            texto="Contacto sin verificar"
                            title="El correo que abrió el ticket no se pudo autenticar (DMARC/DKIM)"
                            data-testid="contacto-sin-verificar"
                        />
                    )}
                    {verHorus ? <BotonVerEn enlace={verHorus} /> : null}
                </div>
                {t.ticketPadreId !== null ? (
                    <Aviso tono="info" titulo="Parte de un incidente" icono={Network}>
                        Este ticket depende de un incidente maestro.{" "}
                        <Link className="text-primary underline-offset-4 hover:underline" href={`/tickets/${t.ticketPadreId}`}>
                            Ver el incidente maestro
                        </Link>
                    </Aviso>
                ) : null}
                <PropuestaResolucion propuesta={t.propuesta} />
                {t.fusionadoEnId !== null ? (
                    <Aviso tono="info" titulo="Ticket fusionado" icono={GitMerge}>
                        Se cerró como duplicado.{" "}
                        <Link className="text-primary underline-offset-4 hover:underline" href={`/tickets/${t.fusionadoEnId}`}>
                            Ver el ticket que se quedó
                        </Link>
                    </Aviso>
                ) : null}
                <div className="flex flex-wrap items-start gap-2">
                    <BarraAcciones
                        ticket={t}
                        usuarios={listaUsuarios}
                        alHecho={(nuevo) => {
                            ticket.reemplazar(nuevo);
                            void mensajes.refrescar();
                            void eventos.refrescar();
                        }}
                        recargar={refrescarTodo}
                    />
                    {t.estado === "cerrado" ? null : puede(["ordenes.crear"]) ? (
                        <Boton asChild variante="secundario" tamano="sm">
                            <Link href={`/ordenes/nueva?ticket=${t.id}`}>
                                <ClipboardPlus />
                                {puede(["tickets.crear_orden"]) ? "Crear orden y asignar" : "Crear orden"}
                            </Link>
                        </Boton>
                    ) : null}
                </div>
            </header>

            <div className="grid grid-cols-[minmax(0,1fr)] gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
                <div className="grid min-w-0 content-start gap-4">
                    <Pestanas<PestanaTicket>
                        etiqueta="Secciones del ticket"
                        seleccionada={pestana}
                        alCambiar={setPestana}
                        pestanas={[
                            {
                                id: "resumen",
                                etiqueta: "Resumen",
                                contenido: (
                                    <div className="grid gap-4">
                                        <Tarjeta>
                                            <TarjetaContenido className="grid gap-4 pt-5 sm:grid-cols-2">
                                                <div className="grid gap-1.5">
                                                    <span className="text-xs font-medium text-muted-foreground">SLA de primera respuesta</span>
                                                    <SlaVisible reloj={t.slaRespuesta} tipo="respuesta" ahora={ahora} />
                                                </div>
                                                <div className="grid gap-1.5">
                                                    <span className="text-xs font-medium text-muted-foreground">SLA de resolución</span>
                                                    <SlaVisible reloj={t.slaResolucion} tipo="resolucion" ahora={ahora} />
                                                </div>
                                                <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm sm:col-span-2">
                                                    <dt className="text-muted-foreground">Asignado</dt>
                                                    <dd data-testid="asignado">{asignado ?? "Sin asignar"}</dd>
                                                    <dt className="text-muted-foreground">Origen</dt>
                                                    <dd>{ORIGENES[t.origen]}</dd>
                                                    <dt className="text-muted-foreground">Creado</dt>
                                                    <dd className="tabular-nums">{formatearFecha(t.creadoEn)}</dd>
                                                    <dt className="text-muted-foreground">Actualizado</dt>
                                                    <dd className="tabular-nums">{formatearFecha(t.actualizadoEn)}</dd>
                                                </dl>
                                            </TarjetaContenido>
                                        </Tarjeta>
                                        <TicketsHijos ticketId={t.id} />
                                    </div>
                                ),
                            },
                            { id: "conversacion", etiqueta: "Conversación", contenido: <Conversacion paginado={mensajes} veNotas={veNotas} ahora={ahora} /> },
                            ...(puede(["ordenes.ver", "ordenes.ver_propias"])
                                ? [
                                      {
                                          id: "ordenes" as const,
                                          etiqueta: "Órdenes",
                                          contenido: (
                                              <OrdenesDelTicket
                                                  ticket={t}
                                                  usuarios={listaUsuarios}
                                                  alCambiarTicket={(nuevo) => {
                                                      ticket.reemplazar(nuevo);
                                                      void eventos.refrescar();
                                                  }}
                                              />
                                          ),
                                      },
                                  ]
                                : []),
                            { id: "historial", etiqueta: "Historial", contenido: <Historial paginado={eventos} ahora={ahora} /> },
                        ]}
                    />
                </div>
                <aside className="grid content-start gap-4" aria-label="Cliente">
                    {t.contacto.verificado ? null : (
                        <Aviso tono="warning" titulo="Contacto sin verificar" icono={ShieldAlert}>
                            El ticket nació de un correo cuyo remitente no se pudo autenticar (DMARC/DKIM): cualquiera puede escribir ese «De:».
                            Confirma la identidad por otro medio antes de dar datos de la cuenta o hacer cambios.
                        </Aviso>
                    )}
                    <FichaCliente ticket={t} enlaces={enlaces} />
                    {puede(["encuestas.ver"]) ? <TarjetaEncuesta key={t.estado} filtro={{ ticketId: t.id }} /> : null}
                </aside>
            </div>
        </>
    );
}

function Conversacion({ paginado, veNotas, ahora }: { paginado: Paginado<Mensaje>; veNotas: boolean; ahora: number }) {
    const { primera, items } = paginado;
    return (
        <Tarjeta>
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Conversación</TarjetaTitulo>
                {veNotas ? null : (
                    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Lock aria-hidden className="size-3.5" />
                        Sin acceso a notas internas
                    </span>
                )}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {primera.estado === "cargando" ? (
                    <>
                        <Esqueleto className="h-16" />
                        <Esqueleto className="h-16" />
                    </>
                ) : primera.estado === "error" ? (
                    <ErrorLectura error={primera.error} que="la conversación" reintentar={primera.reintentar} />
                ) : items.length === 0 ? (
                    <EstadoVacio icono={MessageSquare} titulo="Sin mensajes" descripcion="Aún no hay mensajes en este ticket." className="py-6" />
                ) : (
                    <ol className="grid gap-3" aria-label="Mensajes">
                        {items.map((m) => {
                            const retenido = correoRetenido(m);
                            return (
                                <li
                                    key={m.id}
                                    data-publico={m.publico}
                                    data-correo-retenido={retenido?.motivo}
                                    className={cn(
                                        "grid gap-1.5 rounded-lg border p-3",
                                        m.publico ? "bg-card" : "border-dashed border-warning/50 bg-warning/5",
                                        m.autorTipo === "cliente" && "bg-elevated",
                                        retenido && "border-l-4 border-solid border-warning/60",
                                    )}
                                >
                                    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
                                        <span className="min-w-0 font-medium break-all">{m.autorNombre}</span>
                                        {m.autorTipo === "cliente" ? <span className="text-xs text-muted-foreground">Cliente</span> : null}
                                        {retenido ? (
                                            <InsigniaEstado tono="warning" icono={MailWarning} texto={retenido.texto} />
                                        ) : m.publico ? null : (
                                            <InsigniaEstado tono="warning" icono={Lock} texto="Nota interna" />
                                        )}
                                        <time className="ml-auto text-xs text-muted-foreground tabular-nums" dateTime={m.creadoEn} title={formatearFecha(m.creadoEn)}>
                                            {haceCuanto(m.creadoEn, ahora)}
                                        </time>
                                    </div>
                                    {retenido ? (
                                        <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                                            <Lock aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                                            {retenido.explicacion}
                                        </p>
                                    ) : null}
                                    <p className="text-sm break-words whitespace-pre-wrap">{m.cuerpo}</p>
                                </li>
                            );
                        })}
                    </ol>
                )}
                <PieLista paginado={paginado} que="mensajes" />
            </TarjetaContenido>
        </Tarjeta>
    );
}

const estadoTexto = (v: string | null) => (v && v in ESTADOS ? ESTADOS[v as EstadoTicket].texto.toLowerCase() : (v ?? "—"));
const prioridadTexto = (v: string | null) => (v && v in PRIORIDADES ? PRIORIDADES[v as Prioridad].texto.toLowerCase() : (v ?? "—"));
const relojTexto = (d: Record<string, unknown>) => (d.sla === "respuesta" ? " de primera respuesta" : d.sla === "resolucion" ? " de resolución" : "");

function describirEvento(e: Evento): string {
    switch (e.tipo) {
        case "creado":
            return "Creó el ticket";
        case "estado":
            return `Cambió el estado de ${estadoTexto(e.de)} a ${estadoTexto(e.a)}`;
        case "asignado":
            return e.datos.usuario_id === null || e.datos.usuario_id === undefined ? "Dejó el ticket sin asignar" : "Cambió el responsable";
        case "escalado":
            return e.de && e.a && e.de !== e.a ? `Escaló de ${prioridadTexto(e.de)} a ${prioridadTexto(e.a)}` : "Escaló el ticket";
        case "fusionado":
            return `Fusionó ${e.de ?? ""} en ${e.a ?? ""}`.trim();
        case "sla_en_riesgo":
            return `SLA${relojTexto(e.datos)} en riesgo`;
        case "sla_vencido":
            return `SLA${relojTexto(e.datos)} vencido`;
        case "aviso_incidente":
            return "Avisó al cliente del incidente de red";
    }
}

function Historial({ paginado, ahora }: { paginado: Paginado<Evento>; ahora: number }) {
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
                                    {describirEvento(e)}
                                    {e.motivo ? <span className="text-muted-foreground"> · {e.motivo}</span> : null}
                                </span>
                                <span className="text-xs text-muted-foreground">
                                    {e.actorNombre || "Sistema"} ·{" "}
                                    <time dateTime={e.creadoEn} title={formatearFecha(e.creadoEn)}>
                                        {haceCuanto(e.creadoEn, ahora)}
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

/** Tickets hijos de un incidente maestro (`GET /tickets?ticketPadreId=`). Sin hijos no se muestra nada. */
function TicketsHijos({ ticketId }: { ticketId: number }) {
    const hijos = useLectura((signal) => resolver(api().GET("/api/v1/tickets", { params: { query: { ticketPadreId: ticketId, limit: 50 } }, signal })));
    if (hijos.estado === "cargando") {
        return null;
    }
    if (hijos.estado === "error") {
        return <ErrorLectura error={hijos.error} que="los tickets relacionados" reintentar={hijos.reintentar} />;
    }
    const lista = hijos.datos.data ?? [];
    if (lista.length === 0) {
        return null;
    }
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Tickets del incidente</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ul className="grid gap-2" aria-label="Tickets del incidente">
                    {lista.map((h) => (
                        <li key={h.id} className="flex flex-wrap items-center gap-2 text-sm">
                            <Link className="font-mono text-primary underline-offset-4 hover:underline" href={`/tickets/${h.id}`}>
                                {h.numero}
                            </Link>
                            <span className="min-w-0 flex-1 truncate">{h.asunto}</span>
                            <InsigniaEstadoTicket estado={h.estado} />
                        </li>
                    ))}
                </ul>
                {hijos.datos.nextCursor ? <p className="mt-2 text-xs text-muted-foreground">Se muestran los primeros 50.</p> : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
