import { describe, expect, it } from "vitest";

import {
    esquemaFormularioRecepcion,
    esquemaRecibirTraslado,
    formularioRecepcionInicial,
    lineasDeRecepcion,
    porRecibir,
    sePuedeRehacerRecepcion,
    situacionTraslado,
    type FormularioRecepcion,
} from "./campo";
import {
    DIA_MS,
    ErrorSync,
    ErrorTransporte,
    grupoTraslado,
    TIPO_RECIBIR_TRASLADO,
    TIPOS_OPERACION,
    operacionRecibirTraslado,
    type EntradaRecibirTraslado,
    type OperacionTrasladoLocal,
    type Problema,
    type ResultadoOperacion,
    type Traslado,
} from "./index";
import { montar } from "./pruebas/montaje";

// traslado.recibir (contratos/sync.md §2 y §3): no es de una orden y no entra en las operaciones del motor.
describe("traslado.recibir", () => {
    const datos: EntradaRecibirTraslado = {
        trasladoId: 31,
        firmaAdjuntoId: "0190f000-0000-7000-8000-000000000001",
        recibidoEn: "2026-10-30T15:00:00-06:00",
        lineas: [{ lineaId: 1, cantidadRecibida: 9, seriesFaltantes: ["ZTE-2"] }],
    };

    it("arma la operación sin ordenId ni versionBase", () => {
        const op = operacionRecibirTraslado("0190f000-0000-7000-8000-0000000000aa", datos, "2026-10-30T21:00:00Z");
        expect(op).toEqual({ id: "0190f000-0000-7000-8000-0000000000aa", tipo: "traslado.recibir", ocurridoEn: "2026-10-30T21:00:00Z", datos });
        expect("ordenId" in op).toBe(false);
        expect("versionBase" in op).toBe(false);
    });

    it("no es una operación de orden del motor", () => {
        expect((TIPOS_OPERACION as readonly string[]).includes(TIPO_RECIBIR_TRASLADO)).toBe(false);
    });

    it("valida los datos como la API", () => {
        expect(esquemaRecibirTraslado.safeParse(datos).success).toBe(true);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, lineas: undefined }).success).toBe(true);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, trasladoId: 0 }).success).toBe(false);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, firmaAdjuntoId: "no-uuid" }).success).toBe(false);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, recibidoEn: "ayer" }).success).toBe(false);
        expect(esquemaRecibirTraslado.safeParse({ ...datos, lineas: [{ lineaId: 1, cantidadRecibida: -1 }] }).success).toBe(false);
        const repetida = esquemaRecibirTraslado.safeParse({
            ...datos,
            lineas: [
                { lineaId: 1, cantidadRecibida: 1 },
                { lineaId: 1, cantidadRecibida: 2 },
            ],
        });
        expect(repetida.success).toBe(false);
        expect(repetida.error?.issues[0]?.path).toEqual(["lineas", 1, "lineaId"]);
    });
});

