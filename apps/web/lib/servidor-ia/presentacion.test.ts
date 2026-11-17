import { describe, expect, it } from "vitest";
import { cantidad, cifrasVigentes, COMANDO, duracionLlana, lecturaReciente, lineaTarjeta, enServicio, huellaLegible, lineaEstado, queActualizar, seriesDe, tamano, tarjetaGpu, textoAviso, textoError, textoErrorEnlace, textoPlazas, tiempoRespuesta, tonoFrente, ultimaVez } from "./presentacion";
import type { AvisoServidor, Tablero } from "./tipos";
import { DE_FABRICA, entradaDeUmbrales, esDeFabrica, esquemaEmpresas, esquemaEnlace, esquemaUmbrales, valoresDeUmbrales } from "./umbrales";

const AHORA = Date.parse("2026-11-17T17:02:00Z");
const estado = (e: Partial<Tablero["estado"]>): Tablero["estado"] => ({ motorId: 1, estado: "listo", detalle: null, version: "0.1.0", compatible: true, motivoIncompatible: null, encendidoSeg: 6 * 86_400, tomadoEn: "2026-11-17T17:01:56Z", lecturaOk: true, modelosDisponibles: [], ...e });
const tablero = (e: Partial<Tablero["estado"]>, servidor: Tablero["servidor"] = { gpus: { estado: "omitido", lista: [] }, cpu: { nucleos: 16, usoPct: null }, memoria: { usadoMb: 0, totalMb: 0 }, disco: null, modelos: [], llm: { enCurso: 0, maxSimultaneas: 4 }, rechazadas: 0 }): Tablero => ({ estado: estado(e), servidor, series: null, uso: null, avisos: [] });

