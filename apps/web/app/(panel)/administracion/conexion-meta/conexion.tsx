"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoSecreto, DialogoConfirmar, FormularioPagina, GrupoOpciones, SeccionFormulario } from "@iris/ui";
import { BookOpen, Cloud, Link2, Lock, RefreshCw, Server, Webhook } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { TarjetaEstado } from "@/componentes/meta/tarjeta-estado";
import { CampoCopiar } from "@/componentes/numeros/campo-copiar";
import { SecretoCambiable, SecretoGuardado } from "@/componentes/numeros/secreto-guardado";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { actualizarConexionMeta, leerConexionMeta, probarConexionMeta } from "@/lib/meta/api";
import { esquemaConexionMeta, type EntradaFormConexion, type ValoresFormConexion } from "@/lib/meta/esquemas";
import { NOMBRE_COMPROBACION, presentarConexionMeta, SERVICIO_CENTRAL, tonoComprobacion } from "@/lib/meta/presentacion";
import type { ConexionMeta } from "@/lib/meta/tipos";
import { cuando } from "@/lib/numeros/conexion";
import { enlaceGuia } from "@/lib/numeros/guia";
import { useSesion } from "@/lib/sesion";

const CAMPOS = ["appId", "configId", "graphVersion", "appSecret", "servicioUrl", "claveEnlace"] as const;

/**
 * `/administracion/conexion-meta` (`mensajeria.conexion_meta`, solo admin): una conexión para toda
 * la instalación, la que usan los números que se conectan con QR (contrato §15.1). Modos: app
 * propia, app compartida (servicio central Iris Cloud, todavía no disponible) y, en la nube, de
 * solo lectura.
 */
export function ConexionConMeta() {
    const lectura = useLectura((signal) => leerConexionMeta(signal));
    return (
        <CargaRegistro lectura={lectura} que="la conexión con Meta" noEncontrado="Conexión no encontrada">
            {(c) => (c.gestionada ? <Gestionada conexion={c} /> : <Formulario inicial={c} />)}
        </CargaRegistro>
    );
}

function AvisoInstalacion() {
    const { yo } = useSesion();
    const empresas = (yo.empresas ?? []).map((e) => e.nombre);
    const lista = empresas.length > 1 ? `${empresas.slice(0, -1).join(", ")} y ${empresas.at(-1)}` : empresas[0];
    return (
        <Aviso tono="info" icono={Server} titulo="Una sola conexión para toda la instalación">
            La usan {empresas.length > 1 ? `las ${empresas.length} empresas de este Iris (${lista})` : "todas las empresas de este Iris"} cuando conectan un número con QR. Los números por «datos
            de la API» no la necesitan.
        </Aviso>
    );
}

