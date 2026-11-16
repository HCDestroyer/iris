import { describe, expect, it } from "vitest";
import {
    ACCIONES_POR_DEFECTO,
    ajustesDePrueba,
    completar,
    duracionLegible,
    entradaCategoria,
    entradaGuardada,
    esDelAsistente,
    esquemaCategoria,
    esquemaLimites,
    esquemaPrincipal,
    minutosLegibles,
    proveedorConectado,
    resumenModo,
    textoHaria,
    tonoDe,
    valoresCategoria,
    valoresPrincipal,
    type AjustesFase3,
    type Categoria,
} from "./fase3";

const BASE: AjustesFase3 = {
    activo: true,
    proveedor: "ollama",
    url: "http://10.0.0.20:11434",
    claveGuardadaEn: null,
    modelo: "qwen3:32b",
    modo: "automatico",
    fuentes: { manuales: true, argos: true, horus: true, historial: true },
    topeTokensMes: 5_000_000,
    precioEntradaMillon: null,
    precioSalidaMillon: null,
    version: 4,
};

const CATEGORIA: Categoria = { id: 2, nombre: "Sin internet", descripcion: "No tiene servicio", tono: "peligro", colaId: 2, slaPrimeraRespuestaMin: 10, modo: "responde_solo", condicion: "si_hay_falla", manualIds: [1], activa: true, version: 3 };

describe("ajustes de la fase 3", () => {
    it("un servidor anterior a §17 queda en «Nunca», con las acciones por defecto y sin actuar solo", () => {
        const a = completar(BASE);
        expect(a).toMatchObject({ modoAutonomo: "nunca", esperaMin: 3, autonomoDisponible: false, limites: { prioridadMaxima: "alta", diasAdelanteMax: 2, maxTurnos: 8 } });
        expect(a.acciones).toEqual(ACCIONES_POR_DEFECTO);
        expect(a.acciones.promesa_pago).toBe(false);
    });

    it("lo que manda el servidor gana, también a medias", () => {
        const a = completar({ ...BASE, modoAutonomo: "sin_nadie", esperaMin: 5, autonomoDisponible: true, acciones: { ...ACCIONES_POR_DEFECTO, promesa_pago: true } });
        expect(a).toMatchObject({ modoAutonomo: "sin_nadie", esperaMin: 5, autonomoDisponible: true });
        expect(a.acciones.promesa_pago).toBe(true);
    });

    it("la entrada con lo guardado no lleva la clave ni lo que solo es de lectura", () => {
        const e = entradaGuardada(completar({ ...BASE, claveGuardadaEn: "2026-10-20T15:00:00Z" }));
        expect(e).toMatchObject({ proveedor: "ollama", version: 4, modoAutonomo: "nunca" });
        expect(e).not.toHaveProperty("clave");
        expect(e).not.toHaveProperty("claveGuardadaEn");
        expect(e).not.toHaveProperty("autonomoDisponible");
    });

    it("proveedor conectado: dirección y modelo, o la clave con Anthropic", () => {
        expect(proveedorConectado(BASE)).toBe(true);
        expect(proveedorConectado({ ...BASE, modelo: " " })).toBe(false);
        expect(proveedorConectado({ ...BASE, url: "" })).toBe(false);
        expect(proveedorConectado({ ...BASE, proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: null })).toBe(false);
        expect(proveedorConectado({ ...BASE, proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: "2026-10-20T15:00:00Z" })).toBe(true);
    });

    it("el formulario principal valida la espera y arma la prueba con lo que hay sin guardar", () => {
        const v = valoresPrincipal(completar(BASE), [CATEGORIA]);
        expect(v.modos).toEqual({ "2": "responde_solo" });
        expect(esquemaPrincipal.safeParse(v).success).toBe(true);
        expect(esquemaPrincipal.safeParse({ ...v, esperaMin: 0 }).success).toBe(false);
        expect(esquemaPrincipal.safeParse({ ...v, modoAutonomo: "a_veces" }).success).toBe(false);
        expect(ajustesDePrueba({ ...v, modoAutonomo: "siempre_primero", modos: { "2": "sugiere" } })).toEqual({
            modoAutonomo: "siempre_primero",
            esperaMin: 3,
            acciones: ACCIONES_POR_DEFECTO,
            categorias: [{ id: 2, modo: "sugiere" }],
        });
    });

    it("límites: enteros dentro del rango", () => {
        const ok = { prioridadMaxima: "alta", diasAdelanteMax: "2", maxTurnos: "8" };
        expect(esquemaLimites.safeParse(ok).success).toBe(true);
        expect(esquemaLimites.safeParse({ ...ok, diasAdelanteMax: "0" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...ok, diasAdelanteMax: "2.5" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...ok, maxTurnos: "31" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...ok, prioridadMaxima: "urgente" }).success).toBe(false);
    });
});

