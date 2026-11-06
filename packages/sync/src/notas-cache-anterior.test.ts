// Órdenes guardadas por la app antes de 0.16, cuando las notas aún no traían `version`
// (reporte del PM: «Nota editada · OT-000002 — expected required property versionNota to be present»).
import { describe, expect, it } from "vitest";
import { detallesProblema, textoProblema } from "./campo/textos";
import type { NotaOrden, Paquete, Problema } from "./contrato";
import { ErrorTransporte } from "./errores";
import type { OperacionLocal } from "./modelo";
import { ordenDePrueba } from "./pruebas/fabricas";
import { FECHA, montar, paqueteDePrueba } from "./pruebas/montaje";

const NOTA_ID = "0192f0a0-0000-7000-8000-000000000001";

const NOTA: NotaOrden = {
    id: NOTA_ID,
    texto: "Portón negro",
    autorId: 9,
    autorNombre: "Técnico",
    canal: "campo",
    ocurridoEn: "2026-10-28T14:00:00Z",
    version: 1,
    editadaEn: null,
};

/** La nota como la guardó la app 0.15: sin `version` ni `editadaEn`. */
function notaSinVersion(): NotaOrden {
    const resto: Partial<NotaOrden> = { ...NOTA };
    delete resto.version;
    delete resto.editadaEn;
    return resto as NotaOrden;
}

function paqueteCon(nota: NotaOrden): (fecha: string) => Paquete {
    return (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [ordenDePrueba(1, { notas: [nota] }), ordenDePrueba(2)] });
}

/** Problema que devuelve la API 0.16 cuando falta `versionNota` (validador de esquema, en inglés). */
const SIN_VERSION_NOTA: Problema = {
    type: "urn:iris:problema:validacion",
    title: "Datos inválidos",
    status: 422,
    detail: "Los datos enviados no son válidos",
    errors: [{ field: "datos.", message: "expected required property versionNota to be present", rule: "esquema" }],
};

/** Teléfono con la orden 1 guardada por 0.15 (nota sin versión); el servidor ya está en 0.16. */
async function montarCacheAnterior(versionServidor = 1) {
    const m = await montar({}, false);
    m.transporte.paquete = paqueteCon(notaSinVersion());
    await m.motor.cargarPaquete(FECHA);
    m.transporte.paquete = paqueteCon({ ...NOTA, version: versionServidor });
    m.transporte.llamadas.length = 0;
    return m;
}

function versionesEnviadas(m: Awaited<ReturnType<typeof montar>>): unknown[] {
    return m.transporte.lotes.flatMap((l) => (l.operaciones ?? []).filter((o) => o.tipo.startsWith("nota.")).map((o) => (o.datos as { versionNota?: unknown }).versionNota));
}

