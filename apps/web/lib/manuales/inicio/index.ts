import { MANUALES_FIBRA } from "./fibra";
import { MANUALES_GENERALES } from "./generales";
import { MANUALES_INALAMBRICO } from "./inalambrico";
import type { EntradaManual, PlantillaManual, TecnologiaManual } from "./tipos";

export type { EntradaManual, PlantillaManual, TecnologiaManual } from "./tipos";

/*
 * Manuales de inicio para un ISP con fibra (FTTH) e inalámbrico (WISP): ejemplos editables que se
 * cargan desde la biblioteca («Cargar manuales de inicio», `manuales.editar`). Viven en el panel
 * como datos tipados y se crean con el `POST /mensajeria/manuales` de siempre, uno por llamada.
 */

const porCodigo = (codigo: string): PlantillaManual => {
    const p = [...MANUALES_GENERALES, ...MANUALES_FIBRA, ...MANUALES_INALAMBRICO].find((m) => m.codigo === codigo);
    if (!p) throw new Error(`Manual de inicio desconocido: ${codigo}`);
    return p;
};

/** Todos, en el orden en que se muestran y se crean: de lo general a lo particular. */
export const MANUALES_INICIO: readonly PlantillaManual[] = [
    "sin-internet-primeros-pasos",
    "reiniciar-equipos",
    "ftth-luces-ont",
    "ftth-cuidados-fibra",
    "wisp-antena-poe",
    "wisp-senal-baja",
    "medir-velocidad",
    "mejorar-wifi",
    "cambiar-nombre-clave-wifi",
    "cortes-de-energia",
    "visita-tecnica",
    "pagos-y-reconexion",
    "traslado-de-domicilio",
    "seguridad-y-estafas",
].map(porCodigo);

export const NOMBRE_TECNOLOGIA: Record<TecnologiaManual, string> = { general: "General", ftth: "Fibra (FTTH)", wisp: "Inalámbrico (WISP)" };

/** Cuerpo del alta: «Para el cliente», sin modelo de equipo. */
export function entradaDe(p: PlantillaManual): EntradaManual {
    return { titulo: p.titulo, resumen: p.resumen, cuerpo: p.cuerpo, etiquetas: p.etiquetas, equipo: "", visibilidad: p.visibilidad };
}

/**
 * `Idempotency-Key` estable del alta: la misma plantilla en la misma empresa repite la respuesta
 * guardada (24 h) en lugar de crear otra. Lleva la empresa porque la clave se guarda por usuario,
 * no por empresa, y la revisión porque con otro cuerpo el servidor respondería 422.
 */
export function claveManualInicio(empresaId: number | undefined, p: PlantillaManual): string {
    return `manual-inicio:${empresaId ?? 0}:${p.codigo}:r${p.revision}`;
}

/** Título comparable: sin mayúsculas, acentos ni espacios de más (como lo vería una persona). */
export function tituloComparable(titulo: string): string {
    return titulo.normalize("NFD").replace(/\p{Diacritic}/gu, "").toLowerCase().replace(/\s+/g, " ").trim();
}

/** Las plantillas cuyo título ya está en la biblioteca (activos o no). */
export function yaExistentes(plantillas: readonly PlantillaManual[], titulosBiblioteca: Iterable<string>): Set<string> {
    const titulos = new Set([...titulosBiblioteca].map(tituloComparable));
    return new Set(plantillas.filter((p) => titulos.has(tituloComparable(p.titulo))).map((p) => p.codigo));
}

/** «12 manuales creados», «1 manual creado». */
export function textoResultadoCarga(creados: number, omitidos: readonly string[]): { titulo: string; descripcion?: string } {
    const titulo = creados === 0 ? "No se creó ningún manual" : creados === 1 ? "1 manual de inicio creado" : `${creados} manuales de inicio creados`;
    if (omitidos.length === 0) return { titulo };
    const primeros = omitidos.slice(0, 2).map((t) => `«${t}»`);
    const lista = omitidos.length > 2 ? `${primeros.join(", ")} y ${omitidos.length - 2} más` : primeros.join(" y ");
    return {
        titulo,
        descripcion: omitidos.length === 1 ? `Se omitió 1 que ya estaba en la biblioteca: ${lista}.` : `Se omitieron ${omitidos.length} que ya estaban en la biblioteca: ${lista}.`,
    };
}
