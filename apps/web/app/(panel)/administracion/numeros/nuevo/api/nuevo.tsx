"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Campo, CampoSecreto, CampoSelector, Casilla, FormularioPagina, Interruptor, liberarProteccion, SeccionFormulario, avisar } from "@iris/ui";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { CargaRegistro } from "@/componentes/carga-registro";
import { ChecklistMeta, EnlaceGuia } from "@/componentes/numeros/checklist-meta";
import { TituloPaso } from "@/componentes/numeros/titulo-paso";
import { WebhookMeta } from "@/componentes/numeros/webhook-meta";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearNumero, nombreUso } from "@/lib/conversaciones/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { crearCanal, leerCanales, probarCanal } from "@/lib/numeros/api";
import { esquemaNuevoNumero, USOS, type EntradaNuevoNumero, type ValoresNuevoNumero } from "@/lib/numeros/esquemas";
import { dejarRecienCreado, olvidarRecienCreado } from "@/lib/numeros/recien-creado";
import type { Canal, PruebaCanal } from "@/lib/numeros/tipos";
import type { Schema } from "@iris/contratos";
import { useSesion } from "@/lib/sesion";

const CAMPOS = ["nombre", "numero", "phoneNumberId", "wabaId", "token", "appSecret", "usos", "avisosEmpresa", "colaId"] as const;

/**
 * `/administracion/numeros/nuevo`: los tres pasos en una página (en Meta, datos del número y el
 * webhook). «Guardar y probar» crea el número (Iris genera el verify token), prueba la conexión
 * con Meta y abre «Conectar», que muestra el resultado y el token una sola vez.
 */
export function NuevoNumero() {
    const { puede } = useSesion();
    // Las colas se leen con `conversaciones.ver`; sin él, el número queda sin cola (se elige luego en «Configurar»).
    const verColas = puede(["conversaciones.ver"]);
    const lectura = useLectura(async (signal) => {
        const [canales, colas] = await Promise.all([leerCanales(signal), verColas ? resolver(api().GET("/api/v1/mensajeria/colas", { signal })).then((r) => r.data ?? []) : Promise.resolve(null)]);
        return { canales, colas: colas ? colas.filter((c) => c.activa) : null };
    });
    return (
        <CargaRegistro lectura={lectura} que="los datos para agregar el número" noEncontrado="No se pudo abrir">
            {(d) => <Formulario {...d} />}
        </CargaRegistro>
    );
}

