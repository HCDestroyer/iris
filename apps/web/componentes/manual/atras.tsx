"use client";

import { Boton } from "@iris/ui";
import { ArrowLeft } from "lucide-react";
import { useRouter } from "next/navigation";

/**
 * «Atrás» del manual: vuelve a la pantalla desde donde se abrió (el botón «?»). Si el manual se
 * abrió directo (sin historial), va a `volverA`: el inicio del panel o la app de campo.
 */
export function BotonAtras({ volverA }: { volverA: string }) {
    const router = useRouter();
    return (
        <Boton
            variante="fantasma"
            tamano="lg"
            className="gap-1.5 px-3"
            onClick={() => {
                if (window.history.length > 1) router.back();
                else router.push(volverA);
            }}
        >
            <ArrowLeft aria-hidden className="size-5" />
            Atrás
        </Boton>
    );
}
