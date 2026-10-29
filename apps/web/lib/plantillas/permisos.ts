import type { Schema } from "@iris/contratos";
import type { Permiso } from "../menu";

/**
 * Lo común (plantillas sin empresa) solo lo crea o modifica quien opera en todas las empresas
 * (`empresas.todas`); sin conector todo es común y basta el permiso de la acción (regla de la #20).
 */
export function puedeTocarComun(yo: Pick<Schema<"Yo">, "modoEmpresa">, puede: (p: readonly Permiso[]) => boolean): boolean {
    return yo.modoEmpresa === "sin_conector" || puede(["empresas.todas"]);
}

/** ¿Puede modificar esta plantilla con el permiso `permiso`? */
export function puedeModificar(p: { empresaId: number | null }, permiso: Permiso, yo: Pick<Schema<"Yo">, "modoEmpresa">, puede: (p: readonly Permiso[]) => boolean): boolean {
    return puede([permiso]) && (p.empresaId !== null || puedeTocarComun(yo, puede));
}
