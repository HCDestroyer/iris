import { ErrorApi } from "../api/errores";

/*
 * Errores de la API al usar un cliente o servicio de Argos (verificación en el servidor, PR #70):
 * 422 empresa_no_coincide, 503 argos_no_disponible y 429 cuota_argos, en palabras claras y con
 * qué hacer. `enCliente`: el mensaje va también junto al selector de cliente.
 */
export type ErrorCliente = { titulo: string; detalle: string; enCliente: boolean; reintentable: boolean };

function espera(segundos: number | undefined): string {
    if (!segundos || segundos <= 0) return "en un momento";
    return segundos < 90 ? `en ${segundos} s` : `en ${Math.ceil(segundos / 60)} min`;
}

export function errorDeCliente(error: unknown): ErrorCliente | null {
    if (!(error instanceof ErrorApi)) return null;
    switch (error.codigo) {
        case "empresa_no_coincide":
            return {
                titulo: "El cliente o el servicio no es de esta empresa",
                detalle: "Argos no lo tiene en la empresa activa (o ya no existe). Elige otro cliente o servicio, o cambia de empresa.",
                enCliente: true,
                reintentable: false,
            };
        case "argos_no_disponible":
            return {
                titulo: "Argos no responde",
                detalle: "No se pudo verificar el cliente y su servicio. Inténtalo en un momento o quita el servicio y captura el contacto a mano.",
                enCliente: false,
                reintentable: true,
            };
        case "cuota_argos":
            return {
                titulo: "Demasiadas consultas a Argos",
                detalle: `Se alcanzó el límite de consultas por minuto. Vuelve a intentarlo ${espera(error.reintentarEn)}.`,
                enCliente: false,
                reintentable: true,
            };
        default:
            return null;
    }
}
