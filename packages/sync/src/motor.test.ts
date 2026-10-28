import { describe, expect, it } from "vitest";
import type { Problema, ResultadoOperacion } from "./contrato";
import { ErrorSync, ErrorTransporte } from "./errores";
import { DIA_MS, MotorSync } from "./motor";
import { itemDePrueba, ordenDePrueba } from "./pruebas/fabricas";
import { CABLE, FECHA, montar } from "./pruebas/montaje";
import { vaciarPromesas } from "./pruebas/reloj-manual";

function problema(codigo: string, errors: Problema["errors"] = null): Problema {
    return { type: `urn:iris:problema:${codigo}`, title: codigo, status: 422, errors };
}

function rechazo(id: string, codigo: string, errors: Problema["errors"] = null): ResultadoOperacion {
    return { id, estado: "rechazada", error: problema(codigo, errors) };
}

describe("cola local", () => {
    it("persiste la operación antes de enviarla y la envía tal cual", async () => {
        const { motor, almacen, transporte, reloj } = await montar();
        const { operacion } = await motor.registrar("orden.iniciar_camino", 1, { lat: 14.6, lon: -90.5 });

        expect(almacen.operaciones.get(operacion.id)).toMatchObject({ tipo: "orden.iniciar_camino", estado: "pendiente" });
        expect(transporte.lotes).toHaveLength(0);

        await motor.sincronizar();
        expect(transporte.lotes).toEqual([
            {
                dispositivoId: motor.dispositivoId,
                plataforma: "pwa",
                enviadoEn: new Date(reloj.ahora()).toISOString(),
                cursor: "c1",
                operaciones: [
                    {
                        id: operacion.id,
                        tipo: "orden.iniciar_camino",
                        ordenId: 1,
                        versionBase: 1,
                        ocurridoEn: new Date(reloj.ahora()).toISOString(),
                        datos: { lat: 14.6, lon: -90.5 },
                    },
                ],
            },
        ]);
        expect(almacen.operaciones.size).toBe(0);
    });

    it("si no se pudo guardar, no encola ni envía nada", async () => {
        const { motor, almacen, transporte } = await montar();
        almacen.fallarProxima = new Error("QuotaExceededError");
        await expect(motor.registrar("nota.agregar", 1, { texto: "hola" })).rejects.toThrow("QuotaExceededError");
        expect(motor.obtenerEstado().pendientes).toBe(0);
        await motor.sincronizar();
        expect(transporte.lotes).toHaveLength(0);
    });

    it("sobrevive a cerrar y abrir la app (mismo dispositivo, misma cola)", async () => {
        const { motor, transporte, reabrir } = await montar();
        transporte.sinRed = true;
        const { operacion } = await motor.registrar("orden.llegar", 1, {});
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        expect(motor.obtenerEstado().conexion).toBe("sin_conexion");

        const otra = await reabrir();
        expect(otra.dispositivoId).toBe(motor.dispositivoId);
        expect(otra.obtenerEstado().pendientes).toBe(1);
        expect(otra.obtenerOrden(1)?.estado).toBe("en_sitio");
        transporte.sinRed = false;
        await otra.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => o.id)).toEqual([operacion.id]);
        expect(otra.obtenerEstado().pendientes).toBe(0);
    });

    it("usa ids UUID v7 crecientes y respeta el orden de la cola entre órdenes", async () => {
        const { motor, transporte } = await montar();
        const a1 = await motor.registrar("orden.iniciar_camino", 1, {});
        const b1 = await motor.registrar("nota.agregar", 2, { texto: "portón verde" });
        const a2 = await motor.registrar("orden.llegar", 1, {});
        expect(a1.operacion.id < b1.operacion.id && b1.operacion.id < a2.operacion.id).toBe(true);
        await motor.sincronizar();
        expect(transporte.lotes[0]?.operaciones?.map((o) => o.id)).toEqual([a1.operacion.id, b1.operacion.id, a2.operacion.id]);
    });

    it("parte la cola en lotes del tamaño configurado", async () => {
        const { motor, transporte } = await montar({ tamanoLote: 2 });
        for (let i = 0; i < 5; i += 1) {
            await motor.registrar("nota.agregar", 1, { texto: `n${i}` });
        }
        await motor.sincronizar();
        expect(transporte.lotes.map((l) => (l.operaciones ?? []).length)).toEqual([2, 2, 1]);
    });
});

