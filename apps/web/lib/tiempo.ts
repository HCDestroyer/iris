/** «N min» (o «N s» si falta menos de un minuto) para el tiempo de espera de un 429. */
export function formatearEspera(segundos: number): string {
    const s = Math.max(0, Math.ceil(segundos));
    if (s < 60) {
        return `${s} s`;
    }
    return `${Math.ceil(s / 60)} min`;
}

// Caracteres de control: el navegador quita \t y \n de las URL, y `/\t/otro.sitio` acaba en `//otro.sitio`.
// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u001f\u007f]/;

/**
 * Ruta interna segura para volver tras entrar: evita redirecciones abiertas (`//otro.sitio`,
 * `/\\otro.sitio`, caracteres de control) y exige que resuelva al mismo origen.
 */
export function rutaSegura(valor: string | null | undefined, porDefecto = "/", origen?: string): string {
    if (!valor || !valor.startsWith("/") || valor.startsWith("//") || valor.startsWith("/\\") || CONTROL.test(valor)) {
        return porDefecto;
    }
    const base = origen ?? (typeof window === "undefined" ? "http://localhost" : window.location.origin);
    try {
        if (new URL(valor, base).origin !== new URL(base).origin) {
            return porDefecto;
        }
    } catch {
        return porDefecto;
    }
    if (valor.startsWith("/entrar") || valor.startsWith("/api/")) {
        return porDefecto;
    }
    return valor;
}
