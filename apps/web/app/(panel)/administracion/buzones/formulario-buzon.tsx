"use client";

import {
    Aviso,
    Boton,
    Campo,
    CampoSelector,
    Casilla,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
} from "@iris/ui";
import { KeyRound, RotateCw, ShieldAlert } from "lucide-react";
import { useState, type ChangeEvent, type ReactNode } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useCategorias } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import {
    aEntrada,
    campoDeError,
    exigeContrasenaImap,
    exigeContrasenaSmtp,
    PRIORIDADES_BUZON,
    PUERTOS,
    resolverBuzon,
    valoresIniciales,
    type Buzon,
    type Tls,
    type ValoresBuzon,
} from "@/lib/buzones/esquemas";
import { NOMBRES_TLS } from "@/lib/buzones/presentacion";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { PRIORIDADES } from "@/lib/tickets/presentacion";
import { esConflicto, textoErrorBuzon } from "./acciones-buzon";

type Props = {
    /** Buzón que se edita; sin él, alta. */
    buzon?: Buzon | undefined;
    alCerrar: () => void;
    alGuardar: (b: Buzon) => void;
};

function Seccion({ titulo, children, descripcion }: { titulo: string; descripcion?: ReactNode; children: ReactNode }) {
    return (
        <fieldset className="grid gap-3 border-t pt-4 first:border-t-0 first:pt-0">
            <legend className="float-left mb-1 w-full text-sm font-semibold">{titulo}</legend>
            {descripcion ? <p className="-mt-1 text-sm text-muted-foreground">{descripcion}</p> : null}
            {children}
        </fieldset>
    );
}

const AYUDA_AUTHSERV = (
    <>
        Nombre que tu servidor de correo pone en la cabecera <span className="font-mono">Authentication-Results</span> (p. ej.{" "}
        <span className="font-mono">mx.isp.gt</span>). Con él Iris comprueba DMARC/DKIM y sabe si un correo viene de verdad del cliente.
    </>
);

