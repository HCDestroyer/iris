import type { Problema } from "./contrato-sync";

/** Código de un problema de la API (`urn:iris:problema:<codigo>`). */
export function codigoProblema(problema: Problema | null | undefined): string | null {
    if (!problema) {
        return null;
    }
    const prefijo = "urn:iris:problema:";
    return problema.type.startsWith(prefijo) ? problema.type.slice(prefijo.length) : problema.type;
}

/**
 * Falla al hablar con la API. `red`: sin señal o sin respuesta (se reintenta con espera);
 * `http`: la API respondió con error (`status` y, si vino, el problema RFC 9457).
 */
export class ErrorTransporte extends Error {
    readonly tipo: "red" | "http";
    readonly status: number | null;
    readonly problema: Problema | null;

    constructor(tipo: "red" | "http", mensaje: string, status: number | null = null, problema: Problema | null = null) {
        super(mensaje);
        this.name = "ErrorTransporte";
        this.tipo = tipo;
        this.status = status;
        this.problema = problema;
    }

    /** Sin sesión o sin permiso: no sirve reintentar hasta que el técnico vuelva a entrar. */
    get esDeSesion(): boolean {
        return this.status === 401 || this.status === 403;
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
    | "operacion_no_rechazada";

/** El teléfono no acepta la acción (antes de encolarla). */
export class ErrorSync extends Error {
    readonly codigo: CodigoErrorLocal;

    constructor(codigo: CodigoErrorLocal, mensaje: string) {
        super(mensaje);
        this.name = "ErrorSync";
        this.codigo = codigo;
    }
}
