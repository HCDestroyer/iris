"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, BarraGuardar, Boton, CampoSecreto, DialogoConfirmar, SeccionFormulario, useProteccionSalida } from "@iris/ui";
import { RefreshCw } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { EstadoConexion } from "@/componentes/numeros/estado-conexion";
import { useGenerarVerifyToken } from "@/componentes/numeros/generar-verify-token";
import { PiePagina } from "@/componentes/numeros/pie-pagina";
import { SecretoGuardado } from "@/componentes/numeros/secreto-guardado";
import { TarjetaPrueba } from "@/componentes/numeros/tarjeta-prueba";
import { TituloPaso } from "@/componentes/numeros/titulo-paso";
import { WebhookMeta } from "@/componentes/numeros/webhook-meta";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { formatearNumero } from "@/lib/conversaciones/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { actualizarCanal, cuerpoActualizar, leerCanal, probarCanal } from "@/lib/numeros/api";
import { webhookUrlDe } from "@/lib/numeros/conexion";
import { esquemaCambiarToken, type EntradaCambiarToken, type ValoresCambiarToken } from "@/lib/numeros/esquemas";
import { leerRecienCreado, olvidarRecienCreado, type RecienCreado } from "@/lib/numeros/recien-creado";
import type { Canal, PruebaCanal } from "@/lib/numeros/tipos";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

const LISTA = "/administracion/numeros";
const ID_FORM_TOKEN = "form-cambiar-token";

/**
 * `/administracion/numeros/[id]/conectar`: después de «Agregar número», el resultado de la prueba
 * con Meta, el webhook para pegar en Meta (con el token de verificación, una sola vez) y, si Meta
 * rechazó el token, el campo para cambiarlo y volver a probar. También se llega desde la lista;
 * entonces el token de verificación ya no se ve (está «Generar otro»).
 */
export function ConectarNumero() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const [reciente] = useState<RecienCreado | null>(() => (id === null ? null : leerRecienCreado(id)));
    useEffect(() => {
        olvidarRecienCreado();
    }, []);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        return reciente ? reciente.canal : leerCanal(id, signal);
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el número" noEncontrado="Número no encontrado">
            {(canal) => <Conectar inicial={canal} reciente={reciente} />}
        </CargaRegistro>
    );
}

