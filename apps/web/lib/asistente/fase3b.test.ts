import { describe, expect, it } from "vitest";
import { ajustesDePrueba, cambianAjustes, completar, conAvanzado, entradaLimites, exigenciaDeSaldo, entradaPrincipal, esquemaLimites, PERFIL_POR_DEFECTO, saldoExigeIdentidad, valoresLimites, valoresPrincipal, type PerfilAsistente } from "./fase3";
import {
    ACCIONES_COMUNES,
    ACCIONES_DINERO,
    codigoDePerfil,
    entradaPerfiles,
    esquemaPerfil,
    faltaParaVencer,
    identidadQuedoVerificada,
    nivelVerificacion,
    opcionesDePerfil,
    perfilDeValores,
    perfilesCon,
    perfilesDe,
    perfilesSin,
    requisitoDe,
    resultadoIdentidad,
    resumenPerfil,
    textoMotivoPaso,
    textoReconexion,
    trasFallarRelectura,
    valorDePerfil,
    valoresPerfil,
} from "./fase3b";
import { textoEvento } from "../conversaciones/presentacion";
import type { AjustesAsistente } from "./tipos";

const NOCHE: PerfilAsistente = {
    codigo: "noche",
    nombre: "Noche",
    acciones: { ...PERFIL_POR_DEFECTO.acciones, dar_saldo: true, promesa_pago: true },
    exigeVerificacionFuerte: { dar_saldo: false },
    limites: { prioridadMaxima: "media", diasAdelanteMax: 1 },
};

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
    autonomoDisponible: true,
    modoAutonomo: "sin_nadie",
    esperaMin: 3,
    maxTurnos: 8,
    perfiles: [PERFIL_POR_DEFECTO, NOCHE],
    porCola: [],
    presupuestoAutonomoPct: 70,
    verificacionFuerteMin: 60,
    version: 4,
};

