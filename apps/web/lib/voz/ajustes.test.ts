import { describe, expect, it } from "vitest";
import { entradaDeAjustes, entradaDeCanal, esquemaAjustesVoz, esquemaLlamadasCanal, motivoNoEncender, valoresDeAjustes, valoresDeCanal } from "./ajustes";
import type { AjustesVoz, LlamadasCanal } from "./tipos";

const CANAL: LlamadasCanal = {
    canalId: 3,
    tipoConexion: "api",
    disponible: true,
    llamadasActivas: false,
    colaId: 2,
    segundosSonando: 30,
    fueraDeHorario: "rechazar_con_mensaje",
    iconoVisible: true,
    devolucion: false,
    mensajePerdida: { activo: true, texto: "No pudimos contestarte." },
    requisitos: { cumple: true, requisitos: [] },
    whatsapp: { estado: "aplicado", intentos: 1 },
    version: 4,
};
const AJUSTES: AjustesVoz = { version: 3, grabar: "nunca", duracionMaxPersonaMin: 60, segundosAsignado: 10, maxSalientesPorDia: 50, topeMinutosSalientesMes: 300, maxColgadasMarcandoDia: 3 };

describe("ajustes de llamadas por número (espejo de `POST /canales/{id}/llamadas/configurar`)", () => {
    it("lo leído va y vuelve igual; sin cola ni horario no se mandan", () => {
        const v = valoresDeCanal(CANAL);
        expect(esquemaLlamadasCanal.safeParse(v).success).toBe(true);
        expect(entradaDeCanal(v, CANAL.version)).toEqual({ version: 4, llamadasActivas: false, colaId: 2, segundosSonando: 30, fueraDeHorario: "rechazar_con_mensaje", iconoVisible: true, devolucion: false, mensajePerdida: { activo: true, texto: "No pudimos contestarte." } });
        const sin = entradaDeCanal({ ...v, colaId: "", horarioId: "" }, 4);
        expect("colaId" in sin || "horarioId" in sin).toBe(false);
    });

    it("rangos y textos como el servidor: 15–45 s, mensaje de 1 a 500 aunque esté apagado, sin el asistente", () => {
        const base = valoresDeCanal(CANAL);
        const error = (cambio: Partial<typeof base>) => esquemaLlamadasCanal.safeParse({ ...base, ...cambio }).error?.issues[0]?.message;
        expect(error({ segundosSonando: "14" })).toBe("Entre 15 y 45 segundos");
        expect(error({ segundosSonando: "46" })).toBe("Entre 15 y 45 segundos");
        expect(error({ segundosSonando: "3.5" })).toBe("Entre 15 y 45 segundos");
        expect(error({ segundosSonando: "45" })).toBeUndefined();
        expect(error({ mensajeActivo: false, mensajeTexto: "  " })).toBe("Escribe el mensaje");
        expect(error({ mensajeTexto: "x".repeat(501) })).toBe("Hasta 500 caracteres");
        expect(error({ mensajeTexto: "hola\u0007" })).toBe("Tiene caracteres que no se pueden enviar");
        expect(error({ mensajeTexto: "Línea 1\nLínea 2" })).toBeUndefined();
        // En E1 el asistente no contesta llamadas: no es una opción y lo leído se muestra como rechazar.
        expect(esquemaLlamadasCanal.safeParse({ ...base, fueraDeHorario: "asistente" }).success).toBe(false);
        expect(valoresDeCanal({ ...CANAL, fueraDeHorario: "asistente" }).fueraDeHorario).toBe("rechazar_con_mensaje");
        expect(valoresDeCanal({ ...CANAL, fueraDeHorario: "buzon" }).fueraDeHorario).toBe("buzon");
    });

    it("por qué no se puede encender, en llano", () => {
        expect(motivoNoEncender(CANAL)).toBeNull();
        expect(motivoNoEncender({ ...CANAL, tipoConexion: "qr" })).toContain("QR");
        expect(motivoNoEncender({ ...CANAL, requisitos: { cumple: false, requisitos: [] } })).toContain("requisitos de Meta");
    });
});

describe("ajustes de llamadas de la empresa (campos de E1)", () => {
    it("rangos del servidor; la grabación no existe todavía y no se manda", () => {
        const error = (cambio: Record<string, string>) => esquemaAjustesVoz.safeParse({ ...valoresDeAjustes(AJUSTES), ...cambio }).error?.issues[0]?.message;
        expect(error({ duracionMaxPersonaMin: "9" })).toBe("Entre 10 y 120 minutos");
        expect(error({ duracionMaxPersonaMin: "121" })).toBe("Entre 10 y 120 minutos");
        expect(error({ segundosAsignado: "21" })).toBe("Entre 0 y 20 segundos");
        expect(error({ segundosAsignado: "0" })).toBeUndefined();
        expect(entradaDeAjustes({ duracionMaxPersonaMin: "45", segundosAsignado: "0" }, AJUSTES)).toEqual({ version: 3, duracionMaxPersonaMin: 45, segundosAsignado: 0 });
    });
});
