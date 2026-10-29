import type { Schema } from "@iris/contratos";
import type { Permiso } from "../menu";

type YoComun = Pick<Schema<"Yo">, "modoEmpresa" | "todasLasEmpresas">;

/**
 * Lo común (plantillas sin empresa) solo lo crea o modifica quien tiene `empresas.todas`
 * **global** (`todasLasEmpresas` de `GET /yo`; con alcance no basta); sin conector todo es común y
 * basta el permiso de la acción (regla de la #20).
 */
export function puedeTocarComun(yo: YoComun): boolean {
    return yo.modoEmpresa === "sin_conector" || yo.todasLasEmpresas;
}

/** ¿Puede modificar esta plantilla con el permiso `permiso`? */
export function puedeModificar(p: { empresaId: number | null }, permiso: Permiso, yo: YoComun, puede: (p: readonly Permiso[]) => boolean): boolean {
    return puede([permiso]) && (p.empresaId !== null || puedeTocarComun(yo));
}
