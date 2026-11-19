"use client";

import { Aviso, Boton } from "@iris/ui";
import type { Schema } from "@iris/contratos";
import { ChevronRight, RotateCw } from "lucide-react";
import Link from "next/link";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type TextoConCuenta = Schema<"TextoConCuenta">;

const TIPO: Record<TextoConCuenta["tipo"], string> = {
    manual: "Manual",
    respuesta_rapida: "Respuesta rápida",
    fuera_de_horario: "Fuera de horario de",
};

const MOTIVO: Record<TextoConCuenta["motivo"], string> = {
    cuenta_de_argos: "Cuenta de Argos",
    parece_cuenta: "Parece una cuenta",
};

/** Dónde se corrige cada texto. */
export function enlaceTexto(t: Pick<TextoConCuenta, "tipo" | "id">): string {
    switch (t.tipo) {
        case "manual":
            return `/administracion/manuales/${t.id}/editar`;
        case "respuesta_rapida":
            return `/administracion/respuestas-rapidas/${t.id}/editar`;
        default:
            return `/administracion/numeros/${t.id}/configurar`;
    }
}

/** «1 texto parece…» / «3 textos parecen…». */
export function tituloTextosConCuentas(n: number): string {
    return n === 1 ? "1 texto parece tener un número de cuenta escrito a mano" : `${n} textos parecen tener números de cuenta escritos a mano`;
}

/**
 * Aviso al admin (contrato con Argos 0.8.0 §10.3, filas 12 a 14; §10.2 punto 11): manuales, respuestas
 * rápidas y respuestas de fuera de horario que parecen llevar un número de cuenta. Las cuentas salen
 * de Argos con «Enviar cuentas»; Iris solo avisa y no bloquea guardar. Sin textos, no muestra nada.
 */
export function AvisoTextosConCuentas() {
    const { puede } = useSesion();
    if (!puede(["canales.configurar"])) return null;
    return <AvisoInterno />;
}

function AvisoInterno() {
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/mensajeria/revision-cuentas", { signal }))).data);
    if (lectura.estado === "cargando") return null;
    if (lectura.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudo revisar si hay números de cuenta escritos a mano"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(lectura.error).titulo}
            </Aviso>
        );
    }
    const textos = lectura.datos ?? [];
    if (textos.length === 0) return null;
    return (
        <Aviso tono="warning" titulo={tituloTextosConCuentas(textos.length)} data-testid="textos-con-cuentas">
            <p>Las cuentas salen de Argos con «Enviar cuentas». Borra los números de estos textos para que no queden viejos. Iris no bloquea guardar.</p>
            <ul className="mt-2 grid divide-y divide-border">
                {textos.map((t) => (
                    <li key={`${t.tipo}-${t.id}`}>
                        <Link href={enlaceTexto(t)} className="flex min-w-0 items-center gap-2 py-2 text-sm hover:underline">
                            <span className="min-w-0 flex-1 truncate">
                                {TIPO[t.tipo]} <span className="font-semibold">{t.titulo}</span>
                            </span>
                            <span className="shrink-0 rounded-full bg-elevated px-2 py-0.5 text-xs font-semibold text-muted-foreground">{MOTIVO[t.motivo]}</span>
                            <ChevronRight aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                        </Link>
                    </li>
                ))}
            </ul>
        </Aviso>
    );
}
