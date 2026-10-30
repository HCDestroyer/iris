import type { Schema } from "@iris/contratos";
import type { Page, Route } from "@playwright/test";
import { ADMIN, CONSULTA, CONTRASENA, CSRF, SOPORTE, type Peticion } from "./api-falsa";

/*
 * API falsa del historial de accesos, las sesiones activas, la actividad de la app de campo y los
 * roles propios (identidad/accesos.go, actividad_campo.go y roles.go). Se registra DESPUÉS de
 * simularApi: Playwright corre primero la última ruta, y lo que no es de aquí sigue con
 * route.fallback() a la API falsa general. Las respuestas van tipadas con packages/contratos.
 */

type Rol = Schema<"Rol">;
type Acceso = Schema<"Acceso">;
type Usuario = Schema<"Usuario">;

const AHORA = Date.now();
const hace = (min: number) => new Date(AHORA - min * 60_000).toISOString();

export const CATALOGO: Schema<"PermisoInfo">[] = [
    { codigo: "tickets.ver", descripcion: "Ver tickets, su conversación, historial, categorías y SLA", modulo: "soporte", moduloNombre: "Soporte (tickets)", sensible: false },
    { codigo: "tickets.responder", descripcion: "Responder al cliente en un ticket (mensaje público)", modulo: "soporte", moduloNombre: "Soporte (tickets)", sensible: false },
    { codigo: "buzones.crear", descripcion: "Registrar buzones de correo (IMAP y SMTP) para la entrada de tickets", modulo: "soporte", moduloNombre: "Soporte (tickets)", sensible: true },
    { codigo: "ordenes.ver", descripcion: "Ver órdenes de trabajo (según el alcance) y su historial", modulo: "ordenes", moduloNombre: "Órdenes de trabajo", sensible: false },
    { codigo: "agenda.ver", descripcion: "Ver la agenda: zonas, cuadrillas, técnicos, citas y disponibilidad (según el alcance)", modulo: "agenda", moduloNombre: "Agenda y rutas", sensible: false },
    { codigo: "roles.ver", descripcion: "Ver roles y sus permisos", modulo: "roles", moduloNombre: "Roles y permisos", sensible: false },
    { codigo: "roles.crear", descripcion: "Crear roles propios eligiendo sus permisos (solo los que tienes sin límite de alcance)", modulo: "roles", moduloNombre: "Roles y permisos", sensible: true },
];

const rol = (id: number, codigo: string, nombre: string, permisos: string[], extra: Partial<Rol> = {}): Rol => ({
    id,
    codigo,
    nombre,
    descripcion: "",
    sistema: false,
    activo: true,
    permisos: permisos as Rol["permisos"],
    usuarios: 0,
    version: 1,
    ...extra,
});

export function rolesIniciales(): Rol[] {
    return [
        rol(1, "admin", "Administrador", CATALOGO.map((p) => p.codigo), { sistema: true, descripcion: "Todos los permisos", usuarios: 2 }),
        rol(7, "lectura", "Lectura", ["tickets.ver", "ordenes.ver", "agenda.ver", "roles.ver"], { sistema: true, descripcion: "Solo consulta", usuarios: 3 }),
        rol(20, "mesa_ayuda", "Mesa de ayuda", ["tickets.ver", "tickets.responder"], { descripcion: "Primer nivel de soporte", usuarios: 1, version: 2 }),
        rol(21, "temporada", "Temporada alta", ["ordenes.ver"], { activo: false, version: 4 }),
    ];
}

const acceso = (id: number, min: number, u: Usuario | null, canal: Acceso["canal"], resultado: Acceso["resultado"], dispositivo: string, extra: Partial<Acceso> = {}): Acceso => ({
    id,
    usuarioId: u?.id ?? null,
    usuarioNombre: u?.nombre ?? null,
    usuarioCorreo: u?.correo ?? null,
    canal,
    resultado,
    ip: "190.56.12.4",
    dispositivo,
    app: null,
    sesionId: resultado === "exito" ? id + 500 : null,
    tokenId: null,
    creadoEn: hace(min),
    ...extra,
});

