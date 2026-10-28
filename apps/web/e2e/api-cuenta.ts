import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Cuenta propia y reinicio de MFA en la API falsa (e2e/api-falsa.ts), tipados con
 * packages/contratos y fieles a identidad (i1.md, PR #21):
 *  - contraseña temporal: con `cambioContrasenaRequerido` la sesión solo puede `GET /yo`,
 *    `POST /yo/contrasena` y entrar o salir; el resto, 403 `cambio_contrasena_requerido`;
 *  - `POST /yo/contrasena`: 422 por campo (`actual` incorrecta, `nueva` corta o igual) y 429
 *    `demasiados_intentos` con `Retry-After` que además cierra la sesión;
 *  - `POST /yo/mfa` y `/yo/mfa/activar`;
 *  - `POST /usuarios/{id}/reiniciar-mfa` con `version` (409), `Idempotency-Key`, 404 fuera de
 *    alcance, 422 `reinicio_mfa_propio` / `mfa_no_configurado` / `validacion`.
 */

type Usuario = Schema<"Usuario">;
type Problema = Schema<"Problema">;

export const CONTRASENA_DEFECTO = "correcta-123";
/** Contraseña «actual» que la API falsa trata como el 10.º fallo: 429 y sesión cerrada. */
export const ACTUAL_BLOQUEA = "decimo-intento-malo";
export const SECRETO_MFA = "JBSWY3DPEHPK3PXP";
export const CODIGO_ACTIVAR = "123456";

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema, cabeceras: Record<string, string> = {}) =>
    route.fulfill({ status: p.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(p) });
const runas = (s: string) => [...s].length;

/** Rutas que una sesión con contraseña temporal puede usar (identidad/middleware.go). */
function permitidaConTemporal(metodo: string, ruta: string): boolean {
    return (
        (metodo === "POST" && (ruta === "/api/v1/yo/contrasena" || ruta === "/api/v1/sesion")) ||
        (metodo === "DELETE" && ruta === "/api/v1/sesion") ||
        (metodo === "GET" && ruta === "/api/v1/yo")
    );
}

export type ContextoCuenta = {
    route: Route;
    metodo: string;
    ruta: string;
    actual: Usuario;
    puede: (p: Schema<"Permiso">) => boolean;
    cuerpo: unknown;
    idempotencia: string | undefined;
};

export type ApiCuenta = {
    /** El usuario con lo que cambió en esta prueba (contraseña temporal, MFA, versión). */
    ver: (u: Usuario) => Usuario;
    contrasenaValida: (u: Usuario, contrasena: string) => boolean;
    /** 403 `cambio_contrasena_requerido` si la sesión tiene contraseña temporal y la ruta no está permitida. */
    cortar: (route: Route, u: Usuario, metodo: string, ruta: string) => Promise<boolean>;
    /** El usuario no está en el alcance de quien pide (lista sin él; acciones con 404). */
    fueraDeAlcance: (id: number) => boolean;
    manejar: (c: ContextoCuenta) => Promise<boolean>;
    /** Otra persona cambió el usuario: sube su versión (el próximo reinicio con la vieja da 409). */
    cambiarVersion: (id: number) => void;
    /** El usuario sale del alcance de empresas de quien lo administra. */
    sacarDelAlcance: (id: number) => void;
    /** Cuerpos recibidos en `POST /usuarios/{id}/reiniciar-mfa`. */
    reinicios: { id: number; cuerpo: Schema<"EntradaReiniciarMFA">; idempotencia: string | undefined }[];
};

