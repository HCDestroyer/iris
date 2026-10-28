// Sesión de la app de campo: la misma del panel (cookie `iris_sesion` + CSRF), leída con
// GET /yo y guardada en el teléfono (solo lo no sensible: id, nombre y empresas) para abrir sin
// señal. La empresa elegida se guarda por usuario.
import type { Schema } from "@iris/contratos";
import { borrarLocal, escribirLocal, leerLocal, leerLocalJson } from "@/lib/almacen";
import { crearClienteIris } from "@/lib/api/cliente";
import { ErrorApi, errorDesdeRespuesta, errorSinConexion } from "@/lib/api/errores";

export type EmpresaResumen = Schema<"EmpresaResumen">;

export interface SesionCampo {
    usuarioId: number;
    nombre: string;
    correo: string;
    empresas: EmpresaResumen[];
    modoEmpresa: Schema<"Yo">["modoEmpresa"];
    /** Empresa activa que dio la API (la única, o la de la cabecera). */
    empresaActiva: number | null;
}

const CLAVE_SESION = "iris-campo-sesion";
const claveEmpresa = (usuarioId: number) => `iris-campo-empresa-${usuarioId}`;

function esSesion(v: unknown): v is SesionCampo {
    return typeof v === "object" && v !== null && typeof (v as SesionCampo).usuarioId === "number" && Array.isArray((v as SesionCampo).empresas);
}

export function sesionGuardada(): SesionCampo | null {
    return leerLocalJson<SesionCampo | null>(CLAVE_SESION, (v): v is SesionCampo | null => v === null || esSesion(v), null);
}

export function guardarSesion(s: SesionCampo): void {
    escribirLocal(CLAVE_SESION, JSON.stringify(s));
}

export function olvidarSesion(): void {
    borrarLocal(CLAVE_SESION);
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
