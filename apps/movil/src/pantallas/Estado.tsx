// Indicadores del encabezado (opción elegida por el PM, nov 2026), como la PWA: la nube de
// sincronización (conexión y envíos juntos: check discreto, flecha y número en teal, nube tachada en
// naranja con o sin número) y el GPS, junto a buscar y también en la barra compacta. Tocar
// uno abre su hoja, solo con lo suyo: «Ubicación» («Compartir mi ubicación», HeroUI Switch con la
// lógica del rastreo: apagar = «Dejar de compartir», encender = la acción propia que la levanta, o la
// explicación si falta) o «Conexión y envíos» (la señal, la última sincronización y lo pendiente con
// «Por enviar» y, con señal y algo pendiente, «Enviar ahora»). Las reglas son de `@iris/sync/campo` (`indicadoresEstado`).
import { horaCorta, indicadoresEstado, partesEnvio, resumenEnvio, totalPorEnviar, type EstadoUbicacionIndicador, type IdIndicador, type Indicador } from "@iris/sync/campo";
import { ArrowUp, Building2, ChevronRight, CloudCheck, CloudOff, CloudUpload, KeyRound, MapPin, MapPinOff, TriangleAlert } from "lucide-react-native";
import { useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Boton, Dialogo, Interruptor, Pila, Texto, Tocable } from "../componentes";
import { useCampo } from "../campo/contexto";
import type { EstadoRastreo } from "../campo/ubicacion";
import { useTema } from "../tema/tema";

/** El rastreo nativo en la forma de los indicadores. Lo que no sea `activa` o fuera de curso pasa con una orden en curso. */
export function ubicacionIndicador(e: EstadoRastreo): EstadoUbicacionIndicador {
    switch (e.tipo) {
        case "activa":
            return "activa";
        case "fuera_de_curso":
        case "sin_confirmar":
            return "apagada";
        default:
            return "apagada_en_turno";
    }
}

/** Texto del rastreo en la hoja (el de la fila de ubicación que había en el encabezado). */
export function textoRastreo(e: EstadoRastreo): string {
    switch (e.tipo) {
        case "activa": {
            const hasta = horaCorta(new Date(e.hasta).toISOString()) ?? "";
            return e.soloPrimerPlano ? `Ubicación activa con la app abierta · hasta las ${hasta}` : `Ubicación activa · orden en curso · hasta las ${hasta}`;
        }
        case "pausada_por_ti":
            return "Dejaste de compartir la ubicación · vuelve con tu próximo «En camino», «Llegué» o «Reanudar»";
        case "sin_confirmar":
            return "Ubicación en espera · se enciende al conectar con el servidor";
        case "vencida":
            return "Ubicación apagada: pasó el tope del día sin confirmar con el servidor · vuelve al sincronizar";
        case "fuera_de_curso":
            return "Ubicación apagada · sin orden en curso";
        case "explicar":
            return "Ubicación apagada · falta permitirla";
        case "sin_permiso":
            return "Sin permiso de ubicación · actívalo en los ajustes del teléfono";
        case "error":
            return `GPS: ${e.mensaje}`;
    }
}

/** El icono de cada forma: check, flecha, nube tachada, llave o el pin del GPS. */
function IconoIndicador({ indicador: i, tamano, color }: { indicador: Indicador | undefined; tamano: number; color: string }) {
    switch (i?.forma) {
        case undefined:
        case "al_dia":
            return <CloudCheck size={tamano} color={color} />;
        case "pendientes":
            return i.tono === "error" ? <TriangleAlert size={tamano} color={color} /> : <ArrowUp size={tamano} color={color} />;
        case "sin_senal":
        case "sin_senal_pendientes":
            return <CloudOff size={tamano} color={color} />;
        case "bloqueada":
            return <KeyRound size={tamano} color={color} />;
        case "activa":
            return <MapPin size={tamano} color={color} />;
        default:
            return <MapPinOff size={tamano} color={color} />;
    }
}

function useColores() {
    const { colores, campo } = useTema();
    const fondo: Record<Indicador["tono"], string> = {
        marca: campo.marcaSuave,
        success: campo.panel,
        neutral: campo.panel2,
        info: campo.infoFondo,
        warning: campo.avisoFondo,
        error: campo.errorFondo,
    };
    const tinta: Record<Indicador["tono"], string> = {
        marca: campo.marcaSuaveTexto,
        success: colores.tono.success,
        neutral: colores.textoSuave,
        info: colores.tono.info,
        warning: colores.tono.warning,
        error: colores.tono.error,
    };
    return { fondo, tinta };
}

