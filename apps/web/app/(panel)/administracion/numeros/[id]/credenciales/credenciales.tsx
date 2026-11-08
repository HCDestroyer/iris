"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoSecreto, DialogoConfirmar, FormularioPagina, SeccionFormulario } from "@iris/ui";
import { CircleCheck, Clock, Power, RefreshCw, SlidersHorizontal, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { EstadoConexion } from "@/componentes/numeros/estado-conexion";
import { useGenerarVerifyToken } from "@/componentes/numeros/generar-verify-token";
import { SecretoCambiable as Secreto } from "@/componentes/numeros/secreto-guardado";
import { TarjetaPrueba } from "@/componentes/numeros/tarjeta-prueba";
import { WebhookMeta } from "@/componentes/numeros/webhook-meta";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { avisoEstado } from "@/lib/avisos";
import { formatearNumero } from "@/lib/conversaciones/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { activarCanal, actualizarCanal, cuerpoActualizar, desactivarCanal, leerCanal, probarCanal } from "@/lib/numeros/api";
import { cuando, textoUltimoMensaje, webhookUrlDe } from "@/lib/numeros/conexion";
import { esquemaCredenciales, normalizarNumero, type EntradaCredenciales, type ValoresCredenciales } from "@/lib/numeros/esquemas";
import type { Canal, PruebaCanal } from "@/lib/numeros/tipos";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

const LISTA = "/administracion/numeros";
const CAMPOS = ["nombre", "numero", "phoneNumberId", "wabaId", "token", "appSecret"] as const;

/**
 * `/administracion/numeros/[id]/credenciales`: datos de Meta del número, token y app secret
 * («•••• guardado el …»: vacío = se queda el guardado), el webhook con «Generar otro», «Probar
 * conexión» y «Desactivar número» con confirmación.
 */
export function CredencialesNumero() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => (id === null ? null : leerCanal(id, signal)));
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el número" noEncontrado="Número no encontrado">
            {(canal) => <Formulario inicial={canal} recargar={lectura.reintentar} />}
        </CargaRegistro>
    );
}

