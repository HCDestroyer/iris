import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import type { EntradaUmbrales, Motor, Rango } from "./tipos";

/*
 * Llamadas del Servidor de IA (contratos/voz.md §4.8 y §4.12, base `/api/v1/mensajeria`), con el
 * cliente generado de packages/contratos. Las acciones llevan `Idempotency-Key`.
 */

const MOTORES = "/api/v1/mensajeria/voz/motores";
const SERVIDOR = "/api/v1/mensajeria/servidor-ia";
const accion = (id: number, clave: string) => ({ path: { id }, header: cabeceraIdem(clave) });

/** El servidor de la instalación, o `null` si no hay ninguno enlazado. */
export async function leerMotor(signal?: AbortSignal): Promise<Motor | null> {
    const r = await resolver(api().GET(MOTORES, { signal }));
    return r.data?.[0] ?? null;
}

/** Crea el enlace: devuelve el código de un solo uso, que se muestra una vez (no se guarda por `Idempotency-Key`). */
export const enlazarMotor = (entrada: Schema<"EntradaEnlazarMotor">, clave: string) => resolver(api().POST(`${MOTORES}/enlazar`, { params: { header: cabeceraIdem(clave) }, body: entrada }));
/** Iris se presenta al servidor con el código ya escrito allá. Cualquier falla: 502 `motor_no_responde`; ventana vencida: 409 `enlace_vencido`. */
export const conectarMotor = (id: number, clave: string) => resolver(api().POST(`${MOTORES}/{id}/conectar`, { params: accion(id, clave) }));
/** «Activar», con la huella que el administrador comparó en pantalla: si el servidor ya no es ese, 409 `huella_cambiada`. */
export const activarMotor = (id: number, huellaTls: string, clave: string) => resolver(api().POST(`${MOTORES}/{id}/activar`, { params: accion(id, clave), body: { huellaTls } }));
export const desactivarMotor = (id: number, motivo: string, clave: string) => resolver(api().POST(`${MOTORES}/{id}/desactivar`, { params: accion(id, clave), body: { motivo } }));
/** «Probar»: el resultado queda en `ultimaPrueba` de la tarjeta que devuelve. */
export const probarMotor = (id: number, clave: string) => resolver(api().POST(`${MOTORES}/{id}/probar`, { params: accion(id, clave) }));
/** «Rotar credenciales»; con `revocar`, la anterior deja de valer en el acto. */
export const rotarCredenciales = (id: number, revocar: boolean, clave: string) => resolver(api().POST(`${MOTORES}/{id}/rotar-token`, { params: accion(id, clave), body: revocar ? { revocar: true } : {} }));
export const guardarEmpresasPermitidas = (id: number, entrada: Schema<"EntradaEmpresasMotor">, clave: string) => resolver(api().POST(`${MOTORES}/{id}/empresas/actualizar`, { params: accion(id, clave), body: entrada }));
/** Los modelos que las empresas pueden elegir, de los que declara el servidor; `null` = todos. */
export const guardarModelosPermitidos = (id: number, entrada: Schema<"EntradaModelosMotor">, clave: string) => resolver(api().POST(`${MOTORES}/{id}/modelos/actualizar`, { params: accion(id, clave), body: entrada }));

export const leerTablero = (rango: Rango, signal?: AbortSignal) => resolver(api().GET(`${SERVIDOR}/tablero`, { params: { query: { rango } }, signal }));
export const leerUmbrales = (signal?: AbortSignal) => resolver(api().GET(`${SERVIDOR}/umbrales`, { signal }));
export const guardarUmbrales = (entrada: EntradaUmbrales, clave: string) => resolver(api().POST(`${SERVIDOR}/umbrales/actualizar`, { params: { header: cabeceraIdem(clave) }, body: entrada }));
