import { MarcaIris, Tarjeta } from "@iris/ui";
import type { Metadata } from "next";
import { SelectorTema } from "@/componentes/selector-tema";
import { EntrarSSOSinServidor } from "./sin-servidor";

// El ticket viaja en el fragmento (#): ni el servidor ni el Referer lo ven; la página no se indexa.
export const metadata: Metadata = { title: "Entrar desde Argos", referrer: "no-referrer", robots: { index: false, follow: false } };

export default function PaginaEntrarSSO() {
    return (
        <div className="flex min-h-dvh flex-col">
            <header className="flex items-center justify-between px-4 py-4 sm:px-6">
                <MarcaIris />
                <SelectorTema />
            </header>
            <main className="flex flex-1 items-start justify-center px-4 pt-6 pb-16 sm:items-center sm:pt-0">
                <Tarjeta className="w-full max-w-md p-6 sm:p-7">
                    <EntrarSSOSinServidor />
                </Tarjeta>
            </main>
            <footer className="px-4 pb-6 text-center text-xs text-muted-foreground">Iris · familia Horus</footer>
        </div>
    );
}
