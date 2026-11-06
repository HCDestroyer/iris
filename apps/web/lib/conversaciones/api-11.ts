import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "./api";
import type {
    AgenteDisponible,
    AjustesBandeja,
    BotNumero,
    ColaConSupervisores,
    ConversacionIniciada,
    ConversacionPasada,
    Destinatario,
    Disponibilidad,
    EstadoDisponibilidad,
    NumeroConBot,
    PlantillaCanal,
} from "./contrato-11";

/*
 * PROVISIONAL (fase 1.1, contratos/conversaciones.md §14): llamadas nuevas de la bandeja mientras
 * sus rutas no estén en `packages/contratos`. Usan el mismo cliente del panel (cookies, CSRF,
 * `X-Iris-Empresa`, re-autenticación) con las rutas sin tipar; al regenerar el contrato pasan a
 * `api().GET(…)` / `api().POST(…)` tipados y este archivo se borra.
 */

const M = "/api/v1/mensajeria";

type Opciones = { params?: { query?: Record<string, string | number>; header?: Record<string, string> }; body?: unknown; signal?: AbortSignal };
type Llamada = (ruta: string, o?: Opciones) => Promise<{ data?: unknown; error?: unknown; response: Response }>;
const suelto = () => api() as unknown as { GET: Llamada; POST: Llamada };

async function get<T>(ruta: string, o?: Opciones): Promise<T> {
    return (await resolver(suelto().GET(ruta, o))) as T;
}
async function post<T>(ruta: string, o?: Opciones): Promise<T> {
    return (await resolver(suelto().POST(ruta, o))) as T;
}
const conClave = (clave: string) => ({ header: cabeceraIdem(clave) });

// §14.5 Disponibilidad y ajustes.
export const leerDisponibilidad = (signal?: AbortSignal) => get<Disponibilidad>(`${M}/conversaciones/disponibilidad`, { signal });
export const cambiarDisponibilidad = (estado: EstadoDisponibilidad, version: number, clave: string) =>
    post<Disponibilidad>(`${M}/conversaciones/disponibilidad`, { params: conClave(clave), body: { estado, version } });
/** Sigo activa: responde la disponibilidad actual (no la cambia). */
export const mandarLatido = () => post<Disponibilidad>(`${M}/conversaciones/disponibilidad/latido`);
export const leerAjustes = (signal?: AbortSignal) => get<AjustesBandeja>(`${M}/conversaciones/ajustes`, { signal });
export const guardarAjustes = (cuerpo: AjustesBandeja, clave: string) => post<AjustesBandeja>(`${M}/conversaciones/ajustes`, { params: conClave(clave), body: cuerpo });

// Catálogos con los campos de la fase 1.1.
export const leerAgentesDisponibles = async (signal?: AbortSignal, colaId?: number) =>
    (await get<{ data: AgenteDisponible[] }>(`${M}/conversaciones/agentes`, { params: colaId ? { query: { colaId } } : {}, signal })).data ?? [];
export const leerColas = async (signal?: AbortSignal) => (await get<{ data: ColaConSupervisores[] }>(`${M}/colas`, { signal })).data ?? [];
export const leerCanalesConBot = async (signal?: AbortSignal) => (await get<{ data: NumeroConBot[] }>(`${M}/conversaciones/canales`, { signal })).data ?? [];

// §14.3 Iniciar una conversación.
export const buscarDestinatarios = async (q: string, canalId: number | null, signal?: AbortSignal) =>
    (await get<{ data: Destinatario[] }>(`${M}/conversaciones/destinatarios`, { params: { query: { q, limit: 8, ...(canalId ? { canalId } : {}) } }, signal })).data ?? [];
export const leerDestinatario = (canalId: number, d: Pick<Destinatario, "tipo" | "id">, signal?: AbortSignal) =>
    get<Destinatario>(`${M}/conversaciones/destinatario`, { params: { query: { canalId, ...(d.tipo === "cliente" ? { clienteId: d.id } : { contactoIrisId: d.id }) } }, signal });

export type CuerpoIniciar = {
    id: string;
    canalId: number;
    clienteId?: number;
    contactoIrisId?: number;
    plantilla: { codigo: string; variables: Record<string, string> };
};
export const iniciarConversacion = (cuerpo: CuerpoIniciar) => post<ConversacionIniciada>(`${M}/conversaciones/iniciar`, { params: conClave(cuerpo.id), body: cuerpo });

export const leerPlantillasCanal = async (canalId: number, signal?: AbortSignal) =>
    (await get<{ data: PlantillaCanal[] }>(`${M}/conversaciones/canales/${canalId}/plantillas`, { signal })).data ?? [];

// §14.4 Pasar a…
export const pasarConversacion = (id: number, cuerpo: { version: number; usuarioId: number; nota?: string }, clave: string) =>
    post<ConversacionPasada>(`${M}/conversaciones/${id}/pasar`, { params: conClave(clave), body: cuerpo });

// §14.1 Colas con supervisores y minutos del bot.
export type CuerpoCola = {
    nombre: string;
    descripcion: string;
    slaRespuestaMin: number | null;
    horarioId?: number | null;
    botSinConfirmacionMin?: number;
    botSinDisponiblesMin?: number;
};
export const crearCola = (cuerpo: CuerpoCola, clave: string) => post<ColaConSupervisores>(`${M}/colas`, { params: conClave(clave), body: cuerpo });
export const actualizarCola = (id: number, cuerpo: CuerpoCola & { version: number }, clave: string) =>
    post<ColaConSupervisores>(`${M}/colas/${id}/actualizar`, { params: conClave(clave), body: cuerpo });
export const fijarMiembros = (id: number, version: number, usuarioIds: number[], clave: string) =>
    post<ColaConSupervisores>(`${M}/colas/${id}/miembros`, { params: conClave(clave), body: { version, usuarioIds } });
export const fijarSupervisores = (id: number, version: number, usuarioIds: number[], clave: string) =>
    post<ColaConSupervisores>(`${M}/colas/${id}/supervisores`, { params: conClave(clave), body: { version, usuarioIds } });

/** Cuerpo `bot` de `POST /canales/{id}/configurar` (§14.6). */
export type CuerpoBot = BotNumero;
