"use client";

import { Aviso, Boton, GrupoSegmentado, cn, type OpcionSegmentada } from "@iris/ui";
import { APARIENCIAS, estadoDosPasos, lineaPerfil, totalPorEnviar, type Apariencia } from "@iris/sync/campo";
import { Building2, ChevronRight, CloudUpload, KeyRound, LogOut, Moon, Package, RefreshCw, ShieldCheck, Smartphone, Sun, type LucideIcon } from "lucide-react";
import { useTheme } from "next-themes";
import { useState, useSyncExternalStore, type ReactNode } from "react";
import { ErrorApi } from "@/lib/api/errores";
import { CambiarContrasena, DosPasos, useCuenta } from "./cuenta";
import { useCampo } from "./contexto";

/**
 * «Yo» (diseño aprobado por el PM, nov 2026): el nombre y «Rol · Empresa» de `GET /yo`; Apariencia
 * (Teléfono · Claro · Oscuro, en este navegador con `next-themes`, sin parpadeo al cargar);
 * Seguridad (cambiar contraseña y verificación en dos pasos, cada una en su pantalla); Este teléfono
 * («Mi bodega», «Por enviar», actualizar el día, cambiar de empresa) y «Cerrar sesión» (con lo
 * pendiente: enviar y salir, o salir conservándolo). Como la app nativa.
 */
export function Yo(props: { nombreEmpresa: string | undefined; pedirEmpresa: () => void; bajarPaquete: () => Promise<void> }) {
    const { vista } = useCampo();
    const seccion = vista.tipo === "yo" ? vista.seccion : undefined;
    if (seccion === "contrasena") {
        return <CambiarContrasena />;
    }
    if (seccion === "dos_pasos") {
        return <DosPasos />;
    }
    return <Perfil {...props} />;
}

const ICONOS: Record<Apariencia, LucideIcon> = { telefono: Smartphone, claro: Sun, oscuro: Moon };
/** Valores de `next-themes` (`iris-tema` en el navegador, el mismo del panel). */
const TEMA: Record<Apariencia, "system" | "light" | "dark"> = { telefono: "system", claro: "light", oscuro: "dark" };
const OPCIONES: readonly OpcionSegmentada<Apariencia>[] = APARIENCIAS.map((a) => ({ valor: a.valor, etiqueta: a.texto, icono: ICONOS[a.valor] }));
const sinSuscripcion = () => () => undefined;

function aparienciaDe(tema: string | undefined): Apariencia | undefined {
    return tema === "light" ? "claro" : tema === "dark" ? "oscuro" : tema === "system" ? "telefono" : undefined;
}

function Seccion({ titulo, children }: { titulo: string; children: ReactNode }) {
    return (
        <section className="grid gap-2">
            <h2 className="px-1.5 text-[0.8125rem] font-semibold tracking-[0.06em] text-muted-foreground uppercase">{titulo}</h2>
            {children}
        </section>
    );
}

/** Tarjeta de 28 px con sus filas separadas por una raya. */
function Grupo({ children, testId }: { children: ReactNode; testId?: string }) {
    return (
        <div data-testid={testId} className="overflow-hidden rounded-[1.75rem] border border-border bg-[var(--campo-panel)] [&>*+*]:border-t [&>*+*]:border-border">
            {children}
        </div>
    );
}

/** Fila de ajuste: icono, título y detalle, y a la derecha un valor o un punto; toda la fila es un botón. */
function Fila({ icono: Icono, titulo, detalle, derecha, etiqueta, alPresionar }: { icono: LucideIcon; titulo: string; detalle?: string | null; derecha?: ReactNode; etiqueta?: string; alPresionar: () => void }) {
    return (
        <Boton
            variante="fantasma"
            aria-label={etiqueta ?? titulo}
            className="h-auto min-h-14 w-full justify-start gap-3.5 rounded-none px-4 py-3 text-left font-normal text-foreground md:h-auto [&_svg]:size-[1.375rem]"
            onClick={alPresionar}
        >
            <Icono aria-hidden strokeWidth={1.8} />
            <span className="grid min-w-0 flex-1">
                <span className="text-base font-medium">{titulo}</span>
                {detalle ? <span className="text-[0.8125rem] whitespace-normal text-muted-foreground">{detalle}</span> : null}
            </span>
            {derecha}
            <ChevronRight aria-hidden className="text-muted-foreground" />
        </Boton>
    );
}

