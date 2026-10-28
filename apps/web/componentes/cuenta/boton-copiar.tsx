"use client";

import { Boton } from "@iris/ui";
import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";
import { borradorPortapapeles } from "@/lib/cuenta/portapapeles";

type Estado = "listo" | "copiado" | "fallo";

type Props = {
    texto: string;
    etiqueta?: string;
    copiado?: string;
    disabled?: boolean;
    /** Dato secreto: se borra del portapapeles a los 45 s y al cerrar la pantalla (si sigue ahí). */
    sensible?: boolean;
    /** Borrador de un componente de más arriba (con `sensible`); si no, uno propio. */
    borrador?: ReturnType<typeof borradorPortapapeles> | null;
};

const portapapeles = () => (typeof navigator === "undefined" ? undefined : navigator.clipboard);

/**
 * Copia `texto` al portapapeles y lo confirma («Copiada»). Si el navegador no deja (sin HTTPS,
 * permiso negado), lo dice para que se copie a mano: nunca da por copiado lo que no se copió.
 */
/**
 * Borrador del portapapeles para datos sensibles, vivo mientras viva el componente que lo usa:
 * al desmontarse borra lo pendiente. Úsalo arriba del paso que se cierra (p. ej. el diálogo
 * entero) para que pasar al resultado no borre lo que se acaba de copiar.
 */
export function useBorradorPortapapeles(activo = true): ReturnType<typeof borradorPortapapeles> | null {
    const [borrador] = useState(() => borradorPortapapeles(portapapeles));
    useEffect(() => {
        if (!activo) {
            return undefined;
        }
        return () => {
            void borrador.cerrar();
        };
    }, [activo, borrador]);
    return activo ? borrador : null;
}

export function BotonCopiar({ texto, etiqueta = "Copiar", copiado = "Copiado", disabled, sensible = false, borrador: externo }: Props) {
    const propio = useBorradorPortapapeles(sensible && !externo);
    const borrador = externo ?? propio;
    // El estado es de un texto: con otro (p. ej. otra contraseña generada) lo copiado ya no vale.
    const [resultado, setResultado] = useState<{ estado: Estado; de: string }>({ estado: "listo", de: texto });
    const estado = resultado.de === texto ? resultado.estado : "listo";

    useEffect(() => {
        if (resultado.estado !== "copiado") {
            return undefined;
        }
        const id = window.setTimeout(() => setResultado((r) => ({ ...r, estado: "listo" })), 2500);
        return () => window.clearTimeout(id);
    }, [resultado]);

    async function copiar() {
        try {
            if (!navigator.clipboard) {
                throw new Error("sin portapapeles");
            }
            await navigator.clipboard.writeText(texto);
            if (sensible) {
                borrador?.copiado(texto);
            }
            setResultado({ estado: "copiado", de: texto });
        } catch {
            setResultado({ estado: "fallo", de: texto });
        }
    }

    return (
        <span className="inline-grid gap-1">
            <Boton type="button" variante="secundario" tamano="md" onClick={copiar} disabled={disabled || texto === ""}>
                {estado === "copiado" ? <Check /> : <Copy />}
                {estado === "copiado" ? copiado : etiqueta}
            </Boton>
            <span role="status" className="text-xs text-muted-foreground empty:hidden">
                {estado === "fallo" ? "No se pudo copiar: selecciónala y cópiala a mano." : sensible ? "Se borra del portapapeles en 45 s." : ""}
            </span>
        </span>
    );
}
