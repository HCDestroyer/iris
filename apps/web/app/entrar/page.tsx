import { MarcaIris, Tarjeta } from "@iris/ui";
import type { Metadata } from "next";
import { Suspense } from "react";
import { SelectorTema } from "@/componentes/selector-tema";
import { FormularioEntrar } from "./formulario-entrar";

export const metadata: Metadata = { title: "Iniciar sesión" };

export default function PaginaEntrar() {
    return (
        <div className="flex min-h-dvh flex-col">
            <header className="flex items-center justify-between px-4 py-4 sm:px-6">
                <MarcaIris />
                <SelectorTema />
            </header>
            <main className="flex flex-1 items-start justify-center px-4 pt-6 pb-16 sm:items-center sm:pt-0">
                <Tarjeta className="w-full max-w-sm p-6 sm:p-7">
                    <Suspense>
                        <FormularioEntrar />
                    </Suspense>
                </Tarjeta>
            </main>
            <footer className="px-4 pb-6 text-center text-xs text-muted-foreground">
                Iris · familia Horus
            </footer>
        </div>
    );
}
