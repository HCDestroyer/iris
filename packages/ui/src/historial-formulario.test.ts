import { afterEach, describe, expect, it, vi } from "vitest";
import { estadoSinMarcas, liberarProteccion, salirDeEntradaMarcada } from "./componentes/formulario-pagina";

const MARCA = "__irisCambiosSinGuardar";

/** Un `window` mínimo con historial: `back()` puede despachar `popstate` o no hacer nada. */
function ventanaFalsa(estado: Record<string, unknown>, { backResponde }: { backResponde: boolean }) {
    const oyentes = new Set<() => void>();
    const historia = {
        state: estado as Record<string, unknown> | null,
        back: vi.fn(() => {
            if (!backResponde) return;
            historia.state = estadoSinMarcas(historia.state, [MARCA, "__irisUsuarioCreado"]);
            setTimeout(() => oyentes.forEach((o) => o()), 10);
        }),
        replaceState: vi.fn((nuevo: Record<string, unknown>) => {
            historia.state = nuevo;
        }),
    };
    const ventana = {
        history: historia,
        location: { href: "http://iris.test/usuarios/nuevo" },
        addEventListener: (tipo: string, f: () => void) => tipo === "popstate" && oyentes.add(f),
        removeEventListener: (tipo: string, f: () => void) => tipo === "popstate" && oyentes.delete(f),
        setTimeout: (f: () => void, ms: number) => setTimeout(f, ms),
        clearTimeout: (id: ReturnType<typeof setTimeout>) => clearTimeout(id),
    };
    vi.stubGlobal("window", ventana);
    return historia;
}

afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
});

describe("historial de los formularios en pantalla propia", () => {
    it("estadoSinMarcas quita solo las marcas y conserva el resto (p. ej. el estado del router)", () => {
        expect(estadoSinMarcas({ __NA: true, tree: 1, [MARCA]: true })).toEqual({ __NA: true, tree: 1 });
        expect(estadoSinMarcas({ a: 1, b: 2 }, ["b"])).toEqual({ a: 1 });
        expect(estadoSinMarcas(null)).toEqual({});
    });

    it("sin entrada propia no toca el historial", async () => {
        const h = ventanaFalsa({ __NA: true }, { backResponde: true });
        await liberarProteccion();
        expect(h.back).not.toHaveBeenCalled();
        expect(h.replaceState).not.toHaveBeenCalled();
    });

    it("con entrada propia vuelve una vez y resuelve con el popstate", async () => {
        vi.useFakeTimers();
        const h = ventanaFalsa({ __NA: true, [MARCA]: true }, { backResponde: true });
        const hecho = liberarProteccion();
        await vi.advanceTimersByTimeAsync(10);
        await hecho;
        expect(h.back).toHaveBeenCalledTimes(1);
        expect(h.replaceState).not.toHaveBeenCalled();
        expect(h.state).toEqual({ __NA: true });
    });

    it("si el popstate no llega a tiempo y sigue marcada, le quita la marca sin volver a moverse", async () => {
        vi.useFakeTimers();
        const h = ventanaFalsa({ __NA: true, otra: 1, __irisUsuarioCreado: true }, { backResponde: false });
        const hecho = salirDeEntradaMarcada("__irisUsuarioCreado");
        await vi.advanceTimersByTimeAsync(500);
        await hecho;
        expect(h.back).toHaveBeenCalledTimes(1);
        expect(h.replaceState).toHaveBeenCalledWith({ __NA: true, otra: 1 }, "", "http://iris.test/usuarios/nuevo");
        expect(h.state).toEqual({ __NA: true, otra: 1 });
    });
});
