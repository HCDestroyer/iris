// Envío de un mensaje con menciones (`contratos/chat.md` §3.2). La cola del `MotorChat` aún no lleva
// `menciones` (pedido a I4 en `.claude/integration/i3.md`): esos mensajes salen directo por el
// transporte con un id uuid v7 que se repite tras un error de red o 5xx (el servidor deduplica por
// id) y se renueva tras un 4xx. Los demás mensajes van por la cola del motor.
import { ErrorTransporte } from "@iris/sync";
import type { CuerpoEnviarMensaje, TransporteChat } from "@iris/sync/chat";
import { nuevoIdMensaje } from "../conversaciones/api";

export type CuerpoConMenciones = CuerpoEnviarMensaje & { menciones: number[] };

/** Id del siguiente intento: el mismo sin respuesta o con 5xx; uno nuevo tras un 4xx. */
export function idTrasErrorChat(error: unknown, actual: string, nuevo: () => string = nuevoIdMensaje): string {
    return error instanceof ErrorTransporte && error.status !== null && error.status >= 400 && error.status < 500 ? nuevo() : actual;
}

export async function enviarConMenciones(transporte: TransporteChat, conversacionId: number, id: string, texto: string, menciones: readonly number[]): Promise<void> {
    // El tipo del contrato aún no trae `menciones`; viaja igual en el JSON (ver provisional.ts).
    const cuerpo: CuerpoConMenciones = { id, texto, creado_en: new Date().toISOString(), menciones: [...menciones] };
    await transporte.enviarMensaje(conversacionId, cuerpo);
}
