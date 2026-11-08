"use client";

import { avisar, Aviso, Boton, cn, Interruptor, SeccionFormulario } from "@iris/ui";
import { Building2, RotateCw } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { CargaRegistro } from "@/componentes/carga-registro";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { EstadoConexion } from "@/componentes/numeros/estado-conexion";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { CLASE_PILDORA, formatearNumero, nombreUso, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import { useLectura } from "@/lib/lectura";
import { configurarPrestamo, leerCanalMeta } from "@/lib/meta/api";
import { etiquetaTipoConexion, prestamoConversa } from "@/lib/meta/presentacion";
import type { CanalMeta } from "@/lib/meta/tipos";
import { fechaCorta } from "@/lib/numeros/conexion";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

/**
 * `/administracion/numeros/[id]/prestamo`: un número que otra empresa le presta a la activa
 * (contrato §15.3). Se ve qué puede usar y en qué cola caen sus conversaciones; lo único que esta
 * empresa cambia es si es **su** número de avisos (solo con el uso «Avisos»). Credenciales, horario
 * y conexión son de la dueña.
 */
export function NumeroPrestado() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const { puede } = useSesion();
    const verColas = puede(["conversaciones.ver"]);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const [canal, colas] = await Promise.all([leerCanalMeta(id, signal), verColas ? resolver(api().GET("/api/v1/mensajeria/colas", { signal })).then((r) => r.data ?? []) : Promise.resolve(null)]);
        return canal.prestadoPor && canal.prestamo ? { canal, colas } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.canal.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el número" noEncontrado="Número no encontrado">
            {(d) => <Detalle inicial={d.canal} cola={d.colas?.find((c) => c.id === d.canal.prestamo?.colaId)?.nombre ?? null} verColas={verColas} />}
        </CargaRegistro>
    );
}

function Detalle({ inicial, cola, verColas }: { inicial: CanalMeta; cola: string | null; verColas: boolean }) {
    const { yo, puede } = useSesion();
    const [canal, setCanal] = useState(inicial);
    const [error, setError] = useState<unknown>(null);
    const [guardando, setGuardando] = useState(false);
    const prestamo = canal.prestamo;
    const duena = canal.prestadoPor?.nombre ?? "otra empresa";
    if (!prestamo) return null;
    const conAvisos = prestamo.usos.includes("avisos");

    async function marcar(avisosEmpresa: boolean) {
        if (!prestamo) return;
        setGuardando(true);
        setError(null);
        try {
            const c = await configurarPrestamo(canal.id, { version: prestamo.version, avisosEmpresa }, nuevaClaveIdempotencia());
            setCanal(c);
            avisar({ titulo: avisosEmpresa ? `Los avisos de ${yo.empresaActiva?.nombre ?? "tu empresa"} salen por «${c.nombre}»` : `«${c.nombre}» ya no es tu número de avisos` });
        } catch (e) {
            setError(e);
        } finally {
            setGuardando(false);
        }
    }

    return (
        <div className="mx-auto grid w-full max-w-3xl min-w-0 gap-4">
            <EncabezadoPagina
                titulo={canal.nombre}
                descripcion={
                    <>
                        <span className="font-mono">{formatearNumero(canal.numero)}</span> · {etiquetaTipoConexion(canal.tipoConexion)} · prestado por {duena}
                    </>
                }
            />
            <Aviso tono="info" icono={Building2} titulo={`${canal.nombre} es de ${duena}`}>
                Las credenciales, el horario y la conexión con Meta los administra {duena}. La calidad y el límite de Meta son del número: si se marcan avisos como spam, baja para todas las empresas que lo usan.
            </Aviso>
            <SeccionFormulario titulo="Lo que puede usar tu empresa" descripcion={`Prestado el ${fechaCorta(prestamo.prestadoEn)}.`}>
                <dl className="grid gap-3 text-sm sm:grid-cols-[10rem_1fr]">
                    <dt className="text-muted-foreground">Usos</dt>
                    <dd className="flex flex-wrap gap-1">
                        {prestamo.usos.map((u) => (
                            <span key={u} className={cn("inline-flex h-5 items-center rounded-full px-2 text-[0.6875rem] font-semibold", CLASE_PILDORA[tonoDeCanal({ usos: [u] })])}>
                                {nombreUso(u)}
                            </span>
                        ))}
                    </dd>
                    <dt className="text-muted-foreground">Cola</dt>
                    <dd>{!verColas ? "—" : (cola ?? "Sin cola")}</dd>
                    <dt className="text-muted-foreground">Conexión</dt>
                    <dd>
                        <EstadoConexion conexion={canal.conexion} />
                    </dd>
                </dl>
                <p className="text-sm text-muted-foreground">
                    {prestamoConversa(prestamo.usos)
                        ? "Tus agentes contestan aquí solo las conversaciones de tu empresa; las de la dueña no se ven."
                        : "Solo para avisos: tus agentes no inician conversaciones ni mandan plantillas por este número."}
                </p>
            </SeccionFormulario>
            {puede(["canales.configurar"]) ? (
                <SeccionFormulario titulo="Avisos de tu empresa">
                    <Interruptor
                        etiqueta="Número de avisos de la empresa"
                        ayuda={
                            conAvisos
                                ? `Los avisos de tu Argos salen por aquí con las plantillas de ${duena}. Marcarlo lo quita de tu número de avisos de ahora.`
                                : `${duena} no prestó el uso «Avisos»: los avisos de tu empresa no pueden salir por aquí.`
                        }
                        checked={prestamo.avisosEmpresa}
                        disabled={!conAvisos || guardando}
                        onChange={(e) => void marcar(e.target.checked)}
                    />
                    {error ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudo cambiar"
                            accion={
                                error instanceof ErrorApi && error.estado === 409 ? undefined : (
                                    <Boton variante="contorno" tamano="sm" onClick={() => void marcar(!prestamo.avisosEmpresa)}>
                                        <RotateCw aria-hidden />
                                        Reintentar
                                    </Boton>
                                )
                            }
                        >
                            {error instanceof ErrorApi && error.estado === 409 ? "Alguien más cambió el préstamo: recarga la página." : mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                </SeccionFormulario>
            ) : null}
            <div>
                <Boton asChild variante="contorno">
                    <Link href="/administracion/numeros">Volver a la lista</Link>
                </Boton>
            </div>
        </div>
    );
}
