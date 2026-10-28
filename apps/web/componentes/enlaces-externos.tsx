"use client";

import { Boton } from "@iris/ui";
import { ExternalLink } from "lucide-react";
import { api, resolver } from "@/lib/api/cliente";
import type { EnlacesExternos, EnlaceVer } from "@/lib/enlaces";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/**
 * Orígenes de Argos y Horus (`GET /integraciones/enlaces`, permiso `clientes.ver`). Sin el permiso
 * no se pide; si la lectura falla, `null`: los botones «Ver en …» simplemente no aparecen.
 */
export function useEnlacesExternos(): EnlacesExternos | null {
    const { puede } = useSesion();
    const permitido = puede(["clientes.ver"]);
    const lectura = useLectura<EnlacesExternos | null>(async (signal) => {
        if (!permitido) {
            return null;
        }
        try {
            return await resolver(api().GET("/api/v1/integraciones/enlaces", { signal }));
        } catch {
            return null;
        }
    });
    return lectura.estado === "listo" ? lectura.datos : null;
}

/** Botón que abre el otro sistema en una pestaña nueva, sin `opener` ni `Referer`. */
export function BotonVerEn({ enlace }: { enlace: EnlaceVer }) {
    return (
        <Boton asChild variante="secundario" tamano="sm">
            <a href={enlace.href} target="_blank" rel="noopener noreferrer" title={enlace.titulo} data-ver-en={enlace.sistema}>
                <ExternalLink />
                {enlace.etiqueta}
                <span className="sr-only"> (se abre en otra pestaña)</span>
            </a>
        </Boton>
    );
}
