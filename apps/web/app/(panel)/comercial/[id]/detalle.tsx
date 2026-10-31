"use client";

import { Aviso, Boton, cn, Esqueleto, EstadoVacio, Pestanas, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Check, CircleX, ExternalLink, SearchX } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import type { ReactNode } from "react";
import { InsigniaProspecto, nombreZona, useZonas, type Zona } from "@/componentes/comercial/comun";
import { DatosClienteServicio, ListaTelefonos } from "@/componentes/clientes/datos-cliente-servicio";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nombreUsuario, useUsuarios, type Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { ErrorApi } from "@/lib/api/errores";
import { ESTADOS_PROSPECTO, ORIGENES, PASOS_EMBUDO, textoEstadoCualquiera, textoEvento, type EventoProspecto, type ProspectoDetalle } from "@/lib/comercial/presentacion";
import { telefonos } from "@/lib/datos/telefono";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { usePestanaUrl } from "@/lib/pestanas";
import { enlaceMapa, textoCoordenadas } from "@/lib/ordenes/adjuntos";
import { useSesion } from "@/lib/sesion";
import { BarraAcciones } from "./acciones";
import { Actividades } from "./actividades";
import { TarjetaAlta } from "./alta";
import { TarjetaContrato } from "./contrato";
import { TarjetaCotizacion } from "./cotizacion";
import { TarjetaFactibilidad } from "./factibilidad";


export function DetalleProspecto() {
    const params = useParams<{ id: string }>();
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id < 1) return <NoEncontrado />;
    return <Detalle key={id} id={id} />;
}

function NoEncontrado() {
    return (
        <>
            <Tarjeta>
                <EstadoVacio
                    icono={SearchX}
                    titulo="No encontramos este prospecto"
                    descripcion="Puede que no exista, que sea de otra empresa o que esté fuera de tu alcance."
                    accion={
                        <Boton asChild variante="secundario">
                            <Link href="/comercial">Ir al embudo</Link>
                        </Boton>
                    }
                />
            </Tarjeta>
        </>
    );
}

/** Pestañas de la ficha del prospecto (`?pestana=`). */
const PESTANAS_PROSPECTO = ["resumen", "factibilidad", "cotizacion", "contrato", "actividad"] as const;
type PestanaProspecto = (typeof PESTANAS_PROSPECTO)[number];