export function accesosIniciales(): Acceso[] {
    const lista: Acceso[] = [
        acceso(60, 2, ADMIN, "panel", "exito", "Chrome · Windows"),
        acceso(59, 10, CONSULTA, "campo_nativa", "exito", "App de campo 1.4.0 · Android", { app: "campo/1.4.0 (android)" }),
        acceso(58, 15, CONSULTA, "panel", "contrasena_erronea", "Firefox · Linux"),
        acceso(57, 20, null, "panel", "contrasena_erronea", "Chrome · Android"),
        acceso(56, 30, SOPORTE, "sso", "exito", "Safari · iOS"),
        acceso(55, 40, SOPORTE, "panel", "mfa_fallido", "Safari · iOS"),
        acceso(54, 50, CONSULTA, "campo_pwa", "bloqueado", "Chrome · Android"),
    ];
    // Relleno viejo para la segunda página.
    for (let i = 0; i < 50; i++) {
        lista.push(acceso(53 - i, 60 * 24 + i, ADMIN, "token", "exito", "Script", { tokenId: 3 }));
    }
    return lista;
}

export function sesionesIniciales(): Schema<"SesionActiva">[] {
    return [
        { id: 900, canal: "panel", origen: "password", ip: "190.56.12.4", dispositivo: "Firefox · Linux", app: null, creadaEn: hace(120), ultimoUso: hace(5), expiraEn: hace(-600), actual: false },
        {
            id: 901,
            canal: "campo_nativa",
            origen: "password",
            ip: "181.114.3.9",
            dispositivo: "App de campo 1.4.0 · Android",
            app: "campo/1.4.0 (android)",
            creadaEn: hace(60),
            ultimoUso: hace(1),
            expiraEn: hace(-660),
            actual: false,
        },
    ];
}

const tecnico = (usuarioId: number, nombre: string, extra: Partial<Schema<"ActividadTecnico">>): Schema<"ActividadTecnico"> => ({
    usuarioId,
    fichaId: usuarioId + 100,
    nombre,
    correo: `${nombre.split(" ")[0]?.toLowerCase()}@iris.test`,
    cuadrillaId: 1,
    estado: "al_dia",
    ultimaConexion: null,
    ultimaSincronizacion: null,
    pendientes: null,
    pendientesEn: null,
    rechazadas: 0,
    ultimoRechazo: null,
    plataforma: null,
    ...extra,
});

export function actividadInicial(): Schema<"ActividadCampo"> {
    return {
        generadoEn: new Date(AHORA).toISOString(),
        sinConexionMin: 30,
        atrasoMin: 15,
        ventanaRechazosDias: 7,
        nextCursor: null,
        data: [
            tecnico(31, "Jorge Cifuentes", {
                estado: "al_dia",
                ultimaConexion: hace(2),
                ultimaSincronizacion: hace(3),
                pendientes: 0,
                pendientesEn: hace(2),
                app: { version: "1.4.0", plataforma: "android" },
                plataforma: "android",
                ultimaPosicion: { lat: 14.634915, lon: -90.506882, precisionM: 8, registradaEn: hace(2) },
            }),
            tecnico(32, "Rosa Tzul", {
                estado: "atrasado",
                ultimaConexion: hace(4),
                ultimaSincronizacion: hace(50),
                pendientes: 3,
                pendientesEn: hace(4),
                rechazadas: 2,
                ultimoRechazo: hace(70),
                app: { version: "0.9.2", plataforma: "pwa" },
                plataforma: "pwa",
            }),
            tecnico(33, "Luis Ajanel", { estado: "sin_conexion", ultimaConexion: hace(120), ultimaSincronizacion: hace(125), plataforma: "android" }),
        ],
    };
}

export type ApiAccesos = {
    roles: Rol[];
    accesos: Acceso[];
    sesiones: Schema<"SesionActiva">[];
    actividad: Schema<"ActividadCampo">;
    /** Cuerpos y rutas de lo que cambió estado aquí. */
    peticiones: Peticion[];
    reautenticaciones: () => number;
};

function json(route: Route, cuerpo: unknown, status = 200) {
    return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
}

function problema(route: Route, status: number, codigo: string, detail: string) {
    return route.fulfill({
        status,
        contentType: "application/problem+json",
        body: JSON.stringify({ type: `urn:iris:problema:${codigo}`, title: codigo, status, detail } satisfies Schema<"Problema">),
    });
}

const sensible = (p: string) => CATALOGO.some((c) => c.codigo === p && c.sensible);

