import { describe, expect, it } from "vitest";
import { corteEnCurso, detalleEtapa, huellaLegible, porRevisar, textoBotonSincronizar, textoInforme, type EtapaSincronizacion, type SincronizacionArgos } from "./estado-argos";

const etapa = (e: Partial<EtapaSincronizacion> & Pick<EtapaSincronizacion, "recurso" | "estado">): EtapaSincronizacion => ({
    procesados: 0,
    omitidos: 0,
    total: null,
    iniciadaEn: null,
    terminadaEn: null,
    error: null,
    ...e,
});

const sinc = (s: Partial<SincronizacionArgos>): SincronizacionArgos => ({
    id: "x",
    conectorId: 1,
    estado: "en_curso",
    origen: "activacion",
    excluirRetirados: true,
    contratoArgos: "0.8.0",
    iniciadaEn: "2026-11-19T20:00:00Z",
    actualizadaEn: "2026-11-19T20:00:00Z",
    terminadaEn: null,
    informeEstado: "pendiente",
    informadoEn: null,
    informeDetalle: null,
    etapas: [],
    ...s,
});

describe("estado de la conexión con Argos", () => {
    it("nunca inventa un total: sin total dice cuántos lleva", () => {
        expect(detalleEtapa(etapa({ recurso: "clientes", estado: "en_curso", procesados: 1200 }), undefined)).toBe("1,200 clientes leídos hasta ahora");
        expect(detalleEtapa(etapa({ recurso: "empresas", estado: "en_curso", procesados: 1, total: 2 }), undefined)).toBe("1 de 2 empresas");
        expect(detalleEtapa(etapa({ recurso: "servicios", estado: "completa", procesados: 3, omitidos: 1 }), undefined)).toContain("1 sin guardar");
        expect(detalleEtapa(etapa({ recurso: "cuentas_bancarias", estado: "con_errores" }), undefined)).toBe("No se cargó");
    });

    it("los servicios esperan a los clientes", () => {
        const clientes = etapa({ recurso: "clientes", estado: "en_curso" });
        expect(detalleEtapa(etapa({ recurso: "servicios", estado: "pendiente" }), clientes)).toBe("Espera a los clientes");
        expect(detalleEtapa(etapa({ recurso: "zonas", estado: "pendiente" }), undefined)).toBe("Todavía no empieza");
    });

    it("el botón y el informe según el estado", () => {
        expect(textoBotonSincronizar(undefined)).toBe("Sincronizar");
        expect(textoBotonSincronizar(sinc({ estado: "completa" }))).toBe("Sincronizar de nuevo");
        expect(textoBotonSincronizar(sinc({ estado: "con_errores" }))).toBe("Reanudar");
        expect(textoInforme(sinc({ informeEstado: "descartado" }), (f) => f)).toContain("más nuevo");
        expect(textoInforme(sinc({ informeEstado: "pendiente", excluirRetirados: false }), (f) => f)).toContain("no recibe");
        expect(textoInforme(sinc({ informeEstado: "informado", informadoEn: "AYER" }), (f) => f)).toBe("Avance informado a Argos el AYER.");
    });

    it("el corte solo cuenta con una etapa en curso con error", () => {
        const conCorte = sinc({ etapas: [etapa({ recurso: "clientes", estado: "en_curso", error: "Argos no respondió" })] });
        expect(corteEnCurso(conCorte)?.recurso).toBe("clientes");
        expect(corteEnCurso({ ...conCorte, estado: "con_errores" })).toBeUndefined();
    });

    it("cuenta lo que hay por revisar y formatea la huella", () => {
        expect(porRevisar({ webhooksConError: 1, ultimoWebhookErrorEn: null, webhooksRechazados: 2, pendientes: [{ estado: "agotado", cuantos: 3 }] })).toBe(6);
        expect(porRevisar({ webhooksConError: 0, ultimoWebhookErrorEn: null, webhooksRechazados: 0, pendientes: null })).toBe(0);
        expect(huellaLegible("5f2a9c")).toBe("5F:2A:9C");
    });
});
