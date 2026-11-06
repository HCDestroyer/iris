import type { Schema } from "@iris/contratos";
import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "./api";
import type { Destinatario, EstadoDisponibilidad } from "./tipos";

/*
 * Llamadas de la bandeja 1.1 (contratos/conversaciones.md §14): disponibilidad y ajustes,
 * catálogos con equipo y bot, iniciar una conversación, «Pasar a…» y supervisores de una cola.
 * Con el cliente generado de packages/contratos; las acciones con `Idempotency-Key`.
 */

const M = "/api/v1/mensajeria";

// §14.5 Disponibilidad y ajustes.
export const leerDisponibilidad = (signal?: AbortSignal) => resolver(api().GET(`${M}/conversaciones/disponibilidad`, { signal }));
export const cambiarDisponibilidad = (estado: EstadoDisponibilidad, version: number, clave: string) =>
    resolver(api().POST(`${M}/conversaciones/disponibilidad`, { params: { header: cabeceraIdem(clave) }, body: { estado, version } }));
/** Sigo activa: responde la disponibilidad actual (no la cambia). */
export const mandarLatido = () => resolver(api().POST(`${M}/conversaciones/disponibilidad/latido`));
export const leerAjustes = (signal?: AbortSignal) => resolver(api().GET(`${M}/conversaciones/ajustes`, { signal }));
export const guardarAjustes = (cuerpo: Schema<"EntradaAjustesBandeja">, clave: string) =>
    resolver(api().POST(`${M}/conversaciones/ajustes`, { params: { header: cabeceraIdem(clave) }, body: cuerpo }));

// Catálogos con los campos de la fase 1.1.
export const leerAgentes = async (signal?: AbortSignal) => (await resolver(api().GET(`${M}/conversaciones/agentes`, { signal }))).data ?? [];
export const leerColas = async (signal?: AbortSignal) => (await resolver(api().GET(`${M}/colas`, { signal }))).data ?? [];
export const leerNumeros = async (signal?: AbortSignal) => (await resolver(api().GET(`${M}/conversaciones/canales`, { signal }))).data ?? [];

// §14.3 Iniciar una conversación.
export const buscarDestinatarios = async (q: string, canalId: number | null, signal?: AbortSignal) =>
    (await resolver(api().GET(`${M}/conversaciones/destinatarios`, { params: { query: { q, limit: 8, ...(canalId ? { canalId } : {}) } }, signal }))).data ?? [];
export const leerDestinatario = (canalId: number, d: Pick<Destinatario, "tipo" | "id">, signal?: AbortSignal) =>
    resolver(api().GET(`${M}/conversaciones/destinatario`, { params: { query: { canalId, ...(d.tipo === "cliente" ? { clienteId: d.id } : { contactoIrisId: d.id }) } }, signal }));
export const leerPlantillasNumero = async (canalId: number, signal?: AbortSignal) =>
    (await resolver(api().GET(`${M}/conversaciones/canales/{id}/plantillas`, { params: { path: { id: canalId } }, signal }))).data ?? [];

/** El `id` es a la vez el del mensaje y la `Idempotency-Key` (ver `useIdEnvio`). */
export const iniciarConversacion = (cuerpo: Schema<"EntradaIniciarConv">) =>
    resolver(api().POST(`${M}/conversaciones/iniciar`, { params: { header: cabeceraIdem(cuerpo.id) }, body: cuerpo }));

// §14.4 Pasar a…
export const pasarConversacion = (id: number, cuerpo: Schema<"EntradaPasarConv">, clave: string) =>
    resolver(api().POST(`${M}/conversaciones/{id}/pasar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: cuerpo }));

// §14.1 Colas con supervisores y minutos del bot.
export type CuerpoCola = Schema<"EntradaCola">;
export const crearCola = (cuerpo: CuerpoCola, clave: string) => resolver(api().POST(`${M}/colas`, { params: { header: cabeceraIdem(clave) }, body: cuerpo }));
export const actualizarCola = (id: number, cuerpo: Schema<"EntradaCola"> & { version: number }, clave: string) =>
    resolver(api().POST(`${M}/colas/{id}/actualizar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: cuerpo }));
export const fijarMiembros = (id: number, version: number, usuarioIds: number[], clave: string) =>
    resolver(api().POST(`${M}/colas/{id}/miembros`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: { version, usuarioIds } }));
export const fijarSupervisores = (id: number, version: number, usuarioIds: number[], clave: string) =>
    resolver(api().POST(`${M}/colas/{id}/supervisores`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: { version, usuarioIds } }));
