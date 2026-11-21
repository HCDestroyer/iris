"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Boton, Campo, CampoNumero, Casilla, Esqueleto, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { RefreshCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { leerCentralita } from "@/lib/centralita/api";
import { huellaLegible } from "@/lib/centralita/presentacion";
import type { Centralita } from "@/lib/centralita/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { esquemaTelefono, textoPruebaTelefono, valoresTelefono, type ValoresTelefono } from "@/lib/telefono/administracion";
import { activarTelefono, guardarTelefonoCentralita, probarTelefonoCentralita } from "@/lib/telefono/api";
import type { PruebaTelefono } from "@/lib/telefono/tipos";

const VOLVER = "/administracion/integraciones/centralita";
const CAMPOS = ["wssPuerto", "wssRuta", "sipDominio", "prefijoSalida", "llamadasPorUsuarioDia", "llamadasSimultaneas"] as const;

/**
 * `/administracion/integraciones/centralita/telefono` (`centralita.configurar`): el teléfono de
 * Iris (E4c, contratos/voz.md §3.12; runbook centralita-issabel §9). Iris llega a la Issabel por WSS
 * (8089) con el certificado fijado por su huella; el administrador prueba, compara la huella, pone
 * el prefijo y los límites y confirma que la Issabel tiene las restricciones de la guía (§9.3.1).
 * Al guardar, Iris prueba con lo guardado y lo enciende.
 */
export function TelefonoCentralita() {
    const lectura = useLectura((signal) => leerCentralita(signal));
    if (lectura.estado === "cargando") return <Esqueleto className="h-64" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="la centralita" />;
    if (!lectura.datos) {
        return (
            <Aviso tono="warning" titulo="Primero conecta la centralita">
                El teléfono de Iris usa la misma Issabel que la conexión de solo lectura. Conéctala en Administración › Integraciones › Centralita.
            </Aviso>
        );
    }
    return <Formulario centralita={lectura.datos} />;
}

const clavePrueba = (v: Pick<ValoresTelefono, "wssPuerto" | "wssRuta">) => `${v.wssPuerto}|${v.wssRuta.trim()}`;

