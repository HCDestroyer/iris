"use client";

import { Aviso, Boton } from "@iris/ui";
import Link from "next/link";
import { CargaRegistro } from "@/componentes/carga-registro";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { leerAjustesAsistente } from "@/lib/asistente/api";
import { MAX_PERFILES, perfilesDe } from "@/lib/asistente/fase3b";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { useLectura } from "@/lib/lectura";
import { FormularioPerfil } from "../formulario";

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <NuevoPerfil />
        </RequierePermiso>
    );
}

function NuevoPerfil() {
    const lectura = useLectura((signal) => leerAjustesAsistente(signal));
    return (
        <CargaRegistro lectura={lectura} que="los perfiles del asistente" noEncontrado="Sin ajustes">
            {(ajustes) =>
                perfilesDe(ajustes).length >= MAX_PERFILES ? (
                    <Aviso
                        tono="warning"
                        titulo={`Ya hay ${MAX_PERFILES} perfiles`}
                        accion={
                            <Boton asChild variante="secundario" tamano="sm">
                                <Link href="/administracion/asistente/perfiles">Ver los perfiles</Link>
                            </Boton>
                        }
                    >
                        Quita uno que ya no uses para agregar otro.
                    </Aviso>
                ) : (
                    <FormularioPerfil ajustes={ajustes} />
                )
            }
        </CargaRegistro>
    );
}
