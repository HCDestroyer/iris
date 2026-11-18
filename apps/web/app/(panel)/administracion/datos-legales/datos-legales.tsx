"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, FormularioPagina, InsigniaEstado, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { CircleCheck, ExternalLink, FileWarning, LoaderCircle, RotateCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState, useSyncExternalStore } from "react";
import { useForm } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { CampoCopiar } from "@/componentes/numeros/campo-copiar";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import {
    CAMPO_META,
    CAMPOS_LEGAL,
    enlacesMeta,
    entradaLegal,
    esHttps,
    esquemaLegal,
    faltantesEnLlano,
    guardarDatosLegales,
    leerDatosLegales,
    nombreIdioma,
    paginaResponde,
    PERMISO_CONFIGURAR_LEGAL,
    valoresLegal,
    type DatosLegales,
    type EnlaceMeta,
    type EstadoEnlace,
    type ValoresLegal,
} from "@/lib/legal/datos";
import { CLAVES, TITULOS, type ClavePagina } from "@/lib/legal/paginas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/**
 * `/administracion/datos-legales` (`datos_legales.ver` / `.configurar`): quién opera la instalación
 * (contratos/legal.md). Con estos datos se arman las páginas públicas de privacidad, términos y
 * eliminación de datos que Meta pide; abajo van sus URL, listas para copiar y pegar en Meta, con
 * un indicador de si cada una responde.
 */
export function DatosLegalesPantalla() {
    const { yo } = useSesion();
    // Son de toda la instalación: como el correo general, los ve quien opera sin límite de empresa.
    if (!yo.todasLasEmpresas && yo.modoEmpresa !== "sin_conector") {
        return (
            <Aviso tono="warning" titulo="Sin permiso para ver los datos legales" data-testid="legal-sin-alcance">
                Son de toda la instalación: los configura quien opera en todas las empresas. Pídeselo a un administrador.
            </Aviso>
        );
    }
    return <Carga />;
}

function Carga() {
    const lectura = useLectura((signal) => leerDatosLegales(signal));
    return (
        <CargaRegistro lectura={lectura} que="los datos legales" noEncontrado="Los datos legales no están disponibles">
            {(datos) => <Formulario key={datos.version} datos={datos} alCambiar={lectura.reemplazar} recargar={lectura.reintentar} />}
        </CargaRegistro>
    );
}