describe("aplicación optimista y reconciliación", () => {
    it("cambia el estado al instante y adopta versión y estado del servidor", async () => {
        const { motor, transporte } = await montar();
        await motor.registrar("orden.iniciar_camino", 1, {});
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "en_camino", version: 1, pendientes: 1 });

        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => ({ id: op.id, estado: "aplicada", orden: { version: 7, estado: "en_camino" } })),
        });
        await motor.sincronizar();
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "en_camino", version: 7, pendientes: 0 });

        await motor.registrar("orden.llegar", 1, {});
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.[0]?.versionBase).toBe(7);
    });

    it("trata `duplicada` como confirmada (idempotencia por id)", async () => {
        const { motor, transporte } = await montar();
        const { operacion } = await motor.registrar("nota.agregar", 1, { texto: "ya llegó antes" });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => ({ id: op.id, estado: "duplicada", orden: { version: 3, estado: "programada" } })),
        });
        await motor.sincronizar();
        const orden = motor.obtenerOrden(1);
        expect(orden?.pendientes).toBe(0);
        expect(orden?.version).toBe(3);
        expect(orden?.notas?.map((n) => n.id)).toEqual([operacion.id]);
    });

    it("deja en la cola lo que el servidor no contestó", async () => {
        const { motor, transporte, almacen } = await montar();
        const a = await motor.registrar("nota.agregar", 1, { texto: "1" });
        const b = await motor.registrar("nota.agregar", 2, { texto: "2" });
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).filter((op) => op.ordenId === 1).map((op) => transporte.aplicar(op)) });
        await motor.sincronizar();
        expect(almacen.operaciones.has(a.operacion.id)).toBe(false);
        // No se repite en la misma pasada: queda para la próxima.
        expect(almacen.operaciones.get(b.operacion.id)).toMatchObject({ estado: "pendiente", intentos: 1 });
        expect(transporte.lotes.map((l) => (l.operaciones ?? []).length)).toEqual([2]);
    });

    it("avisa a los suscriptores y mantiene la instantánea estable sin cambios", async () => {
        const { motor } = await montar();
        const vistos: number[] = [];
        const salir = motor.suscribir((e) => vistos.push(e.pendientes));
        const antes = motor.obtenerEstado();
        expect(motor.obtenerEstado()).toBe(antes);
        await motor.registrar("nota.agregar", 1, { texto: "x" });
        expect(vistos.at(-1)).toBe(1);
        salir();
        await motor.registrar("nota.agregar", 1, { texto: "y" });
        expect(vistos.at(-1)).toBe(1);
    });
});

