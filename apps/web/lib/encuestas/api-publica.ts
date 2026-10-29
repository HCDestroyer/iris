import { crearCliente, type ClienteIris, type Schema } from "@iris/contratos";
import { resolver } from "../api/cliente";

/*
 * Cliente de la página pública `/encuesta`: otro actor que el panel. No manda cookies
 * (`credentials: "omit"`), ni `X-CSRF-Token` ni `X-Iris-Empresa`: una sesión del panel abierta en el
 * mismo navegador no cuenta (ni estorba) aquí. El token va en el cuerpo, nunca en la URL.
 */

export type EncuestaPublica = Schema<"EncuestaPublica">;

let cliente: ClienteIris | undefined;

export function apiEncuestaPublica(): ClienteIris {
    cliente ??= crearCliente({
        baseUrl: typeof window === "undefined" ? "http://localhost" : window.location.origin,
        credentials: "omit",
    });
    return cliente;
}

export function consultarEncuesta(token: string, signal?: AbortSignal): Promise<EncuestaPublica> {
    return resolver(apiEncuestaPublica().GET("/api/v1/encuestas/publica/{token}", { params: { path: { token } }, signal }));
}

export function responderEncuesta(token: string, cuerpo: { calificacion: number; comentario?: string }): Promise<EncuestaPublica> {
    return resolver(apiEncuestaPublica().POST("/api/v1/encuestas/publica/{token}/responder", { params: { path: { token } }, body: cuerpo }));
}
