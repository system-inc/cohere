declare const process: { env: Record<string, string | undefined> }

export function databaseAddress(): string {
  const DATABASE_URL = process.env.DATABASE_URL ?? ''
  return DATABASE_URL
}