// Recepción en el motor (LOW 3 de la PR #83): agrupada por traslado con clave negativa.
describe("motor: traslado.recibir", () => {
    const FIRMA = new Uint8Array([137, 80, 78, 71, 1, 2, 3, 4]);
    const problema = (codigo: string, status = 422): Problema => ({ type: `urn:iris:problema:${codigo}`, title: codigo, status });

    it("la clave de grupo es -trasladoId (nunca 0 ni la de una orden)", () => {
        expect(grupoTraslado(31)).toBe(-31);
        expect(grupoTraslado(1)).toBe(-1);
        expect(grupoTraslado(31)).not.toBe(0);
    });

    it("guarda la firma (entidad traslado) y la operación sin ordenId ni versionBase en el cable", async () => {
        const { motor, transporte, subidor, almacen } = await montar();
        const { adjunto, operacion } = await motor.recibirTraslado({ trasladoId: 31, firma: FIRMA, lineas: [{ lineaId: 1, cantidadRecibida: 9 }] });
        expect(operacion).toMatchObject({ tipo: "traslado.recibir", ordenId: -31, versionBase: 0, datos: { trasladoId: 31, firmaAdjuntoId: adjunto.id } });
        expect(almacen.adjuntos.get(adjunto.id)).toMatchObject({ entidad: "traslado", ordenId: -31, tipo: "firma", mime: "image/png", obligatorio: true });
        expect(motor.obtenerEstado().recepciones.map((r) => r.id)).toEqual([operacion.id]);
        expect(motor.obtenerEstado().pendientes).toBe(1);

        await motor.sincronizar();
        expect(subidor.subidas.get(adjunto.id)?.entrada).toMatchObject({ entidad: "traslado", entidadId: "31", tipo: "firma" });
        const cable = transporte.lotes.flatMap((l) => l.operaciones ?? []).find((o) => o.id === operacion.id);
        expect(cable).toEqual({ id: operacion.id, tipo: "traslado.recibir", ocurridoEn: operacion.ocurridoEn, datos: operacion.datos });
        expect(motor.obtenerEstado()).toMatchObject({ pendientes: 0, recepciones: [] });
    });

    it("no se puede recibir dos veces el mismo traslado mientras está en la cola", async () => {
        const { motor, transporte } = await montar();
        transporte.sinRed = true;
        await motor.recibirTraslado({ trasladoId: 31, firma: FIRMA });
        await expect(motor.recibirTraslado({ trasladoId: 31, firma: FIRMA })).rejects.toBeInstanceOf(ErrorSync);
        await expect(motor.recibirTraslado({ trasladoId: 0, firma: FIRMA })).rejects.toBeInstanceOf(ErrorSync);
    });

    it("la espera de un traslado no detiene a otro traslado ni a las órdenes", async () => {
        const { motor, transporte, almacen } = await montar({ espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0 } });
        const a = await motor.recibirTraslado({ trasladoId: 31, firma: FIRMA });
        const b = await motor.recibirTraslado({ trasladoId: 32, firma: FIRMA });
        const orden = await motor.registrar("orden.iniciar_camino", 1, {});
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map(
                (op): ResultadoOperacion =>
                    op.tipo === "traslado.recibir" && (op.datos as { trasladoId: number }).trasladoId === 31
                        ? { id: op.id, estado: "rechazada", error: problema("traslado_sin_confirmar", 503) }
                        : transporte.aplicar(op),
            ),
        });
        await motor.sincronizar();
        // El 31 espera con su propia espera; el 32 y la orden salieron.
        expect(almacen.operaciones.get(a.operacion.id)).toMatchObject({ estado: "pendiente", esperas: 1, ordenId: -31 });
        expect(almacen.operaciones.has(b.operacion.id)).toBe(false);
        expect(almacen.operaciones.has(orden.operacion.id)).toBe(false);
        expect(motor.obtenerEstado().rechazos).toEqual([]);
    });

    it("un rechazo de un traslado no bloquea a otro (ni a una orden): Descartar borra la firma pendiente", async () => {
        const { motor, transporte, almacen } = await montar();
        transporte.sinRed = true;
        const a = await motor.recibirTraslado({ trasladoId: 40, firma: FIRMA });
        transporte.sinRed = false;
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map(
                (op): ResultadoOperacion =>
                    op.id === a.operacion.id ? { id: op.id, estado: "rechazada", error: problema("traslado_no_encontrado", 404) } : transporte.aplicar(op),
            ),
        });
        await motor.sincronizar();
        expect(motor.obtenerEstado().rechazos.map((r) => r.id)).toEqual([a.operacion.id]);
        const b = await motor.recibirTraslado({ trasladoId: 41, firma: FIRMA });
        const orden = await motor.registrar("nota.agregar", 1, { texto: "sigue" });
        await motor.sincronizar();
        expect(almacen.operaciones.has(b.operacion.id)).toBe(false);
        expect(almacen.operaciones.has(orden.operacion.id)).toBe(false);
        // Descartar el rechazado deja la cola limpia.
        await motor.descartar(a.operacion.id);
        expect(motor.obtenerEstado()).toMatchObject({ rechazos: [], recepciones: [] });
    });

    it("la firma con 404 (el traslado aún no llega) espera y se reintenta; luego sale la recepción", async () => {
        const { motor, transporte, subidor, reloj } = await montar({ espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0 } });
        const r = await motor.recibirTraslado({ trasladoId: 50, firma: FIRMA });
        subidor.rechazarAlta = new ErrorTransporte("http", "No existe", 404, problema("traslado_no_encontrado", 404));
        await motor.sincronizar();
        expect(motor.obtenerEstado().adjuntos.find((x) => x.id === r.adjunto.id)).toMatchObject({ estado: "por_crear", esperas: 1 });
        // Retenida detrás de su firma: no salió.
        expect(transporte.lotes.flatMap((l) => l.operaciones ?? []).some((o) => o.id === r.operacion.id)).toBe(false);
        // Antes de la espera no se reintenta el alta.
        const altas = subidor.llamadas.filter((l) => l.startsWith("POST")).length;
        await motor.sincronizar();
        expect(subidor.llamadas.filter((l) => l.startsWith("POST")).length).toBe(altas);
        await reloj.avanzar(1000);
        await motor.sincronizar();
        expect(motor.obtenerEstado().adjuntos.find((x) => x.id === r.adjunto.id)?.estado).toBe("subido");
        expect(transporte.lotes.flatMap((l) => l.operaciones ?? []).some((o) => o.id === r.operacion.id)).toBe(true);
    });

    it("la firma con 404 pasadas 24 h queda rechazada", async () => {
        const { motor, subidor, reloj } = await montar({ espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0 } });
        const r = await motor.recibirTraslado({ trasladoId: 51, firma: FIRMA });
        subidor.rechazarAlta = new ErrorTransporte("http", "No existe", 404, problema("traslado_no_encontrado", 404));
        await motor.sincronizar();
        await reloj.avanzar(DIA_MS);
        subidor.rechazarAlta = new ErrorTransporte("http", "No existe", 404, problema("traslado_no_encontrado", 404));
        await motor.sincronizar();
        expect(motor.obtenerEstado().adjuntos.find((x) => x.id === r.adjunto.id)).toMatchObject({ estado: "rechazado" });
        expect(motor.obtenerEstado().subidas.rechazadas).toBe(1);
    });
});

