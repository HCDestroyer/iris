import type { ReactNode } from "react";
import { MarcoManual } from "@/componentes/manual/marco";

/**
 * Manual de Iris (/manual): lo ve cualquier usuario con sesión (middleware). Va fuera del marco
 * del panel, a pantalla completa con «Atrás», para que el botón «?» de cualquier pantalla (panel,
 * portal o app de campo) abra lo mismo. Las páginas de la app del técnico viven en /campo/manual.
 */
export default function LayoutManual({ children }: { children: ReactNode }) {
    return <MarcoManual volverA="/">{children}</MarcoManual>;
}
