import type { Schema } from "@iris/contratos";
import type { Permiso } from "@/lib/menu";

/*
 * Correo del sistema (`.claude/integration/contratos/correo-sistema.md`): alias de los tipos de
 * `packages/contratos`. Nada escrito a mano.
 */

/** `GET /correo-sistema/{ambito}`. La contraseña nunca viene: solo `tieneContrasena`. */
export type AjusteCorreo = Schema<"CorreoSistema">;
export type SeguridadCorreo = AjusteCorreo["seguridad"];
/** Por dónde saldría hoy un correo de ese ámbito (§2). */
export type OrigenCorreo = AjusteCorreo["origen"];

/** Cuerpo de `guardar`: `contrasena` ausente = se conserva la guardada; `""` = se borra. */
export type EntradaCorreo = Schema<"CuerpoGuardarCorreo">;
/** Cuerpo de `probar`: lo mismo sin `version` ni `activo`, más `para`. */
export type EntradaPrueba = Schema<"CuerpoProbarCorreo">;
/** Lo que comparten `guardar` y `probar`. */
export type CamposCorreo = Omit<EntradaPrueba, "para">;

/** `POST …/probar` responde siempre 200 con el resultado (§3.3). */
export type ResultadoPrueba = Schema<"ResultadoPruebaCorreo">;
export type CausaPrueba = NonNullable<ResultadoPrueba["causa"]>;

export const PERMISO_VER_CORREO = "correo_sistema.ver" satisfies Permiso;
export const PERMISO_CONFIGURAR_CORREO = "correo_sistema.configurar" satisfies Permiso;
