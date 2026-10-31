"use client";

import { useSearchParams } from "next/navigation";
import { CargandoRegistro } from "@/componentes/carga-registro";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { useCatalogosAgenda } from "@/componentes/despacho/datos";
import { FormularioTecnico } from "@/componentes/equipo/formulario-tecnico";
import { api, resolver } from "@/lib/api/cliente";
import { idDeParametro } from "@/lib/parametros";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/**
 * `/tecnicos/nuevo`. Con `?usuario=<id>` (de vuelta de «Crear usuario»), ese usuario queda
 * elegido: su correo se lee de la API (nunca de la URL) y si no se puede leer, se elige a mano.
 */
export function NuevoTecnico() {
    const usuarioId = idDeParametro(useSearchParams().get("usuario"));
    const { puede } = useSesion();
    const catalogos = useCatalogosAgenda();
    const leerUsuario = usuarioId !== null && puede(["usuarios.ver"]);
    const usuario = useLectura(async (signal) =>
        leerUsuario && usuarioId !== null ? resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id: usuarioId } }, signal })) : null,
    );
    if (catalogos.cuadrillas.estado === "cargando" || usuario.estado === "cargando") {
        return <CargandoRegistro que="las cuadrillas" />;
    }
    if (catalogos.cuadrillas.estado === "error") {
        return <AvisoCatalogos {...catalogos} />;
    }
    const inicial = usuario.estado === "listo" && usuario.datos ? { id: usuario.datos.id, correo: usuario.datos.correo } : null;
    return (
        <>
            <AvisoCatalogos {...catalogos} />
            <FormularioTecnico
                cuadrillas={catalogos.cuadrillas.datos}
                zonas={catalogos.zonas.estado === "listo" ? catalogos.zonas.datos : undefined}
                volverA="/tecnicos"
                usuarioInicial={inicial}
            />
        </>
    );
}
