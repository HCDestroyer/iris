import { describe, expect, it } from "vitest";
import type { NotaOrden, Paquete } from "./contrato";
import { ErrorSync } from "./errores";
import { ordenDePrueba } from "./pruebas/fabricas";
import { FECHA, montar, paqueteDePrueba } from "./pruebas/montaje";

const DE_DESPACHO: NotaOrden = {
    id: "0192f0a0-0000-7000-8000-000000000001",
    texto: "Portón negro",
    autorId: 9,
    autorNombre: "Despacho",
    canal: "panel",
    ocurridoEn: "2026-10-28T14:00:00Z",
    version: 1,
    editadaEn: null,
};

function conNota(nota: NotaOrden = DE_DESPACHO): (fecha: string) => Paquete {
    return (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [ordenDePrueba(1, { notas: [nota] }), ordenDePrueba(2)] });
}

async function montarConNota(nota?: NotaOrden) {
    const m = await montar({}, false);
    m.transporte.paquete = conNota(nota);
    await m.motor.cargarPaquete(FECHA);
    m.transporte.llamadas.length = 0;
    return m;
}

describe("notas editables (nota.editar y nota.eliminar)", () => {
    it("editar se ve al instante con la versión siguiente y viaja con la versión que vio el teléfono", async () => {
        const { motor, transporte } = await montarConNota();
        await motor.registrar("nota.editar", 1, { notaId: DE_DESPACHO.id, versionNota: 1, texto: "Portón negro, timbre izquierdo" });
        const nota = motor.obtenerOrden(1)?.notas?.[0];
        expect(nota).toMatchObject({ texto: "Portón negro, timbre izquierdo", version: 2 });
        expect(nota?.editadaEn).not.toBeNull();
        // Otra edición encima (sin señal) va con la versión 2.
        await motor.registrar("nota.editar", 1, { notaId: DE_DESPACHO.id, versionNota: 2, texto: "Timbre izquierdo" });
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => [o.tipo, (o.datos as { versionNota: number }).versionNota])).toEqual([
            ["nota.editar", 1],
            ["nota.editar", 2],
        ]);
        expect(motor.obtenerOrden(1)?.notas?.[0]).toMatchObject({ texto: "Timbre izquierdo", version: 3 });
    });

    it("eliminar la quita de la orden; una nota agregada sin señal se edita y elimina por el id de su operación", async () => {
        const { motor, transporte } = await montarConNota();
        await motor.registrar("nota.eliminar", 1, { notaId: DE_DESPACHO.id, versionNota: 1 });
        expect(motor.obtenerOrden(1)?.notas).toEqual([]);
        const { operacion } = await motor.registrar("nota.agregar", 1, { texto: "Nadie abre" });
        await motor.registrar("nota.editar", 1, { notaId: operacion.id, versionNota: 1, texto: "Nadie abre; dejé aviso" });
        expect(motor.obtenerOrden(1)?.notas?.map((n) => [n.id, n.texto, n.version])).toEqual([[operacion.id, "Nadie abre; dejé aviso", 2]]);
        await motor.registrar("nota.eliminar", 1, { notaId: operacion.id, versionNota: 2 });
        expect(motor.obtenerOrden(1)?.notas).toEqual([]);
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => o.tipo)).toEqual(["nota.eliminar", "nota.agregar", "nota.editar", "nota.eliminar"]);
        expect(motor.obtenerOrden(1)?.notas).toEqual([]);
    });

    it("no encola una nota que no está en la orden ni una edición sin cambios", async () => {
        const { motor } = await montarConNota();
        await expect(motor.registrar("nota.editar", 1, { notaId: "0192f0a0-0000-7000-8000-00000000ffff", versionNota: 1, texto: "x" })).rejects.toThrow(ErrorSync);
        await expect(motor.registrar("nota.editar", 1, { notaId: DE_DESPACHO.id, versionNota: 1, texto: " Portón negro " })).rejects.toMatchObject({ codigo: "sin_cambios" });
        expect(motor.obtenerOrden(1)?.pendientes).toBe(0);
    });

    it("se admiten con la orden cancelada o pausada (evidencia)", async () => {
        const m = await montar({}, false);
        m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [ordenDePrueba(1, { estado: "cancelada", notas: [DE_DESPACHO] }), ordenDePrueba(2, { estado: "pausada", notas: [{ ...DE_DESPACHO, id: "0192f0a0-0000-7000-8000-000000000002" }] })] });
        await m.motor.cargarPaquete(FECHA);
        await m.motor.registrar("nota.editar", 1, { notaId: DE_DESPACHO.id, versionNota: 1, texto: "Llegué y estaba cancelada" });
        await m.motor.registrar("nota.eliminar", 2, { notaId: "0192f0a0-0000-7000-8000-000000000002", versionNota: 1 });
        expect(m.motor.obtenerOrden(1)?.notas?.[0]?.texto).toBe("Llegué y estaba cancelada");
        expect(m.motor.obtenerOrden(2)?.notas).toEqual([]);
    });

    it("un conflicto_version se muestra (nunca en silencio) y «Rehacer» va con la versión que trae el servidor", async () => {
        const { motor, transporte } = await montarConNota();
        const { operacion } = await motor.registrar("nota.editar", 1, { notaId: DE_DESPACHO.id, versionNota: 1, texto: "Mi versión" });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => ({ id: op.id, estado: "rechazada", error: { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409 } })),
        });
        await motor.sincronizar();
        expect(motor.obtenerOrden(1)?.rechazos).toBe(1);
        // Lo que se ve es lo del servidor (la edición rechazada no se aplica en la vista).
        expect(motor.obtenerOrden(1)?.notas?.[0]?.texto).toBe("Portón negro");

        // Despacho la había corregido: llega por el feed con la versión 2.
        transporte.cambios = (cursor) => ({
            cambios: [{ tipo: "actualizada", ordenId: 1, orden: ordenDePrueba(1, { version: 5, notas: [{ ...DE_DESPACHO, texto: "Corregida por despacho", version: 2 }] }) }],
            cursor: `${cursor}+`,
            hayMas: false,
        });
        transporte.lote = () => ({ resultados: [] });
        await motor.sincronizar();
        expect(motor.obtenerOrden(1)?.notas?.[0]?.texto).toBe("Corregida por despacho");
        transporte.cambios = (cursor) => ({ cambios: [], cursor, hayMas: false });
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => transporte.aplicar(op)) });
        const nueva = await motor.rehacer(operacion.id);
        expect((nueva.datos as { versionNota: number }).versionNota).toBe(2);
        await motor.sincronizar();
        expect(motor.obtenerOrden(1)?.notas?.[0]).toMatchObject({ texto: "Mi versión", version: 3 });
    });
});
