// Aplicación optimista de una operación sobre una orden (función pura). La usa el motor para la
// vista del teléfono (base del servidor + cola) y para avanzar la base cuando el lote confirma.
import type {
    EstadoOrden,
    Existencias,
    ExistenciasBodega,
    FormularioOrden,
    ItemChecklist,
    OrdenCampo,
    PausaOrden,
    PlantillaFormulario,
    TipoOperacion,
    ValorChecklist,
} from "./contrato";
import type { OperacionLocal } from "./modelo";

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

/** Estados desde los que se puede pausar (y a los que vuelve al reanudar). */
export const ESTADOS_PAUSABLES: ReadonlySet<EstadoOrden> = new Set<EstadoOrden>(["en_camino", "en_sitio"]);

/** Con la orden pausada, la API responde 422 `orden_pausada` a estas (primero `orden.reanudar`). */
export const TIPOS_BLOQUEADOS_EN_PAUSA: ReadonlySet<TipoOperacion> = new Set<TipoOperacion>([
    "orden.iniciar_camino",
    "orden.llegar",
    "orden.cerrar",
    "orden.fallar",
]);

/** Segundos que la orden lleva pausada en total: las pausas terminadas más la que sigue en curso. */
export function segundosPausada(orden: Pick<OrdenCampo, "estado" | "tiempoPausadoSeg" | "pausadaEn">, ahora: number): number {
    const previo = orden.tiempoPausadoSeg ?? 0;
    if (orden.estado !== "pausada" || !orden.pausadaEn) {
        return previo;
    }
    const desde = Date.parse(orden.pausadaEn);
    return Number.isNaN(desde) ? previo : previo + Math.max(0, Math.floor((ahora - desde) / 1000));
}

/** Ítems de un formulario de la orden (el checklist viene plano con `formularioId`). */
export function itemsDeFormulario(orden: Pick<OrdenCampo, "checklist">, formularioId: string): ItemChecklist[] {
    return (orden.checklist ?? []).filter((i) => i.formularioId === formularioId);
}

/**
 * ¿El formulario tiene respuestas? Las del servidor (`conRespuestas`) o, en el teléfono, alguna
 * marca o un adjunto de uno de sus ítems (lo que la API cuenta para no dejarlo quitar).
 */
export function formularioConRespuestas(orden: Pick<OrdenCampo, "checklist" | "formularios" | "adjuntos">, formularioId: string): boolean {
    const formulario = orden.formularios?.find((f) => f.id === formularioId);
    if (formulario?.conRespuestas) {
        return true;
    }
    const items = itemsDeFormulario(orden, formularioId);
    const ids = new Set(items.map((i) => i.itemId));
    return items.some((i) => i.valor !== null) || (orden.adjuntos ?? []).some((a) => a.itemId !== null && ids.has(a.itemId));
}

/** Formulario nuevo (y sus ítems) a partir de la plantilla del catálogo que vio el técnico. */
function agregarFormulario(orden: OrdenCampo, id: string, plantilla: PlantillaFormulario, ocurridoEn: string): OrdenCampo {
    const formularios = orden.formularios ?? [];
    if (formularios.some((f) => f.id === id || f.plantillaId === plantilla.id)) {
        // Ya está (confirmado, o despacho agregó la misma plantilla: la API lo rechazará con
        // `formulario_duplicado` y el técnico lo verá).
        return orden;
    }
    const formulario: FormularioOrden = {
        id,
        plantillaId: plantilla.id,
        codigo: plantilla.codigo,
        nombre: plantilla.nombre,
        versionId: plantilla.versionId,
        numeroVersion: plantilla.numeroVersion,
        requiereFirma: plantilla.requiereFirma,
        origen: "tecnico",
        agregadoPor: null,
        agregadoEn: ocurridoEn,
        conRespuestas: false,
    };
    const items: ItemChecklist[] = (plantilla.items ?? []).map((i) => ({
        itemId: i.id,
        codigo: i.codigo,
        etiqueta: i.etiqueta,
        tipo: i.tipo,
        obligatorio: i.obligatorio,
        unidad: i.unidad,
        posicion: i.posicion,
        ...(i.opciones ? { opciones: i.opciones } : {}),
        formularioId: id,
        valor: null,
        ocurridoEn: null,
        marcadoPor: null,
    }));
    return {
        ...orden,
        formularios: [...formularios, formulario],
        checklist: [...(orden.checklist ?? []), ...items],
        requiereFirma: orden.requiereFirma || plantilla.requiereFirma,
    };
}

