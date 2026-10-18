"""Build a Windows .ico with DIB entries for small sizes and PNG for 256.

Explorer only accepts PNG-compressed entries at 256x256; every smaller size must
be a BMP (DIB) entry, otherwise list/small-icon views render blank.
"""
import struct
import sys

from PIL import Image

SIZES = [16, 24, 32, 48, 64, 128, 256]


def dib_bytes(image):
    """BITMAPINFOHEADER + 32bpp BGRA XOR map + empty AND mask, bottom-up."""
    image = image.convert('RGBA')
    w, h = image.size
    pixels = image.load()

    header = struct.pack(
        '<IiiHHIIiiII',
        40,          # biSize
        w,           # biWidth
        h * 2,       # biHeight (XOR + AND)
        1,           # biPlanes
        32,          # biBitCount
        0,           # biCompression = BI_RGB
        w * h * 4,   # biSizeImage
        0, 0, 0, 0,
    )

    xor = bytearray()
    for y in range(h - 1, -1, -1):
        for x in range(w):
            r, g, b, a = pixels[x, y]
            xor += bytes((b, g, r, a))

    mask_row = ((w + 31) // 32) * 4
    and_mask = bytes(mask_row * h)

    return header + bytes(xor) + and_mask


def png_bytes(image):
    from io import BytesIO

    buf = BytesIO()
    image.save(buf, format='PNG')
    return buf.getvalue()


def build(source, out, sizes=SIZES):
    base = Image.open(source).convert('RGBA')
    entries = []
    for size in sizes:
        img = base.resize((size, size), Image.LANCZOS)
        data = png_bytes(img) if size >= 256 else dib_bytes(img)
        entries.append((size, img, data))

    payload = bytearray()
    directory = bytearray()
    offset = 6 + 16 * len(entries)
    for size, _img, data in entries:
        directory += struct.pack(
            '<BBBBHHII',
            size if size < 256 else 0,
            size if size < 256 else 0,
            0, 0, 1, 32, len(data), offset,
        )
        payload += data
        offset += len(data)

    with open(out, 'wb') as fh:
        fh.write(struct.pack('<HHH', 0, 1, len(entries)))
        fh.write(directory)
        fh.write(payload)


if __name__ == '__main__':
    build(sys.argv[1], sys.argv[2])
    print('written', sys.argv[2])