describe("categorías", () => {
    it("ida y vuelta del formulario; «Sugiere» quita la condición", () => {
        const v = valoresCategoria(CATEGORIA);
        expect(v).toMatchObject({ colaId: "2", slaPrimeraRespuestaMin: "10", condicion: "si_hay_falla", tono: "peligro" });
        expect(entradaCategoria(v, 3)).toEqual({ nombre: "Sin internet", descripcion: "No tiene servicio", tono: "peligro", colaId: 2, slaPrimeraRespuestaMin: 10, modo: "responde_solo", condicion: "si_hay_falla", manualIds: [1], version: 3 });
        expect(entradaCategoria({ ...v, modo: "sugiere" }).condicion).toBeNull();
        expect(entradaCategoria({ ...v, condicion: "siempre", colaId: "", slaPrimeraRespuestaMin: "" })).toMatchObject({ condicion: null, colaId: null, slaPrimeraRespuestaMin: null });
    });

    it("nueva: sugiere, activa y con el primer color", () => {
        expect(valoresCategoria()).toMatchObject({ nombre: "", modo: "sugiere", condicion: "siempre", activa: true, tono: "info", manualIds: [] });
    });

    it("valida nombre, tiempo y cantidad de manuales", () => {
        const v = valoresCategoria(CATEGORIA);
        expect(esquemaCategoria.safeParse(v).success).toBe(true);
        expect(esquemaCategoria.safeParse({ ...v, nombre: "  " }).success).toBe(false);
        expect(esquemaCategoria.safeParse({ ...v, slaPrimeraRespuestaMin: "0" }).success).toBe(false);
        expect(esquemaCategoria.safeParse({ ...v, slaPrimeraRespuestaMin: "" }).success).toBe(true);
        expect(esquemaCategoria.safeParse({ ...v, manualIds: Array.from({ length: 11 }, (_, i) => i) }).success).toBe(false);
    });

    it("textos del modo y del tiempo", () => {
        expect(resumenModo(CATEGORIA)).toBe("Responde solo si hay falla; si no, a la cola");
        expect(resumenModo({ modo: "responde_solo", condicion: "solo_verificados" })).toBe("Responde solo a verificados");
        expect(resumenModo({ modo: "responde_solo", condicion: null })).toBe("Responde solo");
        expect(resumenModo({ modo: "sugiere", condicion: "si_hay_falla" })).toBe("Sugiere al agente");
        expect(minutosLegibles(15)).toBe("15 min");
        expect(minutosLegibles(120)).toBe("2 h");
        expect(minutosLegibles(90)).toBe("1 h 30 min");
    });

    it("un tono que el panel no conoce se ve neutro", () => {
        expect(tonoDe("peligro")).toBe("peligro");
        expect(tonoDe("fucsia")).toBe("neutro");
        expect(tonoDe("")).toBe("neutro");
    });
});

describe("«Pruébalo» y efecto", () => {
    it("qué haría, con sus fuentes", () => {
        const manual = { tipo: "manual" as const, etiqueta: "Manual «Planes FTTH y WISP»", entidadId: "2" };
        expect(textoHaria({ haria: "responder_solo", fuentes: [manual], respuesta: "Hola" })).toBe("Respondería solo, con Manual «Planes FTTH y WISP»:");
        expect(textoHaria({ haria: "sugerir", fuentes: [], respuesta: "Hola" })).toBe("Le sugeriría al agente:");
        expect(textoHaria({ haria: "pasar_a_persona", fuentes: [], respuesta: null })).toBe("Pasaría a una persona.");
    });

    it("duraciones cortas en palabras", () => {
        expect(duracionLegible(45)).toBe("45 s");
        expect(duracionLegible(100)).toBe("1 min 40 s");
        expect(duracionLegible(840)).toBe("14 min");
        expect(duracionLegible(120)).toBe("2 min");
        expect(duracionLegible(7500)).toBe("2 h 5 min");
    });

    it("un mensaje es del asistente solo si el servidor lo dice", () => {
        expect(esDelAsistente({ autorTipo: "asistente_ia" })).toBe(true);
        expect(esDelAsistente({ automatico: "asistente_ia" })).toBe(true);
        expect(esDelAsistente({ automatico: "bot" })).toBe(false);
        expect(esDelAsistente({ autorTipo: "persona", automatico: null })).toBe(false);
        expect(esDelAsistente({})).toBe(false);
    });
});
