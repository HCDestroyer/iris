import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { cuerpoAsignarRol, esquemaAsignarRol, type DatosAsignarRol } from "@/lib/accesos/esquemas";
import { LONGITUD_MAXIMA_CONTRASENA, LONGITUD_MINIMA_CONTRASENA, longitud } from "@/lib/cuenta/esquemas";

/*
 * Espejo de los validadores de la API para administrar usuarios (OpenAPI: EntradaCrearUsuario,
 * EntradaEditarUsuario, EntradaDesactivarUsuario, EntradaEmpresaUsuario; identidad/crear_usuario.go
 * `normalizarNombreCorreo`, `hashSegunTipo` y `empresasNuevoUsuario`, permisos_efectivos.go
 * `resolverRoles`). La API cuenta en runas: aquí igual (`longitud`).
 */

export type Usuario = Schema<"Usuario">;
export type TipoUsuario = Schema<"EntradaCrearUsuario">["tipo"];

/** Máximos del contrato. */
export const MAX_NOMBRE = 120;
export const MAX_CORREO = 254;
export const MAX_MOTIVO = 500;
export const MAX_ROLES = 50;
export const MAX_EMPRESAS = 50;

const nombre = z
    .string()
    .trim()
    .min(1, "Escribe el nombre")
    .refine((v) => longitud(v) <= MAX_NOMBRE, `Admite hasta ${MAX_NOMBRE} caracteres`);

/** La API lo guarda sin espacios y en minúsculas, y lo compara así (`correo_en_uso`). */
const correo = z
    .string()
    .trim()
    .toLowerCase()
    .min(1, { message: "Escribe el correo", abort: true })
    .max(MAX_CORREO, `Admite hasta ${MAX_CORREO} caracteres`)
    .pipe(z.email("Correo inválido"));

/** Roles de los usuarios de tipo integración (identidad.RolesIntegracion): los únicos que admiten. */
export const ROLES_INTEGRACION = ["integracion_argos", "integracion_horus"] as const;

export function esRolIntegracion(codigo: string): boolean {
    return (ROLES_INTEGRACION as readonly string[]).includes(codigo);
}

/** Clave de una asignación (rol + alcance) para detectar repetidas, como `AsignacionRol.clave()`. */
export function claveAsignacion(a: Schema<"AsignacionRol">): string {
    return `${a.rol}:${a.zonaId ?? 0}:${a.cuadrillaId ?? 0}`;
}

/**
 * Formulario de «Nuevo usuario». `empresasObligatorias`: sin `empresas.todas` y con empresas
 * registradas la API exige al menos una (si no, solo un admin global podría administrarlo).
 */
export function esquemaCrearUsuario({ empresasObligatorias }: { empresasObligatorias: boolean }) {
    return z
        .object({
            nombre,
            correo,
            tipo: z.enum(["persona", "integracion"]),
            contrasena: z.string(),
            roles: z.array(esquemaAsignarRol).max(MAX_ROLES, `Hasta ${MAX_ROLES} roles`),
            empresas: z.array(z.string()).max(MAX_EMPRESAS, `Hasta ${MAX_EMPRESAS} empresas`),
        })
        .superRefine((d, ctx) => {
            if (d.tipo === "persona") {
                if (longitud(d.contrasena) < LONGITUD_MINIMA_CONTRASENA) {
                    ctx.addIssue({ code: "custom", path: ["contrasena"], message: `Usa al menos ${LONGITUD_MINIMA_CONTRASENA} caracteres` });
                } else if (longitud(d.contrasena) > LONGITUD_MAXIMA_CONTRASENA) {
                    ctx.addIssue({ code: "custom", path: ["contrasena"], message: `Admite hasta ${LONGITUD_MAXIMA_CONTRASENA} caracteres` });
                }
            } else if (d.contrasena !== "") {
                ctx.addIssue({ code: "custom", path: ["contrasena"], message: "Las integraciones entran solo con token" });
            }
            const vistas = new Set<string>();
            d.roles.forEach((r, i) => {
                if (!r.rol) return;
                if (d.tipo === "integracion" && (!esRolIntegracion(r.rol) || r.alcance !== "global")) {
                    ctx.addIssue({ code: "custom", path: ["roles", i, "rol"], message: "Una integración solo admite su rol de integración, sin alcance" });
                }
                const clave = claveAsignacion(cuerpoAsignarRol(r));
                if (vistas.has(clave)) {
                    ctx.addIssue({ code: "custom", path: ["roles", i, "rol"], message: "Asignación repetida" });
                }
                vistas.add(clave);
            });
            if (new Set(d.empresas).size !== d.empresas.length) {
                ctx.addIssue({ code: "custom", path: ["empresas"], message: "Empresa repetida" });
            }
            if (empresasObligatorias && d.empresas.length === 0) {
                ctx.addIssue({ code: "custom", path: ["empresas"], message: "Indica al menos una empresa en que operará" });
            }
        });
}

