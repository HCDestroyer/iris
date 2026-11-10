import { describe, expect, it } from "vitest";
import { conectorArgos, cuerpoCambioConector, esquemaPanelArgos, leerVuelta, mensajeVuelta, origenPanel, panelDe, panelDesconocido, panelesDeArgos, presentacionVersion, RUTA_DIAGNOSTICO_ARGOS, textoAlcance, type Conector } from "./presentacion";

describe("origenPanel", () => {
    it("normaliza al origen https://host[:puerto]", () => {
        expect(origenPanel(" https://Argos.ISP.gt/inicio ")).toBe("https://argos.isp.gt");
        expect(origenPanel("https://argos.isp.gt:8443")).toBe("https://argos.isp.gt:8443");
        expect(origenPanel("http://localhost:3000")).toBe("http://localhost:3000");
    });

    it("rechaza lo que no es un panel http(s) limpio", () => {
        for (const v of ["", "argos.isp.gt", "javascript:alert(1)", "ftp://argos.isp.gt", "https://u:p@argos.isp.gt", "https://argos.isp.gt/?x=1", "https://argos.isp.gt/#a"]) {
            expect(origenPanel(v), v).toBeNull();
        }
    });
});

describe("esquemaPanelArgos", () => {
    it("exige una dirección válida (no se puede pulsar «Conectar» sin ella)", () => {
        expect(esquemaPanelArgos.safeParse({ panelUrl: "https://argos.isp.gt" }).success).toBe(true);
        expect(esquemaPanelArgos.safeParse({ panelUrl: "   " }).success).toBe(false);
        expect(esquemaPanelArgos.safeParse({ panelUrl: "argos.isp.gt" }).success).toBe(false);
        expect(esquemaPanelArgos.safeParse({ panelUrl: `https://${"a".repeat(500)}.gt` }).success).toBe(false);
    });
});

describe("leerVuelta", () => {
    const state = "s".repeat(43);

    it("lee codigo, api y state del fragmento", () => {
        expect(leerVuelta(`#codigo=abc123&api=${encodeURIComponent("https://api.argos.gt")}&state=${state}`)).toEqual({ codigo: "abc123", api: "https://api.argos.gt", state });
    });

    it("lee un rechazo", () => {
        expect(leerVuelta(`#error=rechazado&state=${state}`)).toEqual({ error: "rechazado", state });
    });

    it("sin state, o sin código ni error, no hay nada que completar", () => {
        expect(leerVuelta("")).toBeNull();
        expect(leerVuelta("#codigo=abc&api=https://a.gt")).toBeNull();
        expect(leerVuelta(`#state=${state}`)).toBeNull();
        expect(leerVuelta(`#codigo=abc&state=${state}`)).toBeNull();
        expect(leerVuelta(`#codigo=abc&api=https://a.gt&state=${"s".repeat(201)}`)).toBeNull();
    });
});

describe("mensajeVuelta", () => {
    it("explica cada error de «completar»", () => {
        expect(mensajeVuelta("rechazado", 422).titulo).toBe("Rechazaste la conexión en Argos");
        expect(mensajeVuelta("state_vencido", 422).texto).toContain("10 minutos");
        expect(mensajeVuelta("state_invalido", 422).titulo).toBe("Esta vuelta de Argos no es válida");
        expect(mensajeVuelta("origen_no_permitido", 422).texto).toContain("IRIS_ARGOS_ORIGENES");
        expect(mensajeVuelta("codigo_invalido", 422).titulo).toBe("Argos no aceptó el código");
        expect(mensajeVuelta(undefined, 0).titulo).toBe("Sin conexión con Iris");
        expect(mensajeVuelta("otro", 500).titulo).toBe("No se pudo conectar con Argos");
    });
});

