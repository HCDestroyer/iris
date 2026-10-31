"use client";

import { Aviso, Boton, cn, Esqueleto, Globo, LogoIris, MarcaIris, ProveedorAvisosEmergentes, ProveedorRutas } from "@iris/ui";
import { PanelLeftClose, PanelLeftOpen, RotateCw } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useId, useState, useSyncExternalStore, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { limpiarClientesRecientes } from "@/lib/clientes/selector";
import { requiereCambio, rutaCambiarContrasena, suscribirCambioRequerido } from "@/lib/cuenta/sesion";
import { guardarEmpresa, limpiarEmpresa, suscribirProblemasEmpresa } from "@/lib/empresa";
import { menuAgrupado, repartoTelefono } from "@/lib/menu";
import { ProveedorSesion, useSesion, useYo } from "@/lib/sesion";
import { BarraInferior } from "./barra-inferior";
import { BuscadorGlobal } from "./buscador-global";
import { MenuCuentaLista, MenuUsuario, ProveedorCuenta } from "./cuenta/menu-usuario";
import { MigasPanel, ProveedorMigas } from "./migas-panel";
import { ErrorLectura, SinEmpresa } from "./error-lectura";
import { Navegacion } from "./navegacion";
import { AvisoDatosSinEmpresa, ElegirEmpresa, SelectorEmpresa } from "./selector-empresa";
import { SelectorTema } from "./selector-tema";
import { DialogoReautenticar } from "@/componentes/cuenta/dialogo-reautenticar";

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
                <Estructura>
                    {datos.modoEmpresa === "sin_empresa" && !administrativa ? (
                        <SinEmpresa />
                    ) : elegir ? (
                        <ElegirEmpresa motivo={noPermitida ? "no_permitida" : undefined} />
                    ) : (
                        <div key={clave}>{children}</div>
                    )}
                </Estructura>
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
        <div className="min-h-dvh lg:pl-60" aria-busy="true">
            <span className="sr-only" role="status">
                Cargando tu sesión…
            </span>
            <aside className="glass-material fixed inset-y-0 left-0 hidden w-60 flex-col gap-3 border-r p-4 lg:flex">
                <MarcaIris />
                <div className="mt-4 grid gap-2">
                    {Array.from({ length: 6 }, (_, i) => (
                        <Esqueleto key={i} className="h-8" />
                    ))}
                </div>
            </aside>
            <header className="glass-material sticky top-0 z-30 flex h-14 items-center border-b px-4 lg:px-6">
                <MarcaIris className="lg:hidden" />
            </header>
            <main className="mx-auto max-w-6xl p-4 sm:p-6">
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
                    <EstructuraPanel>{children}</EstructuraPanel>
                </ProveedorCuenta>
            </ProveedorMigas>
            <AvisosDelPanel />
        </ProveedorRutas>
    );
}

const CONSULTA_ESCRITORIO = "(min-width: 1024px)";

function suscribirAncho(avisar: () => void) {
    const consulta = window.matchMedia(CONSULTA_ESCRITORIO);
    consulta.addEventListener("change", avisar);
    return () => consulta.removeEventListener("change", avisar);
}

