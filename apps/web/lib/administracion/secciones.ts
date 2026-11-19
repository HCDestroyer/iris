import { Cloud, ClipboardList, Cpu, DatabaseZap, History, KeyRound, Mail, MailCheck, Map as IconoMapa, Plug, Scale, ShieldCheck, Smartphone, Users, Zap, Phone, PhoneCall, CalendarClock, BookOpen, Inbox, Settings2, Sparkles, type LucideIcon } from "lucide-react";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { PERMISOS_CUADRILLAS, PERMISOS_ZONAS } from "@/lib/agenda/permisos";
import { PERMISO_CONFIGURAR_CORREO, PERMISO_VER_CORREO } from "@/lib/correo/tipos";
import { PERMISO_CONFIGURAR_LEGAL, PERMISO_VER_LEGAL } from "@/lib/legal/datos";
import { PERMISO_CONEXION_META } from "@/lib/meta/tipos";
import type { Permiso } from "@/lib/menu";
import { PERMISO_VER, PERMISO_VER_USO } from "@/lib/servidor-ia/tipos";

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
        href: "/administracion/correo",
        titulo: "Correo",
        descripcion: "El correo con que Iris manda avisos a tu equipo. Con correo de prueba antes de guardar.",
        icono: MailCheck,
        permisos: [PERMISO_VER_CORREO, PERMISO_CONFIGURAR_CORREO],
    },
    {
        href: "/administracion/numeros",
        titulo: "Números de WhatsApp",
        descripcion: "Agregar números, credenciales de Meta, conexión, uso, cola, horario y llamadas.",
        icono: Phone,
        permisos: ["canales.configurar", "mensajeria.configurar"],
    },
    {
        href: "/administracion/conexion-meta",
        titulo: "Conexión con Meta",
        descripcion: "La app de Meta (o el servicio central) con que se conectan los números por QR.",
        icono: Cloud,
        permisos: [PERMISO_CONEXION_META],
    },
    {
        href: "/administracion/datos-legales",
        titulo: "Datos legales",
        descripcion: "Quién opera el servicio y los enlaces de privacidad, términos y eliminación de datos que pide Meta.",
        icono: Scale,
        permisos: [PERMISO_VER_LEGAL, PERMISO_CONFIGURAR_LEGAL],
    },
    {
        href: "/administracion/colas",
        titulo: "Colas",
        descripcion: "Miembros, supervisores y tiempos del bot de cada cola de la bandeja.",
        icono: Inbox,
        permisos: ["canales.configurar"],
    },
    {
        href: "/administracion/bandeja",
        titulo: "Ajustes de la bandeja",
        descripcion: "Minutos de inactividad para pasar a No disponible.",
        icono: Settings2,
        permisos: ["canales.configurar"],
    },
    {
        href: "/administracion/llamadas",
        titulo: "Llamadas",
        descripcion: "A quién le suena primero y cuánto puede durar una llamada de WhatsApp.",
        icono: PhoneCall,
        permisos: ["voz.configurar"],
    },
    {
        href: "/administracion/asistente",
        titulo: "Asistente de IA",
        descripcion: "Proveedor, modelo y uso del asistente que sugiere respuestas en la bandeja.",
        icono: Sparkles,
        permisos: [PERMISO_CONFIGURAR_ASISTENTE],
    },
    {
        href: "/administracion/horarios",
        titulo: "Horarios",
        descripcion: "Franjas por día y feriados de los números y las colas.",
        icono: CalendarClock,
        permisos: ["canales.configurar"],
    },
    {
        href: "/administracion/manuales",
        titulo: "Biblioteca de manuales",
        descripcion: "Guías para el equipo y para enviar al cliente desde la bandeja.",
        icono: BookOpen,
        permisos: ["manuales.ver"],
    },
    {
        href: "/administracion/respuestas-rapidas",
        titulo: "Respuestas rápidas",
        descripcion: "Textos que los agentes mandan con un toque en la bandeja de conversaciones.",
        icono: Zap,
        permisos: ["conversaciones.configurar"],
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
        href: "/administracion/servidor-ia",
        titulo: "Servidor de IA",
        descripcion: "La computadora con tarjeta gráfica donde trabaja el asistente: estado, uso y avisos.",
        icono: Cpu,
        permisos: [PERMISO_VER, PERMISO_VER_USO],
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
    {
        href: "/administracion/tokens",
        titulo: "Tokens de API",
        descripcion: "Para integraciones y scripts: revocar y eliminar los que ya no se usan.",
        icono: KeyRound,
        permisos: ["tokens.ver"],
    },
];