/** Alta o edición de un buzón en un diálogo. Las contraseñas nunca se muestran: solo se escriben. */
export function FormularioBuzon({ buzon, alCerrar, alGuardar }: Props) {
    const [antes, setAntes] = useState<Buzon | undefined>(buzon);
    const editando = antes !== undefined;
    const categorias = useCategorias();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    // El esquema depende del buzón leído (cambia al recargar tras un 409); useForm toma las opciones de cada render.
    const form = useForm<ValoresBuzon>({ resolver: resolverBuzon(antes), defaultValues: valoresIniciales(buzon) });
    const { errors, isSubmitting } = form.formState;
    const v = useWatch({ control: form.control });
    const conSmtp = v.conSmtp ?? false;
    const pideImap =
        exigeContrasenaImap({ imapHost: v.imapHost ?? "", imapPuerto: v.imapPuerto ?? "", imapTls: v.imapTls ?? "tls", imapUsuario: v.imapUsuario ?? "" }, antes);
    const pideSmtp = exigeContrasenaSmtp(
        { conSmtp, smtpHost: v.smtpHost ?? "", smtpPuerto: v.smtpPuerto ?? "", smtpTls: v.smtpTls ?? "starttls", smtpUsuario: v.smtpUsuario ?? "" },
        antes,
    );
    const sinAuthserv = (v.authservId ?? "").trim() === "";

    async function guardar(valores: ValoresBuzon) {
        setError(null);
        const entrada = aEntrada(valores);
        try {
            const b = antes
                ? await resolver(
                      api().POST("/api/v1/buzones/{id}/editar", {
                          params: { path: { id: antes.id }, header: { "Idempotency-Key": clave.actual() } },
                          body: { ...entrada, version: antes.version },
                      }),
                  )
                : await resolver(api().POST("/api/v1/buzones", { params: { header: { "Idempotency-Key": clave.actual() } }, body: entrada }));
            clave.renovar();
            alGuardar(b);
        } catch (e) {
            clave.trasError(e);
            let alguno = false;
            if (e instanceof ErrorApi && e.estado === 422) {
                for (const err of e.errores) {
                    const campo = campoDeError(err.field);
                    if (campo) {
                        form.setError(campo, { type: "servidor", message: err.message });
                        alguno = true;
                    }
                }
            }
            if (!alguno) {
                setError(e);
            }
        }
    }

    async function recargar() {
        if (!antes) return;
        setRecargando(true);
        try {
            const b = await resolver(api().GET("/api/v1/buzones/{id}", { params: { path: { id: antes.id } } }));
            setAntes(b);
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    /** Al cambiar el modo TLS, si el puerto era el habitual del otro modo, pasa al habitual del nuevo. */
    function alCambiarTls(servidor: "imap" | "smtp", tls: Tls) {
        const campo = servidor === "imap" ? "imapPuerto" : "smtpPuerto";
        const otro: Tls = tls === "tls" ? "starttls" : "tls";
        if (form.getValues(campo) === String(PUERTOS[servidor][otro])) {
            form.setValue(campo, String(PUERTOS[servidor][tls]), { shouldDirty: true });
        }
    }

    const registroTls = (servidor: "imap" | "smtp") => {
        const r = form.register(servidor === "imap" ? "imapTls" : "smtpTls");
        return {
            ...r,
            onChange: async (e: ChangeEvent<HTMLSelectElement>) => {
                await r.onChange(e);
                alCambiarTls(servidor, e.target.value as Tls);
            },
        };
    };

    const ayudaContrasena = (pide: boolean, servidor: string) =>
        editando
            ? pide
                ? `Cambiaste el servidor, el puerto, el TLS o el usuario: escribe la contraseña ${servidor} de nuevo.`
                : "Déjala vacía para conservar la guardada. Nunca se muestra."
            : "Se guarda cifrada y nunca se muestra.";

    const listaCategorias = categorias.estado === "listo" ? categorias.datos : [];
    const categoriaActual = v.categoriaId ?? "";

    return (
        <DialogoProtegido sucio={form.formState.isDirty} open onOpenChange={(a) => !a && !isSubmitting && alCerrar()}>
            <DialogoContenido
                titulo={editando ? `Editar ${antes.nombre}` : "Nuevo buzón"}
                descripcion={editando ? antes.correo : "Nace inactivo: pruébalo y actívalo cuando esté listo."}
                className="sm:w-[min(44rem,calc(100vw-2rem))]"
            >
                <form noValidate className="grid gap-5" onSubmit={form.handleSubmit(guardar)}>
                    <Seccion titulo="Buzón">
                        <div className="grid items-start gap-3 sm:grid-cols-2">
                            <Campo etiqueta="Nombre" autoComplete="off" maxLength={120} error={errors.nombre?.message} {...form.register("nombre")} />
                            <Campo
                                etiqueta="Dirección"
                                type="email"
                                autoComplete="off"
                                ayuda="Remitente de las respuestas."
                                error={errors.correo?.message}
                                {...form.register("correo")}
                            />
                        </div>
                    </Seccion>

                    <Seccion titulo="Entrada (IMAP)" descripcion="Solo conexiones cifradas: TLS directo o STARTTLS obligatorio, con certificado verificado.">
                        <div className="grid items-start gap-3 sm:grid-cols-[minmax(0,1fr)_7rem_9rem]">
                            <Campo etiqueta="Servidor IMAP" autoComplete="off" spellCheck={false} placeholder="imap.isp.gt" error={errors.imapHost?.message} {...form.register("imapHost")} />
                            <Campo etiqueta="Puerto" inputMode="numeric" autoComplete="off" error={errors.imapPuerto?.message} {...form.register("imapPuerto")} />
                            <CampoSelector etiqueta="Cifrado" error={errors.imapTls?.message} {...registroTls("imap")}>
                                <option value="tls">{NOMBRES_TLS.tls} (993)</option>
                                <option value="starttls">{NOMBRES_TLS.starttls} (143)</option>
                            </CampoSelector>
                        </div>
                        <div className="grid items-start gap-3 sm:grid-cols-2">
                            <Campo etiqueta="Usuario IMAP" autoComplete="off" spellCheck={false} error={errors.imapUsuario?.message} {...form.register("imapUsuario")} />
                            <Campo
                                etiqueta="Contraseña IMAP"
                                type="password"
                                autoComplete="new-password"
                                maxLength={512}
                                placeholder={editando && !pideImap ? "•••••• (se conserva)" : ""}
                                aria-required={pideImap}
                                ayuda={errors.imapContrasena ? undefined : ayudaContrasena(pideImap, "IMAP")}
                                error={errors.imapContrasena?.message}
                                {...form.register("imapContrasena")}
                            />
                            <Campo etiqueta="Carpeta" autoComplete="off" placeholder="INBOX" error={errors.imapCarpeta?.message} {...form.register("imapCarpeta")} />
                            <Campo
                                etiqueta="Carpeta de procesados (opcional)"
                                autoComplete="off"
                                ayuda="Si la indicas, cada correo ya guardado se mueve ahí; si no, solo se marca como leído."
                                error={errors.imapCarpetaProcesados?.message}
                                {...form.register("imapCarpetaProcesados")}
                            />
                        </div>
                    </Seccion>

                    <Seccion titulo="Salida (SMTP)">
                        <Casilla
                            etiqueta="Enviar las respuestas por correo"
                            ayuda="Sin SMTP, las respuestas de los agentes quedan en el ticket y no salen por correo."
                            {...form.register("conSmtp")}
                        />
                        {conSmtp ? (
                            <>
                                <div className="grid items-start gap-3 sm:grid-cols-[minmax(0,1fr)_7rem_9rem]">
                                    <Campo etiqueta="Servidor SMTP" autoComplete="off" spellCheck={false} placeholder="smtp.isp.gt" error={errors.smtpHost?.message} {...form.register("smtpHost")} />
                                    <Campo etiqueta="Puerto SMTP" inputMode="numeric" autoComplete="off" error={errors.smtpPuerto?.message} {...form.register("smtpPuerto")} />
                                    <CampoSelector etiqueta="Cifrado SMTP" error={errors.smtpTls?.message} {...registroTls("smtp")}>
                                        <option value="starttls">{NOMBRES_TLS.starttls} (587)</option>
                                        <option value="tls">{NOMBRES_TLS.tls} (465)</option>
                                    </CampoSelector>
                                </div>
                                <div className="grid items-start gap-3 sm:grid-cols-2">
                                    <Campo
                                        etiqueta="Usuario SMTP"
                                        autoComplete="off"
                                        spellCheck={false}
                                        ayuda="Vacío = el servidor no pide autenticación."
                                        error={errors.smtpUsuario?.message}
                                        {...form.register("smtpUsuario")}
                                    />
                                    <Campo
                                        etiqueta="Contraseña SMTP"
                                        type="password"
                                        autoComplete="new-password"
                                        maxLength={512}
                                        placeholder={editando && !pideSmtp && (v.smtpUsuario ?? "").trim() !== "" ? "•••••• (se conserva)" : ""}
                                        aria-required={pideSmtp}
                                        ayuda={errors.smtpContrasena ? undefined : (v.smtpUsuario ?? "").trim() === "" ? "Sin usuario no se usa contraseña." : ayudaContrasena(pideSmtp, "SMTP")}
                                        error={errors.smtpContrasena?.message}
                                        {...form.register("smtpContrasena")}
                                    />
                                </div>
                            </>
                        ) : null}
                    </Seccion>

                    <Seccion titulo="Verificación del remitente">
                        <Campo
                            etiqueta="authserv-id del servidor de correo"
                            autoComplete="off"
                            spellCheck={false}
                            placeholder="mx.isp.gt"
                            ayuda={AYUDA_AUTHSERV}
                            error={errors.authservId?.message}
                            {...form.register("authservId")}
                        />
                        {sinAuthserv ? (
                            <Aviso tono="warning" titulo="Sin authserv-id no se verifica a nadie" icono={ShieldAlert}>
                                Ningún remitente queda verificado: las respuestas de los clientes por correo entran como <strong>notas internas</strong>{" "}
                                (no reabren ni reanudan el ticket) y los tickets nuevos quedan con el contacto «sin verificar».
                            </Aviso>
                        ) : null}
                    </Seccion>

                    <Seccion titulo="Tickets nuevos" descripcion="Categoría y prioridad de los tickets que abre un correo nuevo.">
                        <div className="grid items-start gap-3 sm:grid-cols-2">
                            <CampoSelector etiqueta="Categoría por defecto" error={errors.categoriaId?.message} {...form.register("categoriaId")}>
                                <option value="">Sin categoría</option>
                                {listaCategorias.map((c) => (
                                    <option key={c.id} value={String(c.id)}>
                                        {c.nombre}
                                    </option>
                                ))}
                                {categoriaActual !== "" && !listaCategorias.some((c) => String(c.id) === categoriaActual) ? (
                                    <option value={categoriaActual}>Categoría {categoriaActual}</option>
                                ) : null}
                            </CampoSelector>
                            <CampoSelector etiqueta="Prioridad por defecto" error={errors.prioridad?.message} {...form.register("prioridad")}>
                                {PRIORIDADES_BUZON.map((p) => (
                                    <option key={p} value={p}>
                                        {PRIORIDADES[p].texto}
                                    </option>
                                ))}
                            </CampoSelector>
                        </div>
                    </Seccion>

                    {error ? (
                        esConflicto(error) ? (
                            <Aviso
                                tono="warning"
                                titulo="Alguien más cambió este buzón"
                                accion={
                                    <Boton variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                                        <RotateCw />
                                        {recargando ? "Recargando…" : "Recargar"}
                                    </Boton>
                                }
                            >
                                Recarga para tomar la versión nueva; lo que escribiste se conserva.
                            </Aviso>
                        ) : (
                            <Aviso tono="error" titulo={textoErrorBuzon(error).titulo}>
                                {textoErrorBuzon(error).detalle}
                            </Aviso>
                        )
                    ) : null}
                    {editando && (pideImap || pideSmtp) ? (
                        <p className="flex items-start gap-2 text-sm text-muted-foreground">
                            <KeyRound aria-hidden className="mt-0.5 size-4 shrink-0" />
                            La contraseña guardada no viaja a otro servidor ni a otro usuario: por eso hay que escribirla de nuevo.
                        </p>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <Boton type="submit" disabled={isSubmitting || esConflicto(error)}>
                            {isSubmitting ? "Guardando…" : editando ? "Guardar cambios" : "Crear buzón"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
