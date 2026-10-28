import type { Schema } from "@iris/contratos";
import type { BrowserContext, Page, Route } from "@playwright/test";

/*
 * API falsa para Playwright, con fixtures tipados con packages/contratos. Reproduce lo que
 * importa al panel: cookies iris_sesion / iris_csrf, CSRF por doble envío en métodos que cambian
 * estado (403 si falta), MFA, 401/429 y paginación por cursor.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;

export const CSRF = "csrf-e2e";
export const CONTRASENA = "correcta-123";
export const CODIGO_OK = "123456";
export const CODIGO_BLOQUEA = "999999";

const ahora = "2026-10-28T15:00:00Z";

export function usuario(datos: Partial<Schema<"Usuario">> & Pick<Schema<"Usuario">, "id" | "nombre" | "correo">): Schema<"Usuario"> {
    return {
        tipo: "persona",
        activo: true,
        mfaActivo: false,
        roles: [{ rol: "lectura" }],
        version: 1,
        creadoEn: ahora,
        actualizadoEn: ahora,
        ...datos,
    };
}

export const ADMIN = usuario({ id: 1, nombre: "Ana López", correo: "ana@iris.test", roles: [{ rol: "admin" }] });
export const CON_MFA = usuario({ id: 2, nombre: "Mario Pérez", correo: "mario@iris.test", mfaActivo: true, roles: [{ rol: "supervisor" }] });
export const LECTOR = usuario({ id: 3, nombre: "Lucía Ramírez", correo: "lucia@iris.test", roles: [{ rol: "tecnico", zonaId: 4 }] });
export const BLOQUEADO = "bloqueado@iris.test";

export const PERMISOS_ADMIN: Permiso[] = [
    "permisos.ver",
    "roles.ver",
    "tokens.crear",
    "tokens.revocar",
    "tokens.ver",
    "usuarios.crear",
    "usuarios.desactivar",
    "usuarios.editar",
    "usuarios.ver",
];

const permisosPorCorreo: Record<string, Permiso[]> = {
    [ADMIN.correo]: PERMISOS_ADMIN,
    [CON_MFA.correo]: ["usuarios.ver"],
    [LECTOR.correo]: [],
};
const usuarios = [ADMIN, CON_MFA, LECTOR];

export const PAGINA_1: Schema<"PaginaUsuario"> = {
    data: [ADMIN, CON_MFA, LECTOR],
    nextCursor: "cursor-2",
};
export const PAGINA_2: Schema<"PaginaUsuario"> = {
    data: [
        usuario({ id: 4, nombre: "Integración Argos", correo: "argos@integraciones.iris", tipo: "integracion", roles: [{ rol: "integracion" }] }),
        usuario({ id: 5, nombre: "Pedro Gómez", correo: "pedro@iris.test", activo: false, roles: [] }),
    ],
    nextCursor: null,
};

export type ApiFalsa = {
    /** Peticiones que cambiaron estado: método, ruta y X-CSRF-Token recibido. */
    mutaciones: { metodo: string; ruta: string; csrf: string | undefined }[];
    /** Hace fallar `GET /yo` o `GET /usuarios` las próximas `n` veces con 500. */
    fallar: (ruta: "/api/v1/yo" | "/api/v1/usuarios", n: number) => void;
};

function problema(route: Route, p: Problema, cabeceras: Record<string, string> = {}) {
    return route.fulfill({
        status: p.status,
        contentType: "application/problem+json",
        headers: cabeceras,
        body: JSON.stringify(p),
    });
}

const cookiesSesion = (valor: string) =>
    [`iris_sesion=${valor}; Path=/; HttpOnly; SameSite=Lax`, `iris_csrf=${CSRF}; Path=/; SameSite=Lax`].join("\n");

/** Con sesión ya iniciada (cookies como las deja la API). */
export async function conSesion(context: BrowserContext, baseURL: string, correo: string) {
    await context.addCookies([
        { name: "iris_sesion", value: `s:${correo}`, url: baseURL, httpOnly: true, sameSite: "Lax" },
        { name: "iris_csrf", value: CSRF, url: baseURL, sameSite: "Lax" },
    ]);
}

