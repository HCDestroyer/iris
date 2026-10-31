import type { Schema } from "@iris/contratos";
import type { Page, Route } from "@playwright/test";
import { CSRF, PAGINA_1, PAGINA_2, type Peticion } from "./api-falsa";

/*
 * API falsa de la administración de usuarios (identidad/crear_usuario.go, editar_usuario.go,
 * desactivar_usuario.go y empresas.go). Se registra DESPUÉS de simularApi (y de simularAccesos):
 * Playwright corre primero la última ruta, y lo que no es de aquí sigue con route.fallback().
 *  - `POST /usuarios`: 422 por campo (contraseña corta, integración con contraseña o con un rol
 *    que no es de integración, empresas obligatorias), 409 `correo_en_uso`; responde 201 con el
 *    `Usuario` (una persona, con `cambioContrasenaRequerido`).
 *  - `POST /usuarios/{id}/editar` y `/desactivar`: `version` (409), 422 `ultimo_admin`.
 *  - `POST /usuarios/{id}/asignar-empresa` y `/quitar-empresa`: 422 `empresa_ya_asignada` /
 *    `empresa_no_asignada`.
 *  - `GET /usuarios/{id}`: el usuario con lo que cambió aquí (o el creado).
 *  - `GET /usuarios` (primera página, sin `q`): los creados aquí van arriba, como en la API (id desc).
 */

type Usuario = Schema<"Usuario">;

export type OpcionesUsuarios = {
    /** Sin `empresas.todas` y con empresas registradas: crear exige al menos una empresa. */
    empresasObligatorias?: boolean;
    /** Ids cuya desactivación deja a Iris sin administradores (422 `ultimo_admin`). */
    ultimosAdmin?: number[];
    /** Además de la lista general, usuarios que la API conoce (p. ej. CONSULTA). */
    extra?: Usuario[];
};

export type ApiUsuarios = {
    /** Usuarios conocidos por id, con lo que cambió en la prueba. */
    usuarios: Map<number, Usuario>;
    /** Cuerpos de lo que cambió estado aquí. */
    peticiones: Peticion[];
    /** Otra persona cambió el usuario: sube su versión (la próxima acción con la vieja da 409). */
    cambiarVersion: (id: number) => void;
    /** Se llama al crear (p. ej. para ofrecerlo como candidato a técnico). */
    alCrear: (fn: (u: Usuario) => void) => void;
};

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, status: number, codigo: string, detail: string, errors?: Schema<"ErrorCampo">[]) =>
    route.fulfill({
        status,
        contentType: "application/problem+json",
        body: JSON.stringify({ type: `urn:iris:problema:${codigo}`, title: codigo === "validacion" ? "Datos inválidos" : codigo, status, detail, ...(errors ? { errors } : {}) } satisfies Schema<"Problema">),
    });
const runas = (s: string) => [...s].length;

