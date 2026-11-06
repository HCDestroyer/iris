// Lo de push que arma la app nativa (Android): el servicio de expo-notifications, la API, el registro
// del teléfono y la tarea en segundo plano. En iOS (aún sin push, `contratos/push.md` §1) no hay
// servicio: la app no pide permiso ni registra.
import { crearGeneradorUuid7 } from "@iris/sync";
import * as Crypto from "expo-crypto";
import { Platform } from "react-native";
import { VERSION_APP } from "../api/version";
import { entorno } from "../entorno";
import { cliente } from "../sesion/sesion";
import { crearApiPush, type ApiPush } from "./api";
import { instalarManejador, servicioPushNativo } from "./nativo";
import { RegistroDispositivo } from "./registro";
import type { ServicioPush } from "./servicio";
import { registrarTareaPush } from "./tarea-push";

const uuid7 = crearGeneradorUuid7(Date.now, (bytes) => {
    Crypto.getRandomValues(bytes);
});

export interface PushNativo {
    servicio: ServicioPush | null;
    api: ApiPush;
    registro: RegistroDispositivo | null;
    baseUrl: string;
    generarId: () => string;
}

export function pushNativo(): PushNativo {
    const api = crearApiPush(cliente);
    if (Platform.OS !== "android") {
        return { servicio: null, api, registro: null, baseUrl: entorno().baseUrl, generarId: uuid7 };
    }
    instalarManejador();
    void registrarTareaPush();
    const servicio = servicioPushNativo(() => entorno().claves);
    const registro = new RegistroDispositivo({ api, claves: entorno().claves, obtenerToken: () => servicio.token(), appVersion: VERSION_APP, generarClave: uuid7 });
    return { servicio, api, registro, baseUrl: entorno().baseUrl, generarId: uuid7 };
}