describe("textos del Servidor de IA", () => {
    it("cantidades, tamaños y tiempos en llano", () => {
        expect(duracionLlana(6 * 86_400 + 5)).toBe("6 días");
        expect(duracionLlana(3_600)).toBe("1 hora");
        expect(duracionLlana(20)).toBe("menos de un minuto");
        expect(tamano(274)).toBe("274 MB");
        expect(tamano(13_414)).toBe("13.1 GB");
        expect(cantidad(10_613)).toBe("10,613");
        expect(cantidad(3_400_000)).toBe("3.4 M");
        expect(cantidad(170_400)).toBe("170 K");
        expect(tiempoRespuesta(850)).toBe("850 ms");
        expect(tiempoRespuesta(1_740)).toBe("1.7 s");
    });

    it("la línea de estado dice versión, compatibilidad y cuánto lleva encendido", () => {
        expect(lineaEstado(estado({}), AHORA)).toEqual({ tono: "ok", titulo: "En línea", detalle: "versión 0.1.0 · compatible con esta versión de Iris · encendido hace 6 días" });
        expect(lineaEstado(estado({ estado: null }), AHORA).titulo).toBe("Sin servidor de IA");
        const caido = lineaEstado(estado({ estado: "no_disponible", tomadoEn: "2026-11-17T16:42:00Z" }), AHORA);
        expect(caido.tono).toBe("error");
        expect(caido.detalle).toContain("visto por última vez hoy a las 10:42 (hace 20 min)");
        expect(ultimaVez("2026-11-16T16:42:00Z", AHORA)).toContain("el 16 de noviembre a las 10:42");
    });

    it("las cifras de ahora solo valen con el servidor en servicio y la última lectura buena", () => {
        expect(enServicio("listo")).toBe(true);
        expect(enServicio("degradado")).toBe(false);
        expect(cifrasVigentes(tablero({}), AHORA)).toBe(true);
        expect(cifrasVigentes(tablero({ lecturaOk: false }), AHORA)).toBe(false);
        expect(cifrasVigentes(tablero({ estado: "no_disponible" }), AHORA)).toBe(false);
        expect(cifrasVigentes(tablero({}, null), AHORA)).toBe(false);
        // Una lectura de hace 5 minutos no es «ahora», aunque el servidor diga `lecturaOk: true`.
        expect(cifrasVigentes(tablero({ lecturaOk: true, tomadoEn: "2026-11-17T16:57:00Z" }), AHORA)).toBe(false);
        expect(cifrasVigentes(tablero({ tomadoEn: "2026-11-17T17:01:00Z" }), AHORA)).toBe(true);
        expect(cifrasVigentes(tablero({ tomadoEn: "2026-11-17T17:00:59Z" }), AHORA)).toBe(false);
        expect(cifrasVigentes(tablero({ tomadoEn: null }), AHORA)).toBe(false);
        expect(lecturaReciente("no es una fecha", AHORA)).toBe(false);
    });

    it("el detalle técnico del servidor solo lo lee quien administra la instalación", () => {
        const degradado = estado({ estado: "degradado", detalle: "texto-secreto" });
        expect(lineaEstado(degradado, AHORA).detalle).not.toContain("texto-secreto");
        expect(lineaEstado(degradado, AHORA, false).detalle).not.toContain("texto-secreto");
        expect(lineaEstado(degradado, AHORA, true).detalle).toContain("texto-secreto");
        expect(lineaEstado(degradado, AHORA).titulo).toBe("Con problemas");
    });

    it("la tarjeta de Integraciones dice cuándo se le vio por última vez, sin afirmar que la lectura es de ahora", () => {
        expect(lineaTarjeta({ estado: "listo", version: "0.1.0", ultimoLatidoEn: "2026-11-17T16:42:00Z" }, AHORA)).toEqual({ tono: "ok", titulo: "En línea", detalle: "versión 0.1.0 · visto por última vez hoy a las 10:42 (hace 20 min)" });
        expect(lineaTarjeta({ estado: "sin_enlazar", version: null, ultimoLatidoEn: null }, AHORA).detalle).toBe("todavía no ha respondido");
    });

    it("un tipo de aviso que el panel no conoce no tumba la pantalla", () => {
        for (const tipo of ["nuevo_tipo", "constructor", "toString"]) {
            const t = textoAviso({ tipo: tipo as AvisoServidor["tipo"], activo: true, desde: "", hasta: null, valor: 3, umbral: 1 });
            expect(t.titulo).toBe("Aviso del servidor de IA");
            expect(typeof t.detalle).toBe("string");
        }
    });

    it("la compatibilidad se explica en los dos sentidos", () => {
        // Solo `iris_antiguo` pide actualizar Iris; los otros tres, el servidor, cada uno con su porqué.
        for (const [motivo, texto] of [
            ["paquete_antiguo", "más antiguo que el mínimo"],
            ["iris_nuevo", "más nueva que lo que Iris Voz 0.0.9 sabe atender"],
            ["protocolo", "no hablan la misma versión del protocolo"],
        ] as const) {
            const servidor = queActualizar({ version: "0.0.9", motivoIncompatible: motivo, versionDisponible: null });
            expect(servidor.titulo).toBe("Hay que actualizar Iris Voz en el servidor");
            expect(servidor.comando).toBe("sudo iris-voz upgrade");
            expect(servidor.detalle).toContain(texto);
            // Sin `versionDisponible` no se dice a cuál versión.
            expect(servidor.detalle).not.toContain("o una más nueva");
        }
        expect(queActualizar({ version: "0.0.9", motivoIncompatible: "paquete_antiguo", versionDisponible: "0.1.0" }).detalle).toContain("a la 0.1.0 o una más nueva");
        const iris = queActualizar({ version: "0.3.0", motivoIncompatible: "iris_antiguo", versionDisponible: null });
        expect(iris.titulo).toBe("Hay que actualizar Iris");
        expect(iris.comando).toBeNull();
    });

    it("una tarjeta gráfica: ámbar al acercarse al umbral, rojo al pasarlo, y nada inventado si falta un dato", () => {
        expect(tonoFrente(70, 85)).toBe("normal");
        expect(tonoFrente(78, 85)).toBe("aviso");
        expect(tonoFrente(85, 85)).toBe("error");
        const t = tarjetaGpu({ indice: 0, nombre: "RTX", usoPct: 63, vramUsadaMb: 14_336, vramTotalMb: 24_564, temperaturaC: 86, potenciaW: null }, DE_FABRICA);
        expect(t.uso?.valor).toBe("63 %");
        expect(t.memoria?.detalle).toBe("58 % · quedan 10.0 GB");
        expect(t.temperatura).toMatchObject({ valor: "86", tono: "error", detalle: "Alta · avisa a los 85 °C" });
        const vacia = tarjetaGpu({ indice: 0, nombre: "RTX", usoPct: null, vramUsadaMb: null, vramTotalMb: null, temperaturaC: null, potenciaW: null }, DE_FABRICA);
        expect(vacia).toMatchObject({ uso: null, memoria: null, temperatura: null });
    });

    it("las series conservan los huecos como `null` y saben si hubo tarjeta gráfica", () => {
        const puntos = [
            { t: "2026-11-17T16:00:00Z", usoPct: null, vramPct: null, peticionesMin: 4, latenciaMediaMs: 1_500, latenciaP95Ms: 3_000 },
            { t: "2026-11-17T16:01:00Z", usoPct: null, vramPct: null, peticionesMin: null, latenciaMediaMs: null, latenciaP95Ms: null },
        ];
        const s = seriesDe(puntos, "1h");
        expect(s.hayGpu).toBe(false);
        expect(s.peticiones).toEqual([4, null]);
        expect(s.media).toEqual([1.5, null]);
        expect(s.instantes).toEqual(["10:00", "10:01"]);
        expect(seriesDe(puntos, "ahora").marcas).toEqual(["hace 10 min", "hace 5 min", "ahora"]);
    });

    it("errores, avisos y fallos del enlace en llano", () => {
        expect(textoError("ocupado", 1)).toEqual({ titulo: "El servidor estaba lleno y la rechazó", veces: "1 vez" });
        expect(textoError("tiempo_agotado", 4).veces).toBe("4 veces");
        const saturacion = textoAviso({ tipo: "saturacion", activo: true, desde: "", hasta: null, valor: 12, umbral: 10 });
        expect(saturacion.titulo).toBe("Saturación");
        expect(saturacion.detalle).toContain("Se rechazaron 12 peticiones");
        expect(saturacion.detalle).toContain("No hay cola");
        // 401: no se distingue código vencido, usado o mal escrito (el servidor no lo dice).
        expect(textoErrorEnlace("no_autorizado", 401)).toContain("no aceptó el código");
        expect(textoErrorEnlace("motor_no_responde", 502)).toContain("no pudo llegar al servidor");
        // Al conectar, la misma respuesta vale para un código malo: el texto no afirma cuál fue.
        expect(textoErrorEnlace("motor_no_responde", 502, true)).toContain("Puede que el código esté mal escrito allá");
        expect(textoErrorEnlace("enlace_vencido", 409)).toContain("ya venció");
        expect(textoErrorEnlace("huella_cambiada", 409)).toBe("El servidor cambió desde que lo comparaste; vuelve a conectar y compara de nuevo.");
        expect(textoErrorEnlace("rotacion_en_curso", 409)).toBe("Ya hay un cambio de llave en curso; espera unos segundos.");
        expect(textoErrorEnlace("otro", 500)).toBeNull();
        // Los seis tipos de episodio tienen su título propio.
        const tipos = ["caido", "incompatible", "vram", "disco", "temperatura", "saturacion"] as const;
        const titulos = tipos.map((tipo) => textoAviso({ tipo, activo: true, desde: "", hasta: null, valor: null, umbral: null }).titulo);
        expect(new Set(titulos).size).toBe(6);
        expect(titulos).not.toContain("Aviso del servidor de IA");
        expect(textoPlazas(6, 4)).toContain("más empresas usando el servidor que turnos disponibles (6 empresas y 4 turnos)");
        expect(textoPlazas(undefined, null)).not.toContain("(");
        expect(huellaLegible("3fa91c07b2de")).toEqual(["3FA9", "1C07", "B2DE"]);
    });
});

