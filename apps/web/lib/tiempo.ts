/** «N min» (o «N s» si falta menos de un minuto) para el tiempo de espera de un 429. */
export function formatearEspera(segundos: number): string {
    const s = Math.max(0, Math.ceil(segundos));
    if (s < 60) {
        return `${s} s`;
    }
    return `${Math.ceil(s / 60)} min`;
}

/** Ruta interna segura para volver tras entrar: evita redirecciones abiertas (`//otro.sitio`). */
export function rutaSegura(valor: string | null | undefined, porDefecto = "/"): string {
    if (!valor || !valor.startsWith("/") || valor.startsWith("//") || valor.startsWith("/\\")) {
        return porDefecto;
    }
    if (valor.startsWith("/entrar") || valor.startsWith("/api/")) {
        return porDefecto;
    }
    return valor;
}
