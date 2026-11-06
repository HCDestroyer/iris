// Acciones de la pantalla de la orden y notas editables (decisión del PM, nov 2026), las mismas en
// la PWA y en la app nativa: el encabezado «● Estado · Tipo · ventana», la acción principal en
// «tinta» con su grupo debajo, el aviso de pausa y lo que muestra cada nota.
import { ESTADOS_TERMINALES } from "../aplicar";
import type { NotaOrden } from "../contrato";
import type { OrdenVista } from "../motor";
import { horaCorta, ventanaCita, ZONA } from "./fechas";
import { llego } from "./pasos";
import { accionesDe } from "./reglas";
import { ESTADOS_TEXTO, MOTIVOS_PAUSA, TIPOS_ORDEN, type TonoEstado } from "./textos";

/** Línea bajo el número: «En sitio · Instalación · 08:30–10:00» con el tono del punto. */
export interface EncabezadoOrden {
    estado: string;
    tono: TonoEstado;
    /** Las partes después del estado (tipo y ventana u hora de la cita). */
    detalle: string[];
    /** Todo junto, para el nombre accesible. */
    texto: string;
}

export function encabezadoOrden(orden: Pick<OrdenVista, "estado" | "tipo" | "cita" | "programadaInicio" | "programadaFin">): EncabezadoOrden {
    const { texto: estado, tono } = ESTADOS_TEXTO[orden.estado];
    const inicio = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const fin = horaCorta(orden.cita?.fin ?? orden.programadaFin);
    // La ventana acordada con el cliente manda; si no hay, la hora de la cita.
    const ventana = ventanaCita(orden.cita) ?? (inicio ? (fin ? `${inicio}–${fin}` : inicio) : undefined);
    const detalle = [TIPOS_ORDEN[orden.tipo], ...(ventana ? [ventana] : [])];
    return { estado, tono, detalle, texto: [estado, ...detalle].join(" · ") };
}

export type AccionGrupo = "en_camino" | "pausar" | "fallar";

/**
 * Lo de arriba de la orden. `principal`: «Empezar trabajo» (aún sin llegar) o «Continuar trabajo»,
 * en tinta y a todo el ancho; `grupo`: debajo, [En camino | No se pudo] si está programada o
 * [Pausar | No se pudo] en camino o en sitio. Pausada: solo `pausa` (el aviso naranja) y
 * `reanudar`. Las reglas son las de `accionesDe` (sin cambiarlas).
 */
export interface AccionesPantallaOrden {
    principal: { texto: "Empezar trabajo" | "Continuar trabajo"; empezado: boolean } | null;
    grupo: AccionGrupo[];
    pausa: { texto: string; desde: string | null; motivo: string | null } | null;
    reanudar: boolean;
}

export function accionesPantallaOrden(orden: OrdenVista, puedeReanudar: boolean): AccionesPantallaOrden {
    const a = accionesDe(orden);
    if (a.enPausa) {
        const abierta = (orden.pausas ?? []).find((p) => p.fin === null);
        const desde = horaCorta(orden.pausadaEn ?? abierta?.inicio) ?? null;
        const motivo = abierta ? (abierta.motivo === "otro" && abierta.nota ? abierta.nota : MOTIVOS_PAUSA[abierta.motivo]) : null;
        const texto = [desde ? `En pausa desde ${desde}` : "En pausa", ...(motivo ? [motivo] : [])].join(" · ");
        return { principal: null, grupo: [], pausa: { texto, desde, motivo }, reanudar: a.reanudar && puedeReanudar };
    }
    const trabajable = a.iniciarCamino || a.llegar || a.trabajo;
    const empezado = llego(orden);
    const grupo: AccionGrupo[] = [];
    if (a.iniciarCamino) {
        grupo.push("en_camino");
    } else if (a.pausar) {
        grupo.push("pausar");
    }
    if (a.fallar) {
        grupo.push("fallar");
    }
    return {
        principal: trabajable ? { texto: empezado ? "Continuar trabajo" : "Empezar trabajo", empezado } : null,
        grupo,
        pausa: null,
        reanudar: false,
    };
}

/** Permiso con el que la API acepta `orden.agregar_formulario` (el técnico lo tiene en su rol base). */
export const PERMISO_AGREGAR_FORMULARIO = "ordenes.registrar";