describe("acciones de dinero y verificación (§17.8.2)", () => {
    it("de fábrica lo de dinero y las visitas vienen apagadas", () => {
        for (const c of ACCIONES_DINERO) expect(PERFIL_POR_DEFECTO.acciones[c]).toBe(false);
        expect(PERFIL_POR_DEFECTO.acciones).toMatchObject({ responder_dudas: true, avisar_fallas: true, crear_ticket: true, agendar_visita: false, pasar_a_persona: true });
        expect([...ACCIONES_COMUNES, ...ACCIONES_DINERO]).toHaveLength(8);
    });

    it("dar el saldo pide identidad salvo que la empresa lo baje; ausente = sí", () => {
        expect(saldoExigeIdentidad(PERFIL_POR_DEFECTO)).toBe(true);
        expect(saldoExigeIdentidad({ exigeVerificacionFuerte: { dar_saldo: false } })).toBe(false);
        expect(requisitoDe("dar_saldo", false)).toContain("DPI o NIT");
        expect(requisitoDe("dar_saldo", true)).toContain("su número");
        // Promesa y reconexión, siempre; la boleta acepta el número.
        expect(requisitoDe("promesa_pago", true)).toContain("siempre");
        expect(requisitoDe("reconexion_temporal", true)).toContain("fuera de horario");
        expect(requisitoDe("recibir_boleta", false)).toContain("su número");
    });

    it("la pantalla simple manda `exigeVerificacionFuerte` del perfil general y conserva los demás", () => {
        const v = valoresPrincipal(BASE, []);
        expect(v.saldoConNumero).toBe(false);
        expect(cambianAjustes(BASE, v)).toBe(false);
        const d = { ...v, acciones: { ...v.acciones, dar_saldo: true }, saldoConNumero: true };
        // Con el saldo apagado, la casilla sola no es un cambio; con el saldo encendido, sí.
        expect(cambianAjustes(BASE, { ...v, saldoConNumero: true })).toBe(false);
        expect(cambianAjustes(BASE, d)).toBe(true);
        const e = entradaPrincipal(BASE, d);
        expect(e.perfiles?.[0]).toMatchObject({ codigo: "defecto", acciones: { dar_saldo: true, recibir_boleta: false }, exigeVerificacionFuerte: { dar_saldo: false } });
        expect(e.perfiles?.[1]).toEqual(NOCHE);
        // Apagar «Dar el saldo» vuelve a exigir identidad, aunque la casilla hubiera quedado marcada.
        expect(exigenciaDeSaldo({ acciones: { dar_saldo: false }, saldoConNumero: true })).toEqual({ dar_saldo: true });
        expect(entradaPrincipal(BASE, { ...d, acciones: { ...d.acciones, dar_saldo: false } }).perfiles?.[0]?.exigeVerificacionFuerte).toEqual({ dar_saldo: true });
        // Un perfil guardado con el saldo apagado y «solo número» no arrastra la opción al formulario.
        const apagado = { ...NOCHE, acciones: { ...NOCHE.acciones, dar_saldo: false } };
        expect(valoresPerfil(apagado).saldoConNumero).toBe(false);
        expect(perfilDeValores({ ...valoresPerfil(NOCHE), acciones: { ...valoresPerfil(NOCHE).acciones, dar_saldo: false } }, "noche").exigeVerificacionFuerte).toEqual({ dar_saldo: true });
    });

    it("avanzado: presupuesto 1–100 y vigencia 5–1440, con los de fábrica si el servidor no los manda", () => {
        const { presupuestoAutonomoPct: _p, verificacionFuerteMin: _v, ...resto } = BASE;
        const viejo: AjustesAsistente = resto;
        void _p;
        void _v;
        // Un servidor que no los manda: ni se inventan al leer ni viajan al guardar.
        expect(conAvanzado(viejo)).toBe(false);
        expect(completar(viejo)).not.toHaveProperty("presupuestoAutonomoPct");
        const sinAvanzado = entradaLimites(viejo, valoresLimites(viejo));
        expect(sinAvanzado).not.toHaveProperty("presupuestoAutonomoPct");
        expect(sinAvanzado).not.toHaveProperty("verificacionFuerteMin");
        expect(sinAvanzado).toMatchObject({ maxTurnos: 8 });
        expect(conAvanzado(BASE)).toBe(true);
        const v = valoresLimites(BASE);
        expect(esquemaLimites.safeParse(v).success).toBe(true);
        for (const malo of [{ presupuestoAutonomoPct: "0" }, { presupuestoAutonomoPct: "101" }, { verificacionFuerteMin: "4" }, { verificacionFuerteMin: "1441" }, { verificacionFuerteMin: "" }]) {
            expect(esquemaLimites.safeParse({ ...v, ...malo }).success).toBe(false);
        }
        expect(entradaLimites(BASE, { ...v, presupuestoAutonomoPct: "50", verificacionFuerteMin: "1440" })).toMatchObject({ presupuestoAutonomoPct: 50, verificacionFuerteMin: 1440 });
    });
});

