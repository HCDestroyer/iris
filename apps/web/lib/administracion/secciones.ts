import { ClipboardList, DatabaseZap, History, KeyRound, Mail, Map as IconoMapa, Plug, ShieldCheck, Smartphone, Users, type LucideIcon } from "lucide-react";
import { PERMISOS_CUADRILLAS, PERMISOS_ZONAS } from "@/lib/agenda/permisos";
import type { Permiso } from "@/lib/menu";

/** Secciones de Administración (índice y buscador global). Sin `href`: aún sin pantalla. */
export type Seccion = { href?: string; titulo: string; descripcion: string; icono: LucideIcon; permisos: readonly Permiso[] };

export const SECCIONES: readonly Seccion[] = [
    {
        href: "/administracion/buzones",
        titulo: "Buzones de correo",
        descripcion: "Correos que abren tickets y servidor de salida de las respuestas.",
        icono: Mail,
        permisos: ["buzones.ver"],
    },
    {
        href: "/administracion/plantillas",
        titulo: "Plantillas de formulario",
        descripcion: "Checklists de las órdenes: ítems, versiones y cuáles se ofrecen por tipo.",
        icono: ClipboardList,
        permisos: ["plantillas.ver"],
    },
    {
        href: "/administracion/cuadrillas",
        titulo: "Cuadrillas",
        descripcion: "Equipos de técnicos y las zonas que atienden.",
        icono: Users,
        permisos: PERMISOS_CUADRILLAS,
    },
    {
        href: "/administracion/zonas",
        titulo: "Zonas",
        descripcion: "Zonas de Argos y locales: crear, renombrar, activar y desactivar.",
        icono: IconoMapa,
        permisos: PERMISOS_ZONAS,
    },
    {
        href: "/administracion/integraciones",
        titulo: "Integraciones",
        descripcion: "Conectores con Argos y Horus Flow: conectar, activar y desactivar.",
        icono: Plug,
        permisos: ["integraciones.ver"],
    },
    {
        href: "/administracion/datos-sin-empresa",
        titulo: "Datos sin empresa",
        descripcion: "Asigna a una empresa lo guardado antes de conectar Argos.",
        icono: DatabaseZap,
        permisos: ["empresas.adoptar"],
    },
    {
        href: "/administracion/accesos",
        titulo: "Accesos",
        descripcion: "Quién entró, cuándo, por qué canal y con qué resultado; sesiones abiertas de cada usuario.",
        icono: History,
        permisos: ["accesos.ver"],
    },
    {
        href: "/administracion/actividad-app",
        titulo: "Actividad de la app",
        descripcion: "Última conexión y sincronización de cada técnico, versión de la app y lo que tiene sin enviar.",
        icono: Smartphone,
        permisos: ["campo.ver_actividad"],
    },
    {
        href: "/administracion/roles",
        titulo: "Roles y permisos",
        descripcion: "Qué puede hacer cada rol; crea, edita y desactiva roles propios.",
        icono: ShieldCheck,
        permisos: ["roles.ver"],
    },
    { titulo: "Tokens de API", descripcion: "Accesos para integraciones y scripts.", icono: KeyRound, permisos: ["tokens.ver"] },
];

