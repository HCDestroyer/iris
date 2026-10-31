// Fixtures de la app de campo, tipados con packages/contratos (OrdenCampo, Paquete…).
import type { Schema } from "@iris/contratos";

type OrdenCampo = Schema<"OrdenCampo">;
type ItemChecklist = Schema<"ItemChecklist">;

export const TECNICO: Schema<"Usuario"> = {
    id: 51,
    nombre: "Tomás Herrera",
    correo: "tomas@iris.test",
    tipo: "persona",
    activo: true,
    mfaActivo: false,
    roles: [{ rol: "tecnico" }],
    empresas: [],
    version: 1,
    creadoEn: "2026-10-01T12:00:00Z",
    actualizadoEn: "2026-10-01T12:00:00Z",
};

/** Otro técnico que usa el mismo teléfono (aislamiento entre usuarios). */
export const TECNICO_B: Schema<"Usuario"> = { ...TECNICO, id: 52, nombre: "Beatriz Sosa", correo: "beatriz@iris.test" };

export const PERMISOS_TECNICO: Schema<"Permiso">[] = [
    "adjuntos.subir",
    "adjuntos.ver",
    "campo.sincronizar",
    "ordenes.cerrar",
    "ordenes.fallar",
    "ordenes.iniciar_camino",
    "ordenes.llegar",
    "ordenes.registrar",
    "ordenes.ver_propias",
];

export const CABLE = 900;
export const CONECTOR = 901;
export const BODEGA = 7;

function item(itemId: number, codigo: string, etiqueta: string, tipo: ItemChecklist["tipo"], obligatorio: boolean, unidad = ""): ItemChecklist {
    return { itemId, codigo, etiqueta, tipo, obligatorio, unidad, posicion: itemId, valor: null, marcadoPor: null, ocurridoEn: null };
}

function orden(id: number, parcial: Partial<OrdenCampo>): OrdenCampo {
    const hoy = new Date().toISOString().slice(0, 10);
    return {
        id,
        numero: `OT-${String(id).padStart(6, "0")}`,
        tipo: "instalacion",
        tipoId: 1,
        estado: "programada",
        version: 1,
        actualizadoEn: `${hoy}T12:00:00Z`,
        creadoEn: `${hoy}T12:00:00Z`,
        asignadaEn: `${hoy}T12:00:00Z`,
        enCaminoEn: null,
        enSitioEn: null,
        finalizadaEn: null,
        programadaInicio: `${hoy}T15:00:00Z`,
        programadaFin: `${hoy}T17:00:00Z`,
        bodegaId: BODEGA,
        clienteId: 3000 + id,
        servicioId: 4000 + id,
        cuadrillaId: null,
        empresaId: null,
        plantillaId: 1,
        prospectoId: null,
        tecnicoId: TECNICO.id,
        ticketId: null,
        zonaId: 4,
        descripcion: "",
        direccion: "",
        lat: null,
        lon: null,
        contacto: { nombre: null, telefono: null },
        motivo: null,
        motivoFalla: null,
        resultado: null,
        requiereFirma: false,
        checklist: [],
        notas: [],
        materiales: [],
        equipos: [],
        adjuntos: [],
        comodato: { disponible: false, motivo: "sin_conector", equipos: [] },
        ...parcial,
    };
}

/** Órdenes del día: una instalación completa (checklist, foto, firma), una visita y un retiro. */
export function ordenesDelDia(): OrdenCampo[] {
    const hoy = new Date().toISOString().slice(0, 10);
    return [
        orden(101, {
            numero: "OT-000101",
            tipo: "instalacion",
            programadaInicio: `${hoy}T14:30:00Z`,
            programadaFin: `${hoy}T16:30:00Z`,
            direccion: "6a avenida 12-34, zona 1, Guatemala",
            lat: 14.6407,
            lon: -90.5133,
            contacto: { nombre: "María Castillo", telefono: "+502 5555-1234" },
            requiereFirma: true,
            descripcion: "Instalación de fibra, plan 100 Mbps. Portón negro.",
            checklist: [
                item(1, "onu_enciende", "La ONU enciende", "si_no", true),
                item(2, "potencia", "Potencia óptica", "medicion", true, "dBm"),
                item(3, "fachada", "Foto de la fachada", "foto", true),
                item(4, "observaciones", "Observaciones", "texto", false),
            ],
            cliente: {
                id: 3101,
                codigo: "C-3101",
                nombre: "María Castillo",
                direccion: "6a avenida 12-34, zona 1, Guatemala",
                estado: "activo",
                lat: 14.6407,
                lon: -90.5133,
                telefono: "+502 5555-1234",
                sincronizadoEn: `${hoy}T12:00:00Z`,
            },
            servicio: {
                id: 4101,
                codigo: "S-4101",
                plan: "Fibra 100 Mbps",
                estado: "por_instalar",
                direccion: "6a avenida 12-34, zona 1",
                ip: "",
                router: "",
                lat: null,
                lon: null,
                sincronizadoEn: `${hoy}T12:00:00Z`,
            },
            comodato: { disponible: true, motivo: null, equipos: [] },
        }),
        orden(102, {
            numero: "OT-000102",
            tipo: "visita",
            programadaInicio: `${hoy}T17:00:00Z`,
            programadaFin: `${hoy}T18:00:00Z`,
            direccion: "Calzada Roosevelt 22-43, zona 11",
            contacto: { nombre: "Jorge Méndez", telefono: "5555-9876" },
            checklist: [item(5, "senal_ok", "Señal restablecida", "si_no", true)],
        }),
        orden(103, {
            numero: "OT-000103",
            tipo: "retiro",
            programadaInicio: `${hoy}T19:00:00Z`,
            programadaFin: `${hoy}T20:00:00Z`,
            direccion: "Boulevard Los Próceres 18-80, zona 10",
            contacto: { nombre: "Ana Pérez", telefono: null },
            comodato: { disponible: true, motivo: null, equipos: [{ unidadId: 77, serie: "HWTC9A1B2C3D", mac: "A0:B1:C2:D3:E4:F5", modelo: "Huawei HG8145V5" }] },
        }),
    ];
}

