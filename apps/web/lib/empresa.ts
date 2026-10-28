import { borrarLocal, escribirLocal, leerLocal } from "./almacen";

/*
 * Empresa activa (multiempresa, ver .claude/integration/i1.md): la elegida se guarda en el
 * navegador y el cliente HTTP la manda en `X-Iris-Empresa` en toda petición a la API. Sin
 * elección guardada no se manda la cabecera (con una sola empresa disponible, la API usa esa).
 */

export const CABECERA_EMPRESA = "X-Iris-Empresa";
const CLAVE = "iris-empresa";

/** Problemas de empresa que la API devuelve en cualquier ruta operativa. */
export type ProblemaEmpresa = "empresa_requerida" | "empresa_no_permitida" | "sin_empresa";
const CODIGOS: ReadonlySet<string> = new Set<ProblemaEmpresa>(["empresa_requerida", "empresa_no_permitida", "sin_empresa"]);

export function esProblemaEmpresa(codigo: string): codigo is ProblemaEmpresa {
    return CODIGOS.has(codigo);
}

// La elección se guarda también en memoria: si el almacenamiento falla, dura la visita.
let enMemoria: string | undefined;
let leida = false;

export function empresaGuardada(): number | undefined {
    if (!leida) {
        enMemoria = leerLocal(CLAVE);
        leida = true;
    }
    const id = Number(enMemoria);
    return enMemoria && Number.isSafeInteger(id) && id > 0 ? id : undefined;
}

export function guardarEmpresa(id: number): void {
    enMemoria = String(id);
    leida = true;
    escribirLocal(CLAVE, enMemoria);
}

export function limpiarEmpresa(): void {
    enMemoria = undefined;
    leida = true;
    borrarLocal(CLAVE);
}

type Oyente = (problema: ProblemaEmpresa) => void;
const oyentes = new Set<Oyente>();

/** El panel escucha los 422/403 de empresa de cualquier petición para reaccionar en un solo lugar. */
export function suscribirProblemasEmpresa(oyente: Oyente): () => void {
    oyentes.add(oyente);
    return () => {
        oyentes.delete(oyente);
    };
}

export function notificarProblemaEmpresa(problema: ProblemaEmpresa): void {
    for (const oyente of oyentes) {
        oyente(problema);
    }
}
