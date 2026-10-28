import type { Metadata } from "next";
import { Inicio } from "./inicio";

export const metadata: Metadata = { title: "Inicio" };

export default function PaginaInicio() {
    return <Inicio />;
}
