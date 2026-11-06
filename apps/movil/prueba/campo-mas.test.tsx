// Botón «+» de la app nativa (diseño aprobado por el PM, nov 2026), como la PWA: la hoja con
// «Agendar tarea propia» y «Tomar una orden libre» (contador; oculta si la empresa no lo permite o
// sin permiso), «Nueva tarea» sin señal (aparece en Hoy con borde punteado y se marca hecha) y
// «Órdenes libres» con confirmación, aviso emergente y el aviso si otro la tomó primero. Motor real
// sobre memoria, claro y oscuro.
import type { OrdenLibre } from "@iris/sync";
import { hoyEnGuatemala } from "@iris/sync/campo";
import { montar, ordenDePrueba, paqueteDePrueba } from "@iris/sync/pruebas";
import {
    fireEvent,
    screen,
    waitFor,
    within,
} from "@testing-library/react-native";
import { abrirCampo, avisoArriba, LibresFalso, sesion, tipos } from "./arnes";

jest.mock("expo-camera", () => ({
    CameraView: () => null,
    useCameraPermissions: () => [null, jest.fn()],
}));

async function montaje(tomarLibres: boolean) {
    const m = await montar({}, false);
    m.transporte.paquete = (fecha) => ({
        ...paqueteDePrueba(fecha),
        ruta: undefined as never,
        ordenes: [
            ordenDePrueba(1, {
                tipo: "visita",
                estado: "programada",
                contacto: { nombre: "Jorge Méndez", telefono: "5555-0001" },
            }),
        ],
        tareas: [],
        tomarLibres,
    });
    await m.motor.cargarPaquete(paqueteDePrueba().fecha);
    return m;
}

function libres(): LibresFalso {
    const l = new LibresFalso();
    const libre = (
        id: number,
        nombre: string,
        distanciaM: number,
    ): OrdenLibre => ({
        ...ordenDePrueba(id, { estado: "pendiente", tecnicoId: null }),
        clienteNombre: nombre,
        distanciaM,
    });
    l.ordenes = [
        libre(120, "Luis Herrera", 1200),
        libre(118, "Carmen Ruiz", 2800),
    ];
    l.tomadasPorOtro.set(118, "Andrea López");
    return l;
}

