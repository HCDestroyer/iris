import { describe, expect, it, vi } from "vitest";
import { hayProteccion, registrarSalidaProtegida, salirDeConversacion } from "./salida";

describe("salida protegida de la conversación", () => {
    it("sin nadie registrado va directo y no hay nada que proteger", () => {
        const directo = vi.fn();
        expect(hayProteccion()).toBe(false);
        salirDeConversacion("/conversaciones?c=42", directo);
        expect(directo).toHaveBeenCalledWith("/conversaciones?c=42");
    });

    it("con el compositor registrado pasa por él (que pregunta) y no navega por su cuenta", () => {
        const protegida = vi.fn();
        const directo = vi.fn();
        const quitar = registrarSalidaProtegida(protegida, () => true);
        salirDeConversacion("/conversaciones?c=42", directo);
        expect(protegida).toHaveBeenCalledWith("/conversaciones?c=42");
        expect(directo).not.toHaveBeenCalled();
        quitar();
        salirDeConversacion("/conversaciones?c=43", directo);
        expect(directo).toHaveBeenCalledWith("/conversaciones?c=43");
    });

    it("`hayProteccion` sigue al compositor en vivo: solo mientras tiene archivos por enviar o subiendo", () => {
        let archivos = 0;
        const quitar = registrarSalidaProtegida(vi.fn(), () => archivos > 0);
        expect(hayProteccion()).toBe(false);
        archivos = 2;
        expect(hayProteccion()).toBe(true);
        archivos = 0;
        expect(hayProteccion()).toBe(false);
        archivos = 1;
        quitar();
        expect(hayProteccion()).toBe(false);
    });

    it("quitar una salida vieja no borra la del compositor nuevo", () => {
        const vieja = vi.fn();
        const nueva = vi.fn();
        const quitarVieja = registrarSalidaProtegida(vieja, () => false);
        const quitarNueva = registrarSalidaProtegida(nueva, () => true);
        quitarVieja();
        expect(hayProteccion()).toBe(true);
        salirDeConversacion("/x", vi.fn());
        expect(nueva).toHaveBeenCalledWith("/x");
        expect(vieja).not.toHaveBeenCalled();
        quitarNueva();
    });
});
