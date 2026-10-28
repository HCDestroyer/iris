import { ColaPosiciones } from "@iris/sync";
import { AlmacenMemoria, TransportePosicionesFalso, RelojManual } from "@iris/sync/pruebas";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { puntoDeLectura, RastreadorUbicacion, type DependenciasUbicacion } from "./ubicacion";

const INICIO = Date.parse("2026-10-28T15:00:00Z");

type Modo = "ok" | "negado" | "sin_senal";

/** `navigator.geolocation` falso: responde según `modo` con la hora actual (relojes falsos de Vitest). */
class GeoFalsa {
    modo: Modo = "ok";
    lecturas = 0;
    getCurrentPosition(exito: PositionCallback, error?: PositionErrorCallback | null): void {
        this.lecturas += 1;
        if (this.modo === "ok") {
            const coords = { latitude: 14.6349, longitude: -90.5069, accuracy: 9, altitude: null, altitudeAccuracy: null, heading: null, speed: 1.5 };
            exito({ coords: { ...coords, toJSON: () => coords }, timestamp: Date.now(), toJSON: () => ({}) } as GeolocationPosition);
            return;
        }
        const code = this.modo === "negado" ? 1 : 3;
        error?.({ code, message: this.modo, PERMISSION_DENIED: 1, POSITION_UNAVAILABLE: 2, TIMEOUT: 3 } as GeolocationPositionError);
    }
    watchPosition(): number {
        return 0;
    }
    clearWatch(): void {}
}

function permisosFalsos(estado: PermissionState): Pick<Permissions, "query"> {
    const status = { state: estado, name: "geolocation", onchange: null } as unknown as PermissionStatus;
    return { query: () => Promise.resolve(status) };
}

async function montar(opciones: { permiso?: PermissionState | null; turno?: string; hoy?: () => string; sinGeo?: boolean } = {}) {
    const geo = new GeoFalsa();
    const almacen = new AlmacenMemoria();
    const reloj = new RelojManual(INICIO);
    const cola = await ColaPosiciones.abrir({ almacen, transporte: new TransportePosicionesFalso(reloj) });
    let turno = opciones.turno;
    let explicacion: string | undefined;
    const deps: DependenciasUbicacion = {
        geolocalizacion: opciones.sinGeo ? null : (geo as Geolocation),
        permisos: opciones.permiso === null ? null : permisosFalsos(opciones.permiso ?? "granted"),
        hoy: opciones.hoy ?? (() => "2026-10-28"),
        leerTurno: () => turno,
        guardarTurno: (f) => {
            turno = f ?? undefined;
        },
        leerExplicacion: () => explicacion,
        guardarExplicacion: (v) => {
            explicacion = v;
        },
        intervaloMs: 60_000,
        esperaLecturaMs: 20_000,
        documento: null,
    };
    const rastreador = new RastreadorUbicacion(cola, deps);
    const desmontar = rastreador.montar();
    await vi.advanceTimersByTimeAsync(0);
    return { geo, almacen, cola, rastreador, desmontar, turno: () => turno };
}

beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(INICIO);
});

afterEach(() => {
    vi.useRealTimers();
});