/** Punto de estado discreto + texto (verde «Activa», neutro «Apagada»): nunca solo color. */
function Punto({ tono, texto }: { tono: "success" | "neutral"; texto: string }) {
    return (
        <span data-testid="estado-dos-pasos" className={cn("inline-flex items-center gap-1.5 text-[0.9375rem] font-medium", tono === "success" ? "text-success" : "text-muted-foreground")}>
            <span aria-hidden className={cn("size-2 rounded-full", tono === "success" ? "bg-success" : "border-[1.5px] border-current")} />
            {texto}
        </span>
    );
}

function Perfil({ nombreEmpresa, pedirEmpresa, bajarPaquete }: { nombreEmpresa: string | undefined; pedirEmpresa: () => void; bajarPaquete: () => Promise<void> }) {
    const { estado, sesion, motor, cerrarSesion, notificar, navegar } = useCampo();
    const { theme, setTheme } = useTheme();
    const montado = useSyncExternalStore(sinSuscripcion, () => true, () => false);
    const cuenta = useCuenta();
    const [confirmarEmpresa, setConfirmarEmpresa] = useState(false);
    const [confirmarSalida, setConfirmarSalida] = useState(false);
    const [saliendo, setSaliendo] = useState(false);
    const porEnviar = estado.pendientes + estado.rechazos.length + estado.esperandoAdjuntos.length + estado.subidas.pendientes;
    const enCola = totalPorEnviar(estado);
    const conSenal = estado.conexion === "en_linea" || estado.conexion === "desconocida";
    const yo = cuenta.estado === "listo" ? cuenta.datos : null;
    const linea = lineaPerfil(yo?.usuario.roles, yo?.empresaActiva?.nombre ?? nombreEmpresa);
    const dosPasos = estadoDosPasos(yo ? yo.usuario.mfaActivo : null);
    const sinLeer = cuenta.estado === "error" && !(cuenta.error instanceof ErrorApi && cuenta.error.estado === 0);

    const salir = async (conservarPendientes: boolean) => {
        setSaliendo(true);
        try {
            await cerrarSesion({ conservarPendientes });
        } catch {
            notificar({ tono: "error", texto: "No se pudo cerrar la sesión en el teléfono. Vuelve a intentarlo." });
            setSaliendo(false);
        }
    };

    const enviarYSalir = async () => {
        setSaliendo(true);
        try {
            await motor.sincronizar();
        } catch {
            // Lo que no salió se informa abajo.
        }
        const e = motor.obtenerEstado();
        const quedan = e.pendientes + e.rechazos.length + e.esperandoAdjuntos.length + e.subidas.pendientes;
        if (quedan === 0) {
            await salir(false);
            return;
        }
        setSaliendo(false);
        notificar({ tono: "warning", texto: `No se pudo enviar todo: quedan ${quedan}. Revisa «Por enviar» o sal conservándolos.` });
    };

    return (
        <section aria-labelledby="titulo-yo" className="grid gap-5">
            <div className="grid gap-1 px-1.5" data-testid="perfil-yo">
                <h1 id="titulo-yo" className="text-[2rem] leading-tight font-bold tracking-[-0.02em]">
                    {sesion.nombre}
                </h1>
                {linea ? <p className="text-base text-muted-foreground">{linea}</p> : null}
            </div>

            <Seccion titulo="Apariencia">
                <div className="rounded-[1.75rem] border border-border bg-[var(--campo-panel)] p-3">
                    <GrupoSegmentado
                        etiqueta="Apariencia"
                        opciones={OPCIONES}
                        valor={montado ? aparienciaDe(theme) : undefined}
                        alCambiar={(a) => {
                            setTheme(TEMA[a]);
                            notificar({ tono: "success", texto: a === "telefono" ? "Iris sigue el tema del teléfono." : a === "claro" ? "Iris en modo claro." : "Iris en modo oscuro." });
                        }}
                        className="grid w-full grid-cols-3 gap-1 rounded-full bg-[var(--campo-panel-2)] p-1"
                        claseOpcion="h-12 rounded-full px-2 text-[0.9375rem] data-selected:bg-[var(--campo-panel)] data-selected:text-foreground data-selected:shadow-[var(--campo-sombra)] dark:data-selected:bg-[var(--campo-lienzo)] [&_svg]:size-[1.125rem]"
                    />
                </div>
            </Seccion>

            <Seccion titulo="Seguridad">
                <Grupo testId="grupo-seguridad">
                    <Fila icono={KeyRound} titulo="Cambiar contraseña" alPresionar={() => navegar({ tipo: "yo", seccion: "contrasena" })} />
                    <Fila
                        icono={ShieldCheck}
                        titulo="Verificación en dos pasos"
                        etiqueta={dosPasos ? `Verificación en dos pasos: ${dosPasos.texto}` : "Verificación en dos pasos"}
                        detalle={sinLeer ? "No se pudo leer si está activa." : dosPasos?.tono === "success" ? "Te pide un código al entrar" : dosPasos ? "Recomendada: protege tu cuenta" : null}
                        derecha={dosPasos ? <Punto tono={dosPasos.tono} texto={dosPasos.texto} /> : null}
                        alPresionar={() => navegar({ tipo: "yo", seccion: "dos_pasos" })}
                    />
                </Grupo>
            </Seccion>

            <Seccion titulo="Este teléfono">
                <Grupo>
                    <Fila icono={Package} titulo="Mi bodega" alPresionar={() => navegar({ tipo: "bodega" })} />
                    {/* «Por enviar» se abre desde aquí (y desde la píldora de estado). */}
                    <Fila
                        icono={CloudUpload}
                        titulo="Por enviar"
                        etiqueta={enCola > 0 ? `Por enviar (${enCola})` : "Por enviar"}
                        derecha={
                            enCola > 0 ? (
                                <span className="text-[0.9375rem] font-bold text-warning tabular-nums">{enCola}</span>
                            ) : (
                                <span className="text-[0.9375rem] text-muted-foreground">Todo enviado</span>
                            )
                        }
                        alPresionar={() => navegar({ tipo: "pendientes" })}
                    />
                    <Fila icono={RefreshCw} titulo="Actualizar órdenes del día" alPresionar={() => void bajarPaquete()} />
                    {sesion.empresas.length > 1 ? (
                        <Fila icono={Building2} titulo="Cambiar de empresa" detalle={nombreEmpresa ?? null} alPresionar={() => (porEnviar > 0 ? setConfirmarEmpresa(true) : pedirEmpresa())} />
                    ) : null}
                </Grupo>
                {sesion.empresas.length > 1 && confirmarEmpresa ? (
                    <Aviso tono="warning" titulo={`Quedan ${porEnviar} cosas por enviar en ${nombreEmpresa ?? "esta empresa"}`}>
                        <p>Se enviarán cuando vuelvas a esta empresa. ¿Cambiar de todos modos?</p>
                        <div className="mt-3 flex flex-wrap justify-end gap-2">
                            <Boton variante="fantasma" tamano="lg" onClick={() => setConfirmarEmpresa(false)}>
                                Quedarme
                            </Boton>
                            <Boton variante="secundario" tamano="lg" onClick={pedirEmpresa}>
                                Cambiar de todos modos
                            </Boton>
                        </div>
                    </Aviso>
                ) : null}
            </Seccion>

            {confirmarSalida && porEnviar > 0 ? (
                <Aviso tono="warning" titulo={`Tienes ${porEnviar} ${porEnviar === 1 ? "cambio" : "cambios"} sin enviar`}>
                    <p>
                        {conSenal
                            ? "Envíalos antes de salir, o sal y consérvalos: se guardan sin datos de clientes y se envían cuando vuelvas a entrar."
                            : "Sin señal no se pueden enviar. Si sales, se conservan sin datos de clientes y se envían cuando vuelvas a entrar."}
                    </p>
                    <div className="mt-3 flex flex-wrap justify-end gap-2">
                        <Boton variante="fantasma" tamano="lg" disabled={saliendo} onClick={() => setConfirmarSalida(false)}>
                            Quedarme
                        </Boton>
                        <Boton variante="secundario" tamano="lg" disabled={saliendo} onClick={() => void salir(true)}>
                            Salir y conservarlos
                        </Boton>
                        {conSenal ? (
                            <Boton tamano="lg" disabled={saliendo} onClick={() => void enviarYSalir()}>
                                {saliendo ? "Enviando…" : "Enviar y salir"}
                            </Boton>
                        ) : null}
                    </div>
                </Aviso>
            ) : (
                <Boton
                    variante="contorno"
                    tamano="lg"
                    className="h-[3.25rem] w-full rounded-full border-border bg-[var(--campo-panel)] text-base text-error hover:text-error md:h-[3.25rem]"
                    disabled={saliendo}
                    onClick={() => (porEnviar > 0 ? setConfirmarSalida(true) : void salir(false))}
                >
                    <LogOut aria-hidden />
                    {saliendo ? "Cerrando sesión…" : "Cerrar sesión"}
                </Boton>
            )}
        </section>
    );
}
