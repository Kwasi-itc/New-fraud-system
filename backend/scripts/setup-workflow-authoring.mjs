// Prepare the local Compose credential without printing it or rotating an
// existing value. Run from any directory with Node.js.
import { randomBytes } from "node:crypto";
import { readFileSync, appendFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";

const root = fileURLToPath(new URL("../../", import.meta.url));
const path = fileURLToPath(new URL("../../.env", import.meta.url));
execFileSync("git", ["check-ignore", "--quiet", ".env"], { cwd: root });
const content = readFileSync(path, "utf8");
const matches = [...content.matchAll(/^WORKFLOW_AUTHORING_TOKEN=(.*)$/gm)];
if (matches.length) {
  const value = matches[0][1].trim().replace(/^(['"])(.*)\1$/, "$2");
  if (matches.length !== 1 || !/^[A-Za-z0-9_-]{32,}$/.test(value)) {
    throw new Error("The existing WORKFLOW_AUTHORING_TOKEN needs review; it was not changed.");
  }
  console.log("Existing private workflow credential retained.");
} else {
  const newline = content.includes("\r\n") ? "\r\n" : "\n";
  appendFileSync(path, `${content.endsWith("\n") ? "" : newline}WORKFLOW_AUTHORING_TOKEN=${randomBytes(48).toString("base64url")}${newline}`, "utf8");
  console.log("Private workflow credential added to ignored local configuration.");
}
