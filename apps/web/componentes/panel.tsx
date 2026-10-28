"use client";

import { Aviso, Boton, Esqueleto, Hoja, HojaContenido, HojaDisparador, MarcaIris } from "@iris/ui";
import { LogOut, Menu, RotateCw } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { menuVisible } from "@/lib/menu";
import { ProveedorSesion, useSesion, useYo } from "@/lib/sesion";
import { ErrorLectura } from "./error-lectura";
import { Navegacion } from "./navegacion";
import { SelectorTema } from "./selector-tema";

/** Layout autenticado: lee `GET /yo` y, solo si salió bien, arma el menú con sus permisos. */
export function Panel({ children }: { children: ReactNode }) {
    const yo = useYo();
    if (yo.estado === "listo") {
        return (
            <ProveedorSesion yo={yo.datos}>
                <Estructura>{children}</Estructura>
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
    const { yo, permisos } = useSesion();
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
                    <div className="hidden min-w-0 text-right sm:grid">
                        <span className="truncate text-sm font-medium" data-testid="usuario-nombre">
                            {yo.usuario.nombre}
                        </span>
                        <span className="truncate text-xs text-muted-foreground">{yo.usuario.correo}</span>
                    </div>
                    <Boton
                        variante="fantasma"
                        tamano="md"
                        onClick={cerrarSesion}
                        disabled={cerrando}
                        title="Cerrar sesión"
                        className="px-2.5 sm:px-3"
                    >
                        <LogOut />
                        <span className="sr-only sm:not-sr-only">{cerrando ? "Cerrando sesión…" : "Cerrar sesión"}</span>
                    </Boton>
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
                {children}
            </main>
        </div>
    );
}