function Formulario({ canales, colas }: { canales: Canal[]; colas: Schema<"ColaConversaciones">[] | null }) {
    const router = useRouter();
    const { yo } = useSesion();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    // Lo que quedara de un alta anterior que no llegó a «Conectar» se olvida al abrir esta pantalla;
    // y, aunque nadie vuelva, vence solo al minuto (`VIDA_RECIEN_CREADO_MS`).
    useEffect(() => {
        olvidarRecienCreado();
    }, []);
    const form = useForm<EntradaNuevoNumero, unknown, ValoresNuevoNumero>({
        resolver: zodResolver(esquemaNuevoNumero),
        defaultValues: { nombre: "", numero: "", phoneNumberId: "", wabaId: "", token: "", appSecret: "", usos: ["soporte"], avisosEmpresa: false, colaId: colas?.[0] ? String(colas[0].id) : "" },
    });
    const usos = useWatch({ control: form.control, name: "usos" }) ?? [];
    const errores = form.formState.errors;
    const avisosHoy = canales.find((c) => c.activo && c.avisosEmpresa);
    const empresa = yo.empresaActiva;

    async function guardar(d: ValoresNuevoNumero) {
        setError(null);
        let canal: Canal;
        let verifyToken: string | null;
        try {
            ({ canal, verifyToken } = await crearCanal(
                {
                    empresaId: empresa?.id ?? null,
                    nombre: d.nombre,
                    numero: d.numero,
                    phoneNumberId: d.phoneNumberId,
                    wabaId: d.wabaId,
                    token: d.token,
                    appSecret: d.appSecret,
                    usos: d.usos,
                    // Sin marcar, el servidor decide (lo marca si la empresa no tiene número de avisos).
                    ...(d.avisosEmpresa ? { avisosEmpresa: true } : {}),
                    colaId: d.colaId ? Number(d.colaId) : null,
                },
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
        avisar({ titulo: `Número «${canal.nombre}» guardado` });
        let prueba: PruebaCanal | null = null;
        let errorPrueba: unknown = null;
        try {
            prueba = await probarCanal(canal.id, nuevaClaveIdempotencia());
        } catch (e) {
            errorPrueba = e;
        }
        // El token de verificación generado pasa a «Conectar» solo en memoria (nunca en la URL).
        dejarRecienCreado({ canal, verifyToken, prueba, errorPrueba });
        await liberarProteccion();
        router.replace(`/administracion/numeros/${canal.id}/conectar`);
    }

    const yaCreado = error instanceof ErrorApi && error.codigo === "idempotencia_no_repetible";
    return (
        <FormularioPagina
            titulo="Agregar número"
            descripcion={
                <>
                    Conecta un número de WhatsApp Cloud API en tres pasos. <EnlaceGuia>Ver la guía</EnlaceGuia>
                </>
            }
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/numeros/nuevo"
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: "Guardar y probar", textoEnviando: "Guardando y probando…", enviando: form.formState.isSubmitting, nota: "Al guardar, Iris prueba la conexión con Meta." }}
            error={
                yaCreado ? (
                    <Aviso tono="warning" titulo="El número ya quedó guardado">
                        Se guardó, pero la respuesta no llegó a esta pantalla. Ábrelo desde la{" "}
                        <Link href="/administracion/numeros" className="font-medium text-primary underline-offset-2 hover:underline">
                            lista de números
                        </Link>{" "}
                        y genera otro token de verificación en «Credenciales».
                    </Aviso>
                ) : error ? (
                    <Aviso tono="error" titulo="No se pudo guardar el número">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo={<TituloPaso numero={1} titulo="En Meta" />} descripcion="Prepara esto en Meta antes de seguir. Lo marcas solo para ti; no se guarda.">
                <ChecklistMeta />
            </SeccionFormulario>
            <SeccionFormulario titulo={<TituloPaso numero={2} titulo="Datos del número" />} descripcion={empresa ? `Se agrega a ${empresa.nombre}, la empresa activa.` : undefined}>
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" ayuda="Así se ve el número en la bandeja." maxLength={120} error={errores.nombre?.message} {...form.register("nombre")} />
                    <Campo
                        etiqueta="Número"
                        ayuda="Con código de país y sin espacios (E.164)."
                        inputMode="tel"
                        autoComplete="off"
                        placeholder="+50222000040"
                        className="[&_input]:font-mono"
                        error={errores.numero?.message}
                        {...form.register("numero")}
                    />
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
                <CampoSecreto etiqueta="Token de acceso" ayuda="Del usuario del sistema, sin vencimiento. Iris lo guarda cifrado y no lo vuelve a mostrar." error={errores.token?.message} {...form.register("token")} />
                <CampoSecreto etiqueta="App secret" ayuda="Iris lo guarda cifrado y no lo vuelve a mostrar." error={errores.appSecret?.message} {...form.register("appSecret")} />
                <fieldset className="grid gap-2">
                    <legend className="mb-1 text-sm font-medium">Usos</legend>
                    <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                        {USOS.map((u) => (
                            <Casilla
                                key={u}
                                etiqueta={nombreUso(u)}
                                checked={usos.includes(u)}
                                onChange={(e) =>
                                    form.setValue("usos", e.target.checked ? [...usos, u] : usos.filter((x) => x !== u), { shouldDirty: true, shouldValidate: form.formState.isSubmitted })
                                }
                            />
                        ))}
                    </div>
                    <p className="text-sm text-muted-foreground">Da color al número en la bandeja y sirve para filtrar; no limita lo que se puede contestar.</p>
                    {errores.usos?.message ? (
                        <p role="alert" className="text-sm text-error">
                            {errores.usos.message}
                        </p>
                    ) : null}
                </fieldset>
                <Interruptor
                    etiqueta="Número de avisos de la empresa"
                    ayuda={`Los avisos de Argos salen por aquí. Pide el uso «Avisos».${avisosHoy ? ` Hoy salen por ${avisosHoy.nombre} (${formatearNumero(avisosHoy.numero)}); marcarlo lo quita de ese.` : ""}`}
                    error={errores.avisosEmpresa?.message}
                    {...form.register("avisosEmpresa")}
                />
                {colas ? (
                    <CampoSelector etiqueta="Cola de las conversaciones nuevas" error={errores.colaId?.message} {...form.register("colaId")}>
                        <option value="">Sin cola</option>
                        {colas.map((c) => (
                            <option key={c.id} value={String(c.id)}>
                                {c.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                ) : null}
            </SeccionFormulario>
            <SeccionFormulario titulo={<TituloPaso numero={3} titulo="Conectar el webhook" />} descripcion="Así Meta le entrega a Iris los mensajes de este número.">
                <WebhookMeta url={null} verifyToken={null} />
            </SeccionFormulario>
        </FormularioPagina>
    );
}