/** Los dos indicadores (zona táctil de 44 px). `compacta`: la copia de la barra compacta, sin testID. */
export function IndicadoresEstado({ alEntrar, alElegirEmpresa, compacta = false }: { alEntrar: () => void; alElegirEmpresa?: (() => void) | undefined; compacta?: boolean }) {
    const { estado, estadoRastreo } = useCampo();
    const { colores } = useTema();
    const { fondo, tinta } = useColores();
    const [abierta, setAbierta] = useState<IdIndicador | null>(null);
    const lista = indicadoresEstado(estado, ubicacionIndicador(estadoRastreo));
    return (
        <>
            <View style={estilos.fila} accessibilityLabel="Estado del teléfono" {...(compacta ? {} : { testID: "indicadores-estado" })}>
                {lista.map((i) => {
                    return (
                        <Tocable key={i.id} etiqueta={i.etiqueta} alPresionar={() => setAbierta(i.id)} estilo={estilos.zona} {...(compacta ? {} : { testID: `indicador-${i.id}` })}>
                            <View style={[estilos.circulo, { backgroundColor: fondo[i.tono], borderColor: colores.borde, width: i.actuar ? 36 : 32, height: i.actuar ? 36 : 32 }]}>
                                <IconoIndicador indicador={i} tamano={18} color={tinta[i.tono]} />
                            </View>
                            {i.marca !== null ? (
                                <View style={[estilos.marca, typeof i.marca === "number" ? estilos.marcaNumero : estilos.marcaPunto, { backgroundColor: i.tono === "marca" ? colores.primario : tinta[i.tono], borderColor: colores.fondo }]}>
                                    {typeof i.marca === "number" ? <Text style={[estilos.textoMarca, { color: i.tono === "marca" ? colores.sobrePrimario : colores.fondo }]}>{i.marca > 99 ? "99+" : String(i.marca)}</Text> : null}
                                </View>
                            ) : null}
                        </Tocable>
                    );
                })}
            </View>
            {abierta ? <HojaEstado seccion={abierta} alCerrar={() => setAbierta(null)} alEntrar={alEntrar} alElegirEmpresa={alElegirEmpresa} /> : null}
        </>
    );
}

