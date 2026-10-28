import type { Cobertura } from "./presentacion";

/*
 * Cobertura en el mapa comercial: un círculo por router con el radio del umbral viable y otro con
 * el de requiere obra (`GET /comercial/cobertura`). Con `disponible = false` no se dibuja nada:
 * no saber no es lo mismo que «sin cobertura».
 */

const METROS_POR_GRADO = 111_320;

/** Anillo cerrado `[lon, lat]` de un círculo de `radioM` metros (aproximación local, ≤ 50 km). */
export function circulo(lat: number, lon: number, radioM: number, pasos = 64): [number, number][] {
    const dLat = radioM / METROS_POR_GRADO;
    const dLon = radioM / (METROS_POR_GRADO * Math.max(Math.cos((lat * Math.PI) / 180), 1e-6));
    const anillo: [number, number][] = [];
    for (let i = 0; i < pasos; i++) {
        const a = (2 * Math.PI * i) / pasos;
        anillo.push([lon + dLon * Math.cos(a), lat + dLat * Math.sin(a)]);
    }
    const primero = anillo[0];
    if (primero) anillo.push(primero);
    return anillo;
}

export type CapaCobertura = "viable" | "obra";

export type ColeccionCobertura = {
    type: "FeatureCollection";
    features: {
        type: "Feature";
        properties: { capa: CapaCobertura; routerId: number };
        geometry: { type: "Polygon"; coordinates: [number, number][][] };
    }[];
};

/** GeoJSON de la cobertura; vacío si no está disponible. El de obra va primero (debajo). */
export function geojsonCobertura(c: Cobertura | null): ColeccionCobertura {
    const vacia: ColeccionCobertura = { type: "FeatureCollection", features: [] };
    if (!c || !c.disponible) return vacia;
    const routers = c.routers ?? [];
    const anillos = (capa: CapaCobertura, radio: number) =>
        routers
            .filter((r) => Number.isFinite(r.lat) && Number.isFinite(r.lon))
            .map((r) => ({
                type: "Feature" as const,
                properties: { capa, routerId: r.id },
                geometry: { type: "Polygon" as const, coordinates: [circulo(r.lat, r.lon, radio)] },
            }));
    return { type: "FeatureCollection", features: [...anillos("obra", c.umbrales.obraM), ...anillos("viable", c.umbrales.viableM)] };
}
