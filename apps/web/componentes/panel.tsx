"use client";

import { Aviso, Boton, Esqueleto, Hoja, HojaContenido, HojaDisparador, MarcaIris } from "@iris/ui";
import { Menu, RotateCw } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { requiereCambio, rutaCambiarContrasena, suscribirCambioRequerido } from "@/lib/cuenta/sesion";
import { guardarEmpresa, limpiarEmpresa, suscribirProblemasEmpresa } from "@/lib/empresa";
import { menuVisible } from "@/lib/menu";
import { ProveedorSesion, useSesion, useYo } from "@/lib/sesion";
import { MenuUsuario } from "./cuenta/menu-usuario";
import { ErrorLectura, SinEmpresa } from "./error-lectura";
import { Navegacion } from "./navegacion";
import { AvisoDatosSinEmpresa, ElegirEmpresa, SelectorEmpresa } from "./selector-empresa";
import { SelectorTema } from "./selector-tema";

/**
 * Layout autenticado: lee `GET /yo` y, solo si salió bien, arma el menú con sus permisos.
 * También gobierna la empresa activa: la elegida va en `X-Iris-Empresa` (lib/empresa.ts) y los
 * 422/403 de empresa de cualquier petición llegan aquí.
 */
/** Rutas administrativas: la API no exige empresa activa para ellas (i1.md). */
const RUTAS_SIN_EMPRESA = ["/usuarios", "/administracion", "/cuenta"];
/** Excepciones dentro de las administrativas: los buzones son de la empresa activa (como los tickets). */
const RUTAS_CON_EMPRESA = ["/administracion/buzones"];
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

function Estructura({ children }: { children: ReactNode }) {
    const { yo, permisos, puede } = useSesion();
    const ruta = usePathname();
    const avisoAdopcion = yo.hayDatosSinEmpresa && puede(["empresas.adoptar"]) && !ruta.startsWith("/administracion/datos-sin-empresa");
    const router = useRouter();
    const entradas = menuVisible(permisos);
    const [menuAbierto, setMenuAbierto] = useState(false);
    const [cerrando, setCerrando] = useState(false);
    const [errorCerrar, setErrorCerrar] = useState<unknown>(null);

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
        router.replace("/entrar");
        router.refresh();
    }

    return (
        <div className="min-h-dvh lg:pl-60">
            <aside
                aria-label="Barra lateral"
                className="glass-material fixed inset-y-0 left-0 z-30 hidden w-60 flex-col gap-6 border-r p-4 lg:flex"
            >
                <Link href="/" className="rounded-md px-1" aria-label="Iris, inicio">
                    <MarcaIris />
                </Link>
                <Navegacion entradas={entradas} />
                <div className="mt-auto grid gap-2 px-1">
                    <span className="text-xs text-muted-foreground">Tema</span>
                    <SelectorTema className="justify-self-start" />
                </div>
            </aside>

            <header className="glass-material sticky top-0 z-20 flex h-14 items-center gap-2 border-b px-2 sm:px-4 lg:px-6">
                <Hoja open={menuAbierto} onOpenChange={setMenuAbierto}>
                    <HojaDisparador asChild>
                        <Boton variante="fantasma" tamano="icono" className="lg:hidden" aria-label="Abrir menú" title="Menú">
                            <Menu />
                        </Boton>
                    </HojaDisparador>
                    <HojaContenido titulo={<MarcaIris />} etiquetaCerrar="Cerrar menú">
                        <div className="mt-4 flex flex-1 flex-col gap-6">
                            <Navegacion entradas={entradas} alNavegar={() => setMenuAbierto(false)} />
                            <div className="mt-auto grid gap-2">
                                <span className="text-xs text-muted-foreground">Tema</span>
                                <SelectorTema className="justify-self-start" />
                            </div>
                        </div>
                    </HojaContenido>
                </Hoja>
                <Link href="/" className="rounded-md lg:hidden" aria-label="Iris, inicio">
                    <MarcaIris />
                </Link>
                <div className="ml-auto flex min-w-0 items-center gap-2 sm:gap-3">
                    <SelectorEmpresa />
                    <MenuUsuario cerrando={cerrando} cerrarSesion={cerrarSesion} />
                </div>
            </header>

            <main id="contenido" className="mx-auto max-w-6xl p-4 sm:p-6">
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
        </div>
    );
}
