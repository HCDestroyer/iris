// GPS nativo: expo-location (permisos y actualizaciones en segundo plano de la tarea) y una lectura
// suelta para «En camino», «Llegué» y el cierre. Lo que decide cuándo encender está en `ubicacion.ts`.
import * as Location from "expo-location";
import { INTERVALO_MS, TAREA_UBICACION, type EstadoPermiso, type GpsFondo, type PermisosGps } from "./ubicacion";

function estadoDe(p: Location.LocationPermissionResponse): EstadoPermiso {
    return p.granted ? "concedido" : p.canAskAgain ? "preguntar" : "negado";
}

async function leerPermisos(): Promise<PermisosGps> {
    const [primerPlano, fondo] = await Promise.all([Location.getForegroundPermissionsAsync(), Location.getBackgroundPermissionsAsync()]);
    return { primerPlano: estadoDe(primerPlano), fondo: estadoDe(fondo) };
}

export const gpsNativo: GpsFondo = {
    permisos: leerPermisos,
    async pedirPermisos() {
        const primerPlano = await Location.requestForegroundPermissionsAsync();
        if (primerPlano.granted) {
            // Android 11+ manda a Ajustes para «Permitir todo el tiempo»; si lo niega, se rastrea con la app abierta.
            await Location.requestBackgroundPermissionsAsync().catch(() => undefined);
        }
        return leerPermisos();
    },
    async iniciar() {
        await Location.startLocationUpdatesAsync(TAREA_UBICACION, {
            accuracy: Location.Accuracy.High,
            timeInterval: INTERVALO_MS,
            distanceInterval: 25,
            deferredUpdatesInterval: INTERVALO_MS,
            pausesUpdatesAutomatically: false,
            activityType: Location.ActivityType.AutomotiveNavigation,
            showsBackgroundLocationIndicator: true,
            foregroundService: {
                notificationTitle: "Iris: orden en curso",
                notificationBody: "Se comparte tu ubicación con despacho hasta que pauses o cierres la orden.",
                killServiceOnDestroy: false,
            },
        });
    },
    async detener() {
        await Location.stopLocationUpdatesAsync(TAREA_UBICACION);
    },
    activo: () => Location.hasStartedLocationUpdatesAsync(TAREA_UBICACION).catch(() => false),
};

export interface Posicion {
    lat: number;
    lon: number;
    precision: number;
}

/**
 * Posición para una acción si el permiso ya está dado (nunca pregunta aquí) y llega a tiempo; si
 * no, `null` y la acción se registra igual. Acepta una lectura de hasta 1 min.
 */
export async function posicionSiPermitida(esperaMs = 8000): Promise<Posicion | null> {
    try {
        if (!(await Location.getForegroundPermissionsAsync()).granted) {
            return null;
        }
        const reciente = await Location.getLastKnownPositionAsync({ maxAge: 60_000, requiredAccuracy: 100 });
        const lectura =
            reciente ??
            (await Promise.race([
                Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.High }),
                new Promise<null>((resolver) => setTimeout(() => resolver(null), esperaMs)),
            ]));
        if (!lectura) {
            return null;
        }
        return {
            lat: Math.round(lectura.coords.latitude * 1e6) / 1e6,
            lon: Math.round(lectura.coords.longitude * 1e6) / 1e6,
            precision: Math.round(lectura.coords.accuracy ?? 0),
        };
    } catch {
        return null;
    }
}
