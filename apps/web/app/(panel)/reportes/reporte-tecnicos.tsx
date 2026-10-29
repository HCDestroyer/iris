"use client";

import { EstadoVacio, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "@iris/ui";
import { Inbox } from "lucide-react";
import { Barras, FilaIndicadores, Indicador } from "@/componentes/reportes/graficos";
import { MarcoReporte, Seccion } from "@/componentes/reportes/marco";
import { useLectura } from "@/lib/lectura";
import { leerTecnicos } from "@/lib/reportes/api";
import type { QueryReporte } from "@/lib/reportes/filtros";
import { textoEntero, textoMinutos, textoPorcentaje } from "@/lib/reportes/presentacion";

export function ReporteTecnicos({ query }: { query: QueryReporte }) {
    const lectura = useLectura((signal) => leerTecnicos(query, signal));
    return (
        <MarcoReporte reporte="tecnicos" query={query} lectura={lectura} reintentar={lectura.reintentar}>
            {(r) => {
                const t = r.total;
                const filas = [...(r.data ?? [])].sort((a, b) => b.finalizadas - a.finalizadas || a.nombre.localeCompare(b.nombre, "es"));
                if (t.finalizadas === 0 || filas.length === 0) {
                    return <EstadoVacio icono={Inbox} titulo="Sin órdenes finalizadas en el período" descripcion="Prueba con otro período o quita filtros." />;
                }
                return (
                    <>
                        <FilaIndicadores>
                            <Indicador etiqueta="Órdenes finalizadas" valor={textoEntero(t.finalizadas)} detalle={`${textoEntero(t.exitosas)} exitosas · ${textoEntero(t.parciales)} parciales · ${textoEntero(t.fallidas)} fallidas`} />
                            <Indicador etiqueta="Éxito" valor={textoPorcentaje(t.porcentajeExito)} detalle="Exitosas sobre finalizadas" />
                            <Indicador etiqueta="Duración media en sitio" valor={textoMinutos(t.duracionMediaSitioMin)} detalle={`De ${textoEntero(t.conTiempoEnSitio)} con llegada registrada, sin pausas`} />
                            <Indicador etiqueta="Tiempo pausado" valor={textoMinutos(t.tiempoPausadoMin)} detalle={`${textoEntero(t.conPausa)} órdenes con pausa`} />
                        </FilaIndicadores>
                        <Barras
                            titulo="Órdenes finalizadas por técnico"
                            hallazgo="Al final de cada barra, las órdenes finalizadas y su porcentaje de éxito."
                            series={[{ nombre: "Finalizadas", clase: "bg-grafico-1" }]}
                            filas={filas.map((f) => ({
                                etiqueta: f.nombre,
                                detalle: `${f.exitosas} exitosas · ${f.parciales} parciales · ${f.fallidas} fallidas`,
                                valores: [{ valor: f.finalizadas, texto: `${textoEntero(f.finalizadas)} · ${f.porcentajeExito === null ? "sin datos" : textoPorcentaje(f.porcentajeExito)}` }],
                            }))}
                        />
                        <Seccion titulo="Desglose por técnico">
                            <Tabla className="whitespace-nowrap">
                                <TablaEncabezado>
                                    <TablaFila>
                                        <TablaTitulo>Técnico</TablaTitulo>
                                        <TablaTitulo className="text-right">Finalizadas</TablaTitulo>
                                        <TablaTitulo className="text-right">Exitosas</TablaTitulo>
                                        <TablaTitulo className="text-right">Parciales</TablaTitulo>
                                        <TablaTitulo className="text-right">Fallidas</TablaTitulo>
                                        <TablaTitulo className="text-right">% éxito</TablaTitulo>
                                        <TablaTitulo className="text-right">Media en sitio</TablaTitulo>
                                        <TablaTitulo className="text-right">Tiempo pausado</TablaTitulo>
                                        <TablaTitulo className="text-right">Pausa media</TablaTitulo>
                                    </TablaFila>
                                </TablaEncabezado>
                                <TablaCuerpo>
                                    {filas.map((f) => (
                                        <TablaFila key={f.tecnicoId}>
                                            <TablaCelda className="font-medium">{f.nombre}</TablaCelda>
                                            <TablaCelda className="text-right">{textoEntero(f.finalizadas)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoEntero(f.exitosas)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoEntero(f.parciales)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoEntero(f.fallidas)}</TablaCelda>
                                            <TablaCelda className="text-right font-medium">{textoPorcentaje(f.porcentajeExito)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoMinutos(f.duracionMediaSitioMin)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoMinutos(f.tiempoPausadoMin)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoMinutos(f.tiempoPausadoMedioMin)}</TablaCelda>
                                        </TablaFila>
                                    ))}
                                </TablaCuerpo>
                            </Tabla>
                        </Seccion>
                    </>
                );
            }}
        </MarcoReporte>
    );
}
