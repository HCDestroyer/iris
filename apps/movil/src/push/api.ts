// Llamadas de la API para las notificaciones (`contratos/push.md` §1, §2 y §5) con la sesión del
// teléfono (cookie + CSRF del llavero) y la empresa fija de cada cliente. Las formas vienen de
// `@iris/contratos`.
import type { ClienteIris, Schema } from "@iris/contratos";
import { resolver } from "../api/errores";

export type Dispositivo = Schema<"Dispositivo">;
export type PreferenciasPush = Schema<"Preferencias">;
export type EntradaPreferenciasPush = Schema<"EntradaPreferencias">;

/** Lo que la app usa de la API de push: inyectable (las pruebas pasan una falsa). */
export interface ApiPush {
    registrarDispositivo(empresa: number | null, entrada: Schema<"EntradaRegistrarDispositivo">, clave: string): Promise<Dispositivo>;
    quitarDispositivo(empresa: number | null, id: number, senal?: AbortSignal): Promise<void>;
    leerPreferencias(): Promise<PreferenciasPush>;
    guardarPreferencias(entrada: EntradaPreferenciasPush, clave: string): Promise<PreferenciasPush>;
    /** Responder un mensaje del chat (contratos/chat.md §3): `id` uuid v7 = Idempotency-Key. */
    responderChat(empresa: number | null, conversacionId: number, id: string, texto: string): Promise<void>;
    leerChat(empresa: number | null, conversacionId: number, hastaMensajeId: string): Promise<void>;
    /** Nota interna en la bandeja con la mención de vuelta a quien escribió. */
    notaBandeja(empresa: number | null, conversacionId: number, id: string, texto: string, menciones: number[]): Promise<void>;
    leerBandeja(empresa: number | null, conversacionId: number, hastaMensajeId: string): Promise<void>;
}

export function crearApiPush(cliente: (empresa?: number | null) => ClienteIris): ApiPush {
    return {
        async registrarDispositivo(empresa, entrada, clave) {
            return resolver(cliente(empresa).POST("/api/v1/dispositivos", { body: entrada, params: { header: { "Idempotency-Key": clave } } }));
        },
        async quitarDispositivo(empresa, id, senal) {
            await resolver(cliente(empresa).POST("/api/v1/dispositivos/{id}/quitar", { params: { path: { id } }, ...(senal ? { signal: senal } : {}) }));
        },
        async leerPreferencias() {
            return resolver(cliente().GET("/api/v1/yo/notificaciones"));
        },
        async guardarPreferencias(entrada, clave) {
            return resolver(cliente().POST("/api/v1/yo/notificaciones", { body: entrada, params: { header: { "Idempotency-Key": clave } } }));
        },
        async responderChat(empresa, conversacionId, id, texto) {
            await resolver(
                cliente(empresa).POST("/api/v1/mensajeria/chat/conversaciones/{id}/mensajes", {
                    params: { path: { id: conversacionId }, header: { "Idempotency-Key": id } },
                    body: { id, texto, creado_en: new Date().toISOString() },
                }),
            );
        },
        async leerChat(empresa, conversacionId, hastaMensajeId) {
            await resolver(cliente(empresa).POST("/api/v1/mensajeria/chat/conversaciones/{id}/leer", { params: { path: { id: conversacionId } }, body: { hasta_mensaje_id: hastaMensajeId } }));
        },
        async notaBandeja(empresa, conversacionId, id, texto, menciones) {
            await resolver(
                cliente(empresa).POST("/api/v1/mensajeria/conversaciones/{id}/notas", {
                    params: { path: { id: conversacionId }, header: { "Idempotency-Key": id } },
                    body: { id, texto, menciones },
                }),
            );
        },
        async leerBandeja(empresa, conversacionId, hastaMensajeId) {
            await resolver(cliente(empresa).POST("/api/v1/mensajeria/conversaciones/{id}/leer", { params: { path: { id: conversacionId } }, body: { hastaMensajeId } }));
        },
    };
}
