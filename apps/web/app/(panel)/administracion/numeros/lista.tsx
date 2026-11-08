"use client";

import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, cn, Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { BookOpen, Building2, CalendarClock, Cloud, Code, Eye, History, KeyRound, MessageCircle, Plus, Power, QrCode, Share2, SlidersHorizontal, Smartphone } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { EstadoConexion } from "@/componentes/numeros/estado-conexion";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { avisoEstado } from "@/lib/avisos";
import { CLASE_PILDORA, formatearNumero, nombreUso, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import { resumirFranjas } from "@/lib/horarios/franjas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { diasSinTelefono, etiquetaTipoConexion, plazoRestante, textoDesconexion } from "@/lib/meta/presentacion";
import { PERMISO_CONEXION_META } from "@/lib/meta/tipos";
import { activarCanal, leerCanales } from "@/lib/numeros/api";
import { presentarConexion } from "@/lib/numeros/conexion";
import { enlaceGuia } from "@/lib/numeros/guia";
import { deLaEmpresa, listaUsos, textoCompartido } from "@/lib/numeros/lista";
import type { Canal } from "@/lib/numeros/tipos";
import { useSesion } from "@/lib/sesion";

/**
 * Lo que muestra una fila. Con `mensajeria.configurar` viene de `GET /canales` (conexión, tipo,
 * préstamos); sin él, de `GET /conversaciones/canales` (sin datos de Meta).
 */
type Fila = Pick<Schema<"NumeroConversaciones">, "id" | "nombre" | "numero" | "usos" | "avisosEmpresa" | "colaId" | "horarioId" | "activo" | "prestadoPor"> & {
    meta: Pick<Canal, "conexion" | "tipoConexion" | "ultimoEcoEn" | "conectadoEn" | "desconectadoMotivo" | "compartidoCon" | "sincronizacion"> | null;
};

const AREAS = cn(
    "grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-3 [grid-template-areas:'num_est''uso_uso''det_det''emp_emp''acc_acc']",
    // Pistas que no dependen del contenido (`minmax(0,…)` y acciones de ancho fijo): cada fila es su
    // propia rejilla y así las columnas quedan alineadas con la cabecera.
    "lg:grid-cols-[minmax(0,1.2fr)_minmax(0,0.7fr)_minmax(0,0.9fr)_minmax(0,1.1fr)_minmax(0,1fr)_9.5rem] lg:items-center lg:gap-y-0",
    "lg:[grid-template-areas:'num_uso_det_est_emp_acc']",
);
const AREAS_SIN_CONEXION = cn(
    "grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-3 [grid-template-areas:'num_num''uso_uso''det_det''emp_emp''acc_acc']",
    "lg:grid-cols-[minmax(0,1.2fr)_minmax(0,0.7fr)_minmax(0,0.9fr)_minmax(0,1fr)_9.5rem] lg:items-center lg:gap-y-0",
    "lg:[grid-template-areas:'num_uso_det_emp_acc']",
);

/**
 * «Números de WhatsApp» (Administración): cada número con su uso, cola y horario; con
 * `mensajeria.configurar`, su conexión con Meta (calculada en el servidor), si es por QR o por la
 * API, y sus credenciales; con `canales.configurar`, «Configurar» y «Compartir» con otras empresas
 * (contrato §15.3). Un número prestado por otra empresa dice de quién es y solo se ve su detalle.
 */
export function ListaNumeros() {
    const { yo, puede } = useSesion();
    const credenciales = puede(["mensajeria.configurar"]);
    const configurar = puede(["canales.configurar"]);
    const conexionMeta = puede([PERMISO_CONEXION_META]);
    // Colas y horarios se leen con `conversaciones.ver`; sin él, esas columnas dicen «—».
    const verBandeja = puede(["conversaciones.ver"]);
    const empresaActiva = yo.empresaActiva?.id ?? null;
    const clave = useClaveIdempotencia();
    const [errorAccion, setErrorAccion] = useState<{ nombre: string; error: unknown } | null>(null);
    const [activando, setActivando] = useState<number | null>(null);
    const lectura = useLectura(async (signal) => {
        const [numeros, colas, horarios] = await Promise.all([
            credenciales
                ? leerCanales(signal).then((cs) =>
                      deLaEmpresa(cs, empresaActiva).map(
                          (c): Fila => ({
                              ...c,
                              prestadoPor: c.prestadoPor,
                              // El préstamo trae sus usos y su cola (§15.3).
                              usos: c.prestamo?.usos ?? c.usos,
                              colaId: c.prestamo ? c.prestamo.colaId : c.colaId,
                              meta: { conexion: c.conexion, tipoConexion: c.tipoConexion, ultimoEcoEn: c.ultimoEcoEn, conectadoEn: c.conectadoEn, desconectadoMotivo: c.desconectadoMotivo, compartidoCon: c.compartidoCon, sincronizacion: c.sincronizacion },
                          }),
                      ),
                  )
                : resolver(api().GET("/api/v1/mensajeria/conversaciones/canales", { signal })).then((r) => (r.data ?? []).map((c): Fila => ({ ...c, meta: null }))),
            verBandeja ? resolver(api().GET("/api/v1/mensajeria/colas", { signal })).then((r) => r.data ?? []) : Promise.resolve(null),
            verBandeja ? resolver(api().GET("/api/v1/mensajeria/horarios", { signal })).then((r) => r.data ?? []) : Promise.resolve(null),
        ]);
        return { numeros, colas, horarios };
    });

    async function activar(f: Fila) {
        setErrorAccion(null);
        setActivando(f.id);
        try {
            await activarCanal(f.id, clave.actual());
            clave.renovar();
            avisar({ titulo: avisoEstado(f.nombre, true) });
            lectura.reintentar();
        } catch (e) {
            clave.trasError(e);
            setErrorAccion({ nombre: f.nombre, error: e });
        } finally {
            setActivando(null);
        }
    }

    const agregar = credenciales ? (
        <Boton asChild>
            <Link href="/administracion/numeros/nuevo">
                <Plus aria-hidden />
                Agregar número
            </Link>
        </Boton>
    ) : null;

    let contenido: ReactNode;
    let descripcion = "Números conectados por la API oficial de Meta (WhatsApp Cloud API) o con QR";
    if (lectura.estado === "cargando") {
        contenido = (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando números…
                </span>
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los números" />;
    } else if (lectura.datos.numeros.length === 0) {
        descripcion = "Conecta un número para recibir y contestar WhatsApp en la bandeja";
        contenido = (
            <Tarjeta>
                <EstadoVacio
                    icono={MessageCircle}
                    titulo="Todavía no hay números de WhatsApp"
                    descripcion="Conéctalo con QR si ya lo usas en la app WhatsApp Business, o con los datos de la API de tu app de Meta."
                    accion={
                        <div className="flex flex-wrap justify-center gap-2">
                            <Boton asChild variante="contorno">
                                <Link href={enlaceGuia()}>
                                    <BookOpen aria-hidden />
                                    Ver la guía
                                </Link>
                            </Boton>
                            {agregar}
                        </div>
                    }
                />
            </Tarjeta>
        );
    } else {
        const { numeros, colas, horarios } = lectura.datos;
        const prestados = numeros.filter((n) => n.prestadoPor);
        const propios = numeros.filter((n) => !n.prestadoPor);
        const activos = propios.filter((n) => n.activo).length;
        const inactivos = propios.length - activos;
        const compartidos = propios.filter((n) => (n.meta?.compartidoCon ?? 0) > 0).length;
        const empresa = yo.empresaActiva?.nombre;
        descripcion = [
            `${activos} ${activos === 1 ? "activo" : "activos"}${inactivos ? ` y ${inactivos} ${inactivos === 1 ? "desactivado" : "desactivados"}` : ""}${empresa ? ` de ${empresa}` : ""}`,
            prestados.length ? `${prestados.length} ${prestados.length === 1 ? "prestado" : "prestados"} por otra empresa` : null,
            compartidos ? `${compartidos} ${compartidos === 1 ? "compartido" : "compartidos"} con otras empresas` : null,
        ]
            .filter(Boolean)
            .join(" · ");
        const conConexion = credenciales;
        contenido = (
            <div className="grid gap-3">
                {errorAccion ? (
                    <Aviso tono="error" titulo={`No se pudo activar «${errorAccion.nombre}»`}>
                        {mensajeDeError(errorAccion.error).titulo}
                    </Aviso>
                ) : null}
                {prestados.map((n) => (
                    <Aviso key={n.id} tono="info" icono={Building2} titulo={`${n.nombre} es de ${n.prestadoPor?.nombre}`} data-aviso-prestado={n.id}>
                        {empresa ?? "Tu empresa"} lo puede usar para {listaUsos(n.usos ?? [], nombreUso)}. Las credenciales, el horario y la conexión los administra {n.prestadoPor?.nombre}.
                    </Aviso>
                ))}
                <div className="overflow-hidden lg:rounded-xl lg:border lg:border-border lg:bg-card">
                    <div
                        aria-hidden
                        className={cn(
                            conConexion ? AREAS : AREAS_SIN_CONEXION,
                            // Después de las áreas: «hidden» gana al «grid» de las áreas en el teléfono.
                            "hidden border-b border-border px-4 py-2.5 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase lg:grid",
                        )}
                    >
                        <span className="[grid-area:num]">Número</span>
                        <span className="[grid-area:uso]">Uso</span>
                        <span className="[grid-area:det]">Cola y horario</span>
                        {conConexion ? <span className="[grid-area:est]">Conexión</span> : null}
                        <span className="[grid-area:emp]">Empresas</span>
                        <span className="text-right [grid-area:acc]">Acciones</span>
                    </div>
                    <ul aria-label="Números de WhatsApp" className="grid gap-3 lg:gap-0">
                        {numeros.map((n) => (
                            <FilaNumero
                                key={n.id}
                                n={n}
                                colas={colas}
                                horarios={horarios}
                                conConexion={conConexion}
                                permisos={{ credenciales, configurar }}
                                activando={activando === n.id}
                                activar={() => void activar(n)}
                            />
                        ))}
                    </ul>
                </div>
            </div>
        );
    }

    const hayNumeros = lectura.estado === "listo" && lectura.datos.numeros.length > 0;
    return (
        <>
            <EncabezadoPagina
                titulo="Números de WhatsApp"
                descripcion={descripcion}
                acciones={
                    <div className="flex w-full flex-wrap gap-2 sm:w-auto [&>*]:flex-1 sm:[&>*]:flex-none">
                        {configurar ? (
                            <Boton asChild variante="secundario">
                                <Link href="/administracion/horarios">
                                    <CalendarClock aria-hidden />
                                    Horarios
                                </Link>
                            </Boton>
                        ) : null}
                        {conexionMeta ? (
                            <Boton asChild variante="contorno">
                                <Link href="/administracion/conexion-meta">
                                    <Cloud aria-hidden />
                                    Conexión con Meta
                                </Link>
                            </Boton>
                        ) : null}
                        {hayNumeros ? agregar : null}
                    </div>
                }
            />
            {contenido}
        </>
    );
}

function FilaNumero({
    n,
    colas,
    horarios,
    conConexion,
    permisos,
    activando,
    activar,
}: {
    n: Fila;
    colas: Schema<"ColaConversaciones">[] | null;
    horarios: Schema<"Horario">[] | null;
    conConexion: boolean;
    permisos: { credenciales: boolean; configurar: boolean };
    activando: boolean;
    activar: () => void;
}) {
    const horario = horarios?.find((h) => h.id === n.horarioId);
    const cola = colas?.find((x) => x.id === n.colaId);
    const m = n.meta;
    const prestado = Boolean(n.prestadoPor);
    const qr = m?.tipoConexion === "qr";
    const desconectado = m?.conexion === "desconectado";
    const dias = qr && !desconectado && m ? diasSinTelefono(m.ultimoEcoEn, m.conectadoEn) : null;
    const historialVigente = qr && m?.sincronizacion ? !plazoRestante(m.sincronizacion.plazo).vencido : false;
    const pide = m ? presentarConexion(m.conexion).credenciales : false;
    return (
        <li
            data-numero={n.id}
            data-tipo-conexion={m?.tipoConexion}
            className={cn("rounded-xl border border-border bg-card p-4 lg:rounded-none lg:border-0 lg:border-b lg:px-4 lg:py-3 lg:last:border-b-0", conConexion ? AREAS : AREAS_SIN_CONEXION)}
        >
            <div className={cn("grid min-w-0 gap-0.5 [grid-area:num]", !n.activo && "text-muted-foreground")}>
                <span className="truncate font-semibold">{n.nombre}</span>
                {n.avisosEmpresa ? <span className="hidden h-5 w-fit items-center rounded-full bg-info/10 px-2 text-[0.6875rem] font-semibold whitespace-nowrap text-info lg:inline-flex">Avisos de la empresa</span> : null}
                <span className="truncate font-mono text-sm text-muted-foreground">{formatearNumero(n.numero)}</span>
            </div>
            <div className="flex flex-wrap content-start gap-1 [grid-area:uso]">
                {n.avisosEmpresa ? <span className="inline-flex h-5 items-center rounded-full bg-info/10 px-2 text-[0.6875rem] font-semibold whitespace-nowrap text-info lg:hidden">Avisos de la empresa</span> : null}
                {(n.usos ?? []).map((u) => (
                    <span key={u} className={cn("inline-flex h-5 items-center rounded-full px-2 text-[0.6875rem] font-semibold", CLASE_PILDORA[tonoDeCanal({ usos: [u] })])}>
                        {nombreUso(u)}
                    </span>
                ))}
            </div>
            <div className={cn("min-w-0 flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground [grid-area:det] lg:grid lg:gap-0.5", n.activo ? "flex" : "hidden lg:grid")}>
                <span className="min-w-0 lg:text-foreground">
                    <span className="lg:hidden">Cola: </span>
                    {!n.activo || !colas ? <Nada /> : (cola?.nombre ?? <span className="text-muted-foreground">Sin cola</span>)}
                </span>
                <span className="min-w-0">{prestado ? `De ${n.prestadoPor?.nombre}` : !n.activo || !horarios ? <Nada /> : horario ? resumirFranjas(horario.franjas) : "Siempre abierto"}</span>
            </div>
            {m ? (
                <div className="grid min-w-0 justify-items-end gap-1 text-right [grid-area:est] lg:justify-items-start lg:text-left">
                    {dias !== null ? (
                        <span className="inline-flex items-start gap-1.5 text-sm font-medium text-warning" data-aviso-telefono>
                            <Smartphone aria-hidden className="mt-0.5 size-4 shrink-0" />
                            Teléfono sin actividad {dias} días
                        </span>
                    ) : (
                        <EstadoConexion conexion={m.conexion} />
                    )}
                    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" data-tipo>
                        {qr ? <QrCode aria-hidden className="size-3.5" /> : <Code aria-hidden className="size-3.5" />}
                        {etiquetaTipoConexion(m.tipoConexion)}
                    </span>
                    {desconectado ? <span className="text-xs text-muted-foreground">{textoDesconexion(m.desconectadoMotivo)}</span> : null}
                    {dias !== null ? <span className="text-xs text-muted-foreground">Meta lo desconecta a los 14 días sin uso. Abre la app en el teléfono.</span> : null}
                </div>
            ) : null}
            <div className="min-w-0 text-sm [grid-area:emp]">
                {prestado ? (
                    <span className="inline-flex items-center gap-1.5 rounded-full border border-info/30 bg-info/10 px-2 py-0.5 text-xs font-semibold text-info" data-insignia="prestado">
                        <Building2 aria-hidden className="size-3.5" />
                        Prestado por {n.prestadoPor?.nombre}
                    </span>
                ) : permisos.configurar && n.activo && m ? (
                    <Link href={`/administracion/numeros/${n.id}/compartir`} className="inline-flex items-start gap-1.5 font-medium text-primary underline-offset-2 hover:underline" data-insignia={m.compartidoCon > 0 ? "compartido" : "compartir"}>
                        <Share2 aria-hidden className="mt-0.5 size-4 shrink-0" />
                        {m.compartidoCon > 0 ? textoCompartido(m.compartidoCon) : "Compartir"}
                        <span className="sr-only"> {n.nombre}</span>
                    </Link>
                ) : m && m.compartidoCon > 0 ? (
                    <span className="inline-flex items-center gap-1.5 text-muted-foreground" data-insignia="compartido">
                        <Share2 aria-hidden className="size-4" />
                        {textoCompartido(m.compartidoCon)}
                    </span>
                ) : (
                    <Nada />
                )}
            </div>
            <div className="grid grid-cols-2 gap-2 [grid-area:acc] lg:flex lg:flex-col lg:items-end [&>*]:w-full lg:[&>*]:w-auto">
                {prestado ? (
                    <Boton asChild variante="contorno" tamano="sm">
                        <Link href={`/administracion/numeros/${n.id}/prestamo`} aria-label={`Ver detalle de ${n.nombre}`}>
                            <Eye aria-hidden />
                            Ver detalle
                        </Link>
                    </Boton>
                ) : (
                    <>
                        {permisos.configurar && n.activo ? (
                            <Boton asChild variante="contorno" tamano="sm">
                                <Link href={`/administracion/numeros/${n.id}/configurar`} aria-label={`Configurar ${n.nombre}`}>
                                    <SlidersHorizontal aria-hidden />
                                    Configurar
                                </Link>
                            </Boton>
                        ) : null}
                        {permisos.credenciales && !n.activo ? (
                            <Boton variante="contorno" tamano="sm" aria-label={`Activar ${n.nombre}`} disabled={activando} onClick={activar}>
                                <Power aria-hidden />
                                {activando ? "Activando…" : "Activar"}
                            </Boton>
                        ) : null}
                        {permisos.credenciales && desconectado ? (
                            <Boton asChild variante="contorno" tamano="sm" className="border-error/60 text-error">
                                <Link href="/administracion/numeros/nuevo/qr" aria-label={`Reconectar ${n.nombre}`}>
                                    <QrCode aria-hidden />
                                    Reconectar
                                </Link>
                            </Boton>
                        ) : permisos.credenciales && qr && historialVigente ? (
                            <Boton asChild variante="contorno" tamano="sm">
                                <Link href={`/administracion/numeros/${n.id}/conectado`} aria-label={`Traer historial de ${n.nombre}`}>
                                    <History aria-hidden />
                                    Historial
                                </Link>
                            </Boton>
                        ) : null}
                        {permisos.credenciales && !qr ? (
                            <Boton asChild variante="contorno" tamano="sm" className={cn(pide && "border-error/60 text-error")}>
                                <Link href={`/administracion/numeros/${n.id}/credenciales`} aria-label={`Credenciales de ${n.nombre}`}>
                                    <KeyRound aria-hidden />
                                    Credenciales
                                </Link>
                            </Boton>
                        ) : null}
                    </>
                )}
            </div>
        </li>
    );
}

function Nada() {
    return (
        <span className="text-muted-foreground">
            <span aria-hidden>—</span>
            <span className="sr-only">Sin dato</span>
        </span>
    );
}