function quitarFormulario(orden: OrdenCampo, formularioId: string): OrdenCampo {
    const formularios = orden.formularios ?? [];
    if (!formularios.some((f) => f.id === formularioId)) {
        return orden;
    }
    const quedan = formularios.filter((f) => f.id !== formularioId);
    return {
        ...orden,
        formularios: quedan,
        checklist: (orden.checklist ?? []).filter((i) => i.formularioId !== formularioId),
        requiereFirma: quedan.some((f) => f.requiereFirma),
    };
}

function msDe(iso: string | null): number {
    if (iso === null) {
        return Number.NEGATIVE_INFINITY;
    }
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? Number.NEGATIVE_INFINITY : ms;
}

/** Valor vigente de un ítem (la API lo tipa como «cualquiera»: boolean, número, texto o uuid). */
export function valorDe(item: ItemChecklist): ValorChecklist | null {
    const v: unknown = item.valor;
    return typeof v === "boolean" || typeof v === "number" || typeof v === "string" ? v : null;
}

/**
 * Regla del checklist (§3): gana la marca con `ocurridoEn` más reciente (empate: la que llega
 * después). La API guarda el historial; el teléfono solo muestra la vigente.
 */
export function fusionarMarca(item: ItemChecklist, valor: ValorChecklist | null, ocurridoEn: string): ItemChecklist {
    if (msDe(ocurridoEn) >= msDe(item.ocurridoEn)) {
        return { ...item, valor: valor as ItemChecklist["valor"], ocurridoEn, marcadoPor: null };
    }
    return item;
}

function cambiarEstado(orden: OrdenCampo, op: OperacionLocal, destino: EstadoOrden): OrdenCampo {
    // Una orden cancelada no cambia de estado por una acción del teléfono (la API la rechaza).
    if (orden.estado === "cancelada") {
        return orden;
    }
    const base = { ...orden, estado: destino };
    switch (op.tipo) {
        case "orden.iniciar_camino":
            return { ...base, enCaminoEn: op.ocurridoEn };
        case "orden.llegar":
            return { ...base, enSitioEn: op.ocurridoEn };
        case "orden.cerrar":
            return { ...base, finalizadaEn: op.ocurridoEn, resultado: op.datos.resultado ?? "exitosa" };
        case "orden.fallar":
            return { ...base, finalizadaEn: op.ocurridoEn, motivoFalla: op.datos.motivoFalla, motivo: op.datos.detalle ?? null };
        default:
            return base;
    }
}

