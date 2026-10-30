import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { VueltaArgosSinServidor } from "./sin-servidor";

// El código, el api y el state viajan en el fragmento (#): ni el servidor ni el Referer los ven; la
// página no se indexa (como /sso/entrar).
export const metadata: Metadata = { title: "Conectar con Argos", referrer: "no-referrer", robots: { index: false, follow: false } };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["integraciones.conectar"]}>
            <VueltaArgosSinServidor />
        </RequierePermiso>
    );
}
