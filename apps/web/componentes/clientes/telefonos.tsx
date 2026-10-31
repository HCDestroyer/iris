import { Phone, Smartphone } from "lucide-react";
import { enlaceTel, type Telefono } from "@/lib/datos/telefono";

/** Teléfonos con el formato único (+502 5512 3344) y enlace para llamar. */
export function ListaTelefonos({ telefonos }: { telefonos: readonly Telefono[] }) {
    return (
        <ul className="grid gap-0.5">
            {telefonos.map((t, i) => {
                const tel = enlaceTel(t);
                const Icono = t.tipo === "movil" ? Smartphone : Phone;
                return (
                    <li key={`${t.tipo}-${i}`} className="flex flex-wrap items-center gap-1.5">
                        <Icono aria-hidden className="size-3.5 text-muted-foreground" />
                        {tel ? (
                            <a href={tel} className="text-primary tabular-nums hover:underline" title={t.original !== t.mostrar ? `Tal como se guardó: ${t.original}` : undefined}>
                                {t.mostrar}
                            </a>
                        ) : (
                            <span className="tabular-nums">{t.mostrar}</span>
                        )}
                        <span className="text-xs text-muted-foreground">{t.tipo === "movil" ? "móvil" : "teléfono"}</span>
                    </li>
                );
            })}
        </ul>
    );
}
