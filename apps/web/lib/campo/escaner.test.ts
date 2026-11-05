// Escáner por campo en la PWA: geometría del visor (BarcodeDetector en píxeles de la cámara, ZXing
// sin posición) y el flujo con lo que entrega el lector del navegador.
import { SesionEscaneo, type EstadoEscaneo } from "@iris/sync/campo";
import type { CodigoLeido, GeometriaVisor } from "@iris/ui";
import { describe, expect, it, vi } from "vitest";
import { codigosQueCuentan, vibrarCorto } from "./escaner";

// Cámara 1280×720 en un teléfono de 390 px (vista 390×600, `object-fit: cover`), recuadro al centro.
const GEOMETRIA: GeometriaVisor = {
    vista: { ancho: 390, alto: 600 },
    fuente: { ancho: 1280, alto: 720 },
    recuadro: { x: 39, y: 206, ancho: 312, alto: 187 },
};

// Etiqueta de una ONU ZTE: la serie al centro del cuadro, la MAC arriba y el EAN abajo (fuera).
const CUADRO: CodigoLeido[] = [
    { valor: "ZTEGC8F1A2B3", formato: "code_128", caja: { x: 520, y: 330, ancho: 240, alto: 60 } },
    { valor: "A0B1C2D3E4F6", formato: "code_128", caja: { x: 520, y: 40, ancho: 240, alto: 60 } },
    { valor: "6972632140024", formato: "ean_13", caja: { x: 520, y: 640, ancho: 240, alto: 60 } },
];

describe("escáner en el navegador", () => {
    it("con BarcodeDetector solo cuentan los del recuadro; con ZXing (sin posición) todos", () => {
        expect(codigosQueCuentan(CUADRO, GEOMETRIA).map((c) => c.valor)).toEqual(["ZTEGC8F1A2B3"]);
        const zxing = CUADRO.map(({ valor, formato }) => ({ valor, formato: formato.toUpperCase() }));
        expect(codigosQueCuentan(zxing, GEOMETRIA)).toHaveLength(3);
    });

    it("de lo que cuenta a la confirmación de un solo candidato, y «Seguir escaneando»", () => {
        vi.useFakeTimers();
        try {
            const estados: EstadoEscaneo[] = [];
            const vibrar = vi.fn(() => true);
            const s = new SesionEscaneo({ objetivo: "serie", alCambiar: (e) => estados.push(e), alPrimerCodigo: () => vibrarCorto({ vibrate: vibrar }) });
            s.detectar(codigosQueCuentan(CUADRO, GEOMETRIA));
            expect(vibrar).toHaveBeenCalledWith(40);
            // Un cuadro no basta: la lista sale al terminar la ventana (~800 ms).
            vi.advanceTimersByTime(700);
            expect(s.estado.candidatos).toBeNull();
            vi.advanceTimersByTime(100);
            expect(s.estado.candidatos?.map((c) => [c.valor, c.insignia])).toEqual([["ZTEGC8F1A2B3", "Parece serie"]]);
            s.seguir();
            expect(estados.at(-1)).toEqual({ detectados: 0, candidatos: null });
            s.liberar();
        } finally {
            vi.useRealTimers();
        }
    });

    it("sin `navigator.vibrate` (Safari) o si falla, no pasa nada", () => {
        expect(() => vibrarCorto({})).not.toThrow();
        expect(() => vibrarCorto(undefined)).not.toThrow();
        expect(() =>
            vibrarCorto({
                vibrate: () => {
                    throw new Error("bloqueado");
                },
            }),
        ).not.toThrow();
    });
});
