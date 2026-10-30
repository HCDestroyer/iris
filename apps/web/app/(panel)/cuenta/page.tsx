import type { Metadata } from "next";
import { MiPerfil } from "./mi-perfil";

export const metadata: Metadata = { title: "Mi perfil" };

export default function PaginaMiPerfil() {
    return <MiPerfil />;
}
