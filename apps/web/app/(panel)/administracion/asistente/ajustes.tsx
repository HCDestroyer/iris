"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoNumero,
    CampoSecreto,
    CampoSelector,
    Casilla,
    cn,
    FormularioPagina,
    Interruptor,
    liberarProteccion,
    ListaEleccion,
    SeccionFormulario,
} from "@iris/ui";
import { Ban, CircleAlert, CircleCheck, Cloud, RefreshCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { useForm, useWatch, type UseFormReturn } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ErrorLectura } from "@/componentes/error-lectura";
import { SecretoCambiable } from "@/componentes/numeros/secreto-guardado";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cambiaDestino, ejemploUrl, entradaDe, esNube, esquemaAjustesAsistente, FUENTES, NUNCA, PROVEEDORES, TEXTO_CLAVE_REQUERIDA, urlValida, valoresDe, type ValoresAjustes } from "@/lib/asistente/ajustes";
import { consultarModelos, guardarAjustesAsistente, leerAjustesAsistente, leerModelosGuardados, leerUso, probarAsistente } from "@/lib/asistente/api";
import { formatearTokens, nombreMes, porcentajeUsadas, textoPausado, textoPrueba } from "@/lib/asistente/presentacion";
import type { AjustesAsistente as Ajustes, ModeloIA, PruebaAsistente, UsoAsistente } from "@/lib/asistente/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const CAMPOS_SERVIDOR = ["url", "clave", "modelo", "topeTokensMes", "precioEntradaMillon", "precioSalidaMillon"] as const;

/**
 * `/administracion/asistente` (`mensajeria.asistente_configurar`): el asistente de IA que sugiere
 * respuestas en la bandeja (contratos/conversaciones.md §16.1). Proveedor, dirección, clave (solo
 * escritura: «•••• guardada el …»), modelo, cuándo sugiere, qué puede consultar, lo que nunca hace
 * y el uso del mes con su tope.
 */
export function AjustesAsistente() {
    const lectura = useLectura((signal) => leerAjustesAsistente(signal));
    return (
        <CargaRegistro lectura={lectura} que="los ajustes del asistente" noEncontrado="Sin ajustes">
            {(a) => <Formulario ajustes={a} />}
        </CargaRegistro>
    );
}

