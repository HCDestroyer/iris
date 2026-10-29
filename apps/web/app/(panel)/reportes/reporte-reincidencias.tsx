"use client";

import { Esqueleto, EstadoVacio, InsigniaEstado, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "@iris/ui";
import { Inbox } from "lucide-react";
import Link from "next/link";
import { FilaIndicadores, Indicador } from "@/componentes/reportes/graficos";
import { MarcoReporte, Seccion } from "@/componentes/reportes/marco";
import { PieLista } from "@/componentes/pie-lista";
import type { EstadoLectura } from "@/lib/lectura";
import { usePaginado } from "@/lib/paginado";
import { leerReincidencias, type ReporteReincidencias as Datos } from "@/lib/reportes/api";
import type { QueryReporte } from "@/lib/reportes/filtros";
import { etiquetaCategoria, textoEntero, textoFechaHora, textoHoras, textoPorcentaje } from "@/lib/reportes/presentacion";
import { PRIORIDADES } from "@/lib/tickets/presentacion";

/**
 * Reincidencias: los totales vienen con la primera página (calculados sobre todo el período); la
 * lista sigue con «Cargar más». Si una página siguiente falla, lo leído sigue y el error va aparte.
 */
export function ReporteReincidencias({ query }: { query: QueryReporte }) {
    const paginado = usePaginado((cursor, signal) => leerReincidencias(query, cursor, signal));
    const { primera } = paginado;
    // La primera página es el reporte entero (totales + data + nextCursor); usePaginado la tipa solo como página.
    const lectura = primera as EstadoLectura<Datos>;
    return (
        <MarcoReporte reporte="reincidencias" query={query} lectura={lectura} reintentar={primera.reintentar}>
            {(r) => {
                const ventana = r.dias === 1 ? "1 día" : `${r.dias} días`;
                if (r.resueltos === 0) {
                    return <EstadoVacio icono={Inbox} titulo="Sin tickets resueltos en el período" descripcion="Prueba con otro período o quita filtros." />;
                }
                return (
                    <>
                        <FilaIndicadores>
                            <Indicador etiqueta="Reincidencia" valor={textoPorcentaje(r.porcentaje)} detalle={`${textoEntero(r.reincidentes)} de ${textoEntero(r.evaluables)} evaluables volvieron en ${ventana}`} />
                            <Indicador etiqueta="Reincidentes" valor={textoEntero(r.reincidentes)} detalle="Con otro ticket del mismo servicio o cliente" />
                            <Indicador etiqueta="Evaluables" valor={textoEntero(r.evaluables)} detalle="Con servicio o cliente para comparar" />
                            <Indicador etiqueta="Tickets resueltos" valor={textoEntero(r.resueltos)} />
                        </FilaIndicadores>
                        <Seccion titulo="Tickets que volvieron">
                            {paginado.items.length === 0 ? (
                                <p className="text-sm text-muted-foreground">Ningún ticket resuelto volvió en {ventana}.</p>
                            ) : (
                                <Tabla className="whitespace-nowrap">
                                    <TablaEncabezado>
                                        <TablaFila>
                                            <TablaTitulo>Ticket resuelto</TablaTitulo>
                                            <TablaTitulo>Prioridad</TablaTitulo>
                                            <TablaTitulo>Categoría</TablaTitulo>
                                            <TablaTitulo>Resuelto</TablaTitulo>
                                            <TablaTitulo>Volvió con</TablaTitulo>
                                            <TablaTitulo className="text-right">Tiempo hasta volver</TablaTitulo>
                                            <TablaTitulo className="text-right">Nuevos en la ventana</TablaTitulo>
                                        </TablaFila>
                                    </TablaEncabezado>
                                    <TablaCuerpo>
                                        {paginado.items.map((f) => (
                                            <TablaFila key={f.ticketId} data-reincidencia={f.ticketId}>
                                                <TablaCelda className="min-w-56">
                                                    <Link href={`/tickets/${f.ticketId}`} className="font-medium text-primary underline-offset-4 hover:underline">
                                                        {f.numero}
                                                    </Link>
                                                    <span className="block max-w-72 truncate text-xs text-muted-foreground">{f.asunto}</span>
                                                </TablaCelda>
                                                <TablaCelda>
                                                    <InsigniaEstado {...PRIORIDADES[f.prioridad]} />
                                                </TablaCelda>
                                                <TablaCelda>{etiquetaCategoria(f)}</TablaCelda>
                                                <TablaCelda className="whitespace-nowrap">{textoFechaHora(f.resueltoEn)}</TablaCelda>
                                                <TablaCelda className="whitespace-nowrap">
                                                    <Link href={`/tickets/${f.siguienteId}`} className="font-medium text-primary underline-offset-4 hover:underline">
                                                        {f.siguienteNumero}
                                                    </Link>
                                                    <span className="block text-xs text-muted-foreground">{textoFechaHora(f.siguienteCreadoEn)}</span>
                                                </TablaCelda>
                                                <TablaCelda className="text-right whitespace-nowrap">{textoHoras(f.horasHastaVolver)}</TablaCelda>
                                                <TablaCelda className="text-right">{textoEntero(f.nuevosEnVentana)}</TablaCelda>
                                            </TablaFila>
                                        ))}
                                    </TablaCuerpo>
                                </Tabla>
                            )}
                            {paginado.cargandoMas ? <Esqueleto className="h-10" /> : null}
                            <PieLista paginado={paginado} que="reincidencias" />
                        </Seccion>
                    </>
                );
            }}
        </MarcoReporte>
    );
}
