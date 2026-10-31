"use client";

import { Boton, EstadoVacio, Esqueleto, Tarjeta, TarjetaContenido } from "@iris/ui";
import { ArrowLeft, SearchX } from "lucide-react";
import Link from "next/link";
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
import { ErrorApi } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { Activo, Mfa, textoRoles, Tipo } from "../lista-usuarios";

/**
 * `/usuarios/[id]`: datos del usuario con «Editar» y «Desactivar», sus empresas, cuentas
 * vinculadas (autologin desde Argos), roles, sesiones y accesos (cada sección según su permiso).
 */
export function DetalleUsuario() {
    const { id: crudo } = useParams<{ id: string }>();
    const id = Number(crudo);
    const { yo, puede } = useSesion();
    const valido = Number.isSafeInteger(id) && id > 0;
    const lectura = useLectura(async (signal) => (valido ? resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id } }, signal })) : null));

    const volver = (
        <Boton variante="fantasma" tamano="sm" asChild className="mb-3 -ml-2">
            <Link href="/usuarios">
                <ArrowLeft />
                Usuarios
            </Link>
        </Boton>
    );

    if (lectura.estado === "cargando") {
        return (
            <>
                {volver}
                <Esqueleto className="mb-4 h-10 w-64" />
                <Esqueleto className="h-40" />
            </>
        );
    }
    if (!valido || (lectura.estado === "error" && lectura.error instanceof ErrorApi && lectura.error.estado === 404) || (lectura.estado === "listo" && lectura.datos === null)) {
        return (
            <>
                {volver}
                <Tarjeta>
                    <EstadoVacio icono={SearchX} titulo="Usuario no encontrado" descripcion="No existe o no está en tu alcance." />
                </Tarjeta>
            </>
        );
    }
    if (lectura.estado === "error") {
        return (
            <>
                {volver}
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el usuario" />
            </>
        );
    }
    const u = lectura.datos;
    if (!u) return null;
    return (
        <>
            {volver}
            <EncabezadoPagina titulo={u.nombre} descripcion={u.correo} acciones={<AccionesUsuario usuario={u} alCambiar={lectura.reemplazar} />} />
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
                <EmpresasUsuario usuario={u} alCambiar={lectura.reemplazar} />
                {u.tipo === "persona" ? <VinculosSSO key={`${u.correo}:${String(u.activo)}`} usuarioId={u.id} propio={u.id === yo.usuario.id} /> : null}
                {/* Pedido 10: roles con alcance, sesiones activas y accesos (cada sección según su permiso). */}
                {puede(["usuarios.asignar_rol", "usuarios.quitar_rol"]) ? <RolesUsuario usuario={u} alCambiar={lectura.reemplazar} /> : null}
                {puede(["accesos.ver"]) ? <SesionesUsuario usuarioId={u.id} propio={u.id === yo.usuario.id} /> : null}
                {puede(["accesos.ver"]) ? <AccesosUsuario usuarioId={u.id} /> : null}
            </div>
        </>
    );
}
