import { describe, expect, it } from "vitest";
import {
    ajustesDePrueba,
    asistenteAtiende,
    cambianAjustes,
    COLOR_DE_TONO,
    completar,
    conPerfilDefecto,
    duracionLegible,
    entradaCategoria,
    entradaDeCategoria,
    entradaGuardada,
    entradaLimites,
    entradaPrincipal,
    esDelAsistente,
    esquemaCategoria,
    esquemaLimites,
    esquemaPrincipal,
    minutosLegibles,
    modoDe,
    PERFIL_POR_DEFECTO,
    perfilDefecto,
    porColaDe,
    porcentajeSinPersona,
    proveedorConectado,
    resumenModo,
    textoHaria,
    textoMotivo,
    tonoDe,
    valoresCategoria,
    valoresLimites,
    valoresPrincipal,
    type Categoria,
    type PerfilAsistente,
} from "./fase3";
import type { AjustesAsistente } from "./tipos";

const NOCHE: PerfilAsistente = { codigo: "noche", nombre: "Noche", acciones: { ...PERFIL_POR_DEFECTO.acciones, promesa_pago: true }, limites: { prioridadMaxima: "media", diasAdelanteMax: 1 } };

const BASE: AjustesAsistente = {
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
    autonomoDisponible: false,
    modoAutonomo: "nunca",
    esperaMin: 3,
    maxTurnos: 8,
    perfiles: [PERFIL_POR_DEFECTO, NOCHE],
    porCola: [{ colaId: 4, accionesApagadas: ["promesa_pago", "accion_que_no_existe"] }],
    version: 4,
};

const CATEGORIA: Categoria = {
    id: 2,
    nombre: "Sin internet",
    descripcion: "No tiene servicio",
    tono: "error",
    colaId: 2,
    slaPrimeraRespuestaMin: 10,
    modo: "responde_solo",
    condicion: "si_hay_falla",
    manualIds: [1],
    esOtro: false,
    activa: true,
    orden: 2,
    version: 3,
};

