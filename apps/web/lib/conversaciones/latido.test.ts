import { describe, expect, it, vi } from "vitest";
import { crearLatido } from "./latido";

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
});