function Detalle({ id }: { id: number }) {
    const detalle = useLectura<ProspectoDetalle>((signal) => resolver(api().GET("/api/v1/prospectos/{id}", { params: { path: { id } }, signal })));
    useNombreEnMigas(detalle.estado === "listo" ? detalle.datos?.nombre : null);
    const eventos = useLectura<EventoProspecto[]>(async (signal) => (await resolver(api().GET("/api/v1/prospectos/{id}/eventos", { params: { path: { id } }, signal }))) ?? []);
    const zonas = useZonas();
    const usuarios = useUsuarios();
    const listaUsuarios = usuarios.estado === "listo" ? usuarios.datos : null;
    const { puede } = useSesion();
    const [pestana, setPestana] = usePestanaUrl(PESTANAS_PROSPECTO, "resumen");

    if (detalle.estado === "cargando") {
        return (
            <>
                <div className="grid gap-4" aria-busy="true">
                    <span className="sr-only" role="status">
                        Cargando prospecto…
                    </span>
                    <Esqueleto className="h-10 w-72" />
                    <Esqueleto className="h-48" />
                </div>
            </>
        );
    }
    if (detalle.estado === "error") {
        if (detalle.error instanceof ErrorApi && detalle.error.estado === 404) return <NoEncontrado />;
        return (
            <>
                <ErrorLectura error={detalle.error} que="el prospecto" reintentar={detalle.reintentar} />
            </>
        );
    }
    const d = detalle.datos;
    const recargar = async () => {
        await Promise.all([detalle.refrescar(), eventos.refrescar()]);
    };

    return (
        <>
            <header className="mb-4 grid gap-3">
                <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="grid min-w-0 gap-1">
                        <span className="font-mono text-sm text-muted-foreground tabular-nums">{d.numero}</span>
                        <h1 className="text-2xl font-semibold tracking-[-0.015em] break-words">{d.nombre}</h1>
                    </div>
                    <InsigniaProspecto estado={d.estado} />
                </div>
                <Pasos estado={d.estado} />
                {d.estado === "perdido" ? (
                    <Aviso tono="error" icono={CircleX} titulo="Prospecto perdido">
                        {d.motivoPerdida ?? "Sin motivo."} {d.perdidoEn ? <span className="text-muted-foreground">({formatearFecha(d.perdidoEn)})</span> : null}
                    </Aviso>
                ) : null}
                <BarraAcciones detalle={d} usuarios={listaUsuarios} recargar={recargar} />
            </header>
            <Pestanas<PestanaProspecto>
                etiqueta="Secciones del prospecto"
                seleccionada={pestana}
                alCambiar={setPestana}
                pestanas={[
                    {
                        id: "resumen",
                        etiqueta: "Resumen",
                        contenido: (
                            <div className="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)] lg:items-start">
                                <div className="grid min-w-0 gap-4">
                                    <TarjetaAlta detalle={d} recargar={recargar} />
                                    <Datos d={d} zonas={zonas.estado === "listo" ? zonas.datos : null} usuarios={listaUsuarios} />
                                </div>
                                <div className="grid min-w-0 gap-4">
                                    {d.alta?.clienteArgosId != null && puede(["clientes.ver"]) ? (
                                        <DatosClienteServicio clienteId={d.alta.clienteArgosId} servicioId={d.alta.servicioArgosId} origen="prospecto" conEstadoCuenta={false} />
                                    ) : null}
                                </div>
                            </div>
                        ),
                    },
                    { id: "factibilidad", etiqueta: "Factibilidad", contenido: <TarjetaFactibilidad detalle={d} recargar={recargar} /> },
                    { id: "cotizacion", etiqueta: "Cotización", contenido: <TarjetaCotizacion detalle={d} recargar={recargar} /> },
                    { id: "contrato", etiqueta: "Contrato", contenido: <TarjetaContrato detalle={d} recargar={recargar} /> },
                    {
                        id: "actividad",
                        etiqueta: "Actividad",
                        contenido: (
                            <div className="grid gap-4 lg:grid-cols-2 lg:items-start [&>*]:min-w-0">
                                <Actividades prospectoId={d.id} usuarios={listaUsuarios} recargar={recargar} />
                                <Historial lectura={eventos} usuarios={listaUsuarios} />
                            </div>
                        ),
                    },
                ]}
            />
        </>
    );
}

/** Pasos del embudo: los anteriores hechos, el actual marcado (perdido no es un paso). */
function Pasos({ estado }: { estado: ProspectoDetalle["estado"] }) {
    const actual = PASOS_EMBUDO.indexOf(estado);
    return (
        <ol className="flex flex-wrap items-center gap-x-1 gap-y-2 text-xs" aria-label="Avance en el embudo">
            {PASOS_EMBUDO.map((p, i) => {
                const hecho = actual >= 0 && i < actual;
                const enCurso = i === actual;
                return (
                    <li key={p} className="flex items-center gap-1" aria-current={enCurso ? "step" : undefined}>
                        <span
                            className={cn(
                                "inline-flex items-center gap-1 rounded-full border px-2 py-0.5",
                                enCurso ? "border-primary bg-primary/10 font-medium text-foreground" : hecho ? "text-foreground" : "text-muted-foreground",
                            )}
                        >
                            {hecho ? <Check aria-hidden className="size-3 text-success" /> : null}
                            {ESTADOS_PROSPECTO[p].texto}
                        </span>
                        {i < PASOS_EMBUDO.length - 1 ? <span aria-hidden className="text-muted-foreground">›</span> : null}
                    </li>
                );
            })}
        </ol>
    );
}

