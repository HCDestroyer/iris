import { describe, expect, it } from "vitest";
import { accionesPantallaOrden, contadorNota, cuandoNota, encabezadoOrden, metaNota, motivoSinFormulario, notasOrdenadas, puedeCambiarNota } from "./campo";
import type { NotaOrden, PausaOrden } from "./contrato";
import type { OrdenVista } from "./motor";
import { ordenDePrueba } from "./pruebas/fabricas";

function vista(parcial: Partial<OrdenVista> = {}): OrdenVista {
    return { ...ordenDePrueba(1), reasignada: false, pendientes: 0, rechazos: 0, bloqueada: false, esperandoAdjuntos: false, subidasPendientes: 0, ...parcial };
}

const NOTA: NotaOrden = {
    id: "0192f0a0-0000-7000-8000-000000000001",
    texto: "Portón negro",
    autorId: 9,
    autorNombre: "Despacho",
    canal: "panel",
    ocurridoEn: "2026-10-27T22:20:00Z",
    version: 1,
    editadaEn: null,
};

describe("pantalla de la orden", () => {
    it("encabezado: estado, tipo y ventana acordada (o la hora de la cita)", () => {
        const conVentana = vista({
            estado: "en_sitio",
            tipo: "instalacion",
            cita: { tecnicoId: 5, inicio: "2026-10-28T14:00:00Z", fin: "2026-10-28T16:00:00Z", ventanaInicio: "2026-10-28T14:30:00Z", ventanaFin: "2026-10-28T16:00:00Z" },
        });
        expect(encabezadoOrden(conVentana)).toEqual({ estado: "En sitio", tono: "info", detalle: ["Instalación", "08:30–10:00"], texto: "En sitio · Instalación · 08:30–10:00" });
        const sinCita = vista({ estado: "pausada", tipo: "visita", programadaInicio: "2026-10-28T15:00:00Z", programadaFin: null });
        expect(encabezadoOrden(sinCita).texto).toBe("Pausada · Visita técnica · 09:00");
        expect(encabezadoOrden(sinCita).tono).toBe("warning");
    });

    it("programada: «Empezar trabajo» y [En camino | No se pudo]", () => {
        expect(accionesPantallaOrden(vista({ estado: "programada" }), true)).toEqual({
            principal: { texto: "Empezar trabajo", empezado: false },
            grupo: ["en_camino", "fallar"],
            pausa: null,
            reanudar: false,
        });
    });

    it("en camino: «Empezar trabajo» y [Pausar | No se pudo]; en sitio, «Continuar trabajo»", () => {
        expect(accionesPantallaOrden(vista({ estado: "en_camino" }), true)).toMatchObject({ principal: { texto: "Empezar trabajo" }, grupo: ["pausar", "fallar"] });
        expect(accionesPantallaOrden(vista({ estado: "en_sitio", enSitioEn: "2026-10-28T15:00:00Z" }), true)).toMatchObject({
            principal: { texto: "Continuar trabajo", empezado: true },
            grupo: ["pausar", "fallar"],
        });
    });

    it("pausada: solo el aviso «En pausa desde HH:MM · motivo» y «Reanudar» (si tiene el permiso)", () => {
        const pausa: PausaOrden = { id: 1, motivo: "esperando_material", nota: null, desdeEstado: "en_sitio", inicio: "2026-10-28T18:10:00Z", fin: null, duracionSeg: null, cierre: null, pausadaPor: 5, cerradaPor: null };
        const pausada = vista({ estado: "pausada", pausadaDesde: "en_sitio", pausadaEn: "2026-10-28T18:10:00Z", pausas: [pausa] });
        expect(accionesPantallaOrden(pausada, true)).toEqual({
            principal: null,
            grupo: [],
            pausa: { texto: "En pausa desde 12:10 · Esperando material", desde: "12:10", motivo: "Esperando material" },
            reanudar: true,
        });
        expect(accionesPantallaOrden(pausada, false).reanudar).toBe(false);
        // Con «Otro motivo», la nota es el motivo.
        const otro = vista({ ...pausada, pausas: [{ ...pausa, motivo: "otro", nota: "Almuerzo" }] });
        expect(accionesPantallaOrden(otro, true).pausa?.texto).toBe("En pausa desde 12:10 · Almuerzo");
    });

    it("cerrada, cancelada o reasignada: sin acción principal ni grupo", () => {
        for (const o of [vista({ estado: "cerrada" }), vista({ estado: "cancelada" }), vista({ estado: "en_sitio", reasignada: true })]) {
            expect(accionesPantallaOrden(o, true)).toMatchObject({ principal: null, grupo: [] });
        }
    });
});

