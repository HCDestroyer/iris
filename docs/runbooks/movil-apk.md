# APK de Android de Iris Campo

La app de campo nativa (`apps/movil`, Expo) se compila como APK en GitHub Actions, sin EAS ni
cuentas externas: workflow `.github/workflows/movil-apk.yml` («APK de Android»).

| Cuándo | Versión (`versionName`) | Firma | URL de la API | Dónde queda |
| --- | --- | --- | --- | --- |
| A mano desde `main` | `X.Y.Z-dev+<sha corto>` (X.Y.Z = último tag) | real si el environment tiene la llave; si no, prueba | input `api_url` | artefacto (30 días) |
| A mano desde otra rama | igual | **siempre prueba** (no entra al environment) | input `api_url` | artefacto (30 días) |
| Versión de release-please | `X.Y.Z` | real (falla si falta la llave) | variable `IRIS_MOVIL_API_URL` | artefacto + GitHub Release |
| Release publicada a mano | la etiqueta sin `v` | real (falla si falta la llave) | variable `IRIS_MOVIL_API_URL` | artefacto + GitHub Release |

Una versión solo se firma con la llave real si la etiqueta es `vX.Y.Z` y su commit está en `main`.

La compilación (npm, Gradle) corre en un job **sin secretos** y deja el APK sin firmar; otro job,
sin checkout ni dependencias, lo firma con el `apksigner` del runner. Solo ese job entra al
environment `android-firma`.

### versionCode

Un solo origen y siempre creciente, para que un APK nuevo se instale encima del anterior (si los
firma la misma llave):

- Versión `X.Y.Z`: `(X·10000 + Y·100 + Z)·100`. Por ejemplo, 0.3.7 → 30700 y 1.2.0 → 1020000.
- A mano: el de la última etiqueta `vX.Y.Z` alcanzable más `(número de corrida % 99) + 1`. Por
  ejemplo, después de 0.3.7 va de 30701 a 30799, siempre por debajo de 0.3.8 (30800). Sin
  etiquetas, la base es 0.0.0.

## Compilarlo a mano

1. GitHub → **Actions** → **APK de Android** → **Run workflow**.
2. Elige la rama (fuera de `main` siempre sale de **prueba**) y escribe la URL de la API: `https://host[:puerto][/ruta]`,
   sin `/api/v1`, usuario ni query (por ejemplo `https://iris.tuisp.gt`). Si no cumple, el job falla
   al empezar: la app se niega a hablar con la API en claro fuera de desarrollo.
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
3. GitHub → **Settings → Environments → New environment** → `android-firma`:
    - **Deployment branches and tags → Selected branches and tags**: la rama `main` y las
      etiquetas `v*` (nada más).
    - **Required reviewers**: el dueño del repo (y quien él decida). Cada firma real espera su
      aprobación.
    - **Environment secrets** (no como secretos del repo):
        - `ANDROID_KEYSTORE_BASE64`: el contenido de `iris-campo.jks.b64`;
        - `ANDROID_KEYSTORE_PASSWORD`: contraseña del keystore;
        - `ANDROID_KEY_ALIAS`: `iris-campo`;
        - `ANDROID_KEY_PASSWORD`: contraseña de la llave.
    - Si estaban como secretos del repositorio, bórralos de ahí.
4. **Settings → Secrets and variables → Actions → Variables**: `IRIS_MOVIL_API_URL` = URL https de
   la API de producción.
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

- El nombre termina en `-prueba.apk` (`iris-campo-0.3.7-dev+abc1234-prueba.apk`). Una versión
  nunca sale de prueba: si falta la llave, el job falla.
- El resumen del job lo dice en un recuadro («APK de PRUEBA») y el certificado es
  `CN=Iris Campo PRUEBA`.
- En el teléfono: `apksigner verify --print-certs iris-campo-…apk` (Android SDK) muestra el mismo DN.

Un APK de prueba sirve para probar en un teléfono limpio, **no** para distribuir: cada corrida usa
otra llave, así que no actualiza una instalación anterior.

## Verificar un APK

Compara el SHA-256 del archivo con el del resumen del job: `sha256sum iris-campo-0.3.0.apk`.
