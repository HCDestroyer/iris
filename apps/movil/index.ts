// Entrada de la app nativa. El entorno se configura al cargar el bundle (también cuando el sistema
// despierta la app sin pantalla para la tarea del GPS); ningún módulo lo lee al importarse, solo
// dentro de funciones. Las tareas del GPS y de las notificaciones se definen al importar
// `tarea-ubicacion` y `tarea-push`.
import { registerRootComponent } from "expo";
import App from "./src/App";
import "./src/campo/tarea-ubicacion";
import "./src/push/tarea-push";
import { configurarEntorno } from "./src/entorno";
import { entornoNativo } from "./src/entorno-nativo";

configurarEntorno(entornoNativo());
registerRootComponent(App);
