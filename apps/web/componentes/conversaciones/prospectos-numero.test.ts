import { describe, expect, it } from "vitest";
import { TEXTO_PROSPECTO_RESTRINGIDO, textoCoincidenciaProspecto } from "./prospectos-numero";

describe("prospectos del número de la conversación (§3.6, P2)", () => {
    it("el principal y otro de sus números, con el estado del embudo", () => {
        expect(textoCoincidenciaProspecto({ numero: "PR-000012", estado: "contactado", principal: true })).toBe("PR-000012 · Contactado · número principal");
        expect(textoCoincidenciaProspecto({ numero: "PR-000013", estado: "nuevo", principal: false })).toBe("PR-000013 · Nuevo · otro número del prospecto");
    });

    it("la restricción nombra lo que no se comparte y cómo se desbloquea", () => {
        expect(TEXTO_PROSPECTO_RESTRINGIDO).toContain("la cotización, la dirección ni el plan");
        expect(TEXTO_PROSPECTO_RESTRINGIDO).toContain("código enviado al número principal");
    });
});
