// Reglas que dependen de la forma exacta de la API de I2 (lote) y de I1 (adjuntos).
import { describe, expect, it } from "vitest";
import type { Problema, ResultadoOperacion } from "./contrato";
import { ErrorTransporte, errorSesionAjena } from "./errores";
import { cubreFaltante } from "./motor";
import { itemDePrueba, ordenDePrueba } from "./pruebas/fabricas";
import { CABLE, montar, paqueteDePrueba } from "./pruebas/montaje";
import { vaciarPromesas } from "./pruebas/reloj-manual";

const problema = (codigo: string, status = 422, errors: Problema["errors"] = null): Problema => ({
    type: `urn:iris:problema:${codigo}`,
    title: codigo,
    status,
    errors,
});
const rechazo = (id: string, codigo: string): ResultadoOperacion => ({ id, estado: "rechazada", error: problema(codigo) });

describe("lote (I2)", () => {
    it("bloqueada_por_rechazo vuelve a la cola detrás del rechazo que la causó", async () => {
        const { motor, transporte } = await montar();
        const llegar = await motor.registrar("orden.llegar", 1, {});
        await motor.registrar("nota.agregar", 1, { texto: "después" });
        transporte.lote = (p) => ({
            resultados: (p.operaciones ?? []).map((op) =>
                op.id === llegar.operacion.id ? rechazo(op.id, "transicion_invalida") : rechazo(op.id, "bloqueada_por_rechazo"),
            ),
        });
        await motor.sincronizar();
        const estado = motor.obtenerEstado();
        expect(estado.rechazos.map((r) => r.id)).toEqual([llegar.operacion.id]);
        expect(estado.pendientes).toBe(1);
        expect(motor.obtenerOrden(1)).toMatchObject({ bloqueada: true });
    });

    it("409 dispositivo_ajeno: no regenera el dispositivo; sin sesión, cola intacta y bucle detenido", async () => {
        const { motor, transporte, almacen, reloj } = await montar();
        const antes = motor.dispositivoId;
        transporte.fallar(1, new ErrorTransporte("http", "Dispositivo ajeno", 409, problema("dispositivo_ajeno", 409)));
        const { operacion } = await motor.registrar("nota.agregar", 1, { texto: "de A" });
        motor.iniciar();
        await vaciarPromesas();
        expect(motor.obtenerEstado()).toMatchObject({ conexion: "sin_sesion", pendientes: 1, proximoIntento: null });
        expect(motor.dispositivoId).toBe(antes);
        expect(almacen.meta.dispositivoId).toBe(antes);
        expect(almacen.operaciones.get(operacion.id)).toMatchObject({ estado: "pendiente" });
        // El bucle se detuvo: aunque pase el tiempo no reintenta.
        await reloj.avanzar(10 * 60_000);
        expect(transporte.lotes).toHaveLength(1);
        motor.detener();
    });

    it("verificarIdentidad: con la sesión de otra persona no envía, no sube, no baja nada", async () => {
        let duena = true;
        const { motor, transporte, subidor } = await montar({
            verificarIdentidad: () => (duena ? Promise.resolve() : Promise.reject(errorSesionAjena())),
        });
        await motor.registrar("nota.agregar", 1, { texto: "de A" });
        await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: new Uint8Array(4), mime: "image/jpeg" });
        transporte.llamadas.length = 0;
        duena = false;
        await expect(motor.sincronizar()).rejects.toMatchObject({ codigo: "sesion_ajena" });
        await expect(motor.cargarPaquete("2026-10-28")).rejects.toMatchObject({ codigo: "sesion_ajena" });
        expect(transporte.llamadas).toEqual([]);
        expect(subidor.llamadas).toEqual([]);
        expect(motor.obtenerEstado()).toMatchObject({ conexion: "sin_sesion", pendientes: 2 });
        // Vuelve el dueño: se envía todo.
        duena = true;
        await motor.sincronizar();
        expect(motor.obtenerEstado().pendientes).toBe(0);
    });

    it("parte el lote por bytes (firma SVG grande)", async () => {
        const { motor, transporte } = await montar({ bytesLote: 1400 });
        const svg = `<svg>${"M1 1L2 2".repeat(80)}</svg>`;
        await motor.registrar("orden.llegar", 1, {});
        await motor.registrar("firma.capturar", 1, { nombre: "Ana", svg });
        await motor.registrar("firma.capturar", 1, { nombre: "Ana", svg });
        await motor.sincronizar();
        expect(transporte.lotes.map((l) => l.operaciones?.length)).toEqual([2, 1]);
    });

    it("problemas de empresa dejan la conexión «sin_empresa»", async () => {
        const { motor, transporte } = await montar();
        transporte.fallar(1, new ErrorTransporte("http", "Empresa", 403, problema("empresa_no_permitida", 403)));
        await motor.registrar("nota.agregar", 1, { texto: "x" });
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        expect(motor.obtenerEstado().conexion).toBe("sin_empresa");
    });

    it("existencias: descuenta de la bodega de la orden", async () => {
        const { motor, transporte } = await montar();
        transporte.paquete = (fecha) => ({
            ...paqueteDePrueba(fecha),
            ordenes: [ordenDePrueba(1, { bodegaId: 8 }), ordenDePrueba(2)],
            existencias: {
                disponible: true,
                motivo: null,
                bodegas: [
                    { bodegaId: 7, lineas: [{ productoId: CABLE, descripcion: "Cable", unidad: "m", cantidad: 50 }] },
                    { bodegaId: 8, lineas: [{ productoId: CABLE, descripcion: "Cable", unidad: "m", cantidad: 5 }] },
                ],
            },
        });
        await motor.cargarPaquete("2026-10-28");
        const { avisos } = await motor.registrar("material.usar", 1, { productoId: CABLE, cantidad: 6 });
        expect(avisos).toEqual(["material_excede_existencia"]);
        const bodegas = motor.obtenerEstado().existencias?.bodegas ?? [];
        expect(bodegas.map((b) => b.lineas?.[0]?.cantidad)).toEqual([50, -1]);
    });
});

