import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";
import { RegistrarSw } from "./_componentes/registrar-sw";

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
