import type { Schema } from "@iris/contratos";
import { CircleUserRound, ClipboardList, Handshake, LifeBuoy, UserRound, type LucideIcon } from "lucide-react";
import { SECCIONES } from "../administracion/secciones";
import { menuVisible, tieneAlguno, type Permiso } from "../menu";

/*
 * Buscador global (Ctrl/⌘ + K): pantallas (filtradas por permiso, en el navegador) y registros
 * con los listados de siempre (`q` + `limit`): tickets, órdenes, prospectos y clientes de Argos.
 * Sin endpoints nuevos.
 */

export type ResultadoBusqueda = { id: string; etiqueta: string; detalle?: string; icono?: LucideIcon; href: string };

export type Fuente = "tickets" | "ordenes" | "prospectos" | "clientes";

/** Por fuente: título, permisos (basta uno) y letras mínimas para preguntar a la API. */
export const FUENTES: readonly { id: Fuente; titulo: string; permisos: readonly Permiso[]; minimo: number }[] = [
    { id: "tickets", titulo: "Tickets", permisos: ["tickets.ver"], minimo: 2 },
    { id: "ordenes", titulo: "Órdenes", permisos: ["ordenes.ver", "ordenes.ver_propias"], minimo: 2 },
    { id: "prospectos", titulo: "Prospectos", permisos: ["prospectos.ver"], minimo: 2 },
    // Argos busca desde 2 letras; con 3 se evitan búsquedas inútiles contra Argos en cada tecla.
    { id: "clientes", titulo: "Clientes", permisos: ["clientes.ver"], minimo: 3 },
];

export const LIMITE_POR_FUENTE = 5;

/** Sin tildes ni mayúsculas, para comparar lo escrito con los nombres de las pantallas. */
export function normalizar(texto: string): string {
    return texto.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().trim();
}

/** Pantallas que el usuario puede abrir: el menú, las secciones de Administración y su perfil. */
export function pantallasVisibles(permisos: readonly Permiso[]): ResultadoBusqueda[] {
    const menu = menuVisible(permisos).map((e) => ({ id: `pantalla:${e.href}`, etiqueta: e.etiqueta, icono: e.icono, href: e.href }));
    const admin = SECCIONES.filter((s) => s.href && tieneAlguno(permisos, s.permisos)).map((s) => ({
        id: `pantalla:${s.href}`,
        etiqueta: s.titulo,
        detalle: "Administración",
        icono: s.icono,
        href: s.href as string,
    }));
    return [...menu, ...admin, { id: "pantalla:/cuenta", etiqueta: "Mi perfil", icono: CircleUserRound, href: "/cuenta" }];
}

/** Pantallas cuyo nombre (o sección) contiene lo escrito; sin texto, todas. */
export function filtrarPantallas(pantallas: readonly ResultadoBusqueda[], texto: string): ResultadoBusqueda[] {
    const t = normalizar(texto);
    if (!t) return [...pantallas];
    return pantallas.filter((p) => normalizar(`${p.etiqueta} ${p.detalle ?? ""}`).includes(t));
}

/** Fuentes que se consultan con este texto y estos permisos. */
export function fuentesActivas(permisos: readonly Permiso[], texto: string): Fuente[] {
    const largo = texto.trim().length;
    return FUENTES.filter((f) => tieneAlguno(permisos, f.permisos) && largo >= f.minimo).map((f) => f.id);
}

export const resultadoTicket = (t: Schema<"Ticket">): ResultadoBusqueda => ({
    id: `ticket:${t.id}`,
    etiqueta: `${t.numero} · ${t.asunto}`,
    ...(t.contacto?.nombre ? { detalle: t.contacto.nombre } : {}),
    icono: LifeBuoy,
    href: `/tickets/${t.id}`,
});

export const resultadoOrden = (o: Schema<"Orden">): ResultadoBusqueda => ({
    id: `orden:${o.id}`,
    etiqueta: o.numero,
    ...(o.direccion ? { detalle: o.direccion } : {}),
    icono: ClipboardList,
    href: `/ordenes/${o.id}`,
});

export const resultadoProspecto = (p: Schema<"Prospecto">): ResultadoBusqueda => ({
    id: `prospecto:${p.id}`,
    etiqueta: `${p.numero} · ${p.nombre}`,
    ...(p.direccion ? { detalle: p.direccion } : {}),
    icono: Handshake,
    href: `/comercial/${p.id}`,
});

/** Un cliente lleva a sus tickets (la bandeja filtrada por cliente). */
export const resultadoCliente = (c: Schema<"ClienteResumen">): ResultadoBusqueda => ({
    id: `cliente:${c.id}`,
    etiqueta: c.nombre,
    detalle: [c.codigo, c.direccion].filter(Boolean).join(" · "),
    icono: UserRound,
    // Solo el id: la bandeja lee el nombre de la API (un nombre en la URL se podría falsificar).
    href: `/tickets?clienteId=${c.id}`,
});
