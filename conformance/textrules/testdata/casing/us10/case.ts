declare const process: { env: Record<string, string | undefined> }

export function databaseAddress(): string {
  const database_url = process.env.DATABASE_URL ?? ''
  return database_url
}