export async function simularApi(page: Page): Promise<ApiFalsa> {
    const mutaciones: ApiFalsa["mutaciones"] = [];
    const fallos = new Map<string, number>();
    const mfaPendiente = new Map<string, number>();

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const cabeceras = await req.allHeaders();
        const cookies = cabeceras.cookie ?? "";
        const sesion = /(?:^|;\s*)iris_sesion=([^;]+)/.exec(cookies)?.[1];
        const correoSesion = sesion?.startsWith("s:") ? decodeURIComponent(sesion.slice(2)) : undefined;
        const csrf = cabeceras["x-csrf-token"];

        if (metodo !== "GET") {
            mutaciones.push({ metodo, ruta, csrf });
        }
        const pendientes = fallos.get(ruta) ?? 0;
        if (pendientes > 0) {
            fallos.set(ruta, pendientes - 1);
            return problema(route, { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 });
        }
        // CSRF por doble envío (salvo el inicio de sesión), como identidad/middleware.go.
        if (metodo !== "GET" && sesion && !(metodo === "POST" && ruta === "/api/v1/sesion") && csrf !== CSRF) {
            return problema(route, { type: "urn:iris:problema:csrf_invalido", title: "CSRF inválido", status: 403 });
        }

        if (metodo === "POST" && ruta === "/api/v1/sesion") {
            const entrada = req.postDataJSON() as Schema<"EntradaIniciarSesion">;
            if (entrada.correo === BLOQUEADO) {
                return problema(
                    route,
                    {
                        type: "urn:iris:problema:demasiados_intentos",
                        title: "Demasiados intentos",
                        status: 429,
                        detail: "Demasiados intentos fallidos con este correo; espera antes de volver a intentar",
                    },
                    { "Retry-After": "540" },
                );
            }
            const u = usuarios.find((x) => x.correo === entrada.correo);
            if (!u || entrada.contrasena !== CONTRASENA) {
                return problema(route, {
                    type: "urn:iris:problema:credenciales_invalidas",
                    title: "Credenciales inválidas",
                    status: 401,
                    detail: "Correo o contraseña incorrectos",
                });
            }
            const respuesta: Schema<"SesionIniciada"> = u.mfaActivo
                ? { csrfToken: CSRF, mfaRequerido: true, expiraEn: "2026-10-28T23:00:00Z" }
                : { csrfToken: CSRF, mfaRequerido: false, expiraEn: "2026-10-28T23:00:00Z", usuario: u };
            if (u.mfaActivo) {
                mfaPendiente.set(u.correo, 0);
            }
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                headers: { "Set-Cookie": cookiesSesion(u.mfaActivo ? `p:${u.correo}` : `s:${u.correo}`) },
                body: JSON.stringify(respuesta),
            });
        }

        if (metodo === "POST" && ruta === "/api/v1/sesion/mfa") {
            const correo = sesion?.startsWith("p:") ? decodeURIComponent(sesion.slice(2)) : undefined;
            const fallidos = correo === undefined ? undefined : mfaPendiente.get(correo);
            if (correo === undefined || fallidos === undefined) {
                return problema(route, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 });
            }
            const { codigo } = req.postDataJSON() as Schema<"EntradaVerificarMFA">;
            if (codigo === CODIGO_BLOQUEA || fallidos >= 4) {
                mfaPendiente.delete(correo);
                return problema(route, {
                    type: "urn:iris:problema:mfa_bloqueado",
                    title: "Demasiados códigos inválidos",
                    status: 401,
                    detail: "La sesión se cerró; vuelve a iniciar sesión",
                });
            }
            if (codigo !== CODIGO_OK) {
                mfaPendiente.set(correo, fallidos + 1);
                return problema(route, {
                    type: "urn:iris:problema:codigo_invalido",
                    title: "Código inválido",
                    status: 401,
                    detail: "El código de verificación no es válido o ya se usó",
                });
            }
            mfaPendiente.delete(correo);
            const u = usuarios.find((x) => x.correo === correo);
            const respuesta: Schema<"SesionIniciada"> = { csrfToken: CSRF, mfaRequerido: false, expiraEn: "2026-10-28T23:00:00Z", ...(u ? { usuario: u } : {}) };
            return route.fulfill({
                status: 200,
                contentType: "application/json",
                headers: { "Set-Cookie": cookiesSesion(`s:${correo}`) },
                body: JSON.stringify(respuesta),
            });
        }

        const actual = usuarios.find((x) => x.correo === correoSesion);
        if (!actual) {
            return problema(route, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 });
        }
        const permisos = permisosPorCorreo[actual.correo] ?? [];

        if (metodo === "DELETE" && ruta === "/api/v1/sesion") {
            return route.fulfill({
                status: 204,
                headers: {
                    "Set-Cookie": [
                        "iris_sesion=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax",
                        "iris_csrf=; Path=/; Max-Age=0; SameSite=Lax",
                    ].join("\n"),
                },
            });
        }
        if (metodo === "GET" && ruta === "/api/v1/yo") {
            const yo: Schema<"Yo"> = { actor: "usuario", usuario: actual, permisos };
            return route.fulfill({ json: yo });
        }
        if (metodo === "GET" && ruta === "/api/v1/usuarios") {
            if (!permisos.includes("usuarios.ver")) {
                return problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
            }
            return route.fulfill({ json: url.searchParams.get("cursor") === PAGINA_1.nextCursor ? PAGINA_2 : PAGINA_1 });
        }
        return problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
    });

    return {
        mutaciones,
        fallar: (ruta, n) => {
            fallos.set(ruta, n);
        },
    };
}