function Conectar({ inicial, reciente }: { inicial: Canal; reciente: RecienCreado | null }) {
    const router = useRouter();
    const { puede } = useSesion();
    const [canal, setCanal] = useState(inicial);
    const [prueba, setPrueba] = useState<PruebaCanal | null>(reciente?.prueba ?? null);
    const [errorPrueba, setErrorPrueba] = useState<unknown>(reciente?.errorPrueba ?? null);
    const [probando, setProbando] = useState(false);
    const [verifyToken, setVerifyToken] = useState<string | null>(reciente?.verifyToken ?? null);
    const [cambiarSecreto, setCambiarSecreto] = useState(false);
    const [errorGuardar, setErrorGuardar] = useState<unknown>(null);
    const clave = useClaveIdempotencia();
    const generar = useGenerarVerifyToken(canal, (nuevo, token) => {
        setCanal(nuevo);
        setVerifyToken(token || null);
    });

    const estadoToken = prueba ? prueba.estado : canal.ultimoEstado;
    const pideToken = !probando && !errorPrueba && (estadoToken === "token_invalido" || estadoToken === "sin_permiso");
    const form = useForm<EntradaCambiarToken, unknown, ValoresCambiarToken>({ resolver: zodResolver(esquemaCambiarToken), defaultValues: { token: "", appSecret: "" } });
    const sucio = pideToken && form.formState.isDirty;
    const proteccion = useProteccionSalida(sucio, router.push);

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

    async function guardarToken(d: ValoresCambiarToken) {
        setErrorGuardar(null);
        try {
            const { canal: nuevo } = await actualizarCanal(canal.id, cuerpoActualizar(canal, { token: d.token, ...(d.appSecret ? { appSecret: d.appSecret } : {}) }), clave.actual());
            clave.renovar();
            setCanal(nuevo);
            form.reset({ token: "", appSecret: "" });
            setCambiarSecreto(false);
            avisar({ titulo: d.appSecret ? "Token y app secret guardados" : "Token guardado" });
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, ["token", "appSecret"], form.setError)) setErrorGuardar(e);
            return;
        }
        await probar();
    }

    const url = webhookUrlDe(canal, typeof window === "undefined" ? "" : window.location.origin);
    const conectado = !probando && prueba?.estado === "conectado" && prueba.webhookVerificado;
    const nota = conectado ? "Listo: el número ya recibe mensajes." : "Puedes terminar después: el número queda en la lista con su estado.";

    return (
        // Con su propio pie fijo: como `FormularioPagina`, oculta la barra inferior del teléfono.
        <div data-slot="formulario-pagina" className="mx-auto grid w-full max-w-3xl min-w-0 gap-4 [&>*]:min-w-0">
            <header className="grid gap-1">
                <h1 className="text-xl font-semibold tracking-[-0.015em] sm:text-2xl">Conectar {canal.nombre}</h1>
                <p className="font-mono text-sm text-muted-foreground">{formatearNumero(canal.numero)}</p>
            </header>

            {prueba || errorPrueba || probando ? (
                <TarjetaPrueba
                    prueba={prueba}
                    error={errorPrueba}
                    probando={probando}
                    numero={canal.numero}
                    acciones={{
                        alVolverAComprobar: () => void probar(),
                        alCambiarToken: () => form.setFocus("token"),
                        credencialesHref: `${LISTA}/${canal.id}/credenciales`,
                        listaHref: LISTA,
                    }}
                />
            ) : (
                <section className="surface-card flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:p-5" aria-label="Probar conexión">
                    <div className="grid min-w-0 flex-1 gap-1">
                        <p className="text-sm text-muted-foreground">Conexión</p>
                        <EstadoConexion conexion={canal.conexion} />
                    </div>
                    <Boton onClick={() => void probar()}>
                        <RefreshCw aria-hidden />
                        Probar conexión
                    </Boton>
                </section>
            )}

            <SeccionFormulario titulo={<TituloPaso numero={1} titulo="En Meta" listo />} descripcion="Cuenta, app y número listos en Meta.">
                {null}
            </SeccionFormulario>

            <SeccionFormulario
                titulo={<TituloPaso numero={2} titulo="Datos del número" listo={!pideToken} />}
                descripcion={
                    <>
                        {canal.nombre} · <span className="font-mono">{canal.numero}</span> · Phone number ID <span className="font-mono">{canal.phoneNumberId}</span>
                        {pideToken ? null : ". Token y app secret guardados."}
                    </>
                }
            >
                {pideToken ? (
                    <form id={ID_FORM_TOKEN} noValidate onSubmit={form.handleSubmit(guardarToken)} aria-label="Cambiar token" className="grid gap-4">
                        {errorGuardar ? (
                            <Aviso tono="error" titulo="No se pudo guardar el token">
                                {mensajeDeError(errorGuardar).titulo}
                            </Aviso>
                        ) : null}
                        <CampoSecreto
                            etiqueta="Token de acceso"
                            placeholder="Pega el token nuevo"
                            error={form.formState.errors.token?.message ?? (form.formState.isDirty ? undefined : "Meta lo rechazó: venció o no tiene permiso sobre este número.")}
                            {...form.register("token")}
                        />
                        {cambiarSecreto ? (
                            <CampoSecreto etiqueta="App secret" placeholder="Pega el app secret nuevo" ayuda="Vacío: se queda el guardado." error={form.formState.errors.appSecret?.message} {...form.register("appSecret")} />
                        ) : (
                            <SecretoGuardado etiqueta="App secret" guardadoEn={canal.appSecretGuardadoEn} accion="Cambiar" alAccion={() => setCambiarSecreto(true)} />
                        )}
                    </form>
                ) : (
                    <Link href={`${LISTA}/${canal.id}/credenciales`} className="justify-self-start text-sm font-medium text-primary underline-offset-2 hover:underline">
                        Editar credenciales
                    </Link>
                )}
            </SeccionFormulario>

            <SeccionFormulario titulo={<TituloPaso numero={3} titulo="Conectar el webhook" listo={Boolean(canal.webhookVerificadoEn)} />} descripcion="Así Meta le entrega a Iris los mensajes de este número.">
                <WebhookMeta url={url} verifyToken={verifyToken} verifyTokenGuardadoEn={canal.verifyTokenGuardadoEn} alGenerarOtro={generar.pedir}
                    generando={generar.generando}
                    error={generar.error}
                    alPegado={() => {
                        setVerifyToken(null);
                        avisar({ titulo: "Token de verificación oculto: ya no se muestra en esta pantalla" });
                    }}
                />
            </SeccionFormulario>

            {pideToken ? (
                <BarraGuardar
                    form={ID_FORM_TOKEN}
                    textoGuardar="Guardar y probar"
                    textoEnviando="Guardando y probando…"
                    enviando={form.formState.isSubmitting}
                    textoCancelar="Terminar después"
                    alCancelar={() => proteccion.salir(LISTA)}
                    nota={nota}
                />
            ) : (
                <PiePagina nota={nota}>
                    {conectado && puede(["canales.configurar"]) ? (
                        <Boton asChild variante="contorno">
                            <Link href={`${LISTA}/${canal.id}/configurar`}>Configurar uso y horario</Link>
                        </Boton>
                    ) : (
                        <Boton asChild variante="contorno">
                            <Link href={LISTA}>{conectado ? "Ir a la lista" : "Terminar después"}</Link>
                        </Boton>
                    )}
                </PiePagina>
            )}

            {generar.dialogo}
            <DialogoConfirmar
                open={proteccion.pendiente !== null}
                onOpenChange={(abierto) => !abierto && proteccion.seguir()}
                titulo="¿Descartar los cambios?"
                descripcion="Pegaste un token que no se guardó. Si sales ahora, se pierde."
                textoCancelar="Seguir editando"
                textoConfirmar="Descartar cambios"
                peligro
                alConfirmar={proteccion.confirmar}
            />
        </div>
    );
}
