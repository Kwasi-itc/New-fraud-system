import "server-only";
import { readFileSync } from "node:fs";

// Server configuration only. Docker mounts credentials as files; browser
// bundles and build arguments never receive service credentials.
export function serviceSecret(name: string): string | undefined {
  const file = process.env[`${name}_FILE`];
  const value = file ? readFileSync(file, "utf8").trim() : process.env[name]?.trim();
  return value || undefined;
}
