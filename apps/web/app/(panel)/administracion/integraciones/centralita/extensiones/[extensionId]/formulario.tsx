"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoSecreto, Esqueleto, FormularioPagina, GrupoOpciones, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { PlugZap, RotateCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { ErrorLectura } from "@/componentes/error-lectura";
import { SecretoCambiable } from "@/componentes/numeros/secreto-guardado";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { leerCentralita, leerExtensiones } from "@/lib/centralita/api";
import type { Centralita, ExtensionCentralita } from "@/lib/centralita/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { esquemaExtension, textoPruebaRegistro, type ValoresExtension } from "@/lib/telefono/administracion";
import { guardarTelefonoExtension, probarRegistro } from "@/lib/telefono/api";
import type { PruebaRegistro } from "@/lib/telefono/tipos";

const VOLVER = "/administracion/integraciones/centralita";

/**
 * `/administracion/integraciones/centralita/extensiones/{id}` (`centralita.asignar_extension`): dónde
 * le suenan las llamadas a una persona (E4c). «En Iris» necesita la contraseña de su extensión en
 * Issabel, que se escribe una vez: Iris la guarda cifrada y no la vuelve a mostrar (ni el navegador
 * ni el celular la reciben). Al guardar, Iris prueba el registro con la guardada.
 */