export type DatosCrearUsuario = z.input<ReturnType<typeof esquemaCrearUsuario>>;

/** Rol vacío para agregar una fila. */
export const ROL_VACIO: DatosAsignarRol = { rol: "", alcance: "global", zonaId: "", cuadrillaId: "" };

export function valoresCrearUsuario(rolInicial?: string): DatosCrearUsuario {
    return {
        nombre: "",
        correo: "",
        tipo: "persona",
        contrasena: "",
        roles: rolInicial ? [{ ...ROL_VACIO, rol: rolInicial }] : [],
        empresas: [],
    };
}

/** Selecciones que faltan para poder crear («Falta elegir …»). */
export function faltanEnCrear(d: Partial<DatosCrearUsuario>, empresasObligatorias: boolean): string[] {
    const faltan: string[] = [];
    if ((d.roles ?? []).some((r) => !r.rol)) faltan.push("el rol de cada fila");
    if ((d.roles ?? []).some((r) => (r.alcance === "zona" && !r.zonaId) || (r.alcance === "cuadrilla" && !r.cuadrillaId))) faltan.push("la zona o cuadrilla del alcance");
    if (empresasObligatorias && (d.empresas ?? []).length === 0) faltan.push("al menos una empresa");
    return faltan;
}

/** Cuerpo de `POST /usuarios`. */
export function cuerpoCrearUsuario(d: DatosCrearUsuario): Schema<"EntradaCrearUsuario"> {
    const cuerpo: Schema<"EntradaCrearUsuario"> = {
        nombre: d.nombre.trim(),
        correo: d.correo.trim().toLowerCase(),
        tipo: d.tipo,
    };
    if (d.tipo === "persona") cuerpo.contrasena = d.contrasena;
    if (d.roles.length > 0) cuerpo.roles = d.roles.map(cuerpoAsignarRol);
    if (d.empresas.length > 0) cuerpo.empresas = d.empresas.map(Number).sort((a, b) => a - b);
    return cuerpo;
}

export const esquemaEditarUsuario = z.object({ nombre, correo });

export type DatosEditarUsuario = z.input<typeof esquemaEditarUsuario>;

/** Cuerpo de `POST /usuarios/{id}/editar`: solo lo que cambió (los ausentes no cambian). */
export function cuerpoEditarUsuario(d: DatosEditarUsuario, antes: Usuario): Schema<"EntradaEditarUsuario"> {
    const cuerpo: Schema<"EntradaEditarUsuario"> = { version: antes.version };
    const n = d.nombre.trim();
    const c = d.correo.trim().toLowerCase();
    if (n !== antes.nombre) cuerpo.nombre = n;
    if (c !== antes.correo) cuerpo.correo = c;
    return cuerpo;
}

/** ¿El cambio de correo borra los vínculos de autologin? (editar_usuario.go: otro usuario, no el propio). */
export function cambioCorreoCierraSSO(d: Pick<DatosEditarUsuario, "correo">, antes: Usuario, propio: boolean): boolean {
    return !propio && antes.tipo === "persona" && d.correo.trim().toLowerCase() !== antes.correo;
}

export const esquemaDesactivarUsuario = z.object({
    motivo: z
        .string()
        .trim()
        .min(1, "Escribe el motivo")
        .refine((v) => longitud(v) <= MAX_MOTIVO, `El motivo admite hasta ${MAX_MOTIVO} caracteres`),
});

export type DatosDesactivarUsuario = z.input<typeof esquemaDesactivarUsuario>;

export function cuerpoDesactivarUsuario(d: DatosDesactivarUsuario, version: number): Schema<"EntradaDesactivarUsuario"> {
    return { version, motivo: d.motivo.trim() };
}

export const esquemaAsignarEmpresa = z.object({ empresaId: z.string().min(1, "Elige la empresa") });

export type DatosAsignarEmpresa = z.input<typeof esquemaAsignarEmpresa>;

export function cuerpoEmpresaUsuario(empresaId: number | string): Schema<"EntradaEmpresaUsuario"> {
    return { empresaId: Number(empresaId) };
}

/** Nombre de una empresa por id con las que conoce la sesión; si no opera en ella, su id. */
export function nombreEmpresa(id: number, conocidas: readonly Schema<"EmpresaResumen">[]): string {
    return conocidas.find((e) => e.id === id)?.nombre ?? `Empresa ${id}`;
}
