import { describe, expect, it } from "vitest";
import { cuerpoResolverAMano, esquemaResolverAMano, resolubleAMano, textoCorregible } from "./pendientes";

describe("resolver a mano un pendiente de Argos", () => {
    it("solo lo rechazado por negocio", () => {
        expect(resolubleAMano({ estado: "fallido_negocio" })).toBe(true);
        for (const estado of ["pendiente", "en_curso", "hecho", "agotado", "bloqueado", "resuelto_manual"] as const) {
            expect(resolubleAMano({ estado })).toBe(false);
        }
    });

    it("pide el motivo y manda la decisión del cargo solo si se tomó", () => {
        expect(esquemaResolverAMano.safeParse({ motivo: "  ok " }).success).toBe(false);
        const d = esquemaResolverAMano.parse({ motivo: "  Consumo rehecho en Argos con origen de la orden  " });
        expect(cuerpoResolverAMano(d, 3)).toEqual({ version: 3, motivo: "Consumo rehecho en Argos con origen de la orden" });
        expect(cuerpoResolverAMano({ ...d, cargo: "resolver_tambien" }, 3)).toEqual({ version: 3, motivo: d.motivo.trim(), cargo: "resolver_tambien" });
        expect(esquemaResolverAMano.safeParse({ motivo: "Algo hecho", cargo: "otra" }).success).toBe(false);
    });

    it("explica cada error corregible", () => {
        expect(textoCorregible("tecnico_no_encontrado")).toMatch(/vincúlalo/);
        expect(textoCorregible("x")).toMatch(/reintenta/);
    });
});
