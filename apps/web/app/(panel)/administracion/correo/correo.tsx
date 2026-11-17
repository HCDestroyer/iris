"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoSecreto,
    CampoSelector,
    Casilla,
    cn,
    DialogoConfirmar,
    FormularioPagina,
    GrupoSegmentado,
    InsigniaEstado,
    liberarProteccion,
    SeccionFormulario,
} from "@iris/ui";
import { BellRing, CircleAlert, CircleCheck, MailCheck, Power, RotateCw, Send, Server } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { activarCorreo, guardarCorreo, leerCorreo } from "@/lib/correo/api";
import {
    ambitoDe,
    atajoDe,
    ATAJOS,
    CAMPOS_CORREO,
    entradaCorreo,
    esquemaCorreo,
    NOMBRE_SEGURIDAD,
    PUERTOS,
    SEGURIDADES,
    textoCausa,
    textoOrigen,
    valoresCorreo,
    type Ambito,
    type ValoresCorreo,
} from "@/lib/correo/correo";
import { PERMISO_CONFIGURAR_CORREO, type AjusteCorreo } from "@/lib/correo/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { DialogoPrueba } from "./prueba";

/**
 * `/administracion/correo` (`correo_sistema.ver` / `.configurar`): el servidor de correo con que
 * Iris manda avisos a sus usuarios (contratos/correo-sistema.md). Antes se configuraba en el `.env`
 * del servidor; ahora aquí, con «Enviar correo de prueba» antes de guardar. Quien tiene el permiso
 * sin límite de empresa elige entre el correo general y el de la empresa activa; los demás, solo el
 * de su empresa.
 */
export function CorreoDelSistema() {
    const { yo } = useSesion();
    const elegido = useSearchParams().get("ambito");
    const ambito = ambitoDe(yo, elegido);
    if (ambito === null) {
        return (
            <Aviso tono="info" titulo="Elige una empresa" data-testid="correo-sin-empresa">
                El correo se configura por empresa: elige una arriba para ver el suyo.
            </Aviso>
        );
    }
    return <Carga key={String(ambito)} ambito={ambito} />;
}

function Carga({ ambito }: { ambito: Ambito }) {
    const lectura = useLectura((signal) => leerCorreo(ambito, signal));
    return (
        <CargaRegistro lectura={lectura} que="el correo" noEncontrado="Sin ajustes de correo">
            {(ajuste) => <Formulario key={ajuste.version} ambito={ambito} ajuste={ajuste} alCambiar={lectura.reemplazar} recargar={lectura.reintentar} />}
        </CargaRegistro>
    );
}

const TONO = { ok: "success", info: "info", aviso: "warning" } as const;
const ICONO = { ok: MailCheck, info: Server, aviso: BellRing } as const;

