// El valor después de que deja de cambiar `ms` (búsqueda en vivo: filtra al dejar de escribir).
// Vacío pasa en el acto: borrar la búsqueda limpia los resultados sin esperar.
import { useEffect, useState } from "react";

export function useDemorado<T>(valor: T, ms: number): T {
    const [demorado, setDemorado] = useState(valor);
    useEffect(() => {
        const t = setTimeout(() => setDemorado(valor), valor === "" ? 0 : ms);
        return () => clearTimeout(t);
    }, [valor, ms]);
    return demorado;
}
