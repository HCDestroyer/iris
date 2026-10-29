// Configuración dinámica sobre app.json: la versión y el versionCode los pone el CI (workflow
// `movil-apk.yml`), nunca a mano. Sin variables (desarrollo) quedan los de app.json y versionCode 1.
//   IRIS_MOVIL_VERSION       → `version` (versionName de Android), p. ej. 0.3.0 o 0.0.0-dev+abc1234
//   IRIS_MOVIL_VERSION_CODE  → `android.versionCode` (entero creciente: github.run_number)

/** @param {{ config: import('expo/config').ExpoConfig }} ctx */
module.exports = ({ config }) => {
    const version = process.env.IRIS_MOVIL_VERSION || config.version;
    const codigo = Number(process.env.IRIS_MOVIL_VERSION_CODE || 1);
    if (!Number.isInteger(codigo) || codigo < 1 || codigo > 2100000000) {
        throw new Error(`IRIS_MOVIL_VERSION_CODE debe ser un entero entre 1 y 2100000000 (vale «${process.env.IRIS_MOVIL_VERSION_CODE}»).`);
    }
    return { ...config, version, android: { ...config.android, versionCode: codigo } };
};
