"use client";

import { Boton, Tarjeta } from "@iris/ui";
import { PhoneCall } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { leerCentralita } from "@/lib/centralita/api";
import { estadoConexion } from "@/lib/centralita/presentacion";
import { PERMISO_ASIGNAR_EXTENSION, PERMISO_CONFIGURAR_CENTRALITA } from "@/lib/centralita/tipos";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/** En «Integraciones»: la centralita Issabel (E4a) con su estado en una línea y el acceso a su pantalla. */
export function TarjetaCentralita() {
    const { puede } = useSesion();
    if (!puede([PERMISO_CONFIGURAR_CENTRALITA, PERMISO_ASIGNAR_EXTENSION])) return null;
    return <Contenido />;
}

function Contenido() {
    const lectura = useLectura((signal) => leerCentralita(signal));
    const [ahora] = useState(() => Date.now());
    const c = lectura.estado === "listo" ? lectura.datos : null;
    const e = c ? estadoConexion(c, ahora) : null;
    return (
        <Tarjeta className="mt-4 flex min-w-0 flex-wrap items-center gap-3 p-4" data-slot="tarjeta-centralita">
            <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-elevated text-muted-foreground">
                <PhoneCall className="size-5" />
            </span>
            <div className="grid min-w-0 flex-1 gap-0.5">
                <h2 className="text-base font-semibold">Centralita</h2>
                {lectura.estado === "cargando" ? (
                    <p className="text-sm text-muted-foreground">Leyendo su estado…</p>
                ) : lectura.estado === "error" ? (
                    <p className="text-sm text-warning">No se pudo leer su estado.</p>
                ) : c && e ? (
                    <p className="text-sm text-muted-foreground">
                        {c.nombre} · {e.texto}
                    </p>
                ) : (
                    <p className="text-sm text-muted-foreground">Issabel de solo lectura: cuando suena la extensión de alguien, Iris le muestra quién llama.</p>
                )}
            </div>
            <Boton asChild variante="secundario" tamano="sm">
                <Link href="/administracion/integraciones/centralita">{c ? "Abrir" : "Conectar"}</Link>
            </Boton>
        </Tarjeta>
    );
}
