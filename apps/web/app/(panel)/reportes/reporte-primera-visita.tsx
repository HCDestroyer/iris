"use client";

import { EstadoVacio, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo } from "@iris/ui";
import { Inbox } from "lucide-react";
import { Barras, FilaIndicadores, Indicador } from "@/componentes/reportes/graficos";
import { MarcoReporte, Seccion } from "@/componentes/reportes/marco";
import { useLectura } from "@/lib/lectura";
import { leerPrimeraVisita, type FilaPrimeraVisita } from "@/lib/reportes/api";
import type { QueryReporte } from "@/lib/reportes/filtros";
import { textoEntero, textoPorcentaje, TIPOS_PRIMERA_VISITA } from "@/lib/reportes/presentacion";

const tipoDe = (f: FilaPrimeraVisita) => (f.tipo ? (TIPOS_PRIMERA_VISITA[f.tipo] ?? f.tipo) : "Sin tipo");
const tecnicoDe = (f: FilaPrimeraVisita) => f.tecnico ?? (f.tecnicoId ? `Técnico ${f.tecnicoId}` : "Sin técnico");

export function ReportePrimeraVisita({ query }: { query: QueryReporte }) {
    const lectura = useLectura((signal) => leerPrimeraVisita(query, signal));
    return (
        <MarcoReporte reporte="primera-visita" query={query} lectura={lectura} reintentar={lectura.reintentar}>
            {(r) => {
                const t = r.total;
                if (t.cerradas === 0) {
                    return <EstadoVacio icono={Inbox} titulo="Sin instalaciones ni visitas cerradas con éxito en el período" descripcion="Prueba con otro período o quita filtros." />;
                }
                const ventana = r.dias === 1 ? "1 día" : `${r.dias} días`;
                return (
                    <>
                        <FilaIndicadores>
                            <Indicador etiqueta="Resueltas en la primera visita" valor={textoPorcentaje(t.porcentaje)} detalle={`${textoEntero(t.resueltas)} de ${textoEntero(t.evaluadas)} evaluadas, sin retorno en ${ventana}`} />
                            <Indicador etiqueta="Con retorno" valor={textoEntero(t.conRetorno)} detalle="Otra orden o ticket del mismo servicio" />
                            <Indicador etiqueta="En observación" valor={textoEntero(t.enObservacion)} detalle={`Aún no cumplen los ${ventana}; no cuentan en el %`} />
                            <Indicador etiqueta="Sin servicio" valor={textoEntero(t.sinServicio)} detalle="No se pueden evaluar; no cuentan en el %" />
                        </FilaIndicadores>
                        <Barras
                            titulo="Primera visita resuelta por tipo"
                            hallazgo={`Porcentaje de las evaluadas sin retorno en ${ventana}.`}
                            series={[{ nombre: "Resueltas", clase: "bg-grafico-1" }]}
                            maximo={100}
                            filas={(r.porTipo ?? []).map((f) => ({
                                etiqueta: tipoDe(f),
                                detalle: `${f.resueltas} de ${f.evaluadas} evaluadas · ${f.enObservacion} en observación · ${f.sinServicio} sin servicio`,
                                valores: [{ valor: f.porcentaje, texto: textoPorcentaje(f.porcentaje) }],
                            }))}
                        />
                        <Seccion titulo="Por tipo">
                            <TablaPrimeraVisita filas={r.porTipo ?? []} columna="Tipo" etiqueta={tipoDe} />
                        </Seccion>
                        <Seccion titulo="Por técnico">
                            <TablaPrimeraVisita filas={r.porTecnico ?? []} columna="Técnico" etiqueta={tecnicoDe} />
                        </Seccion>
                    </>
                );
            }}
        </MarcoReporte>
    );
}

function TablaPrimeraVisita({ filas, columna, etiqueta }: { filas: readonly FilaPrimeraVisita[]; columna: string; etiqueta: (f: FilaPrimeraVisita) => string }) {
    if (filas.length === 0) return <p className="text-sm text-muted-foreground">Sin filas en el período.</p>;
    return (
        <Tabla className="whitespace-nowrap">
            <TablaEncabezado>
                <TablaFila>
                    <TablaTitulo>{columna}</TablaTitulo>
                    <TablaTitulo className="text-right">Cerradas</TablaTitulo>
                    <TablaTitulo className="text-right">Evaluadas</TablaTitulo>
                    <TablaTitulo className="text-right">Resueltas</TablaTitulo>
                    <TablaTitulo className="text-right">Con retorno</TablaTitulo>
                    <TablaTitulo className="text-right">% primera visita</TablaTitulo>
                    <TablaTitulo className="text-right">En observación</TablaTitulo>
                    <TablaTitulo className="text-right">Sin servicio</TablaTitulo>
                </TablaFila>
            </TablaEncabezado>
            <TablaCuerpo>
                {filas.map((f, i) => (
                    <TablaFila key={`${f.tipo ?? ""}${f.tecnicoId ?? ""}-${i}`}>
                        <TablaCelda className="font-medium">{etiqueta(f)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.cerradas)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.evaluadas)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.resueltas)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.conRetorno)}</TablaCelda>
                        <TablaCelda className="text-right font-medium">{textoPorcentaje(f.porcentaje)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.enObservacion)}</TablaCelda>
                        <TablaCelda className="text-right">{textoEntero(f.sinServicio)}</TablaCelda>
                    </TablaFila>
                ))}
            </TablaCuerpo>
        </Tabla>
    );
}