/** Registra la API falsa de accesos y roles (después de simularApi). */
export async function simularAccesos(page: Page, usuarios: Usuario[] = [ADMIN, CONSULTA, SOPORTE]): Promise<ApiAccesos> {
    const estado: ApiAccesos = {
        roles: rolesIniciales(),
        accesos: accesosIniciales(),
        sesiones: sesionesIniciales(),
        actividad: actividadInicial(),
        peticiones: [],
        reautenticaciones: () => reaut,
    };
    let reaut = 0;
    let reautenticado = false;
    const roles = new Map<number, Usuario["roles"]>(usuarios.map((u) => [u.id, [...(u.roles ?? [])]]));

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const cabeceras = await req.allHeaders();
        let cuerpo: unknown = undefined;
        try {
            cuerpo = req.postDataJSON();
        } catch {
            cuerpo = undefined;
        }
        const anotar = () =>
            estado.peticiones.push({ metodo, ruta, query: url.searchParams, empresa: cabeceras["x-iris-empresa"], idempotencia: cabeceras["idempotency-key"], cuerpo });
        if (metodo !== "GET" && cabeceras["x-csrf-token"] !== CSRF && ruta !== "/api/v1/sesion") {
            return route.fallback();
        }

        if (metodo === "POST" && ruta === "/api/v1/sesion/reautenticar") {
            anotar();
            reaut++;
            if ((cuerpo as Schema<"EntradaReautenticar">).contrasena !== CONTRASENA) {
                return problema(route, 401, "credenciales_invalidas", "Correo o contraseña incorrectos");
            }
            reautenticado = true;
            return json(route, { validaHasta: new Date(Date.now() + 15 * 60_000).toISOString() } satisfies Schema<"Reautenticado">);
        }

        // Historial de accesos ------------------------------------------------------------
        const accesosUsuario = /^\/api\/v1\/usuarios\/(\d+)\/accesos$/.exec(ruta);
        if (metodo === "GET" && (ruta === "/api/v1/accesos" || accesosUsuario)) {
            anotar();
            const q = url.searchParams;
            const usuario = accesosUsuario ? Number(accesosUsuario[1]) : q.get("usuarioId") ? Number(q.get("usuarioId")) : null;
            const limite = Number(q.get("limit") ?? 50);
            const antes = q.get("cursor") ? Number(atob(q.get("cursor") ?? "")) : Infinity;
            const desde = q.get("desde") ? Date.parse(q.get("desde") ?? "") : -Infinity;
            const hasta = q.get("hasta") ? Date.parse(q.get("hasta") ?? "") : Infinity;
            const filtrados = estado.accesos.filter(
                (a) =>
                    a.id < antes &&
                    (usuario === null || a.usuarioId === usuario) &&
                    (!q.get("canal") || a.canal === q.get("canal")) &&
                    (!q.get("resultado") || a.resultado === q.get("resultado")) &&
                    Date.parse(a.creadoEn) >= desde &&
                    Date.parse(a.creadoEn) < hasta,
            );
            const pagina = filtrados.slice(0, limite);
            const ultimo = pagina.at(-1);
            return json(route, { data: pagina, nextCursor: filtrados.length > limite && ultimo ? btoa(String(ultimo.id)) : null } satisfies Schema<"PaginaAcceso">);
        }

        // Sesiones activas ----------------------------------------------------------------
        const sesiones = /^\/api\/v1\/usuarios\/(\d+)\/sesiones$/.exec(ruta);
        if (metodo === "GET" && sesiones) {
            anotar();
            return json(route, { data: Number(sesiones[1]) === CONSULTA.id ? estado.sesiones : [], nextCursor: null } satisfies Schema<"PaginaSesionActiva">);
        }
        const cerrarUna = /^\/api\/v1\/usuarios\/(\d+)\/sesiones\/(\d+)\/cerrar$/.exec(ruta);
        if (metodo === "POST" && cerrarUna) {
            anotar();
            const id = Number(cerrarUna[2]);
            if (!estado.sesiones.some((s) => s.id === id)) {
                return problema(route, 404, "no_encontrado", "La sesión no existe, ya está cerrada o venció");
            }
            estado.sesiones = estado.sesiones.filter((s) => s.id !== id);
            return json(route, { usuarioId: Number(cerrarUna[1]), cerradas: 1, sesiones: [id] } satisfies Schema<"SesionesCerradas">);
        }
        const cerrarTodas = /^\/api\/v1\/usuarios\/(\d+)\/cerrar-sesiones$/.exec(ruta);
        if (metodo === "POST" && cerrarTodas) {
            anotar();
            const ids = estado.sesiones.filter((s) => !s.actual).map((s) => s.id);
            estado.sesiones = estado.sesiones.filter((s) => s.actual);
            return json(route, { usuarioId: Number(cerrarTodas[1]), cerradas: ids.length, sesiones: ids } satisfies Schema<"SesionesCerradas">);
        }

        // Actividad de la app -------------------------------------------------------------
        if (metodo === "GET" && ruta === "/api/v1/actividad-campo") {
            anotar();
            return json(route, estado.actividad);
        }

        // Roles y permisos ----------------------------------------------------------------
        if (metodo === "GET" && ruta === "/api/v1/permisos") {
            return json(route, { data: CATALOGO, nextCursor: null } satisfies Schema<"PaginaPermisoInfo">);
        }
        if (metodo === "GET" && ruta === "/api/v1/roles") {
            return json(route, { data: estado.roles, nextCursor: null } satisfies Schema<"PaginaRol">);
        }
        const unRol = /^\/api\/v1\/roles\/(\d+)$/.exec(ruta);
        if (metodo === "GET" && unRol) {
            const r = estado.roles.find((x) => x.id === Number(unRol[1]));
            return r ? json(route, r) : problema(route, 404, "no_encontrado", "No existe");
        }
        if (metodo === "POST" && ruta === "/api/v1/roles") {
            anotar();
            const e = cuerpo as Schema<"EntradaCrearRol">;
            const permisos = e.permisos ?? [];
            if (estado.roles.some((r) => r.codigo === e.codigo)) {
                return problema(route, 409, "rol_existe", `Ya existe un rol con el código ${e.codigo}`);
            }
            if (permisos.some(sensible) && !reautenticado) {
                return problema(route, 403, "reautenticacion_requerida", "Crear este rol da permisos sensibles: confirma tu contraseña");
            }
            const nuevo = rol(100 + estado.roles.length, e.codigo, e.nombre, permisos, { descripcion: e.descripcion ?? "" });
            estado.roles.push(nuevo);
            return json(route, nuevo, 201);
        }
        const accionRol = /^\/api\/v1\/roles\/(\d+)\/(editar|activar|desactivar)$/.exec(ruta);
        if (metodo === "POST" && accionRol) {
            anotar();
            const r = estado.roles.find((x) => x.id === Number(accionRol[1]));
            if (!r) return problema(route, 404, "no_encontrado", "No existe");
            if (r.sistema) return problema(route, 422, "rol_base", "Es un rol base");
            const e = cuerpo as Schema<"EntradaEditarRol">;
            if (e.version !== r.version) return problema(route, 409, "conflicto_version", "Alguien más cambió el rol");
            if (accionRol[2] === "editar") {
                const agregados = (e.permisos ?? []).filter((p) => !(r.permisos ?? []).includes(p));
                if (agregados.some(sensible) && !reautenticado) {
                    return problema(route, 403, "reautenticacion_requerida", "Editar este rol da permisos sensibles: confirma tu contraseña");
                }
                Object.assign(r, { nombre: e.nombre ?? r.nombre, descripcion: e.descripcion ?? r.descripcion, permisos: e.permisos ?? r.permisos });
            } else {
                r.activo = accionRol[2] === "activar";
            }
            r.version++;
            return json(route, r);
        }

        // Asignar y quitar roles ----------------------------------------------------------
        const asignacion = /^\/api\/v1\/usuarios\/(\d+)\/(asignar-rol|quitar-rol)$/.exec(ruta);
        if (metodo === "POST" && asignacion) {
            anotar();
            const id = Number(asignacion[1]);
            const u = usuarios.find((x) => x.id === id);
            if (!u) return route.fallback();
            const a = cuerpo as Schema<"AsignacionRol">;
            const mismo = (x: Schema<"AsignacionRol">) => x.rol === a.rol && (x.zonaId ?? null) === (a.zonaId ?? null) && (x.cuadrillaId ?? null) === (a.cuadrillaId ?? null);
            const actuales = roles.get(id) ?? [];
            if (asignacion[2] === "asignar-rol") {
                if (actuales?.some(mismo)) return problema(route, 422, "rol_ya_asignado", "El usuario ya tiene ese rol con ese alcance");
                roles.set(id, [...(actuales ?? []), a]);
            } else {
                if (!actuales?.some(mismo)) return problema(route, 422, "rol_no_asignado", "El usuario no tiene ese rol con ese alcance");
                roles.set(id, (actuales ?? []).filter((x) => !mismo(x)));
            }
            return json(route, { ...u, roles: roles.get(id) ?? [], version: u.version + 1 } satisfies Usuario);
        }
        return route.fallback();
    });
    return estado;
}
