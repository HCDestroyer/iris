import type { Schema } from "@iris/contratos";
import {
    CalendarDays,
    ClipboardList,
    Handshake,
    LayoutDashboard,
    LifeBuoy,
    Route,
    Settings,
    Users,
    type LucideIcon,
} from "lucide-react";

export type Permiso = Schema<"Permiso">;

export type EntradaMenu = {
    href: string;
    etiqueta: string;
    icono: LucideIcon;
    /**
     * Basta uno de estos permisos (nombres exactos del enum `Permiso` del OpenAPI) para ver la
     * entrada. `null`: la ven todos los usuarios con sesión.
     */
    permisos: readonly Permiso[] | null;
    /** Aún sin pantalla: muestra «Próximamente». */
    proximamente?: boolean;
};

/*
 * Despacho y Comercial todavía no tienen permisos en el enum `Permiso` (los publica I2
 * con sus módulos). Mientras tanto son «Próximamente» para todos. Estas aserciones de tipos
 * fallan en `pnpm typecheck` en cuanto el permiso exista en el contrato, para que se pase de `null` al permiso real en la misma PR que regenera packages/contratos.
 */
type AunNoExiste<P extends string> = P extends Permiso ? false : true;
export const permisosPendientes = {
    "despacho.ver": true satisfies AunNoExiste<"despacho.ver">,
    "prospectos.ver": true satisfies AunNoExiste<"prospectos.ver">,
} as const;

export const MENU: readonly EntradaMenu[] = [
    { href: "/", etiqueta: "Inicio", icono: LayoutDashboard, permisos: null },
    { href: "/tickets", etiqueta: "Tickets", icono: LifeBuoy, permisos: ["tickets.ver"] },
    {
        href: "/ordenes",
        etiqueta: "Órdenes",
        icono: ClipboardList,
        permisos: ["ordenes.ver", "ordenes.ver_propias"],
    },
    { href: "/agenda", etiqueta: "Agenda", icono: CalendarDays, permisos: ["agenda.ver"], proximamente: true },
    { href: "/despacho", etiqueta: "Despacho", icono: Route, permisos: null, proximamente: true },
    { href: "/comercial", etiqueta: "Comercial", icono: Handshake, permisos: null, proximamente: true },
    { href: "/usuarios", etiqueta: "Usuarios", icono: Users, permisos: ["usuarios.ver"] },
    {
        href: "/administracion",
        etiqueta: "Administración",
        icono: Settings,
        permisos: ["buzones.ver", "tokens.ver", "roles.ver", "permisos.ver"],
    },
];

/** ¿Alcanza alguno de `requeridos`? `null` = cualquiera con sesión. */
export function tieneAlguno(permisos: readonly Permiso[], requeridos: readonly Permiso[] | null): boolean {
    return requeridos === null || requeridos.some((p) => permisos.includes(p));
}

/** Entradas del menú que el usuario puede ver, en orden. */
export function menuVisible(permisos: readonly Permiso[] | null | undefined): EntradaMenu[] {
    const propios = permisos ?? [];
    return MENU.filter((e) => tieneAlguno(propios, e.permisos));
}

/** Entrada del menú activa para una ruta (la más específica). */
export function entradaActiva(ruta: string): EntradaMenu | undefined {
    return [...MENU]
        .sort((a, b) => b.href.length - a.href.length)
        .find((e) => (e.href === "/" ? ruta === "/" : ruta === e.href || ruta.startsWith(`${e.href}/`)));
}
