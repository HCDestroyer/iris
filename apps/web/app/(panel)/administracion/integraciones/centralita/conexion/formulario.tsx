"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoSecreto, Casilla, Esqueleto, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { RefreshCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { SecretoCambiable } from "@/componentes/numeros/secreto-guardado";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { activarCentralita, actualizarCentralita, crearCentralita, leerCentralita, probarCentralita, probarDatosCentralita } from "@/lib/centralita/api";
import { cambiaDestino, esquemaConexion, huellaLegible, valoresConexion, type ValoresConexion } from "@/lib/centralita/presentacion";
import type { Centralita, PruebaCentralita } from "@/lib/centralita/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { ResultadoPrueba } from "../centralita";

const VOLVER = "/administracion/integraciones/centralita";

/**
 * `/administracion/integraciones/centralita/conexion` (`centralita.configurar`): conectar o editar la
 * centralita Issabel (contratos/voz.md §3.12). Se prueba antes de guardar: Iris se conecta por TLS,
 * entra con el usuario y comprueba que solo pueda leer; el administrador compara la huella del
 * certificado con la de Issabel. Al guardar, Iris la prueba otra vez y la enciende.
 */
export function ConexionCentralita() {
    const lectura = useLectura((signal) => leerCentralita(signal));
    if (lectura.estado === "cargando") return <Esqueleto className="h-64" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="la centralita" />;
    return <Formulario centralita={lectura.datos} />;
}

function clavePrueba(v: Pick<ValoresConexion, "host" | "puerto" | "usuario" | "secreto" | "cambiarSecreto">): string {
    return [v.host.trim().toLowerCase(), v.puerto, v.usuario.trim(), v.cambiarSecreto ? v.secreto : ""].join("|");
}

