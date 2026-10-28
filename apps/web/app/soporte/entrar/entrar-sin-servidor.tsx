"use client";

import { Cargando, Tarjeta } from "@iris/ui";
import dynamic from "next/dynamic";

/** El canje depende del fragmento de la URL, que solo existe en el navegador: no se pinta en el servidor. */
export const EntrarSinServidor = dynamic(() => import("./entrar-portal").then((m) => m.EntrarPortal), {
    ssr: false,
    loading: () => (
        <Tarjeta className="grid place-items-center py-16">
            <Cargando texto="Entrando a tu soporte…" visible />
        </Tarjeta>
    ),
});
