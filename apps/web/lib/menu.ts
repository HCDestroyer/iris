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

/** Grupos de la barra lateral (propuesta aprobada, §2), en este orden. */
export type GrupoMenu = "operacion" | "equipo" | "ventas" | "analisis" | "administracion";

export const GRUPOS: readonly { id: GrupoMenu; etiqueta: string }[] = [
    { id: "operacion", etiqueta: "Operación" },
    { id: "equipo", etiqueta: "Equipo" },
    { id: "ventas", etiqueta: "Ventas" },
    { id: "analisis", etiqueta: "Análisis" },
    { id: "administracion", etiqueta: "Administración" },
];

export type EntradaMenu = {
    href: string;
    grupo: GrupoMenu;
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
    { href: "/", grupo: "operacion", etiqueta: "Inicio", icono: LayoutDashboard, permisos: null },
    { href: "/tickets", grupo: "operacion", etiqueta: "Tickets", icono: LifeBuoy, permisos: ["tickets.ver"] },
    {
        href: "/ordenes",
        grupo: "operacion",
        etiqueta: "Órdenes",
        icono: ClipboardList,
        permisos: ["ordenes.ver", "ordenes.ver_propias"],
    },
    { href: "/agenda", grupo: "operacion", etiqueta: "Agenda", icono: CalendarDays, permisos: ["agenda.ver"] },
    { href: "/despacho", grupo: "operacion", etiqueta: "Despacho", icono: Route, permisos: ["agenda.ver"] },
    {
        href: "/tecnicos",
        grupo: "equipo",
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
    { href: "/usuarios", grupo: "equipo", etiqueta: "Usuarios", icono: Users, permisos: ["usuarios.ver"] },
    { href: "/comercial", grupo: "ventas", etiqueta: "Comercial", icono: Handshake, permisos: ["prospectos.ver"] },
    { href: "/encuestas", grupo: "ventas", etiqueta: "Encuestas", icono: MessageSquareHeart, permisos: ["encuestas.ver"] },
    { href: "/reportes", grupo: "analisis", etiqueta: "Reportes", icono: ChartColumn, permisos: ["reportes.ver"] },
    {
        href: "/administracion",
        grupo: "administracion",
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

export type GrupoVisible = { id: GrupoMenu; etiqueta: string; entradas: EntradaMenu[] };

/** Entradas visibles agrupadas (los grupos sin entradas no aparecen). */
export function menuAgrupado(permisos: readonly Permiso[] | null | undefined): GrupoVisible[] {
    const visibles = menuVisible(permisos);
    return GRUPOS.map((g) => ({ ...g, entradas: visibles.filter((e) => e.grupo === g.id) })).filter((g) => g.entradas.length > 0);
}

/** Accesos de la barra inferior del teléfono (más «Más», que abre el resto). */
export const BARRA_INFERIOR: readonly string[] = ["/", "/tickets", "/ordenes", "/agenda"];

/** Barra inferior y resto del menú (lo que va en «Más»), según los permisos. */
export function repartoTelefono(permisos: readonly Permiso[] | null | undefined): { barra: EntradaMenu[]; resto: GrupoVisible[] } {
    const barra = menuVisible(permisos).filter((e) => BARRA_INFERIOR.includes(e.href));
    const resto = menuAgrupado(permisos)
        .map((g) => ({ ...g, entradas: g.entradas.filter((e) => !BARRA_INFERIOR.includes(e.href)) }))
        .filter((g) => g.entradas.length > 0);
    return { barra, resto };
}
