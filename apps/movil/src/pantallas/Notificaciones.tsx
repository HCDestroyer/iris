// «Yo» → «Notificaciones» y el primer uso (diseño aprobado por el PM, nov 2026): qué te avisamos
// (por tipo; «Bandeja de clientes» solo con `bandejaDisponible`), el contenido del aviso con su
// muestra, No molestar (desde, hasta y «Avisar lo urgente igual») y «Ajustes del sistema» (los
// canales de Android). Las preferencias son de la cuenta (`GET/POST /yo/notificaciones` con
// `version`): valen en cualquier teléfono. Con el permiso bloqueado, el aviso con «Activar».
import { AtSign, Bell, BellOff, ClipboardList, ExternalLink, Inbox, MessageCircle, Moon, Settings, Zap, type LucideIcon } from "lucide-react-native";
import { useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { ErrorApi } from "../api/errores";
import { Aviso, Boton, Campo, Cargando, Dialogo, Pila, Texto } from "../componentes";
import { EncabezadoPantalla, FilaAjuste, FilaInterruptor, FilaRadio, GrupoFilas, Seccion } from "../componentes/cuenta";
import { useCampo } from "../campo/contexto";
import type { EntradaPreferenciasPush, PreferenciasPush } from "../push/api";
import { usePush } from "../push/contexto";
import { errorHora, lineaNoMolestar } from "../push/textos";
import { useTema } from "../tema/tema";

type Tipo = keyof PreferenciasPush["tipos"];

const TIPOS: readonly { tipo: Tipo; icono: LucideIcon; titulo: string; detalle: string }[] = [
    { tipo: "chat", icono: MessageCircle, titulo: "Mensajes del equipo", detalle: "Directos, de tu cuadrilla y de tus órdenes" },
    { tipo: "mencion", icono: AtSign, titulo: "Menciones", detalle: "Cuando alguien te menciona con @" },
    { tipo: "orden", icono: ClipboardList, titulo: "Órdenes", detalle: "Asignadas a ti o reprogramadas" },
    { tipo: "bandeja", icono: Inbox, titulo: "Bandeja de clientes", detalle: "Un cliente espera y nadie lo atiende" },
];

function entradaDe(p: PreferenciasPush): EntradaPreferenciasPush {
    return { version: p.version, tipos: { ...p.tipos }, contenido: p.contenido, noMolestar: { ...p.noMolestar }, zonaHoraria: p.zonaHoraria };
}

export function Notificaciones() {
    const push = usePush();
    const { navegar, notificar } = useCampo();
    const { colores, campo } = useTema();
    const [guardando, setGuardando] = useState(false);
    const [editando, setEditando] = useState<"desde" | "hasta" | null>(null);
    const [ahora] = useState(Date.now);
    const volver = () => navegar({ tipo: "yo" });

    if (!push || push.preferencias.estado === "cargando") {
        return (
            <Pila espacio={20}>
                <EncabezadoPantalla titulo="Notificaciones" alVolver={volver} />
                <Cargando etiqueta="Leyendo tus notificaciones" />
            </Pila>
        );
    }
    if (push.preferencias.estado === "error") {
        const sinSenal = push.preferencias.error instanceof ErrorApi && push.preferencias.error.estado === 0;
        return (
            <Pila espacio={20}>
                <EncabezadoPantalla titulo="Notificaciones" alVolver={volver} />
                <Aviso
                    tono="error"
                    titulo={sinSenal ? "Hace falta señal" : "No se pudieron leer tus notificaciones"}
                    testID="error-notificaciones"
                    accion={
                        <Boton variante="secundario" alPresionar={push.recargar}>
                            Reintentar
                        </Boton>
                    }
                >
                    {sinSenal ? "Tus notificaciones se guardan en tu cuenta: conéctate para verlas o cambiarlas." : "Vuelve a intentarlo en un momento."}
                </Aviso>
            </Pila>
        );
    }
    const p = push.preferencias.preferencias;
    const bloqueadas = push.permiso === "denegado";
    const sinConfigurar = p.push.estado !== "configurado";
    const inactivo = bloqueadas || guardando;

    const guardar = async (cambio: (e: EntradaPreferenciasPush) => EntradaPreferenciasPush, texto = "Guardado") => {
        setGuardando(true);
        try {
            await push.guardar(cambio(entradaDe(p)));
            notificar({ tono: "success", texto });
        } catch (e) {
            const conflicto = e instanceof ErrorApi && e.estado === 409;
            const sinSenal = e instanceof ErrorApi && e.estado === 0;
            notificar({
                tono: "error",
                texto: conflicto ? "Se cambiaron en otro teléfono: revisa cómo quedaron y vuelve a intentarlo." : sinSenal ? "Hace falta señal para guardar." : "No se pudo guardar. Vuelve a intentarlo.",
            });
        } finally {
            setGuardando(false);
        }
    };

    const tipos = TIPOS.filter((t) => t.tipo !== "bandeja" || p.bandejaDisponible);
    return (
        <Pila espacio={20}>
            <EncabezadoPantalla titulo="Notificaciones" alVolver={volver} />

            {bloqueadas ? (
                <Aviso
                    tono="warning"
                    icono={BellOff}
                    titulo="Las notificaciones están bloqueadas en este teléfono"
                    testID="aviso-bloqueadas"
                    accion={
                        <Boton variante="tinta" icono={ExternalLink} alPresionar={() => void push.abrirAjustes()}>
                            Activar
                        </Boton>
                    }
                >
                    Iris no puede avisarte de mensajes ni de órdenes nuevas hasta que las actives en los ajustes de Android.
                </Aviso>
            ) : null}
            {sinConfigurar ? (
                <Aviso tono="info" icono={Bell} titulo={p.push.texto || "Notificaciones push: sin configurar"} testID="aviso-sin-configurar">
                    Tus preferencias se guardan igual y valen cuando el administrador active el push.
                </Aviso>
            ) : null}

            <Seccion titulo="Qué te avisamos">
                <GrupoFilas testID="grupo-tipos">
                    {tipos.map((t) => (
                        <FilaInterruptor
                            key={t.tipo}
                            icono={t.icono}
                            titulo={t.titulo}
                            detalle={t.detalle}
                            valor={p.tipos[t.tipo]}
                            deshabilitado={inactivo}
                            testID={`tipo-${t.tipo}`}
                            alCambiar={(v) => void guardar((e) => ({ ...e, tipos: { ...e.tipos, [t.tipo]: v } }))}
                        />
                    ))}
                </GrupoFilas>
            </Seccion>

            <Seccion titulo="Contenido del aviso">
                <GrupoFilas testID="grupo-contenido">
                    <FilaRadio
                        titulo="Mostrar el mensaje"
                        detalle="Ves quién escribe y lo que dice"
                        elegida={p.contenido === "completo"}
                        deshabilitado={inactivo}
                        testID="contenido-completo"
                        alElegir={() => p.contenido !== "completo" && void guardar((e) => ({ ...e, contenido: "completo" }))}
                    >
                        {p.contenido === "completo" ? (
                            <View style={[estilos.muestra, { backgroundColor: campo.panel2 }]} testID="muestra-aviso">
                                <View style={[estilos.iniciales, { backgroundColor: colores.primario }]}>
                                    <Text style={[estilos.textoIniciales, { color: colores.sobrePrimario }]}>CR</Text>
                                </View>
                                <View style={estilos.flex}>
                                    <Text style={[estilos.remitente, { color: colores.texto }]}>Carlos Ruiz</Text>
                                    <Text style={{ color: colores.textoSuave }}>ya llegué a la casa</Text>
                                </View>
                            </View>
                        ) : null}
                    </FilaRadio>
                    <FilaRadio
                        titulo="Solo quién escribe"
                        detalle="El texto se lee al abrir Iris"
                        elegida={p.contenido === "remitente"}
                        deshabilitado={inactivo}
                        testID="contenido-remitente"
                        alElegir={() => p.contenido !== "remitente" && void guardar((e) => ({ ...e, contenido: "remitente" }))}
                    >
                        {p.contenido === "remitente" ? (
                            <View style={[estilos.muestra, { backgroundColor: campo.panel2 }]} testID="muestra-aviso">
                                <Text style={{ color: colores.texto }}>Nuevo mensaje de Carlos Ruiz</Text>
                            </View>
                        ) : null}
                    </FilaRadio>
                </GrupoFilas>
            </Seccion>

            <Seccion titulo="Horario">
                <GrupoFilas testID="grupo-horario">
                    <FilaInterruptor
                        icono={Moon}
                        titulo="No molestar"
                        detalle="Sin sonido ni avisos en ese horario"
                        valor={p.noMolestar.activo}
                        deshabilitado={inactivo}
                        testID="no-molestar"
                        alCambiar={(v) => void guardar((e) => ({ ...e, noMolestar: { ...e.noMolestar, activo: v } }))}
                    />
                    <FilaAjuste titulo="Desde" deshabilitado={inactivo} etiqueta={`Desde ${p.noMolestar.desde}`} derecha={<Valor texto={p.noMolestar.desde} />} alPresionar={() => setEditando("desde")} testID="desde" />
                    <FilaAjuste titulo="Hasta" deshabilitado={inactivo} etiqueta={`Hasta ${p.noMolestar.hasta}`} derecha={<Valor texto={p.noMolestar.hasta} />} alPresionar={() => setEditando("hasta")} testID="hasta" />
                    <FilaInterruptor
                        icono={Zap}
                        titulo="Avisar lo urgente igual"
                        detalle="Por ejemplo, una orden reasignada para hoy"
                        valor={p.noMolestar.urgenteIgual}
                        deshabilitado={inactivo}
                        testID="urgente-igual"
                        alCambiar={(v) => void guardar((e) => ({ ...e, noMolestar: { ...e.noMolestar, urgenteIgual: v } }))}
                    />
                </GrupoFilas>
                <Texto suave testID="linea-no-molestar">
                    {lineaNoMolestar(p, ahora)}
                </Texto>
            </Seccion>

            <Seccion titulo="Sonido y pantalla bloqueada">
                <GrupoFilas>
                    <FilaAjuste icono={Settings} titulo="Ajustes del sistema" derecha={<ExternalLink size={20} color={colores.textoSuave} />} etiqueta="Abrir los ajustes del sistema" alPresionar={() => void push.abrirAjustes()} testID="ajustes-sistema" />
                </GrupoFilas>
                <Texto suave>
                    El sonido, la vibración y lo que se ve con el teléfono bloqueado se configuran en Android, por canal: «Mensajes del equipo», «Menciones», «Órdenes»
                    {p.bandejaDisponible ? ", «Bandeja de clientes»" : ""}.
                </Texto>
            </Seccion>

            {editando ? (
                <EditarHora
                    titulo={editando === "desde" ? "Desde" : "Hasta"}
                    valor={p.noMolestar[editando]}
                    otra={p.noMolestar[editando === "desde" ? "hasta" : "desde"]}
                    alCerrar={() => setEditando(null)}
                    alGuardar={(hora) => {
                        const campoHora = editando;
                        setEditando(null);
                        void guardar((e) => ({ ...e, noMolestar: { ...e.noMolestar, [campoHora]: hora } }));
                    }}
                />
            ) : null}
        </Pila>
    );
}

function Valor({ texto }: { texto: string }) {
    const { colores } = useTema();
    return <Text style={[estilos.valor, { color: colores.textoSuave }]}>{texto}</Text>;
}

/** Una hora de No molestar: un solo campo (HH:MM) en un diálogo corto. */
function EditarHora({ titulo, valor, otra, alCerrar, alGuardar }: { titulo: string; valor: string; otra: string; alCerrar: () => void; alGuardar: (hora: string) => void }) {
    const [texto, setTexto] = useState(valor);
    const [error, setError] = useState<string | null>(null);
    return (
        <Dialogo abierto alCerrar={alCerrar} titulo={titulo} descripcion="Hora del día, de 00:00 a 23:59." cambios={texto !== valor} testID="editar-hora">
            <Campo
                etiqueta="Hora"
                valor={texto}
                alCambiar={(t) => {
                    setTexto(t);
                    setError(null);
                }}
                keyboardType="numbers-and-punctuation"
                maxLength={5}
                placeholder="21:00"
                error={error ?? undefined}
                testID="campo-hora"
            />
            <Boton
                variante="tinta"
                alPresionar={() => {
                    const e = errorHora(texto, otra);
                    if (e) {
                        setError(e);
                        return;
                    }
                    alGuardar(texto.trim());
                }}
            >
                Guardar
            </Boton>
        </Dialogo>
    );
}

/** Antes del diálogo de Android (diseño «01-permiso»): «Activar notificaciones» o «Ahora no». */
export function PrimerUsoPush({ activar, ahoraNo }: { activar: () => void; ahoraNo: () => void }) {
    const { colores, campo, radioTarjeta } = useTema();
    const filas: readonly { icono: LucideIcon; titulo: string; detalle: string }[] = [
        { icono: MessageCircle, titulo: "Mensajes y menciones", detalle: "De tu cuadrilla, de tus órdenes y directos." },
        { icono: ClipboardList, titulo: "Órdenes", detalle: "Cuando te asignan una o te cambian la hora." },
        { icono: Moon, titulo: "Tú decides", detalle: "Qué te avisa y cuándo, en «Yo» → «Notificaciones»." },
    ];
    return (
        <View style={[estilos.primerUso, { backgroundColor: campo.lienzo }]} testID="primer-uso-push">
            <View style={estilos.centro}>
                <View style={[estilos.campana, { backgroundColor: colores.primario }]}>
                    <Bell size={36} color={colores.sobrePrimario} />
                </View>
                <Text accessibilityRole="header" style={[estilos.tituloPrimerUso, { color: colores.texto }]}>
                    Que Iris te avise
                </Text>
                <Text style={[estilos.subtitulo, { color: colores.textoSuave }]}>Aunque tengas la app cerrada, te enteras de lo que necesita tu atención.</Text>
            </View>
            <View style={[estilos.tarjeta, { backgroundColor: campo.panel, borderColor: colores.borde, borderRadius: radioTarjeta }]}>
                {filas.map((f) => (
                    <View key={f.titulo} style={estilos.filaPrimerUso}>
                        <View style={[estilos.iconoPrimerUso, { backgroundColor: campo.panel2 }]}>
                            <f.icono size={22} color={colores.texto} />
                        </View>
                        <View style={estilos.flex}>
                            <Text style={[estilos.remitente, { color: colores.texto }]}>{f.titulo}</Text>
                            <Text style={{ color: colores.textoSuave }}>{f.detalle}</Text>
                        </View>
                    </View>
                ))}
            </View>
            <View style={estilos.pie}>
                <Text style={[estilos.nota, { color: colores.textoSuave }]}>Después Android te pedirá el permiso.</Text>
                <Boton variante="tinta" alPresionar={activar} testID="activar-notificaciones">
                    Activar notificaciones
                </Boton>
                <Boton variante="fantasma" alPresionar={ahoraNo} testID="ahora-no">
                    Ahora no
                </Boton>
            </View>
        </View>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    valor: { fontSize: 16, fontVariant: ["tabular-nums"] },
    muestra: { flexDirection: "row", alignItems: "center", gap: 12, borderRadius: 16, padding: 12 },
    iniciales: { width: 40, height: 40, borderRadius: 20, alignItems: "center", justifyContent: "center" },
    textoIniciales: { fontSize: 15, fontWeight: "700" },
    remitente: { fontSize: 16, fontWeight: "600" },
    primerUso: { flex: 1, paddingHorizontal: 24, paddingTop: 72, paddingBottom: 32, gap: 28 },
    centro: { alignItems: "center", gap: 12 },
    campana: { width: 72, height: 72, borderRadius: 20, alignItems: "center", justifyContent: "center" },
    tituloPrimerUso: { fontSize: 26, lineHeight: 32, fontWeight: "700", textAlign: "center" },
    subtitulo: { fontSize: 16, lineHeight: 22, textAlign: "center" },
    tarjeta: { borderWidth: 1, padding: 18, gap: 18 },
    filaPrimerUso: { flexDirection: "row", gap: 14, alignItems: "flex-start" },
    iconoPrimerUso: { width: 40, height: 40, borderRadius: 12, alignItems: "center", justifyContent: "center" },
    pie: { marginTop: "auto", gap: 10 },
    nota: { textAlign: "center", fontSize: 14 },
});
