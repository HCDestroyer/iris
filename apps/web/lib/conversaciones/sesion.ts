import type { ClienteIris } from "@iris/contratos";
import { api, resolver } from "../api/cliente";

/*
 * Verificación de la sesión antes de guardar o enviar algo de la bandeja (lección del chat interno
 * y de la app de campo): el panel puede quedar abierto mientras en otra pestaña se cierra la sesión
 * y entra otra persona, o cambia la empresa. Las cookies son del navegador, no de la pestaña: sin
 * esta comprobación, un mensaje escrito por Andrea saldría a nombre de quien entró después, o un
 * borrador quedaría guardado para otra cuenta.
 */

export class SesionCambiada extends Error {
    constructor(readonly motivo: "otro_usuario" | "otra_empresa") {
        super(motivo === "otro_usuario" ? "La sesión de este navegador ahora es de otra persona" : "La sesión de este navegador ahora trabaja en otra empresa");
        this.name = "SesionCambiada";
    }
}

export type Identidad = { usuarioId: number; empresaId: number | undefined };

/**
 * `GET /yo` y compara con quien abrió la pantalla. Lanza `SesionCambiada`, o el `ErrorApi` de la
 * lectura (401 sin sesión, 0 sin red): en todos los casos **no se envía nada**.
 */
export async function verificarSesion(identidad: Identidad, cliente: ClienteIris = api()): Promise<void> {
    const yo = await resolver(cliente.GET("/api/v1/yo"));
    if (yo.usuario.id !== identidad.usuarioId) {
        throw new SesionCambiada("otro_usuario");
    }
    // También sin empresa: si quien abrió no tenía empresa activa y ahora hay una (o al revés), cambió.
    if (yo.empresaActiva?.id !== identidad.empresaId) {
        throw new SesionCambiada("otra_empresa");
    }
}

export const esSesionCambiada = (e: unknown): e is SesionCambiada => e instanceof SesionCambiada;
