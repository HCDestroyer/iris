"use client";

import {
    Boton,
    Dialogo,
    DialogoContenido,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    MenuDesplegableEtiqueta,
    MenuDesplegableSeparador,
} from "@iris/ui";
import { ChevronDown, CircleCheck, CircleUserRound, KeyRound, LogOut, ShieldCheck, UserRound } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { useState } from "react";
import { rutaConfigurarMfa } from "@/lib/cuenta/sesion";
import { rutaEntrar, useSesion } from "@/lib/sesion";
import { BloqueoContrasena, FormularioContrasena } from "./formulario-contrasena";

type Props = {
    cerrando: boolean;
    cerrarSesion: () => void;
};

/**
 * Menú del usuario en la barra superior: su nombre y correo, «Mi perfil», «Cambiar contraseña»,
 * «Configurar verificación en dos pasos» (si no la tiene) y «Cerrar sesión».
 */
export function MenuUsuario({ cerrando, cerrarSesion }: Props) {
    const { yo, refrescarSesion } = useSesion();
    const router = useRouter();
    const ruta = usePathname();
    const [dialogo, setDialogo] = useState(false);

    return (
        <>
            <MenuDesplegable>
                <MenuDesplegableDisparador asChild>
                    <Boton variante="fantasma" className="max-w-64 px-2 sm:px-2.5" aria-label={`Tu cuenta: ${yo.usuario.nombre}`} title="Tu cuenta">
                        <UserRound />
                        <span className="hidden min-w-0 truncate md:inline" data-testid="usuario-nombre">
                            {yo.usuario.nombre}
                        </span>
                        <ChevronDown className="text-muted-foreground" />
                    </Boton>
                </MenuDesplegableDisparador>
                <MenuDesplegableContenido>
                    <MenuDesplegableEtiqueta>
                        <span className="truncate font-medium">{yo.usuario.nombre}</span>
                        <span className="truncate text-xs font-normal text-muted-foreground">{yo.usuario.correo}</span>
                    </MenuDesplegableEtiqueta>
                    <MenuDesplegableSeparador />
                    <MenuDesplegableElemento icono={CircleUserRound} onSelect={() => router.push("/cuenta")}>
                        Mi perfil
                    </MenuDesplegableElemento>
                    <MenuDesplegableElemento icono={KeyRound} onSelect={() => setDialogo(true)}>
                        Cambiar contraseña
                    </MenuDesplegableElemento>
                    {yo.usuario.mfaActivo ? (
                        <MenuDesplegableElemento icono={CircleCheck} disabled>
                            Verificación en dos pasos activa
                        </MenuDesplegableElemento>
                    ) : (
                        <MenuDesplegableElemento icono={ShieldCheck} onSelect={() => router.push(rutaConfigurarMfa(ruta))}>
                            Configurar verificación en dos pasos
                        </MenuDesplegableElemento>
                    )}
                    <MenuDesplegableSeparador />
                    <MenuDesplegableElemento icono={LogOut} onSelect={cerrarSesion} disabled={cerrando}>
                        {cerrando ? "Cerrando sesión…" : "Cerrar sesión"}
                    </MenuDesplegableElemento>
                </MenuDesplegableContenido>
            </MenuDesplegable>
            <Dialogo open={dialogo} onOpenChange={setDialogo}>
                {dialogo ? (
                    <DialogoCambiarContrasena
                        alExpirar={() => router.replace(rutaEntrar(ruta, "expirada"))}
                        alTerminar={() => {
                            setDialogo(false);
                            refrescarSesion();
                        }}
                        alCancelar={() => setDialogo(false)}
                        volverA={ruta}
                    />
                ) : null}
            </Dialogo>
        </>
    );
}

type Etapa = { tipo: "formulario" } | { tipo: "listo" } | { tipo: "bloqueado"; segundos: number };

function DialogoCambiarContrasena({
    alTerminar,
    alCancelar,
    alExpirar,
    volverA,
}: {
    alTerminar: () => void;
    alCancelar: () => void;
    alExpirar: () => void;
    volverA: string;
}) {
    const [etapa, setEtapa] = useState<Etapa>({ tipo: "formulario" });
    return (
        <DialogoContenido
            titulo="Cambiar contraseña"
            descripcion={etapa.tipo === "formulario" ? "Escribe tu contraseña actual y la nueva. Cerraremos tus otras sesiones abiertas." : undefined}
            // Tras el 429 la sesión ya no existe: cerrar el diálogo dejaría un panel muerto.
            {...(etapa.tipo === "bloqueado" ? { onEscapeKeyDown: (e: KeyboardEvent) => e.preventDefault(), onPointerDownOutside: (e: Event) => e.preventDefault() } : {})}
        >
            {etapa.tipo === "bloqueado" ? (
                <BloqueoContrasena segundos={etapa.segundos} siguiente={volverA} />
            ) : etapa.tipo === "listo" ? (
                <div className="grid gap-4">
                    <div className="flex items-start gap-3" role="status">
                        <CircleCheck aria-hidden className="mt-0.5 size-5 shrink-0 text-success" />
                        <div className="grid gap-1">
                            <p className="font-medium">Contraseña cambiada</p>
                            <p className="text-sm text-muted-foreground">Desde ahora entra con tu contraseña nueva. Cerramos tus otras sesiones abiertas.</p>
                        </div>
                    </div>
                    <Boton onClick={alTerminar} className="justify-self-end">
                        Listo
                    </Boton>
                </div>
            ) : (
                <FormularioContrasena
                    alCambiar={() => setEtapa({ tipo: "listo" })}
                    alBloquear={(segundos) => setEtapa({ tipo: "bloqueado", segundos })}
                    alExpirar={alExpirar}
                    acciones={
                        <Boton variante="fantasma" onClick={alCancelar}>
                            Cancelar
                        </Boton>
                    }
                />
            )}
        </DialogoContenido>
    );
}