describe("rechazos (§2)", () => {
    it("un rechazo bloquea solo las siguientes de la misma orden", async () => {
        const { motor, transporte } = await montar();
        const a1 = await motor.registrar("orden.llegar", 1, {});
        const a2 = await motor.registrar("nota.agregar", 1, { texto: "después" });
        const b1 = await motor.registrar("orden.iniciar_camino", 2, {});
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) =>
                op.ordenId === 1 ? rechazo(op.id, op.id === a1.operacion.id ? "transicion_invalida" : "bloqueada") : transporte.aplicar(op),
            ),
        });
        await motor.sincronizar();

        const estado = motor.obtenerEstado();
        expect(estado.rechazos.map((r) => r.id)).toEqual([a1.operacion.id]);
        expect(estado.rechazos[0]?.error?.type).toBe("urn:iris:problema:transicion_invalida");
        expect(estado.pendientes).toBe(1); // a2, retenida y con id nuevo
        expect(motor.obtenerOrden(1)).toMatchObject({ bloqueada: true, rechazos: 1, pendientes: 1 });
        expect(motor.obtenerOrden(2)).toMatchObject({ estado: "en_camino", pendientes: 0 });
        // La vista no muestra lo rechazado: la orden A no está «en sitio».
        expect(motor.obtenerOrden(1)?.estado).toBe("programada");

        // Lo nuevo de B sale; lo de A sigue retenido.
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => transporte.aplicar(op)) });
        await motor.registrar("orden.llegar", 2, {});
        await motor.registrar("nota.agregar", 1, { texto: "otra" });
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => o.ordenId)).toEqual([2]);

        // El técnico rehace: mismo lugar en la cola, id nuevo, versión actual.
        const nueva = await motor.rehacer(a1.operacion.id);
        expect(nueva.id).not.toBe(a1.operacion.id);
        expect(nueva.secuencia).toBe(a1.operacion.secuencia);
        expect(nueva.ocurridoEn).toBe(a1.operacion.ocurridoEn);
        await motor.sincronizar();
        const ultimo = transporte.lotes.at(-1)?.operaciones ?? [];
        expect(ultimo.map((o) => o.tipo)).toEqual(["orden.llegar", "nota.agregar", "nota.agregar"]);
        expect(ultimo[0]?.id).toBe(nueva.id);
        expect(ultimo[1]?.id).not.toBe(a2.operacion.id);
        expect(motor.obtenerEstado().rechazos).toHaveLength(0);
        expect(b1.operacion.ordenId).toBe(2);
    });

    it("descartar quita el rechazo, revierte la vista y libera la cola", async () => {
        const { motor, transporte } = await montar();
        const a1 = await motor.registrar("orden.iniciar_camino", 1, {});
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => rechazo(op.id, "version_vencida")) });
        await motor.sincronizar();
        await motor.registrar("nota.agregar", 1, { texto: "sigue" });
        await expect(motor.descartar("no-existe")).rejects.toBeInstanceOf(ErrorSync);
        await motor.descartar(a1.operacion.id);
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "programada", rechazos: 0, bloqueada: false });

        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((op) => transporte.aplicar(op)) });
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => o.tipo)).toEqual(["nota.agregar"]);
    });

    it("rehacer acepta datos corregidos y no se usa con pendientes", async () => {
        const { motor, transporte } = await montar();
        const op = await motor.registrar("material.usar", 1, { productoId: CABLE, cantidad: 5 });
        await expect(motor.rehacer(op.operacion.id)).rejects.toMatchObject({ codigo: "operacion_no_rechazada" });
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((o) => rechazo(o.id, "validacion")) });
        await motor.sincronizar();
        await motor.rehacer(op.operacion.id, { productoId: CABLE, cantidad: 3 });
        transporte.lote = (p) => ({ resultados: (p.operaciones ?? []).map((o) => transporte.aplicar(o)) });
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.[0]?.datos).toEqual({ productoId: CABLE, cantidad: 3 });
    });
});

