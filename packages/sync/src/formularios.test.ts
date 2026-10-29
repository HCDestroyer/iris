import { describe, expect, it } from "vitest";
import { formularioConRespuestas, segundosPausada } from "./aplicar";
import type { CambiosConTurno, FormularioOrden, PlantillaFormulario, Problema, ResultadoOperacion } from "./contrato";
import { ErrorSync } from "./errores";
import { itemDePrueba, ordenDePrueba } from "./pruebas/fabricas";
import { FECHA, montar, paqueteDePrueba } from "./pruebas/montaje";

const problema = (codigo: string, status = 422, detail?: string): Problema => ({ type: `urn:iris:problema:${codigo}`, title: codigo, status, ...(detail ? { detail } : {}) });
const rechazo = (id: string, codigo: string, status = 422): ResultadoOperacion => ({ id, estado: "rechazada", error: problema(codigo, status) });

function plantilla(id: number, parcial: Partial<PlantillaFormulario> = {}): PlantillaFormulario {
    return {
        id,
        codigo: `form_${id}`,
        nombre: `Formulario ${id}`,
        descripcion: "",
        empresaId: null,
        activa: true,
        porDefecto: false,
        requiereFirma: false,
        tipo: null,
        version: 1,
        versionActual: 2,
        versionId: 900 + id,
        numeroVersion: 2,
        creadoEn: "2026-10-01T00:00:00Z",
        actualizadoEn: "2026-10-01T00:00:00Z",
        items: [
            { id: 5000 + id * 10, codigo: "potencia", etiqueta: "Potencia óptica", tipo: "medicion", obligatorio: true, unidad: "dBm", posicion: 1 },
            { id: 5001 + id * 10, codigo: "router", etiqueta: "Modelo de router", tipo: "seleccion", obligatorio: false, unidad: "", posicion: 2, opciones: ["A", "B"] },
        ],
        ...parcial,
    };
}

const FORM_TIPO: FormularioOrden = {
    id: "f-tipo",
    plantillaId: 1,
    codigo: "instalacion",
    nombre: "Instalación",
    versionId: 11,
    numeroVersion: 1,
    requiereFirma: false,
    origen: "tipo",
    agregadoPor: null,
    agregadoEn: "2026-10-28T12:00:00Z",
    conRespuestas: false,
};

/** Motor con una orden en sitio (formulario del tipo) y dos plantillas en el catálogo. */
async function conFormularios() {
    const m = await montar();
    m.transporte.paquete = (fecha) => ({
        ...paqueteDePrueba(fecha),
        ordenes: [
            ordenDePrueba(1, { estado: "en_sitio", formularios: [FORM_TIPO], checklist: [itemDePrueba(1, { formularioId: "f-tipo" })] }),
            ordenDePrueba(2, { estado: "programada" }),
        ],
        plantillas: [plantilla(7), plantilla(8, { requiereFirma: true })],
    });
    await m.motor.cargarPaquete(FECHA);
    m.transporte.llamadas.length = 0;
    return m;
}