describe("ubicación en vivo (PWA)", () => {
    it("fuera de turno no lee; en turno guarda un punto al empezar y otro cada minuto", async () => {
        const { geo, almacen, rastreador, turno } = await montar();
        expect(rastreador.obtenerEstado()).toMatchObject({ permiso: "concedido", enTurno: false, activa: false });
        await vi.advanceTimersByTimeAsync(5 * 60_000);
        expect(geo.lecturas).toBe(0);

        rastreador.iniciarTurno();
        await vi.advanceTimersByTimeAsync(0);
        expect(turno()).toBe("2026-10-28");
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, activa: true, gps: "ok" });
        expect(almacen.posiciones.size).toBe(1);

        await vi.advanceTimersByTimeAsync(3 * 60_000);
        expect(almacen.posiciones.size).toBe(4);
        const [primero] = [...almacen.posiciones.values()];
        expect(primero).toMatchObject({ lat: 14.6349, lon: -90.5069, precision: 9, velocidad: 1.5 });
        expect(primero).not.toHaveProperty("rumbo");

        rastreador.terminarTurno();
        await vi.advanceTimersByTimeAsync(3 * 60_000);
        expect(almacen.posiciones.size).toBe(4);
        expect(turno()).toBeUndefined();
    });

    it("con el permiso por preguntar no lee hasta que la pantalla lo pide (tras la explicación)", async () => {
        const { geo, almacen, rastreador } = await montar({ permiso: "prompt" });
        rastreador.iniciarTurno();
        await vi.advanceTimersByTimeAsync(2 * 60_000);
        expect(geo.lecturas).toBe(0);
        expect(rastreador.obtenerEstado()).toMatchObject({ permiso: "preguntar", activa: false });

        await rastreador.pedirPermiso();
        await vi.advanceTimersByTimeAsync(0);
        expect(rastreador.obtenerEstado()).toMatchObject({ permiso: "concedido", activa: true });
        expect(almacen.posiciones.size).toBe(1);
    });

    it("si el técnico niega el permiso, queda «Sin permiso» y no vuelve a leer", async () => {
        const { geo, almacen, rastreador } = await montar({ permiso: null });
        expect(rastreador.obtenerEstado().permiso).toBe("desconocido");
        rastreador.iniciarTurno();
        geo.modo = "negado";
        expect(await rastreador.pedirPermiso()).toBe("denegado");
        await vi.advanceTimersByTimeAsync(5 * 60_000);
        expect(geo.lecturas).toBe(1);
        expect(almacen.posiciones.size).toBe(0);
        expect(rastreador.obtenerEstado()).toMatchObject({ permiso: "denegado", enTurno: true, activa: false });
    });

    it("sin señal de GPS lo dice y sigue intentando cada minuto", async () => {
        const { geo, almacen, rastreador } = await montar();
        geo.modo = "sin_senal";
        rastreador.iniciarTurno();
        await vi.advanceTimersByTimeAsync(0);
        expect(rastreador.obtenerEstado()).toMatchObject({ permiso: "concedido", gps: "sin_senal", activa: true });
        geo.modo = "ok";
        await vi.advanceTimersByTimeAsync(60_000);
        expect(rastreador.obtenerEstado().gps).toBe("ok");
        expect(almacen.posiciones.size).toBe(1);
    });

    it("el turno es del día: el de ayer no cuenta y al cambiar el día termina solo", async () => {
        let dia = "2026-10-28";
        const ayer = await montar({ turno: "2026-10-27" });
        expect(ayer.rastreador.obtenerEstado().enTurno).toBe(false);
        expect(ayer.turno()).toBeUndefined();
        ayer.desmontar();

        const hoy = await montar({ turno: "2026-10-28", hoy: () => dia });
        expect(hoy.rastreador.obtenerEstado()).toMatchObject({ enTurno: true, activa: true });
        dia = "2026-10-29";
        await vi.advanceTimersByTimeAsync(60_000);
        expect(hoy.rastreador.obtenerEstado()).toMatchObject({ enTurno: false, activa: false });
        expect(hoy.turno()).toBeUndefined();
    });

    it("sin geolocalización en el navegador: «no disponible» y nada más", async () => {
        const { rastreador } = await montar({ sinGeo: true });
        rastreador.iniciarTurno();
        expect(await rastreador.pedirPermiso()).toBe("no_disponible");
        expect(rastreador.obtenerEstado()).toMatchObject({ permiso: "no_disponible", activa: false });
    });

    it("el turno termina solo al quedarse sin órdenes abiertas (no si nunca hubo ninguna)", async () => {
        const { geo, rastreador, turno } = await montar();
        // Turno iniciado a mano sin órdenes: sigue.
        rastreador.iniciarTurno();
        expect(rastreador.alCambiarOrdenesAbiertas(0)).toBe(false);
        expect(rastreador.alCambiarOrdenesAbiertas(2)).toBe(false);
        expect(rastreador.alCambiarOrdenesAbiertas(1)).toBe(false);
        expect(rastreador.obtenerEstado().enTurno).toBe(true);
        // Cierra (o falla, cancelan, reasignan) la última: termina y deja de leer.
        expect(rastreador.alCambiarOrdenesAbiertas(0)).toBe(true);
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, activa: false });
        expect(turno()).toBeUndefined();
        const lecturas = geo.lecturas;
        await vi.advanceTimersByTimeAsync(5 * 60_000);
        expect(geo.lecturas).toBe(lecturas);
        // Fuera de turno, otra vez sin órdenes no hace nada.
        expect(rastreador.alCambiarOrdenesAbiertas(1)).toBe(false);
        expect(rastreador.alCambiarOrdenesAbiertas(0)).toBe(false);
    });

    it("recuerda por usuario que aceptó la explicación", async () => {
        const { rastreador } = await montar();
        expect(rastreador.explicacionAceptada()).toBe(false);
        rastreador.marcarExplicacionAceptada();
        expect(rastreador.explicacionAceptada()).toBe(true);
    });

    it("al terminar el turno (cerrar sesión) una lectura en curso ya no se guarda", async () => {
        const { geo, almacen, cola, rastreador } = await montar();
        rastreador.iniciarTurno();
        await vi.advanceTimersByTimeAsync(0);
        expect(almacen.posiciones.size).toBe(1);
        // La respuesta del GPS llega después de «Cerrar sesión».
        let responder: (() => void) | null = null;
        geo.getCurrentPosition = (exito: PositionCallback) => {
            responder = () => {
                const coords = { latitude: 1, longitude: 1, accuracy: 5, altitude: null, altitudeAccuracy: null, heading: null, speed: null };
                exito({ coords: { ...coords, toJSON: () => coords }, timestamp: Date.now(), toJSON: () => ({}) } as GeolocationPosition);
            };
        };
        await vi.advanceTimersByTimeAsync(60_000);
        rastreador.terminarTurno();
        cola.detener();
        (responder as (() => void) | null)?.();
        await vi.advanceTimersByTimeAsync(0);
        expect(almacen.posiciones.size).toBe(1);
        expect(await cola.agregar({ lat: 1, lon: 1, registradaEn: new Date().toISOString() })).toBe(false);
    });

    it("convierte la lectura del navegador en el punto del cable", () => {
        const coords = { latitude: 1, longitude: 2, accuracy: 3, altitude: null, altitudeAccuracy: null, heading: 45, speed: null };
        const p = { coords: { ...coords, toJSON: () => coords }, timestamp: INICIO, toJSON: () => ({}) } as GeolocationPosition;
        expect(puntoDeLectura(p)).toEqual({ lat: 1, lon: 2, precision: 3, rumbo: 45, registradaEn: "2026-10-28T15:00:00.000Z" });
    });
});
