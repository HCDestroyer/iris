// Sesión de la app de campo: la misma del panel (cookie `iris_sesion` + CSRF), leída con
// GET /yo y guardada en el teléfono (solo lo no sensible: id, nombre y empresas) para abrir sin
// señal. La empresa elegida se guarda por usuario.
//
// Regla para abrir sin señal: solo con la sesión guardada de un GET /yo correcto de hace menos de
// HORAS_SIN_SENAL horas (12 por defecto, NEXT_PUBLIC_IRIS_CAMPO_HORAS_SIN_SENAL) y sin cierre de
// sesión explícito; si no, «Conéctate para entrar». A los 7 días la sesión guardada se borra.
import type { Schema } from "@iris/contratos";
import { borrarLocal, escribirLocal, leerLocal, leerLocalJson } from "@/lib/almacen";
import { crearClienteIris } from "@/lib/api/cliente";
import { ErrorApi, errorDesdeRespuesta, errorSinConexion } from "@/lib/api/errores";
import { errorSesionAjena, ErrorTransporte } from "@iris/sync";

export type EmpresaResumen = Schema<"EmpresaResumen">;

export interface SesionCampo {
    usuarioId: number;
    nombre: string;
    correo: string;
    empresas: EmpresaResumen[];
    modoEmpresa: Schema<"Yo">["modoEmpresa"];
    /** Empresa activa que dio la API (la única, o la de la cabecera). */
    empresaActiva: number | null;
    /** Último GET /yo correcto de este usuario (ms). */
    verificadoEn: number;
}

const HORA_MS = 60 * 60 * 1000;
const SIETE_DIAS_MS = 7 * 24 * HORA_MS;
const horasConfiguradas = Number(process.env.NEXT_PUBLIC_IRIS_CAMPO_HORAS_SIN_SENAL);
/** Horas que una sesión verificada en línea permite abrir la app sin señal. */
export const HORAS_SIN_SENAL = Number.isFinite(horasConfiguradas) && horasConfiguradas >= 0 ? horasConfiguradas : 12;

/** ¿Se puede abrir sin señal con esta sesión guardada? */
export function usableSinSenal(s: SesionCampo, ahora = Date.now(), horas = HORAS_SIN_SENAL): boolean {
    const edad = ahora - s.verificadoEn;
    return edad >= 0 && edad <= Math.min(horas * HORA_MS, SIETE_DIAS_MS);
}

const CLAVE_SESION = "iris-campo-sesion";
/** Se cerró sesión sin señal: la cookie sigue en la API; al volver la señal se cierra allí. */
const CLAVE_CIERRE_PENDIENTE = "iris-campo-cierre-pendiente";
const claveEmpresa = (usuarioId: number) => `iris-campo-empresa-${usuarioId}`;

function esSesion(v: unknown): v is SesionCampo {
    const x = v as SesionCampo;
    return typeof v === "object" && v !== null && typeof x.usuarioId === "number" && typeof x.verificadoEn === "number" && Array.isArray(x.empresas);
}

/** La sesión guardada (o `null`); pasados 7 días desde la última verificación, se borra. */
export function sesionGuardada(ahora = Date.now()): SesionCampo | null {
    const s = leerLocalJson<SesionCampo | null>(CLAVE_SESION, (v): v is SesionCampo | null => v === null || esSesion(v), null);
    if (s && ahora - s.verificadoEn > SIETE_DIAS_MS) {
        borrarLocal(CLAVE_SESION);
        return null;
    }
    return s;
}

/** Marca un GET /yo correcto del mismo usuario (para la regla sin señal). */
export function marcarVerificada(usuarioId: number, ahora = Date.now()): void {
    const s = sesionGuardada(ahora);
    if (s && s.usuarioId === usuarioId) {
        guardarSesion({ ...s, verificadoEn: ahora });
    }
}

export function cierrePendiente(): boolean {
    return leerLocal(CLAVE_CIERRE_PENDIENTE) === "1";
}

export function marcarCierrePendiente(pendiente: boolean): void {
    if (pendiente) {
        escribirLocal(CLAVE_CIERRE_PENDIENTE, "1");
    } else {
        borrarLocal(CLAVE_CIERRE_PENDIENTE);
    }
}

