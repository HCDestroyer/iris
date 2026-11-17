"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, CampoNumero, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { entradaDeAjustes, esquemaAjustesVoz, valoresDeAjustes, type ValoresAjustesVoz } from "@/lib/voz/ajustes";
import { guardarAjustesVoz, leerAjustesVoz } from "@/lib/voz/api";
import type { AjustesVoz } from "@/lib/voz/tipos";

/** `/administracion/llamadas`: ajustes de llamadas de la empresa (`GET /voz/ajustes`, campos de E1). */
export function AjustesDeLlamadas() {
    const lectura = useLectura((signal) => leerAjustesVoz(signal));
    return (
        <CargaRegistro lectura={lectura} que="los ajustes de llamadas" noEncontrado="Sin ajustes">
            {(a) => <Formulario ajustes={a} />}
        </CargaRegistro>
    );
}

function Formulario({ ajustes }: { ajustes: AjustesVoz }) {
    const router = useRouter();
    const { puede } = useSesion();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresAjustesVoz>({ resolver: zodResolver(esquemaAjustesVoz), defaultValues: valoresDeAjustes(ajustes) });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <FormularioPagina
            titulo="Ajustes de llamadas"
            descripcion="Valen para todos los números de la empresa que reciben llamadas de WhatsApp."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                try {
                    await guardarAjustesVoz(entradaDeAjustes(d, ajustes), clave.actual());
                    clave.renovar();
                    avisar({ titulo: "Ajustes de llamadas guardados" });
                    await liberarProteccion();
                    router.replace("/administracion");
                } catch (e) {
                    clave.trasError(e);
                    if (!erroresACampos(e, ["duracionMaxPersonaMin", "segundosAsignado"], form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA="/administracion"
            sucio={form.formState.isDirty}
            barra={{ enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: vuelve a abrirlos." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="A quién le suena primero">
                <CampoNumero
                    etiqueta="Segundos que suena solo a quien tiene la conversación"
                    minimo={0}
                    maximo={20}
                    ayuda="Después suena a todas las personas disponibles de la cola. Con 0 suena a todos desde el principio. De fábrica: 10."
                    error={errores.segundosAsignado?.message}
                    {...form.register("segundosAsignado")}
                />
            </SeccionFormulario>
            <SeccionFormulario titulo="Duración">
                <CampoNumero
                    etiqueta="Minutos que puede durar una llamada, como mucho"
                    minimo={10}
                    maximo={120}
                    ayuda="Al llegar a este tiempo la llamada se corta sola. De fábrica: 60."
                    error={errores.duracionMaxPersonaMin?.message}
                    {...form.register("duracionMaxPersonaMin")}
                />
            </SeccionFormulario>
            <SeccionFormulario titulo="Grabación">
                <p className="text-sm text-muted-foreground" data-slot="grabacion-no-disponible">
                    Las llamadas no se graban. La grabación todavía no está disponible.
                </p>
            </SeccionFormulario>
            {puede(["llamadas.configurar"]) ? (
                <p className="text-sm text-muted-foreground">
                    Qué número recibe llamadas, su horario y a qué cola le suenan se ajusta en cada número:{" "}
                    <Link href="/administracion/numeros" className="text-primary underline-offset-2 hover:underline">
                        Números de WhatsApp
                    </Link>
                    .
                </p>
            ) : null}
        </FormularioPagina>
    );
}
