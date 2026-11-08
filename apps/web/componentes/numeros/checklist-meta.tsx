"use client";

import { Aviso, Boton, Casilla } from "@iris/ui";
import { ChevronDown, ChevronRight, CircleHelp, QrCode } from "lucide-react";
import Link from "next/link";
import { useId, useState, type ReactNode } from "react";
import { ANCLAS, enlaceGuia } from "@/lib/numeros/guia";

type Ancla = (typeof ANCLAS)[keyof typeof ANCLAS];

type Punto = {
    id: string;
    titulo: string;
    descripcion: string;
    /** Ruta de menús en Meta (los nombres pueden variar). */
    ruta: readonly string[];
    ayuda: ReactNode;
    ancla: Ancla;
};

const PUNTOS: readonly Punto[] = [
    {
        id: "cuenta",
        titulo: "Cuenta de Meta Business con WhatsApp",
        descripcion: "Un portafolio comercial de Meta, mejor si ya está verificado.",
        ruta: ["business.facebook.com", "Configuración del negocio", "Centro de seguridad"],
        ayuda: "Ahí ves si el portafolio está verificado. Sin verificar puedes seguir, pero Meta limita a cuántos clientes se puede escribir por día.",
        ancla: ANCLAS.portafolio,
    },
    {
        id: "app",
        titulo: "Una app de Meta con el producto WhatsApp",
        descripcion: "Tipo «Negocio», conectada al portafolio de tu ISP.",
        ruta: ["developers.facebook.com/apps", "Crear app", "WhatsApp"],
        ayuda: "Al crear la app, conéctala a tu portafolio y agrega el producto WhatsApp con «Configurar».",
        ancla: ANCLAS.app,
    },
    {
        id: "ids",
        titulo: "Phone number ID y WABA ID",
        descripcion: "Los dos números largos debajo del número que vas a usar.",
        ruta: ["Tu app", "WhatsApp", "Configuración de la API"],
        ayuda: (
            <>
                Elige el número en «De». Debajo salen <strong>Identificador del número de teléfono</strong> (Phone number ID) e <strong>Identificador de la cuenta de WhatsApp Business</strong> (WABA ID).
            </>
        ),
        ancla: ANCLAS.ids,
    },
    {
        id: "token",
        titulo: "Token permanente de un usuario del sistema",
        descripcion: "No uses el token temporal de la página de la API: vence en 24 horas.",
        ruta: ["Configuración del negocio", "Usuarios", "Usuarios del sistema"],
        ayuda: (
            <>
                Agrega un usuario del sistema con rol Administrador, asígnale la app y la cuenta de WhatsApp, y pulsa <strong>Generar token</strong> con caducidad <strong>Nunca</strong> y los permisos{" "}
                <code className="font-mono text-[0.85em]">whatsapp_business_messaging</code> y <code className="font-mono text-[0.85em]">whatsapp_business_management</code>.
            </>
        ),
        ancla: ANCLAS.token,
    },
    {
        id: "secreto",
        titulo: "App secret",
        descripcion: "La clave secreta de la app. Iris la usa para comprobar que los mensajes vienen de Meta.",
        ruta: ["Tu app", "Configuración de la app", "Básica"],
        ayuda: "En «Clave secreta de la app», pulsa «Mostrar» (Meta pide tu contraseña) y cópiala.",
        ancla: ANCLAS.appSecret,
    },
];

/**
 * Paso 1 de «Agregar número»: lo que hay que tener listo en Meta. Las casillas son solo una ayuda
 * para quien llena el formulario: no se guardan ni cuentan como cambios sin guardar.
 */
export function ChecklistMeta() {
    const [marcados, setMarcados] = useState<ReadonlySet<string>>(new Set());
    return (
        <div className="grid gap-4">
            <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border" aria-label="Lo que necesitas en Meta">
                {PUNTOS.map((p) => (
                    <PuntoChecklist
                        key={p.id}
                        punto={p}
                        marcado={marcados.has(p.id)}
                        alMarcar={(m) =>
                            setMarcados((antes) => {
                                const s = new Set(antes);
                                if (m) s.add(p.id);
                                else s.delete(p.id);
                                return s;
                            })
                        }
                    />
                ))}
            </ul>
            <Aviso tono="info" icono={QrCode} titulo="¿Y conectar con código QR?">
                <p>Próximamente: conectar con QR un número que ya usas en WhatsApp Business (coexistencia oficial de Meta).</p>
                <p className="mt-1">Iris no usa conexiones por QR que no son de Meta: Meta bloquea esos números.</p>
                <EnlaceGuia ancla={ANCLAS.qr} className="mt-1 inline-block">
                    Saber más
                </EnlaceGuia>
            </Aviso>
        </div>
    );
}

function PuntoChecklist({ punto, marcado, alMarcar }: { punto: Punto; marcado: boolean; alMarcar: (m: boolean) => void }) {
    const [abierto, setAbierto] = useState(false);
    const idAyuda = useId();
    return (
        <li className="grid gap-1.5 p-3 sm:p-4" data-punto={punto.id}>
            <Casilla etiqueta={punto.titulo} checked={marcado} onChange={(e) => alMarcar(e.target.checked)} />
            <div className="grid gap-1.5 pl-7">
                <p className="text-sm text-muted-foreground">{punto.descripcion}</p>
                <Boton variante="enlace" tamano="sm" className="h-auto justify-self-start px-0" aria-expanded={abierto} aria-controls={idAyuda} onClick={() => setAbierto((a) => !a)}>
                    <CircleHelp aria-hidden />
                    ¿Dónde lo encuentro?
                    {abierto ? <ChevronDown aria-hidden /> : <ChevronRight aria-hidden />}
                </Boton>
                {abierto ? (
                    <div id={idAyuda} className="grid gap-2 rounded-lg bg-background-secondary p-3 text-sm">
                        <ol className="flex flex-wrap items-center gap-1" aria-label="Dónde, en Meta">
                            {punto.ruta.map((tramo, i) => (
                                <li key={tramo} className="inline-flex items-center gap-1">
                                    {i > 0 ? <ChevronRight aria-hidden className="size-3.5 text-muted-foreground" /> : null}
                                    <span className="rounded border border-border bg-card px-1.5 py-0.5 text-xs">{tramo}</span>
                                </li>
                            ))}
                        </ol>
                        <p>{punto.ayuda}</p>
                        <p className="text-xs text-muted-foreground">
                            Los nombres de los menús de Meta pueden variar.{" "}
                            <EnlaceGuia ancla={punto.ancla}>Ver el paso en la guía</EnlaceGuia>
                        </p>
                    </div>
                ) : null}
            </div>
        </li>
    );
}

/**
 * Enlace a la guía en otra pestaña: desde un formulario a medio llenar no se pierde lo escrito (y
 * la protección de cambios sin guardar no pregunta).
 */
export function EnlaceGuia({ ancla, className, children }: { ancla?: Ancla; className?: string; children: ReactNode }) {
    return (
        <Link href={enlaceGuia(ancla)} target="_blank" rel="noopener" className={["font-medium text-primary underline-offset-2 hover:underline", className].filter(Boolean).join(" ")}>
            {children}
            <span className="sr-only"> (se abre en otra pestaña)</span>
        </Link>
    );
}
