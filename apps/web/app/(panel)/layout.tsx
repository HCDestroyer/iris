import type { ReactNode } from "react";
import { Panel } from "@/componentes/panel";

export default function LayoutPanel({ children }: { children: ReactNode }) {
    return <Panel>{children}</Panel>;
}
