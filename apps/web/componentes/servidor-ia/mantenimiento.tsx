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
import { desactivarMotor, guardarEmpresasPermitidas, guardarModelosPermitidos, leerMotor, rotarCredenciales } from "@/lib/servidor-ia/api";
import { COMANDO, fechaDia, TEXTO_ROTACION_EN_CURSO, TEXTO_ROTACION_SIN_CONFIRMAR, textoPlazas } from "@/lib/servidor-ia/presentacion";
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
                            <ModelosPermitidos key={`m${motor.version}`} motor={motor} alGuardar={reemplazar} />
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
    const plazas = motor.capacidad.maxSimultaneas > 0 ? motor.capacidad.maxSimultaneas : null;

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
                {motor.capacidad.plazasInsuficientes ? (
                    <Aviso tono="warning" titulo="No alcanzan los turnos para todas las empresas" data-slot="plazas-insuficientes">
                        {textoPlazas(motor.capacidad.empresasActivas, plazas)}
                    </Aviso>
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

/**
 * Modelos en dos niveles (§4.12): el servidor declara los que su administrador deja usar a Iris
 * (`catalogo.llm`); de esos, aquí se eligen los que las empresas pueden escoger (`null` = todos).
 */
function ModelosPermitidos({ motor, alGuardar }: { motor: Motor; alGuardar: (m: Motor) => void }) {
    const catalogo = (motor.catalogo.llm ?? []).map((m) => m.id);
    const clave = useClaveIdempotencia();
    const [cuales, setCuales] = useState<"todos" | "algunos">(motor.modelosPermitidos === null ? "todos" : "algunos");
    const [elegidos, setElegidos] = useState<string[]>(motor.modelosPermitidos ?? catalogo);
    const [guardando, setGuardando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const [falta, setFalta] = useState(false);
    const antes = motor.modelosPermitidos;
    const ahora = cuales === "todos" ? null : catalogo.filter((m) => elegidos.includes(m));
    const sucio = JSON.stringify(antes === null ? null : [...antes].sort()) !== JSON.stringify(ahora === null ? null : [...ahora].sort());
    const conflicto = error instanceof ErrorApi && error.estado === 409;

    async function guardar() {
        setError(null);
        if (ahora !== null && ahora.length === 0) {
            setFalta(true);
            return;
        }
        setGuardando(true);
        try {
            const nuevo = await guardarModelosPermitidos(motor.id, { modelos: ahora, version: motor.version }, clave.actual());
            clave.renovar();
            avisar({ titulo: "Guardados los modelos que pueden elegir las empresas" });
            alGuardar(nuevo);
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setGuardando(false);
        }
    }

    return (
        <Seccion icono={Download} titulo="Modelos" slot="modelos-permitidos">
            {catalogo.length === 0 ? (
                <p className="text-sm font-medium text-warning">El servidor no deja usar ningún modelo todavía: sin uno, ninguna empresa puede usarlo. Descarga y permite uno con los comandos de abajo.</p>
            ) : (
                <form
                    noValidate
                    className="grid gap-3"
                    onSubmit={(ev) => {
                        ev.preventDefault();
                        void guardar();
                    }}
                >
                    <GrupoOpciones
                        leyenda="Modelos que las empresas pueden elegir"
                        nombre="cuales-modelos"
                        valor={cuales}
                        alCambiar={(v) => {
                            setCuales(v);
                            setFalta(false);
                        }}
                        opciones={[
                            { valor: "todos", etiqueta: "Todos los del servidor", detalle: "También los que se descarguen después." },
                            { valor: "algunos", etiqueta: "Solo algunos", detalle: "Los demás no aparecen en los ajustes del asistente." },
                        ]}
                    />
                    {cuales === "algunos" ? (
                        <div className="grid gap-1.5" role="group" aria-label="Modelos permitidos">
                            {catalogo.map((m) => (
                                <Casilla
                                    key={m}
                                    etiqueta={m}
                                    checked={elegidos.includes(m)}
                                    onChange={(ev) => {
                                        setElegidos(ev.target.checked ? [...elegidos, m] : elegidos.filter((x) => x !== m));
                                        setFalta(false);
                                    }}
                                />
                            ))}
                            {falta ? <p className="text-sm text-error">Elige al menos un modelo, o deja «Todos los del servidor».</p> : null}
                            <p className="text-[0.8125rem] text-muted-foreground">Si quitas el modelo que usa una empresa, su asistente deja de responder hasta que elija otro.</p>
                        </div>
                    ) : (
                        <ul className="flex flex-wrap gap-1.5" aria-label="Modelos del servidor">
                            {catalogo.map((m) => (
                                <li key={m} className="rounded-md bg-elevated px-2 py-0.5 font-mono text-[0.8125rem]">
                                    {m}
                                </li>
                            ))}
                        </ul>
                    )}
                    {error ? (
                        <Aviso tono="error" titulo={conflicto ? "Alguien más lo cambió" : "No se pudo guardar"} data-slot="error-modelos-permitidos">
                            {conflicto ? "Recarga la página para ver el cambio." : error instanceof ErrorApi && error.estado === 422 ? "Alguno de esos modelos ya no está en el servidor. Recarga la página y vuelve a elegir." : mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <div className="flex justify-end">
                        <Boton type="submit" disabled={guardando || !sucio}>
                            {guardando ? "Guardando…" : "Guardar"}
                        </Boton>
                    </div>
                </form>
            )}
            <p className="text-sm text-muted-foreground">Iris solo puede elegir entre los modelos que el servidor ya tiene y deja usar. Para sumar uno, se descarga y se permite en el propio servidor:</p>
            <Comando texto={COMANDO.modeloDescargar} />
            <Comando texto={COMANDO.modeloPermitir} />
            <p className="text-sm text-muted-foreground">Y para borrar del servidor uno que ya no se usa:</p>
            <Comando texto={COMANDO.modeloQuitar} />
        </Seccion>
    );
}

function Credenciales({ motor, alCambiar }: { motor: Motor; alCambiar: (m: Motor) => void }) {
    const [dialogo, setDialogo] = useState<"rotar" | "revocar" | null>(null);
    const [error, setError] = useState<{ error: unknown; releido: boolean } | null>(null);
    // Una clave por acción: si no llegó respuesta, el reintento repite la misma y no rota dos veces.
    const claveRotar = useClaveIdempotencia();
    const claveRevocar = useClaveIdempotencia();
    async function hacer(revocar: boolean) {
        const clave = revocar ? claveRevocar : claveRotar;
        setError(null);
        try {
            alCambiar(await rotarCredenciales(motor.id, revocar, clave.actual()));
            clave.renovar();
            avisar({ titulo: revocar ? "Credenciales revocadas y cambiadas" : "Credenciales rotadas", ...(revocar ? {} : { descripcion: "La anterior sigue valiendo 24 horas." }) });
        } catch (e) {
            clave.trasError(e);
            // Otro cambio de llave está en marcha: no es un fallo, solo hay que esperar.
            if (e instanceof ErrorApi && e.codigo === "rotacion_en_curso") {
                setError({ error: e, releido: false });
                setDialogo(null);
                return;
            }
            // Sin confirmación: se vuelve a leer la tarjeta para enseñar lo que hay ahora.
            let releido = false;
            try {
                const actual = await leerMotor();
                if (actual) {
                    alCambiar(actual);
                    releido = true;
                }
            } catch {
                // Tampoco se pudo leer: queda dicho abajo.
            }
            setError({ error: e, releido });
        }
        setDialogo(null);
    }
    const enCurso = error !== null && error.error instanceof ErrorApi && error.error.codigo === "rotacion_en_curso";
    const sinConfirmar = error !== null && !enCurso && (!(error.error instanceof ErrorApi) || error.error.estado === 0 || error.error.estado >= 500);
    return (
        <Seccion icono={KeyRound} titulo="Credenciales" slot="credenciales">
            <p className="text-sm text-muted-foreground">Es la llave secreta con la que Iris y el servidor se reconocen. Iris la cambia sola cada 30 días; conviene cambiarla a mano si alguien que tenía acceso al servidor ya no trabaja contigo.</p>
            <Aviso tono="info" titulo="Mientras tanto">
                Las respuestas no se interrumpen: durante 24 horas valen la llave nueva y la anterior.
            </Aviso>
            {enCurso ? (
                <Aviso tono="warning" titulo="Espera unos segundos" data-slot="rotacion-en-curso">
                    {TEXTO_ROTACION_EN_CURSO}
                </Aviso>
            ) : error ? (
                <Aviso tono="error" titulo={sinConfirmar ? "No se pudo confirmar el cambio" : "No se pudieron cambiar las credenciales"} data-slot="error-credenciales" data-sin-confirmar={sinConfirmar}>
                    {sinConfirmar
                        ? `${TEXTO_ROTACION_SIN_CONFIRMAR} ${error.releido ? "Abajo está la última rotación que Iris tiene registrada ahora." : "Tampoco se pudo volver a leer el servidor; recarga la página en un momento."}`
                        : mensajeDeError(error.error).titulo}
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
                descripcion="La llave y el certificado anteriores dejan de valer en el acto y se cambian por unos nuevos. Úsalo si crees que alguien los copió. Las respuestas que estén en camino en ese instante pueden fallar. Si lo que estuvo en riesgo fue el propio Iris, esto no basta: ejecuta «sudo iris-voz unpair» en el servidor y vuelve a enlazar."
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
                        await desactivarMotor(motor.id, "desenlazado_desde_el_panel", nuevaClaveIdempotencia());
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