function Formulario({ ajustes }: { ajustes: Ajustes }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<ValoresAjustes>({ resolver: zodResolver(esquemaAjustesAsistente), defaultValues: valoresDe(ajustes) });
    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const [proveedor, url, cambiarClave, quitarClave, modo, activo] = useWatch({ control: form.control, name: ["proveedor", "url", "cambiarClave", "quitarClave", "modo", "activo"] });
    const errores = form.formState.errors;
    const guardada = ajustes.claveGuardadaEn !== null;
    // La clave guardada no viaja a otro servidor (§16.1): con otro proveedor o dirección hay que escribirla.
    const destinoCambiado = guardada && cambiaDestino({ proveedor, url, proveedorGuardado: ajustes.proveedor, urlGuardada: ajustes.url });
    function abrirClaveSiCambia() {
        const d = form.getValues();
        if (guardada && !d.quitarClave && !d.cambiarClave && cambiaDestino(d)) form.setValue("cambiarClave", true);
    }
    const cargarModelos = useRef<(() => void) | null>(null);
    // La lista de modelos se pide al salir de «Dirección», no con cada tecla.
    const regUrl = form.register("url", { onChange: abrirClaveSiCambia });
    const alSalirDeUrl = useCallback((e: { target: { value: string } }) => {
        const v = e.target.value.trim();
        if (v && urlValida(v)) cargarModelos.current?.();
    }, []);

    return (
        <FormularioPagina
            titulo="Asistente de IA"
            descripcion="Sugiere respuestas en la bandeja. Nunca envía nada por su cuenta."
            alEnviar={form.handleSubmit(async (d) => {
                setError(null);
                try {
                    const r = await guardarAjustesAsistente(entradaDe(d, ajustes.version), clave.actual());
                    clave.renovar();
                    avisar({ titulo: r.activo ? "Asistente de IA guardado y activo" : "Asistente de IA guardado (apagado)" });
                    await liberarProteccion();
                    router.replace("/administracion");
                } catch (e) {
                    clave.trasError(e);
                    if (e instanceof ErrorApi && e.codigo === "clave_requerida") {
                        // El campo tiene que estar a la vista antes de marcarle el error.
                        form.setValue("cambiarClave", true);
                        form.setValue("quitarClave", false);
                        form.setError("clave", { message: TEXTO_CLAVE_REQUERIDA }, { shouldFocus: true });
                    } else if (e instanceof ErrorApi && e.codigo === "url_no_permitida") {
                        form.setError("url", { message: e.detalle ?? e.titulo }, { shouldFocus: true });
                    } else if (!erroresACampos(e, CAMPOS_SERVIDOR, form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA="/administracion"
            sucio={form.formState.isDirty}
            barra={{
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió los ajustes: vuelve a abrirlos." } : {}),
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió los ajustes" : "No se pudo guardar"}>
                        {conflicto ? "Recarga la página para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Estado">
                <Interruptor
                    etiqueta="Asistente activo"
                    ayuda={activo ? "Sugiere respuestas a quienes atienden la bandeja." : "Apagado: no sugiere ni resume."}
                    error={errores.activo?.message}
                    {...form.register("activo")}
                />
            </SeccionFormulario>

            <SeccionFormulario titulo="Proveedor de IA">
                <ListaEleccion
                    leyenda="Proveedor de IA"
                    leyendaOculta
                    nombre="proveedor"
                    valor={proveedor}
                    alCambiar={(v) => {
                        form.setValue("proveedor", v as ValoresAjustes["proveedor"], { shouldDirty: true });
                        abrirClaveSiCambia();
                    }}
                    opciones={PROVEEDORES.map((p) => ({
                        valor: p.valor,
                        texto: p.titulo,
                        contenido: (
                            <span className="grid min-w-0 gap-0.5">
                                <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold">
                                    {p.titulo}
                                    {p.recomendado ? <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">Recomendado</span> : null}
                                </span>
                                <span className="text-sm text-muted-foreground">{p.detalle}</span>
                            </span>
                        ),
                    }))}
                />
                <Campo
                    etiqueta="Dirección"
                    inputMode="url"
                    autoComplete="off"
                    spellCheck={false}
                    placeholder={ejemploUrl(proveedor)}
                    ayuda={proveedor === "anthropic" ? "Déjala vacía para usar la de Anthropic." : proveedor === "ollama" ? "La del servidor de Ollama en tu red, con su puerto." : "La URL base del servicio (termina en /v1 o sin él)."}
                    error={errores.url?.message}
                    {...regUrl}
                    onBlur={(e) => {
                        void regUrl.onBlur(e);
                        alSalirDeUrl(e);
                    }}
                />
                {ajustes.claveGuardadaEn && quitarClave ? (
                    <div className="grid gap-1" data-slot="clave-quitada">
                        <p className="text-sm font-medium">Clave</p>
                        <p className="text-sm text-muted-foreground">
                            La clave guardada se quitará al guardar.{" "}
                            <Boton variante="enlace" tamano="sm" className="h-auto px-0 align-baseline" onClick={() => form.setValue("quitarClave", false, { shouldDirty: true })}>
                                Deshacer
                            </Boton>
                        </p>
                    </div>
                ) : ajustes.claveGuardadaEn ? (
                    <div className="grid gap-1.5">
                        {destinoCambiado ? (
                            <p className="text-sm text-warning" data-slot="clave-requerida">
                                {TEXTO_CLAVE_REQUERIDA}
                                {proveedor === "ollama" ? " o quitarla" : ""}.
                            </p>
                        ) : null}
                        <SecretoCambiable
                            etiqueta="Clave"
                            femenino
                            guardadoEn={ajustes.claveGuardadaEn}
                            abierto={cambiarClave}
                            abrir={() => form.setValue("cambiarClave", true)}
                            cerrar={() => {
                                form.setValue("clave", "");
                                form.clearErrors("clave");
                                // Con otro destino la guardada no sirve: el campo sigue abierto.
                                if (!destinoCambiado) form.setValue("cambiarClave", false);
                            }}
                            campo={<CampoClave form={form} proveedor={proveedor} />}
                        />
                        {proveedor === "ollama" ? (
                            <div>
                                <Boton
                                    variante="enlace"
                                    tamano="sm"
                                    className="h-auto px-0"
                                    onClick={() => {
                                        form.setValue("quitarClave", true, { shouldDirty: true });
                                        form.setValue("cambiarClave", false);
                                        form.setValue("clave", "");
                                        form.clearErrors("clave");
                                    }}
                                >
                                    Quitar clave
                                </Boton>
                            </div>
                        ) : null}
                    </div>
                ) : (
                    <CampoClave form={form} proveedor={proveedor} />
                )}
                <SelectorModelo form={form} ajustes={ajustes} registrarCargar={(f) => (cargarModelos.current = f)} />
                <Prueba form={form} destinoCambiado={destinoCambiado} />
                {esNube(proveedor) ? (
                    <Aviso tono="info" icono={Cloud} titulo="Los mensajes salen hacia el proveedor">
                        Con un proveedor en la nube, los mensajes del cliente salen de tu servidor hacia ese proveedor. Contraseñas, documentos y códigos nunca salen: llegan como ***.
                    </Aviso>
                ) : null}
            </SeccionFormulario>

            <SeccionFormulario titulo="Cuándo sugiere">
                <ListaEleccion
                    leyenda="Cuándo sugiere"
                    leyendaOculta
                    nombre="modo"
                    valor={modo}
                    alCambiar={(v) => form.setValue("modo", v as ValoresAjustes["modo"], { shouldDirty: true })}
                    opciones={[
                        {
                            valor: "automatico",
                            texto: "Al llegar un mensaje del cliente",
                            contenido: <OpcionTexto titulo="Al llegar un mensaje del cliente" detalle="La sugerencia ya está lista cuando abres la conversación." />,
                        },
                        { valor: "boton", texto: "Solo al pulsar «Sugerir»", contenido: <OpcionTexto titulo="Solo al pulsar «Sugerir»" detalle="Gasta menos: solo cuando la pides." /> },
                    ]}
                />
            </SeccionFormulario>

            <SeccionFormulario titulo="Qué puede consultar">
                <div className="grid gap-2">
                    {FUENTES.map((f) => (
                        <Casilla key={f.clave} etiqueta={f.titulo} ayuda={f.detalle} {...form.register(`fuentes.${f.clave}`)} />
                    ))}
                </div>
            </SeccionFormulario>

            <SeccionFormulario titulo="Nunca">
                <ul className="grid gap-1.5" aria-label="Lo que el asistente nunca hace">
                    {NUNCA.map((t) => (
                        <li key={t} className="flex items-start gap-2 text-sm">
                            <Ban aria-hidden className="mt-0.5 size-4 shrink-0 text-error" />
                            {t}
                        </li>
                    ))}
                </ul>
            </SeccionFormulario>

            <SeccionFormulario titulo="Uso del mes">
                <Uso />
                <div className="grid gap-3 sm:grid-cols-3">
                    <CampoNumero
                        etiqueta="Tope mensual (tokens)"
                        minimo={1000}
                        maximo={1_000_000_000}
                        ayuda="Al 80 % avisa a los administradores; al 100 % se pausa hasta el mes siguiente."
                        error={errores.topeTokensMes?.message}
                        {...form.register("topeTokensMes")}
                    />
                    <CampoNumero etiqueta="Precio por millón (entrada)" minimo={0} placeholder="Opcional" error={errores.precioEntradaMillon?.message} {...form.register("precioEntradaMillon")} />
                    <CampoNumero etiqueta="Precio por millón (salida)" minimo={0} placeholder="Opcional" error={errores.precioSalidaMillon?.message} {...form.register("precioSalidaMillon")} />
                </div>
                <p className="text-sm text-muted-foreground">Con un proveedor que cobra, pon sus dos precios por millón de tokens y el uso se muestra también en dinero.</p>
            </SeccionFormulario>
        </FormularioPagina>
    );
}

function OpcionTexto({ titulo, detalle }: { titulo: string; detalle: string }) {
    return (
        <span className="grid min-w-0 gap-0.5">
            <span className="text-sm font-semibold">{titulo}</span>
            <span className="text-sm text-muted-foreground">{detalle}</span>
        </span>
    );
}

function CampoClave({ form, proveedor }: { form: UseFormReturn<ValoresAjustes>; proveedor: ValoresAjustes["proveedor"] }) {
    return (
        <CampoSecreto
            etiqueta="Clave"
            nombre="la clave"
            placeholder={proveedor === "ollama" ? "No hace falta para Ollama" : "Pega la clave del proveedor"}
            ayuda="Se guarda cifrada y nunca se vuelve a mostrar."
            error={form.formState.errors.clave?.message}
            {...form.register("clave")}
        />
    );
}

type ListaModelos = { estado: "inicial" } | { estado: "cargando" } | { estado: "listo"; modelos: ModeloIA[] } | { estado: "error"; motivo: string };

/** Por qué no llegó la lista o la prueba: el `detail` del servidor (502 `proveedor_error`, 422…) o la red. */
function motivoModelos(e: unknown): string {
    return e instanceof ErrorApi && e.estado !== 0 ? (e.detalle ?? e.titulo) : mensajeDeError(e).titulo;
}

/**
 * Modelo: la lista del proveedor guardado (`GET /asistente/modelos`) o, con otro proveedor,
 * dirección o clave, de `POST /asistente/modelos`. Se pide al abrir (si hay algo guardado), al
 * salir de «Dirección» y con «Cargar modelos»; nunca con cada tecla. Si falla, dice por qué y deja
 * escribirlo a mano.
 */
function SelectorModelo({ form, ajustes, registrarCargar }: { form: UseFormReturn<ValoresAjustes>; ajustes: Ajustes; registrarCargar: (f: () => void) => void }) {
    const [proveedor, modelo] = useWatch({ control: form.control, name: ["proveedor", "modelo"] });
    const hayGuardado = ajustes.url !== "" || (ajustes.proveedor === "anthropic" && ajustes.claveGuardadaEn !== null);
    const [lista, setLista] = useState<ListaModelos>(hayGuardado ? { estado: "cargando" } : { estado: "inicial" });
    const [aMano, setAMano] = useState(false);
    const control = useRef<AbortController | null>(null);

    function pedir(leer: (signal: AbortSignal) => Promise<ModeloIA[]>) {
        control.current?.abort();
        const c = new AbortController();
        control.current = c;
        leer(c.signal).then(
            (modelos) => !c.signal.aborted && setLista({ estado: "listo", modelos }),
            (e: unknown) => !c.signal.aborted && setLista({ estado: "error", motivo: motivoModelos(e) }),
        );
    }

    function cargar() {
        const d = form.getValues();
        const url = d.url.trim();
        if (url && !urlValida(url)) {
            void form.trigger("url");
            return;
        }
        const clave = d.cambiarClave ? d.clave.trim() : "";
        setLista({ estado: "cargando" });
        // Lo guardado, por GET; otro destino o una clave nueva, por POST (la clave no va en la URL).
        if (!clave && !cambiaDestino(d)) pedir((signal) => leerModelosGuardados(signal));
        else pedir((signal) => consultarModelos({ proveedor: d.proveedor, url, ...(clave ? { clave } : {}) }, signal));
    }

    useEffect(() => {
        registrarCargar(cargar);
    });
    useEffect(() => {
        if (hayGuardado) pedir((signal) => leerModelosGuardados(signal));
        return () => control.current?.abort();
        // Solo al abrir la pantalla.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const error = form.formState.errors.modelo?.message;
    const botonCargar = (
        <Boton variante="enlace" tamano="sm" className="h-auto px-0" onClick={cargar} disabled={lista.estado === "cargando"}>
            {lista.estado === "error" ? "Volver a cargar los modelos" : lista.estado === "cargando" ? "Cargando modelos…" : "Cargar modelos"}
        </Boton>
    );
    const manual = aMano || lista.estado !== "listo" || lista.modelos.length === 0;
    if (manual) {
        return (
            <div className="grid gap-1" data-lista-modelos={lista.estado}>
                <Campo
                    etiqueta="Modelo"
                    autoComplete="off"
                    spellCheck={false}
                    placeholder={proveedor === "anthropic" ? "Vacío: el modelo por defecto" : "Nombre del modelo"}
                    error={error}
                    ayuda={
                        lista.estado === "listo" && lista.modelos.length === 0
                            ? "El proveedor no devolvió modelos: escríbelo a mano."
                            : lista.estado === "inicial"
                              ? "Escríbelo o carga la lista del proveedor."
                              : undefined
                    }
                    {...form.register("modelo")}
                />
                {lista.estado === "error" ? (
                    <p role="alert" className="flex items-start gap-1.5 text-sm text-error" data-slot="error-modelos">
                        <CircleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
                        <span className="min-w-0 break-words">No se pudo leer la lista de modelos: {lista.motivo}</span>
                    </p>
                ) : null}
                <div className="flex flex-wrap gap-x-3">
                    {botonCargar}
                    {aMano && lista.estado === "listo" && lista.modelos.length > 0 ? (
                        <Boton variante="enlace" tamano="sm" className="h-auto px-0" onClick={() => setAMano(false)}>
                            Elegir de la lista
                        </Boton>
                    ) : null}
                </div>
            </div>
        );
    }
    const modelos = lista.modelos;
    const conActual = modelo && !modelos.some((m) => m.id === modelo) ? [{ id: modelo, nombre: modelo }, ...modelos] : modelos;
    return (
        <div className="grid gap-1" data-lista-modelos="listo">
            <CampoSelector etiqueta="Modelo" error={error} placeholder="Elegir…" {...form.register("modelo")}>
                <option value="">Elegir…</option>
                {conActual.map((m) => (
                    <option key={m.id} value={m.id}>
                        {m.nombre}
                    </option>
                ))}
            </CampoSelector>
            <div className="flex flex-wrap gap-x-3">
                <Boton variante="enlace" tamano="sm" className="h-auto px-0" onClick={() => setAMano(true)}>
                    Escribir otro modelo
                </Boton>
                {botonCargar}
            </div>
        </div>
    );
}

/** «Probar conexión» con lo que está en pantalla (sin clave escrita, el servidor usa la guardada). */
function Prueba({ form, destinoCambiado }: { form: UseFormReturn<ValoresAjustes>; destinoCambiado: boolean }) {
    const [estado, setEstado] = useState<{ tipo: "nada" } | { tipo: "probando" } | { tipo: "hecho"; prueba: PruebaAsistente } | { tipo: "fallo"; error: unknown }>({ tipo: "nada" });
    async function probar() {
        const d = form.getValues();
        if (d.url.trim() && !urlValida(d.url.trim())) {
            await form.trigger("url");
            return;
        }
        const clave = d.cambiarClave ? d.clave.trim() : "";
        // Con otro destino la clave guardada no se manda: primero hay que escribirla (o quitarla).
        if (destinoCambiado && !clave && !d.quitarClave) {
            form.setError("clave", { message: TEXTO_CLAVE_REQUERIDA }, { shouldFocus: true });
            return;
        }
        setEstado({ tipo: "probando" });
        try {
            const p = await probarAsistente({ proveedor: d.proveedor, url: d.url.trim(), modelo: d.modelo.trim(), ...(clave ? { clave } : {}) });
            setEstado({ tipo: "hecho", prueba: p });
        } catch (e) {
            setEstado({ tipo: "fallo", error: e });
        }
    }
    const ok = estado.tipo === "hecho" && estado.prueba.estado === "ok";
    return (
        <div className="grid gap-2">
            <Boton variante="secundario" onClick={() => void probar()} disabled={estado.tipo === "probando"} className="w-full sm:w-auto sm:justify-self-start">
                <RefreshCw className={cn(estado.tipo === "probando" && "motion-safe:animate-spin")} />
                {estado.tipo === "probando" ? "Probando…" : "Probar conexión"}
            </Boton>
            {estado.tipo === "hecho" || estado.tipo === "fallo" ? (
                <p role="status" data-prueba={ok ? "ok" : "error"} className={cn("flex items-start gap-1.5 text-sm font-medium", ok ? "text-success" : "text-error")}>
                    {ok ? <CircleCheck aria-hidden className="mt-0.5 size-4 shrink-0" /> : <CircleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />}
                    <span className="min-w-0 break-words">
                        {estado.tipo === "hecho" ? (ok ? textoPrueba(estado.prueba) : `No respondió: ${textoPrueba(estado.prueba)}`) : `No se pudo probar: ${motivoModelos(estado.error)}`}
                    </span>
                </p>
            ) : null}
        </div>
    );
}

/** Uso del mes (`GET /asistente/uso`): si no se pudo leer, «Reintentar» sin cifras. */
function Uso() {
    const lectura = useLectura((signal) => leerUso(undefined, signal));
    if (lectura.estado === "cargando") return <p className="text-sm text-muted-foreground">Leyendo el uso del mes…</p>;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el uso del mes" />;
    return <CifrasUso uso={lectura.datos} />;
}

function CifrasUso({ uso }: { uso: UsoAsistente }) {
    const tokens = uso.tokensEntrada + uso.tokensSalida;
    const usadas = porcentajeUsadas(uso);
    const barra = Math.min(100, Math.max(0, Math.round(uso.porcentaje)));
    return (
        <div className="grid gap-3" data-slot="uso-asistente">
            <p className="text-sm text-muted-foreground first-letter:uppercase">{nombreMes(uso.mes)}</p>
            <div className="grid gap-3 sm:grid-cols-2">
                <div>
                    <p className="text-2xl font-semibold tabular-nums">{formatearTokens(tokens)}</p>
                    <p className="text-sm text-muted-foreground">de tokens (piezas de texto) de un tope de {formatearTokens(uso.topeTokensMes)}</p>
                </div>
                <div>
                    <p className="text-2xl font-semibold tabular-nums">{usadas === null ? "—" : `${usadas} %`}</p>
                    <p className="text-sm text-muted-foreground">
                        {usadas === null ? "Todavía no hay sugerencias este mes" : `de ${uso.sugerencias.toLocaleString("es-GT")} sugerencias se usaron`}
                    </p>
                </div>
            </div>
            <div
                role="progressbar"
                aria-label="Tokens usados del tope del mes"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={barra}
                aria-valuetext={`${barra} % del tope`}
                className="h-2 overflow-hidden rounded-full bg-background-secondary"
            >
                <div className={cn("h-full rounded-full", uso.pausado ? "bg-error" : barra >= 80 ? "bg-warning" : "bg-primary")} style={{ width: `${barra}%` }} />
            </div>
            <p className="text-sm text-muted-foreground">
                {uso.utiles} útiles · {uso.noUtiles} no útiles · {uso.resumenes} resúmenes
                {uso.costo !== null ? ` · ${uso.costo} con tus precios` : ""}
            </p>
            {uso.pausado ? (
                <Aviso tono="error" titulo="En pausa por el tope" data-testid="uso-pausado">
                    Se llegó al tope del mes y no sugiere ni resume. {textoPausado(uso.mes)}
                </Aviso>
            ) : null}
        </div>
    );
}
