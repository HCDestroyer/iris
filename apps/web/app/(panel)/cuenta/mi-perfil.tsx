"use client";

import { Boton, InsigniaEstado, Tarjeta, TarjetaContenido } from "@iris/ui";
import { ShieldCheck, ShieldOff } from "lucide-react";
import Link from "next/link";
import { VinculosSSO } from "@/componentes/cuenta/vinculos-sso";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { rutaConfigurarMfa } from "@/lib/cuenta/sesion";
import { useSesion } from "@/lib/sesion";

/** `/cuenta`: datos propios, verificación en dos pasos y las cuentas vinculadas (con «Quitar»). */
export function MiPerfil() {
    const { yo } = useSesion();
    const u = yo.usuario;
    return (
        <>
            <EncabezadoPagina titulo="Mi perfil" descripcion={u.correo} />
            <div className="grid gap-4">
                <Tarjeta>
                    <TarjetaContenido className="grid gap-3 pt-5">
                        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-sm">
                            <dt className="text-muted-foreground">Nombre</dt>
                            <dd>{u.nombre}</dd>
                            <dt className="text-muted-foreground">Correo</dt>
                            <dd className="break-all">{u.correo}</dd>
                        </dl>
                        <div className="flex flex-wrap items-center gap-2">
                            {u.mfaActivo ? (
                                <InsigniaEstado tono="success" icono={ShieldCheck} texto="Verificación en dos pasos activa" />
                            ) : (
                                <>
                                    <InsigniaEstado tono="neutral" icono={ShieldOff} texto="Sin verificación en dos pasos" />
                                    <Boton variante="secundario" tamano="sm" asChild>
                                        <Link href={rutaConfigurarMfa("/cuenta")}>Configurarla</Link>
                                    </Boton>
                                </>
                            )}
                        </div>
                    </TarjetaContenido>
                </Tarjeta>
                <VinculosSSO usuarioId={u.id} propio />
            </div>
        </>
    );
}
