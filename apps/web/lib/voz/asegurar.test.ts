import { beforeEach, describe, expect, it, vi } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import type { Llamada } from "./tipos";

const leerLlamada = vi.fn<(id: string, signal?: AbortSignal, empresa?: number) => Promise<Llamada>>();
const colgarLlamada = vi.fn<(id: string, clave: string, empresa?: number, propia?: boolean) => Promise<Llamada>>();
vi.mock("./api", () => ({ leerLlamada, colgarLlamada, soltarLlamada: vi.fn() }));

const { colgarSeguro, LECTURAS_CONECTANDO } = await import("./asegurar");

const YO = 1;
const llamada = (cambio: Partial<Llamada>): Llamada => ({
    id: "0199aaaa-0000-7000-8000-000000000001",
    origen: "whatsapp",
    direccion: "entrante",
    numero: "+50255120041",
    titulo: "Carlos Ruiz",
    contacto: { tipo: "desconocido" },
    estado: "conectando",
    usuario: { id: YO, nombre: "Ana López" },
    tramos: [],
    sonoEn: "2026-11-17T16:00:00Z",
    puede: { contestar: false, rechazar: false, colgar: true, tomar: false, transferir: false, anotar: false, escucharGrabacion: false },
    ...cambio,
});
const problema = (estado: number, codigo: string) => new ErrorApi({ estado, codigo, titulo: codigo });
const ctx = { empresa: 1, yoId: YO, esperar: () => Promise.resolve() };

describe("colgarSeguro (solo si es mía): nunca una llamada viva sin audio", () => {
    beforeEach(() => {
        leerLlamada.mockReset();
        colgarLlamada.mockReset();
    });

    it("`conectando` a mi nombre: `colgar?propia=true` responde 200 con la llamada `perdida` (o `terminada`): es el final", async () => {
        for (const estado of ["perdida", "terminada"] as const) {
            leerLlamada.mockReset();
            colgarLlamada.mockReset();
            leerLlamada.mockResolvedValue(llamada({}));
            colgarLlamada.mockResolvedValue(llamada({ estado, motivoFin: "agente_salio" }));
            const fin = await colgarSeguro("x", ctx, true);
            expect(fin).toMatchObject({ tipo: "colgada", llamada: { estado } });
            expect(leerLlamada).toHaveBeenCalledTimes(1);
            expect(colgarLlamada).toHaveBeenCalledTimes(1);
            expect(colgarLlamada.mock.calls[0]?.[3]).toBe(true);
        }
    });

    it("contra un servidor anterior (404 a `propia` mientras conecta): ese 404 no es el final; se relee y, cuando el `accept` llega tarde, se cuelga", async () => {
        leerLlamada.mockResolvedValueOnce(llamada({})).mockResolvedValueOnce(llamada({})).mockResolvedValue(llamada({ estado: "en_curso" }));
        colgarLlamada.mockRejectedValueOnce(problema(404, "no_encontrado")).mockRejectedValueOnce(problema(404, "no_encontrado")).mockResolvedValue(llamada({ estado: "terminada" }));
        const fin = await colgarSeguro("x", ctx, true);
        expect(fin.tipo).toBe("colgada");
        expect(leerLlamada).toHaveBeenCalledTimes(3);
        expect(colgarLlamada).toHaveBeenCalledTimes(3);
        expect(colgarLlamada.mock.calls.every((c) => c[3] === true)).toBe(true);
    });

    it("un 404 de `colgar` no es el final si la lectura dice que sigue viva a mi nombre: se reintenta", async () => {
        leerLlamada.mockResolvedValue(llamada({ estado: "en_curso" }));
        colgarLlamada.mockRejectedValueOnce(problema(404, "no_encontrado")).mockResolvedValue(llamada({ estado: "terminada" }));
        expect((await colgarSeguro("x", ctx, true)).tipo).toBe("colgada");
        expect(colgarLlamada).toHaveBeenCalledTimes(2);
    });

    it("la reserva venció (vuelve a sonar), terminó o la tiene otra persona: nada mío que colgar", async () => {
        for (const l of [llamada({ estado: "sonando" }), llamada({ estado: "terminada" }), llamada({ estado: "en_curso", usuario: { id: 9, nombre: "Rosa" } })]) {
            leerLlamada.mockResolvedValueOnce(l);
            expect((await colgarSeguro("x", ctx, true)).tipo).toBe("nada");
        }
        leerLlamada.mockRejectedValueOnce(problema(404, "no_encontrado"));
        expect((await colgarSeguro("x", ctx, true)).tipo).toBe("nada");
        expect(colgarLlamada).not.toHaveBeenCalled();
    });

    it("si no sale nunca de `conectando`, o colgar falla siempre, no se da por hecho: fallo (se avisa y se ofrece reintentar)", async () => {
        leerLlamada.mockResolvedValue(llamada({}));
        colgarLlamada.mockRejectedValue(problema(404, "no_encontrado"));
        expect((await colgarSeguro("x", ctx, true)).tipo).toBe("fallo");
        expect(leerLlamada).toHaveBeenCalledTimes(LECTURAS_CONECTANDO);
        leerLlamada.mockResolvedValue(llamada({ estado: "en_curso" }));
        colgarLlamada.mockRejectedValue(problema(500, "error_interno"));
        expect((await colgarSeguro("x", ctx, true)).tipo).toBe("fallo");
    });

    it("hablando (sin comprobación previa) cuelga sin `propia`, y `llamada_terminada` o 404 es «ya no está»", async () => {
        colgarLlamada.mockRejectedValueOnce(problema(409, "llamada_terminada"));
        expect((await colgarSeguro("x", ctx, false)).tipo).toBe("nada");
        expect(colgarLlamada.mock.calls[0]?.[3]).toBeUndefined();
        expect(leerLlamada).not.toHaveBeenCalled();
    });
});