describe("conflictos (§3)", () => {
    it("reasignada: lo aplicado se conserva, lo posterior queda rechazado y se puede descartar", async () => {
        const { motor, transporte } = await montar();
        const nota = await motor.registrar("nota.agregar", 1, { texto: "antes de reasignar" });
        const llegar = await motor.registrar("orden.llegar", 1, {});
        const marca = await motor.registrar("checklist.marcar", 1, { itemId: 1, valor: true });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => (op.id === nota.operacion.id ? transporte.aplicar(op) : rechazo(op.id, "orden_reasignada"))),
        });
        await motor.sincronizar();

        const orden = motor.obtenerOrden(1);
        expect(orden?.reasignada).toBe(true);
        expect(orden?.notas?.map((n) => n.id)).toEqual([nota.operacion.id]);
        expect(motor.obtenerEstado().rechazos.map((r) => r.id)).toEqual([llegar.operacion.id, marca.operacion.id]);
        await expect(motor.registrar("nota.agregar", 1, { texto: "x" })).rejects.toMatchObject({ codigo: "orden_reasignada" });

        expect(await motor.descartarOrden(1)).toBe(2);
        expect(motor.obtenerOrden(1)).toBeNull();
    });

    it("reasignada por cambios: se quita si no hay cola; si la hay, se marca y se sigue enviando", async () => {
        const { motor, transporte } = await montar();
        transporte.sinRed = true;
        await motor.registrar("nota.agregar", 2, { texto: "pendiente" });
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        transporte.sinRed = false;
        transporte.cambios = () => ({
            cambios: [
                { tipo: "reasignada", ordenId: 1 },
                { tipo: "reasignada", ordenId: 2 },
            ],
            cursor: "c2",
            hayMas: false,
        });
        transporte.lote = () => ({ resultados: [] });
        await motor.sincronizar();
        expect(motor.obtenerOrden(1)).toBeNull();
        expect(motor.obtenerOrden(2)).toMatchObject({ reasignada: true, pendientes: 1 });
    });

    it("cancelada: se rechaza la acción, pero nota y adjunto pasan como evidencia y no se bloquean", async () => {
        const { motor, transporte } = await montar();
        const llegar = await motor.registrar("orden.llegar", 1, {});
        const nota = await motor.registrar("nota.agregar", 1, { texto: "el cliente no estaba" });
        // Despacho canceló la orden mientras el técnico iba sin señal.
        transporte.ordenes.set(1, { version: 4, estado: "cancelada" });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) =>
                op.tipo === "nota.agregar" ? transporte.aplicar(op) : { ...rechazo(op.id, "orden_cancelada"), orden: { version: 4, estado: "cancelada" } },
            ),
        });
        await motor.sincronizar();
        expect(motor.obtenerEstado().rechazos.map((r) => r.id)).toEqual([llegar.operacion.id]);
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "cancelada", bloqueada: false });
        expect(motor.obtenerOrden(1)?.notas?.map((n) => n.id)).toEqual([nota.operacion.id]);

        await expect(motor.registrar("orden.cerrar", 1, { resultado: "exitosa" })).rejects.toMatchObject({ codigo: "orden_cancelada" });
        const otra = await motor.registrar("nota.agregar", 1, { texto: "foto del portón cerrado" });
        await motor.sincronizar();
        expect(transporte.lotes.at(-1)?.operaciones?.map((o) => o.id)).toEqual([otra.operacion.id]);
    });

    it("cancelada por cambios: la vista lo muestra y solo acepta evidencia", async () => {
        const { motor, transporte } = await montar();
        transporte.cambios = () => ({
            cambios: [{ tipo: "cancelada", ordenId: 1, orden: ordenDePrueba(1, { estado: "cancelada", version: 2 }) }],
            cursor: "c2",
            hayMas: false,
        });
        await motor.sincronizar();
        expect(motor.obtenerOrden(1)?.estado).toBe("cancelada");
        await expect(motor.registrar("orden.llegar", 1, {})).rejects.toBeInstanceOf(ErrorSync);
        await expect(motor.registrar("nota.agregar", 1, { texto: "ok" })).resolves.toBeTruthy();
    });

    it("checklist: gana la marca más reciente por ocurridoEn (la del panel, posterior, gana)", async () => {
        const { motor, transporte, reloj } = await montar();
        transporte.sinRed = true;
        // El técnico marca «no» a las 15:00 sin señal.
        await motor.registrar("checklist.marcar", 1, { itemId: 1, valor: false });
        expect(motor.obtenerOrden(1)?.checklist?.[0]).toMatchObject({ valor: false, marcadoPor: null });
        // Despacho marcó «sí» a las 15:10 desde el panel; llega por cambios (la API guarda el historial).
        transporte.sinRed = false;
        transporte.lote = () => ({ resultados: [] });
        transporte.cambios = () => ({
            cambios: [
                {
                    tipo: "actualizada",
                    ordenId: 1,
                    orden: ordenDePrueba(1, {
                        version: 2,
                        checklist: [itemDePrueba(1, { valor: true as never, ocurridoEn: "2026-10-28T15:10:00.000Z", marcadoPor: 9 })],
                    }),
                },
            ],
            cursor: "c2",
            hayMas: false,
        });
        await motor.sincronizar();
        const item = motor.obtenerOrden(1)?.checklist?.[0];
        expect(item?.valor).toBe(true);
        expect(item?.marcadoPor).toBe(9);

        // Una marca posterior del técnico sí gana en la vista.
        reloj.fijar(Date.parse("2026-10-28T15:20:00Z"));
        await motor.registrar("checklist.marcar", 1, { itemId: 1, valor: false });
        expect(motor.obtenerOrden(1)?.checklist?.[0]?.valor).toBe(false);
    });

    it("faltan_adjuntos: espera a que terminen las subidas y reintenta solo con id nuevo", async () => {
        const { motor, transporte, subidor } = await montar({ tamanoParte: 4 });
        const { adjunto } = await motor.agregarAdjunto({
            ordenId: 1,
            tipo: "foto",
            datos: new Uint8Array(10).fill(7),
            mime: "image/jpeg",
            obligatorio: false,
            item: { itemId: 3, codigo: "fachada" },
        });
        const cerrar = await motor.registrar("orden.cerrar", 1, { resultado: "exitosa" });
        // Se crea la subida pero la señal se corta en la primera parte.
        subidor.cortarTrasPartes = 0;
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        expect(transporte.lotes).toHaveLength(0);

        // Con la subida a medias, el servidor rechaza el cierre: falta la foto de fachada.
        let rechazos = 0;
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => {
                if (op.tipo === "orden.cerrar" && !subidor.completo(adjunto.id)) {
                    rechazos += 1;
                    return rechazo(op.id, "faltan_adjuntos", [{ field: "adjuntos.foto:fachada", message: "Falta la foto de fachada" }]);
                }
                return transporte.aplicar(op);
            }),
        });
        await motor.sincronizar();

        expect(rechazos).toBe(1);
        const [primero, segundo] = transporte.lotes;
        expect(primero?.operaciones?.map((o) => o.tipo)).toEqual(["adjunto.registrar", "orden.cerrar"]);
        expect(segundo?.operaciones?.map((o) => o.tipo)).toEqual(["orden.cerrar"]);
        expect(segundo?.operaciones?.[0]?.id).not.toBe(cerrar.operacion.id);
        expect(segundo?.operaciones?.[0]?.ocurridoEn).toBe(cerrar.operacion.ocurridoEn);
        expect(subidor.completo(adjunto.id)).toBe(true);
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "cerrada", pendientes: 0, esperandoAdjuntos: false });
        expect(motor.obtenerEstado().rechazos).toHaveLength(0);
    });

    it("faltan_adjuntos sin nada que subir queda como rechazo para el técnico", async () => {
        const { motor, transporte } = await montar();
        await motor.registrar("orden.cerrar", 1, { resultado: "exitosa" });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) => rechazo(op.id, "faltan_adjuntos", [{ field: "adjuntos.firma", message: "Falta la firma" }])),
        });
        await motor.sincronizar();
        expect(motor.obtenerEstado().rechazos).toHaveLength(1);
        expect(motor.obtenerEstado().esperandoAdjuntos).toHaveLength(0);
    });

    it("material mayor que la existencia: avisa, se acepta y la existencia queda en negativo", async () => {
        const { motor, almacen } = await montar();
        const normal = await motor.registrar("material.usar", 2, { productoId: CABLE, cantidad: 20 });
        expect(normal.avisos).toEqual([]);
        await motor.sincronizar();
        const { avisos } = await motor.registrar("material.usar", 1, { productoId: CABLE, cantidad: 60 });
        expect(avisos).toEqual(["material_excede_existencia"]);
        const cable = (e: { bodegas: { lineas: { cantidad: number }[] | null }[] | null } | null) => e?.bodegas?.[0]?.lineas?.[0]?.cantidad;
        expect(cable(motor.obtenerEstado().existencias)).toBe(-30);

        await motor.sincronizar();
        expect(motor.obtenerEstado().pendientes).toBe(0);
        expect(cable(motor.obtenerEstado().existencias)).toBe(-30);
        expect(cable(almacen.meta.existencias)).toBe(-30);
        expect(motor.obtenerOrden(1)?.materiales).toHaveLength(1);
    });
});

