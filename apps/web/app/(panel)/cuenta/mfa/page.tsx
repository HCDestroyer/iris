import type { Metadata } from "next";
import { Suspense } from "react";
import { ConfigurarMfa } from "./configurar-mfa";

export const metadata: Metadata = { title: "Verificación en dos pasos" };

export default function PaginaMfa() {
    return (
        <Suspense>
            <ConfigurarMfa />
        </Suspense>
    );
}