function Formulario({ inicial }: { inicial: ConexionMeta }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [conexion, setConexion] = useState(inicial);
    const [verifyToken, setVerifyToken] = useState<string | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [errorPrueba, setErrorPrueba] = useState<unknown>(null);
    const [probando, setProbando] = useState(false);
    const [generar, setGenerar] = useState<"cerrado" | "abierto">("cerrado");
    const [cambiar, setCambiar] = useState({ appSecret: !inicial.appSecretGuardadoEn, claveEnlace: !inicial.claveEnlaceGuardadaEn });
    const valores = (c: ConexionMeta): EntradaFormConexion => ({
        modo: c.modo === "compartida" ? "compartida" : "propia",
        appId: c.appId ?? "",
        configId: c.configId ?? "",
        graphVersion: c.graphVersionPropia ?? "",
        appSecret: "",
        servicioUrl: c.servicioUrl && c.servicioUrl !== SERVICIO_CENTRAL.url ? c.servicioUrl : "",
        claveEnlace: "",
        requiereSecreto: !c.appSecretGuardadoEn,
        requiereClave: !c.claveEnlaceGuardadaEn,
    });
    const form = useForm<EntradaFormConexion, unknown, ValoresFormConexion>({ resolver: zodResolver(esquemaConexionMeta), defaultValues: valores(inicial) });
    const modo = useWatch({ control: form.control, name: "modo" });
    const errores = form.formState.errors;
    const origen = typeof window === "undefined" ? "" : window.location.origin;
    const webhookUrl = conexion.webhookUrlCompleta ? conexion.webhookUrl : `${origen}${conexion.webhookUrl}`;
    const dominio = typeof window === "undefined" ? "" : window.location.hostname;

    async function probar() {
        setProbando(true);
        setErrorPrueba(null);
        try {
            setConexion(await probarConexionMeta(nuevaClaveIdempotencia()));
        } catch (e) {
            setErrorPrueba(e);
        } finally {
            setProbando(false);
        }
    }

    async function guardar(d: ValoresFormConexion) {
        setError(null);
        try {
            const r = await actualizarConexionMeta(
                d.modo === "propia"
                    ? { version: conexion.version, modo: "propia", appId: d.appId, configId: d.configId, graphVersion: d.graphVersion || null, ...(d.appSecret ? { appSecret: d.appSecret } : {}) }
                    : { version: conexion.version, modo: "compartida", servicioUrl: d.servicioUrl || null, ...(d.claveEnlace ? { claveEnlace: d.claveEnlace } : {}) },
                clave.actual(),
            );
            clave.renovar();
            setConexion(r.conexion);
            if (r.verifyToken) setVerifyToken(r.verifyToken);
            setCambiar({ appSecret: !r.conexion.appSecretGuardadoEn, claveEnlace: !r.conexion.claveEnlaceGuardadaEn });
            form.reset(valores(r.conexion));
            avisar({ titulo: "Conexión con Meta guardada" });
            if (d.modo === "propia") await probar();
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, CAMPOS, form.setError)) setError(e);
        }
    }

    async function generarOtro() {
        try {
            const r = await actualizarConexionMeta(
                { version: conexion.version, modo: "propia", appId: conexion.appId ?? "", configId: conexion.configId ?? "", graphVersion: conexion.graphVersionPropia, generarVerifyToken: true },
                nuevaClaveIdempotencia(),
            );
            setConexion(r.conexion);
            setVerifyToken(r.verifyToken);
            form.reset({ ...form.getValues(), requiereSecreto: !r.conexion.appSecretGuardadoEn });
            avisar({ titulo: "Token de verificación nuevo: pégalo en Meta" });
        } catch (e) {
            setError(e);
        } finally {
            setGenerar("cerrado");
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const estado = presentarConexionMeta(conexion);
    const guardadaEnOtroModo = conexion.modo !== "sin_configurar" && conexion.modo !== modo;
    return (
        <FormularioPagina
            titulo="Conexión con Meta"
            descripcion="Cómo se conecta este Iris con Meta para el registro con QR."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion"
            sucio={form.formState.isDirty}
            barra={{
                textoGuardar: modo === "propia" ? "Guardar y probar" : "Guardar",
                enviando: form.formState.isSubmitting,
                nota: "Cambiar el modo no desconecta los números que ya están conectados.",
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió la conexión" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <div className="-mt-2 flex justify-end">
                <Boton asChild variante="contorno" tamano="sm" className="w-full sm:w-auto">
                    <Link href={enlaceGuia()}>
                        <BookOpen aria-hidden />
                        Ver la guía
                    </Link>
                </Boton>
            </div>
            <AvisoInstalacion />
            <SeccionFormulario titulo="Modo">
                <GrupoOpciones<"propia" | "compartida">
                    leyenda={<span className="sr-only">Modo de la conexión</span>}
                    nombre="modo"
                    valor={modo}
                    alCambiar={(v) => form.setValue("modo", v, { shouldDirty: true })}
                    opciones={[
                        { valor: "propia", etiqueta: "App propia", icono: Server, detalle: "Tu ISP pone su app de Meta. Todo se queda en tu servidor. Para el QR, tu app debe ser Tech Provider aprobado." },
                        { valor: "compartida", etiqueta: "App compartida", icono: Cloud, detalle: `Enlazas esta instalación al servicio central ${SERVICIO_CENTRAL.nombre} con una clave. No necesitas app ni aprobación de Meta.` },
                    ]}
                />
                {guardadaEnOtroModo ? <p className="text-sm text-muted-foreground">Al guardar, la conexión pasa a este modo. Los números ya conectados siguen funcionando.</p> : null}
            </SeccionFormulario>

            {modo === "propia" ? (
                <>
                    <SeccionFormulario titulo="Tu app de Meta" descripcion="Está en developers.facebook.com → tu app. Iris guarda el secreto cifrado y no lo vuelve a mostrar.">
                        <div className="grid gap-4 sm:grid-cols-2">
                            <Campo etiqueta="App ID" inputMode="numeric" autoComplete="off" className="[&_input]:font-mono" ayuda="Configuración de la app → Básica." error={errores.appId?.message} {...form.register("appId")} />
                            <Campo
                                etiqueta={
                                    <>
                                        Versión de la Graph API <span className="font-normal text-muted-foreground">(opcional)</span>
                                    </>
                                }
                                autoComplete="off"
                                placeholder={conexion.graphVersion}
                                className="[&_input]:font-mono"
                                ayuda={`Vacío: usa la de Iris (${conexion.graphVersion}).`}
                                error={errores.graphVersion?.message}
                                {...form.register("graphVersion")}
                            />
                        </div>
                        <SecretoCambiable
                            etiqueta="App secret"
                            guardadoEn={conexion.appSecretGuardadoEn}
                            abierto={cambiar.appSecret}
                            abrir={() => setCambiar((c) => ({ ...c, appSecret: true }))}
                            cerrar={() => {
                                form.resetField("appSecret");
                                setCambiar((c) => ({ ...c, appSecret: false }));
                            }}
                            campo={<CampoSecreto etiqueta="App secret" placeholder="Pega el app secret" error={errores.appSecret?.message} {...form.register("appSecret")} />}
                        />
                        <Campo
                            etiqueta="ID de configuración (Facebook Login para empresas)"
                            inputMode="numeric"
                            autoComplete="off"
                            className="[&_input]:font-mono"
                            ayuda="La configuración de tipo «WhatsApp Embedded Signup». Facebook Login para empresas → Configuraciones."
                            error={errores.configId?.message}
                            {...form.register("configId")}
                        />
                        <CampoCopiar
                            etiqueta="Dominio permitido"
                            valor={dominio || null}
                            queSeCopio="Dominio copiado"
                            ayuda="Agrégalo en Facebook Login para empresas → Configuración → «Dominios permitidos para el SDK de JavaScript». Sin él, la ventana de Meta no abre."
                        />
                    </SeccionFormulario>

                    {conexion.modo === "propia" ? (
                        <TarjetaEstado
                            etiqueta={`Probar conexión${conexion.ultimaPruebaEn ? ` · ${cuando(conexion.ultimaPruebaEn)}` : ""}`}
                            titulo={probando ? "Probando con Meta…" : errorPrueba ? "No se pudo probar" : estado.titulo}
                            texto={errorPrueba ? `${mensajeDeError(errorPrueba).titulo}.` : estado.texto}
                            tono={errorPrueba ? "error" : estado.tono}
                            data-estado={conexion.estado}
                            accion={
                                <Boton variante="contorno" onClick={() => void probar()} disabled={probando}>
                                    <RefreshCw aria-hidden />
                                    {errorPrueba ? "Reintentar" : conexion.ultimaPruebaEn ? "Volver a probar" : "Probar"}
                                </Boton>
                            }
                            lineas={conexion.comprobaciones.map((c) => ({ clave: c.clave, titulo: NOMBRE_COMPROBACION[c.clave], texto: c.detalle, tono: tonoComprobacion(c.estado) }))}
                        />
                    ) : null}

                    {conexion.modo === "propia" ? (
                        <SeccionFormulario titulo="Webhook de la app" descripcion="Meta manda aquí los mensajes de todos los números conectados con QR, también los que se escriben desde el teléfono.">
                            <CampoCopiar etiqueta="URL de devolución de llamada" valor={webhookUrl} queSeCopio="URL del webhook copiada" />
                            {verifyToken ? (
                                <div className="grid gap-2">
                                    <CampoCopiar etiqueta="Token de verificación" valor={verifyToken} secreto queSeCopio="Token de verificación copiado" ayuda="Lo generó Iris. Se muestra solo en esta pantalla: pégalo en Meta antes de salir." />
                                    <Boton
                                        variante="contorno"
                                        tamano="sm"
                                        className="justify-self-start"
                                        onClick={() => {
                                            setVerifyToken(null);
                                            avisar({ titulo: "Token de verificación oculto: ya no se muestra en esta pantalla" });
                                        }}
                                    >
                                        Ya lo pegué en Meta
                                    </Boton>
                                </div>
                            ) : (
                                <SecretoGuardado etiqueta="Token de verificación" guardadoEn={conexion.verifyTokenGuardadoEn} accion="Generar otro" alAccion={() => setGenerar("abierto")} />
                            )}
                            <p className="text-sm text-muted-foreground">
                                {conexion.webhookVerificadoEn ? `Verificado por Meta ${cuando(conexion.webhookVerificadoEn)}.` : "Meta aún no verificó el webhook."}
                                {conexion.ultimoWebhookEn ? ` Último mensaje de Meta ${cuando(conexion.ultimoWebhookEn)}.` : ""}
                            </p>
                            <div className="grid gap-1.5">
                                <p className="text-sm font-medium">En Meta, suscríbete a estos campos</p>
                                <ul className="flex flex-wrap gap-1.5" aria-label="Campos del webhook">
                                    {conexion.camposWebhook.map((c) => (
                                        <li key={c} className="rounded-full border border-border bg-background-secondary px-2 py-0.5 font-mono text-xs">
                                            {c}
                                        </li>
                                    ))}
                                </ul>
                                <p className="text-sm text-muted-foreground">«smb_message_echoes» trae lo que se manda desde el teléfono; «history», el historial que el cliente comparte.</p>
                            </div>
                        </SeccionFormulario>
                    ) : null}
                </>
            ) : (
                <>
                    <SeccionFormulario titulo="Enlace con el servicio central" descripcion={`${SERVICIO_CENTRAL.nombre} te da la clave de enlace. Iris la guarda cifrada y no la vuelve a mostrar.`}>
                        <Campo
                            etiqueta="Servicio central"
                            autoComplete="off"
                            placeholder={SERVICIO_CENTRAL.url}
                            className="[&_input]:font-mono"
                            ayuda={`Vacío: ${SERVICIO_CENTRAL.url}. Cámbialo solo si te lo piden.`}
                            error={errores.servicioUrl?.message}
                            {...form.register("servicioUrl")}
                        />
                        <SecretoCambiable
                            etiqueta="Clave de enlace"
                            guardadoEn={conexion.claveEnlaceGuardadaEn}
                            abierto={cambiar.claveEnlace}
                            abrir={() => setCambiar((c) => ({ ...c, claveEnlace: true }))}
                            cerrar={() => {
                                form.resetField("claveEnlace");
                                setCambiar((c) => ({ ...c, claveEnlace: false }));
                            }}
                            campo={<CampoSecreto etiqueta="Clave de enlace" placeholder="Pega la clave de enlace" error={errores.claveEnlace?.message} {...form.register("claveEnlace")} />}
                        />
                    </SeccionFormulario>
                    <TarjetaEstado
                        etiqueta="Estado del enlace"
                        titulo="Servicio central no disponible todavía"
                        texto={`${SERVICIO_CENTRAL.nombre} (${SERVICIO_CENTRAL.url}) aún no está en servicio. Puedes guardar la configuración; mientras tanto, el QR no está disponible y los números se conectan con los datos de la API.`}
                        tono="warning"
                        data-estado="sin_servicio_central"
                    />
                    <SeccionFormulario titulo="Qué hará el servicio central">
                        <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
                            {[
                                { icono: Link2, titulo: "Cambiará con Meta el código del registro", texto: "Cuando alguien conecte un número con QR, recibirá el código de Meta y lo cambiará por el acceso al número." },
                                { icono: Webhook, titulo: "Te reenviará los webhooks", texto: `Meta le avisará a ${SERVICIO_CENTRAL.nombre} y este se lo pasará a este Iris firmado con tu clave de enlace.` },
                                { icono: Lock, titulo: "Nunca entregará el app secret", texto: "Tus mensajes no se guardarán allá: solo pasarán." },
                            ].map((x) => (
                                <li key={x.titulo} className="flex items-start gap-3 p-3 sm:p-4">
                                    <x.icono aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                                    <div className="grid gap-0.5 text-sm">
                                        <p className="font-medium">{x.titulo}</p>
                                        <p className="text-muted-foreground">{x.texto}</p>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    </SeccionFormulario>
                </>
            )}
            <DialogoConfirmar
                open={generar === "abierto"}
                onOpenChange={(a) => setGenerar(a ? "abierto" : "cerrado")}
                titulo="¿Generar otro token de verificación?"
                descripcion="El de ahora deja de valer y el webhook de la app queda sin verificar hasta que pegues el nuevo en Meta y pulses «Verificar y guardar»."
                textoConfirmar="Generar otro"
                textoOcupado="Generando…"
                alConfirmar={generarOtro}
            />
        </FormularioPagina>
    );
}

/** Iris en la nube: la conexión la fija el entorno y aquí solo se ve. */
function Gestionada({ conexion }: { conexion: ConexionMeta }) {
    const estado = presentarConexionMeta(conexion);
    return (
        <div className="mx-auto grid w-full max-w-3xl min-w-0 gap-4">
            <EncabezadoPagina titulo="Conexión con Meta" descripcion={`Iris en la nube de ${SERVICIO_CENTRAL.nombre}`} />
            <section className="surface-card grid gap-4 p-4 sm:p-5" aria-label="Modo">
                <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-base font-semibold">Modo</h2>
                    <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background-secondary px-2 py-0.5 text-xs font-medium">
                        <Lock aria-hidden className="size-3.5" />
                        Administrado por {SERVICIO_CENTRAL.nombre}
                    </span>
                </div>
                <Aviso tono="neutral" titulo="No se puede cambiar en Iris en la nube">
                    {SERVICIO_CENTRAL.nombre} administra la app de Meta. Tú solo conectas tus números con QR o con los datos de la API.
                </Aviso>
            </section>
            <TarjetaEstado
                etiqueta="Estado"
                titulo={estado.titulo}
                texto={conexion.ultimoWebhookEn ? `Último webhook recibido ${cuando(conexion.ultimoWebhookEn)}.` : estado.texto}
                tono={estado.tono}
                data-estado={conexion.estado}
                lineas={[
                    { clave: "qr", titulo: conexion.qrDisponible ? "Registro con QR disponible" : "Registro con QR no disponible", texto: conexion.qrDisponible ? "Puedes agregar números de WhatsApp Business con QR." : "Por ahora, conecta los números con los datos de la API.", tono: conexion.qrDisponible ? "success" : "warning" },
                    ...conexion.comprobaciones.map((c) => ({ clave: c.clave, titulo: NOMBRE_COMPROBACION[c.clave], texto: c.detalle, tono: tonoComprobacion(c.estado) })),
                ]}
            />
        </div>
    );
}