describe("reloj", () => {
    it("mide el desfase, manda ocurridoEn y enviadoEn con el reloj del teléfono y corrige tomadoEn", async () => {
        const { motor, transporte, reloj, almacen, subidor } = await montar();
        transporte.horaServidor = reloj.ahora() + 120_000; // el teléfono va 2 min atrasado
        await motor.cargarPaquete(FECHA);
        expect(motor.obtenerEstado().desfaseMs).toBe(120_000);
        expect(almacen.meta.desfaseMs).toBe(120_000);
        const { operacion } = await motor.registrar("orden.llegar", 1, {});
        // Sin corregir: la API mide `servidor − enviadoEn` y corrige (I2).
        expect(operacion.ocurridoEn).toBe(new Date(reloj.ahora()).toISOString());
        reloj.fijar(reloj.ahora() + 5000);
        const { adjunto } = await motor.agregarAdjunto({
            ordenId: 1,
            tipo: "foto",
            datos: new Uint8Array(4),
            mime: "image/jpeg",
            tomadoEn: "2026-10-28T14:00:00.000Z",
        });
        transporte.horaServidor = reloj.ahora() + 120_000;
        await motor.sincronizar();
        const lote = transporte.lotes.find((l) => l.operaciones?.some((o) => o.id === operacion.id));
        expect(lote?.enviadoEn).toBe(new Date(reloj.ahora()).toISOString());
        expect(lote?.operaciones?.[0]?.ocurridoEn).toBe(operacion.ocurridoEn);
        expect(subidor.subidas.get(adjunto.id)?.entrada.tomadoEn).toBe("2026-10-28T14:02:00.000Z");
    });

    it("reintenta con espera exponencial y jitter, y vuelve al intervalo tras el éxito", async () => {
        const { motor, transporte, reloj } = await montar({
            espera: { baseMs: 1000, maximoMs: 60_000, azar: () => 0.5 },
            intervaloMs: 30_000,
        });
        transporte.fallar(3);
        await motor.registrar("orden.iniciar_camino", 1, {});
        motor.iniciar();
        await vaciarPromesas();
        expect(motor.obtenerEstado()).toMatchObject({ conexion: "sin_conexion", pendientes: 1 });
        expect(motor.obtenerEstado().proximoIntento).toBe(reloj.ahora() + 750);

        // Una acción nueva durante la espera no adelanta el reintento.
        await motor.registrar("orden.llegar", 1, {});
        await vaciarPromesas();
        expect(transporte.lotes).toHaveLength(1); // solo el intento fallido

        await reloj.avanzar(750);
        await reloj.avanzar(1500);
        await reloj.avanzar(3000);
        expect(reloj.esperas).toEqual([750, 1500, 3000, 30_000]);
        expect(motor.obtenerEstado()).toMatchObject({ conexion: "en_linea", pendientes: 0, proximoIntento: null });
        const enviados = transporte.lotes.at(-1)?.operaciones?.map((o) => o.tipo);
        expect(enviados).toEqual(["orden.iniciar_camino", "orden.llegar"]);
        motor.detener();
    });

    it("al volver la señal reintenta enseguida", async () => {
        const { motor, transporte, reloj } = await montar({ espera: { baseMs: 10_000, maximoMs: 60_000, azar: () => 0 } });
        transporte.fallar(1);
        await motor.registrar("nota.agregar", 1, { texto: "x" });
        motor.iniciar();
        await vaciarPromesas();
        motor.establecerConexion(false);
        expect(motor.obtenerEstado().conexion).toBe("sin_conexion");
        motor.establecerConexion(true);
        await vaciarPromesas();
        expect(motor.obtenerEstado().pendientes).toBe(0);
        expect(reloj.ahora()).toBe(Date.parse("2026-10-28T15:00:00Z"));
        motor.detener();
    });

    it("sin sesión (401) no acelera los reintentos y lo informa", async () => {
        const { motor, transporte, reloj } = await montar({ intervaloMs: 45_000 });
        transporte.fallar(2, new ErrorTransporte("http", "Sin sesión", 401));
        await motor.registrar("nota.agregar", 1, { texto: "x" });
        motor.iniciar();
        await vaciarPromesas();
        expect(motor.obtenerEstado().conexion).toBe("sin_sesion");
        await reloj.avanzar(45_000);
        expect(reloj.esperas).toEqual([45_000, 45_000]);
        motor.detener();
    });
});

