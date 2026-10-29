// Sesión de la app nativa: el mismo flujo que la PWA (`POST /sesion`, `POST /sesion/mfa` con el
// código TOTP, `GET /yo`), con la cookie que guarda el teléfono y el CSRF en el llavero. Se guarda
// en el teléfono solo lo no sensible (id, nombre, empresas) para abrir sin señal con la misma regla
// que la PWA: una verificación en línea de hace menos de 12 h, y a los 7 días se borra.
import type { Schema } from "@iris/contratos";
import { errorSesionAjena, ErrorTransporte, PREFIJO_PROBLEMA } from "@iris/sync";
import { crearClienteMovil } from "../api/cliente";
import { ErrorApi, errorDesdeRespuesta, errorSinConexion, resolver } from "../api/errores";
import { entorno, leerJson } from "../entorno";

export type EmpresaResumen = Schema<"EmpresaResumen">;
export type SesionIniciada = Schema<"SesionIniciada">;

export interface SesionCampo {
    usuarioId: number;
    nombre: string;
    correo: string;
    empresas: EmpresaResumen[];
    modoEmpresa: Schema<"Yo">["modoEmpresa"];
    empresaActiva: number | null;
    /** Último GET /yo correcto de este usuario (ms). */
    verificadoEn: number;
}

const HORA_MS = 60 * 60 * 1000;
const SIETE_DIAS_MS = 7 * 24 * HORA_MS;
const horas = Number(process.env.EXPO_PUBLIC_IRIS_CAMPO_HORAS_SIN_SENAL);
/** Horas que una sesión verificada en línea permite abrir la app sin señal (12, como la PWA). */
export const HORAS_SIN_SENAL = Number.isFinite(horas) && horas >= 0 ? horas : 12;

export const CLAVE_SESION = "iris-campo-sesion";
export const CLAVE_CSRF = "iris-campo-csrf";
/** Se cerró sesión sin poder avisar a la API: bloquea la entrada automática hasta lograr el DELETE. */
export const CLAVE_CIERRE_PENDIENTE = "iris-campo-cierre-pendiente";
/** CSRF de la sesión que falta cerrar (el DELETE lo necesita). */
const CLAVE_CSRF_CIERRE = "iris-campo-csrf-cierre";
export const CODIGO_CAMBIO_CONTRASENA = "cambio_contrasena_requerido";
const claveEmpresa = (usuarioId: number) => `iris-campo-empresa-${usuarioId}`;

let csrf: string | null = null;

/** Token CSRF en memoria (lo leen los clientes al armar cada petición). */
export const leerCsrf = (): string | null => csrf;

/** Carga el CSRF guardado en el llavero (al abrir la app). */
export async function cargarCsrf(): Promise<void> {
    csrf = await entorno().secreto.leer(CLAVE_CSRF);
}

async function guardarCsrf(valor: string | null): Promise<void> {
    csrf = valor;
    if (valor) {
        await entorno().secreto.escribir(CLAVE_CSRF, valor);
    } else {
        await entorno().secreto.borrar(CLAVE_CSRF);
    }
}

/** Cliente con la sesión del teléfono (y la empresa fija si se da). */
export function cliente(empresa?: number | null) {
    const e = entorno();
    return crearClienteMovil({ baseUrl: e.baseUrl, fetch: e.fetch, leerCsrf, empresa: empresa ?? null });
}

/** ¿Se puede abrir sin señal con esta sesión guardada? */
export function usableSinSenal(s: SesionCampo, ahora = Date.now(), horasSinSenal = HORAS_SIN_SENAL): boolean {
    const edad = ahora - s.verificadoEn;
    return edad >= 0 && edad <= Math.min(horasSinSenal * HORA_MS, SIETE_DIAS_MS);
}

function esSesion(v: unknown): v is SesionCampo {
    const x = v as SesionCampo;
    return typeof v === "object" && v !== null && typeof x.usuarioId === "number" && typeof x.verificadoEn === "number" && Array.isArray(x.empresas);
}

