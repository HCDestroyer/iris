import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { consultaDeFiltrosOrdenes, FILTROS_ORDENES_INICIALES, hayFiltrosOrdenes, rangoDiaGuatemala } from "./filtros";
import { describirEventoOrden, puntosGps, type EventoOrden } from "./historial";
import {
    COLUMNAS,
    columnaDe,
    ESTADOS_ORDEN,
    ESTADOS_PENDIENTE,
    ORDEN_ESTADOS_ORDEN,
    reintentable,
    resumirError,
    resumirPendientes,
    textoMarcaPendiente,
    textoOperacion,
    type Pendiente,
} from "./presentacion";

const enumEstado = (s: Schema<"Orden">["estado"]) => s;

function pendiente(p: Partial<Pendiente>): Pendiente {
    return {
        id: 1,
        sistema: "argosisp",
        entidad: "orden",
        entidadId: "9",
        operacion: "consumo",
        metodo: "POST",
        ruta: "/inventario/consumos",
        payload: {},
        idempotencyKey: "iris:consumo:9",
        estado: "pendiente",
        intentos: 0,
        proximoIntento: null,
        ultimoError: null,
        ultimoStatus: null,
        dependeDe: null,
        datosIncompletos: false,
        conflicto: null,
        conflictoEn: null,
        payloadConflicto: null,
        hechoEn: null,
        version: 1,
        creadoEn: "2026-10-28T15:00:00Z",
        ...p,
    };
}

describe("estados de la orden", () => {
    it("todo estado tiene icono, texto y tono; ninguno usa solo color", () => {
        for (const e of ORDEN_ESTADOS_ORDEN) {
            expect(ESTADOS_ORDEN[e].texto.length).toBeGreaterThan(0);
            expect(ESTADOS_ORDEN[e].icono).toBeTruthy();
        }
        expect(ESTADOS_ORDEN.cerrada.tono).toBe("success");
        expect(ESTADOS_ORDEN.fallida.tono).toBe("error");
    });

    it("cada estado cae en una sola columna; los finales en la última", () => {
        for (const e of ORDEN_ESTADOS_ORDEN) {
            expect(COLUMNAS.filter((c) => c.estados.includes(e))).toHaveLength(1);
        }
        expect(columnaDe(enumEstado("cancelada")).id).toBe("finalizada");
        expect(columnaDe(enumEstado("en_sitio")).id).toBe("en_sitio");
    });
});

describe("pendientes en Argos", () => {
    it("sin pendientes abiertos no hay marca", () => {
        expect(resumirPendientes([])).toBeUndefined();
        expect(resumirPendientes([pendiente({ estado: "hecho" })])).toBeUndefined();
    });

    it("la marca muestra el peor estado y cuántos", () => {
        const r = resumirPendientes([pendiente({ estado: "pendiente" }), pendiente({ id: 2, estado: "fallido_negocio" }), pendiente({ id: 3, estado: "hecho" })]);
        expect(r).toEqual({ peor: "fallido_negocio", cuantos: 2, conflicto: false });
        expect(r && textoMarcaPendiente(r)).toBe("2 pendientes en Argos · rechazado");
        const uno = resumirPendientes([pendiente({ estado: "bloqueado" })]);
        expect(uno && textoMarcaPendiente(uno)).toBe("Pendiente en Argos · bloqueado");
    });

    it("un conflicto en un pendiente hecho también se marca", () => {
        const r = resumirPendientes([pendiente({ estado: "hecho", conflicto: "cierre_repetido_con_otros_datos" })]);
        expect(r).toEqual({ peor: "hecho", cuantos: 0, conflicto: true });
        expect(r && textoMarcaPendiente(r)).toBe("Conflicto en Argos");
    });

    it("reintentable como la API: fallido, agotado o pendiente con datos completos", () => {
        expect(reintentable(pendiente({ estado: "fallido_negocio" }))).toBe(true);
        expect(reintentable(pendiente({ estado: "agotado" }))).toBe(true);
        expect(reintentable(pendiente({ estado: "bloqueado" }))).toBe(false);
        expect(reintentable(pendiente({ estado: "en_curso" }))).toBe(false);
        expect(reintentable(pendiente({ estado: "fallido_negocio", datosIncompletos: true }))).toBe(false);
    });

    it("resume el último error y traduce operaciones y bloqueos", () => {
        expect(resumirError(null)).toBeUndefined();
        expect(resumirError("depende_de_12_fallido")).toBe("Espera al pendiente 12, que falló");
        const largo = resumirError(`Argos: ${"x".repeat(300)}`, 40);
        expect(largo).toHaveLength(40);
        expect(largo?.endsWith("…")).toBe(true);
        expect(textoOperacion("activar_instalacion")).toBe("Activar la instalación");
        expect(textoOperacion("algo_nuevo")).toBe("algo nuevo");
        expect(ESTADOS_PENDIENTE.agotado.tono).toBe("error");
    });
});

