// Entrada del técnico (diseño «Login C · Combinada», nov 2026), igual en la PWA y la app nativa: el
// usuario recordado del teléfono, los mensajes de error (en línea con el campo cuando son del campo,
// arriba cuando son de la sesión o de la red), los intentos que quedan solo si la API los informa,
// cuándo se ofrece la huella y el aviso «Sin conexión». Sin DOM ni React.

/** Clave (no sensible) del último usuario que entró en este teléfono: nombre y correo, nada más. */
export const CLAVE_USUARIO_RECORDADO = "iris-campo-ultimo-usuario";

export interface UsuarioRecordado {
    nombre: string;
    correo: string;
}

/** El usuario recordado guardado como texto (o `null` si falta o no tiene la forma esperada). */
export function leerUsuarioRecordado(texto: string | null): UsuarioRecordado | null {
    if (!texto) {
        return null;
    }
    try {
        const v = JSON.parse(texto) as Partial<UsuarioRecordado> | null;
        if (v && typeof v.correo === "string" && v.correo.includes("@") && typeof v.nombre === "string") {
            return { nombre: v.nombre.trim() || v.correo, correo: v.correo.trim().toLowerCase() };
        }
    } catch {
        // Ilegible: como si no hubiera.
    }
    return null;
}

/** Lo que se guarda: solo nombre y correo (nunca la contraseña ni un token). */
export function textoUsuarioRecordado(u: UsuarioRecordado): string {
    return JSON.stringify({ nombre: u.nombre, correo: u.correo.trim().toLowerCase() });
}

/** Un error de la API reducido a lo que la pantalla de entrada necesita. */
export interface ErrorEntrada {
    /** 0 = sin conexión. */
    estado: number;
    codigo?: string | undefined;
    titulo?: string | undefined;
    detalle?: string | undefined;
    /** El cuerpo `problem+json` (por si informa los intentos que quedan). */
    problema?: unknown;
}

export interface AvisoEntrar {
    tono: "error" | "warning" | "info";
    titulo: string;
    detalle?: string | undefined;
    /**
     * El error es de un campo: va en línea debajo de él (icono + texto + color) y no arriba. `null`:
     * va en el aviso de arriba de la tarjeta.
     */
    campo: "contrasena" | "codigo" | null;
}

/**
 * Intentos que quedan antes del bloqueo, solo si la API los dice (`intentosRestantes` o
 * `intentos_restantes` en el `problem+json`). Hoy la API no los informa: no se inventan.
 */
export function intentosRestantes(problema: unknown): number | null {
    if (typeof problema !== "object" || problema === null) {
        return null;
    }
    const p = problema as Record<string, unknown>;
    const n = p.intentosRestantes ?? p.intentos_restantes;
    return typeof n === "number" && Number.isInteger(n) && n >= 0 ? n : null;
}

const plural = (n: number, uno: string, varios: string) => `${n} ${n === 1 ? uno : varios}`;

/** Qué decir ante un error al entrar o al verificar el código. */
export function avisoEntrar(e: ErrorEntrada): AvisoEntrar {
    switch (e.codigo) {
        case "credenciales_invalidas": {
            const quedan = intentosRestantes(e.problema);
            return {
                tono: "error",
                titulo: quedan === null ? "Correo o contraseña incorrectos." : `Correo o contraseña incorrectos. Te ${quedan === 1 ? "queda" : "quedan"} ${plural(quedan, "intento", "intentos")}.`,
                campo: "contrasena",
            };
        }
        case "codigo_invalido":
            return { tono: "error", titulo: "Código incorrecto o ya usado. Revisa tu app de autenticación.", campo: "codigo" };
        case "mfa_bloqueado":
            return { tono: "error", titulo: "Demasiados códigos inválidos", detalle: "Por seguridad cerramos el intento. Vuelve a escribir tu contraseña.", campo: null };
        case "no_autenticado":
            return { tono: "warning", titulo: "El inicio de sesión venció", detalle: "Vuelve a escribir tu contraseña.", campo: null };
    }
    if (e.estado === 0 || e.codigo === "sin_conexion" || e.codigo === "api_no_disponible") {
        return { tono: "error", titulo: "No pudimos contactar a Iris", detalle: "Revisa tu conexión e inténtalo de nuevo.", campo: null };
    }
    return { tono: "error", titulo: e.titulo || "Algo salió mal", detalle: e.detalle ?? "Inténtalo de nuevo.", campo: null };
}

/**
 * «Usar huella» (solo la app nativa): desbloquea la sesión que ya está guardada en el teléfono,
 * nunca guarda la contraseña. Solo con biometría disponible, una sesión guardada (con su token en
 * el llavero) y el usuario recordado siendo el dueño de esa sesión.
 */
export function ofrecerHuella(o: { biometria: boolean; sesionGuardada: { correo: string } | null; tokenGuardado: boolean; recordado: UsuarioRecordado | null }): boolean {
    return o.biometria && o.tokenGuardado && o.sesionGuardada !== null && o.recordado !== null && o.sesionGuardada.correo.trim().toLowerCase() === o.recordado.correo;
}

/**
 * Aviso «Sin conexión» de la pantalla de entrada. Con una sesión guardada que todavía sirve sin señal
 * se entra con ella (no se debilita nada: la misma regla de `usableSinSenal`); sin ella, hay que
 * conectarse.
 */
export function avisoSinConexion(sesionUsable: boolean): { titulo: string; detalle: string } {
    return sesionUsable
        ? { titulo: "Sin conexión.", detalle: "Entrarás con la sesión guardada en este teléfono." }
        : { titulo: "Sin conexión.", detalle: "Conéctate para entrar: sin señal solo se abre una sesión verificada en las últimas horas." };
}
