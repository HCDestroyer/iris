"use client";

import "maplibre-gl/dist/maplibre-gl.css";
import { Aviso, Boton, cn, type Tono } from "@iris/ui";
import { Crosshair, MapPin } from "lucide-react";
import type * as MapLibreGL from "maplibre-gl";
import type { GeoJSONSource, Map as MapaLibre, Marker, StyleSpecification } from "maplibre-gl";
import { useTheme } from "next-themes";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { FRESCURAS, type Frescura } from "@/lib/despacho/posiciones";
import { ESTADOS_ORDEN, type EstadoOrden } from "@/lib/ordenes/presentacion";
import { CENTRO_GUATEMALA, encuadrar, FONDO_TONO, TESELAS_OSM, TEXTO_TONO, token, URL_WORKER } from "../mapa/base";

/*
 * Mapa del despacho con MapLibre GL y teselas raster de OpenStreetMap (sin estilo remoto ni
 * fuentes: todo lo que se dibuja encima son marcadores HTML renderizados por React, así se leen
 * con lector de pantalla y se prueban sin WebGL de por medio). Si el navegador no puede crear el
 * mapa (sin WebGL), lo dice y el resto del despacho sigue funcionando con las listas.
 */

export { TESELAS_OSM };

export type PuntoOrden = {
    ordenId: number;
    numero: string;
    estado: EstadoOrden;
    lat: number;
    lon: number;
    detalle: string;
    /** Orden sin técnico (Mapa): marcador cuadrado en tono de aviso. */
    sinAsignar?: boolean;
};
export type PuntoTecnico = {
    tecnicoId: number;
    nombre: string;
    iniciales: string;
    lat: number;
    lon: number;
    frescura: Frescura;
    texto: string;
    /** Color del marcador por su estado del día (Mapa); sin él, por la frescura de la posición. */
    tono?: Tono;
    /** Etiqueta junto al marcador («Tomás · en sitio»). */
    etiqueta?: string;
};
export type LineaRuta = { id: "real" | "propuesta"; coordenadas: [number, number][] };
/** Pedido de centrar el mapa en un punto (`n` cambia en cada pedido). */
export type Enfoque = { lat: number; lon: number; n: number };

function estilo(oscuro: boolean): StyleSpecification {
    return {
        version: 8,
        sources: {
            osm: { type: "raster", tiles: [TESELAS_OSM], tileSize: 256, maxzoom: 19, attribution: "© Colaboradores de OpenStreetMap" },
            rutas: { type: "geojson", data: { type: "FeatureCollection", features: [] } },
        },
        layers: [
            {
                id: "osm",
                type: "raster",
                source: "osm",
                paint: oscuro ? { "raster-brightness-max": 0.62, "raster-saturation": -0.55, "raster-contrast": 0.1 } : {},
            },
            // Propuesta: continua, color de marca. Real (GPS): punteada, neutra encima.
            {
                id: "ruta-propuesta",
                type: "line",
                source: "rutas",
                filter: ["==", ["get", "id"], "propuesta"],
                paint: { "line-color": token("--hx-primary", "#0f766e"), "line-width": 4, "line-opacity": 0.85 },
                layout: { "line-cap": "round", "line-join": "round" },
            },
            {
                id: "ruta-real",
                type: "line",
                source: "rutas",
                filter: ["==", ["get", "id"], "real"],
                paint: { "line-color": token("--hx-text-muted", "#6b7280"), "line-width": 3, "line-dasharray": [2, 2] },
            },
        ],
    };
}

type Marcadores = Map<string, { marker: Marker; elemento: HTMLDivElement }>;