export function guardarSesion(s: SesionCampo): void {
    escribirLocal(CLAVE_SESION, JSON.stringify(s));
}

/** Olvida la sesión guardada y la empresa elegida de ese usuario (cerrar sesión). */
export function olvidarSesion(usuarioId?: number): void {
    borrarLocal(CLAVE_SESION);
    if (usuarioId !== undefined) {
        borrarLocal(claveEmpresa(usuarioId));
    }
}

export function empresaElegida(usuarioId: number): number | null {
    const id = Number(leerLocal(claveEmpresa(usuarioId)));
    return Number.isSafeInteger(id) && id > 0 ? id : null;
}

export function elegirEmpresa(usuarioId: number, empresa: number | null): void {
    if (empresa === null) {
        borrarLocal(claveEmpresa(usuarioId));
    } else {
        escribirLocal(claveEmpresa(usuarioId), String(empresa));
    }
}

export function sesionDesdeYo(yo: Schema<"Yo">): SesionCampo {
    return {
        usuarioId: yo.usuario.id,
        nombre: yo.usuario.nombre,
        correo: yo.usuario.correo,
        empresas: yo.empresas ?? [],
        modoEmpresa: yo.modoEmpresa,
        empresaActiva: yo.empresaActiva?.id ?? null,
        verificadoEn: Date.now(),
    };
}

/**
 * GET /yo con la empresa elegida. Un 403 `empresa_no_permitida` (la guardada ya no vale) se
 * repite sin cabecera. Lanza `ErrorApi` (401 = sin sesión; estado 0 = sin señal).
 */
export async function leerYo(empresa: number | null): Promise<Schema<"Yo">> {
    const pedir = async (conEmpresa: number | null) => {
        const cliente = crearClienteIris({ leerEmpresa: () => conEmpresa ?? undefined, alProblemaEmpresa: () => undefined });
        const r = await cliente.GET("/api/v1/yo", { headers: { "X-Iris-Canal": "campo" } }).catch((e: unknown) => {
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
 * Empresa con la que trabaja el teléfono: `null` sin conector; la única o la elegida; `undefined`
 * si hay varias y falta elegir (o el usuario no tiene ninguna).
 */
export function empresaDeTrabajo(s: SesionCampo, elegida: number | null): number | null | undefined {
    if (s.modoEmpresa === "sin_conector") {
        return null;
    }
    if (elegida !== null && s.empresas.some((e) => e.id === elegida)) {
        return elegida;
    }
    if (s.empresas.length === 1) {
        return s.empresas[0]?.id;
    }
    return undefined;
}

/**
 * Antes de cada envío o bajada del motor: GET /yo debe ser del dueño de la cola (y de su
 * empresa). Si no, `errorSesionAjena()` (la PWA desmonta el motor sin enviar nada). Errores de
 * red o de sesión vuelven como `ErrorTransporte` para el motor.
 */
export async function verificarDueno(usuarioId: number, empresaId: number | null): Promise<void> {
    let yo: Schema<"Yo">;
    try {
        yo = await leerYo(empresaId);
    } catch (e) {
        if (e instanceof ErrorApi) {
            throw e.estado === 0
                ? new ErrorTransporte("red", e.message)
                : new ErrorTransporte("http", e.titulo, e.estado, { type: `urn:iris:problema:${e.codigo}`, title: e.titulo, status: e.estado });
        }
        throw e;
    }
    if (yo.usuario.id !== usuarioId) {
        throw errorSesionAjena("La sesión abierta en este navegador es de otro usuario");
    }
    if (empresaId !== null && yo.empresaActiva?.id !== empresaId) {
        throw errorSesionAjena("La sesión abierta ya no trabaja en esta empresa");
    }
    marcarVerificada(usuarioId);
}

/** DELETE /sesion con el canal de campo; lanza si no hay señal (o la API falla). */
export async function cerrarSesionApi(): Promise<void> {
    const cliente = crearClienteIris({ alProblemaEmpresa: () => undefined });
    const r = await cliente.DELETE("/api/v1/sesion", { headers: { "X-Iris-Canal": "campo" } }).catch((e: unknown) => {
        throw errorSinConexion(e);
    });
    if (!r.response.ok && r.response.status !== 401) {
        throw errorDesdeRespuesta(r.response, r.error);
    }
}