export function aplicarOperacion(orden: OrdenCampo, op: OperacionLocal): OrdenCampo {
    if (op.tipo === "traslado.recibir") {
        // No es de una orden.
        return orden;
    }
    const destino = TRANSICIONES[op.tipo];
    if (destino) {
        return cambiarEstado(orden, op, destino);
    }
    switch (op.tipo) {
        case "checklist.marcar": {
            const { itemId, codigo, valor, formularioId } = op.datos;
            const delFormulario = (item: ItemChecklist) => formularioId === undefined || item.formularioId === undefined || item.formularioId === formularioId;
            return {
                ...orden,
                checklist: (orden.checklist ?? []).map((item) =>
                    (item.itemId === itemId || (itemId === undefined && item.codigo === codigo)) && delFormulario(item)
                        ? fusionarMarca(item, valor, op.ocurridoEn)
                        : item,
                ),
            };
        }
        case "orden.pausar": {
            if (!ESTADOS_PAUSABLES.has(orden.estado)) {
                return orden;
            }
            const desde = orden.estado === "en_camino" ? "en_camino" : "en_sitio";
            // Pausa local (id 0) hasta que el servidor mande la suya en el paquete o los cambios.
            const pausa: PausaOrden = {
                id: 0,
                motivo: op.datos.motivo,
                nota: op.datos.nota ?? null,
                desdeEstado: desde,
                inicio: op.ocurridoEn,
                fin: null,
                duracionSeg: null,
                cierre: null,
                pausadaPor: null,
                cerradaPor: null,
            };
            return { ...orden, estado: "pausada", pausadaDesde: desde, pausadaEn: op.ocurridoEn, pausas: [...(orden.pausas ?? []), pausa] };
        }
        case "orden.reanudar": {
            if (orden.estado !== "pausada") {
                return orden;
            }
            const tiempoPausadoSeg = segundosPausada(orden, Date.parse(op.ocurridoEn));
            const fin = op.ocurridoEn;
            const pausas = (orden.pausas ?? []).map((p) =>
                p.fin === null ? { ...p, fin, duracionSeg: Math.max(0, Math.floor((Date.parse(fin) - Date.parse(p.inicio)) / 1000)), cierre: "reanudada" as const } : p,
            );
            const resto: OrdenCampo = { ...orden, estado: orden.pausadaDesde ?? "en_sitio", tiempoPausadoSeg, pausas };
            delete resto.pausadaEn;
            delete resto.pausadaDesde;
            return resto;
        }
        case "orden.agregar_formulario":
            return op.plantilla ? agregarFormulario(orden, op.id, op.plantilla, op.ocurridoEn) : orden;
        case "orden.quitar_formulario":
            return quitarFormulario(orden, op.datos.formularioId);
        case "nota.agregar": {
            const notas = orden.notas ?? [];
            if (notas.some((n) => n.id === op.id)) {
                return orden;
            }
            const nota = { id: op.id, texto: op.datos.texto, ocurridoEn: op.ocurridoEn, autorId: null, autorNombre: "", canal: "campo" };
            return { ...orden, notas: [...notas, nota] };
        }
        case "material.usar": {
            const materiales = orden.materiales ?? [];
            if (materiales.some((m) => m.id === op.id)) {
                return orden;
            }
            const material = {
                id: op.id,
                productoId: op.datos.productoId,
                cantidad: op.datos.cantidad,
                descripcion: op.datos.descripcion ?? "",
                bodegaId: orden.bodegaId,
                excedeExistencia: false,
                ocurridoEn: op.ocurridoEn,
            };
            return { ...orden, materiales: [...materiales, material] };
        }
        case "equipo.instalar":
        case "equipo.retirar": {
            const equipos = orden.equipos ?? [];
            if (equipos.some((e) => e.id === op.id)) {
                return orden;
            }
            const equipo = {
                id: op.id,
                accion: op.tipo === "equipo.instalar" ? ("instalar" as const) : ("retirar" as const),
                serie: op.datos.serie ?? null,
                mac: op.datos.mac ?? null,
                modelo: op.datos.modelo ?? "",
                unidadId: op.datos.unidadId ?? null,
                condicion: op.datos.condicion ?? null,
                ocurridoEn: op.ocurridoEn,
                validado: false,
            };
            return { ...orden, equipos: [...equipos, equipo] };
        }
        case "firma.capturar":
            return {
                ...orden,
                firma: {
                    nombre: op.datos.nombre,
                    documento: op.datos.documento ?? "",
                    adjuntoId: op.datos.adjuntoId ?? null,
                    ocurridoEn: op.ocurridoEn,
                },
            };
        case "adjunto.registrar": {
            const adjuntos = orden.adjuntos ?? [];
            if (adjuntos.some((a) => a.id === op.datos.adjuntoId)) {
                return orden;
            }
            const adjunto = {
                id: op.datos.adjuntoId,
                tipo: op.datos.tipo,
                itemId: op.datos.itemId ?? null,
                descripcion: op.datos.descripcion ?? "",
                ocurridoEn: op.ocurridoEn,
            };
            return { ...orden, adjuntos: [...adjuntos, adjunto] };
        }
        default:
            return orden;
    }
}

/**
 * Bodega de la que descuenta una orden: la suya o, si no la trae y el técnico tiene una sola,
 * esa. `null` si no se puede saber.
 */
export function bodegaDeOrden(existencias: Existencias | null, bodegaId: number | null): ExistenciasBodega | null {
    const bodegas = existencias?.bodegas ?? [];
    if (bodegaId !== null) {
        return bodegas.find((b) => b.bodegaId === bodegaId) ?? null;
    }
    return bodegas.length === 1 ? (bodegas[0] ?? null) : null;
}

/**
 * Existencias de las bodegas del técnico menos los `material.usar` de la cola, cada uno en la
 * bodega de su orden. Puede quedar en negativo: la API acepta el consumo como evidencia y deja el
 * movimiento en Argos con aviso (§3).
 */
export function descontarExistencias(
    existencias: Existencias | null,
    ops: readonly OperacionLocal[],
    bodegaDe: (ordenId: number) => number | null,
): Existencias | null {
    if (!existencias) {
        return existencias;
    }
    const usado = new Map<string, number>();
    for (const op of ops) {
        if (op.tipo === "material.usar") {
            const bodega = bodegaDeOrden(existencias, bodegaDe(op.ordenId));
            if (bodega) {
                const clave = `${bodega.bodegaId}:${op.datos.productoId}`;
                usado.set(clave, (usado.get(clave) ?? 0) + op.datos.cantidad);
            }
        }
    }
    if (usado.size === 0) {
        return existencias;
    }
    return {
        ...existencias,
        bodegas: (existencias.bodegas ?? []).map((b) => ({
            ...b,
            lineas: (b.lineas ?? []).map((l) => ({ ...l, cantidad: l.cantidad - (usado.get(`${b.bodegaId}:${l.productoId}`) ?? 0) })),
        })),
    };
}
