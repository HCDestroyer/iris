"use client";

import type { Schema } from "@iris/contratos";
import { Boton, Esqueleto, EstadoVacio, GrupoSegmentado, Tarjeta } from "@iris/ui";
import { MapPin, RotateCw, Smartphone, Users } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { InsigniaActividad } from "@/componentes/accesos/insignias";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ESTADOS_ACTIVIDAD, nombrePlataforma, ORDEN_ESTADOS, textoActividad, type ActividadTecnico, type EstadoActividad } from "@/lib/accesos/presentacion";
import { api, resolver } from "@/lib/api/cliente";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";

type Filtro = "todos" | EstadoActividad;

/** Administración → Actividad de la app: cada técnico con su indicador «al día / atrasado / sin conexión». */
export function ActividadApp() {
    const lectura = useLectura<Schema<"ActividadCampo">>((signal) => resolver(api().GET("/api/v1/actividad-campo", { signal })));
    const [filtro, setFiltro] = useState<Filtro>("todos");
    const [actualizando, setActualizando] = useState(false);

    async function actualizar() {
        setActualizando(true);
        await lectura.refrescar();
        setActualizando(false);
    }

    const encabezado = (
        <EncabezadoPagina
            titulo="Actividad de la app"
            descripcion="Última conexión y sincronización de la app de campo de cada técnico, su versión, lo que tiene sin enviar y su última ubicación."
            acciones={
                <Boton variante="secundario" onClick={() => void actualizar()} disabled={actualizando || lectura.estado === "cargando"}>
                    <RotateCw />
                    {actualizando ? "Actualizando…" : "Actualizar"}
                </Boton>
            }
        />
    );

    if (lectura.estado === "cargando") {
        return (
            <>
                {encabezado}
                <div className="grid gap-3" aria-busy="true">
                    <span className="sr-only" role="status">
                        Cargando la actividad…
                    </span>
                    <Esqueleto className="h-28" />
                    <Esqueleto className="h-28" />
                </div>
            </>
        );
    }
    if (lectura.estado === "error") {
        return (
            <>
                {encabezado}
                <ErrorLectura error={lectura.error} que="la actividad de la app" reintentar={lectura.reintentar} />
            </>
        );
    }
    const { data, sinConexionMin, atrasoMin, generadoEn } = lectura.datos;
    const tecnicos = data ?? [];
    // «Hace cuánto» respecto de la hora en que la API armó la lista (no del reloj del navegador).
    const ahora = Date.parse(generadoEn);
    const cuenta = (e: EstadoActividad) => tecnicos.filter((t) => t.estado === e).length;
    const visibles = filtro === "todos" ? tecnicos : tecnicos.filter((t) => t.estado === filtro);
    return (
        <>
            {encabezado}
            <div className="mb-4 grid gap-3">
                <GrupoSegmentado<Filtro>
                    etiqueta="Filtrar por estado"
                    valor={filtro}
                    alCambiar={setFiltro}
                    opciones={[
                        { valor: "todos", etiqueta: `Todos (${tecnicos.length})` },
                        ...ORDEN_ESTADOS.map((e) => ({ valor: e, etiqueta: `${ESTADOS_ACTIVIDAD[e].etiqueta} (${cuenta(e)})`, icono: ESTADOS_ACTIVIDAD[e].icono })),
                    ]}
                />
                <p className="text-xs text-muted-foreground">
                    Sin conexión: nada en {sinConexionMin} min. Atrasado: tiene cambios sin enviar y no sincroniza hace {atrasoMin} min. Datos de{" "}
                    {formatearFecha(generadoEn)}.
                </p>
            </div>
            {visibles.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio
                        icono={Users}
                        titulo={tecnicos.length === 0 ? "Sin técnicos" : "Ningún técnico en este estado"}
                        descripcion={tecnicos.length === 0 ? "Los técnicos con ficha de agenda activa en tu alcance aparecerán aquí." : "Prueba con otro filtro."}
                    />
                </Tarjeta>
            ) : (
                <ul className="grid gap-3 lg:grid-cols-2" aria-label="Técnicos">
                    {visibles.map((t) => (
                        <li key={t.usuarioId} data-tecnico={t.usuarioId}>
                            <TarjetaTecnico t={t} ahora={ahora} />
                        </li>
                    ))}
                </ul>
            )}
        </>
    );
}

function Dato({ etiqueta, children }: { etiqueta: string; children: ReactNode }) {
    return (
        <div className="grid gap-0.5">
            <dt className="text-xs text-muted-foreground">{etiqueta}</dt>
            <dd className="text-sm">{children}</dd>
        </div>
    );
}

function cuando(iso: string | null | undefined, ahora: number): string {
    return iso ? haceCuanto(iso, ahora) : "Nunca";
}

function TarjetaTecnico({ t, ahora }: { t: ActividadTecnico; ahora: number }) {
    const pos = t.ultimaPosicion;
    return (
        <Tarjeta className="grid gap-3 p-4">
            <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="grid min-w-0 gap-0.5">
                    <Link href={`/usuarios/${t.usuarioId}`} className="truncate font-medium underline-offset-2 hover:underline">
                        {t.nombre}
                    </Link>
                    <span className="truncate text-xs text-muted-foreground">{t.correo}</span>
                </div>
                <InsigniaActividad estado={t.estado} texto={textoActividad(t, ahora)} />
            </div>
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3">
                <Dato etiqueta="Última conexión">
                    <span title={formatearFecha(t.ultimaConexion)}>{cuando(t.ultimaConexion, ahora)}</span>
                </Dato>
                <Dato etiqueta="Última sincronización">
                    <span title={formatearFecha(t.ultimaSincronizacion)}>{cuando(t.ultimaSincronizacion, ahora)}</span>
                </Dato>
                <Dato etiqueta="Sin enviar">{t.pendientes === null || t.pendientes === undefined ? "No lo informa" : String(t.pendientes)}</Dato>
                <Dato etiqueta="Rechazadas (7 días)">
                    <span className={t.rechazadas > 0 ? "font-medium text-error" : undefined}>{t.rechazadas}</span>
                </Dato>
                <Dato etiqueta="App">
                    <span className="inline-flex items-center gap-1">
                        <Smartphone aria-hidden className="size-3.5 text-muted-foreground" />
                        {t.app ? `${t.app.version} · ${nombrePlataforma(t.app.plataforma)}` : t.plataforma ? `${nombrePlataforma(t.plataforma)} · versión sin informar` : "Sin datos"}
                    </span>
                </Dato>
                <Dato etiqueta="Última ubicación">
                    {pos ? (
                        <span className="inline-flex items-center gap-1 tabular-nums" title={formatearFecha(pos.registradaEn)}>
                            <MapPin aria-hidden className="size-3.5 text-muted-foreground" />
                            {pos.lat.toFixed(5)}, {pos.lon.toFixed(5)} · {haceCuanto(pos.registradaEn, ahora)}
                        </span>
                    ) : (
                        "Sin ubicación en 24 h"
                    )}
                </Dato>
            </dl>
        </Tarjeta>
    );
}
