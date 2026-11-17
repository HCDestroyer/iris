"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, Casilla, cn, Esqueleto, Tarjeta } from "@iris/ui";
import { Check, KeyRound, Link2, RotateCw, ShieldAlert } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { BotonCopiar } from "@/componentes/cuenta/boton-copiar";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { ZONA } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useLectura } from "@/lib/lectura";
import { activarMotor, conectarMotor, enlazarMotor, leerMotor } from "@/lib/servidor-ia/api";
import { avisoIncompatibleAlEnlazar, COMANDO, huellaComoElServidor, huellaLegible, textoErrorEnlace } from "@/lib/servidor-ia/presentacion";
import type { EnlaceCreado, Motor } from "@/lib/servidor-ia/tipos";
import { esquemaEnlace, type ValoresEnlace } from "@/lib/servidor-ia/umbrales";
import { Comando, RUTA } from "./comun";

const hora = new Intl.DateTimeFormat("es-GT", { hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: ZONA });
const diaHora = new Intl.DateTimeFormat("es-GT", { weekday: "long", hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZone: ZONA });

const textoDe = (e: unknown, alConectar: boolean): string => (e instanceof ErrorApi ? (textoErrorEnlace(e.codigo, e.estado, alConectar) ?? (e.detalle || e.titulo)) : mensajeDeError(e).titulo);

/** Un paso numerado: hecho (✓), el actual (número en la marca) o por venir (apagado). */
function Paso({ numero, titulo, estado, children }: { numero: number; titulo: string; estado: "hecho" | "actual" | "despues"; children?: ReactNode }) {
    return (
        <li className="grid grid-cols-[2.25rem_minmax(0,1fr)] gap-3" data-paso={numero} data-estado={estado}>
            <span
                aria-hidden
                className={cn(
                    "flex size-9 items-center justify-center rounded-full border text-sm font-semibold",
                    estado === "actual" ? "border-primary bg-primary text-primary-foreground" : estado === "hecho" ? "border-success/50 bg-success/10 text-success" : "border-border text-muted-foreground",
                )}
            >
                {estado === "hecho" ? <Check className="size-4" /> : numero}
            </span>
            <Tarjeta className={cn("grid min-w-0 gap-3 p-4 sm:p-5", estado === "despues" && "opacity-60")}>
                <h2 className="text-base font-semibold">
                    <span className="sr-only">
                        Paso {numero}
                        {estado === "hecho" ? " (hecho)" : ""}:{" "}
                    </span>
                    {titulo}
                </h2>
                {children}
            </Tarjeta>
        </li>
    );
}

/**
 * `/administracion/servidor-ia/enlazar`: enlazar el servidor (contratos/voz.md §4.8). Dirección →
 * código de un solo uso que se escribe **en el servidor** (allá no se ve al escribirlo) → Iris se
 * conecta → se compara la huella → «Activar». Hasta activar, Iris no le manda nada más.
 */
export function EnlazarServidorIa() {
    const lectura = useLectura((signal) => leerMotor(signal));
    return (
        <>
            <EncabezadoPagina titulo="Enlazar el servidor de IA" descripcion="Se hace una vez. Necesitas a alguien frente al servidor (o conectado a él) para escribir un código y leer una huella." />
            {lectura.estado === "cargando" ? (
                <div className="grid gap-3" aria-busy="true">
                    <Esqueleto className="h-32" />
                    <Esqueleto className="h-40" />
                </div>
            ) : lectura.estado === "error" ? (
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el servidor de IA" />
            ) : lectura.datos && lectura.datos.estado !== "sin_enlazar" && lectura.datos.estado !== "desactivado" ? (
                <Aviso
                    tono="info"
                    titulo="Ya hay un servidor de IA enlazado"
                    accion={
                        <Boton asChild variante="secundario" tamano="sm">
                            <Link href={RUTA as Route}>Ver el tablero</Link>
                        </Boton>
                    }
                >
                    {lectura.datos.nombre} ({lectura.datos.url}). Para enlazar otro, primero desactiva este en Mantenimiento.
                </Aviso>
            ) : (
                // Uno desactivado se enlaza de nuevo desde el principio (su huella de antes no vale): solo se
                // recuerdan su dirección y su nombre.
                <Pasos pendiente={lectura.datos?.estado === "sin_enlazar" ? lectura.datos : null} previo={lectura.datos} />
            )}
        </>
    );
}

