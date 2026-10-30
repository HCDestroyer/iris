import type { Schema } from "@iris/contratos";

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
