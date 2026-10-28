import { describe, expect, it } from "vitest";
import { baseSegura, enlaceArgosCliente, enlaceHorusTicket, type EnlacesExternos } from "./enlaces";

const AMBOS: EnlacesExternos = {
    argosisp: { conectorId: 1, urlPublica: "https://argos.isp.gt/" },
    horus: { conectorId: 2, urlPublica: "https://horus.isp.gt/flow" },
};

describe("enlaces «Ver en Argos/Horus»", () => {
    it("solo con la base que da la API, http(s) y sin barra final", () => {
        expect(baseSegura("https://argos.isp.gt/")).toBe("https://argos.isp.gt");
        expect(baseSegura("https://horus.isp.gt/flow//")).toBe("https://horus.isp.gt/flow");
        expect(baseSegura("http://10.0.0.5:8080")).toBe("http://10.0.0.5:8080");
        expect(baseSegura("javascript:alert(1)")).toBeNull();
        expect(baseSegura("ftp://argos.isp.gt")).toBeNull();
        expect(baseSegura("https://usuario:clave@argos.isp.gt")).toBeNull();
        expect(baseSegura("no es url")).toBeNull();
        expect(baseSegura(undefined)).toBeNull();
    });

    it("Argos: con cliente y conector activo", () => {
        expect(enlaceArgosCliente(AMBOS, 501)).toMatchObject({ sistema: "argosisp", etiqueta: "Ver en Argos", href: "https://argos.isp.gt" });
        expect(enlaceArgosCliente(AMBOS, null)).toBeNull();
        expect(enlaceArgosCliente({ horus: AMBOS.horus }, 501)).toBeNull();
        expect(enlaceArgosCliente(null, 501)).toBeNull();
    });

    it("Horus: solo en tickets de origen horus y con conector activo", () => {
        expect(enlaceHorusTicket(AMBOS, "horus")).toMatchObject({ sistema: "horus", etiqueta: "Ver en Horus", href: "https://horus.isp.gt/flow" });
        expect(enlaceHorusTicket(AMBOS, "correo")).toBeNull();
        expect(enlaceHorusTicket({ argosisp: AMBOS.argosisp }, "horus")).toBeNull();
        expect(enlaceHorusTicket({ horus: { conectorId: 2, urlPublica: "javascript:alert(1)" } }, "horus")).toBeNull();
    });
});
