"use client";

import { GrupoSegmentado, type OpcionSegmentada } from "@iris/ui";
import { Monitor, Moon, Sun } from "lucide-react";
import { useTheme } from "next-themes";
import { useSyncExternalStore } from "react";

type Tema = "light" | "dark" | "system";

const OPCIONES: readonly OpcionSegmentada<Tema>[] = [
    { valor: "light", etiqueta: "Tema claro", icono: Sun, soloIcono: true },
    { valor: "dark", etiqueta: "Tema oscuro", icono: Moon, soloIcono: true },
    { valor: "system", etiqueta: "Tema del sistema", icono: Monitor, soloIcono: true },
];

const sinSuscripcion = () => () => undefined;

function esTema(valor: string | undefined): valor is Tema {
    return valor === "light" || valor === "dark" || valor === "system";
}

/** Selector de tema (claro, oscuro, sistema). En el servidor no se conoce: se pinta sin selección. */
export function SelectorTema({ className }: { className?: string }) {
    const { theme, setTheme } = useTheme();
    const montado = useSyncExternalStore(sinSuscripcion, () => true, () => false);
    return (
        <GrupoSegmentado
            etiqueta="Tema"
            opciones={OPCIONES}
            valor={montado && esTema(theme) ? theme : undefined}
            alCambiar={setTheme}
            {...(className ? { className } : {})}
        />
    );
}
