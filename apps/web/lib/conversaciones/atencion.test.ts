import { describe, expect, it } from "vitest";
import { avanceEspera, estaDisponible, minutosParaBot, personasDeCola, supervisaCola, textoCarga, textoCuentaRegresiva, textoDisponibles, textoEventoBot } from "./atencion";
import type { AgenteDisponible } from "./contrato-11";

const SOPORTE = { id: 2, nombre: "Soporte" };
const agente = (id: number, nombre: string, disponible: boolean, extra: Partial<AgenteDisponible> = {}): AgenteDisponible => ({
    id,
    nombre,
    supervisa: false,
    colas: [SOPORTE],
    disponibilidad: { estado: disponible ? "disponible" : "no_disponible", origen: disponible ? "manual" : "horario", desde: null },
    abiertas: 0,
    colasSupervisadas: [],
    ...extra,
});

describe("«¿Alguien la atiende?»", () => {
    const ahora = Date.parse("2026-11-06T16:34:00Z");

    it("cuenta los minutos que faltan para el bot, hacia arriba y nunca negativos", () => {
        expect(minutosParaBot("2026-11-06T16:46:00Z", ahora)).toBe(12);
        expect(minutosParaBot("2026-11-06T16:34:20Z", ahora)).toBe(1);
        expect(minutosParaBot("2026-11-06T16:30:00Z", ahora)).toBe(0);
        expect(minutosParaBot(null, ahora)).toBeNull();
        expect(minutosParaBot("no es fecha", ahora)).toBeNull();
    });

    it("dice la cuenta regresiva larga y corta", () => {
        expect(textoCuentaRegresiva(12)).toBe("El bot contestará en 12 min");
        expect(textoCuentaRegresiva(12, true)).toBe("Bot en 12 min");
        expect(textoCuentaRegresiva(0)).toBe("El bot está por contestar");
    });

    it("calcula el avance de la espera entre 0 y 1", () => {
        expect(avanceEspera("2026-11-06T16:16:00Z", "2026-11-06T16:46:00Z", ahora)).toBeCloseTo(0.6);
        expect(avanceEspera("2026-11-06T16:16:00Z", "2026-11-06T16:20:00Z", ahora)).toBe(1);
        expect(avanceEspera(null, "2026-11-06T16:20:00Z", ahora)).toBe(0);
    });
});

describe("disponibles por cola", () => {
    const lista = [
        agente(1, "Rodrigo Sánchez", true),
        agente(2, "Mario Ixcot", false),
        agente(3, "Laura Méndez", true, { supervisa: true, colas: [] }),
        agente(4, "Karla Tun", true, { colas: [{ id: 3, nombre: "Caja" }] }),
        agente(5, "Vera Gómez", true, { colas: [], colasSupervisadas: [SOPORTE] }),
    ];

    it("incluye a los miembros, a los supervisores de la cola y a los globales, con los disponibles primero", () => {
        expect(personasDeCola(lista, 2).map((a) => a.nombre)).toEqual(["Laura Méndez", "Rodrigo Sánchez", "Vera Gómez", "Mario Ixcot"]);
        expect(personasDeCola(lista, 2).filter(estaDisponible)).toHaveLength(3);
        expect(personasDeCola(lista, 3).map((a) => a.nombre)).toEqual(["Karla Tun", "Laura Méndez"]);
    });

    it("dice cuántos hay, o «Nadie disponible»", () => {
        expect(textoDisponibles(0, "Cobros")).toBe("Nadie disponible en Cobros");
        expect(textoDisponibles(1, "Ventas")).toBe("1 disponible en Ventas");
        expect(textoDisponibles(2, "Soporte")).toBe("2 disponibles en Soporte");
    });

    it("dice la carga de cada uno", () => {
        expect(textoCarga({ abiertas: 3 }, false)).toBe("3 conversaciones abiertas");
        expect(textoCarga({ abiertas: 1 }, true)).toBe("Supervisa · 1 abierta");
        expect(textoCarga({}, false)).toBeNull();
    });

    it("reconoce al supervisor global y al de la cola", () => {
        expect(supervisaCola(lista[2], 9)).toBe(true);
        expect(supervisaCola(lista[4], 2)).toBe(true);
        expect(supervisaCola(lista[4], 3)).toBe(false);
        expect(supervisaCola(lista[0], 2)).toBe(false);
    });
});

describe("eventos del bot", () => {
    it("usa los minutos configurados cuando había gente y otro texto si no había nadie", () => {
        expect(textoEventoBot({ tipo: "bot_atendio", detalle: "sin_confirmacion", minutos: 45 })).toBe("Nadie la tomó en 45 min: contestó el bot");
        expect(textoEventoBot({ tipo: "bot_atendio", detalle: "sin_disponibles", minutos: 3 })).toBe("Sin agentes disponibles: contestó el bot");
        expect(textoEventoBot({ tipo: "aviso_sin_atender", disponibles: 3, colaNombre: "Soporte" })).toBe("Se avisó a 3 agentes de Soporte");
        expect(textoEventoBot({ tipo: "pasada", actorNombre: "Ana", asignadoNombre: "Mario Ixcot", detalle: "no_disponible" })).toBe("Ana la pasó a Mario Ixcot (no disponible)");
        expect(textoEventoBot({ tipo: "menu_elegido", detalle: "Pagos", colaNombre: "Caja" })).toBe("Eligió «Pagos» en el menú: pasó a Caja");
        expect(textoEventoBot({ tipo: "tomada" })).toBeNull();
    });
});