describe("formularios sin señal (orden.agregar_formulario / quitar_formulario)", () => {
    it("agrega uno del catálogo: el id de la operación es el del formulario, con sus ítems y la versión que vio", async () => {
        const { motor, transporte, reabrir } = await conFormularios();
        expect(motor.obtenerEstado().plantillas.map((p) => p.id)).toEqual([7, 8]);
        transporte.sinRed = true;

        const { operacion } = await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 8 });
        const orden = motor.obtenerOrden(1);
        expect(orden?.formularios?.map((f) => [f.id, f.origen, f.numeroVersion])).toEqual([
            ["f-tipo", "tipo", 1],
            [operacion.id, "tecnico", 2],
        ]);
        expect(orden?.checklist?.filter((i) => i.formularioId === operacion.id).map((i) => [i.itemId, i.tipo, i.opciones ?? null])).toEqual([
            [5080, "medicion", null],
            [5081, "seleccion", ["A", "B"]],
        ]);
        expect(orden?.requiereFirma).toBe(true);

        // Marca de un ítem del formulario nuevo (selección) y sobrevive a cerrar la app sin señal.
        await motor.registrar("checklist.marcar", 1, { itemId: 5081, valor: "B" });
        const reabierto = await reabrir();
        expect(reabierto.obtenerOrden(1)?.checklist?.find((i) => i.itemId === 5081)?.valor).toBe("B");

        // Viaja solo lo del contrato: { plantillaId, versionId } (la copia de la plantilla es local).
        transporte.sinRed = false;
        await reabierto.sincronizar();
        const ops = transporte.lotes.at(-1)?.operaciones ?? [];
        expect(ops[0]).toMatchObject({ id: operacion.id, tipo: "orden.agregar_formulario", datos: { plantillaId: 8, versionId: 908 } });
        expect(ops[0]).not.toHaveProperty("plantilla");
        expect(reabierto.obtenerOrden(1)?.formularios).toHaveLength(2);
    });

    it("no agrega dos veces la misma plantilla ni una que no está en el catálogo", async () => {
        const { motor } = await conFormularios();
        await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 });
        await expect(motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 })).rejects.toMatchObject({ codigo: "formulario_duplicado" });
        await expect(motor.registrar("orden.agregar_formulario", 1, { plantillaId: 99 })).rejects.toMatchObject({ codigo: "plantilla_desconocida" });
    });

    it("quita solo uno que agregó el técnico y sin respuestas; nunca el del tipo", async () => {
        const { motor, transporte } = await conFormularios();
        transporte.sinRed = true;
        const a = await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 });
        const b = await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 8 });
        await expect(motor.registrar("orden.quitar_formulario", 1, { formularioId: "f-tipo" })).rejects.toMatchObject({ codigo: "formulario_del_tipo" });

        await motor.registrar("checklist.marcar", 1, { itemId: 5070, valor: -18.5 });
        expect(formularioConRespuestas(motor.obtenerOrden(1) ?? ordenDePrueba(1), a.operacion.id)).toBe(true);
        await expect(motor.registrar("orden.quitar_formulario", 1, { formularioId: a.operacion.id })).rejects.toMatchObject({ codigo: "formulario_con_respuestas" });

        await motor.registrar("orden.quitar_formulario", 1, { formularioId: b.operacion.id });
        expect(motor.obtenerOrden(1)?.formularios?.map((f) => f.id)).toEqual(["f-tipo", a.operacion.id]);
        expect(motor.obtenerOrden(1)?.checklist?.some((i) => i.formularioId === b.operacion.id)).toBe(false);
        expect(motor.obtenerOrden(1)?.requiereFirma).toBe(false);
    });

    it("con la plantilla desactivada sin señal: el rechazo queda a la vista y las marcas de ese formulario, detenidas", async () => {
        const { motor, transporte } = await conFormularios();
        const agregar = await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 });
        await motor.registrar("checklist.marcar", 1, { itemId: 5070, valor: -19 });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => (op.id === agregar.operacion.id ? rechazo(op.id, "plantilla_inactiva") : rechazo(op.id, "bloqueada_por_rechazo"))),
        });
        await motor.sincronizar();
        const estado = motor.obtenerEstado();
        expect(estado.rechazos.map((r) => [r.tipo, r.error?.type])).toEqual([["orden.agregar_formulario", "urn:iris:problema:plantilla_inactiva"]]);
        expect(estado.pendientes).toBe(1);
        expect(motor.obtenerOrden(1)).toMatchObject({ bloqueada: true });
        // El formulario no se muestra como si estuviera: nada se resuelve en silencio.
        expect(motor.obtenerOrden(1)?.formularios?.map((f) => f.id)).toEqual(["f-tipo"]);
    });

    it("si un formulario vuelve a la cola con id nuevo, lo que lo cita lo sigue (quitar)", async () => {
        const { motor, transporte } = await conFormularios();
        const nota = await motor.registrar("nota.agregar", 1, { texto: "antes" });
        const agregar = await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => (op.id === nota.operacion.id ? rechazo(op.id, "validacion") : rechazo(op.id, "bloqueada_por_rechazo"))),
        });
        await motor.sincronizar();
        transporte.sinRed = true;
        const nuevoId = motor.obtenerEstado().cola.find((o) => o.tipo === "orden.agregar_formulario")?.id ?? "";
        expect(nuevoId).not.toBe(agregar.operacion.id);
        await motor.registrar("orden.quitar_formulario", 1, { formularioId: nuevoId });
        await motor.descartar(nota.operacion.id);
        transporte.sinRed = false;
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => transporte.aplicar(op)) });
        await motor.sincronizar();
        const ops = transporte.lotes.at(-1)?.operaciones ?? [];
        expect(ops.map((o) => o.tipo)).toEqual(["orden.agregar_formulario", "orden.quitar_formulario"]);
        expect(ops[1]?.datos).toEqual({ formularioId: ops[0]?.id });
    });

    it("el catálogo de cambios reemplaza al del paquete", async () => {
        const { motor, transporte } = await conFormularios();
        transporte.cambios = (cursor) => {
            const c: CambiosConTurno = { cambios: [], cursor, hayMas: false, plantillas: [plantilla(8)] };
            return c;
        };
        await motor.sincronizar();
        expect(motor.obtenerEstado().plantillas.map((p) => p.id)).toEqual([8]);
        await expect(motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 })).rejects.toBeInstanceOf(ErrorSync);
    });

    it("un aviso de la API (versión anterior) queda a la vista hasta que el técnico lo descarta", async () => {
        const { motor, transporte, reabrir } = await conFormularios();
        const { operacion } = await motor.registrar("orden.agregar_formulario", 1, { plantillaId: 7 });
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => ({ ...transporte.aplicar(op), aviso: "version_anterior" as const })) });
        await motor.sincronizar();
        expect(motor.obtenerEstado().avisos).toEqual([{ id: operacion.id, ordenId: 1, tipo: "orden.agregar_formulario", aviso: "version_anterior" }]);
        const reabierto = await reabrir();
        expect(reabierto.obtenerEstado().avisos).toHaveLength(1);
        await reabierto.descartarAviso(operacion.id);
        expect(reabierto.obtenerEstado().avisos).toEqual([]);
    });
});

