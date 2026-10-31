// Lo que queda en el teléfono sin enviar (para «Cerrar sesión» y «Cambiar de empresa» en «Yo»):
// la cola, los rechazos, lo que espera archivos y los archivos por subir. Sin React.
import type { EstadoSync } from "@iris/sync";

export function porEnviarEnTelefono(estado: Pick<EstadoSync, "pendientes" | "rechazos" | "esperandoAdjuntos" | "subidas">): number {
    return estado.pendientes + estado.rechazos.length + estado.esperandoAdjuntos.length + estado.subidas.pendientes;
}
