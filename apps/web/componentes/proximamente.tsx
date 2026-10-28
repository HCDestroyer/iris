"use client";

import { EstadoVacio, Tarjeta } from "@iris/ui";
import { Hourglass } from "lucide-react";
import { MENU } from "@/lib/menu";
import { EncabezadoPagina } from "./encabezado-pagina";
import { RequierePermiso } from "./requiere-permiso";

/** Página de un módulo del menú que aún no tiene pantalla. */
export function Proximamente({ href, descripcion }: { href: string; descripcion: string }) {
    const entrada = MENU.find((e) => e.href === href);
    const titulo = entrada?.etiqueta ?? "Próximamente";
    return (
        <RequierePermiso permisos={entrada?.permisos ?? null}>
            <EncabezadoPagina titulo={titulo} />
            <Tarjeta>
                <EstadoVacio icono={entrada?.icono ?? Hourglass} titulo="Próximamente" descripcion={descripcion} />
            </Tarjeta>
        </RequierePermiso>
    );
}
