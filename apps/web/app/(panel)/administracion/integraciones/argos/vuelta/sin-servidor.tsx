"use client";

import { Cargando } from "@iris/ui";
import dynamic from "next/dynamic";

/** La vuelta depende del fragmento de la URL, que solo existe en el navegador: no se pinta en el servidor. */
export const VueltaArgosSinServidor = dynamic(() => import("./vuelta").then((m) => m.VueltaArgos), {
    ssr: false,
    loading: () => (
        <div className="grid place-items-center py-10">
            <Cargando texto="Completando la conexión con Argos…" visible />
        </div>
    ),
});