function Pasos({ pendiente, previo }: { pendiente: Motor | null; previo: Motor | null }) {
    const router = useRouter();
    // Si quedó un enlace a medias: con huella, falta compararla y activar; sin huella, el código ya
    // no se puede volver a mostrar (se enseña una sola vez) y hay que generar otro.
    const [motor, setMotor] = useState<Motor | null>(pendiente?.huellaTls ? pendiente : null);
    const [enlace, setEnlace] = useState<EnlaceCreado | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [ocupado, setOcupado] = useState<"conectar" | "activar" | null>(null);
    // Se guarda **qué huella** se marcó, no un sí/no: si la huella en pantalla cambia, la marca deja de valer sola.
    const [huellaMarcada, setHuellaMarcada] = useState<string | null>(null);
    const coincide = Boolean(motor?.huellaTls) && huellaMarcada === motor?.huellaTls;
    const [ahora, setAhora] = useState(() => Date.now());
    const form = useForm<ValoresEnlace>({ resolver: zodResolver(esquemaEnlace), defaultValues: { nombre: previo?.nombre ?? "Servidor de IA", url: previo?.url ?? "" } });
    const reenlace = previo?.estado === "desactivado";

    useEffect(() => {
        const id = window.setInterval(() => setAhora(Date.now()), 1000);
        return () => window.clearInterval(id);
    }, []);

    const paso = motor ? "huella" : enlace ? "codigo" : "direccion";
    const vence = enlace ? Date.parse(enlace.venceEn) : NaN;
    // La ventana vencida también la dice el servidor (409 `enlace_vencido`, el mismo código para los dos pasos).
    const vencidoAlla = error instanceof ErrorApi && error.codigo === "enlace_vencido";
    const codigoVencido = enlace !== null && ((!Number.isNaN(vence) && ahora > vence) || vencidoAlla);
    const caduca = motor?.enlaceVenceEn ? Date.parse(motor.enlaceVenceEn) : NaN;
    const enlaceCaducado = motor !== null && ((!Number.isNaN(caduca) && ahora > caduca) || vencidoAlla);

    async function generar(d: ValoresEnlace) {
        setError(null);
        try {
            // El código no se guarda por `Idempotency-Key`: cada intento es una clave nueva.
            setEnlace(await enlazarMotor({ nombre: d.nombre.trim(), url: d.url.trim() }, nuevaClaveIdempotencia()));
        } catch (e) {
            if (!erroresACampos(e, ["nombre", "url"], form.setError)) setError(e);
        }
    }
    async function conectar() {
        if (!enlace) return;
        setError(null);
        setOcupado("conectar");
        try {
            setMotor(await conectarMotor(enlace.motor.id, nuevaClaveIdempotencia()));
            setEnlace(null);
        } catch (e) {
            setError(e);
        } finally {
            setOcupado(null);
        }
    }
    async function activar() {
        if (!motor?.huellaTls || !coincide) return;
        setError(null);
        setOcupado("activar");
        try {
            // Va la huella que está en pantalla y que se marcó como «Coincide»: el servidor activa solo si sigue siendo esa.
            await activarMotor(motor.id, motor.huellaTls, nuevaClaveIdempotencia());
            avisar({ titulo: "Servidor de IA activado", descripcion: "Cada empresa ya puede elegirlo en los ajustes de su asistente." });
            router.replace(RUTA as Route);
        } catch (e) {
            setError(e);
            if (e instanceof ErrorApi && e.codigo === "huella_cambiada") {
                // Lo comparado ya no vale: se desmarca y se enseña la huella de ahora para compararla otra vez.
                // Nunca se reintenta solo.
                setHuellaMarcada(null);
                try {
                    const actual = await leerMotor();
                    setMotor(actual?.huellaTls && actual.estado === "sin_enlazar" ? actual : null);
                } catch {
                    setMotor(null);
                }
            }
        } finally {
            setOcupado(null);
        }
    }
    function empezarDeNuevo() {
        setMotor(null);
        setEnlace(null);
        setHuellaMarcada(null);
        setError(null);
    }

    const codigoError = error instanceof ErrorApi ? error.codigo : "";
    // «Conectar» ya dice si son compatibles: si no, se avisa aquí y no se deja activar.
    const incompatible = motor ? avisoIncompatibleAlEnlazar(motor.paquete) : null;
    const enCurso = codigoError === "rotacion_en_curso";
    const aviso = error && !vencidoAlla ? (
        <Aviso
            tono={enCurso ? "warning" : "error"}
            titulo={enCurso ? "Espera unos segundos" : codigoError === "huella_cambiada" ? "La huella ya no es la que comparaste" : paso === "huella" ? "No se pudo activar" : paso === "codigo" ? "No se pudo conectar" : "No se pudo empezar el enlace"}
            data-slot="error-enlace"
            data-codigo={codigoError}
        >
            {textoDe(error, paso === "codigo")}
        </Aviso>
    ) : null;

    return (
        <ol className="grid max-w-2xl gap-4" data-slot="pasos-enlace" data-paso-actual={paso}>
            <Paso numero={1} titulo="Instala Iris Voz en tu servidor" estado="hecho">
                <p className="text-sm text-muted-foreground">Pide a quien cuida el servidor que ejecute el instalador de Iris Voz. Al terminar muestra dos cosas que vas a necesitar aquí: la dirección del servidor y su huella.</p>
                <p className="text-sm text-muted-foreground">Si ya estaba instalado, este comando abre de nuevo la ventana para enlazar (dura 15 minutos):</p>
                <Comando texto={COMANDO.enlazar} />
            </Paso>

            <Paso numero={2} titulo={paso === "direccion" ? "Escribe la dirección del servidor" : "Escribe el código en el servidor"} estado={paso === "huella" ? "hecho" : "actual"}>
                {paso === "direccion" ? (
                    <form
                        noValidate
                        className="grid gap-3"
                        onSubmit={(ev) => {
                            ev.stopPropagation();
                            void form.handleSubmit(generar)(ev);
                        }}
                    >
                        {reenlace ? (
                            <Aviso tono="info" titulo="Este servidor está desactivado" data-slot="reenlace">
                                Para volver a usarlo se enlaza de nuevo, con un código nuevo. En el servidor, primero borra el enlace anterior con «{COMANDO.desenlazar}» y luego ejecuta «{COMANDO.enlazar}».
                            </Aviso>
                        ) : null}
                        {pendiente && !pendiente.huellaTls ? (
                            <Aviso tono="info" titulo="Quedó un enlace a medias">
                                El código de un solo uso solo se muestra una vez y ya no se puede volver a ver. Genera otro para seguir.
                            </Aviso>
                        ) : null}
                        <Campo etiqueta="Dirección" placeholder="https://10.0.0.5:8443" inputMode="url" autoComplete="off" autoCapitalize="none" spellCheck={false} ayuda="La que mostró el instalador, con su puerto." error={form.formState.errors.url?.message} {...form.register("url")} />
                        <Campo etiqueta="Nombre" ayuda="Para reconocerlo en Iris." maxLength={80} autoComplete="off" error={form.formState.errors.nombre?.message} {...form.register("nombre")} />
                        {aviso}
                        <div>
                            <Boton type="submit" disabled={form.formState.isSubmitting}>
                                <KeyRound />
                                {form.formState.isSubmitting ? "Generando…" : "Generar el código"}
                            </Boton>
                        </div>
                    </form>
                ) : paso === "codigo" && enlace ? (
                    <>
                        <p className="text-sm text-muted-foreground">
                            Este código sirve una sola vez y solo se muestra ahora. En el servidor, <span className="font-medium text-foreground">iris-voz pair</span> lo pide: escríbelo o pégalo allá (no se ve mientras se escribe, es normal).
                        </p>
                        <div className="flex min-w-0 items-center gap-2 rounded-lg bg-elevated py-2 pr-2 pl-3" data-slot="codigo-enlace">
                            <code className="min-w-0 flex-1 font-mono text-sm font-semibold [overflow-wrap:anywhere]">{enlace.codigo}</code>
                            <BotonCopiar texto={enlace.codigo} sensible />
                        </div>
                        {codigoVencido ? (
                            <Aviso
                                tono="warning"
                                titulo="El código venció"
                                data-slot="codigo-vencido"
                                accion={
                                    <Boton variante="secundario" tamano="sm" onClick={empezarDeNuevo}>
                                        <RotateCw />
                                        Generar otro
                                    </Boton>
                                }
                            >
                                Pasaron los 15 minutos. Genera otro y vuelve a ejecutar «{COMANDO.enlazar}» en el servidor.
                            </Aviso>
                        ) : (
                            <p className="text-[0.8125rem] text-muted-foreground" data-slot="codigo-vence">
                                Vence a las {Number.isNaN(vence) ? "—" : hora.format(vence)} (15 minutos). Si se equivocan al escribirlo no pasa nada: se puede volver a intentar.
                            </p>
                        )}
                        {aviso}
                        <div className="flex flex-wrap gap-2">
                            <Boton disabled={ocupado !== null || codigoVencido} onClick={() => void conectar()}>
                                <Link2 />
                                {ocupado === "conectar" ? "Conectando…" : "Ya lo escribieron: conectar"}
                            </Boton>
                            <Boton variante="fantasma" disabled={ocupado !== null} onClick={empezarDeNuevo}>
                                Cambiar la dirección
                            </Boton>
                        </div>
                    </>
                ) : (
                    <p className="flex flex-wrap items-center gap-x-2 text-sm" data-slot="servidor-encontrado">
                        Servidor encontrado en <span className="font-semibold [overflow-wrap:anywhere]">{motor?.url}</span>.
                        <Boton variante="enlace" onClick={empezarDeNuevo}>
                            Cambiar
                        </Boton>
                    </p>
                )}
            </Paso>

            <Paso numero={3} titulo="Compara la huella" estado={paso === "huella" ? (coincide ? "hecho" : "actual") : "despues"}>
                {paso === "huella" && motor?.huellaTls ? (
                    <>
                        <p className="text-sm text-muted-foreground">Tiene que ser la misma que imprimió el instalador en el servidor (también la muestra «{COMANDO.estado}»). Aquí va escrita igual que allá:</p>
                        <p aria-label="Huella del servidor" data-slot="huella" className="rounded-lg bg-elevated p-3 font-mono text-sm font-semibold [overflow-wrap:anywhere]">
                            {huellaComoElServidor(motor.huellaTls)}
                        </p>
                        <p className="text-[0.8125rem] text-muted-foreground">La misma, en grupos de cuatro para leerla más fácil (los espacios y las mayúsculas no cuentan):</p>
                        <p aria-hidden data-slot="huella-grupos" className="grid grid-cols-4 gap-x-3 gap-y-1 rounded-lg bg-elevated p-3 font-mono text-[0.8125rem] tracking-wide text-muted-foreground sm:grid-cols-8">
                            {huellaLegible(motor.huellaTls).map((grupo, i) => (
                                <span key={i}>{grupo}</span>
                            ))}
                        </p>
                        {incompatible ? (
                            <Aviso tono="warning" titulo={incompatible.titulo} data-slot="enlace-incompatible">
                                <span className="grid gap-2">
                                    <span>{incompatible.detalle}</span>
                                    {incompatible.comando ? <Comando texto={incompatible.comando} /> : null}
                                </span>
                            </Aviso>
                        ) : null}
                        <Casilla etiqueta="Coincide con la del instalador" checked={coincide} disabled={ocupado !== null} onChange={(ev) => setHuellaMarcada(ev.target.checked ? motor.huellaTls : null)} />
                        <p className="flex items-start gap-2 rounded-lg bg-elevated p-2.5 text-[0.8125rem] text-muted-foreground">
                            <ShieldAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
                            <span>
                                Si no coincide, <strong className="font-semibold text-foreground">no actives</strong>: podrías estar conectándote a otra máquina. Revisa la dirección y vuelve a empezar.
                            </span>
                        </p>
                    </>
                ) : (
                    <p className="text-sm text-muted-foreground">Cuando Iris se conecte, aquí aparece la huella que vio para que la compares con la del instalador.</p>
                )}
            </Paso>

            <Paso numero={4} titulo="Activar" estado={paso === "huella" && coincide ? "actual" : "despues"}>
                <p className="text-sm text-muted-foreground">Al activar, el código deja de servir y solo esta instalación de Iris puede hablar con el servidor. Hasta entonces Iris no le manda nada.</p>
                {paso === "huella" ? (
                    <>
                        {enlaceCaducado ? (
                            <Aviso
                                tono="warning"
                                titulo="Este enlace caducó sin activarse"
                                data-slot="enlace-caducado"
                                accion={
                                    <Boton variante="secundario" tamano="sm" onClick={empezarDeNuevo}>
                                        <RotateCw />
                                        Empezar de nuevo
                                    </Boton>
                                }
                            >
                                Un enlace sin activar vale 24 horas. Hay que repetirlo desde la dirección.
                            </Aviso>
                        ) : !Number.isNaN(caduca) ? (
                            <p className="text-[0.8125rem] text-muted-foreground" data-slot="enlace-caduca">
                                Si no lo activas, este enlace caduca el {diaHora.format(caduca)} y hay que repetirlo.
                            </p>
                        ) : null}
                        {aviso}
                        <div>
                            <Boton disabled={!coincide || ocupado !== null || enlaceCaducado || incompatible !== null} onClick={() => void activar()}>
                                <Link2 />
                                {ocupado === "activar" ? "Activando…" : "Activar"}
                            </Boton>
                        </div>
                    </>
                ) : null}
            </Paso>
        </ol>
    );
}
