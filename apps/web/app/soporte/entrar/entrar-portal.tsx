"use client";

import { Aviso, Boton, Cargando, EstadoVacio, Tarjeta } from "@iris/ui";
import { Clock, LinkIcon, LogOut, RotateCw, TimerOff } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { apiPortal } from "@/lib/portal/cliente";
import { ticketDesdeFragmento } from "@/lib/portal/fragmento";

type Motivo = "expirada" | "salio" | undefined;

type Estado =
    | { paso: "entrando" }
    | { paso: "sin_enlace"; motivo: Motivo }
    | { paso: "invalido" }
    | { paso: "limite"; minutos: number | undefined }
    | { paso: "error"; sinConexion: boolean };

function leerMotivo(search: string): Motivo {
    const m = new URLSearchParams(search).get("motivo");
    return m === "expirada" || m === "salio" ? m : undefined;
}

/**
 * Canje del enlace del portal del proveedor (`/soporte/entrar#<ticket>`). El ticket se lee del
 * fragmento (nunca de la query), se borra de la URL antes de enviarlo y solo vive en memoria para
 * «Reintentar»: no se registra ni se manda a ningún otro lado.
 */
/** Estado inicial según la URL (solo en el navegador: este componente no se pinta en el servidor). */
function estadoInicial(): { estado: Estado; ticket: string | null } {
    const leido = ticketDesdeFragmento(window.location.hash);
    if (leido === null) {
        return { estado: { paso: "sin_enlace", motivo: leerMotivo(window.location.search) }, ticket: null };
    }
    if ("invalido" in leido) {
        return { estado: { paso: "invalido" }, ticket: null };
    }
    return { estado: { paso: "entrando" }, ticket: leido.ticket };
}

export function EntrarPortal() {
    const router = useRouter();
    const [inicio] = useState(estadoInicial);
    const [estado, setEstado] = useState<Estado>(inicio.estado);
    // Solo en memoria, para «Reintentar»; se descarta al entrar o si el enlace no sirve.
    const ticket = useRef<string | null>(inicio.ticket);
    const enviado = useRef(false);

    const canjear = useCallback(
        async (valor: string) => {
            try {
                await resolver(apiPortal().POST("/api/v1/portal/sesion", { body: { ticket: valor } }));
                ticket.current = null;
                router.replace("/soporte");
            } catch (error) {
                if (error instanceof ErrorApi && (error.estado === 401 || error.estado === 422)) {
                    ticket.current = null;
                    setEstado({ paso: "invalido" });
                } else if (error instanceof ErrorApi && error.estado === 429) {
                    ticket.current = null;
                    setEstado({ paso: "limite", minutos: error.reintentarEn === undefined ? undefined : Math.max(1, Math.ceil(error.reintentarEn / 60)) });
                } else {
                    const sinConexion = error instanceof ErrorApi && (error.codigo === "sin_conexion" || error.codigo === "api_no_disponible");
                    setEstado({ paso: "error", sinConexion });
                }
            }
        },
        [router],
    );

    useEffect(() => {
        // Fuera de la URL antes de nada: no queda en el historial, ni al recargar ni al compartir.
        if (window.location.hash) {
            window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
        }
        if (ticket.current && !enviado.current) {
            enviado.current = true;
            void canjear(ticket.current);
        }
        // Otro enlace abierto en esta misma pestaña sin recargar (solo cambia el fragmento).
        function alCambiarFragmento() {
            const siguiente = estadoInicial();
            window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
            ticket.current = siguiente.ticket;
            setEstado(siguiente.estado);
            if (siguiente.ticket) {
                void canjear(siguiente.ticket);
            }
        }
        window.addEventListener("hashchange", alCambiarFragmento);
        return () => window.removeEventListener("hashchange", alCambiarFragmento);
    }, [canjear]);

    const reintentar = () => {
        if (ticket.current) {
            setEstado({ paso: "entrando" });
            void canjear(ticket.current);
        }
    };

    switch (estado.paso) {
        case "entrando":
            return (
                <Tarjeta className="grid place-items-center py-16">
                    <Cargando texto="Entrando a tu soporte…" visible />
                </Tarjeta>
            );
        case "sin_enlace":
            return (
                <Tarjeta>
                    <EstadoVacio
                        icono={estado.motivo === "salio" ? LogOut : estado.motivo === "expirada" ? Clock : LinkIcon}
                        titulo={
                            estado.motivo === "salio"
                                ? "Cerraste tu sesión"
                                : estado.motivo === "expirada"
                                  ? "Tu sesión terminó"
                                  : "Entra desde el portal de tu proveedor"
                        }
                        descripcion="Para ver tus casos o abrir uno nuevo, entra al portal de clientes de tu proveedor de internet y elige la opción de soporte. Te traerá aquí con un enlace seguro."
                    />
                </Tarjeta>
            );
        case "invalido":
            return (
                <Tarjeta>
                    <EstadoVacio
                        icono={TimerOff}
                        titulo="El enlace venció o ya se usó"
                        descripcion="Por tu seguridad, cada enlace sirve una sola vez y por poco tiempo. Vuelve al portal de clientes de tu proveedor y pide uno nuevo desde la opción de soporte."
                    />
                </Tarjeta>
            );
        case "limite":
            return (
                <Aviso tono="warning" titulo="Demasiados intentos" icono={Clock}>
                    {estado.minutos === undefined
                        ? "Espera unos minutos"
                        : `Espera ${estado.minutos} min`}{" "}
                    y vuelve a entrar desde el portal de clientes de tu proveedor.
                </Aviso>
            );
        case "error":
            return (
                <Aviso
                    tono="error"
                    titulo="No pudimos abrir tu soporte"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {estado.sinConexion ? "No hay conexión. Revisa tu internet e inténtalo de nuevo." : "Tuvimos un problema al entrar. Inténtalo de nuevo en un momento."}
                </Aviso>
            );
    }
}