describe("ajustes de la fase 3 (§17.1)", () => {
    it("el perfil «defecto» es el primero; sin perfiles, el de fábrica", () => {
        expect(perfilDefecto(BASE).codigo).toBe("defecto");
        expect(perfilDefecto({ perfiles: null })).toEqual(PERFIL_POR_DEFECTO);
        expect(perfilDefecto({ perfiles: [] }).acciones.promesa_pago).toBe(false);
    });

    it("cambiar el «defecto» conserva los perfiles por horario", () => {
        const p = conPerfilDefecto(BASE, { limites: { prioridadMaxima: "baja", diasAdelanteMax: 5 } });
        expect(p.map((x) => x.codigo)).toEqual(["defecto", "noche"]);
        expect(p[0]?.limites).toEqual({ prioridadMaxima: "baja", diasAdelanteMax: 5 });
        expect(p[0]?.acciones).toEqual(PERFIL_POR_DEFECTO.acciones);
        expect(p[1]).toEqual(NOCHE);
        expect(conPerfilDefecto({ perfiles: null }, {})).toEqual([PERFIL_POR_DEFECTO]);
    });

    it("la entrada con lo guardado no lleva la clave, lo de solo lectura ni lo opcional de §17", () => {
        const e = entradaGuardada({ ...BASE, claveGuardadaEn: "2026-10-20T15:00:00Z" });
        expect(e).toMatchObject({ proveedor: "ollama", version: 4, modo: "automatico" });
        for (const campo of ["clave", "claveGuardadaEn", "autonomoDisponible", "modoAutonomo", "perfiles", "porCola", "maxTurnos", "esperaMin"]) expect(e).not.toHaveProperty(campo);
    });

    it("proveedor conectado: dirección y modelo, o la clave con Anthropic", () => {
        expect(proveedorConectado(BASE)).toBe(true);
        expect(proveedorConectado({ ...BASE, modelo: " " })).toBe(false);
        expect(proveedorConectado({ ...BASE, url: "" })).toBe(false);
        expect(proveedorConectado({ ...BASE, proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: null })).toBe(false);
        expect(proveedorConectado({ ...BASE, proveedor: "anthropic", url: "", modelo: "", claveGuardadaEn: "2026-10-20T15:00:00Z" })).toBe(true);
    });

    it("la pantalla principal: valores, qué cambió y lo que manda", () => {
        const v = valoresPrincipal(BASE, [CATEGORIA]);
        expect(v.modos).toEqual({ "2": "responde_solo" });
        // `pasar_a_persona` no es un interruptor: siempre va encendida.
        expect(v.acciones).not.toHaveProperty("pasar_a_persona");
        expect(esquemaPrincipal.safeParse(v).success).toBe(true);
        expect(esquemaPrincipal.safeParse({ ...v, esperaMin: 0 }).success).toBe(false);
        expect(esquemaPrincipal.safeParse({ ...v, modoAutonomo: "a_veces" }).success).toBe(false);
        expect(cambianAjustes(BASE, v)).toBe(false);
        // Cambiar solo el modo de una categoría no toca los ajustes.
        expect(cambianAjustes(BASE, { ...v, modos: { "2": "sugiere" } })).toBe(false);
        const d = { ...v, acciones: { ...v.acciones, promesa_pago: true } };
        expect(cambianAjustes(BASE, d)).toBe(true);
        const e = entradaPrincipal(BASE, d);
        expect(e).toMatchObject({ activo: true, modoAutonomo: "nunca", esperaMin: 3, version: 4 });
        expect(e.perfiles?.[0]?.acciones).toMatchObject({ promesa_pago: true, pasar_a_persona: true });
        expect(e.perfiles?.[1]).toEqual(NOCHE);
        expect(e).not.toHaveProperty("porCola");
        expect(e).not.toHaveProperty("maxTurnos");
    });

    it("límites: enteros dentro del rango del servidor y lo apagado por cola", () => {
        const v = valoresLimites(BASE);
        // Una acción que el panel no conoce no se muestra en el formulario…
        expect(v).toEqual({ prioridadMaxima: "alta", diasAdelanteMax: "7", maxTurnos: "8", presupuestoAutonomoPct: "70", verificacionFuerteMin: "60", porCola: { "4": ["promesa_pago"] } });
        // …pero se conserva al guardar: la lista reemplaza a la guardada y no debe borrar lo que no entiende.
        expect(entradaLimites(BASE, v).porCola).toEqual([{ colaId: 4, accionesApagadas: ["promesa_pago", "accion_que_no_existe"] }]);
        // También si se desmarca todo lo conocido de esa cola, y en colas que el formulario no trae.
        expect(porColaDe(BASE, { "4": [] })).toEqual([{ colaId: 4, accionesApagadas: ["accion_que_no_existe"] }]);
        expect(porColaDe({ porCola: [{ colaId: 9, accionesApagadas: ["crear_ticket", "otra_nueva"] }] }, { "2": ["dar_saldo"] })).toEqual([
            { colaId: 2, accionesApagadas: ["dar_saldo"] },
            { colaId: 9, accionesApagadas: ["crear_ticket", "otra_nueva"] },
        ]);
        expect(porColaDe({ porCola: null }, { "2": [] })).toEqual([]);
        expect(esquemaLimites.safeParse(v).success).toBe(true);
        expect(esquemaLimites.safeParse({ ...v, diasAdelanteMax: "0" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...v, diasAdelanteMax: "15" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...v, diasAdelanteMax: "2.5" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...v, maxTurnos: "1" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...v, maxTurnos: "21" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...v, prioridadMaxima: "urgente" }).success).toBe(false);
        expect(esquemaLimites.safeParse({ ...v, porCola: { "4": ["pasar_a_persona"] } }).success).toBe(false);
        const e = entradaLimites(BASE, { ...v, diasAdelanteMax: "3", maxTurnos: "6", porCola: { "2": ["agendar_visita"], "4": [] } });
        expect(e.maxTurnos).toBe(6);
        expect(e.perfiles?.[0]?.limites).toEqual({ prioridadMaxima: "alta", diasAdelanteMax: 3 });
        expect(e.perfiles?.[1]).toEqual(NOCHE);
        // Una cola sin nada conocido apagado solo viaja si tenía algo que el panel no conoce.
        expect(e.porCola).toEqual([
            { colaId: 2, accionesApagadas: ["agendar_visita"] },
            { colaId: 4, accionesApagadas: ["accion_que_no_existe"] },
        ]);
        expect(e).not.toHaveProperty("modoAutonomo");
    });
});