export function MapaDespacho({
    ordenes,
    tecnicos,
    lineas,
    enfoque,
    className,
    etiqueta = "Mapa del despacho",
    alElegirTecnico,
    tecnicoElegido,
    children,
}: {
    ordenes: readonly PuntoOrden[];
    tecnicos: readonly PuntoTecnico[];
    lineas: readonly LineaRuta[];
    enfoque: Enfoque | null;
    className?: string;
    /** Nombre accesible de la región del mapa. */
    etiqueta?: string;
    /** Con él, cada técnico es un botón (Mapa: abre su tarjeta). */
    alElegirTecnico?: (tecnicoId: number) => void;
    tecnicoElegido?: number | null;
    /** Capas encima del mapa (leyenda, tarjeta del técnico). */
    children?: ReactNode;
}) {
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

    // Crear el mapa una vez (la librería se carga solo en el navegador).
    useEffect(() => {
        let cancelado = false;
        const actuales = marcadores.current;
        import("maplibre-gl")
            .then((ml) => {
                if (cancelado || !contenedor.current) return;
                libreria.current = ml;
                // El empaquetador reescribe el import.meta.url de MapLibre y no encuentra su worker:
                // se lo indicamos con un recurso que webpack emite junto al build.
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
                    setFallo("Este navegador no pudo dibujar el mapa (necesita WebGL). Las listas siguen al día.");
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

    // Tema: aclara u oscurece las teselas sin recrear el mapa.
    useEffect(() => {
        const m = mapa.current;
        if (!m || !listo) return;
        const paint = estilo(oscuro).layers[0]?.paint as Record<string, number> | undefined;
        for (const p of ["raster-brightness-max", "raster-saturation", "raster-contrast"] as const) {
            m.setPaintProperty("osm", p, paint?.[p] ?? (p === "raster-brightness-max" ? 1 : 0));
        }
        // Los tokens cambian con la clase .dark: se leen después de que next-themes la aplica.
        requestAnimationFrame(() => {
            if (mapa.current !== m) return;
            m.setPaintProperty("ruta-propuesta", "line-color", token("--hx-primary", "#0f766e"));
            m.setPaintProperty("ruta-real", "line-color", token("--hx-text-muted", "#6b7280"));
        });
    }, [oscuro, listo]);

    // Marcadores: uno por orden y por técnico; React dibuja su contenido por portal.
    useEffect(() => {
        const m = mapa.current;
        const ml = libreria.current;
        if (!m || !ml) return;
        const deseados = new Map<string, [number, number]>();
        for (const o of ordenes) deseados.set(`o${o.ordenId}`, [o.lon, o.lat]);
        for (const t of tecnicos) deseados.set(`t${t.tecnicoId}`, [t.lon, t.lat]);
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
                const marker = new ml.Marker({ element: elemento, anchor: clave.startsWith("t") ? "center" : "bottom" }).setLngLat(lngLat).addTo(m);
                actuales.set(clave, { marker, elemento });
                cambio = true;
            }
        }
        if (cambio) setElementos(new Map([...actuales].map(([k, v]) => [k, v.elemento])));
        if (!encuadrado.current && deseados.size > 0) {
            encuadrado.current = true;
            encuadrar(m, ml, [...deseados.values()]);
        }
    }, [ordenes, tecnicos, listo]);

    // Rutas (real y propuesta) como líneas.
    useEffect(() => {
        const m = mapa.current;
        if (!m || !listo) return;
        const fuente = m.getSource<GeoJSONSource>("rutas");
        fuente?.setData({
            type: "FeatureCollection",
            features: lineas
                .filter((l) => l.coordenadas.length >= 2)
                .map((l) => ({ type: "Feature", properties: { id: l.id }, geometry: { type: "LineString", coordinates: l.coordenadas } })),
        });
    }, [lineas, listo]);

    useEffect(() => {
        if (enfoque && mapa.current) mapa.current.flyTo({ center: [enfoque.lon, enfoque.lat], zoom: Math.max(mapa.current.getZoom(), 14) });
    }, [enfoque]);

    function centrar() {
        const m = mapa.current;
        const ml = libreria.current;
        if (!m || !ml) return;
        encuadrar(m, ml, [...ordenes.map((o): [number, number] => [o.lon, o.lat]), ...tecnicos.map((t): [number, number] => [t.lon, t.lat])]);
    }

    return (
        <div className={cn("relative overflow-hidden rounded-lg border border-border bg-elevated", className)}>
            <div
                ref={contenedor}
                role="region"
                aria-label={etiqueta}
                className="h-full min-h-72 w-full"
                data-testid="mapa"
                data-listo={listo ? "si" : "no"}
                data-lineas={lineas
                    .filter((l) => l.coordenadas.length >= 2)
                    .map((l) => l.id)
                    .join(" ")}
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
            {ordenes.map((o) => {
                const el = elementos.get(`o${o.ordenId}`);
                return el ? createPortal(<MarcadorOrden punto={o} />, el, `o${o.ordenId}`) : null;
            })}
            {tecnicos.map((t) => {
                const el = elementos.get(`t${t.tecnicoId}`);
                return el
                    ? createPortal(
                          <MarcadorTecnico
                              punto={t}
                              {...(alElegirTecnico ? { alElegir: () => alElegirTecnico(t.tecnicoId) } : {})}
                              elegido={tecnicoElegido === t.tecnicoId}
                          />,
                          el,
                          `t${t.tecnicoId}`,
                      )
                    : null;
            })}
            {children}
        </div>
    );
}

function MarcadorOrden({ punto }: { punto: PuntoOrden }) {
    const e = ESTADOS_ORDEN[punto.estado];
    const Icono = e.icono;
    const texto = punto.sinAsignar ? "Sin asignar" : e.texto;
    if (punto.sinAsignar) {
        return (
            <div
                role="img"
                aria-label={`${punto.numero} · ${texto} · ${punto.detalle}`}
                title={`${punto.numero} · ${texto} · ${punto.detalle}`}
                data-marcador-orden={punto.numero}
                data-sin-asignar=""
                className="flex items-center gap-1.5"
            >
                <span className="size-4 rounded-[4px] border-2 border-background bg-warning shadow-[0_0_0_4px_color-mix(in_oklab,var(--hx-warning)_20%,transparent)]" />
                <span className="rounded-md border border-border bg-background px-2 py-0.5 text-[0.6875rem] font-semibold whitespace-nowrap text-foreground shadow">
                    {punto.numero}
                </span>
            </div>
        );
    }
    return (
        <div
            role="img"
            aria-label={`${punto.numero} · ${texto} · ${punto.detalle}`}
            title={`${punto.numero} · ${texto} · ${punto.detalle}`}
            data-marcador-orden={punto.numero}
            className="flex flex-col items-center"
        >
            <span className="flex items-center gap-1 rounded-md border border-border bg-background px-1.5 py-0.5 text-[0.6875rem] font-medium whitespace-nowrap text-foreground shadow">
                <Icono aria-hidden className={cn("size-3", TEXTO_TONO[e.tono])} />
                {punto.numero}
            </span>
            <MapPin aria-hidden className="size-5 fill-background text-foreground" />
        </div>
    );
}

function MarcadorTecnico({ punto, alElegir, elegido = false }: { punto: PuntoTecnico; alElegir?: () => void; elegido?: boolean }) {
    const f = FRESCURAS[punto.frescura];
    const tono = punto.tono ?? f.tono;
    const circulo = (
        <span
            className={cn(
                "grid size-8 shrink-0 place-items-center rounded-full border-2 border-background text-[0.6875rem] font-semibold text-white shadow-md",
                FONDO_TONO[tono],
                punto.frescura === "sin_senal" && "opacity-70",
                elegido && "ring-2 ring-primary ring-offset-2 ring-offset-background",
            )}
        >
            {punto.iniciales}
        </span>
    );
    const etiqueta = punto.etiqueta ? (
        <span className="rounded-md border border-border bg-background px-2 py-0.5 text-[0.6875rem] font-semibold whitespace-nowrap text-foreground shadow">
            {punto.etiqueta}
        </span>
    ) : null;
    if (alElegir) {
        return (
            <Boton
                variante="fantasma"
                className="h-auto gap-1.5 p-0 hover:bg-transparent"
                aria-label={`${punto.nombre} · ${punto.texto}`}
                aria-pressed={elegido}
                title={`${punto.nombre} · ${punto.texto}`}
                data-marcador-tecnico={punto.tecnicoId}
                data-frescura={punto.frescura}
                onClick={alElegir}
            >
                {circulo}
                {etiqueta}
            </Boton>
        );
    }
    return (
        <div
            role="img"
            aria-label={`${punto.nombre} · ${punto.texto}`}
            title={`${punto.nombre} · ${punto.texto}`}
            data-marcador-tecnico={punto.tecnicoId}
            data-frescura={punto.frescura}
            className="flex items-center gap-1.5"
        >
            {circulo}
            {etiqueta}
        </div>
    );
}
