export function safely(): string {
  try {
    return JSON.stringify({})
  } catch (e) {
    return String(e)
  }
}
