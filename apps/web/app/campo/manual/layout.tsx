import type { ReactNode } from "react";
import { MarcoManual } from "@/componentes/manual/marco";

/**
 * Manual de la app del técnico (/campo/manual): las mismas páginas de la sección «App del técnico»
 * del manual, a pantalla completa con «Atrás». Va dentro de /campo para que el service worker las
 * guarde (next.config.ts las precachea) y se lean sin señal. No hay datos de clientes: es el texto
 * estático del manual.
 */
export default function LayoutManualCampo({ children }: { children: ReactNode }) {
    return <MarcoManual volverA="/campo">{children}</MarcoManual>;
}
