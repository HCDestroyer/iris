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
 * Con `?argos=<idExterno>` («Crear ficha» en «Técnicos de Argos sin ficha»), ese técnico de Argos
 * queda elegido si la API lo devuelve sin ficha, y su correo (el de esa fila de la API) busca el
 * usuario de Iris.
 */
export function NuevoTecnico() {
    const params = useSearchParams();
    const usuarioId = idDeParametro(params.get("usuario"));
    const argosId = (params.get("argos") ?? "").trim().slice(0, 100);
    const argosInicial = argosId !== "" ? { idExterno: argosId } : null;
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
                argosInicial={argosInicial}
            />
        </>
    );
}