describe("adjuntos (I1)", () => {
    it("claves de faltan_adjuntos: firma, foto:<código> y el id", () => {
        const foto = { id: "f1", tipo: "foto" as const, claveChecklist: "fachada" };
        expect(cubreFaltante(foto, "foto:fachada")).toBe(true);
        expect(cubreFaltante(foto, "foto:antena")).toBe(false);
        expect(cubreFaltante(foto, "firma")).toBe(false);
        expect(cubreFaltante(foto, "f1")).toBe(true);
        expect(cubreFaltante({ id: "f2", tipo: "firma", claveChecklist: null }, "firma")).toBe(true);
    });

    it("el alta va con entidad orden, claveChecklist y la marca del checklist espera al alta", async () => {
        const { motor, transporte, subidor } = await montar();
        transporte.paquete = (fecha) => ({
            ...paqueteDePrueba(fecha),
            ordenes: [ordenDePrueba(1, { estado: "en_sitio", checklist: [itemDePrueba(4, { codigo: "fachada", tipo: "foto", obligatorio: true })] })],
        });
        await motor.cargarPaquete("2026-10-28");
        subidor.sinRed = true;
        const { adjunto } = await motor.agregarAdjunto({
            ordenId: 1,
            tipo: "foto",
            datos: new Uint8Array(6).fill(1),
            mime: "image/jpeg",
            obligatorio: true,
            item: { itemId: 4, codigo: "fachada" },
            lat: 14.6,
            lon: -90.5,
        });
        await motor.registrar("checklist.marcar", 1, { itemId: 4, valor: adjunto.id });
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        expect(transporte.lotes).toHaveLength(0);

        subidor.sinRed = false;
        await motor.sincronizar();
        expect(subidor.subidas.get(adjunto.id)?.entrada).toMatchObject({
            entidad: "orden",
            entidadId: "1",
            tipo: "foto",
            claveChecklist: "fachada",
            lat: 14.6,
            lon: -90.5,
        });
        expect(transporte.lotes[0]?.operaciones?.map((o) => [o.tipo, o.datos])).toEqual([
            ["adjunto.registrar", { adjuntoId: adjunto.id, tipo: "foto", itemId: 4 }],
            ["checklist.marcar", { itemId: 4, valor: adjunto.id }],
        ]);
    });

    it("409 offset_distinto: sigue desde el offset del servidor", async () => {
        const { motor, subidor } = await montar({ tamanoParte: 4 });
        const { adjunto } = await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: Uint8Array.from({ length: 8 }, (_, i) => i), mime: "image/jpeg" });
        subidor.cortarTrasPartes = 1;
        await expect(motor.sincronizar()).rejects.toBeInstanceOf(ErrorTransporte);
        // El servidor ya recibió 4; el teléfono cree que 0 (perdió la respuesta).
        const enviar = subidor.enviarParte.bind(subidor);
        let primero = true;
        subidor.enviarParte = (id, offset, parte) => {
            if (primero) {
                primero = false;
                return enviar(id, 0, parte);
            }
            return enviar(id, offset, parte);
        };
        subidor.consultarOffset = () => Promise.resolve(0);
        await motor.sincronizar();
        expect(subidor.completo(adjunto.id)).toBe(true);
    });

    it("rechazado por la API: queda visible; rehacer usa otro id y reescribe la cola", async () => {
        const { motor, subidor, transporte } = await montar();
        subidor.rechazarAlta = new ErrorTransporte("http", "Tipo no permitido", 422, problema("mime_no_permitido"));
        const { adjunto } = await motor.agregarAdjunto({ ordenId: 1, tipo: "firma", datos: new Uint8Array(3), mime: "image/png" });
        await motor.registrar("firma.capturar", 1, { nombre: "Ana López", svg: "<svg></svg>", adjuntoId: adjunto.id });
        await motor.registrar("nota.agregar", 1, { texto: "sigue" });
        await motor.sincronizar();
        expect(motor.obtenerEstado().adjuntos.find((a) => a.id === adjunto.id)).toMatchObject({
            estado: "rechazado",
            problema: { type: "urn:iris:problema:mime_no_permitido" },
        });
        expect(motor.obtenerEstado().subidas.rechazadas).toBe(1);
        expect(transporte.lotes).toHaveLength(0); // registrar retenido: retiene su orden

        const nuevo = await motor.rehacerAdjunto(adjunto.id);
        expect(nuevo.id).not.toBe(adjunto.id);
        await motor.sincronizar();
        const tipos = transporte.lotes.flatMap((l) => l.operaciones ?? []).map((o) => [o.tipo, (o.datos as { adjuntoId?: string }).adjuntoId]);
        expect(tipos).toEqual([
            ["adjunto.registrar", nuevo.id],
            ["firma.capturar", nuevo.id],
            ["nota.agregar", undefined],
        ]);
        expect(subidor.completo(nuevo.id)).toBe(true);
    });

    it("descartar un adjunto rechazado quita lo que lo usa y libera la orden", async () => {
        const { motor, subidor, transporte } = await montar();
        subidor.rechazarAlta = new ErrorTransporte("http", "Muy grande", 413, problema("tamano_excedido", 413));
        const { adjunto } = await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: new Uint8Array(3), mime: "image/jpeg" });
        await motor.registrar("nota.agregar", 1, { texto: "sigue" });
        await motor.sincronizar();
        await motor.descartarAdjunto(adjunto.id);
        await motor.sincronizar();
        expect(transporte.lotes.flatMap((l) => l.operaciones ?? []).map((o) => o.tipo)).toEqual(["nota.agregar"]);
        expect(motor.obtenerEstado().adjuntos).toEqual([]);
    });
});

describe("cerrar sesión con pendientes", () => {
    it("olvidarDatosDelServidor deja solo la cola y los adjuntos pendientes", async () => {
        const { motor, almacen, transporte, reabrir } = await montar();
        transporte.sinRed = true;
        const { operacion } = await motor.registrar("nota.agregar", 1, { texto: "sin enviar" });
        const { adjunto } = await motor.agregarAdjunto({ ordenId: 1, tipo: "foto", datos: new Uint8Array(3), mime: "image/jpeg" });
        await motor.olvidarDatosDelServidor();
        expect(almacen.ordenes.size).toBe(0);
        expect(almacen.meta).toMatchObject({ existencias: null, ruta: null, fecha: null, cursor: null });
        expect(almacen.operaciones.has(operacion.id)).toBe(true);
        expect(almacen.bytes.has(adjunto.id)).toBe(true);
        const otra = await reabrir();
        expect(otra.obtenerEstado()).toMatchObject({ ordenes: [], pendientes: 2 });
        // Al volver el dueño, la cola se envía aunque la orden ya no esté en el teléfono.
        transporte.sinRed = false;
        await otra.sincronizar();
        expect(otra.obtenerEstado().pendientes).toBe(0);
    });
});
