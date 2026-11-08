"use client";

import { Boton } from "@iris/ui";
import { Plus } from "lucide-react";
import Link from "next/link";
import { useSesion } from "@/lib/sesion";

/** «Agregar número» desde la guía, solo con `mensajeria.configurar`. */
export function AgregarDesdeGuia() {
    const { puede } = useSesion();
    if (!puede(["mensajeria.configurar"])) return null;
    return (
        <Boton asChild>
            <Link href="/administracion/numeros/nuevo">
                <Plus aria-hidden />
                Agregar número
            </Link>
        </Boton>
    );
}
