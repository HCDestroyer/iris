// La acción al pie de las pantallas con firma (isla fija con «Firmar y cerrar orden» o «Confirmar
// recepción»): una a la vez, la misma referencia mientras no cambie y quitarla no borra la de otra.
import { describe, expect, it, vi } from "vitest";
import { RegistroAccionPie, volverDelCierre, type AccionPie } from "./campo";

const cerrar: AccionPie = { texto: "Firmar y cerrar orden", icono: "cerrar", ocupado: false, volver: volverDelCierre(7), prueba: "firmar-y-cerrar" };

describe("RegistroAccionPie", () => {
    it("registra, avisa, presiona y se quita al desmontar", () => {
        const r = new RegistroAccionPie();
        const oyente = vi.fn();
        const quitarOyente = r.suscribir(oyente);
        expect(r.obtener()).toBeNull();
        const presionar = vi.fn();
        const quitar = r.registrar(cerrar, presionar);
        expect(oyente).toHaveBeenCalledTimes(1);
        const actual = r.obtener();
        expect(actual?.accion).toEqual(cerrar);
        // Sin cambios, la misma referencia (useSyncExternalStore no vuelve a dibujar).
        expect(r.obtener()).toBe(actual);
        actual?.presionar();
        expect(presionar).toHaveBeenCalledTimes(1);
        quitar();
        expect(r.obtener()).toBeNull();
        expect(oyente).toHaveBeenCalledTimes(2);
        quitarOyente();
        r.registrar(cerrar, presionar);
        expect(oyente).toHaveBeenCalledTimes(2);
    });

    it("quitar una acción ya reemplazada no borra la nueva", () => {
        const r = new RegistroAccionPie();
        const quitarVieja = r.registrar(cerrar, vi.fn());
        r.registrar({ ...cerrar, ocupado: true, texto: "Guardando…" }, vi.fn());
        quitarVieja();
        expect(r.obtener()?.accion.texto).toBe("Guardando…");
    });

    it("«Anterior» del paso final lleva a las fotos de la misma orden", () => {
        expect(volverDelCierre(7)).toEqual({ texto: "Anterior", destino: { tipo: "orden", id: 7, paso: "fotos" } });
    });
});