describe("perfiles por horario (§17.1)", () => {
    it("el código sale del nombre: válido, único y nunca uno reservado", () => {
        expect(codigoDePerfil("Noche y feriados", [])).toBe("noche_y_feriados");
        expect(codigoDePerfil("Sábado", [])).toBe("sabado");
        expect(codigoDePerfil("Noche", ["noche"])).toBe("noche_2");
        expect(codigoDePerfil("Solo sugerir", [])).toBe("solo_sugerir_2");
        expect(codigoDePerfil("Defecto", [])).toBe("defecto_2");
        expect(codigoDePerfil("24/7", [])).toBe("perfil");
        expect(codigoDePerfil("x", [])).toBe("x_perfil");
        for (const n of ["Noche y feriados", "24/7", "x", "Un nombre de perfil larguísimo de cuarenta", "ÑANDÚ"]) expect(codigoDePerfil(n, [])).toMatch(/^[a-z][a-z0-9_]{1,29}$/);
    });

    it("un perfil nuevo empieza como el de fábrica y viaja con las nueve acciones", () => {
        const v = valoresPerfil();
        expect(v).toMatchObject({ nombre: "", saldoConNumero: false, prioridadMaxima: "alta", diasAdelanteMax: "2" });
        expect(esquemaPerfil.safeParse(v).success).toBe(false);
        expect(esquemaPerfil.safeParse({ ...v, nombre: "x".repeat(41) }).success).toBe(false);
        expect(esquemaPerfil.safeParse({ ...v, nombre: "Fin de semana", diasAdelanteMax: "15" }).success).toBe(false);
        const p = perfilDeValores({ ...v, nombre: " Fin de semana ", acciones: { ...v.acciones, dar_saldo: true }, saldoConNumero: true }, "fin_de_semana");
        expect(p).toEqual({
            codigo: "fin_de_semana",
            nombre: "Fin de semana",
            acciones: { ...PERFIL_POR_DEFECTO.acciones, dar_saldo: true },
            exigeVerificacionFuerte: { dar_saldo: false },
            limites: { prioridadMaxima: "alta", diasAdelanteMax: 2 },
        });
    });

    it("editar trae lo guardado, agregar va al final y el general no se quita", () => {
        expect(valoresPerfil(NOCHE)).toMatchObject({ nombre: "Noche", saldoConNumero: true, prioridadMaxima: "media", diasAdelanteMax: "1" });
        const nuevo = { ...NOCHE, codigo: "sabado", nombre: "Sábado" };
        expect(perfilesCon(BASE, nuevo).map((p) => p.codigo)).toEqual(["defecto", "noche", "sabado"]);
        expect(perfilesCon(BASE, { ...NOCHE, nombre: "Madrugada" })).toEqual([PERFIL_POR_DEFECTO, { ...NOCHE, nombre: "Madrugada" }]);
        expect(perfilesSin(BASE, "noche")).toEqual([PERFIL_POR_DEFECTO]);
        expect(perfilesSin(BASE, "defecto")).toHaveLength(2);
        // Un servidor que no manda perfiles: el general de fábrica.
        expect(perfilesDe({ perfiles: null })).toEqual([PERFIL_POR_DEFECTO]);
        const e = entradaPerfiles(BASE, perfilesSin(BASE, "noche"));
        expect(e).toMatchObject({ version: 4, proveedor: "ollama", perfiles: [{ codigo: "defecto" }] });
        expect(e).not.toHaveProperty("modoAutonomo");
    });

    it("resumen y opciones de la franja", () => {
        expect(resumenPerfil(PERFIL_POR_DEFECTO)).toBe("3 acciones encendidas · nada de dinero");
        expect(resumenPerfil(NOCHE)).toBe("5 acciones encendidas · 2 de dinero");
        expect(opcionesDePerfil(BASE.perfiles ?? [], null).map((o) => o.valor)).toEqual(["", "solo_sugerir", "noche"]);
        // Un código guardado que ya no existe se ve como tal (el servidor usa el general).
        expect(opcionesDePerfil(BASE.perfiles ?? [], "viejo").at(-1)).toEqual({ valor: "viejo", texto: "Un perfil que ya no existe (solo sugiere)" });
        expect(valorDePerfil(null)).toBe("");
        expect(valorDePerfil("defecto")).toBe("");
        expect(valorDePerfil("noche")).toBe("noche");
    });
});

