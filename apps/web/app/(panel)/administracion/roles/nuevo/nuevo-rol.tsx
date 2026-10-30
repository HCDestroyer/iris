"use client";

import { Boton } from "@iris/ui";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRef } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { FormularioRol } from "@/componentes/roles/formulario-rol";
import { cuerpoCrearRol } from "@/lib/accesos/esquemas";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";

export function NuevoRol() {
    const router = useRouter();
    const clave = useRef(nuevaClaveIdempotencia());
    return (
        <>
            <Boton variante="fantasma" tamano="sm" asChild className="mb-3 -ml-2">
                <Link href="/administracion/roles">
                    <ArrowLeft />
                    Roles y permisos
                </Link>
            </Boton>
            <EncabezadoPagina titulo="Nuevo rol" descripcion="Elige solo los permisos que necesita. No puedes dar permisos que tú no tienes." />
            <FormularioRol
                textoGuardar="Crear rol"
                alGuardar={async (d) => {
                    try {
                        const r = await resolver(api().POST("/api/v1/roles", { params: { header: { "Idempotency-Key": clave.current } }, body: cuerpoCrearRol(d) }));
                        router.push(`/administracion/roles/${r.id}`);
                    } catch (e) {
                        clave.current = claveTrasError(e, clave.current);
                        throw e;
                    }
                }}
            />
        </>
    );
}
