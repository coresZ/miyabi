// Generates a simple square PNG (no dependencies) used as the source for
// `tauri icon`, so the repo ships no binary assets. Usage:
//   node scripts/gen-icon.mjs src-tauri/app-icon.png 1024
import { deflateSync } from 'node:zlib'
import { writeFileSync } from 'node:fs'

const out = process.argv[2] ?? 'app-icon.png'
const size = Number(process.argv[3] ?? 1024)

const crcTable = (() => {
  const table = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    table[n] = c >>> 0
  }
  return table
})()

function crc32(buffer) {
  let c = 0xffffffff
  for (let i = 0; i < buffer.length; i++) c = crcTable[(c ^ buffer[i]) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}

function chunk(type, data) {
  const length = Buffer.alloc(4)
  length.writeUInt32BE(data.length, 0)
  const typeBuf = Buffer.from(type, 'ascii')
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(Buffer.concat([typeBuf, data])), 0)
  return Buffer.concat([length, typeBuf, data, crc])
}

// Diagonal indigo gradient, opaque.
const stride = 1 + size * 4
const raw = Buffer.alloc(stride * size)
for (let y = 0; y < size; y++) {
  const rowStart = y * stride
  raw[rowStart] = 0 // filter: none
  for (let x = 0; x < size; x++) {
    const i = rowStart + 1 + x * 4
    const t = (x + y) / (2 * size)
    raw[i] = 63 + Math.round(40 * t)
    raw[i + 1] = 56 + Math.round(24 * t)
    raw[i + 2] = 190 - Math.round(40 * t)
    raw[i + 3] = 255
  }
}

const ihdr = Buffer.alloc(13)
ihdr.writeUInt32BE(size, 0)
ihdr.writeUInt32BE(size, 4)
ihdr[8] = 8 // bit depth
ihdr[9] = 6 // color type: RGBA

const png = Buffer.concat([
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
  chunk('IHDR', ihdr),
  chunk('IDAT', deflateSync(raw)),
  chunk('IEND', Buffer.alloc(0))
])

writeFileSync(out, png)
console.log(`wrote ${out} (${size}x${size})`)
