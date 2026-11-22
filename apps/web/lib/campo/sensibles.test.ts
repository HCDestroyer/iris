import { describe, expect, it, vi } from "vitest";
import { olvidarAlCerrarSesion, sesionPerdidaPor401, vigilarSesionPerdida, type MotorVigilado } from "./sensibles";
import { sensiblesAOlvidar } from "./sesion";

type Estado = ReturnType<MotorVigilado["obtenerEstado"]>;

function motorFalso(inicial: Estado) {
    let estado = inicial;
    const oyentes = new Set<() => void>();
    const motor: MotorVigilado = {
        suscribir: (o) => {
            oyentes.add(o);
            return () => oyentes.delete(o);
        },
        obtenerEstado: () => estado,
    };
    return {
        motor,
        cambiar(nuevo: Estado) {
            estado = nuevo;
            for (const o of oyentes) {
                o();
            }
        },
    };
}

const enLinea: Estado = { conexion: "en_linea", ultimoError: null };
const sin401: Estado = { conexion: "sin_sesion", ultimoError: { mensaje: "no autenticado", status: 401, problema: null } };
const sin403: Estado = { conexion: "sin_sesion", ultimoError: { mensaje: "requiere sesión de campo", status: 403, problema: null } };

describe("datos del titular con la app abierta (§3.4.8)", () => {
    it("un 401 olvida en el momento lo de esta base y lo de todas; un 403 no", async () => {
        const { motor, cambiar } = motorFalso(enLinea);
        const propias = vi.fn(() => Promise.resolve());
        const todas = vi.fn(() => Promise.resolve(2));
        const dejar = vigilarSesionPerdida(motor, { propias, todas });
        cambiar(sin403);
        expect(propias).not.toHaveBeenCalled();
        expect(todas).not.toHaveBeenCalled();
        cambiar(sin401);
        expect(propias).toHaveBeenCalledTimes(1);
        expect(todas).toHaveBeenCalledTimes(1);
        // Sigue sin sesión: no se repite; tras volver y perderla otra vez, sí.
        cambiar(sin401);
        expect(propias).toHaveBeenCalledTimes(1);
        cambiar(enLinea);
        cambiar(sin401);
        expect(propias).toHaveBeenCalledTimes(2);
        dejar();
        cambiar(enLinea);
        cambiar(sin401);
        expect(propias).toHaveBeenCalledTimes(2);
    });

    it("si ya estaba sin sesión por un 401 al empezar a vigilar, olvida enseguida", () => {
        const { motor } = motorFalso(sin401);
        const propias = vi.fn(() => Promise.resolve());
        const todas = vi.fn(() => Promise.resolve(0));
        vigilarSesionPerdida(motor, { propias, todas });
        expect(propias).toHaveBeenCalledTimes(1);
        expect(sesionPerdidaPor401(sin403)).toBe(false);
    });

    it("cerrar sesión (también sin señal) olvida lo de todas las bases, aunque falle", async () => {
        const todas = vi.fn(() => Promise.resolve(3));
        await olvidarAlCerrarSesion({ todas });
        expect(todas).toHaveBeenCalledTimes(1);
        await expect(olvidarAlCerrarSesion({ todas: () => Promise.reject(new Error("idb")) })).resolves.toBeUndefined();
        // Y al volver a la app tras cerrar sin señal, también.
        expect(sensiblesAOlvidar("conectate", undefined, "sesion_cerrada")).toEqual({});
        expect(sensiblesAOlvidar("conectate", undefined, "sin_verificar")).toBeNull();
    });
});
