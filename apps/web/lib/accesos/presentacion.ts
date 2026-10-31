import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import {
    Ban,
    CircleCheck,
    CircleX,
    Clock,
    KeyRound,
    LockKeyhole,
    Monitor,
    Smartphone,
    TabletSmartphone,
    UserX,
    Waypoints,
    WifiOff,
    type LucideIcon,
} from "lucide-react";
import { haceCuanto } from "@/lib/fechas";

export type Acceso = Schema<"Acceso">;
export type CanalAcceso = Acceso["canal"];
export type ResultadoAcceso = Acceso["resultado"];
export type ActividadTecnico = Schema<"ActividadTecnico">;
export type EstadoActividad = ActividadTecnico["estado"];
export type PermisoInfo = Schema<"PermisoInfo">;
export type Rol = Schema<"Rol">;

type Presentacion = { etiqueta: string; icono: LucideIcon };
type PresentacionTono = Presentacion & { tono: Tono };

/** Canal de un acceso, como lo lee el PM. */
export const CANALES: Record<CanalAcceso, Presentacion> = {
    panel: { etiqueta: "Panel", icono: Monitor },
    campo_pwa: { etiqueta: "App de campo (PWA)", icono: TabletSmartphone },
    campo_nativa: { etiqueta: "App de campo (Android)", icono: Smartphone },
    sso: { etiqueta: "Desde Argos (autologin)", icono: Waypoints },
    token: { etiqueta: "Token de API", icono: KeyRound },
};

/** Resultado de un acceso: icono + texto + color (nunca solo el color). */
export const RESULTADOS: Record<ResultadoAcceso, PresentacionTono> = {
    exito: { etiqueta: "Entró", icono: CircleCheck, tono: "success" },
    contrasena_erronea: { etiqueta: "Contraseña errónea", icono: CircleX, tono: "error" },
    mfa_fallido: { etiqueta: "Código MFA fallido", icono: LockKeyhole, tono: "error" },
    bloqueado: { etiqueta: "Bloqueado por intentos", icono: Ban, tono: "warning" },
    inactivo: { etiqueta: "Usuario desactivado", icono: UserX, tono: "warning" },
};

export const ORDEN_CANALES = Object.keys(CANALES) as CanalAcceso[];
export const ORDEN_RESULTADOS = Object.keys(RESULTADOS) as ResultadoAcceso[];

/** Estado de la app de campo de un técnico. */
export const ESTADOS_ACTIVIDAD: Record<EstadoActividad, PresentacionTono> = {
    al_dia: { etiqueta: "Al día", icono: CircleCheck, tono: "success" },
    atrasado: { etiqueta: "Atrasado", icono: Clock, tono: "warning" },
    sin_conexion: { etiqueta: "Sin conexión", icono: WifiOff, tono: "error" },
};

export const ORDEN_ESTADOS = Object.keys(ESTADOS_ACTIVIDAD) as EstadoActividad[];

const PLATAFORMAS: Record<string, string> = { pwa: "PWA", android: "Android", ios: "iOS" };

/** «Android», «PWA»… (la plataforma tal cual si no se conoce). */
export function nombrePlataforma(p: string | null | undefined): string {
    if (!p) return "—";
    return PLATAFORMAS[p] ?? p;
}

/** El texto del indicador: «Al día», «Atrasado · 3 sin enviar», «Sin conexión hace 2 h». */
export function textoActividad(t: Pick<ActividadTecnico, "estado" | "ultimaConexion" | "pendientes">, ahora: number): string {
    const base = ESTADOS_ACTIVIDAD[t.estado].etiqueta;
    if (t.estado === "sin_conexion") {
        return t.ultimaConexion ? `${base} ${haceCuanto(t.ultimaConexion, ahora)}`.replace(/ (\d{2}\/)/, " desde $1") : `${base} registrada`;
    }
    if (t.estado === "atrasado" && t.pendientes) {
        return `${base} · ${t.pendientes} sin enviar`;
    }
    return base;
}

/** Un grupo de permisos de un módulo, ordenado como lo muestra la pantalla de roles. */
export type GrupoPermisos = { modulo: string; nombre: string; permisos: PermisoInfo[] };

/** Agrupa el catálogo por módulo (orden alfabético del nombre legible; dentro, por código). */
export function agruparPermisos(catalogo: readonly PermisoInfo[]): GrupoPermisos[] {
    const grupos = new Map<string, GrupoPermisos>();
    for (const p of catalogo) {
        const g = grupos.get(p.modulo) ?? { modulo: p.modulo, nombre: p.moduloNombre, permisos: [] };
        g.permisos.push(p);
        grupos.set(p.modulo, g);
    }
    return [...grupos.values()]
        .map((g) => ({ ...g, permisos: [...g.permisos].sort((a, b) => a.codigo.localeCompare(b.codigo)) }))
        .sort((a, b) => a.nombre.localeCompare(b.nombre, "es"));
}

/** Los sensibles que se agregan (para avisar que se pedirá la contraseña). */
export function sensiblesAgregados(catalogo: readonly PermisoInfo[], antes: readonly string[], despues: readonly string[]): PermisoInfo[] {
    const previos = new Set(antes);
    return catalogo.filter((p) => p.sensible && despues.includes(p.codigo) && !previos.has(p.codigo));
}

/** «Zona Norte», «Cuadrilla 4» o «Global» para una asignación de rol. */
export function textoAlcance(a: Pick<Schema<"AsignacionRol">, "zonaId" | "cuadrillaId">, nombres?: { zonas?: Map<number, string>; cuadrillas?: Map<number, string> }): string {
    if (a.zonaId) return `Zona ${nombres?.zonas?.get(a.zonaId) ?? a.zonaId}`;
    if (a.cuadrillaId) return `Cuadrilla ${nombres?.cuadrillas?.get(a.cuadrillaId) ?? a.cuadrillaId}`;
    return "Todas las zonas";
}
