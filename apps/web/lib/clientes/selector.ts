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

export const MAX_RECIENTES = 5;

/** Mínimo de caracteres para buscar por texto (la API lo exige con `servicios=true`). */
export const MIN_BUSQUEDA = 3;

const PREFIJO_RECIENTES = "iris:clientes-recientes:";

/**
 * Lo único que se guarda de un cliente reciente en el navegador: sin dirección, teléfono ni
 * servicios (datos personales). Al elegirlo se vuelve a leer la ficha.
 */
export type ClienteReciente = { id: number; codigo: string; nombre: string };

/** Clave de los recientes en el navegador: por usuario y empresa activa. */
export function claveRecientes(usuarioId: number, empresaId: number | null | undefined): string {
    return `${PREFIJO_RECIENTES}${usuarioId}:${empresaId ?? 0}`;
}

/** Pone el cliente al principio, sin repetir, con solo id, código y nombre, y corta en MAX_RECIENTES. */
export function agregarReciente(lista: readonly ClienteReciente[], cliente: ClienteReciente): ClienteReciente[] {
    const minimo: ClienteReciente = { id: cliente.id, codigo: cliente.codigo, nombre: cliente.nombre };
    return [minimo, ...lista.filter((c) => c.id !== cliente.id).map((c) => ({ id: c.id, codigo: c.codigo, nombre: c.nombre }))].slice(0, MAX_RECIENTES);
}

/** Valida lo leído del navegador (puede venir corrupto o de una versión anterior). */
export function sonRecientes(valor: unknown): valor is ClienteReciente[] {
    return (
        Array.isArray(valor) &&
        valor.every((c: unknown) => {
            if (typeof c !== "object" || c === null) return false;
            const x = c as Record<string, unknown>;
            return typeof x.id === "number" && typeof x.nombre === "string" && typeof x.codigo === "string";
        })
    );
}

/** Lo leído del navegador reducido a id, código y nombre (una versión anterior guardaba más). */
export function soloMinimo(lista: readonly ClienteReciente[]): ClienteReciente[] {
    return lista.map((c) => ({ id: c.id, codigo: c.codigo, nombre: c.nombre }));
}

/** Borra los recientes de todos los usuarios y empresas: al cerrar sesión y ante cualquier 401. */
export function limpiarClientesRecientes(almacen: Pick<Storage, "length" | "key" | "removeItem"> | undefined = navegador()): void {
    if (!almacen) return;
    try {
        const claves: string[] = [];
        for (let i = 0; i < almacen.length; i += 1) {
            const k = almacen.key(i);
            if (k?.startsWith(PREFIJO_RECIENTES)) claves.push(k);
        }
        for (const k of claves) almacen.removeItem(k);
    } catch {
        // Sin almacenamiento: no hay nada que borrar.
    }
}

function navegador(): Storage | undefined {
    try {
        return typeof window === "undefined" ? undefined : window.localStorage;
    } catch {
        return undefined;
    }
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
