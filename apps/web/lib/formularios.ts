import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { ErrorApi } from "./api/errores";

/**
 * Lleva los `errors[]` de un 422 a los campos del formulario (`body.asunto` → `asunto`,
 * `body.contacto.correo` → `contacto.correo`). Devuelve `true` si colocó alguno; si no, el error
 * se muestra como aviso general.
 */
export function erroresACampos<T extends FieldValues>(error: unknown, campos: readonly Path<T>[], setError: UseFormSetError<T>): boolean {
    if (!(error instanceof ErrorApi) || error.estado !== 422) {
        return false;
    }
    let alguno = false;
    for (const e of error.errores) {
        // `body.lineas[0].precio` (API) → `lineas.0.precio` (react-hook-form).
        const campo = e.field.replace(/^body\./, "").replace(/\[(\d+)\]/g, ".$1") as Path<T>;
        if (campos.includes(campo)) {
            setError(campo, { type: "servidor", message: e.message });
            alguno = true;
        }
    }
    return alguno;
}
