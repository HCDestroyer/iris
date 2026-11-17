"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoNumero, FormularioPagina, InsigniaEstado, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { ChevronDown, HardDrive, MemoryStick, RefreshCw, RotateCcw, Server, Thermometer, Users, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { guardarUmbrales, leerTablero, leerUmbrales } from "@/lib/servidor-ia/api";
import { textoAviso } from "@/lib/servidor-ia/presentacion";
import type { AvisoServidor, TipoAviso, Umbrales } from "@/lib/servidor-ia/tipos";
import { CAMPOS_UMBRALES, DE_FABRICA, entradaDeUmbrales, esDeFabrica, esquemaUmbrales, valoresDeUmbrales, type ValoresUmbrales } from "@/lib/servidor-ia/umbrales";
import { NavServidorIa, RUTA } from "./comun";

const ICONO: Record<TipoAviso, LucideIcon> = { caido: Server, incompatible: RefreshCw, vram: MemoryStick, disco: HardDrive, temperatura: Thermometer, saturacion: Users };

type Datos = { umbrales: Umbrales; avisos: AvisoServidor[] };

/**
 * `/administracion/servidor-ia/avisos`: los avisos de la última semana y, en «Avanzado», cuándo
 * avisar (`POST /servidor-ia/umbrales/actualizar`, contratos/voz.md §4.12) y cuánto historial se guarda. «Servidor caído» y
 * «Versión incompatible» avisan siempre: no tienen umbral.
 */
export function AvisosServidorIa() {
    const lectura = useLectura<Datos>(async (signal) => {
        const [umbrales, tablero] = await Promise.all([leerUmbrales(signal), leerTablero("7d", signal)]);
        return { umbrales, avisos: tablero.avisos };
    });
    return (
        <CargaRegistro lectura={lectura} que="los avisos del servidor de IA" noEncontrado="Sin servidor de IA">
            {(d) => <Formulario key={d.umbrales.version} datos={d} />}
        </CargaRegistro>
    );
}

