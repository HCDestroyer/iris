import type { ItemChecklist, OrdenCampo } from "../contrato";

const CREADA = "2026-10-28T12:00:00Z";

export function itemDePrueba(itemId: number, parcial: Partial<ItemChecklist> = {}): ItemChecklist {
    return {
        itemId,
        codigo: `item${itemId}`,
        etiqueta: `Ítem ${itemId}`,
        tipo: "si_no",
        obligatorio: false,
        unidad: "",
        posicion: itemId,
        valor: null,
        marcadoPor: null,
        ocurridoEn: null,
        ...parcial,
    };
}

/** Orden del paquete con todos los campos de `OrdenCampo` (fixture fiel al OpenAPI). */
export function ordenDePrueba(id: number, parcial: Partial<OrdenCampo> = {}): OrdenCampo {
    return {
        id,
        numero: `OT-${String(id).padStart(6, "0")}`,
        tipo: "instalacion",
        tipoId: 1,
        estado: "programada",
        version: 1,
        actualizadoEn: CREADA,
        creadoEn: CREADA,
        asignadaEn: CREADA,
        enCaminoEn: null,
        enSitioEn: null,
        finalizadaEn: null,
        programadaInicio: "2026-10-28T15:00:00Z",
        programadaFin: "2026-10-28T17:00:00Z",
        bodegaId: 7,
        clienteId: 100 + id,
        servicioId: 200 + id,
        cuadrillaId: null,
        empresaId: null,
        plantillaId: 1,
        prospectoId: null,
        tecnicoId: 5,
        ticketId: null,
        zonaId: null,
        descripcion: "",
        direccion: `Calle ${id}, zona 1`,
        lat: 14.6,
        lon: -90.5,
        contacto: { nombre: `Cliente ${id}`, telefono: "5555-0000" },
        motivo: null,
        motivoFalla: null,
        resultado: null,
        requiereFirma: false,
        checklist: [itemDePrueba(1)],
        notas: [],
        materiales: [],
        equipos: [],
        adjuntos: [],
        comodato: { disponible: false, motivo: "sin_conector", equipos: [] },
        ...parcial,
    };
}
