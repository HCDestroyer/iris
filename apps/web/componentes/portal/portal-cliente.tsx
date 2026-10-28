"use client";

import { Aviso, Boton, Esqueleto } from "@iris/ui";
import { LogOut, RotateCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { alTerminarSesionPortal, apiPortal } from "@/lib/portal/cliente";
import { ProveedorSesionPortal } from "@/lib/portal/sesion";
import { MarcoPortal } from "./marco";

const RUTA_EXPIRADA = "/soporte/entrar?motivo=expirada";

/** Marco del portal con sesión: lee `GET /portal/sesion`; un 401 en cualquier llamada vuelve a la entrada. */
export function PortalCliente({ children }: { children: ReactNode }) {
    const router = useRouter();
    const sesion = useLectura((signal) => resolver(apiPortal().GET("/api/v1/portal/sesion", { signal })));
    const [saliendo, setSaliendo] = useState(false);
    const [errorSalir, setErrorSalir] = useState(false);

    useEffect(() => alTerminarSesionPortal(() => router.replace(RUTA_EXPIRADA)), [router]);

    async function salir() {
        setSaliendo(true);
        setErrorSalir(false);
        try {
            await resolver(apiPortal().DELETE("/api/v1/portal/sesion"));
            router.replace("/soporte/entrar?motivo=salio");
        } catch (error) {
            if (error instanceof ErrorApi && error.estado === 401) {
                router.replace("/soporte/entrar?motivo=salio");
                return;
            }
            setErrorSalir(true);
            setSaliendo(false);
        }
    }

    if (sesion.estado === "cargando") {
        return (
            <MarcoPortal>
                <div aria-busy="true" className="grid gap-4">
                    <span className="sr-only" role="status">
                        Cargando…
                    </span>
                    <Esqueleto className="h-8 w-48" />
                    <Esqueleto className="h-32" />
                </div>
            </MarcoPortal>
        );
    }
    if (sesion.estado === "error") {
        const vencida = sesion.error instanceof ErrorApi && sesion.error.estado === 401;
        return (
            <MarcoPortal>
                {vencida ? (
                    <Aviso tono="info" titulo="Tu sesión terminó">
                        Te llevamos a la entrada…
                    </Aviso>
                ) : (
                    <Aviso
                        tono="error"
                        titulo="No pudimos cargar tu soporte"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={sesion.reintentar}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        Revisa tu conexión e inténtalo de nuevo.
                    </Aviso>
                )}
            </MarcoPortal>
        );
    }
    const nombre = sesion.datos.cliente.nombre;
    return (
        <ProveedorSesionPortal value={sesion.datos}>
            <MarcoPortal
                inicio
                acciones={
                    <>
                        <span className="hidden max-w-48 truncate text-sm sm:inline" data-testid="cliente-nombre">
                            {nombre}
                        </span>
                        <Boton variante="fantasma" tamano="sm" onClick={() => void salir()} disabled={saliendo}>
                            <LogOut />
                            {saliendo ? "Saliendo…" : "Salir"}
                        </Boton>
                    </>
                }
            >
                {errorSalir ? (
                    <Aviso tono="error" titulo="No pudimos cerrar tu sesión" className="mb-4">
                        Revisa tu conexión e inténtalo de nuevo.
                    </Aviso>
                ) : null}
                {children}
            </MarcoPortal>
        </ProveedorSesionPortal>
    );
}
