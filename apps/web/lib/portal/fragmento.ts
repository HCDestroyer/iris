/** Largo que admite la API para el ticket de acceso (`EntradaEntrarPortal.ticket`). */
export const TICKET_MIN = 16;
export const TICKET_MAX = 200;

/**
 * Ticket de acceso desde el fragmento (`/soporte/entrar#<ticket>`). El fragmento nunca viaja al
 * servidor ni en el Referer; tampoco se lee de la query. Devuelve `null` si no hay o no tiene la
 * forma que admite la API (se trata como enlace inválido).
 */
export function ticketDesdeFragmento(hash: string): { ticket: string } | { invalido: true } | null {
    let valor = hash.startsWith("#") ? hash.slice(1) : hash;
    try {
        valor = decodeURIComponent(valor);
    } catch {
        return { invalido: true };
    }
    valor = valor.trim();
    if (valor === "") {
        return null;
    }
    if (valor.length < TICKET_MIN || valor.length > TICKET_MAX || /\s/.test(valor)) {
        return { invalido: true };
    }
    return { ticket: valor };
}
