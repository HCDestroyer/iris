import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";

/*
 * Lógica del selector de cliente (pedido del PM: «la selección de cliente debe ser más
 * intuitiva»): qué buscar, cómo mostrar el estado de un servicio, recientes y teclado.
 */

export type ClienteResumen = Schema<"ClienteResumen">;
export type ServicioCliente = Schema<"Servicio">;

/** Lo que entrega el selector: el cliente y, si tiene, el servicio elegido. */
export type SeleccionCliente = { cliente: ClienteResumen; servicio: ServicioCliente | null };

const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
const IPV6 = /^[0-9a-f:]+$/i;

/** ¿El texto es una IP? Entonces se busca por IP (dueño del servicio), no por texto. */
export function esIP(q: string): boolean {
    const t = q.trim();
    return IPV4.test(t) || (t.includes(":") && t.split(":").length >= 3 && IPV6.test(t));
}

const TONOS_SERVICIO: Record<string, Tono> = {
    activo: "success",
    suspendido: "warning",
    cortado: "error",
    moroso: "warning",
    retirado: "neutral",
    cancelado: "neutral",
    pendiente_instalacion: "info",
};

/** Estado de un servicio de Argos como insignia: tono y texto legible («Pendiente instalación»). */
export function estadoServicio(estado: string): { tono: Tono; texto: string } {
    const clave = estado.trim().toLowerCase();
    const texto = clave ? clave.charAt(0).toUpperCase() + clave.slice(1).replaceAll("_", " ") : "Sin estado";
    return { tono: TONOS_SERVICIO[clave] ?? "neutral", texto };
}

/** Servicios para elegir: los vigentes primero (activo, suspendido…), los retirados al final. */
export function ordenarServicios(servicios: readonly ServicioCliente[]): ServicioCliente[] {
    const peso = (s: ServicioCliente) => (["retirado", "cancelado"].includes(s.estado.toLowerCase()) ? 1 : 0);
    return [...servicios].sort((a, b) => peso(a) - peso(b) || a.id - b.id);
}

/**
 * Al elegir un cliente: sin servicios (o un solo servicio) se resuelve solo; con varios hay que
 * preguntar; con `null` (no se pudieron leer) hay que leer la ficha.
 */
export function pasoTrasElegir(servicios: readonly ServicioCliente[] | null | undefined): "leer" | "listo" | "preguntar" {
    if (servicios === null || servicios === undefined) return "leer";
    return servicios.length > 1 ? "preguntar" : "listo";
}

export const MAX_RECIENTES = 5;

/** Clave de los recientes en el navegador: por usuario y empresa activa. */
export function claveRecientes(usuarioId: number, empresaId: number | null | undefined): string {
    return `iris:clientes-recientes:${usuarioId}:${empresaId ?? 0}`;
}

/** Pone el cliente al principio, sin repetir, y corta en MAX_RECIENTES. */
export function agregarReciente(lista: readonly ClienteResumen[], cliente: ClienteResumen): ClienteResumen[] {
    return [cliente, ...lista.filter((c) => c.id !== cliente.id)].slice(0, MAX_RECIENTES);
}

/** Valida lo leído del navegador (puede venir corrupto o de una versión anterior). */
export function sonRecientes(valor: unknown): valor is ClienteResumen[] {
    return (
        Array.isArray(valor) &&
        valor.every((c: unknown) => {
            if (typeof c !== "object" || c === null) return false;
            const x = c as Record<string, unknown>;
            return typeof x.id === "number" && typeof x.nombre === "string" && typeof x.codigo === "string" && typeof x.direccion === "string";
        })
    );
}

/** Índice activo tras una tecla de la lista (con vuelta al principio/fin); `null` si no aplica. */
export function moverIndice(actual: number, total: number, tecla: string): number | null {
    if (total === 0) return null;
    switch (tecla) {
        case "ArrowDown":
            return actual < 0 ? 0 : (actual + 1) % total;
        case "ArrowUp":
            return actual <= 0 ? total - 1 : actual - 1;
        case "Home":
            return 0;
        case "End":
            return total - 1;
        default:
            return null;
    }
}

/** Nombres de las empresas del cliente según las de la sesión («Cable Norte, Fibra Sur»). */
export function nombresEmpresas(cliente: ClienteResumen, empresas: readonly { id: number; nombre: string }[]): string | null {
    const ids = cliente.empresas && cliente.empresas.length > 0 ? cliente.empresas : cliente.empresaId ? [cliente.empresaId] : [];
    const nombres = ids.map((id) => empresas.find((e) => e.id === id)?.nombre).filter((n): n is string => Boolean(n));
    return nombres.length > 0 ? nombres.join(", ") : null;
}
