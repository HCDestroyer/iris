"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, CampoNumero, Casilla, DialogoConfirmar, EstadoVacio, GrupoOpciones, Tarjeta } from "@iris/ui";
import { Building2, Download, KeyRound, Link2, ShieldOff, Unlink, type LucideIcon } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { desactivarMotor, guardarEmpresasPermitidas, leerMotor, rotarCredenciales } from "@/lib/servidor-ia/api";
import { fechaDia } from "@/lib/servidor-ia/presentacion";
import type { Motor } from "@/lib/servidor-ia/tipos";
import { esquemaEmpresas, type ValoresEmpresas } from "@/lib/servidor-ia/umbrales";
import { useSesion } from "@/lib/sesion";
import { Comando, NavServidorIa, RUTA } from "./comun";

function Seccion({ icono: Icono, titulo, slot, children }: { icono: LucideIcon; titulo: string; slot: string; children: ReactNode }) {
    return (
        <Tarjeta className="grid min-w-0 gap-3 p-4 sm:p-5" data-slot={slot}>
            <h2 className="flex items-center gap-2 text-base font-semibold">
                <Icono aria-hidden className="size-4 text-muted-foreground" />
                {titulo}
            </h2>
            {children}
        </Tarjeta>
    );
}

/** `/administracion/servidor-ia/mantenimiento`: tareas poco frecuentes, solo para quien administra la instalación. */
export function MantenimientoServidorIa() {
    const lectura = useLectura((signal) => leerMotor(signal));
    const { reemplazar } = lectura;
    return (
        <>
            <EncabezadoPagina titulo="Mantenimiento del servidor de IA" descripcion="Tareas poco frecuentes. Solo para quien administra toda la instalación." />
            <NavServidorIa />
            <CargaRegistro lectura={lectura} que="el servidor de IA" noEncontrado="Sin servidor de IA">
                {(motor) =>
                    motor === null || motor.estado === "sin_enlazar" ? (
                        <Tarjeta>
                            <EstadoVacio
                                icono={Link2}
                                titulo={motor ? "Falta activar el servidor" : "Sin servidor de IA"}
                                descripcion="No hay nada que mantener todavía."
                                accion={
                                    <Boton asChild>
                                        <Link href={`${RUTA}/enlazar` as Route}>{motor ? "Terminar de enlazar" : "Enlazar servidor"}</Link>
                                    </Boton>
                                }
                            />
                        </Tarjeta>
                    ) : (
                        <div className="grid max-w-2xl gap-4">
                            <QuienPuede key={motor.version} motor={motor} alGuardar={reemplazar} />
                            <ModelosPermitidos motor={motor} />
                            <Credenciales motor={motor} alCambiar={reemplazar} />
                            <Desenlazar motor={motor} />
                        </div>
                    )
                }
            </CargaRegistro>
        </>
    );
}

