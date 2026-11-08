"use client";

import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";
import type { CanalMeta, ConexionMeta, EntradaCanalQr, EntradaConexionMeta, OpcionesPrestamo, Prestamo, RegistroMeta } from "./tipos";

/*
 * Llamadas de §15 (conexión con Meta, QR y préstamos). Pasan por el cliente del panel (cookies,
 * CSRF, empresa activa, re-autenticación). Los secretos y el `code` de Meta van solo en el cuerpo:
 * nunca en la URL, en un log ni en un aviso.
 */

const idem = (clave: string) => ({ "Idempotency-Key": clave });
const conSenal = (signal?: AbortSignal) => (signal ? { signal } : {});

// --- Conexión con Meta (§15.1) -----------------------------------------------------------------

export function leerConexionMeta(signal?: AbortSignal): Promise<ConexionMeta> {
    return resolver(api().GET("/api/v1/mensajeria/conexion-meta", conSenal(signal)));
}

/** Separa el verify token generado (se ve una vez) de la conexión que guarda la pantalla. */
export function separarTokenConexion(c: ConexionMeta): { conexion: ConexionMeta; verifyToken: string | null } {
    const { verifyTokenGenerado, ...conexion } = c;
    return { conexion, verifyToken: verifyTokenGenerado || null };
}

export async function actualizarConexionMeta(body: EntradaConexionMeta, clave: string) {
    return separarTokenConexion(await resolver(api().POST("/api/v1/mensajeria/conexion-meta/actualizar", { params: { header: idem(clave) }, body })));
}

export async function probarConexionMeta(clave: string): Promise<ConexionMeta> {
    return separarTokenConexion(await resolver(api().POST("/api/v1/mensajeria/conexion-meta/probar", { params: { header: idem(clave) } }))).conexion;
}

export function leerRegistroMeta(signal?: AbortSignal): Promise<RegistroMeta> {
    return resolver(api().GET("/api/v1/mensajeria/conexion-meta/registro", conSenal(signal)));
}

// --- Números por QR (§15.2) --------------------------------------------------------------------

/** Se llama en cuanto llega el `code` (vive ~30 s). */
export function crearCanalQr(body: EntradaCanalQr, clave: string): Promise<CanalMeta> {
    return resolver(api().POST("/api/v1/mensajeria/canales/qr", { params: { header: idem(clave) }, body }));
}

export function leerCanalMeta(id: number, signal?: AbortSignal): Promise<CanalMeta> {
    return resolver(api().GET("/api/v1/mensajeria/canales/{id}", { params: { path: { id } }, ...conSenal(signal) }));
}

export function sincronizarCanal(id: number, body: { contactos: boolean; mensajes: boolean }, clave: string): Promise<CanalMeta> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/sincronizar", { params: { path: { id }, header: idem(clave) }, body }));
}

// --- Préstamos (§15.3) -------------------------------------------------------------------------

export async function leerPrestamos(id: number, signal?: AbortSignal): Promise<Prestamo[]> {
    return (await resolver(api().GET("/api/v1/mensajeria/canales/{id}/prestamos", { params: { path: { id } }, ...conSenal(signal) }))).data ?? [];
}

export function leerOpcionesPrestamo(id: number, signal?: AbortSignal): Promise<OpcionesPrestamo> {
    return resolver(api().GET("/api/v1/mensajeria/canales/{id}/prestamos/opciones", { params: { path: { id } }, ...conSenal(signal) }));
}

export function guardarPrestamo(id: number, body: Schema<"EntradaPrestamoHTTPBody">, clave: string): Promise<Prestamo> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/prestamos", { params: { path: { id }, header: idem(clave) }, body }));
}

export function revocarPrestamo(id: number, empresaId: number, body: Schema<"EntradaRevocarHTTPBody">, clave: string): Promise<Prestamo> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/prestamos/{empresa}/revocar", { params: { path: { id, empresa: empresaId }, header: idem(clave) }, body }));
}

/** La empresa que usa un número prestado lo marca (o no) como su número de avisos. */
export function configurarPrestamo(id: number, body: Schema<"EntradaConfigurarPrestamoHTTPBody">, clave: string): Promise<CanalMeta> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/prestamo/configurar", { params: { path: { id }, header: idem(clave) }, body }));
}

// --- Bandeja (§15.3) ---------------------------------------------------------------------------

export function pasarAEmpresa(conversacionId: number, body: Schema<"EntradaPasarEmpresa">, clave: string): Promise<Schema<"ConversacionCliente">> {
    return resolver(api().POST("/api/v1/mensajeria/conversaciones/{id}/pasar-empresa", { params: { path: { id: conversacionId }, header: idem(clave) }, body }));
}
