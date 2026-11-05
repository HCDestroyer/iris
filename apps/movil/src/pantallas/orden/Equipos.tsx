import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { CONDICIONES, esquemaEquipo, normalizarMac, textoElegido, textosEscaneo, type DatosEquipo, type ObjetivoEscaneo, type UsoCodigo } from "@iris/sync/campo";
import { ArrowDownToLine, ArrowUpFromLine, ScanLine, ShieldCheck, ShieldQuestion } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { StyleSheet, View } from "react-native";
import { Boton, BotonIcono, Campo, Insignia, Opciones, Tarjeta, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";
import { Escaner } from "./Escaner";

/** Equipos instalados y retirados según el tipo de orden; serie y MAC escaneadas o a mano. */
export function Equipos({ orden, instalar, retirar }: { orden: OrdenVista; instalar: boolean; retirar: boolean }) {
    const registrados = orden.equipos ?? [];
    const [elegida, setAccion] = useState<"instalar" | "retirar">("instalar");
    const accion = instalar && retirar ? elegida : instalar ? "instalar" : "retirar";
    const comodato = orden.comodato.disponible ? (orden.comodato.equipos ?? []) : [];
    if (!instalar && !retirar && registrados.length === 0) {
        return null;
    }
    return (
        <Tarjeta titulo="Equipos">
            {registrados.map((e) => (
                <Insignia
                    key={e.id}
                    tono="info"
                    icono={e.accion === "instalar" ? ArrowDownToLine : ArrowUpFromLine}
                    texto={`${e.accion === "instalar" ? "Instalado" : "Retirado"} · ${e.serie ?? `Unidad ${e.unidadId ?? "—"}`}${e.mac ? ` · ${e.mac}` : ""}${e.condicion ? ` · ${CONDICIONES[e.condicion]}` : ""}`}
                />
            ))}
            {instalar && retirar ? (
                <Opciones
                    etiqueta="Movimiento"
                    opciones={[
                        { valor: "instalar", etiqueta: "Instalar", icono: ArrowDownToLine },
                        { valor: "retirar", etiqueta: "Retirar", icono: ArrowUpFromLine },
                    ]}
                    valor={accion}
                    alCambiar={setAccion}
                />
            ) : null}
            {instalar || retirar ? <FormularioEquipo key={accion} orden={orden} accion={accion} comodato={comodato} /> : null}
        </Tarjeta>
    );
}

export function FormularioEquipo({ orden, accion, comodato }: { orden: OrdenVista; accion: "instalar" | "retirar"; comodato: NonNullable<OrdenVista["comodato"]["equipos"]> }) {
    const { motor, ejecutar, notificar } = useCampo();
    // Campo que se está escaneando (el escáner sabe su objetivo) y lo último que se llenó escaneando.
    const [escaneando, setEscaneando] = useState<ObjetivoEscaneo | null>(null);
    const [escaneado, setEscaneado] = useState<UsoCodigo>({});
    const vacio: DatosEquipo = { accion, serie: "", mac: "", modelo: "", unidadId: "", condicion: "" };
    const form = useForm<DatosEquipo>({ resolver: zodResolver(esquemaEquipo), defaultValues: vacio });
    const unidadId = useWatch({ control: form.control, name: "unidadId" });
    const serie = useWatch({ control: form.control, name: "serie" });
    const mac = useWatch({ control: form.control, name: "mac" });
    const errores = form.formState.errors;

    const enviar = form.handleSubmit(async (d) => {
        const datos = {
            ...(d.unidadId ? { unidadId: Number(d.unidadId) } : {}),
            ...(d.serie ? { serie: d.serie } : {}),
            ...(d.mac ? { mac: normalizarMac(d.mac) ?? d.mac } : {}),
            ...(d.modelo ? { modelo: d.modelo } : {}),
            ...(d.accion === "retirar" && d.condicion ? { condicion: d.condicion } : {}),
        };
        const tipo = d.accion === "instalar" ? "equipo.instalar" : "equipo.retirar";
        if (await ejecutar(() => motor.registrar(tipo, orden.id, datos), d.accion === "instalar" ? "Equipo instalado registrado" : "Equipo retirado registrado")) {
            form.reset(vacio);
            setEscaneado({});
        }
    });

    const alElegir = (uso: UsoCodigo) => {
        setEscaneando(null);
        if (uso.serie) {
            form.setValue("serie", uso.serie, { shouldValidate: true, shouldDirty: true });
        }
        if (uso.mac) {
            form.setValue("mac", uso.mac, { shouldValidate: true, shouldDirty: true });
        }
        setEscaneado((e) => ({ ...e, ...uso }));
        notificar({ tono: "success", texto: textoElegido(uso) });
    };

    /** Botón de escanear de un campo (icono con nombre accesible, 48 px). */
    const botonEscanear = (objetivo: ObjetivoEscaneo) => (
        <View style={estilos.botonCampo}>
            <BotonIcono icono={ScanLine} etiqueta={textosEscaneo(objetivo).botonCampo} alPresionar={() => setEscaneando(objetivo)} testID={`escanear-${objetivo}`} />
        </View>
    );
    /** Insignia discreta «Escaneado» mientras el valor es el que se leyó (al editarlo, se quita). */
    const insigniaEscaneado = (valor: string, leido: string | undefined): ReactNode =>
        leido && valor === leido ? <Insignia tono="info" icono={ScanLine} texto="Escaneado" testID="escaneado" /> : null;

    return (
        <>
            {accion === "retirar" && comodato.length > 0 ? (
                <Opciones
                    etiqueta="Equipo en comodato"
                    columna
                    opciones={[{ valor: "", etiqueta: "Otro (escribir o escanear)" }, ...comodato.map((c) => ({ valor: String(c.unidadId), etiqueta: `${c.modelo} · ${c.serie}` }))]}
                    valor={unidadId}
                    alCambiar={(v) => {
                        const eq = comodato.find((c) => String(c.unidadId) === v);
                        form.setValue("unidadId", v);
                        form.setValue("serie", eq?.serie ?? "", { shouldValidate: true });
                        form.setValue("mac", eq?.mac ?? "");
                        form.setValue("modelo", eq?.modelo ?? "");
                    }}
                />
            ) : null}
            <Escaner objetivo={escaneando} existentes={orden.equipos ?? []} alElegir={alElegir} alEscribirAMano={() => setEscaneando(null)} alCerrar={() => setEscaneando(null)} />
            <View style={estilos.filaCampo}>
                <View style={estilos.campo}>
                    <Controller
                        control={form.control}
                        name="serie"
                        render={({ field }) => <Campo etiqueta="Número de serie" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} autoCapitalize="characters" autoCorrect={false} error={errores.serie?.message} />}
                    />
                </View>
                {botonEscanear("serie")}
            </View>
            {insigniaEscaneado(serie, escaneado.serie)}
            <View style={estilos.filaCampo}>
                <View style={estilos.campo}>
                    <Controller
                        control={form.control}
                        name="mac"
                        render={({ field }) => (
                            <Campo etiqueta="MAC" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} autoCapitalize="characters" autoCorrect={false} placeholder="AA:BB:CC:DD:EE:FF" error={errores.mac?.message} />
                        )}
                    />
                </View>
                {botonEscanear("mac")}
            </View>
            {insigniaEscaneado(mac, escaneado.mac)}
            {accion === "instalar" ? (
                <Controller
                    control={form.control}
                    name="modelo"
                    render={({ field }) => <Campo etiqueta="Modelo" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} error={errores.modelo?.message} />}
                />
            ) : (
                <Controller
                    control={form.control}
                    name="condicion"
                    render={({ field }) => (
                        <Opciones
                            etiqueta="Condición"
                            opciones={Object.entries(CONDICIONES).map(([valor, etiqueta]) => ({ valor: valor as keyof typeof CONDICIONES, etiqueta }))}
                            valor={field.value === "" ? undefined : field.value}
                            alCambiar={field.onChange}
                            error={errores.condicion?.message}
                        />
                    )}
                />
            )}
            {accion === "retirar" ? (
                <Insignia
                    tono="neutral"
                    icono={comodato.length ? ShieldCheck : ShieldQuestion}
                    texto={comodato.length ? "Elige el equipo del comodato para que coincida con Argos." : "Sin comodato de Argos en el teléfono: se valida al enviar."}
                />
            ) : null}
            <Boton variante="secundario" cargando={form.formState.isSubmitting} alPresionar={() => void enviar()}>
                {accion === "instalar" ? "Registrar equipo instalado" : "Registrar equipo retirado"}
            </Boton>
            {errores.accion ? <Texto>{errores.accion.message}</Texto> : null}
        </>
    );
}

const estilos = StyleSheet.create({
    filaCampo: { flexDirection: "row", alignItems: "flex-start", gap: 8 },
    campo: { flex: 1 },
    // Alineado con la entrada (bajo la etiqueta del campo).
    botonCampo: { paddingTop: 26 },
});