function QuienPuede({ motor, alGuardar }: { motor: Motor; alGuardar: (m: Motor) => void }) {
    const { empresas } = useSesion();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresEmpresas>({
        resolver: zodResolver(esquemaEmpresas),
        defaultValues: { quienes: motor.empresasPermitidas === null ? "todas" : "algunas", empresas: motor.empresasPermitidas ?? [], simultaneasPorEmpresa: String(motor.simultaneasPorEmpresa) },
    });
    const [quienes, elegidas] = useWatch({ control: form.control, name: ["quienes", "empresas"] });
    const errores = form.formState.errors;
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const plazas = motor.capacidad?.maxSimultaneas ?? null;

    async function guardar(d: ValoresEmpresas) {
        setError(null);
        try {
            const nuevo = await guardarEmpresasPermitidas(motor.id, { empresas: d.quienes === "todas" ? null : d.empresas, simultaneasPorEmpresa: Number(d.simultaneasPorEmpresa), version: motor.version }, clave.actual());
            clave.renovar();
            avisar({ titulo: "Guardado quién puede usar el servidor de IA" });
            alGuardar(nuevo);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        }
    }
    return (
        <Seccion icono={Building2} titulo="Quién puede usarlo" slot="quien-puede">
            <form noValidate className="grid gap-3" onSubmit={form.handleSubmit(guardar)}>
                <GrupoOpciones
                    leyenda="Empresas que pueden elegirlo"
                    nombre="quienes"
                    valor={quienes}
                    alCambiar={(v) => form.setValue("quienes", v, { shouldDirty: true, shouldValidate: true })}
                    opciones={[
                        { valor: "todas", etiqueta: "Todas las empresas", detalle: "Cada empresa decide si lo usa en su asistente." },
                        { valor: "algunas", etiqueta: "Solo ciertas empresas", detalle: "Las demás no lo verán como opción." },
                    ]}
                />
                {quienes === "algunas" ? (
                    <div className="grid gap-1.5" role="group" aria-label="Empresas permitidas">
                        {empresas.map((e) => (
                            <Casilla
                                key={e.id}
                                etiqueta={e.nombre}
                                checked={elegidas.includes(e.id)}
                                onChange={(ev) => form.setValue("empresas", ev.target.checked ? [...elegidas, e.id] : elegidas.filter((x) => x !== e.id), { shouldDirty: true, shouldValidate: true })}
                            />
                        ))}
                        {errores.empresas?.message ? <p className="text-sm text-error">{errores.empresas.message}</p> : null}
                        <p className="text-[0.8125rem] text-muted-foreground">A una empresa que quites no se le borra nada: su asistente queda «sin configurar» hasta que elija otro proveedor.</p>
                    </div>
                ) : null}
                <CampoNumero
                    etiqueta="Peticiones a la vez por empresa, como mucho"
                    minimo={1}
                    maximo={16}
                    ayuda={`Para que una empresa no acapare el servidor. Cada empresa que lo usa tiene siempre un lugar asegurado.${plazas === null ? "" : ` El servidor atiende ${plazas} a la vez en total.`} De fábrica: 2.`}
                    error={errores.simultaneasPorEmpresa?.message}
                    {...form.register("simultaneasPorEmpresa")}
                />
                {error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más lo cambió" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
                <div className="flex justify-end">
                    <Boton type="submit" disabled={form.formState.isSubmitting || !form.formState.isDirty}>
                        {form.formState.isSubmitting ? "Guardando…" : "Guardar"}
                    </Boton>
                </div>
            </form>
        </Seccion>
    );
}

function ModelosPermitidos({ motor }: { motor: Motor }) {
    const modelos = motor.catalogo?.llm ?? [];
    return (
        <Seccion icono={Download} titulo="Modelos" slot="modelos-permitidos">
            <p className="text-sm text-muted-foreground">Los modelos se descargan y se permiten en el propio servidor, con un comando: desde Iris solo se ven. Estos son los que las empresas pueden elegir ahora:</p>
            {modelos.length > 0 ? (
                <ul className="flex flex-wrap gap-1.5" aria-label="Modelos permitidos">
                    {modelos.map((m) => (
                        <li key={m.id} className="rounded-md bg-elevated px-2 py-0.5 font-mono text-[0.8125rem]">
                            {m.id}
                        </li>
                    ))}
                </ul>
            ) : (
                <p className="text-sm font-medium text-warning">Ninguno todavía: sin un modelo permitido, ninguna empresa puede usar el servidor.</p>
            )}
            <p className="text-sm text-muted-foreground">Para descargar uno y permitirlo:</p>
            <Comando texto="sudo iris-voz modelo descargar <nombre>" />
            <Comando texto="sudo iris-voz modelo permitir <nombre>" />
            <p className="text-sm text-muted-foreground">Y para quitar uno que ya no se usa:</p>
            <Comando texto="sudo iris-voz modelo quitar <nombre>" />
        </Seccion>
    );
}

function Credenciales({ motor, alCambiar }: { motor: Motor; alCambiar: (m: Motor) => void }) {
    const [dialogo, setDialogo] = useState<"rotar" | "revocar" | null>(null);
    const [error, setError] = useState<unknown>(null);
    async function hacer(revocar: boolean) {
        setError(null);
        try {
            alCambiar(await rotarCredenciales(motor.id, revocar, nuevaClaveIdempotencia()));
            avisar({ titulo: revocar ? "Credenciales revocadas y cambiadas" : "Credenciales rotadas", ...(revocar ? {} : { descripcion: "La anterior sigue valiendo 24 horas." }) });
        } catch (e) {
            setError(e);
        }
        setDialogo(null);
    }
    return (
        <Seccion icono={KeyRound} titulo="Credenciales" slot="credenciales">
            <p className="text-sm text-muted-foreground">Es la llave secreta con la que Iris y el servidor se reconocen. Iris la cambia sola cada 30 días; conviene cambiarla a mano si alguien que tenía acceso al servidor ya no trabaja contigo.</p>
            <Aviso tono="info" titulo="Mientras tanto">
                Las respuestas no se interrumpen: durante 24 horas valen la llave nueva y la anterior. Si el servidor está apagado no se cambia nada y sigue valiendo la actual.
            </Aviso>
            {error ? (
                <Aviso tono="error" titulo="No se pudieron cambiar las credenciales" data-slot="error-credenciales">
                    {error instanceof ErrorApi && error.codigo === "motor_no_responde" ? "El servidor no respondió: no se cambió nada y sigue valiendo la llave actual." : mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            <div className="flex flex-wrap gap-2">
                <Boton variante="contorno" onClick={() => setDialogo("rotar")}>
                    <KeyRound />
                    Rotar credenciales
                </Boton>
                <Boton variante="fantasma" className="text-error" onClick={() => setDialogo("revocar")}>
                    <ShieldOff />
                    Revocar ahora
                </Boton>
            </div>
            <p className="text-[0.8125rem] text-muted-foreground" data-slot="ultima-rotacion">
                {motor.tokenRotadoEn ? `Última rotación: ${fechaDia(motor.tokenRotadoEn)}.` : "Todavía no se han rotado."}
            </p>
            <DialogoConfirmar
                open={dialogo === "rotar"}
                onOpenChange={(a) => !a && setDialogo(null)}
                titulo="¿Rotar las credenciales?"
                descripcion="Iris genera una llave nueva y se la entrega al servidor. La anterior sigue valiendo 24 horas, así que nada se interrumpe."
                textoConfirmar="Rotar"
                textoCancelar="Cancelar"
                textoOcupado="Rotando…"
                alConfirmar={() => hacer(false)}
            />
            <DialogoConfirmar
                open={dialogo === "revocar"}
                onOpenChange={(a) => !a && setDialogo(null)}
                titulo="¿Revocar las credenciales ahora?"
                descripcion="La llave y el certificado anteriores dejan de valer en el acto y se cambian por unos nuevos. Úsalo si crees que alguien los copió. Las respuestas que estén en camino en ese instante pueden fallar. Si lo que estuvo en riesgo fue el propio Iris, esto no basta: ejecuta «sudo iris-voz desenlazar» en el servidor y vuelve a enlazar."
                textoConfirmar="Revocar ahora"
                textoCancelar="Cancelar"
                textoOcupado="Revocando…"
                peligro
                alConfirmar={() => hacer(true)}
            />
        </Seccion>
    );
}

function Desenlazar({ motor }: { motor: Motor }) {
    const router = useRouter();
    const [abierto, setAbierto] = useState(false);
    const [error, setError] = useState<unknown>(null);
    return (
        <Seccion icono={Unlink} titulo="Desenlazar el servidor" slot="desenlazar">
            <p className="text-sm text-muted-foreground">Iris deja de conectarse al servidor. El servidor y sus modelos no se borran; para volver a usarlo hay que enlazarlo de nuevo con un código.</p>
            {error ? (
                <Aviso tono="error" titulo="No se pudo desenlazar">
                    {error instanceof ErrorApi && error.estado === 409 ? "Alguien más lo cambió. Recarga la página." : mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            <div>
                <Boton variante="contorno" className="border-error/50 text-error" onClick={() => setAbierto(true)}>
                    <Unlink />
                    Desenlazar…
                </Boton>
            </div>
            <DialogoConfirmar
                open={abierto}
                onOpenChange={setAbierto}
                titulo={`¿Desenlazar ${motor.nombre}?`}
                descripcion="El asistente de las empresas que lo usan deja de usarlo al instante: lo que atendía solo pasa a personas y las sugerencias muestran error hasta que cada empresa elija otro proveedor o se vuelva a enlazar. No se borra ninguna conversación."
                textoConfirmar="Desenlazar"
                textoCancelar="Dejarlo enlazado"
                textoOcupado="Desenlazando…"
                peligro
                alConfirmar={async () => {
                    setError(null);
                    try {
                        await desactivarMotor(motor.id, motor.version, "desenlazado_desde_el_panel", nuevaClaveIdempotencia());
                        avisar({ titulo: "Servidor de IA desenlazado" });
                        router.replace(RUTA as Route);
                    } catch (e) {
                        setError(e);
                        setAbierto(false);
                    }
                }}
            />
        </Seccion>
    );
}
