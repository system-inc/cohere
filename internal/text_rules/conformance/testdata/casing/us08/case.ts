declare const fs: { constants: { O_RDONLY: number } }

export function readMode(): number {
  const O_RDONLY_MODE = fs.constants.O_RDONLY
  return O_RDONLY_MODE
}
