import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import type { ResultadoLlamada } from "@/lib/voz/tipos";
import type { EntradaActualizarCentralita, EntradaCrearCentralita, EntradaProbarCentralita } from "./tipos";

/* Llamadas a la API de la centralita (contratos/voz.md §3.12), con el cliente tipado. */

const BASE = "/api/v1/mensajeria";
const idem = (clave = nuevaClaveIdempotencia()) => ({ "Idempotency-Key": clave });

export async function leerCentralita(signal?: AbortSignal) {
    return (await resolver(api().GET(`${BASE}/centralita`, { signal }))).centralita ?? null;
}

export function leerMiExtension(signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/centralita/mi-extension`, { signal }));
}

export function probarDatosCentralita(cuerpo: EntradaProbarCentralita) {
    return resolver(api().POST(`${BASE}/centralitas/probar`, { params: { header: idem() }, body: cuerpo }));
}

export function crearCentralita(cuerpo: EntradaCrearCentralita, clave: string) {
    return resolver(api().POST(`${BASE}/centralitas`, { params: { header: idem(clave) }, body: cuerpo }));
}

export function actualizarCentralita(id: number, cuerpo: EntradaActualizarCentralita, clave: string) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/actualizar`, { params: { path: { id }, header: idem(clave) }, body: cuerpo }));
}

export function probarCentralita(id: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/probar`, { params: { path: { id }, header: idem() }, body: {} }));
}

export function activarCentralita(id: number, version: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/activar`, { params: { path: { id }, header: idem() }, body: { version } }));
}

export function desactivarCentralita(id: number, version: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/desactivar`, { params: { path: { id }, header: idem() }, body: { version } }));
}

export function confirmarHuella(id: number, version: number, huella: string) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/confirmar-huella`, { params: { path: { id }, header: idem() }, body: { version, huella } }));
}

export async function leerExtensiones(id: number, signal?: AbortSignal) {
    return (await resolver(api().GET(`${BASE}/centralitas/{id}/extensiones`, { params: { path: { id } }, signal }))).data;
}

export function asignarExtension(id: number, usuarioId: number, extension: string, clave: string) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/extensiones/asignar`, { params: { path: { id }, header: idem(clave) }, body: { usuarioId, extension } }));
}

export function quitarExtension(id: number, extensionId: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/extensiones/{extensionId}/quitar`, { params: { path: { id, extensionId }, header: idem() } }));
}

export type FiltroLlamadas = { clienteId?: number; ticketId?: number; ordenId?: number; mias?: boolean; hoy?: boolean; cursor?: string; limit?: number };

export function listarLlamadasCentralita(filtro: FiltroLlamadas, signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/centralitas/llamadas`, { params: { query: filtro }, signal }));
}

export function anotarLlamadaCentralita(id: string, cuerpo: { version: number; nota?: string; resultado?: ResultadoLlamada }, clave: string) {
    // El servidor no acepta ticket_creado aquí: el ticket se vincula aparte.
    const resultado = cuerpo.resultado === "ticket_creado" ? undefined : cuerpo.resultado;
    return resolver(api().POST(`${BASE}/centralitas/llamadas/{id}/anotar`, { params: { path: { id }, header: idem(clave) }, body: { ...cuerpo, resultado } }));
}

export function asociarClienteCentralita(id: string, clienteId: number, servicioId?: number) {
    return resolver(api().POST(`${BASE}/centralitas/llamadas/{id}/asociar-cliente`, { params: { path: { id }, header: idem() }, body: { clienteId, servicioId } }));
}

export function vincularTicketCentralita(id: string, ticketId: number) {
    return resolver(api().POST(`${BASE}/centralitas/llamadas/{id}/vincular-ticket`, { params: { path: { id }, header: idem() }, body: { ticketId } }));
}