export function HojaEstado({ seccion, alCerrar, alEntrar, alElegirEmpresa }: { seccion: IdIndicador; alCerrar: () => void; alEntrar: () => void; alElegirEmpresa?: (() => void) | undefined }) {
    const { estado, estadoRastreo, rastreo, navegar, motor } = useCampo();
    const [enviando, setEnviando] = useState(false);
    const { colores, campo } = useTema();
    const { fondo, tinta } = useColores();
    const r = resumenEnvio(estado);
    const { pendientes, rechazos } = partesEnvio(r);
    const detalle = [...pendientes, ...(rechazos ? [rechazos] : [])].join(" · ");
    const total = totalPorEnviar(estado);
    const [nube] = indicadoresEstado(estado, ubicacionIndicador(estadoRastreo));
    const ultima = estado.ultimaSincronizacion === null ? null : horaCorta(new Date(estado.ultimaSincronizacion).toISOString());
    const activa = estadoRastreo.tipo === "activa";
    const fueraDeCurso = estadoRastreo.tipo === "fuera_de_curso";
    const cambiar = (encender: boolean) => {
        if (!rastreo) {
            return;
        }
        if (!encender) {
            void rastreo.dejarDeCompartir();
        } else if (estadoRastreo.tipo === "explicar") {
            void rastreo.aceptarExplicacion();
        } else {
            void rastreo.accionPropia();
        }
    };
    const borde = { borderColor: colores.borde, borderWidth: 1 };
    const conSenal = estado.conexion === "en_linea" || estado.conexion === "desconocida";
    const enviarAhora = async () => {
        setEnviando(true);
        try {
            await motor.sincronizar();
        } catch {
            // Lo que no salió sigue en «Por enviar» (y en el número de la nube).
        } finally {
            setEnviando(false);
        }
    };
    return (
        <Dialogo abierto alCerrar={alCerrar} titulo={seccion === "ubicacion" ? "Ubicación" : "Conexión y envíos"} testID="hoja-estado">
            <Pila>
                {seccion === "ubicacion" ? (
                <View testID="seccion-ubicacion" style={[estilos.seccion, { backgroundColor: campo.panel2 }, borde]}>
                    <Interruptor
                        etiqueta="Compartir mi ubicación"
                        descripcion={
                            fueraDeCurso
                                ? "Se enciende sola con tu próxima orden en camino o en sitio. Fuera de curso no se registra nada."
                                : activa
                                  ? "Encendida · despacho te ve en el mapa mientras tengas una orden en curso."
                                  : "Apagada · al encenderla despacho te ve en el mapa mientras tengas una orden en curso."
                        }
                        valor={activa}
                        alCambiar={cambiar}
                        testID="interruptor-ubicacion"
                    />
                    <Text testID="estado-ubicacion" style={{ color: colores.textoSuave, fontSize: 13 }}>
                        {textoRastreo(estadoRastreo)}
                    </Text>
                </View>
                ) : (
                <View testID="seccion-sincronizacion" style={[estilos.seccion, { backgroundColor: campo.panel }, borde]}>
                    <View style={estilos.filaSeccion}>
                        <View style={[estilos.cuadro, { backgroundColor: fondo[nube?.tono ?? "success"] }]}>
                            <IconoIndicador indicador={nube} tamano={20} color={tinta[nube?.tono ?? "success"]} />
                        </View>
                        <View style={estilos.flex}>
                            <Text testID="estado-conexion" style={[estilos.titulo, { color: colores.texto }]}>
                                {r.texto}
                            </Text>
                            <Texto suave>
                                {ultima ? `Última sincronización ${ultima}.` : "Todavía no se sincroniza hoy."}
                                {estado.conexion === "sin_conexion" ? " Puedes seguir trabajando." : ""}
                            </Texto>
                        </View>
                    </View>
                    {estado.conexion === "sin_sesion" ? (
                        <Boton
                            variante="secundario"
                            icono={KeyRound}
                            alPresionar={() => {
                                alCerrar();
                                alEntrar();
                            }}
                        >
                            Entrar de nuevo
                        </Boton>
                    ) : estado.conexion === "sin_empresa" && alElegirEmpresa ? (
                        <Boton
                            variante="secundario"
                            icono={Building2}
                            alPresionar={() => {
                                alCerrar();
                                alElegirEmpresa();
                            }}
                        >
                            Elegir empresa
                        </Boton>
                    ) : null}
                    <View style={[estilos.filaSeccion, estilos.separado, { borderColor: colores.borde }]}>
                        <View style={[estilos.cuadro, { backgroundColor: total > 0 ? fondo[r.rechazos > 0 ? "error" : "warning"] : fondo.neutral }]}>
                            {r.rechazos > 0 ? <TriangleAlert size={20} color={tinta.error} /> : <CloudUpload size={20} color={total > 0 ? tinta.warning : tinta.neutral} />}
                        </View>
                        <View style={estilos.flex}>
                            <Text style={[estilos.titulo, { color: colores.texto }]}>{total > 0 ? `${total} por enviar` : "Todo enviado"}</Text>
                            <Text testID="pendientes-por-enviar" style={{ color: colores.textoSuave, fontSize: 14 }}>
                                {total > 0 ? detalle : "No queda nada en el teléfono por enviar."}
                            </Text>
                        </View>
                    </View>
                    {total > 0 && conSenal && r.porEnviar + Math.max(0, r.archivos) > 0 ? (
                        <Boton icono={CloudUpload} cargando={enviando || estado.sincronizando} alPresionar={() => void enviarAhora()} testID="enviar-ahora">
                            Enviar ahora
                        </Boton>
                    ) : null}
                    {total > 0 ? (
                        <Boton
                            variante="secundario"
                            icono={ChevronRight}
                            alPresionar={() => {
                                alCerrar();
                                navegar({ tipo: "pendientes" });
                            }}
                        >
                            Ver «Por enviar»
                        </Boton>
                    ) : null}
                </View>
                )}
            </Pila>
        </Dialogo>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    fila: { flexDirection: "row", alignItems: "center" },
    zona: { width: 44, height: 44, minHeight: 44, alignItems: "center", justifyContent: "center", borderRadius: 22 },
    circulo: { borderRadius: 18, borderWidth: 1, alignItems: "center", justifyContent: "center" },
    marca: { position: "absolute", top: 0, right: 0, borderWidth: 2, alignItems: "center", justifyContent: "center" },
    marcaNumero: { minWidth: 18, height: 18, borderRadius: 9, paddingHorizontal: 4 },
    marcaPunto: { width: 12, height: 12, borderRadius: 6 },
    textoMarca: { fontSize: 10, fontWeight: "700" },
    seccion: { borderRadius: 22, padding: 12, gap: 10 },
    filaSeccion: { flexDirection: "row", alignItems: "flex-start", gap: 12 },
    separado: { borderTopWidth: 1, paddingTop: 10 },
    cuadro: { width: 44, height: 44, borderRadius: 14, alignItems: "center", justifyContent: "center" },
    titulo: { fontSize: 16, fontWeight: "600" },
});
