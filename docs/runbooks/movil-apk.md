# APK de Android de Iris Campo

La app de campo nativa (`apps/movil`, Expo) se compila como APK en GitHub Actions, sin EAS ni
cuentas externas: workflow `.github/workflows/movil-apk.yml` («APK de Android»).

| Cuándo | Versión (`versionName`) | `versionCode` | URL de la API | Dónde queda |
| --- | --- | --- | --- | --- |
| A mano (Run workflow) | `0.0.0-dev+<sha corto>` | número de corrida | input `api_url` | artefacto de la corrida (30 días) |
| Versión de release-please | `X.Y.Z` | número de corrida | variable del repo `IRIS_MOVIL_API_URL` | artefacto + GitHub Release |
| Release publicada a mano | la etiqueta sin `v` | número de corrida | variable del repo `IRIS_MOVIL_API_URL` | artefacto + GitHub Release |

El `versionCode` siempre crece (`github.run_number`), así que un APK nuevo se instala encima del
anterior **si los firma la misma llave**.

## Compilarlo a mano

1. GitHub → **Actions** → **APK de Android** → **Run workflow**.
2. Elige la rama y escribe la URL de la API **con https** y sin `/api/v1` (por ejemplo
   `https://iris.tuisp.gt`). Sin https el job falla al empezar: la app se niega a hablar con la API
   en claro fuera de desarrollo.
3. Al terminar (unos 20–30 min), en la corrida:
    - el **resumen** muestra el nombre del archivo, la versión, la URL, el **SHA-256 del APK** y el
      certificado con que se firmó;
    - en **Artifacts** está el APK (zip con el `.apk` dentro).

## Llave de producción (la crea el dueño del repo, una vez)

Sin llave propia, cada corrida firma con una llave de **prueba** distinta (ver abajo). Para APKs que
se actualicen unos sobre otros hace falta una llave fija:

1. En una máquina de confianza (con JDK):

    ```bash
    keytool -genkeypair -v -keystore iris-campo.jks -storetype PKCS12 \
      -alias iris-campo -keyalg RSA -keysize 4096 -validity 10000 \
      -dname "CN=Iris Campo, O=<tu ISP>, C=GT"
    ```

    Pide dos contraseñas (keystore y llave; pueden ser iguales). **Guarda el `.jks` y las
    contraseñas en el gestor de secretos de la empresa**: si se pierden, los teléfonos tendrán que
    desinstalar la app (y perder lo no enviado) para instalar una firmada con otra llave.
2. Codifícala: `base64 -w0 iris-campo.jks > iris-campo.jks.b64`.
3. GitHub → **Settings → Secrets and variables → Actions → New repository secret**:
    - `ANDROID_KEYSTORE_BASE64`: el contenido de `iris-campo.jks.b64`;
    - `ANDROID_KEYSTORE_PASSWORD`: contraseña del keystore;
    - `ANDROID_KEY_ALIAS`: `iris-campo`;
    - `ANDROID_KEY_PASSWORD`: contraseña de la llave.
4. En la pestaña **Variables**: `IRIS_MOVIL_API_URL` = URL https de la API de producción.
5. Borra `iris-campo.jks.b64` de la máquina. El keystore nunca va al repo (`*.jks` está en
   `apps/movil/.gitignore`); el job lo escribe en `$RUNNER_TEMP` y lo borra al firmar.

## Instalarlo en un teléfono

1. Baja el APK de la release (o del artefacto: descomprime el zip) en el teléfono.
2. Ábrelo. Android pedirá permitir **instalar apps de fuentes desconocidas** para el navegador o el
   gestor de archivos: Ajustes → Apps → (navegador) → Instalar apps desconocidas → Permitir.
3. Instala. Si ya había una versión **firmada con otra llave**, Android dice «La app no se
   instaló» o «conflicto con un paquete existente»: hay que **enviar lo pendiente**, desinstalar la
   anterior y volver a instalar.
4. Al abrir, Iris pide el permiso de ubicación solo después de explicarlo, cuando hay una orden en
   curso (para el GPS en segundo plano: «Permitir todo el tiempo»).

## ¿Es de prueba?

- El nombre termina en `-prueba.apk` (`iris-campo-0.3.0-prueba.apk`).
- El resumen del job lo dice en un recuadro («APK de PRUEBA») y el certificado es
  `CN=Iris Campo PRUEBA`.
- En el teléfono: `apksigner verify --print-certs iris-campo-…apk` (Android SDK) muestra el mismo DN.

Un APK de prueba sirve para probar en un teléfono limpio, **no** para distribuir: cada corrida usa
otra llave, así que no actualiza una instalación anterior.

## Verificar un APK

Compara el SHA-256 del archivo con el del resumen del job: `sha256sum iris-campo-0.3.0.apk`.