export async function simularUsuarios(page: Page, opciones: OpcionesUsuarios = {}): Promise<ApiUsuarios> {
    const usuarios = new Map<number, Usuario>();
    for (const u of [...(PAGINA_1.data ?? []), ...(PAGINA_2.data ?? []), ...(opciones.extra ?? [])]) usuarios.set(u.id, structuredClone(u));
    const peticiones: Peticion[] = [];
    const oyentes: ((u: Usuario) => void)[] = [];
    const INICIO = 500;
    let siguiente = INICIO;
    const ahora = () => new Date().toISOString();
    const tocar = (u: Usuario, cambios: Partial<Usuario>): Usuario => {
        const nuevo = { ...u, ...cambios, version: u.version + 1, actualizadoEn: ahora() };
        usuarios.set(u.id, nuevo);
        return nuevo;
    };

    await page.route("**/api/v1/usuarios**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const cabeceras = await req.allHeaders();
        if (metodo !== "GET" && cabeceras["x-csrf-token"] !== CSRF) return route.fallback();
        let cuerpo: unknown = undefined;
        try {
            cuerpo = req.postDataJSON();
        } catch {
            cuerpo = undefined;
        }
        const anotar = () => peticiones.push({ metodo, ruta, query: url.searchParams, empresa: cabeceras["x-iris-empresa"], idempotencia: cabeceras["idempotency-key"], cuerpo });

        if (metodo === "POST" && ruta === "/api/v1/usuarios") {
            anotar();
            const e = cuerpo as Schema<"EntradaCrearUsuario">;
            const tipo = e.tipo ?? "persona";
            const errores: Schema<"ErrorCampo">[] = [];
            if (tipo === "persona" && runas(e.contrasena ?? "") < 12) errores.push({ field: "body.contrasena", message: "Mínimo 12 caracteres" });
            if (tipo === "integracion" && e.contrasena) errores.push({ field: "body.contrasena", message: "Las integraciones entran solo con token" });
            (e.roles ?? []).forEach((r, i) => {
                if (tipo === "integracion" && !["integracion_argos", "integracion_horus"].includes(r.rol)) {
                    errores.push({ field: `body.roles[${i}].rol`, message: "Una integración solo admite su rol de integración (integracion_argos, integracion_horus), sin alcance" });
                }
            });
            if (opciones.empresasObligatorias && (e.empresas ?? []).length === 0) errores.push({ field: "body.empresas", message: "Indica al menos una empresa en que operará" });
            if (errores.length > 0) return problema(route, 422, "validacion", "Datos inválidos", errores);
            if ([...usuarios.values()].some((u) => u.correo === e.correo)) return problema(route, 409, "correo_en_uso", `Ya existe un usuario con el correo ${e.correo}`);
            const u: Usuario = {
                id: siguiente++,
                nombre: e.nombre,
                correo: e.correo,
                tipo,
                activo: true,
                mfaActivo: false,
                roles: e.roles ?? [],
                empresas: e.empresas ?? [],
                // Una persona creada por otra entra con una contraseña temporal (crear_usuario.go).
                ...(tipo === "persona" ? { cambioContrasenaRequerido: true } : {}),
                version: 1,
                creadoEn: ahora(),
                actualizadoEn: ahora(),
            };
            usuarios.set(u.id, u);
            for (const fn of oyentes) fn(u);
            return json(route, u, 201);
        }

        // La lista de siempre (api-falsa) con los recién creados arriba.
        if (metodo === "GET" && ruta === "/api/v1/usuarios" && !url.searchParams.get("q") && !url.searchParams.get("cursor") && siguiente > INICIO) {
            const creados = [...usuarios.values()].filter((x) => x.id >= INICIO).sort((a, b) => b.id - a.id);
            const resto = (PAGINA_1.data ?? []).map((x) => usuarios.get(x.id) ?? x);
            return json(route, { ...PAGINA_1, data: [...creados, ...resto] } satisfies Schema<"PaginaUsuario">);
        }

        const m = /^\/api\/v1\/usuarios\/(\d+)(?:\/(editar|desactivar|asignar-empresa|quitar-empresa))?$/.exec(ruta);
        const u = m ? usuarios.get(Number(m[1])) : undefined;
        if (!m || !u) return route.fallback();
        if (metodo === "GET" && !m[2]) return json(route, u);
        if (metodo !== "POST" || !m[2]) return route.fallback();
        anotar();
        if (m[2] === "editar") {
            const e = cuerpo as Schema<"EntradaEditarUsuario">;
            if (e.version !== u.version) return problema(route, 409, "conflicto_version", "El usuario cambió desde que lo leíste");
            if (e.correo && [...usuarios.values()].some((x) => x.id !== u.id && x.correo === e.correo)) {
                return problema(route, 409, "correo_en_uso", `Ya existe un usuario con el correo ${e.correo}`);
            }
            return json(route, tocar(u, { ...(e.nombre ? { nombre: e.nombre } : {}), ...(e.correo ? { correo: e.correo } : {}) }));
        }
        if (m[2] === "desactivar") {
            const e = cuerpo as Schema<"EntradaDesactivarUsuario">;
            if (e.version !== u.version) return problema(route, 409, "conflicto_version", "El usuario cambió desde que lo leíste");
            if (!u.activo) return problema(route, 422, "usuario_inactivo", "El usuario ya está desactivado");
            if (opciones.ultimosAdmin?.includes(u.id)) {
                return problema(route, 422, "ultimo_admin", "El cambio dejaría a Iris sin ningún administrador activo con todos los permisos de administración");
            }
            return json(route, tocar(u, { activo: false }));
        }
        const { empresaId } = cuerpo as Schema<"EntradaEmpresaUsuario">;
        const actuales = u.empresas ?? [];
        if (m[2] === "asignar-empresa") {
            if (actuales.includes(empresaId)) return problema(route, 422, "empresa_ya_asignada", "El usuario ya tiene esa empresa");
            return json(route, tocar(u, { empresas: [...actuales, empresaId].sort((a, b) => a - b) }));
        }
        if (!actuales.includes(empresaId)) return problema(route, 422, "empresa_no_asignada", "El usuario no tiene esa empresa");
        return json(route, tocar(u, { empresas: actuales.filter((x) => x !== empresaId) }));
    });

    return {
        usuarios,
        peticiones,
        cambiarVersion: (id) => {
            const u = usuarios.get(id);
            if (u) usuarios.set(id, { ...u, version: u.version + 1 });
        },
        alCrear: (fn) => oyentes.push(fn),
    };
}
