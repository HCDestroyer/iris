// Qué acciones ve el técnico según el estado y el tipo de la orden (espejo de las reglas de
// apps/api/internal/campo), y qué falta para cerrar. Las no válidas no se muestran.
import { ESTADOS_TERMINALES, valorDe, type OrdenVista, type TipoOrden } from "@iris/sync";

export interface AccionesOrden {
    iniciarCamino: boolean;
    llegar: boolean;
    /** Checklist, materiales, equipos, firma y fotos: con la orden en sitio. */
    trabajo: boolean;
    cerrar: boolean;
    fallar: boolean;
    /** Notas y fotos sueltas: en cualquier estado (también cancelada, como evidencia). */
    notas: boolean;
    instalar: boolean;
    retirar: boolean;
}

const INSTALAN: ReadonlySet<TipoOrden> = new Set<TipoOrden>(["instalacion", "cambio_equipo"]);
const RETIRAN: ReadonlySet<TipoOrden> = new Set<TipoOrden>(["retiro", "cambio_equipo"]);

export function accionesDe(orden: Pick<OrdenVista, "estado" | "tipo" | "reasignada">): AccionesOrden {
    const e = orden.estado;
    const viva = !orden.reasignada && !ESTADOS_TERMINALES.has(e);
    const trabajo = viva && e === "en_sitio";
    return {
        iniciarCamino: viva && e === "programada",
        llegar: viva && (e === "programada" || e === "en_camino"),
        trabajo,
        cerrar: trabajo,
        fallar: viva && (e === "programada" || e === "en_camino" || e === "en_sitio"),
        notas: !orden.reasignada,
        instalar: trabajo && INSTALAN.has(orden.tipo),
        retirar: trabajo && RETIRAN.has(orden.tipo),
    };
}

/**
 * Lo que falta para cerrar según lo que ve el teléfono (la API decide al final con
 * `cierre_incompleto` / `faltan_adjuntos`): checklist obligatorio, equipos del tipo y firma.
 */
export function faltantesCierre(orden: OrdenVista): string[] {
    const faltan: string[] = [];
    for (const item of orden.checklist ?? []) {
        const v = valorDe(item);
        if (item.obligatorio && (v === null || v === "")) {
            faltan.push(`Falta «${item.etiqueta}» del checklist.`);
        }
    }
    const equipos = orden.equipos ?? [];
    if (INSTALAN.has(orden.tipo) && !equipos.some((e) => e.accion === "instalar")) {
        faltan.push("Registra el equipo instalado.");
    }
    if (RETIRAN.has(orden.tipo) && !equipos.some((e) => e.accion === "retirar")) {
        faltan.push("Registra el equipo retirado.");
    }
    if (orden.requiereFirma && !orden.firma) {
        faltan.push("Falta la firma del cliente.");
    }
    return faltan;
}
