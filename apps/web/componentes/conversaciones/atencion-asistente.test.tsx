import { describe, expect, it } from "vitest";
import { ErrorApi } from "@/lib/api/errores";
import { explicarErrorDePausa } from "./atencion-asistente";

const error = (estado: number, codigo: string) => new ErrorApi({ estado, codigo, titulo: "Título del servidor" });

describe("errores de pausar y reanudar el asistente", () => {
    it("solo `conflicto_version` es «la cambió otra persona» (y se relee)", () => {
        expect(explicarErrorDePausa(error(409, "conflicto_version"), "pausar")).toEqual({ texto: "Alguien la cambió; inténtalo de nuevo", releer: true });
    });

    it("otro 409 (la misma acción aún en curso) no dice que la cambió otra persona", () => {
        const e = explicarErrorDePausa(error(409, "idempotencia_en_proceso"), "reanudar");
        expect(e.texto).toBe("No se pudo reanudar: Título del servidor");
        expect(e.releer).toBe(false);
    });

    it("resuelta, sin permiso y ya no disponible, en llano", () => {
        expect(explicarErrorDePausa(error(422, "conversacion_resuelta"), "reanudar")).toEqual({ texto: "La conversación ya está resuelta: el asistente no atiende conversaciones resueltas.", releer: true });
        expect(explicarErrorDePausa(error(403, "permiso_denegado"), "pausar")).toEqual({ texto: "No tienes permiso para pausar el asistente en esta conversación.", releer: false });
        expect(explicarErrorDePausa(error(403, "permiso_denegado"), "reanudar").texto).toBe("No tienes permiso para reanudar el asistente en esta conversación.");
        expect(explicarErrorDePausa(error(404, "no_encontrado"), "pausar")).toEqual({ texto: "Esta conversación ya no está disponible.", releer: true });
    });

    it("lo demás, con el título del error", () => {
        expect(explicarErrorDePausa(error(500, "error_interno"), "pausar")).toEqual({ texto: "No se pudo pausar: Título del servidor", releer: false });
    });
});
