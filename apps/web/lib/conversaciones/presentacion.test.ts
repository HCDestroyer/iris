import { describe, expect, it } from "vitest";
import { AHORA_CONV } from "../../e2e/datos-conversaciones";
import {
    aplicarRespuesta,
    formatearNumero,
    horaLista,
    iniciales,
    separadorDia,
    textoEntrega,
    textoEvento,
    textoUltimo,
    textoVentana,
    tonoDeCanal,
    usoPrincipal,
} from "./presentacion";

describe("presentación de la bandeja", () => {
    it("color del número por su uso (avisos cede a otro uso)", () => {
        expect(usoPrincipal(["avisos", "soporte"])).toBe("soporte");
        expect(tonoDeCanal({ usos: ["cobros"] })).toBe("alerta");
        expect(tonoDeCanal({ usos: ["avisos"] })).toBe("info");
        expect(tonoDeCanal({ usos: [] })).toBe("neutro");
    });

    it("número de Guatemala en grupos e iniciales", () => {
        expect(formatearNumero("+50255120041")).toBe("+502 5512 0041");
        expect(formatearNumero("+14155550100")).toBe("+14155550100");
        expect(iniciales("Carlos Ruiz")).toBe("CR");
        expect(iniciales("Ferretería El Tornillo, S.A.")).toBe("FS");
        expect(iniciales("+50255440018")).toBe("+5");
    });

    it("hora en la lista: hoy, ayer o fecha corta (Guatemala)", () => {
        expect(horaLista("2026-10-28T16:41:00Z", AHORA_CONV)).toBe("10:41");
        expect(horaLista("2026-10-27T20:00:00Z", AHORA_CONV)).toBe("ayer");
        expect(horaLista("2026-10-02T20:00:00Z", AHORA_CONV)).toMatch(/^2 oct/);
        expect(separadorDia("2026-10-28T13:00:00Z", AHORA_CONV)).toBe("Hoy");
    });

    it("ventana de 24 h: horas, minutos o cerrada", () => {
        expect(textoVentana({ abierta: true, cierraEn: new Date(AHORA_CONV + 21 * 3600_000 + 5000).toISOString() }, AHORA_CONV)).toEqual({ largo: "Quedan 21 h para responder", corto: "21 h" });
        expect(textoVentana({ abierta: true, cierraEn: new Date(AHORA_CONV + 35 * 60_000).toISOString() }, AHORA_CONV)?.largo).toBe("Quedan 35 min para responder");
        expect(textoVentana({ abierta: false, cierraEn: null }, AHORA_CONV)).toBeNull();
        expect(textoVentana({ abierta: true, cierraEn: new Date(AHORA_CONV - 1).toISOString() }, AHORA_CONV)).toBeNull();
    });

    it("eventos de sistema en una línea", () => {
        expect(textoEvento({ tipo: "ticket_creado", actorNombre: "Andrea", referencia: "TK-004812" })).toBe("Andrea creó TK-004812");
        expect(textoEvento({ tipo: "transferida", actorNombre: "Andrea", colaNombre: "Caja" })).toBe("Andrea la pasó a Caja");
        expect(textoEvento({ tipo: "respondio_aviso", plantilla: "Técnico en camino" })).toBe("Respondió al aviso «Técnico en camino»");
        expect(textoEvento({ tipo: "algo_nuevo" })).toBe("algo nuevo");
    });

    it("último mensaje de la fila", () => {
        expect(textoUltimo({ direccion: "saliente", tipo: "texto", texto: "Te enviamos un técnico", evento: null, creadoEn: "" })).toBe("Tú: Te enviamos un técnico");
        expect(textoUltimo({ direccion: "entrante", tipo: "imagen", texto: null, evento: null, creadoEn: "" })).toBe("Foto");
        expect(textoUltimo(undefined)).toBe("Sin mensajes");
    });

    it("estado de entrega y respuesta rápida con nombre", () => {
        expect(textoEntrega({ estadoEntrega: "fallido", motivoFalla: "fuera_de_ventana" })).toBe("no se envió (ventana cerrada)");
        expect(aplicarRespuesta("Hola {{nombre}}, te enviamos un técnico.", "Carlos Ruiz")).toBe("Hola Carlos, te enviamos un técnico.");
        expect(aplicarRespuesta("Hola {{nombre}}, ¿cómo estás?", "+50255440018")).toBe("Hola, ¿cómo estás?");
    });
});