describe("paquete del día y cambios", () => {
    it("guarda el paquete en orden de ruta y quita lo viejo sin cola", async () => {
        const { motor, transporte } = await montar();
        expect(motor.obtenerEstado().ordenes.map((o) => o.id)).toEqual([2, 1]);
        expect(motor.obtenerEstado()).toMatchObject({ fecha: FECHA, caducado: false });

        transporte.sinRed = true;
        await motor.registrar("nota.agregar", 1, { texto: "pendiente de ayer" });
        transporte.sinRed = false;
        transporte.paquete = (fecha) => ({
            fecha,
            servidorEn: "2026-10-29T12:00:00Z",
            ordenes: [ordenDePrueba(3)],
            existencias: { disponible: false, motivo: "sin_conector", bodegas: [] },
            cursor: "d1",
        });
        await motor.cargarPaquete("2026-10-29");
        expect(transporte.llamadas.at(-1)).toEqual({ tipo: "paquete", fecha: "2026-10-29" });
        expect(motor.obtenerEstado().ordenes.map((o) => o.id).sort()).toEqual([1, 3]);
    });

    it("pide cambios por cursor, página por página, y guarda el último cursor", async () => {
        const { motor, transporte, almacen } = await montar();
        const paginas: Record<string, ReturnType<typeof transporte.cambios>> = {
            c1: { cambios: [{ tipo: "nueva", ordenId: 3, orden: ordenDePrueba(3) }], cursor: "c2", hayMas: true },
            c2: {
                cambios: [{ tipo: "reprogramada", ordenId: 1, orden: ordenDePrueba(1, { version: 5, programadaInicio: "2026-10-28T20:00:00Z" }) }],
                cursor: "c3",
                hayMas: false,
            },
        };
        transporte.cambios = (cursor) => paginas[cursor] ?? { cambios: [], cursor, hayMas: false };
        await motor.sincronizar();
        expect(transporte.llamadas.map((l) => (l.tipo === "cambios" ? l.cursor : l.tipo))).toEqual(["c1", "c2"]);
        expect(almacen.meta.cursor).toBe("c3");
        expect(motor.obtenerOrden(3)).not.toBeNull();
        expect(motor.obtenerOrden(1)).toMatchObject({ version: 5, programadaInicio: "2026-10-28T20:00:00Z" });
    });

    it("caduca a los 7 días sin sincronizar: borra lo del servidor y conserva la cola", async () => {
        const { motor, transporte, reloj, reabrir } = await montar();
        transporte.sinRed = true;
        await motor.registrar("nota.agregar", 1, { texto: "trabajo sin enviar" });

        reloj.fijar(reloj.ahora() + 7 * DIA_MS);
        const aTiempo = await reabrir();
        expect(aTiempo.obtenerEstado().caducado).toBe(false);
        expect(aTiempo.obtenerEstado().ordenes).toHaveLength(2);

        reloj.fijar(reloj.ahora() + 1);
        const vencido = await reabrir();
        const estado = vencido.obtenerEstado();
        expect(estado).toMatchObject({ caducado: true, fecha: null, ruta: null, existencias: null, pendientes: 1 });
        expect(estado.ordenes.map((o) => o.id)).toEqual([1]);

        transporte.sinRed = false;
        await vencido.cargarPaquete(FECHA);
        await vencido.sincronizar();
        expect(vencido.obtenerEstado()).toMatchObject({ caducado: false, pendientes: 0 });
    });

    it("sin paquete no hay cambios que pedir", async () => {
        const { motor, transporte } = await montar({}, false);
        await motor.sincronizar();
        expect(transporte.llamadas).toEqual([]);
        await expect(motor.registrar("orden.llegar", 99, {})).rejects.toMatchObject({ codigo: "orden_desconocida" });
    });
});