function Formulario({ inicial, recargar }: { inicial: Canal; recargar: () => void }) {
    const router = useRouter();
    const { puede } = useSesion();
    const [canal, setCanal] = useState(inicial);
    const [error, setError] = useState<unknown>(null);
    const [cambiar, setCambiar] = useState({ token: inicial.conexion === "token_invalido" || inicial.conexion === "sin_permiso", appSecret: false });
    const [prueba, setPrueba] = useState<PruebaCanal | null>(null);
    const [errorPrueba, setErrorPrueba] = useState<unknown>(null);
    const [probando, setProbando] = useState(false);
    const [verifyToken, setVerifyToken] = useState<string | null>(null);
    const [confirmarDesactivar, setConfirmarDesactivar] = useState(false);
    const [errorEstado, setErrorEstado] = useState<unknown>(null);
    const clave = useClaveIdempotencia();
    const claveEstado = useClaveIdempotencia();
    const generar = useGenerarVerifyToken(canal, (nuevo, token) => {
        setCanal(nuevo);
        setVerifyToken(token || null);
    });
    const form = useForm<EntradaCredenciales, unknown, ValoresCredenciales>({
        resolver: zodResolver(esquemaCredenciales),
        defaultValues: { nombre: canal.nombre, numero: canal.numero, phoneNumberId: canal.phoneNumberId, wabaId: canal.wabaId, token: "", appSecret: "" },
    });
    const errores = form.formState.errors;

    async function probar() {
        setProbando(true);
        setErrorPrueba(null);
        try {
            const p = await probarCanal(canal.id, nuevaClaveIdempotencia());
            setPrueba(p);
            setCanal((c) => ({ ...c, conexion: p.conexion, ultimoEstado: p.estado, ultimoDetalle: p.detalle, ultimaPruebaEn: p.probadoEn, webhookVerificadoEn: p.webhookVerificadoEn, ultimoMensajeEn: p.ultimoMensajeEn }));
        } catch (e) {
            setErrorPrueba(e);
        } finally {
            setProbando(false);
        }
    }

    async function guardar(d: ValoresCredenciales) {
        setError(null);
        let nuevo: Canal;
        try {
            ({ canal: nuevo } = await actualizarCanal(
                canal.id,
                cuerpoActualizar(canal, {
                    nombre: d.nombre,
                    numero: d.numero,
                    phoneNumberId: d.phoneNumberId,
                    wabaId: d.wabaId,
                    // Vacío = se queda el guardado: no se manda.
                    ...(d.token ? { token: d.token } : {}),
                    ...(d.appSecret ? { appSecret: d.appSecret } : {}),
                }),
                clave.actual(),
            ));
            clave.renovar();
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 409 && e.codigo === "canal_existente") {
                form.setError("phoneNumberId", { message: "Ya hay un número activo con ese Phone number ID" });
                return;
            }
            if (!erroresACampos(e, CAMPOS, form.setError)) setError(e);
            return;
        }
        const cambioConexion = Boolean(d.token) || d.phoneNumberId !== canal.phoneNumberId || normalizarNumero(d.numero) !== canal.numero;
        setCanal(nuevo);
        setCambiar({ token: false, appSecret: false });
        form.reset({ nombre: nuevo.nombre, numero: nuevo.numero, phoneNumberId: nuevo.phoneNumberId, wabaId: nuevo.wabaId, token: "", appSecret: "" });
        avisar({ titulo: `Credenciales de «${nuevo.nombre}» guardadas` });
        // Cambió lo que Meta comprueba: se prueba de una vez (el servidor dejó la conexión «Sin probar»).
        if (cambioConexion) await probar();
    }

    async function cambiarEstado(activo: boolean) {
        setErrorEstado(null);
        try {
            const nuevo = activo ? await activarCanal(canal.id, claveEstado.actual()) : await desactivarCanal(canal.id, claveEstado.actual());
            claveEstado.renovar();
            setCanal(nuevo);
            setConfirmarDesactivar(false);
            avisar({ titulo: avisoEstado(nuevo.nombre, activo) });
        } catch (e) {
            claveEstado.trasError(e);
            setConfirmarDesactivar(false);
            setErrorEstado(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const url = webhookUrlDe(canal, typeof window === "undefined" ? "" : window.location.origin);
    return (
        <FormularioPagina
            titulo={`Credenciales de ${canal.nombre}`}
            descripcion={
                <span className="inline-flex flex-wrap items-center gap-x-1.5">
                    <span className="font-mono">{formatearNumero(canal.numero)}</span>
                    <span aria-hidden>·</span>
                    <EstadoConexion conexion={canal.conexion} />
                </span>
            }
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={LISTA}
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: "Guardar", enviando: form.formState.isSubmitting }}
            error={
                error ? (
                    <Aviso
                        tono="error"
                        titulo={conflicto ? "Alguien más cambió este número" : "No se pudo guardar"}
                        accion={
                            conflicto ? (
                                <Boton variante="contorno" tamano="sm" onClick={recargar}>
                                    <RefreshCw aria-hidden />
                                    Reintentar
                                </Boton>
                            ) : undefined
                        }
                    >
                        {conflicto ? "Vuelve a abrirlo para ver el cambio; lo que escribiste aquí se pierde." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            {puede(["canales.configurar"]) && canal.activo ? (
                <div className="-mt-2 flex justify-end">
                    <Boton asChild variante="contorno" tamano="sm" className="w-full sm:w-auto">
                        <Link href={`${LISTA}/${canal.id}/configurar`}>
                            <SlidersHorizontal aria-hidden />
                            Uso, cola y horario
                        </Link>
                    </Boton>
                </div>
            ) : null}

            {prueba || errorPrueba || probando ? (
                <TarjetaPrueba
                    prueba={prueba}
                    error={errorPrueba}
                    probando={probando}
                    numero={canal.numero}
                    acciones={{
                        alVolverAComprobar: () => void probar(),
                        alCambiarToken: () => {
                            setCambiar((c) => ({ ...c, token: true }));
                            window.setTimeout(() => form.setFocus("token"), 0);
                        },
                        credencialesHref: `${LISTA}/${canal.id}/credenciales`,
                        listaHref: LISTA,
                    }}
                />
            ) : null}

            <SeccionFormulario titulo="Datos del número">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" maxLength={120} error={errores.nombre?.message} {...form.register("nombre")} />
                    <Campo etiqueta="Número" inputMode="tel" autoComplete="off" className="[&_input]:font-mono" error={errores.numero?.message} {...form.register("numero")} />
                    <Campo etiqueta="Phone number ID" inputMode="numeric" autoComplete="off" className="[&_input]:font-mono" error={errores.phoneNumberId?.message} {...form.register("phoneNumberId")} />
                    <Campo
                        etiqueta={
                            <>
                                WABA ID <span className="font-normal text-muted-foreground">(opcional)</span>
                            </>
                        }
                        inputMode="numeric"
                        autoComplete="off"
                        className="[&_input]:font-mono"
                        error={errores.wabaId?.message}
                        {...form.register("wabaId")}
                    />
                </div>
            </SeccionFormulario>

            <SeccionFormulario titulo="Credenciales de Meta" descripcion="Iris no las vuelve a mostrar. Deja vacío lo que no quieras cambiar.">
                <Secreto
                    etiqueta="Token de acceso"
                    guardadoEn={canal.tokenGuardadoEn}
                    abierto={cambiar.token}
                    abrir={() => setCambiar((c) => ({ ...c, token: true }))}
                    cerrar={() => {
                        form.resetField("token");
                        setCambiar((c) => ({ ...c, token: false }));
                    }}
                    campo={<CampoSecreto etiqueta="Token de acceso" placeholder="Pega el token nuevo" error={errores.token?.message} {...form.register("token")} />}
                />
                <Secreto
                    etiqueta="App secret"
                    guardadoEn={canal.appSecretGuardadoEn}
                    abierto={cambiar.appSecret}
                    abrir={() => setCambiar((c) => ({ ...c, appSecret: true }))}
                    cerrar={() => {
                        form.resetField("appSecret");
                        setCambiar((c) => ({ ...c, appSecret: false }));
                    }}
                    campo={<CampoSecreto etiqueta="App secret" placeholder="Pega el app secret nuevo" error={errores.appSecret?.message} {...form.register("appSecret")} />}
                />
            </SeccionFormulario>

            <SeccionFormulario titulo="Webhook">
                <WebhookMeta url={url} verifyToken={verifyToken} verifyTokenGuardadoEn={canal.verifyTokenGuardadoEn} alGenerarOtro={generar.pedir}
                    generando={generar.generando}
                    error={generar.error}
                    alPegado={() => {
                        setVerifyToken(null);
                        avisar({ titulo: "Token de verificación oculto: ya no se muestra en esta pantalla" });
                    }}
                />
                <EstadoWebhook canal={canal} probando={probando} alProbar={() => void probar()} sucio={form.formState.isDirty} />
            </SeccionFormulario>

            <section aria-labelledby="titulo-estado-numero" className="surface-card grid gap-3 border-error/40 p-4 sm:p-5" data-zona-peligro>
                <h2 id="titulo-estado-numero" className="text-base font-semibold">
                    {canal.activo ? "Desactivar número" : "Número desactivado"}
                </h2>
                <p className="text-sm text-muted-foreground">
                    {canal.activo
                        ? "Iris deja de enviar y recibir por este número. Las conversaciones y su historial se quedan, y lo puedes activar otra vez."
                        : "Iris no envía ni recibe por este número. Sus conversaciones se quedan."}
                </p>
                {errorEstado ? (
                    <Aviso tono="error" titulo={canal.activo ? "No se pudo desactivar" : "No se pudo activar"}>
                        {mensajeDeError(errorEstado).titulo}
                    </Aviso>
                ) : null}
                {canal.activo ? (
                    <Boton variante="contorno" className="justify-self-start border-error/60 text-error" onClick={() => setConfirmarDesactivar(true)}>
                        <Power aria-hidden />
                        Desactivar número
                    </Boton>
                ) : (
                    <Boton variante="contorno" className="justify-self-start" onClick={() => void cambiarEstado(true)}>
                        <Power aria-hidden />
                        Activar número
                    </Boton>
                )}
            </section>

            <DialogoConfirmar
                open={confirmarDesactivar}
                onOpenChange={setConfirmarDesactivar}
                titulo={`¿Desactivar ${canal.nombre}?`}
                descripcion={`Iris deja de enviar y recibir por ${formatearNumero(canal.numero)}.`}
                textoConfirmar="Desactivar"
                textoOcupado="Desactivando…"
                peligro
                alConfirmar={() => cambiarEstado(false)}
            >
                {canal.avisosEmpresa ? (
                    <Aviso tono="warning" icono={TriangleAlert} titulo="Es el número de avisos de la empresa">
                        Los avisos de Argos quedan bloqueados hasta que marques otro número como «Número de avisos de la empresa».
                    </Aviso>
                ) : null}
            </DialogoConfirmar>
            {generar.dialogo}
        </FormularioPagina>
    );
}

function EstadoWebhook({ canal, probando, alProbar, sucio }: { canal: Canal; probando: boolean; alProbar: () => void; sucio: boolean }) {
    const verificado = canal.webhookVerificadoEn;
    return (
        <div className="flex flex-col gap-3 rounded-lg border border-border bg-background-secondary p-3 sm:flex-row sm:items-center sm:p-4" data-webhook={verificado ? "verificado" : "sin_verificar"}>
            <div className="flex min-w-0 flex-1 items-start gap-3">
                {verificado ? <CircleCheck aria-hidden className="mt-0.5 size-4 shrink-0 text-success" /> : <Clock aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />}
                <div className="grid min-w-0 gap-0.5 text-sm">
                    <p className="font-medium">{verificado ? `Verificado por Meta ${cuando(verificado)}` : "Meta aún no verificó el webhook"}</p>
                    <p className="text-muted-foreground">{textoUltimoMensaje(canal.ultimoMensajeEn)}</p>
                    {sucio ? <p className="text-muted-foreground">La prueba usa lo guardado: guarda primero lo que cambiaste.</p> : null}
                </div>
            </div>
            <Boton variante="contorno" onClick={alProbar} disabled={probando}>
                <RefreshCw aria-hidden />
                {probando ? "Probando…" : "Probar conexión"}
            </Boton>
        </div>
    );
}
