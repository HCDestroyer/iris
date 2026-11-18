import { describe, expect, it } from "vitest";
import { textoEvento } from "../conversaciones/presentacion";
import { accionDeMenu, franjaAtencion, pideConfirmarReanudar, puntoAtencion, textoEventoAsistente, textoMotivoPausa, type AtencionAsistente } from "./atencion";

const AHORA = Date.parse("2026-11-18T16:00:00Z");
const con = (a: Partial<AtencionAsistente>, extra: object = {}) => ({ atencionAsistente: { atiende: false, motivo: null, reanudable: false, ...a } as AtencionAsistente, ...extra });

describe("quién atiende la conversación (franja)", () => {
    it("sin el campo no se pinta nada", () => {
        expect(franjaAtencion({}, AHORA)).toBeNull();
        expect(accionDeMenu({})).toBeNull();
        expect(puntoAtencion(undefined)).toBeNull();
    });

    it("el asistente atiende: en teal, con «Pausar» y «Tomar»", () => {
        expect(franjaAtencion(con({ atiende: true }), AHORA)).toMatchObject({ estado: "atiende", tono: "marca", texto: "El asistente atiende esta conversación", acciones: ["pausar", "tomar"] });
    });

    it("en pausa: quién, hace cuánto y por qué (si no fue manual), con «Reanudar»", () => {
        const pausa = { en: "2026-11-18T15:55:00Z", motivo: "manual" as const, por: { id: 2, nombre: "Andrea Castillo" } };
        expect(franjaAtencion(con({ motivo: "pausado", reanudable: true, pausa }), AHORA)).toMatchObject({ estado: "pausado", tono: "neutro", texto: "Asistente en pausa · Andrea, hace 5 min", acciones: ["reanudar"], textoReanudar: "Reanudar" });
        expect(franjaAtencion(con({ motivo: "pausado", reanudable: true, pausa: { ...pausa, motivo: "caso_delicado" } }), AHORA)?.texto).toBe("Asistente en pausa · Andrea, hace 5 min · caso delicado");
        // Quien la pausó ya no existe: sin nombre, sin romperse.
        expect(franjaAtencion(con({ motivo: "pausado", reanudable: false, pausa: { en: pausa.en, motivo: "respuesta_incorrecta" } }), AHORA)).toMatchObject({ texto: "Asistente en pausa · hace 5 min · respondió mal", acciones: [] });
        expect(franjaAtencion(con({ motivo: "pausado" }), AHORA)?.texto).toBe("Asistente en pausa");
    });

    it("una persona asignada: nada en la franja; «Pausar» queda en el menú", () => {
        const c = con({ motivo: "persona_asignada" });
        expect(franjaAtencion(c, AHORA)).toBeNull();
        expect(accionDeMenu(c)).toBe("pausar");
    });

    it("nadie la atiende (el asistente se apartó): en ámbar, con «Que siga el asistente» y «Tomar»", () => {
        expect(franjaAtencion(con({ motivo: "persona_presente", reanudable: true }), AHORA)).toMatchObject({
            estado: "persona_presente",
            tono: "aviso",
            texto: "Nadie la está atendiendo: el asistente se apartó cuando una persona la tomó",
            acciones: ["reanudar", "tomar"],
            textoReanudar: "Que siga el asistente",
        });
        expect(franjaAtencion(con({ motivo: "persona_presente", reanudable: false }), AHORA)?.acciones).toEqual(["tomar"]);
    });

    it("la pasó a una persona: dice por qué, «Tomar» a la vista y devolverla en el menú", () => {
        const c = con({ motivo: "espera_persona", reanudable: true }, { asistente: { motivoPaso: "cliente_lo_pidio" } });
        expect(franjaAtencion(c, AHORA)).toMatchObject({ estado: "espera_persona", tono: "aviso", texto: "El asistente la pasó a una persona porque el cliente pidió hablar con una persona", acciones: ["tomar"] });
        expect(accionDeMenu(c)).toBe("reanudar");
        expect(franjaAtencion(con({ motivo: "espera_persona" }), AHORA)?.texto).toBe("El asistente la pasó a una persona: espera en la cola");
        // Ya la tiene alguien: no se ofrece «Tomar».
        expect(franjaAtencion(con({ motivo: "espera_persona" }, { asignado: { id: 3 } }), AHORA)?.acciones).toEqual([]);
        expect(accionDeMenu(con({ motivo: "espera_persona", reanudable: false }))).toBeNull();
    });

    it("tope, bot, categoría y horario: solo informan", () => {
        for (const [motivo, texto] of [
            ["tope", "El asistente está en pausa: se llegó al tope del mes"],
            ["bot", "La atiende el bot de menú"],
            ["categoria_sugiere", "En esta categoría el asistente solo sugiere"],
            ["horario_solo_sugiere", "A esta hora el asistente solo sugiere"],
        ] as const) {
            expect(franjaAtencion(con({ motivo }), AHORA)).toMatchObject({ tono: "neutro", texto, acciones: [] });
            expect(accionDeMenu(con({ motivo }))).toBeNull();
        }
    });

    it("un motivo que el panel no conoce no rompe ni pinta nada", () => {
        expect(franjaAtencion(con({ motivo: "otro_nuevo" as never }), AHORA)).toBeNull();
    });
});

