import { ErrorApi } from "@/lib/api/errores";

/*
 * Errores de la cuenta de WhatsApp (WABA) al guardar un número (contratos/plantillas-whatsapp.md §1.2):
 * una cuenta es de una sola empresa y su WABA se comprueba en Meta con el token del número.
 */

/** El mensaje para el campo «WABA ID», o null si el error no es de la cuenta. */
export function errorDeWaba(e: unknown): string | null {
    if (!(e instanceof ErrorApi)) return null;
    if (e.estado === 409 && e.codigo === "waba_de_otra_empresa")
        return "Esa cuenta de WhatsApp ya la usan números de otra empresa. Cada cuenta es de una sola empresa; para compartir un número, la otra empresa te lo presta.";
    if (e.estado === 422 && e.codigo === "waba_no_coincide") return "Meta dice que este Phone number ID no es de esa cuenta. Copia los dos datos de la misma pantalla de Meta.";
    return null;
}