/** La sesión guardada (o `null`); a los 7 días sin verificar se borra. */
export function sesionGuardada(ahora = Date.now()): SesionCampo | null {
    const s = leerJson(CLAVE_SESION, esSesion);
    if (s && ahora - s.verificadoEn > SIETE_DIAS_MS) {
        entorno().claves.borrar(CLAVE_SESION);
        return null;
    }
    return s;
}

export function guardarSesion(s: SesionCampo): void {
    entorno().claves.escribir(CLAVE_SESION, JSON.stringify(s));
}

export function sesionDesdeYo(yo: Schema<"Yo">, ahora = Date.now()): SesionCampo {
    return {
        usuarioId: yo.usuario.id,
        nombre: yo.usuario.nombre,
        correo: yo.usuario.correo,
        empresas: yo.empresas ?? [],
        modoEmpresa: yo.modoEmpresa,
        empresaActiva: yo.empresaActiva?.id ?? null,
        verificadoEn: ahora,
    };
}

export function empresaElegida(usuarioId: number): number | null {
    const id = Number(entorno().claves.leer(claveEmpresa(usuarioId)));
    return Number.isSafeInteger(id) && id > 0 ? id : null;
}

export function elegirEmpresa(usuarioId: number, empresa: number | null): void {
    if (empresa === null) {
        entorno().claves.borrar(claveEmpresa(usuarioId));
    } else {
        entorno().claves.escribir(claveEmpresa(usuarioId), String(empresa));
    }
}

/** `null` sin conector; la única o la elegida; `undefined` si falta elegir. */
export function empresaDeTrabajo(s: SesionCampo, elegida: number | null): number | null | undefined {
    if (s.modoEmpresa === "sin_conector") {
        return null;
    }
    if (elegida !== null && s.empresas.some((e) => e.id === elegida)) {
        return elegida;
    }
    return s.empresas.length === 1 ? s.empresas[0]?.id : undefined;
}

/** `POST /sesion`: guarda el CSRF; con MFA activo la sesión queda pendiente del código. */
export async function iniciarSesion(datos: Schema<"EntradaIniciarSesion">): Promise<SesionIniciada> {
    // Si quedó una sesión por cerrar en la API, se intenta antes (la nueva la reemplaza en el teléfono).
    await reintentarCierrePendiente();
    const r = await resolver(cliente().POST("/api/v1/sesion", { body: datos }));
    await guardarCsrf(r.csrfToken);
    await olvidarCierrePendiente();
    return r;
}

/** `POST /sesion/mfa` con el código TOTP: la API cambia la sesión por una nueva (y su CSRF). */
export async function verificarMfa(datos: Schema<"EntradaVerificarMFA">): Promise<SesionIniciada> {
    const r = await resolver(cliente().POST("/api/v1/sesion/mfa", { body: datos }));
    await guardarCsrf(r.csrfToken);
    return r;
}

/** GET /yo con la empresa elegida; un 403 `empresa_no_permitida` se repite sin ella. */
export async function leerYo(empresa: number | null): Promise<Schema<"Yo">> {
    const pedir = async (conEmpresa: number | null) => {
        const r = await cliente(conEmpresa)
            .GET("/api/v1/yo")
            .catch((e: unknown) => {
                throw errorSinConexion(e);
            });
        if (!r.response.ok || !r.data) {
            throw errorDesdeRespuesta(r.response, r.error);
        }
        return r.data;
    };
    try {
        return await pedir(empresa);
    } catch (e) {
        if (empresa !== null && e instanceof ErrorApi && e.codigo === "empresa_no_permitida") {
            return pedir(null);
        }
        throw e;
    }
}

/**
 * Antes de cada envío o bajada del motor (como la PWA): la sesión del teléfono debe seguir siendo
 * del dueño de la cola y de su empresa. Si no, `errorSesionAjena()` y no sale nada.
 */