describe("categorías (§17.2)", () => {
    it("ida y vuelta del formulario; «Sugiere» quita la condición", () => {
        const v = valoresCategoria(CATEGORIA);
        expect(v).toMatchObject({ colaId: "2", slaPrimeraRespuestaMin: "10", condicion: "si_hay_falla", tono: "error" });
        expect(entradaCategoria(v)).toEqual({ nombre: "Sin internet", descripcion: "No tiene servicio", tono: "error", colaId: 2, slaPrimeraRespuestaMin: 10, modo: "responde_solo", condicion: "si_hay_falla", manualIds: [1] });
        expect(entradaCategoria({ ...v, modo: "sugiere" }).condicion).toBeNull();
        expect(entradaCategoria({ ...v, condicion: "siempre", colaId: "", slaPrimeraRespuestaMin: "" })).toMatchObject({ condicion: null, colaId: null, slaPrimeraRespuestaMin: null });
        expect(entradaDeCategoria({ ...CATEGORIA, manualIds: null })).toEqual({ ...entradaCategoria(v), manualIds: [] });
    });

    it("sin permiso de configurar llega solo la ficha (sin `modo`): se lee como «Sugiere»", () => {
        const ficha: Categoria = { id: 2, nombre: "Sin internet", tono: "error", activa: true, esOtro: false, descripcion: "", colaId: null, slaPrimeraRespuestaMin: null, condicion: null, manualIds: [], orden: 0, version: 0 };
        expect(modoDe(ficha)).toBe("sugiere");
        expect(modoDe(CATEGORIA)).toBe("responde_solo");
        expect(resumenModo({ condicion: null })).toBe("Sugiere al agente");
        expect(valoresPrincipal(BASE, [ficha]).modos).toEqual({ "2": "sugiere" });
    });

    it("un servidor anterior a §17 queda en «Nunca», sin actuar solo y con los límites de fábrica", () => {
        const viejo: AjustesAsistente = {
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
        expect(completar(viejo)).toMatchObject({ autonomoDisponible: false, modoAutonomo: "nunca", esperaMin: 3, maxTurnos: 8 });
        expect(valoresPrincipal(viejo, [])).toMatchObject({ modoAutonomo: "nunca", esperaMin: 3, acciones: { promesa_pago: false, responder_dudas: true } });
        expect(valoresLimites(viejo)).toEqual({ prioridadMaxima: "alta", diasAdelanteMax: "7", maxTurnos: "8", presupuestoAutonomoPct: "70", verificacionFuerteMin: "60", porCola: {} });
    });

    it("nueva: sugiere, activa y con el primer color", () => {
        expect(valoresCategoria()).toMatchObject({ nombre: "", modo: "sugiere", condicion: "siempre", activa: true, tono: "info", manualIds: [] });
    });

    it("valida nombre, descripción, tiempo y cantidad de manuales como el servidor", () => {
        const v = valoresCategoria(CATEGORIA);
        expect(esquemaCategoria.safeParse(v).success).toBe(true);
        expect(esquemaCategoria.safeParse({ ...v, nombre: "  " }).success).toBe(false);
        expect(esquemaCategoria.safeParse({ ...v, nombre: "x".repeat(41) }).success).toBe(false);
        expect(esquemaCategoria.safeParse({ ...v, descripcion: "x".repeat(301) }).success).toBe(false);
        expect(esquemaCategoria.safeParse({ ...v, slaPrimeraRespuestaMin: "0" }).success).toBe(false);
        expect(esquemaCategoria.safeParse({ ...v, slaPrimeraRespuestaMin: "" }).success).toBe(true);
        expect(esquemaCategoria.safeParse({ ...v, tono: "peligro" }).success).toBe(false);
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

    it("cada tono tiene su color de Horus; uno que el panel no conoce se ve neutro", () => {
        expect(COLOR_DE_TONO).toEqual({ info: "info", error: "peligro", ok: "exito", aviso: "alerta", neutro: "neutro" });
        expect(tonoDe("error")).toBe("error");
        expect(tonoDe("fucsia")).toBe("neutro");
        expect(tonoDe("")).toBe("neutro");
    });
});

describe("«Pruébalo», efecto y bandeja", () => {
    it("la prueba lleva lo que hay en el formulario, sin guardar", () => {
        const v = valoresPrincipal(BASE, [CATEGORIA]);
        const a = ajustesDePrueba({ ...v, modoAutonomo: "siempre_primero", modos: { "2": "sugiere" }, acciones: { ...v.acciones, crear_ticket: false } }, BASE, [CATEGORIA, { ...CATEGORIA, id: 9, nombre: "Vieja", activa: false }]);
        expect(a.modoAutonomo).toBe("siempre_primero");
        expect(a.perfil).toEqual({ acciones: { ...PERFIL_POR_DEFECTO.acciones, crear_ticket: false }, limites: PERFIL_POR_DEFECTO.limites, exigeVerificacionFuerte: { dar_saldo: true } });
        // Solo las activas; con «Sugiere», sin condición.
        expect(a.categorias).toEqual([{ id: 2, nombre: "Sin internet", descripcion: "No tiene servicio", tono: "error", modo: "sugiere", condicion: null, manualIds: [1] }]);
    });

    it("qué haría, con sus fuentes y su motivo", () => {
        const manual = { tipo: "manual" as const, etiqueta: "Manual «Planes FTTH y WISP»", entidadId: "2" };
        expect(textoHaria({ haria: "responde_solo", fuentes: [manual], respuesta: "Hola" })).toBe("Respondería solo, con Manual «Planes FTTH y WISP»:");
        expect(textoHaria({ haria: "sugiere", fuentes: null, respuesta: "Hola" })).toBe("Le sugeriría al agente:");
        expect(textoHaria({ haria: "pasa_a_persona", fuentes: [], respuesta: null })).toBe("Pasaría a una persona.");
        expect(textoMotivo({ motivo: "modo_nunca" })).toBe("Porque en la pregunta 1 está en «Nunca».");
        expect(textoMotivo({ motivo: "sin_falla" })).toContain("no se sabe si hay una falla");
        expect(textoMotivo({ motivo: null })).toBeNull();
    });

    it("% resuelto sin persona: nunca sobre cero conversaciones", () => {
        expect(porcentajeSinPersona({ resueltasSinPersona: 41, totalAtendidas: 100 })).toBe(41);
        expect(porcentajeSinPersona({ resueltasSinPersona: 1, totalAtendidas: 3 })).toBe(33);
        expect(porcentajeSinPersona({ resueltasSinPersona: 0, totalAtendidas: 0 })).toBeNull();
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
        expect(esDelAsistente({ automatico: "asistente" })).toBe(true);
        expect(esDelAsistente({ automatico: "bot", autorTipo: "sistema" })).toBe(false);
        expect(esDelAsistente({ autorTipo: "persona", automatico: null })).toBe(false);
        expect(esDelAsistente({})).toBe(false);
    });

    it("el asistente atiende solo mientras su estado lo diga y la conversación siga abierta", () => {
        expect(asistenteAtiende({ estado: "abierta", asistente: { estado: "atendiendo" } })).toBe(true);
        expect(asistenteAtiende({ estado: "resuelta", asistente: { estado: "atendiendo" } })).toBe(false);
        expect(asistenteAtiende({ estado: "abierta", asistente: { estado: "detenido" } })).toBe(false);
        expect(asistenteAtiende({ estado: "abierta", asistente: { estado: "espera_persona" } })).toBe(false);
        // Entrega 3a: el campo viene ausente.
        expect(asistenteAtiende({ estado: "abierta" })).toBe(false);
    });
});
