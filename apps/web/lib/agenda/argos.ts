import type { Schema } from "@iris/contratos";
import { ErrorApi } from "@/lib/api/errores";

/** Texto fijo cuando Argos no tiene la capacidad (contrato 0.5.0). */
export const SIN_INVENTARIO_POR_TECNICO = "Argos < 1.6.0: inventario por técnico no disponible";

/**
 * Con los conectores leídos: `true` si el de Argos activo informa `inventario_por_tecnico`,
 * `false` si no, `null` si no hay conector de Argos activo (no se sabe: lo dirá la API al vincular).
 */
export function conInventarioPorTecnico(conectores: readonly Pick<Schema<"Conector">, "sistema" | "estado" | "capacidades">[]): boolean | null {
    const argos = conectores.find((c) => c.sistema === "argosisp" && c.estado === "activo");
    if (!argos) return null;
    return (argos.capacidades ?? []).includes("inventario_por_tecnico");
}

/** Una opción del selector de técnicos de Argos: texto y si se puede elegir para esta ficha. */
export function opcionTecnicoArgos(a: Schema<"TecnicoDeArgos">, tecnicoId: number): { texto: string; elegible: boolean } {
    const otraFicha = a.vinculadoA !== undefined && a.vinculadoA.tecnicoId !== tecnicoId;
    const bodegas = (a.bodegas ?? []).filter((b) => b.activa).map((b) => b.id);
    const partes = [a.nombre, a.correo, bodegas.length > 0 ? `bodega ${bodegas.join(", ")}` : "sin bodega activa"];
    if (!a.enEmpresa) partes.push("sin bodega en esta empresa");
    if (otraFicha) partes.push("ya vinculado a otra ficha");
    return { texto: partes.join(" · "), elegible: a.enEmpresa && !otraFicha };
}

/** Explicación de los errores de negocio de `vincular-argos`; `undefined` si es otro. */
export function textoErrorVinculo(error: unknown): string | undefined {
    if (!(error instanceof ErrorApi)) return undefined;
    switch (error.codigo) {
        case "argos_sin_inventario_tecnico":
            return SIN_INVENTARIO_POR_TECNICO;
        case "tecnico_no_encontrado":
            return "Argos ya no tiene ese usuario: vuelve a abrir la lista.";
        case "tecnico_sin_bodega":
            return "Ese usuario no tiene bodega técnico activa en esta empresa: dásela en Argos.";
        case "argos_vinculado_a_otro":
            return "Ese usuario de Argos ya está vinculado a otra ficha: desvincúlalo allá primero.";
        default:
            return undefined;
    }
}
