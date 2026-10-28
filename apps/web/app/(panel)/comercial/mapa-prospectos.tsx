"use client";

import { Aviso, Boton, InsigniaEstado } from "@iris/ui";
import { ChevronDown, CircleCheck, HardHat, RotateCw } from "lucide-react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { queryDe, type Filtros } from "@/lib/comercial/filtros";
import { ESTADOS_PROSPECTO, ORDEN_ESTADOS, textoDistancia, textoMotivo, type Cobertura, type Prospecto } from "@/lib/comercial/presentacion";
import { useLectura } from "@/lib/lectura";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";
import { MapaComercial, type PuntoProspecto } from "@/componentes/comercial/mapa-comercial";

const LIMITE = 200;

/** Prospectos con coordenadas (`conCoordenadas=true`, con los filtros) y la cobertura de la red. */
export function MapaProspectos({ filtros }: { filtros: Filtros }) {
    const { yo } = useSesion();
    const query = queryDe(filtros, yo.usuario.id, { conCoordenadas: true });
    const paginado = usePaginado<Prospecto>((cursor, signal) =>
        resolver(api().GET("/api/v1/prospectos", { params: { query: { ...query, limit: LIMITE, ...(cursor ? { cursor } : {}) } }, ...(signal ? { signal } : {}) })),
    );
    const cobertura = useLectura<Cobertura>((signal) => resolver(api().GET("/api/v1/comercial/cobertura", { signal })));
    const puntos: PuntoProspecto[] = paginado.items.flatMap((p) =>
        p.lat !== null && p.lon !== null ? [{ id: p.id, numero: p.numero, nombre: p.nombre, estado: p.estado, lat: p.lat, lon: p.lon }] : [],
    );
    const datosCobertura = cobertura.estado === "listo" ? cobertura.datos : null;

    return (
        <div className="grid gap-3">
            {paginado.primera.estado === "error" ? <ErrorLectura error={paginado.primera.error} que="los prospectos del mapa" reintentar={paginado.primera.reintentar} /> : null}
            {cobertura.estado === "error" ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo cargar la cobertura"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={cobertura.reintentar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {mensajeDeError(cobertura.error).detalle ?? "El mapa muestra solo los prospectos; no se dibuja cobertura que no se pudo leer."}
                </Aviso>
            ) : datosCobertura && !datosCobertura.disponible ? (
                <Aviso tono="info" titulo="Sin datos de cobertura">
                    {textoMotivo(datosCobertura.motivo)} No se dibuja la cobertura: no saberla no es lo mismo que no tenerla.
                </Aviso>
            ) : null}
            <MapaComercial prospectos={puntos} cobertura={datosCobertura} className="h-[28rem]" />
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm" aria-label="Leyenda del mapa">
                <span className="text-muted-foreground" role="status">
                    {paginado.primera.estado === "listo"
                        ? `${puntos.length} prospectos con ubicación${paginado.siguiente ? " (hay más)" : ""}`
                        : paginado.primera.estado === "cargando"
                          ? "Cargando prospectos…"
                          : ""}
                </span>
                {datosCobertura?.disponible ? (
                    <>
                        <InsigniaEstado tono="success" icono={CircleCheck} texto={`Viable hasta ${textoDistancia(datosCobertura.umbrales.viableM)}`} />
                        <InsigniaEstado tono="warning" icono={HardHat} texto={`Con obra hasta ${textoDistancia(datosCobertura.umbrales.obraM)}`} />
                        <span className="text-muted-foreground">{(datosCobertura.routers ?? []).length} routers</span>
                    </>
                ) : null}
            </div>
            <ul className="flex flex-wrap gap-2" aria-label="Estados">
                {ORDEN_ESTADOS.map((e) => {
                    const p = ESTADOS_PROSPECTO[e];
                    return (
                        <li key={e}>
                            <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} />
                        </li>
                    );
                })}
            </ul>
            {paginado.siguiente ? (
                <Boton variante="contorno" tamano="sm" className="justify-self-center" onClick={paginado.cargarMas} disabled={paginado.cargandoMas}>
                    <ChevronDown />
                    {paginado.cargandoMas ? "Cargando…" : "Cargar más prospectos en el mapa"}
                </Boton>
            ) : null}
            {paginado.errorMas ? (
                <Aviso tono="error" titulo="No se pudieron cargar más prospectos">
                    {mensajeDeError(paginado.errorMas).detalle}
                </Aviso>
            ) : null}
        </div>
    );
}
