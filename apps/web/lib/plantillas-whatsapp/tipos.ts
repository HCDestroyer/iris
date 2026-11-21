import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import type { Permiso } from "@/lib/menu";

/*
 * Plantillas de WhatsApp por cuenta (`.claude/integration/contratos/plantillas-whatsapp.md`). Los
 * tipos vienen de `packages/contratos`; aquí solo los textos en llano y lo que se calcula para mostrar.
 */

export type PlantillaWA = Schema<"PlantillaWA">;
export type PlantillaWADetalle = Schema<"PlantillaWADetalle">;
export type DefinicionPWA = Schema<"DefinicionPWA">;
export type VariablePWA = Schema<"VariablePWA">;
export type BotonPWA = Schema<"BotonPWA">;
export type EncabezadoPWA = Schema<"EncabezadoPWA">;
export type DatoPWA = Schema<"DatoPWA">;
export type CuentaPWA = Schema<"CuentaPWA">;
export type ResumenSincronizacion = Schema<"ResumenSincronizacionPWA">;
export type SugeridaEnCuenta = Schema<"SugeridaEnCuentaPWA">;
export type ReglaPWA = Schema<"ReglaPWA">;
export type Categoria = PlantillaWA["categoria"];
export type EstadoPlantilla = PlantillaWA["estado"];
export type Uso = "avisos" | "conversaciones" | "asistente";

export const PERMISO_VER: Permiso = "plantillas_whatsapp.ver";
export const PERMISO_SINCRONIZAR: Permiso = "plantillas_whatsapp.sincronizar";
export const PERMISO_CREAR: Permiso = "plantillas_whatsapp.crear";
export const PERMISO_EDITAR: Permiso = "plantillas_whatsapp.editar";
export const PERMISO_BORRAR: Permiso = "plantillas_whatsapp.borrar";

export const RUTA = "/administracion/plantillas-whatsapp";

export const CATEGORIAS: readonly { valor: Categoria; texto: string; detalle: string }[] = [
    { valor: "UTILITY", texto: "Servicio", detalle: "Avisos de la cuenta del cliente: factura, pago, visita, corte. Sin ofertas." },
    { valor: "MARKETING", texto: "Marketing", detalle: "Promociones, planes nuevos o encuestas generales. Meta cobra más." },
    { valor: "AUTHENTICATION", texto: "Verificación", detalle: "Solo un código para entrar. Meta pone el texto." },
];

export function textoCategoria(c: string | null | undefined): string {
    return CATEGORIAS.find((x) => x.valor === c)?.texto ?? (c ?? "");
}

export const IDIOMAS: readonly { valor: string; texto: string }[] = [
    { valor: "es", texto: "Español" },
    { valor: "es_MX", texto: "Español (México)" },
    { valor: "es_ES", texto: "Español (España)" },
    { valor: "en", texto: "Inglés" },
    { valor: "en_US", texto: "Inglés (EE. UU.)" },
];

export function textoIdioma(i: string): string {
    return IDIOMAS.find((x) => x.valor === i)?.texto ?? i;
}

export const USOS: readonly { valor: Uso; texto: string; detalle: string }[] = [
    { valor: "avisos", texto: "Avisos automáticos", detalle: "Los avisos que pide Argos o Iris (corte, recordatorio, visita…)." },
    { valor: "conversaciones", texto: "Iniciar conversación", detalle: "Los agentes la ven al escribir primero a un cliente." },
    { valor: "asistente", texto: "Asistente", detalle: "El asistente de IA puede usarla." },
];

export function textoUso(u: string): string {
    return ({ avisos: "Avisos", conversaciones: "Conversaciones", asistente: "Asistente" } as Record<string, string>)[u] ?? u;
}

/** Nombre en llano de los códigos del catálogo de Iris (lo que Argos pide por código). */
export const CODIGOS: Record<string, string> = {
    factura_por_vencer: "Factura por vencer",
    factura_vencida: "Factura vencida",
    pago_recibido: "Pago recibido",
    corte_por_falta_de_pago: "Corte por falta de pago",
    servicio_reactivado: "Servicio reactivado",
    bienvenida: "Bienvenida",
    corte_programado: "Corte programado (mantenimiento)",
    boleta_recibida: "Boleta recibida",
    boleta_confirmada: "Boleta confirmada",
    boleta_rechazada: "Boleta rechazada",
    boleta_falta_dato: "Boleta: falta un dato",
    reconexion_temporal: "Reconexión temporal",
    promesa_aceptada: "Promesa de pago aceptada",
};

export function textoCodigo(c: string | null | undefined): string {
    return c ? (CODIGOS[c] ?? c) : "";
}

