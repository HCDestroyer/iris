"use client";

import { cn } from "@iris/ui";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { entradaActiva, type EntradaMenu } from "@/lib/menu";

/** Menú lateral: solo las entradas que el usuario puede ver (filtradas por `GET /yo`). */
export function Navegacion({ entradas, alNavegar }: { entradas: readonly EntradaMenu[]; alNavegar?: () => void }) {
    const ruta = usePathname();
    const activa = entradaActiva(ruta);
    return (
        <nav aria-label="Principal">
            <ul className="grid gap-0.5">
                {entradas.map((e) => {
                    const actual = activa?.href === e.href;
                    const Icono = e.icono;
                    return (
                        <li key={e.href}>
                            <Link
                                href={e.href}
                                onClick={alNavegar}
                                aria-current={actual ? "page" : undefined}
                                className={cn(
                                    "flex h-9 items-center gap-2.5 rounded-md px-2.5 text-sm font-medium transition-colors duration-150",
                                    actual
                                        ? "bg-primary/12 text-primary"
                                        : "text-muted-foreground hover:bg-elevated hover:text-foreground",
                                )}
                            >
                                <Icono aria-hidden className="size-4 shrink-0" />
                                <span className="flex-1 truncate">{e.etiqueta}</span>
                                {e.proximamente ? (
                                    <span className="text-[0.6875rem] font-normal text-dimmed">Pronto</span>
                                ) : null}
                            </Link>
                        </li>
                    );
                })}
            </ul>
        </nav>
    );
}
