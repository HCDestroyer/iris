import { ArrowLeft } from "lucide-react";
import Link from "next/link";

export function VolverACasos() {
    return (
        <Link href="/soporte" className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft aria-hidden className="size-4" />
            Mis casos
        </Link>
    );
}
