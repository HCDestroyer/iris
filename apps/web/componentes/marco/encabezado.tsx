"use client";

import { BotonFlotante, IndicadorEstado, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento } from "@iris/ui";
import { ClipboardPlus, Ellipsis, LifeBuoy, Plug } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { CREABLES } from "../barra-superior";

/**
 * Los dos accesos rápidos del encabezado: crear ticket y crear orden (según permisos). Se llaman
 * como dicen («Ticket», «Orden»); «Nuevo ticket» va en su globo y en la pantalla de la lista.
 */
const ACCESOS = [
    { href: "/tickets/nuevo", texto: "Ticket", nombre: "Nuevo ticket", icono: LifeBuoy, permiso: "tickets.crear", acento: true },
    { href: "/ordenes/nueva", texto: "Orden", nombre: "Nueva orden", icono: ClipboardPlus, permiso: "ordenes.crear", acento: false },
] as const;

/**
 * Izquierda del encabezado (lo que haces): los accesos rápidos «Ticket» y «Orden» (cada uno una
 * isla) y «Más» con todo lo que se puede crear. Sin permiso para crear nada, no aparecen. En el
 * teléfono no van: crear está en el botón redondo y en «Más».
 */
export function AccesosRapidos() {
    const { puede } = useSesion();
    const router = useRouter();
    const accesos = ACCESOS.filter((a) => puede([a.permiso]));
    const creables = CREABLES.filter((c) => puede([c.permiso]));
    if (creables.length === 0) return null;
    return (
        <div className="hidden items-center gap-2.5 lg:flex" data-slot="accesos-rapidos">
            {accesos.map((a) => {
                const Icono = a.icono;
                return (
                    <BotonFlotante key={a.href} asChild acento={a.acento} className="max-xl:size-11 max-xl:justify-center max-xl:px-0">
                        <Link href={a.href} aria-label={a.texto} title={a.nombre} data-acceso={a.texto.toLowerCase()}>
                            <Icono aria-hidden />
                            <span className="hidden xl:inline">{a.texto}</span>
                        </Link>
                    </BotonFlotante>
                );
            })}
            <MenuDesplegable>
                <MenuDesplegableDisparador>
                    <BotonFlotante redondo aria-label="Crear" title="Crear">
                        <Ellipsis />
                    </BotonFlotante>
                </MenuDesplegableDisparador>
                <MenuDesplegableContenido align="start">
                    {creables.map((c) => (
                        <MenuDesplegableElemento key={c.href} icono={c.icono} onSelect={() => router.push(c.href)}>
                            {c.etiqueta}
                        </MenuDesplegableElemento>
                    ))}
                </MenuDesplegableContenido>
            </MenuDesplegable>
        </div>
    );
}

const NOMBRE_SISTEMA = { argosisp: "Argos", horus: "Horus" } as const;

/**
 * Conexión con Argos y Horus (lo que vigilas): un punto discreto. Verde si hay un conector activo,
 * gris si está apagado o no existe (Iris sigue funcionando con datos locales). Solo con
 * `integraciones.ver`; si la lectura falla no se inventa un estado: el punto queda gris y el globo
 * lo dice. Lleva a Integraciones.
 */
export function EstadoConexiones() {
    const { puede } = useSesion();
    const router = useRouter();
    const permitido = puede(["integraciones.ver"]);
    const lectura = useLectura(async (signal) => (permitido ? ((await resolver(api().GET("/api/v1/integraciones/conectores", { params: { query: { limit: 50 } }, signal }))).data ?? []) : null));
    if (!permitido) return null;
    if (lectura.estado !== "listo" || lectura.datos === null) {
        return (
            <IndicadorEstado
                icono={<Plug />}
                tono="apagado"
                texto={lectura.estado === "error" ? "No se pudo leer la conexión con Argos y Horus" : "Leyendo la conexión con Argos y Horus…"}
                alPresionar={() => router.push("/administracion/integraciones")}
            />
        );
    }
    const activos = (["argosisp", "horus"] as const).filter((s) => lectura.datos?.some((c) => c.sistema === s && c.estado === "activo"));
    const texto =
        activos.length === 2
            ? "Argos y Horus conectados"
            : activos.length === 1
              ? `${NOMBRE_SISTEMA[activos[0] ?? "argosisp"]} conectado; ${NOMBRE_SISTEMA[activos[0] === "horus" ? "argosisp" : "horus"]} sin conectar`
              : "Argos y Horus sin conectar (Iris trabaja con sus datos)";
    return <IndicadorEstado icono={<Plug />} tono={activos.length > 0 ? "bien" : "apagado"} texto={texto} alPresionar={() => router.push("/administracion/integraciones")} />;
}
