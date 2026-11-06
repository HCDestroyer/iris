import { describe, expect, it } from "vitest";

import {
    avisoYaTomada,
    cuandoLibre,
    datosTarea,
    distanciaTexto,
    duracionTexto,
    esquemaTarea,
    intercalarTareas,
    subtituloLibres,
    tareasDelDia,
    textoCuando,
    valoresInicialesTarea,
} from "./campo";
import {
    esGrupoTarea,
    grupoTarea,
    grupoTraslado,
    type Problema,
    type ResultadoOperacion,
    type Tarea,
} from "./index";
import { montar } from "./pruebas/montaje";

const tarea = (
    id: string,
    inicio: string,
    extra: Partial<Tarea> = {},
): Tarea => ({
    id,
    empresaId: null,
    tecnicoId: 7,
    creadaPor: 7,
    que: `Tarea ${id}`,
    inicio,
    fin: inicio,
    duracionMin: 30,
    ordenId: null,
    clienteId: null,
    origen: "campo",
    estado: "pendiente",
    completadaEn: null,
    anuladaEn: null,
    motivo: null,
    version: 1,
    creadoEn: inicio,
    ...extra,
});

describe("Nueva tarea: formulario (espejo de la API)", () => {
    const validos = {
        que: "Recoger material en bodega central",
        dia: "2026-11-05",
        hora: "14:00",
        duracionMin: 30,
        ligada: "",
    };

    it("valida qué, día, hora y duración como la API", () => {
        expect(esquemaTarea.safeParse(validos).success).toBe(true);
        expect(esquemaTarea.safeParse({ ...validos, que: "   " }).success).toBe(
            false,
        );
        expect(
            esquemaTarea.safeParse({ ...validos, que: "a".repeat(201) })
                .success,
        ).toBe(false);
        expect(
            esquemaTarea.safeParse({ ...validos, duracionMin: 4 }).success,
        ).toBe(false);
        expect(
            esquemaTarea.safeParse({ ...validos, duracionMin: 481 }).success,
        ).toBe(false);
        expect(
            esquemaTarea.safeParse({ ...validos, hora: "25:00" }).success,
        ).toBe(false);
        expect(
            esquemaTarea.safeParse({ ...validos, ligada: "cliente:3" }).success,
        ).toBe(false);
    });

    it("arma los datos de tarea.crear en UTC (Guatemala es UTC−6) y con la orden ligada", () => {
        expect(
            datosTarea({ ...validos, que: " Recoger ", ligada: "orden:12" }),
        ).toEqual({
            que: "Recoger",
            inicio: "2026-11-05T20:00:00.000Z",
            duracionMin: 30,
            ordenId: 12,
        });
        expect(datosTarea(validos)).not.toHaveProperty("ordenId");
    });

    it("empieza hoy a la siguiente media hora, con 30 minutos", () => {
        // 13:40 en Guatemala → 14:00.
        expect(
            valoresInicialesTarea(Date.parse("2026-11-05T19:40:00Z")),
        ).toEqual({
            que: "",
            dia: "2026-11-05",
            hora: "14:00",
            duracionMin: 30,
            ligada: "",
        });
    });

    it("textos del cuándo y la duración", () => {
        expect(textoCuando("2026-11-05", "14:00", "2026-11-05")).toBe(
            "Hoy · 14:00",
        );
        expect(textoCuando("2026-11-06", "09:00", "2026-11-05")).toBe(
            "Mañana · 09:00",
        );
        expect(duracionTexto(30)).toBe("30 min");
        expect(duracionTexto(60)).toBe("1 h");
        expect(duracionTexto(90)).toBe("1 h 30 min");
    });
});

describe("tareas en Hoy y Agenda", () => {
    it("intercala las tareas con las órdenes por hora; sin hora, al final", () => {
        const ordenes = [
            { id: 1, hora: "2026-11-05T17:00:00Z" },
            { id: 2, hora: "2026-11-05T21:00:00Z" },
            { id: 3, hora: null },
        ];
        const tareas = [tarea("t1", "2026-11-05T20:00:00Z")];
        expect(
            intercalarTareas(ordenes, (o) => o.hora, tareas).map(
                (e) => e.clave,
            ),
        ).toEqual(["orden-1", "tarea-t1", "orden-2", "orden-3"]);
    });

    it("solo las del día (Guatemala), sin anuladas", () => {
        const tareas = [
            tarea("a", "2026-11-06T05:00:00Z"),
            tarea("b", "2026-11-05T20:00:00Z"),
            tarea("c", "2026-11-05T15:00:00Z", { estado: "anulada" }),
        ];
        // 05:00 UTC del 6 = 23:00 del 5 en Guatemala.
        expect(tareasDelDia(tareas, "2026-11-05").map((t) => t.id)).toEqual([
            "b",
            "a",
        ]);
    });
});

describe("órdenes libres: textos", () => {
    it("distancia, cuándo, subtítulo y el aviso si otro la tomó", () => {
        expect(distanciaTexto(1234)).toBe("1,2 km");
        expect(distanciaTexto(348)).toBe("350 m");
        expect(distanciaTexto(null)).toBeNull();
        expect(cuandoLibre({ programadaInicio: null })).toBe("Sin fecha");
        expect(
            cuandoLibre(
                { programadaInicio: "2026-11-05T22:00:00Z" },
                "2026-11-05",
            ),
        ).toBe("Hoy 16:00");
        expect(subtituloLibres([{ nombre: "Zona Norte" }], "distancia")).toBe(
            "Zona Norte · sin técnico asignado · más cercanas primero",
        );
        expect(avisoYaTomada("OT-000120", "Andrea López")).toBe(
            "OT-000120 ya la tomó Andrea López. La quitamos de la lista.",
        );
    });
});

