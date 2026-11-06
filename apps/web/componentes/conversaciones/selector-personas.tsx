"use client";

import { Autocompletar, Boton } from "@iris/ui";
import { X } from "lucide-react";
import { useState } from "react";
import { estaDisponible } from "@/lib/conversaciones/atencion";
import type { AgenteDisponible } from "@/lib/conversaciones/contrato-11";
import { AvatarDisponible } from "./disponibilidad";

type Props = {
    etiqueta: string;
    /** Texto del campo para agregar («Agregar persona…»). */
    placeholder: string;
    ayuda?: string;
    personas: readonly AgenteDisponible[];
    elegidos: readonly number[];
    alCambiar: (ids: number[]) => void;
    maximo?: number;
    error?: string | undefined;
};

/**
 * Personas de una cola como fichas con su punto de disponibilidad y una «X» para quitarlas, y un
 * campo con búsqueda para agregar más (miembros y supervisores de la cola).
 */
export function SelectorPersonas({ etiqueta, placeholder, ayuda, personas, elegidos, alCambiar, maximo, error }: Props) {
    const [texto, setTexto] = useState("");
    const lista = elegidos.map((id) => personas.find((p) => p.id === id)).filter((p): p is AgenteDisponible => Boolean(p));
    const filtro = texto.trim().toLowerCase();
    const opciones = personas
        .filter((p) => !elegidos.includes(p.id) && (!filtro || p.nombre.toLowerCase().includes(filtro)))
        .sort((a, b) => a.nombre.localeCompare(b.nombre, "es"))
        .map((p) => ({ valor: String(p.id), etiqueta: p.nombre, detalle: estaDisponible(p) ? "Disponible" : "No disponible" }));
    const lleno = maximo !== undefined && elegidos.length >= maximo;
    return (
        <div className="grid min-w-0 gap-2" data-slot="selector-personas">
            {lista.length > 0 ? (
                <ul aria-label={etiqueta} className="flex min-w-0 flex-wrap gap-1.5">
                    {lista.map((p) => (
                        <li key={p.id} className="flex h-8 max-w-full min-w-0 items-center gap-1.5 rounded-full border border-border bg-elevated pr-0.5 pl-1">
                            <AvatarDisponible nombre={p.nombre} disponible={estaDisponible(p)} className="[&_[data-slot=avatar-usuario]]:size-6" />
                            <span className="min-w-0 truncate text-[0.8125rem] font-medium">{p.nombre}</span>
                            <Boton variante="fantasma" tamano="icono-sm" className="size-7 rounded-full" aria-label={`Quitar a ${p.nombre}`} onClick={() => alCambiar(elegidos.filter((x) => x !== p.id))}>
                                <X />
                            </Boton>
                        </li>
                    ))}
                </ul>
            ) : null}
            <Autocompletar
                etiqueta={lista.length ? `Agregar a ${etiqueta.toLowerCase()}` : etiqueta}
                opciones={opciones}
                valor={null}
                texto={texto}
                alBuscar={setTexto}
                alCambiar={(v) => {
                    if (!v) return;
                    alCambiar([...elegidos, Number(v)]);
                    setTexto("");
                }}
                placeholder={lleno ? `Hasta ${maximo}` : placeholder}
                disabled={lleno}
                sinResultados={personas.length === elegidos.length ? "Ya están todos" : "Nadie con ese nombre"}
                ayuda={ayuda}
                error={error}
                conLupa
            />
        </div>
    );
}
