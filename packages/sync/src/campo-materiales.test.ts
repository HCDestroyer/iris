import { describe, expect, it } from "vitest";
import { buscarMateriales, cantidadDeTexto, detalleMaterial, existenciaDespues, nombreBodegaCampo, normalizarBusqueda, textoCantidad, tipoMaterial } from "./campo";
import type { Existencia } from "./index";

const cable: Existencia = { productoId: 900, descripcion: "Cable drop 1 hilo", codigo: "CAB-DROP-1H", cantidad: 120, unidad: "m", modalidad: "consumible_gasto" };
const conector: Existencia = { productoId: 901, descripcion: "Conector SC/APC", codigo: "CON-SCAPC", cantidad: 10, unidad: "u", modalidad: "consumible_cobro" };
const onu: Existencia = { productoId: 902, descripcion: "ONU ZTE F670L", codigo: "ONU-F670", cantidad: 2, unidad: "u", modalidad: "comodato" };
const cinta: Existencia = { productoId: 903, descripcion: "Cinta aislante", cantidad: 0, unidad: "u", modalidad: "" };
const lineas = [onu, cinta, conector, cable];

describe("paso de materiales", () => {
    it("busca por nombre o código, sin tildes ni mayúsculas", () => {
        expect(normalizarBusqueda("  Cáblé   DROP ")).toBe("cable drop");
        expect(buscarMateriales(lineas, "cable").map((l) => l.productoId)).toEqual([900]);
        expect(buscarMateriales(lineas, "CON-SC").map((l) => l.productoId)).toEqual([901]);
        expect(buscarMateriales(lineas, "drop hilo").map((l) => l.productoId)).toEqual([900]);
        expect(buscarMateriales(lineas, "f670").map((l) => l.productoId)).toEqual([902]);
        expect(buscarMateriales(lineas, "nada")).toEqual([]);
    });

    it("sin texto: con existencia primero y en orden alfabético; los que empiezan igual van antes", () => {
        expect(buscarMateriales(lineas, "").map((l) => l.descripcion)).toEqual(["Cable drop 1 hilo", "Conector SC/APC", "ONU ZTE F670L", "Cinta aislante"]);
        // «c»: los que empiezan con c, con existencia primero (la ONU no tiene «c»).
        expect(buscarMateriales(lineas, "c").map((l) => l.productoId)).toEqual([900, 901, 903]);
    });

    it("modalidad: el comodato no se registra como material (va con su serie en Equipos)", () => {
        expect(tipoMaterial("comodato")).toMatchObject({ registrable: false, texto: "Equipo con serie", tono: "info" });
        expect(tipoMaterial("consumible_cobro")).toMatchObject({ registrable: true, texto: "Se cobra al cliente", tono: "warning" });
        expect(tipoMaterial("consumible_gasto")).toMatchObject({ registrable: true, texto: "Consumible" });
        expect(tipoMaterial("")).toMatchObject({ registrable: true, texto: "" });
        expect(tipoMaterial(undefined).registrable).toBe(true);
    });

    it("textos de la línea y de la bodega", () => {
        expect(textoCantidad(2.5, "m")).toBe("2.5 m");
        expect(textoCantidad(3, "")).toBe("3");
        expect(detalleMaterial(cable)).toBe("CAB-DROP-1H · Quedan 120 m · Consumible");
        expect(detalleMaterial(cinta)).toBe("Sin existencia");
        expect(nombreBodegaCampo({ bodegaId: 30, nombre: "Bodega Norte" })).toBe("Bodega Norte");
        expect(nombreBodegaCampo({ bodegaId: 30 })).toBe("Bodega 30");
    });

    it("lo que quedaría con la cantidad elegida, y si excede", () => {
        expect(cantidadDeTexto("")).toBeNull();
        expect(cantidadDeTexto("0")).toBeNull();
        expect(cantidadDeTexto("2,5")).toBe(2.5);
        expect(existenciaDespues(cable, "35")).toEqual({ quedan: 85, excede: false });
        expect(existenciaDespues(conector, "10,5")).toEqual({ quedan: -0.5, excede: true });
        expect(existenciaDespues(cable, "")).toBeNull();
    });
});
