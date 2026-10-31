import type { Schema } from "@iris/contratos";
import {
    CalendarDays,
    ChartColumn,
    ClipboardList,
    HardHat,
    Handshake,
    LayoutDashboard,
    LifeBuoy,
    MessageSquareHeart,
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
 * `despacho.ver` todavía no existe en el enum `Permiso` (pedido a I2 en
 * .claude/integration/i3.md). Despacho usa mientras tanto `agenda.ver`, el permiso real más
 * cercano (calendario y mover citas; el mapa en vivo pide además `campo.ver_posiciones`). Esta
 * aserción de tipos falla en `pnpm typecheck` en cuanto el permiso exista en el contrato, para
 * pasar al permiso real en la misma PR que regenera packages/contratos.
 */
type AunNoExiste<P extends string> = P extends Permiso ? false : true;
export const permisosPendientes = {
    "despacho.ver": true satisfies AunNoExiste<"despacho.ver">,
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
    { href: "/agenda", etiqueta: "Agenda", icono: CalendarDays, permisos: ["agenda.ver"] },
    { href: "/despacho", etiqueta: "Despacho", icono: Route, permisos: ["agenda.ver"] },
    {
        href: "/tecnicos",
        etiqueta: "Técnicos",
        icono: HardHat,
        // Quien administra fichas (alta, edición, estado, turnos o ausencias); ver la agenda no basta.
        permisos: [
            "agenda.crear_tecnico",
            "agenda.editar_tecnico",
            "agenda.activar_tecnico",
            "agenda.desactivar_tecnico",
            "agenda.fijar_turnos",
            "agenda.registrar_ausencia",
            "agenda.anular_ausencia",
        ],
    },
    { href: "/comercial", etiqueta: "Comercial", icono: Handshake, permisos: ["prospectos.ver"] },
    { href: "/encuestas", etiqueta: "Encuestas", icono: MessageSquareHeart, permisos: ["encuestas.ver"] },
    { href: "/reportes", etiqueta: "Reportes", icono: ChartColumn, permisos: ["reportes.ver"] },
    { href: "/usuarios", etiqueta: "Usuarios", icono: Users, permisos: ["usuarios.ver"] },
    {
        href: "/administracion",
        etiqueta: "Administración",
        icono: Settings,
        permisos: [
            "buzones.ver",
            "plantillas.ver",
            "integraciones.ver",
            "tokens.ver",
            "roles.ver",
            "permisos.ver",
            "accesos.ver",
            "campo.ver_actividad",
            "agenda.crear_cuadrilla",
            "agenda.editar_cuadrilla",
            "agenda.activar_cuadrilla",
            "agenda.desactivar_cuadrilla",
            "agenda.crear_zona",
            "agenda.editar_zona",
            "agenda.activar_zona",
            "agenda.desactivar_zona",
        ],
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