function Formulario({ centralita: c }: { centralita: Centralita | null }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const form = useForm<ValoresConexion>({ resolver: zodResolver(esquemaConexion), defaultValues: valoresConexion(c) });
    const valores = useWatch({ control: form.control });
    const [prueba, setPrueba] = useState<{ clave: string; resultado: PruebaCentralita } | null>(null);
    const [huellaOk, setHuellaOk] = useState(false);
    const [probando, setProbando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const errores = form.formState.errors;
    const v = { host: valores.host ?? "", puerto: valores.puerto ?? 5039, usuario: valores.usuario ?? "", secreto: valores.secreto ?? "", cambiarSecreto: valores.cambiarSecreto ?? false };
    const destinoCambiado = cambiaDestino(v, c);
    const vigente = prueba !== null && prueba.clave === clavePrueba(v) ? prueba.resultado : null;
    const listo = vigente?.estado === "ok" && huellaOk;
    const faltan = listo ? [] : vigente?.estado === "ok" ? ["confirmar la huella"] : ["probar la conexión"];

    async function probar() {
        const ok = await form.trigger(["host", "usuario", "secreto"]);
        if (!ok) return;
        if (destinoCambiado && !v.cambiarSecreto) {
            form.setValue("cambiarSecreto", true);
            form.setError("secreto", { message: "Cambiaste la dirección o el usuario: escribe otra vez la contraseña" }, { shouldFocus: true });
            return;
        }
        setProbando(true);
        setError(null);
        setHuellaOk(false);
        try {
            const r = await probarDatosCentralita({
                host: v.host.trim(),
                puerto: v.puerto,
                usuario: v.usuario.trim(),
                ...(v.cambiarSecreto ? { secreto: v.secreto } : {}),
                ...(c ? { centralitaId: c.id } : {}),
            });
            setPrueba({ clave: clavePrueba(v), resultado: r });
        } catch (e) {
            if (!erroresACampos(e, ["host", "puerto", "usuario", "secreto"], form.setError)) setError(e);
        } finally {
            setProbando(false);
        }
    }

    return (
        <FormularioPagina
            titulo="Conexión con Issabel"
            descripcion="Iris solo escucha la centralita. Prueba antes de guardar."
            alEnviar={form.handleSubmit(async (d) => {
                if (!vigente || vigente.estado !== "ok" || !vigente.huella) return;
                setError(null);
                try {
                    const cuerpo = {
                        nombre: d.nombre.trim(),
                        host: d.host.trim(),
                        puerto: d.puerto,
                        usuario: d.usuario.trim(),
                        huellaTls: vigente.huella,
                        ...(d.cambiarSecreto ? { secreto: d.secreto } : {}),
                    };
                    const guardada = c
                        ? await actualizarCentralita(c.id, { ...cuerpo, version: c.version }, clave.actual())
                        : await crearCentralita({ ...cuerpo, secreto: d.secreto }, clave.actual());
                    clave.renovar();
                    // Probar la guardada (con su huella) y encenderla: un paso menos para quien configura.
                    let encendida = guardada.activa;
                    if (!encendida) {
                        const p = await probarCentralita(guardada.id);
                        if (p.estado === "ok" && p.centralita) {
                            await activarCentralita(guardada.id, p.centralita.version);
                            encendida = true;
                        }
                    }
                    avisar({ titulo: encendida ? "Centralita guardada y encendida" : "Centralita guardada" });
                    await liberarProteccion();
                    router.replace(VOLVER);
                } catch (e) {
                    clave.trasError(e);
                    if (e instanceof ErrorApi && e.codigo === "clave_requerida") {
                        form.setValue("cambiarSecreto", true);
                        form.setError("secreto", { message: "Escribe otra vez la contraseña del AMI" }, { shouldFocus: true });
                    } else if (!erroresACampos(e, ["nombre", "host", "puerto", "usuario", "secreto"], form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA={VOLVER}
            sucio={form.formState.isDirty}
            barra={{ enviando: form.formState.isSubmitting, faltan, nota: listo ? "Al guardar, Iris la prueba otra vez y la enciende." : undefined }}
            error={
                error ? (
                    <Aviso tono="error" titulo={error instanceof ErrorApi && error.estado === 409 ? "Alguien más cambió la centralita" : "No se pudo guardar"} data-slot="error-guardar">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Centralita">
                <div className="grid gap-3 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" ayuda="Para reconocerla en Iris." error={errores.nombre?.message} {...form.register("nombre")} />
                    <Campo
                        etiqueta="Dirección"
                        className="font-mono"
                        autoComplete="off"
                        spellCheck={false}
                        placeholder="10.20.0.5"
                        ayuda="IP o nombre dentro de tu red o VPN."
                        error={errores.host?.message}
                        {...form.register("host")}
                    />
                </div>
            </SeccionFormulario>
            <SeccionFormulario titulo="Usuario de solo lectura (AMI)">
                <div className="grid gap-3 sm:grid-cols-2">
                    <Campo etiqueta="Usuario" autoComplete="off" spellCheck={false} placeholder="iris_lectura" error={errores.usuario?.message} {...form.register("usuario")} />
                    {c ? (
                        <div className="grid gap-1.5">
                            {destinoCambiado && !v.cambiarSecreto ? <p className="text-sm text-warning">Cambiaste la dirección o el usuario: escribe otra vez la contraseña.</p> : null}
                            <SecretoCambiable
                                etiqueta="Contraseña"
                                femenino
                                guardadoEn={c.secretoGuardadoEn}
                                abierto={v.cambiarSecreto}
                                abrir={() => form.setValue("cambiarSecreto", true)}
                                cerrar={() => {
                                    form.setValue("secreto", "");
                                    form.clearErrors("secreto");
                                    form.setValue("cambiarSecreto", false);
                                }}
                                campo={<CampoSecreto etiqueta="Contraseña" error={errores.secreto?.message} {...form.register("secreto")} />}
                            />
                        </div>
                    ) : (
                        <CampoSecreto etiqueta="Contraseña" ayuda="Se guarda cifrada y nadie la vuelve a ver." error={errores.secreto?.message} {...form.register("secreto")} />
                    )}
                </div>
                <details className="border-t border-border pt-3">
                    <summary className="cursor-pointer text-sm font-semibold text-muted-foreground">Avanzado</summary>
                    <p className="mt-2 text-sm text-muted-foreground">
                        Puerto <span className="font-mono">{v.puerto}</span>: el AMI cifrado (TLS). El 5038 sin cifrar no se acepta.
                    </p>
                </details>
            </SeccionFormulario>
            <SeccionFormulario titulo="Probar">
                <p className="text-sm text-muted-foreground">Iris se conecta, entra con el usuario y comprueba que solo pueda leer. No guarda nada.</p>
                <div>
                    <Boton variante="secundario" onClick={() => void probar()} disabled={probando}>
                        <RefreshCw />
                        {probando ? "Probando…" : "Probar conexión"}
                    </Boton>
                </div>
                {vigente ? <ResultadoPrueba prueba={vigente} /> : null}
                {vigente?.estado === "ok" && vigente.huella ? (
                    <div className="grid gap-2 rounded-lg border border-border p-3" data-slot="huella">
                        <p className="text-sm font-semibold">Huella del certificado</p>
                        <p className="font-mono text-xs break-all">{huellaLegible(vigente.huella)}</p>
                        <Casilla
                            etiqueta="Es la misma que muestra mi Issabel. Si un día cambia, Iris se detiene y me avisa."
                            checked={huellaOk}
                            onChange={(e) => setHuellaOk(e.target.checked)}
                        />
                    </div>
                ) : null}
            </SeccionFormulario>
            <SeccionFormulario titulo="Cómo preparar Issabel">
                <details>
                    <summary className="cursor-pointer text-sm font-semibold text-muted-foreground">Para quien administra la centralita</summary>
                    <div className="mt-2 grid gap-2 text-sm text-muted-foreground">
                        <p>
                            En <span className="font-mono">/etc/asterisk/manager_custom.conf</span>, un usuario solo para Iris, que entre solo desde la IP de Iris y solo pueda leer
                            llamadas:
                        </p>
                        <pre className="overflow-x-auto rounded-lg bg-elevated p-3 font-mono text-xs text-foreground">
                            {"[iris_lectura]\nsecret = (una contraseña larga)\ndeny = 0.0.0.0/0.0.0.0\npermit = <IP de Iris>/255.255.255.255\nread = call,cdr\nwrite =\neventfilter = !Event: RTCP*"}
                        </pre>
                        <p>
                            Y el AMI cifrado en <span className="font-mono">manager.conf</span>: <span className="font-mono">tlsenable=yes</span>,{" "}
                            <span className="font-mono">tlsbindaddr=0.0.0.0:5039</span>. Nunca abras el 5038 ni el 5039 a internet: usa una VPN. La guía completa está en
                            «Centralita Issabel» de los manuales de instalación.
                        </p>
                    </div>
                </details>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