function Fila({ etiqueta, children }: { etiqueta: string; children: ReactNode }) {
    return (
        <>
            <dt className="text-muted-foreground">{etiqueta}</dt>
            <dd className="min-w-0 break-words">{children}</dd>
        </>
    );
}

function Datos({ d, zonas, usuarios }: { d: ProspectoDetalle; zonas: readonly Zona[] | null; usuarios: readonly Usuario[] | null }) {
    const { yo } = useSesion();
    const mapa = d.lat !== null && d.lon !== null ? enlaceMapa(d.lat, d.lon) : undefined;
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Datos</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-sm">
                    <Fila etiqueta="Dirección">{d.direccion}</Fila>
                    <Fila etiqueta="Ubicación">
                        {d.lat !== null && d.lon !== null ? (
                            mapa ? (
                                <a href={mapa} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
                                    <span className="tabular-nums">{textoCoordenadas(d.lat, d.lon)}</span>
                                    <ExternalLink aria-hidden className="size-3.5" />
                                </a>
                            ) : (
                                textoCoordenadas(d.lat, d.lon)
                            )
                        ) : (
                            <span className="text-muted-foreground">Sin coordenadas</span>
                        )}
                    </Fila>
                    <Fila etiqueta="Teléfono">{d.telefono ? <ListaTelefonos telefonos={telefonos("telefono", d.telefono)} /> : "—"}</Fila>
                    <Fila etiqueta="Correo">{d.correo ?? "—"}</Fila>
                    <Fila etiqueta="Documento">{d.documento ?? "—"}</Fila>
                    <Fila etiqueta="Zona">{nombreZona(zonas, d.zonaId) ?? "Sin zona"}</Fila>
                    <Fila etiqueta="Vendedor">{nombreUsuario(usuarios, d.vendedorId, { id: yo.usuario.id, nombre: yo.usuario.nombre }) ?? "Sin vendedor"}</Fila>
                    <Fila etiqueta="Plan de interés">{d.planInteresNombre ?? "—"}</Fila>
                    <Fila etiqueta="Origen">
                        {ORIGENES[d.origen]}
                        {d.origenDetalle ? <span className="text-muted-foreground"> · {d.origenDetalle}</span> : null}
                    </Fila>
                    <Fila etiqueta="Creado">
                        <span className="tabular-nums">{formatearFecha(d.creadoEn)}</span>
                    </Fila>
                    {d.notas ? <Fila etiqueta="Notas">{d.notas}</Fila> : null}
                </dl>
            </TarjetaContenido>
        </Tarjeta>
    );
}

function Historial({ lectura, usuarios }: { lectura: ReturnType<typeof useLectura<EventoProspecto[]>>; usuarios: readonly Usuario[] | null }) {
    const { yo } = useSesion();
    return (
        <Tarjeta data-testid="historial">
            <TarjetaEncabezado>
                <TarjetaTitulo>Historial</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-20" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} que="el historial" reintentar={lectura.reintentar} />
                ) : lectura.datos.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin movimientos.</p>
                ) : (
                    <ol className="grid gap-2.5 text-sm" aria-label="Historial">
                        {[...lectura.datos].reverse().map((e) => {
                            const de = textoEstadoCualquiera(e.de);
                            const a = textoEstadoCualquiera(e.a);
                            const actor = e.actorTipo === "sistema" ? "Iris" : nombreUsuario(usuarios, e.actorId, { id: yo.usuario.id, nombre: yo.usuario.nombre });
                            return (
                                <li key={e.id} className="grid gap-0.5 border-l-2 pl-3">
                                    <span className="font-medium">
                                        {textoEvento(e.tipo)}
                                        {de || a ? <span className="font-normal text-muted-foreground"> · {[de, a].filter(Boolean).join(" → ")}</span> : null}
                                    </span>
                                    {e.motivo ? <span className="text-muted-foreground">{e.motivo}</span> : null}
                                    <span className="text-xs text-muted-foreground tabular-nums">
                                        {formatearFecha(e.creadoEn)}
                                        {actor ? ` · ${actor}` : ""}
                                    </span>
                                </li>
                            );
                        })}
                    </ol>
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}
