"use client";

import { Boton } from "@iris/ui";
import { Check, Copy } from "lucide-react";
import { useEffect, useState } from "react";

type Estado = "listo" | "copiado" | "fallo";

/**
 * Copia `texto` al portapapeles y lo confirma («Copiada»). Si el navegador no deja (sin HTTPS,
 * permiso negado), lo dice para que se copie a mano: nunca da por copiado lo que no se copió.
 */
export function BotonCopiar({ texto, etiqueta = "Copiar", copiado = "Copiado", disabled }: { texto: string; etiqueta?: string; copiado?: string; disabled?: boolean }) {
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
                {estado === "fallo" ? "No se pudo copiar: selecciónala y cópiala a mano." : ""}
            </span>
        </span>
    );
}
