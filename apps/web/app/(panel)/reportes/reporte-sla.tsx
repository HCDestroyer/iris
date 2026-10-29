"use client";

import { EstadoVacio, InsigniaEstado, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "@iris/ui";
import { Inbox } from "lucide-react";
import type { ReactNode } from "react";
import { Barras, FilaIndicadores, Indicador } from "@/componentes/reportes/graficos";
import { MarcoReporte, Seccion } from "@/componentes/reportes/marco";
import { useLectura } from "@/lib/lectura";
import { leerSLA, type FilaSLA } from "@/lib/reportes/api";
import type { QueryReporte } from "@/lib/reportes/filtros";
import { etiquetaCategoria, ordenarPorPrioridad, SERIES_SLA, textoEntero, textoPorcentaje } from "@/lib/reportes/presentacion";
import { PRIORIDADES } from "@/lib/tickets/presentacion";

export function ReporteSla({ query }: { query: QueryReporte }) {
    const lectura = useLectura((signal) => leerSLA(query, signal));
    return (
        <MarcoReporte reporte="sla" query={query} lectura={lectura} reintentar={lectura.reintentar}>
            {(r) => {
                const t = r.total;
                if (t.resueltos === 0) {
                    return <EstadoVacio icono={Inbox} titulo="Sin tickets resueltos en el período" descripcion="Prueba con otro período o quita filtros." />;
                }
                const porPrioridad = ordenarPorPrioridad(r.porPrioridad ?? []);
                return (
                    <>
                        <FilaIndicadores>
                            <Indicador etiqueta="Respuesta en tiempo" valor={textoPorcentaje(t.porcentajeRespuesta)} detalle={`${textoEntero(t.respuestaEnTiempo)} de ${textoEntero(t.conSlaRespuesta)} con SLA de respuesta`} />
                            <Indicador etiqueta="Resolución en tiempo" valor={textoPorcentaje(t.porcentajeResolucion)} detalle={`${textoEntero(t.resolucionEnTiempo)} de ${textoEntero(t.conSlaResolucion)} con SLA de resolución`} />
                            <Indicador etiqueta="Tickets resueltos" valor={textoEntero(t.resueltos)} detalle="Sin los fusionados" />
                            <Indicador etiqueta="Sin SLA de resolución" valor={textoEntero(t.resueltos - t.conSlaResolucion)} detalle="No cuentan en el porcentaje" />
                        </FilaIndicadores>
                        <Barras
                            titulo="SLA cumplido por prioridad"
                            hallazgo="Porcentaje de tickets que respondieron y se resolvieron dentro de su SLA."
                            series={SERIES_SLA}
                            maximo={100}
                            filas={porPrioridad.map((f) => ({
                                etiqueta: f.prioridad ? PRIORIDADES[f.prioridad].texto : "Sin prioridad",
                                detalle: `Respuesta ${f.respuestaEnTiempo}/${f.conSlaRespuesta} · Resolución ${f.resolucionEnTiempo}/${f.conSlaResolucion}`,
                                valores: [
                                    { valor: f.porcentajeRespuesta, texto: textoPorcentaje(f.porcentajeRespuesta) },
                                    { valor: f.porcentajeResolucion, texto: textoPorcentaje(f.porcentajeResolucion) },
                                ],
                            }))}
                        />
                        <Seccion titulo="Por prioridad">
                            <TablaSla filas={porPrioridad} columna="Prioridad" celda={(f) => (f.prioridad ? <InsigniaEstado {...PRIORIDADES[f.prioridad]} /> : "Sin prioridad")} />
                        </Seccion>
                        <Seccion titulo="Por categoría">
                            <TablaSla filas={r.porCategoria ?? []} columna="Categoría" celda={etiquetaCategoria} />
                        </Seccion>
                    </>
                );
            }}
        </MarcoReporte>
    );
}

function TablaSla({ filas, columna, celda }: { filas: readonly FilaSLA[]; columna: string; celda: (f: FilaSLA) => ReactNode }) {
    if (filas.length === 0) return <p className="text-sm text-muted-foreground">Sin filas en el período.</p>;
    return (
        <Tabla className="whitespace-nowrap">
            <TablaEncabezado>
                <TablaFila>
                    <TablaTitulo>{columna}</TablaTitulo>
                    <TablaTitulo className="text-right">Resueltos</TablaTitulo>
                    <TablaTitulo className="text-right">Respuesta en tiempo</TablaTitulo>
                    <TablaTitulo className="text-right">% respuesta</TablaTitulo>
                    <TablaTitulo className="text-right">Resolución en tiempo</TablaTitulo>
                    <TablaTitulo className="text-right">% resolución</TablaTitulo>
                </TablaFila>
            </TablaEncabezado>
            <TablaCuerpo>
                {filas.map((f, i) => (
                    <TablaFila key={`${f.prioridad ?? ""}${f.categoriaId ?? ""}-${i}`}>
                        <TablaCelda>{celda(f)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.resueltos)}</TablaCelda>
                        <TablaCelda className="text-right">
                            {textoEntero(f.respuestaEnTiempo)} de {textoEntero(f.conSlaRespuesta)}
                        </TablaCelda>
                        <TablaCelda className="text-right font-medium">{textoPorcentaje(f.porcentajeRespuesta)}</TablaCelda>
                        <TablaCelda className="text-right">
                            {textoEntero(f.resolucionEnTiempo)} de {textoEntero(f.conSlaResolucion)}
                        </TablaCelda>
                        <TablaCelda className="text-right font-medium">{textoPorcentaje(f.porcentajeResolucion)}</TablaCelda>
                    </TablaFila>
                ))}
            </TablaCuerpo>
        </Tabla>
    );
}
