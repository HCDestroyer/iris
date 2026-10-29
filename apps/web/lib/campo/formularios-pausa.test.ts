import { itemDePrueba, ordenDePrueba } from "@iris/sync/pruebas";
import type { OrdenVista } from "@iris/sync";
import { describe, expect, it } from "vitest";
import { esquemaPausar } from "./esquemas";
import { duracionCorta } from "./fechas";
import { accionesDe, faltantesCierre } from "./reglas";
import { describirCampo, detallesProblema, faltantesPorFormulario, textoProblema } from "./textos";

const vista = (parcial: Partial<OrdenVista> = {}): OrdenVista => ({
    ...ordenDePrueba(1),
    reasignada: false,
    pendientes: 0,
    rechazos: 0,
    bloqueada: false,
    esperandoAdjuntos: false,
    subidasPendientes: 0,
    ...parcial,
});

describe("pausa en la app de campo", () => {
    it("pausada: no se ofrece cerrar, fallar, llegar ni salir; sí el trabajo (evidencia) y reanudar", () => {
        expect(accionesDe(vista({ estado: "pausada", tipo: "instalacion" }))).toMatchObject({
            cerrar: false,
            fallar: false,
            llegar: false,
            iniciarCamino: false,
            trabajo: true,
            instalar: true,
            notas: true,
            pausar: false,
            reanudar: true,
            agregarFormulario: true,
        });
        expect(accionesDe(vista({ estado: "en_camino" }))).toMatchObject({ pausar: true, reanudar: false });
        expect(accionesDe(vista({ estado: "en_sitio" }))).toMatchObject({ pausar: true, cerrar: true });
        expect(accionesDe(vista({ estado: "programada" }))).toMatchObject({ pausar: false, agregarFormulario: false });
        expect(accionesDe(vista({ estado: "en_camino" }))).toMatchObject({ agregarFormulario: true });
        expect(accionesDe(vista({ estado: "cerrada" }))).toMatchObject({ pausar: false, agregarFormulario: false });
    });

    it("motivo de catálogo y nota obligatoria con «Otro»", () => {
        expect(esquemaPausar.safeParse({ motivo: "clima", nota: "" }).success).toBe(true);
        const otro = esquemaPausar.safeParse({ motivo: "otro", nota: "  " });
        expect(otro.success).toBe(false);
        expect(otro.error?.issues[0]).toMatchObject({ path: ["nota"], message: "Con «Otro motivo» explica qué pasa" });
        expect(esquemaPausar.safeParse({ motivo: "vacaciones", nota: "" }).success).toBe(false);
    });

    it("tiempo pausado legible", () => {
        expect(duracionCorta(30)).toBe("menos de 1 min");
        expect(duracionCorta(15 * 60)).toBe("15 min");
        expect(duracionCorta(65 * 60 + 59)).toBe("1 h 05 min");
    });
});

describe("cierre con varios formularios", () => {
    const problema = {
        type: "urn:iris:problema:cierre_incompleto",
        title: "Cierre incompleto",
        status: 422,
        errors: [
            { field: "formularios.f-1.checklist.potencia", message: "Falta: Potencia óptica (Instalación FTTH)" },
            { field: "formularios.f-2.checklist.router", message: "Falta: Modelo de router (Inventario)" },
            { field: "firma", message: "Falta la firma" },
        ],
    };

    it("muestra el message de la API por formulario y sabe qué ítem falta en cuál", () => {
        expect(textoProblema(problema)).toBe("Falta completar el trabajo antes de cerrar.");
        expect(detallesProblema(problema)).toEqual(["Falta: Potencia óptica (Instalación FTTH)", "Falta: Modelo de router (Inventario)", "Falta la firma del cliente."]);
        expect(faltantesPorFormulario(problema)).toEqual([
            { formularioId: "f-1", codigo: "potencia", mensaje: "Falta: Potencia óptica (Instalación FTTH)" },
            { formularioId: "f-2", codigo: "router", mensaje: "Falta: Modelo de router (Inventario)" },
        ]);
        expect(describirCampo("formularios.x.checklist.y", "")).toBe("Falta un ítem de un formulario (formularios.x.checklist.y).");
    });

    it("lo que ve el teléfono antes de cerrar también dice de qué formulario", () => {
        const orden = vista({
            estado: "en_sitio",
            tipo: "visita",
            formularios: [
                {
                    id: "f-2",
                    plantillaId: 8,
                    codigo: "inventario",
                    nombre: "Inventario",
                    versionId: 1,
                    numeroVersion: 1,
                    requiereFirma: false,
                    origen: "tecnico",
                    agregadoPor: 5,
                    agregadoEn: "2026-10-28T12:00:00Z",
                    conRespuestas: false,
                },
            ],
            checklist: [itemDePrueba(1, { obligatorio: true, etiqueta: "Router", formularioId: "f-2" }), itemDePrueba(2, { obligatorio: true, etiqueta: "Suelto" })],
        });
        expect(faltantesCierre(orden)).toEqual(["Falta «Router» (Inventario).", "Falta «Suelto» del checklist."]);
    });

    it("textos de los rechazos nuevos", () => {
        for (const codigo of ["plantilla_inactiva", "formulario_duplicado", "formulario_del_tipo", "formulario_quitado", "orden_pausada", "orden_ya_pausada", "orden_no_pausada", "version_anterior", "pausa_ajustada"]) {
            expect(textoProblema({ type: `urn:iris:problema:${codigo}`, title: "x", status: 422 })).not.toBe("x");
        }
    });
});
