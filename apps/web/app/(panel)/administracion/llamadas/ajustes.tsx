"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoNumero, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { ChevronDown } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { EtiquetaEnPruebas, FranjaEnPruebas } from "@/componentes/voz/en-pruebas";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { entradaDeAjustes, esquemaAjustesVoz, resumenSalientes, valoresDeAjustes, type ValoresAjustesVoz } from "@/lib/voz/ajustes";
import { guardarAjustesVoz, leerAjustesVoz } from "@/lib/voz/api";
import type { AjustesVoz } from "@/lib/voz/tipos";

const AVANZADOS = ["maxSalientesPorDia", "topeMinutosSalientesMes", "maxColgadasMarcandoDia"] as const;

/** `/administracion/llamadas`: ajustes de llamadas de la empresa (`GET /voz/ajustes`, campos de E1 y E1b). */
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
    const v = useWatch({ control: form.control });
    // «Avanzado» va plegado; se abre solo si uno de sus campos tiene un error que corregir.
    const [abierto, setAbierto] = useState(false);
    const avanzado = abierto || AVANZADOS.some((c) => errores[c] !== undefined);
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
                    if (!erroresACampos(e, ["duracionMaxPersonaMin", "segundosAsignado", ...AVANZADOS], form.setError)) setError(e);
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
            <FranjaEnPruebas />
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
                    Las llamadas no se graban: la grabación todavía no está disponible.
                </p>
            </SeccionFormulario>
            <section aria-labelledby="titulo-avanzado" data-slot="avanzado" className="surface-card grid min-w-0 gap-4 p-4 sm:p-5">
                <h2 id="titulo-avanzado" className="min-w-0">
                    <Boton variante="fantasma" className="h-auto w-full justify-between gap-3 px-0 py-0 text-left hover:bg-transparent" aria-expanded={avanzado} aria-controls="campos-avanzados" onClick={() => setAbierto(!avanzado)}>
                        <span className="grid min-w-0 gap-0.5">
                            <span className="text-base font-semibold tracking-[-0.015em]">Avanzado</span>
                            <span className="text-sm font-normal text-muted-foreground [overflow-wrap:anywhere] whitespace-normal">{resumenSalientes(v)}</span>
                        </span>
                        <ChevronDown aria-hidden className={avanzado ? "size-5 shrink-0 rotate-180 transition-transform" : "size-5 shrink-0 transition-transform"} />
                    </Boton>
                </h2>
                {avanzado ? (
                    <div id="campos-avanzados" className="grid gap-4">
                        <EtiquetaEnPruebas />
                        <p className="text-sm text-muted-foreground" data-slot="nota-cobro">
                            Llamar a un cliente por WhatsApp se cobra por minuto (recibir llamadas no). Estos límites cuidan esa cuenta.
                        </p>
                        <div className="grid gap-4 sm:grid-cols-2">
                            <CampoNumero
                                etiqueta="Llamadas salientes al día, como mucho"
                                minimo={0}
                                maximo={1000}
                                ayuda="Entre todas las personas de la empresa. Con 0 no se puede llamar. De fábrica: 50."
                                error={errores.maxSalientesPorDia?.message}
                                {...form.register("maxSalientesPorDia")}
                            />
                            <CampoNumero
                                etiqueta="Minutos de llamadas salientes al mes, como mucho"
                                minimo={0}
                                maximo={100000}
                                ayuda="Al llegar al tope no se puede llamar hasta el mes siguiente, y la llamada que esté en curso se corta. Con 0 no se puede llamar. De fábrica: 300."
                                error={errores.topeMinutosSalientesMes?.message}
                                {...form.register("topeMinutosSalientesMes")}
                            />
                            <CampoNumero
                                etiqueta="Llamadas a un mismo contacto que se pueden colgar antes de que conteste, por día"
                                minimo={0}
                                maximo={20}
                                ayuda="Pasado ese número, a ese contacto ya no se le llama hasta el día siguiente (evita que parezca acoso). Con 0 no hay límite. De fábrica: 3."
                                error={errores.maxColgadasMarcandoDia?.message}
                                {...form.register("maxColgadasMarcandoDia")}
                            />
                        </div>
                    </div>
                ) : null}
            </section>
            {puede(["llamadas.configurar"]) ? (
                <p className="text-sm text-muted-foreground">
                    Qué número recibe llamadas, su horario, a qué cola le suenan y la plantilla para pedir permiso de llamar se ajustan en cada número:{" "}
                    <Link href="/administracion/numeros" className="text-primary underline-offset-2 hover:underline">
                        Números de WhatsApp
                    </Link>
                    .
                </p>
            ) : null}
        </FormularioPagina>
    );
}
