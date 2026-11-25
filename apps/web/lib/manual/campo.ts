// Claves del manual en la app del técnico (`/campo`), las mismas de la tabla del manual (PR #275,
// `docs/manual/claves.md`). Dentro de `/campo` el «?» abre `/campo/manual/<pantalla>`: el service
// worker solo controla `/campo`, así el manual abre sin señal.
import { rutaManual } from "@iris/ui";
import type { PasoOrden, Vista } from "@iris/sync/campo";

export const CLAVES_CAMPO: Readonly<Record<Vista["tipo"], string>> = {
    lista: "campo/hoy",
    orden: "campo/orden",
    pendientes: "campo/por-enviar",
    recibir: "campo/por-recibir",
    traslado: "campo/por-recibir",
    yo: "campo/yo",
    bodega: "campo/bodega",
    agenda: "campo/agenda",
    semana: "campo/agenda",
    chat: "campo/chat",
    chat_nuevo: "campo/chat",
    conversacion: "campo/chat",
    nueva_tarea: "campo/acciones-rapidas",
    libres: "campo/acciones-rapidas",
};

/** Cada paso de la orden tiene su página (la firma, con el cierre). */
export const CLAVES_PASO: Readonly<Record<PasoOrden, string>> = {
    llegada: "campo/llegada",
    checklist: "campo/checklist",
    materiales: "campo/materiales",
    fotos: "campo/fotos",
    cierre: "campo/cierre",
};

/** Clave del manual de la vista abierta de la app de campo. */
export function claveManualCampo(vista: Vista): string {
    if (vista.tipo === "orden" && vista.paso !== undefined) return CLAVES_PASO[vista.paso];
    return CLAVES_CAMPO[vista.tipo];
}

/** Enlace del «?» dentro de `/campo`: `campo/hoy` → `/campo/manual/hoy`. */
export function rutaManualCampo(clave: string): string {
    return rutaManual(clave, "/campo/manual");
}