function Formulario({ datos }: { datos: Datos }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [ahora] = useState(() => Date.now());
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresUmbrales>({ resolver: zodResolver(esquemaUmbrales), defaultValues: valoresDeUmbrales(datos.umbrales) });
    const v = useWatch({ control: form.control }) as ValoresUmbrales;
    const errores = form.formState.errors;
    const [abierto, setAbierto] = useState(false);
    const avanzado = abierto || CAMPOS_UMBRALES.some((c) => errores[c] !== undefined);
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const activos = datos.avisos.filter((a) => a.activo);
    const pasados = datos.avisos.filter((a) => !a.activo);

    return (
        <FormularioPagina
            titulo="Avisos del servidor de IA"
            descripcion="Iris te avisa cuando el servidor de IA necesita atención."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                try {
                    await guardarUmbrales(entradaDeUmbrales(d, datos.umbrales.version), clave.actual());
                    clave.renovar();
                    avisar({ titulo: "Avisos del servidor de IA guardados" });
                    await liberarProteccion();
                    router.replace(RUTA);
                } catch (e) {
                    clave.trasError(e);
                    if (!erroresACampos(e, CAMPOS_UMBRALES, form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA={RUTA}
            sucio={form.formState.isDirty}
            barra={{ enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los avisos: vuelve a abrirlos." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los avisos" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <NavServidorIa />
            <SeccionFormulario titulo="Avisos de esta semana">
                {datos.avisos.length === 0 ? (
                    <p className="text-sm text-muted-foreground" data-slot="sin-avisos">
                        Ninguno en los últimos 7 días.
                    </p>
                ) : (
                    <ul className="grid gap-4" data-slot="lista-avisos">
                        {[...activos, ...pasados].map((a, i) => {
                            const t = textoAviso(a);
                            const Icono = ICONO[a.tipo];
                            return (
                                <li key={`${a.tipo}-${a.desde}-${i}`} className="grid grid-cols-[2rem_minmax(0,1fr)] gap-3" data-aviso={a.tipo} data-activo={a.activo}>
                                    <span aria-hidden className={`flex size-8 items-center justify-center rounded-lg ${a.activo ? "bg-warning/15 text-warning" : "bg-elevated text-muted-foreground"}`}>
                                        <Icono className="size-4" />
                                    </span>
                                    <div className="grid min-w-0 gap-1">
                                        <p className="text-sm font-semibold">{t.titulo}</p>
                                        <p className="text-[0.8125rem] text-muted-foreground">{t.detalle}</p>
                                        <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
                                            {a.activo ? <InsigniaEstado tono="warning" texto="Activo ahora" /> : <InsigniaEstado tono="neutral" texto="Resuelto" />}
                                            <span>Empezó {haceCuanto(a.desde, ahora)}</span>
                                        </p>
                                    </div>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </SeccionFormulario>

            <SeccionFormulario titulo="Cómo te avisa">
                <p className="text-sm text-muted-foreground">
                    En la campana del panel y por correo, a quien administra toda la instalación. Cada problema avisa una vez cuando empieza y se cierra solo al resolverse; no llega un aviso por cada medición. Cada persona elige si quiere el correo en{" "}
                    <Link href="/cuenta" className="text-primary underline-offset-2 hover:underline">
                        sus notificaciones
                    </Link>
                    .
                </p>
                <p className="text-sm text-muted-foreground">«Servidor caído» y «Versión incompatible» avisan siempre: no se pueden apagar.</p>
            </SeccionFormulario>

            <section aria-labelledby="titulo-avanzado" data-slot="avanzado" className="surface-card grid min-w-0 gap-4 p-4 sm:p-5">
                <h2 id="titulo-avanzado" className="min-w-0">
                    <Boton variante="fantasma" className="h-auto w-full justify-between gap-3 px-0 py-0 text-left hover:bg-transparent" aria-expanded={avanzado} aria-controls="campos-avanzados" onClick={() => setAbierto(!avanzado)}>
                        <span className="grid min-w-0 gap-0.5">
                            <span className="text-base font-semibold tracking-[-0.015em]">Avanzado: cuándo avisar</span>
                            <span className="text-sm font-normal whitespace-normal text-muted-foreground">{esDeFabrica(v) ? "Con los valores de fábrica" : "Con valores cambiados"}</span>
                        </span>
                        <ChevronDown aria-hidden className={avanzado ? "size-5 shrink-0 rotate-180 transition-transform" : "size-5 shrink-0 transition-transform"} />
                    </Boton>
                </h2>
                {avanzado ? (
                    <div id="campos-avanzados" className="grid gap-5">
                        <p className="text-sm text-muted-foreground">Los valores de fábrica sirven para casi todos. Cámbialos solo si recibes avisos de más o de menos.</p>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <CampoNumero etiqueta="Memoria de video: avisa si pasa de (%)" minimo={50} maximo={100} ayuda={`De fábrica: ${DE_FABRICA.vramPct} %.`} error={errores.vramPct?.message} {...form.register("vramPct")} />
                            <CampoNumero etiqueta="Memoria de video: durante (minutos)" minimo={1} maximo={60} ayuda={`De fábrica: ${DE_FABRICA.vramMinutos} min.`} error={errores.vramMinutos?.message} {...form.register("vramMinutos")} />
                        </div>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <CampoNumero etiqueta="Temperatura: avisa si pasa de (°C)" minimo={60} maximo={100} ayuda={`De fábrica: ${DE_FABRICA.temperaturaC} °C.`} error={errores.temperaturaC?.message} {...form.register("temperaturaC")} />
                            <CampoNumero etiqueta="Temperatura: durante (minutos)" minimo={1} maximo={60} ayuda={`De fábrica: ${DE_FABRICA.temperaturaMinutos} min.`} error={errores.temperaturaMinutos?.message} {...form.register("temperaturaMinutos")} />
                        </div>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <CampoNumero etiqueta="Saturación: avisa si se rechazan más de (peticiones)" minimo={1} maximo={1000} ayuda={`Las que no encontraron lugar en el servidor. De fábrica: ${DE_FABRICA.saturacionRechazos}.`} error={errores.saturacionRechazos?.message} {...form.register("saturacionRechazos")} />
                            <CampoNumero etiqueta="Saturación: en (minutos)" minimo={1} maximo={60} ayuda={`De fábrica: ${DE_FABRICA.saturacionMinutos} min.`} error={errores.saturacionMinutos?.message} {...form.register("saturacionMinutos")} />
                        </div>
                        <div className="grid gap-3 sm:grid-cols-2">
                            <CampoNumero etiqueta="Disco de los modelos: avisa si pasa de (%)" minimo={50} maximo={99} ayuda={`De fábrica: ${DE_FABRICA.discoPct} %.`} error={errores.discoPct?.message} {...form.register("discoPct")} />
                        </div>
                        <div className="grid gap-3 sm:grid-cols-2" data-slot="retencion">
                            <CampoNumero etiqueta="Historial detallado: guardar (días)" minimo={1} maximo={30} ayuda={`Lo que alimenta «Ahora», «1 hora» y «24 horas». De fábrica: ${DE_FABRICA.retencionDetalleDias} días.`} error={errores.retencionDetalleDias?.message} {...form.register("retencionDetalleDias")} />
                            <CampoNumero etiqueta="Historial por hora: guardar (días)" minimo={7} maximo={365} ayuda={`Lo que alimenta «7 días» y el uso por empresa. De fábrica: ${DE_FABRICA.retencionHoraDias} días.`} error={errores.retencionHoraDias?.message} {...form.register("retencionHoraDias")} />
                        </div>
                        <div>
                            <Boton
                                type="button"
                                variante="fantasma"
                                tamano="sm"
                                disabled={esDeFabrica(v)}
                                onClick={() => {
                                    const fabrica = valoresDeUmbrales(DE_FABRICA);
                                    for (const c of CAMPOS_UMBRALES) form.setValue(c, fabrica[c], { shouldDirty: true, shouldValidate: true });
                                }}
                            >
                                <RotateCcw />
                                Valores de fábrica
                            </Boton>
                        </div>
                    </div>
                ) : null}
            </section>
        </FormularioPagina>
    );
}
