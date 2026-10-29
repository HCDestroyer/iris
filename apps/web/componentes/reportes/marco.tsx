"use client";

import { Aviso, Boton, Esqueleto, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { FileDown, RotateCw } from "lucide-react";
import { useState, type ReactNode } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { mensajeDeError } from "@/lib/api/errores";
import type { EstadoLectura } from "@/lib/lectura";
import { descargarCsv } from "@/lib/reportes/api";
import { nombreCsv, type QueryReporte, type Reporte } from "@/lib/reportes/filtros";
import { NOMBRES_REPORTE, textoPeriodo } from "@/lib/reportes/presentacion";

/**
 * Tarjeta de un reporte: título, período, «Descargar CSV» (los mismos filtros con
 * `formato=csv`) y el contenido. Mientras carga o si falló, no hay ni un total a la vista.
 */
export function MarcoReporte<T>({
    reporte,
    query,
    lectura,
    reintentar,
    children,
}: {
    reporte: Reporte;
    query: QueryReporte;
    lectura: EstadoLectura<T>;
    reintentar: () => void;
    children: (datos: T) => ReactNode;
}) {
    const nombre = NOMBRES_REPORTE[reporte];
    return (
        <Tarjeta data-reporte={reporte} aria-busy={lectura.estado === "cargando"}>
            <TarjetaEncabezado className="flex flex-wrap items-start justify-between gap-3">
                <div className="grid min-w-0 gap-1">
                    <TarjetaTitulo>{nombre.titulo}</TarjetaTitulo>
                    <TarjetaDescripcion>{nombre.descripcion}</TarjetaDescripcion>
                    <p className="text-xs text-muted-foreground">
                        Período: {textoPeriodo(query.desde, query.hasta)} · hora de Guatemala
                        {query.dias !== undefined ? ` · ventana de ${query.dias === 1 ? "1 día" : `${query.dias} días`}` : ""}
                    </p>
                </div>
                <BotonCsv reporte={reporte} query={query} />
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-6">
                {lectura.estado === "cargando" ? (
                    <div className="grid gap-3" role="status" aria-label="Cargando el reporte">
                        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                            <Esqueleto className="h-20" />
                            <Esqueleto className="h-20" />
                            <Esqueleto className="h-20" />
                            <Esqueleto className="h-20" />
                        </div>
                        <Esqueleto className="h-48" />
                    </div>
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={reintentar} que={`el reporte «${nombre.titulo}»`} />
                ) : (
                    children(lectura.datos)
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

/** «Descargar CSV» con la sesión del panel; un 422 (`exportacion_excedida`…) muestra su `detail`. */
export function BotonCsv({ reporte, query }: { reporte: Reporte; query: QueryReporte }) {
    const [bajando, setBajando] = useState(false);
    const [error, setError] = useState<unknown>(null);

    async function descargar() {
        setBajando(true);
        setError(null);
        try {
            const blob = await descargarCsv(reporte, query);
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = nombreCsv(reporte, query);
            document.body.append(a);
            a.click();
            a.remove();
            setTimeout(() => URL.revokeObjectURL(url), 60_000);
        } catch (e) {
            setError(e);
        } finally {
            setBajando(false);
        }
    }

    const { titulo, detalle } = mensajeDeError(error);
    return (
        <div className="grid justify-items-start gap-2 sm:justify-items-end">
            <Boton variante="secundario" tamano="sm" onClick={descargar} disabled={bajando}>
                <FileDown />
                {bajando ? "Descargando…" : "Descargar CSV"}
            </Boton>
            {error ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo descargar el CSV"
                    className="max-w-md"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={descargar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {detalle ?? titulo}
                </Aviso>
            ) : null}
        </div>
    );
}

/** Tabla con su título visible (la vista accesible de cada gráfico). */
export function Seccion({ titulo, children }: { titulo: string; children: ReactNode }) {
    return (
        <section className="grid gap-2" aria-label={titulo}>
            <h3 className="text-sm font-medium">{titulo}</h3>
            {children}
        </section>
    );
}
