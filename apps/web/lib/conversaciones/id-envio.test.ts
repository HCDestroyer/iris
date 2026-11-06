import { describe, expect, it } from "vitest";
import { ErrorApi, errorSinConexion } from "../api/errores";
import { idTrasError } from "./id-envio";
import { SesionCambiada } from "./sesion";

const nuevo = () => "id-nuevo";

describe("id de envío tras un error (id del cuerpo e Idempotency-Key juntos)", () => {
    it("sin red se repite el mismo id: el servidor no duplica si la primera llegó", () => {
        expect(idTrasError(errorSinConexion(), "id-1", nuevo)).toBe("id-1");
    });

    it("tras un 422 (o cualquier respuesta) va un id nuevo: el servidor guardó esa clave", () => {
        expect(idTrasError(new ErrorApi({ estado: 422, codigo: "baja", titulo: "Baja" }), "id-1", nuevo)).toBe("id-nuevo");
        expect(idTrasError(new ErrorApi({ estado: 409, codigo: "conflicto_version", titulo: "Cambió" }), "id-1", nuevo)).toBe("id-nuevo");
        expect(idTrasError(new ErrorApi({ estado: 503, codigo: "no_disponible", titulo: "Caído" }), "id-1", nuevo)).toBe("id-nuevo");
    });

    it("si la sesión cambió no salió nada: se conserva el id", () => {
        expect(idTrasError(new SesionCambiada("otro_usuario"), "id-1", nuevo)).toBe("id-1");
    });
});
