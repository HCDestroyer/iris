import type { Schema } from "@iris/contratos";
import { Aviso, Boton } from "@iris/ui";
import { ExternalLink } from "lucide-react";
import Link from "next/link";
import { textoEstadoCualquiera } from "@/lib/comercial/presentacion";
import { iniciales } from "@/lib/conversaciones/presentacion";

/*
 * Prospectos del número de la conversación (contrato con Argos §3.6, P2): el chat ubica al
 * prospecto por cualquiera de sus números. Un número que no es el principal no ve la cotización,
 * la dirección ni el plan hasta confirmar con un código enviado al principal; la API nunca manda
 * esos datos aquí y la ficha se lo recuerda a quien atiende.
 */

export type CoincidenciaProspecto = Schema<"CoincidenciaProspecto">;

export const TITULO_PROSPECTO_RESTRINGIDO = "No es el número principal del prospecto";
export const TEXTO_PROSPECTO_RESTRINGIDO = "No compartas la cotización, la dirección ni el plan: primero hay que confirmar con un código enviado al número principal.";

/** Línea secundaria de un prospecto en las coincidencias: número, estado y qué número es. */
export function textoCoincidenciaProspecto(p: Pick<CoincidenciaProspecto, "numero" | "estado" | "principal">): string {
    return [p.numero, textoEstadoCualquiera(p.estado), p.principal ? "número principal" : "otro número del prospecto"].filter(Boolean).join(" · ");
}

/** Aviso de la ficha cuando el número de la conversación no es el principal del prospecto. */
export function AvisoProspectoRestringido() {
    return (
        <Aviso tono="warning" titulo={TITULO_PROSPECTO_RESTRINGIDO}>
            {TEXTO_PROSPECTO_RESTRINGIDO}
        </Aviso>
    );
}

/** Prospectos abiertos con el número (solo los que el usuario ve), con enlace a su ficha. */
export function ListaProspectosNumero({ prospectos }: { prospectos: readonly CoincidenciaProspecto[] }) {
    if (prospectos.length === 0) return null;
    const restringido = prospectos.some((p) => !p.principal);
    return (
        <div className="grid min-w-0 grid-cols-1 gap-1.5" data-testid="prospectos-numero">
            <ul className="grid gap-1.5" aria-label="Prospectos con este número">
                {prospectos.map((p) => (
                    <li key={p.prospectoId} className="flex min-w-0 items-center gap-2.5 rounded-[0.625rem] border border-border p-2">
                        <span aria-hidden className="flex size-[1.875rem] shrink-0 items-center justify-center rounded-full bg-elevated text-[0.6875rem] font-semibold">
                            {iniciales(p.nombre)}
                        </span>
                        <span className="flex min-w-0 flex-1 flex-col">
                            <span className="truncate text-[0.8125rem] font-semibold">{p.nombre}</span>
                            <span className="truncate text-[0.71875rem] text-muted-foreground">{textoCoincidenciaProspecto(p)}</span>
                        </span>
                        <Boton asChild variante="secundario" tamano="sm" className="shrink-0">
                            <Link href={`/comercial/${p.prospectoId}`} aria-label={`Ver el prospecto ${p.numero}`}>
                                <ExternalLink />
                                Ver
                            </Link>
                        </Boton>
                    </li>
                ))}
            </ul>
            {restringido ? <p className="text-xs text-muted-foreground">{TEXTO_PROSPECTO_RESTRINGIDO}</p> : null}
        </div>
    );
}
