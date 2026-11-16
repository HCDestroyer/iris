import type { Permiso } from "@/lib/menu";

/*
 * Correo del sistema (`.claude/integration/contratos/correo-sistema.md`, v0.1.0).
 *
 * PROVISIONAL: el servidor todavía no publica el OpenAPI de estos endpoints, así que las formas de
 * aquí están copiadas del contrato escrito (§1 y §3). En cuanto `packages/contratos` las traiga,
 * este archivo pasa a ser alias de `Schema<"…">` y desaparece lo escrito a mano (CLAUDE.md §10).
 */

export type SeguridadCorreo = "starttls" | "tls" | "ninguna";
/** Por dónde saldría hoy un correo de ese ámbito (§2). */
export type OrigenCorreo = "empresa" | "global" | "entorno" | "ninguno";
export type CausaPrueba = "no_conecta" | "destino_no_permitido" | "certificado" | "seguridad" | "credenciales" | "remitente_rechazado" | "destinatario_rechazado" | "tiempo_agotado" | "otro";

/** `GET /correo-sistema/{ambito}` (§3.1). La contraseña nunca viene: solo `tieneContrasena`. */
export type AjusteCorreo = {
    ambito: string;
    empresaId: number | null;
    configurado: boolean;
    host: string;
    puerto: number;
    seguridad: SeguridadCorreo;
    usuario: string;
    tieneContrasena: boolean;
    remitenteNombre: string;
    remitenteCorreo: string;
    activo: boolean;
    version: number;
    actualizadoEn: string | null;
    origen: OrigenCorreo;
    origenDetalle: string;
    entorno: { configurado: boolean; host: string; remitenteCorreo: string } | null;
    ultimaPrueba: { estado: "ok" | "error"; en: string; causa: CausaPrueba | null } | null;
};

/** Lo que se edita: cuerpo de `guardar` sin `version` ni `activo`, y de `probar` sin `para` (§3.2 y §3.3). */
export type CamposCorreo = {
    host: string;
    puerto: number;
    seguridad: SeguridadCorreo;
    usuario: string;
    /** Ausente = se conserva la guardada; `""` = se borra; un valor = la reemplaza. */
    contrasena?: string;
    remitenteNombre: string;
    remitenteCorreo: string;
};

export type EntradaCorreo = CamposCorreo & { version: number; activo: boolean };
export type EntradaPrueba = CamposCorreo & { para: string };

/** `POST …/probar` responde siempre 200 con el resultado (§3.3). */
export type ResultadoPrueba = { estado: "ok" | "error"; causa: CausaPrueba | null; detalle: string };

// Los permisos todavía no están en el enum generado: se afirman aquí, en un solo lugar.
export const PERMISO_VER_CORREO = "correo_sistema.ver" as Permiso;
export const PERMISO_CONFIGURAR_CORREO = "correo_sistema.configurar" as Permiso;