export function existenciasDelTecnico(): Schema<"Existencias"> {
    return {
        disponible: true,
        motivo: null,
        bodegas: [
            {
                bodegaId: BODEGA,
                lineas: [
                    { productoId: CABLE, descripcion: "Cable drop 1 hilo", unidad: "m", cantidad: 120 },
                    { productoId: CONECTOR, descripcion: "Conector SC/APC", unidad: "u", cantidad: 10 },
                ],
            },
        ],
    };
}

/** La orden del día de Beatriz (TECNICO_B). */
export function ordenesDeB(): OrdenCampo[] {
    const hoy = new Date().toISOString().slice(0, 10);
    return [
        orden(201, {
            numero: "OT-000201",
            tipo: "visita",
            tecnicoId: TECNICO_B.id,
            programadaInicio: `${hoy}T15:30:00Z`,
            direccion: "Avenida Las Américas 9-50, zona 13",
            contacto: { nombre: "Luis Ortiz", telefono: "5555-2020" },
            checklist: [item(7, "senal_ok", "Señal restablecida", "si_no", true)],
        }),
    ];
}

export const ONT = 902;

/** Traslados de bodega central hacia la bodega del técnico (enviados en Argos, por recibir). */
export function trasladosDelTecnico(): Schema<"Traslado">[] {
    const ayer = new Date(Date.now() - 20 * 60 * 60_000).toISOString();
    return [
        {
            id: 31,
            estado: "enviado",
            empresaId: 0,
            bodegaDestinoId: BODEGA,
            enviadoEn: ayer,
            lineas: [
                { lineaId: 1, productoId: CABLE, cantidad: 100, series: null },
                { lineaId: 2, productoId: ONT, cantidad: 2, series: ["ZTEGC8F10001", "ZTEGC8F10002"] },
            ],
        },
        {
            id: 32,
            estado: "enviado",
            empresaId: 0,
            bodegaDestinoId: BODEGA,
            enviadoEn: ayer,
            lineas: [{ lineaId: 3, productoId: CONECTOR, cantidad: 20, series: null }],
        },
    ];
}

/** «Mi bodega» de Tomás en Argos. */
export function bodegaDelTecnico(): Schema<"MiBodegaRespuesta"> {
    const hace = (h: number) => new Date(Date.now() - h * 60 * 60_000).toISOString();
    return {
        bodega: { id: BODEGA, nombre: "Bodega móvil · Tomás", empresaId: 0 },
        existencias: [
            { productoId: CABLE, descripcion: "Cable drop 1 hilo", cantidad: 120, unidad: "m", modalidad: "consumible_gasto" },
            { productoId: CONECTOR, descripcion: "Conector SC/APC", cantidad: 10, unidad: "u", modalidad: "consumible_cobro" },
            { productoId: ONT, descripcion: "ONT ZTE F670L", cantidad: 1, unidad: "u", modalidad: "comodato" },
        ],
        unidades: [{ id: 501, productoId: ONT, descripcion: "ONT ZTE F670L", serie: "ZTEGC8F09999", mac: "A0:B1:C2:D3:E4:00", estado: "disponible" }],
        trasladosPendientes: [31, 32],
        kardex: [
            { id: 9001, tipo: "consumo", productoId: CABLE, descripcion: "Cable drop 1 hilo", cantidad: -35, referencia: "OT-000099", ocurridoEn: hace(26) },
            { id: 9002, tipo: "traslado_entrada", productoId: CONECTOR, descripcion: "Conector SC/APC", cantidad: 10, referencia: "Traslado 28", ocurridoEn: hace(50) },
        ],
        leidoEn: new Date().toISOString(),
    };
}