export async function verificarDueno(usuarioId: number, empresaId: number | null, ahora: () => number = Date.now): Promise<void> {
    let yo: Schema<"Yo">;
    try {
        yo = await leerYo(empresaId);
    } catch (e) {
        if (e instanceof ErrorApi) {
            throw e.estado === 0
                ? new ErrorTransporte("red", e.message)
                : new ErrorTransporte("http", e.titulo, e.estado, { type: `${PREFIJO_PROBLEMA}${e.codigo}`, title: e.titulo, status: e.estado });
        }
        throw e;
    }
    if (yo.usuario.id !== usuarioId) {
        throw errorSesionAjena("La sesión abierta en este teléfono es de otro usuario");
    }
    if (yo.usuario.cambioContrasenaRequerido) {
        throw new ErrorTransporte("http", "Tienes que cambiar tu contraseña", 403, {
            type: `${PREFIJO_PROBLEMA}${CODIGO_CAMBIO_CONTRASENA}`,
            title: "Tienes que cambiar tu contraseña",
            status: 403,
        });
    }
    if (empresaId !== null && yo.empresaActiva?.id !== empresaId) {
        throw errorSesionAjena("La sesión abierta ya no trabaja en esta empresa");
    }
    const guardada = sesionGuardada(ahora());
    if (guardada?.usuarioId === usuarioId) {
        guardarSesion({ ...guardada, verificadoEn: ahora() });
    }
}

/** `DELETE /sesion` con el CSRF de esa sesión. Un 401 (ya no existía) cuenta como cerrada. */
async function borrarSesionApi(csrfSesion: string | null): Promise<void> {
    const e = entorno();
    const c = crearClienteMovil({ baseUrl: e.baseUrl, fetch: e.fetch, leerCsrf: () => csrfSesion });
    try {
        await resolver(c.DELETE("/api/v1/sesion"));
    } catch (error) {
        if (!(error instanceof ErrorApi && error.estado === 401)) {
            throw error;
        }
    }
}

async function olvidarCierrePendiente(): Promise<void> {
    entorno().claves.borrar(CLAVE_CIERRE_PENDIENTE);
    await entorno().secreto.borrar(CLAVE_CSRF_CIERRE);
}

/** ¿Quedó un cierre de sesión sin llegar a la API? */
export function hayCierrePendiente(): boolean {
    return entorno().claves.leer(CLAVE_CIERRE_PENDIENTE) === "1";
}

/**
 * Reintenta el `DELETE /sesion` de un cierre hecho sin señal. Devuelve `true` si no queda nada por
 * cerrar. Mientras devuelva `false`, la app no entra sola con la cookie vieja (pide la contraseña).
 */
export async function reintentarCierrePendiente(): Promise<boolean> {
    if (!hayCierrePendiente()) {
        return true;
    }
    try {
        await borrarSesionApi(await entorno().secreto.leer(CLAVE_CSRF_CIERRE));
    } catch {
        return false;
    }
    await olvidarCierrePendiente();
    return true;
}

/**
 * Cierra la sesión: olvida en el teléfono el CSRF, la sesión guardada y la empresa elegida, y hace
 * `DELETE /sesion`. Si no llega a la API (sin señal o error), queda un «cierre pendiente» que bloquea
 * la entrada automática y se reintenta antes de cualquier `GET /yo` o inicio de sesión.
 */
export async function cerrarSesion(usuarioId: number): Promise<void> {
    const csrfSesion = csrf;
    await guardarCsrf(null);
    entorno().claves.borrar(CLAVE_SESION);
    elegirEmpresa(usuarioId, null);
    try {
        await borrarSesionApi(csrfSesion);
    } catch {
        entorno().claves.escribir(CLAVE_CIERRE_PENDIENTE, "1");
        if (csrfSesion) {
            await entorno().secreto.escribir(CLAVE_CSRF_CIERRE, csrfSesion);
        }
    }
}
