import { ErrorApi, mensajeDeError } from "@/lib/api/errores";

/** ¿Es un 409 por `version`? (no el de una clave de idempotencia aún en proceso). */
export function esConflicto(error: unknown): boolean {
    return error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";
}

/**
 * Texto de un error de escritura en buzones. El 403 más común es el de alcance: las escrituras y
 * `probar` exigen `buzones.*` con alcance global, y un supervisor de zona o cuadrilla lo recibe.
 */
export function textoErrorBuzon(error: unknown): { titulo: string; detalle: string } {
    if (error instanceof ErrorApi && error.estado === 403 && error.codigo === "prohibido") {
        return {
            titulo: "Tu permiso no alcanza para administrar buzones",
            detalle:
                "Crear, editar, activar, desactivar o probar un buzón exige el permiso con alcance global, y el tuyo está limitado a una zona o cuadrilla. Pide a un administrador un rol con alcance global.",
        };
    }
    if (error instanceof ErrorApi && error.codigo === "cifrado_no_configurado") {
        return { titulo: "Iris no puede guardar contraseñas", detalle: "Falta configurar IRIS_CLAVE_CIFRADO en el servidor. Avisa a quien administra Iris." };
    }
    if (error instanceof ErrorApi && error.codigo === "destino_no_permitido") {
        return { titulo: "Servidor no permitido", detalle: "El servidor está en una red interna que Iris no tiene permitida." };
    }
    const { titulo, detalle } = mensajeDeError(error);
    return { titulo, detalle: detalle ?? "" };
}
