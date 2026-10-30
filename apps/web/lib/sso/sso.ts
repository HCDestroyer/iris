import { crearCliente, type ClienteIris, type Schema } from "@iris/contratos";
import { rutaSegura } from "../tiempo";

/*
 * Autologin desde Argos («Ver en Iris», i1.md «Autologin desde Argos»): `/sso/entrar#<ticket>`
 * canjea el ticket (60 s, un solo uso) y sigue según `CanjeSSO.estado`.
 */

export type CanjeSSO = Schema<"CanjeSSO">;
export type UsuarioExterno = Schema<"UsuarioExternoVisto">;

/**
 * Cliente del canje: manda las cookies (la sesión que ya hubiera, para `confirmar_cambio`, y la de
 * vinculación que deja la API en `/api/v1/sso`), pero **ninguna** cabecera del panel: ni
 * `X-CSRF-Token` (el canje es como el login) ni `X-Iris-Empresa`.
 */
let cliente: ClienteIris | undefined;
export function apiSSO(): ClienteIris {
    cliente ??= crearCliente({
        baseUrl: typeof window === "undefined" ? "http://localhost" : window.location.origin,
        credentials: "include",
    });
    return cliente;
}

const DESTINO = /^\/(tickets|ordenes|comercial)\/\d+$/;

/** `destino.ruta` de la API, solo si es interna y de un ticket, orden o prospecto; si no, el inicio. */
export function destinoSeguro(ruta: string | null | undefined, origen?: string): string {
    const segura = rutaSegura(ruta, "/", origen);
    return DESTINO.test(segura) ? segura : "/";
}

export type MensajeSSO = { titulo: string; detalle: string };

/** Texto de cada error del canje o de la vinculación (problem+json `codigo` y estado). */
export function mensajeSSO(estado: number, codigo: string | undefined): MensajeSSO {
    switch (codigo) {
        case "ticket_invalido":
            return { titulo: "El enlace venció o ya se usó", detalle: "Los enlaces de Argos duran un minuto y sirven una sola vez. Vuelve a abrirlo desde Argos." };
        case "conector_apagado":
            return { titulo: "Iris no está conectado con Argos", detalle: "La conexión con Argos está apagada. Entra con tu cuenta de Iris o avisa a un administrador." };
        case "sso_requiere_mfa":
            return {
                titulo: "Tu cuenta necesita doble factor para entrar desde Argos",
                detalle: "Tu usuario administra Iris: activa la verificación en dos pasos (entrando con tu contraseña) para poder entrar desde Argos.",
            };
        case "prohibido":
            return { titulo: "Esta cuenta no puede entrar", detalle: "La cuenta de Iris vinculada está desactivada o no puede iniciar sesión. Habla con un administrador de Iris." };
        case "demasiados_intentos":
            return { titulo: "Cuenta bloqueada por intentos fallidos", detalle: "Espera unos minutos y vuelve a abrir el enlace desde Argos." };
        case "demasiados_canjes":
            return { titulo: "Demasiados intentos en este momento", detalle: "Espera un minuto y vuelve a abrir el enlace desde Argos." };
        case "vinculacion_invalida":
            return { titulo: "La vinculación ya no es válida", detalle: "Venció (dura 10 minutos) o ya se usó. Vuelve a abrir el enlace desde Argos." };
        case "vinculo_existente":
            return { titulo: "Tu cuenta ya está vinculada con otra cuenta de Argos", detalle: "Quita ese vínculo en «Mi perfil» y vuelve a intentarlo." };
        case "cuenta_externa_vinculada":
            return { titulo: "Esa cuenta de Argos ya está vinculada", detalle: "La cuenta de Argos ya está vinculada con otro usuario de Iris. Habla con un administrador." };
        default:
            if (estado === 429) return { titulo: "Demasiados intentos", detalle: "Espera un momento y vuelve a abrir el enlace desde Argos." };
            if (estado === 403) return { titulo: "Esta cuenta no puede entrar", detalle: "Habla con un administrador de Iris." };
            return { titulo: "No se pudo entrar desde Argos", detalle: "Inténtalo de nuevo o entra con tu cuenta de Iris." };
    }
}
