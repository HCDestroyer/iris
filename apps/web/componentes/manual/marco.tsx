import { LogoIris } from "@iris/ui";
import type { ReactNode } from "react";
import { SelectorTema } from "@/componentes/selector-tema";
import { BotonAtras } from "./atras";

/**
 * Marco del manual: a pantalla completa, sin el menú del panel, con «Atrás» siempre a mano. Lo usan
 * /manual (panel) y /campo/manual (app del técnico, sin señal).
 */
export function MarcoManual({ volverA, children }: { volverA: string; children: ReactNode }) {
    return (
        <div className="relative min-h-dvh">
            <header className="sticky top-0 z-20 border-b border-border bg-background">
                <div className="mx-auto flex max-w-3xl items-center gap-2 px-4 py-2 pt-[max(0.5rem,env(safe-area-inset-top))]">
                    <BotonAtras volverA={volverA} />
                    <span className="mx-auto flex min-w-0 items-center gap-2 font-semibold">
                        <LogoIris className="size-6 shrink-0" />
                        <span className="truncate">Manual de Iris</span>
                    </span>
                    <SelectorTema />
                </div>
            </header>
            <main className="mx-auto grid max-w-3xl gap-4 px-4 pt-4 pb-[max(2rem,env(safe-area-inset-bottom))] [&>*]:min-w-0">{children}</main>
        </div>
    );
}
