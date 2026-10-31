# Changelog

## [0.5.0](https://github.com/HCDestroyer/iris/compare/v0.4.0...v0.5.0) (2026-10-31)


### Funcionalidades

* **panel:** navegación con HeroUI, fase 1 (menú agrupado, migas, Ctrl+K, avisos y barra inferior) ([#80](https://github.com/HCDestroyer/iris/issues/80)) ([dcd3dd6](https://github.com/HCDestroyer/iris/commit/dcd3dd6a463a2c00c311b311737a313077bbcfc9))
* **ui:** HeroUI v3 con la identidad Horus, fase 0 (base) ([#77](https://github.com/HCDestroyer/iris/issues/77)) ([4526716](https://github.com/HCDestroyer/iris/commit/45267169eeacbd26bfbc76d5603befc6f7bab943))

## [0.4.0](https://github.com/HCDestroyer/iris/compare/v0.3.0...v0.4.0) (2026-10-31)


### Funcionalidades

* **panel:** crear, editar y desactivar usuarios, sus empresas y crear el usuario desde «Nuevo técnico» ([#75](https://github.com/HCDestroyer/iris/issues/75)) ([9362005](https://github.com/HCDestroyer/iris/commit/9362005dd360142e11855fbb98293608b982d087))

## [0.3.0](https://github.com/HCDestroyer/iris/compare/v0.2.0...v0.3.0) (2026-10-31)


### Funcionalidades

* **campo:** crear orden y asignar desde el ticket, igualar asignación y propuesta de resolver al cerrar ([#69](https://github.com/HCDestroyer/iris/issues/69)) ([f7a7d06](https://github.com/HCDestroyer/iris/commit/f7a7d064b3de61c353835a6cd9df73215aedaf95))
* **infra:** instalador de producción (install.sh / irisctl) y paquete en cada release ([#66](https://github.com/HCDestroyer/iris/issues/66)) ([a2b426d](https://github.com/HCDestroyer/iris/commit/a2b426d3e165ebe6e92204e4d6ad81e3462a3731))
* **integracion:** conectar con Argos con un botón (contrato 0.4.0 §1.4) y panel/API por separado ([#68](https://github.com/HCDestroyer/iris/issues/68)) ([4fe365a](https://github.com/HCDestroyer/iris/commit/4fe365a46d1f75332936864f321a4475f8e45174))
* **integracion:** contrato ArgosISP 0.3.1, servicio y router eliminados, 409 de negocio y alcance del canje ([#60](https://github.com/HCDestroyer/iris/issues/60)) ([b87dd4d](https://github.com/HCDestroyer/iris/commit/b87dd4df9ff2cb87fc6eb816cb8f20bc19031112))
* **nucleo:** historial de accesos, actividad de la app de campo y roles y permisos ([#71](https://github.com/HCDestroyer/iris/issues/71)) ([87ce483](https://github.com/HCDestroyer/iris/commit/87ce483db0bb5b65d974810451e46889bbc1ccc6))
* **panel:** agenda en calendario y pantallas de técnicos, cuadrillas y zonas ([#72](https://github.com/HCDestroyer/iris/issues/72)) ([fd22c8a](https://github.com/HCDestroyer/iris/commit/fd22c8a49037fb1e8f3dc61cbf73f63e8c4544cc))
* **web:** selector de cliente, comercial sin scrolls y patrón de modales con validación ([#73](https://github.com/HCDestroyer/iris/issues/73)) ([36b88b6](https://github.com/HCDestroyer/iris/commit/36b88b6683f0d796ff28f94ba45fbb13474a362b))


### Correcciones

* **infra:** certificados de Mailpit compatibles con OpenSSL 1.1.1 ([#65](https://github.com/HCDestroyer/iris/issues/65)) ([59e6070](https://github.com/HCDestroyer/iris/commit/59e60708132f3998de5fae133e22c4de3befff1d))
* **integracion:** datos de Argos coherentes y aislados por empresa ([#70](https://github.com/HCDestroyer/iris/issues/70)) ([57f8aa8](https://github.com/HCDestroyer/iris/commit/57f8aa83f4a7597479bc5a08b949a3de1048439d))
* **movil:** la firma del APK no falla en silencio por el resumen del certificado ([#64](https://github.com/HCDestroyer/iris/issues/64)) ([828eaa7](https://github.com/HCDestroyer/iris/commit/828eaa75dd42108a644db740750073e92ac13b3a))

## [0.2.0](https://github.com/HCDestroyer/iris/compare/v0.1.0...v0.2.0) (2026-10-30)


### Funcionalidades

* **nucleo:** autologin desde Argos («Ver en Iris») con vinculación de usuarios ([#56](https://github.com/HCDestroyer/iris/issues/56)) ([cd14a24](https://github.com/HCDestroyer/iris/commit/cd14a24fbe361aab460ceae79f0a9381d40bf085))
* **nucleo:** descartar la vinculación SSO y liberar la idempotencia ante rechazos de autorización ([#58](https://github.com/HCDestroyer/iris/issues/58)) ([fca23b9](https://github.com/HCDestroyer/iris/commit/fca23b90216f25450975d56c6f4d1bcfe8042867))
* **panel:** entrada desde Argos, re-autenticación, cuentas vinculadas y conectores ([#57](https://github.com/HCDestroyer/iris/issues/57)) ([7171be9](https://github.com/HCDestroyer/iris/commit/7171be970c3a6f0ae3709f4e66af90c66ac5f2c4))


### Correcciones

* **integracion:** conector Argos según el contrato, con canje solo a orígenes conocidos ([#50](https://github.com/HCDestroyer/iris/issues/50)) ([cd9bbe7](https://github.com/HCDestroyer/iris/commit/cd9bbe79a840168cc580a71742d87f2d65b3cc01))
* **integracion:** empresa del cliente de Argos según sus servicios ([#52](https://github.com/HCDestroyer/iris/issues/52)) ([ab21e27](https://github.com/HCDestroyer/iris/commit/ab21e27207b53a48a615325ba60d6c6c9eb92d74))
* **integracion:** lista de orígenes siempre obligatoria y reconexión sin cortar la integración ([#54](https://github.com/HCDestroyer/iris/issues/54)) ([0faf0cb](https://github.com/HCDestroyer/iris/commit/0faf0cb43230b712fbf94cb9db0c11c9cbc5dd61))

## [0.1.0](https://github.com/HCDestroyer/iris/compare/v0.1.0...v0.1.0) (2026-10-29)


### Funcionalidades

* **agenda:** agenda con citas y ventana, posiciones en vivo y pendientes de la Fase 2 (I2 · Fase 3.1) ([#20](https://github.com/HCDestroyer/iris/issues/20)) ([40f9b7d](https://github.com/HCDestroyer/iris/commit/40f9b7d23900e73c0e9b1da0383e7fdc2e29168a))
* **agenda:** ruta del día con OSRM, publicación y reasignación en caliente (I2 · Fase 3.2) ([#22](https://github.com/HCDestroyer/iris/issues/22)) ([6437a97](https://github.com/HCDestroyer/iris/commit/6437a97d3c3bf86be1160b329304b1c4858f3edb))
* **campo:** I2 · Fase 2 (punto 1): órdenes de trabajo y API de sincronización de la app de campo ([#15](https://github.com/HCDestroyer/iris/issues/15)) ([26acf9f](https://github.com/HCDestroyer/iris/commit/26acf9f59940103fd18c9f29e62115e96fefd671))
* **campo:** I4 · Fase 2 (tarea 2): app de campo /campo (PWA sin conexión) ([#17](https://github.com/HCDestroyer/iris/issues/17)) ([52cba5a](https://github.com/HCDestroyer/iris/commit/52cba5adb312cefaed30f804aa705e23c6a5ea6f))
* **campo:** plantilla común solo explícita y GET /plantillas paginado ([#35](https://github.com/HCDestroyer/iris/issues/35)) ([0dee37c](https://github.com/HCDestroyer/iris/commit/0dee37ca0bfbdbfe7be00cc1cd94dce609229711))
* **campo:** plantillas de formularios con versiones, varios formularios por orden y pausa de tareas (I2) ([#29](https://github.com/HCDestroyer/iris/issues/29)) ([d80537c](https://github.com/HCDestroyer/iris/commit/d80537cd95bb28f7b55235b598cf34cca88dcc3d))
* **campo:** ubicación en vivo del técnico y «en camino» en la app de campo (I4 · Fase 3.3) ([#23](https://github.com/HCDestroyer/iris/issues/23)) ([196583d](https://github.com/HCDestroyer/iris/commit/196583d40c9ef47f3ec0c45b11225f48064447ca))
* **campo:** ubicación según el turno de la agenda (I4) ([#26](https://github.com/HCDestroyer/iris/issues/26)) ([17fa9d1](https://github.com/HCDestroyer/iris/commit/17fa9d1899fc277e001abbf1ff3c81a54fc4283b))
* **comercial:** prospectos, factibilidad, cotización, contrato con firma y ganar (I2 · Fase 4.1) ([#28](https://github.com/HCDestroyer/iris/issues/28)) ([f393dbc](https://github.com/HCDestroyer/iris/commit/f393dbce393386837a0b2c953ff365ea81009835))
* **infra:** I5 · Fase 0: monorepo, docker-compose, CI y release ([#1](https://github.com/HCDestroyer/iris/issues/1)) ([a91edbc](https://github.com/HCDestroyer/iris/commit/a91edbc31c0ccd0b20d6e1096356e4ff24677841))
* **infra:** Playwright en CI, imágenes de producción, compose completo y respaldos cifrados (I5 · tareas 2–5) ([#18](https://github.com/HCDestroyer/iris/issues/18)) ([b1e3c6c](https://github.com/HCDestroyer/iris/commit/b1e3c6c4adc51b4e5ab19b04e3331882543bfb66))
* **infra:** pruebas de carga con k6 ([#42](https://github.com/HCDestroyer/iris/issues/42)) ([39f99f5](https://github.com/HCDestroyer/iris/commit/39f99f5bb60bccac97f1e26a2956a61e3737ad46))
* **integracion:** I1 · Fase 1 (punto 2): conector Argos de lectura y empresa activa ([#6](https://github.com/HCDestroyer/iris/issues/6)) ([e9955a5](https://github.com/HCDestroyer/iris/commit/e9955a5df8a501382c50c1594eaaf307f5fa38bc))
* **integracion:** I1 · Fase 1 (punto 4b): conector Horus, portal del cliente y roles de integración ([#9](https://github.com/HCDestroyer/iris/issues/9)) ([7219874](https://github.com/HCDestroyer/iris/commit/7219874bbf57e873bf463691bd1ad4da7c168876))
* **integracion:** respuesta guardada, cuerpos con referencias verificadas y 409 de negocio en la cola de pendientes (I1) ([#32](https://github.com/HCDestroyer/iris/issues/32)) ([d1183ac](https://github.com/HCDestroyer/iris/commit/d1183acdb860c97be3ff10a2a2f52a8ec3af70ec))
* **movil:** APK de Android compilado y firmado en GitHub Actions ([#49](https://github.com/HCDestroyer/iris/issues/49)) ([f9bcfe3](https://github.com/HCDestroyer/iris/commit/f9bcfe3dbd28b44de86a8a36402ca5d0760b7796))
* **movil:** app de campo nativa con Expo ([#39](https://github.com/HCDestroyer/iris/issues/39)) ([6317278](https://github.com/HCDestroyer/iris/commit/6317278d92f31c2e4193a697eb33626a6b1bf2e8))
* **nucleo:** contadores de reiniciar-mfa, purgas y hash_formato sin DEFAULT ([#37](https://github.com/HCDestroyer/iris/issues/37)) ([f2bb904](https://github.com/HCDestroyer/iris/commit/f2bb9047663673407676228cafb19e7aa08be238))
* **nucleo:** I1 · Fase 0 (tareas 1–3): núcleo de la API, casos de uso auditados e identidad ([#3](https://github.com/HCDestroyer/iris/issues/3)) ([e7b4ae1](https://github.com/HCDestroyer/iris/commit/e7b4ae15a6d51a03383d310257f3da5891d1bffe))
* **nucleo:** I1 · Fase 2: adjuntos con subida reanudable y cola de pendientes hacia Argos ([#14](https://github.com/HCDestroyer/iris/issues/14)) ([a3d2373](https://github.com/HCDestroyer/iris/commit/a3d2373c6437cb842bf7e97e0b8f1e3738ed8f06))
* **nucleo:** rotar la clave de cifrado, reiniciar MFA con contraseña temporal e iris salud (I1) ([#21](https://github.com/HCDestroyer/iris/issues/21)) ([a7e7c22](https://github.com/HCDestroyer/iris/commit/a7e7c2200ffc968dcc31730f5e67e249ae46af3e))
* **nucleo:** rutas con secreto enmascaradas en el log de acceso ([#43](https://github.com/HCDestroyer/iris/issues/43)) ([0c8a214](https://github.com/HCDestroyer/iris/commit/0c8a214c1799b8b05f904f22c905918140ec4877))
* **reportes:** SLA, resolución, órdenes por técnico, primera visita y reincidencias ([#38](https://github.com/HCDestroyer/iris/issues/38)) ([7a8c954](https://github.com/HCDestroyer/iris/commit/7a8c95460ae9f0721909097024d3685c171ff38f))
* **soporte:** encuesta de satisfacción y reporte de satisfacción ([#41](https://github.com/HCDestroyer/iris/issues/41)) ([e1538df](https://github.com/HCDestroyer/iris/commit/e1538df714eb10631c2e909d35257c238805ed9a))
* **soporte:** I2 · Fase 1 (punto 1): tickets y SLA ([#5](https://github.com/HCDestroyer/iris/issues/5)) ([cab7245](https://github.com/HCDestroyer/iris/commit/cab7245e7a55c0ce3f40a5b58fdafbac5f68bc61))
* **soporte:** I2 · Fase 1 (punto 4a): entrada de tickets por correo (IMAP) y respuesta por SMTP ([#8](https://github.com/HCDestroyer/iris/issues/8)) ([4701888](https://github.com/HCDestroyer/iris/commit/47018889fb3699daa684d419912096f9cd1df992))
* **soporte:** I2 · Fase 1 (punto 4c): tickets desde Horus e incidentes maestros, y acciones del cliente en el portal ([#11](https://github.com/HCDestroyer/iris/issues/11)) ([74d7ca6](https://github.com/HCDestroyer/iris/commit/74d7ca6848dee419807bab4b483222c906cfdba3))
* **sync:** I4 · Fase 2 (tarea 1): motor de sincronización sin conexión @iris/sync ([#13](https://github.com/HCDestroyer/iris/issues/13)) ([254cb0c](https://github.com/HCDestroyer/iris/commit/254cb0cc8614f9260cbb7d0164defc39420ce1c2))
* **web:** agenda y despacho con calendario, arrastrar y soltar, mapa en vivo y ruta propuesta (I3 · Fase 3.4) ([#24](https://github.com/HCDestroyer/iris/issues/24)) ([400536d](https://github.com/HCDestroyer/iris/commit/400536d78ac36be671d1674c6528ae88e69b95de))
* **web:** cambio de contraseña, MFA propio y reinicio de MFA en Usuarios (I3) ([#27](https://github.com/HCDestroyer/iris/issues/27)) ([dc2808f](https://github.com/HCDestroyer/iris/commit/dc2808f079ee5d61bd5295db360fe5c2e7020c2c))
* **web:** editor de plantillas, formularios de la orden y tareas pausadas en el panel (I3) ([#33](https://github.com/HCDestroyer/iris/issues/33)) ([2631986](https://github.com/HCDestroyer/iris/commit/26319869f75b7122439a0bda6004afb98b815997))
* **web:** encuesta pública, tarjeta en el panel, lista y reporte de satisfacción ([#46](https://github.com/HCDestroyer/iris/issues/46)) ([b6b65e2](https://github.com/HCDestroyer/iris/commit/b6b65e2dd8082555e37ae7fd1a20f18e8c895756))
* **web:** formularios y pausa en la app de campo, sin señal ([#34](https://github.com/HCDestroyer/iris/issues/34)) ([a62bb25](https://github.com/HCDestroyer/iris/commit/a62bb2546a1ede0e651c6569b43d085e2625b3cb))
* **web:** I3 · Fase 0 (tareas 1–2): panel con identidad Horus, inicio de sesión con MFA y menú por permisos ([#4](https://github.com/HCDestroyer/iris/issues/4)) ([818ff8e](https://github.com/HCDestroyer/iris/commit/818ff8ea1db4e3406a34c3e3abe3975cd0d98063))
* **web:** I3 · Fase 1 (punto 3): bandeja y detalle de tickets, alta rápida y empresa activa en el panel ([#7](https://github.com/HCDestroyer/iris/issues/7)) ([91b2841](https://github.com/HCDestroyer/iris/commit/91b28419e5d9deb20b9cfa1709e9a7282d9a66c0))
* **web:** I3 · Fase 1 (tarea 7): portal de soporte del cliente en /soporte ([#12](https://github.com/HCDestroyer/iris/issues/12)) ([1f0d6b6](https://github.com/HCDestroyer/iris/commit/1f0d6b69b3019d014f5676ae1d2b8a1c29488363))
* **web:** I3 · Fase 1: buzones de correo, «Ver en Argos/Horus» y contacto sin verificar en el panel ([#10](https://github.com/HCDestroyer/iris/issues/10)) ([bf20db3](https://github.com/HCDestroyer/iris/commit/bf20db3a462518c7362e0c78bf269d1dd9b10591))
* **web:** I3 · Fase 2 (tarea 4): tablero y detalle de órdenes con evidencias y pendientes en Argos ([#16](https://github.com/HCDestroyer/iris/issues/16)) ([dacd361](https://github.com/HCDestroyer/iris/commit/dacd361118a7a55ecd04f82337a453aa6e10a873))
* **web:** panel comercial con embudo, mapa de cobertura, cotización, contrato y ganar (I3 · Fase 4.2) ([#31](https://github.com/HCDestroyer/iris/issues/31)) ([d823bb9](https://github.com/HCDestroyer/iris/commit/d823bb94ce1e18b9ccf3f2fc4920a23ac8ecf1d4))
* **web:** pantallas de reportes en el panel ([#40](https://github.com/HCDestroyer/iris/issues/40)) ([806f41b](https://github.com/HCDestroyer/iris/commit/806f41b9a183568cb242ee99526b3dddd075647f))
* **web:** plantillas con cursor y «Cargar más» en la lista y en «Agregar formulario» ([#36](https://github.com/HCDestroyer/iris/issues/36)) ([562452c](https://github.com/HCDestroyer/iris/commit/562452c159acc4da41534ba1499ce8d5642c482e))


### Correcciones

* **nucleo:** guardar el User-Agent de login, MFA y canje del portal ([#44](https://github.com/HCDestroyer/iris/issues/44)) ([36de2ce](https://github.com/HCDestroyer/iris/commit/36de2cef63e6912d7e2529d89d1f1a07c60235a7))
* **nucleo:** idempotencia con HMAC, purga periódica y GET /usuarios/{id} (I1) ([#30](https://github.com/HCDestroyer/iris/issues/30)) ([5ce722e](https://github.com/HCDestroyer/iris/commit/5ce722e40d8897bebe1e4dd173b604b5d0c99b56))
* **nucleo:** IP real del cliente detrás del panel, confiando solo en la IP fija del panel ([#47](https://github.com/HCDestroyer/iris/issues/47)) ([efef180](https://github.com/HCDestroyer/iris/commit/efef180c4583104945718fc73fcbb77d46e00980))


### Rendimiento

* **soporte:** cron de SLA por índices y búsqueda de tickets con trigramas ([#45](https://github.com/HCDestroyer/iris/issues/45)) ([067c4cf](https://github.com/HCDestroyer/iris/commit/067c4cf850dbc82695280c2bcc7c94d5b53a2755))