function Formulario({ centralita: c }: { centralita: Centralita }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const t = c.telefono;
    const form = useForm<ValoresTelefono>({ resolver: zodResolver(esquemaTelefono), defaultValues: valoresTelefono(t) });
    const valores = useWatch({ control: form.control });
    const [prueba, setPrueba] = useState<{ clave: string; resultado: PruebaTelefono } | null>(null);
    const [huellaOk, setHuellaOk] = useState(false);
    const [probando, setProbando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const errores = form.formState.errors;
    const v = { wssPuerto: valores.wssPuerto ?? "8089", wssRuta: valores.wssRuta ?? "/ws" };
    const vigente = prueba !== null && prueba.clave === clavePrueba(v) ? prueba.resultado : null;
    // Sin cambiar el WSS se guarda con la huella confirmada antes; si cambió, hay que probar y confirmar.
    const mismoWss = !!t?.huellaWss && Number(v.wssPuerto) === t.wssPuerto && v.wssRuta.trim() === t.wssRuta;
    const huella = vigente?.estado === "ok" && huellaOk ? vigente.huella : mismoWss ? (t?.huellaWss ?? null) : null;
    const confirmo = valores.confirmoRestricciones ?? false;
    const faltan = [...(huella ? [] : vigente?.estado === "ok" ? ["confirmar la huella"] : ["probar el WSS"]), ...(confirmo ? [] : ["confirmar la guía de la Issabel"])];

    async function probar() {
        if (!(await form.trigger(["wssPuerto", "wssRuta"]))) return;
        setProbando(true);
        setError(null);
        setHuellaOk(false);
        try {
            const r = await probarTelefonoCentralita(c.id, { wssPuerto: Number(v.wssPuerto), wssRuta: v.wssRuta.trim() });
            setPrueba({ clave: clavePrueba(v), resultado: r });
        } catch (e) {
            if (!erroresACampos(e, ["wssPuerto", "wssRuta"], form.setError)) setError(e);
        } finally {
            setProbando(false);
        }
    }

    return (
        <FormularioPagina
            titulo="Teléfono en Iris"
            descripcion="Contestar y llamar desde el panel con la extensión de cada persona. Iris hace de puente: la Issabel no se abre a internet."
            alEnviar={form.handleSubmit(async (d) => {
                if (!huella) return;
                setError(null);
                try {
                    const guardada = await guardarTelefonoCentralita(
                        c.id,
                        {
                            version: c.version,
                            wssPuerto: Number(d.wssPuerto),
                            wssRuta: d.wssRuta.trim(),
                            huellaWss: huella,
                            ...(d.sipDominio.trim() ? { sipDominio: d.sipDominio.trim().toLowerCase() } : {}),
                            prefijoSalida: d.prefijoSalida.trim(),
                            llamadasPorUsuarioDia: Number(d.llamadasPorUsuarioDia),
                            llamadasSimultaneas: Number(d.llamadasSimultaneas),
                            ...(d.confirmoRestricciones ? { confirmoRestricciones: true } : {}),
                        },
                        clave.actual(),
                    );
                    clave.renovar();
                    // Probar lo guardado y encender: un paso menos para quien configura.
                    let titulo = "Teléfono guardado";
                    let detalle: string | undefined;
                    if (!guardada.telefono?.activo && d.confirmoRestricciones) {
                        const p = await probarTelefonoCentralita(c.id);
                        if (p.estado === "ok" && p.centralita) {
                            try {
                                await activarTelefono(c.id, p.centralita.version);
                                titulo = "Teléfono guardado y encendido";
                            } catch (e) {
                                detalle = mensajeDeError(e).titulo;
                            }
                        } else {
                            detalle = textoPruebaTelefono(p).titulo;
                        }
                    }
                    avisar({ titulo, ...(detalle ? { tono: "warning" as const, descripcion: `No se encendió: ${detalle}` } : {}) });
                    await liberarProteccion();
                    router.replace(VOLVER);
                } catch (e) {
                    clave.trasError(e);
                    if (!erroresACampos(e, [...CAMPOS], form.setError)) setError(e);
                }
            })}
            navegar={router.push}
            volverA={VOLVER}
            sucio={form.formState.isDirty || huellaOk}
            barra={{
                enviando: form.formState.isSubmitting,
                faltan,
                nota: faltan.length === 0 && !t?.activo ? "Al guardar, Iris lo prueba con lo guardado y lo enciende." : undefined,
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={error instanceof ErrorApi && error.estado === 409 ? "Alguien más cambió la centralita" : "No se pudo guardar"} data-slot="error-guardar">
                        {mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="WSS de la Issabel" descripcion={`Iris llega a wss://${c.host}:${v.wssPuerto}${v.wssRuta} por la VPN, con el certificado fijado por su huella.`}>
                <div className="grid gap-3 sm:grid-cols-2">
                    <CampoNumero etiqueta="Puerto" minimo={1} maximo={65535} ayuda="De fábrica 8089 (http.conf, tlsbindaddr)." error={errores.wssPuerto?.message} {...form.register("wssPuerto")} />
                    <Campo etiqueta="Ruta" autoComplete="off" spellCheck={false} ayuda="De fábrica /ws." error={errores.wssRuta?.message} {...form.register("wssRuta")} />
                </div>
                <div>
                    <Boton variante="secundario" onClick={() => void probar()} disabled={probando}>
                        <RefreshCw />
                        {probando ? "Probando…" : "Probar"}
                    </Boton>
                </div>
                {vigente ? <ResultadoPruebaTelefono prueba={vigente} /> : null}
                {vigente?.estado === "ok" && vigente.huella ? (
                    <div className="grid gap-2 rounded-lg border border-border p-3" data-slot="huella-wss">
                        <p className="text-sm font-semibold">Huella del certificado del WSS</p>
                        <p className="font-mono text-xs break-all">{huellaLegible(vigente.huella)}</p>
                        <Casilla etiqueta="Es la misma que muestra mi Issabel (openssl x509 -fingerprint -sha256)." checked={huellaOk} onChange={(e) => setHuellaOk(e.target.checked)} />
                    </div>
                ) : mismoWss ? (
                    <p className="text-sm text-muted-foreground" data-slot="huella-guardada">
                        Huella confirmada: <span className="font-mono text-xs break-all">{huellaLegible(t?.huellaWss)}</span>
                    </p>
                ) : null}
            </SeccionFormulario>
            <SeccionFormulario titulo="Salir a la calle">
                <div className="grid gap-3 sm:grid-cols-2">
                    <Campo
                        etiqueta="Dominio SIP"
                        autoComplete="off"
                        spellCheck={false}
                        placeholder={c.host}
                        ayuda="Vacío: la dirección de la Issabel."
                        error={errores.sipDominio?.message}
                        {...form.register("sipDominio")}
                    />
                    <Campo etiqueta="Prefijo de salida" inputMode="numeric" autoComplete="off" placeholder="9" ayuda="Lo que marca la Issabel antes de los 8 números (p. ej. 9)." error={errores.prefijoSalida?.message} {...form.register("prefijoSalida")} />
                    <CampoNumero etiqueta="Llamadas a la calle por persona al día" minimo={1} maximo={300} error={errores.llamadasPorUsuarioDia?.message} {...form.register("llamadasPorUsuarioDia")} />
                    <CampoNumero etiqueta="Llamadas a la vez en la empresa" minimo={1} maximo={100} error={errores.llamadasSimultaneas?.message} {...form.register("llamadasSimultaneas")} />
                </div>
                <p className="text-sm text-muted-foreground">Solo números de Guatemala (8 dígitos que empiezan por 2 a 7) y extensiones del equipo. Nunca internacionales ni números especiales.</p>
            </SeccionFormulario>
            <SeccionFormulario titulo="Antes de encender" descripcion="Iris no ve lo que pasa dentro del audio ni dentro de la Issabel. Por eso la Issabel tiene que tener esto (runbook de la centralita, §9.3.1):">
                <ol className="grid list-decimal gap-1.5 pl-5 text-sm" data-slot="restricciones">
                    <li>
                        Un <b>contexto propio</b> (<span className="font-mono">from-iris</span>) para las extensiones del teléfono de Iris, que solo sale a Guatemala (prefijo + 8 números que empiezan por 2 a 7) y a
                        números internos de 2 a 6 cifras. En cada extensión, <span className="font-mono">Context = from-iris</span>.
                    </li>
                    <li>
                        <span className="font-mono">TRANSFER_CONTEXT=from-iris</span>: una transferencia solo llega a lo que permite ese contexto.
                    </li>
                    <li>
                        <b>Sin transferencias por teclado</b>: «Asterisk Dial Options» y «Asterisk Outbound Trunk Dial Options» sin <span className="font-mono">T</span> ni <span className="font-mono">t</span>.
                    </li>
                    <li>
                        <b>Sin desvíos</b>: la opción <span className="font-mono">i</span> de Dial.
                    </li>
                    <li>
                        <b>Ruta de salida sin internacionales ni números especiales</b>: solo <span className="font-mono">9|[2-7]XXXXXXX</span>. Nada de <span className="font-mono">00.</span>,{" "}
                        <span className="font-mono">+.</span>, <span className="font-mono">1XXX</span> ni <span className="font-mono">_X.</span>
                    </li>
                </ol>
                <details>
                    <summary className="cursor-pointer text-sm font-semibold text-muted-foreground">Cómo comprobarlo desde la consola de la Issabel</summary>
                    <pre className="mt-2 overflow-x-auto rounded-lg bg-elevated p-3 font-mono text-xs text-foreground">
                        {'asterisk -rx "dialplan show 0019005551234@from-iris"   # sin coincidencia\nasterisk -rx "dialplan show 955551234@from-iris"       # sale por from-internal\nasterisk -rx "pjsip show endpoint 201" | grep context  # from-iris\nasterisk -rx "database show" | grep -i dial_options     # sin T ni t'}
                    </pre>
                </details>
                <Casilla etiqueta="Confirmo que la Issabel está configurada como dice la guía" checked={confirmo} onChange={(e) => form.setValue("confirmoRestricciones", e.target.checked, { shouldDirty: true })} />
                {!confirmo && t?.activo ? <p className="text-sm text-warning">Sin esta confirmación el teléfono se apaga al guardar.</p> : null}
            </SeccionFormulario>
            <SeccionFormulario titulo="Qué configurar en Issabel (una vez)">
                <ol className="grid gap-3 text-sm" data-slot="preparar-issabel">
                    <li className="grid gap-1.5">
                        <span>
                            <b>1.</b> Encender WSS en el 8089 (solo lo usa Iris; puede ser el mismo certificado del AMI):
                        </span>
                        <pre className="overflow-x-auto rounded-lg bg-elevated p-3 font-mono text-xs">
                            {"; /etc/asterisk/http_custom.conf\ntlsenable=yes\ntlsbindaddr=0.0.0.0:8089\ntlscertfile=/etc/asterisk/keys/ami.crt\ntlsprivatekey=/etc/asterisk/keys/ami.key"}
                        </pre>
                    </li>
                    <li className="grid gap-1.5">
                        <span>
                            <b>2.</b> En cada extensión que conteste en Iris: <b>PBX › Extensiones › Avanzado</b>, «WebRTC: Sí» (o en pjsip):
                        </span>
                        <pre className="overflow-x-auto rounded-lg bg-elevated p-3 font-mono text-xs">{"webrtc=yes\ntransport=transport-wss\nallow=!all,opus,ulaw\ncontext=from-iris"}</pre>
                    </li>
                    <li>
                        <b>3.</b> Firewall: abrir <b>TCP 8089</b> y el audio <b>UDP 10000–20000</b> <b>solo a la IP de Iris</b>. Nada a internet: los celulares entran por Iris.
                    </li>
                    <li>
                        <b>4.</b> Recargar: <span className="font-mono">asterisk -rx &quot;module reload res_http_websocket.so&quot;</span> y <span className="font-mono">asterisk -rx &quot;pjsip reload&quot;</span>.
                    </li>
                </ol>
            </SeccionFormulario>
        </FormularioPagina>
    );
}

export function ResultadoPruebaTelefono({ prueba }: { prueba: Pick<PruebaTelefono, "estado"> }) {
    const r = textoPruebaTelefono(prueba);
    return (
        <Aviso tono={r.tono === "success" ? "success" : "error"} titulo={r.titulo} data-slot="resultado-prueba-telefono" data-estado={prueba.estado}>
            {r.detalle}
        </Aviso>
    );
}
