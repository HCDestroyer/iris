import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { CorteManual } from "@/lib/ordenes/presentacion";
import { CorteManualOrden, InsigniaPrioridad } from "./corte-manual";

const CORTE: CorteManual = {
    solicitudId: 7001,
    accion: "corte",
    causa: "smartolt_fallo",
    causaTexto: "SmartOLT agotó sus reintentos",
    componente: "cable",
    estado: "en_curso",
};

describe("corte manual de cable (contrato 0.15.0 §13.2)", () => {
    it("muestra la causa, que solo se toca el cable y el estado con texto", () => {
        const html = renderToStaticMarkup(<CorteManualOrden corte={CORTE} />);
        expect(html).toContain("Corte de cable pedido por Argos");
        expect(html).toContain("SmartOLT agotó sus reintentos");
        expect(html).toContain("el internet del servicio sigue igual");
        expect(html).toContain("En curso");
        expect(html).toContain("Solicitud 7001 de Argos");
        expect(html).not.toContain("corte-motivo");
    });

    it("no realizada: el motivo; cancelación pedida: el aviso", () => {
        const html = renderToStaticMarkup(<CorteManualOrden corte={{ ...CORTE, accion: "reconexion", estado: "no_realizada", motivoCodigo: "equipo_no_encontrado", cancelacionPedida: true }} />);
        expect(html).toContain("Reconexión de cable pedida por Argos");
        expect(html).toContain("No se pudo");
        expect(html).toContain("Equipo no encontrado");
        expect(html).toContain("Argos canceló la solicitud con el técnico en camino");
    });

    it("la prioridad alta se ve; la normal, no", () => {
        expect(renderToStaticMarkup(<InsigniaPrioridad prioridad="alta" />)).toContain("Prioridad alta");
        expect(renderToStaticMarkup(<InsigniaPrioridad prioridad="normal" />)).toBe("");
        expect(renderToStaticMarkup(<InsigniaPrioridad prioridad={undefined} />)).toBe("");
    });
});