describe("«Por recibir»: situación y formulario", () => {
    const traslado = (id: number, extra: Partial<Traslado> = {}): Traslado => ({
        id,
        estado: "enviado",
        lineas: [
            { lineaId: 1, productoId: 7, cantidad: 10, series: null },
            { lineaId: 2, productoId: 8, cantidad: 2, series: ["ZTE-1", "ZTE-2"] },
        ],
        ...extra,
    });
    const local = (trasladoId: number, estado: OperacionTrasladoLocal["estado"], codigo?: string): OperacionTrasladoLocal => ({
        id: `op-${trasladoId}`,
        tipo: "traslado.recibir",
        ordenId: grupoTraslado(trasladoId),
        versionBase: 0,
        ocurridoEn: "2026-10-31T15:00:00Z",
        datos: { trasladoId, firmaAdjuntoId: "0190f000-0000-7000-8000-000000000001", recibidoEn: "2026-10-31T15:00:00Z" },
        secuencia: 1,
        estado,
        error: codigo ? { type: `urn:iris:problema:${codigo}`, title: codigo, status: 404 } : null,
        intentos: 0,
    });

    it("cada traslado según su recepción local o la de la API", () => {
        const recepciones = [local(2, "pendiente"), local(3, "rechazada", "traslado_no_encontrado")];
        expect(situacionTraslado(traslado(1), recepciones)).toBe("por_recibir");
        expect(situacionTraslado(traslado(2), recepciones)).toBe("en_telefono");
        expect(situacionTraslado(traslado(3), recepciones)).toBe("rechazado_telefono");
        const conRecepcion = (estado: NonNullable<Traslado["recepcion"]>["estado"]) =>
            traslado(4, { recepcion: { trasladoId: 4, pendienteId: 9, estado, actualizadoEn: "2026-10-31T15:00:00Z" } });
        expect(situacionTraslado(conRecepcion("enviando"), [])).toBe("enviando");
        expect(situacionTraslado(conRecepcion("esperando_firma"), [])).toBe("enviando");
        expect(situacionTraslado(conRecepcion("recibida"), [])).toBe("recibido");
        expect(situacionTraslado(conRecepcion("rechazada"), [])).toBe("rechazado_argos");
        expect(situacionTraslado(conRecepcion("agotada"), [])).toBe("agotado");
        // Cuentan los que faltan por recibir (también si Argos rechazó la recepción anterior).
        expect(porRecibir([traslado(1), traslado(2), conRecepcion("rechazada"), traslado(5, { estado: "cancelado" })], recepciones).map((t) => t.id)).toEqual([1, 4]);
    });

    it("traslado_no_encontrado solo se descarta; lo demás se rehace", () => {
        expect(sePuedeRehacerRecepcion(local(3, "rechazada", "traslado_no_encontrado"))).toBe(false);
        expect(sePuedeRehacerRecepcion(local(3, "rechazada", "traslado_sin_confirmar"))).toBe(true);
    });

    it("si llegó todo no hay líneas; si faltó, lo recibido y las series que no llegaron", () => {
        const t = traslado(1);
        const inicial = formularioRecepcionInicial(t);
        expect(lineasDeRecepcion(t, inicial)).toEqual([]);
        const falta: FormularioRecepcion = {
            lineas: [
                { total: 10, falta: true, cantidad: "7,5", series: [] },
                { total: 2, falta: true, cantidad: "", series: [true, false] },
            ],
        };
        expect(esquemaFormularioRecepcion.safeParse(falta).success).toBe(true);
        expect(lineasDeRecepcion(t, falta)).toEqual([
            { lineaId: 1, cantidadRecibida: 7.5 },
            { lineaId: 2, cantidadRecibida: 1, seriesFaltantes: ["ZTE-2"] },
        ]);
        const mal = esquemaFormularioRecepcion.safeParse({ lineas: [{ total: 10, falta: true, cantidad: "10", series: [] }] });
        expect(mal.success).toBe(false);
        expect(mal.error?.issues[0]?.path).toEqual(["lineas", 0, "cantidad"]);
    });
});
