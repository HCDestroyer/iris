"use client";

import type { Schema } from "@iris/contratos";
import { InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo, type Tono } from "@iris/ui";
import {
    Camera,
    CircleCheck,
    CircleDashed,
    CircleX,
    ExternalLink,
    Gauge,
    HandCoins,
    MessageSquareText,
    MapPin,
    Package,
    PackageCheck,
    PackageMinus,
    PackagePlus,
    ShieldCheck,
    ShieldQuestion,
    TriangleAlert,
    type LucideIcon,
} from "lucide-react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { Galeria, ImagenConReintento, type Foto } from "@/componentes/ordenes/imagen-adjunto";
import { nombreUsuario, type Usuario } from "@/componentes/tickets/catalogos";
import { formatearFecha } from "@/lib/fechas";
import type { Lectura } from "@/lib/lectura";
import { enlaceMapa, esImagenMostrable, textoCoordenadas, type Adjunto } from "@/lib/ordenes/adjuntos";
import { puntosGps, type EventoOrden } from "@/lib/ordenes/historial";
import type { Paginado } from "@/lib/paginado";
import { CONDICIONES, type OrdenDetalle } from "@/lib/ordenes/presentacion";

type Item = Schema<"ItemChecklist">;
export type Yo = { id: number; nombre: string };

function valorItem(i: Item): { texto: string; tono: "neutral" | "success" | "error" } | undefined {
    // El contrato lo publica sin tipo (depende de `tipo`): si_no boolean, texto string, número o uuid.
    const v: unknown = i.valor;
    if (v === null || v === undefined) return undefined;
    switch (i.tipo) {
        case "si_no":
            return v === true ? { texto: "Sí", tono: "success" } : { texto: "No", tono: "error" };
        case "numero":
        case "medicion":
            return { texto: `${typeof v === "number" ? v.toLocaleString("es-GT") : String(v)}${i.unidad ? ` ${i.unidad}` : ""}`, tono: "neutral" };
        case "foto":
            return { texto: "Foto adjunta", tono: "neutral" };
        default:
            return { texto: typeof v === "string" ? v : JSON.stringify(v), tono: "neutral" };
    }
}

/** Ítems de un formulario con la marca vigente de cada uno (quién y cuándo). */
export function ItemsChecklist({ items, final, usuarios, yo, etiqueta }: { items: readonly Item[]; final: boolean; usuarios: readonly Usuario[] | null | undefined; yo: Yo; etiqueta: string }) {
    if (items.length === 0) return <p className="text-sm text-muted-foreground">Sin ítems.</p>;
    return (
        <ol className="grid gap-2" aria-label={etiqueta}>
            {items.map((i) => {
                const valor = valorItem(i);
                return (
                    <li key={i.itemId} className="grid gap-1 border-b pb-2 last:border-0 last:pb-0" data-item={i.codigo}>
                        <div className="flex flex-wrap items-center justify-between gap-2">
                            <span className="text-sm">
                                {i.etiqueta}
                                {i.obligatorio ? <span className="text-muted-foreground"> · obligatorio</span> : null}
                            </span>
                            {valor ? (
                                <InsigniaEstado
                                    tono={valor.tono}
                                    icono={i.tipo === "foto" ? Camera : i.tipo === "texto" || i.tipo === "seleccion" ? MessageSquareText : i.tipo === "si_no" ? (valor.tono === "error" ? CircleX : CircleCheck) : Gauge}
                                    texto={valor.texto}
                                    className="max-w-full whitespace-normal"
                                />
                            ) : (
                                <InsigniaEstado tono={i.obligatorio && !final ? "warning" : "neutral"} icono={CircleDashed} texto="Sin marcar" />
                            )}
                        </div>
                        {i.ocurridoEn ? (
                            <span className="text-xs text-muted-foreground tabular-nums">
                                {nombreUsuario(usuarios, i.marcadoPor, yo) ?? "Sistema"} · {formatearFecha(i.ocurridoEn)}
                            </span>
                        ) : null}
                    </li>
                );
            })}
        </ol>
    );
}

