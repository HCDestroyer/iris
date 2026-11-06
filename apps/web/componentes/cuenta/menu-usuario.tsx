"use client";

import {
    AvatarUsuario,
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
    PanelEmergente,
} from "@iris/ui";
import { ChevronDown, CircleCheck, CircleUserRound, KeyRound, LogOut, ShieldCheck, UserRound } from "lucide-react";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { rutaConfigurarMfa } from "@/lib/cuenta/sesion";
import { listaColas, useDisponibilidad } from "@/lib/conversaciones/disponibilidad";
import { rutaEntrar, useSesion } from "@/lib/sesion";
import { AvatarDisponible, ControlDisponibilidad, MiEstadoCorto, useEstoyDisponible } from "../conversaciones/disponibilidad";
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
function OpcionesCuenta({ cerrando, cerrarSesion, antes, sinEncabezado = false }: Props & { antes?: () => void; sinEncabezado?: boolean }) {
    const { yo } = useSesion();
    const router = useRouter();
    const ruta = usePathname();
    const cuenta = useContext(Cuenta);
    const con = (accion: () => void) => () => {
        antes?.();
        accion();
    };
    return [
        ...(sinEncabezado
            ? []
            : [
                  <MenuDesplegableEtiqueta key="etiqueta">
                      <span className="truncate font-medium">{yo.usuario.nombre}</span>
                      <span className="truncate text-xs font-normal text-muted-foreground">{yo.usuario.correo}</span>
                  </MenuDesplegableEtiqueta>,
                  <MenuDesplegableSeparador key="sep-1" />,
              ]),
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

/** «soporte» → «Soporte», «integracion_argos» → «Integracion argos». */
function textoRol(rol: string | undefined): string {
    if (!rol) return "";
    const t = rol.replaceAll("_", " ");
    return t.charAt(0).toUpperCase() + t.slice(1);
}

/**
 * La cuenta al pie de la barra lateral (nuevo diseño): iniciales, nombre y rol; abre el mismo
 * menú que en la barra superior. Contraída, solo las iniciales.
 */
export function MenuUsuarioLateral({ cerrando, cerrarSesion, contraida = false }: Props & { contraida?: boolean }) {
    const { yo } = useSesion();
    const rol = textoRol(yo.usuario.roles?.[0]?.rol);
    const disponibilidad = useDisponibilidad();
    if (disponibilidad) return <CuentaConDisponibilidad cerrando={cerrando} cerrarSesion={cerrarSesion} contraida={contraida} rol={rol} />;
    return (
        <MenuDesplegable>
            <MenuDesplegableDisparador asChild>
                <Boton
                    variante="fantasma"
                    className={contraida ? "size-10 justify-center rounded-lg p-0" : "h-auto w-full justify-start gap-2.5 rounded-lg px-2 py-2 text-left font-normal"}
                    aria-label={`Tu cuenta: ${yo.usuario.nombre}`}
                    title="Tu cuenta"
                >
                    <AvatarUsuario nombre={yo.usuario.nombre} />
                    {contraida ? null : (
                        <span className="grid min-w-0 flex-1">
                            <span className="truncate text-[0.8125rem] font-semibold" data-testid="usuario-nombre">
                                {yo.usuario.nombre}
                            </span>
                            {rol ? <span className="truncate text-xs text-muted-foreground">{rol}</span> : null}
                        </span>
                    )}
                    {contraida ? null : <ChevronDown className="text-muted-foreground" />}
                </Boton>
            </MenuDesplegableDisparador>
            <MenuDesplegableContenido align="start">{OpcionesCuenta({ cerrando, cerrarSesion })}</MenuDesplegableContenido>
        </MenuDesplegable>
    );
}

/**
 * Con la bandeja (fase 1.1): la cuenta al pie de la barra lateral lleva el punto de disponibilidad
 * y abre un panel con Disponible / No disponible arriba, la línea que lo explica y debajo las
 * opciones de siempre.
 */
function CuentaConDisponibilidad({ cerrando, cerrarSesion, contraida, rol }: Props & { contraida: boolean; rol: string }) {
    const { yo } = useSesion();
    const disponible = useEstoyDisponible();
    const d = useDisponibilidad();
    const colas = d ? listaColas(d.colas) : "";
    const [abierto, setAbierto] = useState(false);
    const estado = disponible === null ? "" : disponible ? ", disponible" : ", no disponible";
    return (
        <PanelEmergente
            titulo="Tu cuenta y disponibilidad"
            lado={contraida ? "end" : "top start"}
            abierto={abierto}
            alCambiar={setAbierto}
            className="w-[min(20rem,calc(100vw-1rem))]"
            disparador={
                <Boton
                    variante="fantasma"
                    className={contraida ? "size-10 justify-center rounded-lg p-0" : "h-auto w-full justify-start gap-2.5 rounded-lg px-2 py-2 text-left font-normal"}
                    aria-label={`Tu cuenta: ${yo.usuario.nombre}${estado}`}
                    title="Tu cuenta"
                >
                    <AvatarDisponible nombre={yo.usuario.nombre} disponible={disponible} />
                    {contraida ? null : (
                        <span className="grid min-w-0 flex-1">
                            <span className="truncate text-[0.8125rem] font-semibold" data-testid="usuario-nombre">
                                {yo.usuario.nombre}
                            </span>
                            {disponible === null ? rol ? <span className="truncate text-xs text-muted-foreground">{rol}</span> : null : <MiEstadoCorto />}
                        </span>
                    )}
                    {contraida ? null : <ChevronDown className="text-muted-foreground" />}
                </Boton>
            }
        >
            <div className="flex min-w-0 items-center gap-3">
                <AvatarDisponible nombre={yo.usuario.nombre} disponible={disponible} tamano="md" />
                <span className="grid min-w-0">
                    <span className="truncate text-sm font-semibold">{yo.usuario.nombre}</span>
                    <span className="truncate text-xs text-muted-foreground">{[rol, colas].filter(Boolean).join(" · ") || yo.usuario.correo}</span>
                </span>
            </div>
            <ControlDisponibilidad />
            <div className="-mx-2 border-t border-border pt-1">
                <MenuEnLinea etiqueta="Tu cuenta">{OpcionesCuenta({ cerrando, cerrarSesion, antes: () => setAbierto(false), sinEncabezado: true })}</MenuEnLinea>
            </div>
        </PanelEmergente>
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
