"use client";

import { Cargando } from "@iris/ui";
import dynamic from "next/dynamic";

/** El canje depende del fragmento de la URL, que solo existe en el navegador: no se pinta en el servidor. */
export const EntrarSSOSinServidor = dynamic(() => import("./entrar-sso").then((m) => m.EntrarSSO), {
    ssr: false,
    loading: () => (
        <div className="grid place-items-center py-10">
            <Cargando texto="Entrando desde Argos…" visible />
        </div>
    ),
});
