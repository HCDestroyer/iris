import { describe, expect, it } from "vitest";
import { ErrorApi } from "../api/errores";
import { errorDeCliente } from "./errores";

describe("errores del cliente de Argos", () => {
    it("traduce los tres casos de la verificación en el servidor", () => {
        const otra = errorDeCliente(new ErrorApi({ estado: 422, codigo: "empresa_no_coincide", titulo: "x" }));
        expect(otra).toMatchObject({ titulo: "El cliente o el servicio no es de esta empresa", enCliente: true, reintentable: false });
        expect(errorDeCliente(new ErrorApi({ estado: 503, codigo: "argos_no_disponible", titulo: "x" }))).toMatchObject({ titulo: "Argos no responde", reintentable: true });
        const cuota = errorDeCliente(new ErrorApi({ estado: 429, codigo: "cuota_argos", titulo: "x", reintentarEn: 45 }));
        expect(cuota?.detalle).toContain("en 45 s");
        expect(errorDeCliente(new ErrorApi({ estado: 409, codigo: "version", titulo: "x" }))).toBeNull();
        expect(errorDeCliente(new Error("x"))).toBeNull();
    });
});
