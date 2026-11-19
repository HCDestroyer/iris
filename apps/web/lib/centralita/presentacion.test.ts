import { describe, expect, it } from "vitest";
import type { Llamada } from "@/lib/voz/tipos";
import { cambiaDestino, esquemaConexion, estadoConexion, huellaLegible, lineaLlamada, quienCentralita, textoPrueba, valoresConexion } from "./presentacion";
import type { Centralita } from "./tipos";

const centralita: Centralita = {
    id: 1, nombre: "Oficina", tipo: "issabel", host: "10.20.0.5", puerto: 5039, usuario: "iris_lectura", secretoGuardadoEn: "2026-11-19T10:00:00Z",
    huellaTls: "4f9a", huellaVista: null, modo: "lectura", estado: "conectada", errorCodigo: null, probadaEn: null, conectadaEn: null,
    desconectadaDesde: null, ultimoEventoEn: null, activa: true, version: 3,
};

const llamada = (extra: Partial<Llamada>): Llamada =>
    ({
        id: "0190", origen: "centralita", direccion: "entrante", numero: "+50255123480", titulo: "+50255123480", contacto: { tipo: "desconocido" },
        estado: "terminada", tramos: [], sonoEn: "2026-11-19T16:31:00Z", puede: { contestar: false, rechazar: false, colgar: false, tomar: false, transferir: false, anotar: true, escucharGrabacion: false },
        version: 1, centralita: { id: 1, receptores: [{ usuarioId: 2, nombre: "Ana", extension: "204" }], coincidencias: 0, candidatos: [], asociar: true },
        ...extra,
    }) as Llamada;

describe("estado de la conexión", () => {
    it("dice en llano cada estado y solo pide actuar cuando hace falta", () => {
        expect(estadoConexion(centralita, 0)).toMatchObject({ tono: "success", texto: "Conectada", actuar: false });
        expect(estadoConexion({ ...centralita, activa: false, estado: "desactivada" }, 0).texto).toBe("Apagada");
        const caida = estadoConexion({ ...centralita, estado: "desconectada", desconectadaDesde: "2026-11-19T16:42:00Z" }, Date.parse("2026-11-19T16:43:00Z"));
        expect(caida.detalle).toContain("las llamadas de ese rato no quedan en Iris");
        expect(caida.actuar).toBe(false);
        expect(estadoConexion({ ...centralita, estado: "error", errorCodigo: "huella_cambiada" }, 0)).toMatchObject({ tono: "error", actuar: true });
    });
});

describe("prueba y huella", () => {
    it("formatea la huella como openssl", () => {
        expect(huellaLegible("4f9a21c0")).toBe("4F:9A:21:C0");
        expect(huellaLegible(null)).toBe("");
    });
    it("explica cada resultado sin códigos", () => {
        expect(textoPrueba({ estado: "ok", accionesDeMas: [], huella: "ab", version: null }).listo).toBe(true);
        expect(textoPrueba({ estado: "permisos_de_mas", accionesDeMas: ["Originate"], huella: null, version: null }).detalle).toContain("Originate");
        expect(textoPrueba({ estado: "no_conecta", accionesDeMas: [], huella: null, version: null }).titulo).toBe("No se pudo conectar");
    });
});

describe("formulario de conexión", () => {
    it("pide la contraseña al crear y no acepta http:// ni puerto en la dirección", () => {
        const v = valoresConexion(null);
        expect(v.cambiarSecreto).toBe(true);
        expect(esquemaConexion.safeParse({ ...v, nombre: "O", host: "http://10.0.0.1", usuario: "iris", secreto: "contrasena" }).success).toBe(false);
        expect(esquemaConexion.safeParse({ ...v, nombre: "O", host: "10.0.0.1:5039", usuario: "iris", secreto: "contrasena" }).success).toBe(false);
        expect(esquemaConexion.safeParse({ ...v, nombre: "O", host: "10.0.0.1", usuario: "iris", secreto: "corta" }).success).toBe(false);
        expect(esquemaConexion.safeParse({ ...v, nombre: "O", host: "10.0.0.1", usuario: "iris", secreto: "contrasena-larga" }).success).toBe(true);
        expect(esquemaConexion.safeParse({ ...v, nombre: "O", host: "10.0.0.1", usuario: "iris", secreto: "a\nAction: Originate" }).success).toBe(false);
    });
    it("con otro destino la contraseña guardada no sirve", () => {
        expect(cambiaDestino({ host: "10.20.0.5", puerto: 5039, usuario: "iris_lectura" }, centralita)).toBe(false);
        expect(cambiaDestino({ host: "10.20.0.6", puerto: 5039, usuario: "iris_lectura" }, centralita)).toBe(true);
        expect(cambiaDestino({ host: "10.20.0.5", puerto: 5039, usuario: "otro" }, centralita)).toBe(true);
    });
});

describe("llamadas de la centralita", () => {
    it("arma la línea del historial", () => {
        expect(lineaLlamada(llamada({ usuario: { id: 2, nombre: "Ana" }, duracionSeg: 240, ticketId: 4812, resultado: "resuelto", centralita: { id: 1, extension: "204", receptores: [], coincidencias: 1, candidatos: [], asociar: true } })))
            .toMatchObject({ tipo: "entrante", titulo: "Contestó Ana · 4 min" });
        expect(lineaLlamada(llamada({ estado: "perdida" })).detalle).toContain("sonó en 204");
        expect(lineaLlamada(llamada({ direccion: "saliente", usuario: { id: 2, nombre: "Ana" } })).titulo).toBe("Ana llamó");
    });
    it("nunca inventa un nombre", () => {
        expect(quienCentralita(llamada({}))).toMatchObject({ nombre: "Número desconocido", conocido: false });
        expect(quienCentralita(llamada({ numero: "", titulo: "Número oculto" })).nombre).toBe("Número oculto");
        expect(quienCentralita(llamada({ centralita: { id: 1, receptores: [], coincidencias: 2, candidatos: [], asociar: true } }))).toMatchObject({ varios: true });
        expect(quienCentralita(llamada({ titulo: "María", contacto: { tipo: "cliente", clienteId: 7 } })).nombre).toBe("María");
    });
});
