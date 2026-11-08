"use client";

import { Aviso, Boton } from "@iris/ui";
import { Info, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { ANCLAS } from "@/lib/numeros/guia";
import { EnlaceGuia } from "./checklist-meta";
import { CampoCopiar } from "./campo-copiar";
import { SecretoGuardado } from "./secreto-guardado";

/**
 * Paso 3 «Conectar el webhook»: la URL y el token de verificación para pegar en Meta, y qué hacer
 * allá. El token generado se muestra solo en esta pantalla (una vez); después queda «•••• guardado»
 * con «Generar otro».
 */
export function WebhookMeta({
    url,
    verifyToken,
    verifyTokenGuardadoEn,
    alGenerarOtro,
    generando = false,
    error,
}: {
    /** `null` antes de guardar: la URL lleva el número que Iris le da al canal. */
    url: string | null;
    /** Recién generado (se ve una vez); `null` si ya se mostró o aún no existe. */
    verifyToken: string | null;
    verifyTokenGuardadoEn?: string | null;
    /** Sin ella (antes de guardar) no se ofrece «Generar otro». */
    alGenerarOtro?: () => void;
    generando?: boolean;
    /** Aviso de un error de «Generar otro». */
    error?: ReactNode;
}) {
    const generarOtro = alGenerarOtro ? (
        <Boton variante="contorno" onClick={alGenerarOtro} disabled={generando}>
            <RefreshCw aria-hidden />
            {generando ? "Generando…" : "Generar otro"}
        </Boton>
    ) : null;
    return (
        <div className="grid min-w-0 gap-4">
            {error}
            <CampoCopiar
                etiqueta="URL de devolución de llamada"
                valor={url}
                queSeCopio="URL del webhook copiada"
                placeholder="https://…/api/v1/mensajeria/canales/…/webhook"
                {...(url ? {} : { ayuda: "La URL completa sale al guardar: lleva el número que Iris le da a este canal." })}
            />
            {verifyToken ? (
                <CampoCopiar
                    etiqueta="Token de verificación"
                    valor={verifyToken}
                    queSeCopio="Token de verificación copiado"
                    ayuda="Lo generó Iris. Se muestra solo en esta pantalla: pégalo en Meta antes de salir."
                    acciones={generarOtro}
                />
            ) : url && alGenerarOtro ? (
                <div className="grid gap-1.5">
                    <SecretoGuardado etiqueta="Token de verificación" guardadoEn={verifyTokenGuardadoEn ?? null} accion="Generar otro" alAccion={alGenerarOtro} ocupado={generando} />
                    <p className="text-sm text-muted-foreground">Si generas otro, pégalo también en Meta o el webhook deja de verificarse.</p>
                </div>
            ) : (
                <CampoCopiar etiqueta="Token de verificación" valor={null} queSeCopio="" placeholder="Iris lo genera al guardar" ayuda="Lo verás una sola vez, en la pantalla que sigue." />
            )}
            <div className="grid gap-1.5 text-sm">
                <p className="font-medium">En Meta</p>
                <ol className="grid list-decimal gap-1 pl-5">
                    <li>
                        Abre tu app → <strong>WhatsApp</strong> → <strong>Configuración</strong> y, en «Webhook», pulsa <strong>Editar</strong>.
                    </li>
                    <li>
                        Pega la URL y el token de verificación, y pulsa <strong>Verificar y guardar</strong>.
                    </li>
                    <li>
                        En «Campos del webhook», suscríbete a <code className="font-mono font-semibold">messages</code>.
                    </li>
                </ol>
            </div>
            <Aviso tono="neutral" icono={Info} titulo="¿La app de Meta ya tiene otro número en Iris?">
                Meta manda todo a una sola URL por app. Este número necesita la suya propia;{" "}
                <EnlaceGuia ancla={ANCLAS.varios}>la guía explica cómo ponerla por número</EnlaceGuia>.
            </Aviso>
        </div>
    );
}
