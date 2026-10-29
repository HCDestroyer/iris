"use client";

import { Aviso, EstadoVacio, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "@iris/ui";
import { Inbox, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { Barras, FilaIndicadores, Indicador } from "@/componentes/reportes/graficos";
import { MarcoReporte, Seccion } from "@/componentes/reportes/marco";
import { CALIFICACIONES, ENTIDADES_PLURAL, TEXTO_CALIFICACION, textoPromedio, UMBRAL_ALERTA } from "@/lib/encuestas/presentacion";
import { useLectura } from "@/lib/lectura";
import { leerSatisfaccion, type FilaSatisfaccion } from "@/lib/reportes/api";
import type { QueryReporte } from "@/lib/reportes/filtros";
import { textoEntero, textoPorcentaje } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";

/** Cantidad por calificación 1–5 (las que no vienen, 0). */
function distribucion(f: FilaSatisfaccion): { calificacion: number; cantidad: number }[] {
    return CALIFICACIONES.map((c) => ({ calificacion: c, cantidad: f.distribucion?.find((d) => d.calificacion === c)?.cantidad ?? 0 }));
}

export function ReporteSatisfaccion({ query }: { query: QueryReporte }) {
    const { puede } = useSesion();
    const lectura = useLectura((signal) => leerSatisfaccion(query, signal));
    return (
        <MarcoReporte reporte="satisfaccion" query={query} lectura={lectura} reintentar={lectura.reintentar}>
            {(r) => {
                const t = r.total;
                if (t.enviadas === 0) {
                    return <EstadoVacio icono={Inbox} titulo="Sin encuestas en el período" descripcion="Se envían al resolver un ticket o cerrar con éxito una instalación o visita." />;
                }
                const dist = [...distribucion(t)].reverse();
                return (
                    <>
                        <FilaIndicadores>
                            <Indicador etiqueta="Promedio" valor={textoPromedio(t.promedio)} detalle={`De ${textoEntero(t.respondidas)} respondidas`} />
                            <Indicador etiqueta="Respondidas" valor={textoPorcentaje(t.porcentajeRespondidas)} detalle={`${textoEntero(t.respondidas)} de ${textoEntero(t.enviadas)} enviadas`} />
                            <Indicador etiqueta="Alertas" valor={textoEntero(t.alertas)} detalle={`Calificación de ${UMBRAL_ALERTA} o menos`} />
                            <Indicador etiqueta="Sin responder" valor={textoEntero(t.pendientes + t.vencidas)} detalle={`${textoEntero(t.pendientes)} pendientes · ${textoEntero(t.vencidas)} vencidas`} />
                        </FilaIndicadores>
                        {t.alertas > 0 ? (
                            <Aviso tono="warning" icono={TriangleAlert} titulo={`${textoEntero(t.alertas)} ${t.alertas === 1 ? "encuesta calificó" : "encuestas calificaron"} con ${UMBRAL_ALERTA} o menos`} data-testid="alertas">
                                {puede(["encuestas.ver"]) ? (
                                    <>
                                        Revísalas en{" "}
                                        <Link
                                            href={`/encuestas?desde=${query.desde}&hasta=${query.hasta}&calificacion=1%2C2${query.entidad ? `&entidad=${query.entidad}` : ""}`}
                                            className="font-medium text-primary underline-offset-4 hover:underline"
                                        >
                                            Encuestas
                                        </Link>{" "}
                                        para dar seguimiento.
                                    </>
                                ) : (
                                    "Pide a soporte que les dé seguimiento."
                                )}
                            </Aviso>
                        ) : null}
                        <Barras
                            titulo="Distribución de calificaciones"
                            hallazgo="Encuestas respondidas por calificación; las de 2 o menos son alertas."
                            series={[{ nombre: "Respuestas", clase: "bg-grafico-1" }]}
                            filas={dist.map((d) => {
                                const c = TEXTO_CALIFICACION[d.calificacion as 1 | 2 | 3 | 4 | 5];
                                return {
                                    etiqueta: `${d.calificacion} · ${c.texto}`,
                                    ...(d.calificacion <= UMBRAL_ALERTA ? { alerta: "alerta" } : {}),
                                    valores: [{ valor: d.cantidad, texto: textoEntero(d.cantidad) }],
                                };
                            })}
                        />
                        <Seccion titulo="Distribución">
                            <Tabla className="whitespace-nowrap">
                                <TablaEncabezado>
                                    <TablaFila>
                                        <TablaTitulo>Calificación</TablaTitulo>
                                        <TablaTitulo className="text-right">Respuestas</TablaTitulo>
                                        <TablaTitulo className="text-right">% de respondidas</TablaTitulo>
                                    </TablaFila>
                                </TablaEncabezado>
                                <TablaCuerpo>
                                    {dist.map((d) => (
                                        <TablaFila key={d.calificacion}>
                                            <TablaCelda>
                                                <span className="inline-flex items-center gap-1.5">
                                                    {TEXTO_CALIFICACION[d.calificacion as 1 | 2 | 3 | 4 | 5].texto} ({d.calificacion})
                                                    {d.calificacion <= UMBRAL_ALERTA ? (
                                                        <span className="inline-flex items-center gap-1 text-xs font-medium text-warning">
                                                            <TriangleAlert aria-hidden className="size-3.5" />
                                                            Alerta
                                                        </span>
                                                    ) : null}
                                                </span>
                                            </TablaCelda>
                                            <TablaCelda className="text-right">{textoEntero(d.cantidad)}</TablaCelda>
                                            <TablaCelda className="text-right">{textoPorcentaje(t.respondidas ? Math.round((d.cantidad / t.respondidas) * 1000) / 10 : null)}</TablaCelda>
                                        </TablaFila>
                                    ))}
                                </TablaCuerpo>
                            </Tabla>
                        </Seccion>
                        <Seccion titulo="Por tipo">
                            {(r.porEntidad ?? []).length === 0 ? (
                                <p className="text-sm text-muted-foreground">Sin filas en el período.</p>
                            ) : (
                                <Tabla className="whitespace-nowrap">
                                    <TablaEncabezado>
                                        <TablaFila>
                                            <TablaTitulo>Tipo</TablaTitulo>
                                            <TablaTitulo className="text-right">Enviadas</TablaTitulo>
                                            <TablaTitulo className="text-right">Respondidas</TablaTitulo>
                                            <TablaTitulo className="text-right">% respondidas</TablaTitulo>
                                            <TablaTitulo className="text-right">Promedio</TablaTitulo>
                                            <TablaTitulo className="text-right">Alertas</TablaTitulo>
                                            <TablaTitulo className="text-right">Pendientes</TablaTitulo>
                                            <TablaTitulo className="text-right">Vencidas</TablaTitulo>
                                        </TablaFila>
                                    </TablaEncabezado>
                                    <TablaCuerpo>
                                        {(r.porEntidad ?? []).map((f, i) => (
                                            <TablaFila key={f.entidad ?? i}>
                                                <TablaCelda className="font-medium">{f.entidad ? ENTIDADES_PLURAL[f.entidad] : "Sin tipo"}</TablaCelda>
                                                <TablaCelda className="text-right">{textoEntero(f.enviadas)}</TablaCelda>
                                                <TablaCelda className="text-right">{textoEntero(f.respondidas)}</TablaCelda>
                                                <TablaCelda className="text-right font-medium">{textoPorcentaje(f.porcentajeRespondidas)}</TablaCelda>
                                                <TablaCelda className="text-right">{textoPromedio(f.promedio)}</TablaCelda>
                                                <TablaCelda className="text-right">{textoEntero(f.alertas)}</TablaCelda>
                                                <TablaCelda className="text-right">{textoEntero(f.pendientes)}</TablaCelda>
                                                <TablaCelda className="text-right">{textoEntero(f.vencidas)}</TablaCelda>
                                            </TablaFila>
                                        ))}
                                    </TablaCuerpo>
                                </Tabla>
                            )}
                        </Seccion>
                    </>
                );
            }}
        </MarcoReporte>
    );
}
