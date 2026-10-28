"use client";

import { ThemeProvider } from "next-themes";
import type { ReactNode } from "react";

/** Tema claro/oscuro/sistema con la clase `.dark` en <html> (la que usan los tokens Horus). */
export function ProveedorTema({ children }: { children: ReactNode }) {
    return (
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange storageKey="iris-tema">
            {children}
        </ThemeProvider>
    );
}
