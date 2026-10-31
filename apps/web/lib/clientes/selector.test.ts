import { describe, expect, it } from "vitest";
import {
    agregarReciente,
    claveRecientes,
    esIP,
    estadoServicio,
    MAX_RECIENTES,
    moverIndice,
    nombresEmpresas,
    ordenarServicios,
    limpiarClientesRecientes,
    soloMinimo,
    sonRecientes,
    type ClienteReciente,
    type ClienteResumen,
    type ServicioCliente,
} from "./selector";

const cliente = (id: number, extra: Partial<ClienteResumen> = {}): ClienteResumen => ({
    id,
    empresaId: 1,
    empresas: [1],
    codigo: `C-${id}`,
    nombre: `Cliente ${id}`,
    telefono: "5555-0000",
    direccion: "Zona 1",
    lat: null,
    lon: null,
    zonaId: null,
    estado: "activo",
    tipo: "persona",
    tipoDocumentoFiscal: "",
    nitCui: "",
    nitCuiMostrar: "",
    movil: "",
    correo: "",
    telefonos: [],
    zonaNombre: "",
    sincronizadoEn: null,
    ...extra,
});

const servicio = (id: number, estado: string): ServicioCliente => ({
    id,
    empresaId: 1,
    clienteId: 1,
    codigo: `S-${id}`,
    planId: null,
    planNombre: "Fibra",
    routerId: null,
    routerNombre: "",
    zonaId: null,
    zonaNombre: "",
    ip: "",
    direccion: "",
    lat: null,
    lon: null,
    estado,
    userPpp: "",
    tipoConexion: "",
    sincronizadoEn: "2026-10-27T20:15:00Z",
});

describe("selector de cliente", () => {
    it("reconoce IPv4 e IPv6 y no confunde teléfonos ni códigos", () => {
        expect(esIP("10.0.0.41")).toBe(true);
        expect(esIP(" 192.168.1.254 ")).toBe(true);
        expect(esIP("2001:db8::1")).toBe(true);
        expect(esIP("5555-0000")).toBe(false);
        expect(esIP("10.0.0")).toBe(false);
        expect(esIP("C-41")).toBe(false);
        expect(esIP("300.1.1.1")).toBe(false);
    });

    it("estado del servicio con tono y texto legible", () => {
        expect(estadoServicio("activo")).toEqual({ tono: "success", texto: "Activo" });
        expect(estadoServicio("suspendido")).toEqual({ tono: "warning", texto: "Suspendido" });
        expect(estadoServicio("pendiente_instalacion")).toEqual({ tono: "info", texto: "Pendiente instalacion" });
        expect(estadoServicio("raro").tono).toBe("neutral");
        expect(estadoServicio("").texto).toBe("Sin estado");
    });

    it("ordena los vigentes primero", () => {
        expect(ordenarServicios([servicio(3, "retirado"), servicio(2, "activo"), servicio(1, "suspendido")]).map((s) => s.id)).toEqual([1, 2, 3]);
    });

    it("recientes: solo id, código y nombre, al principio, sin repetir, con tope y validados", () => {
        let lista: ClienteReciente[] = [];
        for (let i = 1; i <= MAX_RECIENTES + 2; i += 1) lista = agregarReciente(lista, cliente(i));
        expect(lista).toHaveLength(MAX_RECIENTES);
        expect(lista[0]).toEqual({ id: MAX_RECIENTES + 2, codigo: `C-${MAX_RECIENTES + 2}`, nombre: `Cliente ${MAX_RECIENTES + 2}` });
        expect(JSON.stringify(lista)).not.toMatch(/telefono|direccion|servicios|5555/);
        lista = agregarReciente(lista, cliente(5));
        expect(lista.map((c) => c.id).slice(0, 2)).toEqual([5, MAX_RECIENTES + 2]);
        expect(new Set(lista.map((c) => c.id)).size).toBe(lista.length);
        expect(sonRecientes(lista)).toBe(true);
        expect(sonRecientes([{ id: "x" }])).toBe(false);
        expect(sonRecientes("basura")).toBe(false);
        expect(soloMinimo([{ ...cliente(9) }])).toEqual([{ id: 9, codigo: "C-9", nombre: "Cliente 9" }]);
        expect(claveRecientes(7, 2)).toBe("iris:clientes-recientes:7:2");
        expect(claveRecientes(7, null)).toBe("iris:clientes-recientes:7:0");
    });

    it("limpiarClientesRecientes borra solo las claves de recientes", () => {
        const datos = new Map<string, string>([
            ["iris:clientes-recientes:7:2", "[]"],
            ["iris:clientes-recientes:8:0", "[]"],
            ["iris:empresa", "2"],
        ]);
        const almacen = {
            get length() {
                return datos.size;
            },
            key: (i: number) => [...datos.keys()][i] ?? null,
            removeItem: (k: string) => void datos.delete(k),
        };
        limpiarClientesRecientes(almacen);
        expect([...datos.keys()]).toEqual(["iris:empresa"]);
    });

    it("teclado: flechas con vuelta, Inicio y Fin", () => {
        expect(moverIndice(-1, 3, "ArrowDown")).toBe(0);
        expect(moverIndice(2, 3, "ArrowDown")).toBe(0);
        expect(moverIndice(0, 3, "ArrowUp")).toBe(2);
        expect(moverIndice(1, 3, "Home")).toBe(0);
        expect(moverIndice(1, 3, "End")).toBe(2);
        expect(moverIndice(1, 3, "a")).toBeNull();
        expect(moverIndice(0, 0, "ArrowDown")).toBeNull();
    });

    it("nombres de las empresas del cliente", () => {
        const empresas = [
            { id: 1, nombre: "Cable Norte" },
            { id: 2, nombre: "Fibra Sur" },
        ];
        expect(nombresEmpresas(cliente(1, { empresas: [1, 2] }), empresas)).toBe("Cable Norte, Fibra Sur");
        expect(nombresEmpresas(cliente(1, { empresas: null, empresaId: 2 }), empresas)).toBe("Fibra Sur");
        expect(nombresEmpresas(cliente(1, { empresas: [], empresaId: 0 }), empresas)).toBeNull();
    });
});
