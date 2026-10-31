"use client";

import { Boton, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { History } from "lucide-react";
import Link from "next/link";
import { ListaAccesos } from "./lista-accesos";

/** Los últimos accesos del usuario y el enlace al historial completo (Administración → Accesos). */
export function AccesosUsuario({ usuarioId }: { usuarioId: number }) {
    return (
        <Tarjeta data-seccion="accesos">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Accesos recientes</TarjetaTitulo>
                <Boton variante="enlace" asChild>
                    <Link href={`/administracion/accesos?usuarioId=${usuarioId}`}>
                        <History />
                        Ver todo el historial
                    </Link>
                </Boton>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ListaAccesos usuarioId={usuarioId} query={{}} limite={10} conUsuario={false} />
            </TarjetaContenido>
        </Tarjeta>
    );
}
