import type { ConversacionVista } from "@iris/sync/chat";
import { describe, expect, it } from "vitest";
import {
    aplicarMencion,
    candidatosMencion,
    consultaMencion,
    contadoresFiltros,
    filtrarConversaciones,
    hayPunto,
    insigniaDe,
    mencionesVigentes,
    requiereAccionLocal,
    segmentosChat,
    textoMotivo,
    tituloChat,
} from "./presentacion";
import type { MiembroChat } from "./api";

function conv(datos: Partial<ConversacionVista> & Pick<ConversacionVista, "id" | "tipo" | "titulo"> & { menciones?: number; orden_tipo?: string }): ConversacionVista {
    return {
        empresa_id: 1,
        cuadrilla_id: null,
        orden_id: null,
        orden_estado: null,
        no_leidos: 0,
        leido_hasta_mensaje_id: null,
        ultima_actividad: "2026-11-06T16:00:00Z",
        creada_en: "2026-11-06T16:00:00Z",
        pendientes: 0,
        fallidos: 0,
        ...datos,
    } as ConversacionVista;
}

const MIEMBROS: MiembroChat[] = [
    { usuario_id: 5, nombre: "Mario Ixcot", motivo: "tecnico" },
    { usuario_id: 1, nombre: "Ana López", motivo: "despacho" },
    { usuario_id: 3, nombre: "Laura Méndez", motivo: "supervision" },
];

describe("insignias", () => {
    const directa = conv({ id: 1, tipo: "directa", titulo: "Mario", no_leidos: 2 });
    const cuadrilla = conv({ id: 2, tipo: "cuadrilla", titulo: "Cuadrilla Norte", no_leidos: 4 });
    const orden = conv({ id: 3, tipo: "orden", titulo: "OT-000214", no_leidos: 3, menciones: 1 });
    const ordenSinMencion = conv({ id: 4, tipo: "orden", titulo: "OT-000209", no_leidos: 2 });

    it("número en directas y en menciones; punto en lo demás", () => {
        expect(insigniaDe(directa)).toEqual({ numero: 2, punto: false });
        expect(insigniaDe(cuadrilla)).toEqual({ numero: 0, punto: true });
        expect(insigniaDe(orden)).toEqual({ numero: 1, punto: false });
        expect(insigniaDe(ordenSinMencion)).toEqual({ numero: 0, punto: true });
    });

    it("las menciones nunca pasan de los no leídos (ya leídas no cuentan)", () => {
        expect(insigniaDe(conv({ id: 5, tipo: "orden", titulo: "x", no_leidos: 0, menciones: 2 }))).toEqual({ numero: 0, punto: false });
    });

    it("requiere acción y contadores por pestaña (locales o del resumen)", () => {
        const lista = [directa, cuadrilla, orden, ordenSinMencion];
        expect(requiereAccionLocal(lista)).toBe(3);
        expect(hayPunto(lista)).toBe(true);
        expect(contadoresFiltros(lista, null)).toEqual({ todas: 3, directas: 2, cuadrillas: 0, ordenes: 1 });
        const z = { no_leidos: 0, menciones: 0, conversaciones_no_leidas: 0, requiere_accion: 0 };
        const resumen = { ...z, requiere_accion: 7, por_tipo: { directa: { ...z, requiere_accion: 4 }, cuadrilla: { ...z, requiere_accion: 1 }, orden: { ...z, requiere_accion: 2 } } };
        expect(contadoresFiltros(lista, resumen)).toEqual({ todas: 7, directas: 4, cuadrillas: 1, ordenes: 2 });
    });
});

describe("lista", () => {
    it("título de la orden con su tipo", () => {
        expect(tituloChat(conv({ id: 1, tipo: "orden", titulo: "OT-000214", orden_tipo: "Instalación" }))).toBe("OT-000214 · Instalación");
        expect(tituloChat(conv({ id: 2, tipo: "directa", titulo: "x", otro_usuario: { id: 5, nombre: "Mario Ixcot" } }))).toBe("Mario Ixcot");
    });

    it("filtra por tipo y busca sin acentos", () => {
        const lista = [conv({ id: 1, tipo: "directa", titulo: "Laura Méndez" }), conv({ id: 2, tipo: "cuadrilla", titulo: "Cuadrilla Sur" }), conv({ id: 3, tipo: "orden", titulo: "OT-000214" })];
        expect(filtrarConversaciones(lista, "cuadrillas", "").map((c) => c.id)).toEqual([2]);
        expect(filtrarConversaciones(lista, "todas", "mendez").map((c) => c.id)).toEqual([1]);
        expect(filtrarConversaciones(lista, "ordenes", "214").map((c) => c.id)).toEqual([3]);
        expect(filtrarConversaciones(lista, "directas", "sur")).toEqual([]);
    });

    it("rol del remitente", () => {
        expect(textoMotivo("tecnico", "orden")).toBe("Técnico asignado");
        expect(textoMotivo("tecnico", "cuadrilla")).toBe("Técnico");
        expect(textoMotivo("directa", "directa")).toBeNull();
    });
});

describe("menciones", () => {
    it("detecta la @ antes del cursor, al inicio o tras un espacio", () => {
        expect(consultaMencion("hola @Mar", 9)).toEqual({ inicio: 5, fin: 9, texto: "Mar" });
        expect(consultaMencion("@", 1)).toEqual({ inicio: 0, fin: 1, texto: "" });
        expect(consultaMencion("correo@dominio", 14)).toBeNull();
        expect(consultaMencion("hola @Mario Ixcot ya", 20)).toBeNull();
    });

    it("candidatos por cualquier palabra del nombre, sin uno mismo", () => {
        expect(candidatosMencion(MIEMBROS, "ma", 1).map((m) => m.nombre)).toEqual(["Mario Ixcot"]);
        expect(candidatosMencion(MIEMBROS, "men", 1).map((m) => m.nombre)).toEqual(["Laura Méndez"]);
        expect(candidatosMencion(MIEMBROS, "", 1).map((m) => m.usuario_id)).toEqual([3, 5]);
    });

    it("inserta el nombre y conserva solo las menciones que siguen en el texto", () => {
        const c = consultaMencion("ok @ma", 6);
        expect(c).not.toBeNull();
        const r = aplicarMencion("ok @ma", c!, "Mario Ixcot");
        expect(r).toEqual({ texto: "ok @Mario Ixcot ", cursor: 16 });
        expect(mencionesVigentes(`${r.texto}revisa`, [{ id: 5, nombre: "Mario Ixcot" }, { id: 3, nombre: "Laura Méndez" }])).toEqual([5]);
    });

    it("resalta @Nombre de miembros (completo o primer nombre único), nunca HTML", () => {
        const s = segmentosChat("@Mario la caja, ve https://iris.test/x y @Laura Méndez <b>", MIEMBROS, 3);
        expect(s).toEqual([
            { tipo: "mencion", texto: "@Mario", propia: false },
            { tipo: "texto", texto: " la caja, ve " },
            { tipo: "enlace", texto: "https://iris.test/x", url: "https://iris.test/x" },
            { tipo: "texto", texto: " y " },
            { tipo: "mencion", texto: "@Laura Méndez", propia: true },
            { tipo: "texto", texto: " <b>" },
        ]);
        expect(segmentosChat("@Marion no es nadie", MIEMBROS, 1)).toEqual([{ tipo: "texto", texto: "@Marion no es nadie" }]);
    });
});