function Formulario({ datos, alCambiar, recargar }: { datos: DatosLegales; alCambiar: (d: DatosLegales) => void; recargar: () => void }) {
    const router = useRouter();
    const { puede } = useSesion();
    const configura = puede([PERMISO_CONFIGURAR_LEGAL]);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresLegal>({ resolver: zodResolver(esquemaLegal), defaultValues: valoresLegal(datos) });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const faltan = faltantesEnLlano(datos.faltan);

    async function guardar(v: ValoresLegal) {
        setError(null);
        try {
            const guardado = await guardarDatosLegales(entradaLegal(datos, v), clave.actual());
            clave.renovar();
            avisar({
                titulo: "Datos legales guardados",
                descripcion: guardado.completo ? "Las páginas públicas ya están listas para Meta. El cambio se ve en ellas en un minuto." : "Todavía faltan datos para que las páginas estén listas.",
            });
            await liberarProteccion();
            alCambiar(guardado);
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, CAMPOS_LEGAL, form.setError)) setError(e);
        }
    }

    return (
        <FormularioPagina
            titulo="Datos legales"
            descripcion="Quién opera este servicio. Con estos datos Iris arma las páginas públicas de privacidad, términos y eliminación de datos que Meta pide para publicar la app de WhatsApp."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion"
            sucio={form.formState.isDirty}
            barra={{
                enviando: form.formState.isSubmitting,
                ...(configura ? {} : { faltan: ["el permiso"], motivo: "Puedes verlos, pero no tienes permiso para cambiarlos." }),
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los datos: recarga para ver el cambio." } : {}),
            }}
            error={
                <>
                    {datos.completo ? (
                        <Aviso tono="success" icono={CircleCheck} titulo="Datos completos: las páginas públicas están listas para Meta" data-testid="estado-legal" data-completo="true">
                            Copia los enlaces de abajo y pégalos en el panel de Meta.
                        </Aviso>
                    ) : (
                        <Aviso tono="warning" icono={FileWarning} titulo="Faltan datos legales: las páginas públicas no están listas para Meta" data-testid="estado-legal" data-completo="false">
                            <p>Las páginas ya se pueden abrir, pero sin estos datos no dicen quién es el responsable ni cómo contactarlo:</p>
                            <ul className="mt-1.5 list-disc pl-5" aria-label="Datos que faltan">
                                {faltan.map((f) => (
                                    <li key={f}>{f}</li>
                                ))}
                            </ul>
                        </Aviso>
                    )}
                    {error ? (
                        <Aviso
                            tono="error"
                            titulo={conflicto ? "Alguien más cambió los datos" : "No se pudo guardar"}
                            data-testid="error-legal"
                            accion={
                                conflicto ? (
                                    <Boton variante="secundario" tamano="sm" onClick={recargar}>
                                        <RotateCw />
                                        Recargar
                                    </Boton>
                                ) : undefined
                            }
                        >
                            {conflicto
                                ? "Recarga para ver lo que quedó guardado; lo que escribiste aquí se pierde."
                                : `${mensajeDeError(error).titulo}${error instanceof ErrorApi && error.detalle ? `: ${error.detalle}` : ""}. Vuelve a tocar «Guardar».`}
                        </Aviso>
                    ) : null}
                </>
            }
        >
            <SeccionFormulario titulo="Empresa" descripcion="El responsable de los datos de tus clientes. Aparece al inicio de cada página.">
                <Campo
                    etiqueta="Razón social"
                    placeholder="Tu Empresa, Sociedad Anónima"
                    ayuda="El nombre legal, como aparece en tus documentos."
                    maxLength={200}
                    autoComplete="organization"
                    disabled={!configura}
                    error={errores.razonSocial?.message}
                    {...form.register("razonSocial")}
                />
                <Campo
                    etiqueta="Nombre comercial"
                    placeholder="Tu Empresa"
                    ayuda="Como te conocen tus clientes. Debe coincidir con el nombre que usas en Meta."
                    maxLength={120}
                    autoComplete="off"
                    disabled={!configura}
                    error={errores.nombreComercial?.message}
                    {...form.register("nombreComercial")}
                />
            </SeccionFormulario>

            <SeccionFormulario titulo="Contacto de privacidad" descripcion="A dónde escribe un cliente para ver, corregir o eliminar sus datos. Es público.">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo
                        etiqueta="Correo de privacidad"
                        type="email"
                        inputMode="email"
                        placeholder="privacidad@tuempresa.com"
                        maxLength={254}
                        autoComplete="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        disabled={!configura}
                        error={errores.correoPrivacidad?.message}
                        {...form.register("correoPrivacidad")}
                    />
                    <Campo
                        etiqueta="Teléfono (opcional)"
                        type="tel"
                        inputMode="tel"
                        placeholder="+502 2222 3333"
                        maxLength={40}
                        autoComplete="off"
                        disabled={!configura}
                        error={errores.telefono?.message}
                        {...form.register("telefono")}
                    />
                </div>
            </SeccionFormulario>

            <SeccionFormulario titulo="Dirección y país" descripcion="Las leyes de este país son la ley aplicable que citan los términos del servicio.">
                <Campo
                    etiqueta="Dirección"
                    placeholder="Calle, número, zona y ciudad"
                    maxLength={300}
                    autoComplete="off"
                    disabled={!configura}
                    error={errores.direccion?.message}
                    {...form.register("direccion")}
                />
                <Campo etiqueta="País" placeholder="Guatemala" maxLength={80} autoComplete="off" disabled={!configura} error={errores.pais?.message} {...form.register("pais")} />
            </SeccionFormulario>

            <EnlacesMeta completo={datos.completo} />
        </FormularioPagina>
    );
}

const sinSuscripcion = () => () => undefined;

/** Abre la página como la abre Meta (sin cookies, sin seguir redirecciones) y dice si es la que debe ser. */
async function responde(e: EnlaceMeta, senal: AbortSignal): Promise<boolean> {
    try {
        const r = await fetch(e.ruta, { cache: "no-store", credentials: "omit", redirect: "manual", signal: senal });
        return paginaResponde(r.status, r.status === 200 ? await r.text() : "", e.clave);
    } catch {
        return false;
    }
}

