"use client";

import { Boton, cn, Hoja, HojaContenido } from "@iris/ui";
import { Ellipsis } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";
import { entradaActiva, type EntradaMenu } from "@/lib/menu";

type Props = {
    /** Inicio, Tickets, Órdenes y Agenda, según los permisos. */
    entradas: readonly EntradaMenu[];
    masAbierto: boolean;
    alCambiarMas: (abierto: boolean) => void;
    /** Contenido de «Más»: el resto del menú, la empresa, la cuenta y el tema. */
    children: ReactNode;
};

/**
 * Barra inferior del teléfono (debajo de `lg`): lo principal a un toque y «Más» para el resto, en
 * una hoja. Respeta el área segura de abajo (iPhone con barra de inicio).
 */
export function BarraInferior({ entradas, masAbierto, alCambiarMas, children }: Props) {
    const ruta = usePathname();
    const activa = entradaActiva(ruta);
    const enMas = !entradas.some((e) => e.href === activa?.href);
    const clases = (actual: boolean) =>
        cn(
            "flex min-h-14 flex-1 flex-col items-center justify-center gap-0.5 rounded-md px-1 text-[0.6875rem] font-medium transition-colors",
            actual ? "text-primary" : "text-muted-foreground hover:text-foreground",
        );
    return (
        <nav
            aria-label="Accesos rápidos"
            className="glass-material fixed inset-x-0 bottom-0 z-30 border-t px-2 pb-[env(safe-area-inset-bottom)] lg:hidden"
        >
            <ul className="mx-auto flex max-w-xl items-stretch">
                {entradas.map((e) => {
                    const Icono = e.icono;
                    const actual = activa?.href === e.href;
                    return (
                        <li key={e.href} className="flex flex-1">
                            <Link href={e.href} aria-current={actual ? "page" : undefined} className={clases(actual)}>
                                <Icono aria-hidden className="size-5" />
                                <span className="max-w-full truncate">{e.etiqueta}</span>
                            </Link>
                        </li>
                    );
                })}
                <li className="flex flex-1">
                    <Hoja open={masAbierto} onOpenChange={alCambiarMas}>
                        <Boton
                            variante="fantasma"
                            className={cn(clases(enMas && activa !== undefined), "h-auto w-full")}
                            aria-haspopup="dialog"
                            aria-expanded={masAbierto}
                            onClick={() => alCambiarMas(true)}
                        >
                            <Ellipsis aria-hidden className="size-5" />
                            Más
                        </Boton>
                        <HojaContenido titulo="Más" lado="derecha" etiquetaCerrar="Cerrar «Más»">
                            {children}
                        </HojaContenido>
                    </Hoja>
                </li>
            </ul>
        </nav>
    );
}
