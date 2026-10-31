"use client";

import { cn } from "@iris/ui";
import { CalendarCheck, CloudUpload, PackageOpen, UserRound, type LucideIcon } from "lucide-react";
import type { MouseEvent } from "react";
import { pestanaDe, urlDeVista, type Pestana, type Vista } from "@/lib/campo/vista";
import { resumenEnvio } from "./barra-conexion";
import { useCampo } from "./contexto";
import { useInventario } from "./inventario-datos";
import { porRecibir } from "@iris/sync/campo";

const PESTANAS: { id: Pestana; texto: string; icono: LucideIcon; vista: Vista }[] = [
    { id: "hoy", texto: "Hoy", icono: CalendarCheck, vista: { tipo: "lista" } },
    { id: "recibir", texto: "Por recibir", icono: PackageOpen, vista: { tipo: "recibir" } },
    { id: "pendientes", texto: "Pendientes", icono: CloudUpload, vista: { tipo: "pendientes" } },
    { id: "yo", texto: "Yo", icono: UserRound, vista: { tipo: "yo" } },
];

/** Barra inferior de la app de campo: Hoy · Por recibir · Pendientes · Yo (zonas táctiles de 56 px). */
export function NavegacionInferior() {
    const { vista, navegar, estado } = useCampo();
    const { traslados } = useInventario();
    const actual = pestanaDe(vista);
    const envio = resumenEnvio(estado);
    const cuentas: Partial<Record<Pestana, number>> = {
        pendientes: envio.porEnviar + Math.max(0, envio.archivos) + envio.rechazos,
        recibir: traslados.datos ? porRecibir(traslados.datos, estado.recepciones).length : 0,
    };
    return (
        <nav
            aria-label="Secciones de la app"
            data-slot="navegacion-inferior"
            className="glass-material fixed inset-x-0 bottom-0 z-30 mx-auto w-full max-w-2xl border-t border-border pb-[env(safe-area-inset-bottom)]"
        >
            <ul className="grid grid-cols-4">
                {PESTANAS.map((p) => {
                    const Icono = p.icono;
                    const activa = actual === p.id;
                    const cuenta = cuentas[p.id] ?? 0;
                    const ir = (e: MouseEvent<HTMLAnchorElement>) => {
                        e.preventDefault();
                        navegar(p.vista);
                    };
                    return (
                        <li key={p.id}>
                            <a
                                href={urlDeVista(p.vista)}
                                onClick={ir}
                                aria-current={activa ? "page" : undefined}
                                aria-label={cuenta > 0 ? `${p.texto} (${cuenta})` : p.texto}
                                className={cn(
                                    "relative flex min-h-14 flex-col items-center justify-center gap-0.5 px-1 py-1.5 text-xs font-medium transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2",
                                    activa ? "text-primary" : "text-muted-foreground hover:text-foreground",
                                )}
                            >
                                <span className="relative">
                                    <Icono aria-hidden className="size-6" />
                                    {cuenta > 0 ? (
                                        <span
                                            aria-hidden
                                            className="absolute -right-2.5 -top-1.5 grid min-w-4.5 place-content-center rounded-full bg-primary px-1 text-[0.65rem] font-semibold leading-4.5 text-primary-foreground tabular-nums"
                                        >
                                            {cuenta > 99 ? "99+" : cuenta}
                                        </span>
                                    ) : null}
                                </span>
                                {p.texto}
                                {activa ? <span aria-hidden className="absolute inset-x-6 top-0 h-0.5 rounded-full bg-primary" /> : null}
                            </a>
                        </li>
                    );
                })}
            </ul>
        </nav>
    );
}
