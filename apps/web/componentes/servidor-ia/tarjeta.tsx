"use client";

import { Boton, Tarjeta } from "@iris/ui";
import { ChevronRight, Cpu } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useState } from "react";
import { useLectura } from "@/lib/lectura";
import { leerMotor } from "@/lib/servidor-ia/api";
import { lineaTarjeta } from "@/lib/servidor-ia/presentacion";
import { LineaEstado, RUTA, usePermisosServidorIa } from "./comun";

/**
 * La tarjeta del Servidor de IA en Integraciones, junto a Argos y Horus (contratos/voz.md §4.8).
 * Solo para quien administra la instalación: dice cómo está y lleva al tablero.
 */
export function TarjetaServidorIa() {
    const { global } = usePermisosServidorIa();
    if (!global) return null;
    return <Contenido />;
}

function Contenido() {
    const lectura = useLectura((signal) => leerMotor(signal));
    const [ahora] = useState(() => Date.now());
    const motor = lectura.estado === "listo" ? lectura.datos : null;
    return (
        <Tarjeta className="mt-4 flex min-w-0 flex-wrap items-center gap-3 p-4" data-slot="tarjeta-servidor-ia">
            <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-elevated text-muted-foreground">
                <Cpu className="size-5" />
            </span>
            <div className="grid min-w-0 flex-1 gap-0.5">
                <h2 className="text-base font-semibold">Servidor de IA</h2>
                {lectura.estado === "cargando" ? (
                    <p className="text-sm text-muted-foreground">Leyendo su estado…</p>
                ) : lectura.estado === "error" ? (
                    // Sin lectura no se dice ningún estado.
                    <p className="text-sm text-warning">No se pudo leer su estado.</p>
                ) : motor ? (
                    <LineaEstado estado={motor.estado} linea={lineaTarjeta({ estado: motor.estado, version: motor.paquete.version, ultimoLatidoEn: motor.ultimoLatidoEn }, ahora)} />
                ) : (
                    <p className="text-sm text-muted-foreground">Una computadora tuya, con tarjeta gráfica, donde el asistente trabaja sin mandar nada a servicios de fuera. Todavía no hay ninguna enlazada.</p>
                )}
            </div>
            <Boton asChild variante="secundario" tamano="sm">
                <Link href={RUTA as Route}>
                    {motor ? "Abrir el tablero" : "Ver cómo enlazarlo"}
                    <ChevronRight />
                </Link>
            </Boton>
        </Tarjeta>
    );
}
