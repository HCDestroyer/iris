"use client";

import { EstadoVacio, InsigniaEstado, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "@iris/ui";
import { Inbox } from "lucide-react";
import type { ReactNode } from "react";
import { Barras, FilaIndicadores, Indicador } from "@/componentes/reportes/graficos";
import { MarcoReporte, Seccion } from "@/componentes/reportes/marco";
import { useLectura } from "@/lib/lectura";
import { leerResolucion, type FilaResolucion } from "@/lib/reportes/api";
import type { QueryReporte } from "@/lib/reportes/filtros";
import { etiquetaCategoria, ordenarPorPrioridad, textoEntero, textoMinutos } from "@/lib/reportes/presentacion";
import { PRIORIDADES } from "@/lib/tickets/presentacion";

export function ReporteResolucion({ query }: { query: QueryReporte }) {
    const lectura = useLectura((signal) => leerResolucion(query, signal));
    return (
        <MarcoReporte reporte="resolucion" query={query} lectura={lectura} reintentar={lectura.reintentar}>
            {(r) => {
                const t = r.total;
                if (t.resueltos === 0) {
                    return <EstadoVacio icono={Inbox} titulo="Sin tickets resueltos en el período" descripcion="Prueba con otro período o quita filtros." />;
                }
                const porPrioridad = ordenarPorPrioridad(r.porPrioridad ?? []);
                return (
                    <>
                        <FilaIndicadores>
                            <Indicador etiqueta="Mediana" valor={textoMinutos(t.medianaMin)} detalle="La mitad se resolvió en menos tiempo" />
                            <Indicador etiqueta="Promedio" valor={textoMinutos(t.promedioMin)} detalle="Sensible a los casos largos" />
                            <Indicador etiqueta="Tickets resueltos" valor={textoEntero(t.resueltos)} detalle="Sin los fusionados" />
                        </FilaIndicadores>
                        <Barras
                            titulo="Mediana de resolución por prioridad"
                            hallazgo="Tiempo de reloj desde que se abrió el ticket hasta que se resolvió."
                            series={[{ nombre: "Mediana", clase: "bg-grafico-1" }]}
                            filas={porPrioridad.map((f) => ({
                                etiqueta: f.prioridad ? PRIORIDADES[f.prioridad].texto : "Sin prioridad",
                                detalle: `${f.resueltos} resueltos · promedio ${textoMinutos(f.promedioMin)}`,
                                valores: [{ valor: f.medianaMin, texto: textoMinutos(f.medianaMin) }],
                            }))}
                        />
                        <Seccion titulo="Por prioridad">
                            <TablaResolucion filas={porPrioridad} columna="Prioridad" celda={(f) => (f.prioridad ? <InsigniaEstado {...PRIORIDADES[f.prioridad]} /> : "Sin prioridad")} />
                        </Seccion>
                        <Seccion titulo="Por categoría">
                            <TablaResolucion filas={r.porCategoria ?? []} columna="Categoría" celda={etiquetaCategoria} />
                        </Seccion>
                    </>
                );
            }}
        </MarcoReporte>
    );
}

function TablaResolucion({ filas, columna, celda }: { filas: readonly FilaResolucion[]; columna: string; celda: (f: FilaResolucion) => ReactNode }) {
    if (filas.length === 0) return <p className="text-sm text-muted-foreground">Sin filas en el período.</p>;
    return (
        <Tabla className="whitespace-nowrap">
            <TablaEncabezado>
                <TablaFila>
                    <TablaTitulo>{columna}</TablaTitulo>
                    <TablaTitulo className="text-right">Resueltos</TablaTitulo>
                    <TablaTitulo className="text-right">Mediana</TablaTitulo>
                    <TablaTitulo className="text-right">Promedio</TablaTitulo>
                </TablaFila>
            </TablaEncabezado>
            <TablaCuerpo>
                {filas.map((f, i) => (
                    <TablaFila key={`${f.prioridad ?? ""}${f.categoriaId ?? ""}-${i}`}>
                        <TablaCelda>{celda(f)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.resueltos)}</TablaCelda>
                        <TablaCelda className="text-right font-medium">{textoMinutos(f.medianaMin)}</TablaCelda>
                        <TablaCelda className="text-right">{textoMinutos(f.promedioMin)}</TablaCelda>
                    </TablaFila>
                ))}
            </TablaCuerpo>
        </Tabla>
    );
}
