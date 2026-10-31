// Guarda contra el doble toque (como `cierre.tsx` de la PWA): mientras una acción espera el GPS o
// se guarda, un segundo toque no la repite. Si se guardó, queda cerrada (la pantalla cambia); si
// falló, se puede volver a intentar.
import { useCallback, useRef, useState } from "react";

export interface AccionUnica {
    ocupado: boolean;
    /** Corre `accion` si no hay otra en curso; devuelve si se guardó (`false` también si se ignoró). */
    correr: (accion: () => Promise<boolean>) => Promise<boolean>;
}

export function useAccionUnica(): AccionUnica {
    const enCurso = useRef(false);
    const [ocupado, setOcupado] = useState(false);
    const correr = useCallback(async (accion: () => Promise<boolean>) => {
        if (enCurso.current) {
            return false;
        }
        enCurso.current = true;
        setOcupado(true);
        let ok = false;
        try {
            ok = await accion();
        } finally {
            if (!ok) {
                enCurso.current = false;
                setOcupado(false);
            }
        }
        return ok;
    }, []);
    return { ocupado, correr };
}
