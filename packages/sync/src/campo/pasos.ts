// La orden paso a paso (fase 4): resumen → llegada · checklist · materiales y equipos · fotos →
// «Firma del cliente y cierre». Qué falta en cada paso según lo que ve el teléfono (la API decide al
// final con `cierre_incompleto` / `faltan_adjuntos`). Compartido con la app nativa.
import { valorDe, type OrdenVista, type TipoOrden } from "../index";

/** Pasos de la orden, en orden; `cierre` es el paso final y único (firma del cliente + cierre). */
export const PASOS_ORDEN = ["llegada", "checklist", "materiales", "fotos", "cierre"] as const;
export type PasoOrden = (typeof PASOS_ORDEN)[number];

export const TITULOS_PASO: Record<PasoOrden, string> = {
    llegada: "Llegada",
    checklist: "Checklist",
    materiales: "Materiales y equipos",
    fotos: "Fotos",
    cierre: "Firma del cliente y cierre",
};

export interface EstadoPaso {
    paso: PasoOrden;
    titulo: string;
    /** Sin nada obligatorio pendiente (en `cierre`: la orden ya está cerrada). */
    completo: boolean;
    /** El paso no tiene nada obligatorio para esta orden. */
    opcional: boolean;
    /** Lo que falta, en palabras del técnico. */
    faltan: string[];
}

const INSTALAN: ReadonlySet<TipoOrden> = new Set<TipoOrden>(["instalacion", "cambio_equipo"]);
const RETIRAN: ReadonlySet<TipoOrden> = new Set<TipoOrden>(["retiro", "cambio_equipo"]);

type OrdenPasos = Pick<OrdenVista, "estado" | "tipo" | "enSitioEn" | "pausadaDesde" | "checklist" | "formularios" | "equipos" | "materiales" | "adjuntos" | "firma" | "requiereFirma">;

/** ¿Ya llegó al sitio? (en sitio, pausada en sitio o terminada después de llegar). */
export function llego(orden: Pick<OrdenVista, "estado" | "enSitioEn" | "pausadaDesde">): boolean {
    return orden.estado === "en_sitio" || orden.estado === "cerrada" || (orden.estado === "pausada" && orden.pausadaDesde === "en_sitio") || Boolean(orden.enSitioEn);
}

function faltanChecklist(orden: OrdenPasos): string[] {
    const formularios = new Map((orden.formularios ?? []).map((f) => [f.id, f.nombre]));
    const faltan: string[] = [];
    for (const item of orden.checklist ?? []) {
        const v = valorDe(item);
        if (item.obligatorio && (v === null || v === "")) {
            const nombre = item.formularioId ? formularios.get(item.formularioId) : undefined;
            faltan.push(nombre ? `Falta «${item.etiqueta}» (${nombre}).` : `Falta «${item.etiqueta}» del checklist.`);
        }
    }
    return faltan;
}

function faltanEquipos(orden: OrdenPasos): string[] {
    const equipos = orden.equipos ?? [];
    const faltan: string[] = [];
    if (INSTALAN.has(orden.tipo) && !equipos.some((e) => e.accion === "instalar")) {
        faltan.push("Registra el equipo instalado.");
    }
    if (RETIRAN.has(orden.tipo) && !equipos.some((e) => e.accion === "retirar")) {
        faltan.push("Registra el equipo retirado.");
    }
    return faltan;
}

/** Estado de cada paso, en orden. */
export function estadoPasos(orden: OrdenPasos): EstadoPaso[] {
    const llegada = llego(orden);
    const checklist = faltanChecklist(orden);
    const equipos = faltanEquipos(orden);
    const obligatorios = (orden.checklist ?? []).some((i) => i.obligatorio);
    const conEquipos = INSTALAN.has(orden.tipo) || RETIRAN.has(orden.tipo);
    const antes = [...(llegada ? [] : ["Marca tu llegada al sitio."]), ...checklist, ...equipos];
    return [
        { paso: "llegada", titulo: TITULOS_PASO.llegada, completo: llegada, opcional: false, faltan: llegada ? [] : ["Marca tu llegada al sitio."] },
        { paso: "checklist", titulo: TITULOS_PASO.checklist, completo: checklist.length === 0, opcional: !obligatorios, faltan: checklist },
        { paso: "materiales", titulo: TITULOS_PASO.materiales, completo: equipos.length === 0, opcional: !conEquipos, faltan: equipos },
        // Las fotos obligatorias son ítems del checklist; aquí van las fotos sueltas (opcionales).
        { paso: "fotos", titulo: TITULOS_PASO.fotos, completo: true, opcional: true, faltan: [] },
        { paso: "cierre", titulo: TITULOS_PASO.cierre, completo: orden.estado === "cerrada", opcional: false, faltan: antes },
    ];
}

/**
 * Lo que falta antes de abrir «Firma del cliente y cierre»: llegar, el checklist obligatorio y los
 * equipos del tipo. La firma no cuenta: se toma en ese mismo paso.
 */
export function faltantesAntesDeFirmar(orden: OrdenPasos): string[] {
    return estadoPasos(orden).find((p) => p.paso === "cierre")?.faltan ?? [];
}

/** Pasos de trabajo listos (de los 4 previos a la firma), para la barra de progreso. */
export function progresoPasos(orden: OrdenPasos): { listos: number; total: number } {
    const previos = estadoPasos(orden).filter((p) => p.paso !== "cierre");
    return { listos: previos.filter((p) => p.completo).length, total: previos.length };
}

/** Primer paso con algo pendiente (o la firma y el cierre si ya está todo). */
export function siguientePaso(orden: OrdenPasos): PasoOrden {
    return estadoPasos(orden).find((p) => p.paso !== "fotos" && !p.completo)?.paso ?? "cierre";
}
