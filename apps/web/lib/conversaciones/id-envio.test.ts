import { describe, expect, it } from "vitest";
import { ErrorApi, errorSinConexion } from "../api/errores";
import { idTrasError } from "./id-envio";
import { SesionCambiada } from "./sesion";

const nuevo = () => "id-nuevo";

describe("id de envío tras un error (id del cuerpo e Idempotency-Key juntos)", () => {
    it("sin red se repite el mismo id: el servidor no duplica si la primera llegó", () => {
        expect(idTrasError(errorSinConexion(), "id-1", nuevo)).toBe("id-1");
    });

    it("tras un 4xx va un id nuevo: el servidor guardó esa clave", () => {
        expect(idTrasError(new ErrorApi({ estado: 422, codigo: "baja", titulo: "Baja" }), "id-1", nuevo)).toBe("id-nuevo");
        expect(idTrasError(new ErrorApi({ estado: 409, codigo: "conflicto_version", titulo: "Cambió" }), "id-1", nuevo)).toBe("id-nuevo");
    });

    it("tras un 5xx se repite el id: el servidor no guarda los 5xx y la API pudo haber hecho COMMIT", () => {
        expect(idTrasError(new ErrorApi({ estado: 503, codigo: "no_disponible", titulo: "Caído" }), "id-1", nuevo)).toBe("id-1");
        expect(idTrasError(new ErrorApi({ estado: 502, codigo: "api_no_disponible", titulo: "Sin API" }), "id-1", nuevo)).toBe("id-1");
    });

    it("si la sesión cambió no salió nada: se conserva el id", () => {
        expect(idTrasError(new SesionCambiada("otro_usuario"), "id-1", nuevo)).toBe("id-1");
    });
});
