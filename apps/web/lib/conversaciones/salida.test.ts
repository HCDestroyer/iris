import { describe, expect, it, vi } from "vitest";
import { registrarSalidaProtegida, salirDeConversacion } from "./salida";

describe("salida protegida de la conversación", () => {
    it("sin nadie registrado va directo", () => {
        const directo = vi.fn();
        salirDeConversacion("/conversaciones?c=42", directo);
        expect(directo).toHaveBeenCalledWith("/conversaciones?c=42");
    });

    it("con el compositor registrado pasa por él (que pregunta) y no navega por su cuenta", () => {
        const protegida = vi.fn();
        const directo = vi.fn();
        const quitar = registrarSalidaProtegida(protegida);
        salirDeConversacion("/conversaciones?c=42", directo);
        expect(protegida).toHaveBeenCalledWith("/conversaciones?c=42");
        expect(directo).not.toHaveBeenCalled();
        quitar();
        salirDeConversacion("/conversaciones?c=43", directo);
        expect(directo).toHaveBeenCalledWith("/conversaciones?c=43");
    });

    it("quitar una salida vieja no borra la del compositor nuevo", () => {
        const vieja = vi.fn();
        const nueva = vi.fn();
        const quitarVieja = registrarSalidaProtegida(vieja);
        const quitarNueva = registrarSalidaProtegida(nueva);
        quitarVieja();
        salirDeConversacion("/x", vi.fn());
        expect(nueva).toHaveBeenCalledWith("/x");
        expect(vieja).not.toHaveBeenCalled();
        quitarNueva();
    });
});
