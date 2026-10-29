#!/usr/bin/env python3
"""Sumidero SMTP mínimo para la prueba e2e Iris ↔ Argos (sin dependencias).

Acepta cualquier correo en 127.0.0.1:<puerto> y guarda cada uno como <dir>/<n>.eml. Sin TLS ni
autenticación: solo para el laboratorio local de infra/e2e/argos.sh. Uso:

    python3 smtp_sumidero.py <puerto> <dir>
"""
import asyncio
import os
import sys


async def atender(lector, escritor, carpeta):
    def responder(linea):
        escritor.write((linea + "\r\n").encode())

    responder("220 sumidero-e2e ESMTP")
    await escritor.drain()
    datos = None
    while True:
        linea = await lector.readline()
        if not linea:
            break
        if datos is not None:
            if linea in (b".\r\n", b".\n"):
                n = len(os.listdir(carpeta)) + 1
                with open(os.path.join(carpeta, f"{n:04d}.eml"), "wb") as f:
                    f.write(b"".join(datos))
                datos = None
                responder("250 OK guardado")
            else:
                datos.append(linea[1:] if linea.startswith(b"..") else linea)
                continue
        else:
            orden = linea.decode(errors="replace").strip().upper()
            if orden.startswith(("EHLO", "HELO")):
                responder("250-sumidero-e2e")
                responder("250 8BITMIME")
            elif orden.startswith("DATA"):
                datos = []
                responder("354 Termina con <CRLF>.<CRLF>")
            elif orden.startswith("QUIT"):
                responder("221 Adiós")
                await escritor.drain()
                break
            else:
                responder("250 OK")
        await escritor.drain()
    escritor.close()


async def principal(puerto, carpeta):
    os.makedirs(carpeta, exist_ok=True)
    servidor = await asyncio.start_server(lambda l, e: atender(l, e, carpeta), "127.0.0.1", puerto)
    async with servidor:
        await servidor.serve_forever()


if __name__ == "__main__":
    asyncio.run(principal(int(sys.argv[1]), sys.argv[2]))
