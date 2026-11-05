// Qué acciones ve el técnico según el estado y el tipo de la orden (espejo de las reglas de
// apps/api/internal/campo), y qué falta para cerrar. Las no válidas no se muestran.
import { ESTADOS_TERMINALES, valorDe, type OrdenVista, type TipoOrden } from "../index";

export interface AccionesOrden {
    iniciarCamino: boolean;
    llegar: boolean;
    /**
     * Checklist, materiales, equipos, firma y fotos: solo en sitio. Con la orden pausada todo queda
     * de solo lectura (la API responde `orden_pausada`): en pausa no se registra trabajo.
     */
    trabajo: boolean;
    /** La orden está pausada (y sigue siendo del técnico): los pasos se ven de solo lectura. */
    enPausa: boolean;
    cerrar: boolean;
    fallar: boolean;
    /** Notas y fotos sueltas: en cualquier estado (también cancelada, como evidencia). */
    notas: boolean;
    instalar: boolean;
    retirar: boolean;
    /** En camino o en sitio: pausar con motivo. */
    pausar: boolean;
    reanudar: boolean;
    /** Sumar un formulario del catálogo (orden propia, en camino o en sitio; pausada, no). */
    agregarFormulario: boolean;
}

const INSTALAN: ReadonlySet<TipoOrden> = new Set<TipoOrden>(["instalacion", "cambio_equipo"]);
const RETIRAN: ReadonlySet<TipoOrden> = new Set<TipoOrden>(["retiro", "cambio_equipo"]);

export function accionesDe(orden: Pick<OrdenVista, "estado" | "tipo" | "reasignada">): AccionesOrden {
    const e = orden.estado;
    const viva = !orden.reasignada && !ESTADOS_TERMINALES.has(e);
    // Con la orden pausada no se llega, ni se sale, ni se cierra, ni se falla, ni se registra trabajo
    // (checklist, formularios, materiales, equipos, firma): la API responde `orden_pausada`.
    const trabajo = viva && e === "en_sitio";
    return {
        iniciarCamino: viva && e === "programada",
        llegar: viva && (e === "programada" || e === "en_camino"),
        trabajo,
        enPausa: viva && e === "pausada",
        cerrar: viva && e === "en_sitio",
        fallar: viva && (e === "programada" || e === "en_camino" || e === "en_sitio"),
        notas: !orden.reasignada,
        instalar: trabajo && INSTALAN.has(orden.tipo),
        retirar: trabajo && RETIRAN.has(orden.tipo),
        pausar: viva && (e === "en_camino" || e === "en_sitio"),
        reanudar: viva && e === "pausada",
        // Con el trabajo empezado (en camino o en sitio): en una programada solo se ve la visita y en
        // una pausada no se registra trabajo.
        agregarFormulario: viva && (e === "en_camino" || e === "en_sitio"),
    };
}

/**
 * Lo que falta para cerrar según lo que ve el teléfono (la API decide al final con
 * `cierre_incompleto` / `faltan_adjuntos`): checklist obligatorio, equipos del tipo y firma.
 */
export function faltantesCierre(orden: OrdenVista): string[] {
    const faltan: string[] = [];
    const formularios = new Map((orden.formularios ?? []).map((f) => [f.id, f.nombre]));
    for (const item of orden.checklist ?? []) {
        const v = valorDe(item);
        if (item.obligatorio && (v === null || v === "")) {
            const nombre = item.formularioId ? formularios.get(item.formularioId) : undefined;
            faltan.push(nombre ? `Falta «${item.etiqueta}» (${nombre}).` : `Falta «${item.etiqueta}» del checklist.`);
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
