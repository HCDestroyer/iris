import { describe, expect, it, vi } from "vitest";
import { crearLatido, esperaTrasFallos, ESPERA_MAXIMA_MS, tiemposLatido } from "./latido";

function reloj() {
    let t = 1_000_000;
    return { ahora: () => t, avanzar: (ms: number) => void (t += ms) };
}

/** Deja correr las promesas pendientes (el `finally` del envío). */
const vaciar = () => new Promise((r) => setTimeout(r, 0));

describe("latido de disponibilidad", () => {
    it("manda uno con la primera actividad y ninguno más hasta que pase el intervalo", async () => {
        const r = reloj();
        const enviar = vi.fn(() => Promise.resolve());
        const l = crearLatido({ enviar, ahora: r.ahora, intervaloMs: 60_000 });
        l.actividad();
        await vaciar();
        for (let i = 0; i < 20; i++) {
            r.avanzar(2_000);
            l.actividad();
            l.revisar();
        }
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(1);
        r.avanzar(21_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(2);
    });

    it("sin actividad reciente no manda nada aunque pase el intervalo", async () => {
        const r = reloj();
        const enviar = vi.fn(() => Promise.resolve());
        const l = crearLatido({ enviar, ahora: r.ahora, intervaloMs: 60_000, inactivoTrasMs: 90_000 });
        l.actividad();
        await vaciar();
        r.avanzar(60_000);
        l.revisar();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(2);
        r.avanzar(120_000);
        l.revisar();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(2);
        expect(l.activa()).toBe(false);
    });

    it("con la pestaña oculta se detiene y al volver a verla manda si toca", async () => {
        const r = reloj();
        const enviar = vi.fn(() => Promise.resolve());
        const l = crearLatido({ enviar, ahora: r.ahora, intervaloMs: 60_000 });
        l.actividad();
        await vaciar();
        l.visibilidad(false);
        r.avanzar(61_000);
        l.actividad();
        l.revisar();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(1);
        l.visibilidad(true);
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(2);
    });

    it("no se encima: mientras uno está en curso no sale otro, y un fallo no rompe los siguientes", async () => {
        const r = reloj();
        let terminar: (() => void) | undefined;
        const enviar = vi
            .fn<() => Promise<unknown>>()
            .mockImplementationOnce(() => new Promise<void>((ok) => (terminar = ok)))
            .mockImplementationOnce(() => Promise.reject(new Error("sin red")))
            .mockImplementation(() => Promise.resolve());
        const l = crearLatido({ enviar, ahora: r.ahora, intervaloMs: 60_000 });
        l.actividad();
        r.avanzar(70_000);
        l.actividad();
        expect(enviar).toHaveBeenCalledTimes(1);
        terminar?.();
        await vaciar();
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(2);
        r.avanzar(60_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(3);
    });

    it("detenido no vuelve a mandar", async () => {
        const r = reloj();
        const enviar = vi.fn(() => Promise.resolve());
        const l = crearLatido({ enviar, ahora: r.ahora });
        l.detener();
        l.actividad();
        await vaciar();
        expect(enviar).not.toHaveBeenCalled();
    });

    it("con 1 min de inactividad late cada 20 s y revisa cada 10 s: una persona activa no cae", async () => {
        const t = tiemposLatido(1);
        expect(t).toEqual({ intervaloMs: 20_000, revisionMs: 10_000, inactivoTrasMs: 30_000 });
        expect(tiemposLatido(15)).toEqual({ intervaloMs: 60_000, revisionMs: 15_000, inactivoTrasMs: 90_000 });
        const r = reloj();
        const enviar = vi.fn(() => Promise.resolve());
        const l = crearLatido({ enviar, ahora: r.ahora, intervaloMs: t.intervaloMs, inactivoTrasMs: t.inactivoTrasMs });
        // Un minuto con actividad cada 5 s y la revisión cada 10 s: al menos tres latidos, nunca más de 20 s sin uno.
        const envios: number[] = [];
        enviar.mockImplementation(() => {
            envios.push(r.ahora());
            return Promise.resolve();
        });
        for (let s = 0; s <= 60; s += 5) {
            l.actividad();
            if (s % 10 === 0) l.revisar();
            await vaciar();
            r.avanzar(5_000);
        }
        expect(envios.length).toBeGreaterThanOrEqual(3);
        for (let i = 1; i < envios.length; i++) expect((envios[i] ?? 0) - (envios[i - 1] ?? 0)).toBeLessThanOrEqual(25_000);
    });

    it("tras un 5xx o un 429 espera cada vez más (hasta 5 min); tras un éxito vuelve al intervalo", async () => {
        const r = reloj();
        let falla = true;
        const enviar = vi.fn(() => (falla ? Promise.reject(new Error("503")) : Promise.resolve()));
        const l = crearLatido({ enviar, ahora: r.ahora, intervaloMs: 60_000, inactivoTrasMs: 10 * 60_000, trasFallo: () => "esperar" });
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(1);
        // Primer fallo: espera 2 min, no 1.
        r.avanzar(61_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(1);
        r.avanzar(60_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(2);
        falla = false;
        r.avanzar(4 * 60_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(3);
        r.avanzar(60_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(4);
        expect(esperaTrasFallos(60_000, 10)).toBe(ESPERA_MAXIMA_MS);
        expect(esperaTrasFallos(60_000, 1)).toBe(120_000);
    });

    it("un 401 lo detiene: no vuelve a mandar y la respuesta tardía se reconoce", async () => {
        const r = reloj();
        const enviar = vi.fn(() => Promise.reject(new Error("401")));
        const l = crearLatido({ enviar, ahora: r.ahora, trasFallo: () => "detener" });
        l.actividad();
        await vaciar();
        expect(l.detenido()).toBe(true);
        r.avanzar(120_000);
        l.actividad();
        await vaciar();
        expect(enviar).toHaveBeenCalledTimes(1);
    });
});