/**
 * Avisos emergentes: abajo a la derecha en escritorio; en el teléfono arriba, bajo la barra
 * superior, para no tapar la barra inferior ni los botones de las hojas que suben desde abajo.
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
        <ProveedorAvisosEmergentes ubicacion="top" className="top-[calc(4rem+env(safe-area-inset-top))]" />
    );
}

function EstructuraPanel({ children }: { children: ReactNode }) {
    const { yo, permisos, puede } = useSesion();
    const ruta = usePathname();
    const avisoAdopcion = yo.hayDatosSinEmpresa && puede(["empresas.adoptar"]) && !ruta.startsWith("/administracion/datos-sin-empresa");
    const router = useRouter();
    const grupos = menuAgrupado(permisos);
    const telefono = repartoTelefono(permisos);
    const [contraida, setContraida] = useState(leerContraida);
    const [masAbierto, setMasAbierto] = useState(false);
    const [cerrando, setCerrando] = useState(false);
    const [errorCerrar, setErrorCerrar] = useState<unknown>(null);
    const idBarra = useId();

    function alternarContraida() {
        setContraida((c) => {
            guardarContraida(!c);
            return !c;
        });
    }

    async function cerrarSesion() {
        setCerrando(true);
        setErrorCerrar(null);
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
        // Los clientes recientes del selector no sobreviven a la sesión.
        limpiarClientesRecientes();
        router.replace("/entrar");
        router.refresh();
    }

    return (
        <div className={cn("min-h-dvh", contraida ? "lg:pl-16" : "lg:pl-60")}>
            <aside
                id={idBarra}
                aria-label="Barra lateral"
                data-contraida={contraida || undefined}
                className={cn(
                    "glass-material fixed inset-y-0 left-0 z-30 hidden flex-col gap-6 overflow-y-auto border-r py-4 lg:flex",
                    contraida ? "w-16 px-2" : "w-60 px-4",
                )}
            >
                <Link href="/" className="rounded-md px-1" aria-label="Iris, inicio">
                    {contraida ? <LogoIris className="mx-auto" /> : <MarcaIris />}
                </Link>
                <Navegacion grupos={grupos} contraida={contraida} />
                <div className={cn("mt-auto grid gap-2", contraida ? "justify-items-center" : "px-1")}>
                    {contraida ? null : (
                        <>
                            <span className="text-xs text-muted-foreground">Tema</span>
                            <SelectorTema className="justify-self-start" />
                        </>
                    )}
                    <Globo texto={contraida ? "Expandir menú" : "Contraer menú"} lado="end">
                        <Boton
                            variante="fantasma"
                            tamano={contraida ? "icono" : "sm"}
                            className={contraida ? undefined : "justify-self-start px-2 text-muted-foreground"}
                            aria-label={contraida ? "Expandir menú" : "Contraer menú"}
                            aria-expanded={!contraida}
                            aria-controls={idBarra}
                            onClick={alternarContraida}
                        >
                            {contraida ? <PanelLeftOpen /> : <PanelLeftClose />}
                            {contraida ? null : "Contraer menú"}
                        </Boton>
                    </Globo>
                </div>
            </aside>

            <header className="glass-material sticky top-0 z-20 flex h-14 items-center gap-2 border-b px-3 sm:px-4 lg:px-6">
                <Link href="/" className="rounded-md lg:hidden" aria-label="Iris, inicio">
                    <MarcaIris />
                </Link>
                <div className="ml-auto flex min-w-0 items-center gap-2 sm:gap-3">
                    <BuscadorGlobal />
                    <div className="hidden min-w-0 items-center gap-3 lg:flex">
                        <SelectorEmpresa />
                        <MenuUsuario cerrando={cerrando} cerrarSesion={cerrarSesion} />
                    </div>
                </div>
            </header>

            <main id="contenido" className="mx-auto max-w-6xl p-4 pb-[calc(5.5rem+env(safe-area-inset-bottom))] sm:p-6 sm:pb-[calc(5.5rem+env(safe-area-inset-bottom))] lg:pb-6">
                <MigasPanel />
                {errorCerrar ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudo cerrar la sesión"
                        className="mb-4"
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
                {avisoAdopcion ? <AvisoDatosSinEmpresa /> : null}
                {children}
            </main>

            <BarraInferior entradas={telefono.barra} masAbierto={masAbierto} alCambiarMas={setMasAbierto}>
                <div className="mt-2 flex flex-1 flex-col gap-6">
                    {telefono.resto.length > 0 ? <Navegacion grupos={telefono.resto} alNavegar={() => setMasAbierto(false)} /> : null}
                    <div className="grid gap-2">
                        <span className="text-xs font-medium text-dimmed">Empresa</span>
                        <SelectorEmpresa />
                    </div>
                    <MenuCuentaLista cerrando={cerrando} cerrarSesion={cerrarSesion} antes={() => setMasAbierto(false)} />
                    <div className="mt-auto grid gap-2">
                        <span className="text-xs text-muted-foreground">Tema</span>
                        <SelectorTema className="justify-self-start" />
                    </div>
                </div>
            </BarraInferior>
        </div>
    );
}