describe("cola de adjuntos", () => {
    it("sube en partes, se reanuda tras un corte (HEAD) y cerrar espera a los obligatorios", async () => {
        const { motor, transporte, subidor, almacen, reabrir } = await montar({ tamanoParte: 4 });
        const datos = Uint8Array.from({ length: 10 }, (_, i) => i);
        const { adjunto, operacion: registrar } = await motor.agregarAdjunto({
            ordenId: 1,
            tipo: "firma",
            datos,
            mime: "image/png",
            obligatorio: true,
        });
        expect(adjunto.sha256).toMatch(/^[0-9a-f]{64}$/);
        expect(almacen.bytes.has(adjunto.id)).toBe(true);
        await motor.registrar("orden.cerrar", 1, { resultado: "exitosa" });
        expect(motor.obtenerOrden(1)).toMatchObject({ estado: "cerrada", subidasPendientes: 1 });

        subidor.cortarTrasPartes = 1;
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        expect(transporte.lotes).toHaveLength(0); // ni el registro (sin crear) ni el cierre
        expect(almacen.adjuntos.get(adjunto.id)).toMatchObject({ estado: "subiendo", offset: 4 });

        // La app se cierra y se vuelve a abrir: se reanuda desde lo que el servidor confirma.
        const otra = await reabrir({ tamanoParte: 4 });
        subidor.llamadas.length = 0;
        await otra.sincronizar();
        expect(subidor.llamadas).toEqual([`HEAD ${adjunto.id}`, `PATCH ${adjunto.id} 4`, `PATCH ${adjunto.id} 8`]);
        expect(subidor.subidas.get(adjunto.id)?.recibido).toEqual(datos);
        expect(transporte.lotes.map((l) => l.operaciones?.map((o) => o.tipo))).toEqual([["adjunto.registrar"], ["orden.cerrar"]]);
        expect(transporte.lotes[0]?.operaciones?.[0]?.id).toBe(registrar.id);
        expect(almacen.bytes.has(adjunto.id)).toBe(false);
        expect(otra.obtenerEstado().subidas).toEqual({ pendientes: 0, bytesPendientes: 0, conError: 0, rechazadas: 0 });
    });

    it("un error definitivo de un archivo queda visible y no detiene a los demás", async () => {
        const { motor, subidor } = await montar();
        const malo = await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: new Uint8Array(3), mime: "image/jpeg" });
        const bueno = await motor.agregarAdjunto({ ordenId: 2, tipo: "foto", datos: new Uint8Array(3), mime: "image/jpeg" });
        const crear = subidor.crear.bind(subidor);
        subidor.crear = (entrada) =>
            entrada.id === malo.adjunto.id ? Promise.reject(new ErrorTransporte("http", "Archivo muy grande", 413)) : crear(entrada);
        await motor.sincronizar();
        expect(subidor.completo(bueno.adjunto.id)).toBe(true);
        expect(motor.obtenerEstado().subidas).toMatchObject({ pendientes: 1, conError: 1 });
        expect(motor.obtenerOrden(2)?.pendientes).toBe(0);
        expect(motor.obtenerOrden(1)?.pendientes).toBe(1);
    });

    it("descartar el registro rechazado borra el archivo que no se subió", async () => {
        const { motor, transporte, almacen, subidor, reloj } = await montar();
        subidor.sinRed = true;
        const { adjunto, operacion } = await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: new Uint8Array(2), mime: "image/jpeg" });
        // Simula un registro rechazado (p. ej. tras crear la subida en otro intento).
        await almacen.escribir({ operaciones: { poner: [{ ...operacion, estado: "rechazada", error: problema("validacion") }] } });
        const otra = await MotorSync.abrir({ almacen, transporte, subidor, reloj });
        expect(otra.obtenerEstado().rechazos.map((o) => o.id)).toEqual([operacion.id]);
        await otra.descartar(operacion.id);
        expect(almacen.adjuntos.has(adjunto.id)).toBe(false);
        expect(almacen.bytes.has(adjunto.id)).toBe(false);
        expect(otra.obtenerOrden(1)?.adjuntos).toEqual([]);
        expect(motor.dispositivoId).toBe(otra.dispositivoId);
    });
});
