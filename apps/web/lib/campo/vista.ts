// Vista de la app de campo en la URL (una sola página que funciona sin señal).
/** Pantalla visible: la lista del día, el detalle de una orden o lo pendiente por enviar. */
export type Vista = { tipo: "lista" } | { tipo: "orden"; id: number } | { tipo: "pendientes" };

export function vistaDeUrl(busqueda: string): Vista {
    const p = new URLSearchParams(busqueda);
    const orden = Number(p.get("orden"));
    if (Number.isSafeInteger(orden) && orden > 0) {
        return { tipo: "orden", id: orden };
    }
    return p.get("vista") === "pendientes" ? { tipo: "pendientes" } : { tipo: "lista" };
}

export function urlDeVista(v: Vista): string {
    return v.tipo === "orden" ? `/campo?orden=${v.id}` : v.tipo === "pendientes" ? "/campo?vista=pendientes" : "/campo";
}
