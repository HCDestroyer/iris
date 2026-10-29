import type { Problema } from "./contrato";

export const PREFIJO_PROBLEMA = "urn:iris:problema:";

/** La sesión del navegador es de otra persona (o de otra empresa) que la dueña de la cola. */
export const CODIGO_SESION_AJENA = "sesion_ajena";

/** Error para `verificarIdentidad` cuando la sesión no es la del dueño de la cola. */
export function errorSesionAjena(detalle = "La sesión abierta es de otro usuario"): ErrorTransporte {
    return new ErrorTransporte("http", detalle, 401, { type: `${PREFIJO_PROBLEMA}${CODIGO_SESION_AJENA}`, title: "Sesión de otro usuario", status: 401, detail: detalle });
}

/** Código de un problema de la API (`urn:iris:problema:<codigo>`). */
export function codigoProblema(problema: Problema | null | undefined): string | null {
    if (!problema) {
        return null;
    }
    return problema.type.startsWith(PREFIJO_PROBLEMA) ? problema.type.slice(PREFIJO_PROBLEMA.length) : problema.type;
}

/**
 * Falla al hablar con la API. `red`: sin señal o sin respuesta (se reintenta con espera);
 * `http`: la API respondió con error (`status`, el problema RFC 9457 si vino y sus cabeceras).
 */
export class ErrorTransporte extends Error {
    readonly tipo: "red" | "http";
    readonly status: number | null;
    readonly problema: Problema | null;
    readonly cabeceras: Headers | null;

    constructor(
        tipo: "red" | "http",
        mensaje: string,
        status: number | null = null,
        problema: Problema | null = null,
        cabeceras: Headers | null = null,
    ) {
        super(mensaje);
        this.name = "ErrorTransporte";
        this.tipo = tipo;
        this.status = status;
        this.problema = problema;
        this.cabeceras = cabeceras;
    }

    get codigo(): string | null {
        return codigoProblema(this.problema);
    }

    /**
     * Sin sesión, sin permiso o con la de otra persona: no sirve reintentar hasta que entre el
     * dueño de la cola. Incluye 409 `dispositivo_ajeno` (el teléfono es de otro usuario en la API)
     * y `sesion_ajena` (la verificación de identidad del adaptador no coincide).
     */
    get esDeSesion(): boolean {
        const c = this.codigo;
        return this.status === 401 || (this.status === 403 && !this.esDeEmpresa) || c === "dispositivo_ajeno" || c === CODIGO_SESION_AJENA;
    }

    /** La empresa activa no vale (403 `empresa_no_permitida` / `sin_empresa`, 422 `empresa_requerida`). */
    get esDeEmpresa(): boolean {
        const c = this.codigo;
        return c === "empresa_no_permitida" || c === "sin_empresa" || c === "empresa_requerida";
    }

    /** Red, 408, 429 y 5xx: vale la pena reintentar con espera. */
    get esReintentable(): boolean {
        return this.tipo === "red" || this.status === 408 || this.status === 429 || (this.status !== null && this.status >= 500);
    }
}

export type CodigoErrorLocal =
    | "orden_desconocida"
    | "orden_reasignada"
    | "orden_cancelada"
    | "orden_terminada"
    | "operacion_desconocida"
    | "operacion_no_rechazada"
    | "adjunto_desconocido"
    | "adjunto_no_rechazado"
    | "orden_pausada"
    | "orden_no_pausada"
    | "transicion_invalida"
    | "nota_requerida"
    | "plantilla_desconocida"
    | "formulario_duplicado"
    | "formulario_desconocido"
    | "formulario_del_tipo"
    | "formulario_ajeno"
    | "formulario_con_respuestas";

/** El teléfono no acepta la acción (antes de encolarla). */
export class ErrorSync extends Error {
    readonly codigo: CodigoErrorLocal;

    constructor(codigo: CodigoErrorLocal, mensaje: string) {
        super(mensaje);
        this.name = "ErrorSync";
        this.codigo = codigo;
    }
}
