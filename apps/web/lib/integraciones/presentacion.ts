import type { Schema } from "@iris/contratos";
import { z } from "zod";

export type Conector = Schema<"Conector">;
export type Sistema = Conector["sistema"];
export type Previsualizacion = Schema<"Previsualizacion">;

export const SISTEMAS: Record<Sistema, string> = { argosisp: "Argos", horus: "Horus Flow" };

/** El conector activo del mismo sistema que se apagaría al activar `c` (a lo más uno por sistema). */
export function activoQueReemplaza(c: Pick<Conector, "id" | "sistema">, todos: readonly Conector[]): Conector | undefined {
    return todos.find((x) => x.sistema === c.sistema && x.estado === "activo" && x.id !== c.id);
}

/** Por qué no se permite un origen (`Previsualizacion.motivo`). */
export function textoMotivoOrigen(motivo: string | null): string {
    switch (motivo) {
        case "origenes_no_configurados":
            return "Iris no tiene configurados los orígenes permitidos para este sistema (IRIS_ARGOS_ORIGENES / IRIS_HORUS_ORIGENES). Pide al equipo técnico que agregue este origen.";
        case "origen_no_permitido":
            return "Este origen no está en la lista de orígenes permitidos ni es el del conector activo. No conectes con un enlace que no esperabas.";
        default:
            return motivo ? `No se permite este origen (${motivo}).` : "No se permite este origen.";
    }
}

/** El enlace parece de conexión (`https://<sistema>/conectar#<código>`); la API valida el resto. */
export function enlaceValido(enlace: string): boolean {
    try {
        const u = new URL(enlace.trim());
        return (u.protocol === "https:" || u.protocol === "http:") && u.hash.length > 1;
    } catch {
        return false;
    }
}

export type AutorizacionCompletada = Schema<"AutorizacionCompletada">;

/**
 * Dirección del panel de Argos para «Conectar con Argos»: http(s) con host, sin usuario, consulta
 * ni fragmento. Devuelve el origen (`https://host[:puerto]`, como lo normaliza la API) o `null`.
 */
export function origenPanel(crudo: string): string | null {
    let u: URL;
    try {
        u = new URL(crudo.trim());
    } catch {
        return null;
    }
    if ((u.protocol !== "https:" && u.protocol !== "http:") || !u.hostname || u.username || u.password || u.search || u.hash) {
        return null;
    }
    return u.origin;
}

/** Lo que trae el fragmento de `/administracion/integraciones/argos/vuelta` (contrato 0.4.0 §1.4). */
export type Vuelta = { state: string; codigo: string; api: string } | { state: string; error: string };

/** Lee el fragmento de la vuelta; `null` si no trae un `state` con `codigo` y `api`, o con `error`. */
export function leerVuelta(hash: string): Vuelta | null {
    const p = new URLSearchParams(hash.startsWith("#") ? hash.slice(1) : hash);
    const state = p.get("state")?.trim() ?? "";
    if (!state || state.length > 200) return null;
    const error = p.get("error")?.trim();
    if (error) return { state, error: error.slice(0, 100) };
    const codigo = p.get("codigo")?.trim() ?? "";
    const api = p.get("api")?.trim() ?? "";
    if (!codigo || !api || codigo.length > 200 || api.length > 500) return null;
    return { state, codigo, api };
}

/** Título y texto de un error de «completar» (por código del problem+json). */
export function mensajeVuelta(codigo: string | undefined, estado: number): { titulo: string; texto: string } {
    switch (codigo) {
        case "rechazado":
            return { titulo: "Rechazaste la conexión en Argos", texto: "No se conectó nada. Si fue un error, vuelve a intentarlo y pulsa «Aprobar» en Argos." };
        case "state_vencido":
            return { titulo: "La autorización venció", texto: "Pasaron más de 10 minutos desde que pulsaste «Conectar con Argos». Vuelve a intentarlo." };
        case "state_invalido":
            return {
                titulo: "Esta vuelta de Argos no es válida",
                texto: "Ya se usó, no la inició esta sesión o no la inició Iris. Si tú pulsaste «Conectar con Argos», vuelve a intentarlo desde aquí.",
            };
        case "origen_no_permitido":
            return {
                titulo: "La API de Argos no está permitida",
                texto: "La dirección de la API que mandó Argos no está en IRIS_ARGOS_ORIGENES. Pide al equipo técnico que la agregue y vuelve a intentarlo.",
            };
        case "origenes_no_configurados":
            return { titulo: "Faltan los orígenes de Argos", texto: "Iris no tiene configurado IRIS_ARGOS_ORIGENES. Pide al equipo técnico que agregue la API de Argos." };
        case "codigo_invalido":
            return { titulo: "Argos no aceptó el código", texto: "El código venció, ya se usó o no corresponde a esta autorización. Vuelve a intentarlo." };
        case "argos_no_disponible":
            return { titulo: "Argos no responde", texto: "No se pudo completar el canje con Argos. Vuelve a intentarlo en unos minutos." };
        default:
            if (estado === 0) return { titulo: "Sin conexión con Iris", texto: "No se pudo enviar la respuesta de Argos. Revisa tu conexión y reintenta." };
            return { titulo: "No se pudo conectar con Argos", texto: "Vuelve a intentarlo. Si se repite, revisa la configuración del conector." };
    }
}

/** Alcance que informó el canje (`alcanceGlobal`). */
export function textoAlcance(alcanceGlobal: boolean | undefined): string {
    if (alcanceGlobal === true) return "Todas las empresas, también las que se creen después en Argos";
    if (alcanceGlobal === false) return "Solo las empresas de la lista";
    return "Argos no lo informó";
}

/**
 * Panel del conector para mostrar: el que informó el canje o el configurado. `null` si solo se
 * conoce la API (conectores anteriores a Argos 0.4.0).
 */
export function panelDe(c: Pick<Conector, "panelUrl" | "urlPublica" | "apiUrl">): string | null {
    if (c.panelUrl) return c.panelUrl;
    return c.urlPublica && c.urlPublica !== c.apiUrl ? c.urlPublica : null;
}

/** Clave de sessionStorage con el último panel usado en «Conectar con Argos» (para «Reintentar»). */
export const CLAVE_PANEL_ARGOS = "iris.integraciones.panel-argos";

/** Formulario «Conectar con Argos»: espejo de `EntradaAutorizar` (la API normaliza al origen). */
export const esquemaPanelArgos = z.object({
    panelUrl: z
        .string()
        .trim()
        .min(1, "Escribe la dirección del panel de Argos")
        .max(500, "La dirección es demasiado larga")
        .refine((v) => origenPanel(v) !== null, "Debe ser la dirección del panel: https://argos.tu-isp.com"),
});
export type DatosPanelArgos = z.infer<typeof esquemaPanelArgos>;
