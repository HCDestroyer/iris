"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoArea, CampoFecha, CampoSelector, cn, FormularioPagina, liberarProteccion, ListaEleccion, SeccionFormulario } from "@iris/ui";
import { FileText, Landmark, RotateCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { LineaServicio, SelectorServicio } from "@/componentes/clientes/linea-servicio";
import { AvisoAccion, useEnvio } from "@/componentes/comercial/comun";
import { DesdeConversacion } from "@/componentes/conversaciones/desde-conversacion";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { leerMensajes } from "@/lib/conversaciones/api";
import { aliasCuenta, titularCuenta, tituloCuenta, type CuentaBancaria } from "@/lib/conversaciones/cuentas";
import { hora, tamanoLegible } from "@/lib/conversaciones/presentacion";
import type { ConversacionDetalle } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";
import { rutaDescargaAdjunto } from "@/lib/ordenes/adjuntos";
import { hoyEnGuatemala } from "@iris/ui";

const METODOS = [
    ["deposito", "Depósito"],
    ["transferencia", "Transferencia"],
    ["ach", "ACH"],
    ["remesa", "Remesa"],
] as const;

/** Espejo de `EntradaBoleta` del servidor (§6.4). */
export const esquemaBoleta = z
    .object({
        adjuntoId: z.string().min(1, "Elige la foto o el PDF de la boleta"),
        servicioId: z.string(),
        metodo: z.enum(["deposito", "transferencia", "ach", "remesa"]),
        cuentaBancariaId: z.string().min(1, "Elige la cuenta a la que depositó"),
        bancoOrigenId: z.string(),
        referencia: z.string().trim().min(1, "Escribe la referencia").max(60, "Hasta 60 caracteres"),
        monto: z
            .string()
            .trim()
            .regex(/^\d{1,10}(\.\d{1,2})?$/, "Monto en quetzales, p. ej. 250.00"),
        fechaDeposito: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Elige la fecha"),
        nota: z.string().max(500, "Hasta 500 caracteres"),
    })
    .superRefine((v, ctx) => {
        if ((v.metodo === "transferencia" || v.metodo === "ach") && !v.bancoOrigenId) ctx.addIssue({ code: "custom", path: ["bancoOrigenId"], message: "Elige el banco de origen" });
        if (v.fechaDeposito > hoyEnGuatemala()) ctx.addIssue({ code: "custom", path: ["fechaDeposito"], message: "No puede ser una fecha futura" });
    });
type Valores = z.infer<typeof esquemaBoleta>;

/** Una cuenta en la lista: banco y tipo, alias, número y titular (§7.8). */
function OpcionCuenta({ cuenta }: { cuenta: CuentaBancaria }) {
    const alias = aliasCuenta(cuenta);
    return (
        <span className="grid min-w-0 gap-0.5" data-cuenta={cuenta.id}>
            <span className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-sm font-semibold">
                <Landmark aria-hidden className="size-4 shrink-0 self-center text-muted-foreground" />
                {tituloCuenta(cuenta)}
                {alias ? <span className="text-xs font-normal text-muted-foreground">{alias}</span> : null}
            </span>
            <span className="font-mono text-sm tabular-nums [overflow-wrap:anywhere]">{cuenta.numero}</span>
            <span className={cn("text-xs", cuenta.titular ? "text-muted-foreground" : "text-warning")}>{titularCuenta(cuenta)}</span>
        </span>
    );
}

/** «250» → «250.00» (el servidor pide 2 decimales). */
export const montoConDecimales = (m: string) => (m.includes(".") ? m.padEnd(m.indexOf(".") + 3, "0") : `${m}.00`);

/** Boleta de pago que mandó el cliente (más de 3 campos: pantalla propia). Iris la manda a Argos en un job. */
export default function Pagina() {
    return <DesdeConversacion permisos={["conversaciones.registrar_boleta"]}>{(c) => <FormularioBoleta conv={c} />}</DesdeConversacion>;
}

function FormularioBoleta({ conv }: { conv: ConversacionDetalle }) {
    const router = useRouter();
    const envio = useEnvio();
    const volverA = `/conversaciones?c=${conv.id}`;
    const datos = useLectura(async (signal) => {
        const [mensajes, cuentas, bancos] = await Promise.all([
            leerMensajes(conv.id, undefined, signal),
            // Las cuentas de la empresa de esta conversación: la misma contra la que se valida la boleta.
            resolver(api().GET("/api/v1/mensajeria/conversaciones/cuentas-bancarias", { params: { query: { conversacionId: conv.id } }, signal })),
            resolver(api().GET("/api/v1/mensajeria/conversaciones/bancos", { signal })),
        ]);
        const adjuntos = (mensajes.data ?? [])
            .filter((m) => m.adjunto && m.adjunto.estado === "completo" && (m.adjunto.mime.startsWith("image/") || m.adjunto.mime === "application/pdf"))
            .reverse();
        return { adjuntos, cuentas: cuentas.data ?? [], bancos: bancos.data ?? [] };
    });
    const servicios = conv.clienteResumen?.servicios ?? [];
    const form = useForm<Valores>({
        resolver: zodResolver(esquemaBoleta),
        defaultValues: { adjuntoId: "", servicioId: String(conv.contacto.servicioId ?? ""), metodo: "deposito", cuentaBancariaId: "", bancoOrigenId: "", referencia: "", monto: "", fechaDeposito: "", nota: "" },
    });
    const v = useWatch({ control: form.control });
    const errores = form.formState.errors;

    async function guardar(d: Valores) {
        const b = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/mensajeria/conversaciones/{id}/boletas", {
                        params: { path: { id: conv.id }, header: { "Idempotency-Key": clave } },
                        body: {
                            adjuntoId: d.adjuntoId,
                            metodo: d.metodo,
                            cuentaBancariaId: Number(d.cuentaBancariaId),
                            referencia: d.referencia.trim(),
                            monto: montoConDecimales(d.monto.trim()),
                            fechaDeposito: d.fechaDeposito,
                            ...(d.servicioId ? { servicioId: Number(d.servicioId) } : {}),
                            ...(d.bancoOrigenId ? { bancoOrigenId: Number(d.bancoOrigenId) } : {}),
                            ...(d.nota.trim() ? { nota: d.nota.trim() } : {}),
                        },
                    }),
                ),
            { nombres: ["adjuntoId", "servicioId", "metodo", "cuentaBancariaId", "bancoOrigenId", "referencia", "monto", "fechaDeposito", "nota"], setError: form.setError },
        );
        if (b) {
            // Si ya quedó fallida, se dice por qué (queda en la ficha con su detalle).
            if (b.estado === "fallida") avisar({ tono: "error", titulo: `La boleta ${b.referencia} no se pudo registrar en Argos`, ...(b.detalle ? { descripcion: b.detalle } : {}) });
            else avisar({ titulo: `Boleta ${b.referencia} enviada a Argos` });
            await liberarProteccion();
            router.replace(volverA);
        }
    }

    if (conv.contacto.tipo !== "cliente") {
        return <Aviso tono="warning" titulo="Asocia primero el cliente de Argos">La boleta se registra en la cuenta del cliente; esta conversación aún no tiene uno.</Aviso>;
    }
    if (datos.estado === "error") {
        return (
            <Aviso
                tono="error"
                titulo="No se pudieron leer las cuentas de Argos o los adjuntos"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={datos.reintentar}>
                        <RotateCw />
                        Reintentar
                    </Boton>
                }
            >
                {mensajeDeError(datos.error).titulo}
            </Aviso>
        );
    }
    const listo = datos.estado === "listo" ? datos.datos : null;
    const pideBanco = v.metodo === "transferencia" || v.metodo === "ach";
    return (
        <FormularioPagina
            titulo="Boleta de pago"
            descripcion={`De ${conv.titulo}. Iris la manda a Argos; caja la aprueba o la rechaza allá.`}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={volverA}
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: "Enviar a Argos", textoEnviando: "Enviando…", enviando: form.formState.isSubmitting }}
            error={<AvisoAccion envio={envio} que="registrar la boleta" recargar={async () => undefined} />}
        >
            <SeccionFormulario titulo="1. La boleta" descripcion="La foto o el PDF que mandó el cliente en esta conversación.">
                {listo === null ? <p className="text-sm text-muted-foreground">Cargando…</p> : null}
                {listo && listo.adjuntos.length === 0 ? <Aviso tono="warning" titulo="El cliente aún no mandó una foto o un PDF en esta conversación" /> : null}
                <fieldset className="grid gap-2 sm:grid-cols-2" aria-describedby={errores.adjuntoId ? "error-adjunto" : undefined}>
                    <legend className="sr-only">Adjunto</legend>
                    {(listo?.adjuntos ?? []).map((m) => {
                        const a = m.adjunto;
                        if (!a) return null;
                        const ruta = rutaDescargaAdjunto(a.id);
                        const elegido = v.adjuntoId === a.id;
                        return (
                            <Boton
                                key={a.id}
                                variante="secundario"
                                aria-pressed={elegido}
                                className={cn("h-auto justify-start gap-3 p-2 text-left", elegido && "ring-2 ring-primary")}
                                onClick={() => form.setValue("adjuntoId", a.id, { shouldDirty: true, shouldValidate: form.formState.isSubmitted })}
                            >
                                {a.mime.startsWith("image/") && ruta ? (
                                    // eslint-disable-next-line @next/next/no-img-element
                                    <img src={ruta} alt="" className="size-14 shrink-0 rounded-md object-cover" />
                                ) : (
                                    <FileText aria-hidden className="size-8 shrink-0 text-muted-foreground" />
                                )}
                                <span className="grid min-w-0">
                                    <span className="truncate text-sm font-medium">{a.mime === "application/pdf" ? "PDF" : "Foto"} de las {hora(m.creadoEn)}</span>
                                    <span className="truncate text-xs text-muted-foreground">{tamanoLegible(a.tamano)}</span>
                                </span>
                            </Boton>
                        );
                    })}
                </fieldset>
                {errores.adjuntoId?.message ? (
                    <p id="error-adjunto" role="alert" className="text-sm text-error">
                        {errores.adjuntoId.message}
                    </p>
                ) : null}
            </SeccionFormulario>
            <SeccionFormulario titulo="2. La cuenta" descripcion="La cuenta de la empresa a la que depositó el cliente, tal como está en Argos.">
                {listo === null ? <p className="text-sm text-muted-foreground">Cargando…</p> : null}
                {listo && listo.cuentas.length === 0 ? (
                    <Aviso tono="warning" titulo="La empresa no tiene cuentas activas en quetzales en Argos">
                        Las cuentas se crean y se activan en Argos. Si el cliente depositó en una cuenta desactivada, caja registra el pago a mano en Argos.
                    </Aviso>
                ) : null}
                {listo && listo.cuentas.length > 0 ? (
                    <ListaEleccion
                        leyenda="Cuenta de la empresa"
                        nombre="cuentaBancariaId"
                        valor={v.cuentaBancariaId || undefined}
                        alCambiar={(id) => form.setValue("cuentaBancariaId", id, { shouldDirty: true, shouldValidate: form.formState.isSubmitted })}
                        error={errores.cuentaBancariaId?.message}
                        opciones={listo.cuentas.map((c) => ({
                            valor: String(c.id),
                            texto: `${tituloCuenta(c)}, ${c.numero}`,
                            contenido: <OpcionCuenta cuenta={c} />,
                        }))}
                    />
                ) : null}
            </SeccionFormulario>
            <SeccionFormulario titulo="3. El depósito">
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Método" error={errores.metodo?.message} {...form.register("metodo")}>
                        {METODOS.map(([valor, texto]) => (
                            <option key={valor} value={valor}>
                                {texto}
                            </option>
                        ))}
                    </CampoSelector>
                    {pideBanco ? (
                        <CampoSelector etiqueta="Banco de origen" error={errores.bancoOrigenId?.message} disabled={!listo} {...form.register("bancoOrigenId")}>
                            <option value="">Elegir…</option>
                            {(listo?.bancos ?? []).map((b) => (
                                <option key={b.id} value={String(b.id)}>
                                    {b.nombre}
                                </option>
                            ))}
                        </CampoSelector>
                    ) : null}
                    <Campo etiqueta="Referencia" maxLength={60} autoComplete="off" error={errores.referencia?.message} {...form.register("referencia")} />
                    <Campo etiqueta="Monto (Q)" inputMode="decimal" placeholder="250.00" maxLength={14} autoComplete="off" error={errores.monto?.message} {...form.register("monto")} />
                    <CampoFecha etiqueta="Fecha del depósito" error={errores.fechaDeposito?.message} {...form.register("fechaDeposito")} />
                </div>
                {servicios.length > 1 ? (
                    <SelectorServicio
                        leyenda="¿De cuál servicio es el pago?"
                        servicios={servicios}
                        valor={v.servicioId ? Number(v.servicioId) : undefined}
                        alCambiar={(id) => form.setValue("servicioId", String(id), { shouldDirty: true, shouldValidate: true })}
                        error={errores.servicioId?.message}
                        ayuda="Sin elegir, queda el servicio de la conversación."
                    />
                ) : servicios[0] ? (
                    <div className="grid gap-1" data-testid="servicio-boleta">
                        <span className="text-sm font-medium">Servicio</span>
                        <div className="rounded-lg border border-border px-3 py-2.5">
                            <LineaServicio servicio={servicios[0]} />
                        </div>
                    </div>
                ) : null}
                <CampoArea etiqueta="Nota (opcional)" rows={2} maxLength={500} error={errores.nota?.message} {...form.register("nota")} />
            </SeccionFormulario>
        </FormularioPagina>
    );
}
