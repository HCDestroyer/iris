import type { Metadata } from "next";
import type { ReactNode } from "react";
import { PortalCliente } from "@/componentes/portal/portal-cliente";

export const metadata: Metadata = { title: { default: "Mis casos de soporte", template: "%s · Soporte" } };

export default function LayoutPortal({ children }: { children: ReactNode }) {
    return <PortalCliente>{children}</PortalCliente>;
}
