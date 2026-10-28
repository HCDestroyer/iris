"use client";

import "maplibre-gl/dist/maplibre-gl.css";
import { Aviso, Boton, cn } from "@iris/ui";
import { Crosshair, Router as IconoRouter } from "lucide-react";
import type * as MapLibreGL from "maplibre-gl";
import type { GeoJSONSource, Map as MapaLibre, Marker, StyleSpecification } from "maplibre-gl";
import Link from "next/link";
import { useTheme } from "next-themes";
import { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { geojsonCobertura } from "@/lib/comercial/mapa";
import { ESTADOS_PROSPECTO, textoDistancia, type Cobertura, type EstadoProspecto } from "@/lib/comercial/presentacion";
import { aplicarTemaTeselas, CENTRO_GUATEMALA, encuadrar, TESELAS_OSM, TEXTO_TONO, pinturaTeselas, token, URL_WORKER } from "../mapa/base";

/*
 * Mapa comercial: prospectos con coordenadas (marcador por estado: icono + número + color) y la
 * cobertura de la red (un círculo por router con el radio viable y otro con el de requiere obra).
 * Los marcadores son HTML de React (se leen y se prueban sin WebGL); la cobertura, capas GeoJSON.
 */

export type PuntoProspecto = { id: number; numero: string; nombre: string; estado: EstadoProspecto; lat: number; lon: number };

function colores() {
    return { viable: token("--hx-success", "#16a34a"), obra: token("--hx-warning", "#d97706") };
}

function estilo(oscuro: boolean): StyleSpecification {
    const c = colores();
    return {
        version: 8,
        sources: {
            osm: { type: "raster", tiles: [TESELAS_OSM], tileSize: 256, maxzoom: 19, attribution: "© Colaboradores de OpenStreetMap" },
            cobertura: { type: "geojson", data: { type: "FeatureCollection", features: [] } },
        },
        layers: [
            { id: "osm", type: "raster", source: "osm", paint: pinturaTeselas(oscuro) },
            { id: "cobertura-obra", type: "fill", source: "cobertura", filter: ["==", ["get", "capa"], "obra"], paint: { "fill-color": c.obra, "fill-opacity": 0.12 } },
            { id: "cobertura-obra-borde", type: "line", source: "cobertura", filter: ["==", ["get", "capa"], "obra"], paint: { "line-color": c.obra, "line-width": 1.5, "line-dasharray": [3, 2] } },
            { id: "cobertura-viable", type: "fill", source: "cobertura", filter: ["==", ["get", "capa"], "viable"], paint: { "fill-color": c.viable, "fill-opacity": 0.18 } },
            { id: "cobertura-viable-borde", type: "line", source: "cobertura", filter: ["==", ["get", "capa"], "viable"], paint: { "line-color": c.viable, "line-width": 1.5 } },
        ],
    };
}

type Marcadores = Map<string, { marker: Marker; elemento: HTMLDivElement }>;

export function MapaComercial({ prospectos, cobertura, className }: { prospectos: readonly PuntoProspecto[]; cobertura: Cobertura | null; className?: string }) {
    const contenedor = useRef<HTMLDivElement>(null);
    const mapa = useRef<MapaLibre | null>(null);
    const libreria = useRef<typeof MapLibreGL | null>(null);
    const marcadores = useRef<Marcadores>(new Map());
    const [listo, setListo] = useState(false);
    const [fallo, setFallo] = useState<string | null>(null);
    const [sinTeselas, setSinTeselas] = useState(false);
    const [elementos, setElementos] = useState<ReadonlyMap<string, HTMLDivElement>>(new Map());
    const encuadrado = useRef(false);
    const { resolvedTheme } = useTheme();
    const oscuro = resolvedTheme === "dark";
    const oscuroInicial = useRef(oscuro);
    const routers = useMemo(() => (cobertura?.disponible ? (cobertura.routers ?? []) : []), [cobertura]);

    useEffect(() => {
        let cancelado = false;
        const actuales = marcadores.current;
        import("maplibre-gl")
            .then((ml) => {
                if (cancelado || !contenedor.current) return;
                libreria.current = ml;
                if (!ml.getWorkerUrl()) ml.setWorkerUrl(URL_WORKER);
                try {
                    const m = new ml.Map({
                        container: contenedor.current,
                        style: estilo(oscuroInicial.current),
                        center: CENTRO_GUATEMALA,
                        zoom: 11,
                        attributionControl: { compact: true },
                        cooperativeGestures: true,
                    });
                    m.addControl(new ml.NavigationControl({ showCompass: false }), "top-right");
                    m.on("load", () => !cancelado && setListo(true));
                    m.on("error", (e) => {
                        if ("sourceId" in e && e.sourceId === "osm") setSinTeselas(true);
                    });
                    mapa.current = m;
                } catch {
                    setFallo("Este navegador no pudo dibujar el mapa (necesita WebGL). La lista sigue disponible.");
                }
            })
            .catch(() => !cancelado && setFallo("No se pudo cargar el mapa. Recarga la página para intentarlo de nuevo."));
        return () => {
            cancelado = true;
            for (const { marker } of actuales.values()) marker.remove();
            actuales.clear();
            mapa.current?.remove();
            mapa.current = null;
        };
    }, []);

    useEffect(() => {
        const m = mapa.current;
        if (!m || !listo) return;
        aplicarTemaTeselas(m, oscuro);
        requestAnimationFrame(() => {
            if (mapa.current !== m) return;
            const c = colores();
            m.setPaintProperty("cobertura-obra", "fill-color", c.obra);
            m.setPaintProperty("cobertura-obra-borde", "line-color", c.obra);
            m.setPaintProperty("cobertura-viable", "fill-color", c.viable);
            m.setPaintProperty("cobertura-viable-borde", "line-color", c.viable);
        });
    }, [oscuro, listo]);

    useEffect(() => {
        const m = mapa.current;
        if (!m || !listo) return;
        m.getSource<GeoJSONSource>("cobertura")?.setData(geojsonCobertura(cobertura));
    }, [cobertura, listo]);

    useEffect(() => {
        const m = mapa.current;
        const ml = libreria.current;
        if (!m || !ml) return;
        const deseados = new Map<string, [number, number]>();
        for (const p of prospectos) deseados.set(`p${p.id}`, [p.lon, p.lat]);
        for (const r of routers) deseados.set(`r${r.id}`, [r.lon, r.lat]);
        const actuales = marcadores.current;
        let cambio = false;
        for (const [clave, { marker }] of actuales) {
            if (!deseados.has(clave)) {
                marker.remove();
                actuales.delete(clave);
                cambio = true;
            }
        }
        for (const [clave, lngLat] of deseados) {
            const existente = actuales.get(clave);
            if (existente) {
                existente.marker.setLngLat(lngLat);
            } else {
                const elemento = document.createElement("div");
                const marker = new ml.Marker({ element: elemento, anchor: "center" }).setLngLat(lngLat).addTo(m);
                actuales.set(clave, { marker, elemento });
                cambio = true;
            }
        }
        if (cambio) setElementos(new Map([...actuales].map(([k, v]) => [k, v.elemento])));
        if (!encuadrado.current && deseados.size > 0) {
            encuadrado.current = true;
            encuadrar(m, ml, [...deseados.values()]);
        }
    }, [prospectos, routers, listo]);

    function centrar() {
        const m = mapa.current;
        const ml = libreria.current;
        if (!m || !ml) return;
        encuadrar(m, ml, [...prospectos.map((p): [number, number] => [p.lon, p.lat]), ...routers.map((r): [number, number] => [r.lon, r.lat])]);
    }

    return (
        <div className={cn("relative overflow-hidden rounded-lg border border-border bg-elevated", className)}>
            <div
                ref={contenedor}
                role="region"
                aria-label="Mapa de prospectos y cobertura"
                className="h-full min-h-80 w-full"
                data-testid="mapa"
                data-listo={listo ? "si" : "no"}
                data-cobertura={geojsonCobertura(cobertura).features.length}
            />
            {fallo ? (
                <div className="absolute inset-0 grid place-items-center p-4">
                    <Aviso tono="warning" titulo="Mapa no disponible">
                        {fallo}
                    </Aviso>
                </div>
            ) : (
                <Boton variante="secundario" tamano="sm" className="absolute top-2 left-2 shadow" onClick={centrar}>
                    <Crosshair />
                    Centrar
                </Boton>
            )}
            {sinTeselas && !fallo ? (
                <p role="status" className="absolute right-2 bottom-8 left-2 rounded-md bg-background/90 px-2 py-1 text-xs text-muted-foreground">
                    No se pudieron cargar todas las teselas del mapa; los marcadores siguen al día.
                </p>
            ) : null}
            {prospectos.map((p) => {
                const el = elementos.get(`p${p.id}`);
                return el ? createPortal(<MarcadorProspecto punto={p} />, el, `p${p.id}`) : null;
            })}
            {routers.map((r) => {
                const el = elementos.get(`r${r.id}`);
                const texto = `${r.nombre} · viable hasta ${textoDistancia(cobertura?.umbrales.viableM)}, con obra hasta ${textoDistancia(cobertura?.umbrales.obraM)}`;
                return el
                    ? createPortal(
                          <div role="img" aria-label={`Router ${texto}`} title={texto} data-marcador-router={r.id} className="grid size-7 place-items-center rounded-full border-2 border-background bg-foreground text-background shadow-md">
                              <IconoRouter aria-hidden className="size-4" />
                          </div>,
                          el,
                          `r${r.id}`,
                      )
                    : null;
            })}
        </div>
    );
}

function MarcadorProspecto({ punto }: { punto: PuntoProspecto }) {
    const e = ESTADOS_PROSPECTO[punto.estado];
    const Icono = e.icono;
    const texto = `${punto.numero} · ${punto.nombre} · ${e.texto}`;
    return (
        <Link
            href={`/comercial/${punto.id}`}
            aria-label={texto}
            title={texto}
            data-marcador-prospecto={punto.numero}
            data-estado={punto.estado}
            className="flex items-center gap-1 rounded-md border border-border bg-background px-1.5 py-0.5 text-[0.6875rem] font-medium whitespace-nowrap text-foreground shadow hover:border-primary"
        >
            <Icono aria-hidden className={cn("size-3.5", TEXTO_TONO[e.tono])} />
            {punto.numero}
        </Link>
    );
}
