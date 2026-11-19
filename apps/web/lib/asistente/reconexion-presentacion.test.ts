import { describe, expect, it } from "vitest";
import { textoReconexion, textoReconexionActualizada } from "./fase3b";
import { estadoSinPersonal, hastaCorto, textoMotivoReconexion } from "./reconexion-presentacion";

const listo = { argosLoPermite: true, contratoMinimo: "0.8.0", caja: { total: 2, disponibles: 0 }, accionEncendida: true };

describe("reconexión sin personal de caja (contrato con Argos 0.8.0 §7.3)", () => {
    it("el motivo en llano; uno desconocido no se interpreta", () => {
        expect(textoMotivoReconexion("fuera_de_horario")).toBe("fuera de horario");
        expect(textoMotivoReconexion("sin_personal")).toBe("nadie de caja disponible");
        expect(textoMotivoReconexion("otro")).toBeNull();
        expect(textoMotivoReconexion(undefined)).toBeNull();
    });

    it("estado del interruptor: apagado, Argos viejo, sin rol Caja y listo", () => {
        expect(estadoSinPersonal(listo, false)).toMatchObject({ tono: "info", titulo: "Apagado" });
        expect(estadoSinPersonal({ ...listo, accionEncendida: false }, true)).toMatchObject({ tono: "warning", titulo: "«Reconexión temporal» está apagada" });
        expect(estadoSinPersonal({ ...listo, accionEncendida: false }, false)).toMatchObject({ titulo: "Apagado" });
        expect(estadoSinPersonal({ ...listo, argosLoPermite: false }, true)).toMatchObject({ tono: "warning", titulo: "Argos todavía no lo permite" });
        expect(estadoSinPersonal({ ...listo, argosLoPermite: false }, true).detalle).toContain("0.8.0");
        expect(estadoSinPersonal({ ...listo, caja: { total: 0, disponibles: 0 } }, true)).toMatchObject({ tono: "warning", titulo: "Nadie tiene el rol Caja" });
        expect(estadoSinPersonal(listo, true)).toEqual({ tono: "success", titulo: "Listo", detalle: "2 personas de caja; ninguna está disponible ahora." });
        expect(estadoSinPersonal({ ...listo, caja: { total: 1, disponibles: 1 } }, true).detalle).toBe("1 persona de caja; 1 está disponible ahora.");
    });

    it("hasta en hora de Guatemala; un instante inválido no se inventa", () => {
        expect(hastaCorto("2026-11-19T16:00:00.000Z")).toMatch(/19.*10:00/);
        expect(hastaCorto("no")).toBeNull();
        expect(hastaCorto(undefined)).toBeNull();
    });

    it("el hilo dice el motivo y hasta cuándo vale, nunca una hora de corte", () => {
        const t = textoReconexion("Asistente de IA", "aceptada", "sin_personal", "2026-11-19T16:00:00.000Z");
        expect(t).toMatch(/^Reconexión temporal: Argos la aceptó · nadie de caja disponible · vale hasta .*10:00 \(la pidió Asistente de IA\)$/);
        expect(textoReconexion("Asistente de IA", "rechazada", "fuera_de_horario")).toBe("Reconexión temporal: Argos no la aceptó · fuera de horario (la pidió Asistente de IA)");
        // Sin motivo (un servidor anterior): como antes.
        expect(textoReconexion("Asistente de IA", "aceptada")).toBe("Reconexión temporal: Argos la aceptó (la pidió Asistente de IA)");
        for (const x of [t, textoReconexionActualizada("cancelada", "sin_personal", "vencida_sin_revisar"), textoReconexionActualizada("activa", "sin_personal", undefined, "2026-11-19T06:00:00.000Z")]) {
            expect(x.toLowerCase()).not.toContain("se corta");
            expect(x.toLowerCase()).not.toContain("medianoche");
        }
    });

    it("cómo terminó en Argos", () => {
        expect(textoReconexionActualizada("cancelada", "sin_personal", "vencida_sin_revisar")).toBe(
            "Reconexión temporal cancelada: venció sin que caja revisara el comprobante · nadie de caja disponible",
        );
        expect(textoReconexionActualizada("cancelada", "fuera_de_horario", "comprobante_rechazado")).toBe("Reconexión temporal cancelada: caja rechazó el comprobante · fuera de horario");
        expect(textoReconexionActualizada("cancelada", undefined, "otro")).toBe("Reconexión temporal cancelada");
        expect(textoReconexionActualizada("cumplida", "sin_personal")).toBe("Reconexión temporal: el pago se confirmó · nadie de caja disponible");
        expect(textoReconexionActualizada("activa", "sin_personal", undefined, "2026-11-19T16:00:00Z")).toMatch(/^Reconexión temporal activa en Argos hasta .*10:00 · nadie de caja disponible$/);
    });
});
