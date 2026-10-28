import type { Schema } from "@iris/contratos";

/*
 * Enlaces «Ver en Argos» / «Ver en Horus». El origen sale solo de `GET /integraciones/enlaces`
 * (`urlPublica` de cada conector activo). i1.md deja «para después» las rutas dentro de cada
 * sistema y el autologin: mientras no estén documentadas, el enlace abre la base del sistema
 * (pendiente en .claude/integration/i3.md).
 */

export type EnlacesExternos = Schema<"EnlacesExternos">;
export type Sistema = "argosisp" | "horus";

export type EnlaceVer = { sistema: Sistema; etiqueta: string; titulo: string; href: string };

/** Base http(s) sin barra final, o `null` si no es una URL http(s) válida (nunca `javascript:` ni otra). */
export function baseSegura(url: string | undefined): string | null {
    if (!url) {
        return null;
    }
    let u: URL;
    try {
        u = new URL(url);
    } catch {
        return null;
    }
    if ((u.protocol !== "https:" && u.protocol !== "http:") || u.username || u.password) {
        return null;
    }
    return `${u.origin}${u.pathname}`.replace(/\/+$/, "");
}

/** «Ver en Argos» del cliente del ticket: solo con cliente y con conector de Argos activo. */
export function enlaceArgosCliente(enlaces: EnlacesExternos | null | undefined, clienteId: number | null): EnlaceVer | null {
    const base = baseSegura(enlaces?.argosisp?.urlPublica);
    if (clienteId === null || !base) {
        return null;
    }
    return {
        sistema: "argosisp",
        etiqueta: "Ver en Argos",
        // Ruta de la ficha en Argos sin documentar (i1.md): abre ArgosISP.
        titulo: `Abre ArgosISP en otra pestaña (busca el cliente ${clienteId})`,
        href: base,
    };
}

/** «Ver en Horus» en tickets que abrió Horus (hallazgo o router): solo con conector de Horus activo. */
export function enlaceHorusTicket(enlaces: EnlacesExternos | null | undefined, origen: Schema<"Ticket">["origen"]): EnlaceVer | null {
    const base = baseSegura(enlaces?.horus?.urlPublica);
    if (origen !== "horus" || !base) {
        return null;
    }
    return {
        sistema: "horus",
        etiqueta: "Ver en Horus",
        // El ticket aún no trae el id del hallazgo ni del router, y la ruta en Horus no está documentada.
        titulo: "Abre Horus Flow en otra pestaña (hallazgo o router que abrió el ticket)",
        href: base,
    };
}
