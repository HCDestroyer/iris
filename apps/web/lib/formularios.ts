import { asignarErrores } from "@iris/ui";
import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { ErrorApi } from "./api/errores";

/**
 * Lleva los `errors[]` de un 422 a los campos del formulario (`body.asunto` → `asunto`,
 * `body.contacto.correo` → `contacto.correo`), con `asignarErrores` de `@iris/ui`. Devuelve
 * `true` solo si todos quedaron en un campo; si alguno no tiene campo visible, devuelve `false`
 * y la pantalla muestra el aviso general, que lista los restantes (`mensajeDeError`).
 */
export function erroresACampos<T extends FieldValues>(error: unknown, campos: readonly Path<T>[], setError: UseFormSetError<T>): boolean {
    if (!(error instanceof ErrorApi) || error.estado !== 422 || error.errores.length === 0) {
        return false;
    }
    const restantes = asignarErrores(error.errores, campos, (campo, e) => setError(campo, e));
    return restantes.length === 0;
}