describe("notas con la orden guardada antes de 0.16 (sin version)", () => {
    it("editar sin versión no encola nada: pide actualizar, y tras la sincronización sale con versionNota", async () => {
        const m = await montarCacheAnterior();
        const nota = m.motor.obtenerOrden(1)?.notas?.[0];
        expect(nota?.version).toBeUndefined();
        // Lo que manda la pantalla: `versionNota: nota.version` (undefined).
        await expect(m.motor.registrar("nota.editar", 1, { notaId: NOTA_ID, versionNota: nota?.version as number, texto: "Portón verde" })).rejects.toMatchObject({
            codigo: "nota_sin_version",
        });
        await expect(m.motor.registrar("nota.eliminar", 1, { notaId: NOTA_ID, versionNota: nota?.version as number })).rejects.toMatchObject({ codigo: "nota_sin_version" });
        expect(m.motor.obtenerEstado().cola).toEqual([]);

        // La pasada vuelve a bajar el paquete y la nota queda con su versión.
        await m.motor.sincronizar();
        expect(m.transporte.llamadas.filter((l) => l.tipo === "paquete")).toHaveLength(1);
        const fresca = m.motor.obtenerOrden(1)?.notas?.[0];
        expect(fresca?.version).toBe(1);
        await m.motor.registrar("nota.editar", 1, { notaId: NOTA_ID, versionNota: fresca?.version as number, texto: "Portón verde" });
        await m.motor.sincronizar();
        expect(versionesEnviadas(m)).toEqual([1]);
        // Ya refrescada: las pasadas siguientes no vuelven a bajar el paquete.
        expect(m.transporte.llamadas.filter((l) => l.tipo === "paquete")).toHaveLength(1);
    });

    it("al abrir la app actualizada, la primera pasada refresca la orden vieja una sola vez aunque el servidor no la traiga", async () => {
        const m = await montar({}, false);
        m.transporte.paquete = paqueteCon(notaSinVersion());
        await m.motor.cargarPaquete(FECHA);
        m.transporte.llamadas.length = 0;
        const motor = await m.reabrir();
        await motor.sincronizar();
        await motor.sincronizar();
        // Sigue sin versión (servidor viejo): se pidió una vez y no se insiste en cada pasada.
        expect(m.transporte.llamadas.filter((l) => l.tipo === "paquete")).toHaveLength(1);
        await expect(motor.registrar("nota.editar", 1, { notaId: NOTA_ID, versionNota: undefined as unknown as number, texto: "x" })).rejects.toMatchObject({ codigo: "nota_sin_version" });
    });

    it("sin señal la edición tampoco sale sin versión; con señal se refresca y queda bien", async () => {
        const m = await montarCacheAnterior();
        m.transporte.sinRed = true;
        await expect(m.motor.sincronizar()).rejects.toThrow();
        await expect(m.motor.registrar("nota.editar", 1, { notaId: NOTA_ID, versionNota: undefined as unknown as number, texto: "x" })).rejects.toMatchObject({ codigo: "nota_sin_version" });
        m.transporte.sinRed = false;
        await m.motor.sincronizar();
        // La pantalla puede no mandar la versión: el motor pone la de la nota que ve el técnico.
        const { operacion } = await m.motor.registrar("nota.editar", 1, { notaId: NOTA_ID, versionNota: undefined as unknown as number, texto: "x" });
        expect((operacion.datos as { versionNota: number }).versionNota).toBe(1);
    });

    it("la operación rechazada del teléfono del PM se recupera con «Rehacer» usando lo que trae el servidor", async () => {
        const m = await montar({}, false);
        m.transporte.paquete = paqueteCon(notaSinVersion());
        await m.motor.cargarPaquete(FECHA);
        // Lo que quedó en el teléfono con 0.16 antes del arreglo: sin versionNota y rechazada.
        const rechazada = {
            id: "0192f0a0-0000-7000-8000-0000000000aa",
            tipo: "nota.editar",
            ordenId: 1,
            versionBase: 1,
            ocurridoEn: "2026-10-28T15:00:00Z",
            datos: { notaId: NOTA_ID, texto: "Portón verde" },
            secuencia: 1,
            estado: "rechazada",
            error: SIN_VERSION_NOTA,
            intentos: 1,
        } as unknown as OperacionLocal;
        await m.almacen.escribir({ operaciones: { poner: [rechazada] }, meta: { secuencia: 1 } });
        // Despacho la editó dos veces en el panel: el servidor está en la versión 3.
        m.transporte.paquete = paqueteCon({ ...NOTA, texto: "Portón negro, timbre", version: 3 });
        const motor = await m.reabrir();
        m.transporte.llamadas.length = 0;

        const op = motor.obtenerEstado().rechazos[0];
        expect(op?.id).toBe(rechazada.id);
        // El técnico lee español, nunca el mensaje del validador.
        expect(textoProblema(op?.error)).toBe("Algún dato no es válido.");
        expect(detallesProblema(op?.error, null, op?.tipo)).toEqual(["Falta un dato; toca «Rehacer» para enviarlo de nuevo."]);

        const nueva = await motor.rehacer(rechazada.id);
        expect(m.transporte.llamadas.map((l) => l.tipo)).toEqual(["paquete"]);
        expect(nueva.datos).toEqual({ notaId: NOTA_ID, texto: "Portón verde", versionNota: 3 });
        await motor.sincronizar();
        expect(versionesEnviadas(m)).toEqual([3]);
        expect(motor.obtenerEstado().rechazos).toEqual([]);
        expect(motor.obtenerOrden(1)?.notas?.[0]).toMatchObject({ texto: "Portón verde", version: 4 });
    });

    it("«Rehacer» sin señal y con la base sin versión no encola nada; «Descartar» y editar de nuevo funciona", async () => {
        const m = await montar({}, false);
        m.transporte.paquete = paqueteCon(notaSinVersion());
        await m.motor.cargarPaquete(FECHA);
        const rechazada = {
            id: "0192f0a0-0000-7000-8000-0000000000ab",
            tipo: "nota.eliminar",
            ordenId: 1,
            versionBase: 1,
            ocurridoEn: "2026-10-28T15:00:00Z",
            datos: { notaId: NOTA_ID },
            secuencia: 1,
            estado: "rechazada",
            error: SIN_VERSION_NOTA,
            intentos: 1,
        } as unknown as OperacionLocal;
        await m.almacen.escribir({ operaciones: { poner: [rechazada] }, meta: { secuencia: 1 } });
        m.transporte.paquete = paqueteCon({ ...NOTA, version: 2 });
        const motor = await m.reabrir();
        m.transporte.sinRed = true;

        await expect(motor.rehacer(rechazada.id)).rejects.toMatchObject({ codigo: "nota_sin_version" });
        expect(motor.obtenerEstado().rechazos.map((o) => o.id)).toEqual([rechazada.id]);
        expect(motor.obtenerEstado().cola).toEqual([]);

        await motor.descartar(rechazada.id);
        m.transporte.sinRed = false;
        await motor.sincronizar();
        await motor.registrar("nota.eliminar", 1, { notaId: NOTA_ID, versionNota: motor.obtenerOrden(1)?.notas?.[0]?.version as number });
        await motor.sincronizar();
        expect(versionesEnviadas(m)).toEqual([2]);
    });

    it("«Rehacer» no pisa en silencio una edición de despacho hecha después de revisar el conflicto", async () => {
        const m = await montar({}, false);
        m.transporte.paquete = paqueteCon(NOTA);
        await m.motor.cargarPaquete(FECHA);
        const { operacion } = await m.motor.registrar("nota.editar", 1, { notaId: NOTA_ID, versionNota: 1, texto: "Mía" });
        m.transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => ({ id: op.id, estado: "rechazada", error: { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409 } })),
        });
        await m.motor.sincronizar();
        expect(m.motor.obtenerOrden(1)?.rechazos).toBe(1);
        // El técnico revisó la versión 1; despacho la editó (versión 5) y el feed aún no lo trajo.
        m.transporte.paquete = paqueteCon({ ...NOTA, texto: "Corregida por despacho", version: 5 });
        m.transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => m.transporte.aplicar(op)) });
        await expect(m.motor.rehacer(operacion.id)).rejects.toMatchObject({ codigo: "nota_cambiada", message: expect.stringContaining("Corregida por despacho") });
        // Sigue rechazada, con conflicto_version, y la vista muestra el texto nuevo del servidor.
        const rechazo = m.motor.obtenerEstado().rechazos[0];
        expect(rechazo?.id).toBe(operacion.id);
        expect(rechazo?.error).toMatchObject({ type: "urn:iris:problema:conflicto_version", status: 409 });
        expect(m.motor.obtenerOrden(1)?.notas?.[0]).toMatchObject({ texto: "Corregida por despacho", version: 5 });
        expect(m.motor.obtenerEstado().cola).toEqual([]);
        // Ya la revisó (la versión 5 está en la base): ahora sí sale con esa versión.
        const nueva = await m.motor.rehacer(operacion.id);
        expect((nueva.datos as { versionNota: number }).versionNota).toBe(5);
        await m.motor.sincronizar();
        expect(versionesEnviadas(m)).toEqual([1, 5]);
    });

    it("con la caché anterior (sin versión) «Rehacer» usa la del servidor sin preguntar", async () => {
        const m = await montarCacheAnterior(7);
        const rechazada = {
            id: "0192f0a0-0000-7000-8000-0000000000ac",
            tipo: "nota.editar",
            ordenId: 1,
            versionBase: 1,
            ocurridoEn: "2026-10-28T15:00:00Z",
            datos: { notaId: NOTA_ID, texto: "Portón verde" },
            secuencia: 1,
            estado: "rechazada",
            error: SIN_VERSION_NOTA,
            intentos: 1,
        } as unknown as OperacionLocal;
        await m.almacen.escribir({ operaciones: { poner: [rechazada] }, meta: { secuencia: 1 } });
        const motor = await m.reabrir();
        const nueva = await motor.rehacer(rechazada.id);
        expect((nueva.datos as { versionNota: number }).versionNota).toBe(7);
    });

    it("«Rehacer»: sin sesión lanza (no dice «Sin conexión») y un error de la API se distingue de la red", async () => {
        const m = await montar({}, false);
        m.transporte.paquete = paqueteCon(notaSinVersion());
        await m.motor.cargarPaquete(FECHA);
        const rechazada = {
            id: "0192f0a0-0000-7000-8000-0000000000ad",
            tipo: "nota.eliminar",
            ordenId: 1,
            versionBase: 1,
            ocurridoEn: "2026-10-28T15:00:00Z",
            datos: { notaId: NOTA_ID },
            secuencia: 1,
            estado: "rechazada",
            error: SIN_VERSION_NOTA,
            intentos: 1,
        } as unknown as OperacionLocal;
        await m.almacen.escribir({ operaciones: { poner: [rechazada] }, meta: { secuencia: 1 } });
        const motor = await m.reabrir();

        m.transporte.fallar(1, new ErrorTransporte("http", "Sin sesión", 401));
        await expect(motor.rehacer(rechazada.id)).rejects.toBeInstanceOf(ErrorTransporte);
        m.transporte.fallar(1, new ErrorTransporte("http", "Sin permiso", 403));
        await expect(motor.rehacer(rechazada.id)).rejects.toMatchObject({ status: 403 });

        m.transporte.fallar(1, new ErrorTransporte("http", "Error interno", 500));
        await expect(motor.rehacer(rechazada.id)).rejects.toMatchObject({ codigo: "nota_sin_version", message: expect.stringContaining("El servidor no respondió bien") });
        m.transporte.fallar(1);
        await expect(motor.rehacer(rechazada.id)).rejects.toMatchObject({ codigo: "nota_sin_version", message: expect.stringContaining("Sin conexión") });
        expect(motor.obtenerEstado().rechazos.map((o) => o.id)).toEqual([rechazada.id]);
    });

    it("registrar no cambia los datos de quien llama", async () => {
        const m = await montarCacheAnterior();
        await m.motor.sincronizar();
        const datos = { notaId: NOTA_ID, versionNota: undefined as unknown as number, texto: "Otra" };
        const { operacion } = await m.motor.registrar("nota.editar", 1, datos);
        expect((operacion.datos as { versionNota: number }).versionNota).toBe(1);
        expect(datos.versionNota).toBeUndefined();
    });
});
