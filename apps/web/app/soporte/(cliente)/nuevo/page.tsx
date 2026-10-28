import type { Metadata } from "next";
import { NuevoCaso } from "./nuevo-caso";

export const metadata: Metadata = { title: "Reportar un problema" };

export default function PaginaNuevoCaso() {
    return <NuevoCaso />;
}
