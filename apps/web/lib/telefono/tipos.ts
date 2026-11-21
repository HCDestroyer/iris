import type { Schema } from "@iris/contratos";
import type { Permiso } from "@/lib/menu";

/*
 * El teléfono de Iris, E4c (`.claude/integration/contratos/voz.md` §3.12, «E4c»): la extensión
 * `navegador` de cada persona contesta y llama en el panel por el puente SIP de Iris. Tipos de la API
 * (siempre de `packages/contratos`) y los del teléfono en esta pestaña.
 */

export type TelefonoMio = Schema<"TelefonoMio">;
export type MotivoTelefono = NonNullable<TelefonoMio["motivo"]>;
export type SesionTelefono = Schema<"SesionTelefono">;
export type MarcacionTelefono = Schema<"MarcacionTelefono">;
export type EntradaLlamarTelefono = Schema<"EntradaLlamarTelefono">;
export type DestinosTransferencia = Schema<"DestinosTransferencia">;
export type PersonaDestino = Schema<"PersonaDestino">;
export type GrupoDestino = Schema<"GrupoDestino">;
export type TelefonoCentralita = Schema<"TelefonoCentralita">;
export type EntradaTelefonoCentralita = Schema<"EntradaTelefonoCentralita">;
export type EntradaTelefonoExtension = Schema<"EntradaTelefonoExtension">;
export type PruebaTelefono = Schema<"PruebaTelefono">;
export type PruebaRegistro = Schema<"PruebaRegistro">;
export type GrupoCentralita = Schema<"GrupoCentralita">;
export type TipoTelefonoFicha = NonNullable<EntradaLlamarTelefono["telefono"]>;

export const PERMISO_LLAMAR = "centralita.llamar" satisfies Permiso;
export const PERMISO_TRANSFERIR = "centralita.transferir" satisfies Permiso;

/** Por qué el teléfono de esta pestaña no está listo. Cada uno tiene su texto en llano (presentacion.ts). */
export type ErrorTelefono =
    /** No hay conexión con la centralita (o con Iris): se reintenta sola. */
    | { tipo: "sin_conexion"; desde: number }
    /** La centralita no aceptó la contraseña de la extensión: solo un administrador lo arregla. */
    | { tipo: "clave" }
    /** El navegador no deja usar el micrófono. */
    | { tipo: "microfono"; estado: "denegado" | "sin_microfono" | "no_disponible" }
    /** Otra pestaña (u otro dispositivo) tiene el teléfono. */
    | { tipo: "pestana" }
    /**
     * Se espera hasta `hasta` (lo dice `Retry-After`): se abrió demasiadas veces seguidas o, con
     * `rechazos`, la centralita rechazó varias contraseñas seguidas (fail2ban).
     */
    | { tipo: "espera"; hasta: number; rechazos?: boolean }
    /** Iris cortó el teléfono (4003 sin motivo) y `GET /telefono` lo sigue dando por disponible. */
    | { tipo: "cortado" };

/** El teléfono en esta pestaña. */
export type ConexionTelefono =
    /** No disponible para esta persona (sin extensión del navegador, apagado en la empresa…). */
    | { fase: "no_disponible"; motivo: MotivoTelefono | null }
    /** Disponible pero no encendido en este navegador (primera vez o «Ahora no»). */
    | { fase: "apagado" }
    | { fase: "conectando" }
    | { fase: "listo" }
    | { fase: "error"; error: ErrorTelefono };

export type DireccionLlamada = "entrante" | "saliente";

/** Una llamada del teléfono (un diálogo SIP de esta pestaña). */
export type LlamadaTelefono = {
    /** Id local (el de la sesión SIP). */
    id: string;
    direccion: DireccionLlamada;
    /** El número del otro lado (el de la centralita en una entrante; el marcado en una saliente). */
    numero: string;
    /** La llamada de Iris (`X-Iris-Llamada` en una entrante; la de `llamar` en una saliente), si se sabe. */
    llamadaId: string | null;
    /** Cómo se muestra (cliente, compañero o el número). */
    nombre: string;
    fase: "sonando" | "llamando" | "en_curso" | "terminada";
    /** Cuándo se contestó (ms). */
    desde: number | null;
    silenciada: boolean;
    enEspera: boolean;
    /** Por qué terminó, en llano (solo si no fue un colgar normal). */
    motivoFin: string | null;
    /** Es un compañero o un grupo (llamada interna, sin fila en Iris). */
    interna: boolean;
};

export type EstadoTelefono = {
    conexion: ConexionTelefono;
    llamadas: LlamadaTelefono[];
    /** Tu extensión (para los textos). */
    extension: string | null;
};