describe("panelDe", () => {
    it("el panel informado; si no, el configurado; nunca la API", () => {
        expect(panelDe({ panelUrl: "https://argos.gt", urlPublica: "https://argos.gt", apiUrl: "https://api.argos.gt" })).toBe("https://argos.gt");
        expect(panelDe({ panelUrl: null, urlPublica: "https://panel.gt", apiUrl: "https://api.argos.gt" })).toBe("https://panel.gt");
        expect(panelDe({ panelUrl: null, urlPublica: "https://api.argos.gt", apiUrl: "https://api.argos.gt" })).toBeNull();
    });
});

describe("textoAlcance", () => {
    it("distingue global, solo la lista y no informado", () => {
        expect(textoAlcance(true)).toContain("también las que se creen después");
        expect(textoAlcance(false)).toBe("Solo las empresas de la lista");
        expect(textoAlcance(undefined)).toBe("Argos no lo informó");
    });
});

describe("panelDesconocido / panelesDeArgos", () => {
    it("pide confirmar un panel que no es el de ningún conector de Argos", () => {
        expect(panelDesconocido("https://otro.gt", ["https://argos.gt"])).toBe(true);
        expect(panelDesconocido("https://argos.gt", ["https://argos.gt"])).toBe(false);
        expect(panelDesconocido("https://argos.gt", [])).toBe(false);
    });

    it("solo paneles conocidos de Argos, sin repetir", () => {
        const base = { panelUrl: null, urlPublica: "https://api.gt", apiUrl: "https://api.gt" };
        const c = (x: Partial<Conector>) => ({ ...base, sistema: "argosisp", ...x }) as Conector;
        expect(
            panelesDeArgos([
                c({ panelUrl: "https://argos.gt" }),
                c({ panelUrl: "https://argos.gt" }),
                c({}),
                c({ sistema: "horus", panelUrl: "https://horus.gt" }),
            ]),
        ).toEqual(["https://argos.gt"]);
    });
});

describe("mensajeVuelta en proceso", () => {
    it("un 409 idempotencia_en_proceso invita a reintentar", () => {
        expect(mensajeVuelta("idempotencia_en_proceso", 409).titulo).toBe("Iris sigue completando la conexión");
    });
});

describe("cuerpoCambioConector", () => {
    it("manda revocarAnterior solo al activar y si se marcó", () => {
        expect(cuerpoCambioConector("activar", 3, { motivo: "", revocarAnterior: false })).toEqual({ version: 3 });
        expect(cuerpoCambioConector("activar", 3, { motivo: "fuga", revocarAnterior: true })).toEqual({ version: 3, motivo: "fuga", revocarAnterior: true });
        expect(cuerpoCambioConector("desactivar", 3, { motivo: "", revocarAnterior: true })).toEqual({ version: 3 });
    });
});

describe("presentacionVersion", () => {
    it("cada resultado con su tono, icono y texto; sin revisar, neutro", () => {
        expect(presentacionVersion("ok")).toMatchObject({ tono: "success", texto: "Responde bien" });
        expect(presentacionVersion("ruta_no_existe").tono).toBe("warning");
        expect(presentacionVersion("sin_permiso").tono).toBe("error");
        expect(presentacionVersion("no_responde").texto).toBe("No responde");
        expect(presentacionVersion("error").tono).toBe("error");
        expect(presentacionVersion(null)).toMatchObject({ tono: "neutral", texto: "Sin revisar" });
        expect(presentacionVersion(undefined).texto).toBe("Sin revisar");
    });
});

describe("conectorArgos", () => {
    it("el activo de Argos o, si no hay, el más reciente; nunca el de Horus", () => {
        const c = (id: number, sistema: Conector["sistema"], estado: Conector["estado"]) => ({ id, sistema, estado }) as Conector;
        expect(conectorArgos([c(1, "argosisp", "apagado"), c(2, "argosisp", "activo"), c(3, "argosisp", "apagado")])?.id).toBe(2);
        expect(conectorArgos([c(1, "argosisp", "apagado"), c(3, "argosisp", "apagado")])?.id).toBe(3);
        expect(conectorArgos([c(4, "horus", "activo")])).toBeUndefined();
        expect(RUTA_DIAGNOSTICO_ARGOS).toBe("/administracion/integraciones#diagnostico-argos");
    });
});
