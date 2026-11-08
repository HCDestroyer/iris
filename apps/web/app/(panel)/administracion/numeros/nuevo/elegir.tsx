"use client";

import { Aviso, Boton, cn, Esqueleto } from "@iris/ui";
import { CircleAlert, CircleCheck, Clock, Code, KeyRound, QrCode, RotateCw, Smartphone, TriangleAlert, User, Webhook, type LucideIcon } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { leerRegistroMeta } from "@/lib/meta/api";
import { etiquetaModo, textoMotivoQr } from "@/lib/meta/presentacion";
import { PERMISO_CONEXION_META } from "@/lib/meta/tipos";
import { useSesion } from "@/lib/sesion";

/**
 * `/administracion/numeros/nuevo`: cómo se conecta el número. Con QR, un número que ya se usa en la
 * app WhatsApp Business (coexistencia, contrato §15.2), si la conexión con Meta lo permite; con
 * los datos de la API, el formulario de siempre (`/nuevo/api`).
 */
export function ElegirConexion() {
    const { yo, puede } = useSesion();
    const registro = useLectura((signal) => leerRegistroMeta(signal));
    const empresa = yo.empresaActiva?.nombre;

    let estadoQr: ReactNode;
    let qrDisponible = false;
    if (registro.estado === "cargando") {
        estadoQr = <Esqueleto className="h-5 w-56" />;
    } else if (registro.estado === "error") {
        estadoQr = (
            <Aviso
                tono="error"
                titulo="No se pudo saber si el QR está disponible"
                accion={
                    <Boton variante="contorno" tamano="sm" onClick={registro.reintentar}>
                        <RotateCw aria-hidden />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(registro.error).titulo}
            </Aviso>
        );
    } else if (registro.datos.disponible) {
        qrDisponible = true;
        estadoQr = (
            <p className="flex items-center gap-1.5 text-sm text-success" data-qr="disponible">
                <CircleCheck aria-hidden className="size-4 shrink-0" />
                Conexión con Meta lista ({etiquetaModo(registro.datos.modo)})
            </p>
        );
    } else {
        estadoQr = (
            <div className="grid gap-1 text-sm" data-qr="no_disponible">
                <p className="flex items-start gap-1.5 text-warning">
                    <CircleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
                    {textoMotivoQr(registro.datos.motivo)}
                </p>
                {puede([PERMISO_CONEXION_META]) && registro.datos.motivo === "sin_configurar" ? (
                    <Link href="/administracion/conexion-meta" className="font-medium text-primary underline-offset-2 hover:underline">
                        Configurar la conexión con Meta
                    </Link>
                ) : registro.datos.motivo === "sin_configurar" ? (
                    <p className="text-muted-foreground">Pídesela a un administrador de Iris.</p>
                ) : null}
            </div>
        );
    }

    return (
        <div className="grid min-w-0 gap-4">
            <EncabezadoPagina titulo="Agregar número" descripcion={`${empresa ? `Se agrega a ${empresa}, la empresa activa. ` : ""}¿Cómo lo vas a conectar?`} />
            <div className="grid gap-4 lg:grid-cols-2">
                <Opcion
                    icono={QrCode}
                    titulo="Conectar con QR"
                    insignia="Número de WhatsApp Business"
                    destacada
                    texto="Para un número que ya usas en la app WhatsApp Business. Sigue funcionando en el teléfono y también llega a Iris."
                    necesitas={[
                        { icono: Smartphone, texto: "El teléfono con la app WhatsApp Business 2.24.17 o más reciente" },
                        { icono: User, texto: "Una cuenta de Facebook para entrar a Meta" },
                        { icono: Clock, texto: "Unos 5 minutos" },
                    ]}
                    estado={estadoQr}
                    accion={
                        qrDisponible ? (
                            <Boton asChild>
                                <Link href="/administracion/numeros/nuevo/qr">
                                    <QrCode aria-hidden />
                                    Conectar con QR
                                </Link>
                            </Boton>
                        ) : (
                            <Boton disabled>
                                <QrCode aria-hidden />
                                Conectar con QR
                            </Boton>
                        )
                    }
                />
                <Opcion
                    icono={Code}
                    titulo="Ingresar datos de la API"
                    insignia="WhatsApp Cloud API"
                    texto="Para un número que no está en ninguna app de WhatsApp y que ya configuraste en tu app de Meta."
                    necesitas={[
                        { icono: KeyRound, texto: "Phone number ID, token permanente y app secret" },
                        { icono: Webhook, texto: "Pegar en Meta la URL del webhook" },
                        { icono: Clock, texto: "Unos 15 minutos" },
                    ]}
                    estado={<p className="text-sm text-muted-foreground">Este número deja de funcionar en la app del teléfono.</p>}
                    accion={
                        <Boton asChild variante="contorno">
                            <Link href="/administracion/numeros/nuevo/api">
                                <Code aria-hidden />
                                Ingresar datos de la API
                            </Link>
                        </Boton>
                    }
                />
            </div>
            <Aviso tono="neutral" icono={TriangleAlert} titulo="No uses conexiones por QR que no son de Meta">
                Las apps que copian WhatsApp Web hacen que Meta bloquee el número. «Conectar con QR» usa el registro oficial de Meta (coexistencia).
            </Aviso>
        </div>
    );
}

function Opcion({
    icono: Icono,
    titulo,
    insignia,
    texto,
    necesitas,
    estado,
    accion,
    destacada = false,
}: {
    icono: LucideIcon;
    titulo: string;
    insignia: string;
    texto: string;
    necesitas: readonly { icono: LucideIcon; texto: string }[];
    estado: ReactNode;
    accion: ReactNode;
    destacada?: boolean;
}) {
    return (
        <section aria-label={titulo} className={cn("surface-card flex min-w-0 flex-col gap-4 p-4 sm:p-6", destacada && "border-primary/40")}>
            <span className="grid size-12 place-items-center rounded-full bg-primary/10 text-primary">
                <Icono aria-hidden className="size-6" />
            </span>
            <div className="grid gap-1.5">
                <h2 className="flex flex-wrap items-center gap-2 text-lg font-semibold">
                    {titulo}
                    <span className={cn("rounded-full px-2 py-0.5 text-xs font-medium", destacada ? "bg-primary/10 text-primary" : "bg-background-secondary text-muted-foreground")}>{insignia}</span>
                </h2>
                <p className="text-sm text-muted-foreground">{texto}</p>
            </div>
            <div className="grid gap-2">
                <p className="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">Necesitas</p>
                <ul className="grid gap-2 text-sm">
                    {necesitas.map((n) => (
                        <li key={n.texto} className="flex items-start gap-2">
                            <n.icono aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                            {n.texto}
                        </li>
                    ))}
                </ul>
            </div>
            <div className="mt-auto grid gap-3">
                {estado}
                <div>{accion}</div>
            </div>
        </section>
    );
}