describe("«Agregar formulario» en el menú «⋯»", () => {
    const P = ["ordenes.registrar"];
    it("con el trabajo empezado se puede; si no, deshabilitado con el motivo", () => {
        expect(motivoSinFormulario(vista({ estado: "en_camino" }), P)).toBeNull();
        expect(motivoSinFormulario(vista({ estado: "en_sitio" }), P)).toBeNull();
        expect(motivoSinFormulario(vista({ estado: "programada" }), P)).toBe("Disponible con el trabajo empezado (en camino o en sitio).");
        expect(motivoSinFormulario(vista({ estado: "pausada" }), P)).toBe("La orden está pausada: primero reanúdala.");
        expect(motivoSinFormulario(vista({ estado: "cerrada" }), P)).toBe("Esta orden ya no admite formularios.");
        expect(motivoSinFormulario(vista({ estado: "en_sitio", reasignada: true }), P)).toBe("Esta orden ya no admite formularios.");
        expect(motivoSinFormulario(vista({ estado: "en_sitio" }), [])).toBe("Tu usuario no puede registrar trabajo en las órdenes.");
        expect(motivoSinFormulario(vista({ estado: "en_sitio" }), undefined)).not.toBeNull();
    });
});

describe("notas", () => {
    const ahora = Date.parse("2026-10-28T17:00:00Z"); // 11:00 en Guatemala

    it("cuándo: hoy, ayer o la fecha corta, en Guatemala", () => {
        expect(cuandoNota("2026-10-28T16:42:00Z", ahora)).toBe("hoy 10:42");
        expect(cuandoNota("2026-10-27T22:20:00Z", ahora)).toBe("ayer 16:20");
        expect(cuandoNota("2026-10-20T15:05:00Z", ahora)).toBe("20 oct 09:05");
        // 00:30 UTC del 28 es el 27 en Guatemala.
        expect(cuandoNota("2026-10-28T00:30:00Z", ahora)).toBe("ayer 18:30");
    });

    it("meta: «Tú» para las propias (también las del teléfono) y el canal al abrirla", () => {
        expect(metaNota(NOTA, 5, ahora)).toMatchObject({ autor: "Despacho", corta: "Despacho · ayer 16:20", larga: "Despacho · ayer 16:20 · desde el panel" });
        const propia = { ...NOTA, autorId: null, autorNombre: "", canal: "campo", ocurridoEn: "2026-10-28T16:42:00Z", editadaEn: "2026-10-28T16:50:00Z" };
        expect(metaNota(propia, 5, ahora).larga).toBe("Tú · hoy 10:42 · desde el teléfono · editada");
        expect(metaNota({ ...NOTA, autorId: 5 }, 5, ahora).autor).toBe("Tú");
    });

    it("editar y eliminar: el autor, o despacho y supervisión con ordenes.asignar", () => {
        expect(puedeCambiarNota(NOTA, 5, ["ordenes.registrar"])).toBe(false);
        expect(puedeCambiarNota(NOTA, 9, ["ordenes.registrar"])).toBe(true);
        expect(puedeCambiarNota({ autorId: null }, 5, [])).toBe(true);
        expect(puedeCambiarNota(NOTA, 5, ["ordenes.registrar", "ordenes.asignar"])).toBe(true);
    });

    it("contador y orden de llegada", () => {
        expect(contadorNota("Cliente pide dejar el router en la sala, junto a la TV. Cable de 15 m.")).toBe("70 / 4000");
        const b = { ...NOTA, id: "b", ocurridoEn: "2026-10-28T16:42:00Z" };
        expect(notasOrdenadas({ notas: [b, NOTA] }).map((n) => n.id)).toEqual([NOTA.id, "b"]);
    });
});