describe("comandos de iris-voz que se copian", () => {
    it("van siempre en inglés, como el instalador: nunca en español", () => {
        expect(COMANDO).toEqual({
            enlazar: "sudo iris-voz pair",
            desenlazar: "sudo iris-voz unpair",
            estado: "sudo iris-voz status",
            actualizar: "sudo iris-voz upgrade",
            modeloDescargar: "sudo iris-voz model pull <nombre>",
            modeloPermitir: "sudo iris-voz model allow <nombre>",
            modeloQuitar: "sudo iris-voz model remove <nombre>",
        });
        const enEspanol = /iris-voz (instalar|actualizar|enlazar|desenlazar|estado|renovar-certificado|modelo|registros|parar|arrancar|ayuda)\b/;
        for (const comando of Object.values(COMANDO)) expect(comando).not.toMatch(enEspanol);
        // También dentro de los textos de error que nombran un comando.
        for (const texto of [textoErrorEnlace("x", 401), textoErrorEnlace("motor_no_responde", 502, true), textoErrorEnlace("motor_incompatible", 422)]) {
            expect(texto).not.toMatch(enEspanol);
            expect(texto).toMatch(/iris-voz (pair|upgrade)/);
        }
    });
});

describe("umbrales y formularios del Servidor de IA", () => {
    it("los de fábrica: 95 % de memoria de video, 90 % de disco, 85 °C, 10 rechazos en 5 minutos y 7 y 90 días de historial", () => {
        expect(DE_FABRICA).toEqual({ vramPct: 95, vramMinutos: 5, discoPct: 90, temperaturaC: 85, temperaturaMinutos: 5, saturacionRechazos: 10, saturacionMinutos: 5, retencionDetalleDias: 7, retencionHoraDias: 90 });
        const v = valoresDeUmbrales(DE_FABRICA);
        expect(esDeFabrica(v)).toBe(true);
        expect(esDeFabrica({ ...v, temperaturaC: "80" })).toBe(false);
        expect(entradaDeUmbrales({ ...v, temperaturaC: "80" }, 7)).toMatchObject({ version: 7, temperaturaC: 80, vramPct: 95 });
    });

    it("los rangos son los del contrato", () => {
        const v = valoresDeUmbrales(DE_FABRICA);
        const falla = (cambio: Partial<typeof v>) => !esquemaUmbrales.safeParse({ ...v, ...cambio }).success;
        expect(falla({})).toBe(false);
        expect(falla({ vramPct: "49" })).toBe(true);
        expect(falla({ vramPct: "100" })).toBe(false);
        expect(falla({ discoPct: "100" })).toBe(true);
        expect(falla({ temperaturaC: "59" })).toBe(true);
        expect(falla({ temperaturaC: "101" })).toBe(true);
        expect(falla({ saturacionRechazos: "0" })).toBe(true);
        expect(falla({ saturacionRechazos: "1000" })).toBe(false);
        expect(falla({ saturacionMinutos: "61" })).toBe(true);
        expect(falla({ vramMinutos: "cinco" })).toBe(true);
        expect(falla({ retencionDetalleDias: "31" })).toBe(true);
        expect(falla({ retencionHoraDias: "6" })).toBe(true);
        expect(falla({ retencionHoraDias: "365" })).toBe(false);
    });

    it("«solo ciertas empresas» pide al menos una; la dirección del enlace es https sin ruta", () => {
        expect(esquemaEmpresas.safeParse({ quienes: "algunas", empresas: [], simultaneasPorEmpresa: "2" }).success).toBe(false);
        expect(esquemaEmpresas.safeParse({ quienes: "todas", empresas: [], simultaneasPorEmpresa: "2" }).success).toBe(true);
        expect(esquemaEmpresas.safeParse({ quienes: "algunas", empresas: [10], simultaneasPorEmpresa: "17" }).success).toBe(false);
        const url = (u: string) => esquemaEnlace.safeParse({ nombre: "Servidor de IA", url: u }).success;
        expect(url("https://10.0.0.5:8443")).toBe(true);
        expect(url("http://10.0.0.5:8443")).toBe(false);
        expect(url("https://usuario:clave@10.0.0.5:8443")).toBe(false);
        expect(url("https://10.0.0.5:8443/ruta")).toBe(false);
    });
});
