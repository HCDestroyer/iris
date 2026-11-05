import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { RegistrarSw } from "./_componentes/registrar-sw";
import "./campo.css";

export const metadata: Metadata = {
    title: { absolute: "Iris Campo" },
    description: "Órdenes de trabajo del técnico, con y sin señal · familia Horus",
    applicationName: "Iris Campo",
    manifest: "/campo/manifest.webmanifest",
    appleWebApp: { capable: true, title: "Iris Campo", statusBarStyle: "default" },
    formatDetection: { telephone: false },
};

export const viewport: Viewport = {
    width: "device-width",
    initialScale: 1,
    viewportFit: "cover",
    // Con el teclado abierto la ventana se encoge (Chrome): lo fijo abajo (hojas, isla) queda encima.
    interactiveWidget: "resizes-content",
    themeColor: [
        { media: "(prefers-color-scheme: light)", color: "#ffffff" },
        { media: "(prefers-color-scheme: dark)", color: "#0b1014" },
    ],
};

/** App de campo (PWA): sin el marco del panel; el service worker la abre sin señal. */
export default function LayoutCampo({ children }: { children: ReactNode }) {
    return (
        <>
            <RegistrarSw />
            {children}
        </>
    );
}
