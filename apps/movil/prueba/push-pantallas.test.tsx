// Notificaciones en la app (diseño aprobado por el PM): el primer uso antes del diálogo de Android,
// «Yo» → «Notificaciones» (tipos, contenido con muestra, No molestar y «Ajustes del sistema»; las
// preferencias son de la cuenta y se guardan con `version`), el aviso con el permiso bloqueado, el
// registro al entrar, abrir el destino al tocar una notificación (también la que abrió la app cerrada)
// y no avisar el chat que se está viendo. Claro y oscuro.
import { montar } from "@iris/sync/pruebas";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react-native";
import type { Vista } from "@iris/sync/campo";
import { ErrorApi } from "../src/api/errores";
import type { ApiPush, EntradaPreferenciasPush, PreferenciasPush } from "../src/push/api";
import { leerMensajePush, type MensajePush } from "../src/push/mensaje";
import { RegistroDispositivo } from "../src/push/registro";
import { abrirCampo, paqueteCon } from "./arnes";
import { clavesMemoria } from "./preparar";
import { dataChat, preferencias, ServicioPushFalso } from "./push-falso";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

class ApiFalsa implements ApiPush {
    prefs: PreferenciasPush;
    guardadas: EntradaPreferenciasPush[] = [];
    registros: string[] = [];
    conflicto = false;
    constructor(p: PreferenciasPush = preferencias()) {
        this.prefs = p;
    }
    async registrarDispositivo(empresa: number | null) {
        this.registros.push(`registrar:${empresa ?? "-"}`);
        return { id: 9, plataforma: "android" as const, appVersion: "0.17.0", tokenFin: "tttttt", empresaId: empresa, registradoEn: "", actualizadoEn: "", ultimoEnvioEn: null };
    }
    async quitarDispositivo(_e: number | null, id: number) {
        this.registros.push(`quitar:${id}`);
    }
    async leerPreferencias() {
        return this.prefs;
    }
    async guardarPreferencias(e: EntradaPreferenciasPush) {
        if (this.conflicto) {
            this.conflicto = false;
            this.prefs = { ...this.prefs, version: this.prefs.version + 1 };
            throw new ErrorApi({ estado: 409, codigo: "conflicto_version", titulo: "Conflicto" });
        }
        if (e.version !== this.prefs.version) {
            throw new ErrorApi({ estado: 409, codigo: "conflicto_version", titulo: "Conflicto" });
        }
        this.guardadas.push(e);
        this.prefs = { ...this.prefs, ...e, version: e.version + 1 };
        return this.prefs;
    }
    async responderChat() {}
    async leerChat() {}
    async notaBandeja() {}
    async leerBandeja() {}
}

function mensaje(extra: Record<string, string> = {}): MensajePush {
    const m = leerMensajePush(dataChat(extra));
    if (!m) {
        throw new Error("mensaje inválido");
    }
    return m;
}

async function abrir(esquema: "light" | "dark", o: { servicio?: ServicioPushFalso; api?: ApiFalsa; vista?: Vista } = {}) {
    const m = await montar();
    m.transporte.paquete = paqueteCon({});
    await m.motor.cargarPaquete("2026-10-28");
    const servicio = o.servicio ?? new ServicioPushFalso();
    const api = o.api ?? new ApiFalsa();
    const claves = clavesMemoria();
    const registro = new RegistroDispositivo({ api, claves, obtenerToken: () => servicio.token(), appVersion: "0.17.0", generarClave: () => "k" });
    await abrirCampo(m, { esquema, vista: o.vista ?? { tipo: "yo" }, push: { servicio, api, registro, claves, generarId: () => "0193-id", chat: null } });
    await act(async () => {
        await Promise.resolve();
    });
    return { m, servicio, api, claves };
}

