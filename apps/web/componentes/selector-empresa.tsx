"use client";

import { Aviso, Boton, Selector, Tarjeta, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Building2, ChevronRight, DatabaseZap } from "lucide-react";
import Link from "next/link";
import { useSesion } from "@/lib/sesion";
import { useVoz } from "./voz/proveedor";

/**
 * Cambiar de empresa. Con una llamada viva o sonando pregunta antes y la cuelga o suelta con la
 * empresa de la llamada: después del cambio el servidor ya no la encontraría (404) y quedaría viva.
 */
function useCambiarEmpresa(): (id: number) => Promise<void> {
    const { elegirEmpresa } = useSesion();
    const voz = useVoz();
    return async (id) => {
        if (voz && !(await voz.antesDeSalir())) return;
        elegirEmpresa(id);
    };
}

/**
 * Empresa activa en la barra superior: con varias, una lista para cambiar; con una, solo su
 * nombre; sin conector (sin empresas registradas), nada.
 */
export function SelectorEmpresa() {
    const { yo, empresas } = useSesion();
    const cambiar = useCambiarEmpresa();
    if (yo.modoEmpresa === "sin_conector" || empresas.length === 0) {
        return null;
    }
    if (empresas.length === 1) {
        const unica = empresas[0];
        return (
            <span className="inline-flex min-w-0 items-center gap-1.5 text-sm text-muted-foreground" data-testid="empresa-activa">
                <Building2 aria-hidden className="size-4 shrink-0" />
                <span className="truncate">{unica?.nombre}</span>
            </span>
        );
    }
    const activa = yo.empresaActiva?.id;
    return (
        <span className="flex min-w-0 items-center gap-1.5">
            <Building2 aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            <Selector
                tamano="sm"
                aria-label="Empresa activa"
                className="w-[9.5rem] sm:w-52"
                value={activa === undefined ? "" : String(activa)}
                onChange={(e) => {
                    const id = Number(e.target.value);
                    if (id > 0) {
                        void cambiar(id);
                    }
                }}
            >
                {activa === undefined ? (
                    <option value="" disabled>
                        Elegir empresa…
                    </option>
                ) : null}
                {empresas.map((e) => (
                    <option key={e.id} value={e.id}>
                        {e.nombre}
                    </option>
                ))}
            </Selector>
        </span>
    );
}

/** Pantalla para elegir empresa cuando hay varias y no hay ninguna activa (o la API la pidió). */
export function ElegirEmpresa({ motivo }: { motivo?: "no_permitida" | undefined }) {
    const { empresas } = useSesion();
    const cambiar = useCambiarEmpresa();
    return (
        <div className="mx-auto grid max-w-lg gap-4">
            {motivo === "no_permitida" ? (
                <Aviso tono="warning" titulo="La empresa que tenías elegida ya no está disponible">
                    Puede que te la hayan quitado. Elige otra para seguir.
                </Aviso>
            ) : null}
            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>
                        <span className="inline-flex items-center gap-2">
                            <Building2 aria-hidden className="size-5 text-primary" />
                            Elige la empresa
                        </span>
                    </TarjetaTitulo>
                    <TarjetaDescripcion>
                        Trabajas en varias empresas. Los tickets y demás datos que veas serán los de la que elijas; puedes cambiarla
                        en la barra superior.
                    </TarjetaDescripcion>
                </TarjetaEncabezado>
                <ul className="grid gap-2 p-5 pt-0" aria-label="Empresas">
                    {empresas.map((e) => (
                        <li key={e.id}>
                            <Boton variante="secundario" className="w-full justify-between" onClick={() => void cambiar(e.id)}>
                                <span className="truncate">{e.nombre}</span>
                                <ChevronRight aria-hidden />
                            </Boton>
                        </li>
                    ))}
                </ul>
            </Tarjeta>
        </div>
    );
}

/** Aviso para un admin global cuando quedan datos de antes del conector sin empresa. */
export function AvisoDatosSinEmpresa() {
    return (
        <Aviso
            tono="warning"
            titulo="Hay datos de antes del conector sin empresa"
            icono={DatabaseZap}
            className="mb-4"
            accion={
                <Boton asChild variante="secundario" tamano="sm">
                    <Link href="/administracion/datos-sin-empresa">Asignar a una empresa…</Link>
                </Boton>
            }
        >
            Nadie los ve hasta que se asignen a una empresa.
        </Aviso>
    );
}