describe.each(["light", "dark"] as const)("«+» (%s)", (esquema) => {
    it("la hoja ofrece tarea propia y orden libre con su contador; no «Agregar formulario»", async () => {
        const m = await montaje(true);
        await abrirCampo(m, { esquema, libres: libres() });
        await fireEvent.press(
            screen.getByRole("button", { name: "Acciones rápidas" }),
        );
        expect(
            await screen.findByRole("header", { name: "¿Qué quieres hacer?" }),
        ).toBeTruthy();
        expect(screen.getByTestId("accion-tarea")).toHaveTextContent(
            /Agendar tarea propia/,
        );
        expect(
            await screen.findByRole("button", {
                name: "Tomar una orden libre (2)",
            }),
        ).toBeTruthy();
        expect(screen.queryByText("Agregar formulario")).toBeNull();
    });

    it("con el ajuste apagado o sin ordenes.tomar, solo la tarea propia", async () => {
        const m = await montaje(false);
        await abrirCampo(m, { esquema });
        await fireEvent.press(
            screen.getByRole("button", { name: "Acciones rápidas" }),
        );
        expect(await screen.findByTestId("accion-tarea")).toBeTruthy();
        expect(screen.queryByTestId("accion-libres")).toBeNull();

        const m2 = await montaje(true);
        await abrirCampo(m2, {
            esquema,
            sesion: {
                ...sesion,
                permisos: (sesion.permisos ?? []).filter(
                    (p) => p !== "ordenes.tomar",
                ),
            },
        });
        await fireEvent.press(
            screen
                .getAllByRole("button", { name: "Acciones rápidas" })
                .at(-1) ?? screen.getByTestId("boton-mas"),
        );
        await waitFor(() =>
            expect(screen.queryAllByTestId("accion-libres")).toHaveLength(0),
        );
    });

    it("Nueva tarea sin señal: valida, aparece en Hoy con borde punteado y se marca hecha", async () => {
        // El reloj corre desde las 10:00 de hoy en Guatemala: desde las 23:30 el formulario propone
        // la siguiente media hora, que ya es de mañana (la tarea no caería en Hoy).
        const real = Date.now.bind(Date);
        const hoy = new Intl.DateTimeFormat("en-CA", { timeZone: "America/Guatemala", year: "numeric", month: "2-digit", day: "2-digit" }).format(real());
        const desfase = Date.parse(`${hoy}T10:00:00-06:00`) - real();
        const reloj = jest.spyOn(Date, "now").mockImplementation(() => real() + desfase);
        try {
            await nuevaTareaSinSenal();
        } finally {
            reloj.mockRestore();
        }
    });

    async function nuevaTareaSinSenal() {
        const m = await montaje(false);
        m.transporte.sinRed = true;
        await abrirCampo(m, {
            esquema,
            enLinea: () => false,
            vista: { tipo: "nueva_tarea" },
        });
        expect(
            screen.getByRole("header", { name: "Nueva tarea" }),
        ).toBeTruthy();
        await fireEvent.press(screen.getByTestId("agendar"));
        expect(await screen.findByText("Escribe qué vas a hacer")).toBeTruthy();
        await fireEvent.changeText(
            screen.getByTestId("tarea-que"),
            "Recoger material en bodega central",
        );
        await fireEvent.changeText(screen.getByTestId("tarea-hora"), "23:30");
        await fireEvent.press(screen.getByTestId("duracion-30"));
        expect(
            screen.getByTestId("duracion-30").props.accessibilityState,
        ).toMatchObject({ selected: true });
        await fireEvent.press(screen.getByTestId("agendar"));
        await waitFor(() => expect(tipos(m)).toEqual(["tarea.crear"]));
        expect(avisoArriba()).toHaveTextContent(/Tarea agendada/);
        const [op] = m.motor.obtenerEstado().cola;
        expect(op).toMatchObject({
            tipo: "tarea.crear",
            datos: {
                que: "Recoger material en bodega central",
                duracionMin: 30,
            },
        });
        const tarjeta = await screen.findByTestId(`tarea-${op?.id}`);
        expect(tarjeta).toHaveStyle({ borderStyle: "dashed" });
        expect(within(tarjeta).getByText("Tarea propia · 30 min")).toBeTruthy();
        await fireEvent.press(within(tarjeta).getByTestId("tarea-hecha"));
        await waitFor(() =>
            expect(tipos(m)).toEqual(["tarea.crear", "tarea.completar"]),
        );
        expect(
            await within(screen.getByTestId(`tarea-${op?.id}`)).findByText(
                "Hecha · 30 min",
            ),
        ).toBeTruthy();
    }

    it("en Hoy la tarea va intercalada estrictamente por hora entre las citas de «Después»", async () => {
        const m = await montar({}, false);
        const hoy = hoyEnGuatemala();
        // 08:30, 11:00 y 13:00 de Guatemala (UTC−6); la tarea, a las 12:00.
        const a = (hora: string) => `${hoy}T${hora}:00-06:00`;
        m.transporte.paquete = (fecha) => ({
            ...paqueteDePrueba(fecha),
            ruta: undefined as never,
            ordenes: [
                ordenDePrueba(1, {
                    estado: "programada",
                    programadaInicio: new Date(a("08:30")).toISOString(),
                    contacto: { nombre: "María Castillo", telefono: null },
                }),
                ordenDePrueba(3, {
                    estado: "programada",
                    programadaInicio: new Date(a("13:00")).toISOString(),
                    contacto: { nombre: "Ana Pérez", telefono: null },
                }),
                ordenDePrueba(2, {
                    estado: "programada",
                    programadaInicio: new Date(a("11:00")).toISOString(),
                    contacto: { nombre: "Jorge Méndez", telefono: null },
                }),
            ],
            tareas: [],
        });
        await m.motor.cargarPaquete(paqueteDePrueba().fecha);
        await m.motor.registrarTarea("tarea.crear", {
            que: "Recoger material",
            inicio: new Date(a("12:00")).toISOString(),
            duracionMin: 30,
        });
        await abrirCampo(m, { esquema });
        const despues = await screen.findByLabelText("Después");
        const horas = within(despues)
            .getAllByText(/^\d{2}:\d{2}$/)
            .map((t) => t.props.children as string);
        expect(horas).toEqual(["11:00", "12:00", "13:00"]);
    });

    it("Órdenes libres: tomarla con confirmación y aviso; si otro la tomó, se dice quién y sale", async () => {
        const m = await montaje(true);
        const l = libres();
        await abrirCampo(m, { esquema, libres: l, vista: { tipo: "libres" } });
        expect(await screen.findByTestId("libre-120")).toHaveTextContent(
            /Luis Herrera/,
        );
        expect(screen.getByTestId("libre-120")).toHaveTextContent(/1,2 km/);
        expect(
            screen.getByText(
                "Zona Norte · sin técnico asignado · más cercanas primero",
            ),
        ).toBeTruthy();
        await fireEvent.press(screen.getByTestId("tomar-120"));
        expect(
            await screen.findByRole("header", { name: "¿Tomar OT-000120?" }),
        ).toBeTruthy();
        await fireEvent.press(screen.getByTestId("confirmar-tomarla"));
        await waitFor(() => expect(l.tomadas).toEqual([120]));
        await waitFor(() =>
            expect(avisoArriba()).toHaveTextContent(
                /OT-000120 quedó a tu nombre/,
            ),
        );
        await waitFor(() =>
            expect(screen.queryByTestId("libre-120")).toBeNull(),
        );

        await fireEvent.press(screen.getByTestId("tomar-118"));
        await fireEvent.press(await screen.findByTestId("confirmar-tomarla"));
        expect(await screen.findByTestId("ya-tomada")).toHaveTextContent(
            "OT-000118 ya la tomó Andrea López. La quitamos de la lista.",
        );
        await waitFor(() =>
            expect(screen.queryByTestId("libre-118")).toBeNull(),
        );
    });
});
