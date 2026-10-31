"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback } from "react";

/** La pestaña de `?pestana=` si es una de las válidas; si no (o no viene), la inicial. */
export function pestanaDeParametro<I extends string>(valor: string | null | undefined, validas: readonly I[], inicial: I): I {
    return validas.find((v) => v === valor) ?? inicial;
}

/** URL con `?pestana=` (la inicial no se escribe: la ficha limpia abre en ella). */
export function urlConPestana(ruta: string, actuales: URLSearchParams, pestana: string, inicial: string): string {
    const q = new URLSearchParams(actuales);
    if (pestana === inicial) q.delete("pestana");
    else q.set("pestana", pestana);
    const texto = q.toString();
    return texto ? `${ruta}?${texto}` : ruta;
}

/**
 * Pestaña elegida de una ficha, en la URL (`?pestana=`, validada contra una lista fija): un enlace
 * o recargar abren la misma. Cambiar de pestaña reemplaza la entrada del historial (no la apila).
 */
export function usePestanaUrl<I extends string>(validas: readonly I[], inicial: I): [I, (pestana: I) => void] {
    const params = useSearchParams();
    const ruta = usePathname();
    const router = useRouter();
    const actual = pestanaDeParametro(params.get("pestana"), validas, inicial);
    const cambiar = useCallback(
        (pestana: I) => router.replace(urlConPestana(ruta, new URLSearchParams(params.toString()), pestana, inicial), { scroll: false }),
        [router, ruta, params, inicial],
    );
    return [actual, cambiar];
}
