"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, BotonManualPantalla, Campo, CampoArea, CampoNumero, CampoSelector, cn, FormularioPagina, Interruptor, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { ArrowLeft, CircleAlert, CircleCheck, CircleHelp, CircleX, PhoneIncoming, RefreshCw, RotateCw } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { EtiquetaEnPruebas, FranjaEnPruebas } from "@/componentes/voz/en-pruebas";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { leerNumeros } from "@/lib/conversaciones/api-equipo";
import { formatearNumero } from "@/lib/conversaciones/presentacion";
import type { Numero } from "@/lib/conversaciones/tipos";
import { haceCuanto } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { resumirFranjas } from "@/lib/horarios/franjas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { leerCanales } from "@/lib/numeros/api";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";
import { entradaDeCanal, esquemaLlamadasCanal, motivoNoEncender, valoresDeCanal, type ValoresLlamadasCanal } from "@/lib/voz/ajustes";
import { aplicarLlamadasCanal, comprobarLlamadasCanal, configurarLlamadasCanal, leerLlamadasCanal, sincronizarLlamadasCanal } from "@/lib/voz/api";
import { textoRechazadasApagadas, textoRequisito } from "@/lib/voz/presentacion";
import type { LlamadasCanal } from "@/lib/voz/tipos";

/** Lo que hace falta del número para titular la pantalla. */
type NumeroCorto = Pick<Numero, "id" | "nombre" | "numero">;
type Datos = { numero: NumeroCorto; llamadas: LlamadasCanal; colas: Schema<"ColaConversaciones">[]; horarios: Schema<"Horario">[] };

/** `/administracion/numeros/[id]/llamadas`: llamadas de WhatsApp de un número (contratos/voz.md §3.9). */
export function LlamadasDelNumero() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const { puede } = useSesion();
    const conCredenciales = puede(["mensajeria.configurar"]);
    const lectura = useLectura<Datos | null>(async (signal) => {
        if (id === null) return null;
        const [numeros, llamadas, colas, horarios] = await Promise.all([
            leerNumeros(signal),
            leerLlamadasCanal(id, signal),
            resolver(api().GET("/api/v1/mensajeria/colas", { signal })),
            resolver(api().GET("/api/v1/mensajeria/horarios", { signal })),
        ]);
        // Los números de la bandeja; uno que aún no está en ella sale de la lista de números de Meta.
        const numero: NumeroCorto | undefined = numeros.find((n) => n.id === id) ?? (conCredenciales ? (await leerCanales(signal)).find((n) => n.id === id) : undefined);
        return numero ? { numero, llamadas, colas: colas.data ?? [], horarios: horarios.data ?? [] } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.numero.nombre : null);
    const { reemplazar, reintentar } = lectura;
    return (
        <CargaRegistro lectura={lectura} que="las llamadas del número" noEncontrado="Número no encontrado">
            {(d) =>
                d.llamadas.tipoConexion === "qr" ? (
                    <NumeroPorQr numero={d.numero} />
                ) : (
                    // La versión remonta el formulario: tras comprobar o sincronizar se parte de lo nuevo.
                    <Formulario key={d.llamadas.version} {...d} alCambiar={(llamadas) => reemplazar({ ...d, llamadas })} alRecargar={reintentar} />
                )
            }
        </CargaRegistro>
    );
}

/** Regla de fábrica: llamadas solo en números por API; nunca en uno por QR. */
function NumeroPorQr({ numero }: { numero: NumeroCorto }) {
    return (
        <div className="mx-auto grid max-w-2xl gap-4" data-slot="llamadas-qr">
            <div className="grid gap-1">
                <div className="flex min-w-0 items-center gap-1.5">
                    <h1 className="text-xl font-bold">Llamadas de {numero.nombre}</h1>
                    <BotonManualPantalla />
                </div>
                <p className="text-sm text-muted-foreground">{formatearNumero(numero.numero)}</p>
            </div>
            <FranjaEnPruebas />
            <Aviso tono="info" titulo="Este número no puede recibir llamadas">
                Está conectado por QR (la app WhatsApp Business del teléfono). Meta solo permite llamadas en los números conectados por la API de WhatsApp. Los mensajes siguen funcionando igual.
            </Aviso>
            <div>
                <Boton asChild variante="contorno">
                    <Link href="/administracion/numeros">
                        <ArrowLeft />
                        Volver a los números
                    </Link>
                </Boton>
            </div>
        </div>
    );
}

