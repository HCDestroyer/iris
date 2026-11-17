import { describe, expect, it } from "vitest";
import { consumoSalientes, entradaDeAjustes, entradaDeCanal, esquemaAjustesVoz, esquemaLlamadasCanal, motivoNoEncender, resumenSalientes, valoresDeAjustes, valoresDeCanal } from "./ajustes";
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
const AJUSTES: AjustesVoz = { version: 3, grabar: "nunca", duracionMaxPersonaMin: 60, segundosAsignado: 10, maxSalientesPorDia: 50, topeMinutosSalientesMes: 300, maxColgadasMarcandoDia: 3, minutosSalientesMes: 0, salientesHoy: 0 };

describe("ajustes de llamadas por número (espejo de `POST /canales/{id}/llamadas/configurar`)", () => {
    it("lo leído va y vuelve igual; sin cola ni horario no se mandan", () => {
        const v = valoresDeCanal(CANAL);
        expect(esquemaLlamadasCanal.safeParse(v).success).toBe(true);
        expect(entradaDeCanal(v, CANAL.version)).toEqual({ version: 4, llamadasActivas: false, colaId: 2, segundosSonando: 30, fueraDeHorario: "rechazar_con_mensaje", iconoVisible: true, devolucion: false, mensajePerdida: { activo: true, texto: "No pudimos contestarte." }, plantillaPermiso: "" });
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
        expect(entradaDeAjustes({ ...valoresDeAjustes(AJUSTES), duracionMaxPersonaMin: "45", segundosAsignado: "0" }, AJUSTES)).toEqual({
            version: 3,
            duracionMaxPersonaMin: 45,
            segundosAsignado: 0,
            maxSalientesPorDia: 50,
            topeMinutosSalientesMes: 300,
            maxColgadasMarcandoDia: 3,
        });
    });

    it("E1b: límites de las salientes (0 apaga las llamadas; en las colgadas, 0 es sin límite)", () => {
        const error = (cambio: Record<string, string>) => esquemaAjustesVoz.safeParse({ ...valoresDeAjustes(AJUSTES), ...cambio }).error?.issues[0]?.message;
        expect(error({ maxSalientesPorDia: "1001" })).toBe("Entre 0 y 1000 llamadas");
        expect(error({ maxSalientesPorDia: "0" })).toBeUndefined();
        expect(error({ topeMinutosSalientesMes: "100001" })).toBe("Entre 0 y 100 000 minutos");
        expect(error({ topeMinutosSalientesMes: "1.5" })).toBe("Entre 0 y 100 000 minutos");
        expect(error({ maxColgadasMarcandoDia: "21" })).toBe("Entre 0 y 20");
        expect(error({ maxColgadasMarcandoDia: "0" })).toBeUndefined();
        expect(resumenSalientes(valoresDeAjustes(AJUSTES))).toBe("Hasta 50 llamadas salientes al día · 300 minutos al mes");
        expect(resumenSalientes({ ...valoresDeAjustes(AJUSTES), topeMinutosSalientesMes: "0" })).toBe("Las llamadas salientes están apagadas");
    });
});

describe("consumo junto a los topes de salientes", () => {
    it("dice lo que se lleva; cerca desde el 80 % y tope al llegar; sin dato o con el tope en 0 no inventa nada", () => {
        expect(consumoSalientes({ ...AJUSTES, minutosSalientesMes: 120, salientesHoy: 8 })).toEqual({
            mes: { texto: "Llevas 120 de 300 minutos este mes", nivel: "normal" },
            hoy: { texto: "8 de 50 llamadas hoy", nivel: "normal" },
        });
        expect(consumoSalientes({ ...AJUSTES, minutosSalientesMes: 239.6, salientesHoy: 40 })).toMatchObject({ mes: { texto: "Llevas 239 de 300 minutos este mes", nivel: "normal" }, hoy: { nivel: "cerca" } });
        expect(consumoSalientes({ ...AJUSTES, minutosSalientesMes: 240, salientesHoy: 50 })).toMatchObject({ mes: { nivel: "cerca" }, hoy: { nivel: "tope" } });
        expect(consumoSalientes({ ...AJUSTES, minutosSalientesMes: 310 }).mes?.nivel).toBe("tope");
        // Salientes apagadas (tope 0) o un servidor que no manda el dato.
        expect(consumoSalientes({ ...AJUSTES, topeMinutosSalientesMes: 0, maxSalientesPorDia: 0 })).toEqual({ mes: null, hoy: null });
        expect(consumoSalientes({ topeMinutosSalientesMes: 300, maxSalientesPorDia: 50 })).toEqual({ mes: null, hoy: null });
    });
});

describe("E1b: plantilla de permiso de llamada del número", () => {
    it("nombre como en Meta e idioma; vacía se quita y el idioma solo viaja con plantilla", () => {
        const base = valoresDeCanal({ ...CANAL, plantillaPermiso: "permiso_llamada", plantillaPermisoIdioma: "es" });
        expect(base).toMatchObject({ plantillaPermiso: "permiso_llamada", plantillaPermisoIdioma: "es" });
        const error = (cambio: Partial<typeof base>) => esquemaLlamadasCanal.safeParse({ ...base, ...cambio }).error?.issues[0]?.message;
        expect(error({})).toBeUndefined();
        expect(error({ plantillaPermiso: "Permiso Llamada" })).toBe("Solo minúsculas, números y guion bajo, como en Meta");
        expect(error({ plantillaPermisoIdioma: "español" })).toBe("El código del idioma, como «es» o «es_MX»");
        expect(error({ plantillaPermisoIdioma: "es_MX" })).toBeUndefined();
        expect(error({ plantillaPermiso: "", plantillaPermisoIdioma: "" })).toBeUndefined();
        expect(entradaDeCanal(base, 4)).toMatchObject({ plantillaPermiso: "permiso_llamada", plantillaPermisoIdioma: "es" });
        const sin = entradaDeCanal({ ...base, plantillaPermiso: "" }, 4);
        expect(sin.plantillaPermiso).toBe("");
        expect("plantillaPermisoIdioma" in sin).toBe(false);
    });
});