describe("pausa (orden.pausar / orden.reanudar)", () => {
    it("pausa desde en sitio, bloquea cerrar/fallar/llegar/salir, admite evidencia y reanuda donde estaba", async () => {
        const { motor, transporte, reloj } = await conFormularios();
        transporte.ordenes.set(1, { version: 1, estado: "en_sitio" });
        transporte.sinRed = true;
        await expect(motor.registrar("orden.pausar", 2, { motivo: "clima" })).rejects.toMatchObject({ codigo: "transicion_invalida" });
        await expect(motor.registrar("orden.pausar", 1, { motivo: "otro" })).rejects.toMatchObject({ codigo: "nota_requerida" });
        await expect(motor.registrar("orden.reanudar", 1, {})).rejects.toMatchObject({ codigo: "orden_no_pausada" });

        await motor.registrar("orden.pausar", 1, { motivo: "esperando_material" });
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "pausada", pausadaDesde: "en_sitio" });
        expect(motor.obtenerOrden(1)?.pausas?.at(-1)).toMatchObject({ motivo: "esperando_material", fin: null, desdeEstado: "en_sitio" });
        for (const tipo of ["orden.cerrar", "orden.fallar", "orden.llegar", "orden.iniciar_camino"] as const) {
            await expect(motor.registrar(tipo, 1, tipo === "orden.fallar" ? { motivoFalla: "clima" } : {})).rejects.toMatchObject({ codigo: "orden_pausada" });
        }
        await expect(motor.registrar("orden.pausar", 1, { motivo: "clima" })).rejects.toMatchObject({ codigo: "orden_pausada" });
        await motor.registrar("nota.agregar", 1, { texto: "espero el conector" });
        await motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true });
        await motor.registrar("material.usar", 1, { productoId: 900, cantidad: 2 });

        reloj.fijar(reloj.ahora() + 15 * 60_000);
        expect(segundosPausada(motor.obtenerOrden(1) ?? ordenDePrueba(1), reloj.ahora())).toBe(900);
        await motor.registrar("orden.reanudar", 1, {});
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "en_sitio", tiempoPausadoSeg: 900 });
        expect(motor.obtenerOrden(1)).not.toHaveProperty("pausadaEn");
        expect(motor.obtenerOrden(1)?.pausas?.at(-1)).toMatchObject({ duracionSeg: 900, cierre: "reanudada" });

        transporte.sinRed = false;
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => o.tipo)).toEqual([
            "orden.pausar",
            "nota.agregar",
            "checklist.marcar",
            "material.usar",
            "orden.reanudar",
        ]);
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "en_sitio", pendientes: 0 });
    });

    it("una pausa que despacho ya hizo se rechaza a la vista (orden_ya_pausada), y la vista vuelve al estado del servidor", async () => {
        const { motor, transporte } = await conFormularios();
        const { operacion } = await motor.registrar("orden.pausar", 1, { motivo: "acceso" });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => ({ ...rechazo(op.id, "orden_ya_pausada", 409), orden: { version: 4, estado: "pausada" as const } })),
        });
        await motor.sincronizar();
        expect(motor.obtenerEstado().rechazos.map((r) => r.id)).toEqual([operacion.id]);
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "pausada", version: 4 });
    });
});
