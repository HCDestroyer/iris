import type { Metadata } from "next";
import { DetalleCaso } from "./detalle-caso";

export const metadata: Metadata = { title: "Caso" };

export default function PaginaCaso() {
    return <DetalleCaso />;
}
