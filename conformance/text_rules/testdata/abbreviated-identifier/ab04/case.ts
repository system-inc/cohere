export function parity(): number {
  const userMsg = 'a'
  const msgCount = 1
  const ctxValue = 2
  const pageIdx = 3
  const errMsg = 'b'
  return userMsg.length + msgCount + ctxValue + pageIdx + errMsg.length
}
