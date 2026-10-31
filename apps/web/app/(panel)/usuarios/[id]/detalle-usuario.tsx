"use client";

import { EstadoVacio, Esqueleto, Pestanas, Tarjeta, TarjetaContenido } from "@iris/ui";
import { SearchX } from "lucide-react";
import { useParams } from "next/navigation";
import { AccesosUsuario } from "@/componentes/accesos/accesos-usuario";
import { SesionesUsuario } from "@/componentes/accesos/sesiones-usuario";
import { VinculosSSO } from "@/componentes/cuenta/vinculos-sso";
import { RolesUsuario } from "@/componentes/roles/roles-usuario";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { AccionesUsuario } from "@/componentes/usuarios/editar-usuario";
import { EmpresasUsuario } from "@/componentes/usuarios/empresas-usuario";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { ErrorApi } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { usePestanaUrl } from "@/lib/pestanas";
import { useSesion } from "@/lib/sesion";
import { Activo, Mfa, textoRoles, Tipo } from "../lista-usuarios";

/**
 * `/usuarios/[id]`: datos del usuario con «Editar» y «Desactivar», sus empresas, cuentas
 * vinculadas (autologin desde Argos), roles, sesiones y accesos (cada sección según su permiso).
 */
/** Pestañas de la ficha del usuario (`?pestana=`). */
const PESTANAS_USUARIO = ["datos", "roles", "empresas", "sesiones", "accesos"] as const;
type PestanaUsuario = (typeof PESTANAS_USUARIO)[number];

export function DetalleUsuario() {
    const { id: crudo } = useParams<{ id: string }>();
    const id = Number(crudo);
    const { yo, puede } = useSesion();
    const valido = Number.isSafeInteger(id) && id > 0;
    const lectura = useLectura(async (signal) => (valido ? resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id } }, signal })) : null));
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.nombre : null);
    const [pestana, setPestana] = usePestanaUrl(PESTANAS_USUARIO, "datos");


    if (lectura.estado === "cargando") {
        return (
            <>
                <Esqueleto className="mb-4 h-10 w-64" />
                <Esqueleto className="h-40" />
            </>
        );
    }
    if (!valido || (lectura.estado === "error" && lectura.error instanceof ErrorApi && lectura.error.estado === 404) || (lectura.estado === "listo" && lectura.datos === null)) {
        return (
            <>
                <Tarjeta>
                    <EstadoVacio icono={SearchX} titulo="Usuario no encontrado" descripcion="No existe o no está en tu alcance." />
                </Tarjeta>
            </>
        );
    }
    if (lectura.estado === "error") {
        return (
            <>
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el usuario" />
            </>
        );
    }
    const u = lectura.datos;
    if (!u) return null;
    return (
        <>
            <EncabezadoPagina titulo={u.nombre} descripcion={u.correo} acciones={<AccionesUsuario usuario={u} alCambiar={lectura.reemplazar} />} />
            <Pestanas<PestanaUsuario>
                etiqueta="Secciones del usuario"
                seleccionada={pestana}
                alCambiar={setPestana}
                pestanas={[
                    {
                        id: "datos",
                        etiqueta: "Datos",
                        contenido: (
                            <div className="grid gap-4">
                                <Tarjeta>
                                    <TarjetaContenido className="grid gap-3 pt-5">
                                        <div className="flex flex-wrap items-center gap-2">
                                            <Tipo u={u} />
                                            <Mfa u={u} />
                                            <Activo u={u} />
                                        </div>
                                        <p className="text-sm">
                                            <span className="text-muted-foreground">Roles: </span>
                                            {textoRoles(u)}
                                        </p>
                                    </TarjetaContenido>
                                </Tarjeta>
                                {u.tipo === "persona" ? <VinculosSSO key={`${u.correo}:${String(u.activo)}`} usuarioId={u.id} propio={u.id === yo.usuario.id} /> : null}
                            </div>
                        ),
                    },
                    ...(puede(["usuarios.asignar_rol", "usuarios.quitar_rol"]) ? [{ id: "roles" as const, etiqueta: "Roles", contenido: <RolesUsuario usuario={u} alCambiar={lectura.reemplazar} /> }] : []),
                    { id: "empresas", etiqueta: "Empresas", contenido: <EmpresasUsuario usuario={u} alCambiar={lectura.reemplazar} /> },
                    ...(puede(["accesos.ver"])
                        ? [
                              { id: "sesiones" as const, etiqueta: "Sesiones", contenido: <SesionesUsuario usuarioId={u.id} propio={u.id === yo.usuario.id} /> },
                              { id: "accesos" as const, etiqueta: "Accesos", contenido: <AccesosUsuario usuarioId={u.id} /> },
                          ]
                        : []),
                ]}
            />
        </>
    );
}