/**
 * Lo guardado también vive en WhatsApp (encendido, botón de llamar, devolución y horario): si aún
 * no se aplicó allá, se dice; si se agotaron los reintentos, error con «Reintentar».
 */
function EnWhatsApp({ llamadas, ahora, ocupado, aplicando, alReintentar }: { llamadas: LlamadasCanal; ahora: number; ocupado: boolean; aplicando: boolean; alReintentar: () => void }) {
    const w = llamadas.whatsapp;
    if (w.estado === "aplicado") return null;
    const reintentar = (
        <Boton type="button" variante="secundario" tamano="sm" disabled={ocupado} onClick={alReintentar}>
            <RotateCw />
            {aplicando ? "Aplicando…" : w.estado === "error" ? "Reintentar" : "Aplicar ahora"}
        </Boton>
    );
    if (w.estado === "error") {
        return (
            <Aviso tono="error" titulo="No se pudo aplicar en WhatsApp" accion={reintentar} data-slot="en-whatsapp" data-estado="error">
                {w.detalle ?? "WhatsApp no aceptó los ajustes de este número."} Lo guardado en Iris no se pierde.
            </Aviso>
        );
    }
    const proximo = w.proximoIntentoEn ? Date.parse(w.proximoIntentoEn) : NaN;
    const minutos = Number.isNaN(proximo) ? null : Math.max(1, Math.ceil((proximo - ahora) / 60_000));
    return (
        <Aviso tono="warning" titulo="Pendiente de aplicar en WhatsApp" accion={reintentar} data-slot="en-whatsapp" data-estado="pendiente">
            {w.detalle ? `${w.detalle} ` : ""}
            {minutos === null ? "Iris lo vuelve a intentar solo." : `Iris lo vuelve a intentar solo en ${minutos === 1 ? "1 minuto" : `${minutos} minutos`}.`}
        </Aviso>
    );
}

const ICONO_REQUISITO = { cumple: CircleCheck, falta: CircleX, aviso: CircleAlert, sin_comprobar: CircleHelp } as const;
const CLASE_REQUISITO = { cumple: "text-success", falta: "text-error", aviso: "text-warning", sin_comprobar: "text-muted-foreground" } as const;
const TEXTO_REQUISITO = { cumple: "Listo", falta: "Falta", aviso: "Recomendado", sin_comprobar: "Sin comprobar" } as const;

