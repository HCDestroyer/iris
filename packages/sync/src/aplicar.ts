// Aplicación optimista de una operación sobre una orden (función pura). La usa el motor para la
// vista del teléfono (base del servidor + cola) y para avanzar la base cuando el lote confirma.
import type { EstadoOrden, Existencia, ItemChecklist, MarcaChecklist, OrdenCampo, TipoOperacion } from "./contrato-sync";
import type { OperacionLocal } from "./modelo";

/** Autor de las marcas del teléfono aún sin confirmar. */
export const AUTOR_LOCAL = "yo";

/** Tipos que la API acepta como evidencia aunque la orden esté cancelada (§3). */
export const TIPOS_EVIDENCIA: ReadonlySet<TipoOperacion> = new Set<TipoOperacion>(["nota.agregar", "adjunto.registrar"]);

/** Estado al que lleva cada operación que cambia el estado de la orden. */
const TRANSICIONES: Partial<Record<TipoOperacion, EstadoOrden>> = {
    "orden.iniciar_camino": "en_camino",
    "orden.llegar": "en_sitio",
    "orden.cerrar": "cerrada",
    "orden.fallar": "fallida",
};

export const ESTADOS_TERMINALES: ReadonlySet<EstadoOrden> = new Set<EstadoOrden>(["cerrada", "fallida", "cancelada"]);

function msDe(iso: string | null): number {
    if (iso === null) {
        return Number.NEGATIVE_INFINITY;
    }
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? Number.NEGATIVE_INFINITY : ms;
}

/**
 * Regla del checklist (§3): gana la marca más reciente por `ocurridoEn` y el historial guarda
 * ambas (sin repetir la misma marca).
 */
export function fusionarMarca(item: ItemChecklist, marca: MarcaChecklist): ItemChecklist {
    const repetida = item.historial.some(
        (h) => h.ocurridoEn === marca.ocurridoEn && h.por === marca.por && h.valor === marca.valor,
    );
    const historial = repetida
        ? item.historial
        : [...item.historial, marca].sort((a, b) => msDe(a.ocurridoEn) - msDe(b.ocurridoEn));
    if (msDe(marca.ocurridoEn) >= msDe(item.marcadoEn)) {
        return { ...item, historial, valor: marca.valor, marcadoEn: marca.ocurridoEn, marcadoPor: marca.por };
    }
    return { ...item, historial };
}

export function aplicarOperacion(orden: OrdenCampo, op: OperacionLocal): OrdenCampo {
    const destino = TRANSICIONES[op.tipo];
    if (destino) {
        // Una orden cancelada no cambia de estado por una acción del teléfono (la API la rechaza).
        const estado = orden.estado === "cancelada" ? orden.estado : destino;
        return { ...orden, estado };
    }
    switch (op.tipo) {
        case "checklist.marcar": {
            const { itemId, valor } = op.datos;
            const marca: MarcaChecklist = { valor, ocurridoEn: op.ocurridoEn, por: AUTOR_LOCAL };
            return {
                ...orden,
                checklist: orden.checklist.map((item) => (item.id === itemId ? fusionarMarca(item, marca) : item)),
            };
        }
        case "nota.agregar":
            if (orden.notas.some((n) => n.id === op.id)) {
                return orden;
            }
            return { ...orden, notas: [...orden.notas, { id: op.id, texto: op.datos.texto, ocurridoEn: op.ocurridoEn }] };
        case "material.usar":
            if (orden.materiales.some((m) => m.id === op.id)) {
                return orden;
            }
            return {
                ...orden,
                materiales: [...orden.materiales, { id: op.id, productoId: op.datos.productoId, cantidad: op.datos.cantidad }],
            };
        case "equipo.instalar":
        case "equipo.retirar":
            if (orden.equipos.some((e) => e.id === op.id)) {
                return orden;
            }
            return {
                ...orden,
                equipos: [
                    ...orden.equipos,
                    {
                        id: op.id,
                        accion: op.tipo === "equipo.instalar" ? "instalar" : "retirar",
                        serie: op.datos.serie,
                        mac: op.datos.mac ?? null,
                    },
                ],
            };
        case "firma.capturar":
            return {
                ...orden,
                firma: { nombre: op.datos.nombre, documento: op.datos.documento, adjuntoId: op.datos.adjuntoId ?? null },
            };
        case "adjunto.registrar":
            if (orden.adjuntos.includes(op.datos.adjuntoId)) {
                return orden;
            }
            return { ...orden, adjuntos: [...orden.adjuntos, op.datos.adjuntoId] };
        default:
            return orden;
    }
}

/**
 * Existencias de la bodega del técnico menos los `material.usar` de la cola. Puede quedar en
 * negativo: la API acepta el consumo como evidencia y deja pendiente el movimiento en Argos (§3).
 */
export function descontarExistencias(existencias: readonly Existencia[], ops: readonly OperacionLocal[]): Existencia[] {
    const usado = new Map<string, number>();
    for (const op of ops) {
        if (op.tipo === "material.usar") {
            usado.set(op.datos.productoId, (usado.get(op.datos.productoId) ?? 0) + op.datos.cantidad);
        }
    }
    return existencias.map((e) => ({ ...e, cantidad: e.cantidad - (usado.get(e.productoId) ?? 0) }));
}
