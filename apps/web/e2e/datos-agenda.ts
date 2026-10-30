import type { Schema } from "@iris/contratos";
import type { EscenarioOrdenes } from "./datos-ordenes";

/*
 * Fixtures de agenda (zonas, cuadrillas, técnicos, turnos, ausencias y posiciones) para la API
 * falsa, tipados con packages/contratos. Las citas salen de las órdenes del escenario de órdenes
 * (las programadas con técnico), así programar o reprogramar se ve en el calendario. Fechas
 * relativas al día de Guatemala en que se arma el escenario.
 */

type Tecnico = Schema<"Tecnico">;
type OrdenDetalle = Schema<"OrdenDetalle">;

const MIN = 60_000;
const iso = (ms: number) => new Date(ms).toISOString().replace(".000Z", "Z");

/** Medianoche de Guatemala del día de `ahora` (UTC−6), en ms. */
export function medianocheGt(ahora: number): number {
    const local = new Date(ahora - 6 * 3_600_000);
    return Date.UTC(local.getUTCFullYear(), local.getUTCMonth(), local.getUTCDate(), 6);
}

export const LUCIA = 3;
export const JORGE = 11;
export const ROSA = 12;

export const ZONAS: Schema<"Zona">[] = [
    { id: 4, nombre: "Centro", origen: "argos", activa: true, empresaId: null, poligono: null, sincronizadoEn: "2026-10-28T12:00:00Z", version: 1, creadoEn: "2026-10-01T00:00:00Z", actualizadoEn: "2026-10-01T00:00:00Z" },
    { id: 5, nombre: "Mixco", origen: "argos", activa: true, empresaId: null, poligono: null, sincronizadoEn: "2026-10-28T12:00:00Z", version: 1, creadoEn: "2026-10-01T00:00:00Z", actualizadoEn: "2026-10-01T00:00:00Z" },
];

export const CUADRILLAS: Schema<"Cuadrilla">[] = [
    { id: 1, nombre: "Cuadrilla Norte", activa: true, empresaId: null, zonaIds: [4], capacidad: [], version: 1, creadoEn: "2026-10-01T00:00:00Z", actualizadoEn: "2026-10-01T00:00:00Z" },
    { id: 2, nombre: "Cuadrilla Sur", activa: true, empresaId: null, zonaIds: [4, 5], capacidad: [], version: 1, creadoEn: "2026-10-01T00:00:00Z", actualizadoEn: "2026-10-01T00:00:00Z" },
];

const tecnico = (id: number, usuarioId: number, nombre: string, cuadrillaId: number): Tecnico => ({
    id,
    usuarioId,
    nombre,
    correo: `${nombre.split(" ")[0]?.toLowerCase()}@iris.test`,
    cuadrillaId,
    activo: true,
    habilidades: ["fibra"],
    baseLat: null,
    baseLon: null,
    bodegaId: 30,
    telefono: "+502 5555-0000",
    version: 1,
    creadoEn: "2026-10-01T00:00:00Z",
    actualizadoEn: "2026-10-01T00:00:00Z",
});

export const TECNICOS: Tecnico[] = [tecnico(21, LUCIA, "Lucía Ramírez", 1), tecnico(22, JORGE, "Jorge Méndez", 1), tecnico(23, ROSA, "Rosa Ajú", 2)];

/** Turno de todos los técnicos, en minutos de Guatemala (07:00–17:00 todos los días). */
export const TURNO = { desde: 7 * 60, hasta: 17 * 60 };

export type EscenarioAgenda = {
    ausencias: Schema<"AusenciaAgenda">[];
    /** Ventana acordada por orden (la API la guarda en la cita). */
    ventanas: Map<number, { inicio: string; fin: string }>;
    posiciones: Schema<"PosicionTecnico">[];
    /** `GET /agenda` responde `truncado` (como la API con más de 2000 citas). */
    truncado?: boolean;
};

/**
 * Prepara el despacho sobre el escenario de órdenes: coordenadas en las órdenes de hoy de Lucía,
 * una cita de Jorge con ventana (orden 40) y la ausencia de Rosa todo el día.
 */
export function escenarioAgenda(ahora: number, ordenes: EscenarioOrdenes, empresa: number | null, preparar: boolean): EscenarioAgenda {
    const hoy = medianocheGt(ahora);
    const t = (h: number) => iso(hoy + h * 3_600_000);
    if (preparar) prepararOrdenes(ordenes, empresa, t);
    return {
        ausencias: [{ id: 501, tecnicoId: ROSA, fichaId: 23, motivo: "vacaciones", inicio: t(0), fin: t(24) }],
        ventanas: new Map([[40, { inicio: t(13), fin: t(16) }]]),
        posiciones: [
            posicion(LUCIA, 21, "Lucía Ramírez", 1, 14.6015, -90.5108, 40, ahora),
            posicion(JORGE, 22, "Jorge Méndez", 1, 14.6201, -90.5501, 9 * 60, ahora),
            posicion(ROSA, 23, "Rosa Ajú", 2, 14.6305, -90.5801, 5 * 3600, ahora),
        ],
    };
}

function prepararOrdenes(ordenes: EscenarioOrdenes, empresa: number | null, t: (h: number) => string) {
    const coordenadas: Record<number, [number, number]> = { 1: [14.634915, -90.506882], 3: [14.6123, -90.5291], 4: [14.6011, -90.5102], 5: [14.6402, -90.4991] };
    for (const o of ordenes.ordenes) {
        const c = coordenadas[o.id];
        if (c) [o.lat, o.lon] = c;
        if (o.tecnicoId === LUCIA) o.cuadrillaId = 1;
    }
    const base = ordenes.ordenes[2];
    if (base && !ordenes.ordenes.some((o) => o.id === 40)) {
        const nueva: OrdenDetalle = {
            ...base,
            id: 40,
            numero: "OT-000040",
            tipo: "visita",
            tipoId: 2,
            estado: "programada",
            empresaId: empresa,
            tecnicoId: JORGE,
            cuadrillaId: 1,
            zonaId: 4,
            direccion: "Calzada Roosevelt 22-43, zona 11",
            lat: 14.6205,
            lon: -90.5512,
            programadaInicio: t(14),
            programadaFin: t(15),
            version: 2,
        };
        ordenes.ordenes.push(nueva);
        // Segunda cita de Jorge (temprano), para poder reordenar su ruta.
        ordenes.ordenes.push({
            ...nueva,
            id: 41,
            numero: "OT-000041",
            tipo: "mantenimiento",
            tipoId: 5,
            direccion: "6a avenida 8-12, zona 9",
            lat: 14.6052,
            lon: -90.5203,
            programadaInicio: t(8),
            programadaFin: t(9),
            version: 1,
        });
    }
}

function posicion(tecnicoId: number, fichaId: number, nombre: string, cuadrillaId: number, lat: number, lon: number, antiguedadSeg: number, ahora: number): Schema<"PosicionTecnico"> {
    return {
        tecnicoId,
        fichaId,
        nombre,
        cuadrillaId,
        lat,
        lon,
        precision: 12,
        velocidad: tecnicoId === LUCIA ? 8 : null,
        rumbo: null,
        registradaEn: iso(ahora - antiguedadSeg * 1000),
        recibidaEn: iso(ahora - antiguedadSeg * 1000 + 2 * MIN),
        antiguedadSeg,
    };
}