export function ExtensionTelefono({ extensionId }: { extensionId: number }) {
    const lectura = useLectura(async (signal) => {
        const c = await leerCentralita(signal);
        if (!c) return null;
        const x = (await leerExtensiones(c.id, signal))?.find((e) => e.id === extensionId) ?? null;
        return x ? { c, x } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" && lectura.datos ? lectura.datos.x.usuarioNombre : null);
    if (lectura.estado === "cargando") return <Esqueleto className="h-64" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="la extensión" />;
    if (!lectura.datos) {
        return (
            <Aviso tono="warning" titulo="Esta extensión ya no existe">
                Pudo haberse quitado. Vuelve a Centralita para ver las de ahora.
            </Aviso>
        );
    }
    return <Formulario centralita={lectura.datos.c} extension={lectura.datos.x} />;
}

function Formulario({ centralita: c, extension: x }: { centralita: Centralita; extension: ExtensionCentralita }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const guardada = !!x.sipGuardadoEn;
    const form = useForm<ValoresExtension>({
        resolver: zodResolver(esquemaExtension),
        defaultValues: { tipo: x.tipo, sipUsuario: x.sipUsuario ?? x.extension, cambiarSecreto: !guardada, sipSecreto: "" },
    });
    const v = useWatch({ control: form.control });
    const [prueba, setPrueba] = useState<PruebaRegistro | null>(null);
    const [probando, setProbando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const errores = form.formState.errors;
    const navegador = v.tipo === "navegador";
    const usuarioCambiado = guardada && (v.sipUsuario ?? "").trim() !== (x.sipUsuario ?? "");

    async function probar() {
        setProbando(true);
        setError(null);
        try {
            setPrueba(await probarRegistro(c.id, x.id));
        } catch (e) {
            setError(e);
        } finally {
            setProbando(false);
        }
    }

    return (
        <FormularioPagina
            titulo={`Extensión de ${x.usuarioNombre}`}
            descripcion="Dónde le suenan las llamadas de la centralita."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                if (d.tipo === "navegador" && usuarioCambiado && !d.cambiarSecreto) {
                    form.setValue("cambiarSecreto", true);
                    form.setError("sipSecreto", { message: "Cambiaste el usuario SIP: escribe otra vez la contraseña" }, { shouldFocus: true });
                    return;
                }
                try {
                    const cuerpo =
                        d.tipo === "navegador"
                            ? { tipo: "navegador" as const, sipUsuario: d.sipUsuario.trim(), ...(d.cambiarSecreto ? { sipSecreto: d.sipSecreto } : {}) }
                            : { tipo: "escritorio" as const };
                    const nueva = await guardarTelefonoExtension(c.id, x.id, cuerpo, clave.actual());
                    clave.renovar();
                    if (nueva.tipo === "navegador") {
                        // Probar el registro con la guardada: sin «ok» la persona no puede abrir el teléfono.
                        const p = await probarRegistro(c.id, x.id);
                        const r = textoPruebaRegistro(p, x.extension);
                        if (p.estado !== "ok") {
                            setPrueba(p);
                            form.reset({ tipo: "navegador", sipUsuario: nueva.sipUsuario ?? d.sipUsuario, cambiarSecreto: false, sipSecreto: "" });
                            avisar({ tono: "warning", titulo: "Extensión guardada", descripcion: r.titulo });
                            return;
                        }
                        avisar({ titulo: `Extensión guardada: ${r.titulo.toLowerCase()}` });
                    } else {
                        avisar({ titulo: "Extensión guardada: suena en su teléfono de escritorio" });
                    }
                    await liberarProteccion();
                    router.replace(VOLVER);
                } catch (e) {
                    clave.trasError(e);
                    if (e instanceof ErrorApi && e.codigo === "clave_requerida") {
                        form.setValue("cambiarSecreto", true);
                        form.setError("sipSecreto", { message: "Escribe otra vez la contraseña de la extensión" }, { shouldFocus: true });
                    } else if (!erroresACampos(e, ["sipUsuario", "sipSecreto"], form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA={VOLVER}
            sucio={form.formState.isDirty}
            barra={{ enviando: form.formState.isSubmitting, nota: navegador ? "Al guardar, Iris prueba el registro con la contraseña guardada." : undefined }}
            error={
                error ? (
                    <Aviso tono="error" titulo="No se pudo guardar" data-slot="error-guardar">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Extensión">
                <div className="grid gap-3 sm:grid-cols-2">
                    <Campo etiqueta="Persona" value={x.usuarioNombre} disabled readOnly />
                    <Campo etiqueta="Extensión" value={x.extension} disabled readOnly ayuda="Como aparece en Issabel." />
                </div>
                <GrupoOpciones
                    leyenda="Dónde contesta"
                    nombre="tipo-extension"
                    apiladas
                    opciones={[
                        {
                            valor: "navegador",
                            etiqueta: "En Iris",
                            detalle: "Contesta y llama desde el panel con audífonos. Necesita la contraseña de la extensión.",
                        },
                        { valor: "escritorio", etiqueta: "En su teléfono de escritorio", detalle: "Iris solo le muestra quién llama; contesta en el aparato." },
                    ]}
                    valor={v.tipo}
                    alCambiar={(t) => form.setValue("tipo", t, { shouldDirty: true })}
                />
            </SeccionFormulario>
            {navegador ? (
                <SeccionFormulario titulo="Credencial de la extensión">
                    <Campo etiqueta="Usuario SIP" autoComplete="off" spellCheck={false} className="font-mono" ayuda="Casi siempre es el mismo número de la extensión." error={errores.sipUsuario?.message} {...form.register("sipUsuario")} />
                    {usuarioCambiado && !v.cambiarSecreto ? <p className="text-sm text-warning">Cambiaste el usuario SIP: escribe otra vez la contraseña.</p> : null}
                    <SecretoCambiable
                        etiqueta="Contraseña SIP"
                        femenino
                        guardadoEn={x.sipGuardadoEn}
                        abierto={v.cambiarSecreto ?? !guardada}
                        abrir={() => form.setValue("cambiarSecreto", true)}
                        cerrar={() => {
                            form.setValue("sipSecreto", "");
                            form.clearErrors("sipSecreto");
                            form.setValue("cambiarSecreto", false);
                        }}
                        campo={
                            <CampoSecreto
                                etiqueta="Contraseña SIP"
                                ayuda={`La de la extensión en Issabel (PBX › Extensiones › Secret). Iris la guarda cifrada en el servidor y no la vuelve a mostrar, ni a ti ni a ${x.usuarioNombre}. Ni el navegador ni el celular la reciben.`}
                                error={errores.sipSecreto?.message}
                                {...form.register("sipSecreto")}
                            />
                        }
                    />
                    {guardada && !form.formState.isDirty ? (
                        <div>
                            <Boton variante="secundario" onClick={() => void probar()} disabled={probando}>
                                <PlugZap />
                                {probando ? "Probando…" : "Probar registro"}
                            </Boton>
                        </div>
                    ) : null}
                    {prueba ? <ResultadoRegistro prueba={prueba} extension={x.extension} alReintentar={() => void probar()} /> : x.sipGuardadoEn && !x.sipProbadoEn ? (
                        <Aviso tono="warning" titulo="Falta probar el registro">
                            Sin una prueba que pase, {x.usuarioNombre} no puede abrir el teléfono.
                        </Aviso>
                    ) : null}
                </SeccionFormulario>
            ) : null}
        </FormularioPagina>
    );
}

function ResultadoRegistro({ prueba, extension, alReintentar }: { prueba: PruebaRegistro; extension: string; alReintentar: () => void }) {
    const r = textoPruebaRegistro(prueba, extension);
    return (
        <Aviso
            tono={r.tono === "success" ? "success" : r.tono === "warning" ? "warning" : "error"}
            titulo={r.titulo}
            data-slot="resultado-registro"
            data-estado={prueba.estado}
            accion={
                prueba.estado !== "ok" ? (
                    <Boton variante="secundario" tamano="sm" onClick={alReintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                ) : undefined
            }
        >
            {r.detalle}
        </Aviso>
    );
}
