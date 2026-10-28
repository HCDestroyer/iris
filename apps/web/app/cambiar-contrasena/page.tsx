import { MarcaIris, Tarjeta } from "@iris/ui";
import type { Metadata } from "next";
import { Suspense } from "react";
import { SelectorTema } from "@/componentes/selector-tema";
import { CambioContrasena } from "./cambio-contrasena";

export const metadata: Metadata = { title: "Cambiar contraseña" };

/**
 * Fuera del layout del panel: con una contraseña temporal la API responde 403 a todo salvo
 * `GET /yo`, cambiarla y salir, así que aquí no se lee nada más (ni menú ni empresas).
 */
export default function PaginaCambiarContrasena() {
    return (
        <div className="flex min-h-dvh flex-col">
            <header className="flex items-center justify-between px-4 py-4 sm:px-6">
                <MarcaIris />
                <SelectorTema />
            </header>
            <main className="flex flex-1 items-start justify-center px-4 pt-6 pb-16 sm:items-center sm:pt-0">
                <Tarjeta className="w-full max-w-md p-6 sm:p-7">
                    <Suspense>
                        <CambioContrasena />
                    </Suspense>
                </Tarjeta>
            </main>
            <footer className="px-4 pb-6 text-center text-xs text-muted-foreground">Iris · familia Horus</footer>
        </div>
    );
}
