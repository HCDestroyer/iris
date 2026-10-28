import type { ItemChecklist, OrdenCampo } from "../contrato-sync";

export function itemDePrueba(id: string, parcial: Partial<ItemChecklist> = {}): ItemChecklist {
    return {
        id,
        texto: `Ítem ${id}`,
        tipo: "si_no",
        obligatorio: false,
        valor: null,
        marcadoEn: null,
        marcadoPor: null,
        historial: [],
        ...parcial,
    };
}

export function ordenDePrueba(id: string, parcial: Partial<OrdenCampo> = {}): OrdenCampo {
    return {
        id,
        numero: `OT-${id}`,
        tipo: "instalacion",
        estado: "programada",
        version: 1,
        cliente: { id: `cli-${id}`, nombre: `Cliente ${id}` },
        servicio: { id: `srv-${id}` },
        checklist: [itemDePrueba("onu")],
        equiposComodato: [],
        notas: [],
        materiales: [],
        equipos: [],
        adjuntos: [],
        firma: null,
        ...parcial,
    };
}
