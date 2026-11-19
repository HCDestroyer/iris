import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaTokens } from "./lista-tokens";

export const metadata: Metadata = { title: "Tokens de API" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["tokens.ver"]}>
            <ListaTokens />
        </RequierePermiso>
    );
}