function Formulario({ ambito, ajuste, alCambiar, recargar }: { ambito: Ambito; ajuste: AjusteCorreo; alCambiar: (a: AjusteCorreo) => void; recargar: () => void }) {
    const router = useRouter();
    const ruta = usePathname();
    const { yo, puede } = useSesion();
    const configura = puede([PERMISO_CONFIGURAR_CORREO]);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [probando, setProbando] = useState(false);
    const form = useForm<ValoresCorreo>({ resolver: zodResolver(esquemaCorreo), defaultValues: valoresCorreo(ajuste) });
    const [host, puerto, seguridad, contrasena] = useWatch({ control: form.control, name: ["host", "puerto", "seguridad", "contrasena"] });
    const errores = form.formState.errors;
    const sucio = form.formState.isDirty;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const origen = textoOrigen(ajuste, ambito);
    const atajo = atajoDe({ host, puerto, seguridad });
    const empresa = yo.empresaActiva;
    const eligeAmbito = yo.todasLasEmpresas && empresa !== undefined;

    async function guardar(d: ValoresCorreo) {
        setError(null);
        try {
            const guardado = await guardarCorreo(ambito, entradaCorreo(ajuste, d), clave.actual());
            clave.renovar();
            avisar({ titulo: "Correo guardado", descripcion: guardado.activo ? "Los próximos avisos ya salen con estos datos." : "Está apagado: enciéndelo para usarlo." });
            await liberarProteccion();
            alCambiar(guardado);
        } catch (e) {
            clave.trasError(e);
            // El 422 trae el nombre del campo del formulario (§6): va junto a él.
            if (!erroresACampos(e, CAMPOS_CORREO, form.setError)) setError(e);
        }
    }

    async function abrirPrueba() {
        // La prueba usa lo que hay en el formulario, sin guardar: primero tiene que ser válido.
        if (await form.trigger(undefined, { shouldFocus: true })) setProbando(true);
    }

    return (
        <FormularioPagina
            titulo="Correo"
            descripcion="El correo con que Iris manda avisos a tu equipo (por ejemplo, cuando el asistente llega a su tope). No es el de los buzones de soporte."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion"
            sucio={sucio}
            barra={{
                enviando: form.formState.isSubmitting,
                nota: "Guardar no prueba la conexión: para eso está el correo de prueba.",
                ...(configura ? {} : { faltan: ["el permiso"], motivo: "Puedes verlo, pero no tienes permiso para cambiarlo." }),
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió el correo: recarga para ver el cambio." } : {}),
            }}
            error={
                <>
                    {eligeAmbito ? (
                        <div className="grid gap-1.5" data-slot="ambito-correo">
                            <GrupoSegmentado
                                etiqueta="Qué correo configuras"
                                valor={ambito === "global" ? "global" : "empresa"}
                                deshabilitado={sucio}
                                alCambiar={(v) => router.replace(v === "empresa" ? `${ruta}?ambito=empresa` : ruta)}
                                opciones={[
                                    { valor: "global", etiqueta: "General" },
                                    { valor: "empresa", etiqueta: `Solo ${empresa.nombre}` },
                                ]}
                            />
                            <p className="text-sm text-muted-foreground">
                                {sucio
                                    ? "Guarda o cancela los cambios para pasar al otro."
                                    : ambito === "global"
                                      ? "El general lo usan todas las empresas que no tengan uno propio encendido."
                                      : "Si esta empresa tiene uno propio encendido, sus avisos salen por él y no por el general."}
                            </p>
                        </div>
                    ) : null}
                    <Aviso
                        tono={TONO[origen.tono]}
                        icono={ICONO[origen.tono]}
                        titulo={origen.titulo}
                        data-testid="origen-correo"
                        data-origen={ajuste.origen}
                        // En su propia fila (no junto al título): en el teléfono el título no se aprieta.
                        accion={ajuste.configurado ? <Encendido ambito={ambito} ajuste={ajuste} configura={configura} sucio={sucio} alCambiar={alCambiar} recargar={recargar} /> : undefined}
                    >
                        {origen.detalle}
                    </Aviso>
                    {error ? (
                        <Aviso
                            tono="error"
                            titulo={conflicto ? "Alguien más cambió el correo" : "No se pudo guardar"}
                            data-testid="error-correo"
                            accion={
                                conflicto ? (
                                    <Boton variante="secundario" tamano="sm" onClick={recargar}>
                                        <RotateCw />
                                        Recargar
                                    </Boton>
                                ) : undefined
                            }
                        >
                            {conflicto ? "Recarga para ver lo que quedó guardado; lo que escribiste aquí se pierde." : `${mensajeDeError(error).titulo}${error instanceof ErrorApi && error.detalle ? `: ${error.detalle}` : ""}. Vuelve a tocar «Guardar».`}
                        </Aviso>
                    ) : null}
                </>
            }
            despues={probando ? <DialogoPrueba ambito={ambito} valores={form.getValues()} cerrar={() => setProbando(false)} /> : null}
        >
            <SeccionFormulario titulo="¿De quién es el correo?" descripcion="Rellena el servidor por ti. Después puedes cambiar lo que haga falta.">
                <div role="group" aria-label="Proveedor de correo" className="flex flex-wrap gap-2">
                    {ATAJOS.map((a) => (
                        <Boton
                            key={a.id}
                            variante={atajo === a.id ? "primario" : "secundario"}
                            tamano="sm"
                            aria-pressed={atajo === a.id}
                            disabled={!configura}
                            data-atajo={a.id}
                            onClick={() => {
                                for (const campo of ["host", "puerto", "seguridad"] as const) form.setValue(campo, a.relleno[campo], { shouldDirty: true, shouldValidate: form.formState.isSubmitted });
                            }}
                        >
                            {a.nombre}
                        </Boton>
                    ))}
                </div>
                {atajo ? (
                    <p className="text-sm text-muted-foreground" data-slot="nota-atajo">
                        {ATAJOS.find((a) => a.id === atajo)?.nota}
                    </p>
                ) : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="Servidor">
                <Campo etiqueta="Servidor" placeholder="smtp.tuproveedor.com" maxLength={253} autoComplete="off" autoCapitalize="none" spellCheck={false} disabled={!configura} error={errores.host?.message} {...form.register("host")} />
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Puerto" disabled={!configura} error={errores.puerto?.message} {...form.register("puerto")}>
                        {[...new Set([...PUERTOS.map(String), puerto])].map((p) => (
                            <option key={p} value={p}>
                                {p}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoSelector etiqueta="Seguridad" disabled={!configura} error={errores.seguridad?.message} {...form.register("seguridad")}>
                        {SEGURIDADES.map((s) => (
                            <option key={s} value={s}>
                                {NOMBRE_SEGURIDAD[s]}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
            </SeccionFormulario>

            <SeccionFormulario titulo="Cuenta" descripcion="Con la que Iris entra al servidor. Déjalas vacías si tu servidor no pide usuario.">
                <Campo etiqueta="Usuario" placeholder="avisos@tuempresa.com" maxLength={254} autoComplete="off" autoCapitalize="none" spellCheck={false} disabled={!configura} error={errores.usuario?.message} {...form.register("usuario")} />
                <CampoSecreto
                    etiqueta="Contraseña"
                    nombre="la contraseña"
                    placeholder={ajuste.tieneContrasena ? "Guardada" : "Escribe la contraseña"}
                    ayuda={ajuste.tieneContrasena ? "Guardada. Escribe otra para cambiarla. Nunca se vuelve a mostrar." : "Se guarda cifrada y nunca se vuelve a mostrar."}
                    disabled={!configura}
                    error={errores.contrasena?.message}
                    {...form.register("contrasena")}
                />
                {ajuste.tieneContrasena && contrasena === "" ? <Casilla etiqueta="Quitar la contraseña guardada" ayuda="Para un servidor que ya no la pide." disabled={!configura} {...form.register("quitarContrasena")} /> : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="Remitente" descripcion="Quién aparece como autor de los correos.">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" placeholder="Iris · Tu empresa" maxLength={80} autoComplete="off" disabled={!configura} error={errores.remitenteNombre?.message} {...form.register("remitenteNombre")} />
                    <Campo
                        etiqueta="Correo"
                        type="email"
                        inputMode="email"
                        placeholder="no-responder@tuempresa.com"
                        maxLength={254}
                        autoComplete="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        disabled={!configura}
                        error={errores.remitenteCorreo?.message}
                        {...form.register("remitenteCorreo")}
                    />
                </div>
            </SeccionFormulario>

            {configura ? (
                <section aria-labelledby="titulo-probar-correo" data-slot="probar-correo" className="grid min-w-0 gap-3 rounded-[var(--hx-radius-card)] border border-primary/30 bg-primary/[0.06] p-4 sm:p-5 [&>*]:min-w-0">
                    <h2 id="titulo-probar-correo" className="flex items-center gap-2 text-base font-semibold tracking-[-0.015em]">
                        <Send aria-hidden className="size-[1.125rem] text-primary" />
                        Pruébalo antes de guardar
                    </h2>
                    <p className="text-sm text-muted-foreground">Manda un correo de prueba con lo que hay arriba, aunque no lo hayas guardado.</p>
                    <UltimaPrueba ajuste={ajuste} />
                    <Boton variante="secundario" className="justify-self-start" onClick={() => void abrirPrueba()}>
                        <Send />
                        Enviar correo de prueba
                    </Boton>
                </section>
            ) : null}
        </FormularioPagina>
    );
}

/** La última prueba que se hizo con lo guardado, si el servidor la recuerda. */
function UltimaPrueba({ ajuste }: { ajuste: AjusteCorreo }) {
    const u = ajuste.ultimaPrueba;
    if (!u) return null;
    const ok = u.estado === "ok";
    const Icono = ok ? CircleCheck : CircleAlert;
    return (
        <p className={cn("flex items-start gap-1.5 text-sm font-medium", ok ? "text-success" : "text-error")} data-slot="ultima-prueba" data-estado={u.estado}>
            <Icono aria-hidden className="mt-0.5 size-4 shrink-0" />
            <span className="min-w-0 [overflow-wrap:anywhere]">{ok ? "La última prueba salió bien." : `La última prueba falló. ${textoCausa(u.causa)}`}</span>
        </p>
    );
}

/** Encendido o apagado, con su botón: apagar o encender pide confirmación y dice qué pasa después (§2). */
function Encendido({ ambito, ajuste, configura, sucio, alCambiar, recargar }: { ambito: Ambito; ajuste: AjusteCorreo; configura: boolean; sucio: boolean; alCambiar: (a: AjusteCorreo) => void; recargar: () => void }) {
    const clave = useClaveIdempotencia();
    const [confirmar, setConfirmar] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const encender = !ajuste.activo;
    const conflicto = error instanceof ErrorApi && error.estado === 409;

    async function cambiar() {
        setError(null);
        try {
            const nuevo = await activarCorreo(ambito, encender, ajuste.version, clave.actual());
            clave.renovar();
            avisar({ titulo: encender ? "Correo encendido" : "Correo apagado" });
            setConfirmar(false);
            alCambiar(nuevo);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }

    return (
        <span className="flex flex-wrap items-center gap-2" data-slot="encendido-correo">
            <InsigniaEstado tono={ajuste.activo ? "success" : "neutral"} texto={ajuste.activo ? "Encendido" : "Apagado"} data-activo={ajuste.activo} />
            {configura ? (
                <Boton variante="contorno" tamano="sm" disabled={sucio} title={sucio ? "Guarda o cancela los cambios primero" : undefined} onClick={() => setConfirmar(true)}>
                    <Power />
                    {encender ? "Encender" : "Apagar"}
                </Boton>
            ) : null}
            <DialogoConfirmar
                open={confirmar}
                onOpenChange={(v) => {
                    setConfirmar(v);
                    if (!v) setError(null);
                }}
                titulo={encender ? "¿Encender este correo?" : "¿Apagar este correo?"}
                descripcion={
                    encender
                        ? "Los próximos avisos saldrán por este correo."
                        : ambito === "global"
                          ? "Los avisos saldrán por el correo del servidor si hay uno; si no, solo llegarán por la campana."
                          : "Los avisos de esta empresa saldrán por el correo general si hay uno; si no, solo llegarán por la campana."
                }
                textoConfirmar={encender ? "Encender" : "Apagar"}
                peligro={!encender}
                alConfirmar={cambiar}
            >
                {error ? (
                    <Aviso
                        tono="error"
                        titulo={conflicto ? "Alguien más cambió el correo" : encender ? "No se pudo encender" : "No se pudo apagar"}
                        accion={
                            conflicto ? (
                                <Boton variante="secundario" tamano="sm" onClick={recargar}>
                                    <RotateCw />
                                    Recargar
                                </Boton>
                            ) : undefined
                        }
                    >
                        {conflicto ? "Recarga para ver lo que quedó guardado." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
            </DialogoConfirmar>
        </span>
    );
}
