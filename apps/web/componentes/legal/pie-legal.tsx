import Link from "next/link";
import { CLAVES, RUTAS, TITULOS } from "@/lib/legal/paginas";

/** Enlaces a las páginas legales públicas, para el pie de las pantallas sin sesión (entrar, portal de soporte). */
export function PieLegal({ className }: { className?: string }) {
    return (
        <nav aria-label="Documentos legales" className={className} data-slot="pie-legal">
            <ul className="flex flex-wrap justify-center gap-x-4 gap-y-1">
                {CLAVES.map((c) => (
                    <li key={c}>
                        <Link href={RUTAS[c].es} className="rounded-sm underline decoration-current/30 underline-offset-4 hover:text-foreground hover:decoration-current">
                            {TITULOS[c].es}
                        </Link>
                    </li>
                ))}
            </ul>
        </nav>
    );
}
