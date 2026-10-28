import { ColaPosiciones } from "@iris/sync";
import { AlmacenMemoria, TransportePosicionesFalso, RelojManual } from "@iris/sync/pruebas";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TurnoDelDia } from "@iris/sync";
import { hoyEnGuatemala } from "./fechas";
import { leerTurnoLocal } from "./turno";
import { puntoDeLectura, RastreadorUbicacion, type AvisoTurno, type DependenciasUbicacion } from "./ubicacion";

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

interface OpcionesMontaje {
    permiso?: PermissionState | null;
    turno?: string;
    hoy?: () => string;
    sinGeo?: boolean;
    /** Explicación ya aceptada antes (otra visita). */
    explicada?: boolean;
    /** Almacén local compartido (para «reabrir la app»). */
    local?: { turno?: string };
}

async function montar(opciones: OpcionesMontaje = {}) {
    const geo = new GeoFalsa();
    const almacen = new AlmacenMemoria();
    const reloj = new RelojManual(INICIO);
    const cola = await ColaPosiciones.abrir({ almacen, transporte: new TransportePosicionesFalso(reloj) });
    const local = opciones.local ?? {};
    if (opciones.turno !== undefined) {
        local.turno = opciones.turno;
    }
    let explicacion: string | undefined = opciones.explicada ? "aceptada" : undefined;
    const deps: DependenciasUbicacion = {
        geolocalizacion: opciones.sinGeo ? null : (geo as Geolocation),
        permisos: opciones.permiso === null ? null : permisosFalsos(opciones.permiso ?? "granted"),
        hoy: opciones.hoy ?? (() => "2026-10-28"),
        leerTurno: () => local.turno,
        guardarTurno: (valor) => {
            local.turno = valor ?? undefined;
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
    const avisos: AvisoTurno[] = [];
    rastreador.alAvisar((a) => avisos.push(a));
    const desmontar = rastreador.montar();
    await vi.advanceTimersByTimeAsync(0);
    return { geo, almacen, cola, rastreador, desmontar, avisos, local, turno: () => leerTurnoLocal(local.turno) };
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
        expect(turno()).toMatchObject({ fecha: "2026-10-28", enTurno: true });
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, activa: true, gps: "ok", tramos: null, hasta: null, empieza: null });
        expect(almacen.posiciones.size).toBe(1);

        await vi.advanceTimersByTimeAsync(3 * 60_000);
        expect(almacen.posiciones.size).toBe(4);
        const [primero] = [...almacen.posiciones.values()];
        expect(primero).toMatchObject({ lat: 14.6349, lon: -90.5069, precision: 9, velocidad: 1.5 });
        expect(primero).not.toHaveProperty("rumbo");

        rastreador.terminarTurno();
        await vi.advanceTimersByTimeAsync(3 * 60_000);
        expect(almacen.posiciones.size).toBe(4);
        expect(turno()).toBeNull();
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
        expect(ayer.turno()).toBeNull();
        ayer.desmontar();

        const hoy = await montar({ turno: "2026-10-28", hoy: () => dia });
        expect(hoy.rastreador.obtenerEstado()).toMatchObject({ enTurno: true, activa: true });
        dia = "2026-10-29";
        await vi.advanceTimersByTimeAsync(60_000);
        expect(hoy.rastreador.obtenerEstado()).toMatchObject({ enTurno: false, activa: false });
        expect(hoy.turno()).toBeNull();
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
        expect(turno()).toBeNull();
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

// Agenda de prueba (Guatemala = UTC−6): 08:00–12:00 y 13:00–17:00 del 28 de octubre.
const GT = (hora: string, dia = "2026-10-28") => Date.parse(`${dia}T${hora}:00-06:00`);
const iso = (ms: number) => new Date(ms).toISOString();
function agenda(tramos: [string, string][], fecha = "2026-10-28"): TurnoDelDia {
    const t = tramos.map(([a, b]) => ({ inicio: iso(GT(a, fecha)), fin: iso(b === "24:00" ? GT("00:00", siguienteDia(fecha)) : GT(b, fecha)) }));
    return { fecha, inicio: t[0]?.inicio ?? "", fin: t.at(-1)?.fin ?? "", tramos: t };
}
function siguienteDia(fecha: string): string {
    return new Date(Date.parse(`${fecha}T12:00:00Z`) + 86_400_000).toISOString().slice(0, 10);
}
const DIA_PARTIDO = agenda([
    ["08:00", "12:00"],
    ["13:00", "17:00"],
]);

/** Avanza el reloj falso hasta esa hora de Guatemala. */
async function hasta(ms: number) {
    await vi.advanceTimersByTimeAsync(ms - Date.now());
}

describe("turno según la agenda (Paquete.turno)", () => {
    it("se enciende solo al empezar cada tramo (con aviso) y se apaga al terminarlo", async () => {
        vi.setSystemTime(GT("07:30"));
        const { geo, almacen, rastreador, avisos } = await montar({ hoy: () => hoyEnGuatemala(), explicada: true });
        rastreador.establecerAgenda(DIA_PARTIDO);
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, empieza: GT("08:00"), hasta: null });
        expect(rastreador.obtenerEstado().tramos).toHaveLength(2);

        await hasta(GT("08:00"));
        expect(avisos).toEqual(["iniciado_por_agenda"]);
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, activa: true, hasta: GT("12:00"), empieza: null });
        expect(almacen.posiciones.size).toBe(1);

        await hasta(GT("12:00"));
        expect(avisos).toEqual(["iniciado_por_agenda", "terminado_por_agenda"]);
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, activa: false, empieza: GT("13:00") });
        const enAlmuerzo = geo.lecturas;
        await hasta(GT("12:59"));
        expect(geo.lecturas).toBe(enAlmuerzo);

        await hasta(GT("13:00"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, hasta: GT("17:00") });
        await hasta(GT("17:00"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, empieza: null });
        expect(avisos).toEqual(["iniciado_por_agenda", "terminado_por_agenda", "iniciado_por_agenda", "terminado_por_agenda"]);
    });

    it("al abrir la app dentro de un tramo se enciende; sin explicación, sin permiso o con «Ahora no», no", async () => {
        vi.setSystemTime(GT("09:00"));
        const sinExplicar = await montar({ hoy: () => hoyEnGuatemala() });
        sinExplicar.rastreador.establecerAgenda(DIA_PARTIDO);
        expect(sinExplicar.rastreador.obtenerEstado().enTurno).toBe(false);
        // Acepta la explicación (desde el panel o una acción): se enciende al momento.
        sinExplicar.rastreador.marcarExplicacionAceptada();
        await sinExplicar.rastreador.pedirPermiso();
        expect(sinExplicar.rastreador.obtenerEstado()).toMatchObject({ enTurno: true, activa: true });
        expect(sinExplicar.avisos).toEqual(["iniciado_por_agenda"]);
        sinExplicar.desmontar();

        const sinPermiso = await montar({ hoy: () => hoyEnGuatemala(), explicada: true, permiso: "prompt" });
        sinPermiso.rastreador.establecerAgenda(DIA_PARTIDO);
        await vi.advanceTimersByTimeAsync(60_000);
        expect(sinPermiso.rastreador.obtenerEstado().enTurno).toBe(false);
        sinPermiso.desmontar();

        const declinado = await montar({ hoy: () => hoyEnGuatemala(), explicada: true });
        declinado.rastreador.declinarEnVisita();
        declinado.rastreador.establecerAgenda(DIA_PARTIDO);
        await hasta(GT("13:00"));
        expect(declinado.rastreador.obtenerEstado().enTurno).toBe(false);
        expect(declinado.avisos).toEqual([]);
    });

    it("«Terminar turno» a mano dentro de un tramo se respeta hasta el siguiente, también al reabrir la app", async () => {
        vi.setSystemTime(GT("09:00"));
        const local: { turno?: string } = {};
        const primera = await montar({ hoy: () => hoyEnGuatemala(), explicada: true, local });
        primera.rastreador.establecerAgenda(DIA_PARTIDO);
        expect(primera.rastreador.obtenerEstado().enTurno).toBe(true);

        primera.rastreador.terminarTurno();
        expect(primera.rastreador.obtenerEstado()).toMatchObject({ enTurno: false, empieza: GT("13:00") });
        await hasta(GT("10:00"));
        expect(primera.rastreador.obtenerEstado().enTurno).toBe(false);
        primera.desmontar();

        // Reabre la app en el mismo tramo: sigue apagado.
        const segunda = await montar({ hoy: () => hoyEnGuatemala(), explicada: true, local });
        segunda.rastreador.establecerAgenda(DIA_PARTIDO);
        await hasta(GT("11:59"));
        expect(segunda.rastreador.obtenerEstado().enTurno).toBe(false);
        expect(segunda.avisos).toEqual([]);

        // En el tramo siguiente vuelve a encenderse solo.
        await hasta(GT("13:00"));
        expect(segunda.rastreador.obtenerEstado()).toMatchObject({ enTurno: true, hasta: GT("17:00") });
        expect(segunda.avisos).toEqual(["iniciado_por_agenda"]);

        // «Iniciar turno» a mano dentro del tramo sí lo enciende aunque lo haya apagado.
        segunda.rastreador.terminarTurno();
        segunda.rastreador.iniciarTurno();
        expect(segunda.rastreador.obtenerEstado().enTurno).toBe(true);
    });

    it("fuera de tramo «En camino» lo enciende y termina con la última orden o el fin del tramo, lo que ocurra después", async () => {
        vi.setSystemTime(GT("07:00"));
        const { rastreador, avisos } = await montar({ hoy: () => hoyEnGuatemala(), explicada: true });
        rastreador.establecerAgenda(DIA_PARTIDO);
        rastreador.alCambiarOrdenesAbiertas(2);
        expect(rastreador.alTrabajar()).toBe(true);
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, hasta: null });

        // Empieza el tramo: queda atado a él. Cierra todas a las 10:00: sigue hasta las 12:00.
        await hasta(GT("08:00"));
        expect(rastreador.obtenerEstado().hasta).toBe(GT("12:00"));
        await hasta(GT("10:00"));
        expect(rastreador.alCambiarOrdenesAbiertas(0)).toBe(false);
        expect(rastreador.obtenerEstado().enTurno).toBe(true);
        await hasta(GT("12:00"));
        expect(rastreador.obtenerEstado().enTurno).toBe(false);
        expect(avisos).toEqual(["terminado_por_agenda"]);

        // En el tramo de la tarde: «En camino» a las 16:50 y la orden sigue abierta a las 17:00.
        rastreador.alCambiarOrdenesAbiertas(1);
        await hasta(GT("16:50"));
        expect(rastreador.alTrabajar()).toBe(false); // ya estaba en turno (encendido a las 13:00)
        await hasta(GT("17:00"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, hasta: null });
        await hasta(GT("17:40"));
        expect(rastreador.alCambiarOrdenesAbiertas(0)).toBe(true);
        expect(rastreador.obtenerEstado().enTurno).toBe(false);
        expect(avisos).toEqual(["terminado_por_agenda", "iniciado_por_agenda", "terminado_por_ordenes"]);
    });

    it("día sin turno en la agenda (o el turno es de otro día): el turno de siempre", async () => {
        vi.setSystemTime(GT("09:00"));
        const { rastreador, avisos } = await montar({ hoy: () => hoyEnGuatemala(), explicada: true });
        rastreador.establecerAgenda(null);
        await hasta(GT("10:00"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, tramos: null, empieza: null });

        // Turno de ayer guardado en el motor (paquete viejo): no cuenta.
        rastreador.establecerAgenda(agenda([["08:00", "17:00"]], "2026-10-27"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, tramos: null });

        rastreador.alCambiarOrdenesAbiertas(1);
        expect(rastreador.alTrabajar()).toBe(true);
        await hasta(GT("20:00"));
        expect(rastreador.obtenerEstado().enTurno).toBe(true);
        expect(rastreador.alCambiarOrdenesAbiertas(0)).toBe(true);
        expect(avisos).toEqual(["terminado_por_ordenes"]);
    });

    it("el día cambia a medianoche de Guatemala, no de UTC", async () => {
        // 23:30 del 28 en Guatemala = 05:30 del 29 en UTC.
        vi.setSystemTime(GT("23:30"));
        const nocturno = agenda([["22:00", "24:00"]]);
        const { rastreador, turno } = await montar({ hoy: () => hoyEnGuatemala(), explicada: true });
        rastreador.establecerAgenda(nocturno);
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, hasta: GT("00:00", "2026-10-29") });
        expect(turno()).toMatchObject({ fecha: "2026-10-28" });

        await hasta(GT("00:00", "2026-10-29"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, tramos: null });
        expect(turno()).toBeNull();

        // Llega el paquete del 29: el turno de ese día empieza a las 08:00.
        rastreador.establecerAgenda(agenda([["08:00", "17:00"]], "2026-10-29"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: false, empieza: GT("08:00", "2026-10-29") });
        await hasta(GT("08:00", "2026-10-29"));
        expect(rastreador.obtenerEstado()).toMatchObject({ enTurno: true, hasta: GT("17:00", "2026-10-29") });
    });
});
