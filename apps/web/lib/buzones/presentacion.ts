import type { Schema } from "@iris/contratos";

export type PruebaServidor = Schema<"PruebaServidor">;
type CodigoPrueba = NonNullable<PruebaServidor["codigo"]>;

/** Qué falló al probar la conexión, en palabras del operador (sin datos del servidor ni contraseñas). */
export const CODIGOS_PRUEBA: Record<CodigoPrueba, string> = {
    destino_no_permitido: "El servidor está en una red interna no permitida.",
    dns: "No se encontró el servidor: revisa el nombre.",
    conexion: "No se pudo conectar: revisa el servidor, el puerto y que acepte conexiones.",
    tls: "Falló el TLS: el certificado no es válido para ese nombre o la negociación no terminó.",
    sin_tls: "El servidor no ofrece STARTTLS: Iris no envía credenciales en claro.",
    autenticacion: "El servidor rechazó el usuario o la contraseña.",
    carpeta: "No existe la carpeta indicada.",
    protocolo: "El servidor respondió algo inesperado para el protocolo.",
    cifrado: "No se pudo leer la contraseña guardada: vuelve a escribirla.",
};

/** Texto del resultado de un servidor. `detalle` ya viene resumido por la API. */
export function textoPrueba(p: PruebaServidor): string {
    if (p.ok) {
        return "Conexión correcta.";
    }
    const base = p.codigo ? CODIGOS_PRUEBA[p.codigo] : "No se pudo completar la prueba.";
    return p.detalle ? `${base} (${p.detalle})` : base;
}

export const NOMBRES_TLS: Record<Schema<"ServidorIMAP">["tls"], string> = {
    tls: "TLS",
    starttls: "STARTTLS",
};
