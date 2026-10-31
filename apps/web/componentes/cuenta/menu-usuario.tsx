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
    MenuEnLinea,
} from "@iris/ui";
import { ChevronDown, CircleCheck, CircleUserRound, KeyRound, LogOut, ShieldCheck, UserRound } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { rutaConfigurarMfa } from "@/lib/cuenta/sesion";
import { rutaEntrar, useSesion } from "@/lib/sesion";
import { BloqueoContrasena, FormularioContrasena } from "./formulario-contrasena";

type Props = {
    cerrando: boolean;
    cerrarSesion: () => void;
};

type ContextoCuenta = { abrirCambioContrasena: () => void };

const Cuenta = createContext<ContextoCuenta | null>(null);

/**
 * Dueño del diálogo «Cambiar contraseña»: vive fuera del menú para que siga abierto aunque el menú
 * (o la hoja «Más» del teléfono) se cierre al elegir la opción.
 */
export function ProveedorCuenta({ children }: { children: ReactNode }) {
    const { refrescarSesion } = useSesion();
    const router = useRouter();
    const ruta = usePathname();
    const [dialogo, setDialogo] = useState(false);
    const valor = useMemo(() => ({ abrirCambioContrasena: () => setDialogo(true) }), []);
    return (
        <Cuenta.Provider value={valor}>
            {children}
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
        </Cuenta.Provider>
    );
}

/** Opciones de la cuenta: las mismas en el menú de la barra superior y en «Más» del teléfono. */
function OpcionesCuenta({ cerrando, cerrarSesion, antes }: Props & { antes?: () => void }) {
    const { yo } = useSesion();
    const router = useRouter();
    const ruta = usePathname();
    const cuenta = useContext(Cuenta);
    const con = (accion: () => void) => () => {
        antes?.();
        accion();
    };
    return [
        <MenuDesplegableEtiqueta key="etiqueta">
            <span className="truncate font-medium">{yo.usuario.nombre}</span>
            <span className="truncate text-xs font-normal text-muted-foreground">{yo.usuario.correo}</span>
        </MenuDesplegableEtiqueta>,
        <MenuDesplegableSeparador key="sep-1" />,
        <MenuDesplegableElemento key="perfil" icono={CircleUserRound} onSelect={con(() => router.push("/cuenta"))}>
            Mi perfil
        </MenuDesplegableElemento>,
        <MenuDesplegableElemento key="contrasena" icono={KeyRound} onSelect={con(() => cuenta?.abrirCambioContrasena())}>
            Cambiar contraseña
        </MenuDesplegableElemento>,
        yo.usuario.mfaActivo ? (
            <MenuDesplegableElemento key="mfa" icono={CircleCheck} disabled>
                Verificación en dos pasos activa
            </MenuDesplegableElemento>
        ) : (
            <MenuDesplegableElemento key="mfa" icono={ShieldCheck} onSelect={con(() => router.push(rutaConfigurarMfa(ruta)))}>
                Configurar verificación en dos pasos
            </MenuDesplegableElemento>
        ),
        <MenuDesplegableSeparador key="sep-2" />,
        <MenuDesplegableElemento key="salir" icono={LogOut} onSelect={con(cerrarSesion)} disabled={cerrando}>
            {cerrando ? "Cerrando sesión…" : "Cerrar sesión"}
        </MenuDesplegableElemento>,
    ];
}

/**
 * Menú del usuario en la barra superior: su nombre y correo, «Mi perfil», «Cambiar contraseña»,
 * «Configurar verificación en dos pasos» (si no la tiene) y «Cerrar sesión».
 */
export function MenuUsuario({ cerrando, cerrarSesion }: Props) {
    const { yo } = useSesion();
    return (
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
            <MenuDesplegableContenido>{OpcionesCuenta({ cerrando, cerrarSesion })}</MenuDesplegableContenido>
        </MenuDesplegable>
    );
}

/** La cuenta como menú a la vista, dentro de «Más» en el teléfono; `antes` cierra la hoja. */
export function MenuCuentaLista({ cerrando, cerrarSesion, antes }: Props & { antes: () => void }) {
    return <MenuEnLinea etiqueta="Tu cuenta">{OpcionesCuenta({ cerrando, cerrarSesion, antes })}</MenuEnLinea>;
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
            cerrable={etapa.tipo !== "bloqueado"}
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
