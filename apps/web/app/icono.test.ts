import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, it } from "vitest";

it("el favicon es el logo de la marca (docs/marca/logo-mark.svg)", () => {
    const leer = (ruta: string) => readFileSync(fileURLToPath(new URL(ruta, import.meta.url)), "utf8");
    expect(leer("./icon.svg")).toBe(leer("../../../docs/marca/logo-mark.svg"));
});
