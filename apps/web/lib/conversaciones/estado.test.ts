import { describe, expect, it } from "vitest";
import { ANDREA, CONVERSACIONES, conversacion, mensaje } from "../../e2e/datos-conversaciones";
import {
    aplicarConversacion,
    coincide,
    cuantosFiltros,
    FILTROS_INICIALES,
    filtrosDeUrl,
    fusionarMensajes,
    pendientesSinConfirmar,
    unirConversaciones,
    urlDeFiltros,
} from "./estado";

const YO = ANDREA.id;

describe("filtros de la bandeja en la URL", () => {
    it("valida lo que llega y solo escribe lo que no es por defecto", () => {
        const f = filtrosDeUrl(new URLSearchParams("vista=mias&estado=raro&canal=3&cola=-1&etiqueta=x&sla=vencido&q=TK-004812"));
        expect(f).toEqual({ ...FILTROS_INICIALES, vista: "mias", canalId: 3, sla: "vencido", q: "TK-004812" });
        expect(urlDeFiltros(f, 41)).toBe("?vista=mias&canal=3&sla=vencido&q=TK-004812&c=41");
        expect(urlDeFiltros(FILTROS_INICIALES, null)).toBe("");
    });

    it("el contador del botón cuenta número, estado, SLA, etiqueta y cola (no la vista ni la búsqueda)", () => {
        expect(cuantosFiltros({ ...FILTROS_INICIALES, vista: "mias", q: "x" })).toBe(0);
        expect(cuantosFiltros({ ...FILTROS_INICIALES, canalId: 3, estado: "resuelta", sla: "vencido" })).toBe(3);
    });
});

describe("aplicar conversaciones del feed", () => {
    const [carlos, maria] = CONVERSACIONES as [ReturnType<typeof conversacion>, ReturnType<typeof conversacion>];

    it("coincide según vista y filtros", () => {
        expect(coincide(carlos, { ...FILTROS_INICIALES, vista: "mias" }, YO)).toBe(true);
        expect(coincide(maria, { ...FILTROS_INICIALES, vista: "mias" }, YO)).toBe(false);
        expect(coincide(maria, { ...FILTROS_INICIALES, vista: "sin_asignar" }, YO)).toBe(true);
        expect(coincide({ ...carlos, estado: "resuelta" }, FILTROS_INICIALES, YO)).toBe(false);
        expect(coincide(carlos, { ...FILTROS_INICIALES, etiquetaId: 3 }, YO)).toBe(true);
    });

    it("una versión vieja no pisa la nueva; la nueva sube al principio", () => {
        const lista = unirConversaciones([maria, carlos]);
        expect(lista.map((c) => c.id)).toEqual([41, 42]);
        const vieja = aplicarConversacion(lista, { ...carlos, version: 1, titulo: "Viejo" }, FILTROS_INICIALES, YO);
        expect(vieja.find((c) => c.id === 41)?.titulo).toBe("Carlos Ruiz");
        const nueva = aplicarConversacion(lista, { ...maria, version: 9, ultimaActividad: new Date().toISOString() }, FILTROS_INICIALES, YO);
        expect(nueva[0]?.id).toBe(42);
    });

    it("sale de «Mías» si la asignan a otro; con búsqueda no agrega conversaciones nuevas", () => {
        const mias = { ...FILTROS_INICIALES, vista: "mias" as const };
        expect(aplicarConversacion([carlos], { ...carlos, version: 8, asignado: { id: 9, nombre: "Rosa" } }, mias, YO)).toEqual([]);
        const otra = conversacion({ id: 99, titulo: "Nueva" });
        expect(aplicarConversacion([carlos], otra, { ...FILTROS_INICIALES, q: "carlos" }, YO).map((c) => c.id)).toEqual([41]);
        expect(aplicarConversacion([carlos], otra, FILTROS_INICIALES, YO).map((c) => c.id)).toContain(99);
    });
});

describe("mensajes", () => {
    it("fusiona por id, ordena por seq y nunca retrocede el estado de entrega", () => {
        const a = mensaje(1, { direccion: "saliente", texto: "hola", estadoEntrega: "entregado" });
        const b = mensaje(1, { direccion: "entrante", texto: "x" });
        const viejo = { ...a, estadoEntrega: "enviado" as const };
        const r = fusionarMensajes([b, a], [viejo]);
        expect(r.map((m) => m.id)).toEqual([a.id, b.id]);
        expect(r[0]?.estadoEntrega).toBe("entregado");
        expect(fusionarMensajes([a], [{ ...a, estadoEntrega: "fallido", motivoFalla: "agotado" }])[0]?.estadoEntrega).toBe("fallido");
    });

    it("los pendientes confirmados por el servidor desaparecen", () => {
        const m = mensaje(1, { direccion: "saliente", texto: "hola" });
        const pendientes = [
            { id: m.id, modo: "responder" as const, texto: "hola", creadoEn: "", error: null },
            { id: "otro", modo: "nota" as const, texto: "n", creadoEn: "", error: null },
        ];
        expect(pendientesSinConfirmar(pendientes, [m]).map((p) => p.id)).toEqual(["otro"]);
    });
});