describe.each(["light", "dark"] as const)("notificaciones push (%s)", (esquema) => {
    it("al entrar con el permiso concedido registra el teléfono; con un token nuevo, otra vez", async () => {
        const { servicio, api } = await abrir(esquema);
        await waitFor(() => expect(api.registros).toEqual(["registrar:-"]));
        await act(async () => {
            servicio.rotarToken("n".repeat(120));
        });
        await waitFor(() => expect(api.registros).toEqual(["registrar:-", "registrar:-"]));
    });

    it("primer uso: «Ahora no» no pide el permiso ni vuelve a preguntar; «Activar» abre el diálogo y registra", async () => {
        const servicio = new ServicioPushFalso("sin_preguntar");
        const { api } = await abrir(esquema, { servicio });
        expect(await screen.findByTestId("primer-uso-push")).toBeTruthy();
        expect(screen.getByRole("header", { name: "Que Iris te avise" })).toBeTruthy();
        await fireEvent.press(screen.getByRole("button", { name: "Ahora no" }));
        await waitFor(() => expect(screen.queryByTestId("primer-uso-push")).toBeNull());
        expect(servicio.dialogos).toBe(0);
        expect(api.registros).toEqual([]);
    });

    it("primer uso: «Activar notificaciones» pide el permiso de Android y registra el teléfono", async () => {
        const servicio = new ServicioPushFalso("sin_preguntar");
        const { api } = await abrir(esquema, { servicio });
        await fireEvent.press(await screen.findByRole("button", { name: "Activar notificaciones" }));
        await waitFor(() => expect(servicio.dialogos).toBe(1));
        await waitFor(() => expect(api.registros).toEqual(["registrar:-"]));
        expect(screen.queryByTestId("primer-uso-push")).toBeNull();
    });

    it("con el push del servidor sin configurar no hay primer uso (no se pide el permiso)", async () => {
        const servicio = new ServicioPushFalso("sin_preguntar");
        await abrir(esquema, { servicio, api: new ApiFalsa(preferencias({ push: { estado: "sin_configurar", texto: "Notificaciones push: sin configurar" } })) });
        expect(await screen.findByTestId("fila-notificaciones")).toHaveTextContent(/sin configurar/);
        expect(screen.queryByTestId("primer-uso-push")).toBeNull();
    });

    it("«Yo» → «Notificaciones»: tipos, contenido con muestra y No molestar; cada cambio guarda con la versión leída", async () => {
        await abrir(esquema, { api: new ApiFalsa(preferencias({ noMolestar: { activo: true, desde: "21:00", hasta: "06:00", urgenteIgual: true } })) });
        const fila = await screen.findByTestId("fila-notificaciones");
        expect(fila).toHaveTextContent(/No molestar de 21:00 a 06:00/);
        await fireEvent.press(fila);
        expect(await screen.findByRole("header", { name: "Notificaciones" })).toBeTruthy();
        // Sin `bandejaDisponible` no se ofrece la bandeja.
        expect(screen.queryByLabelText("Bandeja de clientes")).toBeNull();
        expect(screen.getByTestId("muestra-aviso")).toHaveTextContent(/Carlos Ruiz.*ya llegué a la casa/);
        expect(screen.getByLabelText("Mostrar el mensaje")).toHaveProp("accessibilityState", expect.objectContaining({ checked: true }));
    });

    it("guardar: apagar «Menciones», elegir «Solo quién escribe» y cambiar «Desde»", async () => {
        const api = new ApiFalsa();
        await abrir(esquema, { api, vista: { tipo: "yo", seccion: "notificaciones" } });
        await fireEvent(await screen.findByTestId("tipo-mencion"), "selectedChange", false);
        await waitFor(() => expect(api.guardadas).toHaveLength(1));
        expect(api.guardadas[0]).toEqual({
            version: 3,
            tipos: { chat: true, mencion: false, orden: true, bandeja: true },
            contenido: "completo",
            noMolestar: { activo: false, desde: "21:00", hasta: "06:00", urgenteIgual: true },
            zonaHoraria: "America/Guatemala",
        });
        await fireEvent.press(screen.getByLabelText("Solo quién escribe"));
        await waitFor(() => expect(api.guardadas).toHaveLength(2));
        expect(api.guardadas[1]).toMatchObject({ version: 4, contenido: "remitente" });
        expect(await screen.findByTestId("muestra-aviso")).toHaveTextContent("Nuevo mensaje de Carlos Ruiz");

        await fireEvent.press(screen.getByLabelText("Desde 21:00"));
        const campo = await screen.findByTestId("campo-hora");
        await fireEvent.changeText(campo, "06:00");
        await fireEvent.press(within(screen.getByTestId("editar-hora")).getByRole("button", { name: "Guardar" }));
        expect(await screen.findByText("«Desde» y «Hasta» no pueden ser iguales.")).toBeTruthy();
        await fireEvent.changeText(screen.getByTestId("campo-hora"), "22:30");
        await fireEvent.press(within(screen.getByTestId("editar-hora")).getByRole("button", { name: "Guardar" }));
        await waitFor(() => expect(api.guardadas).toHaveLength(3));
        expect(api.guardadas[2]?.noMolestar).toEqual({ activo: false, desde: "22:30", hasta: "06:00", urgenteIgual: true });
    });

    it("con «bandejaDisponible» aparece «Bandeja de clientes»; un 409 vuelve a leer lo de ahora", async () => {
        const api = new ApiFalsa(preferencias({ bandejaDisponible: true }));
        await abrir(esquema, { api, vista: { tipo: "yo", seccion: "notificaciones" } });
        expect(await screen.findByLabelText("Bandeja de clientes")).toBeTruthy();
        api.conflicto = true;
        await fireEvent(screen.getByTestId("tipo-chat"), "selectedChange", false);
        await waitFor(() => expect(screen.getAllByText(/Se cambiaron en otro teléfono/).length).toBeGreaterThan(0));
        expect(api.guardadas).toEqual([]);
    });

    it("permiso bloqueado: el aviso con «Activar» abre los ajustes de Android y los ajustes quedan atenuados", async () => {
        const servicio = new ServicioPushFalso("denegado");
        const { api } = await abrir(esquema, { servicio, vista: { tipo: "yo", seccion: "notificaciones" } });
        const aviso = await screen.findByTestId("aviso-bloqueadas");
        expect(aviso).toHaveTextContent(/Las notificaciones están bloqueadas en este teléfono/);
        await fireEvent.press(within(aviso).getByRole("button", { name: "Activar" }));
        expect(servicio.ajustesAbiertos).toBe(1);
        expect(screen.getByTestId("tipo-chat")).toHaveProp("accessibilityState", expect.objectContaining({ disabled: true }));
        expect(api.registros).toEqual([]);
        await fireEvent.press(screen.getByLabelText("Abrir los ajustes del sistema"));
        expect(servicio.ajustesAbiertos).toBe(2);
    });

    it("la notificación que abrió la app cerrada abre su destino al entrar (la orden) y se quita", async () => {
        const servicio = new ServicioPushFalso();
        servicio.pendiente = { accion: "ver_orden", mensaje: mensaje({ tipo: "orden", canal: "orden", grupo: "orden-1", enlace: "iris://orden/1", acciones: "ver_orden,como_llegar", datos: JSON.stringify({ ordenId: 1, hoy: true }) }) };
        await abrir(esquema, { servicio, vista: { tipo: "lista" } });
        expect(await screen.findByRole("header", { name: "OT-000001" })).toBeTruthy();
        expect(servicio.gruposQuitados).toEqual(["orden-1"]);
    });

    it("una notificación de otra persona, o con un enlace inválido, no abre nada", async () => {
        const servicio = new ServicioPushFalso();
        await abrir(esquema, { servicio, vista: { tipo: "lista" } });
        await act(async () => {
            servicio.toca({ accion: "abrir", mensaje: mensaje({ usuarioId: "77" }) });
            servicio.llega(dataChat({ enlace: "iris://chat/0" }));
        });
        expect(servicio.presentadas).toEqual([]);
        expect(screen.queryByTestId("conversacion-chat")).toBeNull();
    });

    it("con la app al frente arma la notificación, salvo la del chat que se está viendo", async () => {
        const servicio = new ServicioPushFalso();
        await abrir(esquema, { servicio, vista: { tipo: "lista" } });
        await act(async () => {
            servicio.llega(dataChat());
        });
        expect(servicio.presentadas.map((p) => p.grupo)).toEqual(["chat-12"]);
    });
});