describe("bandeja (§17.8.2)", () => {
    it("la verificación se lee solo del servidor", () => {
        expect(nivelVerificacion({ verificacion: "fuerte" })).toBe("fuerte");
        expect(nivelVerificacion({ verificacion: "numero" })).toBe("numero");
        expect(nivelVerificacion({ verificacion: null })).toBeNull();
        // Un servidor que no manda el campo: no se muestra nada (ni «verificado» ni «sin verificar»).
        expect(nivelVerificacion({})).toBeUndefined();
        expect(nivelVerificacion({ verificacion: "otra_cosa" })).toBeNull();
    });

    it("la relectura de fondo solo se detiene sin sesión o sin permiso", () => {
        expect(trasFallarRelectura({ estado: 401 })).toBe("detener");
        expect(trasFallarRelectura({ estado: 403 })).toBe("detener");
        // Red caída, 5xx o cualquier otra cosa: se conserva lo que hay y se prueba en el siguiente tic.
        for (const e of [new TypeError("Failed to fetch"), { estado: 0 }, { estado: 503 }, { estado: 404 }, null, "x"]) expect(trasFallarRelectura(e)).toBe("reintentar");
    });

    it("mensajes de sistema de 3b", () => {
        expect(textoReconexion("Asistente de IA", "aceptada")).toBe("Reconexión temporal: Argos la aceptó (la pidió Asistente de IA)");
        expect(textoEvento({ tipo: "reconexion_pedida", actorNombre: "Asistente de IA", detalle: "rechazada" })).toBe("Reconexión temporal: Argos no la aceptó (la pidió Asistente de IA)");
        // Solo llega con lo que decidió Argos; otro valor no se interpreta.
        expect(textoEvento({ tipo: "reconexion_pedida", detalle: "pendiente" })).toBe("Reconexión temporal pedida por Iris");
        // Un evento que el servidor no define se pinta neutro: el panel no afirma que se verificó nada.
        expect(textoEvento({ tipo: "verificar_identidad", actorNombre: "Asistente de IA" })).toBe("verificar identidad");
    });

    it("`identidad_verificada`: texto fijo por resultado; solo «coincide» y «supervisor» dicen que quedó verificada", () => {
        const texto = (detalle?: string) => textoEvento({ tipo: "identidad_verificada", actorNombre: "Asistente de IA", ...(detalle ? { detalle } : {}) });
        expect(texto("coincide")).toBe("Asistente de IA verificó la identidad del cliente");
        expect(textoEvento({ tipo: "identidad_verificada", actorNombre: "Ana López", detalle: "supervisor" })).toBe("Ana López confirmó la identidad del cliente");
        expect(texto("no_coincide")).toBe("Identidad sin verificar: el documento no coincide");
        expect(texto("no_disponible")).toBe("Identidad sin verificar: Argos no pudo comprobarla");
        expect(texto("demasiados_intentos")).toBe("Identidad sin verificar: demasiados intentos");
        // Un resultado desconocido, o ninguno: neutro, sin afirmar nada.
        for (const d of [undefined, "resultado_nuevo", "COINCIDE"]) {
            expect(texto(d)).toBe("Intento de verificar la identidad");
            expect(identidadQuedoVerificada(d)).toBe(false);
            expect(resultadoIdentidad(d)).toBeNull();
        }
        expect(["coincide", "supervisor", "no_coincide", "no_disponible", "demasiados_intentos"].filter(identidadQuedoVerificada)).toEqual(["coincide", "supervisor"]);
    });

    it("motivo del relevo: texto llano por motivo; desconocido o ausente, sin motivo", () => {
        for (const m of ["cliente_lo_pidio", "enojo", "sin_respuesta", "herramienta_fallo", "max_turnos", "tope", "asistente_apagado"]) expect(textoMotivoPaso(m)).toMatch(/^[a-zé]/);
        expect(textoMotivoPaso("asistente_apagado")).toBe("se apagó el modo de atender solo");
        expect(textoMotivoPaso("cliente_lo_pidio")).toBe("el cliente pidió hablar con una persona");
        for (const m of [null, undefined, "", "motivo_nuevo", "toString"]) expect(textoMotivoPaso(m)).toBeNull();
    });

    it("vencimiento de la identidad: lo que falta según `verificacionVenceEn`", () => {
        const ahora = Date.parse("2026-11-16T12:00:00Z");
        expect(faltaParaVencer({ verificacion: "fuerte", verificacionVenceEn: "2026-11-16T12:10:00Z" }, ahora)).toBe(600_000);
        // Ya vencida: cero (se baja en el acto), nunca negativo.
        expect(faltaParaVencer({ verificacion: "fuerte", verificacionVenceEn: "2026-11-16T11:00:00Z" }, ahora)).toBe(0);
        // Sin fecha, con otra cosa que una fecha o con otro nivel: no hay temporizador (queda la relectura).
        expect(faltaParaVencer({ verificacion: "fuerte" }, ahora)).toBeNull();
        expect(faltaParaVencer({ verificacion: "fuerte", verificacionVenceEn: null }, ahora)).toBeNull();
        expect(faltaParaVencer({ verificacion: "fuerte", verificacionVenceEn: "mañana" }, ahora)).toBeNull();
        expect(faltaParaVencer({ verificacion: "numero", verificacionVenceEn: "2026-11-16T12:10:00Z" }, ahora)).toBeNull();
    });

    it("«Pruébalo» manda `exigeVerificacionFuerte` del formulario sin guardar", () => {
        const v = valoresPrincipal(BASE, []);
        expect(ajustesDePrueba(v, BASE, []).perfil?.exigeVerificacionFuerte).toEqual({ dar_saldo: true });
        const conNumero = { ...v, acciones: { ...v.acciones, dar_saldo: true }, saldoConNumero: true };
        expect(ajustesDePrueba(conNumero, BASE, []).perfil).toMatchObject({ acciones: { dar_saldo: true }, exigeVerificacionFuerte: { dar_saldo: false } });
    });
});