describe("filtros del tablero", () => {
    it("día de Guatemala a rango UTC", () => {
        expect(rangoDiaGuatemala("2026-10-28")).toEqual({ desde: "2026-10-28T06:00:00Z", hasta: "2026-10-29T06:00:00Z" });
        expect(rangoDiaGuatemala("2026-12-31")).toEqual({ desde: "2026-12-31T06:00:00Z", hasta: "2027-01-01T06:00:00Z" });
        expect(rangoDiaGuatemala("2026-13-01")).toBeUndefined();
        expect(rangoDiaGuatemala("")).toBeUndefined();
    });

    it("arma la query de GET /ordenes", () => {
        expect(consultaDeFiltrosOrdenes(FILTROS_ORDENES_INICIALES)).toEqual({});
        expect(hayFiltrosOrdenes(FILTROS_ORDENES_INICIALES)).toBe(false);
        expect(
            consultaDeFiltrosOrdenes({ estado: "activas", tipo: "retiro", tecnico: 3, fecha: "2026-10-28", zona: " 4 ", q: " OT-1 " }),
        ).toEqual({
            estado: ["pendiente", "programada", "en_camino", "en_sitio"],
            tipo: ["retiro"],
            tecnicoId: 3,
            desde: "2026-10-28T06:00:00Z",
            hasta: "2026-10-29T06:00:00Z",
            zonaId: 4,
            q: "OT-1",
        });
        expect(consultaDeFiltrosOrdenes({ ...FILTROS_ORDENES_INICIALES, tecnico: "sin", zona: "0", estado: "fallida" })).toEqual({ sinTecnico: true, estado: ["fallida"] });
    });
});

describe("historial y GPS", () => {
    const ev = (e: Partial<EventoOrden>): EventoOrden => ({
        id: 1,
        tipo: "estado",
        de: null,
        a: null,
        motivo: null,
        datos: {},
        actorTipo: "usuario",
        actorId: 3,
        actorNombre: "Lucía",
        canal: "campo",
        ocurridoEn: "2026-10-28T15:00:00Z",
        creadoEn: "2026-10-28T15:00:00Z",
        ...e,
    });

    it("describe cada tipo de evento", () => {
        expect(describirEventoOrden(ev({ tipo: "creada" }))).toBe("Creó la orden");
        expect(describirEventoOrden(ev({ de: "programada", a: "en_camino" }))).toBe("Cambió el estado de programada a en camino");
        expect(describirEventoOrden(ev({ tipo: "asignada", datos: { tecnico_id: null } }))).toBe("Dejó la orden sin técnico");
        expect(describirEventoOrden(ev({ tipo: "asignada", datos: { tecnico_id: 3 } }))).toBe("Cambió el técnico");
        expect(describirEventoOrden(ev({ tipo: "programada", datos: { inicio: "2026-10-28T20:30:00Z", reprogramada: true } }))).toBe("Reprogramó para el 28/10/2026 14:30");
    });

    it("extrae los puntos GPS con su hora, en orden", () => {
        const puntos = puntosGps([
            ev({ id: 2, a: "en_sitio", datos: { lat: 14.6, lon: -90.5, precision: 8, tomado_en: "2026-10-28T16:00:00Z" } }),
            ev({ id: 1, a: "en_camino", datos: { lat: 14.5, lon: -90.4, tomado_en: "2026-10-28T15:30:00Z" } }),
            ev({ id: 3, a: "cerrada", datos: {} }),
            ev({ id: 4, a: "cerrada", datos: { lat: 200, lon: 0 } }),
        ]);
        expect(puntos.map((p) => [p.eventoId, p.que, p.precision])).toEqual([
            [1, "Salió hacia el sitio", undefined],
            [2, "Llegó al sitio", 8],
        ]);
    });
});