/** Las seis URL públicas, para copiar y pegar en Meta, con «Abrir» y si cada una responde. */
function EnlacesMeta({ completo }: { completo: boolean }) {
    // El dominio es el del panel: en el servidor no se conoce.
    const origen = useSyncExternalStore(sinSuscripcion, () => window.location.origin, () => "");
    const [estados, setEstados] = useState<Record<string, EstadoEnlace>>({});
    const [ronda, setRonda] = useState(0);
    const enlaces = origen ? enlacesMeta(origen) : [];

    useEffect(() => {
        if (!origen) return undefined;
        const control = new AbortController();
        for (const e of enlacesMeta(origen)) {
            void responde(e, control.signal).then((ok) => {
                if (!control.signal.aborted) setEstados((s) => ({ ...s, [e.ruta]: ok ? "ok" : "error" }));
            });
        }
        return () => control.abort();
    }, [origen, ronda]);

    const porClave = (clave: ClavePagina) => enlaces.filter((e) => e.clave === clave);
    const algunaFalla = enlaces.some((e) => estados[e.ruta] === "error");

    return (
        <section aria-labelledby="titulo-enlaces-meta" data-slot="enlaces-meta" className="surface-card grid min-w-0 gap-4 p-4 sm:p-5 [&>*]:min-w-0">
            <div className="grid gap-1">
                <h2 id="titulo-enlaces-meta" className="text-base font-semibold tracking-[-0.015em]">
                    Enlaces para Meta
                </h2>
                <p className="text-sm text-muted-foreground">
                    Se pegan en developers.facebook.com › tu app › Configuración de la app › Básica. Usa los de inglés: quienes revisan la app en Meta leen inglés, y cada página enlaza a su versión en
                    español.
                </p>
            </div>
            {origen && !esHttps(origen) ? (
                <Aviso tono="warning" titulo="Meta solo acepta enlaces con https" data-testid="legal-sin-https">
                    Estás en una dirección sin https. Abre el panel con el dominio público de tu instalación para copiar los enlaces que Meta acepta.
                </Aviso>
            ) : null}
            {CLAVES.map((clave) => (
                <div key={clave} className="grid gap-3 border-t pt-4 first:border-t-0" data-pagina={clave}>
                    <div className="grid gap-0.5">
                        <h3 className="text-sm font-semibold">{TITULOS[clave].es}</h3>
                        <p className="text-sm text-muted-foreground">En Meta: «{CAMPO_META[clave]}».</p>
                    </div>
                    {porClave(clave).map((e) => (
                        <CampoCopiar
                            key={e.ruta}
                            etiqueta={`${nombreIdioma(e.idioma)}${e.idioma === "en" ? " (el que se pega en Meta)" : ""}`}
                            valor={e.url}
                            queSeCopio="Enlace copiado"
                            ayuda={<EstadoDeEnlace estado={estados[e.ruta] ?? "comprobando"} completo={completo} ruta={e.ruta} />}
                            acciones={
                                <Boton asChild variante="contorno">
                                    <a href={e.ruta} target="_blank" rel="noreferrer" aria-label={`Abrir ${e.titulo}`}>
                                        <ExternalLink aria-hidden />
                                        Abrir
                                    </a>
                                </Boton>
                            }
                        />
                    ))}
                </div>
            ))}
            <div className="flex flex-wrap items-center gap-3 border-t pt-4">
                <Boton variante="secundario" tamano="sm" onClick={() => { setEstados({}); setRonda((n) => n + 1); }}>
                    <RotateCw />
                    Volver a comprobar
                </Boton>
                {algunaFalla ? <span className="text-sm text-muted-foreground">Si una página no responde, avisa a quien administra el servidor de Iris.</span> : null}
            </div>
        </section>
    );
}

function EstadoDeEnlace({ estado, completo, ruta }: { estado: EstadoEnlace; completo: boolean; ruta: string }) {
    const comun = { "data-estado-enlace": estado, "data-ruta": ruta } as const;
    if (estado === "comprobando") {
        return <InsigniaEstado tono="neutral" icono={LoaderCircle} texto="Comprobando…" {...comun} />;
    }
    if (estado === "error") {
        return <InsigniaEstado tono="error" texto="No responde" {...comun} />;
    }
    return completo ? <InsigniaEstado tono="success" texto="Responde bien" {...comun} /> : <InsigniaEstado tono="warning" texto="Responde, pero faltan datos" {...comun} />;
}