function Formulario({ numero, llamadas, colas, horarios, alCambiar, alRecargar }: Datos & { alCambiar: (l: LlamadasCanal) => void; alRecargar: () => void }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [meta, setMeta] = useState<{ accion: "comprobar" | "sincronizar" | "aplicar" | null; error: unknown }>({ accion: null, error: null });
    const [ahora] = useState(() => Date.now());
    const form = useForm<ValoresLlamadasCanal>({ resolver: zodResolver(esquemaLlamadasCanal), defaultValues: valoresDeCanal(llamadas) });
    const v = useWatch({ control: form.control });
    const errores = form.formState.errors;
    const sucio = form.formState.isDirty;
    const noEncender = motivoNoEncender(llamadas);
    const horario = horarios.find((h) => String(h.id) === v.horarioId);
    const cola = colas.find((c) => String(c.id) === v.colaId);
    const requisitos = llamadas.requisitos.requisitos ?? [];

    async function conMeta(accion: "comprobar" | "sincronizar" | "aplicar") {
        setMeta({ accion, error: null });
        try {
            const nuevo = await (accion === "comprobar" ? comprobarLlamadasCanal(numero.id) : accion === "sincronizar" ? sincronizarLlamadasCanal(numero.id) : aplicarLlamadasCanal(numero.id));
            const titulo =
                accion === "comprobar"
                    ? nuevo.requisitos.cumple
                        ? "El número cumple los requisitos"
                        : "Todavía faltan requisitos"
                    : accion === "sincronizar"
                      ? "Ajustes leídos de Meta"
                      : nuevo.whatsapp.estado === "aplicado"
                        ? "Ajustes aplicados en WhatsApp"
                        : "WhatsApp todavía no los aplica";
            avisar({ titulo, ...(accion === "aplicar" && nuevo.whatsapp.estado !== "aplicado" ? { tono: "warning" as const } : {}) });
            setMeta({ accion: null, error: null });
            alCambiar(nuevo);
        } catch (e) {
            setMeta({ accion: null, error: e });
        }
    }

    async function guardar(d: ValoresLlamadasCanal) {
        setError(null);
        try {
            const r = await configurarLlamadasCanal(numero.id, entradaDeCanal(d, llamadas.version), clave.actual());
            clave.renovar();
            avisar({ titulo: r.llamadasActivas ? `${numero.nombre} ya recibe llamadas` : `Llamadas de ${numero.nombre} guardadas` });
            await liberarProteccion();
            router.replace("/administracion/numeros");
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && (e.codigo === "requisitos_sin_cumplir" || e.codigo === "llamadas_no_disponibles_qr")) {
                setError(e);
                return;
            }
            if (e instanceof ErrorApi && e.codigo === "plantilla_permiso_invalida") {
                // La cuenta de WhatsApp no tiene esa plantilla aprobada en ese idioma: se dice junto al campo.
                form.setError("plantillaPermiso", { message: "WhatsApp no tiene aprobada una plantilla con ese nombre en ese idioma. Revisa el nombre y el idioma en Meta (tiene que ser de permiso de llamada)." }, { shouldFocus: true });
                return;
            }
            const campos = erroresACampos(e, ["llamadasActivas", "colaId", "horarioId", "segundosSonando", "fueraDeHorario", "iconoVisible", "devolucion", "plantillaPermiso", "plantillaPermisoIdioma"], form.setError);
            const textoMensaje = e instanceof ErrorApi ? e.errorDeCampo("mensajePerdida.texto") : undefined;
            if (textoMensaje) form.setError("mensajeTexto", { message: textoMensaje });
            if (!campos && !textoMensaje) setError(e);
        }
    }

    // Solo el conflicto de versión es «cambiaron mientras tanto»; otro 409 se dice con su propio texto.
    const conflicto = error instanceof ErrorApi && error.codigo === "conflicto_version";
    const sinRequisitos = error instanceof ErrorApi && error.codigo === "requisitos_sin_cumplir";
    return (
        <FormularioPagina
            titulo={`Llamadas de ${numero.nombre}`}
            descripcion={formatearNumero(numero.numero)}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/numeros"
            sucio={sucio}
            barra={{ textoGuardar: "Guardar", enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Los ajustes cambiaron mientras tanto: recarga para ver cómo quedaron." } : {}) }}
            error={
                error ? (
                    <Aviso
                        tono="error"
                        titulo={conflicto ? "Los ajustes de este número cambiaron mientras tanto" : sinRequisitos ? "Todavía no se pueden encender las llamadas" : "No se pudo guardar"}
                        {...(conflicto
                            ? {
                                  accion: (
                                      // Otra persona los editó, o Iris volvió a comprobar con Meta: se lee lo nuevo y se intenta otra vez.
                                      <Boton
                                          type="button"
                                          variante="secundario"
                                          tamano="sm"
                                          onClick={() => {
                                              void liberarProteccion().then(alRecargar);
                                          }}
                                      >
                                          <RotateCw />
                                          Recargar
                                      </Boton>
                                  ),
                              }
                            : {})}
                    >
                        {conflicto ? (
                            "Lo que cambiaste aquí no se guardó. Recarga para ver cómo quedó y vuelve a intentarlo."
                        ) : sinRequisitos && error instanceof ErrorApi && error.errores.length > 0 ? (
                            <ul className="grid list-disc gap-0.5 pl-4">
                                {error.errores.map((x, i) => (
                                    <li key={i}>{x.message}</li>
                                ))}
                            </ul>
                        ) : (
                            (mensajeDeError(error).detalle ?? mensajeDeError(error).titulo)
                        )}
                    </Aviso>
                ) : null
            }
        >
            <FranjaEnPruebas />
            <EnWhatsApp llamadas={llamadas} ahora={ahora} ocupado={meta.accion !== null || sucio} aplicando={meta.accion === "aplicar"} alReintentar={() => void conMeta("aplicar")} />
            {llamadas.rechazadasApagadas ? (
                <Aviso
                    tono="warning"
                    titulo="Se están rechazando llamadas de clientes"
                    data-slot="rechazadas-apagadas"
                    {...(noEncender || v.llamadasActivas
                        ? {}
                        : {
                              accion: (
                                  <Boton
                                      type="button"
                                      variante="secundario"
                                      tamano="sm"
                                      onClick={() => {
                                          form.setValue("llamadasActivas", true, { shouldDirty: true, shouldValidate: true });
                                          form.setFocus("llamadasActivas");
                                      }}
                                  >
                                      <PhoneIncoming />
                                      Encender las llamadas
                                  </Boton>
                              ),
                          })}
                >
                    {textoRechazadasApagadas(llamadas.rechazadasApagadas, ahora)}{" "}
                    {noEncender ? "Para encenderlas, primero hay que cumplir los requisitos de Meta de abajo." : v.llamadasActivas ? "Quedan encendidas al guardar." : "Enciéndelas y guarda para empezar a recibirlas."}
                </Aviso>
            ) : null}
            <SeccionFormulario titulo="Requisitos de Meta" descripcion="Meta solo deja recibir llamadas en los números que cumplen esto.">
                <ul className="grid gap-2" aria-label="Requisitos" data-slot="requisitos" data-cumple={llamadas.requisitos.cumple ? "si" : "no"}>
                    {requisitos.map((r) => {
                        const t = textoRequisito(r);
                        const Icono = ICONO_REQUISITO[t.estado];
                        return (
                            <li key={r.codigo} data-requisito={r.codigo} data-estado={t.estado} className="flex min-w-0 items-start gap-2.5">
                                <Icono aria-hidden className={cn("mt-0.5 size-4 shrink-0", CLASE_REQUISITO[t.estado])} />
                                <div className="grid min-w-0 flex-1">
                                    <span className="text-sm">
                                        <span className={cn("font-semibold", CLASE_REQUISITO[t.estado])}>{TEXTO_REQUISITO[t.estado]}:</span> {t.nombre}
                                    </span>
                                    {t.estado === "sin_comprobar" && !t.detalle ? <span className="text-[0.8125rem] text-muted-foreground">Iris no lo puede leer de Meta: se sabrá cuando llegue la primera llamada. No impide encender.</span> : null}
                                    {t.detalle && t.estado !== "cumple" ? <span className="text-[0.8125rem] text-muted-foreground">{t.detalle}</span> : null}
                                </div>
                            </li>
                        );
                    })}
                </ul>
                {requisitos.length === 0 ? <p className="text-sm text-muted-foreground">Todavía no se han comprobado.</p> : null}
                <p className="text-xs text-muted-foreground">
                    {llamadas.requisitos.comprobadoEn ? `Comprobado ${haceCuanto(llamadas.requisitos.comprobadoEn, ahora)}.` : "Sin comprobar."}
                    {llamadas.sincronizadoEn ? ` Ajustes leídos de Meta ${haceCuanto(llamadas.sincronizadoEn, ahora)}.` : ""}
                </p>
                <div className="flex flex-wrap gap-2">
                    <Boton type="button" variante="secundario" tamano="sm" disabled={meta.accion !== null || sucio} onClick={() => void conMeta("comprobar")}>
                        <RotateCw />
                        {meta.accion === "comprobar" ? "Comprobando…" : "Comprobar otra vez"}
                    </Boton>
                    <Boton type="button" variante="contorno" tamano="sm" disabled={meta.accion !== null || sucio} onClick={() => void conMeta("sincronizar")}>
                        <RefreshCw />
                        {meta.accion === "sincronizar" ? "Leyendo de Meta…" : "Leer ajustes de Meta"}
                    </Boton>
                </div>
                {sucio ? <p className="text-xs text-muted-foreground">Guarda tus cambios antes de comprobar o leer de Meta.</p> : null}
                {meta.error ? (
                    <Aviso tono="error" titulo="No se pudo consultar a Meta">
                        {mensajeDeError(meta.error).detalle ?? mensajeDeError(meta.error).titulo}
                    </Aviso>
                ) : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="Recibir llamadas">
                <EtiquetaEnPruebas />
                <Interruptor
                    etiqueta="Este número recibe llamadas de WhatsApp"
                    ayuda={noEncender && !v.llamadasActivas ? noEncender : "Los clientes ven el botón de llamar en el chat de WhatsApp."}
                    disabled={noEncender !== null && !v.llamadasActivas}
                    error={errores.llamadasActivas?.message}
                    {...form.register("llamadasActivas")}
                />
                <Interruptor etiqueta="Mostrar el botón de llamar en WhatsApp" ayuda="Apagado, el número sigue pudiendo recibir llamadas pero el cliente no ve el botón." error={errores.iconoVisible?.message} {...form.register("iconoVisible")} />
            </SeccionFormulario>

            <SeccionFormulario titulo="A quién le suena" descripcion="Solo a quien tiene Iris abierto, está «Disponible» y encendió «Recibir llamadas» en su navegador.">
                <CampoSelector etiqueta="Cola que contesta" error={errores.colaId?.message} {...form.register("colaId")}>
                    <option value="">La cola del número</option>
                    {colas
                        .filter((c) => c.activa || String(c.id) === v.colaId)
                        .map((c) => (
                            <option key={c.id} value={String(c.id)}>
                                {c.nombre}
                            </option>
                        ))}
                </CampoSelector>
                <p className="text-sm text-muted-foreground">
                    Suena primero a quien tiene asignada la conversación y después a {cola ? `las personas de «${cola.nombre}»` : "las personas de la cola del número"}. La contesta quien toque «Contestar» primero.
                </p>
                <CampoNumero
                    etiqueta="Segundos que suena"
                    minimo={15}
                    maximo={45}
                    ayuda="Si nadie contesta en este tiempo, queda como llamada perdida. De fábrica: 30."
                    error={errores.segundosSonando?.message}
                    {...form.register("segundosSonando")}
                />
            </SeccionFormulario>

            <SeccionFormulario titulo="Horario" descripcion="Fuera de este horario las llamadas no suenan.">
                <CampoSelector etiqueta="Horario de las llamadas" error={errores.horarioId?.message} {...form.register("horarioId")}>
                    <option value="">El de la cola o el del número</option>
                    {horarios.map((h) => (
                        <option key={h.id} value={String(h.id)}>
                            {h.nombre}
                        </option>
                    ))}
                </CampoSelector>
                <p className="text-sm text-muted-foreground">
                    {horario ? resumirFranjas(horario.franjas) : "Vale el horario de la cola y, si no tiene, el del número (sin ninguno, siempre abierto)."}{" "}
                    <Link href="/administracion/horarios" className="text-primary underline-offset-2 hover:underline">
                        Administrar horarios
                    </Link>
                </p>
                <p className="text-sm text-muted-foreground" data-slot="horas-whatsapp" data-espejo={llamadas.horasWhatsapp ? "si" : "no"}>
                    {llamadas.horasWhatsapp
                        ? "WhatsApp también tiene este horario: fuera de él, el cliente ni siquiera puede llamar."
                        : "En WhatsApp el número aparece abierto siempre; el horario lo aplica Iris cuando entra la llamada."}
                </p>
                <CampoSelector etiqueta="Si llaman fuera de horario" error={errores.fueraDeHorario?.message} {...form.register("fueraDeHorario")}>
                    <option value="rechazar_con_mensaje">No contestar y mandarle el mensaje de fuera de horario</option>
                    <option value="buzon">Que deje un mensaje en el buzón de voz de WhatsApp</option>
                </CampoSelector>
            </SeccionFormulario>

            <SeccionFormulario titulo="Llamadas perdidas">
                <Interruptor etiqueta="Escribirle al cliente cuando nadie contesta" ayuda="Como mucho una vez por hora a cada cliente." {...form.register("mensajeActivo")} />
                <CampoArea etiqueta="Mensaje" rows={3} maxLength={500} error={errores.mensajeTexto?.message} {...form.register("mensajeTexto")} />
                <Interruptor etiqueta="Preguntarle al cliente si acepta que lo llamen de vuelta" ayuda="WhatsApp se lo pregunta después de una llamada perdida. Si acepta, se le puede devolver la llamada desde Iris." error={errores.devolucion?.message} {...form.register("devolucion")} />
            </SeccionFormulario>

            <SeccionFormulario
                titulo="Permiso para llamar al cliente"
                descripcion="WhatsApp solo deja llamar a quien aceptó recibir llamadas de la empresa. Si el cliente escribió en las últimas 24 horas, Iris se lo pide con un mensaje; si no, hace falta una plantilla de permiso de llamada aprobada en Meta."
            >
                <EtiquetaEnPruebas />
                <div className="grid gap-4 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]" data-slot="plantilla-permiso">
                    <Campo
                        etiqueta="Plantilla de permiso de llamada"
                        placeholder="permiso_de_llamada"
                        autoComplete="off"
                        autoCapitalize="none"
                        spellCheck={false}
                        maxLength={200}
                        ayuda="El nombre tal como está en Meta. Vacío: solo se puede pedir permiso a quien escribió en las últimas 24 horas."
                        error={errores.plantillaPermiso?.message}
                        {...form.register("plantillaPermiso")}
                    />
                    <Campo etiqueta="Idioma" placeholder="es" autoComplete="off" autoCapitalize="none" spellCheck={false} maxLength={5} ayuda="Como «es» o «es_MX»." error={errores.plantillaPermisoIdioma?.message} {...form.register("plantillaPermisoIdioma")} />
                </div>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
