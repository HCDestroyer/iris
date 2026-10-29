/**
 * Quita el fragmento (`#…`, donde viajan el token de la encuesta y el ticket del portal) de la URL
 * sin recargar ni dejarlo en el historial. Con `null` como estado, el router de Next sincroniza su
 * URL canónica (copia su estado interno y aplica la URL nueva); si se le pasa `history.state` (que
 * lleva `__NA`), Next cree que la llamada es suya, no se entera y un `router.refresh()` posterior
 * vuelve a escribir el fragmento.
 */
export function quitarFragmentoDeLaUrl(): void {
    if (typeof window === "undefined" || !window.location.hash) return;
    window.history.replaceState(null, "", `${window.location.pathname}${window.location.search}`);
}
