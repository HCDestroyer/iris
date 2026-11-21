import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import type { EntradaLlamarTelefono, EntradaTelefonoCentralita, EntradaTelefonoExtension } from "@iris/telefono";

/* Llamadas a la API del teléfono de Iris (contratos/voz.md §3.12, «E4c»), con el cliente tipado. */

const BASE = "/api/v1/mensajeria";
const idem = (clave = nuevaClaveIdempotencia()) => ({ "Idempotency-Key": clave });

/** `GET /telefono`: si el teléfono está disponible para mí y por qué no. */
export function leerTelefono(signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/telefono`, { signal }));
}

/** `POST /telefono/abrir`: ticket de un uso (30 s) para el WebSocket y el TURN efímero. */
export function abrirTelefono(signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/telefono/abrir`, { params: { header: idem() }, body: { dispositivo: "panel" }, signal }));
}

/** `POST /telefono/llamar`: el destino SIP que el teléfono marca (el número lo pone el servidor). */
export function llamarTelefono(cuerpo: EntradaLlamarTelefono, clave: string) {
    return resolver(api().POST(`${BASE}/telefono/llamar`, { params: { header: idem(clave) }, body: cuerpo }));
}

/** `GET /telefono/transferir/destinos`: las personas y los grupos de mi empresa. */
export function destinosTransferencia(signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/telefono/transferir/destinos`, { signal }));
}

// Administración ---------------------------------------------------------------------------------

export function guardarTelefonoCentralita(id: number, cuerpo: EntradaTelefonoCentralita, clave: string) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/telefono`, { params: { path: { id }, header: idem(clave) }, body: cuerpo }));
}

export function probarTelefonoCentralita(id: number, cuerpo: { wssPuerto?: number; wssRuta?: string } = {}) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/telefono/probar`, { params: { path: { id }, header: idem() }, body: cuerpo }));
}

export function activarTelefono(id: number, version: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/telefono/activar`, { params: { path: { id }, header: idem() }, body: { version } }));
}

export function desactivarTelefono(id: number, version: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/telefono/desactivar`, { params: { path: { id }, header: idem() }, body: { version } }));
}

export async function leerGrupos(id: number, signal?: AbortSignal) {
    return (await resolver(api().GET(`${BASE}/centralitas/{id}/grupos`, { params: { path: { id } }, signal }))).data ?? [];
}

export function crearGrupo(id: number, cuerpo: { nombre: string; numero: string }, clave: string) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/grupos`, { params: { path: { id }, header: idem(clave) }, body: cuerpo }));
}

export function editarGrupo(id: number, grupoId: number, cuerpo: { version: number; nombre: string; numero: string; activo: boolean }) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/grupos/{grupoId}/actualizar`, { params: { path: { id, grupoId }, header: idem() }, body: cuerpo }));
}

export function guardarTelefonoExtension(id: number, extensionId: number, cuerpo: EntradaTelefonoExtension, clave: string) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/extensiones/{extensionId}/telefono`, { params: { path: { id, extensionId }, header: idem(clave) }, body: cuerpo }));
}

export function probarRegistro(id: number, extensionId: number) {
    return resolver(api().POST(`${BASE}/centralitas/{id}/extensiones/{extensionId}/probar-registro`, { params: { path: { id, extensionId }, header: idem() }, body: {} }));
}
