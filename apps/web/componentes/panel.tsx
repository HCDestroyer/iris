"use client";

import { Aviso, Boton, cn, EncabezadoPanel, Esqueleto, LogoIris, MarcaIris, MenuIsla, ProveedorAvisosEmergentes, ProveedorManual, ProveedorRutas, type GrupoIsla } from "@iris/ui";
import { RotateCw } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { Suspense, useCallback, useEffect, useId, useState, useSyncExternalStore, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { limpiarClientesRecientes } from "@/lib/clientes/selector";
import { limpiarBorradores } from "@/lib/conversaciones/borradores";
import { useNoLeidosConversaciones } from "@/lib/conversaciones/contador";
import { ProveedorDisponibilidad, useDisponibilidad } from "@/lib/conversaciones/disponibilidad";
import { requiereCambio, rutaCambiarContrasena, suscribirCambioRequerido } from "@/lib/cuenta/sesion";
import { guardarEmpresa, limpiarEmpresa, suscribirProblemasEmpresa } from "@/lib/empresa";
import { claveManualDeRuta } from "@/lib/manual/claves";
import { entradaActiva, menuAgrupado, repartoTelefono, type GrupoVisible } from "@/lib/menu";
import { ProveedorSesion, useSesion, useYo } from "@/lib/sesion";
import { contadorDe, ProveedorResumenTickets, useResumenTickets, type ContextoResumen } from "@/lib/tickets/resumen";
import { BarraInferior } from "./barra-inferior";
import { ControlDisponibilidad } from "./conversaciones/disponibilidad";
import { BuscadorGlobal } from "./buscador-global";
import { CuentaEncabezado, MenuCuentaLista, ProveedorCuenta } from "./cuenta/menu-usuario";
import { BotonAvisos } from "./barra-superior";
import { AccesosRapidos, EstadoConexiones } from "./marco/encabezado";
import { BotonChatEquipo, PanelChatEquipo } from "./chat/panel-chat";
import { ProveedorChatEquipo, useChatEquipo } from "./chat/proveedor";
import { MigasPanel, ProveedorMigas } from "./migas-panel";
import { ErrorLectura, SinEmpresa } from "./error-lectura";
import { MisVistas, Navegacion } from "./navegacion";
import { AvisoDatosSinEmpresa, ElegirEmpresa, SelectorEmpresa } from "./selector-empresa";
import { SelectorTema } from "./selector-tema";
import { DialogoReautenticar } from "@/componentes/cuenta/dialogo-reautenticar";
import { AvisoLlamada, BarraLlamada, NotaLlamada } from "./voz/llamada";
import { AvisoTelefono, BotonTelefono } from "./telefono/indicador";
import { BarraTelefono, CapaTelefono } from "./telefono/llamada";
import { ProveedorTelefono } from "./telefono/proveedor";
import { ProveedorCentralita } from "./voz/centralita";
import { ProveedorVoz, useVoz } from "./voz/proveedor";
import { BotonRecibir, ControlRecibir } from "./voz/recibir";

/**
 * Layout autenticado: lee `GET /yo` y, solo si salió bien, arma el menú con sus permisos.
 * También gobierna la empresa activa: la elegida va en `X-Iris-Empresa` (lib/empresa.ts) y los
 * 422/403 de empresa de cualquier petición llegan aquí.
 */
/** Rutas administrativas: la API no exige empresa activa para ellas (i1.md). */
const RUTAS_SIN_EMPRESA = ["/usuarios", "/administracion", "/cuenta"];
/** Excepciones dentro de las administrativas: buzones y plantillas son de la empresa activa (o comunes). */
const RUTAS_CON_EMPRESA = ["/administracion/buzones", "/administracion/plantillas"];
const enRuta = (ruta: string, r: string) => ruta === r || ruta.startsWith(`${r}/`);

export function Panel({ children }: { children: ReactNode }) {
    const yo = useYo();
    const ruta = usePathname();
    const administrativa = RUTAS_SIN_EMPRESA.some((r) => enRuta(ruta, r)) && !RUTAS_CON_EMPRESA.some((r) => enRuta(ruta, r));
    const { refrescar } = yo;
    const [pedida, setPedida] = useState(false);
    const [noPermitida, setNoPermitida] = useState(false);

    useEffect(
        () =>
            suscribirProblemasEmpresa((problema) => {
                if (problema === "empresa_no_permitida") {
                    // La guardada ya no es suya (o no hay conector): se olvida y se vuelve a preguntar.
                    limpiarEmpresa();
                    setNoPermitida(true);
                } else if (problema === "empresa_requerida") {
                    setPedida(true);
                }
                void refrescar();
            }),
        [refrescar],
    );

    const elegirEmpresa = useCallback(
        (id: number) => {
            guardarEmpresa(id);
            setPedida(false);
            setNoPermitida(false);
            void refrescar();
        },
        [refrescar],
    );
    const pedirEmpresa = useCallback(() => setPedida(true), []);

    // Contraseña temporal (tras un reinicio de MFA): `GET /yo` lo dice y cualquier otra petición
    // responde 403 `cambio_contrasena_requerido`. En los dos casos solo queda cambiarla.
    const router = useRouter();
    const [cambioRequerido, setCambioRequerido] = useState(false);
    useEffect(() => suscribirCambioRequerido(() => setCambioRequerido(true)), []);
    const debeCambiar = cambioRequerido || (yo.estado === "listo" && requiereCambio(yo.datos.usuario));
    useEffect(() => {
        if (debeCambiar) {
            router.replace(rutaCambiarContrasena(`${window.location.pathname}${window.location.search}`));
        }
    }, [debeCambiar, router]);
    const refrescarSesion = useCallback(() => void refrescar(), [refrescar]);

    if (debeCambiar) {
        return <EsqueletoPanel />;
    }
    if (yo.estado === "listo") {
        const datos = yo.datos;
        const varias = (datos.empresas?.length ?? 0) > 1;
        const elegir = !administrativa && (datos.modoEmpresa === "por_elegir" || (varias && (pedida || datos.empresaActiva === undefined)));
        // La clave remonta la pantalla al cambiar de empresa: todo se vuelve a leer.
        const clave = `${datos.modoEmpresa}-${datos.empresaActiva?.id ?? 0}`;
        return (
            <ProveedorSesion yo={datos} elegirEmpresa={elegirEmpresa} pedirEmpresa={pedirEmpresa} refrescarSesion={refrescarSesion}>
                <DialogoReautenticar />
                <ProveedorResumenTickets
                    activo={(datos.permisos ?? []).includes("tickets.ver") && !elegir && (datos.modoEmpresa === "sin_conector" || datos.empresaActiva !== undefined)}
                    clave={clave}
                >
                    <Estructura>
                        {datos.modoEmpresa === "sin_empresa" && !administrativa ? (
                            <SinEmpresa />
                        ) : elegir ? (
                            <ElegirEmpresa motivo={noPermitida ? "no_permitida" : undefined} />
                        ) : (
                            <div key={clave}>{children}</div>
                        )}
                    </Estructura>
                </ProveedorResumenTickets>
            </ProveedorSesion>
        );
    }
    if (yo.estado === "error" && !(yo.error instanceof ErrorApi && yo.error.estado === 401)) {
        return (
            <div className="mx-auto grid min-h-dvh max-w-lg content-center gap-6 px-4">
                <MarcaIris />
                <ErrorLectura error={yo.error} reintentar={yo.reintentar} que="tu sesión" />
            </div>
        );
    }
    return <EsqueletoPanel />;
}

function EsqueletoPanel() {
    return (
        <div className="h-dvh overflow-hidden bg-background-secondary" aria-busy="true">
            <span className="sr-only" role="status">
                Cargando tu sesión…
            </span>
            <aside className="fixed top-4 bottom-4 left-4 hidden w-62 flex-col gap-3 rounded-[var(--iris-radio-isla)] border border-border bg-[var(--iris-tarjeta)] p-4 shadow-[var(--iris-sombra-flotante)] lg:flex">
                <MarcaIris className="[&_svg]:size-[2.375rem]" />
                <div className="mt-4 grid gap-2">
                    {Array.from({ length: 6 }, (_, i) => (
                        <Esqueleto key={i} className="h-8" />
                    ))}
                </div>
            </aside>
            <div className="fixed top-2 right-4 left-4 flex h-11 items-center gap-2.5 lg:top-4 lg:left-[18.5rem]">
                <LogoIris className="size-10 lg:hidden" />
                <Esqueleto className="h-11 w-60 rounded-full" />
            </div>
            <main className="fixed inset-x-0 top-[3.75rem] bottom-0 px-4 lg:top-[4.75rem] lg:right-4 lg:left-[18.5rem]">
                <Esqueleto className="mb-5 h-8 w-48" />
                <Esqueleto className="h-40" />
            </main>
        </div>
    );
}

/** Preferencia de cada persona: barra lateral contraída a solo íconos (en este navegador). */
const CLAVE_CONTRAIDA = "iris-menu-contraido";

function leerContraida(): boolean {
    try {
        return window.localStorage.getItem(CLAVE_CONTRAIDA) === "1";
    } catch {
        return false;
    }
}

function guardarContraida(valor: boolean) {
    try {
        window.localStorage.setItem(CLAVE_CONTRAIDA, valor ? "1" : "0");
    } catch {
        // Sin almacenamiento (privado, bloqueado): la preferencia dura lo que la pestaña.
    }
}

function Estructura({ children }: { children: ReactNode }) {
    const router = useRouter();
    return (
        <ProveedorRutas navegar={router.push}>
            <ProveedorMigas>
                <ProveedorCuenta>
                    <DisponibilidadSiAplica>
                        <VozSiAplica>
                            <CentralitaSiAplica>
                                <ProveedorChatEquipo>
                                    <EstructuraPanel>{children}</EstructuraPanel>
                                </ProveedorChatEquipo>
                            </CentralitaSiAplica>
                        </VozSiAplica>
                    </DisponibilidadSiAplica>
                </ProveedorCuenta>
            </ProveedorMigas>
            <AvisosDelPanel />
        </ProveedorRutas>
    );
}

/**
 * Disponibilidad en la bandeja (fase 1.1): solo para quien ve conversaciones y con la empresa
 * activa resuelta. La clave la vuelve a leer al cambiar de empresa.
 */
function DisponibilidadSiAplica({ children }: { children: ReactNode }) {
    const { yo, puede } = useSesion();
    const activa = puede(["conversaciones.ver"]) && (yo.modoEmpresa === "sin_conector" || yo.empresaActiva !== undefined);
    if (!activa) return children;
    return <ProveedorDisponibilidad key={yo.empresaActiva?.id ?? 0}>{children}</ProveedorDisponibilidad>;
}

/**
 * Voz (E1 y E1b): para quien contesta llamadas (`llamadas.contestar`) o llama (`llamadas.llamar`) y ve conversaciones, con la
 * empresa activa resuelta. Va en el layout: el aviso de una llamada y la barra de la llamada en
 * curso siguen al navegar.
 */
function VozSiAplica({ children }: { children: ReactNode }) {
    const { yo, puede } = useSesion();
    const contesta = puede(["llamadas.contestar"]);
    const activa = (contesta || puede(["llamadas.llamar"])) && puede(["conversaciones.ver"]) && (yo.modoEmpresa === "sin_conector" || yo.empresaActiva !== undefined);
    if (!activa) return children;
    return (
        <ProveedorVoz key={yo.empresaActiva?.id ?? 0} contesta={contesta}>
            {children}
        </ProveedorVoz>
    );
}

/**
 * Centralita Issabel (E4a): para cualquier persona con la empresa activa resuelta. El proveedor
 * pregunta si tiene una extensión y solo entonces escucha (con o sin `conversaciones.ver`). El
 * teléfono de Iris (E4c) va por fuera: la ventana de E4a le pregunta si la llamada es suya.
 */
function CentralitaSiAplica({ children }: { children: ReactNode }) {
    const { yo } = useSesion();
    if (yo.modoEmpresa !== "sin_conector" && yo.empresaActiva === undefined) return children;
    return (
        <ProveedorTelefono key={yo.empresaActiva?.id ?? 0}>
            <ProveedorCentralita>{children}</ProveedorCentralita>
        </ProveedorTelefono>
    );
}

const CONSULTA_ESCRITORIO = "(min-width: 1024px)";

/**
 * Pantallas de ancho completo (la bandeja de conversaciones y el chat del equipo): la barra lateral se compacta a riel
 * de íconos (sin tocar la preferencia guardada) y el contenido usa todo el ancho y el alto que
 * queda bajo la barra superior, sin márgenes. En el teléfono las migas no se repiten arriba: la
 * pantalla tiene su título y su «Atrás».
 */
const RUTAS_ANCHO_COMPLETO = ["/conversaciones", "/equipo"];
export const esAnchoCompleto = (ruta: string) => RUTAS_ANCHO_COMPLETO.includes(ruta);

/**
 * Región de avisos en el teléfono (rediseño): sobre la fila del encabezado (a lo ancho), unos
 * segundos: nunca tapa las migas, la isla del menú, «Guardar» ni el panel lateral (que empieza bajo
 * el encabezado). El texto en letra pequeña: el título en hasta dos líneas; con descripción, el
 * título en una y la descripción en hasta dos, con la acción a su derecha. Así el aviso más alto
 * (unos 56 px desde 4 px) termina antes de la miga «‹ …» (64 px) y su contenido nunca se sale de la
 * caja: con la caja fija en 44 px y la acción debajo, el contenido (unos 124 px) se salía por arriba
 * de la pantalla y por debajo tapaba lo de la pantalla, p. ej. «Llamada» en la conversación.
 */
const CLASES_AVISOS_TELEFONO = [
    "top-[calc(env(safe-area-inset-top)+0.25rem)] inset-x-2 w-auto translate-x-0 sm:w-auto",
    "[&_[data-slot=toast]]:min-h-0 [&_[data-slot=toast]]:py-0.5",
    "[&_[data-slot=toast-title]]:text-xs [&_[data-slot=toast-title]]:leading-4 [&_[data-slot=toast-title]]:line-clamp-2",
    "[&_[data-slot=toast-content]:has([data-slot=toast-description])_[data-slot=toast-title]]:line-clamp-1",
    "[&_[data-slot=toast-description]]:text-xs [&_[data-slot=toast-description]]:leading-4 [&_[data-slot=toast-description]]:line-clamp-2",
    "[&_[data-slot=toast-content]]:grid [&_[data-slot=toast-content]]:min-w-0 [&_[data-slot=toast-content]]:grid-cols-[minmax(0,1fr)_auto] [&_[data-slot=toast-content]]:items-center [&_[data-slot=toast-content]]:gap-x-2 [&_[data-slot=toast-content]]:gap-y-0",
    "[&_[data-slot=toast-title]]:col-span-2 [&_[data-slot=toast-description]]:col-start-1 [&_[data-slot=toast-description]]:row-start-2",
    "[&_[data-slot=toast-action-button]]:col-start-2 [&_[data-slot=toast-action-button]]:row-start-2 [&_[data-slot=toast-action-button]]:h-7 [&_[data-slot=toast-action-button]]:px-2 [&_[data-slot=toast-action-button]]:text-xs",
].join(" ");

function suscribirAncho(avisar: () => void) {
    const consulta = window.matchMedia(CONSULTA_ESCRITORIO);
    consulta.addEventListener("change", avisar);
    return () => consulta.removeEventListener("change", avisar);
}

/**
 * Avisos emergentes: abajo a la derecha en escritorio. En el teléfono, sobre la fila del
 * encabezado: así no tapan la miga «‹ …» de la pantalla, ni la isla del menú, ni «Guardar», ni las
 * hojas que suben desde abajo.
 */
function AvisosDelPanel() {
    const escritorio = useSyncExternalStore(
        suscribirAncho,
        () => window.matchMedia(CONSULTA_ESCRITORIO).matches,
        () => true,
    );
    return escritorio ? (
        <ProveedorAvisosEmergentes ubicacion="bottom end" />
    ) : (
        <ProveedorAvisosEmergentes ubicacion="top" className={CLASES_AVISOS_TELEFONO} />
    );
}

/**
 * Grupos del menú para la isla: la operación arriba (sin título), Negocio, «Administración» al pie.
 * Tickets lleva su contador en ámbar si hay SLA en riesgo o vencidos; los demás, en índigo.
 */
function aGruposIsla(grupos: readonly GrupoVisible[], ruta: string, contadores: Readonly<Record<string, number | undefined>>, resumen: ContextoResumen): GrupoIsla[] {
    const activa = entradaActiva(ruta);
    const urgentes = resumen?.estado === "listo" ? resumen.datos.slaVencidos + resumen.datos.slaEnRiesgo : 0;
    return grupos.map((g) => ({
        id: g.id,
        etiqueta: g.etiqueta,
        ...(g.tituloOculto ? { tituloOculto: true } : {}),
        ...(g.id === "administracion" ? { alPie: true } : {}),
        entradas: g.entradas.map((e) => ({
            href: e.href,
            etiqueta: e.etiqueta,
            ...(e.etiquetaCorta ? { etiquetaCorta: e.etiquetaCorta } : e.href === "/conversaciones" ? { etiquetaCorta: "Bandeja" } : e.href === "/administracion" ? { etiquetaCorta: "Admin." } : {}),
            icono: e.icono,
            contador: contadores[e.href],
            ...(e.href === "/tickets" && urgentes > 0 ? { tonoContador: "atencion" as const } : {}),
            actual: activa?.href === e.href,
            ...(e.proximamente ? { proximamente: true } : {}),
        })),
    }));
}

function EstructuraPanel({ children }: { children: ReactNode }) {
    const { yo, permisos, puede, empresas } = useSesion();
    const ruta = usePathname();
    const conEmpresa = yo.modoEmpresa !== "sin_conector" && empresas.length > 0;
    const avisoAdopcion = yo.hayDatosSinEmpresa && puede(["empresas.adoptar"]) && !ruta.startsWith("/administracion/datos-sin-empresa");
    const router = useRouter();
    const grupos = menuAgrupado(permisos);
    const telefono = repartoTelefono(permisos);
    const [contraidaGuardada, setContraida] = useState(leerContraida);
    const anchoCompleto = esAnchoCompleto(ruta);
    // En la bandeja el menú va como riel; «Expandir menú» lo abre solo mientras sigas ahí.
    const [expandidaEn, setExpandidaEn] = useState<string | null>(null);
    const contraida = anchoCompleto ? expandidaEn !== ruta : contraidaGuardada;
    const [masAbierto, setMasAbierto] = useState(false);
    const [cerrando, setCerrando] = useState(false);
    const [errorCerrar, setErrorCerrar] = useState<unknown>(null);
    const idBarra = useId();
    const disponibilidad = useDisponibilidad();
    const chat = useChatEquipo();
    const voz = useVoz();

    function alternarContraida() {
        if (anchoCompleto) {
            setExpandidaEn((e) => (e === ruta ? null : ruta));
            return;
        }
        setContraida((c) => {
            guardarContraida(!c);
            return !c;
        });
    }

    async function cerrarSesion() {
        // Con una llamada viva o sonando se pregunta, y se cuelga o suelta ANTES de cerrar la sesión
        // (después el servidor respondería 401 y la llamada quedaría viva).
        if (voz && !(await voz.antesDeSalir())) return;
        setCerrando(true);
        setErrorCerrar(null);
        // Al cerrar sesión dejas de estar disponible en la bandeja (lo dice el servidor; si no
        // responde en 2 s, la sesión se cierra igual).
        await disponibilidad?.alSalir();
        // Lo que esperaba su «Deshacer» en el chat sale ya, con esta sesión (después daría 401).
        await chat?.vaciar();
        try {
            await resolver(api().DELETE("/api/v1/sesion"));
        } catch (error) {
            // 401: la sesión ya no existía; para el usuario es lo mismo que cerrarla.
            if (!(error instanceof ErrorApi && error.estado === 401)) {
                setErrorCerrar(error);
                setCerrando(false);
                return;
            }
        }
        // Los clientes recientes del selector y los borradores de la bandeja no sobreviven a la sesión.
        limpiarClientesRecientes();
        limpiarBorradores();
        router.replace("/entrar");
        router.refresh();
    }

    const resumen = useResumenTickets();
    const noLeidos = useNoLeidosConversaciones(puede(["conversaciones.ver"]) && (yo.modoEmpresa === "sin_conector" || yo.empresaActiva !== undefined), `${yo.empresaActiva?.id ?? 0}`);
    const contadores = { "/tickets": contadorDe(resumen, "activos"), "/conversaciones": noLeidos, "/equipo": chat?.contadores.todas };

    const gruposIsla = aGruposIsla(grupos, ruta, contadores, resumen);
    const claveManual = claveManualDeRuta(ruta);

    return (
        <ProveedorManual clave={claveManual}>
            <div data-slot="marco-panel" data-contraida={contraida || undefined} className="relative min-h-dvh bg-background-secondary text-[0.875rem] lg:h-dvh lg:overflow-hidden">
                <MenuIsla
                    id={idBarra}
                    grupos={gruposIsla}
                    compacta={contraida}
                    alAlternar={alternarContraida}
                    marca={
                        <Link href="/" className="block rounded-xl" aria-label="Iris, inicio">
                            {contraida ? <LogoIris className="size-[2.375rem]" /> : <MarcaIris className="[&_svg]:size-[2.375rem]" />}
                        </Link>
                    }
                    extra={
                        <Suspense>
                            <MisVistas />
                        </Suspense>
                    }
                />

                {/* Encabezado sin barra: botones sueltos, cada uno una isla. */}
                <div className={cn("absolute top-2 right-4 left-4 z-20 lg:fixed lg:top-4", contraida ? "lg:left-[7rem]" : "lg:left-[18.5rem]")}>
                    <EncabezadoPanel
                        izquierda={
                            <>
                                <Link href="/" className="rounded-xl lg:hidden" aria-label="Iris, inicio">
                                    <LogoIris className="size-10" />
                                </Link>
                                <BuscadorGlobal />
                                <AccesosRapidos />
                            </>
                        }
                        derecha={
                            <>
                                <BotonTelefono />
                                <BotonRecibir />
                                <BotonChatEquipo />
                                <EstadoConexiones />
                                <BotonAvisos />
                                <CuentaEncabezado cerrando={cerrando} cerrarSesion={cerrarSesion} />
                            </>
                        }
                    />
                </div>
                {ruta !== "/equipo" ? <PanelChatEquipo /> : null}
                <CapaTelefono />
                {voz ? (
                    <>
                        <AvisoLlamada />
                        <NotaLlamada />
                    </>
                ) : null}

                {/*
                  * El área de trabajo. Escritorio: la página no se desplaza, el contenido sí (por dentro).
                  * Teléfono: cada pantalla a pantalla completa y se desplaza la página (las listas de
                  * React Aria se cierran si se desplaza un contenedor que no sea la página).
                  */}
                <main
                    id="contenido"
                    data-ancho-completo={anchoCompleto || undefined}
                    className={cn(
                        // Teléfono: la página es el contenedor (pantalla completa) y se desplaza bajo el
                        // encabezado; escritorio: el área de trabajo se desplaza por dentro.
                        "pt-[3.75rem] lg:fixed lg:inset-x-0 lg:top-[4.75rem] lg:bottom-0 lg:overflow-x-hidden lg:overflow-y-auto lg:overscroll-contain lg:pt-0 lg:[scrollbar-width:thin]",
                        contraida ? "lg:left-[7rem]" : "lg:left-[18.5rem]",
                        "lg:right-4 lg:bottom-4",
                        anchoCompleto ? "min-w-0" : "px-4 pb-[calc(6.5rem+env(safe-area-inset-bottom))] lg:pr-1 lg:pb-4 lg:pl-1",
                    )}
                >
                    {voz ? (
                        <Suspense>
                            <BarraLlamada />
                        </Suspense>
                    ) : null}
                    <BarraTelefono />
                    <div className={cn(anchoCompleto ? "min-w-0" : "mx-auto max-w-[96rem] pt-1")}>
                        {/* En la bandeja y en «Equipo» las migas van solo en escritorio, en una fila de 28 px. */}
                        {anchoCompleto ? (
                            <div className="hidden h-7 items-start overflow-hidden px-1 lg:flex">
                                <MigasPanel className="mb-0" />
                            </div>
                        ) : (
                            <MigasPanel className="mb-1.5" />
                        )}
                        {errorCerrar ? (
                            <Aviso
                                tono="error"
                                titulo="No se pudo cerrar la sesión"
                                className={anchoCompleto ? "m-3" : "mb-4"}
                                accion={
                                    <Boton variante="secundario" tamano="sm" onClick={cerrarSesion}>
                                        <RotateCw />
                                        Reintentar
                                    </Boton>
                                }
                            >
                                {mensajeDeError(errorCerrar).detalle}
                            </Aviso>
                        ) : null}
                        <AvisoTelefono />
                        {avisoAdopcion ? <AvisoDatosSinEmpresa /> : null}
                        {children}
                    </div>
                </main>

                {/* Teléfono: isla de menú abajo; «Más» es la barra lateral entera en una hoja. */}
                <BarraInferior entradas={telefono.barra} contadores={contadores} masAbierto={masAbierto} alCambiarMas={setMasAbierto}>
                    <div className="mt-2 flex flex-1 flex-col gap-6">
                        {disponibilidad ? (
                            <section aria-label="Tu disponibilidad" className="grid gap-2 rounded-xl border border-border bg-card p-3">
                                <span className="text-xs font-medium text-dimmed">Conversaciones</span>
                                <ControlDisponibilidad />
                            </section>
                        ) : null}
                        {voz ? (
                            <section aria-label="Llamadas" className="grid gap-2 rounded-xl border border-border bg-card p-3">
                                <span className="text-xs font-medium text-dimmed">Llamadas</span>
                                <ControlRecibir />
                            </section>
                        ) : null}
                        <Navegacion grupos={grupos} contadores={contadores} alNavegar={() => setMasAbierto(false)} />
                        <Suspense>
                            <MisVistas alNavegar={() => setMasAbierto(false)} />
                        </Suspense>
                        {conEmpresa ? (
                            <div className="grid gap-2">
                                <span className="text-xs font-medium text-dimmed">Empresa</span>
                                <SelectorEmpresa />
                            </div>
                        ) : null}
                        <MenuCuentaLista cerrando={cerrando} cerrarSesion={cerrarSesion} antes={() => setMasAbierto(false)} />
                        <div className="mt-auto grid gap-2">
                            <span className="text-xs text-muted-foreground">Tema</span>
                            <SelectorTema className="justify-self-start" />
                        </div>
                    </div>
                </BarraInferior>
            </div>
        </ProveedorManual>
    );
}