/**
 * «Agregar formulario» del menú «⋯»: `null` si se puede (con `ordenes.registrar`, orden propia en
 * camino o en sitio), o el motivo con que el ítem aparece deshabilitado. Las mismas reglas que el
 * «+» de la isla (`accionesRapidas`); también sin señal.
 */
export function motivoSinFormulario(orden: OrdenVista, permisos: readonly string[] | undefined): string | null {
    if (!permisos?.includes(PERMISO_AGREGAR_FORMULARIO)) {
        return "Tu usuario no puede registrar trabajo en las órdenes.";
    }
    const a = accionesDe(orden);
    if (a.agregarFormulario) {
        return null;
    }
    if (a.enPausa) {
        return "La orden está pausada: primero reanúdala.";
    }
    if (orden.reasignada || ESTADOS_TERMINALES.has(orden.estado)) {
        return "Esta orden ya no admite formularios.";
    }
    return "Disponible con el trabajo empezado (en camino o en sitio).";
}

// Notas ------------------------------------------------------------------------------------------

/** Largo máximo de una nota (el de la API). */
export const MAX_NOTA = 4000;

/** «72 / 4000». */
export function contadorNota(texto: string): string {
    return `${texto.length} / ${MAX_NOTA}`;
}

const CANALES: Record<string, string> = {
    campo: "desde el teléfono",
    movil: "desde el teléfono",
    pwa: "desde el teléfono",
    panel: "desde el panel",
    web: "desde el panel",
    api: "por la API",
    integracion: "desde una integración",
    sistema: "del sistema",
};

/** Permiso con el que despacho y supervisión editan o eliminan notas ajenas (la API lo verifica). */
export const PERMISO_NOTAS_AJENAS = "ordenes.asignar";

/** La escribió el técnico: la suya del servidor o una agregada en el teléfono (autor aún sin id). */
export function notaPropia(nota: Pick<NotaOrden, "autorId">, usuarioId: number): boolean {
    return nota.autorId === null || nota.autorId === usuarioId;
}

/** ¿Ve «Editar» y «Eliminar»? Su autor, o despacho y supervisión (`ordenes.asignar`). */
export function puedeCambiarNota(nota: Pick<NotaOrden, "autorId">, usuarioId: number, permisos: readonly string[] | undefined): boolean {
    return notaPropia(nota, usuarioId) || (permisos ?? []).includes(PERMISO_NOTAS_AJENAS);
}

const diaGt = new Intl.DateTimeFormat("en-CA", { timeZone: ZONA, year: "numeric", month: "2-digit", day: "2-digit" });
const fechaCorta = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, day: "numeric", month: "short" });

/** «hoy 10:42», «ayer 16:20» o «28 oct 14:05» (Guatemala). */
export function cuandoNota(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) {
        return "";
    }
    const hora = horaCorta(iso) ?? "";
    const dia = diaGt.format(ms);
    if (dia === diaGt.format(ahora)) {
        return `hoy ${hora}`;
    }
    if (dia === diaGt.format(ahora - 86_400_000)) {
        return `ayer ${hora}`;
    }
    return `${fechaCorta.format(ms).replace(".", "")} ${hora}`;
}

export interface MetaNota {
    autor: string;
    cuando: string;
    canal: string | null;
    editada: boolean;
    /** «Tú · hoy 10:42» (en la lista). */
    corta: string;
    /** «Tú · hoy 10:42 · desde el teléfono · editada» (al abrirla). */
    larga: string;
}

export function metaNota(nota: NotaOrden, usuarioId: number, ahora: number): MetaNota {
    const autor = notaPropia(nota, usuarioId) ? "Tú" : nota.autorNombre || "Despacho";
    const cuando = cuandoNota(nota.ocurridoEn, ahora);
    const canal = CANALES[nota.canal] ?? null;
    const editada = Boolean(nota.editadaEn);
    const corta = [autor, cuando].filter(Boolean).join(" · ");
    return { autor, cuando, canal, editada, corta, larga: [corta, ...(canal ? [canal] : []), ...(editada ? ["editada"] : [])].join(" · ") };
}

/** Las notas en orden de llegada (más vieja arriba), como en la orden del panel. */
export function notasOrdenadas(orden: Pick<OrdenVista, "notas">): NotaOrden[] {
    return [...(orden.notas ?? [])].sort((a, b) => a.ocurridoEn.localeCompare(b.ocurridoEn));
}
