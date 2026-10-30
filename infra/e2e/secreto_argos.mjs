// Descifra (formato v1 de Argos: AES-256-GCM con AAA_PPP_KEY) el secreto del receptor de Iris que
// Argos guarda en integraciones.iris_conector, para la prueba de firma y deduplicación de la e2e.
// Lee el texto cifrado por stdin y escribe el secreto por stdout. Solo laboratorio.
import { createDecipheriv } from 'node:crypto'

const clave = Buffer.from(process.env.AAA_PPP_KEY ?? '', 'base64')
let entrada = ''
for await (const parte of process.stdin) entrada += parte
const [version, iv, tag, datos] = entrada.trim().split('.')
if (version !== 'v1' || clave.length !== 32) {
    console.error('formato o clave inválidos')
    process.exit(1)
}
const d = createDecipheriv('aes-256-gcm', clave, Buffer.from(iv, 'base64url'))
d.setAuthTag(Buffer.from(tag, 'base64url'))
process.stdout.write(Buffer.concat([d.update(Buffer.from(datos, 'base64url')), d.final()]).toString('utf8'))
