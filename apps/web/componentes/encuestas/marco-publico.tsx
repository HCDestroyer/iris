import { MarcaIris } from "@iris/ui";
import type { ReactNode } from "react";
import { SelectorTema } from "@/componentes/selector-tema";

/** Marco de la encuesta pública: sin menú ni sesión, sobrio y para el cliente final. */
export function MarcoEncuesta({ children }: { children: ReactNode }) {
    return (
        <div className="flex min-h-dvh flex-col" data-encuesta-publica>
            <header className="glass-material sticky top-0 z-20 border-b">
                <div className="mx-auto flex h-14 max-w-2xl items-center gap-2 px-4">
                    <span className="inline-flex items-center gap-2">
                        <MarcaIris />
                        <span className="border-l pl-2 text-sm text-muted-foreground">Encuesta</span>
                    </span>
                    <SelectorTema className="ml-auto hidden sm:inline-flex" />
                </div>
            </header>
            <main className="mx-auto w-full max-w-2xl flex-1 px-4 pt-6 pb-16">{children}</main>
            <footer className="flex flex-col items-center gap-3 px-4 pb-6 text-center text-xs text-muted-foreground">
                <SelectorTema className="sm:hidden" />
                <span>Tu opinión nos ayuda a mejorar · Iris</span>
            </footer>
        </div>
    );
}