describe("motor: tareas propias sin conexión", () => {
    const problema = (codigo: string, status = 409): Problema => ({
        type: `urn:iris:problema:${codigo}`,
        title: codigo,
        status,
    });

    it("la clave de grupo de una tarea no choca con órdenes ni traslados", () => {
        const g = grupoTarea("0190f000-0000-7000-8000-000000000001");
        expect(g).toBe(grupoTarea("0190f000-0000-7000-8000-000000000001"));
        expect(esGrupoTarea(g)).toBe(true);
        expect(esGrupoTarea(grupoTraslado(31))).toBe(false);
        expect(g).toBeLessThan(-(2 ** 50) + 1);
    });

    it("la tarea aparece al momento, sale sin ordenId y queda con lo que dice el servidor", async () => {
        const { motor, transporte } = await montar();
        transporte.sinRed = true;
        const crear = await motor.registrarTarea("tarea.crear", {
            que: "Recoger material",
            inicio: "2026-10-28T20:00:00.000Z",
            duracionMin: 30,
        });
        expect(motor.obtenerEstado().tareas).toMatchObject([
            {
                id: crear.id,
                que: "Recoger material",
                estado: "pendiente",
                pendientes: 1,
                fin: "2026-10-28T20:30:00.000Z",
            },
        ]);
        const completar = await motor.registrarTarea("tarea.completar", {
            tareaId: crear.id,
        });
        expect(completar.ordenId).toBe(crear.ordenId);
        expect(motor.obtenerEstado().tareas[0]).toMatchObject({
            estado: "completada",
            pendientes: 2,
        });
        // No se completa dos veces.
        await expect(
            motor.registrarTarea("tarea.anular", { tareaId: crear.id }),
        ).rejects.toMatchObject({ codigo: "tarea_cerrada" });

        transporte.sinRed = false;
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op): ResultadoOperacion => {
                const estado =
                    op.tipo === "tarea.completar" ? "completada" : "pendiente";
                const t = tarea(crear.id, "2026-10-28T20:00:00.000Z", {
                    que: "Recoger material",
                    estado,
                    version: estado === "pendiente" ? 1 : 2,
                });
                return {
                    id: op.id,
                    estado: "aplicada",
                    tarea: { tarea: t, solapaCon: [] },
                };
            }),
        });
        await motor.sincronizar();
        const cable = transporte.lotes
            .flatMap((l) => l.operaciones ?? [])
            .find((o) => o.id === crear.id);
        expect(cable).toEqual({
            id: crear.id,
            tipo: "tarea.crear",
            ocurridoEn: crear.ocurridoEn,
            datos: crear.datos,
        });
        expect(motor.obtenerEstado()).toMatchObject({
            pendientes: 0,
            tareas: [
                {
                    id: crear.id,
                    estado: "completada",
                    version: 2,
                    pendientes: 0,
                },
            ],
        });
    });

    it("un rechazo de una tarea no detiene a otra ni a las órdenes", async () => {
        const { motor, transporte, almacen } = await montar();
        transporte.sinRed = true;
        const mala = await motor.registrarTarea("tarea.crear", {
            que: "Mala",
            inicio: "2026-10-28T20:00:00.000Z",
            duracionMin: 30,
        });
        const buena = await motor.registrarTarea("tarea.crear", {
            que: "Buena",
            inicio: "2026-10-28T21:00:00.000Z",
            duracionMin: 15,
        });
        const orden = await motor.registrar("nota.agregar", 1, {
            texto: "sigue",
        });
        transporte.sinRed = false;
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map(
                (op): ResultadoOperacion =>
                    op.id === mala.id
                        ? {
                              id: op.id,
                              estado: "rechazada",
                              error: problema("tarea_ajena"),
                          }
                        : op.tipo === "tarea.crear"
                          ? {
                                id: op.id,
                                estado: "aplicada",
                                tarea: {
                                    tarea: tarea(
                                        op.id,
                                        "2026-10-28T21:00:00.000Z",
                                    ),
                                    solapaCon: [],
                                },
                            }
                          : transporte.aplicar(op),
            ),
        });
        await motor.sincronizar();
        expect(motor.obtenerEstado().rechazos.map((r) => r.id)).toEqual([
            mala.id,
        ]);
        expect(almacen.operaciones.has(buena.id)).toBe(false);
        expect(almacen.operaciones.has(orden.operacion.id)).toBe(false);
        expect(
            motor.obtenerEstado().tareas.find((t) => t.id === mala.id),
        ).toMatchObject({ rechazada: true });
    });

    it("el paquete y los cambios traen las tareas y si puede tomar órdenes libres", async () => {
        const { motor, transporte } = await montar({}, false);
        const base = transporte.paquete;
        transporte.paquete = (fecha) => ({
            ...base(fecha),
            tareas: [tarea("s1", "2026-10-28T16:00:00Z")],
            tomarLibres: true,
        });
        await motor.cargarPaquete("2026-10-28");
        expect(motor.obtenerEstado()).toMatchObject({
            tomarLibres: true,
            tareas: [{ id: "s1" }],
        });
    });
});
