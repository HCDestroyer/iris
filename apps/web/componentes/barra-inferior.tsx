"use client";

import { Boton, cn, Hoja, HojaContenido, IslaInferior, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento } from "@iris/ui";
import { Ellipsis, Plus } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { entradaActiva, type EntradaMenu } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";
import { CREABLES } from "./barra-superior";

type Props = {
    /** Inicio, Tickets, Órdenes y Agenda, según los permisos. */
    entradas: readonly EntradaMenu[];
    /** Número junto a una entrada (por `href`), solo con datos leídos: en la isla es un punto. */
    contadores?: Readonly<Record<string, number | undefined>>;
    masAbierto: boolean;
    alCambiarMas: (abierto: boolean) => void;
    /** Contenido de «Más»: el resto del menú, la empresa, la cuenta y el tema. */
    children: ReactNode;
};

/**
 * Isla del menú en el teléfono (debajo de `lg`, rediseño): píldora flotante abajo con lo principal
 * a un toque y «Más» para el resto (en una hoja), y a su derecha el botón redondo «Crear» con lo
 * que la persona puede crear. Respeta el área segura de abajo.
 */
export function BarraInferior({ entradas, contadores, masAbierto, alCambiarMas, children }: Props) {
    const ruta = usePathname();
    const router = useRouter();
    const { puede } = useSesion();
    const activa = entradaActiva(ruta);
    const enMas = !entradas.some((e) => e.href === activa?.href);
    const creables = CREABLES.filter((c) => puede([c.permiso]));
    return (
        <IslaInferior
            entradas={entradas.map((e) => ({
                href: e.href,
                etiqueta: e.etiqueta,
                ...(e.etiquetaCorta ? { etiquetaCorta: e.etiquetaCorta } : {}),
                icono: e.icono,
                contador: contadores?.[e.href],
                actual: activa?.href === e.href,
            }))}
            mas={
                <Hoja open={masAbierto} onOpenChange={alCambiarMas}>
                    <Boton
                        variante="fantasma"
                        className={cn(
                            "flex h-auto min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-full px-1 py-1.5 text-[0.65625rem] font-semibold [&_svg]:size-5",
                            enMas && activa !== undefined ? "bg-primario-suave text-primario-texto" : "text-[var(--iris-texto-apagado)]",
                        )}
                        aria-haspopup="dialog"
                        aria-expanded={masAbierto}
                        onClick={() => alCambiarMas(true)}
                    >
                        <Ellipsis aria-hidden />
                        Más
                    </Boton>
                    <HojaContenido titulo="Más" lado="derecha" etiquetaCerrar="Cerrar «Más»">
                        {children}
                    </HojaContenido>
                </Hoja>
            }
            accion={
                creables.length > 0 ? (
                    <MenuDesplegable>
                        <MenuDesplegableDisparador>
                            <Boton aria-label="Crear" title="Crear" className="size-14 min-w-14 p-0 shadow-[var(--iris-sombra-flotante)] md:size-14 [&_svg]:size-6">
                                <Plus />
                            </Boton>
                        </MenuDesplegableDisparador>
                        <MenuDesplegableContenido align="end">
                            {creables.map((c) => (
                                <MenuDesplegableElemento key={c.href} icono={c.icono} onSelect={() => router.push(c.href)}>
                                    {c.etiqueta}
                                </MenuDesplegableElemento>
                            ))}
                        </MenuDesplegableContenido>
                    </MenuDesplegable>
                ) : undefined
            }
        />
    );
}
