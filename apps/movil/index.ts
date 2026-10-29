// Entrada de la app nativa. El entorno se configura al cargar el bundle (también cuando el sistema
// despierta la app sin pantalla para la tarea del GPS); ningún módulo lo lee al importarse, solo
// dentro de funciones. La tarea del GPS se define al importar `tarea-ubicacion`.
import { registerRootComponent } from "expo";
import App from "./src/App";
import "./src/campo/tarea-ubicacion";
import { configurarEntorno } from "./src/entorno";
import { entornoNativo } from "./src/entorno-nativo";

configurarEntorno(entornoNativo());
registerRootComponent(App);
