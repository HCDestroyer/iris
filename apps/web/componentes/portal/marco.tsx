import { MarcaIris } from "@iris/ui";
import Link from "next/link";
import type { ReactNode } from "react";
import { SelectorTema } from "@/componentes/selector-tema";

/**
 * Marco del portal de soporte: separado del panel (sin menú, sin selector de empresa), sobrio y
 * para el cliente final. `acciones` va a la derecha de la barra (nombre y «Salir»).
 */
export function MarcoPortal({ children, acciones, inicio = false }: { children: ReactNode; acciones?: ReactNode; inicio?: boolean }) {
    const marca = (
        <span className="inline-flex items-center gap-2">
            <MarcaIris />
            <span className="border-l pl-2 text-sm text-muted-foreground">Soporte</span>
        </span>
    );
    return (
        <div className="flex min-h-dvh flex-col" data-portal>
            <header className="glass-material sticky top-0 z-20 border-b">
                <div className="mx-auto flex h-14 max-w-3xl items-center gap-2 px-4">
                    {inicio ? (
                        <Link href="/soporte" className="rounded-md" aria-label="Soporte, mis casos">
                            {marca}
                        </Link>
                    ) : (
                        marca
                    )}
                    <div className="ml-auto flex min-w-0 items-center gap-2">
                        {acciones}
                        <SelectorTema className="hidden sm:inline-flex" />
                    </div>
                </div>
            </header>
            <main className="mx-auto w-full max-w-3xl flex-1 px-4 pt-6 pb-16">{children}</main>
            <footer className="flex flex-col items-center gap-3 px-4 pb-6 text-center text-xs text-muted-foreground">
                <SelectorTema className="sm:hidden" />
                <span>Atención al cliente · Iris</span>
            </footer>
        </div>
    );
}
