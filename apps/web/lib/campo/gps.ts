export interface Posicion {
    lat: number;
    lon: number;
    precision: number;
}

/**
 * Posición actual si el técnico la permite y llega a tiempo; si no, `null` (la acción se registra
 * igual, sin GPS). Acepta una lectura de hasta 1 min para no hacer esperar.
 */
export function obtenerPosicion(esperaMs = 8000): Promise<Posicion | null> {
    if (typeof navigator === "undefined" || !navigator.geolocation) {
        return Promise.resolve(null);
    }
    return new Promise((resolver) => {
        const corte = setTimeout(() => resolver(null), esperaMs + 500);
        navigator.geolocation.getCurrentPosition(
            (p) => {
                clearTimeout(corte);
                resolver({
                    lat: Math.round(p.coords.latitude * 1e6) / 1e6,
                    lon: Math.round(p.coords.longitude * 1e6) / 1e6,
                    precision: Math.round(p.coords.accuracy),
                });
            },
            () => {
                clearTimeout(corte);
                resolver(null);
            },
            { enableHighAccuracy: true, timeout: esperaMs, maximumAge: 60_000 },
        );
    });
}

/** Enlace a la app de mapas del teléfono con destino en esas coordenadas (o la dirección). */
export function enlaceComoLlegar(lat: number | null, lon: number | null, direccion: string): string | null {
    if (lat !== null && lon !== null) {
        return `https://www.google.com/maps/dir/?api=1&destination=${lat},${lon}`;
    }
    const d = direccion.trim();
    return d ? `https://www.google.com/maps/dir/?api=1&destination=${encodeURIComponent(d)}` : null;
}