/** Fotos de la orden (adjuntos `foto` completos) en galería con visor dentro del panel. */
/** `adjuntos` listo con `null`: sin permiso `adjuntos.ver`. */
export function Fotos({ orden, adjuntos }: { orden: OrdenDetalle; adjuntos: Lectura<Adjunto[] | null> }) {
    const etiquetas = new Map((orden.checklist ?? []).map((i) => [i.codigo, i.etiqueta]));
    const etiquetasPorItem = new Map((orden.checklist ?? []).map((i) => [i.itemId, i.etiqueta]));
    const registrados = new Map((orden.adjuntos ?? []).map((a) => [a.id, a]));
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Fotos</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {adjuntos.estado === "listo" && adjuntos.datos === null ? (
                    <p className="text-sm text-muted-foreground">
                        {(orden.adjuntos ?? []).filter((a) => a.tipo === "foto").length} fotos registradas. No tienes permiso para ver adjuntos.
                    </p>
                ) : adjuntos.estado === "cargando" ? (
                    <div className="grid grid-cols-3 gap-2" aria-busy="true">
                        <span className="aspect-square animate-pulse rounded-md bg-elevated" />
                        <span className="aspect-square animate-pulse rounded-md bg-elevated" />
                        <span className="aspect-square animate-pulse rounded-md bg-elevated" />
                    </div>
                ) : adjuntos.estado === "error" ? (
                    <ErrorLectura error={adjuntos.error} que="las fotos" reintentar={adjuntos.reintentar} />
                ) : (
                    (() => {
                        const fotos = (adjuntos.datos ?? []).filter((a) => a.tipo === "foto");
                        const mostrables: Foto[] = fotos.filter(esImagenMostrable).map((a) => {
                            const reg = registrados.get(a.id);
                            const titulo =
                                (a.claveChecklist ? etiquetas.get(a.claveChecklist) : undefined) ??
                                (reg?.itemId ? etiquetasPorItem.get(reg.itemId) : undefined) ??
                                (reg?.descripcion || "Foto");
                            const mapa = a.lat !== null && a.lon !== null ? enlaceMapa(a.lat, a.lon) : undefined;
                            return {
                                id: a.id,
                                titulo,
                                detalle: (
                                    <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                                        <span className="tabular-nums">Tomada {formatearFecha(a.tomadoEn ?? a.creadoEn)}</span>
                                        {mapa && a.lat !== null && a.lon !== null ? (
                                            <a className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline" href={mapa} target="_blank" rel="noopener noreferrer">
                                                <MapPin aria-hidden className="size-3.5" />
                                                {textoCoordenadas(a.lat, a.lon)}
                                            </a>
                                        ) : null}
                                    </span>
                                ),
                            };
                        });
                        const enCurso = fotos.filter((a) => a.estado === "pendiente" || a.estado === "subiendo").length;
                        const rechazadas = fotos.filter((a) => a.estado === "rechazado").length;
                        return (
                            <>
                                {mostrables.length === 0 ? <p className="text-sm text-muted-foreground">Sin fotos todavía.</p> : <Galeria fotos={mostrables} etiqueta="Fotos de la orden" />}
                                {enCurso > 0 ? (
                                    <p className="text-sm text-muted-foreground">{enCurso === 1 ? "1 foto se está subiendo" : `${enCurso} fotos se están subiendo`} desde el teléfono.</p>
                                ) : null}
                                {rechazadas > 0 ? (
                                    <InsigniaEstado tono="warning" icono={TriangleAlert} texto={`${rechazadas} ${rechazadas === 1 ? "foto rechazada" : "fotos rechazadas"} al subir`} className="justify-self-start" />
                                ) : null}
                            </>
                        );
                    })()
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

/** Firma del cliente: imagen PNG (adjunto), nombre, documento y hora. */
export function Firma({ orden, verAdjuntos }: { orden: OrdenDetalle; verAdjuntos: boolean }) {
    const f = orden.firma;
    if (!f && !orden.requiereFirma) return null;
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Firma del cliente</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {!f ? (
                    <p className="text-sm text-muted-foreground">Esta orden pide firma y todavía no la tiene.</p>
                ) : (
                    <>
                        {f.adjuntoId && verAdjuntos ? (
                            // Fondo blanco fijo: la tinta de la firma es oscura sobre transparente, también en tema oscuro.
                            <div className="flex h-36 items-center justify-center rounded-md border bg-white p-2" data-testid="firma">
                                <ImagenConReintento id={f.adjuntoId} alt={`Firma de ${f.nombre}`} className="max-h-full max-w-full" />
                            </div>
                        ) : (
                            <p className="text-sm text-muted-foreground">{f.adjuntoId ? "No tienes permiso para ver la imagen de la firma." : "Firma registrada sin imagen."}</p>
                        )}
                        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                            <dt className="text-muted-foreground">Firmó</dt>
                            <dd>{f.nombre}</dd>
                            {f.documento ? (
                                <>
                                    <dt className="text-muted-foreground">Documento</dt>
                                    <dd className="font-mono">{f.documento}</dd>
                                </>
                            ) : null}
                            <dt className="text-muted-foreground">Hora</dt>
                            <dd className="tabular-nums">{formatearFecha(f.ocurridoEn)}</dd>
                        </dl>
                    </>
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function EnlaceMapa({ lat, lon }: { lat: number; lon: number }) {
    const url = enlaceMapa(lat, lon);
    const texto = textoCoordenadas(lat, lon);
    if (!url) return <span className="font-mono text-xs">{texto}</span>;
    return (
        <a className="inline-flex items-center gap-1 font-mono text-xs text-primary underline-offset-4 hover:underline" href={url} target="_blank" rel="noopener noreferrer" title="Abrir en OpenStreetMap">
            {texto}
            <ExternalLink aria-hidden className="size-3" />
        </a>
    );
}

/**
 * Ubicación de la orden y posiciones GPS del técnico con su hora. El mapa propio (MapLibre)
 * llega en la fase 3 con despacho: por ahora, coordenadas con enlace a OpenStreetMap.
 */
export function Gps({ orden, eventos }: { orden: OrdenDetalle; eventos: Paginado<EventoOrden> }) {
    const lectura = eventos.primera;
    const puntos = puntosGps(eventos.items);
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Ubicación y GPS</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3 text-sm">
                <div className="grid gap-0.5">
                    <span className="text-xs font-medium text-muted-foreground">Dirección de la orden</span>
                    <span className="break-words">{orden.direccion || "Sin dirección"}</span>
                    {orden.lat !== null && orden.lon !== null ? <EnlaceMapa lat={orden.lat} lon={orden.lon} /> : <span className="text-muted-foreground">Sin coordenadas</span>}
                </div>
                <div className="grid gap-1.5 border-t pt-3">
                    <span className="text-xs font-medium text-muted-foreground">Posiciones del técnico</span>
                    {lectura.estado === "cargando" ? (
                        <span className="h-10 animate-pulse rounded-md bg-elevated" />
                    ) : lectura.estado === "error" ? (
                        <ErrorLectura error={lectura.error} que="las posiciones" reintentar={lectura.reintentar} />
                    ) : puntos.length === 0 ? (
                        <p className="text-muted-foreground">Sin posiciones registradas.</p>
                    ) : (
                        <ol className="grid gap-2" aria-label="Posiciones GPS">
                            {puntos.map((p) => (
                                <li key={p.eventoId} className="grid gap-0.5" data-gps={p.eventoId}>
                                    <span>{p.que}</span>
                                    <span className="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                                        <span className="tabular-nums">{formatearFecha(p.tomadoEn)}</span>
                                        <EnlaceMapa lat={p.lat} lon={p.lon} />
                                        {p.precision !== undefined ? <span>± {Math.round(p.precision)} m</span> : null}
                                    </span>
                                </li>
                            ))}
                        </ol>
                    )}
                </div>
            </TarjetaContenido>
        </Tarjeta>
    );
}

/** Materiales usados y equipos instalados/retirados (serie y MAC). */
/** Cómo trata Argos cada material (contrato 0.5.0): cobro al cliente, comodato o gasto. */
const MODALIDADES = {
    consumible_cobro: { tono: "info", icono: HandCoins, texto: "Se cobra al cliente" },
    comodato: { tono: "warning", icono: PackagePlus, texto: "Comodato: regístralo como equipo" },
    consumible_gasto: { tono: "neutral", icono: Package, texto: "Gasto" },
} as const satisfies Record<NonNullable<OrdenDetalle["materiales"]>[number]["modalidad"] & string, { tono: Tono; icono: LucideIcon; texto: string }>;

export function MaterialesYEquipos({ orden }: { orden: OrdenDetalle }) {
    const materiales = orden.materiales ?? [];
    const equipos = orden.equipos ?? [];
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Materiales y equipos</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4 text-sm">
                <div className="grid gap-2">
                    <span className="text-xs font-medium text-muted-foreground">Materiales</span>
                    {materiales.length === 0 ? (
                        <p className="text-muted-foreground">Sin materiales registrados.</p>
                    ) : (
                        <ul className="grid gap-2" aria-label="Materiales">
                            {materiales.map((m) => (
                                <li key={m.id} className="flex flex-wrap items-center justify-between gap-2" data-material={m.productoId}>
                                    <span className="min-w-0 break-words">{m.descripcion || `Producto ${m.productoId}`}</span>
                                    <span className="flex min-w-0 flex-wrap items-center justify-end gap-2">
                                        {m.modalidad ? <InsigniaEstado {...MODALIDADES[m.modalidad]} data-modalidad={m.modalidad} /> : null}
                                        {m.excedeExistencia ? <InsigniaEstado tono="warning" icono={TriangleAlert} texto="Más de lo que había en bodega" /> : null}
                                        <span className="font-medium tabular-nums">{m.cantidad.toLocaleString("es-GT")}</span>
                                    </span>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
                <div className="grid gap-2 border-t pt-3">
                    <span className="text-xs font-medium text-muted-foreground">Equipos</span>
                    {equipos.length === 0 ? (
                        <p className="text-muted-foreground">Sin equipos registrados.</p>
                    ) : (
                        <ul className="grid gap-3" aria-label="Equipos">
                            {equipos.map((e) => (
                                <li key={e.id} className="grid gap-1" data-equipo={e.serie ?? e.id}>
                                    <span className="flex flex-wrap items-center gap-1.5">
                                        {e.accion === "instalar" ? (
                                            <InsigniaEstado tono="neutral" icono={PackagePlus} texto="Instalado" />
                                        ) : (
                                            <InsigniaEstado tono="neutral" icono={PackageMinus} texto="Retirado" />
                                        )}
                                        {e.condicion ? <InsigniaEstado {...CONDICIONES[e.condicion]} /> : null}
                                        {e.accion === "retirar" ? (
                                            e.validado ? (
                                                <InsigniaEstado tono="success" icono={ShieldCheck} texto="Verificado en el comodato" />
                                            ) : (
                                                <InsigniaEstado tono="warning" icono={ShieldQuestion} texto="Sin verificar con Argos" />
                                            )
                                        ) : null}
                                    </span>
                                    <span className="font-medium">{e.modelo || "Equipo"}</span>
                                    <dl className="grid grid-cols-[auto_1fr] gap-x-3 text-xs">
                                        <dt className="text-muted-foreground">Serie</dt>
                                        <dd className="font-mono break-all">{e.serie ?? "—"}</dd>
                                        <dt className="text-muted-foreground">MAC</dt>
                                        <dd className="font-mono">{e.mac ?? "—"}</dd>
                                    </dl>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
                {materiales.length === 0 && equipos.length === 0 ? null : (
                    <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <PackageCheck aria-hidden className="size-3.5" />
                        Se descuentan en Argos al cerrar la orden.
                    </p>
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

export function Notas({ orden }: { orden: OrdenDetalle }) {
    const notas = [...(orden.notas ?? [])].sort((a, b) => Date.parse(a.ocurridoEn) - Date.parse(b.ocurridoEn));
    if (notas.length === 0) return null;
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Notas del técnico</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ol className="grid gap-3" aria-label="Notas">
                    {notas.map((n) => (
                        <li key={n.id} className="grid gap-1 rounded-lg border p-3">
                            <span className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
                                <span className="font-medium text-foreground">{n.autorNombre || "Sistema"}</span>
                                <time className="tabular-nums" dateTime={n.ocurridoEn}>
                                    {formatearFecha(n.ocurridoEn)}
                                </time>
                            </span>
                            <p className="text-sm break-words whitespace-pre-wrap">{n.texto}</p>
                        </li>
                    ))}
                </ol>
            </TarjetaContenido>
        </Tarjeta>
    );
}