/** «Factura por vencer» y «Factura vencida». */
export function textoCodigos(cs: readonly string[]): string {
    const t = cs.map((c) => `«${textoCodigo(c)}»`);
    return t.length <= 1 ? (t[0] ?? "") : `${t.slice(0, -1).join(", ")} y ${t[t.length - 1]}`;
}

const HORA = new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
const DIA = new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", day: "numeric", month: "short" });
const DIA_CLAVE = new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala" });

/** «Hoy 10:42», «Ayer 17:05» o «12 nov» (hora de Guatemala). */
export function cuandoCorto(iso: string | null | undefined, ahora: number = Date.now()): string {
    if (!iso) return "—";
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "—";
    const hoy = DIA_CLAVE.format(ahora);
    const ayer = DIA_CLAVE.format(ahora - 86_400_000);
    const dia = DIA_CLAVE.format(ms);
    if (dia === hoy) return `Hoy ${HORA.format(ms)}`;
    if (dia === ayer) return `Ayer ${HORA.format(ms)}`;
    return DIA.format(ms).replace(".", "");
}

export function hora(iso: string): string {
    return HORA.format(Date.parse(iso));
}

/** El estado en llano con su tono (un estado siempre va con icono + texto + color). */
export function estadoVisible(p: Pick<PlantillaWA, "estado" | "pausadaHasta" | "borradaPor">): { texto: string; tono: Tono; atencion: boolean } {
    switch (p.estado) {
        case "BORRADOR":
            return { texto: "Borrador", tono: "neutral", atencion: false };
        case "PENDING":
            return { texto: "En revisión", tono: "info", atencion: false };
        case "IN_APPEAL":
            return { texto: "En apelación", tono: "info", atencion: false };
        case "APPROVED":
            return { texto: "Aprobada", tono: "success", atencion: false };
        case "FLAGGED":
            return { texto: "Aprobada · calidad baja", tono: "warning", atencion: true };
        case "REJECTED":
            return { texto: "Rechazada · corregir", tono: "error", atencion: true };
        case "PAUSED":
            return { texto: p.pausadaHasta ? `Pausada hasta ${hora(p.pausadaHasta)}` : "Pausada", tono: "warning", atencion: true };
        case "DISABLED":
            return { texto: "Deshabilitada por Meta", tono: "error", atencion: true };
        case "LIMIT_EXCEEDED":
            return { texto: "Límite de Meta", tono: "error", atencion: true };
        case "LOCKED":
            return { texto: "Bloqueada por Meta", tono: "error", atencion: true };
        case "ARCHIVED":
            return { texto: "Archivada", tono: "neutral", atencion: false };
        case "PENDING_DELETION":
            return { texto: "Borrándose", tono: "neutral", atencion: false };
        case "DELETED":
            return p.borradaPor === "meta" ? { texto: "Borrada en Meta", tono: "error", atencion: true } : { texto: "Borrada", tono: "neutral", atencion: false };
    }
    return { texto: p.estado, tono: "neutral", atencion: false };
}

export function calidadVisible(p: Pick<PlantillaWA, "estado" | "calidad">): { texto: string; tono: Tono } | null {
    if (!["APPROVED", "FLAGGED", "PAUSED"].includes(p.estado)) return null;
    switch (p.calidad) {
        case "GREEN":
            return { texto: "Alta", tono: "success" };
        case "YELLOW":
            return { texto: "Media", tono: "warning" };
        case "RED":
            return { texto: "Baja", tono: "error" };
    }
    return { texto: "Sin datos aún", tono: "neutral" };
}

/** Por qué hay que atenderla, en llano (o null). */
export function textoAtender(p: Pick<PlantillaWA, "atender" | "codigos" | "categoria" | "categoriaAnterior" | "categoriaProxima">): string | null {
    switch (p.atender) {
        case "borrada_en_uso":
            return p.codigos.length > 0
                ? `Ya no está en Meta. Se usaba en ${textoCodigos(p.codigos)}: elige otra.`
                : "Ya no está en Meta y se usaba: elige otra.";
        case "rechazada":
            return "Meta la rechazó: corrígela y vuelve a enviarla.";
        case "pausada":
            return "Meta la pausó por la calidad: mientras tanto no sale.";
        case "deshabilitada":
            return "Meta la deshabilitó: crea otra.";
        case "bloqueada":
            return "Meta la bloqueó.";
        case "reclasificada":
            return `Meta la cambió de ${textoCategoria(p.categoriaAnterior)} a ${textoCategoria(p.categoria)}${p.categoria === "MARKETING" ? ": Meta cobra más por cada envío" : ""}.`;
        case "reclasificacion_proxima":
            return `Meta avisó que la cambiará a ${textoCategoria(p.categoriaProxima)}.`;
    }
    return null;
}

/** Usos en llano para la columna «Se usa en». */
export function usosVisibles(p: Pick<PlantillaWA, "usos">): string[] {
    return p.usos.map(textoUso);
}