export function apiCuenta(todos: () => Usuario[]): ApiCuenta {
    const cambios = new Map<number, Partial<Usuario>>();
    const contrasenas = new Map<number, string>();
    const secretoPendiente = new Set<number>();
    const fuera = new Set<number>();
    const reinicios: ApiCuenta["reinicios"] = [];

    const ver = (u: Usuario): Usuario => {
        const c = cambios.get(u.id);
        if (!c) return u;
        const r: Usuario = { ...u, ...c };
        // `cambioContrasenaRequerido` solo viaja presente y en true.
        if (r.cambioContrasenaRequerido !== true) delete r.cambioContrasenaRequerido;
        return r;
    };
    const cambiar = (u: Usuario, c: Partial<Usuario>): Usuario => {
        const previo = ver(u);
        cambios.set(u.id, { ...cambios.get(u.id), ...c, version: previo.version + 1 });
        return ver(u);
    };
    const contrasena = (u: Usuario) => contrasenas.get(u.id) ?? CONTRASENA_DEFECTO;
    const validacion = (route: Route, field: string, message: string) =>
        problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field, message }] });

    return {
        ver,
        reinicios,
        contrasenaValida: (u, c) => c === contrasena(u),
        fueraDeAlcance: (id) => fuera.has(id),
        cambiarVersion: (id) => {
            const u = todos().find((x) => x.id === id);
            if (u) cambios.set(id, { ...cambios.get(id), version: ver(u).version + 1 });
        },
        sacarDelAlcance: (id) => {
            fuera.add(id);
        },
        cortar: async (route, u, metodo, ruta) => {
            if (ver(u).cambioContrasenaRequerido !== true || permitidaConTemporal(metodo, ruta)) {
                return false;
            }
            await problema(route, {
                type: "urn:iris:problema:cambio_contrasena_requerido",
                title: "Cambio de contraseña requerido",
                status: 403,
                detail: "Tienes una contraseña temporal: cámbiala (POST /api/v1/yo/contrasena) antes de continuar",
            });
            return true;
        },
        manejar: async ({ route, metodo, ruta, actual, puede, cuerpo, idempotencia }) => {
            if (metodo === "POST" && ruta === "/api/v1/yo/contrasena") {
                const e = cuerpo as Schema<"EntradaCambiarContrasena">;
                if (e.actual === ACTUAL_BLOQUEA) {
                    await problema(
                        route,
                        {
                            type: "urn:iris:problema:demasiados_intentos",
                            title: "Demasiados intentos",
                            status: 429,
                            detail: "Demasiados intentos con una contraseña actual incorrecta: la sesión se cerró; espera antes de volver a entrar",
                        },
                        {
                            "Retry-After": "900",
                            "Set-Cookie": ["iris_sesion=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax", "iris_csrf=; Path=/; Max-Age=0; SameSite=Lax"].join("\n"),
                        },
                    );
                    return true;
                }
                if (e.actual !== contrasena(actual)) {
                    await validacion(route, "actual", "La contraseña actual no es correcta");
                    return true;
                }
                if (e.nueva === e.actual) {
                    await validacion(route, "nueva", "Debe ser distinta de la actual");
                    return true;
                }
                if (runas(e.nueva) < 12) {
                    await validacion(route, "nueva", "Mínimo 12 caracteres");
                    return true;
                }
                contrasenas.set(actual.id, e.nueva);
                await json(route, cambiar(actual, { cambioContrasenaRequerido: false }));
                return true;
            }
            if (metodo === "POST" && ruta === "/api/v1/yo/mfa") {
                if (ver(actual).mfaActivo) {
                    await problema(route, { type: "urn:iris:problema:mfa_activo", title: "MFA activo", status: 422, detail: "El MFA ya está activo" });
                    return true;
                }
                secretoPendiente.add(actual.id);
                const salida: Schema<"MFAConfigurado"> = {
                    secreto: SECRETO_MFA,
                    url: `otpauth://totp/Iris:${encodeURIComponent(actual.correo)}?secret=${SECRETO_MFA}&issuer=Iris`,
                };
                await json(route, salida);
                return true;
            }
            if (metodo === "POST" && ruta === "/api/v1/yo/mfa/activar") {
                const { codigo } = cuerpo as Schema<"EntradaActivarMFA">;
                if (!secretoPendiente.has(actual.id) || codigo !== CODIGO_ACTIVAR) {
                    await problema(route, { type: "urn:iris:problema:codigo_invalido", title: "Código inválido", status: 422, detail: "El código no coincide; revisa la hora del teléfono" });
                    return true;
                }
                secretoPendiente.delete(actual.id);
                await json(route, cambiar(actual, { mfaActivo: true }));
                return true;
            }
            const reinicio = /^\/api\/v1\/usuarios\/(\d+)\/reiniciar-mfa$/.exec(ruta);
            if (metodo === "POST" && reinicio) {
                const id = Number(reinicio[1]);
                const e = cuerpo as Schema<"EntradaReiniciarMFA">;
                reinicios.push({ id, cuerpo: e, idempotencia });
                if (!puede("usuarios.reiniciar_mfa")) {
                    await problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
                    return true;
                }
                if (!idempotencia) {
                    await problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                    return true;
                }
                const objetivo = todos().find((x) => x.id === id);
                // Fuera de alcance: 404 igual que un id inexistente, antes de mirar la versión.
                if (!objetivo || fuera.has(id)) {
                    await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
                    return true;
                }
                if (id === actual.id) {
                    await problema(route, { type: "urn:iris:problema:reinicio_mfa_propio", title: "Regla de negocio", status: 422, detail: "No puedes reiniciar tu propio MFA; pídeselo a otro administrador" });
                    return true;
                }
                const vigente = ver(objetivo);
                if (e.version !== vigente.version) {
                    await problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: `La versión actual es ${vigente.version}` });
                    return true;
                }
                if (runas(e.contrasenaTemporal) < 12) {
                    await validacion(route, "contrasenaTemporal", "Mínimo 12 caracteres");
                    return true;
                }
                if (!vigente.mfaActivo) {
                    await problema(route, { type: "urn:iris:problema:mfa_no_configurado", title: "Regla de negocio", status: 422, detail: "El usuario no tiene MFA configurado" });
                    return true;
                }
                contrasenas.set(id, e.contrasenaTemporal);
                await json(route, cambiar(objetivo, { mfaActivo: false, cambioContrasenaRequerido: true }));
                return true;
            }
            return false;
        },
    };
}
