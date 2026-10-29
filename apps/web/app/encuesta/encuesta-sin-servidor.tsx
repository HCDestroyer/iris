"use client";

import { Cargando, Tarjeta } from "@iris/ui";
import dynamic from "next/dynamic";

/** La encuesta depende del fragmento de la URL, que solo existe en el navegador: no se pinta en el servidor. */
export const EncuestaSinServidor = dynamic(() => import("./encuesta-publica").then((m) => m.EncuestaPublica), {
    ssr: false,
    loading: () => (
        <Tarjeta className="grid place-items-center py-16">
            <Cargando texto="Abriendo la encuesta…" visible />
        </Tarjeta>
    ),
});