describe("antes de reanudar", () => {
    it("avisa si el cliente pidió una persona o está molesto; en lo demás, no", () => {
        expect(pideConfirmarReanudar({ asistente: { motivoPaso: "cliente_lo_pidio" } })).toBe(true);
        expect(pideConfirmarReanudar({ asistente: { motivoPaso: "enojo" } })).toBe(true);
        for (const motivoPaso of ["sin_respuesta", "max_turnos", null, undefined]) expect(pideConfirmarReanudar({ asistente: { motivoPaso } })).toBe(false);
        expect(pideConfirmarReanudar({})).toBe(false);
    });
});

describe("punto de la fila de la lista", () => {
    it("discreto en pausa, ámbar si nadie la atiende; nada en lo demás", () => {
        expect(puntoAtencion(con({ motivo: "pausado" }).atencionAsistente)).toEqual({ estado: "pausado", tono: "neutro", texto: "Asistente en pausa" });
        expect(puntoAtencion(con({ motivo: "persona_presente" }).atencionAsistente)).toEqual({ estado: "persona_presente", tono: "aviso", texto: "Nadie la atiende" });
        expect(puntoAtencion(con({ atiende: true }).atencionAsistente)).toBeNull();
        expect(puntoAtencion(con({ motivo: "persona_asignada" }).atencionAsistente)).toBeNull();
        expect(puntoAtencion(con({ motivo: "tope" }).atencionAsistente)).toBeNull();
    });
});

describe("eventos de sistema del asistente en el hilo", () => {
    it("pausado y reanudado, con quién y el motivo si no fue manual", () => {
        expect(textoEventoAsistente({ tipo: "asistente_pausado", actorNombre: "Andrea", detalle: "manual" })).toBe("Andrea pausó el asistente");
        expect(textoEventoAsistente({ tipo: "asistente_pausado", actorNombre: "Andrea", detalle: "cliente_lo_pidio" })).toBe("Andrea pausó el asistente: lo pidió el cliente");
        // `detalle` de reanudado es de dónde venía, no un motivo: no se pinta.
        expect(textoEventoAsistente({ tipo: "asistente_reanudado", actorNombre: "Andrea", detalle: "persona_presente" })).toBe("Andrea le devolvió la conversación al asistente");
        expect(textoEventoAsistente({ tipo: "asistente_pausado" })).toBe("Iris pausó el asistente");
    });

    it("el bot no pudo contestar: no dice que contestó", () => {
        expect(textoEventoAsistente({ tipo: "bot_sin_respuesta", minutos: 30 })).toBe("Nadie la tomó en 30 min y el bot no pudo contestar: el cliente no acepta mensajes o pasaron 24 h");
        expect(textoEventoAsistente({ tipo: "bot_sin_respuesta", detalle: "sin_disponibles" })).toBe("Sin agentes disponibles y el bot no pudo contestar: el cliente no acepta mensajes o pasaron 24 h");
    });

    it("salen por `textoEvento` (lo que pinta el hilo) y lo demás no cambia", () => {
        expect(textoEvento({ tipo: "asistente_pausado", actorNombre: "Andrea", detalle: "caso_delicado" } as never)).toBe("Andrea pausó el asistente: caso delicado");
        expect(textoEvento({ tipo: "tomada", actorNombre: "Andrea" } as never)).toBe("Andrea tomó la conversación");
        expect(textoEventoAsistente({ tipo: "tomada" })).toBeNull();
    });

    it("motivos de pausa: `manual` y lo desconocido no dicen nada", () => {
        expect(textoMotivoPausa("manual")).toBeNull();
        expect(textoMotivoPausa("otro")).toBeNull();
        expect(textoMotivoPausa(null)).toBeNull();
        expect(textoMotivoPausa("respuesta_incorrecta")).toBe("respondió mal");
    });
});
