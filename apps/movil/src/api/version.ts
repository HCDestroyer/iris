// Versión de la app nativa para `X-Iris-App: campo/<versión> (<plataforma>)`. El CI publica la
// versión del APK en IRIS_MOVIL_VERSION (app.config.js); para que llegue al bundle hay que pasarla
// también como EXPO_PUBLIC_IRIS_MOVIL_VERSION. Sin ella, la de app.json.
import app from "../../app.json";

export const VERSION_APP = process.env.EXPO_PUBLIC_IRIS_MOVIL_VERSION?.trim() || app.expo.version;
