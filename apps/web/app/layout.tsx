import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { ProveedorTema } from "@/componentes/proveedor-tema";
import "./globals.css";

export const metadata: Metadata = {
    title: { default: "Iris", template: "%s · Iris" },
    description: "Operación de campo y atención del ISP · familia Horus",
    applicationName: "Iris",
};

export const viewport: Viewport = {
    themeColor: [
        { media: "(prefers-color-scheme: light)", color: "#ffffff" },
        { media: "(prefers-color-scheme: dark)", color: "#0b1014" },
    ],
};

export default function LayoutRaiz({ children }: { children: ReactNode }) {
    return (
        <html lang="es" suppressHydrationWarning>
            <body className="bg-background font-sans text-foreground antialiased">
                <ProveedorTema>
                    <div className="app-aura" aria-hidden="true" />
                    {children}
                </ProveedorTema>
            </body>
        </html>
    );
}
